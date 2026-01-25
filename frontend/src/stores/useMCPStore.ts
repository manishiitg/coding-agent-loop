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
  // LEGACY: kept for backward compatibility, use mode-specific selections
  selectedServers: string[]

  // Mode-specific server selections (chat vs workflow)
  chatSelectedServers: string[]
  workflowSelectedServers: string[]
  
  // UI state
  expandedServers: Set<string>
  selectedTool: {serverName: string, toolName: string} | null
  toolDetails: Record<string, ToolDefinition>
  loadingToolDetails: Set<string>
  
  // Modal states
  showMCPDetails: boolean
  showRegistryModal: boolean
  showConfigEditor: boolean
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
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

  // Mode-specific server actions
  setChatSelectedServers: (servers: string[]) => void
  setWorkflowSelectedServers: (servers: string[]) => void
  getServersForMode: (mode: 'chat' | 'workflow') => string[]
  
  // Tool detail actions
  setExpandedServers: (servers: Set<string>) => void
  toggleExpandedServer: (server: string) => void
  setSelectedTool: (tool: {serverName: string, toolName: string} | null) => void
  loadToolDetails: (serverName: string) => Promise<void>
  
  // Modal actions
  setShowMCPDetails: (show: boolean) => void
  setShowRegistryModal: (show: boolean) => void
  setShowConfigEditor: (show: boolean) => void
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
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
        // LEGACY: kept for backward compatibility
        selectedServers: [],
        // Mode-specific server selections (initialized empty, will be migrated)
        chatSelectedServers: [],
        workflowSelectedServers: [],
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

        // Mode-specific server actions
        setChatSelectedServers: (servers) => {
          set({ chatSelectedServers: servers })
        },

        setWorkflowSelectedServers: (servers) => {
          set({ workflowSelectedServers: servers })
        },

        getServersForMode: (mode) => {
          const state = get()
          return mode === 'workflow'
            ? state.workflowSelectedServers
            : state.chatSelectedServers
        },

        refreshTools: async () => {
          set({ isLoadingTools: true, toolsError: null })

          try {
            const toolList = await agentApi.getTools() as ToolDefinition[]

            // Get the list of currently available servers from the fresh tool list
            const availableServers = [...new Set(toolList.map((tool: ToolDefinition) => tool.server).filter((server): server is string => typeof server === 'string'))]

            // Filter persisted enabledServers to only include servers that still exist
            // This removes servers that were deleted from the config
            const currentEnabledServers = get().enabledServers
            const filteredEnabledServers = currentEnabledServers.filter(server => availableServers.includes(server))

            // Filter persisted selectedServers to only include servers that still exist
            const currentSelectedServers = get().selectedServers
            const filteredSelectedServers = currentSelectedServers.filter(server =>
              server === "NO_SERVERS" || availableServers.includes(server)
            )

            // Filter persisted toolDetails to only include servers that still exist
            const currentToolDetails = get().toolDetails
            const filteredToolDetails: Record<string, ToolDefinition> = {}
            for (const server of Object.keys(currentToolDetails)) {
              if (availableServers.includes(server)) {
                filteredToolDetails[server] = currentToolDetails[server]
              }
            }

            // Filter expandedServers to only include servers that still exist
            const currentExpandedServers = get().expandedServers
            const filteredExpandedServers = new Set<string>()
            currentExpandedServers.forEach(server => {
              if (availableServers.includes(server)) {
                filteredExpandedServers.add(server)
              }
            })

            set({
              toolList,
              isLoadingTools: false,
              // Auto-enable all servers on first load if none are enabled, otherwise use filtered list
              enabledServers: filteredEnabledServers.length === 0
                ? availableServers
                : filteredEnabledServers,
              // Update selectedServers to remove deleted servers
              selectedServers: filteredSelectedServers,
              // Update toolDetails to remove deleted servers
              toolDetails: filteredToolDetails,
              // Update expandedServers to remove deleted servers
              expandedServers: filteredExpandedServers
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
          // Legacy field (kept for backward compatibility)
          selectedServers: state.selectedServers,
          // Mode-specific server selections
          chatSelectedServers: state.chatSelectedServers,
          workflowSelectedServers: state.workflowSelectedServers,
          expandedServers: Array.from(state.expandedServers), // Convert Set to Array for persistence
          toolDetails: state.toolDetails
        }),
        onRehydrateStorage: () => (state) => {
          // Convert expandedServers array back to Set
          if (state && Array.isArray(state.expandedServers)) {
            state.expandedServers = new Set(state.expandedServers)
          }
          // Ensure loading state is false after rehydration if we have tools
          if (state && state.toolList && state.toolList.length > 0) {
            state.isLoadingTools = false
          }
          // Migration: copy legacy selectedServers to mode-specific if not already set
          if (state) {
            const hasLegacyServers = state.selectedServers && state.selectedServers.length > 0
            const hasChatServers = state.chatSelectedServers && state.chatSelectedServers.length > 0
            const hasWorkflowServers = state.workflowSelectedServers && state.workflowSelectedServers.length > 0

            if (hasLegacyServers && !hasChatServers) {
              state.chatSelectedServers = [...state.selectedServers]
            }
            if (hasLegacyServers && !hasWorkflowServers) {
              state.workflowSelectedServers = [...state.selectedServers]
            }
          }
        }
      }
    ),
    {
      name: 'mcp-store'
    }
  )
)
