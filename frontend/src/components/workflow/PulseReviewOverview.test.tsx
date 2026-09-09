// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { expect, it, vi } from 'vitest'
vi.mock('../../services/api', () => ({ agentApi: { getPlannerFileContent: vi.fn() } }))
vi.mock('../ui/MarkdownRenderer', () => ({ MarkdownRenderer: ({ content }: { content: string }) => <div>{content}</div> }))
import { agentApi } from '../../services/api'
import { PulseReviewOverview } from './PulseReviewOverview'

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
