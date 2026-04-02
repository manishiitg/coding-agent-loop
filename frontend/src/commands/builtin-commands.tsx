import React from 'react'
import { FileText, Lightbulb, Download, Server, Cpu, History, GitBranch, Bot, Layers, Minimize2, Search, Sparkles, CheckCircle2, Wrench, AlertTriangle } from 'lucide-react'
import type { CommandDefinition } from './types'

export const builtinCommands: CommandDefinition[] = [
  {
    command: 'review-plan',
    description: 'Critically review the current workflow plan decisions',
    icon: <Search className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      const runFolder = ctx.getWorkflowStore().selectedRunFolder
      const focus = ctx.beforeSlash.trim()
      const focusText = focus ? ` Focus especially on: ${focus}.` : ''
      const runText = runFolder ? ` Use target_run_folder="${runFolder}" if run evidence would help.` : ''
      ctx.onSubmit(`Run review_plan now.${runText}${focusText} Return findings first.`)
    }
  },
  {
    command: 'optimize-workflow',
    description: 'Analyze the workflow structure against the objective',
    icon: <Sparkles className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      const focusText = focus ? ` Focus especially on: ${focus}.` : ''
      ctx.onSubmit(`Run optimize_workflow now.${focusText} Then summarize the top structural changes to make.`)
    }
  },
  {
    command: 'optimize-step',
    description: 'Optimize one workflow step by step id',
    icon: <Wrench className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'optimizer',
    validate: (ctx) => ctx.beforeSlash.trim() ? null : 'Usage: /optimize-step <step-id>',
    source: 'builtin',
    execute: (ctx) => {
      const stepId = ctx.beforeSlash.trim()
      ctx.onSubmit(`Run optimize_step(step_id="${stepId}") now and summarize the highest-priority fixes for that step.`)
    }
  },
  {
    command: 'replan-results',
    description: 'Rewrite the plan from actual run results',
    icon: <Layers className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'optimizer',
    validate: (ctx) => ctx.getWorkflowStore().selectedRunFolder ? null : 'Select a workflow run folder before using /replan-results',
    source: 'builtin',
    execute: (ctx) => {
      const runFolder = ctx.getWorkflowStore().selectedRunFolder
      const focus = ctx.beforeSlash.trim()
      const focusText = focus ? ` Focus especially on: ${focus}.` : ''
      ctx.onSubmit(`Run replan_workflow_from_results(target_run_folder="${runFolder}") now.${focusText} Rewrite the plan from actual results and summarize what changed.`)
    }
  },
  {
    command: 'tune-step',
    description: 'Run a step, audit its mode/learnings/tools, then optimize',
    icon: <Wrench className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'optimizer',
    validate: (ctx) => ctx.beforeSlash.trim() ? null : 'Usage: /tune-step <step-id>',
    source: 'builtin',
    execute: (ctx) => {
      const stepId = ctx.beforeSlash.trim()
      ctx.onSubmit(`Do a full tune of step "${stepId}". Run all steps below autonomously without pausing for confirmation — only stop at the very end to give a final summary.

1. **Execute**: Run execute_step(step_id="${stepId}") and wait for completion. Note what it produced and whether it succeeded.
2. **Read step definition**: Read plan.json for this step. Check the description — is it specific, actionable, and unambiguous for the execution agent? Check the validation_schema — does it enforce the right output shape? Are required fields correct and types precise?
3. **Best practices audit**: Does the step follow best practices? Flag any of: vague description, missing validation_schema, hardcoded values that should be variables, missing context dependencies, wrong step type for the task, or instructions that would confuse a junior agent.
4. **Check mode**: Read step_config.json. Is the current execution mode (code_exec / tool_search / simple) the right choice? Stable reusable logic → code_exec. Runtime discovery or browser-heavy → tool_search. Single known tool call → simple.
5. **Check learnings**: Read any learnings saved for this step. Are they accurate, complete, and still relevant?
6. **Check pre-discovered tools**: Are pre_discovered_tools correct for this step's needs? Missing tools slow it down; wrong tools waste context.
7. **Optimize**: Run optimize_step(step_id="${stepId}") passing your findings from steps 2–6 as the focus.

Do not wait for my input between steps. Once all 7 steps are done, give a single summary of what changed and what the step's final state is.`)
    }
  },
  {
    command: 'mark-workflow-optimized',
    description: 'Run the readiness gate and show the checklist',
    icon: <CheckCircle2 className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      ctx.onSubmit('Run mark_workflow_optimized now and show the readiness checklist.')
    }
  },
  {
    command: 'audit-descriptions',
    description: 'Check all steps for description vs skill/learning confusion',
    icon: <AlertTriangle className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      const focusText = focus ? ` Pay special attention to: ${focus}.` : ''
      ctx.onSubmit(`Audit every step's description in plan.json. For each step, do the following:

1. Read the step description from plan.json.
2. Read the step's SKILL.md / learnings (if any exist).
3. Check for these problems:

   **Description vs Skill Confusion:**
   - **Description contains runtime learnings**: The description should be an *instruction* (what to do), not a *retrospective* (what worked last time). Phrases like "use batch mode because single inserts timeout", "avoid X which caused failures", or specific tool parameter values discovered at runtime belong in SKILL.md, not the description.
   - **Skill contains task instructions**: The SKILL.md should capture *reusable patterns and pitfalls discovered during execution*, not restate what the step is supposed to do. If the skill reads like a task description, it's confused.
   - **Duplication**: Same guidance appearing in both description and skill — pick one home.
   - **Description is vague because it defers to skill**: The description says something like "follow the skill" or "see learnings" instead of giving clear instructions.

   **Hardcoded Values:**
   - **Hardcoded paths**: Absolute paths ("/app/workspace-docs/...", "/Users/...", "/home/...") or specific local paths. Should use relative paths instead.
   - **Hardcoded run/iteration paths**: References to specific run folders like "runs/iteration-0/...", "execution/step-3/...", or hardcoded group names like "group-1". These break across different runs and groups — the orchestrator resolves these at runtime via context_dependencies.
   - **Hardcoded credentials/secrets**: API keys, tokens, passwords, auth headers, or any sensitive values. Should reference SECRET_* environment variables instead.
   - **Hardcoded IDs/URLs/user-specific values**: Specific spreadsheet IDs, database names, API endpoints, user IDs, email addresses, phone numbers, account numbers, or other environment-specific values. Should use variable placeholders (e.g., {USER_ID}, {SHEET_ID}, {EMAIL}) in descriptions, with actual values in variables.json / variable groups.

   **Todo Task / Orchestrator Description Quality (for todo_task steps only):**
   - **Missing objective/intent**: The orchestrator description should clearly state WHAT we are trying to achieve — the goal and purpose of this orchestration. Without this, the orchestrator can't make intelligent decisions or handle unexpected situations.
   - **Reduced to a sequencer**: If the description is just "call route A, then route B, then route C" or a fixed execution order, it's a script not orchestration. The orchestrator is a capable LLM — its description should enable it to reason about what to do, not just follow a checklist. If fixed sequencing is all that's needed, these should be regular steps instead.
   - **No edge case / failure guidance**: The description should explain how to handle failures, retries, partial results, or unexpected states. The orchestrator's value is making decisions when things don't go as planned.
   - **Inline execution logic**: Detailed task instructions for a specific sub-task written inside the orchestrator description instead of being a sub-agent route. Each distinct task should be its own route with its own description, learnings, and tools.
   - **Duplicates sub-agent descriptions**: The orchestrator restates what sub-agents do instead of focusing on dispatch logic and decision-making.
   - **No routing criteria**: The description doesn't explain WHEN or WHY to use each route — the orchestrator needs to know what conditions or inputs map to which sub-agent.

For each step, report:
- Step ID (and step type)
- Status: CLEAN, CONFUSED (description/skill issues), HARDCODED (hardcoded values found), or WEAK_ORCHESTRATOR (for todo_task steps with orchestrator issues) — a step can have multiple
- If issues found: which problems and a concrete fix suggestion

End with a summary table of all steps and their status.${focusText}`)
    }
  },
  {
    command: 'audit-orchestrators',
    description: 'Audit todo_task orchestrator descriptions for quality',
    icon: <AlertTriangle className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      const focusText = focus ? ` Pay special attention to: ${focus}.` : ''
      ctx.onSubmit(`Audit all todo_task steps in plan.json. For each todo_task step, read its todo_task_step description and all its predefined_routes sub-agent descriptions. Check for these problems:

**Orchestrator Description Quality:**
- **Missing objective/intent**: The orchestrator description must clearly state WHAT we are trying to achieve — the overall goal and purpose. Without this, the orchestrator can't make intelligent decisions when things go wrong or when it encounters unexpected situations. A good orchestrator description answers: "Why do these sub-agents exist together? What outcome are we after?"
- **Reduced to a sequencer**: If the description is just "run route A, then route B, then route C" or a fixed checklist, the orchestrator is being wasted. It's a capable LLM — its description should enable reasoning, not just list steps. If all it does is follow a fixed order, these should be regular steps in sequence instead.
- **No edge case / failure guidance**: The description should explain how to handle failures, retries, partial results, missing data, or unexpected states from sub-agents. What happens if a sub-agent fails? Skip it? Retry? Use a fallback? The orchestrator's core value is making decisions when things don't go as planned.
- **No routing criteria**: The description doesn't explain WHEN or WHY to pick each route. The orchestrator needs to know what conditions, inputs, or states map to which sub-agent.

**Orchestrator vs Sub-Agent Boundary:**
- **Inline execution logic**: Detailed task instructions for a specific sub-task written inside the orchestrator description. Each distinct task should be its own route with its own description, learnings, and tools. The orchestrator should dispatch, not execute.
- **Duplicates sub-agent descriptions**: The orchestrator restates what sub-agents already describe. The orchestrator should focus on coordination and decision-making, not repeat execution details.
- **Sub-agent descriptions too vague**: Sub-agent route descriptions that are too thin because all the detail is in the orchestrator. Each sub-agent should be self-contained — a junior agent reading only its own description should know exactly what to do.

**Hardcoded Values (same checks as regular steps):**
- Hardcoded paths, run/iteration paths, credentials, IDs, group names, or user-specific values in orchestrator or sub-agent descriptions.

For each todo_task step, report:
- Step ID
- Orchestrator description verdict: GOOD or issues found
- Per sub-agent route: route ID + verdict
- Concrete fix suggestions for each issue

End with a summary table.${focusText}`)
    }
  },
  {
    command: 'summarize',
    description: 'Summarize conversation history',
    icon: <FileText className="w-4 h-4" />,
    modes: ['multi-agent'],
    source: 'builtin',
    execute: (ctx) => {
      if (ctx.tabSessionId && !ctx.isSummarizing && !ctx.isStreaming) {
        ctx.handleSummarize(ctx.beforeSlash || undefined)
      }
    }
  },
  {
    command: 'compact',
    description: 'Compact conversation context',
    icon: <Minimize2 className="w-4 h-4" />,
    modes: ['workflow', 'multi-agent'],
    hidden: true,
    source: 'builtin',
    execute: (ctx) => {
      if (ctx.tabSessionId && !ctx.isSummarizing && !ctx.isStreaming) {
        ctx.handleCompact(ctx.beforeSlash || undefined)
      }
    }
  },
  {
    command: 'build-skill',
    description: 'Build a new skill using the skill-creator',
    icon: <Lightbulb className="w-4 h-4" />,
    modes: ['multi-agent'],
    source: 'builtin',
    execute: (ctx) => {
      const currentSkills = ctx.tabConfig?.selectedSkills || []
      if (!currentSkills.includes('skill-creator')) {
        ctx.setTabConfig(ctx.activeTabId, { selectedSkills: [...currentSkills, 'skill-creator'] })
      }
      const wsStore = ctx.getWorkspaceStore()
      const expanded = new Set(wsStore.expandedFolders)
      expanded.add('skills')
      expanded.add('skills/custom')
      wsStore.setExpandedFolders(expanded)
      const skillContext = 'Refer to the skill-creator skill at skills/custom/skill-creator/SKILL.md for instructions on how to build skills.'
      const message = ctx.beforeSlash
        ? `${ctx.beforeSlash}\n\n${skillContext}`
        : `I want to build a skill based on our conversation. ${skillContext}`
      ctx.onSubmit(message)
    }
  },
  {
    command: 'build-subagent',
    description: 'Build a new sub-agent template',
    icon: <Bot className="w-4 h-4" />,
    modes: ['multi-agent'],
    source: 'builtin',
    execute: (ctx) => {
      const currentSkills = ctx.tabConfig?.selectedSkills || []
      if (!currentSkills.includes('subagent-creator') && !currentSkills.includes('custom/subagent-creator')) {
        ctx.setTabConfig(ctx.activeTabId, { selectedSkills: [...currentSkills, 'custom/subagent-creator'] })
      }
      const wsStore = ctx.getWorkspaceStore()
      const expanded = new Set(wsStore.expandedFolders)
      expanded.add('subagents')
      expanded.add('subagents/custom')
      wsStore.setExpandedFolders(expanded)
      const saContext = 'You are in Sub-Agent Builder mode. Create a new sub-agent template in subagents/custom/. Follow the SUBAGENT.md format with YAML frontmatter (name, description, default_reasoning_level, default_tool_mode) and markdown instructions.'
      const message = ctx.beforeSlash
        ? `${ctx.beforeSlash}\n\n${saContext}`
        : `I want to build a sub-agent template. ${saContext}`
      ctx.onSubmit(message)
    }
  },
  {
    command: 'add-skill',
    description: 'Import a skill from GitHub',
    icon: <Download className="w-4 h-4" />,
    modes: ['multi-agent'],
    source: 'builtin',
    execute: (ctx) => {
      ctx.openDialog('skillImport')
    }
  },
  {
    command: 'mcp',
    description: 'View MCP server details and tools',
    icon: <Server className="w-4 h-4" />,
    modes: ['multi-agent'],
    source: 'builtin',
    execute: (ctx) => {
      ctx.getAppStore().setWorkspaceMinimized(true)
      ctx.openDialog('mcpDetails')
    }
  },
  {
    command: 'mcp-add',
    description: 'Add or edit MCP server configuration',
    icon: <Server className="w-4 h-4" />,
    modes: ['multi-agent'],
    source: 'builtin',
    execute: (ctx) => {
      ctx.getAppStore().setWorkspaceMinimized(true)
      ctx.openDialog('mcpConfig')
    }
  },
  {
    command: 'models',
    description: 'Open LLM model configuration',
    icon: <Cpu className="w-4 h-4" />,
    modes: ['multi-agent'],
    source: 'builtin',
    execute: (ctx) => {
      ctx.getAppStore().setWorkspaceMinimized(true)
      ctx.openDialog('models')
    }
  },
  {
    command: 'resume',
    description: 'Resume a previous conversation',
    icon: <History className="w-4 h-4" />,
    modes: ['multi-agent'],
    source: 'builtin',
    execute: (ctx) => {
      ctx.openDialog('resume')
    }
  },
  {
    command: 'spawn',
    description: 'Enable simple sub-agent delegation (fire-and-forget)',
    icon: <GitBranch className="w-4 h-4" />,
    modes: ['multi-agent'],
    source: 'builtin',
    execute: (ctx) => {
      ctx.getAppStore().setDelegationMode('spawn')
      ctx.addToast('Simple delegation enabled - Agent can delegate tasks to sub-agents', 'success')
    }
  },
  {
    command: 'nospawn',
    description: 'Disable all sub-agent delegation',
    icon: <GitBranch className="w-4 h-4" />,
    modes: ['multi-agent'],
    source: 'builtin',
    execute: (ctx) => {
      ctx.getAppStore().setDelegationMode('off')
      ctx.addToast('Sub-agent delegation disabled', 'success')
    }
  },
  {
    command: 'workflow-builder',
    description: 'Generate a workflow spec markdown from this chat',
    icon: <Layers className="w-4 h-4" />,
    modes: ['multi-agent'],
    source: 'builtin',
    execute: (ctx) => {
      const workflowContext = `Create a workflow specification markdown file from this conversation.

Requirements:
- Analyze this chat and extract all required implementation steps.
- Include "Goal", "Constraints", "Required Tools & MCP Servers", "Key Learnings", "Step-by-step Plan", "Parallel Execution Plan", "Validation Checklist", and "Open Questions".
- Make each step actionable and self-contained.
- In "Required Tools & MCP Servers", list exact tool names/MCP servers needed per step and why.
- In "Parallel Execution Plan", identify which tasks can run in parallel vs what is on the critical path.
- Capture important implementation learnings from this conversation and add reusable lessons for future runs.
- Save the output as a .md file in the workspace (for example under Chats/), so I can manually upload/use it later.
- Return a concise summary plus the exact saved file path.`

      const message = ctx.beforeSlash
        ? `${ctx.beforeSlash}\n\n${workflowContext}`
        : workflowContext

      ctx.onSubmit(message)
    }
  },
  {
    command: 'compress-memory',
    description: 'Compress and clean up agent memories',
    icon: <Minimize2 className="w-4 h-4" />,
    modes: ['multi-agent'],
    source: 'builtin',
    execute: (ctx) => {
      const msg = ctx.beforeSlash
        ? `Compress and consolidate my memories, focusing on: ${ctx.beforeSlash}. Use compress_memory.`
        : 'Compress and consolidate all my memories. Use compress_memory to read all files, merge related entries, remove superseded info, and reduce verbosity.'
      ctx.onSubmit(msg)
    }
  }
]
