package security

import (
	"os"
	"strings"
)

// BuildSafeEnvironment returns a sanitized set of environment variables.
// In Docker mode, this is a strict whitelist to prevent secret leakage.
// In native mode, it inherits the host environment but strips known secrets,
// so host-installed tools (aws, node, python, etc.) and their config remain accessible.
func BuildSafeEnvironment() []string {
	if os.Getenv("NATIVE_WORKSPACE") == "true" {
		return buildNativeEnvironment()
	}
	return buildDockerEnvironment()
}

// buildDockerEnvironment returns a strict whitelist for Docker containers.
func buildDockerEnvironment() []string {
	env := []string{
		// Essential shell variables
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/tmp",
		"USER=agent",
		"SHELL=/bin/sh",

		// Locale settings
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",

		// Browser automation (agent-browser + Playwright use system chromium)
		"AGENT_BROWSER_EXECUTABLE_PATH=/usr/bin/chromium",
		"PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH=/usr/bin/chromium",

		// Python: disable output buffering so stdout/stderr are captured even if the process is killed (timeout/signal)
		"PYTHONUNBUFFERED=1",

		// Allow pip install when Python is externally managed (PEP 668); avoids "break system packages" errors in LLM-run shells
		"PIP_BREAK_SYSTEM_PACKAGES=1",

		// DO NOT include:
		// - DATABASE_URL
		// - API_KEYS
		// - JWT_SECRET
		// - Any other secrets from parent process
	}

	return env
}

// buildNativeEnvironment inherits the host environment but strips secrets.
// This preserves PATH, HOME, and tool configs (AWS, Node, Go, etc.) so
// host-installed CLIs work normally, while preventing accidental leakage
// of server-internal secrets to agent-executed shell commands.
func buildNativeEnvironment() []string {
	// Env var names (case-insensitive prefix match) that must NOT leak to shell commands.
	// These are server-internal secrets, not user/agent credentials.
	blockedPrefixes := []string{
		"DATABASE_URL",
		"JWT_SECRET",
		"LANGFUSE_",
		"SUPABASE_",
		"OPENAI_API_KEY",
		"ANTHROPIC_API_KEY",
		"AZURE_OPENAI_",
		"GOOGLE_AI_",
		"BEDROCK_",
		"OPENROUTER_",
		"AGENT_PROVIDER",
		"AGENT_MODEL",
		"DEEP_SEARCH_",
		"MULTI_USER_",
	}

	// Exact env var names to block
	blockedExact := map[string]bool{
		"MCP_API_TOKEN": true,
	}

	var env []string
	for _, kv := range os.Environ() {
		key := kv
		if idx := strings.IndexByte(kv, '='); idx >= 0 {
			key = kv[:idx]
		}

		if blockedExact[key] {
			continue
		}

		blocked := false
		keyUpper := strings.ToUpper(key)
		for _, prefix := range blockedPrefixes {
			if strings.HasPrefix(keyUpper, prefix) {
				blocked = true
				break
			}
		}
		if blocked {
			continue
		}

		env = append(env, kv)
	}

	// Ensure Python output buffering is disabled
	env = append(env, "PYTHONUNBUFFERED=1")

	return env
}
