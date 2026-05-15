import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import { authApi, getAuthToken, setAuthToken, clearAuthToken } from '../services/api'
import type { AuthUser, AuthProvider } from '../services/api'
import { getWorkspaceScopedStorageKey } from './useWorkspaceConnectionStore'

// Key for storing OAuth state in sessionStorage
const OAUTH_STATE_KEY = 'oauth_state'

interface AuthState {
  // User state
  user: AuthUser | null
  isAuthenticated: boolean
  isLoading: boolean
  error: string | null

  // Multi-user mode flag
  isMultiUserMode: boolean
  isMultiUserModeChecked: boolean

  // Available auth providers
  providers: AuthProvider[]

  // Actions
  checkAuthMode: () => Promise<void>
  login: (username: string, password: string, provider?: string) => Promise<void>
  loginWithOAuth: (provider: string) => Promise<void>
  handleOAuthCallback: (code: string, state: string) => Promise<void>
  logout: () => Promise<void>
  checkAuth: () => Promise<void>
  clearError: () => void
}

// Helper to get redirect URI for OAuth
function getOAuthRedirectUri(): string {
  return `${window.location.origin}/auth/callback`
}

// Helper to save OAuth state to sessionStorage
function saveOAuthState(state: string, provider: string): void {
  sessionStorage.setItem(OAUTH_STATE_KEY, JSON.stringify({ state, provider }))
}

// Helper to get and clear OAuth state from sessionStorage
function getAndClearOAuthState(): { state: string; provider: string } | null {
  const data = sessionStorage.getItem(OAUTH_STATE_KEY)
  if (!data) return null
  sessionStorage.removeItem(OAUTH_STATE_KEY)
  try {
    return JSON.parse(data)
  } catch {
    return null
  }
}

export const useAuthStore = create<AuthState>()(
    persist(
      (set) => ({
        // Initial state
        user: null,
        isAuthenticated: false,
        isLoading: false,
        error: null,
        isMultiUserMode: false,
        isMultiUserModeChecked: false,
        providers: [],

        // Check if server is in multi-user mode and get available providers
        checkAuthMode: async () => {
          try {
            const response = await authApi.getAuthMode()
            set({
              isMultiUserMode: response.multi_user_mode,
              isMultiUserModeChecked: true,
              providers: response.providers || []
            })
          } catch (error) {
            console.error('[AUTH] Failed to check auth mode:', error)
            // Default to single-user mode if we can't reach the server
            set({
              isMultiUserMode: false,
              isMultiUserModeChecked: true,
              providers: []
            })
          }
        },

        // Login action for credentials-based providers
        login: async (username: string, password: string, provider?: string) => {
          set({ isLoading: true, error: null })
          try {
            const response = await authApi.login(username, password, provider)
            setAuthToken(response.token)
            set({
              user: response.user,
              isAuthenticated: true,
              isLoading: false,
              error: null
            })
          } catch (error: unknown) {
            const message = error instanceof Error ? error.message : 'Login failed'
            set({ isLoading: false, error: message })
            throw error
          }
        },

        // Start OAuth flow for a provider
        loginWithOAuth: async (provider: string) => {
          set({ isLoading: true, error: null })
          try {
            const redirectUri = getOAuthRedirectUri()
            const response = await authApi.startOAuth(provider, redirectUri)

            // Save state to sessionStorage for verification on callback
            saveOAuthState(response.state, provider)

            // Redirect to OAuth provider
            window.location.href = response.auth_url
          } catch (error: unknown) {
            const message = error instanceof Error ? error.message : 'Failed to start OAuth flow'
            set({ isLoading: false, error: message })
            throw error
          }
        },

        // Handle OAuth callback - exchange code for app JWT
        handleOAuthCallback: async (code: string, state: string) => {
          set({ isLoading: true, error: null })
          try {
            // Verify state matches what we stored
            const savedState = getAndClearOAuthState()
            if (!savedState || savedState.state !== state) {
              throw new Error('Invalid OAuth state - possible CSRF attack')
            }

            const response = await authApi.handleOAuthCallback(code, state)
            setAuthToken(response.token)
            set({
              user: response.user,
              isAuthenticated: true,
              isLoading: false,
              error: null
            })
          } catch (error: unknown) {
            const message = error instanceof Error ? error.message : 'OAuth callback failed'
            set({ isLoading: false, error: message })
            throw error
          }
        },

        // Logout action
        logout: async () => {
          try {
            await authApi.logout()
          } catch (error) {
            console.error('[AUTH] Logout error:', error)
          }
          clearAuthToken()
          set({
            user: null,
            isAuthenticated: false,
            error: null
          })
        },

        // Check current authentication status
        checkAuth: async () => {
          const token = getAuthToken()
          if (!token) {
            set({ user: null, isAuthenticated: false })
            return
          }

          set({ isLoading: true })
          try {
            const user = await authApi.getCurrentUser()
            set({
              user,
              isAuthenticated: true,
              isLoading: false
            })
          } catch (error) {
            console.error('[AUTH] Auth check failed:', error)
            clearAuthToken()
            set({
              user: null,
              isAuthenticated: false,
              isLoading: false
            })
          }
        },

        // Clear error
        clearError: () => set({ error: null }),
      }),
      {
        name: getWorkspaceScopedStorageKey('auth-storage'),
        partialize: (state) => ({
          // Only persist user data, not loading/error states
          user: state.user,
          isAuthenticated: state.isAuthenticated,
        }),
      }
    )
)
