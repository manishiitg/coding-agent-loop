package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

// =====================================================================
// auto_improvement_tools.go — register propose_experiment, propose_metric,
// conclude_experiment, and query_experiment_history with an mcpagent so the
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
		"The next runs (or runs after evaluable_at_lag elapses for delayed metrics) populate measurement.values; " +
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
				"description": "One or more metric ids from <workflow>/metrics.json. Each must exist; expected_direction must match each metric's declared direction.",
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

	if err := agent.RegisterCustomTool("propose_experiment", desc, params, handler); err != nil {
		if logger != nil {
			logger.Warn(fmt.Sprintf("Failed to register propose_experiment: %v", err))
		}
	}
}

// RegisterProposeMetricTool exposes propose_metric to the proposer LLM.
func RegisterProposeMetricTool(agent *mcpagent.Agent, workspacePath, triggerSource string, logger loggerv2.Logger) {
	desc := "Define a new metric or amend an existing one in <workflow>/metrics.json. " +
		"On amend, the prior definition is archived (so the existing trajectory series is preserved and chart renderers break the line at the version transition). " +
		"Use this BEFORE proposing experiments that target a metric that does not yet exist. " +
		"Returns { metric_id, version, status }."
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
				"type": "object",
				"properties": map[string]interface{}{
					"type":       map[string]interface{}{"type": "string", "enum": []string{"eval_step", "telemetry", "external", "delayed_ground_truth", "lineage", "schema_check"}},
					"id":         map[string]interface{}{"type": "string", "description": "For type=eval_step, the eval step id."},
					"field":      map[string]interface{}{"type": "string", "description": "For type=telemetry/external, the dotted path."},
					"joined_via": map[string]interface{}{"type": "string", "description": "For type=delayed_ground_truth."},
				},
				"required": []string{"type"},
			},
			"evaluable_at_lag": map[string]interface{}{
				"type":        "string",
				"description": "Optional. Duration like 30d / 12h / 90s. Forces the experiment loop to wait for this lag before resolving the value.",
			},
			"parent": map[string]interface{}{"type": "string"},
			"amend_existing": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id":     map[string]interface{}{"type": "string"},
					"reason": map[string]interface{}{"type": "string"},
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

	if err := agent.RegisterCustomTool("propose_metric", desc, params, handler); err != nil {
		if logger != nil {
			logger.Warn(fmt.Sprintf("Failed to register propose_metric: %v", err))
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

	if err := agent.RegisterCustomTool("conclude_experiment", desc, params, handler); err != nil {
		if logger != nil {
			logger.Warn(fmt.Sprintf("Failed to register conclude_experiment: %v", err))
		}
	}
}

// RegisterQueryExperimentHistoryTool exposes the optional query_experiment_history
// helper to the proposer. Lets the LLM avoid retrying recently-failed or pinned
// hypotheses without doing raw history.jsonl reads.
func RegisterQueryExperimentHistoryTool(agent *mcpagent.Agent, workspacePath string, logger loggerv2.Logger) {
	desc := "Read the workflow's experiment history. Returns the most recent concluded experiments with their verdicts " +
		"and rationales. Use this BEFORE proposing a new experiment to avoid retrying hypotheses that recently failed " +
		"or that the user has pinned/forbidden. Returns a JSON array."
	params := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"target_metrics": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Filter to experiments that targeted any of these metric ids. Optional.",
			},
			"since": map[string]interface{}{"type": "string", "description": "ISO-8601 timestamp; only experiments concluded on/after."},
			"exclude_pinned": map[string]interface{}{"type": "boolean", "description": "Default true."},
			"limit":          map[string]interface{}{"type": "integer", "description": "Default 20, max 200."},
		},
	}

	handler := func(ctx context.Context, args map[string]interface{}) (string, error) {
		filterMetrics, _ := args["target_metrics"].([]interface{})
		since, _ := args["since"].(string)
		excludePinned := true
		if v, ok := args["exclude_pinned"].(bool); ok {
			excludePinned = v
		}
		limit := 20
		if v, ok := args["limit"].(float64); ok {
			limit = int(v)
		}
		if limit > 200 {
			limit = 200
		}

		hist, err := ReadHistoryExperiments(ctx, workspacePath)
		if err != nil {
			return fmt.Sprintf("query_experiment_history failed: %v", err), nil
		}
		// Build pinned set for filter.
		pinned := map[string]struct{}{}
		if excludePinned {
			cfg, _ := ReadExperimentsConfig(ctx, workspacePath)
			for _, p := range cfg.PinnedHypotheses {
				pinned[strings.TrimSpace(p.Text)] = struct{}{}
			}
		}
		filterSet := map[string]struct{}{}
		for _, x := range filterMetrics {
			if s, ok := x.(string); ok {
				filterSet[s] = struct{}{}
			}
		}

		out := make([]ExperimentRecord, 0, len(hist))
		for _, e := range hist {
			if since != "" && e.ConcludedAt < since {
				continue
			}
			if len(filterSet) > 0 {
				match := false
				for _, m := range e.TargetMetrics {
					if _, ok := filterSet[m]; ok {
						match = true
						break
					}
				}
				if !match {
					continue
				}
			}
			if _, isPinned := pinned[strings.TrimSpace(e.Hypothesis)]; isPinned {
				continue
			}
			out = append(out, e)
		}
		// Most recent first; cap to limit.
		// (history.jsonl is append-order; concluded_at is best proxy for recency.)
		// Inline sort to avoid pulling in sort just for this.
		for i := 0; i < len(out); i++ {
			for j := i + 1; j < len(out); j++ {
				if out[j].ConcludedAt > out[i].ConcludedAt {
					out[i], out[j] = out[j], out[i]
				}
			}
		}
		if len(out) > limit {
			out = out[:limit]
		}
		body, _ := json.MarshalIndent(out, "", "  ")
		return string(body), nil
	}

	if err := agent.RegisterCustomTool("query_experiment_history", desc, params, handler); err != nil {
		if logger != nil {
			logger.Warn(fmt.Sprintf("Failed to register query_experiment_history: %v", err))
		}
	}
}

// RegisterAutoImprovementProposerTools registers the proposer-side tools
// (propose_experiment + propose_metric + query_experiment_history). Call this
// alongside the existing builder tools when the workshop session enters
// optimizer mode.
func RegisterAutoImprovementProposerTools(agent *mcpagent.Agent, workspacePath, triggerSource string, logger loggerv2.Logger) {
	RegisterProposeExperimentTool(agent, workspacePath, triggerSource, logger)
	RegisterProposeMetricTool(agent, workspacePath, triggerSource, logger)
	RegisterQueryExperimentHistoryTool(agent, workspacePath, logger)
}
