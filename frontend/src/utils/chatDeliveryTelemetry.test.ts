// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { PollingEvent } from '../services/api-types'
import {
  configureChatDeliveryTelemetryTransport,
  recordChatDeliveryTelemetry,
  type ChatDeliveryTelemetryBatch,
} from './chatDeliveryTelemetry'

afterEach(() => vi.useRealTimers())

describe('chat delivery telemetry', () => {
  it('ships exact timing milestones without message content', async () => {
    vi.useFakeTimers()
    const batches: ChatDeliveryTelemetryBatch[] = []
    configureChatDeliveryTelemetryTransport(async batch => { batches.push(batch) })
    const event = {
      id: 'event-1',
      type: 'unified_completion',
      session_id: 'session-1',
      timestamp: '2026-09-22T07:10:20.597Z',
      data: { data: { content: 'sensitive reply text' } },
    } as PollingEvent

    recordChatDeliveryTelemetry('sse_received', 'session-1', [event], 'sse', 'tab-1')
    await vi.advanceTimersByTimeAsync(101)

    expect(batches).toHaveLength(1)
    expect(batches[0].events[0]).toMatchObject({
      phase: 'sse_received',
      session_id: 'session-1',
      event_id: 'event-1',
      event_type: 'unified_completion',
      transport: 'sse',
      tab_id: 'tab-1',
      server_event_time: '2026-09-22T07:10:20.597Z',
    })
    expect(JSON.stringify(batches[0])).not.toContain('sensitive reply text')
  })
})
