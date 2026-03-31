package step_based_workflow

import (
	"testing"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
)

func TestApplyStepConfigToAgentConfigPreservesLogicalToolSearchForCLIProviders(t *testing.T) {
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
		false,
		nil,
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

	trueVal := true
	stepConfig := &AgentConfigs{
		UseToolSearchMode: &trueVal,
	}

	hcpo.applyStepConfigToAgentConfig(config, stepConfig, false)

	if !config.UseCodeExecutionMode {
		t.Fatalf("expected CLI providers to keep code execution transport enabled")
	}
	if config.UseToolSearchMode {
		t.Fatalf("expected live tool search mode to stay disabled for CLI provider bridge transport")
	}
	if !config.LogicalUseToolSearchMode {
		t.Fatalf("expected logical tool search mode to be preserved for prompt and metadata semantics")
	}
	if !getEffectiveToolSearchMode(config) {
		t.Fatalf("expected effective tool search mode helper to honor logical tool search mode")
	}
}
