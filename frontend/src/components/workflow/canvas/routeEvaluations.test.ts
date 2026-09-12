import { describe, expect, it } from 'vitest'
import type { EvaluationStep } from '../../../services/api-types'
import type { RoutingStepNodeData, WorkflowNode } from '../hooks/usePlanToFlow'
import { annotateRouteEvaluations, evaluationMatchesRoute, evaluationScopeLabel } from './routeEvaluations'
import { traceRouteGraph } from './routeTrace'

const router = (id: string, type = 'routing'): WorkflowNode => ({
  id: `rendered-${id}`, type, position: { x: 0, y: 0 },
  data: { id, title: id === 'entry' ? 'Schedule' : 'Delivery', step: { id, type },
    routes: [{ route_id: 'daily', route_name: 'Daily' }, { route_id: 'weekly', route_name: 'Weekly' }],
  },
} as WorkflowNode)
const gate = (routing_step_id: string, ...route_ids: string[]) => ({ routing_step_id, route_ids })
const evaluation = (id: string, applies_to_routes?: EvaluationStep['applies_to_routes']): EvaluationStep => ({
  id, title: `Check ${id}`, description: '', success_criteria: '', applies_to_routes,
})
const steps = [evaluation('all'), evaluation('daily', [gate('entry', 'daily')]),
  evaluation('weekly', [gate('entry', 'weekly')]), evaluation('both', [gate('entry', 'daily', 'weekly')]),
  evaluation('nested', [gate('delivery', 'daily')]),
  evaluation('combined', [gate('entry', 'weekly'), gate('delivery', 'daily')])]

describe('route evaluations in the plan', () => {
  it('pairs by the source step ID and route ID, not a repeated route name or canvas ID', () => {
    const original = [router('entry'), router('delivery', 'branch')]
    const result = annotateRouteEvaluations(original, steps)
    const entry = result[0].data as RoutingStepNodeData
    expect(entry.routeEvaluations?.daily.map(step => step.id)).toEqual(['daily', 'both'])
    expect(entry.routeEvaluations?.weekly.map(step => step.id)).toEqual(['weekly', 'both', 'combined'])
    expect(entry.allRouteEvaluationCount).toBe(1)
    expect((result[1].data as RoutingStepNodeData).routeEvaluations?.daily.map(step => step.id)).toEqual(['nested', 'combined'])
    expect(original[0].data.routeEvaluations).toBeUndefined()
  })

  it('shows readable OR/AND scope and keeps unknown references visible', () => {
    const nodes = [router('entry'), router('delivery', 'branch')]
    expect(evaluationScopeLabel(steps[0], nodes)).toBe('All routes')
    expect(evaluationScopeLabel(steps[3], nodes)).toBe('Schedule: Daily or Weekly')
    expect(evaluationScopeLabel(steps[5], nodes)).toBe('Schedule: Weekly AND Delivery: Daily')
    expect(evaluationScopeLabel(evaluation('stale', [gate('missing', 'removed')]), nodes)).toBe('missing: removed')
    expect(evaluationScopeLabel(evaluation('invalid', [gate('entry')]), nodes)).not.toBe('All routes')
  })

  it('refreshing a deleted or changed eval clears its old route pairing', () => {
    const initial = annotateRouteEvaluations([router('entry')], steps)
    const moved = annotateRouteEvaluations(initial, [evaluation('daily', [gate('entry', 'weekly')])])
    expect((moved[0].data as RoutingStepNodeData).routeEvaluations?.daily).toEqual([])
    expect((moved[0].data as RoutingStepNodeData).routeEvaluations?.weekly.map(step => step.id)).toEqual(['daily'])
    expect((annotateRouteEvaluations(moved, [])[0].data as RoutingStepNodeData).routeEvaluations?.weekly).toEqual([])
  })

  it('does not mistake one traced decision for all conditions being satisfied', () => {
    expect(evaluationMatchesRoute(steps[5], 'entry', 'daily')).toBe(false)
    expect(evaluationMatchesRoute(steps[5], 'entry', 'weekly')).toBe(true)
    expect(evaluationMatchesRoute(steps[5], 'delivery', 'weekly')).toBe(false)
    expect(evaluationMatchesRoute(steps[0], 'entry', 'daily')).toBe(true)
  })

  it('tracing keeps global and compatible evals, dims incompatible evals and their edges, and restores on clear', () => {
    const nodes = [router('entry'), { id: 'end', data: {}, position: { x: 0, y: 0 } },
      ...steps.map(step => ({ id: `eval-${step.id}`, type: 'step', position: { x: 0, y: 0 },
        data: { isEvaluationStep: true, step } }))] as WorkflowNode[]
    const edges = [{ id: 'route', source: 'rendered-entry', sourceHandle: 'route-daily', target: 'end' },
      ...steps.map((step, index) => ({ id: `to-${step.id}`, source: index === 0 ? 'end' : `eval-${steps[index - 1].id}`, target: `eval-${step.id}` }))]
    const result = traceRouteGraph(nodes, edges, { nodeId: 'rendered-entry', routeId: 'daily' })
    const opacity = (id: string) => result.nodes.find(node => node.id === `eval-${id}`)?.style?.opacity
    expect(opacity('all')).toBe(1)
    expect(opacity('daily')).toBe(1)
    expect(opacity('both')).toBe(1)
    expect(opacity('nested')).toBe(1) // This trace does not choose the nested decision.
    expect(opacity('weekly')).toBe(0.14)
    expect(opacity('combined')).toBe(0.14)
    expect(result.edges.find(edge => edge.id === 'to-weekly')?.style?.opacity).toBe(0.08)
    expect(traceRouteGraph(nodes, edges, null)).toEqual({ nodes, edges })
  })
})
