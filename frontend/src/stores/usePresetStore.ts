import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import type { PlannerFile } from '../services/api-types'

export interface FolderPreset {
  id: string
  name: string
  description?: string
  category: 'workflow' | 'orchestrator'
  folders: PlannerFile[]
  createdAt: Date
  updatedAt: Date
}

interface PresetState {
  presets: FolderPreset[]
  selectedPreset: FolderPreset | null
  
  // Active preset tracking per category
  activePreset: {
    'deep-research': string | null
    'workflow': string | null
  }
  
  // Actions
  createPreset: (preset: Omit<FolderPreset, 'id' | 'createdAt' | 'updatedAt'>) => void
  updatePreset: (id: string, updates: Partial<Omit<FolderPreset, 'id' | 'createdAt' | 'updatedAt'>>) => void
  deletePreset: (id: string) => void
  selectPreset: (id: string | null) => void
  getPresetsByCategory: (category: 'workflow' | 'orchestrator') => FolderPreset[]
  getPresetById: (id: string) => FolderPreset | undefined
  
  // Active preset management
  setActivePreset: (category: 'deep-research' | 'workflow', presetId: string | null) => void
  getActivePreset: (category: 'deep-research' | 'workflow') => FolderPreset | null
}

export const usePresetStore = create<PresetState>()(
  persist(
    (set, get) => ({
      presets: [],
      selectedPreset: null,
      activePreset: {
        'deep-research': null,
        'workflow': null
      },

      createPreset: (presetData) => {
        const newPreset: FolderPreset = {
          ...presetData,
          id: `preset_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`,
          createdAt: new Date(),
          updatedAt: new Date()
        }
        
        set((state) => ({
          presets: [...state.presets, newPreset],
          selectedPreset: newPreset
        }))
      },

      updatePreset: (id, updates) => {
        set((state) => ({
          presets: state.presets.map(preset =>
            preset.id === id
              ? { ...preset, ...updates, updatedAt: new Date() }
              : preset
          ),
          selectedPreset: state.selectedPreset?.id === id
            ? { ...state.selectedPreset, ...updates, updatedAt: new Date() }
            : state.selectedPreset
        }))
      },

      deletePreset: (id) => {
        set((state) => ({
          presets: state.presets.filter(preset => preset.id !== id),
          selectedPreset: state.selectedPreset?.id === id ? null : state.selectedPreset
        }))
      },

      selectPreset: (id) => {
        const preset = id ? get().presets.find(p => p.id === id) : null
        set({ selectedPreset: preset || null })
      },

      getPresetsByCategory: (category) => {
        return get().presets.filter(preset => preset.category === category)
      },

      getPresetById: (id) => {
        return get().presets.find(preset => preset.id === id)
      },

      // Active preset management
      setActivePreset: (category, presetId) => {
        set((state) => ({
          activePreset: {
            ...state.activePreset,
            [category]: presetId
          }
        }))
      },

      getActivePreset: (category) => {
        const state = get()
        const presetId = state.activePreset[category]
        return presetId ? state.presets.find(p => p.id === presetId) || null : null
      }
    }),
    {
      name: 'folder-presets-storage',
      // Only persist presets and active presets, not selectedPreset (which is temporary state)
      partialize: (state) => ({ 
        presets: state.presets,
        activePreset: state.activePreset
      })
    }
  )
)
