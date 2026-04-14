package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/manishiitg/mcpagent/events"
	"github.com/manishiitg/mcpagent/mcpclient"

	"mcp-agent-builder-go/agent_go/pkg/chathistory"
	"mcp-agent-builder-go/agent_go/pkg/skills"
)

// ThreadID identifies a conversation thread on a platform
type ThreadID struct {
	Platform  string // "slack", "discord", "telegram", "whatsapp"
	ChannelID string
	ThreadTS  string // platform-specific thread root ID
}

// Key returns a unique string key for this thread
func (t ThreadID) Key() string {
	return fmt.Sprintf("%s:%s:%s", t.Platform, t.ChannelID, t.ThreadTS)
}

// BotIncomingMessage represents a message received from a platform
type BotIncomingMessage struct {
	Platform        string
	UserID          string
	UserName        string
	UserEmail       string // resolved by platform connector (e.g. Slack users.info)
	WorkspaceUserID string // pre-resolved workspace user ID (set by simulator from HTTP auth)
	ChannelID       string
	ThreadTS        string // empty = new conversation, set = existing thread
	Text            string // @mention stripped
	Timestamp       time.Time
	IsThreadReply   bool
	IsMention       bool            // true when the bot was @mentioned (vs plain thread reply)
	ThreadHistory   []ThreadMessage // populated when tagged in existing thread
}

func botMetaFromMsg(msg BotIncomingMessage, threadID ThreadID) *chathistory.BotMetadata {
	return &chathistory.BotMetadata{
		Platform:  msg.Platform,
		ChannelID: msg.ChannelID,
		ThreadTS:  threadID.ThreadTS,
		UserID:    msg.UserID,
		UserName:  msg.UserName,
		UserEmail: msg.UserEmail,
	}
}

// ThreadMessage represents a single message in a thread's history
type ThreadMessage struct {
	UserID    string    `json:"user_id"`
	UserName  string    `json:"user_name"`
	Text      string    `json:"text"`
	Timestamp time.Time `json:"timestamp"`
	IsBot     bool      `json:"is_bot"`
}

// MessageBlock represents a rich message block (buttons, sections, etc.)
type MessageBlock struct {
	Type    string          `json:"type"` // "section", "actions", "divider"
	Text    string          `json:"text,omitempty"`
	Buttons []MessageButton `json:"buttons,omitempty"`
}

// MessageButton represents an interactive button in a message
type MessageButton struct {
	Text     string `json:"text"`
	Value    string `json:"value"`
	Style    string `json:"style,omitempty"` // "primary", "danger"
	ActionID string `json:"action_id"`
}

// BotMessageHandler is the callback invoked when a bot receives a message
type BotMessageHandler func(msg BotIncomingMessage)

// BotInteractionHandler is the callback invoked when a user clicks a button
type BotInteractionHandler func(platform, channelID, threadTS, actionID, value, userID string)

// MessageFormatter converts standard Markdown to platform-specific format
type MessageFormatter interface {
	FormatMessage(markdown string) string
	MaxMessageLength() int
	SplitLongMessage(text string) []string
}

// BotConnector is the per-platform bidirectional bot interface
type BotConnector interface {
	NotificationConnector // embeds: Name(), IsEnabled(), SendNotification()

	SupportsThreads() bool // true for Slack, false for WhatsApp/Telegram/web_simulator
	StartListening(ctx context.Context) error
	StopListening()
	SendThreadMessage(ctx context.Context, threadID ThreadID, message string) (string, error)
	SendThreadMessageWithBlocks(ctx context.Context, threadID ThreadID, message string, blocks []MessageBlock) (string, error)
	UpdateMessage(ctx context.Context, threadID ThreadID, messageID string, newText string) error
	GetThreadHistory(ctx context.Context, threadID ThreadID) ([]ThreadMessage, error)
	SetMessageHandler(handler BotMessageHandler)
	SetInteractionHandler(handler BotInteractionHandler)
	GetFormatter() MessageFormatter
}

// BotEventSubscriber abstracts event subscription so services doesn't import internal/events.
// The server layer provides a concrete implementation wrapping EventStore.
type BotEventSubscriber interface {
	SubscribeBot(sessionID string) (<-chan BotEventData, func())
}

// BotEventData is a minimal event representation for bot filtering
type BotEventData struct {
	Type      string
	Timestamp time.Time
	Data      *events.AgentEvent
}

// SessionStartFunc is the function signature for starting an internal chat session.
// It is set by the server layer to avoid import cycles.
type SessionStartFunc func(ctx context.Context, req map[string]interface{}, sessionID string, userID string, eventCallback func(event *events.AgentEvent)) error

// SessionFollowUpFunc is the function signature for injecting a follow-up message into a running session.
// It calls handleQuery with the same session ID but does NOT block on completion.
// The reqMap contains the full session config (servers, skills, delegation mode, API keys, etc.)
// so the follow-up agent is configured identically to the initial session.
type SessionFollowUpFunc func(ctx context.Context, reqMap map[string]interface{}, sessionID string, userID string) error

// DecryptedSecret represents a decrypted user secret ready for injection into agent prompts
type DecryptedSecret struct {
	Name  string
	Value string
}

// UserSecretsLoaderFunc loads decrypted user secrets for a given user ID.
// Used by bot sessions to retrieve server-side stored secrets.
type UserSecretsLoaderFunc func(ctx context.Context, userID string) ([]DecryptedSecret, error)

// BotConversationManager is the platform-agnostic orchestrator for bot sessions.
// Bot conversations are unified with regular chat sessions: each Slack thread
// / DM / web-simulator tab maps to a chat session folder with a BotMetadata
// block attached to its manifest.
type BotConversationManager struct {
	mu         sync.RWMutex
	connectors map[string]BotConnector      // platform name -> connector
	sessions   map[string]*activeBotSession // threadKey -> active session tracking

	chatStore       chathistory.Store
	eventSubscriber BotEventSubscriber

	// Function references set by server layer (to avoid import cycles)
	startSession    SessionStartFunc
	followUpSession SessionFollowUpFunc
	loadUserSecrets UserSecretsLoaderFunc
	mcpConfigPath   string
	workspaceURL    string
}

// activeBotSession tracks an in-progress bot conversation. In the unified
// model there is exactly one ID — the chat session ID (folder name).
type activeBotSession struct {
	mu                sync.Mutex
	SessionID         string // unified chat session id
	UserID            string // workspace user ID for secrets loading
	Status            string
	Platform          string
	ThreadID          ThreadID
	Metadata          *chathistory.BotMetadata // platform/user info for the conversation
	cancel            context.CancelFunc
	eventFilter       *BotEventFilter
	awaitingUserInput bool   // set by event filter on any blocking event
	blockingEventType string // "blocking_human_feedback", "blocking_human_questions"
}

// NewBotConversationManager creates a new manager.
func NewBotConversationManager(chatStore chathistory.Store, mcpConfigPath, workspaceURL string) *BotConversationManager {
	return &BotConversationManager{
		connectors:    make(map[string]BotConnector),
		sessions:      make(map[string]*activeBotSession),
		chatStore:     chatStore,
		mcpConfigPath: mcpConfigPath,
		workspaceURL:  workspaceURL,
	}
}

// SetEventSubscriber sets the event subscriber (injected by server layer)
func (m *BotConversationManager) SetEventSubscriber(sub BotEventSubscriber) {
	m.eventSubscriber = sub
}

// SetStartSessionFunc sets the function used to start internal chat sessions
func (m *BotConversationManager) SetStartSessionFunc(fn SessionStartFunc) {
	m.startSession = fn
}

// SetFollowUpFunc sets the function used to inject follow-up messages into running sessions
func (m *BotConversationManager) SetFollowUpFunc(fn SessionFollowUpFunc) {
	m.followUpSession = fn
}

// SetUserSecretsLoader sets the function used to load decrypted user secrets for bot sessions
func (m *BotConversationManager) SetUserSecretsLoader(fn UserSecretsLoaderFunc) {
	m.loadUserSecrets = fn
}

// RegisterConnector registers a bot connector
func (m *BotConversationManager) RegisterConnector(connector BotConnector) {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := connector.Name()
	m.connectors[name] = connector

	// Set up message handler
	connector.SetMessageHandler(m.HandleIncomingMessage)
	connector.SetInteractionHandler(m.HandleInteraction)

	log.Printf("[BOT_MANAGER] Registered connector: %s", name)
}

// GetConnector returns a connector by platform name
func (m *BotConversationManager) GetConnector(platform string) BotConnector {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connectors[platform]
}

// loadMCPServerNames reads server names from the MCP config.
func (m *BotConversationManager) loadMCPServerNames() []string {
	cfg, err := mcpclient.LoadMergedConfig(m.mcpConfigPath, nil)
	if err != nil || cfg == nil {
		return nil
	}
	return cfg.ListServers()
}

// LoadAvailableCapabilities returns all available MCP servers and skills.
// Used by the config API endpoint.
func (m *BotConversationManager) LoadAvailableCapabilities() (servers []string, discoveredSkills []skills.Skill) {
	servers = m.loadMCPServerNames()
	if m.workspaceURL != "" {
		discoveredSkills, _ = skills.DiscoverSkills(m.workspaceURL)
	}
	return
}

// resolveWorkspaceUserID maps a bot message to a workspace user ID for
// per-user secrets loading. The web simulator pre-resolves this from HTTP auth;
// Slack and other async platforms fall through to "default" since we no
// longer maintain an email→userID lookup table.
func (m *BotConversationManager) resolveWorkspaceUserID(msg BotIncomingMessage) string {
	if msg.WorkspaceUserID != "" {
		return msg.WorkspaceUserID
	}
	return "default"
}

// HandleIncomingMessage processes a message from any platform (async path)
func (m *BotConversationManager) HandleIncomingMessage(msg BotIncomingMessage) {
	// Non-mention messages should only be processed if there's an active session in the thread.
	// Skip access checks and don't reply with "no access" for messages that didn't tag the bot.
	if !msg.IsMention {
		// Quick check: is there even an active session for this thread?
		threadKey := msg.ChannelID + ":" + msg.ThreadTS
		if msg.ThreadTS == "" {
			threadKey = msg.ChannelID + ":" + msg.ChannelID
		}
		m.mu.Lock()
		_, hasSession := m.sessions[threadKey]
		m.mu.Unlock()
		if !hasSession {
			// No active session and not a mention — silently ignore
			return
		}
	}

	// Check allowed_emails filter — merge DB config with BOT_ALLOWED_EMAILS env var
	// Only send rejection message for @mentions (non-mentions are silently ignored above)
	if msg.UserEmail != "" {
		var allowedEmails []string

		// 1. Load from filesystem-backed bot connector config
		globalCfg, _ := m.chatStore.GetBotConnectorConfig(context.Background(), "_global")
		if globalCfg != nil && globalCfg.ConfigJSON != "" {
			var cfgData map[string]json.RawMessage
			if err := json.Unmarshal([]byte(globalCfg.ConfigJSON), &cfgData); err == nil {
				if raw, ok := cfgData["allowed_emails"]; ok {
					json.Unmarshal(raw, &allowedEmails)
				}
			}
		}

		// 2. Merge with BOT_ALLOWED_EMAILS env var (comma-separated)
		if envEmails := os.Getenv("BOT_ALLOWED_EMAILS"); envEmails != "" {
			for _, e := range strings.Split(envEmails, ",") {
				e = strings.TrimSpace(e)
				if e != "" {
					allowedEmails = append(allowedEmails, e)
				}
			}
		}

		// 3. If any allowed emails are configured, enforce the filter
		if len(allowedEmails) > 0 {
			allowed := false
			for _, email := range allowedEmails {
				if strings.EqualFold(email, msg.UserEmail) {
					allowed = true
					break
				}
			}
			if !allowed {
				log.Printf("[BOT_MANAGER] Rejected message from %s (%s) — not in allowed_emails", msg.UserID, msg.UserEmail)
				// Only reply with rejection for direct @mentions
				if msg.IsMention {
					if connector := m.GetConnector(msg.Platform); connector != nil {
						threadID := ThreadID{Platform: msg.Platform, ChannelID: msg.ChannelID, ThreadTS: msg.ThreadTS}
						if threadID.ThreadTS == "" {
							threadID.ThreadTS = fmt.Sprintf("%d", time.Now().UnixNano())
						}
						connector.SendThreadMessage(context.Background(), threadID, "Sorry, you don't have access to use this bot. Please contact your administrator to get access.")
					}
				}
				return
			}
		}
	}

	connector := m.GetConnector(msg.Platform)

	threadID := ThreadID{
		Platform:  msg.Platform,
		ChannelID: msg.ChannelID,
		ThreadTS:  msg.ThreadTS,
	}

	// Determine thread key based on whether platform supports threads
	supportsThreads := connector != nil && connector.SupportsThreads()
	if !supportsThreads {
		threadID.ThreadTS = msg.ChannelID
	} else if threadID.ThreadTS == "" {
		threadID.ThreadTS = fmt.Sprintf("%d", time.Now().UnixNano())
	}

	threadKey := threadID.Key()
	log.Printf("[BOT_MANAGER] Incoming message from %s user=%s thread=%s: %s", msg.Platform, msg.UserID, threadKey, botTruncate(msg.Text, 100))

	// Check if there's an existing in-memory session for this thread.
	// Bot sessions are pure in-memory now — a reply that arrives after a
	// server restart simply starts a new session.
	m.mu.RLock()
	active, exists := m.sessions[threadKey]
	m.mu.RUnlock()

	if exists {
		m.handleExistingSession(active, msg, supportsThreads)
		return
	}

	go m.startNewSessionDirect(msg, threadID)
}

// HandleInteraction processes a button click from a platform
func (m *BotConversationManager) HandleInteraction(platform, channelID, threadTS, actionID, value, userID string) {
	threadKey := fmt.Sprintf("%s:%s:%s", platform, channelID, threadTS)

	m.mu.RLock()
	active, exists := m.sessions[threadKey]
	m.mu.RUnlock()

	if !exists {
		log.Printf("[BOT_MANAGER] Interaction for unknown thread: %s", threadKey)
		return
	}

	switch value {
	case "cancel":
		m.cancelSession(active, "Canceled by user")
	default:
		log.Printf("[BOT_MANAGER] Unknown interaction value: %s for thread %s", value, threadKey)
	}
}

// handleExistingSession routes a message to an existing session based on its status
func (m *BotConversationManager) handleExistingSession(active *activeBotSession, msg BotIncomingMessage, supportsThreads bool) {
	active.mu.Lock()
	status := active.Status
	awaiting := active.awaitingUserInput
	active.mu.Unlock()

	switch status {
	case chathistory.BotSessionStatusRunning, chathistory.BotSessionStatusAwaitingPlanApproval:
		// Check if session is waiting for user input (any blocking event)
		if awaiting {
			m.handleBlockingResponse(active, msg)
			return
		}

		// For thread-less platforms, check for explicit session end commands
		if !supportsThreads && isSessionEndCommand(msg.Text) {
			log.Printf("[BOT_MANAGER] Session end command received for %s", active.SessionID)
			m.cancelSession(active, "Session ended by user.")
			return
		}
		if !supportsThreads {
			connector := m.GetConnector(active.Platform)
			if connector != nil {
				connector.SendThreadMessage(context.Background(), active.ThreadID,
					"A session is currently running. Reply 'done' to end it, or wait for it to complete.")
			}
			return
		}
		// For @mentions, always inject as follow-up.
		// For plain thread replies, only inject if it's a single-user thread (the bot's original requester).
		// In multi-user threads, require @mention to avoid accidental triggers.
		if !msg.IsMention {
			if m.isMultiUserThread(active, msg) {
				log.Printf("[BOT_MANAGER] Ignoring non-mention thread reply from %s in multi-user session %s", msg.UserID, active.SessionID)
				return
			}
			log.Printf("[BOT_MANAGER] Accepting non-mention reply from %s (single-user thread) in session %s", msg.UserID, active.SessionID)
		}
		// Inject follow-up message into the running session
		active.mu.Lock()
		sid := active.SessionID
		uid := active.UserID
		active.mu.Unlock()
		if m.followUpSession != nil && sid != "" {
			log.Printf("[BOT_MANAGER] Sending follow-up to session %s: %s", sid, botTruncate(msg.Text, 80))
			go func() {
				followCtx, followCancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer followCancel()
				err := m.followUpSession(followCtx, m.buildQueryRequest(msg.Text, uid, ""), sid, uid)
				if err != nil {
					log.Printf("[BOT_MANAGER] Follow-up failed: %v", err)
				}
			}()
		} else {
			log.Printf("[BOT_MANAGER] Cannot send follow-up: followUpSession=%v sessionID=%s", m.followUpSession != nil, sid)
		}
	case chathistory.BotSessionStatusCompleted, chathistory.BotSessionStatusFailed:
		threadID := active.ThreadID
		go m.startNewSessionDirect(msg, threadID)
	}
}

// handleBlockingResponse handles a user response to a blocking event (plan_approval, human feedback, etc.)
func (m *BotConversationManager) handleBlockingResponse(active *activeBotSession, msg BotIncomingMessage) {
	text := botNormalizeText(msg.Text)

	active.mu.Lock()
	blockingEvt := active.blockingEventType
	sid := active.SessionID
	uid := active.UserID
	active.mu.Unlock()

	switch blockingEvt {
	case "plan_approval":
		if isPlanApprovalResponse(text) {
			log.Printf("[BOT_MANAGER] Plan approved for session %s", sid)
			m.clearBlockingState(active)
			if m.followUpSession != nil && sid != "" {
				go func() {
					followCtx, followCancel := context.WithTimeout(context.Background(), 5*time.Minute)
					defer followCancel()
					err := m.followUpSession(followCtx, m.buildQueryRequest("Approved. Execute the plan.", uid, ""), sid, uid)
					if err != nil {
						log.Printf("[BOT_MANAGER] Plan approval follow-up failed: %v", err)
					}
				}()
			}
			return
		} else if isPlanRejectionResponse(text) {
			log.Printf("[BOT_MANAGER] Plan rejected for session %s", sid)
			m.clearBlockingState(active)
			m.cancelSession(active, "Plan rejected. Let me know if you'd like to try a different approach!")
			return
		}
		// Not a clear approve/reject — send as feedback to the agent
		if m.followUpSession != nil && sid != "" {
			go func() {
				followCtx, followCancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer followCancel()
				err := m.followUpSession(followCtx, m.buildQueryRequest(msg.Text, uid, ""), sid, uid)
				if err != nil {
					log.Printf("[BOT_MANAGER] Plan feedback follow-up failed: %v", err)
				}
			}()
		}

	default:
		// blocking_human_feedback, blocking_human_questions, or unknown — forward as follow-up
		log.Printf("[BOT_MANAGER] Responding to %s for session %s: %s", blockingEvt, sid, botTruncate(msg.Text, 80))
		m.clearBlockingState(active)
		if m.followUpSession != nil && sid != "" {
			go func() {
				followCtx, followCancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer followCancel()
				err := m.followUpSession(followCtx, m.buildQueryRequest(msg.Text, uid, ""), sid, uid)
				if err != nil {
					log.Printf("[BOT_MANAGER] Blocking response follow-up failed: %v", err)
				}
			}()
		}
	}
}

// handleBlockingResponseSync handles a user response to a blocking event synchronously (web simulator)
func (m *BotConversationManager) handleBlockingResponseSync(ctx context.Context, active *activeBotSession, msg BotIncomingMessage, threadID ThreadID) (*SyncMessageResult, error) {
	text := botNormalizeText(msg.Text)

	active.mu.Lock()
	blockingEvt := active.blockingEventType
	sid := active.SessionID
	uid := active.UserID
	active.mu.Unlock()

	switch blockingEvt {
	case "plan_approval":
		if isPlanApprovalResponse(text) {
			log.Printf("[BOT_MANAGER] HandleMessageSync: plan approved for session %s", sid)
			m.clearBlockingState(active)
			if m.followUpSession != nil {
				err := m.followUpSession(ctx, m.buildQueryRequest("Approved. Execute the plan.", uid, ""), sid, uid)
				if err != nil {
					return nil, fmt.Errorf("plan approval follow-up failed: %w", err)
				}
			}
			return &SyncMessageResult{
				Type:         "follow_up",
				ThreadID:     threadID.ThreadTS,
				SessionID:    sid,
				ThreadOffset: m.getThreadOffset(threadID),
			}, nil
		} else if isPlanRejectionResponse(text) {
			log.Printf("[BOT_MANAGER] HandleMessageSync: plan rejected for session %s", sid)
			m.clearBlockingState(active)
			rejectMsg := "Plan rejected. Let me know if you'd like to try a different approach!"
			m.cancelSession(active, rejectMsg)
			return &SyncMessageResult{
				Type:     "conversation",
				Response: rejectMsg,
				ThreadID: threadID.ThreadTS,
			}, nil
		}
		// Not a clear approve/reject — send as feedback
		if m.followUpSession != nil {
			err := m.followUpSession(ctx, m.buildQueryRequest(msg.Text, uid, ""), sid, uid)
			if err != nil {
				return nil, fmt.Errorf("plan feedback follow-up failed: %w", err)
			}
		}
		return &SyncMessageResult{
			Type:         "follow_up",
			ThreadID:     threadID.ThreadTS,
			SessionID:    sid,
			ThreadOffset: m.getThreadOffset(threadID),
		}, nil

	default:
		// blocking_human_feedback, blocking_human_questions — forward as follow-up
		log.Printf("[BOT_MANAGER] HandleMessageSync: responding to %s for session %s", blockingEvt, sid)
		m.clearBlockingState(active)
		if m.followUpSession != nil {
			err := m.followUpSession(ctx, m.buildQueryRequest(msg.Text, uid, ""), sid, uid)
			if err != nil {
				return nil, fmt.Errorf("blocking response follow-up failed: %w", err)
			}
		}
		return &SyncMessageResult{
			Type:         "follow_up",
			ThreadID:     threadID.ThreadTS,
			SessionID:    sid,
			ThreadOffset: m.getThreadOffset(threadID),
		}, nil
	}
}

// clearBlockingState resets the blocking state on the active session and event filter
func (m *BotConversationManager) clearBlockingState(active *activeBotSession) {
	active.mu.Lock()
	active.awaitingUserInput = false
	active.blockingEventType = ""
	active.Status = chathistory.BotSessionStatusRunning
	ef := active.eventFilter
	active.mu.Unlock()
	if ef != nil {
		ef.ClearBlockingState()
	}
}

// isSessionEndCommand checks if a message is an explicit session end command
func isSessionEndCommand(text string) bool {
	normalized := botNormalizeText(text)
	switch normalized {
	case "done", "end", "stop", "reset", "new session", "quit", "exit":
		return true
	}
	return false
}

// SyncMessageResult is the synchronous result of HandleMessageSync
type SyncMessageResult struct {
	Type         string `json:"type"`                     // "conversation" or "follow_up"
	Response     string `json:"response,omitempty"`       // text reply for conversation
	ThreadID     string `json:"thread_id"`
	SessionID    string `json:"session_id,omitempty"`     // internal chat session ID (for follow_up)
	BotSessionID string `json:"bot_session_id,omitempty"` // set when awaiting confirmation
	ThreadOffset int    `json:"thread_offset,omitempty"`  // current thread message count (for polling init)
}

// HandleMessageSync processes a message synchronously (web simulator).
// Every message either routes to an existing session or starts a new one immediately.
func (m *BotConversationManager) HandleMessageSync(ctx context.Context, msg BotIncomingMessage, threadID ThreadID) (*SyncMessageResult, error) {
	// Check for existing session on this thread
	m.mu.RLock()
	active, exists := m.sessions[threadID.Key()]
	m.mu.RUnlock()

	// Snapshot active session fields under lock for safe access
	var status, sessionID string
	var awaitingInput bool
	if exists {
		active.mu.Lock()
		status = active.Status
		sessionID = active.SessionID
		awaitingInput = active.awaitingUserInput
		active.mu.Unlock()
	}

	// 1. Running session awaiting user input (blocking event) → handle response
	if exists && awaitingInput && sessionID != "" {
		return m.handleBlockingResponseSync(ctx, active, msg, threadID)
	}

	// 2. Running session (not awaiting input) → inject follow-up
	if exists && status == chathistory.BotSessionStatusRunning && sessionID != "" && !awaitingInput {
		active.mu.Lock()
		uid := active.UserID
		active.mu.Unlock()
		log.Printf("[BOT_MANAGER] HandleMessageSync: found active session %s (status=%s) for thread %s", sessionID, status, threadID.Key())
		if m.followUpSession != nil {
			log.Printf("[BOT_MANAGER] HandleMessageSync: injecting follow-up into session %s: %s", sessionID, botTruncate(msg.Text, 80))
			err := m.followUpSession(ctx, m.buildQueryRequest(msg.Text, uid, ""), sessionID, uid)
			if err != nil {
				return nil, fmt.Errorf("follow-up failed: %w", err)
			}
		}
		return &SyncMessageResult{
			Type:         "follow_up",
			ThreadID:     threadID.ThreadTS,
			SessionID:    sessionID,
			ThreadOffset: m.getThreadOffset(threadID),
		}, nil
	}

	// 3. No active session (or completed/failed) → start new session immediately
	log.Printf("[BOT_MANAGER] HandleMessageSync: starting new session for thread %s", threadID.Key())

	// Resolve workspace user ID for per-user secrets
	workspaceUserID := m.resolveWorkspaceUserID(msg)

	newSessionID := uuid.New().String()
	botMeta := botMetaFromMsg(msg, threadID)

	// Load thread history for context continuity (e.g., user replies after hours)
	queryWithHistory := m.buildQueryWithThreadHistory(msg.Text, msg.Platform, threadID)
	queryReq := m.buildQueryRequest(queryWithHistory, workspaceUserID, msg.ChannelID)

	// Track as active session — bot sessions are in-memory only.
	m.mu.Lock()
	activeTask := &activeBotSession{
		SessionID: newSessionID,
		UserID:    workspaceUserID,
		Status:    chathistory.BotSessionStatusRunning,
		Platform:  msg.Platform,
		ThreadID:  threadID,
		Metadata:  botMeta,
	}
	m.sessions[threadID.Key()] = activeTask
	m.mu.Unlock()

	// Start the session in background
	go m.runSession(activeTask, queryReq)

	return &SyncMessageResult{
		Type:         "follow_up",
		ThreadID:     threadID.ThreadTS,
		SessionID:    newSessionID,
		ThreadOffset: m.getThreadOffset(threadID),
	}, nil
}

// startNewSessionDirect creates a unified bot chat session and starts it
// immediately (async path, used by Slack / @mention flows).
func (m *BotConversationManager) startNewSessionDirect(msg BotIncomingMessage, threadID ThreadID) {
	ctx := context.Background()

	workspaceUserID := m.resolveWorkspaceUserID(msg)

	sessionID := uuid.New().String()
	botMeta := botMetaFromMsg(msg, threadID)

	// Load thread history for context continuity (e.g., user replies after hours)
	queryWithHistory := m.buildQueryWithThreadHistory(msg.Text, msg.Platform, threadID)
	queryReq := m.buildQueryRequest(queryWithHistory, workspaceUserID, msg.ChannelID)

	// Track active session — bot sessions are in-memory only.
	m.mu.Lock()
	active := &activeBotSession{
		SessionID: sessionID,
		UserID:    workspaceUserID,
		Status:    chathistory.BotSessionStatusRunning,
		Platform:  msg.Platform,
		ThreadID:  threadID,
		Metadata:  botMeta,
	}
	m.sessions[threadID.Key()] = active
	m.mu.Unlock()

	connector := m.GetConnector(msg.Platform)
	if connector != nil {
		startMsg := "Starting session... (tag me to follow up in this thread)"
		log.Printf("[BOT_MANAGER] Sending starting message to thread %s", threadID.Key())
		if _, sendErr := connector.SendThreadMessage(ctx, threadID, startMsg); sendErr != nil {
			log.Printf("[BOT_MANAGER] Failed to send starting message to %s: %v", threadID.Key(), sendErr)
		}
	} else {
		log.Printf("[BOT_MANAGER] WARNING: no connector for platform %s", msg.Platform)
	}

	m.runSession(active, queryReq)
}

// runSession runs the agent session with event filtering and lifecycle management
func (m *BotConversationManager) runSession(active *activeBotSession, queryReq map[string]interface{}) {
	ctx := context.Background()

	connector := m.GetConnector(active.Platform)
	if connector == nil {
		return
	}

	// Set up event filter for streaming updates to thread
	sessionCtx, cancel := context.WithCancel(ctx)
	active.mu.Lock()
	active.cancel = cancel
	active.eventFilter = NewBotEventFilter(connector, active.ThreadID, active.SessionID, os.Getenv("PUBLIC_URL"), active.UserID)
	sessionID := active.SessionID
	userID := active.UserID
	active.mu.Unlock()

	// Wire up blocking event callback — any blocking event (human feedback, etc.)
	active.eventFilter.SetBlockingEventCallback(func(eventType string) {
		active.mu.Lock()
		log.Printf("[BOT_MANAGER] Blocking event %s for session %s", eventType, active.SessionID)
		active.awaitingUserInput = true
		active.blockingEventType = eventType
		active.mu.Unlock()
	})

	// Wire up session done callback — event filter signals when session is truly complete
	active.eventFilter.SetSessionDoneCallback(func() {
		log.Printf("[BOT_MANAGER] Session done callback for %s", active.SessionID)
		cancel()
	})

	if m.eventSubscriber != nil {
		log.Printf("[BOT_MANAGER] Starting event filter goroutine for session %s", sessionID)
		go active.eventFilter.Start(sessionCtx, m.eventSubscriber, sessionID)
	} else {
		log.Printf("[BOT_MANAGER] WARNING: eventSubscriber is nil, event filter NOT started for session %s", sessionID)
	}

	// Start the actual session in background — don't block on it.
	if m.startSession != nil {
		go func() {
			err := m.startSession(sessionCtx, queryReq, sessionID, userID, func(event *events.AgentEvent) {})
			if err != nil && sessionCtx.Err() == nil {
				log.Printf("[BOT_MANAGER] Session error: %v", err)
				connector.SendThreadMessage(ctx, active.ThreadID, fmt.Sprintf("Session failed: %v", err))
				active.mu.Lock()
				active.Status = chathistory.BotSessionStatusFailed
				active.mu.Unlock()
				cancel()
			}
		}()
	}

	// Block until event filter signals session is done (or context is canceled)
	<-sessionCtx.Done()

	// Session completed or was canceled
	active.mu.Lock()
	alreadyFailed := active.Status == chathistory.BotSessionStatusFailed
	if !alreadyFailed {
		active.Status = chathistory.BotSessionStatusCompleted
	}
	active.mu.Unlock()
	if !alreadyFailed {
		if _, sendErr := connector.SendThreadMessage(ctx, active.ThreadID, "Session completed."); sendErr != nil {
			log.Printf("[BOT_MANAGER] Failed to send completion message to %s: %v", active.ThreadID.Key(), sendErr)
		}
	}

	// Remove from active sessions map so a subsequent message starts a fresh session.
	m.mu.Lock()
	delete(m.sessions, active.ThreadID.Key())
	m.mu.Unlock()
}

// cancelSession cancels a bot session
func (m *BotConversationManager) cancelSession(active *activeBotSession, reason string) {
	ctx := context.Background()

	active.mu.Lock()
	cancelFn := active.cancel
	active.Status = chathistory.BotSessionStatusFailed
	active.mu.Unlock()

	if cancelFn != nil {
		cancelFn()
	}

	if reason != "" {
		connector := m.GetConnector(active.Platform)
		if connector != nil {
			connector.SendThreadMessage(ctx, active.ThreadID, reason)
		}
	}

	m.mu.Lock()
	delete(m.sessions, active.ThreadID.Key())
	m.mu.Unlock()
}

// IsBotSession reports whether a session id matches any bot conversation
// currently tracked in memory.
func (m *BotConversationManager) IsBotSession(sessionID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, s := range m.sessions {
		if s != nil && s.SessionID == sessionID {
			return true
		}
	}
	return false
}

// buildQueryWithThreadHistory loads thread history from the platform and prepends it as context
// to the user's current message. This ensures continuity when a user replies to an existing thread
// after the previous session has completed (e.g., hours later).
func (m *BotConversationManager) buildQueryWithThreadHistory(query string, platform string, threadID ThreadID) string {
	connector := m.GetConnector(platform)
	if connector == nil || !connector.SupportsThreads() {
		return query
	}

	history, err := connector.GetThreadHistory(context.Background(), threadID)
	if err != nil {
		log.Printf("[BOT_MANAGER] Failed to load thread history: %v", err)
		return query
	}

	// Filter out bot status messages (Starting session..., Session completed., etc.)
	// and keep only meaningful conversation turns
	var meaningful []ThreadMessage
	for _, msg := range history {
		text := strings.TrimSpace(msg.Text)
		if text == "" {
			continue
		}
		// Skip bot status/progress messages
		if msg.IsBot && (strings.HasPrefix(text, "Starting session...") || text == "Session completed." ||
			strings.HasPrefix(text, "Session failed:")) {
			continue
		}
		meaningful = append(meaningful, msg)
	}

	// No history (or only the current message) — no context needed
	if len(meaningful) <= 1 {
		return query
	}

	// Build conversation context from history (exclude the last message which is the current one)
	var parts []string
	parts = append(parts, "## Previous Conversation in This Thread\n")
	for _, msg := range meaningful[:len(meaningful)-1] {
		role := "User"
		if msg.IsBot {
			role = "Agent"
		} else if msg.UserName != "" {
			role = msg.UserName
		}
		parts = append(parts, fmt.Sprintf("**%s:** %s\n", role, msg.Text))
	}
	parts = append(parts, "---\n\n## Current Message\n")
	parts = append(parts, query)

	combined := strings.Join(parts, "\n")
	log.Printf("[BOT_MANAGER] Prepended %d messages of thread history to query", len(meaningful)-1)
	return combined
}

// resolveChannelWorkflow looks up the workflow preset ID for a given Slack channel ID.
// Returns "" if no routing is configured for the channel.
func (m *BotConversationManager) resolveChannelWorkflow(channelID string) string {
	botCfg, err := m.chatStore.GetBotConnectorConfig(context.Background(), "slack")
	if err != nil || botCfg == nil {
		return ""
	}
	routing := botCfg.AllowedChannels
	if routing == "" || routing == "[]" || routing == "{}" {
		return ""
	}
	var channelMap map[string]string
	if err := json.Unmarshal([]byte(routing), &channelMap); err != nil {
		return ""
	}
	return channelMap[channelID]
}

// buildQueryRequest constructs a request map for startSessionInternal.
// userID is the workspace user ID used for loading per-user secrets.
// channelID is used for channel→workflow routing: pass the incoming message's ChannelID for new
// sessions, or "" for follow-ups into existing sessions (routing is ignored for follow-ups).
func (m *BotConversationManager) buildQueryRequest(query string, userID string, channelID string) map[string]interface{} {
	req := map[string]interface{}{
		"query": query,
	}

	// Channel → workflow routing: if this Slack channel is mapped to a workflow, run it
	// instead of the default multi-agent chat mode.
	if channelID != "" {
		if workflowID := m.resolveChannelWorkflow(channelID); workflowID != "" {
			req["agent_mode"] = "workflow"
			req["preset_query_id"] = workflowID
			log.Printf("[BOT_MANAGER] Channel %s routed to workflow %s", channelID, workflowID)
		}
	}

	// No default servers — bot starts with no MCP servers (agent has workspace, delegation, and shell tools).
	req["servers"] = []string{}

	// Auto-discover all available skills.
	var defaultSkills []string
	if m.workspaceURL != "" {
		discoveredSkills, err := skills.DiscoverSkills(m.workspaceURL)
		if err == nil {
			for _, s := range discoveredSkills {
				defaultSkills = append(defaultSkills, s.FolderName)
			}
		}
	}
	req["selected_skills"] = defaultSkills

	// Load delegation tier config from workspace file — same source as multiagent chat.
	// server.go resolves the orchestrator model from this at request time via resolveDelegationTierConfig.
	if m.workspaceURL != "" {
		if tierConfig, exists, err := LoadDelegationTierConfig(context.Background(), m.workspaceURL); err != nil {
			log.Printf("[BOT_MANAGER] Warning: failed to load tier config from workspace: %v", err)
		} else if exists && len(tierConfig) > 0 {
			req["delegation_tier_config"] = tierConfig
			log.Printf("[BOT_MANAGER] Loaded delegation tier config from workspace file")
		}

		// Load provider API keys from the workspace encrypted file and inject
		// them into llm_config so handleQuery can use them for all providers.
		if apiKeys, exists, err := LoadProviderKeys(context.Background(), m.workspaceURL); err != nil {
			log.Printf("[BOT_MANAGER] Warning: failed to load provider keys from workspace: %v", err)
		} else if exists && len(apiKeys) > 0 {
			req["llm_config"] = map[string]interface{}{"api_keys": apiKeys}
			log.Printf("[BOT_MANAGER] Loaded %d provider API keys from workspace file", len(apiKeys))
		}
	}

	// Load server-side user secrets and inject as decrypted_secrets
	if m.loadUserSecrets != nil {
		secrets, err := m.loadUserSecrets(context.Background(), userID)
		if err != nil {
			log.Printf("[BOT_MANAGER] Failed to load user secrets: %v", err)
		} else if len(secrets) > 0 {
			secretsList := make([]map[string]string, len(secrets))
			for i, s := range secrets {
				secretsList[i] = map[string]string{"name": s.Name, "value": s.Value}
			}
			req["decrypted_secrets"] = secretsList
			log.Printf("[BOT_MANAGER] Loaded %d user secrets for bot session", len(secrets))
		}
	}

	log.Printf("[BOT_MANAGER] buildQueryRequest: query=%s skills=%v",
		botTruncate(query, 60), defaultSkills)

	return req
}

// isMultiUserThread checks if a thread has multiple distinct human users.
// If only the current message sender has posted (besides the bot), treat it as single-user.
func (m *BotConversationManager) isMultiUserThread(active *activeBotSession, msg BotIncomingMessage) bool {
	connector := m.GetConnector(active.Platform)
	if connector == nil || !connector.SupportsThreads() {
		return false
	}
	history, err := connector.GetThreadHistory(context.Background(), active.ThreadID)
	if err != nil {
		// Can't determine — be conservative, require @mention
		return true
	}
	users := make(map[string]bool)
	for _, m := range history {
		if !m.IsBot && m.UserID != "" {
			users[m.UserID] = true
		}
	}
	return len(users) > 1
}

// Helper functions (prefixed with bot to avoid collisions)

func botTruncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func botNormalizeText(s string) string {
	s = strings.TrimSpace(s)
	return strings.ToLower(s)
}

func isPlanApprovalResponse(text string) bool {
	switch text {
	case "approve", "approved", "execute", "go", "yes", "y", "ok", "proceed", "do it", "run it", "start", "lgtm":
		return true
	}
	return false
}

func isPlanRejectionResponse(text string) bool {
	switch text {
	case "reject", "rejected", "no", "n", "cancel", "stop", "nope", "nah", "abort":
		return true
	}
	return false
}

// getThreadOffset returns the current message count for a thread (web simulator only)
func (m *BotConversationManager) getThreadOffset(threadID ThreadID) int {
	connector := m.GetConnector(threadID.Platform)
	if wsc, ok := connector.(*WebSimulatorConnector); ok {
		return wsc.GetThreadMessageCount(threadID.ThreadTS)
	}
	return 0
}
