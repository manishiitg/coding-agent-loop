package models

// ExecuteShellRequest represents the request to execute a shell command
type ExecuteShellRequest struct {
	Command          string   `json:"command" binding:"required"`           // Shell command to execute
	Args             []string `json:"args,omitempty"`                       // Command arguments (ignored if use_shell is true)
	WorkingDirectory string   `json:"working_directory,omitempty"`          // Relative working directory within docs-dir
	Timeout          int      `json:"timeout,omitempty"`                    // Timeout in seconds (default: 60, max: 300)
	UseShell         bool     `json:"use_shell,omitempty"`                  // Execute through shell (enables pipes, redirects, &&, ||, etc.)
}

// ExecuteShellResponse represents the response from shell command execution
type ExecuteShellResponse struct {
	Stdout         string `json:"stdout"`          // Standard output
	Stderr         string `json:"stderr"`          // Standard error
	ExitCode       int    `json:"exit_code"`      // Process exit code
	ExecutionTimeMs int   `json:"execution_time_ms"` // Execution time in milliseconds
	Command        string `json:"command"`        // Full command that was executed
}

