import type { ChatTab } from '../../stores/useChatStore'
import { useChatStore } from '../../stores/useChatStore'

export type WorkRuntimeSelection = {
  engine: string
  provider?: string
  modelId: string
  reasoningEffort?: string
}

export type ProductEngineSelectionDetail = Partial<WorkRuntimeSelection> & {
  profileId?: string
  tabId?: string
}

export function belongsToWorkProject(tab: ChatTab, projectId: string): boolean {
  return Boolean(tab.metadata?.agentProfileId === 'work' && (
    tab.metadata.agentProfileProjectId === projectId ||
    tab.metadata.agentProfileConversationKey === projectId ||
    tab.metadata.agentProfileConversationKey?.startsWith(`${projectId}:`)
  ))
}

/** Mark every retained chat in a Work project for relaunch on its next turn. */
export function markWorkProjectRuntimeDirty(projectId: string): void {
  const store = useChatStore.getState()
  for (const tab of Object.values(store.chatTabs)) {
    if (!belongsToWorkProject(tab, projectId)) continue
    store.setTabMetadata(tab.tabId, { agentProfileRuntimeDirty: Boolean(tab.sessionId) })
  }
}

function workTabIdentity(tab: ChatTab): string {
  if (tab.metadata?.agentProfileBuilder === true) return 'builder'
  const conversationKey = tab.metadata?.agentProfileConversationKey?.trim()
  if (conversationKey) return `conversation:${conversationKey}`
  const sessionId = tab.sessionId?.trim()
  return sessionId ? `session:${sessionId}` : `tab:${tab.tabId}`
}

/**
 * Work has one permanent Builder plus one visible tab per durable
 * conversation. Prefer the active copy of a duplicated restored conversation;
 * otherwise keep the most recently used copy. This repairs persisted state
 * produced before the Builder reuse invariant was fixed without deleting any
 * backend conversation history.
 */
export function visibleWorkProjectTabs(
  tabs: Record<string, ChatTab>,
  projectId: string,
  activeTabId: string | null,
): ChatTab[] {
  const selected = new Map<string, ChatTab>()
  Object.values(tabs).filter(tab => belongsToWorkProject(tab, projectId)).forEach(tab => {
    const identity = workTabIdentity(tab)
    const current = selected.get(identity)
    if (!current || tab.tabId === activeTabId || (
      current.tabId !== activeTabId &&
      (tab.lastAccessedAt ?? tab.createdAt) > (current.lastAccessedAt ?? current.createdAt)
    )) {
      selected.set(identity, tab)
    }
  })

  return [...selected.values()].sort((a, b) => {
    const aBuilder = a.metadata?.agentProfileBuilder === true
    const bBuilder = b.metadata?.agentProfileBuilder === true
    if (aBuilder !== bBuilder) return aBuilder ? -1 : 1
    return a.createdAt - b.createdAt
  })
}

/**
 * Apply a Work project's runtime selection to its tabs. Same-provider model
 * changes can update retained conversations. A coding-agent provider change is
 * a native conversation boundary, so `newChatsOnly` updates only the permanent
 * Builder while existing chats retain their original CLI binding.
 */
export function setWorkProjectRuntimeSelection(
  projectId: string,
  _sourceTabId: string,
  selection: WorkRuntimeSelection,
  options?: { newChatsOnly?: boolean },
): void {
  const store = useChatStore.getState()
  for (const tab of Object.values(store.chatTabs)) {
    if (!belongsToWorkProject(tab, projectId)) continue
    if (options?.newChatsOnly && tab.metadata?.agentProfileBuilder !== true) continue
    store.setTabMetadata(tab.tabId, {
      agentProfileEngine: selection.engine,
      agentProfileModelID: selection.modelId,
      agentProfileReasoningEffort: selection.reasoningEffort,
      agentProfileRuntimeDirty: options?.newChatsOnly ? false : Boolean(tab.sessionId),
    })
  }
}

/**
 * Apply a model choice emitted by Work's scoped composer. The global active
 * tab can belong to another surface while Work is mounted, so the event must
 * retain the tab that actually originated the selection.
 */
export function applyWorkProjectRuntimeSelection(
  projectId: string,
  fallbackTabId: string | null,
  detail: ProductEngineSelectionDetail | undefined,
): boolean {
  if (detail?.profileId !== 'work' || !detail.engine || !detail.modelId) return false
  const sourceTabId = detail.tabId || fallbackTabId
  if (!sourceTabId) return false
  const sourceTab = useChatStore.getState().chatTabs[sourceTabId]
  if (!sourceTab || !belongsToWorkProject(sourceTab, projectId)) return false

  setWorkProjectRuntimeSelection(projectId, sourceTabId, {
    engine: detail.engine,
    modelId: detail.modelId,
    reasoningEffort: detail.reasoningEffort,
  })
  return true
}
