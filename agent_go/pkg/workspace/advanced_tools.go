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
			Description: "Execute a shell command and return stdout, stderr, and exit code. Runs via shell (`sh -c`) and may be subject to workspace access restrictions.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					// NOTE: use_shell was removed from the tool definition to simplify the interface for the LLM.
					// It is now hardcoded to true internally in ExecuteShellCommand.
					"command": map[string]interface{}{
						"type":        "string",
						"description": "Shell command to execute as a single string, including any arguments and shell operators.",
					},
					"timeout": map[string]interface{}{
						"type":        "integer",
						"description": "Optional timeout in seconds.",
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

// videoReadToolDef returns the read_video tool definition (single source of truth).
func videoReadToolDef() llmtypes.Tool {
	return llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "read_video",
			Description: "Read a video file from workspace and ask a question about it. This tool uploads the video to the configured video-understanding provider and returns a text analysis.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filepath": map[string]interface{}{
						"type":        "string",
						"description": "Path to the video file. Must always be workspace-relative (e.g., 'Downloads/demo.mp4', 'videos/clip.mov'). Do not use absolute paths.",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Question to ask about the video (e.g., 'Summarize this video', 'What actions happen?', 'Extract visible text and events').",
					},
					"provider": map[string]interface{}{
						"type":        "string",
						"description": "Optional video-understanding provider override. Supported: 'kimi' (default) or 'z-ai' (Z.AI Vision MCP video_analysis).",
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

// generateTextLLMToolDef returns the generate_text_llm tool definition (single source of truth).
func generateTextLLMToolDef() llmtypes.Tool {
	return llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "generate_text_llm",
			Description: "Generate text with the workspace tiered LLM configuration. Provide the user message and choose the 'high', 'medium', or 'low' tier to run it.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"user_message": map[string]interface{}{
						"type":        "string",
						"description": "The prompt to send to the selected tier model.",
					},
					"tier": map[string]interface{}{
						"type":        "string",
						"description": "Reasoning tier to use for text generation.",
						"enum":        []string{"high", "medium", "low"},
					},
				},
				"required": []string{"user_message", "tier"},
			}),
		},
	}
}

// searchWebLLMToolDef returns the search_web_llm tool definition (single source of truth).
func searchWebLLMToolDef() llmtypes.Tool {
	return llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "search_web_llm",
			Description: "Search the web using a published search-capable model. If provider is omitted, the workspace primary search provider is used with configured fallbacks.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "The web search query.",
					},
					"provider": map[string]interface{}{
						"type":        "string",
						"description": "Optional published provider override to use for this search, e.g. gemini-cli, vertex, claude-code, codex-cli, or minimax-coding-plan.",
					},
				},
				"required": []string{"query"},
			}),
		},
	}
}

// diffPatchToolDef returns the diff_patch_workspace_file tool definition.
func diffPatchToolDef() llmtypes.Tool {
	return llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "diff_patch_workspace_file",
			Description: "Apply a unified diff patch to a file and return the result.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filepath": map[string]interface{}{
						"type":        "string",
						"description": "Path to the file to patch.",
					},
					"diff": map[string]interface{}{
						"type":        "string",
						"description": "Unified diff patch string to apply.\n\nFormat:\n- Headers: --- a/file and +++ b/file\n- Hunk headers: @@ -startLine,lineCount +startLine,lineCount @@\n- Context lines: ' ' prefix (space + content)\n- Removals: '-' prefix\n- Additions: '+' prefix\n- End with a trailing newline\n\nExample:\n--- a/file\n+++ b/file\n@@ -5,1 +5,1 @@\n-- [ ] task-1\n+- [x] task-1\n",
					},
				},
				"required": []string{"filepath", "diff"},
			}),
		},
	}
}

// GetShellToolDefinitions returns only the shell (execute_shell_command) tool.
func GetShellToolDefinitions() []llmtypes.Tool {
	return []llmtypes.Tool{shellToolDef()}
}

// GetImageToolDefinitions returns image/video understanding tools.
func GetImageToolDefinitions() []llmtypes.Tool {
	return []llmtypes.Tool{imageToolDef(), videoReadToolDef()}
}

// GetPDFToolDefinitions returns only the PDF (read_pdf) tool.
func GetPDFToolDefinitions() []llmtypes.Tool {
	return []llmtypes.Tool{pdfToolDef()}
}

// GetGenerateTextLLMToolDefinitions returns only the text generation tool.
func GetGenerateTextLLMToolDefinitions() []llmtypes.Tool {
	return []llmtypes.Tool{generateTextLLMToolDef()}
}

// GetSearchWebLLMToolDefinitions returns only the web search tool.
func GetSearchWebLLMToolDefinitions() []llmtypes.Tool {
	return []llmtypes.Tool{searchWebLLMToolDef()}
}

// GetDiffPatchToolDefinitions returns only the diff_patch_workspace_file tool definition.
func GetDiffPatchToolDefinitions() []llmtypes.Tool {
	return []llmtypes.Tool{diffPatchToolDef()}
}

// GetAdvancedToolDefinitions returns all advanced workspace tools (shell, image/video, PDF, text generation, diff patch).
func GetAdvancedToolDefinitions() []llmtypes.Tool {
	var tools []llmtypes.Tool
	tools = append(tools, GetShellToolDefinitions()...)
	tools = append(tools, GetImageToolDefinitions()...)
	tools = append(tools, GetPDFToolDefinitions()...)
	tools = append(tools, GetGenerateTextLLMToolDefinitions()...)
	tools = append(tools, GetSearchWebLLMToolDefinitions()...)
	tools = append(tools, GetDiffPatchToolDefinitions()...)
	return tools
}
