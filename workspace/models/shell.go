package models

// ExecuteShellRequest represents the request to execute a shell command
type ExecuteShellRequest struct {
	Command          string `json:"command" binding:"required"`             // Shell command to execute (single string with all arguments)
	WorkingDirectory string `json:"working_directory"`                       // Relative working directory within docs-dir (default: "." for root)
	Timeout          int    `json:"timeout,omitempty"`                      // Timeout in seconds (default: 60, max: 300)
	UseShell         bool   `json:"use_shell,omitempty"`                    // Execute through shell (enables pipes, redirects, &&, ||, etc.)

	// Folder guard configuration
	FolderGuard *FolderGuardConfig `json:"folder_guard,omitempty"`

	// Extra environment variables to inject (only MCP_* and SECRET_* prefixed vars are allowed)
	ExtraEnv map[string]string `json:"extra_env,omitempty"`
}

type FolderGuardConfig struct {
	Enabled         bool     `json:"enabled"`
	ReadPaths       []string `json:"read_paths"`
	WritePaths      []string `json:"write_paths"`
	BlockedPaths    []string `json:"blocked_paths"`    // Paths to block (deny list) - takes precedence over read/write paths
	EnforcementMode string   `json:"enforcement_mode"` // "strict" | "warn" | "audit"
}

// IsPathBlocked checks if a path is in the blocked paths list
func (f *FolderGuardConfig) IsPathBlocked(path string) bool {
	if f == nil || !f.Enabled || len(f.BlockedPaths) == 0 {
		return false
	}
	for _, blocked := range f.BlockedPaths {
		if len(path) >= len(blocked) && path[:len(blocked)] == blocked {
			return true
		}
		// Also check without trailing slash
		blockedNoSlash := blocked
		if len(blocked) > 0 && blocked[len(blocked)-1] == '/' {
			blockedNoSlash = blocked[:len(blocked)-1]
		}
		if path == blockedNoSlash {
			return true
		}
	}
	return false
}

// ExecuteShellResponse represents the response from shell command execution
type ExecuteShellResponse struct {
	Stdout         string `json:"stdout"`          // Standard output
	Stderr         string `json:"stderr"`          // Standard error
	ExitCode       int    `json:"exit_code"`      // Process exit code
	ExecutionTimeMs int   `json:"execution_time_ms"` // Execution time in milliseconds
	Command        string `json:"command"`        // Full command that was executed
}

