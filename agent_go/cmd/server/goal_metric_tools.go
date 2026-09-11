package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
	step "github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"strings"
	"time"
)

func createGoalMetricTools() ([]llmtypes.Tool, map[string]interface{}, map[string]string) {
	var metric, observation map[string]interface{}
	_ = json.Unmarshal([]byte(`{"type": "object", "additionalProperties": false, "properties": {"id": {"type": "string"}, "criterion_id": {"type": "string"}, "name": {"type": "string"}, "role": {"type": "string", "enum": ["primary", "supporting"]}, "unit": {"type": "string"}, "direction": {"type": "string", "enum": ["increase", "decrease", "maintain"]}, "definition": {"type": "string"}, "source": {"type": "string"}, "window": {"type": "string"}, "route": {"type": "string"}, "environment": {"type": "string"}, "collection_frequency": {"type": "string"}, "target_date": {"type": "string"}, "freshness_hours": {"type": "number", "exclusiveMinimum": 0}, "target": {"type": "number"}}, "required": ["id", "criterion_id", "name", "role", "unit", "direction", "definition", "source", "window", "collection_frequency", "freshness_hours"]}`), &metric)
	_ = json.Unmarshal([]byte(`{"type": "object", "additionalProperties": false, "properties": {"criterion_id": {"type": "string"}, "metric": {"type": "string"}, "run_id": {"type": "string"}, "unit": {"type": "string"}, "route": {"type": "string"}, "environment": {"type": "string"}, "observed_at": {"type": "string"}, "status": {"type": "string"}, "value": {"type": "number"}, "evidence": {"type": "array", "items": {"type": "string"}, "minItems": 1}}, "required": ["criterion_id", "metric", "run_id", "unit", "observed_at", "evidence"]}`), &observation)
	specs := []struct {
		name, description, field string
		schema                   map[string]interface{}
	}{
		{"get_goal_metrics", "Read configured primary/supporting metric definitions and recent source-backed observations before setup, collection or strategic review.", "", nil},
		{"configure_goal_metrics", "Builder setup: replace the complete active metric list with exactly one primary metric. Reuse stable IDs; changing measurement meaning requires a new ID. Omitted metrics are retired, history retained. Confirm changes to goal meaning and targets with the user; never invent targets. This saves definitions, not a claim that a collector works.", "metrics", metric},
		{"record_goal_observations", "Record real measurements from workflow runs or scheduled collectors, independently of Pulse reviews. Use configured metric IDs and exact criterion/unit/route/environment. Supply source run ID, actual observation time and evidence. Missing values need status; never substitute zero. Backfill only comparable evidence. Duplicate observations are idempotent; conflicting values are rejected.", "observations", observation},
	}
	tools := []llmtypes.Tool{}
	executors := map[string]interface{}{}
	categories := map[string]string{}
	for _, spec := range specs {
		props := map[string]interface{}{"workspace_path": map[string]interface{}{"type": "string"}}
		required := []string{"workspace_path"}
		if spec.field != "" {
			props[spec.field] = map[string]interface{}{"type": "array", "items": spec.schema, "minItems": 1}
			required = append(required, spec.field)
		}
		tools = append(tools, llmtypes.Tool{Type: "function", Function: &llmtypes.FunctionDefinition{Name: spec.name, Description: spec.description, Parameters: llmtypes.NewParameters(map[string]interface{}{"type": "object", "additionalProperties": false, "properties": props, "required": required})}})
		categories[spec.name] = "workflow"
	}
	executors["get_goal_metrics"] = func(ctx context.Context, args map[string]interface{}) (string, error) {
		ledger, err := step.LoadPulseImpactLedger(ctx, stringToolArg(args, "workspace_path"), 500)
		if err != nil {
			return "", err
		}
		b, err := json.Marshal(map[string]interface{}{"metrics": ledger.Metrics, "observations": ledger.Observations, "progress": step.GoalMetricSnapshots(ledger, time.Now())})
		return string(b), err
	}
	executors["configure_goal_metrics"] = func(ctx context.Context, args map[string]interface{}) (string, error) {
		raw, err := json.Marshal(args["metrics"])
		if err != nil {
			return "", err
		}
		var metrics []step.GoalMetric
		if err = json.Unmarshal(raw, &metrics); err != nil {
			return "", fmt.Errorf("metrics must be an array of metric definitions: %w", err)
		}
		if err = step.ConfigureGoalMetrics(ctx, stringToolArg(args, "workspace_path"), metrics); err != nil {
			return "", err
		}
		return `{"status":"configured","next":"Connect collectors and verify source-backed observations; do not claim measurement is ready yet."}`, nil
	}
	executors["record_goal_observations"] = func(ctx context.Context, args map[string]interface{}) (string, error) {
		raw, err := json.Marshal(args["observations"])
		if err != nil {
			return "", err
		}
		var observations []step.PulseGoalObservation
		if err = json.Unmarshal(raw, &observations); err != nil {
			return "", fmt.Errorf("observations must be an array: %w", err)
		}
		if _, err = step.RecordGoalObservations(ctx, stringToolArg(args, "workspace_path"), observations); err != nil {
			return "", err
		}
		return `{"status":"recorded"}`, nil
	}
	return tools, executors, categories
}

func loadGoalProgressNotificationSections(ctx context.Context, workspacePath string) ([]services.NotificationSummarySection, error) {
	ledger, err := step.LoadPulseImpactLedger(ctx, workspacePath, 500)
	if err != nil {
		return nil, err
	}
	if len(ledger.Metrics) == 0 {
		return []services.NotificationSummarySection{{Heading: "Goal progress", Body: "Measurement setup needed. Use /setup-goals to choose a primary metric and connect collection."}}, nil
	}
	primary := []string{}
	supporting := []string{}
	for _, p := range step.GoalMetricSnapshots(ledger, time.Now()) {
		line := p.Metric.Name + ": " + p.State
		if p.Value != nil {
			line = fmt.Sprintf("%s: %g %s — %s", p.Metric.Name, *p.Value, p.Metric.Unit, p.State)
		}
		line += " · " + p.Metric.Window
		if p.Change != nil {
			line += fmt.Sprintf(" · change %+.2f %s since previous measurement", *p.Change, p.Metric.Unit)
		}
		if p.Metric.Target != nil {
			line += fmt.Sprintf(" · target %g %s (%s)", *p.Metric.Target, p.Metric.Unit, p.Metric.Direction)
			if p.Metric.TargetDate != "" {
				line += " by " + p.Metric.TargetDate
			}
		}
		if p.ObservedAt != "" {
			line += " · observed " + p.ObservedAt
		}
		if p.Metric.Role == "primary" {
			primary = append(primary, line)
		} else {
			supporting = append(supporting, line)
		}
	}
	sections := []services.NotificationSummarySection{{Heading: "Goal progress", Body: strings.Join(primary, "\n")}}
	if len(supporting) > 0 {
		sections = append(sections, services.NotificationSummarySection{Heading: "Supporting metrics", Body: strings.Join(supporting, "\n")})
	}
	return sections, nil
}
