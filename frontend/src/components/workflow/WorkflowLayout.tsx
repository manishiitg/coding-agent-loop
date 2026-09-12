import { usePointerDrag } from '../../hooks/usePointerDrag'
import React, { useMemo, useCallback, useRef, useEffect, forwardRef, useState } from 'react'
import { useShallow } from 'zustand/react/shallow'
import { GripVertical, Laptop, PanelLeftClose, PanelLeftOpen, PanelRightClose, PanelRightOpen, Smartphone, Tablet } from 'lucide-react'
import { WorkflowCanvas, type WorkflowCanvasRef } from './canvas'
import { useGlobalPresetStore } from '../../stores/useGlobalPresetStore'
import { useModeStore } from '../../stores/useModeStore'
import { normalizeEventViewMode, useChatStore, waitForChatStoreHydration, type ChatTab } from '../../stores/useChatStore'
import { useWorkflowStore } from '../../stores/useWorkflowStore'
import { useWorkspaceStore } from '../../stores/useWorkspaceStore'
import { useWorkflowManifestStore } from '../../stores/useWorkflowManifestStore'
import { resolveWorkflowHistoryPath } from '../../utils/workflowHistoryPath'
import ChatArea, { type ChatAreaRef } from '../ChatArea'
import { WorkflowChatTabs } from './WorkflowChatTabs'
import { useRunningWorkflowsStore, useShowRunningDrawer } from '../../stores/useRunningWorkflowsStore'
import { useAppStore } from '../../stores/useAppStore'
import { sanitizeDisplayNameForFolder } from '../../utils/workflowUtils'
import { logger } from '../../utils/logger'
import { startRestoredTransportTerminal } from '../../utils/restoredTerminal'
import { isExternalReadOnlyWorkflowSession, isInternalChildSession, isScheduledSession } from '../../utils/workflowSessionKinds'
import { activeWorkflowTabIdForPreset } from '../../utils/workflowTabOwnership'
import { activateTab } from '../../utils/activateTab'
import {
  reconcileWorkflowRuntimeTab,
  shouldCatchUpRunningWorkflowTranscript,
  workflowRuntimeTabProjection,
} from './workflowRuntimeTabProjection'
import { isBlankWorkflowBuilderTab, resolveWorkflowTabForSession } from '../../utils/workflowTabResolution'
import {
  PreviousChatHistoryPanel,
  chatHistoryConversationPath,
  chatHistoryRuntimeLabel,
  chatHistorySessionTitle,
  chatHistorySupportsNativeResume,
  chatHistoryUsesTerminalRestore,
  chatHistoryWorkshopModeLabel,
} from '../PreviousChatHistoryPanel'
import { chatHistoryOpenDisposition } from '../../utils/chatHistoryOpenDisposition'
import { chatHistoryWorkshopMode } from '../../utils/chatHistoryWorkshopMode'
import {
  REPORT_PREVIEW_PREFERENCE_CHANGED_EVENT,
  readReportPreviewPreference,
  readWorkflowSplitPreference,
  type ReportPreviewDevice,
  writeReportPreviewPreference,
  writeWorkflowSplitPreference,
} from '../../utils/reportPreviewPreference'

// Helper component to get observerId and render ChatArea
// Always renders ChatArea (even without observerId) so it can handle initialization
const ChatAreaWithObserverId = forwardRef<ChatAreaRef, {
  onNewChat: () => void
  hideHeader?: boolean
  hideInput?: boolean
  compact?: boolean
  workflowPreviousChatsPanel?: React.ReactNode
}>(({ onNewChat, hideHeader, hideInput, compact, workflowPreviousChatsPanel }, ref) => {
  // Prefer the active workflow tab when one is selected. The tab strip keeps
  // active workflow tabs visible even while preset metadata is catching up
  // after reload; ChatArea must use the same rule or the input area disappears.
  // Legacy/restored builder tabs may not have presetQueryId, so allow those
  // when there is no exact tab for the active preset.
  const currentPresetId = useGlobalPresetStore(state => state.activePresetIds.workflow)
  const workflowTabId = useChatStore(state =>
    activeWorkflowTabIdForPreset(state.activeTabId, currentPresetId, state.chatTabs)
  )
  const activePhaseId = useChatStore(state => {
    const tabId = activeWorkflowTabIdForPreset(state.activeTabId, currentPresetId, state.chatTabs)
    return tabId ? state.chatTabs[tabId]?.metadata?.phaseId : undefined
  })

  // Show chat input for chat-compatible phases
  const effectiveHideInput = isChatCompatiblePhase(activePhaseId) ? false : hideInput

  // The agent's open_workspace_view calls open the toolbar's views here.
  useWorkflowViewPresentations(workflowTabId)

  return (
    <ChatArea
      ref={ref}
      onNewChat={onNewChat}
      hideHeader={hideHeader}
      hideInput={effectiveHideInput}
      compact={compact}
      workflowPreviousChatsPanel={workflowPreviousChatsPanel}
      // Pass null (not undefined) when no tab matches the active workflow preset.
      // Otherwise ChatArea falls back to the global activeTabId and can briefly
      // render the previous workflow's blocking human-feedback/auth prompt.
      tabId={workflowTabId ?? null}
    />
  )
})
import { agentApi, workflowManifestApi } from '../../services/api'
import {
  type ActiveSessionInfo,
  type ChatHistorySession,
  type ExecutionOptions,
  type PollingEvent,
  type RunningWorkflowInfo,
} from '../../services/api-types'
import { findOrCreateWorkflowTab, isChatCompatiblePhase } from '../../utils/chatSubmitHelpers'
import { useWorkflowViewPresentations } from './useWorkflowViewPresentations'
import { hasWorkflowChatContent } from './workflowChatTabConversion'
import { hydrateTabEvents } from '../../utils/sessionRestore'
import { isReadOnlyWorkflowRunTab, workflowTabsNeedingHydration, hydrateWorkflowTabsPrioritized } from '../../utils/workflowTabHydration'
import { isPreviewView, isWorkspacePaneView } from './workspaceViews'
// Inactive workflow tabs hydrate lazily and fall back to workflow-scoped chat history.

const WORKFLOW_RESTORE_TIMEOUT_MS = 8000
function normalizeWorkflowPath(path?: string | null): string {
  return (path || '').replace(/\/+$/, '')
}

function defaultWorkflowSplitRatio(device: ReportPreviewDevice, width = typeof window === 'undefined' ? 1280 : window.innerWidth): number {
  if (device === 'mobile') return clampWorkflowSplitRatio((width - 480) / width, width)
  if (device === 'desktop') return clampWorkflowSplitRatio(380 / width, width)
  return 0.5
}

function clampWorkflowSplitRatio(ratio: number, width: number): number {
  const minPaneWidth = 240
  const minRatio = Math.max(0.15, Math.min(0.5, minPaneWidth / Math.max(width, minPaneWidth * 2)))
  const maxRatio = Math.min(0.85, 1 - minRatio)
  return Math.max(minRatio, Math.min(maxRatio, ratio))
}

function workflowTabSortTimestamp(tab: ChatTab): number {
  return tab.lastAccessedAt ?? tab.createdAt ?? 0
}

function isInteractiveWorkflowTab(tab: ChatTab): boolean {
  return tab.metadata?.mode === 'workflow' && tab.metadata?.isViewOnly !== true
}

function isRestorableWorkflowChatSession(session: ChatHistorySession): boolean {
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

function applyRestoredWorkflowConversationConfig(tabId: string, session: ChatHistorySession): void {
  const chatStore = useChatStore.getState()
  const path = chatHistoryConversationPath(session)
  const useTerminalRestore = chatHistoryUsesTerminalRestore(session)
  const useNativeResume = chatHistorySupportsNativeResume(session)
  const existingContext = chatStore.getTabConfig(tabId)?.fileContext || []
  const shouldAttachFileFallback = !useTerminalRestore && !useNativeResume
  const nextFileContext = shouldAttachFileFallback
    ? existingContext.some(item => item.path === path)
      ? existingContext
      : [
          ...existingContext,
          {
            name: chatHistorySessionTitle(session),
            path,
            type: 'file' as const,
          },
        ]
    : existingContext.filter(item => item.path !== path)

  chatStore.setTabConfig(tabId, {
    fileContext: nextFileContext,
    restoredConversationPath: path,
    restoredConversationSummary: undefined,
    restoredConversationTitle: chatHistorySessionTitle(session),
    restoredConversationWorkshopModeLabel: chatHistoryWorkshopModeLabel(session),
    restoredConversationRuntimeLabel: chatHistoryRuntimeLabel(session),
    restoredConversationNativeResume: useTerminalRestore || useNativeResume,
  })
  chatStore.setTabMetadata(tabId, { workshopMode: chatHistoryWorkshopMode(session) })
}

function isRunningWorkflowEntry(entry: RunningWorkflowInfo): boolean {
  const status = (entry.status || '').toLowerCase().trim()
  if (!status) return true
  return (
    status === 'running' ||
    status === 'active' ||
    status === 'in_progress' ||
    status === 'paused' ||
    status === 'waiting' ||
    status === 'waiting_feedback' ||
    status === 'waiting_for_input' ||
    status === 'idle' ||
    entry.needs_user_input === true
  )
}

function isExternalReadOnlyWorkflowEntry(entry: RunningWorkflowInfo): boolean {
  return isExternalReadOnlyWorkflowSession({
    sessionId: entry.session_id,
    triggeredBy: entry.triggered_by,
  })
}

function isExternalReadOnlyActiveWorkflowSession(session: ActiveSessionInfo): boolean {
  return isExternalReadOnlyWorkflowSession({
    sessionId: session.session_id,
    triggeredBy: session.triggered_by,
    botPlatform: session.bot_platform,
  })
}

function runningWorkflowBelongsToPreset(
  entry: RunningWorkflowInfo,
  presetId: string,
  workspacePath?: string | null,
): boolean {
  if (entry.preset_query_id) {
    return entry.preset_query_id === presetId
  }
  return Boolean(
    workspacePath &&
    entry.workspace_path &&
    normalizeWorkflowPath(entry.workspace_path) === normalizeWorkflowPath(workspacePath),
  )
}

// Adapts the already-fetched active-sessions cache (kept fresh by
// GlobalActivityMonitor's 5s poll) into the shape the running-workflow tab
// reconciler needs, so it doesn't have to run its own independent
// /api/workflow/running poll for the same underlying tracked-execution data.
function activeSessionToRunningWorkflowInfo(session: ActiveSessionInfo): RunningWorkflowInfo {
  return {
    query_id: session.session_id,
    session_id: session.session_id,
    preset_query_id: session.preset_query_id,
    preset_name: session.preset_name,
    workspace_path: session.workspace_path || '',
    phase_id: session.phase_id,
    phase_name: session.phase_name,
    status: session.status,
    title: session.title,
    query: session.query,
    triggered_by: session.triggered_by || '',
    started_at: session.created_at,
    needs_user_input: session.needs_user_input,
    waiting_message: session.waiting_message,
    waiting_since: session.waiting_since,
    runtime_state: session.runtime_state,
    display_status: session.display_status,
  }
}

const WorkflowPreviousChatsPanel: React.FC<{
  workspacePath: string
  onHasChatsChange?: (hasChats: boolean) => void
  // When true the panel fills the chat pane as the primary landing surface
  // (mirrors the multi-agent landing panel) instead of the compact top strip.
  primary?: boolean
}> = ({ workspacePath, onHasChatsChange, primary = false }) => {
  const activeTabId = useChatStore(state => state.activeTabId)
  const activePresetId = useGlobalPresetStore(state => state.activePresetIds.workflow)
  const setShowChatArea = useWorkflowStore(state => state.setShowChatArea)
  const activeSessionId = useChatStore(state => {
    const tabId = state.activeTabId
    const tab = tabId ? state.chatTabs[tabId] : undefined
    if (!tab?.sessionId || tab.metadata?.mode !== 'workflow') return undefined
    return hasWorkflowChatContent(state.tabEvents[tab.sessionId]) ? tab.sessionId : undefined
  })
  const setTabConfig = useChatStore(state => state.setTabConfig)
  const addToast = useChatStore(state => state.addToast)

  // Shared by the manual "Resume" click (handleResumePreviousChat below,
  // starting tab = whatever's active) and the auto-restore-on-open effect
  // further below (starting tab = the just-created blank builder tab, known
  // synchronously -- reading activeTabId there would race React's state
  // batching).
  const resumeChatSessionIntoTab = useCallback(async (
    session: ChatHistorySession,
    startingTabId: string,
    disposition: ReturnType<typeof chatHistoryOpenDisposition>,
  ) => {
    let targetTabId = startingTabId
    const chatStore = useChatStore.getState()
    const restoredWorkshopMode = chatHistoryWorkshopMode(session)
    let targetTab = chatStore.chatTabs[targetTabId]
    const targetPresetId = targetTab?.metadata?.presetQueryId

    if (
      !targetTab ||
      targetTab.metadata?.mode !== 'workflow' ||
      targetTab.metadata?.isViewOnly === true ||
      (activePresetId && targetPresetId && targetPresetId !== activePresetId) ||
      isBlankWorkflowBuilderTab(targetTab, activePresetId || '', chatStore.tabEvents)
    ) {
      // Restore must target an interactive Chat tab -- never the Builder
      // (it stays blank; a resume from its landing view opens beside it) and
      // never a full-run/Schedule tab (reusing one corrupts its presentation
      // metadata). Same resolution as every other opener: the tab already on
      // this session, else the workflow's idle Chat tab, else a new one.
      const latestStore = useChatStore.getState()
      targetTabId = (await resolveWorkflowTabForSession({
        getTabs: () => useChatStore.getState().chatTabs,
        getTabEvents: () => useChatStore.getState().tabEvents,
        presetQueryId: activePresetId || '',
        sessionId: session.session_id,
        name: 'Automation Builder',
        metadata: { mode: 'workflow', phaseId: 'workflow-builder', phaseName: 'Automation Builder', presetQueryId: activePresetId || undefined, workshopMode: restoredWorkshopMode },
        createChatTab: latestStore.createChatTab,
        updateTabSessionId: latestStore.updateTabSessionId,
      })).tabId
      targetTab = useChatStore.getState().chatTabs[targetTabId]
    }

    if (!targetTab) {
      addToast('Failed to resume previous chat', 'error')
      return
    }

    if (activePresetId && targetTab.metadata?.presetQueryId !== activePresetId) {
      chatStore.setTabMetadata(targetTabId, {
        phaseId: targetTab.metadata?.phaseId || 'workflow-builder',
        phaseName: targetTab.metadata?.phaseName || 'Automation Builder',
        presetQueryId: activePresetId,
      })
    }
    chatStore.setTabMetadata(targetTabId, { workshopMode: restoredWorkshopMode })

    if (targetTab?.sessionId !== session.session_id && targetTab?.sessionId) {
      chatStore.resetTabChat(targetTabId)
    }
    chatStore.updateTabSessionId(targetTabId, session.session_id)

    const path = chatHistoryConversationPath(session)
    const useTerminalRestore = disposition === 'interactive-transport' && chatHistoryUsesTerminalRestore(session)
    const useNativeResume = disposition === 'interactive-transport' && chatHistorySupportsNativeResume(session)
    const existingContext = useChatStore.getState().getTabConfig(targetTabId)?.fileContext || []
    const shouldAttachFileFallback = !useTerminalRestore && !useNativeResume
    const nextFileContext = shouldAttachFileFallback
      ? existingContext.some(item => item.path === path)
        ? existingContext
        : [
            ...existingContext,
            {
              name: chatHistorySessionTitle(session),
              path,
              type: 'file' as const,
            },
          ]
      : existingContext.filter(item => item.path !== path)

    setTabConfig(targetTabId, {
      fileContext: nextFileContext,
      restoredConversationPath: path,
      restoredConversationSummary: undefined,
      restoredConversationTitle: chatHistorySessionTitle(session),
      restoredConversationWorkshopModeLabel: chatHistoryWorkshopModeLabel(session),
      restoredConversationRuntimeLabel: chatHistoryRuntimeLabel(session),
      restoredConversationNativeResume: useTerminalRestore || useNativeResume,
    })
    // A resume started from the Builder landed in a different (Chat) tab;
    // show it. Reused idle tabs aren't activated by resolution the way
    // freshly created ones are.
    if (targetTabId !== startingTabId) {
      activateTab(targetTabId)
      setShowChatArea(true)
    }
    // Both tmux terminal-restore and native-resume sessions reattach into a
    // coding-agent terminal on the backend. Keep that transport restoration,
    // but present its normalized event transcript by default; Raw remains an
    // explicit diagnostic choice rather than the first thing a user sees.
    if (useTerminalRestore || useNativeResume) {
      chatStore.setTabViewMode(targetTabId, 'formatted')
      activateTab(targetTabId)
      setShowChatArea(true)
      startRestoredTransportTerminal(session.session_id, path, session.session_id, workspacePath)
    }
  }, [activePresetId, addToast, setShowChatArea, setTabConfig, workspacePath])

  const handleResumePreviousChat = useCallback(async (session: ChatHistorySession) => {
    const disposition = chatHistoryOpenDisposition(session)
    if (disposition === 'read-only-schedule') {
      const chatStore = useChatStore.getState()
      const scheduleMetadata: NonNullable<ChatTab['metadata']> = {
        mode: 'workflow',
        presetQueryId: activePresetId || undefined,
        isViewOnly: true,
        isScheduledRun: true,
        scheduledJobName: 'Schedule',
        readOnlyRestoredAt: Date.now(),
        userInteractiveContinuation: false,
      }
      // Same resolution as every other opener: the tab already on this
      // session, else this schedule's finished lane, else a new tab bound
      // to the session at creation (it used to be minted with a random id
      // and re-pointed afterwards).
      const { tabId: targetTabId, via } = await resolveWorkflowTabForSession({
        getTabs: () => useChatStore.getState().chatTabs,
        presetQueryId: activePresetId || '',
        sessionId: session.session_id,
        name: 'Schedule',
        metadata: scheduleMetadata,
        createChatTab: chatStore.createChatTab,
        updateTabSessionId: chatStore.updateTabSessionId,
      })
      if (via !== 'created') chatStore.setTabMetadata(targetTabId, scheduleMetadata)
      chatStore.setTabViewMode(targetTabId, 'formatted')
      chatStore.setTabStreaming(targetTabId, false)
      chatStore.setTabCompleted(targetTabId, true)
      chatStore.setTabHasRunningBgAgents(targetTabId, false)
      chatStore.setTabSyntheticTurn(targetTabId, false)
      chatStore.setTabCanSteer(targetTabId, false)

      try {
        const runtime = await hydrateTabEvents(session.session_id, {
          workspacePath,
          fallbackToChatHistory: true,
          preferChatHistory: true,
        })
        chatStore.setTabStreaming(targetTabId, runtime.status === 'running')
        chatStore.setTabCompleted(targetTabId, runtime.status !== 'running')
        chatStore.setTabHasRunningBgAgents(targetTabId, runtime.hasRunningBackgroundAgents ?? false)
        chatStore.setTabSyntheticTurn(targetTabId, runtime.isSyntheticTurn ?? false)
        chatStore.setTabCanSteer(targetTabId, false)
      } catch (error) {
        logger.warn('WorkflowLayout', 'Failed to restore scheduled-run transcript', {
          sessionId: session.session_id,
          error,
        })
        addToast('Failed to open the saved schedule transcript', 'error')
        return
      }

      activateTab(targetTabId)
      setShowChatArea(true)
      return
    }

    if (!activeTabId) {
      addToast('No active automation chat to resume in', 'error')
      return
    }
    await resumeChatSessionIntoTab(session, activeTabId, disposition)
  }, [activeTabId, addToast, resumeChatSessionIntoTab, setShowChatArea, workspacePath, activePresetId])

  return (
    <PreviousChatHistoryPanel
      workspacePath={workspacePath}
      activeSessionId={activeSessionId ?? undefined}
      // The active tab is the conversation workspace. The panel beneath it
      // is only a selector for recent conversations / schedules / bots, so a
      // second "Previous automation chats" heading just repeated the tab.
      title=""
      actionLabel="Open"
      emptyText="No previous automation chats yet."
      onHasChatsChange={onHasChatsChange}
      onSelectSession={handleResumePreviousChat}
      compact={!primary}
      fill={primary}
    />
  )
}

function workflowSessionMatchesPreset(session: ActiveSessionInfo, presetId: string, workspacePath?: string | null): boolean {
  if (session.agent_mode !== 'workflow' && session.agent_mode !== 'workflow_phase') return false

  if (session.preset_query_id && session.preset_query_id === presetId) return true

  const targetWorkspace = normalizeWorkflowPath(workspacePath)
  return !!targetWorkspace && normalizeWorkflowPath(session.workspace_path) === targetWorkspace
}

function isLiveWorkflowSessionForPreset(session: ActiveSessionInfo, presetId: string, workspacePath?: string | null): boolean {
  if (!workflowSessionMatchesPreset(session, presetId, workspacePath)) return false

  const status = (session.status || '').toLowerCase().trim()
  return (
    session.needs_user_input === true ||
    session.has_running_background_agents === true ||
    (session.running_background_agent_count ?? 0) > 0 ||
    status === 'running' ||
    status === 'active' ||
    status === 'in_progress' ||
    status === 'paused' ||
    status === 'waiting' ||
    status === 'waiting_feedback'
  )
}

/**
 * The Builder tab always exists for a workflow: the fixed, blank first tab
 * with the Recent/Schedules/Bots landing view. Resolves to the existing one
 * or creates it. Creating a tab activates it, so callers that have already
 * chosen what the user should be looking at pass keepSelection to get that
 * choice back.
 */
async function ensureWorkflowBuilderTab(presetId: string, options?: { keepSelection?: boolean }): Promise<string> {
  const store = useChatStore.getState()
  const previousActiveTabId = store.activeTabId
  const { tabId, via } = await resolveWorkflowTabForSession({
    getTabs: () => useChatStore.getState().chatTabs,
    getTabEvents: () => useChatStore.getState().tabEvents,
    presetQueryId: presetId,
    name: 'Automation Builder',
    metadata: { mode: 'workflow', phaseId: 'workflow-builder', phaseName: 'Automation Builder', presetQueryId: presetId },
    createChatTab: store.createChatTab,
    updateTabSessionId: store.updateTabSessionId,
  })
  if (options?.keepSelection && via === 'created' && previousActiveTabId && previousActiveTabId !== tabId) {
    activateTab(previousActiveTabId)
  }
  return tabId
}

// restoreWorkflowStateFromEvents has no timeout of its own (it awaits
// agentApi.getRecentSessionEvents / hydrateTabEvents directly), so every
// caller that increments restoringWorkflowSessions via
// beginWorkflowSessionRestore MUST wrap its restore call in this helper --
// otherwise a hung request leaves that counter incremented forever and the
// chat pane stuck on "Restoring previous session..." with no way out.
function withWorkflowRestoreTimeout<T>(promise: Promise<T>, label: string, timeoutMs = WORKFLOW_RESTORE_TIMEOUT_MS): Promise<T> {
  return new Promise((resolve, reject) => {
    const timeout = window.setTimeout(() => {
      reject(new Error(`${label} timed out after ${timeoutMs}ms`))
    }, timeoutMs)

    promise.then(
      value => {
        window.clearTimeout(timeout)
        resolve(value)
      },
      error => {
        window.clearTimeout(timeout)
        reject(error)
      }
    )
  })
}

/**
 * Helper function to restore workflow state from loaded events
 * Called during workflow reconnection to restore:
 * - Current running step ID
 * - Step statuses (running, completed, failed)
 * - Batch progress (for BatchProgressHeader)
 * This ensures the UI shows the correct state immediately after page refresh
 */
async function restoreWorkflowStateFromEvents(
  sessionId: string,
  workspacePath?: string | null,
  allowChatHistoryFallback = false,
  activeTabOnly = false,
): Promise<void> {
  try {
    const { addTabEvents, setTabEvents, setTabLastEventIndex, getTabLastEventIndex, getTabEvents } = useChatStore.getState()
    const workflowStore = useWorkflowStore.getState()

    // Transcript hydration and canvas-state restoration are separate concerns.
    // Another workflow may already own the singleton canvas batch-progress
    // state, but that must never prevent this session's conversation events
    // from being loaded into its tab.
    const shouldRestoreCanvasState = () => {
      const store = useChatStore.getState()
      const activeSession = store.activeTabId ? store.chatTabs[store.activeTabId]?.sessionId : undefined
      return !useWorkflowStore.getState().batchProgress?.isActive && (!activeTabOnly || activeSession === sessionId)
    }

    let events: PollingEvent[] = []
    let lastIndex = -1

    // Load events for this session from the in-memory EventStore. If that
    // buffer has expired, workflow builder chats can still restore their
    // visible transcript from the workflow-scoped conversation file.
    if (allowChatHistoryFallback) {
      await hydrateTabEvents(sessionId, {
        workspacePath: workspacePath || undefined,
        fallbackToChatHistory: true,
        preferChatHistory: true,
      })
      events = getTabEvents(sessionId)
      lastIndex = getTabLastEventIndex(sessionId)
    } else {
      const response = await agentApi.getRecentSessionEvents(sessionId)
      events = response.events as PollingEvent[]
      lastIndex = response.last_processed_index ?? events.length - 1
    }

    if (events.length === 0) {
      return
    }

    // Use setTabEvents (replace) when tab is empty (restoration), addTabEvents (append) when live
    const existingEvents = getTabEvents(sessionId)
    if (!allowChatHistoryFallback) {
      if (existingEvents.length === 0) {
        setTabEvents(sessionId, events)
      } else {
        addTabEvents(sessionId, events)
      }
    }
    // CRITICAL: Use last_processed_index from backend (not events.length - 1)
    // Backend tracks the actual event index which may be higher due to filtering/cleanup
    // Only advance the index if backend is ahead (SSE may have already advanced it)
    const currentIndex = getTabLastEventIndex(sessionId)
    if (lastIndex > currentIndex) {
      setTabLastEventIndex(sessionId, lastIndex)
    }

    if (!shouldRestoreCanvasState()) {
      logger.debug('WorkflowLayout', 'Hydrated workflow transcript without replacing active batch progress', {
        sessionId,
        eventCount: events.length,
      })
      return
    }

    // Scan events to find batch context, current step, and step statuses
    let latestBatchContext: {
      groupName: string
      groupIndex: number
      totalGroups: number
      runFolder: string
    } | null = null
    let completedCount = 0
    let failedCount = 0

    // Track current step and step statuses
    let latestRunningStepId: string | null = null
    const stepStatuses = new Map<string, 'pending' | 'running' | 'completed' | 'failed'>()

    for (const event of events) {
      // Extract from todo_task_step_completed
      if (event.type === 'todo_task_step_completed') {
        const eventData = event.data as Record<string, unknown>
        const data = (eventData?.data as Record<string, unknown>) || eventData
        const stepId = data?.step_id as string
        if (stepId) {
          stepStatuses.set(stepId, 'completed')
          if (latestRunningStepId === stepId) {
            latestRunningStepId = null
          }
        }
      }

      // Extract from batch_group_start
      if (event.type === 'batch_group_start') {
        const eventData = event.data as Record<string, unknown>
        const data = (eventData?.data as Record<string, unknown>) || eventData
        const groupName = data?.group_name as string
        const groupIndex = data?.group_index as number
        const totalGroups = data?.total_groups as number
        const runFolder = data?.run_folder as string

        if (groupName && totalGroups > 0) {
          latestBatchContext = { groupName, groupIndex, totalGroups, runFolder }
        }
      }

      // Count completed/failed from batch_group_end
      if (event.type === 'batch_group_end') {
        const eventData = event.data as Record<string, unknown>
        const data = (eventData?.data as Record<string, unknown>) || eventData
        const success = data?.success as boolean
        if (success === true) completedCount++
        else if (success === false) failedCount++
      }

    }

    // Restore current step ID if we found a running step
    if (latestRunningStepId) {
      logger.debug('WorkflowLayout', `Restoring currentStepId: ${latestRunningStepId}`)
      workflowStore.setCurrentStepId(latestRunningStepId)
    }

    // Restore step statuses
    if (stepStatuses.size > 0) {
      logger.debug('WorkflowLayout', `Restoring ${stepStatuses.size} step statuses`)
      stepStatuses.forEach((status, stepId) => {
        workflowStore.setStepStatus(stepId, status)
      })
    }

    // Restore batch progress if we found batch context with multiple groups
    if (latestBatchContext && latestBatchContext.totalGroups > 1) {
      const remaining = latestBatchContext.totalGroups - completedCount - failedCount

      // Only restore if batch is still active (has remaining groups)
      if (remaining > 0) {
        workflowStore.handleBatchGroupStart(
          latestBatchContext.groupName,
          latestBatchContext.runFolder || '',
          undefined,
          latestBatchContext.groupIndex,
          latestBatchContext.totalGroups
        )

        // Update completed/failed counts if we have them
        if (completedCount > 0 || failedCount > 0) {
          const state = useWorkflowStore.getState()
          if (state.batchProgress) {
            useWorkflowStore.setState({
              batchProgress: {
                ...state.batchProgress,
                completedCount,
                failedCount,
                remainingCount: remaining
              }
            })
          }
        }

        logger.debug('WorkflowLayout', 'Restored batch progress from events:', {
          sessionId,
          groupName: latestBatchContext.groupName,
          groupIndex: latestBatchContext.groupIndex,
          totalGroups: latestBatchContext.totalGroups,
          completedCount,
          failedCount,
          remaining
        })
      }
    }
  } catch (error) {
    logger.warn('WorkflowLayout', 'Failed to restore batch progress:', error)
  }
}

interface WorkflowLayoutProps {
  className?: string
  onCreatePlan?: () => void
  onNewChat: () => void
}

/**
 * Main layout component for workflow mode
 * Shows React Flow canvas as the main area with ChatArea appearing when a phase is started
 * Uses useWorkflowStore for activePhase and showChatArea state (single source of truth)
 */
export const WorkflowLayout: React.FC<WorkflowLayoutProps> = ({
  className = '',
  onCreatePlan,
  onNewChat
}) => {
  const selectedModeCategory = useModeStore(state => state.selectedModeCategory)
  // Narrow selectors: bare useChatStore() re-renders on every store update (10x/sec with 2 parallel sessions)
  const currentWorkflowPhase = useChatStore(state => state.currentWorkflowPhase)
  const setCurrentWorkflowPhase = useChatStore(state => state.setCurrentWorkflowPhase)
  // Use workflow store for UI state (single source of truth)
  const activePhase = useWorkflowStore(state => state.activePhase)
  const showChatArea = useWorkflowStore(state => state.showChatArea)
  const showWorkspacePane = useWorkflowStore(state => state.showWorkspacePane)
  const focusedPane = useWorkflowStore(state => state.focusedPane)
  const setShowChatArea = useWorkflowStore(state => state.setShowChatArea)
  const setShowWorkspacePane = useWorkflowStore(state => state.setShowWorkspacePane)
  const setFocusedPane = useWorkflowStore(state => state.setFocusedPane)
  const workflowWorkspaceView = useWorkflowStore(state => state.workflowWorkspaceView)
  const setWorkflowWorkspaceView = useWorkflowStore(state => state.setWorkflowWorkspaceView)
  const lastCanvasView = useWorkflowStore(state => state.lastCanvasView)
  const minimizeWorkflow = useRunningWorkflowsStore(state => state.minimizeWorkflow)
  const showRunningDrawer = useShowRunningDrawer()

  const getPhaseById = useWorkflowStore(state => state.getPhaseById)
  
  // Ref for the ChatArea component
  const chatAreaRef = useRef<ChatAreaRef>(null)
  // Ref for the WorkflowCanvas component (for triggering refresh)
  const canvasRef = useRef<WorkflowCanvasRef>(null)
  // Store pending query to submit after ChatArea mounts
  const pendingQueryRef = useRef<{ query: string; executionOptions?: ExecutionOptions } | null>(null)
  const isActiveWorkflowSessionRestoring = useChatStore(state => {
    const tab = state.activeTabId ? state.chatTabs[state.activeTabId] : undefined
    return !!tab?.sessionId && (state.restoringWorkflowSessions[tab.sessionId] ?? 0) > 0
  })
  const revealWorkflowChat = useCallback((tabId: string) => {
    const chatStore = useChatStore.getState()
    if (chatStore.chatTabs[tabId]) {
      chatStore.setTabViewMode(tabId, 'formatted')
      activateTab(tabId)
    }
    setShowChatArea(true)
    setShowWorkspacePane(true)
    setFocusedPane('chat')
  }, [setFocusedPane, setShowChatArea, setShowWorkspacePane])
  // Track the previous preset ID for auto-minimize on preset switch
  const previousPresetIdRef = useRef<string | null>(null)
  const pendingReadOnlyRestoreRef = useRef<{ presetId: string | null; tabId: string } | null>(null)
  useEffect(() => {
    const handleReadOnlyRestore = (event: Event) => {
      const detail = (event as CustomEvent<{ presetId?: string | null; tabId?: string }>).detail
      if (!detail?.tabId) return
      pendingReadOnlyRestoreRef.current = {
        presetId: detail.presetId ?? null,
        tabId: detail.tabId,
      }
      revealWorkflowChat(detail.tabId)
    }

    window.addEventListener('workflow-readonly-run-restored', handleReadOnlyRestore)
    return () => window.removeEventListener('workflow-readonly-run-restored', handleReadOnlyRestore)
  }, [revealWorkflowChat])
  // During workflow execution we do not synchronize the file tree event-by-event.
  // The Workspace component marks the view stale after completed workspace work.

  // Get selected run folder and workspace functions (defined early for use in useEffect)
  const selectedRunFolder = useWorkflowStore(state => state.selectedRunFolder)
  const setStepOverride = useWorkflowStore(state => state.setStepOverride)
  const selectedGroupIds = useWorkflowStore(state => state.selectedGroupIds)
  const variablesManifest = useWorkflowStore(state => state.variablesManifest)
  const { fetchFiles, setExpandedFolders } = useWorkspaceStore(useShallow(state => ({
    fetchFiles: state.fetchFiles,
    setExpandedFolders: state.setExpandedFolders,
  })))
  // Subscribe to workspace minimized state so we can skip fetches when panel is hidden
  const workspaceMinimized = useAppStore(state => state.workspaceMinimized)
  const setWorkspaceMinimized = useAppStore(state => state.setWorkspaceMinimized)
  const lastWorkspaceRunExpansionKeyRef = useRef<string | null>(null)
  const reportAutoMinimizedWorkspaceRef = useRef(false)
  const prevWorkflowWorkspaceViewRef = useRef<string | null>(null)

  const workflowHydrationsInFlight = useRef(new Map<string, Promise<void>>())
  const rehydrateWorkflowTabs = useCallback(async (tabs: ChatTab[], currentWorkspacePath?: string | null) => {
    const tabsToHydrate = tabs.filter(tab =>
      tab.sessionId && useChatStore.getState().getTabEvents(tab.sessionId).length === 0
    )
    if (tabsToHydrate.length === 0) return 0

    // Start runtime metadata alongside transcripts, never before them.
    const activeSessionsPromise = withWorkflowRestoreTimeout(
      useChatStore.getState().getActiveSessions(), 'Fetching active workflow sessions',
    ).catch(error => {
      logger.warn('WorkflowLayout', 'Failed to fetch active sessions during rehydrate:', error)
      return []
    })
    let historyPromise: Promise<Map<string, ChatHistorySession>> | undefined
    const getWorkflowHistoryBySession = () => {
      if (!historyPromise) {
        historyPromise = (async () => {
          const history = new Map<string, ChatHistorySession>()
          if (!currentWorkspacePath) return history
          try {
            const response = await withWorkflowRestoreTimeout(
              agentApi.listChatHistorySessions(100, 0, currentWorkspacePath), 'Restoring workflow conversation metadata',
            )
            for (const session of response.sessions || []) {
              if (isRestorableWorkflowChatSession(session)) history.set(session.session_id, session)
            }
          } catch (error) {
            logger.warn('WorkflowLayout', 'Failed to load workflow chat history metadata:', error)
          }
          return history
        })()
      }
      return historyPromise
    }
    const hydrate = (tab: ChatTab): Promise<void> => {
      const sessionId = tab.sessionId!
      // The user may close or replace a queued tab before its worker starts.
      if (useChatStore.getState().chatTabs[tab.tabId]?.sessionId !== sessionId) return Promise.resolve()
      const existing = workflowHydrationsInFlight.current.get(sessionId)
      if (existing) return existing
      const stillOwnsSession = () => useChatStore.getState().chatTabs[tab.tabId]?.sessionId === sessionId
      const task = (async () => {
        useChatStore.getState().beginWorkflowSessionRestore(sessionId)
        try {
          if (isReadOnlyWorkflowRunTab(tab)) {
            const runtime = await withWorkflowRestoreTimeout(
              hydrateTabEvents(sessionId, { workspacePath: currentWorkspacePath || undefined, fallbackToChatHistory: true }),
              `Restoring scheduled run transcript for ${sessionId}`,
            )
            if (stillOwnsSession()) {
              useChatStore.getState().setTabStreaming(tab.tabId, runtime.status === 'running')
              useChatStore.getState().setTabCompleted(tab.tabId, runtime.status === 'completed' || runtime.status === 'stopped')
            }
          } else {
            await withWorkflowRestoreTimeout(
              restoreWorkflowStateFromEvents(sessionId, currentWorkspacePath, true, true),
              `Restoring workflow events for ${sessionId}`,
            )
          }
        } finally {
          useChatStore.getState().endWorkflowSessionRestore(sessionId)
        }

        // Status/config enrichment must not hold the transcript spinner or the
        // selected-tab promise open. Check ownership after every async boundary.
        void (async () => {
          const activeSessions = await activeSessionsPromise
          if (!stillOwnsSession()) return
          if (activeSessions.some(session => session.session_id === sessionId &&
            (session.agent_mode === 'workflow' || session.agent_mode === 'workflow_phase'))) {
            useChatStore.getState().setTabStreaming(tab.tabId, true)
          } else if (!isReadOnlyWorkflowRunTab(tab)) {
            const history = await getWorkflowHistoryBySession()
            const session = history.get(sessionId)
            if (session && stillOwnsSession() && useChatStore.getState().getTabEvents(sessionId).length > 0) {
              // A user may have started another turn while metadata was loading.
              if (!useChatStore.getState().chatTabs[tab.tabId]?.isStreaming) {
                applyRestoredWorkflowConversationConfig(tab.tabId, session)
              }
            }
          }
        })().catch(error => logger.warn('WorkflowLayout', 'Workflow metadata enrichment failed:', error))
      })()
      workflowHydrationsInFlight.current.set(sessionId, task)
      const release = () => { workflowHydrationsInFlight.current.delete(sessionId) }
      void task.then(release, release)
      return task
    }
    return hydrateWorkflowTabsPrioritized(tabsToHydrate, useChatStore.getState().activeTabId, hydrate,
      (tab, error) => logger.warn('WorkflowLayout', `Failed to restore ${tab.sessionId}:`, error))
  }, [])

  // Get active workflow preset (file-backed manifests, not DB presets)
  const activePresetId = useGlobalPresetStore(state => state.activePresetIds.workflow)
  const activeWorkflowPreset = useGlobalPresetStore(state => {
    const presetId = state.activePresetIds.workflow
    return presetId ? state.workflowPresets.find(preset => preset.id === presetId) ?? null : null
  })
  // The manifest registry is the canonical workflow identity. Preset objects
  // can briefly lag behind activePresetId during an in-app workflow switch;
  // deriving history from that stale object made the new workflow request the
  // previous/empty path until a page reload rebuilt all stores.
  const workflowManifests = useWorkflowManifestStore(state => state.workflows)
  const activeWorkflowWorkspacePath = resolveWorkflowHistoryPath(
    activePresetId,
    workflowManifests,
    activeWorkflowPreset,
  )
  // Keep the last concrete workspace path for the active preset during manifest
  // refreshes. A transient null here unmounts the report pane and makes toolbar
  // popups think the user switched workflows.
  const lastWorkspacePathRef = useRef<{ presetId: string | null, path: string | null }>({
    presetId: activePresetId,
    path: activeWorkflowWorkspacePath,
  })

  const workspacePath = useMemo(() => {
    if (activeWorkflowWorkspacePath) {
      lastWorkspacePathRef.current = {
        presetId: activePresetId,
        path: activeWorkflowWorkspacePath,
      }
      return activeWorkflowWorkspacePath
    }

    if (activePresetId && lastWorkspacePathRef.current.presetId === activePresetId) {
      return lastWorkspacePathRef.current.path
    }

    lastWorkspacePathRef.current = {
      presetId: activePresetId,
      path: null,
    }
    return null
  }, [activePresetId, activeWorkflowWorkspacePath])

  const [reportPreviewPreference, setReportPreviewPreference] = useState<ReportPreviewDevice>(
    () => readReportPreviewPreference(workspacePath),
  )
  const splitLayoutRef = useRef<HTMLDivElement>(null)
  const [workspaceSplitRatio, setWorkspaceSplitRatio] = useState(() => (
    readWorkflowSplitPreference(workspacePath, readReportPreviewPreference(workspacePath))
      ?? defaultWorkflowSplitRatio(readReportPreviewPreference(workspacePath))
  ))
  const workspaceSplitRatioRef = useRef(workspaceSplitRatio)

  useEffect(() => {
    const saved = readWorkflowSplitPreference(workspacePath, reportPreviewPreference)
    const width = splitLayoutRef.current?.getBoundingClientRect().width || window.innerWidth
    const next = saved ?? defaultWorkflowSplitRatio(reportPreviewPreference, width)
    workspaceSplitRatioRef.current = next
    setWorkspaceSplitRatio(next)
  }, [reportPreviewPreference, workspacePath])

  const setSplitRatio = useCallback((next: number, persist = false) => {
    const width = splitLayoutRef.current?.getBoundingClientRect().width || window.innerWidth
    const ratio = clampWorkflowSplitRatio(next, width)
    workspaceSplitRatioRef.current = ratio
    setWorkspaceSplitRatio(ratio)
    if (persist) writeWorkflowSplitPreference(workspacePath, ratio, reportPreviewPreference)
  }, [reportPreviewPreference, workspacePath])

  const { start: startSplitDrag, stop: stopSplitDrag } = usePointerDrag()
  useEffect(() => stopSplitDrag, [stopSplitDrag, workspacePath, reportPreviewPreference, showChatArea, showWorkspacePane])
  const handleSplitPointerDown = useCallback((event: React.PointerEvent<HTMLButtonElement>) => {
    if (window.innerWidth < 768) return
    const container = splitLayoutRef.current
    if (!container) return
    const rect = container.getBoundingClientRect()
    if (!rect.width) return
    startSplitDrag(event, {
      onMove: clientX => setSplitRatio((clientX - rect.left) / rect.width),
      onEnd: () => writeWorkflowSplitPreference(workspacePath, workspaceSplitRatioRef.current, reportPreviewPreference),
    })
  }, [reportPreviewPreference, setSplitRatio, startSplitDrag, workspacePath])

  const collapseWorkspaceFromRail = useCallback(() => {
    setShowWorkspacePane(false)
    setWorkspaceMinimized(true)
  }, [setShowWorkspacePane, setWorkspaceMinimized])

  const collapseChatFromRail = useCallback(() => {
    setShowChatArea(false)
    setFocusedPane('preview')
  }, [setFocusedPane, setShowChatArea])

  // A workflow opens in whatever device and split width the user last chose
  // for it (the two sync effects below re-read both on every switch). Nothing
  // on the open path writes a layout preference -- see the invariant in
  // utils/reportPreviewPreference.ts.

  const createFreshWorkflowBuilderTab = useCallback(async (presetId: string) => {
    const tabId = await ensureWorkflowBuilderTab(presetId)
    activateTab(tabId)
    setShowChatArea(true)
  }, [setShowChatArea])

  useEffect(() => {
    // Re-read this workflow's scoped preference (default Tablet only when it
    // has never been chosen) on mount, on
    // workflow switch, and whenever the device changes (event/storage).
    const syncReportPreviewPreference = () => {
      setReportPreviewPreference(readReportPreviewPreference(workspacePath))
    }
    syncReportPreviewPreference()

    window.addEventListener(REPORT_PREVIEW_PREFERENCE_CHANGED_EVENT, syncReportPreviewPreference as EventListener)
    window.addEventListener('storage', syncReportPreviewPreference)

    return () => {
      window.removeEventListener(REPORT_PREVIEW_PREFERENCE_CHANGED_EVENT, syncReportPreviewPreference as EventListener)
      window.removeEventListener('storage', syncReportPreviewPreference)
    }
  }, [workspacePath])

  const workspacePaneVisible = !showChatArea || showWorkspacePane
  // The device tier controls the OUTER workspace pane for every workspace view,
  // not only Plan and Report. Cost, Logs, Learnings, KB, DB, and Files are peer
  // destinations in the same workspace and must retain the same layout choice.
  //
  //   mobile  → preview/files 480px column, chat takes the rest (review-style)
  //   tablet  → equal 50/50 split between chat and preview
  //   laptop  → compact mobile-width chat beside the full desktop workspace
  //   default → 50/50 split (no preview pref, or running in non-preview views)
  const isResponsiveWorkspaceCanvas = showChatArea && workspacePaneVisible
  const previewPaneTier: 'mobile' | 'tablet' | 'laptop' | null = isResponsiveWorkspaceCanvas
    ? reportPreviewPreference === 'mobile'
      ? 'mobile'
      : reportPreviewPreference === 'tablet'
        ? 'tablet'
      : reportPreviewPreference === 'desktop'
        ? 'laptop'
        : null
    : null
  // Backward-compat alias kept for downstream readers — mobile pane behaviour
  // is unchanged.
  const shouldUseMobileReportPane = previewPaneTier === 'mobile'
  const isWorkspaceViewActive = isWorkspacePaneView(workflowWorkspaceView)
  // A narrow workflow viewport is a single-pane surface. The shared toolbar
  // remains above it and focusedPane decides whether chat or workspace owns
  // the content row. At md+ both panes remain visible as the normal split.
  const chatPaneVisibilityClass =
    workspacePaneVisible && focusedPane === 'preview'
      ? 'hidden md:flex'
      : 'flex'
  // The report preview preference drives the outer pane width:
  //   mobile/files → right pane 480px, chat fills the rest (chat is col 1, pane col 2)
  //   tablet → report/flow and chat each take half the available width
  //   laptop → mobile-width chat, report/flow takes the remaining width
  //   default → normal split pane
  // The divider sits between chat and the workspace. Each device preview keeps
  // its own saved ratio, so Mobile, Tablet, and Laptop can be switched without
  // one mode inheriting another mode's layout.
  const splitLayoutClassName = !showChatArea
    ? 'flex-1 min-h-0 flex flex-col'
    : 'flex-1 min-h-0 grid grid-cols-1 grid-rows-[auto_minmax(0,1fr)] md:[grid-template-columns:var(--workflow-split-columns)] md:transition-[grid-template-columns] md:duration-150 md:ease-out'
  const splitLayoutStyle = showChatArea && workspacePaneVisible
    ? ({ '--workflow-split-columns': `minmax(240px, ${workspaceSplitRatio}fr) minmax(240px, ${1 - workspaceSplitRatio}fr)` } as React.CSSProperties)
    : undefined
  const canvasPaneClassName = !showChatArea
    ? 'flex-1 min-h-0 min-w-0'
    : !workspacePaneVisible
      ? 'hidden'
      : `min-h-0 min-w-0 w-full col-start-1 row-start-2 md:w-auto md:col-start-2 md:row-start-2 ${focusedPane === 'chat' ? 'hidden md:block' : ''} ${isWorkspaceViewActive ? 'md:border-l md:border-border' : ''}`

  // Load execution_defaults from workflow.json when workspace changes
  useEffect(() => {
    if (!workspacePath) return
    workflowManifestApi.getWorkflowManifest(workspacePath)
      .then(response => {
        const defaults = response?.manifest?.execution_defaults
        if (!defaults) return
        // Load global step overrides from execution_defaults
        const hasOverrides = defaults.disable_learning !== undefined ||
          defaults.disable_parallel_tool_execution !== undefined ||
          defaults.execution_max_turns !== undefined ||
          (defaults.enabled_custom_tools && defaults.enabled_custom_tools.length > 0)
        if (hasOverrides) {
          setStepOverride({
            disable_learning: defaults.disable_learning !== undefined ? defaults.disable_learning : undefined,
            disable_parallel_tool_execution: defaults.disable_parallel_tool_execution !== undefined ? defaults.disable_parallel_tool_execution : undefined,
            execution_max_turns: defaults.execution_max_turns,
            enabled_custom_tools: defaults.enabled_custom_tools,
          })
        } else {
          setStepOverride(null)
        }
      })
      .catch(() => { /* manifest may not exist yet, use defaults */ })
  }, [workspacePath, setStepOverride])

  // Auto-expand selectedRunFolder and selected groups in workspace sidebar whenever they change
  useEffect(() => {
    // Guard: WorkflowLayout stays mounted (hidden via CSS) in non-workflow modes.
    // Without this check, the fetchFiles(workspacePath) below fires in multi-agent
    // mode and overwrites the workspace file tree with workflow-scoped files,
    // leaving the multi-agent sidebar showing "No files found".
    const activeMode = useModeStore.getState().selectedModeCategory
    if (activeMode !== 'workflow') {
      return
    }

    const selectionKey = selectedRunFolder && selectedRunFolder !== 'new' && workspacePath
      ? `${workspacePath}::${selectedRunFolder}::${(selectedGroupIds ?? []).slice().sort().join(',')}`
      : null

    if (!selectionKey) {
      lastWorkspaceRunExpansionKeyRef.current = null
      return
    }

    if (selectedRunFolder && selectedRunFolder !== 'new' && workspacePath) {
      // Skip fetch when workspace panel is minimized — mark stale for manual refresh
      if (workspaceMinimized) {
        lastWorkspaceRunExpansionKeyRef.current = selectionKey
        useWorkspaceStore.getState().setNeedsRefresh(true)
        return
      }

      if (lastWorkspaceRunExpansionKeyRef.current === selectionKey) {
        return
      }

      // Expand folders in workspace sidebar — skip redundant fetch if Workspace.tsx already loaded files.
      // Workspace.tsx:718 fetches activeFolder on mount/change, so files should already be present.
      const ensureFiles = useWorkspaceStore.getState().files.length > 0
        ? Promise.resolve()
        : fetchFiles(workspacePath || undefined)
      ensureFiles.then(() => {
        // Collapse all other iteration folders first
        const workspaceStore = useWorkspaceStore.getState()
        const expandedFolders = workspaceStore.expandedFolders
        const runsPath = `${workspacePath}/runs`

        // Filter out all iteration-related folders from expandedFolders
        const newExpandedFolders = new Set<string>()
        expandedFolders.forEach(folder => {
          // Keep folders that are NOT under runs/iteration-*
          // Check all patterns: full paths, relative paths, and iteration folders
          const isIterationFolder =
            folder.includes('/runs/iteration-') ||           // Full path: "Workflow/ICICI/runs/iteration-3"
            /^runs\/iteration-/.test(folder) ||             // Relative: "runs/iteration-3/group-1"
            /^iteration-\d+/.test(folder)                   // Just iteration: "iteration-3"

          if (!isIterationFolder) {
            newExpandedFolders.add(folder)
          }
        })


        // Add the runs folder itself to keep it expanded (both full and relative paths)
        newExpandedFolders.add(runsPath)
        newExpandedFolders.add('runs') // Relative path

        // Extract iteration folder from selectedRunFolder (e.g., "iteration-3" from "iteration-3/group-1")
        const iterationFolder = selectedRunFolder.includes('/')
          ? selectedRunFolder.split('/')[0]
          : selectedRunFolder

        // Add all parent folders of the iteration
        const iterationPath = `${workspacePath}/runs/${iterationFolder}`
        const iterationPathParts = iterationPath.split('/')
        let currentPath = ''
        for (const part of iterationPathParts) {
          currentPath = currentPath ? `${currentPath}/${part}` : part
          newExpandedFolders.add(currentPath)
        }

        // Also add relative paths for iteration
        newExpandedFolders.add(`runs/${iterationFolder}`)
        newExpandedFolders.add(iterationFolder)

        // If we have selected groups, expand all of them
        if (selectedGroupIds && selectedGroupIds.length > 0 && variablesManifest?.groups) {
          selectedGroupIds.forEach(groupId => {
            // Find the group to get its name
            const group = variablesManifest.groups?.find(g => g.name === groupId)

            // Use sanitized name for folder naming
            const folderName = group?.name
              ? sanitizeDisplayNameForFolder(group.name)
              : groupId

            // Build the full group path
            const groupPath = `${workspacePath}/runs/${iterationFolder}/${folderName}`

            // Add all parent folders of this group path
            const groupPathParts = groupPath.split('/')
            let groupCurrentPath = ''
            for (const part of groupPathParts) {
              groupCurrentPath = groupCurrentPath ? `${groupCurrentPath}/${part}` : part
              newExpandedFolders.add(groupCurrentPath)
            }

            // Also add relative paths
            newExpandedFolders.add(`runs/${iterationFolder}/${folderName}`)
          })
        }
        // Legacy code removed: selectedRunFolder no longer contains group paths
        // Group selection is now exclusively via selectedGroupIds array

        // Update the expanded folders using the proper setter
        setExpandedFolders(newExpandedFolders)
        lastWorkspaceRunExpansionKeyRef.current = selectionKey
      }).catch(error => {
        logger.error('WorkflowLayout', 'Failed to fetch files for auto-expansion:', error)
      })
    }
  }, [selectedRunFolder, selectedGroupIds, workspacePath, variablesManifest, fetchFiles, setExpandedFolders, workspaceMinimized])

  // Callback ref that gets called when ChatArea mounts/unmounts
  const chatAreaCallbackRef = useCallback((node: ChatAreaRef | null) => {
    chatAreaRef.current = node

    // When ChatArea mounts and we have a pending query, submit it
    if (node && pendingQueryRef.current) {
      const { query, executionOptions } = pendingQueryRef.current
      logger.debug('WorkflowLayout', 'ChatArea mounted, submitting pending query:', {
        query,
        hasExecutionOptions: Boolean(executionOptions)
      })
      node.submitQuery(query, executionOptions).catch(error => {
        logger.error('WorkflowLayout', 'Failed to submit pending query:', error)
      })
      pendingQueryRef.current = null // Clear pending query after submission
    }
  }, [])

  // The global workspace toggle now maps to the workflow's right-side Files
  // pane instead of the old app-level far-right file column.
  //
  // The report preview is exempt: un-minimizing the workspace while Report is
  // open leaves it alone instead of auto-switching to Files — click Files to
  // get there. (The exemption was originally added because the
  // pane host remounted the whole pane per view kind, so the forced switch and
  // its immediate reversal flashed the report; WorkspaceViewHost keeps the pane
  // mounted across switches now, but the leave-the-preview-alone behavior is
  // kept as-is.)
  useEffect(() => {
    if (selectedModeCategory !== 'workflow') return
    const onPreviewView = isPreviewView(workflowWorkspaceView)
    if (!workspaceMinimized && !onPreviewView && (workflowWorkspaceView !== 'files' || !showWorkspacePane)) {
      setShowWorkspacePane(true)
      setWorkflowWorkspaceView('files')
      return
    }
    if (workspaceMinimized && workflowWorkspaceView === 'files') {
      setWorkflowWorkspaceView(lastCanvasView)
    }
  }, [selectedModeCategory, workspaceMinimized, workflowWorkspaceView, showWorkspacePane, lastCanvasView, setShowWorkspacePane, setWorkflowWorkspaceView])

  // Auto-minimize the file workspace sidebar when entering Report so the report
  // has room. Do not reopen it on exit: workflow switches can unmount/remount
  // this layout, and auto-reopening makes the workspace look default-open.
  //
  // Act only on workflowWorkspaceView transitions. While Report is active the
  // user is free to manually reopen the workspace — re-running this effect on
  // workspaceMinimized changes must not fight them and re-close it.
  //
  // Gated on workflow mode — this component stays mounted in multiagent mode
  // via `hidden` CSS, and without the guard the Report-minimize would leak
  // into multiagent's workspace.
  useEffect(() => {
    if (selectedModeCategory !== 'workflow') {
      prevWorkflowWorkspaceViewRef.current = workflowWorkspaceView
      return
    }

    const prev = prevWorkflowWorkspaceViewRef.current
    prevWorkflowWorkspaceViewRef.current = workflowWorkspaceView

    if (workflowWorkspaceView === 'report' && prev !== 'report') {
      if (!workspaceMinimized) {
        reportAutoMinimizedWorkspaceRef.current = true
        setWorkspaceMinimized(true)
      }
      return
    }

    if (workflowWorkspaceView !== 'report' && prev === 'report') {
      reportAutoMinimizedWorkspaceRef.current = false
    }
  }, [selectedModeCategory, workflowWorkspaceView, workspaceMinimized, setWorkspaceMinimized])

  useEffect(() => {
    return () => {
      if (reportAutoMinimizedWorkspaceRef.current) {
        reportAutoMinimizedWorkspaceRef.current = false
      }
    }
  }, [])

  // Track reconnection by preset to prevent duplicate tabs while still allowing
  // Ctrl+K workflow switches to run the reconnect decision for that preset.
  const reconnectedPresetIdsRef = useRef<Set<string>>(new Set())
  const runningWorkflowReconcileInFlightRef = useRef(false)

  // Reconnect workflow tabs on page refresh and first visit to each workflow preset.
  useEffect(() => {
    if (!activePresetId) {
      return
    }
    if (reconnectedPresetIdsRef.current.has(activePresetId)) {
      return
    }

    const reconnectWorkflowTabs = async () => {
      reconnectedPresetIdsRef.current.add(activePresetId)
      // Wait for zustand to rehydrate persisted tabs from localStorage.
      // Without this, chatTabs is empty and dedup fails → duplicate tabs.
      await waitForChatStoreHydration()
      try {
        const { closeTab, createChatTab, getTabEvents, setTabStreaming } = useChatStore.getState()
        const { getPhaseById } = useWorkflowStore.getState()
        const getExistingWorkflowTabsForPreset = () =>
          Object.values(useChatStore.getState().chatTabs)
            .filter(t =>
              t.metadata?.mode === 'workflow' &&
              t.metadata?.presetQueryId === activePresetId
            )
            .sort((a, b) => workflowTabSortTimestamp(b) - workflowTabSortTimestamp(a))

        // 1. Get active (running) sessions from in-memory cache
        //    Include both 'workflow' (execution) and 'workflow_phase' (workflow builder, plan-improvement)
        const activeSessions = await useChatStore.getState().getActiveSessions()
        const internalChildSessionIds = new Set(activeSessions
          .filter(session => isInternalChildSession({
            parentSessionId: session.parent_session_id,
            sessionKind: session.session_kind,
          }))
          .map(session => session.session_id))

        // A previous frontend may already have persisted an internal reviewer as
        // a top-level tab. Remove that UI projection without stopping the child;
        // the backend runtime and its explicit parent link remain intact.
        for (const tab of Object.values(useChatStore.getState().chatTabs)) {
          if (tab.metadata?.mode === 'workflow' && tab.sessionId && internalChildSessionIds.has(tab.sessionId)) {
            await closeTab(tab.tabId, false)
          }
        }

        const activeWorkflowSessions = activeSessions.filter(s =>
          !internalChildSessionIds.has(s.session_id) &&
          (s.agent_mode === 'workflow' || s.agent_mode === 'workflow_phase')
        )

        // 2. Skip DB session restore — only active (running) sessions should auto-create tabs.
        //    Old completed sessions from DB were creating unwanted tabs every time you
        //    open a workflow. Workflow builder conversations are saved to workspace files,
        //    not restored from DB sessions.
        const dbSessions: import('../../services/api-types').ChatHistorySummary[] = []

        // Build a combined list — active sessions first, then recent DB sessions (deduped)
        const activeSessionIds = new Set(activeWorkflowSessions.map(s => s.session_id))
        const runningWorkflowsBySession = new Map<string, RunningWorkflowInfo>()
        try {
          const response = await agentApi.listRunningWorkflows()
          for (const running of response.running || []) {
            if (running.session_id) {
              runningWorkflowsBySession.set(running.session_id, running)
            }
          }
        } catch {
          /* Running registry is an enhancement here; active sessions still restore below. */
        }
        const sessionsToRestore: Array<{
          sessionId: string
          query?: string
          title?: string
          status: string
          isActive: boolean
          phaseId?: string
          phaseName?: string
          triggeredBy?: string
          botPlatform?: string
          isScheduledRun?: boolean
          preloadedEvents?: PollingEvent[]
          lastProcessedIndex?: number
        }> = []
        const queuedSessionIds = new Set<string>()

        // Add active sessions that belong to this preset. We read the
        // running-workflow registry (workflow-owned storage) instead of
        // reaching into the chat session metadata.
        // Match live sessions through preset ID first, then workspace path.
        // This covers older workflow_phase sessions where the tracker knows the
        // workspace but not the preset ID, without attaching unknown sessions to
        // whatever workflow happens to be active.
        // Fallback: if no registry lookup resolved a preset, allow the session
        // through only when its persisted chat tab already binds it to the
        // current preset (so reload doesn't drop a tab the user has been using).
        const chatTabsById = useChatStore.getState().chatTabs
        for (const s of activeWorkflowSessions) {
          const registryRunning = runningWorkflowsBySession.get(s.session_id)
          const scheduledSession = isScheduledSession({
            sessionId: s.session_id,
            triggeredBy: s.triggered_by,
          }) || Boolean(registryRunning && isScheduledSession({
            sessionId: registryRunning.session_id,
            triggeredBy: registryRunning.triggered_by,
          }))
          if (
            (isExternalReadOnlyActiveWorkflowSession(s) && !scheduledSession) ||
            (registryRunning && isExternalReadOnlyWorkflowEntry(registryRunning) && !scheduledSession)
          ) {
            continue
          }
          let belongsToPreset = isLiveWorkflowSessionForPreset(s, activePresetId, workspacePath)
          try {
            const running = registryRunning || await agentApi.getRunningWorkflow(s.session_id)
            if (running.preset_query_id) {
              belongsToPreset = running.preset_query_id === activePresetId
            } else if (workspacePath && running.workspace_path) {
              belongsToPreset = normalizeWorkflowPath(running.workspace_path) === normalizeWorkflowPath(workspacePath)
            }
          } catch {
            /* registry miss — fall through to persisted-tab check below */
          }
          if (!belongsToPreset) {
            const persistedTab = Object.values(chatTabsById).find(
              t => t.sessionId === s.session_id && t.metadata?.mode === 'workflow'
            )
            if (persistedTab?.metadata?.presetQueryId === activePresetId) {
              belongsToPreset = true
            }
          }
          if (!belongsToPreset) continue
          queuedSessionIds.add(s.session_id)
          sessionsToRestore.push({
            sessionId: s.session_id,
            query: s.query,
            title: s.title,
            status: s.status,
            isActive: true,
            triggeredBy: s.triggered_by,
            botPlatform: s.bot_platform,
            isScheduledRun: scheduledSession,
          })
        }

        for (const running of runningWorkflowsBySession.values()) {
          if (!running.session_id || queuedSessionIds.has(running.session_id)) continue
          const scheduledSession = isScheduledSession({
            sessionId: running.session_id,
            triggeredBy: running.triggered_by,
          })
          // Scheduled jobs are first-class parallel workflow tabs. Bots remain
          // external-only, but skipping schedules here made a workflow boot
          // open a blank Builder tab while the live schedule was invisible.
          if (isExternalReadOnlyWorkflowEntry(running) && !scheduledSession) continue
          const belongsToPreset = runningWorkflowBelongsToPreset(running, activePresetId, workspacePath)
          if (!belongsToPreset) continue
          queuedSessionIds.add(running.session_id)
          sessionsToRestore.push({
            sessionId: running.session_id,
            query: running.query,
            title: running.title || running.preset_name || running.phase_name,
            status: running.status || 'running',
            isActive: true,
            phaseId: running.phase_id,
            phaseName: running.phase_name,
            triggeredBy: running.triggered_by,
            isScheduledRun: scheduledSession,
          })
        }

        // Auto-restore is limited to sessions the backend reports as
        // active/running (handled above) plus tabs already persisted in this
        // browser. Finished/saved conversations — of any origin (builder,
        // schedule, bot) — are never auto-opened; the user reopens them
        // explicitly via Resume from the history list.

        // Add the most recent DB session not already in active list
        // Only show completed/running/error sessions (skip dismissed/inactive)
        // Only restore the latest session — older ones stay in history
        const recentDbSessions = dbSessions
          .filter(s => !activeSessionIds.has(s.session_id) && s.status !== 'dismissed' && s.status !== 'inactive')
          .slice(0, 1)
        for (const s of recentDbSessions) {
          const config = s.config && typeof s.config === 'object'
            ? s.config as Record<string, unknown>
            : {}
          const wfMeta = config.workflow_metadata && typeof config.workflow_metadata === 'object'
            ? config.workflow_metadata as Record<string, unknown>
            : {}
          // Try to extract phaseId from metadata, config, or agent_mode
          let phaseId = typeof wfMeta.phase_id === 'string' ? wfMeta.phase_id : undefined
          if (!phaseId && s.agent_mode === 'workflow_phase') {
            // workflow_phase sessions store phase_id in config
            phaseId = typeof config.phase_id === 'string' ? config.phase_id : undefined
          }
          if (!phaseId && s.title) {
            // Fallback: try to extract from title
            const match = s.title.match(/(?:(?:workflow|automation)[- ]builder|planning|evaluation[- ]builder)/i)
            if (match) phaseId = match[0].toLowerCase().replace(/\s/g, '-')
          }
          sessionsToRestore.push({
            sessionId: s.session_id,
            query: undefined,
            title: s.title,
            status: s.status,
            isActive: false,
            phaseId,
            phaseName: typeof wfMeta.phase_name === 'string' ? wfMeta.phase_name : undefined
          })
        }

        // 3. Split sessions into (a) those we need to create a tab for and
        //    (b) those whose tab is already persisted in localStorage but whose
        //    events were never hydrated (workflow events live only in the
        //    in-memory EventStore, not in DB/localStorage, so a page refresh
        //    leaves persisted tabs looking empty until we pull them back).
        const { chatTabs } = useChatStore.getState()
        const existingTabsBySession = new Map<string, string>()
        Object.values(chatTabs).forEach(t => {
          if (t.metadata?.mode === 'workflow' && t.sessionId) {
            existingTabsBySession.set(t.sessionId, t.tabId)
          }
        })
        const newSessions = sessionsToRestore.filter(s => !existingTabsBySession.has(s.sessionId))
        const existingWorkflowTabs = getExistingWorkflowTabsForPreset()
        const interactiveExistingWorkflowTabs = existingWorkflowTabs.filter(tab => !tab.metadata?.isViewOnly)
        const chatStoreForViewMode = useChatStore.getState()
        const activeWorkflowViewMode = normalizeEventViewMode(
          (chatStoreForViewMode.activeTabId ? chatStoreForViewMode.chatTabs[chatStoreForViewMode.activeTabId]?.viewMode : undefined) ||
          chatStoreForViewMode.eventViewModePreference
        )
        const shouldHydrateWorkflowEvents = activeWorkflowViewMode === 'formatted'

        // Only restore sessions that don't have tabs yet
        const sessionsToActuallyRestore = newSessions

        // 3a. Rehydrate events for persisted tabs whose event buffer was lost
        //     on refresh -- read-only scheduled/bot run tabs included (see
        //     rehydrateWorkflowTabs); they were excluded here once, which is
        //     how a finished run's tab got stuck on "Restoring previous
        //     session..." after every backend restart.
        const tabsNeedingHydration = shouldHydrateWorkflowEvents
          ? workflowTabsNeedingHydration(existingWorkflowTabs, getTabEvents)
          : []
        if (tabsNeedingHydration.length > 0) {
          await rehydrateWorkflowTabs(existingWorkflowTabs, workspacePath)
        }

        // 4. Create tabs and load events for new sessions only
        let lastTabId: string | null = null
        for (const session of sessionsToActuallyRestore) {
          // Extract phase ID from workflow metadata, query, or title
          let phaseId: string | null = session.phaseId || null
          if (!phaseId) {
            const queryStr = session.query || session.title || ''
            const match = queryStr.match(/(?:Execute workflow phase:|phase:)\s*(\w+)/i)
            if (match && match[1]) {
              phaseId = match[1]
            }
          }

          const phase = phaseId ? getPhaseById(phaseId) : null
          // Naming priority:
          //   1. Explicit phase / phaseName from the session record
          //   2. The session's Title (scheduled runs get the schedule name
          //      stamped here by stampScheduleNameOnSession on the backend;
          //      regular workflow runs may have a meaningful title too)
          //   3. Fallback to "Schedule" / "Bot" when we know the trigger,
          //      so a scheduled run reconnected on app boot doesn't get
          //      labelled the literal "Workflow"
          //   4. Last resort: phaseId / "Automation Builder" (so the chat
          //      input gating in WorkflowChatTabs treats it as the
          //      builder tab and shows the proper "Chat" label)
          let phaseName: string
          if (session.phaseName || phase?.title || session.title) {
            phaseName = session.phaseName || phase?.title || session.title || ''
          } else if (session.isScheduledRun || session.triggeredBy === 'cron') {
            phaseName = 'Schedule'
          } else if (session.botPlatform) {
            phaseName = session.botPlatform
          } else {
            phaseName = phaseId || 'Automation Builder'
          }

          // Create tab with scheduled-run / bot metadata so downstream
          // UI (chat-input toggle, view-only banner, badge icons) treats
          // it as a read-only observer of an external trigger.
          const isScheduled = session.isScheduledRun || session.triggeredBy === 'cron'
          const isBot = Boolean(session.botPlatform)
          // Same resolution as the running-workflow reconciler: a scheduled
          // run rediscovered on boot takes over its own schedule's finished
          // tab instead of opening a second one beside it.
          const { tabId } = await resolveWorkflowTabForSession({
            getTabs: () => useChatStore.getState().chatTabs,
            presetQueryId: activePresetId,
            sessionId: session.sessionId,
            name: phaseName,
            metadata: {
              mode: 'workflow',
              phaseId: phaseId || undefined,
              phaseName,
              presetQueryId: activePresetId,
              isViewOnly: isScheduled || isBot ? true : undefined,
              isScheduledRun: isScheduled || undefined,
              scheduledJobName: isScheduled ? (session.title || phaseName) : undefined,
              isBotRun: isBot || undefined,
              botPlatform: isBot ? session.botPlatform : undefined,
            },
            createChatTab,
            updateTabSessionId: useChatStore.getState().updateTabSessionId,
          })

          // Workflow switches default to terminal/report surfaces. Event history
          // is only hydrated for the tree/debug view.
          if (shouldHydrateWorkflowEvents && session.preloadedEvents && session.preloadedEvents.length > 0) {
            useChatStore.getState().setTabEvents(session.sessionId, session.preloadedEvents)
            useChatStore.getState().setTabLastEventIndex(
              session.sessionId,
              session.lastProcessedIndex ?? session.preloadedEvents.length - 1,
            )
            setTabStreaming(tabId, session.isActive)
            useChatStore.getState().setTabCompleted(tabId, !session.isActive)
          } else if (shouldHydrateWorkflowEvents) {
            useChatStore.getState().beginWorkflowSessionRestore(session.sessionId)
            try {
              await withWorkflowRestoreTimeout(
                // A scheduled run can outlive the server's EventStore. Its
                // workspace conversation is durable, so use it as the
                // immediate fallback instead of repeatedly restoring an empty
                // volatile buffer and leaving the Schedule lane on a spinner.
                restoreWorkflowStateFromEvents(session.sessionId, workspacePath, isScheduled),
                `Restoring workflow events for ${session.sessionId}`
              )
              if (session.isActive || session.status === 'running') {
                setTabStreaming(tabId, true)
              }
            } catch (err) {
              console.warn('[WorkflowReconnect] Failed to load events for', session.sessionId, err)
            } finally {
              useChatStore.getState().endWorkflowSessionRestore(session.sessionId)
            }
          } else {
            setTabStreaming(tabId, session.isActive || session.status === 'running')
            useChatStore.getState().setTabCompleted(tabId, !(session.isActive || session.status === 'running'))
          }

          lastTabId = tabId
        }

        // Reconnect may create/update tabs, but the active tab is user-owned.
        // Preserve any valid tab for this workflow—Chat, Schedule, or Bot—and
        // auto-select only when the current tab belongs elsewhere or is absent.
        {
          const store = useChatStore.getState()
          if (activeWorkflowTabIdForPreset(store.activeTabId, activePresetId, store.chatTabs)) {
            setShowChatArea(true)
            return
          }
        }

        // 5. Show the chat area with the last tab
        if (lastTabId) {
          activateTab(lastTabId)
          setShowChatArea(true)
        }

        // 6. If no tabs were created/restored, show the Builder tab. The
        // previous-automation-chats panel offers explicit Resume actions;
        // simply opening/selecting a workflow restores only what was already
        // open and never silently resumes the latest saved chat.
        if (!lastTabId) {
          const store = useChatStore.getState()
          if (interactiveExistingWorkflowTabs.length === 0) {
            activateTab(await ensureWorkflowBuilderTab(activePresetId))
            setShowChatArea(true)
          } else {
            const streamingTab = interactiveExistingWorkflowTabs.find(t => t.isStreaming || store.getTabStreamingStatus(t.tabId))
            if (streamingTab) {
              activateTab(streamingTab.tabId)
              setShowChatArea(true)
              return
            } else {
              const builderTab = interactiveExistingWorkflowTabs.find(t => t.metadata?.phaseId === 'workflow-builder')
              if (builderTab) {
                activateTab(builderTab.tabId)
                setShowChatArea(true)
                return
              }
              activateTab(interactiveExistingWorkflowTabs[0].tabId)
              setShowChatArea(true)
              return
            }
          }
        }
      } catch (error) {
        console.warn('[WorkflowReconnect] Failed to reconnect workflow tabs:', error)
      } finally {
        // Whatever was restored or activated above, the Builder tab exists
        // beside it -- a workflow whose persisted tabs are all Chats or
        // read-only lanes would otherwise have none. Runs after every early
        // return above so the selection those made is kept.
        void ensureWorkflowBuilderTab(activePresetId, { keepSelection: true })
      }
    }

    const timeoutId = setTimeout(reconnectWorkflowTabs, 500)
    return () => clearTimeout(timeoutId)
  }, [activePresetId, workspacePath, setShowChatArea, rehydrateWorkflowTabs, createFreshWorkflowBuilderTab])

  useEffect(() => {
    if (!activePresetId || selectedModeCategory !== 'workflow') return

    let cancelled = false
    const reconcileRunningWorkflowTab = async () => {
      if (runningWorkflowReconcileInFlightRef.current) return
      runningWorkflowReconcileInFlightRef.current = true
      try {
        if (cancelled) return
        // Reuse the store's active-sessions cache instead of independently
        // polling /api/workflow/running — both are sourced from the same
        // backend tracker, and GlobalActivityMonitor already keeps this
        // cache fresh every 5s.
        const runningFromActiveSessions = useChatStore.getState().activeSessionsCache
          .filter(session => session.agent_mode === 'workflow' || session.agent_mode === 'workflow_phase')
          .map(activeSessionToRunningWorkflowInfo)
        const projectedRunningWorkflows = runningFromActiveSessions
          .filter(item => item.session_id && isRunningWorkflowEntry(item))
          .filter(item => runningWorkflowBelongsToPreset(item, activePresetId, workspacePath))
          .sort((a, b) => new Date(b.started_at || 0).getTime() - new Date(a.started_at || 0).getTime())
          .flatMap(running => {
            const projection = workflowRuntimeTabProjection(running, activePresetId)
            return projection ? [{ running, projection }] : []
          })

        if (projectedRunningWorkflows.length === 0) return

        const chatStore = useChatStore.getState()
        const activeTab = chatStore.activeTabId ? chatStore.chatTabs[chatStore.activeTabId] : undefined
        const activeViewMode = normalizeEventViewMode(activeTab?.viewMode || chatStore.eventViewModePreference)
        const activeTabOwnedByPreset = Boolean(
          activeWorkflowTabIdForPreset(chatStore.activeTabId, activePresetId, chatStore.chatTabs)
        )
        const shouldSwitch = !activeTabOwnedByPreset

        let selectedRunningTabId: string | null = null
        for (const { running, projection } of projectedRunningWorkflows) {
          if (!running.session_id) continue

          // Existing tab, a finished lane of the same schedule, or a new
          // tab -- one decision shared with the reconnect path and the
          // activity monitor (utils/workflowTabResolution.ts).
          const latestChatStore = useChatStore.getState()
          const { tabId } = await resolveWorkflowTabForSession({
            getTabs: () => useChatStore.getState().chatTabs,
            presetQueryId: activePresetId,
            sessionId: running.session_id,
            name: projection.name,
            metadata: projection.metadata,
            createChatTab: latestChatStore.createChatTab,
            updateTabSessionId: latestChatStore.updateTabSessionId,
          })
          if (projection.autoActivate) selectedRunningTabId ||= tabId

          // Runtime discovery may refresh status for an observed run, but an
          // explicit Schedule/Bot -> Chat promotion is user-owned state. Apply
          // both name and metadata atomically so a polling tick cannot turn the
          // interactive continuation back into a read-only Schedule tab.
          useChatStore.setState(state => {
            const tab = state.chatTabs[tabId]
            if (!tab) return state
            return {
              chatTabs: {
                ...state.chatTabs,
                [tabId]: reconcileWorkflowRuntimeTab(tab, projection),
              },
            }
          })
          chatStore.setTabStreaming(tabId, true)
          chatStore.setTabCompleted(tabId, false)
          chatStore.setTabViewMode(tabId, activeViewMode)

          // This reconciler often discovers a scheduled run after it has
          // already emitted its opening message and several tool/stream
          // events. SSE only guarantees delivery from the live subscription
          // point onward, so explicitly catch up the formatted transcript
          // before relying on the stream for future events.
          if (shouldCatchUpRunningWorkflowTranscript(
            activeViewMode,
            useChatStore.getState().getTabEvents(running.session_id).length,
          )) {
            useChatStore.getState().beginWorkflowSessionRestore(running.session_id)
            try {
              await withWorkflowRestoreTimeout(
                restoreWorkflowStateFromEvents(
                  running.session_id,
                  workspacePath,
                  projection.metadata.isScheduledRun === true,
                ),
                `Restoring workflow events for ${running.session_id}`
              )
            } catch (error) {
              logger.warn('WorkflowLayout', 'Failed to hydrate newly discovered running workflow tab:', error)
            } finally {
              useChatStore.getState().endWorkflowSessionRestore(running.session_id)
            }
          }

        }

        if (shouldSwitch && selectedRunningTabId) {
          activateTab(selectedRunningTabId)
          setShowChatArea(true)
        } else if (activeTab?.sessionId && projectedRunningWorkflows.some(item => item.running.session_id === activeTab.sessionId)) {
          setShowChatArea(true)
        }
      } catch {
        /* Global activity monitor remains the source of truth if this lightweight reconcile misses. */
      } finally {
        runningWorkflowReconcileInFlightRef.current = false
      }
    }

    void reconcileRunningWorkflowTab()
    // Reconcile only ensures the active tab matches the latest running workflow
    // after a tab switch / app boot — it doesn't need sub-second cadence.
    // useRunningWorkflowsStore polls at 2–10s for live status; this slower tick
    // just catches occasional drift.
    const interval = window.setInterval(reconcileRunningWorkflowTab, 10000)
    return () => {
      cancelled = true
      window.clearInterval(interval)
    }
  }, [activePresetId, selectedModeCategory, setShowChatArea, workspacePath])


  // Auto-minimize workflows when switching to a different preset
  useEffect(() => {
    // Skip on initial mount (when previousPresetIdRef.current is null)
    if (previousPresetIdRef.current === null) {
      previousPresetIdRef.current = activePresetId
      return
    }

    // Skip auto-minimize during restore operations (flag is set by RunningWorkflowsDrawer)
    const isRestoringWorkflow = useRunningWorkflowsStore.getState().isRestoringWorkflow
    if (isRestoringWorkflow) {
      logger.debug('WorkflowLayout', 'Skipping auto-minimize during workflow restore')
      previousPresetIdRef.current = activePresetId
      return
    }

    // Check if preset actually changed (not just deps like selectedRunFolder)
    if (previousPresetIdRef.current !== activePresetId && activePresetId) {
      // Update ref immediately so dep-only re-fires don't re-enter this block
      const oldPreset = previousPresetIdRef.current
      previousPresetIdRef.current = activePresetId

      console.log(`%c[WorkflowLayout] Preset changed: ${oldPreset?.slice(0,8)} → ${activePresetId?.slice(0,8)}`, 'color: #FF9800; font-weight: bold')
      console.time(`[WorkflowLayout] preset-switch-effect-${activePresetId?.slice(0,8)}`)

      const chatStore = useChatStore.getState()
      const chatTabs = chatStore.chatTabs

      // Tabs from the old preset stay in memory with their events (hidden by preset filter).
      // We keep events because workflow events aren't stored in DB — clearing them would lose
      // them permanently if the backend's EventStore has already cleaned up.
      // Side effects (workspace refresh, canvas updates) are already skipped for non-active
      // preset tabs via the isActivePresetTab guard in processEventsResponse.

      // Switch active tab to one belonging to the new preset (or close chat area)
      const newPresetTabs = Object.values(chatTabs)
        .filter(t =>
          t.metadata?.mode === 'workflow' &&
          t.metadata?.presetQueryId === activePresetId
        )
        .sort((a, b) => workflowTabSortTimestamp(b) - workflowTabSortTimestamp(a))

      if (newPresetTabs.length > 0) {
        const pendingReadOnlyRestore = pendingReadOnlyRestoreRef.current
        const restoredReadOnlyTab = pendingReadOnlyRestore?.presetId === activePresetId
          ? newPresetTabs.find(t => t.tabId === pendingReadOnlyRestore.tabId && t.metadata?.isViewOnly)
          : undefined
        pendingReadOnlyRestoreRef.current = null
        // Prefer a read-only Schedule/Bot tab only for the immediate restore action.
        // Normal preset switches should not keep reopening stale scheduled-run tabs.
        const interactiveTabs = newPresetTabs.filter(t => !t.metadata?.isViewOnly)
        if (!restoredReadOnlyTab && interactiveTabs.length === 0) {
          void createFreshWorkflowBuilderTab(activePresetId)
          console.timeEnd(`[WorkflowLayout] preset-switch-effect-${activePresetId?.slice(0,8)}`)
          return
        }
        const streamingTab = interactiveTabs.find(t => chatStore.getTabStreamingStatus(t.tabId))
        const builderTab = interactiveTabs.find(t => t.metadata?.phaseId === 'workflow-builder')
        const targetTab = restoredReadOnlyTab || streamingTab || builderTab || interactiveTabs[0] || newPresetTabs[0]
        console.log(`[WorkflowLayout] Switching to tab: ${targetTab.tabId.slice(0,8)} (${newPresetTabs.length} tabs for preset, restoredReadOnly=${!!restoredReadOnlyTab}, streaming=${!!streamingTab}, builder=${!!builderTab})`)
        if (restoredReadOnlyTab) {
          revealWorkflowChat(restoredReadOnlyTab.tabId)
        } else {
          // The navigation coordinator carries the user's explicit Formatted
          // or Terminal preference across workflows while changing ownership.
          activateTab(targetTab.tabId)
          setShowChatArea(true)
        }
        // The Builder tab always exists; keep the selection just made.
        void ensureWorkflowBuilderTab(activePresetId, { keepSelection: true })

        const selectedTarget = useChatStore.getState().chatTabs[targetTab.tabId]
        const targetViewMode = normalizeEventViewMode(selectedTarget?.viewMode || chatStore.eventViewModePreference)
        const tabsNeedingHydration = targetViewMode === 'formatted'
          ? workflowTabsNeedingHydration(newPresetTabs, tab => chatStore.getTabEvents(tab))
          : []
        if (tabsNeedingHydration.length > 0) {
          void rehydrateWorkflowTabs(newPresetTabs, workspacePath)
        }
      } else {
        console.log(`[WorkflowLayout] No tabs for new preset, clearing activeTabId`)
        // Clear activeTabId so the old preset's tab events don't bleed into the new preset's view
        useChatStore.setState({ activeTabId: null })
        // Respect restored per-preset showChatArea — don't force-close if it was open
        const restoredShowChatArea = useWorkflowStore.getState().showChatArea
        if (!restoredShowChatArea) {
          setShowChatArea(false)
        }
      }
      console.timeEnd(`[WorkflowLayout] preset-switch-effect-${activePresetId?.slice(0,8)}`)
    } else {
      // Update the ref for non-preset-change re-fires (dep changes only)
      previousPresetIdRef.current = activePresetId
    }
  }, [activePresetId, minimizeWorkflow, selectedRunFolder, setShowChatArea, rehydrateWorkflowTabs, createFreshWorkflowBuilderTab, workspacePath, revealWorkflowChat])

  // Note: Query submission is now handled via chatAreaCallbackRef when ChatArea mounts
  // No need for useEffect with setTimeout - callback ref is the proper React pattern

  // Handle phase start from toolbar (now accepts execution options directly)
  const handleStartPhase = useCallback(async (phaseId: string, executionOptions?: ExecutionOptions) => {
    // Ensure we're in workflow mode before starting phase
    if (activePresetId) {
      const currentMode = useModeStore.getState().selectedModeCategory
      if (currentMode !== 'workflow') {
        useModeStore.getState().setModeCategory('workflow')
      }
    }

    if (typeof phaseId !== 'string') {
      logger.error('WorkflowLayout', 'Invalid phaseId: expected string, got', typeof phaseId)
      return
    }

    if (!activePresetId) return

    const phase = getPhaseById(phaseId)
    const phaseName = phase?.title || phaseId

    // Single-pass tab lookup: find or create workflow tab
    const result = await findOrCreateWorkflowTab({ phaseId, activePresetId, phaseName })
    if (!result) {
      logger.error('WorkflowLayout', 'Failed to get or create tab for phase', phaseId)
      return
    }

    const { tab, isReusingTab } = result

    // If reusing an existing tab that's already running, just switch to view it
    if (isReusingTab && useChatStore.getState().getTabStreamingStatus(tab.tabId)) {
      logger.debug('WorkflowLayout', 'Tab already running, switching to view it')
      setShowChatArea(true)
      return
    }

    // Update workflow status in database (non-blocking)
    agentApi.updateWorkflow(activePresetId, phaseId, null, undefined).catch(error => {
      logger.error('WorkflowLayout', 'Failed to update workflow status:', error)
    })

    setCurrentWorkflowPhase(phaseId)
    // Clear any explicit view so the pane shows the last canvas view.
    setWorkflowWorkspaceView(null)

    // For chat-compatible phases, just open the tab without auto-submitting a query.
    // The user will type naturally in the chat input.
    if (isChatCompatiblePhase(phaseId)) {
      logger.debug('WorkflowLayout', `Chat-compatible phase ${phaseId} — opening tab for conversation`)
      setShowChatArea(true)
      return
    }

    // Submit the execution query
    const query = `Execute workflow phase: ${phaseId}`

    if (chatAreaRef.current) {
      // ChatArea already mounted (e.g. workflow builder was open) — submit directly
      chatAreaRef.current.submitQuery(query, executionOptions).catch(error => {
        logger.error('WorkflowLayout', 'Failed to submit execution query:', error)
      })
    } else {
      // ChatArea not mounted yet — store pending query for callback ref
      pendingQueryRef.current = { query, executionOptions }
    }

    // Show ChatArea (triggers mount if not already shown)
    setShowChatArea(true)
  }, [activePresetId, setCurrentWorkflowPhase, setShowChatArea, getPhaseById, setWorkflowWorkspaceView])

  // Handle create plan - always opens Automation Builder.
  const handleCreatePlan = useCallback(() => {
    // Ensure we're in workflow mode before creating plan (only if we have an active preset)
    if (activePresetId) {
      const currentMode = useModeStore.getState().selectedModeCategory
      if (currentMode !== 'workflow') {
        useModeStore.getState().setModeCategory('workflow')
      }
    }

    const phases = useWorkflowStore.getState().phases
    const workshopPhase = phases.find(p => p.id === 'workflow-builder')
    const phaseId = workshopPhase?.id || 'workflow-builder'
    logger.debug('WorkflowLayout', 'Create plan requested, starting workflow builder phase:', phaseId)
    setShowChatArea(true)
    handleStartPhase(phaseId)
  }, [handleStartPhase, setShowChatArea, activePresetId])

  const handleToggleChatArea = useCallback(() => {
    const newShow = !showChatArea
    if (newShow) {
      // Ensure a workflow tab is active when showing the chat panel
      // (activeTabId might point to a chat/multi-agent tab from a different mode)
      const chatStore = useChatStore.getState()
      const activeTab = chatStore.getActiveTab()
      if (
        !activeTab ||
        activeTab.metadata?.mode !== 'workflow' ||
        activeTab.metadata?.presetQueryId !== activePresetId ||
        activeTab.metadata?.isViewOnly
      ) {
        const workflowTabs = Object.values(chatStore.chatTabs)
          .filter(t =>
            isInteractiveWorkflowTab(t) &&
            t.metadata?.presetQueryId === activePresetId
          )
          .sort((a, b) => workflowTabSortTimestamp(b) - workflowTabSortTimestamp(a))
        if (workflowTabs.length > 0) {
          const builderTab = workflowTabs.find(t => t.metadata?.phaseId === 'workflow-builder')
          activateTab((builderTab || workflowTabs[0]).tabId)
        }
      }
    }
    setShowChatArea(newShow)
  }, [activePresetId, showChatArea, setShowChatArea])

  // Minimize chat area when drawer opens to reduce renders and stop event processing
  // Open chat area when drawer closes (but not on initial mount)
  const drawerMountedRef = useRef(false)
  useEffect(() => {
    if (!drawerMountedRef.current) {
      drawerMountedRef.current = true
      return
    }
    if (showRunningDrawer) {
      // Minimize chat area when drawer opens
      setShowChatArea(false)
      // When ChatArea is hidden, it will unmount, which stops:
      // 1. Event rendering (EventDisplay won't render)
      // 2. Polling management (useEffect hooks won't run)
      // This significantly reduces browser load
    } else {
      // Open chat area when drawer closes (user just closed the running workflows drawer)
      setShowChatArea(true)
    }
  }, [showRunningDrawer, setShowChatArea])

  // No preset selected state
  if (!activeWorkflowPreset && !workspacePath) {
    return (
      <div className={`flex flex-col h-full ${className}`}>

        <div className="flex-1 flex items-center justify-center bg-gray-50 dark:bg-gray-900">
        <div className="flex flex-col items-center gap-4 text-center max-w-md">
            <div className="w-20 h-20 rounded-full bg-gray-200 dark:bg-gray-700 flex items-center justify-center">
            <span className="text-4xl">🚀</span>
          </div>
          <div>
            <h2 className="text-xl font-semibold text-gray-900 dark:text-gray-100">
              Select an Automation
            </h2>
            <p className="text-sm text-gray-500 dark:text-gray-400 mt-2">
              Choose an automation preset from the sidebar to get started.
              The automation canvas will visualize your plan and let you run it step by step.
            </p>
            </div>
          </div>
        </div>
      </div>
    )
  }

  const canvasElement = (
    <WorkflowCanvas
      ref={canvasRef}
      workspacePath={workspacePath}
      presetQueryId={activePresetId}
      currentPhase={activePhase || currentWorkflowPhase}
      onStartPhase={handleStartPhase}
      onCreatePlan={onCreatePlan || handleCreatePlan}
      showChatArea={showChatArea}
      toolbarOnly={!workspacePaneVisible && showChatArea}
      // When the workspace pane is collapsed, keep the toolbar as a normal
      // first grid item. Spanning a non-existent second column would otherwise
      // leave the full-width chat occupying only half of a desktop viewport.
      sharedToolbar={showChatArea && workspacePaneVisible}
      chatTabsSlot={showChatArea ? <WorkflowChatTabs embedded /> : undefined}
      paneClassName={canvasPaneClassName}
      onToggleChatArea={handleToggleChatArea}
      className={showChatArea && !workspacePaneVisible ? '!h-auto shrink-0' : 'h-full'}
    />
  )

  return (
    <div className={`relative flex flex-col h-full ${className}`}>
      {/* Right-edge tab to re-open the preview pane after it's been collapsed
          (the on-pane collapse button hides it). Only when chat is shown and the
          pane is hidden. */}
      {showChatArea && !workspacePaneVisible && (
        <button
          type="button"
          onClick={() => setShowWorkspacePane(true)}
          title="Show report / plan panel"
          aria-label="Show report / plan panel"
          className="absolute right-0 top-1/2 z-30 hidden -translate-y-1/2 flex-col items-center gap-1.5 rounded-l-lg border border-r-0 border-border bg-background/95 py-3 pl-1.5 pr-1 text-muted-foreground shadow-md backdrop-blur-sm transition-colors hover:bg-muted hover:text-foreground md:flex"
        >
          <PanelRightOpen className="h-4 w-4" />
          <span className="[writing-mode:vertical-rl] text-[10px] font-semibold uppercase tracking-wider">Panel</span>
        </button>
      )}
      {!showChatArea && (
        <button
          type="button"
          onClick={handleToggleChatArea}
          title="Show chat panel"
          aria-label="Show chat panel"
          className="absolute left-0 top-1/2 z-30 hidden -translate-y-1/2 flex-col items-center gap-1.5 rounded-r-lg border border-l-0 border-border bg-background/95 py-3 pl-1 pr-1.5 text-muted-foreground shadow-md backdrop-blur-sm transition-colors hover:bg-muted hover:text-foreground md:flex"
        >
          <PanelLeftOpen className="h-4 w-4" />
          <span className="[writing-mode:vertical-rl] text-[10px] font-semibold uppercase tracking-wider">Chat</span>
        </button>
      )}
      {/* Main Content */}
      {/* Focus-follows-click: a mousedown anywhere in the split focuses the preview
          (gives it ~75%); the chat pane's own capture handler below overrides to
          'chat' when the click lands inside it. Capture fires outer→inner, so the
          deeper chat handler wins for chat clicks while canvas clicks stay 'preview'. */}
      <div
        ref={splitLayoutRef}
        className={splitLayoutClassName}
        style={splitLayoutStyle}
        onMouseDownCapture={showChatArea && workspacePaneVisible ? () => setFocusedPane('preview') : undefined}
      >
        {showChatArea && !workspacePaneVisible && canvasElement}

        {showChatArea && (
          <div
            data-tour="workflow-chat-pane"
            data-testid="tour-workflow-chat-pane"
            onMouseDownCapture={() => setFocusedPane('chat')}
            className={`${chatPaneVisibilityClass} col-start-1 row-start-2 min-h-0 min-w-0 overflow-hidden flex-col bg-background transition-all duration-300 ${
            workspacePaneVisible
              ? `border-b border-border md:col-start-1 md:row-start-2 md:border-b-0 md:border-r ${shouldUseMobileReportPane ? 'flex-1 md:flex-[1.35]' : 'flex-1 basis-1/2'}`
              : 'flex-1'
          }`}>
            {/* WorkflowChatTabs now renders inline in the WorkflowToolbar (chatTabsSlot
                on canvasElement above) so the tabs + status + tools share one bar. */}

            {isActiveWorkflowSessionRestoring && (
              <div className="flex items-center gap-2 border-b border-blue-100 bg-blue-50 px-3 py-1.5 dark:border-blue-800/50 dark:bg-blue-900/20">
                <div className="h-3 w-3 animate-spin rounded-full border-2 border-gray-300 border-t-blue-600 dark:border-gray-600 dark:border-t-blue-400"></div>
                <span className="text-xs text-blue-600 dark:text-blue-400">Restoring previous session...</span>
              </div>
            )}

            {/* The previous-automation-chats list now renders inside ChatArea's
                content area (workflowPreviousChatsPanel below) so it fills the
                pane above the chat input, mirroring the multi-agent landing
                panel — instead of a compact strip stacked on top of the chat. */}

            <div className="min-h-0 flex-1 overflow-hidden">
              <ChatAreaWithObserverId
                ref={chatAreaCallbackRef}
                onNewChat={onNewChat}
                hideHeader
                compact
                workflowPreviousChatsPanel={workspacePath ? (
                  <WorkflowPreviousChatsPanel
                    key={`${activePresetId || 'workflow'}:${workspacePath}`}
                    primary
                    workspacePath={workspacePath}
                  />
                ) : undefined}
              />
            </div>
          </div>
        )}

        {workspacePaneVisible && canvasElement}

        {showChatArea && workspacePaneVisible && (
          <div className="group/split relative z-30 hidden min-h-0 w-0 justify-self-start md:col-start-2 md:row-start-2 md:block">
            <button
              type="button"
              onPointerDown={handleSplitPointerDown}
              onKeyDown={(event) => {
                if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return
                event.preventDefault()
                setSplitRatio(workspaceSplitRatioRef.current + (event.key === 'ArrowLeft' ? -0.02 : 0.02), true)
              }}
              className="absolute -left-1.5 inset-y-0 z-10 w-3 cursor-col-resize touch-none outline-none"
              aria-label="Resize chat and workspace panels"
              aria-orientation="vertical"
              role="separator"
              aria-valuemin={15}
              aria-valuemax={85}
              aria-valuenow={Math.round(workspaceSplitRatio * 100)}
            >
              <span className="absolute bottom-0 left-1/2 top-0 w-px -translate-x-1/2 bg-border transition-colors group-hover/split:bg-primary group-focus-within/split:bg-primary" />
              <span className="absolute left-1/2 top-1/2 flex h-6 w-3.5 -translate-x-1/2 -translate-y-1/2 items-center justify-center rounded-full border border-border bg-background text-muted-foreground shadow-sm transition-colors group-hover/split:border-primary group-hover/split:text-primary group-focus-within/split:border-primary group-focus-within/split:text-primary">
                <GripVertical className="h-3 w-3" />
              </span>
            </button>
            <div className="pointer-events-none absolute left-0 top-1/2 z-20 flex -translate-x-1/2 -translate-y-1/2 flex-col items-center gap-0.5 rounded-md border border-border bg-background/95 p-0.5 shadow-lg backdrop-blur-sm opacity-0 transition-opacity group-hover/split:opacity-100 group-focus-within/split:opacity-100">
              {([
                ['mobile', Smartphone, 'Mobile preview'],
                ['tablet', Tablet, 'Tablet preview'],
                ['desktop', Laptop, 'Laptop preview'],
              ] as const).map(([device, Icon, label]) => (
                <button
                  key={device}
                  type="button"
                  onPointerDown={event => event.stopPropagation()}
                  onClick={() => writeReportPreviewPreference(workspacePath, device)}
                  className={`pointer-events-auto flex h-6 w-6 items-center justify-center rounded transition-colors ${reportPreviewPreference === device ? 'bg-primary text-primary-foreground' : 'text-muted-foreground hover:bg-muted hover:text-foreground'}`}
                  aria-label={label}
                  aria-pressed={reportPreviewPreference === device}
                  title={label}
                >
                  <Icon className="h-3 w-3" />
                </button>
              ))}
              <span className="h-px w-3 bg-border" />
              <button
                type="button"
                onPointerDown={event => event.stopPropagation()}
                onClick={collapseChatFromRail}
                className="pointer-events-auto flex h-6 w-6 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                aria-label="Collapse chat panel"
                title="Collapse chat panel"
              >
                <PanelLeftClose className="h-3 w-3" />
              </button>
              <button
                type="button"
                onPointerDown={event => event.stopPropagation()}
                onClick={collapseWorkspaceFromRail}
                className="pointer-events-auto flex h-6 w-6 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                aria-label="Collapse workspace panel"
                title="Collapse workspace panel"
              >
                <PanelRightClose className="h-3 w-3" />
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

export default WorkflowLayout
