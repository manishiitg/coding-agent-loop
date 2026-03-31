package step_based_workflow

import (
	"strings"
	"testing"
)

func TestExecutionOnlyPromptUsesBridgeBackedToolSearchGuidanceForCLIToolSearchSteps(t *testing.T) {
	agent := &WorkflowExecutionOnlyAgent{}

	prompt := agent.executionOnlySystemPromptProcessor(map[string]string{
		"WorkspacePath":         "/app/workspace-docs/Workflow/confida-oi/runs/iteration-0/default/execution",
		"WorkflowRoot":          "/app/workspace-docs/Workflow/confida-oi",
		"StepExecutionPath":     "/app/workspace-docs/Workflow/confida-oi/runs/iteration-0/default/execution/step-sample",
		"StepContextOutput":     "output.json",
		"LearningHistory":       "",
		"KeepLearningFull":      "false",
		"StepNumber":            "step-sample",
		"PreviousStepsSummary":  "",
		"KnowledgebasePath":     "/app/workspace-docs/Workflow/confida-oi/knowledgebase",
		"UseKnowledgebase":      "true",
		"FolderGuardReadPaths":  "/app/workspace-docs/Workflow/confida-oi/runs/iteration-0/default/execution",
		"FolderGuardWritePaths": "/app/workspace-docs/Workflow/confida-oi/runs/iteration-0/default/execution/step-sample",
		"SkipExecutionCleanup":  "false",
		"IsEvaluationMode":      "false",
		"IsCodeExecutionMode":   "true",
		"UseToolSearchMode":     "true",
		"IsLearnCodeMode":       "false",
	})

	requiredSnippets := []string{
		"Bridge-Backed Tool Search Mode",
		"This step is logically in **tool_search** mode",
		"Do **not** create or rely on a reusable `main.py` unless the step is explicitly in scripted code mode.",
		"Use absolute paths in shell commands.",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("expected prompt to contain %q\n\nPrompt:\n%s", snippet, prompt)
		}
	}

	forbiddenSnippets := []string{
		"## Python Best Practices",
		"Use absolute paths in code.",
	}
	for _, snippet := range forbiddenSnippets {
		if strings.Contains(prompt, snippet) {
			t.Fatalf("expected prompt to omit %q\n\nPrompt:\n%s", snippet, prompt)
		}
	}
}

func TestIsScriptedExecutionModeConfigFalseForToolSearchEvenIfCodeExecTransportIsEnabled(t *testing.T) {
	trueVal := true

	cfg := &AgentConfigs{
		UseCodeExecutionMode: &trueVal,
		UseToolSearchMode:    &trueVal,
	}

	if isScriptedExecutionModeConfig(cfg) {
		t.Fatalf("expected tool_search config to not be treated as scripted code mode when bridge transport enables code execution")
	}
}
