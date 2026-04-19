package step_based_workflow

import (
	"strings"
	"testing"
)

func TestTodoTaskOrchestratorPromptIncludesSharedCodeExecutionSection(t *testing.T) {
	agent := &WorkflowTodoTaskOrchestratorAgent{}

	prompt := agent.todoTaskOrchestratorSystemPromptProcessor(map[string]string{
		"CurrentTodos":               "",
		"ProgressSummary":            "",
		"VariableNames":              "",
		"VariableValues":             "",
		"LearningHistory":            "",
		"StepExecutionPath":          "/app/workspace-docs/Workflow/confida-oi/runs/iteration-0/default/execution/step-qa-no-redlines",
		"DownloadsPath":              "/app/workspace-docs/Workflow/confida-oi/runs/iteration-0/default/execution/Downloads",
		"ExecutionFolderPath":        "/app/workspace-docs/Workflow/confida-oi/runs/iteration-0/default/execution",
		"WorkspacePath":              "/app/workspace-docs/Workflow/confida-oi",
		"WorkflowRoot":               "/app/workspace-docs/Workflow/confida-oi",
		"KnowledgebasePath":          "/app/workspace-docs/Workflow/confida-oi/knowledgebase",
		"FolderGuardReadPaths":       "",
		"FolderGuardWritePaths":      "",
		"ShowToolsSection":           "false",
		"UseKnowledgebase":           "true",
		"IsCodeExecutionMode":        "true",
		"PreviousStepsSummary":       "",
		"StepTitle":                  "QA Scenario: No Redlines",
		"StepDescription":            "Run the no-redlines scenario.",
		"StepSuccessCriteria":        "Scenario completes successfully.",
		"HasBrowserAccess":           "true",
		"PredefinedRoutes":           "- route-nr-setup",
	})

	requiredSnippets := []string{
		"**Sub-agent tool rule**:",
		"Prefer calling these sub-agent tools directly when they are actually available as provider-callable tools in this session.",
		"**CODE EXECUTION MODE — Access MCP Tools via HTTP API:**",
		"{{TOOL_STRUCTURE}}",
		"MCP_API_URL and MCP_API_TOKEN env vars are pre-set",
		"get_api_spec(server_name=\"...\", tool_name=\"...\")",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("expected prompt to contain %q\n\nPrompt:\n%s", snippet, prompt)
		}
	}
}
