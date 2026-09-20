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

  it('keeps exactly one of the Views, Ops, and Setup inline groups open', () => {
    const toolbar = readFileSync('src/components/workflow/canvas/WorkflowToolbar.tsx', 'utf8')

    expect(toolbar).not.toContain('label="Tools"')
    expect(toolbar).toContain('label="Views"')
    expect(toolbar).toContain('label="Ops"')
    expect(toolbar).toContain('label="Setup"')
    expect(toolbar).toContain('hideLabel')
    expect(toolbar.match(/hideToggleWhenOpen/g)).toHaveLength(3)
    expect(toolbar).toContain("useState<'views' | 'ops' | 'setup'>('views')")
    expect(toolbar).toContain("open={openToolbarMenu === 'views'}")
    expect(toolbar).toContain("open={openToolbarMenu === 'ops'}")
    expect(toolbar).toContain("open={openToolbarMenu === 'setup'}")
    expect(toolbar).toContain("onToggle={() => toggleToolbarMenu('views')}")
    expect(toolbar).toContain("onToggle={() => toggleToolbarMenu('ops')}")
    expect(toolbar).toContain("onToggle={() => toggleToolbarMenu('setup')}")
    expect(toolbar).toContain('<WorkspaceToolbarGroup')
    expect(toolbar).toContain('<ToolbarInlineItem')
    expect(toolbar).not.toContain('ToolbarPopoverGroup')
    expect(toolbar).not.toContain('ToolbarPopoverItem')
    expect(toolbar).not.toContain('role="menu"')
  })

  it('keeps report separate, Knowledge visible, Costs and Execution logs in Ops, and Playbooks in Setup', () => {
    const toolbar = readFileSync('src/components/workflow/canvas/WorkflowToolbar.tsx', 'utf8')

    expect(toolbar).toContain("new Set<WorkspaceViewId>(['pulse', 'flow', 'browser', 'workshop'])")
    expect(toolbar).toContain("new Set<WorkspaceViewId>(['knowledge', 'costs', 'execution-logs', 'files', 'backup', 'publish', 'notify'])")
    expect(toolbar).toContain("playbooks: 'Playbooks'")
    expect(toolbar).toContain("mcp: 'Integrations'")
    expect(toolbar).toContain("identity: 'Identity'")
    expect(toolbar).not.toContain("bots: 'Bots'")
    expect(toolbar).not.toContain("email: 'Gmail'")
    expect(toolbar).not.toContain("secrets: 'Secrets'")
    expect(toolbar).not.toContain("folders: 'Folders'")
    expect(toolbar).toContain("view.toolbarGroup === 'capabilities'")
    expect(toolbar).toContain('PRIMARY_TOOLBAR_VIEW_IDS.has(view.id)')
    expect(toolbar.indexOf('<ReportDocumentSwitcher')).toBeLessThan(toolbar.indexOf('aria-label={pendingDecisionCount'))
    expect(toolbar.indexOf('<WorkflowActivityButton')).toBeGreaterThan(toolbar.indexOf('aria-label={pendingDecisionCount'))
    expect(toolbar.indexOf('<WorkflowActivityButton')).toBeLessThan(toolbar.indexOf('workspaceViewDefinitions.map'))
  })

  it('uses Dashboard for first-time AgentWorks users while preserving saved views', () => {
    const store = readFileSync('src/stores/useWorkflowStore.ts', 'utf8')

    expect(store).toContain("normalizeCanvasViewId(getWorkflowStorageItem(LEGACY_CANVAS_VIEW_MODE_KEY)) ?? 'report'")
    expect(store).toContain("loadLegacyWorkspaceViewByPreset()[presetId] ?? 'report'")
    expect(store).toContain('persistedUIState.workflowWorkspaceView ??')
  })
})
