import React from 'react'
import { FileText, Lightbulb, Download, Server, Cpu, Bot, Layers, Minimize2, RefreshCw, GitBranch, CheckCircle, Search, BookOpen, Activity, Cloud, Globe } from 'lucide-react'
import type { CommandContext, CommandDefinition } from './types'

function submitGuidedWorkflowCommand(
  ctx: CommandContext,
  kind: string,
  options: { runFolder?: string | null; background?: boolean } = {}
) {
  const focus = ctx.beforeSlash.trim()
  const args = [
    `kind=${JSON.stringify(kind)}`,
    `focus=${JSON.stringify(focus)}`,
  ]
  if (options.runFolder !== undefined) {
    args.push(`run_folder=${JSON.stringify(options.runFolder || '')}`)
  }
  const guidanceCall = `get_workflow_command_guidance(${args.join(', ')})`

  // Read-only reviews run as a background task so the chat stays responsive: the
  // background agent (same tools) does the heavy read→analyze→write builder/review.html
  // and auto-notifies on completion; the chat agent then surfaces the Top 3 for discussion.
  if (options.background) {
    const instruction =
      `Call ${guidanceCall} and follow the returned instructions verbatim — read the plan and artifacts and write your recommendations to builder/review.html. ` +
      `Treat focus as the request context before the slash command. The tool returns the canonical guided-flow text; do not paraphrase or skip its steps.`
    ctx.onSubmit(
      `Run the /${kind} review as a BACKGROUND task so this chat stays responsive. ` +
      `If the run_in_background tool is available: call run_in_background(name=${JSON.stringify(kind + ' review')}, instruction=${JSON.stringify(instruction)}) and do NOT perform the review yourself this turn — you'll get a completion notification, then summarize its Top 3 recommendations here for discussion. ` +
      `If run_in_background is not available, perform the review inline this turn instead.`
    )
    return
  }

  ctx.onSubmit(
    `Call ${guidanceCall} and follow the returned instructions verbatim. ` +
    `Treat focus as the conversation/request context that appeared before the slash command, including the user's recent constraints and intent. ` +
    `The tool returns the canonical guided-flow text for this command — do not paraphrase or skip its steps.`
  )
}

export const builtinCommands: CommandDefinition[] = [
  {
    command: 'design-plan',
    description: 'Review whether the plan follows design best practices',
    icon: <GitBranch className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'workshop',
    source: 'builtin',
    execute: (ctx) => {
      submitGuidedWorkflowCommand(ctx, 'design-plan', { background: true })
    }
  },
  {
    command: 'review-plan',
    description: 'Critically analyze the workflow plan and dependent artifacts',
    icon: <Search className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: ['workshop', 'run'],
    source: 'builtin',
    execute: (ctx) => {
      submitGuidedWorkflowCommand(ctx, 'review-plan', { background: true })
    }
  },
  {
    command: 'review-speed',
    description: 'Review workflow latency and how to make it faster',
    icon: <Minimize2 className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'workshop',
    source: 'builtin',
    execute: (ctx) => {
      const runFolder = ctx.getWorkflowStore().selectedRunFolder
      submitGuidedWorkflowCommand(ctx, 'review-speed', { runFolder, background: true })
    }
  },
  {
    command: 'review-cost',
    description: 'Review workflow cost and how to reduce it safely',
    icon: <Cpu className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'workshop',
    source: 'builtin',
    execute: (ctx) => {
      const runFolder = ctx.getWorkflowStore().selectedRunFolder
      submitGuidedWorkflowCommand(ctx, 'review-cost', { runFolder, background: true })
    }
  },
  {
    command: 'review-artifact-drift',
    description: 'Check whether artifacts drifted from recent plan changes',
    icon: <RefreshCw className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: ['workshop'],
    source: 'builtin',
    execute: (ctx) => {
      submitGuidedWorkflowCommand(ctx, 'review-artifact-drift', { background: true })
    }
  },
  {
    command: 'improve-knowledge',
    description: 'Improve knowledge notes with targeted cleanup or cross-step consolidation',
    icon: <Layers className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: ['workshop'],
    source: 'builtin',
    execute: (ctx) => {
      submitGuidedWorkflowCommand(ctx, 'improve-knowledge')
    }
  },
  {
    command: 'improve-learnings',
    description: 'Improve global learnings with targeted cleanup or current-plan consolidation',
    icon: <BookOpen className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: ['workshop'],
    source: 'builtin',
    execute: (ctx) => {
      submitGuidedWorkflowCommand(ctx, 'improve-learnings')
    }
  },
  {
    command: 'improve-database',
    description: 'Improve durable data contracts, schemas, and report compatibility',
    icon: <Server className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: ['workshop'],
    source: 'builtin',
    execute: (ctx) => {
      submitGuidedWorkflowCommand(ctx, 'improve-database')
    }
  },
  {
    command: 'design-reporting-ui',
    description: 'Design the reporting UI from scratch: pick HTML (live data) or Markdown documents and build them',
    icon: <CheckCircle className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: ['workshop'],
    source: 'builtin',
    execute: (ctx) => {
      submitGuidedWorkflowCommand(ctx, 'design-reporting-ui')
    }
  },
  {
    command: 'improve-report',
    description: 'Validate reports/report_plan.json and suggest layout/color improvements',
    icon: <CheckCircle className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: ['workshop'],
    source: 'builtin',
    execute: (ctx) => {
      submitGuidedWorkflowCommand(ctx, 'improve-report')
    }
  },
  {
    command: 'improve-evaluation',
    description: 'Validate evaluation/evaluation_plan.json and improve goal/criteria coverage',
    icon: <CheckCircle className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'workshop',
    source: 'builtin',
    execute: (ctx) => {
      const runFolder = ctx.getWorkflowStore().selectedRunFolder
      submitGuidedWorkflowCommand(ctx, 'improve-evaluation', { runFolder })
    }
  },
  {
    command: 'auto-improve',
    description: 'Set up recurring workflow runs and lightweight optimizer checks',
    icon: <Bot className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'workshop',
    source: 'builtin',
    execute: (ctx) => {
      submitGuidedWorkflowCommand(ctx, 'auto-improve')
    }
  },
  {
    command: 'improve-workflow',
    description: 'Use existing run evidence to review, replan if needed, harden, then optionally verify',
    icon: <RefreshCw className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'workshop',
    source: 'builtin',
    execute: (ctx) => {
      submitGuidedWorkflowCommand(ctx, 'improve-workflow')
    }
  },
  {
    command: 'review-code',
    description: 'Review saved scripts (main.py) against step descriptions to detect drift',
    icon: <FileText className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'workshop',
    source: 'builtin',
    execute: (ctx) => {
      submitGuidedWorkflowCommand(ctx, 'review-code', { background: true })
    }
  },
  {
    command: 'monitor',
    description: 'Post-run monitor: record Bug + Goal verdicts for the latest run into the workflow log',
    icon: <Activity className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'workshop',
    source: 'builtin',
    execute: (ctx) => {
      submitGuidedWorkflowCommand(ctx, 'monitor')
    }
  },
  {
    command: 'backup',
    description: 'Set up, run, or restore this workflow’s backup',
    icon: <Cloud className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'workshop',
    source: 'builtin',
    execute: (ctx) => {
      const instruction = `Help me set up or run backup for this workflow. Call get_reference_doc(kind="backup-strategy"), then read workflow.json.backup and backup/status.json.
- If backup is NOT configured yet: set it up — recommend the zero-config local-git default and ask me for any destination details or credentials you need. Write backup/status.json with state "configured_not_verified" and do not back up until I confirm.
- If backup IS configured: run a backup now and report the result (destinations, commit/ref).
- If I asked to restore: restore the tracked files from the latest backup (or a commit I name) instead.
Always write backup/status.json; never write operational status into workflow.json.`
      ctx.onSubmit(ctx.beforeSlash ? `${ctx.beforeSlash}\n\n${instruction}` : instruction)
    }
  },
  {
    command: 'publish',
    description: 'Set up or publish this workflow’s Pulse log & report to a public URL',
    icon: <Globe className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'workshop',
    source: 'builtin',
    execute: (ctx) => {
      const instruction = `Help me set up or run publish for this workflow. Call get_reference_doc(kind="publish-strategy") and follow it exactly, then read workflow.json.publish and publish/status.json.
- If publish is NOT configured: set it up — ask me which static host (Netlify / Vercel / Cloudflare Pages / GitHub Pages / S3 / any), confirm the CLI is installed and logged in, then write workflow.json.publish and publish/status.json with state "configured_not_verified". Do not publish yet.
- If publish IS configured: publish now. Publish BOTH artifacts — bake the report dashboard to static HTML AND publish the Pulse log (builder/improve.html); deploy dashboard.html + pulse.html + the nav index.html wrapper per the reference doc. If publish.targets only lists one, update it to include both first. Make every page theme-aware (inject the prefers-color-scheme shim). Then give me the public URL and confirm what's public.
Always write publish/status.json.`
      ctx.onSubmit(ctx.beforeSlash ? `${ctx.beforeSlash}\n\n${instruction}` : instruction)
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

For secrets, default to no global secrets: set \`workflow_json.capabilities.selected_global_secret_names\` to \`[]\` unless this specific workflow clearly needs named global secrets. Do not use \`null\` as a default because it means all global secrets.

## Step 3 — Extract the steps
Re-read the conversation and extract the concrete, repeatable steps the workflow should run. Each step must have:
- A stable kebab-case \`id\` (e.g. "fetch-data", "analyze-results"), unique within the plan
- A human \`title\`
- A detailed \`description\` of what the step does, in enough detail that a worker with no memory of this conversation could execute it
- A \`success_criteria\` line describing how to tell the step succeeded
- Optionally \`context_dependencies\` (file names produced by earlier steps) and \`context_output\` (file name this step produces)
- Most steps should use \`"type": "regular"\`. Use \`"message_sequence"\` only when the user asked for one persistent agent conversation with a queued sequence of short user messages. Use \`"routing"\` / \`"human_input"\` / \`"todo_task"\` only when the conversation genuinely called for branching, human-in-the-loop, or sub-workflow orchestration.

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
