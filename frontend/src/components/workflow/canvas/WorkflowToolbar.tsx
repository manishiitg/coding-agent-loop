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
  Database,
} from 'lucide-react'
import { useWorkspaceStore } from '../../../stores/useWorkspaceStore'
import { useWorkflowStore, type RunFolder } from '../../../stores/useWorkflowStore'
import { useWorkflowManifestStore } from '../../../stores/useWorkflowManifestStore'
import { useChatStore } from '../../../stores/useChatStore'
import type { StepProgress, VariablesManifest } from '../../../services/api-types'
import type { PlanningResponse } from '../../../utils/stepConfigMatching'
import type { WorkflowExecutionStatus } from '../hooks/useWorkflowExecution'
import type { ExecutionOptions } from '../../../services/api-types'
import { agentApi } from '../../../services/api'
import ConfirmationDialog from '../../ui/ConfirmationDialog'
import BulkStepConfigModal from '../BulkStepConfigModal'
import { useCommandDialogStore } from '../../../stores/useCommandDialogStore'
import LearningsPopup from '../LearningsPopup'
import KBPopup from '../KBPopup'
import ExecutionLogsPopup from '../ExecutionLogsPopup'
import EvaluationPopup from '../EvaluationPopup'
import CostsPopup from '../CostsPopup'
import WorkflowVersionsPopup from '../WorkflowVersionsPopup'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../../ui/tooltip'
import type { PlanStep } from '../../../utils/stepConfigMatching'
import { isConditionalStep } from '../../../utils/stepConfigMatching'
import {
  buildGroupFolderPath,
  extractGroupNameFromFolder,
  sanitizeDisplayNameForFolder,
  resolveGroupFolderPath
} from '../../../utils/workflowUtils'

// Execution phase ID - special phase that should be displayed separately
const EXECUTION_PHASE_ID = 'execution'
const EVAL_EXECUTION_PHASE_ID = 'evaluation-execution'
const REPORT_EXECUTION_PHASE_ID = 'report-execution'


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
  onRefresh?: () => Promise<void>  // Refresh plan and variables
  onSaveLayout?: () => Promise<void>  // Save workflow layout
  onDeleteLayout?: () => Promise<void>  // Delete workflow layout and reset to default
  hasUnsavedLayoutChanges?: boolean  // Whether there are unsaved layout changes
  isSavingLayout?: boolean  // Whether layout is currently being saved
  isDeletingLayout?: boolean  // Whether layout is currently being deleted
  selectedStepIds?: string[]  // IDs of currently selected steps (shows indicator when 2+ selected)
  className?: string
}

interface ToolbarToggleProps {
  label: string
  checked: boolean
  onClick: () => void
  disabled?: boolean
  tooltip: React.ReactNode
}

const ToolbarToggle: React.FC<ToolbarToggleProps> = ({
  label,
  checked,
  onClick,
  disabled = false,
  tooltip
}) => (
  <TooltipProvider delayDuration={150}>
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          onClick={onClick}
          disabled={disabled}
          role="switch"
          aria-checked={checked}
          className={`
            inline-flex h-8 items-center gap-2 rounded-md border px-2.5 text-xs transition-colors
            ${checked
              ? 'border-primary/30 bg-primary/10 text-primary'
              : 'border-border bg-muted/40 text-muted-foreground hover:bg-accent hover:text-accent-foreground'
            }
            ${disabled ? 'cursor-not-allowed opacity-70 hover:bg-primary/10 hover:text-primary' : ''}
          `}
        >
          <span className="font-medium whitespace-nowrap">{label}</span>
          <span
            aria-hidden="true"
            className={`
              relative inline-flex h-4 w-7 shrink-0 items-center rounded-full transition-colors
              ${checked ? 'bg-primary/80' : 'bg-muted-foreground/30'}
            `}
          >
            <span
              className={`
                inline-block h-3 w-3 rounded-full bg-white shadow-sm transition-transform
                ${checked ? 'translate-x-3.5' : 'translate-x-0.5'}
              `}
            />
          </span>
        </button>
      </TooltipTrigger>
      <TooltipContent side="bottom">
        {tooltip}
      </TooltipContent>
    </Tooltip>
  </TooltipProvider>
)

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
  onBulkUpdateSteps,
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
    selectedRunFolder,
    selectedStartPoint,
    selectedBranchStep,
    selectedGroupIds,
    currentRunningGroupId,
    loadRunFolders,
    setSelectedRunFolder,
    loadProgress,
    loadFolderProgressOnDemand,
    setStartPoint,
    setBranchStep,
    buildExecutionOptions,
    loadSavedSettings,
    toggleGroupSelection,
    setSelectedGroupIds,
    clearSelectedGroupIds,
    restoreSelectionFromLocalStorage,
    workflowWorkspaceView,
    workflowWorkspaceSelectionTouched,
    canvasViewMode,
    setCanvasViewMode,
    setWorkflowWorkspaceView,
    layoutDirection,
    setLayoutDirection
  } = useWorkflowStore(useShallow(state => ({
    selectedRunFolder: state.selectedRunFolder,
    selectedStartPoint: state.selectedStartPoint,
    selectedBranchStep: state.selectedBranchStep,
    selectedGroupIds: state.selectedGroupIds,
    currentRunningGroupId: state.currentRunningGroupId,
    loadRunFolders: state.loadRunFolders,
    setSelectedRunFolder: state.setSelectedRunFolder,
    loadProgress: state.loadProgress,
    loadFolderProgressOnDemand: state.loadFolderProgressOnDemand,
    setStartPoint: state.setStartPoint,
    setBranchStep: state.setBranchStep,
    buildExecutionOptions: state.buildExecutionOptions,
    loadSavedSettings: state.loadSavedSettings,
    toggleGroupSelection: state.toggleGroupSelection,
    setSelectedGroupIds: state.setSelectedGroupIds,
    clearSelectedGroupIds: state.clearSelectedGroupIds,
    restoreSelectionFromLocalStorage: state.restoreSelectionFromLocalStorage,
    workflowWorkspaceView: state.workflowWorkspaceView,
    workflowWorkspaceSelectionTouched: state.workflowWorkspaceSelectionTouched,
    canvasViewMode: state.canvasViewMode,
    setCanvasViewMode: state.setCanvasViewMode,
    setWorkflowWorkspaceView: state.setWorkflowWorkspaceView,
    layoutDirection: state.layoutDirection,
    setLayoutDirection: state.setLayoutDirection
  })))

  // Reset start point when switching away from plan mode
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
  
  // Bulk Step Config modal state
  const [showBulkStepConfigModal, setShowBulkStepConfigModal] = useState(false)
  
  // Learnings popup state
  const [showLearningsPopup, setShowLearningsPopup] = useState(false)
  const [showKBPopup, setShowKBPopup] = useState(false)

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
      setShowKBPopup(false)
      setShowExecutionLogsPopup(false)
      setShowCostsPopup(false)
      setShowEvaluationPopup(false)
      setShowVersionsPopup(false)
    }
    prevWorkspacePathRef.current = workspacePath
  }, [workspacePath]) // Only depend on workspacePath - popup states are only read, not dependencies
  
  // Local UI state (dropdowns)
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
  const iterationDropdownRef = useRef<HTMLDivElement>(null)
  const startPointDropdownRef = useRef<HTMLDivElement>(null)
  const iterationDropdownButtonRef = useRef<HTMLButtonElement>(null)
  const startPointButtonRef = useRef<HTMLButtonElement>(null)
  
  // State for dropdown positions (for portal rendering)
  const [iterationDropdownPosition, setIterationDropdownPosition] = useState<{ top: number; left: number } | null>(null)
  const [startPointDropdownPosition, setStartPointDropdownPosition] = useState<{ top: number; left: number } | null>(null)
  
  // Keep isRunning for other uses (like dropdown disabled state)
  const isRunning = status === 'running'
  
  // Main workflow execution phase for the canvas toolbar
  const targetExecutionPhaseId = EXECUTION_PHASE_ID
  
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
            if (g.name === groupFolderName) return true
            const sanitized = groupFolderName.toLowerCase().replace(/[^a-z0-9-]/g, '-').replace(/-+/g, '-').trim()
            const groupSanitized = g.name.toLowerCase().replace(/[^a-z0-9-]/g, '-').replace(/-+/g, '-').trim()
            return sanitized === groupSanitized
          })
          if (matchingGroup) {
            console.log('[WorkflowToolbar] Restoring selectedGroupIds from selectedRunFolder:', matchingGroup.name)
            setSelectedGroupIds([matchingGroup.name])
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

  const isResumingExecution = selectedStartPoint > 0 || selectedBranchStep !== null


  // Helper to format the selected run folder display text
  const getSelectedRunFolderDisplay = useMemo(() => {
    // Show selected group names (iteration is always iteration-0, no need to display)
    if (selectedGroupIds.length > 0 && variablesManifest?.groups) {
      const selectedGroups = variablesManifest.groups.filter(g => selectedGroupIds.includes(g.name))
      if (selectedGroups.length > 0) {
        const groupNames = selectedGroups.map(g => g.name)
        if (groupNames.length === 1) {
          return groupNames[0]
        } else if (groupNames.length <= 3) {
          return groupNames.join(', ')
        } else {
          return `${groupNames.slice(0, 2).join(', ')} +${groupNames.length - 2}`
        }
      }
    }

    return '--Select Group--'
  }, [selectedGroupIds, variablesManifest])

  // Build merged list of iterations and groups
  // Groups from variablesManifest are PRIMARY - runFolders only indicate if groups have run
  const iterationGroups = useMemo(() => {
    interface GroupItem {
      id: string  // Full path like "iteration-1/group-5" or just "iteration-1"
      name: string  // Display name like "group-5" or "iteration-1"
      iteration: string  // e.g., "iteration-1"
      groupName: string | null  // e.g., "group-5" or null if no group
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
    const findMatchingFolder = (iteration: string, group: { name: string }): RunFolder | null => {
      return folders.find(folder => {
        const parts = folder.name.split('/')
        if (parts.length !== 2 || parts[0] !== iteration) return false

        const folderName = parts[1]

        // Check if folder name matches group name
        if (folderName === group.name) return true

        // Check if folder name matches sanitized group name
        const sanitizedGroupName = sanitizeForMatch(group.name)
        const folderNameSanitized = sanitizeForMatch(folderName)
        if (sanitizedGroupName === folderNameSanitized) return true

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
          
          // Determine folder name for the path (use sanitized name)
          const folderName = sanitizeDisplayName(group.name) || group.name

          const item: GroupItem = {
            id: `${iteration}/${folderName}`,
            name: group.name,
            iteration,
            groupName: group.name,
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
            groupName: null,
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
        const existingGroups = iterationMap.get(iteration) || []
        const alreadyExists = existingGroups.some(g => {
          // Check if folder path matches exactly
          if (g.id === folder.name) return true

          // Check if groupName matches
          if (g.groupName === groupName) return true

          // Check if folder name matches sanitized group name
          if (g.name) {
            const sanitizedName = sanitizeForMatch(g.name)
            const folderNameSanitized = sanitizeForMatch(groupName)
            if (sanitizedName === folderNameSanitized) return true
          }

          return false
        })

        if (!alreadyExists) {
          // Folder exists on disk but doesn't match any group in manifest
          // Try to find a matching group in manifest by folder name
          let matchingManifestGroup = null
          if (variablesManifest?.groups) {
            matchingManifestGroup = variablesManifest.groups.find(g => {
              // Check if folder name matches group name
              if (g.name === groupName) return true

              // Check if folder name matches sanitized group name
              const sanitizedName = sanitizeForMatch(g.name)
              const folderNameSanitized = sanitizeForMatch(groupName)
              if (sanitizedName === folderNameSanitized) return true

              return false
            })
          }

          // If we found a matching group in manifest, use its name
          // Otherwise, use folder name as group name (for backward compatibility)
          const item: GroupItem = {
            id: folder.name,
            name: matchingManifestGroup?.name || groupName,
            iteration,
            groupName: matchingManifestGroup?.name || groupName,
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

    return { sortedIterations, iterationMap, items }
  }, [folders, variablesManifest])

  const defaultExecutionSelection = useMemo(() => {
    const enabledManifestGroups = variablesManifest?.groups?.filter(group => group.enabled !== false) || []
    if (enabledManifestGroups.length === 0) {
      return null
    }

    const selectedIteration = selectedRunFolder?.startsWith('iteration-')
      ? selectedRunFolder.split('/')[0]
      : null
    const targetIteration = selectedIteration || iterationGroups.sortedIterations[0] || null
    if (!targetIteration) {
      return null
    }

    const selectedFolderGroupName = extractGroupNameFromFolder(selectedRunFolder, variablesManifest)
    if (selectedFolderGroupName && enabledManifestGroups.some(group => group.name === selectedFolderGroupName)) {
      return {
        groupIds: [selectedFolderGroupName],
        runFolder: selectedRunFolder
          || buildGroupFolderPath(selectedFolderGroupName, targetIteration, variablesManifest)
          || targetIteration
      }
    }

    const groupsInTargetIteration = iterationGroups.iterationMap.get(targetIteration) || []
    const firstEnabledGroup = groupsInTargetIteration.find(group => group.groupName && group.enabled !== false)
    if (firstEnabledGroup?.groupName) {
      return {
        groupIds: [firstEnabledGroup.groupName],
        runFolder: firstEnabledGroup.id
      }
    }

    const fallbackGroupId = enabledManifestGroups[0].name
    return {
      groupIds: [fallbackGroupId],
      runFolder: buildGroupFolderPath(fallbackGroupId, targetIteration, variablesManifest) || targetIteration
    }
  }, [iterationGroups.iterationMap, iterationGroups.sortedIterations, selectedRunFolder, variablesManifest])

  const latestExecutionSelection = useMemo(() => {
    const latestIteration = iterationGroups.sortedIterations[0] || null
    if (!latestIteration) {
      return null
    }

    const groupsInLatestIteration = iterationGroups.iterationMap.get(latestIteration) || []
    const selectedEnabledGroupId = selectedGroupIds.find(groupId =>
      groupsInLatestIteration.some(group => group.groupName === groupId && group.enabled !== false)
    )

    if (selectedEnabledGroupId) {
      return {
        groupIds: [selectedEnabledGroupId],
        runFolder: buildGroupFolderPath(selectedEnabledGroupId, latestIteration, variablesManifest) || latestIteration
      }
    }

    const firstEnabledGroup = groupsInLatestIteration.find(group => group.groupName && group.enabled !== false)
    if (firstEnabledGroup?.groupName) {
      return {
        groupIds: [firstEnabledGroup.groupName],
        runFolder: firstEnabledGroup.id
      }
    }

    return {
      groupIds: [],
      runFolder: latestIteration
    }
  }, [iterationGroups.iterationMap, iterationGroups.sortedIterations, selectedGroupIds, variablesManifest])
  
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
  // Start point is always "from beginning" — resume logic removed
  const startPointOptions: StartPointOption[] = [
    { id: 'from_beginning', label: 'Start from Beginning', icon: Play, description: 'Execute all steps from start' }
  ]

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

  const isExecutionWorkspace =
    workflowWorkspaceView === 'execution' ||
    (workflowWorkspaceSelectionTouched &&
      workflowWorkspaceView === null &&
      (currentPhase === EXECUTION_PHASE_ID || currentPhase === EVAL_EXECUTION_PHASE_ID || currentPhase === REPORT_EXECUTION_PHASE_ID))
  const isBuilderWorkspace =
    workflowWorkspaceView === 'builder' ||
    (workflowWorkspaceSelectionTouched &&
      workflowWorkspaceView === null &&
      currentPhase === 'workflow-builder')
  // View selection should follow the actual canvas/report renderer, not the
  // higher-level workspace mode. That lets Builder/Execution keep whatever
  // view (Flow/Plan/Report) the user last selected.
  const isReportWorkspace = canvasViewMode === 'report'
  const isPlanWorkspace = canvasViewMode === 'plan'
  const isFlowWorkspace = canvasViewMode === 'flow'

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

    const targetExecutionPhaseId = EXECUTION_PHASE_ID

    // Use isExecutionRunning instead of isRunning to allow execution even when other phases are running
    if (!isExecutionRunning) {
      // Check if groups are available and if at least one is selected
      const hasDefinedGroups = !!(variablesManifest?.groups && variablesManifest.groups.length > 0)
      if (!hasDefinedGroups) {
        console.warn('[WorkflowToolbar] Cannot execute: No groups configured')
        return
      }

      if (selectedGroupIds.length === 0) {
        if (!defaultExecutionSelection) {
          console.warn('[WorkflowToolbar] Cannot execute: No groups selected and no default group available')
          return
        }

        console.log('[WorkflowToolbar] Auto-selecting execution target:', defaultExecutionSelection)
        setSelectedGroupIds(defaultExecutionSelection.groupIds)
        setSelectedRunFolder(defaultExecutionSelection.runFolder)

        if (workspacePath) {
          void loadProgress(workspacePath, defaultExecutionSelection.runFolder)
        }
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
        allGroupNames: variablesManifest?.groups?.map(g => g.name) || []
      })
      const options = buildExecutionOptions()
      console.log('[EXECUTION_OPTIONS_DEBUG] [WorkflowToolbar] Starting execution with options:', JSON.stringify({
        execution_strategy: options.execution_strategy,
        resume_from_step: options.resume_from_step,
        resume_from_branch_step: options.resume_from_branch_step,
        selected_run_folder: options.selected_run_folder,
        run_mode: options.run_mode,
        enabled_group_names: options.enabled_group_names,
        enabled_group_names_length: options.enabled_group_names?.length || 0
      }, null, 2))

      // Start phase (will create new tab if none exists, or use existing if we switched to it)
      onStartPhase(targetExecutionPhaseId, options)
    }
  }, [
    isExecutionRunning,
    isExecutionStarting,
    buildExecutionOptions,
    defaultExecutionSelection,
    loadProgress,
    onStartPhase,
    selectedGroupIds,
    setSelectedGroupIds,
    setSelectedRunFolder,
    variablesManifest,
    workspacePath
  ])

  const handleRunEvaluation = useCallback(async (runFolder: string) => {
    if (!runFolder || !runFolder.includes('/')) {
      throw new Error('Select a group-scoped run folder like iteration-2/manish before running evaluation.')
    }

    const options = buildExecutionOptions()
    const inferredGroupName = extractGroupNameFromFolder(runFolder, variablesManifest)
    const enabledGroupNames = inferredGroupName
      ? [inferredGroupName]
      : (options.enabled_group_names && options.enabled_group_names.length > 0 ? options.enabled_group_names : undefined)

    onStartPhase(EVAL_EXECUTION_PHASE_ID, {
      ...options,
      selected_run_folder: runFolder,
      enabled_group_names: enabledGroupNames,
    })
  }, [buildExecutionOptions, onStartPhase, variablesManifest])

  return (
    <>
    <div className={`
      flex items-center justify-between gap-2 px-3 py-1.5 
      bg-background border-b border-border
      relative z-10
      ${className}
    `}>
      {/* Left side - workflow context */}
      <div className="flex items-center gap-3 flex-wrap">
        <div className="flex items-center gap-2">
          <span className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
            Mode
          </span>
          <div className="inline-flex items-center gap-0.5 rounded-lg border border-border bg-muted/60 p-0.5 shadow-sm">
              <button
                onClick={() => {
                  const store = useWorkflowStore.getState()
                  store.setWorkflowWorkspaceView('builder')
                  // Builder owns the chat/workshop experience, so selecting it should
                  // also surface the chat panel rather than requiring a second control.
                  store.setShowChatArea(true)
                  onStartPhase('workflow-builder')
                }}
                className={`px-3 py-1 rounded-md text-xs font-medium transition-all ${
                  isBuilderWorkspace
                    ? 'bg-background text-foreground shadow-sm'
                    : 'text-muted-foreground hover:bg-background/70 hover:text-foreground'
                }`}
              >
                Builder
              </button>
              <button
                onClick={() => {
                  setWorkflowWorkspaceView('execution')
                  if (latestExecutionSelection?.runFolder) {
                    setSelectedRunFolder(latestExecutionSelection.runFolder)
                    if (latestExecutionSelection.groupIds.length > 0) {
                      setSelectedGroupIds(latestExecutionSelection.groupIds)
                    } else {
                      clearSelectedGroupIds()
                    }
                  }
                }}
                className={`px-3 py-1 rounded-md text-xs font-medium transition-all ${
                  isExecutionWorkspace
                    ? 'bg-background text-foreground shadow-sm'
                    : 'text-muted-foreground hover:bg-background/70 hover:text-foreground'
                }`}
              >
                Execution
              </button>
          </div>
        </div>

        <div className="h-5 w-px bg-border" />

        <div className="flex items-center gap-2">
          <span className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
            View
          </span>
          <div className="inline-flex items-center gap-0.5 rounded-lg border border-border bg-muted/60 p-0.5 shadow-sm">
              <button
                onClick={() => {
                  const store = useWorkflowStore.getState()
                  store.setWorkflowWorkspaceView('flow')
                  // Hide chat entirely so the canvas becomes visible. Just collapsing
                  // chatAreaExpanded is not enough — WorkflowLayout derives effective
                  // expansion as `chatAreaExpandedManual || !workspaceMinimized`, which
                  // re-expands chat whenever the workspace panel isn't minimized.
                  store.setShowChatArea(false)
                  store.setCanvasViewMode('flow')
                }}
                className={`px-3 py-1 rounded-md text-xs font-medium transition-all ${
                  isFlowWorkspace
                    ? 'bg-background text-foreground shadow-sm'
                    : 'text-muted-foreground hover:bg-background/70 hover:text-foreground'
                }`}
              >
                Flow
              </button>
              <button
                onClick={() => {
                  const store = useWorkflowStore.getState()
                  store.setWorkflowWorkspaceView('plan')
                  store.setShowChatArea(false)
                  store.setCanvasViewMode('plan')
                }}
                className={`px-3 py-1 rounded-md text-xs font-medium transition-all ${
                  isPlanWorkspace
                    ? 'bg-background text-foreground shadow-sm'
                    : 'text-muted-foreground hover:bg-background/70 hover:text-foreground'
                }`}
              >
                Plan
              </button>
              <button
                onClick={() => {
                  const store = useWorkflowStore.getState()
                  store.setWorkflowWorkspaceView('report')
                  store.setShowChatArea(false)
                  store.setCanvasViewMode('report')
                }}
                className={`px-3 py-1 rounded-md text-xs font-medium transition-all ${
                  isReportWorkspace
                    ? 'bg-background text-foreground shadow-sm'
                    : 'text-muted-foreground hover:bg-background/70 hover:text-foreground'
                }`}
              >
                Report
              </button>
          </div>
        </div>

        <>
            {/* Global Overrides Button */}
            <TooltipProvider delayDuration={150}>
              <Tooltip>
                <TooltipTrigger asChild>
                  <button
                    onClick={() => setShowBulkStepConfigModal(true)}
                    className="flex items-center justify-center w-7 h-7 rounded-md transition-all bg-muted text-foreground hover:bg-accent"
                  >
                    <SlidersHorizontal className="w-3.5 h-3.5" />
                  </button>
                </TooltipTrigger>
                <TooltipContent>
                  <p>Global overrides</p>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>

            {/* Execution Controls - Execute button and configuration dropdowns */}
            {isExecutionWorkspace && (
              <>
                {/* Group Selector */}
                {isExecutionWorkspace && <div className="relative" ref={iterationDropdownRef}>
                  <button
                    ref={iterationDropdownButtonRef}
                    onClick={(e) => {
                      e.stopPropagation()
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
                  
                  {/* Group Dropdown - rendered via portal for proper z-index */}
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
                        {variablesManifest?.groups && variablesManifest.groups.length > 0 ? (
                          <>
                            <div className="text-xs font-semibold text-muted-foreground uppercase tracking-wider px-3 py-1">
                              Groups
                            </div>
                            {/* Select All / Unselect All - only if multiple groups */}
                            {(variablesManifest.groups ?? []).filter(g => g.enabled !== false).length > 1 && (
                              <div className="flex items-center justify-end gap-1 px-2 pb-1">
                                <button
                                  onClick={() => {
                                    const allGroupIds = (variablesManifest.groups ?? [])
                                      .filter(g => g.enabled !== false)
                                      .map(g => g.name)
                                    setSelectedGroupIds(allGroupIds)
                                    setSelectedRunFolder('iteration-0')
                                  }}
                                  disabled={isRunning}
                                  className="p-1 rounded hover:bg-accent disabled:opacity-50 disabled:cursor-not-allowed"
                                >
                                  <CheckSquare className="w-3.5 h-3.5 text-primary" />
                                </button>
                                <button
                                  onClick={() => {
                                    clearSelectedGroupIds()
                                    setSelectedRunFolder('iteration-0')
                                  }}
                                  disabled={isRunning}
                                  className="p-1 rounded hover:bg-accent disabled:opacity-50 disabled:cursor-not-allowed"
                                >
                                  <Square className="w-3.5 h-3.5 text-muted-foreground" />
                                </button>
                              </div>
                            )}
                            {(variablesManifest.groups ?? []).map((group) => {
                              const isDisabled = group.enabled === false
                              const isGroupChecked = selectedGroupIds.includes(group.name)
                              const hasMultipleGroups = (variablesManifest.groups ?? []).filter(g => g.enabled !== false).length > 1

                              return (
                                <div
                                  key={group.name}
                                  className={`
                                    group flex items-center gap-1 px-1
                                    ${isGroupChecked ? 'bg-primary/10' : ''}
                                    ${isDisabled ? 'opacity-60' : ''}
                                  `}
                                >
                                  {hasMultipleGroups && (
                                    <input
                                      type="checkbox"
                                      checked={isGroupChecked}
                                      onChange={() => {
                                        if (group.name) {
                                          toggleGroupSelection(group.name)
                                          setSelectedRunFolder('iteration-0')
                                        }
                                      }}
                                      disabled={isDisabled || isRunning}
                                      className="w-4 h-4 rounded border-border text-primary focus:ring-primary focus:ring-2 cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
                                    />
                                  )}
                                  <button
                                    onClick={() => {
                                      if (group.name) {
                                        setSelectedGroupIds([group.name])
                                        setSelectedRunFolder('iteration-0')
                                        setIsIterationDropdownOpen(false)
                                      }
                                    }}
                                    disabled={isDisabled || isRunning}
                                    className={`
                                      flex-1 text-left px-3 py-2 rounded-md text-sm flex items-center gap-2
                                      ${isGroupChecked
                                        ? 'bg-primary/10 text-primary'
                                        : isDisabled
                                        ? 'hover:bg-accent text-muted-foreground'
                                        : 'hover:bg-accent text-foreground'
                                      }
                                    `}
                                  >
                                    <span className="flex-1 text-xs font-medium">
                                      {group.name}
                                    </span>
                                    {isGroupChecked && <Check className="w-4 h-4 ml-auto" />}
                                  </button>
                                </div>
                              )
                            })}
                          </>
                        ) : (
                          <div className="px-3 py-2 text-xs text-muted-foreground">
                            No variable groups defined. Add groups in Variables.
                          </div>
                        )}
                      </div>
                    </div>,
                    document.body
                  )}
                </div>}

                <div className="w-px h-5 bg-border" />

                {/* Execute/Stop Button - Changes to Stop when execution phase is running */}
                {/* Disable only when execution cannot resolve any selectable group. */}
                {(() => {
                  const hasGroups = !!(variablesManifest?.groups && variablesManifest.groups.length > 0)
                  const noGroupsSelected = selectedGroupIds.length === 0
                  const canAutoSelectGroup = !!defaultExecutionSelection
                  const missingPlan = !hasPlan
                  const isDisabled = isExecutionStarting || (!isExecutionRunning && (missingPlan || !hasGroups || (noGroupsSelected && !canAutoSelectGroup)))

                  return (
                    <button
                      onClick={isExecutionRunning ? onStop : handleExecute}
                      disabled={isDisabled}
                      title={!isExecutionRunning && missingPlan ? 'Build a plan before executing.' : undefined}
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
              </>
            )}

        </>
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

        {/* Show Knowledgebase — entities/relationships accumulated by the KB update agent */}
        {workspacePath && (
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={() => setShowKBPopup(true)}
                className="p-1.5 rounded-md bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors"
              >
                <Database className="w-3.5 h-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="bottom"><p>Knowledgebase</p></TooltipContent>
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

        {/* Multi-Select Indicator - appears when 2+ steps are selected */}
        {!isExecutionWorkspace && selectedStepIds && selectedStepIds.length >= 2 && (
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
    
    {/* Global Overrides Modal */}
    <BulkStepConfigModal
      isOpen={showBulkStepConfigModal}
      onClose={() => setShowBulkStepConfigModal(false)}
      workspacePath={workspacePath ?? null}
    />

    {/* Learnings Popup */}
    <LearningsPopup
      isOpen={showLearningsPopup}
      onClose={() => setShowLearningsPopup(false)}
      workspacePath={workspacePath || null}
      plan={plan || null}
    />

    {/* Knowledgebase Popup */}
    <KBPopup
      isOpen={showKBPopup}
      onClose={() => setShowKBPopup(false)}
      workspacePath={workspacePath || null}
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
      runFolders={runFoldersNames}
      onRunEvaluation={handleRunEvaluation}
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
