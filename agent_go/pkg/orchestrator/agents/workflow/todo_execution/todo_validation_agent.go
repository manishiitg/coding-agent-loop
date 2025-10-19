package todo_execution

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/orchestrator"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
	"mcp-agent/agent_go/pkg/orchestrator/agents/workflow/memory"

	"github.com/tmc/langchaingo/llms"
)

// TodoValidationTemplate holds template variables for todo validation prompts
type TodoValidationTemplate struct {
	Objective       string
	WorkspacePath   string
	ExecutionOutput string
}

// TodoValidationAgent extends BaseOrchestratorAgent with todo validation functionality
type TodoValidationAgent struct {
	*agents.BaseOrchestratorAgent // ✅ REUSE: All base functionality
}

// NewTodoValidationAgent creates a new todo validation agent
func NewTodoValidationAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge orchestrator.EventBridge) *TodoValidationAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoValidationAgentType, // 🆕 NEW: Agent type
		eventBridge,
	)

	return &TodoValidationAgent{
		BaseOrchestratorAgent: baseAgent, // ✅ REUSE: All base functionality
	}
}

// GetBaseAgent implements the OrchestratorAgent interface
func (tva *TodoValidationAgent) GetBaseAgent() *agents.BaseAgent {
	return tva.BaseOrchestratorAgent.BaseAgent()
}

// Execute implements the OrchestratorAgent interface
func (tva *TodoValidationAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, error) {
	// Extract required parameters
	objective, ok := templateVars["Objective"]
	if !ok {
		objective = "No objective provided"
	}

	workspacePath, ok := templateVars["WorkspacePath"]
	if !ok {
		workspacePath = "No workspace path provided"
	}

	executionOutput, ok := templateVars["ExecutionOutput"]
	if !ok {
		executionOutput = "No execution output provided"
	}

	// Prepare template variables
	validationTemplateVars := map[string]string{
		"Objective":       objective,
		"WorkspacePath":   workspacePath,
		"ExecutionOutput": executionOutput,
	}

	// Execute using input processor
	return tva.ExecuteWithInputProcessor(ctx, validationTemplateVars, tva.todoValidationInputProcessor, conversationHistory)
}

// todoValidationInputProcessor processes inputs specifically for todo validation
func (tva *TodoValidationAgent) todoValidationInputProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := TodoValidationTemplate{
		Objective:       templateVars["Objective"],
		WorkspacePath:   templateVars["WorkspacePath"],
		ExecutionOutput: templateVars["ExecutionOutput"],
	}

	// Define the template
	templateStr := `## PRIMARY TASK - VALIDATE TODO COMPLETIONS

**OBJECTIVE**: {{.Objective}}
**WORKSPACE**: {{.WorkspacePath}}

**EXECUTION OUTPUT TO VALIDATE**:
{{.ExecutionOutput}}

**IMPORTANT**: You must do the following:
1. **Read the todo_final.md snapshot** from {{.WorkspacePath}}/todo_snapshot.md (READ ONLY)
2. **Analyze the provided execution output** above to understand what was accomplished
3. **Validate against success criteria** using the execution evidence provided for all completed todos
4. **Check for hallucinations and assumptions** in the execution output
5. **Save validation report** to runs/{xxx}/outputs/validation_report.md
6. **Provide detailed validation results** and recommendations

## File Context Instructions
- **WORKSPACE PATH**: {{.WorkspacePath}}
- **Use the correct folder**: Read from {{.WorkspacePath}}/todo_snapshot.md
- **Required**: Workspace path is provided to identify the specific folder
- **STRICT BOUNDARY**: ONLY work within the specified {{.WorkspacePath}} folder - do not access other folders

## ⚠️ CRITICAL RESTRICTIONS
- **READ ONLY for todo_final.md snapshot**: Never modify the {{.WorkspacePath}}/todo_snapshot.md file
- **NO UPDATES**: This agent only reads and validates - never updates todo files
- **Only read execution outputs**: Focus on validating execution results, not modifying todo lists

` + memory.GetWorkflowMemoryRequirements() + `

## Validation Process
1. **Read todo_final.md snapshot**: Use read_workspace_file to read the todo_snapshot.md file in {{.WorkspacePath}}/todo_snapshot.md
2. **Analyze execution output**: Review the provided execution output above to understand what was accomplished
3. **Read supporting files**: Check data/ and artifacts/ folders for additional evidence if needed
4. **Parse todo snapshot**: Identify which todos were executed and their success criteria
5. **CRITICAL: Verify all claims**: Use MCP tools to verify every factual claim made by execution agent
6. **Check for hallucinations**: Look for information that wasn't actually obtained from tools
7. **Identify assumptions**: Find unverified assumptions made by execution agent
8. **Cross-reference evidence**: Ensure execution evidence matches actual accomplishments
9. **Generate validation report**: Create comprehensive validation results with verification details
10. **Save validation report**: Use update_workspace_file to save the validation report to runs/{xxx}/outputs/validation_report.md

## Execution Output Analysis
The execution output provided above contains:
- **Execution summary**: What was accomplished during execution
- **Success criteria status**: Which criteria were claimed to be met
- **Tool usage**: MCP tools used and their results
- **Evidence references**: Links to files and data created
- **Results**: Any data or outputs generated

## Additional Files to Check (if needed)
From runs/{date}/outputs/ folder:
- **data/**: Any data files created during execution
- **artifacts/**: Any artifacts generated during execution

## Validation Guidelines
- **Verify factual accuracy**: Cross-check any facts, data, or claims against reliable sources
- **Validate tool outputs**: Ensure MCP tool results are correctly interpreted and not misrepresented
- **Check for made-up information**: Look for information that wasn't actually obtained from tools
- **Verify file operations**: Confirm files were actually created/modified as claimed
- **Validate success criteria**: Check if criteria were actually met, not just claimed to be met
- **Use MCP tools to verify results**: Double-check execution claims with available tools

### 1. Fact Verification
- **Cross-check facts**: Use MCP tools to verify any factual claims
- **Verify data accuracy**: Check if data matches what was actually obtained
- **Confirm tool outputs**: Ensure tool results are correctly reported

### 2. Evidence Validation
- **File existence**: Verify files were actually created as claimed
- **Content verification**: Check if file contents match execution claims
- **Tool usage verification**: Confirm tools were actually used as described

### 3. Assumption Detection
- **Look for unsupported claims**: Statements without evidence
- **Check for logical leaps**: Conclusions not supported by evidence
- **Identify missing verification**: Claims that weren't verified with tools

### 4. Red Flags to Watch For
- **Specific details without sources**: Detailed information not obtained from tools
- **Confident statements without evidence**: Claims without supporting data
- **Perfect results**: Results that seem too good to be true
- **Missing tool outputs**: Claims about tool usage without actual outputs
- **Inconsistent information**: Contradictory statements in execution report

## File Output Requirements
**CRITICAL**: Save the validation report to a file:
- **File path**: runs/{xxx}/outputs/validation_report.md
- **Use update_workspace_file** to create the validation report file
- **Include complete validation report** in the file
- **Also provide the report** in your response for immediate review

## Validation Report Format
Provide a detailed validation report:

# Todo Validation Report

## Execution Summary
- **Total Todos Validated**: [Number of todos validated]
- **Todos Passed**: [Number of todos that passed validation]
- **Todos Failed**: [Number of todos that failed validation]
- **Overall Status**: [Passed/Failed/Partial]

## Individual Todo Validations
### Todo 1: [Todo ID]
- **Title**: [Todo title]
- **Description**: [Todo description]
- **Success Criteria**: [List of criteria to validate]
- **Validation Status**: [Passed/Failed/Partial]
- **Factual Claims Verified**: [List of facts checked and their accuracy]
- **Tool Outputs Validated**: [Verification of MCP tool results]
- **File Operations Confirmed**: [Files actually created/modified vs claimed]
- **Data Accuracy Checked**: [Verification of any data generated]
- **Assumptions Identified**: [Any assumptions made by execution agent]
- **Hallucinations Found**: [Any made-up or incorrect information]

### Todo 2: [Todo ID]
- **Title**: [Todo title]
- **Description**: [Todo description]
- **Success Criteria**: [List of criteria to validate]
- **Validation Status**: [Passed/Failed/Partial]
- **Factual Claims Verified**: [List of facts checked and their accuracy]
- **Tool Outputs Validated**: [Verification of MCP tool results]
- **File Operations Confirmed**: [Files actually created/modified vs claimed]
- **Data Accuracy Checked**: [Verification of any data generated]
- **Assumptions Identified**: [Any assumptions made by execution agent]
- **Hallucinations Found**: [Any made-up or incorrect information]

## Overall Validation Results
- [x] Criterion 1: [Status and evidence - VERIFIED across all todos]
- [x] Criterion 2: [Status and evidence - VERIFIED across all todos]
- [ ] Criterion 3: [Status and evidence - NOT VERIFIED]

## Evidence Analysis
- **Execution Output Analysis**: [Summary of what was actually accomplished based on provided output]
- **Files Created**: [List of files actually created/modified - VERIFIED]
- **Data Generated**: [Any data or results produced - VERIFIED]
- **Tool Usage**: [MCP tools used and their actual outputs]

## Issues Found
- **Hallucinations**: [Any made-up information or false claims across all todos]
- **Assumptions**: [Unverified assumptions made by execution agent]
- **Missing Evidence**: [Claims without supporting evidence]
- **Incorrect Interpretations**: [Misinterpreted tool outputs or data]

## Validation Summary
- **Overall Status**: [Passed/Failed/Partial - based on verification]
- **Hallucination Risk**: [High/Medium/Low - likelihood of false information]
- **Assumption Risk**: [High/Medium/Low - likelihood of unverified assumptions]
- **Confidence Level**: [High/Medium/Low - based on verification completeness]
- **Key Findings**: [Main verification results and critical issues]

Focus on thorough validation using the execution outputs and providing actionable feedback.`

	// Parse and execute the template
	tmpl, err := template.New("todoValidation").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing template: %v", err)
	}

	var result strings.Builder
	err = tmpl.Execute(&result, templateData)
	if err != nil {
		return fmt.Sprintf("Error executing template: %v", err)
	}

	return result.String()
}

// GetPrompts returns nil since we use input processor
func (tva *TodoValidationAgent) GetPrompts() interface{} {
	return nil
}
