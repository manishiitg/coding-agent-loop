import { create } from 'zustand'
import type { WorkflowPhase, StepProgress, ExecutionOptions, AgentLLMConfig, VariablesManifest, EvaluationPlan } from '../services/api-types'
import type { AgentConfigs } from '../utils/stepConfigMatching'
import { ExecutionStrategy } from '../services/api-types'
import { agentApi } from '../services/api'
import { useChatStore } from './useChatStore'
import { useGlobalPresetStore } from './useGlobalPresetStore'
import { resolveGroupFolderPath } from '../utils/workflowUtils'
import { normalizeStartPoint, normalizeRunFolder } from '../utils/workflowStateNormalization'

export type WorkflowWorkspaceView = 'builder' | 'execution' | null

// Layout direction for workflow canvas
export type LayoutDirection = 'LR' | 'TB'
export type CanvasViewMode = 'flow' | 'plan'

// Global localStorage key for temporary LLM overrides (persists across page refreshes)
const TEMP_OVERRIDE_LLM_KEY = 'workflow_temp_override_llm'
const TEMP_OVERRIDE_LLM2_KEY = 'workflow_temp_override_llm2'
const TEMP_OVERRIDE_LLM_ENABLED_KEY = 'workflow_temp_override_llm_enabled'
const FALLBACK_TO_ORIGINAL_LLM_KEY = 'workflow_fallback_to_original_llm_on_failure'
const SKIP_LEARNING_WHEN_TEMP_LLM1_KEY = 'workflow_skip_learning_when_temp_llm1'
const SKIP_LEARNING_WHEN_TEMP_LLM2_KEY = 'workflow_skip_learning_when_temp_llm2'
const TEMP_LEARNING_LLM_KEY = 'workflow_temp_learning_llm'
const SELECTED_GROUP_IDS_KEY = 'workflow_selected_group_ids'
const CURRENT_RUNNING_GROUP_ID_KEY = 'workflow_current_running_group_id'
const SELECTED_RUN_FOLDER_KEY = 'workflow_selected_run_folder'
const LAYOUT_DIRECTION_KEY = 'workflow_layout_direction'
const CANVAS_VIEW_MODE_KEY = 'workflow_canvas_view_mode'
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

// Per-preset workflow state — isolated so switching presets doesn't cross-contaminate
export interface PresetWorkflowState {
  selectedRunFolder: string | null
  stepProgress: StepProgress | null
  stepProgressFolder: string | null
  activePhase: string | null
  showChatArea: boolean
  chatAreaExpanded: boolean
  workflowWorkspaceView: WorkflowWorkspaceView
  workflowWorkspaceSelectionTouched: boolean
  workflowChatTabs: Record<string, WorkflowChatTab>
  activeWorkflowTabId: string | null
  selectedGroupIds: string[]
  currentRunningGroupId: string | null
  currentStepId: string | null
  stepStatusMap: Map<string, 'pending' | 'running' | 'completed' | 'failed'>
  batchProgress: {
    isActive: boolean
    totalGroups: number
    currentGroupIndex: number
    currentGroupId: string | null
    completedCount: number
    failedCount: number
    remainingCount: number
    startTime: number | null
  } | null
  selectedStartPoint: number
  selectedBranchStep: {
    parentStepIndex: number
    branchType: 'if_true' | 'if_false'
    branchStepIndex: number
  } | null
}

function createDefaultPresetState(): PresetWorkflowState {
  return {
    selectedRunFolder: null,
    stepProgress: null,
    stepProgressFolder: null,
    activePhase: null,
    showChatArea: false,
    chatAreaExpanded: true,
    workflowWorkspaceView: null,
    workflowWorkspaceSelectionTouched: false,
    workflowChatTabs: {},
    activeWorkflowTabId: null,
    selectedGroupIds: [],
    currentRunningGroupId: null,
    currentStepId: null,
    stepStatusMap: new Map(),
    batchProgress: null,
    selectedStartPoint: 0,
    selectedBranchStep: null,
  }
}

// Snapshot current flat fields into a PresetWorkflowState object
function snapshotPresetState(state: WorkflowStore): PresetWorkflowState {
  return {
    selectedRunFolder: state.selectedRunFolder,
    stepProgress: state.stepProgress,
    stepProgressFolder: state.stepProgressFolder,
    activePhase: state.activePhase,
    showChatArea: state.showChatArea,
    chatAreaExpanded: state.chatAreaExpanded,
    workflowWorkspaceView: state.workflowWorkspaceView,
    workflowWorkspaceSelectionTouched: state.workflowWorkspaceSelectionTouched,
    workflowChatTabs: state.workflowChatTabs,
    activeWorkflowTabId: state.activeWorkflowTabId,
    selectedGroupIds: state.selectedGroupIds,
    currentRunningGroupId: state.currentRunningGroupId,
    currentStepId: state.currentStepId,
    stepStatusMap: new Map(state.stepStatusMap),
    batchProgress: state.batchProgress,
    selectedStartPoint: state.selectedStartPoint,
    selectedBranchStep: state.selectedBranchStep,
  }
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
  selectedRunFolder: string | null // Folder name or null
  isLoadingRunFolders: boolean

  // Step progress
  stepProgress: StepProgress | null
  stepProgressFolder: string | null // Track which folder the current progress belongs to
  isLoadingProgress: boolean

  // Execution options
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
      tempLearningLLM: AgentLLMConfig | null  // Temp LLM for learning agents (used when learnings already exist for a step)
      saveValidationResponses: boolean  // If true, save validation responses to workspace validation folder
      disableShellExecAccess: boolean  // If true, disable all execute_shell_command tool access globally
      disableReadImageAccess: boolean  // If true, disable all read_image tool access globally
  // Variables manifest (for batch execution with multiple groups)
  variablesManifest: VariablesManifest | null

  // Track current preset ID to detect page reload vs preset switch
  _currentPresetId: string | null
  // Per-preset state map — saves/restores state when switching between workflows
  _presetStates: Record<string, PresetWorkflowState>

  // Selected group IDs for execution (multi-select)
  selectedGroupIds: string[] // Array of group IDs to execute

  // Current running group (for batch execution)
  currentRunningGroupId: string | null

  // Current step being executed (for auto-focus on canvas)
  currentStepId: string | null

  // Step execution status map (stepId -> status)
  stepStatusMap: Map<string, 'pending' | 'running' | 'completed' | 'failed'>

  // Batch execution progress (for progress header display)
  batchProgress: {
    isActive: boolean
    totalGroups: number
    currentGroupIndex: number
    currentGroupId: string | null
    completedCount: number
    failedCount: number
    remainingCount: number
    startTime: number | null
  } | null

  // UI state
  activePhase: string | null // Currently running phase
  showChatArea: boolean
  chatAreaExpanded: boolean
  workflowWorkspaceView: WorkflowWorkspaceView
  workflowWorkspaceSelectionTouched: boolean
  layoutDirection: LayoutDirection // Canvas layout direction ('LR' = horizontal, 'TB' = vertical)
  canvasViewMode: CanvasViewMode // 'flow' = React Flow diagram, 'plan' = readable outline

  // Multi-tab chat state
  workflowChatTabs: Record<string, WorkflowChatTab>  // tabId -> tab
  activeWorkflowTabId: string | null  // Currently selected tab

  // === WORKFLOW MODE STATE ===
  workflowMode: 'plan' | 'eval' | 'output'
  workshopMode: 'builder' | 'optimizer' | 'debugger' | 'runner' | 'eval' | 'output'
  workshopModeByPreset: Record<string, 'builder' | 'optimizer' | 'debugger' | 'runner' | 'eval' | 'output'>
  setWorkshopMode: (mode: 'builder' | 'optimizer' | 'debugger' | 'runner' | 'eval' | 'output') => void
  evaluationPlan: EvaluationPlan | null
  evaluationStepProgress: StepProgress | null
  isLoadingEvaluationPlan: boolean

  // Global step override (from step_override.json - applies to all steps)
  stepOverride: AgentConfigs | null

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
  setSelectedRunFolder: (folder: string | null) => void

  // Progress
  loadProgress: (workspacePath: string, runFolder: string, forceLoad?: boolean) => Promise<void>
  loadFolderProgressOnDemand: (workspacePath: string, folderName: string) => Promise<void>
  setStepProgress: (progress: StepProgress | null) => void
  getCompletedStepIndices: () => number[]
  updateStepProgressFromEvent: (progress: StepProgress) => void

  // Execution options
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
  setTempLearningLLM: (config: AgentLLMConfig | null) => void
  clearTempLearningLLM: () => void
  setSaveValidationResponses: () => void
  setDisableShellExecAccess: (enabled: boolean) => void
  setDisableReadImageAccess: (enabled: boolean) => void

  // Variables manifest
  setVariablesManifest: (manifest: VariablesManifest | null) => void

  // Selected group IDs
  toggleGroupSelection: (groupId: string) => void
  setSelectedGroupIds: (groupIds: string[]) => void
  clearSelectedGroupIds: () => void
  // Restore selection state from localStorage (called after API load completes)
  restoreSelectionFromLocalStorage: () => void

  // Current running group
  setCurrentRunningGroupId: (groupId: string | null) => void

  // Current step (for auto-focus on canvas)
  setCurrentStepId: (stepId: string | null) => void

  // Step status updates
  setStepStatus: (stepId: string, status: 'pending' | 'running' | 'completed' | 'failed') => void
  clearStepStatusMap: () => void

  // Consolidated batch group switching handler
  // Handles group start/end events and updates state atomically
  handleBatchGroupStart: (groupId: string, runFolder: string, workspacePath?: string, groupIndex?: number, totalGroups?: number) => void
  handleBatchGroupEnd: (groupId: string, success?: boolean, remainingGroups?: number) => void

  // Reset batch progress (called when batch completes or is canceled)
  resetBatchProgress: () => void

  // UI
  setActivePhase: (phase: string | null) => void
  setShowChatArea: (show: boolean) => void
  setChatAreaExpanded: (expanded: boolean) => void
  setWorkflowWorkspaceView: (view: WorkflowWorkspaceView) => void
  setLayoutDirection: (direction: LayoutDirection) => void
  setCanvasViewMode: (mode: CanvasViewMode) => void

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

  // Global step override
  setStepOverride: (override: AgentConfigs | null) => void

  // Workflow Mode Actions
  setWorkflowMode: (mode: 'plan' | 'eval' | 'output') => void
  setEvaluationPlan: (plan: EvaluationPlan | null) => void
  loadEvaluationPlan: (workspacePath: string) => Promise<void>

  // Persistence (localStorage)
  loadSavedSettings: (presetId: string) => void
  saveSettings: () => void
  // Switch to a new preset - resets context and loads settings in one update
  switchToPreset: (presetId: string) => void

  // Reset
  resetExecutionState: () => void
}

export const useWorkflowStore = create<WorkflowStore>()(
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
      // Selected run folder (persists across page refreshes via localStorage)
      // Can be stored in combined format: { groupIds: [...], runFolder: "..." } or separate key
      selectedRunFolder: (() => {
        try {
          // First try combined format
          const groupData = localStorage.getItem(SELECTED_GROUP_IDS_KEY)
          if (groupData) {
            const parsed = JSON.parse(groupData)
            if (parsed && typeof parsed === 'object' && !Array.isArray(parsed) && parsed.runFolder) {
              return parsed.runFolder
            }
          }
          // Fallback to separate key
          const saved = localStorage.getItem(SELECTED_RUN_FOLDER_KEY)
          if (saved) {
            return saved
          }
        } catch (error) {
          console.error('[WorkflowStore] Failed to load selectedRunFolder:', error)
        }
        return null
      })(),
      isLoadingRunFolders: false,

      // Progress
      stepProgress: null,
      stepProgressFolder: null, // Track which folder the current progress belongs to
      isLoadingProgress: false,

      // Execution options
      selectedStartPoint: 0,
      selectedBranchStep: null,

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
      tempLearningLLM: (() => {
        try {
          const saved = localStorage.getItem(TEMP_LEARNING_LLM_KEY)
          if (saved) {
            const parsed = JSON.parse(saved) as AgentLLMConfig
            if (parsed.provider && parsed.model_id) {
              return parsed
            }
          }
        } catch (error) {
          console.error('[WorkflowStore] Failed to load temp learning LLM from localStorage:', error)
        }
        return null
      })(),
      // Save validation responses to workspace (always enabled, not persisted)
      saveValidationResponses: true,
      // Disable shell exec access (defaults to false, not persisted)
      disableShellExecAccess: false,
      // Disable read image access (defaults to false, not persisted)
      disableReadImageAccess: false,

      // Variables manifest
      variablesManifest: null,

      // Track current preset ID to detect page reload vs preset switch
      _currentPresetId: null as string | null,
      _presetStates: {} as Record<string, PresetWorkflowState>,

      // Selected group IDs (persists across page refreshes via localStorage)
      // Now stored in combined format: { groupIds: [...], runFolder: "..." }
      selectedGroupIds: (() => {
        try {
          const saved = localStorage.getItem(SELECTED_GROUP_IDS_KEY)
          if (saved) {
            const parsed = JSON.parse(saved)
            // Handle new format: { groupIds: [...], runFolder: "..." }
            if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
              if (Array.isArray(parsed.groupIds)) {
                return parsed.groupIds
              }
            }
            // Handle old format: just array of groupIds
            else if (Array.isArray(parsed)) {
              return parsed
            }
          }
        } catch (error) {
          console.error('[WorkflowStore] Failed to load selectedGroupIds:', error)
        }
        return []
      })(),

      // Current running group (persists across page refreshes via localStorage)
      currentRunningGroupId: (() => {
        try {
          const saved = localStorage.getItem(CURRENT_RUNNING_GROUP_ID_KEY)
          if (saved) {
            return saved
          }
        } catch (error) {
          console.error('[WorkflowStore] Failed to load currentRunningGroupId from localStorage:', error)
        }
        return null
      })(),

      // Current step being executed (for auto-focus)
      currentStepId: null,

      // Step execution status map
      stepStatusMap: new Map(),

      // Batch execution progress
      batchProgress: null,

      // UI state
      activePhase: null,
      showChatArea: false,
      chatAreaExpanded: true,
      workflowWorkspaceView: null,
      workflowWorkspaceSelectionTouched: false,
      // Layout direction (persists across page refreshes via localStorage)
      layoutDirection: (() => {
        try {
          const saved = localStorage.getItem(LAYOUT_DIRECTION_KEY)
          if (saved === 'LR' || saved === 'TB') {
            return saved
          }
        } catch (error) {
          console.error('[WorkflowStore] Failed to load layout direction from localStorage:', error)
        }
        return 'LR' // Default to horizontal layout
      })() as LayoutDirection,
      // Canvas view mode (persists across page refreshes via localStorage)
      canvasViewMode: (() => {
        try {
          const saved = localStorage.getItem(CANVAS_VIEW_MODE_KEY)
          if (saved === 'flow' || saved === 'plan') {
            return saved
          }
        } catch {
          // ignore
        }
        return 'flow' as CanvasViewMode
      })(),

      // Multi-tab chat state
      workflowChatTabs: {},
      activeWorkflowTabId: null,

      // === Workflow Mode State ===
      workflowMode: 'plan',
      workshopMode: 'optimizer' as const,
      workshopModeByPreset: {},
      setWorkshopMode: (mode: 'builder' | 'optimizer' | 'debugger' | 'runner' | 'eval' | 'output') => {
        const presetId = useGlobalPresetStore.getState().activePresetIds.workflow
        set((state) => ({
          workshopMode: mode,
          workflowMode: mode === 'eval' ? 'eval' : mode === 'output' ? 'output' : 'plan',
          workshopModeByPreset: presetId
            ? { ...state.workshopModeByPreset, [presetId]: mode }
            : state.workshopModeByPreset,
        }))
      },
      evaluationPlan: null,
      evaluationStepProgress: null,
      isLoadingEvaluationPlan: false,

      // Global step override
      stepOverride: null,

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

      // Get phases that can work on individual steps (currently none — use Workflow Builder instead)
      getStepSpecificPhases: () => {
        return []
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

          // Auto-select latest iteration if no selection exists
          const currentSelection = get().selectedRunFolder
          if (!currentSelection && sorted.length > 0) {
            const latest = sorted[0].name
            set({ selectedRunFolder: latest })
            try {
              localStorage.setItem(SELECTED_RUN_FOLDER_KEY, latest)
            } catch { /* ignore */ }
            get().loadProgress(workspacePath, latest)
          }

          // Validate current selection
          if (currentSelection && !sorted.some(f => f.name === currentSelection)) {
            // Check if the selection looks like a valid iteration folder (e.g., "iteration-3")
            // This handles the case where a folder was just created but hasn't appeared in the list yet
            const isValidIterationPattern = /^iteration-\d+/.test(currentSelection)
            
            if (isValidIterationPattern) {
              // Preserve the selection even if not in list yet (it was likely just created)
              // The folder should appear in the next refresh
            } else {
              // Saved folder no longer exists and doesn't match iteration pattern, default to newest or null
              const newSelection = sorted.length > 0 ? sorted[0].name : null
              set({ selectedRunFolder: newSelection })

              // Persist to localStorage
              try {
                if (newSelection) {
                  localStorage.setItem(SELECTED_RUN_FOLDER_KEY, newSelection)
                } else {
                  localStorage.removeItem(SELECTED_RUN_FOLDER_KEY)
                }
              } catch (error) {
                console.error('[WorkflowStore] Failed to save selectedRunFolder to localStorage:', error)
              }

              // Load progress for new selection if it exists
              if (newSelection) {
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

      setSelectedRunFolder: (folder: string | null) => {
        // Use normalization utility to validate folder path
        const state = get()
        const normalized = normalizeRunFolder(folder, state.variablesManifest)
        set({ selectedRunFolder: normalized })

        // Persist to localStorage - update combined format AND separate key
        // Note: startPoint is NOT persisted - it's calculated from progress
        try {
          // Update combined format
          const existingData = localStorage.getItem(SELECTED_GROUP_IDS_KEY)
          if (existingData) {
            const parsed = JSON.parse(existingData)
            if (parsed && typeof parsed === 'object') {
              parsed.runFolder = normalized
              localStorage.setItem(SELECTED_GROUP_IDS_KEY, JSON.stringify(parsed))
            }
          } else if (normalized) {
            // Create new combined format if it doesn't exist
            const persistData = {
              groupIds: state.selectedGroupIds,
              runFolder: normalized,
              // eslint-disable-next-line @typescript-eslint/no-explicit-any
              presetId: (state as any)._currentPresetId
            }
            localStorage.setItem(SELECTED_GROUP_IDS_KEY, JSON.stringify(persistData))
          }

          // Also update separate key for backward compatibility
          if (normalized) {
            localStorage.setItem(SELECTED_RUN_FOLDER_KEY, normalized)
          } else {
            localStorage.removeItem(SELECTED_RUN_FOLDER_KEY)
          }
        } catch (error) {
          console.error('[WorkflowStore] Failed to save selectedRunFolder to localStorage:', error)
        }

        // Clear progress when switching to null
        if (!normalized) {
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
              // Calculate the resume point based on the group's progress
              const completedStepNumbers = completedIndices.map(idx => idx + 1).sort((a, b) => a - b)
              const lastCompletedStep = completedStepNumbers[completedStepNumbers.length - 1]
              const nextStep = lastCompletedStep + 1
              const resumePoint = nextStep <= totalSteps ? nextStep : lastCompletedStep

              // Always auto-set start point to match loaded progress
              if (currentStartPoint !== resumePoint) {
                console.log('[WorkflowStore] ✅ Updating start point to match group progress:', {
                  previousFolder: previousProgressFolder,
                  newFolder: runFolder,
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
              // Group has no progress - reset to start from beginning
              if (currentStartPoint !== 0) {
                console.log('[WorkflowStore] ✅ Group has no progress, resetting to start from beginning:', {
                  previousFolder: previousProgressFolder,
                  newFolder: runFolder,
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

      setStartPoint: (step: number | string) => {
        // Use normalization utility to ensure canonical format
        const normalized = normalizeStartPoint(step)
        console.log('[RESUME_DEBUG] setStartPoint called:', step, '→ normalized:', normalized)
        set({ selectedStartPoint: normalized, selectedBranchStep: null }) // Clear branch step when setting regular step
        // Note: startPoint is NOT persisted to localStorage - it's always calculated from progress
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
          isResuming,
          isResumingBranch
        })

        // All execution uses learning enabled, no human feedback
        const executionStrategy: string = (isResuming || isResumingBranch)
          ? ExecutionStrategy.RESUME_FROM_STEP_NO_HUMAN
          : ExecutionStrategy.START_FROM_BEGINNING_NO_HUMAN
        // Manual runs reuse the user-selected iteration and clear outputs;
        // scheduled runs create a new iteration in the scheduler.
        const shouldUseSameRun = true

        console.log('[RESUME_DEBUG] Selected execution strategy:', executionStrategy)

        // Resolve the specific group folder path for phases that need context
        // Uses utility function to consolidate logic
        console.log('[buildExecutionOptions] Before resolveGroupFolderPath:', {
          currentRunningGroupId: state.currentRunningGroupId,
          selectedRunFolder: state.selectedRunFolder,
          selectedGroupIds: state.selectedGroupIds,
          selectedGroupIdsLength: state.selectedGroupIds.length,
          hasManifest: !!state.variablesManifest,
          groupsCount: state.variablesManifest?.groups?.length || 0
        })
        const resolvedRunFolder = resolveGroupFolderPath({
          currentRunningGroupId: state.currentRunningGroupId,
          selectedRunFolder: state.selectedRunFolder,
          selectedGroupIds: state.selectedGroupIds,
          manifest: state.variablesManifest
        })
        console.log('[buildExecutionOptions] After resolveGroupFolderPath, resolvedRunFolder:', resolvedRunFolder)
        
        const options: ExecutionOptions = {
          run_mode: shouldUseSameRun ? 'use_same_run' : 'create_new_runs_always',
          selected_run_folder: resolvedRunFolder,
          execution_strategy: executionStrategy,
          workshop_mode: (() => {
            const presetId = useGlobalPresetStore.getState().activePresetIds.workflow
            return (presetId && state.workshopModeByPreset[presetId]) || state.workshopMode
          })(),
        }

        // Include resume_from_step for regular step resuming
        // CRITICAL: Only set resume_from_step if we have a valid step number (> 0)
        // This prevents sending resume_from_step=0 which causes backend to delete all completed steps
        console.log('[RESUME_DEBUG] Checking resume_from_step conditions:', {
          isResuming,
          isResumingBranch,
          selectedStartPoint: state.selectedStartPoint,
          executionStrategy,
          isResumeStrategy: executionStrategy === ExecutionStrategy.RESUME_FROM_STEP_NO_HUMAN ||
                           executionStrategy === ExecutionStrategy.RUN_SINGLE_STEP
        })

        if (isResuming && !isResumingBranch &&
            state.selectedStartPoint > 0 &&
            (executionStrategy === ExecutionStrategy.RESUME_FROM_STEP_NO_HUMAN ||
             executionStrategy === ExecutionStrategy.RUN_SINGLE_STEP)) {
          options.resume_from_step = state.selectedStartPoint
          console.log('[RESUME_DEBUG] ✅ Setting resume_from_step:', state.selectedStartPoint)
        } else if (state.selectedStartPoint > 0 && !isResumingBranch) {
          // Log warning if selectedStartPoint > 0 but strategy is not a resume strategy
          console.warn('[RESUME_DEBUG] ⚠️ selectedStartPoint is', state.selectedStartPoint, 
            'but strategy is', executionStrategy, '- not including resume_from_step')
          console.warn('[RESUME_DEBUG] ⚠️ This means resume_from_step will NOT be set in options!')
        } else if (executionStrategy === ExecutionStrategy.RESUME_FROM_STEP_NO_HUMAN &&
                   !isResumingBranch && state.selectedStartPoint === 0) {
          // CRITICAL: If resume strategy is selected but no valid step, log error and fallback to start from beginning
          console.error('[RESUME_DEBUG] 🚨 CRITICAL: Resume strategy selected but selectedStartPoint is 0! Falling back to start from beginning.')
          console.error('[RESUME_DEBUG] 🚨 This would cause backend to delete all completed steps. Strategy:', executionStrategy)
          // Override strategy to start from beginning to prevent data loss
          options.execution_strategy = ExecutionStrategy.START_FROM_BEGINNING_NO_HUMAN
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
            executionStrategy === ExecutionStrategy.RESUME_FROM_STEP_NO_HUMAN) {
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
        
        // Include temp learning LLM if set (used when learnings already exist for a step)
        if (state.tempLearningLLM) {
          options.temp_learning_llm = state.tempLearningLLM
        }
        
        // Include save validation responses flag (always send to ensure backend knows user preference)
        options.save_validation_responses = state.saveValidationResponses

        // Include enabled group IDs from user selection
        // NOTE: Execute button is disabled when no groups are selected, so if execution happens,
        // selectedGroupIds MUST have values (when groups exist). We use them directly.
        console.log('[EXECUTION_OPTIONS_DEBUG] [useWorkflowStore] Group selection check:', {
          selectedGroupIds: state.selectedGroupIds,
          selectedGroupIdsLength: state.selectedGroupIds.length,
          hasManifest: !!state.variablesManifest,
          groupsCount: state.variablesManifest?.groups?.length || 0
        })
        
        // Prefer explicit group selection from the UI, but fall back to enabled groups
        // from the manifest so workflow builder chat still works before the user has
        // manually clicked a group selector in the canvas.
        if (state.selectedGroupIds.length > 0) {
          options.enabled_group_ids = state.selectedGroupIds
          console.log('[EXECUTION_OPTIONS_DEBUG] [useWorkflowStore] ✅ Set enabled_group_ids from selectedGroupIds:', state.selectedGroupIds)
        } else {
          const enabledManifestGroupIds = (state.variablesManifest?.groups || [])
            .filter(group => group.enabled)
            .map(group => group.group_id)

          if (enabledManifestGroupIds.length > 0) {
            options.enabled_group_ids = enabledManifestGroupIds
            console.log('[EXECUTION_OPTIONS_DEBUG] [useWorkflowStore] ✅ Falling back to enabled manifest groups:', enabledManifestGroupIds)
          } else {
            console.warn('[EXECUTION_OPTIONS_DEBUG] [useWorkflowStore] ⚠️ No groups selected and no enabled manifest groups found - enabled_group_ids will be omitted')
          }
        }

        // Read feature toggles from preset and include when disabled (backend defaults to enabled)
        const presetStore = useGlobalPresetStore.getState()
        const activePreset = presetStore.getActivePreset('workflow')
        const presetLLMConfig = activePreset?.llmConfig

        if (presetLLMConfig?.use_knowledgebase === false) {
          options.enable_knowledgebase = false
        }
        if (presetLLMConfig?.enable_context_summarization === false) {
          options.enable_context_summarization = false
        }

        options.skip_execution_cleanup = false

        console.log('[RESUME_DEBUG] ✅ Final execution options:', JSON.stringify({
          execution_strategy: options.execution_strategy,
          resume_from_step: options.resume_from_step,
          resume_from_branch_step: options.resume_from_branch_step,
          run_mode: options.run_mode,
          selected_run_folder: options.selected_run_folder,
          enabled_group_ids: options.enabled_group_ids,
          skip_execution_cleanup: options.skip_execution_cleanup
        }, null, 2))

        console.log('[EXECUTION_OPTIONS_DEBUG] [useWorkflowStore] buildExecutionOptions returning:', JSON.stringify(options, null, 2))
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
      
      setTempLearningLLM: (config: AgentLLMConfig | null) => {
        set({ tempLearningLLM: config })
        try {
          if (config) {
            // Save to localStorage
            localStorage.setItem(TEMP_LEARNING_LLM_KEY, JSON.stringify(config))
            console.log(`[WorkflowStore] Temporary learning LLM set: ${config.provider}/${config.model_id}`)
          } else {
            // Clear from localStorage
            localStorage.removeItem(TEMP_LEARNING_LLM_KEY)
            console.log('[WorkflowStore] Temporary learning LLM cleared')
          }
        } catch (error) {
          console.error('[WorkflowStore] Failed to save temp learning LLM to localStorage:', error)
        }
      },
      
      clearTempLearningLLM: () => {
        set({ tempLearningLLM: null })
        try {
          localStorage.removeItem(TEMP_LEARNING_LLM_KEY)
          console.log('[WorkflowStore] Temporary learning LLM cleared')
        } catch (error) {
          console.error('[WorkflowStore] Failed to clear temp learning LLM from localStorage:', error)
        }
      },
      
      setSaveValidationResponses: () => {
        set({ saveValidationResponses: true })
        // No longer persisted to localStorage
      },
      
      setDisableShellExecAccess: (enabled: boolean) => {
        set({ disableShellExecAccess: enabled })
        // No longer persisted to localStorage
      },
      
      setDisableReadImageAccess: (enabled: boolean) => {
        set({ disableReadImageAccess: enabled })
        // No longer persisted to localStorage
      },

      setVariablesManifest: (manifest: VariablesManifest | null) => {
        set({ variablesManifest: manifest })
      },

      // Toggle group selection (add if not selected, remove if selected)
      toggleGroupSelection: (groupId: string) => {
        const state = get()
        const currentIds = state.selectedGroupIds
        const isSelected = currentIds.includes(groupId)

        const newIds = isSelected
          ? currentIds.filter(id => id !== groupId)
          : [...currentIds, groupId]

        set({ selectedGroupIds: newIds })

        // Persist to localStorage - save groupIds, runFolder, AND presetId
        // Note: startPoint is NOT persisted - it's calculated from progress
        try {
          const persistData = {
            groupIds: newIds,
            runFolder: state.selectedRunFolder,
            // eslint-disable-next-line @typescript-eslint/no-explicit-any
            presetId: (state as any)._currentPresetId
          }
          localStorage.setItem(SELECTED_GROUP_IDS_KEY, JSON.stringify(persistData))
        } catch (error) {
          console.error('[WorkflowStore] Failed to persist group selection:', error)
        }
      },

      // Set selected group IDs directly
      setSelectedGroupIds: (groupIds: string[]) => {
        const state = get()
        set({ selectedGroupIds: groupIds })

        // Persist to localStorage - save groupIds, runFolder, AND presetId
        // Note: startPoint is NOT persisted - it's calculated from progress
        try {
          const persistData = {
            groupIds: groupIds,
            runFolder: state.selectedRunFolder,
            // eslint-disable-next-line @typescript-eslint/no-explicit-any
            presetId: (state as any)._currentPresetId
          }
          localStorage.setItem(SELECTED_GROUP_IDS_KEY, JSON.stringify(persistData))
        } catch (error) {
          console.error('[WorkflowStore] Failed to persist group selection:', error)
        }
      },

      // Clear all selected group IDs
      clearSelectedGroupIds: () => {
        set({ selectedGroupIds: [] })

        // Clear from localStorage
        try {
          localStorage.removeItem(SELECTED_GROUP_IDS_KEY)
        } catch (error) {
          console.error('[WorkflowStore] Failed to clear group selection:', error)
        }
      },

      // Restore selection state from localStorage (called after API load completes)
      // Note: startPoint is NOT restored - it's calculated from progress via loadProgress
      restoreSelectionFromLocalStorage: () => {
        try {
          let hasChanges = false
          const updates: Partial<WorkflowStore> = {}
          const currentPresetId = get()._currentPresetId
          let canRestoreLegacyKeys = true

          // Restore selectedGroupIds and selectedRunFolder from combined storage
          const savedGroupData = localStorage.getItem(SELECTED_GROUP_IDS_KEY)
          if (savedGroupData) {
            const parsed = JSON.parse(savedGroupData)
            // Handle new format: { groupIds: [...], runFolder: "..." }
            if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
              const storedPresetId = typeof parsed.presetId === 'string' ? parsed.presetId : null
              const presetMatches = !currentPresetId || !storedPresetId || storedPresetId === currentPresetId
              canRestoreLegacyKeys = !storedPresetId

              if (presetMatches) {
                if (Array.isArray(parsed.groupIds) && parsed.groupIds.length > 0) {
                  updates.selectedGroupIds = parsed.groupIds
                  hasChanges = true
                }
                if (parsed.runFolder) {
                  updates.selectedRunFolder = parsed.runFolder
                  hasChanges = true
                }
              }
              // Note: startPoint is intentionally NOT restored - calculated from progress
            }
            // Handle old format: just array of groupIds (backward compatibility)
            else if (Array.isArray(parsed) && parsed.length > 0 && !currentPresetId) {
              updates.selectedGroupIds = parsed
              hasChanges = true
            }
          }

          // Also check separate runFolder key (backward compatibility)
          if (!updates.selectedRunFolder && canRestoreLegacyKeys && !currentPresetId) {
            const savedRunFolder = localStorage.getItem(SELECTED_RUN_FOLDER_KEY)
            if (savedRunFolder) {
              updates.selectedRunFolder = savedRunFolder
              hasChanges = true
            }
          }

          // Restore currentRunningGroupId
          const savedCurrentGroup = canRestoreLegacyKeys && !currentPresetId
            ? localStorage.getItem(CURRENT_RUNNING_GROUP_ID_KEY)
            : null
          if (savedCurrentGroup) {
            updates.currentRunningGroupId = savedCurrentGroup
            hasChanges = true
          }

          if (hasChanges) {
            set(updates)
          }
        } catch (error) {
          console.error('[WorkflowStore] Failed to restore selection from localStorage:', error)
        }
      },

      setCurrentRunningGroupId: (groupId: string | null) => {
        set({ currentRunningGroupId: groupId })

        // Persist to localStorage
        try {
          if (groupId) {
            localStorage.setItem(CURRENT_RUNNING_GROUP_ID_KEY, groupId)
          } else {
            localStorage.removeItem(CURRENT_RUNNING_GROUP_ID_KEY)
          }
        } catch (error) {
          console.error('[WorkflowStore] Failed to save currentRunningGroupId to localStorage:', error)
        }
      },

      setCurrentStepId: (stepId: string | null) => {
        // Only update if value actually changed (prevents unnecessary re-renders and canvas refocus)
        const current = get().currentStepId
        if (current !== stepId) {
          set({ currentStepId: stepId })
        }
      },

      setStepStatus: (stepId: string, status: 'pending' | 'running' | 'completed' | 'failed') => {
        // Only update if status actually changed (prevents unnecessary re-renders)
        const currentStatus = get().stepStatusMap.get(stepId)
        if (currentStatus === status) {
          return // No change, skip update
        }
        set(state => {
          const newMap = new Map(state.stepStatusMap)
          newMap.set(stepId, status)
          return { stepStatusMap: newMap }
        })
      },

      clearStepStatusMap: () => {
        set({ stepStatusMap: new Map() })
      },

      // Consolidated batch group switching handler
      // Handles group start: sets currentRunningGroupId, updates selectedRunFolder, and updates batchProgress
      handleBatchGroupStart: (groupId: string, runFolder: string, workspacePath?: string, groupIndex?: number, totalGroups?: number) => {
        const state = get()

        // Set current running group ID
        set({ currentRunningGroupId: groupId })

        // Persist currentRunningGroupId to localStorage
        try {
          localStorage.setItem(CURRENT_RUNNING_GROUP_ID_KEY, groupId)
        } catch (error) {
          console.error('[WorkflowStore] Failed to save currentRunningGroupId to localStorage:', error)
        }

        // Normalize and update selected run folder
        const normalizedFolder = normalizeRunFolder(runFolder, state.variablesManifest)
        set({ selectedRunFolder: normalizedFolder })

        // Persist selectedRunFolder to localStorage
        try {
          if (normalizedFolder) {
            localStorage.setItem(SELECTED_RUN_FOLDER_KEY, normalizedFolder)
          } else {
            localStorage.removeItem(SELECTED_RUN_FOLDER_KEY)
          }
        } catch (error) {
          console.error('[WorkflowStore] Failed to save selectedRunFolder to localStorage:', error)
        }

        // Update batch progress if index/total provided
        if (groupIndex !== undefined && totalGroups !== undefined) {
          // Initialize batch progress on first group or update existing
          if (!state.batchProgress?.isActive) {
            set({
              batchProgress: {
                isActive: true,
                totalGroups,
                currentGroupIndex: groupIndex,
                currentGroupId: groupId,
                completedCount: 0,
                failedCount: 0,
                remainingCount: totalGroups,
                startTime: Date.now()
              }
            })
          } else {
            set({
              batchProgress: {
                ...state.batchProgress,
                currentGroupIndex: groupIndex,
                currentGroupId: groupId,
                totalGroups // Update in case it changed
              }
            })
          }
        }

        console.log('[WorkflowStore] Batch group started:', {
          groupId,
          runFolder,
          normalizedFolder,
          workspacePath,
          groupIndex,
          totalGroups,
          batchProgress: get().batchProgress
        })

        // Reload run folders and progress if workspace path provided
        if (workspacePath) {
          state.loadRunFolders(workspacePath).catch(err => {
            console.warn('[WorkflowStore] Failed to reload run folders:', err)
          })

          if (normalizedFolder) {
            state.loadProgress(workspacePath, normalizedFolder).catch(err => {
              console.warn('[WorkflowStore] Failed to load progress:', err)
            })
          }
        }
      },

      // Consolidated batch group end handler
      // Clears currentRunningGroupId if it matches, and updates batch progress counts
      handleBatchGroupEnd: (groupId: string, success?: boolean, remainingGroups?: number) => {
        const state = get()

        // Only clear if this is the currently running group
        // This prevents clearing when events arrive out of order
        if (state.currentRunningGroupId === groupId) {
          set({ currentRunningGroupId: null })

          // Clear from localStorage
          try {
            localStorage.removeItem(CURRENT_RUNNING_GROUP_ID_KEY)
          } catch (error) {
            console.error('[WorkflowStore] Failed to clear currentRunningGroupId from localStorage:', error)
          }
        }

        // Update batch progress if we have active batch progress
        if (state.batchProgress && success !== undefined) {
          const newCompleted = success
            ? state.batchProgress.completedCount + 1
            : state.batchProgress.completedCount
          const newFailed = !success
            ? state.batchProgress.failedCount + 1
            : state.batchProgress.failedCount
          const remaining = remainingGroups ?? Math.max(0, state.batchProgress.remainingCount - 1)

          set({
            batchProgress: {
              ...state.batchProgress,
              completedCount: newCompleted,
              failedCount: newFailed,
              remainingCount: remaining,
              // Keep batch active if there are remaining groups
              isActive: remaining > 0,
              // Clear current group ID if batch is done
              currentGroupId: remaining > 0 ? state.batchProgress.currentGroupId : null
            }
          })
        }

        console.log('[WorkflowStore] Batch group ended:', {
          groupId,
          success,
          remainingGroups,
          batchProgress: get().batchProgress
        })
      },

      // Reset batch progress (called when batch completes or is canceled)
      resetBatchProgress: () => {
        set({ batchProgress: null })
      },

      setActivePhase: (phase: string | null) => {
        set({ activePhase: phase })
      },

      setShowChatArea: (show: boolean) => {
        set({ showChatArea: show })
      },

      setChatAreaExpanded: (expanded: boolean) => {
        set({ chatAreaExpanded: expanded })
      },

      setWorkflowWorkspaceView: (view: WorkflowWorkspaceView) => {
        set({
          workflowWorkspaceView: view,
          workflowWorkspaceSelectionTouched: view !== null
        })
      },

      setLayoutDirection: (direction: LayoutDirection) => {
        try {
          localStorage.setItem(LAYOUT_DIRECTION_KEY, direction)
        } catch (error) {
          console.error('[WorkflowStore] Failed to save layout direction to localStorage:', error)
        }
        set({ layoutDirection: direction })
      },

      setCanvasViewMode: (mode: CanvasViewMode) => {
        try {
          localStorage.setItem(CANVAS_VIEW_MODE_KEY, mode)
        } catch {
          // ignore
        }
        set({ canvasViewMode: mode })
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

      // Global step override
      setStepOverride: (override: AgentConfigs | null) => {
        set({ stepOverride: override })
      },

      // Workflow Mode Actions
      setWorkflowMode: (mode: 'plan' | 'eval' | 'output') => {
        const presetId = useGlobalPresetStore.getState().activePresetIds.workflow
        set(state => ({
          workflowMode: mode,
          workshopMode: mode === 'eval'
            ? 'eval'
            : mode === 'output'
              ? 'output'
              : (state.workshopMode === 'eval' || state.workshopMode === 'output' ? 'builder' : state.workshopMode),
          workshopModeByPreset: presetId
            ? {
                ...state.workshopModeByPreset,
                [presetId]: mode === 'eval'
                  ? 'eval'
                  : mode === 'output'
                    ? 'output'
                    : (state.workshopMode === 'eval' || state.workshopMode === 'output' ? 'builder' : state.workshopMode)
              }
            : state.workshopModeByPreset
        }))
      },

      setEvaluationPlan: (plan: EvaluationPlan | null) => {
        set({ evaluationPlan: plan })
      },

      loadEvaluationPlan: async (workspacePath: string) => {
        if (!workspacePath) {
          set({ evaluationPlan: null })
          return
        }

        set({ isLoadingEvaluationPlan: true })
        try {
          // Note: agentApi.getEvaluationPlan needs to be implemented or we use getPlannerFileContent
          // Assuming we use getPlannerFileContent for consistency with usePlanData pattern initially,
          // but eventually we should use a typed API if available. 
          // For now, we'll implement this logic in the hook useEvaluationPlanData 
          // and just store the result here, or call the API here.
          // Let's defer actual API call logic to the hook to keep store simpler, 
          // but we provide the action to update state.
          // Wait, the requirement says "loadEvaluationPlan: (workspacePath: string) => Promise<void>" in store.
          // Let's implement it using file content API for now as per `usePlanData`.
          
          const path = `${workspacePath}/evaluation/evaluation_plan.json`
          const response = await agentApi.getPlannerFileContent(path)
          
          if (response.success && response.data?.content) {
            const plan = JSON.parse(response.data.content) as EvaluationPlan
            set({ evaluationPlan: plan, isLoadingEvaluationPlan: false })
          } else {
            set({ evaluationPlan: null, isLoadingEvaluationPlan: false })
          }
        } catch (error) {
          console.error('[WorkflowStore] Failed to load evaluation plan:', error)
          set({ evaluationPlan: null, isLoadingEvaluationPlan: false })
        }
      },

      // Load saved settings from localStorage for a preset
      // Uses a single set() call to avoid multiple re-renders
      // Restores selectedRunFolder, selectedGroupIds, currentRunningGroupId from localStorage
      // Note: startPoint is NOT restored - it's calculated from progress via loadProgress
      loadSavedSettings: (presetId: string) => {
        if (!presetId) return

        try {
          // Load persisted values from localStorage
          let savedRunFolder: string | null = null
          let savedGroupIds: string[] = []
          let savedCurrentRunningGroupId: string | null = null
          let canRestoreLegacyKeys = false

          try {
            // Load from combined format first
            const groupDataStr = localStorage.getItem(SELECTED_GROUP_IDS_KEY)
            if (groupDataStr) {
              const parsed = JSON.parse(groupDataStr)
              // Handle new format: { groupIds: [...], runFolder: "..." }
              if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
                const storedPresetId = typeof parsed.presetId === 'string' ? parsed.presetId : null
                const presetMatches = !storedPresetId || storedPresetId === presetId
                canRestoreLegacyKeys = !storedPresetId

                if (presetMatches) {
                  if (Array.isArray(parsed.groupIds)) {
                    savedGroupIds = parsed.groupIds
                  }
                  if (parsed.runFolder) {
                    savedRunFolder = parsed.runFolder
                  }
                }
                // Note: startPoint is intentionally NOT restored - calculated from progress
              }
              // Handle old format: just array of groupIds
              else if (Array.isArray(parsed)) {
                canRestoreLegacyKeys = true
                savedGroupIds = parsed
              }
            }

            // Fallback to separate runFolder key if not in combined format
            if (!savedRunFolder && canRestoreLegacyKeys) {
              const runFolderStr = localStorage.getItem(SELECTED_RUN_FOLDER_KEY)
              if (runFolderStr) {
                savedRunFolder = runFolderStr
              }
            }

            const currentGroupStr = canRestoreLegacyKeys
              ? localStorage.getItem(CURRENT_RUNNING_GROUP_ID_KEY)
              : null
            if (currentGroupStr) {
              savedCurrentRunningGroupId = currentGroupStr
            }
          } catch (error) {
            console.error('[WorkflowStore] Failed to load from localStorage:', error)
          }

          // Build state update object - restore from localStorage or use defaults
          // startPoint is always 0 - will be calculated from progress via loadProgress
          // NOTE: activePhase is NOT reset here — it's saved/restored by switchToPreset's
          // per-preset snapshot mechanism. Resetting it here would clobber the restored phase.
          const stateUpdate: Partial<WorkflowStore> = {
            selectedRunFolder: savedRunFolder,
            selectedStartPoint: 0,  // Always start fresh - calculated from progress
            selectedBranchStep: null,
            selectedGroupIds: savedGroupIds,
            currentRunningGroupId: savedCurrentRunningGroupId,
          }

          // Single set() call to avoid multiple re-renders
          set(stateUpdate)
        } catch (error) {
          console.error('[WorkflowStore] Failed to load saved settings:', error)
        }
      },

      // Save current settings to localStorage for a preset
      // NOTE: No longer saves settings to localStorage - removed all persistence
      saveSettings: () => {
        // No-op: Settings are no longer persisted
      },

      // Switch to a new preset - combines reset and load into a single set() call
      // This avoids multiple re-renders that would occur with separate reset + load calls
      switchToPreset: (presetId: string) => {
        if (!presetId) return

        // Check if localStorage has data for the same preset (page reload scenario)
        let storedPresetId: string | null = null
        try {
          const groupDataStr = localStorage.getItem(SELECTED_GROUP_IDS_KEY)
          if (groupDataStr) {
            const parsed = JSON.parse(groupDataStr)
            if (parsed && typeof parsed === 'object' && parsed.presetId) {
              storedPresetId = parsed.presetId
            }
          }
        } catch {
          // Ignore parse errors
        }

        const isSamePreset = storedPresetId === presetId

        try {
          const currentState = get()
          const oldPresetId = currentState._currentPresetId

          // If already on this preset, skip — avoids resetting state when
          // WorkflowModeHandler re-applies the same preset on mount/effect
          if (oldPresetId === presetId) {
            return
          }

          console.log(`%c[WorkflowStore] switchToPreset: ${oldPresetId?.slice(0,8)} → ${presetId?.slice(0,8)}`, 'color: #2196F3; font-weight: bold')
          console.time(`[WorkflowStore] switchToPreset-${presetId?.slice(0,8)}`)

          // Save current flat state into the old preset's slot before switching
          // This preserves all execution state (progress, groups, phase, etc.) so it's
          // restored when the user switches back to this workflow.
          if (oldPresetId && oldPresetId !== presetId) {
            const snapshot = snapshotPresetState(currentState)
            set((state) => ({
              _presetStates: { ...state._presetStates, [oldPresetId]: snapshot }
            }))
          }

          // If switching to the SAME preset (page reload), preserve localStorage
          if (isSamePreset) {
            // Just reset workflow context but preserve selection - localStorage will be restored by loadSavedSettings
            set({
              runFolders: [],
              stepProgress: null,
              variablesManifest: null,
              currentStepId: null,
              stepStatusMap: new Map(),
              batchProgress: null,
              workflowChatTabs: {},
              activeWorkflowTabId: null,
              workflowWorkspaceView: null,
              workflowWorkspaceSelectionTouched: false,
              _currentPresetId: presetId
            } as Partial<WorkflowStore>)
            return
          }

          // Restore saved state for the new preset, or use defaults
          const savedState = currentState._presetStates[presetId]
          const restored = savedState ?? createDefaultPresetState()

          // Apply restored per-preset state to flat fields + reset API-loaded context
          set({
            // Reset workflow context (loaded from API, not per-preset)
            runFolders: [],
            variablesManifest: null,
            // Restore per-preset state
            selectedRunFolder: restored.selectedRunFolder,
            stepProgress: restored.stepProgress,
            stepProgressFolder: restored.stepProgressFolder,
            activePhase: restored.activePhase,
            showChatArea: restored.showChatArea,
            chatAreaExpanded: restored.chatAreaExpanded,
            workflowWorkspaceView: restored.workflowWorkspaceView,
            workflowWorkspaceSelectionTouched: restored.workflowWorkspaceSelectionTouched,
            workflowChatTabs: restored.workflowChatTabs,
            activeWorkflowTabId: restored.activeWorkflowTabId,
            selectedGroupIds: restored.selectedGroupIds,
            currentRunningGroupId: restored.currentRunningGroupId,
            currentStepId: restored.currentStepId,
            stepStatusMap: new Map(restored.stepStatusMap),
            batchProgress: restored.batchProgress,
            selectedStartPoint: restored.selectedStartPoint,
            selectedBranchStep: restored.selectedBranchStep,
            _currentPresetId: presetId
          } as Partial<WorkflowStore>)

          // Only clear localStorage when there's no saved state (first time visiting this preset)
          if (!savedState) {
            try {
              localStorage.removeItem(SELECTED_GROUP_IDS_KEY)
              localStorage.removeItem(CURRENT_RUNNING_GROUP_ID_KEY)
              localStorage.removeItem(SELECTED_RUN_FOLDER_KEY)
            } catch (error) {
              console.error('[WorkflowStore] Failed to clear group localStorage on preset switch:', error)
            }
          }
          console.timeEnd(`[WorkflowStore] switchToPreset-${presetId?.slice(0,8)}`)
        } catch (error) {
          console.error('[WorkflowStore] Failed to switch preset:', error)
        }
      },

      // Reset execution state (called when switching workflows)
      resetExecutionState: () => {
        // Note: We don't clear tempOverrideLLM here because it's a global setting
        // that should persist across workflow switches
        // Note: We don't reset showChatArea or activePhase - these persist or are loaded per-preset

        set({
          runFolders: [],
          selectedRunFolder: 'new',
          stepProgress: null,
          selectedStartPoint: 0,
          selectedBranchStep: null,
          variablesManifest: null,
          selectedGroupIds: [],
          currentRunningGroupId: null,
          currentStepId: null,
          stepStatusMap: new Map(),
          batchProgress: null,
          workflowWorkspaceView: null,
          workflowWorkspaceSelectionTouched: false,
          // activePhase is saved/loaded per-preset, don't reset here
          workflowChatTabs: {},
          activeWorkflowTabId: null
        })

        // Clear group-related localStorage when resetting execution state
        try {
          localStorage.removeItem(SELECTED_GROUP_IDS_KEY)
          localStorage.removeItem(CURRENT_RUNNING_GROUP_ID_KEY)
          localStorage.removeItem(SELECTED_RUN_FOLDER_KEY)
        } catch (error) {
          console.error('[WorkflowStore] Failed to clear group localStorage on reset:', error)
        }
      },

    })
)

// Selector hooks for common patterns
export const useWorkflowPhases = () => useWorkflowStore(state => state.phases)
export const useWorkflowPhasesLoading = () => useWorkflowStore(state => state.isLoadingPhases)
export const useWorkflowRunFolders = () => useWorkflowStore(state => state.runFolders)
export const useWorkflowProgress = () => useWorkflowStore(state => state.stepProgress)
export const useCompletedStepIndices = () => useWorkflowStore(state => state.stepProgress?.completed_step_indices || [])

// Legacy selector removed - use selectedGroupIds array instead
// Group selection is now exclusively managed via checkbox multi-select (selectedGroupIds)
