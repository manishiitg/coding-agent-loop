import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

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

describe('useChatStore hydration bootstrap', () => {
  beforeEach(() => {
    vi.resetModules()
    vi.stubGlobal('localStorage', createMemoryStorage())
  })

  afterEach(() => {
    vi.clearAllTimers()
    vi.useRealTimers()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('finishes synchronous storage hydration before callers need the backstop', async () => {
    const chatStore = await import('./useChatStore')

    await chatStore.waitForChatStoreHydration()

    expect(chatStore.getChatStoreHydrationSnapshot()).toEqual({
      status: 'hydrated',
      error: null,
    })
  }, 15_000)

  it('does not persist streaming chunks or an obsolete tree preference', async () => {
    vi.useFakeTimers()
    const storage = createMemoryStorage()
    const setItem = vi.spyOn(storage, 'setItem')
    vi.stubGlobal('localStorage', storage)
    const chatStore = await import('./useChatStore')
    await chatStore.waitForChatStoreHydration()
    expect(chatStore.normalizeEventViewMode('tree')).toBe('formatted')
    chatStore.useChatStore.setState({ eventViewModePreference: 'formatted' })
    vi.advanceTimersByTime(250)
    setItem.mockClear()

    for (let index = 1; index <= 1_000; index += 1) {
      chatStore.useChatStore.getState().appendStreamingChunk('session-1', index, `chunk-${index}`)
    }
    vi.advanceTimersByTime(250)
    expect(setItem).not.toHaveBeenCalled()

    chatStore.useChatStore.setState({ eventViewModePreference: 'tree' })
    chatStore.useChatStore.setState({ eventViewModePreference: 'formatted' })
    chatStore.useChatStore.setState({ eventViewModePreference: 'tree' })
    vi.advanceTimersByTime(250)

    expect(setItem).not.toHaveBeenCalled()
  })

  it('coalesces repeated workflow switches into one durable write', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-07-13T00:00:00Z'))
    const storage = createMemoryStorage()
    const setItem = vi.spyOn(storage, 'setItem')
    vi.stubGlobal('localStorage', storage)
    const chatStore = await import('./useChatStore')
    await chatStore.waitForChatStoreHydration()

    const firstTab = await chatStore.useChatStore.getState().createChatTab('First workflow', {
      mode: 'workflow',
      phaseId: 'workflow-builder',
      presetQueryId: 'workflow-one',
    })
    vi.setSystemTime(new Date('2026-07-13T00:00:00.001Z'))
    const secondTab = await chatStore.useChatStore.getState().createChatTab('Second workflow', {
      mode: 'workflow',
      phaseId: 'workflow-builder',
      presetQueryId: 'workflow-two',
    })
    vi.advanceTimersByTime(250)
    setItem.mockClear()

    for (let index = 0; index < 100; index += 1) {
      chatStore.useChatStore.getState().switchTab(index % 2 === 0 ? firstTab : secondTab)
    }
    vi.advanceTimersByTime(250)

    expect(setItem).toHaveBeenCalledTimes(1)
    expect(chatStore.useChatStore.getState().activeTabId).toBe(secondTab)
  })

  it('rejects the removed profile-less AgentWorks chat lane', async () => {
    const chatStore = await import('./useChatStore')
    await chatStore.waitForChatStoreHydration()

    await expect(chatStore.useChatStore.getState().createChatTab('Chat', {
      mode: 'multi-agent',
    })).rejects.toThrow('Profile-less AgentWorks Chat has been removed')
    expect(Object.keys(chatStore.useChatStore.getState().chatTabs)).toHaveLength(0)
  })

  it('keeps product-profile project sessions in separate lanes', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-08-07T10:00:00Z'))
    const chatStore = await import('./useChatStore')
    await chatStore.waitForChatStoreHydration()

    const firstProjectTabId = await chatStore.useChatStore.getState().createChatTab('Launch film', {
      mode: 'multi-agent',
      agentProfileId: 'video-studio',
      agentProfileVersion: 1,
      agentProfileWorkspace: 'Chats/Video Studio/projects/launch-film',
    }, 'video-studio:project:launch-film')
    const reusedProjectTabId = await chatStore.useChatStore.getState().createChatTab('Launch film', {
      mode: 'multi-agent',
      agentProfileId: 'video-studio',
      agentProfileVersion: 1,
      agentProfileWorkspace: 'Chats/Video Studio/projects/launch-film',
    })
    const secondProjectTabId = await chatStore.useChatStore.getState().createChatTab('Customer story', {
      mode: 'multi-agent',
      agentProfileId: 'video-studio',
      agentProfileVersion: 1,
      agentProfileWorkspace: 'Chats/Video Studio/projects/customer-story',
    }, 'video-studio:project:customer-story')

    expect(reusedProjectTabId).toBe(firstProjectTabId)
    expect(secondProjectTabId).not.toBe(firstProjectTabId)
    expect(Object.keys(chatStore.useChatStore.getState().chatTabs)).toHaveLength(2)
  })

  it('never reuses a product Builder as the project conversation tab', async () => {
    const chatStore = await import('./useChatStore')
    await chatStore.waitForChatStoreHydration()

    const builderTabId = await chatStore.useChatStore.getState().createChatTab('Builder', {
      mode: 'multi-agent',
      agentProfileId: 'work',
      agentProfileVersion: 1,
      agentProfileWorkspace: 'Chats/Work/projects/demo',
      agentProfileProjectId: 'demo',
      agentProfileBuilder: true,
    })
    const chatTabId = await chatStore.useChatStore.getState().createChatTab('Fix the login form', {
      mode: 'multi-agent',
      agentProfileId: 'work',
      agentProfileVersion: 1,
      agentProfileWorkspace: 'Chats/Work/projects/demo',
      agentProfileProjectId: 'demo',
      agentProfileConversationKey: 'demo:chat-1',
      agentProfileBuilder: false,
    })

    expect(chatTabId).not.toBe(builderTabId)
    expect(chatStore.useChatStore.getState().getTab(builderTabId)?.metadata?.agentProfileBuilder).toBe(true)
    expect(chatStore.useChatStore.getState().getTab(chatTabId)?.metadata?.agentProfileBuilder).toBe(false)
    expect(Object.keys(chatStore.useChatStore.getState().chatTabs)).toHaveLength(2)
  })

  it('does not inherit globally connected MCP or skill selections into a product tab', async () => {
    const chatStore = await import('./useChatStore')
    const { useMCPStore } = await import('./useMCPStore')
    const { useAppStore } = await import('./useAppStore')
    await chatStore.waitForChatStoreHydration()
    useMCPStore.setState({ chatSelectedServers: ['google_sheets'] })
    useAppStore.setState({ lastSelectedSkills: ['personal-skill'] })

    const tabId = await chatStore.useChatStore.getState().createChatTab('Builder', {
      mode: 'multi-agent',
      agentProfileId: 'work',
      agentProfileProjectId: 'isolated-project',
      agentProfileBuilder: true,
    })

    expect(chatStore.useChatStore.getState().getTab(tabId)?.config).toMatchObject({
      selectedServers: ['NO_SERVERS'],
      selectedSkills: [],
    })
  })

  it('reuses a keyed product conversation across profile upgrades and workspace moves', async () => {
    const chatStore = await import('./useChatStore')
    await chatStore.waitForChatStoreHydration()

    const originalTabId = await chatStore.useChatStore.getState().createChatTab('Launch film', {
      mode: 'multi-agent',
      agentProfileId: 'video-studio',
      agentProfileVersion: 1,
      agentProfileWorkspace: 'Chats/Video Studio/projects/old-location',
      agentProfileProjectId: 'launch-2026',
      agentProfileConversationKey: 'launch-2026',
      agentProfileConversationId: 'conversation-launch',
      agentProfileChatContract: 'profile-v1',
    }, 'canonical-session')
    const resumedTabId = await chatStore.useChatStore.getState().createChatTab('Launch film', {
      mode: 'multi-agent',
      agentProfileId: 'video-studio',
      agentProfileVersion: 2,
      agentProfileWorkspace: 'Chats/Video Studio/projects/new-location',
      agentProfileProjectId: 'launch-2026',
      agentProfileConversationKey: 'launch-2026',
      agentProfileConversationId: 'conversation-launch',
      agentProfileChatContract: 'profile-v1',
    }, 'canonical-session')

    expect(resumedTabId).toBe(originalTabId)
    expect(chatStore.useChatStore.getState().getTab(originalTabId)?.metadata).toMatchObject({
      agentProfileVersion: 2,
      agentProfileWorkspace: 'Chats/Video Studio/projects/new-location',
      agentProfileConversationKey: 'launch-2026',
    })
    expect(Object.keys(chatStore.useChatStore.getState().chatTabs)).toHaveLength(1)
  })

  it('restores the existing persisted chat-store envelope', async () => {
    const storage = createMemoryStorage()
    const createdAt = Date.now()
    storage.setItem('chat-store', JSON.stringify({
      state: {
        chatTabs: {
          'legacy-tab': {
            tabId: 'legacy-tab',
            name: 'Existing workflow chat',
            sessionId: 'legacy-session',
            isStreaming: false,
            isCompleted: false,
            hasRunningBgAgents: false,
            isSyntheticTurn: false,
            canSteer: false,
            hideToolCalls: true,
            viewMode: 'terminal',
            config: {
              inputText: '',
              useCodeExecutionMode: true,
              selectedServers: [],
              selectedSkills: [],
              selectedSecrets: [],
              llmConfig: { provider: 'codex-cli', model_id: 'gpt-5.6-sol' },
              fileContext: [],
              browserMode: 'none',
              workflowContext: [],
              queuedMessages: [],
            },
            createdAt,
            lastAccessedAt: createdAt,
            lastViewedEventCount: 0,
            lastViewedEventCounts: { micro: 0 },
            metadata: { mode: 'workflow', presetQueryId: 'existing-workflow' },
          },
        },
        activeTabId: 'legacy-tab',
        eventViewModePreference: 'terminal',
      },
      version: 0,
    }))
    vi.stubGlobal('localStorage', storage)

    const chatStore = await import('./useChatStore')
    await chatStore.waitForChatStoreHydration()

    expect(chatStore.useChatStore.getState().activeTabId).toBe('legacy-tab')
    expect(chatStore.useChatStore.getState().chatTabs['legacy-tab']).toMatchObject({
      name: 'Existing workflow chat',
      sessionId: 'legacy-session',
      metadata: { mode: 'workflow', presetQueryId: 'existing-workflow' },
    })
  })
})
