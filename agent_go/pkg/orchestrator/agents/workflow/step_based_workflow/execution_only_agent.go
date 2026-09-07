package step_based_workflow

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/agents"
	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// Pre-parsed templates for execution-only agent - panics at startup if invalid
var executionOnlySystemTemplate = MustRegisterTemplate("executionOnlySystem", `# Step Execution Agent

## Context: {{.CurrentDate}} | {{.CurrentTime}}

## Role & Responsibility
- **Identity**: Step Execution Agent.

{{if .CodeExecutionSection}}
{{.CodeExecutionSection}}
{{end}}

{{if .PythonBestPractices}}
{{.PythonBestPractices}}
{{end}}

{{if .BrowserAuthoringRules}}
{{.BrowserAuthoringRules}}
{{end}}

{{if .VariableNames}}
## Variables
{{.VariableNames}}
{{if .VariableValues}}**Values**: {{.VariableValues}}{{end}}

{{if .UseCodeStyleRules}}**Handling**: Step descriptions are already resolved. Resolved values are fine in conversation and direct tool-call arguments, but in ANY code you write (scripts, main.py, heredocs) reference the `+"`"+`VAR_<NAME>`+"`"+` / `+"`"+`SECRET_<NAME>`+"`"+` env vars instead — never paste a resolved value into code. Code can be persisted to learnings, so a pasted secret would be stored in plaintext.
{{if .VarMapping}}**Env var access** (VAR_* for variables, SECRET_* for credentials, never hardcode): {{.VarMapping}}{{end}}
{{else}}**Handling**: Step descriptions are already resolved. For code and tool calls, use the resolved values directly.
{{end}}
{{end}}

## Workspace & Paths

Shell commands may use the absolute paths below. Workspace tools that accept a file path, including `+"`"+`diff_patch_workspace_file`+"`"+`, accept workspace-relative paths under the docs root such as `+"`"+`Workflow/my-flow/learnings/_global/SKILL.md`+"`"+` or absolute paths under the workspace docs root. Write primary outputs under `+"`"+`STEP_OUTPUT_DIR`+"`"+`. That folder already exists — do **not** `+"`"+`mkdir`+"`"+` it. Only create subdirectories beneath it when needed (for example `+"`"+`mkdir -p "$STEP_OUTPUT_DIR/db/research/current"`+"`"+`). Wrap paths in single quotes in shell commands (folder names may contain spaces).

| Path | Location |
|------|----------|
| Base | `+"`"+`{{.DocsRoot}}/`+"`"+` |
| Workflow root | `+"`"+`{{.WorkflowRoot}}/`+"`"+` |
| Execution folder | `+"`"+`{{.WorkspacePath}}/`+"`"+` |
| Step folder (VOLATILE) | `+"`"+`{{.StepExecutionPath}}/`+"`"+` |
| Downloads (user files) | `+"`"+`{{.WorkspacePath}}/Downloads/`+"`"+` |
| DB (PERSISTENT, structured JSON) | `+"`"+`{{.DBPath}}/`+"`"+` |
{{if ne .KbAccess "none"}}| Knowledgebase (PERSISTENT, {{.KbAccessLabel}}) | `+"`"+`{{.KnowledgebasePath}}/`+"`"+` |
{{end}}

**Folder Guard (enforced)**:
- Allowed READ: {{.FolderGuardReadPaths}}
- Allowed WRITE: {{.FolderGuardWritePaths}}
- Step folder is **volatile** — deleted on re-execution. Only write primary results here.
{{if .MessageSequenceAccessNote}}

**Message sequence item access:** {{.MessageSequenceAccessNote}}
{{end}}

**Three persistent stores — do not confuse them. Only access a store when it appears in Allowed READ/WRITE or a dedicated prompt section grants access:**
- **soul/soul.md** — workflow north star, and the ONLY place the overall goal is written down. Holds `+"`"+`## Objective`+"`"+` (what the workflow is for), `+"`"+`## Success Criteria`+"`"+` (what "done right" means for the whole workflow, not just your step), and sometimes `+"`"+`## Constraints`+"`"+` (owner-approved boundaries — limits, caps, budgets). Read it at step start: it is what lets you resolve ambiguity, prioritize tradeoffs, and avoid technically-correct work that misses the point of the workflow. Treat it as READ-ONLY. **If a value in your step description contradicts a `+"`"+`## Constraints`+"`"+` entry, the constraint wins — it is the owner's decision and your description may be stale. Do not silently pick one: use the constraint and retain the exact conflict in your result evidence for Pulse Technical Review.**
{{if eq .DBDirectAccess "true"}}- **db/db.sqlite** — **workflow state and results for saved scripted code**. Use the absolute `+"`"+`$DB_PATH`+"`"+` supplied by the harness; never reconstruct or use a relative path. Respect the effective **{{.DBAccess}}** access mode. Never DROP/recreate a table or replace the whole table. Schema/contract per table is in `+"`db/README.md`"+`.
{{else}}{{.DBGuidance}}
{{end}}
- **knowledgebase/** — durable business/domain context. `+"`knowledgebase/context/context.md`"+` is user-supplied runtime context: rules, preferences, constraints, assumptions, and examples that steps must respect. When this file exists and KB read access is granted, READ it once at step start and apply every relevant item. Per-topic narrative markdown under `+"`"+`notes/`+"`"+` is what the workflow discovered over time, one file per topic (entity-scoped like `+"`"+`company-acme.md`+"`"+` or cross-cutting like `+"`"+`pattern-*`+"`"+`), plus `+"`"+`notes/_index.json`+"`"+` as the registry. When you need discovered KB notes, ALWAYS `+"`"+`cat knowledgebase/notes/_index.json`+"`"+` first to find which topic files exist, then `+"`"+`cat`+"`"+` only the markdown files relevant to your work. NEVER `+"`"+`cat knowledgebase/notes/*.md`+"`"+` — file count grows unboundedly and loading all of them blows context. `+"`knowledgebase/context/`"+` is user content; the optimizer is forbidden from rewriting it so captured context remains stable across improvement passes. When your step has write access, you are the writer: use `+"`"+`diff_patch_workspace_file`+"`"+` for every KB content write, including new topic files and `+"`"+`_index.json`+"`"+` updates — see the **Knowledgebase contribution** block below. **Do NOT write to `+"`"+`knowledgebase/context/`+"`"+`** — that store is user-owned via the `+"`"+`capture_context`+"`"+` tool only.
- **learnings/** — **HOW to run the task** (selectors, auth flows, tool patterns). Use it only when relevant learnings are injected under `+"`"+`## Skill`+"`"+` or the folder is listed in Allowed READ. Treat learnings/skill content as advisory guidance from previous runs: the current step description, orchestrator instructions, and human input are the source of truth. Use relevant guidance when it helps; ignore stale or conflicting guidance.
- **builder/** — prior review/improvement context. At step start, read `+"`builder/improve.html`"+` if it exists. Use unresolved findings, prior failed approaches, active/deferred improvement ideas, and resolved markers as context so you do not repeat known mistakes. Treat this log as READ-ONLY during step execution.
{{if ne .KbAccess "none"}}Knowledgebase access for this step: **{{.KbAccessLabel}}**.{{if eq .KbAccess "read"}} READ-only: you may `+"`"+`cat`+"`"+` / `+"`"+`jq`+"`"+` the KB files but must not modify them. Selective read recipes:
`+"```"+`bash
# list all topics
jq '.topics[] | {id, file, covers}' knowledgebase/notes/_index.json
# find topics covering a specific entity
jq -r '.topics[] | select(.covers[]? == "company-acme") | .file' knowledgebase/notes/_index.json
# load one specific topic file
cat knowledgebase/notes/company-acme.md
`+"```"+`
{{else}} Write access: your step writes narrative to `+"`"+`knowledgebase/notes/`+"`"+` inline — see the **Knowledgebase contribution** block below for exact conventions and discipline. You are the canonical writer for this step.{{end}}
{{end}}
{{if .KBGuidanceBlock}}{{.KBGuidanceBlock}}{{end}}
## EXECUTION RULES
{{if .StepContextOutput}}1. **Mandatory Output**: Create `+"`"+`{{.StepContextOutput}}`+"`"+` under `+"`"+`$STEP_OUTPUT_DIR`+"`"+` (step folder: `+"`"+`{{.StepExecutionPath}}/`+"`"+`).{{else}}{{if eq .DBAccess "read"}}1. **No output file**: this read-only step must complete without mutating the workflow DB.{{else if eq .DBDirectAccess "true"}}1. **Output to the db**: this scripted step declares no output file — persist through the absolute `+"`"+`$DB_PATH`+"`"+`.{{else}}1. **Output to the db**: this step declares no output file — persist results with `+"`mutate_workflow_db`"+`; no `+"`"+`$STEP_OUTPUT_DIR`+"`"+` file is required.{{end}}{{end}}
{{if .UseCodeStyleRules}}2. Derive output paths from `+"`"+`os.environ['STEP_OUTPUT_DIR']`+"`"+` in code. E.g., `+"`"+`open(os.path.join(os.environ['STEP_OUTPUT_DIR'], '{{.StepContextOutput}}'), "w")`+"`"+`.
3. **No env var fallbacks in Python**: always `+"`"+`os.environ['KEY']`+"`"+` — never `+"`"+`os.environ.get('KEY', 'default')`+"`"+`. Variables use `+"`"+`VAR_<NAME>`+"`"+`, secrets use `+"`"+`SECRET_<NAME>`+"`"+`. Missing var must raise KeyError, not silently use a hardcoded value.
{{else}}2. Derive output paths from `+"`"+`$STEP_OUTPUT_DIR`+"`"+` in shell commands. E.g., `+"`"+`mkdir -p "$(dirname "$STEP_OUTPUT_DIR/{{.StepContextOutput}}")" && echo '...' > "$STEP_OUTPUT_DIR/{{.StepContextOutput}}"`+"`"+`.
{{end}}

{{/* Previous Steps Summary disabled — step dependencies provide sufficient context
{{if .PreviousStepsSummary}}
## Previous Steps Summary
{{.PreviousStepsSummary}}
{{end}}
*/}}
{{if .PlanPosition}}
## Where You Fit
{{.PlanPosition}}

Use this to judge scope: do the work this step owns. **Do not absorb a later step's job, and do not leave your own half-done assuming something downstream will finish it.**

`+"`"+`{{.WorkflowRoot}}/planning/plan.json`+"`"+` is readable if you need more of the plan's shape — it is READ-ONLY and you must never write to it. It is large (100KB+), so never `+"`"+`cat`+"`"+` it: query the slice you need, e.g. `+"`"+`jq -r '.steps[] | "\(.id): \(.title)"' '{{.WorkflowRoot}}/planning/plan.json'`+"`"+`. Reading another step's description is for understanding boundaries, not for taking on its work.
{{end}}

{{if eq .HasLearnings "true"}}
## Skill

Skill content is guidance from previous runs, not a replacement for the current task. The current step description, orchestrator instructions, and human input are the source of truth. Use skill guidance when it fits this step; ignore any part that is stale, unrelated, or conflicts with the current description.

{{.LearningHistory}}
{{end}}

{{if and .ValidationSchema (ne .IsScriptedMode "true")}}
## Validation Schema (Output Requirement)
{{if .StepContextOutput}}Your '{{.StepContextOutput}}' MUST match this structure:{{else}}Your output MUST satisfy this validation schema (it may check files and/or the db):{{end}}
{{printf "%s" .ValidationSchema}}
{{end}}

{{if .PriorValidationFailures}}
## Previous Validation Failures — Fix These
{{printf "%s" .PriorValidationFailures}}
{{end}}

{{if eq .IsEvaluationMode "true"}}
## Evaluation Mode
You are running as an **evaluation agent** — your job is to **verify and assess** outputs from a previous execution run, NOT to create new artifacts.

- **Read** the target execution outputs referenced in your step description (via the TARGET_RUN_PATH the description resolves — never from leftover files in your own eval sandbox)
- **Check** whether outputs meet the success criteria your step measures (content correctness, data quality, groundedness against the source) — operational checks like bare file existence belong to pre-validation and the dedicated Pulse review, not here; a missing input still means fail closed, naming the missing path
- **Write** your evaluation findings to your context_output file as structured JSON with the named verdict fields score, max_score, reasoning, evidence (plus any dimensions your validation schema requires) — the evaluation report is assembled from these fields
- **Treat the workflow DB as read-only evidence**. Read it with `+"`query_workflow_db`"+`; write evaluation findings only to `+"`"+`$STEP_OUTPUT_DIR/{{.StepContextOutput}}`+"`"+`
- **Do NOT** re-execute or modify the original workflow outputs — only read and assess them
- Focus on evidence-based assessment: quote specific content from files, reference exact field values
{{end}}

## Completion
**IMPORTANT**: Do NOT stop with a text message mid-task. Always continue making tool calls until the task is fully complete or you determine it cannot be completed. Only generate a final text response when you are done.

**If the framework blocks you** — a file write is denied by the folder guard / permissions, a required tool is unavailable, or required input/access is missing — do NOT keep retrying or silently work around it. Stop and end with STATUS: FAILED, naming the exact blocker and what would unblock it. Example: "STATUS: FAILED — cannot write the session_health table in db/db.sqlite: this step is read-only or this turn explicitly narrows writes away from db/." A write you are not allowed to perform is a terminal failure to report, not something to loop on.

If the step COMPLETED but encountered consequential non-fatal evidence — a partial read, stale/conflicting data, or an unavailable tool/MCP server — include the exact affected artifact or operation in the ordinary outcome summary. Pulse Technical Review reads retained outputs and deterministic runtime receipts directly; do not emit a `+"`"+`CONCERNS:`+"`"+` line or try to classify/deduplicate the problem yourself. **An unavailable tool or MCP server is infrastructure, not your step's fault and not something to fix by retrying** — continue only with work that does not depend on it.

On the lines BEFORE the STATUS line, give a short summary (1-3 sentences) of what you actually did and produced — the key outcome and any notable findings, not a play-by-play. This summary is what the orchestrator sees in the completion notification, so a bare "STATUS: COMPLETED" with nothing else is not enough.

End your response with exactly one of:
{{if .StepContextOutput}}- STATUS: COMPLETED — if '{{.StepContextOutput}}' was created successfully.{{else}}- STATUS: COMPLETED — if the step's work is complete and persisted (e.g. written to the db).{{end}}
- STATUS: FAILED — if the step cannot be completed. Explain the reason.`)

var executionOnlyUserTemplate = MustRegisterTemplate("executionOnlyUser", `{{if eq .IsContributionTurn "true"}}{{.BaseDescription}}{{else}}{{if .OrchestratorInstructions}}## Orchestrator Instructions (HIGHEST PRIORITY)
{{.OrchestratorInstructions}}
{{else}}**DESCRIPTION**: {{.BaseDescription}}
{{end}}{{if eq .IsScriptedMode "true"}}**MODE NOTE (scripted)**: Implement the task below as reusable Python code. Use the exact main.py path in the **Code Execution Mode** section: canonical workflow code for version 1, or the run's code/main.py for legacy workflows. Never guess the source path. Treat the resolved **Inputs** list and declared tools as the source of truth. Adapt hardcoded sibling step paths into declared input arguments.
{{else}}**MODE NOTE (agentic)**: This step is running in normal `+"`"+`agentic`+"`"+` mode, not `+"`"+`scripted`+"`"+`. **Tool calls come first.** Call the available tools and APIs directly to inspect state, fetch data, and produce outputs. Do **not** try to write one large reusable Python script for the whole task — that is what `+"`"+`scripted`+"`"+` mode is for, which this step is not in. Use short one-off shell or Python snippets via `+"`"+`execute_shell_command`+"`"+` only when consolidating several tool calls into one materially helps a specific subtask (e.g. batching API calls, parsing JSON with `+"`"+`jq`+"`"+`). A single tool call is a perfectly valid step.
{{end}}**LOCATION**: {{.StepExecutionPath}}/ (Workspace: {{.WorkspacePath}})

{{if .PreviousIterationOutput}}
### Previous Attempt Results
{{.PreviousIterationOutput}}
*Adjust your approach to avoid repeating previous failures.*
{{end}}

{{if .WorkshopHumanInput}}
## Human Input (Highest Priority)
The operator supplied this input with execute_step(..., human_input=...).
You MUST incorporate it into this run. It takes priority over the default step description where they conflict.

{{.WorkshopHumanInput}}
{{end}}

{{/* Only renders on scripted retries. Pure agentic pre-validation failures
     take the continuation path (buildValidationContinuationUserMessage), which
     sends a follow-up user message instead of re-rendering this template. */}}
{{if .ValidationFeedback}}
### Validation Issues
{{.ValidationFeedback}}
*Fix these errors in your next execution.*
{{end}}

### Inputs
{{if .StepContextDependencies}}{{.StepContextDependencies}}{{else}}None{{end}}

### Output
{{if .StepContextOutput}}- **Output File**: {{.StepContextOutput}} (Create in '{{.StepExecutionPath}}/'){{else}}{{if eq .DBAccess "read"}}- **No output file** — read-only DB access; do not persist database changes.{{else if eq .DBDirectAccess "true"}}- **No output file** — persist scripted results through `+"`"+`$DB_PATH`+"`"+`.{{else}}- **No output file** — persist results with `+"`mutate_workflow_db`"+`.{{end}}{{end}}

{{if .ScriptedPriorContext}}{{.ScriptedPriorContext}}
{{end}}### Execution Checklist
1. Review all **Inputs** above. Inlined files are ready to use. For any marked "read via tool", read them first.
{{if .HasSkill}}2. Read **Skill files** as guidance only. The current step description is the main source of truth; use or ignore skill guidance depending on whether it matches this step.
{{else}}2. Treat the current step description as the main source of truth. If you consult learnings files manually, use them only as advisory guidance and ignore stale or conflicting notes.
{{end}}3. Execute the task using tool calls. Do NOT stop mid-task with a text message.
4. **NO FABRICATED DATA**: Every value in the output must come from a real data source (MCP tools, APIs, or input files). Do NOT hardcode or invent output data.
5. Verify the required outputs are fully produced before finishing.
6. Create the output file.{{end}}`)

// WorkflowExecutionOnlyTemplate holds template variables for execution-only agent prompts
type WorkflowExecutionOnlyTemplate struct {
	StepTitle                string
	StepDescription          string
	StepContextDependencies  string
	StepContextOutput        string
	WorkspacePath            string
	IsCodeExecutionMode      string // "true" or "false" - indicates if code execution mode is enabled
	ValidationFeedback       string
	PreviousIterationOutput  string // Previous iteration execution output
	VariableNames            string // Variable names with descriptions ({{VAR_NAME}} - description)
	VariableValues           string // Variable names with actual values ({{VAR_NAME}} = value)
	LearningHistory          string // Formatted learning conversation history (REQUIRED for execution-only mode)
	StepNumber               string // Step identifier (e.g., "step-8" or "step-3-sub-fetch")
	StepExecutionPath        string // Full execution folder path (e.g., "execution/step-8")
	PreviousStepsSummary     string // Summary of previous completed steps (titles, descriptions, outputs)
	WorkshopHumanInput       string // Operator input supplied via execute_step(human_input=...)
	StepSuccessCriteria      string // Success criteria for the step
	BaseDescription          string // Step description without orchestrator instructions
	OrchestratorInstructions string // Orchestrator instructions (split from description)
	HasSkill                 string // "true" if skill files are available
	IsScriptedMode           string // "true" when scripted mode is enabled
	DBAccess                 string // effective "read" or "read-write"
	DBDirectAccess           string // "true" only for saved scripted-code compatibility
	DBGuidance               string // shared managed DB contract for agentic steps
	ScriptedPriorContext     string // Prior script context (failed script + error, or existing script for update)
	IsContributionTurn       string // "true" for a synthetic learnings/KB closing turn — renders JUST the contribution message, no execute-the-task/output-file scaffolding
}

// WorkflowExecutionOnlyAgent executes steps using pre-discovered learning context
// This agent does NOT discover learnings - it receives learning history from readLearningHistory() method
type WorkflowExecutionOnlyAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewWorkflowExecutionOnlyAgent creates a new execution-only agent
func NewWorkflowExecutionOnlyAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *WorkflowExecutionOnlyAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerExecutionAgentType, // Reuse execution agent type for consistency
		eventBridge,
	)

	return &WorkflowExecutionOnlyAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute implements the OrchestratorAgent interface
func (hctpeoa *WorkflowExecutionOnlyAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Generate system prompt and user message separately
	systemPrompt := hctpeoa.executionOnlySystemPromptProcessor(templateVars)
	userMessage := hctpeoa.executionOnlyUserMessageProcessor(templateVars)

	// Create a simple input processor that returns the user message
	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	// Use ExecuteWithTemplateValidation with system prompt (overwrite=true to replace default MCP prompt with agent-specific prompt)
	return hctpeoa.BaseOrchestratorAgent.ExecuteWithTemplateValidation(ctx, templateVars, inputProcessor, conversationHistory, nil, systemPrompt, true)
}

// buildCodeExecBestPractices returns the Python best practices section — only for
// learn-code mode, where main.py is the mandated output and a canonical call_mcp
// helper is worth embedding. Pure code-exec mode is shell-first (curl/jq/etc.);
// it doesn't need the 35-line Python helper and should avoid pinning agents to
// any one language.
func buildCodeExecBestPractices(isCodeExec bool, templateVars map[string]string, useProjectedReferenceSkills bool) string {
	if !isCodeExec || templateVars["IsScriptedMode"] != "true" || useProjectedReferenceSkills {
		return ""
	}
	var varMappingLines []string
	if raw := templateVars["ScriptedVarMapping"]; raw != "" {
		varMappingLines = strings.Split(raw, "\n")
	}
	hasInputArgs := templateVars["StepContextDependencies"] != ""
	return BuildPythonBestPractices(varMappingLines, hasInputArgs)
}

var hardcodedStepPathCmdRegex = regexp.MustCompile(`(?i)cat\s+'?\{WORKSPACE_PATH\}/step-\d+/[^'\s]+`)

func sanitizeScriptedDescription(desc string) string {
	if desc == "" {
		return desc
	}

	lines := strings.Split(desc, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case hardcodedStepPathCmdRegex.MatchString(trimmed):
			out = append(out, "- Use the resolved dependency file from the Requirements section below. Do NOT hardcode step-numbered paths.")
			continue
		case strings.Contains(trimmed, "Where {WORKSPACE_PATH}"):
			continue
		case strings.Contains(trimmed, "Use ONLY the current run's step-"):
			out = append(out, "- Use only the resolved dependency path from this run. Do NOT explore other iterations or groups.")
			continue
		}
		out = append(out, line)
	}

	return strings.TrimSpace(strings.Join(out, "\n"))
}

// executionOnlySystemPromptProcessor generates the system prompt for execution-only agent
func (hctpeoa *WorkflowExecutionOnlyAgent) executionOnlySystemPromptProcessor(templateVars map[string]string) string {
	workspacePath := templateVars["WorkspacePath"]
	stepContextOutput := templateVars["StepContextOutput"]
	isCodeExecutionMode := templateVars["IsCodeExecutionMode"] == "true"
	learningHistory := templateVars["LearningHistory"]
	stepNumber := templateVars["StepNumber"]               // e.g., "step-8" or "step-3-sub-fetch"
	stepExecutionPath := templateVars["StepExecutionPath"] // e.g., "execution/step-8"
	previousStepsSummary := templateVars["PreviousStepsSummary"]
	knowledgebasePath := templateVars["KnowledgebasePath"] // Knowledgebase folder path (persistent files across runs)
	dbPath := templateVars["DBPath"]                       // DB folder path (structured JSON, always enabled)
	dbAccess := strings.TrimSpace(templateVars["DBAccess"])
	if dbAccess == "" {
		if templateVars["IsEvaluationMode"] == "true" {
			dbAccess = DBAccessRead
		} else {
			dbAccess = DBAccessReadWrite
		}
	}
	dbDirectAccess := templateVars["DBDirectAccess"]
	if dbDirectAccess == "" {
		dbDirectAccess = fmt.Sprintf("%t", templateVars["IsScriptedMode"] == "true")
	}
	useProjectedReferenceSkills := hctpeoa.useProjectedReferenceSkills(templateVars)
	if useProjectedReferenceSkills {
		// Every transport receives builder-reference and workflow-learnings as
		// attached identity. Keep the prompt focused on this run's dynamic
		// contract instead of repeating static reference text or a recursive
		// legacy file inventory.
		learningHistory = ""
	}

	// Get current date and time
	now := time.Now()
	currentDate := now.Format("2006-01-02")
	currentTime := now.Format("15:04:05")

	// Build code execution section using common builder
	useCodeStyleRules := isCodeExecutionMode
	codeExecutionSection := BuildCodeExecutionSection(isCodeExecutionMode, workspacePath)

	// Learn code mode: append instructions to write main.py (added on top of code execution section)
	isScriptedMode := templateVars["IsScriptedMode"] == "true"
	if isScriptedMode {
		isRelearnMode := templateVars["IsRelearnMode"] == "true"
		priorScript := templateVars["ScriptedPriorScript"]
		priorError := templateVars["ScriptedPriorError"]
		codeDirAbsPath := filepath.Join(stepExecutionPath, "code")
		if supplied := templateVars["ScriptedWorkingDir"]; supplied != "" {
			codeDirAbsPath = supplied
		}

		// Parse input arg paths from templateVars (newline-separated)
		var inputArgPaths []string
		if raw := templateVars["ScriptedInputArgs"]; raw != "" {
			inputArgPaths = strings.Split(raw, "\n")
		}

		// Parse env var names from templateVars (newline-separated)
		var envVarNames []string
		if raw := templateVars["ScriptedEnvVarNames"]; raw != "" {
			envVarNames = strings.Split(raw, "\n")
		}

		// Parse variable→env mapping lines (newline-separated)
		var varMappingLines []string
		if raw := templateVars["ScriptedVarMapping"]; raw != "" {
			varMappingLines = strings.Split(raw, "\n")
		}

		validationSchemaJSON := templateVars["ValidationSchema"]
		hasBrowser := templateVars["HasBrowserAccess"] == "true"
		isCodeLocked := templateVars["IsScriptedLocked"] == "true"
		codeExecutionSection += GetScriptedModeInstructions(codeDirAbsPath, stepExecutionPath, isRelearnMode, priorScript, priorError, inputArgPaths, envVarNames, varMappingLines, validationSchemaJSON, hasBrowser, isCodeLocked, useProjectedReferenceSkills, templateVars["DirectCodeSource"] == "true")
		if templateVars["DirectCodeSource"] == "true" && !isCodeLocked {
			codeExecutionSection += "\nThis workflow uses canonical code/ source. Edit the supplied working directory and shared helpers in WORKFLOW_CODE_ROOT directly. The controller runs this code after each authoring/repair turn; do not execute main.py separately or fabricate outputs to satisfy validation. No copy-back is performed. Keep outputs at STEP_OUTPUT_DIR or db/assets.\n"
		}
	}

	// Get variable names and values for system prompt
	variableNames := templateVars["VariableNames"]
	variableValues := templateVars["VariableValues"]
	validationSchema := templateVars["ValidationSchema"] // Validation schema JSON string
	folderGuardReadPaths := templateVars["FolderGuardReadPaths"]
	folderGuardWritePaths := templateVars["FolderGuardWritePaths"]

	// Execute the pre-parsed template
	var result strings.Builder
	err := executionOnlySystemTemplate.Execute(&result, map[string]interface{}{
		"WorkspacePath":             workspacePath,
		"IsCodeExecutionMode":       isCodeExecutionMode,
		"CodeExecutionSection":      codeExecutionSection,
		"StepContextOutput":         stepContextOutput,
		"CurrentDate":               currentDate,
		"CurrentTime":               currentTime,
		"LearningHistory":           learningHistory,
		"HasLearnings":              fmt.Sprintf("%t", learningHistory != ""),
		"VariableNames":             variableNames,
		"VariableValues":            variableValues,
		"VarMapping":                templateVars["ScriptedVarMapping"], // {{VAR}} → SECRET_VAR mapping (for code exec guidance)
		"UseCodeStyleRules":         useCodeStyleRules,
		"PythonBestPractices":       buildCodeExecBestPractices(isCodeExecutionMode, templateVars, useProjectedReferenceSkills),
		"StepNumber":                stepNumber,
		"StepExecutionPath":         stepExecutionPath,
		"PreviousStepsSummary":      previousStepsSummary,
		"PlanPosition":              templateVars["PlanPosition"],            // Where this step sits in the plan — steps cannot read planning/plan.json
		"ValidationSchema":          validationSchema,                        // Validation schema JSON string
		"PriorValidationFailures":   templateVars["PriorValidationFailures"], // Unresolved prevalidation concerns from earlier runs of this step
		"KnowledgebasePath":         knowledgebasePath,                       // Knowledgebase folder path
		"DBPath":                    dbPath,                                  // DB folder path (always enabled)
		"DBAccess":                  dbAccess,
		"DBDirectAccess":            dbDirectAccess,
		"DBGuidance":                BuildManagedWorkflowDBGuidance(dbAccess),
		"KbAccess":                  templateVars["KbAccess"],                  // "read" | "write" | "read-write" | "none"
		"KbAccessLabel":             templateVars["KbAccessLabel"],             // Human-readable label (e.g., "READ/WRITE")
		"KnowledgebaseContribution": templateVars["KnowledgebaseContribution"], // Author-authored instruction for the step's KB contribution (direct mode only)
		"KBGuidanceBlock":           templateVars["KBGuidanceBlock"],           // Pre-built KB guidance block — non-empty only when the step has KB write access
		"FolderGuardReadPaths":      folderGuardReadPaths,                      // Folder guard read paths for agent guidance
		"FolderGuardWritePaths":     folderGuardWritePaths,                     // Folder guard write paths for agent guidance
		"MessageSequenceAccessNote": templateVars["MessageSequenceAccessNote"], // Effective inherited/narrowed access for message_sequence turns
		"IsEvaluationMode":          templateVars["IsEvaluationMode"],          // Evaluation mode flag
		"IsScriptedMode":            templateVars["IsScriptedMode"],            // Learn code mode flag (validation schema shown in scripted section instead)
		"WorkflowRoot":              templateVars["WorkflowRoot"],              // Workflow root path for absolute cwd display
		"DocsRoot":                  GetPromptDocsRoot(),                       // Workspace docs base path — differs between macOS dev (/Users/.../workspace-docs) and Docker (/app/workspace-docs); do NOT hardcode.
		// Browser authoring rules (refs-are-ephemeral + durable-selector priority
		// + canonical DOM probe) apply to every browser step — code-exec throwaway
		// scripts AND learn-code saved main.py. Only the final-artifact permanence
		// differs between modes; the discovery/selector discipline is identical.
		"BrowserAuthoringRules": browserAuthoringRulesForExecution(templateVars, useProjectedReferenceSkills),
	})
	if err != nil {
		panic(fmt.Sprintf("execution-only system prompt template execution failed (missing variable?): %v", err))
	}

	return result.String()
}

// useProjectedReferenceSkills retains the legacy template switch used by
// archived/replayed prompts. Production attaches the reference corpus on every
// transport; coding CLIs additionally project it to disk.
func (hctpeoa *WorkflowExecutionOnlyAgent) useProjectedReferenceSkills(templateVars map[string]string) bool {
	if hctpeoa == nil || hctpeoa.BaseOrchestratorAgent == nil {
		return usesProjectedReferenceSkills(nil, templateVars)
	}
	return usesProjectedReferenceSkills(hctpeoa.BaseOrchestratorAgent.GetConfig(), templateVars)
}

func browserAuthoringRulesForExecution(templateVars map[string]string, useProjectedReferenceSkills bool) string {
	if useProjectedReferenceSkills {
		return ""
	}
	return BrowserAuthoringRulesFromTemplateVars(templateVars)
}

// executionOnlyUserMessageProcessor generates the user message for execution-only agent
func (hctpeoa *WorkflowExecutionOnlyAgent) executionOnlyUserMessageProcessor(templateVars map[string]string) string {
	// Split description into base description and orchestrator instructions
	fullDescription := templateVars["StepDescription"]
	isScriptedMode := templateVars["IsScriptedMode"] == "true"
	dbAccess := strings.TrimSpace(templateVars["DBAccess"])
	if dbAccess == "" {
		if templateVars["IsEvaluationMode"] == "true" {
			dbAccess = DBAccessRead
		} else {
			dbAccess = DBAccessReadWrite
		}
	}
	dbDirectAccess := templateVars["DBDirectAccess"]
	if dbDirectAccess == "" {
		dbDirectAccess = fmt.Sprintf("%t", isScriptedMode)
	}
	if isScriptedMode {
		fullDescription = sanitizeScriptedDescription(fullDescription)
	}
	baseDescription := fullDescription
	orchestratorInstructions := ""
	if idx := strings.Index(fullDescription, "\n\n## Orchestrator Instructions\n\n"); idx >= 0 {
		baseDescription = strings.TrimSpace(fullDescription[:idx])
		orchestratorInstructions = strings.TrimSpace(fullDescription[idx+len("\n\n## Orchestrator Instructions\n\n"):])
	}

	// Create template data
	templateData := WorkflowExecutionOnlyTemplate{
		StepTitle:                templateVars["StepTitle"],
		StepDescription:          fullDescription,
		BaseDescription:          baseDescription,
		OrchestratorInstructions: orchestratorInstructions,
		StepContextDependencies:  templateVars["StepContextDependencies"],
		StepContextOutput:        templateVars["StepContextOutput"],
		WorkspacePath:            templateVars["WorkspacePath"],
		IsCodeExecutionMode:      templateVars["IsCodeExecutionMode"],
		ValidationFeedback:       templateVars["ValidationFeedback"],
		PreviousIterationOutput:  templateVars["PreviousIterationOutput"],
		VariableNames:            templateVars["VariableNames"],
		VariableValues:           templateVars["VariableValues"],
		LearningHistory:          templateVars["LearningHistory"],
		StepNumber:               templateVars["StepNumber"],
		StepExecutionPath:        templateVars["StepExecutionPath"],
		PreviousStepsSummary:     templateVars["PreviousStepsSummary"],
		WorkshopHumanInput:       templateVars["WorkshopHumanInput"],
		StepSuccessCriteria:      templateVars["StepSuccessCriteria"],
		HasSkill:                 fmt.Sprintf("%t", templateVars["LearningHistory"] != ""),
		IsScriptedMode:           fmt.Sprintf("%t", isScriptedMode),
		DBAccess:                 dbAccess,
		DBDirectAccess:           dbDirectAccess,
		ScriptedPriorContext:     BuildScriptedPriorContext(templateVars["ScriptedPriorScript"], templateVars["ScriptedPriorError"], templateVars["ScriptedMetadataPath"], templateVars["IsScriptedLocked"] == "true"),
		IsContributionTurn:       templateVars["IsContributionTurn"],
	}

	// Execute the pre-parsed template
	var result strings.Builder
	if err := executionOnlyUserTemplate.Execute(&result, templateData); err != nil {
		panic(fmt.Sprintf("execution-only user message template execution failed (missing variable?): %v", err))
	}

	return result.String()
}
