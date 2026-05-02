package common

import "strings"

// cliProviders is the canonical set of LLM provider IDs that run as CLI
// agents. These providers expose tools through the local CLI's MCP server
// rather than the standard tool-calling API, and they ship CLI-specific
// concerns the rest of the codebase needs to detect:
//   - prompt template variants (the CLI's own tool-calling docs)
//   - HTTP api-bridge tool mapping (mcp__api-bridge__*)
//   - native conversation/context management (the CLI maintains its own history)
//
// Add new CLI providers here. Every other site in the codebase that needed
// this decision was duplicating a literal list — call IsCLIProvider instead.
//
// NOTE: This is *only* the "is this a CLI runtime?" question. It is **not**
// the "should this agent run in code-execution mode?" question — code-exec
// mode is now always on regardless of provider, so call sites that decide
// `UseCodeExecutionMode` should just set it to true unconditionally.
var cliProviders = map[string]struct{}{
	"claude-code": {},
	"gemini-cli":  {},
	"codex-cli":   {},
	"kimi":        {},
}

// IsCLIProvider reports whether the given provider ID is a CLI agent
// runtime (claude-code, gemini-cli, codex-cli, kimi). The lookup is
// case-insensitive and whitespace-trimmed for resilience against config
// drift.
func IsCLIProvider(provider string) bool {
	_, ok := cliProviders[strings.ToLower(strings.TrimSpace(provider))]
	return ok
}
