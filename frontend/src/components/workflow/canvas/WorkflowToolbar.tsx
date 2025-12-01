import React, { useEffect, useRef, useMemo, useCallback } from 'react'
import { 
  Play, 
  Square, 
  Plus,
  Maximize2,
  ZoomIn,
  ZoomOut,
  Loader2,
  GitBranch,
  ChevronDown,
  Check,
  Rocket,
  FolderOpen,
  Zap,
  SkipForward,
  RefreshCw,
  BookOpen,
  FolderTree,
  Trash2
} from 'lucide-react'
import { useWorkspaceStore } from '../../../stores/useWorkspaceStore'
import { useAppStore } from '../../../stores'
import { useWorkflowStore, type ExecutionModeType, type RunFolder } from '../../../stores/useWorkflowStore'
import type { PlannerFile, WorkflowPhase } from '../../../services/api-types'
import type { WorkflowExecutionStatus } from '../hooks/useWorkflowExecution'
import type { ExecutionOptions } from '../../../services/api-types'
import { agentApi } from '../../../services/api'
import ConfirmationDialog from '../../ui/ConfirmationDialog'

// Execution phase ID - special phase that should be displayed separately
const EXECUTION_PHASE_ID = 'execution'

// Execution Mode options - how to run (human feedback, learning, etc.)
const EXECUTION_MODE_OPTIONS: { id: ExecutionModeType; label: string; icon: typeof Play; description: string }[] = [
  { id: 'human_approval', label: 'With Human Approval', icon: Play, description: 'Pause for feedback at each step' },
  { id: 'fast_execution', label: 'Fast Execution', icon: Zap, description: 'Execute all without pausing' },
  { id: 'with_learning', label: 'With Learning', icon: SkipForward, description: 'Human approval + capture learnings' },
]

// Start Point options - where to start execution
type StartPointType = 'from_beginning' | 'resume' | 'single_step'
interface StartPointOption {
  id: StartPointType
  stepNumber?: number  // For resume/single_step, which step to target (1-based)
  label: string
  icon: typeof Play
  description: string
}

interface WorkflowToolbarProps {
  status: WorkflowExecutionStatus
  hasPlan: boolean
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
  showDependencyEdges?: boolean
  onToggleDependencyEdges?: () => void
  onProgressChange?: (completedStepIndices: number[]) => void  // Callback when step progress changes
  onExecutionOptionsChange?: (options: { selectedRunFolder: string; selectedExecutionMode: ExecutionModeType }) => void  // Callback when execution options change
  className?: string
}

export const WorkflowToolbar: React.FC<WorkflowToolbarProps> = ({
  status,
  hasPlan,
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
  showDependencyEdges = false,
  onToggleDependencyEdges,
  onProgressChange,
  onExecutionOptionsChange,
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
  const setExecutionMode = useWorkflowStore(state => state.setExecutionMode)
  const setStartPoint = useWorkflowStore(state => state.setStartPoint)
  const buildExecutionOptions = useWorkflowStore(state => state.buildExecutionOptions)
  const loadSavedSettings = useWorkflowStore(state => state.loadSavedSettings)
  const saveSettings = useWorkflowStore(state => state.saveSettings)
  
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

  // Notify parent when execution options change
  useEffect(() => {
    if (onExecutionOptionsChange) {
      onExecutionOptionsChange({
        selectedRunFolder,
        selectedExecutionMode
      })
    }
  }, [selectedRunFolder, selectedExecutionMode, onExecutionOptionsChange])

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
  
  // Generate start point options based on completed steps
  const startPointOptions = useMemo((): StartPointOption[] => {
    const options: StartPointOption[] = [
      { id: 'from_beginning', label: 'Start from Beginning', icon: Play, description: 'Execute all steps from start' }
    ]
    
    // Add resume options for each step that can be resumed from
    // User can resume from any step after the first completed step
    if (completedStepIndices.length > 0 && totalSteps > 0) {
      // Find the next step after last completed
      const nextStep = Math.max(...completedStepIndices) + 2 // +2 because indices are 0-based, and we want the next step (1-based)
      if (nextStep <= totalSteps) {
        options.push({
          id: 'resume',
          stepNumber: nextStep,
          label: `Resume from Step ${nextStep}`,
          icon: RefreshCw,
          description: `Continue from step ${nextStep} (${completedStepCount} completed)`
        })
      }
      
      // Add option to resume from any other runnable step
      for (let i = 1; i <= totalSteps; i++) {
        // Skip steps that are already in the list or are the "next" step
        if (i === nextStep) continue
        // Can only resume from step if all previous steps are done
        const canResumeFrom = i === 1 || completedStepIndices.filter(idx => idx < i - 1).length === i - 1
        if (canResumeFrom && i > 1) {
          options.push({
            id: 'resume',
            stepNumber: i,
            label: `Resume from Step ${i}`,
            icon: SkipForward,
            description: `Jump to step ${i}`
          })
        }
      }
    }
    
    return options
  }, [completedStepIndices, totalSteps, completedStepCount])

  // Get current start point info
  const currentStartPointInfo = useMemo(() => {
    if (selectedStartPoint === 0) {
      return startPointOptions[0] // "Start from Beginning"
    }
    return startPointOptions.find(o => o.stepNumber === selectedStartPoint) || startPointOptions[0]
  }, [selectedStartPoint, startPointOptions])

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
  const handleSelectStartPoint = useCallback((stepNumber: number) => {
    setStartPoint(stepNumber)
    setIsStartPointDropdownOpen(false)
  }, [setStartPoint])

  // Handle selecting run folder
  const handleSelectRunFolder = useCallback((folder: string) => {
    setSelectedRunFolder(folder)
    setIsIterationDropdownOpen(false)
  }, [setSelectedRunFolder])

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
      onStartPhase(executionPhase.id, options)
    }
  }, [isRunning, executionPhase, buildExecutionOptions, onStartPhase])

  return (
    <>
    <div className={`
      flex items-center justify-between gap-2 px-3 py-1.5 
      bg-white dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700
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
            {/* Execution Controls - Iteration selector + Split button */}
            {executionPhase && (
              <>
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
                          className={`
                            w-full text-left px-3 py-2 rounded-md text-sm flex items-center gap-2
                            ${selectedRunFolder === 'new' 
                              ? 'bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300' 
                              : 'hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-700 dark:text-gray-300'
                            }
                          `}
                        >
                          <Plus className="w-4 h-4" />
                          <span className="font-medium">New Run</span>
                          {selectedRunFolder === 'new' && <Check className="w-4 h-4 ml-auto" />}
                        </button>
                        
                        {runFolders.length > 0 ? (
                          <>
                            <div className="border-t border-gray-200 dark:border-gray-700 my-1" />
                            <div className="text-xs font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider px-3 py-1">
                              Existing Runs ({runFolders.length})
                            </div>
                            {runFolders.map((folder: RunFolder) => {
                              const progress = folder.progress
                              const folderCompletedCount = progress?.completed_step_indices.length || 0
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
                            })}
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
                    <span className="max-w-[110px] truncate">{currentStartPointInfo.label}</span>
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
                            ? selectedStartPoint === 0 
                            : selectedStartPoint === option.stepNumber
                          return (
                            <button
                              key={`${option.id}-${option.stepNumber || idx}`}
                              onClick={() => handleSelectStartPoint(option.stepNumber || 0)}
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
                                    {option.label}
                                  </div>
                                  <div className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                                    {option.description}
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
        {onToggleDependencyEdges && (
          <button
            onClick={onToggleDependencyEdges}
            className={`p-1.5 rounded-md transition-colors ${
              showDependencyEdges 
                ? 'bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300 border border-purple-300 dark:border-purple-700' 
                : 'bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400 hover:bg-gray-200 dark:hover:bg-gray-600'
            }`}
            title={showDependencyEdges ? 'Hide data dependencies' : 'Show data dependencies'}
          >
            <GitBranch className="w-3.5 h-3.5" />
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
    </>
  )
}

export default WorkflowToolbar
