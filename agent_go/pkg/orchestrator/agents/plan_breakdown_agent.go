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

// Execute executes the plan breakdown agent using the standard agent pattern
func (pba *PlanBreakdownAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, error) {
	// Use ExecuteWithInputProcessor to get agent events (orchestrator_agent_start/end)
	// This will automatically emit agent start/end events
	return pba.ExecuteWithInputProcessor(ctx, templateVars, pba.breakdownInputProcessor, conversationHistory)
}

// breakdownInputProcessor processes inputs specifically for plan breakdown - pure prompt renderer
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

// BreakdownStep represents a step in the breakdown analysis
type BreakdownStep struct {
	ID            string   `json:"id"`
	Description   string   `json:"description"`
	Dependencies  []string `json:"dependencies"`
	IsIndependent bool     `json:"is_independent"`
	Reasoning     string   `json:"reasoning"`
}

// BreakdownResponse represents the structured response from breakdown analysis
type BreakdownResponse struct {
	Steps []BreakdownStep `json:"steps"`
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
