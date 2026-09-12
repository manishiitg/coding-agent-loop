package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/contractupgrade"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/costledger"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/fsutil"
	stepworkflow "github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
	orchestratorevents "github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/events"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/pulsemodules"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/schedulerstate"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workflowtypes"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workspace"
	"github.com/robfig/cron/v3"
)

const scheduledBackgroundNoPollingInstruction = "After launching background workflow or step work, do not babysit it with sleep/list_executions/query_step polling loops. Use at most one immediate query_step if you need to confirm the execution_id/status, then stop; [AUTO-NOTIFICATION] messages will resume the conversation when background work completes."

// ScheduleContext bundles everything needed to identify and execute a schedule.
type ScheduleContext struct {
	WorkspacePath string
	WorkflowID    string
	WorkflowLabel string
	Schedule      WorkflowSchedule
	Capabilities  WorkflowCapabilities
	// OwnerUserID is the workflow's WorkflowManifest.CreatedBy, threaded
	// through so startSessionInternal resolves secrets against the account
	// that actually configured them instead of the "default" placeholder
	// user, who never has any stored. Empty for a workflow created before
	// CreatedBy existed -- startSessionInternal's own empty-string handling
	// (falling through to GetDefaultUserID()) is unchanged for that case.
	OwnerUserID   string
	TriggerSource string // "cron" (default) or "manual"; encoded into the session ID
	// OriginSessionID is the chat session that triggered this run, when one did.
	// A scheduled run mints its own session, so without this link its terminals
	// are invisible to the tab that asked for the run. Empty for cron.
	OriginSessionID string
	// ForcePulseReview is used by the toolbar's one-off Pulse action. Normal
	// schedules use workflow.json pulse.enabled instead.
	ForcePulseReview bool
	// PulseOnly suppresses the normal workflow message for the toolbar's one-off
	// Pulse action. Version preflight still runs before Pulse, which reviews the
	// latest retained workflow evidence and then executes the normal finalizer.
	PulseOnly              bool
	PulseEvidenceRunFolder string
	PulseEvidenceRunStatus string
	CalendarItem           *CalendarScheduleItem
	// ScheduledFor is the durable identity of a cron/calendar occurrence. It is
	// empty for a manual trigger, whose actual trigger time is its identity.
	ScheduledFor time.Time
	// QueuedExpiresAt preserves the first queue attempt's max-start deadline
	// across retries. A busy workflow must not extend a stale occurrence forever.
	QueuedExpiresAt       time.Time
	QueuedOccurrenceCount int

	// ProducedRunEvidence reports whether this invocation actually started the
	// workflow and created or restarted a run folder. It is deliberately
	// independent of success: a failed run still produced evidence that Pulse
	// should review. A preflight abort against a pre-existing, untouched run
	// folder (e.g. a workflow reusing iteration-0) did not.
	ProducedRunEvidence bool

	// CapacityResume* carry a run that was suspended on a provider capacity
	// wall back into execution (PLAT-101). The run is resumed in place — same
	// run row, same run folder, starting at the step that could not run —
	// because the steps before it already completed and did real work that must
	// not be replayed.
	CapacityResumeRunID     string
	CapacityResumeRunFolder string
	CapacityResumeFromStep  int
}

const manualWorkflowPulseScheduleID = "manual-pulse"

const scheduleScopeSeparator = "\x1f"

func workflowScheduleRuntimeKey(workspacePath, scheduleID string) string {
	return strings.Join([]string{"workflow", filepath.Clean(strings.TrimSpace(workspacePath)), strings.TrimSpace(scheduleID)}, scheduleScopeSeparator)
}

func scheduleRuntimeKey(sctx *ScheduleContext) string {
	if sctx == nil {
		return ""
	}
	return workflowScheduleRuntimeKey(sctx.WorkspacePath, sctx.Schedule.ID)
}

func scheduleRuntimeKeyHasID(key, scheduleID string) bool {
	return strings.HasSuffix(key, scheduleScopeSeparator+strings.TrimSpace(scheduleID))
}

func scheduleStateLockKeyFromRuntimeKey(runtimeKey string) string {
	parts := strings.Split(runtimeKey, scheduleScopeSeparator)
	if len(parts) < 3 || parts[0] != "workflow" {
		return runtimeKey
	}
	if parts[2] == manualWorkflowPulseScheduleID {
		return strings.Join([]string{"workflow-pulse", parts[1]}, scheduleScopeSeparator)
	}
	return strings.Join(parts[:2], scheduleScopeSeparator)
}

func scheduleStateScope(sctx *ScheduleContext) (scopeType, scopeID, lockKey string) {
	if sctx != nil {
		scopeID = filepath.Clean(strings.TrimSpace(sctx.WorkspacePath))
		if sctx.Schedule.ID == manualWorkflowPulseScheduleID {
			return "workflow", scopeID, strings.Join([]string{"workflow-pulse", scopeID}, scheduleScopeSeparator)
		}
	}
	return "workflow", scopeID, strings.Join([]string{"workflow", scopeID}, scheduleScopeSeparator)
}

// newScheduleSessionID mints the session ID for a scheduled run. Encoding the
// trigger source (cron vs. manual) and the schedule ID prefix makes it easy to
// tell, just from the builder/ filename, where a conversation originated.
func (s *SchedulerService) newScheduleSessionID(sctx *ScheduleContext) string {
	trigger := sctx.TriggerSource
	if trigger == "" {
		trigger = "cron"
	}
	idPrefix := sctx.Schedule.ID
	if len(idPrefix) > 8 {
		idPrefix = idPrefix[:8]
	}
	sessionID := fmt.Sprintf("schedule-%s--%s_%d", trigger, idPrefix, time.Now().UnixNano())
	// Link it back to the chat that asked, so that chat's terminal rail can show
	// the run it started. No-op for cron, which has no originating chat.
	rememberScheduleOrigin(sessionID, sctx.OriginSessionID)
	return sessionID
}

// ScheduleRuntimeState holds in-memory runtime state for a schedule (not persisted in manifest).
type ScheduleRuntimeState struct {
	ActiveRunID         string     `json:"active_run_id,omitempty"`
	LastStatus          string     `json:"last_status,omitempty"`
	LastRunAt           *time.Time `json:"last_run_at,omitempty"`
	NextRunAt           *time.Time `json:"next_run_at,omitempty"`
	LastSessionID       string     `json:"last_session_id,omitempty"`
	LastError           string     `json:"last_error,omitempty"`
	LastDurationMs      *int64     `json:"last_duration_ms,omitempty"`
	RunCount            int        `json:"run_count"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	WaitingSince        *time.Time `json:"waiting_since,omitempty"`
	WaitingUntil        *time.Time `json:"waiting_until,omitempty"`
	WaitingReason       string     `json:"waiting_reason,omitempty"`
	QueuedOccurrences   int        `json:"queued_occurrences,omitempty"`
}

// registeredJob is a schedule registered for wall-clock evaluation.
type registeredJob struct {
	sctx      *ScheduleContext
	cronSched cron.Schedule // nil for calendar (one-time) jobs
	runAt     *time.Time    // non-nil for calendar (one-time) jobs
	lastFired time.Time     // durable scheduled occurrence cursor, not the wall-clock dispatch time
}

type dueRegisteredJob struct {
	job          *registeredJob
	scheduledFor time.Time
}

// SchedulerService manages cron job execution using wall-clock polling.
// Every 60 seconds it evaluates each registered schedule's cron expression against
// the current wall-clock time. This approach is immune to macOS App Nap and sleep/wake
// issues that wedge monotonic-clock-based timers (like gocron).
type SchedulerService struct {
	api  *StreamingAPI
	mu   sync.Mutex
	jobs map[string]*registeredJob // scoped schedule key → job
	// scheduleFingerprints tracks the persisted config loaded for each scoped
	// schedule, including disabled and calendar schedules with no future items.
	scheduleFingerprints map[string]string

	stateStoreMu sync.RWMutex
	stateStore   *schedulerstate.Store
	runCancelsMu sync.Mutex
	runCancels   map[string]context.CancelFunc

	// In-memory runtime state per schedule (survives within server lifetime, reset on restart)
	runtimeStates   map[string]*ScheduleRuntimeState
	runtimeStatesMu sync.RWMutex

	// Schedule-to-workspace index for quick lookups
	workspaceIndex   map[string]string // scheduleID → workspacePath
	workspaceIndexMu sync.RWMutex

	workflowManifestCacheMu        sync.Mutex
	workflowManifestCacheExpiresAt time.Time
	workflowManifestCache          []DiscoveredWorkflow
	queuedResumeMu                 sync.Mutex
	queuedLaunchMu                 sync.Mutex
	queuedLaunching                map[string]bool
}

func (s *SchedulerService) logf(sctx *ScheduleContext, format string, args ...interface{}) {
	scheduleLogfWithContext(scheduleLogContext(sctx), format, args...)
}

func (s *SchedulerService) sessionLogf(sctx *ScheduleContext, sessionID string, format string, args ...interface{}) {
	scheduleLogfWithContext(scheduleLogContext(sctx).WithSession(sessionID), format, args...)
}

// NewSchedulerService creates a new manifest-based SchedulerService.
func NewSchedulerService(api *StreamingAPI) *SchedulerService {
	return &SchedulerService{
		api:                  api,
		jobs:                 make(map[string]*registeredJob),
		scheduleFingerprints: make(map[string]string),
		runCancels:           make(map[string]context.CancelFunc),
		runtimeStates:        make(map[string]*ScheduleRuntimeState),
		workspaceIndex:       make(map[string]string),
	}
}

func (s *SchedulerService) DiscoverWorkflowManifestsCached(ctx context.Context, ttl time.Duration) ([]DiscoveredWorkflow, error) {
	now := time.Now()

	s.workflowManifestCacheMu.Lock()
	if ttl > 0 && now.Before(s.workflowManifestCacheExpiresAt) && s.workflowManifestCache != nil {
		cached := append([]DiscoveredWorkflow(nil), s.workflowManifestCache...)
		s.workflowManifestCacheMu.Unlock()
		return cached, nil
	}
	s.workflowManifestCacheMu.Unlock()

	discovered, err := DiscoverWorkflowManifests(ctx)
	if err != nil {
		return nil, err
	}

	s.workflowManifestCacheMu.Lock()
	s.workflowManifestCache = append([]DiscoveredWorkflow(nil), discovered...)
	if ttl > 0 {
		s.workflowManifestCacheExpiresAt = now.Add(ttl)
	} else {
		s.workflowManifestCacheExpiresAt = time.Time{}
	}
	s.workflowManifestCacheMu.Unlock()

	return discovered, nil
}

func (s *SchedulerService) InvalidateWorkflowManifestCache() {
	s.workflowManifestCacheMu.Lock()
	s.workflowManifestCache = nil
	s.workflowManifestCacheExpiresAt = time.Time{}
	s.workflowManifestCacheMu.Unlock()
}

// Start scans all workspace folders for workflow.json manifests, loads enabled schedules,
// and starts the wall-clock tick loop.
func (s *SchedulerService) Start(ctx context.Context) error {
	scheduleLogf("[SCHEDULER] Starting manifest-based scheduler service...")
	s.stateStoreMu.Lock()
	if s.stateStore == nil {
		storePath := filepath.Join(fsutil.WorkspaceDocsRoot(), "_system", "schedule-state.sqlite")
		store, err := schedulerstate.Open(storePath)
		if err != nil {
			s.stateStoreMu.Unlock()
			return fmt.Errorf("initialize schedule state store: %w", err)
		}
		s.stateStore = store
	}
	if interrupted, err := s.stateStore.InterruptActiveRuns(ctx, "interrupted: server restarted", time.Now().UTC()); err != nil {
		s.stateStoreMu.Unlock()
		return fmt.Errorf("reconcile interrupted schedule runs: %w", err)
	} else if interrupted > 0 {
		scheduleLogf("[SCHEDULER] Marked %d durable schedule run(s) interrupted after restart", interrupted)
	}
	s.stateStoreMu.Unlock()

	// Discover all workflows by scanning workspace-docs/Workflow/*/workflow.json
	workflows := s.discoverWorkflows(ctx)
	scheduleLogf("[SCHEDULER] Discovered %d workflows with manifests", len(workflows))
	for i := range workflows {
		if workflows[i].Manifest.MigrateLegacyPulseSchedule() {
			if err := WriteWorkflowManifest(ctx, workflows[i].WorkspacePath, workflows[i].Manifest); err != nil {
				scheduleLogf("[PULSE] Failed to migrate legacy dedicated schedule in %s: %v", workflows[i].WorkspacePath, err)
			} else {
				scheduleLogf("[PULSE] Migrated legacy dedicated schedule to pulse.enabled in %s", workflows[i].WorkspacePath)
			}
		}
	}
	for _, wf := range workflows {
		// Reviewer rows are stranded by an interrupted agent process and no
		// longer represent live work after restart. This cleanup does not infer
		// backup/publish/notify outcomes; those remain exactly as the agent wrote.
		reviews, err := finalizeAllRunningPulseReviewLogs(ctx, wf.WorkspacePath, "Pulse interrupted because the server restarted")
		if err != nil {
			scheduleLogf("[SCHEDULER] Failed to reconcile stale Pulse review rows in %s: %v", wf.WorkspacePath, err)
		} else if reviews > 0 {
			scheduleLogf("[SCHEDULER] Marked %d stale Pulse review row(s) failed in %s", reviews, wf.WorkspacePath)
		}
		recoveries, err := finalizeRunningPulseReviewRecoveries(ctx, wf.WorkspacePath, "Pulse interrupted because the server restarted before a terminal module result was recorded")
		if err != nil {
			scheduleLogf("[SCHEDULER] Failed to reconcile stale Pulse review recovery attempts in %s: %v", wf.WorkspacePath, err)
		} else if recoveries > 0 {
			scheduleLogf("[SCHEDULER] Marked %d interrupted Pulse review attempt(s) recoverable in %s", recoveries, wf.WorkspacePath)
		}
	}

	// Reconcile the legacy/UI schedule-runs.json projection from the durable
	// scheduler ledger. A final workspace write can fail after the canonical
	// terminal transition succeeds; restart is not evidence that such a run was
	// interrupted (PLAT-017).
	for _, wf := range workflows {
		runs, err := ReadScheduleRuns(ctx, wf.WorkspacePath)
		if err != nil {
			continue
		}
		fixed := 0
		for i := range runs {
			if runs[i].Status == "running" {
				status, errMsg, completedAt, ok := s.durableScheduleRunProjection(ctx, runs[i].ID)
				if !ok {
					status = "interrupted"
					errMsg = "interrupted: server restarted without durable terminal evidence"
					now := time.Now().UTC()
					completedAt = &now
				}
				runs[i].Status = status
				runs[i].Error = errMsg
				runs[i].CompletedAt = completedAt
				if completedAt != nil {
					duration := completedAt.Sub(runs[i].StartedAt).Milliseconds()
					if duration < 0 {
						duration = 0
					}
					runs[i].DurationMs = &duration
				}
				fixed++
			}
		}
		if fixed > 0 {
			_ = WriteScheduleRuns(ctx, wf.WorkspacePath, runs)
			scheduleLogf("[SCHEDULER] Marked %d stale running run(s) as error in %s", fixed, wf.WorkspacePath)
		}
	}

	loaded := 0
	for _, wf := range workflows {
		for _, sched := range wf.Manifest.Schedules {
			sctx := buildScheduleContext(wf.WorkspacePath, wf.Manifest, sched)
			if err := s.LoadSchedule(sctx); err != nil {
				scheduleLogf("[SCHEDULER] Failed to load schedule %s (%s): %v", sched.ID, sched.Name, err)
			} else if sched.Enabled && !sched.PulseReviewOnly {
				loaded++
			}
		}
	}

	scheduleLogf("[SCHEDULER] ✅ Started with %d schedules. Server time: %s, timezone: %s",
		loaded, time.Now().Format(time.RFC3339), time.Now().Location().String())

	// Wall-clock tick loop: every 60s, evaluate all registered schedules against current time.
	go s.tickLoop(ctx)

	// Wait for context cancellation
	<-ctx.Done()
	scheduleLogf("[SCHEDULER] Shutting down (context canceled)")
	return nil
}

func (s *SchedulerService) durableScheduleRunProjection(ctx context.Context, runID string) (string, string, *time.Time, bool) {
	if s == nil || strings.TrimSpace(runID) == "" {
		return "", "", nil, false
	}
	s.stateStoreMu.RLock()
	store := s.stateStore
	if store == nil {
		s.stateStoreMu.RUnlock()
		return "", "", nil, false
	}
	run, err := store.GetRun(ctx, runID)
	s.stateStoreMu.RUnlock()
	if err != nil || !schedulerstate.IsTerminal(run.State) {
		return "", "", nil, false
	}
	status := "error"
	switch run.State {
	case schedulerstate.StateCompleted:
		status = "success"
	case schedulerstate.StatePartial:
		status = "partial"
	case schedulerstate.StateStopped:
		status = "stopped"
	case schedulerstate.StateInterrupted:
		status = "interrupted"
	case schedulerstate.StateFailed:
		status = "error"
	}
	errMsg := run.ErrorMessage
	if status == "success" {
		errMsg = ""
	}
	return status, errMsg, run.CompletedAt, true
}

// tickLoop is the wall-clock scheduler. Every 60 seconds it evaluates each
// registered schedule against the current wall-clock time and fires any that
// are due. Unlike timer-based schedulers (gocron), this approach is immune to
// macOS App Nap and sleep/wake monotonic clock drift — if a job was missed
// during sleep, it fires on the first tick after wake.
func (s *SchedulerService) tickLoop(ctx context.Context) {
	const interval = 60 * time.Second
	const wakeThreshold = 90 * time.Second

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	lastTick := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			gap := t.Sub(lastTick)
			if gap > wakeThreshold {
				scheduleLogf("[SCHEDULER] 💤 WAKE_DETECTED gap=%s now=%s prev_tick=%s",
					gap.Round(time.Second), t.Format(time.RFC3339), lastTick.Format(time.RFC3339))
			}

			s.mu.Lock()
			parts := make([]string, 0, len(s.jobs))
			var toFire []dueRegisteredJob
			var missed []dueRegisteredJob
			for sid, job := range s.jobs {
				if job.cronSched != nil {
					occurrences := dueCronOccurrences(job.cronSched, job.lastFired, t)
					if len(occurrences) > 0 {
						for _, occurrence := range occurrences[:len(occurrences)-1] {
							missed = append(missed, dueRegisteredJob{job: job, scheduledFor: occurrence})
						}
						latest := occurrences[len(occurrences)-1]
						toFire = append(toFire, dueRegisteredJob{job: job, scheduledFor: latest})
					}
					parts = append(parts, fmt.Sprintf("%s next=%s", sid, job.cronSched.Next(t).UTC().Format(time.RFC3339)))
				} else if job.runAt != nil {
					if !job.runAt.After(t) && job.lastFired.Before(*job.runAt) {
						toFire = append(toFire, dueRegisteredJob{job: job, scheduledFor: job.runAt.UTC()})
					}
					parts = append(parts, fmt.Sprintf("%s at=%s", sid, job.runAt.UTC().Format(time.RFC3339)))
				}
			}
			s.mu.Unlock()

			scheduleLogf("[SCHEDULER] ❤️ heartbeat now=%s gap=%s jobs=%d due=%d missed=%d | %s",
				t.Format(time.RFC3339), gap.Round(time.Second), len(parts), len(toFire), len(missed), strings.Join(parts, ", "))

			failedOccurrenceLedger := make(map[*registeredJob]bool)
			for _, due := range missed {
				occurrenceCtx := *due.job.sctx
				occurrenceCtx.ScheduledFor = due.scheduledFor
				if err := s.recordScheduleFireDecision(context.Background(), &occurrenceCtx, "missed_scheduler_gap", "scheduler was not evaluating at the scheduled occurrence; the latest due occurrence will run as catch-up", "", t.UTC()); err != nil {
					failedOccurrenceLedger[due.job] = true
				}
			}
			for _, due := range toFire {
				occurrenceCtx := *due.job.sctx
				occurrenceCtx.ScheduledFor = due.scheduledFor
				if failedOccurrenceLedger[due.job] {
					s.logf(&occurrenceCtx, "[SCHEDULER] refusing to advance occurrence scheduled_for=%s because an earlier gap decision could not be recorded", due.scheduledFor.Format(time.RFC3339))
					continue
				}
				if err := s.recordScheduleFireDecision(context.Background(), &occurrenceCtx, "attempted", "scheduler selected this occurrence for execution", "", t.UTC()); err != nil {
					s.logf(&occurrenceCtx, "[SCHEDULER] refusing to launch occurrence scheduled_for=%s because its durable attempt could not be recorded: %v", due.scheduledFor.Format(time.RFC3339), err)
					continue
				}
				s.mu.Lock()
				if due.job.lastFired.Before(due.scheduledFor) {
					due.job.lastFired = due.scheduledFor
				}
				s.mu.Unlock()
				go s.triggerSchedule(due.job.sctx, due.scheduledFor)
			}

			// Suspended runs wake on their own reset time rather than on a
			// cron occurrence, so they are evaluated every tick alongside the
			// schedules (PLAT-101).
			go s.resumeDueCapacityWaits(context.Background(), t.UTC())
			go s.resumeQueuedScheduleOccurrences(context.Background(), t.UTC())
			// A durable fast-Pulse request (PLAT-262) otherwise sits inert
			// until a regularly scheduled Pulse run happens to coalesce it
			// (see the PulseOnly branch in triggerSchedule) — the whole point
			// of "fast" is dispatching it now, not waiting for that.
			go s.launchPendingFastPulseRequests(context.Background())
			lastTick = t
		}
	}
}

func (s *SchedulerService) launchPendingFastPulseRequests(ctx context.Context) {
	for _, workflow := range s.discoverWorkflows(ctx) {
		request, err := pendingFastPulseRequest(ctx, workflow.WorkspacePath)
		if err != nil {
			scheduleLogf("[PULSE] failed to read fast Pulse request for %s: %v", workflow.WorkspacePath, err)
			continue
		}
		if request == nil {
			continue
		}
		var pulseSchedule *WorkflowSchedule
		for i := range workflow.Manifest.Schedules {
			candidate := &workflow.Manifest.Schedules[i]
			if candidate.Enabled && candidate.PulseReviewOnly {
				pulseSchedule = candidate
				break
			}
		}
		if pulseSchedule == nil {
			scheduleLogf("[PULSE] fast request for %s remains pending: no enabled pulse_review_only schedule", workflow.WorkspacePath)
			continue
		}
		runID, err := s.TriggerNow(workflow.WorkspacePath, pulseSchedule.ID)
		if err != nil {
			// A still-closing workflow or running Pulse is expected to retry on a
			// later tick. Keep the durable request rather than starting a second
			// execution lane or silently dropping the agent's decision.
			scheduleLogf("[PULSE] fast request for %s will retry: %v", workflow.WorkspacePath, err)
			continue
		}
		if err := markFastPulseRequestDelivered(ctx, workflow.WorkspacePath, runID); err != nil {
			scheduleLogf("[PULSE] fast request for %s started as %s but could not be marked delivered: %v", workflow.WorkspacePath, runID, err)
			continue
		}
		scheduleLogf("[PULSE] fast request delivered for %s as dedicated schedule %s (run %s)", workflow.WorkspacePath, pulseSchedule.ID, runID)
	}
}

const maxCronOccurrenceScans = int(workflowScheduleHistoryRetention/time.Minute) + 1

// dueCronOccurrences returns the retained occurrence window after a durable
// cursor. Cron expressions have one-minute resolution, so a seven-day scan is
// bounded to at most 10,080 real occurrences. Every occurrence in that window
// is returned and durably classified; only the newest one executes as catch-up.
func dueCronOccurrences(schedule cron.Schedule, after, now time.Time) []time.Time {
	if schedule == nil || !now.After(after) {
		return nil
	}
	if floor := now.Add(-workflowScheduleHistoryRetention); after.Before(floor) {
		after = floor
	}
	occurrences := make([]time.Time, 0, 64)
	cursor := after
	for scans := 0; scans < maxCronOccurrenceScans; scans++ {
		next := schedule.Next(cursor)
		if next.After(now) || !next.After(cursor) {
			break
		}
		occurrences = append(occurrences, next.UTC())
		cursor = next
	}
	return occurrences
}

// discoveredWorkflow holds a manifest + its workspace path.
type discoveredWorkflow struct {
	WorkspacePath string
	Manifest      *WorkflowManifest
}

// discoverWorkflows scans workspace-docs/Workflow/ for workflow.json files.
func (s *SchedulerService) discoverWorkflows(ctx context.Context) []discoveredWorkflow {
	var results []discoveredWorkflow

	discovered, err := DiscoverWorkflowManifests(ctx)
	if err != nil {
		scheduleLogf("[SCHEDULER] Cannot scan workflow directory: %v", err)
		return nil
	}

	for _, item := range discovered {
		if len(item.Manifest.Schedules) > 0 {
			results = append(results, discoveredWorkflow{
				WorkspacePath: item.WorkspacePath,
				Manifest:      item.Manifest,
			})
		}
	}

	return results
}

// buildScheduleContext creates a ScheduleContext from a manifest and schedule.
func buildScheduleContext(workspacePath string, manifest *WorkflowManifest, sched WorkflowSchedule) *ScheduleContext {
	sctx := &ScheduleContext{
		WorkspacePath: workspacePath,
		WorkflowID:    manifest.ID,
		WorkflowLabel: manifest.Label,
		Schedule:      sched,
		Capabilities:  lockedScheduleCapabilities(manifest.Capabilities),
		OwnerUserID:   manifest.CreatedBy,
	}
	if sched.PulseReviewOnly {
		// PLAT-115: a workflow's own periodic Pulse-review schedule reuses the
		// exact plumbing the manual "Run Pulse now" trigger already exercises
		// (TriggerPulseNow) — skip workflow execution, run the full Gate/
		// Review+Fix/Finalize chain regardless of ordinary run finalization. Unlike
		// TriggerPulseNow this never sets PulseEvidenceRunFolder: that field
		// means "review exactly this one folder," but a periodic pass reviews
		// whatever runs/iteration-N/ backlog exists, decided by Gate's own
		// reasoning (see the dedicated-review Gate branch).
		sctx.PulseOnly = true
		sctx.ForcePulseReview = true
	}
	return sctx
}

func shouldRunPulseLifecycle(sctx *ScheduleContext, manifest *WorkflowManifest) bool {
	if manifest == nil {
		return false
	}
	return effectiveSchedulePulseMode(sctx, manifest) != schedulePulseModeOff
}

func effectiveSchedulePulseMode(sctx *ScheduleContext, manifest *WorkflowManifest) string {
	if sctx != nil && sctx.ForcePulseReview {
		return schedulePulseModeFull
	}
	if sctx == nil || manifest == nil {
		return schedulePulseModeOff
	}
	// A blocking migration may have changed the policy after this run captured
	// its schedule. Use the freshly read manifest for post-run stewardship.
	for _, schedule := range manifest.Schedules {
		if schedule.ID != "" && schedule.ID == sctx.Schedule.ID {
			return manifest.EffectivePulseMode(schedule)
		}
	}
	return manifest.EffectivePulseMode(sctx.Schedule)
}

// pulseScheduleTimingSummary supplies facts to the ordinary-run finalizer; it
// never chooses whether a change is material or rewrites a schedule. That is
// the finalizer agent's judgment, persisted through record_pulse_fast_request.
// Returns "" when Pulse is disabled (no enabled pulse_review_only schedule),
// which the caller uses to omit the Pulse-timing section entirely rather than
// telling the finalizer not to request a fast Pulse it was never going to be
// asked about.
func pulseScheduleTimingSummary(manifest *WorkflowManifest) string {
	if manifest == nil {
		return ""
	}
	var next *time.Time
	found := false
	for _, schedule := range manifest.Schedules {
		if !schedule.Enabled || !schedule.PulseReviewOnly {
			continue
		}
		found = true
		candidate := getNextRunTime(schedule.CronExpression, schedule.Timezone)
		if candidate != nil && (next == nil || candidate.Before(*next)) {
			next = candidate
		}
	}
	if !found {
		return ""
	}
	if next == nil {
		return "An enabled dedicated Pulse schedule exists, but its next occurrence could not be determined. Use record_pulse_fast_request only for clear material evidence."
	}
	return fmt.Sprintf("The next dedicated Pulse review is scheduled for %s (in about %s).", next.Format(time.RFC3339), time.Until(*next).Round(time.Minute))
}

// Stop shuts down the scheduler.
func (s *SchedulerService) Stop() {
	s.runCancelsMu.Lock()
	runCancels := s.runCancels
	s.runCancels = make(map[string]context.CancelFunc)
	s.runCancelsMu.Unlock()
	for _, cancel := range runCancels {
		cancel()
	}
	s.mu.Lock()
	s.jobs = make(map[string]*registeredJob)
	s.scheduleFingerprints = make(map[string]string)
	s.mu.Unlock()
	s.stateStoreMu.Lock()
	if s.stateStore != nil {
		if err := s.stateStore.Close(); err != nil {
			scheduleLogf("[SCHEDULER] Failed to close schedule state store: %v", err)
		}
		s.stateStore = nil
	}
	s.stateStoreMu.Unlock()
	scheduleLogf("[SCHEDULER] Stopped")
}

// LoadSchedule registers a schedule for wall-clock evaluation from a ScheduleContext.
func (s *SchedulerService) LoadSchedule(sctx *ScheduleContext) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sched := sctx.Schedule
	runtimeKey := scheduleRuntimeKey(sctx)
	if s.scheduleFingerprints == nil {
		s.scheduleFingerprints = make(map[string]string)
	}

	// Remove existing registration if any.
	delete(s.jobs, runtimeKey)
	calendarPrefix := runtimeKey + "__cal__"
	for key := range s.jobs {
		if strings.HasPrefix(key, calendarPrefix) {
			delete(s.jobs, key)
		}
	}
	// Legacy manifests may still contain a dedicated Pulse cron. It now serves
	// only as a backwards-compatible enablement signal; recurring Pulse is
	// triggered by completion of a normal scheduled run instead.
	if sched.PulseReviewOnly {
		s.scheduleFingerprints[runtimeKey] = scheduleConfigFingerprint(sctx)
		return nil
	}

	// Update workspace index
	s.workspaceIndexMu.Lock()
	s.workspaceIndex[runtimeKey] = sctx.WorkspacePath
	s.workspaceIndexMu.Unlock()

	if err := EnsureWorkflowScheduleExecutionTracker(context.Background(), sctx.WorkspacePath, sched, time.Now().UTC()); err != nil {
		s.logf(sctx, "[SCHEDULER] Warning: failed to initialize execution history for %s: %v", sched.ID, err)
	}

	if !sched.Enabled {
		s.scheduleFingerprints[runtimeKey] = scheduleConfigFingerprint(sctx)
		return nil
	}

	scheduleType := scheduleTypeOrDefault(sched.ScheduleType)
	var nextRun *time.Time
	sctxCopy := *sctx

	if scheduleType == "calendar" {
		// Calendar schedules: register one job per future calendar item.
		for _, item := range sched.CalendarItems {
			runAt, err := calendarItemRunTime(sched, item)
			if err != nil || !runAt.After(time.Now().UTC()) {
				continue
			}
			if nextRun == nil || runAt.Before(*nextRun) {
				runAtCopy := runAt
				nextRun = &runAtCopy
			}
			itemCopy := item
			itemSctx := sctxCopy
			itemSctx.Schedule = scheduleWithCalendarItem(sched, itemCopy)
			itemSctx.CalendarItem = &itemCopy
			calID := fmt.Sprintf("%s__cal__%s_%s", runtimeKey, item.Date, item.Time)
			s.jobs[calID] = &registeredJob{
				sctx:  &itemSctx,
				runAt: &runAt,
			}
		}
		if nextRun == nil {
			s.logf(sctx, "[SCHEDULER] Calendar schedule %s (%s) has no future items; not registering", sched.ID, sched.Name)
		}
	} else {
		// Cron schedules: parse the expression and register for wall-clock eval.
		loc, err := time.LoadLocation(scheduleTimezoneOrDefault(sched.Timezone))
		if err != nil {
			loc = time.UTC
		}
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		cronSched, err := parser.Parse(sched.CronExpression)
		if err != nil {
			return fmt.Errorf("failed to parse cron expression %q: %w", sched.CronExpression, err)
		}
		// Wrap with timezone-aware location
		cronSched = &tzSchedule{inner: cronSched, loc: loc}
		now := time.Now().UTC()
		lastFired := now.Add(-30 * time.Second) // a genuinely new schedule starts with its next future occurrence
		if latest, ok := s.latestCronOccurrence(sctx); ok {
			lastFired = latest
		} else {
			// The scheduler-state DB can legitimately be empty after an upgrade,
			// replacement, or first deployment of durable fire decisions. Resume
			// from the schedule's persisted tracking window instead of treating an
			// old schedule as newly created and silently skipping every occurrence
			// before this process started.
			if windowStart, ok := WorkflowScheduleTrackingWindowStart(context.Background(), sctx.WorkspacePath, sched.ID); ok && windowStart.Before(now) {
				lastFired = windowStart
			}
		}

		s.jobs[runtimeKey] = &registeredJob{
			sctx:      &sctxCopy,
			cronSched: cronSched,
			lastFired: lastFired,
		}
		nextRun = getNextRunTime(sched.CronExpression, sched.Timezone)
	}

	// Initialize runtime state with next run.
	s.updateRuntimeState(runtimeKey, func(state *ScheduleRuntimeState) {
		state.NextRunAt = nextRun
	})
	s.scheduleFingerprints[runtimeKey] = scheduleConfigFingerprint(sctx)

	nextRunStr := "unknown"
	if nextRun != nil {
		nextRunStr = nextRun.Format(time.RFC3339)
	}
	s.logf(sctx, "[SCHEDULER] Registered schedule %s (%s) type=%s cron=%q timezone=%s next_run=%s",
		sched.ID, sched.Name, scheduleType, sched.CronExpression, sched.Timezone, nextRunStr)
	return nil
}

func (s *SchedulerService) latestCronOccurrence(sctx *ScheduleContext) (time.Time, bool) {
	if sctx == nil {
		return time.Time{}, false
	}
	s.stateStoreMu.RLock()
	defer s.stateStoreMu.RUnlock()
	if s.stateStore == nil {
		return time.Time{}, false
	}
	scopeType, scopeID, _ := scheduleStateScope(sctx)
	decision, err := s.stateStore.LatestFireDecision(context.Background(), scopeType, scopeID, sctx.Schedule.ID, "cron")
	if err != nil || decision.ScheduledFor.IsZero() {
		return time.Time{}, false
	}
	if decision.Decision == "attempted" {
		decision.DecisionID = uuid.NewString()
		decision.Decision = "launch_outcome_unknown"
		decision.Reason = "the occurrence was durably attempted, but no later launch outcome was recorded before restart; execution may or may not have started"
		decision.FiredAt = time.Now().UTC()
		if err := s.stateStore.RecordFireDecision(context.Background(), decision); err != nil {
			s.logf(sctx, "[SCHEDULER_STATE] failed to reconcile interrupted attempted occurrence scheduled_for=%s: %v", decision.ScheduledFor.Format(time.RFC3339), err)
		}
	}
	return decision.ScheduledFor.UTC(), true
}

// tzSchedule wraps a cron.Schedule to evaluate in a specific timezone.
type tzSchedule struct {
	inner cron.Schedule
	loc   *time.Location
}

func (tz *tzSchedule) Next(t time.Time) time.Time {
	return tz.inner.Next(t.In(tz.loc))
}

// ReloadSchedule reloads a schedule from its manifest after it's been updated.
func (s *SchedulerService) ReloadSchedule(ctx context.Context, workspacePath string, scheduleID string) error {
	manifest, found, err := ReadWorkflowManifest(ctx, workspacePath)
	if err != nil || !found {
		return fmt.Errorf("failed to read manifest from %s: %w", workspacePath, err)
	}

	// Find the schedule
	for _, sched := range manifest.Schedules {
		if sched.ID == scheduleID {
			return s.LoadSchedule(buildScheduleContext(workspacePath, manifest, sched))
		}
	}

	// Schedule not found — remove it
	return s.removeJobByKey(workflowScheduleRuntimeKey(workspacePath, scheduleID))
}

func (s *SchedulerService) removeJobByKey(key string) error {
	s.mu.Lock()
	delete(s.jobs, key)
	delete(s.scheduleFingerprints, key)
	// Also remove calendar sub-jobs.
	prefix := key + "__cal__"
	for k := range s.jobs {
		if strings.HasPrefix(k, prefix) {
			delete(s.jobs, k)
		}
	}
	s.mu.Unlock()

	s.workspaceIndexMu.Lock()
	delete(s.workspaceIndex, key)
	s.workspaceIndexMu.Unlock()

	s.runtimeStatesMu.Lock()
	if state := s.runtimeStates[key]; state == nil || state.ActiveRunID == "" {
		delete(s.runtimeStates, key)
	}
	s.runtimeStatesMu.Unlock()

	return nil
}

// RemoveJob removes a schedule only when its ID resolves to one loaded scope.
// Scoped callers should use ReloadSchedule or RemoveWorkflowJob so a copied
// schedule cannot remove another workflow's schedule with the same ID.
func (s *SchedulerService) RemoveJob(scheduleID string) error {
	keys := s.loadedScheduleKeys(scheduleID)
	if len(keys) == 0 {
		return nil
	}
	if len(keys) > 1 {
		return fmt.Errorf("schedule ID %q is ambiguous across %d scopes", scheduleID, len(keys))
	}
	return s.removeJobByKey(keys[0])
}

func (s *SchedulerService) RemoveWorkflowJob(workspacePath, scheduleID string) error {
	return s.removeJobByKey(workflowScheduleRuntimeKey(workspacePath, scheduleID))
}

func (s *SchedulerService) loadedScheduleKeys(scheduleID string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, 1)
	for key, job := range s.jobs {
		if strings.Contains(key, "__cal__") || job == nil || job.sctx == nil || job.sctx.Schedule.ID != scheduleID {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// GetRuntimeState is the legacy unscoped lookup. It returns state only when the
// schedule ID resolves to one loaded scope; scoped callers must use the methods
// below so copied workflows and per-user built-ins cannot collide.
func (s *SchedulerService) GetRuntimeState(scheduleID string) ScheduleRuntimeState {
	keys := s.runtimeKeysForScheduleID(scheduleID)
	if len(keys) == 1 {
		return s.getRuntimeStateByKey(keys[0])
	}
	// Preserve tests and pre-migration in-memory state that used a bare key.
	return s.getRuntimeStateByKey(scheduleID)
}

func (s *SchedulerService) GetRuntimeStateForWorkflow(workspacePath, scheduleID string) ScheduleRuntimeState {
	key := workflowScheduleRuntimeKey(workspacePath, scheduleID)
	merged := s.getRuntimeStateByKey(key)
	s.runCancelsMu.Lock()
	ownsActiveRun := merged.ActiveRunID != "" && s.runCancels[merged.ActiveRunID] != nil
	s.runCancelsMu.Unlock()
	_ = s.reconcileWorkflowScheduleRuns(context.Background(), workspacePath, scheduleID)
	runs, err := ReadScheduleRuns(context.Background(), workspacePath)
	if err == nil {
		durableRunID := ""
		// The durable scheduler receipt includes Pulse's terminal outcome;
		// workflow-only file history must not erase a partial/stopped result.
		if !ownsActiveRun {
			latest := -1
			for i := range runs {
				if runs[i].ScheduleID == scheduleID && (latest < 0 || runs[i].StartedAt.After(runs[latest].StartedAt)) {
					latest = i
				}
			}
			if latest >= 0 {
				if status, message, _, ok := s.durableScheduleRunProjection(context.Background(), runs[latest].ID); ok {
					runs[latest].Status, runs[latest].Error = status, message
					durableRunID = runs[latest].ID
				}
			}
		}
		merged = mergeRuntimeStateWithRuns(merged, scheduleID, runs, ownsActiveRun, durableRunID)
	}
	merged.NextRunAt = s.nextWorkflowScheduleRunAt(workspacePath, scheduleID, time.Now())
	s.stateStoreMu.RLock()
	if s.stateStore != nil {
		if pending, pendingErr := s.stateStore.GetPendingOccurrence(context.Background(), "workflow", workspacePath, scheduleID); pendingErr == nil {
			queuedAt, expiresAt := pending.QueuedAt, pending.ExpiresAt
			merged.LastStatus = "waiting_for_workflow"
			merged.LastError = pending.Reason
			merged.WaitingSince = &queuedAt
			merged.WaitingUntil = &expiresAt
			merged.WaitingReason = pending.Reason
			merged.QueuedOccurrences = pending.OccurrenceCount
		}
	}
	s.stateStoreMu.RUnlock()
	return merged
}

func (s *SchedulerService) getRuntimeStateByKey(key string) ScheduleRuntimeState {
	s.runtimeStatesMu.RLock()
	var merged ScheduleRuntimeState
	if state, ok := s.runtimeStates[key]; ok {
		merged = cloneScheduleRuntimeState(state)
	}
	s.runtimeStatesMu.RUnlock()
	return merged
}

func cloneScheduleRuntimeState(state *ScheduleRuntimeState) ScheduleRuntimeState {
	if state == nil {
		return ScheduleRuntimeState{}
	}
	copy := *state
	if state.LastRunAt != nil {
		value := *state.LastRunAt
		copy.LastRunAt = &value
	}
	if state.NextRunAt != nil {
		value := *state.NextRunAt
		copy.NextRunAt = &value
	}
	if state.LastDurationMs != nil {
		value := *state.LastDurationMs
		copy.LastDurationMs = &value
	}
	if state.WaitingSince != nil {
		value := *state.WaitingSince
		copy.WaitingSince = &value
	}
	if state.WaitingUntil != nil {
		value := *state.WaitingUntil
		copy.WaitingUntil = &value
	}
	return copy
}

func (s *SchedulerService) runtimeKeysForScheduleID(scheduleID string) []string {
	s.runtimeStatesMu.RLock()
	defer s.runtimeStatesMu.RUnlock()
	keys := make([]string, 0, 1)
	for key := range s.runtimeStates {
		if scheduleRuntimeKeyHasID(key, scheduleID) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func (s *SchedulerService) reconcileWorkflowScheduleRuns(ctx context.Context, workspacePath, scheduleID string) error {
	if s == nil || s.api == nil || strings.TrimSpace(workspacePath) == "" {
		return nil
	}

	runs, err := ReadScheduleRuns(ctx, workspacePath)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	changed := false
	for i := range runs {
		if runs[i].Status != "running" {
			continue
		}
		if scheduleID != "" && runs[i].ScheduleID != scheduleID {
			continue
		}

		status, errMsg, shouldFinalize := s.reconciledScheduleRunStatus(&runs[i], now)
		if !shouldFinalize {
			continue
		}

		runs[i].Status = status
		runs[i].Error = errMsg
		durationMs := now.Sub(runs[i].StartedAt).Milliseconds()
		if durationMs < 0 {
			durationMs = 0
		}
		runs[i].DurationMs = &durationMs
		runs[i].CompletedAt = &now
		changed = true
	}

	if !changed {
		return nil
	}
	return WriteScheduleRuns(ctx, workspacePath, runs)
}

func (s *SchedulerService) reconciledScheduleRunStatus(run *ScheduleRunEntry, now time.Time) (string, string, bool) {
	if run == nil {
		return "", "", false
	}

	if strings.TrimSpace(run.SessionID) == "" {
		if now.Sub(run.StartedAt) > 10*time.Minute {
			return "error", "interrupted: no session id recorded", true
		}
		return "", "", false
	}

	session, exists := s.api.getActiveSession(run.SessionID)
	if !exists {
		return "error", "interrupted: session not active", true
	}

	switch session.Status {
	case "running":
		return "", "", false
	case "completed":
		// A scheduled run is many sequential turns sharing one session, and
		// between them the session reads "completed" — the previous turn is
		// done, the next has not started. Finalizing here called the whole run
		// a success while it was on turn 1 of 10.
		//
		// rtslatency's 2026-07-31 02:30 run was recorded success/84,599 ms with
		// completed_at 84 seconds after start, while the log shows turns 2-5
		// running for another hour and turn 6 still going when the process shut
		// down. Once stamped, the record never corrected itself, so every run
		// killed by a restart also read as successful and the workflow looked
		// healthy while its digest had not shipped since 2026-07-29.
		//
		// This reconciler exists to clean up runs the scheduler abandoned, and
		// it cannot tell "between turns" from "finished". Success is the
		// scheduler's verdict to record when its own turn loop ends. Past the
		// grace window an abandoned run is reported interrupted, never success.
		if now.Sub(run.StartedAt) <= scheduleRunAbandonedAfter {
			return "", "", false
		}
		return "error", "interrupted: scheduler never recorded a terminal result", true
	case "stopped", "dismissed":
		return "stopped", fmt.Sprintf("session ended with status %s", session.Status), true
	case "error", "failed", "inactive":
		return "error", fmt.Sprintf("session ended with status %s", session.Status), true
	default:
		return "", "", false
	}
}

// scheduleRunAbandonedAfter bounds how long a run may sit between turns before
// the reconciler treats it as abandoned. It must exceed a real run: rtslatency's
// healthy runs take 45 minutes to 2.5 hours, so a shorter window would finalize
// live work as interrupted.
var scheduleRunAbandonedAfter = 6 * time.Hour

func mergeRuntimeStateWithRuns(state ScheduleRuntimeState, scheduleID string, runs []ScheduleRunEntry, ownsActiveRun bool, durableRunID string) ScheduleRuntimeState {
	var filtered []ScheduleRunEntry
	for _, run := range runs {
		if run.ScheduleID == scheduleID {
			filtered = append(filtered, run)
		}
	}
	if len(filtered) == 0 {
		return state
	}

	sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].StartedAt.After(filtered[j].StartedAt) })
	latest := filtered[0]
	if state.RunCount < len(filtered) {
		state.RunCount = len(filtered)
	}
	// File history records the workflow result before Pulse finishes. Preserve
	// the live in-memory state for that same run so the UI cannot report success
	// and admit another trigger while Pulse still owns the session.
	if state.LastStatus == "running" && ownsActiveRun {
		return state
	}

	shouldAdoptLatest := state.LastRunAt == nil || latest.StartedAt.After(*state.LastRunAt)
	sameRun := state.LastRunAt != nil && latest.StartedAt.Equal(*state.LastRunAt)
	// Session identity alone is insufficient: resumed sessions span many runs.
	if state.ActiveRunID != "" && latest.ID == state.ActiveRunID {
		sameRun = true
	}
	staleRunning := state.LastStatus == "running" && !ownsActiveRun && sameRun && latest.Status != "running"

	durableSameRun := !ownsActiveRun && sameRun && durableRunID != "" && latest.ID == durableRunID
	if shouldAdoptLatest || staleRunning || durableSameRun {
		state.ActiveRunID = ""
		startedAt := latest.StartedAt
		state.LastRunAt = &startedAt
		state.LastStatus = latest.Status
		state.LastSessionID = latest.SessionID
		state.LastError = latest.Error
		state.LastDurationMs = latest.DurationMs
		return state
	}

	if sameRun {
		if state.LastStatus == "" {
			state.LastStatus = latest.Status
		}
		if state.LastSessionID == "" {
			state.LastSessionID = latest.SessionID
		}
		if state.LastError == "" && state.LastStatus == latest.Status {
			state.LastError = latest.Error
		}
		if state.LastDurationMs == nil {
			state.LastDurationMs = latest.DurationMs
		}
	}

	return state
}

// GetWorkspaceForSchedule returns the workspace path for a schedule ID.
func (s *SchedulerService) GetWorkspaceForSchedule(scheduleID string) string {
	s.workspaceIndexMu.RLock()
	defer s.workspaceIndexMu.RUnlock()
	match := ""
	for key, workspacePath := range s.workspaceIndex {
		if !scheduleRuntimeKeyHasID(key, scheduleID) {
			continue
		}
		if match != "" && match != workspacePath {
			return ""
		}
		match = workspacePath
	}
	return match
}

// ListFireDecisions returns the durable tick-loop decision log for one
// schedule — including occurrences the scheduler correctly skipped
// (global pause, busy, a queued dependency) and never turned into a
// schedule_runs row at all. get_schedule_runs's own history only shows
// actual runs, so a schedule that looks silent for days there can be a
// scheduler correctly honoring a pause the whole time, not a defect —
// this is the only read path that can tell the two apart (found live on
// confida-login: four Technical Review passes theorized a missing
// misfire-recovery mechanism instead of reading this durable, already-logged
// decision trail).
func (s *SchedulerService) ListFireDecisions(ctx context.Context, workspacePath, scheduleID string, limit int) ([]schedulerstate.FireDecision, error) {
	s.stateStoreMu.RLock()
	store := s.stateStore
	s.stateStoreMu.RUnlock()
	if store == nil {
		return nil, errors.New("schedule state store is unavailable")
	}
	scopeID := filepath.Clean(strings.TrimSpace(workspacePath))
	return store.ListFireDecisions(ctx, "workflow", scopeID, scheduleID, limit)
}

// TriggerNow triggers a schedule immediately (for manual trigger API).
func (s *SchedulerService) TriggerNow(workspacePath string, scheduleID string) (string, error) {
	return s.TriggerNowFromSession(workspacePath, scheduleID, "")
}

// TriggerNowFromSession is TriggerNow with the chat session that asked for the
// run, so the run's terminals can be surfaced in that chat.
func (s *SchedulerService) TriggerNowFromSession(workspacePath, scheduleID, originSessionID string) (string, error) {
	ctx := context.Background()

	manifest, found, err := ReadWorkflowManifest(ctx, workspacePath)
	if err != nil || !found {
		return "", fmt.Errorf("failed to read manifest from %s: %w", workspacePath, err)
	}

	var sched *WorkflowSchedule
	for i := range manifest.Schedules {
		if manifest.Schedules[i].ID == scheduleID {
			sched = &manifest.Schedules[i]
			break
		}
	}
	if sched == nil {
		return "", fmt.Errorf("schedule %s not found in manifest at %s", scheduleID, workspacePath)
	}
	sctx := buildScheduleContext(workspacePath, manifest, *sched)
	sctx.TriggerSource = "manual"
	sctx.OriginSessionID = originSessionID
	startTime := time.Now().UTC()
	sctx.ScheduledFor = startTime
	if disposition, reason := s.scheduleDependencyDisposition(ctx, sctx, startTime); disposition != scheduleDependencyReady {
		if disposition == scheduleDependencyWaiting && s.queueScheduleOccurrence(ctx, sctx, reason, startTime, "retry") {
			return "queued", nil
		}
		_ = s.recordScheduleFireDecision(ctx, sctx, "blocked_dependency", reason, "", startTime)
		return "", errors.New(reason)
	}

	// A workflow may have one interactive builder chat and one schedule at the
	// same time. Other workflow executions still block a schedule start.
	if activeExec := s.findActiveNonBuilderExecutionForWorkspace(workspacePath); activeExec != nil {
		triggeredBy := activeExec.TriggeredBy
		if strings.TrimSpace(triggeredBy) == "" {
			triggeredBy = "unknown"
		}
		err := fmt.Errorf(
			"workflow already has an active %s run (session: %s)",
			triggeredBy,
			activeExec.SessionID,
		)
		if s.recordOrQueueBlockedSchedule(ctx, sctx, err.Error(), startTime) {
			return "queued", nil
		}
		return "", err
	}

	// Reserve the in-memory run and cancellation handle atomically, then claim
	// the durable lease without holding the global runtime-state mutex.
	runtimeKey := workflowScheduleRuntimeKey(workspacePath, scheduleID)
	workflowRuntimeKeys := make([]string, 0, len(manifest.Schedules))
	for i := range manifest.Schedules {
		workflowRuntimeKeys = append(workflowRuntimeKeys, workflowScheduleRuntimeKey(workspacePath, manifest.Schedules[i].ID))
	}
	runID := uuid.NewString()
	s.runtimeStatesMu.Lock()
	state := s.getRuntimeStateLocked(runtimeKey)
	if state.LastStatus == "running" {
		s.runtimeStatesMu.Unlock()
		if s.recordOrQueueBlockedSchedule(ctx, sctx, "schedule is already running", startTime) {
			return "queued", nil
		}
		return "", fmt.Errorf("job is already running (session: %s)", state.LastSessionID)
	}
	if otherKey, otherSession := runningScheduleInSetLocked(s.runtimeStates, workflowRuntimeKeys, runtimeKey); otherKey != "" {
		s.runtimeStatesMu.Unlock()
		if s.recordOrQueueBlockedSchedule(ctx, sctx, "another schedule owns the workflow", startTime) {
			return "queued", nil
		}
		return "", fmt.Errorf("another schedule is already running (session: %s)", otherSession)
	}
	previousState := *state
	runCtx := s.activateScheduleRunLocked(state, runID, startTime)
	s.runtimeStatesMu.Unlock()
	if err := s.claimScheduleRun(ctx, sctx, runID, startTime); err != nil {
		s.rollbackScheduleRunActivation(runtimeKey, runID, previousState)
		if s.recordOrQueueBlockedSchedule(ctx, sctx, err.Error(), startTime) {
			return "queued", nil
		}
		return "", err
	}
	if s.abortCanceledScheduleRunBeforeStart(runCtx, sctx, runtimeKey, runID) {
		return "", context.Canceled
	}
	s.recordScheduleFireDecision(ctx, sctx, "started", "manual trigger accepted", runID, startTime)

	if err := RecordWorkflowScheduleExecution(context.Background(), workspacePath, *sched, startTime); err != nil {
		s.logf(sctx, "[SCHEDULER] Warning: failed to record manual schedule execution for %s: %v", scheduleID, err)
	}

	go func() {
		if _, err := s.runJob(runCtx, sctx, runID); err != nil {
			scheduleLogf("[SCHEDULER] Triggered job %s failed: %v", scheduleID, err)
		}
	}()

	return "triggered", nil
}

// TriggerPulseNow runs version-preflight -> Pulse against the latest
// retained workflow evidence. It does not execute the workflow or change a saved
// schedule.
func (s *SchedulerService) TriggerPulseNow(workspacePath string) (string, error) {
	ctx := context.Background()
	workspacePath = filepath.Clean(strings.TrimSpace(workspacePath))
	if workspacePath == "." || workspacePath == "" {
		return "", errors.New("workspace_path is required")
	}

	manifest, found, err := ReadWorkflowManifest(ctx, workspacePath)
	if err != nil {
		return "", fmt.Errorf("failed to read manifest from %s: %w", workspacePath, err)
	}
	if !found {
		return "", fmt.Errorf("workflow manifest not found at %s", workspacePath)
	}

	sched := WorkflowSchedule{
		ID:             manualWorkflowPulseScheduleID,
		Name:           "Run Pulse",
		Description:    "One-off Pulse review of the latest retained run; not persisted as a schedule",
		ScheduleType:   "cron",
		CronExpression: "",
		Timezone:       "UTC",
		Mode:           "workshop",
		WorkshopMode:   "run",
	}
	sctx := buildScheduleContext(workspacePath, manifest, sched)
	sctx.TriggerSource = "manual"
	sctx.ForcePulseReview = true
	sctx.PulseOnly = true
	sctx.PulseEvidenceRunFolder, sctx.PulseEvidenceRunStatus = latestRetainedPulseEvidence(ctx, workspacePath)
	startTime := time.Now().UTC()

	// One interactive builder chat may coexist with this run, matching normal
	// schedule behavior. Another workflow/schedule execution may not.
	if activeExec := s.findActiveNonBuilderExecutionForWorkspace(workspacePath); activeExec != nil {
		triggeredBy := strings.TrimSpace(activeExec.TriggeredBy)
		if triggeredBy == "" {
			triggeredBy = "unknown"
		}
		return "", fmt.Errorf("workflow already has an active %s run (session: %s)", triggeredBy, activeExec.SessionID)
	}

	runtimeKey := workflowScheduleRuntimeKey(workspacePath, sched.ID)
	workflowRuntimeKeys := make([]string, 0, len(manifest.Schedules)+1)
	for i := range manifest.Schedules {
		workflowRuntimeKeys = append(workflowRuntimeKeys, workflowScheduleRuntimeKey(workspacePath, manifest.Schedules[i].ID))
	}
	workflowRuntimeKeys = append(workflowRuntimeKeys, runtimeKey)
	runID := uuid.NewString()
	s.runtimeStatesMu.Lock()
	state := s.getRuntimeStateLocked(runtimeKey)
	if state.LastStatus == "running" {
		s.runtimeStatesMu.Unlock()
		return "", fmt.Errorf("Pulse run is already active (session: %s)", state.LastSessionID)
	}
	if _, otherSession := runningScheduleInSetLocked(s.runtimeStates, workflowRuntimeKeys, runtimeKey); otherSession != "" {
		s.runtimeStatesMu.Unlock()
		return "", fmt.Errorf("another schedule is already running (session: %s)", otherSession)
	}
	previousState := *state
	runCtx := s.activateScheduleRunLocked(state, runID, startTime)
	s.runtimeStatesMu.Unlock()

	if err := s.claimScheduleRun(ctx, sctx, runID, startTime); err != nil {
		s.rollbackScheduleRunActivation(runtimeKey, runID, previousState)
		return "", err
	}
	if s.abortCanceledScheduleRunBeforeStart(runCtx, sctx, runtimeKey, runID) {
		return "", context.Canceled
	}

	go func() {
		defer s.cleanupRemovedScheduleRuntimeState(runtimeKey)
		if _, runErr := s.runJob(runCtx, sctx, runID); runErr != nil {
			scheduleLogf("[SCHEDULER] One-off Pulse run failed for %s: %v", workspacePath, runErr)
		}
	}()

	return runID, nil
}

func latestRetainedPulseEvidence(ctx context.Context, workspacePath string) (string, string) {
	runs, err := ReadScheduleRuns(ctx, workspacePath)
	if err == nil {
		if runFolder, status, ok := latestRetainedPulseEvidenceFromRuns(runs); ok {
			return runFolder, status
		}
	}

	// iteration-0 is the active retained slot even before schedule history has
	// been written. Pulse can still review plan/config/report evidence when the
	// folder has no completed run artifacts yet.
	return "iteration-0", "unknown"
}

func latestRetainedPulseEvidenceFromRuns(runs []ScheduleRunEntry) (string, string, bool) {
	fallbackFolder := ""
	fallbackStatus := ""
	for _, run := range runs {
		if run.ScheduleID == manualWorkflowPulseScheduleID || strings.TrimSpace(run.RunFolder) == "" {
			continue
		}
		status := strings.TrimSpace(run.Status)
		if status == "" {
			status = "unknown"
		}
		if status == "running" {
			if fallbackFolder == "" {
				fallbackFolder = strings.TrimSpace(run.RunFolder)
				fallbackStatus = status
			}
			continue
		}
		return strings.TrimSpace(run.RunFolder), status, true
	}
	if fallbackFolder != "" {
		return fallbackFolder, fallbackStatus, true
	}
	return "", "", false
}

// StopRunningJob stops a running scheduled job by canceling its session.
func (s *SchedulerService) StopRunningJobForWorkflow(workspacePath, scheduleID string) {
	s.stopRunningJob(workflowScheduleRuntimeKey(workspacePath, scheduleID), scheduleID)
}

func (s *SchedulerService) StopRunningJob(scheduleID string) {
	keys := s.runtimeKeysForScheduleID(scheduleID)
	if len(keys) != 1 {
		return
	}
	s.stopRunningJob(keys[0], scheduleID)
}

func (s *SchedulerService) stopRunningJob(runtimeKey, scheduleID string) {
	s.runtimeStatesMu.Lock()
	state := s.getRuntimeStateLocked(runtimeKey)
	sessionID := state.LastSessionID
	runID := state.ActiveRunID
	state.LastStatus = "stopped"
	state.LastError = "stopped by user"
	state.ActiveRunID = ""
	s.runtimeStatesMu.Unlock()
	if runID == "" {
		lockKey := scheduleStateLockKeyFromRuntimeKey(runtimeKey)
		s.stateStoreMu.RLock()
		store := s.stateStore
		if store != nil {
			if active, err := store.ActiveRunByLockKey(context.Background(), lockKey); err == nil {
				runID = active.RunID
			}
		}
		s.stateStoreMu.RUnlock()
	}
	if runID != "" {
		s.cancelScheduleRunContext(runID)
		s.transitionScheduleRun(context.Background(), nil, schedulerstate.Transition{
			RunID: runID, To: schedulerstate.StateStopped, Reason: "stopped by user", SessionID: sessionID,
			ErrorMessage: "stopped by user", At: time.Now().UTC(),
		})
	}
	if sessionID == "" {
		return
	}

	scheduleLogf("[STOP] ===== stop requested: schedule=%s run=%s session=%s =====", scheduleID, runID, sessionID)
	scheduleLogf("[SCHEDULER] Stopping running job %s (session: %s)", scheduleID, sessionID)
	if isScheduledSession(sessionID) {
		s.api.markSessionTurnInterrupted(sessionID)
	}
	s.cancelScheduledSessionWork(sessionID, "scheduled job stopped by user", runtimePhaseCanceled)

	scheduleLogf("[SCHEDULER] Stopped job %s (session: %s)", scheduleID, sessionID)
	// Closing bracket for the trace. Everything Stop did sits between this and
	// the "stop requested" line above, so one grep gives the whole story.
	scheduleLogf("[STOP] ===== stop teardown complete: schedule=%s session=%s =====", scheduleID, sessionID)
}

// cancelScheduledSessionWork stops agent, workflow, background, and tmux work
// owned by a scheduled session without changing the schedule's recorded run
// result. Pulse timeout recovery uses this before continuing finalization in a
// fresh session.
func (s *SchedulerService) cancelScheduledSessionWork(sessionID, closeReason string, terminalPhase RuntimePhase) {
	if s == nil || s.api == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	s.api.cancelSessionRuntimeWork(sessionID, closeReason, terminalPhase)
}

// triggerSchedule is called by the tick loop when a schedule is due.
func (s *SchedulerService) triggerSchedule(sctx *ScheduleContext, scheduledFor time.Time) {
	triggerCtx := *sctx
	triggerCtx.ScheduledFor = scheduledFor.UTC()
	sctx = &triggerCtx
	schedID := sctx.Schedule.ID
	runtimeKey := scheduleRuntimeKey(sctx)
	now := time.Now()
	ctx := context.Background()
	s.logf(sctx, "[SCHEDULER] ⏰ Cron fired for %s (%s) at %s", schedID, sctx.Schedule.Name, now.Format(time.RFC3339))

	// Late-fire detection: compare to the next_run we recorded last time. Drift > 60s
	// usually means a missed-fire catch-up after macOS sleep/wake, or scheduler stall.
	s.runtimeStatesMu.RLock()
	if st, ok := s.runtimeStates[runtimeKey]; ok && st.NextRunAt != nil {
		expected := *st.NextRunAt
		drift := now.Sub(expected)
		if drift > 60*time.Second {
			s.logf(sctx, "[SCHEDULER] ⚠️ LATE_FIRE schedule=%s expected=%s actual=%s drift=%s",
				schedID, expected.Format(time.RFC3339), now.Format(time.RFC3339), drift.Round(time.Second))
		}
	}
	s.runtimeStatesMu.RUnlock()

	paused, cfg, err := s.IsGloballyPaused(context.Background())
	if err != nil {
		s.logf(sctx, "[SCHEDULER] ⚠️ Failed to read scheduler config before trigger %s: %v", schedID, err)
	} else if paused {
		pausedAt := ""
		if cfg != nil && cfg.PausedAt != nil {
			pausedAt = cfg.PausedAt.Format(time.RFC3339)
		}
		if pausedAt != "" {
			s.logf(sctx, "[SCHEDULER] ⏸️ Global scheduler pause active since %s, skipping %s", pausedAt, schedID)
		} else {
			s.logf(sctx, "[SCHEDULER] ⏸️ Global scheduler pause active, skipping %s", schedID)
		}
		s.recordScheduleFireDecision(ctx, sctx, "skipped_paused", "global scheduler pause is active", "", now.UTC())
		return
	}

	// Reload the workflow manifest so a cron tick always uses the latest schedule.
	var freshCtx *ScheduleContext
	var workflowScheduleIDs []string
	{
		manifest, found, err := ReadWorkflowManifest(context.Background(), sctx.WorkspacePath)
		if err != nil || !found {
			s.logf(sctx, "[SCHEDULER] ❌ Failed to reload manifest for %s: %v", schedID, err)
			s.recordScheduleFireDecision(ctx, sctx, "failed_to_start", "failed to reload workflow manifest", "", now.UTC())
			return
		}

		workflowScheduleIDs = make([]string, 0, len(manifest.Schedules))
		for i := range manifest.Schedules {
			workflowScheduleIDs = append(workflowScheduleIDs, manifest.Schedules[i].ID)
		}

		// Find current schedule in manifest (may have been updated)
		var currentSched *WorkflowSchedule
		for i := range manifest.Schedules {
			if manifest.Schedules[i].ID == schedID {
				currentSched = &manifest.Schedules[i]
				break
			}
		}
		if currentSched == nil {
			s.logf(sctx, "[SCHEDULER] ❌ Schedule %s not found in manifest, skipping", schedID)
			s.recordScheduleFireDecision(ctx, sctx, "failed_to_start", "schedule no longer exists", "", now.UTC())
			return
		}
		if !currentSched.Enabled {
			if err := EnsureWorkflowScheduleExecutionTracker(context.Background(), sctx.WorkspacePath, *currentSched, time.Now().UTC()); err != nil {
				s.logf(sctx, "[SCHEDULER] Warning: failed to sync disabled execution history for %s: %v", schedID, err)
			}
			s.logf(sctx, "[SCHEDULER] ⏭️ Schedule %s is disabled, skipping", schedID)
			s.recordScheduleFireDecision(ctx, sctx, "skipped_disabled", "schedule is disabled", "", now.UTC())
			return
		}

		if activeExec := s.findActiveNonBuilderExecutionForWorkspace(sctx.WorkspacePath); activeExec != nil {
			triggeredBy := activeExec.TriggeredBy
			if strings.TrimSpace(triggeredBy) == "" {
				triggeredBy = "unknown"
			}
			s.logf(sctx, "[SCHEDULER] ⏭️ Workflow %s already has an active %s run (session: %s), skipping schedule %s",
				sctx.WorkspacePath, triggeredBy, activeExec.SessionID, schedID)
			s.recordOrQueueBlockedSchedule(ctx, sctx, "workflow already has an active execution", now.UTC())
			return
		}

		resolvedSchedule, calendarItem, ok := scheduleWithReloadedCalendarItem(*currentSched, sctx.CalendarItem)
		if !ok {
			s.logf(sctx, "[SCHEDULER] Calendar item for %s no longer exists, skipping", schedID)
			s.recordScheduleFireDecision(ctx, sctx, "failed_to_start", "calendar item no longer exists", "", now.UTC())
			return
		}
		freshCtx = buildScheduleContext(sctx.WorkspacePath, manifest, resolvedSchedule)
		freshCtx.CalendarItem = calendarItem
	}
	freshCtx.TriggerSource = strings.TrimSpace(sctx.TriggerSource)
	if freshCtx.TriggerSource == "" {
		freshCtx.TriggerSource = "cron"
	}
	freshCtx.ScheduledFor = scheduledFor.UTC()
	// The reload above rebuilds the context from the manifest, so a resume's
	// identity has to be carried across explicitly or it is silently dropped
	// and the run restarts from step 1 (PLAT-101).
	freshCtx.CapacityResumeRunID = sctx.CapacityResumeRunID
	freshCtx.CapacityResumeRunFolder = sctx.CapacityResumeRunFolder
	freshCtx.CapacityResumeFromStep = sctx.CapacityResumeFromStep
	if freshCtx.CapacityResumeRunID != "" {
		freshCtx.TriggerSource = "capacity_resume"
	}
	startTime := time.Now().UTC()
	freshCtx.QueuedExpiresAt = sctx.QueuedExpiresAt
	freshCtx.QueuedOccurrenceCount = sctx.QueuedOccurrenceCount
	if disposition, reason := s.scheduleDependencyDisposition(ctx, freshCtx, startTime); disposition != scheduleDependencyReady {
		if disposition == scheduleDependencyBlocked {
			s.discardQueuedOccurrence(ctx, freshCtx)
			decision := "blocked_dependency"
			if strings.Contains(reason, "deadline") {
				decision = "expired_dependency_deadline"
			}
			_ = s.recordScheduleFireDecision(ctx, freshCtx, decision, reason, "", startTime)
			return
		}
		s.logf(freshCtx, "[SCHEDULER] ⏳ Queueing %s: %s", schedID, reason)
		s.queueScheduleOccurrence(ctx, freshCtx, reason, startTime, "retry")
		return
	}

	// Reserve in memory before the durable claim so Stop can cancel even while
	// SQLite is claiming the lease. The database call itself runs without the
	// global runtime-state mutex.
	runID := uuid.NewString()
	if freshCtx.CapacityResumeRunID != "" {
		// Continue the suspended run's own identity so history shows one run
		// that waited, not a failed run followed by an unrelated new one.
		runID = freshCtx.CapacityResumeRunID
	}
	runtimeKey = scheduleRuntimeKey(freshCtx)
	s.runtimeStatesMu.Lock()
	state := s.getRuntimeStateLocked(runtimeKey)
	if state.LastStatus == "running" {
		s.runtimeStatesMu.Unlock()
		s.sessionLogf(freshCtx, state.LastSessionID, "[SCHEDULER] ⏭️ Schedule %s is already running (session: %s), skipping", schedID, state.LastSessionID)
		s.recordOrQueueBlockedSchedule(ctx, freshCtx, "schedule is already running", startTime)
		return
	}
	workflowRuntimeKeys := make([]string, 0, len(workflowScheduleIDs))
	for _, workflowScheduleID := range workflowScheduleIDs {
		workflowRuntimeKeys = append(workflowRuntimeKeys, workflowScheduleRuntimeKey(freshCtx.WorkspacePath, workflowScheduleID))
	}
	if otherKey, otherSession := runningScheduleInSetLocked(s.runtimeStates, workflowRuntimeKeys, scheduleRuntimeKey(freshCtx)); otherKey != "" {
		s.runtimeStatesMu.Unlock()
		s.logf(freshCtx, "[SCHEDULER] ⏭️ Workflow %s already has running schedule %s (session: %s), skipping schedule %s",
			freshCtx.WorkspacePath, otherKey, otherSession, schedID)
		s.recordOrQueueBlockedSchedule(ctx, freshCtx, "another schedule owns the workflow", startTime)
		return
	}
	if freshCtx.CapacityResumeRunID == "" {
		if window, resetsAt, blocked := s.scheduleQuotaBlock(ctx, freshCtx, now); blocked {
			s.runtimeStatesMu.Unlock()
			// The account's window is at its limit, so this run cannot even
			// reach step one. Starting it would record a red run and, once
			// steps begin having side effects, replay them for nothing.
			reason := fmt.Sprintf("provider %s window is exhausted until %s", window, resetsAt.UTC().Format(time.RFC3339))
			s.logf(freshCtx, "[SCHEDULER] ⏭️ Skipping %s: %s", schedID, reason)
			s.recordScheduleFireDecision(ctx, freshCtx, "skipped_waiting_for_capacity", reason, "", startTime)
			return
		}
		if waitingRun, wait := s.outstandingCapacityWait(ctx, freshCtx.WorkspacePath); waitingRun != nil {
			s.runtimeStatesMu.Unlock()
			// Firing here is what turned one capacity wall into a run storm: the
			// new run restarts from step 1, replays the completed steps' side
			// effects, and hits the same wall — every tick until the window
			// happens to reopen. The suspended run resumes itself instead.
			reason := "a run is suspended on provider capacity: " + wait.Describe()
			s.logf(freshCtx, "[SCHEDULER] ⏭️ Skipping %s: %s", schedID, reason)
			s.recordScheduleFireDecision(ctx, freshCtx, "skipped_waiting_for_capacity", reason, waitingRun.ID, startTime)
			return
		}
	}
	previousState := *state
	runCtx := s.activateScheduleRunLocked(state, runID, startTime)
	s.runtimeStatesMu.Unlock()
	if err := s.claimScheduleRun(ctx, freshCtx, runID, startTime); err != nil {
		s.rollbackScheduleRunActivation(runtimeKey, runID, previousState)
		s.recordOrQueueBlockedSchedule(ctx, freshCtx, err.Error(), startTime)
		s.logf(freshCtx, "[SCHEDULER] ⏭️ Durable run lease rejected schedule %s: %v", schedID, err)
		return
	}
	if s.abortCanceledScheduleRunBeforeStart(runCtx, freshCtx, runtimeKey, runID) {
		return
	}
	s.recordScheduleFireDecision(ctx, freshCtx, "started", "cron fire accepted", runID, startTime)

	if err := RecordWorkflowScheduleExecution(context.Background(), freshCtx.WorkspacePath, freshCtx.Schedule, startTime); err != nil {
		s.logf(freshCtx, "[SCHEDULER] Warning: failed to record scheduled execution for %s: %v", schedID, err)
	}

	s.logf(freshCtx, "[SCHEDULER] 🚀 Starting %s (%s)", schedID, freshCtx.Schedule.Name)
	if _, err := s.runJob(runCtx, freshCtx, runID); err != nil {
		s.logf(freshCtx, "[SCHEDULER] ❌ %s failed: %v", schedID, err)
	} else {
		s.logf(freshCtx, "[SCHEDULER] ✅ %s completed", schedID)
	}
}

func (s *SchedulerService) discardQueuedOccurrence(ctx context.Context, sctx *ScheduleContext) {
	if sctx == nil || strings.TrimSpace(sctx.TriggerSource) != "queued" {
		return
	}
	scopeType, scopeID, _ := scheduleStateScope(sctx)
	s.stateStoreMu.RLock()
	defer s.stateStoreMu.RUnlock()
	if s.stateStore != nil {
		_ = s.stateStore.DeletePendingOccurrence(ctx, scopeType, scopeID, sctx.Schedule.ID)
	}
}

const defaultQueuedScheduleStartDelay = 3 * time.Hour

func (s *SchedulerService) recordOrQueueBlockedSchedule(ctx context.Context, sctx *ScheduleContext, reason string, at time.Time) bool {
	if sctx == nil {
		return false
	}
	policy := strings.TrimSpace(sctx.Schedule.CollisionPolicy)
	if policy == "" {
		policy = "skip"
	}
	if policy == "skip" {
		_ = s.recordScheduleFireDecision(ctx, sctx, "skipped_busy", reason, "", at)
		return false
	}
	return s.queueScheduleOccurrence(ctx, sctx, reason, at, policy)
}

func (s *SchedulerService) queueScheduleOccurrence(ctx context.Context, sctx *ScheduleContext, reason string, at time.Time, policy string) bool {
	if sctx == nil {
		return false
	}
	delay := defaultQueuedScheduleStartDelay
	if minutes := sctx.Schedule.MaxStartDelayMinutes; minutes > 0 {
		delay = time.Duration(minutes) * time.Minute
	}
	scopeType, scopeID, _ := scheduleStateScope(sctx)
	scheduledFor := sctx.ScheduledFor
	if scheduledFor.IsZero() {
		scheduledFor = at
	}
	expiresAt := at.Add(delay).UTC()
	if !sctx.QueuedExpiresAt.IsZero() {
		expiresAt = sctx.QueuedExpiresAt.UTC()
	}
	pending := schedulerstate.PendingOccurrence{
		ScopeType: scopeType, ScopeID: scopeID, ScheduleID: sctx.Schedule.ID,
		ScheduledFor: scheduledFor.UTC(), QueuedAt: at.UTC(), ExpiresAt: expiresAt,
		LatestScheduledFor: scheduledFor.UTC(), TriggerSource: strings.TrimSpace(sctx.TriggerSource),
		Policy: policy, OccurrenceCount: 1, Reason: reason,
	}
	if pending.TriggerSource == "" {
		pending.TriggerSource = "cron"
	}
	s.stateStoreMu.RLock()
	store := s.stateStore
	var err error
	if store != nil {
		err = store.UpsertPendingOccurrence(ctx, pending)
	} else {
		err = errors.New("schedule state store is unavailable")
	}
	s.stateStoreMu.RUnlock()
	if err != nil {
		s.logf(sctx, "[SCHEDULER] failed to queue occurrence; recording skipped_busy: %v", err)
		_ = s.recordScheduleFireDecision(ctx, sctx, "skipped_busy", reason+"; queue persistence failed: "+err.Error(), "", at)
		return false
	}
	decision := "queued_busy"
	if strings.TrimSpace(sctx.Schedule.AfterScheduleID) != "" && policy == "retry" {
		decision = "waiting_for_dependency"
	} else if policy == "coalesce" {
		decision = "coalesced_busy"
	} else if policy == "retry" {
		decision = "queued_retry"
	}
	_ = s.recordScheduleFireDecision(ctx, sctx, decision, reason, "", at)
	s.markScheduleWaiting(sctx, reason, expiresAt)
	return true
}

func (s *SchedulerService) markScheduleWaiting(sctx *ScheduleContext, reason string, expiresAt time.Time) {
	if sctx == nil {
		return
	}
	now := time.Now().UTC()
	count := 1
	s.stateStoreMu.RLock()
	if s.stateStore != nil {
		scopeType, scopeID, _ := scheduleStateScope(sctx)
		if pending, err := s.stateStore.GetPendingOccurrence(context.Background(), scopeType, scopeID, sctx.Schedule.ID); err == nil && pending.OccurrenceCount > 0 {
			count = pending.OccurrenceCount
		}
	}
	s.stateStoreMu.RUnlock()
	s.updateRuntimeState(scheduleRuntimeKey(sctx), func(state *ScheduleRuntimeState) {
		state.LastStatus = "waiting_for_workflow"
		state.LastError = reason
		state.WaitingSince = &now
		state.WaitingUntil = &expiresAt
		state.WaitingReason = reason
		state.QueuedOccurrences = count
	})
}

type scheduleDependencyResult int

const (
	scheduleDependencyReady scheduleDependencyResult = iota
	scheduleDependencyWaiting
	scheduleDependencyBlocked
)

func (s *SchedulerService) scheduleDependencyDisposition(ctx context.Context, sctx *ScheduleContext, now time.Time) (scheduleDependencyResult, string) {
	dependencyID := strings.TrimSpace(sctx.Schedule.AfterScheduleID)
	if dependencyID == "" {
		return scheduleDependencyReady, ""
	}
	scopeType, scopeID, _ := scheduleStateScope(sctx)
	location, err := time.LoadLocation(strings.TrimSpace(sctx.Schedule.Timezone))
	if err != nil {
		location = time.UTC
	}
	occurrence := sctx.ScheduledFor
	if occurrence.IsZero() {
		occurrence = time.Now()
	}
	localOccurrence := occurrence.In(location)
	year, month, day := localOccurrence.Date()
	dayStart := time.Date(year, month, day, 0, 0, 0, 0, location).UTC()
	dayEnd := dayStart.In(location).AddDate(0, 0, 1).UTC()
	// A later occurrence of the prerequisite must never release an older
	// dependent occurrence after restart. Limit the lookup to prerequisite
	// occurrences scheduled no later than this dependent's own durable time.
	if dependentEnd := occurrence.UTC().Add(time.Nanosecond); dependentEnd.Before(dayEnd) {
		dayEnd = dependentEnd
	}
	if deadline := strings.TrimSpace(sctx.Schedule.DependencyDeadline); deadline != "" {
		parsed, _ := time.Parse("15:04", deadline)
		deadlineAt := time.Date(year, month, day, parsed.Hour(), parsed.Minute(), 0, 0, location)
		if !now.Before(deadlineAt) {
			return scheduleDependencyBlocked, fmt.Sprintf("dependency deadline %s %s passed before prerequisite %s released", deadline, location, dependencyID)
		}
	}
	s.stateStoreMu.RLock()
	store := s.stateStore
	if store == nil {
		s.stateStoreMu.RUnlock()
		return scheduleDependencyWaiting, "dependency state is unavailable"
	}
	run, err := store.RunForScheduleOccurrence(ctx, scopeType, scopeID, dependencyID, dayStart, dayEnd)
	s.stateStoreMu.RUnlock()
	if err != nil {
		if errors.Is(err, schedulerstate.ErrRunNotFound) {
			return scheduleDependencyWaiting, fmt.Sprintf("waiting for prerequisite schedule %s occurrence on %04d-%02d-%02d", dependencyID, year, month, day)
		}
		return scheduleDependencyWaiting, fmt.Sprintf("could not read prerequisite schedule %s occurrence: %v", dependencyID, err)
	}
	if !schedulerstate.IsTerminal(run.State) {
		return scheduleDependencyWaiting, fmt.Sprintf("prerequisite schedule %s occurrence %s is still %s", dependencyID, run.RunID, run.State)
	}
	terminalPolicy := strings.TrimSpace(sctx.Schedule.AfterTerminalStatus)
	if terminalPolicy == "" {
		terminalPolicy = "completed"
	}
	if terminalPolicy == "completed" && run.State != schedulerstate.StateCompleted {
		return scheduleDependencyBlocked, fmt.Sprintf("prerequisite schedule %s occurrence %s ended %s; completed is required", dependencyID, run.RunID, run.State)
	}
	if delay := sctx.Schedule.AfterDelayMinutes; delay > 0 && run.CompletedAt != nil {
		releaseAt := run.CompletedAt.Add(time.Duration(delay) * time.Minute)
		if now.Before(releaseAt) {
			return scheduleDependencyWaiting, fmt.Sprintf("prerequisite schedule %s completed; waiting until %s for configured delay", dependencyID, releaseAt.Format(time.RFC3339))
		}
	}
	return scheduleDependencyReady, ""
}

func (s *SchedulerService) resumeQueuedScheduleOccurrences(ctx context.Context, now time.Time) {
	if !s.queuedResumeMu.TryLock() {
		return
	}
	defer s.queuedResumeMu.Unlock()
	s.stateStoreMu.RLock()
	store := s.stateStore
	if store == nil {
		s.stateStoreMu.RUnlock()
		return
	}
	pending, err := store.ListPendingOccurrences(ctx)
	s.stateStoreMu.RUnlock()
	if err != nil {
		scheduleLogf("[SCHEDULER] queued occurrence scan failed: %v", err)
		return
	}
	for _, item := range pending {
		// Expiry is durable queue state and does not depend on the manifest still
		// being readable. Drop expired rows first so a renamed/deleted workflow
		// cannot leave an immortal pending occurrence behind.
		if !now.Before(item.ExpiresAt) {
			s.stateStoreMu.RLock()
			_ = store.DeletePendingOccurrence(ctx, item.ScopeType, item.ScopeID, item.ScheduleID)
			s.stateStoreMu.RUnlock()
			expiredCtx := buildScheduleContext(item.ScopeID, &WorkflowManifest{}, WorkflowSchedule{ID: item.ScheduleID})
			expiredCtx.TriggerSource = "queued"
			expiredCtx.ScheduledFor = item.ScheduledFor
			_ = s.recordScheduleFireDecision(ctx, expiredCtx, "expired_busy", "queued occurrence exceeded max_start_delay", "", now)
			s.updateRuntimeState(workflowScheduleRuntimeKey(item.ScopeID, item.ScheduleID), func(state *ScheduleRuntimeState) {
				state.LastStatus = "error"
				state.LastError = "queued occurrence exceeded max_start_delay"
				state.WaitingSince = nil
				state.WaitingUntil = nil
				state.WaitingReason = ""
				state.QueuedOccurrences = 0
			})
			continue
		}
		manifest, found, readErr := ReadWorkflowManifest(ctx, item.ScopeID)
		if readErr != nil || !found {
			continue
		}
		var schedule *WorkflowSchedule
		for i := range manifest.Schedules {
			if manifest.Schedules[i].ID == item.ScheduleID {
				schedule = &manifest.Schedules[i]
				break
			}
		}
		if schedule == nil || !schedule.Enabled || !now.Before(item.ExpiresAt) {
			s.stateStoreMu.RLock()
			_ = store.DeletePendingOccurrence(ctx, item.ScopeType, item.ScopeID, item.ScheduleID)
			s.stateStoreMu.RUnlock()
			if schedule != nil {
				sctx := buildScheduleContext(item.ScopeID, manifest, *schedule)
				sctx.TriggerSource = "queued"
				sctx.ScheduledFor = item.ScheduledFor
				sctx.QueuedExpiresAt = item.ExpiresAt
				sctx.QueuedOccurrenceCount = item.OccurrenceCount
				decision := "expired_busy"
				reason := "queued occurrence exceeded max_start_delay"
				if !schedule.Enabled {
					decision, reason = "skipped_disabled", "schedule disabled while queued"
				}
				_ = s.recordScheduleFireDecision(ctx, sctx, decision, reason, "", now)
			}
			continue
		}
		// Keep the row until triggerSchedule acquires the durable workflow lease.
		// BeginQueuedRun consumes the row and inserts the run in one SQLite
		// transaction. A busy lease, cancellation, or process crash therefore
		// leaves this exact occurrence recoverable on the next scan.
		sctx := buildScheduleContext(item.ScopeID, manifest, *schedule)
		sctx.TriggerSource = "queued"
		sctx.ScheduledFor = item.ScheduledFor
		sctx.QueuedExpiresAt = item.ExpiresAt
		sctx.QueuedOccurrenceCount = item.OccurrenceCount
		launchKey := workflowScheduleRuntimeKey(item.ScopeID, item.ScheduleID)
		if !s.beginQueuedLaunch(launchKey) {
			continue
		}
		go func() {
			defer s.endQueuedLaunch(launchKey)
			s.triggerSchedule(sctx, item.ScheduledFor)
		}()
	}
}

func (s *SchedulerService) beginQueuedLaunch(key string) bool {
	s.queuedLaunchMu.Lock()
	defer s.queuedLaunchMu.Unlock()
	if s.queuedLaunching == nil {
		s.queuedLaunching = make(map[string]bool)
	}
	if s.queuedLaunching[key] {
		return false
	}
	s.queuedLaunching[key] = true
	return true
}

func (s *SchedulerService) endQueuedLaunch(key string) {
	s.queuedLaunchMu.Lock()
	delete(s.queuedLaunching, key)
	s.queuedLaunchMu.Unlock()
}

// runJob executes a scheduled job: updates runtime state, creates run history, executes, updates results.
func (s *SchedulerService) runJob(ctx context.Context, sctx *ScheduleContext, runID string) (string, error) {
	defer s.releaseScheduleRunContext(runID)
	defer s.maintainRunLease(ctx, runID)()
	schedID := sctx.Schedule.ID
	runtimeKey := scheduleRuntimeKey(sctx)
	startTime := time.Now().UTC()
	s.logf(sctx, "[SCHEDULER] runJob starting for %s (%s) at %s, groups=%v",
		schedID, sctx.Schedule.Name, startTime.Format(time.RFC3339), sctx.Schedule.GroupNames)
	if err := ctx.Err(); err != nil {
		s.updateRuntimeState(runtimeKey, func(state *ScheduleRuntimeState) {
			state.ActiveRunID = ""
			state.LastStatus = "stopped"
			state.LastError = "stopped by user before execution started"
		})
		s.transitionScheduleRun(context.Background(), sctx, schedulerstate.Transition{
			RunID: runID, To: schedulerstate.StateStopped, Reason: "stopped before workflow execution started",
			ErrorMessage: "stopped by user", At: time.Now().UTC(),
		})
		return "", errors.Join(errWorkshopSequenceInterrupted, err)
	}
	if strings.TrimSpace(runID) == "" {
		return "", errors.Join(errWorkshopSequenceInterrupted, errors.New("scheduled run is missing its run id"))
	}
	s.runtimeStatesMu.RLock()
	activeState := s.runtimeStates[runtimeKey]
	ownsActiveRun := activeState != nil && activeState.LastStatus == "running" && activeState.ActiveRunID == runID
	s.runtimeStatesMu.RUnlock()
	if !ownsActiveRun {
		return "", errors.Join(errWorkshopSequenceInterrupted, fmt.Errorf("scheduled run %s no longer owns %s", runID, runtimeKey))
	}

	// Clear session/error fields — status is already "running" (set atomically by caller)
	s.updateRuntimeState(runtimeKey, func(state *ScheduleRuntimeState) {
		state.LastSessionID = ""
		state.LastError = ""
	})

	// Create run history entry (file-based)
	if strings.TrimSpace(runID) == "" {
		runID = uuid.New().String()
	}
	triggerSource := strings.TrimSpace(sctx.TriggerSource)
	if triggerSource == "" {
		triggerSource = "cron"
	}
	var scheduledFor *time.Time
	if !sctx.ScheduledFor.IsZero() {
		value := sctx.ScheduledFor.UTC()
		scheduledFor = &value
	}
	run := &ScheduleRunEntry{
		ID:            runID,
		ScheduleID:    schedID,
		TriggerSource: triggerSource,
		ScheduledFor:  scheduledFor,
		Status:        "running",
		GroupNames:    sctx.Schedule.GroupNames,
		StartedAt:     startTime,
	}
	if sctx.CapacityResumeRunID != "" {
		// A resumed run continues its own history row rather than opening a
		// second one. Two rows would read as two runs, when what happened is one
		// run that waited — and it would leave the first row reporting
		// waiting_for_capacity forever, which suppresses the schedule.
		if err := UpdateScheduleRun(ctx, sctx.WorkspacePath, sctx.CapacityResumeRunID, "running", "", nil, sctx.CapacityResumeRunFolder, ""); err != nil {
			s.logf(sctx, "[SCHEDULER] Failed to reopen waiting run entry %s: %v", sctx.CapacityResumeRunID, err)
		}
	} else if err := AppendScheduleRun(ctx, sctx.WorkspacePath, run); err != nil {
		s.logf(sctx, "[SCHEDULER] Failed to create run entry for %s: %v", schedID, err)
	}
	startReason := "workflow execution starting"
	if sctx.PulseOnly {
		startReason = "Pulse version preflight starting; workflow execution is skipped"
	}
	s.transitionScheduleRun(ctx, sctx, schedulerstate.Transition{
		RunID:  runID,
		To:     schedulerstate.StateWorkflowRunning,
		Reason: startReason,
		At:     time.Now().UTC(),
	})
	if sctx.PulseOnly {
		// A cron/manual Pulse that starts before the tick loop dispatches a
		// pending fast request already satisfies the request. Coalescing here
		// prevents a second review of the same evidence one minute later.
		if err := markFastPulseRequestDelivered(ctx, sctx.WorkspacePath, runID); err != nil {
			s.logf(sctx, "[PULSE] failed to consume pending fast request: %v", err)
		}
	}

	// Execute
	sessionID, runFolder, execErr := s.executeJob(ctx, sctx, runID)

	// Calculate results
	durationMs := time.Since(startTime).Milliseconds()

	status := "success"
	errMsg := ""
	userInterrupted := errors.Is(execErr, errWorkshopSequenceInterrupted) || errors.Is(execErr, context.Canceled)
	if userInterrupted {
		status = "stopped"
		if execErr != nil {
			errMsg = execErr.Error()
		} else {
			errMsg = "stopped by user"
			execErr = errWorkshopSequenceInterrupted
		}
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] %s stopped by user after %dms", schedID, durationMs)
	} else if wait, waiting := s.turnLevelCapacityWait(ctx, sctx, execErr, runFolder, time.Now().UTC()); waiting {
		// The wall landed before any step ran, so no step recorded where to
		// resume from. Suspend anyway: the run executed nothing, so restarting
		// it from the top when capacity returns is safe.
		status = scheduleRunStatusWaitingForCapacity
		errMsg = wait.Describe()
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] ⏸️ %s suspended before any step ran: %s", schedID, errMsg)
	} else if wait, waiting := s.classifyCapacityWait(ctx, sctx, execErr, runFolder, startTime); waiting {
		// Not a failure. The run stopped because the provider has no capacity
		// left, holds completed steps whose side effects must not be replayed,
		// and continues from the same step once the window reopens (PLAT-101).
		status = scheduleRunStatusWaitingForCapacity
		errMsg = wait.Describe()
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] ⏸️ %s suspended after %dms: %s", schedID, durationMs, errMsg)
	} else if execErr != nil {
		status = "error"
		errMsg = execErr.Error()
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] %s failed in %dms: %v", schedID, durationMs, execErr)
	} else {
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] %s completed in %dms, session: %s, folder: %s", schedID, durationMs, sessionID, runFolder)
	}
	finishedReason := "workflow execution finished with status " + status
	finishedSessionKind := "workflow"
	if sctx.PulseOnly {
		finishedReason = "Pulse version preflight finished with status " + status + "; workflow execution was skipped"
		finishedSessionKind = "pulse"
	}
	s.transitionScheduleRun(ctx, sctx, schedulerstate.Transition{
		RunID:        runID,
		To:           schedulerstate.StateWorkflowFinished,
		Reason:       finishedReason,
		SessionID:    sessionID,
		SessionKind:  finishedSessionKind,
		RunFolder:    runFolder,
		ErrorMessage: errMsg,
		At:           time.Now().UTC(),
	})

	// Keep the runtime state as "running" until all post-run side effects finish.
	// Pulse runs as several resumed builder-chat turns after the workflow result
	// is recorded; if we mark the schedule successful before Pulse finishes, a
	// frequent cron can start the next workflow run while Pulse is between steps.
	// That makes the next Pulse turn fail with workflow_busy (commonly after the
	// LLM/cost/time report), so cadence/backup/publish/notify never run.
	s.updateRuntimeState(runtimeKey, func(state *ScheduleRuntimeState) {
		state.LastSessionID = sessionID
	})

	// Update run history entry for the workflow run. Post-run Pulse
	// may continue after this, but it does not change the recorded run result.
	pulseResult := pulseLifecycleNotRun
	if err := UpdateScheduleRun(ctx, sctx.WorkspacePath, runID, status, errMsg, &durationMs, runFolder, sessionID); err != nil {
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] Failed to update run entry for %s: %v", schedID, err)
	}

	// Pulse runs after a normal schedule only when its effective pulse mode is
	// basic or full. Basic performs backup, report publishing, and run-summary
	// notification only. Full additionally gates, reviews, repairs, and sends
	// the Pulse summary. workflow.json pulse.enabled supplies the default;
	// schedule pulse_mode can explicitly override it. There is no independent cron.
	// Manual one-off Pulse explicitly forces the same lifecycle.
	// Never affects the run's recorded result.
	// A blocked contract-upgrade preflight is the one failure where Pulse has
	// nothing to steward: the workflow never executed, so there is no
	// evidence to gate on, nothing to review, and nothing to publish. All a
	// pass can do is spend an LLM turn restating the blocker the upgrade turn
	// already reported — on every trigger, for as long as the workflow waits
	// on an owner decision. Skip it and let the preflight error be the record.
	upgradeBlocked := errors.Is(execErr, errWorkflowUpgradePreflightBlocked)
	if upgradeBlocked {
		s.sessionLogf(sctx, sessionID, "[PULSE] skipped for %s: the contract-upgrade preflight blocked, so the workflow did not run and there is no evidence to review", schedID)
	}
	if !upgradeBlocked && !userInterrupted && runFolder != "" {
		if manifest, found, mErr := ReadWorkflowManifest(ctx, sctx.WorkspacePath); mErr == nil && found && shouldRunPulseLifecycle(sctx, manifest) {
			pulseMode := effectiveSchedulePulseMode(sctx, manifest)
			pulseEvidenceStatus := status
			if sctx.PulseOnly && strings.TrimSpace(sctx.PulseEvidenceRunStatus) != "" {
				pulseEvidenceStatus = sctx.PulseEvidenceRunStatus
			}
			// Manual Pulse enters its explicit lifecycle state. A normal schedule
			// continues post-run in the same session against the run it just made.
			if sctx.PulseOnly {
				s.transitionScheduleRun(ctx, sctx, schedulerstate.Transition{
					RunID: runID, To: schedulerstate.StatePulseGate, Reason: "dedicated Pulse review started", SessionID: sessionID, SessionKind: "pulse", At: time.Now().UTC(),
				})
			}
			// Pass the run's sessionID so the next message resumes the SAME chat.
			pulseResult = s.runPulseLifecycle(ctx, sctx, pulseMode, pulseEvidenceStatus, runFolder, sessionID, runID, errMsg)
		}
	}

	// Now the whole scheduled job, including post-run side effects, is done.
	terminalState := schedulerstate.StateCompleted
	if userInterrupted {
		terminalState = schedulerstate.StateStopped
	} else if status == "error" {
		terminalState = schedulerstate.StateFailed
	} else if pulseResult == pulseLifecyclePartial {
		terminalState = schedulerstate.StatePartial
	} else if pulseResult == pulseLifecycleStopped {
		terminalState = schedulerstate.StateStopped
	}
	overallStatus := status
	overallError := errMsg
	if terminalState == schedulerstate.StateStopped {
		overallStatus = "stopped"
		if overallError == "" {
			overallError = "stopped by user"
		}
	} else if terminalState == schedulerstate.StatePartial {
		overallStatus = "partial"
		if overallError == "" {
			overallError = "Pulse completed partially"
		}
	}
	if sctx.PulseOnly {
		durationMs = time.Since(startTime).Milliseconds()
		if err := UpdateScheduleRun(ctx, sctx.WorkspacePath, runID, overallStatus, overallError, &durationMs, runFolder, sessionID); err != nil {
			s.sessionLogf(sctx, sessionID, "[PULSE] failed to finalize one-off Pulse run history: %v", err)
		}
	}
	nextRun := s.nextWorkflowScheduleRunAt(sctx.WorkspacePath, schedID, time.Now())
	s.updateRuntimeState(runtimeKey, func(state *ScheduleRuntimeState) {
		state.ActiveRunID = ""
		state.LastStatus = overallStatus
		state.LastError = overallError
		state.LastDurationMs = &durationMs
		state.NextRunAt = nextRun
		state.RunCount++
		if overallStatus == "error" {
			state.ConsecutiveFailures++
		} else {
			state.ConsecutiveFailures = 0
		}
	})
	terminalReason := "scheduled run finished with status " + status
	if sctx.PulseOnly {
		terminalReason = "one-off Pulse finished with status " + overallStatus + "; workflow execution was skipped"
	}
	s.transitionScheduleRun(ctx, sctx, schedulerstate.Transition{
		RunID: runID, To: terminalState, Reason: terminalReason,
		SessionID: sessionID, ErrorMessage: overallError, At: time.Now().UTC(),
	})
	s.cleanupRemovedScheduleRuntimeState(runtimeKey)

	return sessionID, execErr
}

// runPulseLifecycle continues the scheduled run's main-agent conversation with
// three conceptual turns: Gate, optional Review+Fix, and Finalize.
// The agent owns reviewer selection, specialist delegation, diagnosis, repair,
// and verification. Go supplies tools and permissions, preserves turn ordering,
// and validates the typed durable receipts between turns. Pulse never changes
// the workflow run's recorded status; post-run failures are logged separately.
type pulseLifecycleResult string

const (
	pulseLifecycleNotRun    pulseLifecycleResult = "not_run"
	pulseLifecycleCompleted pulseLifecycleResult = "completed"
	pulseLifecyclePartial   pulseLifecycleResult = "partial"
	pulseLifecycleStopped   pulseLifecycleResult = "stopped"
)

func (s *SchedulerService) runPulseLifecycle(ctx context.Context, sctx *ScheduleContext, pulseMode, runStatus, runFolder, runSessionID, scheduleRunID, runFailureReason string) (pulseResult pulseLifecycleResult) {
	pulseResult = pulseLifecyclePartial
	var reviewFixStartedAt, reviewFixCompletedAt time.Time

	// Resume the SAME session the workflow run just used, so Pulse continues in the
	// run's chat thread — the user sees the run and its post-run steward as one
	// conversation, not a fresh session spun up out of nowhere. Fall back to a new id
	// only if the run somehow didn't record one.
	sessionID := strings.TrimSpace(runSessionID)
	if sessionID == "" {
		sessionID = s.newScheduleSessionID(sctx)
	}
	pulseRunID := sessionID
	defer func() {
		if r := recover(); r != nil {
			s.logf(sctx, "[PULSE] post-run pulse panic (recovered): %v", r)
		}
	}()
	baseReqMap := s.buildWorkshopRequest(ctx, sctx)
	if err := initializePulseFinalCommandStates(ctx, sctx.WorkspacePath, pulseRunID); err != nil {
		s.sessionLogf(sctx, sessionID, "[PULSE] failed to initialize final command state: %v", err)
		return
	}

	// Pulse is one continuing agent conversation. Go sends three ordered turns:
	// Gate, Review+Fix, and Finalize. The selected module's terminal result is
	// the single durable completion boundary; Go preserves ordering while agents
	// own the semantic choices and per-finding lifecycle.
	pulseContext := "A scheduled run of this workflow just finished"
	if pulseMode == schedulePulseModeBasic {
		pulseContext = "A scheduled run of this workflow just finished; its schedule requests basic post-run finalization only"
	}
	if sctx.PulseOnly {
		pulseContext = "This is a manual Pulse-only review of the latest retained workflow evidence. The workflow was not executed by this action"
	}
	intro := pulseLifecycleIntro(pulseContext, sctx.WorkspacePath, pulseRunID, runStatus, runFolder)

	// Pulse does not carry contract upgrades. It used to: b4e4fc14 (2026-07-08)
	// delivered them through this Review+Fix turn, and f58ac5b5 (2026-07-16)
	// replaced that with the blocking pre-run preflight — "contract upgrades are
	// a blocking preflight, not post-run cleanup" — without removing the older
	// path. Both stayed live, and the difference matters: the preflight runs one
	// rung per turn and re-reads the manifest to verify that exact target before
	// advancing, while this path concatenated every outstanding rung into a
	// single review turn with no verification or ordering between them. Observed
	// on confida-login 2026-08-12, where a failed-open preflight left four
	// migrations (1.0.21/22/23/25) bundled into one dispatch. The preflight owns
	// migrations; it retries on the next trigger.
	//
	// Pulse shares the scheduler's session, so withdraw any stamp authorization
	// the preflight left behind rather than letting it outlive its turn.
	contractupgrade.Revoke(sessionID)
	defer contractupgrade.Revoke(sessionID)
	s.sessionLogf(sctx, sessionID, "[PULSE] starting pulse for %s (run_folder=%s status=%s)", sctx.Schedule.ID, runFolder, runStatus)
	introSent := false
	recoveryNotes := []string{}
	runStep := func(st pulseLifecycleStep) pulseLifecycleStepRunResult {
		reqMap := cloneStringInterfaceMap(baseReqMap)
		markPulseLifecycleTurn(reqMap)
		query := st.query
		if st.label == "finalize" {
			query += pulseReviewFixCostContext(s.api.costLedger, sctx.WorkspacePath, reviewFixStartedAt, reviewFixCompletedAt)
		}
		includesIntro := false
		if !introSent {
			priorFailureContext := ""
			if len(recoveryNotes) > 0 {
				priorFailureContext = "\n\nPRIOR PULSE FAILURE CONTEXT. Earlier work in this same conversation did not finish cleanly. Preserve partial/failed status honestly and do not claim skipped work succeeded:\n- " + strings.Join(recoveryNotes, "\n- ")
			}
			query = intro + priorFailureContext + "\n\n" + query
			includesIntro = true
		}
		reqMap["query"] = query
		if err := s.api.startSessionInternal(ctx, reqMap, sessionID, sctx.OwnerUserID, nil); err != nil {
			s.sessionLogf(sctx, sessionID, "[PULSE] step %q did not finish: %v", st.label, err)
			outcome := pulseLifecycleStepWaitFailed
			if errors.Is(err, errWorkshopSequenceInterrupted) || errors.Is(err, context.Canceled) {
				outcome = pulseLifecycleStepInterrupted
			} else if errors.Is(err, errWorkshopIdleWaitTimeout) {
				outcome = pulseLifecycleStepTimedOut
			}
			return pulseLifecycleStepRunResult{outcome: outcome, err: err}
		}
		if includesIntro {
			introSent = true
		}
		s.sessionLogf(sctx, sessionID, "[PULSE] step %q done for %s", st.label, sctx.Schedule.ID)
		return pulseLifecycleStepRunResult{outcome: pulseLifecycleStepCompleted}
	}
	handleStepFailure := func(st pulseLifecycleStep, result pulseLifecycleStepRunResult, needsFollowup bool) string {
		failureLabel := "failed"
		if result.outcome == pulseLifecycleStepTimedOut {
			failureLabel = fmt.Sprintf("made no observable progress for %s", st.idleMaxInactivity())
		}
		reason := fmt.Sprintf("Pulse step %s %s", st.label, failureLabel)
		if result.err != nil && result.outcome != pulseLifecycleStepTimedOut {
			reason += ": " + result.err.Error()
		}
		recoveryNotes = append(recoveryNotes, reason)
		if needsFollowup {
			s.sessionLogf(sctx, sessionID, "[PULSE] continuing in the same Pulse conversation after %s", reason)
		}
		return reason
	}
	abortIfInterrupted := func(st pulseLifecycleStep, result pulseLifecycleStepRunResult) bool {
		if result.outcome != pulseLifecycleStepInterrupted {
			return false
		}
		reason := fmt.Sprintf("Pulse stopped by user during %s", st.label)
		pulseResult = pulseLifecycleStopped
		s.sessionLogf(sctx, sessionID, "[PULSE] %s; no later Review, Fix, Finalize, publish, or notification turn will run", reason)
		return true
	}

	// A scheduled Pulse reviews evidence produced by this invocation. A run that
	// failed still counts; a preflight abort against a pre-existing, untouched
	// run folder does not. Manual PulseOnly is the intentional exception because
	// it explicitly reviews retained evidence without executing the workflow.
	reviewEvidenceAvailable := sctx.ProducedRunEvidence || sctx.PulseOnly
	if pulseMode == schedulePulseModeFull && !reviewEvidenceAvailable {
		s.sessionLogf(sctx, sessionID, "[PULSE] workflow did not start in this invocation; skipping Gate, reviewers, Fixer, dashboard and publish")
	}

	var steps []pulseLifecycleStep
	if pulseMode == schedulePulseModeBasic {
		steps = scheduledRunFinalizeStep(scheduleRunID, notificationInstructionsFromCapabilities(sctx.Capabilities))
		s.sessionLogf(sctx, sessionID, "[PULSE] basic post-run finalization selected for %s; Gate, drift review, reviewers, and Fixer are disabled by this schedule", sctx.Schedule.ID)
	} else if !reviewEvidenceAvailable {
		steps = pulseLifecycleNoRunSteps(pulseRunID, runFailureReason, notificationInstructionsFromCapabilities(sctx.Capabilities))
	} else {
		gateStep := pulseLifecycleGateStep(pulseRunID, runFolder, runStatus)
		if sctx.Schedule.PulseReviewOnly {
			folders, foldersErr := s.api.loadRunFoldersInternal(ctx, sctx.WorkspacePath)
			if foldersErr != nil {
				s.sessionLogf(sctx, sessionID, "[PULSE] periodic backlog listing failed, Gate will reason with no folder listing: %v", foldersErr)
				folders = nil
			}
			gateStep = pulseLifecycleBacklogGateStep(pulseRunID, pulseReviewBacklogSummary(folders))
		}
		gateCompleted := false
		for attempt := 1; attempt <= 2; attempt++ {
			result := runStep(gateStep)
			if abortIfInterrupted(gateStep, result) {
				return
			}
			if result.outcome == pulseLifecycleStepCompleted {
				if err := validatePulseGateCompletion(ctx, sctx.WorkspacePath, pulseRunID); err == nil {
					gateCompleted = true
					break
				} else {
					s.sessionLogf(sctx, sessionID, "[PULSE] Gate completion contract failed (attempt %d/2): %v", attempt, err)
					result = pulseLifecycleStepRunResult{outcome: pulseLifecycleStepWaitFailed, err: err}
				}
			} else if err := validatePulseGateCompletion(ctx, sctx.WorkspacePath, pulseRunID); err == nil {
				// The agent may time out after it has already committed the
				// complete durable worklist. Preserve the failure truth, but do
				// not discard its due-module routing.
				handleStepFailure(gateStep, result, true)
				gateCompleted = true
				s.sessionLogf(sctx, sessionID, "[PULSE] Gate stage failed after recording a complete durable worklist; continuing with selected modules")
				break
			}
			if attempt == 1 {
				handleStepFailure(gateStep, result, true)
				continue
			}
			handleStepFailure(gateStep, result, true)
		}
		if !gateCompleted {
			if err := validatePulseGateCompletion(ctx, sctx.WorkspacePath, pulseRunID); err == nil {
				gateCompleted = true
				s.sessionLogf(sctx, sessionID, "[PULSE] recovered complete durable Gate worklist after the stage failure")
			}
		}
		if gateCompleted {
			// plan_drift_review must run and fully complete BEFORE technical_review
			// is dispatched, or technical_review's backlog read can race a drift
			// finding that hasn't been written yet — see
			// pulseLifecyclePlanDriftReviewStep's own doc comment.
			if planDriftDue, err := pulseWorklistModulesDue(ctx, sctx.WorkspacePath, pulseRunID, pulseModulePlanDriftReview); err != nil {
				s.sessionLogf(sctx, sessionID, "[PULSE] could not inspect plan_drift_review due-module receipt after Gate; preserving the plan drift review turn: %v", err)
				steps = append(steps, pulseLifecyclePlanDriftReviewStep(pulseRunID))
			} else if planDriftDue {
				steps = append(steps, pulseLifecyclePlanDriftReviewStep(pulseRunID))
			}
			for _, module := range []string{pulseModuleTechnicalReview, pulseModuleArchitectureReview, pulseModuleStrategicReview} {
				if due, err := pulseWorklistModulesDue(ctx, sctx.WorkspacePath, pulseRunID, module); err != nil || due {
					steps = append(steps, pulseLifecycleModuleReviewStep(pulseRunID, module))
				}
			}
			steps = append(steps, pulseLifecycleFinalSteps(pulseRunID, notificationInstructionsFromCapabilities(sctx.Capabilities))...)
			if len(steps) > 0 && !isPulseLifecycleFinalStep(steps[0].label) {
				s.transitionScheduleRun(ctx, sctx, schedulerstate.Transition{
					RunID: scheduleRunID, To: schedulerstate.StatePulseModules, Reason: "Pulse Gate recorded due modules",
					SessionID: sessionID, SessionKind: "pulse", At: time.Now().UTC(),
				})
			}
			s.sessionLogf(sctx, sessionID, "[PULSE] selected %d post-gate steps for %s", len(steps), sctx.Schedule.ID)
		} else {
			steps = pulseLifecycleFinalSteps(pulseRunID, notificationInstructionsFromCapabilities(sctx.Capabilities))
		}
	}

	// Index-based (not range) so a late review-fix insertion below is picked
	// up by the loop that is already in progress — range would have captured
	// the pre-insertion length and never see it.
	for i := 0; i < len(steps); i++ {
		st := steps[i]
		reviewModule := pulsemodules.ForStepLabel(st.label)
		isImprovementReview := reviewModule != "" && reviewModule != pulseModulePlanDriftReview
		if st.label == "finalize" {
			st.query += s.prepareWorkflowDatabaseBackupSnapshot(ctx, sctx)
		}
		// No Pulse turn stamps a contract version, so none of them is granted
		// one. Revoking after each turn keeps that true even if a future step
		// starts minting.
		if isImprovementReview {
			if err := beginDuePulseReviewRecoveries(ctx, sctx.WorkspacePath, pulseRunID, reviewModule); err != nil {
				result := pulseLifecycleStepRunResult{outcome: pulseLifecycleStepWaitFailed, err: fmt.Errorf("create Pulse review recovery attempts: %w", err)}
				handleStepFailure(st, result, i < len(steps)-1)
				continue
			}
			if reviewFixStartedAt.IsZero() {
				reviewFixStartedAt = time.Now().UTC()
			}
		}
		result := runStep(st)
		contractupgrade.Revoke(sessionID)
		if abortIfInterrupted(st, result) {
			return
		}
		if result.outcome == pulseLifecycleStepCompleted && (isImprovementReview || st.label == "plan-drift-review") {
			var receiptErr error
			if st.label == "plan-drift-review" {
				receiptErr = validatePulseDueModuleResultsFor(ctx, sctx.WorkspacePath, pulseRunID, pulseModulePlanDriftReview)
			} else {
				receiptErr = validatePulseDueModuleResultsFor(ctx, sctx.WorkspacePath, pulseRunID, reviewModule)
			}
			if receiptErr != nil {
				s.sessionLogf(sctx, sessionID, "[PULSE] %s receipt incomplete; asking the same conversation to reconcile it: %v", st.label, receiptErr)
				continuation := pulseLifecycleReviewFixContinuationStep(pulseRunID, receiptErr)
				result = runStep(continuation)
				if abortIfInterrupted(continuation, result) {
					return
				}
				if result.outcome == pulseLifecycleStepCompleted {
					var retryErr error
					if st.label == "plan-drift-review" {
						retryErr = validatePulseDueModuleResultsFor(ctx, sctx.WorkspacePath, pulseRunID, pulseModulePlanDriftReview)
					} else {
						retryErr = validatePulseDueModuleResultsFor(ctx, sctx.WorkspacePath, pulseRunID, reviewModule)
					}
					if retryErr != nil {
						result = pulseLifecycleStepRunResult{outcome: pulseLifecycleStepWaitFailed, err: retryErr}
					}
				}
			}
			// Only technical_review promises to drain workflow-owned repairs.
			// A strategic-only review must not fail on unrelated repair debt.
			if reviewModule == pulseModuleTechnicalReview && result.outcome == pulseLifecycleStepCompleted {
				if err := validatePulseTechnicalRepairDrain(ctx, sctx.WorkspacePath, pulseRunID); err != nil {
					result = pulseLifecycleStepRunResult{outcome: pulseLifecycleStepWaitFailed, err: err}
				}
			}
		}
		if isImprovementReview {
			reviewFixCompletedAt = time.Now().UTC()
			// This is intentionally scheduler-owned. A child can write a useful
			// checkpoint and then time out or exit before its final tool call; an
			// instruction telling the parent agent to remember that fact is not a
			// durable recovery mechanism.
			if recovered, recoveryErr := recordIncompletePulseReviewRecoveries(ctx, sctx.WorkspacePath, pulseRunID, result, reviewModule); recoveryErr != nil {
				s.sessionLogf(sctx, sessionID, "[PULSE] could not persist incomplete review recovery: %v", recoveryErr)
				if result.outcome == pulseLifecycleStepCompleted {
					result = pulseLifecycleStepRunResult{outcome: pulseLifecycleStepWaitFailed, err: fmt.Errorf("persist incomplete Pulse review recovery: %w", recoveryErr)}
				}
			} else if len(recovered) > 0 {
				s.sessionLogf(sctx, sessionID, "[PULSE] persisted recovery for incomplete review module(s): %s", strings.Join(recovered, ", "))
			}
		}
		if result.outcome != pulseLifecycleStepCompleted {
			handleStepFailure(st, result, i < len(steps)-1)
		}
	}
	if len(recoveryNotes) > 0 {
		pulseResult = pulseLifecyclePartial
		s.sessionLogf(sctx, sessionID, "[PULSE] pulse finalized partially for %s after %d failed/timed-out step(s)", sctx.Schedule.ID, len(recoveryNotes))
	} else {
		pulseResult = pulseLifecycleCompleted
		s.sessionLogf(sctx, sessionID, "[PULSE] pulse completed for %s", sctx.Schedule.ID)
	}
	// Pulse owns its own notification from the durable SQLite state. The popup is
	// the only Pulse presentation; there is no parallel HTML journal to update.
	return pulseResult
}

// prepareWorkflowDatabaseBackupSnapshot makes the protected live SQLite DB
// backupable before the agent begins the ordinary Git/object-store backup.
// This is deliberately deterministic: an agent never needs shell access to
// db.sqlite, its WAL, or its SHM file, and a snapshot failure cannot suppress
// the later publish or notification operations.
func (s *SchedulerService) prepareWorkflowDatabaseBackupSnapshot(ctx context.Context, sctx *ScheduleContext) string {
	if sctx == nil {
		return ""
	}
	manifest, exists, err := ReadWorkflowManifest(ctx, sctx.WorkspacePath)
	if err != nil {
		s.logf(sctx, "[BACKUP] could not inspect workflow backup config before finalization: %v", err)
		return fmt.Sprintf("\n\nMANAGED DATABASE SNAPSHOT. The backend could not inspect workflow backup config: %v. Keep backup status truthful, but continue independently through Publish and Notify.", err)
	}
	if !exists || !workflowBackupRequiresDatabaseSnapshot(manifest.Backup, sctx.TriggerSource) {
		return ""
	}

	client := workspace.NewClient(getWorkspaceAPIURL())
	dbPath := filepath.ToSlash(filepath.Join(sctx.WorkspacePath, "db", "db.sqlite"))
	result, err := client.CreateWorkflowDatabaseBackupSnapshot(ctx, workspace.CreateWorkflowDatabaseBackupSnapshotParams{DBPath: dbPath})
	if err != nil {
		s.logf(sctx, "[BACKUP] managed workflow database snapshot failed: %v", err)
		return fmt.Sprintf("\n\nMANAGED DATABASE SNAPSHOT. The backend attempted the required WAL-aware snapshot before this turn, but it failed: %v. Mark the database portion of Backup partial/failed as appropriate; do not try to read or stage live db/db.sqlite. Publish and Notify are independent and must still run.", err)
	}
	s.logf(sctx, "[BACKUP] managed workflow database snapshot ready at %s", result.SnapshotPath)
	return fmt.Sprintf("\n\nMANAGED DATABASE SNAPSHOT. The backend already created and integrity-checked the current SQLite backup image at %q (sha256 %s, %d bytes). Stage that snapshot and its checksum at %q for every destination covering db-sqlite; never read or stage live db/db.sqlite. Publish and Notify are independent of the Backup result.", result.SnapshotPath, result.SHA256, result.SizeBytes, result.ChecksumPath)
}

func workflowBackupRequiresDatabaseSnapshot(config *WorkflowBackupConfig, triggerSource string) bool {
	if config == nil || !config.Enabled {
		return false
	}
	manual := strings.EqualFold(strings.TrimSpace(triggerSource), "manual")
	if manual && !config.Triggers.AfterManualRun {
		return false
	}
	if !manual && !config.Triggers.AfterScheduledRun {
		return false
	}
	for _, destination := range config.Destinations {
		for _, covered := range destination.Covers {
			if strings.EqualFold(strings.TrimSpace(covered), "db-sqlite") {
				return true
			}
		}
	}
	return false
}

func pulseReviewFixCostContext(ledger *costledger.Ledger, workspacePath string, startedAt, completedAt time.Time) string {
	if startedAt.IsZero() && completedAt.IsZero() {
		return "\n\nREVIEWER/FIXER COST. Review+Fix was not run in this pass, so its cost is $0.00. Include that compact fact in Operations; do not substitute Gate, Finalize, workflow, builder, or prior-pass cost."
	}
	const unavailable = "\n\nREVIEWER/FIXER COST. Cost evidence is unavailable for this pass. Say that plainly in Operations; do not estimate or reuse a prior run's amount."
	if ledger == nil || startedAt.IsZero() || completedAt.IsZero() || !completedAt.After(startedAt) {
		return unavailable
	}
	summary, err := ledger.SummarizeWorkflowScopeWindow(workspacePath, "pulse", startedAt, completedAt)
	if err != nil {
		return unavailable
	}
	total := summary.Total
	if total.AccountingEventCount == 0 {
		return "\n\nREVIEWER/FIXER COST. Review+Fix ran, but no LLM cost events were recorded in its measured window. Report the measurement gap in Operations; do not present it as $0.00 or substitute another cost bucket."
	}
	costLabel := "accounted cost"
	if total.SubscriptionShadowUSD > 0 && total.ProviderActualCostUSD == 0 && total.TokenEstimateCostUSD == 0 {
		costLabel = "estimated token-equivalent cost (subscription-backed coding CLI)"
	}
	return fmt.Sprintf(
		"\n\nREVIEWER/FIXER COST (backend-measured after Review+Fix completed at %s). Include this exact, compact line in the notification's Operations section: Reviewers + Fixer %s: $%.2f across %d LLM call(s). This covers the parent Review+Fix turn, its background reviewer/fixer agents, and any receipt continuation inside that stage. It excludes Gate, Finalize, workflow execution, builder activity, and prior Pulse passes.",
		completedAt.UTC().Format(time.RFC3339), costLabel, total.TotalCostUSD, total.CallCount,
	)
}

type pulseLifecycleStep struct{ label, query string }

type pulseLifecycleStepOutcome string

const (
	pulseLifecycleStepCompleted   pulseLifecycleStepOutcome = "completed"
	pulseLifecycleStepWaitFailed  pulseLifecycleStepOutcome = "wait_failed"
	pulseLifecycleStepTimedOut    pulseLifecycleStepOutcome = "timed_out"
	pulseLifecycleStepInterrupted pulseLifecycleStepOutcome = "interrupted"
)

type pulseLifecycleStepRunResult struct {
	outcome pulseLifecycleStepOutcome
	err     error
}

func pulseLifecycleIntro(contextSummary, workspacePath, pulseRunID, runStatus, runFolder string) string {
	return fmt.Sprintf("PULSE RUN CONTEXT. %s. workspace_path=%q, pulse_run_id=%q, evidence_status=%q, run_folder=%q. This is one continuing Pulse conversation. The scheduler sends Gate, Review+Fix, and Finalize turns in order. Own the reasoning and any useful specialist delegation inside the current turn, use durable workflow state for human answers, keep user-facing output concise, persist the required receipt, then stop so the next turn can continue.",
		contextSummary, workspacePath, pulseRunID, runStatus, runFolder)
}

// isPulseLifecycleFinalStep marks the finalizer, which must still run when
// earlier maintenance failed so notification/final command state stay truthful.
func isPulseLifecycleFinalStep(label string) bool {
	return label == "finalize"
}

func (st pulseLifecycleStep) idleMaxInactivity() time.Duration {
	return schedulerWorkshopMaxInactivity
}

func workflowHasPendingPlanChangelogArtifactReview(ctx context.Context, workspacePath string) (bool, error) {
	workspacePath = strings.Trim(strings.TrimSpace(workspacePath), "/")
	if workspacePath == "" {
		return false, nil
	}

	folder := workspacePath + "/planning/changelog"
	listing, exists, err := listWorkspaceFolder(ctx, folder, 1)
	if err != nil {
		return true, err
	}
	if !exists {
		return false, nil
	}

	var filePaths []string
	collectWorkspaceFilePaths(listing, &filePaths)
	for _, filePath := range filePaths {
		base := filepath.Base(filePath)
		if !strings.HasPrefix(base, "changelog-") || !strings.HasSuffix(strings.ToLower(base), ".json") {
			continue
		}

		content, exists, err := readFileFromWorkspace(ctx, filePath)
		if err != nil {
			return true, err
		}
		if !exists || strings.TrimSpace(content) == "" {
			continue
		}

		var changelog planChangelogFile
		if err := json.Unmarshal([]byte(content), &changelog); err != nil {
			// A malformed changelog still needs human/agent attention; keep the
			// Pulse Artifact Review turn rather than treating it as clean.
			return true, nil
		}
		for _, entry := range changelog.Entries {
			if entry.ArtifactReview == nil || !entry.ArtifactReview.Done {
				return true, nil
			}
		}
	}

	return false, nil
}

func pulseLifecycleSteps() []pulseLifecycleStep {
	steps := []pulseLifecycleStep{
		pulseLifecycleGateStep("<pulse_run_id>", "<run_folder>", "<run_status>"),
		pulseLifecyclePlanDriftReviewStep("<pulse_run_id>"),
		pulseLifecycleModuleReviewStep("<pulse_run_id>", pulseModuleTechnicalReview),
		pulseLifecycleModuleReviewStep("<pulse_run_id>", pulseModuleArchitectureReview),
		pulseLifecycleModuleReviewStep("<pulse_run_id>", pulseModuleStrategicReview),
	}
	steps = append(steps, pulseLifecycleFinalSteps("<pulse_run_id>")...)
	return steps
}

func pulseLifecycleGateStep(pulseRunID, runFolder, runStatus string) pulseLifecycleStep {
	return pulseLifecycleStep{
		label: "gate",
		query: fmt.Sprintf("PULSE GATE / WORKLIST. pulse_run_id=%q, run_folder=%q, run_status=%q. Load read_skill(skills=[{\"name\":\"builder-reference\",\"path\":\"references/pulse-gate.md\"}]) and follow it exactly. Perform only the progressive Gate scan. Choose the pass mode yourself from backlog and new-run evidence, then call record_pulse_worklist exactly once with mode, mode_reason, and all %d module decisions. Then record trustworthy current-run success-criterion observations with record_pulse_impact when available, and stop. Do not fabricate measurements, create interventions/assessments, write workflow artifacts, launch reviewers, fix, back up, publish, or notify.",
			pulseRunID, runFolder, runStatus, len(pulseModuleOrder)),
	}
}

// pulseLifecycleBacklogGateStep is Gate's prompt for a PulseReviewOnly
// periodic pass (PLAT-115): it hands Gate a listing of currently-existing run
// folders instead of pinning it to one run_folder=%q. Deliberately does not
// pre-filter "what's new" in Go — that reasoning belongs to Gate, comparing
// this listing against get_pulse_state's own last_checked_at per module, the
// same kind of judgment call Gate already makes for mode selection.
func pulseLifecycleBacklogGateStep(pulseRunID, backlogSummary string) pulseLifecycleStep {
	return pulseLifecycleStep{
		label: "gate",
		query: fmt.Sprintf("PULSE GATE / WORKLIST — PERIODIC BACKLOG REVIEW. pulse_run_id=%q. This is your workflow's own periodic Pulse review pass: it does not follow one specific run, it reviews whatever has accumulated since your last check. Currently existing run folders (name, status, started_at, completed_at), newest first:\n%s\n\nCompare these against get_pulse_state's last_checked_at per module to reason about what is genuinely new since you last looked — do not assume every listed folder is new, and do not skip the whole backlog because only some of it is. If the number of runs since your last check plausibly exceeds what run_retention_count preserved, say so explicitly in your worklist evidence rather than reviewing a partial sample as if it were complete, and consider raising it as a technical_review finding. Load read_skill(skills=[{\"name\":\"builder-reference\",\"path\":\"references/pulse-gate.md\"}]) and follow it exactly. Perform only the progressive Gate scan. Choose the pass mode yourself from backlog and new-run evidence, then call record_pulse_worklist exactly once with mode, mode_reason, and all %d module decisions. Then record trustworthy current-run success-criterion observations with record_pulse_impact when available, and stop. Do not fabricate measurements, create interventions/assessments, write workflow artifacts, launch reviewers, fix, back up, publish, or notify.",
			pulseRunID, backlogSummary, len(pulseModuleOrder)),
	}
}

// pulseReviewBacklogSummary renders currently-existing run folders as a
// compact, newest-first text listing for pulseLifecycleBacklogGateStep.
// iteration-0 (the live/reused slot, never a stable identity across time —
// see PLAT-115) is included only when its own metadata reports a terminal
// status, so Gate is never handed a run that may still be in flight.
func pulseReviewBacklogSummary(folders []RunFolderInfo) string {
	if len(folders) == 0 {
		return "(no run folders exist yet)"
	}
	var lines []string
	for _, folder := range folders {
		name := strings.TrimSpace(folder.Name)
		if name == "" {
			continue
		}
		if strings.HasPrefix(name, "iteration-0") && !runFolderMetadataIsTerminal(folder.Metadata) {
			continue
		}
		lines = append(lines, "- "+pulseReviewBacklogFolderLine(folder))
	}
	if len(lines) == 0 {
		return "(no reviewable run folders — the only existing folder is still in progress)"
	}
	return strings.Join(lines, "\n")
}

func runFolderMetadataIsTerminal(metadata *RunMetadata) bool {
	if metadata == nil {
		return false
	}
	switch strings.TrimSpace(strings.ToLower(metadata.Status)) {
	case "completed", "failed", "canceled":
		return true
	default:
		return false
	}
}

func pulseReviewBacklogFolderLine(folder RunFolderInfo) string {
	if folder.Metadata == nil {
		return fmt.Sprintf("%s (status=unknown, no run_metadata.json)", folder.Name)
	}
	completedAt := "not completed"
	if folder.Metadata.CompletedAt != nil {
		completedAt = folder.Metadata.CompletedAt.Format(time.RFC3339)
	}
	return fmt.Sprintf("%s (status=%s, started_at=%s, completed_at=%s)",
		folder.Name, folder.Metadata.Status, folder.Metadata.StartedAt.Format(time.RFC3339), completedAt)
}

// pulseLifecyclePlanDriftReviewStep runs plan_drift_review as its own
// lifecycle step, sequenced strictly BEFORE pulseLifecycleAgenticReviewStep's
// technical_review dispatch. Both used to be launched as sibling
// run_in_background children of one combined step, which let technical_review
// read the Pulse finding backlog for repair candidates before
// plan_drift_review's own async handoff finding had necessarily been written
// — a real drift finding could then sit unrepaired for a full extra Pulse
// cycle even though plan_drift_review had already run. runStep blocks until
// a step's turn (and everything it dispatches in the background) fully
// completes, so making this its own preceding step is what actually
// guarantees the ordering; reordering text within one shared prompt would
// not, since same-step dispatches still run concurrently with each other.
func pulseLifecyclePlanDriftReviewStep(pulseRunID string) pulseLifecycleStep {
	return pulseLifecycleStep{
		label: "plan-drift-review",
		query: fmt.Sprintf(`PULSE PLAN DRIFT REVIEW DISPATCH. pulse_run_id=%q. Read the durable Gate worklist via get_pulse_state(view="module", pulse_run_id=<this id>). If plan_drift_review is not due, do nothing and end this turn immediately.

		When plan_drift_review is due, launch exactly one executor with run_in_background. Its instruction must name exact pulse_run_id=%q, and tell it to load read_skill(skills=[{"name":"builder-reference","path":"references/plan-drift-review.md"}]) and follow it exactly. In that one retained turn it establishes ground truth per due step, applies and verifies safe workflow-owned fixes directly, and only routes what it cannot safely fix itself — a genuine human decision, a platform-owned boundary, or (rarely, as a last resort) a fixer_handoff for technical_review to pick up.

		After dispatch, end this parent turn immediately; the runtime waits for the registered child, and this step does not return until it completes — that is the point: technical_review's own dispatch in the next lifecycle step must only ever see a Pulse backlog that already reflects this run's plan_drift_review findings, never a race with them. Do not do review or repair in this parent, render a dashboard, back up, publish, or notify.`, pulseRunID, pulseRunID),
	}
}

// One blocking lifecycle step per module prevents technical mutation from racing
// architecture/strategy evidence reads, while each result/recovery stays independent.
func pulseLifecycleModuleReviewStep(pulseRunID, module string) pulseLifecycleStep {
	label, reference, contract := "technical-review", "technical-review", "Investigate material correctness failures and apply safe workflow-owned repairs. Do not perform a general optimization audit."
	switch module {
	case pulseModuleArchitectureReview:
		label, reference, contract = "architecture-review", "architecture-review", "Improve the construction of a working workflow, including evidence-based execution tier/model choices. Use actual quality, retries, cost and latency evidence; preserve explicit user pins and propose measured trials with a checkpoint and rollback through architecture decisions. Runtime does not change tiers from run counts. Research and propose bounded improvements; do not mutate implementation in the review."
	case pulseModuleStrategicReview:
		label, reference, contract = "strategic-review", "strategy-auditor", "Investigate the goal and strategy using workflow evidence, authorized MCP data, browser and external research. Read get_goal_metrics early, connect proposals to outcome metrics and verification checkpoints, and propose bounded measurement additions or repairs when metrics are missing or inadequate. Do not mutate implementation or perform business actions."
	}
	return pulseLifecycleStep{label: label, query: fmt.Sprintf(`PULSE MODULE REVIEW DISPATCH. pulse_run_id=%q. This step owns ONLY module=%q. Earlier lifecycle steps have finished; later modules must not be dispatched here.
Read the durable Gate worklist. If this module is not due or already has a terminal result, stop. Otherwise launch exactly one run_in_background executor with review_module=%q, pulse_run_id=%q, and an instruction to read get_pulse_state(view="review_notes", module=%q) once for relevant prior reasoning. Load read_skill(skills=[{"name":"builder-reference","path":"references/%s.md"}]). %s
Use saved notes and typed records for interrupted work; read an old Markdown file only if a specific historical record points to it. Do not create or maintain mandatory Markdown checkpoints. Persist selected focus coverage, typed findings/decisions, improvement lifecycle and one terminal result for this module. An improvement proposal must identify expected benefit, baseline, guardrails and next outcome checkpoint. Finish with one record_pulse_result using reason and optional review_note for new reasoning, limitations and next steps. No separate reporting turn or repeated history scan. Reuse existing records. Applied is not evidence of improved outcomes. The shared pass mode must not suppress another module's research.
After dispatch end this parent turn. The runtime waits for the child before proceeding to the next module. Do not render a dashboard, back up, publish or notify here.`, pulseRunID, module, module, pulseRunID, module, reference, contract)}
}

func pulseLifecycleReviewFixContinuationStep(pulseRunID string, receiptErr error) pulseLifecycleStep {
	return pulseLifecycleStep{
		label: "review-fix-continuation",
		query: fmt.Sprintf(`PULSE REVIEW + FIX RECEIPT CHECK. pulse_run_id=%q. Continue after all registered background sequences completed. The prior stage is missing receipts: %s. Load the Gate worklist, typed Pulse state, child status, and saved SQLite review notes. Do not reconstruct findings or fixes from truncated automatic-notification prose, and do not add a consolidation pass. Validate the receipts already persisted by each sequence. Resolve only a genuine cross-module ownership conflict using saved notes and typed rows. If a child ended before its final persistence turn, do not infer or invent its findings, restart it automatically, or mutate workflow artifacts. The scheduler records durable recovery for any module still missing its terminal result after this step. Keep the response compact, then stop.`, pulseRunID, receiptErr),
	}
}

// recordIncompletePulseReviewRecoveries turns a missing current-run receipt
// into a durable next-Gate obligation. It is safe to call after every
// review-fix outcome: modules with a terminal receipt are ignored.
func recordIncompletePulseReviewRecoveries(ctx context.Context, workspacePath, pulseRunID string, stepResult pulseLifecycleStepRunResult, modules ...string) ([]string, error) {
	worklist, ok, err := getPulseWorklistForRun(ctx, workspacePath, pulseRunID)
	if err != nil || !ok {
		if err != nil {
			return nil, fmt.Errorf("read Pulse worklist: %w", err)
		}
		return nil, fmt.Errorf("Pulse worklist %q is missing", pulseRunID)
	}
	reason := "review-fix ended before the module persisted a terminal result"
	if stepResult.err != nil {
		reason += ": " + stepResult.err.Error()
	} else if stepResult.outcome != pulseLifecycleStepCompleted {
		reason += fmt.Sprintf(" (outcome=%s)", stepResult.outcome)
	}
	reviewModules := []string{pulseModuleTechnicalReview, pulseModuleArchitectureReview, pulseModuleStrategicReview}
	var recovered []string
	for _, module := range reviewModules {
		if len(modules) > 0 && !slices.Contains(modules, module) {
			continue
		}
		state, exists := worklist[module]
		if !exists || !strings.EqualFold(strings.TrimSpace(state.LastDecision), "due") || strings.TrimSpace(state.LastResult) != "" {
			continue
		}
		if err := markPulseReviewRecovery(ctx, workspacePath, module, pulseRunID, "", reason); err != nil {
			return recovered, err
		}
		recovered = append(recovered, module)
	}
	return recovered, nil
}

func beginDuePulseReviewRecoveries(ctx context.Context, workspacePath, pulseRunID string, modules ...string) error {
	worklist, ok, err := getPulseWorklistForRun(ctx, workspacePath, pulseRunID)
	if err != nil || !ok {
		if err != nil {
			return err
		}
		return fmt.Errorf("Pulse worklist %q is missing", pulseRunID)
	}
	reviewModules := []string{pulseModuleTechnicalReview, pulseModuleArchitectureReview, pulseModuleStrategicReview}
	for _, module := range reviewModules {
		if len(modules) > 0 && !slices.Contains(modules, module) {
			continue
		}
		state, exists := worklist[module]
		if !exists || !strings.EqualFold(strings.TrimSpace(state.LastDecision), "due") || strings.TrimSpace(state.LastResult) != "" {
			continue
		}
		if err := beginPulseReviewRecovery(ctx, workspacePath, module, pulseRunID, ""); err != nil {
			return err
		}
	}
	return nil
}

type workflowNotificationContentInstructions struct {
	runSummary             string
	pulseSummary           string
	runSummaryChannels     []string
	pulseSummaryChannels   []string
	runSummaryRecipients   []string
	pulseSummaryRecipients []string
}

func notificationInstructionsFromCapabilities(capabilities WorkflowCapabilities) workflowNotificationContentInstructions {
	if capabilities.Notifications == nil {
		return workflowNotificationContentInstructions{}
	}
	notifications := capabilities.Notifications
	return workflowNotificationContentInstructions{
		runSummary:             notifications.EffectiveRunSummaryInstructions(),
		pulseSummary:           notifications.EffectivePulseSummaryInstructions(),
		runSummaryChannels:     append([]string(nil), notifications.RunSummaryChannels...),
		pulseSummaryChannels:   append([]string(nil), notifications.PulseSummaryChannels...),
		runSummaryRecipients:   append([]string(nil), notifications.RunSummaryRecipients...),
		pulseSummaryRecipients: append([]string(nil), notifications.PulseSummaryRecipients...),
	}
}

// pulseSafeRunFailureReason tells the finalizer why the workflow did not run
// without handing it the upgrade instruction a second time.
//
// The finalizer shares the scheduler's session. On confida-login 2026-08-12 it
// was told `did not stamp required version "1.0.21"` three seconds after the
// turn that owed that stamp had been adjudicated and closed — and ten minutes
// later something in that session stamped 1.0.21, which the next preflight
// trusted and skipped the migration for. The grant in pkg/contractupgrade is
// what makes the write impossible; keeping the instruction out of unrelated
// turns is what stops the attempt from being made and reported as a confusing
// refusal in the operator's run-summary email.
//
// The preflight label survives, because naming which migration stalled is real
// diagnostic value. The target version and the stamp verb do not.
//
// Since a declined stamp now skips Pulse outright
// (errWorkflowUpgradePreflightBlocked), this is belt-and-braces rather than the
// primary defense: it still covers any future path that routes an upgrade
// reason into a live session, which is the shape that caused the damage.
func pulseSafeRunFailureReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if !strings.HasPrefix(reason, "workflow upgrade preflight") {
		return reason
	}
	idx := strings.Index(reason, " did not stamp")
	if idx <= 0 {
		return reason
	}
	return reason[:idx] + " did not complete, so the scheduled workflow was not started." +
		" That migration belongs to its own upgrade turn: do not attempt it here and do not stamp a contract version from this turn."
}

// Keep Gmail rendering explicit in every finalizer prompt. Full Pulse also
// loads pulse-finalizer.md and notify_user advertises the same contract, but
// basic/no-run finalizers do not load that skill and an agent can otherwise
// satisfy the neutral summary fields while accidentally sending plain email.
const finalizerRichEmailInstruction = "\n\nRICH GMAIL OUTPUT. When Gmail is configured for this notification, the same notify_user call must include a non-empty plain-text email_subject and one compact, inline-styled email_html body. Keep message_for_user as the channel-neutral plain fallback; do not paste a browser report or a full HTML document into the email."

// pulseLifecycleNoRunSteps is the truthful finalizer for an invocation that
// never produced new run evidence — the workshop session ran but the workflow
// itself never started or restarted a run folder (e.g. a preflight abort
// against a pre-existing, untouched iteration-0). There is no run to review or
// render, so it skips Gate, Review+Fix, and dashboard, preserves any preflight
// edits through the normal backup contract, and tells the user why no result
// exists.
func pulseLifecycleNoRunSteps(pulseRunID, reason string, instructions ...workflowNotificationContentInstructions) []pulseLifecycleStep {
	ownerInstructions := workflowNotificationContentInstructions{}
	if len(instructions) > 0 {
		ownerInstructions = instructions[0]
	}
	reason = pulseSafeRunFailureReason(reason)
	if reason == "" {
		reason = "the scheduler recorded no error, but no workflow run was started or resumed during this invocation"
	}
	routing := ""
	if len(ownerInstructions.runSummaryChannels) > 0 {
		routing += fmt.Sprintf(" Configured run-summary channels: %s; the backend enforces them from notification_kind.", notificationChannelSummary(ownerInstructions.runSummaryChannels))
	}
	if len(ownerInstructions.runSummaryRecipients) > 0 {
		routing += fmt.Sprintf(" The backend addresses email from the workflow's saved run-summary recipients (%s); do not pass email_to.", notificationRecipientSummary(ownerInstructions.runSummaryRecipients))
	}
	content := ""
	if runInstructions := strings.TrimSpace(ownerInstructions.runSummary); runInstructions != "" {
		content = "\n\nApply these saved run-summary content instructions without changing the facts:\n" + runInstructions
	}
	return []pulseLifecycleStep{{"finalize", fmt.Sprintf(
		"PULSE FINALIZER — WORKFLOW DID NOT RUN. pulse_run_id=%q. The scheduled workflow never started in this invocation, so there is no new run evidence. Gate, reviewers, Fixer, dashboard, and publish were intentionally skipped. Do not run them, do not read old evidence as this run, do not write builder/improve.html, and do not invent an outcome.\n\n"+
			"Do these actions in order and record every command with record_pulse_result(command=..., result=..., reason=...): dashboard has no record_pulse_result command and needs no receipt — it is already intentionally skipped by not being rendered. (1) run the configured source-hash-gated backup and record its truthful terminal result; (2) mark publish skipped because nothing was produced; (3) call notify_user exactly once with notification_kind=\"run_summary\" and plainly say the workflow did not start, no results were produced, and the next schedule will retry unless the cause is fixed. Set summary_status=\"no_run\"; include title, compact facts, and sections. Then record notify truthfully.%s%s\n\nThe scheduler's reason is:\n%s%s",
		pulseRunID, routing, finalizerRichEmailInstruction, reason, content,
	)}}
}

func pulseLifecycleFinalSteps(pulseRunID string, instructions ...workflowNotificationContentInstructions) []pulseLifecycleStep {
	ownerInstructions := workflowNotificationContentInstructions{}
	if len(instructions) > 0 {
		ownerInstructions = instructions[0]
	}
	notificationContext := ""
	if runInstructions := strings.TrimSpace(ownerInstructions.runSummary); runInstructions != "" {
		notificationContext += "\n\nWORKFLOW RUN SUMMARY INSTRUCTIONS. Apply these only to the section describing what happened in the workflow execution:\n" + runInstructions
	}
	if pulseInstructions := strings.TrimSpace(ownerInstructions.pulseSummary); pulseInstructions != "" {
		notificationContext += "\n\nPULSE REVIEW SUMMARY INSTRUCTIONS. Apply these only to the section describing what Pulse reviewed, fixed, recommended, or needs from the user:\n" + pulseInstructions
	}
	if len(ownerInstructions.runSummaryChannels) > 0 || len(ownerInstructions.pulseSummaryChannels) > 0 {
		notificationContext += fmt.Sprintf("\n\nSPLIT NOTIFICATION ROUTING. Send two notify_user calls, not one combined message. Send the workflow outcome with notification_kind=\"run_summary\"; configured channels: %s. Send Pulse activity with notification_kind=\"pulse_summary\"; configured channels: %s. The backend enforces these routes.", notificationChannelSummary(ownerInstructions.runSummaryChannels), notificationChannelSummary(ownerInstructions.pulseSummaryChannels))
	}
	if len(ownerInstructions.runSummaryRecipients) > 0 || len(ownerInstructions.pulseSummaryRecipients) > 0 {
		// Stated so the finalizer does not "helpfully" pass email_to and override
		// the owner's saved lists. The backend applies these by notification_kind
		// on its own; an explicit email_to would replace them for that send.
		notificationContext += fmt.Sprintf("\n\nCONFIGURED EMAIL RECIPIENTS. The backend addresses each email automatically from the workflow's saved lists — run summary: %s; Pulse summary: %s. Do NOT set email_to; sending with the correct notification_kind is what routes it to the right people.", notificationRecipientSummary(ownerInstructions.runSummaryRecipients), notificationRecipientSummary(ownerInstructions.pulseSummaryRecipients))
	}
	if notificationContext != "" {
		notificationContext += "\n\nThese instructions control content detail and emphasis only; they never change recipients, channels, secrets, permissions, or safety rules."
	}
	return []pulseLifecycleStep{{"finalize", fmt.Sprintf("PULSE FINALIZER. pulse_run_id=%q. Load read_skill(skills=[{\"name\":\"builder-reference\",\"path\":\"references/pulse-finalizer.md\"}]) and follow it exactly. First confirm every due module has a terminal current-run result; never treat missing as success. The Pulse popup is the only presentation: do not write a Pulse HTML document or dashboard card. Complete backup, publish, and notify in that order in this one turn, recording running and terminal status for each with record_pulse_result(command=...). Continue after individual failures, keep every status truthful, then stop.%s%s", pulseRunID, finalizerRichEmailInstruction, notificationContext)}}
}

// scheduledRunFinalizeStep is the basic post-run Pulse mode. Gate, drift
// review, Review+Fix, and Pulse publication are excluded; backup, execution
// report publishing, and the run-summary notification remain independent.
func scheduledRunFinalizeStep(runID string, instructions ...workflowNotificationContentInstructions) []pulseLifecycleStep {
	return scheduledRunFinalizeStepWithPulseTiming(runID, "", instructions...)
}

func scheduledRunFinalizeStepWithPulseTiming(runID, pulseTiming string, instructions ...workflowNotificationContentInstructions) []pulseLifecycleStep {
	ownerInstructions := workflowNotificationContentInstructions{}
	if len(instructions) > 0 {
		ownerInstructions = instructions[0]
	}
	routing := ""
	if len(ownerInstructions.runSummaryChannels) > 0 {
		routing += fmt.Sprintf(" Configured run-summary channels: %s; the backend enforces them from notification_kind.", notificationChannelSummary(ownerInstructions.runSummaryChannels))
	}
	if len(ownerInstructions.runSummaryRecipients) > 0 {
		routing += fmt.Sprintf(" The backend addresses email from the workflow's saved run-summary recipients (%s); do not pass email_to.", notificationRecipientSummary(ownerInstructions.runSummaryRecipients))
	}
	content := ""
	if runInstructions := strings.TrimSpace(ownerInstructions.runSummary); runInstructions != "" {
		content = "\n\nApply these saved run-summary content instructions without changing the facts:\n" + runInstructions
	}
	fastPulseDecision := ""
	if strings.TrimSpace(pulseTiming) != "" {
		fastPulseDecision = "\n\nPulse timing context: " + pulseTiming + " After completing the ordinary run finalization, decide whether this run created material new evidence that needs an earlier separate Pulse review. For routine/no-change work, or when waiting for the upcoming scheduled review is sufficient, do nothing. For a meaningful workflow/plan/schema/evaluation change, serious regression, or abnormal cost/runtime evidence where waiting is worse, call record_pulse_fast_request exactly once with this run_id, a concrete reason, and bounded artifact references. That only queues/coalesces the existing dedicated Pulse schedule; it never runs review inline or changes cron."
	}
	return []pulseLifecycleStep{{"finalize", fmt.Sprintf(
		"WORKFLOW RUN FINALIZER — BACKUP, REPORT PUBLISH, AND NOTIFY ONLY. run_id=%q. "+
			"This schedule selected basic Pulse, so Gate, drift review, reviewers, and Fixer are intentionally excluded. This is normal, not a missing Pulse pass. Do not run Gate, reviewers, "+
			"or Fixer, do not read old Pulse findings and present them as new, and do not write builder/improve.html.\n\n"+
			"Do these in order and record each with record_pulse_result(command=..., result=..., reason=...): "+
			"(1) run the configured source-hash-gated backup and record its truthful terminal result; "+
			"(2) independently of whether backup succeeded, read workflow.json's publish.targets. A \"report\" (or any non-\"pulse\") target is this run's own execution "+
			"output — publish it normally, following publish-strategy.md, exactly as an ordinary run would; it is fresh this "+
			"run regardless of whether Pulse reviewed anything. The \"pulse\" target specifically has nothing new this pass — "+
			"no Gate/Review+Fix ran — and must be skipped for that reason alone. If \"pulse\" is the only configured target, "+
			"mark the whole publish command skipped with that reason. Never suppress a valid report publish merely because backup was partial or failed. Record one truthful terminal result for publish either way; "+
			"(3) call notify_user exactly once with notification_kind=\"run_summary\" describing plainly and factually what this run "+
			"itself did (actions taken, errors, outcome) — do not include a Pulse findings/fixes section, since none ran this pass — "+
			"and include summary_title, summary_status, summary_fields, summary_sections, and summary_route when route-scoped. summary_status must directly say what the workflow is doing now: completed, failed, blocked, waiting_for_user, waiting_for_platform, monitoring, informational, or no_run. Explain any blocker in the title, message, facts, or sections. Then record notify truthfully.%s%s%s%s",
		runID, routing, finalizerRichEmailInstruction, content, fastPulseDecision,
	)}}
}

func notificationChannelSummary(channels []string) string {
	if len(channels) == 0 {
		return "all enabled channels (legacy default)"
	}
	return strings.Join(channels, ", ")
}

// notificationRecipientSummary describes a saved recipient list for the Pulse
// finalizer prompt. An unset list is spelled out as the account default rather
// than left blank, so the finalizer does not read it as "nobody".
func notificationRecipientSummary(recipients []string) string {
	if len(recipients) == 0 {
		return "the account default recipient"
	}
	return strings.Join(recipients, ", ")
}

func validatePulseDueModuleResults(ctx context.Context, workspacePath, pulseRunID string) error {
	worklist, ok, err := getPulseWorklistForRun(ctx, workspacePath, pulseRunID)
	if err != nil {
		return fmt.Errorf("read Pulse worklist: %w", err)
	}
	if !ok {
		return fmt.Errorf("Pulse worklist %q is missing", pulseRunID)
	}
	var unresolved []string
	for _, module := range pulseModuleOrder {
		state, exists := worklist[module]
		if !exists || strings.TrimSpace(strings.ToLower(state.LastDecision)) != "due" {
			continue
		}
		if strings.TrimSpace(state.LastResult) == "" {
			unresolved = append(unresolved, module)
		}
	}
	if len(unresolved) > 0 {
		return fmt.Errorf("due Pulse modules lack terminal current-run results: %s", strings.Join(unresolved, ", "))
	}
	return nil
}

// validatePulseDueModuleResultsFor is validatePulseDueModuleResults scoped to
// specific modules, needed once plan_drift_review became its own preceding
// lifecycle step: checking the full pulseModuleOrder right after that step
// would wrongly flag technical_review/strategic_review as missing a receipt
// when they simply have not had their turn yet (it runs next, not before).
func validatePulseDueModuleResultsFor(ctx context.Context, workspacePath, pulseRunID string, modules ...string) error {
	worklist, ok, err := getPulseWorklistForRun(ctx, workspacePath, pulseRunID)
	if err != nil {
		return fmt.Errorf("read Pulse worklist: %w", err)
	}
	if !ok {
		return fmt.Errorf("Pulse worklist %q is missing", pulseRunID)
	}
	var unresolved []string
	for _, module := range modules {
		state, exists := worklist[module]
		if !exists || strings.TrimSpace(strings.ToLower(state.LastDecision)) != "due" {
			continue
		}
		if strings.TrimSpace(state.LastResult) == "" {
			unresolved = append(unresolved, module)
		}
	}
	if len(unresolved) > 0 {
		return fmt.Errorf("due Pulse modules lack terminal current-run results: %s", strings.Join(unresolved, ", "))
	}
	return nil
}

func pulseWorklistHasDueModule(ctx context.Context, workspacePath, pulseRunID string) (bool, error) {
	worklist, ok, err := getPulseWorklistForRun(ctx, workspacePath, pulseRunID)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, fmt.Errorf("Pulse worklist %q is missing", pulseRunID)
	}
	for _, module := range pulseModuleOrder {
		state, exists := worklist[module]
		if exists && strings.TrimSpace(strings.ToLower(state.LastDecision)) == "due" {
			return true, nil
		}
	}
	return false, nil
}

// pulseWorklistModulesDue reports whether ANY of the given specific modules
// is marked due in the durable worklist — unlike pulseWorklistHasDueModule
// (any module at all), this lets the caller ask a narrower question, needed
// once plan_drift_review and technical_review/strategic_review became
// separately-sequenced lifecycle steps (each only needs to know about its
// own module set, not "is anything due at all").
func pulseWorklistModulesDue(ctx context.Context, workspacePath, pulseRunID string, modules ...string) (bool, error) {
	worklist, ok, err := getPulseWorklistForRun(ctx, workspacePath, pulseRunID)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, fmt.Errorf("Pulse worklist %q is missing", pulseRunID)
	}
	for _, module := range modules {
		state, exists := worklist[module]
		if exists && strings.TrimSpace(strings.ToLower(state.LastDecision)) == "due" {
			return true, nil
		}
	}
	return false, nil
}

func validatePulseTechnicalRepairDrain(ctx context.Context, workspacePath, pulseRunID string) error {
	due, err := pulseWorklistModulesDue(ctx, workspacePath, pulseRunID, pulseModuleTechnicalReview)
	if err != nil {
		return fmt.Errorf("read technical review completion contract: %w", err)
	}
	if !due {
		return nil
	}
	remaining, err := stepworkflow.CountPulseActionableWorkflowIssues(ctx, workspacePath)
	if err != nil {
		return fmt.Errorf("read actionable Pulse repair backlog: %w", err)
	}
	if remaining > 0 {
		return fmt.Errorf("Review+Fix left %d actionable workflow-owned Pulse issue(s); the repair drain is incomplete", remaining)
	}
	return nil
}

func compactScheduleMessages(messages []string) []string {
	out := make([]string, 0, len(messages))
	for _, msg := range messages {
		if strings.TrimSpace(msg) == "" {
			continue
		}
		out = append(out, msg)
	}
	return out
}

type scheduledWorkshopTurn struct {
	label         string
	query         string
	upgradeTarget string
	// decisionDrain marks the pre-run turn that applies operator decisions the
	// user already answered. It runs on the Pulse LLM like an upgrade turn, but
	// unlike one it is never allowed to fail the run — see the loop in
	// executeWorkshopJob.
	decisionDrain bool
	// failureBlocksRun is set only by an approved decision contract that names
	// a safety/public-action boundary. Ordinary failed repairs keep the old safe
	// plan and do not cancel the scheduled run.
	failureBlocksRun bool
}

// workshopMode returns the least-privileged workflow phase needed by this
// scheduled turn. Normal schedule work is deployed workflow execution and runs
// with the smaller Run prompt/tool/skill surface. Contract upgrades and
// approved-decision application edit workflow-owned artifacts, so those
// bounded maintenance turns run in Workshop mode.
func (turn scheduledWorkshopTurn) workshopMode() string {
	if turn.upgradeTarget != "" || turn.decisionDrain {
		return "workshop"
	}
	return "run"
}

func scheduledDecisionApplyMode(input ReportHumanInput) string {
	mode := strings.ToLower(strings.TrimSpace(input.ApplyContract.Mode))
	switch mode {
	case "no_change", "direct_apply", "targeted_fixer", "external_wait":
		return mode
	default:
		return "legacy_manual"
	}
}

func scheduledDecisionIsApproval(input ReportHumanInput) bool {
	return strings.EqualFold(strings.TrimSpace(input.SelectedOptionID), "approve")
}

// scheduledDecisionPreflightTurns deterministically routes structured decisions.
// Legacy prose-only decisions deliberately receive no mutation turn: applying
// them would recreate the unsafe generic decision-applier path this contract
// replaces.
func scheduledDecisionPreflightTurns(pending []ReportHumanInput) []scheduledWorkshopTurn {
	direct := make([]ReportHumanInput, 0, len(pending))
	turns := make([]scheduledWorkshopTurn, 0, len(pending))
	for _, input := range pending {
		switch scheduledDecisionApplyMode(input) {
		case "no_change", "direct_apply":
			direct = append(direct, input)
		case "targeted_fixer":
			// The target scope is authorization to repair only after an explicit
			// approval. A rejection is still sent through the direct decision
			// handler so it can be truthfully consumed as no change.
			if scheduledDecisionIsApproval(input) {
				turns = append(turns, scheduledTargetedDecisionFixerTurn(input))
			} else {
				direct = append(direct, input)
			}
		}
	}
	if drain, ok := scheduledDecisionDrainTurn(direct); ok {
		turns = append([]scheduledWorkshopTurn{drain}, turns...)
	}
	return turns
}

func scheduledTargetedDecisionFixerTurn(input ReportHumanInput) scheduledWorkshopTurn {
	contract := input.ApplyContract
	issueClause := ""
	if issueID := strings.TrimSpace(contract.IssueID); issueID != "" {
		issueClause = fmt.Sprintf("Read get_pulse_state(view=\"backlog\", detail=\"full\") only for the linked issue %q before changing anything. ", issueID)
	}
	checks, _ := json.Marshal(contract.PreRunChecks)
	return scheduledWorkshopTurn{
		label:            "decision-fixer-preflight",
		decisionDrain:    true,
		failureBlocksRun: strings.EqualFold(contract.FailurePolicy, "block_run"),
		query:            fmt.Sprintf("PRE-RUN TARGETED FIXER. The operator answered approve for decision %q in %s. You are the bounded Fixer, not a reviewer and not the normal workflow run. Read it first with get_human_input_request and confirm it is still answered with selected_option_id=approve. Load read_skill(skills=[{\"name\":\"builder-reference\",\"path\":\"references/pulse-review-fixer.md\"},{\"name\":\"builder-reference\",\"path\":\"references/pulse-fixer-practices.md\"},{\"name\":\"builder-reference\",\"path\":\"references/fix-verification.md\"}]). %sApply ONLY this approved scope: %q. Required pre-run checks: %s. Post-run proof requirement: %q. Make the smallest coherent repair, run every required static or side-effect-free proof through the real consumer where possible, re-read the changed artifacts, and call validate_plan_change whenever planning changed. If the static proof passes, persist the truthful Fixer/Pulse lifecycle outcome (fixed_verified or changed_unverified as the evidence permits), then call mark_human_input_consumed with what changed and the remaining proof boundary. If you cannot prove it, do not consume the decision and do not broaden the repair. Do NOT run workflow steps, public actions, broad Pulse review, backup, publish, or notify.", input.ID, input.WorkspacePath, issueClause, contract.ApprovedScope, string(checks), contract.PostRunProof),
	}
}

// scheduledDecisionDrainTurn returns the pre-run turn that applies answered
// operator decisions, or ok=false when there are none to apply.
//
// Timing is the whole point (PLAT-092/PLAT-093). These decisions overwhelmingly
// change what a run should DO — scoring units, eval cadence, plan edits,
// enabling a feature — so draining them only in the post-run Pulse pass means
// the run that just happened still used the old behavior and the operator's
// answer lands a full cycle late. Measured on the stranded backlog: 24 of 26
// answered decisions required an action rather than a mere acknowledgement.
//
// Applying is deliberately an agent turn rather than Go: the decision's own
// context field carries a prose "what happens next if you approve" section
// written for the operator, not a machine-readable patch, and the typed plan,
// config, eval and schedule tools it needs already exist. This mirrors the
// contract-upgrade preflight, which is the proven precedent for mutating plan
// artifacts before the first schedule message.
func scheduledDecisionDrainTurn(pending []ReportHumanInput) (scheduledWorkshopTurn, bool) {
	if len(pending) == 0 {
		return scheduledWorkshopTurn{}, false
	}
	ids := make([]string, 0, len(pending))
	for _, input := range pending {
		id := strings.TrimSpace(input.ID)
		if id == "" {
			continue
		}
		if workspacePath := strings.TrimSpace(input.WorkspacePath); workspacePath != "" {
			id = workspacePath + ": " + id
		}
		if selected := strings.TrimSpace(input.SelectedOptionID); selected != "" {
			id += " (answered: " + selected + ")"
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return scheduledWorkshopTurn{}, false
	}

	return scheduledWorkshopTurn{
		label:         "decision-drain-preflight",
		decisionDrain: true,
		query: fmt.Sprintf(
			"PRE-RUN DECISION DRAIN. The operator has answered %d decision(s) that are still unapplied. "+
				"Apply them now, BEFORE this run starts, so the run uses what they decided rather than repeating the behavior they already asked you to change.\n\n"+
				"Answered and unapplied: %s\n\n"+
				"Read each one with get_human_input_request(workspace_path=<the exact Workflow/... path shown>, input_id=<the exact decision id shown>). Its `context` states what happens if approved, and `selected_option_id` is the operator's actual answer — honor that answer, including a rejection.\n\n"+
				"These are only direct_apply/no_change decisions. Never reinterpret their operator-facing prose as authority for a plan, prompt, route, validation, database, tool, or cross-artifact repair; those are routed to a dedicated targeted Fixer turn. For each decision, exactly one of:\n"+
				"1. APPLY it with the normal typed tools (plan modification, update_step_config, evaluation, schedule, workflow config), confirm the change actually landed by re-reading the artifact, then call mark_human_input_consumed with an outcome_summary naming what changed. Consume only what you truly applied — never to tidy the list.\n"+
				"2. LEAVE it, when you cannot honestly apply it now: the answer needs evidence from a run that has not happened yet, the premise no longer holds because the plan moved since it was answered (compare its run_id and answered_at against the current plan and changelog), or the intent is ambiguous. Say so plainly in your reply and do not consume it. The post-run Pulse pass will pick it up with fresh evidence.\n\n"+
				"SAFE VALIDATION IS PART OF APPLYING, NOT A REASON TO DEFER. If an approved change asks for a static check, dry-run, non-producing fixture, schema validation, or plan review, perform that proof in this turn. Only evidence that inherently requires a real production run or external side effect may wait for a later run.\n\n"+
				"STRUCTURAL CHANGE IMPACT AUDIT. When a decision changes step topology, routes, ids, dependencies, artifact paths, or orchestration shape, apply the whole coherent change rather than only rewiring `next_step_id`: migrate both control flow and data flow; set exact `context_dependencies` for every promoted consumer; update step configs and any evaluation, report, schedule, validation, or prompt references; search the current workflow artifacts for removed step ids and obsolete path prefixes; and test old/new artifact coexistence so stale output cannot be selected. Re-read the resulting plan, then call validate_plan_change with every removed id/path as forbidden_references and the exact expected_context_dependencies for every changed consumer. Treat passed=true as the required deterministic receipt, not as a substitute for your design judgment or any decision-specific fixture. If it fails, repair and rerun it; if any unexplained old reference remains or the proof still fails, do not consume the decision.\n\n"+
				"IMPACT FOLLOW-THROUGH. When an applied decision is intended to change a measurable workflow result, reliability measure, or measurement quality, call record_pulse_impact with one intervention linked by human_input_id=<the exact decision id>. Record the honest impact_type, metric, expected_direction, baseline_window, future checkpoint, and minimum_evidence_runs; start it as awaiting_evidence or measuring. This records what Pulse should measure later — it does not prove the decision worked. Do not invent an impact record for a rejection, wording-only/admin change, or any decision without a defensible metric. Later Pulse passes will append observations and an assessment when comparable evidence matures.\n\n"+
				"A rejection is applied by consuming it with an outcome_summary recording that the operator declined and nothing changed.\n\n"+
				"Do NOT run the workflow, execute steps, back up, publish, or notify — this turn only applies decisions. Stop when every decision above has been applied or explicitly left with a reason.",
			len(ids), strings.Join(ids, ", "),
		),
	}, true
}

// attachScheduledPendingDecisionNotice makes unanswered operator decisions
// visible to the first normal schedule turn without adding another LLM turn.
// Go only transports typed state here; it does not answer the question, choose
// an option, or block unrelated work. The agent can inspect the full request
// through get_human_input_request and must keep decision-dependent behavior on
// the currently approved configuration until the operator answers.
func attachScheduledPendingDecisionNotice(turns []scheduledWorkshopTurn, pending []ReportHumanInput) []scheduledWorkshopTurn {
	refs := make([]string, 0, len(pending))
	for _, input := range pending {
		id := strings.TrimSpace(input.ID)
		if id == "" {
			continue
		}
		ref := id
		if source := strings.TrimSpace(input.Source); source != "" {
			ref += " (source: " + source + ")"
		}
		refs = append(refs, ref)
	}
	if len(refs) == 0 {
		return turns
	}

	notice := fmt.Sprintf(
		"PENDING OPERATOR DECISIONS. The following typed decisions are unanswered: %s. "+
			"Do not infer an answer and do not apply their proposed changes. Continue only behavior that is valid under the currently approved workflow configuration. "+
			"If one affects this run, inspect it with get_human_input_request and clearly identify the decision-dependent portion you left unchanged; do not block unrelated safe work.\n\n",
		strings.Join(refs, ", "),
	)
	for i := range turns {
		if turns[i].upgradeTarget != "" || turns[i].decisionDrain {
			continue
		}
		turns[i].query = notice + turns[i].query
		break
	}
	return turns
}

func scheduledWorkshopTurns(manifest *WorkflowManifest, messages []string, workspacePath string) ([]scheduledWorkshopTurn, error) {
	upgradePlan := workflowVersionUpgradePlan(manifest)
	manifestVersion := workflowContractVersionForUpgrade(manifest)
	if manifestVersion != WorkflowContractCurrentVersion && (len(upgradePlan) == 0 || upgradePlan[len(upgradePlan)-1].to != WorkflowContractCurrentVersion) {
		return nil, fmt.Errorf(
			"workflow upgrade preflight has no complete upgrade path from version %q to %q; normal schedule message was not started: %w",
			manifestVersion,
			WorkflowContractCurrentVersion,
			errWorkflowUpgradePreflightBlocked,
		)
	}

	turns := make([]scheduledWorkshopTurn, 0, len(upgradePlan)+len(messages))
	for _, upgrade := range upgradePlan {
		query := bindWorkflowUpgradeWorkspacePath(upgrade.query, workspacePath)
		if strings.Contains(query, workflowUpgradeWorkspacePathPlaceholder) {
			return nil, fmt.Errorf("workflow upgrade preflight %s requires a workspace path", upgrade.label)
		}
		turns = append(turns, scheduledWorkshopTurn{
			label:         upgrade.label,
			query:         query,
			upgradeTarget: upgrade.to,
		})
	}
	for i, message := range messages {
		turns = append(turns, scheduledWorkshopTurn{
			label: fmt.Sprintf("schedule-message-%d", i+1),
			query: message,
		})
	}
	return turns, nil
}

func scheduledWorkshopMessages(sctx *ScheduleContext) []string {
	if sctx == nil {
		return nil
	}
	messages := compactScheduleMessages(sctx.Schedule.Messages)
	if sctx.PulseOnly {
		return messages
	}

	// A saved route is executable configuration, not optional prose.  Older
	// schedules could carry both route_selections and a follow-up message such
	// as "after the selected work completes...".  Returning only that message
	// silently discarded the selected route and left the builder to guess what
	// (if anything) it should run.  Always put the canonical route turn first;
	// retained schedule-specific messages are follow-ups, never replacements.
	if len(sctx.Schedule.RouteSelections) > 0 {
		groups, _ := json.Marshal(sctx.Schedule.GroupNames)
		routes, err := json.Marshal(sctx.Schedule.RouteSelections)
		instruction := fmt.Sprintf("Run the full workflow once for each configured schedule group %s using run_full_workflow.", string(groups))
		if err == nil {
			instruction = fmt.Sprintf("Run the full workflow once for each configured schedule group %s using run_full_workflow with route_selections=%s. Do not substitute a schedule-local procedure for the selected plan route.", string(groups), string(routes))
		}
		return append([]string{instruction + " " + scheduledBackgroundNoPollingInstruction}, messages...)
	}
	if len(messages) == 0 {
		groups, _ := json.Marshal(sctx.Schedule.GroupNames)
		instruction := fmt.Sprintf("Run the full workflow once for each configured schedule group %s using run_full_workflow.", string(groups))
		return []string{instruction + " " + scheduledBackgroundNoPollingInstruction}
	}
	if mode := strings.TrimSpace(sctx.Schedule.ExecutionMode); mode != "" {
		prefix := fmt.Sprintf("This invocation has backend-enforced execution_mode=%q; do not weaken or reinterpret it. ", mode)
		for i := range messages {
			messages[i] = prefix + messages[i]
		}
	}
	if sctx.QueuedOccurrenceCount > 1 {
		prefix := fmt.Sprintf("This catch-up invocation coalesces %d scheduled occurrences into one run; do not replay them separately. ", sctx.QueuedOccurrenceCount)
		for i := range messages {
			messages[i] = prefix + messages[i]
		}
	}
	return messages
}

// executeJob builds a session request from the manifest and runs it.
// Returns (sessionID, runFolder, error).
func (s *SchedulerService) executeJob(ctx context.Context, sctx *ScheduleContext, runID string) (string, string, error) {
	if mode := strings.TrimSpace(sctx.Schedule.Mode); mode != "" && mode != "workshop" {
		s.logf(sctx, "[SCHEDULER] Schedule %s uses legacy mode=%s; executing through workshop mode", sctx.Schedule.ID, mode)
	}
	return s.executeWorkshopJob(ctx, sctx, runID)
}

// executeWorkshopJob runs a workflow via the workshop builder path (workflow_phase mode).
func (s *SchedulerService) executeWorkshopJob(ctx context.Context, sctx *ScheduleContext, runID string) (string, string, error) {
	messages := scheduledWorkshopMessages(sctx)
	runFolder := "iteration-0"
	if sctx.PulseOnly && strings.TrimSpace(sctx.PulseEvidenceRunFolder) != "" {
		runFolder = strings.TrimSpace(sctx.PulseEvidenceRunFolder)
	}

	// Snapshot existing run folders before this invocation. executeWorkshopJob's
	// own runFolder above is workshop-session bookkeeping (always "iteration-0"
	// outside PulseOnly, never the folder the triggered workflow run actually
	// used — the LLM's run_full_workflow tool call picks that via the normal
	// iteration rotation). This snapshot lets the post-run check below tell a
	// NEW run folder created by this invocation from a pre-existing one.
	preRunFolders, preRunFoldersErr := s.api.loadRunFoldersInternal(ctx, sctx.WorkspacePath)
	preRunFolderNames := runFolderNameSet(preRunFolders)
	// A failed snapshot is not an empty workspace. Without this distinction the
	// reconciler below compares against an empty baseline, concludes every
	// pre-existing folder is new, and attributes an old failed run to this
	// invocation. The listing is most likely to fail right after a server
	// restart, while the workspace API is still warming up — exactly when
	// scheduled runs resume.
	runFolderBaselineKnown := preRunFoldersErr == nil
	invocationStartedAt := time.Now().UTC()
	sctx.ProducedRunEvidence = false

	sessionID := s.newScheduleSessionID(sctx)

	s.updateRuntimeState(scheduleRuntimeKey(sctx), func(state *ScheduleRuntimeState) {
		state.LastSessionID = sessionID
	})

	if runID != "" {
		_ = UpdateScheduleRun(ctx, sctx.WorkspacePath, runID, "running", "", nil, runFolder, sessionID)
	}

	s.sessionLogf(sctx, sessionID, "[SCHEDULER] Scheduled workflow: executing %d messages for %s (session=%s workspace=%s run_folder=%s pulse_only=%t)",
		len(messages), sctx.Schedule.ID, sessionID, sctx.WorkspacePath, runFolder, sctx.PulseOnly)

	baseReqMap := s.buildWorkshopRequest(ctx, sctx)

	// Workflow contract upgrades are a blocking preflight, not post-run cleanup.
	// A breaking runtime migration (for example message_sequence code items to
	// standalone scripted steps) must finish before the schedule's first normal
	// message can execute. The same builder session is reused so the upgrade is
	// visible in the schedule terminal and the normal run starts only after the
	// on-disk manifest confirms each target version.
	manifest, found, err := ReadWorkflowManifest(ctx, sctx.WorkspacePath)
	if err != nil {
		return sessionID, runFolder, fmt.Errorf("workflow upgrade preflight could not read manifest: %w", err)
	}
	if !found {
		return sessionID, runFolder, fmt.Errorf("workflow upgrade preflight: workflow manifest not found at %s", sctx.WorkspacePath)
	}
	turns, err := scheduledWorkshopTurns(manifest, messages, sctx.WorkspacePath)
	if err != nil {
		return sessionID, runFolder, err
	}
	upgradeCount := len(turns) - len(messages)
	if upgradeCount > 0 {
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] Running %d blocking workflow upgrade preflight turn(s) before %d schedule message(s)", upgradeCount, len(messages))
	}
	// Backstop for the paths that leave this loop without reaching adjudication
	// — a session that fails to start, or an idle wait that expires. Neither
	// may leave a live stamp authorization behind for the schedule messages
	// that follow, or for Pulse afterwards.
	defer contractupgrade.Revoke(sessionID)

	// Apply answered operator decisions before the run, not after it, so this
	// run behaves the way the operator already asked (PLAT-093). Inserted after
	// any contract upgrade — an upgrade can change the very artifacts a decision
	// edits — and before the first schedule message. Failure to read the store
	// is not a reason to skip the run: log it and continue unchanged.
	if pending, listErr := listReportHumanInputs(ctx, sctx.WorkspacePath, "answered", ""); listErr != nil {
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] Could not read answered decisions for the pre-run drain (continuing): %v", listErr)
	} else if decisionTurns := scheduledDecisionPreflightTurns(pending); len(decisionTurns) > 0 {
		insertAt := upgradeCount
		if insertAt < 0 || insertAt > len(turns) {
			insertAt = 0
		}
		turns = append(turns[:insertAt], append(decisionTurns, turns[insertAt:]...)...)
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] Running %d structured answered-decision preflight turn(s) before this run's first schedule message", len(decisionTurns))
	}
	// Unanswered decisions are not executable instructions and must never be
	// silently inferred. Surface them to the first normal schedule turn so the
	// agent can preserve the current approved behavior around the affected
	// boundary while continuing unrelated safe work. This adds no extra turn.
	if pending, listErr := listReportHumanInputs(ctx, sctx.WorkspacePath, "pending", ""); listErr != nil {
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] Could not read pending decisions for pre-run context (continuing): %v", listErr)
	} else if len(pending) > 0 {
		turns = attachScheduledPendingDecisionNotice(turns, pending)
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] Surfaced %d unanswered operator decision(s) to the first schedule message", len(pending))
	}

	// Once a preflight upgrade turn has failed open (see below), any FURTHER
	// upgrade turn in this same run is skipped without attempting it — later
	// migrations may assume the skipped one already ran. Message turns still
	// run normally; the next scheduled run recomputes the upgrade plan from
	// scratch and tries the preflight again.
	preflightFailedOpen := false
	for i, turn := range turns {
		if preflightFailedOpen && turn.upgradeTarget != "" {
			s.sessionLogf(sctx, sessionID, "[SCHEDULER] Skipping workshop turn %d/%d (%s): a prior upgrade preflight failed open this run", i+1, len(turns), turn.label)
			continue
		}
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] Workshop turn %d/%d (%s): %q", i+1, len(turns), turn.label, turn.query)

		reqMap := requestWithWorkshopMode(baseReqMap, turn.workshopMode())
		reqMap["query"] = turn.query
		if turn.upgradeTarget != "" || turn.decisionDrain {
			// The stamp is authorized for the life of this turn and no longer.
			// Revoking at adjudication below is what stops a later turn in the
			// same session from stamping a version this turn declined — the
			// confida-login 2026-08-12 case, where the stamp arrived ten
			// minutes after the verdict and the next preflight skipped the
			// migration it certified. See pkg/contractupgrade.
			contractupgrade.Mint(sessionID, turn.upgradeTarget)
		}

		// Resume the workflow's latest thread (same CLI) on the first message
		// only — later messages already share this run's live session.
		if i == 0 {
			if resumed := s.maybeResumeLatestWorkflowThread(ctx, sctx, reqMap, sessionID); resumed != "" {
				s.sessionLogf(sctx, sessionID, "[SCHEDULER] Resuming latest workflow thread %s for schedule %s", resumed, sctx.Schedule.ID)
			}
		}

		turnStartedAt := time.Now().UTC()
		if err := s.api.startSessionInternal(ctx, reqMap, sessionID, sctx.OwnerUserID, nil); err != nil {
			// A non-blocking direct decision turn that cannot start must not cost
			// the operator the run itself. The decisions stay answered-and-
			// unapplied, exactly as before this turn existed, and the post-run
			// Pulse pass still sees them (PLAT-093). A targeted Fixer with a
			// block_run contract is intentionally not exempt.
			if turn.decisionDrain && !turn.failureBlocksRun {
				s.sessionLogf(sctx, sessionID, "[SCHEDULER] Pre-run decision drain could not start (continuing to the run): %v", err)
				continue
			}
			s.preserveRunEvidenceAfterFailedTurn(ctx, sctx, sessionID, invocationStartedAt)
			return sessionID, runFolder, fmt.Errorf("workshop turn %d/%d (%s) failed: %w", i+1, len(turns), turn.label, err)
		}
		// A turn can dispatch cleanly and still produce nothing: every LLM
		// attempt fails, the failure is recorded as events, the session status
		// stays "completed", and no error reaches here. Without this check the
		// run is recorded success — hetzner-ssh did exactly that on 2026-08-18,
		// failing both turns on quota_exhausted in 8.1 seconds and being filed
		// as a successful security audit.
		//
		// A non-blocking decision turn is exempt for the same reason its
		// dispatch failure is: it must not cost the operator the run itself
		// (PLAT-093). A targeted Fixer with block_run is intentionally checked.
		if !turn.decisionDrain || turn.failureBlocksRun {
			if failure := scheduledTurnFailure(s.api.eventStore, sessionID, turnStartedAt); failure != "" {
				s.preserveRunEvidenceAfterFailedTurn(ctx, sctx, sessionID, invocationStartedAt)
				return sessionID, runFolder, fmt.Errorf("workshop turn %d/%d (%s) produced no response: %s", i+1, len(turns), turn.label, failure)
			}
		}

		// First message of the workshop sequence — stamp schedule name on
		// the session for frontend tab labeling. Subsequent calls are
		// no-ops (helper guards against overwriting an existing Title).
		s.stampScheduleNameOnSession(sessionID, sctx)

		if turn.upgradeTarget != "" {
			// Adjudicating the turn closes it. A turn that has been judged must
			// not be able to change the thing it was judged on, so the stamp
			// stops being accepted here — on the passing path as much as the
			// failing one.
			contractupgrade.Revoke(sessionID)
			updatedManifest, updatedFound, readErr := ReadWorkflowManifest(ctx, sctx.WorkspacePath)
			if readErr != nil {
				return sessionID, runFolder, fmt.Errorf("workflow upgrade preflight %s completed but manifest could not be re-read: %w", turn.label, readErr)
			}
			if !updatedFound || workflowContractVersionForUpgrade(updatedManifest) != turn.upgradeTarget {
				actual := "missing"
				if updatedFound {
					actual = workflowContractVersionForUpgrade(updatedManifest)
				}
				failOpen, failureCount, recordErr := RecordWorkflowSchedulePreflightFailure(ctx, sctx.WorkspacePath, sctx.Schedule, turn.upgradeTarget, time.Now())
				if recordErr != nil {
					s.sessionLogf(sctx, sessionID, "[SCHEDULER] WARNING: failed to persist preflight failure count for %s: %v", turn.label, recordErr)
				}
				if !failOpen {
					return sessionID, runFolder, workflowUpgradePreflightStampError(turn.label, turn.upgradeTarget, actual, failureCount)
				}
				s.sessionLogf(sctx, sessionID,
					"[SCHEDULER] WARNING: workflow upgrade preflight %s failed to stamp %q %d consecutive times (found %q). Failing OPEN: running the normal schedule message on the unstamped contract. This workflow needs owner attention — a supported configuration control could not complete this migration. Retrying the preflight fresh next scheduled run.",
					turn.label, turn.upgradeTarget, failureCount, actual)
				preflightFailedOpen = true
				continue
			}
			if clearErr := ClearWorkflowSchedulePreflightFailures(ctx, sctx.WorkspacePath, sctx.Schedule); clearErr != nil {
				s.sessionLogf(sctx, sessionID, "[SCHEDULER] WARNING: failed to clear preflight failure count for %s: %v", turn.label, clearErr)
			}
		}

		s.sessionLogf(sctx, sessionID, "[SCHEDULER] Workshop turn %d/%d (%s) completed", i+1, len(turns), turn.label)
	}

	// Note: backup-on-completion is not appended here as a message turn. Backup is
	// owned by two arms that share one source-hash-gated contract: the Pulse pass
	// (runPulseLifecycle, final step) for dedicated Pulse runs, and the
	// interactive-run completion directive for interactive runs (and as the fallback
	// when Pulse is off). The shared source-hash
	// gate means whichever arm runs second sees the state already backed up and skips
	// the push — so the overlap can't double-back up.

	// Previously auto-generated a static markdown report here via the report agent.
	// The dynamic report (design doc §2) is a live frontend view over db/ + graph.json;
	// there is no post-run artifact to produce, so scheduled runs now finish without a
	// report side-effect. Users open the report in the UI whenever they want.

	// The workshop chat session finishing its scheduled turns without an
	// infrastructure-level error is not proof the triggered workflow run
	// succeeded — the LLM's run_full_workflow tool call can fail deep inside
	// (e.g. an orchestrator agent-creation error) while the chat session
	// itself still goes idle normally. Reconcile against the run(s) actually
	// created during this invocation, per their own run_metadata.json — the
	// record the execution machinery writes independently of the scheduler
	// (BUG-20260729-10, social-media 2026-07-29: the scheduler recorded
	// "success" for a run that fully failed at its first posting step).
	postRunFolders, postRunFoldersErr := s.api.loadRunFoldersInternal(ctx, sctx.WorkspacePath)
	sctx.ProducedRunEvidence = workshopRunProducedEvidence(preRunFolderNames, postRunFolders, invocationStartedAt)
	if !sctx.ProducedRunEvidence && s.scheduledWorkflowExecutionProducedEvidence(sessionID, invocationStartedAt) {
		sctx.ProducedRunEvidence = true
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] the schedule's linked workflow execution receipt is this invocation's authoritative Pulse evidence")
	}
	if !sctx.ProducedRunEvidence {
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] no run folder was created or restarted during this invocation for %s; Pulse evidence-dependent stages will be skipped", sctx.Schedule.ID)
	}
	// Reconciliation is a set difference, so it is only meaningful when both
	// sides are real. When either listing failed, skip it and say so rather than
	// reconcile against a baseline we do not have: this check exists to catch a
	// run that failed while its session looked healthy, and inventing a failure
	// from a missing snapshot is a worse error than missing one. Failing open is
	// safe here — the run's own metadata remains the durable record, and the
	// next invocation reconciles normally once the listing recovers.
	if !runFolderBaselineKnown || postRunFoldersErr != nil {
		s.sessionLogf(sctx, sessionID,
			"[SCHEDULER] skipping run-outcome reconciliation for %s: run-folder listing unavailable (pre-run err=%v, post-run err=%v); this invocation's outcome stands on its session result alone",
			sctx.Schedule.ID, preRunFoldersErr, postRunFoldersErr)
	} else if failedFolder, found := reconcileWorkshopRunOutcome(preRunFolderNames, postRunFolders, invocationStartedAt); found {
		for _, folder := range postRunFolders {
			if folder.Name == failedFolder && folder.Metadata != nil && folder.Metadata.Recovery["status"] == "step_recovered_run_unverified" {
				return sessionID, runFolder, fmt.Errorf("workflow run %s remains failed: a step retry succeeded, but whole-run recovery is unverified (%v)", failedFolder, folder.Metadata.Recovery["reason"])
			}
		}
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] ⚠️ Workshop session for %s completed normally, but run %s recorded status \"failed\" in its own run_metadata.json", sctx.Schedule.ID, failedFolder)
		return sessionID, runFolder, fmt.Errorf("workflow run %s failed (its run_metadata.json records status \"failed\"), even though the orchestrating workshop session completed its turns without an infrastructure error", failedFolder)
	}

	s.sessionLogf(sctx, sessionID, "[SCHEDULER] ✅ Workshop execution completed for %s, session=%s, folder=%s", sctx.Schedule.ID, sessionID, runFolder)
	return sessionID, runFolder, nil
}

func runFolderNameSet(folders []RunFolderInfo) map[string]bool {
	names := make(map[string]bool, len(folders))
	for _, folder := range folders {
		if folder.Name != "" {
			names[folder.Name] = true
		}
	}
	return names
}

// preserveRunEvidenceAfterFailedTurn consults the durable record before a failed
// turn returns, so Pulse is never told "the workflow did not run" merely because
// the session stopped progressing.
//
// This is PLAT-071 restored. It shipped 2026-08-10 in the dedicated
// waitForWorkshopIdle failure branch, was extended by PLAT-084 the next day, and
// was dropped whole on 2026-08-13 when that branch was replaced by
// waitForConversationTurnTree — leaving workshopRunStartedDuringInvocation as
// dead code and both its unit tests still green, because they call that helper
// directly instead of driving this path. social-media then lost a second run to
// the identical bug on 2026-08-16: a post landed and was verified, the idle wait
// expired anyway, and the operator was emailed that nothing had run.
//
// It sits on the generic turn-failure return rather than an idle-wait-specific
// branch, which widens it deliberately: "did a run actually start?" is answered
// by the run's own metadata regardless of how the session died. Widening is safe
// because workshopRunStartedDuringInvocation demands a real timestamp at or after
// the invocation began and counts an unreadable record as nothing, so no failure
// mode here can manufacture evidence that does not exist.
func (s *SchedulerService) preserveRunEvidenceAfterFailedTurn(ctx context.Context, sctx *ScheduleContext, sessionID string, since time.Time) {
	if s == nil || s.api == nil || sctx == nil || sctx.ProducedRunEvidence {
		return
	}
	// Deliberately the baseline-free check: the pre-run snapshot can itself be
	// lost (PLAT-070), and an empty baseline would make every folder look new
	// and answer "evidence" unconditionally — the opposite error.
	if folders, listErr := s.api.loadRunFoldersInternal(ctx, sctx.WorkspacePath); listErr == nil &&
		workshopRunStartedDuringInvocation(folders, since) {
		sctx.ProducedRunEvidence = true
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] turn failed for %s, but a full workflow run started during this invocation and its own metadata is the authority; preserving run evidence for Pulse", sctx.Schedule.ID)
		return
	}
	if s.scheduledWorkflowExecutionProducedEvidence(sessionID, since) {
		sctx.ProducedRunEvidence = true
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] turn failed for %s, but a linked workflow execution started during this invocation; preserving run evidence for Pulse", sctx.Schedule.ID)
	}
}

// workshopRunStartedDuringInvocation reports whether any run's own metadata says
// it started during this invocation, using only the durable record.
//
// Deliberately takes no before-set. workshopRunProducedEvidence answers "is this
// folder new?" first, which is correct when it holds a real baseline but inverts
// when it does not: an empty baseline (a failed listing — PLAT-070) makes the
// very first folder look new and returns true unconditionally. On the idle-wait
// failure path the question is narrower and has a trustworthy answer that needs
// no baseline at all — did a run actually start? — so ask only that.
//
// A run whose metadata carries no usable timestamp is not counted. The caller
// treats a false here as "no evidence recorded", which is the pre-existing
// behavior, so an unreadable record never manufactures evidence.
func workshopRunStartedDuringInvocation(after []RunFolderInfo, since time.Time) bool {
	for _, folder := range after {
		if folder.Name == "" || folder.Metadata == nil {
			continue
		}
		for _, stamp := range []time.Time{folder.Metadata.StartedAt, folder.Metadata.CreatedAt} {
			if !stamp.IsZero() && !stamp.Before(since) {
				return true
			}
		}
	}
	return false
}

func workshopRunProducedEvidence(before map[string]bool, after []RunFolderInfo, since time.Time) bool {
	for _, folder := range after {
		if workshopRunStartedSince(before, folder, since) {
			return true
		}
	}
	return false
}

// Rotation changes folder names, not execution timestamps. Prefer StartedAt
// over CreatedAt so copying/rotating metadata cannot rejuvenate an old run.
// Name-only evidence is retained solely for legacy metadata without timestamps.
func workshopRunStartedSince(before map[string]bool, folder RunFolderInfo, since time.Time) bool {
	if folder.Name == "" {
		return false
	}
	if folder.Metadata != nil {
		if !folder.Metadata.StartedAt.IsZero() {
			return !folder.Metadata.StartedAt.Before(since)
		}
		if !folder.Metadata.CreatedAt.IsZero() {
			return !folder.Metadata.CreatedAt.Before(since)
		}
	}
	return !before[folder.Name]
}

// scheduledWorkflowExecutionProducedEvidence uses the execution receipt linked
// to this exact schedule session as the authoritative invocation boundary. This
// covers both run_full_workflow's full-run container and execute_step's declared
// workflow-step execution.
//
// The run-folder listing is intentionally only a secondary signal: it is capped
// for dashboard performance, so a workflow that reuses iteration-0/group after
// ten newer iterations exist can be absent even though its run_metadata.json was
// just completed. A generic background agent still does not count.
func (s *SchedulerService) scheduledWorkflowExecutionProducedEvidence(sessionID string, since time.Time) bool {
	if s == nil || s.api == nil || s.api.bgAgentRegistry == nil || strings.TrimSpace(sessionID) == "" {
		return false
	}
	for _, agent := range s.api.bgAgentRegistry.GetAll(sessionID) {
		if agent == nil {
			continue
		}
		snapshot := agent.GetSnapshot()
		if snapshot.CreatedAt.Before(since) {
			continue
		}
		kind := orchestratorevents.ExecutionKind(snapshot.Kind)
		if kind == orchestratorevents.ExecutionKindFullRun ||
			kind == orchestratorevents.ExecutionKindWorkflowStep ||
			snapshot.Metadata["execution_type"] == "full-workflow" ||
			snapshot.Metadata["execution_type"] == "workflow-step" {
			return true
		}
	}
	return false
}

// reconcileWorkshopRunOutcome finds explicit failures started in the invocation
// window. Timestamps take precedence over names because iteration rotation
// moves historical runs into newly named folders. Legacy timestamp-less records
// still use the before-set as a best-effort fallback.
//
// The since-based fallback matters because a workflow that reuses the same
// run-folder name every cycle (e.g. iteration-0/<group>, confirmed live on
// confida-login — see PLAT-182) never appears "new" by name after its first
// cycle: before[folder.Name] is already true every time, so a name-only check
// would skip inspecting that folder's metadata on every single subsequent
// invocation, regardless of what it actually recorded. workshopRunProducedEvidence,
// called moments earlier in the same code path for a different purpose, already
// carries this same since-based fallback for exactly this reason; this function
// previously lacked it.
//
// Ambiguous states — no metadata, "running", "completed", anything else — are
// still never treated as failure; only an explicit "failed" is, so a
// transient listing hiccup, or a workflow genuinely still running in the
// background, fails open toward "cannot verify" rather than toward a false
// failure.
func reconcileWorkshopRunOutcome(before map[string]bool, after []RunFolderInfo, since time.Time) (failedFolder string, found bool) {
	for _, folder := range after {
		if folder.Name == "" || folder.Metadata == nil {
			continue
		}
		if !workshopRunStartedSince(before, folder, since) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(folder.Metadata.Status), "failed") {
			return folder.Name, true
		}
	}
	return "", false
}

// maxWorkflowResumeScan bounds how many of this schedule's most recent runs the
// scheduler inspects when resume_previous=true: if a same-CLI resumable scheduled
// chat is among the latest few, resume it; otherwise start a fresh session.
const maxWorkflowResumeScan = 5

// maybeResumeLatestWorkflowThread wires restored_conversation_session_id into reqMap
// so an opt-in scheduled workflow run continues the schedule's most recent
// scheduled chat in the same Workshop/Run mode instead of starting fresh.
//
// We look at schedule-runs.json for this exact schedule_id first, then validate
// the referenced chat runtime. This deliberately excludes normal user/builder
// chats in the same workflow workspace. A different CLI's external session ID
// is meaningless to the new one, and crossing the Workshop/Run boundary would
// retain the wrong prompt/tool context. Prior run status (success/error) is
// intentionally ignored: resume happens regardless so the agent can recover
// from a failed run.
// Returns the resumed thread's session ID, or "" when the run should start fresh.
func (s *SchedulerService) maybeResumeLatestWorkflowThread(ctx context.Context, sctx *ScheduleContext, reqMap map[string]interface{}, currentSessionID string) string {
	if !sctx.Schedule.ShouldResumePrevious() {
		return ""
	}

	runs, err := listScheduleRunsForResume(ctx, sctx.WorkspacePath, sctx.Schedule.ID)
	if err != nil || len(runs) == 0 {
		return ""
	}
	return s.maybeResumeLatestScheduledThread(sctx, reqMap, currentSessionID, runs, sctx.WorkspacePath)
}

func (s *SchedulerService) maybeResumeLatestScheduledThread(sctx *ScheduleContext, reqMap map[string]interface{}, currentSessionID string, runs []ScheduleRunEntry, workspacePath string) string {
	currentProvider := ""
	if llmConfig := sctx.Capabilities.LLMConfig; llmConfig != nil {
		if llmConfig.BuilderLLM != nil {
			currentProvider = strings.TrimSpace(llmConfig.BuilderLLM.Provider)
		}
		if currentProvider == "" {
			currentProvider = strings.TrimSpace(llmConfig.Provider)
		}
		if currentProvider == "" {
			if builder, _, ok := workflowtypes.ResolveProviderProfileConfig(llmConfig); ok && builder != nil {
				currentProvider = strings.TrimSpace(builder.Provider)
			}
		}
	}
	if currentProvider == "" {
		return ""
	}
	currentWorkshopMode := "run"
	if execOpts, ok := reqMap["execution_options"].(map[string]interface{}); ok {
		if requested, ok := execOpts["workshop_mode"].(string); ok {
			currentWorkshopMode = normalizeChatHistoryWorkshopMode(requested)
		}
	}
	if currentWorkshopMode == "" {
		currentWorkshopMode = "run"
	}

	// Runs are newest-first. Within the latest maxWorkflowResumeScan scheduled
	// chats, resume the most recent one that is a resumable coding-agent thread
	// on the same CLI; skip any that don't qualify (e.g. an API-model thread).
	// If none qualify, start fresh.
	//
	// Validate via ReadChatHistoryRuntimeForSession(sessionID, workspace) — the
	// SAME resolver handleQuery uses when it later honors
	// restored_conversation_session_id — so what we match here is provably what
	// gets resumed.
	checked := 0
	for _, run := range runs {
		sessionID := strings.TrimSpace(run.SessionID)
		if sessionID == "" || sessionID == currentSessionID {
			continue
		}
		checked++
		if checked > maxWorkflowResumeScan {
			break
		}

		rt, ok, rErr := ReadChatHistoryRuntimeForSession("", sessionID, workspacePath)
		if rErr != nil || !ok || rt == nil {
			continue
		}
		// A coding-agent thread the CLI can resume: kind, matching CLI provider,
		// and a captured external session ID to hand to the CLI's --resume.
		if rt.Kind != "coding_agent" {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(rt.Provider), currentProvider) {
			continue
		}
		if !rt.ResumeSupported || strings.TrimSpace(rt.ExternalSessionID) == "" {
			continue
		}
		if !chatHistoryResumeModesCompatible(rt.WorkshopMode, currentWorkshopMode) {
			continue
		}
		reqMap["restored_conversation_session_id"] = sessionID
		return sessionID
	}
	return ""
}

func listScheduleRunsForResume(ctx context.Context, workspacePath, scheduleID string) ([]ScheduleRunEntry, error) {
	if localRuns, ok, err := readLocalScheduleRuns(workspacePath); ok || err != nil {
		if err != nil {
			return nil, err
		}
		return filterScheduleRunsNewestFirst(localRuns, scheduleID), nil
	}
	runs, _, err := ListScheduleRuns(ctx, workspacePath, scheduleID, maxScheduleRuns, 0)
	return runs, err
}

func readLocalScheduleRuns(workspacePath string) ([]ScheduleRunEntry, bool, error) {
	localPath := filepath.Join(fsutil.WorkspaceDocsRoot(), filepath.FromSlash(scheduleRunsPath(workspacePath)))
	data, err := os.ReadFile(localPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, true, err
	}
	var runs []ScheduleRunEntry
	if err := json.Unmarshal(data, &runs); err != nil {
		return nil, true, err
	}
	return runs, true, nil
}

func filterScheduleRunsNewestFirst(runs []ScheduleRunEntry, scheduleID string) []ScheduleRunEntry {
	filtered := make([]ScheduleRunEntry, 0, len(runs))
	for _, run := range runs {
		if run.ScheduleID == scheduleID {
			filtered = append(filtered, run)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].StartedAt.After(filtered[j].StartedAt)
	})
	if len(filtered) > maxScheduleRuns {
		filtered = filtered[:maxScheduleRuns]
	}
	return filtered
}

// stampScheduleNameOnSession updates the tracked session with the
// schedule's display name + triggered_by=cron so the frontend reconnect
// path can identify this as a scheduled run and label the tab using the
// schedule name instead of falling back to the literal "Workflow".
// Safe to call after startSessionInternal returns — the session is
// already tracked by then.
func (s *SchedulerService) stampScheduleNameOnSession(sessionID string, sctx *ScheduleContext) {
	if sctx == nil || strings.TrimSpace(sctx.Schedule.Name) == "" {
		return
	}
	s.api.activeSessionsMux.Lock()
	defer s.api.activeSessionsMux.Unlock()
	if sess, ok := s.api.activeSessions[sessionID]; ok && sess != nil {
		if sess.Title == "" {
			sess.Title = sctx.Schedule.Name
		}
		if sess.TriggeredBy == "" {
			sess.TriggeredBy = "cron"
		}
	}
}

// applyLLMAndSecretsToReqMap adds LLM config, API keys, secrets, and trigger payload to a request map.
func (s *SchedulerService) applyLLMAndSecretsToReqMap(ctx context.Context, reqMap map[string]interface{}, sctx *ScheduleContext) {
	if sctx.Capabilities.SelectedGlobalSecretNames != nil {
		reqMap["selected_global_secrets"] = sctx.Capabilities.SelectedGlobalSecretNames
	}

	if sctx.Capabilities.LLMConfig != nil {
		llmCfg := sctx.Capabilities.LLMConfig
		builderLLM := llmCfg.BuilderLLM
		if builderLLM == nil {
			if resolved, _, ok := workflowtypes.ResolveProviderProfileConfig(llmCfg); ok {
				builderLLM = resolved
			}
		}
		provider := ""
		modelID := ""
		var options map[string]interface{}
		if builderLLM != nil {
			provider = strings.TrimSpace(builderLLM.Provider)
			modelID = strings.TrimSpace(builderLLM.ModelID)
			options = builderLLM.Options
		}
		llmConfigSource := ""
		if provider != "" && modelID != "" {
			primary := map[string]interface{}{
				"provider": provider,
				"model_id": modelID,
			}
			if len(options) > 0 {
				primary["options"] = options
			}
			llmConfig := map[string]interface{}{
				"primary": primary,
			}
			reqMap["llm_config"] = llmConfig
			if llmConfigSource != "" {
				reqMap["llm_config_source"] = llmConfigSource
			}
		}
	}
	// API keys are now handled by MergedProviderAPIKeys in buildWorkshopConfig

	if len(sctx.Schedule.TriggerPayload) > 0 {
		var overrides map[string]interface{}
		if err := json.Unmarshal(sctx.Schedule.TriggerPayload, &overrides); err == nil {
			for k, v := range overrides {
				reqMap[k] = v
			}
		}
	}
}

func cloneStringInterfaceMap(in map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// requestWithWorkshopMode clones the request and its nested execution options
// before setting a per-turn mode. A shallow clone would share execution_options
// with later turns and could accidentally leave a normal run with Workshop
// authority after a migration or decision preflight.
func requestWithWorkshopMode(base map[string]interface{}, mode string) map[string]interface{} {
	reqMap := cloneStringInterfaceMap(base)
	execOpts := map[string]interface{}{}
	if current, ok := reqMap["execution_options"].(map[string]interface{}); ok {
		execOpts = cloneStringInterfaceMap(current)
	}
	execOpts["workshop_mode"] = mode
	reqMap["execution_options"] = execOpts
	return reqMap
}

// markPulseLifecycleTurn tags a Pulse lifecycle turn (Gate, review dispatch,
// Finalize) and elevates that turn to Workshop mode. It keeps the workflow's
// Builder model; pulse_llm applies to the fresh background review agents this
// turn launches (plan drift, technical and strategic review) and KB maintenance.
func markPulseLifecycleTurn(reqMap map[string]interface{}) {
	if reqMap == nil {
		return
	}
	reqMap["pulse_lifecycle_turn"] = true
	execOpts := map[string]interface{}{}
	if current, ok := reqMap["execution_options"].(map[string]interface{}); ok {
		execOpts = cloneStringInterfaceMap(current)
	}
	execOpts["workshop_mode"] = "workshop"
	reqMap["execution_options"] = execOpts
}

// buildWorkshopRequest creates the base request map for scheduled workflow
// execution. Normal turns default to Run mode; bounded preflight and Pulse
// callers explicitly elevate only their own turn to Workshop mode.
func (s *SchedulerService) buildWorkshopRequest(ctx context.Context, sctx *ScheduleContext) map[string]interface{} {
	reqMap := map[string]interface{}{
		"agent_mode":                  "workflow_phase",
		"phase_id":                    workflowtypes.WorkflowStatusWorkflowBuilder,
		"preset_query_id":             sctx.WorkflowID,
		"selected_folder":             sctx.WorkspacePath,
		"triggered_by":                "cron",
		"session_title":               sctx.Schedule.Name,
		"servers":                     sctx.Capabilities.SelectedServers,
		"selected_tools":              sctx.Capabilities.SelectedTools,
		"selected_skills":             sctx.Capabilities.SelectedSkills,
		"browser_mode":                sctx.Capabilities.BrowserMode,
		"use_code_execution_mode":     sctx.Capabilities.UseCodeExecutionMode,
		"disable_live_input_delivery": true,
		// Retain the coding CLI across consecutive turns with the same authority.
		// A Run↔Workshop transition is detected by the server and deliberately
		// relaunches in the mode-specific private runtime and prompt projection.
		"keep_native_session_alive": true,
	}
	if len(sctx.Capabilities.CDPPorts) > 0 {
		reqMap["cdp_ports"] = append([]int(nil), sctx.Capabilities.CDPPorts...)
	}
	if sctx.Capabilities.Notifications != nil {
		if secretName := strings.TrimSpace(sctx.Capabilities.Notifications.SlackWebhookSecretName); secretName != "" {
			reqMap["notification_slack_webhook_secret_name"] = secretName
		}
	}

	s.applyLLMAndSecretsToReqMap(ctx, reqMap, sctx)

	execOpts := map[string]interface{}{
		"selected_run_folder": "iteration-0",
		"execution_strategy":  "start_from_beginning_no_human",
		// A normal schedule is an unattended workflow run. Keep its prompt,
		// tools, projected skills, and private CLI working directory on the Run
		// surface. Maintenance callers elevate their individual turns explicitly.
		"workshop_mode": "run",
	}
	if sctx.CapacityResumeFromStep > 0 {
		// Resume exactly where the capacity wall stopped this run. Resume-from-step
		// cleans step N and everything after it, which is precisely the right
		// scope: step N never completed and no later step started, while steps
		// 1..N-1 completed and are preserved.
		execOpts["execution_strategy"] = stepworkflow.ExecutionStrategyResumeFromStepNoHuman
		execOpts["resume_from_step"] = sctx.CapacityResumeFromStep
		if sctx.CapacityResumeRunFolder != "" {
			execOpts["selected_run_folder"] = sctx.CapacityResumeRunFolder
		}
	}
	// Quota pacing, opt-in per workflow. The orchestrator cannot resolve
	// credentials, so the account identity is resolved here — where it already
	// is for the schedule-level gate — and passed down as a hash.
	if accountKey, threshold := s.quotaPacingForSchedule(ctx, sctx); accountKey != "" && threshold > 0 {
		execOpts["capacity_account_key"] = accountKey
		execOpts["pace_threshold_percent"] = threshold
	}
	if len(sctx.Schedule.GroupNames) > 0 {
		execOpts["enabled_group_names"] = sctx.Schedule.GroupNames
	}
	if mode := strings.TrimSpace(sctx.Schedule.ExecutionMode); mode != "" {
		execOpts["execution_mode"] = mode
	}
	reqMap["execution_options"] = execOpts

	return reqMap
}

var schedulerWorkshopMaxInactivity = 10 * time.Minute

// schedulerWorkshopLiveChildCeiling bounds how long a turn may be held open by
// child work that is registered as running but emits nothing observable. The
// inactivity window above answers "is anything happening?"; it is the wrong
// question once a child is demonstrably alive, because a legitimate step can be
// silent far longer than that — browser workflows deliberately pace themselves
// in tens of seconds per action, and a full run_full_workflow child runs for
// hours. This ceiling exists only so a child that hangs forever cannot block its
// schedule forever.
var schedulerWorkshopLiveChildCeiling = 3 * time.Hour

var errWorkshopIdleWaitTimeout = errors.New("workshop idle wait timed out")
var errWorkshopSequenceInterrupted = errors.New("workshop sequence interrupted by user")
var errWorkshopSessionFailed = errors.New("workshop session failed")

// errWorkflowUpgradePreflightBlocked marks a run that never started because a
// blocking contract-upgrade turn declined to stamp. Pulse is a post-run
// steward, and there is no run to steward here: the workflow did not execute,
// no evidence was produced, and the only honest thing a Pulse pass can do is
// spend an LLM turn saying so. Repeating that on every trigger of a workflow
// waiting on an owner decision is noise, so the caller skips Pulse entirely on
// this error.
var errWorkflowUpgradePreflightBlocked = errors.New("workflow upgrade preflight blocked")

// workflowUpgradePreflightStampError reports an upgrade turn that ran and
// declined to stamp. It wraps errWorkflowUpgradePreflightBlocked so the caller
// can tell this apart from a workflow that ran and failed — the difference
// decides whether Pulse has anything to do.
func workflowUpgradePreflightStampError(label, target, actual string, failureCount int) error {
	return fmt.Errorf(
		"workflow upgrade preflight %s did not stamp required version %q (found %q, failure %d/%d consecutive); normal schedule message was not started: %w",
		label, target, actual, failureCount, workflowSchedulePreflightFailOpenThreshold,
		errWorkflowUpgradePreflightBlocked,
	)
}

func (s *SchedulerService) refreshSessionTmuxSnapshotsForIdleCheck(ctx context.Context, sessionID string) error {
	if s == nil || s.api == nil {
		return nil
	}
	return s.api.refreshSessionTmuxSnapshotsForIdleCheck(ctx, sessionID)
}

func (api *StreamingAPI) refreshSessionTmuxSnapshotsForIdleCheck(ctx context.Context, sessionID string) error {
	if api == nil || api.terminalStore == nil || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	seenTmuxSessions := map[string]struct{}{}
	for _, snapshot := range api.terminalStore.ListMetadata(sessionID) {
		tmuxSession := strings.TrimSpace(snapshot.TmuxSession)
		if tmuxSession == "" {
			continue
		}
		if _, seen := seenTmuxSessions[tmuxSession]; seen {
			continue
		}
		seenTmuxSessions[tmuxSession] = struct{}{}

		captureCtx, cancel := context.WithTimeout(ctx, terminalTmuxActionTimeout)
		content, err := captureTerminalPaneLines(captureCtx, tmuxSession, terminalDefaultRefreshLines)
		cancel()
		if err != nil {
			if isMissingTmuxTargetError(err) {
				api.terminalStore.MarkStale(snapshot.TerminalID)
				continue
			}
			return fmt.Errorf("refresh tmux snapshot %q: %w", tmuxSession, err)
		}
		api.terminalStore.ReplaceContentWithSource(snapshot.TerminalID, content, "tmux_capture")
	}
	return nil
}

// updateRuntimeState owns the complete read-modify-write operation. Callers must
// never retain a runtime-state pointer after the mutex is released.
func (s *SchedulerService) updateRuntimeState(scheduleID string, update func(*ScheduleRuntimeState)) ScheduleRuntimeState {
	s.runtimeStatesMu.Lock()
	defer s.runtimeStatesMu.Unlock()
	state, ok := s.runtimeStates[scheduleID]
	if !ok {
		state = &ScheduleRuntimeState{}
		s.runtimeStates[scheduleID] = state
	}
	if update != nil {
		update(state)
	}
	return *state
}

// activateScheduleRunLocked publishes the active run and its cancellation
// handle as one runtime-state operation. Caller must hold runtimeStatesMu.
func (s *SchedulerService) activateScheduleRunLocked(state *ScheduleRuntimeState, runID string, startedAt time.Time) context.Context {
	state.LastStatus = "running"
	state.ActiveRunID = runID
	state.LastRunAt = &startedAt
	state.LastError = ""
	state.WaitingSince = nil
	state.WaitingUntil = nil
	state.WaitingReason = ""
	state.QueuedOccurrences = 0
	return s.registerScheduleRunContext(runID)
}

func (s *SchedulerService) rollbackScheduleRunActivation(runtimeKey, runID string, previous ScheduleRuntimeState) {
	s.runtimeStatesMu.Lock()
	if current := s.runtimeStates[runtimeKey]; current != nil && current.ActiveRunID == runID {
		*current = previous
	}
	s.runtimeStatesMu.Unlock()
	s.releaseScheduleRunContext(runID)
}

func (s *SchedulerService) abortCanceledScheduleRunBeforeStart(ctx context.Context, sctx *ScheduleContext, runtimeKey, runID string) bool {
	if ctx.Err() == nil {
		return false
	}
	s.updateRuntimeState(runtimeKey, func(state *ScheduleRuntimeState) {
		if state.ActiveRunID != runID {
			return
		}
		state.ActiveRunID = ""
		state.LastStatus = "stopped"
		state.LastError = "stopped before execution started"
	})
	s.transitionScheduleRun(context.Background(), sctx, schedulerstate.Transition{
		RunID: runID, To: schedulerstate.StateStopped, Reason: "stopped before execution started",
		ErrorMessage: "stopped before execution started", At: time.Now().UTC(),
	})
	s.releaseScheduleRunContext(runID)
	s.cleanupRemovedScheduleRuntimeState(runtimeKey)
	return true
}

func (s *SchedulerService) cleanupRemovedScheduleRuntimeState(runtimeKey string) {
	s.mu.Lock()
	_, known := s.scheduleFingerprints[runtimeKey]
	s.mu.Unlock()
	if known {
		return
	}
	s.runtimeStatesMu.Lock()
	if state := s.runtimeStates[runtimeKey]; state == nil || state.ActiveRunID == "" {
		delete(s.runtimeStates, runtimeKey)
	}
	s.runtimeStatesMu.Unlock()
}

func (s *SchedulerService) registerScheduleRunContext(runID string) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	s.runCancelsMu.Lock()
	if s.runCancels == nil {
		s.runCancels = make(map[string]context.CancelFunc)
	}
	if previous := s.runCancels[runID]; previous != nil {
		previous()
	}
	s.runCancels[runID] = cancel
	s.runCancelsMu.Unlock()
	return ctx
}

func (s *SchedulerService) cancelScheduleRunContext(runID string) {
	s.runCancelsMu.Lock()
	cancel := s.runCancels[runID]
	s.runCancelsMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *SchedulerService) releaseScheduleRunContext(runID string) {
	s.runCancelsMu.Lock()
	cancel := s.runCancels[runID]
	delete(s.runCancels, runID)
	s.runCancelsMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *SchedulerService) claimScheduleRun(ctx context.Context, sctx *ScheduleContext, runID string, startedAt time.Time) error {
	s.stateStoreMu.RLock()
	defer s.stateStoreMu.RUnlock()
	if s.stateStore == nil {
		return nil
	}
	scopeType, scopeID, lockKey := scheduleStateScope(sctx)
	triggerSource := strings.TrimSpace(sctx.TriggerSource)
	if triggerSource == "" {
		triggerSource = "cron"
	}
	run := schedulerstate.Run{
		RunID:         runID,
		ScopeType:     scopeType,
		ScopeID:       scopeID,
		LockKey:       lockKey,
		ScheduleID:    sctx.Schedule.ID,
		TriggerSource: triggerSource,
		ScheduledFor:  sctx.ScheduledFor,
		StartedAt:     startedAt,
	}
	if triggerSource == "queued" {
		return s.stateStore.BeginQueuedRun(ctx, run)
	}
	return s.stateStore.BeginRun(ctx, run)
}

func (s *SchedulerService) transitionScheduleRun(ctx context.Context, sctx *ScheduleContext, transition schedulerstate.Transition) {
	if strings.TrimSpace(transition.RunID) == "" {
		return
	}
	s.stateStoreMu.RLock()
	defer s.stateStoreMu.RUnlock()
	if s.stateStore == nil {
		return
	}
	transitionCtx := ctx
	if transitionCtx == nil {
		transitionCtx = context.Background()
	}
	attempts := 1
	if schedulerstate.IsTerminal(transition.To) {
		transitionCtx = context.WithoutCancel(transitionCtx)
		attempts = 3
	}
	var transitionErr error
	for attempt := 0; attempt < attempts; attempt++ {
		attemptCtx, cancel := context.WithTimeout(transitionCtx, 5*time.Second)
		transitionErr = s.stateStore.Transition(attemptCtx, transition)
		cancel()
		if transitionErr == nil {
			return
		}
		if attempt+1 < attempts {
			time.Sleep(time.Duration(attempt+1) * 50 * time.Millisecond)
		}
	}
	if schedulerstate.IsTerminal(transition.To) {
		recoveryCtx, cancel := context.WithTimeout(transitionCtx, 5*time.Second)
		recoveryErr := s.stateStore.ForceTerminal(recoveryCtx, transition)
		cancel()
		if recoveryErr == nil {
			if sctx != nil {
				s.logf(sctx, "[SCHEDULER_STATE] recovered terminal transition run=%s to=%s after error: %v", transition.RunID, transition.To, transitionErr)
			} else {
				scheduleLogf("[SCHEDULER_STATE] recovered terminal transition run=%s to=%s after error: %v", transition.RunID, transition.To, transitionErr)
			}
			return
		}
		transitionErr = errors.Join(transitionErr, recoveryErr)
	}
	if transitionErr != nil {
		if sctx != nil {
			s.logf(sctx, "[SCHEDULER_STATE] transition run=%s to=%s failed: %v", transition.RunID, transition.To, transitionErr)
		} else {
			scheduleLogf("[SCHEDULER_STATE] transition run=%s to=%s failed: %v", transition.RunID, transition.To, transitionErr)
		}
	}
}

func (s *SchedulerService) recordScheduleFireDecision(ctx context.Context, sctx *ScheduleContext, decision, reason, runID string, firedAt time.Time) error {
	if sctx == nil {
		return errors.New("schedule context is required")
	}
	s.stateStoreMu.RLock()
	defer s.stateStoreMu.RUnlock()
	if s.stateStore == nil {
		return errors.New("schedule state store is unavailable")
	}
	scopeType, scopeID, _ := scheduleStateScope(sctx)
	triggerSource := strings.TrimSpace(sctx.TriggerSource)
	if triggerSource == "" {
		triggerSource = "cron"
	}
	scheduledFor := sctx.ScheduledFor
	if scheduledFor.IsZero() {
		scheduledFor = firedAt
	}
	if err := s.stateStore.RecordFireDecision(ctx, schedulerstate.FireDecision{
		DecisionID:    uuid.NewString(),
		ScopeType:     scopeType,
		ScopeID:       scopeID,
		ScheduleID:    sctx.Schedule.ID,
		TriggerSource: triggerSource,
		Decision:      decision,
		Reason:        reason,
		RunID:         runID,
		ScheduledFor:  scheduledFor.UTC(),
		FiredAt:       firedAt,
	}); err != nil {
		s.logf(sctx, "[SCHEDULER_STATE] record fire decision=%s failed: %v", decision, err)
		return err
	}
	return nil
}

// getRuntimeStateLocked returns or creates runtime state. Caller MUST hold runtimeStatesMu write lock.
func (s *SchedulerService) getRuntimeStateLocked(scheduleID string) *ScheduleRuntimeState {
	if state, ok := s.runtimeStates[scheduleID]; ok {
		return state
	}
	state := &ScheduleRuntimeState{}
	s.runtimeStates[scheduleID] = state
	return state
}

func runningScheduleInSetLocked(runtimeStates map[string]*ScheduleRuntimeState, scheduleIDs []string, ignoreScheduleID string) (string, string) {
	for _, scheduleID := range scheduleIDs {
		if scheduleID == "" || scheduleID == ignoreScheduleID {
			continue
		}
		state := runtimeStates[scheduleID]
		if state == nil || state.LastStatus != "running" {
			continue
		}
		return scheduleID, state.LastSessionID
	}
	return "", ""
}

func (s *SchedulerService) findActiveNonBuilderExecutionForWorkspace(workspacePath string) *ActiveWorkflowExecution {
	if s == nil || s.api == nil || strings.TrimSpace(workspacePath) == "" {
		return nil
	}

	tracked := s.api.findRunningTrackedExecutionForWorkspaceWhere(workspacePath, func(exec *TrackedWorkflowExecution) bool {
		return trackedExecutionBlocksScheduledWorkflow(exec)
	})
	if tracked == nil {
		return nil
	}
	active := trackedExecutionToActive(tracked)
	return &active
}

// ScheduleSearchResult holds the result of finding a schedule by ID.
type ScheduleSearchResult struct {
	WorkspacePath string
	Manifest      *WorkflowManifest
	Index         int
}

// findScheduleByIDAny resolves a workflow schedule by ID.
func findScheduleByIDAny(ctx context.Context, scheduleID string) (*ScheduleSearchResult, error) {
	wsPath, manifest, idx, err := findScheduleByID(ctx, scheduleID)
	if err == nil {
		return &ScheduleSearchResult{
			WorkspacePath: wsPath,
			Manifest:      manifest,
			Index:         idx,
		}, nil
	}
	return nil, fmt.Errorf("schedule %s not found", scheduleID)
}

// findScheduleByID scans all workspace manifests to find a schedule by ID.
// Returns (workspacePath, manifest, scheduleIndex, error).
func findScheduleByID(ctx context.Context, scheduleID string) (string, *WorkflowManifest, int, error) {
	discovered, err := DiscoverWorkflowManifests(ctx)
	if err != nil {
		return "", nil, 0, fmt.Errorf("cannot scan workflow directory: %w", err)
	}

	for _, item := range discovered {
		for i, sched := range item.Manifest.Schedules {
			if sched.ID == scheduleID {
				return item.WorkspacePath, item.Manifest, i, nil
			}
		}
	}

	return "", nil, 0, fmt.Errorf("schedule %s not found in any manifest", scheduleID)
}

// getNextRunTime calculates the next scheduled run time.
func getNextRunTime(cronExpr string, timezone string) *time.Time {
	loc, err := time.LoadLocation(scheduleTimezoneOrDefault(timezone))
	if err != nil {
		loc = time.UTC
	}

	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse(cronExpr)
	if err != nil {
		return nil
	}

	next := schedule.Next(time.Now().In(loc)).UTC()
	return &next
}

func buildScheduleCronExpression(cronExpr string, timezone string) string {
	return fmt.Sprintf("CRON_TZ=%s %s", scheduleTimezoneOrDefault(timezone), cronExpr)
}

func scheduleTypeOrDefault(scheduleType string) string {
	if scheduleType == "" {
		return "cron"
	}
	return scheduleType
}

func calendarItemRunTime(sched WorkflowSchedule, item CalendarScheduleItem) (time.Time, error) {
	loc, err := time.LoadLocation(scheduleTimezoneOrDefault(sched.Timezone))
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timezone %q: %w", sched.Timezone, err)
	}
	if item.Date == "" || item.Time == "" {
		return time.Time{}, fmt.Errorf("calendar item date and time are required")
	}
	local, err := time.ParseInLocation("2006-01-02 15:04", item.Date+" "+item.Time, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid calendar item %q %q: expected date YYYY-MM-DD and time HH:MM", item.Date, item.Time)
	}
	return local.UTC(), nil
}

func getNextRunTimeForCalendar(sched WorkflowSchedule) *time.Time {
	now := time.Now().UTC()
	var next *time.Time
	for _, item := range sched.CalendarItems {
		runAt, err := calendarItemRunTime(sched, item)
		if err != nil || !runAt.After(now) {
			continue
		}
		if next == nil || runAt.Before(*next) {
			runAtCopy := runAt
			next = &runAtCopy
		}
	}
	return next
}

func scheduleWithCalendarItem(sched WorkflowSchedule, item CalendarScheduleItem) WorkflowSchedule {
	sched.TriggerPayload = item.TriggerPayload
	if len(item.Messages) > 0 {
		sched.Messages = item.Messages
	}
	return sched
}

func scheduleWithReloadedCalendarItem(sched WorkflowSchedule, requested *CalendarScheduleItem) (WorkflowSchedule, *CalendarScheduleItem, bool) {
	if requested == nil {
		return sched, nil, true
	}
	for i := range sched.CalendarItems {
		item := sched.CalendarItems[i]
		matches := requested.ID != "" && item.ID == requested.ID
		if requested.ID == "" {
			matches = item.Date == requested.Date && item.Time == requested.Time
		}
		if !matches {
			continue
		}
		itemCopy := item
		return scheduleWithCalendarItem(sched, itemCopy), &itemCopy, true
	}
	return sched, nil, false
}

func scheduleConfigFingerprint(sctx *ScheduleContext) string {
	if sctx == nil {
		return ""
	}
	payload, err := json.Marshal(struct {
		Schedule     WorkflowSchedule     `json:"schedule"`
		Capabilities WorkflowCapabilities `json:"capabilities"`
	}{Schedule: sctx.Schedule, Capabilities: sctx.Capabilities})
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256(payload))
}

func ValidateScheduleTimezone(timezone string) error {
	if timezone == "" {
		return fmt.Errorf("timezone is required; use an IANA timezone like UTC, Asia/Kolkata, or America/New_York")
	}
	if timezone != "UTC" && !strings.Contains(timezone, "/") {
		return fmt.Errorf("invalid timezone %q: use an IANA timezone like UTC, Asia/Kolkata, or America/New_York; abbreviations like EST, PST, or IST are not accepted", timezone)
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return fmt.Errorf("invalid timezone %q: use an IANA timezone like UTC, Asia/Kolkata, or America/New_York", timezone)
	}
	return nil
}

// ValidateCronExpression validates a 5-field cron expression.
func ValidateCronExpression(expr string) error {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	_, err := parser.Parse(expr)
	if err != nil {
		return fmt.Errorf("invalid cron expression: %w", err)
	}
	return nil
}

// lockedScheduleCapabilities is the manifest's capabilities as a scheduled run
// (and Pulse, and the capacity wait) must see them: the LLM config rewritten
// to the published default under LLM_CONFIG_LOCKED (lockedPresetLLMConfig),
// everything else untouched. Applied once where the ScheduleContext is built
// so every scheduler consumer of Capabilities.LLMConfig agrees.
func lockedScheduleCapabilities(caps WorkflowCapabilities) WorkflowCapabilities {
	caps.LLMConfig = lockedPresetLLMConfig(caps.LLMConfig)
	return caps
}
