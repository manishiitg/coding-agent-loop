package server

import (
	"context"
	"fmt"
	"path"
	"strings"
)

// =====================================================================
// soul_preconditions.go — read-only soul.md health signal.
//
// The framework's contract: metrics operationalize success_criteria, and
// success_criteria are the user-facing outcome. We surface whether soul.md has
// a real objective + success criteria as a non-blocking signal (the
// framework-health banner / goal card); defining the soul before metrics is the
// agent's responsibility, not a hard gate. The section-extraction logic mirrors
// ReadWorkflowObjectiveFromSoul in soul_helpers.go.
// =====================================================================

// SoulPreconditions reports the populated/missing state of the two
// load-bearing sections in soul.md, for read-only health surfaces.
type SoulPreconditions struct {
	SoulPath          string
	SoulExists        bool
	Objective         string
	SuccessCriteria   string
	ObjectiveOK       bool
	SuccessCriteriaOK bool
}

// ReadSoulPreconditions returns the parsed soul.md state without enforcing
// it. Used by the UI / readiness check / debug surfaces.
func ReadSoulPreconditions(ctx context.Context, workspacePath string) (*SoulPreconditions, error) {
	soulPath := path.Join(strings.Trim(workspacePath, "/"), "soul", "soul.md")
	pre := &SoulPreconditions{SoulPath: soulPath}

	content, exists, err := readFileFromWorkspace(ctx, soulPath)
	if err != nil {
		return nil, fmt.Errorf("read soul.md: %w", err)
	}
	pre.SoulExists = exists
	if !exists {
		return pre, nil
	}

	pre.Objective = extractSoulH2(content, "Objective")
	pre.SuccessCriteria = extractSoulH2(content, "Success Criteria")
	pre.ObjectiveOK = soulSectionPopulated(pre.Objective)
	pre.SuccessCriteriaOK = soulSectionPopulated(pre.SuccessCriteria)
	return pre, nil
}

// extractSoulH2 returns the body of `## <heading>` until the next H2 or
// EOF, with surrounding blank lines trimmed. Mirrors the orchestrator's
// extractor so behavior is consistent across packages.
func extractSoulH2(markdown, heading string) string {
	if markdown == "" {
		return ""
	}
	target := strings.ToLower(strings.TrimSpace(heading))
	var collected []string
	inSection := false
	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		isH2 := strings.HasPrefix(trimmed, "## ") && !strings.HasPrefix(trimmed, "### ")
		if isH2 {
			if inSection {
				break
			}
			title := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "##")))
			if title == target {
				inSection = true
			}
			continue
		}
		if inSection {
			collected = append(collected, line)
		}
	}
	return strings.TrimSpace(strings.Join(collected, "\n"))
}

// soulSectionPopulated returns true iff the body has real content — not
// empty, not just whitespace, not the scaffold's `<TODO: …>` placeholder.
// A body with both a TODO line AND real content is considered populated;
// the user has started filling it in.
func soulSectionPopulated(body string) bool {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return false
	}
	// Strip TODO placeholder lines and check whether anything substantive remains.
	var keep []string
	for _, line := range strings.Split(trimmed, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "<TODO:") && strings.HasSuffix(t, ">") {
			continue
		}
		keep = append(keep, t)
	}
	return len(keep) > 0
}
