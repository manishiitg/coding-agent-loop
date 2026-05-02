package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path"
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

// MetricsHistoryResponse is the JSON shape of GET /api/workflow/metrics-history.
// Rows are the raw appended records from db/metrics_history.jsonl, sorted by
// completed_at ascending so the frontend can render them as a time series
// without re-sorting.
type MetricsHistoryResponse struct {
	Success bool                `json:"success"`
	Rows    []MetricSnapshotRow `json:"rows"`
	Error   string              `json:"error,omitempty"`
}

// handleGetMetricsHistory returns the per-run metric snapshots produced by
// the post-run snapshot hook (see metrics_snapshot.go). One row per metric per
// run. Empty array (not error) when the file does not yet exist — common for
// workflows whose first run hasn't completed since the hook shipped.
func (api *StreamingAPI) handleGetMetricsHistory(w http.ResponseWriter, r *http.Request) {
	if !setupCORS(w, r, http.MethodGet) {
		return
	}
	workspacePath, ok := requireWorkspacePath(w, r)
	if !ok {
		return
	}
	historyPath := path.Join(strings.Trim(workspacePath, "/"), "db", "metrics_history.jsonl")
	rows, err := readJSONLRecords[MetricSnapshotRow](r.Context(), historyPath)
	if err != nil {
		writeAIJSON(w, MetricsHistoryResponse{Success: false, Rows: []MetricSnapshotRow{}, Error: err.Error()})
		return
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].CompletedAt < rows[j].CompletedAt
	})
	writeAIJSON(w, MetricsHistoryResponse{Success: true, Rows: rows})
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

// FrameworkHealthResponse is the JSON shape of GET /api/workflow/framework-health.
// One stop shop for "is the framework wired correctly?": soul preconditions,
// success-criteria coverage by metrics, and the unanchored-metric set.
type FrameworkHealthResponse struct {
	Success           bool     `json:"success"`
	SoulExists        bool     `json:"soul_exists"`
	ObjectiveOK       bool     `json:"objective_ok"`
	SuccessCriteriaOK bool     `json:"success_criteria_ok"`
	Objective         string   `json:"objective,omitempty"`
	SuccessCriteria   string   `json:"success_criteria,omitempty"`
	UnanchoredMetrics []string `json:"unanchored_metrics"` // metric ids with empty linked_success_criteria (excluding telemetry SLOs)
	TelemetryMetrics  []string `json:"telemetry_metrics"`  // unanchored metrics that are explicit telemetry SLOs (cost / duration), surfaced separately
	Error             string   `json:"error,omitempty"`
}

// handleGetFrameworkHealth surfaces the cross-check between soul.md and
// metrics.json so the popup can warn the operator about coverage gaps and
// unanchored metrics in one place.
func (api *StreamingAPI) handleGetFrameworkHealth(w http.ResponseWriter, r *http.Request) {
	if !setupCORS(w, r, http.MethodGet) {
		return
	}
	workspacePath, ok := requireWorkspacePath(w, r)
	if !ok {
		return
	}
	pre, err := ReadSoulPreconditions(r.Context(), workspacePath)
	if err != nil {
		writeAIJSON(w, FrameworkHealthResponse{Success: false, Error: err.Error()})
		return
	}
	resp := FrameworkHealthResponse{
		Success:           true,
		SoulExists:        pre.SoulExists,
		ObjectiveOK:       pre.ObjectiveOK,
		SuccessCriteriaOK: pre.SuccessCriteriaOK,
		Objective:         pre.Objective,
		SuccessCriteria:   pre.SuccessCriteria,
		UnanchoredMetrics: []string{},
		TelemetryMetrics:  []string{},
	}

	metricsFile, exists, err := ReadMetricsFile(r.Context(), workspacePath)
	if err != nil {
		writeAIJSON(w, FrameworkHealthResponse{Success: false, Error: err.Error()})
		return
	}
	if !exists || metricsFile == nil {
		writeAIJSON(w, resp)
		return
	}

	// Surface metrics whose authors didn't link them to a success criterion.
	// The criterion-coverage check (parsing soul.md and string-matching) was
	// removed — the markdown-vs-canonical-label mismatch produced more false
	// positives than signal. Reading metrics + criteria side-by-side is the
	// human-judgment way; the framework no longer tries to enforce it.
	for _, m := range metricsFile.Metrics {
		if len(m.LinkedSuccessCriteria) == 0 {
			if isTelemetryMetric(m) {
				resp.TelemetryMetrics = append(resp.TelemetryMetrics, m.ID)
			} else {
				resp.UnanchoredMetrics = append(resp.UnanchoredMetrics, m.ID)
			}
		}
	}
	writeAIJSON(w, resp)
}

// isTelemetryMetric returns true for the universal telemetry SLOs
// (cost_per_run, run_duration_seconds) and any metric sourced from
// telemetry. These are unanchored by design — surfaced separately from
// "user forgot to link" unanchored metrics so the operator can read the
// distinction at a glance.
func isTelemetryMetric(m Metric) bool {
	if m.Source.Type == MetricSourceTelemetry {
		return true
	}
	switch m.ID {
	case "cost_per_run", "run_duration_seconds":
		return true
	}
	return false
}

// BuilderDocResponse is the JSON shape of GET /api/workflow/builder-doc.
// It returns the markdown content (or empty if the file does not exist yet).
type BuilderDocResponse struct {
	Success bool   `json:"success"`
	Doc     string `json:"doc"`     // "improve" | "review" — echoed back
	Path    string `json:"path"`    // workspace-relative path that was read
	Exists  bool   `json:"exists"`  // false if the file does not exist yet
	Content string `json:"content"` // markdown body, "" when !exists
	Error   string `json:"error,omitempty"`
}

// handleGetBuilderDoc serves the contents of builder/improve.md or
// builder/review.md so the AutoImprovementPopup can render them inline.
// The "doc" query param picks which file. Read-only.
func (api *StreamingAPI) handleGetBuilderDoc(w http.ResponseWriter, r *http.Request) {
	if !setupCORS(w, r, http.MethodGet) {
		return
	}
	workspacePath, ok := requireWorkspacePath(w, r)
	if !ok {
		return
	}
	doc := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("doc")))
	var fileName string
	switch doc {
	case "improve":
		fileName = "improve.md"
	case "review":
		fileName = "review.md"
	default:
		http.Error(w, "doc must be one of: improve, review", http.StatusBadRequest)
		return
	}
	rel := path.Join("builder", fileName)
	full := path.Join(strings.Trim(workspacePath, "/"), rel)
	content, exists, err := readFileFromWorkspace(r.Context(), full)
	if err != nil {
		writeAIJSON(w, BuilderDocResponse{Success: false, Doc: doc, Path: rel, Error: err.Error()})
		return
	}
	if !exists {
		writeAIJSON(w, BuilderDocResponse{Success: true, Doc: doc, Path: rel, Exists: false, Content: ""})
		return
	}
	writeAIJSON(w, BuilderDocResponse{Success: true, Doc: doc, Path: rel, Exists: true, Content: content})
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
