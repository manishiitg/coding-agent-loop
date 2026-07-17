import { describe, expect, it } from 'vitest'
import type { TerminalSnapshot } from '../services/api-types'
import { activeSessionFromWorkflowTerminal, isLiveWorkflowTerminal } from './workflowTerminalActivity'

function terminal(overrides: Partial<TerminalSnapshot>): TerminalSnapshot {
  return {
    terminal_id: 'session:main:session',
    session_id: 'session',
    owner_id: 'main:session',
    execution_id: 'main:session',
    execution_kind: 'main_agent',
    label: 'main',
    scope: 'execution',
    workflow_path: 'Workflow/example',
    active: false,
    state: 'starting',
    content: '',
    rows: [],
    chunk_index: 0,
    status: {},
    created_at: '2026-07-14T00:00:00Z',
    updated_at: '2026-07-14T00:00:00Z',
    ...overrides,
  }
}

describe('isLiveWorkflowTerminal', () => {
  it('does not treat a provisional startup row as an attached terminal', () => {
    expect(isLiveWorkflowTerminal(terminal({ state: 'starting' }))).toBe(false)
  })

  it('retains a completed terminal while its tmux pane still exists', () => {
    expect(isLiveWorkflowTerminal(terminal({
      state: 'completed',
      tmux_session: 'mlp-claude-code-session',
    }))).toBe(true)
  })

  it('does not retain a terminal after the backend marks its pane stale', () => {
    expect(isLiveWorkflowTerminal(terminal({
      state: 'stale',
      tmux_session: 'mlp-claude-code-session',
    }))).toBe(false)
  })

  it('does not misreport foreground terminal activity as a background agent', () => {
    const session = activeSessionFromWorkflowTerminal(terminal({
      active: true,
      state: 'running',
      tmux_session: 'mlp-codex-cli-session',
    }))
    expect(session.has_running_background_agents).toBeUndefined()
    expect(session.has_retained_tmux_session).toBe(true)
  })
})
