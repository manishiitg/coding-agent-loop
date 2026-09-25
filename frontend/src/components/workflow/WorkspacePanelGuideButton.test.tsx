// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { WorkspaceViewHeader } from './WorkspaceViewHeader'
import { WorkspacePanelGuideContext } from './WorkspacePanelGuideContext'
import { WorkspaceViewActions } from './WorkspaceViewActions'
import { BrowserWorkspacePanel } from './BrowserWorkspacePanel'
import { TooltipProvider } from '../ui/tooltip'
import { getWorkspacePanelGuide } from './workspacePanelGuides'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

describe('Panel walkthroughs', () => {
  it('adds an explanation beside Memory actions and closes with Escape', async () => {
    const host = document.createElement('div')
    document.body.append(host)
    const root = createRoot(host)
    try {
      await act(async () => root.render(
        <WorkspaceViewHeader
          title="Memory"
          actions={<><button type="button" data-testid="ask" aria-label="Ask AI">Ask AI</button><button type="button" data-testid="refresh" aria-label="Refresh memory">Refresh</button></>}
        />,
      ))
      const ask = host.querySelector('[data-testid="ask"]')!
      const refresh = host.querySelector('[data-testid="refresh"]')!
      const guideButton = host.querySelector('[aria-label="Walkthrough: Memory"]') as HTMLButtonElement
      expect(guideButton).not.toBeNull()
      expect(ask.compareDocumentPosition(refresh) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
      expect(ask.compareDocumentPosition(guideButton) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
      expect(guideButton.compareDocumentPosition(refresh) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()

      await act(async () => guideButton.click())
      const dialog = host.querySelector('[role="dialog"]')!
      expect(dialog.getAttribute('aria-label')).toBe('Memory walkthrough')
      expect(dialog.textContent).toContain('durable project context')
      expect(dialog.textContent).toContain('How to use it')
      expect(guideButton.getAttribute('aria-expanded')).toBe('true')

      await act(async () => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })))
      expect(host.querySelector('[role="dialog"]')).toBeNull()
      expect(document.activeElement).toBe(guideButton)
    } finally {
      await act(async () => root.unmount())
      host.remove()
    }
  })

  it('provides specific guidance for different panels', () => {
    expect(getWorkspacePanelGuide('Automation').purpose).toContain('work starts')
    expect(getWorkspacePanelGuide('Plan').howTo).toContain('flow')
    expect(getWorkspacePanelGuide('Dashboard').purpose).toContain('results')
    expect(getWorkspacePanelGuide('Schedules for Sales').purpose).toContain('automatically')
  })

  it('separates Crew and AgentWorks guidance for the same panel', async () => {
    const crew = getWorkspacePanelGuide('Automation', 'crew')
    const workflow = getWorkspacePanelGuide('Automation', 'agentworks')
    expect(crew.purpose).toContain('Crew member')
    expect(workflow.purpose).not.toContain('Crew member')
    expect(getWorkspacePanelGuide('Memory', 'crew').group).toBe('Main toolbar')
    expect(getWorkspacePanelGuide('Files', 'crew').group).toBe('Ops')
    expect(getWorkspacePanelGuide('Identity', 'crew').group).toBe('Setup')
    expect(getWorkspacePanelGuide('Plan').group).toBe('Main toolbar')
    expect(getWorkspacePanelGuide('Knowledge').group).toBe('Ops')
    expect(getWorkspacePanelGuide('Workflow playbooks').group).toBe('Setup')

    const host = document.createElement('div')
    document.body.append(host)
    const root = createRoot(host)
    try {
      await act(async () => root.render(
        <WorkspacePanelGuideContext.Provider value="crew">
          <WorkspaceViewHeader title="Automation" />
        </WorkspacePanelGuideContext.Provider>,
      ))
      await act(async () => (host.querySelector('[aria-label="Walkthrough: Automation"]') as HTMLButtonElement).click())
      const dialog = host.querySelector('[role="dialog"]')!
      expect(dialog.textContent).toContain('Crew · Main toolbar')
      expect(dialog.textContent).toContain('Crew member')
    } finally {
      await act(async () => root.unmount())
      host.remove()
    }
  })

  it('explains the active tab inside a shared panel header', async () => {
    const host = document.createElement('div')
    document.body.append(host)
    const root = createRoot(host)
    try {
      await act(async () => root.render(
        <WorkspacePanelGuideContext.Provider value="crew">
          <WorkspaceViewHeader title="Automation" helpTopic="Automation · Chats" />
        </WorkspacePanelGuideContext.Provider>,
      ))
      await act(async () => (host.querySelector('[aria-label="Walkthrough: Automation · Chats"]') as HTMLButtonElement).click())
      expect(host.querySelector('[role="dialog"]')?.textContent).toContain('earlier conversations')

      await act(async () => root.render(
        <WorkspacePanelGuideContext.Provider value="crew">
          <WorkspaceViewHeader title="Automation" helpTopic="Automation · Schedules" />
        </WorkspacePanelGuideContext.Provider>,
      ))
      expect(host.querySelector('[role="dialog"]')?.textContent).toContain('saved instruction')
      expect(host.querySelector('[role="dialog"]')?.textContent).toContain('Crew · Main toolbar')
      expect(getWorkspacePanelGuide('Knowledge · Database').group).toBe('Ops')
      expect(getWorkspacePanelGuide('Identity · Secrets', 'crew').group).toBe('Setup')
    } finally {
      await act(async () => root.unmount())
      host.remove()
    }
  })

  it('places Browser help between Ask AI and Refresh exactly once', () => {
    const html = renderToStaticMarkup(
      <WorkspacePanelGuideContext.Provider value="crew">
        <TooltipProvider>
          <BrowserWorkspacePanel
            workspacePath={null}
            browserMode="auto"
            onBrowserModeChange={() => {}}
            cdpPort={9222}
            onCdpPortChange={() => {}}
            cdpConnected={null}
            cdpError={null}
            cdpChecking={false}
            onCheckCdpConnection={() => {}}
            assistantControl={<WorkspaceViewActions workspacePath={null} message="Browser help" onRefresh={() => {}} />}
          />
        </TooltipProvider>
      </WorkspacePanelGuideContext.Provider>,
    )
    const ask = html.indexOf('aria-label="Ask AI"')
    const walkthrough = html.indexOf('aria-label="Walkthrough: Browser"')
    const refresh = html.indexOf('aria-label="Refresh view"')
    expect(ask).toBeGreaterThanOrEqual(0)
    expect(ask).toBeLessThan(walkthrough)
    expect(walkthrough).toBeLessThan(refresh)
    expect(html.match(/aria-label="Walkthrough: Browser"/g)).toHaveLength(1)
  })
})
