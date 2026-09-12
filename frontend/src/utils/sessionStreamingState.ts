import type { RuntimeSnapshot } from '../services/api-types'
import { runtimeDisplayStatus, runtimeHasBackgroundAgents } from './runtimeActivity'

export type SessionActivitySnapshot = {
  status?: string
  session_status?: string
  display_status?: 'busy' | 'idle' | 'stopped'
  runtime_state?: RuntimeSnapshot
  has_running_background_agents?: boolean
  running_background_agent_count?: number
  is_synthetic_turn?: boolean
  can_steer?: boolean
}

// Active-session recovery, SSE, and event polling must distinguish foreground
// generation from background activity and an idle, retained CLI in the same way.
export function sessionStreamingState(session: SessionActivitySnapshot) {
  const runtime = session.runtime_state
  let status = (session.session_status ?? session.status ?? '').toLowerCase()
  if (runtime) {
    switch (runtime.phase) {
      case 'starting': case 'running': case 'waiting': status = 'running'; break
      case 'completed': status = 'completed'; break
      case 'failed': status = 'error'; break
      case 'canceled': status = 'stopped'; break
      case 'idle': status = 'inactive'; break
    }
  } else if (session.display_status === 'idle' || session.display_status === 'stopped') {
    // Older servers may publish the consolidated display status without a snapshot.
    if (status === 'running' || status === 'paused') status = 'inactive'
  }
  const hasRunningBgAgents = runtimeHasBackgroundAgents(session)
  const isSyntheticTurn = runtime?.foreground_turn.synthetic ?? session.is_synthetic_turn ?? false
  const canSteer = runtime?.foreground_turn.can_steer ?? session.can_steer ?? false
  const foregroundLive = runtime
    ? runtime.phase === 'starting' || runtime.foreground_turn.busy || runtime.foreground_turn.has_cancel || canSteer || (runtime.terminal_busy && !hasRunningBgAgents)
    : status === 'running' && (!hasRunningBgAgents || canSteer)
  const isStreaming = status === 'running' && !isSyntheticTurn && foregroundLive
  const isCompleted = (status === 'completed' || status === 'error') && !hasRunningBgAgents
  const isActive = runtime ? runtimeDisplayStatus(runtime) === 'busy' || runtime.phase === 'waiting'
    : status === 'running' || status === 'paused' || hasRunningBgAgents
  return { status, isStreaming, hasRunningBgAgents, isSyntheticTurn, canSteer, isCompleted, isActive }
}
