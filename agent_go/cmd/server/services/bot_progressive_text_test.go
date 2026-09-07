package services

import (
	"context"
	"sync"
	"testing"
	"time"
)

// While a turn runs, each newly completed assistant text turn (read from
// durable conversation history) goes out as its own message, in order, and
// nothing already sent goes out again on a later poll that sees the same
// turns plus one more.
func TestPollProgressiveTextOnceSendsOnlyNewTurns(t *testing.T) {
	manager := NewBotConversationManager(nil, "", "")
	connector := &testBotConnector{}
	threadID := ThreadID{Platform: "whatsapp", ChannelID: "dm", ThreadTS: "dm"}
	filter := NewBotEventFilter(connector, threadID, "sess-1", "", "user-1")

	texts := []string{"Just filing what you sent over this morning."}
	manager.chatHistory = func(context.Context, string, string) ([]string, error) { return texts, nil }

	sent := manager.pollProgressiveTextOnce(context.Background(), filter, "user-1", "sess-1", 0)
	if sent != 1 || len(connector.sent) != 1 || connector.sent[0] != texts[0] {
		t.Fatalf("after 1 turn: sent=%d messages=%v", sent, connector.sent)
	}

	// A poll that sees the exact same turns again sends nothing new.
	sent = manager.pollProgressiveTextOnce(context.Background(), filter, "user-1", "sess-1", sent)
	if sent != 1 || len(connector.sent) != 1 {
		t.Fatalf("repeat poll with no new turns sent %v, want no additional message", connector.sent)
	}

	// A new turn appears; only it goes out.
	texts = append(texts, "Now filing the six items with their transcripts.")
	sent = manager.pollProgressiveTextOnce(context.Background(), filter, "user-1", "sess-1", sent)
	if sent != 2 || len(connector.sent) != 2 || connector.sent[1] != texts[1] {
		t.Fatalf("after 2nd turn: sent=%d messages=%v", sent, connector.sent)
	}
}

// A long-lived WhatsApp conversation's earlier turns are already sent —
// pollProgressiveText must baseline against what's in conversation_history
// when a new turn starts, not against zero, or every old reply replays as
// "new" the moment the next turn begins. This is the exact bug seen live:
// four already-delivered messages from a prior turn all re-sent within a
// second of a new voice message starting.
func TestPollProgressiveTextBaselinesAgainstExistingHistory(t *testing.T) {
	manager := NewBotConversationManager(nil, "", "")
	connector := &testBotConnector{}
	threadID := ThreadID{Platform: "whatsapp", ChannelID: "dm", ThreadTS: "dm"}
	active := &activeBotSession{
		SessionID: "sess-1",
		UserID:    "user-1",
		Platform:  "whatsapp",
		ThreadID:  threadID,
	}
	active.eventFilter = NewBotEventFilter(connector, threadID, "sess-1", "", "user-1")

	var mu sync.Mutex
	texts := []string{"Just filing what you sent over this morning.", "Still transcribing the voice notes.", "Now filing the six items.", "Hi! Good morning."}
	manager.chatHistory = func(context.Context, string, string) ([]string, error) {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), texts...), nil
	}

	oldInterval := progressiveTextPollInterval
	progressiveTextPollInterval = 10 * time.Millisecond
	defer func() { progressiveTextPollInterval = oldInterval }()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { manager.pollProgressiveText(ctx, active); close(done) }()

	// Give the baseline read and a few ticks time to run against the
	// pre-existing history — none of it should go out.
	time.Sleep(60 * time.Millisecond)
	if sent := connector.snapshot(); len(sent) != 0 {
		t.Fatalf("pre-existing turns were sent as if new: %v", sent)
	}

	// A genuinely new turn appears after the turn started; it goes out.
	mu.Lock()
	texts = append(texts, "The math worksheet has been filed under Fractions.")
	mu.Unlock()
	time.Sleep(60 * time.Millisecond)
	cancel()
	<-done

	sent := connector.snapshot()
	if len(sent) != 1 || sent[0] != "The math worksheet has been filed under Fractions." {
		t.Fatalf("sent = %v, want only the turn added after the poller started", sent)
	}
}

// snapshot reads sent under mu — testBotConnector's own field access isn't
// synchronized, and this test sends from a background poller goroutine
// while asserting from the test goroutine (every other use of
// testBotConnector elsewhere is single-threaded, so the type itself is
// left as-is).
func (c *testBotConnector) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.sent...)
}

// A turn with only a tool call (no text) contributes nothing to send — the
// reader itself is expected to omit it, and a poll over an unchanged list
// stays a no-op either way.
func TestPollProgressiveTextOnceReaderErrorIsNotFatal(t *testing.T) {
	manager := NewBotConversationManager(nil, "", "")
	connector := &testBotConnector{}
	threadID := ThreadID{Platform: "whatsapp", ChannelID: "dm", ThreadTS: "dm"}
	filter := NewBotEventFilter(connector, threadID, "sess-1", "", "user-1")

	manager.chatHistory = func(context.Context, string, string) ([]string, error) {
		return nil, context.DeadlineExceeded
	}
	sent := manager.pollProgressiveTextOnce(context.Background(), filter, "user-1", "sess-1", 3)
	if sent != 3 || len(connector.sent) != 0 {
		t.Fatalf("a reader error changed sent=%d or sent a message %v", sent, connector.sent)
	}
}

// The turn's own final completion must still go out even after the poller
// already sent an earlier, different reply for the same turn — content
// dedup (ShouldSendSyntheticFinal), not a one-shot flag, is what makes this
// safe: SendProgressiveText and the generation_end/unified_completion path
// share the same "already sent this exact text" check.
func TestFinalCompletionStillSendsAfterProgressiveText(t *testing.T) {
	connector := &testBotConnector{}
	threadID := ThreadID{Platform: "whatsapp", ChannelID: "dm", ThreadTS: "dm"}
	filter := NewBotEventFilter(connector, threadID, "sess-1", "", "user-1")

	if !filter.SendProgressiveText(context.Background(), "Still transcribing the voice notes.") {
		t.Fatal("first progressive text was not sent")
	}
	finalText := "Hi! Good morning, and all the best to Myra for today's Maths paper."
	if !filter.ShouldSendSyntheticFinal(finalText) {
		t.Fatal("a genuinely different final reply was suppressed by the earlier progressive send")
	}
	filter.MarkMainTextSent(finalText)
	if _, err := connector.SendThreadMessage(context.Background(), threadID, finalText); err != nil {
		t.Fatal(err)
	}
	if len(connector.sent) != 2 || connector.sent[1] != finalText {
		t.Fatalf("sent = %v, want the progressive text then the distinct final reply", connector.sent)
	}

	// But the same text arriving a second time (the poller having already
	// caught the true final turn moments before generation_end/
	// unified_completion fires) is correctly suppressed as a duplicate.
	if filter.ShouldSendSyntheticFinal(finalText) {
		t.Fatal("an exact repeat of the last sent text was not deduped")
	}
}
