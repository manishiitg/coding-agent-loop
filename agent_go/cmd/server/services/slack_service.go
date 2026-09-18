package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workspace"
)

// Slack state is persisted as two small JSON files under <workspace-docs>/config/:
// slack-config.json (bot_token/app_token/enabled) and
// slack-feedback-messages.json (uniqueID → Slack message ts/channel mapping).
// Legacy _system file locations are still read and migrated forward on load.
// Both are loaded into memory on first access and mutated under a
// package-level mutex.

var (
	slackFSMu           sync.Mutex
	slackMessagesCache  map[string]*slackFeedbackMessageRecord
	slackMessagesLoaded bool
)

func slackConfigFilePath() string {
	return "config/slack-config.json"
}

func slackMessagesFilePath() string {
	return "config/slack-feedback-messages.json"
}

func legacySlackConfigFilePath() string {
	return "_system/slack_config.json"
}

func legacySlackMessagesFilePath() string {
	return "_system/slack_feedback_messages.json"
}

// slackFeedbackMessageRecord stores the mapping for a single slack feedback request.
type slackFeedbackMessageRecord struct {
	UniqueID       string    `json:"unique_id"`
	SlackMessageTS string    `json:"slack_message_ts"`
	ChannelID      string    `json:"slack_channel_id"`
	CreatedAt      time.Time `json:"created_at"`
}

func slackFeedbackMessageKey(messageTS, channelID string) string {
	return messageTS + "|" + channelID
}

func loadSlackConfigFromDisk() (*SlackConfig, error) {
	ctx := context.Background()
	data, exists, err := readWorkspaceFile(ctx, workspaceAPIURL(), slackConfigFilePath())
	if err != nil {
		return nil, err
	}
	if !exists {
		legacyData, legacyExists, legacyErr := readWorkspaceFile(ctx, workspaceAPIURL(), legacySlackConfigFilePath())
		if legacyErr != nil {
			return nil, legacyErr
		}
		if !legacyExists {
			return &SlackConfig{Enabled: false}, nil
		}
		var legacyCfg SlackConfig
		if err := json.Unmarshal([]byte(legacyData), &legacyCfg); err != nil {
			return nil, fmt.Errorf("failed to parse legacy slack_config.json: %w", err)
		}
		decoded, err := decryptSlackConfig(&legacyCfg)
		if err != nil {
			return nil, err
		}
		if err := saveSlackConfigToDisk(decoded); err != nil {
			return nil, err
		}
		return decoded, nil
	}
	var cfg SlackConfig
	if err := json.Unmarshal([]byte(data), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse slack-config.json: %w", err)
	}
	legacyCredentials := cfg.BotToken != "" && !strings.HasPrefix(cfg.BotToken, "encrypted:v1:") || cfg.AppToken != "" && !strings.HasPrefix(cfg.AppToken, "encrypted:v1:")
	decoded, err := decryptSlackConfig(&cfg)
	if err != nil {
		return nil, err
	}
	if legacyCredentials && slackCredentialEncrypt != nil {
		if err := saveSlackConfigToDisk(decoded); err != nil {
			return nil, err
		}
	}
	return decoded, nil
}

func saveSlackConfigToDisk(cfg *SlackConfig) error {
	stored := *cfg
	var err error
	stored.BotToken, err = encodeSlackCredential(cfg.BotToken)
	if err != nil {
		return err
	}
	stored.AppToken, err = encodeSlackCredential(cfg.AppToken)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}
	return writeWorkspaceFile(context.Background(), workspaceAPIURL(), slackConfigFilePath(), string(data))
}

// getSlackFeedbackMessagesLocked returns the in-memory feedback message map,
// lazily loading it from disk on first access. Caller must hold slackFSMu.
func getSlackFeedbackMessagesLocked() (map[string]*slackFeedbackMessageRecord, error) {
	if slackMessagesLoaded {
		return slackMessagesCache, nil
	}
	ctx := context.Background()
	data, exists, err := readWorkspaceFile(ctx, workspaceAPIURL(), slackMessagesFilePath())
	if err != nil {
		return nil, err
	}
	slackMessagesCache = make(map[string]*slackFeedbackMessageRecord)
	if !exists {
		legacyData, legacyExists, legacyErr := readWorkspaceFile(ctx, workspaceAPIURL(), legacySlackMessagesFilePath())
		if legacyErr != nil {
			return nil, legacyErr
		}
		if legacyExists {
			data = legacyData
		}
	}
	if len(data) > 0 {
		if err := json.Unmarshal([]byte(data), &slackMessagesCache); err != nil {
			slackMessagesCache = nil
			return nil, fmt.Errorf("failed to parse slack-feedback-messages.json: %w", err)
		}
		if saveErr := saveSlackFeedbackMessagesLocked(); saveErr != nil {
			slackMessagesCache = nil
			return nil, saveErr
		}
	}
	slackMessagesLoaded = true
	return slackMessagesCache, nil
}

// saveSlackFeedbackMessagesLocked persists the in-memory feedback message map.
// Caller must hold slackFSMu and must have called getSlackFeedbackMessagesLocked first.
func saveSlackFeedbackMessagesLocked() error {
	data, err := json.MarshalIndent(slackMessagesCache, "", "  ")
	if err != nil {
		return err
	}
	return writeWorkspaceFile(context.Background(), workspaceAPIURL(), slackMessagesFilePath(), string(data))
}

// FeedbackResponseFunc is defined in notification_manager.go (same package)

// SlackConfig represents Slack configuration (Socket Mode only)
type SlackConfig struct {
	Enabled  bool   `json:"enabled"`
	BotToken string `json:"bot_token"` // Bot User OAuth Token (xoxb-...)
	AppToken string `json:"app_token"` // App-level token (xapp-...) for Socket Mode
}

// SlackService handles Slack integration for human feedback. It implements
// the NotificationConnector interface and persists its configuration and the
// feedback-message mapping via the helpers above.
type SlackService struct {
	triggerHandler SlackTriggerHandler
	client         *slack.Client
	socketClient   *socketmode.Client // Socket Mode client for WebSocket connection
	config         *SlackConfig
	enabled        bool
	useSocketMode  bool
	socketCtx      context.Context
	socketCancel   context.CancelFunc
	socketMux      sync.RWMutex

	// Bot connector handlers (set by BotConversationManager)
	messageHandler     BotMessageHandler
	interactionHandler BotInteractionHandler
	botID              string
	botUserID          string // Bot's own Slack user ID (for stripping @mentions)

	// Message deduplication — prevents processing the same Slack event twice
	seenMessages   map[string]time.Time
	seenMessagesMu sync.Mutex
}

// Name returns the name of this connector
func (s *SlackService) Name() string {
	return "slack"
}

// SendNotification implements NotificationConnector interface
// This is a wrapper around SendFeedbackNotification for the interface
func (s *SlackService) SendNotification(ctx context.Context, uniqueID string, message string, contextMsg string, buttonOptions *ButtonOptions, dest *NotificationDestination) (string, error) {
	return s.SendFeedbackNotification(ctx, uniqueID, message, contextMsg, buttonOptions, dest)
}

// SendUserNotification sends a non-blocking informational message through
// Slack. Unlike SendNotification, this does not create request metadata or
// buttons; replies remain normal bot-thread messages.
func (s *SlackService) SendUserNotification(ctx context.Context, message string, contextMsg string, dest *NotificationDestination) (string, error) {
	if !s.enabled || s.client == nil {
		return "", fmt.Errorf("slack service is not enabled")
	}

	channelID, threadTS := s.pickUserNotificationDestination(dest)
	if channelID == "" {
		return "", nil
	}

	body := strings.TrimSpace(message)
	if body == "" {
		return "", nil
	}
	if contextMsg = strings.TrimSpace(contextMsg); contextMsg != "" {
		body += "\n\n" + contextMsg
	}

	blocks := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", convertMarkdownToSlackMrkdwn(body), false, false),
			nil,
			nil,
		),
	}
	postOpts := []slack.MsgOption{slack.MsgOptionBlocks(blocks...)}
	if threadTS != "" {
		postOpts = append(postOpts, slack.MsgOptionTS(threadTS))
	}

	logBotOutboundMessage("slack", ThreadID{Platform: "slack", ChannelID: channelID, ThreadTS: threadTS}, "user_notification", body, 1, len(blocks))
	_, timestamp, err := s.client.PostMessageContext(ctx, channelID, postOpts...)
	if err != nil {
		return "", fmt.Errorf("failed to post Slack user notification: %w", err)
	}
	return timestamp, nil
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

// InitSlackService initializes the Slack service from the filesystem-backed
// config file. Called on server startup; Socket Mode is started automatically
// when the config is valid and enabled.
func InitSlackService() (*SlackService, error) {
	service := &SlackService{
		seenMessages: make(map[string]time.Time),
	}

	// Load config first
	if err := service.loadConfig(context.Background()); err != nil {
		SetSlackService(service)
		return service, err
	}
	if service.config != nil {
	}

	// ReloadConfig will start Socket Mode if config is enabled and valid,
	// ensuring Socket Mode connects on server startup.
	if err := service.ReloadConfig(context.Background()); err != nil {
		log.Printf("[SLACK] Failed to reload config on initialization: %v", err)
	}

	SetSlackService(service)
	return service, nil
}

// loadConfig loads Slack configuration from disk.
func (s *SlackService) loadConfig(ctx context.Context) error {
	slackFSMu.Lock()
	defer slackFSMu.Unlock()
	cfg, err := loadSlackConfigFromDisk()
	if err != nil {
		return err
	}
	s.config = cfg
	if cfg != nil {
	}
	return nil
}

// ReloadConfig reloads configuration from disk and (re)starts Socket Mode.
func (s *SlackService) ReloadConfig(ctx context.Context) error {
	// Stop existing Socket Mode connection if running
	s.StopSocketMode()

	if err := s.loadConfig(ctx); err != nil {
		return err
	}

	// Socket Mode connects the app; destinations belong to channel routes.
	if s.config != nil && s.config.Enabled && s.config.BotToken != "" && s.config.AppToken != "" {
		s.client = slack.New(s.config.BotToken)
		s.enabled = true
		s.useSocketMode = true

		// Start Socket Mode connection
		if err := s.StartSocketMode(ctx); err != nil {
			log.Printf("[SLACK] Failed to start Socket Mode: %v", err)
			s.enabled = false
			s.useSocketMode = false
		} else {
			log.Printf("[SLACK] Service enabled with Socket Mode")
		}
	} else {
		s.client = nil
		s.enabled = false
		s.useSocketMode = false
		if s.config != nil {
			log.Printf("[SLACK] Service disabled: enabled=%v, hasBotToken=%v, hasAppToken=%v",
				s.config.Enabled, s.config.BotToken != "", s.config.AppToken != "")
		}
	}

	return nil
}

// IsEnabled checks if Slack notifications are enabled
func (s *SlackService) IsEnabled() bool {
	return s.enabled && s.client != nil
}

// pickDestination resolves where this Slack feedback notification should land.
// Precedence:
//  1. dest.Slack (explicit per-request hint)
//  2. per-user preference (looked up via dest.UserID)
//
// Returns ("", "") if no explicit destination or user preference exists —
// the caller should treat that as "skip silently".
func (s *SlackService) pickDestination(dest *NotificationDestination) (channelID, threadTS string) {
	if dest != nil && dest.Slack != nil && dest.Slack.ChannelID != "" {
		return dest.Slack.ChannelID, dest.Slack.ThreadTS
	}
	if dest != nil && dest.UserID != "" {
		if pref := getNotificationPreferences(dest.UserID); pref != nil && pref.SlackChannelID != "" && !pref.SlackDisabled {
			return pref.SlackChannelID, ""
		}
	}
	return "", ""
}

func (s *SlackService) pickUserNotificationDestination(dest *NotificationDestination) (channelID, threadTS string) {
	if dest != nil && dest.Slack != nil && dest.Slack.ChannelID != "" {
		return dest.Slack.ChannelID, dest.Slack.ThreadTS
	}
	if dest != nil && dest.UserID != "" {
		if pref := getNotificationPreferences(dest.UserID); pref != nil && pref.SlackChannelID != "" && !pref.SlackDisabled {
			return pref.SlackChannelID, ""
		}
	}
	return "", ""
}

// SendFeedbackNotification sends a feedback request to Slack
// Returns Slack message timestamp for tracking
func (s *SlackService) SendFeedbackNotification(
	ctx context.Context,
	uniqueID string,
	message string,
	contextMsg string,
	buttonOptions *ButtonOptions,
	dest *NotificationDestination,
) (string, error) {
	if !s.enabled || s.client == nil {
		return "", fmt.Errorf("slack service is not enabled")
	}

	channelID, threadTS := s.pickDestination(dest)
	if channelID == "" {
		// No hint, no preference, no default — skip silently.
		return "", nil
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

	postOpts := []slack.MsgOption{slack.MsgOptionBlocks(blocks...)}
	if threadTS != "" {
		postOpts = append(postOpts, slack.MsgOptionTS(threadTS))
	}

	logBotOutboundMessage("slack", ThreadID{Platform: "slack", ChannelID: channelID, ThreadTS: threadTS}, "notification", message, 1, len(blocks))

	postedChannelID, timestamp, err := s.client.PostMessage(channelID, postOpts...)
	if err != nil {
		return "", fmt.Errorf("failed to post Slack message: %w", err)
	}

	// Store message mapping
	if err := s.StoreMessageMapping(ctx, uniqueID, timestamp, postedChannelID); err != nil {
		log.Printf("[SLACK] Failed to store message mapping: %v", err)
		// Don't fail the whole operation if mapping storage fails
	}

	return timestamp, nil
}

// convertMarkdownToSlackMrkdwn converts common markdown patterns to Slack's mrkdwn format.
// Slack mrkdwn supports: *bold*, _italic_, ~strike~, `code`, ```code blocks```, > quotes.
// It does NOT natively support: headers, tables, images, task list checkboxes.
// We approximate those so output looks reasonable.
//
// Order matters here: code is protected via placeholders first so later regexes
// can't mangle its contents, then structural (tables, headers) runs before
// inline (bold, italic, strike).
func convertMarkdownToSlackMrkdwn(text string) string {
	if text == "" {
		return text
	}
	result := text

	// Step 1: protect fenced code blocks and inline code from every other regex.
	var codeStore []string
	saveCode := func(s string) string {
		codeStore = append(codeStore, s)
		return fmt.Sprintf("\x00C%d\x00", len(codeStore)-1)
	}
	codeBlockRegex := regexp.MustCompile("(?s)```(?:[a-zA-Z0-9_+-]+)?\\n?(.*?)```")
	result = codeBlockRegex.ReplaceAllStringFunc(result, func(m string) string {
		parts := codeBlockRegex.FindStringSubmatch(m)
		if len(parts) == 2 {
			return saveCode("```\n" + strings.TrimSpace(parts[1]) + "\n```")
		}
		return saveCode(m)
	})
	inlineCodeRegex := regexp.MustCompile("`([^`\\n]+)`")
	result = inlineCodeRegex.ReplaceAllStringFunc(result, saveCode)

	// Step 2: convert tables to fenced code blocks so alignment is preserved.
	// A table is a header row |…|, separator |---|---|, then one or more body rows.
	result = convertMarkdownTables(result, saveCode)

	// Step 3: headers → bold (Slack has no headers).
	headerRegex := regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)
	result = headerRegex.ReplaceAllString(result, "*$1*")

	// Step 4: images → link. Must run BEFORE the link regex so ![alt](url) isn't
	// left with a stray leading "!".
	imgRegex := regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
	result = imgRegex.ReplaceAllStringFunc(result, func(m string) string {
		parts := imgRegex.FindStringSubmatch(m)
		alt := parts[1]
		if alt == "" {
			alt = "image"
		}
		return fmt.Sprintf("<%s|%s>", parts[2], alt)
	})

	// Step 5: links [text](url) → <url|text>.
	linkRegex := regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	result = linkRegex.ReplaceAllString(result, "<$2|$1>")

	// Step 6: bold **text** → *text*. Non-greedy to keep each pair tight.
	boldRegex := regexp.MustCompile(`\*\*(.+?)\*\*`)
	result = boldRegex.ReplaceAllString(result, "*$1*")

	// Step 7: strikethrough ~~text~~ → ~text~.
	strikeRegex := regexp.MustCompile(`~~(.+?)~~`)
	result = strikeRegex.ReplaceAllString(result, "~$1~")

	// Step 8: task list checkboxes. "- [ ] item" → "☐ item", "- [x] item" → "☑ item".
	// Run before the generic bullet regex, which would otherwise swallow the "[ ]".
	taskOpenRegex := regexp.MustCompile(`(?m)^(\s*)[*\-]\s+\[\s\]\s+`)
	result = taskOpenRegex.ReplaceAllString(result, "${1}☐ ")
	taskDoneRegex := regexp.MustCompile(`(?m)^(\s*)[*\-]\s+\[[xX]\]\s+`)
	result = taskDoneRegex.ReplaceAllString(result, "${1}☑ ")

	// Step 9: bullet lists "* item" / "- item" → "• item".
	bulletRegex := regexp.MustCompile(`(?m)^(\s*)[*\-]\s+`)
	result = bulletRegex.ReplaceAllString(result, "${1}• ")

	// Step 10: horizontal rule to a visible em-dash run (Slack renders "---" as text).
	result = regexp.MustCompile(`(?m)^\s*(-{3,}|_{3,}|\*{3,})\s*$`).ReplaceAllString(result, "————————")

	// Step 11: restore code.
	for i, c := range codeStore {
		result = strings.ReplaceAll(result, fmt.Sprintf("\x00C%d\x00", i), c)
	}
	return result
}

// convertMarkdownTables wraps contiguous markdown-table regions in a ``` fence
// so Slack renders them in a monospace block with preserved column alignment.
// saveCode is called to protect the resulting block from subsequent regexes.
func convertMarkdownTables(text string, saveCode func(string) string) string {
	lines := strings.Split(text, "\n")
	isTableRow := func(s string) bool {
		s = strings.TrimSpace(s)
		return strings.HasPrefix(s, "|") && strings.HasSuffix(s, "|") && strings.Count(s, "|") >= 2
	}
	isSeparator := func(s string) bool {
		s = strings.TrimSpace(s)
		if !isTableRow(s) {
			return false
		}
		inner := strings.Trim(s, "|")
		for _, cell := range strings.Split(inner, "|") {
			c := strings.TrimSpace(cell)
			c = strings.Trim(c, ":")
			if c == "" || strings.Trim(c, "-") != "" {
				return false
			}
		}
		return true
	}

	var out []string
	for i := 0; i < len(lines); i++ {
		if isTableRow(lines[i]) && i+1 < len(lines) && isSeparator(lines[i+1]) {
			j := i + 2
			for j < len(lines) && isTableRow(lines[j]) {
				j++
			}
			block := "```\n" + strings.Join(lines[i:j], "\n") + "\n```"
			out = append(out, saveCode(block))
			i = j - 1
			continue
		}
		out = append(out, lines[i])
	}
	return strings.Join(out, "\n")
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
	slackFSMu.Lock()
	defer slackFSMu.Unlock()
	records, err := getSlackFeedbackMessagesLocked()
	if err != nil {
		return fmt.Errorf("failed to load slack feedback messages: %w", err)
	}
	key := slackFeedbackMessageKey(messageTS, channelID)
	if _, exists := records[key]; exists {
		return nil
	}
	records[key] = &slackFeedbackMessageRecord{
		UniqueID:       uniqueID,
		SlackMessageTS: messageTS,
		ChannelID:      channelID,
		CreatedAt:      time.Now().UTC(),
	}
	return saveSlackFeedbackMessagesLocked()
}

// GetUniqueIDFromThread retrieves unique_id from a Slack thread reply
func (s *SlackService) GetUniqueIDFromThread(
	ctx context.Context,
	threadTS string,
	channelID string,
) (string, error) {
	// First, try to get from the in-memory mapping (thread_ts is the parent message timestamp).
	slackFSMu.Lock()
	records, err := getSlackFeedbackMessagesLocked()
	var cached string
	if err == nil {
		if rec, ok := records[slackFeedbackMessageKey(threadTS, channelID)]; ok && rec != nil {
			cached = rec.UniqueID
		}
	}
	slackFSMu.Unlock()
	if cached != "" {
		return cached, nil
	}

	// If not found locally, try to get from Slack via the conversations.replies API
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

// testAppToken validates a Slack app-level token by calling apps.connections.open.
// This is the same endpoint Socket Mode uses to establish a WebSocket connection.
func testAppToken(ctx context.Context, appToken string) error {
	if !strings.HasPrefix(appToken, "xapp-") {
		return fmt.Errorf("App Token invalid: must start with 'xapp-'. Go to Slack App settings → Basic Information → App-Level Tokens to get the correct token")
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://slack.com/api/apps.connections.open", nil)
	if err != nil {
		return fmt.Errorf("App Token test failed: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+appToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("App Token test failed (network error): %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("App Token test failed: unexpected response")
	}

	if !result.OK {
		switch result.Error {
		case "invalid_auth":
			return fmt.Errorf("App Token invalid: the token is incorrect or expired. Go to Slack App settings → Basic Information → App-Level Tokens to regenerate it")
		case "not_allowed_token_type":
			return fmt.Errorf("App Token wrong type: you may have pasted the Bot Token here. The App-Level Token starts with 'xapp-' and is found under Basic Information → App-Level Tokens")
		default:
			return fmt.Errorf("App Token error: %s", result.Error)
		}
	}

	return nil
}

func (s *SlackService) TestConnectionWithConfig(ctx context.Context, config *SlackConfig) (string, error) {
	result := s.DiagnoseConnectionWithConfig(ctx, config)
	if !result.Success {
		messages := []string{result.Message}
		for _, check := range result.Checks {
			if check.Status == "missing" || check.Status == "failed" {
				messages = append(messages, check.Name+": "+check.Message)
			}
		}
		return "", fmt.Errorf("%s", strings.Join(messages, "; "))
	}
	return "", nil
}

// TestConnection tests if Slack connection is working
func (s *SlackService) TestConnection(ctx context.Context) error {
	if err := s.ReloadConfig(ctx); err != nil {
		return fmt.Errorf("failed to reload Slack configuration: %w", err)
	}
	_, err := s.TestConnectionWithConfig(ctx, s.config)
	return err
}

// isMaskedToken returns true when the string is a display-only placeholder
// produced by GetConfig (e.g. "xoxb-...ABCD"). These must never be persisted
// — when the UI round-trips them back on an unrelated Save, we want to keep
// whatever real token is already stored instead of overwriting with garbage.
func isMaskedToken(s string) bool {
	return strings.Contains(s, "...")
}

// SaveConfig persists Slack configuration to the filesystem-backed config file
// and reloads the service so Socket Mode restarts with the new settings.
// Incoming masked tokens are treated as "no change" and replaced by the
// currently-stored real token before writing to disk.
func (s *SlackService) SaveConfig(ctx context.Context, config *SlackConfig) error {
	if config == nil {
		return fmt.Errorf("slack config is required")
	}
	slackFSMu.Lock()
	if s.config != nil {
		if isMaskedToken(config.BotToken) {
			config.BotToken = s.config.BotToken
		}
		if isMaskedToken(config.AppToken) {
			config.AppToken = s.config.AppToken
		}
	}
	if err := saveSlackConfigToDisk(config); err != nil {
		slackFSMu.Unlock()
		log.Printf("[SLACK] Failed to save config: %v", err)
		return fmt.Errorf("failed to save Slack config: %w", err)
	}
	slackFSMu.Unlock()

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
		socketmode.OptionDebug(true), // Enable debug to diagnose connection issues
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
		log.Printf("[SLACK_SOCKET] 🚀 Starting Socket Mode Run()...")
		err := socketClient.Run()

		if err == nil {
			// Normal exit (shouldn't happen unless stopped)
			log.Printf("[SLACK_SOCKET] ⚠️  Socket Mode Run() returned nil (clean exit)")
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
		default:
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
	} else {
	}

	// Handle callback events (like message events)
	if eventsAPIEvent.Type == slackevents.CallbackEvent {
		innerEvent := eventsAPIEvent.InnerEvent
		switch ev := innerEvent.Data.(type) {
		case *slackevents.MessageEvent:
			s.handleSocketModeMessage(ev)
		case *slackevents.AppMentionEvent:
			s.handleAppMentionEvent(ev)
		default:
		}
	} else {
	}
}

// Both Slack event surfaces must select the saved trigger before generic chat,
// regardless of which event arrives first for a human app mention.
func (s *SlackService) handleConfiguredSlackTrigger(ev *slackevents.MessageEvent) bool {
	if s.botID != "" && ev.BotID == s.botID {
		return true
	}
	route := s.resolveSlackChannelWorkflow(ev.Channel)
	if route == nil || s.triggerHandler == nil || !SlackTriggerMatches(route.Trigger, ev, s.botUserID) {
		return false
	}
	if ev.BotID == "" && len(route.BlockedEmails) > 0 && !SlackRouteAllowsEmail(*route, s.resolveUserEmail(ev.User)) {
		return true
	}
	if !s.isDuplicateMessage(ev.Channel + ":" + ev.TimeStamp) {
		if err := s.triggerHandler(context.Background(), ev.Channel, ev); err != nil {
			log.Printf("[SLACK_TRIGGER] %v", err)
		}
	}
	return true
}

// handleSocketModeMessage handles message events from Socket Mode
func (s *SlackService) handleSocketModeMessage(ev *slackevents.MessageEvent) {
	if s.handleConfiguredSlackTrigger(ev) {
		return
	}

	// Only process thread replies (not bot messages)
	if ev.BotID != "" || ev.ThreadTimeStamp == "" || ev.ThreadTimeStamp == ev.TimeStamp {
		return
	}

	// Skip messages that contain an @mention of the bot — those are handled by
	// handleAppMentionEvent. Must run BEFORE the dedup check: if we cache the
	// ts here and then return, the app_mention event (which arrives with the
	// same ts) will hit the cache and be dropped, so the message goes nowhere.
	if s.botUserID != "" && strings.Contains(ev.Text, fmt.Sprintf("<@%s>", s.botUserID)) {
		s.handleSlackBotMessage(ev.User, ev.Channel, ev.ThreadTimeStamp, ev.TimeStamp, s.stripMention(ev.Text), ev.Message, true, true)
		return
	}

	s.handleSlackBotMessage(ev.User, ev.Channel, ev.ThreadTimeStamp, ev.TimeStamp, ev.Text, ev.Message, true, false)
}

func (s *SlackService) handleSlackBotMessage(userID, channelID, threadTS, messageTS, text string, msg *slack.Msg, isThreadReply, isMention bool) {
	if s.messageHandler == nil {
		return
	}

	if s.isDuplicateMessage(channelID + ":" + messageTS) {
		return
	}

	userEmail := s.resolveUserEmail(userID)
	text, presetRoute, handled := s.routeSlackWorkflowMessage(context.Background(), userID, userEmail, channelID, threadTS, text, isThreadReply)
	if handled {
		return
	}
	text = s.appendSlackFileContext(context.Background(), text, msg, channelID, userEmail, presetRoute)

	s.messageHandler(BotIncomingMessage{
		Platform:       "slack",
		UserID:         userID,
		UserName:       userID,
		UserEmail:      userEmail,
		ChannelID:      channelID,
		ThreadTS:       threadTS,
		Text:           text,
		MessageTS:      messageTS,
		Timestamp:      time.Now(),
		IsThreadReply:  isThreadReply,
		IsMention:      isMention,
		PresetWorkflow: presetRoute,
	})

	if isMention {
		return
	}

	// Get unique ID from thread (for feedback responses)
	uniqueID, err := s.GetUniqueIDFromThread(
		context.Background(),
		threadTS,
		channelID,
	)
	if err != nil {
		// Not a feedback thread — this is normal for bot session threads
		return
	}

	// Submit feedback through notification manager (which updates the feedback store)
	notificationManager := GetNotificationManager()
	if notificationManager == nil {
		log.Printf("[SLACK_SOCKET] Notification manager not available, cannot submit response")
		return
	}

	if err := notificationManager.ReceiveNotification(uniqueID, text, "slack"); err != nil {
		log.Printf("[SLACK_SOCKET] Failed to submit feedback via notification manager: %v", err)
		return
	}
}

func (s *SlackService) routeSlackWorkflowMessage(_ context.Context, userID, userEmail, channelID, threadTS, text string, isThreadReply bool) (string, *ChannelRoute, bool) {
	route := s.resolveSlackChannelWorkflow(channelID)
	if route == nil {
		return text, nil, false
	}
	if !SlackRouteAllowsEmail(*route, userEmail) {
		return text, route, true
	}
	return text, route, false
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

	if route := s.resolveSlackChannelWorkflow(callback.Channel.ID); route != nil && len(route.BlockedEmails) > 0 && !SlackRouteAllowsEmail(*route, s.resolveUserEmail(callback.User.ID)) {
		return
	}

	// Process button clicks
	for _, action := range callback.ActionCallback.BlockActions {

		// Extract uniqueID from action_id
		// Format: "feedback_yes_<uniqueID>", "feedback_no_<uniqueID>", or "feedback_option_<index>_<uniqueID>"
		var uniqueID string
		var response string

		actionID := action.ActionID

		// Handle bot connector actions (confirm/cancel)
		if strings.HasPrefix(actionID, "bot_confirm_") || strings.HasPrefix(actionID, "bot_cancel_") {
			if s.interactionHandler != nil {
				value := "confirm"
				if strings.HasPrefix(actionID, "bot_cancel_") {
					value = "cancel"
				}
				threadTS := callback.Message.ThreadTimestamp
				if threadTS == "" {
					threadTS = callback.Message.Timestamp
				}
				s.interactionHandler("slack", callback.Channel.ID, threadTS, actionID, value, callback.User.ID)
			} else {
			}
			continue
		}

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
				log.Printf("[SLACK_SOCKET] Invalid option action ID format: %s", actionID)
				continue
			}
		} else {
			log.Printf("[SLACK_SOCKET] Unknown action ID format: %s", actionID)
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

// --- BotConnector interface implementation ---

func (s *SlackService) Capabilities() ChannelCapabilities {
	return ChannelCapabilities{ChannelHistory: true, Threads: true, MessageEdits: true, StreamingReplies: true, Reactions: true, MessageDeletion: true, ProgressUpdates: true, WorkflowProgress: true}
}

// StartListening starts the bot listener (Socket Mode is already started separately)
func (s *SlackService) StartListening(ctx context.Context) error {
	// Socket Mode is already started via StartSocketMode
	// Resolve bot user ID for mention stripping
	if s.client != nil {
		authResp, err := s.client.AuthTest()
		if err == nil {
			s.botUserID = authResp.UserID
			s.botID = authResp.BotID
			log.Printf("[SLACK_BOT] Bot user ID resolved: %s", s.botUserID)
		}
	}
	return nil
}

// StopListening stops the bot listener
func (s *SlackService) StopListening() {
	s.StopSocketMode()
}

// SetMessageHandler sets the callback for incoming bot messages
func (s *SlackService) SetMessageHandler(handler BotMessageHandler) {
	s.messageHandler = handler
}

// SetInteractionHandler sets the callback for button interactions
func (s *SlackService) SetInteractionHandler(handler BotInteractionHandler) {
	s.interactionHandler = handler
}

// GetFormatter returns the Slack message formatter
func (s *SlackService) GetFormatter() MessageFormatter {
	return &SlackFormatter{}
}

// AddReaction adds a reaction emoji to a Slack message. Used by the session
// manager to layer an "hourglass" on top of the initial "eyes" ack when a
// session runs longer than a short threshold. "already_reacted" is non-fatal.
func (s *SlackService) AddReaction(ctx context.Context, channelID, messageTS, emoji string) error {
	if s.client == nil || channelID == "" || messageTS == "" || emoji == "" {
		return nil
	}
	if err := s.client.AddReaction(emoji, slack.ItemRef{Channel: channelID, Timestamp: messageTS}); err != nil {
		if strings.Contains(err.Error(), "already_reacted") {
			return nil
		}
		return err
	}
	return nil
}

// RemoveReaction removes a reaction emoji from a Slack message. Called when
// a session completes, so the ack reactions clear once the bot has actually
// replied. Missing-reaction errors are treated as non-fatal.
func (s *SlackService) RemoveReaction(ctx context.Context, channelID, messageTS, emoji string) error {
	if s.client == nil || channelID == "" || messageTS == "" || emoji == "" {
		return nil
	}
	if err := s.client.RemoveReaction(emoji, slack.ItemRef{Channel: channelID, Timestamp: messageTS}); err != nil {
		// "no_reaction" means the reaction was already gone — ignore it.
		if strings.Contains(err.Error(), "no_reaction") {
			return nil
		}
		return err
	}
	return nil
}

// SendThreadMessage sends a message to a Slack thread
func (s *SlackService) SendThreadMessage(ctx context.Context, threadID ThreadID, message string) (string, error) {
	if s.client == nil {
		return "", fmt.Errorf("slack client not initialized")
	}

	formatted := convertMarkdownToSlackMrkdwn(message)

	// Split long messages
	formatter := &SlackFormatter{}
	parts := formatter.SplitLongMessage(formatted)
	logBotOutboundMessage("slack", threadID, "thread", formatted, len(parts), 0)

	var lastTS string
	for _, part := range parts {
		// Use section blocks with explicit mrkdwn type for reliable formatting
		sectionBlock := slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, part, false, false),
			nil, nil,
		)
		_, ts, err := s.client.PostMessageContext(ctx,
			threadID.ChannelID,
			slack.MsgOptionText(part, false),
			slack.MsgOptionBlocks(sectionBlock),
			slack.MsgOptionTS(threadID.ThreadTS),
		)
		if err != nil {
			return lastTS, fmt.Errorf("failed to post thread message: %w", err)
		}
		lastTS = ts
	}

	return lastTS, nil
}

// SendThreadMessageWithBlocks sends a message with interactive blocks to a Slack thread
func (s *SlackService) SendThreadMessageWithBlocks(ctx context.Context, threadID ThreadID, message string, blocks []MessageBlock) (string, error) {
	if s.client == nil {
		return "", fmt.Errorf("slack client not initialized")
	}

	formatted := convertMarkdownToSlackMrkdwn(message)

	// Build Slack blocks
	var slackBlocks []slack.Block

	// Text section
	slackBlocks = append(slackBlocks, slack.NewSectionBlock(
		slack.NewTextBlockObject(slack.MarkdownType, formatted, false, false),
		nil, nil,
	))

	// Convert MessageBlocks to Slack blocks
	for _, block := range blocks {
		if block.Type == "actions" && len(block.Buttons) > 0 {
			var actionElements []slack.BlockElement
			for _, btn := range block.Buttons {
				style := slack.StyleDefault
				if btn.Style == "primary" {
					style = slack.StylePrimary
				} else if btn.Style == "danger" {
					style = slack.StyleDanger
				}
				btnElement := slack.NewButtonBlockElement(btn.ActionID, btn.Value,
					slack.NewTextBlockObject(slack.PlainTextType, btn.Text, false, false),
				)
				btnElement.Style = style
				actionElements = append(actionElements, btnElement)
			}
			slackBlocks = append(slackBlocks, slack.NewActionBlock("", actionElements...))
		}
	}

	logBotOutboundMessage("slack", threadID, "blocks", formatted, 1, len(blocks))

	_, ts, err := s.client.PostMessageContext(ctx,
		threadID.ChannelID,
		slack.MsgOptionBlocks(slackBlocks...),
		slack.MsgOptionTS(threadID.ThreadTS),
	)
	if err != nil {
		return "", fmt.Errorf("failed to post thread message with blocks: %w", err)
	}

	return ts, nil
}

// UpdateMessage updates an existing Slack message
func (s *SlackService) UpdateMessage(ctx context.Context, threadID ThreadID, messageID string, newText string) error {
	if s.client == nil {
		return fmt.Errorf("slack client not initialized")
	}

	formatted := convertMarkdownToSlackMrkdwn(newText)
	logBotOutboundMessage("slack", threadID, "update", formatted, 1, 0)

	opts := []slack.MsgOption{slack.MsgOptionText(formatted, false)}
	if len(formatted) <= 3000 {
		sectionBlock := slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, formatted, false, false),
			nil, nil,
		)
		opts = append(opts, slack.MsgOptionBlocks(sectionBlock))
	}
	_, _, _, err := s.client.UpdateMessageContext(ctx, threadID.ChannelID, messageID, opts...)
	if err != nil {
		return fmt.Errorf("failed to update message: %w", err)
	}

	return nil
}

// GetThreadHistory retrieves the full history of a Slack thread
// GetChannelName returns the Slack channel's human-readable name (e.g. "general"),
// or "" on any error. Used to enrich LLM prompt context on new-session starts.
func (s *SlackService) GetChannelName(ctx context.Context, channelID string) string {
	if s.client == nil || channelID == "" {
		return ""
	}
	info, err := s.client.GetConversationInfoContext(ctx, &slack.GetConversationInfoInput{
		ChannelID:         channelID,
		IncludeLocale:     false,
		IncludeNumMembers: false,
	})
	if err != nil || info == nil {
		return ""
	}
	return info.Name
}

func (s *SlackService) GetThreadHistory(ctx context.Context, threadID ThreadID) ([]ThreadMessage, error) {
	if s.client == nil {
		return nil, fmt.Errorf("slack client not initialized")
	}

	params := &slack.GetConversationRepliesParameters{
		ChannelID: threadID.ChannelID,
		Timestamp: threadID.ThreadTS,
		Limit:     100,
	}

	msgs, _, _, err := s.client.GetConversationRepliesContext(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to get thread replies: %w", err)
	}

	var history []ThreadMessage
	for _, msg := range msgs {
		isBot := msg.BotID != "" || (s.botUserID != "" && msg.User == s.botUserID)

		// Parse timestamp
		ts := time.Now()
		if msg.Timestamp != "" {
			// Slack timestamps are Unix epoch with fractional seconds
			parts := strings.SplitN(msg.Timestamp, ".", 2)
			if len(parts) >= 1 {
				var sec int64
				fmt.Sscanf(parts[0], "%d", &sec)
				ts = time.Unix(sec, 0)
			}
		}

		history = append(history, ThreadMessage{
			UserID:    msg.User,
			UserName:  msg.User, // Slack API returns user ID; resolving display names would need users.info
			Text:      msg.Text,
			Timestamp: ts,
			IsBot:     isBot,
		})
	}

	return history, nil
}

// isDuplicateMessage checks if a Slack message timestamp has already been processed.
// Returns true if duplicate. Cleans up entries older than 5 minutes.
func (s *SlackService) isDuplicateMessage(ts string) bool {
	if ts == "" {
		return false
	}
	s.seenMessagesMu.Lock()
	defer s.seenMessagesMu.Unlock()

	if _, seen := s.seenMessages[ts]; seen {
		log.Printf("[SLACK_DEDUP] Skipping duplicate message ts=%s", ts)
		return true
	}
	if s.seenMessages == nil {
		s.seenMessages = make(map[string]time.Time)
	}
	s.seenMessages[ts] = time.Now()

	// Cleanup entries older than 5 minutes
	cutoff := time.Now().Add(-5 * time.Minute)
	for k, v := range s.seenMessages {
		if v.Before(cutoff) {
			delete(s.seenMessages, k)
		}
	}
	return false
}

// handleAppMentionEvent handles @mention events
func (s *SlackService) handleAppMentionEvent(ev *slackevents.AppMentionEvent) {
	if ev.Edited != nil || ev.User == s.botUserID || ev.BotID != "" {
		return
	}
	if s.handleConfiguredSlackTrigger(&slackevents.MessageEvent{User: ev.User, Channel: ev.Channel, Text: ev.Text, TimeStamp: ev.TimeStamp, ThreadTimeStamp: ev.ThreadTimeStamp}) {
		return
	}
	if s.messageHandler == nil {
		log.Printf("[SLACK_BOT] AppMention received but no message handler set, ignoring")
		return
	}

	// Dedup: skip if we've already seen this message timestamp
	if s.isDuplicateMessage(ev.Channel + ":" + ev.TimeStamp) {
		return
	}

	// Strip the @mention from the text
	text := s.stripMention(ev.Text)

	// Determine if this is in a thread
	isThreadReply := ev.ThreadTimeStamp != "" && ev.ThreadTimeStamp != ev.TimeStamp
	threadTS := ev.ThreadTimeStamp
	if threadTS == "" {
		// New channel message — use the message's own timestamp as the thread root
		threadTS = ev.TimeStamp
	}

	log.Printf("[SLACK_BOT] AppMention from user=%s channel=%s thread=%s: %s", ev.User, ev.Channel, threadTS, botTruncate(text, 80))

	userEmail := s.resolveUserEmail(ev.User)
	text, presetRoute, handled := s.routeSlackWorkflowMessage(context.Background(), ev.User, userEmail, ev.Channel, threadTS, text, isThreadReply)
	if handled {
		return
	}
	s.messageHandler(BotIncomingMessage{
		Platform:       "slack",
		UserID:         ev.User,
		UserName:       ev.User,
		UserEmail:      userEmail,
		ChannelID:      ev.Channel,
		ThreadTS:       threadTS,
		Text:           text,
		MessageTS:      ev.TimeStamp,
		Timestamp:      time.Now(),
		IsThreadReply:  isThreadReply,
		IsMention:      true,
		PresetWorkflow: presetRoute,
	})
}

// stripMention removes <@BOTID> from message text
func (s *SlackService) stripMention(text string) string {
	if s.botUserID != "" {
		mention := fmt.Sprintf("<@%s>", s.botUserID)
		text = strings.Replace(text, mention, "", -1)
	}
	// Also strip any remaining <@...> patterns
	mentionRegex := regexp.MustCompile(`<@[A-Z0-9]+>`)
	text = mentionRegex.ReplaceAllString(text, "")
	return strings.TrimSpace(text)
}

// resolveUserEmail looks up a Slack user's email via users.info API.
// Returns empty string on failure (non-fatal).
func (s *SlackService) resolveUserEmail(userID string) string {
	if s.client == nil || userID == "" {
		return ""
	}
	user, err := s.client.GetUserInfo(userID)
	if err != nil {
		log.Printf("[SLACK_BOT] Failed to resolve email for user %s: %v", userID, err)
		return ""
	}
	return user.Profile.Email
}

func (s *SlackService) resolveSlackChannelWorkflow(channelID string) *ChannelRoute {
	channelID = strings.ToUpper(strings.TrimSpace(channelID))
	if channelID == "" {
		return nil
	}
	content, exists, err := readWorkspaceFile(context.Background(), workspaceAPIURL(), "config/bot-connectors.json")
	if err != nil || !exists || content == "" {
		return nil
	}
	var raw map[string]struct {
		AllowedChannels string `json:"allowed_channels"`
	}
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return nil
	}
	slackCfg, ok := raw["slack"]
	if !ok || slackCfg.AllowedChannels == "" || slackCfg.AllowedChannels == "[]" || slackCfg.AllowedChannels == "{}" {
		return nil
	}
	var routes map[string]ChannelRoute
	if err := json.Unmarshal([]byte(slackCfg.AllowedChannels), &routes); err != nil {
		return nil
	}
	if route := ResolveChannelRoute(slackCfg.AllowedChannels, channelID); route != nil {
		return route
	}

	return nil
}

func slackUserChatUploadFolder(userID string) string {
	safeUserID := sanitizeWhatsAppFileName(userID)
	if safeUserID == "" {
		safeUserID = "default"
	}
	return filepath.ToSlash(filepath.Join("_users", safeUserID, "chat_history", "uploads", "slack", time.Now().Format("2006-01-02")))
}

func slackWorkflowUploadFolder(route *ChannelRoute) string {
	if route == nil || strings.TrimSpace(route.WorkspacePath) == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Join(route.WorkspacePath, "incoming", "slack", time.Now().Format("2006-01-02")))
}

func (s *SlackService) appendSlackFileContext(ctx context.Context, text string, msg *slack.Msg, channelID, userEmail string, route *ChannelRoute) string {
	if msg == nil || len(msg.Files) == 0 || s.client == nil {
		return text
	}

	workspaceUserID := emailToUserID(userEmail)
	if workspaceUserID == "" {
		workspaceUserID = channelID
	}
	folderPath := slackWorkflowUploadFolder(route)
	if folderPath == "" {
		folderPath = slackUserChatUploadFolder(workspaceUserID)
	}

	wsClient := workspace.NewClient(workspaceAPIURL(), workspace.WithUserID(workspaceUserID))
	var saved []whatsappDownloadedMedia
	var failures []string
	const maxSlackUploadBytes = 10 * 1024 * 1024

	for _, f := range msg.Files {
		if f.ID == "" && f.URLPrivate == "" && f.URLPrivateDownload == "" {
			continue
		}
		if f.Size > maxSlackUploadBytes {
			failures = append(failures, fmt.Sprintf("%s: file is %.1fMB; Slack bot uploads are limited to 10MB", f.Name, float64(f.Size)/(1024*1024)))
			continue
		}

		fileInfo := &f
		if f.ID != "" {
			if info, _, _, err := s.client.GetFileInfoContext(ctx, f.ID, 0, 0); err == nil && info != nil {
				fileInfo = info
			}
		}
		downloadURL := fileInfo.URLPrivateDownload
		if downloadURL == "" {
			downloadURL = fileInfo.URLPrivate
		}
		if downloadURL == "" {
			failures = append(failures, fmt.Sprintf("%s: no private download URL", fileInfo.Name))
			continue
		}

		var buf bytes.Buffer
		downloadCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		err := s.client.GetFileContext(downloadCtx, downloadURL, &buf)
		cancel()
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: download failed: %v", fileInfo.Name, err))
			continue
		}
		if buf.Len() > maxSlackUploadBytes {
			failures = append(failures, fmt.Sprintf("%s: file is %.1fMB; Slack bot uploads are limited to 10MB", fileInfo.Name, float64(buf.Len())/(1024*1024)))
			continue
		}

		fileName := sanitizeWhatsAppFileName(fileInfo.Name)
		if fileName == "" {
			fileName = sanitizeWhatsAppFileName(fileInfo.Title)
		}
		if fileName == "" {
			ext := extensionForWhatsAppMedia(fileInfo.Mimetype, ".bin")
			fileName = fmt.Sprintf("slack-file-%s%s", sanitizeWhatsAppFileName(fileInfo.ID), ext)
		}

		filePath, err := wsClient.UploadBinary(ctx, folderPath, fileName, buf.Bytes())
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: upload failed: %v", fileName, err))
			continue
		}
		kind := fileInfo.Filetype
		if kind == "" {
			kind = "file"
		}
		mimeType := fileInfo.Mimetype
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		saved = append(saved, whatsappDownloadedMedia{
			Kind:      kind,
			FileName:  fileName,
			FilePath:  filePath,
			MimeType:  mimeType,
			SizeBytes: buf.Len(),
		})
	}

	if len(saved) == 0 && len(failures) == 0 {
		return text
	}
	var sb strings.Builder
	if strings.TrimSpace(text) != "" {
		sb.WriteString(strings.TrimSpace(text))
		sb.WriteString("\n\n")
	}
	for i, media := range saved {
		if i == 0 {
			sb.WriteString("Slack upload received:\n")
		}
		sb.WriteString(fmt.Sprintf("- File: %s\n", media.FilePath))
		sb.WriteString(fmt.Sprintf("  Original name: %s\n", media.FileName))
		sb.WriteString(fmt.Sprintf("  Type: %s\n", media.Kind))
		sb.WriteString(fmt.Sprintf("  MIME type: %s\n", media.MimeType))
		sb.WriteString(fmt.Sprintf("  Size: %d bytes\n", media.SizeBytes))
	}
	for _, failure := range failures {
		sb.WriteString(fmt.Sprintf("- Upload issue: %s\n", failure))
	}
	if len(saved) > 0 {
		sb.WriteString("\nUse the uploaded file path above when reading or analyzing the attachment.")
	}
	return sb.String()
}

// --- SlackFormatter implements MessageFormatter ---

// SlackFormatter converts Markdown to Slack mrkdwn format
type SlackFormatter struct{}

// FormatMessage converts standard Markdown to Slack mrkdwn
func (f *SlackFormatter) FormatMessage(markdown string) string {
	return convertMarkdownToSlackMrkdwn(markdown)
}

// MaxMessageLength returns Slack's message length limit
// Using 3000 to fit within section block text limit
func (f *SlackFormatter) MaxMessageLength() int {
	return 3000
}

// SplitLongMessage splits a message into chunks within Slack's limit
func (f *SlackFormatter) SplitLongMessage(text string) []string {
	maxLen := f.MaxMessageLength()
	if len(text) <= maxLen {
		return []string{text}
	}

	var parts []string
	for len(text) > 0 {
		if len(text) <= maxLen {
			parts = append(parts, text)
			break
		}

		// Try to split at a newline
		splitIdx := strings.LastIndex(text[:maxLen], "\n")
		if splitIdx < maxLen/2 {
			// No good newline split point, split at max length
			splitIdx = maxLen
		}

		parts = append(parts, text[:splitIdx])
		text = text[splitIdx:]
		text = strings.TrimLeft(text, "\n")
	}

	return parts
}

// GetConfig returns current Slack configuration (with masked tokens)
func (s *SlackService) GetConfig() *SlackConfig {
	if s.config == nil {
		return &SlackConfig{Enabled: false}
	}

	// Return config with masked tokens
	config := *s.config
	if config.BotToken != "" && len(config.BotToken) <= 4 {
		config.BotToken = "xoxb-..."
	} else if len(config.BotToken) > 4 {
		config.BotToken = "xoxb-..." + config.BotToken[len(config.BotToken)-4:]
	}
	if config.AppToken != "" && len(config.AppToken) <= 4 {
		config.AppToken = "xapp-..."
	} else if len(config.AppToken) > 4 {
		config.AppToken = "xapp-..." + config.AppToken[len(config.AppToken)-4:]
	}

	return &config
}

// PostRouteMessage makes exactly one Slack post, preserving the root timestamp.
// Route scope, durable idempotency and references are owned by the application.
func (s *SlackService) PostRouteMessage(ctx context.Context, channel, thread, message string) (string, error) {
	if s.client == nil || !s.IsEnabled() {
		return "", fmt.Errorf("Slack connector unavailable")
	}
	_, ts, err := s.client.PostMessageContext(ctx, channel, slack.MsgOptionText(convertMarkdownToSlackMrkdwn(message), false), slack.MsgOptionTS(thread))
	return ts, err
}

// UserEmailForRoute resolves the authenticated Slack actor for exclusions.
func (s *SlackService) UserEmailForRoute(user string) string { return s.resolveUserEmail(user) }

// DeleteMessage removes a temporary bot-owned status message.
func (s *SlackService) DeleteMessage(ctx context.Context, threadID ThreadID, messageID string) error {
	if s.client == nil {
		return fmt.Errorf("slack client not initialized")
	}
	_, _, err := s.client.DeleteMessageContext(ctx, threadID.ChannelID, messageID)
	if err != nil && err.Error() == "message_not_found" {
		return nil
	}
	return err
}
