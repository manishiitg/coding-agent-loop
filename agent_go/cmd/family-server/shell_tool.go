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
// deny-by-default (StrictAllowlist) and scoped to JUST the CURRENT activity
// FOLDER — the whole folder, read AND write (its content files, activity.json,
// its conversation, its attempts). The child annotates the activity's files in
// place (recording "✓ Answered" notes on the real file — no separate copy).
// Everything else is invisible: older activities, materials, reports, memory,
// other subjects — so the child can never reach anything the parent didn't hand
// them. Also gets read-only access to the real machine Downloads folder (see
// hostDownloadsPath). Keep this scoping in sync with childCanSee/childCanWrite
// (child_workspace.go) — both derive from currentActivityDir().
func childShellTool() agentsession.Tool {
	t := shellTool()
	t.Description = "Run a shell command in your learning workspace. You can read and write the activity your parent " +
		"just handed you (all its files are in your current activity folder). You cannot see the parent's answer keys."
	t.Handler = func(ctx context.Context, args map[string]interface{}) (string, error) {
		command, _ := args["command"].(string)
		if strings.TrimSpace(command) == "" {
			return "", fmt.Errorf("command is required")
		}
		cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		// The current activity FOLDER — readable AND writable as a whole, so the
		// tutor works through and annotates its files in place. Nothing else.
		var scope []string
		if dir := currentActivityDir(); dir != "" {
			scope = []string{dir}
		}
		readPaths := append([]string{}, scope...)
		writePaths := append([]string{}, scope...)
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
			// so only the current activity folder + Downloads are visible, not the
			// rest of the real machine or workspace.
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
// and AgentWorks automations (macOS sandbox-exec / Linux namespaces). The
// parent is the trusted authoring agent, so it reads and writes the WHOLE
// workspace (activities under <Subject>/<Topic>/, materials/, reports/,
// memory/, answer keys, conversations) — the only carve-out is skills/, which
// is app-shipped content re-seeded from the binary each startup and kept
// read-only, and the real machine Downloads folder (read-only). Paths are
// workspace-relative.
func shellTool() agentsession.Tool {
	return agentsession.Tool{
		Name: "execute_shell_command",
		Description: "Run a shell command inside the family learning workspace (its working directory). " +
			"Use it to read uploaded material (e.g. cat \"materials/<subject>/<topic>/notes.md\", or ls), " +
			"read files the parent downloaded to this computer's Downloads folder, and to write the content you " +
			"create — the study material / test HTML and its activity.json go inside a " +
			"<Subject>/<Topic>/<activity>/ folder; an answer key goes in that same folder as <name>-KEY.md. " +
			"Paths are relative to the workspace root.",
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
			// Parent reads+writes the whole workspace root; skills/ is read-only
			// (re-seeded from the binary), Downloads read-only.
			readPaths := []string{"."}
			writePaths := []string{"."}
			blockedWrites := []string{"skills", ".agents", "AGENTS.md"}
			if dl := hostDownloadsPath(); dl != "" {
				readPaths = append(readPaths, dl)
				blockedWrites = append(blockedWrites, dl)
			}
			iso := &security.Isolator{
				BaseDir:           workspaceRoot(),
				WorkDir:           workspaceRoot(),
				ReadPaths:         readPaths,
				WritePaths:        writePaths,
				BlockedWritePaths: blockedWrites,
			}
			cmd, cleanup, err := iso.ExecuteIsolated(cctx, command, nil)
			if err != nil {
				return "", fmt.Errorf("sandbox setup failed: %w", err)
			}
			defer cleanup()
			// Every saved secret is available to the parent's shell as
			// SECRET_<NAME> (see secrets_store.go) — the raw value only ever
			// reaches this sandboxed process's environment, never the model's
			// own context. Parent-only: childShellTool() never gets these.
			cmd.Env = append(cmd.Env, secretEnvPairs()...)
			return runShellOutput(cmd)
		},
	}
}
