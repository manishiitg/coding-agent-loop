package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sync"
	"time"
)

var runMetadataLocks [64]sync.Mutex

func lockRunMetadata(workspace, folder string) func() {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(workspace + "\x00" + folder))
	mu := &runMetadataLocks[hash.Sum64()%uint64(len(runMetadataLocks))]
	mu.Lock()
	return mu.Unlock
}

// applyWorkshopRetryRecovery does not infer whole-run success from a successful
// tool call. It requires an unchanged execution contract and complete durable
// completion receipts. Otherwise it records recovery as unverified.
func applyWorkshopRetryRecovery(meta map[string]interface{}, executionID, revision, endRevision, stepID string, total int, retryStarted, completed time.Time) bool {
	if executionID == "" || meta["execution_id"] != executionID || meta["status"] != "failed" {
		return false
	}
	failureAt, _ := time.Parse(time.RFC3339Nano, fmt.Sprint(meta["completed_at"]))
	if failureAt.IsZero() || failureAt.After(retryStarted) {
		return false
	}
	reason := "required workflow completion is not verified"
	complete := revision != "" && revision == endRevision && meta["plan_revision"] == revision
	if !complete {
		reason = "execution contract changed or could not be verified; resume the full workflow to verify recovery"
	}
	required, _ := meta["required_step_ids"].([]interface{})
	receipts, _ := meta["completion_receipts"].(map[string]interface{})
	seen := make(map[string]bool)
	for _, raw := range required {
		id, ok := raw.(string)
		if !ok || id == "" || seen[id] {
			complete = false
		}
		seen[id] = true
		proof, _ := receipts[id].(map[string]interface{})
		stamp, err := time.Parse(time.RFC3339Nano, fmt.Sprint(proof["completed_at"]))
		if proof["execution_id"] != executionID || proof["plan_revision"] != revision || err != nil || stamp.After(completed) || (id == stepID && stamp.Before(retryStarted)) {
			complete = false
		}
	}
	if total <= 0 || len(required) != total || len(seen) != total || !seen[stepID] {
		complete = false
	}
	if failures, ok := meta["persistence_errors"].([]interface{}); ok && len(failures) > 0 {
		complete = false
		reason = "run has unresolved persistence errors"
	}
	state := "step_recovered_run_unverified"
	if complete {
		state = "completed"
		reason = "all required steps have durable completion under the original execution contract"
	}
	receipt := map[string]interface{}{"execution_id": executionID, "step_id": stepID, "status": state, "reason": reason, "retry_started_at": formatRFC3339UTC(retryStarted), "completed_at": formatRFC3339UTC(completed), "retry_plan_revision": revision, "prior_status": meta["status"], "prior_completed_at": meta["completed_at"], "prior_duration_ms": meta["duration_ms"]}
	history, _ := meta["recovery_history"].([]interface{})
	meta["recovery_history"] = append(history, receipt)
	meta["recovery"] = receipt
	if complete {
		meta["status"] = "completed"
		meta["completed_at"] = formatRFC3339UTC(completed)
		if started, err := time.Parse(time.RFC3339Nano, fmt.Sprint(meta["started_at"])); err == nil {
			meta["duration_ms"] = completed.Sub(started).Milliseconds()
		}
	}
	return true
}

func (hcpo *StepBasedWorkflowOrchestrator) recordWorkshopRetryRecovery(ctx context.Context, folder, executionID, revision, stepID string, total int, retryStarted time.Time) error {
	if executionID == "" {
		return nil
	}
	// Compare against the current plan, not the immutable retry snapshot.
	currentPlan, err := hcpo.ReadCurrentPlan(ctx)
	endRevision := ""
	if err == nil {
		endRevision, err = hcpo.ensureExecutablePlanRevision(withExecutionPlan(ctx, currentPlan))
	}
	if err != nil {
		endRevision = ""
	}
	unlock := lockRunMetadata(hcpo.GetWorkspacePath(), folder)
	defer unlock()
	path := workflowRunMetadataPath(folder)
	raw, err := hcpo.ReadWorkspaceFile(ctx, path)
	if err != nil {
		return err
	}
	var meta map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return err
	}
	if !applyWorkshopRetryRecovery(meta, executionID, revision, endRevision, stepID, total, retryStarted, time.Now().UTC()) {
		return nil
	}
	encoded, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return hcpo.WriteWorkspaceFile(ctx, path, string(encoded))
}
