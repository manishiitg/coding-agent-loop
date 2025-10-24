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

	"github.com/tmc/langchaingo/llms"
)

// HumanControlledTodoPlannerValidationTemplate holds template variables for validation prompts
type HumanControlledTodoPlannerValidationTemplate struct {
	StepNumber              string
	TotalSteps              string
	StepTitle               string
	StepDescription         string
	StepSuccessCriteria     string
	StepWhyThisStep         string
	StepContextDependencies string
	StepContextOutput       string
	WorkspacePath           string
	ExecutionHistory        string
}

// ValidationFeedback represents combined issues and recommendations from validation
type ValidationFeedback struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Severity    string `json:"severity"` // HIGH/MEDIUM/LOW
}

// ValidationResponse represents the structured response from validation analysis
type ValidationResponse struct {
	IsSuccessCriteriaMet bool                 `json:"is_success_criteria_met"`
	ExecutionStatus      string               `json:"execution_status"` // COMPLETED/PARTIAL/FAILED/INCOMPLETE
	Reasoning            string               `json:"reasoning"`
	Feedback             []ValidationFeedback `json:"feedback"`
}

// HumanControlledTodoPlannerValidationAgent validates if tasks were completed properly
type HumanControlledTodoPlannerValidationAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledTodoPlannerValidationAgent creates a new human-controlled todo planner validation agent
func NewHumanControlledTodoPlannerValidationAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerValidationAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerValidationAgentType,
		eventBridge,
	)

	return &HumanControlledTodoPlannerValidationAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute implements the OrchestratorAgent interface
func (hctpva *HumanControlledTodoPlannerValidationAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, []llms.MessageContent, error) {
	// Extract variables from template variables
	stepNumber := templateVars["StepNumber"]
	totalSteps := templateVars["TotalSteps"]
	stepTitle := templateVars["StepTitle"]
	stepDescription := templateVars["StepDescription"]
	stepSuccessCriteria := templateVars["StepSuccessCriteria"]
	stepWhyThisStep := templateVars["StepWhyThisStep"]
	stepContextDependencies := templateVars["StepContextDependencies"]
	stepContextOutput := templateVars["StepContextOutput"]
	workspacePath := templateVars["WorkspacePath"]
	executionHistory := templateVars["ExecutionHistory"]

	// Prepare template variables
	validationTemplateVars := map[string]string{
		"StepNumber":              stepNumber,
		"TotalSteps":              totalSteps,
		"StepTitle":               stepTitle,
		"StepDescription":         stepDescription,
		"StepSuccessCriteria":     stepSuccessCriteria,
		"StepWhyThisStep":         stepWhyThisStep,
		"StepContextDependencies": stepContextDependencies,
		"StepContextOutput":       stepContextOutput,
		"WorkspacePath":           workspacePath,
		"ExecutionHistory":        executionHistory,
	}

	// Create template data for validation
	templateData := HumanControlledTodoPlannerValidationTemplate{
		StepNumber:              stepNumber,
		TotalSteps:              totalSteps,
		StepTitle:               stepTitle,
		StepDescription:         stepDescription,
		StepSuccessCriteria:     stepSuccessCriteria,
		StepWhyThisStep:         stepWhyThisStep,
		StepContextDependencies: stepContextDependencies,
		StepContextOutput:       stepContextOutput,
		WorkspacePath:           workspacePath,
		ExecutionHistory:        executionHistory,
	}

	// Execute using template validation
	return hctpva.ExecuteWithTemplateValidation(ctx, validationTemplateVars, hctpva.humanControlledValidationInputProcessor, conversationHistory, templateData)
}

// ExecuteStructured executes the validation agent and returns structured output
func (hctpva *HumanControlledTodoPlannerValidationAgent) ExecuteStructured(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (*ValidationResponse, error) {
	// Define the JSON schema for validation analysis
	schema := `{
		"type": "object",
		"properties": {
			"is_success_criteria_met": {
				"type": "boolean",
				"description": "Whether the success criteria was met based on execution evidence"
			},
			"execution_status": {
				"type": "string",
				"enum": ["COMPLETED", "PARTIAL", "FAILED", "INCOMPLETE"],
				"description": "Overall status of step execution"
			},
			"reasoning": {
				"type": "string",
				"description": "Detailed reasoning for the validation decision"
			},
			"feedback": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"type": {
							"type": "string",
							"description": "Type of feedback (issue, recommendation, etc.)"
						},
						"description": {
							"type": "string",
							"description": "Description of the feedback"
						},
						"severity": {
							"type": "string",
							"enum": ["HIGH", "MEDIUM", "LOW"],
							"description": "Severity of the feedback"
						}
					},
					"required": ["type", "description", "severity"]
				}
			}
		},
		"required": ["is_success_criteria_met", "execution_status", "reasoning"]
	}`

	// Use the base orchestrator agent's ExecuteStructured method
	result, err := agents.ExecuteStructuredWithInputProcessor[ValidationResponse](hctpva.BaseOrchestratorAgent, ctx, templateVars, hctpva.humanControlledValidationInputProcessor, conversationHistory, schema)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// humanControlledValidationInputProcessor processes inputs specifically for task completion validation
func (hctpva *HumanControlledTodoPlannerValidationAgent) humanControlledValidationInputProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := HumanControlledTodoPlannerValidationTemplate{
		StepNumber:              templateVars["StepNumber"],
		TotalSteps:              templateVars["TotalSteps"],
		StepTitle:               templateVars["StepTitle"],
		StepDescription:         templateVars["StepDescription"],
		StepSuccessCriteria:     templateVars["StepSuccessCriteria"],
		StepWhyThisStep:         templateVars["StepWhyThisStep"],
		StepContextDependencies: templateVars["StepContextDependencies"],
		StepContextOutput:       templateVars["StepContextOutput"],
		WorkspacePath:           templateVars["WorkspacePath"],
		ExecutionHistory:        templateVars["ExecutionHistory"],
	}

	// Define the template
	templateStr := `## 🎯 PRIMARY TASK - VALIDATE STEP {{.StepNumber}} EXECUTION

**STEP**: {{.StepNumber}}/{{.TotalSteps}} - {{.StepTitle}}
**STEP DESCRIPTION**: {{.StepDescription}}
**WORKSPACE**: {{.WorkspacePath}}

### 📋 Complete Step Information
**Success Criteria**: {{.StepSuccessCriteria}}
**Why This Step**: {{.StepWhyThisStep}}
**Context Dependencies**: {{.StepContextDependencies}}
**Context Output**: {{.StepContextOutput}}

### 🔍 Step Context Analysis
**Success Criteria**: Use the success criteria above to verify completion
**Why This Step**: The why this step field explains how this contributes to the objective
**Context Dependencies**: Check if context dependencies files were properly read
**Context Output**: Verify if the context output file was created as specified

**EXECUTION CONVERSATION HISTORY TO VALIDATE**:
{{.ExecutionHistory}}

## 🤖 AGENT IDENTITY
- **Role**: Validation Agent
- **Responsibility**: Verify if step {{.StepNumber}} success criteria was met and execution was completed properly
- **Mode**: Success criteria verification with execution output analysis

## 📁 FILE PERMISSIONS
**READ:**
- planning/plan.json (original plan for reference)
- workspace files (to verify execution claims)

**WRITE:**
- validation/step_{{.StepNumber}}_validation_report.md (validation report with execution summary)

**RESTRICTIONS:**
- Only modify files within {{.WorkspacePath}}/todo_creation_human/
- Write validation report to validation/ folder
- Document execution conversation in validation report
- Focus on verifying execution claims

**EXECUTION CONVERSATION TO VALIDATE**:
{{.ExecutionHistory}}


## 🔍 VALIDATION PROCESS

**Step {{.StepNumber}}/{{.TotalSteps}} - "{{.StepTitle}}"**

**Your Task**: Validate if step {{.StepNumber}} was completed successfully by checking if the SUCCESS CRITERIA was met.

**Success Criteria**: {{.StepSuccessCriteria}}

**Validation Steps**:
1. **Review Execution History**: Analyze conversation for evidence of completion
2. **Check Success Criteria**: Verify if criteria "{{.StepSuccessCriteria}}" was met
3. **Analyze Tool Usage**: Check which tools were used and their results
4. **Assess Evidence**: Identify what worked and what didn't

**Decision**:
- ✅ **PASS**: Success criteria met with sufficient evidence
- ❌ **FAIL**: Success criteria not met or insufficient evidence

## 📤 Output Format

**RETURN STRUCTURED JSON RESPONSE ONLY**

Analyze the execution conversation history and validate if step {{.StepNumber}} was completed successfully. Return a JSON response with the following structure:

The response should be a JSON object with:
- is_success_criteria_met: boolean - Whether the success criteria was met based on execution evidence
- execution_status: string - Overall status (COMPLETED/PARTIAL/FAILED/INCOMPLETE)
- reasoning: string - Detailed reasoning for the validation decision
- feedback: array of objects with type, description, and severity (HIGH/MEDIUM/LOW)

Example JSON structure:
` + "```json" + `
{
  "is_success_criteria_met": true,
  "execution_status": "COMPLETED",
  "reasoning": "The execution conversation shows clear evidence that the success criteria was met. The agent successfully used MCP tools to accomplish the step objective and provided detailed results.",
  "feedback": [
    {
      "type": "Issue",
      "description": "Could have provided more detailed tool output",
      "severity": "LOW"
    },
    {
      "type": "Recommendation",
      "description": "Include more detailed tool output in future executions",
      "severity": "LOW"
    }
  ]
}
` + "```" + `

**Note**: Focus on step {{.StepNumber}} execution conversation analysis. Check if the execution conversation provides sufficient evidence that the success criteria was met. Analyze tool usage and execution results to verify completion. Return structured JSON response only.`

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
