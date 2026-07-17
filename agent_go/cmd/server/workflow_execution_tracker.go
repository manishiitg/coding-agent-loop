package server

import (
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	trackedExecutionSourceWorkflowRun        = "workflow_run"
	trackedExecutionSourceWorkshopBackground = "workshop_background"

	trackedExecutionStatusRunning   = "running"
	trackedExecutionStatusCompleted = "completed"
	trackedExecutionStatusFailed    = "failed"
	trackedExecutionStatusCanceled  = "canceled"
)

const trackedExecutionRetention = 24 * time.Hour

// TrackedWorkflowExecution is the backend's unified execution record.
// It keeps top-level workflow runs and workflow-builder background work in one place.
type TrackedWorkflowExecution struct {
	ExecutionID   string            `json:"execution_id"`
	SessionID     string            `json:"session_id"`
	Source        string            `json:"source"`
	Kind          string            `json:"kind"`
	Name          string            `json:"name,omitempty"`
	Query         string            `json:"query,omitempty"`
	PresetQueryID string            `json:"preset_query_id,omitempty"`
	PresetName    string            `json:"preset_name,omitempty"`
	WorkspacePath string            `json:"workspace_path"`
	RunFolder     string            `json:"run_folder,omitempty"`
	PhaseID       string            `json:"phase_id,omitempty"`
	PhaseName     string            `json:"phase_name,omitempty"`
	Status        string            `json:"status"`
	UserID        string            `json:"user_id,omitempty"`
	Title         string            `json:"title,omitempty"`
	TriggeredBy   string            `json:"triggered_by,omitempty"`
	StartedAt     time.Time         `json:"started_at"`
	CompletedAt   *time.Time        `json:"completed_at,omitempty"`
	LastError     string            `json:"last_error,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`

	// Workflow-run UI state mirrored from the existing running-workflow API.
	IsMinimized      bool   `json:"is_minimized,omitempty"`
	MinimizedAt      int64  `json:"minimized_at,omitempty"`
	CurrentStepID    string `json:"current_step_id,omitempty"`
	CurrentStepTitle string `json:"current_step_title,omitempty"`
}

func normalizeTrackedWorkspacePath(workspacePath string) string {
	trimmed := strings.TrimSpace(workspacePath)
	if trimmed == "" {
		return ""
	}
	return filepath.Clean(trimmed)
}

func cloneTrackedMetadata(meta map[string]string) map[string]string {
	if len(meta) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(meta))
	for k, v := range meta {
		cloned[k] = v
	}
	return cloned
}

func normalizeTrackedExecutionKind(kind string) string {
	kind = strings.TrimSpace(strings.ToLower(kind))
	if kind == "" {
		return ""
	}
	return strings.ReplaceAll(kind, "-", "_")
}

func inferTrackedExecutionKind(source, phaseID, name string, meta map[string]string) string {
	if meta != nil {
		if executionType := normalizeTrackedExecutionKind(meta["execution_type"]); executionType != "" {
			return executionType
		}
	}

	lowerName := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.HasPrefix(lowerName, "step-"):
		return "step"
	case strings.Contains(lowerName, "full-workflow"):
		return "full_workflow"
	case strings.Contains(lowerName, "full workflow execution"):
		return "full_workflow"
	case strings.Contains(lowerName, "full evaluation"):
		return "full_evaluation"
	case strings.Contains(lowerName, "evaluation"):
		return "full_evaluation"
	}

	if source == trackedExecutionSourceWorkflowRun {
		if strings.TrimSpace(phaseID) != "" {
			return "workflow_phase"
		}
		return "workflow"
	}

	if phaseID == "workflow-builder" {
		return "workflow_builder_task"
	}

	return "background_task"
}

func trackedExecutionAppearsInRunningWorkflowList(exec *TrackedWorkflowExecution) bool {
	if exec == nil || exec.Status != trackedExecutionStatusRunning {
		return false
	}
	if exec.Source == trackedExecutionSourceWorkflowRun {
		return true
	}
	if exec.Source != trackedExecutionSourceWorkshopBackground {
		return false
	}
	kind := normalizeTrackedExecutionKind(exec.Kind)
	if kind == "" {
		kind = inferTrackedExecutionKind(exec.Source, exec.PhaseID, exec.Name, exec.Metadata)
	}
	return kind == "full_workflow" || kind == "workflow_builder_task"
}

func trackedExecutionToActive(exec *TrackedWorkflowExecution) ActiveWorkflowExecution {
	if exec == nil {
		return ActiveWorkflowExecution{}
	}
	kind := normalizeTrackedExecutionKind(exec.Kind)
	if kind == "" {
		kind = inferTrackedExecutionKind(exec.Source, exec.PhaseID, exec.Name, exec.Metadata)
	}
	return ActiveWorkflowExecution{
		QueryID:          exec.ExecutionID,
		SessionID:        exec.SessionID,
		Kind:             kind,
		PresetQueryID:    exec.PresetQueryID,
		PresetName:       exec.PresetName,
		WorkspacePath:    exec.WorkspacePath,
		RunFolder:        exec.RunFolder,
		PhaseID:          exec.PhaseID,
		PhaseName:        exec.PhaseName,
		Status:           exec.Status,
		UserID:           exec.UserID,
		Title:            exec.Title,
		Query:            exec.Query,
		TriggeredBy:      exec.TriggeredBy,
		StartedAt:        exec.StartedAt,
		IsMinimized:      exec.IsMinimized,
		MinimizedAt:      exec.MinimizedAt,
		CurrentStepID:    exec.CurrentStepID,
		CurrentStepTitle: exec.CurrentStepTitle,
	}
}

func (api *StreamingAPI) pruneTrackedExecutionsLocked(now time.Time) {
	for executionID, exec := range api.trackedWorkflowExecutions {
		if exec == nil {
			delete(api.trackedWorkflowExecutions, executionID)
			continue
		}
		if exec.Status == trackedExecutionStatusRunning {
			continue
		}
		referenceTime := exec.StartedAt
		if exec.CompletedAt != nil {
			referenceTime = *exec.CompletedAt
		}
		if referenceTime.IsZero() || now.Sub(referenceTime) > trackedExecutionRetention {
			delete(api.trackedWorkflowExecutions, executionID)
		}
	}
}

func (api *StreamingAPI) trackExecutionStart(exec *TrackedWorkflowExecution) {
	if api == nil || exec == nil || strings.TrimSpace(exec.ExecutionID) == "" {
		return
	}

	exec.WorkspacePath = normalizeTrackedWorkspacePath(exec.WorkspacePath)
	if exec.StartedAt.IsZero() {
		exec.StartedAt = time.Now().UTC()
	}
	if strings.TrimSpace(exec.Status) == "" {
		exec.Status = trackedExecutionStatusRunning
	}
	if strings.TrimSpace(exec.Kind) == "" {
		exec.Kind = inferTrackedExecutionKind(exec.Source, exec.PhaseID, exec.Name, exec.Metadata)
	}
	exec.Metadata = cloneTrackedMetadata(exec.Metadata)

	api.trackedWorkflowExecutionsMux.Lock()
	if api.trackedWorkflowExecutions == nil {
		api.trackedWorkflowExecutions = make(map[string]*TrackedWorkflowExecution)
	}
	api.trackedWorkflowExecutions[exec.ExecutionID] = exec
	api.pruneTrackedExecutionsLocked(time.Now().UTC())
	api.trackedWorkflowExecutionsMux.Unlock()
	api.observeRuntimeSnapshot(exec.SessionID)
}

func (api *StreamingAPI) trackWorkflowRunStart(exec *ActiveWorkflowExecution) {
	if exec == nil || strings.TrimSpace(exec.QueryID) == "" {
		return
	}
	api.trackExecutionStart(&TrackedWorkflowExecution{
		ExecutionID:      exec.QueryID,
		SessionID:        exec.SessionID,
		Source:           trackedExecutionSourceWorkflowRun,
		Name:             exec.Title,
		Query:            exec.Query,
		PresetQueryID:    exec.PresetQueryID,
		PresetName:       exec.PresetName,
		WorkspacePath:    exec.WorkspacePath,
		RunFolder:        exec.RunFolder,
		PhaseID:          exec.PhaseID,
		PhaseName:        exec.PhaseName,
		Status:           exec.Status,
		UserID:           exec.UserID,
		Title:            exec.Title,
		TriggeredBy:      exec.TriggeredBy,
		StartedAt:        exec.StartedAt,
		IsMinimized:      exec.IsMinimized,
		MinimizedAt:      exec.MinimizedAt,
		CurrentStepID:    exec.CurrentStepID,
		CurrentStepTitle: exec.CurrentStepTitle,
	})
}

func (api *StreamingAPI) trackWorkshopExecutionStart(sessionID, workspacePath, presetQueryID, userID string, executionID, name string) {
	api.trackExecutionStart(&TrackedWorkflowExecution{
		ExecutionID:   executionID,
		SessionID:     sessionID,
		Source:        trackedExecutionSourceWorkshopBackground,
		Name:          name,
		Title:         name,
		PresetQueryID: presetQueryID,
		WorkspacePath: workspacePath,
		PhaseID:       "workflow-builder",
		PhaseName:     "Workflow Builder",
		Status:        trackedExecutionStatusRunning,
		UserID:        userID,
		TriggeredBy:   "workflow_builder",
		StartedAt:     time.Now().UTC(),
	})
}

func (api *StreamingAPI) completeTrackedExecution(executionID, status, errorMessage string, meta map[string]string) {
	if api == nil || strings.TrimSpace(executionID) == "" {
		return
	}
	if strings.TrimSpace(status) == "" {
		status = trackedExecutionStatusCompleted
	}

	api.trackedWorkflowExecutionsMux.Lock()

	exec := api.trackedWorkflowExecutions[executionID]
	if exec == nil || exec.Status != trackedExecutionStatusRunning {
		api.trackedWorkflowExecutionsMux.Unlock()
		return
	}
	sessionID := exec.SessionID

	now := time.Now().UTC()
	exec.Status = status
	exec.CompletedAt = &now
	if strings.TrimSpace(errorMessage) != "" {
		exec.LastError = errorMessage
	}
	if len(meta) > 0 {
		exec.Metadata = cloneTrackedMetadata(meta)
		if runFolder := strings.TrimSpace(meta["run_folder"]); runFolder != "" {
			exec.RunFolder = runFolder
		}
		exec.Kind = inferTrackedExecutionKind(exec.Source, exec.PhaseID, exec.Name, meta)
	}
	api.pruneTrackedExecutionsLocked(now)
	api.trackedWorkflowExecutionsMux.Unlock()
	api.observeRuntimeSnapshot(sessionID)
}

func (api *StreamingAPI) cancelTrackedExecutionsForSession(sessionID string) {
	if api == nil || strings.TrimSpace(sessionID) == "" {
		return
	}

	api.trackedWorkflowExecutionsMux.Lock()

	now := time.Now().UTC()
	for _, exec := range api.trackedWorkflowExecutions {
		if exec == nil || exec.SessionID != sessionID || exec.Status != trackedExecutionStatusRunning {
			continue
		}
		exec.Status = trackedExecutionStatusCanceled
		exec.CompletedAt = &now
	}
	api.pruneTrackedExecutionsLocked(now)
	api.trackedWorkflowExecutionsMux.Unlock()
	api.observeRuntimeSnapshot(sessionID)
}

func (api *StreamingAPI) finalizeTrackedExecutionIfRunning(executionID, status, errorMessage string) {
	if api == nil || strings.TrimSpace(executionID) == "" {
		return
	}

	api.trackedWorkflowExecutionsMux.Lock()

	exec := api.trackedWorkflowExecutions[executionID]
	if exec == nil || exec.Status != trackedExecutionStatusRunning {
		api.trackedWorkflowExecutionsMux.Unlock()
		return
	}
	sessionID := exec.SessionID

	now := time.Now().UTC()
	exec.Status = status
	exec.CompletedAt = &now
	if strings.TrimSpace(errorMessage) != "" {
		exec.LastError = errorMessage
	}
	api.pruneTrackedExecutionsLocked(now)
	api.trackedWorkflowExecutionsMux.Unlock()
	api.observeRuntimeSnapshot(sessionID)
}

// runningWorkflowExecutionBySessionLocked returns the latest running top-level workflow execution.
// api.trackedWorkflowExecutionsMux must already be held.
func (api *StreamingAPI) runningWorkflowExecutionBySessionLocked(sessionID string) *TrackedWorkflowExecution {
	var best *TrackedWorkflowExecution
	for _, exec := range api.trackedWorkflowExecutions {
		if exec == nil || exec.Source != trackedExecutionSourceWorkflowRun || exec.Status != trackedExecutionStatusRunning || exec.SessionID != sessionID {
			continue
		}
		if best == nil || exec.StartedAt.After(best.StartedAt) {
			best = exec
		}
	}
	return best
}

// runningTrackedExecutionBySessionLocked returns the latest running tracked execution
// for a session, including workflow-builder background work. api.trackedWorkflowExecutionsMux
// must already be held.
func (api *StreamingAPI) runningTrackedExecutionBySessionLocked(sessionID string) *TrackedWorkflowExecution {
	var best *TrackedWorkflowExecution
	for _, exec := range api.trackedWorkflowExecutions {
		if exec == nil || exec.Status != trackedExecutionStatusRunning || exec.SessionID != sessionID {
			continue
		}
		if best == nil || exec.StartedAt.After(best.StartedAt) {
			best = exec
		}
	}
	return best
}

// runningWorkflowListExecutionBySessionLocked returns the latest running
// execution for a session that is eligible for /api/workflow/running. This
// deliberately differs from runningTrackedExecutionBySessionLocked, which can
// return internal workflow steps/sub-agents that should not be top-level rows.
// api.trackedWorkflowExecutionsMux must already be held.
func (api *StreamingAPI) runningWorkflowListExecutionBySessionLocked(sessionID string) *TrackedWorkflowExecution {
	var best *TrackedWorkflowExecution
	for _, exec := range api.trackedWorkflowExecutions {
		if exec == nil || exec.SessionID != sessionID || !trackedExecutionAppearsInRunningWorkflowList(exec) {
			continue
		}
		if best == nil || exec.StartedAt.After(best.StartedAt) {
			best = exec
		}
	}
	return best
}

func (api *StreamingAPI) listRunningWorkflowExecutions(userID string) []ActiveWorkflowExecution {
	api.trackedWorkflowExecutionsMux.RLock()
	list := make([]ActiveWorkflowExecution, 0, len(api.trackedWorkflowExecutions))
	for _, exec := range api.trackedWorkflowExecutions {
		if !trackedExecutionAppearsInRunningWorkflowList(exec) {
			continue
		}
		if userID != "" && exec.UserID != "" && exec.UserID != userID {
			continue
		}
		list = append(list, trackedExecutionToActive(exec))
	}
	api.trackedWorkflowExecutionsMux.RUnlock()

	// Enrich with blocking-input state after releasing the map lock because
	// deriveSessionUserInputState reads from the eventStore (separate lock).
	for i := range list {
		needsInput, _, waitingSince, waitingMessage := api.deriveSessionUserInputState(list[i].SessionID)
		list[i].NeedsUserInput = needsInput
		list[i].WaitingMessage = waitingMessage
		list[i].WaitingSince = waitingSince
		if snapshot, ok := api.authoritativeRuntimeSnapshot(list[i].SessionID); ok {
			list[i].RuntimeState = &snapshot
		}
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].StartedAt.After(list[j].StartedAt)
	})
	return list
}

func (api *StreamingAPI) listRunningWorkflowExecutionsForWorkspace(workspacePath string) []ActiveWorkflowExecution {
	normalizedWorkspace := normalizeTrackedWorkspacePath(workspacePath)

	api.trackedWorkflowExecutionsMux.RLock()
	list := make([]ActiveWorkflowExecution, 0, len(api.trackedWorkflowExecutions))
	for _, exec := range api.trackedWorkflowExecutions {
		if !trackedExecutionAppearsInRunningWorkflowList(exec) {
			continue
		}
		if normalizedWorkspace != "" && exec.WorkspacePath != normalizedWorkspace {
			continue
		}
		list = append(list, trackedExecutionToActive(exec))
	}
	api.trackedWorkflowExecutionsMux.RUnlock()

	// Enrich with blocking-input state after releasing the map lock.
	for i := range list {
		needsInput, _, waitingSince, waitingMessage := api.deriveSessionUserInputState(list[i].SessionID)
		list[i].NeedsUserInput = needsInput
		list[i].WaitingMessage = waitingMessage
		list[i].WaitingSince = waitingSince
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].StartedAt.After(list[j].StartedAt)
	})
	return list
}

func (api *StreamingAPI) findRunningTrackedExecutionForWorkspaceWhere(
	workspacePath string,
	matches func(*TrackedWorkflowExecution) bool,
) *TrackedWorkflowExecution {
	normalizedWorkspace := normalizeTrackedWorkspacePath(workspacePath)
	if normalizedWorkspace == "" {
		return nil
	}

	api.trackedWorkflowExecutionsMux.RLock()
	defer api.trackedWorkflowExecutionsMux.RUnlock()

	var best *TrackedWorkflowExecution
	for _, exec := range api.trackedWorkflowExecutions {
		if exec == nil || exec.Status != trackedExecutionStatusRunning || exec.WorkspacePath != normalizedWorkspace {
			continue
		}
		if matches != nil && !matches(exec) {
			continue
		}
		if best == nil || exec.StartedAt.After(best.StartedAt) {
			copyExec := *exec
			best = &copyExec
		}
	}
	return best
}
