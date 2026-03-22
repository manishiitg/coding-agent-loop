import { useMemo } from 'react'
import type { Edge, Node } from '@xyflow/react'
import dagre from 'dagre'
import type { WorkflowOutputPlan } from '../../../services/api-types'

export interface OutputStepNodeData extends Record<string, unknown> {
  id: string
  title: string
  description?: string
  output_filename?: string
  status: 'pending' | 'completed'
  stepIndex: number
  step: {
    id: string
    title: string
    description?: string
    context_dependencies: string[]
    context_output?: string
  }
}

export type OutputWorkflowNode = Node<OutputStepNodeData>

interface UseOutputPlanToFlowResult {
  nodes: OutputWorkflowNode[]
  edges: Edge[]
}

const NODE_DIMENSIONS = {
  step: { width: 320, height: 140 },
  terminal: { width: 80, height: 36 }
}

const DAGRE_CONFIG = {
  rankdir: 'TB',
  nodesep: 100,
  ranksep: 80,
  marginx: 50,
  marginy: 50
}

function layoutWithDagre(nodes: OutputWorkflowNode[], edges: Edge[]) {
  const g = new dagre.graphlib.Graph()
  g.setGraph(DAGRE_CONFIG)
  g.setDefaultEdgeLabel(() => ({}))

  nodes.forEach(node => {
    const dim = node.id === 'start' || node.id === 'end' ? NODE_DIMENSIONS.terminal : NODE_DIMENSIONS.step
    g.setNode(node.id, { width: dim.width, height: dim.height })
  })

  edges.forEach(edge => {
    g.setEdge(edge.source, edge.target)
  })

  dagre.layout(g)

  return {
    nodes: nodes.map(node => {
      const pos = g.node(node.id)
      const dim = node.id === 'start' || node.id === 'end' ? NODE_DIMENSIONS.terminal : NODE_DIMENSIONS.step
      return {
        ...node,
        position: {
          x: pos.x - dim.width / 2,
          y: pos.y - dim.height / 2
        }
      }
    }),
    edges
  }
}

export function useOutputPlanToFlow(plan: WorkflowOutputPlan | null): UseOutputPlanToFlowResult {
  return useMemo(() => {
    if (!plan?.step) {
      return { nodes: [], edges: [] }
    }

    const step = plan.step
    const stepId = step.id || 'final-output'

    const nodes: OutputWorkflowNode[] = [
      {
        id: 'start',
        type: 'start',
        position: { x: 0, y: 0 },
        data: {
          id: 'start',
          title: 'Start',
          status: 'completed',
          stepIndex: -1,
          step: {
            id: 'start',
            title: 'Start',
            context_dependencies: []
          }
        }
      },
      {
        id: stepId,
        type: 'step',
        position: { x: 0, y: 0 },
        data: {
          id: stepId,
          title: step.title || 'Final Report',
          description: step.instructions,
          output_filename: step.output_filename,
          status: step.enabled ? 'completed' : 'pending',
          stepIndex: 0,
          step: {
            id: stepId,
            title: step.title || 'Final Report',
            description: step.instructions,
            context_dependencies: [],
            context_output: step.output_filename
          }
        }
      },
      {
        id: 'end',
        type: 'end',
        position: { x: 0, y: 0 },
        data: {
          id: 'end',
          title: 'End',
          status: 'pending',
          stepIndex: -1,
          step: {
            id: 'end',
            title: 'End',
            context_dependencies: []
          }
        }
      }
    ]

    const edges: Edge[] = [
      {
        id: 'start-to-output',
        source: 'start',
        target: stepId,
        type: 'smoothstep',
        style: { stroke: '#6b7280', strokeWidth: 2 }
      },
      {
        id: 'output-to-end',
        source: stepId,
        target: 'end',
        type: 'smoothstep',
        style: { stroke: '#6b7280', strokeWidth: 2 }
      }
    ]

    return layoutWithDagre(nodes, edges)
  }, [plan])
}
