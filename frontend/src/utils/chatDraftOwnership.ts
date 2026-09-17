import { useChatStore, type ChatTabConfig } from '../stores/useChatStore'
import { captureChatIdentity, isChatIdentityCurrent } from './chatIdentity'

export function captureChatDraft(tabId: string | null | undefined) {
  const tab = tabId ? useChatStore.getState().getTab(tabId) : undefined
  return tab ? {
    tabId: tab.tabId,
    sessionId: tab.sessionId,
    revision: tab.config?.composerRevision ?? 0,
    identity: captureChatIdentity(),
  } : undefined
}

// A callback from an unmounted composer may finish after another instance has
// edited this tab. Compare the shared draft, not the old instance's refs.
export function updateOwnedChatDraft(
  captured: ReturnType<typeof captureChatDraft>,
  patch: Partial<ChatTabConfig>,
): boolean {
  if (!captured || !isChatIdentityCurrent(captured.identity)) return false
  const store = useChatStore.getState()
  const tab = store.getTab(captured.tabId)
  if (!tab || tab.sessionId !== captured.sessionId ||
    (tab.config?.composerRevision ?? 0) !== captured.revision) return false
  store.setTabConfig(captured.tabId, patch)
  return true
}
