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

func (r *terminalPipeRecorder) start(ctx context.Context, rec *terminalPipeRecording, seedHistory bool) error {
	if rec == nil {
		return fmt.Errorf("terminal recording is required")
	}
	if err := os.MkdirAll(filepath.Dir(rec.path), 0o700); err != nil {
		return err
	}
	var seed string
	var err error
	if seedHistory {
		seed, err = captureTerminalPaneLines(ctx, rec.tmuxSession, terminalDefaultRefreshLines)
	} else {
		seed, err = captureTerminalVisiblePane(ctx, rec.tmuxSession)
	}
	if err == nil && strings.TrimSpace(seed) != "" {
		if writeErr := os.WriteFile(rec.path, []byte(strings.TrimRight(seed, "\n")+"\n"), 0o600); writeErr != nil {
			return writeErr
		}
	} else if _, statErr := os.Stat(rec.path); os.IsNotExist(statErr) {
		file, createErr := os.OpenFile(rec.path, os.O_CREATE|os.O_WRONLY, 0o600)
		if createErr != nil {
			return createErr
		}
		_ = file.Close()
	}
	command := "cat >> " + shellSingleQuote(rec.path)
	if err := runTerminalTmuxCommand(ctx, "", "pipe-pane", "-t", rec.tmuxSession, command); err != nil {
		return fmt.Errorf("start tmux pipe-pane recorder: %w", err)
	}
	return nil
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
