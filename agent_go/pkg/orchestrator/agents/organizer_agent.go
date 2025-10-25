package agents

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"github.com/tmc/langchaingo/llms"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator/agents/prompts"
)

// PlanOrganizerAgentType is now defined in base_agent.go

// PlanOrganizerAgent extends BaseOrchestratorAgent with organizer-specific functionality
type PlanOrganizerAgent struct {
	*BaseOrchestratorAgent
	planOrganizerPrompts *prompts.PlanOrganizerPrompts
}

// NewPlanOrganizerAgent creates a new plan organizer agent
func NewPlanOrganizerAgent(config *OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *PlanOrganizerAgent {
	planOrganizerPrompts := prompts.NewPlanOrganizerPrompts()

	baseAgent := NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		PlanOrganizerAgentType,
		eventBridge,
	)

	return &PlanOrganizerAgent{
		BaseOrchestratorAgent: baseAgent,
		planOrganizerPrompts:  planOrganizerPrompts,
	}
}

// Execute executes the plan organizer agent with organizer-specific input processing
func (poa *PlanOrganizerAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, []llms.MessageContent, error) {
	return poa.ExecuteWithInputProcessor(ctx, templateVars, poa.organizerInputProcessor, conversationHistory)
}

// organizerInputProcessor processes inputs specifically for organization using template replacement
func (poa *PlanOrganizerAgent) organizerInputProcessor(templateVars map[string]string) string {
	// Use the predefined prompt with template variable replacement
	templateStr := poa.planOrganizerPrompts.OrganizeWorkflowPrompt

	// Parse and execute the template
	tmpl, err := template.New("organizer").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing organizer template: %v", err)
	}

	var result strings.Builder
	err = tmpl.Execute(&result, templateVars)
	if err != nil {
		return fmt.Sprintf("Error executing organizer template: %v", err)
	}

	return result.String()
}

// Event system - now handled by unified events system
