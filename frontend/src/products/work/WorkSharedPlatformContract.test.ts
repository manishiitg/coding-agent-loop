import { readFileSync, readdirSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const read = (path: string) => readFileSync(path, 'utf8')

describe('Crew shared AgentWorks platform contract', () => {
  it('composes the shared conversation and workspace primitives', () => {
    const surface = read('src/products/work/WorkSurface.tsx')
    const pane = read('src/products/work/WorkWorkspacePane.tsx')

    for (const sharedConversationPrimitive of [
      "../../components/ChatArea",
      "../../components/workspace/WorkspaceSplitDivider",
      "../../platform/ui-control/useWorkspaceUIControl",
    ]) {
      expect(surface).toContain(sharedConversationPrimitive)
    }

    for (const sharedWorkspacePrimitive of [
      "../../components/FileWorkspacePane",
      "../../components/automation/AutomationHubPanel",
      "../../components/workflow/BrowserWorkspacePanel",
      "../../components/workflow/DatabaseView",
      "../../components/workflow/ReportViewer",
      "../../components/workflow/CostsPopup",
      "../../components/workflow/WorkflowBotsPanel",
      "../../components/skills/SkillsManagerPanel",
      "../../components/secrets/SecretSelectionSection",
      "../../components/connectors/ConnectorsBrowser",
    ]) {
      expect(pane).toContain(sharedWorkspacePrimitive)
    }
  })

  it('keeps browser and automation behavior in components shared with AgentWorks', () => {
    const workflowLayout = read('src/components/workflow/WorkflowLayout.tsx')
    const workflowCapabilities = read('src/components/workflow/WorkflowCapabilitiesPanel.tsx')
    const workPane = read('src/products/work/WorkWorkspacePane.tsx')

    expect(workflowLayout).toContain("../automation/AutomationHubPanel")
    expect(workPane).toContain("../../components/automation/AutomationHubPanel")
    expect(workflowCapabilities).toContain("./BrowserWorkspacePanel")
    expect(workPane).toContain("../../components/workflow/BrowserWorkspacePanel")
  })

  it('does not add product-local replacements for shared platform surfaces', () => {
    const files = readdirSync('src/products/work')
    const forbiddenProductForks = [
      'WorkChatArea.tsx',
      'WorkChatInput.tsx',
      'WorkTranscript.tsx',
      'WorkFileWorkspacePane.tsx',
      'WorkTerminal.tsx',
      'WorkBrowserWorkspacePanel.tsx',
      'WorkDatabaseView.tsx',
      'WorkDashboard.tsx',
      'WorkAutomationHubPanel.tsx',
      'WorkCostsPanel.tsx',
      'WorkBotsPanel.tsx',
      'WorkSkillsManager.tsx',
      'WorkSecretsPanel.tsx',
      'WorkMCPBrowser.tsx',
    ]

    expect(files.filter(file => forbiddenProductForks.includes(file))).toEqual([])
  })
})
