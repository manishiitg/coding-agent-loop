import type { ChatHistorySession } from '../services/api-types'
import type { ChatTab } from '../stores/useChatStore'

export function isRestorableWorkflowChatSession(session: ChatHistorySession): boolean {
  const sessionId = (session.session_id || '').toLowerCase()
  if (!sessionId) return false
  if (sessionId.startsWith('schedule-') || sessionId.startsWith('sched_') || sessionId.startsWith('bot-')) {
    return false
  }

  const mode = (session.agent_mode || '').toLowerCase()
  if (mode && mode !== 'workflow' && mode !== 'workflow_phase') {
    return false
  }

  return (session.message_count ?? 0) > 0 || (session.preview_messages?.length ?? 0) > 0 || !!session.query?.trim()
}

export function findLatestRestorableWorkflowChatSession(
  sessions: ChatHistorySession[] | undefined,
): ChatHistorySession | undefined {
  return (sessions || []).find(session =>
    session.can_resume !== false && isRestorableWorkflowChatSession(session),
  )
}

export type LatestWorkflowChatAction =
  | { action: 'keep-active' }
  | { action: 'activate-tab'; tabId: string }
  | { action: 'restore-latest'; session: ChatHistorySession; sessionId: string }

// Resolves which tab a workflow surface should show after reconnect. The
// persisted active tab wins only when it already shows the newest restorable
// chat. Otherwise the newest chat takes focus so a reload always lands on
// the latest conversation instead of a stale tab.
export function resolveLatestWorkflowChatAction(options: {
  sessions: ChatHistorySession[] | undefined
  tabs: Record<string, ChatTab>
  activeTabId: string | null
}): LatestWorkflowChatAction {
  const { sessions, tabs, activeTabId } = options
  const latest = findLatestRestorableWorkflowChatSession(sessions)
  if (!latest?.session_id) return { action: 'keep-active' }
  const activeTab = activeTabId ? tabs[activeTabId] : undefined
  if (activeTab?.sessionId === latest.session_id) return { action: 'keep-active' }
  // Only ever switch between chats of this workspace. When the active tab
  // shows a run, a schedule, a bot, a fresh tab, or a foreign session, its
  // session is absent from this list and today's preserve behavior stands.
  if (!activeTab?.sessionId) return { action: 'keep-active' }
  const activeListed = (sessions || []).some(session => session.session_id === activeTab.sessionId)
  if (!activeListed) return { action: 'keep-active' }
  const existing = Object.values(tabs).find(tab =>
    tab.metadata?.mode === 'workflow' && tab.sessionId === latest.session_id,
  )
  if (existing) return { action: 'activate-tab', tabId: existing.tabId }
  return { action: 'restore-latest', session: latest, sessionId: latest.session_id }
}
