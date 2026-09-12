import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const getRecentSessionEvents = vi.fn()
const getSessionEvents = vi.fn()
const getChatHistoryResumeConversation = vi.fn()

vi.mock('../services/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/api')>()
  return {
    ...actual,
    agentApi: {
      ...actual.agentApi,
      getRecentSessionEvents,
      getSessionEvents,
      getChatHistoryResumeConversation,
    },
  }
})

const createMemoryStorage = (): Storage => {
  const values = new Map<string, string>()
  return {
    get length() { return values.size },
    clear: () => values.clear(),
    getItem: (key) => values.get(key) ?? null,
    key: (index) => Array.from(values.keys())[index] ?? null,
    removeItem: (key) => { values.delete(key) },
    setItem: (key, value) => { values.set(key, value) },
  }
}

describe('session restore chat-history fallback', () => {
  beforeEach(() => {
    vi.resetModules()
    vi.stubGlobal('localStorage', createMemoryStorage())
    getRecentSessionEvents.mockReset()
    getSessionEvents.mockReset()
    getChatHistoryResumeConversation.mockReset()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('prefers the durable transcript over a partial runtime event buffer for every product', async () => {
    getRecentSessionEvents.mockResolvedValue({
      events: [{
        id: 'live-user-only',
        type: 'user_message',
        session_id: 'video-studio:project:launch',
        event_index: 0,
        timestamp: '2026-08-17T00:00:00Z',
        data: { content: 'Create the launch teaser.' },
      }],
      session_status: 'completed',
      has_running_background_agents: false,
      is_synthetic_turn: false,
      can_steer: false,
    })
    getChatHistoryResumeConversation.mockResolvedValue({
      session_id: 'video-studio:project:launch',
      conversation_history: [
        { Role: 'user', Parts: [{ Text: 'Create the launch teaser.' }] },
        { Role: 'assistant', Parts: [{ Text: 'The finished teaser is ready.' }] },
      ],
    })

    const { useChatStore, waitForChatStoreHydration } = await import('../stores/useChatStore')
    const { restoreSession } = await import('./sessionRestore')
    await waitForChatStoreHydration()
    const workspacePath = 'Chats/Video Studio/projects/launch'
    const tabId = await useChatStore.getState().createChatTab('Launch teaser', {
      mode: 'multi-agent',
      agentProfileId: 'agentworks',
      agentProfileVersion: 1,
      agentProfileWorkspace: workspacePath,
    }, 'video-studio:project:launch')

    await expect(restoreSession('video-studio:project:launch', {
      source: 'page-refresh',
      workspacePath,
    })).resolves.toBe(tabId)

    expect(getChatHistoryResumeConversation).toHaveBeenCalledWith(
      'video-studio:project:launch',
      workspacePath,
      20,
      0,
      true,
    )
    expect(useChatStore.getState().getTabEvents('video-studio:project:launch').map((event) => event.type)).toEqual([
      'conversation_resumed',
      'user_message',
      'llm_generation_end',
      'unified_completion',
    ])
    expect(useChatStore.getState().chatTabs[tabId]?.isStreaming).toBe(false)
  })

  it('shares one durable restore when surface and page refresh race', async () => {
    let resolveLiveEvents: ((value: unknown) => void) | undefined
    getRecentSessionEvents.mockImplementation(() => new Promise((resolve) => {
      resolveLiveEvents = resolve
    }))
    getChatHistoryResumeConversation.mockResolvedValue({
      session_id: 'video-studio:project:race',
      conversation_history: [
        { Role: 'user', Parts: [{ Text: 'Show the finished clip.' }] },
        { Role: 'assistant', Parts: [{ Text: 'Here is the completed video.' }] },
      ],
    })

    const { useChatStore, waitForChatStoreHydration } = await import('../stores/useChatStore')
    const { restoreSession } = await import('./sessionRestore')
    await waitForChatStoreHydration()
    const workspacePath = 'Chats/Video Studio/projects/race'
    const tabId = await useChatStore.getState().createChatTab('Race', {
      mode: 'multi-agent',
      agentProfileId: 'video-studio',
      agentProfileVersion: 1,
      agentProfileWorkspace: workspacePath,
    }, 'video-studio:project:race')

    const genericRestore = restoreSession('video-studio:project:race', {
      source: 'page-refresh',
    })
    const productRestore = restoreSession('video-studio:project:race', {
      source: 'surface-open',
      workspacePath,
    })
    resolveLiveEvents?.({
      events: [{
        id: 'live-user-only',
        type: 'user_message',
        session_id: 'video-studio:project:race',
        event_index: 0,
        timestamp: '2026-08-17T00:00:00Z',
        data: { content: 'Show the finished clip.' },
      }],
      session_status: 'completed',
      has_running_background_agents: false,
      is_synthetic_turn: false,
      can_steer: false,
    })

    await expect(Promise.all([genericRestore, productRestore])).resolves.toEqual([tabId, tabId])
    expect(getChatHistoryResumeConversation).toHaveBeenCalledWith(
      'video-studio:project:race',
      workspacePath,
      20,
      0,
      true,
    )
    expect(useChatStore.getState().getTabEvents('video-studio:project:race').map((event) => event.type)).toEqual([
      'conversation_resumed',
      'user_message',
      'llm_generation_end',
      'unified_completion',
    ])
  })

  it('recovers a durable transcript when the live cursor is rejected', async () => {
    const { useChatStore, waitForChatStoreHydration } = await import('../stores/useChatStore')
    const { restoreSession } = await import('./sessionRestore')
    await waitForChatStoreHydration()
    const workspacePath = 'Chats/Video Studio/projects/cursor-recovery'
    const sessionId = 'video-studio:project:cursor-recovery'
    const tabId = await useChatStore.getState().createChatTab('Cursor recovery', {
      mode: 'multi-agent',
      agentProfileId: 'video-studio',
      agentProfileVersion: 1,
      agentProfileWorkspace: workspacePath,
    }, sessionId)
    useChatStore.getState().setTabEvents(sessionId, [{
      id: 'stale-live-user',
      type: 'user_message',
      session_id: sessionId,
      event_index: 42,
      timestamp: '2026-08-17T00:00:00Z',
      data: { type: 'user_message', data: { content: 'Please show the preview.' } },
    }])
    getSessionEvents.mockRejectedValue(new Error('stale event cursor'))
    getChatHistoryResumeConversation.mockResolvedValue({
      session_id: sessionId,
      conversation_history: [
        { Role: 'user', Parts: [{ Text: 'Please show the preview.' }] },
        { Role: 'assistant', Parts: [{ Text: 'The preview is available below.' }] },
      ],
    })

    await expect(restoreSession(sessionId, {
      source: 'page-refresh',
      workspacePath,
    })).resolves.toBe(tabId)

    expect(getSessionEvents).toHaveBeenCalledWith(sessionId, -1)
    expect(useChatStore.getState().getTabEvents(sessionId).map((event) => event.type)).toEqual([
      'conversation_resumed',
      'user_message',
      'llm_generation_end',
      'unified_completion',
    ])
  })

  it('restores prior assistant replies while the latest turn is still running', async () => {
    const { useChatStore, waitForChatStoreHydration } = await import('../stores/useChatStore')
    const { restoreSession } = await import('./sessionRestore')
    await waitForChatStoreHydration()
    const workspacePath = 'Chats/Video Studio/projects/running-history'
    const sessionId = 'video-studio:project:running-history'
    const tabId = await useChatStore.getState().createChatTab('Running history', {
      mode: 'multi-agent',
      agentProfileId: 'video-studio',
      agentProfileVersion: 1,
      agentProfileWorkspace: workspacePath,
    }, sessionId)
    useChatStore.getState().setTabEvents(sessionId, [{
      id: 'live-user-only',
      type: 'user_message',
      session_id: sessionId,
      event_index: 0,
      timestamp: '2026-08-17T00:00:00Z',
      data: { type: 'user_message', data: { content: 'Continue the production.' } },
    }])
    getSessionEvents.mockResolvedValue({
      events: [],
      session_status: 'running',
      has_running_background_agents: false,
      is_synthetic_turn: false,
      can_steer: true,
    })
    getChatHistoryResumeConversation.mockResolvedValue({
      session_id: sessionId,
      conversation_history: [
        { Role: 'user', Parts: [{ Text: 'Continue the production.' }] },
        { Role: 'assistant', Parts: [{ Text: 'I am preparing the next scene.' }] },
      ],
    })

    await expect(restoreSession(sessionId, {
      source: 'page-refresh',
      workspacePath,
    })).resolves.toBe(tabId)

    expect(useChatStore.getState().getTabEvents(sessionId).map((event) => event.type)).toEqual([
      'conversation_resumed',
      'user_message',
      'llm_generation_end',
      'unified_completion',
    ])
    expect(useChatStore.getState().chatTabs[tabId]?.isStreaming).toBe(true)
  })
})
