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
	orchestrator_events "mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
)

// BlockingEventCallback is called when a blocking event is received.
// The eventType is "plan_approval" or "blocking_human_feedback".
type BlockingEventCallback func(eventType string)

// SessionDoneCallback is called when the event filter determines the session is truly complete.
// This fires when: a main-level completion arrives, no delegations are pending, and no blocking events are active.
type SessionDoneCallback func()

// BotEventFilter filters agent events and posts updates to a platform thread.
// It also tracks session lifecycle: delegations, blocking events, and completion.
type BotEventFilter struct {
	connector  BotConnector
	threadID   ThreadID
	sessionID  string // unified chat session id (folder name + thread index key)
	appBaseURL string // public app URL for shareable links (e.g., "https://app.example.com")
	userID     string // workspace user ID for shareable links (e.g., "default")

	mu                  sync.Mutex
	delegationNames     map[string]string // correlationID -> instruction (sub-agent name)
	startedAgents       map[string]bool   // correlationID/name keys already announced as started
	endedAgents         map[string]bool   // correlationID/name keys already announced as ended
	pendingDelegations  int
	awaitingInput       bool   // set on any blocking event, cleared externally
	completionReceived  bool   // set when unified_completion, agent_end, or conversation_end arrives
	sessionDone         bool   // idempotency guard — ensures onSessionDone fires at most once
	workflowStepStarted bool   // true once a workflow step breadcrumb was sent
	baseHierarchy       int    // the minimum hierarchy level seen — treated as "main" level
	baseHierarchySet    bool   // true once baseHierarchy has been calibrated
	lastActivity        string // human-friendly description of current activity
	toolCallCount       int    // total tool calls seen (for progress feel)
	mainTextSent        bool   // true once we've sent main-level text via llm_generation_end
	onBlockingEvent     BlockingEventCallback
	onSessionDone       SessionDoneCallback
}

// NewBotEventFilter creates a new event filter
func NewBotEventFilter(connector BotConnector, threadID ThreadID, sessionID, appBaseURL, userID string) *BotEventFilter {
	return &BotEventFilter{
		connector:       connector,
		threadID:        threadID,
		sessionID:       sessionID,
		appBaseURL:      strings.TrimSuffix(appBaseURL, "/"),
		userID:          userID,
		delegationNames: make(map[string]string),
		startedAgents:   make(map[string]bool),
		endedAgents:     make(map[string]bool),
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

// ResetForNewTurn clears the one-shot completion state so a follow-up user
// message (injected while the session is still held open, e.g. because a
// background workflow is running) starts a fresh turn that can be tracked.
// Without this, sessionDone latches on the first turn and later turns fire
// no onSessionDone — the parent can then be cancelled mid-reply.
func (f *BotEventFilter) ResetForNewTurn() {
	f.mu.Lock()
	f.sessionDone = false
	f.completionReceived = false
	f.mainTextSent = false
	f.mu.Unlock()
	log.Printf("[BOT_FILTER] Reset for new turn")
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
			workflowStepOnly := f.isWhatsAppWorkflowStepOnlyLocked()
			activity := f.lastActivity
			toolCount := f.toolCallCount
			pendingDel := f.pendingDelegations
			f.mu.Unlock()
			// Only send heartbeat if: session active, events flowing recently, and no message sent recently
			if !done && !awaiting && !workflowStepOnly && !lastEventTime.IsZero() &&
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
	case "orchestrator_agent_start":
		msg := f.formatOrchestratorAgentStart(event)
		if msg != "" {
			f.sendMessage(ctx, msg)
			return true
		}

	case "orchestrator_agent_end":
		msg := f.formatOrchestratorAgentEnd(event)
		if msg != "" {
			f.sendMessage(ctx, msg)
			return true
		}

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
		f.mu.Lock()
		suppressWorkflowCompletion := f.isWhatsAppWorkflowStepOnlyLocked()
		f.mu.Unlock()
		if suppressWorkflowCompletion {
			log.Printf("[BOT_FILTER] unified_completion: suppressing WhatsApp workflow completion (level=%d)", event.Data.HierarchyLevel)
		} else if !isMain {
			// Sub-agent completions are intentionally NOT forwarded to the thread —
			// they were spamming Slack with per-step technical dumps (PR lists, log
			// excerpts, etc.) when each nested sub-agent reported back. The user
			// only cares about the main agent's synthesized reply, which arrives
			// via llm_generation_end / main-level unified_completion. Sub-agent
			// detail still lives in workflow logs and the run folder.
			log.Printf("[BOT_FILTER] unified_completion: skipping sub-agent completion (level=%d)", event.Data.HierarchyLevel)
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

	case "agent_end", "conversation_end":
		// Only main-level events signal session completion
		if f.isMainLevel(event) {
			f.mu.Lock()
			f.completionReceived = true
			f.mu.Unlock()
			f.checkSessionDone(event.Type)
		}

	case "agent_error", "conversation_error", "orchestrator_agent_error":
		f.sendMessage(ctx, f.formatErrorEvent(event))
		return true
	}
	return false
}

func (f *BotEventFilter) isWhatsAppWorkflowStepOnlyLocked() bool {
	return strings.EqualFold(f.threadID.Platform, "whatsapp") && f.workflowStepStarted
}

func isWorkflowStepAgent(agentType, agentName string) bool {
	agentType = strings.ToLower(strings.TrimSpace(agentType))
	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		return false
	}

	if agentName == "Full Workflow Execution" ||
		strings.Contains(agentType, "chat") ||
		strings.Contains(agentName, "chat-agent") ||
		strings.Contains(agentType, "learning") ||
		strings.Contains(agentType, "validation") ||
		strings.Contains(agentType, "organizer") {
		return false
	}

	return strings.Contains(agentType, "step") ||
		strings.Contains(agentType, "execution") ||
		strings.Contains(agentType, "conditional") ||
		strings.Contains(agentType, "routing")
}

func workflowStepDisplayName(agentName string) string {
	displayName := strings.TrimSpace(strings.TrimPrefix(agentName, "Step:"))
	if displayName == "" {
		displayName = strings.TrimSpace(agentName)
	}
	return displayName
}

func workflowStepGroup(inputData map[string]string) string {
	if inputData == nil {
		return ""
	}
	group := strings.TrimSpace(inputData["group_name"])
	if group == "" {
		group = strings.TrimSpace(inputData["GroupName"])
	}
	return group
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

// formatOrchestratorAgentStart formats workflow sub-agent starts for bot connectors.
// It intentionally skips the main chat/builder agent and internal helper agents so
// WhatsApp/Slack get step-level breadcrumbs without tool-call noise.
func (f *BotEventFilter) formatOrchestratorAgentStart(event BotEventData) string {
	if event.Data == nil || event.Data.Data == nil {
		return ""
	}

	start, ok := event.Data.Data.(*orchestrator_events.OrchestratorAgentStartEvent)
	if !ok {
		return ""
	}

	agentType := strings.ToLower(strings.TrimSpace(start.AgentType))
	agentName := strings.TrimSpace(start.AgentName)
	if !isWorkflowStepAgent(agentType, agentName) {
		return ""
	}

	displayName := workflowStepDisplayName(agentName)

	key := event.Data.CorrelationID
	if key == "" {
		key = agentType + ":" + displayName
	}
	f.mu.Lock()
	if f.startedAgents[key] {
		f.mu.Unlock()
		return ""
	}
	f.startedAgents[key] = true
	f.workflowStepStarted = true
	f.lastActivity = fmt.Sprintf("Step started: %s", displayName)
	f.mu.Unlock()

	group := workflowStepGroup(start.InputData)
	if group != "" {
		return fmt.Sprintf("Step started (%s): running now [%s].", displayName, group)
	}
	return fmt.Sprintf("Step started (%s): running now.", displayName)
}

// formatOrchestratorAgentEnd formats workflow step ends without forwarding the
// step result body. Tool calls and detailed completions remain in the web UI/logs.
func (f *BotEventFilter) formatOrchestratorAgentEnd(event BotEventData) string {
	if event.Data == nil || event.Data.Data == nil {
		return ""
	}

	end, ok := event.Data.Data.(*orchestrator_events.OrchestratorAgentEndEvent)
	if !ok {
		return ""
	}

	agentType := strings.ToLower(strings.TrimSpace(end.AgentType))
	agentName := strings.TrimSpace(end.AgentName)
	if !isWorkflowStepAgent(agentType, agentName) {
		return ""
	}

	displayName := workflowStepDisplayName(agentName)
	key := event.Data.CorrelationID
	if key == "" {
		key = agentType + ":" + displayName
	}
	f.mu.Lock()
	if f.endedAgents[key] {
		f.mu.Unlock()
		return ""
	}
	f.endedAgents[key] = true
	f.lastActivity = fmt.Sprintf("Step ended: %s", displayName)
	f.mu.Unlock()

	group := workflowStepGroup(end.InputData)
	duration := formatStepDuration(end.Duration)
	status := "completed"
	if !end.Success {
		status = "failed"
	}

	var parts []string
	parts = append(parts, fmt.Sprintf("Step %s (%s)", status, displayName))
	if duration != "" {
		parts = append(parts, duration)
	}
	suffix := ""
	if group != "" {
		suffix = fmt.Sprintf(" [%s]", group)
	}
	return strings.Join(parts, ": ") + suffix + "."
}

func formatStepDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	if d < time.Second {
		return "<1s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
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

// formatErrorEvent renders an error event for display in the bot thread.
// Pulls the actual error message from AgentErrorEvent / ConversationErrorEvent /
// OrchestratorAgentErrorEvent so users see what failed instead of a generic line.
// Falls back to the generic message if the typed payload is unavailable.
func (f *BotEventFilter) formatErrorEvent(event BotEventData) string {
	const fallback = "An error occurred during processing."
	if event.Data == nil || event.Data.Data == nil {
		return fallback
	}
	var errMsg, agent string
	switch d := event.Data.Data.(type) {
	case *events.AgentErrorEvent:
		errMsg = d.Error
	case *events.ConversationErrorEvent:
		errMsg = d.Error
	case *orchestrator_events.OrchestratorAgentErrorEvent:
		errMsg = d.Error
		agent = d.AgentName
	}
	errMsg = strings.TrimSpace(errMsg)
	if errMsg == "" {
		return fallback
	}
	const maxLen = 1500
	if len(errMsg) > maxLen {
		errMsg = errMsg[:maxLen] + "…"
	}
	if agent != "" {
		return fmt.Sprintf("Error in `%s`: %s", agent, errMsg)
	}
	return "Error: " + errMsg
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

	// Strip MCP routing prefixes so heartbeat shows the underlying tool's
	// friendly name instead of leaking the bridge wiring. Supported shapes:
	//   mcp_<server>_<tool>     (single underscore — e.g. mcp_api-bridge_execute_shell_command)
	//   mcp__<server>__<tool>   (double underscore — used by some clients)
	name := parsed.ToolName
	switch {
	case strings.HasPrefix(name, "mcp__"):
		if idx := strings.Index(name[5:], "__"); idx > 0 {
			name = name[5+idx+2:]
		}
	case strings.HasPrefix(name, "mcp_"):
		if idx := strings.Index(name[4:], "_"); idx > 0 {
			name = name[4+idx+1:]
		}
	}

	// Map tool names to user-friendly descriptions
	switch {
	case name == "execute_shell_command":
		return "Running shell commands"
	case name == "delegate" || name == "call_sub_agent" || name == "call_generic_agent":
		return "Delegating to a sub-agent"
	case name == "agent_browser" || strings.HasPrefix(name, "browser_") || name == "web_search":
		return "Browsing the web"
	case strings.HasPrefix(name, "read_") || name == "resources_list":
		return "Reading files"
	case strings.HasPrefix(name, "write_") || strings.HasPrefix(name, "create_"):
		return "Writing files"
	case strings.HasPrefix(name, "edit_") || strings.HasPrefix(name, "diff_patch_"):
		return "Editing files"
	case strings.HasPrefix(name, "search_") || strings.HasPrefix(name, "find_") || name == "search_web_llm":
		return "Searching"
	case strings.HasPrefix(name, "git_"):
		return "Working with git"
	case strings.HasPrefix(name, "image_") || strings.HasPrefix(name, "generate_") || strings.HasPrefix(name, "video_") || name == "read_image" || name == "read_pdf":
		return "Working with media"
	case name == "execute_step" || name == "create_plan" ||
		strings.HasPrefix(name, "add_") || strings.HasPrefix(name, "update_") || strings.HasPrefix(name, "delete_") ||
		name == "debug_step" || name == "query_step" || name == "analyze_step" ||
		name == "cleanup_orphan_step_configs":
		return "Updating the workflow"
	case name == "save_memory" || name == "recall_memory" || name == "enrich_memory":
		return "Working with memory"
	case name == "human_feedback":
		return "Asking the user"
	case name == "generate_text_llm":
		return "Thinking"
	default:
		// Generic fallback — never leak a raw tool name into the chat thread.
		return "Working on the next step"
	}
}

// buildHeartbeatMessage constructs a context-aware heartbeat message.
//
// Goal: a single short status line in the user's chat thread, not a metrics
// dashboard. We surface what's happening (current activity) and whether
// sub-tasks are in flight, but skip the raw tool-call count — it reads as
// noise once it's high ("16 steps completed" doesn't tell the user anything
// actionable in a chat context).
func (f *BotEventFilter) buildHeartbeatMessage(activity string, toolCount int, pendingDelegations int, beatIndex int) string {
	var parts []string

	if pendingDelegations > 0 {
		noun := "sub-task"
		if pendingDelegations > 1 {
			noun = "sub-tasks"
		}
		parts = append(parts, fmt.Sprintf("_%d %s in progress_", pendingDelegations, noun))
	}

	if activity != "" {
		parts = append(parts, fmt.Sprintf("_%s…_", activity))
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
	_, err := f.connector.SendThreadMessage(ctx, f.threadID, content)
	if err != nil {
		log.Printf("[BOT_FILTER] Failed to send message: %v", err)
		return
	}
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
