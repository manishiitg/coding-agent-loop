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
			Description: "Execute a shell command and return stdout, stderr, and exit code. Runs via shell (`sh -c`) with the working directory set to the workspace docs root. Both relative paths (resolved against the docs root) and absolute paths under the docs root are accepted. Absolute paths under any OTHER host root (e.g. /Users/... outside the docs root, /home/...) are rejected by the path guard.",
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
			Description: "Read an image file from workspace and ask a question about it using a provider-backed vision model. Before choosing provider/model_id, call list_llm_capabilities(capability=\"read_image\", include_models=true). If you pass model_id, also pass the matching provider from that capability result; do not pass model_id by itself.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filepath": map[string]interface{}{
						"type":        "string",
						"description": "Full absolute path to the image file under the workspace docs root (e.g., '/Users/.../workspace-docs/_users/default/Chats/photo.png', '/app/workspace-docs/_users/default/Downloads/hdfc_login.png'). Workspace-relative paths are rejected. Absolute paths outside the workspace docs root are rejected.",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Question to ask about the image (e.g., 'What is in this image?', 'Describe this image', 'What text is written here?')",
					},
					"provider": map[string]interface{}{
						"type":        "string",
						"description": "Optional image-analysis provider override. Discover currently usable providers with list_llm_capabilities(capability=\"read_image\", include_models=true). If specifying model_id, pass the matching provider too.",
					},
					"model_id": map[string]interface{}{
						"type":        "string",
						"description": "Optional image-analysis model id. Use a model from list_llm_capabilities(capability=\"read_image\", include_models=true), and pass the matching provider in the same call. Do not use tier labels such as low, medium, high, or auto as model IDs.",
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
			Description: "Read a video file from workspace and ask a question about it using a provider-backed video-understanding model. Direct video providers are not advertised by default; before choosing provider/model_id, call list_llm_capabilities(capability=\"read_video\", include_models=true).",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filepath": map[string]interface{}{
						"type":        "string",
						"description": "Full absolute path to the video file under the workspace docs root (e.g., '/Users/.../workspace-docs/_users/default/Chats/demo.mp4', '/app/workspace-docs/_users/default/Downloads/demo.mp4'). Workspace-relative paths are rejected. Absolute paths outside the workspace docs root are rejected.",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Question to ask about the video (e.g., 'Summarize this video', 'What actions happen?', 'Extract visible text and events').",
					},
					"provider": map[string]interface{}{
						"type":        "string",
						"description": "Optional video-understanding provider override. Discover currently usable providers with list_llm_capabilities(capability=\"read_video\", include_models=true). If specifying model_id, pass the matching provider too.",
					},
					"model_id": map[string]interface{}{
						"type":        "string",
						"description": "Optional video-understanding model id. Use a model from list_llm_capabilities(capability=\"read_video\", include_models=true), and pass the matching provider in the same call.",
					},
				},
				"required": []string{"filepath", "query"},
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
			Description: "Search the web using a published search-capable provider. Before choosing provider/model_id, call list_llm_capabilities(capability=\"search_web\", include_models=true). Provider is required. If you pass model_id, pass the matching provider from that capability result; do not pass model_id by itself. model_id can be omitted only when accepting the backend's working default for that provider.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "The web search query.",
					},
					"provider": map[string]interface{}{
						"type":        "string",
						"description": "Required published provider, e.g. gemini-cli, vertex, claude-code, codex-cli, cursor-cli, pi-cli, or minimax-coding-plan. Discover usable providers with list_llm_capabilities(capability=\"search_web\", include_models=true).",
					},
					"model_id": map[string]interface{}{
						"type":        "string",
						"description": "Optional published model_id override. Use a model from list_llm_capabilities(capability=\"search_web\", include_models=true), and pass the matching provider in the same call. If omitted, or if a provider alias such as codex-cli is passed, the tool selects a working model for the provider.",
					},
				},
				"required": []string{"query", "provider"},
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
			Description: "Apply a unified diff patch to a workspace file and return the result. The filepath may be workspace-relative or an absolute path under the workspace docs root.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filepath": map[string]interface{}{
						"type":        "string",
						"description": "Path to the file to patch. Accepts workspace-relative paths like \"Workflow/my-flow/learnings/_global/SKILL.md\" and absolute paths under the workspace docs root like \"/Users/.../workspace-docs/Workflow/my-flow/learnings/_global/SKILL.md\". Absolute paths outside the docs root are rejected.",
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

// GetAdvancedToolDefinitions returns all advanced workspace tools (shell, image/video, text generation, diff patch).
func GetAdvancedToolDefinitions() []llmtypes.Tool {
	var tools []llmtypes.Tool
	tools = append(tools, GetShellToolDefinitions()...)
	tools = append(tools, GetImageToolDefinitions()...)
	tools = append(tools, GetGenerateTextLLMToolDefinitions()...)
	tools = append(tools, GetSearchWebLLMToolDefinitions()...)
	tools = append(tools, GetDiffPatchToolDefinitions()...)
	return tools
}
