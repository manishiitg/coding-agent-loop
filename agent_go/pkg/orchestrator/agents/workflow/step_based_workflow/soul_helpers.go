package step_based_workflow

import (
	"context"
	"encoding/json"
	"strings"
)

// Soul.md is the canonical source of truth for the workflow's objective and
// success criteria. The builder writes it directly via shell (no dedicated tool)
// and runtime consumers — server template vars, orchestrator hcpo.SetObjective,
// learning prompt injection — read from here, not from plan.json.
//
// Required structural convention:
//
//	# <Workflow display name>
//
//	## Objective
//	<one-paragraph statement of what the workflow is for>
//
//	## Success Criteria
//	<bullet list or paragraph describing when the workflow is "done right">
//
//	## Why  (optional — narrative context)
//	## Decisions & Constraints  (optional — decision log)
//	## Key References  (optional — links to related workflows/docs)
//
// Extra H2 sections are allowed and ignored by the extractor. Section order is
// not significant, but `## Objective` and `## Success Criteria` MUST exist for
// the workflow to be considered "ready to optimize" — see the ready-to-optimize
// slash command for the readiness check.

const (
	soulObjectiveSection        = "Objective"
	soulSuccessCriteriaSection  = "Success Criteria"
	soulDefaultScaffoldTemplate = `# %s

## Objective
<TODO: one-paragraph statement of what this workflow is for.>

## Success Criteria
<TODO: bullet list or paragraph describing when the workflow is "done right".>

## Why
<TODO: narrative context — who asked for this, what problem prompted it, what business outcome it supports.>

## Decisions & Constraints
<TODO: decision log — what was considered, what was chosen, what was ruled out and why.>

## Key References
<TODO: links to related workflows, docs, Slack threads, Linear tickets.>
`
)

// ReadWorkflowObjectiveFromSoul loads soul/soul.md and extracts the Objective and
// Success Criteria sections. Missing file or missing sections return empty strings
// (not errors) so callers can treat "not yet written" as a valid intermediate state.
// A true I/O failure (network, permission) returns the error.
func ReadWorkflowObjectiveFromSoul(
	ctx context.Context,
	workspacePath string,
	readFile func(context.Context, string) (string, error),
) (objective, successCriteria string, err error) {
	path := normalizePathForWorkspaceAPI(SoulFolderName+"/"+SoulFileName, workspacePath)
	content, readErr := readFile(ctx, path)
	if readErr != nil {
		// "file not found" is expected for workflows that haven't scaffolded soul.md yet.
		lower := strings.ToLower(readErr.Error())
		if strings.Contains(lower, "not found") || strings.Contains(lower, "no such file") {
			return "", "", nil
		}
		return "", "", readErr
	}
	return extractSoulSection(content, soulObjectiveSection),
		extractSoulSection(content, soulSuccessCriteriaSection),
		nil
}

// extractSoulSection returns the body of `## <heading>` until the next H2 or EOF.
// Heading match is case-insensitive on trimmed content. Returns empty string if
// the heading is absent. Any leading/trailing blank lines in the body are trimmed.
// Nested H3+ headings within the section are preserved verbatim in the body.
func extractSoulSection(markdown, heading string) string {
	if markdown == "" {
		return ""
	}
	target := strings.ToLower(strings.TrimSpace(heading))
	var collected []string
	inSection := false
	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		// H2 detection: `## Foo` but NOT `### Foo`
		isH2 := strings.HasPrefix(trimmed, "## ") && !strings.HasPrefix(trimmed, "### ")
		if isH2 {
			if inSection {
				// Next H2 ends the section we're collecting.
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

// SoulScaffold returns a default soul.md body for a brand-new workflow with the
// given display label. Used by workflow_creator_tool to seed the file so the
// builder has an obvious structure to fill in.
func SoulScaffold(workflowLabel string) string {
	label := strings.TrimSpace(workflowLabel)
	if label == "" {
		label = "Workflow"
	}
	return stringsReplace(soulDefaultScaffoldTemplate, "%s", label, 1)
}

// stringsReplace wraps strings.Replace with a bounded count — kept as a tiny
// helper so the template above can use %s-style substitution without pulling in
// fmt (which formats differently for markdown-bearing templates).
func stringsReplace(s, old, new string, n int) string {
	return strings.Replace(s, old, new, n)
}

// ResolveWorkflowObjective is the canonical accessor for the workflow's objective
// and success criteria. All runtime consumers — server template vars, learning
// prompt injection, readiness checks — MUST go through this rather than reading
// plan.Objective / plan.SuccessCriteria directly.
//
// Resolution order:
//  1. soul/soul.md `## Objective` and `## Success Criteria` sections (canonical).
//  2. workflow.json `objective` / `success_criteria` (legacy fallback; pre-dated
//     soul.md and is still written by the workflow creator for new workflows).
//
// Scaffold placeholders (`<TODO: ...>`) are treated as empty so a freshly
// created soul.md doesn't leak literal TODO text into prompts.
func (hcpo *StepBasedWorkflowOrchestrator) ResolveWorkflowObjective(ctx context.Context) (objective, successCriteria string) {
	soulObj, soulSC, err := ReadWorkflowObjectiveFromSoul(ctx, hcpo.GetWorkspacePath(), hcpo.ReadWorkspaceFile)
	if err == nil {
		objective = stripSoulTodoPlaceholder(soulObj)
		successCriteria = stripSoulTodoPlaceholder(soulSC)
	}
	if objective != "" && successCriteria != "" {
		return objective, successCriteria
	}
	// Legacy fallback: workflow.json root-level objective/success_criteria.
	manifest, err := hcpo.ReadWorkspaceFile(ctx, "workflow.json")
	if err != nil {
		return objective, successCriteria
	}
	var wf struct {
		Objective       string `json:"objective"`
		SuccessCriteria string `json:"success_criteria"`
	}
	if json.Unmarshal([]byte(manifest), &wf) != nil {
		return objective, successCriteria
	}
	if objective == "" {
		objective = strings.TrimSpace(wf.Objective)
	}
	if successCriteria == "" {
		successCriteria = strings.TrimSpace(wf.SuccessCriteria)
	}
	return objective, successCriteria
}

// stripSoulTodoPlaceholder treats scaffolded `<TODO: ...>` single-line placeholders
// as empty. Multi-line content that happens to mention TODO is preserved.
func stripSoulTodoPlaceholder(v string) string {
	t := strings.TrimSpace(v)
	if !strings.Contains(t, "\n") && strings.HasPrefix(t, "<TODO:") && strings.HasSuffix(t, ">") {
		return ""
	}
	return t
}
