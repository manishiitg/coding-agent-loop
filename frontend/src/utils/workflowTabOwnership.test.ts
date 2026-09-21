import { describe, expect, it } from 'vitest'
import type { ChatTab } from '../stores/useChatStore'
import { activeWorkflowTabIdForPreset, workflowTabBelongsToPreset } from './workflowTabOwnership'

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
      workflowId: 'workflow-a',
    },
    ...overrides,
  }
}

describe('workflow tab ownership', () => {
  it('rejects an active tab owned by another workflow', () => {
    const workflowB = tab({
      tabId: 'tab-b',
      sessionId: 'session-b',
      metadata: { mode: 'workflow', phaseId: 'workflow-builder', workflowId: 'workflow-b' },
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
        workflowId: 'workflow-a',
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
