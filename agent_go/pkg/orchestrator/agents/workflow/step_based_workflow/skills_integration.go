package step_based_workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	sb.WriteString("Skills provide reusable best practices and instructions for specific tasks. They are not specific to this execution, but they should guide how you do the work. **Read each skill's relevant files before executing the step.**\n\n")

	for _, folderName := range selectedSkills {
		skillDir := filepath.Join(docsRoot, "skills", folderName)
		skillPath := filepath.Join(skillDir, "SKILL.md")

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
		sb.WriteString(fmt.Sprintf("- **%s** — `%s/`", name, skillDir))
		if skill.Frontmatter.Description != "" {
			sb.WriteString(fmt.Sprintf("\n  %s", skill.Frontmatter.Description))
		}
		files := listSkillManifestFiles(skillDir)
		if len(files) > 0 {
			sb.WriteString("\n  Available files:")
			for _, file := range files {
				sb.WriteString(fmt.Sprintf("\n  - `%s`", file))
			}
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func listSkillManifestFiles(skillDir string) []string {
	var results []string

	var walk func(currentPath string, relPrefix string)
	walk = func(currentPath string, relPrefix string) {
		entries, err := os.ReadDir(currentPath)
		if err != nil {
			return
		}
		for _, entry := range entries {
			name := entry.Name()
			if shouldSkipSkillManifestEntry(name) {
				continue
			}
			relPath := name
			if relPrefix != "" {
				relPath = filepath.Join(relPrefix, name)
			}
			if entry.IsDir() {
				walk(filepath.Join(currentPath, name), relPath)
				continue
			}
			results = append(results, relPath)
		}
	}

	walk(skillDir, "")

	sort.Strings(results)
	if idx := sort.SearchStrings(results, "SKILL.md"); idx < len(results) && results[idx] == "SKILL.md" && idx != 0 {
		results[0], results[idx] = results[idx], results[0]
	}
	return results
}

func shouldSkipSkillManifestEntry(name string) bool {
	if name == "" {
		return true
	}
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "node_modules", "__pycache__":
		return true
	default:
		return false
	}
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
