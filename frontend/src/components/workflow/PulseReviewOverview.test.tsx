// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { expect, it, vi } from 'vitest'
vi.mock('../../services/api', () => ({ agentApi: { getPlannerFileContent: vi.fn() } }))
vi.mock('../ui/MarkdownRenderer', () => ({ MarkdownRenderer: ({ content }: { content: string }) => <div>{content}</div> }))
import { agentApi } from '../../services/api'
import { PulseReviewOverview } from './PulseReviewOverview'

it('opens saved Markdown in Pulse, shows failures, retries, and closes it', async () => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
  const path = 'Workflow/rtslatency/runs/pulse/pulse-1/strategic-review.md'
  vi.mocked(agentApi.getPlannerFileContent).mockRejectedValueOnce(new Error('Report temporarily unavailable'))
  const container = document.createElement('div'); document.body.append(container)
  const root = createRoot(container)
  try {
    await act(async () => root.render(<PulseReviewOverview moduleStates={[]} coverage={[]} audits={[]} findings={[]} moduleFilter={null} onSelectModule={() => {}}
      reports={[{ path, module: 'strategic_review', pulse_run_id: 'pulse-1', updated_at: '2026-09-09T09:00:00Z' }]} />))
    expect(agentApi.getPlannerFileContent).not.toHaveBeenCalled()
    const report = [...container.querySelectorAll('details')].find(node => node.querySelector('summary')?.textContent?.includes('Read report'))!
    await act(async () => { report.open = true; report.dispatchEvent(new Event('toggle')) })
    expect(agentApi.getPlannerFileContent).toHaveBeenCalledWith(path)
    expect(report.textContent).toContain('Report temporarily unavailable')
    vi.mocked(agentApi.getPlannerFileContent).mockResolvedValue({ success: true, data: { content: '# Strategic findings', filepath: path } } as Awaited<ReturnType<typeof agentApi.getPlannerFileContent>>)
    await act(async () => report.querySelector('button')!.click())
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 10)) })
    expect(report.textContent).toContain('Strategic findings')
    await act(async () => { report.open = false; report.dispatchEvent(new Event('toggle')) })
    expect(report.textContent).not.toContain('Strategic findings')
  } finally { await act(async () => root.unmount()); container.remove() }
})
