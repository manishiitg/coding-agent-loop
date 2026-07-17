import { describe, expect, it } from 'vitest'
import type { ActiveSessionInfo, RuntimePhase, RuntimeSnapshot } from '../services/api-types'
import {
  executionTreeRuntimeStatus,
  runtimeCanSteer,
  runtimeHasBackgroundAgents,
  runtimeNeedsUserInput,
  sessionRuntimeStatus,
} from './runtimeActivity'

function runtime(phase: RuntimePhase, overrides: Partial<RuntimeSnapshot> = {}): RuntimeSnapshot {
  return {
    session_id: 'session-1', generation: 1, revision: 7, phase,
    foreground_turn: { busy: false, has_cancel: false, can_steer: false, synthetic: false },
    background_live: false, terminal_busy: false, waiting_for_user: false,
    last_progress_at: '2026-07-17T00:00:00Z', started_at: '2026-07-17T00:00:00Z',
    observed_at: '2026-07-17T00:00:00Z',
    ...overrides,
  }
}

function session(state: RuntimeSnapshot): ActiveSessionInfo {
  return {
    session_id: state.session_id, observer_id: '', agent_mode: 'workflow', status: 'completed',
    last_activity: state.last_progress_at, created_at: state.started_at, runtime_state: state,
    // Deliberately contradictory legacy values prove runtime_state wins.
    display_status: 'stopped', has_running_background_agents: false, can_steer: false,
  }
}

describe('authoritative runtime activity selector', () => {
  it.each([
    ['starting', 'busy'], ['running', 'busy'], ['waiting', 'idle'], ['idle', 'idle'],
    ['completed', 'stopped'], ['failed', 'stopped'], ['canceled', 'stopped'],
  ] as const)('maps %s to %s consistently', (phase, expected) => {
    expect(sessionRuntimeStatus(session(runtime(phase)))).toBe(expected)
  })

  it('uses the same runtime revision for execution-tree status', () => {
    const state = runtime('running')
    expect(executionTreeRuntimeStatus({
      session_id: state.session_id,
      root: { execution_id: 'root', session_id: state.session_id, kind: 'session', name: 'Session', status: 'running', started_at: state.started_at },
      summary: {
        session_id: state.session_id, session_status: 'completed', display_status: 'stopped', is_session_busy: false,
        running_count: 0, completed_count: 2, failed_count: 0, canceled_count: 0,
        has_running_main_agent: false, has_running_background_agents: false, has_running_tracked_executions: false,
      },
      runtime_state: state,
    })).toBe('busy')
  })

  it('takes background, waiting, and steering signals only from runtime_state when present', () => {
    const value = session(runtime('waiting', {
      background_live: true,
      waiting_for_user: true,
      foreground_turn: { busy: false, has_cancel: false, can_steer: true, synthetic: false },
    }))
    expect(runtimeHasBackgroundAgents(value)).toBe(true)
    expect(runtimeNeedsUserInput(value)).toBe(true)
    expect(runtimeCanSteer(value)).toBe(true)
  })

  it('does not expose stale live children after a terminal runtime boundary', () => {
    const value = session(runtime('canceled', { background_live: true, terminal_busy: true }))
    expect(runtimeHasBackgroundAgents(value)).toBe(false)
    expect(sessionRuntimeStatus(value)).toBe('stopped')
  })
})
