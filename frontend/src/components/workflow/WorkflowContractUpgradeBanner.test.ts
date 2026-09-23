import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('workflow contract upgrade banner', () => {
  const source = readFileSync('src/components/workflow/WorkflowLayout.tsx', 'utf8')
  const upgradeMessage = readFileSync('src/components/workflow/workflowContractUpgradeEvents.ts', 'utf8')

  it('shows pending upgrades above chat and starts them manually', () => {
    expect(source).toContain('contractUpgrade?.required')
    expect(source).toContain('Schedules keep running the saved version and will not update it automatically.')
    expect(source).toContain('Update workflow')
    expect(source).toContain('sendWorkspacePaneMessageToChat')
    expect(upgradeMessage).toContain('Use get_contract_upgrades')
  })

  it('does not offer the write action to read-only collaborators', () => {
    expect(source).toContain("activeWorkflowAccess !== 'read'")
    expect(source).toContain('canStartContractUpgrade ?')
  })
})
