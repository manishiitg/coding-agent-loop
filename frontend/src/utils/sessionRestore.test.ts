import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  addTabEvents: vi.fn(),
  setTabEvents: vi.fn(),
  setTabLastEventIndex: vi.fn(),
  setTabHasMoreOlderEvents: vi.fn(),
  setTabHistoryPagination: vi.fn(),
  getRecentSessionEvents: vi.fn(),
  getChatHistoryConversation: vi.fn(),
  getChatHistoryResumeConversation: vi.fn(),
}))

vi.mock('../stores/useChatStore', () => ({
  useChatStore: {
    getState: () => ({
      addTabEvents: mocks.addTabEvents,
      setTabEvents: mocks.setTabEvents,
      setTabLastEventIndex: mocks.setTabLastEventIndex,
      setTabHasMoreOlderEvents: mocks.setTabHasMoreOlderEvents,
      setTabHistoryPagination: mocks.setTabHistoryPagination,
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

import { conversationToRestoredEvents, hydrateTabEvents } from './sessionRestore'

describe('hydrateTabEvents restored chat fallback', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('prefers complete persisted history when reopening a chat', async () => {
    mocks.getRecentSessionEvents.mockResolvedValue({
      events: [{ id: 'volatile-tail-only', type: 'conversation_end' }],
      session_status: 'completed',
      last_processed_index: -1,
      has_more: false,
    })
    mocks.getChatHistoryResumeConversation.mockResolvedValue({
      session_id: 'restored-session',
      conversation_history: [
        { Role: 'human', Parts: [{ Text: 'Hello' }] },
        { Role: 'ai', Parts: [{ Text: 'I will inspect that.' }] },
        { Role: 'ai', Parts: [{ Text: '[Previous tool call: exec({})]' }] },
        { Role: 'ai', Parts: [{ Text: 'Hi there' }] },
      ],
    })

    await hydrateTabEvents('restored-session', {
      workspacePath: '/workspace/workflow',
      fallbackToChatHistory: true,
      preferChatHistory: true,
    })

    expect(mocks.getChatHistoryResumeConversation).toHaveBeenCalledWith(
      'restored-session',
      '/workspace/workflow',
      20,
      0,
      true,
    )
    expect(mocks.setTabEvents).toHaveBeenCalledWith(
      'restored-session',
      expect.arrayContaining([
        expect.objectContaining({ type: 'user_message' }),
        expect.objectContaining({
          type: 'llm_generation_end',
          data: expect.objectContaining({
            data: expect.objectContaining({ content: 'I will inspect that.', result: 'I will inspect that.' }),
          }),
        }),
        expect.objectContaining({
          type: 'unified_completion',
          data: expect.objectContaining({
            data: expect.objectContaining({ final_result: 'Hi there', result: 'Hi there' }),
          }),
        }),
      ]),
    )
    expect(mocks.setTabLastEventIndex).toHaveBeenCalledWith('restored-session', -1)
    expect(mocks.setTabHasMoreOlderEvents).toHaveBeenCalledWith('restored-session', false)
    expect(mocks.setTabHistoryPagination).toHaveBeenCalledWith('restored-session', null)
    expect(mocks.getRecentSessionEvents).toHaveBeenCalledWith('restored-session')
  })

  it('keeps the in-memory live tail when durable history is older', async () => {
    const liveTail = { id: 'latest-codex-answer', type: 'unified_completion' }
    mocks.getRecentSessionEvents.mockResolvedValue({
      events: [liveTail],
      session_status: 'completed',
      last_processed_index: 7,
      has_more: false,
    })
    mocks.getChatHistoryResumeConversation.mockResolvedValue({
      session_id: 'active-codex-session',
      conversation_history: [{ Role: 'human', Parts: [{ Text: 'Older prompt' }] }],
    })

    await hydrateTabEvents('active-codex-session', { workspacePath: '/workspace/workflow' })

    expect(mocks.setTabEvents).toHaveBeenCalledWith('active-codex-session', expect.any(Array))
    expect(mocks.addTabEvents).toHaveBeenCalledWith('active-codex-session', [liveTail])
    expect(mocks.setTabLastEventIndex).toHaveBeenLastCalledWith('active-codex-session', 7)
  })

  it('does not append a live completion that durable history already restored', async () => {
    const answer = 'The project has seven planned shots and no generated clips yet.'
    mocks.getRecentSessionEvents.mockResolvedValue({
      events: [{
        id: 'live-completion',
        type: 'unified_completion',
        data: { data: { final_result: answer, result: answer } },
      }],
      session_status: 'completed',
      last_processed_index: 8,
      has_more: false,
    })
    mocks.getChatHistoryResumeConversation.mockResolvedValue({
      session_id: 'duplicate-response-session',
      conversation_history: [
        { Role: 'human', Parts: [{ Text: 'What did you create?' }] },
        { Role: 'ai', Parts: [{ Text: answer }] },
      ],
    })

    await hydrateTabEvents('duplicate-response-session', { workspacePath: '/workspace/video' })

    expect(mocks.setTabEvents).toHaveBeenCalledWith('duplicate-response-session', expect.any(Array))
    expect(mocks.addTabEvents).not.toHaveBeenCalled()
    expect(mocks.setTabLastEventIndex).toHaveBeenLastCalledWith('duplicate-response-session', 8)
  })

  it('keeps every meaningful assistant update from one tool-heavy turn', () => {
    const events = conversationToRestoredEvents({
      session_id: 'tool-heavy-session',
      conversation_history: [
        { Role: 'human', Parts: [{ Text: 'Review the run' }] },
        { Role: 'ai', Parts: [{ Text: 'I will inspect the run.' }] },
        { Role: 'ai', Parts: [{ Type: 'function' }] },
        { Role: 'tool', Parts: [{ Text: 'tool result' }] },
        { Role: 'ai', Parts: [{ Text: 'The first finding is confirmed.' }] },
        { Role: 'ai', Parts: [{ Text: 'Done — final result.' }] },
      ],
    })

    const assistantUpdates = events.filter(event => event.type === 'llm_generation_end')
    expect(assistantUpdates.map(event => (event.data as { data?: { content?: string } }).data?.content)).toEqual([
      'I will inspect the run.',
      'The first finding is confirmed.',
      'Done — final result.',
    ])
    expect(events.filter(event => event.type === 'unified_completion')).toHaveLength(1)
  })

  it('keeps turns from before the saved trace above it instead of spreading them across it', () => {
    // Three old turns, then one traced turn. The trace (a restart cleared the
    // rest) holds only the last prompt and its tool call.
    const history = [
      { Role: 'human', Parts: [{ Text: 'first prompt' }], resume_order: 0 },
      { Role: 'ai', Parts: [{ Text: 'first reply' }], resume_order: 1 },
      { Role: 'human', Parts: [{ Text: 'second prompt' }], resume_order: 2 },
      { Role: 'ai', Parts: [{ Text: 'second reply' }], resume_order: 3 },
      { Role: 'human', Parts: [{ Text: 'traced prompt' }], resume_order: 4 },
      { Role: 'ai', Parts: [{ Text: 'traced reply' }], resume_order: 5 },
    ]
    const events = conversationToRestoredEvents({
      session_id: 's',
      conversation_history: history,
      history_source_message_count: 6,
      ui_events: [
        { id: 'u', type: 'user_message', timestamp: '2026-09-03T08:20:03Z', session_id: 's', data: { data: { content: 'traced prompt' } } },
        { id: 't', type: 'tool_call_start', timestamp: '2026-09-03T08:20:09Z', session_id: 's', data: { data: { tool_name: 'execute_shell_command' } } },
        { id: 'e', type: 'agent_end', timestamp: '2026-09-03T08:20:14Z', session_id: 's', data: { data: {} } },
      ],
    } as never)
    const at = (content: string) => Date.parse(events.find(event => {
      const data = (event.data as { data?: { content?: string; final_result?: string } }).data
      return data?.content === content || data?.final_result === content
    })!.timestamp || '')
    const traceStart = Date.parse('2026-09-03T08:20:03Z')
    expect(at('first prompt')).toBeLessThan(at('second reply'))
    expect(at('second reply')).toBeLessThan(traceStart)
    expect(at('traced prompt')).toBeGreaterThanOrEqual(traceStart)
    expect(at('traced reply')).toBeGreaterThan(Date.parse('2026-09-03T08:20:09Z'))
  })

  it('uses the saved formatted trace when a read-only schedule explicitly requests it', async () => {
    mocks.getRecentSessionEvents.mockResolvedValue({
      events: [],
      session_status: 'completed',
      last_processed_index: -1,
      has_more: false,
    })
    mocks.getChatHistoryResumeConversation.mockResolvedValue({
      session_id: 'schedule-session',
      conversation_history: [
        { Role: 'human', Parts: [{ Text: 'Start the scheduled run' }] },
        { Role: 'ai', Parts: [{ Text: 'The first stage completed.' }] },
        { Role: 'human', Parts: [{ Text: 'Continue with the next stage' }] },
        { Role: 'ai', Parts: [{ Text: 'The scheduled run is complete.' }] },
      ],
      ui_events: [
        {
          id: 'child-tool',
          type: 'tool_call_start',
          timestamp: '2026-08-21T01:00:00Z',
          session_id: 'schedule-session',
          terminal_id: 'schedule-session:child',
          terminal_owner_id: 'child',
          data: { data: { tool_name: 'query_workflow_db' } },
        },
        {
          id: 'child-answer',
          type: 'unified_completion',
          timestamp: '2026-08-21T01:00:01Z',
          session_id: 'schedule-session',
          data: { data: { final_result: 'Fixer result' } },
        },
      ],
    })

    await hydrateTabEvents('schedule-session', {
      workspacePath: '/workspace/workflow',
      fallbackToChatHistory: true,
      includeUiEvents: true,
    })

    expect(mocks.getChatHistoryResumeConversation).toHaveBeenCalledWith(
      'schedule-session',
      '/workspace/workflow',
      20,
      0,
      true,
    )
    expect(mocks.setTabEvents).toHaveBeenCalledWith(
      'schedule-session',
      expect.arrayContaining([
        expect.objectContaining({ type: 'conversation_resumed' }),
        expect.objectContaining({
          type: 'user_message',
          data: expect.objectContaining({
            data: expect.objectContaining({ content: 'Start the scheduled run' }),
          }),
        }),
        expect.objectContaining({
          type: 'unified_completion',
          data: expect.objectContaining({
            data: expect.objectContaining({ final_result: 'The scheduled run is complete.' }),
          }),
        }),
        expect.objectContaining({ id: 'child-tool', type: 'tool_call_start' }),
        expect.objectContaining({ id: 'child-answer', type: 'unified_completion' }),
      ]),
    )
  })
})

describe('lazy history hydration', () => {
  beforeEach(() => vi.clearAllMocks())
  it('shows recent history before live status finishes and keeps the older-page cursor', async () => {
    let resolveLive!: (value: unknown) => void
    mocks.getRecentSessionEvents.mockReturnValue(new Promise(resolve => { resolveLive = resolve }))
    mocks.getChatHistoryResumeConversation.mockResolvedValue({
      session_id: 'paged',
      conversation_history: [
        { Role: 'human', Parts: [{ Text: 'Recent question' }] },
        { Role: 'ai', Parts: [{ Text: 'Recent answer' }] },
      ],
      history_pagination: { has_more: true, next_offset: 20 },
    })
    const restoring = hydrateTabEvents('paged', { workspacePath: 'Workflow/test' })
    await vi.waitFor(() => expect(mocks.setTabEvents).toHaveBeenCalled())
    expect(mocks.getChatHistoryResumeConversation).toHaveBeenCalledWith('paged', 'Workflow/test', 20, 0, true)
    resolveLive({ events: [{ id: 'live', type: 'conversation_end' }], session_status: 'completed', has_more: false })
    await restoring
    expect(mocks.setTabHistoryPagination).toHaveBeenCalledWith('paged', { hasMore: true, nextOffset: 20 })
    expect(mocks.setTabHasMoreOlderEvents).toHaveBeenLastCalledWith('paged', true)
  })
})
