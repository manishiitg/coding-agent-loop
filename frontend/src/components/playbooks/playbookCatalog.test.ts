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
    expect(PLAYBOOK_CATALOG).toHaveLength(27)
    expect(PLAYBOOK_CATALOG.every(item => item.teamScope === 'small_team')).toBe(true)
  })

  it('keeps the buyer-question handoff with Search Opportunity Mapper', () => {
    const growth = PLAYBOOK_CATALOG.find(item => item.id === 'website-growth-loop')
    expect(growth?.version).toBe('0.3.0')
    expect(growth?.agentSlots?.find(slot => slot.id === 'search')?.agent_playbook_id).toBe('search-opportunity-mapper')
    expect(growth?.agentSlots?.find(slot => slot.id === 'search')).not.toHaveProperty('accepts')
    expect(growth?.agentSlots?.find(slot => slot.id === 'technical_seo')?.required).toBe(false)
    expect(growth?.handoffs?.some(handoff => handoff.artifact_type === 'shipped-change/v1')).toBe(false)
    expect(growth?.setupChecks).toContain('action_ledger')
  })

  it('exposes the Finance Operations Review team and validated billing handoff', () => {
    const finance = PLAYBOOK_CATALOG.find(item => item.id === 'finance-operations-review')
    expect(finance?.category).toBe('Finance')
    expect(finance?.agentSlots?.filter(slot => slot.required).map(slot => slot.agent_playbook_id)).toEqual(['billing-operations-coordinator', 'finance-analyst'])
    expect(finance?.handoffs?.find(handoff => handoff.required)?.artifact_type).toBe('billing-exception-queue/v1')
    expect(finance?.setupChecks).toContain('test_run')
  })

  it('exposes the Sales route with required qualification and follow-up and optional research', () => {
    const sales = PLAYBOOK_CATALOG.find(item => item.id === 'inbound-lead-to-meeting-review')
    expect(sales?.category).toBe('Sales')
    expect(sales?.version).toBe('0.2.0')
    expect(sales?.agentSlots?.filter(slot => slot.required).map(slot => slot.agent_playbook_id)).toEqual(['lead-intake-qualifier', 'sales-followup-coordinator'])
    expect(sales?.agentSlots?.find(slot => slot.id === 'research')?.required).toBe(false)
    expect(sales?.handoffs?.find(handoff => handoff.required)?.artifact_type).toBe('lead-qualification-brief/v1')
    expect(sales?.setupChecks).toContain('action_ledger')
  })

  it('exposes Customer Success with onboarding and adoption handoffs', () => {
    const success = PLAYBOOK_CATALOG.find(item => item.id === 'new-customer-to-first-value')
    expect(success?.category).toBe('Customer Success')
    expect(success?.agentSlots?.filter(slot => slot.required).map(slot => slot.agent_playbook_id)).toEqual(['customer-onboarding-coordinator', 'product-adoption-analyst'])
    expect(success?.agentSlots?.find(slot => slot.id === 'health')?.required).toBe(false)
    expect(success?.handoffs?.find(handoff => handoff.required)?.artifact_type).toBe('onboarding-milestone-register/v1')
    expect(success?.setupChecks).toContain('first_value_rule')
  })
})
