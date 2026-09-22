import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  setTabEvents: vi.fn(),
  setTabLastEventIndex: vi.fn(),
  setTabHasMoreOlderEvents: vi.fn(),
  setTabHistoryPagination: vi.fn(),
  getTabEvents: vi.fn(),
  getRecentChatEvents: vi.fn(),
}))

vi.mock('../stores/useChatStore', () => ({
  useChatStore: { getState: () => ({
    setTabEvents: mocks.setTabEvents,
    setTabLastEventIndex: mocks.setTabLastEventIndex,
    setTabHasMoreOlderEvents: mocks.setTabHasMoreOlderEvents,
    setTabHistoryPagination: mocks.setTabHistoryPagination,
    getTabEvents: mocks.getTabEvents,
  }) },
}))
vi.mock('../stores/useModeStore', () => ({ useModeStore: { getState: () => ({ setModeCategory: vi.fn() }) } }))
vi.mock('../services/api', () => ({ agentApi: { getRecentChatEvents: mocks.getRecentChatEvents } }))

import { invalidateChatIdentity } from './chatIdentity'
import { hydrateTabEvents } from './sessionRestore'

const response = (events: unknown[], latest = 2) => ({
  events,
  session_status: 'completed',
  last_processed_index: latest,
  latest_sequence: latest,
  oldest_sequence: events.length ? 1 : 0,
  has_more: false,
})

describe('durable SQLite hydration isolation', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    invalidateChatIdentity()
    mocks.getTabEvents.mockReturnValue([])
  })

  it('ignores an older request that resolves after a newer page', async () => {
    let resolveOld!: (value: ReturnType<typeof response>) => void
    mocks.getRecentChatEvents
      .mockImplementationOnce(() => new Promise(done => { resolveOld = done }))
      .mockResolvedValueOnce(response([{ id: 'answer', type: 'streaming_chunk', sequence: 2 }]))

    const oldRequest = hydrateTabEvents('chat')
    await hydrateTabEvents('chat')
    resolveOld(response([{ id: 'question', type: 'user_message', sequence: 1 }], 1))
    await oldRequest

    expect(mocks.setTabEvents).toHaveBeenCalledTimes(1)
    expect(mocks.setTabEvents).toHaveBeenLastCalledWith('chat', [expect.objectContaining({ id: 'answer' })])
  })

  it('rejects a response from the previous account before writing state', async () => {
    let resolve!: (value: ReturnType<typeof response>) => void
    mocks.getRecentChatEvents.mockImplementation(() => new Promise(done => { resolve = done }))
    const pending = hydrateTabEvents('shared-session')
    invalidateChatIdentity()
    resolve(response([{ id: 'private', type: 'user_message' }]))
    await expect(pending).rejects.toThrow('Chat account changed')
    expect(mocks.setTabEvents).not.toHaveBeenCalled()
  })
})
