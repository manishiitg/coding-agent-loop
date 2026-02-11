package security

// BuildSafeEnvironment returns a whitelisted set of environment variables
// This prevents secret leakage (DATABASE_URL, API_KEYS, etc.) to subprocess commands
func BuildSafeEnvironment() []string {
	return []string{
		// Essential shell variables
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/tmp",
		"USER=agent",
		"SHELL=/bin/sh",

		// Locale settings
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",

		// Browser automation (agent-browser uses system chromium on Alpine)
		"AGENT_BROWSER_EXECUTABLE_PATH=/usr/bin/chromium-browser",

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
}
