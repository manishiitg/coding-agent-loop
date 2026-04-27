package server

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"
)

// =====================================================================
// decisions_log.go — append-only writer for builder/decisions.jsonl.
// Schemas: schemas/auto-improvement.schema.json#$defs/DecisionEntry
// =====================================================================

// workflowDecisionsPath returns the canonical path to builder/decisions.jsonl
// for a given workflow workspace path.
func workflowDecisionsPath(workspacePath string) string {
	return path.Join(strings.Trim(workspacePath, "/"), "builder", "decisions.jsonl")
}

// nextDecisionID generates a stable id for a new decision entry.
// Format: dec-<workflowSlug>-<YYYYMMDD>-<sequence>.
func nextDecisionID(ctx context.Context, workspacePath string) (string, error) {
	slug := workflowSlugFromPath(workspacePath)
	today := time.Now().UTC().Format("20060102")
	prefix := fmt.Sprintf("dec-%s-%s-", slug, today)

	lines, _, err := readJSONLLines(ctx, workflowDecisionsPath(workspacePath))
	if err != nil {
		return "", err
	}
	maxSeq := 0
	for _, line := range lines {
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

// AppendDecisionEntry appends a new entry to builder/decisions.jsonl. If
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

	if _, err := appendJSONLRecord(ctx, workflowDecisionsPath(workspacePath), entry); err != nil {
		return entry, err
	}
	return entry, nil
}

// ReadDecisionEntries returns all decisions in builder/decisions.jsonl,
// in append order.
func ReadDecisionEntries(ctx context.Context, workspacePath string) ([]DecisionEntry, error) {
	return readJSONLRecords[DecisionEntry](ctx, workflowDecisionsPath(workspacePath))
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
