import { create } from 'zustand'
import { persist } from 'zustand/middleware'

interface ActivePresetState {
  // Active preset query IDs per mode category
  activePresetQueryIds: {
    'deep-research': string | null
    'workflow': string | null
  }
  
  // Actions
  setActivePresetQueryId: (category: 'deep-research' | 'workflow', presetQueryId: string | null) => void
  getActivePresetQueryId: (category: 'deep-research' | 'workflow') => string | null
  clearActivePresetQueryId: (category: 'deep-research' | 'workflow') => void
}

export const useActivePresetStore = create<ActivePresetState>()(
  persist(
    (set, get) => ({
      activePresetQueryIds: {
        'deep-research': null,
        'workflow': null
      },

      setActivePresetQueryId: (category, presetQueryId) => {
        set((state) => ({
          activePresetQueryIds: {
            ...state.activePresetQueryIds,
            [category]: presetQueryId
          }
        }))
      },

      getActivePresetQueryId: (category) => {
        return get().activePresetQueryIds[category]
      },

      clearActivePresetQueryId: (category) => {
        set((state) => ({
          activePresetQueryIds: {
            ...state.activePresetQueryIds,
            [category]: null
          }
        }))
      }
    }),
    {
      name: 'active-preset-query-storage',
      // Only persist the active preset query IDs
      partialize: (state) => ({ 
        activePresetQueryIds: state.activePresetQueryIds
      })
    }
  )
)
