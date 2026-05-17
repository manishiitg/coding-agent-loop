package orchestrator

import (
	"path/filepath"
	"testing"

	"mcp-agent-builder-go/agent_go/pkg/common"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
)

func TestSyncCodingAgentWorkingDirUsesShellSessionWorkingDir(t *testing.T) {
	docsRoot := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", docsRoot)

	sessionID := "workflow-step-session"
	common.SetSessionWorkingDir(sessionID, "Workflow/testing/runs/iteration-0/execution")
	defer common.ClearSessionShellConfig(sessionID)

	config := agents.NewOrchestratorAgentConfig("step-agent")
	config.MCPSessionID = sessionID
	config.CodingAgentWorkingDir = filepath.Join(docsRoot, "Workflow", "testing")

	syncCodingAgentWorkingDirWithShellSession(config)

	want := filepath.Join(docsRoot, "Workflow", "testing", "runs", "iteration-0", "execution")
	if config.CodingAgentWorkingDir != want {
		t.Fatalf("CodingAgentWorkingDir = %q, want %q", config.CodingAgentWorkingDir, want)
	}
}
