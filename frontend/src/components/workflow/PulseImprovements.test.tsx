import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { expect, it } from 'vitest'
import { PulseImprovements } from './PulseImprovements'
import type { PulseImpactLedger } from '../../services/api-types'

it('distinguishes approval, application and measured outcome without inventing impact', () => {
  const impact: PulseImpactLedger = { observations: [], assessments: [], interventions: [
    { intervention_id: 'one', title: 'Simpler prompts', kind: 'architecture_improvement', criterion_id: 'sc-1', metric: 'latency', impact_type: 'reliability', expected_direction: 'decrease', minimum_evidence_runs: 1, status: 'approved' },
  ] }
  let html = renderToStaticMarkup(<PulseImprovements impact={impact} />)
  expect(html).toContain('Approved · awaiting application')
  expect(html).toContain('Outcome not established')
  expect(html).not.toContain('Applied ·')
  impact.interventions[0].status = 'running'
  html = renderToStaticMarkup(<PulseImprovements impact={impact} />)
  expect(html).toContain('Applied · awaiting outcomes')
  expect(html).toContain('Outcome not established')
  impact.assessments.push({ assessment_id: 'proof', intervention_id: 'one', verdict: 'inconclusive', before_window: 'before', after_window: 'after', confidence: 'low', assessed_at: '2026-09-10', evidence: ['Need comparable samples'] })
  html = renderToStaticMarkup(<PulseImprovements impact={impact} />)
  expect(html).toContain('Not enough evidence')
  expect(html).not.toContain(' · Improved')
  expect(html).toContain('Unknown → Unknown')
})
