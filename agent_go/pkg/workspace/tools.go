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
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
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

// --- Tool Definitions ---

// GetBasicToolDefinitions returns the tool definitions for the basic workspace tools
func GetBasicToolDefinitions() []llmtypes.Tool {
	var tools []llmtypes.Tool

	// Add list_workspace_files tool
	tools = append(tools, llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "list_workspace_files",
			Description: "List all files and folders in the workspace.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"folder": map[string]interface{}{
						"type":        "string",
						"description": "Folder path to filter results (e.g., 'docs', 'examples', 'folder/subfolder')",
					},
					"max_depth": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum depth of hierarchical structure to return (default: 3, max: 10)",
					},
				},
				"required": []string{"folder"},
			}),
		},
	})

	// Add read_workspace_file tool
	tools = append(tools, llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "read_workspace_file",
			Description: "Read the content of a specific file from the workspace by filepath",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filepath": map[string]interface{}{
						"type":        "string",
						"description": "Full file path (e.g., 'docs/example.md', 'configs/settings.json', 'README.md')",
					},
				},
				"required": []string{"filepath"},
			}),
		},
	})

	// Add update_workspace_file tool
	tools = append(tools, llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "update_workspace_file",
			Description: "Create a new file or update/replace the entire content of an existing file in the workspace (upsert behavior). If you are using existing file prefer to use diff_patch_workspace_file instead",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filepath": map[string]interface{}{
						"type":        "string",
						"description": "Full file path of the file to create or update (e.g., 'docs/guide.md', 'configs/settings.json')",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Content to write to the file (will create new file or replace entire existing file)",
					},
				},
				"required": []string{"filepath", "content"},
			}),
		},
	})

	// Add delete_workspace_file tool
	tools = append(tools, llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "delete_workspace_file",
			Description: "Delete a specific file from the workspace permanently. This action cannot be undone. Use with caution.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filepath": map[string]interface{}{
						"type":        "string",
						"description": "Full file path of the file to delete (e.g., 'docs/example.md', 'configs/settings.json')",
					},
				},
				"required": []string{"filepath"},
			}),
		},
	})

	// Add move_workspace_file tool
	tools = append(tools, llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "move_workspace_file",
			Description: "Move a file from one location to another in the workspace. Can be used to move files between folders or rename files.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"source_filepath": map[string]interface{}{
						"type":        "string",
						"description": "Current file path of the file to move (e.g., 'docs/old-file.md', 'configs/settings.json')",
					},
					"destination_filepath": map[string]interface{}{
						"type":        "string",
						"description": "New file path where the file should be moved (e.g., 'archive/old-file.md', 'settings/config.json')",
					},
				},
				"required": []string{"source_filepath", "destination_filepath"},
			}),
		},
	})

	return tools
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

