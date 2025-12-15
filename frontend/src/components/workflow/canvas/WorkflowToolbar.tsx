import React, { useEffect, useRef, useMemo, useCallback, useState } from 'react'
import { 
  Play, 
  Square, 
  Plus,
  Maximize2,
  ZoomIn,
  ZoomOut,
  Loader2,
  ChevronDown,
  Check,
  Rocket,
  FolderOpen,
  Zap,
  SkipForward,
  RefreshCw,
  BookOpen,
  FolderTree,
  Trash2,
  Settings,
  X,
  Brain,
  MessageSquare,
  Circle
} from 'lucide-react'
import { useWorkspaceStore } from '../../../stores/useWorkspaceStore'
import { useAppStore } from '../../../stores'
import { useWorkflowStore, type ExecutionModeType, type RunFolder } from '../../../stores/useWorkflowStore'
import type { PlannerFile, WorkflowPhase, StepProgress } from '../../../services/api-types'
import type { PlanningResponse } from '../../../utils/stepConfigMatching'
import type { WorkflowExecutionStatus } from '../hooks/useWorkflowExecution'
import type { ExecutionOptions } from '../../../services/api-types'
import { agentApi } from '../../../services/api'
import ConfirmationDialog from '../../ui/ConfirmationDialog'
import LLMOverrideModal from '../LLMOverrideModal'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../../ui/tooltip'

// Execution phase ID - special phase that should be displayed separately
const EXECUTION_PHASE_ID = 'execution'

// Execution Mode options - how to run (human feedback, learning, etc.)
const EXECUTION_MODE_OPTIONS: { id: ExecutionModeType; label: string; icon: typeof Play; description: string }[] = [
  { id: 'human_approval', label: 'With Human Approval', icon: Play, description: 'Pause for human approval before going to next step (learning enabled)' },
  { id: 'fast_execution', label: 'Fast Execution', icon: Zap, description: 'Execute all without pausing or learning' },
  { id: 'with_learning', label: 'With Learning (No Human)', icon: SkipForward, description: 'Proceed to next step without human approval (learning enabled)' },
]

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
  onStartPhase: (phaseId: string, executionOptions?: ExecutionOptions) => void
  onStop: () => void
  onCreatePlan: () => void
  onZoomIn: () => void
  onZoomOut: () => void
  onFitView: () => void
  onProgressChange?: (completedStepIndices: number[]) => void  // Callback when step progress changes
  showChatArea?: boolean
  onToggleChatArea?: () => void
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
  onStartPhase,
  onStop,
  onCreatePlan,
  onZoomIn,
  onZoomOut,
  onFitView,
  onProgressChange,
  showChatArea = false,
  onToggleChatArea,
  className = ''
}) => {
  // Workspace store for opening folders
  const { setShowFileContent, highlightFile, fetchFiles } = useWorkspaceStore()
  // App store for toggling workspace visibility
  const { setWorkspaceMinimized } = useAppStore()
  
  // Workflow store - use selectors to ensure proper reactivity
  const phases = useWorkflowStore(state => state.phases)
  const isLoadingPhases = useWorkflowStore(state => state.isLoadingPhases)
  const phasesInitialized = useWorkflowStore(state => state.phasesInitialized)
  const loadPhases = useWorkflowStore(state => state.loadPhases)
  const runFolders = useWorkflowStore(state => state.runFolders)
  const selectedRunFolder = useWorkflowStore(state => state.selectedRunFolder)
  const isLoadingRunFolders = useWorkflowStore(state => state.isLoadingRunFolders)
  const loadRunFolders = useWorkflowStore(state => state.loadRunFolders)
  const setSelectedRunFolder = useWorkflowStore(state => state.setSelectedRunFolder)
  const stepProgress = useWorkflowStore(state => state.stepProgress)
  const loadProgress = useWorkflowStore(state => state.loadProgress)
  const loadFolderProgressOnDemand = useWorkflowStore(state => state.loadFolderProgressOnDemand)
  const getCompletedStepIndices = useWorkflowStore(state => state.getCompletedStepIndices)
  const selectedExecutionMode = useWorkflowStore(state => state.selectedExecutionMode)
  const selectedStartPoint = useWorkflowStore(state => state.selectedStartPoint)
  const selectedBranchStep = useWorkflowStore(state => state.selectedBranchStep)
  const setExecutionMode = useWorkflowStore(state => state.setExecutionMode)
  const setStartPoint = useWorkflowStore(state => state.setStartPoint)
  const setBranchStep = useWorkflowStore(state => state.setBranchStep)
  const buildExecutionOptions = useWorkflowStore(state => state.buildExecutionOptions)
  const loadSavedSettings = useWorkflowStore(state => state.loadSavedSettings)
  const saveSettings = useWorkflowStore(state => state.saveSettings)
  const tempOverrideLLM = useWorkflowStore(state => state.tempOverrideLLM)
  const tempOverrideLLM2 = useWorkflowStore(state => state.tempOverrideLLM2)
  const tempOverrideLLMEnabled = useWorkflowStore(state => state.tempOverrideLLMEnabled)
  const setTempOverrideLLMEnabled = useWorkflowStore(state => state.setTempOverrideLLMEnabled)
  const clearTempOverrideLLM = useWorkflowStore(state => state.clearTempOverrideLLM)
  const clearTempOverrideLLM2 = useWorkflowStore(state => state.clearTempOverrideLLM2)
  const variablesManifest = useWorkflowStore(state => state.variablesManifest)
  
  // LLM Override modal state
  const [showLLMOverrideModal, setShowLLMOverrideModal] = useState(false)
  
  // Helper function to find a folder in the file tree
  const findFolderInTree = (fileList: PlannerFile[], targetPath: string): PlannerFile | null => {
    for (const file of fileList) {
      // Check if this is the folder we're looking for
      if ((file.filepath === targetPath || file.originalFilepath === targetPath) && 
          file.type === 'folder') {
        return file
      }
      // Also check if targetPath ends with this folder's path (for nested paths)
      if (file.filepath && (targetPath.endsWith(file.filepath) || file.filepath.endsWith(targetPath))) {
        if (file.type === 'folder') {
          return file
        }
      }
      // Recurse into children
      if (file.children && file.children.length > 0) {
        const found = findFolderInTree(file.children, targetPath)
        if (found) return found
      }
    }
    return null
  }
  
  // Local UI state (dropdowns)
  const [isDropdownOpen, setIsDropdownOpen] = React.useState(false)
  const [isIterationDropdownOpen, setIsIterationDropdownOpen] = React.useState(false)
  const [isExecutionModeDropdownOpen, setIsExecutionModeDropdownOpen] = React.useState(false)
  const [isStartPointDropdownOpen, setIsStartPointDropdownOpen] = React.useState(false)
  
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
  
  // Loading state for creating new folder
  const [isCreatingFolder, setIsCreatingFolder] = React.useState(false)
  
  // Refs for dropdown click-outside detection
  const dropdownRef = useRef<HTMLDivElement>(null)
  const iterationDropdownRef = useRef<HTMLDivElement>(null)
  const executionModeDropdownRef = useRef<HTMLDivElement>(null)
  const startPointDropdownRef = useRef<HTMLDivElement>(null)
  
  const isRunning = status === 'running'

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

  // Save settings when they change
  useEffect(() => {
    if (presetQueryId) {
      saveSettings(presetQueryId)
    }
  }, [presetQueryId, selectedRunFolder, selectedExecutionMode, selectedStartPoint, saveSettings])

  // Load run folders when workspace path changes
  useEffect(() => {
    if (workspacePath) {
      loadRunFolders(workspacePath)
    }
  }, [workspacePath, loadRunFolders])

  // Load progress when selected run folder changes
  useEffect(() => {
    if (workspacePath && selectedRunFolder !== 'new') {
      loadProgress(workspacePath, selectedRunFolder)
    }
  }, [workspacePath, selectedRunFolder, loadProgress])

  // Notify parent when step progress changes
  useEffect(() => {
    if (onProgressChange) {
      const completedIndices = getCompletedStepIndices()
      onProgressChange(completedIndices)
    }
  }, [stepProgress, onProgressChange, getCompletedStepIndices])

  // Close dropdowns when clicking outside
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setIsDropdownOpen(false)
      }
      if (iterationDropdownRef.current && !iterationDropdownRef.current.contains(event.target as Node)) {
        setIsIterationDropdownOpen(false)
      }
      if (executionModeDropdownRef.current && !executionModeDropdownRef.current.contains(event.target as Node)) {
        setIsExecutionModeDropdownOpen(false)
      }
      if (startPointDropdownRef.current && !startPointDropdownRef.current.contains(event.target as Node)) {
        setIsStartPointDropdownOpen(false)
      }
    }
    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [])

  // Separate execution phase from other phases
  // Only calculate when phases are actually loaded (not empty and not loading)
  const { executionPhase, otherPhases } = useMemo(() => {
    // Don't calculate if phases aren't loaded yet
    if (isLoadingPhases || phases.length === 0) {
      return {
        executionPhase: undefined,
        otherPhases: []
      }
    }
    const execPhase = phases.find((p: WorkflowPhase) => p.id === EXECUTION_PHASE_ID)
    const others = phases.filter((p: WorkflowPhase) => p.id !== EXECUTION_PHASE_ID)
    return {
      executionPhase: execPhase,
      otherPhases: others
    }
  }, [phases, isLoadingPhases])

  // Close dropdown if phases become unavailable
  useEffect(() => {
    if (isDropdownOpen && (isLoadingPhases || otherPhases.length === 0)) {
      setIsDropdownOpen(false)
    }
  }, [isDropdownOpen, isLoadingPhases, otherPhases.length])

  // Calculate progress info
  const completedStepIndices = getCompletedStepIndices()
  const hasExistingProgress = stepProgress !== null && completedStepIndices.length > 0
  const completedStepCount = completedStepIndices.length

  // Get current execution mode info
  const currentModeInfo = EXECUTION_MODE_OPTIONS.find(m => m.id === selectedExecutionMode) || EXECUTION_MODE_OPTIONS[0]

  // Build merged list of iterations and groups
  // Combines existing folders with groups from manifest
  const iterationGroups = useMemo(() => {
    interface GroupItem {
      id: string  // Full path like "iteration-1/group-5" or just "iteration-1"
      name: string  // Display name like "group-5" or "iteration-1"
      iteration: string  // e.g., "iteration-1"
      groupId: string | null  // e.g., "group-5" or null if no group
      progress: StepProgress | null
      exists: boolean  // Whether folder exists
      enabled: boolean  // Whether group is enabled (null if not a group)
    }

    const items: GroupItem[] = []
    const iterationMap = new Map<string, GroupItem[]>()

    // Add existing folders
    runFolders.forEach((folder) => {
      const parts = folder.name.split('/')
      if (parts.length === 2 && parts[1].startsWith('group-')) {
        // Group folder: iteration-X/group-Y
        const iteration = parts[0]
        const groupId = parts[1]
        const item: GroupItem = {
          id: folder.name,
          name: groupId,
          iteration,
          groupId,
          progress: folder.progress || null,
          exists: true,
          enabled: true  // Default, will be updated from manifest if available
        }
        items.push(item)
        if (!iterationMap.has(iteration)) {
          iterationMap.set(iteration, [])
        }
        iterationMap.get(iteration)!.push(item)
      } else if (parts.length === 1 && parts[0].startsWith('iteration-')) {
        // Top-level iteration folder (no groups or single group mode)
        const iteration = parts[0]
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
        if (!iterationMap.has(iteration)) {
          iterationMap.set(iteration, [])
        }
        iterationMap.get(iteration)!.push(item)
      }
    })

    // Add groups from manifest that don't have folders yet
    if (variablesManifest?.groups && variablesManifest.groups.length > 0) {
      // Find the highest iteration number from existing folders
      let maxIteration = 0
      runFolders.forEach((folder) => {
        const match = folder.name.match(/iteration-(\d+)/)
        if (match) {
          const num = parseInt(match[1], 10)
          if (num > maxIteration) {
            maxIteration = num
          }
        }
      })
      
      // Use the latest iteration, or iteration-1 if none exist
      // This is the default iteration for groups that haven't been run yet
      const defaultIteration = maxIteration > 0 ? `iteration-${maxIteration}` : 'iteration-1'

      variablesManifest.groups.forEach((group) => {
        // Check if this group already exists in items (has a folder)
        const existingItem = items.find(item => item.groupId === group.group_id)
        if (!existingItem) {
          // Group doesn't have a folder yet - add it to the default iteration
          const item: GroupItem = {
            id: `${defaultIteration}/${group.group_id}`,
            name: group.group_id,
            iteration: defaultIteration,
            groupId: group.group_id,
            progress: null,
            exists: false,
            enabled: group.enabled
          }
          items.push(item)
          if (!iterationMap.has(defaultIteration)) {
            iterationMap.set(defaultIteration, [])
          }
          iterationMap.get(defaultIteration)!.push(item)
        } else {
          // Update enabled status from manifest for existing group
          existingItem.enabled = group.enabled
        }
      })
    }

    // Sort iterations by number (descending)
    const sortedIterations = Array.from(iterationMap.keys()).sort((a, b) => {
      const numA = parseInt(a.replace('iteration-', '')) || 0
      const numB = parseInt(b.replace('iteration-', '')) || 0
      return numB - numA
    })

    return { sortedIterations, iterationMap, items }
  }, [runFolders, variablesManifest])
  
  // Generate start point options based on completed steps and branch steps
  const startPointOptions = useMemo((): StartPointOption[] => {
    const options: StartPointOption[] = [
      { id: 'from_beginning', label: 'Start from Beginning', icon: Play, description: 'Execute all steps from start' }
    ]
    
    // Add resume options for all completed steps plus the next step after all completed
    if (completedStepIndices.length > 0 && totalSteps > 0) {
      // Convert 0-based indices to 1-based step numbers
      const completedStepNumbers = completedStepIndices.map(idx => idx + 1).sort((a, b) => a - b)
      const lastCompletedStep = completedStepNumbers[completedStepNumbers.length - 1]
      const nextStep = lastCompletedStep + 1
      
      // Add all completed steps as resume options
      completedStepNumbers.forEach(stepNum => {
        options.push({
          id: 'resume',
          stepNumber: stepNum,
          label: `Start Again from Step ${stepNum}`,
          icon: RefreshCw,
          description: `Start again from step ${stepNum} (${completedStepCount} completed)`
        })
      })
      
      // Add next step if it exists (resume from after all completed steps)
      // This is a new step that will run, so it says "Resume" not "Start Again"
      if (nextStep <= totalSteps) {
        options.push({
          id: 'resume',
          stepNumber: nextStep,
          label: `Resume from Step ${nextStep}`,
          icon: RefreshCw,
          description: `Resume from step ${nextStep} (after ${completedStepCount} completed steps)`
        })
      }
    }
    
    // Add branch step resume options for conditional steps with incomplete branches
    if (plan && stepProgress?.branch_steps && Object.keys(stepProgress.branch_steps).length > 0) {
      console.log('[WorkflowToolbar] Processing branch steps:', stepProgress.branch_steps)
      Object.entries(stepProgress.branch_steps).forEach(([stepIndexStr, branchProgress]) => {
        const parentStepIndex = parseInt(stepIndexStr, 10)
        const parentStep = plan.steps?.[parentStepIndex]
        
        // Only process conditional steps
        if (!parentStep || !parentStep.has_condition) {
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
    
    console.log(`[WorkflowToolbar] Generated ${options.length} start point options:`, options.map(o => ({ id: o.id, label: o.label, hasBranchStep: !!o.branchStep })))
    return options
  }, [completedStepIndices, totalSteps, completedStepCount, plan, stepProgress])

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
    return startPointOptions.find(o => o.stepNumber === selectedStartPoint) || startPointOptions[0]
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

  // Get current phase details
  const currentPhaseDetails = phases.find((p: WorkflowPhase) => p.id === currentPhase)
  // Only consider it execution phase if currentPhase is explicitly set to 'execution'
  // If currentPhase is undefined/null, allow dropdown to be enabled
  const isExecutionPhase = currentPhase === EXECUTION_PHASE_ID

  // Allow dropdown even when in execution phase - user should be able to switch to other phases
  const dropdownDisabled = isRunning || isLoadingPhases || otherPhases.length === 0

  // Handle phase selection
  const handleSelectPhase = (phaseId: string) => {
    setIsDropdownOpen(false)
    if (!isRunning) {
      onStartPhase(phaseId)
    }
  }

  // Handle selecting execution mode from dropdown
  const handleSelectExecutionMode = useCallback((modeId: ExecutionModeType) => {
    setExecutionMode(modeId)
    setIsExecutionModeDropdownOpen(false)
  }, [setExecutionMode])
  
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
  const handleSelectRunFolder = useCallback(async (folder: string) => {
    // If "new" is selected, create a new iteration folder via API
    if (folder === 'new' && workspacePath) {
      setIsCreatingFolder(true)
      try {
        // Create new folder via API
        const response = await agentApi.createRunFolder(workspacePath)
        
        if (response.success && response.folder_name) {
          // Select the newly created folder FIRST (before loading folders)
          // This ensures the selection is set immediately and won't be reset by validation
          setSelectedRunFolder(response.folder_name)
          
          // Refresh folder list to include the new folder
          await loadRunFolders(workspacePath)
          
          // Load progress for the new folder (will be empty, but ensures consistency)
          await loadProgress(workspacePath, response.folder_name)
          
          // Refresh workspace files to show the new folder
          await fetchFiles()
        } else {
          console.error('[WorkflowToolbar] Failed to create folder:', response)
          // Fallback: still set to 'new' so user can try again
          setSelectedRunFolder('new')
        }
      } catch (error) {
        console.error('[WorkflowToolbar] Error creating new folder:', error)
        // Fallback: still set to 'new' so user can try again
        setSelectedRunFolder('new')
      } finally {
        setIsCreatingFolder(false)
      }
    } else {
      // Regular folder selection
      setSelectedRunFolder(folder)
    }
    
    setIsIterationDropdownOpen(false)
  }, [setSelectedRunFolder, workspacePath, loadRunFolders, loadProgress, fetchFiles])

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

  // Handle execution button click
  const handleExecute = useCallback(() => {
    if (!isRunning && executionPhase) {
      const options = buildExecutionOptions()
      console.log('[RESUME_DEBUG] 🚀 Starting execution with options:', JSON.stringify({
        execution_strategy: options.execution_strategy,
        resume_from_step: options.resume_from_step,
        resume_from_branch_step: options.resume_from_branch_step
      }, null, 2))
      onStartPhase(executionPhase.id, options)
    }
  }, [isRunning, executionPhase, buildExecutionOptions, onStartPhase])

  return (
    <>
    <div className={`
      flex items-center justify-between gap-2 px-3 py-1.5 
      bg-white dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700
      relative z-10
      ${className}
    `}>
      {/* Left side - Phase selector */}
      <div className="flex items-center gap-2">
        {!hasPlan ? (
          // No plan - show create button
          <button
            onClick={onCreatePlan}
            className="flex items-center gap-1.5 px-2.5 py-1.5 bg-gray-100 dark:bg-gray-700 text-gray-900 dark:text-gray-100 rounded-md hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors font-medium text-xs"
          >
            <Plus className="w-3.5 h-3.5" />
            Create Plan
          </button>
        ) : (
          <>
            {/* Execution Controls - Execute button first, then configuration dropdowns */}
            {executionPhase && (
              <>
                {/* Execute/Stop Button - Changes to Stop when running */}
                <button
                  onClick={isRunning ? onStop : handleExecute}
                  disabled={false}
                  className={`
                    flex items-center gap-1.5 px-2.5 py-1.5 rounded-md transition-all text-xs font-semibold
                    ${isRunning
                      ? 'bg-red-500 dark:bg-red-600 text-white shadow-md hover:bg-red-600 dark:hover:bg-red-700 hover:shadow-lg'
                      : 'bg-purple-500 dark:bg-purple-600 text-white shadow-md hover:bg-purple-600 dark:hover:bg-purple-700 hover:shadow-lg'
                    }
                  `}
                  title={isRunning ? 'Stop execution' : `Execute: ${currentModeInfo.label}`}
                >
                  {isRunning ? (
                    <>
                      <Square className="w-3.5 h-3.5" />
                      <span>Stop</span>
                    </>
                  ) : (
                    <>
                      <Rocket className="w-3.5 h-3.5" />
                      <span>Execute</span>
                    </>
                  )}
                </button>
                
                <div className="w-px h-5 bg-border" />
                
                {/* Iteration Selector */}
                <div className="relative" ref={iterationDropdownRef}>
                  <button
                    onClick={() => !isRunning && setIsIterationDropdownOpen(!isIterationDropdownOpen)}
                    disabled={isRunning || isLoadingRunFolders}
                    className={`
                      flex items-center gap-1.5 px-2 py-1.5 rounded-md transition-all text-xs font-medium
                      ${isRunning
                        ? 'bg-muted text-muted-foreground cursor-not-allowed' 
                        : 'bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600'
                      }
                    `}
                    title="Select iteration folder"
                  >
                    <FolderOpen className="w-3.5 h-3.5" />
                    <span className="max-w-[80px] truncate">
                      {isLoadingRunFolders ? 'Loading...' : selectedRunFolder === 'new' ? 'New Run' : selectedRunFolder}
                    </span>
                    <ChevronDown className={`w-3 h-3 transition-transform ${isIterationDropdownOpen ? 'rotate-180' : ''}`} />
                  </button>
                  
                  {/* Iteration Dropdown */}
                  {isIterationDropdownOpen && !isRunning && (
                    <div className="absolute top-full left-0 mt-1 w-56 bg-white dark:bg-gray-800 rounded-lg shadow-xl border border-gray-200 dark:border-gray-700 z-50 max-h-[300px] overflow-y-auto">
                      <div className="p-1">
                        {/* New Run option */}
                        <button
                          onClick={() => handleSelectRunFolder('new')}
                          disabled={isCreatingFolder}
                          className={`
                            w-full text-left px-3 py-2 rounded-md text-sm flex items-center gap-2
                            ${selectedRunFolder === 'new' 
                              ? 'bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300' 
                              : 'hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-700 dark:text-gray-300'
                            }
                            ${isCreatingFolder ? 'opacity-50 cursor-not-allowed' : ''}
                          `}
                        >
                          {isCreatingFolder ? (
                            <Loader2 className="w-4 h-4 animate-spin" />
                          ) : (
                            <Plus className="w-4 h-4" />
                          )}
                          <span className="font-medium">
                            {isCreatingFolder ? 'Creating...' : 'New Run'}
                          </span>
                          {selectedRunFolder === 'new' && !isCreatingFolder && <Check className="w-4 h-4 ml-auto" />}
                        </button>
                        
                        {(iterationGroups.sortedIterations.length > 0 || runFolders.length > 0) ? (
                          <>
                            <div className="border-t border-gray-200 dark:border-gray-700 my-1" />
                            <div className="text-xs font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider px-3 py-1">
                              {iterationGroups.sortedIterations.length > 0 ? 'Iterations & Groups' : `Existing Runs (${runFolders.length})`}
                            </div>
                            {iterationGroups.sortedIterations.length > 0 ? (
                              // Show grouped by iteration
                              iterationGroups.sortedIterations.map((iteration) => {
                                const groups = iterationGroups.iterationMap.get(iteration) || []
                                const hasGroups = groups.some(g => g.groupId !== null)
                                
                                return (
                                  <div key={iteration}>
                                    {/* Iteration header (only if it has groups or is a top-level folder) */}
                                    {hasGroups ? (
                                      <div className="px-3 py-1.5 text-xs font-semibold text-gray-600 dark:text-gray-400 bg-gray-50 dark:bg-gray-900/50">
                                        {iteration}
                                      </div>
                                    ) : null}
                                    
                                    {/* Groups under this iteration */}
                                    {groups.map((group) => {
                                      const progress = group.progress
                                      const completedCount = progress?.completed_step_indices?.length || 0
                                      const totalSteps = progress?.total_steps || 0
                                      const hasProgress = progress && completedCount > 0
                                      const isSelected = selectedRunFolder === group.id
                                      const isDisabled = group.enabled === false
                                      
                                      return (
                                        <div
                                          key={group.id}
                                          className={`
                                            group flex items-center gap-1 px-1
                                            ${isSelected 
                                              ? 'bg-purple-100 dark:bg-purple-900/30' 
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
                                          <button
                                            onClick={() => handleSelectRunFolder(group.id)}
                                            className={`
                                              flex-1 text-left px-3 py-2 rounded-md text-sm flex items-center gap-2
                                              ${isSelected 
                                                ? 'bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300' 
                                                : isDisabled
                                                ? 'hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-500 dark:text-gray-400'
                                                : 'hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-700 dark:text-gray-300'
                                              }
                                            `}
                                            title={isDisabled ? 'Group is disabled' : group.exists ? undefined : 'Group not run yet'}
                                          >
                                            {group.exists ? (
                                              <FolderOpen className="w-4 h-4" />
                                            ) : (
                                              <Circle className="w-4 h-4" />
                                            )}
                                            <span className="flex-1 font-mono text-xs">
                                              {group.groupId || group.name}
                                            </span>
                                            {hasProgress && (
                                              <span className="text-xs text-gray-500 dark:text-gray-400">
                                                {completedCount}/{totalSteps}
                                              </span>
                                            )}
                                            {!group.exists && !hasProgress && (
                                              <span className="text-[10px] text-gray-400 dark:text-gray-500 italic">
                                                not run
                                              </span>
                                            )}
                                            {isSelected && <Check className="w-4 h-4 ml-auto" />}
                                          </button>
                                          {group.exists && (
                                            <button
                                              onClick={(e) => handleDeleteFolderClick(e, group.id)}
                                              className={`
                                                p-1.5 rounded-md text-gray-400 hover:text-red-600 hover:bg-red-50 dark:hover:bg-red-900/20
                                                opacity-0 group-hover:opacity-100 transition-opacity
                                              `}
                                              title={`Delete ${group.id}`}
                                            >
                                              <Trash2 className="w-3.5 h-3.5" />
                                            </button>
                                          )}
                                        </div>
                                      )
                                    })}
                                  </div>
                                )
                              })
                            ) : (
                              // Fallback: show flat list if no groups (backward compatibility)
                              runFolders.map((folder: RunFolder) => {
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
                                        ? 'bg-purple-100 dark:bg-purple-900/30' 
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
                                          ? 'bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300' 
                                          : 'hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-700 dark:text-gray-300'
                                        }
                                      `}
                                    >
                                      <FolderOpen className="w-4 h-4" />
                                      <span className="flex-1">{folder.name}</span>
                                      {hasProgress && (
                                        <span className="text-xs text-gray-500 dark:text-gray-400">
                                          {folderCompletedCount}/{folderTotalSteps}
                                        </span>
                                      )}
                                      {selectedRunFolder === folder.name && <Check className="w-4 h-4 ml-auto" />}
                                    </button>
                                    <button
                                      onClick={(e) => handleDeleteFolderClick(e, folder.name)}
                                      className={`
                                        p-1.5 rounded-md text-gray-400 hover:text-red-600 hover:bg-red-50 dark:hover:bg-red-900/20
                                        opacity-0 group-hover:opacity-100 transition-opacity
                                      `}
                                      title={`Delete ${folder.name}`}
                                    >
                                      <Trash2 className="w-3.5 h-3.5" />
                                    </button>
                                  </div>
                                )
                              })
                            )}
                          </>
                        ) : !isLoadingRunFolders && workspacePath ? (
                          <div className="px-3 py-2 text-xs text-gray-500 dark:text-gray-400">
                            No existing runs found
                          </div>
                        ) : null}
                      </div>
                    </div>
                  )}
                </div>

                {/* Progress indicator when existing run selected */}
                {hasExistingProgress && (
                  <div className="flex items-center gap-1 px-1.5 py-0.5 bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300 rounded text-[10px] font-medium">
                    <Check className="w-2.5 h-2.5" />
                    <span>{completedStepCount}/{totalSteps}</span>
                  </div>
                )}

                {/* Dropdown 2: Execution Mode - How to run */}
                <div className="relative" ref={executionModeDropdownRef}>
                  <button
                    onClick={() => !isRunning && setIsExecutionModeDropdownOpen(!isExecutionModeDropdownOpen)}
                    disabled={isRunning}
                    className={`
                      flex items-center gap-1.5 px-2 py-1.5 rounded-md transition-all text-xs font-medium
                      ${isRunning
                        ? 'bg-muted text-muted-foreground cursor-not-allowed' 
                        : 'bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600'
                      }
                    `}
                    title="Select execution mode"
                  >
                    {(() => {
                      const Icon = currentModeInfo.icon
                      return <Icon className="w-3.5 h-3.5" />
                    })()}
                    <span className="max-w-[110px] truncate">{currentModeInfo.label}</span>
                    <ChevronDown className={`w-3 h-3 transition-transform ${isExecutionModeDropdownOpen ? 'rotate-180' : ''}`} />
                  </button>
                  
                  {/* Execution Mode Dropdown */}
                  {isExecutionModeDropdownOpen && !isRunning && (
                    <div className="absolute top-full left-0 mt-1 w-64 bg-white dark:bg-gray-800 rounded-lg shadow-xl border border-gray-200 dark:border-gray-700 z-50">
                      <div className="p-1">
                        <div className="text-xs font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider px-3 py-2">
                          Execution Mode
                        </div>
                        {EXECUTION_MODE_OPTIONS.map((mode) => {
                          const Icon = mode.icon
                          const isSelected = mode.id === selectedExecutionMode
                          return (
                            <button
                              key={mode.id}
                              onClick={() => handleSelectExecutionMode(mode.id)}
                              className={`
                                w-full text-left px-3 py-2.5 rounded-md transition-colors
                                ${isSelected 
                                  ? 'bg-purple-100 dark:bg-purple-900/30' 
                                  : 'hover:bg-gray-100 dark:hover:bg-gray-700'
                                }
                              `}
                            >
                              <div className="flex items-start gap-3">
                                <Icon className={`w-4 h-4 mt-0.5 ${isSelected ? 'text-purple-600 dark:text-purple-400' : 'text-gray-500 dark:text-gray-400'}`} />
                                <div className="flex-1 min-w-0">
                                  <div className={`font-medium text-sm ${isSelected ? 'text-purple-700 dark:text-purple-300' : 'text-gray-900 dark:text-gray-100'}`}>
                                    {mode.label}
                                  </div>
                                  <div className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                                    {mode.description}
                                  </div>
                                </div>
                                {isSelected && <Check className="w-4 h-4 text-purple-600 dark:text-purple-400 mt-0.5" />}
                              </div>
                            </button>
                          )
                        })}
                      </div>
                    </div>
                  )}
                </div>

                {/* Dropdown 3: Start Point - Where to start */}
                <div className="relative" ref={startPointDropdownRef}>
                  <button
                    onClick={() => !isRunning && setIsStartPointDropdownOpen(!isStartPointDropdownOpen)}
                    disabled={isRunning}
                    className={`
                      flex items-center gap-1.5 px-2 py-1.5 rounded-md transition-all text-xs font-medium
                      ${isRunning
                        ? 'bg-muted text-muted-foreground cursor-not-allowed' 
                        : 'bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600'
                      }
                    `}
                    title="Select where to start execution"
                  >
                    {(() => {
                      const Icon = currentStartPointInfo.icon
                      return <Icon className="w-3.5 h-3.5" />
                    })()}
                    <span className="max-w-[120px] truncate" title={currentStartPointInfo.label}>{currentStartPointInfo.label}</span>
                    <ChevronDown className={`w-3 h-3 transition-transform ${isStartPointDropdownOpen ? 'rotate-180' : ''}`} />
                  </button>
                  
                  {/* Start Point Dropdown */}
                  {isStartPointDropdownOpen && !isRunning && (
                    <div className="absolute top-full left-0 mt-1 w-64 bg-white dark:bg-gray-800 rounded-lg shadow-xl border border-gray-200 dark:border-gray-700 z-50 max-h-[300px] overflow-y-auto">
                      <div className="p-1">
                        <div className="text-xs font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider px-3 py-2">
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
                                  <div className="text-xs font-semibold text-gray-400 dark:text-gray-500 uppercase tracking-wider">
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
                                  ? 'bg-purple-100 dark:bg-purple-900/30' 
                                  : 'hover:bg-gray-100 dark:hover:bg-gray-700'
                                }
                                  ${option.id === 'resume_branch' ? 'border-l-4 border-blue-400 dark:border-blue-500 ml-0' : ''}
                              `}
                                type="button"
                            >
                              <div className="flex items-start gap-3">
                                  <Icon className={`w-4 h-4 mt-0.5 flex-shrink-0 ${isSelected ? 'text-purple-600 dark:text-purple-400' : option.id === 'resume_branch' ? 'text-blue-500 dark:text-blue-400' : 'text-gray-500 dark:text-gray-400'}`} />
                                <div className="flex-1 min-w-0">
                                    <div className={`font-medium text-sm ${isSelected ? 'text-purple-700 dark:text-purple-300' : option.id === 'resume_branch' ? 'text-blue-700 dark:text-blue-300' : 'text-gray-900 dark:text-gray-100'}`}>
                                    {option.label}
                                  </div>
                                  <div className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                                    {option.description}
                                  </div>
                                </div>
                                  {isSelected && <Check className="w-4 h-4 text-purple-600 dark:text-purple-400 mt-0.5 flex-shrink-0" />}
                              </div>
                            </button>
                            </div>
                          )
                        })}
                      </div>
                    </div>
                  )}
                </div>
              </>
            )}

            {/* Regular Phases Dropdown Selector */}
            <div className="relative" ref={dropdownRef}>
              <button
                onClick={() => {
                  if (!isRunning && !isLoadingPhases && otherPhases.length > 0) {
                    setIsDropdownOpen(!isDropdownOpen)
                  }
                }}
                disabled={dropdownDisabled}
                className={`
                  flex items-center gap-1.5 px-2.5 py-1.5 rounded-md transition-all text-xs font-medium min-w-[160px]
                  ${dropdownDisabled
                    ? 'bg-muted text-muted-foreground cursor-not-allowed' 
                    : 'bg-gray-100 dark:bg-gray-700 text-gray-900 dark:text-gray-100 hover:bg-gray-200 dark:hover:bg-gray-600'
                  }
                `}
              >
                {isLoadingPhases ? (
                  <>
                    <Loader2 className="w-3.5 h-3.5 animate-spin" />
                    <span>Loading phases...</span>
                  </>
                ) : isRunning && !isExecutionPhase ? (
                  <>
                    <Loader2 className="w-3.5 h-3.5 animate-spin" />
                    <span className="flex-1 text-left truncate">
                      {currentPhaseDetails?.title || 'Running...'}
                    </span>
                  </>
                ) : (
                  <>
                    <Play className="w-3.5 h-3.5 flex-shrink-0" />
                    <span className="flex-1 text-left truncate">
                      {currentPhaseDetails && !isExecutionPhase ? currentPhaseDetails.title : 'Select Phase'}
                    </span>
                    <ChevronDown className={`w-3.5 h-3.5 flex-shrink-0 transition-transform ${isDropdownOpen ? 'rotate-180' : ''}`} />
                  </>
                )}
              </button>

              {/* Dropdown Menu - Only show non-execution phases */}
              {isDropdownOpen && !isRunning && !isLoadingPhases && otherPhases.length > 0 && (
                <div className="absolute top-full left-0 mt-1 w-80 bg-white dark:bg-gray-800 rounded-lg shadow-xl border border-gray-200 dark:border-gray-700 z-50 max-h-[400px] overflow-y-auto">
                  <div className="p-2">
                    <div className="text-xs font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider px-3 py-2">
                      Workflow Phases
                    </div>
                    {otherPhases.length === 0 ? (
                      <div className="px-3 py-4 text-sm text-gray-500 dark:text-gray-400 text-center">
                        {isLoadingPhases ? 'Loading phases...' : 'No phases available'}
                      </div>
                    ) : (
                      otherPhases.map((phase: WorkflowPhase) => {
                      const isActive = currentPhase === phase.id
                      return (
                        <button
                          key={phase.id}
                          onClick={() => handleSelectPhase(phase.id)}
                          className={`
                            w-full text-left px-3 py-2.5 rounded-lg transition-colors
                            ${isActive 
                              ? 'bg-gray-200 dark:bg-gray-700 text-gray-900 dark:text-gray-100 font-semibold' 
                              : 'hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-900 dark:text-gray-100'
                            }
                          `}
                        >
                          <div className="flex items-start gap-3">
                            <div className={`
                              w-5 h-5 rounded-full flex items-center justify-center flex-shrink-0 mt-0.5
                              ${isActive 
                                ? 'bg-gray-900 dark:bg-gray-100 text-gray-100 dark:text-gray-900' 
                                : 'bg-gray-200 dark:bg-gray-600 text-gray-500 dark:text-gray-400'
                              }
                            `}>
                              {isActive ? (
                                <Check className="w-3 h-3" />
                              ) : (
                                <span className="text-xs font-medium">
                                  {otherPhases.indexOf(phase) + 1}
                                </span>
                              )}
                            </div>
                            <div className="flex-1 min-w-0">
                              <div className="font-medium text-sm">
                                {phase.title}
                              </div>
                              {phase.description && (
                                <div className="text-xs text-gray-500 dark:text-gray-400 mt-0.5 line-clamp-2">
                                  {phase.description}
                                </div>
                              )}
                            </div>
                          </div>
                        </button>
                      )
                    }))}
                  </div>
                </div>
              )}
            </div>

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
        {status === 'completed' && (
          <div className="flex items-center gap-1.5 px-2 py-1 bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300 rounded-md text-xs">
            <div className="w-1.5 h-1.5 bg-green-500 rounded-full" />
            Completed
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
        {/* Toggle dependency edges - icon only */}
        {/* LLM Override Button and Banner */}
        {tempOverrideLLM || tempOverrideLLM2 ? (
          // Active override indicator with toggle and clear button
          <TooltipProvider>
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
              </div>
              <button
                onClick={() => setTempOverrideLLMEnabled(!tempOverrideLLMEnabled)}
                className={`px-1.5 py-0.5 rounded text-xs font-medium transition-colors ${
                  tempOverrideLLMEnabled 
                    ? 'bg-primary/20 text-primary hover:bg-primary/30' 
                    : 'bg-muted text-muted-foreground hover:bg-muted/80'
                }`}
                title={tempOverrideLLMEnabled ? 'Disable temp LLM overrides' : 'Enable temp LLM overrides'}
              >
                {tempOverrideLLMEnabled ? 'ON' : 'OFF'}
              </button>
              <button
                onClick={() => {
                  clearTempOverrideLLM()
                  clearTempOverrideLLM2()
                }}
                className="p-0.5 rounded hover:bg-muted text-muted-foreground hover:text-foreground"
                title="Clear LLM overrides (removes configs)"
              >
                <X className="w-3 h-3" />
              </button>
            <button
              onClick={() => setShowLLMOverrideModal(true)}
              className="p-0.5 rounded hover:bg-muted text-muted-foreground hover:text-foreground"
              title="Change LLM overrides"
            >
              <Settings className="w-3 h-3" />
            </button>
          </div>
          </TooltipProvider>
        ) : (
          // No override - show button to set one
          <button
            onClick={() => setShowLLMOverrideModal(true)}
            className="p-1.5 rounded-md bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400 hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors"
            title="Set temporary LLM override for execution agents"
          >
            <Brain className="w-3.5 h-3.5" />
          </button>
        )}
        
        {/* Show Learnings - opens workspace and navigates to learnings folder */}
        {workspacePath && (
          <button
            onClick={async () => {
              // Expand workspace if minimized
              setWorkspaceMinimized(false)
              // Small delay to ensure workspace is expanded before navigating
              setTimeout(async () => {
                const learningsPath = `${workspacePath}/learnings`
                console.log('[WorkflowToolbar] Opening learnings folder:', learningsPath)
                // Refresh files to ensure workspace has latest data
                await fetchFiles()
                // Get updated files after refresh (need to access store state directly)
                const storeState = useWorkspaceStore.getState()
                const updatedFiles = storeState.files
                // Find the folder in the tree
                const folder = findFolderInTree(updatedFiles, learningsPath)
                if (folder) {
                  console.log('[WorkflowToolbar] Found learnings folder:', folder.filepath, 'original:', folder.originalFilepath)
                  // Use the exact path from the file tree (prefer originalFilepath for highlighting)
                  const pathToHighlight = folder.originalFilepath || folder.filepath
                  highlightFile(pathToHighlight)
                } else {
                  console.log('[WorkflowToolbar] Folder not found, trying direct path:', learningsPath)
                  // Fallback to direct path
                  highlightFile(learningsPath)
                }
                setShowFileContent(false)
              }, 200)
            }}
            className="p-1.5 rounded-md bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400 hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors"
            title="Show learnings folder"
          >
            <BookOpen className="w-3.5 h-3.5" />
          </button>
        )}

        {/* Show Execution - opens workspace and navigates to execution folder */}
        {workspacePath && selectedRunFolder && selectedRunFolder !== 'new' && (
          <button
            onClick={async () => {
              // Expand workspace if minimized
              setWorkspaceMinimized(false)
              // Small delay to ensure workspace is expanded before navigating
              setTimeout(async () => {
                const executionPath = `${workspacePath}/runs/${selectedRunFolder}/execution`
                console.log('[WorkflowToolbar] Opening execution folder:', executionPath)
                // Refresh files to ensure workspace has latest data
                await fetchFiles()
                // Get updated files after refresh (need to access store state directly)
                const storeState = useWorkspaceStore.getState()
                const updatedFiles = storeState.files
                // Find the folder in the tree
                const folder = findFolderInTree(updatedFiles, executionPath)
                if (folder) {
                  console.log('[WorkflowToolbar] Found execution folder:', folder.filepath, 'original:', folder.originalFilepath)
                  // Use the exact path from the file tree (prefer originalFilepath for highlighting)
                  const pathToHighlight = folder.originalFilepath || folder.filepath
                  highlightFile(pathToHighlight)
                } else {
                  console.log('[WorkflowToolbar] Folder not found, trying direct path:', executionPath)
                  // Fallback to direct path
                  highlightFile(executionPath)
                }
                setShowFileContent(false)
              }, 200)
            }}
            className="p-1.5 rounded-md bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400 hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors"
            title="Show execution folder"
          >
            <FolderTree className="w-3.5 h-3.5" />
          </button>
        )}
        
        {/* Toggle ChatArea Button */}
        {onToggleChatArea && (
          <button
            onClick={onToggleChatArea}
            className={`p-1.5 rounded-md transition-colors ${
              showChatArea
                ? 'bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300 border border-blue-300 dark:border-blue-700'
                : 'bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400 hover:bg-gray-200 dark:hover:bg-gray-600'
            }`}
            title={showChatArea ? 'Hide chat panel' : 'Show chat panel'}
          >
            <MessageSquare className="w-3.5 h-3.5" />
          </button>
        )}
        
        <div className="w-px h-5 bg-gray-200 dark:bg-gray-700 mx-0.5" />
        
        <button
          onClick={onZoomOut}
          className="p-1.5 rounded-md hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-600 dark:text-gray-400 transition-colors"
          title="Zoom out"
        >
          <ZoomOut className="w-3.5 h-3.5" />
        </button>
        <button
          onClick={onZoomIn}
          className="p-1.5 rounded-md hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-600 dark:text-gray-400 transition-colors"
          title="Zoom in"
        >
          <ZoomIn className="w-3.5 h-3.5" />
        </button>
        <button
          onClick={onFitView}
          className="p-1.5 rounded-md hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-600 dark:text-gray-400 transition-colors"
          title="Fit to view"
        >
          <Maximize2 className="w-3.5 h-3.5" />
        </button>
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
    </>
  )
}

export default WorkflowToolbar
