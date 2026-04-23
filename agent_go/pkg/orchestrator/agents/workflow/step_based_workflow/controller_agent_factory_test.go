package step_based_workflow

import (
	"context"
	"testing"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
)

func TestInjectStepEnvIntoShellExecutor_OverridesStaleMCPSessionEnv(t *testing.T) {
	t.Setenv("MCP_API_URL", "http://example.test/s/parent-session")

	var capturedArgs map[string]interface{}
	executors := map[string]interface{}{
		"execute_shell_command": func(ctx context.Context, args map[string]interface{}) (string, error) {
			capturedArgs = args
			return "ok", nil
		},
	}

	injectStepEnvIntoShellExecutor(
		executors,
		"/tmp/workflow/execution/math-solver",
		"/tmp/workflow/execution",
		"step-session-123",
	)

	execFn, ok := executors["execute_shell_command"].(func(context.Context, map[string]interface{}) (string, error))
	if !ok {
		t.Fatal("expected wrapped execute_shell_command executor")
	}

	_, err := execFn(context.Background(), map[string]interface{}{
		"command": "env",
		"extra_env": map[string]interface{}{
			"MCP_SESSION_ID":     "parent-session",
			"MCP_API_URL":        "http://example.test/s/parent-session",
			"STEP_OUTPUT_DIR":    "/stale/output",
			"STEP_EXECUTION_DIR": "/stale/execution",
		},
	})
	if err != nil {
		t.Fatalf("wrapped execute_shell_command returned error: %v", err)
	}

	rawExtraEnv, ok := capturedArgs["extra_env"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected extra_env map, got %#v", capturedArgs["extra_env"])
	}

	if got := rawExtraEnv["STEP_OUTPUT_DIR"]; got != "/tmp/workflow/execution/math-solver" {
		t.Fatalf("expected STEP_OUTPUT_DIR override, got %#v", got)
	}
	if got := rawExtraEnv["STEP_EXECUTION_DIR"]; got != "/tmp/workflow/execution" {
		t.Fatalf("expected STEP_EXECUTION_DIR override, got %#v", got)
	}
	if got := rawExtraEnv["MCP_SESSION_ID"]; got != "step-session-123" {
		t.Fatalf("expected MCP_SESSION_ID override, got %#v", got)
	}
	if got := rawExtraEnv["MCP_API_URL"]; got != "http://example.test/s/step-session-123" {
		t.Fatalf("expected step-scoped MCP_API_URL, got %#v", got)
	}
}

func TestApplyStepConfigToAgentConfigForcesCodeExecForCLIProviders(t *testing.T) {
	base, err := orchestrator.NewBaseOrchestrator(
		loggerv2.NewNoop(),
		nil,
		orchestrator.OrchestratorTypeWorkflow,
		"",
		0,
		"",
		[]string{"test-server"},
		nil,
		false,
		nil,
		1,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewBaseOrchestrator returned error: %v", err)
	}

	hcpo := &StepBasedWorkflowOrchestrator{BaseOrchestrator: base}
	config := agents.NewOrchestratorAgentConfig("test-agent")
	config.LLMConfig.Primary.Provider = "gemini-cli"

	hcpo.applyStepConfigToAgentConfig(config, nil, false)

	if !config.UseCodeExecutionMode {
		t.Fatalf("expected CLI providers to have code execution mode enabled")
	}
}

func TestSelectExecutionLLM_PrefersStepExecutionLLMOverSubAgentAndTiered(t *testing.T) {
	base, err := orchestrator.NewBaseOrchestrator(
		loggerv2.NewNoop(),
		nil,
		orchestrator.OrchestratorTypeWorkflow,
		"",
		0,
		"",
		nil,
		nil,
		false,
		&orchestrator.LLMConfig{},
		1,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewBaseOrchestrator returned error: %v", err)
	}

	hcpo := &StepBasedWorkflowOrchestrator{
		BaseOrchestrator: base,
		tierResolver: NewTierResolver(&TieredLLMConfig{
			Tier1: &AgentLLMConfig{Provider: "openai", ModelID: "tier-1"},
			Tier2: &AgentLLMConfig{Provider: "openai", ModelID: "tier-2"},
			Tier3: &AgentLLMConfig{Provider: "openai", ModelID: "tier-3"},
		}, nil),
	}

	ctx := context.WithValue(context.Background(), workshopTierContextKey{}, int(TierLow))
	ctx = context.WithValue(ctx, virtualtools.SubAgentLLMContextKey, &AgentLLMConfig{
		Provider: "openai",
		ModelID:  "sub-agent",
	})

	cfg := &AgentConfigs{
		ExecutionLLM: &AgentLLMConfig{
			Provider: "openai",
			ModelID:  "step-override",
			Fallbacks: []AgentLLMFallback{
				{Provider: "openai", ModelID: "step-fallback"},
			},
		},
	}

	llm := hcpo.selectExecutionLLM(ctx, cfg, "step-1")
	if llm == nil {
		t.Fatal("expected execution llm config, got nil")
	}
	if llm.Primary.ModelID != "step-override" {
		t.Fatalf("expected step override model, got %q", llm.Primary.ModelID)
	}
	if len(llm.Fallbacks) != 1 || llm.Fallbacks[0].ModelID != "step-fallback" {
		t.Fatalf("expected step fallback to be preserved, got %+v", llm.Fallbacks)
	}
}

func TestSelectExecutionLLM_UsesTierResolverWhenStepExecutionLLMIsUnset(t *testing.T) {
	base, err := orchestrator.NewBaseOrchestrator(
		loggerv2.NewNoop(),
		nil,
		orchestrator.OrchestratorTypeWorkflow,
		"",
		0,
		"",
		nil,
		nil,
		false,
		&orchestrator.LLMConfig{},
		1,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewBaseOrchestrator returned error: %v", err)
	}

	hcpo := &StepBasedWorkflowOrchestrator{
		BaseOrchestrator: base,
		tierResolver: NewTierResolver(&TieredLLMConfig{
			Tier1: &AgentLLMConfig{Provider: "openai", ModelID: "tier-1"},
			Tier2: &AgentLLMConfig{Provider: "openai", ModelID: "tier-2"},
			Tier3: &AgentLLMConfig{Provider: "openai", ModelID: "tier-3"},
		}, nil),
	}

	llm := hcpo.selectExecutionLLM(context.Background(), &AgentConfigs{}, "step-1")
	if llm == nil {
		t.Fatal("expected execution llm config, got nil")
	}
	if llm.Primary.ModelID != "tier-1" {
		t.Fatalf("expected tier-1 model for no learnings path, got %q", llm.Primary.ModelID)
	}
}

func TestSelectExecutionLLM_UsesFixedExecutionTier(t *testing.T) {
	base, err := orchestrator.NewBaseOrchestrator(
		loggerv2.NewNoop(),
		nil,
		orchestrator.OrchestratorTypeWorkflow,
		"",
		0,
		"",
		nil,
		nil,
		false,
		&orchestrator.LLMConfig{},
		1,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewBaseOrchestrator returned error: %v", err)
	}

	hcpo := &StepBasedWorkflowOrchestrator{
		BaseOrchestrator: base,
		tierResolver: NewTierResolver(&TieredLLMConfig{
			Tier1: &AgentLLMConfig{Provider: "openai", ModelID: "tier-1"},
			Tier2: &AgentLLMConfig{Provider: "openai", ModelID: "tier-2"},
			Tier3: &AgentLLMConfig{Provider: "openai", ModelID: "tier-3"},
		}, nil),
	}

	llm := hcpo.selectExecutionLLM(context.Background(), &AgentConfigs{ExecutionTier: "medium"}, "step-1")
	if llm == nil {
		t.Fatal("expected execution llm config, got nil")
	}
	if llm.Primary.ModelID != "tier-2" {
		t.Fatalf("expected tier-2 model for fixed medium execution_tier, got %q", llm.Primary.ModelID)
	}
}

func TestSelectExecutionLLM_WorkshopTierOverrideBeatsFixedExecutionTier(t *testing.T) {
	base, err := orchestrator.NewBaseOrchestrator(
		loggerv2.NewNoop(),
		nil,
		orchestrator.OrchestratorTypeWorkflow,
		"",
		0,
		"",
		nil,
		nil,
		false,
		&orchestrator.LLMConfig{},
		1,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewBaseOrchestrator returned error: %v", err)
	}

	hcpo := &StepBasedWorkflowOrchestrator{
		BaseOrchestrator: base,
		tierResolver: NewTierResolver(&TieredLLMConfig{
			Tier1: &AgentLLMConfig{Provider: "openai", ModelID: "tier-1"},
			Tier2: &AgentLLMConfig{Provider: "openai", ModelID: "tier-2"},
			Tier3: &AgentLLMConfig{Provider: "openai", ModelID: "tier-3"},
		}, nil),
	}

	ctx := context.WithValue(context.Background(), workshopTierContextKey{}, int(TierLow))
	llm := hcpo.selectExecutionLLM(ctx, &AgentConfigs{ExecutionTier: "medium"}, "step-1")
	if llm == nil {
		t.Fatal("expected execution llm config, got nil")
	}
	if llm.Primary.ModelID != "tier-3" {
		t.Fatalf("expected workshop override to win with tier-3 model, got %q", llm.Primary.ModelID)
	}
}
