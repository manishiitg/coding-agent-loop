package server

import (
	"context"
	"encoding/json"
	"fmt"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

// =====================================================================
// auto_improvement_tools.go — register auto-improvement metric tools with an
// mcpagent so the workflow optimizer can maintain planning/metrics.json.
//
// Caller wires these into the agent's tool registry alongside other custom
// tools like reorganize_knowledgebase / consolidate_knowledgebase. Each
// registration captures the workspacePath in a closure so the LLM does not
// need to supply it.
// =====================================================================

// RegisterProposeMetricTool exposes propose_metric to the proposer LLM.
func RegisterProposeMetricTool(agent *mcpagent.Agent, workspacePath, triggerSource string, logger loggerv2.Logger) {
	desc := "Privileged write path for <workflow>/planning/metrics.json (which is folder-guarded against shell writes). " +
		"Defines a new metric: supply id + unit + direction + mode + source. " +
		"Metrics are append-only by id. To change a metric's meaning, retire the old one (retire_metric) and create a new one with a different id. " +
		"Use this when harden/replan evidence needs a metric that does not yet exist, or when metric history shows a broken source definition. " +
		"\n\nBEFORE PROPOSING — sanity-check the source: read db/metrics_history.jsonl for the latest snapshot rows of each existing metric. Any row with has_value=false carries a resolve_error explaining why. The most common gotcha is field=\"some_name\" when the eval step's report has no structured output_content — for flat eval reports (score/max_score only), use field=\"\" (percent score), field=\"score\", or field=\"max_score\". Anything else requires the eval Python to emit structured JSON output containing that key." +
		"\n\nReturns { metric_id, status }."
	params := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"id": map[string]interface{}{
				"type":        "string",
				"description": "Unique kebab.dot id. Pattern: ^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$.",
			},
			"label":     map[string]interface{}{"type": "string"},
			"unit":      map[string]interface{}{"type": "string", "description": "percent | usd | seconds | count | days | ratio | bps"},
			"direction": map[string]interface{}{"type": "string", "enum": []string{"higher_better", "lower_better"}},
			"mode":      map[string]interface{}{"type": "string", "enum": []string{"target", "slo"}},
			"target":    map[string]interface{}{"type": "number", "description": "Required when mode=target."},
			"floor":     map[string]interface{}{"type": "number", "description": "Required when mode=slo + direction=higher_better."},
			"ceiling":   map[string]interface{}{"type": "number", "description": "Required when mode=slo + direction=lower_better."},
			"source": map[string]interface{}{
				"type":        "object",
				"description": "Where each run's metric value comes from. Pick exactly one type:\n  • eval_step — value is the score (or a structured-output field) of a named eval step (must exist in evaluation/evaluation_plan.json). REQUIRES `id`. Example: { type: \"eval_step\", id: \"eval-data-accuracy\" }\n  • telemetry — value is read from run telemetry (cost, latency, etc.). REQUIRES `field`. Example: { type: \"telemetry\", field: \"run.total_cost_usd\" }\nFor external feeds, schema checks, lineage, or delayed-outcome attribution, write an eval step whose Python does the work and emits the value, then point the metric at that eval step (eval_step is the canonical funnel for any non-telemetry metric).\nDo NOT invent other type values like \"db\" or \"manual\" or \"json_file\" — they will fail validation.",
				"properties": map[string]interface{}{
					"type":  map[string]interface{}{"type": "string", "enum": []string{"eval_step", "telemetry"}, "description": "Source kind. Two valid values, listed in the parent description."},
					"id":    map[string]interface{}{"type": "string", "description": "REQUIRED when type=eval_step. The eval step id from evaluation/evaluation_plan.json. When set together with `field`, the metric reads that field from the eval step's structured JSON output (in OutputContent); when `field` is empty, the metric reads the eval step's percent score."},
					"field": map[string]interface{}{"type": "string", "description": "REQUIRED when type=telemetry. Optional when type=eval_step (then it's a field key into the eval step's JSON output). For type=telemetry, six wired field names — three scopes × two measurements each:\n  • run.total_cost_usd — execution cost only (workflow steps)\n  • run.duration_seconds — execution wall-clock seconds (workflow only)\n  • eval.total_cost_usd — evaluation cost only (eval scoring + eval Python)\n  • eval.duration_seconds — evaluation wall-clock seconds\n  • total.cost_usd — execution + evaluation cost combined\n  • total.duration_seconds — execution + evaluation duration combined (sequential, so ~ end-to-end wall-clock)\nUnknown telemetry field names silently return no value."},
				},
				"required": []string{"type"},
			},
		},
		"required": []string{"id", "unit", "direction", "mode", "source"},
	}

	handler := func(ctx context.Context, args map[string]interface{}) (string, error) {
		raw, err := json.Marshal(args)
		if err != nil {
			return "", err
		}
		var input ProposeMetricInput
		if err := json.Unmarshal(raw, &input); err != nil {
			return fmt.Sprintf("invalid arguments for propose_metric: %v", err), nil
		}
		out, err := ProposeMetric(ctx, workspacePath, triggerSource, input)
		if err != nil {
			return fmt.Sprintf("propose_metric failed: %v", err), nil
		}
		body, _ := json.MarshalIndent(out, "", "  ")
		return string(body), nil
	}

	if err := agent.RegisterCustomTool("propose_metric", desc, params, handler, "auto_improvement"); err != nil {
		if logger != nil {
			logger.Warn(fmt.Sprintf("Failed to register propose_metric: %v", err))
		}
	}
}

// RegisterRetireMetricTool exposes retire_metric to the proposer LLM.
// Soft-deletes a metric: removes it from the active metrics array and moves
// it to the archive with an archived_reason. The metric stops being
// collected on future runs; existing rows in db/metrics_history.jsonl that
// reference its id remain (the archive entry preserves what they meant).
func RegisterRetireMetricTool(agent *mcpagent.Agent, workspacePath, triggerSource string, logger loggerv2.Logger) {
	desc := "Retire a metric defined in planning/metrics.json. Removes the " +
		"metric from the active list. Subsequent runs skip the metric in the " +
		"snapshot pipeline. Past db/metrics_history.jsonl rows are preserved " +
		"as-is — the decision-log entry created by this call is the audit " +
		"trail for what those historical values represented." +
		"\n\nCommon use cases: (1) the metric is broken — its latest snapshot " +
		"rows in db/metrics_history.jsonl all show resolve_error (e.g. wrong " +
		"`field` for the eval step). Retire it and propose a new metric with " +
		"a different id and a corrected definition. (2) the metric is no " +
		"longer relevant. (3) the metric is being replaced by a better-defined " +
		"alternative. Always pass a reason that cites the resolve_error or " +
		"superseding metric — the decision log is the only trace future readers " +
		"have for why historical rows under this id should be interpreted." +
		"\n\nReturns { metric_id, status }."
	params := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"id":     map[string]interface{}{"type": "string", "description": "The metric id (kebab.dot) to retire. Must currently exist in metrics.json::metrics[]."},
			"reason": map[string]interface{}{"type": "string", "description": "Why the metric is being retired. Stored in metrics.json::archive[].archived_reason for traceability. Required."},
		},
		"required": []string{"id", "reason"},
	}

	handler := func(ctx context.Context, args map[string]interface{}) (string, error) {
		raw, err := json.Marshal(args)
		if err != nil {
			return "", err
		}
		var input RetireMetricInput
		if err := json.Unmarshal(raw, &input); err != nil {
			return fmt.Sprintf("invalid arguments for retire_metric: %v", err), nil
		}
		out, err := RetireMetric(ctx, workspacePath, triggerSource, input)
		if err != nil {
			return fmt.Sprintf("retire_metric failed: %v", err), nil
		}
		body, _ := json.MarshalIndent(out, "", "  ")
		return string(body), nil
	}

	if err := agent.RegisterCustomTool("retire_metric", desc, params, handler, "auto_improvement"); err != nil {
		if logger != nil {
			logger.Warn(fmt.Sprintf("Failed to register retire_metric: %v", err))
		}
	}
}

// RegisterAutoImprovementProposerTools registers optimizer-side metric tools.
// Call this alongside the existing builder tools when the workshop session
// enters optimizer mode.
//
// Two earlier tools are intentionally NOT registered:
//   - capture_context — the optimizer/builder reads chat for user-supplied
//     business rules and writes them directly via diff_patch_workspace_file
//     (knowledgebase/rules/rules.md) plus a decisions.jsonl entry with
//     source=user. The system prompt documents the format.
func RegisterAutoImprovementProposerTools(agent *mcpagent.Agent, workspacePath, triggerSource string, logger loggerv2.Logger) {
	RegisterProposeMetricTool(agent, workspacePath, triggerSource, logger)
	RegisterRetireMetricTool(agent, workspacePath, triggerSource, logger)
}
