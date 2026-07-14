package step_based_workflow

import (
	"strings"
	"testing"
)

func TestTodoTaskOrchestratorPromptIncludesSharedCodeExecutionSection(t *testing.T) {
	agent := &WorkflowTodoTaskOrchestratorAgent{}

	prompt := agent.todoTaskOrchestratorSystemPromptProcessor(map[string]string{
		"CurrentTodos":          "",
		"ProgressSummary":       "",
		"VariableNames":         "",
		"VariableValues":        "",
		"LearningHistory":       "",
		"StepExecutionPath":     "/app/workspace-docs/Workflow/confida-oi/runs/iteration-0/default/execution/step-qa-no-redlines",
		"DownloadsPath":         "/app/workspace-docs/Workflow/confida-oi/runs/iteration-0/default/execution/Downloads",
		"ExecutionFolderPath":   "/app/workspace-docs/Workflow/confida-oi/runs/iteration-0/default/execution",
		"WorkspacePath":         "/app/workspace-docs/Workflow/confida-oi",
		"WorkflowRoot":          "/app/workspace-docs/Workflow/confida-oi",
		"KnowledgebasePath":     "/app/workspace-docs/Workflow/confida-oi/knowledgebase",
		"FolderGuardReadPaths":  "",
		"FolderGuardWritePaths": "",
		"ShowToolsSection":      "false",
		"UseKnowledgebase":      "true",
		"IsCodeExecutionMode":   "true",
		"PreviousStepsSummary":  "",
		"StepTitle":             "QA Scenario: No Redlines",
		"StepDescription":       "Run the no-redlines scenario.",
		"StepSuccessCriteria":   "Scenario completes successfully.",
		"HasBrowserAccess":      "true",
		"PredefinedRoutes":      "- route-nr-setup",
	})

	requiredSnippets := []string{
		"**Sub-agent tool rule**:",
		"Prefer calling these sub-agent tools directly only when they are actually listed as provider-callable tools in this session.",
		"In bridge-only CLI sessions where only the documented api-bridge tools are native, sub-agent tools are dynamic custom tools:",
		"**CODE EXECUTION MODE — Access MCP Tools via HTTP API:**",
		"{{TOOL_STRUCTURE}}",
		"MCP_CUSTOM and MCP_AUTH",
		"get_api_spec(server_name=\"...\", tool_name=\"...\")",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("expected prompt to contain %q\n\nPrompt:\n%s", snippet, prompt)
		}
	}
}

func TestTodoTaskOrchestratorPromptDocumentsMessageSequenceRoutes(t *testing.T) {
	agent := &WorkflowTodoTaskOrchestratorAgent{}

	prompt := agent.todoTaskOrchestratorSystemPromptProcessor(map[string]string{
		"ShowToolsSection":    "true",
		"IsCodeExecutionMode": "false",
		"PredefinedRoutes":    "- route-sequence",
	})

	requiredSnippets := []string{
		"**Message sequence routes**:",
		"Step type: message_sequence",
		"First call starts the route conversation",
		"instructions are added as initial context",
		"Later calls to the same route resume",
		"instructions become the re-entry user message",
		"message_sequence_restart=true",
		"configured queue is replayed from the beginning",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("expected prompt to contain %q\n\nPrompt:\n%s", snippet, prompt)
		}
	}
}

func TestTodoTaskOrchestratorPromptRequiresDurableConcernHandoff(t *testing.T) {
	agent := &WorkflowTodoTaskOrchestratorAgent{}
	prompt := agent.todoTaskOrchestratorSystemPromptProcessor(map[string]string{})

	for _, want := range []string{
		"## Completion",
		"CONCERNS: <brief evidence-backed concern; include the affected artifact or operation>",
		"unresolved or consequential run evidence",
		"STATUS: COMPLETED",
		"STATUS: FAILED",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("todo-task prompt missing concern handoff %q:\n%s", want, prompt)
		}
	}
}

func TestFormatMessageSequenceRoutePromptBlock(t *testing.T) {
	block := formatMessageSequenceRoutePromptBlock(&MessageSequencePlanStep{})

	requiredSnippets := []string{
		"Step type: message_sequence",
		"route-scoped session resumes",
		"Initial instructions",
		"Re-entry",
		"message_sequence_restart=true",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(block, snippet) {
			t.Fatalf("expected block to contain %q\n\nBlock:\n%s", snippet, block)
		}
	}

	if got := formatMessageSequenceRoutePromptBlock(&RegularPlanStep{}); got != "" {
		t.Fatalf("expected non-message sequence routes to produce no block, got %q", got)
	}
}

func TestTodoTaskOrchestratorUserPromptIncludesWorkshopHumanInput(t *testing.T) {
	agent := &WorkflowTodoTaskOrchestratorAgent{}

	prompt := agent.todoTaskOrchestratorUserMessageProcessor(map[string]string{
		"StepTitle":           "Investigate RCA",
		"StepDescription":     "Gather evidence and synthesize.",
		"StepSuccessCriteria": "Answer is complete.",
		"WorkshopHumanInput":  "focus on production incidents from the last hour",
	}, nil)

	requiredSnippets := []string{
		"## Human Input (Highest Priority)",
		"execute_step(..., human_input=...)",
		"focus on production incidents from the last hour",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("expected prompt to contain %q\n\nPrompt:\n%s", snippet, prompt)
		}
	}
}
