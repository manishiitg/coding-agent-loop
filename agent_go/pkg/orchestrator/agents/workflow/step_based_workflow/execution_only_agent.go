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

{{if .VariableNames}}
## Variables
{{.VariableNames}}
{{if .VariableValues}}**Values**: {{.VariableValues}}{{end}}

**Handling**: Step descriptions are already resolved. For code and tool calls, use the resolved values directly.
{{if and .UseCodeStyleRules .VarMapping}}**Env var access** (VAR_* for variables, SECRET_* for credentials, never hardcode): {{.VarMapping}}{{end}}
{{end}}

## Workspace & Paths

All paths are absolute. Always use `+"`"+`mkdir -p`+"`"+` before writing if the directory may not exist. Wrap paths in single quotes in shell commands (folder names may contain spaces).

| Path | Location |
|------|----------|
| Base | `+"`"+`/app/workspace-docs/`+"`"+` |
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

**Three persistent stores — do not confuse them:**
- **db/** — **workflow state and results**. JSON files this step produces/consumes (processed records, cursors, cumulative output). Owned by your step. READ first, upsert by the builder-defined key, write back merged. NEVER overwrite wholesale — that destroys rows from other groups/runs.
- **knowledgebase/** — **what the workflow has discovered about the subject matter, durable across runs**. Two formats live here, both written **only by the post-step KB update agent** (not by you):
  - `+"`"+`graph.json`+"`"+` + `+"`"+`index.json`+"`"+` — atomic facts as entities and relationships. Read with `+"`"+`cat`+"`"+` / `+"`"+`jq`+"`"+`.
  - `+"`"+`notes/`+"`"+` — per-topic narrative markdown, one file per topic (entity-scoped or `+"`"+`pattern-*`+"`"+`). **Read discipline:** ALWAYS `+"`"+`cat knowledgebase/notes/_index.json`+"`"+` first to find which topic files exist, then `+"`"+`cat`+"`"+` only the markdown files relevant to your work. NEVER `+"`"+`cat knowledgebase/notes/*.md`+"`"+` — file count grows unboundedly and loading all of them blows context.
  - Do NOT write `+"`"+`graph.json`+"`"+`, `+"`"+`index.json`+"`"+`, or any file under `+"`"+`notes/`+"`"+` directly — that breaks the KB update agent's merge discipline.
- **learnings/** — **HOW to run the task** (selectors, auth flows, tool patterns). Read-only for you; the learning agent maintains it after the step. Relevant learnings are already injected under `+"`"+`## Skill`+"`"+` below when applicable.

{{if ne .KbAccess "none"}}Knowledgebase access for this step: **{{.KbAccessLabel}}**.{{if eq .KbAccess "read"}} READ-only: you may `+"`"+`cat`+"`"+` / `+"`"+`jq`+"`"+` the KB files but must not modify them. Selective read recipes:
` + "```" + `bash
# entity by id
jq '.entities[] | select(.id == "company-acme")' knowledgebase/graph.json
# relationships touching an entity
jq '.relationships[] | select(.from == "company-acme" or .to == "company-acme")' knowledgebase/graph.json
# narrative topics covering an entity (returns filenames)
jq -r '.topics[] | select(.covers[]? == "company-acme") | .file' knowledgebase/notes/_index.json
# load one specific topic file
cat knowledgebase/notes/company-acme.md
` + "```" + `
{{else if eq .KbAccess "write"}} Write-scoped: the folder guard allows writes, but the post-step KB update agent is the canonical writer — do not edit `+"`"+`graph.json`+"`"+` / `+"`"+`index.json`+"`"+` / `+"`"+`notes/`+"`"+` yourself; emit facts in your step output and let the KB agent merge.{{end}}
{{end}}
## EXECUTION RULES
1. **Mandatory Output**: Create `+"`"+`{{.StepContextOutput}}`+"`"+` in `+"`"+`{{.StepExecutionPath}}/`+"`"+`.
{{if .UseCodeStyleRules}}2. Use absolute paths in code. E.g., `+"`"+`open("{{.StepExecutionPath}}/{{.StepContextOutput}}", "w")`+"`"+`.
3. **No env var fallbacks in Python**: always `+"`"+`os.environ['KEY']`+"`"+` — never `+"`"+`os.environ.get('KEY', 'default')`+"`"+`. Variables use `+"`"+`VAR_<NAME>`+"`"+`, secrets use `+"`"+`SECRET_<NAME>`+"`"+`. Missing var must raise KeyError, not silently use a hardcoded value.
{{else}}2. Use absolute paths in shell commands. E.g., `+"`"+`echo '...' > '{{.StepExecutionPath}}/{{.StepContextOutput}}'`+"`"+`.
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

{{if .DecisionEvaluationQuestion}}
## Output Formatting for Evaluation
**Evaluation Question**: {{.DecisionEvaluationQuestion}}
Include:
1. **Clear Status**: Succeeded or Failed.
2. **Evidence**: Specific details (file sizes, grep matches, API status codes) that answer the evaluation question.
{{end}}

## Completion
**IMPORTANT**: Do NOT stop with a text message mid-task. Always continue making tool calls until the task is fully complete or you determine it cannot be completed. Only generate a final text response when you are done.

End your response with exactly one of:
- STATUS: COMPLETED — if '{{.StepContextOutput}}' was created successfully.
- STATUS: FAILED — if the step cannot be completed. Explain the reason.`)

var executionOnlyUserTemplate = MustRegisterTemplate("executionOnlyUser", `{{if .WorkshopHumanFeedback}}## 🚨 HUMAN FEEDBACK (CRITICAL — READ FIRST)
{{.WorkshopHumanFeedback}}

{{end}}{{if .OrchestratorInstructions}}## Orchestrator Instructions (HIGHEST PRIORITY)
{{.OrchestratorInstructions}}
{{else}}**DESCRIPTION**: {{.BaseDescription}}
{{end}}{{if eq .IsLearnCodeMode "true"}}**CODE EXEC NOTE**: Implement the task below as reusable Python code. Treat the resolved **Inputs** list and declared tools as the source of truth. If the description contains hardcoded `+"`"+`step-N`+"`"+` paths or interactive browser steps, adapt them into Python logic instead of copying them literally.
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

{{if .DecisionReasoning}}
### Routing Context
{{.DecisionReasoning}}
*Consider why you were routed to this step during execution.*
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
	StepTitle                  string
	StepDescription            string
	StepContextDependencies    string
	WorkshopHumanFeedback      string // Human feedback from workshop execute_step or run_full_workflow human_inputs (shown at top, highest priority)
	StepContextOutput          string
	WorkspacePath              string
	IsCodeExecutionMode        string // "true" or "false" - indicates if code execution mode is enabled
	ValidationFeedback         string
	PreviousIterationOutput    string // Previous iteration execution output
	VariableNames              string // Variable names with descriptions ({{VAR_NAME}} - description)
	VariableValues             string // Variable names with actual values ({{VAR_NAME}} = value)
	LearningHistory            string // Formatted learning conversation history (REQUIRED for execution-only mode)
	LearningFilePaths          string // Learning file paths (when KeepLearningFull is false)
	StepNumber                 string // Step identifier (e.g., "step-8" or "step-3-if-true-0")
	StepExecutionPath          string // Full execution folder path (e.g., "execution/step-8")
	DecisionReasoning          string // Context from decision step that routed to this step (empty if not routed from decision)
	DecisionEvaluationQuestion string // Evaluation question for decision inner steps (used to format output for LLM evaluation)
	PreviousStepsSummary       string // Summary of previous completed steps (titles, descriptions, outputs)
	StepSuccessCriteria        string // Success criteria for the step
	BaseDescription            string // Step description without orchestrator instructions
	OrchestratorInstructions   string // Orchestrator instructions (split from description)
	HasSkill                   string // "true" if skill files are available
	IsLearnCodeMode            string // "true" when learn_code mode is enabled
	LearnCodePriorContext      string // Prior script context (failed script + error, or existing script for update)
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

// buildCodeExecBestPractices returns the Python best practices section for code_exec mode.
// Returns "" when not in code execution mode or no variables/inputs present.
func buildCodeExecBestPractices(isCodeExec bool, templateVars map[string]string) string {
	if !isCodeExec || templateVars["IsLearnCodeMode"] == "true" {
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
		codeExecutionSection += GetLearnCodeModeInstructions(codeDirAbsPath, stepExecutionPath, isRelearnMode, priorScript, priorError, inputArgPaths, envVarNames, varMappingLines, validationSchemaJSON)
	}

	// Get variable names and values for system prompt
	variableNames := templateVars["VariableNames"]
	variableValues := templateVars["VariableValues"]
	decisionEvaluationQuestion := templateVars["DecisionEvaluationQuestion"]
	validationSchema := templateVars["ValidationSchema"] // Validation schema JSON string
	folderGuardReadPaths := templateVars["FolderGuardReadPaths"]
	folderGuardWritePaths := templateVars["FolderGuardWritePaths"]

	// Execute the pre-parsed template
	var result strings.Builder
	err := executionOnlySystemTemplate.Execute(&result, map[string]interface{}{
		"WorkspacePath":              workspacePath,
		"IsCodeExecutionMode":        isCodeExecutionMode,
		"CodeExecutionSection":       codeExecutionSection,
		"StepContextOutput":          stepContextOutput,
		"CurrentDate":                currentDate,
		"CurrentTime":                currentTime,
		"LearningHistory":            learningHistory,
		"HasLearnings":               fmt.Sprintf("%t", learningHistory != ""),
		"KeepLearningFull":           fmt.Sprintf("%t", keepLearningFull),
		"VariableNames":              variableNames,
		"VariableValues":             variableValues,
		"VarMapping":                 templateVars["LearnCodeVarMapping"], // {{VAR}} → SECRET_VAR mapping (for code exec guidance)
		"UseCodeStyleRules":          useCodeStyleRules,
		"PythonBestPractices":        buildCodeExecBestPractices(isCodeExecutionMode, templateVars),
		"StepNumber":                 stepNumber,
		"StepExecutionPath":          stepExecutionPath,
		"PreviousStepsSummary":       previousStepsSummary,
		"DecisionEvaluationQuestion": decisionEvaluationQuestion,
		"ValidationSchema":           validationSchema,                     // Validation schema JSON string
		"KnowledgebasePath":          knowledgebasePath,                    // Knowledgebase folder path
		"DBPath":                     dbPath,                               // DB folder path (always enabled)
		"KbAccess":                   templateVars["KbAccess"],             // "read" | "write" | "read-write" | "none"
		"KbAccessLabel":              templateVars["KbAccessLabel"],        // Human-readable label (e.g., "READ/WRITE")
		"FolderGuardReadPaths":       folderGuardReadPaths,                 // Folder guard read paths for agent guidance
		"FolderGuardWritePaths":      folderGuardWritePaths,                // Folder guard write paths for agent guidance
		"IsEvaluationMode":           templateVars["IsEvaluationMode"],     // Evaluation mode flag
		"IsLearnCodeMode":            templateVars["IsLearnCodeMode"],      // Learn code mode flag (validation schema shown in learn_code section instead)
		"WorkflowRoot":               templateVars["WorkflowRoot"],         // Workflow root path for absolute cwd display
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
		DecisionReasoning:        templateVars["DecisionReasoning"],
		PreviousStepsSummary:     templateVars["PreviousStepsSummary"],
		WorkshopHumanFeedback:    templateVars["WorkshopHumanFeedback"],
		StepSuccessCriteria:      templateVars["StepSuccessCriteria"],
		HasSkill:                 fmt.Sprintf("%t", templateVars["LearningHistory"] != ""),
		IsLearnCodeMode:          fmt.Sprintf("%t", isLearnCodeMode),
		LearnCodePriorContext:    BuildLearnCodePriorContext(templateVars["LearnCodePriorScript"], templateVars["LearnCodePriorError"], templateVars["LearnCodeMetadataPath"]),
	}

	// Execute the pre-parsed template
	var result strings.Builder
	if err := executionOnlyUserTemplate.Execute(&result, templateData); err != nil {
		panic(fmt.Sprintf("execution-only user message template execution failed (missing variable?): %v", err))
	}

	return result.String()
}
