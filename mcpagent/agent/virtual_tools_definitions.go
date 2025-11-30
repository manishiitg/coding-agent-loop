package mcpagent

import "github.com/manishiitg/multi-llm-provider-go/llmtypes"

// CreateWorkspaceTools creates workspace-related virtual tools
func CreateWorkspaceTools() []llmtypes.Tool {
	var workspaceTools []llmtypes.Tool

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
	workspaceTools = append(workspaceTools, listFilesTool)

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
	workspaceTools = append(workspaceTools, readFileTool)

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
	workspaceTools = append(workspaceTools, updateFileTool)

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
	workspaceTools = append(workspaceTools, diffPatchFileTool)

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
	workspaceTools = append(workspaceTools, regexSearchTool)

	// Add semantic_search_workspace_files tool
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
	workspaceTools = append(workspaceTools, semanticSearchTool)

	// Add sync_workspace_to_github tool
	syncGitHubTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "sync_workspace_to_github",
			Description: "Sync all workspace files to GitHub repository using standard git workflow: commit → pull → push. Always pulls first to ensure synchronization. Fails if merge conflicts are detected (requires manual resolution).",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"force": map[string]interface{}{
						"type":        "boolean",
						"description": "Force sync even if there are conflicts (not recommended, default: false)",
					},
					"commit_message": map[string]interface{}{
						"type":        "string",
						"description": "Custom commit message for the sync operation (optional)",
					},
				},
			}),
		},
	}
	workspaceTools = append(workspaceTools, syncGitHubTool)

	// Add get_workspace_github_status tool
	gitHubStatusTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "get_workspace_github_status",
			Description: "Get the current GitHub sync status including pending changes, conflicts, and repository information. Uses git commands to check local repository status and connection to GitHub remote.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"show_pending": map[string]interface{}{
						"type":        "boolean",
						"description": "Show pending changes (default: true)",
					},
					"show_conflicts": map[string]interface{}{
						"type":        "boolean",
						"description": "Show conflicts if any (default: true)",
					},
				},
			}),
		},
	}
	workspaceTools = append(workspaceTools, gitHubStatusTool)

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
	workspaceTools = append(workspaceTools, deleteFileTool)

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
	workspaceTools = append(workspaceTools, moveFileTool)

	// Add execute_shell_command tool
	executeShellTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "execute_shell_command",
			Description: "Execute shell commands and scripts within the workspace directory. Commands run with a 60-second timeout (configurable up to 300 seconds) and are restricted to the workspace boundary (/app/workspace-docs).\n\n**PATH USAGE RULES:**\n- **Tool Parameters**: Use relative paths (e.g., 'working_directory: \"scripts\"' resolves to '/app/workspace-docs/scripts')\n- **Inside Scripts**: When writing Python/shell scripts that reference files, use absolute paths starting with '/app/workspace-docs' (e.g., '/app/workspace-docs/script.py', '/app/workspace-docs/data/file.csv'). This ensures scripts work regardless of the working_directory setting.\n\nReturns stdout, stderr, and exit code. Use 'use_shell: true' for complex commands with pipes (|), redirects (>), chaining (&&, ||), environment variables, or wildcards.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type":        "string",
						"description": "Shell command to execute. If use_shell is true, this can be a complex command with pipes, redirects, etc. (e.g., 'ls', 'grep', 'find', './script.sh', 'ls | grep .md', 'cd dir && ls', 'VAR=value command')",
					},
					"args": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Command arguments as an array of strings (e.g., ['-l', '-a'] for 'ls -l -a'). Ignored if use_shell is true - include arguments in command string instead.",
					},
					"working_directory": map[string]interface{}{
						"type":        "string",
						"description": "Relative directory path within workspace to execute command (default: root of workspace). Example: 'scripts' resolves to '/app/workspace-docs/scripts'. Sets the current working directory (CWD) for command execution, allowing relative paths in commands to resolve relative to this directory.",
					},
					"timeout": map[string]interface{}{
						"type":        "integer",
						"description": "Timeout in seconds (default: 60, max: 300)",
					},
					"use_shell": map[string]interface{}{
						"type":        "boolean",
						"description": "Execute through shell interpreter (sh -c). Enables complex commands with pipes (|), redirects (>), chaining (&&, ||), environment variables, wildcards, etc. Default: false (direct execution, more secure). Set to true for complex commands.",
					},
				},
				"required": []string{"command"},
			}),
		},
	}
	workspaceTools = append(workspaceTools, executeShellTool)

	// Add read_image tool
	readImageTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "read_image",
			Description: "Read an image file from workspace and ask a question about it. This tool will process the image and your question together.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filepath": map[string]interface{}{
						"type":        "string",
						"description": "Path to the image file. Must always be workspace-relative (e.g., 'Downloads/hdfc_login.png', 'images/photo.jpg', 'screenshots/screen.png'). Do not use absolute paths.",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Question to ask about the image (e.g., 'What is in this image?', 'Describe this image', 'What text is written here?')",
					},
				},
				"required": []string{"filepath", "query"},
			}),
		},
	}
	workspaceTools = append(workspaceTools, readImageTool)

	return workspaceTools
}

// GetWorkspaceToolCategory returns the category name for workspace tools
func GetWorkspaceToolCategory() string {
	return "workspace"
}

// GetHumanToolCategory returns the category name for human tools
func GetHumanToolCategory() string {
	return "human"
}
