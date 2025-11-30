import React, { useEffect, useState, useRef, useMemo, useCallback } from 'react'
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
  FolderTree
} from 'lucide-react'
import { useWorkspaceStore } from '../../../stores/useWorkspaceStore'
import { useAppStore } from '../../../stores'
import type { PlannerFile } from '../../../services/api-types'
import type { WorkflowExecutionStatus } from '../hooks/useWorkflowExecution'
import { getWorkflowPhases } from '../../../constants/workflow'
import { agentApi } from '../../../services/api'
import type { WorkflowPhase, ExecutionOptions, StepProgress } from '../../../services/api-types'
import { ExecutionStrategy } from '../../../services/api-types'

// Execution phase ID - special phase that should be displayed separately
const EXECUTION_PHASE_ID = 'execution'

// Execution Mode options - how to run (human feedback, learning, etc.)
type ExecutionModeType = 'human_approval' | 'fast_execution' | 'with_learning'
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
  onExecutionOptionsChange?: (options: { selectedRunFolder: string; selectedExecutionMode: 'human_approval' | 'fast_execution' | 'with_learning' }) => void  // Callback when execution options change
  className?: string
}

// LocalStorage keys for persisting workflow settings
const STORAGE_KEY_PREFIX = 'workflow_settings_'
const getStorageKey = (presetId: string, setting: 'iteration' | 'execution_mode' | 'start_point') => 
  `${STORAGE_KEY_PREFIX}${presetId}_${setting}`

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
  const [phases, setPhases] = useState<WorkflowPhase[]>([])
  const [isDropdownOpen, setIsDropdownOpen] = useState(false)
  const [loadingPhases, setLoadingPhases] = useState(true)
  const dropdownRef = useRef<HTMLDivElement>(null)
  const isRunning = status === 'running'

  // Execution options state
  const [runFolders, setRunFolders] = useState<Array<{ name: string; progress?: StepProgress }>>([])
  const [selectedRunFolder, setSelectedRunFolder] = useState<string>('new')
  const [stepProgress, setStepProgress] = useState<StepProgress | null>(null)
  const [loadingRunFolders, setLoadingRunFolders] = useState(false)
  
  // Dropdown visibility states
  const [isIterationDropdownOpen, setIsIterationDropdownOpen] = useState(false)
  const [isExecutionModeDropdownOpen, setIsExecutionModeDropdownOpen] = useState(false)
  const [isStartPointDropdownOpen, setIsStartPointDropdownOpen] = useState(false)
  
  // Selected values
  const [selectedExecutionMode, setSelectedExecutionMode] = useState<ExecutionModeType>('human_approval')
  const [selectedStartPoint, setSelectedStartPoint] = useState<number>(0) // 0 = from beginning, >0 = resume from step N
  
  // Refs for dropdown click-outside detection
  const iterationDropdownRef = useRef<HTMLDivElement>(null)
  const executionModeDropdownRef = useRef<HTMLDivElement>(null)
  const startPointDropdownRef = useRef<HTMLDivElement>(null)
  
  // Track if we've loaded from localStorage to avoid overwriting with defaults
  const [hasLoadedFromStorage, setHasLoadedFromStorage] = useState(false)

  // Load saved settings from localStorage when workflow changes
  useEffect(() => {
    if (!presetQueryId) {
      setHasLoadedFromStorage(false)
      return
    }
    
    try {
      // Load saved iteration folder
      const savedIteration = localStorage.getItem(getStorageKey(presetQueryId, 'iteration'))
      if (savedIteration) {
        setSelectedRunFolder(savedIteration)
      }
      
      // Load saved execution mode
      const savedMode = localStorage.getItem(getStorageKey(presetQueryId, 'execution_mode'))
      if (savedMode && ['human_approval', 'fast_execution', 'with_learning'].includes(savedMode)) {
        setSelectedExecutionMode(savedMode as ExecutionModeType)
      }
      
      // Load saved start point
      const savedStartPoint = localStorage.getItem(getStorageKey(presetQueryId, 'start_point'))
      if (savedStartPoint) {
        const parsed = parseInt(savedStartPoint, 10)
        if (!isNaN(parsed)) {
          setSelectedStartPoint(parsed)
        }
      }
      
      setHasLoadedFromStorage(true)
    } catch (error) {
      console.error('[WorkflowToolbar] Failed to load settings from localStorage:', error)
      setHasLoadedFromStorage(true)
    }
  }, [presetQueryId])

  // Save iteration folder to localStorage when it changes
  useEffect(() => {
    if (!presetQueryId || !hasLoadedFromStorage) return
    
    try {
      localStorage.setItem(getStorageKey(presetQueryId, 'iteration'), selectedRunFolder)
    } catch (error) {
      console.error('[WorkflowToolbar] Failed to save iteration to localStorage:', error)
    }
  }, [presetQueryId, selectedRunFolder, hasLoadedFromStorage])

  // Save execution mode to localStorage when it changes
  useEffect(() => {
    if (!presetQueryId || !hasLoadedFromStorage) return
    
    try {
      localStorage.setItem(getStorageKey(presetQueryId, 'execution_mode'), selectedExecutionMode)
    } catch (error) {
      console.error('[WorkflowToolbar] Failed to save execution mode to localStorage:', error)
    }
  }, [presetQueryId, selectedExecutionMode, hasLoadedFromStorage])

  // Save start point to localStorage when it changes
  useEffect(() => {
    if (!presetQueryId || !hasLoadedFromStorage) return
    
    try {
      localStorage.setItem(getStorageKey(presetQueryId, 'start_point'), String(selectedStartPoint))
    } catch (error) {
      console.error('[WorkflowToolbar] Failed to save start point to localStorage:', error)
    }
  }, [presetQueryId, selectedStartPoint, hasLoadedFromStorage])

  // Notify parent when execution options change (for use in step run actions)
  useEffect(() => {
    if (onExecutionOptionsChange && hasLoadedFromStorage) {
      onExecutionOptionsChange({
        selectedRunFolder,
        selectedExecutionMode
      })
    }
  }, [selectedRunFolder, selectedExecutionMode, onExecutionOptionsChange, hasLoadedFromStorage])

  // Load phases from backend
  useEffect(() => {
    const loadPhases = async () => {
      try {
        setLoadingPhases(true)
        const loadedPhases = await getWorkflowPhases()
        setPhases(loadedPhases)
      } catch (error) {
        console.error('[WorkflowToolbar] Failed to load phases:', error)
      } finally {
        setLoadingPhases(false)
      }
    }
    loadPhases()
  }, [])

  // Load run folders when workspace path changes
  useEffect(() => {
    const loadRunFolders = async () => {
      if (!workspacePath) {
        setRunFolders([])
        return
      }
      
      try {
        setLoadingRunFolders(true)
        const response = await agentApi.getRunFolders(workspacePath)
        
        // Handle null or missing folders array
        if (!response) {
          setRunFolders([])
          return
        }
        
        // Handle null folders (backend should return [] but handle null gracefully)
        let folders = response.folders
        if (folders === null || folders === undefined) {
          folders = []
        }
        
        if (!Array.isArray(folders)) {
          console.warn('[WorkflowToolbar] response.folders is not an array. Type:', typeof folders)
          setRunFolders([])
          return
        }
        
        // Sort folders by iteration number (descending - newest first)
        const sorted = [...folders].sort((a, b) => {
          const numA = parseInt(a.name.replace('iteration-', '')) || 0
          const numB = parseInt(b.name.replace('iteration-', '')) || 0
          return numB - numA
        })
        
        setRunFolders(sorted)
        
        // Check if current selection is valid (either 'new' or exists in folders)
        // Only change selection if:
        // 1. Current selection is not 'new' AND
        // 2. Current selection doesn't exist in the loaded folders
        setSelectedRunFolder(prev => {
          let newSelection = prev
          if (prev !== 'new' && !sorted.some(f => f.name === prev)) {
            // Saved folder no longer exists, default to newest or 'new'
            if (sorted.length > 0) {
              newSelection = sorted[0].name
            } else {
              newSelection = 'new'
            }
          }
          
          // If a folder is selected and it has progress, set it immediately
          if (newSelection !== 'new') {
            const selectedFolder = sorted.find(f => f.name === newSelection)
            if (selectedFolder?.progress) {
              setStepProgress(selectedFolder.progress)
            }
          }
          
          return newSelection
        })
      } catch (error) {
        console.error('[WorkflowToolbar] Failed to load run folders:', error)
        setRunFolders([])
        // Reset to 'new' if there was an error and we had a folder selected
        setSelectedRunFolder(prev => {
          if (prev !== 'new') {
            return 'new'
          }
          return prev
        })
      } finally {
        setLoadingRunFolders(false)
      }
    }
    loadRunFolders()
  }, [workspacePath]) // Removed selectedRunFolder from dependencies to avoid infinite loops

  // Load step progress when selected run folder changes
  // Always fetch via API to ensure we have the latest progress for the selected iteration
  useEffect(() => {
    const loadProgress = async () => {
      if (!workspacePath || selectedRunFolder === 'new') {
        setStepProgress(null)
        return
      }
      
      // Always fetch progress via API when an iteration is selected
      // This ensures we have the latest data and works for all iterations (not just latest ones)
      try {
        const response = await agentApi.getProgress(workspacePath, selectedRunFolder)
        if (response.exists && response.progress) {
          setStepProgress(response.progress)
          
          // Update the folder info in state so we can show progress in the dropdown
          setRunFolders(prev => prev.map(f => 
            f.name === selectedRunFolder 
              ? { ...f, progress: response.progress || undefined }
              : f
          ))
        } else {
          setStepProgress(null)
        }
      } catch (error) {
        console.error('[WorkflowToolbar] Failed to load progress:', error)
        setStepProgress(null)
      }
    }
    
    loadProgress()
  }, [workspacePath, selectedRunFolder])

  // Notify parent when step progress changes
  useEffect(() => {
    if (onProgressChange) {
      const completedIndices = stepProgress?.completed_step_indices || []
      onProgressChange(completedIndices)
    }
  }, [stepProgress, onProgressChange])

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
  const { executionPhase, otherPhases } = useMemo(() => {
    const execPhase = phases.find(p => p.id === EXECUTION_PHASE_ID)
    const others = phases.filter(p => p.id !== EXECUTION_PHASE_ID)
    return {
      executionPhase: execPhase,
      otherPhases: others
    }
  }, [phases])

  // Calculate progress info
  const hasExistingProgress = stepProgress !== null && stepProgress.completed_step_indices.length > 0
  const completedStepCount = stepProgress?.completed_step_indices.length || 0
  const completedStepIndices = stepProgress?.completed_step_indices || []

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
  const currentPhaseDetails = phases.find(p => p.id === currentPhase)
  const isExecutionPhase = currentPhase === EXECUTION_PHASE_ID

  // Handle phase selection
  const handleSelectPhase = (phaseId: string) => {
    setIsDropdownOpen(false)
    if (!isRunning) {
      onStartPhase(phaseId)
    }
  }

  // Handle selecting execution mode from dropdown
  const handleSelectExecutionMode = useCallback((modeId: ExecutionModeType) => {
    setSelectedExecutionMode(modeId)
    setIsExecutionModeDropdownOpen(false)
  }, [])
  
  // Handle selecting start point from dropdown
  const handleSelectStartPoint = useCallback((stepNumber: number) => {
    setSelectedStartPoint(stepNumber)
    setIsStartPointDropdownOpen(false)
  }, [])

  // Handle execution button click
  const handleExecute = useCallback(() => {
    if (!isRunning && executionPhase) {
      // Convert UI selections to backend ExecutionStrategy
      let executionStrategy: string
      const isResuming = selectedStartPoint > 0
      
      if (selectedExecutionMode === 'fast_execution') {
        // Fast execution - no human feedback
        executionStrategy = isResuming 
          ? ExecutionStrategy.FAST_RESUME_FROM_STEP 
          : ExecutionStrategy.FAST_EXECUTE_ALL
      } else if (selectedExecutionMode === 'with_learning') {
        // With learning - human feedback but captures learnings
        executionStrategy = isResuming 
          ? ExecutionStrategy.RESUME_FROM_STEP_NO_HUMAN  // TODO: Need a "with_learning" strategy
          : ExecutionStrategy.START_FROM_BEGINNING_NO_HUMAN
      } else {
        // Human approval (default) - pause for feedback
        executionStrategy = isResuming 
          ? ExecutionStrategy.RESUME_FROM_STEP 
          : ExecutionStrategy.START_FROM_BEGINNING
      }
      
      const options: ExecutionOptions = {
        run_mode: selectedRunFolder === 'new' ? 'create_new_runs_always' : 'use_same_run',
        selected_run_folder: selectedRunFolder === 'new' ? undefined : selectedRunFolder,
        execution_strategy: executionStrategy,
      }
      
      // For resume, set which step to resume from
      if (isResuming) {
        options.resume_from_step = selectedStartPoint
      }
      
      onStartPhase(executionPhase.id, options)
    }
  }, [isRunning, executionPhase, selectedRunFolder, selectedStartPoint, selectedExecutionMode, onStartPhase])

  return (
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
                    disabled={isRunning || loadingRunFolders}
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
                      {loadingRunFolders ? 'Loading...' : selectedRunFolder === 'new' ? 'New Run' : selectedRunFolder}
                    </span>
                    <ChevronDown className={`w-3 h-3 transition-transform ${isIterationDropdownOpen ? 'rotate-180' : ''}`} />
                  </button>
                  
                  {/* Iteration Dropdown */}
                  {isIterationDropdownOpen && !isRunning && (
                    <div className="absolute top-full left-0 mt-1 w-56 bg-white dark:bg-gray-800 rounded-lg shadow-xl border border-gray-200 dark:border-gray-700 z-50 max-h-[300px] overflow-y-auto">
                      <div className="p-1">
                        {/* New Run option */}
                        <button
                          onClick={() => {
                            setSelectedRunFolder('new')
                            setIsIterationDropdownOpen(false)
                          }}
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
                            {runFolders.map((folder) => {
                              const progress = folder.progress
                              const completedCount = progress?.completed_step_indices.length || 0
                              const totalSteps = progress?.total_steps || 0
                              const hasProgress = progress && completedCount > 0
                              
                              return (
                                <button
                                  key={folder.name}
                                  onClick={() => {
                                    setSelectedRunFolder(folder.name)
                                    // Immediately set progress from cached folder data for instant UI update
                                    if (folder.progress) {
                                      setStepProgress(folder.progress)
                                    }
                                    setIsIterationDropdownOpen(false)
                                  }}
                                  className={`
                                    w-full text-left px-3 py-2 rounded-md text-sm flex items-center gap-2
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
                                      {completedCount}/{totalSteps}
                                    </span>
                                  )}
                                  {selectedRunFolder === folder.name && <Check className="w-4 h-4 ml-auto" />}
                                </button>
                              )
                            })}
                          </>
                        ) : !loadingRunFolders && workspacePath ? (
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
                        {startPointOptions.map((option, idx) => {
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

                {/* Execute Button - Simple button */}
                <button
                  onClick={handleExecute}
                  disabled={isRunning && !isExecutionPhase}
                  className={`
                    flex items-center gap-1.5 px-2.5 py-1.5 rounded-md transition-all text-xs font-semibold
                    ${isExecutionPhase && isRunning
                      ? 'bg-purple-600 dark:bg-purple-700 text-white cursor-not-allowed opacity-75'
                      : isRunning
                      ? 'bg-muted text-muted-foreground cursor-not-allowed'
                      : 'bg-purple-500 dark:bg-purple-600 text-white shadow-md hover:bg-purple-600 dark:hover:bg-purple-700 hover:shadow-lg'
                    }
                  `}
                  title={`Execute: ${currentModeInfo.label}`}
                >
                  {isExecutionPhase && isRunning ? (
                    <>
                      <Loader2 className="w-3.5 h-3.5 animate-spin" />
                      <span>Executing...</span>
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
                onClick={() => !isRunning && !isExecutionPhase && setIsDropdownOpen(!isDropdownOpen)}
                disabled={isRunning || loadingPhases || isExecutionPhase}
                className={`
                  flex items-center gap-1.5 px-2.5 py-1.5 rounded-md transition-all text-xs font-medium min-w-[160px]
                  ${isRunning || isExecutionPhase
                    ? 'bg-muted text-muted-foreground cursor-not-allowed' 
                    : 'bg-gray-100 dark:bg-gray-700 text-gray-900 dark:text-gray-100 hover:bg-gray-200 dark:hover:bg-gray-600'
                  }
                `}
              >
                {loadingPhases ? (
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
              {isDropdownOpen && !isRunning && !isExecutionPhase && (
                <div className="absolute top-full left-0 mt-1 w-80 bg-white dark:bg-gray-800 rounded-lg shadow-xl border border-gray-200 dark:border-gray-700 z-50 max-h-[400px] overflow-y-auto">
                  <div className="p-2">
                    <div className="text-xs font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider px-3 py-2">
                      Workflow Phases
                    </div>
                    {otherPhases.map((phase) => {
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
                    })}
                  </div>
              </div>
            )}
            </div>

            {/* Stop button when running */}
            {isRunning && (
              <button
                onClick={onStop}
                className="flex items-center gap-1.5 px-3 py-2 bg-red-100 dark:bg-red-900/30 hover:bg-red-200 dark:hover:bg-red-900/50 text-red-700 dark:text-red-300 rounded-lg transition-colors text-sm"
              >
                <Square className="w-4 h-4" />
                <span>Stop</span>
              </button>
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
  )
}

export default WorkflowToolbar
