import type { TerminalSnapshot } from '../services/api-types'

const INITIAL_RECONNECT_DELAY_MS = 500
const MAX_RECONNECT_DELAY_MS = 5000

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
