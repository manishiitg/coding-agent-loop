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
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './ui/tooltip'
import { useWorkspaceStore } from '../stores/useWorkspaceStore'
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
  normalizePath,
  adjustFilePathsRecursive,
  findFileByPath
} from '../utils/workspacePathUtils'
import { isTextBasedFile } from '../utils/fileUtils'
import { useIterationExpansion } from './workspace/useIterationExpansion'

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

  // Search Sync Details state
  const [showSearchSyncDetails, setShowSearchSyncDetails] = useState(false)

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
    fetchFiles
  } = useWorkspaceStore()

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
  
  // Filter files to show only the workflow folder when a workflow preset is selected
  // This function collects all files/folders that are within the workflow folder path
  const filterToWorkflowFolder = useCallback((files: PlannerFile[], parentFolderPath: string): PlannerFile[] => {
    // Normalize paths for comparison (remove leading/trailing slashes, lowercase)
    const targetPath = normalizePath(parentFolderPath)
    
    // Recursively collect all files within the workflow folder
    const collectWorkflowFiles = (fileList: PlannerFile[]): PlannerFile[] => {
      const result: PlannerFile[] = []
      
      for (const file of fileList) {
        const filePath = normalizePath(file.filepath)
        
        // Check if this is the workflow folder itself
        if (filePath === targetPath) {
          // Found the workflow folder - include it with all its children
          result.push({
            ...file,
            children: file.children ? collectWorkflowFiles(file.children) : []
          })
        }
        // Check if this file/folder is within the workflow folder
        else if (filePath.startsWith(targetPath + '/')) {
          // This is a child of the workflow folder
          if (file.type === 'folder') {
            result.push({
              ...file,
              children: file.children ? collectWorkflowFiles(file.children) : []
            })
          } else {
            result.push(file)
          }
        }
        // If this is a folder, search its children for workflow folder
        else if (file.type === 'folder' && file.children) {
          const found = collectWorkflowFiles(file.children)
          if (found.length > 0) {
            // Found workflow folder in children - include parent folder with filtered children
            result.push({
              ...file,
              children: found
            })
          }
        }
      }
      
      return result
    }
    
    return collectWorkflowFiles(files)
  }, [])
  
  // Helper function to apply filtering and path adjustment to files
  // This matches the logic in filteredFiles useMemo to ensure paths are consistent
  const applyFilteringAndPathAdjustment = useCallback((filesToProcess: PlannerFile[]): PlannerFile[] => {
    let result = filesToProcess
    
    // Only filter if we're in workflow mode and have a workflow folder path
    // When in chat mode, show all files regardless of preset
    if (selectedModeCategory === 'workflow' && workflowFolderPath) {
      result = filterToWorkflowFolder(filesToProcess, workflowFolderPath)
      
      // Adjust filepaths to show workflow folder as root (remove the workflow folder path prefix)
      // Store original path in originalFilepath for API calls
      result = adjustFilePathsRecursive(result, workflowFolderPath)
    }
    
    return result
  }, [selectedModeCategory, workflowFolderPath, filterToWorkflowFolder])
  
  // Restore expanded folders when files change
  // This runs after store's fetchFiles completes and handles workflow mode filtering
  useEffect(() => {
    // Skip if no files loaded yet
    if (files.length === 0) return
    
    // In workflow mode, completely skip restore effect to let auto-expand handle it
    // This prevents any interference with the auto-expansion logic
    if (selectedModeCategory === 'workflow' && workflowFolderPath) {
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

  // Simple filter: show all folders, filter only files
  const filterFiles = (files: PlannerFile[], query: string): PlannerFile[] => {
    if (!query.trim()) return files
    
    const lowercaseQuery = query.toLowerCase()
    
    const filterRecursive = (fileList: PlannerFile[]): PlannerFile[] => {
      return fileList.map(file => {
        if (file.type === 'folder') {
          // For folders: always show them, filter their children
          return {
            ...file,
            children: file.children ? filterRecursive(file.children) : []
          }
        } else {
          // For files: only show if they match
          const fileName = file.filepath.split('/').pop() || file.filepath
          const matches = fileName.toLowerCase().includes(lowercaseQuery) || 
                        file.filepath.toLowerCase().includes(lowercaseQuery)
          return matches ? file : null
        }
      }).filter(Boolean) as PlannerFile[]
    }
    
    return filterRecursive(files)
  }
  
  // Get filtered files - first filter to workflow folder if preset is active, then apply search
  const filteredFiles = useMemo(() => {
    let result = files
    
    // Only filter if we're in workflow mode and have a workflow folder path
    // When in chat mode, show all files regardless of preset
    if (selectedModeCategory === 'workflow' && workflowFolderPath) {
      result = filterToWorkflowFolder(files, workflowFolderPath)
      
      // Debug logging (only log warnings, not every filter operation)
      if (result.length === 0 && files.length > 0) {
        const presetLabel = activeWorkflowPreset?.label
        const presetFolderPath = activeWorkflowPreset?.selectedFolder?.filepath
        console.warn('[WORKSPACE] No files found after filtering!', {
          workflowFolder: workflowFolderPath,
          preset: presetLabel,
          selectedFolder: presetFolderPath,
          totalFiles: files.length,
          samplePaths: files.slice(0, 3).map(f => f.filepath)
        })
      }
      
      // Adjust filepaths to show workflow folder as root (remove the workflow folder path prefix)
      // Store original path in originalFilepath for API calls
      result = adjustFilePathsRecursive(result, workflowFolderPath)
    }
    
    // Apply search filter
    result = filterFiles(result, searchQuery)
    
    return result
  }, [files, workflowFolderPath, filterToWorkflowFolder, searchQuery, activeWorkflowPreset, selectedModeCategory])
  
  // Enhanced file highlighting with folder expansion and auto-scroll
  useEffect(() => {
    if (highlightedFile) {
      // In workflow mode, we need to convert the original path to the adjusted path for folder expansion
      // Find the file in filtered files that matches the highlightedFile (by originalFilepath or filepath)
      let pathToUse = highlightedFile
      if (selectedModeCategory === 'workflow' && workflowFolderPath) {
        const foundFile = findFileByPath(filteredFiles, highlightedFile)
        // Use the adjusted filepath if found, otherwise use the original path
        if (foundFile) {
          pathToUse = foundFile.filepath
        }
      }
      
      expandFoldersForFile(pathToUse)
      
      // Auto-scroll to highlighted file after a short delay to allow folder expansion
      setTimeout(() => {
        scrollToHighlightedFile(highlightedFile)
      }, 100)
    }
  }, [highlightedFile, expandFoldersForFile, scrollToHighlightedFile, selectedModeCategory, workflowFolderPath, filteredFiles])
  
  // Automatically expand workspace folder when a workflow is first opened
  // Only runs once per workflow preset to allow manual open/close afterward
  useEffect(() => {
    if (selectedModeCategory === 'workflow' && workflowFolderPath && filteredFiles.length > 0) {
      // Check if we've already auto-expanded for this workflow preset
      const workflowPresetId = activeWorkflowPreset?.id || workflowFolderPath
      
      // Only auto-expand if we haven't done it for this workflow yet
      if (autoExpandedWorkflowRef.current !== workflowPresetId) {
        // Small delay to ensure files are fully loaded and rendered
        const timeoutId = setTimeout(() => {
          // Use the store's expandFoldersToLevel function which properly handles the expansion
          // Expand up to 4 levels deep (level 0, 1, 2, 3, 4) - that's 4 more levels after workflow folder
          console.log('[Workspace] Auto-expanding workflow folders using expandFoldersToLevel:', {
            filteredFilesLength: filteredFiles.length,
            firstItem: filteredFiles[0],
            workflowFolderPath
          })
          
          // Determine what files to expand:
          // - If filteredFiles contains the workflow folder itself as root, expand it first, then its children
          // - Otherwise, use filteredFiles directly (which are already the children)
          const workflowFolder = filteredFiles.length > 0 && filteredFiles[0].type === 'folder' ? filteredFiles[0] : null
          const filesToExpand = workflowFolder && workflowFolder.children
            ? workflowFolder.children 
            : filteredFiles
          
          console.log('[Workspace] Files to expand:', {
            workflowFolderPath: workflowFolder?.filepath,
            workflowFolderHasChildren: !!workflowFolder?.children,
            childrenCount: filesToExpand.length,
            firstFew: filesToExpand.slice(0, 3).map(f => ({ path: f.filepath, type: f.type, hasChildren: !!f.children }))
          })
          
          // Expand children up to 4 levels deep (levels 0, 1, 2, 3, 4 = 5 levels total)
          // We use maxLevel=4 to get 4 levels below the workflow folder
          // Pass the workflow folder path as an additional folder to ensure it's expanded
          // Exclude "planning", "variables", "learnings", and "logs" folders to keep them closed by default
          // Note: "logs" exclusion applies to nested paths like "runs/iteration-x/group-x/logs"
          const additionalFolders = workflowFolder ? [workflowFolder.filepath] : undefined
          const excludeFolders = ['planning', 'variables', 'learnings', 'logs']
          expandFoldersToLevel(filesToExpand, 4, additionalFolders, excludeFolders)
          
          if (workflowFolder) {
            console.log('[Workspace] Expanded workflow folder and children:', {
              workflowFolderPath: workflowFolder.filepath,
              childrenCount: filesToExpand.length
            })
          }
          
          // Mark this workflow as auto-expanded
          autoExpandedWorkflowRef.current = workflowPresetId
          
          // Log the result after a short delay to see what was expanded
          setTimeout(() => {
            console.log('[Workspace] After expansion, expandedFolders count:', expandedFolders.size)
          }, 100)
        }, 500) // Increased delay to ensure files are fully processed
        
        return () => clearTimeout(timeoutId)
      }
    } else if (selectedModeCategory !== 'workflow') {
      // Reset the auto-expanded ref when switching away from workflow mode
      autoExpandedWorkflowRef.current = null
    }
  }, [selectedModeCategory, workflowFolderPath, filteredFiles, expandFoldersToLevel, activeWorkflowPreset?.id, expandedFolders, setExpandedFolders])
  
  // Auto-collapse other iterations when an iteration is selected
  // Get selected iteration from workflow store
  const selectedRunFolder = useWorkflowStore(state => state.selectedRunFolder)
  
  // Use custom hook to handle iteration expansion logic
  useIterationExpansion({
    selectedModeCategory,
    workflowFolderPath,
    filteredFiles,
    selectedRunFolder,
    expandedFolders,
    setExpandedFolders
  })
  
  // Close dropdown when clicking outside
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (showActionsDropdown) {
        const target = event.target as Element
        if (!target.closest('.actions-dropdown')) {
          setShowActionsDropdown(false)
        }
      }
    }

    document.addEventListener('mousedown', handleClickOutside)
    return () => {
      document.removeEventListener('mousedown', handleClickOutside)
    }
  }, [showActionsDropdown, setShowActionsDropdown])

  // Load files on component mount
  useEffect(() => {
    fetchFiles()
  }, [fetchFiles])

  // Handle file click - fetch content and show in chat area
  const handleFileClick = async (file: PlannerFile) => {
    if (file.type === 'file' || !file.type) {
      // Reconstruct the original full path if we're in workflow mode with filtered files
      const fullFilePath = getOriginalFilePath(file)
      const fileName = fullFilePath.split('/').pop() || fullFilePath

      // Check if file is viewable
      if (!isTextBasedFile(fileName) && !file.is_image) {
        setError(`File "${fileName}" is a binary file and cannot be viewed in the editor.`)
        return
      }

      try {
        setLoadingFileContent(true)
        
        setSelectedFile({ name: fileName, path: fullFilePath })
        
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
  const selectFileAndEnterSelectionMode = useCallback((file: PlannerFile) => {
    setIsSelectionMode(true)
    setSelectedFiles(new Set([file.filepath]))
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
    if (selectedItems.length === 0) return
    setBulkDeleteDialog({
      isOpen: true,
      isLoading: false,
      items: selectedItems
    })
  }, [getSelectedFilesAsObjects])

  // Confirm bulk delete
  const confirmBulkDelete = async () => {
    if (bulkDeleteDialog.items.length === 0) return

    setBulkDeleteDialog(prev => ({ ...prev, isLoading: true }))

    try {
      const errors: string[] = []
      
      // Delete each item sequentially
      for (const item of bulkDeleteDialog.items) {
        try {
          const fullFilePath = getOriginalFilePath(item)
          if (item.type === 'file') {
            await agentApi.deletePlannerFile(fullFilePath)
          } else {
            await agentApi.deletePlannerFolder(fullFilePath)
          }
        } catch (err) {
          const fileName = item.filepath.split('/').pop() || item.filepath
          errors.push(`${fileName}: ${err instanceof Error ? err.message : 'Failed to delete'}`)
        }
      }

      // Refresh the file list
      await fetchFiles()

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
      await fetchFiles()
      
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
      await fetchFiles()
      
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
      await fetchFiles()
      
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
      await fetchFiles()
      
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

  const handleFileSelect = useCallback(async (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0]
    if (!file) return

    // Validate file type (block executables and system files)
    const blockedExtensions = [
      'exe', 'dll', 'so', 'dylib', 'bin', 'msi', 'dmg', 'iso', 'jar', 'bat', 'cmd', 'com', 'scr'
    ]
    
    const fileExt = file.name.split('.').pop()?.toLowerCase() || ''
    if (blockedExtensions.includes(fileExt)) {
      setError('Executables and system files are not allowed for security reasons.')
      return
    }

    // Validate file size (10MB limit)
    if (file.size > 10 * 1024 * 1024) {
      setError('File size must be less than 10MB')
      return
    }

    try {
      setUploadDialog({ isLoading: true })
      setError(null)

      // Reconstruct folder path using getFullFilePath to handle workflow mode correctly
      const rawFolderPath = uploadDialog.folderPath || '/'
      const folderPath = rawFolderPath === '/' ? '/' : getFullFilePath(rawFolderPath)
      const commitMessage = uploadDialog.commitMessage || `Upload ${file.name}`

      await agentApi.uploadPlannerFile(file, folderPath, commitMessage)
      
      // Refresh file list
      await fetchFiles()
      
      // Close dialog
      closeUploadDialog()
    } catch (err) {
      console.error('Failed to upload file:', err)
      setError(err instanceof Error ? err.message : 'Failed to upload file')
      setUploadDialog({ isLoading: false })
    }
  }, [uploadDialog.folderPath, uploadDialog.commitMessage, setUploadDialog, closeUploadDialog, fetchFiles, setError, getFullFilePath])

  const cancelUpload = useCallback(() => {
    closeUploadDialog()
  }, [closeUploadDialog])

  // Keyboard shortcuts for upload dialog
  useEffect(() => {
    if (!uploadDialog.isOpen) return

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        if (!uploadDialog.isLoading) {
          cancelUpload()
        }
      } else if (event.key === 'Enter') {
        event.preventDefault()
        if (!uploadDialog.isLoading) {
          // Trigger file input click or submit if file is selected
          const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement
          if (fileInput && fileInput.files && fileInput.files.length > 0) {
            handleFileSelect({ target: fileInput } as React.ChangeEvent<HTMLInputElement>)
          }
        }
      }
    }

    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [uploadDialog.isOpen, uploadDialog.isLoading, cancelUpload, handleFileSelect])

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
      await fetchFiles()
      
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

      const result = await agentApi.importWorkflowBackup(fullPath, file, overwrite)
      
      if (result.success) {
        setImportSuccess(`Successfully imported ${result.data?.files_extracted || 0} files`)
        
        // Refresh workspace files
        setTimeout(() => {
          fetchFiles().catch(console.error)
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
                      onClick={fetchFiles}
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
                    </div>
                  </div>
                  )}
                </div>
              )}

              {/* Git Sync Status - Hidden in selection mode */}
              {!isSelectionMode && (
                <div className="relative">
                  <GitSyncStatus onSync={fetchFiles} isVisible={!minimized} />
                </div>
              )}

              {/* Search Sync Status - Hidden button, controlled via dropdown - Hidden in selection mode */}
              {!isSelectionMode && (
                <div className="relative">
                  <SemanticSearchSync 
                    onResync={fetchFiles} 
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
        
        {/* Search/Filter Input - Hidden when workflow folder is filtered in workflow mode */}
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
                onRetry={fetchFiles}
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
                onExportBackup={selectedModeCategory === 'workflow' && activeWorkflowPreset?.selectedFolder?.filepath ? handleExportBackup : undefined}
                onImportBackup={selectedModeCategory === 'workflow' && activeWorkflowPreset?.selectedFolder?.filepath ? handleImportBackupClick : undefined}
                workflowFolderPath={workflowFolderPath}
                isExporting={isExporting}
                isImporting={isImporting}
                isSelectionMode={isSelectionMode}
                selectedFiles={selectedFiles}
                onToggleFileSelection={toggleFileSelection}
                onSelectFileAndEnterSelectionMode={selectFileAndEnterSelectionMode}
              />
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
          const fileName = item.filepath.split('/').pop() || item.filepath
          return `• ${fileName}${item.type === 'folder' ? ' (folder)' : ''}`
        }).join('\n')}${bulkDeleteDialog.items.length > 10 ? `\n... and ${bulkDeleteDialog.items.length - 10} more` : ''}`}
        confirmText={`Delete ${bulkDeleteDialog.items.length} Item${bulkDeleteDialog.items.length !== 1 ? 's' : ''}`}
        cancelText="Cancel"
        type="danger"
        isLoading={bulkDeleteDialog.isLoading}
      />

      {/* Upload Dialog */}
      {uploadDialog.isOpen && (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
          <form onSubmit={(e) => {
            e.preventDefault()
            const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement
            if (fileInput && fileInput.files && fileInput.files.length > 0) {
              handleFileSelect({ target: fileInput } as React.ChangeEvent<HTMLInputElement>)
            }
          }} className="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-md mx-4">
            <h3 className="text-lg font-semibold text-gray-900 dark:text-gray-100 mb-4">
              Upload File
            </h3>
            
            <div className="space-y-4">
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
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-1 focus:ring-blue-500 focus:border-blue-500"
                />
              </div>

              {/* File Input */}
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                  Select File
                </label>
                <input
                  type="file"
                  onChange={handleFileSelect}
                  accept=".txt,.md,.json,.csv,.yaml,.yml,.xml,.html,.css,.js,.py,.go,.java,.c,.cpp,.cs,.php,.rb,.sql,.ts,.vue,.svelte"
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-1 focus:ring-blue-500 focus:border-blue-500"
                />
                <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                  Only text-based files allowed (10MB max)
                </p>
              </div>
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
                type="submit"
                disabled={uploadDialog.isLoading}
                className="px-4 py-2 text-sm bg-blue-600 text-white rounded-md hover:bg-blue-700 disabled:opacity-50"
              >
                {uploadDialog.isLoading ? 'Uploading...' : 'Upload'}
              </button>
            </div>
          </form>
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
      </div>
    </TooltipProvider>
  )
}