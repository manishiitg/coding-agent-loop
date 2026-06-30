package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	"mcp-agent-builder-go/agent_go/internal/terminals"
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

// withFakeAttach swaps the real control-mode loop for one that simply blocks
// until the stream's context is canceled, so manager lifecycle can be tested
// without a real tmux session. It restores the original on cleanup.
func withFakeAttach(t *testing.T) {
	t.Helper()
	orig := liveAttachAttachFn
	liveAttachAttachFn = func(st *liveAttachStream, ctx context.Context) {
		st.markInitialDrainComplete()
		<-ctx.Done()
	}
	origRun := runTerminalTmuxCommand
	runTerminalTmuxCommand = func(ctx context.Context, stdin string, args ...string) error {
		return nil
	}
	t.Cleanup(func() {
		liveAttachAttachFn = orig
		runTerminalTmuxCommand = origRun
	})
}

func TestLiveAttachManagerSubscribeBroadcastUnsubscribe(t *testing.T) {
	withFakeAttach(t)
	m := newLiveAttachManager()

	st1, ch1 := m.subscribe("sessA", 100, 40)
	if st1 == nil || ch1 == nil {
		t.Fatal("first subscribe returned nil")
	}

	// A second subscriber to the same session shares the same stream.
	st2, ch2 := m.subscribe("sessA", 100, 40)
	if st2 != st1 {
		t.Fatal("second subscribe created a new stream for the same session")
	}

	// broadcast fans out to both subscribers.
	st1.broadcast([]byte("hello"))
	for i, ch := range []chan []byte{ch1, ch2} {
		select {
		case b := <-ch:
			if string(b) != "hello" {
				t.Fatalf("sub %d got %q, want %q", i, b, "hello")
			}
		case <-time.After(time.Second):
			t.Fatalf("sub %d did not receive broadcast", i)
		}
	}

	// Unsubscribing one viewer keeps the stream alive.
	st1.unsubscribe(ch1)
	if _, closed := <-ch1; closed {
		t.Fatal("ch1 should be closed after unsubscribe")
	}
	m.mu.Lock()
	_, stillThere := m.sessions["sessA"]
	m.mu.Unlock()
	if !stillThere {
		t.Fatal("stream removed while a subscriber remains")
	}

	// Unsubscribing the last viewer stops the stream and removes it.
	st2.unsubscribe(ch2)
	select {
	case <-st2.done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not stop after last unsubscribe")
	}
	m.mu.Lock()
	_, gone := m.sessions["sessA"]
	m.mu.Unlock()
	if gone {
		t.Fatal("stream not removed from manager after last unsubscribe")
	}
}

func TestLiveAttachManagerResubscribeAfterDeath(t *testing.T) {
	withFakeAttach(t)
	m := newLiveAttachManager()

	st1, ch1 := m.subscribe("sessB", 80, 24)
	st1.unsubscribe(ch1) // last unsubscribe -> stop
	select {
	case <-st1.done:
	case <-time.After(2 * time.Second):
		t.Fatal("first stream did not stop")
	}

	// Subscribing again must create a fresh stream (the old one was dropped).
	st2, ch2 := m.subscribe("sessB", 80, 24)
	if st2 == st1 {
		t.Fatal("resubscribe reused a dead stream")
	}
	if st2 == nil || ch2 == nil {
		t.Fatal("resubscribe returned nil")
	}
	st2.unsubscribe(ch2)
	// Wait for the attach goroutine to exit before the test (and its cleanup,
	// which restores liveAttachAttachFn) returns.
	select {
	case <-st2.done:
	case <-time.After(2 * time.Second):
		t.Fatal("second stream did not stop")
	}
}

func TestLiveAttachManagerSubscribeEmptySession(t *testing.T) {
	withFakeAttach(t)
	m := newLiveAttachManager()
	st, ch := m.subscribe("   ", 80, 24)
	if st != nil || ch != nil {
		t.Fatal("expected nil for empty session name")
	}
}

func TestLiveAttachBroadcastDropsSlowViewer(t *testing.T) {
	withFakeAttach(t)
	m := newLiveAttachManager()
	st, ch := m.subscribe("sessC", 80, 24)

	// Overfill the subscriber buffer; broadcast must not block or panic.
	for i := 0; i < liveAttachSubBuffer+50; i++ {
		st.broadcast([]byte("x"))
	}
	// The channel holds at most its buffer; extra chunks were dropped.
	if got := len(ch); got > liveAttachSubBuffer {
		t.Fatalf("channel length %d exceeds buffer %d", got, liveAttachSubBuffer)
	}

	// Stop and wait for the attach goroutine to exit before cleanup restores
	// liveAttachAttachFn.
	st.unsubscribe(ch)
	select {
	case <-st.done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not stop")
	}
}

func TestLiveAttachManagerDrainsInitialAttachBeforeSubscriber(t *testing.T) {
	orig := liveAttachAttachFn
	liveAttachAttachFn = func(st *liveAttachStream, ctx context.Context) {
		st.broadcast([]byte("initial-repaint"))
		st.markInitialDrainComplete()
		<-ctx.Done()
	}
	origRun := runTerminalTmuxCommand
	runTerminalTmuxCommand = func(ctx context.Context, stdin string, args ...string) error {
		return nil
	}
	t.Cleanup(func() {
		liveAttachAttachFn = orig
		runTerminalTmuxCommand = origRun
	})

	m := newLiveAttachManager()
	st, ch := m.subscribe("sessD", 100, 40)
	if st == nil || ch == nil {
		t.Fatal("subscribe returned nil")
	}
	select {
	case b := <-ch:
		t.Fatalf("subscriber received initial attach repaint %q; want drained", string(b))
	default:
	}

	st.broadcast([]byte("live"))
	select {
	case b := <-ch:
		if string(b) != "live" {
			t.Fatalf("got %q, want live", string(b))
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive post-subscribe live bytes")
	}

	st.unsubscribe(ch)
	select {
	case <-st.done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not stop")
	}
}

func TestHandleTerminalStreamClosesWhenAttachDiesDuringWarmup(t *testing.T) {
	origAttach := liveAttachAttachFn
	liveAttachAttachFn = func(st *liveAttachStream, ctx context.Context) {
		st.markInitialDrainComplete()
		// Simulate a missing/dead tmux target: the control-mode client exits
		// before subscribe has added a WebSocket subscriber.
	}
	origRun := runTerminalTmuxCommand
	runTerminalTmuxCommand = func(ctx context.Context, stdin string, args ...string) error {
		return nil
	}
	t.Cleanup(func() {
		liveAttachAttachFn = origAttach
		runTerminalTmuxCommand = origRun
	})

	store := terminals.NewStore()
	sessionID := "session-live-attach-dead-warmup"
	terminalID := sessionID + ":main:" + sessionID
	store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "main:"+sessionID, "tmux-dead-warmup", "old pane", 1))
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
		t.Fatal("ReadMessage succeeded; want stream websocket to close when attach dies during warmup")
	} else if strings.Contains(err.Error(), "i/o timeout") {
		t.Fatalf("stream stayed open after attach died during warmup: %v", err)
	}
}

func TestLiveAttachSetSizeSkipsDuplicateResize(t *testing.T) {
	var commands []string
	origRun := runTerminalTmuxCommand
	runTerminalTmuxCommand = func(ctx context.Context, stdin string, args ...string) error {
		commands = append(commands, strings.Join(args, " "))
		return nil
	}
	outputCalls := 0
	origRunOutput := runTerminalTmuxOutputCommand
	runTerminalTmuxOutputCommand = func(ctx context.Context, args ...string) (string, error) {
		outputCalls++
		return "120\t36", nil
	}
	t.Cleanup(func() {
		runTerminalTmuxCommand = origRun
		runTerminalTmuxOutputCommand = origRunOutput
	})

	st := &liveAttachStream{tmuxSession: "sessE"}
	st.setSize(120, 36)
	st.setSize(120, 36)
	st.setSize(121, 36)
	if outputCalls != 1 {
		t.Fatalf("terminalWindowSize calls = %d, want 1 duplicate-size guard check", outputCalls)
	}
	if got, want := len(commands), 4; got != want {
		t.Fatalf("tmux command calls = %d, want %d: %#v", got, want, commands)
	}
	if commands[0] != "set-window-option -t sessE window-size manual" || commands[1] != "resize-window -t sessE -x 120 -y 36" {
		t.Fatalf("first resize commands = %#v", commands[:2])
	}
	if commands[2] != "set-window-option -t sessE window-size manual" || commands[3] != "resize-window -t sessE -x 121 -y 36" {
		t.Fatalf("second resize commands = %#v", commands[2:])
	}
}

func TestLiveAttachSetSizeAppliesBeforePtyExists(t *testing.T) {
	var commands []string
	origRun := runTerminalTmuxCommand
	runTerminalTmuxCommand = func(ctx context.Context, stdin string, args ...string) error {
		commands = append(commands, strings.Join(args, " "))
		return nil
	}
	t.Cleanup(func() {
		runTerminalTmuxCommand = origRun
	})

	st := &liveAttachStream{tmuxSession: "sess-no-pty-yet"}
	st.setSize(118, 36)
	if got, want := len(commands), 2; got != want {
		t.Fatalf("tmux command calls before PTY exists = %d, want %d: %#v", got, want, commands)
	}
	if commands[0] != "set-window-option -t sess-no-pty-yet window-size manual" ||
		commands[1] != "resize-window -t sess-no-pty-yet -x 118 -y 36" {
		t.Fatalf("resize commands before PTY exists = %#v", commands)
	}
}

func TestLiveAttachBackfillSeedsScrollbackThenVisibleSnapshot(t *testing.T) {
	origRun := runTerminalTmuxOutputCommand
	var commands []string
	runTerminalTmuxOutputCommand = func(ctx context.Context, args ...string) (string, error) {
		joined := strings.Join(args, " ")
		commands = append(commands, joined)
		if strings.Contains(joined, " -S ") {
			if !strings.Contains(joined, " -E -1") {
				t.Fatalf("history backfill must exclude the visible screen, got args: %s", joined)
			}
			return "old-one\nold-two\n", nil
		}
		return "cur-one\ncur-two\n", nil
	}
	t.Cleanup(func() { runTerminalTmuxOutputCommand = origRun })

	got := string(liveAttachBackfill(context.Background(), "sessF"))
	if len(commands) != 2 {
		t.Fatalf("tmux capture calls = %#v, want history then visible", commands)
	}
	if !strings.HasPrefix(got, "\x1bc") {
		t.Fatalf("backfill prefix = %q, want RIS reset", got[:min(len(got), 8)])
	}
	if strings.Count(got, "\x1bc") != 1 {
		t.Fatalf("backfill should hard-reset only before scrollback seed: %q", got)
	}
	historyAt := strings.Index(got, "old-one\r\nold-two\r\n")
	clearAt := strings.Index(got, "\x1b[H\x1b[2J")
	visibleAt := strings.Index(got, "cur-one\r\ncur-two\r\n")
	if historyAt < 0 || clearAt < 0 || visibleAt < 0 || !(historyAt < clearAt && clearAt < visibleAt) {
		t.Fatalf("backfill should seed history, clear viewport, then paint current screen: %q", got)
	}
}
