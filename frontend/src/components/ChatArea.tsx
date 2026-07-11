import { useEffect, useRef, useCallback, forwardRef, useImperativeHandle, useMemo, useState, type ForwardedRef } from 'react'
import { useRenderLogger, useMemoLogger } from '../utils/renderLogger'
import { isInternalAutoNotificationEvent } from '../utils/internalChatEvents'
import { useShallow } from 'zustand/react/shallow'
import { agentApi, resetSessionId, getSessionId } from '../services/api'
import type { PollingEvent, ExtendedLLMConfiguration, SSEEventMessage, SSEStatusMessage, ExecutionOptions } from '../services/api-types'
import type { AgentMode } from '../stores/types'
import { ChatInput } from './ChatInput'
import { EventDisplay } from './EventDisplay'
import { TerminalCenter } from './TerminalCenter'
import { WorkflowModeHandler, type WorkflowModeHandlerRef, signalPlanModified } from './workflow'
import { ToastContainer } from './ui/Toast'
import { useWorkspaceStore } from '../stores/useWorkspaceStore'
import { useWorkflowStore } from '../stores/useWorkflowStore'
import { useAppStore, useLLMStore, useMCPStore, useChatStore, useGlobalPresetStore } from '../stores'
import { useModeStore, type ModeCategory } from '../stores/useModeStore'
import { PreviousChatHistoryPanel } from './PreviousChatHistoryPanel'
import { resolveChatSurface } from './resolveChatSurface'
import { PresetSelectionOverlay } from './PresetSelectionOverlay'
import { ModeSwitchDialog } from './ui/ModeSwitchDialog'
import { normalizeEventViewMode, type ChatTab } from '../stores/useChatStore'
import type { CustomPreset } from '../types/preset'
import { restoreSession } from '../utils/sessionRestore'
import { logger } from '../utils/logger'
import { summarizeEventForDebug } from '../utils/eventOrdering'
import { secretsApi } from '../api/secrets'
import { useSecretsStore } from '../stores'
import { useSessionExecutionTree } from '../hooks/useSessionExecutionTree'
import { useSessionTerminals } from '../hooks/useSessionTerminals'
import { useResumePreviousChat } from '../hooks/useResumePreviousChat'
import { requestTerminalRefreshBurst } from '../utils/terminalRefresh'
import { isLiveWorkflowTerminal } from '../utils/workflowTerminalActivity'
import { startRestoredTransportTerminal } from '../utils/restoredTerminal'
import {
  determineModeFlag,
  buildLLMConfigWithApiKeys,
  buildQueryRequestPayload,
  resolveOrCreateTab,
  createUserMessageEvent,
  validateExecutionGroups,
  isChatCompatiblePhase,
} from '../utils/chatSubmitHelpers'

// Stable empty array to avoid infinite re-render loops in Zustand selectors
// (a new [] on every selector call breaks referential equality checks)
const EMPTY_EVENTS: PollingEvent[] = []
const AUTO_NOTIFICATION_PREFIX = '[AUTO-NOTIFICATION]'
const RESTORED_CONVERSATION_CONTEXT_MARKER = '\n\nPrevious workflow-builder conversation file:'
const STALE_STREAMING_RECOVERY_GRACE_MS = 10000
// Grace window after a resume marker appears (page-load auto-restore sleeps ~500ms
// before isRestoringChatSessions flips true; a freshly-resumed chat streams its
// first turn shortly after). The previous-chats list stays hidden during this
// window so a NON-empty resumed chat doesn't flash the list before its first
// event/terminal arrives; once it elapses still-empty, the list is revealed.
//
// CRITICAL: this MUST outlast the useSessionTerminals presence probe so the list
// never flashes mid-resume. That probe (src/hooks/useSessionTerminals.ts) polls
// every 3000ms and the restored static snapshot only lands after the async
// restore-terminal POST, so the terminal is typically detected on the 2nd–3rd
// poll (~3–6s). A 2500ms settle expired BEFORE the probe's second poll → the
// resolver fell to 'landing' and the user had to click Resume 2–3 times. 10000ms
// gives the probe ~4 polls to confirm the terminal (→ 'active') before the settle
// can ever fall through to 'landing' (only a genuinely dead/empty resume does).
const RESUME_SETTLE_MS = 10000
const STREAMING_EVENT_TYPES = new Set(['streaming_start', 'streaming_chunk', 'streaming_end'])

type RuntimeEventScope = {
  kind: 'session' | 'delegation' | 'workshop'
  id?: string
}

function isStreamingEventType(type: unknown): type is string {
  return typeof type === 'string' && STREAMING_EVENT_TYPES.has(type)
}

const AUTO_NOTIFICATION_MAX_AGE_MS = 5 * 60 * 1000

function getEventTimestampMs(event: PollingEvent): number | null {
  if (!event.timestamp) return null
  const parsed = Date.parse(event.timestamp)
  return Number.isFinite(parsed) ? parsed : null
}

function formatAutoNotificationTime(event: PollingEvent): string {
  const ts = getEventTimestampMs(event)
  return new Date(ts ?? Date.now()).toLocaleTimeString([], {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  })
}

function getOwnedTerminalStreamKeys(
  sessionId: string,
  event: PollingEvent,
  innerData: Record<string, unknown> | undefined,
  agentEvent: Record<string, unknown> | undefined,
  correlationId: unknown,
  metadata?: Record<string, unknown>,
): string[] {
  const eventRecord = event as unknown as Record<string, unknown>
  const normalizeOwnerId = (value: unknown): string | null => {
    if (typeof value !== 'string') return null
    const trimmed = value.trim()
    if (!trimmed || trimmed.startsWith('main:')) return null
    for (const prefix of ['delegation:', 'workflow:', 'background:', 'agent:', 'batch:']) {
      if (trimmed.startsWith(prefix)) {
        const unprefixed = trimmed.slice(prefix.length).trim()
        return unprefixed && !unprefixed.startsWith('main:') ? unprefixed : null
      }
    }
    return trimmed
  }

  const candidates: unknown[] = [
    eventRecord.execution_id,
    metadata?.execution_id,
    innerData?.execution_id,
    agentEvent?.execution_id,
    metadata?.owner_execution_id,
    metadata?.execution_owner_id,
    innerData?.delegation_id,
    agentEvent?.delegation_id,
    metadata?.delegation_id,
    innerData?.background_agent_id,
    agentEvent?.background_agent_id,
    metadata?.background_agent_id,
    innerData?.agent_id,
    agentEvent?.agent_id,
    metadata?.agent_id,
    innerData?.agent_name,
    agentEvent?.agent_name,
    metadata?.orchestrator_agent_name,
    typeof correlationId === 'string' && (correlationId.startsWith('delegation-') || correlationId.startsWith('workshop-'))
      ? correlationId
      : undefined,
    metadata?.workshop_step_id,
    metadata?.current_step_id,
    metadata?.orchestrator_step_id,
    metadata?.workflow_step_id,
    metadata?.step_id,
  ]
  for (const candidate of candidates) {
    const ownerId = normalizeOwnerId(candidate)
    if (ownerId) return [`${sessionId}:${ownerId}`]
  }
  return []
}

function isForegroundSessionEvent(
  event: PollingEvent,
  component: unknown,
  correlationId: unknown,
): boolean {
  const componentText = typeof component === 'string' ? component : ''
  const correlationText = typeof correlationId === 'string' ? correlationId : ''
  if (
    componentText.startsWith('delegation-') ||
    componentText.startsWith('workshop-') ||
    correlationText.startsWith('delegation-') ||
    correlationText.startsWith('workshop-')
  ) {
    return false
  }

  const kind = (event.execution_kind || '').trim().toLowerCase()
  if (kind && kind !== 'main_agent') {
    return false
  }

  const executionId = (event.execution_id || '').trim().toLowerCase()
  if (!executionId || executionId.startsWith('main:')) {
    return true
  }

  return false
}

function isStaleAutoNotificationEvent(event: PollingEvent): boolean {
  const ts = getEventTimestampMs(event)
  return ts !== null && Date.now() - ts > AUTO_NOTIFICATION_MAX_AGE_MS
}

function getUserMessageContent(event: PollingEvent): string {
  const agentEvent = event.data as Record<string, unknown> | undefined
  const innerData = agentEvent?.data as Record<string, unknown> | undefined
  const content = innerData?.content ?? agentEvent?.content
  return typeof content === 'string' ? content : ''
}

function getDisplaySafeUserMessageContent(content: string): string {
  const markerIndex = content.indexOf(RESTORED_CONVERSATION_CONTEXT_MARKER)
  return (markerIndex >= 0 ? content.slice(0, markerIndex) : content).trim()
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === 'object' ? value as Record<string, unknown> : undefined
}

function firstString(...values: unknown[]): string | undefined {
  for (const value of values) {
    if (typeof value === 'string' && value.trim()) return value.trim()
  }
  return undefined
}

function isRootLikeExecutionId(value?: string): boolean {
  return !value || value.startsWith('main:') || value.startsWith('session:')
}

function getEventPayloadParts(event: PollingEvent) {
  const eventRecord = event as unknown as Record<string, unknown>
  const agentEvent = asRecord(event.data)
  const innerData = asRecord(agentEvent?.data)
  const metadata = asRecord(innerData?.metadata) || asRecord(agentEvent?.metadata)
  return { eventRecord, agentEvent, innerData, metadata }
}

function getRuntimeEventScope(event: PollingEvent): RuntimeEventScope {
  const { eventRecord, agentEvent, innerData, metadata } = getEventPayloadParts(event)
  const component = firstString(eventRecord.component, innerData?.component, agentEvent?.component)
  const correlationId = firstString(
    eventRecord.correlation_id,
    innerData?.correlation_id,
    agentEvent?.correlation_id,
    metadata?.correlation_id
  )
  const delegationId = firstString(innerData?.delegation_id, agentEvent?.delegation_id, metadata?.delegation_id)
  const workshopStepId = firstString(metadata?.workshop_step_id, innerData?.workshop_step_id, agentEvent?.workshop_step_id)
  const executionId = firstString(eventRecord.execution_id)
  const parentExecutionId = firstString(
    eventRecord.parent_execution_id,
    metadata?.parent_execution_id,
    innerData?.parent_execution_id,
    agentEvent?.parent_execution_id
  )
  const backgroundAgentId = firstString(
    innerData?.background_agent_id,
    agentEvent?.background_agent_id,
    innerData?.agent_id,
    agentEvent?.agent_id
  )
  const executionKind = firstString(eventRecord.execution_kind)

  if (component?.startsWith('delegation-')) return { kind: 'delegation', id: component }
  if (delegationId?.startsWith('delegation-')) return { kind: 'delegation', id: delegationId }
  if (correlationId?.startsWith('delegation-')) return { kind: 'delegation', id: correlationId }
  if ((executionKind === 'workflow_step' || executionId?.startsWith('workflow-step:')) && !isRootLikeExecutionId(executionId)) {
    return { kind: 'workshop', id: executionId }
  }
  if (!isRootLikeExecutionId(parentExecutionId)) return { kind: 'workshop', id: parentExecutionId }
  if (!isRootLikeExecutionId(backgroundAgentId)) return { kind: 'workshop', id: backgroundAgentId }
  if (correlationId?.startsWith('workshop-')) return { kind: 'workshop', id: correlationId }
  if (workshopStepId?.startsWith('workshop-')) return { kind: 'workshop', id: workshopStepId }

  return { kind: 'session' }
}

function handleLiveStreamingEvent(
  event: PollingEvent,
  actualSessionId: string,
  chatStore: ReturnType<typeof useChatStore.getState>
) {
  const { agentEvent, innerData, metadata } = getEventPayloadParts(event)
  const scope = getRuntimeEventScope(event)
  const correlationId = innerData?.correlation_id ?? agentEvent?.correlation_id
  const isTerminalStreaming = metadata?.kind === 'terminal'
  const ownedTerminalKeys = isTerminalStreaming
    ? getOwnedTerminalStreamKeys(actualSessionId, event, innerData, agentEvent, correlationId, metadata)
    : []

  if (event.type === 'streaming_start') {
    if (ownedTerminalKeys.length > 0) {
      for (const ownedTerminalKey of ownedTerminalKeys) {
        chatStore.clearOwnedStreamingTerminal(ownedTerminalKey)
      }
    } else if (scope.kind === 'delegation' && scope.id) {
      chatStore.clearDelegationStreamingText(scope.id)
    } else if (scope.kind === 'session') {
      chatStore.clearStreamingText(actualSessionId)
    } else if (scope.kind === 'workshop' && scope.id) {
      chatStore.clearExecutionStreamingText(scope.id)
    }
    return
  }

  if (event.type === 'streaming_chunk') {
    const rawContent = innerData?.content ?? agentEvent?.content
    const content = typeof rawContent === 'string' ? rawContent : ''
    if (!content) return

    const rawIndex = innerData?.chunk_index ?? agentEvent?.chunk_index
    const chunkIndex = typeof rawIndex === 'number' ? rawIndex : -1
    if (ownedTerminalKeys.length > 0) {
      for (const ownedTerminalKey of ownedTerminalKeys) {
        chatStore.setOwnedStreamingTerminalSnapshot(ownedTerminalKey, chunkIndex, content)
      }
    } else if (scope.kind === 'delegation' && scope.id) {
      if (chunkIndex === 0 || chunkIndex === 1) chatStore.clearDelegationStreamingText(scope.id)
      chatStore.appendDelegationStreamingChunk(scope.id, chunkIndex, content)
    } else if (scope.kind === 'workshop' && scope.id) {
      if (chunkIndex === 0 || chunkIndex === 1) chatStore.clearExecutionStreamingText(scope.id)
      chatStore.appendExecutionStreamingChunk(actualSessionId, scope.id, chunkIndex, content)
    } else if (scope.kind === 'session') {
      if (chunkIndex === 0 || chunkIndex === 1) chatStore.clearStreamingText(actualSessionId)
      chatStore.appendStreamingChunk(actualSessionId, chunkIndex, content)
    }
    return
  }

  if (event.type === 'streaming_end' && ownedTerminalKeys.length > 0) {
    for (const ownedTerminalKey of ownedTerminalKeys) {
      chatStore.setOwnedStreamingTerminalActive(ownedTerminalKey, false)
    }
  } else if (event.type === 'streaming_end' && scope.kind === 'session') {
    chatStore.clearStreamingStatus(actualSessionId)
    const sidForClear = actualSessionId
    const textSnapshot = useChatStore.getState().streamingText[sidForClear]
    setTimeout(() => {
      const currentText = useChatStore.getState().streamingText[sidForClear]
      const match = currentText === textSnapshot
      if (currentText && match) {
        useChatStore.getState().clearStreamingText(sidForClear)
      }
    }, 500)
  } else if (event.type === 'streaming_end' && scope.kind === 'workshop' && scope.id) {
    chatStore.clearExecutionStreamingStatus(scope.id)
    const executionIdForClear = scope.id
    const textSnapshot = useChatStore.getState().executionStreaming[executionIdForClear]?.text
    setTimeout(() => {
      const current = useChatStore.getState().executionStreaming[executionIdForClear]
      if (current && current.text === textSnapshot) {
        useChatStore.getState().clearExecutionStreamingText(executionIdForClear)
      }
    }, 500)
  }
}

function withDisplaySafeUserMessage(event: PollingEvent): PollingEvent {
  if (event.type !== 'user_message') return event

  const content = getUserMessageContent(event)
  const safeContent = getDisplaySafeUserMessageContent(content)
  if (!content || safeContent === content) return event

  const agentEvent = event.data as Record<string, unknown> | undefined
  const innerData = agentEvent?.data as Record<string, unknown> | undefined
  if (innerData) {
    return {
      ...event,
      data: {
        ...agentEvent,
        data: {
          ...innerData,
          content: safeContent,
        },
      } as PollingEvent['data'],
    }
  }

  return {
    ...event,
    data: {
      ...agentEvent,
      content: safeContent,
    } as PollingEvent['data'],
  }
}

function getQueuedAutoNotificationTimestampMs(message: string): number | null {
  const match = message.match(/\[(\d{2}):(\d{2}):(\d{2})\]/)
  if (!match) return null

  const now = new Date()
  const parsed = new Date(now)
  parsed.setHours(Number(match[1]), Number(match[2]), Number(match[3]), 0)

  // Handle notifications carried across midnight.
  if (parsed.getTime() - now.getTime() > 60 * 1000) {
    parsed.setDate(parsed.getDate() - 1)
  }

  return parsed.getTime()
}

function isStaleQueuedAutoNotification(message: string): boolean {
  const ts = getQueuedAutoNotificationTimestampMs(message)
  return ts !== null && Date.now() - ts > AUTO_NOTIFICATION_MAX_AGE_MS
}

interface ChatAreaProps {
  // New chat handler
  onNewChat: () => void
  // Hide header when used inside another layout (like WorkflowLayout)
  hideHeader?: boolean
  // Hide input area when used inside workflow mode
  hideInput?: boolean
  // Compact mode for smaller font sizes (used in workflow layout)
  compact?: boolean
  // Suppress terminal content while the parent renders an idle/history state.
  suppressTerminalPane?: boolean
  // Tab ID - if provided, use this tab's session ID (works for both chat and workflow modes).
  // Pass null explicitly to disable all active behavior (SSE, polling, queue) — used when
  // this ChatArea instance is hidden behind another instance for the same tab.
  tabId?: string | null
  // Multi-agent landing previous-chats panel: icon-only (compact) tabs when the
  // chat sits in the narrow rail; labeled tabs when it fills the pane (org panel
  // minimized). The panel always renders in fill mode so it scrolls in both.
  previousChatsCompact?: boolean
  // Workflow landing previous-chats panel. WorkflowLayout owns the panel + its
  // resume handler (so the workflow-scoped history logic isn't duplicated here)
  // and passes the rendered node only when a fresh automation chat should show
  // the list. When present, ChatArea renders it as the primary surface (mirroring
  // the multi-agent landing panel) and suppresses its own workflow empty states.
  workflowPreviousChatsPanel?: React.ReactNode
}

// Ref interface for ChatArea component
export interface ChatAreaRef {
  handleNewChat: () => void
  resetChatState: () => void
  refreshWorkflowPresets: () => Promise<void>
  submitQuery: (query: string, executionOptions?: ExecutionOptions) => Promise<void>
  getEvents: () => PollingEvent[]
  isStreaming: boolean
  currentWorkflowPhase: string
}


// Global flag to ensure auto-restore only happens once per page load
let globalHasRestored = false

// Inner component for chat area
const ChatAreaInner = forwardRef((props: ChatAreaProps, ref: ForwardedRef<ChatAreaRef>) => {
  const { onNewChat, hideHeader = false, hideInput = false, compact = false, suppressTerminalPane = false, tabId, previousChatsCompact = false, workflowPreviousChatsPanel } = props
  // null means "inactive — don't subscribe to any tab or run any effects"
  const isInactive = tabId === null

  // Store subscriptions
  const {
    agentMode,
    setCurrentQuery,
  } = useAppStore(useShallow(state => ({
    agentMode: state.agentMode,
    setCurrentQuery: state.setCurrentQuery,
  })))
  
  const { selectedModeCategory, getAgentModeFromCategory } = useModeStore(useShallow(state => ({
    selectedModeCategory: state.selectedModeCategory,
    getAgentModeFromCategory: state.getAgentModeFromCategory
  })))
  const { getActivePreset, applyPreset, clearActivePreset, currentPresetServers } = useGlobalPresetStore(useShallow(state => ({
    getActivePreset: state.getActivePreset,
    applyPreset: state.applyPreset,
    clearActivePreset: state.clearActivePreset,
    currentPresetServers: state.currentPresetServers
  })))
  
  // Derive correct agent mode from selectedModeCategory (source of truth)
  const correctAgentMode = useMemo(() => {
    if (selectedModeCategory) {
      return getAgentModeFromCategory(selectedModeCategory) as AgentMode
    }
    return agentMode // Fallback to agentMode if selectedModeCategory is null
  }, [selectedModeCategory, agentMode, getAgentModeFromCategory])
  
  // LLM provider configs are read via useLLMStore.getState() in helpers
  
  const {
    toolList: allTools,
    selectedServers,
  } = useMCPStore(useShallow(state => ({
    toolList: state.toolList,
    selectedServers: state.selectedServers,
  })))

  // All servers that are currently connected (status=ok)
  const connectedServers = useMemo<Set<string>>(
    () => new Set(allTools
      .filter(t => t.status === 'ok')
      .map(t => t.server)
      .filter((server): server is string => typeof server === 'string' && server.length > 0)),
    [allTools]
  )
  
  // Get active tab reactively (works for both chat and workflow modes)
  // Use selector to ensure reactivity when tab config changes
  const activeTabIdFromStore = useChatStore(state => state.activeTabId)
  // null = explicitly inactive (no tab); undefined = use store's active tab
  const targetTabId = isInactive ? null : (tabId || activeTabIdFromStore)
  const activeTab = useChatStore(state => 
    targetTabId ? state.chatTabs[targetTabId] : undefined
  )
  const activeEventViewMode = normalizeEventViewMode(activeTab?.viewMode)
  
  // PERF FIX: Stable tab-session key to avoid phantom re-renders.
  //
  // PROBLEM: Previously `const chatTabs = useChatStore(state => state.chatTabs)` subscribed
  // to the full chatTabs object. Every `setTabStreaming`, `setTabCompleted`, `setTabConfig`
  // call creates a new `chatTabs` reference (Zustand immutable update), causing ChatArea
  // to re-render even when no tab/session was added or removed. This caused 10-20 phantom
  // renders between actual data changes (visible as "no dep change" in render logs).
  //
  // FIX: Derive a stable string key from tab IDs + session IDs + modes. This key only
  // changes when tabs are created/deleted or sessions are assigned — NOT when tab properties
  // tabsWithSessions, tabsWithActiveSessions) recompute only when this key changes.
  const tabSessionKey = useChatStore(state => {
    const tabs = state.chatTabs
    const parts: string[] = []
    for (const id of Object.keys(tabs)) {
      const t = tabs[id]
      parts.push(`${id}:${t.sessionId || ''}:${t.metadata?.mode || ''}`)
    }
    return parts.sort().join(',')
  })

  // Determine which servers to use based on mode category
  // CRITICAL: Workflow preset servers should ONLY be used in workflow mode, never leak into multi-agent mode
  const effectiveServers = useMemo<string[]>(() => {
    // For workflow mode, use preset servers
    if (selectedModeCategory === 'workflow') {
      const workflowServers = currentPresetServers.length > 0 ? currentPresetServers : selectedServers
      return workflowServers.filter((server): server is string => typeof server === 'string')
    }
    // For multi-agent mode, ALWAYS use tab's selected servers from config (if available), otherwise fall back to global
    // NEVER use currentPresetServers in multi-agent mode - workflow preset state is isolated to workflow mode only
    const isChatLike = selectedModeCategory === 'multi-agent'
    const tabSelectedServers: string[] = ((isChatLike && activeTab?.config)
      ? activeTab.config.selectedServers 
      : selectedServers).filter((server): server is string => typeof server === 'string')
    
    // If no servers are selected (empty array), default to all connected servers
    if (tabSelectedServers.length === 0) {
      const all = Array.from(connectedServers)
      return all.length > 0 ? all : ["NO_SERVERS"]
    }
    // Filter out servers that aren't currently connected (status=ok).
    // Stale servers from localStorage could block queries if sent to backend.
    const filtered = tabSelectedServers.filter((s): s is string => s === "NO_SERVERS" || connectedServers.has(s))
    return filtered
  }, [
    selectedModeCategory,
    currentPresetServers,
    selectedServers,
    connectedServers,
    activeTab?.config
  ])
  
  // Filter tools to only include those from effective servers
  // If "NO_SERVERS" is selected, return empty tools (pure LLM mode)
  const enabledTools = useMemo(() => {
    if (effectiveServers.includes("NO_SERVERS")) {
      return []
    }

    return allTools.filter(tool =>
      tool.server && effectiveServers.includes(tool.server)
    )
  }, [allTools, effectiveServers])
  
  // PERF FIX: Derive tab lists from stable tabSessionKey instead of raw chatTabs reference.
  // Uses getState() for the actual tab objects (avoids subscription), and tabSessionKey
  // as the recomputation trigger (only changes on tab add/remove/session change).
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const allTabs = useMemo(() => Object.values(useChatStore.getState().chatTabs), [tabSessionKey])
  const tabsWithSessions = useMemo(() => allTabs.filter(tab => tab.sessionId), [allTabs])
  
  // No observer ID syncing needed - sessions are used directly

  const {
    // Chat state
    isStreaming,
    setIsStreaming,
    lastEventIndex,
    setLastEventIndex,
    pollingInterval,
    // Deprecated: totalEvents, setTotalEvents, setLastEventCount, events, setEvents removed
    getTabEvents,
    addTabEvents,
    setTabEvents,
    getTabLastEventIndex,
    setTabLastEventIndex,
    setHasActiveChat,
    autoScroll,
    setAutoScroll,
    finalResponse,
    setIsCompleted,
    isLoadingHistory,
    setIsLoadingHistory,
    // eslint-disable-next-line @typescript-eslint/no-unused-vars
    setIsApprovingWorkflow: _setIsApprovingWorkflow,
    sessionState,
    setSessionState,
    isCheckingActiveSessions,
    setIsCheckingActiveSessions,
    currentWorkflowPhase,
    setCurrentWorkflowPhase,
    setCurrentWorkflowQueryId,
    toasts,
    addToast,
    removeToast,
    resetChatState,
    isAtBottom,
    switchTab,
    setTabSyntheticTurn,
    setTabCanSteer,
  } = useChatStore(useShallow(state => ({
    isStreaming: state.isStreaming,
    setIsStreaming: state.setIsStreaming,
    lastEventIndex: state.lastEventIndex,
    setLastEventIndex: state.setLastEventIndex,
    pollingInterval: state.pollingInterval,
    getTabEvents: state.getTabEvents,
    addTabEvents: state.addTabEvents,
    setTabEvents: state.setTabEvents,
    getTabLastEventIndex: state.getTabLastEventIndex,
    setTabLastEventIndex: state.setTabLastEventIndex,
    setHasActiveChat: state.setHasActiveChat,
    autoScroll: state.autoScroll,
    setAutoScroll: state.setAutoScroll,
    finalResponse: state.finalResponse,
    setIsCompleted: state.setIsCompleted,
    isLoadingHistory: state.isLoadingHistory,
    setIsLoadingHistory: state.setIsLoadingHistory,
    setIsApprovingWorkflow: state.setIsApprovingWorkflow,
    sessionState: state.sessionState,
    setSessionState: state.setSessionState,
    isCheckingActiveSessions: state.isCheckingActiveSessions,
    setIsCheckingActiveSessions: state.setIsCheckingActiveSessions,
    currentWorkflowPhase: state.currentWorkflowPhase,
    setCurrentWorkflowPhase: state.setCurrentWorkflowPhase,
    setCurrentWorkflowQueryId: state.setCurrentWorkflowQueryId,
    toasts: state.toasts,
    addToast: state.addToast,
    removeToast: state.removeToast,
    resetChatState: state.resetChatState,
    isAtBottom: state.isAtBottom,
    switchTab: state.switchTab,
    setTabSyntheticTurn: state.setTabSyntheticTurn,
    setTabCanSteer: state.setTabCanSteer,
  })))

  // Session-specific selector: only re-renders when the ACTIVE session's events change
  // (not when any other session gets events)
  const activeSessionId = activeTab?.sessionId
  const tabEvents = useChatStore((state) =>
    activeSessionId ? state.tabEvents[activeSessionId] || EMPTY_EVENTS : EMPTY_EVENTS
  )

  // Get active preset for workflow mode
  const activeWorkflowPreset = getActivePreset('workflow')
  const selectedWorkflowPreset = activeWorkflowPreset?.id || null
  
  // Always use tab events - never fall back to global events to prevent cross-tab mixing
  // If there are no tabs, return empty array (tabs should always exist in multi-tab mode)
  // Filter out workspace_file_operation events from display
  // (These events are still sent to frontend for workspace store processing, but hidden from chat UI)
  //
  // PERF FIX: Return a ref-stable array when the filtered output hasn't changed.
  // Events are append-only with unique IDs, so comparing length + first/last ID
  // is sufficient. This prevents downstream cascade: EventHierarchy → eventTree →
  // flattenedItems → Virtuoso diff — all skip when the ref is the same.
  // Holds the last returned displayEvents array. Used to avoid creating a new array
  // reference when the filtered output is identical — which would otherwise cascade
  // through EventHierarchy props → eventTree memo → flattenedItems memo → Virtuoso diff,
  // all for zero actual change.
  const displayEventsRef = useRef<PollingEvent[]>([])
  const emptyTerminalRestoreAttemptRef = useRef<Set<string>>(new Set())

  const displayEvents = useMemo(() => {
    const filtered = tabEvents.filter(event => {
      if (event.type === 'workspace_file_operation') return false

      // Synthetic auto-notifications are orchestration input for the agent, not
      // human chat. Keep them in the session/event store for sequencing and
      // history, but do not replay dozens of internal prompts when a read-only
      // scheduled run is converted to an interactive chat. Background-agent
      // start/completion events remain visible as the user-facing status UI.
      if (isInternalAutoNotificationEvent(event)) return false

      // Hide Total Token Usage and Context Offloading events
      if (event.type === 'token_usage') {
        const agentEvent = event.data as { data?: Record<string, unknown> } | undefined
        const payload = agentEvent?.data || event.data as Record<string, unknown> | undefined

        if (payload?.context === 'conversation_total') {
          return false
        }
      }

      if (event.type === 'large_tool_output_detected' || event.type === 'large_tool_output_file_written') {
        return false
      }

      return true
    })

    // REF-STABILITY CHECK
    // .filter() always returns a new array, even when every element passes through unchanged.
    // That new reference triggers downstream useMemo/React.memo to recompute (they compare by ===).
    //
    // Events are append-only with unique IDs and immutable payloads, so we can cheaply detect
    // "same output" by comparing length + first ID + last ID (3 string comparisons).
    //
    // When the check passes we return the *previous* array ref — downstream memos see the same
    // object and bail out entirely: eventTree skip → flattenedItems skip → Virtuoso no-op.
    const prev = displayEventsRef.current
    if (
      filtered.length === prev.length &&   // same count after filtering
      filtered.length > 0 &&               // guard against empty-to-empty flip
      filtered[0]?.id === prev[0]?.id &&   // first event unchanged (catches cleanup trimming from front)
      filtered[filtered.length - 1]?.id === prev[prev.length - 1]?.id  // last event unchanged (catches new appends)
    ) {
      return prev  // same ref → no downstream recomputation
    }

    // Output actually changed — cache the new array for next comparison
    displayEventsRef.current = filtered
    return filtered
  }, [tabEvents])

  const hasConversationContent = useMemo(() => {
    return displayEvents.some(event =>
      event.type === 'user_message' ||
      event.type === 'conversation_end' ||
      event.type === 'unified_completion'
    )
  }, [displayEvents])

  const shouldFetchSessionExecutionTree =
    !!activeSessionId &&
    activeEventViewMode === 'tree'
  const { data: sessionExecutionTree } = useSessionExecutionTree(activeSessionId, shouldFetchSessionExecutionTree)

  // --- Render tracking (filter by [Render] in console) ---
  useRenderLogger('ChatArea', {
    displayEvents: displayEvents.length,
    tabEvents: tabEvents.length,
    isStreaming,
    autoScroll,
    activeTabId: activeTab?.tabId,
    activeSessionId,
    finalResponse: !!finalResponse,
    tabSessionKey,
  })
  useMemoLogger('ChatArea.displayEvents', displayEvents, displayEvents.length)
  
  // Computed values
  const isRequiredFolderSelected = useMemo(() => {
    if (selectedModeCategory !== 'workflow') return true; // No validation needed for other modes
    if (activeTab?.metadata?.isOrganizationAssistant) return true

    // Workflow mode requires Workflow/ folder from preset
    if (selectedModeCategory === 'workflow') {
      const workflowFolder = activeWorkflowPreset?.selectedFolder?.filepath
      return workflowFolder ? workflowFolder.startsWith('Workflow/') : false
    }
    
    return true;
  }, [selectedModeCategory, activeWorkflowPreset])

  // Use currentPresetServers from props (passed from App.tsx when preset is selected)

  // State for preset selection overlay
  const [showPresetSelection, setShowPresetSelection] = useState(false)
  const [pendingModeCategory, setPendingModeCategory] = useState<Exclude<ModeCategory, null> | null>(null)
  
  // State for session restoration loading
  const [isRestoringChatSessions, setIsRestoringChatSessions] = useState(false)
  // Workflow-mode restore flag is owned by WorkflowLayout via useChatStore so we can show
  // an in-panel spinner here while reconnectWorkflowTabs() is replaying events.
  const isRestoringWorkflowSessions = useChatStore(state => state.isRestoringWorkflowSessions)
  // A resumed chat sets restoredConversationPath on the tab (cleared by New Chat's
  // resetTabChat). We no longer hard-hide the list on this marker alone — an empty
  // or stale resume (terminal restore that yielded nothing) would otherwise leave a
  // dead pane with no way back to the chats list. Instead we hide the list only
  // while the resume is genuinely loading (see the two guards below), so an empty
  // resumed tab falls through to the previous-chats list.
  const activeTabHasRestoredConversation = !!activeTab?.config?.restoredConversationPath
  // Terminal / coding-agent resumes have no conversation events — their surface is
  // the terminal pane. Keep the list hidden while the restored session still shows
  // execution-tree activity so it never replaces a populated terminal/coding pane.
  const restoredSessionHasExecutionContent =
    activeTabHasRestoredConversation &&
    !!sessionExecutionTree &&
    (sessionExecutionTree.summary.running_count +
      sessionExecutionTree.summary.completed_count +
      sessionExecutionTree.summary.failed_count +
      sessionExecutionTree.summary.canceled_count) > 0
  // A native/terminal resume reattaches a live terminal but produces NO
  // execution-tree nodes, so restoredSessionHasExecutionContent alone can't see
  // it — without this probe the surface would flip from the restored terminal to
  // the previous-chats landing once the settle window elapsed. Probe the same
  // terminal list that decides TerminalCenter's "No terminals yet" empty state;
  // a present terminal means the resumed tab's surface is the terminal pane.
  const shouldProbeSessionTerminals =
    !!activeSessionId &&
    (
      activeTabHasRestoredConversation ||
      (selectedModeCategory === 'workflow' && activeEventViewMode === 'terminal')
    )
  const { data: sessionTerminals, isFetched: sessionTerminalsFetched } = useSessionTerminals(
    activeSessionId,
    shouldProbeSessionTerminals,
  )
  // Subscribe to active sessions cache updates. Keep this near the terminal
  // surface derivations so pending/running terminal flags are initialized before
  // later restore/surface effects read them.
  const activeSessionsCache = useChatStore((state) => state.activeSessionsCache)
  const activeSessionIds = useMemo(() => {
    return new Set(activeSessionsCache.map(s => s.session_id))
  }, [activeSessionsCache])
  const restoredSessionTerminals = sessionTerminals?.terminals || []
  // A tab observing a specific scheduled/bot run is a read-only view of THAT
  // run, never a fresh chat. The chat-surface resolver keeps such tabs in
  // restoring (while events load) or active (once present) and never lets them
  // bounce to the previous-chats landing panel (the "schedule-bounce" fix).
  const isReadOnlyRunView =
    !!activeTab?.metadata?.isScheduledRun || !!activeTab?.metadata?.isBotRun
  const readOnlyRunHasTerminal =
    isReadOnlyRunView && selectedModeCategory === 'workflow' && restoredSessionTerminals.length > 0
  const restoredSessionHasTerminal =
    (activeTabHasRestoredConversation || readOnlyRunHasTerminal) && restoredSessionTerminals.length > 0
  // Any recognized "this resumed tab is live" signal: execution-tree activity OR
  // a live terminal pane. Feeds the resolver's active-over-landing decision.
  const hasRestoredLiveContent = restoredSessionHasExecutionContent || restoredSessionHasTerminal
  // Use the ACTIVE TAB's streaming flag, not the global state.isStreaming, which
  // lingers true after New Chat from a running conversation (a cross-tab signal,
  // not session-scoped) and would wrongly force 'active'.
  const activeTabStreaming = !!activeTab?.isStreaming
  const activeTabHasRunningBackendSession = !!activeTab?.sessionId && activeSessionsCache.some(session => {
    if (session.session_id !== activeTab.sessionId) return false
    const status = (session.status || '').toLowerCase()
    return status === 'running' || status === 'paused'
  })
  const hasLiveWorkflowTerminal = selectedModeCategory === 'workflow' && restoredSessionTerminals.some(isLiveWorkflowTerminal)
  const terminalProbeSettledEmpty =
    shouldProbeSessionTerminals &&
    sessionTerminalsFetched &&
    restoredSessionTerminals.length === 0
  const terminalActivityStillPlausible =
    !terminalProbeSettledEmpty ||
    activeTab?.metadata?.isViewOnly !== true
  // The terminal empty-state copy is narrower still: "Starting terminal / Your
  // message was sent" is only true for a live submitted/running turn, not merely
  // because an opened/restored workflow tab has historical conversation events.
  // Read-only Schedule/Bot runs can stay "running" in the backend after their
  // terminal pane is gone; once the terminal probe has settled empty, do not keep
  // forcing a dead "Starting terminal" surface.
  const hasPendingTerminalActivity = !suppressTerminalPane && (
    (activeTabStreaming && terminalActivityStillPlausible) ||
    (activeTabHasRunningBackendSession && terminalActivityStillPlausible)
  )
  // Resume give-up TIMER only. A resume that never produces a terminal/content may
  // eventually fall to 'landing'; resumeGaveUp flips true after RESUME_SETTLE_MS so
  // a genuinely-dead resume isn't stuck on a spinner forever. This is purely a
  // timeout — the "resume is pending" decision is derived SYNCHRONOUSLY below, not
  // from this state, so the first render after a resume can never be 'landing'.
  const [resumeGaveUp, setResumeGaveUp] = useState(false)
  useEffect(() => {
    if (!activeTabHasRestoredConversation || hasConversationContent || activeTabStreaming) {
      setResumeGaveUp(false)
      return
    }
    setResumeGaveUp(false)
    const timer = window.setTimeout(() => setResumeGaveUp(true), RESUME_SETTLE_MS)
    return () => window.clearTimeout(timer)
  }, [activeTabHasRestoredConversation, hasConversationContent, activeTabStreaming, activeSessionId])
  // resumePending — SYNCHRONOUS (derived in render, NOT an effect-set state). This
  // is the regression fix: on a Resume click restoredConversationPath is set
  // synchronously (setTabConfig), so this is already true on the FIRST render →
  // resolveChatSurface returns 'restoring' on that render, never the transient
  // 'landing' that the old effect-lagged resumeSettling produced for one render.
  // That transient 'landing' was what let the clear-on-landing effect fire and
  // cancel the resume (the 2-3-click flakiness). resumePending stays true until
  // content/stream arrives (→ 'active') or the give-up timer elapses (→ 'landing').
  const resumePending =
    activeTabHasRestoredConversation &&
    !hasConversationContent &&
    !activeTabStreaming &&
    !resumeGaveUp
  // Resume a previous chat from the landing "Previous chats" panel. The same
  // resume path is used anywhere else that needs to restore a multi-agent chat.
  const handleResumePreviousChat = useResumePreviousChat()

  // State for mode switch dialog
  const [showModeSwitchDialog, setShowModeSwitchDialog] = useState(false)
  const [pendingModeSwitch, setPendingModeSwitch] = useState<Exclude<ModeCategory, null> | null>(null)
  

  // Handle mode selection from dropdown
  // Handle mode switching with preset selection for Workflow
  const handleModeSwitchWithPreset = (category: Exclude<ModeCategory, null>) => {
    if (category === 'multi-agent') {
      // Multi-agent mode doesn't need preset selection
      // Clear any active presets when switching to multi-agent mode
      clearActivePreset('workflow')
      switchMode(category)
    } else {
      // Workflow mode - always show preset selection when switching between modes
      // Clear the current mode's preset first
      if (selectedModeCategory === 'workflow') {
        clearActivePreset('workflow')
      }
      
      // Check if target mode already has a preset
      const activePreset = getActivePreset(category)
      
      if (activePreset) {
        // Preset already selected, switch mode directly
        switchMode(category)
      } else {
        // No preset selected, show preset selection overlay
        setPendingModeCategory(category)
        setShowPresetSelection(true)
      }
    }
  }

  // Switch mode function
  const switchMode = (category: Exclude<ModeCategory, null>) => {
    const { setModeCategory, getAgentModeFromCategory } = useModeStore.getState()
    const { setAgentMode } = useAppStore.getState()
    
    setModeCategory(category)
    
    // Set the corresponding agent mode using centralized mapping
    const agentModeToSet = getAgentModeFromCategory(category) as AgentMode
    setAgentMode(agentModeToSet)
  }

  // Handle preset selection from overlay
  const handlePresetSelected = (presetId: string) => {
    if (pendingModeCategory) {
      // Now switch to the mode
      switchMode(pendingModeCategory)
      
      // Apply the preset after mode switch (this will also set the active preset ID)
      setTimeout(() => {
        const result = applyPreset(presetId, pendingModeCategory)
        if (!result.success) {
          logger.error('ChatArea', 'Failed to apply preset:', result.error)
        }
      }, 100)
      
      // Close overlay
      setShowPresetSelection(false)
      setPendingModeCategory(null)
    }
  }

  // Handle preset selection overlay close
  const handlePresetSelectionClose = () => {
    setShowPresetSelection(false)
    setPendingModeCategory(null)
  }

  
  // Filter toasts to only include types supported by ToastContainer
  const filteredToasts = toasts.filter((toast: { type: string }) => toast.type === 'success' || toast.type === 'info' || toast.type === 'error') as Array<{id: string, message: string, type: 'success' | 'info' | 'error'}>
  
  // Handle mode switch dialog confirmation
  const handleModeSwitchConfirm = () => {
    if (pendingModeSwitch) {
      handleModeSwitchWithPreset(pendingModeSwitch)
      // Clear backend session and reset UI after mode switch
      handleNewChat()
    }
    setShowModeSwitchDialog(false)
    setPendingModeSwitch(null)
  }
  
  // Handle mode switch dialog cancellation
  const handleModeSwitchCancel = () => {
    setShowModeSwitchDialog(false)
    setPendingModeSwitch(null)
  }
  
  // Add ref for auto-scrolling
  const chatContentRef = useRef<HTMLDivElement>(null)
  
  // Add ref for workflow mode handler
  const workflowModeHandlerRef = useRef<WorkflowModeHandlerRef>(null)
  
  
  // Track processed completion events to avoid stopping on old ones
  const processedCompletionEventsRef = useRef<Set<string>>(new Set())
  

  // Selected preset folder state
  const lastEventIndexRef = useRef<number>(-1)
  // Deprecated: totalEventsRef removed
  const previousEventCountRef = useRef<number>(0)

  // Track whether workspace-modifying events occurred during the current run
  const hadWorkspaceActivityRef = useRef<boolean>(false)

  // Ref to track if we're currently performing programmatic scrolling
  const isProgrammaticScrollRef = useRef<boolean>(false)
  const programmaticScrollTimeoutRef = useRef<NodeJS.Timeout | null>(null)
  const manualScrollVersionRef = useRef<number>(0)
  // Local ref for scroll position — avoids Zustand re-renders on every scroll event
  const lastScrollTopRef = useRef<number>(0)

  // Ref to track currentWorkflowPhase without causing callback re-renders
  const currentWorkflowPhaseRef = useRef<string>(currentWorkflowPhase)
  useEffect(() => {
    currentWorkflowPhaseRef.current = currentWorkflowPhase
  }, [currentWorkflowPhase])

  // Observer initialization removed - no longer needed

  // Re-enable auto-scroll when user scrolls back to the bottom.
  // The wheel handler below covers the disable-on-scroll-up path.
  const handleScroll = useCallback(() => {
    if (!chatContentRef.current) return;
    const element = chatContentRef.current;
    const currentScrollTop = element.scrollTop;
    if (isProgrammaticScrollRef.current) {
      lastScrollTopRef.current = currentScrollTop;
      return;
    }

    const movedUp = currentScrollTop < lastScrollTopRef.current - 2;
    const atBottom = isAtBottom(element);
    if (movedUp && !atBottom) {
      manualScrollVersionRef.current += 1;
      if (autoScroll) setAutoScroll(false);
    } else if (atBottom && !autoScroll) {
      setAutoScroll(true);
    }
    lastScrollTopRef.current = currentScrollTop;
  }, [autoScroll, isAtBottom, setAutoScroll]);

  // Set up scroll + wheel event listeners
  useEffect(() => {
    const element = chatContentRef.current;
    if (!element) return;

    lastScrollTopRef.current = element.scrollTop;

    const onWheel = (e: WheelEvent) => {
      if (e.deltaY < 0 && element.scrollTop > 0) {
        // Only disable if user is scrolling up AND there's room to scroll up
        // (i.e., not already at the very top or at the bottom with no overflow)
        const atBottom = element.scrollTop + element.clientHeight >= element.scrollHeight - 150;
        if (!atBottom) {
          manualScrollVersionRef.current += 1;
          setAutoScroll(false);
        }
      }
    };

    element.addEventListener('scroll', handleScroll);
    element.addEventListener('wheel', onWheel, { passive: true });
    return () => {
      element.removeEventListener('scroll', handleScroll);
      element.removeEventListener('wheel', onWheel);
      if (programmaticScrollTimeoutRef.current) {
        clearTimeout(programmaticScrollTimeoutRef.current);
        programmaticScrollTimeoutRef.current = null;
      }
    };
  }, [handleScroll, setAutoScroll]);

  // Reset auto-scroll when starting new conversation (events go from 0 to > 0)
  // Use displayEvents (tabEvents) instead of events to track the actual displayed events
  useEffect(() => {
    const currentEventCount = displayEvents.length
    const previousEventCount = previousEventCountRef.current
    
    // Only reset auto-scroll when starting a new conversation (0 -> > 0)
    // Don't reset if user has manually disabled it or if events are just updating
    const isRestoredMultiAgentHydration = selectedModeCategory === 'multi-agent' && (
      isRestoringChatSessions ||
      activeTabHasRestoredConversation ||
      activeTab?.metadata?.isRestored === true
    )
    if (previousEventCount === 0 && currentEventCount > 0 && !isStreaming && !isRestoredMultiAgentHydration) {
      setAutoScroll(true);
    }
    
    previousEventCountRef.current = currentEventCount
  }, [activeTab?.metadata?.isRestored, activeTabHasRestoredConversation, displayEvents.length, isRestoringChatSessions, isStreaming, selectedModeCategory, setAutoScroll]);

  // Improved auto-scroll for new events
  const scrollToBottom = useCallback((behavior: ScrollBehavior = 'smooth') => {
    if (!chatContentRef.current) return;

    // Mark that we're performing programmatic scrolling
    isProgrammaticScrollRef.current = true
    
    // Clear any existing timeout
    if (programmaticScrollTimeoutRef.current) {
      clearTimeout(programmaticScrollTimeoutRef.current)
    }
    
    // Use requestAnimationFrame for smoother scrolling
    requestAnimationFrame(() => {
      const element = chatContentRef.current
      if (!element) return

      const targetScrollTop = element.scrollHeight - element.clientHeight
      element.scrollTo({
        top: targetScrollTop,
        behavior
      });
      
      // Clear the programmatic scroll flag after scroll completes
      // For smooth scroll, wait longer; for instant, clear immediately
      const timeoutDuration = behavior === 'smooth' ? 600 : 100
      programmaticScrollTimeoutRef.current = setTimeout(() => {
        isProgrammaticScrollRef.current = false
        programmaticScrollTimeoutRef.current = null
      }, timeoutDuration)
    });
  }, [])

  // Callback to re-enable auto-scroll and scroll to bottom after feedback submission
  const handleFeedbackSubmitted = useCallback(() => {
    setAutoScroll(true)
    scrollToBottom('smooth')
  }, [setAutoScroll, scrollToBottom])

  // Auto-scroll to bottom when new events arrive (only if autoScroll is enabled)
  // Use displayEvents (tabEvents) instead of events to track the actual displayed events
  useEffect(() => {
    if (autoScroll && chatContentRef.current && displayEvents.length > 0) {
      // During streaming, use instant scroll — smooth scroll called repeatedly every event
      // causes each call to interrupt the previous animation, producing visible jank.
      scrollToBottom(isStreaming ? 'instant' : 'smooth');
    }
  }, [displayEvents.length, autoScroll, scrollToBottom, isStreaming])

  // Auto-scroll to bottom when final response is updated (only if autoScroll is enabled)
  useEffect(() => {
    if (autoScroll && chatContentRef.current && finalResponse) {
      scrollToBottom('smooth');
    }
  }, [finalResponse, autoScroll, scrollToBottom])

  // Scroll to bottom when switching tabs (including workflow switch via Ctrl+K)
  useEffect(() => {
    if (!targetTabId) return
    // Re-enable auto-scroll so subsequent events keep the view pinned to the bottom
    setAutoScroll(true)
    const scrollVersion = manualScrollVersionRef.current
    const scrollIfUserHasNotMoved = (behavior: ScrollBehavior) => {
      if (manualScrollVersionRef.current === scrollVersion) {
        scrollToBottom(behavior)
      }
    }
    // Small delay to let the new tab's content render before scrolling.
    // Use two attempts: 50ms for fast renders, 300ms as fallback when events are still loading.
    const timer1 = setTimeout(() => scrollIfUserHasNotMoved('instant'), 50)
    const timer2 = setTimeout(() => scrollIfUserHasNotMoved('instant'), 300)
    return () => { clearTimeout(timer1); clearTimeout(timer2) }
  }, [targetTabId, scrollToBottom, setAutoScroll])

  // Cross-mode switchers can change mode/preset before the target ChatArea has
  // fully rendered. Listen for an explicit request and retry shortly after.
  useEffect(() => {
    const handleScrollRequest = () => {
      setAutoScroll(true)
      scrollToBottom('instant')
      const timer1 = setTimeout(() => scrollToBottom('instant'), 80)
      const timer2 = setTimeout(() => scrollToBottom('instant'), 350)
      return () => {
        clearTimeout(timer1)
        clearTimeout(timer2)
      }
    }

    let cleanupTimers: (() => void) | null = null
    const listener = () => {
      cleanupTimers?.()
      cleanupTimers = handleScrollRequest()
    }
    window.addEventListener('chat-scroll-to-bottom', listener)
    return () => {
      cleanupTimers?.()
      window.removeEventListener('chat-scroll-to-bottom', listener)
    }
  }, [scrollToBottom, setAutoScroll])

  // Update refs when values change (for global observer)
  useEffect(() => {
    if (!activeTab) {
      lastEventIndexRef.current = lastEventIndex
    }
  }, [lastEventIndex, activeTab])
  
  // Update displayEvents when active tab changes
  // Tab events are automatically loaded via tabEvents useMemo
  
  // Deprecated: totalEventsRef useEffect removed

  // Workflow preset handlers
  const handleWorkflowPresetSelected = useCallback(async (presetId: string, presetContent: string) => {
    // Apply the preset using the global preset store
    // File context is now preset-specific (from preset.selectedFolder), no need to clear
    applyPreset(presetId, 'workflow')
    setCurrentWorkflowQueryId(presetId) // Store the preset query ID for workflow approval
    
    try {
      // Ensure phases are loaded and get them from store
      const workflowStore = useWorkflowStore.getState()
      if (!workflowStore.phasesInitialized) {
        await workflowStore.loadPhases()
      }
      const phases = workflowStore.phases
      const phaseIds = phases.map(p => p.id)
      const defaultPhase = workflowStore.getDefaultPhase()
      
      // Check if workflow already exists for this preset
      const workflowStatus = await agentApi.getWorkflowStatus(presetId)
      
      if (workflowStatus.success && workflowStatus.workflow) {
        const workflow = workflowStatus.workflow
        const status = workflow.workflow_status
        
        // Set the workflow phase based on the database status
        // Use the status if it's a valid phase ID, otherwise use default (first phase)
        if (status && phaseIds.includes(status)) {
          setCurrentWorkflowPhase(status)
        } else {
          // Default to first phase if status is invalid or not found
          setCurrentWorkflowPhase(defaultPhase)
        }
        
        // Use presetContent directly (this is the objective from preset query)
        setCurrentQuery(presetContent)
      } else {
        // No workflow exists, proceed with default phase
        setCurrentWorkflowPhase(defaultPhase)
        setCurrentQuery(presetContent)
      }
    } catch (error) {
      logger.error('ChatArea', 'Error checking workflow status:', error)
      // Fallback to default phase on error
      const defaultPhase = useWorkflowStore.getState().getDefaultPhase()
      setCurrentWorkflowPhase(defaultPhase)
      setCurrentQuery(presetContent)
    }
  }, [setCurrentQuery, applyPreset, setCurrentWorkflowPhase, setCurrentWorkflowQueryId])

  const handleWorkflowPresetCleared = useCallback(() => {
    clearActivePreset('workflow')
    setCurrentWorkflowQueryId(null) // Clear the stored preset query ID
    const defaultPhase = useWorkflowStore.getState().getDefaultPhase()
    setCurrentWorkflowPhase(defaultPhase) // Reset to default phase
    setCurrentQuery('')
  }, [clearActivePreset, setCurrentWorkflowQueryId, setCurrentWorkflowPhase, setCurrentQuery])
  
  // Clear workflow state when starting a new chat
  const clearWorkflowState = useCallback(() => {
    clearActivePreset('workflow')
    setCurrentWorkflowQueryId(null)
    const defaultPhase = useWorkflowStore.getState().getDefaultPhase()
    setCurrentWorkflowPhase(defaultPhase)
  }, [clearActivePreset, setCurrentWorkflowQueryId, setCurrentWorkflowPhase])

  // Handle human verification actions
  // TODO: Re-enable when RequestHumanFeedbackEvent is available
  /*
  const handleApproveWorkflow = useCallback(async (_requestId: string, eventData?: { next_phase?: string }) => {
    
    setIsApprovingWorkflow(true)  // Set loading state
    
    // Use the stored preset query ID instead of the request ID
    const presetQueryId = currentWorkflowQueryId
    if (!presetQueryId) {
      logger.error('ChatArea', 'No preset query ID available for workflow approval')
      setIsApprovingWorkflow(false)
      return
    }
    
    try {
      // Determine next phase based on event data
      // If next_phase is provided, use it; otherwise get the second phase (planning) as default
      let nextPhase = eventData?.next_phase
      if (!nextPhase) {
        const phases = useWorkflowStore.getState().phases
        // Use second phase (planning) if available, otherwise first phase
        nextPhase = phases.length > 1 ? phases[1].id : (phases.length > 0 ? phases[0].id : 'execution')
      }
      
      // Update workflow status to the determined next phase
      await agentApi.updateWorkflow(presetQueryId, nextPhase)
      
      // Stop any ongoing SSE / polling to prevent events from coming back
      if (currentTab?.sessionId) {
        disconnectSSE(currentTab.sessionId)
      }
      if (pollingInterval) {
        stopPolling()
      }

      // Clear all events to show clean slate for execution phase
      // Note: Using tabEvents now, not global events
      if (currentTab?.sessionId) {
        chatStore.clearTabEvents(currentTab.sessionId)
      }
      // Deprecated: setLastEventCount removed
      setLastEventIndex(-1)
      setFinalResponse('')
      setIsCompleted(false)
      setCurrentUserMessage('')
      setShowUserMessage(false)
      
      // Update phase to the determined next phase
      setCurrentWorkflowPhase(nextPhase as WorkflowPhase)
      
    } catch (error) {
      logger.error('ChatArea', 'Failed to approve workflow:', error)
      // TODO: Show error message to user
    } finally {
      setIsApprovingWorkflow(false)  // Clear loading state
    }
  }, [currentWorkflowQueryId, pollingInterval, setIsApprovingWorkflow, setLastEventIndex, setFinalResponse, setIsCompleted, setCurrentUserMessage, setShowUserMessage, setCurrentWorkflowPhase, setPollingInterval])
  */

  // Observer initialization removed - no longer needed

  // (Batching removed — events are now processed immediately as they arrive)

  // Removed extractUserMessageContent - no longer needed since we removed duplicate detection


  // Get polling management actions from store (before pollEvents callback)
  const { startPolling, stopPolling, getActiveSessions, connectSSE, disconnectSSE, disconnectAllSSE } = useChatStore(useShallow(state => ({
    startPolling: state.startPolling,
    stopPolling: state.stopPolling,
    getActiveSessions: state.getActiveSessions,
    connectSSE: state.connectSSE,
    disconnectSSE: state.disconnectSSE,
    disconnectAllSSE: state.disconnectAllSSE,
  })))
  const buildExecutionOptions = useWorkflowStore(state => state.buildExecutionOptions)

  // Get active sessions from cache (shared across all components)
  const startActiveSessionsPolling = useChatStore(state => state.startActiveSessionsPolling)
  
  // Track recently notified workshop agent names to prevent duplicate notifications
  // (retries emit multiple orchestrator_agent_end events with the same agent name)
  const notifiedWorkshopAgentsRef = useRef<Set<string>>(new Set())
  // Suppress auto-notifications during initial SSE backfill (first 3s after mount).
  // Without this, page reload would replay all old completion events as new notifications.
  // After the backfill window, all notifications are allowed. The dedup set
  // (notifiedWorkshopAgentsRef) still prevents duplicates within a session.
  const hasUserSentMessageRef = useRef(false)
  useEffect(() => {
    const timer = setTimeout(() => {
      hasUserSentMessageRef.current = true
    }, 3000)
    return () => clearTimeout(timer)
  }, [])

  // Reusable event processing logic — shared by both SSE and polling paths.
  // Takes an events response (same shape from SSE or REST) and a tab, then processes
  // session status, streaming chunks, event filtering, and stores events.
  const processEventsResponse = useCallback((
    response: { events: PollingEvent[]; session_status?: string; last_processed_index?: number; has_more?: boolean; has_running_background_agents?: boolean; is_synthetic_turn?: boolean; can_steer?: boolean; session_id?: string },
    sessionId: string,
    tab: ChatTab | null
  ) => {
    const chatStore = useChatStore.getState()
    const actualSessionId = response.session_id || sessionId

    // Check if this tab belongs to the currently active workflow preset.
    // Background preset tabs still store events but skip UI side effects
    // (workspace refresh, canvas updates, step progress) to avoid polluting the visible workflow.
    const isActivePresetTab =
      tab?.metadata?.presetQueryId === useGlobalPresetStore.getState().activePresetIds.workflow

    // --- Session status handling ---
    const sessionStatus = response.session_status
    if (tab && sessionStatus) {
      const hasBgAgents = response.has_running_background_agents ?? false
      const isSyntheticTurn = response.is_synthetic_turn ?? false
      const canSteer = response.can_steer ?? false
      const isForegroundStreaming = sessionStatus === 'running' && !isSyntheticTurn && (!hasBgAgents || canSteer)
      if (sessionStatus === 'completed' || sessionStatus === 'error') {
        if (hasBgAgents) {
          chatStore.setTabCompleted(tab.tabId, false)
          chatStore.setTabStreaming(tab.tabId, false)
        } else {
          chatStore.setTabCompleted(tab.tabId, true)
          chatStore.setTabStreaming(tab.tabId, false)
        }
        chatStore.clearStreamingText(actualSessionId)
      } else if (sessionStatus === 'running') {
        chatStore.setTabCompleted(tab.tabId, false)
        chatStore.setTabStreaming(tab.tabId, isForegroundStreaming)
      } else if (sessionStatus === 'stopped' || sessionStatus === 'inactive') {
        chatStore.setTabCompleted(tab.tabId, false)
        chatStore.setTabStreaming(tab.tabId, false)
        chatStore.clearStreamingText(actualSessionId)
      }
      chatStore.setTabHasRunningBgAgents(tab.tabId, hasBgAgents)
      setTabSyntheticTurn(tab.tabId, isSyntheticTurn)
      setTabCanSteer(tab.tabId, canSteer)
    } else if (!tab && sessionStatus) {
      const hasBgAgents = response.has_running_background_agents ?? false
      const isSyntheticTurn = response.is_synthetic_turn ?? false
      const canSteer = response.can_steer ?? false
      const isForegroundStreaming = sessionStatus === 'running' && !isSyntheticTurn && (!hasBgAgents || canSteer)
      if (sessionStatus === 'completed' || sessionStatus === 'error') {
        setIsStreaming(false)
        setIsCompleted(true)
        setHasActiveChat(false)
        chatStore.clearStreamingText(actualSessionId)
      } else if (sessionStatus === 'running') {
        setIsStreaming(isForegroundStreaming)
        setIsCompleted(false)
      } else if (sessionStatus === 'stopped' || sessionStatus === 'inactive') {
        setIsStreaming(false)
        setIsCompleted(false)
        chatStore.clearStreamingText(actualSessionId)
      }
    }

    // --- Update last event index ---
    // CRITICAL: Must happen BEFORE the empty-events early return below.
    // SSE backfill may contain only streaming events (handled immediately in handleSSEMessage),
    // leaving the batched events array empty. Without updating the index here, tabEventIndices
    // stays at 0 and every SSE reconnection re-fetches all events from the beginning.
    if (response.last_processed_index !== undefined && response.last_processed_index >= 0) {
      let newLastEventIndex = response.last_processed_index
      if (tab) {
        setTabLastEventIndex(actualSessionId, newLastEventIndex)
        if (response.has_more !== undefined) {
          chatStore.setTabHasMoreOlderEvents(actualSessionId, response.has_more)
        }
      } else {
        setLastEventIndex(newLastEventIndex)
      }
    }

    if (response.events.length === 0) return

    // --- Event filtering & processing ---
    const eventsBeforeFilter = response.events as PollingEvent[]
    const newEvents: PollingEvent[] = []
    let hasCompletionEvent = false
    // Check if we already have frontend-created user messages for this session.
    // We only want to suppress the backend echo for the exact same submitted text.
    // Other backend user_message events, like steer pickup notifications injected
    // later by the server, must still be allowed through.
    const existingEvents = chatStore.getTabEvents(actualSessionId)
    const frontendUserMessageContents = new Set(
      existingEvents
        .filter(e => e.type === 'user_message' && e.id?.startsWith('user-message-'))
        .map(e => getDisplaySafeUserMessageContent(getUserMessageContent(e)))
        .filter(Boolean)
    )
    const hasFrontendUserMessage = frontendUserMessageContents.size > 0

    for (const event of eventsBeforeFilter) {
      const agentEvent = event.data as Record<string, unknown> | undefined
      const innerData = agentEvent?.data as Record<string, unknown> | undefined
      const rawComponent = (event as unknown as Record<string, unknown>).component ?? innerData?.component ?? agentEvent?.component
      const rawCorrelationId = (event as unknown as Record<string, unknown>).correlation_id ?? innerData?.correlation_id ?? agentEvent?.correlation_id
      const isForegroundSession = isForegroundSessionEvent(event, rawComponent, rawCorrelationId)
      const runtimeScope = getRuntimeEventScope(event)
      const isSubAgentEvent = !isForegroundSession || runtimeScope.kind !== 'session'

      // Skip backend user_message events when we already have a frontend-created one
      // (avoids duplicate user message bubbles in the chat). Internal
      // [AUTO-NOTIFICATION] messages still enter the event store, but the
      // displayEvents filter keeps them out of the human timeline.
      if (event.type === 'user_message' && hasFrontendUserMessage && !event.id?.startsWith('user-message-')) {
        const msgContent = getDisplaySafeUserMessageContent(getUserMessageContent(event))
        if (
          !msgContent.startsWith(AUTO_NOTIFICATION_PREFIX) &&
          frontendUserMessageContents.has(msgContent)
        ) {
          continue
        }
      }

      if (isStreamingEventType(event.type)) {
        handleLiveStreamingEvent(event, actualSessionId, chatStore)
        continue
      }
      // Allow distinct backend user_message events through when there's no
      // matching frontend-created message. Internal auto-notifications are
      // retained here for orchestration and filtered only at display time.
      if (event.type === 'user_message' && hasFrontendUserMessage) {
        const msgContent = getDisplaySafeUserMessageContent(getUserMessageContent(event))
        if (
          !msgContent.startsWith(AUTO_NOTIFICATION_PREFIX) &&
          !frontendUserMessageContents.has(msgContent)
        ) {
          // This is a distinct backend user_message (for example a steer message
          // picked up mid-run), so keep it visible in the timeline.
        } else if (!msgContent.startsWith(AUTO_NOTIFICATION_PREFIX)) {
          continue
        }
      }

      if (
        event.type === 'auto_notification_steered' ||
        (event.type === 'user_message' && getDisplaySafeUserMessageContent(getUserMessageContent(event)).startsWith(AUTO_NOTIFICATION_PREFIX))
      ) {
        requestTerminalRefreshBurst()
      }

      if (!isSubAgentEvent && (event.type === 'llm_generation_end' || event.type === 'unified_completion' || event.type === 'agent_end' || event.type === 'conversation_end' || event.type === 'conversation_error' || event.type === 'context_cancelled')) {
        hasCompletionEvent = true
      }

      if (event.type === 'delegation_end') {
        const correlationId = innerData?.correlation_id ?? innerData?.delegation_id ?? agentEvent?.correlation_id ?? agentEvent?.delegation_id
        if (correlationId && typeof correlationId === 'string') {
          chatStore.clearDelegationStreamingText(correlationId)
        }
      }

      // Auto-refresh plan canvas when a plan modification tool completes
      if (event.type === 'tool_call_end') {
        const toolName = (innerData?.tool_name ?? agentEvent?.tool_name ?? '') as string
        const isPlanModTool = toolName.startsWith('update_') && (
          toolName.includes('step') || toolName.includes('validation') || toolName.includes('success_criteria')
        )
        const isAddTool = toolName.startsWith('add_') && toolName.includes('step')
        const isDeleteTool = toolName === 'delete_plan_steps'
        if (isPlanModTool || isAddTool || isDeleteTool) {
          console.log('[PLAN REFRESH] Plan modification detected via tool:', toolName)
          signalPlanModified()
        }
      }

      // Also detect workspace_file_operation events targeting plan.json
      if (event.type === 'workspace_file_operation') {
        const filePath = (innerData?.filepath ?? agentEvent?.filepath ?? innerData?.file_path ?? agentEvent?.file_path ?? '') as string
        if (filePath.includes('plan.json') || filePath.includes('step_config.json')) {
          console.log('[PLAN REFRESH] Workspace file operation on plan file:', filePath)
          signalPlanModified()
        }
      }

      // Dedup keys now include correlation_id (unique per execution), so clearing is not needed

      // Auto-notifications for workshop step completions are now handled entirely by the backend
      // via processBackgroundAgentCompletion → executeSyntheticTurn. The backend injects a
      // [AUTO-NOTIFICATION] user_message event which the frontend retains for
      // orchestration but does not show as human chat. No frontend queuing needed.
      //
      // Legacy: orchestrator_agent_end events were previously queued as auto-notifications here.
      // That code has been removed. The backend bgAgentRegistry handles all workshop execution
      // completion notifications.
      if (false && event.type === 'orchestrator_agent_end' && tab) {
        const agentType = (innerData?.agent_type ?? agentEvent?.agent_type ?? '') as string
        const isWorkshopWrapper = agentType === 'workshop-step-execution' || agentType === 'workshop-step-debug' || agentType === 'workshop-step-learning' || agentType === 'workshop-background-task' || agentType === 'workshop-report-execution'
        // Sub-agents within workshop steps have workshop_step_id in metadata (set by ContextAwareEventBridge)
        const metadata = (innerData?.metadata ?? agentEvent?.metadata) as Record<string, unknown> | undefined
        const workshopStepId = metadata?.workshop_step_id as string | undefined
        // Any agent with workshop_step_id metadata is a sub-agent of a workshop step
        // (includes execution, learning, eval, and generic agents)
        const isWorkshopSubAgent = !isWorkshopWrapper && !!workshopStepId
        if ((isWorkshopWrapper || isWorkshopSubAgent) && hasUserSentMessageRef.current) {
          if (isStaleAutoNotificationEvent(event)) {
            console.log('[WORKSHOP] Skipping stale auto-notification event', {
              eventType: event.type,
              agentType,
              timestamp: event.timestamp,
            })
            continue
          }

          const agentName = (innerData?.agent_name ?? agentEvent?.agent_name ?? 'unknown') as string
          const success = (innerData?.success ?? agentEvent?.success) as boolean
          const result = (innerData?.result ?? agentEvent?.result ?? '') as string

          const inputData = (innerData?.input_data ?? agentEvent?.input_data) as Record<string, string> | undefined
          const stepType = inputData?.step_type ?? ''

          // Skip notification for human_input steps — they complete instantly and don't need notifications
          // Skip notification for cancelled steps — only real failures should be reported
          const isCancelled = result.startsWith('Cancelled:')
          if (stepType === 'human_input' || isCancelled) {
            console.log('[WORKSHOP] Skipping notification for step', { agentName, stepType, isCancelled })
          } else {
            const truncated = result.length > 5000 ? result.substring(0, 5000) + '...' : result
            const fullFailureText = result
            const timestamp = formatAutoNotificationTime(event)
            const runFolder = inputData?.run_folder ?? ''
            const runInfo = runFolder ? ` [run: ${runFolder}]` : ''

            // Prefix all notifications so the LLM knows these are automated, not user messages
            const AUTO_PREFIX = `${AUTO_NOTIFICATION_PREFIX} `
            let notification: string
            if (agentType === 'workshop-step-learning') {
              notification = success
                ? `${AUTO_PREFIX}[LEARNING COMPLETE] [${timestamp}] ${agentName} — ${truncated}`
                : `${AUTO_PREFIX}[LEARNING FAILED] [${timestamp}] ${agentName} failed.\nError: ${fullFailureText}`
            } else if (agentType === 'workshop-step-debug') {
              notification = success
                ? `${AUTO_PREFIX}[OPTIMIZATION COMPLETE] [${timestamp}] ${agentName} — ${truncated}`
                : `${AUTO_PREFIX}[OPTIMIZATION FAILED] [${timestamp}] ${agentName} failed.\nError: ${fullFailureText}`
            } else if (agentType === 'workshop-background-task') {
              notification = success
                ? `${AUTO_PREFIX}[BACKGROUND TASK COMPLETE] [${timestamp}] ${agentName} finished.\nResult: ${truncated}`
                : `${AUTO_PREFIX}[BACKGROUND TASK FAILED] [${timestamp}] ${agentName} failed.\nError: ${fullFailureText}`
            } else {
              // Check if the result content indicates failure even when success=true (no execution error)
              // A step can complete without throwing an error but still report STATUS: FAILED in the result
              const resultIndicatesFailure = success && result && /STATUS:\s*FAILED|FAILED:|FAILURE:/i.test(result)
	              // Use frontend workshop mode (from UI toggle) — more reliable than backend auto-detection
	              const wfState = useWorkflowStore.getState()
	              const workshopMode = (() => {
	                const presetId = useGlobalPresetStore.getState().activePresetIds.workflow ?? ''
	                const presetWorkshopMode = presetId ? wfState.workshopModeByPreset[presetId] : undefined
	                return presetWorkshopMode || wfState.workshopMode
	              })() || (inputData?.workshop_mode ?? '') as string

              // Determine if this is a sub-agent within a todo task (vs a top-level step)
              const isSubAgent = isWorkshopSubAgent
              const eventLabel = isSubAgent ? 'SUB-AGENT' : 'STEP'

              // Action hints removed — system prompt already has detailed instructions
              const actionHint = ''

              if (resultIndicatesFailure) {
                notification = `${AUTO_PREFIX}[${eventLabel} FAILED] [${timestamp}]${runInfo} ${agentName} completed but result indicates failure.\nResult: ${fullFailureText}${actionHint}`
              } else if (success) {
                notification = `${AUTO_PREFIX}[${eventLabel} COMPLETED] [${timestamp}]${runInfo} ${agentName} finished successfully.\nResult: ${truncated}${actionHint}`
              } else {
                notification = `${AUTO_PREFIX}[${eventLabel} FAILED] [${timestamp}]${runInfo} ${agentName} failed.\nError: ${fullFailureText}${actionHint}`
              }
            }

	            const corrId = (innerData?.correlation_id ?? agentEvent?.correlation_id ?? '') as string
	            const dedupeKey = `${agentName}::${agentType}::${corrId}`
	            if (notifiedWorkshopAgentsRef.current.has(dedupeKey)) {
	              console.log('[WORKSHOP] Skipping duplicate notification for', dedupeKey)
		            } else {
		              const tabId = tab?.tabId
		              if (typeof tabId !== 'string') {
		                continue
		              }
		              const safeTabId = tabId as string
		              notifiedWorkshopAgentsRef.current.add(dedupeKey)
		              const currentQueue = chatStore.getTabConfig(safeTabId)?.queuedMessages || []
		              chatStore.setTabConfig(safeTabId, { queuedMessages: [...currentQueue, notification] })
		              console.log('[WORKSHOP] Queued step completion notification', { agentName, agentType, success })
		            }
          }
        }
      }

      // Track workspace-modifying events for refresh-on-completion
      if (event.type === 'workspace_file_operation') {
        hadWorkspaceActivityRef.current = true
      }
      if (event.type === 'tool_execution') {
        const toolName = innerData?.tool_name ?? agentEvent?.tool_name
        if (toolName === 'execute_shell_command') {
          hadWorkspaceActivityRef.current = true
        }
      }

      // PERF FIX: Only call processWorkspaceEvent for workspace_file_operation events.
      // Previously called for ALL events (tool_execution, streaming_text, delegation_start, etc.),
      // each incurring function call + event type check + dedup lookup overhead.
      // Also skip if this tab belongs to a background preset (avoid polluting visible workspace)
      if (event.type === 'workspace_file_operation' && isActivePresetTab !== false) {
        useWorkspaceStore.getState().processWorkspaceEvent(event)
      }

      if (event.type === 'learn_code_script_execution') {
        const scriptedData = (innerData ?? agentEvent ?? {}) as Record<string, unknown>
        console.log('[FIX_LEARN_CODE_UI] chat_area_event_received', {
          sessionId: actualSessionId,
          tabId: tab?.tabId ?? null,
          eventId: event.id,
          correlationId: (event as unknown as Record<string, unknown>).correlation_id ?? agentEvent?.correlation_id ?? scriptedData?.correlation_id ?? null,
          stepId: scriptedData.step_id ?? null,
          stepTitle: scriptedData.step_title ?? null,
          fixIteration: scriptedData.fix_iteration ?? null,
          isSavedScript: scriptedData.is_saved_script ?? null,
          success: scriptedData.success ?? null,
        })
      }

      newEvents.push(withDisplaySafeUserMessage(event))
    }
    // PERF FIX: Mark workspace as stale instead of auto-fetching.
    //
    // PROBLEM: Previously called fetchFiles() here, which fetches the entire workspace tree
    // (~2-3MB JSON for large workspaces with many workflow runs). This happened on every
    // completion event and background agent completion.
    //
    // FIX: Set needsRefresh flag → Workspace component shows a "Files may be out of date"
    // banner with a manual "Refresh" button. New files during execution are still added
    // incrementally via addFileToTree (from workspace_file_operation events, no network).
    const isCompletionLike = hasCompletionEvent || newEvents.some(e => e.type === 'background_agent_completed')
    if (isCompletionLike && hadWorkspaceActivityRef.current && isActivePresetTab !== false) {
      hadWorkspaceActivityRef.current = false
      const isChatLikeMode = selectedModeCategory === 'multi-agent'
      if (isChatLikeMode) {
        // Reconcile only dirty folders when we know them; fall back to a full refresh for
        // shell-driven changes where no workspace_file_operation events were emitted.
        console.log('[Workspace] Reconciling workspace (completion event + had workspace activity, multi-agent mode)')
        useWorkspaceStore.getState().refreshDirtyFolders({ fallbackToFullFetch: true })
      } else {
        // Workflow mode: just mark stale — workflow has its own debounced refresh logic
        console.log('[Workspace] Marking needsRefresh (completion event + had workspace activity)')
        useWorkspaceStore.getState().setNeedsRefresh(true)
      }
    }

    // Process workflow events — only for the ACTIVE preset's tabs
    // Background workflow tabs (different preset) still receive and store events via SSE,
    // but we skip side effects (canvas updates, step progress, workspace refresh) to avoid
    // polluting the currently visible workflow's UI state.
    //
    // PERF: Removed step_progress_updated / batch_group_start/end processing from chat events.
    // These were calling setStepStatus/handleBatchGroupStart which update workflowStore →
    // trigger usePlanToFlow → full Dagre layout recomputation for ALL canvas nodes on every event.
    // Step status coloring on the canvas is not needed during chat — it only matters in execution mode.
    // Auto-notifications for step completions are now handled by the backend via
    // processBackgroundAgentCompletion → executeSyntheticTurn. Disabled frontend queuing.
    if (false && selectedModeCategory === 'workflow') {
      for (const event of response.events as PollingEvent[]) {
        if (event.type === 'todo_task_step_completed' && hasUserSentMessageRef.current) {
          if (isStaleAutoNotificationEvent(event)) {
            console.log('[WORKFLOW] Skipping stale todo completion auto-notification', {
              timestamp: event.timestamp,
            })
            continue
          }

          const eventData = event.data as Record<string, unknown> | undefined
          const todoStepData = (eventData?.data as Record<string, unknown>) || eventData
          const stepTitle = todoStepData?.step_title as string | undefined
	          const tabId = tab?.tabId
	          const phaseId = tab?.metadata?.phaseId
		          if (typeof tabId === 'string' && stepTitle && isChatCompatiblePhase(phaseId)) {
		            const safeTabId = tabId as string
		            const dedupeKey = `${stepTitle}::todo-step`
		            if (!notifiedWorkshopAgentsRef.current.has(dedupeKey)) {
		              notifiedWorkshopAgentsRef.current.add(dedupeKey)
		              const notification = `${AUTO_NOTIFICATION_PREFIX} [STEP COMPLETED] [${formatAutoNotificationTime(event)}] ${stepTitle} finished successfully.`
		              const currentQueue = chatStore.getTabConfig(safeTabId)?.queuedMessages || []
		              chatStore.setTabConfig(safeTabId, { queuedMessages: [...currentQueue, notification] })
		            }
		          }
        }
      }
    }

    // Store events for ALL tabs with active SSE connections, including background presets.
    // Why: Background workflows keep SSE alive while running (see tabsWithActiveSessions).
    // Their events must be stored so they're visible when the user switches back — otherwise
    // tool calls, step completions, and agent outputs that arrived while viewing another
    // workflow would be permanently lost. UI side effects (workspace refresh, canvas updates,
    // auto-notifications) are still gated on isActivePresetTab above.
    if (tab && newEvents.length > 0) {
      const finalTab = chatStore.getTab(tab.tabId)
      if (!finalTab) return
      addTabEvents(actualSessionId, newEvents)
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [getTabEvents, setTabLastEventIndex, setLastEventIndex, addTabEvents, setIsStreaming, setIsCompleted, setHasActiveChat, selectedModeCategory])

  // Handle an incoming SSE event message: ignore global streaming display events,
  // then process non-streaming events inline.
  const handleSSEMessage = useCallback((msg: SSEEventMessage, sid: string) => {
    const chatStore = useChatStore.getState()
    const actualSessionId = (msg as unknown as Record<string, unknown>).session_id as string || sid

    const incomingEvents = msg.events

    // Streaming events are transport/progress noise for the main timeline. Keep
    // delegation and owned terminal state, but do not populate the global
    // generation card state.
    const nonStreamingEvents: PollingEvent[] = []
    for (const event of incomingEvents) {
      if (isStreamingEventType(event.type)) {
        // Process streaming events immediately for real-time text display.
        handleLiveStreamingEvent(event, actualSessionId, chatStore)
      } else {
        nonStreamingEvents.push(event)
      }
    }

    // Process non-streaming events immediately (no batching delay)
    if (nonStreamingEvents.length > 0 || msg.session_status) {
      const msgAny = msg as unknown as Record<string, unknown>
      const store = useChatStore.getState()
      const matchingTab = Object.values(store.chatTabs).find(t => t.sessionId === actualSessionId) || null
      processEventsResponse(
        {
          events: nonStreamingEvents,
          session_status: msg.session_status,
          last_processed_index: msg.last_processed_index,
          has_more: msgAny.has_more as boolean | undefined,
          has_running_background_agents: msg.has_running_background_agents,
          is_synthetic_turn: (msg as unknown as Record<string, unknown>).is_synthetic_turn as boolean | undefined,
          can_steer: (msg as unknown as Record<string, unknown>).can_steer as boolean | undefined,
          session_id: actualSessionId !== sid ? actualSessionId : undefined,
        },
        sid,
        matchingTab
      )
    }
  }, [processEventsResponse])

  // Handle SSE status-only messages (no events, just session status updates)
  const handleSSEStatus = useCallback((msg: SSEStatusMessage, sid: string) => {
    handleSSEMessage(
      { events: [], ...msg, last_processed_index: -1 } as SSEEventMessage,
      sid
    )
  }, [handleSSEMessage])

  // Polling function to get events for ALL active sessions (fallback when SSE unavailable)
  const pollEvents = useCallback(async () => {

    const chatStore = useChatStore.getState()

    // Read mode from store directly to avoid stale closure from setInterval capture
    const currentModeCategory = useModeStore.getState().selectedModeCategory

    // Get all tabs that should be polled (all tabs in current mode)
    const allTabs = Object.values(chatStore.chatTabs).filter(tab => {
      // If mode category is null (not yet selected), poll all non-workflow tabs
      if (!currentModeCategory) {
        return tab.metadata?.mode !== 'workflow'
      }
      return tab.metadata?.mode === currentModeCategory
    })
    
    // CRITICAL: Only poll tabs that are:
    // 1. Actively streaming (query in progress)
    // 2. Have session ID in backend's active sessions list (backend determines activity based on events)
    // 3. Multi-agent tabs (always poll — bg agents can produce events after orchestrator completes)
    // We don't poll completed sessions - they're done and won't have new events
    // We also don't poll uninitialized sessions (no query submitted yet)
    //
    // Read activeSessionIds fresh from the store to avoid stale closure from setInterval capture
    const freshActiveIds = new Set(chatStore.activeSessionsCache.map(s => s.session_id))
    const tabsToPoll = allTabs.filter(tab => {
      const currentTab = chatStore.getTab(tab.tabId)
      if (!currentTab?.sessionId) {
        return false
      }

      // Multi-agent tabs always get polled — bg agents can produce events
      // after the orchestrator completes (session_status='completed')
      if (currentTab.metadata?.mode === 'multi-agent') {
        return true
      }

      // Check if session is in backend's active sessions list (source of truth)
      // Backend determines activity based on event activity (10 min timeout)
      // CRITICAL: Also allow polling if tab is streaming (user just submitted a query)
      const isStreaming = currentTab.isStreaming
      const isInActiveSessions = freshActiveIds.has(currentTab.sessionId)

      // Allow polling if:
      // 1. Session is in backend's active sessions list, OR
      // 2. Tab is currently streaming (query just submitted)
      if (!isInActiveSessions && !isStreaming) {
        return false
      }

      // Skip if completed (definitely done) — unless background agents are still running
      if (currentTab.isCompleted && !currentTab.hasRunningBgAgents) {
        return false
      }

      return true
    })
    
    // CRITICAL: Poll by sessionId, not observerId
    // Multiple observers can view the same session, but events are stored per session
    const sessionsToPoll: Array<{ sessionId: string; tab: ChatTab | null }> = []
    
    // Add all tab sessions (deduplicate by sessionId)
    const seenSessionIds = new Set<string>()
    tabsToPoll.forEach(tab => {
      const currentTab = chatStore.getTab(tab.tabId)
      const sessionId = currentTab?.sessionId || tab.sessionId
      if (sessionId && !seenSessionIds.has(sessionId)) {
        seenSessionIds.add(sessionId)
        sessionsToPoll.push({ sessionId, tab: currentTab || tab })
      }
    })
    
    if (sessionsToPoll.length === 0) {
      return
    }
    
    // Poll each session
    for (const { sessionId, tab } of sessionsToPoll) {
      let currentTab = tab
      
      if (tab) {
        // Re-fetch the tab from store to ensure we have the latest session ID
        const fetchedTab = chatStore.getTab(tab.tabId)
        if (!fetchedTab) {
          continue
        }
        currentTab = fetchedTab
        
        // Verify session ID matches
        if (currentTab.sessionId !== sessionId) {
          // Use the new session ID
          if (!currentTab.sessionId) {
            continue
          }
        }
        
        // Double-check: verify this tab should still be polled
        // Only check isCompleted and sessionId - isStreaming is UI-only, not used for polling decisions
        if (currentTab.isCompleted && !currentTab.sessionId) {
          continue
        }
      }
      
      // Get fresh tab from store to ensure we have latest session ID
      const freshTab = currentTab ? chatStore.getTab(currentTab.tabId) : null
      const effectiveSessionId = freshTab?.sessionId || currentTab?.sessionId || sessionId
      
      let rawLastEventIndex = currentTab 
        ? getTabLastEventIndex(effectiveSessionId)
        : lastEventIndexRef.current
      
      // CRITICAL: Detect sentinel value (9999) which means "all events processed" but not an actual index
      // If lastEventIndex is 9999 or higher, check stored events to get the actual last index
      if (rawLastEventIndex >= 9999) {
        const storedEvents = getTabEvents(effectiveSessionId)
        if (storedEvents && storedEvents.length > 0) {
          const actualLastIndex = storedEvents.length - 1
          rawLastEventIndex = actualLastIndex
          // Update the stored index to the correct value
          if (currentTab) {
            setTabLastEventIndex(effectiveSessionId, actualLastIndex)
          } else {
            setLastEventIndex(actualLastIndex)
          }
        } else {
          // No stored events, but sentinel value - reset to 0 to start fresh
          rawLastEventIndex = 0
          if (currentTab) {
            setTabLastEventIndex(effectiveSessionId, 0)
          } else {
            setLastEventIndex(0)
          }
        }
      } else if (rawLastEventIndex === -1) {
        // Safety check: if index is -1 but we have events, use the event count
        // This prevents re-fetching from 0 if index state was lost but events exist
        const storedEvents = getTabEvents(effectiveSessionId)
        if (storedEvents && storedEvents.length > 0) {
          const actualLastIndex = storedEvents.length - 1
          rawLastEventIndex = actualLastIndex
          logger.debug('ChatArea', `Recovered lastEventIndex ${actualLastIndex} for session ${effectiveSessionId}`)
          
          if (currentTab) {
            setTabLastEventIndex(effectiveSessionId, actualLastIndex)
          }
        }
      }
      
      // Ensure lastEventIndex is >= 0 (API requirement)
      // -1 means "no events yet", which should be treated as 0
      const currentLastEventIndex = Math.max(0, rawLastEventIndex === -1 ? 0 : rawLastEventIndex)
      
      // Track which session is currently being polled (for derived isStreaming)

      try {
        const response = await agentApi.getSessionEvents(effectiveSessionId, currentLastEventIndex)

        // If response has a different session ID, update the tab
        if (currentTab && response.session_id && response.session_id !== effectiveSessionId) {
          chatStore.updateTabSessionId(currentTab.tabId, response.session_id)
        }

        processEventsResponse(response, effectiveSessionId, currentTab)
      } catch {
        // Continue polling other observers even if one fails
      }
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps -- selectedModeCategory read from store directly inside callback to avoid stale setInterval closure
  }, [getTabLastEventIndex, setTabLastEventIndex, setLastEventIndex, addTabEvents, getTabEvents, setIsStreaming, setIsCompleted, setHasActiveChat, activeSessionIds, processEventsResponse])

  const handleSSEFallback = useCallback((sessionId: string) => {
    logger.warn('ChatArea', `SSE failed for session ${sessionId}; starting polling fallback`)
    startPolling(pollEvents)
  }, [startPolling, pollEvents])


  
  // Start centralized active sessions polling when component mounts
  useEffect(() => {
    startActiveSessionsPolling()
    return () => {
      // Note: We don't stop polling here because other components might be using it
      // The polling will be managed globally and cleaned up when app unmounts
    }
  }, [startActiveSessionsPolling])

  // Unified page-load restore: handles both active sessions AND persisted tabs with no events.
  // Runs once per page load to avoid duplicate restores from separate effects racing each other.
  useEffect(() => {
    if (globalHasRestored) return
    // Only restore in multi-agent mode (workflow handles its own restore)
    if (selectedModeCategory !== 'multi-agent') return

    const restoreAll = async () => {
      globalHasRestored = true

      try {
        // Wait for active-sessions polling to start and return initial data
        await new Promise(resolve => setTimeout(resolve, 500))

        // --- Phase 1: restore active / recently-completed sessions from backend ---
        const activeSessions = await getActiveSessions(true)
        const restoredSessionIds = new Set<string>()

        if (activeSessions.length > 0) {
          const runningSessions = activeSessions.filter(s => {
            if (s.agent_mode?.toLowerCase() === 'workflow' || s.agent_mode?.toLowerCase() === 'workflow_phase') return false
            if (s.status === 'running') return true
            if (s.status === 'completed' && s.last_activity) {
              if (new Date(s.last_activity).getTime() > Date.now() - 30 * 60 * 1000) return true
            }
            return false
          })

          // Only restore sessions that have a persisted tab or are actively running
          const chatStore = useChatStore.getState()
          const persistedSessionIds = new Set(
            Object.values(chatStore.chatTabs)
              .filter(tab => tab.sessionId)
              .map(tab => tab.sessionId!)
          )
          const sessionsToRestore = runningSessions.filter(s =>
            persistedSessionIds.has(s.session_id) || s.status === 'running'
          )

          if (sessionsToRestore.length > 0) {
            setIsRestoringChatSessions(true)
          }

          for (const activeSession of sessionsToRestore) {
            try {
              const tabId = await restoreSession(activeSession.session_id, {
                title: activeSession.query || 'Active Chat',
                source: 'auto-restore',
              })
              restoredSessionIds.add(activeSession.session_id)
              if (sessionsToRestore.indexOf(activeSession) === 0) {
                switchTab(tabId)
              }
            } catch (err) {
              console.error(`[SessionRestore] auto-restore failed for ${activeSession.session_id}:`, err)
            }
          }
        }

        // --- Phase 2: hydrate persisted tabs that Phase 1 didn't cover ---
        // (completed sessions from history that are in localStorage but have no events)
        const chatStore = useChatStore.getState()
        const tabs = Object.values(chatStore.chatTabs)
        const tabsToHydrate = tabs.filter(tab => {
          if (!tab.sessionId || tab.metadata?.mode === 'workflow') return false
          if (restoredSessionIds.has(tab.sessionId)) return false
          return chatStore.getTabEvents(tab.sessionId).length === 0
        })
        if (tabsToHydrate.length > 0) {
          setIsRestoringChatSessions(true)
        }
        for (const tab of tabsToHydrate) {
          try {
            await restoreSession(tab.sessionId!, { source: 'page-refresh', skipConfigRestore: true })
          } catch (err) {
            console.error(`[SessionRestore] page-refresh hydrate failed for tab ${tab.tabId}:`, err)
          }
        }
      } catch (error) {
        console.error('[SessionRestore] page-load restore failed:', error)
      } finally {
        setIsRestoringChatSessions(false)
      }
    }

    restoreAll()
  }, [getActiveSessions, switchTab, selectedModeCategory])

  // Key that changes whenever any tab's view-mode flips, so the
  // Key that changes whenever any tab's view-mode flips. The SSE effect
  // uses it as a cheap wake-up signal so mode changes do not leave a tab's
  // connection state stale.
  const viewModeKey = useChatStore(state =>
    Object.entries(state.chatTabs)
      .map(([tid, t]) => `${tid}:${normalizeEventViewMode(t.viewMode)}`)
      .sort()
      .join('|')
  )

  // Only poll tabs that have their session ID in the backend's active sessions list
  // Backend determines activity based on event activity (10 min timeout)
  // CRITICAL: Also include tabs that are streaming (user just submitted a query)
  // This ensures restored sessions start polling immediately when replying
  const tabsWithActiveSessions = useMemo(() => {
    const activeIds = activeSessionIds // Capture in closure
    const chatStore = useChatStore.getState() // Get fresh store state to check streaming status
    
    const filtered = tabsWithSessions.filter(tab => {
      // Must have session ID
      if (!tab.sessionId) {
        return false
      }
      
      // Workflow tabs stay lightweight in terminal mode: historical event/tree
      // hydration is only needed when Tree is open. Keep live connections for
      // active terminal turns because some providers build their terminal pane
      // from streaming events instead of tmux snapshots.
      if (tab.metadata?.mode === 'workflow') {
        const activeWfPreset = useGlobalPresetStore.getState().activePresetIds.workflow
        const isActivePreset = tab.metadata?.presetQueryId === activeWfPreset
        const bgTab = chatStore.getTab(tab.tabId)
        const bgStreaming = bgTab?.isStreaming ?? tab.isStreaming
        const bgRunning = bgTab?.hasRunningBgAgents ?? false
        const viewMode = normalizeEventViewMode(bgTab?.viewMode ?? tab.viewMode)
        if (isActivePreset && viewMode === 'tree') return true
        return bgStreaming || bgRunning
      }

      // Skip completed sessions (definitely done) — unless bg agents are still running
      // In multi-agent mode, always keep polling (background agents can restart the session)
      const freshTab = chatStore.getTab(tab.tabId)
      if (tab.isCompleted && !(freshTab?.hasRunningBgAgents) && tab.metadata?.mode !== 'multi-agent') {
        return false
      }

      // CRITICAL: Check streaming status directly from store (not from tab object)
      // This ensures we get the latest streaming status even if tabsWithSessions is stale
      const currentTab = chatStore.getTab(tab.tabId)
      const isStreaming = currentTab?.isStreaming ?? tab.isStreaming

      // CRITICAL: Include tabs that are streaming (user just submitted a query)
      // This handles the case where a restored session is being replied to
      // The backend might not have added it to active sessions yet, but we should poll it
      if (isStreaming) {
        return true
      }

      // Include tabs with running background agents (even if session is "completed")
      if (currentTab?.hasRunningBgAgents) {
        return true
      }

      // In multi-agent mode, always keep polling (background agents can restart session at any time)
      if (tab.metadata?.mode === 'multi-agent') {
        return true
      }

      // Must be in backend's active sessions list
      // If backend says it's active, poll it even if local isStreaming is false
      // This ensures we catch events that come after stop is pressed
      if (!activeIds.has(tab.sessionId)) {
        return false
      }

      return true
    })
    
    return filtered
    // PERF FIX: Removed `chatTabs` from dependencies. Previously this memo recomputed on
    // every setTabStreaming/setTabCompleted/setTabConfig because `chatTabs` changed reference.
    // The function already uses getState() for fresh tab data (lines above), so the memo
    // only needs to recompute when tabsWithSessions or activeSessionIds actually change.
  }, [tabsWithSessions, activeSessionIds])
  
  // SSE connection management — connect/disconnect based on active sessions
  // Falls back to polling if SSE connection fails (handled inside connectSSE's onError callback)
  // NOTE: sseConnections is intentionally NOT in the dependency array to avoid infinite loops
  // (connectSSE updates the store → sseConnections changes → effect re-fires → connectSSE again)
  useEffect(() => {
    // Read SSE state fresh from store (not from React state to avoid dep cycle)
    const currentSSE = useChatStore.getState().sseConnections
    // Determine which session IDs need SSE connections.
    // Terminal view mode used to skip SSE — the assumption was that
    // TerminalCenter's /api/terminals poll covered everything. That was
    // true while every coding-agent provider was tmux-backed (live pane
    // state came from polling). For structured CLI providers (gemini-cli
    // since the contract flip, structured providers) the synthetic terminal is
    // built from streaming_chunk events, so skipping SSE means the pane
    // never updates and user messages appear lost. Connect SSE for every
    // active tab regardless of view mode.
    const neededSessionIds = new Set<string>()
    for (const tab of tabsWithActiveSessions) {
      if (!tab.sessionId) continue
      neededSessionIds.add(tab.sessionId)
    }
    // Connect SSE for sessions that don't have a connection yet (any
    // view mode — see neededSessionIds comment for why terminal mode
    // can no longer skip this).
    for (const tab of tabsWithActiveSessions) {
      if (!tab.sessionId) continue
      const sid = tab.sessionId
      if (currentSSE[sid]) {
        continue
      }

      connectSSE(
        sid,
        (msg: SSEEventMessage) => handleSSEMessage(msg, sid),
        (msg: SSEStatusMessage) => handleSSEStatus(msg, sid),
        () => handleSSEFallback(sid)
      )
    }

    // Disconnect SSE for sessions that are no longer active.
    // Safety guard: never disconnect a session whose tab still has isStreaming=true —
    // tabsWithActiveSessions may have computed before the latest setTabStreaming(true) call,
    // and disconnecting mid-execution would make the stop button disappear.
    const freshChatTabs = useChatStore.getState().chatTabs
    for (const sid of Object.keys(currentSSE)) {
      if (!neededSessionIds.has(sid)) {
        const stillStreaming = Object.values(freshChatTabs).some(
          t => t.sessionId === sid && t.isStreaming === true && !t.isCompleted
        )
        if (!stillStreaming) {
          disconnectSSE(sid)
        }
      }
    }

    // Stop polling when no active sessions
    if (neededSessionIds.size === 0 && pollingInterval) {
      stopPolling()
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps -- sseConnections excluded to prevent infinite loop
  }, [tabsWithActiveSessions, viewModeKey, connectSSE, disconnectSSE, handleSSEMessage, handleSSEStatus, handleSSEFallback, pollingInterval, startPolling, stopPolling, pollEvents])

  // When the active tab's viewMode flips OFF Terminal, kick a single
  // poll immediately so the backfill from the server's 1500-event ring
  // doesn't have to wait for the next poll tick.
  const prevActiveViewModeRef = useRef<string>(activeEventViewMode)
  useEffect(() => {
    if (prevActiveViewModeRef.current === 'terminal' && activeEventViewMode !== 'terminal') {
      void pollEvents()
    }
    prevActiveViewModeRef.current = activeEventViewMode
  }, [activeEventViewMode, pollEvents])

  // Cleanup polling and SSE on unmount
  useEffect(() => {
    return () => {
      // Disconnect all SSE connections
      disconnectAllSSE()
      // Use store's stopPolling to clean up
      if (pollingInterval) {
        stopPolling()
      }
    }
  }, [pollingInterval, stopPolling, disconnectAllSSE])
  

  const stopStreamingInFlightRef = useRef(false)

  const stopStreaming = useCallback(async () => {
    if (stopStreamingInFlightRef.current) return
    stopStreamingInFlightRef.current = true

    const chatStore = useChatStore.getState()
    
    // DO NOT stop polling - let backend determine activity based on events
    // Backend will mark session as inactive after 10 minutes of no events
    // This ensures we catch any pending events after stop is pressed
    
    // Cancel only the foreground LLM turn for this tab. Background/workflow
    // agents are intentionally left running; explicit session stop handles those.
    // CRITICAL: Only use the active tab's session ID - never fall back to global sessionId.
    const sessionIdToStop = activeTab?.sessionId
    if (!sessionIdToStop) {
      logger.warn('ChatArea', 'No session ID available for active tab')
      stopStreamingInFlightRef.current = false
      return
    }

    try {
      await agentApi.cancelCurrentTurn(sessionIdToStop)
    } catch (error) {
      logger.error('ChatArea', 'Failed to cancel current turn:', error)
    } finally {
      // Only mark idle after the backend acknowledges cancellation. If we flip
      // this earlier, queued/new messages can auto-send while the old foreground
      // turn is still accepting cancellation.
      setIsStreaming(false)

      if (activeTab) {
        chatStore.setTabStreaming(activeTab.tabId, false)
        chatStore.setTabCompleted(activeTab.tabId, true)
      }
      stopStreamingInFlightRef.current = false
    }

    // Deprecated: setLastEventCount removed
  }, [setIsStreaming, activeTab])

  // Store execution options for use in the request
  const executionOptionsRef = useRef<ExecutionOptions | undefined>(undefined)

  // Guard: prevent duplicate submission of the same message from any source
  // (Enter key repeat, form submit/click overlap, restore-triggered rerender races, etc.).
  const submitGuardRef = useRef<{ key: string; expiresAt: number } | null>(null)

  // Helper: reset streaming state (replaces 4 duplicated blocks)
  const resetStreamingState = useCallback((tabId?: string) => {
    const store = useChatStore.getState()
    store.setIsStreaming(false)
    store.setHasActiveChat(false)
    if (tabId) store.setTabStreaming(tabId, false)
  }, [])

  // Wrapper function to submit query with the current local query
  const submitQueryWithQuery = useCallback(async (query: string, executionOptions?: ExecutionOptions, options?: { isAutoNotification?: boolean }) => {
    // Mark that user has interacted — enables auto-notifications
    // (prevents stale notifications from SSE backfill on page load)
    hasUserSentMessageRef.current = true

    const trimmedQuery = query?.trim() || ''
    const activeTabModeCategory =
      activeTab?.metadata?.mode === 'workflow' || activeTab?.metadata?.mode === 'multi-agent'
        ? activeTab.metadata.mode
        : null
    const submitModeCategory = activeTabModeCategory ?? selectedModeCategory
    const submitAgentMode = submitModeCategory
      ? getAgentModeFromCategory(submitModeCategory) as AgentMode
      : correctAgentMode
    const activeTabKey = activeTab?.tabId || 'no-tab'
    const submitGuardKey = `${submitModeCategory || 'unknown'}:${activeTabKey}:${trimmedQuery}`
    const now = Date.now()
    const activeGuard = submitGuardRef.current
    if (activeGuard && activeGuard.key === submitGuardKey && activeGuard.expiresAt > now) {
      console.warn('[ChatArea] Blocked duplicate submitQueryWithQuery call', { query: trimmedQuery.substring(0, 50) })
      return
    }
    submitGuardRef.current = { key: submitGuardKey, expiresAt: now + 3000 }
    setTimeout(() => {
      if (submitGuardRef.current?.key === submitGuardKey) {
        submitGuardRef.current = null
      }
    }, 3000)

    console.log('[ChatArea] submitQueryWithQuery called', { query: trimmedQuery.substring(0, 80), stack: new Error().stack?.split('\n').slice(1, 4).join(' <- ') })

    // Get fresh tab state from store to avoid stale closure issues
    const chatStore = useChatStore.getState()
    const freshActiveTab = activeTab?.tabId ? chatStore.chatTabs[activeTab.tabId] : activeTab

    // Early validation
    if (!trimmedQuery) {
      logger.warn('ChatArea', 'Empty query, returning early')
      return
    }

    if (submitModeCategory === 'workflow' && !isRequiredFolderSelected) {
      logger.error('ChatArea', 'Workflow folder required for workflow mode')
      return
    }

    // Resolve or create tab
    const resolved = await resolveOrCreateTab({ freshActiveTab, selectedModeCategory: submitModeCategory })
    if (!resolved) return
    let { tab: currentTab, sessionId: tabSessionId } = resolved

    const pendingRestoredConversationPath = currentTab.config?.restoredConversationPath?.trim() || ''
    const hasLocalSessionEvents = chatStore.getTabEvents(tabSessionId).length > 0
    const matchingActiveSession = chatStore.activeSessionsCache.find(session => session.session_id === tabSessionId)
    const shouldStartFreshEmptySession =
      !options?.isAutoNotification &&
      !pendingRestoredConversationPath &&
      !hasLocalSessionEvents &&
      !matchingActiveSession

    if (shouldStartFreshEmptySession) {
      const freshSessionId = globalThis.crypto.randomUUID()
      chatStore.updateTabSessionId(currentTab.tabId, freshSessionId)
      currentTab = {
        ...currentTab,
        sessionId: freshSessionId,
      }
      tabSessionId = freshSessionId
      logger.debug('ChatArea', `Rotated empty tab ${currentTab.tabId} to fresh session ${freshSessionId}`)
    }

    const effectiveExecutionOptions = executionOptions ?? (
      submitModeCategory === 'workflow' && currentTab?.metadata?.phaseId
        ? buildExecutionOptions()
        : undefined
    )
    executionOptionsRef.current = effectiveExecutionOptions

    if (
      submitModeCategory === 'workflow' &&
      !options?.isAutoNotification &&
      currentTab?.metadata?.phaseId &&
      isChatCompatiblePhase(currentTab.metadata.phaseId)
    ) {
      window.dispatchEvent(new CustomEvent('workflow-chat-user-started', {
        detail: {
          tabId: currentTab.tabId,
          presetQueryId: currentTab.metadata?.presetQueryId,
          phaseId: currentTab.metadata?.phaseId,
        },
      }))
    }

    // Build file context — read preset fresh from store to avoid stale closure
    // when switching between workflows (the closure's activeWorkflowPreset may lag behind)
    const freshWorkflowPreset = (submitModeCategory === 'workflow')
      ? useGlobalPresetStore.getState().getActivePreset('workflow')
      : null
    // Only include visible/removable file context from tab config. Workflow execution
    // folders still travel through workflow_context_paths; restored conversation files
    // use restoredConversationPath so coding agents can native-resume without a visible
    // file chip.
    let effectiveFileContext: Array<{ name: string; path: string; type: 'file' | 'folder' }> = []
    if ((submitModeCategory === 'multi-agent' || submitModeCategory === 'workflow') && currentTab?.config) {
      effectiveFileContext = currentTab.config.fileContext
    }

    const shouldResumeRestoredConversation =
      submitModeCategory === 'multi-agent' ||
      (
        submitModeCategory === 'workflow' &&
        !!currentTab?.metadata?.phaseId &&
        isChatCompatiblePhase(currentTab.metadata.phaseId)
      )
    const storedRestoredConversationPath = shouldResumeRestoredConversation
      ? currentTab?.config?.restoredConversationPath?.trim()
      : ''
    const restoredConversationPath = storedRestoredConversationPath || ''
    const restoredConversationUsesNative = restoredConversationPath
      ? currentTab?.config?.restoredConversationNativeResume === true
      : false
    const restoredConversationSummary = currentTab?.config?.restoredConversationSummary?.trim()
    const restoredConversationHasVisibleFallback = restoredConversationPath
      ? effectiveFileContext.some((file) => file.path === restoredConversationPath)
      : false
    const restoredConversationContext = restoredConversationPath && restoredConversationHasVisibleFallback
      ? `\n\nPrevious workflow-builder conversation file: ${restoredConversationPath}\nThis file is JSON with a top-level conversation_history array. User messages have Role \"human\" or \"user\" and text in Parts[].Text; assistant replies have Role \"ai\" or \"assistant\"; tool calls/results may be interleaved and are usually noisy. To understand the recent context, scan conversation_history from the end for the latest user/assistant Text parts. Do not treat the last JSON entry as the last user request, because it may be a tool result or function call.${restoredConversationSummary ? `\n\n${restoredConversationSummary}` : ''}`
      : ''
    const fileContextForPrompt = restoredConversationPath
      ? effectiveFileContext.filter((file) => file.path !== restoredConversationPath)
      : effectiveFileContext

    const queryBaseWithContext = fileContextForPrompt.length > 0
      ? `${query.trim()}\n\n📁 Files in context: ${fileContextForPrompt.map((file: { path: string }) => file.path).join(', ')}`
      : query.trim()
    const displayQueryWithContext = queryBaseWithContext
    const queryWithContext = `${displayQueryWithContext}${restoredConversationContext}`

    if (restoredConversationUsesNative) {
      chatStore.setTabViewMode(currentTab.tabId, 'terminal')
    }

    // Decrypt selected secrets for payload (passed separately, never in query text)
      // Merge secrets from tab config (multi-agent) and workflow preset
    let decryptedSecrets: Array<{ name: string; value: string }> | undefined
    const tabSecretIds = currentTab?.config?.selectedSecrets || []
    const presetSecretIds = (submitModeCategory === 'workflow' && freshWorkflowPreset)
      ? ((freshWorkflowPreset as CustomPreset).selectedSecrets || [])
      : []
    const selectedSecretIds = [...new Set([...tabSecretIds, ...presetSecretIds])]
    if (selectedSecretIds.length > 0) {
      try {
        const secretsStore = useSecretsStore.getState()
        const secretsToInject = selectedSecretIds
          .map(id => secretsStore.getSecret(id))
          .filter((s): s is NonNullable<typeof s> => !!s)

        if (secretsToInject.length > 0) {
          decryptedSecrets = await Promise.all(
            secretsToInject.map(async (s) => {
              const { value } = await secretsApi.decrypt(s.encryptedValue)
              return { name: s.name, value }
            })
          )
        }
      } catch (err) {
        logger.error('ChatArea', 'Failed to decrypt secrets:', err)
      }
    }

    if (submitModeCategory === 'workflow') {
      useAppStore.getState().setCurrentQuery(displayQueryWithContext)
    }

    // Restored chats should resume naturally in the same session.
    // Only seed an optimistic event_index when the restored history already has backend
    // indices. Mixing "history without indices" and "optimistic message with index 0"
    // creates inconsistent ordering metadata and can make the first follow-up jump around.
    const existingSessionEvents = chatStore.getTabEvents(tabSessionId)
    const indexedEvents = existingSessionEvents.filter((event) => typeof event.event_index === 'number')
    const nextEventIndex = indexedEvents.length > 0
      ? indexedEvents.reduce((maxIndex, event) => Math.max(maxIndex, event.event_index as number), -1) + 1
      : undefined
    const latestExistingTimestampMs = existingSessionEvents.reduce((latest, event) => {
      const ts = getEventTimestampMs(event)
      return ts === null ? latest : Math.max(latest, ts)
    }, 0)
    const optimisticTimestampMs = Math.max(Date.now(), latestExistingTimestampMs + 1)
    const optimisticUserMessage = createUserMessageEvent(
      displayQueryWithContext,
      nextEventIndex,
      new Date(optimisticTimestampMs).toISOString()
    )
    chatStore.addTabEvents(tabSessionId, [optimisticUserMessage])

    // File context is one-shot: it belongs to the message being submitted, not
    // the whole conversation. The request payload below already captured it.
    if (effectiveFileContext.length > 0 || restoredConversationPath) {
      chatStore.setTabConfig(currentTab.tabId, {
        fileContext: [],
        restoredConversationPath: undefined,
        restoredConversationSummary: undefined,
        restoredConversationTitle: undefined,
        restoredConversationWorkshopModeLabel: undefined,
        restoredConversationRuntimeLabel: undefined,
        restoredConversationNativeResume: undefined,
      })
    }

    // Enable auto-scroll and scroll to bottom
    chatStore.setAutoScroll(true)
    setTimeout(() => { scrollToBottom('smooth') }, 50)

    // Clear query text
    useAppStore.getState().setCurrentQuery('')

    // Preserve final response as completion event if needed
    const eventsToCheck = chatStore.getTabEvents(tabSessionId)
    const hasCompletionEvent = eventsToCheck.some(event =>
      event.type === 'unified_completion' || event.type === 'agent_end'
    )
    if (finalResponse && !hasCompletionEvent) {
      const completionEvent: PollingEvent = {
        id: `completion-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`,
        type: 'unified_completion',
        timestamp: new Date().toISOString(),
        data: {
          unified_completion: {
            content: finalResponse,
            timestamp: new Date().toISOString()
          }
        } as PollingEvent['data']
      }
      chatStore.addTabEvents(tabSessionId, [completionEvent])
    }

    // Reset UI state for new query
    chatStore.setFinalResponse('')
    chatStore.setIsCompleted(false)
    chatStore.setIsStreaming(true)
    chatStore.setHasActiveChat(true)
    chatStore.setTabCompleted(currentTab.tabId, false)
    chatStore.setTabStreaming(currentTab.tabId, true)
    requestTerminalRefreshBurst()

    // Reset lastEventIndex so polling starts fresh from the in-memory event store
    // (critical when continuing a restored session — DB events have different indices than in-memory)
    chatStore.setTabLastEventIndex(tabSessionId, -1)

    // SSE connection is established in connectAfterRefresh below (after getActiveSessions)
    // Polling is only used as a fallback if SSE fails (handled by connectSSE's onError)

    processedCompletionEventsRef.current.clear()

    try {
      // Get active presets for the current mode
      const presetStore = useGlobalPresetStore.getState()
      const chatPreset = submitAgentMode === 'multi-agent' ? presetStore.getActivePreset('multi-agent') : null
      // Read workflow preset fresh from store (not from stale closure)
      // For workflow mode, always try to get the active preset regardless of selectedWorkflowPreset closure value
      const workflowPreset = (submitAgentMode === 'workflow' || submitModeCategory === 'workflow')
        ? presetStore.getActivePreset('workflow')
        : null
      const activePreset = workflowPreset || chatPreset

      const presetTools = activePreset?.selectedTools || []
      const filteredPresetTools = presetTools.filter(t => !t.endsWith(':*'))

      const chatPresetId = chatPreset?.id || null
      const workflowPresetId = workflowPreset?.id || null

      // Determine mode flags using helper
      const useCodeExecutionMode = determineModeFlag({
        correctAgentMode: submitAgentMode,
        selectedModeCategory: submitModeCategory || '',
        presetValue: activePreset?.useCodeExecutionMode,
        tabConfigValue: currentTab?.config?.useCodeExecutionMode,
      })
      // Build LLM config
      const isMultiAgentMode = submitModeCategory === 'multi-agent'
      const llmStore = useLLMStore.getState()
      // For multi-agent and workflow phase chat: use tab's LLM if set (user may override)
      const isWorkflowPhaseChat = submitModeCategory === 'workflow'
        && currentTab?.metadata?.phaseId
        && isChatCompatiblePhase(currentTab.metadata.phaseId)
      // For phase chat: prefer preset LLM if user hasn't explicitly overridden
      // (tab config always has a default from workflowPrimaryConfig, so we also check the preset)
      const phaseChatPreset = isWorkflowPhaseChat
        ? (presetStore.getActivePreset('workflow'))
        : null
      const presetBuilderLLM = phaseChatPreset?.llmConfig?.builder_llm
      const presetLLMConfig = presetBuilderLLM?.provider && presetBuilderLLM?.model_id
        ? { provider: presetBuilderLLM.provider, model_id: presetBuilderLLM.model_id, options: presetBuilderLLM.options }
        : null
      const baseLLMConfig = isWorkflowPhaseChat
        ? (currentTab?.config?.llmConfig || presetLLMConfig || llmStore.primaryConfig)
        : (isMultiAgentMode && currentTab?.config?.llmConfig)
          ? currentTab.config.llmConfig
          : llmStore.primaryConfig
      const tierConfig = llmStore.delegationTierConfig
      const effectiveLLMConfig: ExtendedLLMConfiguration = (isMultiAgentMode && tierConfig?.main?.provider && tierConfig?.main?.model_id)
        ? { ...baseLLMConfig, provider: tierConfig.main.provider as ExtendedLLMConfiguration['provider'], model_id: tierConfig.main.model_id }
        : baseLLMConfig

      const llmConfigWithApiKeys = buildLLMConfigWithApiKeys(effectiveLLMConfig)

      // DEBUG: browser config from current tab before payload build
      console.log('[DEBUG browser tab config]', {
        tabId: currentTab?.tabId,
        modeCategory: submitModeCategory,
        browserMode: currentTab?.config?.browserMode,
        enableBrowserAccess: currentTab?.config?.enableBrowserAccess,
        useCdp: currentTab?.config?.useCdp,
        cdpPort: currentTab?.config?.cdpPort,
        selectedServers: currentTab?.config?.selectedServers,
      })

      // Build request payload
      const requestPayload = buildQueryRequestPayload({
        queryWithContext,
        correctAgentMode: submitAgentMode,
        selectedModeCategory: submitModeCategory,
        enabledTools,
        effectiveServers,
        currentTab,
        effectiveLLMConfig,
        llmConfigWithApiKeys,
        useCodeExecutionMode,
        executionOptions: executionOptionsRef.current,
        workflowPresetId,
        chatPresetId,
        filteredPresetTools,
        hasActivePreset: !!activePreset,
        decryptedSecrets,
        selectedGlobalSecrets: activePreset?.selectedGlobalSecretNames ?? undefined,
        restoredConversationPath,
      })

      // Validate execution groups for workflow mode
      const executionPhaseId = currentTab?.metadata?.phaseId
      const requiresGroupValidation = executionPhaseId !== 'evaluation-execution' && executionPhaseId !== 'report-execution'

      if (submitAgentMode === 'workflow' && requestPayload.execution_options && !isWorkflowPhaseChat && requiresGroupValidation) {
        const validationError = validateExecutionGroups(requestPayload.execution_options)
        if (validationError) {
          chatStore.addToast(validationError, 'warning')
          resetStreamingState(currentTab.tabId)
          return
        }
      }

      // DEBUG: log final request payload preset_query_id
      console.log('[DEBUG request payload]', {
        agent_mode: requestPayload.agent_mode,
        preset_query_id: requestPayload.preset_query_id,
        phase_id: (requestPayload as any).phase_id,
        has_files_in_context: requestPayload.query.includes('📁 Files in context:'),
        restored_conversation_path: restoredConversationPath || undefined,
        enable_browser_access: requestPayload.enable_browser_access,
        browser_mode: requestPayload.browser_mode,
        cdp_port: requestPayload.cdp_port,
        enabled_servers: requestPayload.enabled_servers,
      })

      // Mark auto-notification requests so backend treats them as synthetic turns
      if (options?.isAutoNotification) {
        requestPayload.is_auto_notification = true
      }

      // Set session ID and submit
      chatStore.setSessionId(tabSessionId)
      console.log('[WF_DEBUG] 1. Submitting', { tabId: currentTab.tabId, tabSessionId, eventCount: chatStore.getTabEvents(tabSessionId).length, mode: currentTab.metadata?.mode })
      const response = await agentApi.startQuery(requestPayload, tabSessionId)
      console.log('[WF_DEBUG] 2. Response', { status: response.status, responseSessionId: response.session_id || response.query_id, tabSessionId, match: (response.session_id || response.query_id) === tabSessionId })

      if (response.status === 'started' || response.status === 'workflow_started') {
        const responseSessionId = response.session_id || response.query_id
        if (!responseSessionId) {
          console.log('[WF_DEBUG] ERROR: No sessionId in response')
          logger.error('ChatArea', 'No sessionId in response')
          resetStreamingState(currentTab.tabId)
          return
        }

        console.log('[WF_DEBUG] 3. Before updateTabSessionId', { old: tabSessionId, new: responseSessionId, changed: responseSessionId !== tabSessionId, oldEvents: chatStore.getTabEvents(tabSessionId).length, newEvents: chatStore.getTabEvents(responseSessionId).length })
        chatStore.setSessionId(responseSessionId)
        chatStore.updateTabSessionId(currentTab.tabId, responseSessionId)
        console.log('[WF_DEBUG] 4. After updateTabSessionId', { events: chatStore.getTabEvents(responseSessionId).length, activeTabSession: useChatStore.getState().chatTabs[currentTab.tabId]?.sessionId })
        chatStore.setTabStreaming(currentTab.tabId, true)
        chatStore.setTabCompleted(currentTab.tabId, false)
        requestTerminalRefreshBurst()

        // Reactivate historical session if needed
        const currentSessionState = useChatStore.getState().sessionState
        if (currentSessionState === 'completed' || currentSessionState === 'error') {
          chatStore.setSessionState('active')
        }

        // Refresh active sessions cache — SSE connection useEffect will pick up the new session
        const connectAfterRefresh = () => {
          const store = useChatStore.getState()
          const sid = responseSessionId
          console.log('[WF_DEBUG] 5. connectAfterRefresh', { sid, hasSSE: !!store.sseConnections[sid], events: store.tabEvents[sid]?.length ?? 0, sinceIndex: store.tabEventIndices[sid] })
          // Connect SSE for the new session immediately
          if (!store.sseConnections[sid]) {
            connectSSE(
              sid,
              (msg: SSEEventMessage) => handleSSEMessage(msg, sid),
              (msg: SSEStatusMessage) => handleSSEStatus(msg, sid),
              () => handleSSEFallback(sid)
            )
          }
        }

        getActiveSessions(true)
          .then(connectAfterRefresh)
          .catch(error => {
            logger.error('ChatArea', 'Failed to refresh active sessions cache:', error)
            connectAfterRefresh()
          })
      } else if (response.status === 'live_input_delivered') {
        // Single-entry routing (tmux-transport CLI): the backend steered this
        // message into the already-running coding-agent turn instead of starting a
        // new one. Keep the turn streaming — the optimistic user bubble already shows
        // the message (the backend-recorded echo is deduped by exact content), and
        // the running turn's SSE carries the agent output until its completion event
        // clears the spinner. Do NOT resetStreamingState here (that would stop the
        // spinner mid-turn).
        const sid = response.session_id || tabSessionId
        chatStore.setTabStreaming(currentTab.tabId, true)
        chatStore.setTabCompleted(currentTab.tabId, false)
        requestTerminalRefreshBurst()
        if (sid && !useChatStore.getState().sseConnections[sid]) {
          connectSSE(
            sid,
            (msg: SSEEventMessage) => handleSSEMessage(msg, sid),
            (msg: SSEStatusMessage) => handleSSEStatus(msg, sid),
            () => handleSSEFallback(sid)
          )
        }
      } else {
        console.log('[WF_DEBUG] ERROR: Backend non-started response', { status: response.status, message: response.message, response })
        logger.error('ChatArea', 'Backend error:', response)
        resetStreamingState(currentTab.tabId)
      }
    } catch (error) {
      console.log('[WF_DEBUG] ERROR: Submit exception', { error })
      logger.error('ChatArea', 'Failed to submit query:', error)
      resetStreamingState(currentTab.tabId)
    }

  }, [correctAgentMode, selectedModeCategory, getAgentModeFromCategory, isRequiredFolderSelected, isStreaming, stopStreaming, finalResponse, startPolling, effectiveServers, enabledTools, selectedWorkflowPreset, activeWorkflowPreset, pollEvents, processedCompletionEventsRef, activeTab, scrollToBottom, getActiveSessions, resetStreamingState, connectSSE, handleSSEMessage, handleSSEStatus])

  // If the active tab is stuck in streaming state, ChatInput queues the user's text
  // instead of calling /api/query. Force-refresh active sessions so the store can
  // clear stale streaming state and let the queue flush as the next turn.
  useEffect(() => {
    const queuedCount = activeTab?.config?.queuedMessages?.length ?? 0
    if (!activeTab?.isStreaming || !activeTab.sessionId || queuedCount === 0) return

    const streamingAge = activeTab.lastStreamingStartedAt
      ? Date.now() - activeTab.lastStreamingStartedAt
      : Number.POSITIVE_INFINITY
    const delay = Number.isFinite(streamingAge)
      ? Math.max(750, STALE_STREAMING_RECOVERY_GRACE_MS - streamingAge + 250)
      : 750

    const timeout = window.setTimeout(() => {
      getActiveSessions(true).catch(error => {
        logger.warn('ChatArea', 'Failed to refresh active sessions for queued-message recovery:', error)
      })
    }, delay)

    return () => window.clearTimeout(timeout)
  }, [
    activeTab?.tabId,
    activeTab?.sessionId,
    activeTab?.isStreaming,
    activeTab?.lastStreamingStartedAt,
    activeTab?.config?.queuedMessages?.length,
    getActiveSessions,
  ])

  // Auto-send queued messages when agent is idle (not streaming)
  const submitQueryWithQueryRef = useRef(submitQueryWithQuery)
  useEffect(() => { submitQueryWithQueryRef.current = submitQueryWithQuery }, [submitQueryWithQuery])

  useEffect(() => {
    const currentIsStreaming = activeTab?.isStreaming ?? false
    const queuedMessages = activeTab?.config?.queuedMessages || []

    // Read the shared lock from the store (fresh, not from closure) to prevent
    // multiple ChatArea instances from double-processing the same queue.
    const freshConfig = activeTab ? useChatStore.getState().getTabConfig(activeTab.tabId) : undefined
    const isProcessing = freshConfig?.isQueueProcessing ?? false

    // Process queued messages when agent is idle (not streaming).
    // Uses !isStreaming instead of isCompleted because workshop step goroutines
    // may still be running in the background after the main agent turn finishes.
    if (currentIsStreaming || !activeTab || isProcessing || queuedMessages.length === 0) {
      if (queuedMessages.length > 0) {
        console.log(`[QUEUE_DEBUG] Not processing: isStreaming=${currentIsStreaming} hasTab=${!!activeTab} isProcessing=${isProcessing} queueLen=${queuedMessages.length}`)
        // SAFETY: If lock is stuck (isProcessing=true) for more than 10 seconds, force-release it.
        // This can happen if submitQuery promise never resolves or the finally block doesn't run.
        if (isProcessing && !currentIsStreaming && activeTab) {
          const lockKey = `queue_lock_${activeTab.tabId}`
          const lockStore = window as unknown as Record<string, unknown>
          const lastLockTime = lockStore[lockKey] as number | undefined
          if (!lastLockTime) {
            lockStore[lockKey] = Date.now()
          } else if (Date.now() - lastLockTime > 10000) {
            console.warn(`[QUEUE_DEBUG] Force-releasing stuck lock after 10s for tab ${activeTab.tabId}`)
            useChatStore.getState().setTabConfig(activeTab.tabId, { isQueueProcessing: false })
            delete lockStore[lockKey]
          }
        }
      }
      return
    }

    const tabId = activeTab.tabId
    const chatStore = useChatStore.getState()

    // Claim the store-level lock atomically before any async work.
    chatStore.setTabConfig(tabId, { isQueueProcessing: true })
    // Clear stuck-lock tracker
    const lockStore = window as unknown as Record<string, unknown>
    delete lockStore[`queue_lock_${tabId}`]

    // Separate human messages from auto-notifications
    const humanMessages = queuedMessages.filter(m => !m.startsWith(AUTO_NOTIFICATION_PREFIX))
    const autoMessages = queuedMessages.filter(m => m.startsWith(AUTO_NOTIFICATION_PREFIX))
    const freshAutoMessages = autoMessages.filter(m => !isStaleQueuedAutoNotification(m))
    const droppedAutoCount = autoMessages.length - freshAutoMessages.length

    // Human messages: combine all as-is
    // Auto-notifications: if multiple, condense to first line of each to avoid overwhelming the agent
    let combinedMessage: string
    const parts: string[] = []
    if (humanMessages.length > 0) {
      parts.push(humanMessages.map(m => m.trim()).join('\n\n'))
    }
    if (freshAutoMessages.length > 0) {
      if (freshAutoMessages.length === 1) {
        parts.push(freshAutoMessages[0].trim())
      } else {
        // Multiple auto-notifications: take first line of each and combine into a compact summary
        const summaryLines = freshAutoMessages.map(m => {
          const firstLine = m.trim().split('\n')[0]
          return firstLine
        })
        parts.push(`${AUTO_NOTIFICATION_PREFIX} Multiple step completions:\n${summaryLines.map(l => l.replace(AUTO_NOTIFICATION_PREFIX, '').trim()).map(l => `- ${l}`).join('\n')}`)
      }
    }
    combinedMessage = parts.join('\n\n')

    // Clear the entire queue
    chatStore.setTabConfig(tabId, { queuedMessages: [] })

    // Small delay to ensure state is fully processed before sending
    setTimeout(async () => {
      try {
        if (droppedAutoCount > 0) {
          console.log('[QUEUE_DEBUG] Dropped stale auto-notifications before submit', { droppedAutoCount, tabId })
        }

        if (!combinedMessage.trim()) {
          return
        }

        const isAutoOnly = humanMessages.length === 0 && freshAutoMessages.length > 0
        await submitQueryWithQueryRef.current(combinedMessage, undefined, { isAutoNotification: isAutoOnly })
      } catch (error) {
        logger.error('ChatArea', 'Failed to send queued messages:', error)
        // Re-add all messages back to the queue
        const currentChatStore = useChatStore.getState()
        const currentQueue = currentChatStore.getTabConfig(tabId)?.queuedMessages || []
        currentChatStore.setTabConfig(tabId, {
          queuedMessages: [...queuedMessages, ...currentQueue]
        })
        addToast('Failed to send queued messages. They have been re-queued.', 'error')
      } finally {
        // Release the lock after a delay to allow the new session to start streaming
        setTimeout(() => {
          useChatStore.getState().setTabConfig(tabId, { isQueueProcessing: false })
        }, 500)
      }
    }, 200)
  }, [activeTab?.isStreaming, activeTab?.config?.queuedMessages, activeTab?.config?.isQueueProcessing, activeTab?.tabId])

  // Handle new chat for the active tab. Keep this scoped: workflow and
  // multi-agent tabs can coexist, so starting a fresh conversation in one tab
  // must not clear every tab/event/SSE connection in the app.
  const handleNewChat = useCallback(async () => {
    const chatStore = useChatStore.getState()
    // Stop the previous backend session first (if it exists). This closes any
    // tmux-backed CLI owner before the tab rotates to a fresh Chief of Staff
    // session, preventing two pi-cli sessions from sharing the Chats cwd.
    const currentSessionId = getSessionId()
    const sessionIdToClear = activeTab?.sessionId || currentSessionId
    if (sessionIdToClear) {
      try {
        const activeSessions = await getActiveSessions(true)
        const backendKnowsSession = activeSessions.some(session => session.session_id === sessionIdToClear)
        if (backendKnowsSession) {
          await agentApi.stopSession(sessionIdToClear, true)
        }
      } catch (error) {
        logger.error('ChatArea', 'Failed to stop previous session:', error)
        // Continue with frontend reset even if backend stop fails.
      }
    }
    
    // For workflow mode, preserve the selected preset but reset workflow phase
    if (selectedModeCategory === 'workflow' && selectedWorkflowPreset) {
      // Keep the preset selected, just reset the workflow phase to default
      const defaultPhase = useWorkflowStore.getState().getDefaultPhase()
      setCurrentWorkflowPhase(defaultPhase)
      // Don't clear selectedWorkflowPreset or currentWorkflowQueryId
    } else {
      // For other modes, clear workflow state completely
      clearWorkflowState()
    }
    
    if (activeTab) {
      chatStore.resetTabChat(activeTab.tabId)
      chatStore.setTabConfig(activeTab.tabId, {
        queuedMessages: [],
        isQueueProcessing: false,
      })
    } else {
      // Legacy fallback for the rare case where New Chat is triggered before
      // a tab exists. Normal tabbed chats use resetTabChat above.
      resetChatState()
      resetSessionId()
      onNewChat()
    }

    notifiedWorkshopAgentsRef.current.clear()
    
    // Explicitly reset events and tracking for new chat
    // Note: Using tabEvents now, not global events
    // Events are cleared when tab is removed/cleared
    setLastEventIndex(-1)
    processedCompletionEventsRef.current.clear()
    
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [clearWorkflowState, resetChatState, onNewChat, activeTab?.sessionId, activeTab?.tabId, selectedModeCategory, selectedWorkflowPreset, setCurrentWorkflowPhase, setLastEventIndex, getActiveSessions])

  // Refresh workflow presets function
  const refreshWorkflowPresets = useCallback(async () => {
    if (workflowModeHandlerRef.current) {
      await workflowModeHandlerRef.current.refreshPresets()
    }
  }, [])

  // Expose methods to parent component
  useImperativeHandle(ref, () => ({
    handleNewChat,
    resetChatState,
    refreshWorkflowPresets,
    submitQuery: submitQueryWithQuery,
    getEvents: () => displayEvents,
    isStreaming,
    currentWorkflowPhase
  }), [handleNewChat, resetChatState, refreshWorkflowPresets, submitQueryWithQuery, displayEvents, isStreaming, currentWorkflowPhase])

  // Single source of truth for "which surface shows" — shared by BOTH the
  // multi-agent and workflow render branches (see ./resolveChatSurface). The two
  // branches derive `hasContent` differently (mirroring today's behavior):
  // multi-agent keys off conversation events, workflow off any display event.
  // isStreaming below is the ACTIVE TAB's flag (activeTabStreaming), NOT the
  // global state.isStreaming. The global flag is a cross-tab signal that lingers
  // true after New Chat from a running conversation (resetTabChat rotates the
  // sessionId + clears the per-tab flag but does not reset the global one), which
  // wrongly kept a fresh New-Chat tab on 'active' (empty terminal) instead of
  // 'landing'. Reading the per-tab flag scopes "is streaming" to THIS session.
  useEffect(() => {
    if (!activeSessionId || !sessionTerminalsFetched) return
    if (activeEventViewMode !== 'terminal') return
    if (hasPendingTerminalActivity || restoredSessionTerminals.length > 0) return

    const restoredPath = activeTab?.config?.restoredConversationPath?.trim() || ''
    const sourceSessionId = activeTab?.config?.restoredConversationNativeResume === true || selectedModeCategory === 'workflow'
      ? activeSessionId
      : ''
    if (!restoredPath && !sourceSessionId) return

    const workspacePath = selectedModeCategory === 'workflow'
      ? activeWorkflowPreset?.selectedFolder?.filepath
      : undefined
    const attemptKey = `${activeSessionId}:${restoredPath || 'session'}:${workspacePath || ''}`
    if (emptyTerminalRestoreAttemptRef.current.has(attemptKey)) return
    emptyTerminalRestoreAttemptRef.current.add(attemptKey)

    startRestoredTransportTerminal(
      activeSessionId,
      restoredPath || undefined,
      sourceSessionId || undefined,
      workspacePath,
    )
  }, [
    activeEventViewMode,
    activeSessionId,
    activeTab?.config?.restoredConversationNativeResume,
    activeTab?.config?.restoredConversationPath,
    activeWorkflowPreset?.selectedFolder?.filepath,
    hasPendingTerminalActivity,
    restoredSessionTerminals.length,
    selectedModeCategory,
    sessionTerminalsFetched,
  ])
  const multiAgentSurface = resolveChatSurface({
    isRestoring: isRestoringChatSessions,
    // The resolver's resume-pending → 'restoring' input is now the SYNCHRONOUS
    // resumePending (computed in render), not the old effect-lagged state.
    resumeSettling: resumePending,
    hasContent: hasConversationContent,
    isStreaming: activeTabStreaming,
    hasRestoredLiveContent,
    isReadOnlyRunView,
  })
  // Workflow shares the resume-pending signals (resumeSettling + the terminal/
  // execution-tree probe) so a coding-agent/terminal resume in the workflow pane
  // stays 'restoring' until its terminal is confirmed — then 'active' — instead of
  // flashing the previous-chats list on the first Resume click. Both inputs are
  // false/gated whenever there's no restoredConversationPath, so a FRESH workflow
  // chat is unaffected (still resolves to 'landing').
  const workflowSurface = resolveChatSurface({
    isRestoring: isRestoringWorkflowSessions,
    resumeSettling: resumePending,
    hasContent: displayEvents.length > 0,
    isStreaming: activeTabStreaming,
    hasRestoredLiveContent,
    isReadOnlyRunView,
  })
  // In workflow terminal mode, old events are not enough to show the terminal
  // surface. Ctrl+K should open an idle automation to its normal landing/history
  // view unless there is a live submitted turn, a running backend session, or an
  // actual terminal snapshot for this resumed session. Restored terminal rows can
  // be stale snapshots when tmux is gone; they still need to render TerminalCenter
  // so Resume never lands on a blank workflow pane.
  const workflowHasTerminalSurface =
    hasPendingTerminalActivity || hasLiveWorkflowTerminal || restoredSessionHasTerminal
  const visibleWorkflowSurface =
    workflowSurface === 'active' &&
    activeEventViewMode === 'terminal' &&
    !workflowHasTerminalSurface
      ? 'landing'
      : workflowSurface

  // Keep the bottom "Resuming coding session" indicator in sync with the surface
  // (both modes). A native/terminal resume that settles onto the previous-chats
  // list (landing) yielded nothing live, so its restore markers are stale: clear
  // them so the indicator disappears and typing starts a fresh chat instead of
  // silently resuming a chat you're no longer viewing. (File-fallback resumes —
  // NativeResume false — stay: their attached file context still drives the next
  // turn. Read-only run views never reach landing, so they're untouched.)
  const activeChatSurface = selectedModeCategory === 'workflow' ? visibleWorkflowSurface : multiAgentSurface
  useEffect(() => {
    if (activeChatSurface !== 'landing') return
    const tabId = activeTab?.tabId
    const cfg = activeTab?.config
    if (!tabId || cfg?.restoredConversationNativeResume !== true) return
    // HARD GUARD: never clear the restore markers while a resume could still be
    // pending — that is exactly what canceled resumes mid-flight (the 2-3-click
    // bug). A native resume only legitimately reaches 'landing' once the give-up
    // timer has elapsed with no terminal and no content (resumeGaveUp). Until then
    // restoredConversationPath stays in place so the synchronous resumePending keeps
    // the surface on 'restoring'. When in doubt, do NOT clear.
    if (!resumeGaveUp) return
    const restoredPath = cfg.restoredConversationPath?.trim()
    if (!restoredPath) return
    useChatStore.getState().setTabConfig(tabId, {
      restoredConversationPath: undefined,
      restoredConversationSummary: undefined,
      restoredConversationTitle: undefined,
      restoredConversationWorkshopModeLabel: undefined,
      restoredConversationRuntimeLabel: undefined,
      restoredConversationNativeResume: undefined,
      fileContext: (cfg.fileContext || []).filter(item => item.path !== restoredPath),
    })
  }, [activeChatSurface, activeTab?.tabId, activeTab?.config, resumeGaveUp])

  // Multi-agent landing surface = the previous-chats panel (mirrors the old
  // showNormalPreviousChatsPanel).
  const showNormalPreviousChatsPanel = selectedModeCategory === 'multi-agent' && multiAgentSurface === 'landing'
  // Workflow landing panel: WorkflowLayout passes the rendered node only when a
  // fresh automation chat should show the previous-chats list. When present we
  // make it the primary surface (mirrors the multi-agent landing panel) and
  // suppress the workflow empty states / terminal / event display below it.
  const showWorkflowPreviousChatsPanel =
    selectedModeCategory === 'workflow' &&
    visibleWorkflowSurface === 'landing' &&
    !!workflowPreviousChatsPanel
  // Layout: the terminal pane is full-height unless the multi-agent landing list
  // is covering it; the two landing panels are also full-height. (Preserves the
  // original shouldRenderTerminalPane / shouldUseFullHeightContent formulas.)
  const shouldRenderTerminalPane =
    activeEventViewMode === 'terminal' &&
    !showNormalPreviousChatsPanel &&
    !(selectedModeCategory === 'workflow' && visibleWorkflowSurface === 'landing')
  const shouldUseFullHeightContent = shouldRenderTerminalPane || showNormalPreviousChatsPanel || showWorkflowPreviousChatsPanel

  return (
    <div className="flex flex-col h-full min-w-0" data-testid="chat-area-container">
      {/* Preset Selection Overlay */}
      {showPresetSelection && pendingModeCategory && (
        <PresetSelectionOverlay
          isOpen={showPresetSelection}
          onClose={handlePresetSelectionClose}
          onPresetSelected={handlePresetSelected}
          modeCategory={pendingModeCategory}
          setCurrentQuery={setCurrentQuery}
        />
      )}

      {/* Mode Switch Dialog */}
      {showModeSwitchDialog && pendingModeSwitch && (
        <ModeSwitchDialog
          isOpen={showModeSwitchDialog}
          onCancel={handleModeSwitchCancel}
          onConfirm={handleModeSwitchConfirm}
          currentModeCategory={selectedModeCategory}
          newModeCategory={pendingModeSwitch}
        />
      )}



      {/* Chat Content - Separated to prevent input re-renders.
          In terminal mode the inner pane owns its own scrolling
          (the rail + log scroll independently), so this wrapper
          must NOT scroll — otherwise the whole page scrolls
          around the fixed-height terminal box. */}
      <div ref={chatContentRef} className={`flex-1 ${shouldUseFullHeightContent ? 'overflow-hidden' : 'overflow-y-auto'} overflow-x-hidden min-w-0 relative overscroll-y-none ${compact ? 'text-sm' : ''}`} style={{ scrollBehavior: 'auto' }}>
        
        <div className={`min-w-0 ${shouldUseFullHeightContent ? 'flex h-full flex-col' : 'min-h-full'} ${shouldRenderTerminalPane ? '' : (compact ? 'px-2 pb-2' : 'px-3 pb-4')}`}>
          {/* Loading indicator for historical events */}
          {isLoadingHistory && (
            <div className={`flex items-center justify-center ${compact ? 'py-4' : 'py-8'}`}>
              <div className="flex items-center gap-3 text-gray-600 dark:text-gray-400">
                <div className={`${compact ? 'w-4 h-4' : 'w-5 h-5'} border-2 border-gray-300 dark:border-gray-600 border-t-blue-600 dark:border-t-blue-400 rounded-full animate-spin`}></div>
                <span className={compact ? 'text-xs' : 'text-sm'}>Loading chat history...</span>
              </div>
            </div>
          )}

          {/* Loading indicator for active session checking */}
          {isCheckingActiveSessions && (
            <div className={`flex items-center justify-center ${compact ? 'py-4' : 'py-8'}`}>
              <div className="flex items-center gap-3 text-gray-600 dark:text-gray-400">
                <div className={`${compact ? 'w-4 h-4' : 'w-5 h-5'} border-2 border-gray-300 dark:border-gray-600 border-t-green-600 dark:border-t-green-400 rounded-full animate-spin`}></div>
                <span className={compact ? 'text-xs' : 'text-sm'}>Checking for active session...</span>
              </div>
            </div>
          )}

          {/* Active session indicator */}
          {sessionState === 'active' && (
            <div className={`flex items-center justify-center ${compact ? 'py-2' : 'py-4'}`}>
              <div className={`flex items-center gap-2 ${compact ? 'px-2 py-1' : 'px-3 py-2'} bg-green-100 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-lg`}>
                <div className={`${compact ? 'w-1.5 h-1.5' : 'w-2 h-2'} bg-green-500 rounded-full animate-pulse`}></div>
                <span className={`${compact ? 'text-xs' : 'text-sm'} text-green-700 dark:text-green-300 font-medium`}>Live Session - Reconnected</span>
              </div>
            </div>
          )}

          {/* Session error indicator */}
          {sessionState === 'error' && (
            <div className={`flex items-center justify-center ${compact ? 'py-2' : 'py-4'}`}>
              <div className={`flex items-center gap-2 ${compact ? 'px-2 py-1' : 'px-3 py-2'} bg-red-100 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg`}>
                <svg className={`${compact ? 'w-3 h-3' : 'w-4 h-4'} text-red-600 dark:text-red-400`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
                <span className={`${compact ? 'text-xs' : 'text-sm'} text-red-700 dark:text-red-300 font-medium`}>Session Error - Unable to reconnect</span>
              </div>
            </div>
          )}

        {selectedModeCategory === 'workflow' ? (
          <WorkflowModeHandler
            ref={workflowModeHandlerRef}
            onPresetSelected={handleWorkflowPresetSelected}
            onPresetCleared={handleWorkflowPresetCleared}
            onWorkflowPhaseChange={setCurrentWorkflowPhase}
          >
            {/* restoring — reconnectWorkflowTabs is replaying events. */}
            {visibleWorkflowSurface === 'restoring' && (
              <div className="flex flex-col items-center justify-center py-12 gap-3">
                <div className="w-6 h-6 border-2 border-gray-300 dark:border-gray-600 border-t-blue-600 dark:border-t-blue-400 rounded-full animate-spin"></div>
                <p className="text-sm text-gray-500 dark:text-gray-400">Restoring previous session...</p>
              </div>
            )}

            {/* active — terminal-or-events by the view toggle. */}
            {visibleWorkflowSurface === 'active' && activeTab?.sessionId && (
              activeEventViewMode === 'terminal' ? (
                <TerminalCenter currentSessionId={activeTab.sessionId} compact={false} hasPendingTerminalActivity={hasPendingTerminalActivity} />
              ) : (
                <EventDisplay events={displayEvents} executionTree={sessionExecutionTree} onFeedbackSubmitted={handleFeedbackSubmitted} onSendMessage={submitQueryWithQuery} compact={compact} sessionId={activeTab.sessionId} tabId={targetTabId || undefined} />
              )
            )}

            {/* landing — fresh automation chat. Prefer the previous-chats panel
                (WorkflowLayout supplies the node + resume handler so the
                workflow-scoped history logic lives in one place). TerminalCenter
                is intentionally not rendered on landing: "Waiting for terminal"
                is only for an active/pending turn after a message was sent. */}
            {visibleWorkflowSurface === 'landing' && (
              workflowPreviousChatsPanel ?? null
            )}
          </WorkflowModeHandler>
        ) : (
          <>
            {/* restoring — a previous multi-agent session is loading/replaying. */}
            {multiAgentSurface === 'restoring' && (
              <div className="flex flex-col items-center justify-center py-12 gap-3">
                <div className="w-6 h-6 border-2 border-gray-300 dark:border-gray-600 border-t-blue-600 dark:border-t-blue-400 rounded-full animate-spin"></div>
                <p className="text-sm text-gray-500 dark:text-gray-400">Restoring previous session...</p>
              </div>
            )}

            {/* landing — fresh chat / New Chat. The panel renders the list OR its
                own "No previous chats yet." empty, so no separate help page is
                needed here. */}
            {multiAgentSurface === 'landing' && (
              <PreviousChatHistoryPanel
                activeSessionId={hasConversationContent ? activeTab?.sessionId ?? undefined : undefined}
                title="Previous chats"
                actionLabel="Resume"
                emptyText="No previous chats yet."
                onSelectSession={handleResumePreviousChat}
                fill
                compact={previousChatsCompact}
              />
            )}

            {/* active — terminal-or-events by the view toggle. */}
            {multiAgentSurface === 'active' && activeTab?.sessionId && (
              activeEventViewMode === 'terminal' ? (
                <TerminalCenter currentSessionId={activeTab.sessionId} compact={false} hasPendingTerminalActivity={hasPendingTerminalActivity} />
              ) : (
                <EventDisplay events={displayEvents} executionTree={sessionExecutionTree} onFeedbackSubmitted={handleFeedbackSubmitted} onSendMessage={submitQueryWithQuery} compact={compact} sessionId={activeTab.sessionId} tabId={targetTabId || undefined} />
              )
            )}
          </>
        )}
        </div>
      </div>

      {/* Input Area - Completely isolated from event updates, hidden in workflow mode */}
      {!hideInput && (
        <ChatInput
          onSubmit={submitQueryWithQuery}
          onStopStreaming={stopStreaming}
          tabId={targetTabId}
          restoredConversationPending={resumePending && !hasRestoredLiveContent}
        />
      )}
      
      {/* Toast notifications */}
      <ToastContainer 
        toasts={filteredToasts} 
        onRemoveToast={removeToast} 
      />
    </div>
  )
})

ChatAreaInner.displayName = 'ChatAreaInner'

// Main ChatArea component
const ChatArea = ChatAreaInner

ChatArea.displayName = 'ChatArea'
ChatArea.whyDidYouRender = true

export default ChatArea
