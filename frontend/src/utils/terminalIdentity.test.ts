import { describe, expect, it } from 'vitest'
import type { TerminalSnapshot } from '../services/api-types'
import { isMainAgentTerminal } from './terminalIdentity'

const terminal = (overrides: Partial<TerminalSnapshot>): TerminalSnapshot => ({
  terminal_id: 'session-1:main:session-1',
  session_id: 'session-1',
  active: true,
  state: 'running',
  ...overrides,
} as TerminalSnapshot)

describe('isMainAgentTerminal', () => {
  it('recognizes the canonical main owner', () => {
    expect(isMainAgentTerminal(terminal({
      owner_id: 'main:session-1',
      execution_kind: 'main_agent',
    }))).toBe(true)
  })

  it('does not promote a child with an inherited main kind', () => {
    expect(isMainAgentTerminal(terminal({
      terminal_id: 'session-1:pulse-reviewer-eval-789',
      owner_id: 'pulse-reviewer-eval-789',
      execution_kind: 'main_agent',
    }))).toBe(false)
  })
})
