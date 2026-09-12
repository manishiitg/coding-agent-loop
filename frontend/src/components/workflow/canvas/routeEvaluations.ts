import type { EvaluationStep } from '../../../services/api-types'
import type { RoutingStepNodeData, WorkflowNode } from '../hooks/usePlanToFlow'

// Match backend semantics: alternatives within one gate are OR; gates are AND.
// A trace supplies only one decision, so other decisions remain conditional.
export function evaluationMatchesRoute(step: EvaluationStep, routingStepId: string, routeId: string) {
  return (step.applies_to_routes ?? []).every(gate =>
    gate.routing_step_id !== routingStepId || !gate.route_ids.length || gate.route_ids.includes(routeId))
}

export function evaluationScopeLabel(step: EvaluationStep, nodes: WorkflowNode[]): string {
  const gates = step.applies_to_routes ?? []
  if (!gates.length) return 'All routes'
  return gates.map(gate => {
    const router = nodes.find(node => (node.type === 'routing' || node.type === 'branch') &&
      (node.data.step as { id?: string } | undefined)?.id === gate.routing_step_id)
    const data = router?.data as RoutingStepNodeData | undefined
    const routes = gate.route_ids.map(id => data?.routes?.find(route => route.route_id === id)?.route_name || id)
    return `${data?.title || gate.routing_step_id}: ${routes.join(' or ') || 'No routes specified'}`
  }).join(' AND ')
}

export function annotateRouteEvaluations(nodes: WorkflowNode[], steps: EvaluationStep[]): WorkflowNode[] {
  return nodes.map(node => {
    if (node.data.isEvaluationStep) {
      return { ...node, data: { ...node.data,
        evaluationScopeLabel: evaluationScopeLabel(node.data.step as EvaluationStep, nodes),
      } }
    }
    if (node.type !== 'routing' && node.type !== 'branch') return node
    const data = node.data as RoutingStepNodeData
    const routingStepId = data.step.id
    const routeEvaluations = Object.fromEntries((data.routes ?? []).map(route => [route.route_id,
      steps.filter(step => step.applies_to_routes?.some(gate => gate.routing_step_id === routingStepId) &&
        evaluationMatchesRoute(step, routingStepId, route.route_id))
        .map(step => ({ id: step.id, title: step.title || step.id })),
    ]))
    return { ...node, data: { ...data, routeEvaluations,
      allRouteEvaluationCount: steps.filter(step => !step.applies_to_routes?.length).length,
    } }
  })
}
