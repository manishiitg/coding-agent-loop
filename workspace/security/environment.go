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

		// DO NOT include:
		// - DATABASE_URL
		// - API_KEYS
		// - JWT_SECRET
		// - Any other secrets from parent process
	}
}
