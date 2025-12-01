import { create } from 'zustand'
import { devtools } from 'zustand/middleware'
import type { WorkflowPhase, StepProgress, ExecutionOptions } from '../services/api-types'
import { ExecutionStrategy } from '../services/api-types'
import { agentApi } from '../services/api'

// Execution mode options
export type ExecutionModeType = 'human_approval' | 'fast_execution' | 'with_learning'

// LocalStorage key prefix for persisting workflow settings
const STORAGE_KEY_PREFIX = 'workflow_settings_'
const getStorageKey = (presetId: string, setting: 'iteration' | 'execution_mode' | 'start_point') =>
  `${STORAGE_KEY_PREFIX}${presetId}_${setting}`

export interface RunFolder {
  name: string
  progress?: StepProgress
}

interface WorkflowStore {
  // === CONSTANTS (loaded once from API) ===
  phases: WorkflowPhase[]
  isLoadingPhases: boolean
  phasesError: string | null
  phasesInitialized: boolean
  // Promise to prevent duplicate API calls during concurrent access
  _loadPhasesPromise: Promise<void> | null

  // === EXECUTION STATE (per workflow session) ===
  // Run folder management
  runFolders: RunFolder[]
  selectedRunFolder: string // 'new' or folder name
  isLoadingRunFolders: boolean

  // Step progress
  stepProgress: StepProgress | null
  isLoadingProgress: boolean

  // Execution options
  selectedExecutionMode: ExecutionModeType
  selectedStartPoint: number // 0 = beginning, >0 = step number (1-based)

  // UI state
  activePhase: string | null // Currently running phase
  showChatArea: boolean

  // === ACTIONS ===
  // Constants
  loadPhases: () => Promise<void>
  getPhaseById: (id: string) => WorkflowPhase | undefined
  getDefaultPhase: () => string
  getStepSpecificPhases: () => WorkflowPhase[]

  // Run folders
  loadRunFolders: (workspacePath: string) => Promise<void>
  setSelectedRunFolder: (folder: string) => void

  // Progress
  loadProgress: (workspacePath: string, runFolder: string) => Promise<void>
  getCompletedStepIndices: () => number[]

  // Execution options
  setExecutionMode: (mode: ExecutionModeType) => void
  setStartPoint: (step: number) => void
  buildExecutionOptions: () => ExecutionOptions

  // UI
  setActivePhase: (phase: string | null) => void
  setShowChatArea: (show: boolean) => void

  // Persistence (localStorage)
  loadSavedSettings: (presetId: string) => void
  saveSettings: (presetId: string) => void

  // Reset
  resetExecutionState: () => void
}

export const useWorkflowStore = create<WorkflowStore>()(
  devtools(
    (set, get) => ({
      // === Initial State ===
      // Constants
      phases: [],
      isLoadingPhases: false,
      phasesError: null,
      phasesInitialized: false,
      _loadPhasesPromise: null,

      // Run folders
      runFolders: [],
      selectedRunFolder: 'new',
      isLoadingRunFolders: false,

      // Progress
      stepProgress: null,
      isLoadingProgress: false,

      // Execution options
      selectedExecutionMode: 'human_approval',
      selectedStartPoint: 0,

      // UI state
      activePhase: null,
      showChatArea: false,

      // === Actions ===

      // Load phases from backend (with deduplication)
      loadPhases: async () => {
        const state = get()

        // If already initialized, return immediately
        if (state.phasesInitialized) {
          return
        }

        // If a load is already in progress, wait for it
        if (state._loadPhasesPromise) {
          return state._loadPhasesPromise
        }

        // Create and store the promise to prevent duplicate calls
        const loadPromise = (async () => {
          set({ isLoadingPhases: true, phasesError: null })

          try {
            const response = await agentApi.getWorkflowConstants()
            if (response.success) {
              const phases = response.constants?.phases || []
              set({
                phases: phases,
                isLoadingPhases: false,
                phasesInitialized: true,
                _loadPhasesPromise: null
              })
            } else {
              throw new Error(response.message || 'Failed to load workflow constants')
            }
          } catch (error) {
            console.error('[WorkflowStore] Failed to load phases:', error)
            set({
              phasesError: error instanceof Error ? error.message : 'Failed to load phases',
              isLoadingPhases: false,
              _loadPhasesPromise: null
            })
          }
        })()

        set({ _loadPhasesPromise: loadPromise })
        return loadPromise
      },

      getPhaseById: (id: string) => {
        return get().phases.find(p => p.id === id)
      },

      getDefaultPhase: () => {
        const phases = get().phases
        return phases.length > 0 ? phases[0].id : 'variable-extraction'
      },

      // Get phases that can work on individual steps
      getStepSpecificPhases: () => {
        return get().phases.filter(p =>
          p.id === 'plan-tool-optimization' ||
          p.id === 'plan-improvement' ||
          p.id === 'plan-learnings-alignment'
        )
      },

      // Load run folders for a workspace
      loadRunFolders: async (workspacePath: string) => {
        if (!workspacePath) {
          set({ runFolders: [] })
          return
        }

        set({ isLoadingRunFolders: true })

        try {
          const response = await agentApi.getRunFolders(workspacePath)

          if (!response) {
            set({ runFolders: [], isLoadingRunFolders: false })
            return
          }

          let folders = response.folders
          if (folders === null || folders === undefined) {
            folders = []
          }

          if (!Array.isArray(folders)) {
            console.warn('[WorkflowStore] response.folders is not an array. Type:', typeof folders)
            set({ runFolders: [], isLoadingRunFolders: false })
            return
          }

          // Sort folders by iteration number (descending - newest first)
          const sorted = [...folders].sort((a, b) => {
            const numA = parseInt(a.name.replace('iteration-', '')) || 0
            const numB = parseInt(b.name.replace('iteration-', '')) || 0
            return numB - numA
          })

          set({ runFolders: sorted, isLoadingRunFolders: false })

          // Validate current selection
          const currentSelection = get().selectedRunFolder
          if (currentSelection !== 'new' && !sorted.some(f => f.name === currentSelection)) {
            // Saved folder no longer exists, default to newest or 'new'
            const newSelection = sorted.length > 0 ? sorted[0].name : 'new'
            set({ selectedRunFolder: newSelection })

            // Load progress for new selection if it's not 'new'
            if (newSelection !== 'new') {
              get().loadProgress(workspacePath, newSelection)
            }
          }
        } catch (error) {
          console.error('[WorkflowStore] Failed to load run folders:', error)
          set({ runFolders: [], isLoadingRunFolders: false })
        }
      },

      setSelectedRunFolder: (folder: string) => {
        set({ selectedRunFolder: folder })
        // Clear progress when switching to 'new'
        if (folder === 'new') {
          set({ stepProgress: null })
        }
      },

      // Load step progress for a run folder
      loadProgress: async (workspacePath: string, runFolder: string) => {
        if (!workspacePath || runFolder === 'new') {
          set({ stepProgress: null })
          return
        }

        set({ isLoadingProgress: true })

        try {
          const response = await agentApi.getProgress(workspacePath, runFolder)
          if (response.exists && response.progress) {
            set({ stepProgress: response.progress, isLoadingProgress: false })

            // Update the folder info in state so we can show progress in the dropdown
            set(state => ({
              runFolders: state.runFolders.map(f =>
                f.name === runFolder
                  ? { ...f, progress: response.progress || undefined }
                  : f
              )
            }))
          } else {
            set({ stepProgress: null, isLoadingProgress: false })
          }
        } catch (error) {
          console.error('[WorkflowStore] Failed to load progress:', error)
          set({ stepProgress: null, isLoadingProgress: false })
        }
      },

      getCompletedStepIndices: () => {
        return get().stepProgress?.completed_step_indices || []
      },

      setExecutionMode: (mode: ExecutionModeType) => {
        set({ selectedExecutionMode: mode })
      },

      setStartPoint: (step: number) => {
        set({ selectedStartPoint: step })
      },

      // Build execution options from current state
      buildExecutionOptions: () => {
        const state = get()
        const isResuming = state.selectedStartPoint > 0

        // Convert UI selections to backend ExecutionStrategy
        let executionStrategy: string
        if (state.selectedExecutionMode === 'fast_execution') {
          executionStrategy = isResuming
            ? ExecutionStrategy.FAST_RESUME_FROM_STEP
            : ExecutionStrategy.FAST_EXECUTE_ALL
        } else if (state.selectedExecutionMode === 'with_learning') {
          executionStrategy = isResuming
            ? ExecutionStrategy.RESUME_FROM_STEP_NO_HUMAN
            : ExecutionStrategy.START_FROM_BEGINNING_NO_HUMAN
        } else {
          // Human approval (default)
          executionStrategy = isResuming
            ? ExecutionStrategy.RESUME_FROM_STEP
            : ExecutionStrategy.START_FROM_BEGINNING
        }

        const options: ExecutionOptions = {
          run_mode: state.selectedRunFolder === 'new' ? 'create_new_runs_always' : 'use_same_run',
          selected_run_folder: state.selectedRunFolder === 'new' ? undefined : state.selectedRunFolder,
          execution_strategy: executionStrategy,
        }

        if (isResuming) {
          options.resume_from_step = state.selectedStartPoint
        }

        return options
      },

      setActivePhase: (phase: string | null) => {
        set({ activePhase: phase })
      },

      setShowChatArea: (show: boolean) => {
        set({ showChatArea: show })
      },

      // Load saved settings from localStorage for a preset
      loadSavedSettings: (presetId: string) => {
        if (!presetId) return

        try {
          // Load saved iteration folder
          const savedIteration = localStorage.getItem(getStorageKey(presetId, 'iteration'))
          if (savedIteration) {
            set({ selectedRunFolder: savedIteration })
          }

          // Load saved execution mode
          const savedMode = localStorage.getItem(getStorageKey(presetId, 'execution_mode'))
          if (savedMode && ['human_approval', 'fast_execution', 'with_learning'].includes(savedMode)) {
            set({ selectedExecutionMode: savedMode as ExecutionModeType })
          }

          // Load saved start point
          const savedStartPoint = localStorage.getItem(getStorageKey(presetId, 'start_point'))
          if (savedStartPoint) {
            const parsed = parseInt(savedStartPoint, 10)
            if (!isNaN(parsed)) {
              set({ selectedStartPoint: parsed })
            }
          }
        } catch (error) {
          console.error('[WorkflowStore] Failed to load settings from localStorage:', error)
        }
      },

      // Save current settings to localStorage for a preset
      saveSettings: (presetId: string) => {
        if (!presetId) return

        const state = get()
        try {
          localStorage.setItem(getStorageKey(presetId, 'iteration'), state.selectedRunFolder)
          localStorage.setItem(getStorageKey(presetId, 'execution_mode'), state.selectedExecutionMode)
          localStorage.setItem(getStorageKey(presetId, 'start_point'), String(state.selectedStartPoint))
        } catch (error) {
          console.error('[WorkflowStore] Failed to save settings to localStorage:', error)
        }
      },

      // Reset execution state (called when switching workflows)
      resetExecutionState: () => {
        set({
          runFolders: [],
          selectedRunFolder: 'new',
          stepProgress: null,
          selectedExecutionMode: 'human_approval',
          selectedStartPoint: 0,
          activePhase: null,
          showChatArea: false
        })
      }
    }),
    {
      name: 'workflow-store'
    }
  )
)

// Selector hooks for common patterns
export const useWorkflowPhases = () => useWorkflowStore(state => state.phases)
export const useWorkflowPhasesLoading = () => useWorkflowStore(state => state.isLoadingPhases)
export const useWorkflowRunFolders = () => useWorkflowStore(state => state.runFolders)
export const useWorkflowProgress = () => useWorkflowStore(state => state.stepProgress)
export const useCompletedStepIndices = () => useWorkflowStore(state => state.stepProgress?.completed_step_indices || [])

