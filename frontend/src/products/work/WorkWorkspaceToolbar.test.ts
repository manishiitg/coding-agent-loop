import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('WorkWorkspaceToolbar', () => {
  it('keeps primary views visible and groups Files and Database under Ops', () => {
    const source = readFileSync('src/products/work/WorkWorkspacePane.tsx', 'utf8')

    expect(source).not.toContain('label="Views"')
    expect(source).toContain('label="Ops"')
    expect(source).toContain("open={openGroup === 'ops'}")
    expect(source).toContain("open={openGroup === 'setup'}")
    expect(source).toContain("OPS_BUTTONS.some(item => item.id === view) ? 'ops'")
    expect(source).toContain("visibleViews.filter(item => item.id !== 'dashboard').map")
    expect(source.indexOf("const OPS_BUTTONS")).toBeLessThan(source.indexOf("id: 'files', label: 'Files'"))
    expect(source.indexOf("const OPS_BUTTONS")).toBeLessThan(source.indexOf("id: 'database', label: 'Database'"))
    expect(source).not.toContain("id: 'history'")
    expect(source).toContain("id: 'schedules', label: 'Automations'")
    expect(source).toContain('botContent=')
    expect(source).toContain("productTriggerScope={enabledPanels?.has('triggers')")
    expect(source).toContain('showAdditionalGroup')
  })

  it('binds the Work chat to acknowledged workspace view controls', () => {
    const source = readFileSync('src/products/work/WorkSurface.tsx', 'utf8')

    expect(source).toContain('useWorkspaceUIControl(activeSessionId ?? undefined, workUIAdapter)')
    expect(source).toContain('data-ui-workspace={selected.workspacePath}')
    expect(source).toContain('data-ui-view={workPresentationView(workspaceView)}')
    expect(source).toContain('data-ui-view-mounted')
    expect(source).toContain("report: 'dashboard'")
    expect(source).toContain("llm: 'models'")
    expect(source).toContain('landingContent={<WorkNewChatGuide />}')
    expect(source).toContain('This is the persistent conversation for this Crew project.')
  })

  it('opens Dashboard for a Crew project only when no saved view exists', () => {
    const source = readFileSync('src/products/work/WorkSurface.tsx', 'utf8')

    expect(source).toContain("const WORK_VIEW_PREFERENCE_KEY = 'work_workspace_view'")
    expect(source).toContain("if (saved === 'history') return 'schedules'")
    expect(source).toContain("return saved && WORKSPACE_VIEW_IDS.has(saved as WorkWorkspaceView) ? saved as WorkWorkspaceView : 'dashboard'")
    expect(source).toContain('writeWorkWorkspaceView(selected?.id, view)')
    expect(source).toContain('setWorkspaceView(readWorkWorkspaceView(selected?.id))')
  })
})
