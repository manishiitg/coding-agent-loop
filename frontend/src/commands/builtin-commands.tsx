import React from 'react'
import { Activity } from 'lucide-react'
import type { CommandDefinition } from './types'

// /pulse is the only remaining hardcoded builtin: it is an async frontend API
// call (scheduler runPulse + toasts), which no static product.yaml prompt can
// express. Every other builder slash command ships in
// agentworksproduct/product.yaml and is registered as a product command by the
// workflow surface (see agentworksProductCommands.tsx).
export const builtinCommands: CommandDefinition[] = [
  {
    command: 'pulse',
    description: 'Run one complete Pulse now against the latest retained run',
    icon: <Activity className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'workshop',
    source: 'builtin',
    execute: async (ctx) => {
      const workspacePath = ctx.workflowWorkspacePath?.trim()
      if (!workspacePath) {
        ctx.addToast('Open a workflow before running Pulse', 'error')
        return
      }
      try {
        // Keep the scheduler/API graph out of the eager slash-command registry;
        // it depends on workspace stores that also import command metadata.
        const { schedulerApi } = await import('../api/scheduler')
        await schedulerApi.runPulse(workspacePath)
        ctx.addToast('Pulse started', 'success')
      } catch (error) {
        const responseData = (error as { response?: { data?: unknown } })?.response?.data
        const detail = typeof responseData === 'string'
          ? responseData
          : error instanceof Error
            ? error.message
            : 'Unable to start Pulse'
        ctx.addToast(detail.trim() || 'Unable to start Pulse', 'error')
      }
    }
  },
]
