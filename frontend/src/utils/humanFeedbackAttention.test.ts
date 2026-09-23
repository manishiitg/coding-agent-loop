import { describe, expect, it } from 'vitest'
import type { PollingEvent } from '../services/api-types'
import { collectPendingHumanFeedback, getBlockingHumanFeedbackDetails } from './humanFeedbackAttention'

describe('getBlockingHumanFeedbackDetails', () => {
  it('extracts the request from the backend event envelope', () => {
    const event = {
      id: 'feedback-event',
      type: 'blocking_human_feedback',
      timestamp: new Date().toISOString(),
      data: {
        type: 'blocking_human_feedback',
        data: {
          request_id: 'request-123',
          question: 'Enter the OTP',
        },
      },
    } as PollingEvent

    const details = getBlockingHumanFeedbackDetails(event)
    expect(details).toMatchObject({
      requestId: 'request-123',
      question: 'Enter the OTP',
      displayEvent: {
        type: 'blocking_human_feedback',
        data: {
          request_id: 'request-123',
          question: 'Enter the OTP',
        },
      },
    })
  })

  it('rejects unrelated or malformed events', () => {
    const unrelated = { type: 'tool_call_start' } as PollingEvent
    const missingRequest = {
      type: 'blocking_human_feedback',
      data: { type: 'blocking_human_feedback', data: {} },
    } as PollingEvent

    expect(getBlockingHumanFeedbackDetails(unrelated)).toBeNull()
    expect(getBlockingHumanFeedbackDetails(missingRequest)).toBeNull()
  })

  it('collects unsubmitted requests across tabs and drops expired requests', () => {
    const now = Date.parse('2026-07-18T10:05:00.000Z')
    const active = {
      id: 'active-feedback',
      type: 'blocking_human_feedback',
      timestamp: '2026-07-18T10:04:00.000Z',
      data: {
        type: 'blocking_human_feedback',
        data: {
          request_id: 'active',
          question: 'Enter OTP',
          context: 'This request expires in 300 seconds.',
        },
      },
    } as PollingEvent
    const expired = {
      ...active,
      id: 'expired-feedback',
      timestamp: '2026-07-18T09:55:00.000Z',
      data: {
        type: 'blocking_human_feedback',
        data: {
          request_id: 'expired',
          context: 'This request expires in 30 seconds.',
        },
      },
    } as PollingEvent

    const result = collectPendingHumanFeedback(
      { 'background-session': [expired, active] },
      (requestId) => requestId === 'already-submitted',
      now,
    )

    expect(result).toHaveLength(1)
    expect(result[0]).toMatchObject({ requestId: 'active', sessionId: 'background-session' })
  })

  it('treats a request with a durable resolution marker as answered after refresh', () => {
    const now = Date.parse('2026-07-18T10:05:00.000Z')
    const request = {
      id: 'answered-feedback',
      type: 'blocking_human_feedback',
      timestamp: '2026-07-18T10:04:00.000Z',
      data: {
        type: 'blocking_human_feedback',
        data: { request_id: 'answered', question: 'Approve?', context: 'This request expires in 300 seconds.' },
      },
    } as PollingEvent
    const resolution = {
      id: 'answered-resolved',
      type: 'human_feedback_resolved',
      timestamp: '2026-07-18T10:04:30.000Z',
      data: {
        type: 'human_feedback_resolved',
        data: { request_id: 'answered', outcome: 'answered', event_type: 'human_feedback_resolved' },
      },
    } as unknown as PollingEvent

    // No local submission record: the answer came from another device or Slack.
    expect(collectPendingHumanFeedback({ chat: [request] }, () => false, now)).toHaveLength(1)
    expect(collectPendingHumanFeedback({ chat: [request, resolution] }, () => false, now)).toHaveLength(0)
  })
})
