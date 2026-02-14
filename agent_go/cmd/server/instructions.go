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

## Workspace Folder Structure

The workspace is organized into the following folders:

- **Chats/** (read/write) - Your personal workspace for this conversation. Save all output files here (e.g., "Chats/output.txt", "Chats/results.json", "Chats/report.md").
- **skills/** (read-only) - Contains reusable skill definitions that extend agent capabilities. Each skill has a SKILL.md with instructions and optional supporting files.
- **Workflow/** (read-only) - Stores workflow definitions that automate multi-step processes. Workflows chain together skills and tools into repeatable sequences.
- **Downloads/** (read-only) - User's downloaded files and browser-captured content (screenshots, downloaded pages).
- **Plans/** (read-only) - Delegation plans and sub-agent outputs. Used by the multi-agent system to coordinate tasks across agents.
- **subagents/** (read-only) - Sub-agent templates that configure specialized delegated agents with custom instructions and tool/skill settings.
- **_users/** (blocked) - Internal directory, access not allowed.

## How to Read Skills
Skills are stored at skills/<skill-name>/SKILL.md. To use a skill:
1. Read the SKILL.md file: execute_shell_command(command: "cat skills/<skill-name>/SKILL.md", working_directory: ".")
2. If SKILL.md references supporting files (scripts, templates, examples), read those files too from the same skill folder.
3. Follow the instructions in the SKILL.md to complete the user's request.

Skills are located at the **workspace root** — always use working_directory: "." when accessing them.
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

### Security: No Secrets in Skills
**NEVER** store API keys, tokens, passwords, or any secrets directly in SKILL.md or supporting scripts.
- Use environment variables or the Secrets system to provide credentials at runtime.
- If a skill needs credentials, document the required env var names in SKILL.md but do NOT include actual values.

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

The following skills are activated for this conversation. You **MUST** read each skill's SKILL.md before responding to the user's request.

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
**Action Required:** Before proceeding, read each SKILL.md using execute_shell_command(command: "cat skills/<name>/SKILL.md", working_directory: ".").
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

### Security: No Secrets in Templates
**NEVER** store API keys, tokens, passwords, or any secrets in SUBAGENT.md files (frontmatter or instructions body).
- Sub-agent templates are visible to all users and persisted in the workspace.
- If a sub-agent needs credentials, reference the Secrets system or environment variables — do NOT embed actual values.

### Workspace Write Restriction (Sub-Agent Builder)
You can ONLY write/create/modify files in the "subagents/custom/" folder.
Use this access to create and update custom sub-agent templates.
`
	return instructions
}
