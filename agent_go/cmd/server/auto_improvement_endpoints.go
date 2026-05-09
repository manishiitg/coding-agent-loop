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
//   GET  /api/workflow/metrics?workspace_path=...
//
// All read-only.
// =====================================================================

// EvalTrajectoryPoint is one (run_folder, score) sample for a single eval step.
type EvalTrajectoryPoint struct {
	RunFolder    string  `json:"run_folder"`
	GeneratedAt  string  `json:"generated_at"`
	Score        int     `json:"score"`
	MaxScore     int     `json:"max_score"`
	ScorePercent float64 `json:"score_percent"`
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

// MetricsResponse is the JSON shape of GET /api/workflow/metrics.
type MetricsResponse struct {
	Success bool         `json:"success"`
	File    *MetricsFile `json:"file,omitempty"`
	Error   string       `json:"error,omitempty"`
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

// FrameworkHealthResponse is the JSON shape of GET /api/workflow/framework-health.
// One stop shop for "is the framework wired correctly?": soul preconditions,
// success-criteria coverage by metrics, and the unanchored-metric set.
type FrameworkHealthResponse struct {
	Success           bool   `json:"success"`
	SoulExists        bool   `json:"soul_exists"`
	ObjectiveOK       bool   `json:"objective_ok"`
	SuccessCriteriaOK bool   `json:"success_criteria_ok"`
	Objective         string `json:"objective,omitempty"`
	SuccessCriteria   string `json:"success_criteria,omitempty"`
	Error             string `json:"error,omitempty"`
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
	}
	writeAIJSON(w, resp)
}

// BuilderDocResponse is the JSON shape of GET /api/workflow/builder-doc.
// It returns the markdown content (or empty if the file does not exist yet).
type BuilderDocResponse struct {
	Success bool   `json:"success"`
	Doc     string `json:"doc"`     // "improve" | "review" | "soul" — echoed back
	Path    string `json:"path"`    // workspace-relative path that was read
	Exists  bool   `json:"exists"`  // false if the file does not exist yet
	Content string `json:"content"` // markdown body, "" when !exists
	Error   string `json:"error,omitempty"`
}

type BuilderDocArchiveFile struct {
	Path  string `json:"path"`
	Label string `json:"label"`
}

type BuilderDocArchivesResponse struct {
	Success bool                    `json:"success"`
	Files   []BuilderDocArchiveFile `json:"files"`
	Error   string                  `json:"error,omitempty"`
}

// handleGetBuilderDoc serves the contents of builder/improve.md,
// builder/review.md, or soul/soul.md so the AutoImprovementPopup can render
// them inline.
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
	requestedPath := strings.TrimSpace(r.URL.Query().Get("path"))
	var rel string
	switch doc {
	case "improve":
		rel = "builder/improve.md"
	case "review":
		rel = "builder/review.md"
	case "soul":
		rel = "soul/soul.md"
	default:
		http.Error(w, "doc must be one of: improve, review, soul", http.StatusBadRequest)
		return
	}
	if requestedPath != "" {
		if doc != "improve" && doc != "review" {
			http.Error(w, "path is only supported for improve/review archive files", http.StatusBadRequest)
			return
		}
		cleanPath := path.Clean(requestedPath)
		if !isBuilderDocArchivePath(doc, cleanPath) {
			http.Error(w, "path must be under the matching builder/*-archive/ folder and end with .md", http.StatusBadRequest)
			return
		}
		rel = cleanPath
	}
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

func (api *StreamingAPI) handleGetBuilderDocArchives(w http.ResponseWriter, r *http.Request) {
	if !setupCORS(w, r, http.MethodGet) {
		return
	}
	workspacePath, ok := requireWorkspacePath(w, r)
	if !ok {
		return
	}
	doc := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("doc")))
	if doc == "" {
		doc = "improve"
	}
	if doc != "improve" && doc != "review" {
		http.Error(w, "doc must be one of: improve, review", http.StatusBadRequest)
		return
	}
	folder := path.Join(strings.Trim(workspacePath, "/"), "builder", doc+"-archive")
	listing, exists, err := listWorkspaceFolder(r.Context(), folder, 2)
	if err != nil {
		writeAIJSON(w, BuilderDocArchivesResponse{Success: false, Files: []BuilderDocArchiveFile{}, Error: err.Error()})
		return
	}
	if !exists {
		writeAIJSON(w, BuilderDocArchivesResponse{Success: true, Files: []BuilderDocArchiveFile{}})
		return
	}
	var paths []string
	collectWorkspaceFilePaths(listing, &paths)
	files := make([]BuilderDocArchiveFile, 0, len(paths))
	workspacePrefix := strings.Trim(workspacePath, "/") + "/"
	for _, p := range paths {
		rel := strings.TrimPrefix(path.Clean(filepath.ToSlash(p)), workspacePrefix)
		if !isBuilderDocArchivePath(doc, rel) {
			continue
		}
		label := strings.TrimSuffix(path.Base(rel), ".md")
		files = append(files, BuilderDocArchiveFile{Path: rel, Label: label})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path > files[j].Path
	})
	writeAIJSON(w, BuilderDocArchivesResponse{Success: true, Files: files})
}

func isBuilderDocArchivePath(doc, rel string) bool {
	rel = path.Clean(strings.TrimSpace(rel))
	return (doc == "improve" || doc == "review") &&
		strings.HasPrefix(rel, "builder/"+doc+"-archive/") &&
		strings.HasSuffix(rel, ".md") &&
		!strings.Contains(rel, "..")
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
