package handlers

import (
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"strings"
)

const workspaceDebugLoggingEnv = "WORKSPACE_DEBUG_LOGGING"

func workspaceDebugLoggingEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(workspaceDebugLoggingEnv))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func workspaceDebugLogf(format string, args ...interface{}) {
	if workspaceDebugLoggingEnabled() {
		log.Printf(format, args...)
	}
}

func workspaceDebugPrintf(format string, args ...interface{}) {
	if workspaceDebugLoggingEnabled() {
		fmt.Printf(format, args...)
	}
}

// shellCommandLogIdentity is safe to record in diagnostics. Commands can
// contain credentials, customer data, or entire generated artifacts, so even
// opt-in debug logs must not persist their contents.
func shellCommandLogIdentity(command string) (int, string) {
	sum := sha256.Sum256([]byte(command))
	return len(command), fmt.Sprintf("%x", sum[:6])
}
