package step_based_workflow

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/guidance"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/agents"
	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// Pre-parsed templates for Orchestrator orchestrator agent - panics at startup if invalid
var orchestratorSystemTemplate = MustRegisterTemplate("orchestratorSystem", guidance.StepSystemPromptTemplate("orchestrator"))

var orchestratorUserTemplate = MustRegisterTemplate("orchestratorUser", `## Step: {{.StepTitle}}

{{.StepDescription}}

{{if .StepContextDependencies}}
## Input Dependencies
The following files from previous steps are available for reading:
{{.StepContextDependencies}}
{{end}}

{{if .WorkshopHumanInput}}
## Human Input (Highest Priority)
The operator supplied this input with execute_step(..., human_input=...).
You MUST incorporate it into this run. It takes priority over the default step description where they conflict.

{{.WorkshopHumanInput}}
{{end}}

{{if .ValidationFeedback}}
## Pre-Validation Failed (Previous Attempt)
{{.ValidationFeedback}}
Fix the issues above — ensure all required output files are generated in the step folder.
{{end}}

Execute the step objective. Use sub-agents for specialized tasks and direct execution for everything else. Run all tasks to completion.`)

// WorkflowOrchestratorAgent executes the main todo task orchestration step
// This agent manages a todo list and delegates work to predefined or generic sub-agents
type WorkflowOrchestratorAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewWorkflowOrchestratorAgent creates a new todo task orchestrator agent
func NewWorkflowOrchestratorAgent(
	config *agents.OrchestratorAgentConfig,
	logger loggerv2.Logger,
	tracer observability.Tracer,
	eventBridge mcpagent.AgentEventListener,
) *WorkflowOrchestratorAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.OrchestratorAgentType,
		eventBridge,
	)

	return &WorkflowOrchestratorAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// OrchestratorTemplate holds template variables for todo task orchestrator agent prompts
type OrchestratorTemplate struct {
	StepTitle               string
	StepDescription         string
	StepSuccessCriteria     string
	StepContextDependencies string
	WorkspacePath           string
	StepNumber              string
	StepExecutionPath       string
	PreviousStepsSummary    string
	PredefinedRoutes        string // Description of predefined sub-agents
	VariableNames           string
	VariableValues          string
	IsCodeExecutionMode     bool
	LearningHistory         string
}

// Execute implements the OrchestratorAgent interface
// The agent delegates work to sub-agents via tools and runs to completion in a single shot.
func (agent *WorkflowOrchestratorAgent) Execute(
	ctx context.Context,
	templateVars map[string]string,
	conversationHistory []llmtypes.MessageContent,
) (string, []llmtypes.MessageContent, error) {
	// Generate system prompt and user message
	systemPrompt := agent.orchestratorSystemPromptProcessor(templateVars)
	userMessage := agent.orchestratorUserMessageProcessor(templateVars, conversationHistory)

	// Create input processor
	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	// Execute using base agent with template validation (regular tool-based execution)
	result, updatedHistory, err := agent.BaseOrchestratorAgent.ExecuteWithTemplateValidation(
		ctx,
		templateVars,
		inputProcessor,
		conversationHistory,
		nil,          // templateData - not needed
		systemPrompt, // systemPrompt
		true,         // overwriteSystemPrompt
	)
	if err != nil {
		return "", nil, fmt.Errorf("todo task orchestrator execution failed: %w", err)
	}

	return result, updatedHistory, nil
}

// orchestratorSystemPromptProcessor generates the system prompt for todo task orchestrator agent
func (agent *WorkflowOrchestratorAgent) orchestratorSystemPromptProcessor(templateVars map[string]string) string {
	now := time.Now()
	learningHistory := templateVars["LearningHistory"]
	var config *agents.OrchestratorAgentConfig
	if agent != nil && agent.BaseOrchestratorAgent != nil {
		config = agent.BaseOrchestratorAgent.GetConfig()
	}
	if usesProjectedReferenceSkills(config, templateVars) {
		learningHistory = ""
	}

	templateData := map[string]interface{}{
		"CurrentDate":               now.Format("2006-01-02"),
		"CurrentTime":               now.Format("15:04:05"),
		"PredefinedRoutes":          templateVars["PredefinedRoutes"],
		"VariableNames":             templateVars["VariableNames"],
		"VariableValues":            templateVars["VariableValues"],
		"LearningHistory":           learningHistory,
		"StepExecutionPath":         templateVars["StepExecutionPath"],
		"DownloadsPath":             templateVars["DownloadsPath"],
		"ExecutionFolderPath":       templateVars["ExecutionFolderPath"],
		"WorkspacePath":             templateVars["WorkspacePath"],
		"WorkflowRoot":              templateVars["WorkflowRoot"],
		"KnowledgebasePath":         templateVars["KnowledgebasePath"],
		"DBPath":                    templateVars["DBPath"],
		"DBAccess":                  templateVars["DBAccess"],
		"DBDirectAccess":            templateVars["DBDirectAccess"],
		"DBGuidance":                BuildManagedWorkflowDBGuidance(templateVars["DBAccess"]),
		"FolderGuardReadPaths":      templateVars["FolderGuardReadPaths"],
		"FolderGuardWritePaths":     templateVars["FolderGuardWritePaths"],
		"ShowToolsSection":          templateVars["ShowToolsSection"] == "true",
		"KbAccess":                  templateVars["KbAccess"],
		"KbAccessLabel":             templateVars["KbAccessLabel"],
		"KbWriteMethod":             templateVars["KbWriteMethod"],
		"LearningsAccess":           templateVars["LearningsAccess"],
		"KnowledgebaseContribution": templateVars["KnowledgebaseContribution"],
		"KBGuidanceBlock":           templateVars["KBGuidanceBlock"],
		"IsCodeExecutionMode":       templateVars["IsCodeExecutionMode"] == "true",
		"CodeExecutionSection":      BuildCodeExecutionSection(templateVars["IsCodeExecutionMode"] == "true", templateVars["WorkspacePath"]),
		"PreviousStepsSummary":      templateVars["PreviousStepsSummary"],
		"StepTitle":                 templateVars["StepTitle"],
		"StepDescription":           templateVars["StepDescription"],
		"StepSuccessCriteria":       templateVars["StepSuccessCriteria"],
		"ValidationSchema":          templateVars["ValidationSchema"],
		"HasBrowserAccess":          templateVars["HasBrowserAccess"] == "true",
		"LearningsPath":             templateVars["LearningsPath"],
	}

	var result strings.Builder
	if err := orchestratorSystemTemplate.Execute(&result, templateData); err != nil {
		panic(fmt.Sprintf("todo task orchestrator system prompt template execution failed (missing variable?): %v", err))
	}
	return result.String()
}

// orchestratorUserMessageProcessor generates the user message for todo task orchestrator agent
func (agent *WorkflowOrchestratorAgent) orchestratorUserMessageProcessor(
	templateVars map[string]string,
	conversationHistory []llmtypes.MessageContent,
) string {
	// A follow-up turn (scripted message, repair turn, reflection turn) is sent
	// verbatim: the step framing, dependencies, and human input were already
	// delivered on the opening turn and live in the conversation.
	if followUp := strings.TrimSpace(templateVars["FollowUpMessage"]); followUp != "" {
		return followUp
	}
	templateData := map[string]interface{}{
		"StepTitle":               templateVars["StepTitle"],
		"StepDescription":         templateVars["StepDescription"],
		"StepContextDependencies": templateVars["StepContextDependencies"],
		"StepSuccessCriteria":     templateVars["StepSuccessCriteria"],
		"ValidationFeedback":      templateVars["ValidationFeedback"],
		"WorkshopHumanInput":      templateVars["WorkshopHumanInput"],
	}

	var result strings.Builder
	if err := orchestratorUserTemplate.Execute(&result, templateData); err != nil {
		panic(fmt.Sprintf("todo task orchestrator user message template execution failed (missing variable?): %v", err))
	}
	return result.String()
}
