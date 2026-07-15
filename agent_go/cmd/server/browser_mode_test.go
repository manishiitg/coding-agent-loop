package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workflowtypes"
)

func TestApplyRuntimeBrowserModeUsesCDPWhenReachable(t *testing.T) {
	resetRuntimeCDPReachabilityCacheForTest()
	workspace := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/cdp-check" || r.URL.Query().Get("port") != "9333" {
			t.Fatalf("unexpected CDP check URL: %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"connected":true}`))
	}))
	defer workspace.Close()
	t.Setenv("WORKSPACE_API_URL", workspace.URL)
	port := 9333

	req := applyRuntimeBrowserMode(context.Background(), QueryRequest{BrowserMode: "auto", CdpPort: &port})
	if req.BrowserMode != "cdp" || req.CdpPort == nil || *req.CdpPort != port {
		t.Fatalf("auto resolution = mode %q port %v, want cdp:%d", req.BrowserMode, req.CdpPort, port)
	}
	if req.EnableBrowserAccess == nil || !*req.EnableBrowserAccess {
		t.Fatalf("auto CDP should enable browser access")
	}
}

func TestApplyRuntimeBrowserModeFallsBackToHeadlessWhenCDPUnavailable(t *testing.T) {
	resetRuntimeCDPReachabilityCacheForTest()
	workspace := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"connected":false}`))
	}))
	defer workspace.Close()
	t.Setenv("WORKSPACE_API_URL", workspace.URL)

	req := applyRuntimeBrowserMode(context.Background(), QueryRequest{BrowserMode: "auto"})
	if req.BrowserMode != "headless" || req.CdpPort != nil {
		t.Fatalf("auto resolution = mode %q port %v, want headless with no CDP port", req.BrowserMode, req.CdpPort)
	}
	if req.EnableBrowserAccess == nil || !*req.EnableBrowserAccess {
		t.Fatalf("auto headless fallback should enable browser access")
	}
}

func TestApplyRuntimeBrowserModeKeepsReachableConfiguredProfiles(t *testing.T) {
	resetRuntimeCDPReachabilityCacheForTest()
	workspace := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		port := r.URL.Query().Get("port")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"connected":` + map[bool]string{true: "true", false: "false"}[port == "9333"] + `}`))
	}))
	defer workspace.Close()
	t.Setenv("WORKSPACE_API_URL", workspace.URL)

	req := applyRuntimeBrowserMode(context.Background(), QueryRequest{BrowserMode: "auto", CdpPorts: []int{9222, 9333}})
	if req.BrowserMode != "cdp" || req.CdpPort == nil || *req.CdpPort != 9333 {
		t.Fatalf("auto multi-profile resolution = mode %q primary %v, want cdp:9333", req.BrowserMode, req.CdpPort)
	}
	if len(req.CdpPorts) != 1 || req.CdpPorts[0] != 9333 {
		t.Fatalf("reachable CDP ports = %v, want [9333]", req.CdpPorts)
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

func TestWithEffectiveBrowserModeUsesSessionModeWhenRequestIsUnspecified(t *testing.T) {
	sessionID := "test-effective-browser-mode-session"
	common.SetSessionBrowserMode(sessionID, "cdp")

	var api *StreamingAPI
	req := api.withEffectiveBrowserMode(context.Background(), QueryRequest{}, sessionID)

	if req.BrowserMode != "cdp" {
		t.Fatalf("browser mode = %q, want cdp", req.BrowserMode)
	}
}

func TestWithEffectiveBrowserModeHonorsExplicitNone(t *testing.T) {
	sessionID := "test-effective-browser-mode-explicit-none"
	common.SetSessionBrowserMode(sessionID, "cdp")

	var api *StreamingAPI
	req := api.withEffectiveBrowserMode(context.Background(), QueryRequest{BrowserMode: "none"}, sessionID)

	if req.BrowserMode != "none" {
		t.Fatalf("browser mode = %q, want explicit none", req.BrowserMode)
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
