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
    expect(PLAYBOOK_CATALOG).toHaveLength(43)
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

  it('exposes invoice intake with a distinct payables owner and validated document handoff', () => {
    const invoice = PLAYBOOK_CATALOG.find(item => item.id === 'invoice-intake-to-reviewed-payable')
    expect(invoice?.category).toBe('Finance')
    expect(invoice?.agentSlots?.map(slot => slot.agent_playbook_id)).toEqual([
      'document-intake-assistant', 'spend-payables-coordinator',
    ])
    expect(invoice?.handoffs?.[0].artifact_type).toBe('document-intake-record/v1')
    expect(invoice?.setupChecks).toContain('duplicate_policy')
    expect(invoice?.setupChecks).toContain('test_run')
  })

  it('exposes refund review with a distinct close owner and validated decision handoff', () => {
    const refund = PLAYBOOK_CATALOG.find(item => item.id === 'refund-request-to-reconciled-outcome')
    expect(refund?.category).toBe('Finance')
    expect(refund?.agentSlots?.map(slot => slot.agent_playbook_id)).toEqual([
      'billing-operations-coordinator', 'revenue-close-analyst',
    ])
    expect(refund?.handoffs?.[0].artifact_type).toBe('refund-decision/v1')
    expect(refund?.setupChecks).toContain('action_ledger')
    expect(refund?.setupChecks).toContain('test_run')
  })

  it('exposes campaign measurement to a reviewed experiment with optional competitor context', () => {
    const marketing = PLAYBOOK_CATALOG.find(item => item.id === 'campaign-signal-to-reviewed-experiment')
    expect(marketing?.category).toBe('Marketing')
    expect(marketing?.agentSlots?.filter(slot => slot.required).map(slot => slot.agent_playbook_id)).toEqual([
      'campaign-performance-analyst', 'growth-experiment-planner',
    ])
    expect(marketing?.agentSlots?.find(slot => slot.id === 'competitor')?.required).toBe(false)
    expect(marketing?.handoffs?.find(handoff => handoff.required)?.artifact_type).toBe('campaign-performance-brief/v1')
    expect(marketing?.setupChecks).toContain('baseline_rule')
  })

  it('exposes five Shopify routes with distinct two-Crew handoffs', () => {
    const shopify = PLAYBOOK_CATALOG.filter(item => item.category === 'Shopify')
    expect(shopify.map(item => item.id)).toEqual(['order-exception-to-resolution', 'storefront-opportunity-to-verified-change', 'inventory-availability-to-owner-action', 'payment-exception-to-order-decision', 'product-launch-readiness-to-go-no-go'])
    expect(shopify[0].version).toBe('0.1.1')
    expect(shopify[1].agentSlots?.map(slot => slot.agent_playbook_id)).toEqual(['shopify-growth-analyst', 'catalog-merchandising-analyst'])
    expect(shopify[1].handoffs?.[0].artifact_type).toBe('shopify-growth-opportunity/v1')
    expect(shopify[2].handoffs?.[0].artifact_type).toBe('inventory-availability-exception/v1')
    expect(shopify[3].agentSlots?.[0].agent_playbook_id).toBe('payment-operations-investigator')
    expect(shopify[4].handoffs?.[0].artifact_type).toBe('launch-catalog-readiness/v1')
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

  it('exposes a distinct Sales proposal route with a required call brief handoff', () => {
    const proposal = PLAYBOOK_CATALOG.find(item => item.id === 'discovery-to-reviewed-proposal')
    expect(proposal?.agentSlots?.filter(slot => slot.required).map(slot => slot.agent_playbook_id)).toEqual([
      'sales-call-briefing', 'proposal-drafter',
    ])
    expect(proposal?.handoffs?.find(handoff => handoff.required)?.artifact_type).toBe('sales-call-brief/v1')
    expect(proposal?.setupChecks).toContain('pricing_policy')
    expect(proposal?.setupChecks).toContain('test_run')
  })

  it('exposes a Support-to-Product feedback decision with a validated theme handoff', () => {
    const feedback = PLAYBOOK_CATALOG.find(item => item.id === 'feedback-theme-to-product-decision')
    expect(feedback?.category).toBe('Product')
    expect(feedback?.agentSlots?.map(slot => slot.agent_playbook_id)).toEqual([
      'feedback-review-analyst', 'product-feedback-coordinator',
    ])
    expect(feedback?.handoffs?.[0].artifact_type).toBe('feedback-theme-brief/v1')
    expect(feedback?.setupChecks).toContain('action_ledger')
  })

  it('exposes the GTM launch route and reuses the Sales qualification handoff', () => {
    const gtm = PLAYBOOK_CATALOG.find(item => item.id === 'launch-to-qualified-pipeline')
    expect(gtm?.category).toBe('GTM')
    expect(gtm?.agentSlots?.filter(slot => slot.required).map(slot => slot.agent_playbook_id)).toEqual([
      'gtm-strategy-analyst', 'launch-coordinator', 'lead-intake-qualifier', 'sales-followup-coordinator',
    ])
    expect(gtm?.agentSlots?.find(slot => slot.id === 'website')?.required).toBe(false)
    expect(gtm?.handoffs?.map(handoff => handoff.artifact_type)).toEqual([
      'gtm-launch-brief/v1', 'launch-signal-register/v1', 'lead-qualification-brief/v1',
    ])
    expect(gtm?.setupChecks).toContain('test_run')
  })

  it('exposes Customer Success with onboarding and adoption handoffs', () => {
    const success = PLAYBOOK_CATALOG.find(item => item.id === 'new-customer-to-first-value')
    expect(success?.category).toBe('Customer Success')
    expect(success?.agentSlots?.filter(slot => slot.required).map(slot => slot.agent_playbook_id)).toEqual(['customer-onboarding-coordinator', 'product-adoption-analyst'])
    expect(success?.agentSlots?.find(slot => slot.id === 'health')?.required).toBe(false)
    expect(success?.handoffs?.find(handoff => handoff.required)?.artifact_type).toBe('onboarding-milestone-register/v1')
    expect(success?.setupChecks).toContain('first_value_rule')
  })

  it('exposes the Customer Support case route with optional escalation', () => {
    const support = PLAYBOOK_CATALOG.find(item => item.id === 'support-case-to-reviewed-resolution')
    expect(support?.category).toBe('Customer Support')
    expect(support?.agentSlots?.filter(slot => slot.required).map(slot => slot.agent_playbook_id)).toEqual([
      'support-triage-assistant', 'support-reply-drafter',
    ])
    expect(support?.agentSlots?.find(slot => slot.id === 'escalation')?.required).toBe(false)
    expect(support?.handoffs?.find(handoff => handoff.required)?.artifact_type).toBe('support-case-triage/v1')
    expect(support?.setupChecks).toContain('handoff_contract')
  })

  it('exposes QA release gate composition with optional flake investigation', () => {
    const qa = PLAYBOOK_CATALOG.find(item => item.id === 'release-candidate-to-reviewed-gate')
    expect(qa?.category).toBe('QA')
    expect(qa?.agentSlots?.filter(slot => slot.required).map(slot => slot.agent_playbook_id)).toEqual([
      'browser-journey-qa-analyst', 'release-quality-assistant',
    ])
    expect(qa?.agentSlots?.find(slot => slot.id === 'flake')?.required).toBe(false)
    expect(qa?.handoffs?.find(handoff => handoff.required)?.artifact_type).toBe('journey-result/v1')
    expect(qa?.setupChecks).toContain('suite_policy')
  })

  it('exposes the Security finding-to-remediation route', () => {
    const security = PLAYBOOK_CATALOG.find(item => item.id === 'finding-to-verified-remediation')
    expect(security?.category).toBe('Security')
    expect(security?.agentSlots?.map(slot => slot.agent_playbook_id)).toEqual([
      'security-findings-analyst', 'security-remediation-coordinator',
    ])
    expect(security?.handoffs?.[0].artifact_type).toBe('security-finding/v1')
    expect(security?.setupChecks).toContain('remediation_rule')
  })

  it('exposes Operations meeting actions with required status and optional review', () => {
    const operations = PLAYBOOK_CATALOG.find(item => item.id === 'meeting-decision-to-owned-follow-through')
    expect(operations?.category).toBe('Operations')
    expect(operations?.agentSlots?.filter(slot => slot.required).map(slot => slot.agent_playbook_id)).toEqual([
      'meeting-actions-coordinator', 'project-status-reporter',
    ])
    expect(operations?.agentSlots?.find(slot => slot.id === 'review')?.required).toBe(false)
    expect(operations?.handoffs?.find(handoff => handoff.required)?.artifact_type).toBe('meeting-action-register/v1')
    expect(operations?.setupChecks).toContain('owner_policy')
  })
})
