import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('Workflow MCP panel layout', () => {
  it('scrolls the content below a fixed header and has no Save footer', () => {
    const panel = readFileSync('src/components/workflow/WorkflowCapabilitiesPanel.tsx', 'utf8')
    const connectors = readFileSync('src/components/connectors/ConnectorsBrowser.tsx', 'utf8')

    // The header stays fixed (Pulse structure): the section itself never
    // scrolls; the tab content owns the scroll surface instead.
    const section = panel.split('\n').find(line => line.includes('<section className='))
    expect(section).toBeDefined()
    expect(section).not.toContain('overflow-y-auto')
    expect(panel).toContain("(section === 'mcp' || section === 'identity') ? 'min-h-0 flex-1 overflow-y-auto'")
    expect(panel).toContain('manageOwnScroll={false}')
    expect(connectors).toContain("manageOwnScroll ? 'min-h-0 flex-1 overflow-y-auto pt-5' : 'pt-5'")
    expect(panel).toMatch(/mcp:\s*\{[\s\S]*?savesViaManifest: false,/)
  })

  it('persists MCP server and tool selections immediately', () => {
    const source = readFileSync('src/components/workflow/WorkflowCapabilitiesPanel.tsx', 'utf8')

    expect(source).toMatch(/onServerChange=\{\(selected_servers\)[\s\S]*?void persist\(next\)/)
    expect(source).toMatch(/onToolChange=\{\(selected_tools\)[\s\S]*?void persist\(next\)/)
  })

  it('shows an Apps checklist per group whenever it has servers, with no empty-state line', () => {
    const panel = readFileSync('src/components/workflow/WorkflowCapabilitiesPanel.tsx', 'utf8')
    const selection = readFileSync('src/components/ToolSelectionSection.tsx', 'utf8')

    expect(panel).toMatch(/selectedAvailableServers\.length > 0 &&[\s\S]*?<ToolSelectionSection/)
    expect(panel).toMatch(/unselectedAvailableServers\.length > 0 &&[\s\S]*?<ToolSelectionSection/)
    expect(selection).not.toContain('No MCP servers selected yet')
  })

  it('places Refresh and Ask AI in capability headers without rendering a second workspace title bar', () => {
    const panel = readFileSync('src/components/workflow/WorkflowCapabilitiesPanel.tsx', 'utf8')
    const host = readFileSync('src/components/workflow/canvas/WorkspaceViewHost.tsx', 'utf8')

    expect(panel).toContain('<WorkspaceViewActions')
    expect(panel).toContain('getIntegrationTabAskAIMessage(tab)')
    expect(panel).toContain('getWorkspaceAskAIMessage(section)')
    expect(panel).toContain("message={getWorkspaceAskAIMessage('browser')}")
    expect(panel).toContain('refreshLabel="Refresh Browser"')
    expect(host).not.toContain('data-ui-view-assistant')
    for (const view of ['costs', 'execution-logs', 'schedules', 'pulse', 'backup', 'publish', 'notify']) {
      expect(host).toContain(`headerAction={askAI('${view}')}`)
    }
    for (const view of ['access']) {
      expect(host).toContain(`headerAction={refreshAndAskAI('${view}')}`)
    }
    // Folders moved under the Identity tabs; it no longer has a host header.
    expect(host).not.toContain("headerAction={refreshAndAskAI('folders')}")
    // Knowledge is a self-served umbrella (like capabilities sections): the
    // host mounts it without header actions; its Ask AI follows the active tab.
    expect(host).toContain("case 'knowledge':")
    expect(host).toContain('<KnowledgeView workspacePath={workspacePath} plan={plan} />')
    expect(host).toContain("getWorkspaceAskAIMessage('report')")
    expect(host).toContain("getWorkspaceAskAIMessage('flow')")
    expect(host).toContain("getWorkspaceAskAIMessage('files')")
  })

  it('embeds workflow skills inside the Integrations section instead of a standalone view', () => {
    const panel = readFileSync('src/components/workflow/WorkflowCapabilitiesPanel.tsx', 'utf8')
    const views = readFileSync('src/components/workflow/workspaceViews.ts', 'utf8')
    const host = readFileSync('src/components/workflow/canvas/WorkspaceViewHost.tsx', 'utf8')
    const skillsPanel = readFileSync('src/components/skills/SkillsManagerPanel.tsx', 'utf8')

    expect(panel).toMatch(/section === 'mcp'[\s\S]*?<SkillsManagerPanel/)
    expect(panel).not.toContain("section === 'skills'")
    expect(views).not.toMatch(/id: 'skills'/)
    expect(host).not.toContain("case 'skills':")
    expect(skillsPanel).toContain('manageOwnScroll')
    // One refresh (the header remounts the tab) plus an Ask AI install button.
    expect(skillsPanel).toContain('hideRefresh')
    expect(panel).toContain('hideRefresh')
    expect(panel).toContain('Install a skill')
  })

  it('splits each tab into This workflow and Platform connected groups', () => {
    const panel = readFileSync('src/components/workflow/WorkflowCapabilitiesPanel.tsx', 'utf8')
    const selection = readFileSync('src/components/ToolSelectionSection.tsx', 'utf8')
    const skillsPanel = readFileSync('src/components/skills/SkillsManagerPanel.tsx', 'utf8')
    const connectors = readFileSync('src/components/connectors/ConnectorsBrowser.tsx', 'utf8')

    // No chips strip: each tab shows workflow picks first, then the platform shelf.
    expect(panel).not.toContain('For this workflow')
    expect(panel).toContain('This workflow')
    expect(panel).toContain('Platform connected')
    expect(panel).toContain('selectedAvailableServers')
    expect(panel).toContain('unselectedAvailableServers')
    expect(panel).toContain('splitSelectionGroups')
    expect(skillsPanel).toContain('splitSelectionGroups')
    expect(panel).toContain('Search apps')
    expect(panel).toContain('Search skills')
    expect(panel).toContain('query={searchQuery}')
    expect(panel).toMatch(/onToggleSkill=\{\(folderName\)[\s\S]*?void persist\(next\)/)
    // The directory's Connected shelf would duplicate the Apps checklist.
    expect(panel).toContain('hideConnectedSection')
    // No selected-only checklist anymore: every connected server ticks inline.
    expect(selection).not.toContain('showSelectedOnly')
    expect(selection).toContain('manageOwnScroll')
    expect(skillsPanel).toContain('hideSearch')
    expect(skillsPanel).toContain('hideSelectionChips')
    expect(connectors).toContain('hideSearch')
    expect(connectors).toContain('hideConnectedSection')
  })

  it('separates apps, skills, slack, whatsapp, and gmail into tabs with apps first', () => {
    const panel = readFileSync('src/components/workflow/WorkflowCapabilitiesPanel.tsx', 'utf8')

    expect(panel).toContain("useState<McpTab>('apps')")
    expect(panel).toContain("{ value: 'apps', label: 'MCPs' }")
    expect(panel).toMatch(/MCP_TABS[^=]*=[\s\S]*?'apps'[\s\S]*?'skills'[\s\S]*?'slack'[\s\S]*?'whatsapp'[\s\S]*?'gmail'/)
    expect(panel).toContain('tabs={section ===')
    expect(panel).toContain('options: MCP_TABS')
    expect(panel).toContain("ariaLabel: 'Integrations'")
    expect(panel).toContain('fixedChannel="slack"')
    expect(panel).toContain('fixedChannel="whatsapp"')
  })

  it('embeds bots and gmail inside the Integrations tabs instead of standalone views', () => {
    const panel = readFileSync('src/components/workflow/WorkflowCapabilitiesPanel.tsx', 'utf8')
    const views = readFileSync('src/components/workflow/workspaceViews.ts', 'utf8')
    const host = readFileSync('src/components/workflow/canvas/WorkspaceViewHost.tsx', 'utf8')

    expect(panel).toMatch(/section === 'mcp'[\s\S]*?<WorkflowBotsPanel/)
    expect(panel).toMatch(/section === 'mcp'[\s\S]*?<WorkflowEmailPanel/)
    expect(panel).not.toContain("section === 'bots'")
    expect(panel).not.toContain("section === 'email'")
    expect(views).not.toMatch(/id: 'bots'/)
    expect(views).not.toMatch(/id: 'email'/)
    expect(host).not.toContain("case 'bots':")
    expect(host).not.toContain("case 'email':")
  })

  it('explains the attached-apps checklist and has no count footer', () => {
    const panel = readFileSync('src/components/workflow/WorkflowCapabilitiesPanel.tsx', 'utf8')
    const selection = readFileSync('src/components/ToolSelectionSection.tsx', 'utf8')

    expect(panel).toContain('Tick one to let this workflow use it')
    expect(selection).not.toContain('Selected:')
  })

  it('routes skill adds through builder chat instead of toggling directly', () => {
    const panel = readFileSync('src/components/workflow/WorkflowCapabilitiesPanel.tsx', 'utf8')
    const manager = readFileSync('src/components/skills/SkillsManagerPanel.tsx', 'utf8')
    const row = readFileSync('src/components/skills/SkillRow.tsx', 'utf8')

    expect(panel).toContain('sendWorkspacePaneMessageToChat')
    expect(panel).toContain('onAddViaChat')
    expect(manager).toContain('onAddViaChat')
    expect(row).toContain('onRequestAdd')
    expect(row).toContain('MessageCircle')
    expect(row).toContain('<Check')
  })

  it('shares one builder banner above the tabs covering apps and skills', () => {
    const panel = readFileSync('src/components/workflow/WorkflowCapabilitiesPanel.tsx', 'utf8')
    const connectors = readFileSync('src/components/connectors/ConnectorsBrowser.tsx', 'utf8')

    expect(panel).toContain("Can't find what you need?")
    expect(panel).toContain('Ask builder to help')
    expect(panel).toMatch(/Can't find what you need\?[\s\S]*?Platform connected/)
    expect(panel).toContain('hideBanner')
    expect(connectors).toContain('hideBanner')
  })
})
