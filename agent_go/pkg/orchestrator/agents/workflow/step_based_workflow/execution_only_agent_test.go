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
		"IsScriptedMode":        "false",
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

func TestExecutionOnlyPromptRequiresDurableConcernHandoff(t *testing.T) {
	agent := &WorkflowExecutionOnlyAgent{}
	prompt := agent.executionOnlySystemPromptProcessor(map[string]string{})

	for _, want := range []string{
		"CONCERNS: <brief evidence-backed concern; include the affected artifact or operation>",
		"immediately before the STATUS line",
		"unresolved or consequential run evidence",
		"completion notification and the durable run summary",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("execution prompt missing concern handoff %q:\n%s", want, prompt)
		}
	}
}

func TestExecutionOnlyPromptUsesAbsoluteDBPathEnv(t *testing.T) {
	agent := &WorkflowExecutionOnlyAgent{}

	prompt := agent.executionOnlySystemPromptProcessor(map[string]string{
		"WorkspacePath":         "/app/workspace-docs/Workflow/test/runs/iteration-0/default/execution",
		"WorkflowRoot":          "/app/workspace-docs/Workflow/test",
		"StepExecutionPath":     "/app/workspace-docs/Workflow/test/runs/iteration-0/default/execution/step-sample",
		"StepContextOutput":     "",
		"LearningHistory":       "",
		"StepNumber":            "step-sample",
		"KnowledgebasePath":     "/app/workspace-docs/Workflow/test/knowledgebase",
		"FolderGuardReadPaths":  "/app/workspace-docs/Workflow/test/runs/iteration-0/default/execution, /app/workspace-docs/Workflow/test/db",
		"FolderGuardWritePaths": "/app/workspace-docs/Workflow/test/runs/iteration-0/default/execution/step-sample, /app/workspace-docs/Workflow/test/db",
		"IsEvaluationMode":      "false",
		"IsCodeExecutionMode":   "false",
		"IsScriptedMode":        "false",
	})

	requiredSnippets := []string{
		"Always use the absolute `$DB_PATH` env var",
		"sqlite3 \"$DB_PATH\" \"SELECT ...\"",
		"NEVER use relative `db/db.sqlite` from step code or shell",
		"persist your results to the workflow database via the absolute `$DB_PATH`",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("expected prompt to contain %q\n\nPrompt:\n%s", snippet, prompt)
		}
	}
	if strings.Contains(prompt, "sqlite3 db/db.sqlite") {
		t.Fatalf("execution prompt still teaches relative sqlite path\n\nPrompt:\n%s", prompt)
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
		"IsScriptedMode":          "false",
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
		"IsScriptedMode":        "false",
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
		"IsScriptedMode":      "false",
		"LearningHistory":     "Use the legacy selector.",
	})
	if !strings.Contains(userPrompt, "Read **Skill files** as guidance only") ||
		!strings.Contains(userPrompt, "The current step description is the main source of truth") {
		t.Fatalf("expected user prompt to treat skill as advisory\n\nPrompt:\n%s", userPrompt)
	}
}
