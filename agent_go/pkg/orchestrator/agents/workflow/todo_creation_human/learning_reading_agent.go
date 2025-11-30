package todo_creation_human

import (
	"context"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	"mcpagent/observability"
)

// HumanControlledTodoPlannerLearningReadingTemplate holds template variables for learning reading prompts
type HumanControlledTodoPlannerLearningReadingTemplate struct {
	StepTitle               string
	StepDescription         string
	LearningsPath           string // Learnings folder path for reading learning files and scripts/code
	IsCodeExecutionMode     string // "true" or "false" - determines which learnings folder to read from
	StepContextDependencies string // Context files from previous steps (comma-separated)
	WorkspacePath           string // Workspace path for reading context dependency files
}

// HumanControlledTodoPlannerLearningReadingAgent reads learning files and code patterns only
type HumanControlledTodoPlannerLearningReadingAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledTodoPlannerLearningReadingAgent creates a new learning reading agent
func NewHumanControlledTodoPlannerLearningReadingAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerLearningReadingAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerExecutionAgentType, // Reuse execution agent type for consistency
		eventBridge,
	)

	return &HumanControlledTodoPlannerLearningReadingAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute implements the OrchestratorAgent interface
func (hctplra *HumanControlledTodoPlannerLearningReadingAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Generate system prompt and user message separately
	systemPrompt := hctplra.learningReadingSystemPromptProcessor(templateVars)
	userMessage := hctplra.learningReadingUserMessageProcessor(templateVars)

	// Create a simple input processor that returns the user message
	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	// Use ExecuteWithTemplateValidation with system prompt (overwrite=true to replace default MCP prompt with agent-specific prompt)
	return hctplra.BaseOrchestratorAgent.ExecuteWithTemplateValidation(ctx, templateVars, inputProcessor, conversationHistory, nil, systemPrompt, true)
}

// learningReadingSystemPromptProcessor generates the system prompt for learning reading agent
func (hctplra *HumanControlledTodoPlannerLearningReadingAgent) learningReadingSystemPromptProcessor(templateVars map[string]string) string {
	learningsPath := templateVars["LearningsPath"]
	isCodeExecutionMode := templateVars["IsCodeExecutionMode"] == "true"
	workspacePath := templateVars["WorkspacePath"]
	stepContextDependencies := templateVars["StepContextDependencies"]
	hasContextDependencies := stepContextDependencies != ""

	// Get current date and time
	now := time.Now()
	currentDate := now.Format("2006-01-02")
	currentTime := now.Format("15:04:05")

	// Define the system prompt template
	templateStr := `# Learning Reading Agent

## 📅 Current Session
**Date**: {{.CurrentDate}} | **Time**: {{.CurrentTime}}

## 🤖 Agent Identity
- **Role**: Learning Reading Agent (Discovery Only - NO Execution)  
- **Responsibility**: Discover and read step-specific learning files{{if .IsCodeExecutionMode}} and Go code patterns{{else}} and scripts{{end}}  
- **Output**: Conversation history will be passed to execution agent

## 📁 File Permissions
**READ ONLY**: {{.LearningsPath}}/{{if and .IsCodeExecutionMode .HasContextDependencies}}, {{.WorkspacePath}}/ (context files){{end}}  
**NO WRITE** - This agent only reads learnings

## 🎯 Learning Discovery Flow

**BE SELECTIVE: Only read files DIRECTLY relevant to the current step**

{{if and .IsCodeExecutionMode .HasContextDependencies}}┌─────────────────────────────────────────┐
│ 1. Read Context Dependencies FIRST     │
│    (from {{.WorkspacePath}}/)           │
│    - Understand file structure/format   │
│    - Identify data patterns              │
│    - See how to work with context files  │
└─────────────────────────────────────────┘
              ↓
{{end}}┌─────────────────────────────────────────┐
│ {{if and .IsCodeExecutionMode .HasContextDependencies}}2{{else}}1{{end}}. Analyze Step Title & Description      │
│    - Extract KEY CONCEPTS & KEYWORDS     │
│    - Identify technologies/operations    │
└─────────────────────────────────────────┘
              ↓
┌─────────────────────────────────────────┐
│ {{if and .IsCodeExecutionMode .HasContextDependencies}}3{{else}}2{{end}}. List Available Files               │
│    - {{.LearningsPath}}/                │
{{if .IsCodeExecutionMode}}│    - {{.LearningsPath}}/code/           │{{else}}│    - {{.LearningsPath}}/scripts/        │{{end}}
└─────────────────────────────────────────┘
              ↓
┌─────────────────────────────────────────┐
│ {{if and .IsCodeExecutionMode .HasContextDependencies}}4{{else}}3{{end}}. Select by Keyword Match            │
│    Priority 1: Exact keyword match       │
│    Priority 2: Related keywords          │
│    Priority 3: Relevant general files    │
│    SKIP: Unrelated files/topics          │
└─────────────────────────────────────────┘
              ↓
┌─────────────────────────────────────────┐
│ {{if and .IsCodeExecutionMode .HasContextDependencies}}5{{else}}4{{end}}. Read Selected Files ONLY           │{{if .IsCodeExecutionMode}}
│    - *{keyword}_learning.md              │
│    - *{keyword}_code.go                  │{{else}}
│    - *{keyword}_learning.md              │
│    - *{keyword}_script.py                │{{end}}
│    Quality > Quantity (2-3 relevant      │
│    files better than 10+ unrelated)      │
└─────────────────────────────────────────┘
              ↓
┌─────────────────────────────────────────┐
│ {{if and .IsCodeExecutionMode .HasContextDependencies}}6{{else}}5{{end}}. Summarize Findings                 │{{if and .IsCodeExecutionMode .HasContextDependencies}}
│    - Context structure learned           │
│    - Files read + relevance              │
│    - Key patterns/code examples          │
│    - How context informs learnings       │{{else}}
│    - Files read + relevance              │
│    - Key patterns{{if .IsCodeExecutionMode}}/code examples{{else}}/scripts{{end}}       │{{end}}
└─────────────────────────────────────────┘

## 📤 Output Requirements
{{if and .IsCodeExecutionMode .HasContextDependencies}}- **Context Dependencies**: What you learned about file structure/format from context files  
{{end}}- **Files Read**: List with brief relevance explanation  
- **Key Insights**: Main patterns, best practices, {{if .IsCodeExecutionMode}}code examples{{else}}script examples{{end}}  
- **Relevance**: Why each file applies to current step{{if and .IsCodeExecutionMode .HasContextDependencies}}  
- **Context Integration**: How context dependencies inform learning selection{{end}}

Focus on quality over quantity - your conversation history goes to the execution agent.`

	// Parse and execute the template
	tmpl, err := template.New("learningReadingSystemPrompt").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing learning reading system prompt template: %v", err)
	}

	var result strings.Builder
	err = tmpl.Execute(&result, map[string]interface{}{
		"LearningsPath":          learningsPath,
		"IsCodeExecutionMode":    isCodeExecutionMode,
		"WorkspacePath":          workspacePath,
		"HasContextDependencies": hasContextDependencies,
		"CurrentDate":            currentDate,
		"CurrentTime":            currentTime,
	})
	if err != nil {
		return fmt.Sprintf("Error executing learning reading system prompt template: %v", err)
	}

	return result.String()
}

// learningReadingUserMessageProcessor generates the user message for learning reading agent
func (hctplra *HumanControlledTodoPlannerLearningReadingAgent) learningReadingUserMessageProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := HumanControlledTodoPlannerLearningReadingTemplate{
		StepTitle:               templateVars["StepTitle"],
		StepDescription:         templateVars["StepDescription"],
		LearningsPath:           templateVars["LearningsPath"],
		IsCodeExecutionMode:     templateVars["IsCodeExecutionMode"],
		StepContextDependencies: templateVars["StepContextDependencies"],
		WorkspacePath:           templateVars["WorkspacePath"],
	}

	// Define the user message template
	templateStr := `## 🎯 Discover Learnings for: {{.StepTitle}}

**STEP DESCRIPTION**: {{.StepDescription}}
{{if and (eq .IsCodeExecutionMode "true") .StepContextDependencies}}

## 📋 Context Dependencies (Read FIRST in Code Execution Mode)
**Files**: {{.StepContextDependencies}}  
**Location**: {{.WorkspacePath}}/  
**Why**: Understand file structure/format to select better learning files and code patterns
{{end}}

## ✅ Discovery Checklist
{{if and (eq .IsCodeExecutionMode "true") .StepContextDependencies}}1. ✓ Read context dependencies from {{.WorkspacePath}}/ (understand file structure)
2{{else}}1{{end}}. ✓ Analyze step - extract keywords and technologies
{{if and (eq .IsCodeExecutionMode "true") .StepContextDependencies}}3{{else}}2{{end}}. ✓ List files in {{.LearningsPath}}/{{if eq .IsCodeExecutionMode "true"}} and {{.LearningsPath}}/code/{{else}} and {{.LearningsPath}}/scripts/{{end}}
{{if and (eq .IsCodeExecutionMode "true") .StepContextDependencies}}4{{else}}3{{end}}. ✓ Select by keyword match:
   - Priority 1: Exact keyword in filename
   - Priority 2: Related keywords
   - Priority 3: Relevant general files
   - SKIP: Unrelated files/topics
{{if and (eq .IsCodeExecutionMode "true") .StepContextDependencies}}5{{else}}4{{end}}. ✓ Read ONLY selected files ({{if eq .IsCodeExecutionMode "true"}}*.md, *.go{{else}}*.md, *.py{{end}})
{{if and (eq .IsCodeExecutionMode "true") .StepContextDependencies}}6{{else}}5{{end}}. ✓ Summarize:{{if and (eq .IsCodeExecutionMode "true") .StepContextDependencies}}
   - Context structure learned
{{end}}   - Files read + why relevant
   - Key patterns{{if eq .IsCodeExecutionMode "true"}}/code examples{{else}}/scripts{{end}}{{if and (eq .IsCodeExecutionMode "true") .StepContextDependencies}}
   - How context informs learnings{{end}}

**Remember**: Quality > Quantity. Your conversation history goes to the execution agent.`

	// Parse and execute the template
	tmpl, err := template.New("learningReadingUserMessage").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing learning reading user message template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing learning reading user message template: %v", err)
	}

	return result.String()
}
