package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/agentsession"
)

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
		},
	}
}
