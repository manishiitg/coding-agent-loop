package step_based_workflow

import (
	"strings"
	"testing"
)

func TestExecutionOnlyPromptIncludesCodeExecutionInstructions(t *testing.T) {
	agent := &WorkflowExecutionOnlyAgent{}

	prompt := agent.executionOnlySystemPromptProcessor(map[string]string{
		"WorkspacePath":         "/app/workspace-docs/Workflow/test/runs/iteration-0/default/execution",
		"WorkflowRoot":          "/app/workspace-docs/Workflow/test",
		"StepExecutionPath":     "/app/workspace-docs/Workflow/test/runs/iteration-0/default/execution/step-sample",
		"StepContextOutput":     "output.json",
		"LearningHistory":       "",
		"KeepLearningFull":      "false",
		"StepNumber":            "step-sample",
		"PreviousStepsSummary":  "",
		"KnowledgebasePath":     "/app/workspace-docs/Workflow/test/knowledgebase",
		"UseKnowledgebase":      "true",
		"FolderGuardReadPaths":  "/app/workspace-docs/Workflow/test/runs/iteration-0/default/execution",
		"FolderGuardWritePaths": "/app/workspace-docs/Workflow/test/runs/iteration-0/default/execution/step-sample",
		"IsEvaluationMode":      "false",
		"IsCodeExecutionMode":   "true",
		"IsLearnCodeMode":       "false",
	})

	requiredSnippets := []string{
		"CODE EXECUTION MODE",
		"Derive output paths from `os.environ['STEP_OUTPUT_DIR']` in code.",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("expected prompt to contain %q\n\nPrompt:\n%s", snippet, prompt)
		}
	}
}

func TestExecutionOnlyUserPromptIncludesWorkshopHumanInput(t *testing.T) {
	agent := &WorkflowExecutionOnlyAgent{}

	prompt := agent.executionOnlyUserMessageProcessor(map[string]string{
		"StepTitle":               "Write back to Notion",
		"StepDescription":         "Publish the RCA.",
		"StepContextDependencies": "",
		"StepContextOutput":       "notion_writeback.json",
		"WorkspacePath":           "/app/workspace-docs/Workflow/test/runs/iteration-0/default/execution",
		"StepExecutionPath":       "/app/workspace-docs/Workflow/test/runs/iteration-0/default/execution/step-writeback",
		"IsCodeExecutionMode":     "true",
		"IsLearnCodeMode":         "false",
		"WorkshopHumanInput":      "post RCA q-20260509T124321-rslat as wiki page",
	})

	requiredSnippets := []string{
		"## Human Input (Highest Priority)",
		"execute_step(..., human_input=...)",
		"post RCA q-20260509T124321-rslat as wiki page",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("expected prompt to contain %q\n\nPrompt:\n%s", snippet, prompt)
		}
	}
}

func TestExecutionOnlyPromptsTreatSkillAsAdvisory(t *testing.T) {
	agent := &WorkflowExecutionOnlyAgent{}

	systemPrompt := agent.executionOnlySystemPromptProcessor(map[string]string{
		"WorkspacePath":         "/app/workspace-docs/Workflow/test/runs/iteration-0/default/execution",
		"WorkflowRoot":          "/app/workspace-docs/Workflow/test",
		"StepExecutionPath":     "/app/workspace-docs/Workflow/test/runs/iteration-0/default/execution/step-sample",
		"StepContextOutput":     "output.json",
		"LearningHistory":       "Use the legacy selector.",
		"StepNumber":            "step-sample",
		"KnowledgebasePath":     "/app/workspace-docs/Workflow/test/knowledgebase",
		"FolderGuardReadPaths":  "/app/workspace-docs/Workflow/test/runs/iteration-0/default/execution, /app/workspace-docs/Workflow/test/learnings/_global",
		"FolderGuardWritePaths": "/app/workspace-docs/Workflow/test/runs/iteration-0/default/execution/step-sample",
		"IsEvaluationMode":      "false",
		"IsCodeExecutionMode":   "false",
		"IsLearnCodeMode":       "false",
	})
	systemSnippets := []string{
		"Treat learnings/skill content as advisory guidance from previous runs",
		"the current step description, orchestrator instructions, and human input are the source of truth",
		"Skill content is guidance from previous runs, not a replacement for the current task",
	}
	for _, snippet := range systemSnippets {
		if !strings.Contains(systemPrompt, snippet) {
			t.Fatalf("expected system prompt to contain %q\n\nPrompt:\n%s", snippet, systemPrompt)
		}
	}

	userPrompt := agent.executionOnlyUserMessageProcessor(map[string]string{
		"StepTitle":           "Fetch report",
		"StepDescription":     "Use the current report endpoint.",
		"StepContextOutput":   "output.json",
		"StepExecutionPath":   "/app/workspace-docs/Workflow/test/runs/iteration-0/default/execution/step-fetch",
		"IsCodeExecutionMode": "false",
		"IsLearnCodeMode":     "false",
		"LearningHistory":     "Use the legacy selector.",
	})
	if !strings.Contains(userPrompt, "Read **Skill files** as guidance only") ||
		!strings.Contains(userPrompt, "The current step description is the main source of truth") {
		t.Fatalf("expected user prompt to treat skill as advisory\n\nPrompt:\n%s", userPrompt)
	}
}
