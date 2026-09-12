import { describe, expect, it } from 'vitest'
import type { EvaluationStep } from '../../../services/api-types'
import type { WorkflowNode } from '../hooks/usePlanToFlow'
import { appendEvaluationGroups } from './evaluationLayout'
import { traceRouteGraph } from './routeTrace'

const check = (id: string, routes?: string[]): EvaluationStep => ({ id, title: id, description: '', success_criteria: '',
  ...(routes ? { applies_to_routes: [{ routing_step_id: 'router', route_ids: routes }] } : {}),
})
const flow = { nodes: [
  { id: 'router', type: 'branch', position: { x: 0, y: 0 }, data: { id: 'router', title: 'Trading', step: { id: 'router', type: 'branch' },
    routes: ['propose', 'test', 'trade'].map(route_id => ({ route_id, route_name: route_id })) } },
  { id: 'end', type: 'end', position: { x: 0, y: 400 }, data: { id: 'end', title: 'End' } },
] as WorkflowNode[], edges: [{ id: 'test-route', source: 'router', target: 'end', sourceHandle: 'route-test' }] }

describe('grouped evaluation layout', () => {
  it.each([false, true])('groups trading-sized plans without duplicate checks or serial edges (horizontal=%s)', horizontal => {
    const steps = [...Array.from({ length: 10 }, (_, i) => check(`shared-${i}`)), check('proposal', ['propose']),
      ...Array.from({ length: 4 }, (_, i) => check(`test-${i}`, ['test'])),
      check('exits', ['trade']), check('entry', ['trade']), check('risk', ['test', 'trade'])]
    const result = appendEvaluationGroups(flow, steps, { horizontal, workspacePath: 'Workflow/trading' })
    const cards = result.nodes.filter(n => n.data.isEvaluationStep)
    const groups = result.nodes.filter(n => n.type === 'evaluation-group')
    expect(cards).toHaveLength(18)
    expect(groups).toHaveLength(5)
    expect(new Set(cards.map(n => n.id)).size).toBe(18)
    expect(cards.map(n => n.data.step)).toEqual(expect.arrayContaining(steps))
    expect(result.edges.filter(e => e.source.startsWith('workflow-evaluation-step-'))).toHaveLength(0)
    expect(Math.max(...cards.map(n => n.position.y)) - Math.min(...cards.map(n => n.position.y))).toBeLessThan(1000)
    for (const a of cards) for (const b of cards) {
      if (a.id === b.id) continue
      const overlaps = a.position.x < b.position.x + 280 && a.position.x + 280 > b.position.x &&
        a.position.y < b.position.y + 76 && a.position.y + 76 > b.position.y
      expect(overlaps).toBe(false)
    }
    const traced = traceRouteGraph(result.nodes, result.edges, { nodeId: 'router', routeId: 'test' })
    expect(traced.nodes.find(n => n.id === 'workflow-evaluation-step-risk')?.style?.opacity).toBe(1)
    expect(traced.nodes.find(n => n.id === 'workflow-evaluation-step-proposal')?.style?.opacity).toBe(0.14)
    expect(traced.nodes.find(n => n.data.title === 'All routes')?.style?.opacity).toBe(1)
    expect(traced.nodes.find(n => n.type === 'evaluation-group' && n.data.title === 'Trading: propose')?.style?.opacity).toBe(0.14)
    expect(flow.nodes).toHaveLength(2)
  })
  it('groups equivalent OR scopes regardless of route order and preserves unknown scopes', () => {
    const steps = [check('a', ['test', 'trade']), check('b', ['trade', 'test']), check('c', ['removed'])]
    const result = appendEvaluationGroups(flow, steps, { horizontal: false })
    expect(result.nodes.filter(n => n.type === 'evaluation-group')).toHaveLength(2)
    expect(result.nodes.some(n => n.data.title === 'Trading: removed')).toBe(true)
    expect(appendEvaluationGroups(flow, [], { horizontal: false }).nodes).toHaveLength(2)
  })
})
