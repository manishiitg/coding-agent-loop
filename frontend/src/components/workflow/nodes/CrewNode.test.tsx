// @vitest-environment happy-dom
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { expect, it, vi } from 'vitest'
import { CrewNode } from './CrewNode'
import type { CrewStepNodeData } from '../hooks/usePlanToFlow'
vi.mock('@xyflow/react', () => ({ Handle: () => null, Position: { Top: 'top', Bottom: 'bottom' } }))
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

const crewStep = {
  type: 'crew',
  id: 'crew-1',
  title: 'Review with RTS',
  crew_profile_id: 'work',
  crew_project_id: 'rts',
  trigger_id: 'trig-1',
  instruction: 'Review the PR',
  context_output: 'review.md',
} as const

const data = (overrides: Partial<CrewStepNodeData> = {}): CrewStepNodeData => ({
  id: 'crew-1',
  title: 'Review with RTS',
  crew_profile_id: 'work',
  crew_project_id: 'rts',
  trigger_id: 'trig-1',
  status: 'pending',
  stepIndex: 0,
  step: { ...crewStep },
  ...overrides,
})

async function renderNode(nodeData: CrewStepNodeData, selected = false): Promise<{ host: HTMLElement; unmount: () => Promise<void> }> {
  const host = document.createElement('div')
  const root = createRoot(host)
  await act(async () => root.render(<CrewNode data={nodeData} selected={selected} />))
  return { host, unmount: () => act(async () => root.unmount()) }
}

it('shows the crew project, trigger, and response file', async () => {
  const { host, unmount } = await renderNode(data())
  try {
    expect(host.textContent).toContain('Review with RTS')
    expect(host.textContent).toContain('rts')
    expect(host.textContent).toContain('trig-1')
    expect(host.textContent).toContain('review.md')
  } finally {
    await unmount()
  }
})

it('defaults the response file to response.md', async () => {
  const { host, unmount } = await renderNode(data({ step: { ...crewStep, context_output: undefined } }))
  try {
    expect(host.textContent).toContain('response.md')
  } finally {
    await unmount()
  }
})

it('marks running and failed runs', async () => {
  const running = await renderNode(data({ status: 'running' }))
  try {
    expect(running.host.textContent).toContain('Running')
  } finally {
    await running.unmount()
  }
  const failed = await renderNode(data({ status: 'failed' }))
  try {
    expect(failed.host.textContent).toContain('Failed')
  } finally {
    await failed.unmount()
  }
})

it('highlights plan changes', async () => {
  const { host, unmount } = await renderNode(data({ changeType: 'added' }))
  try {
    // Badge text is lowercase in the DOM; CSS capitalize renders it as Added.
    expect(host.textContent).toContain('added')
  } finally {
    await unmount()
  }
})
