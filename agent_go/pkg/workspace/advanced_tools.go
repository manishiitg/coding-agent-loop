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

// webToolDef returns the fetch_web_content tool definition (single source of truth).
func webToolDef() llmtypes.Tool {
	return llmtypes.Tool{
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

// GetWebToolDefinitions returns only the web fetch (fetch_web_content) tool.
func GetWebToolDefinitions() []llmtypes.Tool {
	return []llmtypes.Tool{webToolDef()}
}

// GetPDFToolDefinitions returns only the PDF (read_pdf) tool.
func GetPDFToolDefinitions() []llmtypes.Tool {
	return []llmtypes.Tool{pdfToolDef()}
}

// GetAdvancedToolDefinitions returns all advanced workspace tools (shell, image, web, PDF). No duplication: built from the single-tool getters.
func GetAdvancedToolDefinitions() []llmtypes.Tool {
	var tools []llmtypes.Tool
	tools = append(tools, GetShellToolDefinitions()...)
	tools = append(tools, GetImageToolDefinitions()...)
	tools = append(tools, GetWebToolDefinitions()...)
	tools = append(tools, GetPDFToolDefinitions()...)
	return tools
}
