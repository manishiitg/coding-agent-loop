package step_based_workflow

import (
	"fmt"
	"path/filepath"
	"strings"

	prompt "github.com/manishiitg/mcpagent/agent/prompt"
)

// PromptSections holds pre-built prompt sections that can be injected into any agent's
// system prompt. All agent types (execution, todo task, conditional, evaluation) should
// use these common builders for consistency.
type PromptSections struct {
	CodeExecution string // Code execution or tool search mode instructions
	Learnings     string // Formatted learning history section
	PreviousSteps string // Previous steps context section
}

// BuildCodeExecutionSection returns the code execution or tool search mode instructions.
// This is the single source of truth for code execution/tool search prompt sections.
// isCodeExecution: agent uses code execution mode (HTTP API calls via shell)
// isToolSearch: agent uses tool search mode (search_tools/add_tool/remove_tool)
// workspacePath: absolute workspace path for code examples
func BuildCodeExecutionSection(isCodeExecution bool, isToolSearch bool, workspacePath string) string {
	if isCodeExecution {
		return prompt.GetCodeExecutionInstructions(workspacePath)
	}
	if isToolSearch {
		return prompt.GetToolSearchInstructions()
	}
	return ""
}

// BuildLearningsSection returns the formatted learning history section for the system prompt.
// learningHistory: the formatted learning content (empty string means no learnings)
// keepLearningFull: whether full learning content is included (vs paths-only)
func BuildLearningsSection(learningHistory string, keepLearningFull bool) string {
	if learningHistory == "" {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Learning History (Secondary Guidance)\n")
	sb.WriteString(learningHistory)
	sb.WriteString("\n\n")
	sb.WriteString("- **Workflows**: Use validated sequences from learnings, but adapt args to this specific step.\n")
	sb.WriteString("- **Patterns**: Use tool hints/error recovery patterns from learnings.\n")
	sb.WriteString("- **Conflict**: If learning conflicts with step requirement, the step wins.\n")
	if !keepLearningFull {
		sb.WriteString("- **Note**: These learnings are incomplete. Rely primarily on the step description and your own capabilities.\n")
	}

	return sb.String()
}

// BuildPreviousStepsSection returns the previous steps context section for the system prompt.
// previousStepsSummary: the formatted summary from buildPreviousStepsSummary()
func BuildPreviousStepsSection(previousStepsSummary string) string {
	if previousStepsSummary == "" {
		return ""
	}
	return previousStepsSummary
}

// BuildVariablesSection returns the variables section for the system prompt.
// variableNames: formatted variable names (empty if no variables)
// variableValues: formatted variable values (empty if no values)
func BuildVariablesSection(variableNames string, variableValues string) string {
	if variableNames == "" {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Variables\n")
	sb.WriteString(variableNames)
	sb.WriteString("\n")
	if variableValues != "" {
		sb.WriteString(fmt.Sprintf("**Values**: %s\n", variableValues))
	}
	sb.WriteString("\n**Handling**: Step descriptions are already resolved. For code and tool calls, use the resolved values directly.\n")
	return sb.String()
}

// ResolveDependencyPaths maps dependency filenames to full absolute paths by matching them to
// the producing step's execution folder. This is the common logic used by both execution and
// todo task agents to show full paths instead of bare filenames.
func ResolveDependencyPaths(
	deps []string,
	stepIndex int,
	allSteps []PlanStepInterface,
	executionWorkspacePath string,
	docsRoot string,
	variableValues map[string]string,
) []string {
	if len(deps) == 0 {
		return nil
	}
	toAbs := func(path string) string {
		if path == "" || docsRoot == "" {
			return path
		}
		return filepath.Join(docsRoot, path)
	}

	fullPathDeps := make([]string, 0, len(deps))
	for _, dep := range deps {
		fullPath := dep // Default to bare filename if no match found
		for j := 0; j < stepIndex && j < len(allSteps); j++ {
			prevOutput := ResolveVariables(allSteps[j].GetContextOutput().String(), variableValues)
			if prevOutput == dep {
				prevStepPath := fmt.Sprintf("step-%d", j+1)
				if allSteps[j].StepType() == StepTypeDecision {
					prevStepPath = fmt.Sprintf("step-%d-decision", j+1)
				}
				prevStepExecPath := getExecutionFolderPath(executionWorkspacePath, prevStepPath)
				fullPath = fmt.Sprintf("%s/%s", toAbs(prevStepExecPath), dep)
				break
			}
		}
		fullPathDeps = append(fullPathDeps, fullPath)
	}
	return fullPathDeps
}

// GetPromptDocsRoot returns the workspace docs root path for use in prompts.
// Always returns /app/workspace-docs — all agents (execution, todo task, conditional)
// run inside Docker where workspace-docs is mounted at this path.
func GetPromptDocsRoot() string {
	return "/app/workspace-docs"
}

// toAbsPaths converts a slice of workspace-relative paths to absolute paths by prepending docsRoot.
func toAbsPaths(docsRoot string, paths []string) []string {
	result := make([]string, len(paths))
	for i, p := range paths {
		if p == "" || docsRoot == "" {
			result[i] = p
		} else {
			result[i] = filepath.Join(docsRoot, p)
		}
	}
	return result
}
