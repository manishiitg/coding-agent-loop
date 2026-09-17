import { describe, expect, it } from 'vitest'
import type { ChatTab } from '../../stores/useChatStore'
import { applyWorkProjectRuntimeSelection, markWorkProjectRuntimeDirty, preferredWorkProjectTabId, setWorkProjectRuntimeSelection, visibleWorkProjectTabs, workConversationResumeKey, workTabDisplayName } from './workTabs'
import { useChatStore } from '../../stores/useChatStore'

function tab(overrides: Partial<ChatTab> & Pick<ChatTab, 'tabId'>): ChatTab {
  return {
    name: 'Chat',
    sessionId: overrides.tabId,
    isStreaming: false,
    isCompleted: false,
    hasRunningBgAgents: false,
    isSyntheticTurn: false,
    canSteer: false,
    hideToolCalls: true,
    viewMode: 'formatted',
    config: {} as ChatTab['config'],
    createdAt: 1,
    lastAccessedAt: 1,
    lastViewedEventCount: 0,
    lastViewedEventCounts: { micro: 0 },
    metadata: {
      mode: 'multi-agent',
      agentProfileId: 'work',
      agentProfileProjectId: 'project-1',
    },
    ...overrides,
  }
}

describe('visibleWorkProjectTabs', () => {
  it('pins one Builder first and shows one tab per conversation', () => {
    const builder = tab({ tabId: 'builder', name: 'Builder', metadata: { mode: 'multi-agent', agentProfileId: 'work', agentProfileProjectId: 'project-1', agentProfileBuilder: true } })
    const oldCopy = tab({ tabId: 'old', createdAt: 2, lastAccessedAt: 2, metadata: { mode: 'multi-agent', agentProfileId: 'work', agentProfileProjectId: 'project-1', agentProfileConversationKey: 'project-1:chat-1' } })
    const activeCopy = tab({ tabId: 'active', createdAt: 3, lastAccessedAt: 3, metadata: { mode: 'multi-agent', agentProfileId: 'work', agentProfileProjectId: 'project-1', agentProfileConversationKey: 'project-1:chat-1' } })

    expect(visibleWorkProjectTabs({ builder, old: oldCopy, active: activeCopy }, 'project-1', 'active').map(item => item.tabId)).toEqual(['builder', 'active'])
  })

  it('does not mix tabs from another Work project', () => {
    const current = tab({ tabId: 'current' })
    const other = tab({ tabId: 'other', metadata: { mode: 'multi-agent', agentProfileId: 'work', agentProfileProjectId: 'project-2' } })
    expect(visibleWorkProjectTabs({ current, other }, 'project-1', null).map(item => item.tabId)).toEqual(['current'])
  })
})

describe('workTabDisplayName', () => {
  it('limits a tab label to three words and twenty characters', () => {
    expect(workTabDisplayName('Browser resume test C — reply only with ACK-C.')).toBe('Browser resume test…')
    expect(workTabDisplayName('One unusuallylongword title')).toBe('One unusuallylongwo…')
  })

  it('keeps short labels unchanged', () => {
    expect(workTabDisplayName('Fix login')).toBe('Fix login')
  })
})

describe('workConversationResumeKey', () => {
  it('upgrades a retained legacy project-key tab to its own durable slot', () => {
    const legacy = tab({
      tabId: 'legacy',
      sessionId: 'saved-session',
      metadata: { mode: 'multi-agent', agentProfileId: 'work', agentProfileProjectId: 'project-1', agentProfileConversationKey: 'project-1' },
    })
    expect(workConversationResumeKey(legacy, 'project-1')).toBe('project-1:saved-session')
  })

  it('preserves a tab-specific logical key across deployments', () => {
    const retained = tab({
      tabId: 'retained',
      sessionId: 'replacement-session',
      metadata: { mode: 'multi-agent', agentProfileId: 'work', agentProfileProjectId: 'project-1', agentProfileConversationKey: 'project-1:chat-identity' },
    })
    expect(workConversationResumeKey(retained, 'project-1')).toBe('project-1:chat-identity')
  })

  it('does not manufacture a binding for an unsent tab', () => {
    expect(workConversationResumeKey(tab({ tabId: 'empty', sessionId: null }), 'project-1')).toBeNull()
  })
})

describe('preferredWorkProjectTabId', () => {
  it('preserves the active project tab', () => {
    const builder = tab({ tabId: 'builder', metadata: { mode: 'multi-agent', agentProfileId: 'work', agentProfileProjectId: 'project-1', agentProfileBuilder: true } })
    const older = tab({ tabId: 'older', lastAccessedAt: 10 })
    const newer = tab({ tabId: 'newer', lastAccessedAt: 20 })
    expect(preferredWorkProjectTabId({ builder, older, newer }, 'project-1', 'older')).toBe('older')
  })

  it('opens the most recently accessed chat when the saved active tab is unrelated', () => {
    const builder = tab({ tabId: 'builder', metadata: { mode: 'multi-agent', agentProfileId: 'work', agentProfileProjectId: 'project-1', agentProfileBuilder: true }, lastAccessedAt: 30 })
    const older = tab({ tabId: 'older', lastAccessedAt: 10 })
    const newer = tab({ tabId: 'newer', lastAccessedAt: 20 })
    const unrelated = tab({ tabId: 'unrelated', metadata: { mode: 'multi-agent', agentProfileId: 'work', agentProfileProjectId: 'other-project' }, lastAccessedAt: 40 })
    expect(preferredWorkProjectTabId({ builder, older, newer, unrelated }, 'project-1', 'unrelated')).toBe('newer')
  })

  it('falls back to Builder when the project has no durable chat', () => {
    const builder = tab({ tabId: 'builder', metadata: { mode: 'multi-agent', agentProfileId: 'work', agentProfileProjectId: 'project-1', agentProfileBuilder: true } })
    expect(preferredWorkProjectTabId({ builder }, 'project-1', null)).toBe('builder')
  })
})

describe('setWorkProjectRuntimeSelection', () => {

  it('marks retained project chats dirty after durable context changes', () => {
    const current = tab({ tabId: 'current' })
    const other = tab({ tabId: 'other', metadata: { mode: 'multi-agent', agentProfileId: 'work', agentProfileProjectId: 'project-2' } })
    useChatStore.setState({ chatTabs: { current, other } })

    markWorkProjectRuntimeDirty('project-1')

    expect(useChatStore.getState().chatTabs.current.metadata?.agentProfileRuntimeDirty).toBe(true)
    expect(useChatStore.getState().chatTabs.other.metadata?.agentProfileRuntimeDirty).toBeUndefined()
  })
  it('keeps every chat in a Work project on the same saved runtime', () => {
    const builder = tab({ tabId: 'builder', name: 'Builder', metadata: { mode: 'multi-agent', agentProfileId: 'work', agentProfileProjectId: 'project-1', agentProfileBuilder: true } })
    const chat = tab({ tabId: 'chat', metadata: { mode: 'multi-agent', agentProfileId: 'work', agentProfileProjectId: 'project-1', agentProfileConversationKey: 'project-1:chat-1' } })
    const secondChat = tab({ tabId: 'second', metadata: { mode: 'multi-agent', agentProfileId: 'work', agentProfileProjectId: 'project-1', agentProfileConversationKey: 'project-1:chat-2' } })
    const other = tab({ tabId: 'other', metadata: { mode: 'multi-agent', agentProfileId: 'work', agentProfileProjectId: 'project-2', agentProfileBuilder: true } })
    useChatStore.setState({ chatTabs: { builder, chat, second: secondChat, other } })

    setWorkProjectRuntimeSelection('project-1', 'chat', {
      engine: 'muse-cli',
      modelId: 'muse-spark-1.3-contributor',
      reasoningEffort: 'high',
    })

    const tabs = useChatStore.getState().chatTabs
    expect(tabs.chat.metadata).toMatchObject({ agentProfileEngine: 'muse-cli', agentProfileModelID: 'muse-spark-1.3-contributor' })
    expect(tabs.builder.metadata).toMatchObject({ agentProfileEngine: 'muse-cli', agentProfileModelID: 'muse-spark-1.3-contributor' })
    expect(tabs.second.metadata).toMatchObject({ agentProfileEngine: 'muse-cli', agentProfileModelID: 'muse-spark-1.3-contributor', agentProfileRuntimeDirty: true })
    expect(tabs.other.metadata?.agentProfileEngine).toBeUndefined()
  })

  it('changes only the Builder when the coding-agent provider is for new chats', () => {
    const builder = tab({ tabId: 'builder', name: 'Builder', metadata: { mode: 'multi-agent', agentProfileId: 'work', agentProfileProjectId: 'project-1', agentProfileBuilder: true, agentProfileEngine: 'muse-cli', agentProfileModelID: 'muse-spark-1.3-contributor' } })
    const chat = tab({ tabId: 'chat', metadata: { mode: 'multi-agent', agentProfileId: 'work', agentProfileProjectId: 'project-1', agentProfileConversationKey: 'project-1:chat-1', agentProfileEngine: 'muse-cli', agentProfileModelID: 'muse-spark-1.3-contributor' } })
    useChatStore.setState({ chatTabs: { builder, chat } })

    setWorkProjectRuntimeSelection('project-1', 'chat', {
      engine: 'claude-code',
      provider: 'claude-code',
      modelId: 'claude-sonnet-5',
    }, { newChatsOnly: true })

    const tabs = useChatStore.getState().chatTabs
    expect(tabs.builder.metadata).toMatchObject({ agentProfileEngine: 'claude-code', agentProfileModelID: 'claude-sonnet-5', agentProfileRuntimeDirty: false })
    expect(tabs.chat.metadata).toMatchObject({ agentProfileEngine: 'muse-cli', agentProfileModelID: 'muse-spark-1.3-contributor' })
  })

  it('uses the composer tab carried by the event instead of the global active tab', () => {
    const builder = tab({ tabId: 'builder', name: 'Builder', metadata: { mode: 'multi-agent', agentProfileId: 'work', agentProfileProjectId: 'project-1', agentProfileBuilder: true } })
    const chat = tab({ tabId: 'chat', metadata: { mode: 'multi-agent', agentProfileId: 'work', agentProfileProjectId: 'project-1', agentProfileConversationKey: 'project-1:chat-1' } })
    const globallyActive = tab({ tabId: 'global', metadata: { mode: 'multi-agent', agentProfileId: 'work', agentProfileProjectId: 'project-2' } })
    useChatStore.setState({ activeTabId: 'global', chatTabs: { builder, chat, global: globallyActive } })

    expect(applyWorkProjectRuntimeSelection('project-1', 'builder', {
      profileId: 'work',
      tabId: 'chat',
      engine: 'muse-cli',
      modelId: 'muse-spark-1.3-contributor',
    })).toBe(true)

    const tabs = useChatStore.getState().chatTabs
    expect(tabs.chat.metadata?.agentProfileEngine).toBe('muse-cli')
    expect(tabs.builder.metadata?.agentProfileEngine).toBe('muse-cli')
    expect(tabs.global.metadata?.agentProfileEngine).toBeUndefined()
  })
})
