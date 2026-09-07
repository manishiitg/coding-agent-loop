import { describe, expect, it, vi } from 'vitest'

import {
  createLiveInputSubmissionCoordinator,
  shouldRouteChatInputToLiveTransport,
  shouldAppendOptimisticLiveInputMessage,
  shouldRefreshSessionEventStream,
  shouldUseRetainedLiveInput,
} from './liveInputSubmission'

describe('shouldRouteChatInputToLiveTransport', () => {
  const base = {
    hasSession: true,
    isCodingAgentProvider: true,
    isWorkflowMode: true,
    usesStructuredTransport: false,
  }

  it('routes an interactive coding-agent chat through live input', () => {
    expect(shouldRouteChatInputToLiveTransport(base)).toBe(true)
  })

  it('routes structured Cursor follow-ups through the normal query path', () => {
    expect(shouldRouteChatInputToLiveTransport({ ...base, usesStructuredTransport: true })).toBe(false)
  })

  it('requires an existing backend session', () => {
    expect(shouldRouteChatInputToLiveTransport({ ...base, hasSession: false })).toBe(false)
  })
})

describe('createLiveInputSubmissionCoordinator', () => {
  it('executes a rapid duplicate live message exactly once', async () => {
    const submitLiveInput = createLiveInputSubmissionCoordinator()
    let resolve!: (value: boolean) => void
    const submit = vi.fn(() => new Promise<boolean>(done => { resolve = done }))

    const first = submitLiveInput('session-a', 'continue with this', submit)
    const duplicate = submitLiveInput('session-a', 'continue with this', submit)

    expect(duplicate).toBe(first)
    expect(submit).toHaveBeenCalledTimes(1)
    resolve(true)
    await expect(Promise.all([first, duplicate])).resolves.toEqual([true, true])
  })

  it('does not suppress different messages or a later intentional repeat', async () => {
    const submitLiveInput = createLiveInputSubmissionCoordinator()
    const submit = vi.fn(async (value: string) => value)

    await Promise.all([
      submitLiveInput('session-a', 'first', () => submit('first')),
      submitLiveInput('session-a', 'second', () => submit('second')),
    ])
    await submitLiveInput('session-a', 'first', () => submit('first-again'))

    expect(submit).toHaveBeenCalledTimes(3)
  })
})

describe('shouldUseRetainedLiveInput', () => {
  const base = {
    requested: true,
    fullTurnStreaming: true,
    turnIsStreaming: false,
    hasSession: true,
    hasOneShotContext: false,
  }

  it('starts a tracked turn for an idle product conversation', () => {
    expect(shouldUseRetainedLiveInput(base)).toBe(false)
  })

  it('preserves native tmux steering while the product turn is running', () => {
    expect(shouldUseRetainedLiveInput({ ...base, turnIsStreaming: true })).toBe(true)
  })

  it('keeps the existing AgentWorks retained-input behavior outside product full-turn mode', () => {
    expect(shouldUseRetainedLiveInput({ ...base, fullTurnStreaming: false })).toBe(true)
  })

  it('never uses retained input for one-shot context', () => {
    expect(shouldUseRetainedLiveInput({ ...base, turnIsStreaming: true, hasOneShotContext: true })).toBe(false)
  })
})

describe('shouldRefreshSessionEventStream', () => {
  it('reattaches a product stream even when a stale connection object remains', () => {
    expect(shouldRefreshSessionEventStream(true, true)).toBe(true)
  })

  it('reattaches an AgentWorks stream when an old connection wrapper remains', () => {
    expect(shouldRefreshSessionEventStream(false, true)).toBe(true)
  })

  it('connects any surface when no stream exists', () => {
    expect(shouldRefreshSessionEventStream(false, false)).toBe(true)
  })
})

describe('shouldAppendOptimisticLiveInputMessage', () => {
  it('does not append a second copy when SSE delivered the acknowledged message first', () => {
    const events = [{
      type: 'user_message',
      data: {
        data: {
          content: 'how are you',
          metadata: { message_id: 'steer-123' },
        },
      },
    }]

    expect(shouldAppendOptimisticLiveInputMessage(events, 'steer-123')).toBe(false)
  })

  it('still appends for a different acknowledgement or before the backend echo arrives', () => {
    const events = [{
      type: 'user_message',
      data: { data: { content: 'same text', metadata: { message_id: 'older' } } },
    }]

    expect(shouldAppendOptimisticLiveInputMessage(events, 'newer')).toBe(true)
    expect(shouldAppendOptimisticLiveInputMessage([], 'newer')).toBe(true)
  })
})
