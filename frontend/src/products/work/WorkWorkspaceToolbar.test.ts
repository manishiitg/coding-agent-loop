import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('WorkWorkspaceToolbar', () => {
  it('keeps Views and Setup mutually exclusive and follows the selected panel', () => {
    const source = readFileSync('src/products/work/WorkWorkspacePane.tsx', 'utf8')

    expect(source).toContain("open={openGroup === 'views'}")
    expect(source).toContain('label="Views" hideLabel')
    expect(source).toContain("open={openGroup === 'setup'}")
    expect(source).toContain("onToggle={() => setOpenGroup('views')}")
    expect(source).toContain("onToggle={() => setOpenGroup('setup')}")
    expect(source).toContain("setOpenGroup(SETUP_BUTTONS.some(item => item.id === view) ? 'setup' : 'views')")
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
