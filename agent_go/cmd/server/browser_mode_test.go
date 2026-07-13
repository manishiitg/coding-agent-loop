package server

import (
	"context"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workflowtypes"
)

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

func TestWithEffectiveBrowserModeUsesSessionModeWhenRequestIsNone(t *testing.T) {
	sessionID := "test-effective-browser-mode-session"
	common.SetSessionBrowserMode(sessionID, "cdp")

	var api *StreamingAPI
	req := api.withEffectiveBrowserMode(context.Background(), QueryRequest{BrowserMode: "none"}, sessionID)

	if req.BrowserMode != "cdp" {
		t.Fatalf("browser mode = %q, want cdp", req.BrowserMode)
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
		SelectedSkills:            []string{"playwright-usage"},
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
	if got := req.SelectedSkills; len(got) != 1 || got[0] != "playwright-usage" {
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
