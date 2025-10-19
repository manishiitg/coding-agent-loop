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

// OrchestratorPlanningAgent extends BaseOrchestratorAgent with planning-specific functionality
type OrchestratorPlanningAgent struct {
	*BaseOrchestratorAgent
	planningPrompts *prompts.PlanningPrompts
}

// NewOrchestratorPlanningAgent creates a new planning agent
func NewOrchestratorPlanningAgent(config *OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge EventBridge) *OrchestratorPlanningAgent {
	planningPrompts := prompts.NewPlanningPrompts()

	baseAgent := NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		PlanningAgentType,
		eventBridge,
	)

	return &OrchestratorPlanningAgent{
		BaseOrchestratorAgent: baseAgent,
		planningPrompts:       planningPrompts,
	}
}

// Initialize initializes the planning agent (delegates to base)
func (pa *OrchestratorPlanningAgent) Initialize(ctx context.Context) error {
	return pa.BaseOrchestratorAgent.Initialize(ctx)
}

// Execute executes the planning agent with planning-specific input processing
func (pa *OrchestratorPlanningAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, error) {
	return pa.ExecuteWithInputProcessor(ctx, templateVars, pa.planningInputProcessor, conversationHistory)
}

// planningInputProcessor processes inputs specifically for planning using template replacement
func (pa *OrchestratorPlanningAgent) planningInputProcessor(templateVars map[string]string) string {
	// Use the predefined prompt with template variable replacement
	templateStr := pa.planningPrompts.PlanNextStepPrompt

	// Parse and execute the template
	tmpl, err := template.New("planning").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing planning template: %v", err)
	}

	var result strings.Builder
	err = tmpl.Execute(&result, templateVars)
	if err != nil {
		return fmt.Sprintf("Error executing planning template: %v", err)
	}

	return result.String()
}

// All other methods (GetType, GetConfig, Close, BaseAgent, GetBaseAgent, createLLM)
// are now inherited from BaseOrchestratorAgent
