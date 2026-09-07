// A read-only look at a conversation the parent doesn't drive directly —
// the isolated check-in (product.yaml schedules[].isolated) or an activity's
// conversation with the child — opened by a known session id rather than
// resolved like the parent's own chat (the resolve endpoint refuses a key on
// a singleton profile; an activity's keyed conversation belongs to the child
// profile, not this one). Uses AgentWorks' own restore/hydrate/render path
// (the same one PlatformChat uses for the parent's own chat), read-only.
import { useEffect, useState } from 'react'
import { QueryClientProvider } from '@tanstack/react-query'
import ChatArea from '../../../components/ChatArea'
import { useChatStore, waitForChatStoreHydration } from '../../../stores/useChatStore'
import { useModeStore } from '../../../stores/useModeStore'
import { useAppStore } from '../../../stores/useAppStore'
import { hydrateTabEvents, restoreSession } from '../../../utils/sessionRestore'
import { api } from '../api'
import { FAMILY_WORKSPACE, PARENT_PROFILE_ID, PARENT_PROFILE_VERSION, SparkQuillConversation, queryClient } from './PlatformChat'

type Props = {
  sessionId: string
  title?: string
  /** The conversation's own profile and workspace — an activity's conversation is the CHILD profile's, in its own activity folder, not the parent's. */
  agentProfileId?: string
  agentProfileVersion?: number
  workspacePath?: string
  onClose: () => void
}

export default function PulseHistoryViewer({ sessionId, title = 'Check-in history', agentProfileId = PARENT_PROFILE_ID, agentProfileVersion = PARENT_PROFILE_VERSION, workspacePath = FAMILY_WORKSPACE, onClose }: Props) {
  const [tabId, setTabId] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    let openedTabId: string | null = null
    const open = async () => {
      await api.ensureSession()
      useModeStore.getState().setModeCategory('multi-agent')
      useAppStore.getState().setAgentMode('multi-agent')
      await waitForChatStoreHydration()
      const chatStore = useChatStore.getState()
      const createdTabId = await chatStore.createChatTab(title, {
        mode: 'multi-agent',
        agentProfileId,
        agentProfileVersion,
        agentProfileWorkspace: workspacePath,
        agentProfileChatContract: 'profile-v1',
      }, sessionId)
      if (!chatStore.getTab(createdTabId)) throw new Error('the conversation tab could not be created')
      const restoredTabId = await restoreSession(sessionId, { title, source: 'sparkquill-history-view', skipConfigRestore: true, workspacePath })
      await hydrateTabEvents(sessionId, { workspacePath, fallbackToChatHistory: true, preferChatHistory: true })
      if (cancelled) { await chatStore.closeTab(restoredTabId, true, false); return }
      openedTabId = restoredTabId
      chatStore.switchTab(restoredTabId)
      setTabId(restoredTabId)
    }
    void open().catch((err) => {
      if (!cancelled) setError(err instanceof Error ? err.message : 'Could not open this conversation.')
    })
    return () => {
      cancelled = true
      if (openedTabId) void useChatStore.getState().closeTab(openedTabId, true, false)
    }
  }, [sessionId, title, agentProfileId, agentProfileVersion, workspacePath])

  return (
    <div className="fl-pulse-history-overlay" role="dialog" aria-label={title}>
      <div className="fl-pulse-history-panel">
        <div className="fl-pulse-history-head">
          <span>{title}</span>
          <button type="button" className="fl-pulse-popover-close" onClick={onClose} aria-label="Close">×</button>
        </div>
        <div className="fl-pulse-history-body">
          {error ? (
            <p className="fl-note">Couldn't open this conversation: {error}</p>
          ) : tabId ? (
            <QueryClientProvider client={queryClient}>
              <ChatArea
                tabId={tabId}
                onNewChat={() => {}}
                contentRenderer={SparkQuillConversation}
                inputVariant="product"
                fullTurnStreaming
                hideRuntimeStatus
                hideInput
              />
            </QueryClientProvider>
          ) : (
            <p className="fl-note">Loading…</p>
          )}
        </div>
      </div>
    </div>
  )
}
