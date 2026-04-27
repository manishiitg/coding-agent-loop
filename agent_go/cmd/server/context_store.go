package server

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"
)

// =====================================================================
// context_store.go — readers/writers for context/ store (Type 3 only).
//   context/rules.md
//   context/examples/*
//   context/clarifications.jsonl
//
// Schemas: schemas/auto-improvement.schema.json#$defs/ClarificationEntry
// =====================================================================

func contextRulesPath(workspacePath string) string {
	return path.Join(strings.Trim(workspacePath, "/"), "context", "rules.md")
}

func contextClarificationsPath(workspacePath string) string {
	return path.Join(strings.Trim(workspacePath, "/"), "context", "clarifications.jsonl")
}

// ReadContextRules returns the contents of context/rules.md (or empty string).
func ReadContextRules(ctx context.Context, workspacePath string) (string, bool, error) {
	return readFileFromWorkspace(ctx, contextRulesPath(workspacePath))
}

// AppendContextRule adds a new rule under the given (optional) section to
// context/rules.md. If the section does not exist it is created at the bottom.
// Section is just a "## <name>" markdown heading.
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
			"Each rule lands as a bullet under a section heading. The agent reads this on every run.\n\n"
	}

	body := existing
	section = strings.TrimSpace(section)
	if section == "" {
		section = "General"
	}
	heading := "## " + section
	bullet := "- " + ruleText + "\n"

	if strings.Contains(body, heading) {
		// Insert bullet at end of that section. Find the next heading or EOF.
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

// nextClarificationID generates a stable id for a new clarification entry.
// Format: clar-<workflowSlug>-<YYYYMMDD>-<sequence>.
func nextClarificationID(ctx context.Context, workspacePath string) (string, error) {
	slug := workflowSlugFromPath(workspacePath)
	today := time.Now().UTC().Format("20060102")
	prefix := fmt.Sprintf("clar-%s-%s-", slug, today)

	lines, _, err := readJSONLLines(ctx, contextClarificationsPath(workspacePath))
	if err != nil {
		return "", err
	}
	maxSeq := 0
	for _, line := range lines {
		idx := strings.Index(line, prefix)
		if idx < 0 {
			continue
		}
		tail := line[idx+len(prefix):]
		end := strings.IndexByte(tail, '"')
		if end < 0 {
			continue
		}
		seq := 0
		for _, r := range tail[:end] {
			if r < '0' || r > '9' {
				seq = 0
				break
			}
			seq = seq*10 + int(r-'0')
		}
		if seq > maxSeq {
			maxSeq = seq
		}
	}
	return fmt.Sprintf("%s%03d", prefix, maxSeq+1), nil
}

// AppendClarificationEntry appends a Type-3 clarification record. Enforces:
//   - source must be "user"
//   - target_metrics must be non-empty
func AppendClarificationEntry(ctx context.Context, workspacePath string, entry ClarificationEntry) (ClarificationEntry, error) {
	if entry.Source != "user" {
		return entry, fmt.Errorf("clarification source must be \"user\"")
	}
	if len(entry.TargetMetrics) == 0 {
		return entry, fmt.Errorf("clarification target_metrics is required and must contain at least one metric id")
	}
	if strings.TrimSpace(entry.Trigger) == "" {
		entry.Trigger = "capture-context"
	}
	if entry.AppliedChanges == nil {
		entry.AppliedChanges = []string{}
	}
	if entry.Ts == "" {
		entry.Ts = nowUTC()
	}
	if entry.ID == "" {
		id, err := nextClarificationID(ctx, workspacePath)
		if err != nil {
			return entry, err
		}
		entry.ID = id
	}

	if _, err := appendJSONLRecord(ctx, contextClarificationsPath(workspacePath), entry); err != nil {
		return entry, err
	}
	return entry, nil
}

// ReadClarificationEntries returns all clarifications in context/clarifications.jsonl.
func ReadClarificationEntries(ctx context.Context, workspacePath string) ([]ClarificationEntry, error) {
	return readJSONLRecords[ClarificationEntry](ctx, contextClarificationsPath(workspacePath))
}

// CaptureContext is the high-level helper used by the capture_context tool
// and the /api/workflow/capture-context endpoint. It (a) appends the rule
// text to context/rules.md, (b) writes the clarifications.jsonl entry,
// (c) writes a builder/decisions.jsonl audit record cross-linking
// everything. Returns the persisted clarification.
//
// Non-empty target_metrics is the only mandatory validation gate from the
// framework's "Type 3 must declare target metric" rule. Caller is responsible
// for verifying the workflow is actually Type 3.
func CaptureContext(ctx context.Context, workspacePath, section, ruleText string, targetMetrics []string, exampleNote string) (ClarificationEntry, DecisionEntry, error) {
	if len(targetMetrics) == 0 {
		return ClarificationEntry{}, DecisionEntry{}, fmt.Errorf("capture_context requires non-empty target_metrics")
	}
	if strings.TrimSpace(ruleText) == "" {
		return ClarificationEntry{}, DecisionEntry{}, fmt.Errorf("capture_context requires rule text")
	}

	if err := AppendContextRule(ctx, workspacePath, section, ruleText); err != nil {
		return ClarificationEntry{}, DecisionEntry{}, fmt.Errorf("append rule: %w", err)
	}

	clar := ClarificationEntry{
		Source:         "user",
		Trigger:        "capture-context",
		RuleAdded:      ruleText,
		RuleSection:    section,
		Clarification:  exampleNote,
		AppliedChanges: []string{"context/rules.md"},
		TargetMetrics:  targetMetrics,
	}
	persistedClar, err := AppendClarificationEntry(ctx, workspacePath, clar)
	if err != nil {
		return clar, DecisionEntry{}, fmt.Errorf("append clarification: %w", err)
	}

	dec := DecisionEntry{
		Source:         DecisionSourceUser,
		Trigger:        "capture-context",
		Rationale:      fmt.Sprintf("user-supplied rule: %s", truncate(ruleText, 120)),
		AppliedChanges: []string{"context/rules.md", "context/clarifications.jsonl"},
		TargetMetrics:  targetMetrics,
	}
	persistedDec, err := AppendDecisionEntry(ctx, workspacePath, dec)
	if err != nil {
		return persistedClar, dec, fmt.Errorf("append decision: %w", err)
	}

	return persistedClar, persistedDec, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
