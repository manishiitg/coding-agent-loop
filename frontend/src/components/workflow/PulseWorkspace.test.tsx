import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
vi.mock('../../services/api', () => ({ agentApi: {}, getApiBaseUrl: () => 'http://127.0.0.1:99999' }))
import { PulseWorkspace } from './PulseWorkspace'

describe('PulseWorkspace information hierarchy', () => {
  it('shows drift status and review coverage before findings', () => {
    const html = renderToStaticMarkup(<PulseWorkspace workspacePath="Workflow/example"
      moduleStates={[{ workspace_path: 'Workflow/example', module: 'plan_drift_review', last_pulse_run_id: 'pulse-1', last_gate_decision: 'due', last_reason: 'Changed steps need checking.' }]}
      finalCommandStates={[]} reviewFocuses={[]} reviewFocusSelections={[{
        workspace_path: 'Workflow/example', module: 'technical_review', focus_key: 'store_integrity', route_scope: 'workflow/learnings',
        last_reviewed_at: '2026-08-31T10:16:48Z', last_pulse_run_id: 'pulse-learning', last_verdict: 'Consolidated learnings and verified references.', updated_at: '2026-08-31T10:16:48Z',
      }]} statusError={null} />)
    expect(html.indexOf('Work areas')).toBeLessThan(html.indexOf('Issues and follow-through'))
    expect(html).toContain('Drift check')
    expect(html).toContain('Pending check')
    expect(html).not.toContain('Plan drift review')
    expect(html).not.toContain('Gate decision:')
    expect(html).not.toContain('Selected this run:')
    for (const label of ['Health', 'Strategy', 'Learnings', 'Knowledge base', 'Report accuracy', 'Models, cost and efficiency', 'No review results recorded yet', 'Review reports']) expect(html).toContain(label)
    expect(html).toContain('Last reviewed Aug 31, 2026')
    expect(html).toContain('Consolidated learnings and verified references.')
    expect(html).toContain('No specific review recorded')
  })
})
