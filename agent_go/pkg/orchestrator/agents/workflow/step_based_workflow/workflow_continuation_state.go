package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/agents"

	mcpagent "github.com/manishiitg/mcpagent/agent"
)

const workflowContinuationStateFilename = "continuation_state.json"

var workflowContinuationRunFolderMu sync.Mutex

const (
	workflowContinuationOwnerStepExecution = "workflow_step"

	workflowContinuationStatusPending        = "pending"
	workflowContinuationStatusRunning        = "running"
	workflowContinuationStatusWaitingForLock = "waiting_for_lock"
	workflowContinuationStatusRecoveryQueued = "recovery_queued"
	workflowContinuationStatusCompleted      = "completed"
	workflowContinuationStatusFailed         = "failed"
	workflowContinuationStatusSkipped        = "skipped"

	workflowContinuationPhaseMainExecution  = "main_execution"
	workflowContinuationPhasePreValidation  = "pre_validation"
	workflowContinuationPhaseKBReview       = "kb_review"
	workflowContinuationPhaseDirectLearning = "direct_learning"
	workflowContinuationPhaseLearningAgent  = "learning_agent"
	workflowContinuationPhaseKBUpdateAgent  = "kb_update_agent"
)

type WorkflowContinuationState struct {
	SchemaVersion      int                                        `json:"schema_version"`
	WorkspacePath      string                                     `json:"workspace_path,omitempty"`
	RunFolder          string                                     `json:"run_folder,omitempty"`
	StepID             string                                     `json:"step_id,omitempty"`
	StepPath           string                                     `json:"step_path,omitempty"`
	OwnerKind          string                                     `json:"owner_kind,omitempty"`
	AgentSessionHandle *mcpagent.AgentSessionHandle               `json:"agent_session_handle,omitempty"`
	Phases             map[string]WorkflowContinuationPhaseRecord `json:"phases,omitempty"`
	UpdatedAt          string                                     `json:"updated_at"`
}

type WorkflowContinuationPhaseRecord struct {
	Status             string                       `json:"status"`
	Error              string                       `json:"error,omitempty"`
	AgentSessionHandle *mcpagent.AgentSessionHandle `json:"agent_session_handle,omitempty"`
	UpdatedAt          string                       `json:"updated_at"`
}

type workflowContinuationStateUpdate struct {
	workspacePath string
	runFolder     string
	stepID        string
	stepPath      string
	ownerKind     string
	phase         string
	status        string
	errorMessage  string
	handle        *mcpagent.AgentSessionHandle
	now           time.Time
}

func currentAgentSessionHandle(agent agents.OrchestratorAgent) *mcpagent.AgentSessionHandle {
	if agent == nil {
		return nil
	}
	base := agent.GetBaseAgent()
	if base == nil || base.Agent() == nil {
		return nil
	}
	handle := base.Agent().CurrentAgentSessionHandle()
	if handle == nil || handle.Empty() {
		return nil
	}
	return handle
}

func workflowContinuationStatePath(workspacePath, runFolder, stepID, stepPath string) string {
	validationWorkspacePath := strings.TrimSpace(workspacePath)
	if runFolder = strings.TrimSpace(runFolder); runFolder != "" {
		validationWorkspacePath = fmt.Sprintf("%s/runs/%s", validationWorkspacePath, runFolder)
	}
	return fmt.Sprintf("%s/%s", getValidationFolderPath(validationWorkspacePath, stepID, stepPath), workflowContinuationStateFilename)
}

func (hcpo *StepBasedWorkflowOrchestrator) withWorkflowContinuationRunFolder(runFolder string, fn func()) {
	if hcpo == nil || fn == nil {
		return
	}
	workflowContinuationRunFolderMu.Lock()
	previous := hcpo.selectedRunFolder
	if strings.TrimSpace(runFolder) != "" {
		hcpo.selectedRunFolder = strings.TrimSpace(runFolder)
	}
	defer func() {
		hcpo.selectedRunFolder = previous
		workflowContinuationRunFolderMu.Unlock()
	}()
	fn()
}

func upsertWorkflowContinuationState(existing *WorkflowContinuationState, update workflowContinuationStateUpdate) *WorkflowContinuationState {
	now := update.now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	timestamp := now.UTC().Format(time.RFC3339Nano)
	state := existing
	if state == nil {
		state = &WorkflowContinuationState{}
	}
	state.SchemaVersion = 1
	state.WorkspacePath = update.workspacePath
	state.RunFolder = update.runFolder
	state.StepID = update.stepID
	state.StepPath = update.stepPath
	state.OwnerKind = update.ownerKind
	state.UpdatedAt = timestamp
	if update.handle != nil && !update.handle.Empty() {
		state.AgentSessionHandle = update.handle
	}
	if state.Phases == nil {
		state.Phases = make(map[string]WorkflowContinuationPhaseRecord)
	}
	if update.phase != "" && update.status != "" {
		phaseHandle := update.handle
		if (phaseHandle == nil || phaseHandle.Empty()) && state.Phases != nil {
			if existingPhase, ok := state.Phases[update.phase]; ok {
				phaseHandle = existingPhase.AgentSessionHandle
			}
		}
		state.Phases[update.phase] = WorkflowContinuationPhaseRecord{
			Status:             update.status,
			Error:              truncateWorkflowContinuationError(update.errorMessage),
			AgentSessionHandle: phaseHandle,
			UpdatedAt:          timestamp,
		}
	}
	return state
}

func workflowContinuationStatusNeedsRecovery(status string) bool {
	switch strings.TrimSpace(status) {
	case workflowContinuationStatusPending, workflowContinuationStatusRunning, workflowContinuationStatusWaitingForLock, workflowContinuationStatusRecoveryQueued:
		return true
	default:
		return false
	}
}

func workflowContinuationPhaseStatus(state *WorkflowContinuationState, phase string) string {
	if state == nil || state.Phases == nil {
		return ""
	}
	return strings.TrimSpace(state.Phases[phase].Status)
}

func workflowContinuationPhaseHandle(state *WorkflowContinuationState, phase string) *mcpagent.AgentSessionHandle {
	if state == nil {
		return nil
	}
	if state.Phases != nil {
		if record, ok := state.Phases[phase]; ok && record.AgentSessionHandle != nil && !record.AgentSessionHandle.Empty() {
			return record.AgentSessionHandle
		}
	}
	if state.AgentSessionHandle != nil && !state.AgentSessionHandle.Empty() {
		return state.AgentSessionHandle
	}
	return nil
}

func (state *WorkflowContinuationState) postStepGateSatisfied() bool {
	if state == nil {
		return false
	}
	if workflowContinuationPhaseStatus(state, workflowContinuationPhaseMainExecution) != workflowContinuationStatusCompleted {
		return false
	}
	preValidationStatus := workflowContinuationPhaseStatus(state, workflowContinuationPhasePreValidation)
	return preValidationStatus == "" || preValidationStatus == workflowContinuationStatusCompleted
}

func (state *WorkflowContinuationState) PendingRecoveryPhases() []string {
	if !state.postStepGateSatisfied() {
		return nil
	}
	phases := make([]string, 0, 4)
	for _, phase := range []string{
		workflowContinuationPhaseKBReview,
		workflowContinuationPhaseDirectLearning,
		workflowContinuationPhaseLearningAgent,
		workflowContinuationPhaseKBUpdateAgent,
	} {
		if workflowContinuationStatusNeedsRecovery(workflowContinuationPhaseStatus(state, phase)) {
			phases = append(phases, phase)
		}
	}
	return phases
}

func truncateWorkflowContinuationError(message string) string {
	message = strings.TrimSpace(message)
	if len(message) <= 2000 {
		return message
	}
	return message[:1000] + "\n... (truncated) ...\n" + message[len(message)-1000:]
}

func (hcpo *StepBasedWorkflowOrchestrator) recordWorkflowContinuationPhase(
	ctx context.Context,
	stepID string,
	stepPath string,
	ownerKind string,
	phase string,
	status string,
	errorMessage string,
	agent agents.OrchestratorAgent,
) {
	hcpo.recordWorkflowContinuationPhaseForRunFolder(ctx, hcpo.selectedRunFolder, stepID, stepPath, ownerKind, phase, status, errorMessage, agent)
}

func (hcpo *StepBasedWorkflowOrchestrator) recordWorkflowContinuationPhaseForRunFolder(
	ctx context.Context,
	runFolder string,
	stepID string,
	stepPath string,
	ownerKind string,
	phase string,
	status string,
	errorMessage string,
	agent agents.OrchestratorAgent,
) {
	if hcpo == nil || strings.TrimSpace(stepID) == "" || strings.TrimSpace(phase) == "" || strings.TrimSpace(status) == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	path := workflowContinuationStatePath(hcpo.GetWorkspacePath(), runFolder, stepID, stepPath)
	var existing *WorkflowContinuationState
	if content, err := hcpo.ReadWorkspaceFile(ctx, path); err == nil && strings.TrimSpace(content) != "" {
		var parsed WorkflowContinuationState
		if jsonErr := json.Unmarshal([]byte(content), &parsed); jsonErr == nil {
			existing = &parsed
		}
	}
	state := upsertWorkflowContinuationState(existing, workflowContinuationStateUpdate{
		workspacePath: hcpo.GetWorkspacePath(),
		runFolder:     strings.TrimSpace(runFolder),
		stepID:        stepID,
		stepPath:      stepPath,
		ownerKind:     ownerKind,
		phase:         phase,
		status:        status,
		errorMessage:  errorMessage,
		handle:        currentAgentSessionHandle(agent),
		now:           time.Now().UTC(),
	})
	body, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to marshal workflow continuation state for %s/%s: %v", stepID, phase, err))
		return
	}
	if err := hcpo.WriteWorkspaceFile(ctx, path, string(body)); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to persist workflow continuation state to %s: %v", path, err))
	}
}
