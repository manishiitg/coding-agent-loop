package step_based_workflow

import (
	"context"
	"fmt"
	"path/filepath"

	"mcp-agent-builder-go/agent_go/pkg/skills"
)

// SystemSkill defines a skill that should be auto-installed on startup
type SystemSkill struct {
	Source string // CLI source (owner/repo@skill or owner/repo for all)
	Name   string // Expected skill folder name after install
}

// GetSystemSkills returns the list of skills that should be auto-installed on startup.
// Add new required skills here.
func GetSystemSkills() []SystemSkill {
	return []SystemSkill{
		{Source: "anthropics/skills@skill-creator", Name: "skill-creator"},
	}
}

// ensureSkillCreator ensures the Anthropic skill-creator skill is installed in the workspace.
// If it doesn't exist, installs it via the skills CLI. Returns the absolute path to the SKILL.md.
func (hcpo *StepBasedWorkflowOrchestrator) ensureSkillCreator(ctx context.Context) (string, error) {
	return ensureSkillInstalled(ctx, "skill-creator")
}

// ensureSkillInstalled ensures a skill is installed. Checks workspace first,
// installs via CLI if not found. Returns the absolute path to the SKILL.md.
func ensureSkillInstalled(ctx context.Context, skillName string) (string, error) {
	workspaceAPIURL := getWorkspaceAPIURL()
	if workspaceAPIURL == "" {
		return "", fmt.Errorf("workspace API URL not available")
	}

	skillFilePath := filepath.Join("skills", skillName, "SKILL.md")
	absPath := filepath.Join(GetPromptDocsRoot(), skillFilePath)

	// Check if it already exists
	if _, err := skills.GetSkill(workspaceAPIURL, skillName); err == nil {
		return absPath, nil
	}

	// Look up the source from system skills
	var source string
	for _, ss := range GetSystemSkills() {
		if ss.Name == skillName {
			source = ss.Source
			break
		}
	}
	if source == "" {
		return "", fmt.Errorf("skill '%s' not found and no known source to install from", skillName)
	}

	// Install via CLI
	result, err := skills.ImportToWorkspace(ctx, workspaceAPIURL, source)
	if err != nil {
		return "", fmt.Errorf("failed to install skill '%s' from %s: %w", skillName, source, err)
	}

	// Verify it was installed
	for _, name := range result.InstalledSkills {
		if name == skillName {
			return absPath, nil
		}
	}
	return "", fmt.Errorf("skill '%s' was not found in source %s", skillName, source)
}

// SyncSystemSkills ensures all system skills are installed.
// Call this on server startup. Skips skills that already exist.
// Returns the number of newly installed skills and any errors.
func SyncSystemSkills(ctx context.Context, workspaceAPIURL string) (installed int, errors []string) {
	if !skills.IsAvailable() {
		errors = append(errors, "skills CLI (npx) not available — skipping system skills sync")
		return 0, errors
	}

	systemSkills := GetSystemSkills()

	// Group by source to batch installs
	sourceSkills := make(map[string][]string) // source -> list of skill names
	var toInstall []SystemSkill

	for _, ss := range systemSkills {
		// Check if already exists
		if _, err := skills.GetSkill(workspaceAPIURL, ss.Name); err == nil {
			continue // Already installed
		}
		toInstall = append(toInstall, ss)
		sourceSkills[ss.Source] = append(sourceSkills[ss.Source], ss.Name)
	}

	if len(toInstall) == 0 {
		return 0, nil
	}

	// Install missing skills
	for _, ss := range toInstall {
		result, err := skills.ImportToWorkspace(ctx, workspaceAPIURL, ss.Source)
		if err != nil {
			errors = append(errors, fmt.Sprintf("failed to install %s: %v", ss.Name, err))
			continue
		}
		for _, name := range result.InstalledSkills {
			if name == ss.Name {
				installed++
				break
			}
		}
	}

	return installed, errors
}
