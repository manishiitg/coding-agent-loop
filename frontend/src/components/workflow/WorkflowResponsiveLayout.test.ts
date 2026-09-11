import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('workflow responsive pane contract', () => {
  it('keeps the shared toolbar above a single focused pane on narrow screens', () => {
    const layout = readFileSync('src/components/workflow/WorkflowLayout.tsx', 'utf8')

    expect(layout).toContain('grid grid-cols-1 grid-rows-[auto_minmax(0,1fr)]')
    expect(layout).toContain('sharedToolbar={showChatArea && workspacePaneVisible}')
    expect(layout).toContain("workspacePaneVisible && focusedPane === 'preview'")
    expect(layout).toContain("focusedPane === 'chat' ? 'hidden md:block' : ''")
    expect(layout).toContain('col-start-1 row-start-2 min-h-0')
  })

  it('uses the persistent toolbar controls to switch the focused narrow pane', () => {
    const tabs = readFileSync('src/components/workflow/WorkflowChatTabs.tsx', 'utf8')
    const store = readFileSync('src/stores/useWorkflowStore.ts', 'utf8')

    expect(tabs).toContain("setFocusedPane('chat')")
    expect(store).toContain("get().setFocusedPane('preview')")
  })
})
