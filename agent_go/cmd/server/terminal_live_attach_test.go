package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLiveAttachEnabled(t *testing.T) {
	for _, val := range []string{"", "0", "false", "off", "random", "1", "true"} {
		t.Setenv("RUNLOOP_TERMINAL_LIVE_ATTACH", val)
		if got := liveAttachEnabled(); !got {
			t.Errorf("liveAttachEnabled() with %q = false, want true", val)
		}
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

func TestLiveAttachSetSizeSkipsDuplicateResize(t *testing.T) {
	calls := 0
	origRun := runTerminalTmuxCommand
	runTerminalTmuxCommand = func(ctx context.Context, stdin string, args ...string) error {
		calls++
		return nil
	}
	t.Cleanup(func() { runTerminalTmuxCommand = origRun })

	st := &liveAttachStream{tmuxSession: "sessE"}
	st.setSize(120, 36)
	st.setSize(120, 36)
	st.setSize(121, 36)
	if calls != 2 {
		t.Fatalf("resize calls = %d, want 2", calls)
	}
}
