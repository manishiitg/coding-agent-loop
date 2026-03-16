import { create } from 'zustand'
import type { PlannerFile, PollingEvent } from '../services/api-types'
import { agentApi } from '../services/api'
import { extractFolderPaths, processHierarchicalFiles } from '../utils/fileUtils'
import { getTypedEventData } from '../generated/event-types'
import type { WorkspaceFileOperationEvent } from '../generated/events-bridge'

interface WorkspaceState {
  // File Management
  files: PlannerFile[]
  fileIndex: Map<string, PlannerFile> // O(1) lookup index for files by filepath/originalFilepath
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
  binaryFileData: ArrayBuffer | null
  setBinaryFileData: (data: ArrayBuffer | null) => void
  
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
  addFileToTree: (filepath: string) => void
  removeFile: (filepath: string) => void
  updateFile: (filepath: string, updates: Partial<PlannerFile>) => void
  
  // File fetching — activeFolder remembers the last folder scope (e.g., workflow path)
  // so that unscoped fetchFiles() calls from other components don't replace the scoped tree
  activeFolder: string | null
  setActiveFolder: (folder: string | null) => void
  fetchFiles: (folder?: string, options?: { maxDepth?: number }) => Promise<void>
  
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

  // Stale indicator — set when workspace has been modified but not re-fetched
  // (avoids expensive 2-3MB fetchFiles during active execution)
  needsRefresh: boolean
  setNeedsRefresh: (needsRefresh: boolean) => void

  // Auto-refresh: when enabled, automatically fetches files instead of showing the stale banner
  autoRefresh: boolean
  setAutoRefresh: (autoRefresh: boolean) => void

  // Reset all state
  resetWorkspaceState: () => void
}

// --- Defensive: deduplicate workspace events to prevent loop when polling returns same events ---
// When the backend re-sends the same workspace_file_operation events in poll responses,
// we skip processing duplicates within this window to avoid repeated highlight/scroll cycles.
const WORKSPACE_EVENT_DEDUP_MS = 2000
const recentlyProcessedWorkspaceEvents = new Map<string, number>()

function shouldSkipDuplicateWorkspaceEvent(eventId: string | undefined, filepath: string, operation: string): boolean {
  const key = eventId || `${filepath}:${operation}`
  const now = Date.now()
  const lastProcessed = recentlyProcessedWorkspaceEvents.get(key)
  if (lastProcessed && now - lastProcessed < WORKSPACE_EVENT_DEDUP_MS) {
    return true // Skip duplicate
  }
  recentlyProcessedWorkspaceEvents.set(key, now)
  // Prune old entries to prevent unbounded growth
  if (recentlyProcessedWorkspaceEvents.size > 100) {
    const cutoff = now - WORKSPACE_EVENT_DEDUP_MS * 2
    for (const [k, ts] of recentlyProcessedWorkspaceEvents) {
      if (ts < cutoff) recentlyProcessedWorkspaceEvents.delete(k)
    }
  }
  return false
}

// Helper function to build file index for O(1) lookups
const buildFileIndex = (files: PlannerFile[]): Map<string, PlannerFile> => {
  const index = new Map<string, PlannerFile>()
  
  const indexFile = (file: PlannerFile) => {
    // Index by filepath (adjusted path in workflow mode)
    index.set(file.filepath, file)
    
    // Also index by originalFilepath if available (original path)
    if ('originalFilepath' in file && file.originalFilepath) {
      index.set(file.originalFilepath, file)
    }
    
    // Index by filename for relative filename lookups
    const filename = file.filepath.split('/').pop() || file.filepath
    if (filename && filename !== file.filepath) {
      // Only add if filename is different from full path (avoid duplicates)
      if (!index.has(filename) || index.get(filename) === file) {
        index.set(filename, file)
      }
    }
    
    // Index children recursively
    if (file.children && file.children.length > 0) {
      file.children.forEach(indexFile)
    }
  }
  
  files.forEach(indexFile)
  return index
}

// --- Incremental file tree updates ---
// Previously, every workspace_file_operation event for a new file triggered a full fetchFiles()
// call (~2MB payload for large workspaces). Now we insert new files into the tree client-side
// using addFileToTree(), avoiding the network round-trip entirely.

// Image file extensions for client-side detection (mirrors backend isImageFile in documents.go)
const IMAGE_EXTENSIONS = new Set(['.png', '.jpg', '.jpeg', '.gif', '.bmp', '.webp', '.svg', '.ico'])

const isImageFile = (filename: string): boolean => {
  const dot = filename.lastIndexOf('.')
  if (dot === -1) return false
  return IMAGE_EXTENSIONS.has(filename.slice(dot).toLowerCase())
}

// Insert a new file into the correct position in a hierarchical file tree,
// creating only TRULY missing intermediate folders.
//
// The tree from fetchFiles(folder) has nodes with FULL multi-segment filepaths
// (e.g., "Workflow/MyProject/runs"), NOT single-segment names. We walk the tree
// by matching existing folder prefixes. If no existing parent can be found at
// root level, we bail out (return null) and let the caller fall back to fetchFiles
// — this avoids creating phantom duplicate folders when the tree is scoped to a
// workspace sub-path.
const insertIntoTree = (files: PlannerFile[], newFile: PlannerFile): PlannerFile[] | null => {
  const targetPath = newFile.filepath
  const root = [...files]

  // Recursively descend into the deepest matching parent folder, then create
  // any remaining intermediate folders and append the new file.
  // Returns false if no matching parent was found at all (caller should fallback).
  const insertAt = (items: PlannerFile[], parentPath: string, isRoot: boolean): boolean => {
    // Try to descend into a deeper matching folder first
    for (let i = 0; i < items.length; i++) {
      const item = items[i]
      if (item.type === 'folder' && targetPath.startsWith(item.filepath + '/')) {
        // Clone this node (immutable update) and descend into its children
        const cloned = { ...item, children: [...(item.children || [])] }
        items[i] = cloned
        return insertAt(cloned.children!, item.filepath, false)
      }
    }

    // No matching folder found at this level.
    // If we're still at root and never descended, bail out — we can't safely create
    // intermediate folders because the tree uses multi-segment filepaths and we'd
    // create duplicates (e.g., a "Workflow" folder alongside "Workflow/MyProject").
    if (isRoot && !parentPath) {
      return false
    }

    // We're inside a matched parent — create remaining intermediate folders
    const remaining = targetPath.slice(parentPath.length + 1)
    const parts = remaining.split('/')

    if (parts.length === 1) {
      // Direct child — just append
      if (!items.some(f => f.filepath === targetPath)) {
        items.push(newFile)
      }
      return true
    }

    // Create missing intermediate folders for the remaining path segments
    let current = items
    for (let i = 0; i < parts.length - 1; i++) {
      const folderPath = parentPath + '/' + parts.slice(0, i + 1).join('/')

      const existingIdx = current.findIndex(f => f.filepath === folderPath && f.type === 'folder')
      if (existingIdx === -1) {
        const newFolder: PlannerFile = {
          filepath: folderPath,
          content: '',
          last_modified: new Date().toISOString(),
          type: 'folder',
          children: [],
        }
        current.push(newFolder)
        current = newFolder.children!
      } else {
        // Clone existing folder to keep immutable
        const cloned = { ...current[existingIdx], children: [...(current[existingIdx].children || [])] }
        current[existingIdx] = cloned
        current = cloned.children!
      }
    }

    if (!current.some(f => f.filepath === targetPath)) {
      current.push(newFile)
    }
    return true
  }

  const inserted = insertAt(root, '', true)
  return inserted ? root : null
}

const initialState = {
  files: [],
  fileIndex: new Map<string, PlannerFile>(),
  loading: true,
  error: null,
  searchQuery: '',
  selectedFile: null,
  fileContent: '',
  loadingFileContent: false,
  showFileContent: false,
  showRevisionsModal: false,
  binaryFileData: null,
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
  activeFolder: null,
  highlightedFile: null,
  highlightTimeout: null,
  expandedFolders: new Set<string>(),
  needsRefresh: false,
  autoRefresh: false
}

// Tracks in-flight fetchFiles requests to deduplicate concurrent calls for the same folder
let inflightFetch: { folder: string | undefined; promise: Promise<void> } | null = null

// Merge a fetched subfolder node into an existing file tree (replaces the matching stub node)
function mergeSubfolderIntoTree(tree: PlannerFile[], targetPath: string, replacement: PlannerFile): PlannerFile[] {
  return tree.map(node => {
    if (node.filepath === targetPath) {
      return replacement
    }
    if (node.children && node.children.length > 0 && targetPath.startsWith(node.filepath + '/')) {
      return { ...node, children: mergeSubfolderIntoTree(node.children, targetPath, replacement) }
    }
    return node
  })
}

export const useWorkspaceStore = create<WorkspaceState>()(
    (set, get) => ({
      ...initialState,
      
      // File Management
      setFiles: (files) => {
        const index = buildFileIndex(files)
        set({ files, fileIndex: index })
      },
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
      setBinaryFileData: (data) => set({ binaryFileData: data }),
      
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

      // Stale indicator
      setNeedsRefresh: (needsRefresh: boolean) => {
        if (get().needsRefresh === needsRefresh) return
        // When auto-refresh is enabled, fetch immediately instead of showing the banner
        if (needsRefresh && get().autoRefresh) {
          console.log('[Workspace] Auto-refresh enabled — fetching files automatically')
          get().fetchFiles()
          return // fetchFiles sets needsRefresh=false on completion
        }
        set({ needsRefresh })
      },

      // Auto-refresh toggle
      setAutoRefresh: (autoRefresh: boolean) => {
        set({ autoRefresh })
        // If enabling auto-refresh while stale, refresh immediately
        if (autoRefresh && get().needsRefresh) {
          console.log('[Workspace] Auto-refresh enabled while stale — fetching files')
          get().fetchFiles()
        }
      },

      // File Operations
      addFile: (file) => set((state) => {
        const updatedFiles = [...state.files, file]
        const index = buildFileIndex(updatedFiles)
        return { files: updatedFiles, fileIndex: index }
      }),
      // Incremental file insert: adds a single file to the correct position in the
      // hierarchical tree without re-fetching the entire file list from the server.
      // Called from processWorkspaceEvent when a tool creates/updates a file not yet in the tree.
      addFileToTree: (filepath: string) => set((state) => {
        // Skip if file already exists in the index — no work needed
        const normalizedPath = filepath.trim()
        if (state.fileIndex.has(normalizedPath)) {
          return state
        }

        const fileName = normalizedPath.split('/').pop() || normalizedPath
        const newFile: PlannerFile = {
          filepath: normalizedPath,
          content: '',
          last_modified: new Date().toISOString(),
          type: 'file',
          is_image: isImageFile(fileName),
        }

        // insertIntoTree walks the tree by matching existing folder prefixes and inserts
        // the file. Returns null if no matching parent folder was found (tree is scoped
        // to a sub-path and we can't safely create intermediates without duplicates).
        // In that case we no-op — the debounced fetchFiles from WorkflowLayout will
        // pick up the file shortly.
        const updatedFiles = insertIntoTree([...state.files], newFile)
        if (!updatedFiles) {
          return state // Couldn't insert safely — let fetchFiles handle it
        }
        const index = buildFileIndex(updatedFiles)
        return { files: updatedFiles, fileIndex: index }
      }),
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
        
        // Rebuild index after removal
        const index = buildFileIndex(updatedFiles)
        
        return { 
          files: updatedFiles,
          fileIndex: index,
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
        const updatedFiles = updateItem(state.files)
        const index = buildFileIndex(updatedFiles)
        return { files: updatedFiles, fileIndex: index }
      }),
      
      // Active folder — remembers the current workspace scope (e.g., "Workflow/MyProject").
      // When components call fetchFiles() without a folder, this prevents them from
      // accidentally fetching the entire root tree and replacing the workflow-scoped tree.
      setActiveFolder: (folder: string | null) => {
        set({ activeFolder: folder })
      },

      // File fetching — uses activeFolder as fallback when no folder is explicitly passed,
      // so that unscoped fetchFiles() calls from StepNode, WorkflowCanvas, etc. stay scoped.
      fetchFiles: async (folder?: string, options?: { maxDepth?: number }) => {
        // Use activeFolder as fallback so unscoped calls stay scoped to the workflow folder.
        // When an explicit folder IS passed (including undefined from Workspace.tsx chat mode),
        // callers who want to update the scope should call setActiveFolder() separately.
        const effectiveFolder = folder ?? get().activeFolder ?? undefined

        // Deduplicate: if a fetch for the same folder is already in flight, reuse it
        if (inflightFetch && inflightFetch.folder === effectiveFolder) {
          return inflightFetch.promise
        }

        const promise = (async () => {
          try {
            set({ loading: true, error: null })
            console.time(`[PERF] fetchFiles (folder=${effectiveFolder || 'root'})`)
            console.trace(`[PERF] fetchFiles called (folder=${effectiveFolder || 'root'})`)
            console.time(`[PERF] fetchFiles-network (folder=${effectiveFolder || 'root'})`)
            const response = await agentApi.getPlannerFiles(effectiveFolder, -1, options?.maxDepth)
            console.timeEnd(`[PERF] fetchFiles-network (folder=${effectiveFolder || 'root'})`)
            if (response.success && response.data) {
              // Guard against mount race condition: if this fetch was unscoped (effectiveFolder
              // was undefined) but by the time the response arrived, activeFolder has been set,
              // it means a scoped fetch is already in flight or completed. Storing these unscoped
              // results would replace the correctly scoped tree with the full root tree.
              const currentActiveFolder = get().activeFolder
              if (!effectiveFolder && currentActiveFolder) {
                return
              }

              const allFiles = response.data

              // Process hierarchical structure from API
              console.time(`[PERF] processHierarchicalFiles (${allFiles.length} files)`)
              const processedFiles = processHierarchicalFiles(allFiles)
              console.timeEnd(`[PERF] processHierarchicalFiles (${allFiles.length} files)`)

              // If fetching a subfolder of activeFolder, merge into the existing tree
              // instead of replacing — used for lazy-loading iteration contents
              if (effectiveFolder && currentActiveFolder &&
                  effectiveFolder !== currentActiveFolder &&
                  effectiveFolder.startsWith(currentActiveFolder + '/')) {
                const rootNode = processedFiles.length === 1 ? processedFiles[0] : processedFiles.find(f => f.filepath === effectiveFolder)
                if (rootNode) {
                  const mergedFiles = mergeSubfolderIntoTree(get().files, effectiveFolder, rootNode)
                  const index = buildFileIndex(mergedFiles)
                  set({ files: mergedFiles, fileIndex: index, needsRefresh: false })
                }
                // If rootNode not found (e.g., iteration doesn't exist), silently skip
              } else {
                const index = buildFileIndex(processedFiles)
                set({ files: processedFiles, fileIndex: index, needsRefresh: false })
              }

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
            console.timeEnd(`[PERF] fetchFiles (folder=${effectiveFolder || 'root'})`)
            set({ loading: false })
            inflightFetch = null
          }
        })()

        inflightFetch = { folder: effectiveFolder, promise }
        return promise
      },
      
      // File highlighting
      highlightFile: async (filepath: string) => {
        const state = get()

        // Clear existing timeout
        if (state.highlightTimeout) {
          clearTimeout(state.highlightTimeout)
        }

        // Defensive: skip if already highlighting this file (prevents scroll/rerender loop)
        if (state.highlightedFile === filepath) {
          // Just extend the timeout — single set() instead of two
          const timeout = setTimeout(() => {
            set({ highlightedFile: null, highlightTimeout: null })
          }, 5000)
          set({ highlightTimeout: timeout })
          return
        }

        // PERF FIX: Single set() call instead of two separate ones.
        // Previously: set({ highlightedFile }) + set({ highlightTimeout }) = 2 re-renders.
        // Now: set({ highlightedFile, highlightTimeout }) = 1 re-render.
        const timeout = setTimeout(() => {
          set({ highlightedFile: null, highlightTimeout: null })
        }, 5000)
        set({ highlightedFile: filepath, highlightTimeout: timeout })
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
                }
              }
            }
            
            // Method 3: Last resort - check if data is directly on event.data
            if (!eventData && event.data && typeof event.data === 'object') {
              const directData = event.data as Record<string, unknown>
              if (directData.operation || directData.filepath) {
                eventData = directData as WorkspaceFileOperationEvent
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
            // Defensive: skip duplicate events (prevents loop when polling re-sends same events)
            const eventId = (event as { id?: string }).id
            if (shouldSkipDuplicateWorkspaceEvent(eventId, filepath || '', operation || '')) {
              return true // Handled (skipped as duplicate)
            }
            // Check should_highlight flag (defaults to true for backward compatibility)
            const shouldHighlight = eventData.should_highlight !== false
            
            if (!operation) {
              return true
            }
            
            // Backend emits full filepaths (e.g., "Workflow/MyProject/file.txt")
            // highlightFile searches in raw unfiltered files, so full paths work correctly
            // Workspace component handles filtering and path adjustment for display
            
            if (operation === 'read') {
              // PERF FIX: Read operations don't modify files — skip highlighting entirely.
              //
              // PROBLEM: Every 'read' event triggered highlightFile() → 2 Zustand set() calls
              // (highlightedFile + highlightTimeout) → 2 re-renders of Workspace sidebar.
              // A typical workflow step has 20-50 file reads, causing 40-100 unnecessary re-renders.
              //
              // FIX: Early return for reads. Only 'update', 'patch', 'delete', 'move' need processing.
              return true
            }

            if (operation === 'update' || operation === 'patch') {
              if (!filepath) {
                return true
              }

              // Skip highlighting if should_highlight is false (e.g., for logs/ folder)
              if (!shouldHighlight) {
                return true
              }

              // Use index for O(1) lookup instead of O(n) tree search
              const state = get()
              const normalizedPath = filepath.trim()
              const fileExists = state.fileIndex.has(normalizedPath) ||
                                state.fileIndex.has(normalizedPath.split('/').pop() || '')

              // If file doesn't exist, insert it incrementally (avoids full ~2MB re-fetch).
              // NOTE: We only call highlightFile() here — NOT expandFoldersForFile().
              // Folder expansion is handled by the Workspace component's highlightedFile effect,
              // which uses the display-adjusted path (correct for workflow mode).
              if (!fileExists) {
                get().addFileToTree(filepath)
                // Small delay for Zustand state to propagate before highlighting
                setTimeout(() => {
                  get().highlightFile(filepath)
                }, 50)
              } else {
                get().highlightFile(filepath)
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
                  return true
                }
                
                const state = get()
                const normalizedPath = filepath.trim()
                const fileExists = state.fileIndex.has(normalizedPath) || 
                                  state.fileIndex.has(normalizedPath.split('/').pop() || '')
                if (!fileExists) {
                  // Move destination: insert incrementally instead of full re-fetch
                  get().addFileToTree(filepath)
                  setTimeout(() => {
                    get().highlightFile(filepath)
                  }, 50)
                } else {
                  get().highlightFile(filepath)
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
          // We no longer call fetchFiles() here when file is missing, because calling
          // it without a folder param fetches ALL files from root, replacing the
          // workflow-scoped tree. The file will appear after the next debounced fetch.
          
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
            // For "logs", exclude if it's nested under iteration/group pattern
            // Paths can be adjusted in workflow mode, so check for patterns like:
            // - runs/iteration-x/group-x/logs (original)
            // - iteration-x/group-x/logs (adjusted, if runs is workflow folder)
            // - Any path ending with /logs that's under an iteration/group structure
            if (lowerExclude === 'logs') {
              // Check if path ends with /logs
              const endsWithLogs = lowerPath.endsWith(`/${lowerExclude}`) || lowerPath === lowerExclude
              if (!endsWithLogs) return false
              
              // Check if it's in an iteration/group structure
              // Match patterns like: .../iteration-X/.../logs or .../group-X/.../logs
              const hasIterationPattern = /iteration-\d+/.test(lowerPath)
              // Also check for runs/ pattern (in case paths aren't adjusted)
              const hasRunsPattern = lowerPath.includes('/runs/') || lowerPath.startsWith('runs/')
              
              // Exclude if it's in runs structure OR has iteration pattern
              return hasIterationPattern || hasRunsPattern
            }
            // For other folders, check if path contains the folder name at any level
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
              // Check both filepath (adjusted) and originalFilepath (original) to catch logs folders
              const filePathToCheck = file.filepath
              const originalPathToCheck = ('originalFilepath' in file && file.originalFilepath) ? file.originalFilepath : null
              const isExcluded = shouldExcludeFolder(filePathToCheck) || (originalPathToCheck && shouldExcludeFolder(originalPathToCheck))
              
              if (currentLevel <= maxLevel && !isExcluded) {
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
      resetWorkspaceState: () => {
        recentlyProcessedWorkspaceEvents.clear()
        set({
          ...initialState,
          fileIndex: new Map<string, PlannerFile>(),
          expandedFolders: new Set<string>(),
          highlightTimeout: null,
          needsRefresh: false
        })
      }
    })
)
