import { useMemo } from 'react'
import type { Node, Edge } from '@xyflow/react'
import dagre from 'dagre'
import type { EvaluationPlan, EvaluationStep } from '../../../services/api-types'

export interface EvaluationStepNodeData extends Record<string, unknown> {
  id: string
  title: string
  description?: string
  success_criteria?: string
  status: 'pending' | 'running' | 'completed' | 'failed'
  stepIndex: number
  step: EvaluationStep
  workspacePath?: string | null
  selectedRunFolder?: string
  // Mark as evaluation step for StepNode styling
  isEvaluationStep: boolean
}

// Reuse StepNode type but with evaluation data
export type EvaluationWorkflowNode = Node<EvaluationStepNodeData>

interface UseEvaluationPlanToFlowResult {
  nodes: EvaluationWorkflowNode[]
  edges: Edge[]
}

interface UseEvaluationPlanToFlowOptions {
  completedStepIndices?: number[] // 0-based
  workspacePath?: string | null
  selectedRunFolder?: string
}

// Node dimensions
const NODE_DIMENSIONS = {
  step: { width: 280, height: 120 },
  start: { width: 80, height: 36 },
  end: { width: 80, height: 36 }
}

const DAGRE_CONFIG = {
  rankdir: 'TB', // Top to bottom for eval flow
  nodesep: 100,
  ranksep: 80,
  marginx: 50,
  marginy: 50
}

function layoutWithDagre(nodes: EvaluationWorkflowNode[], edges: Edge[]) {
  const g = new dagre.graphlib.Graph()
  g.setGraph(DAGRE_CONFIG)
  g.setDefaultEdgeLabel(() => ({}))

  nodes.forEach(node => {
    const dim = node.id === 'start' || node.id === 'end' ? NODE_DIMENSIONS.start : NODE_DIMENSIONS.step
    g.setNode(node.id, { width: dim.width, height: dim.height })
  })

  edges.forEach(edge => {
    g.setEdge(edge.source, edge.target)
  })

  dagre.layout(g)

  const layoutedNodes = nodes.map(node => {
    const pos = g.node(node.id)
    const dim = node.id === 'start' || node.id === 'end' ? NODE_DIMENSIONS.start : NODE_DIMENSIONS.step
    return {
      ...node,
      position: {
        x: pos.x - dim.width / 2,
        y: pos.y - dim.height / 2
      }
    }
  })

  return { nodes: layoutedNodes, edges }
}

export function useEvaluationPlanToFlow(
  plan: EvaluationPlan | null,
  options: UseEvaluationPlanToFlowOptions = {}
): UseEvaluationPlanToFlowResult {
  const {
    completedStepIndices = [],
    workspacePath,
    selectedRunFolder
  } = options

  return useMemo(() => {
    if (!plan || !plan.steps) {
      return { nodes: [], edges: [] }
    }

    const nodes: EvaluationWorkflowNode[] = []
    const edges: Edge[] = []

    // Start Node
    nodes.push({
      id: 'start',
      type: 'start', // Reuse start node
      position: { x: 0, y: 0 },
      data: {
        id: 'start',
        title: 'Start',
        status: 'completed',
        stepIndex: -1,
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        step: {} as any,
        isEvaluationStep: true
      }
    })

    // Steps
    plan.steps.forEach((step, index) => {
      const isCompleted = completedStepIndices.includes(index)
      const status = isCompleted ? 'completed' : 'pending'
      
      nodes.push({
        id: step.id,
        type: 'step', // Reuse StepNode
        position: { x: 0, y: 0 },
        data: {
          id: step.id,
          title: step.title,
          description: step.description,
          success_criteria: step.success_criteria,
          status,
          stepIndex: index,
          step,
          workspacePath,
          selectedRunFolder,
          isEvaluationStep: true
        }
      })

      // Edge from previous node
      const prevId = index === 0 ? 'start' : plan.steps[index - 1].id
      edges.push({
        id: `${prevId}-to-${step.id}`,
        source: prevId,
        target: step.id,
        type: 'smoothstep',
        style: { stroke: '#6b7280', strokeWidth: 2 }
      })
    })

    // End Node
    if (plan.steps.length > 0) {
      const lastId = plan.steps[plan.steps.length - 1].id
      nodes.push({
        id: 'end',
        type: 'end', // Reuse end node
        position: { x: 0, y: 0 },
        data: {
          id: 'end',
          title: 'End',
          status: 'pending',
          stepIndex: -1,
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          step: {} as any,
          isEvaluationStep: true
        }
      })

      edges.push({
        id: `${lastId}-to-end`,
        source: lastId,
        target: 'end',
        type: 'smoothstep',
        style: { stroke: '#6b7280', strokeWidth: 2 }
      })
    } else {
      // Direct start -> end if no steps
      nodes.push({
        id: 'end',
        type: 'end',
        position: { x: 0, y: 0 },
        data: {
          id: 'end',
          title: 'End',
          status: 'pending',
          stepIndex: -1,
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          step: {} as any,
          isEvaluationStep: true
        }
      })
      edges.push({
        id: 'start-to-end',
        source: 'start',
        target: 'end',
        type: 'smoothstep',
        style: { stroke: '#6b7280', strokeWidth: 2 }
      })
    }

    return layoutWithDagre(nodes, edges)
  }, [plan, completedStepIndices, workspacePath, selectedRunFolder])
}
