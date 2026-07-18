import { describe, expect, it } from 'vitest'
import type { TerminalSnapshot } from '../services/api-types'
import {
  GEOMETRY_RECONNECT_AFTER_CLOSE,
  planGeometryChange,
  planLiveAttachClose,
  terminalGridNeedsReconnect,
  terminalReconnectDelayMs,
  terminalSnapshotCanReconnect,
} from './terminalReconnect'

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

  it('reconnects only for a usable changed terminal grid', () => {
    const current = { cols: 120, rows: 40 }
    const minimum = { cols: 40, rows: 10 }

    expect(terminalGridNeedsReconnect(current, { cols: 100, rows: 40 }, minimum)).toBe(true)
    expect(terminalGridNeedsReconnect(current, { cols: 120, rows: 42 }, minimum)).toBe(true)
    expect(terminalGridNeedsReconnect(current, { cols: 120, rows: 40 }, minimum)).toBe(false)
    expect(terminalGridNeedsReconnect(current, { cols: 20, rows: 8 }, minimum)).toBe(false)
    expect(terminalGridNeedsReconnect(current, undefined, minimum)).toBe(false)
  })
})

describe('geometry change ordering', () => {
  const base = { hasSocket: true, alreadyPending: false, needsReconnect: true, superseded: false }

  it('suspends output before closing the socket', () => {
    // The whole point of the geometry reconnect: xterm must stop accepting the
    // old-width socket's bytes BEFORE anything closes or resizes, or bytes
    // wrapped for the old width land on the new grid and scramble.
    expect(planGeometryChange(base)).toEqual(['suspend-output', 'close-socket'])
  })

  it('fits only after the socket has closed, then reopens', () => {
    // The second half runs from onclose. fit must NOT appear in the first half:
    // resizing xterm while the socket is still open is the original bug.
    expect(planGeometryChange(base)).not.toContain('fit')
    expect(GEOMETRY_RECONNECT_AFTER_CLOSE).toEqual(['fit', 'open-socket'])
  })

  it('does nothing when the grid is unchanged', () => {
    expect(planGeometryChange({ ...base, needsReconnect: false })).toEqual([])
  })

  it('does not start a second reconnect while one is pending', () => {
    expect(planGeometryChange({ ...base, alreadyPending: true })).toEqual([])
  })

  it('only re-fits when a reconnect is already pending with no socket', () => {
    // Fit so the pending reconnect opens at the latest geometry, but do not
    // start a competing one.
    expect(planGeometryChange({ ...base, hasSocket: false })).toEqual(['fit'])
  })

  it('never reconnects a superseded pane', () => {
    // Reconnecting here would steal the terminal back from the window that
    // owns it, and that window would steal it back — a ping-pong with no
    // convergence, since a successful seed resets the client backoff.
    expect(planGeometryChange({ ...base, superseded: true })).toEqual([])
    expect(planGeometryChange({ ...base, superseded: true, hasSocket: false })).toEqual([])
  })
})

describe('live-attach close classification', () => {
  const SUPERSEDED = 4001
  const base = {
    code: 1006,
    paneClosed: false,
    wasCurrentSocket: true,
    resizeReconnectPending: false,
    reconnectOnClose: true,
    supersededCloseCode: SUPERSEDED,
  }

  it('recovers from an ordinary disconnect', () => {
    expect(planLiveAttachClose(base)).toBe('recover')
  })

  it('continues the geometry reconnect when one is pending', () => {
    expect(planLiveAttachClose({ ...base, resizeReconnectPending: true })).toBe('geometry-reconnect')
  })

  it('treats the superseded code as terminal, even mid-geometry-reconnect', () => {
    // Eviction must win over every reconnect path, otherwise two open tabs
    // evict each other in a loop.
    expect(planLiveAttachClose({ ...base, code: SUPERSEDED })).toBe('superseded')
    expect(planLiveAttachClose({ ...base, code: SUPERSEDED, resizeReconnectPending: true })).toBe('superseded')
    expect(planLiveAttachClose({ ...base, code: SUPERSEDED, reconnectOnClose: false })).toBe('superseded')
  })

  it('ignores unmounted panes and stale sockets', () => {
    expect(planLiveAttachClose({ ...base, paneClosed: true })).toBe('ignore')
    expect(planLiveAttachClose({ ...base, wasCurrentSocket: false })).toBe('ignore')
    // A stale socket must not be able to trigger a take-over either.
    expect(planLiveAttachClose({ ...base, code: SUPERSEDED, wasCurrentSocket: false })).toBe('ignore')
  })

  it('does not reconnect a settled terminal', () => {
    expect(planLiveAttachClose({ ...base, reconnectOnClose: false })).toBe('ignore')
  })
})
