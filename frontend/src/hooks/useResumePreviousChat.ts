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
 * Shared resume handler for product-owned chat history.
 *
 * Product surfaces create and own their profile-bound tab before rendering
 * ChatArea. This handler only attaches history to that existing tab; it must
 * never manufacture the removed profile-less AgentWorks Chat. It mirrors the
 * workflow builder's resume flow: attach the prior
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

    if (!targetTabId || targetTab?.metadata?.mode !== 'multi-agent' || !targetTab.metadata.agentProfileId) {
      useChatStore.getState().addToast('Open the product chat before resuming its history.', 'error')
      return
    }

    // Work's permanent Workshop tab is a history/new-chat launch surface. A
    // resume must open the selected durable conversation in its own chat tab;
    // attaching it to Workshop makes old messages appear under the launcher,
    // while the next submission still follows launcher semantics and silently
    // creates a different conversation.
    if (targetTab.metadata.agentProfileBuilder === true) {
      const existing = Object.values(chatStore.chatTabs).find(tab =>
        tab.metadata?.agentProfileBuilder !== true &&
        tab.metadata?.agentProfileId === targetTab?.metadata?.agentProfileId &&
        tab.metadata?.agentProfileProjectId === targetTab?.metadata?.agentProfileProjectId &&
        tab.sessionId === session.session_id,
      )
      if (existing) {
        targetTabId = existing.tabId
        targetTab = existing
      } else {
        const builderConfig = targetTab.config
        const projectId = targetTab.metadata.agentProfileProjectId
        const resumedTabId = await chatStore.createChatTab(chatHistorySessionTitle(session), {
          ...targetTab.metadata,
          agentProfileBuilder: false,
          agentProfileConversationKey: projectId ? `${projectId}:${session.session_id}` : session.session_id,
          agentProfileConversationId: undefined,
        }, session.session_id)
        if (builderConfig) chatStore.setTabConfig(resumedTabId, { ...builderConfig })
        targetTabId = resumedTabId
        targetTab = chatStore.chatTabs[resumedTabId]
      }
    }

    // Clear the current conversation before resuming a different one (or the
    // same one), so the user gets a clean slate that resumes the selected chat
    // — matches New Chat's reset-in-place. On an empty landing tab this is a
    // cheap no-op (just rotates the session id).
    const events = targetTab.sessionId ? useChatStore.getState().tabEvents[targetTab.sessionId] : undefined
    const hasContent = Array.isArray(events) && events.length > 0
    if (targetTab?.sessionId !== session.session_id && hasContent) {
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

    // Reattach the transport for continuation, but reopen the saved structured
    // conversation by default. Raw remains available as an explicit terminal
    // diagnostic view.
    if (useTerminalRestore || useNativeResume) {
      latestStore.setTabViewMode(targetTabId, 'tree')
      startRestoredTransportTerminal(
        session.session_id,
        path,
        session.session_id,
        session.workspace_path || session.runtime?.workspace_path,
      )
    }
    latestStore.switchTab(targetTabId)
  }, [])
}
