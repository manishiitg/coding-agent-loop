import { describe, expect, it } from 'vitest'
import {
  isInterruptibleSessionStatus,
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
  it('does not prompt for a completed retained session', () => {
    expect(shouldConfirmNewMultiAgentChat(baseTab, activeSession({ status: 'completed' }))).toBe(false)
  })

  it('does not prompt when there is no matching active session', () => {
    expect(shouldConfirmNewMultiAgentChat(baseTab, null)).toBe(false)
    expect(shouldConfirmNewMultiAgentChat(baseTab, activeSession({ session_id: 'other', status: 'running' }))).toBe(false)
  })

  it('prompts for local running tab state before asking the backend', () => {
    expect(shouldConfirmNewMultiAgentChat({ ...baseTab, isStreaming: true })).toBe(true)
    expect(shouldConfirmNewMultiAgentChat({ ...baseTab, hasRunningBgAgents: true })).toBe(true)
    expect(shouldConfirmNewMultiAgentChat({ ...baseTab, canSteer: true })).toBe(true)
  })

  it('prompts for a matching running or input-waiting backend session', () => {
    expect(shouldConfirmNewMultiAgentChat(baseTab, activeSession({ status: 'running' }))).toBe(true)
    expect(shouldConfirmNewMultiAgentChat(baseTab, activeSession({ needs_user_input: true }))).toBe(true)
    expect(shouldConfirmNewMultiAgentChat(baseTab, activeSession({ has_running_background_agents: true }))).toBe(true)
  })

  it('ignores workflow and hidden org tabs', () => {
    expect(shouldConfirmNewMultiAgentChat({ ...baseTab, metadata: { mode: 'workflow' } }, activeSession({ status: 'running' }))).toBe(false)
    expect(
      shouldConfirmNewMultiAgentChat(
        { ...baseTab, metadata: { mode: 'multi-agent', isOrganizationAssistant: true } },
        activeSession({ status: 'running' }),
      ),
    ).toBe(false)
  })

  it('treats active session status as interruptible and completed as safe', () => {
    expect(isInterruptibleSessionStatus({ session_id: 'session-1', status: 'active', can_steer: false })).toBe(true)
    expect(isInterruptibleSessionStatus({ session_id: 'session-1', status: 'completed', can_steer: false })).toBe(false)
    expect(isInterruptibleSessionStatus({ session_id: 'session-1', status: 'completed', can_steer: true })).toBe(true)
  })
})
