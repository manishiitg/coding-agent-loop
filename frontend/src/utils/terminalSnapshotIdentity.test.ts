import { describe, expect, it } from 'vitest'
import type { TerminalSnapshot } from '../services/api-types'
import { reconcileTerminalSnapshots } from './terminalSnapshotIdentity'

const terminal = (id: string, overrides: Partial<TerminalSnapshot> = {}): TerminalSnapshot => ({
  terminal_id: id,
  session_id: 'session-1',
  content: '',
  rows: [],
  chunk_index: 1,
  active: true,
  status: { provider_label: 'Codex', input_tokens: 10 },
  created_at: '2026-07-13T00:00:00Z',
  updated_at: '2026-07-13T00:00:01Z',
  ...overrides,
})

describe('reconcileTerminalSnapshots', () => {
  it('preserves the array for repeated empty polls', () => {
    const current: TerminalSnapshot[] = []

    expect(reconcileTerminalSnapshots(current, [])).toBe(current)
  })

  it('preserves the array and objects for semantically identical polls', () => {
    const current = [terminal('one'), terminal('two')]
    const incoming = current.map(item => ({
      ...item,
      rows: [...item.rows],
      status: { ...item.status },
    }))

    const result = reconcileTerminalSnapshots(current, incoming)

    expect(result).toBe(current)
    expect(result[0]).toBe(current[0])
  })

  it('does not churn state for 20 unchanged active terminals', () => {
    const current = Array.from({ length: 20 }, (_, index) => terminal(`terminal-${index}`))
    const incoming = current.map(item => ({
      ...item,
      rows: [...item.rows],
      status: { ...item.status },
    }))

    expect(reconcileTerminalSnapshots(current, incoming)).toBe(current)
  })

  it('ignores backend ordering changes when the terminal set is unchanged', () => {
    const current = [terminal('one'), terminal('two')]
    const incoming = [{ ...current[1] }, { ...current[0] }]

    expect(reconcileTerminalSnapshots(current, incoming)).toBe(current)
  })

  it('reuses unchanged objects while applying changed snapshots', () => {
    const current = [terminal('one'), terminal('two')]
    const changed = terminal('two', { chunk_index: 2, updated_at: '2026-07-13T00:00:02Z' })

    const result = reconcileTerminalSnapshots(current, [{ ...current[0] }, changed])

    expect(result).not.toBe(current)
    expect(result[0]).toBe(current[0])
    expect(result[1]).toBe(changed)
  })

  it('applies additions and removals', () => {
    const current = [terminal('one'), terminal('two')]
    const added = terminal('three')

    expect(reconcileTerminalSnapshots(current, [current[0], added])).toEqual([current[0], added])
    expect(reconcileTerminalSnapshots(current, [current[0]])).toEqual([current[0]])
  })
})
