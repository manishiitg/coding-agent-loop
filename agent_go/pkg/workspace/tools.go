package workspace

import (
	"context"
	"fmt"
	"os"
	"encoding/json"

	"mcp-agent-builder-go/agent_go/pkg/browser"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

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
					"commit_message": map[string]interface{}{
						"type":        "string",
						"description": "Optional commit message for version control",
					},
				},
				"required": []string{"filepath", "content"},
			}),
		},
	})

	// Add diff_patch_workspace_file tool (unified diff patching)
	tools = append(tools, llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "diff_patch_workspace_file",
			Description: "🚨 CRITICAL WORKFLOW: 1) MANDATORY - Use read_workspace_file first to see exact current content 2) Generate diff using 'diff -U0' format with perfect context matching 3) Apply patch. This tool requires precise unified diff format - context lines must match file exactly. Use for targeted, surgical changes to specific file sections. ⚠️ FAILURE TO FOLLOW WORKFLOW WILL RESULT IN PATCH FAILURES.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filepath": map[string]interface{}{
						"type":        "string",
						"description": "Full file path of the file to patch (e.g., 'docs/guide.md', 'configs/settings.json')",
					},
					"diff": map[string]interface{}{
						"type":        "string",
						"description": "🚨 CRITICAL REQUIREMENTS - Unified diff format string to apply:\n\n**MANDATORY FORMAT (like 'diff -U0'):**\n- Headers: --- a/file.md\n+++ b/file.md\n- Hunk headers: @@ -startLine,lineCount +startLine,lineCount @@\n- Context lines: ' ' prefix (SPACE + content - MUST match file exactly)\n- Removals: '-' prefix (MINUS + content)\n- Additions: '+' prefix (PLUS + content)\n- MUST end with newline character\n\n🚨 CRITICAL: Context lines start with SPACE ( ), NOT minus (-) !\n   Correct: ' # Header' (space + content)\n   Wrong:   '- # Header' (minus + content)\n\n**PERFECT EXAMPLE:**\n--- a/todo.md\n+++ b/todo.md\n@@ -1,3 +1,4 @@\n # Todo List\n+**New addition**: Added via unified diff\n \n ## Objective\n@@ -4,3 +5,4 @@\n ## Notes\n - Leverages tavily-search for comprehensive research\n+- Added new methodology note\n\n**🚨 CRITICAL VALIDATION CHECKLIST:**\n- ✅ File exists and was read with read_workspace_file\n- ✅ Context lines copied EXACTLY from file content (including whitespace)\n- ✅ Hunk headers show correct line numbers\n- ✅ Diff ends with newline character\n- ✅ Proper unified diff format (---/+++ headers)\n- ✅ No truncated or malformed lines\n- ✅ Test with simple single-line addition first",
					},
					"commit_message": map[string]interface{}{
						"type":        "string",
						"description": "Optional commit message for version control",
					},
				},
				"required": []string{"filepath", "diff"},
			}),
		},
	})

	// Add regex_search_workspace_files tool
	tools = append(tools, llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "regex_search_workspace_files",
			Description: "Search files in the workspace using regex patterns across full content. Searches text-based files within the specified folder only. Requires 'folder' parameter.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Regex search query to find in files (e.g., 'docker', 'test.*file', \\d{4}-\\d{2}-\\d{2}', '(error|exception)', 'markdown')",
					},
					"folder": map[string]interface{}{
						"type":        "string",
						"description": "Folder path to search within (e.g., 'docs', 'src', 'configs'). Required.",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results to return (default: 20, max: 100)",
					},
				},
				"required": []string{"query", "folder"},
			}),
		},
	})

	// Add semantic_search_workspace_files tool only if enabled
	if IsSemanticSearchEnabled() {
		tools = append(tools, llmtypes.Tool{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "semantic_search_workspace_files",
				Description: "Search files using AI-powered semantic similarity. Finds content by meaning, not just exact text matches. Uses embeddings to understand context and relationships between concepts. For exact text matches, use search_workspace_files tool instead.",
				Parameters: llmtypes.NewParameters(map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query": map[string]interface{}{
							"type":        "string",
							"description": "Natural language search query (e.g., 'docker configuration', 'error handling', 'API endpoints', 'authentication setup', 'database connection')",
						},
						"folder": map[string]interface{}{
							"type":        "string",
							"description": "Folder path to search within (e.g., 'docs', 'src', 'configs'). Required parameter for semantic search.",
						},
						"limit": map[string]interface{}{
							"type":        "integer",
							"description": "Maximum number of semantic results to return (default: 10, max: 50)",
						},
					},
					"required": []string{"query", "folder"},
				}),
			},
		})
	}

	// Add glob_discover_workspace_files tool
	tools = append(tools, llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "glob_discover_workspace_files",
			Description: "Discover files in the workspace using glob patterns. Supports standard glob syntax: * (matches any characters), ? (matches single character), [chars] (matches character set), and ** (matches zero or more directories recursively). Examples: '*.go' finds all Go files, '**/*.md' finds all Markdown files recursively, 'docs/**/*.txt' finds all text files in docs and subdirectories.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pattern": map[string]interface{}{
						"type":        "string",
						"description": "Glob pattern to match files (e.g., '*.go', '**/*.md', 'docs/**/*.txt', 'test_*.py'). Supports * for any characters, ? for single character, [chars] for character set, and ** for recursive directory matching.",
					},
					"folder": map[string]interface{}{
						"type":        "string",
						"description": "Folder path to search within (e.g., 'docs', 'src', 'configs'). If not specified, searches from workspace root.",
					},
					"max_depth": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum depth of directories to search recursively (default: unlimited, -1 for unlimited)",
					},
					"include_dirs": map[string]interface{}{
						"type":        "boolean",
						"description": "Include directories in results (default: false, only files are returned)",
					},
				},
				"required": []string{"pattern"},
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
					"commit_message": map[string]interface{}{
						"type":        "string",
						"description": "Optional commit message for version control",
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
					"commit_message": map[string]interface{}{
						"type":        "string",
						"description": "Optional commit message for version control",
					},
				},
				"required": []string{"source_filepath", "destination_filepath"},
			}),
		},
	})

	return tools
}

// GetAllToolDefinitions aggregates definitions from all categories
func GetAllToolDefinitions() []llmtypes.Tool {
	var tools []llmtypes.Tool
	tools = append(tools, GetBasicToolDefinitions()...)
	tools = append(tools, GetAdvancedToolDefinitions()...)
	tools = append(tools, GetGitToolDefinitions()...)
	tools = append(tools, browser.GetToolDefinition())
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
		return client.ListWorkspaceFiles(ctx, params)
	}

	executors["read_workspace_file"] = func(ctx context.Context, args map[string]interface{}) (string, error) {
		var params ReadWorkspaceFileParams
		if err := mapToStruct(args, &params); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		return client.ReadWorkspaceFile(ctx, params)
	}

	executors["update_workspace_file"] = func(ctx context.Context, args map[string]interface{}) (string, error) {
		var params UpdateWorkspaceFileParams
		if err := mapToStruct(args, &params); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		return client.UpdateWorkspaceFile(ctx, params)
	}

	executors["diff_patch_workspace_file"] = func(ctx context.Context, args map[string]interface{}) (string, error) {
		var params DiffPatchWorkspaceFileParams
		if err := mapToStruct(args, &params); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		return client.DiffPatchWorkspaceFile(ctx, params)
	}

	executors["regex_search_workspace_files"] = func(ctx context.Context, args map[string]interface{}) (string, error) {
		var params RegexSearchWorkspaceFilesParams
		if err := mapToStruct(args, &params); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		return client.RegexSearchWorkspaceFiles(ctx, params)
	}

	// Only register semantic search executor if enabled
	if IsSemanticSearchEnabled() {
		executors["semantic_search_workspace_files"] = func(ctx context.Context, args map[string]interface{}) (string, error) {
			var params SemanticSearchWorkspaceFilesParams
			if err := mapToStruct(args, &params); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			return client.SemanticSearchWorkspaceFiles(ctx, params)
		}
	}

	executors["glob_discover_workspace_files"] = func(ctx context.Context, args map[string]interface{}) (string, error) {
		var params GlobDiscoverWorkspaceFilesParams
		if err := mapToStruct(args, &params); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		return client.GlobDiscoverWorkspaceFiles(ctx, params)
	}

	executors["delete_workspace_file"] = func(ctx context.Context, args map[string]interface{}) (string, error) {
		var params DeleteWorkspaceFileParams
		if err := mapToStruct(args, &params); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		return client.DeleteWorkspaceFile(ctx, params)
	}

	executors["move_workspace_file"] = func(ctx context.Context, args map[string]interface{}) (string, error) {
		var params MoveWorkspaceFileParams
		if err := mapToStruct(args, &params); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		return client.MoveWorkspaceFile(ctx, params)
	}

	return executors
}

// IsSemanticSearchEnabled checks if semantic search is enabled
func IsSemanticSearchEnabled() bool {
	return os.Getenv("ENABLE_SEMANTIC_SEARCH") == "true"
}

// NewAdvancedExecutor creates executors for advanced workspace tools (shell, image, web fetch, pdf)
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

	executors["fetch_web_content"] = func(ctx context.Context, args map[string]interface{}) (string, error) {
		var params FetchWebContentParams
		if err := mapToStruct(args, &params); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		return client.FetchWebContent(ctx, params)
	}

	executors["read_pdf"] = func(ctx context.Context, args map[string]interface{}) (string, error) {
		var params ReadPDFParams
		if err := mapToStruct(args, &params); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		return client.ReadPDF(ctx, params)
	}

	return executors
}

// NewGitExecutor creates executors for git workspace tools
func NewGitExecutor(client *Client) map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	executors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	executors["sync_workspace_to_github"] = func(ctx context.Context, args map[string]interface{}) (string, error) {
		var params SyncWorkspaceToGithubParams
		if err := mapToStruct(args, &params); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		return client.SyncWorkspaceToGithub(ctx, params)
	}

	executors["get_workspace_github_status"] = func(ctx context.Context, args map[string]interface{}) (string, error) {
		var params GetWorkspaceGithubStatusParams
		if err := mapToStruct(args, &params); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		return client.GetWorkspaceGithubStatus(ctx, params)
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

// NewAllExecutors creates executors for ALL workspace tools (basic, advanced, git, browser)
func NewAllExecutors(client *Client) map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	executors := NewBasicExecutor(client)

	// Merge advanced executors
	for k, v := range NewAdvancedExecutor(client) {
		executors[k] = v
	}

	// Merge git executors
	for k, v := range NewGitExecutor(client) {
		executors[k] = v
	}

	// Merge browser executors
	for k, v := range NewBrowserExecutor(client) {
		executors[k] = v
	}

	return executors
}
