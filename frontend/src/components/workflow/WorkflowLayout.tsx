import React, { useMemo, useCallback, useRef, useEffect, forwardRef, useState } from 'react'
import { WorkflowCanvas, type WorkflowCanvasRef } from './canvas'
import { useGlobalPresetStore } from '../../stores/useGlobalPresetStore'
import { useModeStore } from '../../stores/useModeStore'
import { normalizeEventViewMode, useChatStore, waitForChatStoreHydration, type ChatTab } from '../../stores/useChatStore'
import { useWorkflowStore } from '../../stores/useWorkflowStore'
import { useWorkspaceStore } from '../../stores/useWorkspaceStore'
import ChatArea, { type ChatAreaRef } from '../ChatArea'
import { WorkflowChatTabs } from './WorkflowChatTabs'
import { useRunningWorkflowsStore, useShowRunningDrawer } from '../../stores/useRunningWorkflowsStore'
import { useAppStore } from '../../stores/useAppStore'
import { sanitizeDisplayNameForFolder } from '../../utils/workflowUtils'
import { logger } from '../../utils/logger'
import { startRestoredTransportTerminal } from '../../utils/restoredTerminal'
import {
  PreviousChatHistoryPanel,
  chatHistoryConversationPath,
  chatHistoryRuntimeLabel,
  chatHistorySessionTitle,
  chatHistorySupportsNativeResume,
  chatHistoryUsesTerminalRestore,
  chatHistoryWorkshopModeLabel,
} from '../PreviousChatHistoryPanel'
import {
  REPORT_PREVIEW_PREFERENCE_CHANGED_EVENT,
  REPORT_PREVIEW_PREFERENCE_KEY,
} from './ReportViewer'

// Helper component to get observerId and render ChatArea
// Always renders ChatArea (even without observerId) so it can handle initialization
const ChatAreaWithObserverId = forwardRef<ChatAreaRef, {
  onNewChat: () => void
  hideHeader?: boolean
  hideInput?: boolean
  compact?: boolean
  hidePhaseChatEmptyState?: boolean
  suppressTerminalPane?: boolean
}>(({ onNewChat, hideHeader, hideInput, compact, hidePhaseChatEmptyState, suppressTerminalPane }, ref) => {
  // Prefer the active workflow tab when one is selected. The tab strip keeps
  // active workflow tabs visible even while preset metadata is catching up
  // after reload; ChatArea must use the same rule or the input area disappears.
  // Legacy/restored builder tabs may not have presetQueryId, so allow those
  // when there is no exact tab for the active preset.
  const currentPresetId = useGlobalPresetStore(state => state.activePresetIds.workflow)
  const workflowTabId = useChatStore(state => {
    const tabId = state.activeTabId
    const tab = tabId ? state.chatTabs[tabId] : null
    if (tab?.metadata?.mode !== 'workflow') return undefined
    const tabPresetId = tab.metadata?.presetQueryId
    if (tabPresetId === currentPresetId) return tabId
    if (tabId === state.activeTabId) return tabId
    if (!tabPresetId && tab.metadata?.phaseId === 'workflow-builder') {
      const hasExactPresetTab = Object.values(state.chatTabs).some(candidate =>
        candidate.metadata?.mode === 'workflow' &&
        candidate.metadata?.presetQueryId === currentPresetId &&
        (candidate.sessionId || candidate.isStreaming)
      )
      if (!hasExactPresetTab) return tabId
    }
    return undefined
  })
  const activePhaseId = useChatStore(state => {
    const tabId = state.activeTabId
    const tab = tabId ? state.chatTabs[tabId] : null
    if (tab?.metadata?.mode !== 'workflow') return undefined
    const tabPresetId = tab.metadata?.presetQueryId
    if (tabPresetId === currentPresetId) return tab?.metadata?.phaseId
    if (tabId === state.activeTabId) return tab?.metadata?.phaseId
    if (!tabPresetId && tab.metadata?.phaseId === 'workflow-builder') {
      const hasExactPresetTab = Object.values(state.chatTabs).some(candidate =>
        candidate.metadata?.mode === 'workflow' &&
        candidate.metadata?.presetQueryId === currentPresetId &&
        (candidate.sessionId || candidate.isStreaming)
      )
      if (!hasExactPresetTab) return tab?.metadata?.phaseId
    }
    return undefined
  })

  // Show chat input for chat-compatible phases
  const effectiveHideInput = isChatCompatiblePhase(activePhaseId) ? false : hideInput

  return (
    <ChatArea
      ref={ref}
      onNewChat={onNewChat}
      hideHeader={hideHeader}
      hideInput={effectiveHideInput}
      compact={compact}
      hidePhaseChatEmptyState={hidePhaseChatEmptyState}
      suppressTerminalPane={suppressTerminalPane}
      // Pass null (not undefined) when no tab matches the active workflow preset.
      // Otherwise ChatArea falls back to the global activeTabId and can briefly
      // render the previous workflow's blocking human-feedback/auth prompt.
      tabId={workflowTabId ?? null}
    />
  )
})
import { agentApi, workflowManifestApi } from '../../services/api'
import ConfirmationDialog from '../ui/ConfirmationDialog'
import {
  type ActiveSessionInfo,
  type ChatHistorySession,
  type ExecutionOptions,
  type PollingEvent,
  type RunningWorkflowInfo,
  type TerminalSnapshot,
} from '../../services/api-types'
import { getRawEventData } from '../../generated/event-types'
import { findOrCreateWorkflowTab, isChatCompatiblePhase } from '../../utils/chatSubmitHelpers'
// hydrateTabEvents removed - no longer hydrating inactive tabs on reload to prevent page hang

// Stable empty array for Zustand selector (must be module-level to avoid referential instability)
const EMPTY_WORKFLOW_EVENTS: PollingEvent[] = []
const WORKFLOW_RESTORE_TIMEOUT_MS = 8000
const WORKFLOW_CHAT_CONTENT_EVENT_TYPES = new Set(['user_message', 'conversation_end', 'unified_completion'])

function normalizeWorkflowPath(path?: string | null): string {
  return (path || '').replace(/\/+$/, '')
}

function hasWorkflowChatContent(events?: PollingEvent[]): boolean {
  return (events || []).some(event => WORKFLOW_CHAT_CONTENT_EVENT_TYPES.has(event.type || ''))
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

const WorkflowPreviousChatsPanel: React.FC<{
  workspacePath: string
  onHasChatsChange?: (hasChats: boolean) => void
}> = ({ workspacePath, onHasChatsChange }) => {
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

  const handleResumePreviousChat = useCallback(async (session: ChatHistorySession) => {
    if (!activeTabId) {
      addToast('No active workflow chat to resume in', 'error')
      return
    }

    let targetTabId = activeTabId
    const chatStore = useChatStore.getState()
    let targetTab = chatStore.chatTabs[targetTabId]
    const targetPresetId = targetTab?.metadata?.presetQueryId

    if (
      !targetTab ||
      targetTab.metadata?.mode !== 'workflow' ||
      (activePresetId && targetPresetId && targetPresetId !== activePresetId)
    ) {
      targetTabId = await chatStore.createChatTab('Workflow Builder', {
        mode: 'workflow',
        phaseId: 'workflow-builder',
        phaseName: 'Workflow Builder',
        presetQueryId: activePresetId || undefined,
      })
      targetTab = useChatStore.getState().chatTabs[targetTabId]
    }

    if (!targetTab) {
      addToast('Failed to resume previous chat', 'error')
      return
    }

    if (activePresetId && targetTab.metadata?.presetQueryId !== activePresetId) {
      chatStore.setTabMetadata(targetTabId, {
        phaseId: targetTab.metadata?.phaseId || 'workflow-builder',
        phaseName: targetTab.metadata?.phaseName || 'Workflow Builder',
        presetQueryId: activePresetId,
      })
    }

    if (targetTab?.sessionId === session.session_id) {
      chatStore.resetTabChat(targetTabId)
    }

    const path = chatHistoryConversationPath(session)
    const useTerminalRestore = chatHistoryUsesTerminalRestore(session)
    const useNativeResume = chatHistorySupportsNativeResume(session)
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
    // Both tmux terminal-restore and native-resume sessions reattach into a
    // coding-agent tmux terminal on the backend, so open the terminal view and
    // kick the restore for either — not just transport === "tmux". (Previously
    // native-resume showed the "Resuming coding session" banner but never
    // surfaced the terminal.)
    if (useTerminalRestore || useNativeResume) {
      chatStore.setTabViewMode(targetTabId, 'terminal')
      chatStore.switchTab(targetTabId)
      setShowChatArea(true)
      const restoredTab = useChatStore.getState().chatTabs[targetTabId]
      startRestoredTransportTerminal(restoredTab?.sessionId, path)
    }
  }, [activePresetId, activeTabId, addToast, setShowChatArea, setTabConfig])

  return (
    <PreviousChatHistoryPanel
      workspacePath={workspacePath}
      activeSessionId={activeSessionId ?? undefined}
      title="Previous workflow chats"
      actionLabel="Resume"
      emptyText="No previous workflow chats yet."
      onHasChatsChange={onHasChatsChange}
      onSelectSession={handleResumePreviousChat}
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

function isLiveWorkflowTerminalForPath(terminal: TerminalSnapshot, workspacePath?: string | null): boolean {
  const targetWorkspace = normalizeWorkflowPath(workspacePath)
  if (!targetWorkspace || normalizeWorkflowPath(terminal.workflow_path) !== targetWorkspace) return false

  const state = (terminal.state || '').toLowerCase().trim()
  return state === 'running'
}

function shouldBlockWorkflowNewChatForSession(
  session: ActiveSessionInfo,
  presetId: string,
  workspacePath?: string | null,
  terminals?: TerminalSnapshot[],
): boolean {
  if (!isLiveWorkflowSessionForPreset(session, presetId, workspacePath)) return false

  if (
    session.needs_user_input === true ||
    session.has_running_background_agents === true ||
    (session.running_background_agent_count ?? 0) > 0
  ) {
    return true
  }

  const status = (session.status || '').toLowerCase().trim()
  if (status === 'paused' || status === 'waiting' || status === 'waiting_feedback') {
    return true
  }

  if (!terminals) {
    return true
  }

  return terminals.some(terminal =>
    terminal.session_id === session.session_id &&
    isLiveWorkflowTerminalForPath(terminal, workspacePath)
  )
}

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
 * - Current running step ID (for StepLegend)
 * - Step statuses (running, completed, failed)
 * - Batch progress (for BatchProgressHeader)
 * This ensures the UI shows the correct state immediately after page refresh
 */
async function restoreWorkflowStateFromEvents(sessionId: string): Promise<void> {
  try {
    const { addTabEvents, setTabEvents, setTabLastEventIndex, getTabLastEventIndex, getTabEvents } = useChatStore.getState()
    const workflowStore = useWorkflowStore.getState()

    // Skip if batch progress is already active (avoid overwriting live state)
    if (workflowStore.batchProgress?.isActive) {
      logger.debug('WorkflowLayout', 'Batch progress already active, skipping restore')
      return
    }

    // Load events for this session from the in-memory EventStore. If the
    // server restarted, there's nothing to replay — the workflow run folder
    // is the durable source of truth for the run's state.
    const response = await agentApi.getRecentSessionEvents(sessionId)
    const events = response.events as PollingEvent[]
    const lastIndex = response.last_processed_index ?? events.length - 1

    if (events.length === 0) {
      return
    }

    // Use setTabEvents (replace) when tab is empty (restoration), addTabEvents (append) when live
    const existingEvents = getTabEvents(sessionId)
    if (existingEvents.length === 0) {
      setTabEvents(sessionId, events)
    } else {
      addTabEvents(sessionId, events)
    }
    // CRITICAL: Use last_processed_index from backend (not events.length - 1)
    // Backend tracks the actual event index which may be higher due to filtering/cleanup
    // Only advance the index if backend is ahead (SSE may have already advanced it)
    const currentIndex = getTabLastEventIndex(sessionId)
    if (lastIndex > currentIndex) {
      setTabLastEventIndex(sessionId, lastIndex)
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
  const { selectedModeCategory } = useModeStore()
  // Narrow selectors: bare useChatStore() re-renders on every store update (10x/sec with 2 parallel sessions)
  const currentWorkflowPhase = useChatStore(state => state.currentWorkflowPhase)
  const setCurrentWorkflowPhase = useChatStore(state => state.setCurrentWorkflowPhase)
  const addToast = useChatStore(state => state.addToast)
  const activeSessionId = useChatStore(state => {
    const tab = state.activeTabId ? state.chatTabs[state.activeTabId] : undefined
    return tab?.metadata?.mode === 'workflow' ? tab.sessionId : undefined
  })

  // Use workflow store for UI state (single source of truth)
  const activePhase = useWorkflowStore(state => state.activePhase)
  const showChatArea = useWorkflowStore(state => state.showChatArea)
  const showWorkspacePane = useWorkflowStore(state => state.showWorkspacePane)
  const setShowChatArea = useWorkflowStore(state => state.setShowChatArea)
  const workflowWorkspaceView = useWorkflowStore(state => state.workflowWorkspaceView)
  const setWorkflowWorkspaceView = useWorkflowStore(state => state.setWorkflowWorkspaceView)
  const canvasViewMode = useWorkflowStore(state => state.canvasViewMode)
  const minimizeWorkflow = useRunningWorkflowsStore(state => state.minimizeWorkflow)
  const showRunningDrawer = useShowRunningDrawer()

  const getPhaseById = useWorkflowStore(state => state.getPhaseById)
  
  // Ref for the ChatArea component
  const chatAreaRef = useRef<ChatAreaRef>(null)
  // Ref for the WorkflowCanvas component (for triggering refresh)
  const canvasRef = useRef<WorkflowCanvasRef>(null)
  // Per-session high-water marks for event processing.
  // Using Maps instead of single refs prevents re-scanning all historical events when switching
  // between workflow tabs. Without this, every tab switch fires canvasRef.refresh() for every
  // historical todo_steps_extracted event — causing hangs proportional to event history depth.
  const lastProcessedEventIndexRef = useRef<Map<string, number>>(new Map())
  // Store pending query to submit after ChatArea mounts
  const pendingQueryRef = useRef<{ query: string; executionOptions?: ExecutionOptions } | null>(null)
  // Loading state for session restoration (shown between chat tabs and chat area).
  // Lifted into useChatStore so ChatArea can render an in-panel spinner during restore.
  const isRestoringWorkflowSessions = useChatStore(state => state.isRestoringWorkflowSessions)
  const setIsRestoringWorkflowSessions = useChatStore(state => state.setIsRestoringWorkflowSessions)
  const [hasPreviousWorkflowChats, setHasPreviousWorkflowChats] = useState(false)
  const [hasLoadedPreviousWorkflowChats, setHasLoadedPreviousWorkflowChats] = useState(false)
  // Kill-and-start confirmation when "+ new chat" hits a running workflow session.
  // Holds the session ID(s) to stop and a human-readable description for the dialog.
  const [killAndStartState, setKillAndStartState] = useState<{
    isOpen: boolean
    sessionIdsToStop: string[]
    description: string
    isStopping: boolean
  }>({ isOpen: false, sessionIdsToStop: [], description: '', isStopping: false })
  useEffect(() => {
    if (!isRestoringWorkflowSessions) return
    const timeout = window.setTimeout(() => {
      console.warn('[WorkflowReconnect] Restore indicator timed out; clearing stuck restoring state')
      setIsRestoringWorkflowSessions(false)
    }, WORKFLOW_RESTORE_TIMEOUT_MS + 2000)
    return () => window.clearTimeout(timeout)
  }, [isRestoringWorkflowSessions, setIsRestoringWorkflowSessions])
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
    }

    window.addEventListener('workflow-readonly-run-restored', handleReadOnlyRestore)
    return () => window.removeEventListener('workflow-readonly-run-restored', handleReadOnlyRestore)
  }, [])
  // NOTE: During workflow execution, we no longer auto-fetch workspace files (response is 2-3MB).
  // New files are added incrementally via addFileToTree from workspace_file_operation events.
  // The Workspace component shows a "Refresh" banner when needsRefresh is set.

  // Get selected run folder and workspace functions (defined early for use in useEffect)
  const selectedRunFolder = useWorkflowStore(state => state.selectedRunFolder)
  const setStepOverride = useWorkflowStore(state => state.setStepOverride)
  const selectedGroupIds = useWorkflowStore(state => state.selectedGroupIds)
  const variablesManifest = useWorkflowStore(state => state.variablesManifest)
  const { fetchFiles, setExpandedFolders } = useWorkspaceStore()
  // Subscribe to workspace minimized state so we can skip fetches when panel is hidden
  const workspaceMinimized = useAppStore(state => state.workspaceMinimized)
  const setWorkspaceMinimized = useAppStore(state => state.setWorkspaceMinimized)
  const lastWorkspaceRunExpansionKeyRef = useRef<string | null>(null)
  const reportAutoMinimizedWorkspaceRef = useRef(false)
  const prevWorkflowWorkspaceViewRef = useRef<string | null>(null)

  const rehydrateWorkflowTabs = useCallback(async (tabs: ChatTab[]) => {
    const tabsToHydrate = tabs.filter(tab =>
      tab.sessionId && useChatStore.getState().getTabEvents(tab.sessionId).length === 0
    )
    if (tabsToHydrate.length === 0) {
      return 0
    }

    let activeSessions: Awaited<ReturnType<typeof useChatStore.getState>>['activeSessionsCache'] = []
    try {
      activeSessions = await withWorkflowRestoreTimeout(
        useChatStore.getState().getActiveSessions(),
        'Fetching active workflow sessions'
      )
    } catch (err) {
      console.warn('[WorkflowReconnect] Failed to fetch active sessions during rehydrate; continuing without live-session status:', err)
    }
    const activeWorkflowSessionIds = new Set(
      activeSessions
        .filter(session => session.agent_mode === 'workflow' || session.agent_mode === 'workflow_phase')
        .map(session => session.session_id)
    )
    const { setTabStreaming } = useChatStore.getState()

    for (const tab of tabsToHydrate) {
      if (!tab.sessionId) continue
      try {
        await withWorkflowRestoreTimeout(
          restoreWorkflowStateFromEvents(tab.sessionId),
          `Restoring workflow events for ${tab.sessionId}`
        )
        if (activeWorkflowSessionIds.has(tab.sessionId)) {
          setTabStreaming(tab.tabId, true)
        }
      } catch (err) {
        console.warn('[WorkflowReconnect] Failed to rehydrate events for persisted tab', tab.sessionId, err)
      }
    }

    return tabsToHydrate.length
  }, [])

  // Get active workflow preset (file-backed manifests, not DB presets)
  const { getActivePreset } = useGlobalPresetStore()
  const activePresetId = useGlobalPresetStore(state => state.activePresetIds.workflow)
  const activeWorkflowPreset = getActivePreset('workflow')
  const activeWorkflowWorkspacePath = activeWorkflowPreset?.selectedFolder?.filepath ?? null
  const showResumeHint = useChatStore(state => {
    const tabId = state.activeTabId
    const tab = tabId ? state.chatTabs[tabId] : null
    if (
      !tab ||
      tab.metadata?.mode !== 'workflow' ||
      tab.metadata?.phaseId !== 'workflow-builder' ||
      tab.metadata?.isViewOnly ||
      tab.isStreaming ||
      tab.config?.restoredConversationPath
    ) {
      return false
    }
    const tabEvents = tab.sessionId ? state.tabEvents[tab.sessionId] : undefined
    return !hasWorkflowChatContent(tabEvents)
  })
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

  useEffect(() => {
    if (!showResumeHint || !workspacePath) {
      setHasPreviousWorkflowChats(false)
      setHasLoadedPreviousWorkflowChats(false)
    }
  }, [showResumeHint, workspacePath])

  const handleHasPreviousWorkflowChatsChange = useCallback((hasChats: boolean, isLoaded: boolean = true) => {
    setHasPreviousWorkflowChats(hasChats)
    setHasLoadedPreviousWorkflowChats(isLoaded)
  }, [])

  const [reportPreviewPreference, setReportPreviewPreference] = useState<'auto' | 'desktop' | 'tablet' | 'mobile'>(() => {
    try {
      const saved = localStorage.getItem(REPORT_PREVIEW_PREFERENCE_KEY)
      return saved === 'desktop' || saved === 'tablet' || saved === 'mobile' ? saved : 'auto'
    } catch {
      return 'auto'
    }
  })

  const createFreshWorkflowBuilderTab = useCallback(async (presetId: string) => {
    const chatStore = useChatStore.getState()
    const oldTabs = Object.values(chatStore.chatTabs).filter(tab =>
      tab.metadata?.mode === 'workflow' &&
      tab.metadata?.phaseId === 'workflow-builder' &&
      tab.metadata?.presetQueryId === presetId &&
      !chatStore.getTabStreamingStatus(tab.tabId)
    )

    for (const tab of oldTabs) {
      await chatStore.closeTab(tab.tabId, false)
    }

    const tabId = await chatStore.createChatTab('Workflow Builder', {
      mode: 'workflow',
      phaseId: 'workflow-builder',
      phaseName: 'Workflow Builder',
      presetQueryId: presetId
    })
    chatStore.switchTab(tabId)
    setShowChatArea(true)
  }, [setShowChatArea])

  useEffect(() => {
    const syncReportPreviewPreference = () => {
      try {
        const saved = localStorage.getItem(REPORT_PREVIEW_PREFERENCE_KEY)
        setReportPreviewPreference(saved === 'desktop' || saved === 'tablet' || saved === 'mobile' ? saved : 'auto')
      } catch {
        setReportPreviewPreference('auto')
      }
    }

    window.addEventListener(REPORT_PREVIEW_PREFERENCE_CHANGED_EVENT, syncReportPreviewPreference as EventListener)
    window.addEventListener('storage', syncReportPreviewPreference)

    return () => {
      window.removeEventListener(REPORT_PREVIEW_PREFERENCE_CHANGED_EVENT, syncReportPreviewPreference as EventListener)
      window.removeEventListener('storage', syncReportPreviewPreference)
    }
  }, [])

  const workspacePaneVisible = !showChatArea || showWorkspacePane
  // Each preview tier controls the OUTER report pane width when both chat and
  // canvas are visible — not just the inner shell. Otherwise switching to
  // laptop mode would only change the inner max-width while the surrounding
  // pane stayed pinned at 50% of the screen.
  //
  //   mobile  → report 480px column, chat takes the rest (review-style)
  //   tablet  → 50/50 split between builder/chat and the workflow view
  //   laptop  → chat collapses to ~360px, report fills the remaining width
  //   default → 50/50 split (no preview pref, or running in non-report views)
  const isPreviewableWorkspaceCanvas =
    showChatArea &&
    workspacePaneVisible &&
    (canvasViewMode === 'report' || canvasViewMode === 'flow')
  const previewPaneTier: 'mobile' | 'tablet' | 'laptop' | null = isPreviewableWorkspaceCanvas
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
  const isWorkspaceViewActive =
    workflowWorkspaceView === 'flow' ||
    workflowWorkspaceView === 'report'
  const chatPaneVisibilityClass =
    workspacePaneVisible && isWorkspaceViewActive
      ? 'hidden md:flex'
      : 'flex'
  const splitLayoutClassName = !showChatArea
    ? 'flex-1 min-h-0 flex flex-col'
    : !workspacePaneVisible
      ? 'flex-1 min-h-0 flex flex-col'
      : previewPaneTier === 'mobile'
        ? 'flex-1 min-h-0 flex flex-col md:grid md:grid-cols-[minmax(0,1fr)_480px] md:grid-rows-[auto_minmax(0,1fr)]'
        : previewPaneTier === 'tablet'
          ? 'flex-1 min-h-0 flex flex-col md:grid md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)] md:grid-rows-[auto_minmax(0,1fr)]'
          : previewPaneTier === 'laptop'
            ? 'flex-1 min-h-0 flex flex-col md:grid md:grid-cols-[360px_minmax(0,1fr)] md:grid-rows-[auto_minmax(0,1fr)]'
            : 'flex-1 min-h-0 flex flex-col md:grid md:grid-cols-[minmax(320px,0.9fr)_minmax(360px,1.1fr)] md:grid-rows-[auto_minmax(0,1fr)]'
  const canvasPaneClassName = !showChatArea
    ? 'flex-1 min-h-0 min-w-0 transition-all duration-300'
    : !workspacePaneVisible
    ? 'hidden'
    : previewPaneTier === 'mobile'
        ? 'min-h-0 min-w-0 transition-all duration-300 w-full md:col-start-2 md:row-start-2 md:w-[480px] md:flex-none'
        : previewPaneTier === 'tablet'
          ? 'min-h-0 min-w-0 transition-all duration-300 w-full md:col-start-2 md:row-start-2 md:w-full md:flex-none'
          : 'min-h-0 min-w-0 transition-all duration-300 md:col-start-2 md:row-start-2'

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

  // When switching to a session we haven't seen yet, initialize its high-water mark to the
  // current event count — skipping all historical events. The canvas initializes via usePlanData
  // independently; replaying old todo_steps_extracted events would fire multiple canvas.refresh()
  // calls for no benefit and cause the visible hang on tab switch.
  useEffect(() => {
    const sid = activeSessionId
    if (!sid) return
    if (!lastProcessedEventIndexRef.current.has(sid)) {
      const evts = useChatStore.getState().tabEvents[sid] ?? []
      lastProcessedEventIndexRef.current.set(sid, evts.length - 1)
    }
  }, [activeSessionId])

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

  const processPlanUpdateEvents = useCallback((sessionId: string, events: PollingEvent[]) => {
    if (events.length === 0) return
    // Find new todo_steps_extracted events that we haven't processed yet
    const lastIdx = lastProcessedEventIndexRef.current.get(sessionId) ?? events.length - 1
    for (let i = lastIdx + 1; i < events.length; i++) {
      const event = events[i]
      
      if (event.type === 'todo_steps_extracted') {
        logger.debug('WorkflowLayout', `[PlanUpdate] Event ${i}: type=${event.type}, timestamp=${event.timestamp}`)
        // Use helper function to extract raw event data (handles nested structure)
        const rawData = getRawEventData(event)
        const eventData = rawData as {
          extracted_steps?: unknown[], 
          total_steps_extracted?: number, 
          plan_source?: string, 
          extraction_method?: string, 
          workspace_path?: string,
          metadata?: {
            [k: string]: unknown
          }
        } | undefined
        
        if (!eventData) {
          logger.warn('WorkflowLayout', '[PlanUpdate] Could not extract event data from event:', event)
          continue
        }
        
        const stepCount = (eventData?.extracted_steps?.length) || eventData?.total_steps_extracted || 0
        const planSource = eventData?.plan_source || 'unknown'
        const extractionMethod = eventData?.extraction_method || 'unknown'
        
        // Extract changed step IDs from metadata (granular event data)
        // Metadata is at the top level of the event data (from BaseEventData)
        const metadata = eventData?.metadata || {}
        const changedStepIDs = (Array.isArray(metadata.changed_step_ids) 
          ? metadata.changed_step_ids as string[] 
          : []) || []
        const deletedStepIDs = (Array.isArray(metadata.deleted_step_ids) 
          ? metadata.deleted_step_ids as string[] 
          : []) || []
        
        logger.debug('WorkflowLayout', `[PlanUpdate] Detected plan update event:`, {
          stepCount,
          planSource,
          extractionMethod,
          workspacePath: eventData?.workspace_path,
          changedStepIDs,
          deletedStepIDs,
          hasMetadata: !!(eventData?.metadata),
          metadataKeys: eventData?.metadata ? Object.keys(eventData.metadata) : [],
          metadata: eventData?.metadata,
          rawEventData: rawData,
          eventIndex: i
        })
        
        // Trigger canvas refresh with granular change data
        if (canvasRef.current) {
          logger.debug('WorkflowLayout', '[PlanUpdate] Calling canvasRef.current.refresh() with granular changes')
          canvasRef.current.refresh(changedStepIDs, deletedStepIDs).then((changes) => {
            logger.debug('WorkflowLayout', '[PlanUpdate] Canvas refresh completed:', changes)
          }).catch((err) => {
            logger.error('WorkflowLayout', '[PlanUpdate] Canvas refresh failed:', err)
          })
        } else {
          logger.warn('WorkflowLayout', '[PlanUpdate] canvasRef.current is null, cannot refresh')
        }
      }
      
      // Update index processed - do this for ALL events to avoid re-scanning
      lastProcessedEventIndexRef.current.set(sessionId, i)
    }
  }, [])

  // Listen for todo_steps_extracted events without subscribing the whole layout
  // render path to high-frequency chat/tool event updates.
  useEffect(() => {
    if (!activeSessionId) return

    const sessionId = activeSessionId
    return useChatStore.subscribe((state, prevState) => {
      const events = state.tabEvents[sessionId] ?? EMPTY_WORKFLOW_EVENTS
      const previousEvents = prevState.tabEvents[sessionId] ?? EMPTY_WORKFLOW_EVENTS
      if (events === previousEvents || events.length === previousEvents.length) return
      processPlanUpdateEvents(sessionId, events)
    })
  }, [activeSessionId, processPlanUpdateEvents])

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
        const { createChatTab, switchTab, getTabEvents, setTabStreaming } = useChatStore.getState()
        const { getPhaseById } = useWorkflowStore.getState()
        const getExistingWorkflowTabsForPreset = () =>
          Object.values(useChatStore.getState().chatTabs)
            .filter(t =>
              t.metadata?.mode === 'workflow' &&
              t.metadata?.presetQueryId === activePresetId
            )
            .sort((a, b) => b.createdAt - a.createdAt)

        // 1. Get active (running) sessions from in-memory cache
        //    Include both 'workflow' (execution) and 'workflow_phase' (workflow builder, plan-improvement)
        const activeSessions = await useChatStore.getState().getActiveSessions()
        const activeWorkflowSessions = activeSessions.filter(s =>
          s.agent_mode === 'workflow' || s.agent_mode === 'workflow_phase'
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
          let belongsToPreset = isLiveWorkflowSessionForPreset(s, activePresetId, workspacePath)
          try {
            const running = runningWorkflowsBySession.get(s.session_id) || await agentApi.getRunningWorkflow(s.session_id)
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
            // Carry through scheduler/bot identity so the tab labelling
            // chain below uses "<Schedule Name>" instead of falling to
            // the literal "Workflow" string. Backend stamps Title +
            // TriggeredBy='cron' on scheduled sessions via
            // stampScheduleNameOnSession.
            triggeredBy: s.triggered_by,
            botPlatform: s.bot_platform,
            isScheduledRun: s.triggered_by === 'cron',
          })
        }

        for (const running of runningWorkflowsBySession.values()) {
          if (!running.session_id || queuedSessionIds.has(running.session_id)) continue
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
            isScheduledRun: running.triggered_by === 'cron',
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
            const match = s.title.match(/(?:workflow[- ]builder|planning|evaluation[- ]builder)/i)
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

        // Only restore sessions that don't have tabs yet
        const sessionsToActuallyRestore = newSessions

        const needsTabHydration = existingWorkflowTabs.some(tab =>
          tab.sessionId && getTabEvents(tab.sessionId).length === 0
        )

        if (sessionsToActuallyRestore.length > 0 || needsTabHydration) {
          setIsRestoringWorkflowSessions(true)
        }

        // 3a. Rehydrate events for persisted tabs whose event buffer was lost on refresh.
        if (needsTabHydration) {
          await rehydrateWorkflowTabs(existingWorkflowTabs)
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
          //   4. Last resort: phaseId / "Workflow Builder" (so the chat
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
            phaseName = phaseId || 'Workflow Builder'
          }

          // Create tab with scheduled-run / bot metadata so downstream
          // UI (chat-input toggle, view-only banner, badge icons) treats
          // it as a read-only observer of an external trigger.
          const isScheduled = session.isScheduledRun || session.triggeredBy === 'cron'
          const isBot = Boolean(session.botPlatform)
          const tabId = await createChatTab(phaseName, {
            mode: 'workflow',
            phaseId: phaseId || undefined,
            phaseName,
            presetQueryId: activePresetId,
            isViewOnly: isScheduled || isBot ? true : undefined,
            isScheduledRun: isScheduled || undefined,
            scheduledJobName: isScheduled ? (session.title || phaseName) : undefined,
            isBotRun: isBot || undefined,
            botPlatform: isBot ? session.botPlatform : undefined,
          }, session.sessionId)

          // Load events from in-memory EventStore (workflow events are NOT stored in DB)
          // restoreWorkflowStateFromEvents fetches from the polling API which reads EventStore
          if (session.preloadedEvents && session.preloadedEvents.length > 0) {
            useChatStore.getState().setTabEvents(session.sessionId, session.preloadedEvents)
            useChatStore.getState().setTabLastEventIndex(
              session.sessionId,
              session.lastProcessedIndex ?? session.preloadedEvents.length - 1,
            )
            setTabStreaming(tabId, session.isActive)
            useChatStore.getState().setTabCompleted(tabId, !session.isActive)
          } else {
            try {
              await restoreWorkflowStateFromEvents(session.sessionId)
              if (session.isActive || session.status === 'running') {
                setTabStreaming(tabId, true)
              }
            } catch (err) {
              console.warn('[WorkflowReconnect] Failed to load events for', session.sessionId, err)
            }
          }

          lastTabId = tabId
        }

        // 5. Show the chat area with the last tab
        if (lastTabId) {
          switchTab(lastTabId)
          setShowChatArea(true)
        }

        // 6. If no tabs were created/restored, show an empty Workflow Builder tab.
        // Previous conversations are attached explicitly with /resume instead of
        // auto-querying builder history during every workflow switch.
        if (!lastTabId) {
          const store = useChatStore.getState()
          if (existingWorkflowTabs.length === 0) {
            const defaultTabId = await createChatTab('Workflow Builder', {
              mode: 'workflow',
              phaseId: 'workflow-builder',
              phaseName: 'Workflow Builder',
              presetQueryId: activePresetId
            })
            switchTab(defaultTabId)
            setShowChatArea(true)
          } else {
            const streamingTab = existingWorkflowTabs.find(t => t.isStreaming || store.getTabStreamingStatus(t.tabId))
            if (streamingTab) {
              switchTab(streamingTab.tabId)
              setShowChatArea(true)
              return
            } else {
              const builderTab = existingWorkflowTabs.find(t => t.metadata?.phaseId === 'workflow-builder')
              if (builderTab) {
                switchTab(builderTab.tabId)
                setShowChatArea(true)
                return
              }
              switchTab(existingWorkflowTabs[0].tabId)
              setShowChatArea(true)
              return
            }
          }
        }
      } catch (error) {
        console.warn('[WorkflowReconnect] Failed to reconnect workflow tabs:', error)
      } finally {
        setIsRestoringWorkflowSessions(false)
      }
    }

    const timeoutId = setTimeout(reconnectWorkflowTabs, 500)
    return () => clearTimeout(timeoutId)
  }, [activePresetId, workspacePath, setShowChatArea, setIsRestoringWorkflowSessions, rehydrateWorkflowTabs, createFreshWorkflowBuilderTab])

  useEffect(() => {
    if (!activePresetId || selectedModeCategory !== 'workflow') return

    let cancelled = false
    const reconcileRunningWorkflowTab = async () => {
      if (runningWorkflowReconcileInFlightRef.current) return
      runningWorkflowReconcileInFlightRef.current = true
      try {
        const response = await agentApi.listRunningWorkflows()
        if (cancelled) return
        const runningWorkflows = (response.running || [])
          .filter(item => item.session_id && isRunningWorkflowEntry(item))
          .filter(item => runningWorkflowBelongsToPreset(item, activePresetId, workspacePath))
          .sort((a, b) => new Date(b.started_at || 0).getTime() - new Date(a.started_at || 0).getTime())

        if (runningWorkflows.length === 0) return

        const chatStore = useChatStore.getState()
        const activeTab = chatStore.activeTabId ? chatStore.chatTabs[chatStore.activeTabId] : undefined
        const activeViewMode = normalizeEventViewMode(activeTab?.viewMode || chatStore.eventViewModePreference)
        const activeTabIsStreaming = activeTab
          ? activeTab.isStreaming || chatStore.getTabStreamingStatus(activeTab.tabId)
          : false
        const activeIsBuilderForPreset =
          activeTab?.metadata?.mode === 'workflow' &&
          activeTab.metadata?.phaseId === 'workflow-builder' &&
          (activeTab.metadata?.presetQueryId === activePresetId || !activeTab.metadata?.presetQueryId)
        const activeIsCompletedWorkflowForPreset =
          activeTab?.metadata?.mode === 'workflow' &&
          activeTab.metadata?.presetQueryId === activePresetId &&
          !activeTabIsStreaming
        const latestRunning = runningWorkflows[0]
        const shouldSwitch =
          !activeTab ||
          activeTab.metadata?.mode !== 'workflow' ||
          (
            activeTab.sessionId !== latestRunning.session_id &&
            (activeViewMode === 'terminal' || activeIsBuilderForPreset || activeIsCompletedWorkflowForPreset)
          )

        let selectedRunningTabId: string | null = null
        for (const running of runningWorkflows) {
          if (!running.session_id) continue

          const existingTab = Object.values(chatStore.chatTabs).find(tab =>
            tab.metadata?.mode === 'workflow' && tab.sessionId === running.session_id
          )

          let tabId = existingTab?.tabId
          if (!tabId) {
            const phaseName = running.phase_name || running.title || running.preset_name || 'Running workflow'
            tabId = await chatStore.createChatTab(phaseName, {
              mode: 'workflow',
              phaseId: running.phase_id || undefined,
              phaseName,
              presetQueryId: activePresetId,
            }, running.session_id)
          }
          selectedRunningTabId ||= tabId

          chatStore.setTabMetadata(tabId, {
            ...chatStore.chatTabs[tabId]?.metadata,
            mode: 'workflow',
            phaseId: running.phase_id || chatStore.chatTabs[tabId]?.metadata?.phaseId,
            phaseName: running.phase_name || chatStore.chatTabs[tabId]?.metadata?.phaseName,
            presetQueryId: activePresetId,
          })
          chatStore.setTabStreaming(tabId, true)
          chatStore.setTabCompleted(tabId, false)
          chatStore.setTabViewMode(tabId, activeViewMode)

          if (chatStore.getTabEvents(running.session_id).length === 0) {
            try {
              const eventsResponse = await agentApi.getRecentSessionEvents(running.session_id)
              if (!cancelled && eventsResponse.events.length > 0) {
                chatStore.setTabEvents(running.session_id, eventsResponse.events)
                chatStore.setTabLastEventIndex(
                  running.session_id,
                  eventsResponse.last_processed_index ?? eventsResponse.events.length - 1,
                )
              }
            } catch {
              /* Terminal mode can still render from /api/terminals if event hydration misses. */
            }
          }
        }

        if (shouldSwitch && selectedRunningTabId) {
          chatStore.switchTab(selectedRunningTabId)
          setShowChatArea(true)
        } else if (activeTab?.sessionId && runningWorkflows.some(item => item.session_id === activeTab.sessionId)) {
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
        .sort((a, b) => b.createdAt - a.createdAt)

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
        chatStore.switchTab(targetTab.tabId)
        setShowChatArea(true)

        const needsHydration = newPresetTabs.some(tab =>
          tab.sessionId && chatStore.getTabEvents(tab.sessionId).length === 0
        )
        if (needsHydration) {
          setIsRestoringWorkflowSessions(true)
          void rehydrateWorkflowTabs(newPresetTabs).finally(() => {
            setIsRestoringWorkflowSessions(false)
          })
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
  }, [activePresetId, minimizeWorkflow, selectedRunFolder, setShowChatArea, setIsRestoringWorkflowSessions, rehydrateWorkflowTabs, createFreshWorkflowBuilderTab])

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
    setWorkflowWorkspaceView('builder')

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

  // Handle create plan - always opens Workflow Builder.
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
      if (!activeTab || activeTab.metadata?.mode !== 'workflow') {
        const workflowTabs = Object.values(chatStore.chatTabs)
          .filter(t => t.metadata?.mode === 'workflow')
          .sort((a, b) => b.createdAt - a.createdAt)
        if (workflowTabs.length > 0) {
          chatStore.switchTab(workflowTabs[0].tabId)
        }
      }
    }
    setShowChatArea(newShow)
  }, [showChatArea, setShowChatArea])

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

  const handleWorkflowNewChat = useCallback(async () => {
    if (activePresetId) {
      const [sessionsResult, terminalsResult] = await Promise.allSettled([
        useChatStore.getState().getActiveSessions(true),
        agentApi.listTerminals(undefined, 'none'),
      ])
      const terminalSnapshots = terminalsResult.status === 'fulfilled'
        ? (terminalsResult.value.terminals || [])
        : undefined

      const blockingSessionIds: string[] = []
      let blockingSessionLabel = ''
      if (sessionsResult.status === 'fulfilled') {
        const runningSession = sessionsResult.value.find(session =>
          shouldBlockWorkflowNewChatForSession(session, activePresetId, workspacePath, terminalSnapshots)
        )
        if (runningSession) {
          blockingSessionIds.push(runningSession.session_id)
          blockingSessionLabel = 'workflow chat session'
        }
      } else {
        logger.warn('WorkflowLayout', 'Failed to check active sessions before starting new workflow chat:', sessionsResult.reason)
      }

      if (terminalsResult.status === 'fulfilled') {
        const runningTerminal = terminalsResult.value.terminals?.find(terminal =>
          terminal.session_id !== activeSessionId &&
          isLiveWorkflowTerminalForPath(terminal, workspacePath)
        )
        if (runningTerminal && !blockingSessionIds.includes(runningTerminal.session_id)) {
          blockingSessionIds.push(runningTerminal.session_id)
          blockingSessionLabel = blockingSessionLabel
            ? `${blockingSessionLabel} and terminal`
            : 'terminal session'
        }
      } else {
        logger.warn('WorkflowLayout', 'Failed to check active terminals before starting new workflow chat:', terminalsResult.reason)
      }

      if (blockingSessionIds.length > 0) {
        setKillAndStartState({
          isOpen: true,
          sessionIdsToStop: blockingSessionIds,
          description: `Another ${blockingSessionLabel} is currently running for this workflow. Starting a new chat will stop it.`,
          isStopping: false,
        })
        return
      }

      await createFreshWorkflowBuilderTab(activePresetId)
      return
    }

    chatAreaRef.current?.handleNewChat()
  }, [activePresetId, activeSessionId, createFreshWorkflowBuilderTab, workspacePath])

  const handleKillAndStart = useCallback(async () => {
    if (!activePresetId) {
      setKillAndStartState(prev => ({ ...prev, isOpen: false }))
      return
    }
    setKillAndStartState(prev => ({ ...prev, isStopping: true }))
    const sessionIds = killAndStartState.sessionIdsToStop
    const results = await Promise.allSettled(
      sessionIds.map(sid => agentApi.stopSession(sid, true))
    )
    results.forEach((r, idx) => {
      if (r.status === 'rejected') {
        logger.warn('WorkflowLayout', `Failed to stop session ${sessionIds[idx]} during kill-and-start:`, r.reason)
      }
    })
    setKillAndStartState({ isOpen: false, sessionIdsToStop: [], description: '', isStopping: false })
    try {
      await createFreshWorkflowBuilderTab(activePresetId)
    } catch (err) {
      logger.error('WorkflowLayout', 'createFreshWorkflowBuilderTab failed after kill-and-start:', err)
      addToast('Failed to start new chat after stopping the previous one.', 'error')
    }
  }, [activePresetId, addToast, createFreshWorkflowBuilderTab, killAndStartState.sessionIdsToStop])

  const handleCloseKillAndStart = useCallback(() => {
    setKillAndStartState(prev => prev.isStopping ? prev : { isOpen: false, sessionIdsToStop: [], description: '', isStopping: false })
  }, [])

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
              Select a Workflow
            </h2>
            <p className="text-sm text-gray-500 dark:text-gray-400 mt-2">
              Choose a workflow preset from the sidebar to get started.
              The workflow canvas will visualize your plan and let you run it step by step.
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
      sharedToolbar={showChatArea}
      paneClassName={canvasPaneClassName}
      onToggleChatArea={handleToggleChatArea}
      className={showChatArea && !workspacePaneVisible ? '!h-auto shrink-0' : 'h-full'}
    />
  )

  return (
    <div className={`flex flex-col h-full ${className}`}>
      {/* Main Content */}
      <div className={splitLayoutClassName}>
        {showChatArea && !workspacePaneVisible && canvasElement}

        {showChatArea && (
          <div data-tour="workflow-chat-pane" data-testid="tour-workflow-chat-pane" className={`${chatPaneVisibilityClass} min-h-0 min-w-0 overflow-hidden flex-col bg-background transition-all duration-300 ${
            workspacePaneVisible
              ? `border-b border-border md:col-start-1 md:row-start-2 md:border-b-0 md:border-r ${shouldUseMobileReportPane ? 'flex-1 md:flex-[1.35]' : 'flex-1 basis-1/2'}`
              : 'flex-1'
          }`}>
            <div className="flex-shrink-0">
              <WorkflowChatTabs onNewChat={handleWorkflowNewChat} />
            </div>

            {isRestoringWorkflowSessions && (
              <div className="flex items-center gap-2 border-b border-blue-100 bg-blue-50 px-3 py-1.5 dark:border-blue-800/50 dark:bg-blue-900/20">
                <div className="h-3 w-3 animate-spin rounded-full border-2 border-gray-300 border-t-blue-600 dark:border-gray-600 dark:border-t-blue-400"></div>
                <span className="text-xs text-blue-600 dark:text-blue-400">Restoring previous session...</span>
              </div>
            )}

            {showResumeHint && workspacePath && (
              <div className="shrink-0">
                <WorkflowPreviousChatsPanel
                  workspacePath={workspacePath}
                  onHasChatsChange={handleHasPreviousWorkflowChatsChange}
                />
              </div>
            )}

            <div className="min-h-0 flex-1 overflow-hidden">
              <ChatAreaWithObserverId
                ref={chatAreaCallbackRef}
                onNewChat={onNewChat}
                hideHeader
                hideInput
                compact
                hidePhaseChatEmptyState={showResumeHint && (!hasLoadedPreviousWorkflowChats || hasPreviousWorkflowChats)}
                suppressTerminalPane={showResumeHint && (!hasLoadedPreviousWorkflowChats || hasPreviousWorkflowChats)}
              />
            </div>
          </div>
        )}

        {workspacePaneVisible && canvasElement}
      </div>
      <ConfirmationDialog
        isOpen={killAndStartState.isOpen}
        onClose={handleCloseKillAndStart}
        onConfirm={handleKillAndStart}
        title="Stop running session?"
        message={killAndStartState.description}
        confirmText={killAndStartState.isStopping ? 'Stopping…' : 'Stop and start new'}
        cancelText="Cancel"
        type="warning"
        isLoading={killAndStartState.isStopping}
      />
    </div>
  )
}

export default WorkflowLayout
