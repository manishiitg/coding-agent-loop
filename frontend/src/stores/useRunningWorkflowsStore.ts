import { create } from 'zustand'
import { devtools } from 'zustand/middleware'
import type { StepProgress, GetEventsResponse } from '../services/api-types'
import { agentApi } from '../services/api'
import { useChatStore } from './useChatStore'
import { getTypedEventData } from '../generated/event-types'
import {
  POLLING_INTERVALS,
  WORKFLOW_LIMITS,
  EVENT_CONFIG,
  VALIDATION_CONFIG,
  STORAGE_KEYS
} from '../constants/runningWorkflows'
import {
  hasWorkflowCompletion,
  hasWorkflowError,
  calculateBackoffDelay
} from '../utils/workflowEventProcessor'

// Running workflow interface for tracking active workflows
export interface RunningWorkflow {
  id: string                    // Unique ID for this running workflow
  presetId: string              // For context restoration
  presetName: string            // Display name
  workspacePath: string         // Workspace context
  sessionId: string             // Backend session ID (for reconnection)
  runFolder: string             // Run folder context
  phaseId: string               // Which phase (planning, execution, etc.)
  phaseName: string             // Display name
  status: 'running' | 'completed' | 'failed' | 'paused'
  progress?: StepProgress       // Current step progress
  currentStepTitle?: string     // Title of the currently executing step
  selectedGroupIds?: string[]   // Selected group IDs (for batch execution)
  minimizedAt: number           // Timestamp when minimized
  lastUpdated: number           // Last status check
  lastProcessedEventIndex: number // Last event index processed (for incremental updates)
  failedPollCount: number       // Number of consecutive failed polls
  lastPollError?: string        // Last poll error message
}

// Helper functions for persistence
const loadRunningWorkflowsFromStorage = (): RunningWorkflow[] => {
  try {
    const saved = localStorage.getItem(STORAGE_KEYS.RUNNING_WORKFLOWS)
    if (saved) {
      return JSON.parse(saved) as RunningWorkflow[]
    }
  } catch (error) {
    console.error('[RunningWorkflowsStore] Failed to load from localStorage:', error)
  }
  return []
}

const saveRunningWorkflowsToStorage = (workflows: RunningWorkflow[]) => {
  try {
    localStorage.setItem(STORAGE_KEYS.RUNNING_WORKFLOWS, JSON.stringify(workflows))
  } catch (error) {
    console.error('[RunningWorkflowsStore] Failed to save to localStorage:', error)
  }
}

interface RunningWorkflowsStore {
  // State
  runningWorkflows: RunningWorkflow[]
  showRunningDrawer: boolean
  runningPollingInterval: NodeJS.Timeout | null
  isRestoringWorkflow: boolean
  lastValidationTime: number | null
  drawerIsOpen: boolean  // Track drawer state for adaptive polling

  // Actions
  minimizeWorkflow: (params: {
    presetId: string
    presetName: string
    workspacePath: string
    sessionId: string
    runFolder: string
    phaseId: string
    phaseName: string
    progress?: StepProgress
    selectedGroupIds?: string[]
  }) => void
  restoreWorkflow: (runningWorkflowId: string) => RunningWorkflow | undefined
  removeRunningWorkflow: (id: string) => void
  updateRunningWorkflowStatus: (id: string, updates: Partial<RunningWorkflow>) => void
  setShowRunningDrawer: (show: boolean) => void
  setIsRestoringWorkflow: (isRestoring: boolean) => void
  getRunningWorkflowCount: () => { running: number; total: number }
  refreshRunningWorkflowStatuses: () => void

  // Polling
  startRunningPolling: (drawerOpen?: boolean) => void
  stopRunningPolling: () => void
  pollRunningWorkflows: () => Promise<void>

  // Validation
  validateRunningWorkflows: (force?: boolean) => Promise<void>

  // Cleanup
  cleanupCompletedWorkflows: () => void
}

export const useRunningWorkflowsStore = create<RunningWorkflowsStore>()(
  devtools(
    (set, get) => ({
      // Initial State
      runningWorkflows: loadRunningWorkflowsFromStorage(),
      showRunningDrawer: false,
      runningPollingInterval: null,
      isRestoringWorkflow: false,
      lastValidationTime: null,
      drawerIsOpen: false,

      // Minimize workflow and add to tracked list
      minimizeWorkflow: (params) => {
        const state = get()
        const minimizedAt = Date.now()

        // Check if this workflow is already tracked (by sessionId)
        const existingIndex = state.runningWorkflows.findIndex(
          bg => bg.sessionId === params.sessionId
        )

        if (existingIndex >= 0) {
          // Update existing entry instead of creating duplicate
          const updated = [...state.runningWorkflows]
          updated[existingIndex] = {
            ...updated[existingIndex],
            ...params,
            status: 'running',
            selectedGroupIds: params.selectedGroupIds,
            lastUpdated: Date.now(),
            failedPollCount: 0,  // Reset failure count
            lastProcessedEventIndex: updated[existingIndex].lastProcessedEventIndex || 0
          }
          set({ runningWorkflows: updated })
          saveRunningWorkflowsToStorage(updated)
          console.log(`[RunningWorkflowsStore] Updated existing running workflow: ${params.sessionId}`)
        } else {
          // Create new running workflow entry
          const runningWorkflow: RunningWorkflow = {
            id: crypto.randomUUID(),
            presetId: params.presetId,
            presetName: params.presetName,
            workspacePath: params.workspacePath,
            sessionId: params.sessionId,
            runFolder: params.runFolder,
            phaseId: params.phaseId,
            phaseName: params.phaseName,
            status: 'running',
            progress: params.progress,
            selectedGroupIds: params.selectedGroupIds,
            minimizedAt,
            lastUpdated: Date.now(),
            lastProcessedEventIndex: 0,
            failedPollCount: 0
          }

          // Limit to max tracked workflows (remove oldest if exceeded)
          let newRunningWorkflows = [...state.runningWorkflows, runningWorkflow]
          if (newRunningWorkflows.length > WORKFLOW_LIMITS.MAX_TRACKED_WORKFLOWS) {
            // Sort by minimizedAt and keep the most recent
            newRunningWorkflows = newRunningWorkflows
              .sort((a, b) => b.minimizedAt - a.minimizedAt)
              .slice(0, WORKFLOW_LIMITS.MAX_TRACKED_WORKFLOWS)
          }

          set({ runningWorkflows: newRunningWorkflows })
          saveRunningWorkflowsToStorage(newRunningWorkflows)
          console.log(`[RunningWorkflowsStore] Minimized workflow: ${params.presetName} - ${params.phaseName}`)
        }

        // Start polling (adaptive based on drawer state)
        get().startRunningPolling(state.drawerIsOpen)

        // Update session metadata
        agentApi.updateChatSession(params.sessionId, {
          config: {
            workflow_metadata: {
              preset_id: params.presetId,
              preset_name: params.presetName,
              workspace_path: params.workspacePath,
              run_folder: params.runFolder,
              phase_id: params.phaseId,
              phase_name: params.phaseName,
              is_minimized: true,
              minimized_at: minimizedAt,
              step_progress: params.progress,
              current_step_title: (params.progress as any)?.last_completed_step_title
            }
          }
        } as any).catch(error => {
          console.warn('[RunningWorkflowsStore] Failed to update session metadata:', error)
        })
      },

      // Restore workflow from tracked list
      restoreWorkflow: (runningWorkflowId: string) => {
        const state = get()
        const runningWorkflow = state.runningWorkflows.find(bg => bg.id === runningWorkflowId)

        if (!runningWorkflow) {
          console.warn(`[RunningWorkflowsStore] Running workflow ${runningWorkflowId} not found`)
          return undefined
        }

        // Remove from running workflows list
        const newRunningWorkflows = state.runningWorkflows.filter(bg => bg.id !== runningWorkflowId)
        set({ runningWorkflows: newRunningWorkflows })
        saveRunningWorkflowsToStorage(newRunningWorkflows)

        console.log(`[RunningWorkflowsStore] Restored workflow: ${runningWorkflow.presetName} - ${runningWorkflow.phaseName}`)

        // Clear is_minimized flag in session metadata
        agentApi.updateChatSession(runningWorkflow.sessionId, {
          config: {
            workflow_metadata: {
              preset_id: runningWorkflow.presetId,
              preset_name: runningWorkflow.presetName,
              workspace_path: runningWorkflow.workspacePath,
              run_folder: runningWorkflow.runFolder,
              phase_id: runningWorkflow.phaseId,
              phase_name: runningWorkflow.phaseName,
              is_minimized: false,
              step_progress: runningWorkflow.progress,
              current_step_title: runningWorkflow.currentStepTitle
            }
          }
        } as any).catch(error => {
          console.warn('[RunningWorkflowsStore] Failed to clear session metadata:', error)
        })

        return runningWorkflow
      },

      // Remove workflow from tracked list
      removeRunningWorkflow: (id: string) => {
        const state = get()
        const workflow = state.runningWorkflows.find(w => w.id === id)

        // Cleanup events for this session
        if (workflow?.sessionId) {
          useChatStore.getState().cleanupTabEvents?.(
            workflow.sessionId,
            EVENT_CONFIG.MAX_EVENTS_PER_COMPLETED_SESSION
          )
        }

        const newRunningWorkflows = state.runningWorkflows.filter(bg => bg.id !== id)
        set({ runningWorkflows: newRunningWorkflows })
        saveRunningWorkflowsToStorage(newRunningWorkflows)

        console.log(`[RunningWorkflowsStore] Removed running workflow: ${id}`)
      },

      // Update workflow status
      updateRunningWorkflowStatus: (id: string, updates: Partial<RunningWorkflow>) => {
        const state = get()
        const index = state.runningWorkflows.findIndex(bg => bg.id === id)

        if (index < 0) return

        const updated = [...state.runningWorkflows]
        updated[index] = {
          ...updated[index],
          ...updates,
          lastUpdated: Date.now()
        }

        set({ runningWorkflows: updated })
        saveRunningWorkflowsToStorage(updated)
      },

      // Show/hide drawer
      setShowRunningDrawer: (show: boolean) => {
        set({ showRunningDrawer: show, drawerIsOpen: show })

        // Restart polling with new rate when drawer state changes
        const state = get()
        if (state.runningPollingInterval) {
          get().startRunningPolling(show)
        }
      },

      setIsRestoringWorkflow: (isRestoring: boolean) => {
        set({ isRestoringWorkflow: isRestoring })
      },

      // Get count of running workflows
      getRunningWorkflowCount: () => {
        const state = get()
        const running = state.runningWorkflows.filter(bg => bg.status === 'running').length
        return { running, total: state.runningWorkflows.length }
      },

      // Refresh statuses from stored events
      refreshRunningWorkflowStatuses: () => {
        const state = get()
        const chatStore = useChatStore.getState()

        const updated = state.runningWorkflows.map(bg => {
          const events = chatStore.tabEvents[bg.sessionId] || []

          if (events.length > 0) {
            const hasCompletion = hasWorkflowCompletion(events)
            const hasError = hasWorkflowError(events)

            if (hasError) {
              return { ...bg, status: 'failed' as const, lastUpdated: Date.now() }
            }
            if (hasCompletion) {
              return { ...bg, status: 'completed' as const, lastUpdated: Date.now() }
            }
          }

          return bg
        })

        const hasChanges = updated.some((bg, i) => bg.status !== state.runningWorkflows[i].status)
        if (hasChanges) {
          set({ runningWorkflows: updated })
          saveRunningWorkflowsToStorage(updated)
        }
      },

      // Start adaptive polling
      startRunningPolling: (drawerOpen = false) => {
        const state = get()

        // Clear existing interval
        if (state.runningPollingInterval) {
          clearInterval(state.runningPollingInterval)
        }

        const runningCount = state.runningWorkflows.filter(w => w.status === 'running').length

        // Choose polling interval based on context
        let interval: number = POLLING_INTERVALS.BACKGROUND
        if (drawerOpen) {
          interval = POLLING_INTERVALS.ACTIVE
          console.log('[RunningWorkflowsStore] Starting fast polling (drawer open)')
        } else if (runningCount === 0) {
          interval = POLLING_INTERVALS.IDLE
          console.log('[RunningWorkflowsStore] Starting slow polling (no running workflows)')
        } else {
          console.log('[RunningWorkflowsStore] Starting background polling')
        }

        const pollingInterval = setInterval(() => {
          get().pollRunningWorkflows().catch(error => {
            console.error('[RunningWorkflowsStore] Error polling running workflows:', error)
          })
        }, interval)

        set({ runningPollingInterval: pollingInterval })
      },

      // Stop polling
      stopRunningPolling: () => {
        const state = get()
        if (state.runningPollingInterval) {
          console.log('[RunningWorkflowsStore] Stopping polling')
          clearInterval(state.runningPollingInterval)
          set({ runningPollingInterval: null })
        }
      },

      // Poll events for all running workflows (with optimizations)
      pollRunningWorkflows: async () => {
        const state = get()
        // Poll 'running', 'completed', and 'failed' workflows - they might become active again after restart
        const workflowsToPoll = state.runningWorkflows.filter(bg => 
          bg.status === 'running' || bg.status === 'completed' || bg.status === 'failed'
        )

        if (workflowsToPoll.length === 0) {
          // Stop polling when no workflows to poll
          if (state.runningPollingInterval) {
            get().stopRunningPolling()
          }
          return
        }

        const chatStore = useChatStore.getState()

        for (const bg of workflowsToPoll) {
          // Skip if too many consecutive failures
          if (bg.failedPollCount >= VALIDATION_CONFIG.MAX_POLL_RETRIES) {
            console.warn(`[RunningWorkflowsStore] Skipping poll for ${bg.id} (too many failures)`)
            continue
          }

          try {
            // Get last processed event index (use 0 if not set)
            const lastIndex = Math.max(0, bg.lastProcessedEventIndex || 0)

            // Poll for new events using 'tiny' mode (minimal events)
            const response = await agentApi.getSessionEvents(bg.sessionId, lastIndex, {
              eventMode: 'tiny'
            })

            // Check session status from response
            // For workflows, we prioritize workflow_end events over session_status
            // because session_status might be 'completed' when a single agent finishes,
            // not when the whole workflow finishes
            let shouldMarkCompleted = false
            let shouldMarkPaused = false
            let shouldMarkFailed = false
            let hasRunningEvents = false  // Track if we see events indicating workflow is still running

            if (response.session_status === 'active' || response.session_status === 'running') {
              if (bg.status === 'completed' || bg.status === 'failed') {
                // Workflow was marked completed/failed but is now active again - resume tracking
                // This can happen when a workflow is restarted after failure
                get().updateRunningWorkflowStatus(bg.id, { 
                  status: 'running',
                  failedPollCount: 0,  // Reset failure count on restart
                  lastPollError: undefined
                })
                console.log(`[RunningWorkflowsStore] Workflow resumed (was ${bg.status}): ${bg.presetName}`)
              }
            } else if (response.session_status === 'stopped') {
              shouldMarkPaused = true
            } else if (response.session_status === 'error') {
              shouldMarkFailed = true
            }
            // Note: We don't mark as completed based on session_status alone for workflows
            // We'll check events first to see if there's a workflow_end event

            if (response.events && response.events.length > 0) {
              // Add events to store (for later restore)
              chatStore.addTabEvents(bg.sessionId, response.events)

              // Update last event index
              // last_event_index may exist in response but isn't in the type definition
              const responseWithIndex = response as GetEventsResponse & { last_event_index?: number }
              const newLastIndex = responseWithIndex.last_event_index ?? (lastIndex + response.events.length)
              chatStore.setTabLastEventIndex(bg.sessionId, newLastIndex)

              // Process events for progress updates
              for (const event of response.events) {
                // Check for events that indicate workflow is still running
                // These events mean the workflow is active, even if it was previously marked as completed
                const runningEventTypes = [
                  'step_progress_updated',
                  'agent_start',
                  'tool_call_start',
                  'tool_call_end',
                  'llm_generation_end',
                  'orchestrator_agent_start'
                ]
                if (event.type && runningEventTypes.includes(event.type)) {
                  hasRunningEvents = true
                }

                // Handle step_progress_updated
                if (event.type === 'step_progress_updated') {
                  const eventData = getTypedEventData(event, 'step_progress_updated')

                  if (eventData) {
                    // Note: Progress details (completed_step_indices, total_steps) are no longer in the event
                    // The frontend should load progress from the API if needed
                    const updates: Partial<RunningWorkflow> = {}
                    if (eventData.current_step_id) {
                      // Update current step ID if available
                      // Note: We can't update progress here since it's not in the event anymore
                    }

                    get().updateRunningWorkflowStatus(bg.id, updates)
                  }
                }

                // Check for completion/error events (primary check for workflows)
                // workflow_end event is the true indicator of workflow completion
                if (event.type && hasWorkflowCompletion([event])) {
                  shouldMarkCompleted = true
                  console.log(`[RunningWorkflowsStore] Workflow completion event detected: ${event.type} for ${bg.presetName}`)
                } else if (event.type && hasWorkflowError([event])) {
                  shouldMarkFailed = true
                  console.log(`[RunningWorkflowsStore] Workflow error event detected: ${event.type} for ${bg.presetName}`)
                }
              }

              // Update last processed event index
              get().updateRunningWorkflowStatus(bg.id, {
                lastProcessedEventIndex: newLastIndex
              })
            }

            // Apply status updates after processing events
            // This ensures we see workflow_end events before marking as completed
            
            // If we see running events (step_execution_start, step_progress_updated, etc.) 
            // and workflow was marked as completed, reset it back to running
            // This handles cases where workflow was incorrectly marked as completed
            if (hasRunningEvents && bg.status === 'completed') {
              get().updateRunningWorkflowStatus(bg.id, { 
                status: 'running',
                failedPollCount: 0,
                lastPollError: undefined
              })
              console.log(`[RunningWorkflowsStore] Workflow resumed (saw running events, was completed): ${bg.presetName}`)
              // Continue processing - don't return, let it keep polling
            } else if (shouldMarkFailed) {
              get().updateRunningWorkflowStatus(bg.id, { status: 'failed' })
              console.log(`[RunningWorkflowsStore] Workflow failed: ${bg.presetName}`)
              continue
            } else if (shouldMarkCompleted) {
              // Only mark as completed if we saw a workflow_end event
              get().updateRunningWorkflowStatus(bg.id, { status: 'completed' })
              chatStore.cleanupTabEvents?.(bg.sessionId, EVENT_CONFIG.MAX_EVENTS_PER_COMPLETED_SESSION)
              console.log(`[RunningWorkflowsStore] Workflow completed (workflow_end event): ${bg.presetName}`)
              continue
            } else if (shouldMarkPaused) {
              get().updateRunningWorkflowStatus(bg.id, { status: 'paused' })
              console.log(`[RunningWorkflowsStore] Workflow stopped: ${bg.presetName}`)
              continue
            } else if (response.session_status === 'completed' && bg.status !== 'completed') {
              // If session_status is completed but we didn't see workflow_end event,
              // it might be a false positive (single agent completion). 
              // Don't mark as completed yet - keep polling to see if more events arrive
              console.log(`[RunningWorkflowsStore] Session status is 'completed' but no workflow_end event yet for ${bg.presetName} - continuing to poll`)
            }

            // Reset failure count on success
            if (bg.failedPollCount > 0) {
              get().updateRunningWorkflowStatus(bg.id, {
                failedPollCount: 0,
                lastPollError: undefined
              })
            }

          } catch (error) {
            console.error(`[RunningWorkflowsStore] Error polling workflow ${bg.id}:`, error)

            const newFailureCount = bg.failedPollCount + 1
            get().updateRunningWorkflowStatus(bg.id, {
              failedPollCount: newFailureCount,
              lastPollError: error instanceof Error ? error.message : 'Unknown error'
            })

            // Show toast on max failures
            if (newFailureCount >= VALIDATION_CONFIG.MAX_POLL_RETRIES) {
              chatStore.addToast?.(
                `Failed to update workflow "${bg.presetName}" - check connection`,
                'error'
              )
            }

            // Calculate backoff delay for next attempt
            const backoffDelay = calculateBackoffDelay(
              newFailureCount,
              VALIDATION_CONFIG.RETRY_BASE_DELAY,
              VALIDATION_CONFIG.RETRY_MAX_DELAY
            )
            console.log(`[RunningWorkflowsStore] Will retry after ${backoffDelay}ms (attempt ${newFailureCount})`)
          }
        }

        // Auto-cleanup old completed workflows
        get().cleanupCompletedWorkflows()
      },

      // Validate running workflows against backend (with caching)
      validateRunningWorkflows: async (force = false) => {
        const state = get()
        const now = Date.now()

        // Skip if recently validated (unless forced)
        if (!force && state.lastValidationTime) {
          const age = now - state.lastValidationTime
          if (age < VALIDATION_CONFIG.VALIDATION_CACHE_TTL) {
            console.log('[RunningWorkflowsStore] Skipping validation (recently validated)')
            return
          }
        }

        if (state.runningWorkflows.length === 0) return

        console.log(`[RunningWorkflowsStore] Validating ${state.runningWorkflows.length} workflows...`)

        const validWorkflows: RunningWorkflow[] = []
        const removedIds: string[] = []

        for (const bg of state.runningWorkflows) {
          try {
            const sessionStatus = await agentApi.getSessionStatus(bg.sessionId)

            if (sessionStatus) {
              let updatedStatus = bg.status
              if (sessionStatus.status === 'error') {
                updatedStatus = 'failed'
              } else if (sessionStatus.status === 'completed') {
                updatedStatus = 'completed'
              } else if (sessionStatus.status === 'stopped') {
                updatedStatus = 'paused'
              } else if (sessionStatus.status === 'active' || sessionStatus.status === 'running') {
                updatedStatus = 'running'
              }

              validWorkflows.push({
                ...bg,
                status: updatedStatus,
                lastUpdated: now
              })
            } else {
              removedIds.push(bg.id)
            }
          } catch (error) {
            console.warn(`[RunningWorkflowsStore] Session ${bg.sessionId} not found, removing`)
            removedIds.push(bg.id)
          }
        }

        if (removedIds.length > 0) {
          console.log(`[RunningWorkflowsStore] Removed ${removedIds.length} stale workflows`)
        }

        set({ runningWorkflows: validWorkflows, lastValidationTime: now })
        saveRunningWorkflowsToStorage(validWorkflows)
      },

      // Cleanup old completed workflows
      cleanupCompletedWorkflows: () => {
        const state = get()
        const now = Date.now()

        const filtered = state.runningWorkflows.filter(wf => {
          // Keep running workflows
          if (wf.status === 'running') return true

          // Remove old completed/failed workflows
          const age = now - wf.lastUpdated
          if (age > WORKFLOW_LIMITS.COMPLETED_WORKFLOW_TTL) {
            console.log(`[RunningWorkflowsStore] Auto-removing old ${wf.status} workflow: ${wf.presetName}`)
            // Cleanup events
            if (wf.sessionId) {
              useChatStore.getState().cleanupTabEvents?.(
                wf.sessionId,
                EVENT_CONFIG.MAX_EVENTS_PER_COMPLETED_SESSION
              )
            }
            return false
          }

          return true
        })

        if (filtered.length !== state.runningWorkflows.length) {
          set({ runningWorkflows: filtered })
          saveRunningWorkflowsToStorage(filtered)
        }
      },
    }),
    {
      name: 'running-workflows-store'
    }
  )
)

// Selector hooks
export const useRunningWorkflows = () => useRunningWorkflowsStore(state => state.runningWorkflows)
export const useShowRunningDrawer = () => useRunningWorkflowsStore(state => state.showRunningDrawer)
export const useRunningWorkflowsRunningCount = () => useRunningWorkflowsStore(
  state => state.runningWorkflows.filter(bg => bg.status === 'running').length
)
export const useRunningWorkflowsTotalCount = () => useRunningWorkflowsStore(
  state => state.runningWorkflows.length
)
