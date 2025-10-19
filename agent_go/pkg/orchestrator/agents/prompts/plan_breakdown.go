package prompts

// PlanBreakdownPrompts contains the predefined prompts for plan breakdown operations
type PlanBreakdownPrompts struct {
	AnalyzeDependenciesPrompt string
}

// NewPlanBreakdownPrompts creates a new instance of plan breakdown prompts
func NewPlanBreakdownPrompts() *PlanBreakdownPrompts {
	mm := NewMemoryManagement()

	// Define agent-specific content
	agentType := "plan_breakdown"
	agentDescription := "that analyzes execution plans and identifies independent steps that can be executed in parallel."
	specificContext := "The planning result, objective, and workspace path will be provided in the user message."

	specificInstructions := `## 🔍 DEPENDENCY ANALYSIS: IDENTIFY INDEPENDENT STEPS WITH DETAILED EXECUTION DESCRIPTIONS

**ANALYSIS PRIORITY**: Analyze the execution plan and identify steps that can be executed in parallel, providing detailed execution descriptions

## 📋 ANALYSIS CHECKLIST:
1. **Read Planning Result**: Analyze the provided execution plan thoroughly
2. **Identify Dependencies**: Examine each step for dependencies on other steps
3. **Assess Data Dependencies**: Identify data flow dependencies between steps
4. **Determine Independence**: Mark steps as independent only if 100% certain
5. **Generate Detailed Descriptions**: Create comprehensive execution descriptions for each step
6. **Document Reasoning**: Provide clear reasoning for each independence assessment

## Core Principles:
1. **Independence Verification**: Only mark steps as independent if you are absolutely certain they have no dependencies
2. **Conservative Approach**: When in doubt, mark steps as dependent rather than independent
3. **Clear Documentation**: Provide clear reasoning for why steps are independent or dependent
4. **Detailed Execution**: Generate comprehensive descriptions that explain exactly what needs to be executed
5. **Parallel Optimization**: Focus on identifying the maximum number of truly independent steps

## Important Guidelines:
- Be extremely conservative - only mark steps as independent if you are 100% certain
- Think about data flow and information dependencies
- Consider execution context and environment state
- Generate detailed execution descriptions that specify exactly what actions need to be taken
- Include specific tools, commands, or operations required for each step

Remember: It's better to have fewer parallel steps that are truly independent than to have many steps that might conflict with each other.

## Current Context
**OBJECTIVE**: {{.Objective}}
**WORKSPACE**: {{.WorkspacePath}}
**PLANNING RESULT**: {{.PlanningResult}}

## Expected Output Format:
Return a JSON object with the following structure:
{
  "steps": [
    {
      "id": "step_1",
      "description": "Detailed execution description specifying exactly what needs to be done, including specific tools, commands, operations, and expected outcomes",
      "dependencies": ["step_2", "step_3"] or [],
      "is_independent": true or false,
      "reasoning": "Clear explanation for independence assessment"
    }
  ]
}

## Description Requirements:
- **Specific Actions**: Detail exactly what actions need to be performed
- **Tools Required**: Specify which MCP tools or commands are needed
- **Expected Outcomes**: Describe what should be achieved
- **Success Criteria**: Define how to measure completion
- **Context**: Include any relevant context or constraints

Focus on identifying the maximum number of truly independent steps that can be executed in parallel without conflicts, with each step having a comprehensive execution description.`

	// Build the complete prompt
	completePrompt := mm.GetBasePromptTemplate(agentType, agentDescription, specificContext, specificInstructions, "")

	return &PlanBreakdownPrompts{
		AnalyzeDependenciesPrompt: completePrompt,
	}
}
