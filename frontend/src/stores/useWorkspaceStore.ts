import { create } from 'zustand'
import { devtools } from 'zustand/middleware'
import type { PlannerFile, PollingEvent } from '../services/api-types'
import { agentApi } from '../services/api'
import { findFileInTree, extractFolderPaths } from '../utils/fileUtils'

// Helper functions
const processHierarchicalFiles = (files: PlannerFile[]): PlannerFile[] => {
  // API returns hierarchical structure directly, just ensure type is set correctly
  return files.map(file => ({
    ...file,
    type: file.type || 'file', // Ensure type is set
    children: file.children || [] // Ensure children array exists
  }))
}

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
  highlightTimeout: NodeJS.Timeout | null
  highlightFile: (filepath: string) => Promise<void>
  clearHighlight: () => void
  
  // Folder expansion
  expandedFolders: Set<string>
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
      
      // Actions Dropdown
      setShowActionsDropdown: (showActionsDropdown) => set({ showActionsDropdown }),
      
      // File Operations
      addFile: (file) => set((state) => ({
        files: [...state.files, file]
      })),
      removeFile: (filepath) => set((state) => {
        const removeItem = (files: PlannerFile[]): PlannerFile[] => {
          return files.filter(file => {
            if (file.filepath === filepath) {
              return false // Remove this item
            }
            if (file.children) {
              return {
                ...file,
                children: removeItem(file.children)
              }
            }
            return true
          }).map(file => {
            if (file.children) {
              return {
                ...file,
                children: removeItem(file.children)
              }
            }
            return file
          })
        }
        return { files: removeItem(state.files) }
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
          
          if (!fileExists) {
            // File not found in workspace, refreshing
            
            // Trigger file refresh
            await get().fetchFiles()
            
            // Wait a bit for state to update after refresh
            setTimeout(() => {
              set({ highlightedFile: filepath })
                  // Highlighting file after refresh
            }, 100)
          } else {
            set({ highlightedFile: filepath })
            // Highlighting existing file
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
      
      // Process workspace events and trigger highlighting
      processWorkspaceEvent: (event: PollingEvent) => {
        // Only process tool_call_start events
        if (event.type !== 'tool_call_start' || !event.data) {
          return false
        }
        
        const eventData = event.data as Record<string, unknown>
        if (!eventData?.data) {
          return false
        }
        
        const toolData = eventData.data as Record<string, unknown>
        const toolName = toolData.tool_name as string
        const toolParams = toolData.tool_params as Record<string, unknown>
        
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
      expandFoldersForFile: (filepath: string) => {
        const foldersToExpand = extractFolderPaths(filepath)
        console.log('[WorkspaceStore] Expanding folders for file:', filepath)
        console.log('[WorkspaceStore] Folders to expand:', foldersToExpand)
        
        set(state => ({
          expandedFolders: new Set([...state.expandedFolders, ...foldersToExpand])
        }))
        
        console.log('[WorkspaceStore] Updated expandedFolders:', [...foldersToExpand])
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
      resetWorkspaceState: () => set(initialState)
    }),
    {
      name: 'workspace-store',
      partialize: (state: WorkspaceState) => ({
        // Only persist search query and dialog states
        searchQuery: state.searchQuery,
        uploadDialog: state.uploadDialog,
        createFolderDialog: state.createFolderDialog,
        deleteDialog: state.deleteDialog
      })
    }
  )
)
