import { useEffect, useCallback, useRef, useMemo, useState } from 'react'
import { Plus, Upload, FolderPlus, ChevronDown, Filter, CheckSquare, X, Trash2 } from 'lucide-react'
import { agentApi, workspaceApi } from '../services/api'
import type { PlannerFile } from '../services/api-types'
import PlannerFileList from './workspace/PlannerFileList'
import { isValidJSON } from '../utils/event-helpers'
import GitSyncStatus from './workspace/GitSyncStatus'
import SemanticSearchSync from './workspace/SemanticSearchSync'
import CreateFolderDialog from './workspace/CreateFolderDialog'
import MoveFileDialog from './workspace/MoveFileDialog'
import RenameFileDialog from './workspace/RenameFileDialog'
import ConfirmationDialog from './ui/ConfirmationDialog'
import ImportProgressDialog from './ui/ImportProgressDialog'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './ui/tooltip'
import { useWorkspaceStore } from '../stores/useWorkspaceStore'
import { useCapabilitiesStore } from '../stores/useCapabilitiesStore'
import { useGlobalPresetStore } from '../stores/useGlobalPresetStore'
import { useModeStore } from '../stores/useModeStore'
import { useWorkflowStore } from '../stores/useWorkflowStore'
import { useChatStore } from '../stores/useChatStore'
import { usePresetApplication } from '../stores/useGlobalPresetStore'
import {
  collectFolderPaths,
  restoreExpandedFolders,
  getOriginalPath,
  isPathWithinFolder,
  adjustFilePathsRecursive,
  findIterationFolders
} from '../utils/workspacePathUtils'
import { useIterationExpansion } from './workspace/useIterationExpansion'
import { useCodePrototypeStore } from '../stores/useCodePrototypeStore'

interface WorkspaceProps {
  minimized: boolean
  onToggleMinimize: () => void
}

export default function Workspace({
  minimized,
  onToggleMinimize
}: WorkspaceProps) {
  // Get mode-specific file context and handlers
  const { selectedModeCategory } = useModeStore()
  const { getActiveTab, setTabConfig } = useChatStore()
  const { getActivePreset } = usePresetApplication()
  
  // Get file context based on mode: chat mode uses tab config, workflow mode uses preset
  const chatFileContext = useMemo(() => {
    if (selectedModeCategory === 'chat') {
      const activeTab = getActiveTab()
      return activeTab?.config?.fileContext || []
    } else if (selectedModeCategory === 'workflow') {
      const activePreset = getActivePreset('workflow')
      if (activePreset?.selectedFolder) {
        return [{
          name: activePreset.selectedFolder.filepath.split('/').pop() || '',
          path: activePreset.selectedFolder.filepath,
          type: (activePreset.selectedFolder.type || 'folder') as 'file' | 'folder'
        }]
      }
    }
    return []
  }, [selectedModeCategory, getActiveTab, getActivePreset])
  
  // Add file to context handler - mode-specific
  const addFileToContext = useCallback((file: { name: string; path: string; type: 'file' | 'folder' }) => {
    if (selectedModeCategory === 'chat') {
      const activeTab = getActiveTab()
      if (activeTab) {
        const currentContext = activeTab.config?.fileContext || []
        setTabConfig(activeTab.tabId, {
          fileContext: [...currentContext, file]
        })
      }
    }
    // Workflow mode doesn't support adding files to context (preset folder is fixed)
  }, [selectedModeCategory, getActiveTab, setTabConfig])

  // Get active workflow preset to filter workspace to selected folder
  // Subscribe directly to store state to make it reactive
  const activePresetId = useGlobalPresetStore(state => state.activePresetIds.workflow)
  const customPresets = useGlobalPresetStore(state => state.customPresets)
  const predefinedPresets = useGlobalPresetStore(state => state.predefinedPresets)
  
  const activeWorkflowPreset = useMemo(() => {
    if (selectedModeCategory === 'workflow' && activePresetId) {
      // Check custom presets first
      const customPreset = customPresets.find(p => p.id === activePresetId)
      if (customPreset) return customPreset
      
      // Check predefined presets
      const predefinedPreset = predefinedPresets.find(p => p.id === activePresetId)
      if (predefinedPreset) return predefinedPreset
    }
    return null
  }, [selectedModeCategory, activePresetId, customPresets, predefinedPresets])


  // Export/Import backup state
  const [isExporting, setIsExporting] = useState(false)
  const [isImporting, setIsImporting] = useState(false)
  const [importProgress, setImportProgress] = useState(0)
  const [importingFileName, setImportingFileName] = useState<string>('')
  const [exportError, setExportError] = useState<string | null>(null)
  const [importError, setImportError] = useState<string | null>(null)
  const [importSuccess, setImportSuccess] = useState<string | null>(null)
  const backupFileInputRef = useRef<HTMLInputElement>(null)

  // Multi-select state
  const [isSelectionMode, setIsSelectionMode] = useState(false)
  const [selectedFiles, setSelectedFiles] = useState<Set<string>>(new Set())
  const [bulkDeleteDialog, setBulkDeleteDialog] = useState<{
    isOpen: boolean
    isLoading: boolean
    items: PlannerFile[]
  }>({
    isOpen: false,
    isLoading: false,
    items: []
  })

  // Server refresh search state (re-fetch file tree when local search finds nothing)
  const [serverSearchLoading, setServerSearchLoading] = useState(false)

  // Search Sync Details state
  const [showSearchSyncDetails, setShowSearchSyncDetails] = useState(false)

  // Multi-file upload state
  const [pendingFiles, setPendingFiles] = useState<File[]>([])
  const [isDragOver, setIsDragOver] = useState(false)
  const [uploadProgress, setUploadProgress] = useState<{ current: number; total: number } | null>(null)
  const [uploadResults, setUploadResults] = useState<{ name: string; success: boolean; error?: string }[] | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)

  const {
    files,
    loading,
    error,
    setError,
    searchQuery,
    setSearchQuery,
    uploadDialog,
    setUploadDialog,
    openUploadDialog,
    closeUploadDialog,
    createFolderDialog,
    openCreateFolderDialog,
    closeCreateFolderDialog,
    deleteDialog,
    setDeleteDialog,
    openDeleteDialog,
    closeDeleteDialog,
    deleteAllFilesDialog,
    setDeleteAllFilesDialog,
    openDeleteAllFilesDialog,
    closeDeleteAllFilesDialog,
    showActionsDropdown,
    setShowActionsDropdown,
    expandedFolders,
    setExpandedFolders,
    expandFoldersForFile,
    expandFoldersToLevel,
    toggleFolder,
    highlightedFile,
    setSelectedFile,
    setFileContent,
    setLoadingFileContent,
    setShowFileContent,
    fetchFiles,
    setActiveFolder,
    setBinaryFileData,
    needsRefresh,
    setNeedsRefresh
  } = useWorkspaceStore()

  const isSemanticSearchEnabled = useCapabilitiesStore(s => s.capabilities?.workspace?.semantic_search_enabled ?? false)
  const isGitSyncEnabled = useCapabilitiesStore(s => s.capabilities?.workspace?.github_sync_enabled ?? false)

  // Note: moveDialog from store is shadowed by local state below
  // We should rename the local one to avoid confusion or remove the one from store if unused
  // But for now, we'll keep the local one as it was added for the rename feature
  // and the store one might be used by other components or hooks
  
  // Local Move Dialog State (overrides store state for this component)
  const [localMoveDialog, setLocalMoveDialog] = useState<{
    isOpen: boolean
    isLoading: boolean
    item: PlannerFile | null
    destinationPath: string
    commitMessage: string
  }>({
    isOpen: false,
    isLoading: false,
    item: null,
    destinationPath: '',
    commitMessage: ''
  })
  const openLocalMoveDialog = (item: PlannerFile) => {
    setLocalMoveDialog({
      isOpen: true,
      isLoading: false,
      item,
      destinationPath: '', // Will be set in dialog
      commitMessage: ''
    })
  }
  const closeLocalMoveDialog = () => {
    setLocalMoveDialog({
      isOpen: false,
      isLoading: false,
      item: null,
      destinationPath: '',
      commitMessage: ''
    })
  }

  // Rename Dialog State
  const [renameDialog, setRenameDialog] = useState<{
    isOpen: boolean
    isLoading: boolean
    item: PlannerFile | null
  }>({
    isOpen: false,
    isLoading: false,
    item: null
  })
  
  const openRenameDialog = (item: PlannerFile) => {
    setRenameDialog({
      isOpen: true,
      isLoading: false,
      item
    })
  }
  
  const closeRenameDialog = () => {
    setRenameDialog({
      isOpen: false,
      isLoading: false,
      item: null
    })
  }
  
  // Ref for the workspace scrollable container
  const workspaceScrollRef = useRef<HTMLDivElement>(null)
  
  // Track which workflow preset we've already auto-expanded to prevent re-expansion
  const autoExpandedWorkflowRef = useRef<string | null>(null)
  // Track whether we've auto-expanded Chats/ for chat mode
  const autoExpandedChatRef = useRef(false)
  // Track whether we've auto-expanded Plans/ for multi-agent mode
  const autoExpandedMultiAgentRef = useRef(false)
  // Tracks which iterations have been lazy-loaded, keyed by workflowFolder
  const loadedIterationsRef = useRef<Map<string, Set<string>>>(new Map())
  // Prevents repeated recovery fetches when wrong files appear in code-prototype mode
  const protoRecoveryInFlightRef = useRef(false)
  
  // Stable empty Set for loadingChildren prop to prevent unnecessary re-renders
  const emptyLoadingSet = useMemo(() => new Set<string>(), [])
  
  // Get workflow folder path from selected workflow folder in the preset
  // The selectedFolder.filepath is the folder path stored in the database
  // We'll use this directly to filter the workspace
  const workflowFolderPath = useMemo(() => {
    if (activeWorkflowPreset?.selectedFolder?.filepath) {
      const filepath = activeWorkflowPreset.selectedFolder.filepath
      // The filepath from the database is the folder path (e.g., "Workflow/MyProject/" or "Workflow/MyProject")
      // If it ends with a file (has extension), get the parent folder
      const parts = filepath.split('/').filter(Boolean)
      if (parts.length > 0) {
        const lastPart = parts[parts.length - 1]
        const isFile = lastPart.includes('.')
        if (isFile && parts.length > 1) {
          // It's a file, get its parent folder
          return parts.slice(0, -1).join('/')
        } else {
          // It's already a folder - use it directly
          return parts.join('/')
        }
      }
    }
    return null
  }, [activeWorkflowPreset])

  const currentPrototypeProject = useCodePrototypeStore(s => s.currentProject)
  const codePrototypeFolderPath = useMemo(() => {
    if (selectedModeCategory === 'code-prototype' && currentPrototypeProject) {
      const path = `Projects/${currentPrototypeProject.name}`
      return path
    }
    return null
  }, [selectedModeCategory, currentPrototypeProject])

  // Determine which folder to pass to the API based on mode
  const activeFolder = useMemo(() => {
    if (selectedModeCategory === 'workflow' && workflowFolderPath) {
      return workflowFolderPath
    }
    if (selectedModeCategory === 'code-prototype' && codePrototypeFolderPath) {
      return codePrototypeFolderPath
    }
    // For chat mode and default, fetch root (Chats + skills are at root level)
    return undefined
  }, [selectedModeCategory, workflowFolderPath, codePrototypeFolderPath])

  // Helper function to apply filtering and path adjustment to files
  // This matches the logic in filteredFiles useMemo to ensure paths are consistent
  const applyFilteringAndPathAdjustment = useCallback((filesToProcess: PlannerFile[]): PlannerFile[] => {
    let result = filesToProcess
    
    // Only filter if we're in workflow mode and have a workflow folder path
    // When in chat mode, show all files regardless of preset
    if (selectedModeCategory === 'workflow' && workflowFolderPath) {
      // Files are already scoped to the workflow folder by the API (folder param)
      // Just adjust filepaths to show workflow folder as root
      result = adjustFilePathsRecursive(result, workflowFolderPath)
    } else if (selectedModeCategory === 'code-prototype' && codePrototypeFolderPath) {
      const hasProjectFiles = result.some(f =>
        f.filepath === codePrototypeFolderPath ||
        f.filepath === codePrototypeFolderPath + '/' ||
        f.filepath.startsWith(codePrototypeFolderPath + '/')
      )
      if (!hasProjectFiles) return []
      const contentsOfFolder = result.filter(f =>
        f.filepath.replace(/\/$/, '') !== codePrototypeFolderPath.replace(/\/$/, '')
      )
      result = adjustFilePathsRecursive(contentsOfFolder, codePrototypeFolderPath)
    } else if (selectedModeCategory === 'multi-agent') {
      // Multi Agent Chat mode: show Plans/, Chats/, Downloads/, skills/ and subagents/ folders
      result = filesToProcess.filter(f => {
        const topFolder = f.filepath.split('/')[0]
        return topFolder === 'Plans' || topFolder === 'Chats' || topFolder === 'Downloads' || topFolder === 'skills' || topFolder === 'subagents'
      })
    } else if (selectedModeCategory === 'chat') {
      // Chat mode: show Chats/, Downloads/, skills/ and subagents/ top-level folders
      result = filesToProcess.filter(f => {
        const topFolder = f.filepath.split('/')[0]
        return topFolder === 'Chats' || topFolder === 'Downloads' || topFolder === 'skills' || topFolder === 'subagents'
      })
    }

    return result
  }, [selectedModeCategory, workflowFolderPath, codePrototypeFolderPath])
  
  // Restore expanded folders when files change
  // This runs after store's fetchFiles completes and handles workflow mode filtering
  useEffect(() => {
    // Skip if no files loaded yet
    if (files.length === 0) return
    
    // In workflow mode, completely skip restore effect to let auto-expand handle it
    // This prevents any interference with the auto-expansion logic
    if ((selectedModeCategory === 'workflow' && workflowFolderPath) ||
        (selectedModeCategory === 'code-prototype' && codePrototypeFolderPath)) {
      return
    }
    
    // Get current expanded folders
    const currentExpanded = expandedFolders
    const previouslyExpanded = new Set(currentExpanded)
    
    // Apply filtering and path adjustment to get display files
    const displayFiles = applyFilteringAndPathAdjustment(files)
    
    // Collect all available folder paths from display files
    const availableFolderPaths = collectFolderPaths(displayFiles, true)
    
    // Restore expanded folders that still exist in the filtered tree
    const restoredExpanded = restoreExpandedFolders(previouslyExpanded, availableFolderPaths)
    
    // Only update if something changed AND we had folders expanded before
    // This prevents auto-collapse when files refresh during agent runs
    if (previouslyExpanded.size > 0 && restoredExpanded.size !== previouslyExpanded.size) {
      console.log('[Workspace] Restoring expanded folders:', {
        previousCount: previouslyExpanded.size,
        restoredCount: restoredExpanded.size,
        lost: previouslyExpanded.size - restoredExpanded.size
      })
      setExpandedFolders(restoredExpanded)
    }
  }, [files, applyFilteringAndPathAdjustment, expandedFolders, setExpandedFolders, selectedModeCategory, workflowFolderPath])
  
  // Function to scroll to highlighted file
  const scrollToHighlightedFile = useCallback((filepath: string) => {
    if (!workspaceScrollRef.current) return
    
    // Find the highlighted file element by looking for the data attribute or class
    // Check both data-filepath (adjusted path) and data-original-filepath (original path)
    // This ensures workspace tool events can scroll to files even when paths are adjusted in workflow mode
    const highlightedElement = workspaceScrollRef.current.querySelector(`[data-filepath="${filepath}"]`) ||
                              workspaceScrollRef.current.querySelector(`[data-original-filepath="${filepath}"]`) ||
                              workspaceScrollRef.current.querySelector(`[data-highlighted="true"]`)
    
    if (highlightedElement) {
      highlightedElement.scrollIntoView({
        behavior: 'smooth',
        block: 'center',
        inline: 'nearest'
      })
    }
  }, [])

  // Helper function to get the original filepath for API calls
  // Uses originalFilepath if available (when path was adjusted for display), otherwise uses filepath
  const getOriginalFilePath = useCallback((file: PlannerFile | string): string => {
    // If file is a string, it's a path string
    if (typeof file === 'string') {
      const filepath = file
      // If we're not in workflow mode or don't have a workflow folder path, return as-is
      if (selectedModeCategory !== 'workflow' || !workflowFolderPath) {
        return filepath
      }
      
      // Check if the filepath has been adjusted (doesn't start with workflow folder path)
      // Use the utility function to reconstruct if needed
      if (!isPathWithinFolder(filepath, workflowFolderPath)) {
        return getOriginalPath(filepath, workflowFolderPath)
      }
      
      // If the filepath already starts with the workflow folder path, use it as-is
      return filepath
    }
    
    // If file is a PlannerFile object, use originalFilepath if available
    return file.originalFilepath || file.filepath
  }, [selectedModeCategory, workflowFolderPath])
  
  // Legacy function name for backward compatibility
  const getFullFilePath = getOriginalFilePath

  // Recursively prune the runs/ subtree to only show the specified iteration
  const pruneRunsToIteration = (node: PlannerFile, iterationName: string): PlannerFile => {
    if (node.filepath === 'runs' && node.children) {
      const matching = node.children.find(c =>
        c.filepath === `runs/${iterationName}` || c.filepath.endsWith(`/${iterationName}`)
      )
      return { ...node, children: matching ? [matching] : [] }
    }
    if (node.children && node.children.length > 0) {
      return { ...node, children: node.children.map(c => pruneRunsToIteration(c, iterationName)) }
    }
    return node
  }

  // Filter by query: match file names and folder names; hide folders with no match and no matching descendants
  const filterFiles = (files: PlannerFile[], query: string): PlannerFile[] => {
    if (!query.trim()) return files
    
    const lowercaseQuery = query.toLowerCase()
    
    const filterRecursive = (fileList: PlannerFile[]): PlannerFile[] => {
      return fileList.map(file => {
        if (file.type === 'folder') {
          const filteredChildren = file.children ? filterRecursive(file.children) : []
          const folderName = file.filepath.split('/').filter(Boolean).pop() || file.filepath
          const folderMatches = folderName.toLowerCase().includes(lowercaseQuery) ||
            file.filepath.toLowerCase().includes(lowercaseQuery)
          const keepFolder = folderMatches || filteredChildren.length > 0
          if (!keepFolder) return null
          return {
            ...file,
            children: filteredChildren
          }
        } else {
          const fileName = file.filepath.split('/').pop() || file.filepath
          const matches = fileName.toLowerCase().includes(lowercaseQuery) ||
            file.filepath.toLowerCase().includes(lowercaseQuery)
          return matches ? file : null
        }
      }).filter(Boolean) as PlannerFile[]
    }
    
    return filterRecursive(files)
  }
  
  // Iteration display filter — which iteration to show in the workspace
  // Gets selectedRunFolder from workflow store early (also used at line ~569 via hook)
  const selectedRunFolder = useWorkflowStore(state => state.selectedRunFolder)
  const [workspaceDisplayedIteration, setWorkspaceDisplayedIteration] = useState<string | null>(null)
  const [showIterationDropdown, setShowIterationDropdown] = useState(false)

  // Reset workspace iteration choice when switching workflows
  const prevActiveFolderForIterRef = useRef<string | undefined>(undefined)
  if (prevActiveFolderForIterRef.current !== activeFolder) {
    prevActiveFolderForIterRef.current = activeFolder
    if (activeFolder) loadedIterationsRef.current.delete(activeFolder)
    // Don't reset workspaceDisplayedIteration — preserve user's selection across minor re-mounts
  }

  // Compute available iterations from the raw file tree (populated even from shallow fetch)
  const availableIterations = useMemo(() => {
    if (selectedModeCategory !== 'workflow' || !workflowFolderPath) return []
    const workflowItem = files.find(f => f.filepath.replace(/\/$/, '') === workflowFolderPath)
    if (!workflowItem?.children) return []
    const runsFolder = workflowItem.children.find(c =>
      c.filepath === workflowFolderPath + '/runs' || c.filepath === workflowFolderPath + '/runs/'
    )
    if (!runsFolder?.children) return []
    return runsFolder.children
      .filter(c => /iteration-\d+/.test(c.filepath))
      .map(c => c.filepath.split('/').pop() ?? '')
      .filter(Boolean)
      .sort((a, b) => {
        const numA = parseInt(a.match(/iteration-(\d+)/)?.[1] ?? '0')
        const numB = parseInt(b.match(/iteration-(\d+)/)?.[1] ?? '0')
        return numA - numB
      })
  }, [files, selectedModeCategory, workflowFolderPath])

  const latestIteration = availableIterations[availableIterations.length - 1] ?? null

  // Effective iteration to display: user override → selectedRunFolder (top-level only) → latest
  const effectiveDisplayedIteration = useMemo(() => {
    if (workspaceDisplayedIteration) return workspaceDisplayedIteration
    if (selectedRunFolder && selectedRunFolder !== 'new') {
      // selectedRunFolder can be "iteration-N" or "iteration-N/group-X" — take top-level only
      return selectedRunFolder.split('/')[0]
    }
    return latestIteration
  }, [workspaceDisplayedIteration, selectedRunFolder, latestIteration])

  // Get filtered files - first filter to workflow folder if preset is active, then apply search
  const filteredFiles = useMemo(() => {
    let result = files

    // Only filter if we're in workflow mode and have a workflow folder path
    // When in chat mode, show all files regardless of preset
    if (selectedModeCategory === 'workflow' && workflowFolderPath) {
      // The API returns the workflow folder itself AND its children as flat top-level siblings.
      // e.g., [Workflow/codeanalysis, Workflow/codeanalysis/knowledgebase, Workflow/codeanalysis/learnings, ...]
      // The folder item (Workflow/codeanalysis) already has children nested inside it.
      // If we adjust all flat items, we get duplicates: the folder's children appear BOTH
      // inside the folder AND as top-level siblings. Fix: find the workflow folder item
      // and use only it (with its nested children) as the root of the tree.
      const workflowFolderItem = result.find(f => {
        const normalized = f.filepath.replace(/\/$/, '')
        const normalizedTarget = workflowFolderPath.replace(/\/$/, '')
        return normalized === normalizedTarget
      })
      if (workflowFolderItem && workflowFolderItem.children && workflowFolderItem.children.length > 0) {
        // Use the folder as a single root with its proper nested children
        result = adjustFilePathsRecursive([workflowFolderItem], workflowFolderPath)
      } else {
        // Fallback: no hierarchical folder found, adjust all items
        result = adjustFilePathsRecursive(result, workflowFolderPath)
      }

      // Filter runs/ to only show the selected/latest iteration (not all iterations)
      if (effectiveDisplayedIteration) {
        result = result.map(node => pruneRunsToIteration(node, effectiveDisplayedIteration))
      }
    } else if (selectedModeCategory === 'code-prototype') {
      if (!codePrototypeFolderPath) return []

      // Guard: if the store's files are from a root/stale fetch (no file belongs to this project),
      // return empty. The recovery effect below will re-fetch the correct project folder.
      const hasProjectFiles = result.some(f =>
        f.filepath === codePrototypeFolderPath ||
        f.filepath === codePrototypeFolderPath + '/' ||
        f.filepath.startsWith(codePrototypeFolderPath + '/')
      )
      if (!hasProjectFiles) return []

      // Exclude the folder node itself; show all contents with paths stripped of the prefix.
      const contentsOfFolder = result.filter(f =>
        f.filepath.replace(/\/$/, '') !== codePrototypeFolderPath.replace(/\/$/, '')
      )
      result = adjustFilePathsRecursive(contentsOfFolder, codePrototypeFolderPath)
    } else if (selectedModeCategory === 'multi-agent') {
      // Multi Agent Chat mode: show Plans/, Chats/, Downloads/, skills/ and subagents/ folders
      result = files.filter(f => {
        const topFolder = f.filepath.split('/')[0]
        return topFolder === 'Plans' || topFolder === 'Chats' || topFolder === 'Downloads' || topFolder === 'skills' || topFolder === 'subagents'
      })
    } else if (selectedModeCategory === 'chat') {
      // Chat mode: show Chats/, Downloads/, skills/ and subagents/ top-level folders
      result = files.filter(f => {
        const topFolder = f.filepath.split('/')[0]
        return topFolder === 'Chats' || topFolder === 'Downloads' || topFolder === 'skills' || topFolder === 'subagents'
      })
    }

    // Apply search filter
    result = filterFiles(result, searchQuery)

    return result
  }, [files, workflowFolderPath, codePrototypeFolderPath, searchQuery, selectedModeCategory, effectiveDisplayedIteration])

  // Refresh file tree from server (re-fetch all files so local filter can find them)
  const handleRefreshAndSearch = useCallback(async () => {
    setServerSearchLoading(true)
    try {
      await fetchFiles(undefined)
    } catch (err) {
      console.error('Failed to refresh files:', err)
    } finally {
      setServerSearchLoading(false)
    }
  }, [fetchFiles])

  // File highlighting with auto-scroll.
  // In workflow mode we only scroll — we do NOT call expandFoldersForFile() because it
  // opens ALL parent folders with no depth limit, overriding the maxLevel=3 cap from
  // the initial auto-expand. The relevant folders should already be open from the
  // auto-expand or run-folder expansion effects.
  // In chat mode we still expand folders since there's no depth concern.
  useEffect(() => {
    if (highlightedFile) {
      if (selectedModeCategory !== 'workflow') {
        // Chat mode: expand folders to reveal the highlighted file
        expandFoldersForFile(highlightedFile)
      }

      // Auto-scroll to highlighted file after a short delay
      setTimeout(() => {
        scrollToHighlightedFile(highlightedFile)
      }, 100)
    }
  }, [highlightedFile, expandFoldersForFile, scrollToHighlightedFile, selectedModeCategory])
  
  // Automatically expand workspace folder when a workflow is first opened
  // Only runs once per workflow preset or mode switch to allow manual open/close afterward
  useEffect(() => {
    if (selectedModeCategory === 'workflow' && workflowFolderPath && filteredFiles.length > 0) {
      // Check if we've already auto-expanded for this workflow preset
      const workflowPresetId = activeWorkflowPreset?.id || workflowFolderPath
      
      // Only auto-expand if we haven't done it for this workflow yet
      if (autoExpandedWorkflowRef.current !== workflowPresetId) {
        // Small delay to ensure files are fully loaded and rendered
        const timeoutId = setTimeout(() => {
          // Determine what files to expand
          const workflowFolder = filteredFiles.length > 0 && filteredFiles[0].type === 'folder' ? filteredFiles[0] : null
          const filesToExpand = workflowFolder && workflowFolder.children
            ? workflowFolder.children 
            : filteredFiles
          
          // Expand top-level workflow structure only — exclude runs/ to avoid expanding all iterations
          const additionalFolders = workflowFolder ? [workflowFolder.filepath] : undefined
          const excludeFolders = ['planning', 'variables', 'learnings', 'logs', 'runs']
          expandFoldersToLevel(filesToExpand, 1, additionalFolders, excludeFolders)
          
          // Mark this workflow as auto-expanded
          autoExpandedWorkflowRef.current = workflowPresetId
        }, 500)
        
        return () => clearTimeout(timeoutId)
      }
    } else if (selectedModeCategory !== 'workflow') {
      // Reset the auto-expanded ref when switching away
      autoExpandedWorkflowRef.current = null
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedModeCategory, workflowFolderPath, filteredFiles, expandFoldersToLevel, activeWorkflowPreset?.id])

  // In chat mode, auto-expand Chats/ folder by default (skills/ stays closed)
  useEffect(() => {
    if (selectedModeCategory === 'chat' && filteredFiles.length > 0 && !autoExpandedChatRef.current) {
      const hasChatsFolder = filteredFiles.some(f => f.filepath === 'Chats' || f.filepath === 'Chats/')
      if (hasChatsFolder) {
        autoExpandedChatRef.current = true
        setExpandedFolders(new Set(['Chats']))
      }
    } else if (selectedModeCategory !== 'chat') {
      autoExpandedChatRef.current = false
    }
  }, [selectedModeCategory, filteredFiles, setExpandedFolders])

  // In multi-agent mode, auto-expand Plans/ folder by default
  useEffect(() => {
    if (selectedModeCategory === 'multi-agent' && filteredFiles.length > 0 && !autoExpandedMultiAgentRef.current) {
      const hasPlansFolder = filteredFiles.some(f => f.filepath === 'Plans' || f.filepath === 'Plans/')
      if (hasPlansFolder) {
        autoExpandedMultiAgentRef.current = true
        setExpandedFolders(new Set(['Plans']))
      }
    } else if (selectedModeCategory !== 'multi-agent') {
      autoExpandedMultiAgentRef.current = false
    }
  }, [selectedModeCategory, filteredFiles, setExpandedFolders])
  
  // Use custom hook to handle iteration expansion logic
  useIterationExpansion({
    selectedModeCategory,
    workflowFolderPath,
    filteredFiles,
    selectedRunFolder,
    expandedFolders,
    setExpandedFolders
  })

  // Lazy-load the selected/latest iteration's full files when it changes
  useEffect(() => {
    if (selectedModeCategory !== 'workflow' || !activeFolder || !effectiveDisplayedIteration) return

    const loaded = loadedIterationsRef.current.get(activeFolder)
    if (loaded?.has(effectiveDisplayedIteration)) return // Already fetched this session

    if (!loadedIterationsRef.current.has(activeFolder)) {
      loadedIterationsRef.current.set(activeFolder, new Set())
    }
    loadedIterationsRef.current.get(activeFolder)!.add(effectiveDisplayedIteration)
    fetchFiles(activeFolder + '/runs/' + effectiveDisplayedIteration)
  }, [selectedModeCategory, activeFolder, effectiveDisplayedIteration, fetchFiles])

  // Close dropdown when clicking outside
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (showActionsDropdown) {
        const target = event.target as Element
        if (!target.closest('.actions-dropdown')) {
          setShowActionsDropdown(false)
        }
        if (!target.closest('.iteration-filter-dropdown')) {
          setShowIterationDropdown(false)
        }
      }
    }

    document.addEventListener('mousedown', handleClickOutside)
    return () => {
      document.removeEventListener('mousedown', handleClickOutside)
    }
  }, [showActionsDropdown, setShowActionsDropdown, showIterationDropdown])

  // Load files on component mount and re-fetch when active folder changes.
  // Optimization: skip the fetch when workspace panel is minimized to avoid wasting bandwidth
  // Keep the store's activeFolder in sync with the workspace scope.
  // This ensures that unscoped fetchFiles() calls from other components (StepNode, WorkflowCanvas,
  // etc.) stay scoped to the workflow folder instead of fetching the entire root tree.
  useEffect(() => {
    setActiveFolder(activeFolder ?? null)
  }, [activeFolder, setActiveFolder])

  // on data the user can't see. When the user opens the panel (minimized transitions false → true),
  // this effect re-runs and triggers the fetch automatically.
  // In workflow mode, use max_depth=1 for the initial fetch to avoid loading all iteration files
  // (which can be slow with many iterations). The selected/latest iteration is loaded separately.
  useEffect(() => {
    if (!minimized) {
      if (selectedModeCategory === 'workflow') {
        fetchFiles(activeFolder, { maxDepth: 1 })
      } else {
        fetchFiles(activeFolder)
      }
    }
  }, [activeFolder, fetchFiles, minimized, selectedModeCategory])

  // Recovery effect for code-prototype mode: if something replaces the store's files with
  // root-level files (race condition from another component's fetchFiles() call), re-fetch
  // the project folder so the workspace doesn't stay blank.
  useEffect(() => {
    if (selectedModeCategory !== 'code-prototype' || !codePrototypeFolderPath || minimized) {
      protoRecoveryInFlightRef.current = false
      return
    }
    if (files.length === 0) return // Initial empty state — normal, no recovery needed

    const hasProjectFiles = files.some(f =>
      f.filepath === codePrototypeFolderPath ||
      f.filepath === codePrototypeFolderPath + '/' ||
      f.filepath.startsWith(codePrototypeFolderPath + '/')
    )
    if (!hasProjectFiles && !protoRecoveryInFlightRef.current) {
      protoRecoveryInFlightRef.current = true
      console.log('[PROTO-WORKSPACE] Wrong files detected, re-fetching project folder')
      fetchFiles(codePrototypeFolderPath).then(() => {
        protoRecoveryInFlightRef.current = false
      })
    } else if (hasProjectFiles) {
      protoRecoveryInFlightRef.current = false
    }
  }, [files, selectedModeCategory, codePrototypeFolderPath, minimized, fetchFiles])

  // Check if a file is a viewable binary format (xlsx, docx, pdf) that we can render
  const isViewableBinaryFile = (fileName: string): boolean => {
    const ext = fileName.split('.').pop()?.toLowerCase() || ''
    return ['xls', 'xlsx', 'docx', 'pdf', 'webm', 'mp4'].includes(ext)
  }

  // Handle file click - fetch content and show in chat area
  const handleFileClick = async (file: PlannerFile) => {
    if (file.type === 'file' || !file.type) {
      // Reconstruct the original full path if we're in workflow mode with filtered files
      const fullFilePath = getOriginalFilePath(file)
      const fileName = fullFilePath.split('/').pop() || fullFilePath

      try {
        setLoadingFileContent(true)

        setSelectedFile({ name: fileName, path: fullFilePath })

        // For viewable binary files (xlsx, docx), fetch as raw binary
        if (isViewableBinaryFile(fileName)) {
          const response = await workspaceApi.get(
            `/api/documents/${encodeURIComponent(fullFilePath)}`,
            { params: { download: 'true' }, responseType: 'arraybuffer' }
          )
          setBinaryFileData(response.data as ArrayBuffer)
          setFileContent('') // Clear text content
          setShowFileContent(true)
          return
        }

        // Clear binary data when viewing text files
        setBinaryFileData(null)

        // Use the reconstructed full filepath for the API call
        const response = await agentApi.getPlannerFileContent(fullFilePath)

        if (response.success && response.data) {
          // Check if content exists and is a string
          if (response.data.content === undefined || response.data.content === null) {
            // Content is missing - this can happen for certain file types or error cases
            setError('File content is not available. This may be a binary file or the file may be empty.')
            setLoadingFileContent(false)
            return
          }

          let processedContent = typeof response.data.content === 'string'
            ? response.data.content
            : String(response.data.content)
          let isJsonFile = false
          let formattedJson = null

          // Check if this is an image file
          if (response.data.is_image && processedContent && processedContent.startsWith('data:image/')) {
            // For images, the content is already base64 encoded data URL
            // No processing needed for images
          } else {
            // Process the content to convert escaped newlines to actual newlines
            // Only process if content is a non-empty string
            if (processedContent && typeof processedContent === 'string') {
              processedContent = processedContent
                .replace(/\\n/g, '\n')  // Convert \n to actual newlines
                .replace(/\\t/g, '\t')  // Convert \t to actual tabs
                .replace(/\\r/g, '\r'); // Convert \r to actual carriage returns

              // Check if this is a JSON file (by extension OR content)
              const extensionIsJson = file.filepath.toLowerCase().endsWith('.json')
              const contentIsJson = isValidJSON(processedContent)
              isJsonFile = extensionIsJson || contentIsJson

              // If it's a JSON file, try to parse and format it
              if (isJsonFile) {
                try {
                  const parsed = JSON.parse(processedContent)
                  formattedJson = JSON.stringify(parsed, null, 2)
                } catch (parseError) {
                  // If JSON parsing fails, keep the original content
                  console.warn('Failed to parse JSON file:', parseError)
                  formattedJson = null
                }
              }
            }
          }

          // Backend returns "[Binary file: N bytes]" for non-text files — single source of truth
          if (typeof processedContent === 'string' && processedContent.startsWith('[Binary file:')) {
            setError(`File "${fileName}" is a binary file and cannot be viewed in the editor.`)
            setLoadingFileContent(false)
            setShowFileContent(false)
            return
          }

          // Store both original content and formatted JSON (if applicable)
          setFileContent(processedContent || '')
          if (formattedJson) {
            setFileContent(formattedJson)
          }
          setShowFileContent(true)
        } else {
          setError(response.message || 'Failed to load file content')
        }
      } catch (err) {
        console.error('Failed to fetch file content:', err)
        setError(err instanceof Error ? err.message : 'Failed to fetch file content')
      } finally {
        setLoadingFileContent(false)
      }
    }
  }

  // Handle folder click - only folders are clickable now
  const handleFolderClick = (folder: PlannerFile) => {
    if (folder.type === 'folder') {
      // In workflow mode, disable direct clicking on iteration/group folders
      // Only allow checkbox-based selection for these folders
      if (selectedModeCategory === 'workflow') {
        const isIterationFolder =
          folder.filepath.includes('/runs/iteration-') ||
          /\/iteration-\d+$/.test(folder.filepath) ||
          /\/runs\/iteration-\d+\/[^/]+$/.test(folder.filepath) // Group folders

        if (isIterationFolder) {
          console.log('[Workspace] Ignoring click on iteration/group folder (use checkbox instead):', folder.filepath)
          return // Don't toggle expansion for iteration/group folders in workflow mode
        }
      }

      // Toggle folder expansion
      if (expandedFolders.has(folder.filepath)) {
        // Collapse folder
        toggleFolder(folder.filepath)
      } else {
        // Expand folder - children are already loaded
        toggleFolder(folder.filepath)
      }
    }
  }

  // Handle file delete
  const handleFileDelete = (file: PlannerFile) => {
    openDeleteDialog(file)
  }

  // Handle folder delete
  const handleFolderDelete = (folder: PlannerFile) => {
    openDeleteDialog(folder)
  }

  // Handle delete all contents in folder
  const handleDeleteAllFilesInFolder = (folder: PlannerFile) => {
    openDeleteAllFilesDialog(folder)
  }

  // Helper function to collect only top-level file paths (not recursive)
  // API handles recursive deletion automatically, so we only need to select top-level items
  const collectTopLevelFilePaths = useCallback((files: PlannerFile[]): string[] => {
    return files.map(file => file.filepath)
  }, [])

  // Toggle selection mode
  const toggleSelectionMode = useCallback(() => {
    setIsSelectionMode(prev => {
      if (!prev) {
        // Entering selection mode - clear any previous selections
        setSelectedFiles(new Set())
      } else {
        // Exiting selection mode - clear selections
        setSelectedFiles(new Set())
      }
      return !prev
    })
  }, [])

  // Toggle file selection - only select/unselect the item itself (not children)
  // API handles recursive deletion automatically, so we don't need to select children
  const toggleFileSelection = useCallback((file: PlannerFile) => {
    setSelectedFiles(prev => {
      const newSet = new Set(prev)
      const filePath = file.filepath
      
      if (newSet.has(filePath)) {
        // Unselect: remove this item only (not children)
        newSet.delete(filePath)
      } else {
        // Select: add this item only (not children)
        newSet.add(filePath)
      }
      return newSet
    })
  }, [])

  // Select file and enter selection mode
  // If it's a folder with children, enter selection mode without pre-selecting the folder itself
  // (to prevent accidentally deleting the entire folder when user only wants to select children)
  // If it's a file or leaf folder, pre-select it
  const selectFileAndEnterSelectionMode = useCallback((file: PlannerFile) => {
    setIsSelectionMode(true)
    if (file.type === 'folder' && file.children && file.children.length > 0) {
      // Don't pre-select parent folders — user likely wants to select items inside
      setSelectedFiles(new Set())
    } else {
      setSelectedFiles(new Set([file.filepath]))
    }
  }, [])

  // Select/Deselect all visible files (top-level only, not recursive)
  // API handles recursive deletion automatically, so we only need to select top-level items
  const toggleSelectAll = useCallback(() => {
    const allPaths = collectTopLevelFilePaths(filteredFiles)
    setSelectedFiles(prev => {
      const newSet = new Set(prev)
      // Check if all top-level items are selected
      const allSelected = allPaths.length > 0 && allPaths.every(path => newSet.has(path))
      if (allSelected) {
        // Deselect all top-level items
        allPaths.forEach(path => newSet.delete(path))
      } else {
        // Select all top-level items
        allPaths.forEach(path => newSet.add(path))
      }
      return newSet
    })
  }, [filteredFiles, collectTopLevelFilePaths])

  // Check if all top-level files are selected
  const areAllFilesSelected = useMemo(() => {
    if (selectedFiles.size === 0) return false
    const allPaths = collectTopLevelFilePaths(filteredFiles)
    return allPaths.length > 0 && allPaths.every(path => selectedFiles.has(path))
  }, [selectedFiles, filteredFiles, collectTopLevelFilePaths])

  // Get selected files as PlannerFile objects
  const getSelectedFilesAsObjects = useCallback((): PlannerFile[] => {
    const findFileByPath = (files: PlannerFile[], path: string): PlannerFile | null => {
      for (const file of files) {
        if (file.filepath === path) return file
        if (file.children) {
          const found = findFileByPath(file.children, path)
          if (found) return found
        }
      }
      return null
    }

    const selected: PlannerFile[] = []
    selectedFiles.forEach(path => {
      const file = findFileByPath(filteredFiles, path)
      if (file) selected.push(file)
    })
    return selected
  }, [selectedFiles, filteredFiles])

  // Handle bulk delete
  const handleBulkDelete = useCallback(() => {
    const selectedItems = getSelectedFilesAsObjects()
    console.log('[BulkDelete] Selected files set:', Array.from(selectedFiles))
    console.log('[BulkDelete] Resolved items:', selectedItems.map(item => ({
      filepath: item.filepath,
      originalFilepath: item.originalFilepath,
      type: item.type,
      hasChildren: !!(item.children && item.children.length > 0)
    })))
    if (selectedItems.length === 0) return
    setBulkDeleteDialog({
      isOpen: true,
      isLoading: false,
      items: selectedItems
    })
  }, [getSelectedFilesAsObjects, selectedFiles])

  // Confirm bulk delete
  const confirmBulkDelete = async () => {
    if (bulkDeleteDialog.items.length === 0) return

    setBulkDeleteDialog(prev => ({ ...prev, isLoading: true }))

    try {
      const errors: string[] = []

      // Deduplicate: if a parent folder is selected along with its children,
      // only delete the parent (folder deletion is recursive).
      // This prevents accidentally deleting more than intended.
      const allPaths = new Set(bulkDeleteDialog.items.map(item => getOriginalFilePath(item)))
      const itemsToDelete = bulkDeleteDialog.items.filter(item => {
        const itemPath = getOriginalFilePath(item)
        // Check if any ancestor of this item is also in the selection
        const parts = itemPath.split('/')
        for (let i = 1; i < parts.length; i++) {
          const ancestorPath = parts.slice(0, i).join('/')
          if (allPaths.has(ancestorPath)) {
            // An ancestor folder is also selected — skip this item (parent will handle it)
            console.log(`[BulkDelete] Skipping "${itemPath}" — ancestor "${ancestorPath}" is also selected`)
            return false
          }
        }
        return true
      })

      console.log('[BulkDelete] Items to delete:', itemsToDelete.map(item => ({
        filepath: item.filepath,
        originalFilepath: item.originalFilepath,
        resolvedPath: getOriginalFilePath(item),
        type: item.type
      })))

      // Delete each item sequentially
      for (const item of itemsToDelete) {
        try {
          const fullFilePath = getOriginalFilePath(item)
          console.log(`[BulkDelete] Deleting ${item.type}: "${fullFilePath}" (display: "${item.filepath}")`)
          if (item.type === 'file') {
            await agentApi.deletePlannerFile(fullFilePath)
          } else {
            await agentApi.deletePlannerFolder(fullFilePath)
          }
          console.log(`[BulkDelete] Successfully deleted: "${fullFilePath}"`)
        } catch (err) {
          const fileName = item.filepath.split('/').pop() || item.filepath
          errors.push(`${fileName}: ${err instanceof Error ? err.message : 'Failed to delete'}`)
          console.error(`[BulkDelete] Failed to delete "${item.filepath}":`, err)
        }
      }

      // Refresh the file list
      await fetchFiles(activeFolder)

      // Clear selection and exit selection mode
      setSelectedFiles(new Set())
      setIsSelectionMode(false)

      // Close dialog
      setBulkDeleteDialog({ isOpen: false, isLoading: false, items: [] })

      // Show errors if any
      if (errors.length > 0) {
        setError(`Some files could not be deleted:\n${errors.join('\n')}`)
      }
    } catch (err) {
      console.error('Failed to delete items:', err)
      setError(err instanceof Error ? err.message : 'Failed to delete items')
      setBulkDeleteDialog(prev => ({ ...prev, isLoading: false }))
    }
  }

  // Cancel bulk delete
  const cancelBulkDelete = () => {
    setBulkDeleteDialog({ isOpen: false, isLoading: false, items: [] })
  }

  // Confirm delete
  const confirmDelete = async () => {
    if (!deleteDialog.item) return

    setDeleteDialog({ isLoading: true })

    try {
      // Use original filepath if available (when path was adjusted for display)
      const fullFilePath = getOriginalFilePath(deleteDialog.item)
      
      // Extract parent folder path to preserve its expanded state
      const itemPath = deleteDialog.item.filepath // Use display path for expanded folders
      const pathParts = itemPath.split('/').filter(Boolean)
      let parentFolderPath: string | null = null
      
      if (pathParts.length > 1) {
        // Parent folder is all parts except the last one
        parentFolderPath = pathParts.slice(0, -1).join('/')
      } else if (pathParts.length === 1) {
        // Item is at root level, no parent folder
        parentFolderPath = null
      }
      
      // Preserve parent folder expansion before refresh
      if (parentFolderPath) {
        const currentExpanded = useWorkspaceStore.getState().expandedFolders
        const newExpanded = new Set(currentExpanded)
        newExpanded.add(parentFolderPath)
        setExpandedFolders(newExpanded)
      }
      
      if (deleteDialog.item.type === 'file') {
        await agentApi.deletePlannerFile(fullFilePath)
      } else {
        await agentApi.deletePlannerFolder(fullFilePath)
      }
      
      // Refresh the file list to show updated state
      await fetchFiles(activeFolder)
      
      // Close dialog
      closeDeleteDialog()
    } catch (err) {
      console.error('Failed to delete item:', err)
      setError(err instanceof Error ? err.message : 'Failed to delete item')
      setDeleteDialog({ isLoading: false })
    }
  }

  // Cancel delete
  const cancelDelete = () => {
    closeDeleteDialog()
  }

  // Confirm delete all contents
  const confirmDeleteAllFiles = async () => {
    if (!deleteAllFilesDialog.folder) return

    setDeleteAllFilesDialog({ isLoading: true })

    try {
      // Use original filepath if available (when path was adjusted for display)
      const fullFolderPath = getOriginalFilePath(deleteAllFilesDialog.folder)
      
      await agentApi.deleteAllFilesInFolder(fullFolderPath)
      
      // Refresh the file list to show updated state
      await fetchFiles(activeFolder)
      
      // Close dialog
      closeDeleteAllFilesDialog()
    } catch (err) {
      console.error('Failed to delete all contents:', err)
      setError(err instanceof Error ? err.message : 'Failed to delete all contents')
      setDeleteAllFilesDialog({ isLoading: false })
    }
  }

  // Cancel delete all contents
  const cancelDeleteAllFiles = () => {
    closeDeleteAllFilesDialog()
  }

  // Handle file download
  const handleFileDownload = async (file: PlannerFile) => {
    try {
      // Use original filepath if available (when path was adjusted for display)
      const fullFilePath = getOriginalFilePath(file)
      const fileName = fullFilePath.split('/').pop() || fullFilePath
      const extension = fileName.split('.').pop()?.toLowerCase() || ''
      
      // List of binary file extensions
      const binaryExtensions = ['xls', 'xlsx', 'pdf', 'doc', 'docx', 'ppt', 'pptx', 'zip', 'rar', '7z', 'tar', 'gz', 'exe', 'dll', 'so', 'dylib', 'bin', 'qif']
      const isLikelyBinary = binaryExtensions.includes(extension)
      
      let blob: Blob
      let mimeType: string

      // For binary files, fetch directly as blob from workspace API with download parameter
      if (isLikelyBinary) {
        // Use workspaceApi (same as other workspace operations) with blob response type
        // IMPORTANT: responseType must be 'blob' to prevent axios from parsing JSON
        // Also set Accept header to request binary content
        const response = await workspaceApi.get(`/api/documents/${encodeURIComponent(fullFilePath)}`, {
          params: { download: 'true' },
          responseType: 'blob',
          headers: {
            'Accept': 'application/octet-stream'
          }
        })
        
        // Check if we got JSON instead of blob (backend might not have handled download param correctly)
        if (response.data instanceof Blob) {
          // Check if it's actually JSON by reading the first few bytes
          const firstBytes = await response.data.slice(0, 10).arrayBuffer()
          const firstBytesArray = new Uint8Array(firstBytes)
          const firstChar = String.fromCharCode(firstBytesArray[0])
          
          // JSON typically starts with '{' or '['
          if (firstChar === '{' || firstChar === '[') {
            // It's JSON, read it and check
            const text = await response.data.text()
            try {
              const jsonData = JSON.parse(text)
              if (jsonData.success !== undefined) {
                // It's the API JSON response, not the binary file
                if (jsonData.data?.content?.startsWith('[Binary file:')) {
                  throw new Error('Binary file download is not supported. The API only returns metadata for binary files. Please download the file directly from the file system.')
                }
                console.error('[Download] Server returned JSON instead of binary:', jsonData)
                throw new Error(`Server returned JSON instead of binary file. The download parameter may not be working correctly. Response: ${JSON.stringify(jsonData).substring(0, 200)}`)
              }
            } catch {
              // Not JSON, use as-is
            }
          }
          
          blob = response.data
          mimeType = blob.type || getMimeType(extension)
        } else {
          // Fallback: if we got something else, try to convert it
          mimeType = getMimeType(extension)
          blob = new Blob([response.data], { type: mimeType })
        }
      } else {
        // For text files and images, use the regular API
        const response = await agentApi.getPlannerFileContent(fullFilePath)
        
        if (!response.success || !response.data) {
          setError(response.message || 'Failed to download file')
          return
        }

        // Check if it's a binary file placeholder (even though extension suggests text)
        // In this case, retry using the binary download endpoint to get actual file content
        if (response.data.content?.startsWith('[Binary file:')) {
          // File is actually binary, use binary download path to get the actual file
          try {
            const binaryResponse = await workspaceApi.get(`/api/documents/${encodeURIComponent(fullFilePath)}`, {
              params: { download: 'true' },
              responseType: 'blob',
              headers: {
                'Accept': 'application/octet-stream'
              },
              transformResponse: [(data) => data] // Prevent axios from parsing response
            })
            
            // Verify we got actual binary data, not JSON
            if (binaryResponse.data instanceof Blob) {
              // Check if it's actually JSON by reading the first few bytes
              const firstBytes = await binaryResponse.data.slice(0, 10).arrayBuffer()
              const firstBytesArray = new Uint8Array(firstBytes)
              const firstChar = String.fromCharCode(firstBytesArray[0])
              
              // JSON typically starts with '{' or '['
              if (firstChar === '{' || firstChar === '[') {
                // It's JSON, read it and check
                const text = await binaryResponse.data.text()
                try {
                  const jsonData = JSON.parse(text)
                  if (jsonData.success !== undefined) {
                    // It's the API JSON response, not the binary file
                    console.error('[Download] Server returned JSON instead of binary:', jsonData)
                    throw new Error(`Server returned JSON instead of binary file. The download parameter may not be working correctly.`)
                  }
                } catch {
                  // Not JSON, use as-is
                }
              }
              
              blob = binaryResponse.data
              mimeType = blob.type || getMimeType(extension)
            } else {
              mimeType = getMimeType(extension)
              blob = new Blob([binaryResponse.data], { type: mimeType })
            }
          } catch (binaryErr) {
            console.error('[Download] Failed to download binary file:', binaryErr)
            throw new Error('This file is binary but could not be downloaded. Please download it directly from the file system.')
          }
          
          // Skip the rest of the text/image handling and download immediately
          const url = URL.createObjectURL(blob)
          const link = document.createElement('a')
          link.href = url
          link.download = fileName
          document.body.appendChild(link)
          link.click()
          document.body.removeChild(link)
          URL.revokeObjectURL(url)
          return
        }

        // Handle different file types
        if (response.data.is_image && response.data.content.startsWith('data:image/')) {
          // Image file - convert base64 data URL to blob
          const base64Data = response.data.content.split(',')[1]
          const imageType = response.data.content.match(/data:image\/([^;]+)/)?.[1] || 'png'
          const binaryString = atob(base64Data)
          const bytes = new Uint8Array(binaryString.length)
          for (let i = 0; i < binaryString.length; i++) {
            bytes[i] = binaryString.charCodeAt(i)
          }
          blob = new Blob([bytes], { type: `image/${imageType}` })
          mimeType = `image/${imageType}`
        } else {
          // Text file - create blob from content
          const content = response.data.content
          mimeType = getMimeType(extension)
          blob = new Blob([content], { type: mimeType })
        }
      }

      // Create download link and trigger download
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = fileName
      document.body.appendChild(link)
      link.click()
      document.body.removeChild(link)
      URL.revokeObjectURL(url)
    } catch (err) {
      console.error('Failed to download file:', err)
      setError(err instanceof Error ? err.message : 'Failed to download file')
    }
  }

  // Helper function to get MIME type from extension
  const getMimeType = (extension: string): string => {
    const mimeTypes: Record<string, string> = {
      'txt': 'text/plain',
      'json': 'application/json',
      'js': 'text/javascript',
      'ts': 'text/typescript',
      'tsx': 'text/typescript',
      'jsx': 'text/javascript',
      'html': 'text/html',
      'css': 'text/css',
      'md': 'text/markdown',
      'py': 'text/python',
      'go': 'text/plain',
      'yaml': 'text/yaml',
      'yml': 'text/yaml',
      'xml': 'text/xml',
      'csv': 'text/csv',
      'xls': 'application/vnd.ms-excel',
      'xlsx': 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
      'pdf': 'application/pdf',
      'doc': 'application/msword',
      'docx': 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
      'ppt': 'application/vnd.ms-powerpoint',
      'pptx': 'application/vnd.openxmlformats-officedocument.presentationml.presentation',
      'zip': 'application/zip',
      'rar': 'application/x-rar-compressed',
      '7z': 'application/x-7z-compressed',
      'tar': 'application/x-tar',
      'gz': 'application/gzip',
    }
    return mimeTypes[extension] || 'application/octet-stream'
  }

  // Confirm move
  const confirmMove = async (destinationPath: string, commitMessage?: string) => {
    if (!localMoveDialog.item) return

    setLocalMoveDialog(prev => ({ ...prev, isLoading: true }))

    try {
      // Use original filepath if available (when path was adjusted for display)
      const fullFilePath = getOriginalFilePath(localMoveDialog.item)
      
      // Reconstruct destination path using getFullFilePath to handle workflow mode correctly
      // This ensures paths like "HRMS PR Review/itemName" become "Workflow/HRMS PR Review/itemName"
      const fullDestinationPath = getFullFilePath(destinationPath)
      
      await agentApi.movePlannerFile(fullFilePath, fullDestinationPath, commitMessage)
      
      // Refresh the file list to show updated state
      await fetchFiles(activeFolder)
      
      // Update selected file if it was moved
      if (localMoveDialog.item.filepath === highlightedFile) {
        // Clear highlight since file moved
        setSelectedFile(null)
        setFileContent('')
        setShowFileContent(false)
      }
      
      // Close dialog
      closeLocalMoveDialog()
    } catch (err) {
      console.error('Failed to move item:', err)
      setError(err instanceof Error ? err.message : 'Failed to move item')
      setLocalMoveDialog(prev => ({ ...prev, isLoading: false }))
      throw err // Re-throw to let dialog handle the error
    }
  }

  // Cancel move
  const cancelMove = () => {
    closeLocalMoveDialog()
  }

  // Confirm rename
  const confirmRename = async (newName: string, commitMessage?: string) => {
    if (!renameDialog.item) return

    setRenameDialog(prev => ({ ...prev, isLoading: true }))

    try {
      // Use original filepath if available (when path was adjusted for display)
      const fullFilePath = getOriginalFilePath(renameDialog.item)
      
      // Calculate new path
      // Handle root level files correctly
      const pathParts = fullFilePath.split('/')
      const parentPath = pathParts.length > 1 ? pathParts.slice(0, -1).join('/') : ''
      const destinationPath = parentPath ? `${parentPath}/${newName}` : newName
      
      await agentApi.movePlannerFile(fullFilePath, destinationPath, commitMessage)
      
      // Refresh the file list to show updated state
      await fetchFiles(activeFolder)
      
      // Update selected file if it was renamed
      if (renameDialog.item.filepath === highlightedFile) {
        // Clear highlight since file renamed (or maybe we should update it to new name?)
        // For now, let's just clear selection to avoid errors
        setSelectedFile(null)
        setFileContent('')
        setShowFileContent(false)
      }
      
      // Close dialog
      closeRenameDialog()
    } catch (err) {
      console.error('Failed to rename item:', err)
      setError(err instanceof Error ? err.message : 'Failed to rename item')
      setRenameDialog(prev => ({ ...prev, isLoading: false }))
      throw err // Re-throw to let dialog handle the error
    }
  }

  // Handle file rename
  const handleFileRename = (file: PlannerFile) => {
    openRenameDialog(file)
  }

  // Handle folder rename
  const handleFolderRename = (folder: PlannerFile) => {
    openRenameDialog(folder)
  }

  // Handle file move
  const handleFileMove = (file: PlannerFile) => {
    openLocalMoveDialog(file)
  }

  // Handle folder move
  const handleFolderMove = (folder: PlannerFile) => {
    openLocalMoveDialog(folder)
  }

  // Upload functionality
  const handleUploadClick = () => {
    openUploadDialog('/')
  }

  // Upload to specific folder
  const handleFolderUploadClick = (folderPath: string) => {
    // For string paths, use legacy reconstruction (backward compatibility)
    const fullFolderPath = getFullFilePath(folderPath)
    openUploadDialog(fullFolderPath)
  }

  const blockedExtensions = useMemo(() => [
    'exe', 'dll', 'so', 'dylib', 'bin', 'msi', 'dmg', 'iso', 'jar', 'bat', 'cmd', 'com', 'scr'
  ], [])

  const validateFile = useCallback((file: File): string | null => {
    const fileExt = file.name.split('.').pop()?.toLowerCase() || ''
    if (blockedExtensions.includes(fileExt)) {
      return 'Blocked file type (executable/system file)'
    }
    if (file.size > 10 * 1024 * 1024) {
      return 'File exceeds 10MB limit'
    }
    return null
  }, [blockedExtensions])

  const addFiles = useCallback((files: FileList | File[]) => {
    const newFiles = Array.from(files)
    setPendingFiles(prev => {
      const existingNames = new Set(prev.map(f => f.name))
      const unique = newFiles.filter(f => !existingNames.has(f.name))
      return [...prev, ...unique]
    })
    setUploadResults(null)
  }, [])

  const handleFileSelect = useCallback((event: React.ChangeEvent<HTMLInputElement>) => {
    const files = event.target.files
    if (!files || files.length === 0) return
    addFiles(files)
    // Reset input so the same files can be re-selected
    event.target.value = ''
  }, [addFiles])

  const removeFile = useCallback((index: number) => {
    setPendingFiles(prev => prev.filter((_, i) => i !== index))
  }, [])

  const handleUploadAll = useCallback(async () => {
    if (pendingFiles.length === 0) return

    setUploadDialog({ isLoading: true })
    setError(null)
    setUploadResults(null)

    const rawFolderPath = uploadDialog.folderPath || '/'
    const folderPath = rawFolderPath === '/' ? '/' : getFullFilePath(rawFolderPath)
    const results: { name: string; success: boolean; error?: string }[] = []

    for (let i = 0; i < pendingFiles.length; i++) {
      const file = pendingFiles[i]
      setUploadProgress({ current: i + 1, total: pendingFiles.length })

      const validationError = validateFile(file)
      if (validationError) {
        results.push({ name: file.name, success: false, error: validationError })
        continue
      }

      try {
        const commitMessage = uploadDialog.commitMessage || `Upload ${file.name}`
        await agentApi.uploadPlannerFile(file, folderPath, commitMessage)
        results.push({ name: file.name, success: true })
      } catch (err) {
        const msg = err instanceof Error ? err.message : 'Upload failed'
        results.push({ name: file.name, success: false, error: msg })
      }
    }

    setUploadProgress(null)
    setUploadResults(results)

    const allSucceeded = results.every(r => r.success)
    if (allSucceeded) {
      await fetchFiles(activeFolder)
      setPendingFiles([])
      closeUploadDialog()
      setUploadResults(null)
    } else {
      await fetchFiles(activeFolder)
      // Keep dialog open to show results; remove succeeded files from pending
      const failedNames = new Set(results.filter(r => !r.success).map(r => r.name))
      setPendingFiles(prev => prev.filter(f => failedNames.has(f.name)))
      setUploadDialog({ isLoading: false })
    }
  }, [pendingFiles, uploadDialog.folderPath, uploadDialog.commitMessage, setUploadDialog, closeUploadDialog, fetchFiles, activeFolder, setError, getFullFilePath, validateFile])

  const cancelUpload = useCallback(() => {
    setPendingFiles([])
    setUploadResults(null)
    setUploadProgress(null)
    setIsDragOver(false)
    closeUploadDialog()
  }, [closeUploadDialog])

  const handleDragOver = useCallback((e: React.DragEvent) => {
    e.preventDefault()
    e.stopPropagation()
    setIsDragOver(true)
  }, [])

  const handleDragLeave = useCallback((e: React.DragEvent) => {
    e.preventDefault()
    e.stopPropagation()
    setIsDragOver(false)
  }, [])

  const handleDrop = useCallback((e: React.DragEvent) => {
    e.preventDefault()
    e.stopPropagation()
    setIsDragOver(false)
    if (e.dataTransfer.files && e.dataTransfer.files.length > 0) {
      addFiles(e.dataTransfer.files)
    }
  }, [addFiles])

  const formatFileSize = useCallback((bytes: number) => {
    if (bytes < 1024) return `${bytes} B`
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
  }, [])

  // Keyboard shortcuts for upload dialog
  useEffect(() => {
    if (!uploadDialog.isOpen) return

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        if (!uploadDialog.isLoading) {
          cancelUpload()
        }
      }
    }

    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [uploadDialog.isOpen, uploadDialog.isLoading, cancelUpload])

  // Folder creation handlers
  const handleCreateFolder = (parentFolder?: PlannerFile | string) => {
    // Use originalFilepath if parentFolder is a PlannerFile object, otherwise reconstruct from string
    const fullParentPath = parentFolder 
      ? (typeof parentFolder === 'string' 
          ? getFullFilePath(parentFolder) 
          : getOriginalFilePath(parentFolder))
      : undefined
    openCreateFolderDialog(fullParentPath)
  }

  const handleCreateFolderSubmit = async (folderPath: string, commitMessage?: string) => {
    try {
      // Reconstruct folder path using getFullFilePath to handle workflow mode correctly
      // This ensures paths are correct even if parentPath wasn't properly fixed
      const fullFolderPath = getFullFilePath(folderPath)
      await agentApi.createPlannerFolder(fullFolderPath, commitMessage)
      
      // Refresh file list to show the new folder
      await fetchFiles(activeFolder)
      
      // Close dialog
      closeCreateFolderDialog()
    } catch (err) {
      console.error('Failed to create folder:', err)
      throw err // Re-throw to let the dialog handle the error
    }
  }

  const cancelCreateFolder = () => {
    closeCreateFolderDialog()
  }

  // Export backup handler - accepts folder path (uses workflow folder if not provided)
  const handleExportBackup = async (folderPath?: string) => {
    const workspacePath = folderPath || activeWorkflowPreset?.selectedFolder?.filepath
    if (!workspacePath) {
      setError('No workspace folder selected')
      return
    }

    setIsExporting(true)
    setExportError(null)

    try {
      // If folderPath is provided from folder dropdown, it's already the original path
      // Otherwise, use the workflow preset's selected folder path
      // Only reconstruct if we're in workflow mode and the path might be adjusted
      const fullPath = folderPath && selectedModeCategory === 'workflow' && workflowFolderPath
        ? getOriginalFilePath(folderPath)
        : workspacePath
      const blob = await agentApi.exportWorkflowBackup(fullPath)
      
      // Create download link
      const url = window.URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      
      // Generate filename
      const workspaceName = fullPath.split('/').pop() || 'workspace'
      const timestamp = new Date().toISOString().replace(/[:.]/g, '-').slice(0, -5)
      link.download = `${workspaceName}-backup-${timestamp}.zip`
      
      document.body.appendChild(link)
      link.click()
      document.body.removeChild(link)
      window.URL.revokeObjectURL(url)
    } catch (error) {
      console.error('Export failed:', error)
      setExportError(error instanceof Error ? error.message : 'Failed to export backup')
      // Auto-dismiss error message after 5 seconds
      setTimeout(() => {
        setExportError(null)
      }, 5000)
    } finally {
      setIsExporting(false)
    }
  }

  // Import backup click handler
  const handleImportBackupClick = (folderPath?: string) => {
    // Store the folder path in a data attribute or state for use in handleImportBackup
    if (backupFileInputRef.current) {
      backupFileInputRef.current.setAttribute('data-folder-path', folderPath || '')
    }
    backupFileInputRef.current?.click()
  }

  // Import backup handler
  const handleImportBackup = async (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0]
    if (!file) return

    // Get folder path from input's data attribute
    const folderPath = backupFileInputRef.current?.getAttribute('data-folder-path') || ''
    const workspacePath = folderPath || activeWorkflowPreset?.selectedFolder?.filepath
    
    if (!workspacePath) {
      setError('No workspace folder selected')
      return
    }

    // Validate file type
    if (!file.name.endsWith('.zip')) {
      setImportError('Please select a ZIP file')
      setIsImporting(false)
      // Auto-dismiss error message after 5 seconds
      setTimeout(() => {
        setImportError(null)
      }, 5000)
      return
    }

    setIsImporting(true)
    setImportProgress(0)
    setImportingFileName(file.name)
    setImportError(null)
    setImportSuccess(null)

    try {
      // If folderPath is provided from folder dropdown, it's already the original path
      // Otherwise, use the workflow preset's selected folder path
      // Only reconstruct if we're in workflow mode and the path might be adjusted
      const fullPath = folderPath && selectedModeCategory === 'workflow' && workflowFolderPath
        ? getOriginalFilePath(folderPath)
        : workspacePath
      
      // Ask for confirmation
      const overwrite = window.confirm(
        'This will restore the workspace from the backup. Existing files may be overwritten. Continue?'
      )

      if (!overwrite) {
        setIsImporting(false)
        setImportingFileName('')
        if (backupFileInputRef.current) backupFileInputRef.current.value = ''
        return
      }

      const result = await agentApi.importWorkflowBackup(
        fullPath, 
        file, 
        true, // overwrite confirmed above
        (progress) => setImportProgress(progress)
      )
      
      if (result.success) {
        setImportSuccess(`Successfully imported ${result.data?.files_extracted || 0} files`)
        
        // Refresh workspace files
        setTimeout(() => {
          fetchFiles(activeFolder).catch(console.error)
        }, 500)
        
        // Auto-dismiss success message after 5 seconds
        setTimeout(() => {
          setImportSuccess(null)
        }, 5000)
      } else {
        setImportError(result.message || 'Import failed')
        // Auto-dismiss error message after 5 seconds
        setTimeout(() => {
          setImportError(null)
        }, 5000)
      }
    } catch (error) {
      console.error('Import failed:', error)
      setImportError(error instanceof Error ? error.message : 'Failed to import backup')
    } finally {
      setIsImporting(false)
      setImportProgress(0)
      setImportingFileName('')
      // Reset file input
      if (backupFileInputRef.current) {
        backupFileInputRef.current.value = ''
        backupFileInputRef.current.removeAttribute('data-folder-path')
      }
    }
  }

  return (
    <TooltipProvider>
      <div className="flex flex-col h-full bg-gray-50 dark:bg-gray-900">
      {/* Header */}
      <div className="px-4 py-2 border-b border-gray-200 dark:border-gray-700">
        {minimized ? (
          <div className="flex items-center justify-between">
            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation()
                    onToggleMinimize()
                  }}
                  className="flex items-center gap-2 px-2 py-1 text-gray-700 dark:text-gray-300 hover:text-gray-900 dark:hover:text-gray-100 hover:bg-gray-100 dark:hover:bg-slate-700 rounded transition-colors"
                  title="Expand workspace"
                >
                  <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
                  </svg>
                  <span className="text-sm font-medium">Workspace</span>
                </button>
              </TooltipTrigger>
              <TooltipContent>
                <p>Expand workspace (Ctrl+6)</p>
              </TooltipContent>
            </Tooltip>
          </div>
        ) : (
          <div className="flex items-center justify-between mb-3">
            <div className="flex items-center gap-3">
              <h2 className="text-base font-semibold text-gray-900 dark:text-gray-100">
                Workspace
              </h2>
              {/* Selection mode UI */}
              {isSelectionMode && (
                <div className="flex items-center gap-2">
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <label className="flex items-center cursor-pointer relative">
                        <input
                          type="checkbox"
                          checked={areAllFilesSelected}
                          onChange={toggleSelectAll}
                          className="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-700"
                        />
                        {selectedFiles.size > 0 && (
                          <span className="absolute -top-1 -right-1 bg-blue-500 text-white text-xs rounded-full w-5 h-5 flex items-center justify-center font-medium">
                            {selectedFiles.size}
                          </span>
                        )}
                      </label>
                    </TooltipTrigger>
                    <TooltipContent>
                      <p>Select All</p>
                    </TooltipContent>
                  </Tooltip>
                </div>
              )}
            </div>
            <div className="flex items-center gap-2">
              {/* Selection mode controls */}
              {isSelectionMode && (
                <>
                  {selectedFiles.size > 0 && (
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <button
                          onClick={handleBulkDelete}
                          disabled={loading || bulkDeleteDialog.isLoading}
                          className="p-2 text-red-600 hover:text-red-700 dark:text-red-400 dark:hover:text-red-300 disabled:opacity-50 relative"
                        >
                          <Trash2 className="w-4 h-4" />
                          <span className="absolute -top-1 -right-1 bg-red-500 text-white text-xs rounded-full w-5 h-5 flex items-center justify-center font-medium">
                            {selectedFiles.size}
                          </span>
                        </button>
                      </TooltipTrigger>
                      <TooltipContent>
                        <p>Delete {selectedFiles.size} selected file{selectedFiles.size !== 1 ? 's' : ''}</p>
                      </TooltipContent>
                    </Tooltip>
                  )}
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <button
                        onClick={toggleSelectionMode}
                        className="p-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
                      >
                        <X className="w-4 h-4" />
                      </button>
                    </TooltipTrigger>
                    <TooltipContent>
                      <p>Exit selection mode</p>
                    </TooltipContent>
                  </Tooltip>
                </>
              )}
              
              {/* Refresh button - always visible when not in selection mode */}
              {!isSelectionMode && (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <button
                      onClick={() => fetchFiles(activeFolder)}
                      disabled={loading}
                      className="p-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 disabled:opacity-50"
                    >
                      <svg className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
                      </svg>
                    </button>
                  </TooltipTrigger>
                  <TooltipContent>
                    <p>Refresh files</p>
                  </TooltipContent>
                </Tooltip>
              )}
              
              {/* Combined Actions Dropdown - Hidden in selection mode */}
              {!isSelectionMode && (
                <div className="relative actions-dropdown">
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <button
                        onClick={() => setShowActionsDropdown(!showActionsDropdown)}
                        disabled={loading}
                        className="p-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 disabled:opacity-50 flex items-center gap-1"
                      >
                        <Plus className="w-4 h-4" />
                        <ChevronDown className="w-3 h-3" />
                      </button>
                    </TooltipTrigger>
                    <TooltipContent>
                      <p>Add files or folders</p>
                    </TooltipContent>
                  </Tooltip>

                  {/* Dropdown Menu */}
                  {showActionsDropdown && (
                  <div className="absolute top-full right-0 mt-2 w-48 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-lg z-50">
                    <div className="py-1">
                      <button
                        onClick={() => {
                          handleUploadClick()
                          setShowActionsDropdown(false)
                        }}
                        className="w-full px-4 py-2 text-left text-sm text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 flex items-center gap-2"
                      >
                        <Upload className="w-4 h-4" />
                        Upload File
                      </button>
                      <button
                        onClick={() => {
                          handleCreateFolder()
                          setShowActionsDropdown(false)
                        }}
                        className="w-full px-4 py-2 text-left text-sm text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 flex items-center gap-2"
                      >
                        <FolderPlus className="w-4 h-4" />
                        Create Folder
                      </button>
                      <div className="border-t border-gray-200 dark:border-gray-700 my-1"></div>
                      <button
                        onClick={() => {
                          toggleSelectionMode()
                          setShowActionsDropdown(false)
                        }}
                        className="w-full px-4 py-2 text-left text-sm text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 flex items-center gap-2"
                      >
                        <CheckSquare className="w-4 h-4" />
                        Select Files
                      </button>
                      {isSemanticSearchEnabled && (
                      <button
                        onClick={() => {
                          setShowSearchSyncDetails(true)
                          setShowActionsDropdown(false)
                        }}
                        className="w-full px-4 py-2 text-left text-sm text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 flex items-center gap-2"
                      >
                        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
                        </svg>
                        Search Sync Details
                      </button>
                      )}
                    </div>
                  </div>
                  )}
                </div>
              )}

              {/* Git Sync Status - Hidden in selection mode */}
              {!isSelectionMode && isGitSyncEnabled && (
                <div className="relative">
                  <GitSyncStatus onSync={() => fetchFiles(activeFolder)} isVisible={!minimized} />
                </div>
              )}

              {/* Search Sync Status - Hidden button, controlled via dropdown - Hidden in selection mode */}
              {!isSelectionMode && isSemanticSearchEnabled && (
                <div className="relative">
                  <SemanticSearchSync
                    onResync={() => fetchFiles(activeFolder)}
                    isVisible={!minimized}
                    hideButton={true}
                    showDetailsExternal={showSearchSyncDetails}
                    onDetailsClose={() => setShowSearchSyncDetails(false)}
                  />
                </div>
              )}

              {/* Minimize button - Hidden in selection mode */}
              {!isSelectionMode && (
                <div className="flex items-center gap-1">
                  <span className="text-xs text-gray-400 dark:text-gray-500 font-mono">⌘5</span>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <button
                        onClick={onToggleMinimize}
                        className="p-1 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 transition-colors relative group"
                      >
                        <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
                        </svg>
                      </button>
                    </TooltipTrigger>
                    <TooltipContent>
                      <p>{minimized ? "Expand workspace" : "Minimize workspace"} (Ctrl+5)</p>
                    </TooltipContent>
                  </Tooltip>
                </div>
              )}
            </div>
          </div>
        )}
        
        {/* Workflow Filter Banner - Only show in workflow mode */}
        {!minimized && selectedModeCategory === 'workflow' && workflowFolderPath && (
          <div className="mb-2 px-2.5 py-1.5 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-md">
            <div className="flex items-center justify-between gap-2">
              <div className="flex items-center gap-1.5 flex-1 min-w-0">
                <Filter className="w-3.5 h-3.5 text-blue-600 dark:text-blue-400 flex-shrink-0" />
                <span className="text-xs text-blue-900 dark:text-blue-100 truncate">
                  Filters workspace as per workflow
                </span>
                {activeWorkflowPreset?.label && (
                  <span className="text-xs text-blue-700 dark:text-blue-300 flex-shrink-0">
                    ({activeWorkflowPreset.label})
                  </span>
                )}
              </div>
            </div>
          </div>
        )}

        {/* Search/Filter Input - Hidden when workspace is filtered (workflow) */}
        {!minimized && (selectedModeCategory !== 'workflow' || !workflowFolderPath) && (
          <div className="relative">
            <div className="absolute inset-y-0 left-0 pl-3 flex items-center pointer-events-none">
              <svg className="h-4 w-4 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
              </svg>
            </div>
            <input
              type="text"
              placeholder="Search files and folders..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="block w-full pl-10 pr-10 py-2 border border-gray-300 dark:border-gray-600 rounded-md leading-5 bg-white dark:bg-gray-800 placeholder-gray-500 dark:placeholder-gray-400 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-1 focus:ring-blue-500 focus:border-blue-500 text-sm"
            />
            {searchQuery && (
              <div className="absolute inset-y-0 right-0 pr-3 flex items-center">
                <Tooltip>
                  <TooltipTrigger asChild>
                    <button
                      onClick={() => setSearchQuery('')}
                      className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
                    >
                      <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                      </svg>
                    </button>
                  </TooltipTrigger>
                  <TooltipContent>
                    <p>Clear search</p>
                  </TooltipContent>
                </Tooltip>
              </div>
            )}
          </div>
        )}
        
      </div>

      {/* Content */}
      {!minimized && (
        <div className="flex-1 overflow-hidden">
          {/* PERF FIX: Stale workspace banner — replaces automatic fetchFiles() calls.
              Instead of fetching the full workspace tree (~2-3MB) on every step_progress_updated
              and completion event, we show this banner and let the user refresh manually.
              New files are still added incrementally via addFileToTree (no network). */}
          {needsRefresh && (
            <div className="flex items-center justify-between px-3 py-1.5 bg-yellow-500/10 border-b border-yellow-500/20 text-xs">
              <span className="text-yellow-600 dark:text-yellow-500">Files may be out of date</span>
              <button
                onClick={() => {
                  console.log('[Workspace] Manual refresh triggered by user')
                  setNeedsRefresh(false)
                  fetchFiles(activeFolder)
                }}
                disabled={loading}
                className="ml-2 px-2 py-0.5 rounded text-yellow-600 dark:text-yellow-500 hover:bg-yellow-500/20 font-medium disabled:opacity-50"
              >
                Refresh
              </button>
            </div>
          )}
          {/* Iteration filter badge — shown in workflow mode when iterations exist */}
          {selectedModeCategory === 'workflow' && availableIterations.length > 0 && (
            <div className="flex items-center gap-2 px-3 py-1.5 bg-blue-500/5 border-b border-blue-500/20 text-xs">
              <span className="text-blue-600 dark:text-blue-400 font-medium shrink-0">Filters workspace as per workflow</span>
              <div className="relative ml-auto iteration-filter-dropdown">
                <button
                  onClick={() => setShowIterationDropdown(prev => !prev)}
                  className="flex items-center gap-1 px-2 py-0.5 rounded bg-blue-500/10 hover:bg-blue-500/20 text-blue-700 dark:text-blue-300 font-medium"
                >
                  {effectiveDisplayedIteration ?? 'Select iteration'}
                  <ChevronDown className="w-3 h-3" />
                </button>
                {showIterationDropdown && (
                  <div className="absolute right-0 top-full mt-1 z-50 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded shadow-lg min-w-[140px] max-h-48 overflow-y-auto">
                    {availableIterations.map(iter => (
                      <button
                        key={iter}
                        onClick={() => {
                          setWorkspaceDisplayedIteration(iter)
                          setShowIterationDropdown(false)
                        }}
                        className={`w-full text-left px-3 py-1.5 text-xs hover:bg-gray-100 dark:hover:bg-gray-700 ${
                          iter === effectiveDisplayedIteration ? 'font-semibold text-blue-600 dark:text-blue-400' : 'text-gray-700 dark:text-gray-300'
                        }`}
                      >
                        {iter}
                      </button>
                    ))}
                  </div>
                )}
              </div>
            </div>
          )}

          {/* Folder Structure - Full Width */}
          <div ref={workspaceScrollRef} className="h-full overflow-y-auto">
            <div className="p-4">
              <PlannerFileList
                files={filteredFiles}
                loading={loading}
                error={error}
                onFolderClick={handleFolderClick}
                onFileClick={handleFileClick}
                onFileDelete={handleFileDelete}
                onFolderDelete={handleFolderDelete}
                onDeleteAllFilesInFolder={handleDeleteAllFilesInFolder}
                onRetry={() => fetchFiles(activeFolder)}
                expandedFolders={expandedFolders}
                loadingChildren={emptyLoadingSet}
                chatFileContext={chatFileContext}
                addFileToContext={addFileToContext}
                highlightedFile={highlightedFile}
                onFolderUpload={handleFolderUploadClick}
                onCreateFolder={handleCreateFolder}
                onFileMove={handleFileMove}
                onFolderMove={handleFolderMove}
                onFileRename={handleFileRename}
                onFolderRename={handleFolderRename}
                onFileDownload={handleFileDownload}
                hideAddToChat={selectedModeCategory === 'workflow' && !!workflowFolderPath}
                onExportBackup={handleExportBackup}
                onImportBackup={handleImportBackupClick}
                workflowFolderPath={workflowFolderPath}
                isExporting={isExporting}
                isImporting={isImporting}
                importProgress={importProgress}
                isSelectionMode={isSelectionMode}
                selectedFiles={selectedFiles}
                onToggleFileSelection={toggleFileSelection}
                onSelectFileAndEnterSelectionMode={selectFileAndEnterSelectionMode}
              />

              {/* Refresh from server when local search finds nothing */}
              {searchQuery.trim() && filteredFiles.length === 0 && (
                <div className="flex flex-col items-center gap-2 pt-2 pb-4">
                  <button
                    onClick={handleRefreshAndSearch}
                    disabled={serverSearchLoading}
                    className="flex items-center gap-1.5 px-3 py-1.5 text-xs text-blue-600 dark:text-blue-400 bg-blue-50 dark:bg-blue-900/30 hover:bg-blue-100 dark:hover:bg-blue-900/50 rounded-md transition-colors disabled:opacity-50"
                  >
                    {serverSearchLoading ? (
                      <>
                        <svg className="animate-spin h-3 w-3" viewBox="0 0 24 24"><circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" fill="none" /><path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" /></svg>
                        Refreshing files...
                      </>
                    ) : (
                      <>
                        <svg className="h-3 w-3" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" /></svg>
                        Refresh &amp; search
                      </>
                    )}
                  </button>
                </div>
              )}
            </div>
          </div>
        </div>
      )}

      {/* Minimized Icons */}
      {minimized && (
        <div className="flex-1 flex flex-col items-center py-4 space-y-4">
          {/* Files Icon - Click to expand workspace */}
          <Tooltip>
            <TooltipTrigger asChild>
          <button
                onClick={onToggleMinimize}
            className="p-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 transition-colors"
                title="Expand Workspace"
          >
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2H5a2 2 0 00-2-2z" />
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 5a2 2 0 012-2h4a2 2 0 012 2v2H8V5z" />
            </svg>
          </button>
            </TooltipTrigger>
            <TooltipContent>
              <p>Expand Workspace</p>
            </TooltipContent>
          </Tooltip>

          {/* Search Icon - Click to expand workspace */}
          <Tooltip>
            <TooltipTrigger asChild>
          <button
                onClick={onToggleMinimize}
            className="p-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 transition-colors"
                title="Expand Workspace"
          >
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
            </svg>
          </button>
            </TooltipTrigger>
            <TooltipContent>
              <p>Expand Workspace</p>
            </TooltipContent>
          </Tooltip>

          {/* Document Icon - Click to expand workspace */}
          <Tooltip>
            <TooltipTrigger asChild>
          <button
                onClick={onToggleMinimize}
            className="p-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 transition-colors"
                title="Expand Workspace"
          >
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
            </svg>
          </button>
            </TooltipTrigger>
            <TooltipContent>
              <p>Expand Workspace</p>
            </TooltipContent>
          </Tooltip>
        </div>
      )}

      {/* Delete Confirmation Dialog */}
      <ConfirmationDialog
        isOpen={deleteDialog.isOpen}
        onClose={cancelDelete}
        onConfirm={confirmDelete}
        title={deleteDialog.item?.type === 'folder' ? 'Delete Folder' : 'Delete File'}
        message={`Are you sure you want to delete "${deleteDialog.item ? deleteDialog.item.filepath.split('/').pop() : 'this item'}"? This action cannot be undone.${
          deleteDialog.item?.type === 'folder' ? ' This will delete all files and subfolders inside.' : ''
        }`}
        confirmText="Delete"
        cancelText="Cancel"
        type="danger"
        isLoading={deleteDialog.isLoading}
      />

      {/* Delete All Files Confirmation Dialog */}
      <ConfirmationDialog
        isOpen={deleteAllFilesDialog.isOpen}
        onClose={cancelDeleteAllFiles}
        onConfirm={confirmDeleteAllFiles}
        title="Delete All Contents"
        message={`Are you sure you want to delete ALL CONTENTS (files and folders) in "${deleteAllFilesDialog.folder ? deleteAllFilesDialog.folder.filepath.split('/').pop() : 'this folder'}"? This action cannot be undone. The folder itself will remain, but all files and subfolders inside will be permanently deleted.`}
        confirmText="Delete All Contents"
        cancelText="Cancel"
        type="warning"
        isLoading={deleteAllFilesDialog.isLoading}
      />

      {/* Bulk Delete Confirmation Dialog */}
      <ConfirmationDialog
        isOpen={bulkDeleteDialog.isOpen}
        onClose={cancelBulkDelete}
        onConfirm={confirmBulkDelete}
        title={`Delete ${bulkDeleteDialog.items.length} Item${bulkDeleteDialog.items.length !== 1 ? 's' : ''}`}
        message={`Are you sure you want to delete ${bulkDeleteDialog.items.length} item${bulkDeleteDialog.items.length !== 1 ? 's' : ''}? This action cannot be undone.\n\n${bulkDeleteDialog.items.slice(0, 10).map(item => {
          return `• ${item.filepath}${item.type === 'folder' ? ' (folder)' : ''}`
        }).join('\n')}${bulkDeleteDialog.items.length > 10 ? `\n... and ${bulkDeleteDialog.items.length - 10} more` : ''}`}
        confirmText={`Delete ${bulkDeleteDialog.items.length} Item${bulkDeleteDialog.items.length !== 1 ? 's' : ''}`}
        cancelText="Cancel"
        type="danger"
        isLoading={bulkDeleteDialog.isLoading}
      />

      {/* Upload Dialog */}
      {uploadDialog.isOpen && (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
          <div className="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-md mx-4 max-h-[80vh] flex flex-col">
            <h3 className="text-lg font-semibold text-gray-900 dark:text-gray-100 mb-4">
              Upload Files
            </h3>

            <div className="space-y-4 overflow-y-auto flex-1 min-h-0">
              {/* Upload Destination Display */}
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                  Upload Destination
                </label>
                <div className="px-3 py-2 bg-gray-50 dark:bg-gray-700 border border-gray-300 dark:border-gray-600 rounded-md">
                  <p className="text-sm text-gray-900 dark:text-gray-100">
                    {uploadDialog.folderPath === '/' ? 'Root directory (/)' : uploadDialog.folderPath}
                  </p>
                </div>
              </div>

              {/* Commit Message Input */}
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                  Commit Message (Optional)
                </label>
                <input
                  type="text"
                  value={uploadDialog.commitMessage}
                  onChange={(e) => setUploadDialog({ commitMessage: e.target.value })}
                  placeholder="Upload description"
                  disabled={uploadDialog.isLoading}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-1 focus:ring-blue-500 focus:border-blue-500 disabled:opacity-50"
                />
              </div>

              {/* Drag & Drop Zone */}
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                  Select Files
                </label>
                <div
                  onDragOver={handleDragOver}
                  onDragLeave={handleDragLeave}
                  onDrop={handleDrop}
                  onClick={() => !uploadDialog.isLoading && fileInputRef.current?.click()}
                  className={`border-2 border-dashed rounded-md p-6 text-center cursor-pointer transition-colors ${
                    isDragOver
                      ? 'border-blue-500 bg-blue-50 dark:bg-blue-900/20'
                      : 'border-gray-300 dark:border-gray-600 hover:border-gray-400 dark:hover:border-gray-500'
                  } ${uploadDialog.isLoading ? 'opacity-50 pointer-events-none' : ''}`}
                >
                  <Upload className="mx-auto h-8 w-8 text-gray-400 dark:text-gray-500 mb-2" />
                  <p className="text-sm text-gray-600 dark:text-gray-400">
                    Drag files here or click to browse
                  </p>
                  <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                    Text-based files only, 10MB max each
                  </p>
                </div>
                <input
                  ref={fileInputRef}
                  type="file"
                  multiple
                  onChange={handleFileSelect}
                  accept=".txt,.md,.json,.csv,.yaml,.yml,.xml,.html,.css,.js,.py,.go,.java,.c,.cpp,.cs,.php,.rb,.sql,.ts,.vue,.svelte"
                  className="hidden"
                />
              </div>

              {/* Selected Files List */}
              {pendingFiles.length > 0 && (
                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                    {pendingFiles.length} file{pendingFiles.length !== 1 ? 's' : ''} selected
                  </label>
                  <div className="border border-gray-200 dark:border-gray-600 rounded-md divide-y divide-gray-200 dark:divide-gray-600 max-h-40 overflow-y-auto">
                    {pendingFiles.map((file, index) => {
                      const validationError = validateFile(file)
                      return (
                        <div key={`${file.name}-${index}`} className="flex items-center justify-between px-3 py-2 text-sm">
                          <div className="flex-1 min-w-0 mr-2">
                            <p className={`truncate ${validationError ? 'text-red-600 dark:text-red-400' : 'text-gray-900 dark:text-gray-100'}`}>
                              {file.name}
                            </p>
                            {validationError ? (
                              <p className="text-xs text-red-500 dark:text-red-400">{validationError}</p>
                            ) : (
                              <p className="text-xs text-gray-500 dark:text-gray-400">{formatFileSize(file.size)}</p>
                            )}
                          </div>
                          {!uploadDialog.isLoading && (
                            <button
                              type="button"
                              onClick={() => removeFile(index)}
                              className="flex-shrink-0 p-1 text-gray-400 hover:text-red-500 dark:hover:text-red-400"
                            >
                              <X className="h-4 w-4" />
                            </button>
                          )}
                        </div>
                      )
                    })}
                  </div>
                </div>
              )}

              {/* Upload Progress */}
              {uploadProgress && (
                <div className="text-sm text-blue-600 dark:text-blue-400">
                  Uploading file {uploadProgress.current} of {uploadProgress.total}...
                </div>
              )}

              {/* Upload Results */}
              {uploadResults && (
                <div className="border border-gray-200 dark:border-gray-600 rounded-md divide-y divide-gray-200 dark:divide-gray-600 max-h-32 overflow-y-auto">
                  {uploadResults.map((result, index) => (
                    <div key={index} className="flex items-center px-3 py-1.5 text-sm">
                      <span className={`mr-2 ${result.success ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400'}`}>
                        {result.success ? '\u2713' : '\u2717'}
                      </span>
                      <span className="truncate text-gray-900 dark:text-gray-100">{result.name}</span>
                      {result.error && (
                        <span className="ml-2 text-xs text-red-500 dark:text-red-400 truncate">{result.error}</span>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </div>

            {/* Action Buttons */}
            <div className="flex justify-end gap-2 mt-6">
              <button
                type="button"
                onClick={cancelUpload}
                disabled={uploadDialog.isLoading}
                className="px-4 py-2 text-sm text-gray-600 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200 disabled:opacity-50"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={handleUploadAll}
                disabled={uploadDialog.isLoading || pendingFiles.length === 0}
                className="px-4 py-2 text-sm bg-blue-600 text-white rounded-md hover:bg-blue-700 disabled:opacity-50"
              >
                {uploadDialog.isLoading
                  ? uploadProgress
                    ? `Uploading ${uploadProgress.current}/${uploadProgress.total}...`
                    : 'Uploading...'
                  : `Upload${pendingFiles.length > 0 ? ` (${pendingFiles.length})` : ''}`}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Create Folder Dialog */}
      <CreateFolderDialog
        isOpen={createFolderDialog.isOpen}
        onClose={cancelCreateFolder}
        onCreateFolder={handleCreateFolderSubmit}
        parentPath={createFolderDialog.parentPath}
      />

      {/* Move File/Folder Dialog */}
      <MoveFileDialog
        isOpen={localMoveDialog.isOpen}
        onClose={cancelMove}
        onMove={confirmMove}
        item={localMoveDialog.item}
        destinationPath={localMoveDialog.destinationPath}
        setDestinationPath={(path) => setLocalMoveDialog(prev => ({ ...prev, destinationPath: path }))}
        commitMessage={localMoveDialog.commitMessage}
        setCommitMessage={(message) => setLocalMoveDialog(prev => ({ ...prev, commitMessage: message }))}
        isLoading={localMoveDialog.isLoading}
      />

      {/* Rename File/Folder Dialog */}
      <RenameFileDialog
        isOpen={renameDialog.isOpen}
        onClose={closeRenameDialog}
        onRename={confirmRename}
        item={renameDialog.item}
        isLoading={renameDialog.isLoading}
      />

      {/* Hidden file input for backup import */}
      <input
        ref={backupFileInputRef}
        type="file"
        accept=".zip"
        onChange={handleImportBackup}
        className="hidden"
      />

      {/* Error/Success Messages for Backup Operations */}
      {exportError && (
        <div className="fixed bottom-4 right-4 bg-red-500 text-white px-4 py-3 rounded-lg shadow-lg z-50 max-w-md">
          <div className="flex items-start justify-between gap-3">
            <div>
              <p className="font-medium">Export Failed</p>
              <p className="text-sm text-red-100">{exportError}</p>
            </div>
            <button
              onClick={() => setExportError(null)}
              className="text-white hover:text-red-100 flex-shrink-0"
            >
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          </div>
        </div>
      )}

      {importError && (
        <div className="fixed bottom-4 right-4 bg-red-500 text-white px-4 py-3 rounded-lg shadow-lg z-50 max-w-md">
          <div className="flex items-start justify-between gap-3">
            <div>
              <p className="font-medium">Import Failed</p>
              <p className="text-sm text-red-100">{importError}</p>
            </div>
            <button
              onClick={() => setImportError(null)}
              className="text-white hover:text-red-100 flex-shrink-0"
            >
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          </div>
        </div>
      )}

      {importSuccess && (
        <div className="fixed bottom-4 right-4 bg-green-500 text-white px-4 py-3 rounded-lg shadow-lg z-50 max-w-md">
          <div className="flex items-start justify-between gap-3">
            <div>
              <p className="font-medium">Import Successful</p>
              <p className="text-sm text-green-100">{importSuccess}</p>
            </div>
            <button
              onClick={() => setImportSuccess(null)}
              className="text-white hover:text-green-100 flex-shrink-0"
            >
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          </div>
        </div>
      )}

      <ImportProgressDialog 
        isOpen={isImporting} 
        progress={importProgress} 
        fileName={importingFileName} 
      />
      </div>
    </TooltipProvider>
  )
}