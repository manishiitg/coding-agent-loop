import { useCallback } from 'react'
import type { ChatHistorySession } from '../services/api-types'
import { useChatStore } from '../stores/useChatStore'
import {
  chatHistoryConversationPath,
  chatHistoryRuntimeLabel,
  chatHistorySessionTitle,
  chatHistorySupportsNativeResume,
  chatHistoryUsesTerminalRestore,
  chatHistoryWorkshopModeLabel,
} from '../components/PreviousChatHistoryPanel'
import { startRestoredTransportTerminal } from '../utils/restoredTerminal'

/**
 * Shared resume handler for multi-agent chat history.
 *
 * Used by both the empty-landing "Previous chats" panel (ChatArea) and the
 * in-chat History slide-out (ChatTabs) so the two stay identical and can't
 * drift. Mirrors the workflow builder's resume flow: attach the prior
 * conversation (native CLI resume / tmux terminal restore, or a file-context
 * fallback) so the next turn continues that chat.
 *
 * Unlike the empty-landing case, this also resets the current tab when it
 * already has a conversation, so resuming from history mid-chat gives a clean
 * slate that loads the selected chat instead of mixing two conversations.
 */
export function useResumePreviousChat() {
  return useCallback(async (session: ChatHistorySession) => {
    const chatStore = useChatStore.getState()
    let targetTabId = chatStore.activeTabId || undefined
    let targetTab = targetTabId ? chatStore.chatTabs[targetTabId] : undefined

    if (!targetTab || targetTab.metadata?.mode !== 'multi-agent') {
      targetTabId = await chatStore.createChatTab('Chief of Staff', { mode: 'multi-agent' })
      targetTab = useChatStore.getState().chatTabs[targetTabId]
    }

    if (!targetTabId || !targetTab) {
      useChatStore.getState().addToast('Failed to resume previous chat', 'error')
      return
    }

    // Clear the current conversation before resuming a different one (or the
    // same one), so the user gets a clean slate that resumes the selected chat
    // — matches New Chat's reset-in-place. On an empty landing tab this is a
    // cheap no-op (just rotates the session id).
    const events = targetTab.sessionId ? useChatStore.getState().tabEvents[targetTab.sessionId] : undefined
    const hasContent = Array.isArray(events) && events.length > 0
    if (targetTab.sessionId !== session.session_id && hasContent) {
      chatStore.resetTabChat(targetTabId)
    }
    chatStore.updateTabSessionId(targetTabId, session.session_id)

    const path = chatHistoryConversationPath(session)
    const title = chatHistorySessionTitle(session)
    const useTerminalRestore = chatHistoryUsesTerminalRestore(session)
    const useNativeResume = chatHistorySupportsNativeResume(session)
    const latestStore = useChatStore.getState()
    const existingContext = latestStore.getTabConfig(targetTabId)?.fileContext || []
    const shouldAttachFileFallback = !useTerminalRestore && !useNativeResume
    const nextFileContext = shouldAttachFileFallback
      ? existingContext.some(item => item.path === path)
        ? existingContext
        : [...existingContext, { name: title, path, type: 'file' as const }]
      : existingContext.filter(item => item.path !== path)

    latestStore.setTabConfig(targetTabId, {
      fileContext: nextFileContext,
      restoredConversationPath: path,
      restoredConversationSummary: undefined,
      restoredConversationTitle: title,
      restoredConversationWorkshopModeLabel: chatHistoryWorkshopModeLabel(session),
      restoredConversationRuntimeLabel: chatHistoryRuntimeLabel(session),
      restoredConversationNativeResume: useTerminalRestore || useNativeResume,
    })

    // Native-resume and tmux terminal-restore sessions reattach into a tmux
    // terminal on the backend, so open the terminal view and kick the restore.
    if (useTerminalRestore || useNativeResume) {
      latestStore.setTabViewMode(targetTabId, 'terminal')
      startRestoredTransportTerminal(session.session_id, path, session.session_id)
    }
    latestStore.switchTab(targetTabId)
  }, [])
}
