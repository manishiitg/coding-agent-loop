import { describe, expect, it } from 'vitest'
import type { TerminalSnapshot } from '../services/api-types'
import { hasFreshTerminalDetailBody } from './terminalDetailFreshness'

function snapshot(patch: Partial<TerminalSnapshot> = {}): TerminalSnapshot {
  return {
    terminal_id: 'session-1:main:session-1',
    session_id: 'session-1',
    active: false,
    state: 'completed',
    content: '',
    rows: [],
    chunk_index: 2,
    status: {},
    created_at: '',
    updated_at: '2026-07-18T12:00:00Z',
    ...patch,
  }
}

describe('terminal detail freshness', () => {
  it('does not treat an older rendered fallback as current', () => {
    const currentListSnapshot = snapshot({ chunk_index: 2, content: '' })
    const staleRenderedFallback = snapshot({ chunk_index: 1, content: 'old turn' })

    expect(hasFreshTerminalDetailBody(currentListSnapshot, undefined)).toBe(false)
    // The stale fallback intentionally is not an input: rendering it must not
    // prevent a detail request for currentListSnapshot revision 2.
    expect(staleRenderedFallback.content).toBe('old turn')
  })

  it('accepts content cached for the exact current revision', () => {
    const currentListSnapshot = snapshot({ chunk_index: 2 })
    const exactCachedDetail = snapshot({ chunk_index: 2, content: 'new synthetic turn' })

    expect(hasFreshTerminalDetailBody(currentListSnapshot, exactCachedDetail)).toBe(true)
  })

  it('accepts content included directly in the current list snapshot', () => {
    expect(hasFreshTerminalDetailBody(snapshot({ content: 'current pane' }))).toBe(true)
  })
})
