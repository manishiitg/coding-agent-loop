package server

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"
)

// =====================================================================
// decisions_log.go — append-only structured decision entries in builder/improve.md.
// Schemas: schemas/auto-improvement.schema.json#$defs/DecisionEntry
// =====================================================================

const improveDecisionFence = "improve-decision"

// workflowImprovePath returns the canonical path to builder/improve.md for a
// given workflow workspace path. improve.md is the single source of truth for
// auto-improvement narrative and structured decisions.
func workflowImprovePath(workspacePath string) string {
	return path.Join(strings.Trim(workspacePath, "/"), "builder", "improve.md")
}

// nextDecisionID generates a stable id for a new decision entry.
// Format: dec-<workflowSlug>-<YYYYMMDD>-<sequence>.
func nextDecisionID(ctx context.Context, workspacePath string) (string, error) {
	slug := workflowSlugFromPath(workspacePath)
	today := time.Now().UTC().Format("20060102")
	prefix := fmt.Sprintf("dec-%s-%s-", slug, today)

	content, exists, err := readFileFromWorkspace(ctx, workflowImprovePath(workspacePath))
	if err != nil && exists {
		return "", err
	}
	maxSeq := 0
	for _, line := range strings.Split(content, "\n") {
		// Cheap scan: search for `"id":"<prefix>NNN"` substring without full unmarshal.
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

// AppendDecisionEntry appends a structured decision record to builder/improve.md. If
// `entry.ID` is empty, a new id is generated. If `entry.Ts` is empty, the
// current UTC time is used. Returns the persisted entry (with id/ts filled).
//
// Caller is responsible for honoring decision_log_mutability; this function
// only appends.
func AppendDecisionEntry(ctx context.Context, workspacePath string, entry DecisionEntry) (DecisionEntry, error) {
	if strings.TrimSpace(string(entry.Source)) == "" {
		return entry, fmt.Errorf("decision entry: source is required")
	}
	if strings.TrimSpace(entry.Trigger) == "" {
		return entry, fmt.Errorf("decision entry: trigger is required")
	}
	if entry.AppliedChanges == nil {
		entry.AppliedChanges = []string{}
	}
	if entry.Ts == "" {
		entry.Ts = nowUTC()
	}
	if entry.ID == "" {
		id, err := nextDecisionID(ctx, workspacePath)
		if err != nil {
			return entry, err
		}
		entry.ID = id
	}

	existing, exists, err := readFileFromWorkspace(ctx, workflowImprovePath(workspacePath))
	if err != nil && exists {
		return entry, err
	}
	body := strings.TrimRight(existing, "\n")
	if strings.TrimSpace(body) == "" {
		body = "# Improve Log\n"
	}

	encoded, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return entry, err
	}
	var b strings.Builder
	b.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("\n### ")
	b.WriteString(entry.Ts)
	b.WriteString(" — ")
	b.WriteString(entry.ID)
	b.WriteString(" — ")
	b.WriteString(entry.Trigger)
	b.WriteString(" — DECISION\n\n")
	b.WriteString("```")
	b.WriteString(improveDecisionFence)
	b.WriteString("\n")
	b.Write(encoded)
	b.WriteString("\n```\n\n")
	if strings.TrimSpace(entry.Rationale) != "" {
		b.WriteString("**Rationale:** ")
		b.WriteString(entry.Rationale)
		b.WriteString("\n\n")
	}
	if len(entry.AppliedChanges) > 0 {
		b.WriteString("**Applied changes:**\n")
		for _, item := range entry.AppliedChanges {
			b.WriteString("- `")
			b.WriteString(item)
			b.WriteString("`\n")
		}
		b.WriteString("\n")
	}
	if len(entry.TargetMetrics) > 0 {
		b.WriteString("**Target metrics:** ")
		for i, metric := range entry.TargetMetrics {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString("`")
			b.WriteString(metric)
			b.WriteString("`")
		}
		b.WriteString("\n")
	}

	if err := writeFileToWorkspace(ctx, workflowImprovePath(workspacePath), b.String()); err != nil {
		return entry, err
	}
	return entry, nil
}

// workflowSlugFromPath extracts a stable slug from a workspace path for use in
// generated ids. e.g. "Workflow/check-form-26as-xspaces" -> "check-form-26as-xspaces".
func workflowSlugFromPath(workspacePath string) string {
	cleaned := strings.Trim(workspacePath, "/")
	parts := strings.Split(cleaned, "/")
	if len(parts) == 0 {
		return "wf"
	}
	last := parts[len(parts)-1]
	if last == "" {
		return "wf"
	}
	// Sanitize: keep [a-z0-9-_], lowercase.
	var b strings.Builder
	for _, r := range strings.ToLower(last) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := b.String()
	if out == "" {
		return "wf"
	}
	return out
}
