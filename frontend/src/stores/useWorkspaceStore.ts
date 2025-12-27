import { create } from 'zustand'
import { devtools } from 'zustand/middleware'
import type { PlannerFile, PollingEvent } from '../services/api-types'
import { agentApi } from '../services/api'
import { findFileInTree, extractFolderPaths, processHierarchicalFiles } from '../utils/fileUtils'
import { getTypedEventData } from '../generated/event-types'
import type { WorkspaceFileOperationEvent } from '../generated/events-bridge'

interface WorkspaceState {
  // File Management
  files: PlannerFile[]
  setFiles: (files: PlannerFile[]) => void
  loading: boolean
  setLoading: (loading: boolean) => void
  error: string | null
  setError: (error: string | null) => void
  
  // Search/Filter
  searchQuery: string
  setSearchQuery: (query: string) => void
  
  // Selected file and content
  selectedFile: {name: string, path: string} | null
  setSelectedFile: (file: {name: string, path: string} | null) => void
  fileContent: string
  setFileContent: (content: string) => void
  loadingFileContent: boolean
  setLoadingFileContent: (loading: boolean) => void
  showFileContent: boolean
  setShowFileContent: (show: boolean) => void
  showRevisionsModal: boolean
  setShowRevisionsModal: (show: boolean) => void
  
  // Edit mode state
  isEditMode: boolean
  setIsEditMode: (isEdit: boolean) => void
  editedContent: string
  setEditedContent: (content: string) => void
  isSaving: boolean
  setIsSaving: (saving: boolean) => void
  getHasUnsavedChanges: () => boolean
  saveFile: (commitMessage?: string) => Promise<{success: boolean; error?: string}>
  
  // Upload Dialog
  uploadDialog: {
    isOpen: boolean
    isLoading: boolean
    folderPath: string
    commitMessage: string
  }
  setUploadDialog: (dialog: Partial<WorkspaceState['uploadDialog']>) => void
  openUploadDialog: (folderPath?: string) => void
  closeUploadDialog: () => void
  
  // Create Folder Dialog
  createFolderDialog: {
    isOpen: boolean
    parentPath?: string
  }
  setCreateFolderDialog: (dialog: Partial<WorkspaceState['createFolderDialog']>) => void
  openCreateFolderDialog: (parentPath?: string) => void
  closeCreateFolderDialog: () => void
  
  // Delete Dialog
  deleteDialog: {
    isOpen: boolean
    item: PlannerFile | null
    isLoading: boolean
  }
  setDeleteDialog: (dialog: Partial<WorkspaceState['deleteDialog']>) => void
  openDeleteDialog: (item: PlannerFile) => void
  closeDeleteDialog: () => void
  
  // Delete All Files Dialog
  deleteAllFilesDialog: {
    isOpen: boolean
    folder: PlannerFile | null
    isLoading: boolean
  }
  setDeleteAllFilesDialog: (dialog: Partial<WorkspaceState['deleteAllFilesDialog']>) => void
  openDeleteAllFilesDialog: (folder: PlannerFile) => void
  closeDeleteAllFilesDialog: () => void
  
  // Move Dialog
  moveDialog: {
    isOpen: boolean
    item: PlannerFile | null
    destinationPath: string
    commitMessage: string
    isLoading: boolean
  }
  setMoveDialog: (dialog: Partial<WorkspaceState['moveDialog']>) => void
  openMoveDialog: (item: PlannerFile) => void
  closeMoveDialog: () => void
  
  // Actions Dropdown
  showActionsDropdown: boolean
  setShowActionsDropdown: (show: boolean) => void
  
  // File Operations
  addFile: (file: PlannerFile) => void
  removeFile: (filepath: string) => void
  updateFile: (filepath: string, updates: Partial<PlannerFile>) => void
  
  // File fetching
  fetchFiles: () => Promise<void>
  
  // File highlighting
  highlightedFile: string | null
  highlightTimeout: ReturnType<typeof setTimeout> | null
  highlightFile: (filepath: string) => Promise<void>
  clearHighlight: () => void
  
  // Folder expansion
  expandedFolders: Set<string>
  setExpandedFolders: (folders: Set<string>) => void
  expandFoldersForFile: (filepath: string) => void
  toggleFolder: (folderPath: string) => void
  expandFoldersToLevel: (files: PlannerFile[], maxLevel?: number, additionalFolders?: string[], excludeFolders?: string[]) => void
  
  // Auto-scroll functionality
  scrollToFile: (filepath: string) => Promise<void>
  
  // Event processing
  processWorkspaceEvent: (event: PollingEvent) => boolean
  
  // Reset all state
  resetWorkspaceState: () => void
}

const initialState = {
  files: [],
  loading: true,
  error: null,
  searchQuery: '',
  selectedFile: null,
  fileContent: '',
  loadingFileContent: false,
  showFileContent: false,
  showRevisionsModal: false,
  isEditMode: false,
  editedContent: '',
  isSaving: false,
  uploadDialog: {
    isOpen: false,
    isLoading: false,
    folderPath: '/',
    commitMessage: ''
  },
  createFolderDialog: {
    isOpen: false,
    parentPath: undefined
  },
  deleteDialog: {
    isOpen: false,
    item: null,
    isLoading: false
  },
  deleteAllFilesDialog: {
    isOpen: false,
    folder: null,
    isLoading: false
  },
  moveDialog: {
    isOpen: false,
    item: null,
    destinationPath: '',
    commitMessage: '',
    isLoading: false
  },
  showActionsDropdown: false,
  highlightedFile: null,
  highlightTimeout: null,
  expandedFolders: new Set<string>()
}

export const useWorkspaceStore = create<WorkspaceState>()(
  devtools(
    (set, get) => ({
      ...initialState,
      
      // File Management
      setFiles: (files) => set({ files }),
      setLoading: (loading) => set({ loading }),
      setError: (error) => set({ error }),
      
      // Search/Filter
      setSearchQuery: (searchQuery) => set({ searchQuery }),
      
      // Selected file and content
      setSelectedFile: (file) => set({ selectedFile: file }),
      setFileContent: (content) => set({ fileContent: content }),
      setLoadingFileContent: (loading) => set({ loadingFileContent: loading }),
      setShowFileContent: (show) => set({ showFileContent: show }),
      setShowRevisionsModal: (show) => set({ showRevisionsModal: show }),
      
      // Edit mode state
      setIsEditMode: (isEdit) => set({ isEditMode: isEdit }),
      setEditedContent: (content) => set({ editedContent: content }),
      setIsSaving: (saving) => set({ isSaving: saving }),
      getHasUnsavedChanges: () => {
        const state = get()
        return state.editedContent !== state.fileContent && state.isEditMode
      },
      saveFile: async (commitMessage?: string) => {
        const state = get()
        if (!state.selectedFile) {
          return { success: false, error: 'No file selected' }
        }
        
        set({ isSaving: true })
        try {
          const response = await agentApi.updatePlannerFile(
            state.selectedFile.path,
            state.editedContent,
            commitMessage
          )
          
          if (response.success) {
            set({
              fileContent: state.editedContent,
              editedContent: '',
              isEditMode: false,
              isSaving: false
            })
            // Refresh file list
            await get().fetchFiles()
            return { success: true }
          } else {
            set({ isSaving: false })
            return { success: false, error: response.message || 'Failed to save file' }
          }
        } catch (error) {
          set({ isSaving: false })
          const errorMessage = error instanceof Error ? error.message : 'Failed to save file'
          return { success: false, error: errorMessage }
        }
      },
      
      // Upload Dialog
      setUploadDialog: (dialog) => set((state) => ({
        uploadDialog: { ...state.uploadDialog, ...dialog }
      })),
      openUploadDialog: (folderPath = '/') => set({
        uploadDialog: {
          isOpen: true,
          isLoading: false,
          folderPath,
          commitMessage: ''
        }
      }),
      closeUploadDialog: () => set({
        uploadDialog: {
          isOpen: false,
          isLoading: false,
          folderPath: '/',
          commitMessage: ''
        }
      }),
      
      // Create Folder Dialog
      setCreateFolderDialog: (dialog) => set((state) => ({
        createFolderDialog: { ...state.createFolderDialog, ...dialog }
      })),
      openCreateFolderDialog: (parentPath) => set({
        createFolderDialog: {
          isOpen: true,
          parentPath
        }
      }),
      closeCreateFolderDialog: () => set({
        createFolderDialog: {
          isOpen: false,
          parentPath: undefined
        }
      }),
      
      // Delete Dialog
      setDeleteDialog: (dialog) => set((state) => ({
        deleteDialog: { ...state.deleteDialog, ...dialog }
      })),
      openDeleteDialog: (item) => set({
        deleteDialog: {
          isOpen: true,
          item,
          isLoading: false
        }
      }),
      closeDeleteDialog: () => set({
        deleteDialog: {
          isOpen: false,
          item: null,
          isLoading: false
        }
      }),
      
      // Delete All Files Dialog
      setDeleteAllFilesDialog: (dialog) => set((state) => ({
        deleteAllFilesDialog: { ...state.deleteAllFilesDialog, ...dialog }
      })),
      openDeleteAllFilesDialog: (folder) => set({
        deleteAllFilesDialog: {
          isOpen: true,
          folder,
          isLoading: false
        }
      }),
      closeDeleteAllFilesDialog: () => set({
        deleteAllFilesDialog: {
          isOpen: false,
          folder: null,
          isLoading: false
        }
      }),
      
      // Move Dialog
      setMoveDialog: (dialog) => set((state) => ({
        moveDialog: { ...state.moveDialog, ...dialog }
      })),
      openMoveDialog: (item) => {
        set({
          moveDialog: {
            isOpen: true,
            item,
            destinationPath: '',
            commitMessage: '',
            isLoading: false
          }
        })
      },
      closeMoveDialog: () => set({
        moveDialog: {
          isOpen: false,
          item: null,
          destinationPath: '',
          commitMessage: '',
          isLoading: false
        }
      }),
      
      // Actions Dropdown
      setShowActionsDropdown: (showActionsDropdown) => set({ showActionsDropdown }),
      
      // File Operations
      addFile: (file) => set((state) => ({
        files: [...state.files, file]
      })),
      removeFile: (filepath) => set((state) => {
        // IMPORTANT: In workflow mode, state.files is already filtered to only contain files
        // within the workflow folder (this filtering happens in Workspace component).
        // This means we can only remove files that are within the workflow folder scope,
        // which respects the global workflow mode restriction.
        //
        // The filepath parameter is the original path from the tool result (e.g., "Workflow/Some Task/file.txt").
        // In workflow mode, files in the store have:
        //   - filepath: adjusted path (workflow folder prefix removed, e.g., "file.txt")
        //   - originalFilepath: original path (e.g., "Workflow/Some Task/file.txt")
        // We match against both to handle either format.
        
        const removeItem = (files: PlannerFile[]): PlannerFile[] => {
          return files
            .filter(file => {
              // Match by filepath (adjusted in workflow mode) or originalFilepath (original path)
              // This ensures we can remove files whether they have adjusted or original paths
              const matchesFilepath = file.filepath === filepath
              const matchesOriginal = 'originalFilepath' in file && file.originalFilepath === filepath
              
              // If this file matches, don't include it (filter it out)
              if (matchesFilepath || matchesOriginal) {
                return false
              }
              
              // File doesn't match - keep it
              return true
            })
            .map(file => {
              // Recursively process children if they exist
              if (file.children && file.children.length > 0) {
                return {
                  ...file,
                  children: removeItem(file.children)
                }
              }
              return file
            })
        }
        
        const updatedFiles = removeItem(state.files)
        
        // Check if any file was actually removed (files count decreased)
        // This helps ensure we're only removing files that exist in the store
        // (which should already be filtered to workflow folder in workflow mode)
        const fileWasRemoved = updatedFiles.length < state.files.length || 
          JSON.stringify(updatedFiles) !== JSON.stringify(state.files)
        
        // Remove the deleted file/folder from expandedFolders if it was expanded
        // Also remove any child folders that were expanded
        const updatedExpanded = new Set(state.expandedFolders)
        
        if (fileWasRemoved) {
          const removeFromExpanded = (path: string) => {
            // Remove the exact path
            updatedExpanded.delete(path)
            // Also remove any paths that start with the deleted path (child folders)
            // This handles both adjusted and original paths
            Array.from(updatedExpanded).forEach(expandedPath => {
              if (expandedPath.startsWith(path + '/') || expandedPath === path) {
                updatedExpanded.delete(expandedPath)
              }
            })
          }
          
          // Remove from expanded folders using the filepath parameter
          // This could be either adjusted or original path, but we remove both possibilities
          removeFromExpanded(filepath)
        }
        
        return { 
          files: updatedFiles,
          expandedFolders: updatedExpanded // Preserve other expanded folders
        }
      }),
      updateFile: (filepath, updates) => set((state) => {
        const updateItem = (files: PlannerFile[]): PlannerFile[] => {
          return files.map(file => {
            if (file.filepath === filepath) {
              return { ...file, ...updates }
            }
            if (file.children) {
              return {
                ...file,
                children: updateItem(file.children)
              }
            }
            return file
          })
        }
        return { files: updateItem(state.files) }
      }),
      
      // File fetching
      fetchFiles: async () => {
        try {
          set({ loading: true, error: null })
          const response = await agentApi.getPlannerFiles()
          if (response.success && response.data) {
            const allFiles = response.data
            
            // Process hierarchical structure from API
            const processedFiles = processHierarchicalFiles(allFiles)
            set({ files: processedFiles })
            
            // NOTE: Expansion restoration is now handled by the Workspace component
            // which has the necessary context about workflow mode and filtered files
            // The store just fetches and stores raw data
          } else {
            set({ error: response.message || 'Failed to load files' })
          }
        } catch (err) {
          console.error('Failed to fetch Planner files:', err)
          set({ error: err instanceof Error ? err.message : 'Failed to fetch files' })
        } finally {
          set({ loading: false })
        }
      },
      
      // File highlighting
      highlightFile: async (filepath: string) => {
        console.log('[WorkspaceStore] highlightFile called:', filepath)
        const state = get()
        
        // Clear existing timeout
        if (state.highlightTimeout) {
          clearTimeout(state.highlightTimeout)
        }
        
        try {
          // Check if file exists in current file tree
          const fileExists = findFileInTree(state.files, filepath)
          console.log('[WorkspaceStore] highlightFile - file check:', {
            filepath,
            fileExists,
            currentHighlightedFile: state.highlightedFile,
            totalFiles: state.files.length
          })
          
          // If file doesn't exist, refresh the tree
          // Note: In workflow mode, the component will handle filtering
          // so we only refresh if the file is truly missing from raw data
          if (!fileExists) {
            console.log('[WorkspaceStore] File not found in tree, refreshing:', filepath)
            await get().fetchFiles()
            
            // Wait a bit for state to update after refresh
            setTimeout(() => {
              console.log('[WorkspaceStore] Setting highlightedFile after refresh:', filepath)
              set({ highlightedFile: filepath })
            }, 100)
          } else {
            console.log('[WorkspaceStore] Setting highlightedFile (file exists):', filepath)
            set({ highlightedFile: filepath })
          }
          
          // Auto-clear highlight after 5 seconds
          const timeout = setTimeout(() => {
            console.log('[WorkspaceStore] Clearing highlight after timeout:', filepath)
            set({ highlightedFile: null, highlightTimeout: null })
          }, 5000)
          
          set({ highlightTimeout: timeout })
          
        } catch (error) {
          console.error('[WorkspaceStore] Error highlighting file:', error)
        }
      },
      
      // Process workspace events and trigger highlighting or file removal
      processWorkspaceEvent: (event: PollingEvent) => {
        // Handle workspace_file_operation events
        if (event.type === 'workspace_file_operation') {
          try {
            // Try multiple ways to extract event data (event structure may vary)
            let eventData: WorkspaceFileOperationEvent | undefined
            
            // Method 1: Use typed helper (for properly structured events)
            const typedData = getTypedEventData(event, 'workspace_file_operation')
            if (typedData) {
              eventData = typedData as WorkspaceFileOperationEvent
            }
            
            // Method 2: Fallback - access nested data directly
            if (!eventData && event.data && typeof event.data === 'object') {
              const agentEvent = event.data as { data?: unknown }
              const nestedData = agentEvent.data
              if (nestedData && typeof nestedData === 'object') {
                const dataObj = nestedData as Record<string, unknown>
                if (dataObj.operation || dataObj.filepath) {
                  eventData = nestedData as WorkspaceFileOperationEvent
                  console.log('[WorkspaceStore] Using fallback data extraction')
                }
              }
            }
            
            // Method 3: Last resort - check if data is directly on event.data
            if (!eventData && event.data && typeof event.data === 'object') {
              const directData = event.data as Record<string, unknown>
              if (directData.operation || directData.filepath) {
                eventData = directData as WorkspaceFileOperationEvent
                console.log('[WorkspaceStore] Using direct data extraction')
              }
            }
            
            if (!eventData) {
              console.warn('[WorkspaceStore] Could not extract workspace_file_operation event data', {
                eventType: event.type,
                hasData: !!event.data,
                eventDataStructure: event.data,
                eventKeys: event.data ? Object.keys(event.data) : []
              })
              return true
            }
            
            const { operation, filepath } = eventData
            // Check should_highlight flag (defaults to true for backward compatibility)
            const shouldHighlight = eventData.should_highlight !== false
            
            console.log('[WorkspaceStore] Processing workspace_file_operation event:', {
              operation,
              filepath,
              folder: eventData.folder,
              turn: eventData.turn,
              server_name: eventData.server_name,
              should_highlight: eventData.should_highlight,
              willHighlight: shouldHighlight
            })
            
            if (!operation) {
              console.warn('[WorkspaceStore] No operation in workspace_file_operation event')
              return true
            }
            
            // Backend emits full filepaths (e.g., "Workflow/MyProject/file.txt")
            // highlightFile searches in raw unfiltered files, so full paths work correctly
            // Workspace component handles filtering and path adjustment for display
            
            if (operation === 'read' || operation === 'update' || operation === 'patch') {
              if (!filepath) {
                console.warn('[WorkspaceStore] No filepath in event for operation:', operation)
                return true
              }
              
              // Skip highlighting if should_highlight is false (e.g., for logs/ folder)
              if (!shouldHighlight) {
                console.log('[WorkspaceStore] Skipping highlight for file (should_highlight=false):', filepath)
                return true
              }
              
              // Check if file exists in raw file tree, refresh if new, then highlight
              const state = get()
              const fileExists = findFileInTree(state.files, filepath)
              console.log('[WorkspaceStore] File check for highlighting:', {
                filepath,
                fileExists,
                operation,
                totalFiles: state.files.length,
                firstFewFiles: state.files.slice(0, 3).map(f => f.filepath)
              })
              
              // Always try to highlight - if file doesn't exist, refresh first
              if (!fileExists) {
                // File not found - refresh tree to show it (especially for new files)
                console.log('[WorkspaceStore] File not found, refreshing tree:', filepath)
                get().fetchFiles().then(() => {
                  // Wait a bit longer for state to update after refresh
                  setTimeout(() => {
                    console.log('[WorkspaceStore] Calling highlightFile after refresh:', filepath)
                    get().highlightFile(filepath)
                    // Expand folders to show the file (works with workflow folder filtering)
                    get().expandFoldersForFile(filepath)
                  }, 300)
                }).catch(err => {
                  console.error('[WorkspaceStore] Error refreshing files:', err)
                  // Still try to highlight even if refresh fails
                  setTimeout(() => {
                    get().highlightFile(filepath)
                    get().expandFoldersForFile(filepath)
                  }, 100)
                })
              } else {
                // File exists - highlight and expand folders immediately
                console.log('[WorkspaceStore] File exists, calling highlightFile:', filepath)
                get().highlightFile(filepath)
                get().expandFoldersForFile(filepath)
              }
            } else if (operation === 'delete') {
              if (filepath) {
                get().removeFile(filepath)
                // Clear selection if deleted file was selected
                const state = get()
                if (state.selectedFile?.path === filepath) {
                  set({ selectedFile: null, fileContent: '', showFileContent: false })
                }
              }
            } else if (operation === 'list') {
              // List operation - no highlighting needed
            } else if (operation === 'move') {
              // Move operation: source file is deleted, destination file is updated
              // Both events are emitted separately, so we handle them individually
              // The delete event removes the source, the update event highlights the destination
              if (filepath) {
                // Skip highlighting if should_highlight is false (e.g., for logs/ folder)
                if (!shouldHighlight) {
                  console.log('[WorkspaceStore] Skipping highlight for moved file (should_highlight=false):', filepath)
                  return true
                }
                
                const state = get()
                const fileExists = findFileInTree(state.files, filepath)
                if (!fileExists) {
                  get().fetchFiles().then(() => {
                    setTimeout(() => {
                      get().highlightFile(filepath)
                      get().expandFoldersForFile(filepath)
                    }, 300)
                  })
                } else {
                  get().highlightFile(filepath)
                  get().expandFoldersForFile(filepath)
                }
              }
            }
            
            return true
          } catch (error) {
            console.error('[WorkspaceStore] Error processing workspace_file_operation event:', error, event)
            return true // Return true to indicate we handled it (even if there was an error)
          }
        }
        
        return false
      },
      
      clearHighlight: () => {
        const state = get()
        if (state.highlightTimeout) {
          clearTimeout(state.highlightTimeout)
        }
        set({ highlightedFile: null, highlightTimeout: null })
      },
      
      // Auto-scroll to file without highlighting
      scrollToFile: async (filepath: string) => {
        try {
          // Check if file exists and refresh if needed
          const state = get()
          const fileExists = findFileInTree(state.files, filepath)
          if (!fileExists) {
            // File not found, refresh the file list
            await get().fetchFiles()
          }
          
          // Use a small delay to ensure DOM is updated
          setTimeout(() => {
            // Find the file element and scroll to it
            const fileElement = document.querySelector(`[data-filepath="${filepath}"]`)
            if (fileElement) {
              fileElement.scrollIntoView({
                behavior: 'smooth',
                block: 'center',
                inline: 'nearest'
              })
            }
          }, 100)
        } catch (error) {
          console.error('[WorkspaceStore] Error scrolling to file:', error)
        }
      },
      
      // Folder expansion methods
      setExpandedFolders: (folders: Set<string>) => {
        set({ expandedFolders: folders })
      },
      
      expandFoldersForFile: (filepath: string) => {
        const foldersToExpand = extractFolderPaths(filepath)
        
        set(state => ({
          expandedFolders: new Set([...state.expandedFolders, ...foldersToExpand])
        }))
      },
      
      toggleFolder: (folderPath: string) => {
        set(state => {
          const newExpanded = new Set(state.expandedFolders)
          if (newExpanded.has(folderPath)) {
            newExpanded.delete(folderPath)
          } else {
            newExpanded.add(folderPath)
          }
          return { expandedFolders: newExpanded }
        })
      },
      
      expandFoldersToLevel: (files: PlannerFile[], maxLevel: number = 2, additionalFolders?: string[], excludeFolders?: string[]) => {
        // Start with existing expanded folders to merge with them instead of replacing
        const currentExpanded = get().expandedFolders
        const foldersToExpand = new Set<string>(currentExpanded)
        
        // Add any additional folders that should be included (e.g., workflow folder)
        if (additionalFolders) {
          additionalFolders.forEach(folder => foldersToExpand.add(folder))
        }
        
        // Helper function to check if a folder path should be excluded
        const shouldExcludeFolder = (folderPath: string): boolean => {
          if (!excludeFolders || excludeFolders.length === 0) return false
          const lowerPath = folderPath.toLowerCase()
          return excludeFolders.some(excludeFolder => {
            const lowerExclude = excludeFolder.toLowerCase()
            return lowerPath.includes(`/${lowerExclude}`) || 
                   lowerPath.startsWith(lowerExclude) ||
                   lowerPath.endsWith(`/${lowerExclude}`)
          })
        }
        
        const collectFoldersAtLevel = (fileList: PlannerFile[], currentLevel: number) => {
          fileList.forEach(file => {
            if (file.type === 'folder') {
              // Expand folders up to and including maxLevel (0-indexed, so maxLevel=4 means levels 0,1,2,3,4)
              // But skip excluded folders
              if (currentLevel <= maxLevel && !shouldExcludeFolder(file.filepath)) {
                // Use filepath (which is adjusted in workflow mode) to match rendering
                foldersToExpand.add(file.filepath)
              }
              // Always recurse into children to check deeper levels, even if we don't expand this folder
              // This ensures we can expand nested folders even if their parent isn't expanded
              if (file.children && file.children.length > 0) {
                collectFoldersAtLevel(file.children, currentLevel + 1)
              }
            }
          })
        }
        
        collectFoldersAtLevel(files, 0)
        
        set({ expandedFolders: foldersToExpand })
      },
      
      // Reset all state
      resetWorkspaceState: () => set({
        ...initialState,
        expandedFolders: new Set<string>(),
        highlightTimeout: null
      })
    }),
    {
      name: 'workspace-store'
    }
  )
)
