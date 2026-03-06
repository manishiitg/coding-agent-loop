package geminicli

import "github.com/manishiitg/multi-llm-provider-go/llmtypes"

// Constants for custom metadata keys
const (
	MetadataKeyGeminiModel      = "gemini_model"
	MetadataKeyResumeSessionID  = "gemini_resume_session_id"
	MetadataKeyApprovalMode     = "gemini_approval_mode"
	MetadataKeySystemPromptFile = "gemini_system_prompt_file"
	MetadataKeyAllowedTools     = "gemini_allowed_tools"
	// MetadataKeyProjectSettings holds a JSON string to write to .gemini/settings.json
	// in a temp working directory. This controls tools.core (tool restriction),
	// mcpServers (MCP bridge), and other Gemini CLI project settings.
	MetadataKeyProjectSettings = "gemini_project_settings"

	// MetadataKeyProjectDirID controls which per-invocation project directory to use.
	// When set, the adapter uses /tmp/gemini-cli-project-{id} instead of generating a new one.
	// This is used to ensure resume calls use the same directory as the original invocation.
	MetadataKeyProjectDirID = "gemini_project_dir_id"
)

// WithGeminiModel sets the --model flag for the Gemini CLI.
func WithGeminiModel(model string) llmtypes.CallOption {
	return func(opts *llmtypes.CallOptions) {
		ensureMetadata(opts)
		opts.Metadata.Custom[MetadataKeyGeminiModel] = model
	}
}

// WithResumeSessionID sets the --resume flag with a session ID so the Gemini CLI
// resumes an existing session instead of starting a new one.
func WithResumeSessionID(sessionID string) llmtypes.CallOption {
	return func(opts *llmtypes.CallOptions) {
		ensureMetadata(opts)
		opts.Metadata.Custom[MetadataKeyResumeSessionID] = sessionID
	}
}

// WithApprovalMode sets the --approval-mode flag for the Gemini CLI.
// Use "yolo" to skip all permission prompts.
func WithApprovalMode(mode string) llmtypes.CallOption {
	return func(opts *llmtypes.CallOptions) {
		ensureMetadata(opts)
		opts.Metadata.Custom[MetadataKeyApprovalMode] = mode
	}
}

// WithSystemPromptFile sets the GEMINI_SYSTEM_MD environment variable
// to a file path containing the system prompt.
func WithSystemPromptFile(path string) llmtypes.CallOption {
	return func(opts *llmtypes.CallOptions) {
		ensureMetadata(opts)
		opts.Metadata.Custom[MetadataKeySystemPromptFile] = path
	}
}

// WithProjectSettings sets a JSON string to be written as .gemini/settings.json
// in a temporary working directory. The Gemini CLI is then run from that directory.
// This is the primary mechanism for:
//   - Restricting built-in tools via tools.core allowlist
//   - Configuring MCP servers (bridge) via mcpServers
//   - Any other project-level settings
//
// Example JSON:
//
//	{"tools":{"core":["google_web_search"]},"mcpServers":{"api-bridge":{"command":"/path/to/mcpbridge","env":{"MCP_API_URL":"..."}}}}
func WithProjectSettings(settingsJSON string) llmtypes.CallOption {
	return func(opts *llmtypes.CallOptions) {
		ensureMetadata(opts)
		opts.Metadata.Custom[MetadataKeyProjectSettings] = settingsJSON
	}
}

// WithProjectDirID sets an explicit project directory ID so the Gemini CLI uses
// /tmp/gemini-cli-project-{id}. This ensures resume calls and retries use the
// same isolated project directory as the original invocation.
func WithProjectDirID(id string) llmtypes.CallOption {
	return func(opts *llmtypes.CallOptions) {
		ensureMetadata(opts)
		opts.Metadata.Custom[MetadataKeyProjectDirID] = id
	}
}

// WithAllowedTools sets the --allowed-tools flag for the Gemini CLI.
// These tools bypass the confirmation dialog.
// Example: "mcp__api-bridge__*" to allow all tools from the bridge.
func WithAllowedTools(tools string) llmtypes.CallOption {
	return func(opts *llmtypes.CallOptions) {
		ensureMetadata(opts)
		opts.Metadata.Custom[MetadataKeyAllowedTools] = tools
	}
}

func ensureMetadata(opts *llmtypes.CallOptions) {
	if opts.Metadata == nil {
		opts.Metadata = &llmtypes.Metadata{Custom: make(map[string]interface{})}
	}
	if opts.Metadata.Custom == nil {
		opts.Metadata.Custom = make(map[string]interface{})
	}
}
