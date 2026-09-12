import { describe, expect, it, vi } from 'vitest'
import { isReadOnlyWorkflowRunTab, workflowTabsNeedingHydration, hydrateWorkflowTabsPrioritized } from '../workflowTabHydration'
import type { PollingEvent } from '../../services/api-types'

const tab = (tabId: string, sessionId: string | undefined, metadata: Record<string, unknown>) =>
  ({ tabId, sessionId, metadata } as unknown as Parameters<typeof workflowTabsNeedingHydration>[0][number])

describe('workflowTabsNeedingHydration', () => {
  const events: Record<string, PollingEvent[]> = {
    'schedule-cron--d4007648_1': [],
    'schedule-cron--5227790a_2': [{ id: 'e1', type: 'user_message' } as unknown as PollingEvent],
    'chat-interactive': [],
  }
  const getTabEvents = (sessionId: string) => events[sessionId] ?? []

  it('includes a read-only scheduled-run tab whose events are gone (the stuck "Restoring previous session" case)', () => {
    const scheduled = tab('t1', 'schedule-cron--d4007648_1', { mode: 'workflow', isViewOnly: true, isScheduledRun: true, presetQueryId: 'wf_1' })
    const result = workflowTabsNeedingHydration([scheduled], getTabEvents)
    expect(result.map(t => t.tabId)).toEqual(['t1'])
  })

  it('skips tabs that already hold events, and tabs without a session', () => {
    const hydrated = tab('t2', 'schedule-cron--5227790a_2', { mode: 'workflow', isViewOnly: true, isScheduledRun: true })
    const blank = tab('t3', undefined, { mode: 'workflow', phaseId: 'workflow-builder' })
    const interactive = tab('t4', 'chat-interactive', { mode: 'workflow', phaseId: 'workflow-builder' })
    const result = workflowTabsNeedingHydration([hydrated, blank, interactive], getTabEvents)
    expect(result.map(t => t.tabId)).toEqual(['t4'])
  })

  it('ignores non-workflow tabs', () => {
    const multi = tab('t5', 'chat-interactive', { mode: 'multi-agent' })
    expect(workflowTabsNeedingHydration([multi], getTabEvents)).toEqual([])
  })
})

describe('isReadOnlyWorkflowRunTab', () => {
  it('is true only for a view-only workflow tab', () => {
    expect(isReadOnlyWorkflowRunTab({ metadata: { mode: 'workflow', isViewOnly: true } } as never)).toBe(true)
    expect(isReadOnlyWorkflowRunTab({ metadata: { mode: 'workflow' } } as never)).toBe(false)
    expect(isReadOnlyWorkflowRunTab({ metadata: { mode: 'multi-agent', isViewOnly: true } } as never)).toBe(false)
    expect(isReadOnlyWorkflowRunTab({ metadata: undefined } as never)).toBe(false)
  })
})

function deferred() {
  let resolve!: () => void
  const promise = new Promise<void>(yes => { resolve = yes })
  return { promise, resolve }
}

describe('prioritized workflow hydration', () => {
  it('starts the selected tab first and returns before inactive tabs finish', async () => {
    const tabs = ['old', 'selected', 'third', 'fourth'].map(id => tab(id, id, { mode: 'workflow' }))
    const pending = new Map(tabs.map(t => [t.tabId, deferred()]))
    const started: string[] = []
    let inFlight = 0
    let maxInFlight = 0
    const restore = hydrateWorkflowTabsPrioritized(tabs, 'selected', async t => {
      started.push(t.tabId)
      maxInFlight = Math.max(maxInFlight, ++inFlight)
      try { await pending.get(t.tabId)!.promise } finally { inFlight-- }
    }, vi.fn())
    expect(started).toEqual(['selected', 'old'])
    pending.get('selected')!.resolve()
    expect(await restore).toBe(4)
    expect(started).toEqual(['selected', 'old', 'third'])
    expect(inFlight).toBe(2)
    pending.get('old')!.resolve()
    pending.get('third')!.resolve()
    await vi.waitFor(() => expect(started).toContain('fourth'))
    pending.get('fourth')!.resolve()
    await vi.waitFor(() => expect(inFlight).toBe(0))
    expect(maxInFlight).toBe(2)
  })

  it('releases the selected tab on failure and continues the other tabs', async () => {
    const error = new Error('expired session')
    const onError = vi.fn()
    const tabs = [tab('selected', 'one', { mode: 'workflow' }), tab('other', 'two', { mode: 'workflow' })]
    const hydrate = vi.fn(async (t: typeof tabs[number]) => { if (t.tabId === 'selected') throw error })
    expect(await hydrateWorkflowTabsPrioritized(tabs, 'selected', hydrate, onError)).toBe(2)
    expect(onError).toHaveBeenCalledWith(tabs[0], error)
    expect(hydrate).toHaveBeenCalledWith(tabs[1])
  })

  it('handles an empty list and a selection from another workflow', async () => {
    const hydrate = vi.fn(async () => {})
    expect(await hydrateWorkflowTabsPrioritized([], null, hydrate, vi.fn())).toBe(0)
    const first = tab('first', 'one', { mode: 'workflow' })
    expect(await hydrateWorkflowTabsPrioritized([first], 'elsewhere', hydrate, vi.fn())).toBe(1)
    expect(hydrate).toHaveBeenCalledWith(first)
  })
})
