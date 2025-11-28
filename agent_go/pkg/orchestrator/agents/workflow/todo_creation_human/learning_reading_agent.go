package todo_creation_human

import (
	"context"
	"fmt"
	"strings"
	"text/template"
	"time"

	"llm-providers/llmtypes"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	"mcpagent/observability"
)

// HumanControlledTodoPlannerLearningReadingTemplate holds template variables for learning reading prompts
type HumanControlledTodoPlannerLearningReadingTemplate struct {
	StepTitle           string
	StepDescription     string
	LearningsPath       string // Learnings folder path for reading learning files and scripts/code
	IsCodeExecutionMode string // "true" or "false" - determines which learnings folder to read from
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

	// Get current date and time
	now := time.Now()
	currentDate := now.Format("2006-01-02")
	currentTime := now.Format("15:04:05")

	// Define the system prompt template
	templateStr := `# Learning Reading Agent

## 📅 **CURRENT SESSION INFORMATION**
**Date**: {{.CurrentDate}}
**Time**: {{.CurrentTime}}

## 🤖 AGENT IDENTITY
- **Role**: Learning Reading Agent
- **Responsibility**: Discover and read step-specific learning files{{if .IsCodeExecutionMode}} and Go code patterns{{else}} and scripts{{end}} from the learnings folder
- **Mode**: Learning discovery only (NO execution)

## 📁 FILE PERMISSIONS

**READ ONLY:**
- Learning files/scripts from {{.LearningsPath}}/ (read-only access)
- **NO** writing permissions - this agent only reads learnings

## 🔍 LEARNING DISCOVERY GUIDELINES

**Your ONLY task is to discover and read STEP-SPECIFIC learning files and code patterns. You do NOT execute any steps.**

**CRITICAL: Be SELECTIVE - Only read files that are DIRECTLY relevant to the current step. Do NOT read all files.**

1. **Understand the Step First**:
   - Analyze the step title and description to identify KEY CONCEPTS and KEYWORDS
   - Focus on what the step is trying to accomplish
   - Identify the main technologies, tools, or operations mentioned

2. **List files to discover options**:
   - Use list_workspace_files to discover files in {{.LearningsPath}}/ (max_depth: 1)
   {{if .IsCodeExecutionMode}}
   - Use list_workspace_files to discover Go code files in {{.LearningsPath}}/code/ (max_depth: 1)
   {{else}}
   - Use list_workspace_files to discover Python scripts in {{.LearningsPath}}/scripts/ (max_depth: 1)
   {{end}}

3. **Selective File Matching - READ ONLY STEP-SPECIFIC FILES**:
   - **Priority 1**: Files whose names contain EXACT keywords from the step title/description
   - **Priority 2**: Files whose names contain RELATED keywords (same concept, different wording)
   - **Priority 3**: General learnings files ONLY if they mention concepts relevant to this step
   - **DO NOT READ**: Files that are clearly about different topics, unrelated steps, or general learnings that don't apply

4. **File Naming Patterns to Look For**:
   - Step-specific: *{step_keyword}_learning.md{{if .IsCodeExecutionMode}}, *{step_keyword}_code.go{{else}}, *{step_keyword}_script.py{{end}}
   - Related: Files with similar concepts but different wording
   - General: Only if they contain relevant patterns (check file names first, don't read blindly)

5. **Read Only Selected Files**:
   - Read files that match the step keywords/concepts
   - Skip files that are clearly unrelated
   - If unsure, check file name relevance before reading

{{if .IsCodeExecutionMode}}
6. **Code Pattern Selection**:
   - Read Go code files that match step keywords
   - Focus on code patterns that solve similar problems to the current step
   - Skip code patterns for unrelated operations
{{else}}
6. **Script Selection**:
   - Read Python scripts that match step keywords
   - Focus on scripts that perform similar operations to the current step
   - Skip scripts for unrelated tasks
{{end}}

**Discovery Strategy**: 
- **Be SELECTIVE**: Only read files SPECIFIC to this step or DIRECTLY RELATED
- **Quality over Quantity**: Better to read 2-3 highly relevant files than 10+ unrelated files
- **Name-based matching**: Use keywords from step title/description to identify relevant files
- **Skip unrelated files**: Don't read files about different topics or unrelated steps

## 📤 Output Format

Provide a focused summary of:
- **Files Discovered**: List the step-specific learning files and{{if .IsCodeExecutionMode}} Go code patterns{{else}} scripts{{end}} you found
- **Files Read**: List ONLY the files you actually read (with brief descriptions of why they're relevant)
- **Key Insights**: Summarize the main patterns, best practices, and{{if .IsCodeExecutionMode}} code examples{{else}} script examples{{end}} found in the step-specific files
- **Relevance**: Explain why each file/{{if .IsCodeExecutionMode}}code pattern{{else}}script{{end}} is relevant to the current step

**Important**: 
- Be SELECTIVE - only read and report files that are directly relevant to this step
- Your conversation history will be passed to the execution agent
- Focus on quality, step-specific learnings rather than reading everything`

	// Parse and execute the template
	tmpl, err := template.New("learningReadingSystemPrompt").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing learning reading system prompt template: %v", err)
	}

	var result strings.Builder
	err = tmpl.Execute(&result, map[string]interface{}{
		"LearningsPath":       learningsPath,
		"IsCodeExecutionMode": isCodeExecutionMode,
		"CurrentDate":         currentDate,
		"CurrentTime":         currentTime,
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
		StepTitle:           templateVars["StepTitle"],
		StepDescription:     templateVars["StepDescription"],
		LearningsPath:       templateVars["LearningsPath"],
		IsCodeExecutionMode: templateVars["IsCodeExecutionMode"],
	}

	// Define the user message template
	templateStr := `## 🎯 PRIMARY TASK - DISCOVER AND READ STEP-SPECIFIC LEARNINGS

**Your ONLY task**: Discover and read learning files{{if eq .IsCodeExecutionMode "true"}} and Go code patterns{{else}} and scripts{{end}} that are SPECIFIC to the current step.

**CURRENT STEP**: {{.StepTitle}}
**STEP DESCRIPTION**: {{.StepDescription}}

**CRITICAL: Be SELECTIVE - Only read files that are DIRECTLY relevant to this step.**

**Instructions**:
1. **Analyze the step** - Identify key concepts, keywords, and technologies from the step title and description
2. **List files to discover options**:
   - List files in {{.LearningsPath}}/ to see available learning files
   {{if eq .IsCodeExecutionMode "true"}}
   - List Go code files in {{.LearningsPath}}/code/ to see available code patterns
   {{else}}
   - List Python scripts in {{.LearningsPath}}/scripts/ to see available scripts
   {{end}}
3. **Select files by relevance**:
   - **Read files** whose names contain keywords from the step title/description
   - **Read files** that are clearly about the same topic/concept as this step
   - **Skip files** that are about different topics or unrelated steps
   - **Skip general learnings** unless they mention concepts relevant to this step
4. **Read only selected files**:
   {{if eq .IsCodeExecutionMode "true"}}
   - Read step-specific learning markdown files and Go code patterns
   {{else}}
   - Read step-specific learning markdown files and Python scripts
   {{end}}
   - Don't read files that are clearly unrelated
5. **Summarize your findings** - Provide a focused summary of step-specific learnings you discovered

**Important**: 
- You are ONLY discovering and reading learnings - you do NOT execute any steps
- Be SELECTIVE - quality over quantity, only read step-specific files
- Your conversation history will be passed to the execution agent
- Focus on files that directly help with this specific step`

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
