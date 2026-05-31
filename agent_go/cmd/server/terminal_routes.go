package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gorilla/mux"
	llmproviders "github.com/manishiitg/multi-llm-provider-go"

	"mcp-agent-builder-go/agent_go/internal/terminals"
)

const listTerminalContentMaxBytes = 64 * 1024
const terminalTmuxActionTimeout = 5 * time.Second
const terminalDefaultRefreshLines = 2000
const terminalDefaultDetailHistoryLines = 10000
const terminalMaxCaptureLines = 20000

var runTerminalTmuxCommand = func(ctx context.Context, stdin string, args ...string) error {
	cmd := exec.CommandContext(ctx, "tmux", args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return fmt.Errorf("tmux %s: %w", strings.Join(args, " "), err)
		}
		return fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, message)
	}
	return nil
}

var runTerminalTmuxOutputCommand = func(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "tmux", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return "", fmt.Errorf("tmux %s: %w", strings.Join(args, " "), err)
		}
		return "", fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, message)
	}
	return string(output), nil
}

var forceCompleteCodingAgentTmuxSession = llmproviders.ForceCompleteCodingAgentTmuxSession

type listTerminalsResponse struct {
	Terminals []terminals.Snapshot `json:"terminals"`
	Total     int                  `json:"total"`
}

type sendTerminalInputRequest struct {
	Text   string `json:"text"`
	Submit bool   `json:"submit,omitempty"`
}

type sendTerminalKeyRequest struct {
	Key string `json:"key"`
}

type resizeTerminalRequest struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

type terminalActionResponse struct {
	OK       bool               `json:"ok"`
	Terminal terminals.Snapshot `json:"terminal,omitempty"`
}

// handleListTerminals returns current view-only terminal snapshots.
// GET /api/terminals?session_id=<session>
func (api *StreamingAPI) handleListTerminals(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if api.terminalStore == nil {
		_ = json.NewEncoder(w).Encode(listTerminalsResponse{Terminals: []terminals.Snapshot{}})
		return
	}

	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	contentMode := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("content")))
	snapshots := api.terminalStore.List(sessionID)
	planTypes := newTerminalPlanTypeResolver(r.Context())
	filtered := make([]terminals.Snapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot.SessionID == "" {
			continue
		}
		if api.canAccessTerminalSession(r, snapshot.SessionID) {
			filtered = append(filtered, api.terminalSnapshotForList(r.Context(), planTypes, snapshot, contentMode))
		}
	}

	_ = json.NewEncoder(w).Encode(listTerminalsResponse{
		Terminals: filtered,
		Total:     len(filtered),
	})
}

// handleGetTerminal returns one current view-only terminal snapshot.
// GET /api/terminals/{terminal_id}
func (api *StreamingAPI) handleGetTerminal(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if api.terminalStore == nil {
		http.Error(w, "Terminal not found", http.StatusNotFound)
		return
	}

	terminalID := strings.TrimSpace(mux.Vars(r)["terminal_id"])
	if terminalID == "" {
		http.Error(w, "Terminal ID is required", http.StatusBadRequest)
		return
	}
	snapshot, ok := api.terminalStore.Get(terminalID)
	if !ok || !api.canAccessTerminalSession(r, snapshot.SessionID) {
		http.Error(w, "Terminal not found", http.StatusNotFound)
		return
	}
	// Capture live tmux content when: explicitly requested via content=deep/tmux,
	// OR when the terminal is inactive (active=false) and has a tmux session.
	// Inactive tmux terminals receive no event-stream updates, so every GET acts
	// as a lightweight refresh — if the pane content changed (e.g. after Claude
	// Code context compaction), ChunkIndex increments, the list-poll returns the
	// new value, and the frontend re-fetches automatically.
	shouldCaptureTmux := shouldCaptureTerminalPaneForDetail(snapshot, r)
	if shouldCaptureTmux {
		lines := terminalCaptureLinesFromRequest(r, terminalDefaultDetailHistoryLines)
		ctx, cancel := context.WithTimeout(r.Context(), terminalTmuxActionTimeout)
		content, err := captureTerminalPaneLines(ctx, snapshot.TmuxSession, lines)
		cancel()
		if err == nil {
			if refreshed, ok := api.terminalStore.RefreshContent(snapshot.TerminalID, content); ok {
				snapshot = refreshed
			}
		} else if isMissingTmuxTargetError(err) && !snapshot.Active {
			// The backing tmux session is gone and no lifecycle event will
			// arrive to close it. Mark the snapshot stale so the frontend's
			// inactive-terminal probe stops capturing a dead session every 3s.
			if stale, ok := api.terminalStore.MarkStale(snapshot.TerminalID); ok {
				snapshot = stale
			}
		}
	}

	_ = json.NewEncoder(w).Encode(api.enrichTerminalSnapshot(r.Context(), newTerminalPlanTypeResolver(r.Context()), snapshot))
}

func shouldCaptureTerminalPaneForDetail(snapshot terminals.Snapshot, r *http.Request) bool {
	if strings.TrimSpace(snapshot.TmuxSession) == "" {
		return false
	}
	if wantsDeepTerminalContent(r) || !snapshot.Active {
		return true
	}
	return terminalSnapshotHasPromptCompletionFallback(snapshot.Content)
}

func terminalSnapshotHasPromptCompletionFallback(content string) bool {
	lower := strings.ToLower(content)
	return strings.Contains(lower, "status: completed") ||
		strings.Contains(lower, "status: complete")
}

// handleDismissTerminal removes one terminal snapshot from the UI.
// DELETE /api/terminals/{terminal_id}
func (api *StreamingAPI) handleDismissTerminal(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if api.terminalStore == nil {
		http.Error(w, "Terminal not found", http.StatusNotFound)
		return
	}

	terminalID := strings.TrimSpace(mux.Vars(r)["terminal_id"])
	if terminalID == "" {
		http.Error(w, "Terminal ID is required", http.StatusBadRequest)
		return
	}
	snapshot, ok := api.terminalStore.Get(terminalID)
	if !ok || !api.canAccessTerminalSession(r, snapshot.SessionID) {
		http.Error(w, "Terminal not found", http.StatusNotFound)
		return
	}
	if !api.terminalStore.Dismiss(terminalID) {
		http.Error(w, "Terminal not found", http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]bool{"dismissed": true})
}

// handleCompleteTerminal marks one terminal snapshot complete and, when the
// backing provider wait loop is still active, asks it to return through the
// normal execution path. The workflow controller then runs validation,
// learning/KB hooks, progress updates, and auto-notification itself.
// POST /api/terminals/{terminal_id}/complete
func (api *StreamingAPI) handleCompleteTerminal(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	before, ok := api.requireAccessibleTerminal(w, r)
	if !ok {
		return
	}
	snapshot, ok := api.terminalStore.MarkCompleted(before.TerminalID)
	if !ok {
		http.Error(w, "Terminal not found", http.StatusNotFound)
		return
	}
	if before.Active && strings.TrimSpace(before.TmuxSession) != "" {
		forceCompleteCodingAgentTmuxSession(before.TmuxSession)
	}
	_ = json.NewEncoder(w).Encode(terminalActionResponse{OK: true, Terminal: api.enrichTerminalSnapshot(r.Context(), newTerminalPlanTypeResolver(r.Context()), snapshot)})
}

// handleFailTerminal marks one view-only terminal snapshot failed.
// POST /api/terminals/{terminal_id}/fail
func (api *StreamingAPI) handleFailTerminal(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	before, ok := api.requireAccessibleTerminal(w, r)
	if !ok {
		return
	}
	snapshot, ok := api.terminalStore.MarkFailed(before.TerminalID)
	if !ok {
		http.Error(w, "Terminal not found", http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(terminalActionResponse{OK: true, Terminal: api.enrichTerminalSnapshot(r.Context(), newTerminalPlanTypeResolver(r.Context()), snapshot)})
}

// handleRefreshTerminal captures the current tmux pane and updates the snapshot.
// POST /api/terminals/{terminal_id}/refresh
func (api *StreamingAPI) handleRefreshTerminal(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	snapshot, ok := api.requireAccessibleTerminal(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(snapshot.TmuxSession) == "" {
		http.Error(w, "Terminal has no tmux session", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), terminalTmuxActionTimeout)
	defer cancel()
	lines := terminalCaptureLinesFromRequest(r, terminalDefaultRefreshLines)
	content, err := captureTerminalPaneLines(ctx, snapshot.TmuxSession, lines)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	updated, ok := api.terminalStore.RefreshContent(snapshot.TerminalID, content)
	if !ok {
		http.Error(w, "Terminal not found", http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(terminalActionResponse{OK: true, Terminal: api.enrichTerminalSnapshot(r.Context(), newTerminalPlanTypeResolver(r.Context()), updated)})
}

// handleKillTerminal kills the backing tmux session and marks the UI snapshot failed.
// POST /api/terminals/{terminal_id}/kill
func (api *StreamingAPI) handleKillTerminal(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	snapshot, ok := api.requireAccessibleTerminal(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(snapshot.TmuxSession) == "" {
		http.Error(w, "Terminal has no tmux session", http.StatusBadRequest)
		return
	}
	if !snapshot.Active {
		_ = json.NewEncoder(w).Encode(terminalActionResponse{OK: true, Terminal: api.enrichTerminalSnapshot(r.Context(), newTerminalPlanTypeResolver(r.Context()), snapshot)})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), terminalTmuxActionTimeout)
	defer cancel()
	if err := runTerminalTmuxCommand(ctx, "", "kill-session", "-t", snapshot.TmuxSession); err != nil {
		if !isMissingTmuxTargetError(err) {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
	}
	updated, ok := api.terminalStore.MarkFailed(snapshot.TerminalID)
	if !ok {
		http.Error(w, "Terminal not found", http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(terminalActionResponse{OK: true, Terminal: api.enrichTerminalSnapshot(r.Context(), newTerminalPlanTypeResolver(r.Context()), updated)})
}

// handleSendTerminalInput pastes text into the terminal's tmux pane.
// POST /api/terminals/{terminal_id}/input
func (api *StreamingAPI) handleSendTerminalInput(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	snapshot, ok := api.requireAccessibleTerminal(w, r)
	if !ok {
		return
	}

	var req sendTerminalInputRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid terminal input request", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Text) == "" && !req.Submit {
		http.Error(w, "Text or submit=true is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(snapshot.TmuxSession) == "" {
		http.Error(w, "Terminal has no tmux session", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), terminalTmuxActionTimeout)
	defer cancel()
	if req.Text != "" {
		if err := pasteTerminalText(ctx, snapshot.TmuxSession, req.Text); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
	}
	if req.Submit {
		if err := sendTerminalKey(ctx, snapshot.TmuxSession, "enter"); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
	}
	_ = json.NewEncoder(w).Encode(terminalActionResponse{OK: true, Terminal: api.enrichTerminalSnapshot(r.Context(), newTerminalPlanTypeResolver(r.Context()), snapshot)})
}

// handleSendTerminalKey sends a small allowlisted key to the terminal's tmux pane.
// POST /api/terminals/{terminal_id}/key
func (api *StreamingAPI) handleSendTerminalKey(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	snapshot, ok := api.requireAccessibleTerminal(w, r)
	if !ok {
		return
	}

	var req sendTerminalKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid terminal key request", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(snapshot.TmuxSession) == "" {
		http.Error(w, "Terminal has no tmux session", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), terminalTmuxActionTimeout)
	defer cancel()
	if err := sendTerminalKey(ctx, snapshot.TmuxSession, req.Key); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = json.NewEncoder(w).Encode(terminalActionResponse{OK: true, Terminal: api.enrichTerminalSnapshot(r.Context(), newTerminalPlanTypeResolver(r.Context()), snapshot)})
}

// handleTerminalSizeHint records the operator's viewport size as the preferred
// tmux launch size WITHOUT needing an existing terminal. Called by the frontend
// at startup so the very first coding-agent session launches at the correct
// width rather than the default 120×36.
// POST /api/terminals/size-hint  body: {"cols": int, "rows": int}
func (api *StreamingAPI) handleTerminalSizeHint(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var req resizeTerminalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Cols <= 0 || req.Rows <= 0 {
		http.Error(w, "cols and rows must be positive integers", http.StatusBadRequest)
		return
	}
	llmproviders.SetCodingAgentTmuxSize(req.Cols, req.Rows)
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// handleResizeTerminal resizes the backing tmux window and records the size
// as the process-wide preferred size so subsequent coding-agent tmux launches
// match the operator's viewport.
// POST /api/terminals/{terminal_id}/resize  body: {"cols": int, "rows": int}
func (api *StreamingAPI) handleResizeTerminal(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	snapshot, ok := api.requireAccessibleTerminal(w, r)
	if !ok {
		return
	}

	var req resizeTerminalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid resize request", http.StatusBadRequest)
		return
	}
	if req.Cols <= 0 || req.Rows <= 0 {
		http.Error(w, "cols and rows must be positive", http.StatusBadRequest)
		return
	}
	// Always update the preferred size so newly-launched sessions adopt the
	// operator's viewport even if THIS terminal has no live tmux window.
	llmproviders.SetCodingAgentTmuxSize(req.Cols, req.Rows)

	if strings.TrimSpace(snapshot.TmuxSession) == "" {
		// No live pane to resize, but the preferred size was still recorded.
		_ = json.NewEncoder(w).Encode(terminalActionResponse{OK: true, Terminal: api.enrichTerminalSnapshot(r.Context(), newTerminalPlanTypeResolver(r.Context()), snapshot)})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), terminalTmuxActionTimeout)
	defer cancel()
	if err := runTerminalTmuxCommand(ctx, "", "resize-window", "-t", snapshot.TmuxSession, "-x", strconv.Itoa(req.Cols), "-y", strconv.Itoa(req.Rows)); err != nil {
		// If the backing tmux session is gone, mark the terminal stale (which
		// also clears TmuxSession) and report success — the preferred-size
		// update already landed, and there is no live pane to resize. Without
		// this branch, every subsequent frontend resize POST returns 502 until
		// the next pane refresh re-detects staleness.
		if isMissingTmuxTargetError(err) {
			if stale, ok := api.terminalStore.MarkStale(snapshot.TerminalID); ok {
				snapshot = stale
			}
			_ = json.NewEncoder(w).Encode(terminalActionResponse{OK: true, Terminal: api.enrichTerminalSnapshot(r.Context(), newTerminalPlanTypeResolver(r.Context()), snapshot)})
			return
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	_ = json.NewEncoder(w).Encode(terminalActionResponse{OK: true, Terminal: api.enrichTerminalSnapshot(r.Context(), newTerminalPlanTypeResolver(r.Context()), snapshot)})
}

func (api *StreamingAPI) requireAccessibleTerminal(w http.ResponseWriter, r *http.Request) (terminals.Snapshot, bool) {
	if api.terminalStore == nil {
		http.Error(w, "Terminal not found", http.StatusNotFound)
		return terminals.Snapshot{}, false
	}
	terminalID := strings.TrimSpace(mux.Vars(r)["terminal_id"])
	if terminalID == "" {
		http.Error(w, "Terminal ID is required", http.StatusBadRequest)
		return terminals.Snapshot{}, false
	}
	snapshot, ok := api.terminalStore.Get(terminalID)
	if !ok || !api.canAccessTerminalSession(r, snapshot.SessionID) {
		http.Error(w, "Terminal not found", http.StatusNotFound)
		return terminals.Snapshot{}, false
	}
	return snapshot, true
}

func pasteTerminalText(ctx context.Context, tmuxSession, text string) error {
	tmuxSession = strings.TrimSpace(tmuxSession)
	if tmuxSession == "" {
		return fmt.Errorf("tmux session is required")
	}
	bufferName := fmt.Sprintf("mcp-terminal-ui-%d", time.Now().UnixNano())
	if err := runTerminalTmuxCommand(ctx, text, "load-buffer", "-b", bufferName, "-"); err != nil {
		return fmt.Errorf("failed to load terminal input into tmux buffer: %w", err)
	}
	if err := runTerminalTmuxCommand(ctx, "", "paste-buffer", "-d", "-p", "-r", "-b", bufferName, "-t", tmuxSession); err != nil {
		return fmt.Errorf("failed to paste terminal input into tmux session: %w", err)
	}
	return nil
}

func captureTerminalPane(ctx context.Context, tmuxSession string) (string, error) {
	return captureTerminalPaneLines(ctx, tmuxSession, terminalDefaultRefreshLines)
}

func captureTerminalPaneLines(ctx context.Context, tmuxSession string, lines int) (string, error) {
	tmuxSession = strings.TrimSpace(tmuxSession)
	if tmuxSession == "" {
		return "", fmt.Errorf("tmux session is required")
	}
	if lines <= 0 {
		lines = terminalDefaultRefreshLines
	}
	if lines > terminalMaxCaptureLines {
		lines = terminalMaxCaptureLines
	}
	// -e preserves ANSI SGR (color, bold, dim, …) so the frontend can colorize
	// the snapshot via ansi_up. Mirrors the captureXPaneForDisplay flag used
	// by the live-stream path in multi-llm-provider-go; without it, terminal
	// refresh / completion fetches would silently strip color even though the
	// running session was emitting it.
	//
	// -J rejoins lines that tmux hard-wrapped at the pane width into a single
	// logical line. The web pane is a whitespace-pre-wrap <pre> whose width
	// rarely equals the tmux pane width, so without -J a long line gets
	// double-wrapped (once by tmux, again by the browser) and shows a ragged
	// right edge. -J only joins genuinely wrapped continuations — TUI box
	// borders, which end at the pane edge deliberately, are left intact.
	content, err := runTerminalTmuxOutputCommand(ctx, "capture-pane", "-p", "-e", "-J", "-t", tmuxSession, "-S", fmt.Sprintf("-%d", lines))
	if err != nil {
		return "", fmt.Errorf("failed to capture terminal pane: %w", err)
	}
	return collapseBlankRuns(content), nil
}

// terminalMaxConsecutiveBlankLines caps how many blank rows survive a collapse.
// Agy and other CLIs use cursor positioning to repaint loading spinners
// ("Generating...") in place; with `capture-pane -e`, every frame leaves its
// current pane state in scrollback — typically the spinner row followed by ~25
// empty rows of pane area — so the runs must be capped or the snapshot becomes
// a near-empty scroll. But the TUIs deliberately separate sections with 2–3
// blank rows; capping at 1 stacked those sections directly together and made
// the re-captured (inactive/suspended) pane hard to read. Keeping up to 2
// preserves that separation while still squashing the spinner gaps. Must match
// paneview.CollapseBlankRuns so the active stream and the re-captured snapshot
// render with identical spacing.
const terminalMaxConsecutiveBlankLines = 2

// collapseBlankRuns squeezes any run of blank/whitespace-only lines down to at
// most terminalMaxConsecutiveBlankLines and trims trailing whitespace from each
// line. It also deduplicates consecutive Braille-spinner lines (⠀–⣿), keeping
// only the last frame so the animated "Generating…" indicator doesn't stack up
// as dozens of separate lines in the captured scrollback.
func collapseBlankRuns(s string) string {
	if s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	lines = stripInputBoxTrailerLines(lines)
	out := make([]string, 0, len(lines))
	blankRun := 0
	for i, line := range lines {
		trimmed := strings.TrimRight(line, " \t\r")
		if strings.TrimSpace(trimmed) == "" {
			blankRun++
			if blankRun <= terminalMaxConsecutiveBlankLines {
				out = append(out, "")
			}
			continue
		}
		blankRun = 0
		// Skip Braille-spinner lines that are immediately followed by another
		// Braille-spinner line — only the last frame in a run is kept.
		if isTerminalSpinnerLine(trimmed) {
			next := i + 1
			for next < len(lines) && strings.TrimSpace(lines[next]) == "" {
				next++
			}
			if next < len(lines) && isTerminalSpinnerLine(strings.TrimRight(lines[next], " \t\r")) {
				continue // earlier frame — skip
			}
		}
		out = append(out, trimmed)
	}
	return strings.Join(out, "\n")
}

// isTerminalSpinnerLine returns true when a line begins with a Braille block
// character (U+2800–U+28FF), which CLIs like agy use for spinner animations.
func isTerminalSpinnerLine(line string) bool {
	r, _ := utf8.DecodeRuneInString(line)
	return r >= 0x2800 && r <= 0x28FF
}

// stripInputBoxTrailerLines removes the agy input-box region and everything
// below it (animation cursor-positioning artifacts like "oa", "ad", "di").
// The input box is a ─── top border + › prompt + ─── bottom border; we strip
// from the top border onward.
func stripInputBoxTrailerLines(lines []string) []string {
	lastBorderIdx := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) >= 20 && isAllDashRunes(trimmed) {
			lastBorderIdx = i
		}
	}
	if lastBorderIdx < 0 {
		return lines
	}
	// Walk back to find the top border (the ─── line above the › prompt).
	topBorderIdx := -1
	for i := lastBorderIdx - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if len(trimmed) >= 20 && isAllDashRunes(trimmed) {
			topBorderIdx = i
			break
		}
		if trimmed != "" && !strings.HasPrefix(trimmed, ">") {
			break
		}
	}
	cutAt := lastBorderIdx + 1
	if topBorderIdx >= 0 {
		cutAt = topBorderIdx
	}
	return lines[:cutAt]
}

func isAllDashRunes(s string) bool {
	for _, r := range s {
		if r != '─' && r != '-' && r != '━' {
			return false
		}
	}
	return true
}

func wantsDeepTerminalContent(r *http.Request) bool {
	contentMode := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("content")))
	refresh := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("refresh")))
	return contentMode == "deep" || contentMode == "tmux" || refresh == "true" || refresh == "1"
}

func terminalCaptureLinesFromRequest(r *http.Request, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get("lines"))
	if raw == "" {
		return fallback
	}
	lines, err := strconv.Atoi(raw)
	if err != nil || lines <= 0 {
		return fallback
	}
	if lines > terminalMaxCaptureLines {
		return terminalMaxCaptureLines
	}
	return lines
}

func sendTerminalKey(ctx context.Context, tmuxSession, key string) error {
	tmuxSession = strings.TrimSpace(tmuxSession)
	if tmuxSession == "" {
		return fmt.Errorf("tmux session is required")
	}
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "enter", "return":
		return runTerminalTmuxCommand(ctx, "", "send-keys", "-t", tmuxSession, "C-m")
	case "esc", "escape":
		return runTerminalTmuxCommand(ctx, "", "send-keys", "-t", tmuxSession, "Escape")
	case "ctrl-c", "ctrl_c", "interrupt", "cancel":
		return runTerminalTmuxCommand(ctx, "", "send-keys", "-t", tmuxSession, "C-c")
	case "ctrl-o", "ctrl_o", "expand":
		return runTerminalTmuxCommand(ctx, "", "send-keys", "-t", tmuxSession, "C-o")
	case "tab":
		// e.g. allowlist a gated MCP-tool prompt, accept a menu selection.
		return runTerminalTmuxCommand(ctx, "", "send-keys", "-t", tmuxSession, "Tab")
	case "up", "arrow-up", "arrow_up":
		return runTerminalTmuxCommand(ctx, "", "send-keys", "-t", tmuxSession, "Up")
	case "down", "arrow-down", "arrow_down":
		return runTerminalTmuxCommand(ctx, "", "send-keys", "-t", tmuxSession, "Down")
	default:
		return fmt.Errorf("unsupported terminal key %q", key)
	}
}

func isMissingTmuxTargetError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "can't find pane") ||
		strings.Contains(message, "can't find session") ||
		strings.Contains(message, "no server running")
}

func (api *StreamingAPI) enrichTerminalSnapshot(ctx context.Context, planTypes *terminalPlanTypeResolver, snapshot terminals.Snapshot) terminals.Snapshot {
	active, exists := api.getActiveSession(snapshot.SessionID)
	if !exists || active == nil {
		enrichedSnapshot := snapshot.WithContext(terminals.Context{})
		enrichedSnapshot = withTerminalPlanStepType(ctx, planTypes, enrichedSnapshot)
		return withTerminalRows(enrichedSnapshot.WithContext(terminals.Context{}))
	}
	enriched := api.buildActiveSessionInfoSummary(active)
	if enriched == nil {
		enrichedSnapshot := snapshot.WithContext(terminals.Context{})
		enrichedSnapshot = withTerminalPlanStepType(ctx, planTypes, enrichedSnapshot)
		return withTerminalRows(enrichedSnapshot.WithContext(terminals.Context{}))
	}
	enrichedSnapshot := snapshot.WithContext(terminals.Context{
		WorkflowName:  enriched.WorkflowName,
		WorkflowLabel: enriched.WorkflowLabel,
		WorkspacePath: enriched.WorkspacePath,
		ExecutionName: enriched.CurrentExecutionName,
	})
	enrichedSnapshot = withTerminalPlanStepType(ctx, planTypes, enrichedSnapshot)
	return withTerminalRows(enrichedSnapshot.WithContext(terminals.Context{}))
}

func (api *StreamingAPI) terminalSnapshotForList(ctx context.Context, planTypes *terminalPlanTypeResolver, snapshot terminals.Snapshot, contentMode string) terminals.Snapshot {
	if isMetadataOnlyTerminalList(contentMode) {
		return compactTerminalSnapshotForList(api.enrichTerminalSnapshotMetadata(ctx, planTypes, snapshot), contentMode)
	}
	return compactTerminalSnapshotForList(api.enrichTerminalSnapshot(ctx, planTypes, snapshot), contentMode)
}

func (api *StreamingAPI) enrichTerminalSnapshotMetadata(ctx context.Context, planTypes *terminalPlanTypeResolver, snapshot terminals.Snapshot) terminals.Snapshot {
	active, exists := api.getActiveSession(snapshot.SessionID)
	if !exists || active == nil {
		enrichedSnapshot := snapshot.WithContext(terminals.Context{})
		enrichedSnapshot = withTerminalPlanStepType(ctx, planTypes, enrichedSnapshot)
		return enrichedSnapshot.WithContext(terminals.Context{})
	}
	enriched := api.buildActiveSessionInfoSummary(active)
	if enriched == nil {
		enrichedSnapshot := snapshot.WithContext(terminals.Context{})
		enrichedSnapshot = withTerminalPlanStepType(ctx, planTypes, enrichedSnapshot)
		return enrichedSnapshot.WithContext(terminals.Context{})
	}
	enrichedSnapshot := snapshot.WithContext(terminals.Context{
		WorkflowName:  enriched.WorkflowName,
		WorkflowLabel: enriched.WorkflowLabel,
		WorkspacePath: enriched.WorkspacePath,
		ExecutionName: enriched.CurrentExecutionName,
	})
	enrichedSnapshot = withTerminalPlanStepType(ctx, planTypes, enrichedSnapshot)
	return enrichedSnapshot.WithContext(terminals.Context{})
}

type terminalPlanTypeResolver struct {
	ctx        context.Context
	byWorkflow map[string]map[string]string
}

func newTerminalPlanTypeResolver(ctx context.Context) *terminalPlanTypeResolver {
	if ctx == nil {
		ctx = context.Background()
	}
	return &terminalPlanTypeResolver{
		ctx:        ctx,
		byWorkflow: make(map[string]map[string]string),
	}
}

func withTerminalPlanStepType(ctx context.Context, resolver *terminalPlanTypeResolver, snapshot terminals.Snapshot) terminals.Snapshot {
	if strings.TrimSpace(snapshot.StepType) != "" {
		return snapshot
	}
	stepID := strings.TrimSpace(snapshot.StepID)
	if stepID == "" || strings.HasPrefix(stepID, "main_agent:") {
		return snapshot
	}
	workflowPath := strings.Trim(strings.TrimSpace(snapshot.WorkflowPath), "/")
	if workflowPath == "" {
		return snapshot
	}
	if resolver == nil {
		resolver = newTerminalPlanTypeResolver(ctx)
	}
	if stepType := resolver.stepType(workflowPath, stepID); stepType != "" {
		snapshot.StepType = stepType
	}
	return snapshot
}

func (r *terminalPlanTypeResolver) stepType(workflowPath, stepID string) string {
	workflowPath = strings.Trim(strings.TrimSpace(workflowPath), "/")
	stepID = strings.TrimSpace(stepID)
	if workflowPath == "" || stepID == "" {
		return ""
	}
	if r.byWorkflow == nil {
		r.byWorkflow = make(map[string]map[string]string)
	}
	stepTypes, ok := r.byWorkflow[workflowPath]
	if !ok {
		stepTypes = loadTerminalPlanStepTypes(r.ctx, workflowPath)
		r.byWorkflow[workflowPath] = stepTypes
	}
	return stepTypes[stepID]
}

func loadTerminalPlanStepTypes(ctx context.Context, workflowPath string) map[string]string {
	stepTypes := make(map[string]string)
	content, exists, err := readFileFromWorkspace(ctx, strings.Trim(workflowPath, "/")+"/planning/plan.json")
	if err != nil || !exists || strings.TrimSpace(content) == "" {
		return stepTypes
	}
	var raw any
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return stepTypes
	}
	collectTerminalPlanStepTypes(raw, stepTypes)
	return stepTypes
}

func collectTerminalPlanStepTypes(value any, stepTypes map[string]string) {
	switch typed := value.(type) {
	case map[string]any:
		id, hasID := typed["id"].(string)
		stepType, hasType := typed["type"].(string)
		if hasID && hasType && isWorkflowPlanStepType(stepType) {
			if _, exists := stepTypes[id]; !exists {
				stepTypes[id] = stepType
			}
		}
		for _, child := range typed {
			collectTerminalPlanStepTypes(child, stepTypes)
		}
	case []any:
		for _, child := range typed {
			collectTerminalPlanStepTypes(child, stepTypes)
		}
	}
}

func isWorkflowPlanStepType(stepType string) bool {
	switch strings.TrimSpace(stepType) {
	case "regular", "human_input", "todo_task", "routing", "message_sequence":
		return true
	default:
		return false
	}
}

func (api *StreamingAPI) canAccessTerminalSession(r *http.Request, sessionID string) bool {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return false
	}

	currentUserID := GetUserIDFromContext(r.Context())
	activeSession, exists := api.getActiveSession(sessionID)
	if exists {
		return activeSession.UserID == "" || activeSession.UserID == currentUserID
	}
	if api.eventStore == nil {
		return currentUserID == GetDefaultUserID()
	}
	owner := api.eventStore.GetSessionOwner(sessionID)
	if owner != "" {
		return owner == currentUserID
	}
	return currentUserID == GetDefaultUserID()
}

func compactTerminalSnapshotForList(snapshot terminals.Snapshot, contentMode string) terminals.Snapshot {
	switch contentMode {
	case "none", "metadata":
		snapshot.Content = ""
		snapshot.Rows = []terminals.Row{}
		snapshot.Status = compactTerminalStatusForList(snapshot.Status)
	case "full":
		snapshot = withTerminalRows(snapshot)
		return snapshot
	default:
		snapshot.Content = terminalContentTail(snapshot.Content, listTerminalContentMaxBytes)
		snapshot = withTerminalRows(snapshot)
	}
	return snapshot
}

func isMetadataOnlyTerminalList(contentMode string) bool {
	return contentMode == "none" || contentMode == "metadata"
}

func compactTerminalStatusForList(status terminals.Status) terminals.Status {
	status.StatusText = truncateTerminalListString(status.StatusText, 240)
	status.AssistantPreview = truncateTerminalListString(status.AssistantPreview, 800)
	status.ToolSummary = truncateTerminalListString(status.ToolSummary, 240)
	status.ToolName = truncateTerminalListString(status.ToolName, 180)
	status.PreValidationSummary = truncateTerminalListString(status.PreValidationSummary, 800)
	return status
}

func truncateTerminalListString(value string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	if utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:maxRunes])) + "..."
}

func withTerminalRows(snapshot terminals.Snapshot) terminals.Snapshot {
	if strings.TrimSpace(snapshot.Content) == "" {
		snapshot.Rows = []terminals.Row{}
		return snapshot
	}
	if strings.TrimSpace(snapshot.TmuxSession) != "" && strings.ToLower(strings.TrimSpace(snapshot.StepTransport)) == "tmux" {
		snapshot.Rows = []terminals.Row{}
		return snapshot
	}
	if len(snapshot.Rows) > 0 {
		snapshot.Status = terminals.StatusWithRows(snapshot.Status, snapshot.Rows)
		return snapshot
	}
	snapshot.Rows = terminals.ParseRows(snapshot.Content)
	if snapshot.Rows == nil {
		snapshot.Rows = []terminals.Row{}
	}
	snapshot.Status = terminals.StatusWithRows(snapshot.Status, snapshot.Rows)
	return snapshot
}

func terminalContentTail(content string, maxBytes int) string {
	if maxBytes <= 0 || len(content) <= maxBytes {
		return content
	}

	start := len(content) - maxBytes
	for start < len(content) && !utf8.RuneStart(content[start]) {
		start++
	}

	tail := content[start:]
	if newline := strings.IndexByte(tail, '\n'); newline >= 0 && newline < 4096 {
		tail = tail[newline+1:]
	}
	return "[terminal output truncated; showing latest output]\n" + tail
}
