package step_based_workflow

import (
	"fmt"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
)

func agentConfigUseCodeExecutionMode(cfg *agents.OrchestratorAgentConfig) bool {
	if cfg == nil {
		return false
	}
	return cfg.UseCodeExecutionMode
}

func agentConfigProvider(cfg *agents.OrchestratorAgentConfig) string {
	if cfg == nil {
		return ""
	}
	return cfg.LLMConfig.Primary.Provider
}

func agentConfigModelLabel(cfg *agents.OrchestratorAgentConfig) string {
	if cfg == nil {
		return ""
	}
	if cfg.LLMConfig.Primary.ModelID == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s", cfg.LLMConfig.Primary.Provider, cfg.LLMConfig.Primary.ModelID)
}
