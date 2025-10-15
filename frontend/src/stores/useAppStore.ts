import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import { devtools } from 'zustand/middleware'
import type { PlannerFile, FileContextItem, AgentMode } from './types'
import { useModeStore, type ModeCategory } from './useModeStore'

interface AppState {
  // Agent configuration
  agentMode: AgentMode
  
  // Workspace state
  files: PlannerFile[]
  selectedFile: {name: string, path: string} | null
  fileContent: string
  loadingFileContent: boolean
  showFileContent: boolean
  showRevisionsModal: boolean
  
  // File context for chat
  chatFileContext: FileContextItem[]
  
  // Chat session state
  currentQuery: string
  chatSessionId: string
  chatSessionTitle: string
  selectedPresetId: string | null
  
  // UI state
  sidebarMinimized: boolean
  workspaceMinimized: boolean
  
  // File operations
  searchQuery: string
  
  // Loading states
  loadingFiles: boolean
  filesError: string | null
  
  // Actions
  setAgentMode: (mode: AgentMode) => void
  
  // Mode category helpers
  getModeCategory: () => ModeCategory
  setModeCategory: (category: ModeCategory) => void
  requiresNewChat: boolean
  
  // Workspace actions
  setFiles: (files: PlannerFile[]) => void
  setSelectedFile: (file: {name: string, path: string} | null) => void
  setFileContent: (content: string) => void
  setLoadingFileContent: (loading: boolean) => void
  setShowFileContent: (show: boolean) => void
  setShowRevisionsModal: (show: boolean) => void
  
  // File context actions
  addFileToContext: (file: FileContextItem) => void
  removeFileFromContext: (path: string) => void
  clearFileContext: () => void
  
  // Chat actions
  setCurrentQuery: (query: string) => void
  setChatSessionId: (id: string) => void
  setChatSessionTitle: (title: string) => void
  setSelectedPresetId: (id: string | null) => void
  
  // UI actions
  setSidebarMinimized: (minimized: boolean) => void
  setWorkspaceMinimized: (minimized: boolean) => void
  
  // File operations
  setSearchQuery: (query: string) => void
  
  // Loading actions
  setLoadingFiles: (loading: boolean) => void
  setFilesError: (error: string | null) => void
  
  // Helper methods
  isFileInContext: (path: string) => boolean
  getContextFileCount: () => number
}

export const useAppStore = create<AppState>()(
  devtools(
    persist(
      (set, get) => ({
        // Initial state
        agentMode: 'ReAct',
        requiresNewChat: false,
        files: [],
        selectedFile: null,
        fileContent: '',
        loadingFileContent: false,
        showFileContent: false,
        showRevisionsModal: false,
        chatFileContext: [],
        currentQuery: '',
        chatSessionId: '',
        chatSessionTitle: '',
        selectedPresetId: null,
        sidebarMinimized: false,
        workspaceMinimized: false,
        searchQuery: '',
        loadingFiles: true,
        filesError: null,

        // Actions
        setAgentMode: (mode) => {
          const currentMode = get().agentMode
          set({ 
            agentMode: mode,
            requiresNewChat: currentMode !== mode
          })
        },

        // Mode category helpers
        getModeCategory: () => {
          const { getModeCategoryFromAgentMode } = useModeStore.getState()
          return getModeCategoryFromAgentMode(get().agentMode)
        },

        setModeCategory: (category) => {
          const { getAgentModeFromCategory } = useModeStore.getState()
          const agentMode = getAgentModeFromCategory(category)
          get().setAgentMode(agentMode as AgentMode)
        },

        // Workspace actions
        setFiles: (files) => {
          set({ files })
        },

        setSelectedFile: (file) => {
          set({ selectedFile: file })
        },

        setFileContent: (content) => {
          set({ fileContent: content })
        },

        setLoadingFileContent: (loading) => {
          set({ loadingFileContent: loading })
        },

        setShowFileContent: (show) => {
          set({ showFileContent: show })
        },

        setShowRevisionsModal: (show) => {
          set({ showRevisionsModal: show })
        },

        // File context actions
        addFileToContext: (file) => {
          set((state) => {
            const exists = state.chatFileContext.some(f => f.path === file.path)
            if (exists) return state
            
            return {
              chatFileContext: [...state.chatFileContext, file]
            }
          })
        },

        removeFileFromContext: (path) => {
          set((state) => ({
            chatFileContext: state.chatFileContext.filter(f => f.path !== path)
          }))
        },

        clearFileContext: () => {
          set({ chatFileContext: [] })
        },

        // Chat actions
        setCurrentQuery: (query) => {
          set({ currentQuery: query })
        },

        setChatSessionId: (id) => {
          set({ chatSessionId: id })
        },

        setChatSessionTitle: (title) => {
          set({ chatSessionTitle: title })
        },

        setSelectedPresetId: (id) => {
          set({ selectedPresetId: id })
        },

        // UI actions
        setSidebarMinimized: (minimized) => {
          set({ sidebarMinimized: minimized })
        },

        setWorkspaceMinimized: (minimized) => {
          set({ workspaceMinimized: minimized })
        },

        // File operations
        setSearchQuery: (query) => {
          set({ searchQuery: query })
        },

        // Loading actions
        setLoadingFiles: (loading) => {
          set({ loadingFiles: loading })
        },

        setFilesError: (error) => {
          set({ filesError: error })
        },

        // Helper methods
        isFileInContext: (path) => {
          const state = get()
          return state.chatFileContext.some(f => f.path === path)
        },

        getContextFileCount: () => {
          const state = get()
          return state.chatFileContext.length
        }
      }),
      {
        name: 'app-store',
        partialize: (state) => ({
        // Only persist user preferences and important state
        agentMode: state.agentMode,
        sidebarMinimized: state.sidebarMinimized,
        workspaceMinimized: state.workspaceMinimized,
        chatFileContext: state.chatFileContext,
        selectedPresetId: state.selectedPresetId
        // Note: requiresNewChat is not persisted as it's temporary state
        })
      }
    ),
    {
      name: 'app-store'
    }
  )
)
