import type { RoutingStepNodeData, WorkflowEdge, WorkflowNode } from '../hooks/usePlanToFlow'
import type { EvaluationStep } from '../../../services/api-types'
import { evaluationMatchesRoute } from './routeEvaluations'

export interface RouteTrace {
  nodeId: string
  routeId: string
}

// Resolve the exact choice from its source handle, never from an edge label or
// an ID split (route and step IDs can themselves contain hyphens).
export function routeTraceFromEdge(nodes: WorkflowNode[], edge: WorkflowEdge): RouteTrace | null {
  const source = nodes.find(node => node.id === edge.source)
  if (!source || (source.type !== 'routing' && source.type !== 'branch')) return null
  const routes = (source.data as RoutingStepNodeData).routes
  const route = routes?.find(route => edge.sourceHandle === `route-${route.route_id}` ||
    edge.sourceHandle === `handoff-${route.route_id}`)
  return route ? { nodeId: source.id, routeId: route.route_id } : null
}

// Follow the rendered execution graph, including nested decisions, shared joins
// and handoffs. A loop back to the selected router must not open its siblings.
export function collectRouteTrace(edges: WorkflowEdge[], trace: RouteTrace) {
  const outgoing = new Map<string, WorkflowEdge[]>()
  for (const edge of edges) {
    if (edge.id.startsWith('dep-')) continue
    const list = outgoing.get(edge.source) || []
    list.push(edge)
    outgoing.set(edge.source, list)
  }
  const entryEdges = (outgoing.get(trace.nodeId) || []).filter(edge =>
    edge.sourceHandle === `route-${trace.routeId}` || edge.sourceHandle === `handoff-${trace.routeId}`)
  if (!entryEdges.length) return null
  const nodeIds = new Set([trace.nodeId])
  const edgeIds = new Set(entryEdges.map(edge => edge.id))
  const queue = entryEdges.map(edge => edge.target)
  for (let index = 0; index < queue.length; index++) {
    const id = queue[index]
    if (nodeIds.has(id)) continue
    nodeIds.add(id)
    for (const edge of outgoing.get(id) || []) {
      edgeIds.add(edge.id)
      if (!nodeIds.has(edge.target)) queue.push(edge.target)
    }
  }
  return { nodeIds, edgeIds }
}

export function traceRouteGraph(nodes: WorkflowNode[], edges: WorkflowEdge[], trace: RouteTrace | null) {
  const focus = trace ? collectRouteTrace(edges, trace) : null
  if (!focus) return { nodes, edges }
  const routingStepId = (nodes.find(node => node.id === trace!.nodeId)?.data.step as { id?: string } | undefined)?.id
  if (routingStepId) {
    for (const node of nodes) {
      if (node.type === 'evaluation-group') focus.nodeIds.delete(node.id)
    }
    for (const node of nodes) {
      if (node.data.isEvaluationStep) {
        if (evaluationMatchesRoute(node.data.step as EvaluationStep, routingStepId, trace!.routeId)) {
          focus.nodeIds.add(node.id)
          if (typeof node.data.evaluationGroupId === 'string') focus.nodeIds.add(node.data.evaluationGroupId)
        } else {
          focus.nodeIds.delete(node.id)
        }
      }

    }
    for (const edge of edges) {
      if (!focus.nodeIds.has(edge.source) || !focus.nodeIds.has(edge.target)) focus.edgeIds.delete(edge.id)
    }
  }
  // Supporting cards stay with the step they describe.
  for (const node of nodes) {
    if (typeof node.data.parentStepId === 'string' && focus.nodeIds.has(node.data.parentStepId)) {
      focus.nodeIds.add(node.id)
    }
  }
  return {
    nodes: nodes.map(node => ({ ...node, style: { ...node.style,
      opacity: focus.nodeIds.has(node.id) ? 1 : 0.14 } })),
    edges: edges.map(edge => {
      const active = focus.edgeIds.has(edge.id)
      const opacity = active ? 1 : 0.08
      return { ...edge, animated: active && edge.animated, zIndex: active ? 1 : 0,
        style: { ...edge.style, opacity, strokeWidth: active ? 3 : edge.style?.strokeWidth },
        labelStyle: { ...edge.labelStyle, opacity },
        labelBgStyle: { ...edge.labelBgStyle, opacity },
      }
    }),
  }
}
