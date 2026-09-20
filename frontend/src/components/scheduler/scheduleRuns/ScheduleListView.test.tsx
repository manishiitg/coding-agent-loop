// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { describe, expect, it, vi } from 'vitest'
import { ScheduleListView } from './ScheduleListView'
import type { ScheduledJob } from '../../../services/api-types'

vi.mock('../../../services/api', () => ({ getApiBaseUrl: () => 'https://agent.example', getAuthToken: () => null }))

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

const job = (overrides: Partial<ScheduledJob> & { id: string; name: string }): ScheduledJob => ({
  description: '',
  entity_type: 'workflow',
  cron_expression: '0 9 * * 1-5',
  timezone: 'UTC',
  enabled: true,
  run_count: 0,
  consecutive_failures: 0,
  ...overrides,
})

function stubPanel(filteredJobs: ScheduledJob[]) {
  const noop = () => {}
  return {
    filteredJobs,
    presetMap: new Map(),
    showWorkflowIdentityInScheduleRows: false,
    isReadOnlyUser: false,
    handleStopRun: noop,
    handleTrigger: noop,
    triggering: null,
    handleToggle: noop,
    handleRunDestination: noop,
    openActionMenuJobId: null,
    setOpenActionMenuJobId: noop,
    handleDelete: noop,
    expandedRunHistoryJobIds: new Set<string>(),
    runsByJob: {},
    runsLoadingJobIds: new Set<string>(),
    deletingRunSessionIds: new Set<string>(),
    toggleRunHistory: noop,
    openScheduledRun: noop,
    deleteScheduledRunSession: noop,
  }
}

async function renderList(filteredJobs: ScheduledJob[]) {
  const host = document.createElement('div')
  document.body.append(host)
  const root = createRoot(host)
  await act(async () => root.render(<ScheduleListView panel={stubPanel(filteredJobs)} />))
  return { host, unmount: async () => { await act(async () => root.unmount()); host.remove() } }
}

describe('ScheduleListView declutter', () => {
  it('hides a single group but shows multiple groups', async () => {
    const { host, unmount } = await renderList([
      job({ id: 'a', name: 'Solo', group_names: ['Default Group'], run_count: 1 }),
      job({ id: 'b', name: 'Multi', group_names: ['Team A', 'Team B'], run_count: 1 }),
    ])
    try {
      expect(host.textContent).not.toContain('Default Group')
      expect(host.textContent).toContain('Groups: Team A, Team B')
    } finally {
      await unmount()
    }
  })

  it('shows section counts without description lines', async () => {
    const { host, unmount } = await renderList([
      job({ id: 'a', name: 'Solo', run_count: 0 }),
    ])
    try {
      expect(host.textContent).toContain('Automation schedules')
      expect(host.textContent).toContain('· 1')
      expect(host.textContent).not.toContain('idle, paused, or waiting')
    } finally {
      await unmount()
    }
  })

  it('keeps the route pill next to the schedule name', async () => {
    const { host, unmount } = await renderList([
      job({ id: 'a', name: 'Trade', route_selections: { step: 'trade' }, run_count: 1 }),
    ])
    try {
      const titleRow = host.querySelector('[title="Trade"]')?.parentElement
      expect(titleRow?.textContent).toContain('Route: trade')
    } finally {
      await unmount()
    }
  })

  it('says Not run yet instead of Last ran: never', async () => {
    const { host, unmount } = await renderList([
      job({ id: 'a', name: 'Fresh', run_count: 0 }),
    ])
    try {
      expect(host.textContent).toContain('Not run yet')
      expect(host.textContent).not.toContain('Last ran: never')
    } finally {
      await unmount()
    }
  })
})
