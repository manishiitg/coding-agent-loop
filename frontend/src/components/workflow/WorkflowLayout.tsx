import React, { useMemo, useCallback, useRef, useEffect, forwardRef } from 'react'
import { WorkflowCanvas, type WorkflowCanvasRef } from './canvas'
import { useGlobalPresetStore } from '../../stores/useGlobalPresetStore'
import { useModeStore } from '../../stores/useModeStore'
import { useChatStore } from '../../stores/useChatStore'
import { useWorkflowStore } from '../../stores/useWorkflowStore'
import { useWorkspaceStore } from '../../stores/useWorkspaceStore'
import ChatArea, { type ChatAreaRef } from '../ChatArea'
import { WorkflowChatTabs } from './WorkflowChatTabs'
import { useRunningWorkflowsStore, useShowRunningDrawer } from '../../stores/useRunningWorkflowsStore'
import { useAppStore } from '../../stores/useAppStore'
import { sanitizeDisplayNameForFolder } from '../../utils/workflowUtils'
import { logger } from '../../utils/logger'

// Helper component to get observerId and render ChatArea
// Always renders ChatArea (even without observerId) so it can handle initialization
const ChatAreaWithObserverId = forwardRef<ChatAreaRef, {
  onNewChat: () => void
  hideHeader?: boolean
  hideInput?: boolean
  compact?: boolean
}>(({ onNewChat, hideHeader, hideInput, compact }, ref) => {
  // Only pass tabId if the active tab belongs to workflow mode,
  // so we never bleed multi-agent / chat content into WorkflowLayout.
  const workflowTabId = useChatStore(state => {
    const tabId = state.activeTabId
    const tab = tabId ? state.chatTabs[tabId] : null
    return tab?.metadata?.mode === 'workflow' ? tabId : undefined
  })
  return (
    <ChatArea
      ref={ref}
      onNewChat={onNewChat}
      hideHeader={hideHeader}
      hideInput={hideInput}
      compact={compact}
      tabId={workflowTabId ?? undefined}
    />
  )
})
import { agentApi } from '../../services/api'
import { type ExecutionOptions, type PollingEvent } from '../../services/api-types'
import { getTypedEventData, getRawEventData } from '../../generated/event-types'
import { usePlanData } from './hooks/usePlanData'
import { findOrCreateWorkflowTab } from '../../utils/chatSubmitHelpers'
// hydrateTabEvents removed - no longer hydrating inactive tabs on reload to prevent page hang

// Stable empty array for Zustand selector (must be module-level to avoid referential instability)
const EMPTY_WORKFLOW_EVENTS: PollingEvent[] = []

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
    const { addTabEvents, setTabLastEventIndex, getTabLastEventIndex } = useChatStore.getState()
    const workflowStore = useWorkflowStore.getState()

    // Skip if batch progress is already active (avoid overwriting live state)
    if (workflowStore.batchProgress?.isActive) {
      logger.debug('WorkflowLayout', 'Batch progress already active, skipping restore')
      return
    }

    // Load events for this session
    const response = await agentApi.getSessionEvents(sessionId, -1)
    const events = response.events as PollingEvent[]

    if (events.length === 0) {
      return
    }

    // Append events with dedup (avoids losing SSE-streamed events in a race)
    addTabEvents(sessionId, events)
    // CRITICAL: Use last_processed_index from backend (not events.length - 1)
    // Backend tracks the actual event index which may be higher due to filtering/cleanup
    const lastIndex = response.last_processed_index ?? events.length - 1
    // Only advance the index if backend is ahead (SSE may have already advanced it)
    const currentIndex = getTabLastEventIndex(sessionId)
    if (lastIndex > currentIndex) {
      setTabLastEventIndex(sessionId, lastIndex)
    }

    // Scan events to find batch context, current step, and step statuses
    let latestBatchContext: {
      groupId: string
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
      // Extract from step_progress_updated (most common, has batch context and step info)
      if (event.type === 'step_progress_updated') {
        // Try event.data.data first (standard format), then event.data (direct format)
        const eventData = event.data as Record<string, unknown>
        const data = (eventData?.data as Record<string, unknown>) || eventData
        const groupId = data?.group_id as string
        const groupIndex = data?.group_index as number
        const totalGroups = data?.total_groups as number
        const runFolder = data?.run_folder as string
        const stepId = data?.current_step_id as string
        const status = data?.status as string

        if (groupId && totalGroups > 0) {
          latestBatchContext = { groupId, groupIndex, totalGroups, runFolder }
        }

        // Track step status
        if (stepId && status) {
          if (status === 'start') {
            latestRunningStepId = stepId
            stepStatuses.set(stepId, 'running')
          } else if (status === 'end') {
            stepStatuses.set(stepId, 'completed')
            if (latestRunningStepId === stepId) {
              latestRunningStepId = null
            }
          } else if (status === 'failed') {
            stepStatuses.set(stepId, 'failed')
            if (latestRunningStepId === stepId) {
              latestRunningStepId = null
            }
          }
        }
      }

      // Extract from batch_group_start
      if (event.type === 'batch_group_start') {
        const eventData = event.data as Record<string, unknown>
        const data = (eventData?.data as Record<string, unknown>) || eventData
        const groupId = data?.group_id as string
        const groupIndex = data?.group_index as number
        const totalGroups = data?.total_groups as number
        const runFolder = data?.run_folder as string

        if (groupId && totalGroups > 0) {
          latestBatchContext = { groupId, groupIndex, totalGroups, runFolder }
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
          latestBatchContext.groupId,
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
          groupId: latestBatchContext.groupId,
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
  const { currentWorkflowPhase, setCurrentWorkflowPhase, getTabEvents } = useChatStore()
  // Get events from active tab instead of global events
  const activeTab = useChatStore(state => state.getActiveTab())
  const activeSessionId = activeTab?.sessionId
  // Subscribe to tabEvents length so we re-run the memo when new events arrive.
  // Using a Zustand selector for the full array caused infinite-loop issues because
  // the inline selector closure captured activeSessionId and was recreated on every render.
  const tabEventsLength = useChatStore((state) =>
    activeSessionId ? (state.tabEvents[activeSessionId]?.length ?? 0) : 0
  )
  const events = React.useMemo(() => {
    // tabEventsLength dependency ensures this re-evaluates when events are added
    void tabEventsLength
    return activeSessionId ? getTabEvents(activeSessionId) : EMPTY_WORKFLOW_EVENTS
  }, [activeSessionId, getTabEvents, tabEventsLength])

  // Use workflow store for UI state (single source of truth)
  const activePhase = useWorkflowStore(state => state.activePhase)
  const showChatArea = useWorkflowStore(state => state.showChatArea)
  const setShowChatArea = useWorkflowStore(state => state.setShowChatArea)
  const chatAreaExpanded = useWorkflowStore(state => state.chatAreaExpanded)
  const minimizeWorkflow = useRunningWorkflowsStore(state => state.minimizeWorkflow)
  const stepProgress = useWorkflowStore(state => state.stepProgress)
  const showRunningDrawer = useShowRunningDrawer()

  const getPhaseById = useWorkflowStore(state => state.getPhaseById)
  
  // Ref for the ChatArea component
  const chatAreaRef = useRef<ChatAreaRef>(null)
  // Ref for the WorkflowCanvas component (for triggering refresh)
  const canvasRef = useRef<WorkflowCanvasRef>(null)
  // Track the last processed event index to avoid duplicate refreshes
  const lastProcessedEventIndexRef = useRef(-1)
  // Track the last processed step progress event index to avoid duplicate workspace refreshes
  const lastProcessedStepProgressIndexRef = useRef(-1)
  // Store pending query to submit after ChatArea mounts
  const pendingQueryRef = useRef<{ query: string; executionOptions?: ExecutionOptions } | null>(null)
  // Track the previous preset ID for auto-minimize on preset switch
  const previousPresetIdRef = useRef<string | null>(null)
  // NOTE: During workflow execution, we no longer auto-fetch workspace files (response is 2-3MB).
  // New files are added incrementally via addFileToTree from workspace_file_operation events.
  // The Workspace component shows a "Refresh" banner when needsRefresh is set.

  // Get selected run folder and workspace functions (defined early for use in useEffect)
  const selectedRunFolder = useWorkflowStore(state => state.selectedRunFolder)
  const setSelectedRunFolder = useWorkflowStore(state => state.setSelectedRunFolder)
  const updateStepProgressFromEvent = useWorkflowStore(state => state.updateStepProgressFromEvent)
  const selectedGroupIds = useWorkflowStore(state => state.selectedGroupIds)
  const variablesManifest = useWorkflowStore(state => state.variablesManifest)
  const { fetchFiles, setExpandedFolders } = useWorkspaceStore()
  // Subscribe to workspace minimized state so we can skip fetches when panel is hidden
  const workspaceMinimized = useAppStore(state => state.workspaceMinimized)

  // Get active workflow preset
  const activePresetId = useGlobalPresetStore(state => state.activePresetIds.workflow)
  const customPresets = useGlobalPresetStore(state => state.customPresets)
  const predefinedPresets = useGlobalPresetStore(state => state.predefinedPresets)

  const activeWorkflowPreset = useMemo(() => {
    if (activePresetId) {
      const customPreset = customPresets.find(p => p.id === activePresetId)
      if (customPreset) return customPreset

      const predefinedPreset = predefinedPresets.find(p => p.id === activePresetId)
      if (predefinedPreset) return predefinedPreset
    }
    return null
  }, [activePresetId, customPresets, predefinedPresets])

  // Get workspace path from active preset
  const workspacePath = useMemo(() => {
    if (activeWorkflowPreset?.selectedFolder?.filepath) {
      return activeWorkflowPreset.selectedFolder.filepath
    }
    return null
  }, [activeWorkflowPreset])

  // Auto-expand selectedRunFolder and selected groups in workspace sidebar whenever they change
  useEffect(() => {
    if (selectedRunFolder && selectedRunFolder !== 'new' && workspacePath) {
      // Skip fetch when workspace panel is minimized — mark stale for manual refresh
      if (workspaceMinimized) {
        useWorkspaceStore.getState().setNeedsRefresh(true)
        return
      }
      // Fetch files first to ensure folder exists, then expand
      fetchFiles(workspacePath || undefined).then(() => {
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
            // Find the group to get its display name
            const group = variablesManifest.groups?.find(g => g.group_id === groupId)

            // Use sanitized display_name if available, otherwise use group_id
            // Sanitization ensures consistent folder naming with backend
            const folderName = group?.display_name
              ? sanitizeDisplayNameForFolder(group.display_name)
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

  // Get plan data to map step indices to step IDs
  const { plan } = usePlanData(workspacePath)

  // Reset lastProcessedEventIndexRef when switching tabs/sessions to ensure we process events for the new session
  useEffect(() => {
    lastProcessedEventIndexRef.current = -1
  }, [activeTab?.sessionId])

  // Listen for todo_steps_extracted events to auto-refresh the canvas (with granular data from backend)
  useEffect(() => {
    if (events.length === 0) return
    
    // Find new todo_steps_extracted events that we haven't processed yet
    for (let i = lastProcessedEventIndexRef.current + 1; i < events.length; i++) {
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
      lastProcessedEventIndexRef.current = i
    }
  }, [events])

  // Listen for step_progress_updated events to refresh workspace files for current iteration
  useEffect(() => {
    if (events.length === 0 || !workspacePath) {
      return
    }

    // Find new step_progress_updated events that we haven't processed yet
    for (let i = lastProcessedStepProgressIndexRef.current + 1; i < events.length; i++) {
      const event = events[i]

      if (event.type === 'step_progress_updated') {
        // Use typed helper function to get properly typed event data
        const eventData = getTypedEventData(event, 'step_progress_updated')

        if (!eventData) {
          continue
        }

        // Check if this event is for the current workspace
        const isForCurrentWorkspace = eventData.workspace_path === workspacePath
        // Check if selectedRunFolder matches OR if selectedRunFolder is 'new' (just started execution)
        // When selectedRunFolder is 'new', we should still process events to update the store
        const isForCurrentOrNewRun =
          selectedRunFolder === 'new' ||
          selectedRunFolder === eventData.run_folder

        // Process if this event is for the current workspace and either:
        // 1. Matches the selected run folder, OR
        // 2. We just started execution (selectedRunFolder is 'new')
        if (isForCurrentWorkspace && isForCurrentOrNewRun) {
          // If selectedRunFolder is 'new', update it to the actual run folder from the event
          if (selectedRunFolder === 'new' && eventData.run_folder) {
            setSelectedRunFolder(eventData.run_folder)
          }

          // Auto-focus disabled - running step name is now shown in StepLegend instead
          // This prevents the canvas from jumping around during workflow execution

          // PERF FIX: Mark workspace stale instead of calling debouncedFetchFiles().
          //
          // PROBLEM: Previously called debouncedFetchFiles(workspacePath) on every
          // step_progress_updated event (500ms debounce). Each fetch is ~2-3MB for large
          // workspaces. During a 10-step workflow, this triggered 10+ fetches.
          //
          // FIX: Set needsRefresh flag. New files are added incrementally via addFileToTree
          // (from workspace_file_operation events, no network call). The Workspace component
          // shows a "Files may be out of date" banner for manual refresh.
          useWorkspaceStore.getState().setNeedsRefresh(true)
          
          lastProcessedStepProgressIndexRef.current = i
        }
      }
    }
  }, [events, workspacePath, selectedRunFolder, setSelectedRunFolder, updateStepProgressFromEvent, plan])

  // Track if reconnection has already been attempted to prevent duplicates
  const hasReconnectedRef = useRef(false)

  // Reset reconnection flag when preset changes so new preset's sessions get reconnected
  useEffect(() => {
    hasReconnectedRef.current = false
  }, [activePresetId])

  // Reconnect workflow tabs on page refresh by matching active sessions with same presetQueryId
  useEffect(() => {
    // Only run when we have an active preset
    if (!activePresetId) {
      return
    }

    // Prevent multiple reconnection attempts
    if (hasReconnectedRef.current) {
      return
    }

    const reconnectWorkflowTabs = async () => {
      // Mark as attempted immediately to prevent race conditions
      hasReconnectedRef.current = true
      try {
        // First, check for and clean up any duplicate tabs (same sessionId)
        const { chatTabs, closeTab } = useChatStore.getState()
        const sessionIdToTabId = new Map<string, string[]>()
        Object.values(chatTabs).forEach(tab => {
          if (tab.sessionId && tab.metadata?.mode === 'workflow') {
            if (!sessionIdToTabId.has(tab.sessionId)) {
              sessionIdToTabId.set(tab.sessionId, [])
            }
            sessionIdToTabId.get(tab.sessionId)!.push(tab.tabId)
          }
        })
        
        // Close duplicate tabs (keep the first one)
        for (const [sessionId, tabIds] of sessionIdToTabId.entries()) {
          if (tabIds.length > 1) {
            logger.warn('Reconnection', `Found ${tabIds.length} duplicate tabs for session ${sessionId}, closing duplicates:`, tabIds)
            for (let i = 1; i < tabIds.length; i++) {
              await closeTab(tabIds[i])
            }
          }
        }
        
        // Get all active sessions from cache
        const { getActiveSessions } = useChatStore.getState()
        const activeSessions = await getActiveSessions()

        // Get chat store functions
        const { createChatTab, switchTab } = useChatStore.getState()
        const { getPhaseById } = useWorkflowStore.getState()

        // If no active sessions, skip Phase 1 but still run Phase 2 cleanup
        if (activeSessions.length === 0) {
          // Close all persisted workflow tabs — none have active sessions
          const allWorkflowTabs = Object.values(useChatStore.getState().chatTabs)
            .filter(tab => tab.metadata?.mode === 'workflow' && tab.sessionId)
          if (allWorkflowTabs.length > 0) {
            logger.debug('Reconnection', `No active sessions — closing ${allWorkflowTabs.length} stale workflow tabs`)
            const { closeTab } = useChatStore.getState()
            for (const tab of allWorkflowTabs) {
              closeTab(tab.tabId)
            }
          }
          return
        }

        // Phase 1: Process active sessions in parallel
        logger.debug('Reconnection', `Phase 1: Checking ${activeSessions.length} active sessions (preset: ${activePresetId})`)
        const processSession = async (activeSession: typeof activeSessions[number]) => {
          if (activeSession.agent_mode !== 'workflow') {
            return
          }

          // Fetch chat session to get preset_query_id
          let chatSession
          let presetQueryId: string | undefined
          let source = 'unknown'
          try {
            chatSession = await agentApi.getChatSession(activeSession.session_id)
            presetQueryId = chatSession.preset_query_id
            source = 'api'
          } catch {
            const existingTabsForSession = Object.values(useChatStore.getState().chatTabs)
              .filter(tab => tab.sessionId === activeSession.session_id && tab.metadata?.mode === 'workflow')

            if (existingTabsForSession.length > 0) {
              presetQueryId = existingTabsForSession[0].metadata?.presetQueryId
              source = 'existing-tab'
            } else {
              source = 'failed-lookup'
              return
            }
          }

          logger.debug('Reconnection', `Session ${activeSession.session_id}: preset=${presetQueryId} (${source})`)

          // Extract phase ID from query
          let phaseId: string | null = null
          if (activeSession.query) {
            const match = activeSession.query.match(/Execute workflow phase:\s*(\w+)(?:\s|$|\n)/i)
            if (match && match[1]) {
              phaseId = match[1]
            } else {
              const simpleMatch = activeSession.query.match(/phase:\s*(\w+)/i)
              if (simpleMatch && simpleMatch[1]) {
                phaseId = simpleMatch[1]
              }
            }
          }

          if (!phaseId) {
            return
          }

          // STRICT RESTORATION: Only reconnect if the session explicitly belongs to this preset
          // OR if the session is an orphan (no preset ID) and we have an active preset (adopt it)
          const shouldReconnect = presetQueryId === activePresetId || (!presetQueryId && activePresetId)
          logger.debug('Reconnection', `Session ${activeSession.session_id} reconnect=${shouldReconnect}`)

          if (!shouldReconnect) {
            return
          }

          logger.debug('Reconnection', `Reconnecting session ${activeSession.session_id} (phase: ${phaseId})`)

          // Check if we already have a tab for this session
          const existingTabs = Object.values(useChatStore.getState().chatTabs)

          // First, check if ANY tab already has this sessionId
          let existingTab = existingTabs.find(tab =>
            tab.sessionId === activeSession.session_id &&
            tab.metadata?.mode === 'workflow'
          )

          if (!existingTab) {
            // Check by phaseId and presetQueryId
            const matchingTabs = existingTabs.filter(tab =>
              tab.metadata?.mode === 'workflow' &&
              tab.metadata?.phaseId === phaseId &&
              (tab.metadata?.presetQueryId === activePresetId || (!tab.metadata?.presetQueryId && activePresetId))
            )

            if (matchingTabs.length > 0) {
              existingTab = matchingTabs[0]
              if (existingTab.sessionId !== activeSession.session_id) {
                logger.debug('Reconnection', `Updating tab ${existingTab.tabId} sessionId to ${activeSession.session_id}`)
                const { updateTabSessionId } = useChatStore.getState()
                updateTabSessionId(existingTab.tabId, activeSession.session_id)
                existingTab = { ...existingTab, sessionId: activeSession.session_id }
              }
            }
          }

          if (existingTab) {

            // Update tab's metadata if presetQueryId is missing
            if (!existingTab.metadata?.presetQueryId && activePresetId) {
              const state = useChatStore.getState()
              const updatedTab = {
                ...existingTab,
                metadata: {
                  ...existingTab.metadata,
                  presetQueryId: activePresetId
                }
              }
              useChatStore.setState({
                chatTabs: {
                  ...state.chatTabs,
                  [existingTab.tabId]: updatedTab
                }
              })
            }

            switchTab(existingTab.tabId)
            setShowChatArea(true)

            // Restore batch progress from events (shows batch box immediately after refresh)
            await restoreWorkflowStateFromEvents(activeSession.session_id)
            if (activeSession.status === 'running') {
              useChatStore.getState().setTabStreaming(existingTab.tabId, true)
            }

            return
          }

          // Final safety check
          const finalCheck = Object.values(useChatStore.getState().chatTabs).find(
            tab => tab.sessionId === activeSession.session_id &&
            tab.metadata?.mode === 'workflow'
          )
          if (finalCheck) {
            logger.warn('Reconnection', `Race condition detected: Tab ${finalCheck.tabId} exists with sessionId ${activeSession.session_id}`)
            switchTab(finalCheck.tabId)
            setShowChatArea(true)
            // Restore batch progress from events (shows batch box immediately after refresh)
            await restoreWorkflowStateFromEvents(activeSession.session_id)
            if (activeSession.status === 'running') {
              useChatStore.getState().setTabStreaming(finalCheck.tabId, true)
            }
            return
          }

          // Create a new tab
          const phase = getPhaseById(phaseId)
          const phaseName = phase?.title || phaseId

          logger.debug('Reconnection', `Creating tab for phase ${phaseId}, session ${activeSession.session_id}`)
          const tabId = await createChatTab(phaseName, {
            mode: 'workflow',
            phaseId,
            phaseName,
            presetQueryId: activePresetId
          }, activeSession.session_id)

          switchTab(tabId)
          setShowChatArea(true)

          // Restore batch progress from events (shows batch box immediately after refresh)
          await restoreWorkflowStateFromEvents(activeSession.session_id)
          if (activeSession.status === 'running') {
            useChatStore.getState().setTabStreaming(tabId, true)
          }
        }
        await Promise.allSettled(activeSessions.map(processSession))

        // Phase 2: Clean up stale persisted tabs that are not active sessions
        // Previously this hydrated ALL old tabs (loading events from backend), which caused
        // the page to hang. Now we just close inactive tabs to free resources.
        const activeSessionIdSet = new Set(activeSessions.map(s => s.session_id))
        const staleTabs = Object.values(useChatStore.getState().chatTabs)
          .filter(tab =>
            tab.metadata?.mode === 'workflow' &&
            tab.sessionId &&
            !activeSessionIdSet.has(tab.sessionId)
          )

        if (staleTabs.length > 0) {
          logger.debug('Reconnection', `Phase 2: Closing ${staleTabs.length} inactive workflow tabs (no active session)`)
          const { closeTab } = useChatStore.getState()
          for (const tab of staleTabs) {
            closeTab(tab.tabId)
          }
        }
      } catch (error) {
        logger.error('Reconnection', 'Error during reconnection:', error)
      }
    }

    // Run reconnection check after a short delay to ensure stores are initialized
    const timeoutId = setTimeout(reconnectWorkflowTabs, 500)
    return () => clearTimeout(timeoutId)
  }, [activePresetId, setShowChatArea])


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

    // Check if preset actually changed
    if (previousPresetIdRef.current !== activePresetId && activePresetId) {
      const chatStore = useChatStore.getState()
      const chatTabs = chatStore.chatTabs

      // Find ALL workflow tabs with active sessions that are NOT for the NEW preset
      // This ensures we minimize any running workflows when switching presets
      const tabsToMinimize = Object.values(chatTabs).filter(tab =>
        tab.metadata?.mode === 'workflow' &&
        tab.sessionId &&
        // Minimize if: streaming, or has old presetId, or has no presetId (legacy tabs)
        (tab.isStreaming ||
         tab.metadata?.presetQueryId === previousPresetIdRef.current ||
         tab.metadata?.presetQueryId !== activePresetId)
      )

      if (tabsToMinimize.length > 0) {
        logger.debug('WorkflowLayout', `Preset changed from ${previousPresetIdRef.current} to ${activePresetId}, auto-minimizing ${tabsToMinimize.length} workflows`)

        // Minimize each running tab
        for (const tab of tabsToMinimize) {
          if (tab.sessionId) {
            // Determine which preset this tab belongs to
            const tabPresetId = tab.metadata?.presetQueryId || previousPresetIdRef.current || 'unknown'
            const tabCustomPreset = customPresets.find(p => p.id === tabPresetId)
            const tabPredefinedPreset = predefinedPresets.find(p => p.id === tabPresetId)
            const tabPresetLabel = tabCustomPreset?.label || tabPredefinedPreset?.label || tab.name || 'Workflow'
            const tabWorkspacePath = tabCustomPreset?.selectedFolder?.filepath || tabPredefinedPreset?.selectedFolder?.filepath || ''

            minimizeWorkflow({
              presetId: tabPresetId,
              presetName: tabPresetLabel,
              workspacePath: tabWorkspacePath,
              sessionId: tab.sessionId,
              runFolder: selectedRunFolder || '',
              phaseId: tab.metadata?.phaseId || 'unknown',
              phaseName: tab.name,
              progress: stepProgress || undefined,
              selectedGroupIds: useWorkflowStore.getState().selectedGroupIds
            })

            // Close the tab UI (keep session running in background)
            chatStore.closeTab(tab.tabId, false, true)
          }
        }

        // Close the chat area to show the new preset's canvas
        setShowChatArea(false)
      }
    }

    // Update the ref for next comparison
    previousPresetIdRef.current = activePresetId
  }, [activePresetId, customPresets, predefinedPresets, minimizeWorkflow, selectedRunFolder, stepProgress, setShowChatArea])

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

    // Store pending query to submit after ChatArea mounts
    const query = `Execute workflow phase: ${phaseId}`
    pendingQueryRef.current = { query, executionOptions }

    // Show ChatArea when starting a phase (triggers ChatArea mount)
    setShowChatArea(true)
  }, [activePresetId, setCurrentWorkflowPhase, setShowChatArea, getPhaseById])

  // Handle create plan - starts the planning or evaluation-designer phase depending on workflow mode
  const handleCreatePlan = useCallback(() => {
    // Ensure we're in workflow mode before creating plan (only if we have an active preset)
    if (activePresetId) {
      const currentMode = useModeStore.getState().selectedModeCategory
      if (currentMode !== 'workflow') {
        useModeStore.getState().setModeCategory('workflow')
      }
    }

    const workflowMode = useWorkflowStore.getState().workflowMode
    const phases = useWorkflowStore.getState().phases

    if (workflowMode === 'eval') {
      const evalDesignerPhase = phases.find(p => p.id === 'evaluation-designer')
      const evalPhaseId = evalDesignerPhase?.id || 'evaluation-designer'
      logger.debug('WorkflowLayout', 'Create eval plan requested, starting eval designer phase:', evalPhaseId)
      setShowChatArea(true)
      handleStartPhase(evalPhaseId)
    } else {
      // Look for the "planning" phase explicitly, fallback to second phase (index 1) if not found
      const planningPhase = phases.find(p => p.id === 'planning') || (phases.length > 1 ? phases[1] : phases[0])
      const planningPhaseId = planningPhase?.id || 'planning'
      logger.debug('WorkflowLayout', 'Create plan requested, starting planning phase:', planningPhaseId)
      setShowChatArea(true)
      handleStartPhase(planningPhaseId)
    }
  }, [handleStartPhase, setShowChatArea, activePresetId])

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
  if (!activeWorkflowPreset) {
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

  return (
    <div className={`flex flex-col h-full ${className}`}>
      {/* Main Content */}
      <div className="flex-1 flex min-h-0 relative">
        {/* Canvas - main area, shrinks when ChatArea is shown */}
        <div className={`flex-1 min-w-0 transition-all duration-300 ${showChatArea ? (chatAreaExpanded ? 'w-1/4' : 'w-1/2') : ''}`}>
          <WorkflowCanvas
            ref={canvasRef}
            workspacePath={workspacePath}
            presetQueryId={activePresetId}
            currentPhase={activePhase || currentWorkflowPhase}
            onStartPhase={handleStartPhase}
            onCreatePlan={onCreatePlan || handleCreatePlan}
            showChatArea={showChatArea}
            onToggleChatArea={() => {
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
            }}
            className="h-full"
          />
        </div>

        {/* ChatArea Panel - appears on right side, positioned below toolbar */}
        <div className={`${showChatArea ? (chatAreaExpanded ? 'w-3/4' : 'w-1/2') : 'w-0 overflow-hidden'} border-l border-gray-200 dark:border-gray-700 flex flex-col min-h-0 bg-white dark:bg-gray-900 absolute right-0 top-0 bottom-0 transition-all duration-300`} style={{ top: '40px' }}>
          {showChatArea && (
            <>
              {/* Workflow Chat Tabs - only shows active workflow tabs */}
              <div className="flex-shrink-0">
                <WorkflowChatTabs />
              </div>

              {/* Single ChatArea component - takes remaining space */}
              <div className="flex-1 min-h-0">
                <ChatAreaWithObserverId
                  ref={chatAreaCallbackRef}
                  onNewChat={onNewChat}
                  hideHeader
                  hideInput
                  compact
                />
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  )
}

export default WorkflowLayout
