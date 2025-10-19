package agents

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"github.com/tmc/langchaingo/llms"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/orchestrator/agents/prompts"
)

// OrchestratorParallelExecutionAgent extends BaseOrchestratorAgent with parallel execution-specific functionality
type OrchestratorParallelExecutionAgent struct {
	*BaseOrchestratorAgent
	parallelExecutionPrompts *prompts.ParallelExecutionPrompts
}

// NewOrchestratorParallelExecutionAgent creates a new parallel execution agent
func NewOrchestratorParallelExecutionAgent(ctx context.Context, config *OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge interface{}) *OrchestratorParallelExecutionAgent {
	parallelExecutionPrompts := prompts.NewParallelExecutionPrompts()

	baseAgent := NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		ParallelExecutionAgentType,
		eventBridge,
	)

	return &OrchestratorParallelExecutionAgent{
		BaseOrchestratorAgent:    baseAgent,
		parallelExecutionPrompts: parallelExecutionPrompts,
	}
}

// Initialize initializes the parallel execution agent (delegates to base)
func (pea *OrchestratorParallelExecutionAgent) Initialize(ctx context.Context) error {
	return pea.BaseOrchestratorAgent.Initialize(ctx)
}

// Execute executes the parallel execution agent using the standard agent pattern
func (pea *OrchestratorParallelExecutionAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, error) {
	// Use ExecuteWithInputProcessor to get agent events (orchestrator_agent_start/end)
	// This will automatically emit agent start/end events
	return pea.ExecuteWithInputProcessor(ctx, templateVars, pea.parallelExecutionInputProcessor, conversationHistory)
}

// parallelExecutionInputProcessor processes inputs specifically for parallel execution using template replacement
func (pea *OrchestratorParallelExecutionAgent) parallelExecutionInputProcessor(templateVars map[string]string) string {
	// Use the predefined parallel execution prompt with template variable replacement
	templateStr := pea.parallelExecutionPrompts.ExecuteStepPrompt

	// Parse and execute the template
	tmpl, err := template.New("parallel_execution").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing parallel execution template: %v", err)
	}

	var result strings.Builder
	err = tmpl.Execute(&result, templateVars)
	if err != nil {
		return fmt.Sprintf("Error executing parallel execution template: %v", err)
	}

	return result.String()
}

// GetType returns the agent type
func (pea *OrchestratorParallelExecutionAgent) GetType() string {
	return string(ParallelExecutionAgentType)
}

// Event system - now handled by unified events system

// BaseAgent returns the underlying base agent for direct access
func (pea *OrchestratorParallelExecutionAgent) BaseAgent() *BaseAgent {
	return pea.AgentTemplate.baseAgent
}
