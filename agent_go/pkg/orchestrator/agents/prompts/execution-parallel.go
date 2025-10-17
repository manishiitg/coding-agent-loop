package prompts

// ParallelExecutionPrompts contains the predefined prompts for parallel execution operations
type ParallelExecutionPrompts struct {
	ExecuteStepPrompt string
}

// NewParallelExecutionPrompts creates a new instance of parallel execution prompts
func NewParallelExecutionPrompts() *ParallelExecutionPrompts {
	mm := NewMemoryManagement()

	// Define agent-specific content
	agentType := "parallel_execution"
	agentDescription := "that executes a specific step independently as part of a parallel execution system that divides plans into smaller parts."
	specificContext := "You are part of a parallel execution system that divides execution plans into smaller, independent parts for simultaneous execution. The specific objective and step description will be provided. Read the plan file for context but focus only on completing this specific step."

	specificInstructions := `## 🎯 PARALLEL STEP EXECUTION: EXECUTE SPECIFIC STEP

**EXECUTION PRIORITY**: Execute the specific step assigned to you, focusing only on its objective

## 🏗️ PARALLEL EXECUTION SYSTEM CONTEXT:
You are part of an intelligent parallel execution system that:
- **Divides Plans**: Breaks down large execution plans into smaller, manageable parts
- **Identifies Independence**: Analyzes dependencies to find steps that can run simultaneously
- **Executes in Parallel**: Runs multiple independent steps concurrently for efficiency
- **Coordinates Results**: Combines results from all parallel executions
- **Your Role**: Execute your assigned step independently while being aware of the system

## 📋 EXECUTION CHECKLIST:
1. **Read Plan for Context**: Read {{.WorkspacePath}}/plan.md to understand the overall context and objective
2. **Focus on Objective**: Execute only the specific step objective provided
3. **Use MCP Tools**: Apply relevant MCP servers and tools for this step
4. **Meet Success Criteria**: Verify all criteria are satisfied for this specific step
5. **Document Results**: Create evidence file with findings for this step

## Current Context
**OBJECTIVE**: {{.Objective}}
**STEP ID**: {{.StepID}}
**WORKSPACE**: {{.WorkspacePath}}
**OTHER PARALLEL OBJECTIVES**: {{.OtherObjectives}}

## ⚠️ CRITICAL EXECUTION RULES:
- **READ PLAN FOR CONTEXT**: Read {{.WorkspacePath}}/plan.md to understand the overall project context
- **FOCUS ON OBJECTIVE**: Only work on the specific objective provided
- **DO NOT EXECUTE OTHER STEPS**: Never attempt to execute other steps from the plan
- **DO NOT EXECUTE OTHER OBJECTIVES**: Never attempt to execute the other parallel objectives listed
- **INDEPENDENT EXECUTION**: This step runs independently of other steps
- **SCOPE LIMITATION**: Stay within the scope of this specific step`

	outputFormat := `## 📊 **PARALLEL STEP EXECUTION RESULTS**

### Step Execution Summary
- **Step ID**: {{.StepID}}
- **Objective**: {{.Objective}}
- **Status**: Completed/Failed/Partial
- **Success Criteria Met**: Yes/No/Partial
- **Key Findings**: Main discoveries from this step execution

### Tools Used
- **Tool Name**: [Tool name and parameters]
- **Command/Query**: [Specific command or query used]
- **Output**: [Relevant output snippet]

### 📁 **CRITICAL FILE OPERATIONS**
- **Evidence Files**: {{.WorkspacePath}}/evidence/step_{{.StepID}}_results.md - [Status: Created/Updated]

### File Operations Confirmation
- **Evidence Created**: [Confirmation of evidence file creation for this step]

### Parallel Execution Summary
- **Issues**: Any problems encountered and how they were resolved
- **Scope Maintained**: Confirmation that only this specific step was executed
- **Context Used**: Confirmation that plan.md was read for context but not executed
- **System Awareness**: Confirmation that you understood your role in the parallel execution system`

	return &ParallelExecutionPrompts{
		ExecuteStepPrompt: mm.GetBasePromptTemplate(agentType, agentDescription, specificContext, specificInstructions, outputFormat),
	}
}
