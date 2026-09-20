import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  addTabEvents: vi.fn(),
  setTabEvents: vi.fn(),
  patchTabEvents: vi.fn(),
  setTabLastEventIndex: vi.fn(),
  setTabHasMoreOlderEvents: vi.fn(),
  setTabHistoryPagination: vi.fn(),
  getTabEvents: vi.fn(),
  getRecentSessionEvents: vi.fn(),
  getChatHistoryConversation: vi.fn(),
  getChatHistoryResumeConversation: vi.fn(),
}))

vi.mock('../stores/useChatStore', () => ({
  useChatStore: {
    getState: () => ({
      addTabEvents: mocks.addTabEvents,
      setTabEvents: mocks.setTabEvents,
      patchTabEvents: mocks.patchTabEvents,
      setTabLastEventIndex: mocks.setTabLastEventIndex,
      setTabHasMoreOlderEvents: mocks.setTabHasMoreOlderEvents,
      setTabHistoryPagination: mocks.setTabHistoryPagination,
      getTabEvents: mocks.getTabEvents,
    }),
  },
}))

vi.mock('../stores/useModeStore', () => ({
  useModeStore: { getState: () => ({ setModeCategory: vi.fn() }) },
}))

vi.mock('../services/api', () => ({
  agentApi: {
    getRecentSessionEvents: mocks.getRecentSessionEvents,
    getChatHistoryConversation: mocks.getChatHistoryConversation,
    getChatHistoryResumeConversation: mocks.getChatHistoryResumeConversation,
  },
}))

import { hydrateTabEvents } from './sessionRestore'
import { invalidateChatIdentity } from './chatIdentity'

describe('durable hydration isolation', () => {
  beforeEach(() => { vi.resetAllMocks(); invalidateChatIdentity() })
  it('does not erase a newer durable answer when an older hydration resolves last', async () => {
    vi.resetAllMocks()
    let stored: any[] = []
    mocks.getTabEvents.mockImplementation(() => stored)
    mocks.setTabEvents.mockImplementation((_id, events) => { stored = events })
    mocks.getRecentSessionEvents.mockResolvedValue({events: [], session_status: 'completed', last_processed_index: -1})
    let resolveOld!: (value: any) => void
    mocks.getChatHistoryResumeConversation.mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
    mocks.getChatHistoryResumeConversation.mockResolvedValueOnce({session_id: 'audit', conversation_history: [
      {Role: 'human', Parts: [{Text: 'Remember the release name'}]},
      {Role: 'ai', Parts: [{Text: 'The release name is ORCHID'}]},
    ]})
    const oldHydration = hydrateTabEvents('audit')
    await hydrateTabEvents('audit')
    expect(JSON.stringify(stored)).toContain('ORCHID')
    resolveOld({session_id: 'audit', conversation_history: [{Role: 'human', Parts: [{Text: 'Remember the release name'}]}]})
    await oldHydration
    expect(JSON.stringify(stored)).toContain('ORCHID')
  })
  it('keeps the newer durable answer when a newer request receives a shorter server snapshot', async () => {
    let stored: any[] = []
    mocks.getTabEvents.mockImplementation(() => stored)
    mocks.setTabEvents.mockImplementation((_id, events) => { stored = events })
    mocks.getRecentSessionEvents.mockResolvedValue({ events: [], session_status: 'completed' })
    const user = { Role: 'human', Parts: [{ Text: 'Remember ORCHID' }] }
    mocks.getChatHistoryResumeConversation.mockResolvedValueOnce({session_id: 'lag', conversation_history: [user,
      { Role: 'ai', Parts: [{ Text: 'ORCHID remembered' }] },
    ]})
    await hydrateTabEvents('lag')
    mocks.getChatHistoryResumeConversation.mockResolvedValueOnce({ session_id: 'lag', conversation_history: [user] })
    await hydrateTabEvents('lag')
    expect(JSON.stringify(stored)).toContain('ORCHID remembered')
  })

  it('rejects the old account response without writing events or cursors', async () => {
    mocks.getTabEvents.mockReturnValue([])
    mocks.getRecentSessionEvents.mockResolvedValue({ events: [], session_status: 'completed' })
    let resolve!: (value: any) => void
    mocks.getChatHistoryResumeConversation.mockImplementation(() => new Promise(done => { resolve = done }))
    const pending = hydrateTabEvents('shared-session-id')
    invalidateChatIdentity()
    resolve({ session_id: 'shared-session-id', conversation_history: [{ Role: 'human', Parts: [{ Text: 'Private old account' }] }] })
    await expect(pending).rejects.toThrow('Chat account changed')
    expect(mocks.setTabEvents).not.toHaveBeenCalled()
    expect(mocks.setTabLastEventIndex).not.toHaveBeenCalled()
  })

  it('rejects lower authoritative revisions even if they contain more rows', async () => {
    let stored: any[] = []
    mocks.getTabEvents.mockImplementation(() => stored)
    mocks.setTabEvents.mockImplementation((_id, events) => { stored = events })
    mocks.getRecentSessionEvents.mockResolvedValue({ events: [], session_status: 'completed' })
    mocks.getChatHistoryResumeConversation.mockResolvedValueOnce({ session_id: 'revised', revision: 9, conversation_history: [
      { Role: 'human', Parts: [{ Text: 'Latest turn' }] }, { Role: 'ai', Parts: [{ Text: 'Latest durable answer' }] },
    ] })
    await hydrateTabEvents('revised')
    mocks.getChatHistoryResumeConversation.mockResolvedValueOnce({ session_id: 'revised', revision: 8, conversation_history: [
      { Role: 'human', Parts: [{ Text: 'Old turn' }] }, { Role: 'ai', Parts: [{ Text: 'Stale durable answer' }] }, { Role: 'human', Parts: [{ Text: 'Old append' }] },
    ] })
    await hydrateTabEvents('revised')
    expect(JSON.stringify(stored)).toContain('Latest durable answer')
    expect(JSON.stringify(stored)).not.toContain('Stale durable answer')
  })

})
