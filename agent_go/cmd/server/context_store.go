package server

import (
	"context"
	"fmt"
	"path"
	"strings"
)

// =====================================================================
// context_store.go — readers/writers for user-supplied workflow context.
//
//   <workflow>/knowledgebase/context/context.md
//   <workflow>/knowledgebase/context/examples/
//
// This helper only writes context.md. It does not touch the improvement
// ledger — the agent narrates context captures into builder/improve.html on
// its turn.
//
// `knowledgebase/context/` is intentionally excluded from
// reorganize_knowledgebase / consolidate_knowledgebase passes — user-supplied
// content is never silently rewritten by the optimizer.
// =====================================================================

func contextRulesPath(workspacePath string) string {
	return path.Join(strings.Trim(workspacePath, "/"), "knowledgebase", "context", "context.md")
}

func legacyContextRulesPath(workspacePath string) string {
	return path.Join(strings.Trim(workspacePath, "/"), "knowledgebase", "rules", "rules.md")
}

// ReadContextRules returns the contents of knowledgebase/context/context.md.
func ReadContextRules(ctx context.Context, workspacePath string) (string, bool, error) {
	contents, exists, err := readFileFromWorkspace(ctx, contextRulesPath(workspacePath))
	if err != nil || exists {
		return contents, exists, err
	}
	return readFileFromWorkspace(ctx, legacyContextRulesPath(workspacePath))
}

// AppendContextRule adds a new rule under the given (optional) section to
// knowledgebase/context/context.md. If the section does not exist it is created.
func AppendContextRule(ctx context.Context, workspacePath, section, ruleText string) error {
	ruleText = strings.TrimSpace(ruleText)
	if ruleText == "" {
		return fmt.Errorf("context text is empty")
	}

	existing, exists, err := ReadContextRules(ctx, workspacePath)
	if err != nil {
		return err
	}
	if !exists {
		existing = "# Workflow Context\n\n" +
			"This file accumulates runtime business context supplied by the user: rules, preferences, constraints, assumptions, and examples that future workflow steps must respect. " +
			"Context is persisted via the `capture_context` tool (the builder agent recognizes durable runtime context in conversation and offers to capture). " +
			"Each captured item lands as a bullet under a section heading. " +
			"Steps with `knowledgebase_access` set to `read` (or `read-write`) automatically see this file at runtime — " +
			"the context folder is a sub-section of the knowledgebase.\n\n" +
			"This file is **excluded** from `reorganize_knowledgebase` and `consolidate_knowledgebase` passes — " +
			"user-supplied content is never silently rewritten by the optimizer.\n\n"
	}

	body := existing
	section = strings.TrimSpace(section)
	if section == "" {
		section = "General"
	}
	heading := "## " + section
	bullet := "- " + ruleText + "\n"

	if strings.Contains(body, heading) {
		idx := strings.Index(body, heading)
		searchFrom := idx + len(heading)
		nextIdx := strings.Index(body[searchFrom:], "\n## ")
		insertAt := len(body)
		if nextIdx >= 0 {
			insertAt = searchFrom + nextIdx
		}
		body = body[:insertAt] + "\n" + bullet + body[insertAt:]
	} else {
		if !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		body += "\n" + heading + "\n\n" + bullet
	}

	return writeFileToWorkspace(ctx, contextRulesPath(workspacePath), body)
}

// CaptureContext is the high-level helper used by the capture_context tool.
// It appends the rule text to knowledgebase/context/context.md and returns a
// summary of what was written (section, target metrics, applied changes). It
// does NOT write to the improvement ledger — the agent narrates context
// captures into builder/improve.html on its turn.
//
// Non-empty target_metrics is the mandatory validation gate for
// business-context capture: every persisted rule must declare what metric(s)
// it is meant to move. Caller is responsible for verifying the workflow
// profile actually allows business-context accumulation.
func CaptureContext(ctx context.Context, workspacePath, section, ruleText string, targetMetrics []string, exampleNote string) (DecisionEntry, error) {
	if len(targetMetrics) == 0 {
		return DecisionEntry{}, fmt.Errorf("capture_context requires non-empty target_metrics")
	}
	if strings.TrimSpace(ruleText) == "" {
		return DecisionEntry{}, fmt.Errorf("capture_context requires context text")
	}
	if err := validateContextTargetMetrics(ctx, workspacePath, targetMetrics); err != nil {
		return DecisionEntry{}, err
	}
	section = strings.TrimSpace(section)
	if section == "" {
		section = "General"
	}

	if err := AppendContextRule(ctx, workspacePath, section, ruleText); err != nil {
		return DecisionEntry{}, fmt.Errorf("append context: %w", err)
	}

	rationale := fmt.Sprintf("user-supplied context: %s", truncate(ruleText, 120))
	if exampleNote != "" {
		rationale = fmt.Sprintf("%s — note: %s", rationale, truncate(exampleNote, 80))
	}
	dec := DecisionEntry{
		Source:         DecisionSourceUser,
		Trigger:        "capture-context",
		Rationale:      rationale,
		AppliedChanges: []string{"knowledgebase/context/context.md"},
		TargetMetrics:  targetMetrics,
		RuleAdded:      ruleText,
		RuleSection:    section,
	}
	return dec, nil
}

type CaptureContextInput struct {
	Section       string   `json:"section,omitempty"`
	ContextText   string   `json:"context_text,omitempty"`
	TargetMetrics []string `json:"target_metrics,omitempty"`
	ExampleNote   string   `json:"example_note,omitempty"`
}

type CaptureContextOutput struct {
	DecisionID     string   `json:"decision_id,omitempty"`
	Status         string   `json:"status"`
	Section        string   `json:"section,omitempty"`
	TargetMetrics  []string `json:"target_metrics,omitempty"`
	AppliedChanges []string `json:"applied_changes,omitempty"`
}

func CaptureContextTool(ctx context.Context, workspacePath string, input CaptureContextInput) (*CaptureContextOutput, error) {
	decision, err := CaptureContext(ctx, workspacePath, input.Section, input.ContextText, input.TargetMetrics, input.ExampleNote)
	if err != nil {
		return nil, err
	}
	return &CaptureContextOutput{
		DecisionID:     decision.ID,
		Status:         "captured",
		Section:        decision.RuleSection,
		TargetMetrics:  decision.TargetMetrics,
		AppliedChanges: decision.AppliedChanges,
	}, nil
}

func validateContextTargetMetrics(ctx context.Context, workspacePath string, targetMetrics []string) error {
	file, exists, err := ReadMetricsFile(ctx, workspacePath)
	if err != nil {
		return fmt.Errorf("read metrics.json: %w", err)
	}
	if !exists || file == nil || len(file.Metrics) == 0 {
		return fmt.Errorf("capture_context requires existing planning/metrics.json metrics; run /define-success before capturing context")
	}
	active := make(map[string]struct{}, len(file.Metrics))
	for _, metric := range file.Metrics {
		id := strings.TrimSpace(metric.ID)
		if id != "" {
			active[id] = struct{}{}
		}
	}
	var missing []string
	for _, id := range targetMetrics {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			missing = append(missing, "<empty>")
			continue
		}
		if _, ok := active[trimmed]; !ok {
			missing = append(missing, trimmed)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("capture_context target_metrics not found in active metrics: %s", strings.Join(missing, ", "))
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
