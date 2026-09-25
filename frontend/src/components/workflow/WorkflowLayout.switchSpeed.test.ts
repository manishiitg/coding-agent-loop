import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

// The workflow pane hides the chat behind "Loading conversation..." until the
// reconnect settles. Measured on a local stack, a switch back to an
// already-open workflow spent ~600ms there with its transcript in memory: a
// fixed 500ms delay plus server reads issued one after another.
describe('workflow switch latency', () => {
  const layout = readFileSync('src/components/workflow/WorkflowLayout.tsx', 'utf8')

  it('starts the reconnect without a settle delay', () => {
    expect(layout).toContain('setTimeout(reconnectWorkflowTabs, 0)')
    expect(layout).not.toMatch(/setTimeout\(reconnectWorkflowTabs, [1-9]/)
  })

  it('does not hide a transcript that is already in memory', () => {
    expect(layout).toMatch(/isWorkflowConversationResolving = Boolean\([\s\S]*?!activeTabHasCachedWorkflowConversation,?\s*\)/)
  })

  it('issues the reconnect reads together', () => {
    const reconnect = layout.slice(layout.indexOf('const reconnectWorkflowTabs'), layout.indexOf('setTimeout(reconnectWorkflowTabs'))
    const runningRead = reconnect.indexOf('agentApi.listRunningWorkflows()')
    const historyRead = reconnect.indexOf("agentApi.listChatHistorySessions(5, 0, workspacePath, 'chat')")
    const activeRead = reconnect.indexOf('await useChatStore.getState().getActiveSessions()')
    expect(runningRead).toBeGreaterThan(-1)
    expect(historyRead).toBeGreaterThan(-1)
    expect(runningRead).toBeLessThan(activeRead)
    expect(historyRead).toBeLessThan(activeRead)
  })

  it('restores persisted chats without a fixed wait or a one-by-one loop', () => {
    const chatArea = readFileSync('src/components/ChatArea.tsx', 'utf8')
    const restore = chatArea.slice(chatArea.indexOf('const restoreAll = async'), chatArea.indexOf('restoreAll()'))
    expect(restore).not.toContain('setTimeout(resolve, 500)')
    expect(restore).not.toMatch(/for \(const tab of tabsToHydrate\)[\s\S]*?await restoreSession/)
  })
})
