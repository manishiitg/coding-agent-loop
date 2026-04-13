import { create } from 'zustand'
import { agentApi } from '../services/api'
import type { CapabilitiesResponse } from '../services/api-types'

interface CapabilitiesState {
  capabilities: CapabilitiesResponse | null
  loading: boolean
  error: string | null
  
  // Actions
  fetchCapabilities: () => Promise<void>
  
  // Helpers
  isGitSyncEnabled: () => boolean
  isLocalMode: () => boolean
}

export const useCapabilitiesStore = create<CapabilitiesState>()(
    (set, get) => ({
      capabilities: null,
      loading: false,
      error: null,

      fetchCapabilities: async () => {
        set({ loading: true, error: null })
        try {
          const capabilities = await agentApi.getCapabilities()
          set({ capabilities, loading: false })
        } catch (err) {
          console.error('Failed to fetch capabilities:', err)
          set({ 
            error: err instanceof Error ? err.message : 'Failed to fetch capabilities', 
            loading: false 
          })
        }
      },

      isGitSyncEnabled: () => {
        return get().capabilities?.workspace?.github_sync_enabled ?? false
      },

      isLocalMode: () => {
        return get().capabilities?.local_mode ?? false
      }
    })
)
