package virtualtools

import (
	"context"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// GetWorkspaceBasicToolCategory returns the category name for workspace basic tools
func GetWorkspaceBasicToolCategory() string {
	return "workspace_basic"
}

// CreateWorkspaceBasicTools creates workspace basic virtual tools (9 tools)
// These are the core file/folder management and search tools (git tools moved to workspace_git)
func CreateWorkspaceBasicTools() []llmtypes.Tool {
	var tools []llmtypes.Tool

	// Add list_workspace_files tool
	listFilesTool := llmtypes.Tool{
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
	}
	tools = append(tools, listFilesTool)

	// Add read_workspace_file tool
	readFileTool := llmtypes.Tool{
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
	}
	tools = append(tools, readFileTool)

	// Add update_workspace_file tool
	updateFileTool := llmtypes.Tool{
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
	}
	tools = append(tools, updateFileTool)

	// Add diff_patch_workspace_file tool (unified diff patching)
	diffPatchFileTool := llmtypes.Tool{
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
						"description": "🚨 CRITICAL REQUIREMENTS - Unified diff format string to apply:\n\n**MANDATORY FORMAT (like 'diff -U0'):**\n- Headers: --- a/file.md\\n+++ b/file.md\n- Hunk headers: @@ -startLine,lineCount +startLine,lineCount @@\n- Context lines: ' ' prefix (SPACE + content - MUST match file exactly)\n- Removals: '-' prefix (MINUS + content)\n- Additions: '+' prefix (PLUS + content)\n- MUST end with newline character\n\n🚨 CRITICAL: Context lines start with SPACE ( ), NOT minus (-)!\n   Correct: ' # Header' (space + content)\n   Wrong:   '- # Header' (minus + content)\n\n**PERFECT EXAMPLE:**\n--- a/todo.md\n+++ b/todo.md\n@@ -1,3 +1,4 @@\n # Todo List\n+**New addition**: Added via unified diff\n \n ## Objective\n@@ -4,3 +5,4 @@\n ## Notes\n - Leverages tavily-search for comprehensive research\n+- Added new methodology note\n\n**🚨 CRITICAL VALIDATION CHECKLIST:**\n- ✅ File exists and was read with read_workspace_file\n- ✅ Context lines copied EXACTLY from file content (including whitespace)\n- ✅ Hunk headers show correct line numbers\n- ✅ Diff ends with newline character\n- ✅ Proper unified diff format (---/+++ headers)\n- ✅ No truncated or malformed lines\n- ✅ Test with simple single-line addition first",
					},
					"commit_message": map[string]interface{}{
						"type":        "string",
						"description": "Optional commit message for version control",
					},
				},
				"required": []string{"filepath", "diff"},
			}),
		},
	}
	tools = append(tools, diffPatchFileTool)

	// Add regex_search_workspace_files tool
	regexSearchTool := llmtypes.Tool{
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
	}
	tools = append(tools, regexSearchTool)

	// Add semantic_search_workspace_files tool only if enabled via environment variable
	if isSemanticSearchEnabled() {
		semanticSearchTool := llmtypes.Tool{
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
		}
		tools = append(tools, semanticSearchTool)
	}

	// Add glob_discover_workspace_files tool
	globDiscoverTool := llmtypes.Tool{
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
	}
	tools = append(tools, globDiscoverTool)

	// Add delete_workspace_file tool
	deleteFileTool := llmtypes.Tool{
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
	}
	tools = append(tools, deleteFileTool)

	// Add move_workspace_file tool
	moveFileTool := llmtypes.Tool{
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
	}
	tools = append(tools, moveFileTool)

	return tools
}

// CreateWorkspaceBasicToolExecutors creates the execution functions for workspace basic tools
func CreateWorkspaceBasicToolExecutors() map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	executors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	executors["list_workspace_files"] = handleListWorkspaceFiles
	executors["read_workspace_file"] = handleReadWorkspaceFile
	executors["update_workspace_file"] = handleUpdateWorkspaceFile
	executors["diff_patch_workspace_file"] = handleDiffPatchWorkspaceFile
	executors["regex_search_workspace_files"] = handleRegexSearchWorkspaceFiles
	// Only register semantic search executor if enabled
	if isSemanticSearchEnabled() {
		executors["semantic_search_workspace_files"] = handleSemanticSearchWorkspaceFiles
	}
	executors["glob_discover_workspace_files"] = handleGlobDiscoverWorkspaceFiles
	executors["delete_workspace_file"] = handleDeleteWorkspaceFile
	executors["move_workspace_file"] = handleMoveWorkspaceFile

	return executors
}
