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
