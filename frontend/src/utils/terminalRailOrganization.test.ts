import { describe, expect, it } from 'vitest'
import type { TerminalSnapshot } from '../services/api-types'
import {
  hiddenSelectedTerminalRailGroup,
  organizeTerminalRail,
  terminalRailLogicalKey,
  terminalRailTitle,
} from './terminalRailOrganization'

const terminal = (id: string, overrides: Partial<TerminalSnapshot> = {}): TerminalSnapshot => ({
  terminal_id: id,
  session_id: 'session-1',
  content: '',
  rows: [],
  chunk_index: 1,
  active: false,
  state: 'completed',
  status: { provider_label: 'Claude Code' },
  created_at: '2026-07-14T00:00:00Z',
  updated_at: '2026-07-14T00:01:00Z',
  ...overrides,
})

const organize = (terminals: TerminalSnapshot[]) => organizeTerminalRail(terminals, {
  getState: item => item.state || (item.active ? 'running' : 'completed'),
  isMainAgent: item => item.execution_kind === 'main_agent',
})

describe('terminal rail organization', () => {
  it('keeps the main agent out of logical task groups', () => {
    const groups = organize([
      terminal('main', { execution_kind: 'main_agent' }),
      terminal('step', { execution_kind: 'workflow_step', step_id: 'collect-price', step_name: 'Collect Price' }),
    ])

    expect(groups).toHaveLength(1)
    expect(groups[0].title).toBe('Collect Price')
  })

  it('collapses repeated attempts of the same workflow step', () => {
    const first = terminal('attempt-1', {
      execution_kind: 'workflow_step',
      step_id: 'collect-insider',
      step_name: 'Collect Insider Activity',
      step_attempt: 1,
    })
    const second = terminal('attempt-2', {
      execution_kind: 'workflow_step',
      step_id: 'collect-insider',
      step_name: 'Collect Insider Activity',
      step_attempt: 2,
      updated_at: '2026-07-14T00:02:00Z',
    })

    const groups = organize([first, second])

    expect(groups).toHaveLength(1)
    expect(groups[0].terminals.map(item => item.terminal_id)).toEqual(['attempt-2', 'attempt-1'])
    expect(groups[0].representative.terminal_id).toBe('attempt-2')
  })

  it('keeps a still-running earlier attempt as the group representative', () => {
    const running = terminal('attempt-1', {
      active: true,
      state: 'running',
      execution_kind: 'workflow_step',
      step_id: 'collect-social',
      step_name: 'Collect Social Momentum',
      step_attempt: 1,
    })
    const completed = terminal('attempt-2', {
      execution_kind: 'workflow_step',
      step_id: 'collect-social',
      step_name: 'Collect Social Momentum',
      step_attempt: 2,
      updated_at: '2026-07-14T00:02:00Z',
    })

    const groups = organize([running, completed])

    expect(groups[0].representative.terminal_id).toBe('attempt-1')
    expect(groups[0].terminals).toHaveLength(2)
    expect(groups[0].section).toBe('active')
  })

  it('promotes a restarted attempt of a stopped step into Active', () => {
    const stopped = terminal('stopped-attempt', {
      active: false,
      state: 'failed',
      execution_kind: 'workflow_step',
      step_id: 'survey-app-and-refresh-knowledge',
      step_name: 'Survey App and Refresh Knowledge',
      step_attempt: 1,
    })
    const restarted = terminal('restarted-attempt', {
      active: true,
      state: 'running',
      execution_kind: 'workflow_step',
      step_id: 'survey-app-and-refresh-knowledge',
      step_name: 'Survey App and Refresh Knowledge',
      step_attempt: 2,
      updated_at: '2026-07-14T00:02:00Z',
    })

    const groups = organize([stopped, restarted])

    expect(groups).toHaveLength(1)
    expect(groups[0].section).toBe('active')
    expect(groups[0].representative.terminal_id).toBe('restarted-attempt')
  })

  it('groups message-sequence turns under their owning step', () => {
    const first = terminal('turn-1', {
      step_type: 'message_sequence',
      step_id: 'message-sequence-load',
      parent_step_id: 'score-and-plan',
      agent_name: 'message-sequence-load',
    })
    const second = terminal('turn-2', {
      step_type: 'message_sequence',
      step_id: 'message-sequence-validate',
      parent_step_id: 'score-and-plan',
      agent_name: 'message-sequence-validate',
    })

    expect(terminalRailLogicalKey(first)).toBe(terminalRailLogicalKey(second))
    expect(terminalRailTitle(first)).toBe('Score and plan sequence')
    expect(organize([first, second])).toHaveLength(1)
  })

  it('puts live, failed, workflow, and reviewer tasks in distinct sections', () => {
    const groups = organize([
      terminal('running', { active: true, state: 'running', step_id: 'collect-price', step_name: 'Collect Price' }),
      terminal('failed', { state: 'failed', step_id: 'delivery', step_name: 'Deliver Briefing' }),
      terminal('done', { step_id: 'score', step_name: 'Score Ideas' }),
      terminal('review', { execution_kind: 'background_agent', agent_name: 'Evaluation Health Reviewer' }),
      terminal('underscore-review', { execution_kind: 'background_agent', agent_name: 'learning_health' }),
    ])

    expect(Object.fromEntries(groups.map(group => [group.title, group.section]))).toEqual({
      'Collect Price': 'active',
      'Deliver Briefing': 'attention',
      'Score Ideas': 'workflow',
      'Evaluation Health Reviewer': 'review',
      'Learning health': 'review',
    })
  })

  it('identifies only the selected completed child hidden by the active filter', () => {
    const active = terminal('active', {
      active: true,
      state: 'running',
      step_id: 'collect-price',
      step_name: 'Collect Price',
    })
    const selectedDone = terminal('selected-done', {
      step_id: 'score',
      step_name: 'Score Ideas',
      tmux_session: 'tmux-score',
    })
    const otherDone = terminal('other-done', {
      step_id: 'deliver',
      step_name: 'Deliver Briefing',
    })
    const groups = organize([active, selectedDone, otherDone])
    const visible = groups.filter(group => group.section === 'active')

    expect(hiddenSelectedTerminalRailGroup(groups, visible, selectedDone)?.title).toBe('Score Ideas')
    expect(hiddenSelectedTerminalRailGroup(groups, visible, otherDone)?.title).toBe('Deliver Briefing')
    expect(hiddenSelectedTerminalRailGroup(groups, groups, selectedDone)).toBeNull()
  })
})
