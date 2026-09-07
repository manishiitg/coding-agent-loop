// The parent chat in platform mode: AgentWorks' own ChatArea, exactly as
// Video Studio hosts it, so SSE, restore, streaming, submission and tool
// rendering are the same code in every product and a fix lands everywhere.
// SparkQuill adds only what is its own: the suggestion pills under a reply,
// and the product events (presentations, family/activity/pin updates) that
// the workspace panel reacts to.
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import ChatArea, { type ChatContentRendererProps } from '../../../components/ChatArea'
import { TerminalEventTranscript } from '../../../components/TerminalEventTranscript'
import ToastHost from '../../../components/ui/ToastHost'
import { agentApi } from '../../../services/api'
import { useChatStore, waitForChatStoreHydration } from '../../../stores/useChatStore'
import { useModeStore } from '../../../stores/useModeStore'
import { useAppStore } from '../../../stores/useAppStore'
import { hydrateTabEvents, restoreSession } from '../../../utils/sessionRestore'
import { setProductCommands } from '../../../commands/registry'
import { usePresentationEvents } from '../../../platform/presentations/usePresentationEvents'
import { useProductInteractions } from '../../../platform/interactions/useProductInteractions'
import { useProductNotifications, type ProductNotification } from '../../../platform/notifications/useProductNotifications'
import { ProductSuggestions } from '../../../platform/chat/ProductSuggestions'
import type { QuickCommand } from '../stores/types'
import { api } from '../api'
import { toProductCommandDefinitions } from './productCommands'

export const PARENT_PROFILE_ID = 'sparkquill'
export const PARENT_PROFILE_VERSION = 1
export const FAMILY_WORKSPACE = 'Chats/SparkQuill'
const CHILD_PROFILE_ID = 'sparkquill-child'

/** The family's chosen learning helper and model (family.json `engine`/`model`), undefined before onboarding picks one. */
export async function familyRuntime(): Promise<{ engine?: string; model?: string }> {
  const state = await api.setup().catch(() => null)
  return { engine: state?.engine || undefined, model: state?.model || undefined }
}

/**
 * Keeps already-open SparkQuill tabs on the engine the parent just chose in
 * Settings, so the next turn uses it without a relaunch. Tabs are opened
 * once per page load and carry the engine in their metadata (ChatArea sends
 * it as `engine` on every profile query).
 */
export function applyFamilyEngineToOpenTabs(engine: string, model?: string): void {
  const store = useChatStore.getState()
  for (const tab of Object.values(store.chatTabs)) {
    const id = tab.metadata?.agentProfileId
    if (id === PARENT_PROFILE_ID || id === CHILD_PROFILE_ID) store.setTabMetadata(tab.tabId, { agentProfileEngine: engine, agentProfileModelID: model ?? '' })
  }
}

/**
 * Starts a fresh parent conversation: rotates the profile's conversation on
 * the server (the old one stays in history), closes the tab this page opened
 * for it, and forgets it so the next PlatformChat mount opens the new one.
 * The caller remounts PlatformChat (a changed `key`) after this resolves.
 */
export async function startNewParentConversation(): Promise<void> {
  const store = useChatStore.getState()
  await agentApi.startNewAgentProfileConversation(PARENT_PROFILE_ID, {})
  await forgetOpenParentTabs()
}

/**
 * Reopens an earlier parent conversation: the server makes it the live one
 * (the one it replaces stays in history), and the page's tab for the old
 * live conversation is closed so the next PlatformChat mount opens it. The
 * caller remounts PlatformChat (a changed `key`) after this resolves.
 */
export async function switchParentConversation(sessionId: string): Promise<void> {
  await agentApi.switchAgentProfileConversation(PARENT_PROFILE_ID, { session_id: sessionId })
  await forgetOpenParentTabs()
}

async function forgetOpenParentTabs(): Promise<void> {
  const store = useChatStore.getState()
  openedTab = null
  for (const tab of Object.values(store.chatTabs)) {
    if (tab.metadata?.agentProfileId === PARENT_PROFILE_ID) await store.closeTab(tab.tabId, true, false)
  }
}

export type ProductInteraction = { kind: string; payload: Record<string, unknown> }
export type ProductPresentation = { kind: string; payload: Record<string, unknown> }
type ProfileDeclaration = { tools?: { presentation?: { kind?: string }; interaction?: { kind?: string; render?: string } }[] }

type Props = {
  title: string
  childName: string
  theme: 'light' | 'dark'
  commands: QuickCommand[]
  landing: ReactNode
  onInteraction: (e: ProductInteraction) => void
  onPresentation: (p: ProductPresentation) => void
  /** Every notify_user message in the conversation so far (the app keeps them on screen until dismissed). */
  onNotifications?: (n: ProductNotification[]) => void
}

export const queryClient = new QueryClient()
// The one tab this page opened for the parent conversation (module-level so a
// remount finds it).
let openedTab: Promise<{ tabId: string; sessionId: string }> | null = null

// The tool binding that asked for in-chat suggestions (product.yaml
// `interaction: { kind, render: chat.suggestions }`), if any.
let suggestionsKind: string | null = null

export function SparkQuillConversation({ events, isStreaming, isRestoring, streamingText, streamingStatus, hasOlder, loadingOlder, historyError, onLoadOlder, landingContent, onSubmitQuery }: ChatContentRendererProps) {
  if (!isRestoring && events.length === 0 && !streamingText) return <>{landingContent}</>
  return (
    <div className="fl-platform-transcript">
      <TerminalEventTranscript
        events={events}
        terminal={null}
        loading={isRestoring}
        error={historyError}
        streamingText={streamingText}
        streamingStatus={streamingStatus}
        hasOlder={hasOlder}
        loadingOlder={loadingOlder}
        onLoadOlder={onLoadOlder}
        onRetry={onLoadOlder}
        surfaceClassName="fl-platform-surface"
        assistantLabel="Quill"
        assistantIcon={<img src="/sparkquill-mark.svg" alt="" width={16} height={16} />}
      />
      {isStreaming && !streamingText && (
        <div className="fl-thinking fl-platform-working"><img src="/sparkquill-loader.svg" alt="" width={30} height={30} /><span>{streamingStatus ? `Quill is: ${streamingStatus}…` : 'Working on it…'}</span></div>
      )}
      {onSubmitQuery && suggestionsKind && <ProductSuggestions events={events} kind={suggestionsKind} onSubmit={onSubmitQuery} hidden={isStreaming} />}
    </div>
  )
}

export default function PlatformChat({ title, childName, theme, commands, landing, onInteraction, onPresentation, onNotifications }: Props) {
  const [tabId, setTabId] = useState<string | null>(null)
  const [sessionId, setSessionId] = useState<string | null>(null)
  const [presentationKinds, setPresentationKinds] = useState<string[]>([])
  const [error, setError] = useState<string | null>(null)

  // The shared components style themselves by the html class, the way the
  // AgentWorks shell does; keep it in step with this app's theme.
  useEffect(() => {
    document.documentElement.classList.remove('light', 'dark')
    document.documentElement.classList.add(theme)
  }, [theme])

  useEffect(() => {
    setProductCommands(toProductCommandDefinitions(commands))
    return () => setProductCommands([])
  }, [commands])

  // Same sequence Video Studio runs to open its project conversation. It runs
  // once per page load: a remount (or React's dev double-invoke) reuses the
  // tab it already opened instead of re-hydrating it, which would throw away
  // the live turn in the store.
  useEffect(() => {
    let cancelled = false
    const prepare = async () => {
      if (openedTab) {
        const { tabId: existingTabId, sessionId: existingSessionId } = await openedTab
        if (cancelled) return
        useChatStore.getState().switchTab(existingTabId)
        setSessionId(existingSessionId)
        setTabId(existingTabId)
        return
      }
      openedTab = openTab()
      const opened = await openedTab
      if (cancelled) return
      setSessionId(opened.sessionId)
      setTabId(opened.tabId)
    }
    const openTab = async () => {
      // The shared service layer reads this app's login from local storage;
      // make sure it exists before the first request goes out.
      await api.ensureSession()
      useModeStore.getState().setModeCategory('multi-agent')
      useAppStore.getState().setAgentMode('multi-agent')
      await waitForChatStoreHydration()
      const chatStore = useChatStore.getState()
      const existing = Object.values(chatStore.chatTabs).find((tab) => tab.metadata?.agentProfileId === PARENT_PROFILE_ID)
      // What the product declares in product.yaml: which panels this
      // surface offers and which presentation kinds its tools emit.
      const profile = await agentApi.getAgentProfile(PARENT_PROFILE_ID).catch(() => null) as ProfileDeclaration | null
      const suggestionBinding = (profile?.tools ?? []).find((t) => t.interaction?.render === 'chat.suggestions')
      suggestionsKind = suggestionBinding ? (suggestionBinding.interaction?.kind || 'suggestions') : null
      setPresentationKinds((profile?.tools ?? []).map((t) => t.presentation?.kind).filter((k): k is string => typeof k === 'string' && k.length > 0))
      const [conversation, runtime] = await Promise.all([
        agentApi.resolveAgentProfileConversation(PARENT_PROFILE_ID, {}, existing?.sessionId ?? undefined),
        familyRuntime(),
      ])
      const createdTabId = await chatStore.createChatTab(title, {
        mode: 'multi-agent',
        agentProfileId: PARENT_PROFILE_ID,
        agentProfileVersion: PARENT_PROFILE_VERSION,
        agentProfileWorkspace: FAMILY_WORKSPACE,
        agentProfileChatContract: 'profile-v1',
        agentProfileEngine: runtime.engine,
        agentProfileModelID: runtime.model,
        agentProfileConversationKey: conversation.conversation_key,
        agentProfileConversationId: conversation.conversation_id,
      }, conversation.session_id)
      if (!chatStore.getTab(createdTabId)) throw new Error('the conversation tab could not be created')
      const restoredTabId = await restoreSession(conversation.session_id, { title, source: 'sparkquill-open', skipConfigRestore: true, workspacePath: FAMILY_WORKSPACE })
      await hydrateTabEvents(conversation.session_id, { workspacePath: FAMILY_WORKSPACE, fallbackToChatHistory: true, preferChatHistory: true })
      chatStore.switchTab(restoredTabId)
      return { tabId: restoredTabId, sessionId: conversation.session_id }
    }
    void prepare().catch((err) => {
      openedTab = null
      if (!cancelled) setError(err instanceof Error ? err.message : 'Could not open the conversation.')
    })
    return () => { cancelled = true }
  }, [title])

  // Product events ride the same stream ChatArea keeps, through the
  // platform's own selectors (the same ones Video Studio uses for its
  // presentations). Events already present when the tab opened are history,
  // not something to act on again.
  const interactions = useProductInteractions(sessionId ?? undefined)
  const notifications = useProductNotifications(sessionId ?? undefined)
  useEffect(() => { onNotifications?.(notifications) }, [notifications, onNotifications])
  const presentations = usePresentationEvents(sessionId ?? undefined, presentationKinds)
  // Both lists are in event order and only grow within a session, so what
  // is new is everything past the count already handed over.
  const handed = useRef<{ interactions: number; presentations: number } | null>(null)
  useEffect(() => {
    if (!sessionId) return
    if (!handed.current) { handed.current = { interactions: interactions.length, presentations: presentations.length }; return }
    for (const it of interactions.slice(handed.current.interactions)) onInteraction({ kind: it.kind, payload: it.payload })
    for (const p of presentations.slice(handed.current.presentations)) onPresentation({ kind: p.kind, payload: p.payload })
    handed.current = { interactions: interactions.length, presentations: presentations.length }
  }, [sessionId, interactions, presentations, onInteraction, onPresentation])

  return (
    <QueryClientProvider client={queryClient}>
      <div className="fl-platform-chat">
        {error ? (
          <p className="fl-note">Couldn’t open the conversation: {error}</p>
        ) : tabId ? (
          <ChatArea
            tabId={tabId}
            onNewChat={() => {}}
            landingContent={landing}
            contentRenderer={SparkQuillConversation}
            inputVariant="product"
            fullTurnStreaming
            hideRuntimeStatus
            composerPlaceholder={`Ask anything about ${childName || 'your child'}’s learning…`}
          />
        ) : (
          <p className="fl-note">Connecting to Quill…</p>
        )}
        <ToastHost />
      </div>
    </QueryClientProvider>
  )
}
