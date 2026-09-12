import type { EvaluationStep } from '../../../services/api-types'
import type { WorkflowNode, WorkflowEdge, EvaluationStepNodeData } from '../hooks/usePlanToFlow'
import { annotateRouteEvaluations, evaluationScopeLabel } from './routeEvaluations'

const CARD_WIDTH = 280
const CARD_HEIGHT = 76
const GAP = 16
const GROUP_WIDTH = CARD_WIDTH * 2 + GAP

// Group by the exact AND/OR gate identity, not display labels. Shared checks
// appear once, even when they apply to several routes.
function scopeKey(step: EvaluationStep) {
  return JSON.stringify((step.applies_to_routes ?? []).map(gate => ({
    routing_step_id: gate.routing_step_id,
    route_ids: [...new Set(gate.route_ids)].sort(),
  })).sort((a, b) => JSON.stringify(a).localeCompare(JSON.stringify(b))))
}

export function appendEvaluationGroups(
  flow: { nodes: WorkflowNode[]; edges: WorkflowEdge[] },
  steps: EvaluationStep[],
  options: { horizontal: boolean; workspacePath?: string | null; selectedRunFolder?: string | null },
) {
  if (!flow.nodes.length) return flow
  const nodes = [...flow.nodes]
  const edges = [...flow.edges]
  if (!steps.length) return { nodes: annotateRouteEvaluations(nodes, []), edges }
  const end = nodes.find(node => node.id === 'end')
  if (!end) return flow
  const groups = new Map<string, { step: EvaluationStep; index: number }[]>()
  steps.forEach((step, index) => {
    const key = scopeKey(step)
    groups.set(key, [...(groups.get(key) ?? []), { step, index }])
  })
  const ordered = [...groups.entries()].sort(([a], [b]) => Number(a === '[]') - Number(b === '[]'))
  const origin = options.horizontal
    ? { x: Math.max(...nodes.map(n => n.position.x + (n.width ?? 320))) + 100, y: end.position.y }
    : { x: end.position.x - GROUP_WIDTH, y: Math.max(...nodes.map(n => n.position.y + (n.height ?? 300))) + 100 }
  let rowY = origin.y
  let rowHeight = 0
  let slot = 0
  ordered.forEach(([key, members]) => {
    const shared = key === '[]'
    if (shared && slot) { rowY += rowHeight + 36; slot = 0; rowHeight = 0 }
    const columns = shared ? 4 : 2
    const width = shared ? GROUP_WIDTH * 2 + 32 : GROUP_WIDTH
    const height = 64 + Math.ceil(members.length / columns) * (CARD_HEIGHT + GAP)
    const x = origin.x + slot * (GROUP_WIDTH + 32)
    const groupId = `workflow-evaluation-group-${encodeURIComponent(key)}`
    const scope = shared ? 'All routes' : evaluationScopeLabel(members[0].step, flow.nodes)
    nodes.push({ id: groupId, type: 'evaluation-group', position: { x, y: rowY },
      width, height: 52, draggable: false, selectable: false,
      data: { id: groupId, title: scope, kind: 'evaluation', configured: true,
        detail: `${members.length} evaluation${members.length === 1 ? '' : 's'}${shared ? ' · shared across every route' : ' · runs only when its route conditions match'}` },
      style: { width },
    })
    edges.push({ id: `end-to-${groupId}`, source: 'end', target: groupId, type: 'smoothstep',
      style: { stroke: '#64748b', strokeWidth: 1, strokeDasharray: '4 5' } })
    members.forEach(({ step, index }, position) => {
      const id = `workflow-evaluation-step-${step.id || index}`
      const data: EvaluationStepNodeData = { id, title: step.title || `Evaluation ${index + 1}`,
        description: step.description, success_criteria: step.success_criteria, status: 'pending',
        stepIndex: index, step, workspacePath: options.workspacePath,
        selectedRunFolder: options.selectedRunFolder ?? undefined,
        isEvaluationStep: true, evaluationGroupId: groupId,
      }
      nodes.push({ id, type: 'evaluation-card', data, draggable: false,
        position: { x: x + (position % columns) * (CARD_WIDTH + GAP), y: rowY + 64 + Math.floor(position / columns) * (CARD_HEIGHT + GAP) },
        width: CARD_WIDTH, height: CARD_HEIGHT,
      })
    })
    rowHeight = Math.max(rowHeight, height)
    slot += shared ? 2 : 1
    if (slot === 2) { rowY += rowHeight + 36; slot = 0; rowHeight = 0 }
  })
  return { nodes: annotateRouteEvaluations(nodes, steps), edges }
}
