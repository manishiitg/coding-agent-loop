import { describe, expect, it } from 'vitest'
import type { ChatTab } from '../../stores/useChatStore'
import { applyWorkProjectRuntimeSelection, setWorkProjectRuntimeSelection, visibleWorkProjectTabs } from './workTabs'
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

describe('setWorkProjectRuntimeSelection', () => {
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
