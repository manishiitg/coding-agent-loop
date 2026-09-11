import type { ActiveSessionInfo } from '../services/api-types'
import type { ChatTab } from '../stores/useChatStore'
import { isProductProjectSession } from './activitySessions'

function isProductConversation(sessionId?: string | null): boolean {
  if (!sessionId) return false
  // Current product registry IDs and the older project-owned session format.
  return sessionId.startsWith('product-') || isProductProjectSession({ session_id: sessionId })
}

export function isAgentWorksSwitcherTab(tab: Pick<ChatTab, 'metadata' | 'sessionId'>): boolean {
  const profile = tab.metadata?.agentProfileId?.trim()
  return (!profile || profile === 'agentworks') && !isProductConversation(tab.sessionId)
}

export function scopeQuickSwitcherToAgentWorks(
  tabs: Record<string, ChatTab>,
  sessions: ActiveSessionInfo[],
): { tabs: Record<string, ChatTab>; sessions: ActiveSessionInfo[] } {
  const excludedSessionIds = new Set(Object.values(tabs)
    .filter(tab => !isAgentWorksSwitcherTab(tab))
    .map(tab => tab.sessionId)
    .filter(Boolean))
  return {
    tabs: Object.fromEntries(Object.entries(tabs).filter(([, tab]) => isAgentWorksSwitcherTab(tab))),
    sessions: sessions.filter(session =>
      !isProductConversation(session.session_id) && !excludedSessionIds.has(session.session_id)),
  }
}
