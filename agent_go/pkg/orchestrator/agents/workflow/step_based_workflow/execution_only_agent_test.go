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

func TestEvaluationPromptsUseOnlyFileOutputContract(t *testing.T) {
	agent := &WorkflowExecutionOnlyAgent{}
	vars := map[string]string{
		"WorkspacePath":           "/app/workspace-docs/Workflow/test/evaluation/runs/iteration-0/default/execution",
		"WorkflowRoot":            "/app/workspace-docs/Workflow/test",
		"StepExecutionPath":       "/app/workspace-docs/Workflow/test/evaluation/runs/iteration-0/default/execution/eval-result",
		"StepContextOutput":       defaultEvaluationContextOutput,
		"StepDescription":         "Score the source-grounded result.",
		"StepContextDependencies": "",
		"LearningHistory":         "",
		"StepNumber":              "eval-result",
		"KnowledgebasePath":       "/app/workspace-docs/Workflow/test/knowledgebase",
		"FolderGuardReadPaths":    "/app/workspace-docs/Workflow/test/db",
		"FolderGuardWritePaths":   "/app/workspace-docs/Workflow/test/evaluation/runs/iteration-0/default/execution/eval-result",
		"IsEvaluationMode":        "true",
		"IsCodeExecutionMode":     "false",
		"IsScriptedMode":          "false",
	}

	systemPrompt := agent.executionOnlySystemPromptProcessor(vars)
	userPrompt := agent.executionOnlyUserMessageProcessor(vars)
	for _, prompt := range []string{systemPrompt, userPrompt} {
		if !strings.Contains(prompt, defaultEvaluationContextOutput) {
			t.Fatalf("evaluation prompt must name %q\n\nPrompt:\n%s", defaultEvaluationContextOutput, prompt)
		}
		for _, forbidden := range []string{"Output to the db", "persist your results to the workflow database", "No output file"} {
			if strings.Contains(prompt, forbidden) {
				t.Fatalf("evaluation prompt contains conflicting DB-output instruction %q\n\nPrompt:\n%s", forbidden, prompt)
			}
		}
	}
	for _, required := range []string{"READ-ONLY workflow evidence", "Evaluation findings are never persisted"} {
		if !strings.Contains(systemPrompt, required) {
			t.Fatalf("evaluation system prompt missing %q\n\nPrompt:\n%s", required, systemPrompt)
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

func TestExecutionOnlyCLIPromptUsesProjectedReferencesAndStaysUnderBudget(t *testing.T) {
	agent := &WorkflowExecutionOnlyAgent{}
	vars := map[string]string{
		"UseProjectedReferenceSkills": "true",
		"WorkspacePath":               "/app/workspace-docs/Workflow/test/runs/iteration-0/default/execution",
		"WorkflowRoot":                "/app/workspace-docs/Workflow/test",
		"StepExecutionPath":           "/app/workspace-docs/Workflow/test/runs/iteration-0/default/execution/step-browser",
		"StepContextOutput":           "result.json",
		"LearningHistory":             "legacy recursive inventory that must not be rendered",
		"StepNumber":                  "step-browser",
		"KnowledgebasePath":           "/app/workspace-docs/Workflow/test/knowledgebase",
		"DBPath":                      "/app/workspace-docs/Workflow/test/db/db.sqlite",
		"KbAccess":                    "read-write",
		"KbAccessLabel":               "READ/WRITE",
		"KbWriteMethod":               "direct",
		"FolderGuardReadPaths":        "/app/workspace-docs/Workflow/test, /app/workspace-docs/Workflow/test/learnings/_global",
		"FolderGuardWritePaths":       "/app/workspace-docs/Workflow/test/runs/iteration-0/default/execution/step-browser, /app/workspace-docs/Workflow/test/db",
		"IsEvaluationMode":            "false",
		"IsCodeExecutionMode":         "true",
		"IsScriptedMode":              "true",
		"HasBrowserAccess":            "true",
		"ScriptedInputArgs":           "/app/workspace-docs/Workflow/test/runs/iteration-0/default/execution/input.json",
		"ScriptedEnvVarNames":         "STEP_OUTPUT_DIR\nDB_PATH\nMCP_API_URL\nMCP_API_TOKEN\nSECRET_PASSWORD",
		"ScriptedVarMapping":          "{{ACCOUNT}} → os.environ['VAR_ACCOUNT']",
		"ValidationSchema":            `{"required_files":[{"path":"result.json"}]}`,
	}

	prompt := agent.executionOnlySystemPromptProcessor(vars)
	t.Logf("compact CLI execution prompt: %d bytes", len(prompt))
	for _, forbidden := range []string{
		"legacy recursive inventory",
		"## Python Best Practices",
		"## Browser automation rules",
		strings.TrimSpace(BuildMainPyAuthoringRules()),
	} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("CLI prompt still embeds projected reference material %q", forbidden)
		}
	}
	const maxCLISystemPromptBytes = 30_000
	if len(prompt) > maxCLISystemPromptBytes {
		t.Fatalf("CLI execution system prompt is %d bytes; budget is %d", len(prompt), maxCLISystemPromptBytes)
	}
}

func TestExecutionOnlyAPIPromptKeepsOneInlineAuthoringFallback(t *testing.T) {
	agent := &WorkflowExecutionOnlyAgent{}
	prompt := agent.executionOnlySystemPromptProcessor(map[string]string{
		"UseProjectedReferenceSkills": "false",
		"StepExecutionPath":           "/app/workspace-docs/Workflow/test/execution/step-scripted",
		"IsCodeExecutionMode":         "true",
		"IsScriptedMode":              "true",
		"HasBrowserAccess":            "true",
	})

	if !strings.Contains(prompt, strings.TrimSpace(BuildMainPyAuthoringRules())) {
		t.Fatal("API prompt lost its inline main.py authoring fallback")
	}
	if got := strings.Count(prompt, "## Python Best Practices"); got != 1 {
		t.Fatalf("API prompt must contain exactly one Python fallback section, got %d", got)
	}
	if !strings.Contains(prompt, "## Browser automation rules") {
		t.Fatal("API prompt lost its inline browser authoring fallback")
	}
}

// A synthetic learnings/KB closing turn (IsContributionTurn) must render as JUST the
// contribution instruction — the execute-the-task / Inputs / Output / "create the
// output file" scaffolding is contradictory and wasteful on a write-only turn. A
// normal turn keeps that scaffolding. Regression for the learnings/KB turn carrying
// execution scaffolding.
func TestContributionTurnUserMessageDropsExecutionScaffolding(t *testing.T) {
	agent := &WorkflowExecutionOnlyAgent{}
	contribMsg := "## Learnings Contribution (dedicated turn)\nCapture HOW to run this; write it to SKILL.md, then stop."

	contribution := agent.executionOnlyUserMessageProcessor(map[string]string{
		"StepDescription":    contribMsg,
		"IsContributionTurn": "true",
		"StepContextOutput":  "",
	})
	for _, forbidden := range []string{"Execution Checklist", "Create the output file", "MODE NOTE", "### Inputs", "### Output", "Verify the required outputs"} {
		if strings.Contains(contribution, forbidden) {
			t.Fatalf("contribution turn must not carry execution scaffolding %q:\n%s", forbidden, contribution)
		}
	}
	if !strings.Contains(contribution, "write it to SKILL.md, then stop.") {
		t.Fatalf("contribution message body was lost:\n%s", contribution)
	}

	normal := agent.executionOnlyUserMessageProcessor(map[string]string{
		"StepDescription":   "Fetch and persist the report.",
		"StepContextOutput": "report.json",
		"StepExecutionPath": "execution/step-1",
	})
	for _, want := range []string{"Execution Checklist", "Create the output file", "### Inputs", "### Output"} {
		if !strings.Contains(normal, want) {
			t.Fatalf("a normal execution turn must keep its scaffolding %q:\n%s", want, normal)
		}
	}
}
