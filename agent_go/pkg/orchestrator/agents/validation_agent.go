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

// OrchestratorValidationAgent extends BaseOrchestratorAgent with validation-specific functionality
type OrchestratorValidationAgent struct {
	*BaseOrchestratorAgent
	validationPrompts *prompts.ValidationPrompts
}

// NewOrchestratorValidationAgent creates a new validation agent
func NewOrchestratorValidationAgent(config *OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge EventBridge) *OrchestratorValidationAgent {
	validationPrompts := prompts.NewValidationPrompts()

	baseAgent := NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		ValidationAgentType,
		eventBridge,
	)

	return &OrchestratorValidationAgent{
		BaseOrchestratorAgent: baseAgent,
		validationPrompts:     validationPrompts,
	}
}

// Initialize initializes the validation agent (delegates to base)
func (va *OrchestratorValidationAgent) Initialize(ctx context.Context) error {
	return va.BaseOrchestratorAgent.Initialize(ctx)
}

// Execute executes the validation agent with validation-specific input processing
func (va *OrchestratorValidationAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, error) {
	return va.ExecuteWithInputProcessor(ctx, templateVars, va.validationInputProcessor, conversationHistory)
}

// validationInputProcessor processes inputs specifically for validation using template replacement
func (va *OrchestratorValidationAgent) validationInputProcessor(templateVars map[string]string) string {
	// Use the predefined prompt with template variable replacement
	templateStr := va.validationPrompts.ValidateResultsPrompt

	// Parse and execute the template
	tmpl, err := template.New("validation").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing validation template: %v", err)
	}

	var result strings.Builder
	err = tmpl.Execute(&result, templateVars)
	if err != nil {
		return fmt.Sprintf("Error executing validation template: %v", err)
	}

	return result.String()
}

// All other methods (GetType, GetConfig, Close, BaseAgent, GetBaseAgent, createLLM)
// are now inherited from BaseOrchestratorAgent
