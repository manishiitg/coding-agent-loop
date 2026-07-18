import type { TerminalSnapshot } from '../services/api-types'

const INITIAL_RECONNECT_DELAY_MS = 500
const MAX_RECONNECT_DELAY_MS = 5000

export type TerminalGrid = { cols: number; rows: number }

export function terminalReconnectDelayMs(failedAttempts: number): number {
  const attempt = Math.max(0, Math.floor(failedAttempts))
  return Math.min(MAX_RECONNECT_DELAY_MS, INITIAL_RECONNECT_DELAY_MS * (2 ** attempt))
}

export function terminalSnapshotCanReconnect(snapshot: TerminalSnapshot): boolean {
  const state = (snapshot.state || '').trim().toLowerCase()
  if (state === 'completed' || state === 'failed' || state === 'closing' || state === 'stale') return false
  if (!snapshot.tmux_session) return false
  return Boolean(snapshot.active) || state === 'running' || state === 'idle' || state === ''
}

// The live-attach pane keeps ONE fixed grid per connection. Changing the grid
// is therefore a connection lifecycle event, not a resize: xterm must not be
// resized while bytes wrapped for the old width can still arrive, or they land
// on the new grid and scramble. These planners own that ordering so it is
// testable independently of the DOM/WebSocket plumbing that executes it.

// Steps a geometry change performs, IN ORDER. suspend-output must come first
// (it stops the old socket's bytes from reaching xterm) and fit must come after
// close-socket, so no old-width byte can be interpreted on the new grid.
export type GeometryChangeStep = 'suspend-output' | 'close-socket' | 'fit' | 'open-socket'

// The ordered steps performed once the closing socket reports back. The backend
// re-seeds on every connect, so open-socket implies the reseed.
export const GEOMETRY_RECONNECT_AFTER_CLOSE: readonly GeometryChangeStep[] = ['fit', 'open-socket']

export function planGeometryChange(input: {
  hasSocket: boolean
  alreadyPending: boolean
  needsReconnect: boolean
  superseded: boolean
}): GeometryChangeStep[] {
  if (!input.needsReconnect || input.alreadyPending) return []
  // A superseded pane is intentionally detached; reconnecting would steal the
  // terminal back from the window that owns it.
  if (input.superseded) return []
  // No socket means a reconnect is already pending. Fit now so that reconnect
  // opens at the latest geometry, but do not start a second one.
  if (!input.hasSocket) return ['fit']
  return ['suspend-output', 'close-socket']
}

export type LiveAttachCloseAction =
  | 'ignore'
  | 'superseded'
  | 'geometry-reconnect'
  | 'recover'

// Classifies why a live-attach socket closed. Order matters: the superseded
// code is checked before every reconnect path, because reconnecting after an
// eviction would evict whoever replaced this viewer and start a ping-pong that
// never converges.
export function planLiveAttachClose(input: {
  code: number
  paneClosed: boolean
  wasCurrentSocket: boolean
  resizeReconnectPending: boolean
  reconnectOnClose: boolean
  supersededCloseCode: number
}): LiveAttachCloseAction {
  if (input.paneClosed || !input.wasCurrentSocket) return 'ignore'
  if (input.code === input.supersededCloseCode) return 'superseded'
  if (input.resizeReconnectPending) return 'geometry-reconnect'
  if (!input.reconnectOnClose) return 'ignore'
  return 'recover'
}

export function terminalGridNeedsReconnect(
  current: TerminalGrid,
  proposed: TerminalGrid | undefined,
  minimum: TerminalGrid,
): boolean {
  if (!proposed || proposed.cols < minimum.cols || proposed.rows < minimum.rows) return false
  return proposed.cols !== current.cols || proposed.rows !== current.rows
}
