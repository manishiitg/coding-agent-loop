package virtualtools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// GetWorkspaceBrowserToolCategory returns the category name for workspace browser tools
func GetWorkspaceBrowserToolCategory() string {
	return "workspace_browser"
}

// CreateWorkspaceBrowserTools creates the single agent_browser virtual tool
// This tool enables browser automation for web testing, form filling, screenshots, and data extraction
func CreateWorkspaceBrowserTools() []llmtypes.Tool {
	var tools []llmtypes.Tool

	// Single agent_browser tool that wraps the agent-browser CLI
	agentBrowserTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "agent_browser",
			Description: "Execute browser automation commands using agent-browser CLI. Supports navigation, interaction, screenshots, and data extraction. IMPORTANT: Read the agent-browser skill documentation to understand how to use this tool effectively (snapshot → interact → re-snapshot workflow).",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type":        "string",
						"description": "Command to execute. Common commands: open (navigate to URL), snapshot (get page elements with refs like @e1), click (click element by ref), fill (clear and type text), type (type without clearing), press (keyboard key), screenshot (capture image), wait (wait for element/text/URL/network), get (get text/html/value/attr/title/url/count), scroll (scroll page/element into view), select (dropdown option), hover (hover over element), close (close browser), eval (run JavaScript), back, forward, reload.",
					},
					"args": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "string",
						},
						"description": "Arguments for the command. Examples: ['https://google.com'] for open, ['-i'] for snapshot (interactive elements), ['@e1'] for click, ['@e2', 'search query'] for fill, ['Enter'] for press, ['page.png'] for screenshot, ['text', '@e1'] for get.",
					},
					"session": map[string]interface{}{
						"type":        "string",
						"description": "Session name for the browser instance. Required. Use different session names to run multiple browsers in parallel.",
					},
				},
				"required": []string{"command", "session"},
			}),
		},
	}
	tools = append(tools, agentBrowserTool)

	return tools
}

// CreateWorkspaceBrowserToolExecutors creates the execution functions for workspace browser tools
func CreateWorkspaceBrowserToolExecutors() map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	executors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	executors["agent_browser"] = handleAgentBrowser

	return executors
}

// handleAgentBrowser executes the agent-browser CLI command via workspace-api
func handleAgentBrowser(ctx context.Context, args map[string]interface{}) (string, error) {
	command, ok := args["command"].(string)
	if !ok || command == "" {
		return "", fmt.Errorf("command is required")
	}

	// Build command arguments
	var cmdArgs []string

	// Add stealth options to avoid bot detection
	cmdArgs = append(cmdArgs, "--user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	cmdArgs = append(cmdArgs, "--args", "--disable-blink-features=AutomationControlled")

	// Add session flag (required)
	session, ok := args["session"].(string)
	if !ok || session == "" {
		return "", fmt.Errorf("session is required")
	}
	cmdArgs = append(cmdArgs, "--session", session)

	// Add the command
	cmdArgs = append(cmdArgs, command)

	// Add command arguments if provided
	if argsArray, ok := args["args"].([]interface{}); ok {
		for _, arg := range argsArray {
			if argStr, ok := arg.(string); ok {
				cmdArgs = append(cmdArgs, argStr)
			}
		}
	}

	// Always add --json for machine-readable output
	cmdArgs = append(cmdArgs, "--json")

	// Execute via workspace-api with timeout
	timeout := getTimeoutForCommand(command)
	output, err := executeAgentBrowserViaWorkspaceAPI(ctx, cmdArgs, timeout)
	if err != nil {
		return "", err
	}

	return output, nil
}

// getTimeoutForCommand returns an appropriate timeout based on the command type
func getTimeoutForCommand(command string) time.Duration {
	switch command {
	case "open", "goto", "navigate":
		return 60 * time.Second // Navigation can take longer
	case "screenshot", "pdf":
		return 60 * time.Second // Screenshots/PDFs can take longer
	case "wait":
		return 120 * time.Second // Wait operations may have longer timeouts
	case "close", "quit", "exit":
		return 10 * time.Second // Close should be quick
	default:
		return 30 * time.Second // Default timeout for most operations
	}
}

// FolderGuardConfig represents folder access restrictions
type FolderGuardConfig struct {
	Enabled      bool     `json:"enabled"`
	ReadPaths    []string `json:"read_paths"`
	WritePaths   []string `json:"write_paths"`
	BlockedPaths []string `json:"blocked_paths"`
}

// ShellExecuteRequest represents the request body for workspace-api /api/execute
type ShellExecuteRequest struct {
	Command     string             `json:"command"`
	Args        []string           `json:"args,omitempty"`
	Timeout     int                `json:"timeout,omitempty"`
	FolderGuard *FolderGuardConfig `json:"folder_guard,omitempty"`
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

// quoteShellArg quotes a shell argument if it contains special characters
func quoteShellArg(arg string) string {
	// If arg contains spaces, parentheses, or other special shell chars, quote it
	if strings.ContainsAny(arg, " \t\n()[]{}|&;'\"\\$`<>*?!") {
		// Use single quotes and escape any single quotes within
		escaped := strings.ReplaceAll(arg, "'", "'\"'\"'")
		return "'" + escaped + "'"
	}
	return arg
}

// executeAgentBrowserViaWorkspaceAPI runs an agent-browser command via the workspace-api shell endpoint
func executeAgentBrowserViaWorkspaceAPI(ctx context.Context, args []string, timeout time.Duration) (string, error) {
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Build the shell command: agent-browser [args...] with proper quoting
	quotedArgs := make([]string, len(args))
	for i, arg := range args {
		quotedArgs[i] = quoteShellArg(arg)
	}
	fullCommand := "agent-browser " + strings.Join(quotedArgs, " ")

	// Extract folder guard paths from context (same as execute_shell_command)
	var folderGuardReadPaths []string
	var folderGuardWritePaths []string
	var folderGuardBlockedPaths []string

	if readPaths := ctx.Value(FolderGuardReadPathsKey); readPaths != nil {
		if paths, ok := readPaths.([]string); ok {
			folderGuardReadPaths = paths
		}
	}
	if writePaths := ctx.Value(FolderGuardWritePathsKey); writePaths != nil {
		if paths, ok := writePaths.([]string); ok {
			folderGuardWritePaths = paths
		}
	}
	if blockedPaths := ctx.Value(FolderGuardBlockedPathsKey); blockedPaths != nil {
		if paths, ok := blockedPaths.([]string); ok {
			folderGuardBlockedPaths = paths
		}
	}

	// Prepare request body
	reqBody := ShellExecuteRequest{
		Command: fullCommand,
		Timeout: int(timeout.Seconds()),
	}

	// Add folder guard if paths are set in context
	if len(folderGuardReadPaths) > 0 || len(folderGuardWritePaths) > 0 || len(folderGuardBlockedPaths) > 0 {
		reqBody.FolderGuard = &FolderGuardConfig{
			Enabled:      true,
			ReadPaths:    folderGuardReadPaths,
			WritePaths:   folderGuardWritePaths,
			BlockedPaths: folderGuardBlockedPaths,
		}
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make HTTP request to workspace-api
	apiURL := getWorkspaceAPIURL() + "/api/execute"
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("command timed out after %v", timeout)
		}
		return "", fmt.Errorf("failed to execute command via workspace-api: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Parse response
	var apiResp APIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w (body: %s)", err, string(body))
	}

	// Check for errors
	if !apiResp.Success {
		errMsg := apiResp.Message
		if apiResp.Error != "" {
			errMsg = apiResp.Error
		}
		// Include stderr if available
		if apiResp.Data.Stderr != "" {
			return "", fmt.Errorf("command failed: %s\nStderr: %s", errMsg, apiResp.Data.Stderr)
		}
		return "", fmt.Errorf("command failed: %s", errMsg)
	}

	// Check exit code
	if apiResp.Data.ExitCode != 0 {
		output := apiResp.Data.Stdout
		if apiResp.Data.Stderr != "" {
			output = apiResp.Data.Stderr
		}
		return "", fmt.Errorf("command exited with code %d: %s", apiResp.Data.ExitCode, strings.TrimSpace(output))
	}

	// Return stdout (which contains JSON output from agent-browser)
	return apiResp.Data.Stdout, nil
}
