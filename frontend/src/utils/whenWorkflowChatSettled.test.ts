// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('../services/api', () => ({ agentApi: {}, getApiBaseUrl: () => '', getAuthToken: () => null }))

import { whenWorkflowChatSettled } from './whenWorkflowChatSettled'
import { useChatStore } from '../stores/useChatStore'

describe('whenWorkflowChatSettled', () => {
  beforeEach(() => { vi.useFakeTimers(); useChatStore.setState({ restoringWorkflowSessions: {} }) })
  afterEach(() => vi.useRealTimers())

  it('resolves right away when nothing is restoring', async () => {
    const settled = vi.fn()
    void whenWorkflowChatSettled().then(settled)
    await vi.advanceTimersByTimeAsync(1)
    expect(settled).toHaveBeenCalled()
  })

  it('waits for the restoring chat, then resolves', async () => {
    useChatStore.setState({ restoringWorkflowSessions: { s1: 1 } })
    const settled = vi.fn()
    void whenWorkflowChatSettled().then(settled)
    await vi.advanceTimersByTimeAsync(500)
    expect(settled).not.toHaveBeenCalled()
    useChatStore.setState({ restoringWorkflowSessions: { s1: 0 } })
    await vi.advanceTimersByTimeAsync(0)
    expect(settled).toHaveBeenCalled()
  })

  it('never waits past the cap', async () => {
    useChatStore.setState({ restoringWorkflowSessions: { s1: 1 } })
    const settled = vi.fn()
    void whenWorkflowChatSettled(3000).then(settled)
    await vi.advanceTimersByTimeAsync(3000)
    expect(settled).toHaveBeenCalled()
  })
})
