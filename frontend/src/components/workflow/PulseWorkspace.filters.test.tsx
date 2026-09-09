// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PulseFindingLifecycle, PulseReviewFocus } from '../../services/api-types'

vi.mock('../../services/api', () => ({ agentApi: {
  getPulseFindings: vi.fn(), getPulseReviews: vi.fn(), getPulseImpact: vi.fn(), getPulseContext: vi.fn(),
} }))
vi.mock('./SoulViewer', () => ({ SoulViewer: () => null }))
vi.mock('./ReportHumanInputPanel', () => ({ ReportHumanInputPanel: () => null }))

import { agentApi } from '../../services/api'
import { PulseWorkspace } from './PulseWorkspace'
import { WORKFLOW_LOG_REFRESH_EVENT } from './workflowEvents'

function finding(id: string, module: string, status: string): PulseFindingLifecycle {
  return { finding_id: id, module, step_id: module, kind: 'issue', phase: 'review', status,
    text: id, seen_count: 1, fix_attempts: [], verifications: [], events: [] }
}
const queued = [finding('PUL-Q1', 'step-revise-draft', 'queued_for_engineering'),
  finding('PUL-Q2', 'step-check-draft-approval', 'queued_for_engineering')]
const evidence = Array.from({ length: 4 }, (_, index) => ({
  ...finding(`PUL-E${index}`, 'strategic_review', 'acknowledged'),
  details: { recommended_route: 'evidence_wait', next_check: 'After ten completed growth days', reproduction: { safe: true } },
  events: [{ event_type: 'proposal_recorded', summary: 'Old proposal', recorded_at: '2026-08-01' }],
}))
const platforms = Array.from({ length: 4 }, (_, index) => finding(`PUL-P${index}`, `step-platform-${index}`, 'external_action_required'))
const resolved = Array.from({ length: 11 }, (_, index) => finding(`PUL-R${index}`,
  index < 3 ? 'plan_drift_review' : 'technical_review', 'resolved'))
const records = [...queued, ...evidence, ...platforms, ...resolved]
const selections: PulseReviewFocus[] = [{ workspace_path: 'Workflow/substack', module: 'technical_review',
  focus_key: 'execution_health', updated_at: '2026-09-05', issue_ids: ['PUL-Q1', 'PUL-Q2', 'PUL-P0', 'PUL-P1', 'PUL-P2'] }]

describe('Pulse workspace filter interactions', () => {
  let container: HTMLDivElement
  let root: Root
  const render = (path = 'Workflow/substack') => root.render(<PulseWorkspace workspacePath={path}
    moduleStates={[]} finalCommandStates={[]} reviewFocuses={[]} reviewFocusSelections={selections} statusError={null} />)
  const button = (prefix: string) => {
    const match = [...container.querySelectorAll('button')].find((node) => node.textContent?.startsWith(prefix))
    expect(match, `Missing button ${prefix}`).toBeTruthy()
    return match!
  }
  const click = async (prefix: string) => { await act(async () => button(prefix).click()) }
  const count = (prefix: string) => Number(button(prefix).querySelector('span')?.textContent)
  const shown = () => [...container.querySelectorAll('[aria-expanded]')].length
  const shownCount = (n: number) => expect(container.textContent).toContain(`${n} shown`)

  beforeEach(async () => {
    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
    vi.mocked(agentApi.getPulseFindings).mockResolvedValue({ success: true, findings: records })
    vi.mocked(agentApi.getPulseReviews).mockResolvedValue({ success: true, reviews: [] })
    vi.mocked(agentApi.getPulseImpact).mockResolvedValue({ success: true, impact: { interventions: [], observations: [], assessments: [] } })
    vi.mocked(agentApi.getPulseContext).mockResolvedValue({ success: true, records: [], total: 0 })
    container = document.createElement('div')
    document.body.append(container)
    root = createRoot(container)
    await act(async () => render())
  })
  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
  })

  it('reloads changed findings on a decision update without resetting the selected filter', async () => {
    await click('Queued for Pulse')
    shownCount(2)
    vi.mocked(agentApi.getPulseFindings).mockResolvedValue({ success: true,
      findings: records.map(item => item.finding_id === 'PUL-Q1' ? { ...item, status: 'resolved' } : item),
    })
    await act(async () => window.dispatchEvent(new CustomEvent(WORKFLOW_LOG_REFRESH_EVENT)))
    expect(button('Queued for Pulse').getAttribute('aria-pressed')).toBe('true')
    shownCount(1)
    expect(container.textContent).not.toContain('PUL-Q1')
    expect(container.textContent).toContain('PUL-Q2')
  })

  it('clicks all nine queues and resets both category and area', async () => {
    await click('Clear filter')
    for (const [label, expected] of [
      ['Current', 10], ['Pulse to fix', 0], ['Queued for Pulse', 2], ['Waiting for evidence', 4],
      ['Your decisions', 0], ['Ideas', 0], ['Paused', 0], ['Platform repair pending', 4], ['Resolved', 11],
    ] as const) {
      await click(label)
      expect(count(label)).toBe(expected)
      shownCount(expected)
      expect(shown()).toBe(expected)
      expect(button(label).getAttribute('aria-pressed')).toBe('true')
    }
    await click('Drift check')
    await click('Resolved')
    expect(count('Resolved')).toBe(3)
    shownCount(3)
    await click('Clear filter')
    shownCount(10)
    expect(count('Resolved')).toBe(11)
    expect(container.querySelector('[aria-label="Clear review area filter"]')).toBeNull()
  })

  it('keeps step-reported technical issues visible and badges scoped', async () => {
    await click('Technical review')
    expect(count('Current')).toBe(5)
    expect(count('Queued for Pulse')).toBe(2)
    expect(count('Platform repair pending')).toBe(3)
    expect(count('Waiting for evidence')).toBe(0)
    await click('Queued for Pulse')
    shownCount(2)
    expect(container.textContent).toContain('PUL-Q1')
    expect(container.textContent).toContain('PUL-Q2')
    expect(container.textContent).toContain('Step-Revise-Draft')
    expect(container.textContent).toContain('Technical review › Execution health')
  })

  it('shows evidence waits, resets category on every area switch, and removes only the area', async () => {
    await click('Strategic review')
    expect(button('Current').getAttribute('aria-pressed')).toBe('true')
    expect(count('Waiting for evidence')).toBe(4)
    expect(count('Ideas')).toBe(0)
    await click('Waiting for evidence')
    shownCount(4)
    await act(async () => [...container.querySelectorAll<HTMLElement>('[role="button"][aria-expanded]')]
      .find((node) => node.textContent?.includes('PUL-E0'))!.click())
    expect(container.textContent).toContain('After ten completed growth days')
    await click('Ideas')
    shownCount(0)
    await click('Technical review')
    expect(button('Current').getAttribute('aria-pressed')).toBe('true')
    shownCount(5)
    await click('Strategic review')
    await click('Platform repair pending')
    expect(count('Platform repair pending')).toBe(0)
    shownCount(0)
    await act(async () => (container.querySelector('[aria-label="Clear review area filter"]') as HTMLButtonElement).click())
    expect(button('Platform repair pending').getAttribute('aria-pressed')).toBe('true')
    shownCount(4)
  })


  it('opens drift content and resolved findings together when no current findings remain', async () => {
    expect(container.querySelector('[aria-label="Technical review content"]')).not.toBeNull()
    await click('Drift check')
    expect(button('Drift check').getAttribute('aria-pressed')).toBe('true')
    expect(container.querySelector('[aria-label="Drift check content"]')?.textContent).toContain('No current drift findings.')
    expect(container.querySelector('[aria-label="Technical review content"]')).toBeNull()
    expect(container.textContent).not.toContain('View drift findings')
    expect(button('Resolved').getAttribute('aria-pressed')).toBe('true')
    shownCount(3)
    expect(container.textContent).toContain('PUL-R0')
    await click('Strategic review')
    expect(container.querySelector('[aria-label="Drift check content"]')).toBeNull()
    expect(container.querySelector('[aria-label="Strategic review content"]')).not.toBeNull()
    shownCount(4)
  })

  it('opens an explicit empty drift view when no findings were recorded', async () => {
    vi.mocked(agentApi.getPulseFindings).mockResolvedValue({ success: true, findings: [] })
    await act(async () => window.dispatchEvent(new CustomEvent(WORKFLOW_LOG_REFRESH_EVENT)))
    await click('Drift check')
    expect(button('Current').getAttribute('aria-pressed')).toBe('true')
    shownCount(0)
    expect(container.textContent).toContain('Completed drift checks and their details are shown above.')
    expect(container.textContent).not.toContain('Nothing in this queue')
  })

  it('does not carry filters into another workflow', async () => {
    await click('Drift check')
    await click('Resolved')
    await act(async () => render('Workflow/another'))
    expect(button('Current').getAttribute('aria-pressed')).toBe('true')
    expect(button('Technical review').getAttribute('aria-pressed')).toBe('true')
    shownCount(5)
  })
})
