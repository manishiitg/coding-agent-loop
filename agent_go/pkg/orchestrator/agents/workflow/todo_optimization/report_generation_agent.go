package todo_optimization

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
	"mcp-agent/agent_go/pkg/orchestrator/agents/workflow/memory"

	"github.com/tmc/langchaingo/llms"
)

// ReportGenerationTemplate holds template variables for report generation prompts
type ReportGenerationTemplate struct {
	Objective        string
	WorkspacePath    string
	CritiqueFeedback string
	ReportFileName   string
}

// ReportGenerationAgent extends BaseOrchestratorAgent with report generation functionality
type ReportGenerationAgent struct {
	*agents.BaseOrchestratorAgent // ✅ REUSE: All base functionality
}

// NewReportGenerationAgent creates a new report generation agent
func NewReportGenerationAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *ReportGenerationAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.ReportGenerationAgentType, // 🆕 NEW: Agent type
		eventBridge,
	)

	return &ReportGenerationAgent{
		BaseOrchestratorAgent: baseAgent, // ✅ REUSE: All base functionality
	}
}

// Execute implements the OrchestratorAgent interface
func (rga *ReportGenerationAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, []llms.MessageContent, error) {
	// Extract objective from template variables
	objective, ok := templateVars["Objective"]
	if !ok {
		objective = "No objective provided"
	}

	// Extract workspace path from template variables
	workspacePath, ok := templateVars["WorkspacePath"]
	if !ok {
		workspacePath = "No workspace path provided"
	}

	// Extract critique feedback from template variables
	critiqueFeedback, ok := templateVars["CritiqueFeedback"]
	if !ok {
		critiqueFeedback = ""
	}

	// Prepare template variables
	reportTemplateVars := map[string]string{
		"Objective":        objective,
		"WorkspacePath":    workspacePath,
		"CritiqueFeedback": critiqueFeedback,
	}

	// Execute using input processor
	return rga.ExecuteWithInputProcessor(ctx, reportTemplateVars, rga.reportGenerationInputProcessor, conversationHistory)
}

// reportGenerationInputProcessor processes inputs specifically for report generation
func (rga *ReportGenerationAgent) reportGenerationInputProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := ReportGenerationTemplate{
		Objective:        templateVars["Objective"],
		WorkspacePath:    templateVars["WorkspacePath"],
		CritiqueFeedback: templateVars["CritiqueFeedback"],
		ReportFileName:   "technical_execution_report.md",
	}

	// Define the template
	templateStr := `## PRIMARY TASK - GENERATE DETAILED TECHNICAL REPORT

**OBJECTIVE**: {{.Objective}}
**WORKSPACE**: {{.WorkspacePath}}

{{if .CritiqueFeedback}}
## Previous Critique Feedback
Based on the previous critique, incorporate the following feedback to improve the report:

{{.CritiqueFeedback}}

Use this feedback to make the report more accurate, comprehensive, and analytically sound.
{{end}}

**IMPORTANT**: You must do BOTH of the following:
1. **Create reports folder**: Create reports/ folder inside the selected runs/{xxx}/ folder
2. **Save the report** to {{.WorkspacePath}}/runs/{xxx}/reports/{{.ReportFileName}} using workspace tools (OVERWRITE if exists)
3. **Display the complete report** in your response for the user to see

## File Context Instructions
- **WORKSPACE PATH**: {{.WorkspacePath}}
- **Use the correct folder**: Create files in {{.WorkspacePath}} folder
- **Required**: Workspace path is provided to identify the specific folder
- **STRICT BOUNDARY**: ONLY work within the specified {{.WorkspacePath}} folder - do not access other folders

` + memory.GetWorkflowMemoryRequirements() + `

## Instructions
1. **Use workspace path**: {{.WorkspacePath}} to identify the correct folder
2. **Check for runs folders**: Use list_workspace_files to check if there are multiple runs/{xxx} folders
3. **Handle multiple runs folders**: If multiple runs folders exist and user hasn't specified which one:
   - **Ask user to specify**: "Multiple runs folders found: [list folders]. Please specify which runs folder to generate the report for."
   - **Wait for user response**: Do not proceed until user specifies the runs folder
4. **Select runs folder**: Use the specified runs folder or the only available one
5. **Create reports folder**: Create reports/ folder inside the selected runs/{xxx}/ folder
6. **Analyze workflow structure**: Use read_workspace_file to explore the {{.WorkspacePath}} folder structure
7. **Gather execution history**: Read all relevant files to understand the complete workflow execution

## Technical Report Generation Process
8. **Explore workspace structure**: Use list_workspace_files to understand the folder organization
9. **Read key files**: Use read_workspace_file to analyze:
   - Original plan and requirements
   - Evidence/ folder contents (execution details and outputs)
   - Progress/ folder contents (completion status)
   - Selected runs/{xxx}/ folder (execution history and artifacts)
   - Context/ folder (requirements and constraints)
10. **Analyze execution patterns**: Review evidence files to understand what was accomplished
11. **Identify final outputs**: Determine what deliverables were created, what failed, and what remains
12. **Extract technical insights**: Identify key learnings, challenges, and technical recommendations
13. **Synthesize findings**: Combine all information into a comprehensive technical report
14. **Save report**: Save the technical report to {{.WorkspacePath}}/runs/{xxx}/reports/{{.ReportFileName}} (OVERWRITE if exists)

## Technical Report Structure
Create a detailed technical report with the following sections:

# Technical Execution Report

## Executive Summary
- **Objective**: [Original objective]
- **Overall Status**: [Success/Partial/Failed]
- **Key Deliverables**: [Major outputs created]
- **Technical Achievements**: [Significant technical accomplishments]
- **Outstanding Work**: [Remaining technical work]
- **Technical Recommendations**: [Next steps and improvements]

## Technical Overview
- **Total Work Items**: [Number of work items in original plan]
- **Completed Items**: [Number and percentage completed]
- **Failed Items**: [Number and details of technical failures]
- **In Progress**: [Current status of ongoing technical work]

## Detailed Technical Analysis

### Successfully Delivered
[For each completed deliverable:]
- **Deliverable ID**: [e.g., deliverable_001]
- **Title**: [Deliverable title]
- **Technical Status**: [Success/Partial/Failed]
- **Output Files**: [Specific files created and their locations]
- **Technical Evidence**: [File references and key technical outputs]
- **Technical Learnings**: [Important technical insights gained]

### Technical Failures
[For each failed deliverable:]
- **Deliverable ID**: [e.g., deliverable_002]
- **Title**: [Deliverable title]
- **Technical Failure Reason**: [Why it failed technically]
- **Technical Impact**: [Effect on overall objective]
- **Technical Recommendations**: [How to address the technical issues]

### Outstanding Technical Work
[For each incomplete deliverable:]
- **Deliverable ID**: [e.g., deliverable_003]
- **Title**: [Deliverable title]
- **Current Technical Status**: [What's been accomplished technically]
- **Next Technical Steps**: [What needs to be done technically]
- **Technical Dependencies**: [What's blocking technical progress]

## Technical Outputs Summary
- **Files Created**: [List of all technical files created during execution]
- **Key Deliverables**: [Important technical outputs and results]
- **Technical Artifacts**: [Code, configs, documentation, data files]
- **Visual Evidence**: [Screenshots, diagrams, logs if available]
- **Tool Usage**: [MCP tools used and their technical effectiveness]

## Technical Lessons Learned
- **What Worked Well Technically**: [Successful technical patterns and approaches]
- **Technical Challenges Faced**: [Technical obstacles encountered]
- **Tool Technical Effectiveness**: [Which MCP tools were most/least useful technically]
- **Technical Process Improvements**: [Suggestions for better technical workflow]

## Technical Recommendations
- **Immediate Technical Actions**: [What should be done next technically]
- **Technical Process Improvements**: [How to improve future technical workflows]
- **Technical Tool Usage**: [Better ways to use available tools technically]
- **Technical Resource Needs**: [Additional technical resources or tools needed]

## Detailed Technical Metrics
- **Execution Time**: [Total time spent on technical work]
- **Technical Tool Calls**: [Number and types of MCP tool calls made]
- **File Operations**: [Technical files read, created, updated]
- **Technical Error Rate**: [Percentage of failed technical operations]
- **Code Quality Metrics**: [If applicable, code quality indicators]
- **Performance Metrics**: [If applicable, performance measurements]

## Technical Report File Format
Create a detailed technical markdown file with the following structure:

# Technical Execution Report

## Executive Summary
[Comprehensive technical overview of the entire workflow execution]

## Technical Overview
[High-level technical statistics and status]

## Detailed Technical Analysis
[In-depth technical analysis of each deliverable]

## Technical Outputs Summary
[Summary of all technical evidence and outputs]

## Technical Lessons Learned
[Key technical insights and learnings]

## Technical Recommendations
[Actionable technical recommendations for next steps]

## Detailed Technical Metrics
[Comprehensive technical metrics and details]

## Critical Technical Requirements
- **CRITICAL**: Check for multiple runs folders and ask user to specify which one if needed
- **CRITICAL**: Create reports/ folder inside the selected runs/{xxx}/ folder
- **CRITICAL**: Analyze ALL technical evidence files in the selected runs/{xxx}/ folder
- **CRITICAL**: Provide specific technical file references for all claims
- **CRITICAL**: Include actionable technical recommendations
- **CRITICAL**: Document both technical successes and failures honestly
- **CRITICAL**: Focus on final technical outputs and deliverables
- **CRITICAL**: Save the technical report to {{.WorkspacePath}}/runs/{xxx}/reports/{{.ReportFileName}} (OVERWRITE if exists)
- **CRITICAL**: Always use the same filename "{{.ReportFileName}}" to ensure consistent file updates`

	// Parse and execute the template
	tmpl, err := template.New("reportGeneration").Parse(templateStr)
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
