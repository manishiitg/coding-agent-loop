import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
vi.mock('../../services/api', () => ({ agentApi: {}, getApiBaseUrl: () => 'http://127.0.0.1:99999' }))
vi.mock('../../api/playbooks', () => ({ playbooksApi: { list: vi.fn(), listInstalled: vi.fn() } }))
vi.mock('./ReportHumanInputPanel', () => ({ ReportHumanInputPanel: () => <section>Needs your decision</section> }))
import { manualPulseReviewMessage, PulseWorkspace } from './PulseWorkspace'

describe('PulseWorkspace information hierarchy', () => {
  it('leads with Goal Work for the user and keeps platform upkeep one tap away', () => {
    const html = renderToStaticMarkup(<PulseWorkspace workspacePath="Workflow/example"
      moduleStates={[{ workspace_path: 'Workflow/example', module: 'plan_drift_review', last_pulse_run_id: 'pulse-1', last_gate_decision: 'due', last_reason: 'Changed steps need checking.' }]}
      planDriftDue planDriftDueItems={[{ step_id: 'prepare-booking', step_type: 'regular', reason: 'The plan changed or a prior drift check remains unresolved.' }]}
      finalCommandStates={[]} reviewFocuses={[]} reviewFocusSelections={[]} statusError={null}
      goalWork={[
        { id: 'GW-1', kind: 'goal_work', title: 'Nobody replies to commenters', status: 'done', action_taken: 'Drafted replies for 12 commenters', links: ['pulse/work/2026-09-23/replies.md'], metric: 'followers', expected_direction: 'increase', check_at: '2026-09-30', created_at: '2026-09-23', updated_at: '2026-09-23' },
        { id: 'GW-2', kind: 'goal_work', title: 'Try a weekly carousel', status: 'idea', links: [], created_at: '2026-09-23', updated_at: '2026-09-23' },
        { id: 'GW-3', kind: 'constraint_challenge', title: 'Longer posts may grow followers', status: 'needs_user', decision_id: 'goal-work-length', constraint_text: 'Posts remain 150-350 words', constraint_class: 'choice', links: [], created_at: '2026-09-23', updated_at: '2026-09-23' },
      ]}
      autonomyRun="auto" focusAreas={['Find more audience strategies like SaaS Builder']} onSaveFocusAreas={async () => true} />)
    expect(html).toContain('For you')
    expect(html).toContain('Platform health')
    expect(html.indexOf('Progress toward goals')).toBeLessThan(html.indexOf('Needs your decision'))
    expect(html.indexOf('Needs your decision')).toBeLessThan(html.indexOf('Did for you'))
    expect(html.indexOf('Did for you')).toBeLessThan(html.indexOf('Challenging your rules'))
    expect(html.indexOf('Challenging your rules')).toBeLessThan(html.indexOf('Next up'))
    expect(html.indexOf('Next up')).toBeLessThan(html.indexOf('Pulse permissions'))
    expect(html).toContain('Drafted replies for 12 commenters')
    expect(html).toContain('replies.md')
    expect(html).toContain('Waiting to see the effect')
    expect(html).toContain('Try a weekly carousel')
    expect(html).toContain('Posts remain 150-350 words')
    expect(html).toContain('Choice: open to a test')
    expect(html).toContain('Focus areas')
    expect(html).toContain('Find more audience strategies like SaaS Builder')
    expect(html.indexOf('Focus areas')).toBeLessThan(html.indexOf('Did for you'))
    expect(html).toContain('Run Goal Work now')
    expect(html).toContain('Run on its own (within your rules)')
    expect(html).toContain('Always asks you')
    expect(html).toContain('a Plan Drift check is due')
    // Platform upkeep and the retired improvement ledger are not on the user's view.
    expect(html).not.toContain('Maintenance issues')
    expect(html).not.toContain('Strategic proposals')
    expect(html).not.toContain('Platform improvements')
    expect(html).not.toContain('Run automatically')
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
