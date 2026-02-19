import { create } from 'zustand'
import type { ToolDefinition } from './types'
import { agentApi } from '../services/api'

interface ToolSelectionInstanceState {
  // UI state per instance
  expandedServers: Set<string>
  serverToolMode: Record<string, 'all' | 'specific'>
  loadingServers: Set<string>
}

interface ToolSelectionState {
  // Shared tool details cache (keyed by server name)
  toolDetails: Record<string, ToolDefinition[]>
  
  // Instance-specific state (keyed by instance ID)
  instances: Record<string, ToolSelectionInstanceState>
  
  // Actions
  getInstanceState: (instanceId: string) => ToolSelectionInstanceState
  setExpandedServers: (instanceId: string, servers: Set<string>) => void
  toggleExpandedServer: (instanceId: string, server: string) => void
  setServerToolMode: (instanceId: string, mode: Record<string, 'all' | 'specific'>) => void
  updateServerToolMode: (instanceId: string, server: string, mode: 'all' | 'specific') => void
  setLoadingServer: (instanceId: string, server: string, loading: boolean) => void
  
  // Tool loading (shared cache)
  loadServerTools: (serverName: string, force?: boolean) => Promise<void>
  getServerTools: (serverName: string) => ToolDefinition[] | undefined
  isServerLoading: (instanceId: string, serverName: string) => boolean
  
  // Computed actions
  syncServerToolMode: (
    instanceId: string,
    selectedServers: string[],
    selectedTools: string[]
  ) => void
  
  // Cleanup
  removeInstance: (instanceId: string) => void
}

const createInitialInstanceState = (): ToolSelectionInstanceState => ({
  expandedServers: new Set(),
  serverToolMode: {},
  loadingServers: new Set(),
})

export const useToolSelectionStore = create<ToolSelectionState>()(
    (set, get) => ({
      // Initial state
      toolDetails: {},
      instances: {},

      // Get or create instance state
      getInstanceState: (instanceId: string) => {
        const state = get()
        if (!state.instances[instanceId]) {
          set((state) => ({
            instances: {
              ...state.instances,
              [instanceId]: createInitialInstanceState(),
            },
          }))
          return createInitialInstanceState()
        }
        return state.instances[instanceId]
      },

      // Expanded servers
      setExpandedServers: (instanceId: string, servers: Set<string>) => {
        set((state) => {
          const instance = state.getInstanceState(instanceId)
          return {
            instances: {
              ...state.instances,
              [instanceId]: {
                ...instance,
                expandedServers: servers,
              },
            },
          }
        })
      },

      toggleExpandedServer: (instanceId: string, server: string) => {
        set((state) => {
          const instance = state.getInstanceState(instanceId)
          const newExpanded = new Set(instance.expandedServers)
          if (newExpanded.has(server)) {
            newExpanded.delete(server)
          } else {
            newExpanded.add(server)
          }
          return {
            instances: {
              ...state.instances,
              [instanceId]: {
                ...instance,
                expandedServers: newExpanded,
              },
            },
          }
        })
      },

      // Server tool mode
      setServerToolMode: (instanceId: string, mode: Record<string, 'all' | 'specific'>) => {
        set((state) => {
          const instance = state.getInstanceState(instanceId)
          return {
            instances: {
              ...state.instances,
              [instanceId]: {
                ...instance,
                serverToolMode: mode,
              },
            },
          }
        })
      },

      updateServerToolMode: (instanceId: string, server: string, mode: 'all' | 'specific') => {
        set((state) => {
          const instance = state.getInstanceState(instanceId)
          return {
            instances: {
              ...state.instances,
              [instanceId]: {
                ...instance,
                serverToolMode: {
                  ...instance.serverToolMode,
                  [server]: mode,
                },
              },
            },
          }
        })
      },

      // Loading state
      setLoadingServer: (instanceId: string, server: string, loading: boolean) => {
        set((state) => {
          const instance = state.getInstanceState(instanceId)
          const newLoading = new Set(instance.loadingServers)
          if (loading) {
            newLoading.add(server)
          } else {
            newLoading.delete(server)
          }
          return {
            instances: {
              ...state.instances,
              [instanceId]: {
                ...instance,
                loadingServers: newLoading,
              },
            },
          }
        })
      },

      // Load server tools (shared cache)
      loadServerTools: async (serverName: string, force = false) => {
        const state = get()
        
        // Check if already loaded (unless force)
        if (!force && state.toolDetails[serverName]) {
          return
        }

        try {
          const response = await agentApi.getToolDetail(serverName)
          
          // Handle different response formats
          let serverTools: ToolDefinition[]
          if (Array.isArray(response)) {
            serverTools = response
          } else if (response && typeof response === 'object' && 'tools' in response) {
            serverTools = (response as { tools: ToolDefinition[] }).tools || []
          } else if (response && typeof response === 'object' && 'data' in response) {
            serverTools = (response as { data: ToolDefinition[] }).data || []
          } else {
            console.warn(`[ToolSelection] Unexpected response format for ${serverName}:`, response)
            serverTools = []
          }
          
          set((state) => {
            // Only update if not already set (avoid unnecessary re-renders)
            if (!force && state.toolDetails[serverName]) {
              return state
            }
            console.log('[ToolSelection] Tools loaded:', serverName, serverTools.length, 'tools')
            return {
              toolDetails: {
                ...state.toolDetails,
                [serverName]: serverTools,
              },
            }
          })
        } catch (error) {
          console.error(`Failed to load tools for ${serverName}:`, error)
        }
      },

      getServerTools: (serverName: string) => {
        return get().toolDetails[serverName]
      },

      isServerLoading: (instanceId: string, serverName: string) => {
        const instance = get().getInstanceState(instanceId)
        return instance.loadingServers.has(serverName)
      },

      // Sync server tool mode based on selected servers and tools
      syncServerToolMode: (instanceId: string, selectedServers: string[], selectedTools: string[]) => {
        const state = get()
        const instance = state.getInstanceState(instanceId)
        const newMode: Record<string, 'all' | 'specific'> = {}
        
        selectedServers.forEach(server => {
          // Check if server has the "all tools" marker
          const hasAllToolsMarker = selectedTools.includes(`${server}:*`)
          
          if (hasAllToolsMarker) {
            // Server is in "all tools" mode
            newMode[server] = 'all'
          } else {
            // Check if server has specific tools selected
            const serverTools = selectedTools.filter(t => 
              t.startsWith(`${server}:`) && !t.endsWith(':*')
            )
            // If server previously had mode set to 'specific', preserve it (user explicitly switched)
            // Otherwise, if specific tools are selected, use 'specific' mode; else default to 'all'
            const previousMode = instance.serverToolMode[server]
            if (previousMode === 'specific' || serverTools.length > 0) {
              // User explicitly switched to specific mode OR has specific tools selected
              newMode[server] = 'specific'
            } else {
              newMode[server] = 'all'
            }
          }
        })
        
        // Only update if mode actually changed to prevent infinite loops
        const hasChanged = Object.keys(newMode).some(server => 
          newMode[server] !== instance.serverToolMode[server]
        ) || Object.keys(instance.serverToolMode).some(server => 
          !selectedServers.includes(server)
        )
        
        if (hasChanged) {
          // Auto-expand all selected servers
          const newExpanded = new Set(instance.expandedServers)
          selectedServers.forEach(server => {
            newExpanded.add(server)
          })
          // Remove servers that are no longer selected
          instance.expandedServers.forEach(server => {
            if (!selectedServers.includes(server)) {
              newExpanded.delete(server)
            }
          })
          
          set({
            instances: {
              ...state.instances,
              [instanceId]: {
                ...instance,
                serverToolMode: newMode,
                expandedServers: newExpanded,
              },
            },
          })
        }
      },

      // Cleanup
      removeInstance: (instanceId: string) => {
        set((state) => {
          const newInstances = { ...state.instances }
          delete newInstances[instanceId]
          return { instances: newInstances }
        })
      },
    })
)

