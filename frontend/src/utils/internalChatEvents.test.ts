import { describe, expect, it } from 'vitest'
import type { PollingEvent } from '../services/api-types'
import { isInternalAutoNotificationEvent } from './internalChatEvents'

const event = (data: unknown): PollingEvent => ({
  id: 'event-1',
  type: 'user_message',
  data,
} as PollingEvent)

describe('internal chat event visibility', () => {
  it('recognizes nested and direct auto-notification user messages', () => {
    expect(isInternalAutoNotificationEvent(event({
      data: { content: '[AUTO-NOTIFICATION] Agent completed' },
    }))).toBe(true)
    expect(isInternalAutoNotificationEvent(event({
      content: '  [AUTO-NOTIFICATION] Started: background task',
    }))).toBe(true)
  })

  it('keeps real user messages and non-user events visible', () => {
    expect(isInternalAutoNotificationEvent(event({
      data: { content: 'Please summarize the run' },
    }))).toBe(false)
    expect(isInternalAutoNotificationEvent({
      ...event({ data: { content: '[AUTO-NOTIFICATION] Agent completed' } }),
      type: 'background_agent_completed',
    } as PollingEvent)).toBe(false)
  })
})
