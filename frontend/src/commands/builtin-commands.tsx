import React from 'react'
import { FileText, Lightbulb, Download, Server, Cpu, History, GitBranch, Bot, Layers, Minimize2 } from 'lucide-react'
import type { CommandDefinition } from './types'

export const builtinCommands: CommandDefinition[] = [
  {
    command: 'summarize',
    description: 'Summarize conversation history',
    icon: <FileText className="w-4 h-4" />,
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
    source: 'builtin',
    execute: (ctx) => {
      ctx.openDialog('skillImport')
    }
  },
  {
    command: 'mcp',
    description: 'View MCP server details and tools',
    icon: <Server className="w-4 h-4" />,
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
    source: 'builtin',
    execute: (ctx) => {
      ctx.openDialog('resume')
    }
  },
  {
    command: 'spawn',
    description: 'Enable simple sub-agent delegation (fire-and-forget)',
    icon: <GitBranch className="w-4 h-4" />,
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
