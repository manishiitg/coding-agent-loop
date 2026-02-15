package virtualtools

import (
	"context"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"mcp-agent-builder-go/agent_go/pkg/browser"
)

// GetWorkspaceBrowserToolCategory returns the category name for workspace browser tools
func GetWorkspaceBrowserToolCategory() string {
	return "workspace_browser"
}

// CreateWorkspaceBrowserTools creates the single agent_browser virtual tool
func CreateWorkspaceBrowserTools() []llmtypes.Tool {
	return []llmtypes.Tool{browser.GetToolDefinition()}
}

// CreateWorkspaceBrowserToolExecutors creates the execution functions for workspace browser tools
// An optional cdpPort can be passed to connect to an existing Chrome via CDP instead of launching headless.
func CreateWorkspaceBrowserToolExecutors(cdpPort ...int) map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	executors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	// Wire up the browser executor from the pkg/browser package
	browserClient := browser.NewClient(getWorkspaceAPIURL())
	var opts []browser.ExecutorOption
	if len(cdpPort) > 0 && cdpPort[0] > 0 {
		opts = append(opts, browser.WithCdpPort(cdpPort[0]))
	}
	browserExecutor := browser.NewExecutor(browserClient, opts...)
	executors["agent_browser"] = browserExecutor.HandleAgentBrowser

	return executors
}
