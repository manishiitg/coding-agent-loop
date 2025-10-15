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

	specificInstructions := `## 🔍 DEPENDENCY ANALYSIS: IDENTIFY INDEPENDENT STEPS

**ANALYSIS PRIORITY**: Analyze the execution plan and identify steps that can be executed in parallel

## 📋 ANALYSIS CHECKLIST:
1. **Read Planning Result**: Analyze the provided execution plan thoroughly
2. **Identify Dependencies**: Examine each step for dependencies on other steps
3. **Assess Data Dependencies**: Identify data flow dependencies between steps
4. **Determine Independence**: Mark steps as independent only if 100% certain
5. **Document Reasoning**: Provide clear reasoning for each independence assessment

## Core Principles:
1. **Independence Verification**: Only mark steps as independent if you are absolutely certain they have no dependencies
2. **Conservative Approach**: When in doubt, mark steps as dependent rather than independent
3. **Clear Documentation**: Provide clear reasoning for why steps are independent or dependent
4. **Parallel Optimization**: Focus on identifying the maximum number of truly independent steps

## Important Guidelines:
- Be extremely conservative - only mark steps as independent if you are 100% certain
- Think about data flow and information dependencies
- Consider execution context and environment state

Remember: It's better to have fewer parallel steps that are truly independent than to have many steps that might conflict with each other.

## Expected Output Format:
Step 1: [Description]
- Dependencies: [List of dependencies or "None"]
- Independent: [true/false]
- Reasoning: [Explanation]

Step 2: [Description]
- Dependencies: [List of dependencies or "None"]
- Independent: [true/false]
- Reasoning: [Explanation]

[Continue for all steps...]

Focus on identifying the maximum number of truly independent steps that can be executed in parallel without conflicts.`

	// Build the complete prompt
	completePrompt := mm.GetBasePromptTemplate(agentType, agentDescription, specificContext, specificInstructions, "")

	return &PlanBreakdownPrompts{
		AnalyzeDependenciesPrompt: completePrompt,
	}
}
