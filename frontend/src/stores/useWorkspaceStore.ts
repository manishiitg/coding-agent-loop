import { create } from 'zustand'
import { devtools } from 'zustand/middleware'
import type { PlannerFile, PollingEvent } from '../services/api-types'
import { agentApi } from '../services/api'
import { findFileInTree, extractFolderPaths, processHierarchicalFiles } from '../utils/fileUtils'
import { parseWorkspaceToolCalls, selectPrimaryFile } from '../utils/goCodeParser'

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
  expandFoldersToLevel: (files: PlannerFile[], maxLevel?: number) => void
  
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
        const state = get()
        
        // Clear existing timeout
        if (state.highlightTimeout) {
          clearTimeout(state.highlightTimeout)
        }
        
        try {
          // Check if file exists in current file tree
          const fileExists = findFileInTree(state.files, filepath)
          
          // If file doesn't exist, refresh the tree
          // Note: In workflow mode, the component will handle filtering
          // so we only refresh if the file is truly missing from raw data
          if (!fileExists) {
            console.log('[WorkspaceStore] File not found, refreshing:', filepath)
            await get().fetchFiles()
            
            // Wait a bit for state to update after refresh
            setTimeout(() => {
              set({ highlightedFile: filepath })
            }, 100)
          } else {
            set({ highlightedFile: filepath })
          }
          
          // Auto-clear highlight after 5 seconds
          const timeout = setTimeout(() => {
            set({ highlightedFile: null, highlightTimeout: null })
          }, 5000)
          
          set({ highlightTimeout: timeout })
          
        } catch (error) {
          console.error('[WorkspaceStore] Error highlighting file:', error)
        }
      },
      
      // Process workspace events and trigger highlighting or file removal
      processWorkspaceEvent: (event: PollingEvent) => {
        // Handle tool_call_start events for highlighting
        if (event.type === 'tool_call_start' && event.data) {
          const eventData = event.data as Record<string, unknown>
          if (!eventData?.data) {
            return false
          }
          
          const toolData = eventData.data as Record<string, unknown>
          const toolName = toolData.tool_name as string
          const toolParams = toolData.tool_params as Record<string, unknown>
          
          // Check if this is write_code tool (code execution mode)
          if (toolName === 'write_code') {
            try {
              const args = JSON.parse((toolParams?.arguments as string) || '{}')
              const code = args.code as string
              
              if (code) {
                // Parse Go code to find workspace tool calls
                const workspaceCalls = parseWorkspaceToolCalls(code)
                
                if (workspaceCalls.length > 0) {
                  // Select the most relevant file to highlight (prioritizes update/delete over read/list)
                  const primaryCall = selectPrimaryFile(workspaceCalls)
                  
                  if (primaryCall && primaryCall.filepath) {
                    // Trigger highlighting for the primary file only
                    // This prevents overwhelming the UI when multiple files are involved
                    get().highlightFile(primaryCall.filepath)
                    
                    return true
                  }
                }
              }
            } catch (error) {
              console.error('[WorkspaceStore] Failed to parse write_code arguments:', error)
            }
            
            return false
          }
          
          // Check if this is a file creation/modification tool
          const fileCreationTools = ['update_workspace_file', 'patch_workspace_file', 'diff_patch_workspace_file', 'read_workspace_file', 'get_workspace_file_nested']
          if (!fileCreationTools.includes(toolName)) {
            return false
          }
          
          try {
            const args = JSON.parse((toolParams?.arguments as string) || '{}')
            const filepath = args.filepath as string
            
            if (filepath) {
              // Detected file operation
              
              // Trigger file highlighting
              get().highlightFile(filepath)
              
              return true
            } else {
              // Tool detected but no filepath in arguments
            }
          } catch (error) {
            console.error('[WorkspaceStore] Failed to parse tool arguments:', error)
          }
          
          return false
        }
        
        // Handle tool_call_end events for file creation/update/deletion
        if (event.type === 'tool_call_end' && event.data) {
          const eventData = event.data as Record<string, unknown>
          if (!eventData?.data) {
            return false
          }
          
          const toolData = eventData.data as Record<string, unknown>
          const toolName = toolData.tool_name as string
          const toolParams = toolData.tool_params as Record<string, unknown>
          
          // Check if this is write_code tool (code execution mode)
          if (toolName === 'write_code') {
            try {
              // Get the code from tool arguments to parse workspace tool calls
              const args = JSON.parse((toolParams?.arguments as string) || '{}')
              const code = args.code as string
              
              if (code) {
                // Parse Go code to find workspace tool calls
                const workspaceCalls = parseWorkspaceToolCalls(code)
                
                if (workspaceCalls.length > 0) {
                  const state = get()
                  const filesToRefresh: string[] = []
                  const filesToHighlight: string[] = []
                  
                  // Process each workspace tool call
                  for (const call of workspaceCalls) {
                    const filepath = call.filepath
                    
                    if (!filepath) continue
                    
                    // Check if file exists in current tree
                    const fileExists = findFileInTree(state.files, filepath)
                    
                    // For update operations, check if file is new or existing
                    if (call.operation === 'update') {
                      if (!fileExists) {
                        // New file created - needs refresh
                        filesToRefresh.push(filepath)
                      }
                      // Always highlight updated files (new or existing)
                      filesToHighlight.push(filepath)
                    } else if (call.operation === 'delete') {
                      // Deletion is handled by checking if file still exists
                      // If it was deleted, it won't be in the tree after refresh
                      if (fileExists) {
                        // File exists now but should be deleted - refresh to update
                        filesToRefresh.push(filepath)
                      }
                    }
                    // Read operations don't need refresh/highlight
                  }
                  
                  // If we have files to refresh (new files or deletions), refresh the tree
                  if (filesToRefresh.length > 0) {
                    console.log('[WorkspaceStore] Refreshing tree for write_code operations:', filesToRefresh)
                    get().fetchFiles().then(() => {
                      // After refresh, highlight all modified files
                      setTimeout(() => {
                        filesToHighlight.forEach(filepath => {
                          get().highlightFile(filepath)
                        })
                      }, 200)
                    })
                  } else if (filesToHighlight.length > 0) {
                    // No refresh needed, just highlight existing files
                    filesToHighlight.forEach(filepath => {
                      get().highlightFile(filepath)
                    })
                  }
                  
                  return filesToRefresh.length > 0 || filesToHighlight.length > 0
                }
              }
            } catch (error) {
              console.error('[WorkspaceStore] Failed to parse write_code on tool_call_end:', error)
            }
            
            return false
          }
          
          // Handle file deletion
          if (toolName === 'delete_workspace_file') {
            try {
              const result = (toolData.result as string) || ''
              if (!result) {
                return false
              }
              
              const parsedResult = JSON.parse(result)
              const filepath = (parsedResult.filepath as string) || ''
              const deleted = (parsedResult.deleted as boolean) || false
              
              if (filepath && deleted) {
                // File or folder was successfully deleted, remove it from UI
                get().removeFile(filepath)
                
                // Also clear selection if the deleted file was selected
                const state = get()
                if (state.selectedFile?.path === filepath) {
                  set({ selectedFile: null, fileContent: '', showFileContent: false })
                }
                
                return true
              }
            } catch (error) {
              console.error('[WorkspaceStore] Failed to parse delete tool result:', error)
            }
            
            return false
          }
          
          // Handle file creation/update - refresh to show new/updated files
          const fileModificationTools = [
            'update_workspace_file', 
            'patch_workspace_file', 
            'diff_patch_workspace_file'
          ]
          
          if (fileModificationTools.includes(toolName)) {
            try {
              // Get filepath from tool arguments
              const args = JSON.parse((toolParams?.arguments as string) || '{}')
              const filepath = args.filepath as string
              
              if (filepath) {
                // Check if file exists in current tree
                const state = get()
                const fileExists = findFileInTree(state.files, filepath)
                
                // If file doesn't exist (new file) or we want to ensure it's up-to-date, refresh
                if (!fileExists) {
                  console.log('[WorkspaceStore] New file created, refreshing tree:', filepath)
                  // Refresh files to show the newly created file
                  get().fetchFiles().then(() => {
                    // After refresh, highlight the file
                    setTimeout(() => {
                      get().highlightFile(filepath)
                    }, 200)
                  })
                } else {
                  // File exists, just highlight it (it might have been updated)
                  get().highlightFile(filepath)
                }
                
                return true
              }
            } catch (error) {
              console.error('[WorkspaceStore] Failed to parse file modification tool result:', error)
            }
            
            return false
          }
          
          return false
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
      
      expandFoldersToLevel: (files: PlannerFile[], maxLevel: number = 2) => {
        const foldersToExpand = new Set<string>()
        
        const collectFoldersAtLevel = (fileList: PlannerFile[], currentLevel: number) => {
          fileList.forEach(file => {
            if (file.type === 'folder' && currentLevel < maxLevel) {
              foldersToExpand.add(file.filepath)
              if (file.children) {
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
