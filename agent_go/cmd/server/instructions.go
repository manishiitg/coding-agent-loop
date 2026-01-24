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
	// 	return `

	// **File Operations Protocol:**
	// When working with files, follow this CRITICAL 5-step workflow:
	// 1. **READ FIRST**: 🚨 MANDATORY - Always use read_workspace_file to see exact current content
	// 2. **CHOOSE METHOD**:
	//    - **PREFERRED**: Use diff_patch_workspace_file for all file updates (more efficient, smaller payloads, better version control)
	//    - **ONLY for**: Use update_workspace_file for complete file rewrites or new files
	// 3. **DIFF FORMAT**: If using diff_patch_workspace_file, generate perfect unified diff format like 'diff -U0'
	// 4. **CONTEXT MATCHING**: 🚨 CRITICAL - Context lines (starting with space) must match file content EXACTLY
	// 5. **VERIFY**: Test your approach before applying changes

	// **Diff Patch Requirements:**
	// - ✅ Use read_workspace_file first to get exact file content
	// - ✅ Copy context lines EXACTLY from the file (including spaces/tabs)
	// - ✅ Ensure diff ends with a newline character
	// - ✅ Use proper unified diff format with ---/+++ headers
	// - ✅ Generate diffs like 'diff -U0' would produce
	// - ✅ Verify line numbers in hunk headers match actual file

	// **🚨 CRITICAL CONTEXT LINE FORMAT:**
	// - Context lines MUST start with SPACE ( ), NOT minus (-)!
	// - Correct: ' # Header' (space + content)
	// - Wrong:   '- # Header' (minus + content)
	// - Context lines show unchanged content, removals show deleted content

	// ` + utils.GetCommonFileInstructions() + `

	// `
	return utils.GetCommonFileInstructions()

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
