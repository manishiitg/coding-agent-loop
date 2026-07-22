package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/browser"
	"github.com/manishiitg/coding-agent-loop/workspace/security"
)

// The browser feature REUSES AgentWorks' pkg/browser.Executor wholesale — the
// full CDP tab manager (cdp_tabs.go/cdp_registry.go), artifact broker, live
// status, and error rewriting — rather than reimplementing any of it. The
// Executor talks to a "workspace-api" HTTP server (POST /api/execute, GET
// /api/cdp-check); AgentWorks runs that as a separate service. This file is
// the ONLY app-specific glue: a tiny loopback server implementing exactly
// those two endpoints, backed by family-server's own security.Isolator and
// the shared workspace/security artifact-staging helpers. It is bound to
// 127.0.0.1 on an ephemeral port and never exposed on the public API — the
// endpoint runs shell commands, so it must never be reachable from the
// frontend/CORS surface.

var (
	browserBackendURL  string
	browserBackendOnce sync.Once
)

// startBrowserBackend brings up the loopback execution backend and returns its
// base URL (http://127.0.0.1:<port>). Idempotent.
func startBrowserBackend() (string, error) {
	var startErr error
	browserBackendOnce.Do(func() {
		ln, err := net.Listen("tcp", "127.0.0.1:0") // loopback only, ephemeral port
		if err != nil {
			startErr = fmt.Errorf("browser backend listen: %w", err)
			return
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/api/execute", handleBrowserExecute)
		mux.HandleFunc("/api/cdp-check", handleBrowserCDPCheck)
		browserBackendURL = "http://" + ln.Addr().String()
		go func() {
			if err := http.Serve(ln, mux); err != nil {
				log.Printf("[browser-backend] serve stopped: %v", err)
			}
		}()
		log.Printf("[browser-backend] listening on %s (loopback only)", browserBackendURL)
	})
	if startErr != nil {
		return "", startErr
	}
	return browserBackendURL, nil
}

// handleBrowserCDPCheck mirrors workspace-api's /api/cdp-check: an HTTP probe
// of Chrome's /json/version on 127.0.0.1:<port> (not a bare TCP dial — Chrome
// answers HTTP), returning {connected, error}. This is how the reused
// Executor decides CDP is actually reachable.
func handleBrowserCDPCheck(w http.ResponseWriter, r *http.Request) {
	port := strings.TrimSpace(r.URL.Query().Get("port"))
	if port == "" {
		port = "9222"
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/json/version")
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"connected": false, "error": err.Error()})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		writeJSON(w, http.StatusOK, map[string]interface{}{"connected": false, "error": fmt.Sprintf("HTTP %d", resp.StatusCode)})
		return
	}
	var v struct {
		Browser           string `json:"Browser"`
		WebSocketDebugURL string `json:"webSocketDebuggerUrl"`
	}
	if json.NewDecoder(resp.Body).Decode(&v) != nil || (v.Browser == "" && v.WebSocketDebugURL == "") {
		writeJSON(w, http.StatusOK, map[string]interface{}{"connected": false, "error": "unexpected /json/version response"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"connected": true, "browser": v.Browser})
}

// handleBrowserExecute mirrors workspace-api's /api/execute: run one
// agent-browser command under family-server's sandbox, honoring the reused
// Executor's FolderGuard + ArtifactTransfer + UploadTransfer contract so
// screenshots/downloads/recordings are staged into the workspace exactly as
// AgentWorks does. Only the execution + staging is app-specific; all the
// browser logic that decides WHAT to run lives in the reused Executor.
func handleBrowserExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req browser.ShellExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, browser.APIResponse{Success: false, Error: "invalid JSON body"})
		return
	}
	command := stripShellWrapper(strings.TrimSpace(req.Command))
	if command == "" {
		writeJSON(w, http.StatusBadRequest, browser.APIResponse{Success: false, Error: "command is required"})
		return
	}

	root := workspaceRoot()
	workingDir := root
	if wd := strings.TrimSpace(req.WorkingDirectory); wd != "" && wd != "." {
		joined := filepath.Clean(filepath.Join(root, wd))
		if joined != root && !strings.HasPrefix(joined, root+string(os.PathSeparator)) {
			writeJSON(w, http.StatusBadRequest, browser.APIResponse{Success: false, Error: "working directory escapes workspace"})
			return
		}
		workingDir = joined
	}
	if err := os.MkdirAll(workingDir, 0o700); err != nil {
		writeJSON(w, http.StatusInternalServerError, browser.APIResponse{Success: false, Error: err.Error()})
		return
	}

	fg := req.FolderGuard
	guarded := fg != nil && fg.Enabled

	// Artifact/upload transfers only ever come with an enabled FolderGuard
	// (the Executor gates them that way); reject otherwise, matching AgentWorks.
	if req.ArtifactTransfer != nil {
		if !guarded {
			writeJSON(w, http.StatusBadRequest, browser.APIResponse{Success: false, Error: "artifact transfer requires folder guard"})
			return
		}
		if err := security.PrepareBrowserArtifactStaging(req.ArtifactTransfer.SourcePath); err != nil {
			writeJSON(w, http.StatusBadRequest, browser.APIResponse{Success: false, Error: err.Error()})
			return
		}
	}
	for _, up := range req.UploadTransfers {
		if !guarded {
			writeJSON(w, http.StatusBadRequest, browser.APIResponse{Success: false, Error: "upload transfer requires folder guard"})
			return
		}
		if err := security.StageBrowserUpload(up.SourcePath, up.StagedPath, root, workingDir, fg.ReadPaths, fg.WritePaths, fg.BlockedPaths); err != nil {
			writeJSON(w, http.StatusBadRequest, browser.APIResponse{Success: false, Error: err.Error()})
			return
		}
		staged := up.StagedPath
		defer security.CleanupBrowserUploadStaging(staged)
	}

	timeout := 60 * time.Second
	if req.Timeout > 0 {
		timeout = time.Duration(req.Timeout) * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	start := time.Now()
	stdout, stderr, exitCode, runErr := runBrowserCommand(ctx, command, workingDir, root, fg, guarded)
	elapsed := int(time.Since(start).Milliseconds())

	if ctx.Err() == context.DeadlineExceeded {
		writeJSON(w, http.StatusRequestTimeout, browser.APIResponse{
			Success: false, Message: "command timed out",
			Data: browser.ShellExecuteResponse{Stdout: stdout, Stderr: "TIMEOUT: " + stderr, ExitCode: -1, ExecutionTimeMs: elapsed, Command: command},
		})
		return
	}
	if runErr != nil && exitCode == -1 {
		// A real start failure (binary missing, sandbox setup) — not a normal
		// non-zero exit (those keep Success:true with the real code below).
		writeJSON(w, http.StatusInternalServerError, browser.APIResponse{
			Success: false, Error: runErr.Error(),
			Data: browser.ShellExecuteResponse{Stdout: stdout, Stderr: stderr, ExitCode: -1, ExecutionTimeMs: elapsed, Command: command},
		})
		return
	}

	// Publish a produced artifact into the workspace on success.
	if exitCode == 0 && req.ArtifactTransfer != nil && req.ArtifactTransfer.Finalize {
		if err := security.FinalizeBrowserArtifact(req.ArtifactTransfer.SourcePath, req.ArtifactTransfer.DestinationPath, req.ArtifactTransfer.Kind, root, fg.WritePaths, fg.BlockedPaths, fg.BlockedWritePaths); err != nil {
			writeJSON(w, http.StatusBadRequest, browser.APIResponse{Success: false, Error: err.Error(),
				Data: browser.ShellExecuteResponse{Stdout: stdout, Stderr: stderr, ExitCode: exitCode, ExecutionTimeMs: elapsed, Command: command}})
			return
		}
	}

	writeJSON(w, http.StatusOK, browser.APIResponse{
		Success: true,
		Data:    browser.ShellExecuteResponse{Stdout: stdout, Stderr: stderr, ExitCode: exitCode, ExecutionTimeMs: elapsed, Command: command},
	})
}

// runBrowserCommand executes one command, returning stdout, stderr, exit code
// (-1 for a start failure), and a start error if any. A normal non-zero exit
// returns exitCode>0 with runErr nil.
func runBrowserCommand(ctx context.Context, command, workingDir, root string, fg *browser.FolderGuardConfig, guarded bool) (string, string, int, error) {
	var cmd *exec.Cmd
	var cleanup func()
	if guarded {
		iso := &security.Isolator{
			BaseDir:           root,
			WorkDir:           workingDir,
			ReadPaths:         fg.ReadPaths,
			WritePaths:        fg.WritePaths,
			BlockedPaths:      fg.BlockedPaths,
			BlockedWritePaths: fg.BlockedWritePaths,
		}
		c, cl, err := iso.ExecuteIsolated(ctx, command, nil)
		if err != nil {
			return "", "", -1, fmt.Errorf("sandbox setup failed: %w", err)
		}
		cmd, cleanup = c, cl
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
		cmd.Dir = workingDir
		cmd.Env = security.BuildSafeEnvironment()
	}
	if cleanup != nil {
		defer cleanup()
	}

	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	stdout, stderr := outBuf.String(), errBuf.String()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return stdout, stderr, exitErr.ExitCode(), nil // normal non-zero exit
		}
		return stdout, stderr, -1, err // start failure
	}
	return stdout, stderr, 0, nil
}

// stripShellWrapper unwraps a leading `sh -c`/`bash -c` some callers add, so
// the command runs the same whether wrapped or not (mirrors workspace-api).
func stripShellWrapper(command string) string {
	for _, p := range []string{"sh -c ", "bash -c "} {
		if strings.HasPrefix(command, p) {
			return strings.TrimSpace(command[len(p):])
		}
	}
	return command
}
