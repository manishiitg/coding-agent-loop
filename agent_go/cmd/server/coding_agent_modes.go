package server

import (
	"path/filepath"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/fsutil"

	"github.com/manishiitg/mcpagent/llm"
)

func codingAgentPersistentInteractiveFlags(provider string, allowPersistentInteractive, usesStructuredTransport bool) (claudeCode bool, codexCLI bool, cursorCLI bool, piCLI bool, museCLI bool) {
	normalizedProvider := strings.ToLower(strings.TrimSpace(provider))
	if usesStructuredTransport {
		return false, false, false, false, false
	}
	if !allowPersistentInteractive ||
		!llm.IsTmuxCodingAgentProvider(llm.Provider(normalizedProvider), "") {
		return false, false, false, false, false
	}

	switch normalizedProvider {
	case strings.ToLower(string(llm.ProviderClaudeCode)):
		return true, false, false, false, false
	case strings.ToLower(string(llm.ProviderCodexCLI)):
		return false, true, false, false, false
	case strings.ToLower(string(llm.ProviderCursorCLI)):
		return false, false, true, false, false
	case strings.ToLower(string(llm.ProviderPiCLI)):
		return false, false, false, true, false
	case strings.ToLower(string(llm.ProviderMuseCLI)):
		return false, false, false, false, true
	default:
		return false, false, false, false, false
	}
}

// codingAgentUsesStructuredTransportForChat resolves the transport for a
// user-facing chat. Cursor and Muse normally prefer their structured JSON
// protocols, but the interactive Workflow Builder deliberately uses retained
// tmux so a user can inspect the terminal and steer the active turn.
// Workflow execution does not use this helper: steps and background execution
// remain force-structured in applyWorkflowTransportToAgentConfig.
func codingAgentUsesStructuredTransportForChat(provider, policy string, isInteractiveWorkflowBuilder bool) bool {
	if isInteractiveWorkflowBuilder {
		trimmed := strings.TrimSpace(provider)
		if strings.EqualFold(trimmed, string(llm.ProviderCursorCLI)) ||
			strings.EqualFold(trimmed, string(llm.ProviderMuseCLI)) {
			return false
		}
	}
	return codingAgentUsesStructuredTransportForPolicy(provider, policy)
}

// codingAgentUsesStructuredTransport is the product-level default for coding
// providers whose native CLI JSON protocol is more reliable than terminal UI
// automation. Cursor conversations retain continuity with the provider's
// native --resume session ID; they do not use a persistent tmux pane.
//
// Muse keeps the structured default outside the interactive builder: its
// exec --json lane is the reliable one-shot path for workflows and
// background execution. Interactive builder chats opt into the tmux lane via
// codingAgentUsesStructuredTransportForChat above, where the adapter's
// persistent pool owns a live pane the user can inspect and steer.
func codingAgentUsesStructuredTransport(provider string) bool {
	normalized := strings.ToLower(strings.TrimSpace(provider))
	return normalized == strings.ToLower(string(llm.ProviderCursorCLI)) ||
		normalized == strings.ToLower(string(llm.ProviderMuseCLI))
}

// codingAgentUsesStructuredTransportForPolicy resolves the product/profile
// transport setting before the legacy provider default. A profile's explicit
// policy is authoritative; "auto" retains the shared application default.
func codingAgentUsesStructuredTransportForPolicy(provider, policy string) bool {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "structured":
		return true
	case "tmux":
		return false
	default:
		return codingAgentUsesStructuredTransport(provider)
	}
}

func codingAgentRequestAllowsPersistentInteractive(req *QueryRequest, sessionID string) bool {
	if req == nil {
		return false
	}
	// Backend-created children and typed runtime stages have durable outputs and
	// completion notifications, not a user who can continue their native CLI.
	if strings.TrimSpace(req.ParentSessionID) != "" || strings.TrimSpace(req.SessionKind) != "" || req.IsAutoNotification {
		return false
	}
	// A scheduler knows that its immediately following turn belongs to the same
	// conversation. Keep the CLI process alive across that boundary rather than
	// sending Claude Code /exit and spawning a separate --resume process.
	if req.KeepNativeSessionAlive {
		return true
	}
	// "Make interactive" deliberately keeps the schedule session ID. This
	// explicit promotion therefore outranks its historical trigger/ID shape.
	if req.UserInteractiveContinuation {
		return true
	}
	// Workflow Builder chats are represented internally as workflow_phase, but
	// they are still ordinary user-interactive main chats. Classify by origin
	// and ownership instead of agent mode so their conversation tmux survives.
	return !isScheduledSessionIdentity(sessionID, req.TriggeredBy)
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
