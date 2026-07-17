import type { ActiveSessionInfo, SessionStatusResponse } from '../services/api-types'
import { isScheduledSession } from './workflowSessionKinds'

export type NewChatConfirmationTabState = {
  tabId: string
  sessionId?: string | null
  isStreaming?: boolean
  hasRunningBgAgents?: boolean
  isSyntheticTurn?: boolean
  metadata?: {
    mode?: 'workflow' | 'multi-agent'
    isOrganizationAssistant?: boolean
    isScheduledRun?: boolean
  }
}

const RESET_RISK_SESSION_STATUSES = new Set([
  'active',
  'busy',
  'in_progress',
  'paused',
  'running',
  'waiting',
  'waiting_for_input',
])

const normalizeStatus = (status: unknown): string =>
  typeof status === 'string' ? status.trim().toLowerCase() : ''

export function shouldConfirmForActiveSession(session?: ActiveSessionInfo | null): boolean {
  if (!session) return false
  if (session.has_retained_tmux_session || session.has_running_background_agents || session.needs_user_input) {
    return true
  }
  return RESET_RISK_SESSION_STATUSES.has(normalizeStatus(session.status))
}

export function shouldConfirmForSessionStatus(status?: SessionStatusResponse | null): boolean {
  if (!status) return false
  if (status.has_retained_tmux_session) return true
  return RESET_RISK_SESSION_STATUSES.has(normalizeStatus(status.status))
}

export function findBlockingMultiAgentSession(
  sessions?: ActiveSessionInfo[] | null,
  preferredSessionId?: string | null,
): ActiveSessionInfo | null {
  const blockingSessions = (sessions || []).filter(session =>
    normalizeStatus(session.agent_mode) === 'multi-agent' &&
    !isScheduledSession({
      sessionId: session.session_id,
      triggeredBy: session.triggered_by,
      botPlatform: session.bot_platform,
    }) &&
    shouldConfirmForActiveSession(session)
  )

  if (blockingSessions.length === 0) {
    return null
  }

  if (preferredSessionId) {
    return blockingSessions.find(session => session.session_id === preferredSessionId) || blockingSessions[0]
  }

  return blockingSessions[0]
}

export function shouldConfirmNewMultiAgentChat(
  tab?: NewChatConfirmationTabState | null,
  activeSession?: ActiveSessionInfo | null,
): boolean {
  if (!tab) return false
  if (tab.metadata?.mode !== 'multi-agent') return false
  if (tab.metadata?.isScheduledRun) return false

  if (tab.isStreaming || tab.hasRunningBgAgents || tab.isSyntheticTurn) {
    return true
  }

  if (!tab.sessionId || !activeSession || activeSession.session_id !== tab.sessionId) {
    return false
  }

  return shouldConfirmForActiveSession(activeSession)
}
