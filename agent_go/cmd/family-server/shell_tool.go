package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/agentsession"
	"github.com/manishiitg/coding-agent-loop/workspace/security"
)

// hostDownloadsPath returns the real macOS/Linux Downloads folder on this
// machine — an ABSOLUTE path outside workspaceRoot(). Mirrors AgentWorks'
// PI_HOST_DOWNLOADS_PATH/HOST_DOWNLOADS_PATH convention (env override, else
// $HOME/Downloads) so a parent who downloaded a syllabus/form/PDF can have the
// agent read it directly, the same grant AgentWorks gives a workflow session.
// Isolator accepts absolute ReadPaths/BlockedWritePaths alongside
// workspace-relative ones (filepath.IsAbs check in isolator.go), so this can be
// added straight into the same lists as "shared"/"parent"/etc.
func hostDownloadsPath() string {
	for _, key := range []string{"PI_HOST_DOWNLOADS_PATH", "HOST_DOWNLOADS_PATH"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return filepath.Clean(v)
		}
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, "Downloads")
}

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
// namespaces). It's a REAL access boundary, not a UI filter: the sandbox is
// deny-by-default (StrictAllowlist) and scoped to JUST the CURRENT activity —
// the exact files the parent most recently handed off, plus the child's own
// child/attempts/ scratch space and the child/current-task.json pointer. The
// child reads AND writes those activity files directly (recording "✓ Answered"
// progress notes onto the real file — there is no separate copy). Everything
// else is invisible: older activities, the rest of shared/, and parent/
// entirely — so the child can never reach the parent's answer keys or private
// notes. Also gets read-only access to the real machine Downloads folder (see
// hostDownloadsPath), same grant AgentWorks gives a workflow session. Keep
// this scoping in sync with childCanSee/childCanWrite (child_workspace.go).
func childShellTool() agentsession.Tool {
	t := shellTool()
	t.Description = "Run a shell command in your learning workspace. You can read and write the lesson/practice your parent " +
		"just handed you, and read or write your own work under child/attempts/. You cannot see the parent's answer keys."
	t.Handler = func(ctx context.Context, args map[string]interface{}) (string, error) {
		command, _ := args["command"].(string)
		if strings.TrimSpace(command) == "" {
			return "", fmt.Errorf("command is required")
		}
		cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		// The current activity's real (shared/) item paths — readable AND
		// writable, so the tutor annotates them in place. Plus the child's own
		// scratch space and the pointer file.
		items := currentActivityItems()
		readPaths := append([]string{"child/current-task.json", "child/attempts"}, items...)
		writePaths := append([]string{"child/attempts"}, items...)
		blockedWrites := []string{}
		if dl := hostDownloadsPath(); dl != "" {
			readPaths = append(readPaths, dl)
			blockedWrites = append(blockedWrites, dl) // read-only: block writes there explicitly
		}
		iso := &security.Isolator{
			BaseDir:           workspaceRoot(),
			WorkDir:           workspaceRoot(),
			ReadPaths:         readPaths,
			WritePaths:        writePaths,
			BlockedWritePaths: blockedWrites,
			// StrictAllowlist: the child is an untrusted party — deny-by-default
			// so only the current activity's files, child/attempts, and Downloads
			// are visible, not the rest of the real machine or workspace.
			StrictAllowlist: true,
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
// Sandboxed with the SAME workspace/security.Isolator mechanism as Child Mode
// and AgentWorks automations (macOS sandbox-exec / Linux namespaces) — not a
// bespoke parent-only exemption. The parent can read everything (shared/,
// parent/, skills/, child/ — it needs to skim child/ for progress evidence,
// and can write into child/ too, e.g. filing something on the child's behalf)
// but can only WRITE under shared/, parent/, and child/. skills/ is app-shipped
// reference content seeded from the binary on every startup: the OS sandbox
// makes it genuinely read-only here, the same guarantee that keeps the child
// out of parent/ — this isn't a suggestion the agent can accidentally violate.
// Also gets read-only access to the real machine Downloads folder (see
// hostDownloadsPath), matching the grant AgentWorks gives a workflow session.
func shellTool() agentsession.Tool {
	return agentsession.Tool{
		Name: "execute_shell_command",
		Description: "Run a shell command inside the family learning workspace (its working directory). " +
			"Use it to read uploaded material (e.g. cat \"shared/materials/<subject>/<topic>/notes.md\", or ls), " +
			"read files the parent downloaded to this computer's Downloads folder, " +
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
			readPaths := []string{"shared", "parent", "skills", "child"}
			blockedWrites := []string{}
			if dl := hostDownloadsPath(); dl != "" {
				readPaths = append(readPaths, dl)
				blockedWrites = append(blockedWrites, dl)
			}
			iso := &security.Isolator{
				BaseDir:           workspaceRoot(),
				WorkDir:           workspaceRoot(),
				ReadPaths:         readPaths,
				WritePaths:        []string{"shared", "parent", "child"},
				BlockedWritePaths: blockedWrites,
			}
			cmd, cleanup, err := iso.ExecuteIsolated(cctx, command, nil)
			if err != nil {
				return "", fmt.Errorf("sandbox setup failed: %w", err)
			}
			defer cleanup()
			return runShellOutput(cmd)
		},
	}
}
