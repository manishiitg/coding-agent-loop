package server

import (
	"context"
	"encoding/json"
	"fmt"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"

	"mcp-agent-builder-go/agent_go/cmd/server/guidance"
)

// =====================================================================
// auto_improvement_tools.go — register auto-improvement tools with an
// mcpagent so the workflow optimizer can maintain planning/metrics.json and
// capture user-supplied runtime context.
//
// Caller wires these into the agent's tool registry alongside other custom
// tools like reorganize_knowledgebase / consolidate_knowledgebase. Each
// registration captures the workspacePath in a closure so the LLM does not
// need to supply it.
// =====================================================================

// RegisterProposeMetricTool exposes propose_metric to the proposer LLM.
func RegisterProposeMetricTool(agent *mcpagent.Agent, workspacePath, triggerSource string, logger loggerv2.Logger) {
	desc := "Privileged write path for <workflow>/planning/metrics.json (which is folder-guarded against shell writes). " +
		"Defines a new metric, or amends an existing metric when amend_existing is supplied. Supply id + unit + direction + mode + source. " +
		"Also supply role when known: primary for the small set of north-star or must-not-break metrics that steer optimization, secondary for diagnostic/supporting metrics. Supply category to group the signal by area (for example outcome, execution, guardrail, content_quality, strategy_learning, telemetry). " +
		"Existing metric ids are protected: to change one, pass amend_existing:{id,reason}; the prior active definition is archived and the metric version increments. " +
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
			"role":      map[string]interface{}{"type": "string", "enum": []string{"primary", "secondary"}, "description": "primary = north-star or must-not-break metric that directly steers improvement; secondary = diagnostic/supporting metric used to explain or protect primary movement."},
			"category":  map[string]interface{}{"type": "string", "description": "Freeform grouping for UI and optimizer reasoning. Recommended values: outcome, execution, guardrail, content_quality, strategy_learning, telemetry."},
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
			"success_criteria": map[string]interface{}{
				"type":        "string",
				"description": "The soul.md success criterion this metric operationalizes. Quote or summarize the exact criterion.",
			},
			"parent": map[string]interface{}{
				"type":        "string",
				"description": "Optional parent metric id for UI grouping. No weighted rollup.",
			},
			"amend_existing": map[string]interface{}{
				"type":        "object",
				"description": "Required when changing an existing metric id. The top-level id must match amend_existing.id. The previous active definition is copied to metrics.json::archive and the new definition gets version+1.",
				"properties": map[string]interface{}{
					"id":     map[string]interface{}{"type": "string", "description": "Existing active metric id to amend. Must match top-level id."},
					"reason": map[string]interface{}{"type": "string", "description": "Why this definition/source/threshold is changing. Stored in metrics.json::archive[].archived_reason. Narrate the change in builder/improve.html yourself."},
				},
				"required": []string{"id", "reason"},
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

	// Gated: propose_metric mutates planning/metrics.json — the proposer must
	// have read optimize-playbook (metric design rules: north-star vs.
	// diagnostic, source contract for snapshot resolution, when to amend vs.
	// retire) before defining a new metric.
	gatedHandler := guidance.WithDocPrecondition([]string{"optimize-playbook"}, guidance.DefaultTracker(), handler)
	if err := agent.RegisterCustomTool("propose_metric", desc, params, gatedHandler, "auto_improvement"); err != nil {
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
		"metric from the active list and archives its final definition. Subsequent runs skip the metric in the " +
		"snapshot pipeline. Past db/metrics_history.jsonl rows are preserved " +
		"as-is — the metrics.json::archive entry is the audit " +
		"trail for what those historical values represented." +
		"\n\nCommon use cases: (1) the metric is broken — its latest snapshot " +
		"rows in db/metrics_history.jsonl all show resolve_error (e.g. wrong " +
		"`field` for the eval step). Prefer propose_metric with amend_existing " +
		"when the same metric id should keep representing the same outcome with a corrected source. (2) the metric is no " +
		"longer relevant. (3) the metric is being replaced by a better-defined " +
		"alternative. Always pass a reason that cites the resolve_error or " +
		"superseding metric — metrics.json::archive is the trace future readers " +
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

	// Gated: same reason as propose_metric — the optimizer must understand
	// when retiring is the right action vs. amend_existing on the same id.
	gatedHandler := guidance.WithDocPrecondition([]string{"optimize-playbook"}, guidance.DefaultTracker(), handler)
	if err := agent.RegisterCustomTool("retire_metric", desc, params, gatedHandler, "auto_improvement"); err != nil {
		if logger != nil {
			logger.Warn(fmt.Sprintf("Failed to register retire_metric: %v", err))
		}
	}
}

// RegisterCaptureContextTool exposes capture_context to the optimizer/builder.
// It is the privileged, structured path for durable user-supplied runtime
// context. The tool validates target metric anchoring and writes the context
// file; narrating the capture into builder/improve.html is the agent's job.
func RegisterCaptureContextTool(agent *mcpagent.Agent, workspacePath string, logger loggerv2.Logger) {
	desc := "Capture durable user-supplied runtime business context for this workflow. " +
		"Use only after the user confirms the item should be remembered across runs, and only when the Workflow Profile allows business-context accumulation. " +
		"Writes to knowledgebase/context/context.md. Record the capture in builder/improve.html yourself as a User rule (authoritative) entry. " +
		"Every capture must name target_metrics so context stays tied to measurable outcomes. " +
		"Use for persistent rules, preferences, constraints, assumptions, examples, ICP filters, approval rules, brand voice, or domain context that workflow steps must respect. " +
		"Do not use for one-off instructions, general chat memory, workflow-discovered facts that belong in knowledgebase/notes, or execution recipes that belong in learnings."
	params := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"section": map[string]interface{}{
				"type":        "string",
				"description": "Markdown section heading in knowledgebase/context/context.md. Defaults to General when empty.",
			},
			"context_text": map[string]interface{}{
				"type":        "string",
				"description": "The durable user-supplied context to capture. Keep it concise and faithful to the user's wording.",
			},
			"target_metrics": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Metric ids this context is expected to affect. Required so context capture is tied to workflow outcomes.",
			},
			"example_note": map[string]interface{}{
				"type":        "string",
				"description": "Optional short note about the example or source context for this capture.",
			},
		},
		"required": []string{"context_text", "target_metrics"},
	}

	handler := func(ctx context.Context, args map[string]interface{}) (string, error) {
		raw, err := json.Marshal(args)
		if err != nil {
			return "", err
		}
		var input CaptureContextInput
		if err := json.Unmarshal(raw, &input); err != nil {
			return fmt.Sprintf("invalid arguments for capture_context: %v", err), nil
		}
		out, err := CaptureContextTool(ctx, workspacePath, input)
		if err != nil {
			return fmt.Sprintf("capture_context failed: %v", err), nil
		}
		body, _ := json.MarshalIndent(out, "", "  ")
		return string(body), nil
	}

	if err := agent.RegisterCustomTool("capture_context", desc, params, handler, "auto_improvement"); err != nil {
		if logger != nil {
			logger.Warn(fmt.Sprintf("Failed to register capture_context: %v", err))
		}
	}
}

// RegisterAutoImprovementProposerTools registers optimizer-side framework tools.
// Call this alongside the existing builder tools when the workshop session
// enters optimizer mode.
func RegisterAutoImprovementProposerTools(agent *mcpagent.Agent, workspacePath, triggerSource string, logger loggerv2.Logger) {
	RegisterProposeMetricTool(agent, workspacePath, triggerSource, logger)
	RegisterRetireMetricTool(agent, workspacePath, triggerSource, logger)
	RegisterCaptureContextTool(agent, workspacePath, logger)
}
