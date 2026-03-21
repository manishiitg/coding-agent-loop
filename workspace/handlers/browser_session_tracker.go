package handlers

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"
)

// MaxBrowserSessionsPerChat is the max concurrent headless browser sessions per chat/workflow session.
const MaxBrowserSessionsPerChat = 2

// MaxBrowserSessionsGlobal is the absolute max across all sessions.
const MaxBrowserSessionsGlobal = 8

type browserSessionEntry struct {
	browserSession string
	chatSessionID  string
	lastUsed       time.Time
	createdAt      time.Time
}

// BrowserSessionTracker tracks active agent-browser sessions at the workspace-api level.
// This catches browser sessions started via shell exec (code execution mode).
var browserSessionTracker = &struct {
	mu       sync.Mutex
	sessions map[string]*browserSessionEntry
}{
	sessions: make(map[string]*browserSessionEntry),
}

// sessionRegex extracts --session <name> from agent-browser commands
var sessionRegex = regexp.MustCompile(`--session\s+(\S+)`)

// commandRegex extracts the agent-browser subcommand (open, close, snapshot, etc.)
var commandRegex = regexp.MustCompile(`agent-browser\s+.*?(?:--session\s+\S+\s+)?(\w+)`)

// CheckBrowserSessionLimit checks if an agent-browser shell command would exceed session limits.
// Returns an error string if the limit is exceeded, empty string if OK.
// Also tracks the session for future limit checks.
func CheckBrowserSessionLimit(command string, extraEnv map[string]string) string {
	// Only intercept agent-browser commands
	if !strings.Contains(command, "agent-browser") {
		return ""
	}

	// Extract session name
	sessionMatch := sessionRegex.FindStringSubmatch(command)
	if len(sessionMatch) < 2 {
		return "" // No session flag — can't track
	}
	browserSession := sessionMatch[1]

	// Detect the subcommand
	subCommand := detectSubCommand(command, browserSession)

	// Get chat session ID from env
	chatSessionID := ""
	if extraEnv != nil {
		chatSessionID = extraEnv["MCP_SESSION_ID"]
	}

	bt := browserSessionTracker
	bt.mu.Lock()
	defer bt.mu.Unlock()

	isOpenCommand := subCommand == "open" || subCommand == "goto" || subCommand == "navigate"
	isCloseCommand := subCommand == "close" || subCommand == "quit" || subCommand == "exit"

	log.Printf("[BROWSER_SHELL_TRACKER] cmd=%q browser=%q chat=%q sub=%q open=%v close=%v (active: %d global)",
		truncate(command, 100), browserSession, truncateID(chatSessionID), subCommand, isOpenCommand, isCloseCommand, len(bt.sessions))

	if isCloseCommand {
		if _, exists := bt.sessions[browserSession]; exists {
			delete(bt.sessions, browserSession)
			log.Printf("[BROWSER_SHELL_TRACKER] Removed session: browser=%q (remaining: %d)", browserSession, len(bt.sessions))
		}
		return ""
	}

	if isOpenCommand {
		// Check if this session already exists (reuse is always OK)
		if _, exists := bt.sessions[browserSession]; !exists {
			// Check per-chat limit
			if chatSessionID != "" {
				chatCount := 0
				var chatSessions []string
				for _, s := range bt.sessions {
					if s.chatSessionID == chatSessionID {
						chatCount++
						chatSessions = append(chatSessions, s.browserSession)
					}
				}
				if chatCount >= MaxBrowserSessionsPerChat {
					msg := fmt.Sprintf(
						"ERROR: Cannot open browser session %q — already %d active sessions (max %d per workflow). "+
							"Active: %v. Close one first: agent-browser --session <name> close",
						browserSession, chatCount, MaxBrowserSessionsPerChat, chatSessions)
					log.Printf("[BROWSER_SHELL_TRACKER] LIMIT: %s", msg)
					return msg
				}
			}

			// Check global limit
			if len(bt.sessions) >= MaxBrowserSessionsGlobal {
				// Auto-evict oldest
				var oldest *browserSessionEntry
				var oldestName string
				for name, s := range bt.sessions {
					if oldest == nil || s.lastUsed.Before(oldest.lastUsed) {
						oldest = s
						oldestName = name
					}
				}
				if oldestName != "" {
					log.Printf("[BROWSER_SHELL_TRACKER] Global limit (%d), evicting oldest: %q (age=%s)",
						MaxBrowserSessionsGlobal, oldestName, time.Since(oldest.createdAt).Round(time.Second))
					delete(bt.sessions, oldestName)
				}
			}
		}
	}

	// Track/update the session
	if existing, exists := bt.sessions[browserSession]; exists {
		existing.lastUsed = time.Now()
		if chatSessionID != "" {
			existing.chatSessionID = chatSessionID
		}
	} else {
		bt.sessions[browserSession] = &browserSessionEntry{
			browserSession: browserSession,
			chatSessionID:  chatSessionID,
			lastUsed:       time.Now(),
			createdAt:      time.Now(),
		}
		log.Printf("[BROWSER_SHELL_TRACKER] New session: browser=%q chat=%q (total: %d)",
			browserSession, truncateID(chatSessionID), len(bt.sessions))
	}

	return ""
}

// detectSubCommand extracts the agent-browser subcommand from a shell command string.
// agent-browser commands look like: agent-browser [flags] --session <name> <subcommand> [args]
func detectSubCommand(command, browserSession string) string {
	// Find text after the session name
	idx := strings.Index(command, browserSession)
	if idx < 0 {
		return "unknown"
	}
	after := strings.TrimSpace(command[idx+len(browserSession):])
	// First word after session name is the subcommand
	parts := strings.Fields(after)
	if len(parts) == 0 {
		return "unknown"
	}
	// Skip flags that might appear between session and command
	for _, p := range parts {
		if !strings.HasPrefix(p, "-") {
			return p
		}
	}
	return "unknown"
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func truncateID(s string) string {
	if len(s) <= 8 {
		return s
	}
	return s[:8]
}
