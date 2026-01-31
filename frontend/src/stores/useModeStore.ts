import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import { devtools } from 'zustand/middleware'
import type { AgentMode } from './types'

export type ModeCategory = 'chat' | 'workflow' | 'skill_builder' | null

interface ModeState {
  // Core mode selection
  selectedModeCategory: ModeCategory
  hasCompletedInitialSetup: boolean
  
  // Preset tracking per category
  lastSelectedPreset: {
    'workflow': string | null
  }
  
  // Actions
  setModeCategory: (category: ModeCategory) => void
  completeInitialSetup: () => void
  setLastPreset: (category: 'workflow', presetId: string | null) => void
  resetModeSelection: () => void
  
  // Helpers
  getModeCategoryFromAgentMode: (agentMode: string) => ModeCategory
  getAgentModeFromCategory: (category: ModeCategory) => string
}

export const useModeStore = create<ModeState>()(
  devtools(
    persist(
      (set, get) => {
        // Sync flag to prevent circular updates
        let isSyncing = false
        
        return {
          // Initial state
          selectedModeCategory: null,
          hasCompletedInitialSetup: false,
          lastSelectedPreset: {
            'workflow': null
          },

          // Actions
          setModeCategory: (category) => {
            const currentCategory = get().selectedModeCategory
            
            // Only update if category actually changed
            if (currentCategory === category) {
              return
            }
            
            set({ selectedModeCategory: category })
            
            // Automatically sync AppStore's agentMode when category changes
            // Use dynamic import to avoid circular dependency at module level
            if (!isSyncing) {
              isSyncing = true
              import('./useAppStore').then(({ useAppStore }) => {
                const { getAgentModeFromCategory } = get()
                const agentMode = getAgentModeFromCategory(category)
                const appStore = useAppStore.getState()
                
                // Only update if agentMode would be different
                if (appStore.agentMode !== agentMode) {
              // Call setAgentMode to sync AppStore
              appStore.setAgentMode(agentMode as AgentMode)
                }
                isSyncing = false
              }).catch(() => {
                isSyncing = false
              })
            }
          },

          completeInitialSetup: () => {
            set({ hasCompletedInitialSetup: true })
          },

          setLastPreset: (category, presetId) => {
            set((state) => ({
              lastSelectedPreset: {
                ...state.lastSelectedPreset,
                [category]: presetId
              }
            }))
          },

          resetModeSelection: () => {
            set({
              selectedModeCategory: null,
              hasCompletedInitialSetup: false,
              lastSelectedPreset: {
                'workflow': null
              }
            })
          },

          // Helpers
          getModeCategoryFromAgentMode: (agentMode) => {
            switch (agentMode) {
              case 'simple':
                return 'chat'
              case 'workflow':
                return 'workflow'
              case 'skill_builder':
                return 'skill_builder'
              default:
                return null
            }
          },

          getAgentModeFromCategory: (category) => {
            switch (category) {
              case 'chat':
                return 'simple' // Default to simple for chat mode
              case 'workflow':
                return 'workflow'
              case 'skill_builder':
                return 'skill_builder'
              default:
                return 'simple'
            }
          }
        }
      },
      {
        name: 'mode-store',
        partialize: (state) => ({
          selectedModeCategory: state.selectedModeCategory,
          hasCompletedInitialSetup: state.hasCompletedInitialSetup,
          lastSelectedPreset: state.lastSelectedPreset
        })
      }
    ),
    {
      name: 'mode-store'
    }
  )
)
