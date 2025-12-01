import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import { devtools } from 'zustand/middleware'
import type { SavedLLMConfig, LLMProvider } from '../services/api-types'

// ═══════════════════════════════════════════════════════════════════
// SAVED LLM CONFIGS STORE
// Manages named LLM configuration presets stored in localStorage
// ═══════════════════════════════════════════════════════════════════

interface SavedLLMConfigsState {
  // All saved configurations
  configs: SavedLLMConfig[]
  
  // Currently selected primary config ID
  primaryConfigId: string | null
  
  // Fallback config IDs in priority order
  fallbackConfigIds: string[]
  
  // Loading state
  isLoading: boolean
  
  // ─────────────────────────────────────────────────────────────────
  // CRUD Operations
  // ─────────────────────────────────────────────────────────────────
  
  // Create a new saved config
  addConfig: (config: Omit<SavedLLMConfig, 'id' | 'created_at' | 'updated_at'>) => SavedLLMConfig
  
  // Update an existing config
  updateConfig: (id: string, updates: Partial<Omit<SavedLLMConfig, 'id' | 'created_at' | 'updated_at'>>) => void
  
  // Delete a config
  deleteConfig: (id: string) => void
  
  // Get a config by ID
  getConfig: (id: string) => SavedLLMConfig | undefined
  
  // Get all configs for a specific provider
  getConfigsByProvider: (provider: LLMProvider) => SavedLLMConfig[]
  
  // ─────────────────────────────────────────────────────────────────
  // Selection Operations
  // ─────────────────────────────────────────────────────────────────
  
  // Set the primary config
  setPrimaryConfigId: (id: string | null) => void
  
  // Set fallback configs (in priority order)
  setFallbackConfigIds: (ids: string[]) => void
  
  // Add a fallback config
  addFallbackConfigId: (id: string) => void
  
  // Remove a fallback config
  removeFallbackConfigId: (id: string) => void
  
  // Reorder fallback configs
  reorderFallbackConfigIds: (newOrder: string[]) => void
  
  // ─────────────────────────────────────────────────────────────────
  // Utility Operations
  // ─────────────────────────────────────────────────────────────────
  
  // Get the resolved primary config
  getPrimaryConfig: () => SavedLLMConfig | undefined
  
  // Get resolved fallback configs in order
  getFallbackConfigs: () => SavedLLMConfig[]
  
  // Check if a config is in use (primary or fallback)
  isConfigInUse: (id: string) => boolean
  
  // Duplicate a config with new name
  duplicateConfig: (id: string, newName: string) => SavedLLMConfig | undefined
}

// Generate UUID
const generateId = (): string => {
  return crypto.randomUUID?.() || 
    'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
      const r = Math.random() * 16 | 0
      const v = c === 'x' ? r : (r & 0x3 | 0x8)
      return v.toString(16)
    })
}

export const useSavedLLMConfigsStore = create<SavedLLMConfigsState>()(
  devtools(
    persist(
      (set, get) => ({
        // ─────────────────────────────────────────────────────────────────
        // Initial State
        // ─────────────────────────────────────────────────────────────────
        configs: [],
        primaryConfigId: null,
        fallbackConfigIds: [],
        isLoading: false,
        
        // ─────────────────────────────────────────────────────────────────
        // CRUD Operations
        // ─────────────────────────────────────────────────────────────────
        
        addConfig: (configData) => {
          const now = new Date().toISOString()
          const newConfig: SavedLLMConfig = {
            id: generateId(),
            name: configData.name,
            provider: configData.provider,
            model_id: configData.model_id,
            options: configData.options,
            created_at: now,
            updated_at: now
          }
          
          set((state) => ({
            configs: [...state.configs, newConfig]
          }))
          
          return newConfig
        },
        
        updateConfig: (id, updates) => {
          set((state) => ({
            configs: state.configs.map((config) =>
              config.id === id
                ? {
                    ...config,
                    ...updates,
                    updated_at: new Date().toISOString()
                  }
                : config
            )
          }))
        },
        
        deleteConfig: (id) => {
          // Remove from configs
          set((state) => ({
            configs: state.configs.filter((config) => config.id !== id),
            // Clear primary if it was this config
            primaryConfigId: state.primaryConfigId === id ? null : state.primaryConfigId,
            // Remove from fallbacks
            fallbackConfigIds: state.fallbackConfigIds.filter((fid) => fid !== id)
          }))
        },
        
        getConfig: (id) => {
          return get().configs.find((config) => config.id === id)
        },
        
        getConfigsByProvider: (provider) => {
          return get().configs.filter((config) => config.provider === provider)
        },
        
        // ─────────────────────────────────────────────────────────────────
        // Selection Operations
        // ─────────────────────────────────────────────────────────────────
        
        setPrimaryConfigId: (id) => {
          set({ primaryConfigId: id })
        },
        
        setFallbackConfigIds: (ids) => {
          set({ fallbackConfigIds: ids })
        },
        
        addFallbackConfigId: (id) => {
          const state = get()
          // Don't add if already in fallbacks or is primary
          if (state.fallbackConfigIds.includes(id) || state.primaryConfigId === id) {
            return
          }
          set((state) => ({
            fallbackConfigIds: [...state.fallbackConfigIds, id]
          }))
        },
        
        removeFallbackConfigId: (id) => {
          set((state) => ({
            fallbackConfigIds: state.fallbackConfigIds.filter((fid) => fid !== id)
          }))
        },
        
        reorderFallbackConfigIds: (newOrder) => {
          set({ fallbackConfigIds: newOrder })
        },
        
        // ─────────────────────────────────────────────────────────────────
        // Utility Operations
        // ─────────────────────────────────────────────────────────────────
        
        getPrimaryConfig: () => {
          const state = get()
          if (!state.primaryConfigId) return undefined
          return state.configs.find((config) => config.id === state.primaryConfigId)
        },
        
        getFallbackConfigs: () => {
          const state = get()
          return state.fallbackConfigIds
            .map((id) => state.configs.find((config) => config.id === id))
            .filter((config): config is SavedLLMConfig => config !== undefined)
        },
        
        isConfigInUse: (id) => {
          const state = get()
          return state.primaryConfigId === id || state.fallbackConfigIds.includes(id)
        },
        
        duplicateConfig: (id, newName) => {
          const original = get().getConfig(id)
          if (!original) return undefined
          
          return get().addConfig({
            name: newName,
            provider: original.provider,
            model_id: original.model_id,
            options: original.options ? { ...original.options } : undefined
          })
        }
      }),
      {
        name: 'saved-llm-configs-storage',
        version: 1,
        // Only persist these fields
        partialize: (state) => ({
          configs: state.configs,
          primaryConfigId: state.primaryConfigId,
          fallbackConfigIds: state.fallbackConfigIds
        })
      }
    ),
    { name: 'SavedLLMConfigsStore' }
  )
)

// ═══════════════════════════════════════════════════════════════════
// Selector Hooks for common access patterns
// ═══════════════════════════════════════════════════════════════════

export const useSavedConfigs = () => useSavedLLMConfigsStore((state) => state.configs)
export const usePrimaryConfigId = () => useSavedLLMConfigsStore((state) => state.primaryConfigId)
export const useFallbackConfigIds = () => useSavedLLMConfigsStore((state) => state.fallbackConfigIds)

