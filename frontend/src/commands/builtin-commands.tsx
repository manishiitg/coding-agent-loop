import React from 'react'
import { FileText, Lightbulb, Download, Server, Cpu, Bot, Layers, Minimize2, AlertTriangle, RefreshCw, Wrench, GitBranch, CheckCircle, Search, Lock, Beaker } from 'lucide-react'
import type { CommandDefinition } from './types'

export const builtinCommands: CommandDefinition[] = [
  {
    command: 'design-flow',
    description: 'Validate context dependency chain between steps',
    icon: <GitBranch className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'builder',
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      ctx.onSubmit(`Call get_workflow_command_guidance(kind="design-flow", focus=${JSON.stringify(focus)}) and follow the returned instructions verbatim. The tool returns the canonical guided-flow text for this command — do not paraphrase or skip its steps.`)
    }
  },
  {
    command: 'ready-to-optimize',
    description: 'Check if workflow is ready to move to optimizer mode',
    icon: <CheckCircle className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'builder',
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      ctx.onSubmit(`Call get_workflow_command_guidance(kind="ready-to-optimize", focus=${JSON.stringify(focus)}) and follow the returned instructions verbatim. The tool returns the canonical guided-flow text for this command — do not paraphrase or skip its steps.`)
    }
  },
  {
    command: 'review-plan',
    description: 'Critically analyze the workflow design for weaknesses and improvements',
    icon: <Search className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: ['builder', 'optimizer', 'run'],
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      ctx.onSubmit(`Call get_workflow_command_guidance(kind="review-plan", focus=${JSON.stringify(focus)}) and follow the returned instructions verbatim. The tool returns the canonical guided-flow text for this command — do not paraphrase or skip its steps.`)
    }
  },
  {
    command: 'review-goal-alignment',
    description: 'Goal-vs-outcome alignment: does a real run achieve the objective, are success_criteria met, does eval measure them',
    icon: <CheckCircle className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      const runFolder = ctx.getWorkflowStore().selectedRunFolder
      ctx.onSubmit(`Call get_workflow_command_guidance(kind="review-goal-alignment", focus=${JSON.stringify(focus)}, run_folder=${JSON.stringify(runFolder || '')}) and follow the returned instructions verbatim. The tool returns the canonical guided-flow text for this command — do not paraphrase or skip its steps.`)
    }
  },
  {
    command: 'review-speed',
    description: 'Review workflow latency and how to make it faster',
    icon: <Minimize2 className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      const runFolder = ctx.getWorkflowStore().selectedRunFolder
      ctx.onSubmit(`Call get_workflow_command_guidance(kind="review-speed", focus=${JSON.stringify(focus)}, run_folder=${JSON.stringify(runFolder || '')}) and follow the returned instructions verbatim. The tool returns the canonical guided-flow text for this command — do not paraphrase or skip its steps.`)
    }
  },
  {
    command: 'review-cost',
    description: 'Review workflow cost and how to reduce it safely',
    icon: <Cpu className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      const runFolder = ctx.getWorkflowStore().selectedRunFolder
      ctx.onSubmit(`Call get_workflow_command_guidance(kind="review-cost", focus=${JSON.stringify(focus)}, run_folder=${JSON.stringify(runFolder || '')}) and follow the returned instructions verbatim. The tool returns the canonical guided-flow text for this command — do not paraphrase or skip its steps.`)
    }
  },
  {
    command: 'review-config',
    description: 'Review per-step KB / db / lock_learnings / lock_code recommendations',
    icon: <Lock className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: ['builder', 'optimizer', 'run'],
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      ctx.onSubmit(`Call get_workflow_command_guidance(kind="review-config", focus=${JSON.stringify(focus)}) and follow the returned instructions verbatim. The tool returns the canonical guided-flow text for this command — do not paraphrase or skip its steps.`)
    }
  },
  {
    command: 'improve-report',
    description: 'Validate reports/report_plan.json and suggest layout/color improvements',
    icon: <CheckCircle className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'reporting',
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      ctx.onSubmit(`Call get_workflow_command_guidance(kind="improve-report", focus=${JSON.stringify(focus)}) and follow the returned instructions verbatim. The tool returns the canonical guided-flow text for this command — do not paraphrase or skip its steps.`)
    }
  },
  {
    command: 'improve-eval',
    description: 'Validate evaluation/evaluation_plan.json and improve goal/criteria coverage',
    icon: <CheckCircle className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      const runFolder = ctx.getWorkflowStore().selectedRunFolder
      ctx.onSubmit(`Call get_workflow_command_guidance(kind="improve-eval", focus=${JSON.stringify(focus)}, run_folder=${JSON.stringify(runFolder || '')}) and follow the returned instructions verbatim. The tool returns the canonical guided-flow text for this command — do not paraphrase or skip its steps.`)
    }
  },
  {
    command: 'improve-continuously',
    description: 'Set up recurring workflow run + slower recurring optimizer improvement',
    icon: <Bot className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      ctx.onSubmit(`Call get_workflow_command_guidance(kind="improve-continuously", focus=${JSON.stringify(focus)}) and follow the returned instructions verbatim. The tool returns the canonical guided-flow text for this command — do not paraphrase or skip its steps.`)
    }
  },
  {
    command: 'improve-workflow',
    description: 'Use existing run evidence to review, replan if needed, harden, then optionally verify',
    icon: <RefreshCw className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      const runFolder = ctx.getWorkflowStore().selectedRunFolder
      ctx.onSubmit(`Call get_workflow_command_guidance(kind="improve-workflow", focus=${JSON.stringify(focus)}, iteration=${JSON.stringify(runFolder ? runFolder.split('/')[0] : '')}) and follow the returned instructions verbatim. The tool returns the canonical guided-flow text for this command — do not paraphrase or skip its steps.`)
    }
  },
  {
    command: 'review-code',
    description: 'Review saved scripts (main.py) against step descriptions to detect drift',
    icon: <FileText className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      ctx.onSubmit(`Call get_workflow_command_guidance(kind="review-code", focus=${JSON.stringify(focus)}) and follow the returned instructions verbatim. The tool returns the canonical guided-flow text for this command — do not paraphrase or skip its steps.`)
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
      const saContext = 'You are in Sub-Agent Builder mode. Create a new sub-agent template in subagents/custom/. Follow the SUBAGENT.md format with YAML frontmatter (name, description, default_reasoning_level) and markdown instructions.'
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
    command: 'workflow-builder',
    description: 'Turn this conversation into a reusable workflow (Workflow/<name>/)',
    icon: <Layers className="w-4 h-4" />,
    modes: ['multi-agent'],
    source: 'builtin',
    execute: (ctx) => {
      const instruction = `Turn our current conversation into a new reusable workflow by calling the \`create_workflow\` tool with a valid workflow.json and plan.json.

## Step 1 — Pick a folder_name AND a display label
Workflows have two separate names:
- **folder_name** (the on-disk path under \`Workflow/\`) — must be **shell-safe kebab-case**: lowercase letters/digits with hyphens between words, no spaces, no underscores, no uppercase, no special characters (e.g. "customer-onboarding", "sales-report", "api-health-check"). 2-5 words, ≤64 chars.
- **label** (the human-readable display name that goes in \`workflow_json.label\`) — can be any string: spaces, capitalization, punctuation, whatever reads naturally (e.g. "Customer Onboarding", "AWS Cost Analysis Q3", "Müller's Pipeline").

If I gave you a label in my preamble, keep it verbatim as the \`label\` and slugify it for the \`folder_name\`. If I gave you a kebab-case name, use it for \`folder_name\` and also as the starting point for \`label\` (titlecased). Otherwise infer both from what we've been working on. If you cannot produce a clean folder_name, ask me one clarifying question instead of proceeding.

## Step 2 — Pick the capabilities from context
Analyze this conversation and select ONLY the MCP servers, skills, and LLM tier settings that are actually relevant to the workflow being extracted. **Do not blindly copy every currently-enabled server and skill — pick the ones the steps actually need.** If a server was enabled in chat but never used for this specific work, leave it out.

## Step 3 — Extract the steps
Re-read the conversation and extract the concrete, repeatable steps the workflow should run. Each step must have:
- A stable kebab-case \`id\` (e.g. "fetch-data", "analyze-results"), unique within the plan
- A human \`title\`
- A detailed \`description\` of what the step does, in enough detail that a worker with no memory of this conversation could execute it
- A \`success_criteria\` line describing how to tell the step succeeded
- Optionally \`context_dependencies\` (file names produced by earlier steps) and \`context_output\` (file name this step produces)
- Most steps should use \`"type": "regular"\`. Use \`"conditional"\` / \`"routing"\` / \`"human_input"\` / \`"todo_task"\` only when the conversation genuinely called for branching or human-in-the-loop.

## Step 4 — Call create_workflow
Build the two JSON objects yourself in this turn and call the privileged tool:

\`create_workflow(folder_name: "<kebab-name>", workflow_json: {..., label: "<human-readable>", ...}, plan_json: {...})\`

**IMPORTANT**: Use the \`create_workflow\` tool — do NOT try to \`mkdir\` or write files with shell commands. The \`Workflow/\` folder is read-only to normal shell writes; \`create_workflow\` is the only path that can create a new workflow folder. The tool validates folder_name (shell-safe kebab-case), enforces required JSON fields, refuses to overwrite existing workflows, and writes both files in one call.

The workflow.json schema (required: schema_version, id, label) and the plan.json schema (required: steps array with type/id/title) are already documented in your system prompt — follow that shape exactly.

## Step 5 — Report back to me
After the tool returns, tell me:
- The folder path returned by the tool
- The display label
- A one-line summary of what the workflow does
- The step IDs + titles (numbered list)
- Tell me I can pick it from the workflow picker to activate it.`

      const message = ctx.beforeSlash
        ? `${ctx.beforeSlash}\n\n${instruction}`
        : instruction

      ctx.onSubmit(message)
    }
  },
  // ===== Auto-improvement framework =====
  // See docs/workflow/auto_improvement_framework.md.
  // Note: business-context capture is intentionally NOT a slash command.
  // The builder agent's system prompt teaches it to recognize business
  // rules in conversation and offer to persist them via the
  // capture_context tool. A separate slash command would be redundant.
  {
    command: 'improve-setup-framework',
    description: 'One-time setup: write the Workflow Profile to improve.md and bootstrap metrics for the auto-improvement framework',
    icon: <Wrench className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      ctx.onSubmit(`Call get_workflow_command_guidance(kind="improve-setup-framework", focus=${JSON.stringify(focus)}) and follow the returned instructions verbatim. The tool returns the canonical guided-flow text for this command — do not paraphrase or skip its steps.`)
    }
  },
  {
    command: 'propose-experiment',
    description: 'Open ONE experiment: pick a metric, formulate a hypothesis, apply the intervention through the framework gate',
    icon: <Beaker className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      ctx.onSubmit(`Call get_workflow_command_guidance(kind="propose-experiment", focus=${JSON.stringify(focus)}) and follow the returned instructions verbatim. The tool returns the canonical guided-flow text for this command — do not paraphrase or skip its steps.`)
    }
  },
  {
    command: 'exp-abort',
    description: 'Revert and abort the active experiment',
    icon: <AlertTriangle className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      ctx.onSubmit(`Call get_workflow_command_guidance(kind="exp-abort", focus=${JSON.stringify(focus)}) and follow the returned instructions verbatim. The tool returns the canonical guided-flow text for this command — do not paraphrase or skip its steps.`)
    }
  },
  {
    command: 'exp-extend',
    description: 'Add more measurement runs to the active experiment',
    icon: <RefreshCw className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      ctx.onSubmit(`Call get_workflow_command_guidance(kind="exp-extend", focus=${JSON.stringify(focus)}) and follow the returned instructions verbatim. The tool returns the canonical guided-flow text for this command — do not paraphrase or skip its steps.`)
    }
  },
  {
    command: 'exp-conclude',
    description: 'Manually render a verdict for the active experiment (overrides evaluator)',
    icon: <CheckCircle className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      ctx.onSubmit(`Call get_workflow_command_guidance(kind="exp-conclude", focus=${JSON.stringify(focus)}) and follow the returned instructions verbatim. The tool returns the canonical guided-flow text for this command — do not paraphrase or skip its steps.`)
    }
  },
  {
    command: 'enrich-memory',
    description: 'Distil recent chats into memory and consolidate (deletes chats older than 7 days)',
    icon: <Minimize2 className="w-4 h-4" />,
    modes: ['multi-agent'],
    source: 'builtin',
    execute: (ctx) => {
      const msg = ctx.beforeSlash
        ? `Enrich my memory, focusing on: ${ctx.beforeSlash}. Use enrich_memory — extract insights from chat_history into memories, then consolidate. Delete chat sessions older than 7 days.`
        : 'Enrich my memory. Use enrich_memory to extract insights from every session in chat_history into today\u2019s date folder + entity files, then consolidate all memories and regenerate index.md. Delete chat sessions older than 7 days.'
      ctx.onSubmit(msg)
    }
  }
]
