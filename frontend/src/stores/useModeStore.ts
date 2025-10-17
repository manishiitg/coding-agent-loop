import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import { devtools } from 'zustand/middleware'

export type ModeCategory = 'chat' | 'deep-research' | 'workflow' | null

interface ModeState {
  // Core mode selection
  selectedModeCategory: ModeCategory
  hasCompletedInitialSetup: boolean
  
  // Preset tracking per category
  lastSelectedPreset: {
    'deep-research': string | null
    'workflow': string | null
  }
  
  // Actions
  setModeCategory: (category: ModeCategory) => void
  completeInitialSetup: () => void
  setLastPreset: (category: 'deep-research' | 'workflow', presetId: string | null) => void
  resetModeSelection: () => void
  
  // Helpers
  getModeCategoryFromAgentMode: (agentMode: string) => ModeCategory
  getAgentModeFromCategory: (category: ModeCategory) => string
}

export const useModeStore = create<ModeState>()(
  devtools(
    persist(
      (set) => ({
        // Initial state
        selectedModeCategory: null,
        hasCompletedInitialSetup: false,
        lastSelectedPreset: {
          'deep-research': null,
          'workflow': null
        },

        // Actions
        setModeCategory: (category) => {
          set({ selectedModeCategory: category })
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
              'deep-research': null,
              'workflow': null
            }
          })
        },

        // Helpers
        getModeCategoryFromAgentMode: (agentMode) => {
          switch (agentMode) {
            case 'simple':
            case 'ReAct':
              return 'chat'
            case 'orchestrator':
              return 'deep-research'
            case 'workflow':
              return 'workflow'
            default:
              return null
          }
        },

        getAgentModeFromCategory: (category) => {
          switch (category) {
            case 'chat':
              return 'ReAct' // Default to ReAct for chat mode
            case 'deep-research':
              return 'orchestrator'
            case 'workflow':
              return 'workflow'
            default:
              return 'ReAct'
          }
        }
      }),
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
