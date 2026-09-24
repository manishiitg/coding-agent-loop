// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, describe, expect, it, vi } from 'vitest'
import WorkflowWalkthrough from './WorkflowWalkthrough'
import { dismissWorkflowWalkthrough, isWorkflowWalkthroughDismissed } from '../../utils/onboarding'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

const targets: HTMLElement[] = []
const addTarget = (name: string, width = 200) => {
  const element = document.createElement('button')
  element.dataset.tour = name
  element.getBoundingClientRect = () => ({
    x: 20, y: 20, left: 20, top: 20, right: 20 + width, bottom: 50,
    width, height: 30, toJSON: () => ({}),
  }) as DOMRect
  document.body.append(element)
  targets.push(element)
  return element
}

afterEach(() => {
  targets.splice(0).forEach(target => target.remove())
})

describe('Context walkthroughs', () => {
  it('remembers each screen guide independently', () => {
    const surfaces = ['overview', 'empty-automation', 'automation', 'empty-crew', 'crew'] as const
    const originalStorage = Object.getOwnPropertyDescriptor(window, 'localStorage')
    const values = new Map<string, string>()
    Object.defineProperty(window, 'localStorage', {
      configurable: true,
      value: {
        getItem: (key: string) => values.get(key) ?? null,
        setItem: (key: string, value: string) => { values.set(key, value) },
      },
    })
    try {
      dismissWorkflowWalkthrough('empty-crew')
      expect(isWorkflowWalkthroughDismissed('empty-crew')).toBe(true)
      for (const surface of surfaces.filter(surface => surface !== 'empty-crew')) {
        expect(isWorkflowWalkthroughDismissed(surface)).toBe(false)
      }
    } finally {
      if (originalStorage) Object.defineProperty(window, 'localStorage', originalStorage)
      else Reflect.deleteProperty(window, 'localStorage')
    }
  })

  it('guides first-run users through the controls visible on Activity', async () => {
    addTarget('activity-feed')
    addTarget('workflow-add-edit')
    addTarget('global-activity')
    addTarget('global-schedules')
    addTarget('global-providers')
    const host = document.createElement('div')
    document.body.append(host)
    const root = createRoot(host)
    try {
      await act(async () => root.render(<WorkflowWalkthrough isOpen surface="overview" onClose={() => {}} />))
      const dialog = document.querySelector('[data-testid="workflow-walkthrough-dialog"]')!
      expect(dialog.textContent).toContain('1 of 5')
      expect(dialog.textContent).toContain('Open or create an automation')
      for (const title of ['Activity', 'Your Activity home', 'Schedules', 'Providers']) {
        await act(async () => (dialog.querySelector('[data-testid="workflow-walkthrough-next"]') as HTMLButtonElement).click())
        expect(dialog.textContent).toContain(title)
      }
      expect(dialog.querySelector('[data-testid="workflow-walkthrough-done"]')).not.toBeNull()
    } finally {
      await act(async () => root.unmount())
      host.remove()
    }
  })

  it('offers a usable exit while the workspace is loading', async () => {
    const host = document.createElement('div')
    document.body.append(host)
    const root = createRoot(host)
    try {
      await act(async () => root.render(<WorkflowWalkthrough isOpen surface="overview" onClose={() => {}} />))
      const dialog = document.querySelector('[data-testid="workflow-walkthrough-dialog"]')!
      expect(dialog.textContent).toContain('Explore AgentWorks')
      expect(dialog.querySelector('[data-testid="workflow-walkthrough-done"]')).not.toBeNull()
      expect(dialog.querySelector('[data-testid="workflow-walkthrough-next"]')).toBeNull()
    } finally {
      await act(async () => root.unmount())
      host.remove()
    }
  })

  it('moves to a visible step when the page changes under the tour', async () => {
    vi.useFakeTimers()
    const selector = addTarget('workflow-add-edit')
    const host = document.createElement('div')
    document.body.append(host)
    const root = createRoot(host)
    try {
      await act(async () => root.render(<WorkflowWalkthrough isOpen surface="overview" onClose={() => {}} />))
      const dialog = document.querySelector('[data-testid="workflow-walkthrough-dialog"]')!
      expect(dialog.textContent).toContain('Open or create an automation')
      selector.remove()
      addTarget('global-activity')
      await act(async () => vi.advanceTimersByTime(300))
      expect(dialog.textContent).toContain('Activity')
      expect(dialog.textContent).toContain('1 of 1')
    } finally {
      await act(async () => root.unmount())
      host.remove()
      vi.useRealTimers()
    }
  })

  it('starts a separate guide when an automation workspace opens', async () => {
    addTarget('workflow-add-edit')
    addTarget('workflow-chat-pane')
    addTarget('chat-input-box')
    addTarget('chat-send-controls')
    addTarget('workflow-dashboard')
    addTarget('workflow-views')
    addTarget('workflow-operations')
    addTarget('workflow-setup')
    const host = document.createElement('div')
    document.body.append(host)
    const root = createRoot(host)
    try {
      await act(async () => root.render(<WorkflowWalkthrough isOpen surface="automation" onClose={() => {}} />))
      const dialog = document.querySelector('[data-testid="workflow-walkthrough-dialog"]')!
      expect(dialog.getAttribute('aria-label')).toBe('Automation workspace walkthrough')
      expect(dialog.textContent).toContain('Current automation')
      expect(dialog.textContent).toContain('1 of 8')
      await act(async () => (dialog.querySelector('[data-testid="workflow-walkthrough-next"]') as HTMLButtonElement).click())
      expect(dialog.textContent).toContain('Build in chat')
    } finally {
      await act(async () => root.unmount())
      host.remove()
    }
  })

  it.each([
    { surface: 'empty-automation' as const, target: 'automation-empty-state', title: 'Start with an automation', label: 'Empty automation walkthrough' },
    { surface: 'empty-crew' as const, target: 'crew-empty-state', title: 'Your Crew starts here', label: 'Empty Crew walkthrough' },
    { surface: 'crew' as const, target: 'crew-selector', title: 'Current Crew member', label: 'Crew workspace walkthrough' },
  ])('starts the $surface guide on its own screen', async ({ surface, target, title, label }) => {
    addTarget(target)
    const host = document.createElement('div')
    document.body.append(host)
    const root = createRoot(host)
    try {
      await act(async () => root.render(<WorkflowWalkthrough isOpen surface={surface} onClose={() => {}} />))
      const dialog = document.querySelector('[data-testid="workflow-walkthrough-dialog"]')!
      expect(dialog.getAttribute('aria-label')).toBe(label)
      expect(dialog.textContent).toContain(title)
      expect(dialog.textContent).toContain('1 of 1')
    } finally {
      await act(async () => root.unmount())
      host.remove()
    }
  })

  it('skips Crew tools that are absent from a shared or collapsed workspace', async () => {
    addTarget('crew-selector')
    addTarget('crew-chat')
    addTarget('work-tools')
    const host = document.createElement('div')
    document.body.append(host)
    const root = createRoot(host)
    try {
      await act(async () => root.render(<WorkflowWalkthrough isOpen surface="crew" onClose={() => {}} />))
      const dialog = document.querySelector('[data-testid="workflow-walkthrough-dialog"]')!
      expect(dialog.textContent).toContain('1 of 3')
      await act(async () => (dialog.querySelector('[data-testid="workflow-walkthrough-next"]') as HTMLButtonElement).click())
      expect(dialog.textContent).toContain('Work together in chat')
      await act(async () => (dialog.querySelector('[data-testid="workflow-walkthrough-next"]') as HTMLButtonElement).click())
      expect(dialog.textContent).toContain('Workspace tools')
    } finally {
      await act(async () => root.unmount())
      host.remove()
    }
  })

  it('skips chat steps when the Crew pane is too narrow to use', async () => {
    addTarget('crew-selector')
    const chat = addTarget('crew-chat', 56)
    const input = addTarget('chat-input-box')
    chat.append(input)
    addTarget('crew-workspace')
    const host = document.createElement('div')
    document.body.append(host)
    const root = createRoot(host)
    try {
      await act(async () => root.render(<WorkflowWalkthrough isOpen surface="crew" onClose={() => {}} />))
      const dialog = document.querySelector('[data-testid="workflow-walkthrough-dialog"]')!
      expect(dialog.textContent).toContain('1 of 2')
      await act(async () => (dialog.querySelector('[data-testid="workflow-walkthrough-next"]') as HTMLButtonElement).click())
      expect(dialog.textContent).toContain('Workspace pane')
    } finally {
      await act(async () => root.unmount())
      host.remove()
    }
  })

  it('resets progress when moving from Activity into an automation', async () => {
    addTarget('workflow-add-edit')
    addTarget('global-activity')
    const host = document.createElement('div')
    document.body.append(host)
    const root = createRoot(host)
    try {
      await act(async () => root.render(<WorkflowWalkthrough isOpen surface="overview" onClose={() => {}} />))
      const dialog = document.querySelector('[data-testid="workflow-walkthrough-dialog"]')!
      await act(async () => (dialog.querySelector('[data-testid="workflow-walkthrough-next"]') as HTMLButtonElement).click())
      expect(dialog.textContent).toContain('Activity')
      addTarget('workflow-chat-pane')
      await act(async () => root.render(<WorkflowWalkthrough isOpen surface="automation" openToken={1} onClose={() => {}} />))
      expect(dialog.textContent).toContain('Current automation')
      expect(dialog.textContent).toContain('1 of 2')
    } finally {
      await act(async () => root.unmount())
      host.remove()
    }
  })
})
