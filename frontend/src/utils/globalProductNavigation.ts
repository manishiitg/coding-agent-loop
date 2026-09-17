import type { ActiveSessionInfo } from '../services/api-types'
import { useChatStore, type ChatTab } from '../stores/useChatStore'
import { useProductSurfaceStore } from '../stores/useProductSurfaceStore'
import { activateTab } from './activateTab'
import { openCanonicalActivitySession } from './workflowSessionRestore'

const normalizedPath = (value?: string | null): string => (value || '')
  .trim()
  .replace(/\\/g, '/')
  .replace(/^\/+|\/+$/g, '')

export function isWorkProductSession(session: Pick<ActiveSessionInfo, 'session_id' | 'workspace_path'>): boolean {
  if (session.session_id.startsWith('work:project:')) return true
  return /(?:^|\/)Chats\/Work\/projects\/[^/]+(?:\/|$)/i.test(normalizedPath(session.workspace_path))
}

export function workProjectIdForTab(tab?: Pick<ChatTab, 'metadata'> | null): string | null {
  const explicit = tab?.metadata?.agentProfileProjectId?.trim()
  if (explicit) return explicit
  const key = tab?.metadata?.agentProfileConversationKey?.trim()
  if (!key) return null
  return key.split(':')[0]?.trim() || null
}

export function workProjectIdForSession(
  session: Pick<ActiveSessionInfo, 'session_id' | 'preset_query_id'>,
  tab?: Pick<ChatTab, 'metadata'> | null,
): string | null {
  const fromTab = workProjectIdForTab(tab)
  if (fromTab) return fromTab
  const preset = session.preset_query_id?.trim()
  if (preset) return preset
  return session.session_id.startsWith('work:project:')
    ? session.session_id.slice('work:project:'.length).split(':')[0]?.trim() || null
    : null
}

export function openGlobalTab(tabId: string): boolean {
  const tab = useChatStore.getState().chatTabs[tabId]
  if (!tab) return false
  const surfaces = useProductSurfaceStore.getState()
  if (tab.metadata?.agentProfileId === 'work') {
    surfaces.setSelectedWorkProjectId(workProjectIdForTab(tab))
    surfaces.setProductSurface('work')
  } else {
    surfaces.setProductSurface('agentworks')
  }
  return activateTab(tabId)
}

export async function openGlobalActivitySession(
  session: ActiveSessionInfo,
  options: { title?: string; source?: string } = {},
): Promise<void> {
  const chatStore = useChatStore.getState()
  const tab = Object.values(chatStore.chatTabs).find(candidate => candidate.sessionId === session.session_id)
  if (tab?.metadata?.agentProfileId === 'work' || isWorkProductSession(session)) {
    const surfaces = useProductSurfaceStore.getState()
    surfaces.setSelectedWorkProjectId(workProjectIdForSession(session, tab))
    if (!tab) {
      surfaces.setPendingWorkView(session.bot_platform ? 'bots' : session.triggered_by ? 'schedules' : null)
    }
    surfaces.setProductSurface('work')
    if (tab) activateTab(tab.tabId)
    return
  }
  useProductSurfaceStore.getState().setProductSurface('agentworks')
  await openCanonicalActivitySession(session, options)
}
