package terminals

import (
	"encoding/json"
	"fmt"
	"regexp"
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
	StepExecutionMode string     `json:"step_execution_mode,omitempty"` // "scripted" | "agentic" (legacy: "learn_code" | "code_exec")
	StepTransport     string     `json:"step_transport,omitempty"`      // "tmux" | "api" | legacy labels
	StepTriggeredBy   string     `json:"step_triggered_by,omitempty"`   // e.g., "workflow_executor", "parent_step:X"
	AgentName         string     `json:"agent_name,omitempty"`
	DisplayTitle      string     `json:"display_title,omitempty"`
	DisplayMeta       string     `json:"display_meta,omitempty"`
	TmuxSession       string     `json:"tmux_session,omitempty"`
	Content           string     `json:"content"`
	ContentSource     string     `json:"content_source,omitempty"`
	Rows              []Row      `json:"rows"`
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
	ProviderLabel            string  `json:"provider_label,omitempty"`
	StatusText               string  `json:"status_text,omitempty"`
	AssistantPreview         string  `json:"assistant_preview,omitempty"`
	ToolSummary              string  `json:"tool_summary,omitempty"`
	ToolName                 string  `json:"tool_name,omitempty"`
	ToolCount                int     `json:"tool_count,omitempty"`
	InputTokens              int     `json:"input_tokens,omitempty"`
	OutputTokens             int     `json:"output_tokens,omitempty"`
	CacheCreationInputTokens int     `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int     `json:"cache_read_input_tokens,omitempty"`
	TotalInputTokens         int     `json:"total_input_tokens,omitempty"`
	TotalOutputTokens        int     `json:"total_output_tokens,omitempty"`
	CostUSD                  float64 `json:"cost_usd,omitempty"`
	// StatusMeta carries raw provider statusline extras that don't have a
	// first-class field (context window, git branch, rate limits, …) so the UI
	// can surface them without the store needing to know each provider's schema.
	StatusMeta                map[string]interface{} `json:"status_meta,omitempty"`
	DurationMs                int64                  `json:"duration_ms,omitempty"`
	PreValidationStatus       string                 `json:"pre_validation_status,omitempty"`
	PreValidationSummary      string                 `json:"pre_validation_summary,omitempty"`
	PreValidationPassedChecks int                    `json:"pre_validation_passed_checks,omitempty"`
	PreValidationFailedChecks int                    `json:"pre_validation_failed_checks,omitempty"`
	PreValidationTotalChecks  int                    `json:"pre_validation_total_checks,omitempty"`
	// RateLimited is set when the rendered pane content matches any
	// known provider rate-limit / quota-exhausted message (see
	// rate_limit.go). The terminal stays "running" from the store's
	// perspective because the tmux pane is still alive, but the
	// frontend can surface a distinct badge so the user knows the
	// underlying work is blocked, not making progress.
	RateLimited bool `json:"rate_limited,omitempty"`
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
	toolLines      map[string]*terminalToolLines
}

const terminalInactiveAfter = 2 * time.Minute
const terminalPromptCompletionInactiveAfter = time.Minute
const terminalToolTextMaxRunes = 2400

var (
	regexpMCPToken      = regexp.MustCompile(`(?i)(MCP_API_TOKEN=)[^\s"'\\]+`)
	regexpSensitiveEnv  = regexp.MustCompile(`(?i)\b([A-Z0-9_]*(?:API_KEY|TOKEN|SECRET)=)[^\s"'\\]+`)
	regexpBearerToken   = regexp.MustCompile(`(?i)(Authorization:\s*Bearer\s+)[^\s"'\\]+`)
	regexpSecretEnv     = regexp.MustCompile(`(?m)(SECRET_[A-Z0-9_]+=)[^\s"'\\]+`)
	regexpProviderSKKey = regexp.MustCompile(`\bsk-[A-Za-z0-9][A-Za-z0-9_-]{10,}\b`)
	regexpGoogleAPIKey  = regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{20,}\b`)
)

type terminalToolLines struct {
	order []string
	items map[string]*terminalToolLine
}

type terminalToolLine struct {
	name         string
	args         string
	result       string
	resultPrefix string
}

func NewStore() *Store {
	return &Store{
		byID:           make(map[string]Snapshot),
		bySession:      make(map[string]map[string]struct{}),
		dismissed:      make(map[string]struct{}),
		forcedInactive: make(map[string]time.Time),
		toolLines:      make(map[string]*terminalToolLines),
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
	case string(agentevents.ToolCallStart), string(agentevents.ToolCallEnd), string(agentevents.ToolCallError):
		metadata := metadataForEvent(event)
		if !isNonTmuxWorkflowTerminalMetadata(metadata) {
			return
		}
		s.upsertToolLine(sessionID, event, metadata)
	case "pre_validation_completed":
		s.updatePreValidationStatus(sessionID, event)
	case "status_line":
		s.handleStatusLine(sessionID, event)
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
	out = dedupeCurrentMainAgentSnapshots(out)

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Active != out[j].Active {
			return out[i].Active
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

// ListMetadata returns current snapshots without content-dependent reconciliation.
// It is used by high-frequency UI rail polls that only need identity, state,
// timestamps, and compact status fields. Avoiding content scans here keeps large
// streaming tmux panes from blocking the terminal list endpoint.
func (s *Store) ListMetadata(sessionID string) []Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.pruneExpiredLocked(now)

	var out []Snapshot
	if strings.TrimSpace(sessionID) != "" {
		for terminalID := range s.bySession[sessionID] {
			if snapshot, ok := s.byID[terminalID]; ok {
				out = append(out, snapshot)
			}
		}
	} else {
		out = make([]Snapshot, 0, len(s.byID))
		for _, snapshot := range s.byID {
			out = append(out, snapshot)
		}
	}
	out = dedupeCurrentMainAgentSnapshots(out)

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Active != out[j].Active {
			return out[i].Active
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

// SessionHasBusyCodingTmux reports whether the session has a coding-agent tmux
// terminal whose pane currently looks busy (actively processing). This lets a
// resumed/launch-only coding agent — which has no server-managed foreground
// turn — still accept live "steer" input while it's mid-task. Content is the
// last-refreshed pane snapshot (the frontend probe refreshes inactive tmux
// terminals every few seconds), so this is an in-memory read, no tmux capture.
func (s *Store) SessionHasBusyCodingTmux(sessionID string) bool {
	if s == nil {
		return false
	}
	for _, snapshot := range s.List(sessionID) {
		if strings.TrimSpace(snapshot.TmuxSession) == "" {
			continue
		}
		// Only a LIVE terminal can be busy. A completed/exited/stale pane (Active=false,
		// set by MarkCompleted/markTerminalState/MarkStale) keeps its last-captured
		// content, which for a coding-agent (codex) that exited mid-spinner still shows
		// a "Working…"/"esc to interrupt" line — but it is no longer processing. Counting
		// that stale snapshot as busy keeps the session "steerable"/running forever:
		// session_status never flips to completed, the chat's per-tab isStreaming stays
		// stuck true, and the user's next message routes to live-input on the dead pane
		// (silently lost / stranded) instead of starting a new /api/query turn. Skipping
		// non-Active terminals lets the session complete so follow-up turns submit.
		if !snapshot.Active {
			continue
		}
		if terminalContentLooksBusy(snapshot.Content) {
			return true
		}
	}
	return false
}

// SessionHasRetainedCodingTmux reports whether this session still has a live
// tmux-backed coding-agent pane. This intentionally does not inspect "busy"
// text: an idle retained CLI can still hold agent context, accept the next user
// message, and be terminated by New Chat.
func (s *Store) SessionHasRetainedCodingTmux(sessionID string) bool {
	sessionID = strings.TrimSpace(sessionID)
	if s == nil || sessionID == "" {
		return false
	}
	for _, snapshot := range s.List(sessionID) {
		if !snapshot.Active {
			continue
		}
		if strings.TrimSpace(snapshot.TmuxSession) != "" {
			return true
		}
	}
	return false
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
		delete(s.toolLines, terminalID)
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

// MarkStale flags a terminal whose backing tmux session has disappeared without
// a lifecycle completion event. Unlike MarkCompleted/MarkFailed it does not set
// a forcedInactive override, so a later successful capture can still reclassify
// the pane through RefreshContent's stale-recovery path. It is idempotent so the
// frontend's inactive-terminal probe can stop once the snapshot reads stale.
//
// The backing TmuxSession is also cleared so downstream handlers that act on
// the live pane (resize-window, send-keys, paste-buffer) short-circuit at their
// "no live pane" branch and return OK instead of hitting tmux and bubbling up a
// "can't find session" failure as a 502. The trade-off: a transient tmux hiccup
// that would previously self-heal via the recovery path now requires the
// terminal to be re-attached. In practice a dead session name does not come
// back, so this is the right default.
func (s *Store) MarkStale(terminalID string) (Snapshot, bool) {
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
	if snapshot.State == "stale" && !snapshot.Active && strings.TrimSpace(snapshot.TmuxSession) == "" {
		return snapshot, true
	}
	now := time.Now()
	snapshot.Active = false
	snapshot.State = "stale"
	snapshot.TmuxSession = ""
	snapshot.UpdatedAt = now
	s.byID[terminalID] = snapshot
	return snapshot, true
}

// UpsertStaticSnapshot publishes a persisted terminal buffer as a read-only
// snapshot for a new/restored UI session. It intentionally clears TmuxSession:
// this snapshot is only the last rendered pane, not a live tmux target.
func (s *Store) UpsertStaticSnapshot(sessionID string, snapshot Snapshot) (Snapshot, bool) {
	sessionID = strings.TrimSpace(sessionID)
	if s == nil || sessionID == "" || strings.TrimSpace(snapshot.Content) == "" {
		return Snapshot{}, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	ownerID := strings.TrimSpace(snapshot.OwnerID)
	if ownerID == "" || strings.HasPrefix(ownerID, "main:") || currentTerminalIsMainAgent(snapshot) {
		ownerID = "main:" + sessionID
	}
	terminalID := terminalIDFor(sessionID, ownerID)
	// Never clobber a LIVE tmux terminal with the static buffer. Once a resumed
	// session's transport has been materialized under this canonical terminalID
	// (Active + a real TmuxSession), a late static re-publish — the frontend's
	// restore-terminal POST races the /api/query re-launch that materializes the
	// live pane — must NOT reset it back to Active:false / TmuxSession:"". Doing so
	// strips the tmux_session the frontend needs to fire /resize, so tmux geometry
	// never matches the xterm and pi-cli's full-screen redraws append instead of
	// overwrite (duplicated status bar / stacked "Working..."). The live snapshot
	// already shows current content, so the static buffer adds nothing — keep live.
	if existing, ok := s.byID[terminalID]; ok && existing.Active && strings.TrimSpace(existing.TmuxSession) != "" {
		return existing, true
	}
	snapshot.TerminalID = terminalID
	snapshot.SessionID = sessionID
	snapshot.OwnerID = ownerID
	snapshot.TmuxSession = ""
	snapshot.Active = false
	if strings.TrimSpace(snapshot.State) == "" || snapshot.State == "running" || snapshot.State == "closing" {
		snapshot.State = "stale"
	}
	snapshot.ClosesAt = nil
	snapshot.RetentionSeconds = 0
	if snapshot.ExecutionKind == "" {
		snapshot.ExecutionKind = "main_agent"
	}
	if snapshot.Scope == "" {
		snapshot.Scope = "main_agent"
	}
	if snapshot.StepID == "" && currentTerminalIsMainAgent(snapshot) {
		snapshot.StepID = "main_agent:" + sessionID
	}
	if snapshot.StepTransport == "" {
		snapshot.StepTransport = "tmux"
	}
	if snapshot.ContentSource == "" {
		snapshot.ContentSource = "tmux_capture"
	}
	if snapshot.CreatedAt.IsZero() {
		snapshot.CreatedAt = now
	}
	snapshot.UpdatedAt = now
	snapshot.Rows = nil
	previousStatus := snapshot.Status
	snapshot.Status = DeriveStatus(snapshot.Content, nil)
	preserveEphemeralStatusFields(&snapshot.Status, previousStatus)
	fillDisplayContext(&snapshot)

	if _, ok := s.dismissed[terminalID]; ok {
		delete(s.dismissed, terminalID)
	}
	s.byID[terminalID] = snapshot
	if s.bySession[sessionID] == nil {
		s.bySession[sessionID] = make(map[string]struct{})
	}
	s.bySession[sessionID][terminalID] = struct{}{}
	if currentTerminalIsMainAgent(snapshot) {
		s.removeCurrentMainAgentAliasesLocked(sessionID, terminalID)
	}
	return snapshot, true
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

// RefreshContent stores the latest terminal pane content from an
// operator-requested tmux capture. It keeps manual inactive overrides inactive,
// but otherwise lets the same lifecycle heuristics classify the refreshed pane.
// It simply replaces the stored content (no snapshot accumulation).
func (s *Store) RefreshContent(terminalID, content string) (Snapshot, bool) {
	return s.RefreshContentWithSource(terminalID, content, "")
}

// RefreshContentWithSource is RefreshContent plus a display-source marker used
// by the UI to keep retained tmux snapshots on the xterm rendering path.
func (s *Store) RefreshContentWithSource(terminalID, content, contentSource string) (Snapshot, bool) {
	return s.refreshContent(terminalID, content, contentSource)
}

// ReplaceContent refreshes a terminal pane with an authoritative capture,
// storing the latest content. It is identical to RefreshContent.
func (s *Store) ReplaceContent(terminalID, content string) (Snapshot, bool) {
	return s.ReplaceContentWithSource(terminalID, content, "")
}

// ReplaceContentWithSource is ReplaceContent plus a display-source marker used
// by the UI to keep retained tmux snapshots on the xterm rendering path.
func (s *Store) ReplaceContentWithSource(terminalID, content, contentSource string) (Snapshot, bool) {
	return s.refreshContent(terminalID, content, contentSource)
}

func (s *Store) SetDisplayContent(terminalID, content, contentSource string) (Snapshot, bool) {
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
	contentSource = strings.TrimSpace(contentSource)
	contentChanged := snapshot.Content != content
	sourceChanged := contentSource != "" && snapshot.ContentSource != contentSource
	if contentChanged {
		snapshot.Content = content
		snapshot.Rows = nil
	}
	if sourceChanged {
		snapshot.ContentSource = contentSource
	}
	if contentChanged || sourceChanged {
		snapshot.ChunkIndex++
		snapshot.UpdatedAt = now
	}
	s.byID[terminalID] = snapshot
	return snapshot, true
}

func (s *Store) refreshContent(terminalID, content, contentSource string) (Snapshot, bool) {
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
	lifecycleContent := content
	contentChanged := snapshot.Content != content
	snapshot.Content = content
	if contentChanged {
		snapshot.ChunkIndex++
	}
	if contentSource = strings.TrimSpace(contentSource); contentSource != "" {
		snapshot.ContentSource = contentSource
	}
	previousStatus := snapshot.Status
	snapshot.Status = DeriveStatus(lifecycleContent, nil)
	preserveEphemeralStatusFields(&snapshot.Status, previousStatus)
	if _, forced := s.forcedInactive[terminalID]; !forced {
		if snapshot.Active {
			if terminalCanCompleteFromCapturedIdle(snapshot) && terminalContentLooksIdle(lifecycleContent) {
				snapshot.Active = false
				snapshot.State = terminalStateFromContent(lifecycleContent, false)
				snapshot.ClosesAt = nil
				snapshot.RetentionSeconds = 0
			} else {
				snapshot.State = terminalStateFromContent(lifecycleContent, true)
			}
		} else if snapshot.State == "stale" {
			snapshot.Active = terminalStateFromContent(lifecycleContent, true) == "running" && !terminalContentLooksIdle(lifecycleContent)
			snapshot.State = terminalStateFromContent(lifecycleContent, snapshot.Active)
		} else if contentChanged && snapshot.State == "completed" && terminalContentLooksBusy(lifecycleContent) {
			// tmux session restarted inside the same terminal (e.g. Claude Code
			// context compaction: /exit then a fresh process in the same pane).
			// The content changed and now looks busy — re-activate so the UI
			// picks up the live output again.
			snapshot.Active = true
			snapshot.State = "running"
		} else if terminalStateFromContent(lifecycleContent, false) == "failed" {
			snapshot.State = "failed"
		}
	} else if contentChanged && snapshot.State == "completed" && terminalContentLooksBusy(lifecycleContent) {
		// Even force-completed terminals should restart when the tmux pane shows a
		// new process running (e.g. after Claude Code compaction). Clear the
		// forcedInactive entry so the terminal can participate in normal lifecycle.
		delete(s.forcedInactive, terminalID)
		snapshot.Active = true
		snapshot.State = "running"
	}
	if contentChanged || snapshot.UpdatedAt.IsZero() {
		snapshot.UpdatedAt = now
	}
	s.byID[terminalID] = snapshot
	return snapshot, true
}

func terminalCanCompleteFromCapturedIdle(snapshot Snapshot) bool {
	if !terminalUsesIdleTimeout(snapshot) || !boundedTerminalCanSelfComplete(snapshot) {
		return false
	}
	if snapshot.ExecutionKind == "main_agent" || strings.HasPrefix(snapshot.OwnerID, "main:") {
		return false
	}
	return snapshot.ExecutionKind == "workflow_step" ||
		snapshot.Scope == "workflow_step" ||
		strings.HasPrefix(snapshot.OwnerID, "workflow-step:")
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
	inactiveNewTurn := exists && !current.Active && isNewTerminalTurn(current, now, chunkIndex)
	sameTurnExtension := exists && chunkIndex < current.ChunkIndex && terminalContentExtends(current.Content, content)
	chunkIndexResetTurn := exists && current.Active && !sameTurnExtension && isChunkIndexResetTerminalTurn(current, content, chunkIndex)
	if exists && chunkIndex < current.ChunkIndex && !inactiveNewTurn && !chunkIndexResetTurn && !sameTurnExtension {
		return
	}
	freshTurn := inactiveNewTurn || chunkIndexResetTurn
	// Rerun: a new turn has arrived for an owner whose previous turn
	// already completed. Archive the existing entry under a derived
	// terminalID so the read-only snapshot of the prior run stays in
	// the rail, then drop through to create a fresh entry at the
	// canonical terminalID for the new live turn. Skips when the
	// current entry is empty (no real content yet) — that's just a
	// pre-stream placeholder, not a finished run worth archiving.
	if exists && freshTurn && strings.TrimSpace(current.Content) != "" && shouldArchiveTerminalTurn(current) {
		archived := current
		archived.TerminalID = fmt.Sprintf("%s:turn-%d", terminalID, current.CreatedAt.UnixNano())
		archived.Active = false
		s.byID[archived.TerminalID] = archived
		if s.bySession[sessionID] == nil {
			s.bySession[sessionID] = make(map[string]struct{})
		}
		s.bySession[sessionID][archived.TerminalID] = struct{}{}
		// Force the canonical ID to be repopulated from scratch below.
		exists = false
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
	current.StepID = firstNonEmpty(workflowStepIDFromOwner(ownerID), stringValue(metadata, "current_step_id"), stringValue(metadata, "workflow_step_id"), stringValue(metadata, "step_id"), current.StepID)
	// Main-agent terminals have no natural step_id, but the rail tree
	// needs a stable identifier so child terminals (workshop backgrounds
	// spawned by the main agent via run_full_workflow / run_step / etc.)
	// can point to it as their parent. Synthesize one per session — the
	// rail builder uses this value as a parent key, not for display.
	if current.StepID == "" && current.ExecutionKind == "main_agent" {
		current.StepID = "main_agent:" + sessionID
	}
	// step_name (from rich-context push) takes priority over the
	// legacy step_title key; both are accepted for backward compat.
	current.StepName = firstNonEmpty(stringValue(metadata, "step_name"), stringValue(metadata, "step_title"), stringValue(metadata, "current_step_title"), current.StepName)
	current.StepType = firstNonEmpty(stringValue(metadata, "plan_step_type"), stringValue(metadata, "workflow_step_type"), stringValue(metadata, "current_step_type"), stringValue(metadata, "step_type"), current.StepType)
	if stepIndex := intValue(metadata["step_index"]); stepIndex > 0 {
		current.StepIndex = stepIndex
	}
	if stepTotal := intValue(metadata["step_total"]); stepTotal > 0 {
		current.StepTotal = stepTotal
	}
	current.ParentStepID = firstNonEmpty(stringValue(metadata, "parent_step_id"), current.ParentStepID)
	// Default rooting: a terminal with no parent_step_id and not itself
	// the main agent gets implicitly parented to the main-agent
	// terminal of this session. The rail's buildTree only nests when
	// the parent step_id matches another terminal in the list, so this
	// is self-correcting: if no main_agent terminal exists for this
	// session, the synthetic parent_step_id won't resolve and the
	// terminal just renders at root anyway. Workshop backgrounds
	// spawned via run_full_workflow / run_step now hang off the main
	// agent in the rail.
	if current.ParentStepID == "" && current.ExecutionKind != "main_agent" {
		current.ParentStepID = "main_agent:" + sessionID
	}
	if stepAttempt := intValue(metadata["step_attempt"]); stepAttempt > 0 {
		current.StepAttempt = stepAttempt
	}
	current.StepExecutionMode = firstNonEmpty(stringValue(metadata, "step_execution_mode"), current.StepExecutionMode)
	current.StepTransport = firstNonEmpty(stringValue(metadata, "step_transport"), current.StepTransport)
	current.StepTriggeredBy = firstNonEmpty(stringValue(metadata, "step_triggered_by"), current.StepTriggeredBy)
	current.AgentName = firstNonEmpty(stringValue(metadata, "agent_name"), stringValue(metadata, "orchestrator_agent_name"), current.AgentName)
	current.TmuxSession = firstNonEmpty(
		stringValue(metadata, "tmux_session"),
		stringValue(metadata, "tmux_session_name"),
		stringValue(metadata, "claude_code_interactive_session"),
		stringValue(metadata, "codex_interactive_session"),
		stringValue(metadata, "gemini_interactive_session"),
		stringValue(metadata, "cursor_interactive_session"),
		current.TmuxSession,
	)
	content = s.contentWithToolLinesLocked(terminalID, content)
	lifecycleContent := content
	contentChanged := !exists || current.Content != content
	current.Content = content
	if rows := terminalRowsFromMetadata(metadata); len(rows) > 0 {
		current.Rows = rows
	} else if contentChanged {
		current.Rows = nil
	}
	if sameTurnExtension && chunkIndex < current.ChunkIndex {
		chunkIndex = current.ChunkIndex
	}
	current.ChunkIndex = chunkIndex
	current.Active = true
	current.State = terminalStateFromContent(lifecycleContent, true)
	current.ClosesAt = nil
	current.RetentionSeconds = 0
	previousStatus := current.Status
	current.Status = DeriveStatus(lifecycleContent, metadata)
	preserveEphemeralStatusFields(&current.Status, previousStatus)
	if contentChanged || current.UpdatedAt.IsZero() {
		current.UpdatedAt = now
	}
	fillDisplayContext(&current)

	s.removeTmuxAliasesLocked(sessionID, terminalID, current.TmuxSession)
	s.byID[terminalID] = current
	if _, ok := s.bySession[sessionID]; !ok {
		s.bySession[sessionID] = make(map[string]struct{})
	}
	s.bySession[sessionID][terminalID] = struct{}{}
	if currentTerminalIsMainAgent(current) {
		s.removeCurrentMainAgentAliasesLocked(sessionID, terminalID)
	}
}

func boundedTerminalCanSelfComplete(snapshot Snapshot) bool {
	return snapshot.ExecutionKind != "" ||
		snapshot.Scope != "" ||
		strings.HasPrefix(snapshot.OwnerID, "main:") ||
		strings.HasPrefix(snapshot.OwnerID, "workflow-step:")
}

func terminalUsesIdleTimeout(snapshot Snapshot) bool {
	return strings.EqualFold(strings.TrimSpace(snapshot.StepTransport), "tmux") ||
		strings.TrimSpace(snapshot.TmuxSession) != ""
}

func (s *Store) upsertToolLine(sessionID string, event storeevents.Event, metadata map[string]interface{}) {
	if event.Data == nil || event.Data.Data == nil {
		return
	}
	ownerID := terminalOwnerID(sessionID, event, metadata)
	terminalID := terminalIDFor(sessionID, ownerID)
	if terminalID == "" {
		return
	}
	now := event.Timestamp
	if now.IsZero() {
		now = time.Now()
	}

	var toolCallID, toolName, args, result, resultPrefix string
	switch data := event.Data.Data.(type) {
	case *agentevents.ToolCallStartEvent:
		toolCallID = data.ToolCallID
		toolName = data.ToolName
		args = data.ToolParams.Arguments
	case *agentevents.ToolCallEndEvent:
		toolCallID = data.ToolCallID
		toolName = data.ToolName
		result = data.Result
		resultPrefix = "✓"
	case *agentevents.ToolCallErrorEvent:
		toolCallID = data.ToolCallID
		toolName = data.ToolName
		result = data.Error
		resultPrefix = "✗"
	default:
		return
	}
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		toolName = "tool"
	}
	toolCallID = strings.TrimSpace(toolCallID)
	if toolCallID == "" {
		toolCallID = fmt.Sprintf("%s:%d", toolName, now.UnixNano())
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.dismissed[terminalID]; ok {
		return
	}
	lines := s.toolLines[terminalID]
	if lines == nil {
		lines = &terminalToolLines{items: make(map[string]*terminalToolLine)}
		s.toolLines[terminalID] = lines
	}
	item := lines.items[toolCallID]
	if item == nil {
		item = &terminalToolLine{}
		lines.items[toolCallID] = item
		lines.order = append(lines.order, toolCallID)
	}
	item.name = firstNonEmpty(toolName, item.name)
	if args != "" {
		item.args = redactTerminalToolText(args)
	}
	if result != "" {
		item.result = redactTerminalToolText(result)
		item.resultPrefix = resultPrefix
	}

	snapshot, ok := s.byID[terminalID]
	if !ok {
		return
	}
	snapshot.Content = s.contentWithToolLinesLocked(terminalID, snapshot.Content)
	previousStatus := snapshot.Status
	snapshot.Status = DeriveStatus(snapshot.Content, metadata)
	preserveEphemeralStatusFields(&snapshot.Status, previousStatus)
	snapshot.UpdatedAt = now
	s.byID[terminalID] = snapshot
}

func (s *Store) contentWithToolLinesLocked(terminalID, content string) string {
	lines := s.toolLines[terminalID]
	if lines == nil || len(lines.order) == 0 {
		return content
	}

	base, doneFooter := splitTerminalDoneFooter(stripTerminalToolLines(content))
	var b strings.Builder
	b.WriteString(strings.TrimRight(base, "\n"))
	b.WriteString("\n")
	for _, id := range lines.order {
		item := lines.items[id]
		if item == nil {
			continue
		}
		name := firstNonEmpty(item.name, "tool")
		fmt.Fprintf(&b, "→ tool: %s(%s)\n", name, truncateTerminalToolText(item.args))
		if item.result != "" {
			prefix := firstNonEmpty(item.resultPrefix, "✓")
			fmt.Fprintf(&b, "%s result %s: %s\n", prefix, name, truncateTerminalToolText(item.result))
		}
	}
	if doneFooter != "" {
		b.WriteString(strings.TrimLeft(doneFooter, "\n"))
	}
	return b.String()
}

func splitTerminalDoneFooter(content string) (string, string) {
	trimmed := strings.TrimRight(content, "\n")
	if strings.HasPrefix(trimmed, "[done") {
		return "", trimmed + "\n"
	}
	if idx := strings.LastIndex(trimmed, "\n[done"); idx >= 0 {
		return trimmed[:idx], trimmed[idx+1:] + "\n"
	}
	return content, ""
}

func stripTerminalToolLines(content string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(line, "→ tool: ") || strings.HasPrefix(line, "✓ result ") || strings.HasPrefix(line, "✗ result ") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func redactTerminalToolText(value string) string {
	return RedactSensitiveTerminalText(value)
}

// RedactSensitiveTerminalText removes common secret shapes before terminal text
// is surfaced outside the terminal package.
func RedactSensitiveTerminalText(value string) string {
	value = regexpMCPToken.ReplaceAllString(value, "$1[redacted]")
	value = regexpSensitiveEnv.ReplaceAllString(value, "$1[redacted]")
	value = regexpBearerToken.ReplaceAllString(value, "$1[redacted]")
	value = regexpSecretEnv.ReplaceAllString(value, "$1[redacted]")
	value = regexpProviderSKKey.ReplaceAllString(value, "sk-[redacted]")
	value = regexpGoogleAPIKey.ReplaceAllString(value, "AIza[redacted]")
	return value
}

func truncateTerminalToolText(value string) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= terminalToolTextMaxRunes {
		return value
	}
	return string(runes[:terminalToolTextMaxRunes]) + "... [truncated]"
}

func terminalRowsFromMetadata(metadata map[string]interface{}) []Row {
	if len(metadata) == 0 {
		return nil
	}
	raw, ok := metadata["rows"]
	if !ok || raw == nil {
		raw, ok = metadata["terminal_rows"]
	}
	if !ok || raw == nil {
		return nil
	}
	switch rows := raw.(type) {
	case []Row:
		return cloneTerminalRows(rows)
	case []map[string]interface{}:
		return terminalRowsFromMaps(rows)
	case []interface{}:
		return terminalRowsFromInterfaces(rows)
	default:
		data, err := json.Marshal(raw)
		if err != nil {
			return nil
		}
		var parsed []Row
		if err := json.Unmarshal(data, &parsed); err != nil {
			return nil
		}
		return cloneTerminalRows(parsed)
	}
}

func terminalRowsFromMaps(items []map[string]interface{}) []Row {
	rows := make([]Row, 0, len(items))
	for _, item := range items {
		row := Row{
			Kind:         stringValue(item, "kind"),
			Text:         stringValue(item, "text"),
			Name:         stringValue(item, "name"),
			Args:         stringValue(item, "args"),
			Result:       stringValue(item, "result"),
			ResultPrefix: stringValue(item, "result_prefix"),
		}
		if row.Kind != "" {
			rows = append(rows, row)
		}
	}
	return rows
}

func terminalRowsFromInterfaces(items []interface{}) []Row {
	rows := make([]Row, 0, len(items))
	for _, item := range items {
		switch value := item.(type) {
		case Row:
			rows = append(rows, value)
		case map[string]interface{}:
			rows = append(rows, terminalRowsFromMaps([]map[string]interface{}{value})...)
		default:
			data, err := json.Marshal(value)
			if err != nil {
				continue
			}
			var row Row
			if err := json.Unmarshal(data, &row); err != nil {
				continue
			}
			if row.Kind != "" {
				rows = append(rows, row)
			}
		}
	}
	return rows
}

func cloneTerminalRows(rows []Row) []Row {
	out := make([]Row, 0, len(rows))
	for _, row := range rows {
		if row.Kind == "" {
			continue
		}
		out = append(out, row)
	}
	return out
}

func (s *Store) reconcileTerminalStateLocked(terminalID string, now time.Time) (Snapshot, bool) {
	snapshot, ok := s.byID[terminalID]
	if !ok {
		return Snapshot{}, false
	}
	if snapshot.Active &&
		terminalUsesIdleTimeout(snapshot) &&
		boundedTerminalCanSelfComplete(snapshot) &&
		terminalHasPromptCompletionFallback(snapshot.Content) &&
		terminalLooksInactiveAfter(snapshot, now, terminalPromptCompletionInactiveAfter) {
		snapshot.Active = false
		snapshot.State = "completed"
		snapshot.ClosesAt = nil
		snapshot.RetentionSeconds = 0
		s.byID[terminalID] = snapshot
		return snapshot, true
	}
	if snapshot.Active &&
		terminalUsesIdleTimeout(snapshot) &&
		boundedTerminalCanSelfComplete(snapshot) &&
		terminalLooksInactive(snapshot, now) {
		snapshot.Active = false
		snapshot.State = "completed"
		snapshot.ClosesAt = nil
		snapshot.RetentionSeconds = 0
		s.byID[terminalID] = snapshot
	}
	return snapshot, true
}

func terminalLooksInactive(snapshot Snapshot, now time.Time) bool {
	return terminalLooksInactiveAfter(snapshot, now, terminalInactiveAfter)
}

func terminalLooksInactiveAfter(snapshot Snapshot, now time.Time, threshold time.Duration) bool {
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
	return now.Sub(lastUpdate) >= threshold
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
		if strings.Contains(existingID, ":turn-") {
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

func (s *Store) removeCurrentMainAgentAliasesLocked(sessionID, keepTerminalID string) {
	for terminalID := range s.bySession[sessionID] {
		if terminalID == keepTerminalID || strings.Contains(terminalID, ":turn-") {
			continue
		}
		snapshot, ok := s.byID[terminalID]
		if !ok || !currentTerminalIsMainAgent(snapshot) {
			continue
		}
		s.removeTerminalLocked(terminalID)
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
	delete(s.toolLines, terminalID)
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

func isChunkIndexResetTerminalTurn(current Snapshot, content string, chunkIndex int) bool {
	if current.ChunkIndex <= 2 || chunkIndex > 2 {
		return false
	}
	if strings.TrimSpace(content) == "" || content == current.Content {
		return false
	}
	return true
}

func shouldArchiveTerminalTurn(current Snapshot) bool {
	if currentTerminalIsMainAgent(current) && strings.TrimSpace(current.TmuxSession) != "" {
		return false
	}
	return true
}

func terminalContentExtends(existing, next string) bool {
	existing = strings.TrimRight(existing, "\n")
	next = strings.TrimRight(next, "\n")
	if existing == "" || next == "" || existing == next {
		return false
	}
	return strings.HasPrefix(next, existing)
}

func dedupeCurrentMainAgentSnapshots(snapshots []Snapshot) []Snapshot {
	if len(snapshots) <= 1 {
		return snapshots
	}
	out := make([]Snapshot, 0, len(snapshots))
	mainBySession := map[string]int{}
	for _, snapshot := range snapshots {
		if !currentTerminalIsMainAgent(snapshot) {
			out = append(out, snapshot)
			continue
		}
		sessionID := strings.TrimSpace(snapshot.SessionID)
		if sessionID == "" {
			sessionID = strings.TrimSpace(snapshot.OwnerID)
		}
		if sessionID == "" {
			sessionID = strings.TrimSpace(snapshot.TerminalID)
		}
		if idx, ok := mainBySession[sessionID]; ok {
			if shouldPreferTerminalSnapshot(snapshot, out[idx]) {
				out[idx] = snapshot
			}
			continue
		}
		mainBySession[sessionID] = len(out)
		out = append(out, snapshot)
	}
	return out
}

func currentTerminalIsMainAgent(snapshot Snapshot) bool {
	if strings.Contains(snapshot.TerminalID, ":turn-") {
		return false
	}
	kind := strings.ToLower(strings.TrimSpace(firstNonEmpty(snapshot.ExecutionKind, snapshot.Scope)))
	return kind == "main_agent" || kind == "main" || kind == "chat"
}

func shouldPreferTerminalSnapshot(candidate, existing Snapshot) bool {
	if candidate.Active != existing.Active {
		return candidate.Active
	}
	candidateRunning := candidate.State == "running"
	existingRunning := existing.State == "running"
	if candidateRunning != existingRunning {
		return candidateRunning
	}
	return candidate.UpdatedAt.After(existing.UpdatedAt)
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

type preValidationStatusUpdate struct {
	stepID       string
	stepPath     string
	stepTitle    string
	status       string
	summary      string
	passedChecks int
	failedChecks int
	totalChecks  int
}

func (s *Store) updatePreValidationStatus(sessionID string, event storeevents.Event) {
	update, ok := preValidationStatusFromEvent(event)
	if !ok {
		return
	}
	metadata := metadataForEvent(event)
	if update.stepID != "" && stringValue(metadata, "step_id") == "" {
		metadata["step_id"] = update.stepID
	}
	if update.stepPath != "" && stringValue(metadata, "step_path") == "" {
		metadata["step_path"] = update.stepPath
	}
	if update.stepTitle != "" && stringValue(metadata, "step_title") == "" {
		metadata["step_title"] = update.stepTitle
	}
	ownerID := terminalOwnerID(sessionID, event, metadata)
	now := event.Timestamp
	if now.IsZero() {
		now = time.Now()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	terminalID, snapshot, ok := s.findPreValidationTargetLocked(sessionID, ownerID, update, metadata)
	if !ok {
		return
	}
	snapshot.Status.PreValidationStatus = update.status
	snapshot.Status.PreValidationSummary = update.summary
	snapshot.Status.PreValidationPassedChecks = update.passedChecks
	snapshot.Status.PreValidationFailedChecks = update.failedChecks
	snapshot.Status.PreValidationTotalChecks = update.totalChecks
	if strings.TrimSpace(snapshot.Status.StatusText) == "" {
		snapshot.Status.StatusText = update.summary
	}
	snapshot.UpdatedAt = now
	fillDisplayContext(&snapshot)
	s.byID[terminalID] = snapshot
}

func (s *Store) findPreValidationTargetLocked(sessionID, ownerID string, update preValidationStatusUpdate, metadata map[string]interface{}) (string, Snapshot, bool) {
	if terminalID, snapshot, ok := s.findInactiveTargetLocked(sessionID, ownerID, metadata); ok {
		return terminalID, snapshot, true
	}

	sessionTerminals := s.bySession[sessionID]
	var bestID string
	var best Snapshot
	for terminalID := range sessionTerminals {
		snapshot, ok := s.byID[terminalID]
		if !ok || !snapshotMatchesPreValidation(snapshot, update) {
			continue
		}
		if bestID == "" || betterPreValidationTarget(snapshot, best) {
			bestID = terminalID
			best = snapshot
		}
	}
	if bestID == "" {
		return "", Snapshot{}, false
	}
	return bestID, best, true
}

func snapshotMatchesPreValidation(snapshot Snapshot, update preValidationStatusUpdate) bool {
	if update.stepID != "" {
		if snapshot.StepID == update.stepID ||
			strings.HasSuffix(snapshot.OwnerID, ":"+update.stepID) ||
			strings.Contains(snapshot.OwnerID, ":"+update.stepID+":") ||
			strings.Contains(snapshot.ExecutionID, ":"+update.stepID+":") ||
			strings.HasSuffix(snapshot.ExecutionID, ":"+update.stepID) {
			return true
		}
	}
	if update.stepPath != "" {
		if snapshot.StepID == update.stepPath ||
			strings.Contains(snapshot.OwnerID, update.stepPath) ||
			strings.Contains(snapshot.ExecutionID, update.stepPath) {
			return true
		}
	}
	if update.stepTitle != "" {
		return strings.EqualFold(snapshot.StepName, update.stepTitle) || strings.EqualFold(snapshot.Label, update.stepTitle)
	}
	return false
}

func betterPreValidationTarget(candidate, current Snapshot) bool {
	if candidate.Active != current.Active {
		return candidate.Active
	}
	candidateArchived := strings.Contains(candidate.TerminalID, ":turn-")
	currentArchived := strings.Contains(current.TerminalID, ":turn-")
	if candidateArchived != currentArchived {
		return !candidateArchived
	}
	candidateUpdated := candidate.UpdatedAt
	if candidateUpdated.IsZero() {
		candidateUpdated = candidate.CreatedAt
	}
	currentUpdated := current.UpdatedAt
	if currentUpdated.IsZero() {
		currentUpdated = current.CreatedAt
	}
	return candidateUpdated.After(currentUpdated)
}

func preValidationStatusFromEvent(event storeevents.Event) (preValidationStatusUpdate, bool) {
	data, ok := eventDataMap(event)
	if !ok {
		return preValidationStatusUpdate{}, false
	}
	overallPass, hasOverallPass := boolValue(data["overall_pass"])
	passedChecks := intValue(data["passed_checks"])
	failedChecks := intValue(data["failed_checks"])
	totalChecks := intValue(data["total_checks"])
	if !hasOverallPass && totalChecks == 0 && passedChecks == 0 && failedChecks == 0 {
		return preValidationStatusUpdate{}, false
	}
	if failedChecks == 0 && totalChecks > 0 && passedChecks <= totalChecks {
		failedChecks = totalChecks - passedChecks
	}

	status := "failed"
	if overallPass {
		status = "passed"
	}
	summaryStatus := status
	if status == "passed" {
		summaryStatus = "passed"
	}
	summary := fmt.Sprintf("Pre-validation %s", summaryStatus)
	if totalChecks > 0 {
		summary = fmt.Sprintf("Pre-validation %s: %d/%d checks", summaryStatus, passedChecks, totalChecks)
	}
	if status == "failed" {
		if errors := stringSliceValue(data["errors"]); len(errors) > 0 {
			summary = fmt.Sprintf("%s - %s", summary, errors[0])
		}
	}

	return preValidationStatusUpdate{
		stepID:       firstNonEmpty(stringValue(data, "step_id"), stringValue(data, "current_step_id"), stringValue(data, "workflow_step_id")),
		stepPath:     stringValue(data, "step_path"),
		stepTitle:    stringValue(data, "step_title"),
		status:       status,
		summary:      summary,
		passedChecks: passedChecks,
		failedChecks: failedChecks,
		totalChecks:  totalChecks,
	}, true
}

func eventDataMap(event storeevents.Event) (map[string]interface{}, bool) {
	if event.Data == nil || event.Data.Data == nil {
		return nil, false
	}
	switch data := event.Data.Data.(type) {
	case *agentevents.GenericEventData:
		if data == nil || len(data.Data) == 0 {
			return nil, false
		}
		return data.Data, true
	}
	encoded, err := json.Marshal(event.Data.Data)
	if err != nil {
		return nil, false
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(encoded, &decoded); err != nil || len(decoded) == 0 {
		return nil, false
	}
	return decoded, true
}

func preserveEphemeralStatusFields(status *Status, previous Status) {
	if status == nil {
		return
	}
	// Preserve pre-validation
	if previous.PreValidationStatus != "" {
		status.PreValidationStatus = previous.PreValidationStatus
		status.PreValidationSummary = previous.PreValidationSummary
		status.PreValidationPassedChecks = previous.PreValidationPassedChecks
		status.PreValidationFailedChecks = previous.PreValidationFailedChecks
		status.PreValidationTotalChecks = previous.PreValidationTotalChecks
		if strings.TrimSpace(status.StatusText) == "" && strings.HasPrefix(previous.StatusText, "Pre-validation ") {
			status.StatusText = previous.StatusText
		}
	}
	// Preserve real-time telemetry (tokens/cost/label). DeriveStatus rebuilds
	// Status from pane content on every refresh and has no statusline data, so
	// without this the out-of-band telemetry set by handleStatusLine is wiped.
	if status.InputTokens == 0 {
		status.InputTokens = previous.InputTokens
	}
	if status.OutputTokens == 0 {
		status.OutputTokens = previous.OutputTokens
	}
	if status.CacheCreationInputTokens == 0 {
		status.CacheCreationInputTokens = previous.CacheCreationInputTokens
	}
	if status.CacheReadInputTokens == 0 {
		status.CacheReadInputTokens = previous.CacheReadInputTokens
	}
	if status.TotalInputTokens == 0 {
		status.TotalInputTokens = previous.TotalInputTokens
	}
	if status.TotalOutputTokens == 0 {
		status.TotalOutputTokens = previous.TotalOutputTokens
	}
	if status.CostUSD == 0 {
		status.CostUSD = previous.CostUSD
	}
	if status.StatusMeta == nil {
		status.StatusMeta = previous.StatusMeta
	}
	// Keep the previous ProviderLabel when the freshly-derived one is empty, or
	// when the previous one carries a model detail (the "provider · model" form
	// set by handleStatusLine) that the derived one lacks. A length comparison
	// would wrongly drop a newer-but-shorter label; checking for the " · "
	// separator captures the actual "has model detail" intent.
	hasModelDetail := func(s string) bool { return strings.Contains(s, " · ") }
	if status.ProviderLabel == "" ||
		(previous.ProviderLabel != "" && hasModelDetail(previous.ProviderLabel) && !hasModelDetail(status.ProviderLabel)) {
		status.ProviderLabel = previous.ProviderLabel
	}
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
	default:
		if withBase, ok := event.Data.Data.(interface {
			GetBaseEventData() *agentevents.BaseEventData
		}); ok {
			if base := withBase.GetBaseEventData(); base != nil {
				metadata = mergeMetadata(metadata, base.Metadata)
			}
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

func isNonTmuxWorkflowTerminalMetadata(metadata map[string]interface{}) bool {
	if strings.ToLower(strings.TrimSpace(stringValue(metadata, "step_transport"))) == "tmux" {
		return false
	}
	return firstNonEmpty(
		stringValue(metadata, "execution_owner_id"),
		stringValue(metadata, "current_step_id"),
		stringValue(metadata, "workflow_step_id"),
		stringValue(metadata, "step_id"),
	) != ""
}

func terminalOwnerID(sessionID string, event storeevents.Event, metadata map[string]interface{}) string {
	if terminalEventIsMainAgent(event, metadata) {
		return "main:" + sessionID
	}
	if ownerID := workflowStepOwnerCandidate(event, metadata); validTerminalOwner(ownerID, sessionID) {
		return ownerID
	}
	if ownerID := firstValidTerminalOwner(sessionID,
		stringValue(metadata, "execution_owner_id"),
		stringValue(metadata, "owner_execution_id"),
	); ownerID != "" {
		return ownerID
	}
	if ownerID := firstValidTerminalOwner(sessionID,
		stringValue(metadata, "background_agent_id"),
		stringValue(metadata, "delegation_id"),
		stringValue(metadata, "agent_id"),
	); ownerID != "" {
		return ownerID
	}
	if ownerID := firstValidTerminalOwner(sessionID,
		stringValue(metadata, "execution_id"),
		event.ExecutionID,
	); ownerID != "" {
		return ownerID
	}
	return firstValidTerminalOwner(sessionID,
		stringValue(metadata, "current_step_id"),
		stringValue(metadata, "workflow_step_id"),
		stringValue(metadata, "step_id"),
		stringValue(metadata, "correlation_id"),
	)
}

func terminalEventIsMainAgent(event storeevents.Event, metadata map[string]interface{}) bool {
	kind := strings.ToLower(strings.TrimSpace(firstNonEmpty(
		event.ExecutionKind,
		stringValue(metadata, "execution_kind"),
		stringValue(metadata, "scope"),
	)))
	return kind == "main_agent" || kind == "main" || kind == "chat"
}

func firstValidTerminalOwner(sessionID string, candidates ...string) string {
	for _, candidate := range candidates {
		if validTerminalOwner(candidate, sessionID) {
			return strings.TrimSpace(candidate)
		}
	}
	return ""
}

func validTerminalOwner(candidate, sessionID string) bool {
	candidate = strings.TrimSpace(candidate)
	return candidate != "" && candidate != sessionID
}

func workflowStepOwnerCandidate(event storeevents.Event, metadata map[string]interface{}) string {
	for _, candidate := range []string{
		event.ExecutionID,
		stringValue(metadata, "execution_id"),
		stringValue(metadata, "agent_execution_id"),
	} {
		candidate = strings.TrimSpace(candidate)
		if strings.HasPrefix(candidate, "workflow-step:") {
			return candidate
		}
	}

	workflowID := firstNonEmpty(
		stringValue(metadata, "workflow_execution_id"),
		stringValue(metadata, "workflow_run_id"),
		stringValue(metadata, "execution_owner_id"),
		stringValue(metadata, "execution_id"),
		event.ExecutionID,
	)
	stepID := firstNonEmpty(
		stringValue(metadata, "current_step_id"),
		stringValue(metadata, "workflow_step_id"),
		stringValue(metadata, "step_id"),
	)
	if strings.HasPrefix(workflowID, "workflow-full-") && stepID != "" {
		return "workflow-step:" + workflowID + ":" + stepID
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
		// Prefer a human title (plan step title, or the agent's own name for
		// step-less maintenance agents like learning/organize) over the raw step
		// ID. The ID — e.g. "_global" for the global-learnings skill — is a folder
		// / lookup key, not a display name. Falls back to the ID when no human name
		// is present, so genuine steps without a title are unchanged.
		return firstNonEmpty(snapshot.StepName, snapshot.AgentName, snapshot.StepID)
	case "main_agent", "main", "chat":
		return firstNonEmpty(snapshot.AgentName, snapshot.StepName)
	case "background_agent", "background", "delegation", "todo_task", "sub_agent":
		return firstNonEmpty(snapshot.AgentName, snapshot.StepName, cleanOpaqueLabel(snapshot.Label), snapshot.StepID)
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

func boolValue(value interface{}) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		trimmed := strings.TrimSpace(strings.ToLower(typed))
		if trimmed == "" {
			return false, false
		}
		switch trimmed {
		case "true", "1", "yes", "y", "passed", "pass":
			return true, true
		case "false", "0", "no", "n", "failed", "fail":
			return false, true
		default:
			return false, false
		}
	case int:
		return typed != 0, true
	case int64:
		return typed != 0, true
	case float64:
		return typed != 0, true
	case float32:
		return typed != 0, true
	default:
		return false, false
	}
}

func stringSliceValue(value interface{}) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" {
				out = append(out, text)
			}
		}
		return out
	case string:
		if trimmed := strings.TrimSpace(typed); trimmed != "" {
			return []string{trimmed}
		}
	}
	return nil
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

func (s *Store) handleStatusLine(sessionID string, event storeevents.Event) {
	if event.Data == nil || event.Data.Data == nil {
		return
	}

	var provider, model, tmuxSession string
	var inputTokens, outputTokens int
	var cacheCreationTokens, cacheReadTokens, totalInputTokens, totalOutputTokens int
	var costUSD float64
	var statusMeta map[string]interface{}
	var found bool

	switch data := event.Data.Data.(type) {
	case *agentevents.StreamingStatusLineEvent:
		provider = data.Provider
		model = data.Model
		tmuxSession = data.TmuxSession
		inputTokens = data.InputTokens
		outputTokens = data.OutputTokens
		cacheCreationTokens = data.CacheCreationInputTokens
		cacheReadTokens = data.CacheReadInputTokens
		totalInputTokens = data.TotalInputTokens
		totalOutputTokens = data.TotalOutputTokens
		costUSD = data.CostUSD
		statusMeta = data.Metadata
		found = true
	case *agentevents.GenericEventData:
		provider, _ = data.Data["provider"].(string)
		model, _ = data.Data["model"].(string)
		tmuxSession, _ = data.Data["tmux_session"].(string)
		inputTokens = intValue(data.Data["input_tokens"])
		outputTokens = intValue(data.Data["output_tokens"])
		cacheCreationTokens = intValue(data.Data["cache_creation_input_tokens"])
		cacheReadTokens = intValue(data.Data["cache_read_input_tokens"])
		totalInputTokens = intValue(data.Data["total_input_tokens"])
		totalOutputTokens = intValue(data.Data["total_output_tokens"])
		costUSD = floatValue(data.Data["cost_usd"])
		statusMeta, _ = data.Data["metadata"].(map[string]interface{})
		found = true
	}

	if !found {
		return
	}
	tmuxSession = firstNonEmpty(
		tmuxSession,
		stringValue(statusMeta, "tmux_session"),
		stringValue(statusMeta, "tmux_session_name"),
		stringValue(statusMeta, "pi_interactive_session"),
		stringValue(statusMeta, "claude_code_interactive_session"),
		stringValue(statusMeta, "codex_interactive_session"),
		stringValue(statusMeta, "gemini_interactive_session"),
		stringValue(statusMeta, "cursor_interactive_session"),
	)

	// Use the provider name verbatim — the adapter owns its display name
	// (e.g. "agy-cli", "claudecode"); the store must not re-map provider ids.
	providerLabel := provider
	if model != "" {
		if providerLabel != "" {
			providerLabel = fmt.Sprintf("%s · %s", provider, model)
		} else {
			providerLabel = model
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	tmuxSession = strings.TrimSpace(tmuxSession)
	updated := false
	for terminalID := range s.bySession[sessionID] {
		snapshot, exists := s.byID[terminalID]
		if !exists {
			continue
		}
		// When the event identifies its tmux session, scope the update to the
		// terminal that owns it — a session can host several coding-agent panes,
		// and the telemetry belongs to exactly one. Fall back to updating every
		// terminal only when no tmux session is carried (older producers).
		if tmuxSession != "" && strings.TrimSpace(snapshot.TmuxSession) != tmuxSession {
			continue
		}
		snapshot.Status.InputTokens = inputTokens
		snapshot.Status.OutputTokens = outputTokens
		snapshot.Status.CacheCreationInputTokens = cacheCreationTokens
		snapshot.Status.CacheReadInputTokens = cacheReadTokens
		snapshot.Status.TotalInputTokens = totalInputTokens
		snapshot.Status.TotalOutputTokens = totalOutputTokens
		snapshot.Status.CostUSD = costUSD
		snapshot.Status.ProviderLabel = providerLabel
		if statusMeta != nil {
			snapshot.Status.StatusMeta = statusMeta
		}
		snapshot.UpdatedAt = now
		s.byID[terminalID] = snapshot
		updated = true
	}
	if updated || tmuxSession == "" {
		return
	}
	statusMetadata := mergeMetadata(metadataForEvent(event), statusMeta)
	statusMetadata["kind"] = firstNonEmpty(stringValue(statusMetadata, "kind"), "terminal")
	statusMetadata["tmux_session"] = tmuxSession
	statusMetadata["step_transport"] = firstNonEmpty(stringValue(statusMetadata, "step_transport"), "tmux")
	ownerID := terminalOwnerID(sessionID, event, statusMetadata)
	if ownerID == "" {
		ownerID = "main:" + sessionID
	}
	executionKind := firstNonEmpty(event.ExecutionKind, stringValue(statusMetadata, "execution_kind"))
	if executionKind == "" && strings.HasPrefix(ownerID, "main:") {
		executionKind = "main_agent"
	}
	scope := terminalScope(event, statusMetadata)
	if scope == "session" && strings.HasPrefix(ownerID, "main:") {
		scope = "main_agent"
	}
	label := terminalLabel(event, statusMetadata, ownerID)
	if label == "Terminal" && providerLabel != "" {
		label = providerLabel
	}
	snapshot := Snapshot{
		TerminalID:    terminalIDFor(sessionID, ownerID),
		SessionID:     sessionID,
		OwnerID:       ownerID,
		ExecutionID:   firstNonEmpty(event.ExecutionID, stringValue(statusMeta, "execution_id")),
		ExecutionKind: executionKind,
		Label:         label,
		Scope:         scope,
		StepID:        firstNonEmpty(workflowStepIDFromOwner(ownerID), stringValue(statusMetadata, "current_step_id"), stringValue(statusMetadata, "workflow_step_id"), stringValue(statusMetadata, "step_id")),
		StepTransport: "tmux",
		TmuxSession:   tmuxSession,
		ContentSource: "tmux_live",
		ChunkIndex:    0,
		Active:        true,
		State:         "running",
		Status: Status{
			InputTokens:              inputTokens,
			OutputTokens:             outputTokens,
			CacheCreationInputTokens: cacheCreationTokens,
			CacheReadInputTokens:     cacheReadTokens,
			TotalInputTokens:         totalInputTokens,
			TotalOutputTokens:        totalOutputTokens,
			CostUSD:                  costUSD,
			ProviderLabel:            providerLabel,
			StatusMeta:               statusMeta,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if snapshot.ExecutionID == "" {
		snapshot.ExecutionID = snapshot.OwnerID
	}
	if snapshot.StepID == "" && currentTerminalIsMainAgent(snapshot) {
		snapshot.StepID = "main_agent:" + sessionID
	}
	fillDisplayContext(&snapshot)
	if _, ok := s.dismissed[snapshot.TerminalID]; ok {
		delete(s.dismissed, snapshot.TerminalID)
	}
	s.byID[snapshot.TerminalID] = snapshot
	if s.bySession[sessionID] == nil {
		s.bySession[sessionID] = make(map[string]struct{})
	}
	s.bySession[sessionID][snapshot.TerminalID] = struct{}{}
	if currentTerminalIsMainAgent(snapshot) {
		s.removeCurrentMainAgentAliasesLocked(sessionID, snapshot.TerminalID)
	}
}
