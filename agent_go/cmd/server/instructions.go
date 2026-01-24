package server

import (
	"fmt"
	"log"
	"strings"

	"mcp-agent-builder-go/agent_go/pkg/skills"
	"mcp-agent-builder-go/agent_go/pkg/utils"
)

// AgentInstructions contains custom instructions for both React and Simple agents
type AgentInstructions struct {
	ResponseFormatting string
}

// GetAgentInstructions returns the custom instructions for agents
func GetAgentInstructions() string {
	instructions := utils.GetCommonFileInstructions()

	// Add chat mode folder restriction note
	instructions += `

## Workspace Write Restriction
You can only write/create/modify files in the "Chats/" folder. All other folders are read-only.
If you need to save output, create files in "Chats/" (e.g., "Chats/output.txt", "Chats/results.json").
`
	return instructions
}

// buildSkillPrompt builds the system prompt section for selected skills
// It provides paths to skills and instructions for the agent to discover them using workspace tools
func buildSkillPrompt(selectedSkills []string) string {
	if len(selectedSkills) == 0 {
		return ""
	}

	var promptParts []string

	// Add skills discovery instructions
	promptParts = append(promptParts, `
## Active Skills

### What is a Skill?
A skill is a reusable set of instructions that guides you on how to handle specific tasks or workflows. Skills are stored in the workspace under the "skills/" folder. Each skill contains:
- **SKILL.md**: The main instruction file with detailed guidance on how to perform a specific task
- **Additional files**: Some skills may include reference files, templates, or examples

### How to Use Skills:
1. **Read the skill first**: Use execute_shell_command with "cat skills/<skill-name>/SKILL.md" to read the skill instructions
2. **Follow the instructions**: The SKILL.md contains step-by-step guidance - follow it carefully
3. **Check for additional files**: Use "ls -la skills/<skill-name>/" to see if there are supporting files
4. **Apply to user's request**: Use the skill's methodology to address what the user is asking for

**IMPORTANT: Before responding to the user's request, you MUST first read and understand the skill instructions.**

### Activated Skills:
`)

	// List each skill with its path
	for _, folderName := range selectedSkills {
		skill, err := skills.GetSkill("", folderName)
		if err != nil {
			log.Printf("[SKILLS] Warning: Failed to load skill metadata %s: %v", folderName, err)
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

**Action Required:** Before proceeding with the user's request, use execute_shell_command with "cat" to read each skill's SKILL.md and follow the instructions within.
`)

	return strings.Join(promptParts, "\n")
}
