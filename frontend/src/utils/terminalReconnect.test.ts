import { describe, expect, it } from 'vitest'
import type { TerminalSnapshot } from '../services/api-types'
import { terminalReconnectDelayMs, terminalSnapshotCanReconnect } from './terminalReconnect'

function snapshot(patch: Partial<TerminalSnapshot>): TerminalSnapshot {
  return {
    terminal_id: 'terminal-1',
    session_id: 'session-1',
    active: true,
    state: 'running',
    tmux_session: 'tmux-1',
    content: '',
    rows: [],
    chunk_index: 0,
    status: {},
    created_at: '',
    updated_at: '',
    ...patch,
  }
}

describe('terminal reconnect recovery', () => {
  it('backs off quickly and caps retries at five seconds', () => {
    expect([0, 1, 2, 3, 4, 20].map(terminalReconnectDelayMs))
      .toEqual([500, 1000, 2000, 4000, 5000, 5000])
  })

  it('continues for live and idle tmux panes', () => {
    expect(terminalSnapshotCanReconnect(snapshot({ state: 'running' }))).toBe(true)
    expect(terminalSnapshotCanReconnect(snapshot({ active: false, state: 'idle' }))).toBe(true)
  })

  it('stops for settled or detached panes', () => {
    expect(terminalSnapshotCanReconnect(snapshot({ active: false, state: 'completed' }))).toBe(false)
    expect(terminalSnapshotCanReconnect(snapshot({ state: 'stale' }))).toBe(false)
    expect(terminalSnapshotCanReconnect(snapshot({ tmux_session: '' }))).toBe(false)
  })
})
