package todo_execution

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator/agents"

	"github.com/tmc/langchaingo/llms"
)

// TodoValidationTemplate holds template variables for validation prompts
type TodoValidationTemplate struct {
	Objective           string
	WorkspacePath       string
	ExecutionOutput     string
	StepNumber          int
	TotalSteps          int
	StepTitle           string
	StepSuccessCriteria string
}

// ValidationResponse represents the structured output from validation agent
type ValidationResponse struct {
	IsObjectiveSuccessCriteriaMet bool   `json:"is_objective_success_criteria_met"`
	Feedback                      string `json:"feedback"`
}

// TodoValidationAgent extends BaseOrchestratorAgent with validation functionality
type TodoValidationAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewTodoValidationAgent creates a new validation agent
func NewTodoValidationAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *TodoValidationAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.ValidationAgentType,
		eventBridge,
	)

	return &TodoValidationAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// ExecuteStructured executes the validation agent and returns structured output
func (tva *TodoValidationAgent) ExecuteStructured(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (*ValidationResponse, error) {
	// Define the JSON schema for validation response
	schema := `{
		"type": "object",
		"properties": {
			"is_objective_success_criteria_met": {
				"type": "boolean",
				"description": "True if both objective is completed AND success criteria are met"
			},
			"feedback": {
				"type": "string",
				"description": "Detailed feedback about what was accomplished and what needs improvement"
			}
		},
		"required": ["is_objective_success_criteria_met", "feedback"]
	}`

	// Use the base orchestrator agent's ExecuteStructured method
	result, err := agents.ExecuteStructuredWithInputProcessor[ValidationResponse](tva.BaseOrchestratorAgent, ctx, templateVars, tva.todoValidationInputProcessor, conversationHistory, schema)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// Execute implements the OrchestratorAgent interface
func (tva *TodoValidationAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, []llms.MessageContent, error) {
	// Use ExecuteWithInputProcessor to get agent events (orchestrator_agent_start/end)
	// This will automatically emit agent start/end events
	return tva.ExecuteWithInputProcessor(ctx, templateVars, tva.todoValidationInputProcessor, conversationHistory)
}

// todoValidationInputProcessor processes inputs specifically for single step validation
func (tva *TodoValidationAgent) todoValidationInputProcessor(templateVars map[string]string) string {
	// Parse numeric fields from templateVars
	stepNumber := 0
	totalSteps := 0
	if stepNumStr, exists := templateVars["StepNumber"]; exists {
		if parsed, err := fmt.Sscanf(stepNumStr, "%d", &stepNumber); err != nil || parsed != 1 {
			stepNumber = 0
		}
	}
	if totalStepsStr, exists := templateVars["TotalSteps"]; exists {
		if parsed, err := fmt.Sscanf(totalStepsStr, "%d", &totalSteps); err != nil || parsed != 1 {
			totalSteps = 0
		}
	}

	// Create template data
	templateData := TodoValidationTemplate{
		Objective:           templateVars["StepDescription"], // Use step description as objective
		WorkspacePath:       templateVars["WorkspacePath"],
		ExecutionOutput:     templateVars["ExecutionOutput"],
		StepNumber:          stepNumber,
		TotalSteps:          totalSteps,
		StepTitle:           templateVars["StepTitle"],
		StepSuccessCriteria: templateVars["StepSuccessCriteria"],
	}

	// Define the template for single step validation
	templateStr := `## PRIMARY TASK - BASIC STEP VALIDATION

**STEP**: {{.StepNumber}}/{{.TotalSteps}}
**TITLE**: {{.StepTitle}}
**SUCCESS CRITERIA**: {{.StepSuccessCriteria}}

**EXECUTION OUTPUT TO VALIDATE**:
{{.ExecutionOutput}}

## 🤖 AGENT IDENTITY
- **Role**: Basic Validation Agent
- **Responsibility**: Simple validation - check if objective is done and success criteria are met
- **Mode**: Quick verification only

## 📁 FILE PERMISSIONS
**WRITE:**
- {{.WorkspacePath}}/validation_report.md ONLY

**EXECUTION HISTORY:**
The execution output is provided above in "EXECUTION OUTPUT TO VALIDATE" section.
You do NOT need to read any files - validate based on the execution history provided.

**RESTRICTIONS:**
- Only write to validation_report.md
- Validation is based on the execution history provided, not file contents

## BASIC VALIDATION PROCESS
**Your ONLY job is to answer these 2 simple questions:**

1. **Is the step objective completed?**
   - Did the execution agent accomplish what the step was supposed to do?
   - Analyze the execution history provided above to see what was actually done

2. **Are the success criteria met?**
   - Check each success criterion: {{.StepSuccessCriteria}}
   - Verify if the execution history shows evidence that each criterion was satisfied

## SIMPLE VALIDATION RULES
- **Focus on results, not process**: Don't worry about how it was done, just if it was done
- **Evidence-based**: Look for concrete evidence in the execution history provided
- **Binary decision**: Pass/Fail based on objective completion and criteria satisfaction
- **No complex analysis**: Keep it simple and straightforward
- **No file reading needed**: All information is provided in the execution history above

## STRUCTURED OUTPUT REQUIREMENTS
**CRITICAL**: You must return a structured JSON response with these exact fields:

- **is_objective_success_criteria_met**: boolean (true if BOTH objective completed AND success criteria met)
- **feedback**: string (detailed feedback about what was accomplished and what needs improvement)

**IMPORTANT**: Return ONLY valid JSON that matches the required schema. No explanations or additional text.

## VALIDATION REPORT FORMAT

**CRITICAL**: You are validating ONE SINGLE STEP, not all steps.

**Step Being Validated**: "{{.StepTitle}}" (Step {{.StepNumber}}/{{.TotalSteps}})

Create a focused step-wise validation report that covers ONLY this single step:

# Step Validation Report: "{{.StepTitle}}"

## 📋 Single Step Summary
**IMPORTANT**: This report covers ONLY step "{{.StepTitle}}" ({{.StepNumber}}/{{.TotalSteps}}), not any other steps.

- **Step Number**: {{.StepNumber}}/{{.TotalSteps}}
- **Step Title**: "{{.StepTitle}}"
- **Validation Status**: [PASSED/FAILED]
- **Validation Scope**: Single Step Only (NOT all steps)

## 🔍 Validation Results FOR THIS STEP

### 1. Single Step Objective Completion
**Validating ONLY**: "{{.StepTitle}}"
- **Status**: [COMPLETED/NOT COMPLETED]
- **What Was Attempted**: [What the execution agent tried to do for THIS SPECIFIC STEP]
- **Evidence**: [Summary of what was accomplished for "{{.StepTitle}}" only]

### 2. Single Step Success Criteria Check
**Validating ONLY**: "{{.StepTitle}}"
- **Criteria**: {{.StepSuccessCriteria}}
- **Status**: [MET/NOT MET]
- **Evidence**: [Evidence for each criterion for THIS SPECIFIC STEP only]

## 📊 Overall Assessment FOR THIS STEP ONLY
- **Step**: "{{.StepTitle}}"
- **Final Status**: [PASSED/FAILED]
- **Reason**: [Why THIS SPECIFIC STEP passed or failed]
- **Scope**: ONLY step "{{.StepTitle}}", not other steps

## 💡 Recommendations FOR THIS STEP ONLY
- [Recommendations specifically for "{{.StepTitle}}"]
- **Next Steps**: [How to retry ONLY "{{.StepTitle}}" if it failed]

**CRITICAL REMINDER**: 
- This validation report is ONLY for step "{{.StepTitle}}" ({{.StepNumber}}/{{.TotalSteps}})
- Do NOT validate other steps in this report
- Focus solely on whether THIS SPECIFIC STEP was completed correctly

**Save this report to**: {{.WorkspacePath}}/validation_report.md

**IMPORTANT**: Return ONLY valid JSON that matches the required schema. No explanations or additional text.`

	// Parse and execute the template
	tmpl, err := template.New("validation").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing validation template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing validation template: %v", err)
	}

	return result.String()
}
