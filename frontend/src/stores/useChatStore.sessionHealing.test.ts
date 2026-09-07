import { describe, expect, it } from 'vitest'
import type { ActiveSessionInfo } from '../services/api-types'
import { reconcileSessionTabStreamingState, type ChatState, type ChatTab } from './useChatStore'

// A turn a bot channel (WhatsApp, Slack, a scheduled run) starts on a session
// that already has an open UI tab looks, to that tab, identical to no turn at
// all: the tab's own isStreaming/hasRunningBgAgents never flip because the
// UI never submitted anything, and the tab's SSE stream can be silently dead
// (a queued EventSource sits "connecting" forever without ever firing
// onerror — see ChatArea.tsx's foreground-catch-up comment). Caught live: a
// WhatsApp-triggered SparkQuill reply saved to chat history but never
// appeared in the already-open parent tab, even after switching tabs away
// and back.
const baseTab = (overrides: Partial<ChatTab>): ChatTab => ({
  tabId: 'tab-1',
  name: 'Chat',
  sessionId: 'session-1',
  isStreaming: false,
  isCompleted: true,
  hasRunningBgAgents: false,
  isSyntheticTurn: false,
  canSteer: false,
  hideToolCalls: false,
  viewMode: 'formatted',
  config: {},
  createdAt: Date.now(),
  lastViewedEventCount: 0,
  lastViewedEventCounts: {},
  ...overrides,
} as unknown as ChatTab)

const activeSession = (overrides: Partial<ActiveSessionInfo>): ActiveSessionInfo => ({
  session_id: 'session-1',
  observer_id: 'session-1',
  agent_mode: 'multi-agent',
  status: 'running',
  last_activity: new Date().toISOString(),
  created_at: new Date().toISOString(),
  ...overrides,
})

describe('reconcileSessionTabStreamingState', () => {
  it('marks an idle tab streaming when the backend reports its session running', () => {
    const state = {
      chatTabs: { 'tab-1': baseTab({}) },
      tabSessionStatus: {},
    } as unknown as ChatState

    const result = reconcileSessionTabStreamingState(state, [activeSession({ status: 'running' })], Date.now())

    expect(result.chatTabs?.['tab-1'].isStreaming).toBe(true)
    expect(result.chatTabs?.['tab-1'].lastStreamingStartedAt).toBeGreaterThan(0)
  })

  it('marks an idle tab streaming when the backend reports running background agents, even if status is not "running"', () => {
    const state = {
      chatTabs: { 'tab-1': baseTab({}) },
      tabSessionStatus: {},
    } as unknown as ChatState

    const result = reconcileSessionTabStreamingState(
      state,
      [activeSession({ status: 'completed', has_running_background_agents: true })],
      Date.now(),
    )

    expect(result.chatTabs?.['tab-1'].isStreaming).toBe(true)
    expect(result.chatTabs?.['tab-1'].hasRunningBgAgents).toBe(true)
  })

  it('leaves an already-streaming tab alone (no redundant update)', () => {
    const state = {
      chatTabs: { 'tab-1': baseTab({ isStreaming: true, lastStreamingStartedAt: 123 }) },
      tabSessionStatus: {},
    } as unknown as ChatState

    const result = reconcileSessionTabStreamingState(state, [activeSession({ status: 'running' })], Date.now())

    expect(result.chatTabs).toBeUndefined()
  })

  it('does not heal a tab whose session the backend does not report at all', () => {
    const state = {
      chatTabs: { 'tab-1': baseTab({}) },
      tabSessionStatus: {},
    } as unknown as ChatState

    const result = reconcileSessionTabStreamingState(state, [], Date.now())

    expect(result.chatTabs).toBeUndefined()
  })

  it('still clears stale local streaming state once the backend session is no longer active (existing behavior)', () => {
    const state = {
      chatTabs: { 'tab-1': baseTab({ isStreaming: true, lastStreamingStartedAt: 0 }) },
      tabSessionStatus: { 'tab-1': { status: 'running', agentMode: 'multi-agent', lastActivity: null } },
    } as unknown as ChatState

    const result = reconcileSessionTabStreamingState(state, [], Date.now() + 10 * 60 * 1000)

    expect(result.chatTabs?.['tab-1'].isStreaming).toBe(false)
    expect(result.tabSessionStatus?.['tab-1'].status).toBeNull()
  })
})
