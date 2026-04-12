import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import type { AgentMode } from './types'
import { useModeStore, type ModeCategory } from './useModeStore'

interface AppState {
  // Agent configuration
  agentMode: AgentMode
  
  // Chat session state
  currentQuery: string
  selectedPresetId: string | null
  
  // UI state
  sidebarMinimized: boolean
  workspaceMinimized: boolean
  showWorkflowsOverview: boolean
  
  // Code execution mode (for multi-agent mode when no preset is active)
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
  setSelectedPresetId: (id: string | null) => void
  
  // UI actions
  setSidebarMinimized: (minimized: boolean) => void
  setWorkspaceMinimized: (minimized: boolean) => void
  setShowWorkflowsOverview: (show: boolean) => void
  setUseCodeExecutionMode: (enabled: boolean) => void
  // Last-used tab settings — inherited by new tabs
  lastSelectedSkills: string[]
  lastSelectedSubAgents: string[]
  lastBrowserMode: 'none' | 'headless' | 'cdp' | 'playwright' | 'stealth'
  lastEnableImageGeneration: boolean
  lastGWSAccess: boolean
  syncLastTabSettings: (update: Partial<Pick<AppState, 'lastSelectedSkills' | 'lastSelectedSubAgents' | 'lastBrowserMode' | 'lastEnableImageGeneration' | 'lastGWSAccess'>>) => void
}

export const useAppStore = create<AppState>()(
    persist(
      (set, get) => {
        // Sync flag to prevent circular updates
        let isSyncing = false
        
        return {
          // Initial state
          agentMode: 'simple',
          requiresNewChat: false,
          currentQuery: '',
          selectedPresetId: null,
          sidebarMinimized: false,
          workspaceMinimized: false,
          showWorkflowsOverview: false,
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
              const { getModeCategoryFromAgentMode, getAgentModeFromCategory, setModeCategory } = useModeStore.getState()
              const currentCategory = useModeStore.getState().selectedModeCategory

              // Don't override if the current category already maps to the same agent mode.
              // This prevents 'multi-agent' (which maps to 'simple') from being overwritten incorrectly.
              const currentCategoryAgentMode = currentCategory ? getAgentModeFromCategory(currentCategory) : null
              if (currentCategoryAgentMode !== mode) {
                const category = getModeCategoryFromAgentMode(mode)
                if (category && category !== currentCategory) {
                  setModeCategory(category)
                }
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

        setShowWorkflowsOverview: (show) => {
          set({ showWorkflowsOverview: show })
        },

        setUseCodeExecutionMode: (enabled) => {
          set({ useCodeExecutionMode: enabled })
        },

        lastSelectedSkills: [],
        lastSelectedSubAgents: [],
        lastBrowserMode: 'none',
        lastEnableImageGeneration: false,
        lastGWSAccess: false,
        syncLastTabSettings: (update) => {
          set(update)
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
        showWorkflowsOverview: state.showWorkflowsOverview,
        selectedPresetId: state.selectedPresetId,
        useCodeExecutionMode: state.useCodeExecutionMode,
        lastSelectedSkills: state.lastSelectedSkills,
        lastSelectedSubAgents: state.lastSelectedSubAgents,
        lastBrowserMode: state.lastBrowserMode,
        lastEnableImageGeneration: state.lastEnableImageGeneration,
        lastGWSAccess: state.lastGWSAccess
        // Note: requiresNewChat is not persisted as it's temporary state
        // File context is now mode-specific: multi-agent tabs have their own, workflow uses preset
        }),
        // Drop legacy `delegationMode` persisted from v2 — multi-agent is the default now.
        version: 3,
        migrate: (persistedState: unknown, _version: number) => {
          const state = persistedState as Record<string, unknown>
          delete state.delegationMode
          if (state.showWorkflowsOverview === undefined) {
            state.showWorkflowsOverview = false
          }
          return state as unknown as AppState
        }
      }
    )
)
