package server

import "testing"

// PLAT-307 regression: a manual workflow run (agent_mode=="workflow") must
// not receive the MCP server management toolset, matching a scheduled/cron
// run (agent_mode=="workflow_phase") and the workflow builder chat itself --
// only interactive, non-workflow-scoped chat should be able to search
// external registries, install new MCP servers, or delete existing ones.
func TestMCPServerToolsEligibleExcludesManualWorkflowRun(t *testing.T) {
	cases := []struct {
		name             string
		isToolBackedChat bool
		agentMode        string
		want             bool
	}{
		{"regular multi-agent chat", true, "multi-agent", true},
		{"empty agent mode defaults to chat", true, "", true},
		{"legacy simple mode", true, "simple", true},
		{"manual workflow run is excluded", true, "workflow", false},
		{"manual workflow run with whitespace is excluded", true, "  workflow  ", false},
		{"workflow builder chat is excluded upstream, but stays excluded here too", false, "workflow_phase", false},
		{"not tool-backed at all", false, "multi-agent", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mcpServerToolsEligible(tc.isToolBackedChat, tc.agentMode); got != tc.want {
				t.Errorf("mcpServerToolsEligible(%v, %q) = %v, want %v", tc.isToolBackedChat, tc.agentMode, got, tc.want)
			}
		})
	}
}
