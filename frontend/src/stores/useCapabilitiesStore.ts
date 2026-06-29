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
  isLocalMode: () => boolean
}

export const useCapabilitiesStore = create<CapabilitiesState>()(
    (set, get) => ({
      capabilities: null,
      loading: false,
      error: null,

      fetchCapabilities: async () => {
        set({ loading: true, error: null })
        // Retry with backoff: the frontend starts faster than the Go backend
        // compiles/listens, so an early single fetch hits a dead backend and would
        // otherwise leave capabilities permanently empty (no retry) until a manual
        // reload. Retry until the backend answers; once it does, the store updates
        // and dependent gates (e.g. terminal_live_attach) flip on their own.
        for (let attempt = 1; attempt <= 15; attempt++) {
          try {
            const capabilities = await agentApi.getCapabilities()
            set({ capabilities, loading: false, error: null })
            return
          } catch (err) {
            if (attempt === 15) {
              set({ error: err instanceof Error ? err.message : 'Failed to fetch capabilities', loading: false })
              return
            }
            await new Promise((r) => setTimeout(r, Math.min(400 * attempt, 3000)))
          }
        }
      },

      isLocalMode: () => {
        return get().capabilities?.local_mode ?? false
      }
    })
)
