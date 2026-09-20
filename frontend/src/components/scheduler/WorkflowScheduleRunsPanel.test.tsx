// @vitest-environment happy-dom
import { readFileSync } from 'node:fs'
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { describe, expect, it, vi } from 'vitest'

const { hookState } = vi.hoisted(() => ({ hookState: { current: null as null | Record<string, unknown> } }))
vi.mock('./scheduleRuns/useScheduleRunsData', () => ({ useScheduleRunsData: () => hookState.current }))
vi.mock('./scheduleRuns/ScheduleTableView', () => ({ ScheduleTableView: () => <div data-testid="schedule-table" /> }))
vi.mock('./scheduleRuns/ScheduleListView', () => ({ ScheduleListView: () => null }))
vi.mock('./scheduleRuns/ScheduleGroupsView', () => ({ ScheduleGroupsView: () => null }))
vi.mock('./scheduleRuns/ScheduleCalendarView', () => ({ ScheduleCalendarView: () => null }))
vi.mock('./scheduleRuns/ScheduleOverviewView', () => ({ ScheduleOverviewView: () => null }))
vi.mock('../workflow/WorkflowAPITriggersView', () => ({ default: () => null }))
vi.mock('../workflow/ProductAPITriggersView', () => ({ default: () => <div data-testid="product-webhooks" /> }))
vi.mock('../automation/TriggerDeliveryHistoryPanel', () => ({ TriggerDeliveryHistoryPanel: () => <div data-testid="delivery-history" /> }))
vi.mock('../../stores/useWorkflowStore', () => ({ useWorkflowStore: (selector: (state: unknown) => unknown) => selector({ workspaceViewTarget: null }) }))
import WorkflowScheduleRunsPanel from './WorkflowScheduleRunsPanel'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

function buildPanelState(overrides: Record<string, unknown> = {}) {
  const setActiveView = vi.fn()
  const setSelectedWorkflowFilter = vi.fn()
  return {
    fns: { setActiveView, setSelectedWorkflowFilter },
    state: {
      panelTitle: 'Automation Schedules',
      isLoading: false,
      error: null,
      isWorkflowScoped: false,
      activeView: 'schedules',
      setActiveView,
      activeFilter: 'all',
      setActiveFilter: vi.fn(),
      searchQuery: '',
      setSearchQuery: vi.fn(),
      selectedWorkflowFilter: 'all',
      setSelectedWorkflowFilter,
      panelJobs: [{ id: 'a1', name: 'Morning' }],
      workflowOptions: [{ value: 'wf-alpha', label: 'Alpha' }],
      filteredJobs: [{ id: 'a1', name: 'Morning' }],
      workflowGroups: [],
      monthlyCalendar: { total: 0 },
      filterPills: [{ key: 'all', label: 'All', count: 1 }],
      activeFilterLabel: 'All',
      workflowScheduleSummary: { workflows: 1, running: 0, fullyPaused: 0, partlyPaused: 0 },
      summary: { total: 1 },
      isSchedulerPaused: false,
      isReadOnlyUser: false,
      handleToggleGlobalPause: vi.fn(),
      isUpdatingSchedulerPause: false,
      loadJobs: vi.fn(),
      ...overrides,
    },
  }
}

async function renderPanel(state: Record<string, unknown>) {
  hookState.current = state
  const host = document.createElement('div'); document.body.append(host); const root = createRoot(host)
  await act(async () => root.render(<WorkflowScheduleRunsPanel embedded onClose={() => {}} />))
  return { host, root }
}

describe('schedule panel views', () => {
  it('offers a Workflows view alongside List and Calendar', async () => {
    const { fns, state } = buildPanelState()
    const { host, root } = await renderPanel(state)
    try {
      const labels = Array.from(host.querySelectorAll('[aria-label="Schedule views"] button')).map(b => b.textContent)
      expect(labels).toEqual(['Workflows', 'List', 'Calendar'])
      await act(async () => host.querySelectorAll<HTMLButtonElement>('[aria-label="Schedule views"] button')[0]!.click())
      expect(fns.setActiveView).toHaveBeenCalledWith('by-workflow')
    } finally { await act(async () => root.unmount()); host.remove() }
  })

  it('returns from a drilled-down workflow to the grouping', async () => {
    const { fns, state } = buildPanelState({ selectedWorkflowFilter: 'wf-alpha' })
    const { host, root } = await renderPanel(state)
    try {
      const back = Array.from(host.querySelectorAll<HTMLButtonElement>('button')).find(b => b.textContent?.includes('All workflows'))!
      expect(host.textContent).toContain('All workflows')
      expect(host.textContent).toContain('Alpha')
      await act(async () => back.click())
      expect(fns.setSelectedWorkflowFilter).toHaveBeenCalledWith('all')
      expect(fns.setActiveView).toHaveBeenCalledWith('by-workflow')
    } finally { await act(async () => root.unmount()); host.remove() }
  })

  it('shows no back control when no workflow filter is active', async () => {
    const { state } = buildPanelState({ selectedWorkflowFilter: 'all' })
    const { host, root } = await renderPanel(state)
    try {
      expect(host.textContent).not.toContain('All workflows')
    } finally { await act(async () => root.unmount()); host.remove() }
  })

  it('hides its header row for hosts that own the header', async () => {
    const { state } = buildPanelState({ isWorkflowScoped: true })
    hookState.current = state
    const host = document.createElement('div'); document.body.append(host); const root = createRoot(host)
    await act(async () => root.render(<WorkflowScheduleRunsPanel embedded hideHeader onClose={() => {}} />))
    try {
      expect(host.textContent).not.toContain('Automation Schedules')
      expect(host.querySelector('[aria-label="Schedule views"]')).not.toBeNull()
    } finally { await act(async () => root.unmount()); host.remove() }
  })

  it('reloads jobs when the owning header bumps refreshToken', async () => {
    const { state } = buildPanelState()
    const loadJobs = state.loadJobs as ReturnType<typeof vi.fn>
    hookState.current = state
    const host = document.createElement('div'); document.body.append(host); const root = createRoot(host)
    await act(async () => root.render(<WorkflowScheduleRunsPanel embedded hideHeader refreshToken={0} onClose={() => {}} />))
    try {
      expect(loadJobs).not.toHaveBeenCalled()
      await act(async () => root.render(<WorkflowScheduleRunsPanel embedded hideHeader refreshToken={1} onClose={() => {}} />))
      expect(loadJobs).toHaveBeenCalledWith(true)
    } finally { await act(async () => root.unmount()); host.remove() }
  })

  it('reports status snapshots for the owning header', async () => {
    const { state } = buildPanelState()
    const onStatus = vi.fn()
    hookState.current = state
    const host = document.createElement('div'); document.body.append(host); const root = createRoot(host)
    await act(async () => root.render(<WorkflowScheduleRunsPanel embedded hideHeader onStatus={onStatus} onClose={() => {}} />))
    try {
      expect(onStatus).toHaveBeenCalled()
      const snapshot = onStatus.mock.calls[0][0] as Record<string, unknown>
      expect((snapshot.summary as { total: number }).total).toBe(1)
      expect(snapshot.isLoading).toBe(false)
    } finally { await act(async () => root.unmount()); host.remove() }
  })

  it('keeps views, search, and state filter in one toolbar row without a count line', async () => {
    const { state } = buildPanelState({ isWorkflowScoped: true })
    const { host, root } = await renderPanel(state)
    try {
      const toolbar = host.querySelector('.sticky')
      expect(toolbar?.querySelector('[aria-label="Schedule views"]')).not.toBeNull()
      expect(toolbar?.querySelector('[aria-label="Search schedules"]')).not.toBeNull()
      expect(toolbar?.querySelector('[aria-label="Filter schedules by state"]')).not.toBeNull()
      expect(host.querySelectorAll('[aria-label="Search schedules"]').length).toBe(1)
      expect(host.textContent).not.toContain('1 schedule · All')
    } finally { await act(async () => root.unmount()); host.remove() }
  })

  it('shares Schedules, Triggers, and Bots in one product automation hub', async () => {
    const { state } = buildPanelState()
    hookState.current = state
    const host = document.createElement('div'); document.body.append(host); const root = createRoot(host)
    await act(async () => root.render(<WorkflowScheduleRunsPanel embedded entityType="product" productTriggerScope={{ profileId: 'work', projectId: 'project-1' }} botContent={<div data-testid="bots">Bots content</div>} onClose={() => {}} />))
    try {
      const tabs = Array.from(host.querySelectorAll<HTMLButtonElement>('[aria-label="Automation channels"] button'))
      expect(tabs.map(tab => tab.textContent)).toEqual(['Schedules', 'Triggers', 'Bots'])
      await act(async () => { tabs[1]!.click(); await Promise.resolve() })
      expect(host.querySelector('[data-testid="product-webhooks"]')).not.toBeNull()
      await act(async () => { tabs[2]!.click(); await Promise.resolve() })
      expect(host.querySelector('[data-testid="bots"]')).not.toBeNull()
    } finally { await act(async () => root.unmount()); host.remove() }
  })
})

describe('schedule group headers', () => {
  it('use card-title style instead of kickers', () => {
    const list = readFileSync('src/components/scheduler/scheduleRuns/ScheduleListView.tsx', 'utf8')
    expect(list).toContain('text-sm font-semibold text-foreground">Automation schedules')
    expect(list).toContain('text-sm font-semibold text-amber-600 dark:text-amber-400">Missed schedules')
    expect(list).not.toContain('uppercase tracking-wide text-amber-600')
    expect(list).not.toContain('uppercase tracking-wide text-muted-foreground">Automation schedules')
  })
})
