package server

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/manishiitg/multi-llm-provider-go/pkg/tmuxinput"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/liveattach"
	"github.com/manishiitg/coding-agent-loop/agent_go/internal/terminals"
)

func TestLiveAttachEnabled(t *testing.T) {
	// The RUNLOOP_TERMINAL_LIVE_ATTACH flag was removed; live-attach is always on.
	if !liveAttachEnabled() {
		t.Error("liveAttachEnabled() = false, want true (flag removed, always on)")
	}
}

func TestTmuxVersionAtLeast(t *testing.T) {
	cases := []struct {
		banner string
		major  int
		minor  int
		want   bool
	}{
		{"tmux 3.6a", 2, 9, true},
		{"tmux 3.0", 2, 9, true},
		{"tmux 2.9", 2, 9, true},
		{"tmux 2.9a", 2, 9, true},
		{"tmux 2.8", 2, 9, false},
		{"tmux 1.8", 2, 9, false},
		{"tmux next-3.4", 2, 9, true},
		{"tmux 2.10", 2, 9, true},
		{"tmux 3.6a", 3, 0, true},
		{"tmux 2.9", 3, 0, false},
		{"3.6a", 2, 9, true}, // no "tmux" prefix
		{"tmux ", 2, 9, false},
		{"garbage", 2, 9, false},
		{"", 2, 9, false},
	}
	for _, tc := range cases {
		if got := tmuxVersionAtLeast(tc.banner, tc.major, tc.minor); got != tc.want {
			t.Errorf("tmuxVersionAtLeast(%q, %d, %d) = %v, want %v", tc.banner, tc.major, tc.minor, got, tc.want)
		}
	}
}

func TestHandleTerminalStreamMissingManagerReturns404(t *testing.T) {
	api := &StreamingAPI{} // liveAttach is nil
	req := httptest.NewRequest(http.MethodGet, "/api/terminals/main:sess/stream", nil)
	rec := httptest.NewRecorder()
	api.handleTerminalStream(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestResolveLiveAttachRestartRecoveryRequiresMatchingTmuxOwner(t *testing.T) {
	oldRun := runTerminalTmuxCommand
	oldOutput := runTerminalTmuxOutputCommand
	defer func() {
		runTerminalTmuxCommand = oldRun
		runTerminalTmuxOutputCommand = oldOutput
	}()
	runTerminalTmuxCommand = func(context.Context, string, ...string) error { return nil }
	runTerminalTmuxOutputCommand = func(_ context.Context, args ...string) (string, error) {
		if len(args) == 5 && args[0] == "show-options" && args[4] == tmuxinput.OwnerSessionOption {
			return "owner-session\n", nil
		}
		return "", nil
	}

	api := &StreamingAPI{}
	req := httptest.NewRequest(http.MethodGet, "/api/terminals/main/stream?session_id=owner-session&tmux_session=mlp-test", nil)
	req = mux.SetURLVars(req, map[string]string{"terminal_id": "main"})
	rec := httptest.NewRecorder()
	snapshot, ok := api.resolveLiveAttachTerminal(rec, req)
	if !ok {
		t.Fatalf("restart recovery rejected matching owner: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if snapshot.SessionID != "owner-session" || snapshot.TmuxSession != "mlp-test" {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
}

func TestResolveLiveAttachRestartRecoveryRejectsDifferentTmuxOwner(t *testing.T) {
	oldRun := runTerminalTmuxCommand
	oldOutput := runTerminalTmuxOutputCommand
	defer func() {
		runTerminalTmuxCommand = oldRun
		runTerminalTmuxOutputCommand = oldOutput
	}()
	runTerminalTmuxCommand = func(context.Context, string, ...string) error { return nil }
	runTerminalTmuxOutputCommand = func(context.Context, ...string) (string, error) {
		return "different-session\n", nil
	}

	api := &StreamingAPI{}
	req := httptest.NewRequest(http.MethodGet, "/api/terminals/main/stream?session_id=owner-session&tmux_session=mlp-other", nil)
	req = mux.SetURLVars(req, map[string]string{"terminal_id": "main"})
	rec := httptest.NewRecorder()
	if _, ok := api.resolveLiveAttachTerminal(rec, req); ok {
		t.Fatal("restart recovery accepted a tmux session owned by another chat")
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestLiveAttachInitialSizeRejectsTinyGeometry(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/stream?cols=20&rows=8", nil)
	cols, rows := liveAttachInitialSize(req)
	if cols != liveAttachDefaultCols || rows != liveAttachDefaultRows {
		t.Fatalf("initial size = %dx%d, want defaults %dx%d", cols, rows, liveAttachDefaultCols, liveAttachDefaultRows)
	}
}

// fakeControlChannel emulates the tmux control-mode command loop: it reads
// command lines the stream writes to its control stdin and answers each one
// FIFO via deliverReply, using the provided responder. It is the test stand-in
// for the in-band %begin/%end protocol.
type fakeControlChannel struct {
	commands chan string
}

// installFakeAttach swaps the real control-mode loop for a fake that services
// in-band commands with respond() and blocks until the stream stops. It
// restores the original on cleanup and returns the channel of observed
// command lines.
func installFakeAttach(t *testing.T, respond func(cmd string) liveattach.Reply) *fakeControlChannel {
	t.Helper()
	fake := &fakeControlChannel{commands: make(chan string, 64)}
	orig := liveAttachAttachFn
	liveAttachAttachFn = func(st *liveAttachStream, ctx context.Context) {
		pr, pw := io.Pipe()
		st.setControlWriter(pw)
		go func() {
			<-ctx.Done()
			_ = pr.Close()
			_ = pw.Close()
		}()
		// Reader and responder are separate goroutines, mirroring the real
		// transport: the pipe is always being read (a PTY's kernel buffer
		// never blocks the writer on the scanner), while this goroutine plays
		// the scanner and answers commands FIFO.
		lines := make(chan string, 256)
		go func() {
			defer close(lines)
			sc := bufio.NewScanner(pr)
			for sc.Scan() {
				lines <- sc.Text()
			}
		}()
		for line := range lines {
			select {
			case fake.commands <- line:
			default:
			}
			var reply liveattach.Reply
			if respond != nil {
				reply = respond(line)
			}
			st.deliverReply(reply)
		}
	}
	origRun := runTerminalTmuxCommand
	runTerminalTmuxCommand = func(ctx context.Context, stdin string, args ...string) error {
		return nil
	}
	t.Cleanup(func() {
		liveAttachAttachFn = orig
		runTerminalTmuxCommand = origRun
	})
	return fake
}

// seedResponder answers the seed-chain commands with a canned seed.
func seedResponder(history, screen []string, cursor string) func(string) liveattach.Reply {
	return func(cmd string) liveattach.Reply {
		switch {
		case strings.Contains(cmd, "#{history_size}"):
			return liveattach.Reply{Lines: []string{strconv.Itoa(len(history))}}
		case strings.HasPrefix(cmd, "capture-pane") && strings.Contains(cmd, " -S "):
			return liveattach.Reply{Lines: history}
		case strings.HasPrefix(cmd, "capture-pane"):
			return liveattach.Reply{Lines: screen}
		case strings.Contains(cmd, "#{cursor_x}"):
			return liveattach.Reply{Lines: []string{cursor}}
		}
		return liveattach.Reply{}
	}
}

func waitStreamDone(t *testing.T, st *liveAttachStream) {
	t.Helper()
	select {
	case <-st.done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not stop")
	}
}

func TestLiveAttachAddViewerSeedsAndStreams(t *testing.T) {
	installFakeAttach(t, seedResponder(
		[]string{"old-one", "old-two", ""},
		[]string{"cur-one", "cur-two"},
		"4,2",
	))
	m := newLiveAttachManager()

	st, viewer, seed, err := m.addViewer(context.Background(), "sessA", 100, 40)
	if err != nil {
		t.Fatalf("addViewer: %v", err)
	}
	got := string(seed)
	if !strings.HasPrefix(got, "\x1bc") {
		t.Fatalf("seed prefix = %q, want RIS reset", got[:min(len(got), 8)])
	}
	historyAt := strings.Index(got, "old-one\r\nold-two\r\n")
	visibleAt := strings.Index(got, "cur-one\r\ncur-two")
	cursorAt := strings.LastIndex(got, "\x1b[3;5H")
	if historyAt < 0 || visibleAt < 0 || historyAt >= visibleAt {
		t.Fatalf("seed should be history then current screen: %q", got)
	}
	// Regression guard: no viewport clear between history and screen. xterm.js
	// ED(2) erases in place (no scrollback push), so a 2J here destroys the
	// history tail that is still inside the viewport right after the RIS —
	// the screen paint must scroll history into scrollback naturally instead.
	if strings.Contains(got, "\x1b[2J") {
		t.Fatalf("seed must not clear the viewport (erases in-viewport history): %q", got)
	}
	if cursorAt < 0 || cursorAt < visibleAt {
		t.Fatalf("seed should restore tmux cursor after visible screen: %q", got)
	}

	// Live bytes broadcast after the splice reach the viewer.
	st.broadcast([]byte("live"))
	select {
	case b := <-viewer.ch:
		if string(b) != "live" {
			t.Fatalf("got %q, want live", string(b))
		}
	case <-time.After(time.Second):
		t.Fatal("viewer did not receive live bytes")
	}

	st.unsubscribe(viewer)
	waitStreamDone(t, st)
}

func TestLiveAttachSeedWaitsForResizeRepaintGrace(t *testing.T) {
	var resizeRepliedAt time.Time
	var firstCaptureAt time.Time
	installFakeAttach(t, func(cmd string) liveattach.Reply {
		switch {
		case strings.HasPrefix(cmd, "resize-window"):
			resizeRepliedAt = time.Now()
			return liveattach.Reply{}
		case strings.Contains(cmd, "#{history_size}"):
			firstCaptureAt = time.Now()
			return liveattach.Reply{Lines: []string{"0"}}
		case strings.Contains(cmd, "#{cursor_x}"):
			return liveattach.Reply{Lines: []string{"0,0"}}
		case strings.HasPrefix(cmd, "capture-pane"):
			return liveattach.Reply{Lines: []string{"repainted-screen"}}
		default:
			return liveattach.Reply{}
		}
	})
	m := newLiveAttachManager()
	st, viewer, seed, err := m.addViewer(context.Background(), "resizeGrace", 117, 35)
	if err != nil {
		t.Fatalf("addViewer: %v", err)
	}
	if resizeRepliedAt.IsZero() || firstCaptureAt.IsZero() {
		t.Fatalf("missing resize/capture timestamps: resize=%v capture=%v", resizeRepliedAt, firstCaptureAt)
	}
	if delay := firstCaptureAt.Sub(resizeRepliedAt); delay < liveAttachResizeNoOutputGrace-40*time.Millisecond {
		t.Fatalf("seed capture started %v after resize reply; want repaint grace near %v", delay, liveAttachResizeNoOutputGrace)
	}
	if !strings.Contains(string(seed), "repainted-screen") {
		t.Fatalf("seed = %q, want repainted screen", string(seed))
	}
	st.unsubscribe(viewer)
	waitStreamDone(t, st)
}

func TestLiveAttachForceViewerRepaintSignalsPaneWithoutResize(t *testing.T) {
	seed := seedResponder(nil, []string{"screen-with-working-spinner"}, "0,0")
	fake := installFakeAttach(t, func(cmd string) liveattach.Reply {
		if strings.Contains(cmd, "#{pane_pid}") {
			return liveattach.Reply{Lines: []string{"4242"}}
		}
		return seed(cmd)
	})
	originalSignal := signalLiveAttachPaneProcess
	signaledPID := 0
	signalLiveAttachPaneProcess = func(pid int) error {
		signaledPID = pid
		return nil
	}
	t.Cleanup(func() { signalLiveAttachPaneProcess = originalSignal })

	m := newLiveAttachManager()
	st, viewer, _, err := m.addViewer(context.Background(), "repaint-all-providers", 117, 35)
	if err != nil {
		t.Fatalf("addViewer: %v", err)
	}
	defer func() {
		st.unsubscribe(viewer)
		waitStreamDone(t, st)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// Ignore the initial seed commands; repaint itself must not resize.
	for {
		select {
		case <-fake.commands:
		default:
			goto seededCommandsDrained
		}
	}

seededCommandsDrained:
	if err := st.forceViewerRepaint(ctx); err != nil {
		t.Fatalf("forceViewerRepaint: %v", err)
	}

	var repaintCommands []string
	for {
		select {
		case cmd := <-fake.commands:
			repaintCommands = append(repaintCommands, cmd)
		default:
			goto drained
		}
	}

drained:
	if signaledPID != 4242 {
		t.Fatalf("signaled pid = %d, want 4242", signaledPID)
	}
	if len(repaintCommands) != 1 || !strings.Contains(repaintCommands[0], "#{pane_pid}") {
		t.Fatalf("repaint commands = %q, want only pane pid lookup", repaintCommands)
	}
	for _, cmd := range repaintCommands {
		if strings.HasPrefix(cmd, "resize-window") {
			t.Fatalf("same-size repaint unexpectedly changed geometry: %q", cmd)
		}
	}
}

func TestLiveAttachSeedSpliceExcludesPreSeedOutput(t *testing.T) {
	// Output broadcast BEFORE the seed's final reply must not reach the viewer:
	// it is, by protocol order, already contained in the screen capture. The
	// fake emits pane output right before answering the cursor query, exactly
	// like a busy CLI streaming during a (re)connect.
	var stRef *liveAttachStream
	installFakeAttach(t, func(cmd string) liveattach.Reply {
		switch {
		case strings.Contains(cmd, "#{history_size}"):
			return liveattach.Reply{Lines: []string{"1"}}
		case strings.HasPrefix(cmd, "capture-pane") && strings.Contains(cmd, " -S "):
			return liveattach.Reply{Lines: []string{"history"}}
		case strings.Contains(cmd, "#{cursor_x}"):
			return liveattach.Reply{Lines: []string{"0,0"}}
		case strings.HasPrefix(cmd, "capture-pane"):
			// The screen capture is the splice point. Pane output observed up to
			// this instant is, by protocol order, already folded into this
			// snapshot — it must NOT also be streamed to the viewer.
			stRef.broadcast([]byte("pre-seed-spinner-tick"))
			return liveattach.Reply{Lines: []string{"screen-with-spinner"}}
		}
		return liveattach.Reply{}
	})
	m := newLiveAttachManager()
	stRef = m.stream("sessB", 100, 40)

	st, viewer, seed, err := m.addViewer(context.Background(), "sessB", 100, 40)
	if err != nil {
		t.Fatalf("addViewer: %v", err)
	}
	if !strings.Contains(string(seed), "screen-with-spinner") {
		t.Fatalf("seed missing screen: %q", string(seed))
	}
	select {
	case b := <-viewer.ch:
		t.Fatalf("viewer received pre-seed output %q; must be dropped (already in the capture)", string(b))
	default:
	}

	st.broadcast([]byte("post-seed"))
	select {
	case b := <-viewer.ch:
		if string(b) != "post-seed" {
			t.Fatalf("got %q, want post-seed", string(b))
		}
	case <-time.After(time.Second):
		t.Fatal("viewer did not receive post-seed bytes")
	}

	st.unsubscribe(viewer)
	waitStreamDone(t, st)
}

// TestLiveAttachManagerSupersedesPreviousViewer pins the single-viewer rule.
// tmux has ONE window size, so two viewers on different grids can never both
// render correctly: whichever seeds last resizes the window, and the incumbent
// is left rendering bytes wrapped for a width its xterm never had. The newest
// viewer wins and the incumbent is evicted, marked so its WebSocket closes with
// liveAttachSupersededCloseCode rather than a generic going-away.
func TestLiveAttachManagerSupersedesPreviousViewer(t *testing.T) {
	installFakeAttach(t, seedResponder(nil, []string{"screen"}, "0,0"))
	m := newLiveAttachManager()

	st1, first, _, err := m.addViewer(context.Background(), "sessC", 120, 36)
	if err != nil {
		t.Fatalf("first addViewer: %v", err)
	}
	// A second viewer at a DIFFERENT grid — the case that used to corrupt the
	// incumbent silently, because resizeToThen recorded the new size before
	// %layout-change arrived and the reconcile then saw a valid self-resize.
	st2, second, _, err := m.addViewer(context.Background(), "sessC", 80, 24)
	if err != nil {
		t.Fatalf("second addViewer: %v", err)
	}
	if st2 != st1 {
		t.Fatal("second viewer created a new stream for the same session")
	}

	if !viewerClosed(t, first.ch) {
		t.Fatal("incumbent viewer survived a new attach at a different grid")
	}
	if !first.wasSuperseded() {
		t.Fatal("evicted viewer not marked superseded; its socket would close as going-away and the client would reconnect (livelock)")
	}

	// The newest viewer owns the stream and still receives live bytes.
	if second.wasSuperseded() {
		t.Fatal("newest viewer must not be marked superseded")
	}
	st1.broadcast([]byte("hello"))
	select {
	case b, ok := <-second.ch:
		if !ok {
			t.Fatal("newest viewer channel closed")
		}
		if string(b) != "hello" {
			t.Fatalf("got %q, want hello", b)
		}
	case <-time.After(time.Second):
		t.Fatal("newest viewer did not receive broadcast")
	}

	m.mu.Lock()
	_, stillThere := m.sessions["sessC"]
	m.mu.Unlock()
	if !stillThere {
		t.Fatal("stream removed while the newest viewer is still attached")
	}

	// Unsubscribing the last viewer stops the stream and removes it.
	st2.unsubscribe(second)
	waitStreamDone(t, st2)
	m.mu.Lock()
	_, gone := m.sessions["sessC"]
	m.mu.Unlock()
	if gone {
		t.Fatal("stream not removed from manager after last unsubscribe")
	}
}

func TestLiveAttachManagerResubscribeAfterDeath(t *testing.T) {
	installFakeAttach(t, seedResponder(nil, []string{"screen"}, "0,0"))
	m := newLiveAttachManager()

	st1, ch1, _, err := m.addViewer(context.Background(), "sessD", 80, 24)
	if err != nil {
		t.Fatalf("addViewer: %v", err)
	}
	st1.unsubscribe(ch1) // last unsubscribe -> stop
	waitStreamDone(t, st1)

	// Subscribing again must create a fresh stream (the old one was dropped).
	st2, ch2, _, err := m.addViewer(context.Background(), "sessD", 80, 24)
	if err != nil {
		t.Fatalf("resubscribe: %v", err)
	}
	if st2 == st1 {
		t.Fatal("resubscribe reused a dead stream")
	}
	st2.unsubscribe(ch2)
	waitStreamDone(t, st2)
}

func TestLiveAttachManagerRejectsEmptySession(t *testing.T) {
	installFakeAttach(t, nil)
	m := newLiveAttachManager()
	if _, _, _, err := m.addViewer(context.Background(), "   ", 80, 24); err == nil {
		t.Fatal("expected error for empty session name")
	}
}

func TestLiveAttachManagerRejectsUnsafeSession(t *testing.T) {
	installFakeAttach(t, nil)
	m := newLiveAttachManager()
	_, _, _, err := m.addViewer(context.Background(), "bad name; kill-server", 80, 24)
	if err == nil {
		t.Fatal("expected error for session name with unsafe characters")
	}
	// The failed seed must not leak an idle attach.
	m.mu.Lock()
	st := m.sessions["bad name; kill-server"]
	m.mu.Unlock()
	if st != nil {
		waitStreamDone(t, st)
	}
}

func TestLiveAttachBroadcastDropsSlowViewerEntirely(t *testing.T) {
	installFakeAttach(t, seedResponder(nil, []string{"screen"}, "0,0"))
	m := newLiveAttachManager()
	st, viewer, _, err := m.addViewer(context.Background(), "sessE", 80, 24)
	if err != nil {
		t.Fatalf("addViewer: %v", err)
	}

	// Overfill the subscriber buffer; the viewer must be dropped (channel
	// closed) rather than silently losing mid-stream chunks, and the
	// now-idle stream must stop.
	for i := 0; i < liveAttachSubBuffer+50; i++ {
		st.broadcast([]byte("x"))
	}
	received := 0
	for range viewer.ch {
		received++
	}
	if received > liveAttachSubBuffer {
		t.Fatalf("received %d chunks, want <= buffer %d", received, liveAttachSubBuffer)
	}
	waitStreamDone(t, st)
}

func TestLiveAttachSetSizeDedupsAndSendsInBand(t *testing.T) {
	fake := installFakeAttach(t, seedResponder(nil, []string{"screen"}, "0,0"))
	m := newLiveAttachManager()
	st, viewer, _, err := m.addViewer(context.Background(), "sessF", 120, 36)
	if err != nil {
		t.Fatalf("addViewer: %v", err)
	}

	st.setSize(120, 36) // duplicate: no new command
	st.setSize(121, 36) // change: one resize-window

	// Commands are written by the async pump; collect until the second resize
	// is observed (or time out) before tearing the stream down.
	var commands []string
	deadline := time.After(2 * time.Second)
	for !strings.HasSuffix(strings.Join(commands, "|"), "resize-window -t sessF -x 121 -y 36") {
		select {
		case c := <-fake.commands:
			commands = append(commands, c)
		case <-deadline:
			t.Fatalf("timed out waiting for second resize; commands = %#v", commands)
		}
	}
	st.unsubscribe(viewer)
	waitStreamDone(t, st)

	var resizes []string
	for _, c := range commands {
		if strings.HasPrefix(c, "resize-window") {
			resizes = append(resizes, c)
		}
	}
	want := []string{
		"resize-window -t sessF -x 120 -y 36",
		"resize-window -t sessF -x 121 -y 36",
	}
	if len(resizes) != len(want) || resizes[0] != want[0] || resizes[1] != want[1] {
		t.Fatalf("resize commands = %#v, want %#v (dedup must drop the duplicate 120x36)", resizes, want)
	}
	if commands[0] != "set-window-option -t sessF window-size manual" {
		t.Fatalf("first in-band command = %q, want window-size manual pin", commands[0])
	}
}

func TestHandleTerminalStreamClosesWhenAttachDies(t *testing.T) {
	// Simulate a missing/dead tmux target: the control-mode client exits
	// immediately, so the seed can never complete and the WebSocket must
	// close instead of hanging.
	origAttach := liveAttachAttachFn
	liveAttachAttachFn = func(st *liveAttachStream, ctx context.Context) {}
	origRun := runTerminalTmuxCommand
	runTerminalTmuxCommand = func(ctx context.Context, stdin string, args ...string) error {
		return nil
	}
	t.Cleanup(func() {
		liveAttachAttachFn = origAttach
		runTerminalTmuxCommand = origRun
	})

	store := terminals.NewStore()
	sessionID := "session-live-attach-dead"
	terminalID := sessionID + ":main:" + sessionID
	store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "main:"+sessionID, "tmux-dead", "old pane", 1))
	api := &StreamingAPI{terminalStore: store, liveAttach: newLiveAttachManager()}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = mux.SetURLVars(r, map[string]string{"terminal_id": terminalID})
		api.handleTerminalStream(w, r)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/stream?cols=80&rows=24"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial stream: %v", err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("ReadMessage succeeded; want stream websocket to close when attach dies")
	} else if strings.Contains(err.Error(), "i/o timeout") {
		t.Fatalf("stream stayed open after attach died: %v", err)
	}
}

func TestHandleTerminalStreamPersistsAnsiTranscript(t *testing.T) {
	installFakeAttach(t, seedResponder(nil, []string{"\x1b[34mseed\x1b[0m"}, "0,0"))

	store := terminals.NewStore()
	sessionID := "session-live-attach-ansi"
	terminalID := sessionID + ":main:" + sessionID
	tmuxSession := "tmux-live-attach-ansi"
	store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "main:"+sessionID, tmuxSession, "plain event pane", 1))
	api := &StreamingAPI{terminalStore: store, liveAttach: newLiveAttachManager()}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = mux.SetURLVars(r, map[string]string{"terminal_id": terminalID})
		api.handleTerminalStream(w, r)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/stream?cols=80&rows=24"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial stream: %v", err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	_, seed, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	if !strings.Contains(string(seed), "\x1b[34mseed") {
		t.Fatalf("seed lost ANSI bytes: %q", string(seed))
	}
	stored, ok := store.Get(terminalID)
	if !ok {
		t.Fatalf("terminal %q not found", terminalID)
	}
	if stored.ContentSource != "tmux_capture" || !strings.Contains(stored.Content, "\x1b[34mseed") {
		t.Fatalf("stored initial transcript = source %q content %q", stored.ContentSource, stored.Content)
	}

	api.liveAttach.mu.Lock()
	stream := api.liveAttach.sessions[tmuxSession]
	api.liveAttach.mu.Unlock()
	if stream == nil {
		t.Fatal("live attach stream was not registered")
	}
	stream.broadcast([]byte("\x1b[31mlive\x1b[0m"))

	_, live, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read live bytes: %v", err)
	}
	if !strings.Contains(string(live), "\x1b[31mlive") {
		t.Fatalf("live message lost ANSI bytes: %q", string(live))
	}

	_ = conn.Close()
	deadline := time.Now().Add(2 * time.Second)
	for {
		stored, ok = store.Get(terminalID)
		if ok && stored.ContentSource == "tmux_capture" && strings.Contains(stored.Content, "\x1b[31mlive") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("stored final transcript missing ANSI live bytes: ok=%t source=%q content=%q", ok, stored.ContentSource, stored.Content)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestHandleTerminalStreamRejectsCrossSiteOrigin(t *testing.T) {
	store := terminals.NewStore()
	sessionID := "session-live-attach-origin-reject"
	terminalID := sessionID + ":main:" + sessionID
	store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "main:"+sessionID, "tmux-origin-reject", "pane", 1))
	api := &StreamingAPI{
		config:        ServerConfig{CORSOrigins: []string{"loopback"}},
		terminalStore: store,
		liveAttach:    newLiveAttachManager(),
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = mux.SetURLVars(r, map[string]string{"terminal_id": terminalID})
		api.handleTerminalStream(w, r)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/stream?cols=80&rows=24"
	header := http.Header{"Origin": []string{"https://evil.example"}}
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err == nil {
		conn.Close()
		t.Fatal("dial stream succeeded from disallowed origin")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("status = %d, want %d", status, http.StatusForbidden)
	}
}

func TestHandleTerminalStreamAllowsLoopbackOrigin(t *testing.T) {
	installFakeAttach(t, seedResponder(nil, []string{"loopback seed"}, "0,0"))

	store := terminals.NewStore()
	sessionID := "session-live-attach-origin-allow"
	terminalID := sessionID + ":main:" + sessionID
	store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "main:"+sessionID, "tmux-origin-allow", "pane", 1))
	api := &StreamingAPI{
		config:        ServerConfig{CORSOrigins: []string{"loopback"}},
		terminalStore: store,
		liveAttach:    newLiveAttachManager(),
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = mux.SetURLVars(r, map[string]string{"terminal_id": terminalID})
		api.handleTerminalStream(w, r)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/stream?cols=80&rows=24"
	header := http.Header{"Origin": []string{"http://127.0.0.1:51734"}}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("dial stream from loopback origin: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, seed, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	if !strings.Contains(string(seed), "loopback seed") {
		t.Fatalf("seed = %q, want loopback seed", string(seed))
	}
}

func TestBuildLiveAttachSeedOmitsHistoryJoinArtifacts(t *testing.T) {
	// History uses no -J: the capture command must not join wrapped lines
	// (joining preserves trailing spaces, which drags background fills out to
	// full-width gray bars in scrollback).
	fake := installFakeAttach(t, seedResponder([]string{"h"}, []string{"s"}, "0,0"))
	m := newLiveAttachManager()
	st, viewer, _, err := m.addViewer(context.Background(), "sessG", 80, 24)
	if err != nil {
		t.Fatalf("addViewer: %v", err)
	}
	st.unsubscribe(viewer)
	waitStreamDone(t, st)

	for {
		select {
		case c := <-fake.commands:
			if strings.HasPrefix(c, "capture-pane") {
				if strings.Contains(c, "-J") {
					t.Fatalf("capture command uses -J: %q", c)
				}
				if strings.Contains(c, " -S ") && !strings.Contains(c, " -E -1") {
					t.Fatalf("history capture must exclude the visible screen: %q", c)
				}
			}
			continue
		default:
		}
		break
	}
}

// TestLiveAttachRealTmuxEndToEnd drives the REAL transport — tmux control-mode
// attach, in-band seed, live %output — against a scratch tmux session, and
// asserts the core seam this transport exists for: content is delivered
// EXACTLY once (the seed and the live stream neither overlap nor gap), even
// though the pane keeps printing while the viewer connects. Skipped when tmux
// is unavailable or too old.
func TestLiveAttachRealTmuxEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), terminalTmuxActionTimeout)
	ok, _ := liveAttachTmuxSupported(ctx)
	cancel()
	if !ok {
		t.Skip("tmux unavailable or too old")
	}

	session := "live-attach-e2e-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	// A pane that prints a seed marker, then streams numbered ticker lines
	// forever — the "busy CLI" a viewer connects into.
	script := `echo seed-"x"-marker; i=0; while true; do i=$((i+1)); echo tick-"x"-$i; sleep 0.05; done`
	if err := exec.Command("tmux", "new-session", "-d", "-s", session, "-x", "100", "-y", "30", "sh", "-c", script).Run(); err != nil {
		t.Skipf("cannot create tmux session: %v", err)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", session).Run() })
	time.Sleep(300 * time.Millisecond) // let the seed marker and first ticks land

	store := terminals.NewStore()
	sessionID := "session-live-attach-e2e"
	terminalID := sessionID + ":main:" + sessionID
	store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "main:"+sessionID, session, "pane", 1))
	api := &StreamingAPI{terminalStore: store, liveAttach: newLiveAttachManager()}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = mux.SetURLVars(r, map[string]string{"terminal_id": terminalID})
		api.handleTerminalStream(w, r)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/stream?cols=100&rows=30"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial stream: %v", err)
	}
	defer conn.Close()

	// Collect the seed + ~1s of live stream.
	var transcript strings.Builder
	deadline := time.Now().Add(3 * time.Second)
	_ = conn.SetReadDeadline(deadline)
	for time.Now().Before(deadline) {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if strings.Contains(err.Error(), "i/o timeout") {
				break
			}
			t.Fatalf("read stream: %v", err)
		}
		transcript.Write(msg)
		if transcript.Len() > 1<<20 {
			break
		}
	}
	got := transcript.String()

	if n := strings.Count(got, "seed-x-marker"); n != 1 {
		t.Fatalf("seed marker appeared %d times, want exactly 1 (seed/stream overlap or gap)\ntranscript tail: %q", n, tail(got, 800))
	}
	// Every ticker line the transcript contains must appear exactly once: a
	// duplicate means the seed capture and the live splice overlapped (the
	// stacked-spinner bug); tick numbers may be partially cut at both ends of
	// the observation window, so only fully-framed lines are counted.
	ticks := map[int]int{}
	for _, m := range strings.Split(got, "\n") {
		m = strings.TrimSpace(stripAnsiForTest(m))
		if !strings.HasPrefix(m, "tick-x-") {
			continue
		}
		if n, err := strconv.Atoi(strings.TrimPrefix(m, "tick-x-")); err == nil {
			ticks[n]++
		}
	}
	if len(ticks) < 5 {
		t.Fatalf("too few ticker lines observed (%d); stream not live?\ntranscript tail: %q", len(ticks), tail(got, 800))
	}
	// No duplicates: a duplicated tick means the seed capture and the live
	// splice overlapped (the stacked-spinner bug).
	for n, c := range ticks {
		if c > 1 {
			t.Errorf("tick %d appeared %d times (duplication)", n, c)
		}
	}
	// No gap: between the lowest and highest fully-framed tick, every number
	// must be present. A missing middle tick means %output fell into the seed's
	// capture->splice gap and was dropped, permanently desyncing xterm (the
	// exact corruption this transport must not produce). Tick numbers may be
	// clipped only at the two ends of the observation window.
	lo, hi := 1<<30, 0
	for n := range ticks {
		if n < lo {
			lo = n
		}
		if n > hi {
			hi = n
		}
	}
	var missing []int
	for n := lo; n <= hi; n++ {
		if ticks[n] == 0 {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		t.Errorf("gap in ticker stream: missing %v within observed range [%d,%d]\ntranscript tail: %q", missing, lo, hi, tail(got, 800))
	}
	if !t.Failed() {
		t.Logf("verified contiguous ticks [%d,%d], %d lines, zero dup/gap", lo, hi, len(ticks))
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;:]*[A-Za-z]|\x1b.`)

func stripAnsiForTest(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

func TestParseLiveAttachCursor(t *testing.T) {
	cases := []struct {
		reply  liveattach.Reply
		wantX  int
		wantY  int
		wantOK bool
	}{
		{liveattach.Reply{Lines: []string{"4,2"}}, 4, 2, true},
		{liveattach.Reply{Lines: []string{" 10 , 0 "}}, 10, 0, true},
		{liveattach.Reply{Lines: []string{"nope"}}, 0, 0, false},
		{liveattach.Reply{Lines: []string{"-1,3"}}, 0, 0, false},
		{liveattach.Reply{Lines: nil}, 0, 0, false},
		{liveattach.Reply{Err: true, Lines: []string{"4,2"}}, 0, 0, false},
	}
	for _, tc := range cases {
		x, y, ok := parseLiveAttachCursor(tc.reply)
		if x != tc.wantX || y != tc.wantY || ok != tc.wantOK {
			t.Errorf("parseLiveAttachCursor(%+v) = (%d,%d,%v), want (%d,%d,%v)", tc.reply, x, y, ok, tc.wantX, tc.wantY, tc.wantOK)
		}
	}
}

// windowSizeResponder answers the layout-change reconcile query with the given
// geometry, delegating everything else to the standard seed responder.
func windowSizeResponder(cols, rows int, screen []string) func(string) liveattach.Reply {
	seed := seedResponder(nil, screen, "0,0")
	return func(cmd string) liveattach.Reply {
		if strings.Contains(cmd, "#{window_width}") {
			return liveattach.Reply{Lines: []string{fmt.Sprintf("%d,%d", cols, rows)}}
		}
		return seed(cmd)
	}
}

func viewerClosed(t *testing.T, ch chan []byte) bool {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return true
			}
			// Drain any live bytes and keep waiting for the close.
		case <-deadline:
			return false
		}
	}
}

func TestLiveAttachLayoutChangeDropsViewersOnExternalResize(t *testing.T) {
	// tmux reports a window size this stream never applied: something else
	// (an operator, an adapter, another client) resized the window. Bytes are
	// now wrapped for a width the viewer's xterm does not have, so the viewer
	// must be dropped to force a reconnect + re-seed at the real geometry.
	installFakeAttach(t, windowSizeResponder(80, 24, []string{"screen"}))
	m := newLiveAttachManager()
	st, viewer, _, err := m.addViewer(context.Background(), "sessLayout", 120, 36)
	if err != nil {
		t.Fatalf("addViewer: %v", err)
	}

	st.handleLayoutChange()

	if !viewerClosed(t, viewer.ch) {
		t.Fatal("viewer channel still open after an external geometry change")
	}
	st.mu.Lock()
	appliedCols, appliedRows := st.appliedCols, st.appliedRows
	st.mu.Unlock()
	// The applied-size cache must be invalidated too: otherwise the next seed
	// takes resizeToThen's same-size fast path and skips the resize-window it
	// now needs, re-seeding at the wrong geometry.
	if appliedCols != 0 || appliedRows != 0 {
		t.Fatalf("applied geometry = %dx%d, want invalidated (0x0)", appliedCols, appliedRows)
	}
	waitStreamDone(t, st)
}

func TestLiveAttachLayoutChangeIgnoresSelfInflictedResize(t *testing.T) {
	// Our OWN resize-window also emits %layout-change. tmux completes a command
	// before flushing notifications, so appliedCols already matches by the time
	// the reconcile query is answered — the viewer must survive, or every
	// connect would immediately drop and reconnect itself.
	installFakeAttach(t, windowSizeResponder(120, 36, []string{"screen"}))
	m := newLiveAttachManager()
	st, viewer, _, err := m.addViewer(context.Background(), "sessSelf", 120, 36)
	if err != nil {
		t.Fatalf("addViewer: %v", err)
	}

	st.handleLayoutChange()

	// Give the reconcile query time to round-trip, then prove the viewer is
	// still spliced into the broadcast.
	time.Sleep(200 * time.Millisecond)
	st.broadcast([]byte("still-live"))
	select {
	case b, ok := <-viewer.ch:
		if !ok {
			t.Fatal("viewer dropped by a self-inflicted layout change")
		}
		if string(b) != "still-live" {
			t.Fatalf("got %q, want still-live", string(b))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("viewer did not receive live bytes after layout change")
	}
	st.mu.Lock()
	appliedCols := st.appliedCols
	st.mu.Unlock()
	if appliedCols != 120 {
		t.Fatalf("applied cols = %d, want 120 preserved", appliedCols)
	}

	st.unsubscribe(viewer)
	waitStreamDone(t, st)
}

func TestClampLiveAttachGeometry(t *testing.T) {
	if _, _, ok := clampLiveAttachGeometry(20, 8); ok {
		t.Fatal("below-minimum geometry must be rejected")
	}
	cols, rows, ok := clampLiveAttachGeometry(120, 36)
	if !ok || cols != 120 || rows != 36 {
		t.Fatalf("in-range geometry = %dx%d ok=%v, want 120x36 ok=true", cols, rows, ok)
	}
	// An absurd client grid becomes a real resize-window and makes every later
	// capture-pane proportionally huge; clamp instead of trusting it.
	cols, rows, ok = clampLiveAttachGeometry(5000, 2000)
	if !ok || cols != liveAttachMaxCols || rows != liveAttachMaxRows {
		t.Fatalf("oversized geometry = %dx%d ok=%v, want %dx%d ok=true",
			cols, rows, ok, liveAttachMaxCols, liveAttachMaxRows)
	}
}

func TestLiveAttachInitialSizeClampsOversizedGeometry(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/stream?cols=5000&rows=2000", nil)
	cols, rows := liveAttachInitialSize(req)
	if cols != liveAttachMaxCols || rows != liveAttachMaxRows {
		t.Fatalf("initial size = %dx%d, want clamp to %dx%d", cols, rows, liveAttachMaxCols, liveAttachMaxRows)
	}
}

func TestLiveAttachRawInputChunksLargePaste(t *testing.T) {
	// Each byte becomes its own send-keys -H argument, so a large paste must be
	// split across commands or the argv blows past ARG_MAX and never reaches
	// the pane.
	var mu sync.Mutex
	var calls [][]string
	origRun := runTerminalTmuxCommand
	runTerminalTmuxCommand = func(ctx context.Context, stdin string, args ...string) error {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, append([]string(nil), args...))
		return nil
	}
	t.Cleanup(func() { runTerminalTmuxCommand = origRun })

	api := &StreamingAPI{}
	payload := bytes.Repeat([]byte("a"), liveAttachRawInputChunkBytes*2+7)
	api.liveAttachRawInput(context.Background(), "sessPaste", payload)

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 3 {
		t.Fatalf("send-keys call count = %d, want 3 chunks", len(calls))
	}
	var got []byte
	for _, args := range calls {
		if len(args) < 5 || args[0] != "send-keys" || args[3] != "-H" {
			t.Fatalf("unexpected send-keys argv: %#v", args[:min(len(args), 6)])
		}
		if hexBytes := len(args) - 4; hexBytes > liveAttachRawInputChunkBytes {
			t.Fatalf("chunk carried %d bytes, want <= %d", hexBytes, liveAttachRawInputChunkBytes)
		}
		for _, h := range args[4:] {
			var b byte
			if _, err := fmt.Sscanf(h, "%02x", &b); err != nil {
				t.Fatalf("bad hex argument %q: %v", h, err)
			}
			got = append(got, b)
		}
	}
	// Chunking must be transparent: the pane sees the exact original bytes.
	if !bytes.Equal(got, payload) {
		t.Fatalf("reassembled payload (%d bytes) != original (%d bytes)", len(got), len(payload))
	}
}

// TestLiveAttachRealTmuxExternalResizeDropsViewer covers the SCANNER WIRING for
// %layout-change, which the fake control channel cannot exercise: it replaces
// runControlMode wholesale, so nothing there ever classifies a real
// notification line. Here a genuine `tmux resize-window` from outside the
// transport must reach runControlMode's event branch and drop the viewer.
func TestLiveAttachRealTmuxExternalResizeDropsViewer(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), terminalTmuxActionTimeout)
	ok, _ := liveAttachTmuxSupported(ctx)
	cancel()
	if !ok {
		t.Skip("tmux unavailable or too old")
	}

	session := "live-attach-resize-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	script := `while true; do echo tick; sleep 0.05; done`
	if err := exec.Command("tmux", "new-session", "-d", "-s", session, "-x", "100", "-y", "30", "sh", "-c", script).Run(); err != nil {
		t.Skipf("cannot create tmux session: %v", err)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", session).Run() })
	time.Sleep(300 * time.Millisecond)

	m := newLiveAttachManager()
	st, viewer, seed, err := m.addViewer(context.Background(), session, 100, 30)
	if err != nil {
		t.Fatalf("addViewer: %v", err)
	}
	if len(seed) == 0 {
		t.Fatal("empty seed")
	}
	t.Cleanup(func() { st.stop() })

	// Something other than this transport changes the window. tmux now wraps
	// output at 80 columns while the viewer's xterm is still on a 100-column
	// grid — the desync that must not survive.
	if err := exec.Command("tmux", "resize-window", "-t", session, "-x", "80", "-y", "24").Run(); err != nil {
		t.Skipf("cannot resize tmux window: %v", err)
	}

	if !viewerClosed(t, viewer.ch) {
		t.Fatal("viewer survived an external tmux resize; %layout-change was not reconciled")
	}
}

// TestHandleTerminalStreamSupersededCloseCode is the end-to-end half of the
// single-viewer rule: the evicted socket must close with the DISTINGUISHABLE
// code, because that code is the only thing telling the frontend not to
// reconnect. A generic going-away here would make two open tabs evict each
// other in a ~500ms loop (a successful seed resets the client's backoff), each
// round re-seeding the full scrollback.
func TestHandleTerminalStreamSupersededCloseCode(t *testing.T) {
	installFakeAttach(t, seedResponder(nil, []string{"screen"}, "0,0"))

	store := terminals.NewStore()
	sessionID := "session-live-attach-supersede"
	terminalID := sessionID + ":main:" + sessionID
	tmuxSession := "tmux-live-attach-supersede"
	store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "main:"+sessionID, tmuxSession, "pane", 1))
	api := &StreamingAPI{terminalStore: store, liveAttach: newLiveAttachManager()}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = mux.SetURLVars(r, map[string]string{"terminal_id": terminalID})
		api.handleTerminalStream(w, r)
	}))
	defer server.Close()
	base := "ws" + strings.TrimPrefix(server.URL, "http") + "/stream"

	first, _, err := websocket.DefaultDialer.Dial(base+"?cols=120&rows=36", nil)
	if err != nil {
		t.Fatalf("dial first viewer: %v", err)
	}
	defer first.Close()
	_ = first.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, _, err := first.ReadMessage(); err != nil {
		t.Fatalf("first viewer seed: %v", err)
	}

	gotCode := make(chan int, 1)
	first.SetCloseHandler(func(code int, text string) error {
		gotCode <- code
		return nil
	})

	// A second tab opens the same terminal at a different grid.
	second, _, err := websocket.DefaultDialer.Dial(base+"?cols=80&rows=24", nil)
	if err != nil {
		t.Fatalf("dial second viewer: %v", err)
	}
	defer second.Close()
	_ = second.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, _, err := second.ReadMessage(); err != nil {
		t.Fatalf("second viewer seed: %v", err)
	}

	// Draining the first connection surfaces the close frame to its handler.
	go func() {
		for {
			if _, _, err := first.ReadMessage(); err != nil {
				return
			}
		}
	}()

	select {
	case code := <-gotCode:
		if code != liveAttachSupersededCloseCode {
			t.Fatalf("close code = %d, want %d (superseded)", code, liveAttachSupersededCloseCode)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("evicted viewer never received a close frame")
	}
}

// TestLiveAttachResizeSettlesUnderContinuousOutput pins the busy-repaint path.
// A CLI streaming without pause never produces a quiet window, so the seed used
// to wait the full liveAttachResizeMaxWait on EVERY geometry reconnect — which
// is now an ordinary layout change, not a rare event.
func TestLiveAttachResizeSettlesUnderContinuousOutput(t *testing.T) {
	st := &liveAttachStream{
		tmuxSession: "busy",
		subs:        make(map[*liveAttachViewer]struct{}),
		done:        make(chan struct{}),
		writeCh:     make(chan *liveAttachCmd, 4),
	}
	t.Cleanup(func() { close(st.done) })

	// A writer that never stops: lastOutputAt keeps advancing, so the quiet
	// window can never be reached.
	stopNoise := make(chan struct{})
	defer close(stopNoise)
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopNoise:
				return
			case <-ticker.C:
				st.mu.Lock()
				st.lastOutputAt = time.Now()
				st.mu.Unlock()
			}
		}
	}()

	resizeStartedAt := time.Now()
	time.Sleep(20 * time.Millisecond) // let output post-date the resize
	done := make(chan time.Duration, 1)
	started := time.Now()
	go st.waitForResizeRepaint(resizeStartedAt, func() { done <- time.Since(started) })

	select {
	case waited := <-done:
		if waited >= liveAttachResizeMaxWait {
			t.Fatalf("waited %v under continuous output; want release near %v, not the %v ceiling",
				waited, liveAttachResizeBusyRepaintWait, liveAttachResizeMaxWait)
		}
		if waited < liveAttachResizeBusyRepaintWait-40*time.Millisecond {
			t.Fatalf("released after %v; want at least ~%v so the CLI has repainted",
				waited, liveAttachResizeBusyRepaintWait)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("resize settle never completed")
	}
}
