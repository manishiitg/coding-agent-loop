package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

// FeedbackStoreFunc is a function type for creating feedback requests (to avoid import cycle)
type FeedbackStoreFunc func(uniqueID string, message string) error

// FeedbackResponseFunc is defined in notification_manager.go (same package)

var (
	// createFeedbackRequest is used for test connections only
	// For production feedback requests, use notification manager instead
	createFeedbackRequest FeedbackStoreFunc
)

// SetFeedbackStoreFuncs sets the feedback store function for creating test requests
// Note: For receiving feedback, use notification manager's ReceiveNotification instead
func SetFeedbackStoreFuncs(createFn FeedbackStoreFunc) {
	createFeedbackRequest = createFn
}

// SlackConfig represents Slack configuration (Socket Mode only)
type SlackConfig struct {
	Enabled   bool   `json:"enabled"`
	BotToken  string `json:"bot_token"` // Bot User OAuth Token (xoxb-...)
	AppToken  string `json:"app_token"` // App-level token (xapp-...) for Socket Mode
	ChannelID string `json:"channel_id"`
}

// SlackService handles Slack integration for human feedback
// It implements the NotificationConnector interface
type SlackService struct {
	db            *sql.DB
	client        *slack.Client
	socketClient  *socketmode.Client // Socket Mode client for WebSocket connection
	config        *SlackConfig
	enabled       bool
	channelID     string
	useSocketMode bool
	socketCtx     context.Context
	socketCancel  context.CancelFunc
	socketMux     sync.RWMutex
}

// Name returns the name of this connector
func (s *SlackService) Name() string {
	return "slack"
}

// SendNotification implements NotificationConnector interface
// This is a wrapper around SendFeedbackNotification for the interface
func (s *SlackService) SendNotification(ctx context.Context, uniqueID string, message string, contextMsg string, buttonOptions *ButtonOptions) (string, error) {
	return s.SendFeedbackNotification(ctx, uniqueID, message, contextMsg, buttonOptions)
}

var (
	globalSlackService *SlackService
	slackServiceMux    sync.RWMutex
)

// SetSlackService sets the global Slack service instance
func SetSlackService(service *SlackService) {
	slackServiceMux.Lock()
	defer slackServiceMux.Unlock()
	globalSlackService = service
}

// GetSlackService returns the global Slack service instance (may be nil if not initialized)
func GetSlackService() *SlackService {
	slackServiceMux.RLock()
	defer slackServiceMux.RUnlock()
	return globalSlackService
}

// InitSlackService initializes the Slack service with database connection
// This is called on server startup and will automatically start Socket Mode if config is enabled
func InitSlackService(db *sql.DB) (*SlackService, error) {
	service := &SlackService{
		db: db,
	}

	// Load config first
	err := service.loadConfig(context.Background())
	if err != nil {
		SetSlackService(service)
		return service, err
	}

	// ReloadConfig will start Socket Mode if config is enabled and valid
	// This ensures Socket Mode connects on server startup
	if err := service.ReloadConfig(context.Background()); err != nil {
		log.Printf("[SLACK] Failed to reload config on initialization: %v", err)
		// Don't fail initialization, just log the error
	}

	SetSlackService(service)
	return service, nil
}

// loadConfig loads Slack configuration from database (Socket Mode only)
func (s *SlackService) loadConfig(ctx context.Context) error {
	query := `SELECT enabled, bot_token, channel_id, COALESCE(app_token, '') as app_token
	          FROM slack_feedback_config 
	          WHERE id = 'slack_config'`

	var enabled bool
	var botToken, channelID, appToken sql.NullString

	err := s.db.QueryRowContext(ctx, query).Scan(&enabled, &botToken, &channelID, &appToken)
	if err != nil {
		if err == sql.ErrNoRows {
			// No config exists yet, return nil with empty config
			s.config = &SlackConfig{Enabled: false}
			return nil
		}
		return fmt.Errorf("failed to load Slack config: %w", err)
	}

	s.config = &SlackConfig{
		Enabled:   enabled,
		BotToken:  botToken.String,
		ChannelID: channelID.String,
		AppToken:  appToken.String,
	}

	return nil
}

// ReloadConfig reloads configuration from database
func (s *SlackService) ReloadConfig(ctx context.Context) error {
	// Stop existing Socket Mode connection if running
	s.StopSocketMode()

	if err := s.loadConfig(ctx); err != nil {
		return err
	}

	// Only enable if all required fields are present (Socket Mode requires bot token, app token, and channel)
	if s.config != nil && s.config.Enabled && s.config.BotToken != "" && s.config.AppToken != "" && s.config.ChannelID != "" {
		s.client = slack.New(s.config.BotToken)
		s.enabled = true
		s.channelID = s.config.ChannelID
		s.useSocketMode = true

		// Start Socket Mode connection
		if err := s.StartSocketMode(ctx); err != nil {
			log.Printf("[SLACK] Failed to start Socket Mode: %v", err)
			s.enabled = false
			s.useSocketMode = false
		} else {
			log.Printf("[SLACK] Service enabled with Socket Mode: channel=%s", s.channelID)
		}
	} else {
		s.client = nil
		s.enabled = false
		s.channelID = ""
		s.useSocketMode = false
		if s.config != nil {
			log.Printf("[SLACK] Service disabled: enabled=%v, hasBotToken=%v, hasAppToken=%v, hasChannelID=%v",
				s.config.Enabled, s.config.BotToken != "", s.config.AppToken != "", s.config.ChannelID != "")
		}
	}

	return nil
}

// IsEnabled checks if Slack notifications are enabled
func (s *SlackService) IsEnabled() bool {
	return s.enabled && s.client != nil && s.channelID != ""
}

// SendFeedbackNotification sends a feedback request to Slack
// Returns Slack message timestamp for tracking
func (s *SlackService) SendFeedbackNotification(
	ctx context.Context,
	uniqueID string,
	message string,
	contextMsg string,
	buttonOptions *ButtonOptions,
) (string, error) {
	if !s.IsEnabled() {
		return "", fmt.Errorf("slack service is not enabled")
	}

	// Format Slack message with blocks
	blocks := formatSlackMessage(uniqueID, message, contextMsg, buttonOptions)

	// Post message to Slack
	// Note: Slack metadata API is limited, so we'll include unique_id in message text instead
	// Add unique_id to footer for tracking
	footerText := fmt.Sprintf("Reply to this message to provide feedback\nRequest ID: `%s`", uniqueID)
	blocks = append(blocks, slack.NewContextBlock(
		"",
		slack.NewTextBlockObject("mrkdwn", footerText, false, false),
	))

	channelID, timestamp, err := s.client.PostMessage(
		s.channelID,
		slack.MsgOptionBlocks(blocks...),
	)

	if err != nil {
		return "", fmt.Errorf("failed to post Slack message: %w", err)
	}

	// Store message mapping
	if err := s.StoreMessageMapping(ctx, uniqueID, timestamp, channelID); err != nil {
		log.Printf("[SLACK] Failed to store message mapping: %v", err)
		// Don't fail the whole operation if mapping storage fails
	}

	return timestamp, nil
}

// convertMarkdownToSlackMrkdwn converts common markdown patterns to Slack's mrkdwn format
// Slack mrkdwn supports: *bold*, _italic_, ~strikethrough~, `code`, > quotes, lists
// It does NOT support: headers (#), tables, complex nested structures
func convertMarkdownToSlackMrkdwn(text string) string {
	if text == "" {
		return text
	}

	result := text

	// Convert headers to bold (## Header -> *Header*)
	// Match headers with 1-6 # symbols
	headerRegex := regexp.MustCompile(`(?m)^(#{1,6})\s+(.+)$`)
	result = headerRegex.ReplaceAllStringFunc(result, func(match string) string {
		parts := headerRegex.FindStringSubmatch(match)
		if len(parts) == 3 {
			// Convert header to bold
			return fmt.Sprintf("*%s*", strings.TrimSpace(parts[2]))
		}
		return match
	})

	// Convert code blocks (```language\ncode\n```) to ```code```
	// Slack supports code blocks with triple backticks
	codeBlockRegex := regexp.MustCompile("(?s)```(?:[a-zA-Z]+)?\\n(.*?)```")
	result = codeBlockRegex.ReplaceAllStringFunc(result, func(match string) string {
		// Extract code content
		codeContent := codeBlockRegex.FindStringSubmatch(match)
		if len(codeContent) == 2 {
			return fmt.Sprintf("```\n%s\n```", strings.TrimSpace(codeContent[1]))
		}
		return match
	})

	// Convert markdown links [text](url) to Slack format <url|text>
	linkRegex := regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	result = linkRegex.ReplaceAllStringFunc(result, func(match string) string {
		parts := linkRegex.FindStringSubmatch(match)
		if len(parts) == 3 {
			// Slack format: <url|text>
			return fmt.Sprintf("<%s|%s>", parts[2], parts[1])
		}
		return match
	})

	// Convert markdown horizontal rules (---) to a simple separator
	result = regexp.MustCompile(`(?m)^---+$`).ReplaceAllString(result, "---")

	// Note: Other markdown features like tables are not supported by Slack
	// They will be displayed as plain text, which is acceptable

	return result
}

// formatSlackMessage formats a feedback request as Slack blocks
func formatSlackMessage(uniqueID, question, contextMsg string, buttonOptions *ButtonOptions) []slack.Block {
	blocks := []slack.Block{
		slack.NewHeaderBlock(slack.NewTextBlockObject("plain_text", "🤔 Human Feedback Required", false, false)),
	}

	// Convert markdown to Slack mrkdwn format
	convertedQuestion := convertMarkdownToSlackMrkdwn(question)
	convertedContext := convertMarkdownToSlackMrkdwn(contextMsg)

	// Question section with @channel mention to notify everyone
	questionText := fmt.Sprintf("<!channel>\n\n*Question:*\n%s", convertedQuestion)
	if contextMsg != "" {
		questionText += fmt.Sprintf("\n\n*Context:*\n%s", convertedContext)
	}

	blocks = append(blocks, slack.NewSectionBlock(
		slack.NewTextBlockObject("mrkdwn", questionText, false, false),
		nil, nil,
	))

	// Add interactive buttons if button options are provided
	if buttonOptions != nil {
		var buttonElements []slack.BlockElement

		if buttonOptions.YesNoOnly {
			// Yes/No buttons
			yesLabel := buttonOptions.YesLabel
			if yesLabel == "" {
				yesLabel = "Approve"
			}
			noLabel := buttonOptions.NoLabel
			if noLabel == "" {
				noLabel = "Reject"
			}

			// Create Yes button (primary style)
			yesButton := slack.NewButtonBlockElement(
				fmt.Sprintf("feedback_yes_%s", uniqueID),
				uniqueID,
				slack.NewTextBlockObject("plain_text", yesLabel, false, false),
			)
			yesButton.Style = "primary"

			// Create No button (danger style)
			noButton := slack.NewButtonBlockElement(
				fmt.Sprintf("feedback_no_%s", uniqueID),
				uniqueID,
				slack.NewTextBlockObject("plain_text", noLabel, false, false),
			)
			noButton.Style = "danger"

			buttonElements = []slack.BlockElement{yesButton, noButton}
		} else if len(buttonOptions.Options) > 0 {
			// Multiple choice buttons
			for i, optionLabel := range buttonOptions.Options {
				actionID := fmt.Sprintf("feedback_option_%d_%s", i, uniqueID)
				button := slack.NewButtonBlockElement(
					actionID,
					uniqueID,
					slack.NewTextBlockObject("plain_text", optionLabel, false, false),
				)
				buttonElements = append(buttonElements, button)
			}
		}

		// Add action block with buttons if we have any
		if len(buttonElements) > 0 {
			// Slack allows max 5 buttons per action block, so we may need multiple blocks
			maxButtonsPerBlock := 5
			for i := 0; i < len(buttonElements); i += maxButtonsPerBlock {
				end := i + maxButtonsPerBlock
				if end > len(buttonElements) {
					end = len(buttonElements)
				}
				actionBlock := slack.NewActionBlock(
					fmt.Sprintf("feedback_actions_%s_%d", uniqueID, i/maxButtonsPerBlock),
					buttonElements[i:end]...,
				)
				blocks = append(blocks, actionBlock)
			}
		}
	}

	// Footer
	blocks = append(blocks, slack.NewContextBlock(
		"",
		slack.NewTextBlockObject("mrkdwn", "Reply to this message or click a button to provide feedback", false, false),
	))

	return blocks
}

// StoreMessageMapping stores the mapping between unique_id and Slack message
func (s *SlackService) StoreMessageMapping(
	ctx context.Context,
	uniqueID string,
	messageTS string,
	channelID string,
) error {
	query := `INSERT INTO slack_feedback_messages (unique_id, slack_message_ts, slack_channel_id, slack_thread_ts)
	          VALUES (?, ?, ?, ?)
	          ON CONFLICT(unique_id, slack_message_ts) DO NOTHING`

	_, err := s.db.ExecContext(ctx, query, uniqueID, messageTS, channelID, messageTS)
	if err != nil {
		return fmt.Errorf("failed to store message mapping: %w", err)
	}

	return nil
}

// GetUniqueIDFromThread retrieves unique_id from a Slack thread reply
func (s *SlackService) GetUniqueIDFromThread(
	ctx context.Context,
	threadTS string,
	channelID string,
) (string, error) {
	// First, try to get from database mapping (thread_ts is the parent message timestamp)
	query := `SELECT unique_id FROM slack_feedback_messages 
	          WHERE slack_message_ts = ? AND slack_channel_id = ?
	          LIMIT 1`

	var uniqueID string
	err := s.db.QueryRowContext(ctx, query, threadTS, channelID).Scan(&uniqueID)
	if err == nil {
		return uniqueID, nil
	}

	// If not found in DB, try to get from Slack message using conversations.replies API
	if s.client == nil {
		return "", fmt.Errorf("Slack client not initialized")
	}

	// Get the parent message using conversations.replies
	// GetConversationReplies returns: (msgs []Message, hasMore bool, nextCursor string, err error)
	replies, _, _, err := s.client.GetConversationReplies(&slack.GetConversationRepliesParameters{
		ChannelID: channelID,
		Timestamp: threadTS,
		Limit:     1,
	})

	if err != nil {
		return "", fmt.Errorf("failed to get Slack message: %w", err)
	}

	if len(replies) == 0 {
		return "", fmt.Errorf("message not found")
	}

	// Extract unique_id from message text (we include it in the footer)
	// For now, we'll rely on database mapping primarily
	// If not in DB, we can't reliably extract it from message text

	return "", fmt.Errorf("unique_id not found - message may not be a feedback request")
}

// TestConnectionWithConfig tests Slack connection with provided config (without saving)
// Returns the test unique ID so the frontend can poll for replies
func (s *SlackService) TestConnectionWithConfig(ctx context.Context, config *SlackConfig) (string, error) {
	// Validate config
	if config == nil {
		return "", fmt.Errorf("config is required")
	}
	if !config.Enabled {
		return "", fmt.Errorf("slack service is not enabled in the provided config")
	}
	if config.BotToken == "" {
		return "", fmt.Errorf("slack bot token is missing in the provided config")
	}
	if config.ChannelID == "" {
		return "", fmt.Errorf("slack channel ID is missing in the provided config")
	}

	// Create temporary client with provided config
	client := slack.New(config.BotToken)

	// Test by getting channel info
	_, err := client.GetConversationInfo(&slack.GetConversationInfoInput{
		ChannelID:         config.ChannelID,
		IncludeLocale:     false,
		IncludeNumMembers: false,
	})

	if err != nil {
		// Provide more helpful error messages based on Slack API errors
		var slackErr slack.SlackErrorResponse
		if errors.As(err, &slackErr) {
			switch slackErr.Err {
			case "invalid_auth":
				return "", fmt.Errorf("invalid bot token: please check that your bot token is correct and starts with 'xoxb-'. Make sure you copied the 'Bot User OAuth Token' from OAuth & Permissions, not the App-Level Token")
			case "channel_not_found":
				return "", fmt.Errorf("channel not found: please check that the channel ID is correct and the bot is a member of the channel")
			case "not_authed":
				return "", fmt.Errorf("authentication failed: the bot token is invalid or expired. Please regenerate it in Slack App settings")
			case "missing_scope":
				return "", fmt.Errorf("missing required scope: the bot needs 'channels:read' scope. Please add it in OAuth & Permissions and reinstall the app")
			default:
				return "", fmt.Errorf("Slack API error: %s", slackErr.Err)
			}
		}
		return "", fmt.Errorf("failed to connect to Slack: %w", err)
	}

	// Send a test message with webhook testing instructions
	testUniqueID := fmt.Sprintf("test-connection-%d", time.Now().Unix())

	testBlocks := formatSlackMessage(
		testUniqueID,
		"🧪 **Connection Test Message**",
		"This is a test message to verify Slack integration.\n\n**To test webhook:** Reply to this message in a thread. Your reply will be captured by the webhook and logged in the server.",
		nil, // No button options for test message
	)

	_, messageTS, err := client.PostMessage(config.ChannelID, slack.MsgOptionBlocks(testBlocks...))
	if err != nil {
		return "", fmt.Errorf("failed to send test message: %w", err)
	}

	// Create a feedback request so we can track replies
	// Note: We use a function variable to avoid import cycle with virtual-tools
	// This will be set by the caller if needed
	if createFeedbackRequest != nil {
		if err := createFeedbackRequest(testUniqueID, "Test connection message"); err != nil {
			log.Printf("[SLACK] Warning: Failed to create feedback request for test: %v", err)
		}
	}

	// Store the message mapping for Socket Mode testing (if we have database access)
	if s.db != nil {
		if err := s.StoreMessageMapping(ctx, testUniqueID, messageTS, config.ChannelID); err != nil {
			log.Printf("[SLACK] Warning: Failed to store test message mapping: %v", err)
		}
	}

	return testUniqueID, nil
}

// TestConnection tests if Slack connection is working
func (s *SlackService) TestConnection(ctx context.Context) error {
	// Reload config first to ensure we have the latest state
	if err := s.ReloadConfig(ctx); err != nil {
		return fmt.Errorf("failed to reload configuration: %w", err)
	}

	if !s.IsEnabled() {
		// Provide more detailed error message
		if s.config == nil {
			return fmt.Errorf("slack configuration not found, please save your configuration first")
		}
		if !s.config.Enabled {
			return fmt.Errorf("slack service is not enabled, please enable it in the configuration")
		}
		if s.config.BotToken == "" {
			return fmt.Errorf("slack bot token is missing, please configure it in the settings")
		}
		if s.config.ChannelID == "" {
			return fmt.Errorf("slack channel ID is missing, please configure it in the settings")
		}
		if s.client == nil {
			return fmt.Errorf("slack client not initialized, please check your bot token")
		}
		return fmt.Errorf("slack service is not enabled (enabled=%v, hasClient=%v, channelID=%q)",
			s.enabled, s.client != nil, s.channelID)
	}

	// Test by getting channel info
	_, err := s.client.GetConversationInfo(&slack.GetConversationInfoInput{
		ChannelID:         s.channelID,
		IncludeLocale:     false,
		IncludeNumMembers: false,
	})

	if err != nil {
		// Provide more helpful error messages based on Slack API errors
		var slackErr slack.SlackErrorResponse
		if errors.As(err, &slackErr) {
			switch slackErr.Err {
			case "invalid_auth":
				return fmt.Errorf("invalid bot token: please check that your bot token is correct and starts with 'xoxb-'. Make sure you copied the 'Bot User OAuth Token' from OAuth & Permissions, not the App-Level Token")
			case "channel_not_found":
				return fmt.Errorf("channel not found: please check that the channel ID is correct and the bot is a member of the channel")
			case "not_authed":
				return fmt.Errorf("authentication failed: the bot token is invalid or expired. Please regenerate it in Slack App settings")
			case "missing_scope":
				return fmt.Errorf("missing required scope: the bot needs 'channels:read' scope. Please add it in OAuth & Permissions and reinstall the app")
			default:
				return fmt.Errorf("Slack API error: %s", slackErr.Err)
			}
		}
		return fmt.Errorf("failed to connect to Slack: %w", err)
	}

	// Send a test message with @channel notification
	testBlocks := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", "<!channel>\n\n✅ Slack integration test successful!", false, false),
			nil, nil,
		),
	}

	_, _, err = s.client.PostMessage(s.channelID, slack.MsgOptionBlocks(testBlocks...))
	if err != nil {
		// Provide helpful error messages for message sending errors too
		var slackErr slack.SlackErrorResponse
		if errors.As(err, &slackErr) {
			switch slackErr.Err {
			case "invalid_auth":
				return fmt.Errorf("invalid bot token: please check that your bot token is correct and starts with 'xoxb-'. Make sure you copied the 'Bot User OAuth Token' from OAuth & Permissions")
			case "missing_scope":
				return fmt.Errorf("missing required scope: the bot needs 'chat:write' scope. Please add it in OAuth & Permissions and reinstall the app")
			case "channel_not_found":
				return fmt.Errorf("channel not found: please check that the channel ID is correct and the bot is a member of the channel")
			default:
				return fmt.Errorf("Slack API error: %s", slackErr.Err)
			}
		}
		return fmt.Errorf("failed to send test message: %w", err)
	}

	return nil
}

// SaveConfig saves Slack configuration to database (Socket Mode only)
func (s *SlackService) SaveConfig(ctx context.Context, config *SlackConfig) error {
	query := `INSERT INTO slack_feedback_config (id, enabled, bot_token, channel_id, app_token, updated_at)
	          VALUES ('slack_config', ?, ?, ?, ?, CURRENT_TIMESTAMP)
	          ON CONFLICT(id) DO UPDATE SET
	            enabled = excluded.enabled,
	            bot_token = excluded.bot_token,
	            channel_id = excluded.channel_id,
	            app_token = excluded.app_token,
	            updated_at = CURRENT_TIMESTAMP`

	_, err := s.db.ExecContext(ctx, query,
		config.Enabled,
		config.BotToken,
		config.ChannelID,
		config.AppToken,
	)

	if err != nil {
		log.Printf("[SLACK] Failed to save config: %v", err)
		return fmt.Errorf("failed to save Slack config: %w", err)
	}

	// Reload config
	if err := s.ReloadConfig(ctx); err != nil {
		log.Printf("[SLACK] Failed to reload config after save: %v", err)
		return fmt.Errorf("failed to reload config after save: %w", err)
	}

	return nil
}

// StartSocketMode starts Socket Mode WebSocket connection
func (s *SlackService) StartSocketMode(ctx context.Context) error {
	s.socketMux.Lock()
	defer s.socketMux.Unlock()

	if s.socketClient != nil {
		// Already running
		return nil
	}

	if s.config == nil || s.config.AppToken == "" {
		return fmt.Errorf("app token is required for Socket Mode")
	}

	if s.config.BotToken == "" {
		return fmt.Errorf("bot token is required for Socket Mode")
	}

	// Create Slack client with app-level token
	api := slack.New(
		s.config.BotToken,
		slack.OptionAppLevelToken(s.config.AppToken),
		slack.OptionDebug(true),
		slack.OptionLog(log.New(os.Stdout, "[SLACK_API] ", log.Lshortfile|log.LstdFlags)),
	)

	// Create Socket Mode client
	socketClient := socketmode.New(
		api,
		socketmode.OptionDebug(false), // Disable debug logging to reduce ping message noise
		socketmode.OptionLog(log.New(os.Stdout, "[SLACK_SOCKET] ", log.Lshortfile|log.LstdFlags)),
	)

	s.socketClient = socketClient
	s.socketCtx, s.socketCancel = context.WithCancel(context.Background())

	// Start Socket Mode connection with automatic reconnection
	// Run() blocks, so run in goroutine with reconnection logic
	go s.runSocketModeWithReconnect()

	// Give Run() a moment to start and establish connection
	time.Sleep(500 * time.Millisecond)

	// Start event handler in goroutine
	// Note: socketClient.Events channel is populated by Run()
	go s.handleSocketModeEvents()
	return nil
}

// runSocketModeWithReconnect runs Socket Mode with automatic reconnection
// This handles connection drops and automatically reconnects with exponential backoff
func (s *SlackService) runSocketModeWithReconnect() {
	maxRetries := 10 // Maximum reconnection attempts before using max delay
	baseDelay := 2 * time.Second
	maxDelay := 60 * time.Second
	retryCount := 0

	for {
		// Check if we should stop (context canceled or service disabled)
		select {
		case <-s.socketCtx.Done():
			return
		default:
		}

		// Verify socketClient still exists (service might have been stopped)
		s.socketMux.RLock()
		socketClient := s.socketClient
		shouldReconnect := socketClient != nil && s.config != nil && s.config.Enabled
		s.socketMux.RUnlock()

		if !shouldReconnect {
			return
		}

		if retryCount > 0 {
			// Calculate exponential backoff delay
			delay := baseDelay * time.Duration(1<<uint(retryCount-1)) // 2s, 4s, 8s, 16s, 32s, 60s (capped)
			if delay > maxDelay {
				delay = maxDelay
			}
			// Only log first few reconnection attempts
			if retryCount <= 3 {
				log.Printf("[SLACK_SOCKET] 🔄 Reconnecting in %v (attempt %d/%d)...", delay, retryCount, maxRetries)
			}

			select {
			case <-time.After(delay):
				// Continue with reconnection
			case <-s.socketCtx.Done():
				return
			}
		}

		// Run Socket Mode (this blocks until connection fails or is stopped)
		err := socketClient.Run()

		if err == nil {
			// Normal exit (shouldn't happen unless stopped)
			return
		}

		// Connection error occurred
		log.Printf("[SLACK_SOCKET] ❌ Socket Mode Run() error: %v", err)

		// Check if we should stop (context canceled)
		select {
		case <-s.socketCtx.Done():
			return
		default:
		}

		// Increment retry count
		retryCount++

		// If we've exceeded max retries, log and continue anyway (will retry with max delay)
		if retryCount > maxRetries {
			log.Printf("[SLACK_SOCKET] ⚠️  Exceeded %d reconnection attempts, will continue retrying with max delay", maxRetries)
			retryCount = maxRetries // Cap retry count to prevent overflow
		}
	}
}

// StopSocketMode stops Socket Mode WebSocket connection
func (s *SlackService) StopSocketMode() {
	s.socketMux.Lock()
	defer s.socketMux.Unlock()

	if s.socketCancel != nil {
		s.socketCancel()
		s.socketCancel = nil
	}

	if s.socketClient != nil {
		// Socket Mode client doesn't have explicit Close, but context cancel should stop it
		s.socketClient = nil
		log.Printf("[SLACK] Socket Mode stopped")
	}
}

// handleSocketModeEvents handles events from Socket Mode
func (s *SlackService) handleSocketModeEvents() {
	if s.socketClient == nil {
		log.Printf("[SLACK_SOCKET] ⚠️  handleSocketModeEvents: socketClient is nil")
		return
	}

	for evt := range s.socketClient.Events {
		switch evt.Type {
		case socketmode.EventTypeConnectionError:
			log.Printf("[SLACK] Socket Mode connection error: %v", evt.Data)
		case socketmode.EventTypeEventsAPI:
			s.handleSocketModeEvent(evt)
		case socketmode.EventTypeInteractive:
			s.handleSocketModeInteractive(evt)
		}
	}
}

// handleSocketModeEvent handles Events API events from Socket Mode
func (s *SlackService) handleSocketModeEvent(evt socketmode.Event) {
	eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
	if !ok {
		return
	}

	// Acknowledge the event
	if evt.Request != nil {
		s.socketClient.Ack(*evt.Request)
	}

	// Handle callback events (like message events)
	if eventsAPIEvent.Type == slackevents.CallbackEvent {
		innerEvent := eventsAPIEvent.InnerEvent
		switch ev := innerEvent.Data.(type) {
		case *slackevents.MessageEvent:
			s.handleSocketModeMessage(ev)
		}
	}
}

// handleSocketModeMessage handles message events from Socket Mode
func (s *SlackService) handleSocketModeMessage(ev *slackevents.MessageEvent) {
	// Only process thread replies (not bot messages)
	if ev.BotID != "" || ev.ThreadTimeStamp == "" || ev.ThreadTimeStamp == ev.TimeStamp {
		return
	}

	// Get unique ID from thread
	uniqueID, err := s.GetUniqueIDFromThread(
		context.Background(),
		ev.ThreadTimeStamp,
		ev.Channel,
	)
	if err != nil {
		log.Printf("[SLACK_SOCKET] ⚠️  Failed to get unique_id for thread %s: %v", ev.ThreadTimeStamp, err)
		return
	}

	// Submit feedback through notification manager (which updates the feedback store)
	// This ensures all connectors go through the same path
	notificationManager := GetNotificationManager()
	if notificationManager == nil {
		log.Printf("[SLACK_SOCKET] ❌ Notification manager not available, cannot submit response")
		return
	}

	if err := notificationManager.ReceiveNotification(uniqueID, ev.Text, "slack"); err != nil {
		log.Printf("[SLACK_SOCKET] ❌ Failed to submit feedback via notification manager: %v", err)
		return
	}
}

// handleSocketModeInteractive handles interactive events (button clicks) from Socket Mode
func (s *SlackService) handleSocketModeInteractive(evt socketmode.Event) {
	// Get the interaction callback
	callback, ok := evt.Data.(slack.InteractionCallback)
	if !ok {
		if evt.Request != nil {
			s.socketClient.Ack(*evt.Request)
		}
		return
	}

	// Acknowledge the event immediately
	if evt.Request != nil {
		s.socketClient.Ack(*evt.Request)
	}

	// Only handle button actions
	if callback.Type != slack.InteractionTypeBlockActions {
		return
	}

	// Process button clicks
	for _, action := range callback.ActionCallback.BlockActions {

		// Extract uniqueID from action_id
		// Format: "feedback_yes_<uniqueID>", "feedback_no_<uniqueID>", or "feedback_option_<index>_<uniqueID>"
		var uniqueID string
		var response string

		actionID := action.ActionID
		if strings.HasPrefix(actionID, "feedback_yes_") {
			uniqueID = strings.TrimPrefix(actionID, "feedback_yes_")
			response = action.Text.Text // Use button label (e.g., "Approve")
		} else if strings.HasPrefix(actionID, "feedback_no_") {
			uniqueID = strings.TrimPrefix(actionID, "feedback_no_")
			response = action.Text.Text // Use button label (e.g., "Reject")
		} else if strings.HasPrefix(actionID, "feedback_option_") {
			// Format: "feedback_option_<index>_<uniqueID>"
			parts := strings.Split(actionID, "_")
			if len(parts) >= 4 {
				uniqueID = strings.Join(parts[3:], "_") // uniqueID may contain underscores
				response = action.Text.Text             // Use button label (the option text)
			} else {
				log.Printf("[SLACK_SOCKET] ⚠️  Invalid option action ID format: %s", actionID)
				continue
			}
		} else {
			log.Printf("[SLACK_SOCKET] ⚠️  Unknown action ID format: %s", actionID)
			continue
		}

		if uniqueID == "" {
			log.Printf("[SLACK_SOCKET] ⚠️  Could not extract uniqueID from action ID: %s", actionID)
			continue
		}

		// Submit response via notification manager
		notificationManager := GetNotificationManager()
		if notificationManager == nil {
			log.Printf("[SLACK_SOCKET] ❌ Notification manager not available, cannot submit response")
			continue
		}

		if err := notificationManager.ReceiveNotification(uniqueID, response, "slack"); err != nil {
			log.Printf("[SLACK_SOCKET] ❌ Failed to submit button response: %v", err)
			continue
		}

		// Update the message to show the button was clicked (optional - can disable buttons)
		// For now, we'll just log it - Slack will automatically disable the button after click
	}
}

// GetConfig returns current Slack configuration (with masked tokens)
func (s *SlackService) GetConfig() *SlackConfig {
	if s.config == nil {
		return &SlackConfig{Enabled: false}
	}

	// Return config with masked tokens
	config := *s.config
	if len(config.BotToken) > 4 {
		config.BotToken = "xoxb-..." + config.BotToken[len(config.BotToken)-4:]
	}
	if len(config.AppToken) > 4 {
		config.AppToken = "xapp-..." + config.AppToken[len(config.AppToken)-4:]
	}

	return &config
}
