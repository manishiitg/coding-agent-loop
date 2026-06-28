package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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

	// Boot the recorder FIRST so pipe-pane is attached before we emit output. The
	// seed is now a clean control prologue (no flattened text snapshot), so only
	// bytes the CLI writes AFTER pipe-pane attaches appear in the stream. /bin/sh
	// is a normal-buffer command, so the prologue is RIS (no alternate screen).
	boot := getTerminalHistoryDetail(t, api, terminalID)
	if boot.ContentSource != "tmux_pipe" {
		t.Fatalf("boot content_source = %q, want tmux_pipe", boot.ContentSource)
	}
	if !strings.Contains(boot.Content, terminalPipeNormalScreenPrologue) {
		t.Fatalf("boot content should begin with the normal-screen prologue (RIS), got:\n%q", boot.Content)
	}
	if strings.Contains(boot.Content, terminalPipeAltScreenPrologue) {
		t.Fatalf("normal-buffer /bin/sh must NOT be seeded with the alternate-screen prologue, got:\n%q", boot.Content)
	}

	sendTmuxLiteralCommand(t, ctx, sessionName, "printf '\\033[31mred-start\\033[0m\\n'; printf '\\033[33mspinner-frame-1\\033[0m\\n'; printf 'MCP_API_TOKEN=super-secret\\nMCP_AUTH=Authorization: Bearer bearer-secret\\nSECRET_FOO=hidden-secret\\n'")
	first := waitForTerminalDetail(t, api, terminalID, "red-start")
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

	static := getTerminalHistoryDetailNoHeaderCheck(t, api, terminalID)
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

func waitForTerminalDetail(t *testing.T, api *StreamingAPI, terminalID, needle string) terminals.Snapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var latest terminals.Snapshot
	for time.Now().Before(deadline) {
		latest = getTerminalHistoryDetail(t, api, terminalID)
		if strings.Contains(latest.Content, needle) {
			return latest
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for terminal detail to contain %q; latest=%q", needle, latest.Content)
	return terminals.Snapshot{}
}

func getTerminalHistoryDetail(t *testing.T, api *StreamingAPI, terminalID string) terminals.Snapshot {
	t.Helper()
	snapshot, header := getTerminalHistoryDetailWithHeader(t, api, terminalID)
	if header != "tmux_pipe" {
		t.Fatalf("debug content source header = %q, want tmux_pipe", header)
	}
	return snapshot
}

func getTerminalHistoryDetailNoHeaderCheck(t *testing.T, api *StreamingAPI, terminalID string) terminals.Snapshot {
	t.Helper()
	snapshot, _ := getTerminalHistoryDetailWithHeader(t, api, terminalID)
	return snapshot
}

func getTerminalHistoryDetailWithHeader(t *testing.T, api *StreamingAPI, terminalID string) (terminals.Snapshot, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/terminals/"+terminalID+"?content=history&debug=1", nil)
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

// TestTerminalPipeRecorderPrologueMatchesScreenMode verifies the seed prologue is
// chosen from the pane's real alternate-screen state, and that an unknown state
// falls back to the safe normal-screen reset (never forcing the alternate screen).
func TestTerminalPipeRecorderPrologueMatchesScreenMode(t *testing.T) {
	cases := []struct {
		name        string
		alternateOn string
		queryErr    bool
		want        string
	}{
		{name: "alt-screen TUI", alternateOn: "1", want: terminalPipeAltScreenPrologue},
		{name: "normal buffer", alternateOn: "0", want: terminalPipeNormalScreenPrologue},
		{name: "unknown value falls back to RIS", alternateOn: "garbage", want: terminalPipeNormalScreenPrologue},
		{name: "query error falls back to RIS", queryErr: true, want: terminalPipeNormalScreenPrologue},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			oldRunOutput := runTerminalTmuxOutputCommand
			runTerminalTmuxOutputCommand = func(ctx context.Context, args ...string) (string, error) {
				if tc.queryErr {
					return "", context.DeadlineExceeded
				}
				return tc.alternateOn, nil
			}
			defer func() { runTerminalTmuxOutputCommand = oldRunOutput }()

			got := terminalPipeRecorderPrologue(context.Background(), "mlp-some-session")
			if got != tc.want {
				t.Fatalf("prologue = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestTerminalPipeRecorderResetForResizeTruncatesAndReseeds verifies a geometry
// change truncates the old-width frames out of the pipe log and re-seeds a fresh
// mode-aware prologue so the frontend never replays mismatched-width frames.
func TestTerminalPipeRecorderResetForResizeTruncatesAndReseeds(t *testing.T) {
	tmuxSession := "mlp-claude-resize-reset"
	dir := t.TempDir()
	path := filepath.Join(dir, terminalPipeRecorderFileName(tmuxSession))
	oldFrames := "\x1b[?1049h\x1b[2J\x1b[Hold-width frame one\x1b[2;1Hold-width frame two"
	if err := os.WriteFile(path, []byte(oldFrames), 0o600); err != nil {
		t.Fatalf("seed old pipe log: %v", err)
	}

	recorder := &terminalPipeRecorder{
		root: dir,
		sessions: map[string]*terminalPipeRecording{
			tmuxSession: {tmuxSession: tmuxSession, path: path, started: true},
		},
	}

	oldRunOutput := runTerminalTmuxOutputCommand
	runTerminalTmuxOutputCommand = func(ctx context.Context, args ...string) (string, error) {
		joined := strings.Join(args, " ")
		switch {
		case strings.Contains(joined, "#{alternate_on}"):
			return "1", nil
		case strings.Contains(joined, "#{window_width}"):
			return "120\t40", nil
		default:
			return "", nil
		}
	}
	oldRun := runTerminalTmuxCommand
	runTerminalTmuxCommand = func(ctx context.Context, stdin string, args ...string) error { return nil }
	defer func() {
		runTerminalTmuxOutputCommand = oldRunOutput
		runTerminalTmuxCommand = oldRun
	}()

	recorder.ResetForResize(context.Background(), tmuxSession)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pipe log after reset: %v", err)
	}
	got := string(data)
	if strings.Contains(got, "old-width frame") {
		t.Fatalf("reset must truncate old-width frames, got:\n%q", got)
	}
	if got != terminalPipeAltScreenPrologue {
		t.Fatalf("reset must re-seed the alternate-screen prologue, got:\n%q", got)
	}
}

// TestTerminalPipeRecorderResetForResizeIgnoresUnknownSession verifies the reset
// is a safe no-op when no live recording exists for the session.
func TestTerminalPipeRecorderResetForResizeIgnoresUnknownSession(t *testing.T) {
	recorder := &terminalPipeRecorder{
		root:     t.TempDir(),
		sessions: make(map[string]*terminalPipeRecording),
	}
	called := false
	oldRunOutput := runTerminalTmuxOutputCommand
	runTerminalTmuxOutputCommand = func(ctx context.Context, args ...string) (string, error) {
		called = true
		return "0", nil
	}
	defer func() { runTerminalTmuxOutputCommand = oldRunOutput }()

	recorder.ResetForResize(context.Background(), "mlp-missing-session")
	if called {
		t.Fatalf("ResetForResize must not query tmux for an unknown session")
	}
}
