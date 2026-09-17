import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
vi.mock('../../services/api', () => ({ agentApi: {}, getApiBaseUrl: () => 'http://127.0.0.1:99999' }))
vi.mock('../../api/playbooks', () => ({ playbooksApi: { list: vi.fn(), listInstalled: vi.fn() } }))
vi.mock('./ReportHumanInputPanel', () => ({ ReportHumanInputPanel: () => <section>Needs your decision</section> }))
import { manualPulseReviewMessage, PulseWorkspace } from './PulseWorkspace'

describe('PulseWorkspace information hierarchy', () => {
  it('shows drift status and review coverage before findings', () => {
    const html = renderToStaticMarkup(<PulseWorkspace workspacePath="Workflow/example"
      moduleStates={[{ workspace_path: 'Workflow/example', module: 'plan_drift_review', last_pulse_run_id: 'pulse-1', last_gate_decision: 'due', last_reason: 'Changed steps need checking.' }]}
      planDriftDue planDriftDueItems={[{ step_id: 'prepare-booking', step_type: 'regular', reason: 'The plan changed or a prior drift check remains unresolved.' }]}
      finalCommandStates={[]} reviewFocuses={[]} reviewFocusSelections={[{
        workspace_path: 'Workflow/example', module: 'technical_review', focus_key: 'store_integrity', route_scope: 'workflow/learnings',
        last_reviewed_at: '2026-08-31T10:16:48Z', last_pulse_run_id: 'pulse-learning', last_verdict: 'Consolidated learnings and verified references.', updated_at: '2026-08-31T10:16:48Z',
      }]} statusError={null} />)
    expect(html.indexOf('Progress toward goals')).toBeLessThan(html.indexOf('Goals, metrics &amp; strategy'))
    expect(html.indexOf('Goals, metrics &amp; strategy')).toBeLessThan(html.indexOf('Strategic proposals'))
    expect(html.indexOf('Strategic proposals')).toBeLessThan(html.indexOf('Platform health &amp; stability'))
    expect(html.indexOf('Platform health &amp; stability')).toBeLessThan(html.indexOf('Needs your decision'))
    expect(html.indexOf('Needs your decision')).toBeLessThan(html.indexOf('Issues and follow-through'))
    expect(html.indexOf('Goals, metrics &amp; strategy')).toBeLessThan(html.indexOf('Platform health &amp; stability'))
    expect(html.indexOf('Platform health &amp; stability')).toBeLessThan(html.indexOf('Issues and follow-through'))
    expect(html).toContain('Drift check')
    expect(html).toContain('Plan Drift is due')
    expect(html).toContain('1 step needs compatibility review before Technical, Architecture, or Strategy can run.')
    expect(html).toContain('prepare-booking')
    expect(html.match(/Plan Drift is due/g)).toHaveLength(1)
    expect(html.match(/Waiting for Plan Drift/g)).toHaveLength(4)
    expect(html).not.toContain('Plan drift review')
    expect(html).not.toContain('Gate decision:')
    expect(html).not.toContain('Selected this run:')
    expect(html).not.toContain('Finalization')
    expect(html).not.toContain('Dashboard, backup, publish, and notification outcomes')
    for (const label of ['Technical', 'Architecture', 'Strategy', 'Goals, metrics', 'Platform health']) expect(html).toContain(label)
    expect(html).not.toContain('Review notes and reports')
    expect(html).not.toContain('Read report')
    expect(html).not.toContain('Latest check')
    expect(html).not.toContain('View review history')
    expect(html).toContain('Run automatically')
    expect(html).toContain('Run now')
  })

  it('routes every manual review through its intended guided command', () => {
    expect(manualPulseReviewMessage('technical_review')).toContain('kind="engineering-review"')
    expect(manualPulseReviewMessage('architecture_review', 'Workflow/example')).toContain('workspace_path="Workflow/example"')
    expect(manualPulseReviewMessage('architecture_review', 'Workflow/example')).toContain('references/architecture-review.md')
    expect(manualPulseReviewMessage('strategic_review')).toContain('kind="strategy-auditor"')
    expect(manualPulseReviewMessage('plan_drift_review')).toContain('kind="review-artifact-drift"')
    expect(() => manualPulseReviewMessage('unknown')).toThrow('Unsupported Pulse review module')
  })
})
