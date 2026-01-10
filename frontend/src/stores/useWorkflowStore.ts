import { create } from 'zustand'
import { devtools } from 'zustand/middleware'
import type { WorkflowPhase, StepProgress, ExecutionOptions, AgentLLMConfig, VariablesManifest } from '../services/api-types'
import { ExecutionStrategy } from '../services/api-types'
import { agentApi } from '../services/api'
import { useChatStore } from './useChatStore'
import { resolveGroupFolderPath } from '../utils/workflowUtils'
import { normalizeGroupIds, validateGroupIds, normalizeStartPoint, normalizeRunFolder } from '../utils/workflowStateNormalization'

// Execution mode options
export type ExecutionModeType = 'human_approval' | 'fast_execution' | 'with_learning'

// LocalStorage key prefix for persisting workflow settings
const STORAGE_KEY_PREFIX = 'workflow_settings_'
const getStorageKey = (presetId: string, setting: 'iteration' | 'execution_mode' | 'start_point' | 'selected_groups' | 'branch_step' | 'active_phase') =>
  `${STORAGE_KEY_PREFIX}${presetId}_${setting}`

// Global localStorage key for temporary LLM overrides (persists across page refreshes)
const TEMP_OVERRIDE_LLM_KEY = 'workflow_temp_override_llm'
const TEMP_OVERRIDE_LLM2_KEY = 'workflow_temp_override_llm2'
const TEMP_OVERRIDE_LLM_ENABLED_KEY = 'workflow_temp_override_llm_enabled'
const FALLBACK_TO_ORIGINAL_LLM_KEY = 'workflow_fallback_to_original_llm_on_failure'
const SKIP_LEARNING_WHEN_TEMP_LLM1_KEY = 'workflow_skip_learning_when_temp_llm1'
const SKIP_LEARNING_WHEN_TEMP_LLM2_KEY = 'workflow_skip_learning_when_temp_llm2'
const SAVE_VALIDATION_RESPONSES_KEY = 'workflow_save_validation_responses'
const DISABLE_SHELL_EXEC_ACCESS_KEY = 'workflow_disable_shell_exec_access'
const DISABLE_READ_IMAGE_ACCESS_KEY = 'workflow_disable_read_image_access'
// NOTE: Running workflows logic has been moved to useRunningWorkflowsStore.ts
// This store now focuses on workflow execution state and configuration

export interface RunFolder {
  name: string
  progress?: StepProgress
}

export interface WorkflowChatTab {
  tabId: string  // Unique ID: `phase_${phaseId}_${timestamp}`
  phaseId: string  // Workflow phase ID (e.g., "planning", "execution")
  phaseName: string  // Display name from WorkflowPhase
  observerId: string  // Unique observer ID for this tab
  sessionId: string | null  // Chat session ID if exists
  isActive: boolean  // Whether this phase is currently running
  isStreaming: boolean  // Whether this tab's execution is currently running
  isCompleted: boolean  // Whether this tab's execution has completed (detected from completion events)
  createdAt: number  // Timestamp for ordering
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
  stepProgressFolder: string | null // Track which folder the current progress belongs to
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
      disableShellExecAccess: boolean  // If true, disable all execute_shell_command tool access globally
      disableReadImageAccess: boolean  // If true, disable all read_image tool access globally

  // Variables manifest (for batch execution with multiple groups)
  variablesManifest: VariablesManifest | null

  // Selected group IDs for execution (multi-select)
  selectedGroupIds: string[] // Array of group IDs to execute

  // Current running group (for batch execution)
  currentRunningGroupId: string | null

  // UI state
  activePhase: string | null // Currently running phase
  showChatArea: boolean

  // Multi-tab chat state
  workflowChatTabs: Record<string, WorkflowChatTab>  // tabId -> tab
  activeWorkflowTabId: string | null  // Currently selected tab

  // === ACTIONS ===
  // Constants
  loadPhases: () => Promise<void>
  setPhases: (phases: WorkflowPhase[]) => void
  getPhaseById: (id: string) => WorkflowPhase | undefined
  getDefaultPhase: () => string
  getStepSpecificPhases: () => WorkflowPhase[]

  // Run folders
  loadRunFolders: (workspacePath: string) => Promise<void>
  setRunFolders: (folders: RunFolder[]) => void
  setSelectedRunFolder: (folder: string) => void

  // Progress
  loadProgress: (workspacePath: string, runFolder: string, forceLoad?: boolean) => Promise<void>
  loadFolderProgressOnDemand: (workspacePath: string, folderName: string) => Promise<void>
  setStepProgress: (progress: StepProgress | null) => void
  getCompletedStepIndices: () => number[]
  updateStepProgressFromEvent: (progress: StepProgress) => void

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
  setSaveValidationResponses: () => void
  setDisableShellExecAccess: (enabled: boolean) => void
  setDisableReadImageAccess: (enabled: boolean) => void

  // Variables manifest
  setVariablesManifest: (manifest: VariablesManifest | null) => void

  // Selected group IDs
  toggleGroupSelection: (groupId: string) => void
  setSelectedGroupIds: (groupIds: string[]) => void
  clearSelectedGroupIds: () => void

  // Current running group
  setCurrentRunningGroupId: (groupId: string | null) => void
  
  // Consolidated batch group switching handler
  // Handles group start/end events and updates state atomically
  handleBatchGroupStart: (groupId: string, runFolder: string, workspacePath?: string) => void
  handleBatchGroupEnd: (groupId: string) => void

  // UI
  setActivePhase: (phase: string | null) => void
  setShowChatArea: (show: boolean) => void

  // Workflow chat tabs
  createWorkflowTab: (phaseId: string, phaseName: string) => Promise<string>  // Returns tabId
  switchWorkflowTab: (tabId: string) => void
  closeWorkflowTab: (tabId: string) => Promise<void>
  getWorkflowTab: (tabId: string) => WorkflowChatTab | undefined
  getActiveWorkflowTab: () => WorkflowChatTab | undefined
  getTabsByPhase: (phaseId: string) => WorkflowChatTab[]
  setTabStreaming: (tabId: string, isStreaming: boolean) => void
  setTabCompleted: (tabId: string, isCompleted: boolean) => void
  updateTabSessionId: (tabId: string, sessionId: string) => void
  // Computed: Get tab's streaming status (derived from polling status)
  getTabStreamingStatus: (tabId: string) => boolean
  // Check if tab has completion events
  checkTabCompletion: (tabId: string, events: Array<{ type: string }>) => boolean

  // Persistence (localStorage)
  loadSavedSettings: (presetId: string) => void
  saveSettings: (presetId: string) => void
  // Switch to a new preset - resets context and loads settings in one update
  switchToPreset: (presetId: string) => void

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
      stepProgressFolder: null, // Track which folder the current progress belongs to
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
            return parsed
          }
        } catch (error) {
          console.error('[WorkflowStore] Failed to load skip learning when tempLLM2 from localStorage:', error)
        }
        return false  // Default to false
      })(),
      // Save validation responses to workspace (always enabled)
      saveValidationResponses: true,
      // Disable shell exec access (persists across page refreshes via localStorage)
      // Load from localStorage on initialization
      disableShellExecAccess: (() => {
        try {
          const saved = localStorage.getItem(DISABLE_SHELL_EXEC_ACCESS_KEY)
          if (saved !== null) {
            const parsed = JSON.parse(saved) as boolean
            return parsed
          }
        } catch (error) {
          console.error('[WorkflowStore] Failed to load disable shell exec access from localStorage:', error)
        }
        return false  // Default to false (shell exec enabled by default)
      })(),
      // Disable read image access (persists across page refreshes via localStorage)
      // Load from localStorage on initialization
      disableReadImageAccess: (() => {
        try {
          const saved = localStorage.getItem(DISABLE_READ_IMAGE_ACCESS_KEY)
          if (saved !== null) {
            const parsed = JSON.parse(saved) as boolean
            return parsed
          }
        } catch (error) {
          console.error('[WorkflowStore] Failed to load disable read image access from localStorage:', error)
        }
        return false  // Default to false (read image enabled by default)
      })(),

      // Variables manifest
      variablesManifest: null,
      selectedGroupIds: [],

      // Current running group
      currentRunningGroupId: null,

      // UI state
      activePhase: null,
      showChatArea: false,

      // Multi-tab chat state
      workflowChatTabs: {},
      activeWorkflowTabId: null,

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

      setPhases: (phases: WorkflowPhase[]) => {
        set({
          phases: phases,
          phasesInitialized: true
        })
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
          p.id === 'plan-improvement'
        )
      },

      // Load run folders for a workspace
      loadRunFolders: async (workspacePath: string) => {
        console.log('[WorkflowStore] loadRunFolders called:', {
          workspacePath,
          stackTrace: new Error().stack?.split('\n').slice(0, 5).join('\n')
        })

        if (!workspacePath) {
          console.log('[WorkflowStore] loadRunFolders: No workspacePath, clearing runFolders')
          set({ runFolders: [] })
          return
        }

        set({ isLoadingRunFolders: true })

        try {
          const response = await agentApi.getRunFolders(workspacePath)
          console.log('[WorkflowStore] loadRunFolders response:', {
            hasResponse: !!response,
            foldersCount: response?.folders?.length || 0,
            folders: response?.folders?.map(f => f.name)
          })

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

      setRunFolders: (folders: RunFolder[]) => {
        set({ runFolders: folders })
      },

      setSelectedRunFolder: (folder: string) => {
        // Use normalization utility to validate folder path
        const state = get()
        const normalized = normalizeRunFolder(folder, state.variablesManifest)
        set({ selectedRunFolder: normalized })
        // Clear progress when switching to 'new'
        if (normalized === 'new') {
          set({ stepProgress: null, stepProgressFolder: null })
        }
      },

      // Load step progress for a run folder
      // NOTE: During active execution (isStreaming=true), this will skip API calls
      // and trust step_progress_updated events instead to avoid race conditions.
      loadProgress: async (workspacePath: string, runFolder: string, forceLoad = false) => {
        if (!workspacePath || runFolder === 'new') {
          set({ stepProgress: null })
          return
        }

        // During execution, skip API calls - trust events instead
        // This prevents race conditions where API might return stale data
        // before backend cleanup completes (e.g., when resuming from step 3)
        const { isStreaming } = useChatStore.getState()
        if (isStreaming && !forceLoad) {
          console.log('[PROGRESS_DEBUG] Skipping loadProgress during execution - trusting step_progress_updated events')
          return
        }

        set({ isLoadingProgress: true, stepProgressFolder: null }) // Clear folder tracking while loading

        try {
          const response = await agentApi.getProgress(workspacePath, runFolder)
          console.log('[PROGRESS_DEBUG] Loaded progress response:', {
            exists: response.exists,
            hasProgress: !!response.progress,
            completedStepIndices: response.progress?.completed_step_indices,
            totalSteps: response.progress?.total_steps,
            runFolder
          })
          if (response.exists && response.progress) {
            const state = get()
            const previousProgressFolder = state.stepProgressFolder
            
            set({ 
              stepProgress: response.progress, 
              stepProgressFolder: runFolder,
              isLoadingProgress: false 
            })
            console.log('[PROGRESS_DEBUG] Set stepProgress in store:', {
              completedStepIndices: response.progress.completed_step_indices,
              totalSteps: response.progress.total_steps,
              folder: runFolder
            })

            // Update the folder info in state so we can show progress in the dropdown
            set(prevState => ({
              runFolders: prevState.runFolders.map(f =>
                f.name === runFolder
                  ? { ...f, progress: response.progress || undefined }
                  : f
              )
            }))

            // Update start point when switching groups - recalculate based on new group's progress
            // Always update start point to match the loaded progress (handles both folder switches and initial loads)
            const completedIndices = response.progress.completed_step_indices || []
            const totalSteps = response.progress.total_steps || 0
            const currentStartPoint = get().selectedStartPoint
            
            // Check if folder changed (group switch detected)
            const folderChanged = previousProgressFolder && previousProgressFolder !== runFolder && previousProgressFolder !== 'new'
            
            console.log('[WorkflowStore] loadProgress - checking start point update:', {
              previousProgressFolder,
              newFolder: runFolder,
              folderChanged,
              completedIndices: completedIndices.length,
              totalSteps,
              currentStartPoint
            })
            
            if (completedIndices.length > 0 && totalSteps > 0) {
              // Calculate the resume point based on the NEW group's progress
              const completedStepNumbers = completedIndices.map(idx => idx + 1).sort((a, b) => a - b)
              const lastCompletedStep = completedStepNumbers[completedStepNumbers.length - 1]
              const nextStep = lastCompletedStep + 1
              const resumePoint = nextStep <= totalSteps ? nextStep : lastCompletedStep
              
              // Always update to match the loaded progress (don't check folderChanged - always sync)
              if (currentStartPoint !== resumePoint) {
                console.log('[WorkflowStore] ✅ Updating start point to match group progress:', {
                  previousFolder: previousProgressFolder,
                  newFolder: runFolder,
                  folderChanged,
                  completedSteps: completedStepNumbers,
                  lastCompletedStep,
                  resumePoint,
                  previousStartPoint: currentStartPoint
                })
                // Normalize and set start point, clear branch step
                const normalized = normalizeStartPoint(resumePoint)
                set({ selectedStartPoint: normalized, selectedBranchStep: null })
              } else {
                console.log('[WorkflowStore] Start point already matches progress, skipping update')
              }
            } else if (completedIndices.length === 0) {
              // New group has no progress - reset to start from beginning
              if (currentStartPoint !== 0) {
                console.log('[WorkflowStore] ✅ Group has no progress, resetting to start from beginning:', {
                  previousFolder: previousProgressFolder,
                  newFolder: runFolder,
                  folderChanged,
                  previousStartPoint: currentStartPoint
                })
                // Reset to 0 and clear branch step
                set({ selectedStartPoint: 0, selectedBranchStep: null })
              } else {
                console.log('[WorkflowStore] Start point already 0, skipping update')
              }
            }
          } else {
            // No progress file exists - only reset progress, preserve user's selectedStartPoint
            // The user's selection should only be reset when they explicitly choose "Start from Beginning"
            const currentStartPoint = get().selectedStartPoint
            const state = get()
            const previousProgressFolder = state.stepProgressFolder
            
            set({ stepProgress: null, stepProgressFolder: null, isLoadingProgress: false })
            console.log('[RESUME_DEBUG] No progress file found, preserving selectedStartPoint:', currentStartPoint)
            
            // If switching to a folder with no progress, reset start point
            if (previousProgressFolder && previousProgressFolder !== runFolder && previousProgressFolder !== 'new') {
              if (currentStartPoint !== 0) {
                console.log('[WorkflowStore] Group switched to folder with no progress, resetting to start from beginning:', {
                  previousFolder: previousProgressFolder,
                  newFolder: runFolder,
                  previousStartPoint: currentStartPoint
                })
                set({ selectedStartPoint: 0 })
              }
            }
          }
        } catch (error) {
          // On error, only reset progress, preserve user's selectedStartPoint
          // The user's selection should only be reset when they explicitly choose "Start from Beginning"
          const currentStartPoint = get().selectedStartPoint
          set({ stepProgress: null, stepProgressFolder: null, isLoadingProgress: false })
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

      setStepProgress: (progress: StepProgress | null) => {
        const state = get()
        const previousProgressFolder = state.stepProgressFolder
        const currentFolder = state.selectedRunFolder
        
        // Clear folder tracking if progress is cleared
        set({ 
          stepProgress: progress,
          stepProgressFolder: progress ? currentFolder : null
        })
        
        // Update start point when switching groups - same logic as in loadProgress
        if (progress && currentFolder && currentFolder !== 'new') {
          // Check if folder changed (group switch detected)
          const folderChanged = previousProgressFolder && previousProgressFolder !== currentFolder && previousProgressFolder !== 'new'
          const completedIndices = progress.completed_step_indices || []
          const totalSteps = progress.total_steps || 0
          const currentStartPoint = get().selectedStartPoint
          
          console.log('[WorkflowStore] setStepProgress - checking start point update:', {
            previousProgressFolder,
            newFolder: currentFolder,
            folderChanged,
            completedIndices: completedIndices.length,
            totalSteps,
            currentStartPoint
          })
          
          if (completedIndices.length > 0 && totalSteps > 0) {
            // Calculate the resume point based on the NEW group's progress
            const completedStepNumbers = completedIndices.map(idx => idx + 1).sort((a, b) => a - b)
            const lastCompletedStep = completedStepNumbers[completedStepNumbers.length - 1]
            const nextStep = lastCompletedStep + 1
            const resumePoint = nextStep <= totalSteps ? nextStep : lastCompletedStep
            
            // Always update to match the loaded progress (don't check folderChanged - always sync)
            if (currentStartPoint !== resumePoint) {
              console.log('[WorkflowStore] ✅ Updating start point (via setStepProgress):', {
                previousFolder: previousProgressFolder,
                newFolder: currentFolder,
                folderChanged,
                completedSteps: completedStepNumbers,
                lastCompletedStep,
                resumePoint,
                previousStartPoint: currentStartPoint
              })
              // Normalize and set start point, clear branch step
              const normalized = normalizeStartPoint(resumePoint)
              set({ selectedStartPoint: normalized, selectedBranchStep: null })
            } else {
              console.log('[WorkflowStore] Start point already matches progress (setStepProgress), skipping update')
            }
          } else if (completedIndices.length === 0) {
            // New group has no progress - reset to start from beginning
            if (currentStartPoint !== 0) {
              console.log('[WorkflowStore] ✅ Group has no progress (via setStepProgress), resetting:', {
                previousFolder: previousProgressFolder,
                newFolder: currentFolder,
                folderChanged,
                previousStartPoint: currentStartPoint
              })
              // Reset to 0 and clear branch step
              set({ selectedStartPoint: 0, selectedBranchStep: null })
            } else {
              console.log('[WorkflowStore] Start point already 0 (setStepProgress), skipping update')
            }
          }
        } else if (!progress && previousProgressFolder) {
          // Progress cleared - reset start point if it was set
          const currentStartPoint = get().selectedStartPoint
          if (currentStartPoint !== 0) {
            console.log('[WorkflowStore] Progress cleared, resetting start point to 0')
            set({ selectedStartPoint: 0 })
          }
        }
      },

      // Update step progress from event data (called when step_progress_updated events are received)
      updateStepProgressFromEvent: (progress: StepProgress) => {
        const state = get()
        // Only update if this progress is for the currently selected run folder
        // Note: We can't check run_folder here since it's not in StepProgress type,
        // but the caller (WorkflowLayout) will filter by run_folder before calling this
        set({ stepProgress: progress })
        
        // Also update the folder info in state if it exists
        if (state.selectedRunFolder && state.selectedRunFolder !== 'new') {
          set(prevState => ({
            runFolders: prevState.runFolders.map(f =>
              f.name === state.selectedRunFolder
                ? { ...f, progress }
                : f
            )
          }))
        }
      },

      setExecutionMode: (mode: ExecutionModeType) => {
        set({ selectedExecutionMode: mode })
      },

      setStartPoint: (step: number | string) => {
        // Use normalization utility to ensure canonical format
        const normalized = normalizeStartPoint(step)
        console.log('[RESUME_DEBUG] setStartPoint called:', step, '→ normalized:', normalized)
        set({ selectedStartPoint: normalized, selectedBranchStep: null }) // Clear branch step when setting regular step
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

        // Resolve the specific group folder path for phases that need context
        // Uses utility function to consolidate logic
        const resolvedRunFolder = resolveGroupFolderPath({
          currentRunningGroupId: state.currentRunningGroupId,
          selectedRunFolder: state.selectedRunFolder,
          selectedGroupIds: state.selectedGroupIds,
          manifest: state.variablesManifest
        })
        
        const options: ExecutionOptions = {
          run_mode: state.selectedRunFolder === 'new' ? 'create_new_runs_always' : 'use_same_run',
          selected_run_folder: resolvedRunFolder,
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

        // Include tool access disable flags
        if (state.disableShellExecAccess) {
          options.disable_shell_exec_access = true
        }
        if (state.disableReadImageAccess) {
          options.disable_read_image_access = true
        }

        // Include enabled group IDs from variables manifest (for batch execution)
        // Group selection is now exclusively via checkboxes (selectedGroupIds)
        // Priority: selectedGroupIds (explicit user selection) > all enabled groups (default)
        if (state.variablesManifest) {
          if (state.variablesManifest.groups && state.variablesManifest.groups.length > 0) {
            // If specific groups were selected via checkboxes, use those
            if (state.selectedGroupIds.length > 0) {
              // Use validation utility to ensure all groups exist and are enabled
              const validGroupIds = validateGroupIds(state.selectedGroupIds, state.variablesManifest)
              if (validGroupIds.length > 0) {
                options.enabled_group_ids = validGroupIds
              } else {
                // Fall back to all enabled groups if selected groups are invalid
                const enabledGroupIDs = state.variablesManifest.groups
                  .filter(g => g.enabled)
                  .map(g => g.group_id)
                if (enabledGroupIDs.length > 0) {
                  options.enabled_group_ids = enabledGroupIDs
                }
              }
            } else {
              // No specific groups selected via checkboxes - use all enabled groups
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
      
      setSaveValidationResponses: () => {
        set({ saveValidationResponses: true })
        try {
          // Always save true to localStorage
          localStorage.setItem(SAVE_VALIDATION_RESPONSES_KEY, JSON.stringify(true))
          console.log(`[WorkflowStore] Save validation responses forced to true`)
        } catch (error) {
          console.error('[WorkflowStore] Failed to save save validation responses to localStorage:', error)
        }
      },
      
      setDisableShellExecAccess: (enabled: boolean) => {
        set({ disableShellExecAccess: enabled })
        try {
          // Save to localStorage
          localStorage.setItem(DISABLE_SHELL_EXEC_ACCESS_KEY, JSON.stringify(enabled))
          console.log(`[WorkflowStore] Disable shell exec access set: ${enabled}`)
        } catch (error) {
          console.error('[WorkflowStore] Failed to save disable shell exec access to localStorage:', error)
        }
      },
      
      setDisableReadImageAccess: (enabled: boolean) => {
        set({ disableReadImageAccess: enabled })
        try {
          // Save to localStorage
          localStorage.setItem(DISABLE_READ_IMAGE_ACCESS_KEY, JSON.stringify(enabled))
          console.log(`[WorkflowStore] Disable read image access set: ${enabled}`)
        } catch (error) {
          console.error('[WorkflowStore] Failed to save disable read image access to localStorage:', error)
        }
      },

      setVariablesManifest: (manifest: VariablesManifest | null) => {
        console.log('[WorkflowStore] setVariablesManifest called:', {
          hasManifest: !!manifest,
          groupsCount: manifest?.groups?.length || 0
        })
        set({ variablesManifest: manifest })
      },

      // Toggle group selection (add if not selected, remove if selected)
      toggleGroupSelection: (groupId: string) => {
        const state = get()
        const currentIds = state.selectedGroupIds
        const isSelected = currentIds.includes(groupId)
        
        if (isSelected) {
          set({ selectedGroupIds: currentIds.filter(id => id !== groupId) })
        } else {
          set({ selectedGroupIds: [...currentIds, groupId] })
        }
      },

      // Set selected group IDs directly
      setSelectedGroupIds: (groupIds: string[]) => {
        set({ selectedGroupIds: groupIds })
      },

      // Clear all selected group IDs
      clearSelectedGroupIds: () => {
        set({ selectedGroupIds: [] })
      },

      setCurrentRunningGroupId: (groupId: string | null) => {
        set({ currentRunningGroupId: groupId })
      },

      // Consolidated batch group switching handler
      // Handles group start: sets currentRunningGroupId and updates selectedRunFolder with normalization
      handleBatchGroupStart: (groupId: string, runFolder: string, workspacePath?: string) => {
        const state = get()
        
        // Set current running group ID
        set({ currentRunningGroupId: groupId })
        
        // Normalize and update selected run folder
        const normalizedFolder = normalizeRunFolder(runFolder, state.variablesManifest)
        set({ selectedRunFolder: normalizedFolder })
        
        console.log('[WorkflowStore] Batch group started:', {
          groupId,
          runFolder,
          normalizedFolder,
          workspacePath
        })
        
        // Reload run folders and progress if workspace path provided
        if (workspacePath) {
          state.loadRunFolders(workspacePath).catch(err => {
            console.warn('[WorkflowStore] Failed to reload run folders:', err)
          })
          
          state.loadProgress(workspacePath, normalizedFolder).catch(err => {
            console.warn('[WorkflowStore] Failed to load progress:', err)
          })
        }
      },

      // Consolidated batch group end handler
      // Clears currentRunningGroupId only if it matches the ended group
      handleBatchGroupEnd: (groupId: string) => {
        const state = get()
        
        // Only clear if this is the currently running group
        // This prevents clearing when events arrive out of order
        if (state.currentRunningGroupId === groupId) {
          set({ currentRunningGroupId: null })
          console.log('[WorkflowStore] Batch group ended, cleared currentRunningGroupId:', groupId)
        } else {
          console.log('[WorkflowStore] Batch group ended but not currently running:', {
            endedGroup: groupId,
            currentRunningGroup: state.currentRunningGroupId
          })
        }
      },

      setActivePhase: (phase: string | null) => {
        set({ activePhase: phase })
      },

      setShowChatArea: (show: boolean) => {
        set({ showChatArea: show })
      },

      // Workflow chat tabs
      createWorkflowTab: async (phaseId: string, phaseName: string): Promise<string> => {
        // Generate unique tab ID
        const tabId = `phase_${phaseId}_${Date.now()}`
        
        // Generate new session ID for tab
        const sessionIdForTab = crypto.randomUUID()
        
        // Create tab entry
        const tab: WorkflowChatTab = {
          tabId,
          phaseId,
          phaseName,
          observerId: sessionIdForTab, // Keep for backward compatibility, set to sessionId
          sessionId: sessionIdForTab,
          isActive: false,
          isStreaming: false,
          isCompleted: false,
          createdAt: Date.now()
        }
        
        // Add to store
        set((state) => ({
          workflowChatTabs: {
            ...state.workflowChatTabs,
            [tabId]: tab
          },
          activeWorkflowTabId: tabId
        }))
        
        return tabId
      },

      switchWorkflowTab: (tabId: string) => {
        const state = get()
        if (!state.workflowChatTabs[tabId]) {
          console.warn(`[WorkflowStore] Tab ${tabId} not found`)
          return
        }
        
        set({ activeWorkflowTabId: tabId })
      },

      closeWorkflowTab: async (tabId: string) => {
        const state = get()
        const tab = state.workflowChatTabs[tabId]
        
        if (!tab) {
          console.warn(`[WorkflowStore] Tab ${tabId} not found`)
          return
        }
        
        // If tab is streaming, stop it first
        if (tab.isStreaming && tab.sessionId) {
          try {
            await agentApi.stopSession(tab.sessionId)
            console.log(`[WorkflowStore] Stopped session ${tab.sessionId} for tab ${tabId}`)
          } catch (error) {
            console.error(`[WorkflowStore] Failed to stop session for tab ${tabId}:`, error)
          }
        }
        
        // No observer removal needed - observers are no longer used
        
        // Remove tab from store
        const newTabs = { ...state.workflowChatTabs }
        delete newTabs[tabId]
        
        // If closing active tab, switch to another tab or clear active
        let newActiveTabId = state.activeWorkflowTabId
        if (state.activeWorkflowTabId === tabId) {
          const remainingTabs = Object.values(newTabs)
          if (remainingTabs.length > 0) {
            // Switch to most recently created tab
            const sortedTabs = remainingTabs.sort((a, b) => b.createdAt - a.createdAt)
            newActiveTabId = sortedTabs[0].tabId
          } else {
            newActiveTabId = null
            set({ showChatArea: false })
          }
        }
        
        set({
          workflowChatTabs: newTabs,
          activeWorkflowTabId: newActiveTabId
        })
      },

      getWorkflowTab: (tabId: string) => {
        return get().workflowChatTabs[tabId]
      },

      getActiveWorkflowTab: () => {
        const state = get()
        if (!state.activeWorkflowTabId) {
          return undefined
        }
        return state.workflowChatTabs[state.activeWorkflowTabId]
      },

      getTabsByPhase: (phaseId: string) => {
        const state = get()
        return Object.values(state.workflowChatTabs).filter(tab => tab.phaseId === phaseId)
      },

      setTabStreaming: (tabId: string, isStreaming: boolean) => {
        const state = get()
        const tab = state.workflowChatTabs[tabId]
        if (!tab) {
          console.warn(`[WorkflowStore] Tab ${tabId} not found for setTabStreaming`)
          return
        }
        
        set((state) => ({
          workflowChatTabs: {
            ...state.workflowChatTabs,
            [tabId]: {
              ...state.workflowChatTabs[tabId],
              isStreaming
            }
          }
        }))
      },

      updateTabSessionId: (tabId: string, sessionId: string) => {
        const state = get()
        const tab = state.workflowChatTabs[tabId]
        if (!tab) {
          console.warn(`[WorkflowStore] Tab ${tabId} not found for updateTabSessionId`)
          return
        }
        
        set((state) => ({
          workflowChatTabs: {
            ...state.workflowChatTabs,
            [tabId]: {
              ...state.workflowChatTabs[tabId],
              sessionId
            }
          }
        }))
      },

      setTabCompleted: (tabId: string, isCompleted: boolean) => {
        const state = get()
        const tab = state.workflowChatTabs[tabId]
        if (!tab) {
          console.warn(`[WorkflowStore] Tab ${tabId} not found for setTabCompleted`)
          return
        }
        
        set((state) => ({
          workflowChatTabs: {
            ...state.workflowChatTabs,
            [tabId]: {
              ...state.workflowChatTabs[tabId],
              isCompleted
            }
          }
        }))
      },

      // Computed: Get tab's streaming status (derived from polling and completion)
      getTabStreamingStatus: (tabId: string) => {
        const state = get()
        const tab = state.workflowChatTabs[tabId]
        if (!tab) {
          return false
        }
        
        // If tab is marked as completed, it's not streaming
        if (tab.isCompleted) {
          return false
        }
        
        // Get chat store state to check polling status
        const chatStore = useChatStore.getState()
        
        // Tab is streaming if:
        // 1. Polling is active
        // 2. This tab's observer ID matches the currently polled observer
        // 3. Not manually paused (stored isStreaming !== false)
        const isPolling = chatStore.pollingInterval !== null
        
        // If polling and not completed, it's streaming
        // The stored tab.isStreaming can be used to pause (e.g., human feedback)
        if (isPolling) {
          return tab.isStreaming !== false // Respect manual pause
        }
        
        return false
      },

      // Check if events contain completion events for this tab's observer
      checkTabCompletion: (tabId: string, events: Array<{ type: string }>) => {
        const state = get()
        const tab = state.workflowChatTabs[tabId]
        if (!tab) {
          return false
        }
        
        // Check if any events are completion events
        // For workflow mode: workflow_end, request_human_feedback
        // For chat mode: unified_completion, agent_end, conversation_end, etc.
        const completionEventTypes = [
          'workflow_end',
          'request_human_feedback',
          'unified_completion',
          'agent_end',
          'conversation_end',
          'conversation_error',
          'agent_error'
        ]
        
        return events.some(event => 
          event.type && completionEventTypes.includes(event.type)
        )
      },

      // Load saved settings from localStorage for a preset
      // Uses a single set() call to avoid multiple re-renders
      loadSavedSettings: (presetId: string) => {
        if (!presetId) return

        try {
          const currentState = get()
          
          // Build state update object - start with defaults
          const stateUpdate: Partial<WorkflowStore> = {
            selectedRunFolder: 'new',
            selectedExecutionMode: 'human_approval',
            selectedStartPoint: 0,
            selectedBranchStep: null,
            selectedGroupIds: [],
            activePhase: null
          }

          // Load saved iteration folder - use normalization utility
          const savedIteration = localStorage.getItem(getStorageKey(presetId, 'iteration'))
          if (savedIteration) {
            stateUpdate.selectedRunFolder = normalizeRunFolder(savedIteration, currentState.variablesManifest)
          }

          // Load saved execution mode
          const savedMode = localStorage.getItem(getStorageKey(presetId, 'execution_mode'))
          if (savedMode && ['human_approval', 'fast_execution', 'with_learning'].includes(savedMode)) {
            stateUpdate.selectedExecutionMode = savedMode as ExecutionModeType
          }

          // Load saved start point - ALWAYS check localStorage first
          // Priority: localStorage saved value > current state (if > 0) > default (0)
          // Use normalization utility to ensure canonical format
          const savedStartPoint = localStorage.getItem(getStorageKey(presetId, 'start_point'))
          if (savedStartPoint) {
            // Use normalization utility (handles string/number conversion)
            const normalized = normalizeStartPoint(savedStartPoint)
            stateUpdate.selectedStartPoint = normalized
            console.log('[RESUME_DEBUG] loadSavedSettings: Loaded start point from localStorage:', normalized, '(current state was:', currentState.selectedStartPoint, ')')
          } else if (currentState.selectedStartPoint > 0) {
            // No saved value in localStorage, but user has made a selection in current session - preserve it
            stateUpdate.selectedStartPoint = currentState.selectedStartPoint
            console.log('[RESUME_DEBUG] loadSavedSettings: No saved value in localStorage, preserving current selection:', currentState.selectedStartPoint)
          } else {
            // No saved value and no current selection - use default (0)
            stateUpdate.selectedStartPoint = 0
            console.log('[RESUME_DEBUG] loadSavedSettings: No saved value and no current selection, using default (0)')
          }

          // Load saved selected group IDs
          // CRITICAL: Use normalization utility to ensure canonical group_ids
          // This handles cases where old data might have folder names instead of group_ids
          const savedGroups = localStorage.getItem(getStorageKey(presetId, 'selected_groups'))
          if (savedGroups) {
            try {
              const parsed = JSON.parse(savedGroups) as string[]
              if (Array.isArray(parsed) && parsed.length > 0) {
                // Use normalization utility (single source of truth)
                const normalized = normalizeGroupIds(parsed, currentState.variablesManifest)
                stateUpdate.selectedGroupIds = normalized
                
                if (normalized.length !== parsed.length) {
                  console.log('[WorkflowStore] Normalized group IDs:', {
                    original: parsed,
                    normalized,
                    removed: parsed.length - normalized.length
                  })
                }
              }
            } catch (e) {
              console.error('[WorkflowStore] Failed to parse saved group IDs:', e)
            }
          }

          // Load saved branch step - CRITICAL: Only load from localStorage if user hasn't made a selection
          // This prevents race conditions where loadSavedSettings runs after user selects a branch step
          if (currentState.selectedBranchStep === null) {
            // No user selection - safe to load from localStorage
            const savedBranchStep = localStorage.getItem(getStorageKey(presetId, 'branch_step'))
            if (savedBranchStep) {
              try {
                const parsed = JSON.parse(savedBranchStep)
                if (parsed && typeof parsed.parentStepIndex === 'number') {
                  stateUpdate.selectedBranchStep = parsed
                  console.log('[RESUME_DEBUG] loadSavedSettings: Loaded branch step from localStorage:', parsed)
                }
              } catch (e) {
                console.error('[WorkflowStore] Failed to parse saved branch step:', e)
              }
            }
          } else {
            // User has made a selection - preserve it and don't overwrite
            stateUpdate.selectedBranchStep = currentState.selectedBranchStep
            console.log('[RESUME_DEBUG] loadSavedSettings: Preserving user-selected branch step:', currentState.selectedBranchStep)
          }

          // Load saved active phase
          const savedActivePhase = localStorage.getItem(getStorageKey(presetId, 'active_phase'))
          if (savedActivePhase) {
            stateUpdate.activePhase = savedActivePhase
          }

          // Single set() call to avoid multiple re-renders
          set(stateUpdate)
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
          localStorage.setItem(getStorageKey(presetId, 'selected_groups'), JSON.stringify(state.selectedGroupIds))
          // Save branch step (null-safe)
          if (state.selectedBranchStep) {
            localStorage.setItem(getStorageKey(presetId, 'branch_step'), JSON.stringify(state.selectedBranchStep))
          } else {
            localStorage.removeItem(getStorageKey(presetId, 'branch_step'))
          }
          // Save active phase (null-safe)
          if (state.activePhase) {
            localStorage.setItem(getStorageKey(presetId, 'active_phase'), state.activePhase)
          } else {
            localStorage.removeItem(getStorageKey(presetId, 'active_phase'))
          }
        } catch (error) {
          console.error('[WorkflowStore] Failed to save settings to localStorage:', error)
        }
      },

      // Switch to a new preset - combines reset and load into a single set() call
      // This avoids multiple re-renders that would occur with separate reset + load calls
      switchToPreset: (presetId: string) => {
        if (!presetId) return

        try {
          // Start with context reset values (workflow-specific data that must be cleared)
          const stateUpdate: Partial<WorkflowStore> = {
            // Reset workflow context (loaded from API, not per-preset)
            runFolders: [],
            stepProgress: null,
            variablesManifest: null,
            currentRunningGroupId: null,
            workflowChatTabs: {},
            activeWorkflowTabId: null,
            // Defaults for user-selectable settings (will be overwritten by saved values)
            selectedRunFolder: 'new',
            selectedExecutionMode: 'human_approval',
            selectedStartPoint: 0,
            selectedBranchStep: null,
            selectedGroupIds: [],
            activePhase: null
          }

          // Load saved iteration folder - use normalization utility
          const savedIteration = localStorage.getItem(getStorageKey(presetId, 'iteration'))
          if (savedIteration) {
            const currentState = get()
            stateUpdate.selectedRunFolder = normalizeRunFolder(savedIteration, currentState.variablesManifest)
          }

          // Load saved execution mode
          const savedMode = localStorage.getItem(getStorageKey(presetId, 'execution_mode'))
          if (savedMode && ['human_approval', 'fast_execution', 'with_learning'].includes(savedMode)) {
            stateUpdate.selectedExecutionMode = savedMode as ExecutionModeType
          }

          // Load saved start point - use normalization utility
          const savedStartPoint = localStorage.getItem(getStorageKey(presetId, 'start_point'))
          if (savedStartPoint) {
            stateUpdate.selectedStartPoint = normalizeStartPoint(savedStartPoint)
          }

          // Load saved selected group IDs
          // CRITICAL: Use normalization utility to ensure canonical group_ids
          const savedGroups = localStorage.getItem(getStorageKey(presetId, 'selected_groups'))
          if (savedGroups) {
            try {
              const parsed = JSON.parse(savedGroups) as string[]
              if (Array.isArray(parsed) && parsed.length > 0) {
                const currentState = get()
                // Use normalization utility (single source of truth)
                const normalized = normalizeGroupIds(parsed, currentState.variablesManifest)
                stateUpdate.selectedGroupIds = normalized
              }
            } catch (e) {
              console.error('[WorkflowStore] Failed to parse saved group IDs:', e)
            }
          }

          // Load saved branch step
          const savedBranchStep = localStorage.getItem(getStorageKey(presetId, 'branch_step'))
          if (savedBranchStep) {
            try {
              const parsed = JSON.parse(savedBranchStep)
              if (parsed && typeof parsed.parentStepIndex === 'number') {
                stateUpdate.selectedBranchStep = parsed
              }
            } catch (e) {
              console.error('[WorkflowStore] Failed to parse saved branch step:', e)
            }
          }

          // Load saved active phase
          const savedActivePhase = localStorage.getItem(getStorageKey(presetId, 'active_phase'))
          if (savedActivePhase) {
            stateUpdate.activePhase = savedActivePhase
          }

          // Single set() call - resets context AND loads saved settings
          set(stateUpdate)
        } catch (error) {
          console.error('[WorkflowStore] Failed to switch preset:', error)
        }
      },

      // Reset execution state (called when switching workflows)
      // Note: Values reset here will be immediately overwritten by loadSavedSettings()
      // which loads the per-preset saved values from localStorage
      resetExecutionState: () => {
        // Note: We don't clear tempOverrideLLM here because it's a global setting
        // that should persist across workflow switches
        // Note: We don't reset showChatArea or activePhase - these persist or are loaded per-preset

        set({
          runFolders: [],
          selectedRunFolder: 'new',
          stepProgress: null,
          selectedExecutionMode: 'human_approval',
          selectedStartPoint: 0,
          selectedBranchStep: null,
          variablesManifest: null,
          selectedGroupIds: [],
          currentRunningGroupId: null,
          // activePhase is saved/loaded per-preset, don't reset here
          workflowChatTabs: {},
          activeWorkflowTabId: null
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

// Legacy selector removed - use selectedGroupIds array instead
// Group selection is now exclusively managed via checkbox multi-select (selectedGroupIds)

