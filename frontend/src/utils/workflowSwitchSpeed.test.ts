// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

// Persisted stores bind their storage when first imported.
vi.hoisted(() => {
  const data = new Map<string, string>()
  Object.defineProperty(globalThis, 'localStorage', {
    configurable: true,
    value: {
      getItem: (key: string) => data.get(key) ?? null,
      setItem: (key: string, value: string) => { data.set(key, value) },
      removeItem: (key: string) => { data.delete(key) },
      clear: () => data.clear(),
      key: () => null,
      length: 0,
    },
  })
})
const listRunningWorkflows = vi.hoisted(() => vi.fn())
vi.mock('../services/api', async importOriginal => {
  const actual = await importOriginal<typeof import('../services/api')>()
  return { ...actual, agentApi: { ...actual.agentApi, listRunningWorkflows } }
})

import type { ActiveSessionInfo, PollingEvent } from '../services/api-types'
import { useChatStore, type ChatTab } from '../stores/useChatStore'
import type { CustomPreset } from '../types/preset'
import { openWorkflowPresetPage } from './workflowSessionRestore'
import { resetWorkflowNavigationForTests } from './workflowNavigation'

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(r => { resolve = r })
  return { promise, resolve }
}

const preset = {
  id: 'workflow-a',
  label: 'Workflow A',
  selectedFolder: { filepath: 'Workflow/a' },
} as unknown as CustomPreset

function builderTab(overrides: Partial<ChatTab> = {}): ChatTab {
  return {
    tabId: 'chat-a',
    name: 'Automation Builder',
    sessionId: 'session-a',
    isStreaming: false,
    isCompleted: true,
    hasRunningBgAgents: false,
    isSyntheticTurn: false,
    canSteer: false,
    hideToolCalls: false,
    viewMode: 'formatted',
    config: {} as ChatTab['config'],
    createdAt: 1,
    lastAccessedAt: 1,
    lastViewedEventCount: 0,
    lastViewedEventCounts: { micro: 0 },
    metadata: { mode: 'workflow', phaseId: 'workflow-builder', presetQueryId: 'workflow-a' },
    ...overrides,
  }
}

const otherTab = builderTab({
  tabId: 'chat-b',
  sessionId: 'session-b',
  metadata: { mode: 'workflow', phaseId: 'workflow-builder', presetQueryId: 'workflow-b' },
})

let activeSessions: ReturnType<typeof deferred<ActiveSessionInfo[]>>
let running: ReturnType<typeof deferred<{ running: [] }>>

beforeEach(() => {
  resetWorkflowNavigationForTests()
  activeSessions = deferred<ActiveSessionInfo[]>()
  running = deferred<{ running: [] }>()
  listRunningWorkflows.mockReturnValue(running.promise)
  useChatStore.setState({
    getActiveSessions: vi.fn(() => activeSessions.promise),
  })
})

afterEach(() => {
  useChatStore.setState({ chatTabs: {}, activeTabId: null, tabEvents: {} })
  listRunningWorkflows.mockReset()
})

describe('switching to a workflow', () => {
  it('shows a workflow seen earlier from memory before any server read returns', async () => {
    const cached = builderTab()
    useChatStore.setState({
      chatTabs: { [cached.tabId]: cached, [otherTab.tabId]: otherTab },
      activeTabId: otherTab.tabId,
      tabEvents: { 'session-a': [{ id: 'e1' } as PollingEvent] },
    })

    const opening = openWorkflowPresetPage(preset)

    // Synchronously on the click: no round trip stands between the user and
    // the last conversation they saw in this workflow.
    expect(useChatStore.getState().activeTabId).toBe(cached.tabId)

    activeSessions.resolve([])
    running.resolve({ running: [] })
    await opening
    expect(useChatStore.getState().activeTabId).toBe(cached.tabId)
  })

  it('reads the running registry alongside active sessions, not after them', async () => {
    const cached = builderTab()
    useChatStore.setState({
      chatTabs: { [cached.tabId]: cached },
      activeTabId: null,
      tabEvents: { 'session-a': [{ id: 'e1' } as PollingEvent] },
    })

    const opening = openWorkflowPresetPage(preset)
    await Promise.resolve()

    // The active-session read is still pending, yet the registry read has
    // already been issued.
    expect(listRunningWorkflows).toHaveBeenCalledTimes(1)

    activeSessions.resolve([])
    running.resolve({ running: [] })
    await opening
  })

  it('waits for resolution when the workflow has nothing in memory', async () => {
    const empty = builderTab()
    useChatStore.setState({
      chatTabs: { [empty.tabId]: empty, [otherTab.tabId]: otherTab },
      activeTabId: otherTab.tabId,
      tabEvents: {},
    })

    const opening = openWorkflowPresetPage(preset)
    expect(useChatStore.getState().activeTabId).toBe(otherTab.tabId)

    activeSessions.resolve([])
    running.resolve({ running: [] })
    await opening
    expect(useChatStore.getState().activeTabId).toBe(empty.tabId)
  })
})
