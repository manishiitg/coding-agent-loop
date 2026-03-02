import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import { agentApi } from '../services/api'
import type { PlannerFile, PresetQuery, PresetLLMConfig, CreatePresetQueryRequest, UpdatePresetQueryRequest } from '../services/api-types'
import type { CustomPreset, PredefinedPreset } from '../types/preset'
import { useAppStore } from './useAppStore'
import { type ModeCategory } from './useModeStore'
import { useWorkspaceStore } from './useWorkspaceStore'
import { useMCPStore } from './useMCPStore'
import { useLLMStore } from './useLLMStore'
import { useWorkflowStore } from './useWorkflowStore'
import { useSecretsStore } from './useSecretsStore'

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
  activePresetIds: Record<Exclude<ModeCategory, null>, string | null>
  
  // Current preset application state
  currentPresetServers: string[]
  currentPresetTools: string[] // Array of "server:tool" strings
  selectedPresetFolder: string | null
  currentQuery: string
  
  // Actions for database management
  refreshPresets: () => Promise<void>
  addPreset: (label: string, query?: string, selectedServers?: string[], selectedTools?: string[], selectedSkills?: string[], agentMode?: 'simple' | 'workflow', selectedFolder?: PlannerFile, llmConfig?: PresetLLMConfig, useCodeExecutionMode?: boolean, enableContextSummarization?: boolean, useToolSearchMode?: boolean, enableBrowserAccess?: boolean, enableContextEditing?: boolean, selectedSecrets?: string[]) => Promise<CustomPreset | null>
  updatePreset: (id: string, label: string, query?: string, selectedServers?: string[], selectedTools?: string[], selectedSkills?: string[], agentMode?: 'simple' | 'workflow', selectedFolder?: PlannerFile, llmConfig?: PresetLLMConfig, useCodeExecutionMode?: boolean, enableContextSummarization?: boolean, useToolSearchMode?: boolean, enableBrowserAccess?: boolean, enableContextEditing?: boolean, selectedSecrets?: string[]) => Promise<void>
  savePreset: (label: string, query?: string, selectedServers?: string[], selectedTools?: string[], selectedSkills?: string[], agentMode?: 'simple' | 'workflow', selectedFolder?: PlannerFile, llmConfig?: PresetLLMConfig, useCodeExecutionMode?: boolean, id?: string, enableContextSummarization?: boolean, useToolSearchMode?: boolean, enableBrowserAccess?: boolean, enableContextEditing?: boolean, selectedSecrets?: string[], selectedGlobalSecretNames?: string[] | null) => Promise<CustomPreset | null>
  deletePreset: (id: string) => Promise<void>
  duplicatePreset: (presetId: string) => Promise<CustomPreset | null>
  updatePredefinedServerSelection: (presetId: string, selectedServers: string[]) => void
  
  // Actions for preset application
  applyPreset: (presetOrId: CustomPreset | PredefinedPreset | string, modeCategory: Exclude<ModeCategory, null>) => PresetApplicationResult
  clearActivePreset: (modeCategory: Exclude<ModeCategory, null>) => void
  getActivePreset: (modeCategory: Exclude<ModeCategory, null>) => CustomPreset | PredefinedPreset | null
  
  // Actions for current state management
  setCurrentPresetServers: (servers: string[]) => void
  setCurrentPresetTools: (tools: string[]) => void
  setSelectedPresetFolder: (folderPath: string | null) => void
  setCurrentQuery: (query: string) => void
  clearPresetState: () => void
  setActivePreset: (modeCategory: Exclude<ModeCategory, null>, presetId: string | null) => void
  
  // Helper actions
  getPresetsForMode: (modeCategory: Exclude<ModeCategory, null>) => (CustomPreset | PredefinedPreset)[]
  isPresetActive: (presetId: string, modeCategory: Exclude<ModeCategory, null>) => boolean
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
        'workflow': null,
        'multi-agent': null,
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
          
          // Import secrets store for resolving secret names to IDs
          // useSecretsStore imported at top level
          const secretsState = useSecretsStore.getState()

          // Filter custom and predefined presets from the same response
          const customPresets: CustomPreset[] = response.presets
            .filter(preset => !preset.is_predefined)
            .map((preset: PresetQuery) => {
            let selectedServers: string[] = []
            let selectedTools: string[] = []
            let selectedSkills: string[] = []
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
                const parsedTools = JSON.parse(preset.selected_tools)
                selectedTools = parsedTools || []
              }
            } catch (error) {
              console.error('[PRESET] Error parsing selected tools:', error)
            }

            try {
              if (preset.selected_skills) {
                const parsedSkills = JSON.parse(preset.selected_skills)
                selectedSkills = parsedSkills || []
              }
            } catch (error) {
              console.error('[PRESET] Error parsing selected skills:', error)
            }

            // After both selectedServers and selectedTools are loaded, add "*" markers for servers in "all tools" mode
            // A server is in "all tools" mode if it's in selectedServers but has no specific tools
            if (selectedServers.length > 0) {
              selectedServers.forEach(server => {
                const hasSpecificTools = selectedTools.some(t => 
                  t.startsWith(`${server}:`) && !t.endsWith(':*')
                )
                if (!hasSpecificTools && !selectedTools.includes(`${server}:*`)) {
                  selectedTools.push(`${server}:*`)
                }
              })
            }
            
            // Handle selected_folder - could be string, null, or undefined
            if (preset.selected_folder && typeof preset.selected_folder === 'string') {
              selectedFolder = {
                filepath: preset.selected_folder,
                content: '',
                last_modified: '',
                type: 'folder' as const,
                children: []
              }
            }
            
            // Parse LLM config safely
            let llmConfig: PresetLLMConfig | undefined
            try {
              if (preset.llm_config) {
                if (typeof preset.llm_config === 'string') {
                  llmConfig = JSON.parse(preset.llm_config)
                } else {
                  llmConfig = preset.llm_config as unknown as PresetLLMConfig
                }
              }
            } catch (error) {
              console.error('[PRESET] Error parsing LLM config:', error)
              llmConfig = undefined
            }

            // Parse pre-discovered tools safely
            let preDiscoveredTools: string[] = []
            try {
              if (preset.pre_discovered_tools) {
                preDiscoveredTools = JSON.parse(preset.pre_discovered_tools)
              }
            } catch (error) {
              console.error('[PRESET] Error parsing pre-discovered tools:', error)
            }
            
            // Parse selected_secrets (stored as names in DB, resolve to local IDs)
            let selectedSecrets: string[] = []
            try {
              if (preset.selected_secrets) {
                const secretNames: string[] = typeof preset.selected_secrets === 'string'
                  ? JSON.parse(preset.selected_secrets)
                  : preset.selected_secrets
                if (secretNames && secretNames.length > 0) {
                  selectedSecrets = secretNames
                    .map(name => secretsState.getSecretByName(name)?.id)
                    .filter((id): id is string => !!id)
                }
              }
            } catch (error) {
              console.error('[PRESET] Error parsing selected secrets:', error)
            }

            // Parse selected_global_secret_names (null in DB = all selected)
            let selectedGlobalSecretNames: string[] | null = null
            try {
              if (preset.selected_global_secret_names) {
                const parsed = typeof preset.selected_global_secret_names === 'string'
                  ? JSON.parse(preset.selected_global_secret_names)
                  : preset.selected_global_secret_names
                if (Array.isArray(parsed)) {
                  selectedGlobalSecretNames = parsed
                }
              }
            } catch (error) {
              console.error('[PRESET] Error parsing selected global secret names:', error)
            }

            return {
              id: preset.id,
              label: preset.label,
              query: preset.query || '',
              createdAt: new Date(preset.created_at).getTime(),
              selectedServers,
              selectedTools, // NEW
              selectedSkills, // Skill folder names
              selectedSecrets, // Secret IDs resolved from backend names
              selectedGlobalSecretNames, // Per-preset global secret selection (null=all)
              agentMode: preset.agent_mode as 'simple' | 'workflow' | undefined,
              selectedFolder,
              llmConfig,
              useCodeExecutionMode: preset.use_code_execution_mode,
              useToolSearchMode: preset.use_tool_search_mode,
              preDiscoveredTools,
              enableContextSummarization: preset.enable_context_summarization !== undefined ? preset.enable_context_summarization : true,
              enableContextEditing: preset.enable_context_editing !== undefined ? preset.enable_context_editing : false,
              enableBrowserAccess: preset.enable_browser_access ?? false
            }
          })

          // Convert predefined presets
          const predefinedPresets: PredefinedPreset[] = response.presets
            .filter(preset => preset.is_predefined)
            .map((preset: PresetQuery) => {
              // Parse LLM config safely
              let llmConfig: PresetLLMConfig | undefined
              try {
                if (preset.llm_config) {
                  if (typeof preset.llm_config === 'string') {
                    llmConfig = JSON.parse(preset.llm_config)
                  } else {
                    llmConfig = preset.llm_config as unknown as PresetLLMConfig
                  }
                }
              } catch (error) {
                console.error('[PRESET] Error parsing LLM config:', error)
                llmConfig = undefined
              }
              
              let selectedFolder: PlannerFile | undefined = undefined;
              // Handle selected_folder - could be string, null, or undefined
              if (preset.selected_folder && typeof preset.selected_folder === 'string') {
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
                query: preset.query || '',
                selectedServers: [],
                selectedTools: [], // NEW: Predefined presets don't have custom tool selection
                agentMode: preset.agent_mode as 'simple' | 'workflow' | undefined,
                selectedFolder,
                llmConfig,
                useCodeExecutionMode: preset.use_code_execution_mode,
                enableContextSummarization: preset.enable_context_summarization !== undefined ? preset.enable_context_summarization : true,
                enableContextEditing: preset.enable_context_editing !== undefined ? preset.enable_context_editing : false
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
      
      addPreset: async (label, query, selectedServers, selectedTools, selectedSkills, agentMode, selectedFolder, llmConfig, useCodeExecutionMode, enableContextSummarization, useToolSearchMode, enableBrowserAccess, enableContextEditing, selectedSecrets) => {
        // Apply workflow-specific default for tool search mode
        // When agentMode is 'workflow' and useToolSearchMode is not explicitly provided, default to true
        const effectiveToolSearchMode = useToolSearchMode !== undefined ? useToolSearchMode : (agentMode === 'workflow')

        try {
          // Logic for Tool Search Mode:
          // If enabled, the tools selected in the UI become "pre-discovered tools" (always available).
          // We clear selected_tools to allow the agent to search ALL tools from the selected servers.
          let toolsForBackend = selectedTools?.filter(t => !t.endsWith(':*')) || []
          let preDiscoveredTools: string[] = []

          if (effectiveToolSearchMode) {
            // Extract tool names from "server:tool" format
            preDiscoveredTools = toolsForBackend.map(t => {
              const parts = t.split(':')
              return parts.length > 1 ? parts[1] : t
            })
            // Clear selected_tools to allow searching all tools
            toolsForBackend = []
          }

          console.log('[PRESET_SAVE] Before filtering:', {
            selectedServers,
            selectedTools,
            selectedSkills,
            toolsForBackend,
            preDiscoveredTools,
            effectiveToolSearchMode,
            label
          });

          const request: CreatePresetQueryRequest = {
            label,
            query: query || '',
            selected_servers: selectedServers,
            selected_tools: toolsForBackend, // Filtered tools (empty if tool search mode)
            selected_skills: selectedSkills, // Skill folder names for workflow
            agent_mode: agentMode,
            selected_folder: selectedFolder?.filepath,
            use_tool_search_mode: effectiveToolSearchMode,
            pre_discovered_tools: preDiscoveredTools
          }
          
          // Include LLM config if provided
          if (llmConfig) {
            request.llm_config = llmConfig
          }
          
          // Include code execution mode - always send it if it's a boolean (true or false)
          // Don't use !== undefined check as it will skip false values
          if (useCodeExecutionMode !== undefined) {
            request.use_code_execution_mode = useCodeExecutionMode
            console.log('[code_execution] [PRESET_SAVE] Including code execution mode in create request:', useCodeExecutionMode)
          } else {
            console.log('[code_execution] [PRESET_SAVE] Code execution mode is undefined, not including in request')
          }
          
          // Include context summarization if provided
          if (enableContextSummarization !== undefined) {
            request.enable_context_summarization = enableContextSummarization
          }

          // Include browser access if provided
          if (enableBrowserAccess !== undefined) {
            request.enable_browser_access = enableBrowserAccess
          }

          // Include context editing if provided
          if (enableContextEditing !== undefined) {
            request.enable_context_editing = enableContextEditing
          }

          console.log('[code_execution] [PRESET_SAVE] Sending to backend:', {
            request,
            use_code_execution_mode: request.use_code_execution_mode
          });
          
          const response = await agentApi.createPresetQuery(request)
          
          const newPreset: CustomPreset = {
            id: response.id,
            label: response.label,
            query: response.query || '',
            createdAt: new Date(response.created_at).getTime(),
            selectedServers,
            selectedTools, // Keep original selection for UI
            selectedSkills, // Skill folder names
            selectedSecrets, // Secret IDs (persisted to DB as names)
            agentMode,
            selectedFolder,
            llmConfig,
            useCodeExecutionMode,
            useToolSearchMode: effectiveToolSearchMode,
            preDiscoveredTools,
            enableContextSummarization,
            enableContextEditing,
            enableBrowserAccess
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

      updatePreset: async (id, label, query, selectedServers, selectedTools, selectedSkills, agentMode, selectedFolder, llmConfig, useCodeExecutionMode, enableContextSummarization, useToolSearchMode, enableBrowserAccess, enableContextEditing, selectedSecrets) => {
        // CRITICAL: Log ALL arguments using rest parameters to see what's actually passed
        console.error('[code_execution] [PRESET_STORE] ========== updatePreset CALLED ==========')
        console.error('[code_execution] [PRESET_STORE] Arguments received:', {
          'arg1-id': id,
          'arg2-label': label,
          'arg3-query': query?.substring(0, 30),
          'arg4-selectedServers': selectedServers,
          'arg5-selectedTools': selectedTools,
          'arg6-agentMode': agentMode,
          'arg7-selectedFolder': selectedFolder ? 'defined' : 'undefined',
          'arg8-llmConfig': llmConfig ? 'defined' : 'undefined',
          'arg9-useCodeExecutionMode': useCodeExecutionMode,
          'arg9-type': typeof useCodeExecutionMode,
          'arg10-useToolSearchMode': useToolSearchMode
        })
        
        console.log('[code_execution] [PRESET_STORE] updatePreset called')
        console.log('[code_execution] [PRESET_STORE] id:', id)
        console.log('[code_execution] [PRESET_STORE] label:', label)
        console.log('[code_execution] [PRESET_STORE] param8 (useCodeExecutionMode):', useCodeExecutionMode, 'type:', typeof useCodeExecutionMode)
        console.log('[code_execution] [PRESET_STORE] param10 (useToolSearchMode):', useToolSearchMode)
        console.log('[code_execution] [PRESET_STORE] All params:', {
          'param1-id': id,
          'param2-label': label,
          'param3-query': query?.substring(0, 50) + '...',
          'param4-selectedServers': selectedServers,
          'param5-selectedTools': selectedTools,
          'param6-agentMode': agentMode,
          'param7-selectedFolder': selectedFolder ? 'defined' : 'undefined',
          'param8-llmConfig': llmConfig ? 'defined' : 'undefined',
          'param9-useCodeExecutionMode': useCodeExecutionMode,
          'param9-type': typeof useCodeExecutionMode,
          'param10-useToolSearchMode': useToolSearchMode
        })
        
        // Check if maybe parameters are shifted
        console.log('[code_execution] [PRESET_STORE] Parameter count check - function expects 10 params, checking each:')
        console.log('[code_execution] [PRESET_STORE] 1. id:', id)
        console.log('[code_execution] [PRESET_STORE] 2. label:', label)
        console.log('[code_execution] [PRESET_STORE] 3. query:', query?.substring(0, 30))
        console.log('[code_execution] [PRESET_STORE] 4. selectedServers:', selectedServers)
        console.log('[code_execution] [PRESET_STORE] 5. selectedTools:', selectedTools)
        console.log('[code_execution] [PRESET_STORE] 6. agentMode:', agentMode)
        console.log('[code_execution] [PRESET_STORE] 7. selectedFolder:', selectedFolder ? 'defined' : 'undefined')
        console.log('[code_execution] [PRESET_STORE] 8. llmConfig:', llmConfig ? 'defined' : 'undefined')
        console.log('[code_execution] [PRESET_STORE] 9. useCodeExecutionMode:', useCodeExecutionMode, 'type:', typeof useCodeExecutionMode)
        console.log('[code_execution] [PRESET_STORE] 10. useToolSearchMode:', useToolSearchMode)
        
        try {
          // Logic for Tool Search Mode:
          // If enabled, the tools selected in the UI become "pre-discovered tools" (always available).
          // We clear selected_tools to allow the agent to search ALL tools from the selected servers.
          let toolsForBackend = selectedTools?.filter(t => !t.endsWith(':*')) || []
          let preDiscoveredTools: string[] = []

          if (useToolSearchMode) {
            // Extract tool names from "server:tool" format
            preDiscoveredTools = toolsForBackend.map(t => {
              const parts = t.split(':')
              return parts.length > 1 ? parts[1] : t
            })
            // Clear selected_tools to allow searching all tools
            toolsForBackend = []
          }
          
          const request: UpdatePresetQueryRequest = {
            label,
            query: query || '',
            selected_servers: selectedServers,
            selected_tools: toolsForBackend, // Filtered tools (empty if tool search mode)
            selected_skills: selectedSkills, // Skill folder names for workflow
            agent_mode: agentMode,
            selected_folder: selectedFolder?.filepath,
            use_tool_search_mode: useToolSearchMode,
            pre_discovered_tools: preDiscoveredTools
          }

          // Include LLM config if provided
          if (llmConfig) {
            request.llm_config = llmConfig
          }

          // Include code execution mode - always send it if it's a boolean (true or false)
          // Don't use !== undefined check as it will skip false values
          if (useCodeExecutionMode !== undefined) {
            request.use_code_execution_mode = useCodeExecutionMode
            console.log('[code_execution] [PRESET] Including code execution mode in update request:', useCodeExecutionMode)
          } else {
            console.log('[code_execution] [PRESET] Code execution mode is undefined, not including in request')
          }

          // Include context summarization if provided
          if (enableContextSummarization !== undefined) {
            request.enable_context_summarization = enableContextSummarization
          }

          // Include browser access if provided
          if (enableBrowserAccess !== undefined) {
            request.enable_browser_access = enableBrowserAccess
          }

          // Include context editing if provided
          if (enableContextEditing !== undefined) {
            request.enable_context_editing = enableContextEditing
          }

          console.log('[code_execution] [PRESET] Updating preset with request:', request)

          await agentApi.updatePresetQuery(id, request)

          set(state => ({
            customPresets: state.customPresets.map(preset =>
              preset.id === id
                ? {
                    ...preset,
                    label,
                    query,
                    selectedServers,
                    selectedTools, // Keep original UI selection
                    selectedSkills, // Skill folder names
                    selectedSecrets, // Secret IDs (persisted to DB as names)
                    agentMode,
                    selectedFolder,
                    llmConfig,
                    useCodeExecutionMode,
                    useToolSearchMode,
                    preDiscoveredTools,
                    enableContextSummarization,
                    enableContextEditing,
                    enableBrowserAccess
                  }
                : preset
            )
          }))
        } catch (error) {
          console.error('[PRESET] Error updating preset:', error)
          throw error
        }
      },

      savePreset: async (label, query, selectedServers, selectedTools, selectedSkills, agentMode, selectedFolder, llmConfig, useCodeExecutionMode, id, enableContextSummarization, useToolSearchMode, enableBrowserAccess, enableContextEditing, selectedSecrets, selectedGlobalSecretNames) => {
        // Apply workflow-specific default for tool search mode
        // When agentMode is 'workflow' and useToolSearchMode is not explicitly provided, default to true
        const effectiveToolSearchMode = useToolSearchMode !== undefined ? useToolSearchMode : (agentMode === 'workflow')

        // Logic for Tool Search Mode (shared):
        // If enabled, the tools selected in the UI become "pre-discovered tools" (always available).
        // We clear selected_tools to allow the agent to search ALL tools from the selected servers.
        let toolsForBackend = selectedTools?.filter(t => !t.endsWith(':*')) || []
        let preDiscoveredTools: string[] = []

        if (effectiveToolSearchMode) {
          // Extract tool names from "server:tool" format
          preDiscoveredTools = toolsForBackend.map(t => {
            const parts = t.split(':')
            return parts.length > 1 ? parts[1] : t
          })
          // Clear selected_tools to allow searching all tools
          toolsForBackend = []
        }

        // Convert secret IDs to names for backend persistence (names are device-independent)
        // useSecretsStore imported at top level
        const secretNamesForBackend = selectedSecrets
          ?.map(secretId => useSecretsStore.getState().getSecret(secretId)?.name)
          .filter((n): n is string => !!n) || []

        if (id) {
          // Update existing preset
          try {
            const request: UpdatePresetQueryRequest = {
              label,
              query: query || '',
              selected_servers: selectedServers,
              selected_tools: toolsForBackend,
              selected_skills: selectedSkills, // Skill folder names for workflow
              selected_secrets: secretNamesForBackend, // Secret names for backend persistence
              selected_global_secret_names: selectedGlobalSecretNames ?? undefined, // null=all (omit), []=none
              agent_mode: agentMode,
              selected_folder: selectedFolder?.filepath,
              use_tool_search_mode: effectiveToolSearchMode,
              pre_discovered_tools: preDiscoveredTools
            }
            
            // Include LLM config if provided
            if (llmConfig) {
              request.llm_config = llmConfig
            }
            
            // Include code execution mode if provided
            if (useCodeExecutionMode !== undefined) {
              request.use_code_execution_mode = useCodeExecutionMode
              console.log('[code_execution] [PRESET_STORE] Including code execution mode in update request:', useCodeExecutionMode)
            }
            
            // Include context summarization if provided
            if (enableContextSummarization !== undefined) {
              request.enable_context_summarization = enableContextSummarization
            }

            // Include browser access if provided
            if (enableBrowserAccess !== undefined) {
              request.enable_browser_access = enableBrowserAccess
            }

            // Include context editing if provided
            if (enableContextEditing !== undefined) {
              request.enable_context_editing = enableContextEditing
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
                      selectedTools, // Keep original UI selection
                      selectedSkills, // Skill folder names
                      selectedSecrets, // Secret IDs (persisted to DB as names)
                      selectedGlobalSecretNames, // Per-preset global secret selection
                      agentMode,
                      selectedFolder,
                      llmConfig,
                      useCodeExecutionMode,
                      useToolSearchMode: effectiveToolSearchMode,
                      preDiscoveredTools,
                      enableContextSummarization,
                      enableContextEditing,
                      enableBrowserAccess
                    }
                  : preset
              )
            }))

            // Sync global secret selection to secrets store if this is the active preset
            const activeWorkflowId = get().activePresetIds.workflow
            const activeChatId = get().activePresetIds.chat
            if (id === activeWorkflowId || id === activeChatId) {
              useSecretsStore.getState().setSelectedGlobalSecretNames(selectedGlobalSecretNames ?? null)
            }

            // Return the updated preset
            const updatedPreset = get().customPresets.find(p => p.id === id)
            return updatedPreset || null
          } catch (error) {
            console.error('[code_execution] [PRESET_STORE] Error updating preset:', error)
            throw error
          }
        } else {
          // Create new preset
          try {
            const request: CreatePresetQueryRequest = {
              label,
              query: query || '',
              selected_servers: selectedServers,
              selected_tools: toolsForBackend,
              selected_skills: selectedSkills, // Skill folder names for workflow
              selected_secrets: secretNamesForBackend, // Secret names for backend persistence
              selected_global_secret_names: selectedGlobalSecretNames ?? undefined, // null=all (omit), []=none
              agent_mode: agentMode,
              selected_folder: selectedFolder?.filepath,
              use_tool_search_mode: effectiveToolSearchMode,
              pre_discovered_tools: preDiscoveredTools
            }

            // Include LLM config if provided
            if (llmConfig) {
              request.llm_config = llmConfig
            }

            // Include code execution mode if provided
            if (useCodeExecutionMode !== undefined) {
              request.use_code_execution_mode = useCodeExecutionMode
              console.log('[code_execution] [PRESET_STORE] Including code execution mode in create request:', useCodeExecutionMode)
            }

            // Include context summarization if provided
            if (enableContextSummarization !== undefined) {
              request.enable_context_summarization = enableContextSummarization
            }

            // Include browser access if provided
            if (enableBrowserAccess !== undefined) {
              request.enable_browser_access = enableBrowserAccess
            }

            // Include context editing if provided
            if (enableContextEditing !== undefined) {
              request.enable_context_editing = enableContextEditing
            }

            console.log('[code_execution] [PRESET_STORE] Creating preset with request:', {
              ...request,
              use_code_execution_mode: request.use_code_execution_mode
            })

            const response = await agentApi.createPresetQuery(request)

            const newPreset: CustomPreset = {
              id: response.id,
              label: response.label,
              query: response.query || '',
              createdAt: new Date(response.created_at).getTime(),
              selectedServers,
              selectedTools, // Keep original UI selection
              selectedSkills, // Skill folder names
              selectedSecrets, // Secret IDs (persisted to DB as names)
              selectedGlobalSecretNames, // Per-preset global secret selection
              agentMode,
              selectedFolder,
              llmConfig,
              useCodeExecutionMode,
              useToolSearchMode: effectiveToolSearchMode,
              preDiscoveredTools,
              enableContextSummarization,
              enableContextEditing,
              enableBrowserAccess
            }

            set(state => ({
              customPresets: [...state.customPresets, newPreset]
            }))

            return newPreset
          } catch (error) {
            console.error('[code_execution] [PRESET_STORE] Error creating preset:', error)
            throw error
          }
        }
      },

      deletePreset: async (id) => {
        try {
          await agentApi.deletePresetQuery(id)
          
          set(state => ({
            customPresets: state.customPresets.filter(preset => preset.id !== id),
            activePresetIds: {
              chat: state.activePresetIds.chat === id ? null : state.activePresetIds.chat,
              workflow: state.activePresetIds.workflow === id ? null : state.activePresetIds.workflow,
              'multi-agent': state.activePresetIds['multi-agent'] === id ? null : state.activePresetIds['multi-agent'],
            }
          }))
        } catch (error) {
          console.error('[PRESET] Error deleting preset:', error)
          throw error
        }
      },
      
      duplicatePreset: async (presetId) => {
        try {
          const state = get()
          const originalPreset = state.customPresets.find(p => p.id === presetId)
          
          if (!originalPreset) {
            throw new Error('Preset not found')
          }
          
          // Find next available version number
          const baseName = originalPreset.label
          const versionRegex = /-v(\d+)$/
          const match = baseName.match(versionRegex)
          const baseNameWithoutVersion = match ? baseName.slice(0, match.index) : baseName
          
          // Find all presets with the same base name
          const existingVersions = state.customPresets
            .filter(p => {
              const pMatch = p.label.match(versionRegex)
              const pBaseName = pMatch ? p.label.slice(0, pMatch.index) : p.label
              return pBaseName === baseNameWithoutVersion
            })
            .map(p => {
              const pMatch = p.label.match(versionRegex)
              return pMatch ? parseInt(pMatch[1], 10) : 0
            })
          
          // Find next available version
          let nextVersion = 2
          while (existingVersions.includes(nextVersion)) {
            nextVersion++
          }
          
          const newLabel = `${baseNameWithoutVersion}-v${nextVersion}`
          
          // Handle folder duplication if exists
          let newFolder: PlannerFile | undefined = undefined
          if (originalPreset.selectedFolder?.filepath) {
            const originalFolderPath = originalPreset.selectedFolder.filepath
            const folderPathParts = originalFolderPath.split('/')
            const folderName = folderPathParts[folderPathParts.length - 1]
            const folderNameMatch = folderName.match(versionRegex)
            const baseFolderName = folderNameMatch ? folderName.slice(0, folderNameMatch.index) : folderName
            const newFolderName = `${baseFolderName}-v${nextVersion}`
            
            // Build new folder path
            folderPathParts[folderPathParts.length - 1] = newFolderName
            const newFolderPath = folderPathParts.join('/')
            
            // Create empty new folder instead of copying
            try {
              await agentApi.createPlannerFolder(newFolderPath, `Create folder for duplicated preset ${newLabel}`)
              // Create new folder object with updated path
              newFolder = {
                ...originalPreset.selectedFolder,
                filepath: newFolderPath,
                type: 'folder' as const
              }
              
              // For workflow presets, copy plan.json, step_config.json, and variables.json
              if (originalPreset.agentMode === 'workflow') {
                const planJsonPath = `${originalFolderPath}/planning/plan.json`
                const stepConfigJsonPath = `${originalFolderPath}/planning/step_config.json`
                const variablesJsonPath = `${originalFolderPath}/variables/variables.json`
                const newPlanJsonPath = `${newFolderPath}/planning/plan.json`
                const newStepConfigJsonPath = `${newFolderPath}/planning/step_config.json`
                const newVariablesJsonPath = `${newFolderPath}/variables/variables.json`
                
                // Create planning subdirectory if it doesn't exist
                const planningFolderPath = `${newFolderPath}/planning`
                try {
                  await agentApi.createPlannerFolder(planningFolderPath, `Create planning folder for duplicated workflow preset ${newLabel}`)
                } catch {
                  // Planning folder might already exist, continue
                }
                
                // Create variables subdirectory if it doesn't exist
                const variablesFolderPath = `${newFolderPath}/variables`
                try {
                  await agentApi.createPlannerFolder(variablesFolderPath, `Create variables folder for duplicated workflow preset ${newLabel}`)
                } catch {
                  // Variables folder might already exist, continue
                }
                
                try {
                  // Copy plan.json
                  const planResponse = await agentApi.getPlannerFileContent(planJsonPath)
                  if (planResponse.success && planResponse.data?.content) {
                    await agentApi.updatePlannerFile(
                      newPlanJsonPath,
                      planResponse.data.content,
                      `Copy plan.json for duplicated workflow preset ${newLabel}`
                    )
                  }
                } catch {
                  // Continue even if plan.json copy fails
                }
                
                try {
                  // Copy step_config.json
                  const stepConfigResponse = await agentApi.getPlannerFileContent(stepConfigJsonPath)
                  if (stepConfigResponse.success && stepConfigResponse.data?.content) {
                    await agentApi.updatePlannerFile(
                      newStepConfigJsonPath,
                      stepConfigResponse.data.content,
                      `Copy step_config.json for duplicated workflow preset ${newLabel}`
                    )
                  }
                } catch {
                  // Continue even if step_config.json copy fails
                }
                
                try {
                  // Copy variables.json
                  const variablesResponse = await agentApi.getPlannerFileContent(variablesJsonPath)
                  if (variablesResponse.success && variablesResponse.data?.content) {
                    await agentApi.updatePlannerFile(
                      newVariablesJsonPath,
                      variablesResponse.data.content,
                      `Copy variables.json for duplicated workflow preset ${newLabel}`
                    )
                  }
                } catch {
                  // Continue even if variables.json copy fails
                }
              }
            } catch {
              // Continue without folder if creation fails
              // Reset newFolder to undefined if folder creation failed
              newFolder = undefined
            }
          }
          
          // Create new preset with duplicated data
          const newPreset = await state.savePreset(
            newLabel,
            originalPreset.query,
            originalPreset.selectedServers,
            originalPreset.selectedTools,
            originalPreset.selectedSkills, // Skill folder names
            originalPreset.agentMode,
            newFolder,
            originalPreset.llmConfig,
            originalPreset.useCodeExecutionMode,
            undefined, // id (new preset)
            undefined, // enableContextSummarization
            undefined, // useToolSearchMode
            undefined, // enableBrowserAccess
            undefined, // enableContextEditing
            originalPreset.selectedSecrets, // Copy secret selections
            originalPreset.selectedGlobalSecretNames // Copy global secret selection
          )
          
          // If original preset had a workflow, create a new workflow for the duplicated preset
          if (originalPreset.agentMode === 'workflow' && newPreset) {
            try {
              const workflowStatus = await agentApi.getWorkflowStatus(presetId)
              if (workflowStatus.success && workflowStatus.workflow) {
                // Create new workflow with same status and selected options
                await agentApi.createWorkflow(newPreset.id, false) // humanVerificationRequired = false
                
                // Update workflow with same status and selected options if they exist
                if (workflowStatus.workflow.workflow_status || workflowStatus.workflow.selected_options) {
                  await agentApi.updateWorkflow(
                    newPreset.id,
                    workflowStatus.workflow.workflow_status,
                    workflowStatus.workflow.selected_options || null
                  )
                }
              }
            } catch {
              // Continue even if workflow duplication fails
            }
          }
          
          return newPreset
        } catch (error) {
          console.error('[PRESET] Error duplicating preset:', error)
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
          
          // Note: Session IDs are now managed per-tab, not globally
          // When a preset is applied, tabs will be created with new session IDs as needed

          // Handle workflow state when switching workflows
          if (modeCategory === 'workflow') {
            const workflowStore = useWorkflowStore.getState()

            // Save current preset's settings before switching away
            const currentPresetId = get().activePresetIds.workflow
            if (currentPresetId && currentPresetId !== preset.id) {
              workflowStore.saveSettings()
            }

            // Switch to new preset - resets context and loads saved settings in one update
            workflowStore.switchToPreset(preset.id)
          }

          // Set the current query in both stores (only if query exists and is non-empty)
          if (preset.query && preset.query.trim()) {
            set({ currentQuery: preset.query })

            // Also update the AppStore's currentQuery for ChatInput/ChatArea components
            useAppStore.getState().setCurrentQuery(preset.query)
          }
          
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

          // Keep MCP store in sync so UI reflects selection (mode-specific)
          // Workflow mode: sync to workflowSelectedServers
          // Chat mode: don't sync - let user's manual selection persist
          if (modeCategory === 'workflow') {
            try {
              const { setWorkflowSelectedServers } = useMCPStore.getState()
              if (typeof setWorkflowSelectedServers === 'function') {
                setWorkflowSelectedServers(servers)
              }
            } catch (error) {
              console.warn('[GlobalPresetStore] Failed to sync MCP store:', error)
            }
          }
          // For chat mode, don't sync to global MCP store - let user's manual selection persist in chatSelectedServers
          
          // Set folder selection
          const folderPath = preset.selectedFolder?.filepath || null
          set({ selectedPresetFolder: folderPath })
          
          // Apply LLM configuration if preset has one (mode-specific)
          if (preset.llmConfig) {
            const llmState = useLLMStore.getState()
            const {
              getConfigForMode,
              setChatPrimaryConfig,
              setWorkflowPrimaryConfig,
              setPrimaryConfig // Also update legacy for backward compatibility
            } = llmState

            // Get the appropriate mode-specific config as base
            const mode: 'chat' | 'workflow' = modeCategory === 'workflow' ? 'workflow' : 'chat'
            const modeConfig = getConfigForMode(mode)
            const currentPrimaryConfig = modeConfig.primaryConfig

            const updatedConfig = {
              ...currentPrimaryConfig, // Preserve existing mode-specific configuration
              provider: preset.llmConfig.provider || currentPrimaryConfig.provider,
              model_id: preset.llmConfig.model_id || currentPrimaryConfig.model_id
            }

            // Update mode-specific config
            if (modeCategory === 'workflow') {
              setWorkflowPrimaryConfig(updatedConfig)
            } else {
              setChatPrimaryConfig(updatedConfig)
            }

            // Also update legacy config for backward compatibility
            setPrimaryConfig(updatedConfig)
          }
          
          // Sync per-preset global secret selection to secrets store
          // This ensures the SecretSelectionDropdown reflects the preset's setting
          if ('selectedGlobalSecretNames' in preset) {
            const presetGlobalSecrets = (preset as CustomPreset).selectedGlobalSecretNames
            useSecretsStore.getState().setSelectedGlobalSecretNames(presetGlobalSecrets ?? null)
          }

          // Handle workspace folder selection
          if (folderPath) {
            // Clear any previously selected file in workspace
            useWorkspaceStore.getState().setSelectedFile(null)
            
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
            
            // Note: File context is now preset-specific (stored in preset.selectedFolder)
            // No need to manipulate global file context
          } else {
            // Clear workspace selection if no folder
            useWorkspaceStore.getState().setSelectedFile(null)
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
            'workflow': null,
            'multi-agent': null,
          }
        })
      },

      setActivePreset: (modeCategory: Exclude<ModeCategory, null>, presetId: string | null) => {
        set(state => ({
          activePresetIds: {
            ...state.activePresetIds,
            [modeCategory]: presetId
          }
        }))
      },
      
      // Helper actions
      getPresetsForMode: (modeCategory) => {
        const state = get()
        const allPresets = [...state.customPresets, ...state.predefinedPresets]
        
        return allPresets.filter(preset => {
          if (modeCategory === 'chat') {
            return preset.agentMode === 'simple'
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
    savePreset: store.savePreset,
    deletePreset: store.deletePreset,
    duplicatePreset: store.duplicatePreset,
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
