package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/manishiitg/mcpagent/events"

	"mcp-agent-builder-go/agent_go/pkg/database"
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
	Platform      string
	UserID        string
	UserName      string
	ChannelID     string
	ThreadTS      string // empty = new conversation, set = existing thread
	Text          string // @mention stripped
	Timestamp     time.Time
	IsThreadReply bool
	ThreadHistory []ThreadMessage // populated when tagged in existing thread
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

// BotConversationManager is the platform-agnostic orchestrator for bot sessions
type BotConversationManager struct {
	mu         sync.RWMutex
	connectors map[string]BotConnector      // platform name -> connector
	sessions   map[string]*activeBotSession // threadKey -> active session tracking

	db             database.Database
	eventSubscriber BotEventSubscriber
	analyzer       *BotAnalyzer

	// Function references set by server layer (to avoid import cycles)
	startSession  SessionStartFunc
	mcpConfigPath string
	workspaceURL  string
}

// activeBotSession tracks an in-progress bot conversation
type activeBotSession struct {
	BotSessionID string
	SessionID    string // internal chat session ID
	Status       string
	Platform     string
	ThreadID     ThreadID
	cancel       context.CancelFunc
	eventFilter  *BotEventFilter
}

// NewBotConversationManager creates a new manager
func NewBotConversationManager(db database.Database, mcpConfigPath, workspaceURL string) *BotConversationManager {
	return &BotConversationManager{
		connectors:    make(map[string]BotConnector),
		sessions:      make(map[string]*activeBotSession),
		db:            db,
		analyzer:      NewBotAnalyzer(mcpConfigPath, workspaceURL),
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

// HandleIncomingMessage processes a message from any platform
func (m *BotConversationManager) HandleIncomingMessage(msg BotIncomingMessage) {
	threadID := ThreadID{
		Platform:  msg.Platform,
		ChannelID: msg.ChannelID,
		ThreadTS:  msg.ThreadTS,
	}

	// If threadTS is empty, this is a new channel message — the message itself becomes the thread root
	if threadID.ThreadTS == "" {
		threadID.ThreadTS = fmt.Sprintf("%d", time.Now().UnixNano())
	}

	threadKey := threadID.Key()
	log.Printf("[BOT_MANAGER] Incoming message from %s user=%s thread=%s: %s", msg.Platform, msg.UserID, threadKey, botTruncate(msg.Text, 100))

	// Check if there's an existing session for this thread
	m.mu.RLock()
	active, exists := m.sessions[threadKey]
	m.mu.RUnlock()

	if exists {
		m.handleExistingSession(active, msg)
		return
	}

	// Also check DB for active sessions on this thread
	existing, err := m.db.GetBotSessionByThread(context.Background(), msg.Platform, msg.ChannelID, threadID.ThreadTS)
	if err != nil {
		log.Printf("[BOT_MANAGER] Error looking up thread: %v", err)
	}

	if existing != nil && (existing.Status == database.BotSessionStatusRunning || existing.Status == database.BotSessionStatusAnalyzing || existing.Status == database.BotSessionStatusAwaitingConfirmation) {
		// Rehydrate active session
		m.mu.Lock()
		active = &activeBotSession{
			BotSessionID: existing.ID,
			SessionID:    existing.SessionID,
			Status:       existing.Status,
			Platform:     existing.Platform,
			ThreadID:     threadID,
		}
		m.sessions[threadKey] = active
		m.mu.Unlock()
		m.handleExistingSession(active, msg)
		return
	}

	// Start new bot session
	go m.startNewSession(msg, threadID)
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
	case "confirm":
		if active.Status == database.BotSessionStatusAwaitingConfirmation {
			go m.confirmAndStart(active)
		}
	case "cancel":
		m.cancelSession(active, "Cancelled by user")
	default:
		log.Printf("[BOT_MANAGER] Unknown interaction value: %s for thread %s", value, threadKey)
	}
}

// handleExistingSession routes a message to an existing session based on its status
func (m *BotConversationManager) handleExistingSession(active *activeBotSession, msg BotIncomingMessage) {
	switch active.Status {
	case database.BotSessionStatusRunning:
		log.Printf("[BOT_MANAGER] Forwarding follow-up to running session %s", active.SessionID)
		// TODO: inject user message into running session via handleQuery continuation
	case database.BotSessionStatusAwaitingConfirmation:
		text := botNormalizeText(msg.Text)
		if text == "yes" || text == "confirm" || text == "go" || text == "ok" {
			go m.confirmAndStart(active)
		} else if text == "no" || text == "cancel" || text == "stop" {
			m.cancelSession(active, "Cancelled by user")
		}
	case database.BotSessionStatusCompleted, database.BotSessionStatusFailed:
		threadID := active.ThreadID
		go m.startNewSession(msg, threadID)
	}
}

// startNewSession initiates the analyze → confirm → run flow
func (m *BotConversationManager) startNewSession(msg BotIncomingMessage, threadID ThreadID) {
	ctx := context.Background()

	// Create bot session in DB
	botSession, err := m.db.CreateBotSession(ctx, &database.CreateBotSessionRequest{
		Platform:  msg.Platform,
		ChannelID: msg.ChannelID,
		ThreadTS:  threadID.ThreadTS,
		UserID:    msg.UserID,
		UserName:  msg.UserName,
		Query:     msg.Text,
	})
	if err != nil {
		log.Printf("[BOT_MANAGER] Failed to create bot session: %v", err)
		return
	}

	// Track active session
	m.mu.Lock()
	active := &activeBotSession{
		BotSessionID: botSession.ID,
		Status:       database.BotSessionStatusAnalyzing,
		Platform:     msg.Platform,
		ThreadID:     threadID,
	}
	m.sessions[threadID.Key()] = active
	m.mu.Unlock()

	// Get connector
	connector := m.GetConnector(msg.Platform)
	if connector == nil {
		log.Printf("[BOT_MANAGER] No connector for platform: %s", msg.Platform)
		return
	}

	// Send "analyzing" message
	msgID, err := connector.SendThreadMessage(ctx, threadID, "Analyzing your request...")
	if err != nil {
		log.Printf("[BOT_MANAGER] Failed to send analyzing message: %v", err)
	}
	m.recordMessage(botSession.ID, "outgoing", "analysis", "Analyzing your request...", msgID)

	// Fetch thread history if this is an existing thread
	var threadHistory []ThreadMessage
	if msg.IsThreadReply && len(msg.ThreadHistory) == 0 {
		threadHistory, err = connector.GetThreadHistory(ctx, threadID)
		if err != nil {
			log.Printf("[BOT_MANAGER] Failed to get thread history: %v", err)
		}
	} else {
		threadHistory = msg.ThreadHistory
	}

	// Store thread context
	if len(threadHistory) > 0 {
		contextJSON, _ := json.Marshal(threadHistory)
		m.db.UpdateBotSession(ctx, botSession.ID, &database.UpdateBotSessionRequest{
			ConfigJSON: string(contextJSON),
		})
	}

	// Run analysis
	analysis, err := m.analyzer.Analyze(ctx, msg.Text, threadHistory)
	if err != nil {
		log.Printf("[BOT_MANAGER] Analysis failed: %v", err)
		connector.UpdateMessage(ctx, threadID, msgID, fmt.Sprintf("Analysis failed: %v", err))
		m.cancelSession(active, fmt.Sprintf("Analysis failed: %v", err))
		return
	}

	// Store analysis result
	analysisJSON, _ := json.Marshal(analysis)
	m.db.UpdateBotSession(ctx, botSession.ID, &database.UpdateBotSessionRequest{
		AnalysisJSON: string(analysisJSON),
		Status:       database.BotSessionStatusAwaitingConfirmation,
		PresetID:     analysis.MatchedPresetID,
	})

	active.Status = database.BotSessionStatusAwaitingConfirmation

	// Check auto-confirm
	botConfig, _ := m.db.GetBotConnectorConfig(ctx, msg.Platform)
	if botConfig != nil && botConfig.AutoConfirm {
		go m.confirmAndStart(active)
		return
	}

	// Send confirmation message with buttons
	confirmMsg := m.buildConfirmationMessage(analysis)
	blocks := []MessageBlock{
		{
			Type: "actions",
			Buttons: []MessageButton{
				{Text: "Confirm", Value: "confirm", Style: "primary", ActionID: fmt.Sprintf("bot_confirm_%s", botSession.ID)},
				{Text: "Cancel", Value: "cancel", Style: "danger", ActionID: fmt.Sprintf("bot_cancel_%s", botSession.ID)},
			},
		},
	}
	confirmMsgID, err := connector.SendThreadMessageWithBlocks(ctx, threadID, confirmMsg, blocks)
	if err != nil {
		log.Printf("[BOT_MANAGER] Failed to send confirmation: %v", err)
	}
	m.recordMessage(botSession.ID, "outgoing", "confirmation", confirmMsg, confirmMsgID)
}

// confirmAndStart begins the actual agent session
func (m *BotConversationManager) confirmAndStart(active *activeBotSession) {
	ctx := context.Background()

	botSession, err := m.db.GetBotSession(ctx, active.BotSessionID)
	if err != nil {
		log.Printf("[BOT_MANAGER] Failed to get bot session: %v", err)
		return
	}

	var analysis AnalysisResult
	if botSession.AnalysisJSON != "" {
		if err := json.Unmarshal([]byte(botSession.AnalysisJSON), &analysis); err != nil {
			log.Printf("[BOT_MANAGER] Failed to parse analysis: %v", err)
			return
		}
	}

	sessionID := uuid.New().String()
	queryReq := m.buildQueryRequest(analysis, botSession)

	m.db.UpdateBotSession(ctx, active.BotSessionID, &database.UpdateBotSessionRequest{
		SessionID: sessionID,
		Status:    database.BotSessionStatusRunning,
	})
	active.SessionID = sessionID
	active.Status = database.BotSessionStatusRunning

	connector := m.GetConnector(active.Platform)
	if connector == nil {
		return
	}

	startMsg := fmt.Sprintf("Starting session... (%s)", analysis.Summary)
	msgID, _ := connector.SendThreadMessage(ctx, active.ThreadID, startMsg)
	m.recordMessage(active.BotSessionID, "outgoing", "progress", startMsg, msgID)

	// Set up event filter for streaming updates to thread
	sessionCtx, cancel := context.WithCancel(ctx)
	active.cancel = cancel
	active.eventFilter = NewBotEventFilter(connector, active.ThreadID, active.BotSessionID, m.db)
	if m.eventSubscriber != nil {
		go active.eventFilter.Start(sessionCtx, m.eventSubscriber, sessionID)
	}

	// Start the actual session
	if m.startSession != nil {
		err = m.startSession(sessionCtx, queryReq, sessionID, "default", func(event *events.AgentEvent) {
			// Events are also handled by the event filter via EventStore subscription
		})
		if err != nil {
			log.Printf("[BOT_MANAGER] Session failed: %v", err)
			connector.SendThreadMessage(ctx, active.ThreadID, fmt.Sprintf("Session failed: %v", err))
			m.db.CompleteBotSession(ctx, active.BotSessionID, database.BotSessionStatusFailed)
			active.Status = database.BotSessionStatusFailed
			return
		}
	}

	// Session completed
	connector.SendThreadMessage(ctx, active.ThreadID, "Session completed.")
	m.db.CompleteBotSession(ctx, active.BotSessionID, database.BotSessionStatusCompleted)
	active.Status = database.BotSessionStatusCompleted

	if active.cancel != nil {
		active.cancel()
	}
}

// cancelSession cancels a bot session
func (m *BotConversationManager) cancelSession(active *activeBotSession, reason string) {
	ctx := context.Background()

	if active.cancel != nil {
		active.cancel()
	}

	m.db.CompleteBotSession(ctx, active.BotSessionID, database.BotSessionStatusFailed)
	active.Status = database.BotSessionStatusFailed

	connector := m.GetConnector(active.Platform)
	if connector != nil {
		connector.SendThreadMessage(ctx, active.ThreadID, reason)
	}

	m.mu.Lock()
	delete(m.sessions, active.ThreadID.Key())
	m.mu.Unlock()
}

// IsBotSession checks if a sessionID belongs to a bot session
func (m *BotConversationManager) IsBotSession(sessionID string) bool {
	bs, err := m.db.GetBotSessionBySessionID(context.Background(), sessionID)
	return err == nil && bs != nil
}

// GetBotSessionForInternal returns the bot session for an internal session ID
func (m *BotConversationManager) GetBotSessionForInternal(sessionID string) *database.BotSession {
	bs, err := m.db.GetBotSessionBySessionID(context.Background(), sessionID)
	if err != nil {
		return nil
	}
	return bs
}

// buildConfirmationMessage creates a human-readable confirmation message
func (m *BotConversationManager) buildConfirmationMessage(analysis *AnalysisResult) string {
	msg := fmt.Sprintf("Here's what I'll set up:\n\n*%s*\n", analysis.Summary)

	if analysis.DelegationMode == "plan" {
		msg += "- Mode: Multi-Agent Chat\n"
	} else {
		msg += "- Mode: Simple Chat\n"
	}

	if len(analysis.RequiredServers) > 0 {
		msg += fmt.Sprintf("- MCP Servers: %s\n", botJoinStrings(analysis.RequiredServers))
	}
	if len(analysis.RequiredSkills) > 0 {
		msg += fmt.Sprintf("- Skills: %s\n", botJoinStrings(analysis.RequiredSkills))
	}
	if analysis.NeedsBrowser {
		msg += "- Browser access: Yes\n"
	}
	if analysis.NeedsWorkspace {
		msg += "- Workspace access: Yes\n"
	}

	if analysis.MatchedPresetName != "" {
		msg += fmt.Sprintf("- Based on preset: %s\n", analysis.MatchedPresetName)
	}

	return msg
}

// buildQueryRequest constructs a map from the analysis result suitable for startSessionInternal
func (m *BotConversationManager) buildQueryRequest(analysis AnalysisResult, botSession *database.BotSession) map[string]interface{} {
	query := analysis.RewrittenQuery
	if query == "" {
		query = botSession.Query
	}

	req := map[string]interface{}{
		"query":                   query,
		"servers":                 analysis.RequiredServers,
		"selected_skills":        analysis.RequiredSkills,
		"selected_subagents":     analysis.RequiredSubAgents,
		"delegation_mode":        analysis.DelegationMode,
		"enable_workspace_access": analysis.NeedsWorkspace,
		"enable_browser_access":   analysis.NeedsBrowser,
	}

	if analysis.MatchedPresetID != "" {
		req["preset_query_id"] = analysis.MatchedPresetID
	}

	return req
}

// recordMessage stores a bot message in the database
func (m *BotConversationManager) recordMessage(botSessionID, direction, msgType, content, platformMsgID string) {
	_, err := m.db.CreateBotMessage(context.Background(), &database.CreateBotMessageRequest{
		BotSessionID:      botSessionID,
		Direction:         direction,
		MessageType:       msgType,
		Content:           content,
		PlatformMessageID: platformMsgID,
	})
	if err != nil {
		log.Printf("[BOT_MANAGER] Failed to record message: %v", err)
	}
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

func botJoinStrings(ss []string) string {
	return strings.Join(ss, ", ")
}
