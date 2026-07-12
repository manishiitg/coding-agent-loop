package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/manishiitg/multi-llm-provider-go/pkg/tmuxinput"

	"mcp-agent-builder-go/agent_go/internal/liveattach"
	"mcp-agent-builder-go/agent_go/internal/terminals"
)

// Live-attach terminal transport (control mode, in-band).
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
//
// All tmux commands the transport needs (resize-window, the capture-pane
// backfill, the cursor query) are written to the control client's OWN stdin
// and answered in-stream between %begin/%end guards. tmux serializes those
// replies with %output, so a reply is an exact barrier: a viewer spliced into
// the broadcast at its seed's %end can never see output that the seed capture
// already contained (no duplicated spinners) and never misses output produced
// after it (no gap). The previous design ran capture-pane/display-message as
// separate tmux processes, which raced the live stream on every (re)connect.

const (
	// liveAttachSubBuffer bounds a single viewer's pending-bytes channel. A
	// viewer that falls this far behind is dropped entirely (its channel is
	// closed, the WebSocket closes, and the frontend reconnects and re-seeds);
	// silently dropping individual chunks would tear escape sequences.
	liveAttachSubBuffer = 256
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

// liveAttachCmd is one in-band command written to the control client's stdin.
// Its reply arrives on done; onReply (optional) runs first, inline in the
// scanner goroutine, so it is ordered exactly against the %output stream.
type liveAttachCmd struct {
	text    string
	onReply func(liveattach.Reply)
	done    chan liveattach.Reply
}

// liveAttachStream is a single `tmux -CC attach` control-mode client.
type liveAttachStream struct {
	mgr         *liveAttachManager
	tmuxSession string

	mu          sync.Mutex
	subs        map[chan []byte]struct{}
	cancel      context.CancelFunc
	done        chan struct{}
	appliedCols int
	appliedRows int
	// seeding counts viewers between seedViewer entry and splice/abandon, so
	// idle-stop logic does not kill the attach under a viewer that has not
	// reached the subs map yet.
	seeding int

	// In-band command plumbing. sendCommand enqueues onto writeCh; a single
	// writer pump (started when the control stdin exists) appends each command
	// to pending and then writes it, so queue order always equals write order
	// and no lock is ever held across the (potentially blocking) write.
	// deliverReply pops pending FIFO from the scanner goroutine.
	pendingMu sync.Mutex
	pending   []*liveAttachCmd
	writeCh   chan *liveAttachCmd
	pumpOnce  sync.Once

	// initial geometry for the attach PTY.
	cols int
	rows int
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

// stream returns the live control-mode stream for a tmux session, creating and
// starting it on first use. Returns nil for an empty session name.
func (m *liveAttachManager) stream(tmuxSession string, cols, rows int) *liveAttachStream {
	tmuxSession = strings.TrimSpace(tmuxSession)
	if tmuxSession == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
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
			writeCh:     make(chan *liveAttachCmd, 64),
			cols:        cols,
			rows:        rows,
		}
		m.sessions[tmuxSession] = st
		// Queued before start so it is the first in-band command: pin the
		// window so explicit resize-window calls own the geometry (the browser
		// grid is authoritative, not the control client's PTY size).
		if _, err := st.sendCommand(fmt.Sprintf("set-window-option -t %s window-size manual", tmuxSession), nil); err != nil {
			log.Printf("[live-attach] queue window-size manual session=%s: %v", tmuxSession, err)
		}
		st.start()
	}
	return st
}

// addViewer registers a viewer for a tmux session and returns the shared
// stream, the viewer's byte channel, and the seed frame (reset + scrollback
// history + current screen + cursor). The seed is captured IN-BAND: the viewer
// channel is spliced into the broadcast set at the seed's %end, inside the
// scanner goroutine, so seed and stream can neither overlap nor gap.
func (m *liveAttachManager) addViewer(ctx context.Context, tmuxSession string, cols, rows int) (*liveAttachStream, chan []byte, []byte, error) {
	// Validate before ANY in-band command is built: control-mode command lines
	// are string-parsed by tmux, so an unsafe session name must never reach
	// the queue.
	if err := liveAttachValidTarget(strings.TrimSpace(tmuxSession)); err != nil {
		return nil, nil, nil, err
	}
	st := m.stream(tmuxSession, cols, rows)
	if st == nil {
		return nil, nil, nil, fmt.Errorf("empty tmux session name")
	}
	ch, seed, err := st.seedViewer(ctx, cols, rows)
	if err != nil {
		return nil, nil, nil, err
	}
	return st, ch, seed, nil
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
	st.mu.Unlock()
}

// start launches the attach goroutine. Caller holds mgr.mu.
func (st *liveAttachStream) start() {
	ctx, cancel := context.WithCancel(context.Background())
	st.cancel = cancel
	go func() {
		defer cancel()
		defer st.mgr.dropStream(st)
		defer close(st.done)
		liveAttachAttachFn(st, ctx)
	}()
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
	last := ok && len(st.subs) == 0 && st.seeding == 0
	st.mu.Unlock()
	if last {
		st.stop()
	}
}

// broadcast fans a decoded pane chunk out to all viewers. A viewer whose
// buffer is full is dropped entirely (channel closed -> its WebSocket closes ->
// the frontend reconnects and re-seeds); forwarding a stream with holes would
// tear escape sequences and corrupt the pane.
func (st *liveAttachStream) broadcast(b []byte) {
	if len(b) == 0 {
		return
	}
	cp := make([]byte, len(b))
	copy(cp, b)
	dropped := false
	st.mu.Lock()
	for ch := range st.subs {
		select {
		case ch <- cp:
		default:
			delete(st.subs, ch)
			close(ch)
			dropped = true
			log.Printf("[live-attach] session=%s dropped slow viewer (buffer full)", st.tmuxSession)
		}
	}
	last := dropped && len(st.subs) == 0 && st.seeding == 0
	st.mu.Unlock()
	if last {
		st.stop()
	}
}

// setControlWriter publishes the control client's stdin and starts the single
// writer pump that flushes queued commands to it.
func (st *liveAttachStream) setControlWriter(w io.Writer) {
	st.pumpOnce.Do(func() {
		go st.writePump(w)
	})
}

// writePump is the ONLY goroutine that writes command lines to the control
// stdin. Appending to pending and writing happen in one goroutine, so queue
// order always equals write order (tmux numbers replies in arrival order) and
// no lock is held across the write — a blocked write can therefore never
// deadlock deliverReply on the scanner side.
func (st *liveAttachStream) writePump(w io.Writer) {
	for {
		select {
		case cmd := <-st.writeCh:
			st.pendingMu.Lock()
			st.pending = append(st.pending, cmd)
			st.pendingMu.Unlock()
			if _, err := io.WriteString(w, cmd.text+"\n"); err != nil {
				// A broken control stdin means the attach is dying; the scanner
				// will unwind the stream and close st.done, which unblocks any
				// waiters. Nothing more useful to do here.
				log.Printf("[live-attach] session=%s write command %q: %v", st.tmuxSession, cmd.text, err)
				return
			}
		case <-st.done:
			return
		}
	}
}

// enqueuePending appends a command to the reply queue without writing it.
// Used for the attach handshake, whose %begin/%end pair tmux emits unasked.
func (st *liveAttachStream) enqueuePending(cmd *liveAttachCmd) {
	st.pendingMu.Lock()
	st.pending = append(st.pending, cmd)
	st.pendingMu.Unlock()
}

// sendCommand queues one command line for the control client's stdin and
// registers it for FIFO reply matching.
func (st *liveAttachStream) sendCommand(text string, onReply func(liveattach.Reply)) (*liveAttachCmd, error) {
	cmd := &liveAttachCmd{text: text, onReply: onReply, done: make(chan liveattach.Reply, 1)}
	select {
	case st.writeCh <- cmd:
		return cmd, nil
	case <-st.done:
		return nil, fmt.Errorf("live-attach stream for %s is closed", st.tmuxSession)
	}
}

// deliverReply hands a completed %begin/%end block to the oldest pending
// command. Runs in the scanner goroutine, so onReply callbacks are perfectly
// ordered against broadcast %output.
func (st *liveAttachStream) deliverReply(reply liveattach.Reply) {
	st.pendingMu.Lock()
	var cmd *liveAttachCmd
	if len(st.pending) > 0 {
		cmd = st.pending[0]
		st.pending = st.pending[1:]
	}
	st.pendingMu.Unlock()
	if cmd == nil {
		log.Printf("[live-attach] session=%s unmatched command reply (number=%s err=%v)", st.tmuxSession, reply.Number, reply.Err)
		return
	}
	if reply.Err {
		log.Printf("[live-attach] session=%s command failed: %q -> %s", st.tmuxSession, cmd.text, strings.Join(reply.Lines, " / "))
	}
	if cmd.onReply != nil {
		cmd.onReply(reply)
	}
	cmd.done <- reply
}

// setSize applies the requested geometry to the tmux window via an in-band
// resize-window. The app's live pane must be the same grid as browser xterm;
// window-size manual (pinned at attach) makes these calls authoritative.
func (st *liveAttachStream) setSize(cols, rows int) {
	if cols < terminalMinResizeCols || rows < terminalMinResizeRows {
		return
	}
	st.mu.Lock()
	if st.appliedCols == cols && st.appliedRows == rows {
		st.mu.Unlock()
		return
	}
	// Optimistic: FIFO ordering means the last sent resize is the one that
	// sticks; a failed reply just allows a redundant retry later.
	st.appliedCols, st.appliedRows = cols, rows
	st.mu.Unlock()
	if _, err := st.sendCommand(fmt.Sprintf("resize-window -t %s -x %d -y %d", st.tmuxSession, cols, rows), nil); err != nil {
		log.Printf("[live-attach] resize-window session=%s %dx%d: %v", st.tmuxSession, cols, rows, err)
		st.mu.Lock()
		if st.appliedCols == cols && st.appliedRows == rows {
			st.appliedCols, st.appliedRows = 0, 0
		}
		st.mu.Unlock()
	}
}

// seedViewer captures the seed in-band and splices the viewer channel into the
// broadcast set at the final reply's %end (inside the scanner goroutine).
func (st *liveAttachStream) seedViewer(ctx context.Context, cols, rows int) (ch chan []byte, seed []byte, err error) {
	st.mu.Lock()
	st.seeding++
	st.mu.Unlock()
	seeded := false
	defer func() {
		st.mu.Lock()
		st.seeding--
		idle := len(st.subs) == 0 && st.seeding == 0 && !seeded
		st.mu.Unlock()
		if idle {
			// The seed failed before any viewer reached the subs map; without
			// this the attach would idle forever with zero viewers.
			st.stop()
		}
	}()

	if err := liveAttachValidTarget(st.tmuxSession); err != nil {
		return nil, nil, err
	}
	st.setSize(cols, rows)

	ch = make(chan []byte, liveAttachSubBuffer)
	var seedMu sync.Mutex
	abandoned := false
	resultCh := make(chan []byte, 1)

	// The seed chain runs entirely in the scanner goroutine (each step is an
	// onReply callback), serialized against %output. Command order is chosen so
	// the CURRENT-SCREEN capture is the LAST command and the viewer is spliced
	// on ITS %end:
	//   1. #{history_size} — tmux clamps a capture-pane -S/-E range into the
	//      visible screen when the scrollback is empty, which would seed the
	//      first screen row twice; only capture history that actually exists.
	//   2. capture-pane history slice (scrollback only, no -J).
	//   3. cursor query.
	//   4. capture-pane current screen — its %end is the splice point.
	//
	// The screen MUST be captured last and be the splice point. tmux emits pane
	// %output between our consecutive commands (never inside a reply block), so
	// any %output produced before the screen capture is folded into that
	// snapshot, and any %output after it is delivered to the freshly-spliced
	// viewer — no byte is both captured and streamed (duplication) or neither
	// (a gap that permanently desyncs xterm's grid from the CLI). The cursor is
	// queried just BEFORE the screen; a %output that moves the cursor in that
	// microscopic window leaves the seeded cursor momentarily stale, which the
	// first live redraw corrects — strictly preferable to dropping that output.
	var historyCmd, cursorCmd *liveAttachCmd
	finish := func(screen liveattach.Reply) {
		seedMu.Lock()
		defer seedMu.Unlock()
		if abandoned {
			return
		}
		// The history/cursor replies are FIFO-earlier, so their buffered done
		// channels are guaranteed filled (historyCmd may be nil: no scrollback).
		var history, cursor liveattach.Reply
		if historyCmd != nil {
			select {
			case history = <-historyCmd.done:
			default:
			}
		}
		if cursorCmd != nil {
			select {
			case cursor = <-cursorCmd.done:
			default:
			}
		}
		// Splice before the scanner classifies any further line: every %output
		// after this instant post-dates the screen capture.
		st.mu.Lock()
		st.subs[ch] = struct{}{}
		st.mu.Unlock()
		resultCh <- buildLiveAttachSeed(history, screen, cursor)
	}
	_, err = st.sendCommand(
		fmt.Sprintf("display-message -p -t %s '#{history_size}'", st.tmuxSession),
		func(sizeReply liveattach.Reply) {
			historySize := 0
			if !sizeReply.Err && len(sizeReply.Lines) > 0 {
				if n, err := strconv.Atoi(strings.TrimSpace(sizeReply.Lines[0])); err == nil {
					historySize = n
				}
			}
			if historySize > liveAttachBackfillHistoryLines {
				historySize = liveAttachBackfillHistoryLines
			}
			var sendErr error
			if historySize > 0 {
				historyCmd, sendErr = st.sendCommand(fmt.Sprintf("capture-pane -t %s -p -e -S -%d -E -1", st.tmuxSession, historySize), nil)
				if sendErr != nil {
					return // stream dying; the seed waiter unblocks via st.done
				}
			}
			cursorCmd, sendErr = st.sendCommand(fmt.Sprintf("display-message -p -t %s '#{cursor_x},#{cursor_y}'", st.tmuxSession), nil)
			if sendErr != nil {
				return
			}
			// Screen capture LAST; finish() splices the viewer on its %end.
			if _, sendErr = st.sendCommand(fmt.Sprintf("capture-pane -t %s -p -e", st.tmuxSession), finish); sendErr != nil {
				return
			}
		},
	)
	if err != nil {
		return nil, nil, err
	}

	abandon := func() {
		seedMu.Lock()
		abandoned = true
		seedMu.Unlock()
		// If the splice raced ahead of the abandon flag, undo it.
		select {
		case <-resultCh:
			st.unsubscribe(ch)
		default:
		}
	}

	timeout := time.NewTimer(terminalTmuxActionTimeout)
	defer timeout.Stop()
	select {
	case seed := <-resultCh:
		seeded = true
		return ch, seed, nil
	case <-st.done:
		return nil, nil, fmt.Errorf("live-attach stream for %s closed during seed", st.tmuxSession)
	case <-ctx.Done():
		abandon()
		return nil, nil, ctx.Err()
	case <-timeout.C:
		abandon()
		return nil, nil, fmt.Errorf("live-attach seed for %s timed out", st.tmuxSession)
	}
}

// buildLiveAttachSeed renders the in-band capture replies as a clean seed for
// a connecting viewer:
//  1. RIS clears stale emulator state from the previous mount/session.
//  2. The bounded history slice becomes xterm-native scrollback.
//  3. The current visible tmux screen is painted immediately below it as the
//     authoritative live frame — `capture-pane -p` always yields the full pane
//     height, so the screen lines scroll the history up into scrollback and
//     land exactly on the viewport. The cursor is left at tmux's real cursor
//     so cursor-relative live redraws (spinners/status rows) update the
//     seeded screen in place.
//
// Do NOT clear the viewport between history and screen: xterm.js ED(2)
// (\x1b[2J) ERASES viewport rows in place (no scrollback push unless
// scrollOnEraseInDisplay is set, which we don't set — live TUI repaints would
// stack stale frames). Right after a RIS the tail of the history is still IN
// the viewport, so a 2J here destroyed up to one screenful of the most recent
// scrollback on every (re)connect — with short histories, all of it.
//
// The history capture deliberately omits -J: joining preserves trailing
// spaces, which drags cell background fills (e.g. Claude Code's neutral
// canvas) out to full-width gray bars in scrollback.
func buildLiveAttachSeed(history, screen, cursor liveattach.Reply) []byte {
	var b []byte
	b = append(b, []byte("\x1bc")...)

	historyLines := history.Lines
	for len(historyLines) > 0 && strings.TrimSpace(historyLines[len(historyLines)-1]) == "" {
		historyLines = historyLines[:len(historyLines)-1]
	}
	if !history.Err && len(historyLines) > 0 {
		b = append(b, []byte(strings.Join(historyLines, "\r\n"))...)
		b = append(b, []byte("\r\n")...)
	}

	// SGR reset so attributes from the last history line can't bleed into the
	// screen paint (each capture-pane -e line re-establishes its own colors).
	b = append(b, []byte("\x1b[0m")...)
	if !screen.Err {
		b = append(b, []byte(strings.Join(screen.Lines, "\r\n"))...)
	}

	if x, y, ok := parseLiveAttachCursor(cursor); ok {
		b = append(b, []byte(fmt.Sprintf("\x1b[%d;%dH", y+1, x+1))...)
	}
	return b
}

// parseLiveAttachCursor reads the "#{cursor_x},#{cursor_y}" reply.
func parseLiveAttachCursor(cursor liveattach.Reply) (int, int, bool) {
	if cursor.Err || len(cursor.Lines) == 0 {
		return 0, 0, false
	}
	parts := strings.Split(strings.TrimSpace(cursor.Lines[0]), ",")
	if len(parts) != 2 {
		return 0, 0, false
	}
	x, errX := strconv.Atoi(strings.TrimSpace(parts[0]))
	y, errY := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errX != nil || errY != nil || x < 0 || y < 0 {
		return 0, 0, false
	}
	return x, y, true
}

// liveAttachValidTarget rejects session names that could not be written safely
// as a bare in-band command argument. App session names (mlp-…) always pass.
func liveAttachValidTarget(tmuxSession string) error {
	if tmuxSession == "" {
		return fmt.Errorf("empty tmux session name")
	}
	for _, r := range tmuxSession {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.' || r == ':':
		default:
			return fmt.Errorf("tmux session name %q contains unsupported character %q", tmuxSession, r)
		}
	}
	return nil
}

// runControlMode is the real attach loop: it starts `tmux -CC attach` under a
// PTY (control mode still needs a real tty for the client's own stdio — plain
// pipes fail tcgetattr), parses the control protocol, answers in-band command
// replies, and broadcasts decoded pane bytes. It returns on ctx cancel, %exit,
// or the session going away.
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

	cmd := exec.CommandContext(ctx, "tmux", "-CC", "attach", "-t", st.tmuxSession)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		log.Printf("[live-attach] attach failed session=%s: %v", st.tmuxSession, err)
		return
	}

	// tmux emits an unsolicited empty %begin/%end pair for the attach itself;
	// give it a queue slot before any real command so FIFO matching holds.
	// Enqueued before the writer pump starts, so it is guaranteed to sit ahead
	// of every queued command in pending.
	st.enqueuePending(&liveAttachCmd{text: "(attach)", done: make(chan liveattach.Reply, 1)})
	st.setControlWriter(ptmx)

	// Reap the PTY and control client when the context is canceled.
	go func() {
		<-ctx.Done()
		_ = ptmx.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}()

	proto := &liveattach.Protocol{}
	sc := bufio.NewScanner(ptmx)
	sc.Buffer(make([]byte, liveAttachScannerInitial), liveattach.MaxControlLineBytes)
	for sc.Scan() {
		pl, reply := proto.Feed(sc.Text())
		if reply != nil {
			st.deliverReply(*reply)
			continue
		}
		switch pl.Kind {
		case liveattach.LinePaneOutput:
			st.broadcast(pl.Data)
		case liveattach.LineEvent:
			if pl.IsExit() {
				log.Printf("[live-attach] session=%s control exit: %s", st.tmuxSession, pl.Raw)
				return
			}
			// %layout-change / %window-renamed / %session-changed / … : routed
			// here, never into the pane stream. No per-event handling needed;
			// reconnect re-seeds cover layout changes.
		default:
			// LineEmpty / LineStray / LineReplyBody: ignore.
		}
	}
	if err := sc.Err(); err != nil {
		log.Printf("[live-attach] scanner error session=%s: %v", st.tmuxSession, err)
	}
}

// liveAttachUpgrader upgrades the request to a WebSocket. Authentication and
// session ownership are enforced by the route's AuthMiddleware +
// requireAccessibleTerminal before the upgrade. Browser-origin checks are an
// additional CSRF/WS-hijacking backstop because this stream can inject pane
// input, not just observe it.
func (api *StreamingAPI) liveAttachUpgrader() websocket.Upgrader {
	return websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 64 * 1024,
		CheckOrigin:     api.checkLiveAttachOrigin,
	}
}

func (api *StreamingAPI) checkLiveAttachOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	return isAllowedCORSOrigin(origin, api.config.CORSOrigins)
}

// handleTerminalStream is GET /api/terminals/{terminal_id}/stream — the
// live-attach WebSocket endpoint.
func (api *StreamingAPI) handleTerminalStream(w http.ResponseWriter, r *http.Request) {
	if !liveAttachEnabled() || api.liveAttach == nil {
		http.Error(w, "live-attach terminal transport disabled", http.StatusNotFound)
		return
	}
	snapshot, ok := api.resolveLiveAttachTerminal(w, r)
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
	upgrader := api.liveAttachUpgrader()
	conn, err := upgrader.Upgrade(unwrapResponseWriter(w), r, nil)
	if err != nil {
		// Upgrade already wrote an error response.
		return
	}
	defer conn.Close()

	// The HTTP request context is tied to the (now hijacked) request; use a
	// fresh connection-scoped context for the tmux side commands.
	connCtx, connCancel := context.WithCancel(context.Background())
	defer connCancel()

	// Seed + subscribe in one atomic in-band operation: the viewer channel is
	// spliced into the broadcast exactly at the seed capture's %end, so the
	// seed and the live stream can neither overlap (duplicated frames) nor
	// gap. If the attach dies during seeding this returns an error and the
	// WebSocket closes, prompting the frontend to reconnect against the
	// latest terminal snapshot.
	st, ch, seed, err := api.liveAttach.addViewer(connCtx, snapshot.TmuxSession, cols, rows)
	if err != nil {
		log.Printf("[live-attach] seed terminal=%s session=%s: %v", snapshot.TerminalID, snapshot.TmuxSession, err)
		return
	}
	defer st.unsubscribe(ch)
	transcript := &liveAttachTranscript{}
	persistTranscript := func() {
		api.persistLiveAttachTranscript(snapshot.TerminalID, transcript.content())
	}

	if len(seed) > 0 {
		transcript.append(seed)
		persistTranscript()
		_ = conn.WriteMessage(websocket.BinaryMessage, seed)
	}
	defer persistTranscript()

	// Writer: decoded pane bytes -> WebSocket (binary). Ends when the channel
	// is closed (unsubscribe / stream death / slow-viewer drop) or the
	// connection write fails. If the tmux stream dies before the browser sends
	// input, actively close the WebSocket so the frontend reconnects against
	// the latest terminal snapshot instead of sitting on a blank/stale
	// "connected" stream.
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

func (api *StreamingAPI) resolveLiveAttachTerminal(w http.ResponseWriter, r *http.Request) (terminals.Snapshot, bool) {
	terminalID := strings.TrimSpace(mux.Vars(r)["terminal_id"])
	if terminalID == "" {
		http.Error(w, "Terminal ID is required", http.StatusBadRequest)
		return terminals.Snapshot{}, false
	}
	if api.terminalStore != nil {
		if snapshot, ok := api.terminalStore.Get(terminalID); ok && api.canAccessTerminalSession(r, snapshot.SessionID) {
			return snapshot, true
		}
	}

	// Backend restarts clear the in-memory terminal store while the browser can
	// still hold the last terminal snapshot and the tmux session can still be
	// alive. Recover the live stream from those client-supplied identifiers only
	// after applying the same session access check and verifying the tmux-native
	// owner metadata. A client-supplied tmux name alone is not authorization.
	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	tmuxSession := strings.TrimSpace(r.URL.Query().Get("tmux_session"))
	if sessionID == "" || tmuxSession == "" || !api.canAccessTerminalSession(r, sessionID) {
		http.Error(w, "Terminal not found", http.StatusNotFound)
		return terminals.Snapshot{}, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), terminalTmuxActionTimeout)
	defer cancel()
	if err := runTerminalTmuxCommand(ctx, "", "has-session", "-t", tmuxSession); err != nil {
		http.Error(w, "Terminal not found", http.StatusNotFound)
		return terminals.Snapshot{}, false
	}
	ownerSessionID, err := runTerminalTmuxOutputCommand(ctx, "show-options", "-v", "-t", tmuxSession, tmuxinput.OwnerSessionOption)
	if err != nil || strings.TrimSpace(ownerSessionID) != sessionID {
		log.Printf("[LIVE_ATTACH] Rejected unowned restart recovery terminal=%q tmux=%q requested_session=%q", terminalID, tmuxSession, sessionID)
		http.Error(w, "Terminal not found", http.StatusNotFound)
		return terminals.Snapshot{}, false
	}
	return terminals.Snapshot{
		TerminalID:    terminalID,
		SessionID:     sessionID,
		TmuxSession:   tmuxSession,
		ContentSource: "tmux_capture",
		Active:        true,
		State:         "running",
	}, true
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
	priority := tmuxinput.PriorityNormal
	// A lone ESC and Ctrl-C are interrupts. Arrow/navigation keys are multi-byte
	// escape sequences and must retain normal FIFO ordering.
	if len(data) == 1 && (data[0] == 0x03 || data[0] == 0x1b) {
		priority = tmuxinput.PriorityInterrupt
	}
	_, err := tmuxinput.Default.Do(cctx, tmuxinput.Request{
		SessionID: tmuxSession,
		Source:    "terminal-live-raw",
		Priority:  priority,
	}, func(ctx context.Context) error {
		return runTerminalTmuxCommand(ctx, "", args...)
	})
	if err != nil {
		log.Printf("[live-attach] raw input session=%s: %v", tmuxSession, err)
	}
}

// liveAttachControlFrame handles a JSON text frame: resize (in-band
// resize-window), input (load-buffer+paste-buffer, the existing path), or key
// (send-keys named key, the existing path). Unrecognized JSON falls back to
// raw input for robustness.
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
		if err := deliverTerminalInput(cctx, tmuxSession, ctrl.Text, ctrl.Submit, terminalTmuxSessionLooksCursor(tmuxSession), "terminal-live-input"); err != nil {
			log.Printf("[live-attach] input session=%s: %v", tmuxSession, err)
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
