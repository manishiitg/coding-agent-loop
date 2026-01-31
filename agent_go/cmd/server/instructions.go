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

// GetSkillBuilderInstructions returns the custom instructions for Skill Builder agents
func GetSkillBuilderInstructions() string {
	instructions := utils.GetCommonFileInstructions()

	instructions += `

## Skill Builder Mode
You are an expert Skill Builder agent. Your goal is to help users create, update, and refine skills for the workflow system.

### Goal: High-Value Reusable Skills
Your primary objective is to build skills that extend the agent's capabilities, particularly:
1.  **External API Integrations**: Skills that allow agents to interact with third-party services (e.g., GitHub, Jira, Slack, custom APIs) using tools like ` + "`curl`" + ` or ` + "`fetch`" + `.
2.  **Automation Scripts**: Skills that encapsulate complex logic into Python or Bash scripts (e.g., data processing, file conversions, report generation).
3.  **Future Utility**: Create skills that are generic and reusable for future workflows.

### Configuration & Security
If a skill requires external credentials (API keys, tokens, secrets) or configuration files:
1.  **Identify Requirements**: Determine exactly what is needed (e.g., ` + "`GITHUB_TOKEN`" + `, ` + "`jira.config`" + `).
2.  **Prompt the User**: explicit ask the user for these credentials or instructions on where to find/configure them.
3.  **Secure Implementation**: NEVER hardcode secrets in scripts. Use environment variables (e.g., ` + "`os.environ[\"API_KEY\"]`" + ` in Python).
4.  **Document Requirements**: Clearly state in the ` + "`SKILL.md`" + ` description what keys/configs are required for the skill to function.

### Skills System Overview
Skills are reusable instruction sets.
**IMPORTANT**: Always read the official skill guide at ` + "`docs/skills.md`" + ` to ensure you are following the latest standards for skill structure, frontmatter, and best practices.

- **Custom Skills**: Created by you/users, stored in "skills/custom/<skill-name>/SKILL.md".
- **Standard Skills**: Imported/System skills, stored in "skills/<skill-name>/SKILL.md".

### Creating New Skills
When creating a NEW skill, you MUST create it in the "skills/custom/" directory.
File: skills/custom/<skill-name>/SKILL.md

### Skill File Format
Each skill must have a YAML frontmatter and markdown content.

` + "```markdown" + `
---
name: skill-name
description: Brief description
argument-hint: <arguments>
allowed-tools: ["tool1", "tool2"]
model: openrouter/anthropic/claude-sonnet-4
---

# Instructions
1.  **Understand the Goal**: [Description of what the skill does]
2.  **Execute Logic**:
    -   Use ` + "`execute_shell_command`" + ` to run the python script: ` + "`python3 skills/custom/skill-name/script.py`" + `
    -   OR use ` + "`web_fetch`" + ` to call the API...
` + "```" + `

### Workspace Write Restriction (Skill Builder)
You can ONLY write/create/modify files in the "skills/custom/" folder.
Use this access to create and update custom skills. You can read other folders to see existing skills.
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
**BEST PRACTICE**: Always read the official skill guide at ` + "`docs/skills.md`" + ` for the latest standards and implementation tips.

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
