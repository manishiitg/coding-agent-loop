import React, { useEffect, useRef, useMemo, useCallback, useState } from 'react'
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
} from 'lucide-react'
import { useWorkspaceStore } from '../../../stores/useWorkspaceStore'
import { useWorkflowStore, type RunFolder } from '../../../stores/useWorkflowStore'
import { useChatStore } from '../../../stores/useChatStore'
import type { PlannerFile, WorkflowPhase, StepProgress, VariablesManifest } from '../../../services/api-types'
import type { PlanningResponse } from '../../../utils/stepConfigMatching'
import type { WorkflowExecutionStatus } from '../hooks/useWorkflowExecution'
import type { ExecutionOptions } from '../../../services/api-types'
import { agentApi } from '../../../services/api'
import ConfirmationDialog from '../../ui/ConfirmationDialog'
import LLMOverrideModal from '../LLMOverrideModal'
import BulkStepConfigModal from '../BulkStepConfigModal'
import LearningsPopup from '../LearningsPopup'
import ExecutionLogsPopup from '../ExecutionLogsPopup'
import EvaluationPopup from '../EvaluationPopup'
import CostsPopup from '../CostsPopup'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../../ui/tooltip'
import type { PlanStep } from '../../../utils/stepConfigMatching'
import { isConditionalStep } from '../../../utils/stepConfigMatching'
import { sanitizeDisplayNameForFolder, resolveGroupFolderPath } from '../../../utils/workflowUtils'

// Execution phase ID - special phase that should be displayed separately
const EXECUTION_PHASE_ID = 'execution'


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
  onRefresh,
  onSaveLayout,
  onDeleteLayout,
  hasUnsavedLayoutChanges = false,
  isSavingLayout = false,
  isDeletingLayout = false,
  className = ''
}) => {
  // Normalize runFolders to avoid repeated null checks throughout the component
  const folders = runFolders ?? []

  // Workspace store for opening folders
  const fetchFiles = useWorkspaceStore(state => state.fetchFiles)

  // Workflow store - use useShallow to prevent unnecessary re-renders
  // Note: runFolders, variablesManifest, stepProgress come from props (passed from WorkflowCanvas)
  const {
    phases,
    isLoadingPhases,
    phasesInitialized,
    selectedRunFolder,
    selectedExecutionMode,
    selectedStartPoint,
    selectedBranchStep,
    tempOverrideLLM,
    tempOverrideLLM2,
    tempOverrideLLMEnabled,
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
    saveSettings,
    setTempOverrideLLMEnabled,
    clearTempOverrideLLM,
    clearTempOverrideLLM2,
    toggleGroupSelection,
    setSelectedGroupIds,
    clearSelectedGroupIds
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
    saveSettings: state.saveSettings,
    setTempOverrideLLMEnabled: state.setTempOverrideLLMEnabled,
    clearTempOverrideLLM: state.clearTempOverrideLLM,
    clearTempOverrideLLM2: state.clearTempOverrideLLM2,
    toggleGroupSelection: state.toggleGroupSelection,
    setSelectedGroupIds: state.setSelectedGroupIds,
    clearSelectedGroupIds: state.clearSelectedGroupIds
  })))

  // Calculate the best run folder to use for popups (context-aware)
  // Priority: currentRunningGroupId > selectedRunFolder (if group path) > first selectedGroupIds
  const contextRunFolder = useMemo(() => {
    const resolved = resolveGroupFolderPath({
      currentRunningGroupId,
      selectedRunFolder,
      selectedGroupIds,
      manifest: variablesManifest
    })
    return resolved || (selectedRunFolder === 'new' ? null : selectedRunFolder)
  }, [currentRunningGroupId, selectedRunFolder, selectedGroupIds, variablesManifest])
  
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
  
  // State for expanded iterations (only show groups when expanded)
  const [expandedIterations, setExpandedIterations] = React.useState<Set<string>>(new Set())
  
  // Refs for dropdown click-outside detection
  const dropdownRef = useRef<HTMLDivElement>(null)
  const iterationDropdownRef = useRef<HTMLDivElement>(null)
  const startPointDropdownRef = useRef<HTMLDivElement>(null)
  
  // Keep isRunning for other uses (like dropdown disabled state)
  const isRunning = status === 'running'
  
  // Check if execution phase specifically is running (not just any phase)
  // Use a selector that only recalculates when chatTabs or pollingInterval actually change
  const isExecutionRunning = useChatStore(state => {
    const chatTabs = state.chatTabs
    const pollingInterval = state.pollingInterval
    const allTabs = Object.values(chatTabs)
    
    try {
      // Filter for execution phase tabs
      const executionTabs = allTabs.filter(tab => 
        tab.metadata?.mode === 'workflow' && tab.metadata?.phaseId === EXECUTION_PHASE_ID
      )
      
      // Check if any execution tab is streaming
      return executionTabs.some(tab => {
        // If tab is completed, it's not streaming
        if (tab.isCompleted) return false
        
        // Tab is streaming if polling is active and tab is not manually paused
        const isPolling = pollingInterval !== null
        if (isPolling) {
          return tab.isStreaming !== false // Respect manual pause
        }
        
        return false
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

  // selectedGroupIds is already included in the batched selector above
  
  // Save settings when they change (including selectedGroupIds)
  // Use refs to track previous values and only save when they actually change
  const prevSettingsRef = useRef<string>('')
  useEffect(() => {
    const settingsKey = `${presetQueryId}|${selectedRunFolder}|${selectedExecutionMode}|${selectedStartPoint}|${JSON.stringify(selectedGroupIds)}`
    
    // Only save if settings actually changed
    if (prevSettingsRef.current !== settingsKey && presetQueryId) {
      prevSettingsRef.current = settingsKey
      saveSettings(presetQueryId)
    }
  }, [presetQueryId, selectedRunFolder, selectedExecutionMode, selectedStartPoint, selectedGroupIds, saveSettings])

  // NOTE: loadRunFolders and loadProgress are NOT called here anymore.
  // useWorkspaceState in WorkflowCanvas handles initial load of:
  // - run_folders (via setRunFolders)
  // - variables_manifest (via setVariablesManifest)
  // - selected_progress (via setStepProgress)
  // This eliminates duplicate API calls on initial page load.

  // Close dropdowns when clicking outside
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setIsDropdownOpen(false)
      }
      if (iterationDropdownRef.current && !iterationDropdownRef.current.contains(event.target as Node)) {
        setIsIterationDropdownOpen(false)
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
    if (selectedRunFolder === 'new') {
      return 'New Run'
    }
    if (!selectedRunFolder) {
      return 'Select...'
    }
    // Check if it's a group path (e.g., "iteration-1/group-1" or "iteration-1/production")
    const isGroupPath = selectedRunFolder.includes('/') && selectedRunFolder.split('/').length === 2
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
          // Use the original display name from manifest, not the sanitized folder name
          return `${iteration}/${group.display_name}`
        } else if (group) {
          // No display name, use group_id: "iteration-14/group-1"
          return `${iteration}/${group.group_id}`
        }
      }
      
      // Fallback: show the full path as-is
      return selectedRunFolder
    }
    
    // It's just an iteration folder - check if groups are selected via checkboxes or "All Groups" mode
    if (selectedGroupIds.length > 0 && variablesManifest?.groups) {
      // Find selected groups from manifest
      const selectedGroups = variablesManifest.groups.filter(g => selectedGroupIds.includes(g.group_id))
      if (selectedGroups.length > 0) {
        // Show display names or group_ids of selected groups with iteration prefix
        const groupNames = selectedGroups.map(g => g.display_name || g.group_id)
        if (groupNames.length === 1) {
          // Show full path for single group: "iteration-5/realtraining-188"
          return `${selectedRunFolder}/${groupNames[0]}`
        } else if (groupNames.length <= 3) {
          return groupNames.join(', ')
        } else {
          return `${groupNames.slice(0, 2).join(', ')} +${groupNames.length - 2}`
        }
      }
    }
    
    // Check if it's an iteration without a group path (all groups mode)
    if (!selectedRunFolder.includes('/group-') && !selectedRunFolder.includes('/') && variablesManifest?.groups) {
      const enabledGroups = variablesManifest.groups.filter(g => g.enabled)
      if (enabledGroups.length > 1) {
        return `${selectedRunFolder} (All Groups)`
      }
    }
    
    // Just show the iteration folder name
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
    if (!selectedRunFolder || selectedRunFolder === 'new' || !iterationGroups.sortedIterations.length) {
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
    const dataStr = `${indicesStr}|${branchStepsStr}|${totalSteps}|${planStepsCount}`
    
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
          label: `Start Again from step${stepNum}: ${stepTitle}`,
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
          label: `Resume from step${nextStep}: ${stepTitle}`,
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
  }, [completedStepIndices, totalSteps, plan?.steps?.length, stepProgress?.branch_steps]) // Only depend on specific data, not whole objects - using ref comparison to prevent loops

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

  // Auto-select the latest resume point when there are completed steps
  // CRITICAL: Only auto-resume if there's no saved value in localStorage
  // If there's a saved value, it means user explicitly selected it, so preserve it
  useEffect(() => {
    // Only auto-select if:
    // 1. There are completed steps
    // 2. Current selectedStartPoint is 0 (Start from Beginning)
    // 3. Total steps is known
    // 4. No saved start point in localStorage (user hasn't explicitly selected one)
    if (completedStepIndices.length > 0 && totalSteps > 0 && selectedStartPoint === 0 && presetQueryId) {
      // Check if there's a saved start point in localStorage
      // If there is, don't auto-resume - let loadSavedSettings handle it
      const STORAGE_KEY_PREFIX = 'workflow_settings_'
      const savedStartPoint = localStorage.getItem(`${STORAGE_KEY_PREFIX}${presetQueryId}_start_point`)
      
      if (savedStartPoint) {
        const parsed = parseInt(savedStartPoint, 10)
        if (!isNaN(parsed) && parsed > 0) {
          // There's a saved value - don't auto-resume, let loadSavedSettings handle it
          console.log('[WorkflowToolbar] Skipping auto-resume - found saved start point in localStorage:', parsed)
          return
        }
      }
      
      // No saved value - safe to auto-resume
      // Calculate the resume point (next step after last completed)
      const completedStepNumbers = completedStepIndices.map(idx => idx + 1).sort((a, b) => a - b)
      const lastCompletedStep = completedStepNumbers[completedStepNumbers.length - 1]
      const nextStep = lastCompletedStep + 1

      // If next step exists, resume from there; otherwise resume from last completed
      const resumePoint = nextStep <= totalSteps ? nextStep : lastCompletedStep

      console.log('[WorkflowToolbar] Auto-resume triggered:', {
        completedSteps: completedStepNumbers,
        lastCompletedStep,
        resumePoint,
        totalSteps
      })
      setStartPoint(resumePoint)
    }
  }, [completedStepIndices, totalSteps, selectedStartPoint, setStartPoint, presetQueryId])

  // Get current phase details
  const currentPhaseDetails = phases.find((p: WorkflowPhase) => p.id === currentPhase)
  // Only consider it execution phase if currentPhase is explicitly set to 'execution'
  // If currentPhase is undefined/null, allow dropdown to be enabled
  const isExecutionPhase = currentPhase === EXECUTION_PHASE_ID

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
    const phaseTabs = getTabsByPhaseId(phaseId)
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
      // This includes plan-improvement, plan-tool-optimization, plan-learnings-alignment,
      // evaluation-execution, and code-exec-debugging
      if (phaseId === 'plan-improvement' || phaseId === 'plan-tool-optimization' ||
          phaseId === 'plan-learnings-alignment' || phaseId === 'evaluation-execution' ||
          phaseId === 'code-exec-debugging') {
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

  // Handle execution button click - finds/reuses execution tab
  const handleExecute = useCallback(async () => {
    // Use isExecutionRunning instead of isRunning to allow execution even when other phases are running
    if (!isExecutionRunning && executionPhase) {
      // CRITICAL: If "new" is selected, create a new iteration folder first
      // This ensures we always have a specific iteration selected before execution
      if (selectedRunFolder === 'new' && workspacePath) {
        console.log('[WorkflowToolbar] "New Run" selected - creating new iteration folder before execution')
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
            
            console.log(`[WorkflowToolbar] Created and selected new iteration folder: ${response.folder_name}`)
          } else {
            console.error('[WorkflowToolbar] Failed to create folder before execution:', response)
            // Don't proceed with execution if folder creation failed
            setIsCreatingFolder(false)
            return
          }
        } catch (error) {
          console.error('[WorkflowToolbar] Error creating new folder before execution:', error)
          // Don't proceed with execution if folder creation failed
          setIsCreatingFolder(false)
          return
        } finally {
          setIsCreatingFolder(false)
        }
      }
      
      // Find existing execution phase tab
      // Get execution tabs from generalized chat store
      const chatStore = useChatStore.getState()
      const allTabs = Object.values(chatStore.chatTabs)
      const executionTabs = allTabs.filter(tab => 
        tab.metadata?.mode === 'workflow' && tab.metadata?.phaseId === EXECUTION_PHASE_ID
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
      
      // Build execution options (now with the newly created folder if it was 'new')
      const options = buildExecutionOptions()
      console.log('[RESUME_DEBUG] 🚀 Starting execution with options:', JSON.stringify({
        execution_strategy: options.execution_strategy,
        resume_from_step: options.resume_from_step,
        resume_from_branch_step: options.resume_from_branch_step,
        selected_run_folder: options.selected_run_folder,
        run_mode: options.run_mode
      }, null, 2))
      
      // Start phase (will create new tab if none exists, or use existing if we switched to it)
      onStartPhase(executionPhase.id, options)
    }
  }, [isExecutionRunning, executionPhase, buildExecutionOptions, onStartPhase, selectedRunFolder, workspacePath, setSelectedRunFolder, loadRunFolders, loadProgress, fetchFiles])

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
            {/* Regular Phases Dropdown Selector - moved before execution button */}
            <div className="relative" ref={dropdownRef}>
              <button
                onClick={() => {
                  console.log('[WorkflowToolbar] Phase dropdown button clicked:', { isLoadingPhases, otherPhasesLength: otherPhases.length, isDropdownOpen })
                  // Allow dropdown to open even when phases are running - enables parallel execution
                  // Only block if phases are loading or there are no phases available
                  if (!isLoadingPhases && otherPhases.length > 0) {
                    setIsDropdownOpen(!isDropdownOpen)
                  } else {
                    console.warn('[WorkflowToolbar] Dropdown blocked:', { isLoadingPhases, otherPhasesLength: otherPhases.length })
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
                ) : (() => {
                  // Check if current phase is running
                  const getTabsByPhaseId = useChatStore.getState().getTabsByPhaseId
                  const getTabStreamingStatus = useChatStore.getState().getTabStreamingStatus
                  const currentPhaseTabs = currentPhase ? getTabsByPhaseId(currentPhase) : []
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

              {/* Dropdown Menu - Only show non-execution phases */}
              {isDropdownOpen && !isLoadingPhases && otherPhases.length > 0 && (
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
                      // Check if THIS specific phase is running or completed
                      const getTabsByPhaseId = useChatStore.getState().getTabsByPhaseId
                      const getTabStreamingStatus = useChatStore.getState().getTabStreamingStatus
                      const phaseTabs = getTabsByPhaseId(phase.id)
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
                              ? 'opacity-50 cursor-not-allowed bg-gray-100 dark:bg-gray-800'
                              : isActive 
                                ? 'bg-gray-200 dark:bg-gray-700 text-gray-900 dark:text-gray-100 font-semibold' 
                                : 'hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-900 dark:text-gray-100'
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
                                    ? 'bg-gray-900 dark:bg-gray-100 text-gray-100 dark:text-gray-900' 
                                    : 'bg-gray-200 dark:bg-gray-600 text-gray-500 dark:text-gray-400'
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
                                  {otherPhases.indexOf(phase) + 1}
                                </span>
                              )}
                            </div>
                            <div className="flex-1 min-w-0">
                              <div className="font-medium text-sm flex items-center gap-2">
                                {phase.title}
                                {isPhaseRunning && (
                                  <span className="text-xs px-1.5 py-0.5 bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300 rounded flex items-center gap-1">
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

            {/* Refresh Button - Reload plan and variables */}
            {onRefresh && (
              <>
                <TooltipProvider>
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
                                 bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600"
                    >
                      <RefreshCw className="w-3.5 h-3.5" />
                    </button>
                  </TooltipTrigger>
                  <TooltipContent>
                    <p>Refresh plan, step config, and variables</p>
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>
                {executionPhase && <div className="w-px h-5 bg-border" />}
              </>
            )}

            {/* Execution Controls - Execute button and configuration dropdowns */}
            {executionPhase && (
              <>
                {/* Execute/Stop Button - Changes to Stop when execution phase is running */}
                <button
                  onClick={isExecutionRunning ? onStop : handleExecute}
                  disabled={isCreatingFolder}
                  className={`
                    flex items-center gap-1.5 px-2.5 py-1.5 rounded-md transition-all text-xs font-semibold
                    ${isExecutionRunning
                      ? 'bg-red-500 dark:bg-red-600 text-white shadow-md hover:bg-red-600 dark:hover:bg-red-700 hover:shadow-lg'
                      : isCreatingFolder
                      ? 'bg-purple-400 dark:bg-purple-500 text-white shadow-md cursor-not-allowed opacity-75'
                      : 'bg-purple-500 dark:bg-purple-600 text-white shadow-md hover:bg-purple-600 dark:hover:bg-purple-700 hover:shadow-lg'
                    }
                  `}
                  title={isExecutionRunning ? 'Stop execution' : isCreatingFolder ? 'Creating new iteration folder...' : 'Execute workflow'}
                >
                  {isExecutionRunning ? (
                    <>
                      <Square className="w-3.5 h-3.5" />
                      <span>Stop</span>
                    </>
                  ) : isCreatingFolder ? (
                    <>
                      <Loader2 className="w-3.5 h-3.5 animate-spin" />
                      <span>Creating...</span>
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
                    onClick={() => !isRunning && !isLoadingWorkspaceState && setIsIterationDropdownOpen(!isIterationDropdownOpen)}
                    disabled={isRunning || isLoadingWorkspaceState}
                    className={`
                      flex items-center gap-1.5 px-2 py-1.5 rounded-md transition-all text-xs font-medium
                      ${isRunning || isLoadingWorkspaceState
                        ? 'bg-muted text-muted-foreground cursor-not-allowed'
                        : 'bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600'
                      }
                    `}
                    title={isLoadingWorkspaceState ? "Loading workspace data..." : "Select iteration folder"}
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
                        
                        {(iterationGroups.sortedIterations.length > 0 || folders.length > 0) ? (
                          <>
                            <div className="border-t border-gray-200 dark:border-gray-700 my-1" />
                            <div className="text-xs font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider px-3 py-1">
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
                                        className="w-full px-3 py-1.5 text-xs font-semibold text-gray-600 dark:text-gray-400 bg-gray-50 dark:bg-gray-900/50 flex items-center justify-between gap-2 hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors cursor-pointer"
                                        title={isExpanded ? 'Collapse iteration' : 'Expand iteration'}
                                      >
                                        <div className="flex items-center gap-2">
                                          {isExpanded ? (
                                            <ChevronDown className="w-3.5 h-3.5" />
                                          ) : (
                                            <ChevronRight className="w-3.5 h-3.5" />
                                          )}
                                          <span>{iteration}</span>
                                        </div>
                                        {/* Select All / Unselect All buttons - only show if multiple groups */}
                                        {hasMultipleGroups && isExpanded && (
                                          <div className="flex items-center gap-1" onClick={(e) => e.stopPropagation()}>
                                            <button
                                              onClick={(e) => {
                                                e.stopPropagation()
                                                const allGroupIds = enabledGroups.map(g => g.groupId!).filter(Boolean) as string[]
                                                setSelectedGroupIds(allGroupIds)
                                                // When selecting all groups, show the iteration folder in workspace
                                                if (allGroupIds.length > 0) {
                                                  setSelectedRunFolder(iteration)
                                                }
                                              }}
                                              disabled={isRunning}
                                              className="p-1 rounded hover:bg-gray-200 dark:hover:bg-gray-700 disabled:opacity-50 disabled:cursor-not-allowed"
                                              title="Select all groups"
                                            >
                                              <CheckSquare className="w-3.5 h-3.5 text-purple-600 dark:text-purple-400" />
                                            </button>
                                            <button
                                              onClick={(e) => {
                                                e.stopPropagation()
                                                clearSelectedGroupIds()
                                                // When unselecting all groups, set to iteration folder
                                                setSelectedRunFolder(iteration)
                                              }}
                                              disabled={isRunning}
                                              className="p-1 rounded hover:bg-gray-200 dark:hover:bg-gray-700 disabled:opacity-50 disabled:cursor-not-allowed"
                                              title="Unselect all groups"
                                            >
                                              <Square className="w-3.5 h-3.5 text-gray-500 dark:text-gray-400" />
                                            </button>
                                          </div>
                                        )}
                                      </div>
                                    ) : null}
                                    
                                    {/* Only show groups when iteration is expanded */}
                                    {isExpanded && (
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
                                          {/* Checkbox for group selection (only show if group has an ID and multiple groups exist) */}
                                          {group.groupId && hasMultipleGroups && (
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
                                                    // Multiple groups selected - set to iteration folder
                                                    setSelectedRunFolder(group.iteration)
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
                                              className="w-4 h-4 rounded border-gray-300 dark:border-gray-600 text-purple-600 focus:ring-purple-500 focus:ring-2 cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
                                              title={isDisabled ? 'Group is disabled' : isGroupChecked ? 'Deselect group' : 'Select group for execution'}
                                            />
                                          )}
                                          <button
                                            onClick={() => {
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
                                                ? 'bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300' 
                                                : isDisabled
                                                ? 'hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-500 dark:text-gray-400'
                                                : 'hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-700 dark:text-gray-300'
                                              }
                                            `}
                                            title={isDisabled ? 'Group is disabled' : group.exists ? undefined : 'Group not run yet'}
                                          >
                                            {/* Only show folder icon if folder exists - checkbox is the primary indicator for selection */}
                                            {group.exists ? (
                                              <FolderOpen className="w-4 h-4" />
                                            ) : hasMultipleGroups ? (
                                              // When checkboxes are present, don't show circle icon (checkbox is the indicator)
                                              null
                                            ) : (
                                              // Only show circle if no checkboxes (single group mode)
                                              <Circle className="w-4 h-4" />
                                            )}
                                            <span className="flex-1 text-xs flex items-center gap-1.5">
                                              <span className={group.displayName ? 'font-medium' : 'font-mono'}>
                                                {group.displayName || group.groupId || group.name}
                                              </span>
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
                                      </>
                                    )}
                                  </div>
                                )
                              })
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
                        ) : !isLoadingWorkspaceState && workspacePath ? (
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

                {/* Dropdown 2: Start Point - Where to start */}
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
        
        {/* Show Costs - opens popup with cost analysis across all iterations */}
        {workspacePath && (
          <button
            onClick={() => setShowCostsPopup(true)}
            className="p-1.5 rounded-md bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400 hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors"
            title="Show cost analysis"
          >
            <DollarSign className="w-3.5 h-3.5" />
          </button>
        )}
        
        {/* Show Execution Logs - opens popup with detailed execution logs */}
        {workspacePath && (
          <button
            onClick={() => setShowExecutionLogsPopup(true)}
            className="p-1.5 rounded-md bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400 hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors"
            title="Show execution logs"
          >
            <FileText className="w-3.5 h-3.5" />
          </button>
        )}
        
        {/* Show Learnings - opens popup with learning metadata */}
        {workspacePath && (
          <button
            onClick={() => setShowLearningsPopup(true)}
            className="p-1.5 rounded-md bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400 hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors"
            title="Show step learnings"
          >
            <BookOpen className="w-3.5 h-3.5" />
          </button>
        )}

        {/* Show Evaluation Reports - opens popup with evaluation scores */}
        {workspacePath && (
          <button
            onClick={() => setShowEvaluationPopup(true)}
            className="p-1.5 rounded-md bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400 hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors"
            title="Show evaluation reports"
          >
            <BarChart3 className="w-3.5 h-3.5" />
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
        
        {/* Bulk Step Config Button */}
        {hasPlan && plan && onBulkUpdateSteps && (
          <button
            onClick={() => setShowBulkStepConfigModal(true)}
            className="p-1.5 rounded-md bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400 hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors"
            title="Bulk configure all steps"
          >
            <Settings className="w-3.5 h-3.5" />
          </button>
        )}
        
        {/* Layout Controls Group - Save and Reset */}
        {(onSaveLayout || onDeleteLayout) && (
          <>
            <div className="w-px h-5 bg-gray-200 dark:bg-gray-700 mx-0.5" />
            <div className="flex items-center gap-1">
              {/* Save Layout Button */}
              {onSaveLayout && (
                <TooltipProvider>
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
                          ? 'bg-gray-100 dark:bg-gray-700 text-gray-400 dark:text-gray-500 cursor-not-allowed'
                          : hasUnsavedLayoutChanges
                          ? 'bg-blue-100 dark:bg-blue-900/30 text-blue-600 dark:text-blue-400 hover:bg-blue-200 dark:hover:bg-blue-900/50 animate-pulse'
                          : 'hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-600 dark:text-gray-400'
                      }`}
                      title={isSavingLayout ? 'Saving layout...' : (hasUnsavedLayoutChanges ? 'Save layout (unsaved changes)' : 'Save layout')}
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
                <TooltipProvider>
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
                            ? 'bg-gray-100 dark:bg-gray-700 text-gray-400 dark:text-gray-500 cursor-not-allowed'
                            : 'hover:bg-orange-100 dark:hover:bg-orange-900/30 text-orange-600 dark:text-orange-400 hover:text-orange-700 dark:hover:text-orange-300'
                        }`}
                        title={isDeletingLayout ? 'Resetting layout...' : 'Reset layout to default'}
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
        onBulkUpdate={onBulkUpdateSteps}
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
      runFolders={folders.map(rf => rf.name)}
      selectedRunFolder={contextRunFolder}
    />

    {/* Execution Logs Popup */}
    <ExecutionLogsPopup
      isOpen={showExecutionLogsPopup}
      onClose={() => setShowExecutionLogsPopup(false)}
      workspacePath={workspacePath || null}
      runFolder={contextRunFolder}
      runFolders={folders.map(rf => rf.name)}
    />

    {/* Evaluation Reports Popup */}
    <EvaluationPopup
      isOpen={showEvaluationPopup}
      onClose={() => setShowEvaluationPopup(false)}
      workspacePath={workspacePath || null}
      selectedRunFolder={contextRunFolder}
    />
    </>
  )
}

WorkflowToolbar.whyDidYouRender = true

export default WorkflowToolbar
