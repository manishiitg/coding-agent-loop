// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { expect, it, vi } from 'vitest'
vi.mock('../../services/api', () => ({ agentApi: { getPlannerFileContent: vi.fn() } }))
vi.mock('../ui/MarkdownRenderer', () => ({ MarkdownRenderer: ({ content }: { content: string }) => <div>{content}</div> }))
import { agentApi } from '../../services/api'
import { PulseReviewOverview } from './PulseReviewOverview'

it('keeps review history collapsed, pages older checks, and resets it when changing areas', async () => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
  const container = document.createElement('div'); document.body.append(container)
  const root = createRoot(container)
  const audits = Array.from({ length: 13 }, (_, i) => ({
    workspace_path: 'Workflow/example', module: 'technical_review', pulse_run_id: `run-${i}`,
    result: 'done', reason: `Review outcome ${i}`, recorded_at: `2026-09-${String(20 - i).padStart(2, '0')}T09:00:00Z`,
    verification: [`Verified check ${i}`],
  }))
  const reports = audits.slice(0, 3).map(audit => ({ module: audit.module, pulse_run_id: audit.pulse_run_id,
    path: `Workflow/example/runs/pulse/${audit.pulse_run_id}/technical-review.md`, updated_at: audit.recorded_at }))
  const render = (moduleFilter: string) => root.render(<PulseReviewOverview moduleStates={[]} coverage={[]} findings={[]}
    audits={audits} reports={reports} moduleFilter={moduleFilter} onSelectModule={() => {}} />)
  const button = (prefix: string) => [...container.querySelectorAll<HTMLButtonElement>('button')].find(node => node.textContent?.startsWith(prefix))!
  try {
    await act(async () => render('technical_review'))
    const checks = () => container.querySelector('[aria-label="Health checks and results"]')!
    expect(checks().querySelectorAll('details')).toHaveLength(1)
    expect(container.querySelector('[aria-label="Review history"]')).toBeNull()
    expect(container.querySelector('[aria-label="Health content"]')!.querySelectorAll('[aria-haspopup="dialog"]')).toHaveLength(1)
    await act(async () => button('View report history').click())
    expect(container.querySelector('[aria-label="Health content"]')!.querySelectorAll('[aria-haspopup="dialog"]')).toHaveLength(3)
    await act(async () => button('View review history').click())
    expect(container.querySelector('[aria-label="Review history"]')!.querySelectorAll('details')).toHaveLength(10)
    await act(async () => button('Show more reviews').click())
    expect(container.querySelector('[aria-label="Review history"]')!.querySelectorAll('details')).toHaveLength(12)
    expect(checks().textContent).toContain('Verified check 12')
    await act(async () => render('strategic_review'))
    await act(async () => render('technical_review'))
    expect(container.querySelector('[aria-label="Review history"]')).toBeNull()
    expect(checks().querySelectorAll('details')).toHaveLength(1)
    expect(container.querySelector('[aria-label="Health content"]')!.querySelectorAll('[aria-haspopup="dialog"]')).toHaveLength(1)
  } finally { await act(async () => root.unmount()); container.remove() }
})

it('opens saved Markdown outside the workspace panel, retries errors, and closes the reader', async () => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
  const path = 'Workflow/rtslatency/runs/pulse/pulse-1/strategic-review.md'
  vi.mocked(agentApi.getPlannerFileContent).mockRejectedValueOnce(new Error('Report temporarily unavailable'))
  const container = document.createElement('div'); document.body.append(container)
  const root = createRoot(container)
  try {
    await act(async () => root.render(<PulseReviewOverview moduleStates={[]} coverage={[]} audits={[]} findings={[]} moduleFilter="strategic_review" onSelectModule={() => {}}
      reports={[{ path, module: 'strategic_review', pulse_run_id: 'pulse-1', updated_at: '2026-09-09T09:00:00Z' }]} />))
    expect(agentApi.getPlannerFileContent).not.toHaveBeenCalled()
    const trigger = container.querySelector<HTMLButtonElement>('[aria-haspopup="dialog"]')!
    await act(async () => trigger.click())
    const report = document.querySelector('dialog')!
    expect(report.open).toBe(true)
    expect(container.contains(report)).toBe(false)
    expect(report.className).toContain('w-screen')
    expect(agentApi.getPlannerFileContent).toHaveBeenCalledWith(path)
    expect(report.textContent).toContain('Report temporarily unavailable')
    vi.mocked(agentApi.getPlannerFileContent).mockResolvedValue({ success: true, data: { content: '# Strategic findings', filepath: path } } as Awaited<ReturnType<typeof agentApi.getPlannerFileContent>>)
    await act(async () => [...report.querySelectorAll('button')].find(button => button.textContent === 'Retry')!.click())
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 10)) })
    expect(report.textContent).toContain('Strategic findings')
    await act(async () => report.querySelector<HTMLButtonElement>('[aria-label="Close report"]')!.click())
    expect(document.querySelector('dialog')).toBeNull()
    await act(async () => trigger.click())
    await act(async () => document.querySelector('dialog')!.dispatchEvent(new Event('cancel', { cancelable: true })))
    expect(document.querySelector('dialog')).toBeNull()
  } finally { await act(async () => root.unmount()); container.remove() }
})

it('shows Architecture as its own review area with a separate drift check', async () => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
  const container = document.createElement('div'); const root = createRoot(container)
  try {
    await act(async () => root.render(<PulseReviewOverview moduleStates={[]} coverage={[]} findings={[]} audits={[]} reports={[]} moduleFilter="architecture_review" onSelectModule={() => {}} />))
    const navigation = container.querySelector('[aria-label="Pulse work areas"]')!
    expect(navigation.querySelectorAll('button')).toHaveLength(3)
    expect(navigation.textContent).toContain('Architecture')
    expect(navigation.textContent).not.toContain('Drift check')
    expect(container.querySelector('[aria-label="Architecture content"]')?.textContent).toContain('Learning quality')
    expect(container.querySelector('[aria-label="Health content"]')).toBeNull()
  } finally { await act(async () => root.unmount()) }
})
