package agents

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/orchestrator/agents/prompts"

	"github.com/tmc/langchaingo/llms"
)

// PlanBreakdownTemplate holds template variables for plan breakdown prompts
type PlanBreakdownTemplate struct {
	PlanningResult string
	Objective      string
	WorkspacePath  string
}

// PlanBreakdownAgent analyzes dependencies and creates independent steps for parallel execution
type PlanBreakdownAgent struct {
	*BaseOrchestratorAgent
	breakdownPrompts *prompts.PlanBreakdownPrompts
}

// NewPlanBreakdownAgent creates a new plan breakdown agent
func NewPlanBreakdownAgent(config *OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge interface{}) *PlanBreakdownAgent {
	breakdownPrompts := prompts.NewPlanBreakdownPrompts()

	baseAgent := NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		PlanBreakdownAgentType,
		eventBridge,
	)

	return &PlanBreakdownAgent{
		BaseOrchestratorAgent: baseAgent,
		breakdownPrompts:      breakdownPrompts,
	}
}

// Initialize initializes the plan breakdown agent (delegates to base)
func (pba *PlanBreakdownAgent) Initialize(ctx context.Context) error {
	return pba.BaseOrchestratorAgent.Initialize(ctx)
}

// Execute executes the plan breakdown agent with breakdown-specific input processing
func (pba *PlanBreakdownAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, error) {
	return pba.ExecuteWithInputProcessor(ctx, templateVars, pba.breakdownInputProcessor, conversationHistory)
}

// breakdownInputProcessor processes inputs specifically for plan breakdown using template replacement
func (pba *PlanBreakdownAgent) breakdownInputProcessor(templateVars map[string]string) string {
	// Use the predefined prompt with template variable replacement
	templateStr := pba.breakdownPrompts.AnalyzeDependenciesPrompt

	// Parse and execute the template
	tmpl, err := template.New("breakdown").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing breakdown template: %v", err)
	}

	var result strings.Builder
	err = tmpl.Execute(&result, templateVars)
	if err != nil {
		return fmt.Sprintf("Error executing breakdown template: %v", err)
	}

	return result.String()
}

// AnalyzeDependencies analyzes the planning result and returns independent steps
func (pba *PlanBreakdownAgent) AnalyzeDependencies(ctx context.Context, planningResult, objective, workspacePath string) (string, error) {
	pba.AgentTemplate.GetLogger().Infof("🔍 Starting dependency analysis for plan breakdown")

	// Create template data
	templateData := PlanBreakdownTemplate{
		PlanningResult: planningResult,
		Objective:      objective,
		WorkspacePath:  workspacePath,
	}

	// Use the Execute method with template variables
	templateVars := map[string]string{
		"PlanningResult": templateData.PlanningResult,
		"Objective":      templateData.Objective,
		"WorkspacePath":  templateData.WorkspacePath,
	}

	response, err := pba.Execute(ctx, templateVars, []llms.MessageContent{})
	if err != nil {
		return "", fmt.Errorf("failed to generate dependency analysis: %w", err)
	}

	pba.AgentTemplate.GetLogger().Infof("✅ Dependency analysis completed successfully")
	return response, nil
}

// GetAgentType returns the agent type
func (pba *PlanBreakdownAgent) GetAgentType() AgentType {
	return PlanBreakdownAgentType
}

// GetAgentName returns a human-readable name for the agent
func (pba *PlanBreakdownAgent) GetAgentName() string {
	return "Plan Breakdown Agent"
}

// GetAgentDescription returns a description of what this agent does
func (pba *PlanBreakdownAgent) GetAgentDescription() string {
	return "Analyzes execution plans and identifies independent steps that can be executed in parallel"
}
