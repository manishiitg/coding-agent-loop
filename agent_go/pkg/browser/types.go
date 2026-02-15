package browser

// FolderGuardConfig represents folder access restrictions
type FolderGuardConfig struct {
	Enabled      bool     `json:"enabled"`
	ReadPaths    []string `json:"read_paths"`
	WritePaths   []string `json:"write_paths"`
	BlockedPaths []string `json:"blocked_paths"`
}

// ShellExecuteRequest represents the request body for workspace-api /api/execute
type ShellExecuteRequest struct {
	Command          string             `json:"command"`
	WorkingDirectory string             `json:"working_directory"`
	Args             []string           `json:"args,omitempty"`
	Timeout          int                `json:"timeout,omitempty"`
	FolderGuard      *FolderGuardConfig `json:"folder_guard,omitempty"`
}

// ShellExecuteResponse represents the response data from workspace-api /api/execute
type ShellExecuteResponse struct {
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	ExitCode        int    `json:"exit_code"`
	ExecutionTimeMs int    `json:"execution_time_ms"`
	Command         string `json:"command"`
}

// APIResponse represents the workspace-api response wrapper
type APIResponse struct {
	Success bool                 `json:"success"`
	Message string               `json:"message"`
	Data    ShellExecuteResponse `json:"data"`
	Error   string               `json:"error,omitempty"`
}
