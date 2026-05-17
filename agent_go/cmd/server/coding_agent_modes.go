package server

import (
	"path/filepath"
	"strings"

	"mcp-agent-builder-go/agent_go/pkg/fsutil"

	"github.com/manishiitg/mcpagent/llm"
)

func codingAgentPersistentInteractiveFlags(provider string) (claudeCode bool, codexCLI bool, geminiCLI bool) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case strings.ToLower(string(llm.ProviderClaudeCode)):
		return true, false, false
	case strings.ToLower(string(llm.ProviderCodexCLI)):
		return false, true, false
	case strings.ToLower(string(llm.ProviderGeminiCLI)):
		return false, false, true
	default:
		return false, false, false
	}
}

func codingAgentClaudeCodeChatTransport(provider string) string {
	if strings.ToLower(strings.TrimSpace(provider)) == strings.ToLower(string(llm.ProviderClaudeCode)) {
		return llm.ClaudeCodeTransportExperimental
	}
	return ""
}

func codingAgentWorkspaceWorkingDir(workspaceRelativeFolder string) string {
	rel := strings.TrimSpace(workspaceRelativeFolder)
	if rel == "" {
		rel = perUserChatsFolderFor("")
	}
	return filepath.Join(fsutil.WorkspaceDocsRoot(), filepath.FromSlash(rel))
}
