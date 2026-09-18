import { beforeEach, describe, expect, it, vi } from 'vitest'

const state = vi.hoisted(() => ({ tabs: {} as Record<string, any>, generation: 1, verify: vi.fn(async () => [] as unknown[]) }))
vi.mock('../stores/useChatStore', () => ({ useChatStore: { subscribe: vi.fn(), getState: () => ({
  chatTabs: state.tabs,
  getTab: (id: string) => state.tabs[id],
  getTabConfig: (id: string) => state.tabs[id]?.config,
  setTabConfig: (id: string, patch: object) => { if (state.tabs[id]) Object.assign(state.tabs[id].config, patch) },
  getActiveSessions: () => state.verify(),
}) } }))
vi.mock('../stores/useAuthStore', () => ({ useAuthStore: { subscribe: vi.fn(), getState: () => ({}) } }))
vi.mock('../utils/chatIdentity', () => ({
  captureChatIdentity: () => state.generation,
  isChatIdentityCurrent: (value: number) => value === state.generation,
}))
import { drainChatQueue, resetChatQueueDeliveryReceiptsForTests, sendQueuedChatMessage } from '../utils/chatQueueController'

const build = (messages: string[]) => ({ message: messages.join('\n\n'), isAutoNotification: messages.every(m => m.startsWith('[AUTO-NOTIFICATION]')) })
function tab(sessionId: string, message: string) {
  return { sessionId, isStreaming: false, config: { queuedMessages: [message], isQueueProcessing: false } }
}

describe('queued notification and human message ownership', () => {
  beforeEach(() => {
    resetChatQueueDeliveryReceiptsForTests()
    state.verify.mockReset().mockResolvedValue([])
    state.generation = 1
    state.tabs = { A: tab('schedule-cron--job_1', '[AUTO-NOTIFICATION] Scheduled step completed'), B: tab('chat-B', 'message for B') }
  })
  it('keeps delivery in its originating conversation independent of selection', async () => {
    const send = vi.fn(async (_message: string, _options: unknown) => true)
    await drainChatQueue('A', send, build)
    expect(send).toHaveBeenCalledWith('[AUTO-NOTIFICATION] Scheduled step completed', expect.objectContaining({
      sourceTabId: 'A', sourceSessionId: 'schedule-cron--job_1', isAutoNotification: true,
    }))
    expect(state.tabs.B.config.queuedMessages).toEqual(['message for B'])
  })
  it('drains a background human queue without selecting its tab', async () => {
    const send = vi.fn(async (_message: string, _options: unknown) => true)
    await drainChatQueue('B', send, build)
    expect(send).toHaveBeenCalledWith('message for B', expect.objectContaining({sourceTabId: 'B', isAutoNotification: false}))
  })
  it.each(['replace', 'close', 'account'])('does not alter a new owner after %s while awaiting delivery', async change => {
    let release!: (value: boolean) => void
    const send = vi.fn((_message: string, _options: unknown) => new Promise<boolean>(resolve => { release = resolve }))
    const run = drainChatQueue('A', send, build)
    await Promise.resolve()
    if (change === 'replace') state.tabs.A = tab('replacement-session', 'new message')
    if (change === 'close') delete state.tabs.A
    if (change === 'account') { state.generation++; state.tabs.A = tab('schedule-cron--job_1', 'other account message') }
    const expected = structuredClone(state.tabs)
    release(false)
    await run
    expect(state.tabs).toEqual(expected)
  })
  it('retains rejected messages and pauses automatic retries', async () => {
    const send = vi.fn(async (_message: string, _options: unknown) => false)
    await drainChatQueue('A', send, build)
    expect(state.tabs.A.config.queuedMessages).toEqual(['[AUTO-NOTIFICATION] Scheduled step completed'])
    expect(state.tabs.A.config.isQueueProcessing).toBe(false)
    expect(state.tabs.A.config.queueError).toContain('not confirmed')
    await drainChatQueue('A', send, build)
    expect(send).toHaveBeenCalledTimes(1)
  })
  it('keeps the same submission ID for an explicit retry after reload', async () => {
    const send = vi.fn(async (_message: string, _options: unknown) => false)
    await drainChatQueue('A', send, build)
    const id = state.tabs.A.config.queuedSubmission.id
    state.tabs.A = JSON.parse(JSON.stringify(state.tabs.A))
    state.tabs.A.config.queueError = undefined
    await drainChatQueue('A', send, build)
    expect(send.mock.calls[1]?.[1]).toMatchObject({ submissionId: id })
  })
  it('retains messages until acceptance, then removes only the submitted prefix', async () => {
    let release!: (value: boolean) => void
    const send = vi.fn((_message: string, _options: unknown) => new Promise<boolean>(resolve => { release = resolve }))
    const run = drainChatQueue('B', send, build)
    await Promise.resolve()
    expect(state.tabs.B.config.queuedMessages).toEqual(['message for B'])
    state.tabs.B.config.queuedMessages.push('later message')
    release(true)
    await run
    expect(state.tabs.B.config.queuedMessages).toEqual(['later message'])
  })
  it('does not dispatch the same queue twice from multiple views', async () => {
    let release!: (value: boolean) => void
    const send = vi.fn((_message: string, _options: unknown) => new Promise<boolean>(resolve => { release = resolve }))
    const first = drainChatQueue('B', send, build)
    const second = drainChatQueue('B', send, build)
    await Promise.resolve()
    expect(send).toHaveBeenCalledTimes(1)
    release(true)
    await Promise.all([first, second])
  })
  it('deduplicates delayed delivery from another tab projecting the same session', async () => {
    state.tabs.C = tab('chat-B', 'message for B')
    const send = vi.fn(async (_message: string, _options: unknown) => true)

    await sendQueuedChatMessage('B', 0, 'message for B', send)
    await sendQueuedChatMessage('C', 0, 'message for B', send)

    expect(send).toHaveBeenCalledTimes(1)
    expect(state.tabs.B.config.queuedMessages).toEqual([])
    expect(state.tabs.C.config.queuedMessages).toEqual([])
  })
  it('serializes concurrent delivery across tabs projecting the same session', async () => {
    state.tabs.C = tab('chat-B', 'message for B')
    let release!: (value: boolean) => void
    const send = vi.fn((_message: string, _options: unknown) => new Promise<boolean>(resolve => { release = resolve }))

    const first = sendQueuedChatMessage('B', 0, 'message for B', send)
    const overlapping = await sendQueuedChatMessage('C', 0, 'message for B', send)
    expect(overlapping).toBe(false)
    expect(send).toHaveBeenCalledTimes(1)
    release(true)
    await first

    await sendQueuedChatMessage('C', 0, 'message for B', send)
    expect(send).toHaveBeenCalledTimes(1)
    expect(state.tabs.C.config.queuedMessages).toEqual([])
  })
  it('sends the selected duplicate occurrence and preserves its earlier twin', async () => {
    state.tabs.B.config.queuedMessages = ['same', 'middle', 'same', 'tail']
    const send = vi.fn(async (_message: string, _options: unknown) => true)
    await sendQueuedChatMessage('B', 2, 'same', send)
    expect(send).toHaveBeenCalledWith('same', expect.objectContaining({ sourceTabId: 'B', sourceSessionId: 'chat-B', preferLiveInput: true, queuedDelivery: true }))
    expect(state.tabs.B.config.queuedMessages).toEqual(['same', 'middle', 'tail'])
  })
  it('retains the selected receipt across failure and retries the same ID', async () => {
    state.tabs.B.config.queuedMessages = ['first', 'selected', 'third']
    const send = vi.fn(async (_message: string, _options: unknown) => false)
    await sendQueuedChatMessage('B', 1, 'selected', send)
    const id = state.tabs.B.config.queuedSubmission.id
    expect(state.tabs.B.config.queuedMessages).toEqual(['first', 'selected', 'third'])
    state.tabs.B.config.queueError = undefined
    send.mockResolvedValue(true)
    await drainChatQueue('B', send, build)
    expect(send.mock.calls[1]?.[1]).toMatchObject({ submissionId: id })
    expect(state.tabs.B.config.queuedMessages).toEqual(['first', 'third'])
  })
  it('does not resend an accepted snapshot after concurrent queue edits', async () => {
    let release!: (value: boolean) => void
    const send = vi.fn((_message: string, _options: unknown) => new Promise<boolean>(resolve => { release = resolve }))
    const run = sendQueuedChatMessage('B', 0, 'message for B', send)
    state.tabs.B.config.queuedMessages = ['replacement']
    release(true)
    await run
    expect(state.tabs.B.config.queuedMessages).toEqual(['replacement'])
    expect(state.tabs.B.config.queuedSubmission.accepted).toBe(true)
    state.tabs.B.config.queueError = undefined
    await drainChatQueue('B', send, build)
    expect(send).toHaveBeenCalledTimes(1)
    expect(state.tabs.B.config.queueError).toContain('will not be resent')
  })
  it('snapshots queued messages before waiting for server verification', async () => {
    let verify!: () => void
    state.verify.mockImplementationOnce(() => new Promise(resolve => { verify = () => resolve([]) }))
    const send = vi.fn(async (_message: string, _options: unknown) => true)
    const run = drainChatQueue('B', send, build)
    state.tabs.B.config.queuedMessages.push('later')
    verify()
    await run
    expect(send).toHaveBeenCalledWith('message for B', expect.anything())
    expect(state.tabs.B.config.queuedMessages).toEqual(['later'])
  })
  it('shares the background delivery lock with manual Send now', async () => {
    let release!: (value: boolean) => void
    const send = vi.fn((_message: string, _options: unknown) => new Promise<boolean>(resolve => { release = resolve }))
    const manual = sendQueuedChatMessage('B', 0, 'message for B', send)
    await drainChatQueue('B', send, build)
    expect(send).toHaveBeenCalledTimes(1)
    release(true)
    await manual
  })

  it.each(['human decision question', 'generated dashboard button'])('does not redeliver %s when idle delivery starts streaming', async message => {
    state.tabs.B.config.queuedMessages = [message]
    let release!: (value: boolean) => void
    const send = vi.fn((_message: string, _options: unknown) => new Promise<boolean>(resolve => { release = resolve }))
    const idle = drainChatQueue('B', send, build)
    await Promise.resolve()
    state.tabs.B.isStreaming = true
    // The live-input effect sees the entry before the first response settles.
    const live = await sendQueuedChatMessage('B', 0, message, send)
    expect(live).toBe(false)
    expect(state.tabs.B.config.queuedMessages).toEqual([message])
    expect(send).toHaveBeenCalledTimes(1)
    release(true)
    await idle
    expect(state.tabs.B.config.queuedMessages).toEqual([])
    expect(state.tabs.B.config.queueError).toBeUndefined()
  })

  it('keeps separate report actions and later arrivals in order during live delivery', async () => {
    state.tabs.B.isStreaming = true
    state.tabs.B.config.queuedMessages = ['decision question', 'dashboard action']
    let release!: (value: boolean) => void
    const send = vi.fn((_message: string, _options: unknown) => new Promise<boolean>(resolve => { release = resolve }))
    const first = sendQueuedChatMessage('B', 0, 'decision question', send)
    state.tabs.B.config.queuedMessages.push('later action')
    expect(state.tabs.B.config.queuedMessages).toHaveLength(3)
    release(true)
    await first
    expect(state.tabs.B.config.queuedMessages).toEqual(['dashboard action', 'later action'])
    expect(send.mock.calls[0]?.[0]).toBe('decision question')
  })

})
