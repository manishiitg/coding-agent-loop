import { describe, expect, it } from 'vitest'
import { hasLiveBackgroundAgents, isTerminalActivityStatus } from './activitySessions'

describe('activity session helpers', () => {
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
