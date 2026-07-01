package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"

	"mcp-agent-builder-go/agent_go/internal/liveattach"
)

// Live-attach terminal transport (Phase 1, control mode).
//
// This is the output/render transport described in
// docs/refactor/terminal_live_attach_transport.md. It is the selected-terminal
// tmux transport: the legacy snapshot/replay path no longer renders selected
// live tmux panes.
//
// GET /api/terminals/{id}/stream upgrades to a WebSocket that attaches one
// `tmux -CC attach` control-mode client per tmux session, parses %output into
// the live pane byte stream, and forwards browser input / resize back to the
// session.

const (
	// liveAttachSubBuffer bounds a single viewer's pending-bytes channel. A
	// viewer that falls this far behind has bytes dropped (not the whole
	// stream blocked); the frontend re-seeds via the current-screen backfill on
	// reconnect.
	liveAttachSubBuffer = 256
	// liveAttachInitialDrainDelay gives a newly-created control-mode attach client
	// a short no-subscriber window to emit tmux's initial screen repaint. The PoC
	// had the attach loop running before the browser connected, so this first
	// repaint was naturally dropped; without the same warm-up here the browser gets
	// backfill followed by the attach repaint of the same screen.
	liveAttachInitialDrainDelay = 180 * time.Millisecond
	liveAttachInitialDrainWait  = 750 * time.Millisecond
	// liveAttachScannerInitial is the initial control-line scan buffer; it grows
	// up to liveattach.MaxControlLineBytes for large %output bursts.
	liveAttachScannerInitial = 1 << 16
	liveAttachDefaultCols    = 120
	liveAttachDefaultRows    = 36
	// Bounded history seeded into xterm's native scrollback on connect. The
	// current visible pane is still painted separately after a viewport clear,
	// so old spinner/status redraws remain scrollback-only and cannot corrupt
	// the live screen.
	liveAttachBackfillHistoryLines = 2000
	// Minimum tmux version for the control-mode attach transport. `window-size`
	// window-size was added in tmux 2.9. The live transport uses explicit
	// resize-window plus window-size manual; control mode (-CC) has existed since
	// 1.8. We pin 2.9 as the floor.
	liveAttachMinTmuxMajor = 2
	liveAttachMinTmuxMinor = 9
)

var liveAttachBackfillFn = liveAttachBackfill

// liveAttachEnabled reports whether the live-attach transport is turned on.
// Live-attach is now the only transport for the selected terminal, so this is
// always on (the RUNLOOP_TERMINAL_LIVE_ATTACH feature flag was removed).
func liveAttachEnabled() bool {
	return true
}

// newLiveAttachManagerIfEnabled always creates the manager (live-attach is the
// only transport now). The tmux-version guard is kept: if the local tmux is too
// old for control-mode attach, return nil so the endpoint stays inert / 404.
func newLiveAttachManagerIfEnabled() *liveAttachManager {
	ctx, cancel := context.WithTimeout(context.Background(), terminalTmuxActionTimeout)
	defer cancel()
	ok, version := liveAttachTmuxSupported(ctx)
	if !ok {
		log.Printf("[live-attach] tmux is unsupported (need >= %d.%d, have %q); endpoint disabled",
			liveAttachMinTmuxMajor, liveAttachMinTmuxMinor, version)
		return nil
	}
	log.Printf("[live-attach] enabled (control-mode transport); tmux %q", version)
	return newLiveAttachManager()
}

// liveAttachTmuxSupported reports whether `tmux -V` meets the minimum version.
func liveAttachTmuxSupported(ctx context.Context) (bool, string) {
	out, err := runTerminalTmuxOutputCommand(ctx, "-V")
	if err != nil {
		return false, ""
	}
	version := strings.TrimSpace(out)
	return tmuxVersionAtLeast(version, liveAttachMinTmuxMajor, liveAttachMinTmuxMinor), version
}

// tmuxVersionAtLeast parses a `tmux -V` banner ("tmux 3.6a", "tmux next-3.4",
// "tmux 2.9") and reports whether it is >= major.minor.
func tmuxVersionAtLeast(banner string, major, minor int) bool {
	v := strings.TrimSpace(banner)
	v = strings.TrimPrefix(v, "tmux")
	v = strings.TrimSpace(v)
	// Skip any leading non-numeric label such as "next-".
	if i := strings.IndexAny(v, "0123456789"); i > 0 {
		v = v[i:]
	} else if i < 0 {
		return false
	}
	maj, rest, ok := leadingInt(v)
	if !ok {
		return false
	}
	min := 0
	if strings.HasPrefix(rest, ".") {
		min, _, _ = leadingInt(rest[1:])
	}
	if maj != major {
		return maj > major
	}
	return min >= minor
}

// leadingInt reads a run of leading ASCII digits and returns the value, the
// remainder, and whether at least one digit was read.
func leadingInt(s string) (int, string, bool) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, s, false
	}
	n, err := strconv.Atoi(s[:i])
	if err != nil {
		return 0, s, false
	}
	return n, s[i:], true
}

// liveAttachManager owns one control-mode attach client per tmux session,
// shared by that session's WebSocket viewers (the product constraint is a
// single viewer per terminal, but the manager is written to tolerate more).
type liveAttachManager struct {
	mu       sync.Mutex
	sessions map[string]*liveAttachStream
}

func newLiveAttachManager() *liveAttachManager {
	return &liveAttachManager{sessions: make(map[string]*liveAttachStream)}
}

// liveAttachStream is a single `tmux -CC attach` control-mode client.
type liveAttachStream struct {
	mgr         *liveAttachManager
	tmuxSession string

	mu          sync.Mutex
	subs        map[chan []byte]struct{}
	ptmx        *os.File
	cancel      context.CancelFunc
	done        chan struct{}
	drainedOnce sync.Once
	drained     chan struct{}
	cols        int
	rows        int
	appliedCols int
	appliedRows int
}

// liveAttachAttachFn runs the real control-mode attach loop for a stream. It is
// a package var so tests can substitute a fake that does not exec tmux.
var liveAttachAttachFn = func(st *liveAttachStream, ctx context.Context) {
	st.runControlMode(ctx)
}

func (m *liveAttachManager) hasSession(tmuxSession string) bool {
	tmuxSession = strings.TrimSpace(tmuxSession)
	if tmuxSession == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.sessions[tmuxSession]
	return ok
}

// subscribe registers a viewer for a tmux session, starting the attach client
// on the first subscriber. It returns the shared stream and the viewer's byte
// channel; both are nil if the session name is empty.
func (m *liveAttachManager) subscribe(tmuxSession string, cols, rows int) (*liveAttachStream, chan []byte) {
	tmuxSession = strings.TrimSpace(tmuxSession)
	if tmuxSession == "" {
		return nil, nil
	}
	m.mu.Lock()
	st := m.sessions[tmuxSession]
	if st != nil && st.isDone() {
		delete(m.sessions, tmuxSession)
		st = nil
	}
	if st == nil {
		st = &liveAttachStream{
			mgr:         m,
			tmuxSession: tmuxSession,
			subs:        make(map[chan []byte]struct{}),
			done:        make(chan struct{}),
			drained:     make(chan struct{}),
			cols:        cols,
			rows:        rows,
		}
		m.sessions[tmuxSession] = st
		st.start()
	}
	m.mu.Unlock()

	// Apply the desired grid before adding this viewer. On the first subscribe,
	// there are no subscribers yet, so tmux's resize repaint and control-mode
	// attach's initial full-screen repaint are drained instead of being appended
	// after the backfill.
	st.setSize(cols, rows)
	st.waitInitialDrain()
	if st.isDone() {
		return nil, nil
	}

	ch := make(chan []byte, liveAttachSubBuffer)
	st.mu.Lock()
	st.subs[ch] = struct{}{}
	st.mu.Unlock()
	return st, ch
}

func (st *liveAttachStream) isDone() bool {
	if st == nil || st.done == nil {
		return true
	}
	select {
	case <-st.done:
		return true
	default:
		return false
	}
}

// dropStream removes a (now-finished) stream from the manager and closes every
// remaining subscriber channel so their WebSocket writers unblock.
func (m *liveAttachManager) dropStream(st *liveAttachStream) {
	m.mu.Lock()
	if m.sessions[st.tmuxSession] == st {
		delete(m.sessions, st.tmuxSession)
	}
	m.mu.Unlock()

	st.mu.Lock()
	for ch := range st.subs {
		delete(st.subs, ch)
		close(ch)
	}
	st.ptmx = nil
	st.mu.Unlock()
}

// start launches the attach goroutine. Caller holds mgr.mu.
func (st *liveAttachStream) start() {
	ctx, cancel := context.WithCancel(context.Background())
	st.cancel = cancel
	go func() {
		defer cancel()
		defer st.markInitialDrainComplete()
		defer st.mgr.dropStream(st)
		defer close(st.done)
		liveAttachAttachFn(st, ctx)
	}()
}

func (st *liveAttachStream) markInitialDrainComplete() {
	st.drainedOnce.Do(func() {
		close(st.drained)
	})
}

func (st *liveAttachStream) waitInitialDrain() {
	if st == nil || st.drained == nil {
		return
	}
	select {
	case <-st.drained:
	case <-time.After(liveAttachInitialDrainWait):
	}
}

// stop cancels the attach client; the goroutine then unwinds via dropStream.
func (st *liveAttachStream) stop() {
	st.mu.Lock()
	cancel := st.cancel
	st.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// unsubscribe removes a viewer and stops the attach client on the last one.
func (st *liveAttachStream) unsubscribe(ch chan []byte) {
	st.mu.Lock()
	_, ok := st.subs[ch]
	if ok {
		delete(st.subs, ch)
		close(ch)
	}
	last := ok && len(st.subs) == 0
	st.mu.Unlock()
	if last {
		st.stop()
	}
}

// broadcast fans a decoded pane chunk out to all viewers, dropping for any
// viewer whose buffer is full rather than blocking the whole stream.
func (st *liveAttachStream) broadcast(b []byte) {
	if len(b) == 0 {
		return
	}
	cp := make([]byte, len(b))
	copy(cp, b)
	st.mu.Lock()
	for ch := range st.subs {
		select {
		case ch <- cp:
		default:
			// Slow viewer: drop. Reconnect re-seeds via current-screen backfill.
		}
	}
	st.mu.Unlock()
}

// setSize records the requested geometry and applies it directly to the tmux
// window. The app's live pane must be the same grid as browser xterm; relying on
// the control-mode PTY size is not authoritative for pi-cli sessions on macOS.
// This mirrors the static resize path and the PoC demo: pin window-size manual,
// then resize-window to the fitted xterm grid.
func (st *liveAttachStream) setSize(cols, rows int) {
	if cols < terminalMinResizeCols || rows < terminalMinResizeRows {
		return
	}
	st.mu.Lock()
	if st.appliedCols == cols && st.appliedRows == rows {
		st.cols, st.rows = cols, rows
		st.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), terminalTmuxActionTimeout)
		defer cancel()
		if curW, curH, ok := terminalWindowSize(ctx, st.tmuxSession); ok && curW == cols && curH == rows {
			return
		}
	} else {
		st.cols, st.rows = cols, rows
		st.mu.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), terminalTmuxActionTimeout)
	defer cancel()
	st.enableManualWindowSize(ctx)
	if err := runTerminalTmuxCommand(ctx, "", "resize-window", "-t", st.tmuxSession, "-x", strconv.Itoa(cols), "-y", strconv.Itoa(rows)); err != nil {
		log.Printf("[live-attach] resize-window session=%s %dx%d: %v", st.tmuxSession, cols, rows, err)
		return
	}
	st.mu.Lock()
	st.appliedCols, st.appliedRows = cols, rows
	st.mu.Unlock()
}

// runControlMode is the real attach loop: it starts `tmux -CC attach` under a
// PTY (control mode still needs a real tty for the client's own stdio — plain
// pipes fail tcgetattr), parses the control protocol, and broadcasts decoded
// pane bytes. It returns on ctx cancel, %exit, or the session going away.
func (st *liveAttachStream) runControlMode(ctx context.Context) {
	st.mu.Lock()
	cols, rows := st.cols, st.rows
	st.mu.Unlock()
	if cols <= 0 {
		cols = liveAttachDefaultCols
	}
	if rows <= 0 {
		rows = liveAttachDefaultRows
	}

	// Keep explicit resize-window calls authoritative. Control-mode PTY sizing is
	// not reliable enough here; the browser grid owns the tmux window size.
	st.enableManualWindowSize(ctx)

	cmd := exec.CommandContext(ctx, "tmux", "-CC", "attach", "-t", st.tmuxSession)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		log.Printf("[live-attach] attach failed session=%s: %v", st.tmuxSession, err)
		return
	}
	drainTimer := time.AfterFunc(liveAttachInitialDrainDelay, st.markInitialDrainComplete)
	defer drainTimer.Stop()
	st.mu.Lock()
	st.ptmx = ptmx
	st.mu.Unlock()

	// Reap the PTY and control client when the context is canceled.
	go func() {
		<-ctx.Done()
		_ = ptmx.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}()

	sc := bufio.NewScanner(ptmx)
	sc.Buffer(make([]byte, liveAttachScannerInitial), liveattach.MaxControlLineBytes)
	for sc.Scan() {
		pl := liveattach.ClassifyLine(sc.Text())
		switch pl.Kind {
		case liveattach.LinePaneOutput:
			st.broadcast(pl.Data)
		case liveattach.LineEvent:
			if pl.IsExit() {
				log.Printf("[live-attach] session=%s control exit: %s", st.tmuxSession, pl.Raw)
				return
			}
			// %layout-change / %window-renamed / %session-changed / %begin /
			// %end / … : routed here, never into the pane stream. Phase 1 has
			// no per-event handling beyond %exit; reconnect backfill covers
			// layout/size changes.
		default:
			// LineEmpty / LineStray: ignore.
		}
	}
	if err := sc.Err(); err != nil {
		log.Printf("[live-attach] scanner error session=%s: %v", st.tmuxSession, err)
	}
}

// enableManualWindowSize pins the tmux window so explicit resize-window calls
// govern the geometry. Best-effort.
func (st *liveAttachStream) enableManualWindowSize(ctx context.Context) {
	cctx, cancel := context.WithTimeout(ctx, terminalTmuxActionTimeout)
	defer cancel()
	if err := runTerminalTmuxCommand(cctx, "", "set-window-option", "-t", st.tmuxSession, "window-size", "manual"); err != nil {
		log.Printf("[live-attach] set window-size manual session=%s: %v", st.tmuxSession, err)
	}
}

// liveAttachUpgrader upgrades the request to a WebSocket. Authentication and
// session ownership are enforced by the route's AuthMiddleware +
// requireAccessibleTerminal before the upgrade; CheckOrigin is permissive
// because access is already gated by the JWT/ownership check (the same posture
// as the existing authed API surface).
var liveAttachUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 64 * 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// handleTerminalStream is GET /api/terminals/{terminal_id}/stream — the
// live-attach WebSocket endpoint.
func (api *StreamingAPI) handleTerminalStream(w http.ResponseWriter, r *http.Request) {
	if !liveAttachEnabled() || api.liveAttach == nil {
		http.Error(w, "live-attach terminal transport disabled", http.StatusNotFound)
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

	cols, rows := liveAttachInitialSize(r)

	// The request log middleware wraps the ResponseWriter; unwrap to the raw
	// writer so gorilla/websocket can Hijack it.
	conn, err := liveAttachUpgrader.Upgrade(unwrapResponseWriter(w), r, nil)
	if err != nil {
		// Upgrade already wrote an error response.
		return
	}
	defer conn.Close()

	// The HTTP request context is tied to the (now hijacked) request; use a
	// fresh connection-scoped context for the tmux side commands.
	connCtx, connCancel := context.WithCancel(context.Background())
	defer connCancel()

	// 1) Subscribe to the shared control-mode attach for this tmux session. On a
	//    first viewer, subscribe warms the attach with no subscriber so tmux's
	//    initial repaint is drained before the browser sees live bytes.
	st, ch := api.liveAttach.subscribe(snapshot.TmuxSession, cols, rows)
	if st == nil || ch == nil {
		return
	}
	defer st.unsubscribe(ch)
	transcript := &liveAttachTranscript{}
	persistTranscript := func() {
		api.persistLiveAttachTranscript(snapshot.TerminalID, transcript.content())
	}

	// 2) One current-screen backfill snapshot so the fresh viewer sees current
	//    screen state before queued live %output resumes. The writer goroutine is
	//    started after this, so any live bytes produced during capture are
	//    delivered after the backfill instead of interleaving with it.
	if bf := liveAttachBackfillFn(connCtx, snapshot.TmuxSession); len(bf) > 0 {
		transcript.append(bf)
		persistTranscript()
		_ = conn.WriteMessage(websocket.BinaryMessage, bf)
	}
	defer persistTranscript()

	// Writer: decoded pane bytes -> WebSocket (binary). Ends when the channel
	// is closed (unsubscribe / stream death) or the connection write fails.
	// If the tmux stream dies before the browser sends input, actively close the
	// WebSocket so the frontend reconnects against the latest terminal snapshot
	// instead of sitting on a blank/stale "connected" stream.
	var writeMu sync.Mutex
	go func() {
		defer persistTranscript()
		for b := range ch {
			transcript.append(b)
			writeMu.Lock()
			err := conn.WriteMessage(websocket.BinaryMessage, b)
			writeMu.Unlock()
			if err != nil {
				return
			}
		}
		writeMu.Lock()
		_ = conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseGoingAway, "tmux stream closed"),
			time.Now().Add(time.Second),
		)
		writeMu.Unlock()
		_ = conn.Close()
	}()

	// Reader: WebSocket -> tmux, via the EXISTING input path.
	//   binary frame -> raw byte passthrough (send-keys -H)
	//   text frame   -> JSON control: resize | input | key
	for {
		mt, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		switch mt {
		case websocket.BinaryMessage:
			api.liveAttachRawInput(connCtx, snapshot.TmuxSession, data)
		case websocket.TextMessage:
			api.liveAttachControlFrame(connCtx, st, snapshot.TmuxSession, data)
		}
	}
}

type liveAttachTranscript struct {
	mu   sync.Mutex
	data []byte
}

func (t *liveAttachTranscript) append(b []byte) {
	if t == nil || len(b) == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.data = append(t.data, b...)
	t.data = trimLiveAttachTranscript(t.data)
}

func (t *liveAttachTranscript) content() string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(append([]byte(nil), t.data...))
}

func trimLiveAttachTranscript(data []byte) []byte {
	if len(data) <= terminalPipeRecorderMaxBytes {
		return data
	}
	tail := append([]byte(nil), data[len(data)-terminalPipeRecorderTrimBytes:]...)
	tail = trimToTerminalSequenceBoundary(tail)
	if len(tail) == 0 {
		return tail
	}
	trimmed := make([]byte, 0, len(terminalPipeNormalScreenPrologue)+len(tail))
	trimmed = append(trimmed, terminalPipeNormalScreenPrologue...)
	trimmed = append(trimmed, tail...)
	return trimmed
}

func (api *StreamingAPI) persistLiveAttachTranscript(terminalID, content string) {
	if api == nil || api.terminalStore == nil {
		return
	}
	terminalID = strings.TrimSpace(terminalID)
	if terminalID == "" || !liveAttachTranscriptHasDisplayContent(content) {
		return
	}
	api.terminalStore.SetDisplayContent(terminalID, content, "tmux_capture")
}

func liveAttachTranscriptHasDisplayContent(content string) bool {
	content = strings.ReplaceAll(content, terminalPipeNormalScreenPrologue, "")
	content = strings.ReplaceAll(content, terminalPipeAltScreenPrologue, "")
	content = strings.ReplaceAll(content, "\x1b[H\x1b[2J", "")
	return strings.TrimSpace(content) != ""
}

// unwrapResponseWriter peels off any ResponseWriter wrappers (e.g. the request
// log recorder) so the underlying writer's http.Hijacker is reachable.
func unwrapResponseWriter(w http.ResponseWriter) http.ResponseWriter {
	for {
		u, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return w
		}
		w = u.Unwrap()
	}
}

func liveAttachInitialSize(r *http.Request) (int, int) {
	cols, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("cols")))
	rows, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("rows")))
	if cols < terminalMinResizeCols || rows < terminalMinResizeRows {
		return liveAttachDefaultCols, liveAttachDefaultRows
	}
	return cols, rows
}

// liveAttachBackfill returns a clean seed for a connecting viewer:
//  1. RIS clears stale emulator state from the previous mount/session.
//  2. A bounded history slice is written as plain terminal lines, giving xterm
//     real native scrollback after reconnect/resume.
//  3. CSI 2J clears only the viewport, preserving that scrollback.
//  4. The current visible tmux screen is painted last as the authoritative live
//     frame before control-mode %output resumes.
//
// Do not use a single `capture-pane -S ...` as the visible seed. Flattened
// scrollback can contain old spinner/status frames and line wrapping from prior
// geometry; painting the current screen separately keeps those artifacts out of
// the viewport while still making manual scroll work.
func liveAttachBackfill(ctx context.Context, tmuxSession string) []byte {
	cctx, cancel := context.WithTimeout(ctx, terminalTmuxActionTimeout)
	defer cancel()

	var b []byte
	b = append(b, []byte("\x1bc")...)
	if history, err := runTerminalTmuxOutputCommand(cctx, "capture-pane", "-t", tmuxSession, "-p", "-e", "-J", "-S", fmt.Sprintf("-%d", liveAttachBackfillHistoryLines), "-E", "-1"); err == nil {
		history = strings.TrimRight(history, "\r\n")
		if history != "" {
			b = append(b, []byte(strings.ReplaceAll(history, "\n", "\r\n"))...)
			b = append(b, []byte("\r\n")...)
		}
	} else {
		log.Printf("[live-attach] history backfill session=%s: %v", tmuxSession, err)
	}

	out, err := runTerminalTmuxOutputCommand(cctx, "capture-pane", "-t", tmuxSession, "-p", "-e")
	if err != nil {
		log.Printf("[live-attach] backfill session=%s: %v", tmuxSession, err)
		return b
	}
	body := strings.ReplaceAll(out, "\n", "\r\n")
	b = append(b, []byte("\x1b[H\x1b[2J")...)
	b = append(b, []byte(body)...)
	return b
}

// liveAttachRawInput forwards raw terminal input bytes faithfully via
// `send-keys -H` (hex), so Enter (0d), Ctrl-C (03), arrows, and pastes all pass
// through. Reuses the existing tmux exec helper.
func (api *StreamingAPI) liveAttachRawInput(ctx context.Context, tmuxSession string, data []byte) {
	if len(data) == 0 {
		return
	}
	args := make([]string, 0, len(data)+4)
	args = append(args, "send-keys", "-t", tmuxSession, "-H")
	for _, b := range data {
		args = append(args, fmt.Sprintf("%02x", b))
	}
	cctx, cancel := context.WithTimeout(ctx, terminalTmuxActionTimeout)
	defer cancel()
	if err := runTerminalTmuxCommand(cctx, "", args...); err != nil {
		log.Printf("[live-attach] raw input session=%s: %v", tmuxSession, err)
	}
}

// liveAttachControlFrame handles a JSON text frame: resize (control PTY size),
// input (load-buffer+paste-buffer, the existing path), or key (send-keys named
// key, the existing path). Unrecognized JSON falls back to raw input for
// robustness.
func (api *StreamingAPI) liveAttachControlFrame(ctx context.Context, st *liveAttachStream, tmuxSession string, data []byte) {
	var ctrl struct {
		Type   string `json:"type"`
		Cols   int    `json:"cols"`
		Rows   int    `json:"rows"`
		Text   string `json:"text"`
		Submit bool   `json:"submit"`
		Key    string `json:"key"`
	}
	if err := json.Unmarshal(data, &ctrl); err != nil {
		api.liveAttachRawInput(ctx, tmuxSession, data)
		return
	}
	switch ctrl.Type {
	case "resize":
		st.setSize(ctrl.Cols, ctrl.Rows)
	case "input":
		cctx, cancel := context.WithTimeout(ctx, terminalTmuxActionTimeout)
		defer cancel()
		if ctrl.Text != "" {
			if err := pasteTerminalText(cctx, tmuxSession, ctrl.Text); err != nil {
				log.Printf("[live-attach] input session=%s: %v", tmuxSession, err)
			}
		}
		if ctrl.Submit {
			if err := sendTerminalKey(cctx, tmuxSession, "enter"); err != nil {
				log.Printf("[live-attach] submit session=%s: %v", tmuxSession, err)
			}
		}
	case "key":
		cctx, cancel := context.WithTimeout(ctx, terminalTmuxActionTimeout)
		defer cancel()
		if err := sendTerminalKey(cctx, tmuxSession, ctrl.Key); err != nil {
			log.Printf("[live-attach] key session=%s: %v", tmuxSession, err)
		}
	default:
		api.liveAttachRawInput(ctx, tmuxSession, data)
	}
}
