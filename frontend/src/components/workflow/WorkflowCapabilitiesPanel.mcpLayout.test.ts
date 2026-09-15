import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('Workflow MCP panel layout', () => {
  it('uses the complete panel as its scroll surface and has no Save footer', () => {
    const panel = readFileSync('src/components/workflow/WorkflowCapabilitiesPanel.tsx', 'utf8')
    const connectors = readFileSync('src/components/connectors/ConnectorsBrowser.tsx', 'utf8')

    expect(panel).toContain("section === 'mcp' ? 'overflow-y-auto' : ''")
    expect(panel).toContain('manageOwnScroll={false}')
    expect(connectors).toContain("manageOwnScroll ? 'min-h-0 flex-1 overflow-y-auto pt-5' : 'pt-5'")
    expect(panel).toMatch(/mcp:\s*\{[\s\S]*?savesViaManifest: false,/)
  })

  it('persists MCP server and tool selections immediately', () => {
    const source = readFileSync('src/components/workflow/WorkflowCapabilitiesPanel.tsx', 'utf8')

    expect(source).toMatch(/onServerChange=\{\(selected_servers\)[\s\S]*?void persist\(next\)/)
    expect(source).toMatch(/onToolChange=\{\(selected_tools\)[\s\S]*?void persist\(next\)/)
  })

  it('places Refresh and Ask AI in capability headers without rendering a second workspace title bar', () => {
    const panel = readFileSync('src/components/workflow/WorkflowCapabilitiesPanel.tsx', 'utf8')
    const host = readFileSync('src/components/workflow/canvas/WorkspaceViewHost.tsx', 'utf8')

    expect(panel).toContain('<WorkspaceViewActions')
    expect(panel).toContain('message={getWorkspaceAskAIMessage(section)}')
    expect(panel).toContain("message={getWorkspaceAskAIMessage('browser')}")
    expect(panel).toContain('refreshLabel="Refresh Browser"')
    expect(host).not.toContain('data-ui-view-assistant')
    for (const view of ['costs', 'execution-logs', 'knowledgebase', 'database', 'evaluation', 'schedules', 'pulse', 'backup', 'publish', 'notify']) {
      expect(host).toContain(`headerAction={askAI('${view}')}`)
    }
    for (const view of ['learnings', 'folders', 'access']) {
      expect(host).toContain(`headerAction={refreshAndAskAI('${view}')}`)
    }
    expect(host).toContain("getWorkspaceAskAIMessage('report')")
    expect(host).toContain("getWorkspaceAskAIMessage('flow')")
    expect(host).toContain("getWorkspaceAskAIMessage('files')")
  })
})
