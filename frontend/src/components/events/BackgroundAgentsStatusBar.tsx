import React, { useMemo } from 'react'
import type { PollingEvent } from '../../services/api-types'

interface BackgroundAgent {
  agentId: string
  name: string
  status: 'running' | 'completed' | 'failed' | 'canceled'
  startTime: number
  duration?: string
}

interface BackgroundAgentsStatusBarProps {
  events: PollingEvent[]
}

/**
 * A persistent status bar showing background agents and their status.
 * Shows only when there are background agents in the session.
 * Each agent is shown as a small pill with name + status icon + elapsed time.
 */
export const BackgroundAgentsStatusBar: React.FC<BackgroundAgentsStatusBarProps> = ({ events }) => {
  const agents = useMemo(() => {
    const agentMap = new Map<string, BackgroundAgent>()

    for (const event of events) {
      if (event.type === 'background_agent_started') {
        const data = event.data as {
          data?: { agent_id?: string; name?: string; fields?: { agent_id?: string; name?: string } }
          agent_id?: string
          name?: string
        }
        const fields = data?.data?.fields || data?.data || data
        const agentId = fields?.agent_id
        const name = fields?.name || 'Agent'
        if (agentId) {
          agentMap.set(agentId, {
            agentId,
            name,
            status: 'running',
            startTime: event.timestamp ? new Date(event.timestamp as string).getTime() : Date.now(),
          })
        }
      } else if (event.type === 'background_agent_completed') {
        const data = event.data as {
          data?: { agent_id?: string; name?: string; status?: string; duration?: string; fields?: { agent_id?: string; name?: string; status?: string; duration?: string } }
          agent_id?: string
          name?: string
          status?: string
          duration?: string
        }
        const fields = data?.data?.fields || data?.data || data
        const agentId = fields?.agent_id
        if (agentId && agentMap.has(agentId)) {
          const existing = agentMap.get(agentId)!
          existing.status = (fields?.status === 'failed' ? 'failed' : 'completed') as 'completed' | 'failed'
          existing.duration = fields?.duration
        }
      } else if (event.type === 'background_agent_terminated') {
        const data = event.data as {
          data?: { agent_id?: string; fields?: { agent_id?: string } }
          agent_id?: string
        }
        const fields = data?.data?.fields || data?.data || data
        const agentId = fields?.agent_id
        if (agentId && agentMap.has(agentId)) {
          agentMap.get(agentId)!.status = 'canceled'
        }
      }
    }

    return Array.from(agentMap.values())
  }, [events])

  if (agents.length === 0) return null

  return (
    <div className="flex flex-wrap gap-1.5 px-3 py-1.5 bg-gray-50 dark:bg-gray-900/40 border-b border-gray-200 dark:border-gray-700/50">
      {agents.map((agent) => (
        <AgentChip key={agent.agentId} agent={agent} />
      ))}
    </div>
  )
}

const AgentChip: React.FC<{ agent: BackgroundAgent }> = ({ agent }) => {
  const statusConfig = {
    running: {
      dot: 'bg-blue-500 animate-pulse',
      border: 'border-blue-300 dark:border-blue-600',
      bg: 'bg-blue-50 dark:bg-blue-900/20',
      text: 'text-blue-700 dark:text-blue-300',
    },
    completed: {
      dot: 'bg-green-500',
      border: 'border-green-300 dark:border-green-600',
      bg: 'bg-green-50 dark:bg-green-900/20',
      text: 'text-green-700 dark:text-green-300',
    },
    failed: {
      dot: 'bg-red-500',
      border: 'border-red-300 dark:border-red-600',
      bg: 'bg-red-50 dark:bg-red-900/20',
      text: 'text-red-700 dark:text-red-300',
    },
    canceled: {
      dot: 'bg-gray-400',
      border: 'border-gray-300 dark:border-gray-600',
      bg: 'bg-gray-50 dark:bg-gray-800/20',
      text: 'text-gray-500 dark:text-gray-400',
    },
  }

  const config = statusConfig[agent.status]

  return (
    <div className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full border text-xs ${config.border} ${config.bg} ${config.text}`}>
      <span className={`inline-block w-1.5 h-1.5 rounded-full ${config.dot}`} />
      <span className="font-medium truncate max-w-[120px]">{agent.name}</span>
      {agent.status === 'running' && <ElapsedTime startTime={agent.startTime} />}
      {agent.duration && agent.status !== 'running' && (
        <span className="opacity-75">{agent.duration}</span>
      )}
    </div>
  )
}

const ElapsedTime: React.FC<{ startTime: number }> = ({ startTime }) => {
  const [elapsed, setElapsed] = React.useState('')

  React.useEffect(() => {
    const update = () => {
      const seconds = Math.floor((Date.now() - startTime) / 1000)
      if (seconds < 60) {
        setElapsed(`${seconds}s`)
      } else {
        const mins = Math.floor(seconds / 60)
        const secs = seconds % 60
        setElapsed(`${mins}m ${secs}s`)
      }
    }
    update()
    const interval = setInterval(update, 1000)
    return () => clearInterval(interval)
  }, [startTime])

  return <span className="opacity-75 tabular-nums">{elapsed}</span>
}
