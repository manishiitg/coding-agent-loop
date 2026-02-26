package workspace

import (
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// shellToolDef returns the execute_shell_command tool definition (single source of truth).
func shellToolDef() llmtypes.Tool {
	return llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "execute_shell_command",
			Description: "Execute shell commands and scripts within the workspace directory. Commands run with a 60-second timeout (configurable up to 300 seconds) and are restricted to the workspace boundary.\n\n**PATH USAGE RULES:**\n- **Tool Parameters**: Use relative paths (e.g., 'working_directory: \"scripts\"')\n- **Inside Scripts**: When writing Python/shell scripts that reference files, use relative paths from the working directory or workspace root. The shell starts in the workspace root by default.\n\nReturns stdout, stderr, and exit code. Always executes via shell (sh -c), supporting pipes (|), redirects (>), chaining (&&, ||), environment variables, and wildcards.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					// NOTE: use_shell was removed from the tool definition to simplify the interface for the LLM. 
					// It is now hardcoded to true internally in ExecuteShellCommand.
					"command": map[string]interface{}{
						"type":        "string",
						"description": "Shell command to execute as a single string including all arguments. Supports pipes, redirects, chaining, env vars, and wildcards (e.g., 'ls -la | grep .md', 'cat file.txt', 'python3 script.py'). Do NOT wrap with 'sh -c'.",
					},
					"working_directory": map[string]interface{}{
						"type":        "string",
						"description": "Relative directory path within workspace to execute command. Example: 'scripts' resolves to the scripts/ folder in workspace. Use '.' for workspace root.",
					},
					"timeout": map[string]interface{}{
						"type":        "integer",
						"description": "Timeout in seconds (default: 60, max: 300)",
					},
				},
				"required": []string{"command", "working_directory"},
			}),
		},
	}
}

// imageToolDef returns the read_image tool definition (single source of truth).
func imageToolDef() llmtypes.Tool {
	return llmtypes.Tool{
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
}

// pdfToolDef returns the read_pdf tool definition (single source of truth).
func pdfToolDef() llmtypes.Tool {
	return llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "read_pdf",
			Description: "Read and extract text content from a PDF file in the workspace. Returns the text content of all pages. Useful for analyzing documents, reports, and other PDF files.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filepath": map[string]interface{}{
						"type":        "string",
						"description": "Path to the PDF file. Must be workspace-relative (e.g., 'documents/report.pdf', 'Downloads/contract.pdf'). Do not use absolute paths.",
					},
					"page_range": map[string]interface{}{
						"type":        "string",
						"description": "Optional page range to extract (e.g., '1-5', '1,3,5', 'all'). Default: 'all'",
					},
					"max_pages": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of pages to extract (default: 50, max: 100). Use to limit large documents.",
					},
					"password": map[string]interface{}{
						"type":        "string",
						"description": "Optional password to decrypt a password-protected PDF.",
					},
				},
				"required": []string{"filepath"},
			}),
		},
	}
}

// GetShellToolDefinitions returns only the shell (execute_shell_command) tool.
func GetShellToolDefinitions() []llmtypes.Tool {
	return []llmtypes.Tool{shellToolDef()}
}

// GetImageToolDefinitions returns only the image (read_image) tool.
func GetImageToolDefinitions() []llmtypes.Tool {
	return []llmtypes.Tool{imageToolDef()}
}

// GetPDFToolDefinitions returns only the PDF (read_pdf) tool.
func GetPDFToolDefinitions() []llmtypes.Tool {
	return []llmtypes.Tool{pdfToolDef()}
}

// GetAdvancedToolDefinitions returns all advanced workspace tools (shell, image, PDF, diff_patch). No duplication: built from the single-tool getters.
func GetAdvancedToolDefinitions() []llmtypes.Tool {
	var tools []llmtypes.Tool
	tools = append(tools, GetShellToolDefinitions()...)
	tools = append(tools, GetImageToolDefinitions()...)
	tools = append(tools, GetPDFToolDefinitions()...)
	tools = append(tools, GetDiffPatchToolDefinitions()...)
	return tools
}

// GetDiffPatchToolDefinitions returns the diff_patch_workspace_file tool definition.
func GetDiffPatchToolDefinitions() []llmtypes.Tool {
	return []llmtypes.Tool{diffPatchToolDef()}
}

func diffPatchToolDef() llmtypes.Tool {
	return llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "diff_patch_workspace_file",
			Description: "Apply a unified diff patch to a workspace file. Use execute_shell_command to 'cat' the file first to see its exact content, then generate a diff using 'diff -U0' format with perfect context matching. Use for targeted, surgical changes to specific file sections.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filepath": map[string]interface{}{
						"type":        "string",
						"description": "Full file path of the file to patch (e.g., 'docs/guide.md', 'Plans/plan-123/plan.md')",
					},
					"diff": map[string]interface{}{
						"type":        "string",
						"description": "Unified diff format string to apply:\n\n**FORMAT (like 'diff -U0'):**\n- Headers: --- a/file.md and +++ b/file.md\n- Hunk headers: @@ -startLine,lineCount +startLine,lineCount @@\n- Context lines: ' ' prefix (SPACE + content - MUST match file exactly)\n- Removals: '-' prefix (MINUS + content)\n- Additions: '+' prefix (PLUS + content)\n- MUST end with newline character\n\nContext lines start with SPACE ( ), NOT minus (-). Example:\n--- a/plan.md\n+++ b/plan.md\n@@ -5,1 +5,1 @@\n-- [ ] **task-1**: Do something\n+- [x] **task-1**: Do something\n",
					},
				},
				"required": []string{"filepath", "diff"},
			}),
		},
	}
}
