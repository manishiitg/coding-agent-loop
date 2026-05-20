package terminals

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	storeevents "mcp-agent-builder-go/agent_go/internal/events"

	agentevents "github.com/manishiitg/mcpagent/events"
)

// Snapshot is the latest view-only terminal/TUI screen for one coding-agent execution.
type Snapshot struct {
	TerminalID        string     `json:"terminal_id"`
	SessionID         string     `json:"session_id"`
	OwnerID           string     `json:"owner_id,omitempty"`
	ExecutionID       string     `json:"execution_id,omitempty"`
	ExecutionKind     string     `json:"execution_kind,omitempty"`
	Label             string     `json:"label,omitempty"`
	Scope             string     `json:"scope,omitempty"`
	WorkflowPath      string     `json:"workflow_path,omitempty"`
	WorkflowName      string     `json:"workflow_name,omitempty"`
	WorkflowLabel     string     `json:"workflow_label,omitempty"`
	StepID            string     `json:"step_id,omitempty"`
	StepName          string     `json:"step_name,omitempty"`
	StepType          string     `json:"step_type,omitempty"`
	StepIndex         int        `json:"step_index,omitempty"`
	StepTotal         int        `json:"step_total,omitempty"`
	ParentStepID      string     `json:"parent_step_id,omitempty"`
	StepAttempt       int        `json:"step_attempt,omitempty"`
	StepExecutionMode string     `json:"step_execution_mode,omitempty"` // "learn_code" | "code_exec"
	StepTransport     string     `json:"step_transport,omitempty"`      // "tmux" | "structured"
	StepTriggeredBy   string     `json:"step_triggered_by,omitempty"`   // e.g., "workflow_executor", "parent_step:X"
	AgentName         string     `json:"agent_name,omitempty"`
	DisplayTitle      string     `json:"display_title,omitempty"`
	DisplayMeta       string     `json:"display_meta,omitempty"`
	TmuxSession       string     `json:"tmux_session,omitempty"`
	Content           string     `json:"content"`
	ChunkIndex        int        `json:"chunk_index"`
	Active            bool       `json:"active"`
	State             string     `json:"state"`
	ClosesAt          *time.Time `json:"closes_at,omitempty"`
	RetentionSeconds  int        `json:"retention_seconds,omitempty"`
	Status            Status     `json:"status"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// Status is a conservative, human-readable summary derived from the raw TUI.
type Status struct {
	ProviderLabel    string  `json:"provider_label,omitempty"`
	StatusText       string  `json:"status_text,omitempty"`
	AssistantPreview string  `json:"assistant_preview,omitempty"`
	ToolSummary      string  `json:"tool_summary,omitempty"`
	ToolName         string  `json:"tool_name,omitempty"`
	ToolCount        int     `json:"tool_count,omitempty"`
	InputTokens      int     `json:"input_tokens,omitempty"`
	OutputTokens     int     `json:"output_tokens,omitempty"`
	CostUSD          float64 `json:"cost_usd,omitempty"`
	DurationMs       int64   `json:"duration_ms,omitempty"`
}

// Context contains higher-level session data used to enrich terminal labels.
type Context struct {
	WorkflowName  string
	WorkflowLabel string
	WorkspacePath string
	ExecutionName string
}

// Store keeps current terminal screens outside durable event history.
type Store struct {
	mu             sync.RWMutex
	byID           map[string]Snapshot
	bySession      map[string]map[string]struct{}
	dismissed      map[string]struct{}
	forcedInactive map[string]time.Time
}

const terminalStaleAfter = 30 * time.Minute

func NewStore() *Store {
	return &Store{
		byID:           make(map[string]Snapshot),
		bySession:      make(map[string]map[string]struct{}),
		dismissed:      make(map[string]struct{}),
		forcedInactive: make(map[string]time.Time),
	}
}

// HandleEvent ingests terminal streaming events emitted by coding-agent adapters.
func (s *Store) HandleEvent(sessionID string, event storeevents.Event) {
	if s == nil {
		return
	}
	sessionID = firstNonEmpty(sessionID, event.SessionID)
	if sessionID == "" {
		return
	}

	switch event.Type {
	case "streaming_chunk":
		content, chunkIndex, metadata, ok := terminalChunk(event)
		if !ok || strings.TrimSpace(content) == "" || !isTerminalMetadata(metadata) {
			return
		}
		s.upsertTerminal(sessionID, event, metadata, content, chunkIndex)
	case "streaming_end":
		metadata := metadataForEvent(event)
		if !isTerminalMetadata(metadata) {
			return
		}
		s.markInactive(sessionID, terminalOwnerID(sessionID, event, metadata), metadata)
	}
}

func (s *Store) List(sessionID string) []Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.pruneExpiredLocked(now)

	var out []Snapshot
	if strings.TrimSpace(sessionID) != "" {
		for terminalID := range s.bySession[sessionID] {
			if snapshot, ok := s.reconcileTerminalStateLocked(terminalID, now); ok {
				out = append(out, snapshot)
			}
		}
	} else {
		out = make([]Snapshot, 0, len(s.byID))
		for terminalID := range s.byID {
			if snapshot, ok := s.reconcileTerminalStateLocked(terminalID, now); ok {
				out = append(out, snapshot)
			}
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Active != out[j].Active {
			return out[i].Active
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

func (s *Store) Get(terminalID string) (Snapshot, bool) {
	terminalID = strings.TrimSpace(terminalID)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.pruneExpiredLocked(now)
	return s.reconcileTerminalStateLocked(terminalID, now)
}

func (s *Store) RemoveSession(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if s == nil || sessionID == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for terminalID := range s.bySession[sessionID] {
		delete(s.byID, terminalID)
		delete(s.forcedInactive, terminalID)
		delete(s.dismissed, terminalID)
	}
	delete(s.bySession, sessionID)
}

func (s *Store) Dismiss(terminalID string) bool {
	terminalID = strings.TrimSpace(terminalID)
	if s == nil || terminalID == "" {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[terminalID]; !ok {
		return false
	}
	s.removeTerminalLocked(terminalID)
	s.dismissed[terminalID] = struct{}{}
	return true
}

// MarkCompleted is an operator override for terminal lifecycle bugs. It marks
// only the view-only terminal snapshot complete; it does not kill tmux or mutate
// workflow execution state.
func (s *Store) MarkCompleted(terminalID string) (Snapshot, bool) {
	return s.markTerminalState(terminalID, "completed")
}

// MarkFailed is an operator override for terminal lifecycle bugs. It marks
// only the view-only terminal snapshot failed; it does not mutate workflow
// execution state.
func (s *Store) MarkFailed(terminalID string) (Snapshot, bool) {
	return s.markTerminalState(terminalID, "failed")
}

func (s *Store) markTerminalState(terminalID, state string) (Snapshot, bool) {
	terminalID = strings.TrimSpace(terminalID)
	if s == nil || terminalID == "" {
		return Snapshot{}, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, ok := s.byID[terminalID]
	if !ok {
		return Snapshot{}, false
	}
	now := time.Now()
	snapshot.Active = false
	snapshot.State = state
	snapshot.ClosesAt = nil
	snapshot.RetentionSeconds = 0
	snapshot.UpdatedAt = now
	s.byID[terminalID] = snapshot
	s.forcedInactive[terminalID] = now
	return snapshot, true
}

// RefreshContent replaces the terminal pane content from an operator-requested
// tmux capture. It keeps manual inactive overrides inactive, but otherwise lets
// the same lifecycle heuristics classify the refreshed pane.
func (s *Store) RefreshContent(terminalID, content string) (Snapshot, bool) {
	terminalID = strings.TrimSpace(terminalID)
	if s == nil || terminalID == "" {
		return Snapshot{}, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, ok := s.byID[terminalID]
	if !ok {
		return Snapshot{}, false
	}
	now := time.Now()
	snapshot.Content = content
	snapshot.ChunkIndex++
	if snapshot.Status.ProviderLabel != "" {
		providerLabel := snapshot.Status.ProviderLabel
		snapshot.Status = DeriveStatus(content, nil)
		if snapshot.Status.ProviderLabel == "" {
			snapshot.Status.ProviderLabel = providerLabel
		}
	} else {
		snapshot.Status = DeriveStatus(content, nil)
	}
	if _, forced := s.forcedInactive[terminalID]; !forced {
		if snapshot.Active {
			snapshot.State = terminalStateFromContent(content, true)
			if boundedTerminalCanSelfComplete(snapshot) && terminalContentLooksIdle(content) {
				snapshot.Active = false
				snapshot.State = terminalStateFromContent(content, false)
			}
		} else if snapshot.State == "stale" {
			snapshot.Active = terminalStateFromContent(content, true) == "running" && !terminalContentLooksIdle(content)
			snapshot.State = terminalStateFromContent(content, snapshot.Active)
		} else if terminalStateFromContent(content, false) == "failed" {
			snapshot.State = "failed"
		}
	}
	snapshot.UpdatedAt = now
	s.byID[terminalID] = snapshot
	return snapshot, true
}

func (s *Store) upsertTerminal(sessionID string, event storeevents.Event, metadata map[string]interface{}, content string, chunkIndex int) {
	ownerID := terminalOwnerID(sessionID, event, metadata)
	terminalID := terminalIDFor(sessionID, ownerID)
	now := event.Timestamp
	if now.IsZero() {
		now = time.Now()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.dismissed[terminalID]; ok {
		return
	}

	current, exists := s.byID[terminalID]
	if forcedAt, forced := s.forcedInactive[terminalID]; forced {
		if !isNewTerminalTurnAfterManualComplete(current, now, chunkIndex, forcedAt) {
			return
		}
		delete(s.forcedInactive, terminalID)
	}
	if exists && chunkIndex < current.ChunkIndex && !isNewTerminalTurn(current, now, chunkIndex) {
		return
	}
	if !exists {
		current = Snapshot{
			TerminalID: terminalID,
			SessionID:  sessionID,
			OwnerID:    ownerID,
			CreatedAt:  now,
		}
	}

	current.ExecutionID = firstNonEmpty(event.ExecutionID, stringValue(metadata, "execution_id"), stringValue(metadata, "execution_owner_id"), ownerID)
	current.ExecutionKind = firstNonEmpty(event.ExecutionKind, stringValue(metadata, "execution_kind"))
	current.Label = terminalLabel(event, metadata, ownerID)
	current.Scope = terminalScope(event, metadata)
	current.WorkflowPath = firstNonEmpty(stringValue(metadata, "workflow_path"), stringValue(metadata, "workspace_path"), stringValue(metadata, "working_directory"))
	current.WorkflowName = firstNonEmpty(stringValue(metadata, "workflow_name"), stringValue(metadata, "workflow_id"), workflowNameFromPath(current.WorkflowPath))
	current.WorkflowLabel = firstNonEmpty(stringValue(metadata, "workflow_label"), stringValue(metadata, "preset_name"), current.WorkflowName)
	current.StepID = firstNonEmpty(workflowStepIDFromOwner(ownerID), stringValue(metadata, "current_step_id"), stringValue(metadata, "workflow_step_id"), stringValue(metadata, "step_id"))
	// step_name (from rich-context push) takes priority over the
	// legacy step_title key; both are accepted for backward compat.
	current.StepName = firstNonEmpty(stringValue(metadata, "step_name"), stringValue(metadata, "step_title"), stringValue(metadata, "current_step_title"))
	current.StepType = firstNonEmpty(stringValue(metadata, "current_step_type"), stringValue(metadata, "workflow_step_type"), stringValue(metadata, "step_type"), stringValue(metadata, "plan_step_type"))
	current.StepIndex = intValue(metadata["step_index"])
	current.StepTotal = intValue(metadata["step_total"])
	current.ParentStepID = stringValue(metadata, "parent_step_id")
	current.StepAttempt = intValue(metadata["step_attempt"])
	current.StepExecutionMode = stringValue(metadata, "step_execution_mode")
	current.StepTransport = stringValue(metadata, "step_transport")
	current.StepTriggeredBy = stringValue(metadata, "step_triggered_by")
	current.AgentName = firstNonEmpty(stringValue(metadata, "agent_name"), stringValue(metadata, "orchestrator_agent_name"))
	current.TmuxSession = firstNonEmpty(
		stringValue(metadata, "tmux_session"),
		stringValue(metadata, "tmux_session_name"),
		stringValue(metadata, "claude_code_interactive_session"),
		stringValue(metadata, "codex_interactive_session"),
		stringValue(metadata, "gemini_interactive_session"),
		stringValue(metadata, "cursor_interactive_session"),
	)
	current.Content = content
	current.ChunkIndex = chunkIndex
	current.Active = true
	current.State = terminalStateFromContent(content, true)
	if boundedTerminalCanSelfComplete(current) && terminalContentLooksIdle(content) {
		current.Active = false
		current.State = terminalStateFromContent(content, false)
	}
	current.ClosesAt = nil
	current.RetentionSeconds = 0
	current.Status = DeriveStatus(content, metadata)
	current.UpdatedAt = now
	fillDisplayContext(&current)

	s.removeTmuxAliasesLocked(sessionID, terminalID, current.TmuxSession)
	s.byID[terminalID] = current
	if _, ok := s.bySession[sessionID]; !ok {
		s.bySession[sessionID] = make(map[string]struct{})
	}
	s.bySession[sessionID][terminalID] = struct{}{}
}

func boundedTerminalCanSelfComplete(snapshot Snapshot) bool {
	if snapshot.ExecutionKind == "main_agent" || snapshot.Scope == "main_agent" || strings.HasPrefix(snapshot.OwnerID, "main:") {
		return false
	}
	return snapshot.ExecutionKind != "" || snapshot.Scope == "workflow_step" || strings.HasPrefix(snapshot.OwnerID, "workflow-step:")
}

func (s *Store) reconcileTerminalStateLocked(terminalID string, now time.Time) (Snapshot, bool) {
	snapshot, ok := s.byID[terminalID]
	if !ok {
		return Snapshot{}, false
	}
	if snapshot.Active && boundedTerminalCanSelfComplete(snapshot) && terminalContentLooksIdle(snapshot.Content) {
		snapshot.Active = false
		snapshot.State = terminalStateFromContent(snapshot.Content, false)
		snapshot.ClosesAt = nil
		snapshot.RetentionSeconds = 0
		snapshot.UpdatedAt = now
		s.byID[terminalID] = snapshot
		return snapshot, true
	}
	if snapshot.Active && boundedTerminalCanSelfComplete(snapshot) && terminalLooksStale(snapshot, now) {
		snapshot.Active = false
		snapshot.State = "stale"
		snapshot.ClosesAt = nil
		snapshot.RetentionSeconds = 0
		s.byID[terminalID] = snapshot
	}
	return snapshot, true
}

func terminalLooksStale(snapshot Snapshot, now time.Time) bool {
	if now.IsZero() {
		now = time.Now()
	}
	lastUpdate := snapshot.UpdatedAt
	if lastUpdate.IsZero() {
		lastUpdate = snapshot.CreatedAt
	}
	if lastUpdate.IsZero() {
		return false
	}
	return now.Sub(lastUpdate) >= terminalStaleAfter
}

func (s *Store) removeTmuxAliasesLocked(sessionID, terminalID, tmuxSession string) {
	tmuxSession = strings.TrimSpace(tmuxSession)
	if tmuxSession == "" {
		return
	}
	for existingID := range s.bySession[sessionID] {
		if existingID == terminalID {
			continue
		}
		existing, ok := s.byID[existingID]
		if !ok || strings.TrimSpace(existing.TmuxSession) != tmuxSession {
			continue
		}
		delete(s.byID, existingID)
		delete(s.forcedInactive, existingID)
		delete(s.bySession[sessionID], existingID)
	}
	if len(s.bySession[sessionID]) == 0 {
		delete(s.bySession, sessionID)
	}
}

func (s *Store) pruneExpiredLocked(now time.Time) {
	if now.IsZero() {
		now = time.Now()
	}
	for terminalID, snapshot := range s.byID {
		if snapshot.Active || snapshot.ClosesAt == nil || now.Before(*snapshot.ClosesAt) {
			continue
		}
		s.removeTerminalLocked(terminalID)
	}
}

func (s *Store) removeTerminalLocked(terminalID string) {
	snapshot, ok := s.byID[terminalID]
	if !ok {
		return
	}
	delete(s.byID, terminalID)
	delete(s.forcedInactive, terminalID)
	if snapshot.SessionID == "" {
		return
	}
	delete(s.bySession[snapshot.SessionID], terminalID)
	if len(s.bySession[snapshot.SessionID]) == 0 {
		delete(s.bySession, snapshot.SessionID)
	}
}

func isNewTerminalTurn(current Snapshot, eventTime time.Time, chunkIndex int) bool {
	if !current.Active {
		return true
	}
	if chunkIndex > 2 {
		return false
	}
	if eventTime.IsZero() || current.UpdatedAt.IsZero() {
		return false
	}
	return eventTime.After(current.UpdatedAt.Add(250 * time.Millisecond))
}

func isNewTerminalTurnAfterManualComplete(current Snapshot, eventTime time.Time, chunkIndex int, forcedAt time.Time) bool {
	if chunkIndex > 2 {
		return false
	}
	if eventTime.IsZero() {
		return false
	}
	if !forcedAt.IsZero() && !eventTime.After(forcedAt.Add(250*time.Millisecond)) {
		return false
	}
	if !current.UpdatedAt.IsZero() && !eventTime.After(current.UpdatedAt.Add(250*time.Millisecond)) {
		return false
	}
	return true
}

// WithContext returns a copy enriched with session-level context. Terminal
// stream metadata wins; session context fills gaps.
func (snapshot Snapshot) WithContext(ctx Context) Snapshot {
	snapshot.WorkflowPath = firstNonEmpty(snapshot.WorkflowPath, ctx.WorkspacePath)
	snapshot.WorkflowName = firstNonEmpty(snapshot.WorkflowName, ctx.WorkflowName, workflowNameFromPath(snapshot.WorkflowPath))
	snapshot.WorkflowLabel = firstNonEmpty(snapshot.WorkflowLabel, ctx.WorkflowLabel, snapshot.WorkflowName)
	if snapshot.Scope == "" && ctx.ExecutionName != "" {
		snapshot.Scope = "session"
	}
	if snapshot.StepName == "" && snapshot.StepID == "" && snapshot.ExecutionKind != "main_agent" {
		snapshot.StepName = ctx.ExecutionName
	}
	if snapshot.AgentName == "" && snapshot.ExecutionKind == "main_agent" {
		snapshot.AgentName = firstNonEmpty(ctx.ExecutionName, "Main agent")
	}
	fillDisplayContext(&snapshot)
	return snapshot
}

func (s *Store) markInactive(sessionID, ownerID string, metadata map[string]interface{}) {
	terminalID := terminalIDFor(sessionID, ownerID)
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, ok := s.byID[terminalID]
	if !ok {
		var resolvedID string
		resolvedID, snapshot, ok = s.findInactiveTargetLocked(sessionID, ownerID, metadata)
		if !ok {
			return
		}
		terminalID = resolvedID
	}
	state := terminalStateFromContent(snapshot.Content, false)
	now := time.Now()
	if state == "running" {
		snapshot.Active = true
		snapshot.State = "running"
		snapshot.ClosesAt = nil
		snapshot.RetentionSeconds = 0
		snapshot.UpdatedAt = now
		s.byID[terminalID] = snapshot
		return
	}
	snapshot.Active = false
	// Retention is caller-decided: whoever emits the streaming events
	// attaches terminal_retention_seconds to metadata when the
	// terminal is transient (workflow step, sub-agent, one-shot CLI
	// invocation, etc.). The store has no opinion — main_agent and
	// any other persistent context simply never set this key.
	retentionSeconds := intValue(metadata["terminal_retention_seconds"])
	if retentionSeconds > 0 {
		closesAt := now.Add(time.Duration(retentionSeconds) * time.Second)
		snapshot.ClosesAt = &closesAt
		snapshot.RetentionSeconds = retentionSeconds
		if state != "failed" {
			state = "closing"
		}
	}
	snapshot.State = state
	snapshot.UpdatedAt = now
	// Surface per-call completion meta (tokens, cost, duration) attached
	// to the streaming_end event. Tmux terminals don't carry these in
	// their pane content (the synthetic [done · ...] line is suppressed
	// to avoid clobbering the pane scrape), so the streaming_end is the
	// only place the structured numbers arrive. Non-tmux transports also
	// benefit — Status fields beat regex-parsing the trailer.
	if in := intValue(metadata["input_tokens"]); in > 0 {
		snapshot.Status.InputTokens = in
	}
	if out := intValue(metadata["output_tokens"]); out > 0 {
		snapshot.Status.OutputTokens = out
	}
	if cost := floatValue(metadata["cost_usd_estimated"]); cost > 0 {
		snapshot.Status.CostUSD = cost
	}
	if dur := int64Value(metadata["duration_ms"]); dur > 0 {
		snapshot.Status.DurationMs = dur
	}
	s.byID[terminalID] = snapshot
}

func (s *Store) findInactiveTargetLocked(sessionID, ownerID string, metadata map[string]interface{}) (string, Snapshot, bool) {
	sessionTerminals := s.bySession[sessionID]
	if len(sessionTerminals) == 0 {
		return "", Snapshot{}, false
	}

	tmuxSession := firstNonEmpty(
		stringValue(metadata, "tmux_session"),
		stringValue(metadata, "tmux_session_name"),
		stringValue(metadata, "claude_code_interactive_session"),
		stringValue(metadata, "codex_interactive_session"),
		stringValue(metadata, "gemini_interactive_session"),
		stringValue(metadata, "cursor_interactive_session"),
	)
	stepID := firstNonEmpty(
		workflowStepIDFromOwner(ownerID),
		stringValue(metadata, "current_step_id"),
		stringValue(metadata, "workflow_step_id"),
		stringValue(metadata, "step_id"),
	)

	if tmuxSession != "" {
		for terminalID := range sessionTerminals {
			snapshot, ok := s.byID[terminalID]
			if ok && snapshot.TmuxSession == tmuxSession {
				return terminalID, snapshot, true
			}
		}
	}

	for terminalID := range sessionTerminals {
		snapshot, ok := s.byID[terminalID]
		if !ok {
			continue
		}
		if ownerMatchesTerminal(ownerID, snapshot) {
			return terminalID, snapshot, true
		}
		if stepID != "" && (snapshot.StepID == stepID || strings.HasSuffix(snapshot.OwnerID, ":"+stepID)) {
			return terminalID, snapshot, true
		}
	}

	return "", Snapshot{}, false
}

func ownerMatchesTerminal(ownerID string, snapshot Snapshot) bool {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return false
	}
	return snapshot.OwnerID == ownerID ||
		snapshot.ExecutionID == ownerID ||
		strings.HasSuffix(snapshot.OwnerID, ":"+ownerID) ||
		strings.HasSuffix(ownerID, ":"+snapshot.OwnerID)
}

func terminalChunk(event storeevents.Event) (string, int, map[string]interface{}, bool) {
	metadata := metadataForEvent(event)
	if event.Data == nil || event.Data.Data == nil {
		return "", 0, metadata, false
	}

	switch data := event.Data.Data.(type) {
	case *agentevents.StreamingChunkEvent:
		metadata = mergeMetadata(metadata, data.Metadata)
		return data.Content, data.ChunkIndex, metadata, true
	case *agentevents.GenericEventData:
		metadata = mergeMetadata(metadata, data.Metadata)
		content, _ := data.Data["content"].(string)
		return content, intValue(data.Data["chunk_index"]), metadata, content != ""
	default:
		return "", 0, metadata, false
	}
}

func metadataForEvent(event storeevents.Event) map[string]interface{} {
	metadata := map[string]interface{}{}
	if event.Data == nil {
		return metadata
	}
	if event.Data.SessionID != "" {
		metadata["session_id"] = event.Data.SessionID
	}
	if event.Data.CorrelationID != "" {
		metadata["correlation_id"] = event.Data.CorrelationID
	}
	switch data := event.Data.Data.(type) {
	case *agentevents.StreamingEndEvent:
		metadata = mergeMetadata(metadata, data.Metadata)
	case *agentevents.GenericEventData:
		metadata = mergeMetadata(metadata, data.Metadata)
		if nested, ok := data.Data["metadata"].(map[string]interface{}); ok {
			metadata = mergeMetadata(metadata, nested)
		}
	}
	return metadata
}

func mergeMetadata(base, extra map[string]interface{}) map[string]interface{} {
	if base == nil {
		base = map[string]interface{}{}
	}
	for key, value := range extra {
		base[key] = value
	}
	return base
}

func isTerminalMetadata(metadata map[string]interface{}) bool {
	kind := strings.ToLower(firstNonEmpty(
		stringValue(metadata, "kind"),
		stringValue(metadata, "stream_kind"),
		stringValue(metadata, "display_kind"),
		stringValue(metadata, "mode"),
	))
	return kind == "terminal" || kind == "tmux" || kind == "tui"
}

func terminalOwnerID(sessionID string, event storeevents.Event, metadata map[string]interface{}) string {
	candidates := []string{
		event.ExecutionID,
		stringValue(metadata, "execution_owner_id"),
		stringValue(metadata, "owner_execution_id"),
		stringValue(metadata, "execution_id"),
		stringValue(metadata, "background_agent_id"),
		stringValue(metadata, "delegation_id"),
		stringValue(metadata, "agent_id"),
		stringValue(metadata, "current_step_id"),
		stringValue(metadata, "workflow_step_id"),
		stringValue(metadata, "step_id"),
		stringValue(metadata, "correlation_id"),
	}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" && candidate != sessionID {
			return candidate
		}
	}
	return ""
}

func terminalIDFor(sessionID, ownerID string) string {
	if ownerID == "" {
		return sessionID
	}
	return fmt.Sprintf("%s:%s", sessionID, ownerID)
}

func workflowStepIDFromOwner(ownerID string) string {
	parts := strings.Split(strings.TrimSpace(ownerID), ":")
	if len(parts) < 3 || parts[0] != "workflow-step" {
		return ""
	}
	return strings.TrimSpace(parts[len(parts)-1])
}

func terminalLabel(event storeevents.Event, metadata map[string]interface{}, ownerID string) string {
	return firstNonEmpty(
		stringValue(metadata, "agent_name"),
		stringValue(metadata, "orchestrator_agent_name"),
		stringValue(metadata, "step_title"),
		stringValue(metadata, "title"),
		stringValue(metadata, "name"),
		stringValue(metadata, "current_step_id"),
		stringValue(metadata, "workflow_step_id"),
		stringValue(metadata, "step_id"),
		event.ExecutionID,
		ownerID,
		"Terminal",
	)
}

func fillDisplayContext(snapshot *Snapshot) {
	if snapshot == nil {
		return
	}
	workflowLabel := firstNonEmpty(snapshot.WorkflowLabel, snapshot.WorkflowName, workflowNameFromPath(snapshot.WorkflowPath))
	kindLabel := executionKindLabel(snapshot.ExecutionKind, snapshot.Scope)
	taskLabel := firstNonEmpty(terminalTaskLabel(*snapshot), cleanOpaqueLabel(snapshot.Label))
	stepTypeLabel := humanizeIdentifier(snapshot.StepType)

	switch {
	case workflowLabel != "" && taskLabel != "" && kindLabel != "":
		snapshot.DisplayTitle = fmt.Sprintf("%s -> %s", workflowLabel, taskLabel)
		snapshot.DisplayMeta = strings.Join(uniqueNonEmpty(stepTypeLabel, kindLabel), " · ")
	case workflowLabel != "" && kindLabel != "":
		snapshot.DisplayTitle = fmt.Sprintf("%s -> %s", workflowLabel, kindLabel)
		snapshot.DisplayMeta = strings.Join(uniqueNonEmpty(stepTypeLabel, taskLabel), " · ")
	case taskLabel != "" && kindLabel != "":
		snapshot.DisplayTitle = fmt.Sprintf("%s -> %s", kindLabel, taskLabel)
		snapshot.DisplayMeta = strings.Join(uniqueNonEmpty(stepTypeLabel, workflowLabel), " · ")
	case taskLabel != "":
		snapshot.DisplayTitle = taskLabel
		snapshot.DisplayMeta = strings.Join(uniqueNonEmpty(stepTypeLabel, firstNonEmpty(workflowLabel, kindLabel)), " · ")
	default:
		snapshot.DisplayTitle = firstNonEmpty(workflowLabel, kindLabel, "Terminal")
		snapshot.DisplayMeta = strings.Join(uniqueNonEmpty(stepTypeLabel, cleanOpaqueLabel(firstNonEmpty(snapshot.ExecutionID, snapshot.OwnerID))), " · ")
	}

	snapshot.DisplayMeta = strings.Join(uniqueNonEmpty(snapshot.DisplayMeta), " · ")
}

func terminalTaskLabel(snapshot Snapshot) string {
	switch firstNonEmpty(snapshot.ExecutionKind, snapshot.Scope) {
	case "workflow_step", "step", "execution_only":
		return firstNonEmpty(snapshot.StepID, snapshot.StepName)
	default:
		return firstNonEmpty(snapshot.StepName, snapshot.AgentName, snapshot.StepID)
	}
}

func executionKindLabel(kind, scope string) string {
	switch firstNonEmpty(kind, scope) {
	case "main_agent":
		return "Main agent"
	case "workflow_step", "step", "execution_only":
		return "Workflow step"
	case "background_agent", "background":
		return "Background agent"
	case "delegation", "todo_task", "sub_agent":
		return "Sub-agent"
	case "session":
		return "Session"
	case "execution":
		return "Execution"
	default:
		return humanizeIdentifier(firstNonEmpty(kind, scope))
	}
}

func workflowNameFromPath(path string) string {
	trimmed := strings.Trim(strings.TrimSpace(path), "/")
	if trimmed == "" {
		return ""
	}
	parts := strings.FieldsFunc(trimmed, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	for i, part := range parts {
		if part == "Workflow" && i+1 < len(parts) {
			return humanizeIdentifier(parts[i+1])
		}
	}
	return humanizeIdentifier(parts[len(parts)-1])
}

func cleanOpaqueLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || looksOpaqueID(value) {
		return ""
	}
	return humanizeIdentifier(value)
}

func looksOpaqueID(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	if lower == "" {
		return false
	}
	if strings.HasPrefix(lower, "main:") && len(lower) > len("main:")+16 {
		return true
	}
	hexish := 0
	for _, r := range lower {
		if (r >= 'a' && r <= 'f') || (r >= '0' && r <= '9') || r == '-' {
			hexish++
		}
	}
	return len(lower) >= 24 && hexish == len(lower)
}

func humanizeIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.TrimPrefix(value, "exec-")
	value = strings.TrimPrefix(value, "exec_")
	value = strings.ReplaceAll(value, "_", " ")
	value = strings.ReplaceAll(value, "-", " ")
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return ""
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func uniqueNonEmpty(values ...string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func terminalScope(event storeevents.Event, metadata map[string]interface{}) string {
	if scope := stringValue(metadata, "scope"); scope != "" {
		return scope
	}
	switch event.ExecutionKind {
	case "workflow_step", "step", "execution_only":
		return "workflow_step"
	case "background_agent", "background":
		return "background_agent"
	case "delegation", "todo_task", "sub_agent":
		return "delegation"
	}
	if terminalOwnerID(event.SessionID, event, metadata) == "" {
		return "session"
	}
	return "execution"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func stringValue(values map[string]interface{}, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func floatValue(value interface{}) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case string:
		parsed, _ := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed
	default:
		return 0
	}
}

func int64Value(value interface{}) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	case float32:
		return int64(typed)
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed
	default:
		return 0
	}
}

func intValue(value interface{}) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(typed))
		return parsed
	default:
		return 0
	}
}
