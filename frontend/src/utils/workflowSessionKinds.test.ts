import { describe, expect, it } from 'vitest'
import { workflowTriggerLabel, isExternalReadOnlyWorkflowSession, isInternalChildSession, isScheduledSession } from './workflowSessionKinds'

describe('isExternalReadOnlyWorkflowSession', () => {
  it.each([
    { sessionId: 'schedule-cron--abc_123' },
    { sessionId: 'schedule-manual--abc_123' },
    { sessionId: 'workflow-schedule-run-123' },
    { sessionId: 'bot-slack-123' },
    { sessionId: 'bot-123' },
    { sessionId: 'session-123', triggeredBy: 'cron' },
    { sessionId: 'session-123', triggeredBy: 'schedule-manual' },
    { sessionId: 'session-123', botPlatform: 'whatsapp' },
  ])('recognizes an independent schedule or bot lane: %o', identity => {
    expect(isExternalReadOnlyWorkflowSession(identity)).toBe(true)
  })

  it('does not classify an interactive builder chat as external', () => {
    expect(isExternalReadOnlyWorkflowSession({
      sessionId: 'f5df36c5-acae-496c-8255-757cb36d9db0',
      triggeredBy: 'user',
    })).toBe(false)
  })

  it('distinguishes schedules from bot sessions', () => {
    expect(isScheduledSession({ sessionId: 'schedule-cron--abc_123' })).toBe(true)
    expect(isScheduledSession({ sessionId: 'session-123', triggeredBy: 'cron' })).toBe(true)
    expect(isScheduledSession({ sessionId: 'bot-slack-123', botPlatform: 'slack' })).toBe(false)
  })
})

describe('isInternalChildSession', () => {
  it('recognizes an explicitly parented Pulse reviewer', () => {
    expect(isInternalChildSession({
      parentSessionId: 'pulse-root-1',
      sessionKind: 'pulse_reviewer',
    })).toBe(true)
  })

  it('does not hide a top-level Pulse or workflow session', () => {
    expect(isInternalChildSession({ sessionKind: 'pulse' })).toBe(false)
  })
})


describe('workflow trigger labels', () => {
  it('identifies older webhook sessions even when stamped as cron', () => {
    expect(workflowTriggerLabel({ sessionId: 'schedule-webhook--abc_123', triggeredBy: 'cron' })).toBe('Webhook')
  })
  it('preserves webhook read-only behavior with explicit metadata alone', () => {
    expect(isExternalReadOnlyWorkflowSession({ sessionId: 'session-123', triggeredBy: 'webhook' })).toBe(true)
    expect(workflowTriggerLabel({ triggeredBy: 'webhook' })).toBe('Webhook')
  })
  it('distinguishes time triggers, manual launches, and ordinary chats', () => {
    expect(workflowTriggerLabel({ sessionId: 'schedule-cron--abc_123' })).toBe('Scheduled')
    expect(workflowTriggerLabel({ sessionId: 'schedule-manual--abc_123', triggeredBy: 'cron' })).toBe('Manual')
    expect(workflowTriggerLabel({ sessionId: 'bot-slack-123' })).toBeUndefined()
    expect(workflowTriggerLabel({ sessionId: 'chat-123' })).toBeUndefined()
  })
})
