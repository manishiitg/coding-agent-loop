import type { ActiveSessionInfo, RuntimeSnapshot, SessionExecutionTreeResponse } from '../services/api-types'

export type RuntimeDisplayStatus = 'busy' | 'idle' | 'stopped'

export function runtimeDisplayStatus(runtime?: RuntimeSnapshot | null): RuntimeDisplayStatus | undefined {
  switch (runtime?.phase) {
    case 'starting':
    case 'running':
      return 'busy'
    case 'completed':
    case 'failed':
    case 'canceled':
      return 'stopped'
    case 'waiting':
    case 'idle':
      return 'idle'
    default:
      return undefined
  }
}

export function sessionRuntimeStatus(session?: Pick<ActiveSessionInfo, 'runtime_state' | 'display_status' | 'status'> | null): RuntimeDisplayStatus {
  if (!session) return 'idle'
  return runtimeDisplayStatus(session.runtime_state) || session.display_status || legacyDisplayStatus(session.status)
}

export function executionTreeRuntimeStatus(tree?: SessionExecutionTreeResponse | null): RuntimeDisplayStatus | undefined {
  return runtimeDisplayStatus(tree?.runtime_state) || tree?.summary.display_status
}

export function runtimeHasBackgroundAgents(session?: Pick<ActiveSessionInfo, 'runtime_state' | 'has_running_background_agents' | 'running_background_agent_count'> | null): boolean {
  if (!session) return false
  if (session.runtime_state) {
    return runtimeDisplayStatus(session.runtime_state) !== 'stopped' && session.runtime_state.background_live
  }
  return session.has_running_background_agents === true || (session.running_background_agent_count ?? 0) > 0
}

export function runtimeNeedsUserInput(session?: Pick<ActiveSessionInfo, 'runtime_state' | 'needs_user_input'> | null): boolean {
  if (!session) return false
  return session.runtime_state?.waiting_for_user ?? session.needs_user_input === true
}

export function runtimeCanSteer(session?: Pick<ActiveSessionInfo, 'runtime_state' | 'can_steer'> | null): boolean {
  if (!session) return false
  return session.runtime_state?.foreground_turn.can_steer ?? session.can_steer === true
}

function legacyDisplayStatus(status?: string): RuntimeDisplayStatus {
  switch ((status || '').trim().toLowerCase()) {
    case 'running':
    case 'active':
    case 'in_progress':
    case 'paused':
      return 'busy'
    case 'completed':
    case 'stopped':
    case 'error':
    case 'failed':
    case 'cancelled':
    case 'canceled':
      return 'stopped'
    default:
      return 'idle'
  }
}
