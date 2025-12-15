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

// Global localStorage key for temporary LLM overrides (persists across page refreshes)
const TEMP_OVERRIDE_LLM_KEY = 'workflow_temp_override_llm'
const TEMP_OVERRIDE_LLM2_KEY = 'workflow_temp_override_llm2'
const TEMP_OVERRIDE_LLM_ENABLED_KEY = 'workflow_temp_override_llm_enabled'
const FALLBACK_TO_ORIGINAL_LLM_KEY = 'workflow_fallback_to_original_llm_on_failure'
const SKIP_LEARNING_WHEN_TEMP_LLM1_KEY = 'workflow_skip_learning_when_temp_llm1'
const SKIP_LEARNING_WHEN_TEMP_LLM2_KEY = 'workflow_skip_learning_when_temp_llm2'
const SAVE_VALIDATION_RESPONSES_KEY = 'workflow_save_validation_responses'

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
      selectedBranchStep: {  // For resuming from branch steps
        parentStepIndex: number;  // 0-based index of conditional step
        branchType: 'if_true' | 'if_false';  // Which branch
        branchStepIndex: number;  // 0-based index within the branch
      } | null
  
      // Temporary LLM overrides (persists across page refreshes via localStorage)
      // Cascading fallback: tempLLM1 → tempLLM2 → step LLM (on validation failures)
      tempOverrideLLM: AgentLLMConfig | null  // First override LLM (used on first attempt)
      tempOverrideLLM2: AgentLLMConfig | null  // Second override LLM (used on second attempt if tempLLM1 fails)
      tempOverrideLLMEnabled: boolean  // Whether temp LLM overrides are enabled (configs are preserved when disabled)
      fallbackToOriginalLLMOnFailure: boolean  // If true, use original LLM instead of temp override when validation fails
      skipLearningWhenTempLLM1: boolean  // If true, skip learning phases when tempLLM1 is used
      skipLearningWhenTempLLM2: boolean  // If true, skip learning phases when tempLLM2 is used
      saveValidationResponses: boolean  // If true, save validation responses to workspace validation folder

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
  setBranchStep: (branchStep: { parentStepIndex: number; branchType: 'if_true' | 'if_false'; branchStepIndex: number } | null) => void
  buildExecutionOptions: () => ExecutionOptions
  
  // Temporary LLM overrides
  setTempOverrideLLM: (config: AgentLLMConfig | null) => void
  clearTempOverrideLLM: () => void
  setTempOverrideLLM2: (config: AgentLLMConfig | null) => void
  clearTempOverrideLLM2: () => void
  setTempOverrideLLMEnabled: (enabled: boolean) => void
  setFallbackToOriginalLLMOnFailure: (enabled: boolean) => void
  setSkipLearningWhenTempLLM1: (enabled: boolean) => void
  setSkipLearningWhenTempLLM2: (enabled: boolean) => void
  setSaveValidationResponses: (enabled: boolean) => void

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
      
      // Temporary LLM overrides (persists across page refreshes via localStorage)
      // Load from localStorage on initialization
      tempOverrideLLM: (() => {
        try {
          const saved = localStorage.getItem(TEMP_OVERRIDE_LLM_KEY)
          if (saved) {
            const parsed = JSON.parse(saved) as AgentLLMConfig
            if (parsed.provider && parsed.model_id) {
              console.log(`[WorkflowStore] Loaded temp override LLM1 from localStorage: ${parsed.provider}/${parsed.model_id}`)
              return parsed
            }
          }
        } catch (error) {
          console.error('[WorkflowStore] Failed to load temp override LLM1 from localStorage:', error)
        }
        return null
      })(),
      tempOverrideLLM2: (() => {
        try {
          const saved = localStorage.getItem(TEMP_OVERRIDE_LLM2_KEY)
          if (saved) {
            const parsed = JSON.parse(saved) as AgentLLMConfig
            if (parsed.provider && parsed.model_id) {
              console.log(`[WorkflowStore] Loaded temp override LLM2 from localStorage: ${parsed.provider}/${parsed.model_id}`)
              return parsed
            }
          }
        } catch (error) {
          console.error('[WorkflowStore] Failed to load temp override LLM2 from localStorage:', error)
        }
        return null
      })(),
      // Temp LLM override enabled state (persists across page refreshes via localStorage)
      // Defaults to true if any temp LLM is configured, false otherwise
      tempOverrideLLMEnabled: (() => {
        try {
          const saved = localStorage.getItem(TEMP_OVERRIDE_LLM_ENABLED_KEY)
          if (saved !== null) {
            const parsed = JSON.parse(saved) as boolean
            console.log(`[WorkflowStore] Loaded temp override LLM enabled state from localStorage: ${parsed}`)
            return parsed
          }
          // Default to true if we have any temp LLM configured
          const hasLLM1 = localStorage.getItem(TEMP_OVERRIDE_LLM_KEY)
          const hasLLM2 = localStorage.getItem(TEMP_OVERRIDE_LLM2_KEY)
          return !!(hasLLM1 || hasLLM2)
        } catch (error) {
          console.error('[WorkflowStore] Failed to load temp override LLM enabled state from localStorage:', error)
          return false
        }
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
      // Skip learning when tempLLM1 is active (persists across page refreshes via localStorage)
      // Load from localStorage on initialization
      skipLearningWhenTempLLM1: (() => {
        try {
          const saved = localStorage.getItem(SKIP_LEARNING_WHEN_TEMP_LLM1_KEY)
          if (saved !== null) {
            const parsed = JSON.parse(saved) as boolean
            console.log(`[WorkflowStore] Loaded skip learning when tempLLM1 from localStorage: ${parsed}`)
            return parsed
          }
        } catch (error) {
          console.error('[WorkflowStore] Failed to load skip learning when tempLLM1 from localStorage:', error)
        }
        return false  // Default to false
      })(),
      // Skip learning when tempLLM2 is active (persists across page refreshes via localStorage)
      // Load from localStorage on initialization
      skipLearningWhenTempLLM2: (() => {
        try {
          const saved = localStorage.getItem(SKIP_LEARNING_WHEN_TEMP_LLM2_KEY)
          if (saved !== null) {
            const parsed = JSON.parse(saved) as boolean
            console.log(`[WorkflowStore] Loaded skip learning when tempLLM2 from localStorage: ${parsed}`)
            return parsed
          }
        } catch (error) {
          console.error('[WorkflowStore] Failed to load skip learning when tempLLM2 from localStorage:', error)
        }
        return false  // Default to false
      })(),
      // Save validation responses to workspace (persists across page refreshes via localStorage)
      // Load from localStorage on initialization
      saveValidationResponses: (() => {
        try {
          const saved = localStorage.getItem(SAVE_VALIDATION_RESPONSES_KEY)
          if (saved !== null) {
            const parsed = JSON.parse(saved) as boolean
            console.log(`[WorkflowStore] Loaded save validation responses from localStorage: ${parsed}`)
            return parsed
          }
        } catch (error) {
          console.error('[WorkflowStore] Failed to load save validation responses from localStorage:', error)
        }
        return true  // Default to true (save validation responses by default)
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
        return phases.length > 0 ? phases[0].id : 'planning'
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
            // No progress file exists - only reset progress, preserve user's selectedStartPoint
            // The user's selection should only be reset when they explicitly choose "Start from Beginning"
            const currentStartPoint = get().selectedStartPoint
            set({ stepProgress: null, isLoadingProgress: false })
            console.log('[RESUME_DEBUG] No progress file found, preserving selectedStartPoint:', currentStartPoint)
          }
        } catch (error) {
          // On error, only reset progress, preserve user's selectedStartPoint
          // The user's selection should only be reset when they explicitly choose "Start from Beginning"
          const currentStartPoint = get().selectedStartPoint
          set({ stepProgress: null, isLoadingProgress: false })
          console.error('[RESUME_DEBUG] Failed to load progress, preserving selectedStartPoint:', currentStartPoint, error)
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
        console.log('[RESUME_DEBUG] setStartPoint called:', step)
        set({ selectedStartPoint: step, selectedBranchStep: null }) // Clear branch step when setting regular step
      },
      setBranchStep: (branchStep: { parentStepIndex: number; branchType: 'if_true' | 'if_false'; branchStepIndex: number } | null) => {
        set({ selectedBranchStep: branchStep, selectedStartPoint: 0 }) // Clear regular step when setting branch step
      },

      // Build execution options from current state
      buildExecutionOptions: () => {
        const state = get()
        const isResuming = state.selectedStartPoint > 0
        const isResumingBranch = state.selectedBranchStep !== null

        console.log('[RESUME_DEBUG] buildExecutionOptions called:', {
          selectedStartPoint: state.selectedStartPoint,
          selectedBranchStep: state.selectedBranchStep,
          selectedExecutionMode: state.selectedExecutionMode,
          isResuming,
          isResumingBranch
        })

        // Convert UI selections to backend ExecutionStrategy
        let executionStrategy: string
        if (state.selectedExecutionMode === 'fast_execution') {
          executionStrategy = (isResuming || isResumingBranch)
            ? ExecutionStrategy.FAST_RESUME_FROM_STEP
            : ExecutionStrategy.FAST_EXECUTE_ALL
        } else if (state.selectedExecutionMode === 'with_learning') {
          executionStrategy = (isResuming || isResumingBranch)
            ? ExecutionStrategy.RESUME_FROM_STEP_NO_HUMAN
            : ExecutionStrategy.START_FROM_BEGINNING_NO_HUMAN
        } else {
          // Human approval (default)
          executionStrategy = (isResuming || isResumingBranch)
            ? ExecutionStrategy.RESUME_FROM_STEP
            : ExecutionStrategy.START_FROM_BEGINNING
        }

        console.log('[RESUME_DEBUG] Selected execution strategy:', executionStrategy)

        const options: ExecutionOptions = {
          run_mode: state.selectedRunFolder === 'new' ? 'create_new_runs_always' : 'use_same_run',
          selected_run_folder: state.selectedRunFolder === 'new' ? undefined : state.selectedRunFolder,
          execution_strategy: executionStrategy,
        }

        // Include resume_from_step for regular step resuming
        // CRITICAL: Only set resume_from_step if we have a valid step number (> 0)
        // This prevents sending resume_from_step=0 which causes backend to delete all completed steps
        console.log('[RESUME_DEBUG] Checking resume_from_step conditions:', {
          isResuming,
          isResumingBranch,
          selectedStartPoint: state.selectedStartPoint,
          executionStrategy,
          isResumeStrategy: executionStrategy === ExecutionStrategy.RESUME_FROM_STEP ||
                           executionStrategy === ExecutionStrategy.RESUME_FROM_STEP_NO_HUMAN ||
                           executionStrategy === ExecutionStrategy.FAST_RESUME_FROM_STEP ||
                           executionStrategy === ExecutionStrategy.RUN_SINGLE_STEP
        })
        
        if (isResuming && !isResumingBranch &&
            state.selectedStartPoint > 0 &&
            (executionStrategy === ExecutionStrategy.RESUME_FROM_STEP ||
             executionStrategy === ExecutionStrategy.RESUME_FROM_STEP_NO_HUMAN ||
             executionStrategy === ExecutionStrategy.FAST_RESUME_FROM_STEP ||
             executionStrategy === ExecutionStrategy.RUN_SINGLE_STEP)) {
          options.resume_from_step = state.selectedStartPoint
          console.log('[RESUME_DEBUG] ✅ Setting resume_from_step:', state.selectedStartPoint)
        } else if (state.selectedStartPoint > 0 && !isResumingBranch) {
          // Log warning if selectedStartPoint > 0 but strategy is not a resume strategy
          console.warn('[RESUME_DEBUG] ⚠️ selectedStartPoint is', state.selectedStartPoint, 
            'but strategy is', executionStrategy, '- not including resume_from_step')
          console.warn('[RESUME_DEBUG] ⚠️ This means resume_from_step will NOT be set in options!')
        } else if ((executionStrategy === ExecutionStrategy.RESUME_FROM_STEP ||
                    executionStrategy === ExecutionStrategy.RESUME_FROM_STEP_NO_HUMAN ||
                    executionStrategy === ExecutionStrategy.FAST_RESUME_FROM_STEP) &&
                   !isResumingBranch && state.selectedStartPoint === 0) {
          // CRITICAL: If resume strategy is selected but no valid step, log error and fallback to start from beginning
          console.error('[RESUME_DEBUG] 🚨 CRITICAL: Resume strategy selected but selectedStartPoint is 0! Falling back to start from beginning.')
          console.error('[RESUME_DEBUG] 🚨 This would cause backend to delete all completed steps. Strategy:', executionStrategy)
          // Override strategy to start from beginning to prevent data loss
          if (state.selectedExecutionMode === 'fast_execution') {
            options.execution_strategy = ExecutionStrategy.FAST_EXECUTE_ALL
          } else if (state.selectedExecutionMode === 'with_learning') {
            options.execution_strategy = ExecutionStrategy.START_FROM_BEGINNING_NO_HUMAN
          } else {
            options.execution_strategy = ExecutionStrategy.START_FROM_BEGINNING
          }
          console.log('[RESUME_DEBUG] ✅ Overridden strategy to:', options.execution_strategy)
        } else {
          console.log('[RESUME_DEBUG] ℹ️ Not setting resume_from_step:', {
            isResuming,
            isResumingBranch,
            selectedStartPoint: state.selectedStartPoint,
            executionStrategy,
            reason: !isResuming ? 'not resuming' : 
                    isResumingBranch ? 'resuming branch' :
                    state.selectedStartPoint === 0 ? 'startPoint is 0' :
                    'strategy mismatch'
          })
        }

        // Include resume_from_branch_step for branch step resuming
        if (isResumingBranch && state.selectedBranchStep &&
            (executionStrategy === ExecutionStrategy.RESUME_FROM_STEP ||
             executionStrategy === ExecutionStrategy.RESUME_FROM_STEP_NO_HUMAN ||
             executionStrategy === ExecutionStrategy.FAST_RESUME_FROM_STEP)) {
          options.resume_from_branch_step = {
            parent_step_index: state.selectedBranchStep.parentStepIndex,
            branch_type: state.selectedBranchStep.branchType,
            branch_step_index: state.selectedBranchStep.branchStepIndex
          }
        }
        
        // Include temporary LLM overrides if enabled and set (cascading fallback: tempLLM1 → tempLLM2 → step LLM)
        // When disabled, explicitly set to null to ensure backend clears them
        if (state.tempOverrideLLMEnabled) {
          if (state.tempOverrideLLM) {
            options.temp_override_llm = state.tempOverrideLLM
          }
          if (state.tempOverrideLLM2) {
            options.temp_override_llm2 = state.tempOverrideLLM2
          }
        } else {
          // Explicitly set to undefined when disabled to ensure backend clears any existing overrides
          options.temp_override_llm = undefined
          options.temp_override_llm2 = undefined
        }
        
        // Include fallback to original LLM on failure if enabled
        if (state.fallbackToOriginalLLMOnFailure) {
          options.fallback_to_original_llm_on_failure = true
        }

        // Include skip learning when tempLLM flags if set
        if (state.skipLearningWhenTempLLM1) {
          options.skip_learning_when_temp_llm1 = true
        }
        if (state.skipLearningWhenTempLLM2) {
          options.skip_learning_when_temp_llm2 = true
        }
        
        // Include save validation responses flag (always send to ensure backend knows user preference)
        options.save_validation_responses = state.saveValidationResponses

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
              } else {
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

        console.log('[RESUME_DEBUG] ✅ Final execution options:', JSON.stringify({
          execution_strategy: options.execution_strategy,
          resume_from_step: options.resume_from_step,
          resume_from_branch_step: options.resume_from_branch_step,
          run_mode: options.run_mode,
          selected_run_folder: options.selected_run_folder
        }, null, 2))

        return options
      },
      
      // Temporary LLM override actions
      setTempOverrideLLM: (config: AgentLLMConfig | null) => {
        set({ tempOverrideLLM: config })
        try {
          if (config) {
            // Save to localStorage
            localStorage.setItem(TEMP_OVERRIDE_LLM_KEY, JSON.stringify(config))
            console.log(`[WorkflowStore] Temporary LLM override 1 set: ${config.provider}/${config.model_id}`)
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
          console.log('[WorkflowStore] Temporary LLM override 1 cleared')
        } catch (error) {
          console.error('[WorkflowStore] Failed to clear temp override 1 from localStorage:', error)
        }
      },
      
      setTempOverrideLLM2: (config: AgentLLMConfig | null) => {
        set({ tempOverrideLLM2: config })
        try {
          if (config) {
            // Save to localStorage
            localStorage.setItem(TEMP_OVERRIDE_LLM2_KEY, JSON.stringify(config))
            console.log(`[WorkflowStore] Temporary LLM override 2 set: ${config.provider}/${config.model_id}`)
          } else {
            // Clear from localStorage
            localStorage.removeItem(TEMP_OVERRIDE_LLM2_KEY)
            console.log('[WorkflowStore] Temporary LLM override 2 cleared')
          }
        } catch (error) {
          console.error('[WorkflowStore] Failed to save temp override 2 to localStorage:', error)
        }
      },
      
      clearTempOverrideLLM2: () => {
        set({ tempOverrideLLM2: null })
        try {
          localStorage.removeItem(TEMP_OVERRIDE_LLM2_KEY)
          console.log('[WorkflowStore] Temporary LLM override 2 cleared')
        } catch (error) {
          console.error('[WorkflowStore] Failed to clear temp override 2 from localStorage:', error)
        }
      },
      
      setTempOverrideLLMEnabled: (enabled: boolean) => {
        set({ tempOverrideLLMEnabled: enabled })
        try {
          localStorage.setItem(TEMP_OVERRIDE_LLM_ENABLED_KEY, JSON.stringify(enabled))
          console.log(`[WorkflowStore] Temp override LLM enabled state set: ${enabled}`)
        } catch (error) {
          console.error('[WorkflowStore] Failed to save temp override LLM enabled state to localStorage:', error)
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
      
      setSkipLearningWhenTempLLM1: (enabled: boolean) => {
        set({ skipLearningWhenTempLLM1: enabled })
        try {
          // Save to localStorage
          localStorage.setItem(SKIP_LEARNING_WHEN_TEMP_LLM1_KEY, JSON.stringify(enabled))
          console.log(`[WorkflowStore] Skip learning when tempLLM1 set: ${enabled}`)
        } catch (error) {
          console.error('[WorkflowStore] Failed to save skip learning when tempLLM1 to localStorage:', error)
        }
      },
      
      setSkipLearningWhenTempLLM2: (enabled: boolean) => {
        set({ skipLearningWhenTempLLM2: enabled })
        try {
          // Save to localStorage
          localStorage.setItem(SKIP_LEARNING_WHEN_TEMP_LLM2_KEY, JSON.stringify(enabled))
          console.log(`[WorkflowStore] Skip learning when tempLLM2 set: ${enabled}`)
        } catch (error) {
          console.error('[WorkflowStore] Failed to save skip learning when tempLLM2 to localStorage:', error)
        }
      },
      
      setSaveValidationResponses: (enabled: boolean) => {
        set({ saveValidationResponses: enabled })
        try {
          // Save to localStorage
          localStorage.setItem(SAVE_VALIDATION_RESPONSES_KEY, JSON.stringify(enabled))
          console.log(`[WorkflowStore] Save validation responses set: ${enabled}`)
        } catch (error) {
          console.error('[WorkflowStore] Failed to save save validation responses to localStorage:', error)
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
          selectedBranchStep: null,
          variablesManifest: null,
          currentRunningGroupId: null,
          activePhase: null,
          showChatArea: false
        })
      },

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

// Computed selector for selected group ID
export const useSelectedGroupId = () => {
  return useWorkflowStore(state => {
    // Extract group ID from selectedRunFolder if it contains a group path
    // Pattern: iteration-X/group-Y
    if (state.selectedRunFolder && state.selectedRunFolder !== 'new' && state.selectedRunFolder.includes('/group-')) {
      const parts = state.selectedRunFolder.split('/')
      if (parts.length === 2 && parts[1].startsWith('group-')) {
        return parts[1] // e.g., "group-5"
      }
    }
    return null
  })
}

