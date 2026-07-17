import { describe, expect, it } from 'vitest'
import {
  findBlockingMultiAgentSession,
  shouldConfirmForSessionStatus,
  shouldConfirmNewMultiAgentChat,
  type NewChatConfirmationTabState,
} from './newChatConfirmation'
import type { ActiveSessionInfo } from '../services/api-types'

const baseTab: NewChatConfirmationTabState = {
  tabId: 'tab-1',
  sessionId: 'session-1',
  metadata: { mode: 'multi-agent' },
}

const activeSession = (overrides: Partial<ActiveSessionInfo>): ActiveSessionInfo => ({
  session_id: 'session-1',
  observer_id: '',
  agent_mode: 'multi-agent',
  status: 'completed',
  last_activity: '2026-06-29T00:00:00Z',
  created_at: '2026-06-29T00:00:00Z',
  ...overrides,
})

describe('new chat confirmation policy', () => {
  it('does not prompt for a completed session with no retained tmux', () => {
    expect(shouldConfirmNewMultiAgentChat(baseTab, activeSession({ status: 'completed' }))).toBe(false)
  })

  it('does not prompt when there is no matching active session', () => {
    expect(shouldConfirmNewMultiAgentChat(baseTab, null)).toBe(false)
    expect(shouldConfirmNewMultiAgentChat(baseTab, activeSession({ session_id: 'other', status: 'running' }))).toBe(false)
  })

  it('prompts for local running tab state before asking the backend', () => {
    expect(shouldConfirmNewMultiAgentChat({ ...baseTab, isStreaming: true })).toBe(true)
    expect(shouldConfirmNewMultiAgentChat({ ...baseTab, hasRunningBgAgents: true })).toBe(true)
  })

  it('prompts for a matching running, retained tmux, or input-waiting backend session', () => {
    expect(shouldConfirmNewMultiAgentChat(baseTab, activeSession({ status: 'running' }))).toBe(true)
    expect(shouldConfirmNewMultiAgentChat(baseTab, activeSession({ status: 'completed', has_retained_tmux_session: true }))).toBe(true)
    expect(shouldConfirmNewMultiAgentChat(baseTab, activeSession({ needs_user_input: true }))).toBe(true)
    expect(shouldConfirmNewMultiAgentChat(baseTab, activeSession({ has_running_background_agents: true }))).toBe(true)
  })

  it('ignores workflow tabs but still protects active organization chat sessions', () => {
    expect(shouldConfirmNewMultiAgentChat({ ...baseTab, metadata: { mode: 'workflow' } }, activeSession({ status: 'running' }))).toBe(false)
    expect(
      shouldConfirmNewMultiAgentChat(
        { ...baseTab, metadata: { mode: 'multi-agent', isOrganizationAssistant: true } },
        activeSession({ status: 'running' }),
      ),
    ).toBe(true)
  })

  it('treats active status and retained tmux as reset risks while completed is safe', () => {
    expect(shouldConfirmForSessionStatus({ session_id: 'session-1', status: 'active', can_steer: false })).toBe(true)
    expect(shouldConfirmForSessionStatus({ session_id: 'session-1', status: 'busy', can_steer: false })).toBe(true)
    expect(shouldConfirmForSessionStatus({ session_id: 'session-1', status: 'completed', can_steer: false })).toBe(false)
    expect(shouldConfirmForSessionStatus({ session_id: 'session-1', status: 'completed', can_steer: true })).toBe(false)
    expect(shouldConfirmForSessionStatus({ session_id: 'session-1', status: 'completed', has_retained_tmux_session: true })).toBe(true)
  })

  it('does not prompt only because a completed session is steerable', () => {
    expect(shouldConfirmNewMultiAgentChat(baseTab, activeSession({ status: 'completed' }))).toBe(false)
  })

  it('finds a blocking chief-of-staff backend session even when the tab session id is stale', () => {
    const blocker = activeSession({ session_id: 'live-session', status: 'busy' })
    const completed = activeSession({ session_id: 'old-session', status: 'completed' })
    const workflow = activeSession({ session_id: 'workflow-session', agent_mode: 'workflow_phase', status: 'running' })

    expect(findBlockingMultiAgentSession([completed, workflow, blocker], 'stale-session')).toBe(blocker)
    expect(findBlockingMultiAgentSession([completed, workflow], 'stale-session')).toBeNull()
  })

  it('prefers the active tab session when multiple chief-of-staff sessions would block reset', () => {
    const matching = activeSession({ session_id: 'session-1', status: 'running' })
    const retained = activeSession({ session_id: 'retained-session', status: 'completed', has_retained_tmux_session: true })

    expect(findBlockingMultiAgentSession([retained, matching], 'session-1')).toBe(matching)
  })

  it('does not let a Chief of Staff schedule block or get reset by New Chat', () => {
    const schedule = activeSession({
      session_id: 'schedule-cron--org-pulse_123',
      status: 'running',
      triggered_by: 'cron',
    })
    const scheduleTab = {
      ...baseTab,
      sessionId: schedule.session_id,
      metadata: { mode: 'multi-agent' as const, isScheduledRun: true },
      isStreaming: true,
    }

    expect(findBlockingMultiAgentSession([schedule])).toBeNull()
    expect(shouldConfirmNewMultiAgentChat(scheduleTab, schedule)).toBe(false)
  })
})
