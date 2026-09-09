package server

import (
	"strings"

	workflow "github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
	agentprompt "github.com/manishiitg/mcpagent/agent/prompt"
)

// buildWorkflowPhaseSystemPrompt is the complete server-side composition path.
// Builder and Run receive scoped, on-demand builder-reference skills instead
// of the generic chat's GetWorkspaceReference. Other phases retain their contract. Optional
// runtime sections (notification preferences, secret names, browser configuration)
// are passed in by the caller, so size and content tests exercise the same path.
func buildWorkflowPhaseSystemPrompt(phase string, vars map[string]string, ctx promptContext, additions ...string) (string, []string, []string, error) {
	parts := &workflowPromptParts{parts: []string{workflow.PhaseChatSystemPrompt(phase, vars)}}
	// Other phase templates keep their existing reference contract. Builder
	// (including Run) is the skill-backed surface migrated here.
	if phase != "workflow-builder" {
		parts.parts = append(parts.parts, GetWorkspaceReference(ctx.ShellRoot, ctx.PerUserChatsFolder))
	}
	if vars["IsCodeExecutionMode"] == "true" {
		parts.parts = append(parts.parts, agentprompt.GetCodeExecutionInstructions(vars["WorkspacePath"]))
	}
	ctx.IsWorkflowPhase = true
	ctx.WorkflowMode = vars["WorkshopMode"]
	included, skipped, err := assemblePromptSections(parts, ctx)
	if err != nil {
		return "", included, skipped, err
	}
	if ctx.WorkflowUIAvailable {
		parts.parts = append(parts.parts, "## Workflow views\n\nUse open_workspace_view to show the relevant report, plan, or other view, and refresh_workspace_view after changing its content. When using agent_browser to open or navigate a page for the user, call open_workspace_view(view=\"browser\") so they can watch the live browser in the right-hand pane. Open it once when beginning browser work; do not repeatedly switch away from a view the user chose. The Browser view streams automatically and does not need refresh_workspace_view after navigation. Only an applied receipt confirms the UI switched. Read builder-reference/references/workspace-views.md before choosing a view.")
	}
	for _, addition := range additions {
		if strings.TrimSpace(addition) != "" {
			parts.parts = append(parts.parts, addition)
		}
	}
	return strings.Join(parts.parts, "\n\n"), included, skipped, nil
}

type workflowPromptParts struct{ parts []string }

func (p *workflowPromptParts) AddInstructions(sections ...string) error {
	p.parts = append(p.parts, sections...)
	return nil
}
