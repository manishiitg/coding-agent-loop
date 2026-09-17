// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PulseFindingLifecycle, PulseReviewFocus } from '../../services/api-types'

vi.mock('../../services/api', () => ({
  getApiBaseUrl: () => '',
  agentApi: {
    getPulseFindings: vi.fn(), getPulseReviews: vi.fn(), getPulseImpact: vi.fn(), getPulseContext: vi.fn(),
  },
}))
vi.mock('../../api/playbooks', () => ({
  playbooksApi: { list: vi.fn(), listInstalled: vi.fn() },
}))
vi.mock('./SoulViewer', () => ({
  SoulViewer: () => null,
  WORKFLOW_SOUL_REFRESH_EVENT: 'workflow-soul-refresh',
}))
vi.mock('./ReportHumanInputPanel', () => ({ ReportHumanInputPanel: () => null }))

import { agentApi } from '../../services/api'
import { playbooksApi } from '../../api/playbooks'
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
    vi.mocked(playbooksApi.list).mockResolvedValue([{
      id: 'browser-performance-validation', title: 'Browser Performance Validation', description: 'Measure journeys.', version: '0.2.1',
      category: 'Performance Engineering', order: 1, inputCount: 6, toolCount: 3, pulseFocus: [
        { module: 'strategic_review', label: 'Performance strategy', focus_areas: ['Budgets align with the workflow goal'], review_when: ['A customer goal or budget changes'] },
      ],
    }])
    vi.mocked(playbooksApi.listInstalled).mockResolvedValue([{
      id: 'browser-performance-validation', title: 'Browser Performance Validation', version: '0.2.1', category: 'Performance Engineering',
      skill_name: 'agentworks-playbook-browser-performance-validation', source_hash: 'test', status: 'ready', installed_at: '2026-09-13',
    }])
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
    await click('Technical')
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
    await click('Strategy')
    expect(container.textContent).toContain('Budgets align with the workflow goal')
    expect(container.textContent).toContain('Browser Performance Validation · Performance strategy')
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
    await click('Technical')
    expect(button('Current').getAttribute('aria-pressed')).toBe('true')
    shownCount(5)
    await click('Strategy')
    await click('Platform repair pending')
    expect(count('Platform repair pending')).toBe(0)
    shownCount(0)
    await act(async () => (container.querySelector('[aria-label="Clear review area filter"]') as HTMLButtonElement).click())
    expect(button('Platform repair pending').getAttribute('aria-pressed')).toBe('true')
    shownCount(4)
  })

  it('keeps custom playbook focus on strategy and not architecture', async () => {
    expect(button('Architecture').textContent).not.toContain('Strategic focus')
    await click('Architecture')
    expect(container.textContent).not.toContain('Strategic playbook focus')
    await click('Strategy')
    expect(container.textContent).toContain('Strategic playbook focus')
    expect(container.textContent).toContain('Budgets align with the workflow goal')
  })

  it('shows reviewer run counts and dates at the bottom', async () => {
    vi.mocked(agentApi.getPulseReviews).mockResolvedValue({ success: true, reviews: [
      ...['2026-09-17T03:00:00Z', '2026-09-14T03:00:00Z', '2026-09-11T03:00:00Z', '2026-09-08T03:00:00Z']
        .map((recorded_at, index) => ({ id: index + 1, module: 'technical_review', review_run_id: `tech-${index}`, finding_count: 0, verification_count: 0, recorded_at })),
      { id: 10, module: 'strategic_review', review_run_id: 'strategy-1', finding_count: 1, verification_count: 0, recorded_at: '2026-09-17T04:00:00Z' },
      { id: 11, module: 'architecture_review', review_run_id: 'architecture-1', finding_count: 0, verification_count: 0, recorded_at: '2026-09-10T04:00:00Z' },
    ] })
    await act(async () => window.dispatchEvent(new CustomEvent(WORKFLOW_LOG_REFRESH_EVENT)))
    const history = container.querySelector('[aria-label="Review run history"]')!
    const cards = [...history.querySelectorAll(':scope > div:last-child > div')]
    const card = (label: string) => cards.find((item) => item.textContent?.startsWith(label))!
    expect(card('Plan Drift').textContent).toContain('0 runs')
    expect(card('Technical').textContent).toContain('4 runs')
    expect(card('Technical').textContent).toContain('View all 4 run dates')
    expect(card('Technical').textContent).toContain('2026')
    expect(card('Architecture').textContent).toContain('1 run')
    expect(card('Strategic').textContent).toContain('1 run')
  })


  it('opens drift content and resolved findings together when no current findings remain', async () => {
    expect(container.querySelector('[aria-label="Strategy content"]')).not.toBeNull()
    await click('Drift check')
    expect(button('Drift check').getAttribute('aria-pressed')).toBe('true')
    expect(container.querySelector('[aria-label="Drift check content"]')?.textContent).toContain('No current drift findings.')
    expect(container.querySelector('[aria-label="Strategy content"]')).toBeNull()
    expect(container.textContent).not.toContain('View drift findings')
    expect(button('Resolved').getAttribute('aria-pressed')).toBe('true')
    shownCount(3)
    expect(container.textContent).toContain('PUL-R0')
    await click('Strategy')
    expect(container.querySelector('[aria-label="Drift check content"]')).toBeNull()
    expect(container.querySelector('[aria-label="Strategy content"]')).not.toBeNull()
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
    expect(button('Strategy').getAttribute('aria-pressed')).toBe('true')
    shownCount(10)
  })
})
