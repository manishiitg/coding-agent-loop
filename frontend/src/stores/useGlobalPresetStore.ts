import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import { agentApi } from '../services/api'
import type { PlannerFile, PresetQuery, PresetLLMConfig, CreatePresetQueryRequest, UpdatePresetQueryRequest } from '../services/api-types'
import type { CustomPreset, PredefinedPreset } from '../types/preset'
import { useAppStore } from './useAppStore'
import { useWorkspaceStore } from './useWorkspaceStore'
import { useChatStore } from './useChatStore'
import { useMCPStore } from './useMCPStore'
import { useLLMStore } from './useLLMStore'

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
  currentPresetTools: string[] // Array of "server:tool" strings
  selectedPresetFolder: string | null
  currentQuery: string
  
  // Actions for database management
  refreshPresets: () => Promise<void>
  addPreset: (label: string, query: string, selectedServers?: string[], selectedTools?: string[], agentMode?: 'simple' | 'ReAct' | 'orchestrator' | 'workflow', selectedFolder?: PlannerFile, llmConfig?: PresetLLMConfig) => Promise<CustomPreset | null>
  updatePreset: (id: string, label: string, query: string, selectedServers?: string[], selectedTools?: string[], agentMode?: 'simple' | 'ReAct' | 'orchestrator' | 'workflow', selectedFolder?: PlannerFile, llmConfig?: PresetLLMConfig) => Promise<void>
  deletePreset: (id: string) => Promise<void>
  updatePredefinedServerSelection: (presetId: string, selectedServers: string[]) => void
  
  // Actions for preset application
  applyPreset: (presetOrId: CustomPreset | PredefinedPreset | string, modeCategory: 'chat' | 'deep-research' | 'workflow') => PresetApplicationResult
  clearActivePreset: (modeCategory: 'chat' | 'deep-research' | 'workflow') => void
  getActivePreset: (modeCategory: 'chat' | 'deep-research' | 'workflow') => CustomPreset | PredefinedPreset | null
  
  // Actions for current state management
  setCurrentPresetServers: (servers: string[]) => void
  setCurrentPresetTools: (tools: string[]) => void
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
      currentPresetTools: [],
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
            let selectedTools: string[] = []
            let selectedFolder: PlannerFile | undefined
            
            try {
              if (preset.selected_servers) {
                selectedServers = JSON.parse(preset.selected_servers)
              }
            } catch (error) {
              console.error('[PRESET] Error parsing selected servers:', error)
            }
            
            try {
              if (preset.selected_tools) {
                selectedTools = JSON.parse(preset.selected_tools)
              }
            } catch (error) {
              console.error('[PRESET] Error parsing selected tools:', error)
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
            
            // Parse LLM config safely
            let llmConfig: { provider: 'openrouter' | 'bedrock' | 'openai'; model_id: string } | undefined
            try {
              if (preset.llm_config) {
                if (typeof preset.llm_config === 'string') {
                  llmConfig = JSON.parse(preset.llm_config)
                } else {
                  llmConfig = preset.llm_config
                }
              }
            } catch (error) {
              console.error('[PRESET] Error parsing LLM config:', error)
              llmConfig = undefined
            }
            
            return {
              id: preset.id,
              label: preset.label,
              query: preset.query,
              createdAt: new Date(preset.created_at).getTime(),
              selectedServers,
              selectedTools, // NEW
              agentMode: preset.agent_mode as 'simple' | 'ReAct' | 'orchestrator' | 'workflow' | undefined,
              selectedFolder,
              llmConfig
            }
          })
          
          // Convert predefined presets
          const predefinedPresets: PredefinedPreset[] = response.presets
            .filter(preset => preset.is_predefined)
            .map((preset: PresetQuery) => {
              // Parse LLM config safely
              let llmConfig: { provider: 'openrouter' | 'bedrock' | 'openai'; model_id: string } | undefined
              try {
                if (preset.llm_config) {
                  if (typeof preset.llm_config === 'string') {
                    llmConfig = JSON.parse(preset.llm_config)
                  } else {
                    llmConfig = preset.llm_config
                  }
                }
              } catch (error) {
                console.error('[PRESET] Error parsing LLM config:', error)
                llmConfig = undefined
              }
              
              return {
                id: preset.id,
                label: preset.label,
                query: preset.query,
                selectedServers: [],
                selectedTools: [], // NEW: Predefined presets don't have custom tool selection
                agentMode: preset.agent_mode as 'simple' | 'ReAct' | 'orchestrator' | 'workflow' | undefined,
                selectedFolder: preset.selected_folder ? {
                  filepath: preset.selected_folder,
                  content: '',
                  last_modified: '',
                  type: 'folder' as const,
                  children: []
                } : undefined,
                llmConfig
              }
            })
          
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
      
      addPreset: async (label, query, selectedServers, selectedTools, agentMode, selectedFolder, llmConfig) => {
        try {
          const request: CreatePresetQueryRequest = {
            label,
            query,
            selected_servers: selectedServers,
            selected_tools: selectedTools, // NEW
            agent_mode: agentMode,
            selected_folder: selectedFolder?.filepath
          }
          
          // Include LLM config if provided
          if (llmConfig) {
            request.llm_config = llmConfig
          }
          
          console.log('[PRESET] Creating preset with request:', request)
          
          const response = await agentApi.createPresetQuery(request)
          
          const newPreset: CustomPreset = {
            id: response.id,
            label: response.label,
            query: response.query,
            createdAt: new Date(response.created_at).getTime(),
            selectedServers,
            selectedTools, // NEW
            agentMode,
            selectedFolder,
            llmConfig
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
      
      updatePreset: async (id, label, query, selectedServers, selectedTools, agentMode, selectedFolder, llmConfig) => {
        try {
          const request: UpdatePresetQueryRequest = {
            label,
            query,
            selected_servers: selectedServers,
            selected_tools: selectedTools, // NEW
            agent_mode: agentMode,
            selected_folder: selectedFolder?.filepath
          }
          
          // Include LLM config if provided
          if (llmConfig) {
            request.llm_config = llmConfig
          }
          
          console.log('[PRESET] Updating preset with request:', request)
          
          await agentApi.updatePresetQuery(id, request)
          
          set(state => ({
            customPresets: state.customPresets.map(preset =>
              preset.id === id
                ? {
                    ...preset,
                    label,
                    query,
                    selectedServers,
                    selectedTools, // NEW
                    agentMode,
                    selectedFolder,
                    llmConfig
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
      
      updatePredefinedServerSelection: (presetId, selectedServers) => {
        set(state => ({
          predefinedServerSelections: {
            ...state.predefinedServerSelections,
            [presetId]: selectedServers
          }
        }))
      },
      
      // Unified preset application function - handles both preset objects and preset IDs
      applyPreset: (presetOrId, modeCategory) => {
        try {
          let preset: CustomPreset | PredefinedPreset | null = null
          
          // Handle different input types
          if (typeof presetOrId === 'string') {
            // If string, treat as preset ID and find the preset
            const state = get()
            const customPreset = state.customPresets.find(p => p.id === presetOrId)
            const predefinedPreset = state.predefinedPresets.find(p => p.id === presetOrId)
            preset = customPreset || predefinedPreset || null
            
            if (!preset) {
              return {
                success: false,
                error: 'Preset not found'
              }
            }
          } else {
            // If object, use it directly
            preset = presetOrId as CustomPreset | PredefinedPreset
          }
          
          // Clear chatSessionId to allow fresh observer initialization
          useAppStore.getState().setChatSessionId('')
          
          // Clear only the observer ID, not the entire chat state
          const { setObserverId } = useChatStore.getState()
          setObserverId('')
          
          // Set the current query in both stores
          set({ currentQuery: preset.query })
          
          // Also update the AppStore's currentQuery for ChatInput/ChatArea components
          useAppStore.getState().setCurrentQuery(preset.query)
          
          // Set server selection (use predefined selection if not present on preset)
          const state = get()
          const servers =
            (preset.selectedServers && preset.selectedServers.length > 0)
              ? preset.selectedServers
              : (state.predefinedServerSelections[preset.id] || [])
          set({ currentPresetServers: servers })

          // Set tool selection from preset
          const tools = preset.selectedTools || []
          set({ currentPresetTools: tools })

          // Keep MCP store in sync so UI reflects selection
          try {
            const { setSelectedServers } = useMCPStore.getState()
            if (typeof setSelectedServers === 'function') {
              setSelectedServers(servers)
            }
          } catch (error) {
            console.warn('[GlobalPresetStore] Failed to sync MCP store:', error)
          }
          
          // Set folder selection
          const folderPath = preset.selectedFolder?.filepath || null
          set({ selectedPresetFolder: folderPath })
          
          // Debug: Log the entire preset object to see what's available
          console.log('[PRESET] Full preset object:', preset)
          console.log('[PRESET] Preset llmConfig:', preset.llmConfig)
          
          // Apply LLM configuration if preset has one
          if (preset.llmConfig) {
            console.log('[PRESET] Applying LLM config:', preset.llmConfig)
            const { setPrimaryConfig, primaryConfig } = useLLMStore.getState()
            setPrimaryConfig({
              ...primaryConfig, // Preserve existing configuration
              provider: preset.llmConfig.provider,
              model_id: preset.llmConfig.model_id
            })
            console.log('[PRESET] LLM config applied successfully')
          } else {
            console.log('[PRESET] No LLM config found in preset')
          }
          
          // Handle workspace folder selection
          if (folderPath) {
            // Clear any previously selected file in workspace
            useWorkspaceStore.getState().setSelectedFile(null)
            
            // Clear existing file context to avoid duplicates
            useAppStore.getState().clearFileContext()
            
            // Select the preset folder in workspace
            useWorkspaceStore.getState().setSelectedFile({
              name: folderPath.split('/').pop() || folderPath,
              path: folderPath
            })
            
            // Clear file content view to show folder structure
            useWorkspaceStore.getState().setShowFileContent(false)
            
            // Expand the folder to show its contents
            const { expandFoldersForFile } = useWorkspaceStore.getState()
            expandFoldersForFile(folderPath)
            
            // Add the folder to chat context for AI processing
            const folderName = folderPath.split('/').pop() || folderPath
            useAppStore.getState().addFileToContext({
              name: folderName,
              path: folderPath,
              type: 'folder'
            })
          } else {
            // Clear workspace selection and file context if no folder
            useWorkspaceStore.getState().setSelectedFile(null)
            useAppStore.getState().clearFileContext()
          }
          
          // Set active preset ID
          set(state => ({
            activePresetIds: {
              ...state.activePresetIds,
              [modeCategory]: preset.id
            }
          }))
          
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
      
      // Clear active preset for a mode category
      clearActivePreset: (modeCategory) => {
        set(state => ({
          activePresetIds: {
            ...state.activePresetIds,
            [modeCategory]: null
          }
        }))
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
      
      setCurrentPresetTools: (tools) => {
        set({ currentPresetTools: tools })
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
          currentPresetTools: [],
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
        currentPresetTools: state.currentPresetTools,
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
    clearActivePreset: store.clearActivePreset,
    getActivePreset: store.getActivePreset,
    clearPresetState: store.clearPresetState,
    isPresetActive: store.isPresetActive,
    getPresetsForMode: store.getPresetsForMode,
    currentPresetServers: store.currentPresetServers,
    currentPresetTools: store.currentPresetTools,
    activePresetIds: store.activePresetIds,
    customPresets: store.customPresets,
    predefinedPresets: store.predefinedPresets
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
