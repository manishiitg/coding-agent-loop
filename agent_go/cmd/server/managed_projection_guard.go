package server

import (
	"path/filepath"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
)

// managedCodingAgentProjectionWritePaths are owned by the active coding-agent
// adapter. They carry the assembled system prompt, selected skills, MCP
// configuration and provider runtime metadata. Agents may read them, but must
// not mutate them as ordinary project files.
var managedCodingAgentProjectionWritePaths = []string{
	"AGENTS.md",
	"CLAUDE.md",
	"GEMINI.md",
	".agents",
	".claude",
	".codex",
	".cursor",
	".gemini",
	".pi",
}

func protectManagedCodingAgentProjectionWrites(sessionID, workspaceRoot string) {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(workspaceRoot) == "" {
		return
	}
	blocked := make([]string, 0, len(managedCodingAgentProjectionWritePaths)*2)
	if current := common.GetSessionShellConfig(sessionID); current != nil {
		blocked = append(blocked, current.BlockedWritePaths...)
	}
	for _, relative := range managedCodingAgentProjectionWritePaths {
		// Workspace tools use workspace-relative paths, while the native shell
		// commonly addresses the same artifact relative to its working dir.
		// Carry both spellings through the shared guard.
		blocked = append(blocked, filepath.Join(workspaceRoot, relative), relative)
	}
	common.SetSessionFolderGuardBlockedWritePaths(sessionID, common.DeduplicateStrings(blocked))
}
