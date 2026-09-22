import { describe, expect, it } from 'vitest'
import type { ChatTab } from '../stores/useChatStore'
import { isExecutionConversationTab } from './sessionEventSource'

const tab = (metadata: ChatTab['metadata']) => ({ metadata } as ChatTab)

describe('session event source', () => {
  it('keeps interactive Crew and Builder chats on SQLite', () => {
    expect(isExecutionConversationTab(tab({ mode: 'multi-agent', agentProfileId: 'work' }))).toBe(false)
    expect(isExecutionConversationTab(tab({ mode: 'workflow' }))).toBe(false)
  })

  it('routes execution-run tabs to JSON diagnostics', () => {
    expect(isExecutionConversationTab(tab({ mode: 'workflow', isScheduledRun: true }))).toBe(true)
    expect(isExecutionConversationTab(tab({ mode: 'workflow', isViewOnly: true }))).toBe(false)
    expect(isExecutionConversationTab(tab({ mode: 'multi-agent', isExecutionRun: true }))).toBe(true)
    expect(isExecutionConversationTab(tab({ mode: 'multi-agent', isViewOnly: true, agentProfileConversationKey: 'project:history:run' }))).toBe(false)
    expect(isExecutionConversationTab({ sessionId: 'project:trigger:job:run', metadata: { mode: 'multi-agent', isViewOnly: true, agentProfileId: 'work' } } as ChatTab)).toBe(true)
  })
})
