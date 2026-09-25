package services

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// Limits for the follow-up "new messages" block. Alerts from apps like
// Sentry can be long; the block is context, not a transcript dump.
const (
	threadCatchupMaxMessages     = 20
	threadCatchupMaxMessageChars = 2000
	threadCatchupMaxTotalChars   = 8000
	threadCatchupWatermarkMaxAge = 30 * 24 * time.Hour
)

// threadSeenTracker remembers, per bot thread, the newest thread message the
// agent has already been given (through the first-turn history prepend or an
// earlier follow-up's catch-up block). It is keyed by thread, not session, so
// it survives a completed session being continued under a new activeBotSession.
type threadSeenTracker struct {
	mu   sync.Mutex
	seen map[string]threadSeenMark
}

type threadSeenMark struct {
	ts        time.Time // newest thread message given to the agent
	updatedAt time.Time // wall clock of the update, for pruning idle threads
}

func (t *threadSeenTracker) get(key string) (time.Time, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	mark, ok := t.seen[key]
	return mark.ts, ok
}

// advance moves the watermark forward (never back).
func (t *threadSeenTracker) advance(key string, ts time.Time) {
	if ts.IsZero() {
		return
	}
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.seen == nil {
		t.seen = make(map[string]threadSeenMark)
	}
	if prev, ok := t.seen[key]; ok && !ts.After(prev.ts) {
		return
	}
	t.seen[key] = threadSeenMark{ts: ts, updatedAt: now}
	cutoff := now.Add(-threadCatchupWatermarkMaxAge)
	for k, v := range t.seen {
		if v.updatedAt.Before(cutoff) {
			delete(t.seen, k)
		}
	}
}

// markThreadHistorySeen records that every message in history has been given
// to the agent (first-turn prepend).
func (m *BotConversationManager) markThreadHistorySeen(threadID ThreadID, history []ThreadMessage) {
	var newest time.Time
	for _, msg := range history {
		if msg.Timestamp.After(newest) {
			newest = msg.Timestamp
		}
	}
	m.threadSeen.advance(threadID.Key(), newest)
}

// threadCatchupContext returns a "new messages in this thread" block for a
// follow-up turn in an existing threaded bot conversation: everything posted
// in the thread since the agent last saw it, excluding this bot's own posts
// and the current message. Other people's messages and other apps' posts
// (Sentry alerts, GitHub notifications) are included. Returns "" when there
// is nothing new or the history cannot be read (the turn continues without it).
func (m *BotConversationManager) threadCatchupContext(msg BotIncomingMessage, threadID ThreadID) string {
	connector := m.GetConnector(threadID.Platform)
	if connector == nil || !connector.Capabilities().Threads || threadID.ThreadTS == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	history, err := connector.GetThreadHistory(ctx, threadID)
	if err != nil {
		log.Printf("[BOT_MANAGER] Follow-up thread catch-up skipped: failed to load thread history for %s: %v", threadID.Key(), err)
		return ""
	}

	key := threadID.Key()
	// Lower bound: the newest message already given to the agent. Without
	// one (the conversation was continued after a server restart, or began
	// without a history prepend) fall back to this bot's latest post in the
	// thread: its reply, and what it answered, are in the conversation.
	lower, known := m.threadSeen.get(key)
	if !known {
		for _, h := range history {
			if h.IsSelf && h.Timestamp.After(lower) {
				lower = h.Timestamp
			}
		}
	}
	// Upper bound: the current message. Anything posted after it is left for
	// the next turn.
	current, hasCurrent := parseSlackTS(msg.MessageTS)
	if strings.TrimSpace(msg.MessageTS) == "" {
		hasCurrent = false
	}

	var fresh []ThreadMessage
	var newest time.Time
	for _, h := range history {
		if msg.MessageTS != "" && h.TS == msg.MessageTS {
			if h.Timestamp.After(newest) {
				newest = h.Timestamp
			}
			continue
		}
		if !h.Timestamp.After(lower) {
			continue
		}
		if hasCurrent && !h.Timestamp.Before(current) {
			continue
		}
		if h.Timestamp.After(newest) {
			newest = h.Timestamp
		}
		if h.IsSelf {
			continue
		}
		fresh = append(fresh, h)
	}
	// Without a platform id for the current message, drop the latest human
	// message carrying the same text: that is the message being answered.
	if !hasCurrent {
		want := strings.TrimSpace(msg.Text)
		for i := len(fresh) - 1; i >= 0; i-- {
			if !fresh[i].IsBot && strings.TrimSpace(fresh[i].Text) == want {
				fresh = append(fresh[:i], fresh[i+1:]...)
				break
			}
		}
	}
	if hasCurrent && current.After(newest) {
		newest = current
	}
	m.threadSeen.advance(key, newest)

	var kept []ThreadMessage
	for _, h := range fresh {
		if strings.TrimSpace(h.Text) == "" {
			continue
		}
		kept = append(kept, h)
	}
	if len(kept) == 0 {
		return ""
	}
	omitted := 0
	if len(kept) > threadCatchupMaxMessages {
		omitted = len(kept) - threadCatchupMaxMessages
		kept = kept[omitted:]
	}

	lines := make([]string, 0, len(kept))
	total := 0
	// Build newest-first against the total budget, then restore order, so
	// the messages closest to the current one survive the cap.
	for i := len(kept) - 1; i >= 0; i-- {
		line := formatThreadCatchupLine(kept[i])
		if total+len(line) > threadCatchupMaxTotalChars && len(lines) > 0 {
			omitted += i + 1
			break
		}
		total += len(line)
		lines = append(lines, line)
	}
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}

	var b strings.Builder
	b.WriteString("## New messages in this thread since your last reply\n\n")
	if omitted > 0 {
		fmt.Fprintf(&b, "(%d earlier message(s) omitted)\n\n", omitted)
	}
	b.WriteString(strings.Join(lines, "\n"))
	b.WriteString("\n---\n\n")
	log.Printf("[BOT_MANAGER] Added %d new thread message(s) to follow-up (thread=%s)", len(lines), key)
	return b.String()
}

func formatThreadCatchupLine(h ThreadMessage) string {
	role := "User"
	switch {
	case h.IsBot && h.UserName != "":
		role = h.UserName + " (app)"
	case h.IsBot:
		role = "App"
	case h.UserName != "":
		role = h.UserName
	}
	text := strings.TrimSpace(h.Text)
	if r := []rune(text); len(r) > threadCatchupMaxMessageChars {
		text = strings.TrimSpace(string(r[:threadCatchupMaxMessageChars])) + " …[truncated]"
	}
	tsLabel := h.Timestamp.UTC().Format("2006-01-02 15:04 UTC")
	return fmt.Sprintf("**%s** (%s): %s\n", role, tsLabel, text)
}

// withThreadCatchup prepends a catch-up block to a follow-up turn's text.
func withThreadCatchup(catchup, text string) string {
	if catchup == "" {
		return text
	}
	if !strings.HasPrefix(strings.TrimSpace(text), "## ") {
		text = "## Current Message\n" + text
	}
	return catchup + text
}
