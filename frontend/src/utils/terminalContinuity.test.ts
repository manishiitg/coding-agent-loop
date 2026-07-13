import { describe, expect, it } from 'vitest'
import type { TerminalSnapshot } from '../services/api-types'
import { preserveTerminalContinuity } from './terminalContinuity'

const liveMain: TerminalSnapshot = {
  terminal_id: 'session-1:main:session-1',
  session_id: 'session-1',
  owner_id: 'main:session-1',
  execution_kind: 'main_agent',
  tmux_session: 'tmux-main',
  state: 'running',
  active: true,
} as TerminalSnapshot

describe('preserveTerminalContinuity', () => {
  it('keeps the live pane while an active next-turn handoff returns no terminal', () => {
    const result = preserveTerminalContinuity([liveMain], [], {
      sameScope: true,
      hasPendingActivity: true,
      emptyPollCount: 50,
      gracePolls: 10,
    })

    expect(result.terminals).toEqual([liveMain])
  })

  it('keeps the live pane when the handoff exposes only a hidden archived turn', () => {
    const archived = {
      ...liveMain,
      terminal_id: `${liveMain.terminal_id}:turn-1`,
      active: false,
      state: 'completed',
    } as TerminalSnapshot
    const result = preserveTerminalContinuity([liveMain], [archived], {
      sameScope: true,
      hasPendingActivity: true,
      emptyPollCount: 0,
      gracePolls: 10,
    })

    expect(result.terminals.map(terminal => terminal.terminal_id)).toEqual([
      archived.terminal_id,
      liveMain.terminal_id,
    ])
  })

  it('switches to the new canonical pane as soon as it is registered', () => {
    const next = { ...liveMain, tmux_session: 'tmux-next', updated_at: new Date().toISOString() }
    const result = preserveTerminalContinuity([liveMain], [next], {
      sameScope: true,
      hasPendingActivity: true,
      emptyPollCount: 4,
      gracePolls: 10,
    })

    expect(result.terminals).toEqual([next])
    expect(result.emptyPollCount).toBe(0)
  })

  it('eventually drops a gone pane when there is no pending turn', () => {
    const result = preserveTerminalContinuity([liveMain], [], {
      sameScope: true,
      hasPendingActivity: false,
      emptyPollCount: 10,
      gracePolls: 10,
    })

    expect(result.terminals).toEqual([])
  })
})
