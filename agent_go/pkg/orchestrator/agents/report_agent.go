package agents

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator/agents/prompts"

	"github.com/tmc/langchaingo/llms"
)

// OrchestratorReportAgent extends BaseOrchestratorAgent with report generation functionality
type OrchestratorReportAgent struct {
	*BaseOrchestratorAgent
	reportPrompts *prompts.ReportPrompts
}

// NewOrchestratorReportAgent creates a new report agent
func NewOrchestratorReportAgent(config *OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *OrchestratorReportAgent {
	reportPrompts := prompts.NewReportPrompts()

	baseAgent := NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		ReportGenerationAgentType,
		eventBridge,
	)

	return &OrchestratorReportAgent{
		BaseOrchestratorAgent: baseAgent,
		reportPrompts:         reportPrompts,
	}
}

// Execute executes the report agent with report-specific input processing
func (ra *OrchestratorReportAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, error) {
	return ra.ExecuteWithInputProcessor(ctx, templateVars, ra.reportInputProcessor, conversationHistory)
}

// reportInputProcessor processes inputs specifically for report generation using template replacement
func (ra *OrchestratorReportAgent) reportInputProcessor(templateVars map[string]string) string {
	// Use the predefined prompt with template variable replacement
	templateStr := ra.reportPrompts.GenerateReportPrompt

	// Parse and execute the template
	tmpl, err := template.New("report").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing report template: %v", err)
	}

	var result strings.Builder
	err = tmpl.Execute(&result, templateVars)
	if err != nil {
		return fmt.Sprintf("Error executing report template: %v", err)
	}

	return result.String()
}
