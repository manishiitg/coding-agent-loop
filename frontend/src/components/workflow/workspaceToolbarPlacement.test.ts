import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('workflow Ask AI placement', () => {
  it('keeps Ask AI inside every selected view instead of the global toolbar', () => {
    const toolbar = readFileSync('src/components/workflow/canvas/WorkflowToolbar.tsx', 'utf8')
    const host = readFileSync('src/components/workflow/canvas/WorkspaceViewHost.tsx', 'utf8')

    expect(toolbar).not.toContain('<AskAIButton')
    expect(host).toContain('data-ui-view-assistant')
    expect(host).toContain('getWorkspaceAskAIMessage(effectiveView)')
  })

  it('keeps Views and Setup mutually exclusive', () => {
    const toolbar = readFileSync('src/components/workflow/canvas/WorkflowToolbar.tsx', 'utf8')

    expect(toolbar).toContain("open={openToolbarGroup === 'views'}")
    expect(toolbar).toContain("open={openToolbarGroup === 'setup'}")
    expect(toolbar).toContain("onToggle={() => setOpenToolbarGroup('views')}")
    expect(toolbar).toContain("onToggle={() => setOpenToolbarGroup('setup')}")
  })

  it('keeps report separate and the frequent workflow tools in the requested order', () => {
    const toolbar = readFileSync('src/components/workflow/canvas/WorkflowToolbar.tsx', 'utf8')

    expect(toolbar).toContain("new Set<WorkspaceViewId>(['pulse', 'playbooks', 'flow', 'costs', 'files', 'browser', 'schedules'])")
    expect(toolbar).toContain("view.id === 'flow' || view.id === 'costs' || view.id === 'files' || view.id === 'browser'")
    expect(toolbar.indexOf('<ReportDocumentSwitcher')).toBeLessThan(toolbar.indexOf('title="Views: Pulse, playbooks, plan, costs, files, browser and schedules"'))
  })
})
