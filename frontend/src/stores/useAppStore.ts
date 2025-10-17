import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import { devtools } from 'zustand/middleware'
import type { FileContextItem, AgentMode } from './types'
import { useModeStore, type ModeCategory } from './useModeStore'

interface AppState {
  // Agent configuration
  agentMode: AgentMode
  
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
  
  // Actions
  setAgentMode: (mode: AgentMode) => void
  
  // Mode category helpers
  getModeCategory: () => ModeCategory
  setModeCategory: (category: ModeCategory) => void
  requiresNewChat: boolean
  clearRequiresNewChat: () => void
  
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
        chatFileContext: [],
        currentQuery: '',
        chatSessionId: '',
        chatSessionTitle: '',
        selectedPresetId: null,
        sidebarMinimized: false,
        workspaceMinimized: false,

        // Actions
        setAgentMode: (mode) => {
          const currentMode = get().agentMode
          // Keep ModeStore category in sync
          const { getModeCategoryFromAgentMode, setModeCategory } = useModeStore.getState()
          const category = getModeCategoryFromAgentMode(mode)
          if (category) setModeCategory(category)
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

        clearRequiresNewChat: () => {
          set({ requiresNewChat: false })
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
