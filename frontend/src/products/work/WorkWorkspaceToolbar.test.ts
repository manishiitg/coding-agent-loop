import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('WorkWorkspaceToolbar', () => {
  it('keeps workspace views visible and expands Setup for a selected setup panel', () => {
    const source = readFileSync('src/products/work/WorkWorkspacePane.tsx', 'utf8')

    expect(source).not.toContain('label="Views"')
    expect(source).toContain('open={setupOpen}')
    expect(source).toContain('onToggle={() => setSetupOpen(current => !current)}')
    expect(source).toContain('setSetupOpen(SETUP_BUTTONS.some(item => item.id === view))')
    expect(source).toContain("visibleViews.filter(item => item.id !== 'dashboard').map")
    expect(source.indexOf("id: 'history', label: 'Workshop'")).toBeGreaterThan(source.indexOf("id: 'browser', label: 'Browser'"))
    expect(source).toContain('showAll')
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
})
