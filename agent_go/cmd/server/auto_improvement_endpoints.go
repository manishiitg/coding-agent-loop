package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
)

// =====================================================================
// HTTP endpoints for the auto-improvement framework.
//
// Routes (registered in server.go alongside the other api/workflow/* routes):
//   GET  /api/workflow/eval-trajectory?workspace_path=...
//   GET  /api/workflow/decisions?workspace_path=...
//   GET  /api/workflow/metrics?workspace_path=...
//   GET  /api/workflow/experiments?workspace_path=...&include_history=true
//
// All read-only; mutating endpoints land later (Phase 5 user-side actions).
// =====================================================================

// EvalTrajectoryPoint is one (run_folder, score) sample for a single eval step.
type EvalTrajectoryPoint struct {
	RunFolder      string  `json:"run_folder"`
	GeneratedAt    string  `json:"generated_at"`
	Score          int     `json:"score"`
	MaxScore       int     `json:"max_score"`
	ScorePercent   float64 `json:"score_percent"`
}

// EvalTrajectorySeries is one eval step's time series.
type EvalTrajectorySeries struct {
	StepID string                `json:"step_id"`
	Points []EvalTrajectoryPoint `json:"points"`
}

// EvalTrajectoryResponse is the JSON shape of GET /api/workflow/eval-trajectory.
type EvalTrajectoryResponse struct {
	Success bool                   `json:"success"`
	Series  []EvalTrajectorySeries `json:"series"`
	Error   string                 `json:"error,omitempty"`
}

// (api *StreamingAPI) handleGetEvalTrajectory streams a per-eval-step time
// series built from the existing /scores/evaluation/ daily files. Zero schema
// change; this is the cheapest first step of the framework.
func (api *StreamingAPI) handleGetEvalTrajectory(w http.ResponseWriter, r *http.Request) {
	if !setupCORS(w, r, http.MethodGet) {
		return
	}
	workspacePath, ok := requireWorkspacePath(w, r)
	if !ok {
		return
	}
	series, err := computeEvalTrajectory(r.Context(), workspacePath)
	if err != nil {
		writeAIJSON(w, EvalTrajectoryResponse{Success: false, Error: err.Error()})
		return
	}
	writeAIJSON(w, EvalTrajectoryResponse{Success: true, Series: series})
}

func computeEvalTrajectory(ctx context.Context, workspacePath string) ([]EvalTrajectorySeries, error) {
	reports, err := readAllEvaluationReportsFromScores(ctx, workspacePath)
	if err != nil {
		return nil, err
	}
	bySeries := make(map[string][]EvalTrajectoryPoint)
	for runFolder, report := range reports {
		for _, step := range report.StepScores {
			pct := 0.0
			if step.MaxScore > 0 {
				pct = (float64(step.Score) / float64(step.MaxScore)) * 100.0
			}
			bySeries[step.StepID] = append(bySeries[step.StepID], EvalTrajectoryPoint{
				RunFolder:    runFolder,
				GeneratedAt:  report.GeneratedAt,
				Score:        step.Score,
				MaxScore:     step.MaxScore,
				ScorePercent: pct,
			})
		}
	}
	out := make([]EvalTrajectorySeries, 0, len(bySeries))
	for stepID, points := range bySeries {
		sort.Slice(points, func(i, j int) bool {
			return points[i].GeneratedAt < points[j].GeneratedAt
		})
		out = append(out, EvalTrajectorySeries{StepID: stepID, Points: points})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StepID < out[j].StepID })
	return out, nil
}

// DecisionsFeedResponse is the JSON shape of GET /api/workflow/decisions.
type DecisionsFeedResponse struct {
	Success   bool            `json:"success"`
	Decisions []DecisionEntry `json:"decisions"`
	Error     string          `json:"error,omitempty"`
}

func (api *StreamingAPI) handleGetDecisionsFeed(w http.ResponseWriter, r *http.Request) {
	if !setupCORS(w, r, http.MethodGet) {
		return
	}
	workspacePath, ok := requireWorkspacePath(w, r)
	if !ok {
		return
	}
	entries, err := ReadDecisionEntries(r.Context(), workspacePath)
	if err != nil {
		writeAIJSON(w, DecisionsFeedResponse{Success: false, Error: err.Error()})
		return
	}
	writeAIJSON(w, DecisionsFeedResponse{Success: true, Decisions: entries})
}

// MetricsResponse is the JSON shape of GET /api/workflow/metrics.
type MetricsResponse struct {
	Success bool        `json:"success"`
	File    *MetricsFile `json:"file,omitempty"`
	Error   string      `json:"error,omitempty"`
}

func (api *StreamingAPI) handleGetMetrics(w http.ResponseWriter, r *http.Request) {
	if !setupCORS(w, r, http.MethodGet) {
		return
	}
	workspacePath, ok := requireWorkspacePath(w, r)
	if !ok {
		return
	}
	file, exists, err := ReadMetricsFile(r.Context(), workspacePath)
	if err != nil {
		writeAIJSON(w, MetricsResponse{Success: false, Error: err.Error()})
		return
	}
	if !exists {
		writeAIJSON(w, MetricsResponse{Success: true, File: &MetricsFile{Metrics: []Metric{}}})
		return
	}
	writeAIJSON(w, MetricsResponse{Success: true, File: file})
}

// ExperimentsResponse is the JSON shape of GET /api/workflow/experiments.
type ExperimentsResponse struct {
	Success  bool                `json:"success"`
	Active   []ExperimentRecord  `json:"active"`
	History  []ExperimentRecord  `json:"history,omitempty"`
	Error    string              `json:"error,omitempty"`
}

func (api *StreamingAPI) handleGetExperiments(w http.ResponseWriter, r *http.Request) {
	if !setupCORS(w, r, http.MethodGet) {
		return
	}
	workspacePath, ok := requireWorkspacePath(w, r)
	if !ok {
		return
	}
	includeHistory := r.URL.Query().Get("include_history") == "true"

	active, err := ReadActiveExperiments(r.Context(), workspacePath)
	if err != nil {
		writeAIJSON(w, ExperimentsResponse{Success: false, Error: err.Error()})
		return
	}
	resp := ExperimentsResponse{Success: true, Active: active}
	if includeHistory {
		hist, err := ReadHistoryExperiments(r.Context(), workspacePath)
		if err != nil {
			writeAIJSON(w, ExperimentsResponse{Success: false, Error: err.Error()})
			return
		}
		resp.History = hist
	}
	writeAIJSON(w, resp)
}

// --- Shared HTTP helpers ----------------------------------------------------

func setupCORS(w http.ResponseWriter, r *http.Request, method string) bool {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", method+", OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return false
	}
	if r.Method != method {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}

func requireWorkspacePath(w http.ResponseWriter, r *http.Request) (string, bool) {
	workspacePath := r.URL.Query().Get("workspace_path")
	if workspacePath == "" {
		http.Error(w, "workspace_path parameter is required", http.StatusBadRequest)
		return "", false
	}
	cleaned := filepath.Clean(workspacePath)
	if strings.Contains(cleaned, "..") {
		http.Error(w, "Invalid workspace path", http.StatusBadRequest)
		return "", false
	}
	return cleaned, true
}

func writeAIJSON(w http.ResponseWriter, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}
