// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { describe, expect, it, vi } from 'vitest'
import type { ScheduledJob } from '../../../services/api-types'
import type { WorkflowScheduleGroup } from './helpers'
import { ScheduleGroupsView } from './ScheduleGroupsView'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

function buildGroup(overrides: Partial<WorkflowScheduleGroup> & { key: string; label: string }): WorkflowScheduleGroup {
  const jobs = (overrides.jobs ?? []) as ScheduledJob[]
  return {
    jobs,
    running: 0,
    missed: 0,
    issues: 0,
    enabled: jobs.filter(j => j.enabled).length,
    paused: jobs.filter(j => !j.enabled).length,
    runCount: 0,
    ...overrides,
    key: overrides.key,
    label: overrides.label,
  }
}

function buildPanel(groups: WorkflowScheduleGroup[], overrides: Record<string, unknown> = {}) {
  return {
    workflowGroups: groups,
    isSchedulerPaused: false,
    isReadOnlyUser: false,
    setActiveFilter: vi.fn(),
    setActiveView: vi.fn(),
    setSelectedWorkflowFilter: vi.fn(),
    handleToggleWorkflowGroupPause: vi.fn(),
    bulkUpdatingGroupKey: null,
    ...overrides,
  }
}

describe('schedule groups view', () => {
  const alpha = buildGroup({
    key: 'wf-alpha', label: 'Alpha',
    jobs: [
      { id: 'a1', name: 'Morning', enabled: true } as ScheduledJob,
      { id: 'a2', name: 'Evening', enabled: false } as ScheduledJob,
    ],
  })
  const beta = buildGroup({
    key: 'wf-beta', label: 'Beta',
    jobs: [{ id: 'b1', name: 'Nightly', enabled: false } as ScheduledJob],
  })

  it('opens a workflow group into its filtered schedules', async () => {
    const panel = buildPanel([alpha, beta])
    const host = document.createElement('div'); document.body.append(host); const root = createRoot(host)
    try {
      await act(async () => root.render(<ScheduleGroupsView panel={panel} />))
      expect(host.textContent).toContain('Alpha')
      expect(host.textContent).toContain('2 schedules')
      await act(async () => host.querySelector<HTMLButtonElement>('[aria-label="View Alpha schedules"]')!.click())
      expect(panel.setSelectedWorkflowFilter).toHaveBeenCalledWith('wf-alpha')
      expect(panel.setActiveFilter).toHaveBeenCalledWith('all')
      expect(panel.setActiveView).toHaveBeenCalledWith('schedules')
    } finally { await act(async () => root.unmount()); host.remove() }
  })

  it('pauses and resumes every schedule in a workflow group', async () => {
    const panel = buildPanel([alpha, beta])
    const host = document.createElement('div'); document.body.append(host); const root = createRoot(host)
    try {
      await act(async () => root.render(<ScheduleGroupsView panel={panel} />))
      await act(async () => host.querySelector<HTMLButtonElement>('[aria-label="Pause Alpha schedules"]')!.click())
      expect(panel.handleToggleWorkflowGroupPause).toHaveBeenCalledWith(alpha)
      await act(async () => host.querySelector<HTMLButtonElement>('[aria-label="Resume Beta schedules"]')!.click())
      expect(panel.handleToggleWorkflowGroupPause).toHaveBeenCalledWith(beta)
    } finally { await act(async () => root.unmount()); host.remove() }
  })

  it('hides group pause controls from read-only users', async () => {
    const panel = buildPanel([alpha], { isReadOnlyUser: true })
    const host = document.createElement('div'); document.body.append(host); const root = createRoot(host)
    try {
      await act(async () => root.render(<ScheduleGroupsView panel={panel} />))
      expect(host.querySelector('[aria-label="Pause Alpha schedules"]')).toBeNull()
      expect(host.querySelector('[aria-label="View Alpha schedules"]')).not.toBeNull()
    } finally { await act(async () => root.unmount()); host.remove() }
  })
})
