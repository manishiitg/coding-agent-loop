import { describe, expect, it } from 'vitest'
import type { ChatTab } from '../stores/useChatStore'
import {
  activeWorkflowTabHasCachedConversation,
  activeWorkflowTabIdForPreset,
  cachedWorkflowTabIdForPreset,
  workflowTabBelongsToPreset,
} from './workflowTabOwnership'

function tab(overrides: Partial<ChatTab> = {}): ChatTab {
  return {
    tabId: 'tab-a',
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
    lastViewedEventCount: 0,
    lastViewedEventCounts: { micro: 0 },
    metadata: {
      mode: 'workflow',
      phaseId: 'workflow-builder',
      presetQueryId: 'workflow-a',
    },
    ...overrides,
  }
}

describe('workflow tab ownership', () => {
  it('rejects an active tab owned by another workflow', () => {
    const workflowB = tab({
      tabId: 'tab-b',
      sessionId: 'session-b',
      metadata: { mode: 'workflow', phaseId: 'workflow-builder', presetQueryId: 'workflow-b' },
    })
    const tabs = { [workflowB.tabId]: workflowB }

    expect(workflowTabBelongsToPreset(workflowB, 'workflow-a', tabs)).toBe(false)
    expect(activeWorkflowTabIdForPreset(workflowB.tabId, 'workflow-a', tabs)).toBeUndefined()
  })

  it('accepts the tab explicitly owned by the selected workflow', () => {
    const workflowA = tab()
    const tabs = { [workflowA.tabId]: workflowA }

    expect(activeWorkflowTabIdForPreset(workflowA.tabId, 'workflow-a', tabs)).toBe(workflowA.tabId)
  })

  it('allows a legacy unowned builder only when no explicit destination tab exists', () => {
    const legacy = tab({
      tabId: 'legacy',
      sessionId: null,
      metadata: { mode: 'workflow', phaseId: 'workflow-builder' },
    })
    const explicit = tab()

    expect(workflowTabBelongsToPreset(legacy, 'workflow-a', { legacy })).toBe(true)
    expect(workflowTabBelongsToPreset(legacy, 'workflow-a', { legacy, [explicit.tabId]: explicit })).toBe(false)
  })

  it('keeps an explicitly selected Schedule tab owned without a time limit', () => {
    const schedule = tab({
      tabId: 'schedule',
      sessionId: 'schedule-run',
      metadata: {
        mode: 'workflow',
        presetQueryId: 'workflow-a',
        isViewOnly: true,
        isScheduledRun: true,
        // Old timestamps must not give a background reconcile permission to
        // replace the user's active tab.
        readOnlyRestoredAt: 1,
      },
    })

    expect(activeWorkflowTabIdForPreset(schedule.tabId, 'workflow-a', { schedule })).toBe(schedule.tabId)
  })
})

describe('cached workflow conversation', () => {
  const event = { id: 'e1' }

  it('treats an active tab of the selected workflow with events as showable at once', () => {
    const workflowA = tab()
    const tabs = { [workflowA.tabId]: workflowA }

    expect(activeWorkflowTabHasCachedConversation(workflowA.tabId, 'workflow-a', tabs, { 'session-a': [event] })).toBe(true)
    // Nothing in memory yet: the pane must wait for the reconnect.
    expect(activeWorkflowTabHasCachedConversation(workflowA.tabId, 'workflow-a', tabs, {})).toBe(false)
    // Another workflow's transcript is never shown for the selected one.
    expect(activeWorkflowTabHasCachedConversation(workflowA.tabId, 'workflow-b', tabs, { 'session-a': [event] })).toBe(false)
  })

  it('picks the most recently opened persistent Chat that still has its transcript', () => {
    const older = tab({ tabId: 'older', sessionId: 'older-session', lastAccessedAt: 10 })
    const newer = tab({ tabId: 'newer', sessionId: 'newer-session', lastAccessedAt: 20 })
    const emptyNewest = tab({ tabId: 'empty', sessionId: 'empty-session', lastAccessedAt: 30 })
    const scheduleRun = tab({
      tabId: 'schedule',
      sessionId: 'schedule-session',
      lastAccessedAt: 40,
      metadata: { mode: 'workflow', presetQueryId: 'workflow-a', isViewOnly: true, isScheduledRun: true },
    })
    const otherWorkflow = tab({
      tabId: 'other',
      sessionId: 'other-session',
      lastAccessedAt: 50,
      metadata: { mode: 'workflow', phaseId: 'workflow-builder', presetQueryId: 'workflow-b' },
    })
    const tabs = Object.fromEntries([older, newer, emptyNewest, scheduleRun, otherWorkflow].map(t => [t.tabId, t]))
    const tabEvents = {
      'older-session': [event],
      'newer-session': [event],
      'schedule-session': [event],
      'other-session': [event],
    }

    expect(cachedWorkflowTabIdForPreset('workflow-a', tabs, tabEvents)).toBe('newer')
    expect(cachedWorkflowTabIdForPreset('workflow-c', tabs, tabEvents)).toBeUndefined()
    expect(cachedWorkflowTabIdForPreset('workflow-a', tabs, {})).toBeUndefined()
  })
})
