package server

import (
	"encoding/json"
	"fmt"
	"log"
	"path"
	"sort"
	"strings"

	browserinstructions "mcp-agent-builder-go/agent_go/pkg/instructions"
	"mcp-agent-builder-go/agent_go/pkg/skills"
	"mcp-agent-builder-go/agent_go/pkg/utils"
)

// AgentInstructions contains custom instructions for both React and Simple agents
type AgentInstructions struct {
	ResponseFormatting string
}

// GetAgentInstructions returns the custom instructions for agents.
// workspaceAbsPath is the absolute filesystem path to the workspace root (e.g. "/app/workspace-docs/_users/default").
// If empty, only relative paths are shown.
func GetAgentInstructions(workspaceAbsPath string) string {
	instructions := utils.GetCommonFileInstructions()

	// Add workspace root path info
	wsPathNote := ""
	if workspaceAbsPath != "" {
		wsPathNote = fmt.Sprintf("\n**Workspace root (absolute path):** `%s`\nAll relative paths below are relative to this root.\n", workspaceAbsPath)
	}

	// Add chat mode folder restriction note
	instructions += `

## Workspace Folder Structure
` + wsPathNote + `
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
1. Read the SKILL.md file: execute_shell_command(command: "cat skills/<skill-name>/SKILL.md")
2. If SKILL.md references supporting files (scripts, templates, examples), read those files too from the same skill folder.
3. Follow the instructions in the SKILL.md to complete the user's request.

Skills are located at the **workspace root** — use workspace-relative paths (e.g., "skills/<skill-name>/SKILL.md") when accessing them.
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

// buildSkillPrompt builds the system prompt section for selected skills.
// It provides paths to skills and instructions for the agent to discover them using workspace tools.
// workspaceAbsPath is optional — if provided, absolute paths are also shown alongside relative ones.
func buildSkillPrompt(selectedSkills []string, workspaceAbsPath string, workspaceAPIURL ...string) string {
	if len(selectedSkills) == 0 {
		return ""
	}

	var promptParts []string

	// Add skills discovery instructions
	promptParts = append(promptParts, `
## Available Skills

The following skills are available for this conversation. Each skill extends your capabilities with specialized instructions and tools.

**Important:** Before taking any significant action (creating a plan, delegating tasks, executing multi-step work, or performing analysis), read the SKILL.md files for relevant skills so you understand what capabilities are available and can use them effectively. For simple conversational messages (greetings, clarifying questions, etc.), you do not need to read skills first — just respond naturally.

### Available Skills:
`)

	// List each skill with its path (relative + absolute)
	for _, folderName := range selectedSkills {
		wsURL := ""
		if len(workspaceAPIURL) > 0 {
			wsURL = workspaceAPIURL[0]
		}
		skill, err := skills.GetSkill(wsURL, folderName)
		relPath := fmt.Sprintf("skills/%s/SKILL.md", folderName)
		absNote := ""
		if workspaceAbsPath != "" {
			absNote = fmt.Sprintf("\n  - Absolute: `%s`", path.Join(workspaceAbsPath, relPath))
		}
		if err != nil {
			log.Printf("[SKILLS] Warning: Failed to load skill metadata %s: %v", folderName, err)
			promptParts = append(promptParts, fmt.Sprintf("- **%s**: Read instructions from `%s`%s", folderName, relPath, absNote))
			continue
		}

		promptParts = append(promptParts, fmt.Sprintf("- **%s**: %s\n  - Path: `%s`%s",
			skill.Frontmatter.Name,
			skill.Frontmatter.Description,
			relPath,
			absNote))
	}

	promptParts = append(promptParts, `
**How to read skills efficiently:**
1. **Quick scan first:** Use execute_shell_command(command: "head -100 skills/<name>/SKILL.md") to read the first few lines (frontmatter + description) of each skill to determine if it is relevant to the user's request.
2. **Read fully if relevant:** If a skill matches the user's intent, read the complete file: execute_shell_command(command: "cat skills/<name>/SKILL.md"). Then read any supporting files (scripts, templates, examples) referenced in the SKILL.md.
3. **Skip if not relevant:** If the description clearly does not match the user's request, move on — no need to read the full file.
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

// buildWorkflowContextPrompt builds rich context about selected workflows for injection into chat system prompt.
// Provides comprehensive context about the workflow.
// Includes full plan.json, step config, variables, execution history with step-level detail,
// file location guide with step naming conventions, and learnings.
func buildWorkflowContextPrompt(paths []string, workspaceAPIURL string) string {
	if len(paths) == 0 || workspaceAPIURL == "" {
		return ""
	}

	client := skills.NewWorkspaceAPIClient(workspaceAPIURL)
	var sections []string

	sections = append(sections, "\n## Workflow Context\n\nThe following workflow(s) have been selected for this conversation. Use the information below to answer questions about workflow structure, execution history, and debugging.\n")

	for _, wsPath := range paths {
		section := buildSingleWorkflowContext(client, wsPath)
		if section != "" {
			sections = append(sections, section)
		}
	}

	if len(sections) <= 1 {
		return "" // No workflow context was actually built
	}

	return strings.Join(sections, "\n")
}

// buildSingleWorkflowContext builds comprehensive context for a single workflow path
func buildSingleWorkflowContext(client *skills.WorkspaceAPIClient, wsPath string) string {
	var parts []string
	workflowName := path.Base(wsPath)

	parts = append(parts, fmt.Sprintf("### Workflow: %s\n", workflowName))
	parts = append(parts, fmt.Sprintf("**Workspace Path:** `%s/`\n", wsPath))

	// 0. Workflow memory (memory/ folder) — user-saved knowledge for this workflow
	// Also check legacy instructions.md for backward compatibility
	memoryDir := path.Join(wsPath, "memory")
	memoryContent := readDirectoryMarkdownFiles(client, memoryDir)
	if memoryContent == "" {
		// Fallback: read legacy instructions.md
		memoryContent = readFileContent(client, path.Join(wsPath, "instructions.md"))
	}
	if memoryContent != "" {
		parts = append(parts, "**Workflow Memory:**")
		parts = append(parts, memoryContent)
		parts = append(parts, "")
	}

	// 1. Full plan.json content (not a summary — the agent needs the real data)
	planContent := readFileContent(client, path.Join(wsPath, "planning", "plan.json"))
	if planContent != "" {
		parts = append(parts, "**Current Plan (plan.json):**")
		parts = append(parts, "```json")
		parts = append(parts, planContent)
		parts = append(parts, "```")
		parts = append(parts, "")
	}

	// 2. Step config (per-step LLM, tool, and mode settings)
	stepConfig := readFileContent(client, path.Join(wsPath, "planning", "step_config.json"))
	if stepConfig != "" {
		parts = append(parts, "**Step Config (step_config.json):**")
		parts = append(parts, "```json")
		parts = append(parts, stepConfig)
		parts = append(parts, "```")
		parts = append(parts, "")
	}

	// 3. Variables
	varsSummary := buildVariablesSummary(client, wsPath)
	if varsSummary != "" {
		parts = append(parts, "**Variables:**")
		parts = append(parts, varsSummary)
		parts = append(parts, "")
	}

	// 4. Execution history with step-level detail
	execSummary := buildExecutionSummary(client, wsPath)
	if execSummary != "" {
		parts = append(parts, "**Execution History:**")
		parts = append(parts, execSummary)
		parts = append(parts, "")
	}

	// 5. Learnings overview
	learningsSummary := buildLearningsSummary(client, wsPath)
	if learningsSummary != "" {
		parts = append(parts, "**Learnings:**")
		parts = append(parts, learningsSummary)
		parts = append(parts, "")
	}

	// 6. File locations guide (matching plan improvement agent's detail level)
	parts = append(parts, fmt.Sprintf(`**File Locations:**
- Plan file: `+"`%s/planning/plan.json`"+`
- Step config: `+"`%s/planning/step_config.json`"+` — per-step LLM, tool, and mode settings
- Variables: `+"`%s/variables/variables.json`"+`
- Learnings: `+"`%s/learnings/`"+` and `+"`%s/learnings/{step_id}/`"+`
- Knowledgebase: `+"`%s/knowledgebase/`"+` — persistent files across runs
- Runs: `+"`%s/runs/`"+`
- Evaluation reports: `+"`%s/evaluation/runs/{runFolder}/evaluation_report.json`"+`
`, wsPath, wsPath, wsPath, wsPath, wsPath, wsPath, wsPath, wsPath))

	// 7. Step folder naming conventions and log file guide
	parts = append(parts, `**Step Folder Naming (inside execution/ and logs/):**
- Regular steps: `+"`step-{X}/`"+` (X = 1-based step number)
- Conditional branches: `+"`step-{X}-if-true-{idx}/`"+`, `+"`step-{X}-if-false-{idx}/`"+`
- Decision steps: `+"`step-{X}-decision/`"+`
- Sub-agents (orchestration/todo_task): `+"`step-{X}-sub-agent-{idx}/`"+`
- Generic agents (todo_task only): `+"`step-{X}-generic-agent-{idx}/`"+`

**Key Log Files Per Step:**
- All steps: `+"`logs/step-X/validation-{N}.json`"+` (validation attempts), `+"`logs/step-X/execution/execution-attempt-{A}-iteration-{I}.json`"+` (execution result)
- Full LLM conversation: `+"`logs/step-X/execution/execution-attempt-{A}-iteration-{I}-conversation.json`"+`
- Conditional: `+"`logs/step-X/conditional-evaluation.json`"+` — condition_result, condition_reason, branch_executed
- Decision: `+"`logs/step-X/decision-evaluation.json`"+` — decision_result, decision_reasoning, routing targets
- Orchestration/TodoTask: `+"`logs/step-X/orchestration-execution.json`"+` (JSONL, one line per iteration)
- TodoTask progress: `+"`execution/step-X/tasks.md`"+` — markdown task list with checkbox progress
- Steps done: `+"`execution/steps_done.json`"+` — which steps completed, branch decisions, retry counts

**How to Investigate:**
- Read plan: `+"`read_file`"+` on `+"`{path}/planning/plan.json`"+`
- Check step output: `+"`read_file`"+` on `+"`{path}/runs/{iteration}/execution/step-{N}_*.json`"+`
- Check step logs: `+"`list_files`"+` on `+"`{path}/runs/{iteration}/logs/step-{N}/`"+`
- Check progress: `+"`read_file`"+` on `+"`{path}/runs/{iteration}/execution/steps_done.json`"+`
- Check learnings: `+"`list_files`"+` on `+"`{path}/learnings/`"+`
- All paths are workspace-relative (e.g., "Workflow/myproject/plan.md")
`)

	return strings.Join(parts, "\n")
}

// readFileContent reads a file and returns its content, or empty string on error
func readFileContent(client *skills.WorkspaceAPIClient, filePath string) string {
	content, err := client.ReadFile(filePath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(content)
}

// readDirectoryMarkdownFiles reads all .md files from a directory and concatenates them.
// Returns empty string if directory doesn't exist or has no .md files.
func readDirectoryMarkdownFiles(client *skills.WorkspaceAPIClient, dirPath string) string {
	entries, err := client.ListFiles(dirPath)
	if err != nil {
		return ""
	}

	var memoryParts []string
	for _, entry := range entries {
		if entry.Type != "file" || !strings.HasSuffix(entry.Filepath, ".md") {
			continue
		}
		content := readFileContent(client, entry.Filepath)
		if content != "" {
			memoryParts = append(memoryParts, content)
		}
	}
	if len(memoryParts) == 0 {
		return ""
	}
	return strings.Join(memoryParts, "\n\n---\n\n")
}

// variableEntry represents a variable in variables.json
type variableEntry struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Group string `json:"group"`
}

// variablesManifest represents the full variables.json structure
type variablesManifest struct {
	Variables []variableEntry `json:"variables"`
	Groups    []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	} `json:"groups"`
}

// buildVariablesSummary reads variables.json and summarizes
func buildVariablesSummary(client *skills.WorkspaceAPIClient, wsPath string) string {
	varsPath := path.Join(wsPath, "variables", "variables.json")
	content, err := client.ReadFile(varsPath)
	if err != nil {
		return "" // Variables are optional
	}

	var manifest variablesManifest
	if err := json.Unmarshal([]byte(content), &manifest); err != nil {
		return ""
	}

	var lines []string
	for _, v := range manifest.Variables {
		preview := v.Value
		if len(preview) > 80 {
			preview = preview[:80] + "..."
		}
		groupStr := ""
		if v.Group != "" {
			groupStr = fmt.Sprintf(" (group: %s)", v.Group)
		}
		lines = append(lines, fmt.Sprintf("- {{%s}}: %s%s", v.Name, preview, groupStr))
	}

	if len(manifest.Groups) > 0 {
		var groupDescs []string
		for _, g := range manifest.Groups {
			status := "disabled"
			if g.Enabled {
				status = "enabled"
			}
			groupDescs = append(groupDescs, fmt.Sprintf("%s (%s)", g.Name, status))
		}
		lines = append(lines, fmt.Sprintf("Groups: %s", strings.Join(groupDescs, ", ")))
	}

	return strings.Join(lines, "\n")
}

// buildExecutionSummary lists iteration folders with step-level progress detail
func buildExecutionSummary(client *skills.WorkspaceAPIClient, wsPath string) string {
	runsPath := path.Join(wsPath, "runs")
	entries, err := client.ListFiles(runsPath)
	if err != nil {
		return "" // No runs yet
	}

	// Filter to iteration folders
	var iterations []string
	for _, entry := range entries {
		if entry.Type == "folder" {
			iterations = append(iterations, entry.Filepath)
		}
	}

	if len(iterations) == 0 {
		return ""
	}

	// Sort iterations to show latest last
	sort.Strings(iterations)

	var lines []string
	for _, iterPath := range iterations {
		iterName := path.Base(iterPath)
		progress := getIterationProgress(client, iterPath)
		lines = append(lines, fmt.Sprintf("- %s: %s", iterName, progress))
	}

	// For the latest iteration, show detailed steps_done.json content
	latestIter := iterations[len(iterations)-1]
	stepsDoneContent := readFileContent(client, path.Join(latestIter, "execution", "steps_done.json"))
	if stepsDoneContent != "" {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("Latest run (%s) steps_done.json:", path.Base(latestIter)))
		lines = append(lines, "```json")
		lines = append(lines, stepsDoneContent)
		lines = append(lines, "```")
	}

	return strings.Join(lines, "\n")
}

// getIterationProgress reads steps_done.json to determine progress
func getIterationProgress(client *skills.WorkspaceAPIClient, iterPath string) string {
	// steps_done.json is inside execution/ folder
	doneFile := path.Join(iterPath, "execution", "steps_done.json")
	content, err := client.ReadFile(doneFile)
	if err != nil {
		return "no progress data"
	}

	// steps_done.json is typically an array of completed step objects
	var stepsDone []json.RawMessage
	if err := json.Unmarshal([]byte(content), &stepsDone); err != nil {
		return "unable to parse progress"
	}

	return fmt.Sprintf("%d steps completed", len(stepsDone))
}

// buildLearningsSummary lists which steps have learnings
func buildLearningsSummary(client *skills.WorkspaceAPIClient, wsPath string) string {
	learningsPath := path.Join(wsPath, "learnings")
	entries, err := client.ListFiles(learningsPath)
	if err != nil {
		return "" // No learnings yet
	}

	if len(entries) == 0 {
		return "No learnings recorded yet."
	}

	var lines []string
	for _, entry := range entries {
		name := path.Base(entry.Filepath)
		if entry.Type == "folder" {
			lines = append(lines, fmt.Sprintf("- `learnings/%s/` (step-specific learnings)", name))
		} else {
			lines = append(lines, fmt.Sprintf("- `learnings/%s` (shared learning)", name))
		}
	}

	return strings.Join(lines, "\n")
}

// getGWSQuickStartInstructions returns inline instructions for using Google Workspace via the gws CLI.
func getGWSQuickStartInstructions() string {
	return browserinstructions.GetGWSQuickStartInstructions()
}
