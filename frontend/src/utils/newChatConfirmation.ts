import type { ActiveSessionInfo, SessionStatusResponse } from '../services/api-types'

export type NewChatConfirmationTabState = {
  tabId: string
  sessionId?: string | null
  isStreaming?: boolean
  hasRunningBgAgents?: boolean
  isSyntheticTurn?: boolean
  canSteer?: boolean
  metadata?: {
    mode?: 'workflow' | 'multi-agent'
    isOrganizationAssistant?: boolean
  }
}

const INTERRUPTIBLE_SESSION_STATUSES = new Set([
  'active',
  'in_progress',
  'paused',
  'running',
  'waiting',
  'waiting_for_input',
])

const normalizeStatus = (status: unknown): string =>
  typeof status === 'string' ? status.trim().toLowerCase() : ''

export function isInterruptibleActiveSession(session?: ActiveSessionInfo | null): boolean {
  if (!session) return false
  if (session.has_running_background_agents || session.needs_user_input) return true
  return INTERRUPTIBLE_SESSION_STATUSES.has(normalizeStatus(session.status))
}

export function isInterruptibleSessionStatus(status?: SessionStatusResponse | null): boolean {
  if (!status) return false
  return status.can_steer === true || INTERRUPTIBLE_SESSION_STATUSES.has(normalizeStatus(status.status))
}

export function shouldConfirmNewMultiAgentChat(
  tab?: NewChatConfirmationTabState | null,
  activeSession?: ActiveSessionInfo | null,
): boolean {
  if (!tab) return false
  if (tab.metadata?.mode !== 'multi-agent') return false
  if (tab.metadata?.isOrganizationAssistant === true) return false

  if (tab.isStreaming || tab.hasRunningBgAgents || tab.isSyntheticTurn || tab.canSteer) {
    return true
  }

  if (!tab.sessionId || !activeSession || activeSession.session_id !== tab.sessionId) {
    return false
  }

  return isInterruptibleActiveSession(activeSession)
}
