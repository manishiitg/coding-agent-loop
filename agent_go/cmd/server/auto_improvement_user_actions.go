package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// =====================================================================
// auto_improvement_user_actions.go — user-side mutation endpoints.
// All POST. Each one writes a decisions.jsonl entry with source=user so
// the audit trail of human steering is preserved.
// =====================================================================

type abortExperimentReq struct {
	WorkspacePath string `json:"workspace_path"`
	ExperimentID  string `json:"experiment_id"`
	Reason        string `json:"reason"`
	ActorUser     string `json:"actor_user,omitempty"`
}

func (api *StreamingAPI) handleAbortExperiment(w http.ResponseWriter, r *http.Request) {
	if !setupCORS(w, r, http.MethodPost) {
		return
	}
	var req abortExperimentReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.WorkspacePath == "" || req.ExperimentID == "" {
		http.Error(w, "workspace_path and experiment_id are required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		http.Error(w, "reason is required", http.StatusBadRequest)
		return
	}
	ctx := r.Context()

	// Apply revert and remove from active.
	if err := ApplyRevertByExperimentID(ctx, req.WorkspacePath, req.ExperimentID); err != nil {
		writeAIJSON(w, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	// Move to history with status=aborted.
	file, _, _ := ReadActiveFile(ctx, req.WorkspacePath)
	if rec := FindActiveExperiment(file, req.ExperimentID); rec != nil {
		rec.Status = ExpStatusAborted
		rec.ConcludedAt = nowUTC()
		_ = AppendHistoryRecord(ctx, req.WorkspacePath, *rec)
	}
	_ = RemoveActiveExperiment(ctx, req.WorkspacePath, req.ExperimentID)

	dec := DecisionEntry{
		Source:             DecisionSourceUser,
		Trigger:            "override:abort_experiment",
		Rationale:          fmt.Sprintf("aborted experiment %s: %s", req.ExperimentID, req.Reason),
		AppliedChanges:     []string{},
		LinkedExperimentID: req.ExperimentID,
	}
	if req.ActorUser != "" {
		dec.EditedBy = req.ActorUser
	}
	_, err := AppendDecisionEntry(ctx, req.WorkspacePath, dec)
	if err != nil {
		writeAIJSON(w, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	writeAIJSON(w, map[string]interface{}{"success": true})
}

type extendExperimentReq struct {
	WorkspacePath  string `json:"workspace_path"`
	ExperimentID   string `json:"experiment_id"`
	AdditionalRuns int    `json:"additional_runs"`
	Reason         string `json:"reason"`
	ActorUser      string `json:"actor_user,omitempty"`
}

func (api *StreamingAPI) handleExtendExperiment(w http.ResponseWriter, r *http.Request) {
	if !setupCORS(w, r, http.MethodPost) {
		return
	}
	var req extendExperimentReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.WorkspacePath == "" || req.ExperimentID == "" {
		http.Error(w, "workspace_path and experiment_id are required", http.StatusBadRequest)
		return
	}
	if req.AdditionalRuns <= 0 {
		http.Error(w, "additional_runs must be > 0", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	file, _, err := ReadActiveFile(ctx, req.WorkspacePath)
	if err != nil {
		writeAIJSON(w, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	rec := FindActiveExperiment(file, req.ExperimentID)
	if rec == nil {
		http.Error(w, "experiment not found", http.StatusNotFound)
		return
	}
	rec.Measurement.TargetRuns += req.AdditionalRuns
	if rec.Status == ExpStatusEvaluating {
		// Reset to measuring; the next run-completion will recompute.
		rec.Status = ExpStatusMeasuring
		rec.Conclusion = nil
	}
	if err := UpsertActiveExperiment(ctx, req.WorkspacePath, *rec); err != nil {
		writeAIJSON(w, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	dec := DecisionEntry{
		Source:             DecisionSourceUser,
		Trigger:            "override:extend_experiment",
		Rationale:          fmt.Sprintf("extended experiment %s by %d runs: %s", req.ExperimentID, req.AdditionalRuns, req.Reason),
		AppliedChanges:     []string{},
		LinkedExperimentID: req.ExperimentID,
	}
	if req.ActorUser != "" {
		dec.EditedBy = req.ActorUser
	}
	_, _ = AppendDecisionEntry(ctx, req.WorkspacePath, dec)
	writeAIJSON(w, map[string]interface{}{"success": true})
}

type manualConcludeReq struct {
	WorkspacePath string  `json:"workspace_path"`
	ExperimentID  string  `json:"experiment_id"`
	Verdict       Verdict `json:"verdict"`
	Reason        string  `json:"reason"`
	Rationale     string  `json:"rationale"`
	ActorUser     string  `json:"actor_user,omitempty"`
}

func (api *StreamingAPI) handleManualConcludeExperiment(w http.ResponseWriter, r *http.Request) {
	if !setupCORS(w, r, http.MethodPost) {
		return
	}
	var req manualConcludeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.WorkspacePath == "" || req.ExperimentID == "" {
		http.Error(w, "workspace_path and experiment_id are required", http.StatusBadRequest)
		return
	}
	rationale := strings.TrimSpace(req.Rationale)
	if rationale == "" {
		rationale = strings.TrimSpace(req.Reason)
	}
	if rationale == "" {
		rationale = "user manual conclusion"
	}

	in := ConcludeExperimentInput{
		ExperimentID: req.ExperimentID,
		Rationale:    rationale,
		OverrideVerdict: &VerdictOverride{
			To:     req.Verdict,
			Reason: req.Reason,
		},
	}
	out, err := ConcludeExperiment(r.Context(), req.WorkspacePath, "user:"+req.ActorUser, in)
	if err != nil {
		writeAIJSON(w, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	writeAIJSON(w, map[string]interface{}{"success": true, "final_verdict": out.FinalVerdict, "archived": out.Archived})
}

type approveExperimentReq struct {
	WorkspacePath string `json:"workspace_path"`
	ExperimentID  string `json:"experiment_id"`
	Gate          string `json:"gate"` // "hypothesis" | "conclusion"
	ActorUser     string `json:"actor_user,omitempty"`
}

func (api *StreamingAPI) handleApproveExperiment(w http.ResponseWriter, r *http.Request) {
	if !setupCORS(w, r, http.MethodPost) {
		return
	}
	var req approveExperimentReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.WorkspacePath == "" || req.ExperimentID == "" || req.Gate == "" {
		http.Error(w, "workspace_path, experiment_id, gate are required", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	file, _, err := ReadActiveFile(ctx, req.WorkspacePath)
	if err != nil {
		writeAIJSON(w, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	rec := FindActiveExperiment(file, req.ExperimentID)
	if rec == nil {
		http.Error(w, "experiment not found", http.StatusNotFound)
		return
	}
	now := nowUTC()
	switch req.Gate {
	case "hypothesis":
		if rec.Status != ExpStatusAwaitingApproval {
			writeAIJSON(w, map[string]interface{}{"success": false, "error": "experiment is not awaiting hypothesis approval"})
			return
		}
		rec.Approvals.HypothesisApprovedBy = req.ActorUser
		rec.Approvals.HypothesisApprovedAt = now
		rec.Status = ExpStatusMeasuring
		if err := UpsertActiveExperiment(ctx, req.WorkspacePath, *rec); err != nil {
			writeAIJSON(w, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
	case "conclusion":
		if rec.Status != ExpStatusAwaitingConclusionApproval {
			writeAIJSON(w, map[string]interface{}{"success": false, "error": "experiment is not awaiting conclusion approval"})
			return
		}
		rec.Approvals.ConclusionApprovedBy = req.ActorUser
		rec.Approvals.ConclusionApprovedAt = now
		// Now apply the conclusion (already populated by conclude_experiment).
		_, _, err := applyConclusion(ctx, req.WorkspacePath, rec)
		if err != nil {
			writeAIJSON(w, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
	default:
		writeAIJSON(w, map[string]interface{}{"success": false, "error": "gate must be \"hypothesis\" or \"conclusion\""})
		return
	}

	dec := DecisionEntry{
		Source:             DecisionSourceUser,
		Trigger:            "override:approve:" + req.Gate,
		Rationale:          fmt.Sprintf("approved %s gate for experiment %s", req.Gate, req.ExperimentID),
		AppliedChanges:     []string{},
		LinkedExperimentID: req.ExperimentID,
	}
	if req.ActorUser != "" {
		dec.EditedBy = req.ActorUser
	}
	_, _ = AppendDecisionEntry(ctx, req.WorkspacePath, dec)
	writeAIJSON(w, map[string]interface{}{"success": true})
}

type recordMeasurementReq struct {
	WorkspacePath string `json:"workspace_path"`
	RunFolder     string `json:"run_folder"`
}

// handleRecordMeasurement is the run-completion hook entry point. The eval
// pipeline (or scheduler / builder UI) calls this whenever a new eval report
// has been persisted for a run. The handler iterates active experiments,
// resolves each target metric for this run, appends to measurement.values,
// and triggers compute_verdict when target_runs is reached.
func (api *StreamingAPI) handleRecordMeasurement(w http.ResponseWriter, r *http.Request) {
	if !setupCORS(w, r, http.MethodPost) {
		return
	}
	var req recordMeasurementReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.WorkspacePath == "" || req.RunFolder == "" {
		http.Error(w, "workspace_path and run_folder are required", http.StatusBadRequest)
		return
	}
	if err := RecordMeasurement(r.Context(), req.WorkspacePath, req.RunFolder); err != nil {
		writeAIJSON(w, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	writeAIJSON(w, map[string]interface{}{"success": true})
}

type captureContextReq struct {
	WorkspacePath string   `json:"workspace_path"`
	Section       string   `json:"section,omitempty"`
	RuleText      string   `json:"rule_text"`
	TargetMetrics []string `json:"target_metrics"`
	ExampleNote   string   `json:"example_note,omitempty"`
}

func (api *StreamingAPI) handleCaptureContext(w http.ResponseWriter, r *http.Request) {
	if !setupCORS(w, r, http.MethodPost) {
		return
	}
	var req captureContextReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.WorkspacePath == "" {
		http.Error(w, "workspace_path is required", http.StatusBadRequest)
		return
	}
	dec, err := CaptureContext(r.Context(), req.WorkspacePath, req.Section, req.RuleText, req.TargetMetrics, req.ExampleNote)
	if err != nil {
		writeAIJSON(w, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	writeAIJSON(w, map[string]interface{}{
		"success":     true,
		"decision_id": dec.ID,
	})
}
