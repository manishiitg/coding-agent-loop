package virtualtools

import (
	"context"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// GetWorkspaceAdvancedToolCategory returns the category name for workspace advanced tools
func GetWorkspaceAdvancedToolCategory() string {
	return "workspace_advanced"
}

// CreateWorkspaceAdvancedTools creates workspace advanced virtual tools (4 tools)
// These are the shell command execution, image analysis, web fetch, and PDF reading tools
func CreateWorkspaceAdvancedTools() []llmtypes.Tool {
	var tools []llmtypes.Tool

	// Add execute_shell_command tool
	executeShellTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "execute_shell_command",
			Description: "Execute shell commands and scripts within the workspace directory. Commands are executed through a shell interpreter (sh -c), enabling pipes (|), redirects (>), chaining (&&, ||), environment variables, wildcards, and heredocs. Commands run with a 60-second timeout (configurable up to 300 seconds) and are restricted to the workspace boundary (/app/workspace-docs).\n\n**PATH USAGE RULES:**\n- **Tool Parameters**: Use relative paths (e.g., 'working_directory: \"scripts\"' resolves to '/app/workspace-docs/scripts')\n- **Inside Scripts**: When writing Python/shell scripts that reference files, use absolute paths starting with '/app/workspace-docs' (e.g., '/app/workspace-docs/script.py', '/app/workspace-docs/data/file.csv'). This ensures scripts work regardless of the working_directory setting.\n\nReturns stdout, stderr, and exit code.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type":        "string",
						"description": "Shell command to execute. Supports complex commands with pipes, redirects, chaining, env vars, wildcards, and heredocs (e.g., 'ls -la', 'cat file.txt | grep pattern', 'echo \"content\" > file.txt', 'cmd1 && cmd2'). Include all arguments directly in the command string.",
					},
					"working_directory": map[string]interface{}{
						"type":        "string",
						"description": "REQUIRED. Relative directory path within workspace to execute command. Example: 'Workflow/MyProject/runs/initial/execution/step-1' resolves to '/app/workspace-docs/Workflow/MyProject/runs/initial/execution/step-1'. Sets the current working directory (CWD) for command execution, allowing relative paths in commands to resolve relative to this directory.",
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
	tools = append(tools, executeShellTool)

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
	tools = append(tools, readImageTool)

	// Add fetch_web_content tool
	fetchWebContentTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "fetch_web_content",
			Description: "Fetch content from a URL. Supports HTTP/HTTPS requests with configurable timeout. Can optionally convert HTML responses to markdown for easier processing by the LLM.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type":        "string",
						"description": "The URL to fetch content from (must be http:// or https://)",
					},
					"timeout": map[string]interface{}{
						"type":        "integer",
						"description": "Timeout in seconds (default: 30, max: 120)",
					},
					"convert_to_markdown": map[string]interface{}{
						"type":        "boolean",
						"description": "Convert HTML to markdown for easier processing (default: true)",
					},
					"headers": map[string]interface{}{
						"type":        "object",
						"description": "Optional custom headers (e.g., {\"Authorization\": \"Bearer token\"})",
					},
				},
				"required": []string{"url"},
			}),
		},
	}
	tools = append(tools, fetchWebContentTool)

	// Add read_pdf tool
	readPDFTool := llmtypes.Tool{
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
				},
				"required": []string{"filepath"},
			}),
		},
	}
	tools = append(tools, readPDFTool)

	return tools
}

// CreateWorkspaceAdvancedToolExecutors creates the execution functions for workspace advanced tools
func CreateWorkspaceAdvancedToolExecutors() map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	executors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	executors["execute_shell_command"] = handleExecuteShellCommand
	executors["read_image"] = handleReadImage
	executors["fetch_web_content"] = handleFetchWebContent
	executors["read_pdf"] = handleReadPDF

	return executors
}
