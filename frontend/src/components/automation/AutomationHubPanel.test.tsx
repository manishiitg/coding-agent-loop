// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { describe, expect, it, vi } from 'vitest'

const storeState = { workspaceViewTarget: null as null | { view: string; target: string; token: number } }
const openWorkspaceView = vi.fn()

vi.mock('../../stores/useWorkflowStore', () => ({
  useWorkflowStore: Object.assign(
    (selector: (state: unknown) => unknown) => selector(storeState),
    { getState: () => ({ openWorkspaceView }) },
  ),
}))
vi.mock('../scheduler/WorkflowScheduleRunsPanel', () => ({ default: () => <div data-testid="schedules">Schedule content</div> }))
vi.mock('../workflow/ProductAPITriggersView', () => ({ default: () => <div data-testid="triggers">Trigger content</div> }))
vi.mock('../workflow/WorkflowAPITriggersView', () => ({ default: () => <div data-testid="workflow-triggers">Workflow trigger content</div> }))
vi.mock('./TriggerDeliveryHistoryPanel', () => ({ TriggerDeliveryHistoryPanel: () => <div data-testid="delivery-history" /> }))

import { AutomationHubPanel } from './AutomationHubPanel'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

describe('AutomationHubPanel', () => {
  it('keeps chats, schedules, triggers, and bots in one responsive identity-aware panel', async () => {
    const host = document.createElement('div')
    document.body.append(host)
    const root = createRoot(host)
    await act(async () => root.render(<AutomationHubPanel
      entityType="product"
      workspacePath="Work/projects/release"
      entityLabel="Release Crew"
      entityIcon="🚀"
      productTriggerScope={{ profileId: 'work', projectId: 'release' }}
      chatContent={<div data-testid="chats">Chat history</div>}
      botContent={<div data-testid="bots">Bot routes</div>}
    />))

    try {
      expect(host.textContent).toContain('🚀')
      expect(host.textContent).toContain('Release Crew')
      const tabs = Array.from(host.querySelectorAll<HTMLButtonElement>('[aria-label="Automation center"] [role="tab"]'))
      expect(tabs.map(tab => tab.getAttribute('aria-label'))).toEqual(['Chats', 'Schedules', 'Triggers', 'Bots'])
      expect(host.querySelector('[data-testid="chats"]')).not.toBeNull()

      await act(async () => { tabs[1]!.click(); await Promise.resolve() })
      expect(host.querySelector('[data-testid="schedules"]')).not.toBeNull()
      await act(async () => { tabs[2]!.click(); await Promise.resolve() })
      expect(host.querySelector('[data-testid="triggers"]')).not.toBeNull()
      await act(async () => { tabs[3]!.click(); await Promise.resolve() })
      expect(host.querySelector('[data-testid="bots"]')).not.toBeNull()
      expect(openWorkspaceView).toHaveBeenLastCalledWith('workshop', 'bots')
    } finally {
      await act(async () => root.unmount())
      host.remove()
    }
  })
})
