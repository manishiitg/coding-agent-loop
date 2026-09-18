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
    const layout = readFileSync('src/components/workflow/WorkflowLayout.tsx', 'utf8')
    const tabs = readFileSync('src/components/workflow/WorkflowChatTabs.tsx', 'utf8')
    const store = readFileSync('src/stores/useWorkflowStore.ts', 'utf8')

    expect(tabs).toContain("setFocusedPane('chat')")
    expect(tabs).toContain('window.innerWidth < 768')
    expect(layout).toContain("if (window.innerWidth < 768) setFocusedPane('preview')")
    expect(layout).toContain("if (window.innerWidth < 768) setFocusedPane('chat')")
    expect(store).toContain("get().setFocusedPane('preview')")
  })

  it('keeps one Automation center in the right toolbar and outside the persistent Chat composer', () => {
    const layout = readFileSync('src/components/workflow/WorkflowLayout.tsx', 'utf8')
    const toolbar = readFileSync('src/components/workflow/canvas/WorkflowToolbar.tsx', 'utf8')
    const views = readFileSync('src/components/workflow/workspaceViews.ts', 'utf8')
    const viewHost = readFileSync('src/components/workflow/canvas/WorkspaceViewHost.tsx', 'utf8')
    const tabs = readFileSync('src/components/workflow/WorkflowChatTabs.tsx', 'utf8')
    const chatArea = readFileSync('src/components/ChatArea.tsx', 'utf8')
    const store = readFileSync('src/stores/useWorkflowStore.ts', 'utf8')

    expect(toolbar).toContain("'browser', 'workshop'")
    expect(toolbar).toContain("view.toolbarGroup === 'capabilities' && view.id !== 'bots'")
    expect(views.indexOf("id: 'workshop'")).toBeGreaterThan(views.indexOf("id: 'browser'"))
    expect(views).toContain("id: 'workshop', kind: 'inspector', label: 'Automation'")
    expect(layout).toContain('workshopPanel={workspacePath ? (')
    expect(layout).toContain('<AutomationHubPanel')
    expect(layout).toContain('chatOnly')
    expect(layout).toContain('botContent=')
    expect(viewHost).toContain("effectiveView === 'workshop' ? 'flex overflow-hidden' : ''")
    expect(layout).toContain("listChatHistorySessions(5, 0, workspacePath, 'chat')")
    expect(layout).toContain('workflowLandingContent={<WorkflowNewChatGuide />}')
    expect(layout).toContain('This is your persistent conversation for this workflow.')
    expect(tabs).toContain("displayName={isPersistentChat ? 'Chat'")
    expect(tabs).toContain('canClose={!isPersistentChat')
    expect(chatArea).not.toContain('a message typed there starts a conversation in a NEW Chat tab')
    expect(store).toContain("const resolvedView: WorkspaceViewId = automationTarget ? 'workshop' : view")
  })
})
