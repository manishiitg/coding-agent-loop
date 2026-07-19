import type { ActiveSessionInfo } from '../services/api-types'
import { runtimeHasBackgroundAgents, sessionRuntimeStatus } from './runtimeActivity'
import { isScheduledSession } from './workflowSessionKinds'

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
  session: Pick<ActiveSessionInfo, 'status' | 'runtime_state' | 'display_status' | 'has_running_background_agents' | 'running_background_agent_count'>,
): boolean {
  if (session.runtime_state) return runtimeHasBackgroundAgents(session)
  if (isTerminalActivityStatus(session.status)) return false
  return runtimeHasBackgroundAgents(session)
}

const ACTIVE_SESSION_WORK_STATUSES = new Set([
  'running',
  'active',
  'in_progress',
  'paused',
  'waiting',
  'waiting_feedback',
])

export function hasActiveSessionWork(
  session?: Pick<ActiveSessionInfo, 'status' | 'runtime_state' | 'display_status' | 'needs_user_input' | 'has_running_background_agents' | 'running_background_agent_count'> | null,
): boolean {
  if (!session || sessionRuntimeStatus(session) === 'stopped') return false
  if (session.runtime_state) return sessionRuntimeStatus(session) === 'busy' || session.runtime_state.waiting_for_user || runtimeHasBackgroundAgents(session)
  return ACTIVE_SESSION_WORK_STATUSES.has(normalizedActivityStatus(session.status)) ||
    session.needs_user_input === true ||
    hasLiveBackgroundAgents(session)
}

/**
 * Title for a non-workflow activity item. Scheduled Chief of Staff sessions
 * must never expose their scheduler envelope or task prompt as a UI label.
 * New backend sessions provide `title`; the generic fallback covers sessions
 * created by an older backend and restored history that predates that field.
 */
export function nonWorkflowActivityTitle(
  session: Pick<ActiveSessionInfo, 'session_id' | 'triggered_by' | 'current_execution_name' | 'title' | 'query'>,
): string {
  const explicitTitle = session.current_execution_name?.trim() || session.title?.trim()
  if (explicitTitle) return explicitTitle
  if (isScheduledSession({ sessionId: session.session_id, triggeredBy: session.triggered_by })) {
    return 'Chief of Staff task'
  }
  return session.query?.trim() || 'Agent chat'
}

// A main-agent coding CLI keeps its tmux pane alive after a turn finishes so the
// user can send a follow-up without relaunching. The backend flips such an idle,
// non-steerable session to status "completed" (so chat streaming state clears and
// the next message starts a fresh turn), which would otherwise drop it from the
// activity monitor. But the agent is still ALIVE and waiting — it should stay
// visible, distinctly from an actively-processing one (clock vs spinner). Bounded
// to a window after the last activity so a truly-forgotten pane eventually clears;
// matches the 30-min abandonment window the backend uses for background agents.
export const RETAINED_TMUX_ACTIVE_WINDOW_MS = 30 * 60 * 1000
export function hasIdleAliveCodingAgent(
  session: Pick<ActiveSessionInfo, 'has_retained_tmux_session' | 'last_activity'>,
  now: number = Date.now(),
): boolean {
  if (session.has_retained_tmux_session !== true) return false
  const last = session.last_activity ? Date.parse(session.last_activity) : NaN
  // Unknown/unparseable timestamp: the live pane is the primary signal — show it
  // (the backend reaper still bounds how long the pane itself stays alive).
  if (Number.isNaN(last)) return true
  return now - last < RETAINED_TMUX_ACTIVE_WINDOW_MS
}
