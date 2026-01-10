import React, { useMemo, useCallback, useRef, useEffect, forwardRef } from 'react'
import { WorkflowCanvas, type WorkflowCanvasRef } from './canvas'
import { useGlobalPresetStore } from '../../stores/useGlobalPresetStore'
import { useModeStore } from '../../stores/useModeStore'
import { useChatStore, type ChatTab } from '../../stores/useChatStore'
import { useWorkflowStore } from '../../stores/useWorkflowStore'
import { useWorkspaceStore } from '../../stores/useWorkspaceStore'
import ChatArea, { type ChatAreaRef } from '../ChatArea'
import { ChatHeader } from '../ChatHeader'
import { WorkflowChatTabs } from './WorkflowChatTabs'
import { RunningWorkflowsIndicator } from './RunningWorkflowsIndicator'
import { RunningWorkflowsDrawer } from './RunningWorkflowsDrawer'
import { useRunningWorkflowsStore, type RunningWorkflow } from '../../stores/useRunningWorkflowsStore'
import { sanitizeDisplayNameForFolder } from '../../utils/workflowUtils'

// Helper component to get observerId and render ChatArea
// Always renders ChatArea (even without observerId) so it can handle initialization
const ChatAreaWithObserverId = forwardRef<ChatAreaRef, { 
  onNewChat: () => void
  hideHeader?: boolean
  hideInput?: boolean
  compact?: boolean
}>(({ onNewChat, hideHeader, hideInput, compact }, ref) => {
  // Always render ChatArea - it will handle the case when sessionId is undefined
  return (
    <ChatArea
      ref={ref}
      onNewChat={onNewChat}
      hideHeader={hideHeader}
      hideInput={hideInput}
      compact={compact}
      tabId={useChatStore.getState().activeTabId || undefined}
    />
  )
})
import { agentApi } from '../../services/api'
import { type ExecutionOptions } from '../../services/api-types'
import { getRawEventData } from '../../generated/event-types'
import { usePlanData } from './hooks/usePlanData'

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
  const events = React.useMemo(() => {
    return activeTab?.sessionId ? getTabEvents(activeTab.sessionId) : []
  }, [activeTab?.sessionId, getTabEvents])
  
  // Use workflow store for UI state (single source of truth)
  const activePhase = useWorkflowStore(state => state.activePhase)
  const showChatArea = useWorkflowStore(state => state.showChatArea)
  const setShowChatArea = useWorkflowStore(state => state.setShowChatArea)
  const minimizeWorkflow = useRunningWorkflowsStore(state => state.minimizeWorkflow)
  const stepProgress = useWorkflowStore(state => state.stepProgress)

  // Tab management (generalized for both chat and workflow)
  const { createChatTab, getActiveTab } = useChatStore()
  const getPhaseById = useWorkflowStore(state => state.getPhaseById)
  
  // Ref for the ChatArea component
  const chatAreaRef = useRef<ChatAreaRef>(null)
  // Ref for the WorkflowCanvas component (for triggering refresh)
  const canvasRef = useRef<WorkflowCanvasRef>(null)
  // Track the last processed event index to avoid duplicate refreshes
  const lastProcessedEventIndexRef = useRef(-1)
  // Track the last processed step progress event index to avoid duplicate workspace refreshes
  const lastProcessedStepProgressIndexRef = useRef(-1)
  // Track the last processed step execution start event index to avoid duplicate focus calls
  const lastProcessedStepStartIndexRef = useRef(-1)
  // Store pending query to submit after ChatArea mounts
  const pendingQueryRef = useRef<{ query: string; executionOptions?: ExecutionOptions } | null>(null)
  // Track the previous preset ID for auto-minimize on preset switch
  const previousPresetIdRef = useRef<string | null>(null)

  // Get selected run folder and workspace functions (defined early for use in useEffect)
  const selectedRunFolder = useWorkflowStore(state => state.selectedRunFolder)
  const setSelectedRunFolder = useWorkflowStore(state => state.setSelectedRunFolder)
  const updateStepProgressFromEvent = useWorkflowStore(state => state.updateStepProgressFromEvent)
  const selectedGroupIds = useWorkflowStore(state => state.selectedGroupIds)
  const variablesManifest = useWorkflowStore(state => state.variablesManifest)
  const { fetchFiles, setExpandedFolders } = useWorkspaceStore()

  // Get active workflow preset
  const activePresetId = useGlobalPresetStore(state => state.activePresetIds.workflow)
  const customPresets = useGlobalPresetStore(state => state.customPresets)
  const predefinedPresets = useGlobalPresetStore(state => state.predefinedPresets)

  const activeWorkflowPreset = useMemo(() => {
    if (selectedModeCategory === 'workflow' && activePresetId) {
      const customPreset = customPresets.find(p => p.id === activePresetId)
      if (customPreset) return customPreset

      const predefinedPreset = predefinedPresets.find(p => p.id === activePresetId)
      if (predefinedPreset) return predefinedPreset
    }
    return null
  }, [selectedModeCategory, activePresetId, customPresets, predefinedPresets])

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
      const folderPath = `${workspacePath}/runs/${selectedRunFolder}`
      console.log('[WorkflowLayout] Auto-expanding selected run folder in workspace:', folderPath)
      console.log('[WorkflowLayout] Selected group IDs:', selectedGroupIds)

      // Fetch files first to ensure folder exists, then expand
      console.log('[WorkflowLayout] Starting fetchFiles...')
      fetchFiles().then(() => {
        console.log('[WorkflowLayout] fetchFiles completed successfully')
        // Collapse all other iteration folders first
        const workspaceStore = useWorkspaceStore.getState()
        const expandedFolders = workspaceStore.expandedFolders
        const runsPath = `${workspacePath}/runs`

        console.log('[WorkflowLayout] Current expanded folders:', Array.from(expandedFolders))
        console.log('[WorkflowLayout] Runs path:', runsPath)
        console.log('[WorkflowLayout] Target folder path:', folderPath)

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
          } else {
            console.log('[WorkflowLayout] Collapsing iteration folder:', folder)
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
          console.log('[WorkflowLayout] Expanding selected groups:', selectedGroupIds)

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
            console.log('[WorkflowLayout] Adding group folder to expanded:', groupPath)

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

        console.log('[WorkflowLayout] New expanded folders:', Array.from(newExpandedFolders))

        // Update the expanded folders using the proper setter
        setExpandedFolders(newExpandedFolders)

        console.log('[WorkflowLayout] Collapsed other iterations, expanded only:', selectedRunFolder)
      }).catch(error => {
        console.error('[WorkflowLayout] Failed to fetch files for auto-expansion:', error)
      })
    }
  }, [selectedRunFolder, selectedGroupIds, workspacePath, variablesManifest, fetchFiles, setExpandedFolders])

  // Callback ref that gets called when ChatArea mounts/unmounts
  const chatAreaCallbackRef = useCallback((node: ChatAreaRef | null) => {
    chatAreaRef.current = node

    // When ChatArea mounts and we have a pending query, submit it
    if (node && pendingQueryRef.current) {
      const { query, executionOptions } = pendingQueryRef.current
      console.log('[WorkflowLayout] ChatArea mounted via callback ref, submitting pending query:', query)
      node.submitQuery(query, executionOptions).catch(error => {
        console.error('[WorkflowLayout] Failed to submit pending query:', error)
      })
      pendingQueryRef.current = null // Clear pending query after submission
    }
  }, [])

  // Get plan data to map step indices to step IDs
  const { plan } = usePlanData(workspacePath)

  // Listen for todo_steps_extracted events to auto-refresh the canvas (with granular data from backend)
  useEffect(() => {
    if (events.length === 0) return
    
    console.log(`[WorkflowPlanUpdate] Processing events (total: ${events.length}, last processed: ${lastProcessedEventIndexRef.current})`)
    
    // Find new todo_steps_extracted events that we haven't processed yet
    for (let i = lastProcessedEventIndexRef.current + 1; i < events.length; i++) {
      const event = events[i]
      console.log(`[WorkflowPlanUpdate] Event ${i}: type=${event.type}, timestamp=${event.timestamp}`)
      
      if (event.type === 'todo_steps_extracted') {
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
          console.warn('[WorkflowPlanUpdate] Could not extract event data from event:', event)
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
        
        console.log(`[WorkflowPlanUpdate] Detected plan update event:`, {
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
          console.log('[WorkflowPlanUpdate] Calling canvasRef.current.refresh() with granular changes')
          canvasRef.current.refresh(changedStepIDs, deletedStepIDs).then((changes) => {
            console.log('[WorkflowPlanUpdate] Canvas refresh completed, changes:', changes)
          }).catch((err) => {
            console.error('[WorkflowPlanUpdate] Canvas refresh failed:', err)
          })
        } else {
          console.warn('[WorkflowPlanUpdate] canvasRef.current is null, cannot refresh')
        }
        lastProcessedEventIndexRef.current = i
      }
    }
  }, [events])

  // Listen for step_execution_start events to auto-focus on running steps
  useEffect(() => {
    if (events.length === 0 || !canvasRef.current) {
      return
    }
    
    // Find new step_execution_start events that we haven't processed yet
    for (let i = lastProcessedStepStartIndexRef.current + 1; i < events.length; i++) {
      const event = events[i]
      
      if (event.type === 'step_execution_start') {
        // Use helper function to extract raw event data (handles nested structure)
        const rawData = getRawEventData(event)
        const eventData = rawData as {
          step_id?: string
          run_folder?: string
          workspace_path?: string
        } | undefined
        
        const stepId = eventData?.step_id
        const runFolder = eventData?.run_folder
        const workspacePathFromEvent = eventData?.workspace_path
        
        // Only focus if this event is for the current workspace and run folder
        if (stepId && runFolder === selectedRunFolder && workspacePathFromEvent === workspacePath) {
          console.log('[WorkflowLayout] Step started, focusing on step:', {
            stepId,
            runFolder,
            workspacePath: workspacePathFromEvent,
            eventIndex: i
          })
          
          // Focus on the running step
          canvasRef.current.focusStep(stepId)
          
          lastProcessedStepStartIndexRef.current = i
        }
      }
    }
  }, [events, workspacePath, selectedRunFolder])

  // Listen for step_progress_updated events to refresh workspace files for current iteration
  useEffect(() => {
    if (events.length === 0 || !workspacePath) {
      return
    }

    // Find new step_progress_updated events that we haven't processed yet
    for (let i = lastProcessedStepProgressIndexRef.current + 1; i < events.length; i++) {
      const event = events[i]

      if (event.type === 'step_progress_updated') {
        // Use helper function to extract raw event data (handles nested structure)
        const rawData = getRawEventData(event)
        const eventData = rawData as {
          run_folder?: string
          workspace_path?: string
          completed_step_indices?: number[]
          total_steps?: number
          last_completed_step?: number
          last_completed_step_id?: string  // Step ID for direct node updates (new field from backend)
          last_completed_step_title?: string
          branch_steps?: {
            [k: string]: {
              branch_executed: string
              completed_steps: string[]
            }
          }
        } | undefined

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

        console.log('[WorkflowLayout] step_progress_updated event received:', {
          eventRunFolder: eventData.run_folder,
          eventWorkspacePath: eventData.workspace_path,
          selectedRunFolder,
          workspacePath,
          isForCurrentWorkspace,
          isForCurrentOrNewRun,
          willProcess: isForCurrentWorkspace && isForCurrentOrNewRun,
          completedIndices: eventData.completed_step_indices,
          lastCompleted: eventData.last_completed_step
        })

        // Process if this event is for the current workspace and either:
        // 1. Matches the selected run folder, OR
        // 2. We just started execution (selectedRunFolder is 'new')
        if (isForCurrentWorkspace && isForCurrentOrNewRun) {
          // If selectedRunFolder is 'new', update it to the actual run folder from the event
          if (selectedRunFolder === 'new' && eventData.run_folder) {
            console.log('[WorkflowLayout] Updating selectedRunFolder from new to:', eventData.run_folder)
            setSelectedRunFolder(eventData.run_folder)
          }
          console.log('[WorkflowLayout] Step progress updated for current iteration:', {
            runFolder: eventData.run_folder,
            completedSteps: eventData.completed_step_indices?.length || 0,
            totalSteps: eventData.total_steps || 0,
            lastCompletedStep: eventData.last_completed_step,
            eventIndex: i
          })
          
          // Update the workflow store's stepProgress from event data
          // This ensures completedStepIndices is updated in real-time during execution
          if (eventData.completed_step_indices !== undefined && eventData.total_steps !== undefined) {
            // Convert branch_steps keys from string to number (event has string keys, StepProgress expects number keys)
            const branchSteps: Record<number, { branch_executed: string; completed_steps: string[] }> | undefined =
              eventData.branch_steps ? Object.fromEntries(
                Object.entries(eventData.branch_steps).map(([key, value]) => [parseInt(key, 10), value])
              ) : undefined

            updateStepProgressFromEvent({
              completed_step_indices: eventData.completed_step_indices,
              total_steps: eventData.total_steps,
              branch_steps: branchSteps,
              last_updated: new Date().toISOString(),
              last_completed_step_id: eventData.last_completed_step_id  // Pass step ID for direct node updates
            })
          }
          
          // Auto-focus on the last completed step if available
          // Use last_completed_step_id directly from event (no index mapping needed)
          if (eventData.last_completed_step_id && canvasRef.current) {
            console.log('[WorkflowLayout] Focusing on completed step using step_id:', {
              stepId: eventData.last_completed_step_id,
              stepTitle: eventData.last_completed_step_title,
              stepIndex: eventData.last_completed_step
            })
            canvasRef.current.focusStep(eventData.last_completed_step_id)
          } else if (eventData.last_completed_step !== undefined && eventData.last_completed_step >= 0 && plan?.steps && canvasRef.current) {
            // Fallback: use index mapping if step_id not available (backwards compatibility)
            const stepIndex = eventData.last_completed_step
            if (stepIndex < plan.steps.length) {
              const completedStep = plan.steps[stepIndex]
              if (completedStep?.id) {
                console.log('[WorkflowLayout] Focusing on completed step using index mapping:', {
                  stepIndex,
                  stepId: completedStep.id,
                  stepTitle: completedStep.title
                })
                canvasRef.current.focusStep(completedStep.id)
              }
            }
          }
          
          // Refresh workspace files to show new execution files
          fetchFiles().catch((err) => {
            console.error('[WorkflowLayout] Failed to refresh workspace files:', err)
          })
          
          lastProcessedStepProgressIndexRef.current = i
        }
      }
    }
  }, [events, workspacePath, selectedRunFolder, setSelectedRunFolder, fetchFiles, updateStepProgressFromEvent, plan])

  // Track if reconnection has already been attempted to prevent duplicates
  const hasReconnectedRef = useRef(false)

  // Reconnect workflow tabs on page refresh by matching active sessions with same presetQueryId
  useEffect(() => {
    // Only run in workflow mode and when we have an active preset
    if (selectedModeCategory !== 'workflow' || !activePresetId) {
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
            console.warn(`[DUPLICATE_DEBUG] Found ${tabIds.length} duplicate tabs for session ${sessionId}, closing duplicates:`, tabIds)
            for (let i = 1; i < tabIds.length; i++) {
              await closeTab(tabIds[i])
            }
          }
        }
        
        // Get all active sessions from cache
        const { getActiveSessions } = useChatStore.getState()
        const activeSessions = await getActiveSessions()
        
        if (activeSessions.length === 0) {
          return
        }

        // Get chat store functions
        const { createChatTab, switchTab } = useChatStore.getState()
        const { getPhaseById } = useWorkflowStore.getState()

        // Check each active session
        for (const activeSession of activeSessions) {
          try {
            if (activeSession.agent_mode !== 'workflow') {
              continue
            }

            // Fetch chat session to get preset_query_id
            let chatSession
            let presetQueryId: string | undefined
            try {
              chatSession = await agentApi.getChatSession(activeSession.session_id)
              presetQueryId = chatSession.preset_query_id
            } catch {
              const existingTabsForSession = Object.values(useChatStore.getState().chatTabs)
                .filter(tab => tab.sessionId === activeSession.session_id && tab.metadata?.mode === 'workflow')
              
              if (existingTabsForSession.length > 0) {
                presetQueryId = existingTabsForSession[0].metadata?.presetQueryId
              } else {
                continue
              }
            }
            
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
              continue
            }

            const shouldReconnect = presetQueryId === activePresetId || (!presetQueryId && activePresetId)
            if (!shouldReconnect) {
              continue
            }

            // DEBUG: Log all existing tabs before checking
            const allTabsBefore = Object.values(useChatStore.getState().chatTabs)
              .filter(tab => tab.metadata?.mode === 'workflow')
              .map(tab => ({
                tabId: tab.tabId,
                sessionId: tab.sessionId,
                phaseId: tab.metadata?.phaseId,
                presetQueryId: tab.metadata?.presetQueryId,
                name: tab.name
              }))
            console.log(`[DUPLICATE_DEBUG] Reconnecting session ${activeSession.session_id} (phaseId: ${phaseId}, presetQueryId: ${activePresetId})`)
            console.log(`[DUPLICATE_DEBUG] Existing workflow tabs BEFORE check:`, allTabsBefore)

            // Check if we already have a tab for this session
            const existingTabs = Object.values(useChatStore.getState().chatTabs)
            
            // First, check if ANY tab already has this sessionId
            let existingTab = existingTabs.find(tab => 
              tab.sessionId === activeSession.session_id &&
              tab.metadata?.mode === 'workflow'
            )

            if (existingTab) {
              console.log(`[DUPLICATE_DEBUG] ✅ Found tab by sessionId: ${existingTab.tabId}`)
            } else {
              // Check by phaseId and presetQueryId
              const matchingTabs = existingTabs.filter(tab => 
                tab.metadata?.mode === 'workflow' &&
                tab.metadata?.phaseId === phaseId &&
                (tab.metadata?.presetQueryId === activePresetId || (!tab.metadata?.presetQueryId && activePresetId))
              )
              
              console.log(`[DUPLICATE_DEBUG] Tabs matching phaseId=${phaseId} AND presetQueryId=${activePresetId}:`, 
                matchingTabs.map(t => ({ tabId: t.tabId, sessionId: t.sessionId, phaseId: t.metadata?.phaseId, presetQueryId: t.metadata?.presetQueryId })))
              
              if (matchingTabs.length > 0) {
                existingTab = matchingTabs[0]
                console.log(`[DUPLICATE_DEBUG] ✅ Found tab by phaseId/presetQueryId: ${existingTab.tabId} (sessionId: ${existingTab.sessionId})`)
                
                if (existingTab.sessionId !== activeSession.session_id) {
                  console.log(`[DUPLICATE_DEBUG] Updating sessionId from ${existingTab.sessionId} to ${activeSession.session_id}`)
                  const { updateTabSessionId } = useChatStore.getState()
                  updateTabSessionId(existingTab.tabId, activeSession.session_id)
                  existingTab = { ...existingTab, sessionId: activeSession.session_id }
                }
              } else {
                console.log(`[DUPLICATE_DEBUG] ❌ No matching tab found for phaseId=${phaseId}, presetQueryId=${activePresetId}`)
              }
            }

            if (existingTab) {
              console.log(`[DUPLICATE_DEBUG] ✅ Reusing existing tab ${existingTab.tabId}, NOT creating duplicate`)
              
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
              
              if (!presetQueryId && activePresetId) {
                try {
                  await agentApi.updateChatSession(activeSession.session_id, {
                    preset_query_id: activePresetId
                  })
                } catch (error) {
                  console.error(`[DUPLICATE_DEBUG] Failed to update chat session:`, error)
                }
              }
              
              continue
            }

            // Final safety check
            const finalCheck = Object.values(useChatStore.getState().chatTabs).find(
              tab => tab.sessionId === activeSession.session_id &&
              tab.metadata?.mode === 'workflow'
            )
            if (finalCheck) {
              console.warn(`[DUPLICATE_DEBUG] ⚠️ Race condition detected: Tab ${finalCheck.tabId} exists with sessionId ${activeSession.session_id}`)
              switchTab(finalCheck.tabId)
              setShowChatArea(true)
              continue
            }

            // Create a new tab
            const phase = getPhaseById(phaseId)
            const phaseName = phase?.title || phaseId
            
            console.log(`[DUPLICATE_DEBUG] 🆕 Creating NEW tab for phaseId=${phaseId}, sessionId=${activeSession.session_id}`)
            const tabId = await createChatTab(phaseName, {
              mode: 'workflow',
              phaseId,
              phaseName,
              presetQueryId: activePresetId
            }, activeSession.session_id)

            // DEBUG: Log all tabs after creation
            const allTabsAfter = Object.values(useChatStore.getState().chatTabs)
              .filter(tab => tab.metadata?.mode === 'workflow')
              .map(tab => ({
                tabId: tab.tabId,
                sessionId: tab.sessionId,
                phaseId: tab.metadata?.phaseId,
                presetQueryId: tab.metadata?.presetQueryId,
                name: tab.name
              }))
            console.log(`[DUPLICATE_DEBUG] Existing workflow tabs AFTER creation:`, allTabsAfter)

            switchTab(tabId)
            setShowChatArea(true)
            
            if (!presetQueryId && activePresetId) {
              try {
                await agentApi.updateChatSession(activeSession.session_id, {
                  preset_query_id: activePresetId
                })
              } catch (error) {
                console.error(`[DUPLICATE_DEBUG] Failed to update chat session:`, error)
              }
            }
          } catch (error) {
            console.error(`[DUPLICATE_DEBUG] Error reconnecting session ${activeSession.session_id}:`, error)
          }
        }
      } catch (error) {
        console.error('[DUPLICATE_DEBUG] Error during reconnection:', error)
      }
    }

    // Run reconnection check after a short delay to ensure stores are initialized
    const timeoutId = setTimeout(reconnectWorkflowTabs, 500)
    return () => clearTimeout(timeoutId)
  }, [selectedModeCategory, activePresetId, setShowChatArea])

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
      console.log('[WorkflowLayout] Skipping auto-minimize during workflow restore')
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
        console.log(`[WorkflowLayout] Preset changed from ${previousPresetIdRef.current} to ${activePresetId}, auto-minimizing ${tabsToMinimize.length} workflows`)

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
              runFolder: selectedRunFolder,
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
    // Ensure we're in workflow mode before starting phase (only if we have an active preset)
    if (activePresetId) {
      const currentMode = useModeStore.getState().selectedModeCategory
      if (currentMode !== 'workflow') {
        useModeStore.getState().setModeCategory('workflow')
      }
    }
    
    // Validate phaseId is actually a string, not a Promise
    if (typeof phaseId !== 'string') {
      console.error('[WorkflowLayout] ❌ Invalid phaseId: expected string, got', typeof phaseId, phaseId)
      return
    }
    // Check if it's a Promise object (has .then method) - runtime check for safety
    const phaseIdValue = phaseId as unknown
    if (phaseIdValue && typeof phaseIdValue === 'object' && 'then' in phaseIdValue && typeof (phaseIdValue as { then: unknown }).then === 'function') {
      console.error('[WorkflowLayout] ❌ Invalid phaseId: received Promise instead of string', phaseId)
      return
    }
    
    if (!activePresetId) {
      return
    }
    
    // Get phase name
    const phase = getPhaseById(phaseId)
    const phaseName = phase?.title || phaseId
    
    // DEBUG: Log all existing tabs before checking
    const allTabsBefore = Object.values(useChatStore.getState().chatTabs)
      .filter(tab => tab.metadata?.mode === 'workflow')
      .map(tab => ({
        tabId: tab.tabId,
        sessionId: tab.sessionId,
        phaseId: tab.metadata?.phaseId,
        presetQueryId: tab.metadata?.presetQueryId,
        name: tab.name
      }))
    console.log(`[DUPLICATE_DEBUG] handleStartPhase called for phaseId=${phaseId}, presetQueryId=${activePresetId}`)
    console.log(`[DUPLICATE_DEBUG] Existing workflow tabs BEFORE check:`, allTabsBefore)
    
    // Check if a tab for this phase already exists
    const getTabsByPhaseId = useChatStore.getState().getTabsByPhaseId
    const getTabStreamingStatus = useChatStore.getState().getTabStreamingStatus
    const switchTab = useChatStore.getState().switchTab
    const existingPhaseTabs = getTabsByPhaseId(phaseId)
    
    console.log(`[DUPLICATE_DEBUG] Tabs with phaseId=${phaseId}:`, existingPhaseTabs.map(t => ({
      tabId: t.tabId,
      sessionId: t.sessionId,
      presetQueryId: t.metadata?.presetQueryId,
      name: t.name
    })))
    
    // Find existing tab for this phase (prefer running, but reuse any existing tab)
    const runningTab = existingPhaseTabs.find(tab => getTabStreamingStatus(tab.tabId))
    const existingTab = runningTab || (existingPhaseTabs.length > 0 ? existingPhaseTabs.sort((a, b) => b.createdAt - a.createdAt)[0] : null)
    
    let tabId: string
    let tab: ChatTab | undefined
    let isReusingTab = false
    
    if (existingTab) {
      console.log(`[DUPLICATE_DEBUG] ✅ Reusing existing tab ${existingTab.tabId} for phaseId=${phaseId}, NOT creating duplicate`)
      tabId = existingTab.tabId
      switchTab(tabId)
      tab = getActiveTab()
      isReusingTab = true
    } else {
      // Check if there's a tab with same phaseId and presetQueryId but different sessionId
      const allTabs = Object.values(useChatStore.getState().chatTabs)
      const matchingTab = allTabs.find(tab => 
        tab.metadata?.mode === 'workflow' &&
        tab.metadata?.phaseId === phaseId &&
        (tab.metadata?.presetQueryId === activePresetId || (!tab.metadata?.presetQueryId && activePresetId))
      )
      
      if (matchingTab) {
        console.log(`[DUPLICATE_DEBUG] ✅ Found tab with matching phaseId/presetQueryId: ${matchingTab.tabId}, reusing it`)
        tabId = matchingTab.tabId
        switchTab(tabId)
        tab = getActiveTab()
        isReusingTab = true
      } else {
        try {
          console.log(`[DUPLICATE_DEBUG] 🆕 Creating NEW tab for phaseId=${phaseId}, presetQueryId=${activePresetId}`)
          tabId = await createChatTab(phaseName, {
            mode: 'workflow',
            phaseId,
            phaseName,
            presetQueryId: activePresetId || undefined
          })
          
          // DEBUG: Log all tabs after creation
          const allTabsAfter = Object.values(useChatStore.getState().chatTabs)
            .filter(tab => tab.metadata?.mode === 'workflow')
            .map(tab => ({
              tabId: tab.tabId,
              sessionId: tab.sessionId,
              phaseId: tab.metadata?.phaseId,
              presetQueryId: tab.metadata?.presetQueryId,
              name: tab.name
            }))
          console.log(`[DUPLICATE_DEBUG] Existing workflow tabs AFTER creation:`, allTabsAfter)
          
          tab = getActiveTab()
        } catch (error) {
          console.error('[DUPLICATE_DEBUG] Failed to create workflow tab:', error)
          return
        }
      }
    }
    
    if (!tab) {
      console.error('[WorkflowLayout] Failed to get active tab')
      return
    }
    
    // If reusing an existing tab that's already running, don't submit a new query
    if (isReusingTab && getTabStreamingStatus(tab.tabId)) {
      console.log('[WorkflowLayout] Tab is already running, not submitting new query. Just switched to view it.')
      setShowChatArea(true) // Ensure ChatArea is visible
      return
    }
    
    // Update workflow status in database (non-blocking for parallel execution)
    // Note: This is informational only - each tab has its own session and can run independently
    // The backend reads the phase from the query/context, not just from the database
    agentApi.updateWorkflow(activePresetId, phaseId, null, undefined).catch(error => {
      console.error('[WorkflowLayout] Failed to update workflow status (non-blocking):', error)
      // Continue anyway - parallel execution should work regardless
    })
    
    // Don't set global activePhase - allow multiple phases to run in parallel
    // setActivePhase(phaseId) // Removed to allow parallel execution
    setCurrentWorkflowPhase(phaseId) // This is per-tab, so it's fine
    
    // Build query for the phase
    const query = `Execute workflow phase: ${phaseId}`
    
    // Store pending query to submit after ChatArea mounts
    pendingQueryRef.current = { query, executionOptions }
    
    // Show ChatArea when starting a phase (this will trigger ChatArea to mount)
    setShowChatArea(true)
    
    // Note: Query will be submitted via useEffect when ChatArea ref becomes available
  }, [activePresetId, setCurrentWorkflowPhase, setShowChatArea, createChatTab, getPhaseById, getActiveTab])

  // Handle create plan - starts the planning phase (ID: "planning")
  const handleCreatePlan = useCallback(() => {
    // Ensure we're in workflow mode before creating plan (only if we have an active preset)
    if (activePresetId) {
      const currentMode = useModeStore.getState().selectedModeCategory
      if (currentMode !== 'workflow') {
        useModeStore.getState().setModeCategory('workflow')
      }
    }

    const phases = useWorkflowStore.getState().phases
    // Look for the "planning" phase explicitly, fallback to second phase (index 1) if not found
    const planningPhase = phases.find(p => p.id === 'planning') || (phases.length > 1 ? phases[1] : phases[0])
    const planningPhaseId = planningPhase?.id || 'planning'
    console.log('[WorkflowLayout] Create plan requested, starting planning phase:', planningPhaseId)

    // Show ChatArea immediately (synchronously) before starting the phase
    setShowChatArea(true)
    // Note: Don't set activePhase here - allow parallel execution

    // Then start the phase (which will also set showChatArea, but this ensures it's visible immediately)
    handleStartPhase(planningPhaseId)
  }, [handleStartPhase, setShowChatArea, activePresetId])

  // Handle restoring a workflow from running list
  const handleRestoreWorkflow = useCallback(async (workflow: RunningWorkflow) => {
    console.log('[WorkflowLayout] Restoring workflow from running list:', workflow)

    // Ensure we're in workflow mode
    const currentMode = useModeStore.getState().selectedModeCategory
    if (currentMode !== 'workflow') {
      useModeStore.getState().setModeCategory('workflow')
    }

    // If this workflow is for a different preset, switch to it
    if (workflow.presetId !== activePresetId) {
      console.log('[WorkflowLayout] Switching to preset:', workflow.presetId)
      useGlobalPresetStore.getState().setActivePreset('workflow', workflow.presetId)
    }

    // Load run folders to find the latest iteration
    const { loadRunFolders, setSelectedGroupIds } = useWorkflowStore.getState()

    if (workflow.workspacePath) {
      try {
        // Load run folders for this workspace
        await loadRunFolders(workflow.workspacePath)

        // Get the updated run folders from state
        const folders = useWorkflowStore.getState().runFolders

        if (folders.length > 0) {
          // Folders are already sorted by iteration number descending (newest first)
          const latestFolder = folders[0]
          console.log('[WorkflowLayout] Latest iteration:', latestFolder.name, 'Stored iteration:', workflow.runFolder)

          // Always use the latest iteration
          setSelectedRunFolder(latestFolder.name)
        } else if (workflow.runFolder && workflow.runFolder !== 'new') {
          // No folders found, use the stored run folder
          setSelectedRunFolder(workflow.runFolder)
        }
      } catch (error) {
        console.error('[WorkflowLayout] Failed to load run folders during restore:', error)
        // Fallback to stored run folder
        if (workflow.runFolder && workflow.runFolder !== 'new') {
          setSelectedRunFolder(workflow.runFolder)
        }
      }
    } else if (workflow.runFolder && workflow.runFolder !== 'new') {
      // No workspace path, just use stored run folder
      setSelectedRunFolder(workflow.runFolder)
    }

    // Note: Workspace folder expansion is handled automatically by useEffect when selectedRunFolder changes

    // Restore selected group IDs if they exist
    if (workflow.selectedGroupIds && workflow.selectedGroupIds.length > 0) {
      console.log('[WorkflowLayout] Restoring selected groups:', workflow.selectedGroupIds)
      setSelectedGroupIds(workflow.selectedGroupIds)
    }

    // Create a new tab connected to the restored session
    const { createChatTab, switchTab } = useChatStore.getState()
    const { getPhaseById } = useWorkflowStore.getState()

    // Get phase info
    const phase = getPhaseById(workflow.phaseId)
    const phaseName = phase?.title || workflow.phaseName

    // Check if a tab with this sessionId already exists
    const existingTabs = Object.values(useChatStore.getState().chatTabs)
    const existingTab = existingTabs.find(tab =>
      tab.sessionId === workflow.sessionId &&
      tab.metadata?.mode === 'workflow'
    )

    if (existingTab) {
      // Tab already exists, just switch to it
      console.log('[WorkflowLayout] Found existing tab for session, switching to it:', existingTab.tabId)
      switchTab(existingTab.tabId)
    } else {
      // Create a new tab connected to the session
      console.log('[WorkflowLayout] Creating new tab for restored session:', workflow.sessionId)
      const tabId = await createChatTab(phaseName, {
        mode: 'workflow',
        phaseId: workflow.phaseId,
        phaseName,
        presetQueryId: workflow.presetId
      }, workflow.sessionId)  // Pass sessionId to connect to existing session

      switchTab(tabId)
    }

    // Show the chat area so user can see the logs
    setShowChatArea(true)

    console.log('[WorkflowLayout] Workflow restored, sessionId:', workflow.sessionId)
  }, [activePresetId, setSelectedRunFolder, setShowChatArea])

  // No preset selected state
  if (!activeWorkflowPreset) {
    return (
      <div className={`flex flex-col h-full ${className}`}>
        {/* Header */}
        <ChatHeader
          chatSessionTitle=""
          chatSessionId=""
          sessionState="active"
        />
        
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
        <div className={`flex-1 min-w-0 transition-all duration-300 ${showChatArea ? 'w-1/2' : ''}`}>
          <WorkflowCanvas
            ref={canvasRef}
            workspacePath={workspacePath}
            presetQueryId={activePresetId}
            currentPhase={activePhase || currentWorkflowPhase}
            onStartPhase={handleStartPhase}
            onCreatePlan={onCreatePlan || handleCreatePlan}
            showChatArea={showChatArea}
            onToggleChatArea={() => setShowChatArea(!showChatArea)}
            className="h-full"
          />
        </div>

        {/* ChatArea Panel - appears on right side, positioned below toolbar */}
        <div className={`${showChatArea ? 'w-1/2' : 'w-0 overflow-hidden'} border-l border-gray-200 dark:border-gray-700 flex flex-col min-h-0 bg-white dark:bg-gray-900 absolute right-0 top-0 bottom-0 transition-all duration-300`} style={{ top: '40px' }}>
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

        {/* Running Workflows Drawer - overlays when open */}
        <RunningWorkflowsDrawer
          onRestoreWorkflow={handleRestoreWorkflow}
        />

        {/* Running Workflows Indicator - positioned within workflow area */}
        <RunningWorkflowsIndicator />
      </div>
    </div>
  )
}

export default WorkflowLayout
