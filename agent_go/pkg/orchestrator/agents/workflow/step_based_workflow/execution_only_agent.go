package step_based_workflow

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/observability"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"

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

**Handling**: Step descriptions are already resolved. For code and tool calls, use the resolved values directly.
{{if and .UseCodeStyleRules .VarMapping}}**Env var access** (VAR_* for variables, SECRET_* for credentials, never hardcode): {{.VarMapping}}{{end}}
{{end}}

## Workspace & Paths

All paths are absolute. Write primary outputs under `+"`"+`STEP_OUTPUT_DIR`+"`"+`. That folder already exists — do **not** `+"`"+`mkdir`+"`"+` it. Only create subdirectories beneath it when needed (for example `+"`"+`mkdir -p "$STEP_OUTPUT_DIR/db/research/current"`+"`"+`). Wrap paths in single quotes in shell commands (folder names may contain spaces).

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

**Three persistent stores — do not confuse them. Only access a store when it appears in Allowed READ/WRITE or a dedicated prompt section grants access:**
- **soul/soul.md** — workflow north star: objective and success criteria. At step start, read it if present and use it to resolve ambiguity, prioritize tradeoffs, and avoid technically-correct work that misses the workflow goal. Treat it as READ-ONLY during step execution.
- **db/** — **workflow state and results**. JSON files this step produces/consumes (processed records, cursors, cumulative output). Owned by your step. READ first, upsert by the builder-defined key, write back merged. NEVER overwrite wholesale — that destroys rows from other groups/runs.
- **knowledgebase/** — **what the workflow has discovered about the subject matter, durable across runs**. Per-topic narrative markdown under `+"`"+`notes/`+"`"+`, one file per topic (entity-scoped like `+"`"+`company-acme.md`+"`"+` or cross-cutting like `+"`"+`pattern-*`+"`"+`), plus `+"`"+`notes/_index.json`+"`"+` as the registry. When KB access is granted, ALWAYS `+"`"+`cat knowledgebase/notes/_index.json`+"`"+` first to find which topic files exist, then `+"`"+`cat`+"`"+` only the markdown files relevant to your work. NEVER `+"`"+`cat knowledgebase/notes/*.md`+"`"+` — file count grows unboundedly and loading all of them blows context. **Also at `+"`knowledgebase/rules/rules.md`"+`**: user-supplied business rules (Type 3 workflows). When this file exists, READ it once at step start and **respect every applicable rule** — it carries imperatives like *"never X"*, *"always Y"*, regulatory clauses, and persona-specific constraints. Rules are user content; the optimizer is forbidden from rewriting `+"`"+`knowledgebase/rules/`+"`"+` so they remain stable across improvement passes. {{if eq .KbWriteMethod "direct"}}Writes for this step: handled directly per the step's contract — see the **Knowledgebase contribution** block below. Use shell heredoc (new files) or `+"`"+`diff_patch_workspace_file`+"`"+` (existing files), and keep `+"`"+`_index.json`+"`"+` in sync. **Do NOT write to `+"`"+`knowledgebase/rules/`+"`"+`** — that store is user-owned via the `+"`"+`capture_context`+"`"+` tool only.{{else}}Written **only by the post-step KB update agent** (not by you). Do NOT write under `+"`"+`notes/`+"`"+` directly — that breaks the KB update agent's merge discipline. `+"`"+`knowledgebase/rules/`+"`"+` is user-owned via `+"`"+`capture_context`+"`"+` and is never written by step agents.{{end}}
- **learnings/** — **HOW to run the task** (selectors, auth flows, tool patterns). Use it only when relevant learnings are injected under `+"`"+`## Skill`+"`"+` or the folder is listed in Allowed READ.
- **builder/** — prior review/improvement context. At step start, read `+"`builder/review.md`"+` and `+"`builder/improve.md`"+` if they exist. Use unresolved findings, prior failed approaches, active/deferred improvement ideas, and resolved markers as context so you do not repeat known mistakes. Treat these logs as READ-ONLY during step execution.

{{if ne .KbAccess "none"}}Knowledgebase access for this step: **{{.KbAccessLabel}}**.{{if eq .KbAccess "read"}} READ-only: you may `+"`"+`cat`+"`"+` / `+"`"+`jq`+"`"+` the KB files but must not modify them. Selective read recipes:
`+"```"+`bash
# list all topics
jq '.topics[] | {id, file, covers}' knowledgebase/notes/_index.json
# find topics covering a specific entity
jq -r '.topics[] | select(.covers[]? == "company-acme") | .file' knowledgebase/notes/_index.json
# load one specific topic file
cat knowledgebase/notes/company-acme.md
`+"```"+`
{{else if eq .KbWriteMethod "direct"}} Direct write: your step writes narrative to `+"`"+`knowledgebase/notes/`+"`"+` inline — see the **Knowledgebase contribution** block below for exact conventions and discipline. The post-step KB update agent does NOT run for this step — you are the canonical writer.{{else}} Write-scoped (agent method): the folder guard would allow writes, but the post-step KB update agent is the canonical writer — do not edit `+"`"+`notes/`+"`"+` yourself; emit observations in your step output and let the KB agent append to the right topic files.{{end}}
{{end}}
{{if .KBGuidanceBlock}}{{.KBGuidanceBlock}}{{end}}
## EXECUTION RULES
1. **Mandatory Output**: Create `+"`"+`{{.StepContextOutput}}`+"`"+` under `+"`"+`$STEP_OUTPUT_DIR`+"`"+` (step folder: `+"`"+`{{.StepExecutionPath}}/`+"`"+`).
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

{{if eq .HasLearnings "true"}}
## Skill

{{.LearningHistory}}
{{end}}

{{if and .ValidationSchema (ne .IsLearnCodeMode "true")}}
## Validation Schema (Output Requirement)
Your '{{.StepContextOutput}}' MUST match this structure:
{{printf "%s" .ValidationSchema}}
{{end}}

{{if eq .IsEvaluationMode "true"}}
## Evaluation Mode
You are running as an **evaluation agent** — your job is to **verify and assess** outputs from a previous execution run, NOT to create new artifacts.

- **Read** the target execution outputs referenced in your step description
- **Check** whether outputs meet the defined criteria (file existence, content correctness, data quality)
- **Write** your evaluation findings to your context_output file as structured JSON
- **Do NOT** re-execute or modify the original workflow outputs — only read and assess them
- Focus on evidence-based assessment: quote specific content from files, reference exact field values
{{end}}

## Completion
**IMPORTANT**: Do NOT stop with a text message mid-task. Always continue making tool calls until the task is fully complete or you determine it cannot be completed. Only generate a final text response when you are done.

End your response with exactly one of:
- STATUS: COMPLETED — if '{{.StepContextOutput}}' was created successfully.
- STATUS: FAILED — if the step cannot be completed. Explain the reason.`)

var executionOnlyUserTemplate = MustRegisterTemplate("executionOnlyUser", `{{if .OrchestratorInstructions}}## Orchestrator Instructions (HIGHEST PRIORITY)
{{.OrchestratorInstructions}}
{{else}}**DESCRIPTION**: {{.BaseDescription}}
{{end}}{{if eq .IsLearnCodeMode "true"}}**CODE EXEC NOTE**: Implement the task below as reusable Python code. Treat the resolved **Inputs** list and declared tools as the source of truth. If the description contains hardcoded `+"`"+`step-N`+"`"+` paths or interactive browser steps, adapt them into Python logic instead of copying them literally.
{{else}}**CODE EXEC NOTE**: This step is running in normal `+"`"+`code_exec`+"`"+` mode, not `+"`"+`learn_code`+"`"+`. Do **not** try to write one large reusable Python script for the whole task. Prefer calling the available tools and APIs step by step to inspect state, fetch data, and produce outputs. Batching API calls is fine when it improves performance, but keep it task-focused for this run rather than turning it into a reusable `+"`"+`main.py`+"`"+` authoring exercise. Use short one-off shell or Python snippets only when they materially help a specific subtask.
{{end}}**LOCATION**: {{.StepExecutionPath}}/ (Workspace: {{.WorkspacePath}})

{{if .PreviousIterationOutput}}
### Previous Attempt Results
{{.PreviousIterationOutput}}
*Adjust your approach to avoid repeating previous failures.*
{{end}}

{{if .ValidationFeedback}}
### Validation Issues
{{.ValidationFeedback}}
*Fix these errors in your next execution.*
{{end}}

### Inputs
{{if .StepContextDependencies}}{{.StepContextDependencies}}{{else}}None{{end}}

### Output
- **Output File**: {{.StepContextOutput}} (Create in '{{.StepExecutionPath}}/')

{{if .LearnCodePriorContext}}{{.LearnCodePriorContext}}
{{end}}### Execution Checklist
1. Review all **Inputs** above. Inlined files are ready to use. For any marked "read via tool", read them first.
{{if .HasSkill}}2. Read **Skill files** — they contain validated workflows from previous runs.
{{end}}3. Execute the task using tool calls. Do NOT stop mid-task with a text message.
4. **NO FABRICATED DATA**: Every value in the output must come from a real data source (MCP tools, APIs, or input files). Do NOT hardcode or invent output data.
5. Verify the required outputs are fully produced before finishing.
6. Create the output file.`)

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
	LearningFilePaths        string // Learning file paths (when KeepLearningFull is false)
	StepNumber               string // Step identifier (e.g., "step-8" or "step-3-if-true-0")
	StepExecutionPath        string // Full execution folder path (e.g., "execution/step-8")
	PreviousStepsSummary     string // Summary of previous completed steps (titles, descriptions, outputs)
	StepSuccessCriteria      string // Success criteria for the step
	BaseDescription          string // Step description without orchestrator instructions
	OrchestratorInstructions string // Orchestrator instructions (split from description)
	HasSkill                 string // "true" if skill files are available
	IsLearnCodeMode          string // "true" when learn_code mode is enabled
	LearnCodePriorContext    string // Prior script context (failed script + error, or existing script for update)
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
func buildCodeExecBestPractices(isCodeExec bool, templateVars map[string]string) string {
	if !isCodeExec || templateVars["IsLearnCodeMode"] != "true" {
		return ""
	}
	var varMappingLines []string
	if raw := templateVars["LearnCodeVarMapping"]; raw != "" {
		varMappingLines = strings.Split(raw, "\n")
	}
	hasInputArgs := templateVars["StepContextDependencies"] != ""
	return BuildPythonBestPractices(varMappingLines, hasInputArgs)
}

var hardcodedStepPathCmdRegex = regexp.MustCompile(`(?i)cat\s+'?\{WORKSPACE_PATH\}/step-\d+/[^'\s]+`)

func sanitizeLearnCodeDescription(desc string) string {
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
	// Feature flag: KeepLearningFull (set by controller with priority: step config > env var > default false)
	keepLearningFullStr := templateVars["KeepLearningFull"]
	keepLearningFull := keepLearningFullStr == "true"
	stepNumber := templateVars["StepNumber"]               // e.g., "step-8" or "step-3-if-true-0"
	stepExecutionPath := templateVars["StepExecutionPath"] // e.g., "execution/step-8"
	previousStepsSummary := templateVars["PreviousStepsSummary"]
	knowledgebasePath := templateVars["KnowledgebasePath"] // Knowledgebase folder path (persistent files across runs)
	dbPath := templateVars["DBPath"]                       // DB folder path (structured JSON, always enabled)

	// Get current date and time
	now := time.Now()
	currentDate := now.Format("2006-01-02")
	currentTime := now.Format("15:04:05")

	// Build code execution section using common builder
	useCodeStyleRules := isCodeExecutionMode
	codeExecutionSection := BuildCodeExecutionSection(isCodeExecutionMode, workspacePath)

	// Learn code mode: append instructions to write main.py (added on top of code execution section)
	isLearnCodeMode := templateVars["IsLearnCodeMode"] == "true"
	if isLearnCodeMode {
		isRelearnMode := templateVars["IsRelearnMode"] == "true"
		priorScript := templateVars["LearnCodePriorScript"]
		priorError := templateVars["LearnCodePriorError"]
		codeDirAbsPath := filepath.Join(stepExecutionPath, "code")

		// Parse input arg paths from templateVars (newline-separated)
		var inputArgPaths []string
		if raw := templateVars["LearnCodeInputArgs"]; raw != "" {
			inputArgPaths = strings.Split(raw, "\n")
		}

		// Parse env var names from templateVars (newline-separated)
		var envVarNames []string
		if raw := templateVars["LearnCodeEnvVarNames"]; raw != "" {
			envVarNames = strings.Split(raw, "\n")
		}

		// Parse variable→env mapping lines (newline-separated)
		var varMappingLines []string
		if raw := templateVars["LearnCodeVarMapping"]; raw != "" {
			varMappingLines = strings.Split(raw, "\n")
		}

		validationSchemaJSON := templateVars["ValidationSchema"]
		hasBrowser := templateVars["HasBrowserAccess"] == "true"
		isCodeLocked := templateVars["IsLearnCodeLocked"] == "true"
		codeExecutionSection += GetLearnCodeModeInstructions(codeDirAbsPath, stepExecutionPath, isRelearnMode, priorScript, priorError, inputArgPaths, envVarNames, varMappingLines, validationSchemaJSON, hasBrowser, isCodeLocked)
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
		"KeepLearningFull":          fmt.Sprintf("%t", keepLearningFull),
		"VariableNames":             variableNames,
		"VariableValues":            variableValues,
		"VarMapping":                templateVars["LearnCodeVarMapping"], // {{VAR}} → SECRET_VAR mapping (for code exec guidance)
		"UseCodeStyleRules":         useCodeStyleRules,
		"PythonBestPractices":       buildCodeExecBestPractices(isCodeExecutionMode, templateVars),
		"StepNumber":                stepNumber,
		"StepExecutionPath":         stepExecutionPath,
		"PreviousStepsSummary":      previousStepsSummary,
		"ValidationSchema":          validationSchema,                          // Validation schema JSON string
		"KnowledgebasePath":         knowledgebasePath,                         // Knowledgebase folder path
		"DBPath":                    dbPath,                                    // DB folder path (always enabled)
		"KbAccess":                  templateVars["KbAccess"],                  // "read" | "write" | "read-write" | "none"
		"KbAccessLabel":             templateVars["KbAccessLabel"],             // Human-readable label (e.g., "READ/WRITE")
		"KbWriteMethod":             templateVars["KbWriteMethod"],             // "agent" | "direct" — who writes KB (post-step agent vs step itself)
		"KnowledgebaseContribution": templateVars["KnowledgebaseContribution"], // Author-authored instruction for the step's KB contribution (direct mode only)
		"KBGuidanceBlock":           templateVars["KBGuidanceBlock"],           // Pre-built KB guidance block — non-empty only when KbWriteMethod == "direct"
		"FolderGuardReadPaths":      folderGuardReadPaths,                      // Folder guard read paths for agent guidance
		"FolderGuardWritePaths":     folderGuardWritePaths,                     // Folder guard write paths for agent guidance
		"IsEvaluationMode":          templateVars["IsEvaluationMode"],          // Evaluation mode flag
		"IsLearnCodeMode":           templateVars["IsLearnCodeMode"],           // Learn code mode flag (validation schema shown in learn_code section instead)
		"WorkflowRoot":              templateVars["WorkflowRoot"],              // Workflow root path for absolute cwd display
		"DocsRoot":                  GetPromptDocsRoot(),                       // Workspace docs base path — differs between macOS dev (/Users/.../workspace-docs) and Docker (/app/workspace-docs); do NOT hardcode.
		// Browser authoring rules (refs-are-ephemeral + durable-selector priority
		// + canonical DOM probe) apply to every browser step — code-exec throwaway
		// scripts AND learn-code saved main.py. Only the final-artifact permanence
		// differs between modes; the discovery/selector discipline is identical.
		"BrowserAuthoringRules": BrowserAuthoringRulesFromTemplateVars(templateVars),
	})
	if err != nil {
		panic(fmt.Sprintf("execution-only system prompt template execution failed (missing variable?): %v", err))
	}

	return result.String()
}

// executionOnlyUserMessageProcessor generates the user message for execution-only agent
func (hctpeoa *WorkflowExecutionOnlyAgent) executionOnlyUserMessageProcessor(templateVars map[string]string) string {
	// Split description into base description and orchestrator instructions
	fullDescription := templateVars["StepDescription"]
	isLearnCodeMode := templateVars["IsLearnCodeMode"] == "true"
	if isLearnCodeMode {
		fullDescription = sanitizeLearnCodeDescription(fullDescription)
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
		LearningFilePaths:        templateVars["LearningFilePaths"],
		StepNumber:               templateVars["StepNumber"],
		StepExecutionPath:        templateVars["StepExecutionPath"],
		PreviousStepsSummary:     templateVars["PreviousStepsSummary"],
		StepSuccessCriteria:      templateVars["StepSuccessCriteria"],
		HasSkill:                 fmt.Sprintf("%t", templateVars["LearningHistory"] != ""),
		IsLearnCodeMode:          fmt.Sprintf("%t", isLearnCodeMode),
		LearnCodePriorContext:    BuildLearnCodePriorContext(templateVars["LearnCodePriorScript"], templateVars["LearnCodePriorError"], templateVars["LearnCodeMetadataPath"], templateVars["IsLearnCodeLocked"] == "true"),
	}

	// Execute the pre-parsed template
	var result strings.Builder
	if err := executionOnlyUserTemplate.Execute(&result, templateData); err != nil {
		panic(fmt.Sprintf("execution-only user message template execution failed (missing variable?): %v", err))
	}

	return result.String()
}
