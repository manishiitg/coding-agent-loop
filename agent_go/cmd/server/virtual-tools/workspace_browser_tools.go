package virtualtools

import (
	"context"
	"log"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/browser"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// GetWorkspaceBrowserToolCategory returns the category name for workspace browser tools
func GetWorkspaceBrowserToolCategory() string {
	return "workspace_browser"
}

// CreateWorkspaceBrowserTools creates the single agent_browser virtual tool
func CreateWorkspaceBrowserTools() []llmtypes.Tool {
	return []llmtypes.Tool{browser.GetToolDefinition()}
}

// CreateWorkspaceBrowserToolExecutors creates the execution functions for workspace browser tools.
// Optional CDP ports authorize one or more independently-profiled Chrome browsers.
func CreateWorkspaceBrowserToolExecutors(cdpPort ...int) map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	return CreateWorkspaceBrowserToolExecutorsWithSession("", cdpPort...)
}

// CreateWorkspaceBrowserToolExecutorsWithSession creates browser tool executors with chat session tracking.
// sessionID is the chat/workflow session ID — used to enforce per-session browser limits.
// Multiple ports are an explicit opt-in for separate login identities within
// one run; normal workflow concurrency should continue sharing one CDP port.
func CreateWorkspaceBrowserToolExecutorsWithSession(sessionID string, cdpPort ...int) map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	executors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	// Wire up the browser executor from the pkg/browser package
	browserClient := browser.NewClient(getWorkspaceAPIURL())
	var opts []browser.ExecutorOption
	if len(cdpPort) > 0 {
		opts = append(opts, browser.WithCdpPorts(cdpPort...))
	}
	browserExecutor := browser.NewExecutor(browserClient, opts...)

	// Wrap executor to inject session IDs into context.
	// - ChatSessionIDKey = agent-level ID (isolated for share_browser=false, parent otherwise)
	// - WorkflowSessionIDKey = always the parent workflow session ID
	executors["agent_browser"] = func(ctx context.Context, args map[string]interface{}) (string, error) {
		// If the context already has an isolated session ID (set by share_browser=false),
		// use it as the agent-level session. Otherwise use the parent sessionID.
		if isolatedID, ok := ctx.Value(SubAgentIsolatedSessionIDKey).(string); ok && isolatedID != "" {
			ctx = context.WithValue(ctx, common.ChatSessionIDKey, isolatedID)
			log.Printf("[BROWSER_TOOLS] Using isolated agent session: %s (parent workflow: %s)", isolatedID, sessionID)
		} else if existingID, ok := ctx.Value(common.ChatSessionIDKey).(string); ok && existingID != "" {
			// Preserve the session injected by /s/{session_id}/tools/... routes.
			// For share_browser=false code-exec sub-agents this is the isolated
			// sub-agent session; overwriting it with the parent would collapse
			// browser isolation.
			log.Printf("[BROWSER_TOOLS] Preserving context agent session: %s (parent workflow: %s)", existingID, sessionID)
		} else if sessionID != "" {
			ctx = context.WithValue(ctx, common.ChatSessionIDKey, sessionID)
		}
		// Always set the workflow-level session to the parent (for per-workflow limits)
		if sessionID != "" {
			ctx = context.WithValue(ctx, common.WorkflowSessionIDKey, sessionID)
		}
		return browserExecutor.HandleAgentBrowser(ctx, args)
	}

	return executors
}
