package server

import (
	"context"
	"encoding/json"
	"fmt"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

// =====================================================================
// auto_improvement_tools.go — register propose_experiment, propose_metric,
// and conclude_experiment with an mcpagent so the
// proposer (builder in optimizer mode) and evaluator (narrow-context agent)
// can call them.
//
// Caller wires these into the agent's tool registry alongside other custom
// tools like reorganize_knowledgebase / consolidate_knowledgebase. Each
// registration captures the workspacePath in a closure so the LLM does not
// need to supply it.
// =====================================================================

// RegisterProposeExperimentTool exposes propose_experiment to the proposer LLM.
// `triggerSource` is the slash-command name the experiment will be tagged with
// (e.g. "improve-eval", "improve-workflow"). For the optimizer loop, pass
// "improve-continuously".
func RegisterProposeExperimentTool(agent *mcpagent.Agent, workspacePath, triggerSource string, logger loggerv2.Logger) {
	desc := "Open an experiment to test a hypothesis about the workflow. " +
		"Pre-registers the hypothesis, captures a baseline from recent run history, snapshots world_state, " +
		"applies the intervention atomically with a revertable diff, and starts a measurement window. " +
		"The next runs populate measurement.values; " +
		"when target_runs is reached, the system computes the verdict and the evaluator agent narrates it via conclude_experiment. " +
		"Do NOT use this tool to read state or to apply unconditional fixes — open an experiment ONLY when you have a falsifiable hypothesis " +
		"about how the change will move a declared metric. Returns { experiment_id, status, decisions_entry_id }."

	params := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"hypothesis": map[string]interface{}{
				"type":        "string",
				"description": "Short prose, ≤200 chars. State the change and the metric it should move and by how much. Example: \"Adding rule 'never recommend assets <6mo old' will reduce risk.false_positive_rate by ≥10pp.\"",
			},
			"target_metrics": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "One or more metric ids from <workflow>/planning/metrics.json. Each must exist; expected_direction must match each metric's declared direction.",
			},
			"expected_direction": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"increase", "decrease", "maintain"},
				"description": "Direction of the predicted metric movement.",
			},
			"expected_magnitude": map[string]interface{}{
				"type":        "number",
				"description": "Predicted absolute change in the metric's unit. > 0.",
			},
			"intervention_changes": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Workflow-relative path. Must match experiments/config.json::allowed_intervention_paths. Forbidden: .env, .git/, workflow.json.",
						},
						"operation": map[string]interface{}{
							"type": "string",
							"enum": []string{"append", "replace", "patch", "create"},
						},
						"content": map[string]interface{}{
							"type":        "string",
							"description": "New content (operation=append/replace/create) or unified diff (operation=patch).",
						},
					},
					"required": []string{"path", "operation", "content"},
				},
				"description": "File writes the intervention applies. Captured as a revertable diff before any write.",
			},
			"measurement_runs": map[string]interface{}{
				"type":        "integer",
				"description": "Override the workflow's default sample size. Optional.",
			},
			"linked_review_finding": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Optional builder/review.md finding ids (F-YYYY-MM-DD-NNN) this experiment is intended to resolve. Stored on the builder/decisions.jsonl entry for audit linkage.",
			},
			"linked_improve_entry": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Optional builder/improve.md proposal ids (I-YYYY-MM-DD-NNN) this experiment is intended to resolve. Stored on the builder/decisions.jsonl entry for audit linkage.",
			},
		},
		"required": []string{"hypothesis", "target_metrics", "expected_direction", "expected_magnitude", "intervention_changes"},
	}

	handler := func(ctx context.Context, args map[string]interface{}) (string, error) {
		raw, err := json.Marshal(args)
		if err != nil {
			return "", err
		}
		var input ProposeExperimentInput
		if err := json.Unmarshal(raw, &input); err != nil {
			return fmt.Sprintf("invalid arguments for propose_experiment: %v", err), nil
		}
		out, err := ProposeExperiment(ctx, workspacePath, triggerSource, input)
		if err != nil {
			return fmt.Sprintf("propose_experiment failed: %v", err), nil
		}
		body, _ := json.MarshalIndent(out, "", "  ")
		return string(body), nil
	}

	if err := agent.RegisterCustomTool("propose_experiment", desc, params, handler, "auto_improvement"); err != nil {
		if logger != nil {
			logger.Warn(fmt.Sprintf("Failed to register propose_experiment: %v", err))
		}
	}
}

// RegisterProposeMetricTool exposes propose_metric to the proposer LLM.
func RegisterProposeMetricTool(agent *mcpagent.Agent, workspacePath, triggerSource string, logger loggerv2.Logger) {
	desc := "Privileged write path for <workflow>/planning/metrics.json (which is folder-guarded against shell writes). " +
		"Three modes:\n" +
		"  1. Define a new metric — supply id + unit + direction + mode + source.\n" +
		"  2. Amend an existing metric — supply the new definition + amend_existing={id, reason}; the prior definition archives, version bumps, the trajectory line breaks at the transition.\n" +
		"  3. Set active_mode only — supply just `active_mode` (omit the metric fields). For dual-mode workflows declared in improve.md (e.g. explore/exploit cycles); the value lives at the top of metrics.json so steps can branch on it via the variable resolver.\n" +
		"You may also pass `active_mode` alongside a metric definition to do both in one call. " +
		"Use this BEFORE proposing experiments that target a metric that does not yet exist. " +
		"Returns { metric_id, version, status, active_mode }."
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
			"parent": map[string]interface{}{"type": "string"},
			"linked_success_criteria": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "REQUIRED for outcome metrics; optional for telemetry. The success_criteria entries (or their indices/short labels) from planning/plan.json that this metric operationalizes. The framework cannot enforce alignment between metric movement and user-facing success on its own — this field is the trace. Example: [\"Email reply rate ≥ 30%\"] or [\"sc-0\", \"sc-2\"]. Leave empty only for purely auxiliary metrics like cost_per_run / run_duration_seconds (telemetry SLOs that don't operationalize a plan-level criterion). Empty for outcome metrics is allowed but flagged in the UI as an unanchored metric.",
			},
			"amend_existing": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id":     map[string]interface{}{"type": "string"},
					"reason": map[string]interface{}{"type": "string"},
				},
				"required": []string{"id", "reason"},
			},
			"active_mode": map[string]interface{}{
				"type":        "string",
				"description": "Optional. For dual-mode workflows (declared in improve.md as explore/exploit, train/serve, etc.) — sets the runtime mode. Steps can read this via the variable resolver to branch behavior. Pass alone (without metric fields) to update only the mode; pass alongside a metric definition to do both in one write.",
			},
		},
		// No top-level required: either supply a metric definition (id+unit+direction+mode+source)
		// OR active_mode alone. The handler enforces the choice and reports a clear error if neither
		// is supplied.
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
	desc := "Retire a metric defined in planning/metrics.json. Soft delete: " +
		"removes the metric from the active list and moves the prior definition to " +
		"metrics.json::archive[] with the supplied reason. Subsequent runs skip the " +
		"metric in the snapshot pipeline. Past db/metrics_history.jsonl rows are " +
		"preserved as-is — the archive entry is the audit trail for what those " +
		"historical values represented. Use this when a metric is superseded, " +
		"deprecated, or no longer relevant. Returns { metric_id, version, status }."
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

// RegisterConcludeExperimentTool exposes conclude_experiment to the EVALUATOR
// agent only. The evaluator must be a separate, narrow-scope agent with no
// other tools (this is the proposer ≠ evaluator guardrail).
func RegisterConcludeExperimentTool(agent *mcpagent.Agent, workspacePath string, logger loggerv2.Logger) {
	desc := "Narrate the system-computed verdict for an experiment whose measurement window has closed. " +
		"The verdict is already populated by the heuristic computer; your job is to write a short rationale " +
		"explaining why that verdict is honest given the data. Override the verdict ONLY when you have strong " +
		"evidence the heuristic is wrong — large world_state drift between started_at and concluded_at, or " +
		"clearly visible confound. Override-with-reason is audit-flagged. Returns { final_verdict, archived }."
	params := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"experiment_id": map[string]interface{}{"type": "string"},
			"rationale":     map[string]interface{}{"type": "string", "description": "Short prose, ≤500 chars."},
			"override_verdict": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"to":     map[string]interface{}{"type": "string", "enum": []string{"kept", "reverted", "inconclusive", "extend"}},
					"reason": map[string]interface{}{"type": "string"},
				},
				"required": []string{"to", "reason"},
			},
		},
		"required": []string{"experiment_id", "rationale"},
	}

	handler := func(ctx context.Context, args map[string]interface{}) (string, error) {
		raw, err := json.Marshal(args)
		if err != nil {
			return "", err
		}
		var input ConcludeExperimentInput
		if err := json.Unmarshal(raw, &input); err != nil {
			return fmt.Sprintf("invalid arguments for conclude_experiment: %v", err), nil
		}
		out, err := ConcludeExperiment(ctx, workspacePath, "evaluator-agent", input)
		if err != nil {
			return fmt.Sprintf("conclude_experiment failed: %v", err), nil
		}
		body, _ := json.MarshalIndent(out, "", "  ")
		return string(body), nil
	}

	if err := agent.RegisterCustomTool("conclude_experiment", desc, params, handler, "auto_improvement"); err != nil {
		if logger != nil {
			logger.Warn(fmt.Sprintf("Failed to register conclude_experiment: %v", err))
		}
	}
}

// RegisterAutoImprovementProposerTools registers the proposer-side tools
// (propose_experiment + propose_metric). Call this alongside the existing
// builder tools when the workshop session enters optimizer mode.
//
// Two earlier tools are intentionally NOT registered:
//   - capture_context — the optimizer/builder reads chat for user-supplied
//     business rules and writes them directly via diff_patch_workspace_file
//     (knowledgebase/rules/rules.md) plus a decisions.jsonl entry with
//     source=user. The system prompt documents the format.
//   - query_experiment_history — before propose_experiment, the agent reads
//     experiments/history.jsonl and experiments/config.json::pinned_hypotheses
//     directly. Two file reads, no special tool needed.
func RegisterAutoImprovementProposerTools(agent *mcpagent.Agent, workspacePath, triggerSource string, logger loggerv2.Logger) {
	RegisterProposeExperimentTool(agent, workspacePath, triggerSource, logger)
	RegisterProposeMetricTool(agent, workspacePath, triggerSource, logger)
	RegisterRetireMetricTool(agent, workspacePath, triggerSource, logger)
}
