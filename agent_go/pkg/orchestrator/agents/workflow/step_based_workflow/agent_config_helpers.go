package step_based_workflow

import "mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"

func getEffectiveToolSearchMode(cfg *agents.OrchestratorAgentConfig) bool {
	if cfg == nil {
		return false
	}
	if cfg.LogicalUseToolSearchMode {
		return true
	}
	return cfg.UseToolSearchMode
}
