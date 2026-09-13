import { describe, expect, it } from 'vitest'
import type { InstalledPlaybook } from '../../services/api-types'
import type { CustomPreset } from '../../types/preset'
import type { PlaybookCatalogItem } from './playbookCatalog'
import { buildPlaybookCoverage } from './playbookCoverage'

const playbook = (id: string, version = '1.1.0'): PlaybookCatalogItem => ({
  id, title: id, description: `${id} description`, version, category: 'Browser QA', order: 1, inputCount: 1, toolCount: 1,
})

const installation = (id: string, version: string, status: InstalledPlaybook['status']): InstalledPlaybook => ({
  id, title: id, version, category: 'Browser QA', skill_name: `agentworks-playbook-${id}`, source_hash: 'test', status, installed_at: '2026-09-13',
})

const preset: CustomPreset = { id: 'workflow-1', label: 'Checkout QA', createdAt: 1, selectedFolder: { filepath: 'Workflow/checkout' } }

describe('Engineering operations playbook coverage', () => {
  it('distinguishes active, update, setup, and unused capabilities across workflows', () => {
    const catalog = [playbook('active'), playbook('update'), playbook('draft'), playbook('unused')]
    const rows = buildPlaybookCoverage(catalog, [{ preset, workspacePath: 'Workflow/checkout', installed: [
      installation('active', '1.1.0', 'ready'), installation('update', '1.0.0', 'ready'), installation('draft', '1.1.0', 'draft'),
    ] }])
    expect(rows.map(row => [row.id, row.state, row.uses.length])).toEqual([
      ['active', 'active', 1], ['update', 'update_available', 1], ['draft', 'draft', 1], ['unused', 'not_used', 0],
    ])
    expect(rows[1].uses[0].preset.label).toBe('Checkout QA')
  })
})
