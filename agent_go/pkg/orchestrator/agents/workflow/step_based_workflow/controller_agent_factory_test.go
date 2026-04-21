package step_based_workflow

import (
	"context"
	"testing"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
)

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
