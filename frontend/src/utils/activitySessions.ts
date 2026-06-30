import type { ActiveSessionInfo } from '../services/api-types'

export function normalizedActivityStatus(status?: string): string {
  return (status || '').toLowerCase().trim()
}

export function isTerminalActivityStatus(status?: string): boolean {
  const normalized = normalizedActivityStatus(status)
  return normalized === 'completed' ||
    normalized === 'stopped' ||
    normalized === 'error' ||
    normalized === 'failed' ||
    normalized === 'cancelled' ||
    normalized === 'canceled'
}

export function hasLiveBackgroundAgents(
  session: Pick<ActiveSessionInfo, 'status' | 'has_running_background_agents' | 'running_background_agent_count'>,
): boolean {
  if (isTerminalActivityStatus(session.status)) return false
  return session.has_running_background_agents === true ||
    (session.running_background_agent_count ?? 0) > 0
}
