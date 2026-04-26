package services

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// SimulatorMessage represents a message in the simulator thread
type SimulatorMessage struct {
	ID        string          `json:"id"`
	Text      string          `json:"text"`
	Blocks    []MessageBlock  `json:"blocks,omitempty"`
	IsBot     bool            `json:"is_bot"`
	Timestamp time.Time       `json:"timestamp"`
}

// simulatorThread holds an in-memory thread's messages
type simulatorThread struct {
	mu        sync.RWMutex
	messages  []SimulatorMessage
	createdAt time.Time
}

// AddUserMessage appends a user message to the thread
func (t *simulatorThread) AddUserMessage(text string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.messages = append(t.messages, SimulatorMessage{
		ID:        uuid.New().String(),
		Text:      text,
		IsBot:     false,
		Timestamp: time.Now(),
	})
}

// ThreadInfo describes a thread for the API listing
type ThreadInfo struct {
	ThreadID     string    `json:"thread_id"`
	Preview      string    `json:"preview"`
	CreatedAt    time.Time `json:"created_at"`
	MessageCount int       `json:"message_count"`
}

// WebSimulatorConnector implements BotConnector with in-memory message storage
type WebSimulatorConnector struct {
	mu                 sync.RWMutex
	threads            map[string]*simulatorThread // threadTS -> thread
	threaded           bool                        // dynamic thread mode flag
	messageHandler     BotMessageHandler
	interactionHandler BotInteractionHandler
	stopCleanup        chan struct{}
}

// NewWebSimulatorConnector creates a new simulator connector
func NewWebSimulatorConnector() *WebSimulatorConnector {
	wsc := &WebSimulatorConnector{
		threads:     make(map[string]*simulatorThread),
		stopCleanup: make(chan struct{}),
	}
	go wsc.cleanupLoop()
	return wsc
}

// --- BotConnector interface implementation ---

func (w *WebSimulatorConnector) Name() string {
	return "web_simulator"
}

func (w *WebSimulatorConnector) IsEnabled() bool {
	return true
}

func (w *WebSimulatorConnector) SupportsThreads() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.threaded
}

// SetThreaded changes the thread mode at runtime
func (w *WebSimulatorConnector) SetThreaded(on bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.threaded = on
}

// IsThreaded returns the current thread mode
func (w *WebSimulatorConnector) IsThreaded() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.threaded
}

func (w *WebSimulatorConnector) SendNotification(ctx context.Context, uniqueID string, message string, contextMsg string, buttonOptions *ButtonOptions, dest *NotificationDestination) (string, error) {
	// Not a notification connector — no-op
	return "", nil
}

func (w *WebSimulatorConnector) StartListening(ctx context.Context) error {
	return nil
}

func (w *WebSimulatorConnector) StopListening() {
	close(w.stopCleanup)
}

// AddReaction is a no-op for the web simulator — it has no emoji reactions.
func (w *WebSimulatorConnector) AddReaction(ctx context.Context, channelID, messageTS, emoji string) error {
	return nil
}

// RemoveReaction is a no-op for the web simulator — it has no emoji reactions.
func (w *WebSimulatorConnector) RemoveReaction(ctx context.Context, channelID, messageTS, emoji string) error {
	return nil
}

func (w *WebSimulatorConnector) SendThreadMessage(ctx context.Context, threadID ThreadID, message string) (string, error) {
	thread := w.getOrCreateThread(threadID.ThreadTS)
	msgID := uuid.New().String()

	thread.mu.Lock()
	thread.messages = append(thread.messages, SimulatorMessage{
		ID:        msgID,
		Text:      message,
		IsBot:     true,
		Timestamp: time.Now(),
	})
	thread.mu.Unlock()

	log.Printf("[WEB_SIMULATOR] Bot message in thread %s: %s", threadID.ThreadTS, botTruncate(message, 80))
	return msgID, nil
}

func (w *WebSimulatorConnector) SendThreadMessageWithBlocks(ctx context.Context, threadID ThreadID, message string, blocks []MessageBlock) (string, error) {
	thread := w.getOrCreateThread(threadID.ThreadTS)
	msgID := uuid.New().String()

	thread.mu.Lock()
	thread.messages = append(thread.messages, SimulatorMessage{
		ID:        msgID,
		Text:      message,
		Blocks:    blocks,
		IsBot:     true,
		Timestamp: time.Now(),
	})
	thread.mu.Unlock()

	log.Printf("[WEB_SIMULATOR] Bot message+blocks in thread %s: %s (%d blocks)", threadID.ThreadTS, botTruncate(message, 80), len(blocks))
	return msgID, nil
}

func (w *WebSimulatorConnector) UpdateMessage(ctx context.Context, threadID ThreadID, messageID string, newText string) error {
	thread := w.getThread(threadID.ThreadTS)
	if thread == nil {
		return fmt.Errorf("thread %s not found", threadID.ThreadTS)
	}

	thread.mu.Lock()
	defer thread.mu.Unlock()

	for i, msg := range thread.messages {
		if msg.ID == messageID {
			thread.messages[i].Text = newText
			return nil
		}
	}

	return fmt.Errorf("message %s not found in thread %s", messageID, threadID.ThreadTS)
}

// GetChannelName returns "" — web simulator has no concept of Slack-style channel names.
func (w *WebSimulatorConnector) GetChannelName(ctx context.Context, channelID string) string {
	return ""
}

func (w *WebSimulatorConnector) GetThreadHistory(ctx context.Context, threadID ThreadID) ([]ThreadMessage, error) {
	thread := w.getThread(threadID.ThreadTS)
	if thread == nil {
		return nil, nil
	}

	thread.mu.RLock()
	defer thread.mu.RUnlock()

	var result []ThreadMessage
	for _, msg := range thread.messages {
		userName := "Bot"
		userID := "bot"
		if !msg.IsBot {
			userName = "User"
			userID = "simulator_user"
		}
		result = append(result, ThreadMessage{
			UserID:    userID,
			UserName:  userName,
			Text:      msg.Text,
			Timestamp: msg.Timestamp,
			IsBot:     msg.IsBot,
		})
	}
	return result, nil
}

func (w *WebSimulatorConnector) SetMessageHandler(handler BotMessageHandler) {
	w.messageHandler = handler
}

func (w *WebSimulatorConnector) SetInteractionHandler(handler BotInteractionHandler) {
	w.interactionHandler = handler
}

func (w *WebSimulatorConnector) GetFormatter() MessageFormatter {
	return &WebFormatter{}
}

// --- Public API methods (called by REST routes) ---

// HandleSimulatedMessage creates a new thread (threaded) or uses fixed ID (non-threaded) and dispatches the message
func (w *WebSimulatorConnector) HandleSimulatedMessage(text string) string {
	var threadTS string
	if w.IsThreaded() {
		threadTS = fmt.Sprintf("sim_%d", time.Now().UnixNano())
	} else {
		threadTS = "simulator"
	}
	thread := w.getOrCreateThread(threadTS)

	// Add user message to thread
	thread.mu.Lock()
	thread.messages = append(thread.messages, SimulatorMessage{
		ID:        uuid.New().String(),
		Text:      text,
		IsBot:     false,
		Timestamp: time.Now(),
	})
	thread.mu.Unlock()

	log.Printf("[WEB_SIMULATOR] User message: %s → thread %s", botTruncate(text, 80), threadTS)

	// Dispatch to bot manager via message handler
	if w.messageHandler != nil {
		w.messageHandler(BotIncomingMessage{
			Platform:  "web_simulator",
			UserID:    "simulator_user",
			UserName:  "Simulator User",
			ChannelID: "simulator",
			ThreadTS:  threadTS,
			Text:      text,
			Timestamp: time.Now(),
		})
	}

	return threadTS
}

// HandleSimulatedInteraction dispatches a button click to the bot manager
func (w *WebSimulatorConnector) HandleSimulatedInteraction(threadTS, actionID, value string) {
	log.Printf("[WEB_SIMULATOR] Interaction in thread %s: action=%s value=%s", threadTS, actionID, value)

	if w.interactionHandler != nil {
		w.interactionHandler("web_simulator", "simulator", threadTS, actionID, value, "simulator_user")
	}
}

// GetThreadMessages returns messages for a thread, optionally after a given index
func (w *WebSimulatorConnector) GetThreadMessages(threadTS string, sinceIndex int) []SimulatorMessage {
	thread := w.getThread(threadTS)
	if thread == nil {
		return nil
	}

	thread.mu.RLock()
	defer thread.mu.RUnlock()

	if sinceIndex >= len(thread.messages) {
		return nil
	}

	result := make([]SimulatorMessage, len(thread.messages)-sinceIndex)
	copy(result, thread.messages[sinceIndex:])
	return result
}

// GetThreadMessageCount returns the total number of messages in a thread
func (w *WebSimulatorConnector) GetThreadMessageCount(threadTS string) int {
	thread := w.getThread(threadTS)
	if thread == nil {
		return 0
	}

	thread.mu.RLock()
	defer thread.mu.RUnlock()
	return len(thread.messages)
}

// CleanupThread removes a thread from memory
func (w *WebSimulatorConnector) CleanupThread(threadTS string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.threads, threadTS)
}

// ListThreads returns info about all threads, sorted newest-first
func (w *WebSimulatorConnector) ListThreads() []ThreadInfo {
	w.mu.RLock()
	defer w.mu.RUnlock()

	infos := make([]ThreadInfo, 0, len(w.threads))
	for ts, t := range w.threads {
		t.mu.RLock()
		preview := ""
		for _, msg := range t.messages {
			if !msg.IsBot {
				preview = msg.Text
				break
			}
		}
		if len(preview) > 80 {
			preview = preview[:80] + "..."
		}
		infos = append(infos, ThreadInfo{
			ThreadID:     ts,
			Preview:      preview,
			CreatedAt:    t.createdAt,
			MessageCount: len(t.messages),
		})
		t.mu.RUnlock()
	}

	// Sort newest-first
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].CreatedAt.After(infos[j].CreatedAt)
	})

	return infos
}

// --- Public helpers ---

// AddUserMessage stores a user message in a thread
func (w *WebSimulatorConnector) AddUserMessage(threadTS string, text string) {
	thread := w.getOrCreateThread(threadTS)
	thread.AddUserMessage(text)
}

// --- Internal helpers ---

func (w *WebSimulatorConnector) getOrCreateThread(threadTS string) *simulatorThread {
	w.mu.Lock()
	defer w.mu.Unlock()

	if t, ok := w.threads[threadTS]; ok {
		return t
	}

	t := &simulatorThread{
		createdAt: time.Now(),
	}
	w.threads[threadTS] = t
	return t
}

func (w *WebSimulatorConnector) getThread(threadTS string) *simulatorThread {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.threads[threadTS]
}

// cleanupLoop sweeps threads older than 1 hour every 5 minutes
func (w *WebSimulatorConnector) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.mu.Lock()
			cutoff := time.Now().Add(-1 * time.Hour)
			for ts, thread := range w.threads {
				if thread.createdAt.Before(cutoff) {
					delete(w.threads, ts)
					log.Printf("[WEB_SIMULATOR] Cleaned up expired thread %s", ts)
				}
			}
			w.mu.Unlock()
		case <-w.stopCleanup:
			return
		}
	}
}

// --- WebFormatter: pass-through markdown formatter ---

type WebFormatter struct{}

func (f *WebFormatter) FormatMessage(markdown string) string {
	return markdown
}

func (f *WebFormatter) MaxMessageLength() int {
	return 65536
}

func (f *WebFormatter) SplitLongMessage(text string) []string {
	max := f.MaxMessageLength()
	if len(text) <= max {
		return []string{text}
	}

	var parts []string
	for len(text) > 0 {
		if len(text) <= max {
			parts = append(parts, text)
			break
		}
		// Split at last newline before max
		splitAt := max
		if idx := strings.LastIndex(text[:max], "\n"); idx > 0 {
			splitAt = idx + 1
		}
		parts = append(parts, text[:splitAt])
		text = text[splitAt:]
	}
	return parts
}
