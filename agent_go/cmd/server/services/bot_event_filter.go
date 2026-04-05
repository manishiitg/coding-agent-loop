package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/manishiitg/mcpagent/events"

	"mcp-agent-builder-go/agent_go/pkg/database"
)

// BlockingEventCallback is called when a blocking event is received.
// The eventType is "plan_approval", "blocking_human_feedback", or "blocking_human_questions".
type BlockingEventCallback func(eventType string)

// SessionDoneCallback is called when the event filter determines the session is truly complete.
// This fires when: a main-level completion arrives, no delegations are pending, and no blocking events are active.
type SessionDoneCallback func()

// BotEventFilter filters agent events and posts updates to a platform thread.
// It also tracks session lifecycle: delegations, blocking events, and completion.
type BotEventFilter struct {
	connector    BotConnector
	threadID     ThreadID
	botSessionID string
	db           database.Database
	appBaseURL   string // public app URL for shareable links (e.g., "https://app.example.com")
	userID       string // workspace user ID for shareable links (e.g., "default")

	mu                 sync.Mutex
	delegationNames    map[string]string // correlationID -> instruction (sub-agent name)
	pendingDelegations int
	awaitingInput      bool // set on any blocking event, cleared externally
	completionReceived bool // set when unified_completion, agent_end, or conversation_end arrives
	sessionDone        bool // idempotency guard — ensures onSessionDone fires at most once
	baseHierarchy      int      // the minimum hierarchy level seen — treated as "main" level
	baseHierarchySet   bool     // true once baseHierarchy has been calibrated
	lastActivity       string   // human-friendly description of current activity
	toolCallCount      int      // total tool calls seen (for progress feel)
	mainTextSent       bool     // true once we've sent main-level text via llm_generation_end
	onBlockingEvent    BlockingEventCallback
	onSessionDone      SessionDoneCallback
}

// NewBotEventFilter creates a new event filter
func NewBotEventFilter(connector BotConnector, threadID ThreadID, botSessionID string, db database.Database, appBaseURL string, userID string) *BotEventFilter {
	return &BotEventFilter{
		connector:       connector,
		threadID:        threadID,
		botSessionID:    botSessionID,
		db:              db,
		appBaseURL:      strings.TrimSuffix(appBaseURL, "/"),
		userID:          userID,
		delegationNames: make(map[string]string),
	}
}

// SetBlockingEventCallback sets the callback for blocking events (plan_approval, human feedback, etc.)
func (f *BotEventFilter) SetBlockingEventCallback(cb BlockingEventCallback) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onBlockingEvent = cb
}

// SetSessionDoneCallback sets the callback invoked when the session is determined to be complete
func (f *BotEventFilter) SetSessionDoneCallback(cb SessionDoneCallback) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onSessionDone = cb
}

// ClearBlockingState clears the awaiting input flag (called when user responds to a blocking event).
// Also resets completionReceived so the session stays alive for the follow-up agent's completion.
func (f *BotEventFilter) ClearBlockingState() {
	f.mu.Lock()
	f.awaitingInput = false
	f.completionReceived = false
	f.mainTextSent = false // reset so follow-up agent's completion will be shown
	f.mu.Unlock()
	log.Printf("[BOT_FILTER] Blocking state cleared, completion reset")
}

// Start begins listening to events for a session and forwarding filtered updates to the thread
func (f *BotEventFilter) Start(ctx context.Context, subscriber BotEventSubscriber, sessionID string) {
	log.Printf("[BOT_FILTER] Start: subscribing to events for session %s", sessionID)
	ch, unsubscribe := subscriber.SubscribeBot(sessionID)
	defer func() {
		log.Printf("[BOT_FILTER] Start: exiting event loop for session %s", sessionID)
		unsubscribe()
	}()

	// Heartbeat — escalating intervals, minimal Slack-native style
	heartbeatIntervals := []time.Duration{30 * time.Second, 60 * time.Second, 90 * time.Second}
	heartbeatCount := 0
	heartbeat := time.NewTicker(heartbeatIntervals[0])
	defer heartbeat.Stop()
	lastSendTime := time.Now()
	lastEventTime := time.Time{} // zero until first event received

	for {
		select {
		case <-ctx.Done():
			log.Printf("[BOT_FILTER] Start: context canceled for session %s", sessionID)
			return

		case <-heartbeat.C:
			f.mu.Lock()
			done := f.sessionDone
			awaiting := f.awaitingInput
			activity := f.lastActivity
			toolCount := f.toolCallCount
			pendingDel := f.pendingDelegations
			f.mu.Unlock()
			// Only send heartbeat if: session active, events flowing recently, and no message sent recently
			if !done && !awaiting && !lastEventTime.IsZero() &&
				time.Since(lastEventTime) < 60*time.Second &&
				time.Since(lastSendTime) > 20*time.Second {
				msg := f.buildHeartbeatMessage(activity, toolCount, pendingDel, heartbeatCount)
				f.sendMessage(ctx, msg)
				lastSendTime = time.Now()
			}
			// Escalate interval: 30s → 60s → 90s (then stay at 90s)
			heartbeatCount++
			if heartbeatCount < len(heartbeatIntervals) {
				heartbeat.Reset(heartbeatIntervals[heartbeatCount])
			}

		case event, ok := <-ch:
			if !ok {
				log.Printf("[BOT_FILTER] Start: channel closed for session %s", sessionID)
				return
			}
			lastEventTime = time.Now()
			sent := f.processEvent(ctx, event)
			if sent {
				lastSendTime = time.Now()
			}
		}
	}
}

// isMainLevel returns true if the event is at the base hierarchy level for this session.
// Bot sessions may run at hierarchy > 0 (e.g., handleQuery wraps the agent), so we
// calibrate the base level from the first event and treat it as "main".
func (f *BotEventFilter) isMainLevel(event BotEventData) bool {
	if event.Data == nil {
		return false
	}
	f.mu.Lock()
	if !f.baseHierarchySet {
		f.baseHierarchy = event.Data.HierarchyLevel
		f.baseHierarchySet = true
		log.Printf("[BOT_FILTER] Calibrated base hierarchy level to %d", f.baseHierarchy)
	}
	base := f.baseHierarchy
	f.mu.Unlock()
	return event.Data.HierarchyLevel <= base
}

// processEvent handles a single event. Returns true if a message was sent to the thread.
func (f *BotEventFilter) processEvent(ctx context.Context, event BotEventData) bool {
	if event.Data == nil {
		return false
	}

	switch event.Type {
	case "delegation_start":
		f.trackDelegationName(event)
		f.mu.Lock()
		f.pendingDelegations++
		pending := f.pendingDelegations
		// Set activity to sub-agent name if available
		if name := f.getDelegationName(event); name != "" {
			f.lastActivity = fmt.Sprintf("Delegated: %s", name)
		} else {
			f.lastActivity = "Delegating to sub-agent"
		}
		f.mu.Unlock()
		log.Printf("[BOT_FILTER] delegation_start: pending=%d", pending)

	case "tool_call_start":
		// Track activity for heartbeat — translate tool names to user-friendly descriptions
		if toolDesc := f.describeToolCall(event); toolDesc != "" {
			f.mu.Lock()
			f.toolCallCount++
			f.lastActivity = toolDesc
			f.mu.Unlock()
		}

	case "llm_generation_end":
		msg := f.formatGenerationEnd(event)
		if msg != "" {
			log.Printf("[BOT_FILTER] llm_generation_end: sending %d chars (level=%d)", len(msg), event.Data.HierarchyLevel)
			f.sendMessage(ctx, msg)
			if f.isMainLevel(event) {
				f.mu.Lock()
				f.mainTextSent = true
				f.mu.Unlock()
			}
			return true
		} else {
			log.Printf("[BOT_FILTER] llm_generation_end: skipped (mainLevel=%v, level=%d)", f.isMainLevel(event), event.Data.HierarchyLevel)
		}

	case "delegation_end":
		f.mu.Lock()
		if f.pendingDelegations > 0 {
			f.pendingDelegations--
		}
		pending := f.pendingDelegations
		awaiting := f.awaitingInput
		f.mu.Unlock()
		log.Printf("[BOT_FILTER] delegation_end: pending=%d awaitingInput=%v", pending, awaiting)
		f.checkSessionDone("delegation_end")

	case "unified_completion":
		// Skip server error completions (from failed follow-ups, etc.)
		if uc, ok := event.Data.Data.(*events.UnifiedCompletionEvent); ok && uc.Status == "error" {
			log.Printf("[BOT_FILTER] unified_completion: skipping error completion (agent_type=%s)", uc.AgentType)
			return false
		}
		sent := false
		isMain := f.isMainLevel(event)
		if uc, ok := event.Data.Data.(*events.UnifiedCompletionEvent); ok {
			log.Printf("[BOT_FILTER] unified_completion: isMain=%v level=%d result_len=%d", isMain, event.Data.HierarchyLevel, len(uc.FinalResult))
		}
		if !isMain {
			// Sub-agent completions — always show their result summary
			msg := f.formatUnifiedCompletion(event)
			if msg != "" {
				f.sendMessage(ctx, msg)
				sent = true
			}
		} else {
			// Main-level completion — only send if no text was already sent via llm_generation_end
			// (avoids duplicates when the agent sends text + completion with same content)
			f.mu.Lock()
			alreadySent := f.mainTextSent
			f.mu.Unlock()
			if !alreadySent {
				msg := f.formatUnifiedCompletion(event)
				if msg != "" {
					log.Printf("[BOT_FILTER] unified_completion: sending main-level result (no prior text sent)")
					f.sendMessage(ctx, msg)
					sent = true
				}
			} else {
				log.Printf("[BOT_FILTER] unified_completion: skipping main-level (text already sent via generation_end)")
			}
		}
		// Only main-level completions signal that the session is done.
		if isMain {
			f.mu.Lock()
			f.completionReceived = true
			f.mu.Unlock()
			f.checkSessionDone("unified_completion")
		}
		return sent

	case "plan_approval":
		msg := f.formatPlanApproval(event)
		if msg != "" {
			f.sendMessage(ctx, msg)
			return true
		}
		f.setBlocking("plan_approval")

	case "blocking_human_feedback":
		msg := f.formatBlockingFeedback(event)
		if msg != "" {
			f.sendMessage(ctx, msg)
			return true
		}
		f.setBlocking("blocking_human_feedback")

	case "blocking_human_questions":
		msg := f.formatBlockingQuestions(event)
		if msg != "" {
			f.sendMessage(ctx, msg)
			return true
		}
		f.setBlocking("blocking_human_questions")

	case "agent_end", "conversation_end":
		// Only main-level events signal session completion
		if f.isMainLevel(event) {
			f.mu.Lock()
			f.completionReceived = true
			f.mu.Unlock()
			f.checkSessionDone(event.Type)
		}

	case "agent_error", "conversation_error":
		f.sendMessage(ctx, "An error occurred during processing.")
		return true
	}
	return false
}

// setBlocking sets the blocking state and notifies the callback
func (f *BotEventFilter) setBlocking(eventType string) {
	f.mu.Lock()
	f.awaitingInput = true
	cb := f.onBlockingEvent
	f.mu.Unlock()

	log.Printf("[BOT_FILTER] Blocking event: %s", eventType)
	if cb != nil {
		cb(eventType)
	}
}

// checkSessionDone checks if the session is truly complete:
// completionReceived AND no pending delegations AND not awaiting user input.
// The sessionDone flag ensures onSessionDone fires at most once.
func (f *BotEventFilter) checkSessionDone(eventType string) {
	f.mu.Lock()
	if f.sessionDone {
		f.mu.Unlock()
		return
	}
	pending := f.pendingDelegations
	awaiting := f.awaitingInput
	completed := f.completionReceived
	cb := f.onSessionDone
	if pending == 0 && !awaiting && completed {
		f.sessionDone = true
	}
	f.mu.Unlock()

	if pending > 0 {
		log.Printf("[BOT_FILTER] %s: %d delegations still pending", eventType, pending)
		return
	}
	if awaiting {
		log.Printf("[BOT_FILTER] %s: awaiting user input", eventType)
		return
	}
	if !completed {
		log.Printf("[BOT_FILTER] %s: no completion event yet", eventType)
		return
	}
	log.Printf("[BOT_FILTER] %s: session done", eventType)
	if cb != nil {
		cb()
	}
}

// trackDelegationName extracts the sub-agent instruction from delegation_start events.
func (f *BotEventFilter) trackDelegationName(event BotEventData) {
	if event.Data == nil || event.Data.Data == nil {
		return
	}

	raw, err := json.Marshal(event.Data.Data)
	if err != nil {
		return
	}
	var parsed struct {
		DelegationID string `json:"delegation_id"`
		Instruction  string `json:"instruction"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return
	}

	corrID := event.Data.CorrelationID
	if corrID == "" {
		corrID = parsed.DelegationID
	}
	if corrID != "" && parsed.Instruction != "" {
		// Store a short name — first line capped at 60 chars
		name := strings.SplitN(parsed.Instruction, "\n", 2)[0]
		if len(name) > 60 {
			name = name[:60] + "..."
		}
		f.mu.Lock()
		f.delegationNames[corrID] = name
		f.mu.Unlock()
	}
}

// formatUnifiedCompletion formats a unified_completion event.
func (f *BotEventFilter) formatUnifiedCompletion(event BotEventData) string {
	if event.Data == nil || event.Data.Data == nil {
		return ""
	}

	uc, ok := event.Data.Data.(*events.UnifiedCompletionEvent)
	if !ok {
		return ""
	}

	isSubAgent := !f.isMainLevel(event)

	summary := uc.FinalResult

	if isSubAgent {
		name := ""
		corrID := event.Data.CorrelationID
		if corrID != "" {
			f.mu.Lock()
			name = f.delegationNames[corrID]
			f.mu.Unlock()
		}
		if name == "" {
			name = "Sub-agent"
		}

		if summary != "" {
			return fmt.Sprintf("**[%s]**\n%s", name, summary)
		}
		return "" // no useful info to show
	}

	// Main-level completion — only show if there's actual content
	if summary != "" {
		return fmt.Sprintf("**Result:**\n%s", summary)
	}
	// No summary — skip (Session completed message covers this)
	return ""
}

// formatPlanApproval formats a plan_approval event.
func (f *BotEventFilter) formatPlanApproval(event BotEventData) string {
	if event.Data == nil || event.Data.Data == nil {
		return "**Plan ready for review.**\nReply **approve** to execute or **reject** to cancel."
	}

	raw, err := json.Marshal(event.Data.Data)
	if err != nil {
		return "**Plan ready for review.**\nReply **approve** to execute or **reject** to cancel."
	}
	var parsed struct {
		Question string `json:"question"`
		Context  string `json:"context"` // plan markdown
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "**Plan ready for review.**\nReply **approve** to execute or **reject** to cancel."
	}

	var sb strings.Builder
	sb.WriteString("**Plan ready for review:**\n\n")

	planContent := parsed.Context
	if planContent != "" {
		sb.WriteString(planContent)
		sb.WriteString("\n\n")
	}

	sb.WriteString("Reply **approve** to execute or **reject** to cancel.")
	return sb.String()
}

// formatBlockingFeedback formats a blocking_human_feedback event.
func (f *BotEventFilter) formatBlockingFeedback(event BotEventData) string {
	if event.Data == nil || event.Data.Data == nil {
		return "The agent needs your input to continue."
	}

	raw, err := json.Marshal(event.Data.Data)
	if err != nil {
		return "The agent needs your input to continue."
	}
	var parsed struct {
		Question string `json:"question"`
		Message  string `json:"message"`
		Prompt   string `json:"prompt"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "The agent needs your input to continue."
	}

	question := parsed.Question
	if question == "" {
		question = parsed.Message
	}
	if question == "" {
		question = parsed.Prompt
	}
	if question != "" {
		return fmt.Sprintf("**Waiting for input:**\n%s", question)
	}
	return "The agent needs your input to continue."
}

// formatBlockingQuestions formats a blocking_human_questions event.
func (f *BotEventFilter) formatBlockingQuestions(event BotEventData) string {
	if event.Data == nil || event.Data.Data == nil {
		return "The agent has questions that need your answers."
	}

	raw, err := json.Marshal(event.Data.Data)
	if err != nil {
		return "The agent has questions that need your answers."
	}
	var parsed struct {
		Questions []struct {
			Question string `json:"question"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil || len(parsed.Questions) == 0 {
		return "The agent has questions that need your answers."
	}

	msg := "**Questions from agent:**\n"
	for i, q := range parsed.Questions {
		if i >= 5 {
			msg += fmt.Sprintf("...and %d more\n", len(parsed.Questions)-5)
			break
		}
		msg += fmt.Sprintf("%d. %s\n", i+1, q.Question)
	}
	return msg
}



// getDelegationName extracts the sub-agent name from a delegation event (must hold lock or call before lock).
func (f *BotEventFilter) getDelegationName(event BotEventData) string {
	if event.Data == nil || event.Data.Data == nil {
		return ""
	}
	raw, err := json.Marshal(event.Data.Data)
	if err != nil {
		return ""
	}
	var parsed struct {
		DelegationID string `json:"delegation_id"`
		Instruction  string `json:"instruction"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ""
	}
	name := strings.SplitN(parsed.Instruction, "\n", 2)[0]
	if len(name) > 50 {
		name = name[:50] + "…"
	}
	return name
}

// describeToolCall translates a tool_call_start event into a user-friendly activity description.
func (f *BotEventFilter) describeToolCall(event BotEventData) string {
	if event.Data == nil || event.Data.Data == nil {
		return ""
	}
	raw, err := json.Marshal(event.Data.Data)
	if err != nil {
		return ""
	}
	var parsed struct {
		ToolName string `json:"tool_name"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil || parsed.ToolName == "" {
		return ""
	}

	// Map tool names to user-friendly descriptions
	switch {
	case parsed.ToolName == "execute_shell_command":
		return "Running commands"
	case parsed.ToolName == "create_delegation_plan":
		return "Planning approach"
	case parsed.ToolName == "delegate":
		return "Delegating to sub-agent"
	case strings.HasPrefix(parsed.ToolName, "read_") || parsed.ToolName == "resources_list":
		return "Reading files"
	case strings.HasPrefix(parsed.ToolName, "write_") || strings.HasPrefix(parsed.ToolName, "create_"):
		return "Writing files"
	case strings.HasPrefix(parsed.ToolName, "edit_"):
		return "Editing files"
	case strings.HasPrefix(parsed.ToolName, "search_") || strings.HasPrefix(parsed.ToolName, "find_"):
		return "Searching"
	case strings.HasPrefix(parsed.ToolName, "browser_") || parsed.ToolName == "web_search":
		return "Browsing the web"
	case strings.HasPrefix(parsed.ToolName, "git_"):
		return "Working with git"
	default:
		// For MCP/unknown tools, humanize the name
		return "Using " + strings.ReplaceAll(parsed.ToolName, "_", " ")
	}
}

// buildHeartbeatMessage constructs a context-aware heartbeat message.
func (f *BotEventFilter) buildHeartbeatMessage(activity string, toolCount int, pendingDelegations int, beatIndex int) string {
	var parts []string

	// Progress indicator
	if pendingDelegations > 0 {
		parts = append(parts, fmt.Sprintf("_%d sub-task(s) running_", pendingDelegations))
	}

	// Current activity
	if activity != "" {
		parts = append(parts, fmt.Sprintf("_%s_", activity))
	}

	// Steps completed gives a sense of progress
	if toolCount > 0 {
		parts = append(parts, fmt.Sprintf("_%d steps completed_", toolCount))
	}

	if len(parts) == 0 {
		return "_Still working on it…_"
	}

	return strings.Join(parts, "  •  ")
}

// formatGenerationEnd formats an llm_generation_end event to show LLM response text.
// Only shows for main agent (level 0) — sub-agent results come via unified_completion.
// Skips pure tool-call turns (no text content).
func (f *BotEventFilter) formatGenerationEnd(event BotEventData) string {
	// Skip sub-agent generations — their results are shown via unified_completion
	if event.Data == nil || event.Data.Data == nil || !f.isMainLevel(event) {
		return ""
	}

	gen, ok := event.Data.Data.(*events.LLMGenerationEndEvent)
	if !ok {
		return ""
	}

	content := strings.TrimSpace(gen.Content)
	if content == "" {
		return "" // pure tool-call turn, no text to show
	}

	return content
}

func (f *BotEventFilter) sendMessage(ctx context.Context, content string) {
	content = f.replaceWorkspacePaths(content)
	msgID, err := f.connector.SendThreadMessage(ctx, f.threadID, content)
	if err != nil {
		log.Printf("[BOT_FILTER] Failed to send message: %v", err)
		return
	}
	f.recordMessage(content, msgID)
}

// workspacePathPattern is the core pattern for workspace file paths.
// Matches Chats/xxx/yyy.ext or Downloads/xxx/yyy.ext
// (at least one subfolder + file with extension).
const workspacePathPattern = `(?:Chats|Downloads)/[\w][\w. -]*/[\w][\w./ -]*\.\w+`

// mdLinkWithWorkspacePath matches markdown links whose URL is a workspace path: [text](Chats/xxx/file.md)
var mdLinkWithWorkspacePath = regexp.MustCompile(`\[([^\]]+)\]\((` + workspacePathPattern + `)\)`)

// bareWorkspacePath matches workspace file paths not inside markdown link syntax.
// Uses a negative lookbehind for '(' to avoid matching paths already wrapped in markdown links.
var bareWorkspacePath = regexp.MustCompile(`(?:^|[^(])(` + workspacePathPattern + `)`)

// replaceWorkspacePaths converts workspace file paths in message text to shareable URLs.
// Paths inside markdown links get their URL replaced; bare paths become new markdown links.
func (f *BotEventFilter) replaceWorkspacePaths(text string) string {
	if f.appBaseURL == "" {
		return text
	}

	// Pass 1: Replace workspace paths already inside markdown links [text](Chats/xxx/file.md)
	text = mdLinkWithWorkspacePath.ReplaceAllStringFunc(text, func(match string) string {
		parts := mdLinkWithWorkspacePath.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		return fmt.Sprintf("[%s](%s)", parts[1], f.buildShareableURL(parts[2]))
	})

	// Pass 2: Wrap bare workspace paths as markdown links
	// After pass 1, all markdown-linked paths are already converted to full URLs,
	// so the bare path regex won't double-match them.
	text = bareWorkspacePath.ReplaceAllStringFunc(text, func(match string) string {
		parts := bareWorkspacePath.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}
		filePath := parts[1]
		prefix := strings.TrimSuffix(match, filePath)
		return prefix + fmt.Sprintf("[%s](%s)", filePath, f.buildShareableURL(filePath))
	})

	return text
}

// buildShareableURL constructs a shareable URL for a workspace file path.
func (f *BotEventFilter) buildShareableURL(filePath string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(filePath))
	url := fmt.Sprintf("%s/file?path=%s", f.appBaseURL, encoded)
	if f.userID != "" {
		url += "&uid=" + f.userID
	}
	return url
}

func (f *BotEventFilter) recordMessage(content, platformMsgID string) {
	f.db.CreateBotMessage(context.Background(), &database.CreateBotMessageRequest{
		BotSessionID:      f.botSessionID,
		Direction:         "outgoing",
		MessageType:       "progress",
		Content:           content,
		PlatformMessageID: platformMsgID,
	})
}
