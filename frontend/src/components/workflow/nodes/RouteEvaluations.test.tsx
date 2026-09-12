// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { expect, it, vi } from 'vitest'
import type { RoutingStepNodeData, StepNodeData } from '../hooks/usePlanToFlow'
vi.mock('@xyflow/react', () => ({ Handle: () => null, Position: { Top: 'top', Bottom: 'bottom' } }))
import { RoutingStepNode } from './RoutingStepNode'
import { StepNode } from './StepNode'

it('renders per-route evaluation counts and their names without replacing trace controls', async () => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
  const host = document.createElement('div')
  const root = createRoot(host)
  const onTraceRoute = vi.fn()
  try {
    await act(async () => root.render(<RoutingStepNode data={{
      id: 'router', title: 'Schedule', stepIndex: 0, status: 'pending', onTraceRoute,
      step: { id: 'router', type: 'routing' }, routes: [{ route_id: 'daily', route_name: 'Daily' }, { route_id: 'weekly', route_name: 'Weekly' }],
      routeEvaluations: { daily: [{ id: 'check', title: 'Daily completeness' }], weekly: [] },
    } as unknown as RoutingStepNodeData} />))
    const daily = host.querySelector<HTMLButtonElement>('[aria-label="Trace route: Daily"]')!
    expect(daily.textContent).toContain('1 eval')
    expect(daily.title).toContain('Daily completeness')
    expect(host.querySelector('[aria-label="Trace route: Weekly"]')?.textContent).toContain('0 evals')
    await act(async () => daily.click())
    expect(onTraceRoute).toHaveBeenCalledWith('daily')
  } finally {
    await act(async () => root.unmount())
  }
})

it('labels evaluation cards with their scope, including fallback IDs for standalone eval views', async () => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
  const host = document.createElement('div')
  const root = createRoot(host)
  const data = { id: 'eval', title: 'Check output', stepIndex: 0, status: 'pending', isEvaluationStep: true,
    step: { id: 'eval', applies_to_routes: [{ routing_step_id: 'router', route_ids: ['daily'] }] },
  } as unknown as StepNodeData
  try {
    await act(async () => root.render(<StepNode data={{ ...data, evaluationScopeLabel: 'Schedule: Daily' }} />))
    expect(host.textContent).toContain('Evaluation · Schedule: Daily')
    await act(async () => root.render(<StepNode data={data} />))
    expect(host.textContent).toContain('router: daily')
    expect(host.textContent).not.toContain('All routes')
  } finally {
    await act(async () => root.unmount())
  }
})
