// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { describe, expect, it, vi } from 'vitest'
import type { ScheduledJob } from '../../../services/api-types'
import { ScheduleCalendarView } from './ScheduleCalendarView'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

describe('schedule calendar view', () => {
  it('labels projections and hides paused schedules until requested', async () => {
    const date = new Date()
    const key = `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-01`
    const active = { id: 'active', name: 'Active schedule', enabled: true } as ScheduledJob
    const paused = { id: 'paused', name: 'Paused schedule', enabled: false } as ScheduledJob
    const panel = {
      setCalendarMonth: vi.fn(),
      monthlyCalendar: { label: 'Current month', localTimeZone: 'Asia/Kolkata', total: 2, cells: [{ key, date: key, day: 1, items: [
        { job: active, time: '09:00', label: 'Active schedule' },
        { job: paused, time: '10:00', label: 'Paused schedule' },
      ] }] },
      selectedCalendarDate: key,
      setSelectedCalendarDate: vi.fn(),
      showJobInWorkflowGroups: vi.fn(),
    }
    const host = document.createElement('div'); document.body.append(host); const root = createRoot(host)
    try {
      await act(async () => root.render(<ScheduleCalendarView panel={panel} />))
      expect(host.textContent).toContain('1 planned occurrence')
      expect(host.textContent).toContain('planned times')
      expect(host.textContent).not.toContain('Paused schedule')
      const checkbox = host.querySelector<HTMLInputElement>('input[type="checkbox"]')!
      await act(async () => { checkbox.click() })
      expect(host.textContent).toContain('2 planned occurrences')
      expect(host.textContent).toContain('Paused schedule · Paused')
    } finally { await act(async () => root.unmount()); host.remove() }
  })
})
