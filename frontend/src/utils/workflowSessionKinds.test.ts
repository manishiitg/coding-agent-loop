import { describe, expect, it } from 'vitest'
import { isExternalReadOnlyWorkflowSession, isScheduledSession } from './workflowSessionKinds'

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
