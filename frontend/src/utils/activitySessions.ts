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

const ACTIVE_SESSION_WORK_STATUSES = new Set([
  'running',
  'active',
  'in_progress',
  'paused',
  'waiting',
  'waiting_feedback',
])

export function hasActiveSessionWork(
  session?: Pick<ActiveSessionInfo, 'status' | 'needs_user_input' | 'has_running_background_agents' | 'running_background_agent_count'> | null,
): boolean {
  if (!session || isTerminalActivityStatus(session.status)) return false
  return ACTIVE_SESSION_WORK_STATUSES.has(normalizedActivityStatus(session.status)) ||
    session.needs_user_input === true ||
    hasLiveBackgroundAgents(session)
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
