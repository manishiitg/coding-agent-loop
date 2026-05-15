import { getApiBaseUrl, getAuthToken } from './api'
import type { SSEEventMessage, SSEStatusMessage } from './api-types'
import { logger } from '../utils/logger'

export interface SSECallbacks {
  onMessage: (msg: SSEEventMessage) => void
  onStatusUpdate: (msg: SSEStatusMessage) => void
  onError?: (error: Event) => void
  onOpen?: () => void
}

/**
 * SSEConnection wraps an EventSource for receiving real-time session events.
 *
 * - Listens for named events: "event" (new events) and "status" (status-only updates).
 * - Uses the Last-Event-ID mechanism for automatic reconnection catch-up.
 * - Falls back to polling after `maxConsecutiveErrors` failures.
 */
export class SSEConnection {
  private eventSource: EventSource | null = null
  private sessionId: string
  private sinceIndex: number
  private callbacks: SSECallbacks
  private consecutiveErrors = 0
  private maxConsecutiveErrors = 5
  private closed = false

  constructor(
    sessionId: string,
    sinceIndex: number,
    callbacks: SSECallbacks
  ) {
    this.sessionId = sessionId
    this.sinceIndex = sinceIndex
    this.callbacks = callbacks
    this.connect()
  }

  private connect() {
    if (this.closed) return

    // Check EventSource support
    if (typeof EventSource === 'undefined') {
      logger.warn('SSE', 'EventSource not supported in this browser')
      this.callbacks.onError?.(new Event('unsupported'))
      return
    }

    const baseUrl = getApiBaseUrl()
    const params = new URLSearchParams()
    if (this.sinceIndex >= 0) {
      params.set('since', String(this.sinceIndex))
    }

    // EventSource can't send Authorization headers, so pass JWT as query param
    const authToken = getAuthToken()
    if (authToken) {
      params.set('token', authToken)
    }

    const url = `${baseUrl}/api/sessions/${this.sessionId}/events/stream?${params}`

    this.eventSource = new EventSource(url, { withCredentials: true })

    this.eventSource.onopen = () => {
      this.consecutiveErrors = 0
      logger.debug('SSE', `Connected to session ${this.sessionId}`)
      this.callbacks.onOpen?.()
    }

    // Named event: "event" — carries new events
    this.eventSource.addEventListener('event', (e: MessageEvent) => {
      try {
        const msg: SSEEventMessage = JSON.parse(e.data)
        // Update sinceIndex from the SSE id for reconnect
        if (e.lastEventId) {
          this.sinceIndex = parseInt(e.lastEventId, 10)
        }
        this.callbacks.onMessage(msg)
      } catch (err) {
        logger.error('SSE', 'Failed to parse event message:', err)
      }
    })

    // Named event: "status" — status-only updates
    this.eventSource.addEventListener('status', (e: MessageEvent) => {
      try {
        const msg: SSEStatusMessage = JSON.parse(e.data)
        this.callbacks.onStatusUpdate(msg)
      } catch (err) {
        logger.error('SSE', 'Failed to parse status message:', err)
      }
    })

    this.eventSource.onerror = (e: Event) => {
      this.consecutiveErrors++
      logger.warn('SSE', `Connection error for session ${this.sessionId} (${this.consecutiveErrors}/${this.maxConsecutiveErrors})`)

      if (this.consecutiveErrors >= this.maxConsecutiveErrors) {
        logger.error('SSE', `Too many consecutive errors for session ${this.sessionId}, triggering fallback`)
        this.close()
        this.callbacks.onError?.(e)
      }
      // Otherwise EventSource will auto-reconnect with Last-Event-ID
    }
  }

  /** Close the connection permanently. */
  close() {
    this.closed = true
    if (this.eventSource) {
      this.eventSource.close()
      this.eventSource = null
    }
    logger.debug('SSE', `Closed connection for session ${this.sessionId}`)
  }

  /** Whether the connection is currently open. */
  get isConnected(): boolean {
    return this.eventSource !== null && this.eventSource.readyState !== EventSource.CLOSED
  }

  /** Whether the connection has been permanently closed. */
  get isClosed(): boolean {
    return this.closed
  }
}
