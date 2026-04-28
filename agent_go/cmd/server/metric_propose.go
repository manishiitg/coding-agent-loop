package server

import (
	"context"
	"fmt"
	"strings"
)

// =====================================================================
// metric_propose.go — handler for the propose_metric tool.
//
// Versioning: an `amend_existing` request archives the prior definition into
// metrics.json::archive[] with archived_at + archived_reason, then writes the
// new definition under the same id with version+1. Chart renderers should
// break the line at the version transition.
//
// Schemas: schemas/auto-improvement.schema.json#$defs/ToolInput_propose_metric
// =====================================================================

// AmendRequest is the optional in-place amend payload on propose_metric.
type AmendRequest struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// ProposeMetricInput is the LLM-supplied portion of a metric proposal.
type ProposeMetricInput struct {
	ID                    string          `json:"id"`
	Label                 string          `json:"label,omitempty"`
	Unit                  string          `json:"unit"`
	Direction             MetricDirection `json:"direction"`
	Mode                  MetricMode      `json:"mode"`
	Target                *float64        `json:"target,omitempty"`
	Floor                 *float64        `json:"floor,omitempty"`
	Ceiling               *float64        `json:"ceiling,omitempty"`
	Source                MetricSource    `json:"source"`
	EvaluableAtLag        string          `json:"evaluable_at_lag,omitempty"`
	Parent                string          `json:"parent,omitempty"`
	LinkedSuccessCriteria []string        `json:"linked_success_criteria,omitempty"`
	AmendExisting         *AmendRequest   `json:"amend_existing,omitempty"`
}

// ProposeMetricOutput is what the tool returns to the proposer LLM.
type ProposeMetricOutput struct {
	MetricID string `json:"metric_id"`
	Version  int    `json:"version"`
	Status   string `json:"status"`
}

// ProposeMetric is the system entrypoint for adding or amending a metric.
//
// Trigger is the originating slash command name (used in the audit decision
// entry). Caller is responsible for honoring oversight gates that may require
// human approval before this commits — for v1, supervised + amend on a metric
// already used by ≥1 active rule is treated as "high-risk" but still applies
// (UI later can convert this to an awaiting-approval flow).
func ProposeMetric(ctx context.Context, workspacePath, trigger string, input ProposeMetricInput) (*ProposeMetricOutput, error) {
	// Build the candidate metric.
	candidate := Metric{
		ID:                    strings.TrimSpace(input.ID),
		Label:                 input.Label,
		Unit:                  input.Unit,
		Direction:             input.Direction,
		Mode:                  input.Mode,
		Target:                input.Target,
		Floor:                 input.Floor,
		Ceiling:               input.Ceiling,
		Source:                input.Source,
		EvaluableAtLag:        input.EvaluableAtLag,
		Parent:                input.Parent,
		LinkedSuccessCriteria: trimAndDedupe(input.LinkedSuccessCriteria),
	}
	if err := ValidateMetric(&candidate); err != nil {
		return nil, fmt.Errorf("invalid metric: %w", err)
	}

	// Load (or create) metrics file.
	file, exists, err := ReadMetricsFile(ctx, workspacePath)
	if err != nil {
		return nil, err
	}
	if !exists || file == nil {
		file = &MetricsFile{Metrics: []Metric{}, Archive: []MetricArchiveEntry{}}
	}

	status := "added"
	if input.AmendExisting != nil {
		if strings.TrimSpace(input.AmendExisting.Reason) == "" {
			return nil, fmt.Errorf("amend_existing.reason is required")
		}
		// Find existing.
		idx := -1
		for i := range file.Metrics {
			if file.Metrics[i].ID == input.AmendExisting.ID {
				idx = i
				break
			}
		}
		if idx < 0 {
			return nil, fmt.Errorf("amend_existing.id %q not found in metrics.json", input.AmendExisting.ID)
		}
		prior := file.Metrics[idx]
		// Archive prior definition.
		file.Archive = append(file.Archive, MetricArchiveEntry{
			ID:             prior.ID,
			Version:        max1(prior.Version),
			ArchivedAt:     nowUTC(),
			ArchivedReason: input.AmendExisting.Reason,
			Definition:     prior,
		})
		// Write new definition under same id, version+1.
		candidate.ID = prior.ID
		candidate.Version = max1(prior.Version) + 1
		file.Metrics[idx] = candidate
		status = "amended"
	} else {
		// New metric. Reject duplicate id.
		for _, m := range file.Metrics {
			if m.ID == candidate.ID {
				return nil, fmt.Errorf("metric id %q already exists; use amend_existing to update", candidate.ID)
			}
		}
		candidate.Version = 1
		file.Metrics = append(file.Metrics, candidate)
	}

	// Validate full file (catches parent-link issues).
	if err := ValidateMetricsFile(file); err != nil {
		return nil, fmt.Errorf("metrics.json validation: %w", err)
	}

	if err := WriteMetricsFile(ctx, workspacePath, file); err != nil {
		return nil, fmt.Errorf("write metrics.json: %w", err)
	}

	// Audit decision entry.
	dec := DecisionEntry{
		Source:         DecisionSourceAgent,
		Trigger:        trigger,
		Rationale:      fmt.Sprintf("metric %s: %s", status, candidate.ID),
		AppliedChanges: []string{"planning/metrics.json"},
		TargetMetrics:  []string{candidate.ID},
	}
	if _, err := AppendDecisionEntry(ctx, workspacePath, dec); err != nil {
		return nil, fmt.Errorf("append decision: %w", err)
	}

	return &ProposeMetricOutput{
		MetricID: candidate.ID,
		Version:  candidate.Version,
		Status:   status,
	}, nil
}

// trimAndDedupe drops empty entries and duplicates while preserving order.
// Used so linked_success_criteria stays clean even if the LLM passes
// trailing whitespace or accidentally repeats an entry.
func trimAndDedupe(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		t := strings.TrimSpace(s)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func max1(v int) int {
	if v < 1 {
		return 1
	}
	return v
}
