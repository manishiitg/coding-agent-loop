package browser

import (
	"context"
	"fmt"
	"os"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/common"
)

// ExecutorOption is a functional option for configuring an Executor
type ExecutorOption func(*Executor)

// WithCdpPort configures the executor to connect to an existing Chrome via CDP on the given port
func WithCdpPort(port int) ExecutorOption {
	return func(e *Executor) {
		e.CdpPort = port
	}
}

// Executor handles the execution of browser tool commands
type Executor struct {
	Client  *Client
	CdpPort int // When > 0, connect to existing Chrome via CDP instead of launching headless
}

// NewExecutor creates a new browser executor
func NewExecutor(client *Client, opts ...ExecutorOption) *Executor {
	e := &Executor{
		Client: client,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// HandleAgentBrowser executes the agent-browser CLI command
func (e *Executor) HandleAgentBrowser(ctx context.Context, args map[string]interface{}) (string, error) {
	command, ok := args["command"].(string)
	if !ok || command == "" {
		return "", fmt.Errorf("command is required")
	}

	// Build command arguments
	var cmdArgs []string

	// If CDP mode is enabled, connect to existing Chrome instead of launching headless
	// agent-browser runs inside Docker, so we pass a full URL using host.docker.internal
	// to reach Chrome on the host machine. Chrome rejects non-localhost Host headers,
	// so we resolve host.docker.internal to its IP and use http://<ip>:<port>.
	if e.CdpPort > 0 {
		cdpURL := resolveCdpURL(e.CdpPort)
		cmdArgs = append(cmdArgs, "--cdp", cdpURL)
	} else {
		// Add stealth options to avoid bot detection (not needed for user's real Chrome)
		cmdArgs = append(cmdArgs, "--user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
		cmdArgs = append(cmdArgs, "--args", "--disable-blink-features=AutomationControlled")
	}

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

	// Build FolderGuard from context (same pattern as shell tool)
	var folderGuard *FolderGuardConfig
	if allowedWrites, ok := ctx.Value(common.FolderGuardAllowedWriteFolderKey).([]string); ok && len(allowedWrites) > 0 {
		folderGuard = &FolderGuardConfig{
			Enabled:    true,
			WritePaths: allowedWrites,
			ReadPaths:  []string{"."},
		}
	}

	// Use browser downloads path as working directory if set (for screenshots/downloads in workflows)
	workingDir := "."
	if downloadsPath, ok := ctx.Value(common.BrowserDownloadsPathKey).(string); ok && downloadsPath != "" {
		workingDir = downloadsPath
	}

	// Execute via client
	output, err := e.Client.ExecuteCommand(ctx, cmdArgs, &ExecuteOptions{
		Timeout:         timeout,
		FolderGuard:     folderGuard,
		WorkingDirectory: workingDir,
	})
	if err != nil {
		return "", err
	}

	return output, nil
}

// resolveCdpURL builds the CDP URL for agent-browser running inside Docker.
// agent-browser executes inside the workspace Docker container, so "localhost"
// refers to the container, not the host. We use host.docker.internal to reach
// the host's Chrome. Chrome rejects non-IP Host headers, so we mark the URL
// for runtime resolution in client.go (getent resolves hostname→IP inside container).
// CDP_HOST env var overrides this (e.g. "localhost" when not using Docker).
func resolveCdpURL(port int) string {
	if host := os.Getenv("CDP_HOST"); host != "" {
		return fmt.Sprintf("http://%s:%d", host, port)
	}
	// Use host.docker.internal — client.go will resolve this to an IP at runtime
	// inside the container to avoid Chrome rejecting non-IP Host headers.
	return fmt.Sprintf("http://host.docker.internal:%d", port)
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
