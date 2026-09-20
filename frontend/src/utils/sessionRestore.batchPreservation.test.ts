import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

// Follow-up P2: receipt application must not drop micro-batched events.
// appendTimelineAndApplyConfirmations used getTabEvents + setTabEvents to
// land verdicts, and setTabEvents clears the pending micro-batch — a tool
// event buffered in the same 200ms/1s window as a durability receipt was
// silently discarded. These tests drive the REAL store with fake timers:
// receipts upgrade committed rows while buffered events still flush.

const createMemoryStorage = (): Storage => {
  const values = new Map<string, string>()
  return {
    get length() {
      return values.size
    },
    clear: () => values.clear(),
    getItem: (key) => values.get(key) ?? null,
    key: (index) => Array.from(values.keys())[index] ?? null,
    removeItem: (key) => {
      values.delete(key)
    },
    setItem: (key, value) => {
      values.set(key, value)
    },
  }
}

const SESSION = 'p2-batch-session'
const STEER_ID = 'steer-message-1789877212206170000'

const liveUserRow = () => ({
  id: 'backend-steer-echo-1',
  type: 'user_message',
  timestamp: '2026-09-20T09:36:50.000+05:30',
  session_id: SESSION,
  data: { data: { content: 'ok dont run workflow', metadata: {
    source: 'coding_agent_live_input',
    delivery_status: 'sent_to_cli',
    provider: 'muse-cli',
    message_id: STEER_ID,
    confirmation: 'fast',
  } } },
})

// tool_call is not an "important" batch type, so addTabEvents buffers it
// for the next flush instead of landing it synchronously.
const toolEvent = () => ({
  id: 'tool-1',
  type: 'tool_call',
  timestamp: '2026-09-20T09:36:51.000+05:30',
  session_id: SESSION,
  data: { data: { content: 'running tests' } },
})

const confirmedWireEvent = () => ({
  id: `${STEER_ID}:confirmed`,
  type: 'live_input_confirmed',
  timestamp: '2026-09-20T09:36:52.208807+05:30',
  session_id: SESSION,
  data: {
    type: 'live_input_confirmed',
    timestamp: '2026-09-20T09:36:52.208807+05:30',
    event_index: 0,
    data: {
      timestamp: '2026-09-20T09:36:52.208801+05:30',
      hierarchy_level: 0,
      metadata: {
        confirmation: 'confirmed',
        latency_ms: 1225,
        message_id: STEER_ID,
        proof_source: '/tmp/session.jsonl',
        provider: 'muse-cli',
        source: 'coding_agent_live_input',
      },
      message_id: STEER_ID,
      outcome: 'confirmed',
      proof_source: '/tmp/session.jsonl',
      latency_ms: 1225,
      provider: 'muse-cli',
    },
  },
})

async function loadModules() {
  const [{ useChatStore }, { appendTimelineAndApplyConfirmations }, { readLiveInputConfirmation }] = await Promise.all([
    import('../stores/useChatStore'),
    import('./sessionRestore'),
    import('./liveInputReceipt'),
  ])
  return { useChatStore, appendTimelineAndApplyConfirmations, readLiveInputConfirmation }
}

describe('receipt application preserves micro-batched events', () => {
  beforeEach(() => {
    vi.resetModules()
    vi.stubGlobal('localStorage', createMemoryStorage())
  })

  afterEach(() => {
    vi.clearAllTimers()
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('keeps a buffered tool event across a same-window receipt', async () => {
    const { useChatStore, appendTimelineAndApplyConfirmations, readLiveInputConfirmation } = await loadModules()
    useChatStore.getState().setTabEvents(SESSION, [liveUserRow() as never])

    vi.useFakeTimers()
    // Buffer a tool event: the micro-batch flush is now pending.
    useChatStore.getState().addTabEvents(SESSION, [toolEvent() as never])
    expect(useChatStore.getState().getTabEvents(SESSION)).toHaveLength(1)

    const update = readLiveInputConfirmation(confirmedWireEvent() as never)
    expect(update).not.toBeNull()
    appendTimelineAndApplyConfirmations(SESSION, [], [update!])

    // Let the micro-batch flush (covers the 200ms active and 1s
    // background intervals).
    vi.advanceTimersByTime(1500)

    const events = useChatStore.getState().getTabEvents(SESSION)
    expect(events.map(event => event.id)).toContain('tool-1')
    const row = events.find(event => event.id === 'backend-steer-echo-1')
    expect((row?.data as { data?: { metadata?: { confirmation?: string } } })?.data?.metadata?.confirmation).toBe('confirmed')
  })

  it('upgrades the committed row when no batch is pending', async () => {
    const { useChatStore, appendTimelineAndApplyConfirmations, readLiveInputConfirmation } = await loadModules()
    useChatStore.getState().setTabEvents(SESSION, [liveUserRow() as never])

    const update = readLiveInputConfirmation(confirmedWireEvent() as never)
    expect(update).not.toBeNull()
    appendTimelineAndApplyConfirmations(SESSION, [], [update!])

    const events = useChatStore.getState().getTabEvents(SESSION)
    expect(events).toHaveLength(1)
    const row = events.find(event => event.id === 'backend-steer-echo-1')
    expect((row?.data as { data?: { metadata?: { confirmation?: string } } })?.data?.metadata?.confirmation).toBe('confirmed')
  })

  it('flushes buffered events on the batch interval without receipts', async () => {
    const { useChatStore } = await loadModules()
    useChatStore.getState().setTabEvents(SESSION, [liveUserRow() as never])

    vi.useFakeTimers()
    useChatStore.getState().addTabEvents(SESSION, [toolEvent() as never])
    expect(useChatStore.getState().getTabEvents(SESSION)).toHaveLength(1)
    vi.advanceTimersByTime(1500)

    const events = useChatStore.getState().getTabEvents(SESSION)
    expect(events.map(event => event.id).sort()).toEqual(['backend-steer-echo-1', 'tool-1'])
  })
})
