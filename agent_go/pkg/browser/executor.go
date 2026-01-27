package browser

import (
	"context"
	"fmt"
	"time"
)

// Executor handles the execution of browser tool commands
type Executor struct {
	Client *Client
}

// NewExecutor creates a new browser executor
func NewExecutor(client *Client) *Executor {
	return &Executor{
		Client: client,
	}
}

// HandleAgentBrowser executes the agent-browser CLI command
func (e *Executor) HandleAgentBrowser(ctx context.Context, args map[string]interface{}) (string, error) {
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

	// Determine timeout
	timeout := getTimeoutForCommand(command)

	// Execute via client
	output, err := e.Client.ExecuteCommand(ctx, cmdArgs, &ExecuteOptions{
		Timeout: timeout,
		// Note: FolderGuard can be added here if passed via context or struct fields in future
	})
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
