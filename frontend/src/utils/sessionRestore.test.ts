import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  chatTabs: {} as Record<string, { sessionId: string; metadata?: { isViewOnly?: boolean } }>,
  addTabEvents: vi.fn(),
  setTabEvents: vi.fn(),
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
      chatTabs: mocks.chatTabs,
      addTabEvents: mocks.addTabEvents,
      setTabEvents: mocks.setTabEvents,
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

import { conversationToRestoredEvents, hydrateTabEvents } from './sessionRestore'

describe('hydrateTabEvents restored chat fallback', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    mocks.chatTabs = {}
    mocks.getTabEvents.mockReturnValue([])
  })

  it('reads a shared bot transcript without resuming its owner session', async () => {
    mocks.chatTabs = { observer: { sessionId: 'shared-bot', metadata: { isViewOnly: true } } }
    mocks.getChatHistoryResumeConversation.mockRejectedValue({ isAxiosError: true, response: { status: 403 } })
    mocks.getChatHistoryConversation.mockResolvedValue({ session_id: 'shared-bot', conversation_history: [
      { Role: 'human', Parts: [{ Text: 'Hello' }] },
      { Role: 'ai', Parts: [{ Text: 'Saved bot answer' }] },
    ] })
    mocks.getRecentSessionEvents.mockRejectedValue({ isAxiosError: true, response: { status: 404 } })
    await hydrateTabEvents('shared-bot', { workspacePath: 'Workflow/shared' })
    expect(mocks.getChatHistoryConversation).toHaveBeenCalledWith('shared-bot', 'Workflow/shared')
    expect(mocks.setTabEvents).toHaveBeenCalledWith('shared-bot', expect.arrayContaining([
      expect.objectContaining({ type: 'unified_completion' }),
    ]))
  })

  it('preserves a resume permission error for an interactive tab', async () => {
    const denied = { isAxiosError: true, response: { status: 403 } }
    mocks.getChatHistoryResumeConversation.mockRejectedValue(denied)
    mocks.getRecentSessionEvents.mockResolvedValue({ events: [] })
    await expect(hydrateTabEvents('private-session')).rejects.toBe(denied)
    expect(mocks.getChatHistoryConversation).not.toHaveBeenCalled()
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
    const liveTail = {
      id: 'latest-codex-answer',
      type: 'unified_completion',
      timestamp: '2026-09-15T12:41:23Z',
      data: { data: { final_result: 'Latest live answer', result: 'Latest live answer' } },
    }
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

    expect(mocks.setTabEvents).toHaveBeenCalledWith(
      'active-codex-session',
      expect.arrayContaining([expect.objectContaining({ id: 'latest-codex-answer' })]),
    )
    expect(mocks.addTabEvents).not.toHaveBeenCalled()
    expect(mocks.setTabLastEventIndex).toHaveBeenLastCalledWith('active-codex-session', 7)
  })

  it('orders an older live progress tail before the newer durable final answer', async () => {
    const currentPrompt = 'Create the dedicated Linear and GitHub reporting step.'
    const finalAnswer = 'I created finalize-linear-qa and connected it directly to end.'
    mocks.getRecentSessionEvents.mockResolvedValue({
      events: [
        {
          id: 'current-user',
          type: 'user_message',
          timestamp: '2026-09-16T08:00:00Z',
          data: { data: { content: currentPrompt } },
        },
        {
          id: 'progress-before-final',
          type: 'conversation_thinking',
          timestamp: '2026-09-16T08:07:00Z',
          data: { data: { content: 'Testing the implementation' } },
        },
      ],
      session_status: 'completed',
      last_processed_index: 84,
      has_more: false,
    })
    mocks.getChatHistoryResumeConversation.mockResolvedValue({
      session_id: 'post-deploy-ordering-regression',
      conversation_history: [
        { Role: 'human', Parts: [{ Text: 'Earlier question' }], resume_order: 0 },
        { Role: 'ai', Parts: [{ Text: 'Earlier answer' }], resume_order: 1 },
        { Role: 'human', Parts: [{ Text: currentPrompt }], resume_order: 2 },
        { Role: 'ai', Parts: [{ Text: finalAnswer }], resume_order: 3 },
      ],
      history_source_message_count: 4,
      ui_events: [
        {
          id: 'older-persisted-turn',
          type: 'user_message',
          timestamp: '2026-09-16T07:30:00Z',
          data: { data: { content: 'Earlier question' } },
        },
      ],
    })

    await hydrateTabEvents('post-deploy-ordering-regression', { workspacePath: '/workspace/workflow' })

    const [, events] = mocks.setTabEvents.mock.calls.at(-1) as [string, Array<{ id?: string; type: string; data?: unknown }>]
    const progressIndex = events.findIndex(event => event.id === 'progress-before-final')
    const finalIndex = events.findIndex(event => {
      if (event.type !== 'unified_completion') return false
      return JSON.stringify(event.data).includes(finalAnswer)
    })
    expect(progressIndex).toBeGreaterThan(-1)
    expect(finalIndex).toBeGreaterThan(progressIndex)
  })

  it('does not erase a user message and completion that arrive while history hydration is in flight', async () => {
    let resolveHistory!: (value: unknown) => void
    mocks.getRecentSessionEvents.mockResolvedValue({
      events: [],
      session_status: 'completed',
      last_processed_index: 455,
      has_more: false,
    })
    mocks.getChatHistoryResumeConversation.mockReturnValue(new Promise(resolve => { resolveHistory = resolve }))

    const optimisticUser = {
      id: 'new-user-message',
      type: 'user_message',
      timestamp: '2026-09-15T12:40:26Z',
      data: { data: { content: 'Run the complete test now.' } },
    }
    const liveCompletion = {
      id: 'new-live-completion',
      type: 'unified_completion',
      timestamp: '2026-09-15T12:41:23Z',
      data: { data: { final_result: 'The complete test passed.' } },
    }

    const hydration = hydrateTabEvents('active-race-session', { workspacePath: '/workspace/workflow' })
    mocks.getTabEvents.mockReturnValue([optimisticUser, liveCompletion])
    resolveHistory({
      session_id: 'active-race-session',
      conversation_history: [
        { Role: 'human', Parts: [{ Text: 'Older question' }] },
        { Role: 'ai', Parts: [{ Text: 'Older answer' }] },
      ],
    })
    await hydration

    expect(mocks.setTabEvents).toHaveBeenCalledWith(
      'active-race-session',
      expect.arrayContaining([
        expect.objectContaining({ id: 'new-user-message', type: 'user_message' }),
        expect.objectContaining({ id: 'new-live-completion', type: 'unified_completion' }),
      ]),
    )
  })

  it('keeps a live completion across a second stale-history hydration', async () => {
    const finalAnswer = 'The Cursor review finished successfully.'
    const liveCompletion = {
      id: 'cursor-live-completion',
      type: 'unified_completion',
      timestamp: '2026-09-16T18:18:58Z',
      data: { data: { final_result: finalAnswer } },
    }
    let storedEvents: Array<Record<string, unknown>> = []
    mocks.getTabEvents.mockImplementation(() => storedEvents)
    mocks.setTabEvents.mockImplementation((_sessionId, events) => {
      storedEvents = events
    })
    mocks.getChatHistoryResumeConversation.mockResolvedValue({
      session_id: 'cursor-stale-history',
      conversation_history: [
        { Role: 'human', Parts: [{ Text: 'Review the pull request.' }], resume_order: 0 },
      ],
      history_source_message_count: 1,
    })
    mocks.getRecentSessionEvents
      .mockResolvedValueOnce({
        events: [liveCompletion],
        session_status: 'completed',
        last_processed_index: 12,
        has_more: false,
      })
      .mockResolvedValueOnce({
        events: [],
        session_status: 'completed',
        last_processed_index: 12,
        has_more: false,
      })

    await hydrateTabEvents('cursor-stale-history', { workspacePath: '/workspace/workflow' })
    expect(storedEvents.some(event => event.id === liveCompletion.id)).toBe(true)

    await hydrateTabEvents('cursor-stale-history', { workspacePath: '/workspace/workflow' })
    expect(storedEvents).toEqual(expect.arrayContaining([
      expect.objectContaining({ id: liveCompletion.id, type: 'unified_completion' }),
    ]))
    expect(JSON.stringify(storedEvents)).toContain(finalAnswer)
  })

  it('does not replace a live retained turn with an eager stale-history paint', async () => {
    let resolveHistory!: (value: unknown) => void
    let resolveEvents!: (value: unknown) => void
    const storedEvents: Array<Record<string, unknown>> = [{
      id: 'optimistic-retained-user',
      type: 'user_message',
      timestamp: '2026-09-16T13:06:45Z',
      data: { data: { content: 'what do you do this delegation' } },
    }]
    mocks.getTabEvents.mockImplementation(() => storedEvents)
    mocks.setTabEvents.mockImplementation((_sessionId, events) => {
      storedEvents.splice(0, storedEvents.length, ...events)
    })
    mocks.getChatHistoryResumeConversation.mockReturnValue(new Promise(resolve => { resolveHistory = resolve }))
    mocks.getRecentSessionEvents.mockReturnValue(new Promise(resolve => { resolveEvents = resolve }))

    const hydration = hydrateTabEvents('retained-resume-race', { workspacePath: '/workspace/work' })
    resolveHistory({
      session_id: 'retained-resume-race',
      conversation_history: [
        { Role: 'human', Parts: [{ Text: 'older question' }] },
        { Role: 'ai', Parts: [{ Text: 'older answer' }] },
      ],
    })
    await Promise.resolve()
    await Promise.resolve()

    // History resolving first must not overwrite the optimistic message while
    // the live event window is still loading. A second projection treats the
    // first projection as restored trace and would otherwise discard this row.
    expect(storedEvents.some(event => event.id === 'optimistic-retained-user')).toBe(true)

    resolveEvents({
      events: [],
      session_status: 'completed',
      last_processed_index: 328,
      has_more: false,
    })
    await hydration

    expect(storedEvents).toEqual(expect.arrayContaining([
      expect.objectContaining({ id: 'optimistic-retained-user', type: 'user_message' }),
    ]))
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

  it('deduplicates a flat live user envelope against the durable user carrier', () => {
    const prompt = 'Create the launch teaser.'
    const events = conversationToRestoredEvents({
      session_id: 'flat-live-user-envelope',
      conversation_history: [
        { Role: 'user', Parts: [{ Text: prompt }] },
        { Role: 'assistant', Parts: [{ Text: 'The finished teaser is ready.' }] },
      ],
      ui_events: [{
        id: 'live-user-only',
        type: 'user_message',
        timestamp: '2026-08-17T00:00:00Z',
        data: { content: prompt } as never,
      }],
    })

    expect(events.filter(event => event.type === 'user_message')).toHaveLength(1)
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

  it('hides legacy double-wrapped tool context while keeping the real reply', () => {
    const events = conversationToRestoredEvents({
      session_id: 'double-wrapped-tool-context',
      conversation_history: [
        { Role: 'human', Parts: [{ Text: 'hi' }] },
        { Role: 'ai', Parts: [{ Text: '[Previous tool result]: [Previous tool result: read -> internal output]' }] },
        { Role: 'ai', Parts: [{ Text: 'Hello — what would you like to work on?' }] },
      ],
    })

    const visibleText = events.map(event => JSON.stringify(event.data)).join('\n')
    expect(visibleText).not.toContain('[Previous tool result]')
    expect(visibleText).toContain('Hello — what would you like to work on?')
  })

  it('preserves a real reply stored in the same message after a tool artifact', () => {
    const events = conversationToRestoredEvents({
      session_id: 'combined-tool-context-and-reply',
      conversation_history: [
        { Role: 'human', Parts: [{ Text: 'hi' }] },
        {
          Role: 'ai',
          Parts: [{
            Text: '[Previous tool result]: [Previous tool result: read -> NO_MEMORY_FILE]\n\nNo memory yet for this project. Hi — what would you like to work on?',
          }],
        },
      ],
    })

    const visibleText = events.map(event => JSON.stringify(event.data)).join('\n')
    expect(visibleText).not.toContain('[Previous tool result]')
    expect(visibleText).not.toContain('NO_MEMORY_FILE')
    expect(visibleText).toContain('No memory yet for this project')
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

  it('interleaves an older saved trace before newer durable chat turns', () => {
    const events = conversationToRestoredEvents({
      session_id: 'chronological-restore',
      conversation_history: [
        { Role: 'human', Parts: [{ Text: 'traced prompt' }], resume_order: 0 },
        { Role: 'ai', Parts: [{ Text: 'progress before tool' }], resume_order: 1 },
        { Role: 'human', Parts: [{ Text: 'latest question' }], resume_order: 2 },
        { Role: 'ai', Parts: [{ Text: 'latest answer' }], resume_order: 3 },
      ],
      history_source_message_count: 4,
      ui_events: [
        { id: 'u', type: 'user_message', timestamp: '2026-09-03T08:20:00Z', session_id: 'chronological-restore', data: { data: { content: 'traced prompt' } } },
        { id: 't', type: 'tool_call_start', timestamp: '2026-09-03T08:20:05Z', session_id: 'chronological-restore', data: { data: { tool_name: 'read' } } },
        { id: 'e', type: 'agent_end', timestamp: '2026-09-03T08:20:10Z', session_id: 'chronological-restore', data: { data: {} } },
      ],
    } as never)

    const label = (event: (typeof events)[number]) => {
      if (event.id === 't') return 'tool'
      const data = (event.data as { data?: { content?: string; final_result?: string } }).data
      return data?.content || data?.final_result || event.type
    }
    const ordered = events.map(label)
    expect(ordered[0]).toBe('conversation_resumed')
    expect(ordered.indexOf('tool')).toBeLessThan(ordered.indexOf('progress before tool'))
    expect(ordered.indexOf('progress before tool')).toBeLessThan(ordered.indexOf('latest question'))
    expect(ordered.indexOf('agent_end')).toBeLessThan(ordered.indexOf('latest question'))
    expect(ordered.indexOf('latest question')).toBeLessThan(ordered.indexOf('latest answer'))
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
  beforeEach(() => {
    vi.resetAllMocks()
    mocks.getTabEvents.mockReturnValue([])
  })
  it('reconciles recent history and live status once while keeping the older-page cursor', async () => {
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
    await Promise.resolve()
    await Promise.resolve()
    expect(mocks.setTabEvents).not.toHaveBeenCalled()
    expect(mocks.getChatHistoryResumeConversation).toHaveBeenCalledWith('paged', 'Workflow/test', 20, 0, true)
    resolveLive({ events: [{ id: 'live', type: 'conversation_end' }], session_status: 'completed', has_more: false })
    await restoring
    expect(mocks.setTabHistoryPagination).toHaveBeenCalledWith('paged', { hasMore: true, nextOffset: 20 })
    expect(mocks.setTabHasMoreOlderEvents).toHaveBeenLastCalledWith('paged', true)
  })
})
