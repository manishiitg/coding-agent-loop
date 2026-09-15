// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { describe, expect, it, vi } from 'vitest'

const { captured } = vi.hoisted(() => ({ captured: { props: null as null | { embedded: boolean; active: boolean; onClose: () => void } } }))
vi.mock('./scheduler/WorkflowScheduleRunsPanel', () => ({ default: (props: { embedded: boolean; active: boolean; onClose: () => void }) => {
  captured.props = props
  return <div data-testid="schedules-panel" />
} }))
const { storeState } = vi.hoisted(() => ({ storeState: { setShowSchedulesOverview: vi.fn() } }))
vi.mock('../stores/useAppStore', () => ({ useAppStore: (selector: (state: unknown) => unknown) => selector({ showSchedulesOverview: true, setShowSchedulesOverview: storeState.setShowSchedulesOverview }) }))
vi.mock('../stores/useLLMStore', () => ({ useLLMStore: (selector: (state: unknown) => unknown) => selector({ showLLMModal: false }) }))
import SchedulesPage from './SchedulesPage'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

describe('Schedules page', () => {
  it('embeds the active schedules panel and closes back to the workspace', async () => {
    const host = document.createElement('div'); document.body.append(host); const root = createRoot(host)
    try {
      await act(async () => root.render(<SchedulesPage />))
      expect(host.querySelector('h1')?.textContent).toBe('Schedules')
      expect(host.querySelector('[data-testid="schedules-panel"]')).not.toBeNull()
      expect(captured.props?.embedded).toBe(true)
      expect(captured.props?.active).toBe(true)
      await act(async () => captured.props?.onClose())
      expect(storeState.setShowSchedulesOverview).toHaveBeenCalledWith(false)
    } finally { await act(async () => root.unmount()); host.remove() }
  })
})
