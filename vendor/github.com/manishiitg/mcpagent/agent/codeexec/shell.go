package codeexec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
)

// maxOutputBytes is the maximum size of stdout/stderr captured from shell commands (100KB).
const maxOutputBytes = 100 * 1024

// ShellCommandParams is the JSON-schema for execute_shell_command.
var ShellCommandParams = map[string]interface{}{
	"type": "object",
	"properties": map[string]interface{}{
		"command": map[string]interface{}{
			"type":        "string",
			"description": "The shell command to execute",
		},
	},
	"required": []string{"command"},
}

// ShellCommandDescription is the tool description for execute_shell_command.
const ShellCommandDescription = "Execute a shell command and return stdout, stderr, and exit code. Use this to run code, call HTTP endpoints with curl, or perform any shell operation."

// ExecuteShellCommand runs a shell command via sh -c and returns
// a formatted string with exit_code, stdout, and stderr.
// env specifies the environment for the child process; if nil, the current
// process's environment is inherited (which may leak secrets).
// Callers should pass BuildSafeEnvironment() plus any required env vars.
// stdout and stderr are each capped at maxOutputBytes.
func ExecuteShellCommand(ctx context.Context, args map[string]interface{}, env []string) (string, error) {
	command, ok := args["command"].(string)
	if !ok {
		return "", fmt.Errorf("command must be a string")
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", command) //nolint:gosec // G204: intentional — this tool's purpose is to execute user-provided commands
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if env != nil {
		cmd.Env = env
	}

	err := cmd.Run()

	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return "", fmt.Errorf("failed to execute command: %w", err)
		}
	}

	stdoutStr := truncateOutput(stdout.Bytes(), maxOutputBytes)
	stderrStr := truncateOutput(stderr.Bytes(), maxOutputBytes)

	return fmt.Sprintf("exit_code: %d\nstdout:\n%s\nstderr:\n%s", exitCode, stdoutStr, stderrStr), nil
}

// truncateOutput truncates output to maxBytes and appends a truncation notice.
func truncateOutput(data []byte, maxBytes int) string {
	if len(data) <= maxBytes {
		return string(data)
	}
	return string(data[:maxBytes]) + "\n... [truncated, output exceeded 100KB limit]"
}
