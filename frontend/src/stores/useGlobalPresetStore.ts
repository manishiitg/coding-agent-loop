import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import { agentApi } from '../services/api'
import type { PlannerFile, PresetQuery } from '../services/api-types'
import type { CustomPreset, PredefinedPreset } from '../types/preset'
import { useAppStore } from './useAppStore'
import { useWorkspaceStore } from './useWorkspaceStore'
import { useChatStore } from './useChatStore'

export interface PresetApplicationResult {
  success: boolean
  preset?: CustomPreset | PredefinedPreset
  error?: string
}

interface GlobalPresetState {
  // Database presets
  customPresets: CustomPreset[]
  predefinedPresets: PredefinedPreset[]
  predefinedServerSelections: Record<string, string[]>
  loading: boolean
  error: string | null
  
  // Active preset tracking per mode category
  activePresetIds: {
    'chat': string | null
    'deep-research': string | null
    'workflow': string | null
  }
  
  // Current preset application state
  currentPresetServers: string[]
  selectedPresetFolder: string | null
  currentQuery: string
  
  // Actions for database management
  refreshPresets: () => Promise<void>
  addPreset: (label: string, query: string, selectedServers?: string[], agentMode?: 'simple' | 'ReAct' | 'orchestrator' | 'workflow', selectedFolder?: PlannerFile) => Promise<CustomPreset | null>
  updatePreset: (id: string, label: string, query: string, selectedServers?: string[], agentMode?: 'simple' | 'ReAct' | 'orchestrator' | 'workflow', selectedFolder?: PlannerFile) => Promise<void>
  deletePreset: (id: string) => Promise<void>
  updatePredefinedServerSelection: (presetLabel: string, selectedServers: string[]) => void
  
  // Actions for preset application
  applyPreset: (preset: CustomPreset | PredefinedPreset, modeCategory: 'chat' | 'deep-research' | 'workflow') => PresetApplicationResult
  setActivePresetId: (modeCategory: 'chat' | 'deep-research' | 'workflow', presetId: string | null) => void
  getActivePresetId: (modeCategory: 'chat' | 'deep-research' | 'workflow') => string | null
  getActivePreset: (modeCategory: 'chat' | 'deep-research' | 'workflow') => CustomPreset | PredefinedPreset | null
  
  // Actions for current state management
  setCurrentPresetServers: (servers: string[]) => void
  setSelectedPresetFolder: (folderPath: string | null) => void
  setCurrentQuery: (query: string) => void
  clearPresetState: () => void
  
  // Helper actions
  getPresetsForMode: (modeCategory: 'chat' | 'deep-research' | 'workflow') => (CustomPreset | PredefinedPreset)[]
  isPresetActive: (presetId: string, modeCategory: 'chat' | 'deep-research' | 'workflow') => boolean
}

export const useGlobalPresetStore = create<GlobalPresetState>()(
  persist(
    (set, get) => ({
      // Initial state
      customPresets: [],
      predefinedPresets: [],
      predefinedServerSelections: {},
      loading: false,
      error: null,
      
      activePresetIds: {
        'chat': null,
        'deep-research': null,
        'workflow': null
      },
      
      currentPresetServers: [],
      selectedPresetFolder: null,
      currentQuery: '',
      
      // Database management actions
      refreshPresets: async () => {
        set({ loading: true, error: null })
        try {
          const response = await agentApi.getPresetQueries()
          
          // Filter custom and predefined presets from the same response
          const customPresets: CustomPreset[] = response.presets
            .filter(preset => !preset.is_predefined)
            .map((preset: PresetQuery) => {
            let selectedServers: string[] = []
            let selectedFolder: PlannerFile | undefined
            
            try {
              if (preset.selected_servers) {
                selectedServers = JSON.parse(preset.selected_servers)
              }
            } catch (error) {
              console.error('[PRESET] Error parsing selected servers:', error)
            }
            
            if (preset.selected_folder) {
              selectedFolder = {
                filepath: preset.selected_folder,
                content: '',
                last_modified: '',
                type: 'folder' as const,
                children: []
              }
            }
            
            return {
              id: preset.id,
              label: preset.label,
              query: preset.query,
              createdAt: new Date(preset.created_at).getTime(),
              selectedServers,
              agentMode: preset.agent_mode as 'simple' | 'ReAct' | 'orchestrator' | 'workflow' | undefined,
              selectedFolder
            }
          })
          
          // Convert predefined presets
          const predefinedPresets: PredefinedPreset[] = response.presets
            .filter(preset => preset.is_predefined)
            .map((preset: PresetQuery) => ({
              id: preset.id,
              label: preset.label,
              query: preset.query,
              selectedServers: [],
              agentMode: preset.agent_mode as 'simple' | 'ReAct' | 'orchestrator' | 'workflow' | undefined,
              selectedFolder: preset.selected_folder ? {
                filepath: preset.selected_folder,
                content: '',
                last_modified: '',
                type: 'folder' as const,
                children: []
              } : undefined
            }))
          
          set({ 
            customPresets, 
            predefinedPresets, 
            loading: false 
          })
        } catch (error) {
          console.error('[PRESET] Error refreshing presets:', error)
          set({ 
            error: error instanceof Error ? error.message : 'Failed to refresh presets',
            loading: false 
          })
        }
      },
      
      addPreset: async (label, query, selectedServers, agentMode, selectedFolder) => {
        try {
          const request = {
            label,
            query,
            selected_servers: selectedServers,
            agent_mode: agentMode,
            selected_folder: selectedFolder?.filepath
          }
          
          const response = await agentApi.createPresetQuery(request)
          
          const newPreset: CustomPreset = {
            id: response.id,
            label: response.label,
            query: response.query,
            createdAt: new Date(response.created_at).getTime(),
            selectedServers,
            agentMode,
            selectedFolder
          }
          
          set(state => ({
            customPresets: [...state.customPresets, newPreset]
          }))
          
          return newPreset
        } catch (error) {
          console.error('[PRESET] Error adding preset:', error)
          throw error
        }
      },
      
      updatePreset: async (id, label, query, selectedServers, agentMode, selectedFolder) => {
        try {
          const request = {
            label,
            query,
            selected_servers: selectedServers,
            agent_mode: agentMode,
            selected_folder: selectedFolder?.filepath
          }
          
          await agentApi.updatePresetQuery(id, request)
          
          set(state => ({
            customPresets: state.customPresets.map(preset =>
              preset.id === id
                ? {
                    ...preset,
                    label,
                    query,
                    selectedServers,
                    agentMode,
                    selectedFolder
                  }
                : preset
            )
          }))
        } catch (error) {
          console.error('[PRESET] Error updating preset:', error)
          throw error
        }
      },
      
      deletePreset: async (id) => {
        try {
          await agentApi.deletePresetQuery(id)
          
          set(state => ({
            customPresets: state.customPresets.filter(preset => preset.id !== id),
            activePresetIds: {
              chat: state.activePresetIds.chat === id ? null : state.activePresetIds.chat,
              'deep-research': state.activePresetIds['deep-research'] === id ? null : state.activePresetIds['deep-research'],
              workflow: state.activePresetIds.workflow === id ? null : state.activePresetIds.workflow
            }
          }))
        } catch (error) {
          console.error('[PRESET] Error deleting preset:', error)
          throw error
        }
      },
      
      updatePredefinedServerSelection: (presetLabel, selectedServers) => {
        set(state => ({
          predefinedServerSelections: {
            ...state.predefinedServerSelections,
            [presetLabel]: selectedServers
          }
        }))
      },
      
      // Preset application actions
      applyPreset: (preset, modeCategory) => {
        console.log('[GlobalPresetStore] applyPreset called with:', { preset, modeCategory })
        try {
          // Clear chatSessionId to allow fresh observer initialization
          useAppStore.getState().setChatSessionId('')
          console.log('[GlobalPresetStore] Cleared chatSessionId for fresh observer initialization')
          
          // Clear only the observer ID, not the entire chat state
          const { setObserverId } = useChatStore.getState()
          setObserverId('')
          console.log('[GlobalPresetStore] Cleared observerId for fresh observer')
          
          // The ChatArea component will detect the empty observerId and initialize a new one
          console.log('[GlobalPresetStore] Observer will be re-initialized by ChatArea component')
          
          // Set the current query in both stores
          set({ currentQuery: preset.query })
          
          // Also update the AppStore's currentQuery for ChatInput/ChatArea components
          useAppStore.getState().setCurrentQuery(preset.query)
          console.log('[GlobalPresetStore] Set currentQuery in both stores:', preset.query)
          
          // Set server selection
          const servers = preset.selectedServers || []
          set({ currentPresetServers: servers })
          console.log('[GlobalPresetStore] Set currentPresetServers:', servers)
          
          // Set folder selection
          const folderPath = preset.selectedFolder?.filepath || null
          set({ selectedPresetFolder: folderPath })
          console.log('[GlobalPresetStore] Set selectedPresetFolder:', folderPath)
          
          // Handle workspace folder selection
          if (folderPath) {
            // Clear any previously selected file in workspace
            useAppStore.getState().setSelectedFile(null)
            
            // Clear existing file context to avoid duplicates
            useAppStore.getState().clearFileContext()
            console.log('[GlobalPresetStore] Cleared existing file context')
            
            // Select the preset folder in workspace
            useAppStore.getState().setSelectedFile({
              name: folderPath.split('/').pop() || folderPath,
              path: folderPath
            })
            
            // Clear file content view to show folder structure
            useAppStore.getState().setShowFileContent(false)
            
            // Expand the folder to show its contents
            const { expandFoldersForFile } = useWorkspaceStore.getState()
            console.log('[GlobalPresetStore] Expanding folder:', folderPath)
            expandFoldersForFile(folderPath)
            console.log('[GlobalPresetStore] Folder expansion called for:', folderPath)
            
            // Add the folder to chat context for AI processing
            const folderName = folderPath.split('/').pop() || folderPath
            useAppStore.getState().addFileToContext({
              name: folderName,
              path: folderPath,
              type: 'folder'
            })
            
            console.log('[GlobalPresetStore] Selected, expanded, and added folder to chat context:', folderPath)
          } else {
            // Clear workspace selection and file context if no folder
            useAppStore.getState().setSelectedFile(null)
            useAppStore.getState().clearFileContext()
            console.log('[GlobalPresetStore] Cleared workspace selection and file context')
          }
          
          // Set active preset ID
          set(state => ({
            activePresetIds: {
              ...state.activePresetIds,
              [modeCategory]: preset.id
            }
          }))
          console.log('[GlobalPresetStore] Set activePresetId for', modeCategory, 'to:', preset.id)
          
          return {
            success: true,
            preset
          }
        } catch (error) {
          console.error('[PRESET] Error applying preset:', error)
          return {
            success: false,
            error: error instanceof Error ? error.message : 'Failed to apply preset'
          }
        }
      },
      
      setActivePresetId: (modeCategory, presetId) => {
        set(state => ({
          activePresetIds: {
            ...state.activePresetIds,
            [modeCategory]: presetId
          }
        }))
      },
      
      getActivePresetId: (modeCategory) => {
        return get().activePresetIds[modeCategory]
      },
      
      getActivePreset: (modeCategory) => {
        const state = get()
        const presetId = state.activePresetIds[modeCategory]
        
        if (!presetId) return null
        
        // Check custom presets first
        const customPreset = state.customPresets.find(p => p.id === presetId)
        if (customPreset) return customPreset
        
        // Check predefined presets
        const predefinedPreset = state.predefinedPresets.find(p => p.id === presetId)
        if (predefinedPreset) return predefinedPreset
        
        return null
      },
      
      // Current state management
      setCurrentPresetServers: (servers) => {
        set({ currentPresetServers: servers })
      },
      
      setSelectedPresetFolder: (folderPath) => {
        set({ selectedPresetFolder: folderPath })
      },
      
      setCurrentQuery: (query) => {
        set({ currentQuery: query })
      },
      
      clearPresetState: () => {
        set({
          currentPresetServers: [],
          selectedPresetFolder: null,
          currentQuery: '',
          activePresetIds: {
            'chat': null,
            'deep-research': null,
            'workflow': null
          }
        })
      },
      
      // Helper actions
      getPresetsForMode: (modeCategory) => {
        const state = get()
        const allPresets = [...state.customPresets, ...state.predefinedPresets]
        
        return allPresets.filter(preset => {
          if (modeCategory === 'chat') {
            return preset.agentMode === 'simple' || preset.agentMode === 'ReAct'
          } else if (modeCategory === 'deep-research') {
            return preset.agentMode === 'orchestrator'
          } else if (modeCategory === 'workflow') {
            return preset.agentMode === 'workflow'
          }
          return false
        })
      },
      
      isPresetActive: (presetId, modeCategory) => {
        return get().activePresetIds[modeCategory] === presetId
      }
    }),
    {
      name: 'global-preset-storage',
      // Only persist the essential state, not temporary UI state
      partialize: (state) => ({
        customPresets: state.customPresets,
        predefinedPresets: state.predefinedPresets,
        predefinedServerSelections: state.predefinedServerSelections,
        activePresetIds: state.activePresetIds,
        currentPresetServers: state.currentPresetServers,
        selectedPresetFolder: state.selectedPresetFolder,
        currentQuery: state.currentQuery
      })
    }
  )
)

// Export convenience hooks for specific functionality
export const usePresetApplication = () => {
  const store = useGlobalPresetStore()
  return {
    applyPreset: store.applyPreset,
    setActivePresetId: store.setActivePresetId,
    getActivePresetId: store.getActivePresetId,
    getActivePreset: store.getActivePreset,
    clearPresetState: store.clearPresetState,
    isPresetActive: store.isPresetActive,
    getPresetsForMode: store.getPresetsForMode,
    currentPresetServers: store.currentPresetServers
  }
}

export const usePresetManagement = () => {
  const store = useGlobalPresetStore()
  return {
    customPresets: store.customPresets,
    predefinedPresets: store.predefinedPresets,
    predefinedServerSelections: store.predefinedServerSelections,
    loading: store.loading,
    error: store.error,
    refreshPresets: store.refreshPresets,
    addPreset: store.addPreset,
    updatePreset: store.updatePreset,
    deletePreset: store.deletePreset,
    updatePredefinedServerSelection: store.updatePredefinedServerSelection
  }
}

export const usePresetState = () => {
  const store = useGlobalPresetStore()
  return {
    currentPresetServers: store.currentPresetServers,
    selectedPresetFolder: store.selectedPresetFolder,
    currentQuery: store.currentQuery,
    setCurrentPresetServers: store.setCurrentPresetServers,
    setSelectedPresetFolder: store.setSelectedPresetFolder,
    setCurrentQuery: store.setCurrentQuery
  }
}
