import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import type { AgentMode } from './types'

export type ModeCategory = 'workflow' | 'multi-agent' | 'organization' | null

interface ModeState {
  // Core mode selection
  selectedModeCategory: ModeCategory
  hasCompletedInitialSetup: boolean

  // Preset tracking per category
  lastSelectedPreset: {
    'workflow': string | null
    'organization': string | null
  }

  // Actions
  setModeCategory: (category: ModeCategory) => void
  completeInitialSetup: () => void
  setLastPreset: (category: 'workflow' | 'organization', presetId: string | null) => void
  resetModeSelection: () => void

  // Helpers
  getModeCategoryFromAgentMode: (agentMode: string) => ModeCategory
  getAgentModeFromCategory: (category: ModeCategory) => string
}

export const useModeStore = create<ModeState>()(
    persist(
      (set, get) => {
        // Sync flag to prevent circular updates
        let isSyncing = false

        return {
          // Initial state
          selectedModeCategory: null,
          hasCompletedInitialSetup: false,
          lastSelectedPreset: {
            'workflow': null,
            'organization': null
          },

          // Actions
          setModeCategory: (category) => {
            const normalizedCategory = category
            const currentCategory = get().selectedModeCategory

            // Only update if category actually changed
            if (currentCategory === normalizedCategory) {
              return
            }

            set({ selectedModeCategory: normalizedCategory })

            // Automatically sync AppStore's agentMode when category changes
            // Use dynamic import to avoid circular dependency at module level
            if (!isSyncing) {
              isSyncing = true
              import('./useAppStore').then(({ useAppStore }) => {
                const { getAgentModeFromCategory } = get()
                const agentMode = getAgentModeFromCategory(normalizedCategory)
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
                'workflow': null,
                'organization': null
              }
            })
          },

          // Helpers
          getModeCategoryFromAgentMode: (agentMode) => {
            switch (agentMode) {
              case 'simple':
                return 'multi-agent'
              case 'workflow':
                return 'workflow'
              case 'organization':
                return 'organization'
              case 'multi-agent':
                return 'multi-agent'
              case 'organization_chat':
                return 'organization'
              default:
                return null
            }
          },

          getAgentModeFromCategory: (category) => {
            switch (category) {
              case 'workflow':
                return 'workflow'
              case 'organization':
                return 'organization_chat'
              case 'multi-agent':
                return 'simple'
              default:
                return 'simple'
            }
          }
        }
      },
      {
        name: 'mode-store',
        version: 2,
        partialize: (state) => ({
          selectedModeCategory: state.selectedModeCategory,
          hasCompletedInitialSetup: state.hasCompletedInitialSetup,
          lastSelectedPreset: state.lastSelectedPreset
        }),
        migrate: (persistedState: unknown) => {
          const state = persistedState as ModeState
          return state
        }
      }
    )
)
