package server

import (
	"reflect"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workflowtypes"
)

func TestConfiguredCDPPortsForAutoRemainCandidates(t *testing.T) {
	ports := configuredCDPPortsForMode("auto", nil, nil)
	if !reflect.DeepEqual(ports, []int{9222}) {
		t.Fatalf("default auto candidates = %v, want [9222]", ports)
	}
	ports = configuredCDPPortsForMode("auto", nil, []int{9222, 9333})
	if !reflect.DeepEqual(ports, []int{9222, 9333}) {
		t.Fatalf("configured auto candidates = %v", ports)
	}
	if ports := configuredCDPPortsForMode("headless", nil, []int{9222}); len(ports) != 0 {
		t.Fatalf("headless must not retain CDP candidates: %v", ports)
	}
}

func TestGetCdpPortsPreservesPrimaryAndDeduplicates(t *testing.T) {
	primary := 9333
	ports := getCdpPorts(QueryRequest{BrowserMode: "cdp", CdpPort: &primary, CdpPorts: []int{9222, 9333, -1, 9444}})
	want := []int{9333, 9222, 9444}
	if !reflect.DeepEqual(ports, want) {
		t.Fatalf("getCdpPorts() = %v, want %v", ports, want)
	}
}

func TestGetCdpPortsIgnoresStalePortsOutsideCDPModes(t *testing.T) {
	for _, mode := range []string{"none", "headless"} {
		t.Run(mode, func(t *testing.T) {
			primary := 9222
			ports := getCdpPorts(QueryRequest{BrowserMode: mode, CdpPort: &primary, CdpPorts: []int{9333}})
			if len(ports) != 0 {
				t.Fatalf("getCdpPorts() = %v for mode %q, want no CDP authorization", ports, mode)
			}
		})
	}
}

func TestBuildChatBrowserConfigUsesBrowserModeCDPWithoutEnableBrowserAccess(t *testing.T) {
	req := QueryRequest{BrowserMode: "cdp"}

	cfg := buildChatBrowserConfig(req)

	if !cfg.HasAgentBrowser {
		t.Fatalf("expected CDP browser_mode to enable agent_browser")
	}
	if cfg.Mode != "cdp" {
		t.Fatalf("mode = %q, want cdp", cfg.Mode)
	}
	if cfg.CdpPort != 9222 {
		t.Fatalf("cdp port = %d, want default 9222", cfg.CdpPort)
	}
}

func TestApplyMultiAgentCapabilitiesToRequestOverridesRequestCapabilities(t *testing.T) {
	globalSecrets := []string{"GLOBAL_TOKEN"}
	req := QueryRequest{
		Servers:               []string{"old-server"},
		EnabledServers:        []string{"old-enabled"},
		SelectedTools:         []string{"old:tool"},
		SelectedSkills:        []string{"old-skill"},
		BrowserMode:           "none",
		SelectedGlobalSecrets: &[]string{"OLD_GLOBAL"},
	}

	applyMultiAgentCapabilitiesToRequest(&req, WorkflowCapabilities{
		SelectedServers:           []string{"filesystem", "agent-browser"},
		SelectedTools:             []string{"filesystem:read_file"},
		SelectedSkills:            []string{"agent-browser"},
		SelectedGlobalSecretNames: &globalSecrets,
		BrowserMode:               "CDP",
		UseCodeExecutionMode:      true,
		LLMConfig: &workflowtypes.PresetLLMConfig{
			SchemaVersion: workflowtypes.LLMConfigSchemaVersion,
			Mode:          workflowtypes.LLMConfigModeExplicit,
			BuilderLLM: &workflowtypes.AgentLLMConfig{
				Provider: "openai",
				ModelID:  "gpt-test",
				Fallbacks: []workflowtypes.AgentLLMFallback{{
					Provider: "anthropic",
					ModelID:  "claude-test",
				}},
			},
		},
	})

	if len(req.Servers) != 0 {
		t.Fatalf("legacy servers = %v, want cleared", req.Servers)
	}
	if got := req.EnabledServers; len(got) != 2 || got[0] != "filesystem" || got[1] != "agent-browser" {
		t.Fatalf("enabled servers = %v, want saved selection", got)
	}
	if got := req.SelectedTools; len(got) != 1 || got[0] != "filesystem:read_file" {
		t.Fatalf("selected tools = %v, want saved selection", got)
	}
	if got := req.SelectedSkills; len(got) != 1 || got[0] != "agent-browser" {
		t.Fatalf("selected skills = %v, want saved selection", got)
	}
	if req.BrowserMode != "cdp" {
		t.Fatalf("browser mode = %q, want cdp", req.BrowserMode)
	}
	if req.EnableBrowserAccess == nil || !*req.EnableBrowserAccess {
		t.Fatalf("EnableBrowserAccess = %v, want true for cdp", req.EnableBrowserAccess)
	}
	if !req.UseCodeExecutionMode {
		t.Fatalf("UseCodeExecutionMode = false, want true")
	}
	if req.SelectedGlobalSecrets == nil || len(*req.SelectedGlobalSecrets) != 1 || (*req.SelectedGlobalSecrets)[0] != "GLOBAL_TOKEN" {
		t.Fatalf("selected global secrets = %v, want saved selection", req.SelectedGlobalSecrets)
	}
	if req.LLMConfig == nil || req.LLMConfig.Primary.Provider != "openai" || req.LLMConfig.Primary.ModelID != "gpt-test" {
		t.Fatalf("llm config = %+v, want saved phase llm", req.LLMConfig)
	}
	if len(req.LLMConfig.Fallbacks) != 1 || req.LLMConfig.Fallbacks[0].Provider != "anthropic" || req.LLMConfig.Fallbacks[0].ModelID != "claude-test" {
		t.Fatalf("llm fallbacks = %+v, want saved fallback", req.LLMConfig.Fallbacks)
	}
}

func TestApplyMultiAgentCapabilitiesToRequestDisablesBrowserForNone(t *testing.T) {
	enabled := true
	req := QueryRequest{
		BrowserMode:          "cdp",
		EnableBrowserAccess:  &enabled,
		UseCodeExecutionMode: true,
	}

	applyMultiAgentCapabilitiesToRequest(&req, WorkflowCapabilities{BrowserMode: "none"})

	if req.BrowserMode != "none" {
		t.Fatalf("browser mode = %q, want none", req.BrowserMode)
	}
	if req.EnableBrowserAccess == nil || *req.EnableBrowserAccess {
		t.Fatalf("EnableBrowserAccess = %v, want false for none", req.EnableBrowserAccess)
	}
	if req.UseCodeExecutionMode {
		t.Fatalf("UseCodeExecutionMode = true, want saved false")
	}
}

func TestApplyMultiAgentCapabilitiesToRequestEnablesBrowserForAuto(t *testing.T) {
	req := QueryRequest{}
	applyMultiAgentCapabilitiesToRequest(&req, WorkflowCapabilities{BrowserMode: "auto", CDPPorts: []int{9222, 9333}})

	if req.BrowserMode != "auto" {
		t.Fatalf("browser mode = %q, want auto", req.BrowserMode)
	}
	if req.EnableBrowserAccess == nil || !*req.EnableBrowserAccess {
		t.Fatalf("EnableBrowserAccess = %v, want true for auto", req.EnableBrowserAccess)
	}
	if !reflect.DeepEqual(req.CdpPorts, []int{9222, 9333}) {
		t.Fatalf("CdpPorts = %v, want saved multi-profile ports", req.CdpPorts)
	}
}
