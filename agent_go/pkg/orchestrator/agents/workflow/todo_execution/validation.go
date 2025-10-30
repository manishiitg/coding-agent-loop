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

**VALIDATION APPROACH:**
- **Primary Source**: Use the execution output provided above in "EXECUTION OUTPUT TO VALIDATE"
- **File Access**: You may read workspace files if needed to verify artifacts were created
- **Focus**: Validate based on what was accomplished, using execution history as primary evidence

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

**IMPORTANT**: You are validating step "{{.StepTitle}}" (Step {{.StepNumber}}/{{.TotalSteps}}) only.

Create a focused validation report for this single step:

# Step Validation Report: "{{.StepTitle}}"

## 📋 Step Summary
- **Step Number**: {{.StepNumber}}/{{.TotalSteps}}
- **Step Title**: "{{.StepTitle}}"
- **Validation Status**: [PASSED/FAILED]

## 🔍 Validation Results

### 1. Objective Completion
- **Status**: [COMPLETED/NOT COMPLETED]
- **What Was Attempted**: [What the execution agent tried to do]
- **Evidence**: [Summary of what was accomplished]

### 2. Success Criteria Check
- **Criteria**: {{.StepSuccessCriteria}}
- **Status**: [MET/NOT MET]
- **Evidence**: [Evidence for each criterion]

## 📊 Overall Assessment
- **Final Status**: [PASSED/FAILED]
- **Reason**: [Why this step passed or failed]

## 💡 Recommendations
- [Specific recommendations for this step]
- **Next Steps**: [How to improve if retry is needed]

**Save this report to**: {{.WorkspacePath}}/validation_report.md

**REMINDER**: Focus on validating step "{{.StepTitle}}" only. Return ONLY valid JSON matching the required schema.`

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
