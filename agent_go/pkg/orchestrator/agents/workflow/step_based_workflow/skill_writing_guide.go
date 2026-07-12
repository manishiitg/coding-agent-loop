package step_based_workflow

import (
	"context"
	"fmt"

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
