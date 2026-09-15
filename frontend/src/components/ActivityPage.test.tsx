// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { describe, expect, it, vi } from 'vitest'

vi.mock('./EmployeeDashboard', () => ({ EmployeeDashboard: () => <input aria-label="Find updates" /> }))
import ActivityPage from './ActivityPage'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

describe('Activity page', () => {
  it('shows the updates dashboard with no schedules tab', async () => {
    const host = document.createElement('div'); document.body.append(host); const root = createRoot(host)
    try {
      await act(async () => root.render(<ActivityPage />))
      expect(host.querySelector('h1')?.textContent).toBe('Activity')
      expect(host.querySelector('[aria-label="Find updates"]')).not.toBeNull()
      expect(host.querySelector('[role="tablist"]')).toBeNull()
      expect(host.querySelector('#activity-tab-schedules')).toBeNull()
    } finally { await act(async () => root.unmount()); host.remove() }
  })
})
