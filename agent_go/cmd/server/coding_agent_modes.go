package server

import (
	"path/filepath"
	"strings"

	"mcp-agent-builder-go/agent_go/pkg/fsutil"

	"github.com/manishiitg/mcpagent/llm"
)

func codingAgentPersistentInteractiveFlags(provider string) (claudeCode bool, codexCLI bool, geminiCLI bool, cursorCLI bool, agyCLI bool, piCLI bool) {
	normalizedProvider := strings.ToLower(strings.TrimSpace(provider))
	if !llm.IsTmuxCodingAgentProvider(llm.Provider(normalizedProvider), "") {
		return false, false, false, false, false, false
	}

	switch normalizedProvider {
	case strings.ToLower(string(llm.ProviderClaudeCode)):
		return true, false, false, false, false, false
	case strings.ToLower(string(llm.ProviderCodexCLI)):
		return false, true, false, false, false, false
	case strings.ToLower(string(llm.ProviderGeminiCLI)):
		return false, false, true, false, false, false
	case strings.ToLower(string(llm.ProviderCursorCLI)):
		return false, false, false, true, false, false
	case strings.ToLower(string(llm.ProviderAgyCLI)):
		return false, false, false, false, true, false
	case strings.ToLower(string(llm.ProviderPiCLI)):
		return false, false, false, false, false, true
	default:
		return false, false, false, false, false, false
	}
}

func codingAgentClaudeCodeChatTransport(provider string) string {
	if strings.ToLower(strings.TrimSpace(provider)) == strings.ToLower(string(llm.ProviderClaudeCode)) {
		return llm.ClaudeCodeTransportTmux
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
