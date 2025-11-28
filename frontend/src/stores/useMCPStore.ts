import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import { devtools } from 'zustand/middleware'
import type { ToolDefinition, StoreActions } from './types'
import { agentApi } from '../services/api'

interface MCPState extends StoreActions {
  // Server and tool data
  toolList: ToolDefinition[]
  enabledServers: string[]
  enabledTools: string[]
  
  // Server selection (unified approach)
  selectedServers: string[] // Single source of truth for current selection
  
  // UI state
  expandedServers: Set<string>
  selectedTool: {serverName: string, toolName: string} | null
  toolDetails: Record<string, ToolDefinition>
  loadingToolDetails: Set<string>
  
  // Modal states
  showMCPDetails: boolean
  showRegistryModal: boolean
  showConfigEditor: boolean
  showApiTester: { serverName: string; toolName: string; toolDetail?: any } | null
  
  // Loading states
  isLoadingTools: boolean
  toolsError: string | null
  
  // Actions
  setEnabledServers: (servers: string[]) => void
  setSelectedServers: (servers: string[]) => void
  toggleServer: (server: string) => void
  selectAllServers: () => void
  clearAllServers: () => void
  refreshTools: () => Promise<void>
  
  // Tool detail actions
  setExpandedServers: (servers: Set<string>) => void
  toggleExpandedServer: (server: string) => void
  setSelectedTool: (tool: {serverName: string, toolName: string} | null) => void
  loadToolDetails: (serverName: string) => Promise<void>
  
  // Modal actions
  setShowMCPDetails: (show: boolean) => void
  setShowRegistryModal: (show: boolean) => void
  setShowConfigEditor: (show: boolean) => void
  setShowApiTester: (value: { serverName: string; toolName: string; toolDetail?: any } | null) => void
  
  // Helper methods
  getAvailableServers: () => string[]
  getServerGroups: () => Record<string, ToolDefinition[]>
  isServerEnabled: (server: string) => boolean
  isServerSelected: (server: string) => boolean
}

export const useMCPStore = create<MCPState>()(
  devtools(
    persist(
      (set, get) => ({
        // Initial state
        toolList: [],
        enabledServers: [],
        enabledTools: [],
        selectedServers: [],
        expandedServers: new Set(),
        selectedTool: null,
        toolDetails: {},
        loadingToolDetails: new Set(),
        showMCPDetails: false,
        showRegistryModal: false,
        showConfigEditor: false,
        showApiTester: null,
        isLoadingTools: true,
        toolsError: null,

        // Actions
        setEnabledServers: (servers) => {
          set({ enabledServers: servers })
        },

        setSelectedServers: (servers) => {
          set({ selectedServers: servers })
        },

        toggleServer: (server) => {
          set((state) => {
            // Remove "NO_SERVERS" if toggling a real server
            const filtered = state.selectedServers.filter(s => s !== "NO_SERVERS")
            const newSelected = filtered.includes(server)
              ? filtered.filter(s => s !== server)
              : [...filtered, server]
            
            return { selectedServers: newSelected }
          })
        },

        selectAllServers: () => {
          const state = get()
          const availableServers = state.getAvailableServers()
          // Filter out "NO_SERVERS" and set actual server names
          set({ selectedServers: availableServers.filter(s => s !== "NO_SERVERS") })
        },

        clearAllServers: () => {
          // Set "NO_SERVERS" to indicate no servers should be used
          set({ selectedServers: ["NO_SERVERS"] })
        },

        refreshTools: async () => {
          set({ isLoadingTools: true, toolsError: null })
          
          try {
            const toolList = await agentApi.getTools() as ToolDefinition[]
            set({ 
              toolList, 
              isLoadingTools: false,
              // Auto-enable all servers on first load if none are enabled
              enabledServers: get().enabledServers.length === 0 
                ? [...new Set(toolList.map((tool: ToolDefinition) => tool.server).filter((server): server is string => typeof server === 'string'))]
                : get().enabledServers
            })
          } catch (error) {
            set({ 
              toolsError: error instanceof Error ? error.message : 'Failed to load tools',
              isLoadingTools: false 
            })
          }
        },

        // Tool detail actions
        setExpandedServers: (servers) => {
          set({ expandedServers: servers })
        },

        toggleExpandedServer: (server) => {
          set((state) => {
            const newExpanded = new Set(state.expandedServers)
            if (newExpanded.has(server)) {
              newExpanded.delete(server)
            } else {
              newExpanded.add(server)
            }
            return { expandedServers: newExpanded }
          })
        },

        setSelectedTool: (tool) => {
          set({ selectedTool: tool })
        },

        loadToolDetails: async (serverName) => {
          const state = get()
          if (state.toolDetails[serverName] || state.loadingToolDetails.has(serverName)) {
            return // Already loaded or loading
          }

          set((state) => ({
            loadingToolDetails: new Set([...state.loadingToolDetails, serverName])
          }))

          try {
            const toolDetail = await agentApi.getToolDetail(serverName)
            set((state) => ({
              toolDetails: {
                ...state.toolDetails,
                [serverName]: toolDetail
              },
              loadingToolDetails: new Set([...state.loadingToolDetails].filter(s => s !== serverName))
            }))
          } catch (error) {
            console.error(`Failed to load tool details for ${serverName}:`, error)
            set((state) => ({
              loadingToolDetails: new Set([...state.loadingToolDetails].filter(s => s !== serverName))
            }))
          }
        },

        // Modal actions
        setShowMCPDetails: (show) => {
          set({ showMCPDetails: show })
        },

        setShowRegistryModal: (show) => {
          set({ showRegistryModal: show })
        },

        setShowConfigEditor: (show) => {
          set({ showConfigEditor: show })
        },

        setShowApiTester: (value) => {
          set({ showApiTester: value })
        },

        // Helper methods
        getAvailableServers: () => {
          const state = get()
          return [...new Set(state.toolList.map((tool: ToolDefinition) => tool.server).filter((server): server is string => typeof server === 'string'))]
        },

        getServerGroups: () => {
          const state = get()
          const groups: Record<string, ToolDefinition[]> = {}
          state.toolList.forEach(tool => {
            if (tool.server) {
              if (!groups[tool.server]) {
                groups[tool.server] = []
              }
              groups[tool.server].push(tool)
            }
          })
          return groups
        },

        isServerEnabled: (server) => {
          const state = get()
          return state.enabledServers.includes(server)
        },

        isServerSelected: (server) => {
          const state = get()
          // Filter out "NO_SERVERS" when checking if a real server is selected
          return state.selectedServers.filter(s => s !== "NO_SERVERS").includes(server)
        },

        // Generic actions
        reset: () => {
          set({
            toolList: [],
            enabledServers: [],
            enabledTools: [],
            selectedServers: [],
            expandedServers: new Set(),
            selectedTool: null,
            toolDetails: {},
            loadingToolDetails: new Set(),
            showMCPDetails: false,
            showRegistryModal: false,
            showConfigEditor: false,
            isLoadingTools: true,
            toolsError: null
          })
        },

        setLoading: (loading) => {
          set({ isLoadingTools: loading })
        },

        setError: (error) => {
          set({ toolsError: error })
        }
      }),
      {
        name: 'mcp-store',
        partialize: (state) => ({
          // Only persist user preferences, not temporary state
          enabledServers: state.enabledServers,
          selectedServers: state.selectedServers,
          expandedServers: Array.from(state.expandedServers), // Convert Set to Array for persistence
          toolDetails: state.toolDetails
        }),
        onRehydrateStorage: () => (state) => {
          // Convert expandedServers array back to Set
          if (state && Array.isArray(state.expandedServers)) {
            state.expandedServers = new Set(state.expandedServers)
          }
        }
      }
    ),
    {
      name: 'mcp-store'
    }
  )
)
