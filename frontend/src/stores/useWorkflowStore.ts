import { create } from 'zustand'
import { devtools } from 'zustand/middleware'
import type { WorkflowPhase, StepProgress, ExecutionOptions, AgentLLMConfig, VariablesManifest } from '../services/api-types'
import { ExecutionStrategy } from '../services/api-types'
import { agentApi } from '../services/api'

// Execution mode options
export type ExecutionModeType = 'human_approval' | 'fast_execution' | 'with_learning'

// LocalStorage key prefix for persisting workflow settings
const STORAGE_KEY_PREFIX = 'workflow_settings_'
const getStorageKey = (presetId: string, setting: 'iteration' | 'execution_mode' | 'start_point') =>
  `${STORAGE_KEY_PREFIX}${presetId}_${setting}`

// Global localStorage key for temporary LLM override (persists across page refreshes)
const TEMP_OVERRIDE_LLM_KEY = 'workflow_temp_override_llm'
const FALLBACK_TO_ORIGINAL_LLM_KEY = 'workflow_fallback_to_original_llm_on_failure'

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
  
      // Temporary LLM override (persists across page refreshes via localStorage)
      tempOverrideLLM: AgentLLMConfig | null
      fallbackToOriginalLLMOnFailure: boolean  // If true, use original LLM instead of temp override when validation fails

  // Variables manifest (for batch execution with multiple groups)
  variablesManifest: VariablesManifest | null

  // Current running group (for batch execution)
  currentRunningGroupId: string | null

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
  loadFolderProgressOnDemand: (workspacePath: string, folderName: string) => Promise<void>
  getCompletedStepIndices: () => number[]

  // Execution options
  setExecutionMode: (mode: ExecutionModeType) => void
  setStartPoint: (step: number) => void
  buildExecutionOptions: () => ExecutionOptions
  
  // Temporary LLM override
  setTempOverrideLLM: (config: AgentLLMConfig | null) => void
  clearTempOverrideLLM: () => void
  setFallbackToOriginalLLMOnFailure: (enabled: boolean) => void

  // Variables manifest
  setVariablesManifest: (manifest: VariablesManifest | null) => void

  // Current running group
  setCurrentRunningGroupId: (groupId: string | null) => void

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
      
      // Temporary LLM override (persists across page refreshes via localStorage)
      // Load from localStorage on initialization
      tempOverrideLLM: (() => {
        try {
          const saved = localStorage.getItem(TEMP_OVERRIDE_LLM_KEY)
          if (saved) {
            const parsed = JSON.parse(saved) as AgentLLMConfig
            if (parsed.provider && parsed.model_id) {
              console.log(`[WorkflowStore] Loaded temp override from localStorage: ${parsed.provider}/${parsed.model_id}`)
              return parsed
            }
          }
        } catch (error) {
          console.error('[WorkflowStore] Failed to load temp override from localStorage:', error)
        }
        return null
      })(),
      // Fallback to original LLM on failure (persists across page refreshes via localStorage)
      // Load from localStorage on initialization
      fallbackToOriginalLLMOnFailure: (() => {
        try {
          const saved = localStorage.getItem(FALLBACK_TO_ORIGINAL_LLM_KEY)
          if (saved !== null) {
            const parsed = JSON.parse(saved) as boolean
            console.log(`[WorkflowStore] Loaded fallback to original LLM on failure from localStorage: ${parsed}`)
            return parsed
          }
        } catch (error) {
          console.error('[WorkflowStore] Failed to load fallback to original LLM on failure from localStorage:', error)
        }
        return false  // Default to false
      })(),

      // Variables manifest
      variablesManifest: null,

      // Current running group
      currentRunningGroupId: null,

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
            // Check if the selection looks like a valid iteration folder (e.g., "iteration-3")
            // This handles the case where a folder was just created but hasn't appeared in the list yet
            const isValidIterationPattern = /^iteration-\d+/.test(currentSelection)
            
            if (isValidIterationPattern) {
              // Preserve the selection even if not in list yet (it was likely just created)
              // The folder should appear in the next refresh
              console.log(`[WorkflowStore] Preserving selection "${currentSelection}" - folder may not be in list yet`)
            } else {
              // Saved folder no longer exists and doesn't match iteration pattern, default to newest or 'new'
              const newSelection = sorted.length > 0 ? sorted[0].name : 'new'
              set({ selectedRunFolder: newSelection })

              // Load progress for new selection if it's not 'new'
              if (newSelection !== 'new') {
                get().loadProgress(workspacePath, newSelection)
              }
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
            // No progress file exists - reset start point to 0 (start from beginning)
            // This ensures that even if localStorage has a saved resume point, we reset it
            // when there's no actual progress to resume from
            set({ stepProgress: null, isLoadingProgress: false, selectedStartPoint: 0 })
            console.log('[WorkflowStore] No progress file found, resetting selectedStartPoint to 0')
          }
        } catch (error) {
          console.error('[WorkflowStore] Failed to load progress:', error)
          // On error, also reset start point to 0 since we can't verify progress exists
          set({ stepProgress: null, isLoadingProgress: false, selectedStartPoint: 0 })
        }
      },

      // Load progress on-demand for a folder (for dropdown display)
      // Only loads if folder doesn't have progress data yet
      loadFolderProgressOnDemand: async (workspacePath: string, folderName: string) => {
        if (!workspacePath || folderName === 'new') return

        // Check if folder already has progress
        const folder = get().runFolders.find(f => f.name === folderName)
        if (folder?.progress) return // Already has progress, no need to load

        try {
          const response = await agentApi.getProgress(workspacePath, folderName)
          if (response.exists && response.progress) {
            // Update folder info with loaded progress
            set(state => ({
              runFolders: state.runFolders.map(f =>
                f.name === folderName
                  ? { ...f, progress: response.progress || undefined }
                  : f
              )
            }))
          }
        } catch (error) {
          // Silent fail - this is on-demand loading for display purposes
          console.debug('[WorkflowStore] On-demand progress load failed for', folderName, error)
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

        // Only include resume_from_step if we're actually resuming
        // Double-check: if strategy is start_from_beginning, don't include resume_from_step
        if (isResuming && 
            (executionStrategy === ExecutionStrategy.RESUME_FROM_STEP ||
             executionStrategy === ExecutionStrategy.RESUME_FROM_STEP_NO_HUMAN ||
             executionStrategy === ExecutionStrategy.FAST_RESUME_FROM_STEP ||
             executionStrategy === ExecutionStrategy.RUN_SINGLE_STEP)) {
          options.resume_from_step = state.selectedStartPoint
        } else if (state.selectedStartPoint > 0) {
          // Log warning if selectedStartPoint > 0 but strategy is not a resume strategy
          console.warn('[WorkflowStore] selectedStartPoint is', state.selectedStartPoint, 
            'but strategy is', executionStrategy, '- not including resume_from_step')
        }
        
        // Include temporary LLM override if set
        if (state.tempOverrideLLM) {
          options.temp_override_llm = state.tempOverrideLLM
        }
        
        // Include fallback to original LLM on failure if enabled
        if (state.fallbackToOriginalLLMOnFailure) {
          options.fallback_to_original_llm_on_failure = true
          console.log('[WorkflowStore] Including fallback_to_original_llm_on_failure=true in execution options')
        } else {
          console.log('[WorkflowStore] fallbackToOriginalLLMOnFailure is false, not including in execution options')
        }

        // Check if selectedRunFolder contains a specific group path
        // Pattern: iteration-X/group-Y
        let selectedGroupId: string | null = null
        if (state.selectedRunFolder && state.selectedRunFolder !== 'new' && state.selectedRunFolder.includes('/group-')) {
          // Extract group ID from path like "iteration-1/group-5"
          const parts = state.selectedRunFolder.split('/')
          if (parts.length === 2 && parts[1].startsWith('group-')) {
            selectedGroupId = parts[1] // e.g., "group-5"
          }
        }

        // Include enabled group IDs from variables manifest (for batch execution)
        // This ensures disabled groups are not executed even if the file is stale
        if (state.variablesManifest) {
          if (state.variablesManifest.groups && state.variablesManifest.groups.length > 0) {
            // If a specific group was selected via folder path, run only that group
            if (selectedGroupId) {
              // Verify the group exists in manifest
              const groupExists = state.variablesManifest.groups.some(g => g.group_id === selectedGroupId)
              if (groupExists) {
                options.enabled_group_ids = [selectedGroupId]
                console.log(`[WorkflowStore] Running only selected group: ${selectedGroupId}`)
              } else {
                console.warn(`[WorkflowStore] Selected group ${selectedGroupId} not found in manifest, using all enabled groups`)
                // Fall back to all enabled groups
                const enabledGroupIDs = state.variablesManifest.groups
                  .filter(g => g.enabled)
                  .map(g => g.group_id)
                if (enabledGroupIDs.length > 0) {
                  options.enabled_group_ids = enabledGroupIDs
                }
              }
            } else {
              // No specific group selected - use all enabled groups
              const enabledGroupIDs = state.variablesManifest.groups
                .filter(g => g.enabled)
                .map(g => g.group_id)
              if (enabledGroupIDs.length > 0) {
                options.enabled_group_ids = enabledGroupIDs
              }
            }
          }
          // Single-group mode: if no groups array, all variables are in one virtual group
          // In this case, we don't need to set enabled_group_ids as the backend handles it
        }

        return options
      },
      
      // Temporary LLM override actions
      setTempOverrideLLM: (config: AgentLLMConfig | null) => {
        set({ tempOverrideLLM: config })
        try {
          if (config) {
            // Save to localStorage
            localStorage.setItem(TEMP_OVERRIDE_LLM_KEY, JSON.stringify(config))
            console.log(`[WorkflowStore] Temporary LLM override set: ${config.provider}/${config.model_id}`)
          } else {
            // Clear from localStorage
            localStorage.removeItem(TEMP_OVERRIDE_LLM_KEY)
            console.log('[WorkflowStore] Temporary LLM override cleared')
          }
        } catch (error) {
          console.error('[WorkflowStore] Failed to save temp override to localStorage:', error)
        }
      },
      
      clearTempOverrideLLM: () => {
        set({ tempOverrideLLM: null })
        try {
          localStorage.removeItem(TEMP_OVERRIDE_LLM_KEY)
          console.log('[WorkflowStore] Temporary LLM override cleared')
        } catch (error) {
          console.error('[WorkflowStore] Failed to clear temp override from localStorage:', error)
        }
      },
      
      setFallbackToOriginalLLMOnFailure: (enabled: boolean) => {
        set({ fallbackToOriginalLLMOnFailure: enabled })
        try {
          // Save to localStorage
          localStorage.setItem(FALLBACK_TO_ORIGINAL_LLM_KEY, JSON.stringify(enabled))
          console.log(`[WorkflowStore] Fallback to original LLM on failure set: ${enabled}`)
        } catch (error) {
          console.error('[WorkflowStore] Failed to save fallback to original LLM on failure to localStorage:', error)
        }
      },

      setVariablesManifest: (manifest: VariablesManifest | null) => {
        set({ variablesManifest: manifest })
      },

      setCurrentRunningGroupId: (groupId: string | null) => {
        set({ currentRunningGroupId: groupId })
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
        // Note: We don't clear tempOverrideLLM here because it's a global setting
        // that should persist across workflow switches
        set({
          runFolders: [],
          selectedRunFolder: 'new',
          stepProgress: null,
          selectedExecutionMode: 'human_approval',
          selectedStartPoint: 0,
          variablesManifest: null,
          currentRunningGroupId: null,
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

