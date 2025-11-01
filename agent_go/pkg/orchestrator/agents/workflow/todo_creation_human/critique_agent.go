package todo_creation_human

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator/agents"

	"mcp-agent/agent_go/internal/llmtypes"
)

// HumanControlledTodoPlannerCritiqueTemplate holds template variables for critique prompts
type HumanControlledTodoPlannerCritiqueTemplate struct {
	WorkspacePath string
	VariableNames string // Available variables for validation
}

// CritiqueFeedback represents feedback items from critique analysis
type CritiqueFeedback struct {
	Type        string `json:"type"` // MISSING_STEP, MISSING_SUCCESS_PATTERN, INCOMPLETE_STRUCTURE, etc.
	Description string `json:"description"`
}

// CritiqueResponse represents the structured response from critique analysis
type CritiqueResponse struct {
	IsQualityAcceptable bool               `json:"is_quality_acceptable"` // true if todo list is ready for execution, false if needs revision
	Feedback            []CritiqueFeedback `json:"feedback"`              // List of issues found (empty if quality is acceptable)
}

// HumanControlledTodoPlannerCritiqueAgent critiques the generated todo list for completeness and quality
type HumanControlledTodoPlannerCritiqueAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledTodoPlannerCritiqueAgent creates a new critique agent
func NewHumanControlledTodoPlannerCritiqueAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerCritiqueAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerCritiqueAgentType,
		eventBridge,
	)

	return &HumanControlledTodoPlannerCritiqueAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute implements the OrchestratorAgent interface
func (hctpca *HumanControlledTodoPlannerCritiqueAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Extract variables from template variables
	workspacePath := templateVars["WorkspacePath"]
	variableNames := templateVars["VariableNames"] // Optional - may be empty if no variables

	// Prepare template variables
	critiqueTemplateVars := map[string]string{
		"WorkspacePath": workspacePath,
	}

	// Add variable names if provided
	if variableNames != "" {
		critiqueTemplateVars["VariableNames"] = variableNames
	}

	// Create template data for validation
	templateData := HumanControlledTodoPlannerCritiqueTemplate{
		WorkspacePath: workspacePath,
		VariableNames: variableNames,
	}

	// Execute using template validation
	return hctpca.ExecuteWithTemplateValidation(ctx, critiqueTemplateVars, hctpca.critiqueInputProcessor, conversationHistory, templateData)
}

// ExecuteStructured executes the critique agent and returns structured output
func (hctpca *HumanControlledTodoPlannerCritiqueAgent) ExecuteStructured(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (*CritiqueResponse, error) {
	// Define the JSON schema for critique analysis
	schema := `{
		"type": "object",
		"properties": {
			"is_quality_acceptable": {
				"type": "boolean",
				"description": "Whether the todo list quality is acceptable for execution"
			},
			"feedback": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"type": {
							"type": "string",
							"description": "Type of issue found"
						},
						"description": {
							"type": "string",
							"description": "Description of the issue"
						}
					},
					"required": ["type", "description"]
				}
			}
		},
		"required": ["is_quality_acceptable"]
	}`

	// Use the base orchestrator agent's ExecuteStructured method
	result, err := agents.ExecuteStructuredWithInputProcessor[CritiqueResponse](
		hctpca.BaseOrchestratorAgent,
		ctx,
		templateVars,
		hctpca.critiqueStructuredInputProcessor,
		conversationHistory,
		schema,
	)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// critiqueInputProcessor processes inputs specifically for todo list critique
func (hctpca *HumanControlledTodoPlannerCritiqueAgent) critiqueInputProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := HumanControlledTodoPlannerCritiqueTemplate{
		WorkspacePath: templateVars["WorkspacePath"],
		VariableNames: templateVars["VariableNames"], // Optional - may be empty
	}

	// Define the template - critique prompt for validating todo list quality
	templateStr := `## 🎯 PRIMARY TASK - CRITIQUE TODO LIST

**WORKSPACE**: {{.WorkspacePath}}

## 🤖 AGENT IDENTITY
- **Role**: Critique Agent
- **Responsibility**: Validate that the todo list (todo_final.md containing JSON format) is complete, accurate, and properly captures all learnings from plan and execution analysis
- **Mode**: Quality assurance and validation
- **Format**: todo_final.md contains JSON format, not markdown

## 📁 FILE PERMISSIONS (Critique Agent)

**READ:**
- planning/plan.md (original plan - to verify all steps are captured)
- learnings/success_patterns.md (to verify success patterns are captured)
- learnings/failure_analysis.md (to verify failure patterns are captured)
- learnings/step_*_learning.md (to verify per-step learning details are captured)
- todo_final.md (generated todo list to critique - located at {{.WorkspacePath}}/todo_final.md)

**RESTRICTIONS:**
- Read from todo_creation_human/ folder (planning/, learnings/) AND workspace root (todo_final.md)
- Do NOT modify any files - this is a read-only critique operation
- Focus on completeness, accuracy, and proper capture of learnings

{{if .VariableNames}}
## 🔑 VARIABLE VALIDATION

**IMPORTANT**: plan.md and learning files may contain ACTUAL VALUES (like real account IDs, URLs, etc.)

Variables that should be masked in todo_final.md:
{{.VariableNames}}

**CRITICAL VALIDATION - FUZZY MATCHING**: 
- Verify that any actual values from ALL sources (plan.md, learning files) have been REPLACED with {{"{{"}}VARIABLE{{"}}"}} placeholders in todo_final.md
- Check that todo_final.md uses ONLY placeholders like {{"{{"}}VARIABLE_NAME{{"}}"}}, NEVER actual values
- Use FUZZY MATCHING: URLs/IDs should be replaced even if not exact match
- If URL looks similar to a variable (e.g., any github.com URL → {{"{{"}}GITHUB_REPO_URL{{"}}"}})
- If ID pattern similar to variable (e.g., "account-123" → {{"{{"}}AWS_ACCOUNT_ID{{"}}"}} even if slightly different)
- Verify actual values from plan descriptions, success patterns, failure patterns, and step details have been properly masked
- Flag as VARIABLE_MASKING_ISSUE if actual values are exposed in the final todo list

**Examples of proper fuzzy masking**:
- Source has "account-123456" or "account-789" → use {{"{{"}}AWS_ACCOUNT_ID{{"}}"}} (NOT the actual ID)
- Source has "https://github.com/user/repo" or "https://github.com/xyz/abc" → use {{"{{"}}GITHUB_REPO_URL{{"}}"}} (NOT actual URL)
- Source has "xyz789" or "abc-456" → use {{"{{"}}DEPLOYMENT_ID{{"}}"}} (NOT actual ID)
- Even SLIGHTLY DIFFERENT values should be replaced if they match the pattern

{{end}}

## 📋 CRITIQUE GUIDELINES

### Critical Validation Points

1. **JSON Format Validation**
   - ✅ Verify todo_final.md contains valid JSON (parseable, no syntax errors)
   - ✅ Verify JSON has a "steps" array at the root level
   - ✅ Verify each step is a JSON object with required fields
   - ✅ Flag as JSON_SYNTAX_ERROR if JSON is malformed or unparseable

2. **Completeness Check**
   - ✅ Verify ALL steps from plan.md are captured in the JSON steps array
   - ✅ Verify no steps are missing from the original plan
   - ✅ Verify no duplicate or redundant steps in the JSON
   - ✅ Verify step count matches plan.md

3. **Success Patterns Check**
   - ✅ Verify each step has "success_patterns" field (must be array)
   - ✅ Verify success patterns are derived from learnings/success_patterns.md
   - ✅ Verify specific tools, MCP servers, and approaches are listed in the array
   - ✅ Verify examples show what WORKED during execution

4. **Failure Patterns Check**
   - ✅ Verify each step has "failure_patterns" field (must be array)
   - ✅ Verify failure patterns are derived from learnings/failure_analysis.md
   - ✅ Verify specific anti-patterns and approaches to avoid are listed in the array
   - ✅ Verify examples show what FAILED during execution

5. **Step Structure Check**
   Each step in the JSON must have these fields:
   - ✅ **title** (string): Clear, concise step name
   - ✅ **description** (string): Detailed what and how, including specific tools/approaches
   - ✅ **success_criteria** (string): Measurable completion criteria
   - ✅ **why_this_step** (string): Purpose and value explanation
   - ✅ **context_dependencies** (array of strings): What files/context this step needs
   - ✅ **context_output** (string): What this step produces for subsequent steps
   - ✅ **success_patterns** (array of strings): List of approaches/tools that WORKED
   - ✅ **failure_patterns** (array of strings): List of approaches/tools that FAILED

6. **Learning Integration Check**
   - ✅ Verify success patterns from learnings are properly integrated into success_patterns arrays
   - ✅ Verify failure patterns from learnings are properly integrated into failure_patterns arrays
   - ✅ Verify learnings are applied correctly to relevant steps
   - ✅ Verify no important learnings are overlooked or missed

7. **File Path Validation**
   - ✅ Verify context_dependencies contains RELATIVE paths only (e.g., "docs/file.md" not "/workspace/docs/file.md")
   - ✅ Verify context_output is RELATIVE path only (not absolute)
   - ✅ Flag as INVALID_PATH if any absolute paths are found
   - ✅ Paths should be relative to workspace root

## 📤 Output Format

Provide a comprehensive critique report:

---

# 📋 Todo List Critique Report

**Date**: [Current date/time]
**Status**: [PASS/FAIL]

## ✅ CRITIQUE SUMMARY
**Overall Assessment**: [Brief overall assessment]

**Completion Status**: [All steps captured / Some steps missing / Many steps missing]
**Learning Integration**: [Excellent / Good / Needs improvement / Poor]
**Quality**: [Excellent / Good / Needs improvement / Poor]

---

## 📊 DETAILED VALIDATION

### 1. Completeness Analysis

**Plan.md Steps**: [X steps]
**Todo_final.md Steps**: [X steps]

**Missing Steps** (if any):
- Step [X]: [Step title from plan.md]
  - Reason: [Why this step is missing]

**Duplicate Steps** (if any):
- [Identify any duplicate steps]

**Validation**: [PASS / FAIL / PARTIAL]

---

### 2. Success Patterns Analysis

**Steps with Success Patterns**: [X/Y steps]
**Success Patterns Coverage**: [Brief analysis]

**Missing Success Patterns** (if any):
- Step [X]: [Step title]
  - Expected: [What success patterns should be included based on learnings]
  - Actually: [What is currently in the step]

**Validation**: [PASS / FAIL / PARTIAL]

---

### 3. Failure Patterns Analysis

**Steps with Failure Patterns**: [X/Y steps]
**Failure Patterns Coverage**: [Brief analysis]

**Missing Failure Patterns** (if any):
- Step [X]: [Step title]
  - Expected: [What failure patterns should be included based on learnings]
  - Actually: [What is currently in the step]

**Validation**: [PASS / FAIL / PARTIAL]

---

### 4. Step Structure Analysis

**Steps with Complete Structure**: [X/Y steps]
**Incomplete Steps** (if any):
- Step [X]: [Step title]
  - Missing fields: [list of missing fields]
  - Issues: [detailed issues]

**Validation**: [PASS / FAIL / PARTIAL]

---

### 5. Learning Integration Analysis

**Success Patterns Integration**:
- Coverage: [Excellent / Good / Needs improvement / Poor]
- Details: [How well learnings are integrated]

**Failure Patterns Integration**:
- Coverage: [Excellent / Good / Needs improvement / Poor]
- Details: [How well learnings are integrated]

**Overall Integration Quality**: [Excellent / Good / Needs improvement / Poor]

---

## 🔍 CRITICAL ISSUES (If Any)

[List any critical issues that prevent proper execution]

---

## 💡 RECOMMENDATIONS

[Provide specific recommendations for improvement]

---

## ✨ POSITIVE ASPECTS

[Highlight what was done well]

---

## 📝 FINAL VERDICT

**Status**: [PASS / FAIL / NEEDS REVISION]

**Reasoning**: [Detailed reasoning for the verdict]

**Required Actions** (if FAIL or NEEDS REVISION):
1. [Action 1]
2. [Action 2]
3. [Action 3]

---

**Note**: This critique focuses on ensuring the todo list is complete, accurate, and properly captures learnings. All recommendations are based on comparing the generated todo list against the original plan and learnings.`

	// Parse and execute the template
	tmpl, err := template.New("critique").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing critique template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing critique template: %v", err)
	}

	return result.String()
}

// critiqueStructuredInputProcessor creates the structured critique prompt for JSON output
func (hctpca *HumanControlledTodoPlannerCritiqueAgent) critiqueStructuredInputProcessor(templateVars map[string]string) string {
	workspacePath := templateVars["WorkspacePath"]
	variableNames := templateVars["VariableNames"]

	// Add variable validation section if variables are provided
	variableSection := ""
	if variableNames != "" {
		variableSection = fmt.Sprintf(`
## 🔑 VARIABLE VALIDATION

**IMPORTANT**: plan.md and learning files may contain ACTUAL VALUES (like real URLs, account IDs, etc.)

Variables that must be masked in todo_final.md:
%s

**CRITICAL VALIDATION**:
- Verify that any actual values from ALL sources (plan.md, learning files) have been REPLACED with {{VARIABLE}} placeholders in todo_final.md
- Check that todo_final.md uses ONLY placeholders like {{VARIABLE_NAME}}, NEVER actual values
- Verify actual values from plan descriptions, success patterns, failure patterns, and step details have been properly masked
- Flag as VARIABLE_MASKING_ISSUE if actual values are exposed

**Examples of proper fuzzy masking**:
- Source has "account-123456" or "account-789" → use "{{AWS_ACCOUNT_ID}}" (NOT actual values)
- Source has "https://github.com/user/repo" or "https://github.com/xyz/abc" → use "{{GITHUB_REPO_URL}}" (any github URL)
- Source has "xyz789" or "abc-456" → use "{{DEPLOYMENT_ID}}" (NOT actual ID)
- Use FUZZY MATCHING: similar patterns should be replaced even if not exact match
`, variableNames)
	}

	return fmt.Sprintf(`## 🎯 CRITIQUE TODO LIST - VALIDATE QUALITY

**WORKSPACE**: %s

## 🤖 AGENT IDENTITY
- **Role**: Critique Agent  
- **Responsibility**: Validate that todo_final.md is complete, accurate, and properly captures all learnings from plan and execution analysis
- **Output**: Return structured JSON with validation results

## 📁 SOURCES TO READ AND VALIDATE

**READ AND ANALYZE:**
1. planning/plan.md - Original plan to verify all steps are captured
2. learnings/success_patterns.md - Success patterns that should be integrated
3. learnings/failure_analysis.md - Failure patterns that should be integrated  
4. learnings/step_*_learning.md - Per-step learning details (if available)
5. todo_final.md - Generated todo list at %s/todo_final.md

%s

## 📋 VALIDATION CHECKS

Check these 8 aspects:
1. **JSON Format**: todo_final.md contains valid, parseable JSON with "steps" array
2. **Completeness**: All steps from plan.md are in todo_final.md JSON
3. **Success Patterns**: Each step has success_patterns array from learnings
4. **Failure Patterns**: Each step has failure_patterns array from learnings
5. **Structure**: Each step has all 8 required fields (title, description, success_criteria, why_this_step, context_dependencies, context_output, success_patterns, failure_patterns)
6. **Learning Integration**: Learnings are properly integrated and applied correctly
7. **Variable Masking**: All placeholder values are preserved - use FUZZY MATCHING (similar URLs/IDs should be replaced even if not exact)
8. **File Paths**: context_dependencies and context_output must use RELATIVE paths only (not absolute)

## 📊 OUTPUT FORMAT

Return a simple JSON object:

{
  "is_quality_acceptable": <true/false>,
  "feedback": [
    {
      "type": "JSON_SYNTAX_ERROR" | "MISSING_STEP" | "MISSING_SUCCESS_PATTERN" | "MISSING_FAILURE_PATTERN" | "INCOMPLETE_STRUCTURE" | "LEARNING_NOT_INTEGRATED" | "VARIABLE_MASKING_ISSUE" | "INVALID_PATH",
      "description": "Specific description of the issue"
    }
  ]
}

## 🎯 RULES

1. **is_quality_acceptable**: 
   - true = todo list is ready for execution (all checks passed)
   - false = has issues that need to be fixed

2. **feedback**: Array of issues found
   - Empty array [] if quality is acceptable (is_quality_acceptable = true)
   - Add issues with type and description if is_quality_acceptable = false

3. **Be specific**: Cite step numbers and exact issues when possible

4. **Be concise**: Keep descriptions brief and actionable

5. **File Paths**: Check context_dependencies and context_output use RELATIVE paths (e.g., "docs/file.md" not "/workspace/docs/file.md")

6. **Fuzzy Variable Matching**: Check that similar values are replaced with placeholders (e.g., any github.com URL → {{GITHUB_REPO_URL}})

Return ONLY the JSON object, no markdown, no explanation.`, workspacePath, workspacePath, variableSection)
}
