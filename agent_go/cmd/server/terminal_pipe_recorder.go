package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"mcp-agent-builder-go/agent_go/internal/terminals"
)

const terminalPipeRecorderMaxBytes = 4 * 1024 * 1024
const terminalPipeRecorderTrimBytes = terminalPipeRecorderMaxBytes / 2
const terminalPipeRecorderTrimInterval = 15 * time.Second

type terminalPipeRecorder struct {
	mu       sync.Mutex
	root     string
	sessions map[string]*terminalPipeRecording
}

type terminalPipeRecording struct {
	tmuxSession string
	path        string
	started     bool
	starting    bool
	trimming    bool
	lastErr     string
}

func newTerminalPipeRecorder() *terminalPipeRecorder {
	if !terminalPipeRecorderEnabled() {
		return nil
	}
	root := terminalPipeRecorderRoot()
	if err := os.MkdirAll(root, 0o700); err != nil {
		log.Printf("Terminal pipe recorder disabled; cannot create %s: %v", root, err)
		return nil
	}
	return &terminalPipeRecorder{
		root:     root,
		sessions: make(map[string]*terminalPipeRecording),
	}
}

func terminalPipeRecorderEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("RUNLOOP_TERMINAL_PIPE_RECORDER"))) {
	case "0", "false", "no", "off", "disabled":
		return false
	default:
		return true
	}
}

func terminalPipeRecorderRoot() string {
	if raw := strings.TrimSpace(os.Getenv("RUNLOOP_TERMINAL_PIPE_DIR")); raw != "" {
		return raw
	}
	if cacheDir, err := os.UserCacheDir(); err == nil && strings.TrimSpace(cacheDir) != "" {
		return filepath.Join(cacheDir, "Runloop", "terminal-pipes")
	}
	return filepath.Join(os.TempDir(), "runloop-terminal-pipes")
}

func (r *terminalPipeRecorder) ObserveSnapshots(snapshots []terminals.Snapshot) {
	if r == nil {
		return
	}
	for _, snapshot := range snapshots {
		if strings.TrimSpace(snapshot.TmuxSession) == "" || !snapshot.Active {
			continue
		}
		r.ensureAsync(snapshot.TmuxSession)
	}
}

func (r *terminalPipeRecorder) ensureAsync(tmuxSession string) {
	tmuxSession = strings.TrimSpace(tmuxSession)
	if r == nil || tmuxSession == "" {
		return
	}
	rec, shouldStart := r.markStarting(tmuxSession)
	if !shouldStart {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), terminalTmuxActionTimeout)
		defer cancel()
		err := r.start(ctx, rec, false)
		r.markStarted(tmuxSession, err)
	}()
}

func (r *terminalPipeRecorder) markStarting(tmuxSession string) (*terminalPipeRecording, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec := r.sessions[tmuxSession]
	if rec == nil {
		rec = &terminalPipeRecording{
			tmuxSession: tmuxSession,
			path:        filepath.Join(r.root, terminalPipeRecorderFileName(tmuxSession)),
		}
		r.sessions[tmuxSession] = rec
	}
	if rec.started || rec.starting {
		return rec, false
	}
	rec.starting = true
	return rec, true
}

func (r *terminalPipeRecorder) markStarted(tmuxSession string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec := r.sessions[tmuxSession]
	if rec == nil {
		return
	}
	rec.starting = false
	if err != nil {
		rec.lastErr = err.Error()
		return
	}
	rec.started = true
	rec.lastErr = ""
	if !rec.trimming {
		rec.trimming = true
		go r.trimLoop(tmuxSession, rec.path)
	}
}

// start attaches the tmux pipe-pane recorder for a session. seedHistory is kept
// for call-site compatibility but no longer selects a text seed: the log is now
// seeded with a screen-mode-aware control prologue (see below) regardless.
func (r *terminalPipeRecorder) start(ctx context.Context, rec *terminalPipeRecording, seedHistory bool) error {
	_ = seedHistory
	if rec == nil {
		return fmt.Errorf("terminal recording is required")
	}
	if err := os.MkdirAll(filepath.Dir(rec.path), 0o700); err != nil {
		return err
	}
	// Seed the pipe log with a clean, screen-mode-aware control prologue instead
	// of a flattened capture-pane TEXT snapshot. pipe-pane attaches AFTER a TUI
	// has already entered the alternate screen, so the byte stream carries partial
	// in-place absolute-cursor redraws but no alt-screen-enter and no initial
	// clear. The old text seed (rendered text, no control codes, bare \n) pre-
	// filled the xterm's NORMAL buffer with cells the CLI never re-addresses, so
	// its in-place redraws left stale fragments behind — the "litter". The
	// prologue gives xterm a clean slate in the SAME mode the pane is really in;
	// the CLI's own bytes then paint onto it.
	prologue := terminalPipeRecorderPrologue(ctx, rec.tmuxSession)
	if err := os.WriteFile(rec.path, []byte(prologue), 0o600); err != nil {
		return err
	}
	command := "cat >> " + shellSingleQuote(rec.path)
	if err := runTerminalTmuxCommand(ctx, "", "pipe-pane", "-t", rec.tmuxSession, command); err != nil {
		return fmt.Errorf("start tmux pipe-pane recorder: %w", err)
	}
	// Now that pipe-pane is attached, force one full repaint so a complete first
	// frame arrives as the CLI's OWN bytes on the clean prologue slate — rather
	// than waiting for the next incidental redraw, which could otherwise leave an
	// alt-screen pane blank until the next activity.
	forceTerminalPaneRepaint(ctx, rec.tmuxSession)
	return nil
}

// ResetForResize truncates the pipe log on a geometry change and re-seeds it with
// a fresh, screen-mode-aware prologue, then forces a repaint. Without this the
// log would still hold frames captured at the OLD width; replayed concatenated
// with the new width's frames they land at the wrong rows and pile up as litter.
// Call it AFTER tmux has resized the window so the forced repaint is captured at
// the new geometry. No-op when the session has no live recording.
func (r *terminalPipeRecorder) ResetForResize(ctx context.Context, tmuxSession string) {
	if r == nil {
		return
	}
	tmuxSession = strings.TrimSpace(tmuxSession)
	if tmuxSession == "" {
		return
	}
	rec, ok := r.recording(tmuxSession)
	if !ok || rec == nil {
		return
	}
	prologue := terminalPipeRecorderPrologue(ctx, tmuxSession)
	if err := os.WriteFile(rec.path, []byte(prologue), 0o600); err != nil {
		log.Printf("Terminal pipe recorder resize reset failed for %s: %v", tmuxSession, err)
		return
	}
	forceTerminalPaneRepaint(ctx, tmuxSession)
}

const (
	// terminalPipeAltScreenPrologue enters the alternate screen, clears it and
	// homes the cursor — the clean slate a full-screen TUI (claudecode, codex,
	// gemini, cursor, pi) needs before its absolute-cursor redraws make sense.
	terminalPipeAltScreenPrologue = "\x1b[?1049h\x1b[2J\x1b[H"
	// terminalPipeNormalScreenPrologue is a full terminal reset (RIS) for a
	// normal-buffer / line-based command. It clears the emulator WITHOUT entering
	// the alternate screen, so a plain CLI is never wrongly trapped on an alt
	// buffer. It is also the safe fallback when the screen mode is unknown.
	terminalPipeNormalScreenPrologue = "\x1bc"
)

// terminalPipeRecorderPrologue returns the control prologue to seed the pipe log
// with, matched to the pane's REAL screen mode. Forcing the alternate screen on a
// normal-buffer CLI (or vice versa) is exactly the failure we must avoid, so when
// the state cannot be determined we fall back to the safest option (RIS) rather
// than guessing alt-screen.
func terminalPipeRecorderPrologue(ctx context.Context, tmuxSession string) string {
	if altOn, ok := terminalPaneAlternateScreenOn(ctx, tmuxSession); ok && altOn {
		return terminalPipeAltScreenPrologue
	}
	return terminalPipeNormalScreenPrologue
}

// terminalPaneAlternateScreenOn reports whether the tmux pane is currently
// showing the alternate screen (#{alternate_on} == 1). The second return value is
// false when tmux could not be queried or returned an unexpected value, signalling
// callers to use the safe fallback prologue.
func terminalPaneAlternateScreenOn(ctx context.Context, tmuxSession string) (bool, bool) {
	tmuxSession = strings.TrimSpace(tmuxSession)
	if tmuxSession == "" {
		return false, false
	}
	out, err := runTerminalTmuxOutputCommand(ctx, "display-message", "-p", "-t", tmuxSession, "#{alternate_on}")
	if err != nil {
		return false, false
	}
	switch strings.TrimSpace(out) {
	case "1":
		return true, true
	case "0":
		return false, true
	default:
		return false, false
	}
}

// forceTerminalPaneRepaint nudges a full-screen TUI into emitting a fresh, full
// frame by briefly toggling the tmux window height by one row and restoring it. A
// redraw is driven by SIGWINCH, which tmux only raises on an ACTUAL size change,
// so a same-size resize is a no-op; toggling one row guarantees the signal. The
// WIDTH is left untouched so a normal-buffer CLI's line wrapping is never
// reflowed (and line-based shells simply ignore the SIGWINCH). The repaint lands
// in the pipe log as the CLI's own bytes, painting the clean prologue slate.
// Best-effort: any failure is ignored because the prologue already left a usable
// clean screen.
func forceTerminalPaneRepaint(ctx context.Context, tmuxSession string) {
	tmuxSession = strings.TrimSpace(tmuxSession)
	if tmuxSession == "" {
		return
	}
	width, height, ok := terminalWindowSize(ctx, tmuxSession)
	if !ok || width <= 0 || height <= 1 {
		return
	}
	if err := runTerminalTmuxCommand(ctx, "", "resize-window", "-t", tmuxSession, "-x", strconv.Itoa(width), "-y", strconv.Itoa(height-1)); err != nil {
		return
	}
	_ = runTerminalTmuxCommand(ctx, "", "resize-window", "-t", tmuxSession, "-x", strconv.Itoa(width), "-y", strconv.Itoa(height))
}

// terminalWindowSize returns the tmux WINDOW dimensions. We deliberately read the
// window (not the pane, whose height excludes any status line) so the height
// toggle in forceTerminalPaneRepaint restores the exact original geometry.
func terminalWindowSize(ctx context.Context, tmuxSession string) (int, int, bool) {
	tmuxSession = strings.TrimSpace(tmuxSession)
	if tmuxSession == "" {
		return 0, 0, false
	}
	out, err := runTerminalTmuxOutputCommand(ctx, "display-message", "-p", "-t", tmuxSession, "#{window_width}\t#{window_height}")
	if err != nil {
		return 0, 0, false
	}
	parts := strings.Split(strings.TrimSpace(out), "\t")
	if len(parts) != 2 {
		return 0, 0, false
	}
	width, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errW != nil || errH != nil {
		return 0, 0, false
	}
	return width, height, true
}

func (r *terminalPipeRecorder) Content(ctx context.Context, snapshot terminals.Snapshot, seedHistory bool) (string, terminalPaneCaptureStats, bool, error) {
	stats := terminalPaneCaptureStats{RequestedLines: terminalDefaultRefreshLines, ContentSource: "tmux_pipe"}
	if r == nil {
		return "", stats, false, nil
	}
	tmuxSession := strings.TrimSpace(snapshot.TmuxSession)
	if tmuxSession == "" {
		return "", stats, false, nil
	}
	rec, ok := r.recording(tmuxSession)
	if !ok {
		var shouldStart bool
		rec, shouldStart = r.markStarting(tmuxSession)
		if !shouldStart {
			return "", stats, false, nil
		}
		if err := r.start(ctx, rec, seedHistory || !snapshot.Active); err != nil {
			r.markStarted(tmuxSession, err)
			return "", stats, false, err
		}
		r.markStarted(tmuxSession, nil)
	}
	start := time.Now()
	content, err := r.readRecording(rec.path)
	stats.Duration = time.Since(start)
	if err != nil {
		return "", stats, false, err
	}
	if strings.TrimSpace(content) == "" {
		return "", stats, false, nil
	}
	stats.RawBytes = len(content)
	stats.RawLines = terminalLineCount(content)
	stats.CollapsedBytes = stats.RawBytes
	stats.CollapsedLines = stats.RawLines
	return content, stats, true, nil
}

func (r *terminalPipeRecorder) recording(tmuxSession string) (*terminalPipeRecording, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec := r.sessions[tmuxSession]
	if rec == nil || !rec.started {
		return rec, false
	}
	return rec, true
}

func (r *terminalPipeRecorder) readRecording(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	data = trimTerminalPipeRecorderData(path, data)
	return redactedTerminalPipeRecorderContent(path, data), nil
}

func (r *terminalPipeRecorder) trimLoop(tmuxSession, path string) {
	ticker := time.NewTicker(terminalPipeRecorderTrimInterval)
	defer ticker.Stop()
	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), terminalTmuxActionTimeout)
		err := runTerminalTmuxCommand(ctx, "", "has-session", "-t", tmuxSession)
		cancel()
		if err != nil {
			return
		}
		_ = trimTerminalPipeRecorderFile(path)
	}
}

func trimTerminalPipeRecorderFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() <= terminalPipeRecorderMaxBytes {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	trimmed := trimTerminalPipeRecorderData(path, data)
	if len(trimmed) == len(data) {
		return nil
	}
	return os.WriteFile(path, trimmed, 0o600)
}

func trimTerminalPipeRecorderData(path string, data []byte) []byte {
	if len(data) <= terminalPipeRecorderMaxBytes {
		return data
	}
	data = data[len(data)-terminalPipeRecorderTrimBytes:]
	data = trimToTerminalSequenceBoundary(data)
	if len(data) > 0 {
		_ = os.WriteFile(path, data, 0o600)
	}
	return data
}

func redactedTerminalPipeRecorderContent(path string, data []byte) string {
	content := string(data)
	redacted := terminals.RedactSensitiveTerminalText(content)
	if redacted != content {
		_ = os.WriteFile(path, []byte(redacted), 0o600)
	}
	return redacted
}

func terminalPipeRecorderFileName(tmuxSession string) string {
	sum := sha256.Sum256([]byte(tmuxSession))
	return "tmux-" + hex.EncodeToString(sum[:12]) + ".log"
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func trimToTerminalSequenceBoundary(data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	for i, b := range data {
		if b == '\n' || b == '\r' {
			return data[i+1:]
		}
	}
	return data
}

func terminalPipeRecorderDebugHeaders(w http.ResponseWriter, stats terminalPaneCaptureStats) {
	w.Header().Set("X-Runloop-Terminal-Content-Source", stats.ContentSource)
	w.Header().Set("X-Runloop-Terminal-Pipe-Bytes", strconv.Itoa(stats.RawBytes))
	w.Header().Set("X-Runloop-Terminal-Pipe-Lines", strconv.Itoa(stats.RawLines))
}
