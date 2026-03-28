package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/browser"
	"mcp-agent-builder-go/agent_go/pkg/common"

	"github.com/manishiitg/mcpagent/events"
)

// workspaceEventEmitterKey matches the key used in virtualtools package
const workspaceEventEmitterKey common.ContextKey = "workspace_event_emitter"

// emitWorkspaceFileEvent emits a workspace_file_operation event if an emitter is present in context
func emitWorkspaceFileEvent(ctx context.Context, operation, filepath, folder string) {
	emitter, ok := ctx.Value(workspaceEventEmitterKey).(interface {
		HandleEvent(ctx context.Context, event *events.AgentEvent) error
	})
	if !ok || emitter == nil {
		return
	}

	turn, _ := ctx.Value(common.ContextKey("turn")).(int)
	serverName, _ := ctx.Value(common.ContextKey("server_name")).(string)

	eventData := events.NewWorkspaceFileOperationEvent(operation, filepath, folder, turn, serverName)
	agentEvent := &events.AgentEvent{
		Type:      events.WorkspaceFileOperation,
		Timestamp: time.Now(),
		Data:      eventData,
	}
	_ = emitter.HandleEvent(ctx, agentEvent)
}

// Helper to convert generic map args to typed struct
func mapToStruct(args map[string]interface{}, v interface{}) error {
	data, err := json.Marshal(args)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// NewBasicExecutor creates executors for basic workspace file tools
func NewBasicExecutor(client *Client) map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	executors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	executors["list_workspace_files"] = func(ctx context.Context, args map[string]interface{}) (string, error) {
		var params ListWorkspaceFilesParams
		if err := mapToStruct(args, &params); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		result, err := client.ListWorkspaceFiles(ctx, params)
		if err == nil {
			emitWorkspaceFileEvent(ctx, "list", "", params.Folder)
		}
		return result, err
	}

	executors["read_workspace_file"] = func(ctx context.Context, args map[string]interface{}) (string, error) {
		var params ReadWorkspaceFileParams
		if err := mapToStruct(args, &params); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		result, err := client.ReadWorkspaceFile(ctx, params)
		if err == nil {
			emitWorkspaceFileEvent(ctx, "read", params.Filepath, "")
		}
		return result, err
	}

	executors["update_workspace_file"] = func(ctx context.Context, args map[string]interface{}) (string, error) {
		var params UpdateWorkspaceFileParams
		if err := mapToStruct(args, &params); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		result, err := client.UpdateWorkspaceFile(ctx, params)
		if err == nil {
			emitWorkspaceFileEvent(ctx, "update", params.Filepath, "")
		}
		return result, err
	}

	executors["delete_workspace_file"] = func(ctx context.Context, args map[string]interface{}) (string, error) {
		var params DeleteWorkspaceFileParams
		if err := mapToStruct(args, &params); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		result, err := client.DeleteWorkspaceFile(ctx, params)
		if err == nil {
			emitWorkspaceFileEvent(ctx, "delete", params.Filepath, "")
		}
		return result, err
	}

	executors["move_workspace_file"] = func(ctx context.Context, args map[string]interface{}) (string, error) {
		var params MoveWorkspaceFileParams
		if err := mapToStruct(args, &params); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		result, err := client.MoveWorkspaceFile(ctx, params)
		if err == nil {
			emitWorkspaceFileEvent(ctx, "move", params.DestinationFilepath, "")
		}
		return result, err
	}

	return executors
}

// IsSemanticSearchEnabled checks if semantic search is enabled
func IsSemanticSearchEnabled() bool {
	return os.Getenv("WORKSPACE_ENABLE_SEMANTIC_SEARCH") == "true" || os.Getenv("ENABLE_SEMANTIC_SEARCH") == "true"
}

// IsGitSyncEnabled checks if GitHub sync is enabled
func IsGitSyncEnabled() bool {
	// Enabled if WORKSPACE_ENABLE_GITHUB_SYNC is true OR if GITHUB_TOKEN is present and not explicitly disabled
	enabled := os.Getenv("WORKSPACE_ENABLE_GITHUB_SYNC")
	if enabled == "false" {
		return false
	}
	return enabled == "true" || os.Getenv("GITHUB_TOKEN") != ""
}

// NewAdvancedExecutor creates executors for advanced workspace tools (shell, image, pdf, diff patch)
func NewAdvancedExecutor(client *Client) map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	executors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	executors["execute_shell_command"] = func(ctx context.Context, args map[string]interface{}) (string, error) {
		var params ExecuteShellCommandParams
		if err := mapToStruct(args, &params); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		return client.ExecuteShellCommand(ctx, params)
	}

	executors["read_image"] = func(ctx context.Context, args map[string]interface{}) (string, error) {
		var params ReadImageParams
		if err := mapToStruct(args, &params); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		return client.ReadImage(ctx, params)
	}

	executors["read_pdf"] = func(ctx context.Context, args map[string]interface{}) (string, error) {
		var params ReadPDFParams
		if err := mapToStruct(args, &params); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		return client.ReadPDF(ctx, params)
	}

	executors["diff_patch_workspace_file"] = func(ctx context.Context, args map[string]interface{}) (string, error) {
		var params DiffPatchWorkspaceFileParams
		if err := mapToStruct(args, &params); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		result, err := client.DiffPatchWorkspaceFile(ctx, params)
		if err == nil {
			emitWorkspaceFileEvent(ctx, "patch", params.Filepath, "")
		}
		return result, err
	}

	return executors
}

// NewBrowserExecutor creates executors for browser tools
func NewBrowserExecutor(client *Client) map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	executors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	browserClient := browser.NewClient(client.BaseURL)
	browserExecutor := browser.NewExecutor(browserClient)
	executors["agent_browser"] = browserExecutor.HandleAgentBrowser

	return executors
}

