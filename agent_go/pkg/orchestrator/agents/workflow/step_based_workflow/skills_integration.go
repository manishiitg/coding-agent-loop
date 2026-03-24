package step_based_workflow

import (
	"context"
	"fmt"
	"path/filepath"
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
// It provides skill metadata, absolute paths, and instructions for the agent to read them.
// docsRoot is the absolute workspace docs path (e.g., "/app/workspace-docs/") for building absolute skill paths.
func BuildWorkflowSkillPrompt(ctx context.Context, selectedSkills []string, bo *orchestrator.BaseOrchestrator, docsRoot string) string {
	if len(selectedSkills) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n## Active Skills\n\n")
	sb.WriteString("Skills provide reusable instructions for specific tasks. **Read each skill's SKILL.md before executing the step.**\n\n")

	for _, folderName := range selectedSkills {
		skillPath := filepath.Join(docsRoot, "skills", folderName, "SKILL.md")

		skill, err := skills.GetSkill("", folderName)
		if err != nil {
			// Skill not found — provide folder name and path so agent can still attempt to read it
			sb.WriteString(fmt.Sprintf("- **%s** — `%s`\n", folderName, skillPath))
			continue
		}

		name := skill.Frontmatter.Name
		if name == "" {
			name = folderName
		}
		sb.WriteString(fmt.Sprintf("- **%s** — `%s`", name, skillPath))
		if skill.Frontmatter.Description != "" {
			sb.WriteString(fmt.Sprintf("\n  %s", skill.Frontmatter.Description))
		}
		sb.WriteString("\n")
	}

	return sb.String()
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
