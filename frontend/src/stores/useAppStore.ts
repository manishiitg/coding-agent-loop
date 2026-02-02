import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import { devtools } from 'zustand/middleware'
import type { AgentMode } from './types'
import { useModeStore, type ModeCategory } from './useModeStore'

interface AppState {
  // Agent configuration
  agentMode: AgentMode
  
  // Chat session state
  currentQuery: string
  chatSessionId: string
  chatSessionTitle: string
  selectedPresetId: string | null
  
  // UI state
  sidebarMinimized: boolean
  workspaceMinimized: boolean
  
  // Code execution mode (for chat mode when no preset is active)
  useCodeExecutionMode: boolean
  
  // Actions
  setAgentMode: (mode: AgentMode) => void
  
  // Mode category helpers
  getModeCategory: () => ModeCategory
  setModeCategory: (category: ModeCategory) => void
  requiresNewChat: boolean
  clearRequiresNewChat: () => void
  
  // Chat actions
  setCurrentQuery: (query: string) => void
  setChatSessionId: (id: string) => void
  setChatSessionTitle: (title: string) => void
  setSelectedPresetId: (id: string | null) => void
  
  // UI actions
  setSidebarMinimized: (minimized: boolean) => void
  setWorkspaceMinimized: (minimized: boolean) => void
  setUseCodeExecutionMode: (enabled: boolean) => void

}

export const useAppStore = create<AppState>()(
  devtools(
    persist(
      (set, get) => {
        // Sync flag to prevent circular updates
        let isSyncing = false
        
        return {
          // Initial state
          agentMode: 'simple',
          requiresNewChat: false,
          currentQuery: '',
          chatSessionId: '',
          chatSessionTitle: '',
          selectedPresetId: null,
          sidebarMinimized: false,
          workspaceMinimized: false,
          useCodeExecutionMode: true, // Default to enabled
          // Actions
          setAgentMode: (mode) => {
            const currentMode = get().agentMode
            
            // Only update if mode actually changed
            if (currentMode === mode) {
              return
            }
            
            set({ 
              agentMode: mode,
              requiresNewChat: currentMode !== mode
            })
            
            // Sync ModeStore category when agentMode changes
            // Only sync if not already syncing to prevent circular updates
            if (!isSyncing) {
              isSyncing = true
              const { getModeCategoryFromAgentMode, setModeCategory } = useModeStore.getState()
              const category = getModeCategoryFromAgentMode(mode)
              
              // Update ModeStore if category would be different
              if (category && category !== useModeStore.getState().selectedModeCategory) {
                setModeCategory(category)
              }
              isSyncing = false
            }
          },

        // Mode category helpers
        getModeCategory: () => {
          const { getModeCategoryFromAgentMode } = useModeStore.getState()
          return getModeCategoryFromAgentMode(get().agentMode)
        },

        // Simplified: Just delegate to ModeStore, which handles all synchronization
        setModeCategory: (category) => {
          useModeStore.getState().setModeCategory(category)
        },

        clearRequiresNewChat: () => {
          set({ requiresNewChat: false })
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

        setUseCodeExecutionMode: (enabled) => {
          set({ useCodeExecutionMode: enabled })
        },
        }
      },
      {
        name: 'app-store',
        partialize: (state) => ({
        // Only persist user preferences and important state
        agentMode: state.agentMode,
        sidebarMinimized: state.sidebarMinimized,
        workspaceMinimized: state.workspaceMinimized,
        selectedPresetId: state.selectedPresetId,
        useCodeExecutionMode: state.useCodeExecutionMode
        // Note: requiresNewChat is not persisted as it's temporary state
        // File context is now mode-specific: chat tabs have their own, workflow uses preset
        })
      }
    ),
    {
      name: 'app-store'
    }
  )
)
