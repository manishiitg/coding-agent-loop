package server

import (
	"context"
	"fmt"
	"path"
	"strings"
)

// =====================================================================
// soul_preconditions.go — gate the auto-improvement framework on soul.md.
//
// The framework's contract: experiments move metrics → metrics
// operationalize success_criteria → success_criteria are the user-facing
// outcome. If soul.md is missing or empty, there is no north star, and
// metrics defined against it are arbitrary. So before any metric can be
// added, both `## Objective` and `## Success Criteria` must hold real
// content — not the scaffold placeholders.
//
// Self-contained reader so the framework files don't pull a dependency
// on the orchestrator package; the section-extraction logic mirrors
// ReadWorkflowObjectiveFromSoul in soul_helpers.go.
// =====================================================================

// SoulPreconditions reports the populated/missing state of the two
// load-bearing sections in soul.md. Used by propose_metric to refuse
// adding metrics to a workflow that doesn't yet declare what success
// looks like.
type SoulPreconditions struct {
	SoulPath           string
	SoulExists         bool
	Objective          string
	SuccessCriteria    string
	ObjectiveOK        bool
	SuccessCriteriaOK  bool
}

// RequireSoulPreconditions returns nil iff soul.md exists and both
// sections contain real (non-placeholder, non-empty) text. The returned
// error message names exactly which section is missing so the caller can
// repair without guessing.
func RequireSoulPreconditions(ctx context.Context, workspacePath string) error {
	pre, err := ReadSoulPreconditions(ctx, workspacePath)
	if err != nil {
		return err
	}
	if !pre.SoulExists {
		return fmt.Errorf("soul/soul.md is missing — write it first with `## Objective` and `## Success Criteria` sections. Metrics without an objective are arbitrary; without success_criteria, the framework has no north star to verdict against")
	}
	if !pre.ObjectiveOK && !pre.SuccessCriteriaOK {
		return fmt.Errorf("soul/soul.md exists but `## Objective` and `## Success Criteria` are both empty or still TODO placeholders — fill them in before defining metrics")
	}
	if !pre.ObjectiveOK {
		return fmt.Errorf("soul/soul.md `## Objective` section is empty or still a TODO placeholder — fill it in before defining metrics")
	}
	if !pre.SuccessCriteriaOK {
		return fmt.Errorf("soul/soul.md `## Success Criteria` section is empty or still a TODO placeholder — fill it in before defining metrics. Success criteria are the north star metrics operationalize")
	}
	return nil
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
