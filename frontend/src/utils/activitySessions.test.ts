import { describe, expect, it } from 'vitest'
import {
  hasActiveSessionWork,
  hasIdleAliveCodingAgent,
  hasLiveBackgroundAgents,
  isTerminalActivityStatus,
  RETAINED_TMUX_ACTIVE_WINDOW_MS,
} from './activitySessions'

describe('activity session helpers', () => {
  it('does not treat an idle retained session as active work', () => {
    expect(hasActiveSessionWork({ status: 'completed' })).toBe(false)
  })

  it('recognizes running and waiting sessions as active work', () => {
    expect(hasActiveSessionWork({ status: 'running' })).toBe(true)
    expect(hasActiveSessionWork({ status: 'waiting_feedback' })).toBe(true)
  })

  it('ignores stale background-agent flags on completed sessions', () => {
    expect(hasLiveBackgroundAgents({
      status: 'completed',
      has_running_background_agents: true,
      running_background_agent_count: 1,
    })).toBe(false)
  })

  it('ignores stale background-agent flags on stopped sessions', () => {
    expect(hasLiveBackgroundAgents({
      status: 'stopped',
      has_running_background_agents: true,
      running_background_agent_count: 1,
    })).toBe(false)
  })

  it('keeps background-agent flags for live sessions', () => {
    expect(hasLiveBackgroundAgents({
      status: 'running',
      has_running_background_agents: true,
      running_background_agent_count: 1,
    })).toBe(true)
  })

  it('recognizes terminal statuses', () => {
    expect(isTerminalActivityStatus('completed')).toBe(true)
    expect(isTerminalActivityStatus('stopped')).toBe(true)
    expect(isTerminalActivityStatus('running')).toBe(false)
  })
})

describe('hasIdleAliveCodingAgent', () => {
  const now = Date.parse('2026-07-03T09:00:00Z')

  it('is false when no retained tmux pane', () => {
    expect(hasIdleAliveCodingAgent({ has_retained_tmux_session: false, last_activity: '2026-07-03T08:59:00Z' }, now)).toBe(false)
    expect(hasIdleAliveCodingAgent({ last_activity: '2026-07-03T08:59:00Z' }, now)).toBe(false)
  })

  it('shows an idle-alive pane whose last activity is within the window', () => {
    // completed-but-alive: backend flipped status to completed, pane still up
    expect(hasIdleAliveCodingAgent({
      has_retained_tmux_session: true,
      last_activity: new Date(now - 5 * 60 * 1000).toISOString(),
    }, now)).toBe(true)
  })

  it('hides a pane abandoned longer than the window', () => {
    expect(hasIdleAliveCodingAgent({
      has_retained_tmux_session: true,
      last_activity: new Date(now - RETAINED_TMUX_ACTIVE_WINDOW_MS - 1000).toISOString(),
    }, now)).toBe(false)
  })

  it('shows a live pane with an unknown/unparseable timestamp (pane liveness is the signal)', () => {
    expect(hasIdleAliveCodingAgent({ has_retained_tmux_session: true, last_activity: '' }, now)).toBe(true)
    expect(hasIdleAliveCodingAgent({ has_retained_tmux_session: true, last_activity: 'not-a-date' }, now)).toBe(true)
  })
})
