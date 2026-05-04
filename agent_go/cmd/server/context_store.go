package server

import (
	"context"
	"fmt"
	"path"
	"strings"
)

// =====================================================================
// context_store.go — readers/writers for the Type-3 business-rule store.
//
//   <workflow>/knowledgebase/rules/rules.md
//   <workflow>/knowledgebase/rules/examples/
//
// Audit trail (formerly context/clarifications.jsonl) is now folded into
// the unified builder/decisions.jsonl with Source=user + Trigger=capture-context.
//
// `knowledgebase/rules/` is intentionally excluded from
// reorganize_knowledgebase / consolidate_knowledgebase passes — user-supplied
// content is never silently rewritten by the optimizer.
// =====================================================================

func contextRulesPath(workspacePath string) string {
	return path.Join(strings.Trim(workspacePath, "/"), "knowledgebase", "rules", "rules.md")
}

// ReadContextRules returns the contents of knowledgebase/rules/rules.md.
func ReadContextRules(ctx context.Context, workspacePath string) (string, bool, error) {
	return readFileFromWorkspace(ctx, contextRulesPath(workspacePath))
}

// AppendContextRule adds a new rule under the given (optional) section to
// knowledgebase/rules/rules.md. If the section does not exist it is created.
func AppendContextRule(ctx context.Context, workspacePath, section, ruleText string) error {
	ruleText = strings.TrimSpace(ruleText)
	if ruleText == "" {
		return fmt.Errorf("rule text is empty")
	}

	existing, exists, err := ReadContextRules(ctx, workspacePath)
	if err != nil {
		return err
	}
	if !exists {
		existing = "# Workflow Rules\n\n" +
			"This file accumulates business rules supplied by the user. " +
			"Rules are persisted via the `capture_context` tool (the builder agent recognizes them in conversation and offers to capture). " +
			"Each rule lands as a bullet under a section heading. " +
			"Steps with `knowledgebase_access` set to `read` (or `read-write`) automatically see this file at runtime — " +
			"the rules folder is a sub-section of the knowledgebase.\n\n" +
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

// CaptureContext is the high-level helper used by the capture_context tool
// and the /api/workflow/capture-context endpoint. It (a) appends the rule
// text to knowledgebase/rules/rules.md, (b) writes a single
// builder/decisions.jsonl entry with Source=user + Trigger=capture-context
// + the rule-specific fields populated. Returns the persisted decision.
//
// Non-empty target_metrics is the mandatory validation gate (Type 3 must
// declare what the rule is meant to move). Caller is responsible for
// verifying the workflow is actually Type 3.
func CaptureContext(ctx context.Context, workspacePath, section, ruleText string, targetMetrics []string, exampleNote string) (DecisionEntry, error) {
	if len(targetMetrics) == 0 {
		return DecisionEntry{}, fmt.Errorf("capture_context requires non-empty target_metrics")
	}
	if strings.TrimSpace(ruleText) == "" {
		return DecisionEntry{}, fmt.Errorf("capture_context requires rule text")
	}
	section = strings.TrimSpace(section)
	if section == "" {
		section = "General"
	}

	if err := AppendContextRule(ctx, workspacePath, section, ruleText); err != nil {
		return DecisionEntry{}, fmt.Errorf("append rule: %w", err)
	}

	rationale := fmt.Sprintf("user-supplied rule: %s", truncate(ruleText, 120))
	if exampleNote != "" {
		rationale = fmt.Sprintf("%s — note: %s", rationale, truncate(exampleNote, 80))
	}
	dec := DecisionEntry{
		Source:         DecisionSourceUser,
		Trigger:        "capture-context",
		Rationale:      rationale,
		AppliedChanges: []string{"knowledgebase/rules/rules.md"},
		TargetMetrics:  targetMetrics,
		RuleAdded:      ruleText,
		RuleSection:    section,
	}
	persistedDec, err := AppendDecisionEntry(ctx, workspacePath, dec)
	if err != nil {
		return dec, fmt.Errorf("append decision: %w", err)
	}
	return persistedDec, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
