import React, { useEffect, useRef, useMemo, useCallback, useState } from 'react'
import { createPortal } from 'react-dom'
import { useShallow } from 'zustand/react/shallow'
import {
  Play,
  Square,
  Plus,
  Loader2,
  ChevronDown,
  ChevronRight,
  Check,
  Rocket,
  FolderOpen,
  RefreshCw,
  BookOpen,
  Trash2,
  Settings,
  SlidersHorizontal,
  X,
  Brain,
  MessageSquare,
  Circle,
  CheckSquare,
  Save,
  RotateCcw,
  FileText,
  BarChart3,
  DollarSign,
  ArrowRight,
  ArrowDown,
  Package,
} from 'lucide-react'
import { useWorkspaceStore } from '../../../stores/useWorkspaceStore'
import { useWorkflowStore, type RunFolder } from '../../../stores/useWorkflowStore'
import { useChatStore } from '../../../stores/useChatStore'
import type { WorkflowPhase, StepProgress, VariablesManifest } from '../../../services/api-types'
import type { PlanningResponse } from '../../../utils/stepConfigMatching'
import type { WorkflowExecutionStatus } from '../hooks/useWorkflowExecution'
import type { ExecutionOptions } from '../../../services/api-types'
import { agentApi } from '../../../services/api'
import ConfirmationDialog from '../../ui/ConfirmationDialog'
import LLMOverrideModal from '../LLMOverrideModal'
import BulkStepConfigModal from '../BulkStepConfigModal'
import { useCommandDialogStore } from '../../../stores/useCommandDialogStore'
import LearningsPopup from '../LearningsPopup'
import ExecutionLogsPopup from '../ExecutionLogsPopup'
import EvaluationPopup from '../EvaluationPopup'
import CostsPopup from '../CostsPopup'
import WorkflowVersionsPopup from '../WorkflowVersionsPopup'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../../ui/tooltip'
import type { PlanStep, AgentConfigs } from '../../../utils/stepConfigMatching'
import { isConditionalStep } from '../../../utils/stepConfigMatching'
import { sanitizeDisplayNameForFolder, resolveGroupFolderPath } from '../../../utils/workflowUtils'

// Execution phase ID - special phase that should be displayed separately
const EXECUTION_PHASE_ID = 'execution'
const EVAL_EXECUTION_PHASE_ID = 'evaluation-execution'


// Start Point options - where to start execution
type StartPointType = 'from_beginning' | 'resume' | 'single_step' | 'resume_branch'
interface StartPointOption {  
  id: StartPointType
  stepNumber?: number  // For resume/single_step, which step to target (1-based)
  branchStep?: {  // For resume_branch
    parentStepIndex: number;  // 0-based index of conditional step
    branchType: 'if_true' | 'if_false';  // Which branch
    branchStepIndex: number;  // 0-based index within the branch
  };
  label: string
  icon: typeof Play
  description: string
}

interface WorkflowToolbarProps {
  status: WorkflowExecutionStatus
  hasPlan: boolean
  plan?: PlanningResponse | null  // Plan data for identifying conditional steps and branches
  currentPhase?: string
  workspacePath?: string | null
  totalSteps?: number
  presetQueryId?: string | null  // Used to persist settings per workflow
  // API data passed as props (avoids store subscription issues)
  runFolders: RunFolder[]
  variablesManifest: VariablesManifest | null
  stepProgress: StepProgress | null
  isLoadingWorkspaceState?: boolean  // Whether workspace state (iterations, manifest) is loading
  onStartPhase: (phaseId: string, executionOptions?: ExecutionOptions) => void
  onStop: () => void
  onCreatePlan: () => void
  showChatArea?: boolean
  onToggleChatArea?: () => void
  onBulkUpdateSteps?: (updates: Array<{ stepId: string; updates: Partial<PlanStep> }>) => Promise<void>  // Bulk update function
  stepOverride?: AgentConfigs | null  // Global step override config
  onSaveStepOverride?: (agentConfigs: AgentConfigs | null) => Promise<void>  // Save global step override
  onRefresh?: () => Promise<void>  // Refresh plan and variables
  onSaveLayout?: () => Promise<void>  // Save workflow layout
  onDeleteLayout?: () => Promise<void>  // Delete workflow layout and reset to default
  hasUnsavedLayoutChanges?: boolean  // Whether there are unsaved layout changes
  isSavingLayout?: boolean  // Whether layout is currently being saved
  isDeletingLayout?: boolean  // Whether layout is currently being deleted
  selectedStepIds?: string[]  // IDs of currently selected steps (shows indicator when 2+ selected)
  className?: string
}

export const WorkflowToolbar: React.FC<WorkflowToolbarProps> = ({
  status,
  hasPlan,
  plan,
  currentPhase,
  workspacePath,
  totalSteps = 0,
  presetQueryId,
  runFolders,
  variablesManifest,
  stepProgress,
  isLoadingWorkspaceState = false,
  onStartPhase,
  onStop,
  onCreatePlan,
  showChatArea = false,
  onToggleChatArea,
  onBulkUpdateSteps,
  stepOverride,
  onSaveStepOverride,
  onRefresh,
  onSaveLayout,
  onDeleteLayout,
  hasUnsavedLayoutChanges = false,
  isSavingLayout = false,
  isDeletingLayout = false,
  selectedStepIds,
  className = ''
}) => {
  // Normalize runFolders to avoid repeated null checks throughout the component
  const folders = useMemo(() => runFolders ?? [], [runFolders])

  // Workspace store for opening folders
  const fetchFiles = useWorkspaceStore(state => state.fetchFiles)

  // Workflow store - use useShallow to prevent unnecessary re-renders
  // Note: runFolders, variablesManifest, stepProgress come from props (passed from WorkflowCanvas)
  const {
    phases,
    isLoadingPhases,
    phasesInitialized,
    selectedRunFolder,
    selectedStartPoint,
    selectedBranchStep,
    tempOverrideLLM,
    tempOverrideLLM2,
    tempOverrideLLMEnabled,
    tempLearningLLM,
    selectedGroupIds,
    currentRunningGroupId,
    loadPhases,
    loadRunFolders,
    setSelectedRunFolder,
    loadProgress,
    loadFolderProgressOnDemand,
    setStartPoint,
    setBranchStep,
    buildExecutionOptions,
    loadSavedSettings,
    setTempOverrideLLMEnabled,
    clearTempOverrideLLM,
    clearTempOverrideLLM2,
    clearTempLearningLLM,
    toggleGroupSelection,
    setSelectedGroupIds,
    clearSelectedGroupIds,
    restoreSelectionFromLocalStorage,
    workflowMode,
    setWorkflowMode,
    layoutDirection,
    setLayoutDirection
  } = useWorkflowStore(useShallow(state => ({
    phases: state.phases,
    isLoadingPhases: state.isLoadingPhases,
    phasesInitialized: state.phasesInitialized,
    selectedRunFolder: state.selectedRunFolder,
    selectedExecutionMode: state.selectedExecutionMode,
    selectedStartPoint: state.selectedStartPoint,
    selectedBranchStep: state.selectedBranchStep,
    tempOverrideLLM: state.tempOverrideLLM,
    tempOverrideLLM2: state.tempOverrideLLM2,
    tempOverrideLLMEnabled: state.tempOverrideLLMEnabled,
    tempLearningLLM: state.tempLearningLLM,
    selectedGroupIds: state.selectedGroupIds,
    currentRunningGroupId: state.currentRunningGroupId,
    loadPhases: state.loadPhases,
    loadRunFolders: state.loadRunFolders,
    setSelectedRunFolder: state.setSelectedRunFolder,
    loadProgress: state.loadProgress,
    loadFolderProgressOnDemand: state.loadFolderProgressOnDemand,
    setStartPoint: state.setStartPoint,
    setBranchStep: state.setBranchStep,
    buildExecutionOptions: state.buildExecutionOptions,
    loadSavedSettings: state.loadSavedSettings,
    setTempOverrideLLMEnabled: state.setTempOverrideLLMEnabled,
    clearTempOverrideLLM: state.clearTempOverrideLLM,
    clearTempOverrideLLM2: state.clearTempOverrideLLM2,
    clearTempLearningLLM: state.clearTempLearningLLM,
    toggleGroupSelection: state.toggleGroupSelection,
    setSelectedGroupIds: state.setSelectedGroupIds,
    clearSelectedGroupIds: state.clearSelectedGroupIds,
    restoreSelectionFromLocalStorage: state.restoreSelectionFromLocalStorage,
    workflowMode: state.workflowMode,
    setWorkflowMode: state.setWorkflowMode,
    layoutDirection: state.layoutDirection,
    setLayoutDirection: state.setLayoutDirection
  })))

  // Reset start point when switching to eval mode
  useEffect(() => {
    if (workflowMode === 'eval' && selectedStartPoint !== 0) {
      setStartPoint(0)
    }
  }, [workflowMode, selectedStartPoint, setStartPoint])

  // Calculate the best run folder to use for popups (context-aware)
  // Priority: currentRunningGroupId > selectedRunFolder (if group path) > first selectedGroupIds
  const contextRunFolder = useMemo(() => {
    const resolved = resolveGroupFolderPath({
      currentRunningGroupId,
      selectedRunFolder,
      selectedGroupIds,
      manifest: variablesManifest
    })
    return resolved || selectedRunFolder
  }, [currentRunningGroupId, selectedRunFolder, selectedGroupIds, variablesManifest])
  
  // Memoize runFolders array to prevent unnecessary re-renders in popups
  const runFoldersNames = useMemo(() => {
    return folders.map(rf => rf.name)
  }, [folders])
  
  // LLM Override modal state
  const [showLLMOverrideModal, setShowLLMOverrideModal] = useState(false)
  
  
  // Bulk Step Config modal state
  const [showBulkStepConfigModal, setShowBulkStepConfigModal] = useState(false)
  
  // Learnings popup state
  const [showLearningsPopup, setShowLearningsPopup] = useState(false)

  // Execution logs popup state
  const [showExecutionLogsPopup, setShowExecutionLogsPopup] = useState(false)

  // Costs popup state
  const [showCostsPopup, setShowCostsPopup] = useState(false)

  // Evaluation popup state
  const [showEvaluationPopup, setShowEvaluationPopup] = useState(false)

  // Versions popup state
  const [showVersionsPopup, setShowVersionsPopup] = useState(false)
  
  // Close popups when workspacePath changes (switching workflows)
  // Use a ref to track previous workspacePath to avoid closing on initial mount
  const prevWorkspacePathRef = useRef<string | null | undefined>(workspacePath)
  useEffect(() => {
    // Only close if workspacePath actually changed (not on initial mount)
    if (prevWorkspacePathRef.current !== undefined && prevWorkspacePathRef.current !== workspacePath) {
      setShowLearningsPopup(false)
      setShowExecutionLogsPopup(false)
      setShowCostsPopup(false)
      setShowEvaluationPopup(false)
      setShowVersionsPopup(false)
    }
    prevWorkspacePathRef.current = workspacePath
  }, [workspacePath]) // Only depend on workspacePath - popup states are only read, not dependencies
  
  // Local UI state (dropdowns)
  const [isDropdownOpen, setIsDropdownOpen] = React.useState(false)
  const [isIterationDropdownOpen, setIsIterationDropdownOpen] = React.useState(false)
  const [isStartPointDropdownOpen, setIsStartPointDropdownOpen] = React.useState(false)
  const [isCreatingIteration, setIsCreatingIteration] = React.useState(false)
  
  // Delete confirmation dialog state
  const [deleteDialog, setDeleteDialog] = React.useState<{
    isOpen: boolean
    folderName: string | null
    isLoading: boolean
  }>({
    isOpen: false,
    folderName: null,
    isLoading: false
  })
  
  
  // State for expanded iterations (only show groups when expanded)
  const [expandedIterations, setExpandedIterations] = React.useState<Set<string>>(new Set())
  
  // Refs for dropdown click-outside detection
  const dropdownRef = useRef<HTMLDivElement>(null)
  const iterationDropdownRef = useRef<HTMLDivElement>(null)
  const startPointDropdownRef = useRef<HTMLDivElement>(null)
  const dropdownButtonRef = useRef<HTMLButtonElement>(null)
  const iterationDropdownButtonRef = useRef<HTMLButtonElement>(null)
  const startPointButtonRef = useRef<HTMLButtonElement>(null)
  
  // State for dropdown positions (for portal rendering)
  const [dropdownPosition, setDropdownPosition] = useState<{ top: number; left: number } | null>(null)
  const [iterationDropdownPosition, setIterationDropdownPosition] = useState<{ top: number; left: number } | null>(null)
  const [startPointDropdownPosition, setStartPointDropdownPosition] = useState<{ top: number; left: number } | null>(null)
  
  // Keep isRunning for other uses (like dropdown disabled state)
  const isRunning = status === 'running'
  
  // Determine target execution phase ID based on mode
  const targetExecutionPhaseId = workflowMode === 'eval' ? EVAL_EXECUTION_PHASE_ID : EXECUTION_PHASE_ID
  
  // Check if execution phase specifically is running (not just any phase)
  // Use a selector that only recalculates when chatTabs, pollingInterval, or sseConnections change
  const isExecutionRunning = useChatStore(state => {
    const chatTabs = state.chatTabs
    const pollingInterval = state.pollingInterval
    const sseConnections = state.sseConnections
    const allTabs = Object.values(chatTabs)

    try {
      // Filter for execution phase tabs belonging to the current preset
      const executionTabs = allTabs.filter(tab =>
        tab.metadata?.mode === 'workflow' &&
        tab.metadata?.phaseId === targetExecutionPhaseId &&
        tab.metadata?.presetQueryId === presetQueryId
      )

      // Check if any execution tab is streaming
      return executionTabs.some(tab => {
        // If tab is completed, it's not streaming
        if (tab.isCompleted) return false

        // Tab is streaming if there's an active connection (SSE or polling) and tab is not manually paused
        const hasActiveConnection = pollingInterval !== null
          || (tab.sessionId != null && sseConnections[tab.sessionId] != null)
        if (hasActiveConnection) {
          return tab.isStreaming !== false // Respect manual pause
        }

        // Also show Stop if tab.isStreaming is explicitly true (set immediately on query submit,
        // before SSE/polling connects)
        return tab.isStreaming === true
      })
    } catch (error) {
      console.error('[WorkflowToolbar] Error checking execution phase status:', error)
      return false
    }
  }) // Zustand will handle memoization - only re-render if result changes

  // Load phases on mount (store handles deduplication)
  // Only load if not already initialized
  useEffect(() => {
    if (!phasesInitialized && !isLoadingPhases) {
      loadPhases()
    }
  }, [loadPhases, phasesInitialized, isLoadingPhases])

  // Load saved settings when preset changes
  useEffect(() => {
    if (presetQueryId) {
      loadSavedSettings(presetQueryId)
    }
  }, [presetQueryId, loadSavedSettings])

  // Restore selection from localStorage after workspace state finishes loading
  // This ensures localStorage values are restored AFTER all API data is loaded
  const hasRestoredRef = useRef(false)
  useEffect(() => {
    // Only restore once when workspace loading completes and manifest is available
    if (!isLoadingWorkspaceState && variablesManifest && !hasRestoredRef.current) {
      restoreSelectionFromLocalStorage()
      hasRestoredRef.current = true

      // After restoring, load progress for the restored run folder
      const restoredRunFolder = useWorkflowStore.getState().selectedRunFolder
      if (restoredRunFolder && workspacePath) {
        loadProgress(workspacePath, restoredRunFolder)
      }
    }
    // Reset the flag when workspace starts loading (preset change)
    if (isLoadingWorkspaceState) {
      hasRestoredRef.current = false
    }
  }, [isLoadingWorkspaceState, variablesManifest, restoreSelectionFromLocalStorage, workspacePath, loadProgress])

  // Restore selectedGroupIds from execution state when page refreshes during execution
  // This handles the case where execution is running but selectedGroupIds was lost on page refresh
  useEffect(() => {
    if (isExecutionRunning && selectedGroupIds.length === 0 && currentRunningGroupId) {
      // If execution is running but no groups are selected, restore from currentRunningGroupId
      console.log('[WorkflowToolbar] Restoring selectedGroupIds from currentRunningGroupId:', currentRunningGroupId)
      setSelectedGroupIds([currentRunningGroupId])
    } else if (isExecutionRunning && selectedGroupIds.length === 0 && variablesManifest?.groups) {
      // If we have groups in manifest but none selected, try to infer from selectedRunFolder
      // Extract group ID from selectedRunFolder if it's a group path
      if (selectedRunFolder && selectedRunFolder.includes('/')) {
        const parts = selectedRunFolder.split('/')
        if (parts.length === 2) {
          const groupFolderName = parts[1]
          // Try to find matching group in manifest
          const matchingGroup = variablesManifest.groups.find(g => {
            if (g.group_id === groupFolderName) return true
            if (g.display_name) {
              const sanitized = groupFolderName.toLowerCase().replace(/[^a-z0-9-]/g, '-').replace(/-+/g, '-').trim()
              const groupSanitized = g.display_name.toLowerCase().replace(/[^a-z0-9-]/g, '-').replace(/-+/g, '-').trim()
              return sanitized === groupSanitized
            }
            return false
          })
          if (matchingGroup) {
            console.log('[WorkflowToolbar] Restoring selectedGroupIds from selectedRunFolder:', matchingGroup.group_id)
            setSelectedGroupIds([matchingGroup.group_id])
          }
        }
      }
    }
  }, [isExecutionRunning, selectedGroupIds.length, currentRunningGroupId, variablesManifest, selectedRunFolder, setSelectedGroupIds])

  // selectedGroupIds is already included in the batched selector above
  
  // Settings are no longer persisted to localStorage - removed save logic

  // NOTE: loadRunFolders and loadProgress are NOT called here anymore.
  // useWorkspaceState in WorkflowCanvas handles initial load of:
  // - run_folders (via setRunFolders)
  // - variables_manifest (via setVariablesManifest)
  // - selected_progress (via setStepProgress)
  // This eliminates duplicate API calls on initial page load.

  // Close dropdowns when clicking outside
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      const target = event.target as Node
      
      // Check first dropdown (phase selector)
      const phaseDropdown = document.querySelector('[data-phase-dropdown]')
      const clickedPhaseButton = dropdownButtonRef.current?.contains(target)
      const clickedPhaseDropdown = phaseDropdown?.contains(target)
      if (!clickedPhaseButton && !clickedPhaseDropdown) {
        setIsDropdownOpen(false)
      }
      
      // Check iteration dropdown
      const iterationDropdown = document.querySelector('[data-iteration-dropdown]')
      const clickedIterationButton = iterationDropdownButtonRef.current?.contains(target)
      const clickedIterationDropdown = iterationDropdown?.contains(target)
      if (!clickedIterationButton && !clickedIterationDropdown) {
        setIsIterationDropdownOpen(false)
      }
      
      // Check start point dropdown
      const startPointDropdown = document.querySelector('[data-start-point-dropdown]')
      const clickedStartPointButton = startPointButtonRef.current?.contains(target)
      const clickedStartPointDropdown = startPointDropdown?.contains(target)
      if (!clickedStartPointButton && !clickedStartPointDropdown) {
        setIsStartPointDropdownOpen(false)
      }
    }
    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [])
  
  // Calculate dropdown positions when they open
  useEffect(() => {
    if (isDropdownOpen && dropdownButtonRef.current) {
      const rect = dropdownButtonRef.current.getBoundingClientRect()
      setDropdownPosition({
        top: rect.bottom + 4, // mt-1 = 4px
        left: rect.left
      })
    } else {
      setDropdownPosition(null)
    }
  }, [isDropdownOpen])
  
  useEffect(() => {
    if (isIterationDropdownOpen && iterationDropdownButtonRef.current) {
      const rect = iterationDropdownButtonRef.current.getBoundingClientRect()
      setIterationDropdownPosition({
        top: rect.bottom + 4, // mt-1 = 4px
        left: rect.left
      })
    } else {
      setIterationDropdownPosition(null)
    }
  }, [isIterationDropdownOpen])
  
  useEffect(() => {
    if (isStartPointDropdownOpen && startPointButtonRef.current) {
      const rect = startPointButtonRef.current.getBoundingClientRect()
      setStartPointDropdownPosition({
        top: rect.bottom + 4, // mt-1 = 4px
        left: rect.left
      })
    } else {
      setStartPointDropdownPosition(null)
    }
  }, [isStartPointDropdownOpen])

  // Separate phases based on mode
  // Only calculate when phases are actually loaded (not empty and not loading)
  const { otherPhases, visiblePhases } = useMemo(() => {
    // Don't calculate if phases aren't loaded yet
    if (isLoadingPhases || phases.length === 0) {
      return {
        executionPhase: undefined,
        evalPhases: [],
        planPhases: [],
        otherPhases: [],
        visiblePhases: []
      }
    }

    const evalPhaseIds = ['evaluation-builder', 'evaluation-execution']
    // Planning and execution are always available in plan mode, but execution is handled separately in UI
    const planPhaseIds = ['planning'] 

    const executionPhase = phases.find((p: WorkflowPhase) => p.id === EXECUTION_PHASE_ID)
    const evalPhases = phases.filter((p: WorkflowPhase) => evalPhaseIds.includes(p.id))
    const planPhases = phases.filter((p: WorkflowPhase) => planPhaseIds.includes(p.id))
    
    // Other phases are those not in eval list and not execution
    const otherPhases = phases.filter((p: WorkflowPhase) => 
      !evalPhaseIds.includes(p.id) && 
      p.id !== EXECUTION_PHASE_ID
    )

    // Determine which phases to show in dropdown based on mode
    let visiblePhases: WorkflowPhase[] = []
    if (workflowMode === 'eval') {
      // In Eval mode: Show Eval phases
      visiblePhases = evalPhases
    } else {
      // In Plan mode: Show Planning phases + Others (excluding execution which has its own button)
      // Filter out eval phases from plan mode dropdown to keep it clean
      visiblePhases = otherPhases.filter(p => !evalPhaseIds.includes(p.id))
    }

    return {
      executionPhase,
      evalPhases,
      planPhases,
      otherPhases,
      visiblePhases
    }
  }, [phases, isLoadingPhases, workflowMode])

  // Close dropdown if phases become unavailable
  useEffect(() => {
    if (isDropdownOpen && (isLoadingPhases || visiblePhases.length === 0)) {
      setIsDropdownOpen(false)
    }
  }, [isDropdownOpen, isLoadingPhases, visiblePhases.length])

  // Calculate progress info - use ref to track previous value and prevent infinite loops
  const completedStepIndicesRef = useRef<number[]>([])
  const stepProgressDataRef = useRef<string>('')
  const completedStepIndicesDepsRef = useRef<{ indices: number[] | undefined, totalSteps: number }>({ indices: undefined, totalSteps: 0 })
  
  const completedStepIndices = useMemo(() => {
    // Extract the array inside useMemo to avoid dependency issues
    const indices = stepProgress?.completed_step_indices || []
    const sorted = indices.slice().sort((a, b) => a - b)
    
    // Track dependency changes
    const prevDeps = completedStepIndicesDepsRef.current
    const depsChanged = prevDeps.indices !== indices || prevDeps.totalSteps !== totalSteps
    if (depsChanged) {
      completedStepIndicesDepsRef.current = { indices, totalSteps }
    }
    
    // Create a stable string representation of the data
    const dataStr = JSON.stringify(sorted) + String(totalSteps)
    
    // Only update if data actually changed
    if (stepProgressDataRef.current !== dataStr) {
      stepProgressDataRef.current = dataStr
      completedStepIndicesRef.current = sorted
    }
    
    return completedStepIndicesRef.current
  }, [stepProgress?.completed_step_indices, totalSteps]) // Only depend on the array, not the whole object
  
  const hasExistingProgress = stepProgress !== null && completedStepIndices.length > 0
  const completedStepCount = completedStepIndices.length


  // Helper to format the selected run folder display text
  const getSelectedRunFolderDisplay = useMemo(() => {
    // If no groups are selected via checkboxes, show "--Select--"
    if (selectedGroupIds.length === 0) {
      // Only show a specific folder if it's a direct group path (user clicked on a specific group)
      const isGroupPath = selectedRunFolder && selectedRunFolder.includes('/') && selectedRunFolder.split('/').length === 2
      if (isGroupPath) {
        // Extract iteration and group folder name
        const parts = selectedRunFolder.split('/')
        const iteration = parts[0] // e.g., "iteration-14"
        const groupFolderName = parts[1] // e.g., "group-1" or "siddharth"
        
        // Find the group in manifest to get display name
        if (variablesManifest?.groups) {
          const group = variablesManifest.groups.find(g => {
            // Check if group_id matches or if display_name (sanitized) matches
            if (groupFolderName.startsWith('group-')) {
              return g.group_id === groupFolderName
            } else {
              // Display name - need to match sanitized version
              const sanitizedDisplayName = groupFolderName.toLowerCase().replace(/[^a-z0-9-]/g, '-').replace(/-+/g, '-').trim()
              const groupSanitized = g.display_name?.toLowerCase().replace(/[^a-z0-9-]/g, '-').replace(/-+/g, '-').trim()
              return groupSanitized === sanitizedDisplayName
            }
          })
          
          if (group && group.display_name) {
            // Show full path with original display name: "iteration-14/Siddharth"
            return `${iteration}/${group.display_name}`
          } else if (group) {
            // No display name, use group_id: "iteration-14/group-1"
            return `${iteration}/${group.group_id}`
          }
        }
        
        // Fallback: show the full path as-is
        return selectedRunFolder
      }
      
      // No groups selected and not a specific group path - show "--Select--"
      return '--Select--'
    }
    
    // Groups are selected via checkboxes - show them
    if (selectedGroupIds.length > 0 && variablesManifest?.groups) {
      // Find selected groups from manifest
      const selectedGroups = variablesManifest.groups.filter(g => selectedGroupIds.includes(g.group_id))
      if (selectedGroups.length > 0) {
        // Show display names or group_ids of selected groups
        const groupNames = selectedGroups.map(g => g.display_name || g.group_id)
        
        // Extract iteration from selectedRunFolder (could be "iteration-5" or "iteration-5/group-1")
        let iteration: string | null = null
        if (selectedRunFolder && selectedRunFolder !== 'new') {
          if (selectedRunFolder.includes('/')) {
            // It's a group path - extract iteration
            iteration = selectedRunFolder.split('/')[0]
          } else {
            // It's just an iteration folder
            iteration = selectedRunFolder
          }
        }
        
        if (groupNames.length === 1) {
          // Single group selected - show with iteration if available
          if (iteration) {
            return `${iteration}/${groupNames[0]}`
          }
          return groupNames[0]
        } else if (groupNames.length <= 3) {
          // Multiple groups - show with iteration prefix if available
          if (iteration) {
            return `${iteration}: ${groupNames.join(', ')}`
          }
          return groupNames.join(', ')
        } else {
          // Many groups - show with iteration prefix if available
          if (iteration) {
            return `${iteration}: ${groupNames.slice(0, 2).join(', ')} +${groupNames.length - 2}`
          }
          return `${groupNames.slice(0, 2).join(', ')} +${groupNames.length - 2}`
        }
      }
    }
    
    // Fallback
    if (!selectedRunFolder) {
      return '--Select--'
    }
    
    return selectedRunFolder
  }, [selectedRunFolder, selectedGroupIds, variablesManifest])

  // Build merged list of iterations and groups
  // Groups from variablesManifest are PRIMARY - runFolders only indicate if groups have run
  const iterationGroups = useMemo(() => {
    console.log('[WorkflowToolbar] Building iterationGroups:', {
      hasManifest: !!variablesManifest,
      groupsCount: variablesManifest?.groups?.length || 0,
      runFoldersCount: folders.length,
      runFolderNames: folders.map(f => f.name)
    })

    interface GroupItem {
      id: string  // Full path like "iteration-1/group-5" or just "iteration-1"
      name: string  // Display name like "group-5" or "iteration-1"
      displayName?: string  // Optional user-friendly name from manifest
      iteration: string  // e.g., "iteration-1"
      groupId: string | null  // e.g., "group-5" or null if no group
      progress: StepProgress | null
      exists: boolean  // Whether folder exists (from runFolders)
      enabled: boolean  // Whether group is enabled
    }

    const items: GroupItem[] = []
    const iterationMap = new Map<string, GroupItem[]>()

    // Helper function to sanitize display names for matching (used for comparing folder names)
    const sanitizeForMatch = (name: string) => name.toLowerCase().replace(/[^a-z0-9-]/g, '-').replace(/-+/g, '-').trim()
    
    // Use utility function for sanitizing display names for folder paths
    const sanitizeDisplayName = sanitizeDisplayNameForFolder

    // Helper function to find matching runFolder for a group
    const findMatchingFolder = (iteration: string, group: { group_id: string; display_name?: string }): RunFolder | null => {
      return folders.find(folder => {
        const parts = folder.name.split('/')
        if (parts.length !== 2 || parts[0] !== iteration) return false
        
        const folderName = parts[1]
        
        // Check if folder name matches group_id
        if (folderName === group.group_id) return true
        
        // Check if folder name matches sanitized display_name
        if (group.display_name) {
          const sanitizedDisplayName = sanitizeForMatch(group.display_name)
          const folderNameSanitized = sanitizeForMatch(folderName)
          if (sanitizedDisplayName === folderNameSanitized) return true
        }
        
        return false
      }) || null
    }

    // Get all iterations from runFolders
    const existingIterations = new Set<string>()
    folders.forEach((folder) => {
      const match = folder.name.match(/^(iteration-\d+)/)
      if (match) {
        existingIterations.add(match[1])
      }
    })

    // If no iterations exist but we have groups in manifest, default to iteration-1
    // This ensures groups are shown even before the first run
    if (existingIterations.size === 0 && variablesManifest?.groups && variablesManifest.groups.length > 0) {
      existingIterations.add('iteration-1')
    }

    // PRIMARY: Start with groups from variablesManifest
    if (variablesManifest?.groups && variablesManifest.groups.length > 0) {
      // For each group in manifest, add it to all iterations
      variablesManifest.groups.forEach((group) => {
        existingIterations.forEach((iteration) => {
          // Check if matching folder exists in runFolders
          const matchingFolder = findMatchingFolder(iteration, group)
          
          // Determine folder name for the path (use sanitized display_name if available, otherwise group_id)
          const folderName = group.display_name && sanitizeDisplayName(group.display_name)
            ? sanitizeDisplayName(group.display_name)
            : group.group_id
          
          const item: GroupItem = {
            id: `${iteration}/${folderName}`,
            name: group.group_id,
            displayName: group.display_name,
            iteration,
            groupId: group.group_id,
            progress: matchingFolder?.progress || null,
            exists: matchingFolder !== null, // Only true if matching folder found
            enabled: group.enabled
          }
          
          items.push(item)
          if (!iterationMap.has(iteration)) {
            iterationMap.set(iteration, [])
          }
          iterationMap.get(iteration)!.push(item)
        })
      })
    }

    // Also add folders from runFolders that aren't in the manifest
    // This handles both top-level iteration folders and group folders
    folders.forEach((folder) => {
      const parts = folder.name.split('/')

      if (parts.length === 1 && parts[0].startsWith('iteration-')) {
        // Top-level iteration folder (no groups - backward compatibility)
        const iteration = parts[0]

        // Only add if this iteration doesn't already have groups
        if (!iterationMap.has(iteration)) {
          const item: GroupItem = {
            id: folder.name,
            name: iteration,
            iteration,
            groupId: null,
            progress: folder.progress || null,
            exists: true,
            enabled: true
          }
          items.push(item)
          iterationMap.set(iteration, [item])
        }
      } else if (parts.length === 2 && parts[0].startsWith('iteration-')) {
        // Group folder (iteration-X/group-name)
        const iteration = parts[0]
        const groupName = parts[1]

        // Check if this group is already in the map (from variablesManifest)
        // Need to check both by folder path AND by matching folder name to sanitized display_name
        const existingGroups = iterationMap.get(iteration) || []
        const alreadyExists = existingGroups.some(g => {
          // Check if folder path matches exactly
          if (g.id === folder.name) return true
          
          // Check if groupId matches (for folders named with group_id like "group-5")
          if (g.groupId === groupName) return true
          
          // Check if folder name matches sanitized display_name
          // This handles cases where folder is named with display_name but manifest has different group_id
          if (g.displayName) {
            const sanitizedDisplayName = sanitizeForMatch(g.displayName)
            const folderNameSanitized = sanitizeForMatch(groupName)
            if (sanitizedDisplayName === folderNameSanitized) return true
          }
          
          return false
        })

        if (!alreadyExists) {
          // Folder exists on disk but doesn't match any group in manifest
          // Try to find a matching group in manifest by folder name (might be display_name)
          let matchingManifestGroup = null
          if (variablesManifest?.groups) {
            matchingManifestGroup = variablesManifest.groups.find(g => {
              // Check if folder name matches group_id
              if (g.group_id === groupName) return true
              
              // Check if folder name matches sanitized display_name
              if (g.display_name) {
                const sanitizedDisplayName = sanitizeForMatch(g.display_name)
                const folderNameSanitized = sanitizeForMatch(groupName)
                if (sanitizedDisplayName === folderNameSanitized) return true
              }
              
              return false
            })
          }
          
          // If we found a matching group in manifest, use its group_id
          // Otherwise, use folder name as group_id (for backward compatibility)
          const item: GroupItem = {
            id: folder.name,
            name: matchingManifestGroup?.group_id || groupName,
            displayName: matchingManifestGroup?.display_name || groupName,
            iteration,
            groupId: matchingManifestGroup?.group_id || groupName,
            progress: folder.progress || null,
            exists: true,
            enabled: matchingManifestGroup?.enabled ?? true
          }
          items.push(item)
          if (!iterationMap.has(iteration)) {
            iterationMap.set(iteration, [])
          }
          iterationMap.get(iteration)!.push(item)
        }
      }
    })

    // Sort iterations by number (descending)
    const sortedIterations = Array.from(iterationMap.keys()).sort((a, b) => {
      const numA = parseInt(a.replace('iteration-', '')) || 0
      const numB = parseInt(b.replace('iteration-', '')) || 0
      return numB - numA
    })

    console.log('[WorkflowToolbar] iterationGroups result:', {
      iterations: sortedIterations,
      itemsCount: items.length,
      items: items.map(i => ({ id: i.id, displayName: i.displayName, groupId: i.groupId }))
    })

    return { sortedIterations, iterationMap, items }
  }, [folders, variablesManifest])
  
  // Auto-expand the iteration containing the selected run folder
  useEffect(() => {
    if (!selectedRunFolder || !iterationGroups.sortedIterations.length) {
      return
    }
    
    // Find which iteration contains the selected run folder
    let targetIteration: string | null = null
    
    // Check if selectedRunFolder is an iteration name itself
    if (iterationGroups.sortedIterations.includes(selectedRunFolder)) {
      targetIteration = selectedRunFolder
    } else {
      // Check if selectedRunFolder is a group within an iteration (format: iteration-X/group-name)
      for (const iteration of iterationGroups.sortedIterations) {
        const groups = iterationGroups.iterationMap.get(iteration) || []
        if (groups.some(g => g.id === selectedRunFolder)) {
          targetIteration = iteration
          break
        }
      }
    }
    
    // Expand only the target iteration, collapse all others
    if (targetIteration) {
      setExpandedIterations(new Set([targetIteration]))
    } else {
      // If no iteration found, collapse all
      setExpandedIterations(new Set())
    }
  }, [selectedRunFolder, iterationGroups.sortedIterations, iterationGroups.iterationMap])
  
  // Toggle iteration expansion
  const toggleIteration = useCallback((iteration: string) => {
    setExpandedIterations(prev => {
      const next = new Set(prev)
      if (next.has(iteration)) {
        next.delete(iteration)
      } else {
        next.add(iteration)
      }
      return next
    })
  }, [])
  
  // Generate start point options based on completed steps and branch steps
  // Use ref to track previous options and prevent recalculation loops
  const startPointOptionsRef = useRef<StartPointOption[]>([])
  const startPointOptionsDataRef = useRef<string>('')
  const startPointOptionsDepsRef = useRef<{ completedStepIndices: number[], totalSteps: number, planStepsLength: number, branchSteps: Record<string, unknown> | undefined }>({
    completedStepIndices: [],
    totalSteps: 0,
    planStepsLength: 0,
    branchSteps: undefined
  })
  
  const startPointOptions = useMemo((): StartPointOption[] => {
    const options: StartPointOption[] = [
      { id: 'from_beginning', label: 'Start from Beginning', icon: Play, description: 'Execute all steps from start' }
    ]
    
    // In Eval mode, only "Start from Beginning" is allowed
    if (workflowMode === 'eval') {
      return options
    }
    
    // Extract specific data inside useMemo to avoid dependency issues
    const stepProgressBranchSteps = stepProgress?.branch_steps
    const planSteps = plan?.steps
    const planStepsCount = planSteps?.length || 0
    
    // Debug: Track dependency changes
    const prevDeps = startPointOptionsDepsRef.current
    const depsChanged = 
      prevDeps.completedStepIndices !== completedStepIndices ||
      prevDeps.totalSteps !== totalSteps ||
      prevDeps.planStepsLength !== planStepsCount ||
      prevDeps.branchSteps !== stepProgressBranchSteps
    
    if (depsChanged) {
      startPointOptionsDepsRef.current = {
        completedStepIndices,
        totalSteps,
        planStepsLength: planStepsCount,
        branchSteps: stepProgressBranchSteps
      }
    }
    
    // Create stable data representation for comparison
    const indicesStr = JSON.stringify(completedStepIndices)
    const branchStepsStr = stepProgressBranchSteps ? JSON.stringify(stepProgressBranchSteps) : ''
    const dataStr = `${indicesStr}|${branchStepsStr}|${totalSteps}|${planStepsCount}|${selectedStartPoint}`
    
    // Only recalculate if data actually changed
    if (startPointOptionsDataRef.current === dataStr && startPointOptionsRef.current.length > 0) {
      return startPointOptionsRef.current
    }
    
    startPointOptionsDataRef.current = dataStr
    
    // Add resume options for all completed steps plus the next step after all completed
    if (completedStepIndices.length > 0 && totalSteps > 0) {
      // Convert 0-based indices to 1-based step numbers
      const completedStepNumbers = completedStepIndices.map(idx => idx + 1).sort((a, b) => a - b)
      const lastCompletedStep = completedStepNumbers[completedStepNumbers.length - 1]
      const nextStep = lastCompletedStep + 1
      
      // Add all completed steps as resume options
      completedStepNumbers.forEach(stepNum => {
        const stepIndex = stepNum - 1 // Convert to 0-based
        const step = plan?.steps?.[stepIndex]
        const stepTitle = step?.title || `Step ${stepNum}`
        options.push({
          id: 'resume',
          stepNumber: stepNum,
          label: `Start Again from step ${stepNum}: ${stepTitle}`,
          icon: RefreshCw,
          description: `Start again from step ${stepNum}`
        })
      })

      // Add next step if it exists (resume from after all completed steps)
      // This is a new step that will run, so it says "Resume" not "Start Again"
      if (nextStep <= totalSteps) {
        const stepIndex = nextStep - 1 // Convert to 0-based
        const step = plan?.steps?.[stepIndex]
        const stepTitle = step?.title || `Step ${nextStep}`
        options.push({
          id: 'resume',
          stepNumber: nextStep,
          label: `Resume from step ${nextStep}: ${stepTitle}`,
          icon: RefreshCw,
          description: `Resume from step ${nextStep}`
        })
      }
    }
    
    // Add branch step resume options for conditional steps with incomplete branches
    if (planSteps && stepProgressBranchSteps && Object.keys(stepProgressBranchSteps).length > 0) {
      Object.entries(stepProgressBranchSteps).forEach(([stepIndexStr, branchProgress]) => {
        const parentStepIndex = parseInt(stepIndexStr, 10)
        const parentStep = planSteps[parentStepIndex]
        
        // Only process conditional steps
        if (!parentStep || !isConditionalStep(parentStep)) {
          return
        }
        
        const branchType = branchProgress.branch_executed === 'if_true' ? 'if_true' : 'if_false'
        const branchSteps = branchType === 'if_true' ? parentStep.if_true_steps : parentStep.if_false_steps
        
        if (!branchSteps || branchSteps.length === 0) {
          return
        }
        
        // Find first incomplete branch step
        let firstIncompleteIndex = -1
        for (let i = 0; i < branchSteps.length; i++) {
          const branchStepPath = `step-${parentStepIndex + 1}-${branchType === 'if_true' ? 'if-true' : 'if-false'}-${i}`
          const isCompleted = branchProgress.completed_steps?.includes(branchStepPath) || false
          
          if (!isCompleted) {
            firstIncompleteIndex = i
            break
          }
        }
        
        const branchLabel = branchType === 'if_true' ? 'Yes' : 'No'
        const completedBranchSteps = branchProgress.completed_steps?.filter((path: string) => 
          path.startsWith(`step-${parentStepIndex + 1}-${branchType === 'if_true' ? 'if-true' : 'if-false'}-`)
        ).length || 0
        
        if (firstIncompleteIndex === -1) {
          // All branch steps completed - add resume option to re-execute from the first branch step
          // This allows re-running the branch even if all steps are completed (similar to regular steps)
          console.log(`[WorkflowToolbar] Adding branch resume option (all completed): Step ${parentStepIndex + 1}, ${branchLabel} branch, step 1 (${completedBranchSteps}/${branchSteps.length} completed)`)
          options.push({
            id: 'resume_branch',
            branchStep: {
              parentStepIndex,
              branchType,
              branchStepIndex: 0 // Start from first branch step
            },
            label: `🔀 Resume: Step ${parentStepIndex + 1} → ${branchLabel} Branch → Step 1`,
            icon: RefreshCw,
            description: `Re-execute ${branchLabel} branch from step 1 (${completedBranchSteps}/${branchSteps.length} completed)`
          })
        } else {
          // Add resume option for the first incomplete branch step
          console.log(`[WorkflowToolbar] Adding branch resume option: Step ${parentStepIndex + 1}, ${branchLabel} branch, step ${firstIncompleteIndex + 1} (${completedBranchSteps}/${branchSteps.length} completed)`)
          options.push({
            id: 'resume_branch',
            branchStep: {
              parentStepIndex,
              branchType,
              branchStepIndex: firstIncompleteIndex
            },
            label: `🔀 Resume: Step ${parentStepIndex + 1} → ${branchLabel} Branch → Step ${firstIncompleteIndex + 1}`,
            icon: RefreshCw,
            description: `Continue from ${branchLabel} branch, step ${firstIncompleteIndex + 1} (${completedBranchSteps}/${branchSteps.length} completed)`
          })
        }
      })
    }
    
    // If selectedStartPoint > 0 but no resume options were added (e.g., completedStepIndices is empty),
    // add the selected start point as an option so the user can see and change their selection
    if (selectedStartPoint > 0 && totalSteps > 0) {
      const hasSelectedOption = options.some(o => o.stepNumber === selectedStartPoint)
      if (!hasSelectedOption) {
        // Add options for all steps up to selectedStartPoint so user can change their selection
        for (let stepNum = 1; stepNum <= Math.min(selectedStartPoint, totalSteps); stepNum++) {
          // Skip if already exists
          if (options.some(o => o.stepNumber === stepNum)) continue

          const stepIndex = stepNum - 1
          const step = planSteps?.[stepIndex]
          const stepTitle = step?.title || `Step ${stepNum}`
          const isSelectedStep = stepNum === selectedStartPoint
          options.push({
            id: 'resume',
            stepNumber: stepNum,
            label: isSelectedStep
              ? `Resume from step ${stepNum}: ${stepTitle}`
              : `Start Again from step ${stepNum}: ${stepTitle}`,
            icon: RefreshCw,
            description: isSelectedStep
              ? `Resume from step ${stepNum}`
              : `Start again from step ${stepNum}`
          })
        }
      }
    }

    // Sort options: from_beginning first, then regular resume options, then branch resume options
    options.sort((a, b) => {
      if (a.id === 'from_beginning') return -1
      if (b.id === 'from_beginning') return 1
      if (a.id === 'resume_branch' && b.id !== 'resume_branch') return 1
      if (b.id === 'resume_branch' && a.id !== 'resume_branch') return -1
      if (a.id === 'resume_branch' && b.id === 'resume_branch') {
        // Sort branch options by parent step index, then branch step index
        const aParent = a.branchStep?.parentStepIndex ?? 0
        const bParent = b.branchStep?.parentStepIndex ?? 0
        if (aParent !== bParent) return aParent - bParent
        return (a.branchStep?.branchStepIndex ?? 0) - (b.branchStep?.branchStepIndex ?? 0)
      }
      // Regular resume options - sort by step number
      return (a.stepNumber ?? 0) - (b.stepNumber ?? 0)
    })
    
    // Only log when options actually change (not on every render)  
    // Removed console.log to prevent excessive logging - uncomment for debugging
    // console.log(`[WorkflowToolbar] Generated ${options.length} start point options:`, options.map(o => ({ id: o.id, label: o.label, hasBranchStep: !!o.branchStep })))
    
    // Store in ref for next comparison
    startPointOptionsRef.current = options
    return options
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [completedStepIndices, totalSteps, plan?.steps?.length, stepProgress?.branch_steps, selectedStartPoint]) // Only depend on specific data, not whole objects - using ref comparison to prevent loops

  // Get current start point info
  const currentStartPointInfo = useMemo(() => {
    if (selectedBranchStep && selectedBranchStep !== null && selectedBranchStep !== undefined) {
      // Find branch step option
      return startPointOptions.find(o =>
        o.id === 'resume_branch' &&
        o.branchStep !== undefined &&
        o.branchStep !== null &&
        o.branchStep.parentStepIndex === selectedBranchStep.parentStepIndex &&
        o.branchStep.branchType === selectedBranchStep.branchType &&
        o.branchStep.branchStepIndex === selectedBranchStep.branchStepIndex
      ) || startPointOptions[0]
    }
    if (selectedStartPoint === 0) {
      return startPointOptions[0] // "Start from Beginning"
    }
    const found = startPointOptions.find(o => o.stepNumber === selectedStartPoint)
    console.log('[WorkflowToolbar] currentStartPointInfo:', {
      selectedStartPoint,
      optionsCount: startPointOptions.length,
      found: found?.label,
      fallback: startPointOptions[0]?.label
    })

    // If option not found but we have a valid start point, create a synthetic option
    if (!found && selectedStartPoint > 0) {
      return {
        id: 'resume',
        stepNumber: selectedStartPoint,
        label: `Resume from Step ${selectedStartPoint}`,
        icon: Play,
        description: `Resume execution from step ${selectedStartPoint}`
      }
    }

    return found || startPointOptions[0]
  }, [selectedStartPoint, selectedBranchStep, startPointOptions])

  // Validate selectedStartPoint - only check if it's within valid range (1 to totalSteps)
  // We don't need to check if it's in startPointOptions because:
  // 1. startPointOptions is just a UI convenience showing common options
  // 2. Users should be able to select any step number, not just completed ones
  // 3. The backend will handle execution from any valid step number
  useEffect(() => {
    // Don't validate if no start point is selected
    if (selectedStartPoint === 0) {
      return
    }

    // Don't validate if totalSteps is not yet known
    if (totalSteps === 0) {
      return
    }

    // Only validate that the step number is within valid range
    if (selectedStartPoint < 1 || selectedStartPoint > totalSteps) {
      console.log(`[WorkflowToolbar] Selected start point ${selectedStartPoint} is out of range (1-${totalSteps}), resetting to 0`)
      setStartPoint(0)
    } else {
      console.log(`[WorkflowToolbar] ✅ Selected start point ${selectedStartPoint} is valid (range: 1-${totalSteps})`)
    }
  }, [selectedStartPoint, totalSteps, setStartPoint])

  // Auto-resume logic removed - start point always defaults to 0

  // Get current phase details
  const currentPhaseDetails = phases.find((p: WorkflowPhase) => p.id === currentPhase)
  // Only consider it execution phase if currentPhase is explicitly set to 'execution'
  // If currentPhase is undefined/null, allow dropdown to be enabled
  const isExecutionPhase = currentPhase === EXECUTION_PHASE_ID || currentPhase === EVAL_EXECUTION_PHASE_ID

  // Allow dropdown to be enabled even when phases are running - this enables parallel execution
  // Only disable if phases are loading or there are no other phases available
  // Individual phase checks (if a specific phase is running) are handled in handleSelectPhase
  const dropdownDisabled = isLoadingPhases || otherPhases.length === 0

  // Handle phase selection
  const handleSelectPhase = (phaseId: string) => {
    console.log('[WorkflowToolbar] handleSelectPhase called:', { phaseId, isRunning, status })
    setIsDropdownOpen(false)
    
    // Check if THIS SPECIFIC phase's tab is running (not just any tab)
    const getTabsByPhaseId = useChatStore.getState().getTabsByPhaseId
    const phaseTabs = getTabsByPhaseId(phaseId, presetQueryId || undefined)
    const getTabStreamingStatus = useChatStore.getState().getTabStreamingStatus
    const isPhaseRunning = phaseTabs.some(tab => getTabStreamingStatus(tab.tabId))
    
    console.log('[WorkflowToolbar] Phase status check:', {
      phaseId,
      phaseTabsCount: phaseTabs.length,
      isPhaseRunning,
      phaseTabs: phaseTabs.map(t => ({ tabId: t.tabId, isStreaming: getTabStreamingStatus(t.tabId) }))
    })
    
    if (!isPhaseRunning) {
      // For phases that need run folder context, pass execution options
      if (phaseId === 'evaluation-execution' ||
          phaseId === 'evaluation-builder') {
        // Build execution options to include selected_run_folder
        const executionOptions = buildExecutionOptions()
        console.log('[WorkflowToolbar] Starting', phaseId, 'with execution options:', executionOptions)
        onStartPhase(phaseId, executionOptions)
      } else {
        // For other phases, don't pass execution options
        console.log('[WorkflowToolbar] Starting', phaseId, 'without execution options')
        onStartPhase(phaseId)
      }
    } else {
      console.warn('[WorkflowToolbar] Phase selection blocked: phase', phaseId, 'is already running')
    }
  }

  

  // Handle selecting start point from dropdown
  const handleSelectStartPoint = useCallback((option: StartPointOption) => {
    if (option.id === 'resume_branch' && option.branchStep) {
      setBranchStep(option.branchStep)
    } else if (option.stepNumber !== undefined) {
      setStartPoint(option.stepNumber)
      // Note: setStartPoint already clears selectedBranchStep in the store
    } else if (option.id === 'from_beginning') {
      setStartPoint(0) // "Start from Beginning"
      // Note: setStartPoint already clears selectedBranchStep in the store
    }
    setIsStartPointDropdownOpen(false)
  }, [setStartPoint, setBranchStep])

  // Handle selecting run folder
  const handleSelectRunFolder = useCallback((folder: string) => {
    setSelectedRunFolder(folder)
    setIsIterationDropdownOpen(false)
  }, [setSelectedRunFolder])

  // Handle creating new iteration
  const handleCreateIteration = useCallback(async () => {
    if (!workspacePath || isCreatingIteration) return

    setIsCreatingIteration(true)
    try {
      const response = await agentApi.createRunFolder(workspacePath)
      
      if (response.success && response.folder_name) {
        // Refresh workspace files to reflect new folder
        await fetchFiles()
        
        // Refresh folder list in store (for backward compatibility)
        await loadRunFolders(workspacePath)
        
        // Select the newly created iteration
        setSelectedRunFolder(response.folder_name)
        
        // Load progress for the new iteration (will be empty, but ensures consistency)
        await loadProgress(workspacePath, response.folder_name)
        
        // CRITICAL: Refresh workspace state in parent component to update runFolders prop
        // This ensures the new iteration appears in the dropdown immediately
        if (onRefresh) {
          await onRefresh()
        }
        
        // Close dropdown
        setIsIterationDropdownOpen(false)
      } else {
        console.error('[WorkflowToolbar] Failed to create iteration:', response.message)
        alert(`Failed to create iteration: ${response.message || 'Unknown error'}`)
      }
    } catch (error) {
      console.error('[WorkflowToolbar] Failed to create iteration:', error)
      alert(`Failed to create iteration: ${error instanceof Error ? error.message : 'Unknown error'}`)
    } finally {
      setIsCreatingIteration(false)
    }
  }, [workspacePath, isCreatingIteration, fetchFiles, loadRunFolders, setSelectedRunFolder, loadProgress, onRefresh])

  // Handle delete folder confirmation
  const handleDeleteFolderClick = useCallback((e: React.MouseEvent, folderName: string) => {
    e.stopPropagation() // Prevent selecting the folder when clicking delete
    setDeleteDialog({
      isOpen: true,
      folderName,
      isLoading: false
    })
  }, [])

  // Handle delete folder confirmation
  const handleDeleteFolderConfirm = useCallback(async () => {
    if (!deleteDialog.folderName || !workspacePath) return

    setDeleteDialog(prev => ({ ...prev, isLoading: true }))

    try {
      await agentApi.deleteRunFolder(workspacePath, deleteDialog.folderName)
      
      // Refresh workspace files to reflect deletion
      await fetchFiles()
      
      // Refresh folder list
      await loadRunFolders(workspacePath)
      
      // Get updated folders after refresh (folders are sorted descending by iteration number)
      const updatedFolders = useWorkflowStore.getState().runFolders
      
      // If deleted folder was selected, or we want to show next highest iteration
      // Select the highest remaining iteration (first in sorted array)
      if (selectedRunFolder === deleteDialog.folderName || updatedFolders.length > 0) {
        const nextHighest = updatedFolders.length > 0 ? updatedFolders[0].name : 'new'
        setSelectedRunFolder(nextHighest)
        
        // Load progress for the selected iteration if it's not 'new'
        if (nextHighest !== 'new') {
          await loadProgress(workspacePath, nextHighest)
        }
      }
      
      // Close dialog
      setDeleteDialog({
        isOpen: false,
        folderName: null,
        isLoading: false
      })
    } catch (error) {
      console.error('[WorkflowToolbar] Failed to delete folder:', error)
      // Keep dialog open on error so user can retry
      setDeleteDialog(prev => ({ ...prev, isLoading: false }))
    }
  }, [deleteDialog.folderName, workspacePath, selectedRunFolder, setSelectedRunFolder, loadRunFolders, loadProgress, fetchFiles])

  // Visual feedback + double-click guard: shows "Starting..." with spinner while
  // waiting for SSE to connect (can take 20s+). Without this, the button stays as
  // "Execute" and users click again thinking nothing happened.
  const [isExecutionStarting, setIsExecutionStarting] = useState(false)

  // Clear "Starting..." state when execution actually starts running
  useEffect(() => {
    if (isExecutionRunning && isExecutionStarting) {
      setIsExecutionStarting(false)
    }
  }, [isExecutionRunning, isExecutionStarting])

  // Handle execution button click - finds/reuses execution tab
  const handleExecute = useCallback(async () => {
    // Prevent double-click: block if execution is already being started
    if (isExecutionStarting) {
      console.log('[WorkflowToolbar] Execution already starting, ignoring duplicate click')
      return
    }

    // Determine target execution phase based on mode
    const targetExecutionPhaseId = workflowMode === 'eval' ? EVAL_EXECUTION_PHASE_ID : EXECUTION_PHASE_ID
    const targetPhase = phases.find(p => p.id === targetExecutionPhaseId)

    // Use isExecutionRunning instead of isRunning to allow execution even when other phases are running
    if (!isExecutionRunning && targetPhase) {
      // Check if groups are available and if at least one is selected
      const hasGroups = variablesManifest?.groups && variablesManifest.groups.length > 0
      if (hasGroups && selectedGroupIds.length === 0) {
        console.warn('[WorkflowToolbar] Cannot execute: No groups selected')
        return
      }

      // Set guard + show "Starting..." AFTER validation passes (so errors don't lock the button)
      setIsExecutionStarting(true)
      // Safety net: auto-clear after 60s in case SSE never connects (can take 20s+)
      setTimeout(() => { setIsExecutionStarting(false) }, 60000)

      // Find existing execution phase tab
      // Get execution tabs from generalized chat store
      const chatStore = useChatStore.getState()
      const allTabs = Object.values(chatStore.chatTabs)
      const executionTabs = allTabs.filter(tab =>
        tab.metadata?.mode === 'workflow' &&
        tab.metadata?.phaseId === targetExecutionPhaseId &&
        tab.metadata?.presetQueryId === presetQueryId
      )
      const existingExecutionTab = executionTabs.length > 0 ? executionTabs[0] : null

      if (existingExecutionTab) {
        // Reuse existing execution tab
        console.log(`[WorkflowToolbar] Reusing existing execution tab: ${existingExecutionTab.tabId}`)

        // Switch to it if not already active
        if (chatStore.activeTabId !== existingExecutionTab.tabId) {
          chatStore.switchTab(existingExecutionTab.tabId)
        }

        // No observer ID syncing needed - sessions are used directly
      } else {
        console.log('[WorkflowToolbar] No existing execution tab, creating new one')
      }

      // Build execution options
      const workflowStore = useWorkflowStore.getState()
      console.log('[EXECUTION_OPTIONS_DEBUG] [WorkflowToolbar] Before buildExecutionOptions:', {
        selectedGroupIds: workflowStore.selectedGroupIds,
        selectedGroupIdsLength: workflowStore.selectedGroupIds?.length || 0,
        hasVariablesManifest: !!variablesManifest,
        groupsCount: variablesManifest?.groups?.length || 0,
        allGroupIds: variablesManifest?.groups?.map(g => g.group_id) || []
      })
      const options = buildExecutionOptions()
      console.log('[EXECUTION_OPTIONS_DEBUG] [WorkflowToolbar] Starting execution with options:', JSON.stringify({
        execution_strategy: options.execution_strategy,
        resume_from_step: options.resume_from_step,
        resume_from_branch_step: options.resume_from_branch_step,
        selected_run_folder: options.selected_run_folder,
        run_mode: options.run_mode,
        enabled_group_ids: options.enabled_group_ids,
        enabled_group_ids_length: options.enabled_group_ids?.length || 0
      }, null, 2))

      // Start phase (will create new tab if none exists, or use existing if we switched to it)
      onStartPhase(targetExecutionPhaseId, options)
    }
  }, [isExecutionRunning, isExecutionStarting, phases, workflowMode, buildExecutionOptions, onStartPhase, selectedGroupIds, variablesManifest])

  // Determine target execution phase for rendering
  const targetExecutionPhase = phases.find(p => p.id === targetExecutionPhaseId)

  return (
    <>
    <div className={`
      flex items-center justify-between gap-2 px-3 py-1.5 
      bg-background border-b border-border
      relative z-10
      ${className}
    `}>
      {/* Left side - Phase selector */}
      <div className="flex items-center gap-2">
        {!hasPlan ? (
          // No plan - show create button + refresh button
          <>
            <button
              onClick={onCreatePlan}
              className="flex items-center gap-1.5 px-2.5 py-1.5 bg-muted text-foreground rounded-md hover:bg-accent transition-colors font-medium text-xs"
            >
              <Plus className="w-3.5 h-3.5" />
              {workflowMode === 'eval' ? 'Create Evaluation Plan' : 'Build Plan'}
            </button>
            {onRefresh && (
              <TooltipProvider delayDuration={150}>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <button
                      onClick={async () => {
                        try {
                          await onRefresh()
                        } catch (err) {
                          console.error('[WorkflowToolbar] Failed to refresh:', err)
                        }
                      }}
                      className="flex items-center justify-center w-7 h-7 rounded-md transition-all text-xs
                                 bg-muted text-foreground hover:bg-accent"
                    >
                      <RefreshCw className="w-3.5 h-3.5" />
                    </button>
                  </TooltipTrigger>
                  <TooltipContent>
                    <p>Refresh plan, step config, and variables</p>
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>
            )}
          </>
        ) : (
          <>
            {/* Mode Toggle */}
            <div className="flex items-center gap-1 bg-muted rounded-md p-1 mr-2 border border-border">
              <button
                onClick={() => setWorkflowMode('plan')}
                className={`px-3 py-1 rounded-md text-xs font-medium transition-all ${
                  workflowMode === 'plan'
                    ? 'bg-background text-foreground shadow-sm'
                    : 'text-muted-foreground hover:text-foreground'
                }`}
              >
                Plan+Exec
              </button>
              <button
                onClick={() => setWorkflowMode('eval')}
                className={`px-3 py-1 rounded-md text-xs font-medium transition-all ${
                  workflowMode === 'eval'
                    ? 'bg-background text-foreground shadow-sm'
                    : 'text-muted-foreground hover:text-foreground'
                }`}
              >
                Eval
              </button>
            </div>

            {/* Regular Phases Dropdown Selector - moved before execution button */}
            <div className="relative" ref={dropdownRef}>
              <button
                ref={dropdownButtonRef}
                onClick={() => {
                  console.log('[WorkflowToolbar] Phase dropdown button clicked:', { isLoadingPhases, visiblePhasesLength: visiblePhases.length, isDropdownOpen })
                  // Allow dropdown to open even when phases are running - enables parallel execution
                  // Only block if phases are loading or there are no phases available
                  if (!isLoadingPhases && visiblePhases.length > 0) {
                    setIsDropdownOpen(!isDropdownOpen)
                  } else {
                    console.warn('[WorkflowToolbar] Dropdown blocked:', { isLoadingPhases, visiblePhasesLength: visiblePhases.length })
                  }
                }}
                disabled={dropdownDisabled}
                className={`
                  flex items-center gap-1.5 px-2.5 py-1.5 rounded-md transition-all text-xs font-medium min-w-[160px]
                  ${dropdownDisabled
                    ? 'bg-muted text-muted-foreground cursor-not-allowed' 
                    : 'bg-muted text-foreground hover:bg-accent'
                  }
                `}
              >
                {isLoadingPhases ? (
                  <>
                    <Loader2 className="w-3.5 h-3.5 animate-spin" />
                    <span>Loading phases...</span>
                  </>
                ) : (() => {
                  // Check if current phase is running
                  const getTabsByPhaseId = useChatStore.getState().getTabsByPhaseId
                  const getTabStreamingStatus = useChatStore.getState().getTabStreamingStatus
                  const currentPhaseTabs = currentPhase ? getTabsByPhaseId(currentPhase, presetQueryId || undefined) : []
                  const isCurrentPhaseRunning = currentPhaseTabs.some(tab => getTabStreamingStatus(tab.tabId))
                  
                  if (isCurrentPhaseRunning && !isExecutionPhase) {
                    return (
                      <>
                        <Loader2 className="w-3.5 h-3.5 animate-spin flex-shrink-0" />
                        <span className="flex-1 text-left truncate">
                          {currentPhaseDetails?.title || 'Running...'}
                        </span>
                        <ChevronDown className={`w-3.5 h-3.5 flex-shrink-0 transition-transform ${isDropdownOpen ? 'rotate-180' : ''}`} />
                      </>
                    )
                  }
                  
                  return (
                    <>
                      <Play className="w-3.5 h-3.5 flex-shrink-0" />
                      <span className="flex-1 text-left truncate">
                        {currentPhaseDetails && !isExecutionPhase ? currentPhaseDetails.title : 'Select Phase'}
                      </span>
                      <ChevronDown className={`w-3.5 h-3.5 flex-shrink-0 transition-transform ${isDropdownOpen ? 'rotate-180' : ''}`} />
                    </>
                  )
                })()}
              </button>

              {/* Dropdown Menu - rendered via portal for proper z-index */}
              {isDropdownOpen && !isLoadingPhases && visiblePhases.length > 0 && dropdownPosition && createPortal(
                <div 
                  data-phase-dropdown
                  ref={dropdownRef}
                  className="fixed w-80 bg-popover rounded-lg shadow-xl border border-border z-[9999] max-h-[400px] overflow-y-auto"
                  style={{
                    top: `${dropdownPosition.top}px`,
                    left: `${dropdownPosition.left}px`
                  }}
                >
                  <div className="p-2">
                    <div className="text-xs font-semibold text-muted-foreground uppercase tracking-wider px-3 py-2">
                      {workflowMode === 'eval' ? 'Evaluation Phases' : 'Workflow Phases'}
                    </div>
                    {visiblePhases.length === 0 ? (
                      <div className="px-3 py-4 text-sm text-muted-foreground text-center">
                        {isLoadingPhases ? 'Loading phases...' : 'No phases available'}
                      </div>
                    ) : (
                      visiblePhases.map((phase: WorkflowPhase) => {
                      const isActive = currentPhase === phase.id
                      // Check if THIS specific phase is running or completed
                      const getTabsByPhaseId = useChatStore.getState().getTabsByPhaseId
                      const getTabStreamingStatus = useChatStore.getState().getTabStreamingStatus
                      const phaseTabs = getTabsByPhaseId(phase.id, presetQueryId || undefined)
                      // Find the most recent or active tab for this phase
                      const activePhaseTab = phaseTabs.length > 0 
                        ? phaseTabs.sort((a, b) => b.createdAt - a.createdAt)[0] // Most recent
                        : null
                      const isPhaseRunning = activePhaseTab ? getTabStreamingStatus(activePhaseTab.tabId) : false
                      const isPhaseCompleted = activePhaseTab?.isCompleted || false
                      const isDisabled = isPhaseRunning // Only disable if running, allow clicking completed phases
                      
                      return (
                        <button
                          key={phase.id}
                          onClick={() => handleSelectPhase(phase.id)}
                          disabled={isDisabled}
                          className={`
                            w-full text-left px-3 py-2.5 rounded-lg transition-colors
                            ${isDisabled
                              ? 'opacity-50 cursor-not-allowed bg-muted'
                              : isActive 
                                ? 'bg-accent text-accent-foreground font-semibold' 
                                : 'hover:bg-accent text-foreground'
                            }
                          `}
                        >
                          <div className="flex items-start gap-3">
                            <div className={`
                              w-5 h-5 rounded-full flex items-center justify-center flex-shrink-0 mt-0.5
                              ${isPhaseRunning
                                ? 'bg-purple-500 dark:bg-purple-600'
                                : isPhaseCompleted
                                  ? 'bg-green-500 dark:bg-green-600'
                                  : isActive 
                                    ? 'bg-foreground text-background'
                                    : 'bg-muted text-muted-foreground'
                              }
                            `}>
                              {isPhaseRunning ? (
                                <Loader2 className="w-3 h-3 text-white animate-spin" />
                              ) : isPhaseCompleted ? (
                                <Check className="w-3 h-3 text-white" />
                              ) : isActive ? (
                                <Check className="w-3 h-3" />
                              ) : (
                                <span className="text-xs font-medium">
                                  {visiblePhases.indexOf(phase) + 1}
                                </span>
                              )}
                            </div>
                            <div className="flex-1 min-w-0">
                              <div className="font-medium text-sm flex items-center gap-2">
                                {phase.title}
                                {isPhaseRunning && (
                                  <span className="text-xs px-1.5 py-0.5 bg-primary/10 text-primary rounded flex items-center gap-1">
                                    <Loader2 className="w-2.5 h-2.5 animate-spin" />
                                    Running
                                  </span>
                                )}
                                {isPhaseCompleted && !isPhaseRunning && (
                                  <span className="text-xs px-1.5 py-0.5 bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300 rounded">
                                    Completed
                                  </span>
                                )}
                              </div>
                              {phase.description && (
                                <div className="text-xs text-muted-foreground mt-0.5 line-clamp-2">
                                  {phase.description}
                                </div>
                              )}
                            </div>
                          </div>
                        </button>
                      )
                    }))}
                  </div>
                </div>,
                document.body
              )}
            </div>

            {/* Refresh Button - Reload plan and variables */}
            {onRefresh && (
              <>
                <TooltipProvider delayDuration={150}>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <button
                      onClick={async () => {
                        try {
                          await onRefresh()
                        } catch (err) {
                          console.error('[WorkflowToolbar] Failed to refresh:', err)
                        }
                      }}
                      className="flex items-center justify-center w-7 h-7 rounded-md transition-all text-xs
                                 bg-muted text-foreground hover:bg-accent"
                    >
                      <RefreshCw className="w-3.5 h-3.5" />
                    </button>
                  </TooltipTrigger>
                  <TooltipContent>
                    <p>Refresh plan, step config, and variables</p>
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>
                {targetExecutionPhase && <div className="w-px h-5 bg-border" />}
              </>
            )}

            {/* Execution Controls - Execute button and configuration dropdowns */}
            {targetExecutionPhase && (
              <>
                {/* Execute/Stop Button - Changes to Stop when execution phase is running */}
                {/* Disable if no groups are selected (when groups are available) - but only when NOT running */}
                {(() => {
                  const hasGroups = variablesManifest?.groups && variablesManifest.groups.length > 0
                  const noGroupsSelected = selectedGroupIds.length === 0
                  // Disable when: starting up, or no groups selected (when groups exist)
                  const isDisabled = isExecutionStarting || (!isExecutionRunning && hasGroups && noGroupsSelected)

                  return (
                    <button
                      onClick={isExecutionRunning ? onStop : handleExecute}
                      disabled={isDisabled}
                      className={`
                        flex items-center gap-1.5 px-2.5 py-1.5 rounded-md transition-all text-xs font-semibold
                        ${isExecutionRunning
                          ? 'bg-destructive text-destructive-foreground shadow-md hover:bg-destructive/90 hover:shadow-lg'
                          : isExecutionStarting
                          ? 'bg-primary/70 text-primary-foreground shadow-md cursor-wait'
                          : isDisabled
                          ? 'bg-muted text-muted-foreground shadow-md cursor-not-allowed opacity-75'
                          : 'bg-primary text-primary-foreground shadow-md hover:bg-primary/90 hover:shadow-lg'
                        }
                      `}
                    >
                      {isExecutionRunning ? (
                        <>
                          <Square className="w-3.5 h-3.5" />
                          <span>Stop</span>
                        </>
                      ) : isExecutionStarting ? (
                        <>
                          <Loader2 className="w-3.5 h-3.5 animate-spin" />
                          <span>Starting...</span>
                        </>
                      ) : (
                        <>
                          <Rocket className="w-3.5 h-3.5" />
                          <span>Execute</span>
                        </>
                      )}
                    </button>
                  )
                })()}
                
                <div className="w-px h-5 bg-border" />
                
                {/* Iteration Selector */}
                <div className="relative" ref={iterationDropdownRef}>
                  <button
                    ref={iterationDropdownButtonRef}
                    onClick={(e) => {
                      e.stopPropagation() // Prevok ent event bubbling
                      if (!isRunning && !isLoadingWorkspaceState) {
                        setIsIterationDropdownOpen(!isIterationDropdownOpen)
                      }
                    }}
                    disabled={isRunning || isLoadingWorkspaceState}
                    className={`
                      flex items-center gap-1.5 px-2 py-1.5 rounded-md transition-all text-xs font-medium
                      ${isRunning || isLoadingWorkspaceState
                        ? 'bg-muted text-muted-foreground cursor-not-allowed'
                        : 'bg-muted text-foreground hover:bg-accent hover:text-accent-foreground'
                      }
                    `}
                  >
                    {isLoadingWorkspaceState ? (
                      <Loader2 className="w-3.5 h-3.5 animate-spin" />
                    ) : (
                      <FolderOpen className="w-3.5 h-3.5" />
                    )}
                    <span className="max-w-[120px] truncate" title={getSelectedRunFolderDisplay}>
                      {isLoadingWorkspaceState ? 'Loading...' : getSelectedRunFolderDisplay}
                    </span>
                    <ChevronDown className={`w-3 h-3 transition-transform ${isIterationDropdownOpen ? 'rotate-180' : ''}`} />
                  </button>
                  
                  {/* Iteration Dropdown - rendered via portal for proper z-index */}
                  {isIterationDropdownOpen && !isRunning && iterationDropdownPosition && createPortal(
                    <div 
                      data-iteration-dropdown
                      ref={iterationDropdownRef}
                      className="fixed w-56 bg-popover rounded-lg shadow-xl border border-border z-[9999] max-h-[300px] overflow-y-auto"
                      style={{
                        top: `${iterationDropdownPosition.top}px`,
                        left: `${iterationDropdownPosition.left}px`
                      }}
                    >
                      <div className="p-1">
                        {/* Create New Iteration Button */}
                        <button
                          onClick={handleCreateIteration}
                          disabled={isCreatingIteration || !workspacePath}
                          className={`
                            w-full flex items-center gap-2 px-3 py-2 rounded-md text-sm transition-colors mb-1
                            ${isCreatingIteration || !workspacePath
                              ? 'bg-muted text-muted-foreground cursor-not-allowed'
                              : 'bg-primary/10 text-primary hover:bg-primary/20'
                            }
                          `}
                        >
                          {isCreatingIteration ? (
                            <>
                              <Loader2 className="w-4 h-4 animate-spin" />
                              <span>Creating...</span>
                            </>
                          ) : (
                            <>
                              <Plus className="w-4 h-4" />
                              <span>Create New Iteration</span>
                            </>
                          )}
                        </button>
                        
                        {(iterationGroups.sortedIterations.length > 0 || folders.length > 0) ? (
                          <>
                            <div className="border-t border-border my-1" />
                            <div className="text-xs font-semibold text-muted-foreground uppercase tracking-wider px-3 py-1">
                              {iterationGroups.sortedIterations.length > 0 ? 'Iterations & Groups' : `Existing Runs (${folders.length})`}
                            </div>
                            {iterationGroups.sortedIterations.length > 0 ? (
                              // Show grouped by iteration
                              iterationGroups.sortedIterations.map((iteration) => {
                                const groups = iterationGroups.iterationMap.get(iteration) || []
                                const hasGroups = groups.some(g => g.groupId !== null)
                                const enabledGroups = groups.filter(g => g.enabled !== false)
                                const hasMultipleGroups = enabledGroups.length > 1
                                const isExpanded = expandedIterations.has(iteration)
                                
                                return (
                                  <div key={iteration}>
                                    {/* Iteration header (only if it has groups or is a top-level folder) */}
                                    {hasGroups ? (
                                      <div
                                        role="button"
                                        tabIndex={0}
                                        onClick={() => toggleIteration(iteration)}
                                        onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') toggleIteration(iteration) }}
                                        className="w-full px-3 py-1.5 text-xs font-semibold text-muted-foreground bg-muted/50 flex items-center justify-between gap-2 hover:bg-muted transition-colors cursor-pointer"
                                      >
                                        <div className="flex items-center gap-2">
                                          {isExpanded ? (
                                            <ChevronDown className="w-3.5 h-3.5" />
                                          ) : (
                                            <ChevronRight className="w-3.5 h-3.5" />
                                          )}
                                          <span>{iteration}</span>
                                        </div>
                                        {/* Select All / Unselect All buttons - only show if multiple groups and not in eval mode */}
                                        {hasMultipleGroups && isExpanded && workflowMode !== 'eval' && (
                                          <div className="flex items-center gap-1" onClick={(e) => e.stopPropagation()}>
                                            <button
                                              onClick={(e) => {
                                                e.stopPropagation()
                                                const allGroupIds = enabledGroups.map(g => g.groupId!).filter(Boolean) as string[]
                                                setSelectedGroupIds(allGroupIds)
                                                // Use the first selected group's folder path so loadProgress can find steps_done.json
                                                if (allGroupIds.length > 0) {
                                                  const firstGroup = enabledGroups.find(g => g.groupId)
                                                  if (firstGroup) {
                                                    setSelectedRunFolder(firstGroup.id)
                                                  } else {
                                                    setSelectedRunFolder(iteration)
                                                  }
                                                }
                                              }}
                                              disabled={isRunning}
                                              className="p-1 rounded hover:bg-accent disabled:opacity-50 disabled:cursor-not-allowed"
                                            >
                                              <CheckSquare className="w-3.5 h-3.5 text-primary" />
                                            </button>
                                            <button
                                              onClick={(e) => {
                                                e.stopPropagation()
                                                clearSelectedGroupIds()
                                                // When unselecting all groups, set to iteration folder
                                                setSelectedRunFolder(iteration)
                                              }}
                                              disabled={isRunning}
                                              className="p-1 rounded hover:bg-accent disabled:opacity-50 disabled:cursor-not-allowed"
                                            >
                                              <Square className="w-3.5 h-3.5 text-muted-foreground" />
                                            </button>
                                          </div>
                                        )}
                                      </div>
                                    ) : (
                                      /* Top-level iteration folder without groups - render as clickable item */
                                      (() => {
                                        const iterItem = groups[0] // The single item with groupId: null
                                        const progress = iterItem?.progress
                                        const completedCount = progress?.completed_step_indices?.length || 0
                                        const totalSteps = progress?.total_steps || 0
                                        const hasProgress = progress && completedCount > 0
                                        const isSelected = selectedRunFolder === iteration
                                        return (
                                          <>
                                            <button
                                              onClick={() => handleSelectRunFolder(iteration)}
                                              className={`
                                                w-full text-left px-3 py-2 rounded-md text-sm flex items-center gap-2
                                                ${isSelected
                                                  ? 'bg-primary/10 text-primary'
                                                  : 'hover:bg-accent text-foreground'
                                                }
                                              `}
                                            >
                                              <FolderOpen className="w-4 h-4" />
                                              <span className="flex-1 text-xs font-mono">{iteration}</span>
                                              {hasProgress && (
                                                <span className="text-[10px] text-muted-foreground">
                                                  {completedCount}/{totalSteps}
                                                </span>
                                              )}
                                            </button>
                                          </>
                                        )
                                      })()
                                    )}

                                    {/* Only show groups when iteration is expanded */}
                                    {isExpanded && hasGroups && (
                                      <>
                                        {/* Groups under this iteration - show ALL groups from manifest, not just ones with folders */}
                                        {/* Filter out iteration folders (groupId === null) since we already show the iteration header */}
                                        {groups.filter(group => group.groupId !== null).map((group) => {
                                      const progress = group.progress
                                      const completedCount = progress?.completed_step_indices?.length || 0
                                      const totalSteps = progress?.total_steps || 0
                                      const hasProgress = progress && completedCount > 0
                                      const isSelected = selectedRunFolder === group.id
                                      const isDisabled = group.enabled === false
                                      const isGroupChecked = group.groupId ? selectedGroupIds.includes(group.groupId) : false
                                      
                                      return (
                                        <div
                                          key={group.id}
                                          className={`
                                            group flex items-center gap-1 px-1
                                            ${isSelected 
                                              ? 'bg-primary/10' 
                                              : ''
                                            }
                                            ${isDisabled ? 'opacity-60' : ''}
                                          `}
                                          onMouseEnter={() => {
                                            // Load progress on-demand if not already loaded and folder exists
                                            if (!group.progress && group.exists && workspacePath) {
                                              loadFolderProgressOnDemand(workspacePath, group.id)
                                            }
                                          }}
                                        >
                                          {/* Checkbox for group selection (only show if group has an ID, multiple groups exist, and not in eval mode) */}
                                          {group.groupId && hasMultipleGroups && workflowMode !== 'eval' && (
                                            <input
                                              type="checkbox"
                                              checked={isGroupChecked}
                                              onChange={(e) => {
                                                e.stopPropagation()
                                                if (group.groupId) {
                                                  // Calculate what the new selection count will be after toggle
                                                  const currentlySelected = selectedGroupIds.includes(group.groupId)
                                                  const willBeSelected = !currentlySelected
                                                  const currentCount = selectedGroupIds.length
                                                  const newSelectedCount = willBeSelected ? currentCount + 1 : currentCount - 1
                                                  
                                                  // Toggle the selection
                                                  toggleGroupSelection(group.groupId)
                                                  
                                                  // If multiple groups will be selected, show parent iteration folder in workspace
                                                  // Otherwise show the specific group folder
                                                  if (newSelectedCount > 1) {
                                                    // Multiple groups selected - use a specific group's folder so loadProgress can find steps_done.json
                                                    if (willBeSelected) {
                                                      // Just checked this group - use it for progress loading
                                                      setSelectedRunFolder(group.id)
                                                    } else {
                                                      // Unchecked a group but >1 remain - use the first remaining group
                                                      const remainingGroupIds = selectedGroupIds.filter(id => id !== group.groupId)
                                                      const firstRemaining = iterationGroups.items.find(
                                                        g => g.groupId && remainingGroupIds.includes(g.groupId) && g.iteration === group.iteration
                                                      )
                                                      setSelectedRunFolder(firstRemaining ? firstRemaining.id : group.iteration)
                                                    }
                                                  } else if (newSelectedCount === 1) {
                                                    // Single group will be selected - set to that group's folder
                                                    const selectedGroupId = willBeSelected ? group.groupId : selectedGroupIds.find(id => id !== group.groupId)
                                                    if (selectedGroupId) {
                                                      const selectedGroup = iterationGroups.items.find(g => g.groupId === selectedGroupId && g.iteration === group.iteration)
                                                      if (selectedGroup) {
                                                        setSelectedRunFolder(selectedGroup.id)
                                                      } else {
                                                        setSelectedRunFolder(group.iteration)
                                                      }
                                                    } else {
                                                      setSelectedRunFolder(group.iteration)
                                                    }
                                                  } else {
                                                    // No groups selected - set to iteration folder
                                                    setSelectedRunFolder(group.iteration)
                                                  }
                                                }
                                              }}
                                              onClick={(e) => e.stopPropagation()}
                                              disabled={isDisabled || isRunning}
                                              className="w-4 h-4 rounded border-border text-primary focus:ring-primary focus:ring-2 cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
                                            />
                                          )}
                                          <button
                                            onClick={(e) => {
                                              e.stopPropagation() // Prevent event from bubbling to parent
                                              // Select only this group when clicking the button (not checkbox)
                                              if (group.groupId) {
                                                setSelectedGroupIds([group.groupId])
                                              }
                                              // Use full group path (e.g., "iteration-5/group-name") to load correct progress
                                              handleSelectRunFolder(group.id)
                                            }}
                                            className={`
                                              flex-1 text-left px-3 py-2 rounded-md text-sm flex items-center gap-2
                                              ${isSelected 
                                                ? 'bg-primary/10 text-primary' 
                                                : isDisabled
                                                ? 'hover:bg-accent text-muted-foreground'
                                                : 'hover:bg-accent text-foreground'
                                              }
                                            `}
                                          >
                                            {/* Only show folder icon if folder exists - checkbox is the primary indicator for selection */}
                                            {group.exists ? (
                                              <FolderOpen className="w-4 h-4" />
                                            ) : (hasMultipleGroups && workflowMode !== 'eval') ? (
                                              // When checkboxes are present and not in eval mode, don't show circle icon (checkbox is the indicator)
                                              null
                                            ) : (
                                              // Show circle if no checkboxes or in eval mode
                                              <Circle className="w-4 h-4" />
                                            )}
                                            <span className="flex-1 text-xs flex items-center gap-1.5">
                                              <span className={group.displayName ? 'font-medium' : 'font-mono'}>
                                                {group.displayName || group.groupId || group.name}
                                              </span>
                                            </span>
                                            {hasProgress && (
                                              <span className="text-xs text-muted-foreground">
                                                {completedCount}/{totalSteps}
                                              </span>
                                            )}
                                            {!group.exists && !hasProgress && (
                                              <span className="text-[10px] text-muted-foreground/70 italic">
                                                not run
                                              </span>
                                            )}
                                            {isSelected && <Check className="w-4 h-4 ml-auto" />}
                                          </button>
                                          {group.exists && (
                                            <button
                                              onClick={(e) => handleDeleteFolderClick(e, group.id)}
                                              className={`
                                                p-1.5 rounded-md text-muted-foreground hover:text-destructive hover:bg-destructive/10
                                                opacity-0 group-hover:opacity-100 transition-opacity
                                              `}
                                            >
                                              <Trash2 className="w-3.5 h-3.5" />
                                            </button>
                                          )}
                                        </div>
                                      )
                                    })}
                                      </>
                                    )}
                                  </div>
                                )
                              })
                            ) : folders.length > 0 && !variablesManifest?.groups?.length ? (
                              // Iterations exist but no variable groups defined
                              <>
                                {folders.map((folder: RunFolder) => {
                                  const progress = folder.progress
                                  const folderCompletedCount = progress?.completed_step_indices?.length || 0
                                  const folderTotalSteps = progress?.total_steps || 0
                                  const hasProgress = progress && folderCompletedCount > 0
                                  const isSelected = selectedRunFolder === folder.name

                                  return (
                                    <button
                                      key={folder.name}
                                      onClick={() => handleSelectRunFolder(folder.name)}
                                      className={`
                                        w-full text-left px-3 py-2 rounded-md text-sm flex items-center gap-2
                                        ${isSelected
                                          ? 'bg-primary/10 text-primary'
                                          : 'hover:bg-accent text-foreground'
                                        }
                                      `}
                                    >
                                      <FolderOpen className="w-4 h-4" />
                                      <span className="flex-1 text-xs font-mono">{folder.name}</span>
                                      {hasProgress && (
                                        <span className="text-[10px] text-muted-foreground">
                                          {folderCompletedCount}/{folderTotalSteps}
                                        </span>
                                      )}
                                    </button>
                                  )
                                })}
                                <div className="border-t border-border mt-1 pt-1 px-3 py-2 text-[10px] text-amber-600 dark:text-amber-400">
                                  No variable groups defined. Add groups in Variables to run per-group iterations.
                                </div>
                              </>
                            ) : (
                              // Fallback: show flat list if no groups (backward compatibility)
                              folders.map((folder: RunFolder) => {
                                const progress = folder.progress
                                const folderCompletedCount = progress?.completed_step_indices?.length || 0
                                const folderTotalSteps = progress?.total_steps || 0
                                const hasProgress = progress && folderCompletedCount > 0
                                
                                return (
                                  <div
                                    key={folder.name}
                                    className={`
                                      group flex items-center gap-1 px-1
                                      ${selectedRunFolder === folder.name 
                                        ? 'bg-primary/10' 
                                        : ''
                                      }
                                    `}
                                    onMouseEnter={() => {
                                      // Load progress on-demand if not already loaded
                                      if (!folder.progress && workspacePath) {
                                        loadFolderProgressOnDemand(workspacePath, folder.name)
                                      }
                                    }}
                                  >
                                    <button
                                      onClick={() => handleSelectRunFolder(folder.name)}
                                      className={`
                                        flex-1 text-left px-3 py-2 rounded-md text-sm flex items-center gap-2
                                        ${selectedRunFolder === folder.name 
                                          ? 'bg-primary/10 text-primary' 
                                          : 'hover:bg-accent text-foreground'
                                        }
                                      `}
                                    >
                                      <FolderOpen className="w-4 h-4" />
                                      <span className="flex-1">{folder.name}</span>
                                      {hasProgress && (
                                        <span className="text-xs text-muted-foreground">
                                          {folderCompletedCount}/{folderTotalSteps}
                                        </span>
                                      )}
                                      {selectedRunFolder === folder.name && <Check className="w-4 h-4 ml-auto" />}
                                    </button>
                                    <button
                                      onClick={(e) => handleDeleteFolderClick(e, folder.name)}
                                      className={`
                                        p-1.5 rounded-md text-muted-foreground hover:text-destructive hover:bg-destructive/10
                                        opacity-0 group-hover:opacity-100 transition-opacity
                                      `}
                                    >
                                      <Trash2 className="w-3.5 h-3.5" />
                                    </button>
                                  </div>
                                )
                              })
                            )}
                          </>
                        ) : !isLoadingWorkspaceState && workspacePath ? (
                          <div className="px-3 py-2 text-xs text-muted-foreground">
                            No existing runs found
                          </div>
                        ) : null}
                      </div>
                    </div>,
                    document.body
                  )}
                </div>

                {/* Progress indicator when existing run selected */}
                {hasExistingProgress && (
                  <div className="flex items-center gap-1 px-1.5 py-0.5 bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300 rounded text-[10px] font-medium">
                    <Check className="w-2.5 h-2.5" />
                    <span>{completedStepCount}/{totalSteps}</span>
                  </div>
                )}

                {/* Dropdown 2: Start Point - Where to start */}
                <div className="relative" ref={startPointDropdownRef}>
                  <button
                    ref={startPointButtonRef}
                    onClick={() => !isRunning && setIsStartPointDropdownOpen(!isStartPointDropdownOpen)}
                    disabled={isRunning}
                    className={`
                      flex items-center gap-1.5 px-2 py-1.5 rounded-md transition-all text-xs font-medium
                      ${isRunning
                        ? 'bg-muted text-muted-foreground cursor-not-allowed' 
                        : 'bg-muted text-foreground hover:bg-accent'
                      }
                    `}
                  >
                    {(() => {
                      const Icon = currentStartPointInfo.icon
                      return <Icon className="w-3.5 h-3.5" />
                    })()}
                    <span className="max-w-[120px] truncate" title={currentStartPointInfo.label}>{currentStartPointInfo.label}</span>
                    <ChevronDown className={`w-3 h-3 transition-transform ${isStartPointDropdownOpen ? 'rotate-180' : ''}`} />
                  </button>
                  
                  {/* Start Point Dropdown - rendered via portal for proper z-index */}
                  {isStartPointDropdownOpen && !isRunning && startPointDropdownPosition && createPortal(
                    <div 
                      data-start-point-dropdown
                      ref={startPointDropdownRef}
                      className="fixed w-64 bg-popover rounded-lg shadow-xl border border-border z-[9999] max-h-[300px] overflow-y-auto"
                      style={{
                        top: `${startPointDropdownPosition.top}px`,
                        left: `${startPointDropdownPosition.left}px`
                      }}
                    >
                      <div className="p-1">
                        <div className="text-xs font-semibold text-muted-foreground uppercase tracking-wider px-3 py-2">
                          Start Point
                        </div>
                        {startPointOptions.map((option: StartPointOption, idx: number) => {
                          const Icon = option.icon
                          const isSelected = option.id === 'from_beginning' 
                            ? (selectedStartPoint === 0 && !selectedBranchStep)
                            : option.id === 'resume_branch'
                            ? (selectedBranchStep !== null && 
                               selectedBranchStep !== undefined &&
                               option.branchStep !== undefined &&
                               option.branchStep !== null &&
                               selectedBranchStep.parentStepIndex === option.branchStep.parentStepIndex &&
                               selectedBranchStep.branchType === option.branchStep.branchType &&
                               selectedBranchStep.branchStepIndex === option.branchStep.branchStepIndex)
                            : selectedStartPoint === option.stepNumber
                          
                          // Check if previous option was not a branch option and this one is (add separator)
                          const prevOption = idx > 0 ? startPointOptions[idx - 1] : null
                          const showBranchSeparator = option.id === 'resume_branch' && prevOption && prevOption.id !== 'resume_branch'
                          
                          return (
                            <div key={`${option.id}-${option.stepNumber || option.branchStep?.parentStepIndex || idx}`}>
                              {showBranchSeparator && (
                                <div className="px-3 py-2">
                                  <div className="text-xs font-semibold text-muted-foreground/70 uppercase tracking-wider">
                                    Branch Resume Options
                                  </div>
                                </div>
                              )}
                            <button
                                onClick={(e) => {
                                  e.preventDefault()
                                  e.stopPropagation()
                                  handleSelectStartPoint(option)
                                }}
                              className={`
                                  w-full text-left px-3 py-2.5 rounded-md transition-colors cursor-pointer
                                ${isSelected 
                                  ? 'bg-primary/10' 
                                  : 'hover:bg-accent'
                                }
                                  ${option.id === 'resume_branch' ? 'border-l-4 border-blue-400 dark:border-blue-500 ml-0' : ''}
                              `}
                                type="button"
                            >
                              <div className="flex items-start gap-3">
                                  <Icon className={`w-4 h-4 mt-0.5 flex-shrink-0 ${isSelected ? 'text-primary' : option.id === 'resume_branch' ? 'text-blue-500 dark:text-blue-400' : 'text-muted-foreground'}`} />
                                <div className="flex-1 min-w-0">
                                    <div className={`font-medium text-sm ${isSelected ? 'text-primary' : option.id === 'resume_branch' ? 'text-blue-700 dark:text-blue-300' : 'text-foreground'}`}>
                                    {option.label}
                                  </div>
                                  <div className="text-xs text-muted-foreground mt-0.5">
                                    {option.description}
                                  </div>
                                </div>
                                  {isSelected && <Check className="w-4 h-4 text-primary mt-0.5 flex-shrink-0" />}
                              </div>
                            </button>
                            </div>
                          )
                        })}
                      </div>
                    </div>,
                    document.body
                  )}
                </div>
              </>
            )}

          </>
        )}
      </div>

      {/* Center - Status indicator */}
      <div className="flex items-center gap-1.5">
        {status === 'waiting_feedback' && (
          <div className="flex items-center gap-1.5 px-2 py-1 bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300 rounded-md text-xs">
            <div className="w-1.5 h-1.5 bg-amber-500 rounded-full animate-pulse" />
            Waiting for feedback
          </div>
        )}
        {status === 'failed' && (
          <div className="flex items-center gap-1.5 px-2 py-1 bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-300 rounded-md text-xs">
            <div className="w-1.5 h-1.5 bg-red-500 rounded-full" />
            Failed
          </div>
        )}
      </div>

      {/* Right side - View controls */}
      <div className="flex items-center gap-1">
        {/* LLM Override Button and Banner */}
        {tempOverrideLLM || tempOverrideLLM2 || tempLearningLLM ? (
          // Active override indicator with toggle and clear button
          <TooltipProvider delayDuration={150}>
            <div className={`flex items-center gap-1 px-2 py-1 bg-secondary border border-border rounded-md shadow-sm ${!tempOverrideLLMEnabled ? 'opacity-60' : ''}`}>
              <div className="flex items-center gap-0.5">
                <Tooltip>
                  <TooltipTrigger asChild>
                    <div className="cursor-help">
                      <Brain className={`w-3.5 h-3.5 ${tempOverrideLLMEnabled && tempOverrideLLM ? 'text-primary fill-primary/20' : 'text-muted-foreground'}`} />
                    </div>
                  </TooltipTrigger>
                  <TooltipContent>
                    <p>{tempOverrideLLM ? `Temp LLM 1: ${tempOverrideLLM.provider}/${tempOverrideLLM.model_id}` : 'Temp LLM 1: not set'}</p>
                    {!tempOverrideLLMEnabled && <p className="text-xs mt-1 text-muted-foreground">(Disabled)</p>}
                  </TooltipContent>
                </Tooltip>
                <span className="text-xs text-muted-foreground">→</span>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <div className="cursor-help">
                      <Brain className={`w-3.5 h-3.5 ${tempOverrideLLMEnabled && tempOverrideLLM2 ? 'text-primary fill-primary/20' : 'text-muted-foreground'}`} />
                    </div>
                  </TooltipTrigger>
                  <TooltipContent>
                    <p>{tempOverrideLLM2 ? `Temp LLM 2: ${tempOverrideLLM2.provider}/${tempOverrideLLM2.model_id}` : 'Temp LLM 2: not set'}</p>
                    {!tempOverrideLLMEnabled && <p className="text-xs mt-1 text-muted-foreground">(Disabled)</p>}
                  </TooltipContent>
                </Tooltip>
                {tempLearningLLM && (
                  <>
                    <span className="text-xs text-muted-foreground">|</span>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <div className="cursor-help">
                          <Brain className="w-3.5 h-3.5 text-primary fill-primary/20" />
                        </div>
                      </TooltipTrigger>
                      <TooltipContent>
                        <p>Temp Learning LLM: {tempLearningLLM.provider}/{tempLearningLLM.model_id}</p>
                        <p className="text-xs mt-1 text-muted-foreground">(Used when learnings exist)</p>
                      </TooltipContent>
                    </Tooltip>
                  </>
                )}
              </div>
              <Tooltip>
                <TooltipTrigger asChild>
                  <button
                    onClick={() => setTempOverrideLLMEnabled(!tempOverrideLLMEnabled)}
                    className={`px-1.5 py-0.5 rounded text-xs font-medium transition-colors ${
                      tempOverrideLLMEnabled
                        ? 'bg-primary/20 text-primary hover:bg-primary/30'
                        : 'bg-muted text-muted-foreground hover:bg-muted/80'
                    }`}
                  >
                    {tempOverrideLLMEnabled ? 'ON' : 'OFF'}
                  </button>
                </TooltipTrigger>
                <TooltipContent side="bottom"><p>{tempOverrideLLMEnabled ? 'Disable overrides' : 'Enable overrides'}</p></TooltipContent>
              </Tooltip>
              <Tooltip>
                <TooltipTrigger asChild>
                  <button
                    onClick={() => {
                      clearTempOverrideLLM()
                      clearTempOverrideLLM2()
                      clearTempLearningLLM()
                    }}
                    className="p-0.5 rounded hover:bg-muted text-muted-foreground hover:text-foreground"
                  >
                    <X className="w-3 h-3" />
                  </button>
                </TooltipTrigger>
                <TooltipContent side="bottom"><p>Clear overrides</p></TooltipContent>
              </Tooltip>
              <Tooltip>
                <TooltipTrigger asChild>
                  <button
                    onClick={() => setShowLLMOverrideModal(true)}
                    className="p-0.5 rounded hover:bg-muted text-muted-foreground hover:text-foreground"
                  >
                    <Settings className="w-3 h-3" />
                  </button>
                </TooltipTrigger>
                <TooltipContent side="bottom"><p>Change overrides</p></TooltipContent>
              </Tooltip>
          </div>
          </TooltipProvider>
        ) : (
          // No override - show button to set one
          <TooltipProvider delayDuration={150}>
            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  onClick={() => setShowLLMOverrideModal(true)}
                  className="p-1.5 rounded-md bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors"
                >
                  <Brain className="w-3.5 h-3.5" />
                </button>
              </TooltipTrigger>
              <TooltipContent side="bottom"><p>LLM override</p></TooltipContent>
            </Tooltip>
          </TooltipProvider>
        )}
        
        <TooltipProvider delayDuration={150}>
        {/* Show Costs - opens popup with cost analysis across all iterations */}
        {workspacePath && (
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={() => setShowCostsPopup(true)}
                className="p-1.5 rounded-md bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors"
              >
                <DollarSign className="w-3.5 h-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="bottom"><p>Costs</p></TooltipContent>
          </Tooltip>
        )}

        {/* Show Execution Logs - opens popup with detailed execution logs */}
        {workspacePath && (
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={() => setShowExecutionLogsPopup(true)}
                className="p-1.5 rounded-md bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors"
              >
                <FileText className="w-3.5 h-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="bottom"><p>Execution logs</p></TooltipContent>
          </Tooltip>
        )}

        {/* Show Learnings - opens popup with learning metadata */}
        {workspacePath && (
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={() => setShowLearningsPopup(true)}
                className="p-1.5 rounded-md bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors"
              >
                <BookOpen className="w-3.5 h-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="bottom"><p>Learnings</p></TooltipContent>
          </Tooltip>
        )}

        {/* Show Evaluation Reports - opens popup with evaluation scores */}
        {workspacePath && (
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={() => setShowEvaluationPopup(true)}
                className="p-1.5 rounded-md bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors"
              >
                <BarChart3 className="w-3.5 h-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="bottom"><p>Evaluation reports</p></TooltipContent>
          </Tooltip>
        )}

        {/* Show Versions - opens popup with version publish/revert */}
        {workspacePath && (
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={() => setShowVersionsPopup(true)}
                className="p-1.5 rounded-md bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors"
              >
                <Package className="w-3.5 h-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="bottom"><p>Versions</p></TooltipContent>
          </Tooltip>
        )}

        {/* Toggle ChatArea Button */}
        {onToggleChatArea && (
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={onToggleChatArea}
                className={`p-1.5 rounded-md transition-colors ${
                  showChatArea
                    ? 'bg-primary/10 text-primary border border-primary/30'
                    : 'bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground'
                }`}
              >
                <MessageSquare className="w-3.5 h-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="bottom"><p>{showChatArea ? 'Hide chat' : 'Show chat'}</p></TooltipContent>
          </Tooltip>
        )}

        {/* Multi-Select Indicator - appears when 2+ steps are selected */}
        {selectedStepIds && selectedStepIds.length >= 2 && (
          <Tooltip>
            <TooltipTrigger asChild>
              <div className="flex items-center gap-1.5 px-2 py-1 rounded-md bg-primary/10 text-primary border border-primary/30">
                <CheckSquare className="w-3.5 h-3.5" />
                <span className="text-xs font-medium">{selectedStepIds.length} Selected</span>
              </div>
            </TooltipTrigger>
            <TooltipContent side="bottom"><p>{selectedStepIds.length} steps selected - configure in sidebar</p></TooltipContent>
          </Tooltip>
        )}

        {/* Bulk Step Config Button */}
        {hasPlan && plan && onBulkUpdateSteps && (
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={() => setShowBulkStepConfigModal(true)}
                className="p-1.5 rounded-md bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors"
              >
                <SlidersHorizontal className="w-3.5 h-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="bottom"><p>Bulk configure steps</p></TooltipContent>
          </Tooltip>
        )}

        {/* Workflow Settings Button — opens the preset settings modal from the top header */}
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              onClick={() => useCommandDialogStore.getState().openDialog('presetSettings')}
              className="p-1.5 rounded-md bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors"
            >
              <Settings className="w-3.5 h-3.5" />
            </button>
          </TooltipTrigger>
          <TooltipContent side="bottom"><p>Settings</p></TooltipContent>
        </Tooltip>
        </TooltipProvider>

        {/* Layout Controls Group - Direction, Save and Reset */}
        {(onSaveLayout || onDeleteLayout) && (
          <>
            <div className="w-px h-5 bg-border mx-0.5" />
            <div className="flex items-center gap-1">
              {/* Layout Direction Toggle */}
              <TooltipProvider delayDuration={150}>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <button
                      onClick={() => {
                        const newDirection = layoutDirection === 'LR' ? 'TB' : 'LR'
                        console.log('[WorkflowToolbar] Layout direction toggled:', newDirection)
                        setLayoutDirection(newDirection)
                      }}
                      className="p-1.5 rounded-md transition-colors hover:bg-accent text-muted-foreground"
                    >
                      {layoutDirection === 'LR' ? (
                        <ArrowRight className="w-3.5 h-3.5" />
                      ) : (
                        <ArrowDown className="w-3.5 h-3.5" />
                      )}
                    </button>
                  </TooltipTrigger>
                  <TooltipContent>
                    {layoutDirection === 'LR' ? 'Horizontal layout (click for vertical)' : 'Vertical layout (click for horizontal)'}
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>

              {/* Save Layout Button */}
              {onSaveLayout && (
                <TooltipProvider delayDuration={150}>
                  <Tooltip>
                    <TooltipTrigger asChild>
                    <button
                      onClick={async () => {
                        console.log('[WorkflowToolbar] Save button clicked')
                        if (onSaveLayout && !isSavingLayout) {
                          try {
                            await onSaveLayout()
                          } catch (error) {
                            console.error('[WorkflowToolbar] Error saving layout:', error)
                          }
                        }
                      }}
                      disabled={isSavingLayout}
                      className={`p-1.5 rounded-md transition-colors ${
                        isSavingLayout
                          ? 'bg-muted text-muted-foreground cursor-not-allowed'
                          : hasUnsavedLayoutChanges
                          ? 'bg-blue-100 dark:bg-blue-900/30 text-blue-600 dark:text-blue-400 hover:bg-blue-200 dark:hover:bg-blue-900/50 animate-pulse'
                          : 'hover:bg-accent text-muted-foreground'
                      }`}
                    >
                      {isSavingLayout ? (
                        <Loader2 className="w-3.5 h-3.5 animate-spin" />
                      ) : (
                        <Save className={`w-3.5 h-3.5 ${hasUnsavedLayoutChanges ? 'text-blue-600 dark:text-blue-400' : ''}`} />
                      )}
                    </button>
                  </TooltipTrigger>
                  <TooltipContent>
                    {isSavingLayout ? 'Saving layout...' : (hasUnsavedLayoutChanges ? 'Save layout (unsaved changes)' : 'Save layout')}
                  </TooltipContent>
                </Tooltip>
                </TooltipProvider>
              )}
              
              {/* Delete/Reset Layout Button */}
              {onDeleteLayout && (
                <TooltipProvider delayDuration={150}>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <button
                        onClick={async () => {
                          console.log('[WorkflowToolbar] Delete layout button clicked')
                          if (onDeleteLayout && !isDeletingLayout) {
                            // Confirm before deleting
                            if (window.confirm('Are you sure you want to delete the saved layout and reset to default? This cannot be undone.')) {
                              try {
                                await onDeleteLayout()
                              } catch (error) {
                                console.error('[WorkflowToolbar] Error deleting layout:', error)
                              }
                            }
                          }
                        }}
                        disabled={isDeletingLayout}
                        className={`p-1.5 rounded-md transition-colors ${
                          isDeletingLayout
                            ? 'bg-muted text-muted-foreground cursor-not-allowed'
                            : 'hover:bg-orange-100 dark:hover:bg-orange-900/30 text-orange-600 dark:text-orange-400 hover:text-orange-700 dark:hover:text-orange-300'
                        }`}
                      >
                        {isDeletingLayout ? (
                          <Loader2 className="w-3.5 h-3.5 animate-spin" />
                        ) : (
                          <RotateCcw className="w-3.5 h-3.5" />
                        )}
                      </button>
                    </TooltipTrigger>
                    <TooltipContent>
                      {isDeletingLayout ? 'Resetting layout...' : 'Reset layout to default'}
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
              )}
            </div>
          </>
        )}
      </div>
    </div>
    {/* Delete Confirmation Dialog */}
    <ConfirmationDialog
      isOpen={deleteDialog.isOpen}
      onClose={() => setDeleteDialog({ isOpen: false, folderName: null, isLoading: false })}
      onConfirm={handleDeleteFolderConfirm}
      title="Delete Iteration Folder"
      message={`Are you sure you want to delete "${deleteDialog.folderName}"? This will permanently delete the folder and all its contents (execution results, validation outputs, etc.). This action cannot be undone.`}
      confirmText="Delete"
      cancelText="Cancel"
      type="danger"
      isLoading={deleteDialog.isLoading}
    />
    
    {/* LLM Override Modal */}
    <LLMOverrideModal
      isOpen={showLLMOverrideModal}
      onClose={() => setShowLLMOverrideModal(false)}
    />
    
    {/* Bulk Step Config Modal */}
    {onBulkUpdateSteps && (
      <BulkStepConfigModal
        isOpen={showBulkStepConfigModal}
        onClose={() => setShowBulkStepConfigModal(false)}
        plan={plan || null}
        stepOverride={stepOverride || null}
        onSaveStepOverride={onSaveStepOverride || (async () => {})}
      />
    )}

    {/* Learnings Popup */}
    <LearningsPopup
      isOpen={showLearningsPopup}
      onClose={() => setShowLearningsPopup(false)}
      workspacePath={workspacePath || null}
      plan={plan || null}
    />

    {/* Costs Popup */}
    <CostsPopup
      isOpen={showCostsPopup}
      onClose={() => setShowCostsPopup(false)}
      workspacePath={workspacePath || null}
      runFolders={runFoldersNames}
      selectedRunFolder={contextRunFolder}
    />

    {/* Execution Logs Popup */}
    <ExecutionLogsPopup
      isOpen={showExecutionLogsPopup}
      onClose={() => setShowExecutionLogsPopup(false)}
      workspacePath={workspacePath || null}
      runFolder={contextRunFolder}
      runFolders={runFoldersNames}
    />

    {/* Evaluation Reports Popup */}
    <EvaluationPopup
      isOpen={showEvaluationPopup}
      onClose={() => setShowEvaluationPopup(false)}
      workspacePath={workspacePath || null}
      selectedRunFolder={contextRunFolder}
    />

    {/* Workflow Versions Popup */}
    <WorkflowVersionsPopup
      isOpen={showVersionsPopup}
      onClose={() => setShowVersionsPopup(false)}
      workspacePath={workspacePath || null}
      onRefresh={async () => {
        if (onRefresh) await onRefresh()
        fetchFiles()
      }}
    />
    </>
  )
}

WorkflowToolbar.whyDidYouRender = true

export default WorkflowToolbar
