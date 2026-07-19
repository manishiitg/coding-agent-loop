package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/agentsession"
	"github.com/manishiitg/coding-agent-loop/workspace/security"
)

// runShellOutput runs a prepared *exec.Cmd and normalises its combined output
// into the string the tool returns (truncated, non-empty, error-annotated).
func runShellOutput(cmd *exec.Cmd) (string, error) {
	out, err := cmd.CombinedOutput()
	result := string(out)
	const maxOut = 100 * 1024
	if len(result) > maxOut {
		result = result[:maxOut] + "\n...[output truncated]"
	}
	if err != nil {
		if strings.TrimSpace(result) != "" {
			return result + "\n[exit error: " + err.Error() + "]", nil
		}
		return "", fmt.Errorf("command failed: %w", err)
	}
	if strings.TrimSpace(result) == "" {
		return "(command produced no output)", nil
	}
	return result, nil
}

// childShellTool wires the EXISTING execute_shell_command for CHILD MODE, hardened
// with the reused workspace/security.Isolator (macOS sandbox-exec / Linux
// namespaces). The child may read its lessons and practice (shared/) and read and
// write its own work (child/), but the sandbox denies parent/ entirely — so the
// child agent can never reach the parent's answer keys or private notes.
func childShellTool() agentsession.Tool {
	t := shellTool()
	t.Description = "Run a shell command in your learning workspace. You can read your lessons and practice under " +
		"shared/ and read or write your own work under child/attempts/. You cannot see the parent's answer keys."
	t.Handler = func(ctx context.Context, args map[string]interface{}) (string, error) {
		command, _ := args["command"].(string)
		if strings.TrimSpace(command) == "" {
			return "", fmt.Errorf("command is required")
		}
		cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		iso := &security.Isolator{
			BaseDir:    workspaceRoot(),
			WorkDir:    workspaceRoot(),
			ReadPaths:  []string{"shared", "child"},
			WritePaths: []string{"child"},
		}
		// Pass the whole command as a single string (args nil) so it is executed
		// verbatim by the sandbox shell, not word-split.
		cmd, cleanup, err := iso.ExecuteIsolated(cctx, command, nil)
		if err != nil {
			return "", fmt.Errorf("sandbox setup failed: %w", err)
		}
		defer cleanup()
		return runShellOutput(cmd)
	}
	return t
}

// shellTool wires the EXISTING execute_shell_command bridge tool to a local
// executor scoped to the family workspace. It is NOT a new tool —
// execute_shell_command is already in mcpagent bridgeTools; the lean family
// runtime simply never registered its executor. In bridge-only mode the coding
// agent routes ALL of its file reads and writes through this one shell tool, so
// this is how "the existing structure" reads uploaded material and writes the
// study material / tests it generates.
//
// The command runs with the workspace as its working directory. For the parent
// (the trusted person running the app) a cwd-scoped shell is sufficient; the
// hardened workspace/security.Isolator sandbox is reserved for Child Mode.
func shellTool() agentsession.Tool {
	return agentsession.Tool{
		Name: "execute_shell_command",
		Description: "Run a shell command inside the family learning workspace (its working directory). " +
			"Use it to read uploaded material (e.g. cat \"shared/materials/<subject>/<topic>/notes.md\", or ls) " +
			"and to write the study material or practice tests you create (write files under shared/study/, " +
			"shared/tests/, or parent/ for answer keys). Paths are relative to the workspace root.",
		Category: "family_tools",
		Params: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{
					"type":        "string",
					"description": "the shell command to run, relative to the workspace working directory",
				},
			},
			"required": []string{"command"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			command, _ := args["command"].(string)
			if strings.TrimSpace(command) == "" {
				return "", fmt.Errorf("command is required")
			}
			cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
			defer cancel()
			cmd := exec.CommandContext(cctx, "bash", "-c", command)
			cmd.Dir = workspaceRoot()
			return runShellOutput(cmd)
		},
	}
}
