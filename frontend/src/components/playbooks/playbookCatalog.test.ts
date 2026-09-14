import { describe, expect, it } from 'vitest'
import { isNewerPlaybookVersion, PLAYBOOK_CATALOG } from './playbookCatalog'

describe('isNewerPlaybookVersion', () => {
  it('compares semantic versions without treating older catalog data as an update', () => {
    expect(isNewerPlaybookVersion('0.6.1', '0.6.0')).toBe(true)
    expect(isNewerPlaybookVersion('1.0.0', '0.9.9')).toBe(true)
    expect(isNewerPlaybookVersion('0.6.0', '0.6.0')).toBe(false)
    expect(isNewerPlaybookVersion('0.5.9', '0.6.0')).toBe(false)
  })
})

describe('small-team catalog', () => {
  it('uses one engineering operations intelligence playbook and scopes every playbook to small teams', () => {
    const intelligence = PLAYBOOK_CATALOG.filter(item => item.category === 'Engineering Operations Intelligence')
    expect(intelligence.map(item => item.id)).toEqual(['engineering-operations-intelligence'])
    expect(PLAYBOOK_CATALOG).toHaveLength(23)
    expect(PLAYBOOK_CATALOG.every(item => item.teamScope === 'small_team')).toBe(true)
  })
})
