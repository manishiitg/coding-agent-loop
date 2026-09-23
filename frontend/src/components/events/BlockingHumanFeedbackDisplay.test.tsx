// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('../../services/api', () => ({
  agentApi: {},
  getApiBaseUrl: () => '',
  getAuthToken: () => null,
}))

import { BlockingHumanFeedbackDisplay } from './BlockingHumanFeedbackDisplay'
import { useChatStore } from '../../stores/useChatStore'
import type { PollingEvent } from '../../services/api-types'

// eslint-disable-next-line @typescript-eslint/no-explicit-any
;(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true

const requestEvent = {
  type: 'blocking_human_feedback',
  data: { question: 'Deploy now?', request_id: 'req-resolved-elsewhere', allow_feedback: false },
  timestamp: '2026-09-23T00:00:00Z',
}

function resolutionMarker(requestId: string): PollingEvent {
  return {
    id: `resolved-${requestId}`,
    type: 'human_feedback_resolved',
    timestamp: '2026-09-23T00:01:00Z',
    data: { data: { request_id: requestId, resolution: 'answered' } },
  } as unknown as PollingEvent
}

describe('BlockingHumanFeedbackDisplay', () => {
  let container: HTMLDivElement
  let root: Root

  beforeEach(() => {
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
    useChatStore.setState({ tabEvents: {} })
  })

  afterEach(() => {
    act(() => root.unmount())
    container.remove()
    useChatStore.setState({ tabEvents: {} })
  })

  it('shows the form while no resolution marker exists', () => {
    act(() => root.render(<BlockingHumanFeedbackDisplay event={requestEvent} onApprove={() => {}} surfaceNotifications={false} />))
    expect(container.textContent).not.toContain('Response submitted')
  })

  it('treats a durable resolution marker from another surface as answered', () => {
    useChatStore.setState({ tabEvents: { 'session-a': [resolutionMarker('req-resolved-elsewhere')] } })
    act(() => root.render(<BlockingHumanFeedbackDisplay event={requestEvent} onApprove={() => {}} surfaceNotifications={false} />))
    expect(container.textContent).toContain('Response submitted')
  })
})
