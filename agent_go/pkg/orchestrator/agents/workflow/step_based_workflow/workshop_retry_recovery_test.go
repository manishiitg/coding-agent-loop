package step_based_workflow

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestWorkshopRetryRecoveryRequiresCompleteSameExecution(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name               string
		indices            []int
		total              int
		revision, end, id  string
		stale, persistence bool
		want               string
	}{
		{"complete", []int{0, 1}, 2, "rev", "rev", "run", false, false, "completed"},
		{"missing", []int{1}, 2, "rev", "rev", "run", false, false, "failed"},
		{"duplicates", []int{1, 1}, 2, "rev", "rev", "run", false, false, "failed"},
		{"invalid-index", []int{0, 2}, 2, "rev", "rev", "run", false, false, "failed"},
		{"changed-plan", []int{0, 1}, 2, "new", "new", "run", false, false, "failed"},
		{"mid-retry-edit", []int{0, 1}, 2, "rev", "new", "run", false, false, "failed"},
		{"missing-revision", []int{0, 1}, 2, "", "", "run", false, false, "failed"},
		{"stale-progress", []int{0, 1}, 2, "rev", "rev", "run", true, false, "failed"},
		{"persistence-error", []int{0, 1}, 2, "rev", "rev", "run", false, true, "failed"},
		{"reused-slot", []int{0, 1}, 2, "rev", "rev", "other", false, false, "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			meta := map[string]interface{}{"execution_id": "run", "status": "failed", "plan_revision": "rev", "started_at": formatRFC3339UTC(now.Add(-time.Hour)), "completed_at": formatRFC3339UTC(now.Add(-time.Minute)), "duration_ms": int64(100)}
			if tc.persistence {
				meta["persistence_errors"] = []interface{}{"disk failure"}
			}
			updated := now.Add(time.Second)
			if tc.stale {
				updated = now.Add(-time.Second)
			}
			required := []interface{}{}
			proofs := map[string]interface{}{}
			for _, index := range tc.indices {
				id := fmt.Sprintf("step%d", index)
				required = append(required, id)
				proofs[id] = map[string]interface{}{"execution_id": "run", "plan_revision": tc.revision, "completed_at": formatRFC3339UTC(updated)}
			}
			meta["required_step_ids"], meta["completion_receipts"] = required, proofs
			changed := applyWorkshopRetryRecovery(meta, tc.id, tc.revision, tc.end, "step1", tc.total, now, now.Add(2*time.Second))
			if meta["status"] != tc.want {
				t.Fatalf("status=%v want=%s", meta["status"], tc.want)
			}
			if tc.id != "run" {
				if changed || meta["recovery"] != nil {
					t.Fatal("modified another execution")
				}
				return
			}
			if !changed {
				t.Fatal("missing recovery evidence")
			}
			history := meta["recovery_history"].([]interface{})
			receipt := history[0].(map[string]interface{})
			if receipt["prior_status"] != "failed" || receipt["prior_completed_at"] != formatRFC3339UTC(now.Add(-time.Minute)) {
				t.Fatal("lost original failure")
			}
		})
	}
}

func TestWorkshopRetryRecoveryReceiptsRoundTrip(t *testing.T) {
	const folder = "iteration-2/default"
	const path = "Workflow/instagram/runs/" + folder + "/run_metadata.json"
	files := map[string]string{
		path:                                    `{"execution_id":"run","status":"failed","plan_revision":"rev","required_step_ids":["step"],"completed_at":"2026-01-01T00:00:00Z"}`,
		"Workflow/instagram/planning/plan.json": snapshotTestPlan(t, "step", "work"),
	}
	c := &StepBasedWorkflowOrchestrator{BaseOrchestrator: newFakeWorkspaceAPIWithContent(t, files), selectedRunFolder: folder}
	plan, err := c.ReadCurrentPlan(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC()
	c.finishCompletionReceipts(t.Context(), folder, "run", "rev", plan.Steps, &StepProgress{CompletedStepIndices: []int{0}})
	var meta map[string]interface{}
	if err := json.Unmarshal([]byte(files[path]), &meta); err != nil {
		t.Fatal(err)
	}
	if !applyWorkshopRetryRecovery(meta, "run", "rev", "rev", "step", 1, start, time.Now().UTC()) || meta["status"] != "completed" {
		t.Fatalf("recovery failed: %#v", meta)
	}
	before := files[path]
	if err := c.updateCompletionMetadata(t.Context(), folder, "different-run", func(m map[string]interface{}) { m["status"] = "completed" }); err == nil {
		t.Fatal("accepted wrong execution")
	}
	if files[path] != before {
		t.Fatal("overwrote another execution")
	}
	files[path] = "invalid-json"
	if err := c.updateCompletionMetadata(t.Context(), folder, "run", func(map[string]interface{}) {}); err == nil || files[path] != "invalid-json" {
		t.Fatal("recreated corrupt metadata")
	}
}

func TestWorkshopRetryRecoveryRejectsMissingOrForeignProof(t *testing.T) {
	for _, kind := range []string{"missing", "wrong-execution", "wrong-revision"} {
		t.Run(kind, func(t *testing.T) {
			now := time.Now().UTC()
			proof := map[string]interface{}{"execution_id": "run", "plan_revision": "rev", "completed_at": formatRFC3339UTC(now)}
			if kind == "wrong-execution" {
				proof["execution_id"] = "other"
			}
			if kind == "wrong-revision" {
				proof["plan_revision"] = "other"
			}
			proofs := map[string]interface{}{"step": proof}
			if kind == "missing" {
				delete(proofs, "step")
			}
			meta := map[string]interface{}{"execution_id": "run", "status": "failed", "plan_revision": "rev", "completed_at": formatRFC3339UTC(now.Add(-time.Hour)), "required_step_ids": []interface{}{"step"}, "completion_receipts": proofs}
			applyWorkshopRetryRecovery(meta, "run", "rev", "rev", "step", 1, now, now.Add(time.Second))
			if meta["status"] != "failed" {
				t.Fatal("unproven run marked complete")
			}
		})
	}
}

func TestWorkshopRetryRecoveryDoesNotRewriteTerminalOrNewFailure(t *testing.T) {
	now := time.Now().UTC()
	for _, status := range []string{"completed", "running", "failed"} {
		meta := map[string]interface{}{"execution_id": "run", "status": status, "completed_at": formatRFC3339UTC(now.Add(time.Second))}
		if applyWorkshopRetryRecovery(meta, "run", "r", "r", "s", 1, now, now) {
			t.Fatalf("rewrote %s/new failure", status)
		}
	}
}
