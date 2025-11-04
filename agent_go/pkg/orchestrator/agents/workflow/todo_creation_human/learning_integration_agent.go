package todo_creation_human

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"mcp-agent/agent_go/internal/llmtypes"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
)

// HumanControlledLearningIntegrationAgent reads plan.json and learnings files, then enhances plan with success/failure patterns
// This agent does NOT write files - it only returns enhanced JSON
type HumanControlledLearningIntegrationAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledLearningIntegrationAgent creates a new learning integration agent
func NewHumanControlledLearningIntegrationAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledLearningIntegrationAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.PlanReaderAgentType, // Reuse plan reader type for now
		eventBridge,
	)

	return &HumanControlledLearningIntegrationAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// ExecuteStructured executes the learning integration agent and returns enhanced PlanningResponse with patterns
func (hclia *HumanControlledLearningIntegrationAgent) ExecuteStructured(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (*PlanningResponse, []llmtypes.MessageContent, error) {
	// Define the JSON schema - same as PlanningResponse but with patterns required
	schema := `{
		"type": "object",
		"properties": {
			"steps": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"title": {
							"type": "string",
							"description": "Short, clear title for the step"
						},
						"description": {
							"type": "string",
							"description": "Detailed description of what this step accomplishes"
						},
						"success_criteria": {
							"type": "string",
							"description": "How to verify this step was completed successfully"
						},
						"context_dependencies": {
							"type": "array",
							"items": {
								"type": "string"
							},
							"description": "List of context files from previous steps that this step depends on"
						},
						"context_output": {
							"type": "string",
							"description": "What context file this step will create for other agents"
						},
						"success_patterns": {
							"type": "array",
							"items": {
								"type": "string"
							},
							"description": "List of MCP server tool calls that successfully achieved the step description. Extract from step_X_learning.md files and match to appropriate steps based on which tools worked to accomplish the step goal."
						},
						"failure_patterns": {
							"type": "array",
							"items": {
								"type": "string"
							},
							"description": "List of MCP server tool calls that failed to achieve the step description or should be avoided. Extract from step_X_learning.md files and match to appropriate steps based on which tools didn't work for the step goal."
						},
						"has_loop": {
							"type": "boolean",
							"description": "Whether this step needs to loop until condition is met"
						},
						"loop_condition": {
							"type": "string",
							"description": "Condition that must be met to exit the loop (REQUIRED when has_loop is true)"
						},
						"max_iterations": {
							"type": "integer",
							"description": "Maximum number of loop iterations allowed to prevent infinite loops (default: 10). Only include when has_loop is true"
						},
						"loop_description": {
							"type": "string",
							"description": "Human-readable explanation of why the loop is needed and how it works. Only include when has_loop is true and a description is provided"
						}
					},
					"required": ["title", "description", "success_criteria", "has_loop"]
				}
			}
		},
		"required": ["steps"]
	}`

	// Use the base orchestrator agent's ExecuteStructured method
	result, updatedHistory, err := agents.ExecuteStructuredWithInputProcessor[PlanningResponse](hclia.BaseOrchestratorAgent, ctx, templateVars, hclia.learningIntegrationInputProcessor, conversationHistory, schema, "", false)
	if err != nil {
		return nil, nil, err
	}

	return &result, updatedHistory, nil
}

// Execute implements the OrchestratorAgent interface
// NOTE: This method is NOT USED - use ExecuteStructured() instead
func (hclia *HumanControlledLearningIntegrationAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	return "", nil, fmt.Errorf("Execute() is not used for learning integration agent - use ExecuteStructured() instead")
}

// learningIntegrationInputProcessor processes inputs for learning integration
func (hclia *HumanControlledLearningIntegrationAgent) learningIntegrationInputProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := map[string]string{
		"Objective":        templateVars["Objective"],
		"WorkspacePath":    templateVars["WorkspacePath"],
		"ExistingPlanJSON": templateVars["ExistingPlanJSON"], // Current plan.json content
	}

	// Define the template for learning integration
	templateStr := `## 📖 PRIMARY TASK - ENHANCE PLAN WITH SUCCESS/FAILURE PATTERNS FROM LEARNINGS

**OBJECTIVE**: {{.Objective}}
**WORKSPACE**: {{.WorkspacePath}}

## 🤖 AGENT IDENTITY
- **Role**: Learning Integration Agent
- **Responsibility**: Enhance existing plan.json with success/failure patterns from learnings files
- **Input**: plan.json + learnings/ folder files
- **Output**: Enhanced plan.json with patterns added
- **NO FILE WRITING**: This agent does NOT write files - only returns enhanced JSON
- **READ ONLY**: This agent reads files but does NOT write any files

## 📁 FILE PERMISSIONS
**READ:**
- **{{.WorkspacePath}}/planning/plan.json** (current plan - already loaded in ExistingPlanJSON)
- **{{.WorkspacePath}}/learnings/step_*_learning.md** (per-step learning details with both success and failure patterns - if exists)

**NO WRITE PERMISSIONS:**
- This agent does NOT write any files - only returns enhanced JSON

## 📋 TASK DESCRIPTION

**Your ONLY Job**:
1. Read the current plan from ExistingPlanJSON (already provided below)
2. **Read learnings files** from {{.WorkspacePath}}/learnings/ directory (if they exist):
   - Read {{.WorkspacePath}}/learnings/step_*_learning.md files to extract both success and failure patterns
   - Each step_X_learning.md file contains comprehensive learnings including which MCP server tool calls worked and which failed for that specific step
3. **Match learnings to steps**: Analyze which patterns apply to which steps based on:
   - Step titles and descriptions matching the learning file context
   - Which MCP server tool calls successfully achieved each step's description/goal
   - Which MCP server tool calls failed to achieve each step's description/goal
4. **Enhance each step** with appropriate success_patterns and failure_patterns arrays:
   - **success_patterns**: MCP server tool calls that successfully achieved the step description
   - **failure_patterns**: MCP server tool calls that failed or should be avoided for the step description
5. **Preserve all existing plan data**: Keep all other fields exactly as they are
6. Return enhanced JSON response

**CRITICAL REQUIREMENTS**:
- **Preserve ALL existing plan data**: Do NOT modify title, description, success_criteria, context_dependencies, context_output, has_loop, loop_condition, max_iterations, or loop_description
- **ONLY add patterns**: Add success_patterns and failure_patterns arrays to each step
- **Match patterns intelligently**: Match learnings to steps based on relevance (step titles, descriptions, tools mentioned, etc.)
- **Use empty arrays**: If no patterns match a step, use empty arrays [] for both success_patterns and failure_patterns
- **Handle missing files gracefully**: If learnings files don't exist, return plan with empty pattern arrays

## 📊 CURRENT PLAN JSON

{{.ExistingPlanJSON}}

## 📚 LEARNINGS FILES TO READ

**IMPORTANT**: You must read these files using workspace tools:
- **{{.WorkspacePath}}/learnings/step_*_learning.md** - Per-step comprehensive learnings (read all matching files)
  - Each file contains both success patterns (which MCP server tool calls worked to achieve the step) and failure patterns (which MCP server tool calls failed or should be avoided)
  - Files are named based on step number (e.g., step_1_learning.md, step_2_learning.md)

**File Reading Instructions**:
- Use read_workspace_file tool to read each step_X_learning.md file
- Handle missing files gracefully (they may not exist if steps haven't been executed yet)
- Extract patterns from markdown content:
  - **Success patterns**: Which MCP server tool calls (format: server_name.tool_name) successfully achieved the step description
  - **Failure patterns**: Which MCP server tool calls failed or should be avoided for achieving the step description
- Match patterns to appropriate steps based on:
  - Step number matching (e.g., step_1_learning.md → step 1)
  - Step description matching (which tools were used to achieve what goal)
  - MCP server tool relevance (tools mentioned in step description)

## 🎯 PATTERN MATCHING GUIDELINES

**Primary Focus**: Identify which MCP server tool calls successfully achieved each step's description/goal, and which failed.

**Success Patterns**:
- Extract MCP server tool calls (format: server_name.tool_name) that successfully worked to achieve the step description
- Include exact MCP server names, tool names, and arguments if available in the learning file
- Focus on tools that accomplished specific parts of the step goal
- Match to steps where:
  - Step number matches (e.g., step_1_learning.md → step 1)
  - Step description mentions similar tools or goals
  - Tools were used to achieve the same objective

**Failure Patterns**:
- Extract MCP server tool calls that failed to achieve the step description or should be avoided
- Include tools to avoid, common mistakes, or anti-patterns
- Focus on tools that didn't work for achieving the step goal
- Match to steps where:
  - Step number matches (e.g., step_1_learning.md → step 1)
  - Step description might use similar tools
  - Tools failed for similar objectives

**Matching Logic**:
1. **Primary Match**: Step number (step_X_learning.md → step X in plan)
2. **Secondary Match**: Step description content and goals
3. **Tertiary Match**: MCP server tools mentioned in step description
4. **Context Match**: Domain/context alignment (e.g., AWS-related patterns to AWS-related steps)

**IMPORTANT - MCP Server Tools Only**:
- **DO capture**: MCP server tools (format: server_name.tool_name) that relate to achieving the step description
- **DO NOT capture**: Workspace management tools (write_workspace_file, read_workspace_file, etc.)
- Focus on tools that actually accomplished (success) or failed to accomplish (failure) the step's goal

## 📤 OUTPUT FORMAT

**RETURN STRUCTURED JSON RESPONSE ONLY**

Return the enhanced plan JSON with success_patterns and failure_patterns added to each step.

**Example Enhanced Step**:
` + "```json" + `
{
  "title": "Deploy application",
  "description": "Deploy the application to production",
  "success_criteria": "Application is running and accessible",
  "context_dependencies": [],
  "context_output": "deployment_results.md",
  "success_patterns": [
    "aws.ec2_create_instance with proper security group configuration successfully achieved the deployment step",
    "aws.ec2_describe_instances verified deployment status before marking complete"
  ],
  "failure_patterns": [
    "aws.ec2_run_instances failed (deprecated API - should be avoided)",
    "Using default security groups failed (security risk)"
  ],
  "has_loop": false
}
` + "```" + `

**Pattern Format**:
- Success patterns should describe which MCP server tool (server_name.tool_name) successfully achieved the step description
- Failure patterns should describe which MCP server tool failed or should be avoided for the step description
- Focus on tools that directly relate to accomplishing the step's goal

**IMPORTANT NOTES**: 
1. Read step_X_learning.md files using read_workspace_file tool
2. Extract both success and failure patterns from each learning file
3. Match patterns intelligently to steps based on step number, description, and MCP tool relevance
4. Focus on MCP server tool calls (server_name.tool_name) that achieved or failed to achieve the step description
5. Preserve ALL existing plan data - only add patterns
6. Use empty arrays [] if no patterns match a step
7. Return ONLY valid JSON - no explanations or markdown
8. This agent enhances plans with learnings - execution is handled by other agents
9. Handle missing learnings files gracefully (return plan with empty pattern arrays)
10. Only capture MCP server tools, not workspace management tools (write_workspace_file, read_workspace_file, etc.)`

	// Parse and execute the template
	tmpl, err := template.New("learning_integration").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing learning integration template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing learning integration template: %v", err)
	}

	return result.String()
}
