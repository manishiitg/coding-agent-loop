// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { describe, expect, it, vi } from 'vitest'

vi.mock('./EmployeeDashboard', () => ({ EmployeeDashboard: () => <input aria-label="Find updates" /> }))
vi.mock('./scheduler/WorkflowScheduleRunsPanel', () => ({ default: ({ embedded, active }: { embedded: boolean; active: boolean }) => <div data-embedded={embedded} data-active={active}><input aria-label="Find schedules" /></div> }))
vi.mock('../stores/useAppStore', () => ({ useAppStore: (selector: (state: unknown) => unknown) => selector({ showWorkflowsOverview: true, setShowWorkflowsOverview: vi.fn() }) }))
vi.mock('../stores/useLLMStore', () => ({ useLLMStore: (selector: (state: unknown) => unknown) => selector({ showLLMModal: false }) }))
import ActivityPage from './ActivityPage'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

describe('Activity page', () => {
  it('switches between updates and embedded schedules by keyboard, retaining each view', async () => {
    const host = document.createElement('div'); document.body.append(host); const root = createRoot(host)
    try {
      await act(async () => root.render(<ActivityPage />))
      const updates = host.querySelector<HTMLButtonElement>('#activity-tab-updates')!
      const schedules = host.querySelector<HTMLButtonElement>('#activity-tab-schedules')!
      const updateInput = host.querySelector<HTMLInputElement>('[aria-label="Find updates"]')!
      updateInput.value = 'Trading'
      expect(updates.getAttribute('aria-selected')).toBe('true')
      expect(host.querySelector('[aria-label="Find schedules"]')).toBeNull()
      await act(async () => schedules.click())
      const scheduleInput = host.querySelector<HTMLInputElement>('[aria-label="Find schedules"]')!
      scheduleInput.value = 'Daily'
      expect(host.querySelector('[data-embedded="true"][data-active="true"]')).not.toBeNull()
      expect(host.querySelector('#activity-panel-updates')?.hasAttribute('hidden')).toBe(true)
      await act(async () => schedules.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true })))
      expect(document.activeElement).toBe(updates)
      expect(updates.getAttribute('aria-selected')).toBe('true')
      expect(host.querySelector('[aria-label="Find updates"]')).toBe(updateInput)
      expect(updateInput.value).toBe('Trading')
      expect(host.querySelector('[data-active="false"]')).not.toBeNull()
      await act(async () => updates.dispatchEvent(new KeyboardEvent('keydown', { key: 'End', bubbles: true })))
      expect(document.activeElement).toBe(schedules)
      expect(host.querySelector('[aria-label="Find schedules"]')).toBe(scheduleInput)
      expect(scheduleInput.value).toBe('Daily')
    } finally { await act(async () => root.unmount()); host.remove() }
  })
})
