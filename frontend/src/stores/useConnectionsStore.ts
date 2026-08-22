import { create } from 'zustand'
import {
  connectionsApi,
  ConnectionError,
  type CatalogEntry,
  type Connection,
  type ConnectionsSummary,
  type ConnectPayload,
  type FriendlyError,
  type TestResult,
} from '../services/connectionsApi'

/** How long to wait for the user to finish an OAuth approval before giving up. */
const OAUTH_POLL_INTERVAL_MS = 2000
const OAUTH_POLL_TIMEOUT_MS = 5 * 60 * 1000
/** Faster cadence once the callback page has told us the approval landed. */
const OAUTH_SETTLE_INTERVAL_MS = 400
/** Marker posted by the server's OAuth callback page. Must match the backend. */
const OAUTH_RESULT_MESSAGE = 'agentworks-oauth-result'

interface OAuthResultMessage {
  type: typeof OAUTH_RESULT_MESSAGE
  status: 'success' | 'error'
  message?: string
}

function isOAuthResult(data: unknown): data is OAuthResultMessage {
  return (
    typeof data === 'object' &&
    data !== null &&
    (data as { type?: unknown }).type === OAUTH_RESULT_MESSAGE
  )
}

/**
 * Opens the provider's consent page. The tab keeps its opener on purpose: that
 * is what lets the callback page hand focus back to this tab and close itself,
 * rather than stranding the user on a finished page. (`noopener` would sever
 * exactly that link, so it is deliberately absent here.)
 */
function openOAuthWindow(url: string): Window | null {
  return window.open(url, 'agentworks-oauth')
}

interface ConnectionsState {
  catalog: CatalogEntry[]
  connections: Connection[]
  summary: ConnectionsSummary

  isLoadingCatalog: boolean
  isLoadingConnections: boolean
  /** Catalog/connection id currently mid-connect, so its card can show a spinner. */
  connectingId: string | null
  /** Catalog/connection id currently being tested. */
  testingId: string | null
  /** Catalog/connection id currently being disconnected. */
  disconnectingId: string | null

  loadError: FriendlyError | null
  actionError: FriendlyError | null

  loadCatalog: () => Promise<void>
  loadConnections: () => Promise<void>
  refresh: () => Promise<void>

  connect: (id: string, payload?: ConnectPayload) => Promise<ConnectOutcome>
  disconnect: (id: string) => Promise<boolean>
  test: (id: string) => Promise<TestResult | null>

  clearActionError: () => void
  getConnection: (id: string) => Connection | undefined
}

export type ConnectOutcome =
  | { kind: 'connected' }
  | { kind: 'needs_client_id'; scopes?: string[]; resource?: string }
  | { kind: 'failed'; error: FriendlyError }

const emptySummary: ConnectionsSummary = { connected: 0, needs_attention: 0, total: 0 }

function toFriendly(err: unknown): FriendlyError {
  if (err instanceof ConnectionError) return err.friendly
  return {
    code: 'unknown',
    title: 'Something went wrong',
    message: 'The action could not be completed.',
    action: 'retry',
    raw: String(err),
  }
}

type WaitOutcome =
  | { kind: 'connected' }
  | { kind: 'abandoned' }
  | { kind: 'rejected'; message: string }

/**
 * Waits for an OAuth approval to land. The token exchange happens server-side,
 * so the authoritative signal is still the connection turning healthy — but the
 * callback page posts its outcome back here first, which lets the wait finish
 * (and the sign-in window close) immediately instead of on the next poll.
 */
async function waitForConnection(
  id: string,
  isStillConnecting: () => boolean,
  authWindow: Window | null
): Promise<WaitOutcome> {
  const deadline = Date.now() + OAUTH_POLL_TIMEOUT_MS
  let approved = false
  let rejection: string | null = null

  const onMessage = (event: MessageEvent) => {
    if (!isOAuthResult(event.data)) return
    if (event.data.status === 'success') approved = true
    else rejection = event.data.message || 'Access was not granted.'
  }
  window.addEventListener('message', onMessage)

  try {
    while (Date.now() < deadline) {
      await new Promise(resolve =>
        setTimeout(resolve, approved ? OAUTH_SETTLE_INTERVAL_MS : OAUTH_POLL_INTERVAL_MS)
      )

      if (rejection) return { kind: 'rejected', message: rejection }

      // The user closed the modal or started another connect — stop polling.
      if (!isStillConnecting()) return { kind: 'abandoned' }

      try {
        const { connections } = await connectionsApi.list()
        const match = connections.find(c => c.id === id)
        if (match?.health === 'connected') return { kind: 'connected' }
      } catch {
        // Transient failure while the user is still approving; keep waiting.
      }
    }
    return { kind: 'abandoned' }
  } finally {
    window.removeEventListener('message', onMessage)
    // Belt and braces: the callback page closes itself, but if the browser
    // refused that, close it from this side so focus lands back on the app.
    try {
      authWindow?.close()
    } catch {
      // Cross-origin close can throw; the page's own close already covers it.
    }
    window.focus()
  }
}

export const useConnectionsStore = create<ConnectionsState>((set, get) => ({
  catalog: [],
  connections: [],
  summary: emptySummary,

  isLoadingCatalog: false,
  isLoadingConnections: false,
  connectingId: null,
  testingId: null,
  disconnectingId: null,

  loadError: null,
  actionError: null,

  loadCatalog: async () => {
    set({ isLoadingCatalog: true })
    try {
      const { integrations } = await connectionsApi.getCatalog()
      set({ catalog: integrations, isLoadingCatalog: false, loadError: null })
    } catch (err) {
      // A missing catalog must not hide already-connected integrations, so this
      // records the error without clearing the connections list.
      set({ isLoadingCatalog: false, loadError: toFriendly(err) })
    }
  },

  loadConnections: async () => {
    set({ isLoadingConnections: true })
    try {
      const { connections, summary } = await connectionsApi.list()
      set({ connections, summary, isLoadingConnections: false, loadError: null })
    } catch (err) {
      set({ isLoadingConnections: false, loadError: toFriendly(err) })
    }
  },

  refresh: async () => {
    await Promise.all([get().loadCatalog(), get().loadConnections()])
  },

  connect: async (id, payload = {}) => {
    set({ connectingId: id, actionError: null })
    try {
      const result = await connectionsApi.connect(id, payload)

      if (result.status === 'needs_client_id') {
        set({ connectingId: null })
        return {
          kind: 'needs_client_id',
          scopes: result.scopes_supported,
          resource: result.resource,
        }
      }

      if (result.status === 'connected') {
        set({ connectingId: null })
        await get().loadConnections()
        return { kind: 'connected' }
      }

      // OAuth: hand the user to the provider, then wait for the callback.
      const authWindow = result.auth_url ? openOAuthWindow(result.auth_url) : null

      const outcome = await waitForConnection(
        id,
        () => get().connectingId === id,
        authWindow
      )
      set({ connectingId: null })
      await get().loadConnections()

      if (outcome.kind === 'connected') return { kind: 'connected' }

      const error: FriendlyError =
        outcome.kind === 'rejected'
          ? {
              code: 'not_completed',
              title: 'Authentication failed',
              message: outcome.message,
              action: 'retry',
            }
          : {
              code: 'not_completed',
              title: 'Authentication was not completed',
              message:
                'The sign-in window was closed or timed out before access was approved. Try connecting again.',
              action: 'retry',
            }
      set({ actionError: error })
      return { kind: 'failed', error }
    } catch (err) {
      const error = toFriendly(err)
      set({ connectingId: null, actionError: error })
      return { kind: 'failed', error }
    }
  },

  disconnect: async id => {
    set({ disconnectingId: id, actionError: null })
    try {
      await connectionsApi.disconnect(id)
      await get().loadConnections()
      set({ disconnectingId: null })
      return true
    } catch (err) {
      set({ disconnectingId: null, actionError: toFriendly(err) })
      return false
    }
  },

  test: async id => {
    set({ testingId: id, actionError: null })
    try {
      const result = await connectionsApi.test(id)
      set({ testingId: null })
      return result
    } catch (err) {
      set({ testingId: null, actionError: toFriendly(err) })
      return null
    }
  },

  clearActionError: () => set({ actionError: null }),

  getConnection: id => get().connections.find(c => c.id === id),
}))
