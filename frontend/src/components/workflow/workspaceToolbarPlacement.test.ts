import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('workflow Ask AI placement', () => {
  it('keeps Ask AI inside every selected view instead of the global toolbar', () => {
    const toolbar = readFileSync('src/components/workflow/canvas/WorkflowToolbar.tsx', 'utf8')
    const host = readFileSync('src/components/workflow/canvas/WorkspaceViewHost.tsx', 'utf8')

    expect(toolbar).not.toContain('<AskAIButton')
    expect(host).not.toContain('data-ui-view-assistant')
    expect(host).toContain('headerAction={askAI(')
    expect(host).toContain('assistantControl={workspacePath ? (')
  })

  it('keeps frequent tools visible without a group label and uses mutually exclusive Ops and Setup popovers', () => {
    const toolbar = readFileSync('src/components/workflow/canvas/WorkflowToolbar.tsx', 'utf8')

    expect(toolbar).not.toContain('label="Views"')
    expect(toolbar).not.toContain('label="Tools"')
    expect(toolbar).toContain("open={openToolbarMenu === 'ops'}")
    expect(toolbar).toContain("open={openToolbarMenu === 'setup'}")
    expect(toolbar).toContain("onToggle={() => toggleToolbarMenu('ops')}")
    expect(toolbar).toContain("onToggle={() => toggleToolbarMenu('setup')}")
    expect(toolbar).toContain('role="menu" aria-label={label}')
  })

  it('keeps report separate, Knowledgebase visible, Costs in Ops, and Playbooks in Setup', () => {
    const toolbar = readFileSync('src/components/workflow/canvas/WorkflowToolbar.tsx', 'utf8')

    expect(toolbar).toContain("new Set<WorkspaceViewId>(['pulse', 'flow', 'knowledgebase', 'files', 'browser', 'workshop', 'schedules', 'execution-logs'])")
    expect(toolbar).toContain("new Set<WorkspaceViewId>(['costs', 'learnings', 'database', 'evaluation', 'backup', 'publish', 'notify'])")
    expect(toolbar).toContain("playbooks: 'Playbooks'")
    expect(toolbar).toContain('PRIMARY_TOOLBAR_VIEW_IDS.has(view.id)')
    expect(toolbar.indexOf('<ReportDocumentSwitcher')).toBeLessThan(toolbar.indexOf('aria-label={pendingDecisionCount'))
    expect(toolbar.indexOf('<WorkflowActivityButton')).toBeGreaterThan(toolbar.indexOf('aria-label={pendingDecisionCount'))
    expect(toolbar.indexOf('<WorkflowActivityButton')).toBeLessThan(toolbar.indexOf('workspaceViewDefinitions.map'))
  })
})
