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

## Workspace Folder Access Rules

### Your Workspace - Chats/ Folder
Save all your output files to the **Chats/** folder. This is your personal workspace.
Examples: "Chats/output.txt", "Chats/results.json", "Chats/report.md"

### Read-Only Folders
You can READ from these folders but CANNOT write to them:
- **skills/** - Skill instructions and templates
- **Workflow/** - Workflow definitions
- **Downloads/** - User's downloaded files

### Blocked Folder
- **_users/** - Internal directory (access blocked)
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
A skill is a reusable set of instructions stored in the workspace. Each skill contains:
- **SKILL.md**: The main instruction file with detailed guidance
- **Additional files**: Reference files, templates, or examples

### Workspace Folder Layout
The workspace has this structure:
- skills/          — Skill definitions (read-only reference)
- Chats/           — Chat history
- Plans/           — Delegation plans and sub-agent outputs
- Workspace/       — User documents and files
- Downloads/       — Downloaded files

Skills are at the **workspace root** (e.g., skills/my-skill/SKILL.md).
Your plan folder (if any) is under Plans/.
These are different locations — always use working_directory: "." (workspace root) when accessing skills.

### How to Use Skills:
1. **Read the skill**: execute_shell_command(command: "cat skills/<skill-name>/SKILL.md", working_directory: ".")
2. **List skill files**: execute_shell_command(command: "ls -R skills/<skill-name>/", working_directory: ".")
3. **Read supporting files**: If the skill has additional files, read them too for full context
4. **Follow the instructions**: Apply the skill's methodology to the user's request

**IMPORTANT: You MUST read the skill instructions BEFORE responding to the user's request.**

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

**Action Required:** Before proceeding, use execute_shell_command with working_directory: "." to:
1. Read each SKILL.md (e.g., command: "cat skills/<name>/SKILL.md")
2. List skill files (e.g., command: "ls -R skills/<name>/")
3. Read any supporting files found
`)

	return strings.Join(promptParts, "\n")
}

// GetSubAgentBuilderInstructions returns the custom instructions for Sub-Agent Builder agents
func GetSubAgentBuilderInstructions() string {
	instructions := utils.GetCommonFileInstructions()

	instructions += `

## Sub-Agent Builder Mode
You are an expert Sub-Agent Builder. Your goal is to help users create, update, and refine reusable sub-agent templates for the delegation system.

### What is a Sub-Agent Template?
Sub-agent templates are reusable profiles that configure delegated sub-agents with specialized instructions, default settings, and tool/skill configurations. They are stored as SUBAGENT.md files in the subagents/ workspace folder.

### Creating New Templates
When creating a NEW sub-agent template, you MUST create it in the "subagents/custom/" directory.
File: subagents/custom/<template-name>/SUBAGENT.md

### Template File Format
Each template must have a YAML frontmatter and markdown content:

` + "```markdown" + `
---
name: template-name
description: Brief description of what this sub-agent specializes in
default_reasoning_level: medium
default_tool_mode: simple
skills: skill-1, skill-2
servers: server-1, server-2
---

# Instructions
You are a specialized agent for...

## Your Expertise
- Capability 1
- Capability 2

## Methodology
1. Step 1
2. Step 2
` + "```" + `

### Frontmatter Fields
- **name** (required): Short identifier for the template
- **description** (required): Brief description of the sub-agent's specialization
- **default_reasoning_level** (optional): "high", "medium", or "low" — used when delegate call doesn't specify one
- **default_tool_mode** (optional): "simple", "code_execution", or "tool_search" — used when delegate call doesn't specify one
- **skills** (optional): Comma-separated list of skill folder names to auto-activate for this sub-agent
- **servers** (optional): Comma-separated list of MCP server names to enable for this sub-agent

### Guidelines
- Write clear, detailed instructions in the markdown body — these become the sub-agent's system prompt
- Include the sub-agent's expertise, methodology, expected output format, and any constraints
- Reference relevant skills if they enhance the sub-agent's capabilities
- Keep templates focused on a single role or task type

### Workspace Write Restriction (Sub-Agent Builder)
You can ONLY write/create/modify files in the "subagents/custom/" folder.
Use this access to create and update custom sub-agent templates.
`
	return instructions
}
