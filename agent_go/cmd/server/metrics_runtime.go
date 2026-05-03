package server

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
)

// =====================================================================
// metrics_runtime.go — read/validate/write <workflow>/planning/metrics.json and
// resolve metric values from their declared sources.
//
// Schemas: schemas/auto-improvement.schema.json#$defs/MetricsFile
// =====================================================================

var metricIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)

// workflowMetricsPath returns the canonical path to <workflow>/planning/metrics.json.
//
// Lives under planning/ so the existing FolderGuard BlockedWritePaths (which
// already covers planning/) makes it tool-only by construction: shell writes
// to planning/ are blocked at the kernel sandbox level; the privileged
// propose_metric tool path goes through the workspace API and bypasses the
// block. Same pattern that forces step_config.json edits through
// update_step_config rather than shell.
func workflowMetricsPath(workspacePath string) string {
	return path.Join(strings.Trim(workspacePath, "/"), "planning", "metrics.json")
}

// ReadMetricsFile loads <workflow>/planning/metrics.json. Returns (file, true, nil) when
// present. (nil, false, nil) when the file does not exist (no metrics yet).
func ReadMetricsFile(ctx context.Context, workspacePath string) (*MetricsFile, bool, error) {
	content, exists, err := readFileFromWorkspace(ctx, workflowMetricsPath(workspacePath))
	if err != nil {
		return nil, false, err
	}
	if !exists || strings.TrimSpace(content) == "" {
		return nil, false, nil
	}
	var file MetricsFile
	if err := json.Unmarshal([]byte(content), &file); err != nil {
		return nil, true, fmt.Errorf("parse metrics.json: %w", err)
	}
	return &file, true, nil
}

// WriteMetricsFile validates and persists <workflow>/planning/metrics.json atomically.
func WriteMetricsFile(ctx context.Context, workspacePath string, file *MetricsFile) error {
	if file == nil {
		return fmt.Errorf("metrics file is nil")
	}
	if err := ValidateMetricsFile(file); err != nil {
		return err
	}
	body, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metrics.json: %w", err)
	}
	return writeFileToWorkspace(ctx, workflowMetricsPath(workspacePath), string(body))
}

// ValidateMetricsFile checks structural integrity of metrics.json.
func ValidateMetricsFile(file *MetricsFile) error {
	if file == nil {
		return fmt.Errorf("file is nil")
	}
	seen := make(map[string]struct{}, len(file.Metrics))
	for i := range file.Metrics {
		if err := ValidateMetric(&file.Metrics[i]); err != nil {
			return fmt.Errorf("metrics[%d]: %w", i, err)
		}
		id := file.Metrics[i].ID
		if _, dup := seen[id]; dup {
			return fmt.Errorf("metrics: duplicate id %q", id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

// ValidateMetric validates a single metric definition.
func ValidateMetric(m *Metric) error {
	if m == nil {
		return fmt.Errorf("metric is nil")
	}
	if !metricIDPattern.MatchString(m.ID) {
		return fmt.Errorf("id %q does not match kebab.dot pattern", m.ID)
	}
	if strings.TrimSpace(m.Unit) == "" {
		return fmt.Errorf("unit is required")
	}
	switch m.Direction {
	case HigherBetter, LowerBetter:
	default:
		return fmt.Errorf("invalid direction %q", m.Direction)
	}
	switch m.Mode {
	case MetricModeTarget:
		if m.Target == nil {
			return fmt.Errorf("mode=target requires target")
		}
	case MetricModeSLO:
		if m.Direction == HigherBetter && m.Floor == nil {
			return fmt.Errorf("mode=slo with higher_better requires floor")
		}
		if m.Direction == LowerBetter && m.Ceiling == nil {
			return fmt.Errorf("mode=slo with lower_better requires ceiling")
		}
	default:
		return fmt.Errorf("invalid mode %q", m.Mode)
	}
	if err := validateMetricSource(&m.Source); err != nil {
		return fmt.Errorf("source: %w", err)
	}
	return nil
}

// validSourceTypesHint is the canonical list returned in error messages so
// agents don't have to brute-force the enum by trial-and-error.
const validSourceTypesHint = `valid source.type values: "eval_step" (requires id), "telemetry" (requires field). For external feeds, schema checks, lineage, or delayed outcomes, write a Python eval step and use source.type=eval_step.`

func validateMetricSource(s *MetricSource) error {
	switch s.Type {
	case MetricSourceEvalStep:
		if strings.TrimSpace(s.ID) == "" {
			return fmt.Errorf("source.type=eval_step requires id (the eval step id from evaluation/evaluation_plan.json). %s", validSourceTypesHint)
		}
	case MetricSourceTelemetry:
		if strings.TrimSpace(s.Field) == "" {
			return fmt.Errorf("source.type=telemetry requires field (dotted path, e.g. run.total_cost_usd). %s", validSourceTypesHint)
		}
	default:
		return fmt.Errorf("invalid source.type %q. %s", s.Type, validSourceTypesHint)
	}
	return nil
}

// FindMetric returns the metric with the given id, or nil.
func FindMetric(file *MetricsFile, id string) *Metric {
	if file == nil {
		return nil
	}
	for i := range file.Metrics {
		if file.Metrics[i].ID == id {
			return &file.Metrics[i]
		}
	}
	return nil
}

// ResolveMetricValue extracts the metric's value for a given run from its
// declared source. Returns (value, true) when a value is available now,
// (0, false) when the value is not yet available (e.g., delayed ground truth
// or external feed has not delivered).
//
// Sources implemented in Phase 2: eval_step, telemetry. Others land later.
func ResolveMetricValue(ctx context.Context, workspacePath, runFolder string, m *Metric) (float64, bool, error) {
	if m == nil {
		return 0, false, fmt.Errorf("metric is nil")
	}
	switch m.Source.Type {
	case MetricSourceEvalStep:
		return resolveFromEvalStep(ctx, workspacePath, runFolder, m.Source.ID, m.Source.Field)
	case MetricSourceTelemetry:
		return resolveFromTelemetry(ctx, workspacePath, runFolder, m.Source.Field)
	default:
		return 0, false, fmt.Errorf("unsupported metric source type %q", m.Source.Type)
	}
}

// resolveFromEvalStep reads a value from the named eval step. Two modes:
//
//	field == ""  → returns the eval step's percent score (Score/MaxScore*100).
//	               Used when one eval step → one metric, no structured output.
//
//	field != ""  → looks up the field key in the eval step's structured JSON
//	               output (OutputContent.Content) and returns the numeric
//	               value. Used when one eval step's code emits an object with
//	               many named fields and many metrics each pull one field.
//
// Both modes read from the same per-run evaluation report already persisted
// to /scores/evaluation/<group>/<date>.json by the eval pipeline.
func resolveFromEvalStep(ctx context.Context, workspacePath, runFolder, stepID, field string) (float64, bool, error) {
	reports, err := readAllEvaluationReportsFromScores(ctx, workspacePath)
	if err != nil {
		return 0, false, err
	}
	report, ok := reports[runFolder]
	if !ok {
		return 0, false, nil
	}
	for _, step := range report.StepScores {
		if step.StepID != stepID {
			continue
		}

		// Top-level shortcuts so a metric can target step.Score / step.MaxScore
		// directly without requiring the eval to emit structured output.
		switch strings.TrimSpace(field) {
		case "":
			if step.MaxScore <= 0 {
				return float64(step.Score), true, nil
			}
			return (float64(step.Score) / float64(step.MaxScore)) * 100.0, true, nil
		case "score":
			return float64(step.Score), true, nil
		case "max_score":
			return float64(step.MaxScore), true, nil
		}

		// Otherwise: field-keyed lookup into the eval step's structured JSON output.
		if step.OutputContent == nil {
			return 0, false, fmt.Errorf("eval step %q produced no OutputContent; cannot read field %q (set field=\"score\" for the raw score, or drop the field for the percent score)", stepID, field)
		}
		if !step.OutputContent.IsJSON {
			return 0, false, fmt.Errorf("eval step %q output is not JSON; cannot read field %q", stepID, field)
		}
		obj, ok := step.OutputContent.Content.(map[string]interface{})
		if !ok {
			return 0, false, fmt.Errorf("eval step %q output is %T, not an object; cannot read field %q", stepID, step.OutputContent.Content, field)
		}
		raw, present := obj[field]
		if !present {
			return 0, false, fmt.Errorf("field %q not present in eval step %q output (keys: %v)", field, stepID, mapKeys(obj))
		}
		return coerceToFloat(raw, field)
	}
	return 0, false, nil
}

// coerceToFloat converts a JSON-decoded value to float64. Handles the four
// common cases: float64 (JSON numbers always decode this way in Go),
// bool (true=1, false=0), string (parse), and nil (missing → not-available).
func coerceToFloat(raw interface{}, field string) (float64, bool, error) {
	switch v := raw.(type) {
	case float64:
		return v, true, nil
	case bool:
		if v {
			return 1, true, nil
		}
		return 0, true, nil
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return 0, false, nil
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, false, fmt.Errorf("field %q is non-numeric string %q", field, v)
		}
		return f, true, nil
	case nil:
		return 0, false, nil
	default:
		return 0, false, fmt.Errorf("field %q is type %T, not numeric", field, raw)
	}
}

func mapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// resolveFromTelemetry reads the named field out of this run's telemetry
// payload. Six fields are wired:
//
//	run.total_cost_usd   — execution-scope cost (workflow steps).
//	run.duration_seconds — execution-scope wall-clock seconds.
//	eval.total_cost_usd  — evaluation-scope cost (eval scoring + eval Python).
//	eval.duration_seconds — evaluation-scope wall-clock seconds.
//	total.cost_usd       — execution + evaluation cost combined.
//	total.duration_seconds — execution + evaluation duration combined
//	                         (per-scope duration summed; eval runs after exec
//	                         so this approximates end-to-end wall-clock).
//
// Unknown field names return (0, false, nil) so a workflow that declares an
// unsupported telemetry metric just doesn't progress for that metric — no
// crash, no silent zero.
func resolveFromTelemetry(ctx context.Context, workspacePath, runFolder, field string) (float64, bool, error) {
	field = strings.TrimSpace(field)
	if field == "" {
		return 0, false, fmt.Errorf("telemetry source field is empty")
	}

	switch field {
	case "run.total_cost_usd":
		return tokenFileCostUSD(ctx, workspacePath, runFolder, orchestrator.CostScopeExecution)
	case "run.duration_seconds":
		return tokenFileDurationSeconds(ctx, workspacePath, runFolder, orchestrator.CostScopeExecution)

	case "eval.total_cost_usd":
		return tokenFileCostUSD(ctx, workspacePath, runFolder, orchestrator.CostScopeEvaluation)
	case "eval.duration_seconds":
		return tokenFileDurationSeconds(ctx, workspacePath, runFolder, orchestrator.CostScopeEvaluation)

	case "total.cost_usd":
		runCost, runOK, err := tokenFileCostUSD(ctx, workspacePath, runFolder, orchestrator.CostScopeExecution)
		if err != nil {
			return 0, false, err
		}
		evalCost, evalOK, err := tokenFileCostUSD(ctx, workspacePath, runFolder, orchestrator.CostScopeEvaluation)
		if err != nil {
			return 0, false, err
		}
		// Available if either scope reported a value; missing scopes contribute zero.
		if !runOK && !evalOK {
			return 0, false, nil
		}
		return runCost + evalCost, true, nil

	case "total.duration_seconds":
		runDur, runOK, err := tokenFileDurationSeconds(ctx, workspacePath, runFolder, orchestrator.CostScopeExecution)
		if err != nil {
			return 0, false, err
		}
		evalDur, evalOK, err := tokenFileDurationSeconds(ctx, workspacePath, runFolder, orchestrator.CostScopeEvaluation)
		if err != nil {
			return 0, false, err
		}
		if !runOK && !evalOK {
			return 0, false, nil
		}
		return runDur + evalDur, true, nil

	default:
		// Unknown telemetry field: not an error, just unavailable. Recognized
		// fields are documented above.
		return 0, false, nil
	}
}

// tokenFileCostUSD sums TotalCost across all models in the named scope's
// TokenUsageFile for runFolder. Returns (0, false, nil) when no token file
// is found — common for workflows that didn't track costs in that scope.
func tokenFileCostUSD(ctx context.Context, workspacePath, runFolder string, scope orchestrator.CostScope) (float64, bool, error) {
	tokenFile, ok, err := readRunTokenUsageForScope(ctx, workspacePath, runFolder, scope)
	if err != nil || !ok || tokenFile == nil {
		return 0, false, err
	}
	return orchestrator.TokenUsageTotalCostUSD(tokenFile), true, nil
}

// tokenFileDurationSeconds returns UpdatedAt - CreatedAt of the named scope's
// TokenUsageFile, in seconds.
func tokenFileDurationSeconds(ctx context.Context, workspacePath, runFolder string, scope orchestrator.CostScope) (float64, bool, error) {
	tokenFile, ok, err := readRunTokenUsageForScope(ctx, workspacePath, runFolder, scope)
	if err != nil || !ok || tokenFile == nil {
		return 0, false, err
	}
	if tokenFile.CreatedAt.IsZero() || tokenFile.UpdatedAt.IsZero() {
		return 0, false, nil
	}
	dur := tokenFile.UpdatedAt.Sub(tokenFile.CreatedAt).Seconds()
	if dur < 0 {
		dur = 0
	}
	return dur, true, nil
}

// metricRunWindow captures this specific run's wall-clock window, used to
// disambiguate cost entries when a run folder name is reused across runs
// (e.g. iteration-0/default-group rotates each new run). Reading run_metadata.json
// gives us this run's actual time window so we can filter the cost ledger.
type metricRunWindow struct {
	Start time.Time
	End   time.Time
}

// readRunMetadataWindow returns this run's [created_at, completed_at + grace]
// window from runs/<runFolder>/run_metadata.json. The 6-hour grace tail
// covers evaluation costs that get written after execution finishes (eval
// runs sequentially after the workflow). Returns (zero, false, nil) when no
// metadata file exists — caller should fall back to a behaviour that does
// not depend on the window.
func readRunMetadataWindow(ctx context.Context, workspacePath, runFolder string) (metricRunWindow, bool, error) {
	metaPath := path.Join(strings.Trim(workspacePath, "/"), "runs", strings.Trim(runFolder, "/"), "run_metadata.json")
	content, exists, err := readFileFromWorkspace(ctx, metaPath)
	if err != nil {
		return metricRunWindow{}, false, err
	}
	if !exists || strings.TrimSpace(content) == "" {
		return metricRunWindow{}, false, nil
	}
	var meta struct {
		CreatedAt   time.Time `json:"created_at"`
		CompletedAt time.Time `json:"completed_at"`
	}
	if err := json.Unmarshal([]byte(content), &meta); err != nil {
		return metricRunWindow{}, false, err
	}
	if meta.CreatedAt.IsZero() {
		return metricRunWindow{}, false, nil
	}
	end := meta.CompletedAt
	if end.IsZero() {
		// Run hasn't finished — bound the window at "now + grace" so in-flight
		// cost entries are still picked up. This also handles the case where
		// completed_at hasn't been written yet.
		end = time.Now().UTC()
	}
	return metricRunWindow{
		Start: meta.CreatedAt,
		End:   end.Add(6 * time.Hour), // generous tail for eval-scope costs
	}, true, nil
}

// readRunTokenUsageForScope returns this run's TokenUsageFile in the given
// cost scope. To avoid over-aggregating across reused run folder names (e.g.
// iteration-0/default-group rotates every run), it reads run_metadata.json
// and only merges daily cost entries whose own created_at falls within the
// run's window.
//
// When run_metadata.json is missing, falls back to the legacy
// merge-across-all-time behaviour so older workflows without metadata still
// resolve a value. The fallback over-aggregates for reused folder names —
// but the same was true before this change.
func readRunTokenUsageForScope(ctx context.Context, workspacePath, runFolder string, scope orchestrator.CostScope) (*orchestrator.TokenUsageFile, bool, error) {
	window, hasWindow, err := readRunMetadataWindow(ctx, workspacePath, runFolder)
	if err != nil {
		return nil, false, err
	}
	if !hasWindow {
		// Legacy fallback: aggregate across all daily files. Equivalent to the
		// pre-fix behaviour. Right answer for unique-per-run folder names;
		// wrong answer for reused folder names but no worse than before.
		all, err := readAllRunTokenUsageFromCosts(ctx, workspacePath, scope)
		if err != nil {
			return nil, false, err
		}
		tokenFile, ok := all[runFolder]
		if !ok || tokenFile == nil {
			return nil, false, nil
		}
		return tokenFile, true, nil
	}

	// Window-aware path: walk the scope's daily cost files and merge only
	// entries whose own created_at falls within this run's window.
	root := workspaceCostPath(workspacePath, "costs", string(scope))
	if err := ensureWorkspaceCostMigration(ctx, workspacePath); err != nil {
		return nil, false, err
	}
	filePaths, err := listWorkspaceFilesRecursive(ctx, root)
	if err != nil {
		return nil, false, err
	}

	var merged *orchestrator.TokenUsageFile
	for _, filePath := range filePaths {
		if !strings.HasSuffix(filePath, ".json") {
			continue
		}
		content, exists, err := readFileFromWorkspace(ctx, filePath)
		if err != nil {
			return nil, false, err
		}
		if !exists {
			continue
		}
		var dailyFile orchestrator.DailyGroupTokenUsageFile
		if err := json.Unmarshal([]byte(content), &dailyFile); err != nil {
			continue
		}
		orchestrator.EnsureDailyGroupTokenUsageFilePricing(&dailyFile)

		entry := dailyFile.RunFolders[runFolder]
		if entry == nil {
			continue
		}
		// Window filter: keep entries whose own created_at falls inside the
		// run's window. Entries whose created_at is before window.Start belong
		// to a previous reuse of the run folder name; entries after window.End
		// belong to a future run.
		if entry.CreatedAt.IsZero() {
			continue
		}
		if entry.CreatedAt.Before(window.Start) || entry.CreatedAt.After(window.End) {
			continue
		}
		merged = orchestrator.MergeTokenUsageFiles(merged, entry)
	}
	if merged == nil {
		return nil, false, nil
	}
	return merged, true, nil
}
