package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	stepworkflow "github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
)

// crewStepPollInterval is the cadence at which a crew step polls its Crew
// run. Crews answer on the order of minutes, so five seconds keeps run
// metadata fresh without hammering the store.
const crewStepPollInterval = 5 * time.Second

// crewStepMaxRecoveryAttempts caps how many terminal-failed duplicate
// deliveries one crew step invocation looks past before giving up. Each
// iteration consumes one real prior failed attempt, so the cap is a
// failsafe, not a retry budget.
const crewStepMaxRecoveryAttempts = 100

// crewStepRunner implements stepworkflow.CrewStepRunner: it invokes a Crew
// trigger for one workflow crew step and polls the run to terminal state.
// One runner serves one workflow run; it closes over the run owner, so crew
// lookups stay user-scoped without trusting step content for identity.
type crewStepRunner struct {
	crews        *ProductScheduleService
	userID       string
	pollInterval time.Duration
}

func newCrewStepRunner(crews *ProductScheduleService, userID string) *crewStepRunner {
	return &crewStepRunner{crews: crews, userID: userID, pollInterval: crewStepPollInterval}
}

// RunCrewStep invokes the trigger and returns the successful Crew run's
// final response. Anything else — revoked access, crew run failed or
// stopped, timeout, or cancellation — returns an error.
func (r *crewStepRunner) RunCrewStep(ctx context.Context, req stepworkflow.CrewStepRequest) (stepworkflow.CrewStepResult, error) {
	if r == nil || r.crews == nil {
		return stepworkflow.CrewStepResult{}, fmt.Errorf("crew steps are unavailable: the crew service is not bound")
	}
	caller := triggerCaller{Type: triggerCallerWorkflow, ID: req.WorkflowID}
	// Fail fast on revoked access before dispatching, using the same
	// permission check as trigger invocation.
	if err := r.crews.checkInternalCrewAccess(ctx, r.userID, req.ProfileID, req.ProjectID, req.TriggerID, caller); err != nil {
		return stepworkflow.CrewStepResult{}, fmt.Errorf("crew step %q cannot invoke its trigger: %w", req.StepID, err)
	}
	payload, err := json.Marshal(map[string]interface{}{
		"source":           "workflow",
		"workflow_id":      req.WorkflowID,
		"workflow_run_id":  req.WorkflowRunFolder,
		"workflow_step_id": req.StepID,
		"group":            req.Group,
		"instruction":      req.Instruction,
		"inputs":           req.Inputs,
	})
	if err != nil {
		return stepworkflow.CrewStepResult{}, fmt.Errorf("crew step %q cannot encode its trigger payload: %w", req.StepID, err)
	}
	// The delivery ID is stable per step attempt, so a retried run adopts
	// the live delivery instead of invoking the trigger twice.
	base := "crew-step:" + req.WorkflowID + ":" + req.WorkflowRunFolder + ":" + req.StepID
	runID := ""
	for attempt := 0; ; attempt++ {
		deliveryID := base
		if attempt > 0 {
			deliveryID = fmt.Sprintf("%s#retry-%d", base, attempt)
		}
		if attempt > crewStepMaxRecoveryAttempts {
			return stepworkflow.CrewStepResult{}, fmt.Errorf("crew step %q gave up after %d prior failed attempts", req.StepID, attempt)
		}
		result, err := r.crews.dispatchInternalProductTrigger(ctx, internalCrewTriggerCall{
			UserID: r.userID, ProfileID: req.ProfileID, ProjectID: req.ProjectID, TriggerID: req.TriggerID,
			Caller: caller, WorkflowRunID: req.WorkflowRunFolder, WorkflowStepID: req.StepID,
			DeliveryID: deliveryID, Payload: payload,
		})
		if err != nil {
			return stepworkflow.CrewStepResult{}, fmt.Errorf("crew step %q cannot invoke its trigger: %w", req.StepID, err)
		}
		runID = result.RunID
		if !result.Duplicate {
			break
		}
		// A duplicate adopts the earlier delivery: a live run is polled
		// below, a successful run is returned as-is, and a terminally
		// failed run re-invokes with a fresh delivery ID.
		status, err := r.crews.getInternalProductTriggerRun(ctx, r.userID, req.ProfileID, req.ProjectID, req.TriggerID, runID, caller)
		if err != nil {
			if ctxErr := crewStepContextError(ctx, req, runID); ctxErr != nil {
				return stepworkflow.CrewStepResult{}, ctxErr
			}
			return stepworkflow.CrewStepResult{}, fmt.Errorf("crew step %q cannot read its adopted run: %w", req.StepID, err)
		}
		if !status.Terminal {
			break
		}
		if status.Status == "success" {
			adopted := crewStepResultFromStatus(status)
			adopted.Timeline = appendCrewPollTransition(nil, status.Status)
			return adopted, nil
		}
	}
	return r.pollCrewStep(ctx, req, caller, runID)
}

// pollCrewStep waits for one Crew run to reach terminal state. The first
// poll is immediate; later polls wait pollInterval. Timeout and
// cancellation arrive through ctx, which the executor deadlines from the
// step's timeout_seconds.
func (r *crewStepRunner) pollCrewStep(ctx context.Context, req stepworkflow.CrewStepRequest, caller triggerCaller, runID string) (stepworkflow.CrewStepResult, error) {
	interval := r.pollInterval
	if interval <= 0 {
		interval = crewStepPollInterval
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	partial := stepworkflow.CrewStepResult{CrewRunID: runID}
	for {
		select {
		case <-ctx.Done():
			return partial, crewStepContextError(ctx, req, runID)
		case <-timer.C:
		}
		status, err := r.crews.getInternalProductTriggerRun(ctx, r.userID, req.ProfileID, req.ProjectID, req.TriggerID, runID, caller)
		if err != nil {
			if ctxErr := crewStepContextError(ctx, req, runID); ctxErr != nil {
				return partial, ctxErr
			}
			return partial, fmt.Errorf("crew step %q lost its crew run %s: %w", req.StepID, runID, err)
		}
		observed := crewStepResultFromStatus(status)
		observed.Timeline = appendCrewPollTransition(partial.Timeline, status.Status)
		partial = observed
		if !status.Terminal {
			timer.Reset(interval)
			continue
		}
		if status.Status == "success" {
			return partial, nil
		}
		detail := strings.TrimSpace(status.Error)
		if detail == "" {
			detail = "no error detail recorded"
		}
		return partial, fmt.Errorf("crew step %q failed: crew run %s ended %s: %s", req.StepID, runID, status.Status, detail)
	}
}

// crewStepContextError reports a timeout or cancellation when ctx ended,
// or nil when ctx is still alive. Poll requests that fail because the
// deadline lapsed mid-request surface as timeouts, not request errors.
func crewStepContextError(ctx context.Context, req stepworkflow.CrewStepRequest, runID string) error {
	if ctx.Err() == nil {
		return nil
	}
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("crew step %q timed out after %d seconds waiting for crew run %s", req.StepID, req.TimeoutSeconds, runID)
	}
	return fmt.Errorf("crew step %q canceled while waiting for crew run %s: %w", req.StepID, runID, ctx.Err())
}

func crewStepResultFromStatus(status productWebhookRunStatus) stepworkflow.CrewStepResult {
	return stepworkflow.CrewStepResult{
		CrewRunID: status.RunID, SessionID: status.SessionID,
		Status: status.Status, FinalResponse: status.FinalResponse,
		Error: status.Error, StartedAt: status.StartedAt, CompletedAt: status.CompletedAt,
		Usage: status.Usage,
	}
}

// appendCrewPollTransition records a newly observed Crew run status. Only
// changes extend the timeline, so a long steady poll adds no noise.
func appendCrewPollTransition(timeline []stepworkflow.CrewPollTransition, status string) []stepworkflow.CrewPollTransition {
	status = strings.TrimSpace(status)
	if status == "" {
		return timeline
	}
	if len(timeline) > 0 && timeline[len(timeline)-1].Status == status {
		return timeline
	}
	return append(timeline, stepworkflow.CrewPollTransition{Status: status, At: time.Now().UTC()})
}

// checkInternalCrewAccess verifies that a workflow run may invoke a Crew
// trigger, using the same permission check as trigger invocation. Runs call
// it before the first step executes and crew steps call it before each
// invocation, so revoked access fails fast.
func (s *ProductScheduleService) checkInternalCrewAccess(ctx context.Context, userID, profileID, projectID, triggerID string, caller triggerCaller) error {
	_, _, _, trigger, err := s.findInternalProductTrigger(ctx, userID, profileID, projectID, triggerID)
	if err != nil {
		return err
	}
	if !trigger.Caller.matchesPresented(triggerCallerWorkflow, caller) {
		return ErrInternalCallerMismatch
	}
	return nil
}

// preflightCrewSteps verifies Crew access for every crew step in a plan
// before the run starts. Plans without crew steps skip the manifest read
// and cost nothing beyond the plan read the run performs anyway.
func preflightCrewSteps(ctx context.Context, crews *ProductScheduleService, userID, workspacePath string) error {
	if crews == nil {
		return nil
	}
	plan, err := readPlanFromWorkspace(ctx, workspacePath)
	if err != nil || plan == nil {
		return nil
	}
	seenCrew := false
	for _, step := range plan.Steps {
		if _, ok := step.(*stepworkflow.CrewPlanStep); ok {
			seenCrew = true
			break
		}
	}
	if !seenCrew {
		return nil
	}
	manifest, exists, err := ReadWorkflowManifest(ctx, workspacePath)
	if err != nil || !exists || manifest == nil {
		return fmt.Errorf("cannot verify crew access: workflow manifest is unavailable")
	}
	for _, step := range plan.Steps {
		crew, ok := step.(*stepworkflow.CrewPlanStep)
		if !ok {
			continue
		}
		if !crewProjectAttached(manifest, crew.CrewProfileID, crew.CrewProjectID) {
			return fmt.Errorf("crew step %q cannot run: crew project %q is not attached to this workflow; attach it read-only with manage_crew_attachment before running", crew.GetID(), crew.CrewProjectID)
		}
		caller := triggerCaller{Type: triggerCallerWorkflow, ID: manifest.ID}
		if err := crews.checkInternalCrewAccess(ctx, userID, crew.CrewProfileID, crew.CrewProjectID, crew.TriggerID, caller); err != nil {
			return fmt.Errorf("crew step %q cannot run: crew access check failed: %w", crew.GetID(), err)
		}
	}
	return nil
}

// crewProjectAttached reports whether a crew project is attached to the
// workflow under any alias.
func crewProjectAttached(manifest *WorkflowManifest, profileID, projectID string) bool {
	if manifest == nil {
		return false
	}
	profileID = normalizeInternalProfileID(profileID)
	for _, attachment := range manifest.CrewAttachments {
		if strings.TrimSpace(attachment.CrewProfileID) == profileID && strings.TrimSpace(attachment.CrewProjectID) == strings.TrimSpace(projectID) {
			return true
		}
	}
	return false
}
