import { create } from 'zustand'
import type { PlannerFile, PollingEvent } from '../services/api-types'
import { agentApi } from '../services/api'
import { extractFolderPaths, processHierarchicalFiles } from '../utils/fileUtils'
import { getTypedEventData } from '../generated/event-types'
import type { WorkspaceFileOperationEvent } from '../generated/events-bridge'
import {
  WORKSPACE_SCROLL_TO_FILE_EVENT,
  type WorkspaceScrollToFileDetail,
} from '../utils/plannerFileTree'

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
  fetchFiles: (folder?: string, options?: { maxDepth?: number; force?: boolean }) => Promise<void>
  
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

  // Dirty folders tracked from workspace_file_operation events.
  // Used to reconcile only changed subtrees instead of re-fetching the full workspace tree.
  dirtyFolders: Set<string>
  markDirtyFolder: (folder: string | null | undefined) => void
  refreshDirtyFolders: (options?: { fallbackToFullFetch?: boolean }) => Promise<void>

  // Reset all state
  resetWorkspaceState: () => void
}

// --- Defensive: deduplicate workspace events to prevent loop when polling returns same events ---
// When the backend re-sends the same workspace_file_operation events in poll responses,
// we skip processing duplicates within this window to avoid repeated highlight/scroll cycles.
const WORKSPACE_EVENT_DEDUP_MS = 2000
const recentlyProcessedWorkspaceEvents = new Map<string, number>()
const DIRTY_REFRESH_DEBOUNCE_MS = 600
let dirtyRefreshTimeout: ReturnType<typeof setTimeout> | null = null

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

const normalizeWorkspacePath = (path: string | null | undefined): string | null => {
  if (!path) return null
  const normalized = path.trim().replace(/^\/+|\/+$/g, '')
  return normalized || null
}

const getParentFolderPath = (path: string | null | undefined): string | null => {
  const normalized = normalizeWorkspacePath(path)
  if (!normalized) return null
  const lastSlash = normalized.lastIndexOf('/')
  return lastSlash === -1 ? null : normalized.slice(0, lastSlash)
}

const pathsOverlap = (left: string | null | undefined, right: string | null | undefined): boolean => {
  const normalizedLeft = normalizeWorkspacePath(left)
  const normalizedRight = normalizeWorkspacePath(right)

  if (!normalizedLeft || !normalizedRight) {
    return !normalizedLeft && !normalizedRight
  }

  return (
    normalizedLeft === normalizedRight ||
    normalizedLeft.startsWith(normalizedRight + '/') ||
    normalizedRight.startsWith(normalizedLeft + '/')
  )
}

const pruneDirtyFoldersToScope = (dirtyFolders: Set<string>, scope: string | null | undefined): Set<string> => {
  const normalizedScope = normalizeWorkspacePath(scope)
  if (!normalizedScope) {
    return new Set(dirtyFolders)
  }

  return new Set(
    Array.from(dirtyFolders).filter(folder => pathsOverlap(folder, normalizedScope))
  )
}

const hasRelevantDirtyFolders = (dirtyFolders: Set<string>, scope: string | null | undefined): boolean => (
  pruneDirtyFoldersToScope(dirtyFolders, scope).size > 0
)

const pruneDirtyFoldersForFetch = (
  dirtyFolders: Set<string>,
  refreshedFolder: string | null | undefined,
  clearDescendants: boolean
): Set<string> => {
  const normalizedRefreshedFolder = normalizeWorkspacePath(refreshedFolder)
  if (!normalizedRefreshedFolder) {
    return new Set()
  }

  return new Set(
    Array.from(dirtyFolders).filter(folder => {
      if (folder === normalizedRefreshedFolder) return false
      if (clearDescendants && folder.startsWith(normalizedRefreshedFolder + '/')) return false
      return true
    })
  )
}

const isFetchAffectedByDirtyState = (fetchFolder: string | null | undefined, dirtyFolders: Set<string>): boolean => {
  if (dirtyFolders.size === 0) return false

  const normalizedFetchFolder = normalizeWorkspacePath(fetchFolder)
  if (!normalizedFetchFolder) {
    return true
  }

  return Array.from(dirtyFolders).some(folder => pathsOverlap(folder, normalizedFetchFolder))
}

const resolveRefreshTargetFolder = (
  candidate: string | null | undefined,
  state: Pick<WorkspaceState, 'activeFolder' | 'fileIndex'>
): string | null => {
  let current = normalizeWorkspacePath(candidate)
  const activeFolder = normalizeWorkspacePath(state.activeFolder)

  while (current) {
    if (activeFolder && current === activeFolder) {
      return activeFolder
    }

    const indexed = state.fileIndex.get(current)
    if (indexed?.type === 'folder') {
      return current
    }

    current = getParentFolderPath(current)
  }

  return activeFolder ?? null
}

const deriveDirtyRefreshFolder = (
  eventData: Pick<WorkspaceFileOperationEvent, 'operation' | 'filepath' | 'folder'>,
  state: Pick<WorkspaceState, 'activeFolder' | 'fileIndex'>
): string | null => {
  const candidates: Array<string | null> = []
  const operation = eventData.operation
  const filepath = normalizeWorkspacePath(eventData.filepath)
  const folderHint = normalizeWorkspacePath(eventData.folder)

  if (folderHint) {
    candidates.push(folderHint)
  }

  if (filepath) {
    const shouldPreferParent =
      operation === 'update' ||
      operation === 'patch' ||
      operation === 'delete' ||
      operation === 'move'

    if (shouldPreferParent) {
      candidates.push(getParentFolderPath(filepath))
    }
    candidates.push(filepath)
    if (!shouldPreferParent) {
      candidates.push(getParentFolderPath(filepath))
    }
  }

  for (const candidate of candidates) {
    const resolved = resolveRefreshTargetFolder(candidate, state)
    if (resolved) {
      return resolved
    }
  }

  return normalizeWorkspacePath(state.activeFolder)
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
  dirtyFolders: new Set<string>()
}

interface FileTreeCacheEntry {
  files: PlannerFile[]
  fileIndex: Map<string, PlannerFile>
}

const ROOT_FOLDER_CACHE_KEY = '__root__'
const FILE_TREE_CACHE_LIMIT = 2
const fileTreeCache = new Map<string, FileTreeCacheEntry>()

function getFileTreeCacheKey(folder: string | undefined, maxDepth?: number): string {
  return `${folder ?? ROOT_FOLDER_CACHE_KEY}::${maxDepth ?? 'full'}`
}

function findReusableFileTreeCacheEntry(folder: string | undefined, maxDepth?: number): FileTreeCacheEntry | null {
  const exact = fileTreeCache.get(getFileTreeCacheKey(folder, maxDepth))
  if (exact) return exact

  if (maxDepth !== undefined) {
    return fileTreeCache.get(getFileTreeCacheKey(folder, undefined)) ?? null
  }

  return null
}

function clearFileTreeCache() {
  fileTreeCache.clear()
}

function setFileTreeCacheEntry(key: string, entry: FileTreeCacheEntry) {
  if (fileTreeCache.has(key)) {
    fileTreeCache.delete(key)
  }
  fileTreeCache.set(key, entry)

  while (fileTreeCache.size > FILE_TREE_CACHE_LIMIT) {
    const oldestKey = fileTreeCache.keys().next().value
    if (!oldestKey) break
    fileTreeCache.delete(oldestKey)
  }
}

// Tracks in-flight fetchFiles requests to deduplicate concurrent calls for the same folder/depth
let inflightFetch: { key: string; promise: Promise<void> } | null = null
let fetchAttemptSequence = 0
const latestFetchAttemptByKey = new Map<string, number>()

function describeWorkspaceFetchError(err: unknown): { message: string; details: Record<string, unknown> } {
  if (!err || typeof err !== 'object') {
    return { message: 'Failed to fetch files', details: { error: String(err) } }
  }

  const error = err as {
    message?: string
    code?: string
    config?: { baseURL?: string; url?: string; params?: unknown; method?: string }
    response?: { status?: number; statusText?: string; data?: unknown }
  }

  return {
    message: error.message || 'Failed to fetch files',
    details: {
      message: error.message,
      code: error.code,
      method: error.config?.method,
      baseURL: error.config?.baseURL,
      url: error.config?.url,
      params: error.config?.params,
      status: error.response?.status,
      statusText: error.response?.statusText,
      responseData: error.response?.data,
    }
  }
}

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

      // Debounced smart refresh so bursts of completion/progress events trigger a single reconcile pass.
      // This preserves the "don't fetch on every workspace event" behavior while still updating
      // automatically without a separate user-facing toggle.
      markDirtyFolder: (folder) => {
        const normalizedFolder = normalizeWorkspacePath(folder)
        if (!normalizedFolder) return

        set((state) => {
          if (state.dirtyFolders.has(normalizedFolder)) {
            return state
          }

          const nextDirtyFolders = new Set(state.dirtyFolders)
          nextDirtyFolders.add(normalizedFolder)

          return {
            dirtyFolders: nextDirtyFolders,
            needsRefresh: true
          }
        })
      },

      refreshDirtyFolders: async (options?: { fallbackToFullFetch?: boolean }) => {
        const state = get()
        const activeFolder = normalizeWorkspacePath(state.activeFolder)
        const scopedDirtyFolders = pruneDirtyFoldersToScope(state.dirtyFolders, activeFolder)

        if (scopedDirtyFolders.size === 0) {
          if (options?.fallbackToFullFetch) {
            await get().fetchFiles(
              state.activeFolder ?? undefined,
              state.activeFolder ? { force: true } : { force: true, maxDepth: 2 }
            )
            return
          }

          set({ needsRefresh: false })
          return
        }

        const refreshTargetByDirtyFolder = new Map<string, string>()
        const refreshTargets = Array.from(scopedDirtyFolders)
          .map(folder => {
            const target = activeFolder && activeFolder.startsWith(folder + '/')
              ? activeFolder
              : folder
            refreshTargetByDirtyFolder.set(folder, target)
            return target
          })
          .filter((folder): folder is string => !!folder)
          .sort((left, right) => left.split('/').length - right.split('/').length)

        const uniqueTargets = Array.from(new Set(refreshTargets))
        const targetSet = new Set(uniqueTargets)

        for (const folder of uniqueTargets) {
          await get().fetchFiles(folder, { force: true, maxDepth: 1 })
        }

        const remainingDirtyFolders = new Set(get().dirtyFolders)
        refreshTargetByDirtyFolder.forEach((target, dirtyFolder) => {
          if (targetSet.has(target)) {
            remainingDirtyFolders.delete(dirtyFolder)
          }
        })

        set({
          dirtyFolders: remainingDirtyFolders,
          needsRefresh: hasRelevantDirtyFolders(remainingDirtyFolders, get().activeFolder)
        })
      },
      
      // File Management
      setFiles: (files) => {
        clearFileTreeCache()
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
            // Refresh file list after a manual save — bypass cache so the tree reflects
            // any filesystem-level changes immediately.
            const activeFolder = get().activeFolder
            await get().fetchFiles(
              activeFolder ?? undefined,
              activeFolder ? { force: true } : { force: true, maxDepth: 2 }
            )
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
        const scheduleDirtyRefresh = (fallbackToFullFetch: boolean) => {
          if (dirtyRefreshTimeout) {
            clearTimeout(dirtyRefreshTimeout)
          }

          dirtyRefreshTimeout = setTimeout(() => {
            dirtyRefreshTimeout = null
            get().refreshDirtyFolders({ fallbackToFullFetch }).catch(error => {
              console.error('[Workspace] Failed to refresh dirty folders:', error)
            })
          }, DIRTY_REFRESH_DEBOUNCE_MS)
        }

        // Smart refresh is always enabled: reconcile dirty folders instead of fetching the full tree.
        if (needsRefresh) {
          console.log('[Workspace] Smart refresh — reconciling dirty folders automatically')
          if (!get().needsRefresh) {
            set({ needsRefresh: true })
          }
          scheduleDirtyRefresh(true)
          return
        }

        if (get().needsRefresh === needsRefresh) return
        set({ needsRefresh })
      },

      // File Operations
      addFile: (file) => set((state) => {
        clearFileTreeCache()
        const updatedFiles = [...state.files, file]
        const index = buildFileIndex(updatedFiles)
        return { files: updatedFiles, fileIndex: index }
      }),
      // Incremental file insert: adds a single file to the correct position in the
      // hierarchical tree without re-fetching the entire file list from the server.
      // Called from processWorkspaceEvent when a tool creates/updates a file not yet in the tree.
      addFileToTree: (filepath: string) => set((state) => {
        clearFileTreeCache()
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
        clearFileTreeCache()
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
        clearFileTreeCache()
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
        set((state) => {
          const scopedDirtyFolders = pruneDirtyFoldersToScope(state.dirtyFolders, folder)
          return {
            activeFolder: folder,
            error: null,
            dirtyFolders: scopedDirtyFolders,
            needsRefresh: hasRelevantDirtyFolders(scopedDirtyFolders, folder)
          }
        })
      },

      // File fetching — uses activeFolder as fallback when no folder is explicitly passed,
      // so that unscoped fetchFiles() calls from StepNode, WorkflowCanvas, etc. stay scoped.
      fetchFiles: async (folder?: string, options?: { maxDepth?: number; force?: boolean }) => {
        // Use activeFolder as fallback so unscoped calls stay scoped to the workflow folder.
        // When an explicit folder IS passed (including undefined from Workspace.tsx chat mode),
        // callers who want to update the scope should call setActiveFolder() separately.
        const effectiveFolder = folder ?? get().activeFolder ?? undefined
        const requestKey = getFileTreeCacheKey(effectiveFolder, options?.maxDepth)

        const applyProcessedFiles = (processedFiles: PlannerFile[]) => {
          const currentActiveFolder = get().activeFolder
          const nextNeedsRefresh = hasRelevantDirtyFolders(get().dirtyFolders, currentActiveFolder)

          if (!effectiveFolder && currentActiveFolder) {
            return
          }

          if (
            effectiveFolder &&
            currentActiveFolder &&
            effectiveFolder !== currentActiveFolder &&
            effectiveFolder.startsWith(currentActiveFolder + '/')
          ) {
            const rootNode = processedFiles.length === 1
              ? processedFiles[0]
              : processedFiles.find(file => file.filepath === effectiveFolder)

            if (rootNode) {
              const mergedFiles = mergeSubfolderIntoTree(get().files, effectiveFolder, rootNode)
              const index = buildFileIndex(mergedFiles)
              set({ files: mergedFiles, fileIndex: index, needsRefresh: nextNeedsRefresh, error: null })
            }
            return
          }

          const index = buildFileIndex(processedFiles)
          set({ files: processedFiles, fileIndex: index, needsRefresh: nextNeedsRefresh, error: null })
        }

        if (!options?.force && !isFetchAffectedByDirtyState(effectiveFolder, get().dirtyFolders)) {
          const cachedEntry = findReusableFileTreeCacheEntry(effectiveFolder, options?.maxDepth)
          if (cachedEntry) {
            applyProcessedFiles(cachedEntry.files)
            return
          }
        }

        // Deduplicate: if a fetch for the same folder/depth is already in flight, reuse it
        if (inflightFetch && inflightFetch.key === requestKey) {
          return inflightFetch.promise
        }

        const requestId = ++fetchAttemptSequence
        latestFetchAttemptByKey.set(requestKey, requestId)
        const isLatestFetchAttempt = () => latestFetchAttemptByKey.get(requestKey) === requestId
        const shouldApplyFetchError = () => {
          const attemptedFolder = normalizeWorkspacePath(effectiveFolder)
          const currentActiveFolder = normalizeWorkspacePath(get().activeFolder)
          const isVisibleScope = attemptedFolder === currentActiveFolder || (!attemptedFolder && !currentActiveFolder)
          return isLatestFetchAttempt() && (isVisibleScope || get().files.length === 0)
        }

        const promise = (async () => {
          try {
            set({ loading: true, error: null })
            const response = await agentApi.getPlannerFiles(effectiveFolder, -1, options?.maxDepth)
            if (response.success && response.data) {
              const allFiles = response.data
              const processedFiles = processHierarchicalFiles(allFiles)
              setFileTreeCacheEntry(requestKey, {
                files: processedFiles,
                fileIndex: buildFileIndex(processedFiles)
              })
              applyProcessedFiles(processedFiles)

              if (options?.force) {
                const nextDirtyFolders = pruneDirtyFoldersForFetch(
                  get().dirtyFolders,
                  effectiveFolder,
                  options.maxDepth === undefined
                )
                set({
                  dirtyFolders: nextDirtyFolders,
                  needsRefresh: hasRelevantDirtyFolders(nextDirtyFolders, get().activeFolder)
                })
              }
            } else {
              const currentActiveFolder = get().activeFolder
              const isScopedSubfolderFetch = !!(
                effectiveFolder &&
                currentActiveFolder &&
                effectiveFolder !== currentActiveFolder &&
                effectiveFolder.startsWith(currentActiveFolder + '/')
              )
              const message = response.message || 'Failed to load files'

              if (isScopedSubfolderFetch && /folder does not exist/i.test(message)) {
                console.warn('[WorkspaceStore] Ignoring missing scoped subfolder during lazy-load:', effectiveFolder)
                if (options?.force) {
                  const nextDirtyFolders = pruneDirtyFoldersForFetch(get().dirtyFolders, effectiveFolder, false)
                  set({
                    dirtyFolders: nextDirtyFolders,
                    needsRefresh: hasRelevantDirtyFolders(nextDirtyFolders, get().activeFolder)
                  })
                }
                return
              }

              if (shouldApplyFetchError()) {
                set({ error: message })
              } else {
                console.warn('[WorkspaceStore] Ignoring stale/background file fetch response error:', {
                  folder: effectiveFolder,
                  message
                })
              }
            }
          } catch (err) {
            const describedError = describeWorkspaceFetchError(err)
            console.error('Failed to fetch Planner files:', JSON.stringify(describedError.details))
            const currentActiveFolder = get().activeFolder
            const isScopedSubfolderFetch = !!(
              effectiveFolder &&
              currentActiveFolder &&
              effectiveFolder !== currentActiveFolder &&
              effectiveFolder.startsWith(currentActiveFolder + '/')
            )
            const message = describedError.message

            if (isScopedSubfolderFetch && /folder does not exist/i.test(message)) {
              console.warn('[WorkspaceStore] Ignoring missing scoped subfolder error during lazy-load:', effectiveFolder)
              if (options?.force) {
                const nextDirtyFolders = pruneDirtyFoldersForFetch(get().dirtyFolders, effectiveFolder, false)
                set({
                  dirtyFolders: nextDirtyFolders,
                  needsRefresh: hasRelevantDirtyFolders(nextDirtyFolders, get().activeFolder)
                })
              }
              return
            }

            if (shouldApplyFetchError()) {
              set({ error: message })
            } else {
              console.warn('[WorkspaceStore] Ignoring stale/background file fetch error:', {
                folder: effectiveFolder,
                message
              })
            }
          } finally {
            set({ loading: false })
            if (inflightFetch?.key === requestKey) {
              inflightFetch = null
            }
            if (latestFetchAttemptByKey.get(requestKey) === requestId) {
              latestFetchAttemptByKey.delete(requestKey)
            }
          }
        })()

        inflightFetch = { key: requestKey, promise }
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

              const dirtyFolder = deriveDirtyRefreshFolder(eventData, get())
              if (dirtyFolder) {
                get().markDirtyFolder(dirtyFolder)
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
                const dirtyFolder = deriveDirtyRefreshFolder(eventData, get())
                if (dirtyFolder) {
                  get().markDirtyFolder(dirtyFolder)
                }

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

                const dirtyFolder = deriveDirtyRefreshFolder(eventData, get())
                if (dirtyFolder) {
                  get().markDirtyFolder(dirtyFolder)
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
          
          // Virtual rows do not exist in the DOM until their parent list scrolls
          // them into view. Expand ancestors first, then ask the list to reveal it.
          get().expandFoldersForFile(filepath)
          setTimeout(() => {
            window.dispatchEvent(new CustomEvent<WorkspaceScrollToFileDetail>(
              WORKSPACE_SCROLL_TO_FILE_EVENT,
              { detail: { filepath } },
            ))
          }, 50)

          // Small non-virtual lists still use the native DOM scroll path.
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
        clearFileTreeCache()
        if (dirtyRefreshTimeout) {
          clearTimeout(dirtyRefreshTimeout)
          dirtyRefreshTimeout = null
        }
        set({
          ...initialState,
          fileIndex: new Map<string, PlannerFile>(),
          expandedFolders: new Set<string>(),
          highlightTimeout: null,
          needsRefresh: false,
          dirtyFolders: new Set<string>()
        })
      }
    })
)
