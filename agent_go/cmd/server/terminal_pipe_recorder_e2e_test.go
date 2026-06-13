package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"mcp-agent-builder-go/agent_go/internal/terminals"
)

func TestTerminalPipeRecorderHTTPE2EPreservesAnsiAndAppends(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not found in PATH")
	}

	sessionName := "runloop-pipe-e2e-" + strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", "-")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := runTerminalTmuxCommand(ctx, "", "new-session", "-d", "-s", sessionName, "/bin/sh"); err != nil {
		t.Fatalf("start tmux session: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cleanupCancel()
		_ = runTerminalTmuxCommand(cleanupCtx, "", "kill-session", "-t", sessionName)
	})

	sendTmuxLiteralCommand(t, ctx, sessionName, "printf '\\033[31mred-start\\033[0m\\n'; printf '\\033[33mspinner-frame-1\\033[0m\\n'; printf 'MCP_API_TOKEN=super-secret\\nMCP_AUTH=Authorization: Bearer bearer-secret\\nSECRET_FOO=hidden-secret\\n'")
	waitForTmuxCapture(t, ctx, sessionName, "red-start")

	store := terminals.NewStore()
	api := &StreamingAPI{
		terminalStore: store,
		terminalPipeRecorder: &terminalPipeRecorder{
			root:     t.TempDir(),
			sessions: make(map[string]*terminalPipeRecording),
		},
	}
	sessionID := "session-terminal-pipe-e2e"
	ownerID := "workflow-step:pipe-recorder"
	terminalID := sessionID + ":" + ownerID
	store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, ownerID, sessionName, "seed pane", 1))

	first := getTerminalScreenDetail(t, api, terminalID)
	if first.ContentSource != "tmux_pipe" {
		t.Fatalf("content_source = %q, want tmux_pipe", first.ContentSource)
	}
	if !strings.Contains(first.Content, "\x1b[31m") || !strings.Contains(first.Content, "red-start") {
		t.Fatalf("first content should preserve red ANSI output, got:\n%q", first.Content)
	}
	if !strings.Contains(first.Content, "\x1b[33m") || !strings.Contains(first.Content, "spinner-frame-1") {
		t.Fatalf("first content should preserve spinner ANSI output, got:\n%q", first.Content)
	}
	for _, leaked := range []string{"super-secret", "bearer-secret", "hidden-secret"} {
		if strings.Contains(first.Content, leaked) {
			t.Fatalf("first content leaked secret %q:\n%q", leaked, first.Content)
		}
	}
	for _, redacted := range []string{"MCP_API_TOKEN=[redacted]", "MCP_AUTH=Authorization: Bearer [redacted]", "SECRET_FOO=[redacted]"} {
		if !strings.Contains(first.Content, redacted) {
			t.Fatalf("first content missing redacted marker %q:\n%q", redacted, first.Content)
		}
	}

	sendTmuxLiteralCommand(t, ctx, sessionName, "printf '\\033[34mblue-append\\033[0m\\n'")
	second := waitForTerminalDetail(t, api, terminalID, "blue-append")
	if second.ContentSource != "tmux_pipe" {
		t.Fatalf("second content_source = %q, want tmux_pipe", second.ContentSource)
	}
	if !strings.HasPrefix(second.Content, first.Content) {
		t.Fatalf("pipe-backed content must append, not replace\nfirst=%q\nsecond=%q", first.Content, second.Content)
	}
	if !strings.Contains(second.Content, "\x1b[34m") || !strings.Contains(second.Content, "blue-append") {
		t.Fatalf("second content should preserve appended blue ANSI output, got:\n%q", second.Content)
	}

	store.HandleEvent(sessionID, terminalRouteEndEvent(sessionID, ownerID, sessionName, 0))
	if err := runTerminalTmuxCommand(ctx, "", "kill-session", "-t", sessionName); err != nil {
		t.Fatalf("kill tmux session: %v", err)
	}

	static := getTerminalScreenDetailNoHeaderCheck(t, api, terminalID)
	if static.State != "stale" || static.TmuxSession != "" {
		t.Fatalf("static terminal state/tmux = %q/%q, want stale with no tmux session", static.State, static.TmuxSession)
	}
	if static.ContentSource != "tmux_pipe" {
		t.Fatalf("static content_source = %q, want tmux_pipe", static.ContentSource)
	}
	if !strings.Contains(static.Content, "\x1b[34m") || !strings.Contains(static.Content, "blue-append") {
		t.Fatalf("static content should preserve pipe ANSI after tmux is gone, got:\n%q", static.Content)
	}
	stored, ok := store.Get(terminalID)
	if !ok {
		t.Fatalf("stored terminal missing after stale transition")
	}
	if stored.ContentSource != "tmux_pipe" || !strings.Contains(stored.Content, "\x1b[34m") {
		t.Fatalf("stored static snapshot lost pipe color source/content: source=%q content=%q", stored.ContentSource, stored.Content)
	}
}

func sendTmuxLiteralCommand(t *testing.T, ctx context.Context, sessionName, command string) {
	t.Helper()
	if err := runTerminalTmuxCommand(ctx, "", "send-keys", "-t", sessionName, "-l", command); err != nil {
		t.Fatalf("send tmux command literal: %v", err)
	}
	if err := runTerminalTmuxCommand(ctx, "", "send-keys", "-t", sessionName, "C-m"); err != nil {
		t.Fatalf("send tmux enter: %v", err)
	}
}

func waitForTmuxCapture(t *testing.T, ctx context.Context, sessionName, needle string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		content, err := runTerminalTmuxOutputCommand(ctx, "capture-pane", "-p", "-e", "-t", sessionName)
		if err == nil && strings.Contains(content, needle) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for tmux capture to contain %q", needle)
}

func waitForTerminalDetail(t *testing.T, api *StreamingAPI, terminalID, needle string) terminals.Snapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var latest terminals.Snapshot
	for time.Now().Before(deadline) {
		latest = getTerminalScreenDetail(t, api, terminalID)
		if strings.Contains(latest.Content, needle) {
			return latest
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for terminal detail to contain %q; latest=%q", needle, latest.Content)
	return terminals.Snapshot{}
}

func getTerminalScreenDetail(t *testing.T, api *StreamingAPI, terminalID string) terminals.Snapshot {
	t.Helper()
	snapshot, header := getTerminalScreenDetailWithHeader(t, api, terminalID)
	if header != "tmux_pipe" {
		t.Fatalf("debug content source header = %q, want tmux_pipe", header)
	}
	return snapshot
}

func getTerminalScreenDetailNoHeaderCheck(t *testing.T, api *StreamingAPI, terminalID string) terminals.Snapshot {
	t.Helper()
	snapshot, _ := getTerminalScreenDetailWithHeader(t, api, terminalID)
	return snapshot
}

func getTerminalScreenDetailWithHeader(t *testing.T, api *StreamingAPI, terminalID string) (terminals.Snapshot, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/terminals/"+terminalID+"?content=screen&debug=1", nil)
	req = mux.SetURLVars(req, map[string]string{"terminal_id": terminalID})
	rec := httptest.NewRecorder()
	api.handleGetTerminal(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get terminal status = %d body=%s", rec.Code, rec.Body.String())
	}
	var snapshot terminals.Snapshot
	if err := json.NewDecoder(rec.Body).Decode(&snapshot); err != nil {
		t.Fatalf("decode terminal detail: %v", err)
	}
	return snapshot, rec.Header().Get("X-Runloop-Terminal-Content-Source")
}
