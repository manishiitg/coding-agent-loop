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

// OrchestratorExecutionAgent extends BaseOrchestratorAgent with execution-specific functionality
type OrchestratorExecutionAgent struct {
	*BaseOrchestratorAgent
	executionPrompts *prompts.ExecutionPrompts
}

// NewOrchestratorExecutionAgent creates a new execution agent
func NewOrchestratorExecutionAgent(ctx context.Context, config *OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *OrchestratorExecutionAgent {
	executionPrompts := prompts.NewExecutionPrompts()

	baseAgent := NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		ExecutionAgentType,
		eventBridge,
	)

	return &OrchestratorExecutionAgent{
		BaseOrchestratorAgent: baseAgent,
		executionPrompts:      executionPrompts,
	}
}

// Execute executes the execution agent with execution-specific input processing
func (ea *OrchestratorExecutionAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, []llms.MessageContent, error) {
	return ea.ExecuteWithInputProcessor(ctx, templateVars, ea.executionInputProcessor, conversationHistory)
}

// executionInputProcessor processes inputs specifically for execution using template replacement
func (ea *OrchestratorExecutionAgent) executionInputProcessor(templateVars map[string]string) string {
	// Use the predefined prompt with template variable replacement
	templateStr := ea.executionPrompts.ExecuteStepPrompt

	// Parse and execute the template
	tmpl, err := template.New("execution").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing execution template: %v", err)
	}

	var result strings.Builder
	err = tmpl.Execute(&result, templateVars)
	if err != nil {
		return fmt.Sprintf("Error executing execution template: %v", err)
	}

	return result.String()
}

// Event system - now handled by unified events system
