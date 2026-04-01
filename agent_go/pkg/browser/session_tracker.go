package browser

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// MaxBrowserSessionsPerChat is the max concurrent headless browser sessions per chat/workflow session.
// When exceeded, the tool returns an error telling the LLM to close a session first.
const MaxBrowserSessionsPerChat = 1

// MaxBrowserSessionsGlobal is the absolute max across all sessions.
// When exceeded, the oldest session globally is auto-evicted.
const MaxBrowserSessionsGlobal = 8

// browserSessionInfo tracks a headless browser session
type browserSessionInfo struct {
	browserSession string // agent-browser session name (e.g., "twitter_research")
	chatSessionID  string // chat/workflow session ID that owns this browser
	lastUsed       time.Time
	createdAt      time.Time
}

// SessionTracker tracks active headless browser sessions to prevent unbounded growth.
// Thread-safe — shared across all Executor instances.
type SessionTracker struct {
	mu       sync.Mutex
	sessions map[string]*browserSessionInfo // browser session name → info
}

var globalTracker = &SessionTracker{
	sessions: make(map[string]*browserSessionInfo),
}

// GetSessionTracker returns the global session tracker
func GetSessionTracker() *SessionTracker {
	return globalTracker
}

// Touch marks a browser session as recently used (or registers it if new).
// chatSessionID is the owning chat/workflow session ID.
// Returns true if this is a new browser session.
func (t *SessionTracker) Touch(browserSession, chatSessionID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	existing, exists := t.sessions[browserSession]
	if exists {
		existing.lastUsed = time.Now()
		// Update chat session ID if it changed (e.g., session reused across workflows)
		if chatSessionID != "" {
			existing.chatSessionID = chatSessionID
		}
		return false
	}
	t.sessions[browserSession] = &browserSessionInfo{
		browserSession: browserSession,
		chatSessionID:  chatSessionID,
		lastUsed:       time.Now(),
		createdAt:      time.Now(),
	}
	log.Printf("[BROWSER_TRACKER] New session registered: browser=%q chat=%q (total: %d)", browserSession, chatSessionID, len(t.sessions))
	return true
}

// Remove removes a browser session from tracking
func (t *SessionTracker) Remove(browserSession string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if info, exists := t.sessions[browserSession]; exists {
		log.Printf("[BROWSER_TRACKER] Session removed: browser=%q chat=%q age=%s (remaining: %d)",
			browserSession, info.chatSessionID, time.Since(info.createdAt).Round(time.Second), len(t.sessions)-1)
	}
	delete(t.sessions, browserSession)
}

// CountForChat returns the number of browser sessions owned by a chat/workflow session
func (t *SessionTracker) CountForChat(chatSessionID string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	count := 0
	for _, s := range t.sessions {
		if s.chatSessionID == chatSessionID {
			count++
		}
	}
	return count
}

// SessionsForChat returns browser session names owned by a chat/workflow session
func (t *SessionTracker) SessionsForChat(chatSessionID string) []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var names []string
	for _, s := range t.sessions {
		if s.chatSessionID == chatSessionID {
			names = append(names, s.browserSession)
		}
	}
	return names
}

// Count returns the total number of tracked sessions
func (t *SessionTracker) Count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.sessions)
}

// GetOldestSession returns the name of the least-recently-used session globally.
func (t *SessionTracker) GetOldestSession() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var oldest *browserSessionInfo
	for _, s := range t.sessions {
		if oldest == nil || s.lastUsed.Before(oldest.lastUsed) {
			oldest = s
		}
	}
	if oldest == nil {
		return ""
	}
	return oldest.browserSession
}

// GetOldestSessionForChat returns the least-recently-used session for a specific chat session.
func (t *SessionTracker) GetOldestSessionForChat(chatSessionID string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var oldest *browserSessionInfo
	for _, s := range t.sessions {
		if s.chatSessionID == chatSessionID {
			if oldest == nil || s.lastUsed.Before(oldest.lastUsed) {
				oldest = s
			}
		}
	}
	if oldest == nil {
		return ""
	}
	return oldest.browserSession
}

// CheckLimits checks if adding a new browser session for this chat would exceed limits.
// Returns an error message if limits are exceeded, empty string if OK.
func (t *SessionTracker) CheckLimits(browserSession, chatSessionID string) string {
	t.mu.Lock()
	defer t.mu.Unlock()

	// If this browser session already exists, it's a reuse — always OK
	if _, exists := t.sessions[browserSession]; exists {
		return ""
	}

	// Check per-chat limit
	if chatSessionID != "" {
		chatCount := 0
		var chatSessions []string
		for _, s := range t.sessions {
			if s.chatSessionID == chatSessionID {
				chatCount++
				chatSessions = append(chatSessions, s.browserSession)
			}
		}
		if chatCount >= MaxBrowserSessionsPerChat {
			return fmt.Sprintf(
				"ERROR: Cannot open browser session %q — you already have %d active browser sessions (max %d per workflow). "+
					"Active sessions: %v. Close one first using agent_browser(command=\"close\", session=\"<name>\") before opening a new one.",
				browserSession, chatCount, MaxBrowserSessionsPerChat, chatSessions)
		}
	}

	return ""
}

// ActiveSessions returns a snapshot of all active sessions with details
func (t *SessionTracker) ActiveSessions() []map[string]string {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := make([]map[string]string, 0, len(t.sessions))
	for _, s := range t.sessions {
		result = append(result, map[string]string{
			"browser_session": s.browserSession,
			"chat_session":    s.chatSessionID,
			"age":             time.Since(s.createdAt).Round(time.Second).String(),
			"idle":            time.Since(s.lastUsed).Round(time.Second).String(),
		})
	}
	return result
}

// RemoveAllForChat removes all browser sessions for a given chat/workflow session
func (t *SessionTracker) RemoveAllForChat(chatSessionID string) []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var removed []string
	for name, s := range t.sessions {
		if s.chatSessionID == chatSessionID {
			removed = append(removed, name)
			delete(t.sessions, name)
		}
	}
	if len(removed) > 0 {
		log.Printf("[BROWSER_TRACKER] Removed %d sessions for chat %q: %v (remaining: %d)",
			len(removed), chatSessionID, removed, len(t.sessions))
	}
	return removed
}

// CloseAllForChat closes all browser processes for a given chat/workflow session
// and removes them from the tracker. This should be called when a session ends
// (workflow completion, stop, or clear) to prevent chromium process accumulation.
func (t *SessionTracker) CloseAllForChat(chatSessionID string, client *Client) {
	sessions := t.SessionsForChat(chatSessionID)
	if len(sessions) == 0 {
		return
	}

	log.Printf("[BROWSER_CLEANUP] Closing %d browser session(s) for chat %q: %v", len(sessions), chatSessionID, sessions)
	for _, session := range sessions {
		if client != nil {
			closeArgs := []string{
				"--user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
				"--args", "--disable-blink-features=AutomationControlled",
				"--session", session, "close", "--json",
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_, err := client.ExecuteCommand(ctx, closeArgs, &ExecuteOptions{Timeout: 10 * time.Second})
			cancel()
			if err != nil {
				log.Printf("[BROWSER_CLEANUP] Failed to close session %q: %v (will remove from tracker anyway)", session, err)
			} else {
				log.Printf("[BROWSER_CLEANUP] Closed browser session %q", session)
			}
		}
	}

	// Remove all from tracker
	removed := t.RemoveAllForChat(chatSessionID)
	log.Printf("[BROWSER_CLEANUP] Cleanup complete for chat %q: removed %d session(s) from tracker", chatSessionID, len(removed))
}

// Clear removes all tracked sessions
func (t *SessionTracker) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()
	count := len(t.sessions)
	t.sessions = make(map[string]*browserSessionInfo)
	log.Printf("[BROWSER_TRACKER] All %d sessions cleared", count)
}
