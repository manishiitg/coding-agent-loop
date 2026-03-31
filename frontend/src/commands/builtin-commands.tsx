import React from 'react'
import { FileText, Lightbulb, Download, Server, Cpu, History, GitBranch, Bot, Layers, Minimize2, Search, Sparkles, CheckCircle2, Wrench } from 'lucide-react'
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
    command: 'infer-objective',
    description: 'Infer the workflow objective only if it is truly missing',
    icon: <FileText className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'builder',
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      const focusText = focus ? ` Focus especially on: ${focus}.` : ''
      ctx.onSubmit(`Check planning/plan.json first. If the root objective is truly missing, run infer_objective and summarize the proposed objective and draft success criteria for confirmation. If objective already exists, explain that infer_objective is not needed.${focusText}`)
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
