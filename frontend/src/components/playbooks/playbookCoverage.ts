import type { InstalledPlaybook } from '../../services/api-types'
import type { CustomPreset, PredefinedPreset } from '../../types/preset'
import { isNewerPlaybookVersion, type PlaybookCatalogItem } from './playbookCatalog'

export type WorkflowPlaybookInstallation = {
  preset: CustomPreset | PredefinedPreset
  workspacePath: string
  installed: InstalledPlaybook[]
}

export type PlaybookCoverageRow = PlaybookCatalogItem & {
  uses: Array<WorkflowPlaybookInstallation & { installation: InstalledPlaybook }>
  state: 'not_used' | 'active' | 'draft' | 'update_available'
}

export function buildPlaybookCoverage(catalog: PlaybookCatalogItem[], workflows: WorkflowPlaybookInstallation[]): PlaybookCoverageRow[] {
  return catalog.map(playbook => {
    const uses = workflows.flatMap(workflow => {
      const installation = workflow.installed.find(item => item.id === playbook.id && item.status !== 'disabled')
      return installation ? [{ ...workflow, installation }] : []
    })
    const state: PlaybookCoverageRow['state'] = uses.some(use => isNewerPlaybookVersion(playbook.version, use.installation.version))
      ? 'update_available'
      : uses.some(use => use.installation.status !== 'ready')
        ? 'draft'
        : uses.length > 0 ? 'active' : 'not_used'
    return { ...playbook, uses, state }
  })
}
