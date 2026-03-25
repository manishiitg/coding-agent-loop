package step_based_workflow

import (
	"context"
	"fmt"
	"path/filepath"

	"mcp-agent-builder-go/agent_go/pkg/skills"
)

const skillCreatorName = "skill-creator"
const skillCreatorGitHubURL = "https://github.com/anthropics/skills/tree/main/skills/skill-creator"

// ensureSkillCreator ensures the Anthropic skill-creator skill is installed in the workspace.
// If it doesn't exist, installs it from GitHub. Returns the absolute path to the SKILL.md.
func (hcpo *StepBasedWorkflowOrchestrator) ensureSkillCreator(ctx context.Context) (string, error) {
	workspaceAPIURL := getWorkspaceAPIURL()
	if workspaceAPIURL == "" {
		return "", fmt.Errorf("workspace API URL not available")
	}

	skillFilePath := filepath.Join("skills", skillCreatorName, "SKILL.md")
	absPath := filepath.Join(GetPromptDocsRoot(), skillFilePath)

	// Check if it already exists
	if _, err := skills.GetSkill(workspaceAPIURL, skillCreatorName); err == nil {
		return absPath, nil
	}

	// Install from GitHub
	hcpo.GetLogger().Info(fmt.Sprintf("📦 Installing skill-creator from %s", skillCreatorGitHubURL))
	result, err := skills.ImportGitHubSkill(workspaceAPIURL, skillCreatorGitHubURL, "")
	if err != nil {
		return "", fmt.Errorf("failed to install skill-creator from GitHub: %w", err)
	}
	if !result.Success {
		return "", fmt.Errorf("failed to install skill-creator: %s", result.Error)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Installed skill-creator as '%s'", result.SkillName))
	return absPath, nil
}
