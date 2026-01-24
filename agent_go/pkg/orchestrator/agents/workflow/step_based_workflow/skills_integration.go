package step_based_workflow

import (
	"context"
	"fmt"
	"strings"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/skills"
)

// GetEffectiveSkills returns the skills to use for a step.
// Priority: step-level EnabledSkills > orchestrator-level SelectedSkills
func GetEffectiveSkills(stepConfig *AgentConfigs, orchestrator *orchestrator.BaseOrchestrator) []string {
	// Check step-level override first
	if stepConfig != nil && len(stepConfig.EnabledSkills) > 0 {
		return stepConfig.EnabledSkills
	}
	// Fall back to orchestrator-level (preset default)
	return orchestrator.GetSelectedSkills()
}

// BuildWorkflowSkillPrompt builds the system prompt section for skills in workflow mode.
// It provides paths to skills and instructions for the workflow agent to discover them.
func BuildWorkflowSkillPrompt(ctx context.Context, selectedSkills []string, bo *orchestrator.BaseOrchestrator) string {
	if len(selectedSkills) == 0 {
		return ""
	}

	var promptParts []string

	// Add skills discovery instructions for workflow agents
	promptParts = append(promptParts, `
## 🎯 Active Skills

### What is a Skill?
A skill is a reusable set of instructions that guides you on how to handle specific tasks or workflows. Skills are stored in the workspace under the "skills/" folder. Each skill contains:
- **SKILL.md**: The main instruction file with detailed guidance
- **Additional files**: Some skills may include reference files, templates, or examples

### How to Use Skills in Workflow:
1. **Read the skill**: Read the SKILL.md file from the workspace
2. **Follow the instructions**: Apply the skill's methodology to the current step
3. **Check for additional files**: List the skill folder to find supporting files

**IMPORTANT: Before executing the step, read and understand the relevant skill instructions.**

### Activated Skills:
`)

	// List each skill with its path
	for _, folderName := range selectedSkills {
		// Try to get skill metadata
		skill, err := skills.GetSkill("", folderName)
		if err != nil {
			// Still add the path even if we can't get metadata
			skillPath := fmt.Sprintf("skills/%s/SKILL.md", folderName)
			promptParts = append(promptParts, fmt.Sprintf("- **%s**: Read instructions from `%s`", folderName, skillPath))
			continue
		}

		skillPath := fmt.Sprintf("skills/%s/SKILL.md", folderName)
		promptParts = append(promptParts, fmt.Sprintf("- **%s**: %s\n  - Path: `%s`",
			skill.Frontmatter.Name,
			skill.Frontmatter.Description,
			skillPath))
	}

	promptParts = append(promptParts, `

**Action Required:** Read each skill's SKILL.md from the workspace before executing the step.
`)

	return strings.Join(promptParts, "\n")
}

// BuildSkillFolderGuardPaths builds the folder guard paths for skills.
// Returns (readPaths, writePaths) - skills are read-only
func BuildSkillFolderGuardPaths(selectedSkills []string) (readPaths []string, writePaths []string) {
	if len(selectedSkills) == 0 {
		return nil, nil
	}

	// Build list of allowed skill paths (read-only)
	readPaths = make([]string, 0, len(selectedSkills)*2)
	for _, skill := range selectedSkills {
		readPaths = append(readPaths, "skills/"+skill+"/")
		readPaths = append(readPaths, "skills/"+skill)
	}

	// No write paths for skills - they are read-only
	return readPaths, nil
}
