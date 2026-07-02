import { useMemo, useRef, useEffect } from 'react'
import type { Node, Edge } from '@xyflow/react'
import dagre from 'dagre'
import type { PlanStep, PlanningResponse, AgentLLMConfig, ValidationSchema, RoutingRoute, MessageSequenceItem } from '../../../utils/stepConfigMatching'
import { isConditionalStep, isHumanInputStep, isTodoTaskStep, isRoutingStep, isMessageSequenceStep } from '../../../utils/stepConfigMatching'
import type { ChangeType, PlanChanges } from './usePlanData'
import type { VariablesManifest, EvaluationStep } from '../../../services/api-types'
import type { VariablesNodeData } from '../nodes/VariablesNode'
import { useActiveWorkflowPreset } from '../../../hooks/useActiveWorkflowPreset'
import { useLLMStore } from '../../../stores/useLLMStore'

const ROUTE_EDGE_LABEL_STYLE = { fill: 'hsl(var(--muted-foreground))', fontWeight: 500, fontSize: 10 }
const COMPLETION_EDGE_LABEL_STYLE = { fill: 'hsl(var(--primary))', fontWeight: 600, fontSize: 11 }
const EDGE_LABEL_BG_STYLE = { fill: 'hsl(var(--popover))', fillOpacity: 0.92 }
const ROUTING_EDGE_COLORS = ['#0f766e', '#2563eb', '#7c3aed', '#ea580c', '#0891b2']

// Node data types for our custom nodes
export interface StepNodeData extends Record<string, unknown> {
  id: string
  title: string
  description?: string
  success_criteria?: string
  status: 'pending' | 'running' | 'completed' | 'failed'
  stepIndex: number
  step: PlanStep
  changeType?: ChangeType  // Highlight type for visual feedback
  workspacePath?: string | null  // Workspace path for file opening
  selectedRunFolder?: string  // Selected iteration folder for file opening
  validation_schema?: ValidationSchema  // Validation schema from plan.json
  isEvaluationStep?: boolean  // True when rendered from evaluation_plan.json in the main flow
  // Sub-agent specific fields
  parentOrchestratorTitle?: string  // Title of parent orchestrator node (for sub-agents)
  routeName?: string  // Route name from orchestration_routes (for sub-agents)
  routeCondition?: string  // Condition from orchestration_routes (for sub-agents)
  isOrphan?: boolean  // True for orphan steps (workshop-only, not in main execution flow)
}

export interface ConditionalNodeData extends Record<string, unknown> {
  id: string
  title: string
  description?: string
  condition_question?: string
  condition_context?: string
  status: 'pending' | 'running' | 'completed' | 'failed' | 'evaluating' | 'decided_true' | 'decided_false'
  stepIndex: number
  step: PlanStep
  changeType?: ChangeType  // Highlight type for visual feedback
  workspacePath?: string | null  // Workspace path for file opening
  selectedRunFolder?: string  // Selected iteration folder for file opening
  validation_schema?: ValidationSchema  // Validation schema from plan.json
  isOrphan?: boolean  // True for orphan steps (workshop-only, not in main execution flow)
}

export interface TodoTaskNodeData extends Record<string, unknown> {
  id: string
  title: string
  todo_task_step?: PlanStep  // DEPRECATED: kept for backwards compat
  predefined_routes?: Array<{ route_id: string; route_name: string; condition: string; sub_agent_step?: PlanStep; orphan_step_ref?: string; context_to_pass?: string }>
  enable_generic_agent?: boolean
  status: 'pending' | 'running' | 'failed' | 'executing' | 'evaluating' | 'orchestrating' | 'completed'
  stepIndex: number
  step: PlanStep
  changeType?: ChangeType  // Highlight type for visual feedback
  workspacePath?: string | null  // Workspace path for file opening
  selectedRunFolder?: string  // Selected iteration folder for file opening
  validation_schema?: ValidationSchema  // Validation schema from plan.json (now flat on step)
  parentOrchestratorTitle?: string  // Title of parent orchestrator node (for nested todo sub-agents)
  routeName?: string  // Route name from orchestration/todo routes
  routeCondition?: string  // Condition from orchestration/todo routes
  isOrphan?: boolean  // True for orphan steps (workshop-only, not in main execution flow)
}

export interface HumanInputNodeData extends Record<string, unknown> {
  id: string
  title: string
  question?: string
  response_type?: string
  options?: string[]
  status: 'pending' | 'waiting' | 'completed'
  stepIndex: number
  step: PlanStep
  changeType?: ChangeType  // Highlight type for visual feedback
  workspacePath?: string | null  // Workspace path for file opening
  selectedRunFolder?: string  // Selected iteration folder for file opening
  isOrphan?: boolean  // True for orphan steps (workshop-only, not in main execution flow)
}

export interface RoutingStepNodeData extends Record<string, unknown> {
  id: string
  title: string
  routing_question?: string
  routes?: RoutingRoute[]
  status: 'pending' | 'running' | 'completed' | 'failed' | 'executing' | 'evaluating' | 'routed'
  stepIndex: number
  step: PlanStep
  changeType?: ChangeType
  workspacePath?: string | null
  selectedRunFolder?: string
  validation_schema?: ValidationSchema
  isOrphan?: boolean  // True for orphan steps (workshop-only, not in main execution flow)
}

export interface MessageSequenceNodeData extends Record<string, unknown> {
  id: string
  title: string
  description?: string
  items?: MessageSequenceItem[]   // Ordered queue of user_message / code / prevalidation / foreach items
  status: 'pending' | 'running' | 'completed' | 'failed' | 'executing'
  stepIndex: number
  step: PlanStep
  changeType?: ChangeType
  workspacePath?: string | null
  selectedRunFolder?: string
  validation_schema?: ValidationSchema
  isOrphan?: boolean  // True for orphan steps (workshop-only, not in main execution flow)
}

export interface ValidationNodeData extends Record<string, unknown> {
  id: string
  parentStepId: string
  parentStepTitle: string
  status: 'pending' | 'running' | 'passed' | 'failed'
  llmProvider?: string  // LLM provider (e.g., 'openai', 'bedrock')
  llmModel?: string  // LLM model name
}

export interface LearningNodeData extends Record<string, unknown> {
  id: string
  parentStepId: string
  parentStepTitle: string
  status: 'pending' | 'running' | 'completed' | 'skipped'
  llmProvider?: string  // LLM provider (e.g., 'openai', 'bedrock')
  llmModel?: string  // LLM model name
}

export interface EvaluationNodeData extends Record<string, unknown> {
  id: string
  parentStepId: string
  parentStepTitle: string
  evaluationQuestion?: string
  status: 'pending' | 'running' | 'evaluated_true' | 'evaluated_false'
  llmProvider?: string  // LLM provider (e.g., 'openai', 'bedrock')
  llmModel?: string  // LLM model name
}

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
  isEvaluationStep: boolean
}

export interface WorkflowArtifactNodeData extends Record<string, unknown> {
  id: string
  title: string
  description?: string
  kind: 'evaluation' | 'output'
  configured: boolean
  detail?: string
}

export type WorkflowNodeData = StepNodeData | ConditionalNodeData | TodoTaskNodeData | HumanInputNodeData | RoutingStepNodeData | ValidationNodeData | LearningNodeData | EvaluationNodeData | VariablesNodeData | EvaluationStepNodeData | WorkflowArtifactNodeData

// Node and edge types
export type WorkflowNode = Node<WorkflowNodeData>
export type WorkflowEdge = Edge

interface UsePlanToFlowResult {
  nodes: WorkflowNode[]
  edges: WorkflowEdge[]
}

interface UsePlanToFlowOptions {
  showDependencyEdges?: boolean // Default: false (hide dependency edges for cleaner view)
  changes?: PlanChanges | null  // Optional: highlight changes on nodes
  completedStepIndices?: number[]  // 0-based indices of completed steps (from steps_done.json)
  stepStatusMap?: Map<string, 'pending' | 'running' | 'completed' | 'failed'> | Record<string, 'pending' | 'running' | 'completed' | 'failed'> | null  // Step status from events (Map or serialized object for stable comparison)
  workspacePath?: string | null  // Workspace path for file opening
  selectedRunFolder?: string  // Selected iteration folder for file opening
  variablesManifest?: VariablesManifest | null  // Variables manifest for Variables node
  onOpenVariablesSidebar?: () => void  // Callback for opening variables sidebar
  isLoadingVariables?: boolean  // Whether variables are being loaded
  layoutDirection?: 'LR' | 'TB'  // Layout direction: 'LR' = horizontal, 'TB' = vertical
  disabled?: boolean  // Keep the last computed graph and skip layout work when hidden
}

// Dagre layout configuration - returns config based on layout direction
const getDagreConfig = (direction: 'LR' | 'TB') => ({
  rankdir: direction,
  // For LR: nodesep = vertical spacing, ranksep = horizontal spacing
  // For TB: nodesep = horizontal spacing, ranksep = vertical spacing
  // TB nodesep is the MINIMUM horizontal gap between sibling nodes; dagre adds
  // more automatically so each branch gets room proportional to its own subtree.
  // Keep this a modest floor and let dagre drive the dynamic, per-subtree spread.
  nodesep: direction === 'LR' ? 200 : 160,
  ranksep: direction === 'LR' ? 150 : 220,
  marginx: 80,
  marginy: 80
})

// Node dimensions for layout calculation (base dimensions) - smaller since content is simplified
const NODE_DIMENSIONS = {
  step: { width: 280, height: 120 },
  conditional: { width: 240, height: 100 },
  routing: { width: 280, height: 200 },
  message_sequence: { width: 320, height: 240 },
  todo_task: { width: 300, height: 120 },
  human_input: { width: 260, height: 120 },
  loop: { width: 300, height: 140 },
  start: { width: 96, height: 40 },
  end: { width: 96, height: 40 },
  variables: { width: 220, height: 120 },
  'workflow-artifact': { width: 220, height: 120 }
}

const SUB_AGENT_LAYOUT = {
  parentGap: 72,
  cellGap: 32,
  cellWidth: Math.max(NODE_DIMENSIONS.step.width, NODE_DIMENSIONS.todo_task.width),
  cellHeight: NODE_DIMENSIONS.step.height
}

const ROUTING_TREE_LAYOUT = {
  parentGap: 120,
  laneGap: 180,
  minLaneWidth: NODE_DIMENSIONS.routing.width
}

function getSubAgentGridMetrics(count: number, direction: 'LR' | 'TB') {
  if (count <= 0) {
    return { columns: 0, rows: 0, width: 0, height: 0 }
  }

  const verticalColumns = count <= 2 ? count : 2
  const columns = direction === 'TB'
    ? verticalColumns
    : Math.ceil(count / (count >= 5 ? 2 : 1))
  const rows = direction === 'TB'
    ? Math.ceil(count / verticalColumns)
    : (count >= 5 ? 2 : 1)

  return {
    columns,
    rows,
    width: (columns * SUB_AGENT_LAYOUT.cellWidth) + ((columns - 1) * SUB_AGENT_LAYOUT.cellGap),
    height: (rows * SUB_AGENT_LAYOUT.cellHeight) + ((rows - 1) * SUB_AGENT_LAYOUT.cellGap)
  }
}

// countOrphanStepRefs walks the plan and counts how many todo_task routes
// reference each orphan step via `orphan_step_ref`. Used to show on an orphan
// node whether (and how often) it is reused as a shared sub-agent definition.
function countOrphanStepRefs(steps: PlanStep[] | undefined): Map<string, number> {
  const counts = new Map<string, number>()
  const visit = (list?: PlanStep[]) => {
    if (!list) return
    for (const s of list) {
      if (isTodoTaskStep(s) && Array.isArray(s.predefined_routes)) {
        for (const r of s.predefined_routes) {
          if (r.orphan_step_ref) counts.set(r.orphan_step_ref, (counts.get(r.orphan_step_ref) || 0) + 1)
          if (r.sub_agent_step) visit([r.sub_agent_step])
        }
      }
    }
  }
  visit(steps)
  return counts
}

function textReferencesKnowledgebase(text: string): boolean {
  return /\bknowledgebase[\\/]/i.test(text)
}

function textReferencesDatabase(text: string): boolean {
  return /\$DB_PATH\b|\bdb[\\/]|db\.sqlite|\bsqlite3?\b/i.test(text)
}

/**
 * Estimate node height based on content
 * Simplified version - nodes no longer show description, success criteria, or validation schema
 * Only accounts for: context files, loop conditions
 */
function estimateNodeHeight(node: WorkflowNode): number {
  const baseDimensions = NODE_DIMENSIONS[node.type as keyof typeof NODE_DIMENSIONS] || NODE_DIMENSIONS.step
  let estimatedHeight = baseDimensions.height

  // Get node data
  const data = node.data as StepNodeData | ConditionalNodeData | Record<string, unknown>

  // Base height components (header, padding, footer) - simplified
  const headerHeight = 60 // Header with buttons
  const footerHeight = 40 // Config footer
  const padding = 16 // Top and bottom padding

  // Content height estimation - only for context files
  let contentHeight = 0

  // Step metadata and context files
  if ('step' in data && data.step && typeof data.step === 'object') {
    const step = data.step as PlanStep

    const learningConfig = step.agent_configs
    const learningObjective = typeof learningConfig?.learning_objective === 'string'
      ? learningConfig.learning_objective.trim()
      : ''
    const knowledgebaseContribution = typeof learningConfig?.knowledgebase_contribution === 'string'
      ? learningConfig.knowledgebase_contribution.trim()
      : ''
    const learningAccess = learningConfig?.learnings_access
    const knowledgebaseAccess = learningConfig?.knowledgebase_access
    const dbAccess = learningConfig?.db_access
    const executionMode = typeof learningConfig?.declared_execution_mode === 'string'
      ? learningConfig.declared_execution_mode.trim()
      : ''
    const contextInputs = Array.isArray(step.context_dependencies) ? step.context_dependencies : []
    const contextOutput = step.context_output
    const contextOutputs = Array.isArray(contextOutput)
      ? contextOutput
      : (contextOutput ? [contextOutput] : [])
    const validationFiles = step.validation_schema?.files?.map(file => file.file_name) || []
    const referenceText = [
      step.description,
      step.success_criteria,
      ...contextInputs,
      ...contextOutputs,
      ...validationFiles
    ].filter((value): value is string => typeof value === 'string' && value.length > 0).join('\n')
    const referencesKnowledgebase = textReferencesKnowledgebase(referenceText)
    const referencesDatabase = textReferencesDatabase(referenceText)
    const hasLearningMetadata = (
      learningObjective.length > 0 ||
      learningConfig?.lock_learnings === true ||
      learningAccess === 'read' ||
      learningAccess === 'read-write' ||
      learningAccess === 'none'
    )
    const hasKnowledgebaseMetadata = (
      knowledgebaseContribution.length > 0 ||
      knowledgebaseAccess === 'read' ||
      knowledgebaseAccess === 'write' ||
      knowledgebaseAccess === 'read-write' ||
      knowledgebaseAccess === 'none' ||
      referencesKnowledgebase
    )
    const hasDbMetadata = dbAccess === 'read' || dbAccess === 'write' || dbAccess === 'read-write' || dbAccess === 'none' || referencesDatabase
    const metadataRowCount = [hasLearningMetadata, hasKnowledgebaseMetadata, hasDbMetadata].filter(Boolean).length
    if (metadataRowCount > 0) {
      contentHeight += (hasLearningMetadata ? 44 : 0) + (hasKnowledgebaseMetadata ? 44 : 0) + (hasDbMetadata ? 28 : 0) +
        Math.max(0, metadataRowCount - 1) * 6 + 8
      if (learningObjective.length > 120) contentHeight += 10
      if (knowledgebaseContribution.length > 120) contentHeight += 10
    }

    if (contextInputs.length > 0 || contextOutputs.length > 0) {
      const totalFiles = contextInputs.length + contextOutputs.length
      contentHeight += 20 + (totalFiles * 20) + 8 // Base + per file + spacing
    }

    if (executionMode.length > 0) {
      contentHeight += 22
    }
  }

  // For message_sequence nodes, add height for title, badges, and item rows
  if (node.type === 'message_sequence') {
    const messageData = data as MessageSequenceNodeData
    const seqItems = messageData.items || []
    const visibleCount = Math.min(seqItems.length, 6)
    const hiddenCount = seqItems.length - visibleCount
    const hasStoreBadges = seqItems.some(item => {
      const itemReferenceText = [
        item.title,
        item.message,
        item.script_path,
        item.source,
        ...(item.input_files || []),
        ...(item.output_files || [])
      ].filter((value): value is string => typeof value === 'string' && value.length > 0).join('\n')

      return (
        item.write_access?.db === true ||
        item.write_access?.knowledgebase === true ||
        item.write_access?.learnings === true ||
        item.kind === 'db' ||
        item.kind === 'knowledgebase' ||
        textReferencesDatabase(itemReferenceText) ||
        textReferencesKnowledgebase(itemReferenceText)
      )
    })

    contentHeight += 35
    if (messageData.description) contentHeight += 20
    if (hasStoreBadges) contentHeight += 24
    contentHeight += 20 + Math.max(visibleCount, 1) * 24
    if (hiddenCount > 0) contentHeight += 18
  }

  // For todo_task nodes, add height for predefined routes and generic agent indicator
  if (node.type === 'todo_task') {
    const todoData = data as TodoTaskNodeData
    if (todoData.predefined_routes && todoData.predefined_routes.length > 0) {
      contentHeight += 36 + Math.min(todoData.predefined_routes.length, 4) * 32
    }
    const step = todoData.step
    if (step && 'messages' in step && Array.isArray(step.messages) && step.messages.length > 0) {
      contentHeight += 30 + Math.min(step.messages.length, 20) * 28
    }
    if (todoData.enable_generic_agent) {
      contentHeight += 20
    }
  }

  // For routing nodes, add height for routing question + route labels
  if (node.type === 'routing') {
    const routingData = data as RoutingStepNodeData
    if (routingData.routing_question) {
      contentHeight += 40 // routing question box
    }
    contentHeight += 25 // route count badge
    if (routingData.routes && routingData.routes.length > 0) {
      contentHeight += (routingData.routes.length * 34) + 12
    }
  }

  // Calculate total estimated height
  estimatedHeight = headerHeight + padding + contentHeight + footerHeight

  // Add safety margin (20% extra) - reduced from 40% since nodes are simpler
  estimatedHeight = Math.ceil(estimatedHeight * 1.2)

  // Ensure minimum height
  estimatedHeight = Math.max(estimatedHeight, baseDimensions.height)

  return estimatedHeight
}

function getNodeLayoutDimensions(node: WorkflowNode): { width: number; height: number } {
  const baseDimensions = NODE_DIMENSIONS[node.type as keyof typeof NODE_DIMENSIONS] || NODE_DIMENSIONS.step
  return {
    width: baseDimensions.width,
    height: Math.max(baseDimensions.height, estimateNodeHeight(node))
  }
}

function getImmediateSubAgentParentId(nodeId: string, parentIds: Set<string>): string | null {
  if (!nodeId.includes('-sub-agent-')) {
    return null
  }

  return Array.from(parentIds)
    .filter(parentId => nodeId.startsWith(`${parentId}-sub-agent-`))
    .sort((a, b) => b.length - a.length)[0] || null
}

function getSubAgentGridMetricsFromDimensions(dimensions: Array<{ width: number; height: number }>, direction: 'LR' | 'TB') {
  const count = dimensions.length
  const base = getSubAgentGridMetrics(count, direction)
  if (count <= 0) {
    return { ...base, columnWidths: [], rowHeights: [] }
  }

  const columnWidths = Array.from({ length: base.columns }, (_, column) => {
    return dimensions.reduce((max, dims, index) => {
      return index % base.columns === column ? Math.max(max, dims.width) : max
    }, SUB_AGENT_LAYOUT.cellWidth)
  })
  const rowHeights = Array.from({ length: base.rows }, (_, row) => {
    return dimensions.reduce((max, dims, index) => {
      return Math.floor(index / base.columns) === row ? Math.max(max, dims.height) : max
    }, SUB_AGENT_LAYOUT.cellHeight)
  })
  const width = columnWidths.reduce((sum, value) => sum + value, 0) + Math.max(0, base.columns - 1) * SUB_AGENT_LAYOUT.cellGap
  const height = rowHeights.reduce((sum, value) => sum + value, 0) + Math.max(0, base.rows - 1) * SUB_AGENT_LAYOUT.cellGap

  return {
    ...base,
    width,
    height,
    columnWidths,
    rowHeights
  }
}

function getNodeFootprintDimensions(
  node: WorkflowNode,
  allNodes: WorkflowNode[],
  parentIds: Set<string>,
  direction: 'LR' | 'TB',
  visited: Set<string> = new Set()
): { width: number; height: number } {
  const ownDimensions = getNodeLayoutDimensions(node)
  if (visited.has(node.id) || node.type !== 'todo_task') {
    return ownDimensions
  }

  const nextVisited = new Set(visited)
  nextVisited.add(node.id)

  const childFootprints = allNodes
    .filter(candidate => getImmediateSubAgentParentId(candidate.id, parentIds) === node.id)
    .map(child => getNodeFootprintDimensions(child, allNodes, parentIds, direction, nextVisited))

  if (childFootprints.length === 0) {
    return ownDimensions
  }

  const childGrid = getSubAgentGridMetricsFromDimensions(childFootprints, direction)
  if (direction === 'TB') {
    return {
      width: Math.max(ownDimensions.width, childGrid.width),
      height: ownDimensions.height + SUB_AGENT_LAYOUT.parentGap + childGrid.height
    }
  }

  return {
    width: ownDimensions.width + SUB_AGENT_LAYOUT.parentGap + childGrid.width,
    height: Math.max(ownDimensions.height, childGrid.height)
  }
}

function getRoutingChildren(nodeId: string, nodes: WorkflowNode[], edges: WorkflowEdge[]): string[] {
  const node = nodes.find(candidate => candidate.id === nodeId)
  if (node?.type === 'routing') {
    const step = (node.data as RoutingStepNodeData).step
    if (step && isRoutingStep(step)) {
      const stepIdToNodeId = new Map<string, string>()
      nodes.forEach(candidate => {
        const candidateStep = (candidate.data as { step?: PlanStep }).step
        if (candidateStep?.id) {
          stepIdToNodeId.set(candidateStep.id, candidate.id)
        }
      })

      const seenTargets = new Set<string>()
      return step.routes
        .map(route => stepIdToNodeId.get(route.next_step_id))
        .filter((targetId): targetId is string => {
          if (!targetId || seenTargets.has(targetId)) return false
          seenTargets.add(targetId)
          return true
        })
    }
  }

  const children: string[] = []
  const seen = new Set<string>()

  edges.forEach(edge => {
    if (
      edge.source === nodeId &&
      typeof edge.sourceHandle === 'string' &&
      edge.sourceHandle.startsWith('route-') &&
      edge.target !== 'end' &&
      !seen.has(edge.target)
    ) {
      seen.add(edge.target)
      children.push(edge.target)
    }
  })

  return children
}

function getWorkflowBranchFootprint(
  nodeId: string,
  nodes: WorkflowNode[],
  edges: WorkflowEdge[],
  todoParentIds: Set<string>,
  visited: Set<string> = new Set()
): { width: number; height: number } {
  const node = nodes.find(candidate => candidate.id === nodeId)
  if (!node || visited.has(nodeId)) {
    return { width: 0, height: 0 }
  }

  const nextVisited = new Set(visited)
  nextVisited.add(nodeId)
  const ownFootprint = getNodeFootprintDimensions(node, nodes, todoParentIds, 'TB')
  if (node.type !== 'routing') {
    return ownFootprint
  }

  const childFootprints = getRoutingChildren(node.id, nodes, edges)
    .map(childId => getWorkflowBranchFootprint(childId, nodes, edges, todoParentIds, nextVisited))
    .filter(footprint => footprint.width > 0 && footprint.height > 0)

  if (childFootprints.length === 0) {
    return ownFootprint
  }

  const childrenWidth = childFootprints.reduce((sum, footprint) => sum + footprint.width, 0) +
    Math.max(0, childFootprints.length - 1) * SUB_AGENT_LAYOUT.cellGap
  const childrenHeight = Math.max(...childFootprints.map(footprint => footprint.height))

  return {
    width: Math.max(ownFootprint.width, childrenWidth),
    height: ownFootprint.height + SUB_AGENT_LAYOUT.parentGap + childrenHeight
  }
}

function applyVerticalRoutingTreeLayout(nodes: WorkflowNode[], edges: WorkflowEdge[]): WorkflowNode[] {
  const positionedNodes = nodes.map(node => ({ ...node, position: { ...node.position } }))
  const nodeIndexById = new Map(positionedNodes.map((node, index) => [node.id, index]))
  const nodeById = new Map(positionedNodes.map(node => [node.id, node]))
  const todoParentIds = new Set(positionedNodes.filter(node => node.type === 'todo_task').map(node => node.id))
  const incomingEdgesByTarget = new Map<string, WorkflowEdge[]>()
  const outgoingEdgesBySource = new Map<string, WorkflowEdge[]>()

  edges.forEach(edge => {
    const incomingEdges = incomingEdgesByTarget.get(edge.target) || []
    incomingEdges.push(edge)
    incomingEdgesByTarget.set(edge.target, incomingEdges)

    const outgoingEdges = outgoingEdgesBySource.get(edge.source) || []
    outgoingEdges.push(edge)
    outgoingEdgesBySource.set(edge.source, outgoingEdges)
  })

  const routedTargets = new Set(
    positionedNodes
      .filter(node => node.type === 'routing')
      .flatMap(node => getRoutingChildren(node.id, positionedNodes, edges))
  )

  const setNodePosition = (nodeId: string, x: number, y: number) => {
    const nodeIndex = nodeIndexById.get(nodeId)
    if (nodeIndex === undefined) return
    const nextNode = {
      ...positionedNodes[nodeIndex],
      position: { x, y }
    }
    positionedNodes[nodeIndex] = nextNode
    nodeById.set(nodeId, nextNode)
  }

  const isSimpleRoutingTreeRoot = (nodeId: string, visited: Set<string> = new Set()): boolean => {
    if (visited.has(nodeId)) return false
    const node = nodeById.get(nodeId)
    if (!node || node.type !== 'routing') return false

    const nextVisited = new Set(visited)
    nextVisited.add(nodeId)
    const childIds = getRoutingChildren(nodeId, positionedNodes, edges)
    if (childIds.length === 0) return false

    return childIds.every(childId => {
      const child = nodeById.get(childId)
      if (!child) return false

      const nonParentIncomingEdges = (incomingEdgesByTarget.get(childId) || [])
        .filter(edge => edge.source !== nodeId)
      if (nonParentIncomingEdges.length > 0) {
        return false
      }

      const nonEndOutgoingEdges = (outgoingEdgesBySource.get(childId) || [])
        .filter(edge => edge.target !== 'end')

      if (child.type === 'routing') {
        return isSimpleRoutingTreeRoot(childId, nextVisited)
      }

      return nonEndOutgoingEdges.length === 0
    })
  }

  const placeBranch = (nodeId: string, left: number, top: number, visited: Set<string> = new Set()) => {
    const nodeIndex = nodeIndexById.get(nodeId)
    if (nodeIndex === undefined || visited.has(nodeId)) return

    const node = positionedNodes[nodeIndex]
    const nextVisited = new Set(visited)
    nextVisited.add(nodeId)
    const footprint = getWorkflowBranchFootprint(nodeId, positionedNodes, edges, todoParentIds, visited)
    const ownFootprint = getNodeFootprintDimensions(node, positionedNodes, todoParentIds, 'TB')
    const ownDimensions = getNodeLayoutDimensions(node)
    const nodeLeft = left + (footprint.width - ownFootprint.width) / 2 + (ownFootprint.width - ownDimensions.width) / 2
    setNodePosition(nodeId, nodeLeft, top)

    if (node.type !== 'routing') return

    const childIds = getRoutingChildren(node.id, positionedNodes, edges)
    if (childIds.length === 0) return

    const childFootprints = childIds.map(childId =>
      getWorkflowBranchFootprint(childId, positionedNodes, edges, todoParentIds, nextVisited)
    )
    const totalChildrenWidth = childFootprints.reduce((sum, childFootprint) => sum + childFootprint.width, 0) +
      Math.max(0, childFootprints.length - 1) * SUB_AGENT_LAYOUT.cellGap
    let childLeft = left + (footprint.width - totalChildrenWidth) / 2
    const childTop = top + ownFootprint.height + SUB_AGENT_LAYOUT.parentGap

    childIds.forEach((childId, index) => {
      placeBranch(childId, childLeft, childTop, nextVisited)
      childLeft += childFootprints[index].width + SUB_AGENT_LAYOUT.cellGap
    })
  }

  positionedNodes
    .filter(node => (
      node.type === 'routing' &&
      !routedTargets.has(node.id) &&
      isSimpleRoutingTreeRoot(node.id)
    ))
    .forEach(node => {
      const footprint = getWorkflowBranchFootprint(node.id, positionedNodes, edges, todoParentIds)
      const nodeDimensions = getNodeLayoutDimensions(node)
      const left = node.position.x + (nodeDimensions.width / 2) - (footprint.width / 2)
      placeBranch(node.id, left, node.position.y)
    })

  const syncNodeById = (nodeId: string) => {
    const nodeIndex = nodeIndexById.get(nodeId)
    if (nodeIndex === undefined) return
    nodeById.set(nodeId, positionedNodes[nodeIndex])
  }

  const getControlOutgoingEdges = (nodeId: string): WorkflowEdge[] => (
    (outgoingEdgesBySource.get(nodeId) || []).filter(edge => (
      edge.target !== 'end' &&
      edge.target !== 'start' &&
      edge.target !== 'variables' &&
      !edge.id.startsWith('dep-') &&
      nodeById.has(edge.target)
    ))
  )

  const collectReachableControlNodes = (startId: string): Set<string> => {
    const reachable = new Set<string>()
    const stack = [startId]

    while (stack.length > 0) {
      const nodeId = stack.pop()
      if (!nodeId || reachable.has(nodeId) || !nodeById.has(nodeId)) continue

      reachable.add(nodeId)
      getControlOutgoingEdges(nodeId).forEach(edge => {
        if (!reachable.has(edge.target)) {
          stack.push(edge.target)
        }
      })
    }

    return reachable
  }

  const getBoundsForIds = (nodeIds: string[]) => {
    if (nodeIds.length === 0) return null

    let left = Number.POSITIVE_INFINITY
    let top = Number.POSITIVE_INFINITY
    let right = Number.NEGATIVE_INFINITY
    let bottom = Number.NEGATIVE_INFINITY

    nodeIds.forEach(nodeId => {
      const node = nodeById.get(nodeId)
      if (!node) return
      const dimensions = getNodeLayoutDimensions(node)
      left = Math.min(left, node.position.x)
      top = Math.min(top, node.position.y)
      right = Math.max(right, node.position.x + dimensions.width)
      bottom = Math.max(bottom, node.position.y + dimensions.height)
    })

    if (!Number.isFinite(left) || !Number.isFinite(top) || !Number.isFinite(right) || !Number.isFinite(bottom)) {
      return null
    }

    return {
      left,
      top,
      right,
      bottom,
      width: right - left,
      height: bottom - top
    }
  }

  const shiftNodes = (nodeIds: string[], dx: number, dy: number) => {
    if (dx === 0 && dy === 0) return

    nodeIds.forEach(nodeId => {
      const nodeIndex = nodeIndexById.get(nodeId)
      if (nodeIndex === undefined) return

      positionedNodes[nodeIndex] = {
        ...positionedNodes[nodeIndex],
        position: {
          x: positionedNodes[nodeIndex].position.x + dx,
          y: positionedNodes[nodeIndex].position.y + dy
        }
      }
      syncNodeById(nodeId)
    })
  }

  positionedNodes
    .filter(node => node.type === 'routing')
    .sort((a, b) => a.position.y - b.position.y)
    .forEach(routingNode => {
      const currentRoutingNode = nodeById.get(routingNode.id) || routingNode
      const routeChildIds = getRoutingChildren(currentRoutingNode.id, positionedNodes, edges)
        .filter(childId => nodeById.has(childId))
      if (routeChildIds.length <= 1) return

      const reachableByRoute = routeChildIds.map(childId => ({
        childId,
        nodeIds: collectReachableControlNodes(childId)
      }))
      const routeCountByNode = new Map<string, number>()

      reachableByRoute.forEach(route => {
        route.nodeIds.forEach(nodeId => {
          routeCountByNode.set(nodeId, (routeCountByNode.get(nodeId) || 0) + 1)
        })
      })

      const lanes = reachableByRoute
        .map(route => {
          const privateNodeIds = Array.from(route.nodeIds)
            .filter(nodeId => routeCountByNode.get(nodeId) === 1 && nodeId !== currentRoutingNode.id)
            .sort((a, b) => {
              const nodeA = nodeById.get(a)
              const nodeB = nodeById.get(b)
              if (!nodeA || !nodeB) return 0
              if (Math.abs(nodeA.position.y - nodeB.position.y) > 8) {
                return nodeA.position.y - nodeB.position.y
              }
              return nodeA.position.x - nodeB.position.x
            })
          const bounds = getBoundsForIds(privateNodeIds)

          if (!bounds) return null

          return {
            childId: route.childId,
            nodeIds: privateNodeIds,
            bounds,
            laneWidth: Math.max(bounds.width, ROUTING_TREE_LAYOUT.minLaneWidth)
          }
        })
        .filter((lane): lane is NonNullable<typeof lane> => Boolean(lane))

      if (lanes.length <= 1) return

      const routingDimensions = getNodeLayoutDimensions(currentRoutingNode)
      const routingCenterX = currentRoutingNode.position.x + routingDimensions.width / 2
      const routingBottom = currentRoutingNode.position.y + routingDimensions.height
      const totalLaneWidth = lanes.reduce((sum, lane) => sum + lane.laneWidth, 0) +
        Math.max(0, lanes.length - 1) * ROUTING_TREE_LAYOUT.laneGap
      let laneLeft = routingCenterX - totalLaneWidth / 2
      const childTop = routingBottom + ROUTING_TREE_LAYOUT.parentGap

      if (import.meta.env.DEV) {
        console.log('[WorkflowRouteTreeLayout] routing lanes', {
          routingNodeId: currentRoutingNode.id,
          routeChildIds,
          childTop,
          totalLaneWidth,
          lanes: lanes.map(lane => ({
            childId: lane.childId,
            nodeIds: lane.nodeIds,
            laneWidth: lane.laneWidth,
            bounds: lane.bounds
          }))
        })
      }

      lanes.forEach(lane => {
        const childNode = nodeById.get(lane.childId)
        if (!childNode) return

        const targetLeft = laneLeft + (lane.laneWidth - lane.bounds.width) / 2
        const dx = targetLeft - lane.bounds.left
        const dy = childTop - childNode.position.y
        shiftNodes(lane.nodeIds, dx, dy)
        laneLeft += lane.laneWidth + ROUTING_TREE_LAYOUT.laneGap
      })
    })

  return positionedNodes
}

/**
 * Calculate topology metrics to adjust layout spacing
 */
function calculateTopologyMetrics(nodes: WorkflowNode[]): { hasOrchestrator: boolean; maxOrchestratorDepth: number; maxOrchestratorSubAgents: number; maxRoutingBranches: number } {
  let hasOrchestrator = false
  let maxOrchestratorDepth = 0
  let maxOrchestratorSubAgents = 0
  let maxRoutingBranches = 0

  nodes.forEach(node => {
    if (node.type === 'todo_task') {
      hasOrchestrator = true
      const data = node.data as TodoTaskNodeData
      const routes = (data as TodoTaskNodeData).predefined_routes
      const numRoutes = routes?.length || 0

      // Count actual sub-agents
      maxOrchestratorSubAgents = Math.max(maxOrchestratorSubAgents, numRoutes)

      maxOrchestratorDepth = Math.max(maxOrchestratorDepth, numRoutes)
    }
    if (node.type === 'routing') {
      const routes = (node.data as RoutingStepNodeData).routes
      maxRoutingBranches = Math.max(maxRoutingBranches, routes?.length || 0)
    }
  })

  return { hasOrchestrator, maxOrchestratorDepth, maxOrchestratorSubAgents, maxRoutingBranches }
}

/**
 * Reposition branch nodes for conditional and decision nodes
 * Uses Subtree Translation: Moves the entire branch structure together to preserve relative layout.
 * - Finds the branch root (e.g. -true-0)
 * - Calculates delta to move root to desired position
 * - Applies delta to ALL nodes in that branch (descendants)
 */
function positionBranchNodes(nodes: WorkflowNode[], direction: 'LR' | 'TB'): WorkflowNode[] {
  const adjustedNodes = [...nodes]
  const nodeMap = new Map(adjustedNodes.map((n, i) => [n.id, i]))

  // Find branching nodes (conditional/decision)
  const branchingNodes = adjustedNodes.filter(n =>
    n.type === 'conditional'
  )

  branchingNodes.forEach(parent => {
    // Get up-to-date parent node
    const parentIndex = nodeMap.get(parent.id)
    if (parentIndex === undefined) return
    const currentParent = adjustedNodes[parentIndex]

    const parentDims = NODE_DIMENSIONS[currentParent.type as keyof typeof NODE_DIMENSIONS] || NODE_DIMENSIONS.step
    const parentCenterX = currentParent.position.x + parentDims.width / 2
    const parentCenterY = currentParent.position.y + parentDims.height / 2

    // Check for todo_task nodes in branches to determine spacing
    // We look at all nodes in the branch to be safe, or just direct children?
    // Looking at all nodes in branch is better for spacing safety.
    const trueBranchPrefix = `${currentParent.id}-true-`
    const falseBranchPrefix = `${currentParent.id}-false-`
    
    const trueHasOrchestrator = adjustedNodes.some(n => n.id.startsWith(trueBranchPrefix) && n.type === 'todo_task')
    const falseHasOrchestrator = adjustedNodes.some(n => n.id.startsWith(falseBranchPrefix) && n.type === 'todo_task')

    // Base offset
    let branchOffset = 200
    if (trueHasOrchestrator || falseHasOrchestrator) {
      branchOffset = 350
    }

    // Helper to shift a branch subtree
    const shiftBranch = (prefix: string, offsetMult: number) => {
      // Find root of the branch (index 0)
      const rootId = `${prefix}0`
      const rootIndex = nodeMap.get(rootId)
      
      if (rootIndex !== undefined) {
        const rootNode = adjustedNodes[rootIndex]
        const rootDims = NODE_DIMENSIONS[rootNode.type as keyof typeof NODE_DIMENSIONS] || NODE_DIMENSIONS.step
        
        let dx = 0, dy = 0
        
        if (direction === 'LR') {
          // LR: Shift Y. Target Y is parentCenter +/- offset
          const targetY = parentCenterY + (branchOffset * offsetMult) - (rootDims.height / 2)
          dy = targetY - rootNode.position.y
        } else {
          // TB: Shift X. Target X is parentCenter +/- offset
          const targetX = parentCenterX + (branchOffset * offsetMult) - (rootDims.width / 2)
          dx = targetX - rootNode.position.x
        }
        
        // Apply shift to ALL nodes in this branch
        if (dx !== 0 || dy !== 0) {
          adjustedNodes.forEach((n, i) => {
            if (n.id.startsWith(prefix)) {
              adjustedNodes[i] = {
                ...n,
                position: {
                  x: n.position.x + dx,
                  y: n.position.y + dy
                }
              }
            }
          })
        }
    }
    }

    // Shift True Branch (Offset Multiplier: -1 for Top/Left)
    shiftBranch(trueBranchPrefix, -1)

    // Shift False Branch (Offset Multiplier: 1 for Bottom/Right)
    shiftBranch(falseBranchPrefix, 1)
  })

  return adjustedNodes
}

/**
 * Global collision detection and resolution
 * Detects overlaps between all nodes and resolves them by shifting nodes
 * This handles overlaps from todo_task sub-agents, conditional branches, loops, etc.
 * For LR layout: prefer vertical shifts. For TB layout: prefer horizontal shifts.
 */
function detectAndResolveCollisions(nodes: WorkflowNode[], direction: 'LR' | 'TB'): WorkflowNode[] {
  const MIN_SEPARATION = 40 // Restored to 40 to prevent overlaps
  const adjustedNodes = [...nodes]

  // Get bounding box for a node (using estimated height based on content)
  const getBounds = (node: WorkflowNode): { left: number; right: number; top: number; bottom: number } => {
    const dimensions = getNodeLayoutDimensions(node)
    return {
      left: node.position.x,
      right: node.position.x + dimensions.width,
      top: node.position.y,
      bottom: node.position.y + dimensions.height
    }
  }

  // Check if two bounding boxes overlap (with minimum separation)
  const boxesOverlap = (
    a: { left: number; right: number; top: number; bottom: number },
    b: { left: number; right: number; top: number; bottom: number }
  ): boolean => {
    // Calculate overlap area (positive if overlapping, negative if separated)
    const horizontalOverlap = Math.min(a.right, b.right) - Math.max(a.left, b.left)
    const verticalOverlap = Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top)

    // Overlap if both dimensions have positive overlap (boxes intersect)
    // Also check if they're too close (within MIN_SEPARATION)
    if (horizontalOverlap > 0 && verticalOverlap > 0) {
      return true // Full overlap
    }

    // Check if boxes are too close (within MIN_SEPARATION) even if not overlapping
    const hDistance = horizontalOverlap < 0 ? -horizontalOverlap : 0
    const vDistance = verticalOverlap < 0 ? -verticalOverlap : 0

    // If boxes are close horizontally and vertically, they need more separation
    return (hDistance < MIN_SEPARATION && vDistance < MIN_SEPARATION) ||
      (horizontalOverlap > 0 && vDistance < MIN_SEPARATION) ||
      (verticalOverlap > 0 && hDistance < MIN_SEPARATION)
  }

  // Calculate how much to shift node 'a' to resolve overlap with node 'b'
  // For LR layout: prefer vertical shifts. For TB layout: prefer horizontal shifts.
  const calculateShift = (
    a: { left: number; right: number; top: number; bottom: number },
    b: { left: number; right: number; top: number; bottom: number }
  ): { dx: number; dy: number } => {
    // Calculate actual overlap amounts (positive = overlapping, negative = separated)
    const horizontalOverlap = Math.min(a.right, b.right) - Math.max(a.left, b.left)
    const verticalOverlap = Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top)

    // Calculate distances if separated
    const hDistance = horizontalOverlap < 0 ? -horizontalOverlap : 0
    const vDistance = verticalOverlap < 0 ? -verticalOverlap : 0

    // Determine which dimension has the most overlap or needs the most separation
    const hOverlapAmount = horizontalOverlap > 0 ? horizontalOverlap : 0
    const vOverlapAmount = verticalOverlap > 0 ? verticalOverlap : 0

    // If fully overlapping (both dimensions overlap), prefer perpendicular shift to flow direction
    if (hOverlapAmount > 0 && vOverlapAmount > 0) {
      if (direction === 'LR') {
        // LR layout - prefer vertical shift (perpendicular to horizontal flow)
        const shiftY = a.top < b.top
          ? -(vOverlapAmount + MIN_SEPARATION)  // Move a up
          : (vOverlapAmount + MIN_SEPARATION)   // Move a down
        return { dx: 0, dy: shiftY }
      } else {
        // TB layout - prefer horizontal shift (perpendicular to vertical flow)
        const shiftX = a.left < b.left
          ? -(hOverlapAmount + MIN_SEPARATION)  // Move a left
          : (hOverlapAmount + MIN_SEPARATION)   // Move a right
        return { dx: shiftX, dy: 0 }
    }
    }

    // Partial overlap or too close - determine best direction based on overlap axis
    // Ideally, we want to separate in the direction that they are closest/overlapping to preserve alignment
    
    // Case 1: Overlapping/aligned horizontally (share X range), but separated vertically
    // We should separate them vertically to maintain column/stack alignment
    // ONLY if they are too close vertically
    if (hOverlapAmount > 0 && vDistance < MIN_SEPARATION) {
      const shiftY = a.top < b.top
        ? -(MIN_SEPARATION - vDistance) // Shift only by needed amount
        : (MIN_SEPARATION - vDistance)
      return { dx: 0, dy: shiftY }
    } 
    
    // Case 2: Overlapping/aligned vertically (share Y range), but separated horizontally
    // We should separate them horizontally to maintain row alignment
    // ONLY if they are too close horizontally
    else if (vOverlapAmount > 0 && hDistance < MIN_SEPARATION) {
      const shiftX = a.left < b.left
        ? -(MIN_SEPARATION - hDistance) // Shift only by needed amount
        : (MIN_SEPARATION - hDistance)
      return { dx: shiftX, dy: 0 }
    } 
    
    // Case 3: Corner-to-corner (too close diagonally)
    // Use layout preference to decide separation direction
    else if (hDistance < MIN_SEPARATION && vDistance < MIN_SEPARATION) {
      if (direction === 'LR') {
        // LR layout - prefer vertical separation
        const shiftY = a.top < b.top
          ? -(MIN_SEPARATION - vDistance)
          : (MIN_SEPARATION - vDistance)
        return { dx: 0, dy: shiftY }
      } else {
        // TB layout - prefer horizontal separation
        const shiftX = a.left < b.left
          ? -(MIN_SEPARATION - hDistance)
          : (MIN_SEPARATION - hDistance)
        return { dx: shiftX, dy: 0 }
      }
    }

    return { dx: 0, dy: 0 }
  }

  // Sort nodes by position (top to bottom, then left to right)
  const sortedNodes = [...adjustedNodes].sort((a, b) => {
    const aBounds = getBounds(a)
    const bBounds = getBounds(b)
    if (Math.abs(aBounds.top - bBounds.top) > 10) {
      return aBounds.top - bBounds.top
    }
    return aBounds.left - bBounds.left
  })

  // Track cumulative shifts for each node
  const shifts = new Map<string, { dx: number; dy: number }>()
  adjustedNodes.forEach(node => {
    shifts.set(node.id, { dx: 0, dy: 0 })
  })

  // Check each node against all previous nodes
  for (let i = 0; i < sortedNodes.length; i++) {
    const currentNode = sortedNodes[i]
    const currentBounds = getBounds(currentNode)

    // Apply any existing shifts to current bounds
    const currentShift = shifts.get(currentNode.id) || { dx: 0, dy: 0 }
    let adjustedCurrentBounds = {
      left: currentBounds.left + currentShift.dx,
      right: currentBounds.right + currentShift.dx,
      top: currentBounds.top + currentShift.dy,
      bottom: currentBounds.bottom + currentShift.dy
    }

    // Check against all previous nodes
    for (let j = 0; j < i; j++) {
      const otherNode = sortedNodes[j]
      const otherBounds = getBounds(otherNode)

      // Apply any existing shifts to other bounds
      const otherShift = shifts.get(otherNode.id) || { dx: 0, dy: 0 }
      const adjustedOtherBounds = {
        left: otherBounds.left + otherShift.dx,
        right: otherBounds.right + otherShift.dx,
        top: otherBounds.top + otherShift.dy,
        bottom: otherBounds.bottom + otherShift.dy
    }

      // Check for overlap
      if (boxesOverlap(adjustedCurrentBounds, adjustedOtherBounds)) {
        const shift = calculateShift(adjustedCurrentBounds, adjustedOtherBounds)

        if (shift.dx !== 0 || shift.dy !== 0) {
          // Log collision details for debugging

          // Update shift for current node
          const currentShiftValue = shifts.get(currentNode.id) || { dx: 0, dy: 0 }
          shifts.set(currentNode.id, {
            dx: currentShiftValue.dx + shift.dx,
            dy: currentShiftValue.dy + shift.dy
          })

          // Update adjusted bounds for next checks
          adjustedCurrentBounds = {
            left: adjustedCurrentBounds.left + shift.dx,
            right: adjustedCurrentBounds.right + shift.dx,
            top: adjustedCurrentBounds.top + shift.dy,
            bottom: adjustedCurrentBounds.bottom + shift.dy
          }
        }
      }
    }
  }


  // Apply all shifts to nodes
  const shiftedNodes = adjustedNodes.map(node => {
    const shift = shifts.get(node.id)
    if (shift && (shift.dx !== 0 || shift.dy !== 0)) {
      return {
        ...node,
        position: {
          x: node.position.x + shift.dx,
          y: node.position.y + shift.dy
        }
      }
    }
    return node
  })

  return shiftedNodes
}

/**
 * Auto-layout nodes using Dagre algorithm
 */
function layoutWithDagre(nodes: WorkflowNode[], edges: WorkflowEdge[], direction: 'LR' | 'TB'): { nodes: WorkflowNode[], edges: WorkflowEdge[] } {
  // Calculate topology metrics to determine spacing requirements
  const { maxOrchestratorSubAgents, maxRoutingBranches } = calculateTopologyMetrics(nodes)

  // Get config based on layout direction
  const baseConfig = getDagreConfig(direction)

  // Dynamic config based on topology. Widen spacing when the graph fans out —
  // many todo-task sub-agents OR many routing branches — so sibling lanes stay
  // visually distinct instead of cramming together.
  const fanOut = Math.max(maxOrchestratorSubAgents, maxRoutingBranches)
  const spacingMultiplier = fanOut > 3 ? 1.6 : fanOut > 2 ? 1.35 : 1

  const dynamicConfig = {
    ...baseConfig,
    // nodesep (sibling/lane separation) widens most with fan-out; ranksep grows
    // gentler so the tree doesn't become excessively tall.
    nodesep: baseConfig.nodesep * spacingMultiplier,
    ranksep: baseConfig.ranksep * Math.min(spacingMultiplier, 1.3)
  }

  const g = new dagre.graphlib.Graph()
  g.setGraph(dynamicConfig)
  g.setDefaultEdgeLabel(() => ({}))

  // Exclude SUB-AGENT nodes and HEADER nodes from Dagre
  // Sub-agents are positioned manually below todo_task nodes
  // Header nodes (start, variables) are positioned manually in a horizontal row
  // Branch nodes MUST be in Dagre to maintain graph connectivity
  const excludedNodeIds = new Set<string>()

  nodes.forEach(node => {
    if (node.id.includes('-sub-agent-')) {
      excludedNodeIds.add(node.id)
    }
    // Exclude header nodes - they're positioned manually before Dagre runs
    if (node.id === 'start' || node.id === 'variables') {
      excludedNodeIds.add(node.id)
    }
  })

  // Log excluded nodes for debugging
  if (excludedNodeIds.size > 0) {
    const headerNodes = Array.from(excludedNodeIds).filter(id => id === 'start' || id === 'variables')
    if (headerNodes.length > 0) {
      // console.log('[LAYOUT BUG] Excluding header nodes from Dagre:', headerNodes.join(', '))
    }
  }

  const todoTaskNodeIds = new Set(nodes.filter(node => node.type === 'todo_task').map(node => node.id))

  // Add all nodes except excluded nodes to Dagre graph
  nodes.forEach(node => {
    if (!excludedNodeIds.has(node.id)) {
      const layoutDimensions = getNodeLayoutDimensions(node)
      let width = layoutDimensions.width
      let height = layoutDimensions.height

      // For todo tasks, use compound dimensions to reserve space for sub-agents
      if (node.type === 'todo_task') {
        const immediateSubAgentDimensions = nodes
          .filter(candidate => getImmediateSubAgentParentId(candidate.id, todoTaskNodeIds) === node.id)
          .map(candidate => getNodeFootprintDimensions(candidate, nodes, todoTaskNodeIds, direction))
        const numSubAgents = immediateSubAgentDimensions.length

        if (numSubAgents > 0) {
          const subAgentGrid = getSubAgentGridMetricsFromDimensions(immediateSubAgentDimensions, direction)

          if (direction === 'LR') {
            height = height + SUB_AGENT_LAYOUT.parentGap + subAgentGrid.height
            width = Math.max(width, subAgentGrid.width)
          } else {
            width = Math.max(width, subAgentGrid.width)
            height = height + SUB_AGENT_LAYOUT.parentGap + subAgentGrid.height
          }
        }
      }

      g.setNode(node.id, { width, height })
    }
  })

  // Add all edges except those involving sub-agents
  edges.forEach(edge => {
    if (!excludedNodeIds.has(edge.source) && !excludedNodeIds.has(edge.target)) {

      
      const minlen = 1

      // Note: With compound dimensions, minlen logic is less critical but still useful for extra safety
      // We keep a simplified version to ensure connections don't look cramped

      // Apply the calculated minlen (if > 1)
      if (minlen > 1) {
        g.setEdge(edge.source, edge.target, { minlen })
      } else {
        g.setEdge(edge.source, edge.target)
      }
    }
  })

  // Run layout
  dagre.layout(g)

  // Apply positions to nodes (only for nodes that were in Dagre)
  const layoutedNodes = nodes.map(node => {
    if (excludedNodeIds.has(node.id)) {
      // Keep excluded nodes at initial position (will be positioned manually later)
      return node
    }

    const nodeWithPosition = g.node(node.id)
    if (!nodeWithPosition) {
      // Node wasn't in Dagre graph, keep original position
      return node
    }

    const dims = getNodeLayoutDimensions(node)

    // Calculate position based on Compound vs Standard dimensions
    let x = nodeWithPosition.x
    let y = nodeWithPosition.y

    // Default centering (Dagre returns center)
    x -= dims.width / 2
    y -= dims.height / 2

    // Adjust for TodoTask Compound positioning
    if (node.type === 'todo_task') {
      const immediateSubAgentDimensions = nodes
        .filter(candidate => getImmediateSubAgentParentId(candidate.id, todoTaskNodeIds) === node.id)
        .map(candidate => getNodeFootprintDimensions(candidate, nodes, todoTaskNodeIds, direction))
      const numSubAgents = immediateSubAgentDimensions.length

      if (numSubAgents > 0) {
        const subAgentGrid = getSubAgentGridMetricsFromDimensions(immediateSubAgentDimensions, direction)

        if (direction === 'LR') {
          const compoundHeight = dims.height + SUB_AGENT_LAYOUT.parentGap + subAgentGrid.height
          const compoundTop = nodeWithPosition.y - (compoundHeight / 2)
          y = compoundTop
          x = nodeWithPosition.x - (dims.width / 2)
        } else {
          const compoundHeight = dims.height + SUB_AGENT_LAYOUT.parentGap + subAgentGrid.height
          const compoundTop = nodeWithPosition.y - (compoundHeight / 2)
          y = compoundTop
          x = nodeWithPosition.x - (dims.width / 2)
        }
      }
    }

    return {
      ...node,
      position: { x, y }
    }
  })

  // Position branch nodes based on direction:
  // LR: TRUE above, FALSE below
  // TB: TRUE left, FALSE right
  // Dagre owns layout (including branch separation); the manual branch
  // repositioning pass is disabled — see the dagre-only simplification.
  return { nodes: layoutedNodes, edges }
}

/**
 * Determine change type for a step based on detected changes
 */
function getChangeType(stepId: string, changes?: PlanChanges | null): ChangeType | undefined {
  if (!changes) return undefined
  if (changes.added.includes(stepId)) return 'added'
  if (changes.updated.includes(stepId)) return 'updated'
  if (changes.deleted.includes(stepId)) return 'deleted'
  return undefined
}

/**
 * Convert a PlanStep to a React Flow node
 */
function stepToNode(
  step: PlanStep,
  stepIndex: number,
  parentId?: string,
  branchType?: 'true' | 'false',
  changes?: PlanChanges | null,
  stepStatusMap?: Map<string, 'pending' | 'running' | 'completed' | 'failed'>,
  workspacePath?: string | null,
  selectedRunFolder?: string,
  completedStepIds?: Set<string> // Set of completed step IDs (converted from indices for step_id-based matching)
): WorkflowNode {
  const nodeId = parentId
    ? `${parentId}-${branchType}-${stepIndex}`
    : step.id || `step-${stepIndex}`

  // Determine change type for highlighting
  const changeType = getChangeType(step.id || nodeId, changes)

  // Determine status: Use step_id as primary matching method (stepStatusMap > completedStepIds > pending)
  let status: 'pending' | 'running' | 'completed' | 'failed' = 'pending'
  const stepId = step.id || nodeId

  // Primary: Check stepStatusMap (from events) - this is the most up-to-date and uses step_id
  if (stepStatusMap && stepStatusMap.has(stepId)) {
    status = stepStatusMap.get(stepId)!
  } else if (!parentId && completedStepIds && completedStepIds.has(stepId)) {
    // Primary: Check completedStepIds (converted from completedStepIndices) - uses step_id for matching
    // Only for main plan steps, not nested branches (nested steps are tracked differently)
    status = 'completed' as const
  } else {
    // Default: pending
    status = 'pending' as const
  }

  // For conditional nodes, use condition_question as title if step.title is missing
  // For other steps, use step.title or fallback to "Step N"
  const getStepTitle = () => {
    if (isConditionalStep(step)) {
      // For conditional nodes, prefer condition_question over generic "Step N"
      return step.title || step.condition_question || `Condition ${stepIndex + 1}`
    }
    if (isTodoTaskStep(step)) {
      // For todo task nodes, use step title or fallback
      return step.title || `Todo Task ${stepIndex + 1}`
    }
    if (isHumanInputStep(step)) {
      // For human input nodes, prefer question over generic title
      return step.title || step.question || `Human Input ${stepIndex + 1}`
    }
    // For regular steps, use step.title or fallback
    // For nested branch steps, use a more descriptive fallback
    if (parentId) {
      // This is a step inside a branch
      return step.title || `Branch Step ${stepIndex + 1}`
    }
    return step.title || `Step ${stepIndex + 1}`
  }

  const baseData = {
    id: nodeId,
    title: getStepTitle(),
    description: step.description,
    success_criteria: step.success_criteria,
    status,
    stepIndex,
    step,
    changeType,
    workspacePath,
    selectedRunFolder,
    validation_schema: step.validation_schema
  }

  if (isConditionalStep(step)) {
    return {
      id: nodeId,
      type: 'conditional',
      position: { x: 0, y: 0 },
      data: {
        ...baseData,
        condition_question: step.condition_question,
        condition_context: step.condition_context
        // Note: status is inherited from baseData (computed based on completedStepIndices)
      } as ConditionalNodeData
    }
  }

  if (isRoutingStep(step)) {
    return {
      id: nodeId,
      type: 'routing',
      position: { x: 0, y: 0 },
      data: {
        ...baseData,
        routing_question: step.routing_question,
        routes: step.routes,
        validation_schema: step.validation_schema
      } as RoutingStepNodeData
    }
  }

  if (isTodoTaskStep(step)) {
    return {
      id: nodeId,
      type: 'todo_task',
      position: { x: 0, y: 0 },
      data: {
        ...baseData,
        todo_task_step: step.todo_task_step,  // backwards compat
        predefined_routes: step.predefined_routes,
        enable_generic_agent: step.enable_generic_agent,
        // Flat format: validation_schema is directly on step
        validation_schema: step.validation_schema || step.todo_task_step?.validation_schema
        // Note: status is inherited from baseData (computed based on completedStepIndices)
      } as TodoTaskNodeData
    }
  }

  if (isHumanInputStep(step)) {
    return {
      id: nodeId,
      type: 'human_input',
      position: { x: 0, y: 0 },
      data: {
        ...baseData,
        question: step.question,
        response_type: step.response_type || 'text',
        options: step.options
        // Note: status is inherited from baseData (computed based on completedStepIndices)
      } as HumanInputNodeData
    }
  }

  if (isMessageSequenceStep(step)) {
    return {
      id: nodeId,
      type: 'message_sequence',
      position: { x: 0, y: 0 },
      data: {
        ...baseData,
        items: step.items
        // Note: status is inherited from baseData (computed based on completedStepIndices)
      } as MessageSequenceNodeData
    }
  }

  return {
    id: nodeId,
    type: 'step',
    position: { x: 0, y: 0 },
    data: baseData as StepNodeData
  }
}

/**
 * Create validation and learning nodes for a step
 * DISABLED: Validation and learning nodes are no longer displayed in the workflow canvas
 * Returns empty nodes and edges, with the step itself as the exit node
 */
function createValidationLearningNodes(
  stepNodeId: string
): { nodes: WorkflowNode[], edges: WorkflowEdge[], exitNodeId: string } {
  // Validation and learning nodes are no longer displayed in the workflow canvas
  // Simply return the step itself as the exit node
  return { nodes: [], edges: [], exitNodeId: stepNodeId }
}

/**
 * Process steps recursively to handle nested branches
 */
function processSteps(
  steps: PlanStep[],
  parentId: string | undefined,
  branchType: 'true' | 'false' | undefined,
  changes: PlanChanges | null | undefined,
  presetUseCodeExecutionMode: boolean,
  presetLLMConfig: AgentLLMConfig | undefined,
  availableLLMs: Array<{ provider: string; model: string; label: string }>,
  completedStepIndices: number[] = [],
  stepStatusMap?: Map<string, 'pending' | 'running' | 'completed' | 'failed'>,
  workspacePath?: string | null,
  selectedRunFolder?: string,
  stepIdToNodeIdMap?: Map<string, string>, // Map of step ID to node ID for next_step_id lookups
  completedStepIds?: Set<string> // Set of completed step IDs (converted from indices for step_id-based matching)
): { nodes: WorkflowNode[], edges: WorkflowEdge[] } {
  const nodes: WorkflowNode[] = []
  const edges: WorkflowEdge[] = []

  // Track the last "exit" node ID for edge connections
  // Can be a single node ID, array of branch exit nodes, or null
  let lastExitNodeId: string | string[] | null = null
  // Track conditional nodes with empty branches for label purposes
  const conditionalEmptyBranches = new Map<string, { trueEmpty: boolean; falseEmpty: boolean }>()

  // Nodes reached by an explicit branch/route edge (routing routes, conditional
  // if_true/if_false, human-input yes/no, todo next_step_id). These are entered
  // ONLY via their branch edge, so they must NOT also receive an array-order
  // sequential edge — otherwise sibling routes get cross-linked into one cramped
  // linear chain instead of fanning out as distinct branches under the router.
  const explicitBranchTargetNodeIds = new Set<string>()
  const addBranchTarget = (stepId?: string) => {
    if (!stepId || stepId === 'end') return
    const targetNodeId = stepIdToNodeIdMap?.get(stepId)
    if (targetNodeId) explicitBranchTargetNodeIds.add(targetNodeId)
  }
  steps.forEach(s => {
    if (isRoutingStep(s) && s.routes) s.routes.forEach(r => addBranchTarget(r.next_step_id))
    if (isConditionalStep(s)) {
      addBranchTarget(s.if_true_next_step_id)
      addBranchTarget(s.if_false_next_step_id)
    }
    if (isHumanInputStep(s)) {
      addBranchTarget(s.if_yes_next_step_id)
      addBranchTarget(s.if_no_next_step_id)
    }
    if (isTodoTaskStep(s)) addBranchTarget(s.next_step_id)
  })

  const buildTodoTaskSubAgentGraph = (
    todoTaskStep: PlanStep,
    todoTaskNodeId: string,
    todoTaskNodeData: TodoTaskNodeData,
    includeCompletionEdge: boolean
  ): { nodes: WorkflowNode[], edges: WorkflowEdge[] } => {
    const todoTaskEdges: WorkflowEdge[] = []
    const todoTaskSubAgentNodes: WorkflowNode[] = []
    const parentStepIndex = todoTaskNodeData.stepIndex
    const todoTaskTitle = todoTaskNodeData.title || todoTaskStep.title || `Todo Task ${parentStepIndex + 1}`

    if (isTodoTaskStep(todoTaskStep) && todoTaskStep.predefined_routes && todoTaskStep.predefined_routes.length > 0) {
      todoTaskStep.predefined_routes.forEach((route) => {
        const isEndRoute = route.route_id?.toLowerCase() === 'end'

        if (isEndRoute) {
          if (includeCompletionEdge) {
            todoTaskEdges.push({
              id: `${todoTaskNodeId}-route-${route.route_id}-to-end`,
              source: todoTaskNodeId,
              sourceHandle: route.route_id,
              target: 'end',
              type: 'step',
              style: { stroke: '#ef4444', strokeWidth: 2 },
              animated: false
            })
          }
          return
        }

        if (!route.sub_agent_step) {
          return
        }

        const routeId = route.route_id || route.sub_agent_step.id || String(todoTaskStep.predefined_routes?.indexOf(route) ?? 0)
        const subAgentNodeId = `${todoTaskNodeId}-sub-agent-${routeId}`
        const subAgentStep = route.sub_agent_step
        const stepId = subAgentStep.id || subAgentNodeId

        let status: 'pending' | 'running' | 'completed' | 'failed' = 'pending'
        if (stepStatusMap && stepStatusMap.has(stepId)) {
          status = stepStatusMap.get(stepId)!
        }

        const changeType = getChangeType(stepId, changes)

        if (isTodoTaskStep(subAgentStep)) {
          const nestedTodoNode: WorkflowNode = {
            id: subAgentNodeId,
            type: 'todo_task',
            position: { x: 0, y: 0 },
            data: {
              id: subAgentNodeId,
              title: subAgentStep.title || `${route.route_name || route.route_id || routeId}`,
              description: subAgentStep.description,
              success_criteria: subAgentStep.success_criteria,
              status,
              stepIndex: parentStepIndex,
              step: subAgentStep,
              changeType,
              todo_task_step: subAgentStep.todo_task_step,  // backwards compat
              predefined_routes: subAgentStep.predefined_routes,
              enable_generic_agent: subAgentStep.enable_generic_agent,
              validation_schema: subAgentStep.validation_schema || subAgentStep.todo_task_step?.validation_schema,
              workspacePath,
              selectedRunFolder,
              parentOrchestratorTitle: todoTaskTitle,
              routeName: route.route_name || undefined,
              routeCondition: route.condition || undefined
            } as TodoTaskNodeData
          }

          todoTaskSubAgentNodes.push(nestedTodoNode)

          const nestedTodoGraph = buildTodoTaskSubAgentGraph(
            subAgentStep,
            subAgentNodeId,
            nestedTodoNode.data as TodoTaskNodeData,
            false
          )
          todoTaskSubAgentNodes.push(...nestedTodoGraph.nodes)
          todoTaskEdges.push(...nestedTodoGraph.edges)
        } else if (isMessageSequenceStep(subAgentStep)) {
          // A message_sequence sub-agent must render as a MessageSequenceNode so its
          // ordered items show — not as a generic step card.
          const seqNode: WorkflowNode = {
            id: subAgentNodeId,
            type: 'message_sequence',
            position: { x: 0, y: 0 },
            data: {
              id: subAgentNodeId,
              title: subAgentStep.title || `${route.route_name || route.route_id || routeId}`,
              description: subAgentStep.description,
              items: subAgentStep.items,
              status,
              stepIndex: parentStepIndex,
              step: subAgentStep,
              changeType,
              validation_schema: subAgentStep.validation_schema,
              workspacePath,
              selectedRunFolder,
              parentOrchestratorTitle: todoTaskTitle,
              routeName: route.route_name || undefined,
              routeCondition: route.condition || undefined
            } as MessageSequenceNodeData
          }

          todoTaskSubAgentNodes.push(seqNode)
        } else {
          const subAgentNode: WorkflowNode = {
            id: subAgentNodeId,
            type: 'step',
            position: { x: 0, y: 0 },
            data: {
              id: subAgentNodeId,
              title: subAgentStep.title || `${route.route_name || route.route_id || routeId}`,
              description: subAgentStep.description,
              success_criteria: subAgentStep.success_criteria,
              status,
              stepIndex: parentStepIndex,
              step: subAgentStep,
              changeType,
              validation_schema: subAgentStep.validation_schema,
              workspacePath,
              selectedRunFolder,
              parentOrchestratorTitle: todoTaskTitle,
              routeName: route.route_name || undefined,
              routeCondition: route.condition || undefined
            } as StepNodeData
          }

          todoTaskSubAgentNodes.push(subAgentNode)
        }

        todoTaskEdges.push({
          id: `${todoTaskNodeId}-route-${route.route_id}-to-sub-agent`,
          source: todoTaskNodeId,
          sourceHandle: route.route_id,
          target: subAgentNodeId,
          targetHandle: 'top',
          type: 'step',
          style: { stroke: '#8b5cf6', strokeWidth: 2, strokeDasharray: '5,5' },
          animated: false
        })
      })
    }

    if (isTodoTaskStep(todoTaskStep) && todoTaskStep.enable_generic_agent) {
      const routeId = 'generic'
      const subAgentNodeId = `${todoTaskNodeId}-sub-agent-${routeId}`

      let status: 'pending' | 'running' | 'completed' | 'failed' = 'pending'
      if (stepStatusMap && stepStatusMap.has(subAgentNodeId)) {
        status = stepStatusMap.get(subAgentNodeId)!
      }

      const subAgentNode: WorkflowNode = {
        id: subAgentNodeId,
        type: 'step',
        position: { x: 0, y: 0 },
        data: {
          id: subAgentNodeId,
          title: 'Generic Agent',
          description: 'Executes ad-hoc tasks using workspace tools',
          success_criteria: 'Task completion verified by orchestrator',
          status,
          stepIndex: parentStepIndex,
          step: {
            id: subAgentNodeId,
            title: 'Generic Agent',
            description: 'Executes ad-hoc tasks using workspace tools',
            type: 'regular',
            agent_configs: {
              disable_learning: true
            }
          } as PlanStep,
          changeType: undefined,
          workspacePath,
          selectedRunFolder,
          parentOrchestratorTitle: todoTaskTitle,
          routeName: 'Generic Execution',
          routeCondition: 'Ad-hoc tasks'
        } as StepNodeData
      }

      todoTaskSubAgentNodes.push(subAgentNode)

      todoTaskEdges.push({
        id: `${todoTaskNodeId}-route-generic-to-sub-agent`,
        source: todoTaskNodeId,
        target: subAgentNodeId,
        targetHandle: 'top',
        type: 'smoothstep',
        style: { stroke: '#8b5cf6', strokeWidth: 2, strokeDasharray: '5,5' },
        animated: false
      })
    }

    if (includeCompletionEdge && isTodoTaskStep(todoTaskStep) && todoTaskStep.next_step_id) {
      const targetNodeId = stepIdToNodeIdMap?.get(todoTaskStep.next_step_id)
      if (targetNodeId) {
        todoTaskEdges.push({
          id: `${todoTaskNodeId}-todo-task-to-${targetNodeId}`,
          source: todoTaskNodeId,
          target: targetNodeId,
          type: 'smoothstep',
          style: { stroke: '#8b5cf6', strokeWidth: 2 },
          animated: false
        })
      } else if (todoTaskStep.next_step_id === 'end') {
        todoTaskEdges.push({
          id: `${todoTaskNodeId}-todo-task-to-end`,
          source: todoTaskNodeId,
          target: 'end',
          type: 'smoothstep',
          label: 'Complete',
          labelStyle: COMPLETION_EDGE_LABEL_STYLE,
          labelBgStyle: EDGE_LABEL_BG_STYLE,
          labelBgPadding: [4, 4] as [number, number],
          labelBgBorderRadius: 4,
          style: { stroke: '#8b5cf6', strokeWidth: 2 },
          animated: false
        })
      }
    }

    return { nodes: todoTaskSubAgentNodes, edges: todoTaskEdges }
  }

  steps.forEach((step, index) => {
    const node = stepToNode(step, index, parentId, branchType, changes, stepStatusMap, workspacePath, selectedRunFolder, completedStepIds)
    nodes.push(node)

    // Create edge from previous step's exit node (sequential flow).
    // Skip when this node is an explicit branch/route target — it already has an
    // incoming edge from its router/branch, and an extra array-order edge would
    // cross-link sibling routes into a single cramped chain.
    // If lastExitNodeId is an array, it means we're connecting from multiple branch exits
    if (lastExitNodeId && !explicitBranchTargetNodeIds.has(node.id)) {
      if (Array.isArray(lastExitNodeId)) {
        // Connect from all branch exit nodes to this step
        lastExitNodeId.forEach((exitNodeId, i) => {
          if (exitNodeId) {
            // Check if this exit node is a conditional with empty branches
            const emptyInfo = conditionalEmptyBranches.get(exitNodeId)
            const isConditionalWithEmptyBranch = emptyInfo && (emptyInfo.trueEmpty || emptyInfo.falseEmpty)

            edges.push({
              id: `${exitNodeId}-to-${node.id}-${i}`,
              source: exitNodeId,
              target: node.id,
              type: 'smoothstep',
              animated: false,
              style: {
                stroke: isConditionalWithEmptyBranch ? (emptyInfo?.trueEmpty ? '#22c55e' : '#ef4444') : '#6b7280',
                strokeWidth: 2
              },
              label: isConditionalWithEmptyBranch ? (emptyInfo?.trueEmpty ? 'Yes' : 'No') : undefined,
              labelStyle: isConditionalWithEmptyBranch ? {
                fill: emptyInfo?.trueEmpty ? '#22c55e' : '#ef4444',
                fontWeight: 600,
                fontSize: 11
              } : undefined,
              labelBgStyle: isConditionalWithEmptyBranch ? {
                fill: emptyInfo?.trueEmpty ? '#f0fdf4' : '#fef2f2',
                fillOpacity: 0.9
              } : undefined,
              labelBgPadding: isConditionalWithEmptyBranch ? [4, 4] as [number, number] : undefined,
              labelBgBorderRadius: isConditionalWithEmptyBranch ? 4 : undefined
            })
          }
        })
      } else {
        // Single exit node (normal sequential flow)
        // Check if this exit node is a conditional with empty branches
        const emptyInfo = conditionalEmptyBranches.get(lastExitNodeId)
        const isConditionalWithEmptyBranch = emptyInfo && (emptyInfo.trueEmpty || emptyInfo.falseEmpty)

        edges.push({
          id: `${lastExitNodeId}-to-${node.id}`,
          source: lastExitNodeId,
          target: node.id,
          type: 'smoothstep',
          animated: false,
          style: {
            stroke: isConditionalWithEmptyBranch ? (emptyInfo?.trueEmpty ? '#22c55e' : '#ef4444') : '#6b7280',
            strokeWidth: 2
          },
          label: isConditionalWithEmptyBranch ? (emptyInfo?.trueEmpty ? 'Yes' : 'No') : undefined,
          labelStyle: isConditionalWithEmptyBranch ? {
            fill: emptyInfo?.trueEmpty ? '#22c55e' : '#ef4444',
            fontWeight: 600,
            fontSize: 11
          } : undefined,
          labelBgStyle: isConditionalWithEmptyBranch ? {
            fill: emptyInfo?.trueEmpty ? '#f0fdf4' : '#fef2f2',
            fillOpacity: 0.9
          } : undefined,
          labelBgPadding: isConditionalWithEmptyBranch ? [4, 4] as [number, number] : undefined,
          labelBgBorderRadius: isConditionalWithEmptyBranch ? 4 : undefined
        })
      }
    }

    // Add validation/learning nodes for non-conditional and non-human-input steps
    // Decision steps also have validation/learning for their inner step execution
    // Human input steps don't have validation/learning (they just ask questions)
    if (!isConditionalStep(step) && !isHumanInputStep(step)) {
      const vlResult = createValidationLearningNodes(
        node.id
      )
      nodes.push(...vlResult.nodes)
      edges.push(...vlResult.edges)
      lastExitNodeId = vlResult.exitNodeId
    } else {
      // For conditional nodes, track branch exit nodes to reconnect to next step
      lastExitNodeId = null
    }

    // Handle conditional branches
    if (isConditionalStep(step)) {
      const branchExitNodes: string[] = []

      // Process if_true_steps
      let trueBranchExitNodeId: string | null = null
      if (step.if_true_steps && step.if_true_steps.length > 0) {
        const trueBranch = processSteps(
          step.if_true_steps,
          node.id,
          'true',
          changes,
          presetUseCodeExecutionMode,
          presetLLMConfig,
          availableLLMs,
          completedStepIndices,
          stepStatusMap,
          workspacePath,
          selectedRunFolder,
          stepIdToNodeIdMap,
          completedStepIds
        )
        nodes.push(...trueBranch.nodes)
        edges.push(...trueBranch.edges)

        // Connect conditional to first true branch step
        if (trueBranch.nodes.length > 0) {
          edges.push({
            id: `${node.id}-true-branch`,
            source: node.id,
            target: trueBranch.nodes[0].id,
            type: 'smoothstep',
            label: 'Yes',
            labelStyle: { fill: '#22c55e', fontWeight: 600, fontSize: 11 },
            labelBgStyle: { fill: '#f0fdf4', fillOpacity: 0.9 },
            labelBgPadding: [4, 4] as [number, number],
            labelBgBorderRadius: 4,
            style: { stroke: '#22c55e', strokeWidth: 2 },
            animated: false
          })

          // Find the last node in the true branch (exit node)
          // This could be a step, validation, or learning node
          const lastTrueNode = trueBranch.nodes[trueBranch.nodes.length - 1]
          if (lastTrueNode) {
            trueBranchExitNodeId = lastTrueNode.id
            branchExitNodes.push(lastTrueNode.id)
          }
        }
      } else {
        // No true branch steps - conditional node itself is an exit
        // We'll create an edge to the next step below (either via next_step_id or sequential)
        trueBranchExitNodeId = node.id
        branchExitNodes.push(node.id)
        // Track that true branch is empty
        const emptyInfo = conditionalEmptyBranches.get(node.id) || { trueEmpty: false, falseEmpty: false }
        emptyInfo.trueEmpty = true
        conditionalEmptyBranches.set(node.id, emptyInfo)
      }

      // Process if_false_steps
      let falseBranchExitNodeId: string | null = null
      if (step.if_false_steps && step.if_false_steps.length > 0) {
        const falseBranch = processSteps(
          step.if_false_steps,
          node.id,
          'false',
          changes,
          presetUseCodeExecutionMode,
          presetLLMConfig,
          availableLLMs,
          completedStepIndices,
          stepStatusMap,
          workspacePath,
          selectedRunFolder,
          stepIdToNodeIdMap,
          completedStepIds
        )
        nodes.push(...falseBranch.nodes)
        edges.push(...falseBranch.edges)

        // Connect conditional to first false branch step
        if (falseBranch.nodes.length > 0) {
          edges.push({
            id: `${node.id}-false-branch`,
            source: node.id,
            target: falseBranch.nodes[0].id,
            type: 'smoothstep',
            label: 'No',
            labelStyle: { fill: '#ef4444', fontWeight: 600, fontSize: 11 },
            labelBgStyle: { fill: '#fef2f2', fillOpacity: 0.9 },
            labelBgPadding: [4, 4] as [number, number],
            labelBgBorderRadius: 4,
            style: { stroke: '#ef4444', strokeWidth: 2 },
            animated: false
          })

          // Find the last node in the false branch (exit node)
          const lastFalseNode = falseBranch.nodes[falseBranch.nodes.length - 1]
          if (lastFalseNode) {
            falseBranchExitNodeId = lastFalseNode.id
            branchExitNodes.push(lastFalseNode.id)
          }
        }
      } else {
        // No false branch steps - conditional node itself is an exit (if not already added)
        if (!branchExitNodes.includes(node.id)) {
          falseBranchExitNodeId = node.id
          branchExitNodes.push(node.id)
        }
        // Track that false branch is empty
        const emptyInfo = conditionalEmptyBranches.get(node.id) || { trueEmpty: false, falseEmpty: false }
        emptyInfo.falseEmpty = true
        conditionalEmptyBranches.set(node.id, emptyInfo)
      }

      // Handle next_step_id connections
      // Check if_true_next_step_id and if_false_next_step_id to create explicit connections
      const nextStepEdges: WorkflowEdge[] = []

      // Handle true branch next_step_id
      if (trueBranchExitNodeId) {
        if (step.if_true_next_step_id) {
          // Explicit next_step_id provided
          const targetNodeId = stepIdToNodeIdMap?.get(step.if_true_next_step_id)
          if (targetNodeId) {
            // Create edge from true branch exit node to target
            nextStepEdges.push({
              id: `${trueBranchExitNodeId}-to-${targetNodeId}-true-next`,
              source: trueBranchExitNodeId,
              target: targetNodeId,
              type: 'smoothstep',
              label: step.if_true_steps && step.if_true_steps.length > 0 ? undefined : 'Yes', // Show "Yes" label if branch is empty
              labelStyle: step.if_true_steps && step.if_true_steps.length > 0 ? undefined : { fill: '#22c55e', fontWeight: 600, fontSize: 11 },
              labelBgStyle: step.if_true_steps && step.if_true_steps.length > 0 ? undefined : { fill: '#f0fdf4', fillOpacity: 0.9 },
              labelBgPadding: step.if_true_steps && step.if_true_steps.length > 0 ? undefined : [4, 4] as [number, number],
              labelBgBorderRadius: step.if_true_steps && step.if_true_steps.length > 0 ? undefined : 4,
              style: { stroke: '#22c55e', strokeWidth: 2, strokeDasharray: step.if_true_steps && step.if_true_steps.length > 0 ? '5,5' : undefined },
              animated: false
            })
          } else if (step.if_true_next_step_id === 'end') {
            // Connect to end node
            nextStepEdges.push({
              id: `${trueBranchExitNodeId}-to-end-true-next`,
              source: trueBranchExitNodeId,
              target: 'end',
              type: 'smoothstep',
              label: step.if_true_steps && step.if_true_steps.length > 0 ? undefined : 'Yes', // Show "Yes" label if branch is empty
              labelStyle: step.if_true_steps && step.if_true_steps.length > 0 ? undefined : { fill: '#22c55e', fontWeight: 600, fontSize: 11 },
              labelBgStyle: step.if_true_steps && step.if_true_steps.length > 0 ? undefined : { fill: '#f0fdf4', fillOpacity: 0.9 },
              labelBgPadding: step.if_true_steps && step.if_true_steps.length > 0 ? undefined : [4, 4] as [number, number],
              labelBgBorderRadius: step.if_true_steps && step.if_true_steps.length > 0 ? undefined : 4,
              style: { stroke: '#22c55e', strokeWidth: 2, strokeDasharray: step.if_true_steps && step.if_true_steps.length > 0 ? '5,5' : undefined },
              animated: false
            })
          }
        }
        // If no explicit next_step_id and branch is empty, we'll use default sequential flow (handled below)
      }

      // Handle false branch next_step_id
      if (falseBranchExitNodeId) {
        if (step.if_false_next_step_id) {
          // Explicit next_step_id provided
          const targetNodeId = stepIdToNodeIdMap?.get(step.if_false_next_step_id)
          if (targetNodeId) {
            // Create edge from false branch exit node to target
            nextStepEdges.push({
              id: `${falseBranchExitNodeId}-to-${targetNodeId}-false-next`,
              source: falseBranchExitNodeId,
              target: targetNodeId,
              type: 'smoothstep',
              label: step.if_false_steps && step.if_false_steps.length > 0 ? undefined : 'No', // Show "No" label if branch is empty
              labelStyle: step.if_false_steps && step.if_false_steps.length > 0 ? undefined : { fill: '#ef4444', fontWeight: 600, fontSize: 11 },
              labelBgStyle: step.if_false_steps && step.if_false_steps.length > 0 ? undefined : { fill: '#fef2f2', fillOpacity: 0.9 },
              labelBgPadding: step.if_false_steps && step.if_false_steps.length > 0 ? undefined : [4, 4] as [number, number],
              labelBgBorderRadius: step.if_false_steps && step.if_false_steps.length > 0 ? undefined : 4,
              style: { stroke: '#ef4444', strokeWidth: 2, strokeDasharray: step.if_false_steps && step.if_false_steps.length > 0 ? '5,5' : undefined },
              animated: false
            })
          } else if (step.if_false_next_step_id === 'end') {
            // Connect to end node
            nextStepEdges.push({
              id: `${falseBranchExitNodeId}-to-end-false-next`,
              source: falseBranchExitNodeId,
              target: 'end',
              type: 'smoothstep',
              label: step.if_false_steps && step.if_false_steps.length > 0 ? undefined : 'No', // Show "No" label if branch is empty
              labelStyle: step.if_false_steps && step.if_false_steps.length > 0 ? undefined : { fill: '#ef4444', fontWeight: 600, fontSize: 11 },
              labelBgStyle: step.if_false_steps && step.if_false_steps.length > 0 ? undefined : { fill: '#fef2f2', fillOpacity: 0.9 },
              labelBgPadding: step.if_false_steps && step.if_false_steps.length > 0 ? undefined : [4, 4] as [number, number],
              labelBgBorderRadius: step.if_false_steps && step.if_false_steps.length > 0 ? undefined : 4,
              style: { stroke: '#ef4444', strokeWidth: 2, strokeDasharray: step.if_false_steps && step.if_false_steps.length > 0 ? '5,5' : undefined },
              animated: false
            })
          }
        }
        // If no explicit next_step_id and branch is empty, we'll use default sequential flow (handled below)
      }

      // Add next_step_id edges if any were created
      edges.push(...nextStepEdges)

      // Set lastExitNodeId to array of branch exit nodes (or single node if only one branch)
      // Only use this for sequential flow if next_step_id is not provided
      // If next_step_id is provided, we've already created explicit edges above
      if (step.if_true_next_step_id || step.if_false_next_step_id) {
        // Explicit next_step_id provided - don't set lastExitNodeId (edges already created)
        lastExitNodeId = null
      } else {
        // No explicit next_step_id - use default sequential flow
        if (branchExitNodes.length === 1) {
          lastExitNodeId = branchExitNodes[0]
        } else if (branchExitNodes.length > 1) {
          lastExitNodeId = branchExitNodes
        } else {
          // No branches at all - conditional node itself is the exit
          lastExitNodeId = node.id
        }
      }
    }

    // Handle routing step edge routing
    // Routing steps evaluate a question and route to one of N possible next steps
    if (isRoutingStep(step)) {
      const routingEdges: WorkflowEdge[] = []
      const sourceNodeId = (typeof lastExitNodeId === 'string' ? lastExitNodeId : node.id)

      if (step.routes) {
        step.routes.forEach((route, routeIndex) => {
          const targetNodeId = stepIdToNodeIdMap?.get(route.next_step_id)
          const routeColor = ROUTING_EDGE_COLORS[routeIndex % ROUTING_EDGE_COLORS.length]

          if (targetNodeId) {
            const isSelectedRoute = !step.selected_route_id || route.route_id === step.selected_route_id
            routingEdges.push({
              id: `${sourceNodeId}-routing-${route.route_id}-to-${targetNodeId}`,
              source: sourceNodeId,
              sourceHandle: `route-${route.route_id}`,
              target: targetNodeId,
              type: 'routing',
              label: route.route_name || route.route_id,
              labelStyle: { ...ROUTE_EDGE_LABEL_STYLE, opacity: isSelectedRoute ? 1 : 0.5 },
              labelBgStyle: EDGE_LABEL_BG_STYLE,
              labelBgPadding: [3, 3] as [number, number],
              labelBgBorderRadius: 4,
              data: {
                routeIndex,
                routeCount: step.routes?.length || 0,
                routeName: route.route_name || route.route_id,
                selected: isSelectedRoute,
                color: routeColor
              },
              style: {
                stroke: isSelectedRoute ? routeColor : '#94a3b8',
                strokeWidth: isSelectedRoute ? 2.5 : 1.25,
                opacity: isSelectedRoute ? 1 : 0.4
              },
              animated: false
            })
          } else if (route.next_step_id === 'end') {
            const isSelectedRoute = !step.selected_route_id || route.route_id === step.selected_route_id
            routingEdges.push({
              id: `${sourceNodeId}-routing-${route.route_id}-to-end`,
              source: sourceNodeId,
              sourceHandle: `route-${route.route_id}`,
              target: 'end',
              type: 'routing',
              label: route.route_name || route.route_id,
              labelStyle: { ...ROUTE_EDGE_LABEL_STYLE, opacity: isSelectedRoute ? 1 : 0.5 },
              labelBgStyle: EDGE_LABEL_BG_STYLE,
              labelBgPadding: [3, 3] as [number, number],
              labelBgBorderRadius: 4,
              data: {
                routeIndex,
                routeCount: step.routes?.length || 0,
                routeName: route.route_name || route.route_id,
                selected: isSelectedRoute,
                color: routeColor
              },
              style: {
                stroke: isSelectedRoute ? routeColor : '#94a3b8',
                strokeWidth: isSelectedRoute ? 2.5 : 1.25,
                opacity: isSelectedRoute ? 1 : 0.4
              },
              animated: false
            })
          }
        })
      }

      edges.push(...routingEdges)
      lastExitNodeId = null // Routing steps handle their own routing
    }

    // Handle human input step edge routing
    // Human input steps ask a question and route based on response (yes/no or multiple choice)
    if (isHumanInputStep(step)) {
      const humanInputEdges: WorkflowEdge[] = []
      // Use the human input node itself as source (no validation/learning nodes for human input)
      const sourceNodeId = node.id

      // Determine routing based on response_type
      if (step.response_type === 'yesno') {
        // Yes/No routing
        if (step.if_yes_next_step_id) {
          const targetNodeId = stepIdToNodeIdMap?.get(step.if_yes_next_step_id)
          if (targetNodeId) {
            humanInputEdges.push({
              id: `${sourceNodeId}-human-input-yes-to-${targetNodeId}`,
              source: sourceNodeId,
              target: targetNodeId,
              type: 'smoothstep',
              label: 'Yes',
              labelStyle: { fill: '#22c55e', fontWeight: 600, fontSize: 11 },
              labelBgStyle: { fill: '#f0fdf4', fillOpacity: 0.9 },
              labelBgPadding: [4, 4] as [number, number],
              labelBgBorderRadius: 4,
              style: { stroke: '#22c55e', strokeWidth: 2 },
              animated: false
            })
          } else if (step.if_yes_next_step_id === 'end') {
            humanInputEdges.push({
              id: `${sourceNodeId}-human-input-yes-to-end`,
              source: sourceNodeId,
              target: 'end',
              type: 'smoothstep',
              label: 'Yes',
              labelStyle: { fill: '#22c55e', fontWeight: 600, fontSize: 11 },
              labelBgStyle: { fill: '#f0fdf4', fillOpacity: 0.9 },
              labelBgPadding: [4, 4] as [number, number],
              labelBgBorderRadius: 4,
              style: { stroke: '#22c55e', strokeWidth: 2 },
              animated: false
            })
          }
        }

        if (step.if_no_next_step_id) {
          const targetNodeId = stepIdToNodeIdMap?.get(step.if_no_next_step_id)
          if (targetNodeId) {
            humanInputEdges.push({
              id: `${sourceNodeId}-human-input-no-to-${targetNodeId}`,
              source: sourceNodeId,
              target: targetNodeId,
              type: 'smoothstep',
              label: 'No',
              labelStyle: { fill: '#ef4444', fontWeight: 600, fontSize: 11 },
              labelBgStyle: { fill: '#fef2f2', fillOpacity: 0.9 },
              labelBgPadding: [4, 4] as [number, number],
              labelBgBorderRadius: 4,
              style: { stroke: '#ef4444', strokeWidth: 2 },
              animated: false
            })
          } else if (step.if_no_next_step_id === 'end') {
            humanInputEdges.push({
              id: `${sourceNodeId}-human-input-no-to-end`,
              source: sourceNodeId,
              target: 'end',
              type: 'smoothstep',
              label: 'No',
              labelStyle: { fill: '#ef4444', fontWeight: 600, fontSize: 11 },
              labelBgStyle: { fill: '#fef2f2', fillOpacity: 0.9 },
              labelBgPadding: [4, 4] as [number, number],
              labelBgBorderRadius: 4,
              style: { stroke: '#ef4444', strokeWidth: 2 },
              animated: false
            })
          }
        }
      } else if (step.response_type === 'multiple_choice' && step.option_routes) {
        // Multiple choice routing - create edges for each option route
        Object.entries(step.option_routes).forEach(([optionKey, nextStepId]) => {
          const targetNodeId = stepIdToNodeIdMap?.get(nextStepId)
          const optionLabel = step.options?.[parseInt(optionKey)] || optionKey

          if (targetNodeId) {
            humanInputEdges.push({
              id: `${sourceNodeId}-human-input-option-${optionKey}-to-${targetNodeId}`,
              source: sourceNodeId,
              target: targetNodeId,
              type: 'smoothstep',
              label: optionLabel,
              labelStyle: { fill: '#3b82f6', fontWeight: 600, fontSize: 11 },
              labelBgStyle: { fill: '#eff6ff', fillOpacity: 0.9 },
              labelBgPadding: [4, 4] as [number, number],
              labelBgBorderRadius: 4,
              style: { stroke: '#3b82f6', strokeWidth: 2 },
              animated: false
            })
          } else if (nextStepId === 'end') {
            humanInputEdges.push({
              id: `${sourceNodeId}-human-input-option-${optionKey}-to-end`,
              source: sourceNodeId,
              target: 'end',
              type: 'smoothstep',
              label: optionLabel,
              labelStyle: { fill: '#ef4444', fontWeight: 600, fontSize: 11 },
              labelBgStyle: { fill: '#fef2f2', fillOpacity: 0.9 },
              labelBgPadding: [4, 4] as [number, number],
              labelBgBorderRadius: 4,
              style: { stroke: '#ef4444', strokeWidth: 2 },
              animated: false
            })
          }
        })
      } else {
        // Text response or default routing - use next_step_id
        if (step.next_step_id) {
          const targetNodeId = stepIdToNodeIdMap?.get(step.next_step_id)
          if (targetNodeId) {
            humanInputEdges.push({
              id: `${sourceNodeId}-human-input-to-${targetNodeId}`,
              source: sourceNodeId,
              target: targetNodeId,
              type: 'smoothstep',
              style: { stroke: '#6b7280', strokeWidth: 2 },
              animated: false
            })
          } else if (step.next_step_id === 'end') {
            humanInputEdges.push({
              id: `${sourceNodeId}-human-input-to-end`,
              source: sourceNodeId,
              target: 'end',
              type: 'smoothstep',
              style: { stroke: '#6b7280', strokeWidth: 2 },
              animated: false
            })
          }
        }
      }

      edges.push(...humanInputEdges)

      // Human input steps handle their own routing - don't connect to next sequential step
      lastExitNodeId = null
    }

    // Handle todo_task step edge routing
    // Todo task steps have predefined routes (sub-agents)
    // and optionally a generic agent. After sub-agents complete, they return to the main todo task node.
    // The todo task step connects to next_step_id when all tasks are complete.
    if (isTodoTaskStep(step)) {
      const todoTaskGraph = buildTodoTaskSubAgentGraph(
        step,
        node.id,
        node.data as TodoTaskNodeData,
        true
      )

      nodes.push(...todoTaskGraph.nodes)
      edges.push(...todoTaskGraph.edges)

      // Todo task steps handle their own routing - don't connect to next sequential step
      lastExitNodeId = null
    }

    // Handle message_sequence step next_step_id: draw an EXPLICIT edge to the
    // target. Without this, a sequence's next_step_id only connected via array
    // order, so when several routes' sequences all point at the same downstream
    // step (e.g. each portal -> normalize), only the last one linked and the
    // shared step looked unconnected. Now every sequence draws its own edge, so
    // the convergence (shared finish line) is visible.
    if (isMessageSequenceStep(step) && step.next_step_id) {
      const sourceNodeId = (typeof lastExitNodeId === 'string' ? lastExitNodeId : node.id)
      const targetNodeId = step.next_step_id === 'end' ? 'end' : stepIdToNodeIdMap?.get(step.next_step_id)
      if (targetNodeId) {
        edges.push({
          id: `${sourceNodeId}-msgseq-next-to-${targetNodeId}`,
          source: sourceNodeId,
          target: targetNodeId,
          type: 'smoothstep',
          animated: false,
          style: { stroke: '#6b7280', strokeWidth: 2 }
        })
        lastExitNodeId = null // explicit next_step_id edge created; don't also array-chain
      }
    }
  })

  return { nodes, edges }
}

/**
 * Check if a node is a step-type node (has step data)
 */
function isStepTypeNode(node: WorkflowNode): node is WorkflowNode & { data: StepNodeData | ConditionalNodeData | TodoTaskNodeData | HumanInputNodeData | MessageSequenceNodeData } {
  return node.type === 'step' || node.type === 'conditional' || node.type === 'todo_task' || node.type === 'human_input' || node.type === 'message_sequence'
}

/**
 * Create edges based on context dependencies
 */
function createDependencyEdges(nodes: WorkflowNode[]): WorkflowEdge[] {
  const edges: WorkflowEdge[] = []

  // Filter to only step-type nodes (not validation/learning)
  const stepNodes = nodes.filter(isStepTypeNode)

  // Create a map of context_output to node ID
  const outputToNodeMap = new Map<string, string>()
  stepNodes.forEach(node => {
    const step = node.data.step
    if (step.context_output) {
      const outputs = Array.isArray(step.context_output)
        ? step.context_output
        : [step.context_output]
      outputs.forEach((output: string) => {
        outputToNodeMap.set(output, node.id)
      })
    }
  })

  // Create edges for context dependencies
  stepNodes.forEach(node => {
    const step = node.data.step
    if (step.context_dependencies && step.context_dependencies.length > 0) {
      step.context_dependencies.forEach((dep: string) => {
        const sourceNodeId = outputToNodeMap.get(dep)
        if (sourceNodeId && sourceNodeId !== node.id) {
          // Shorten the dependency label for readability
          const shortLabel = dep.length > 20 ? dep.substring(0, 18) + '...' : dep
          edges.push({
            id: `dep-${sourceNodeId}-to-${node.id}-${dep}`,
            source: sourceNodeId,
            target: node.id,
            type: 'smoothstep',
            style: { stroke: '#8b5cf6', strokeDasharray: '4,4', strokeWidth: 1.5, opacity: 0.7 },
            animated: false,
            label: shortLabel,
            labelStyle: { fill: '#8b5cf6', fontSize: 9, fontWeight: 500 },
            labelBgStyle: { fill: '#f5f3ff', fillOpacity: 0.85 },
            labelBgPadding: [3, 3] as [number, number],
            labelBgBorderRadius: 3
          })
        }
      })
    }
  })

  return edges
}

/**
 * Hook to convert plan.json to React Flow nodes and edges
 */
export function usePlanToFlow(
  plan: PlanningResponse | null,
  options: UsePlanToFlowOptions = {}
): UsePlanToFlowResult {
  const {
    showDependencyEdges = false,
    changes = null,
    completedStepIndices = [],
    stepStatusMap,
    variablesManifest = null,
    onOpenVariablesSidebar,
    isLoadingVariables = false,
    layoutDirection = 'TB',
    disabled = false
  } = options

  // Get preset for code execution mode default
  const activePreset = useActiveWorkflowPreset()

  const presetUseCodeExecutionMode = activePreset?.useCodeExecutionMode ?? false

  // Get preset LLM configs
  const presetLLMConfig = activePreset?.llmConfig || undefined
  // Get available LLMs for model name formatting
  const { availableLLMs } = useLLMStore()

  // Convert serialized stepStatusMap to Map if needed, and create stable reference for dependency comparison
  const stepStatusMapSerialized = useMemo(() => {
    if (!stepStatusMap) return null
    // If it's already a Map, serialize it for stable comparison
    if (stepStatusMap instanceof Map) {
      return Object.fromEntries(stepStatusMap)
    }
    // If it's already an object, return as-is
    return stepStatusMap
  }, [stepStatusMap])

  // Use a ref for stepStatusMap so status changes don't trigger full node recalculation.
  // Status updates are handled by the fast-path effect in WorkflowCanvas (setNodes in-place).
  const stepStatusMapRef = useRef<Map<string, 'pending' | 'running' | 'completed' | 'failed'> | undefined>(undefined)
  useEffect(() => {
    if (!stepStatusMapSerialized) {
      stepStatusMapRef.current = undefined
    } else {
      stepStatusMapRef.current = new Map(Object.entries(stepStatusMapSerialized)) as Map<string, 'pending' | 'running' | 'completed' | 'failed'>
    }
  }, [stepStatusMapSerialized])
  // Also keep a computed value for initial render (ref won't be set yet on first render)
  const stepStatusMapAsMap = stepStatusMapRef.current ?? (
    stepStatusMapSerialized
      ? new Map(Object.entries(stepStatusMapSerialized)) as Map<string, 'pending' | 'running' | 'completed' | 'failed'>
      : undefined
  )

  const lastComputedFlowRef = useRef<UsePlanToFlowResult>({ nodes: [], edges: [] })

  return useMemo(() => {
    if (disabled) {
      return lastComputedFlowRef.current
    }

    if (!plan || !plan.steps || plan.steps.length === 0) {
      const emptyResult = { nodes: [], edges: [] }
      lastComputedFlowRef.current = emptyResult
      return emptyResult
    }

    // Convert completedStepIndices to completedStepIds (Set of step IDs) for step_id-based matching
    // This ensures we match by step_id instead of index for better reliability
    const completedStepIds = new Set<string>()
    const convertIndicesToIds = (steps: PlanStep[], indices: number[]) => {
      indices.forEach(index => {
        if (index >= 0 && index < steps.length) {
          const step = steps[index]
          if (step?.id) {
            completedStepIds.add(step.id)
          }
        }
      })
    }
    convertIndicesToIds(plan.steps, completedStepIndices)

    // Create step ID to node ID map for next_step_id lookups
    // First pass: create all nodes to build the map
    const stepIdToNodeIdMap = new Map<string, string>()
    const buildStepIdMap = (steps: PlanStep[], parentId?: string, branchType?: 'true' | 'false') => {
      steps.forEach((step, index) => {
        const nodeId = parentId
          ? `${parentId}-${branchType}-${index}`
          : step.id || `step-${index}`
        if (step.id) {
          stepIdToNodeIdMap.set(step.id, nodeId)
        }
        // Recursively process branch steps
        if (isConditionalStep(step)) {
          if (isConditionalStep(step)) {
            if (step.if_true_steps) {
              buildStepIdMap(step.if_true_steps, nodeId, 'true')
            }
            if (step.if_false_steps) {
              buildStepIdMap(step.if_false_steps, nodeId, 'false')
            }
          }
        }
      })
    }
    buildStepIdMap(plan.steps)

    // Also map orphan step IDs
    if (plan.orphan_steps) {
      plan.orphan_steps.forEach((step, index) => {
        const nodeId = `orphan-${step.id || `step-${index}`}`
        if (step.id) {
          stepIdToNodeIdMap.set(step.id, nodeId)
        }
      })
    }

    // Process all steps to create nodes and sequential edges (with change highlighting)
    const { nodes: processedNodes, edges: sequentialEdges } = processSteps(
      plan.steps,
      undefined,
      undefined,
      changes,
      presetUseCodeExecutionMode,
      presetLLMConfig,
      availableLLMs,
      completedStepIndices,
      stepStatusMapAsMap,
      options.workspacePath,
      options.selectedRunFolder,
      stepIdToNodeIdMap,
      completedStepIds
    )

    // Process orphan steps (workshop-only, not connected to main flow)
    let orphanNodes: WorkflowNode[] = []
    if (plan.orphan_steps && plan.orphan_steps.length > 0) {
      const { nodes: orphanProcessedNodes } = processSteps(
        plan.orphan_steps,
        undefined,
        undefined,
        changes,
        presetUseCodeExecutionMode,
        presetLLMConfig,
        availableLLMs,
        [],  // no completed step indices for orphan steps
        stepStatusMapAsMap,
        options.workspacePath,
        options.selectedRunFolder,
        stepIdToNodeIdMap,
        new Set<string>()  // no completed step IDs for orphan steps
      )

      // Mark all orphan nodes and remap IDs with 'orphan-' prefix. Attach how
      // many routes reuse each orphan via orphan_step_ref so the node can show
      // whether it's a shared/reused definition or genuinely unused.
      const orphanReuseCounts = countOrphanStepRefs([...(plan.steps || []), ...(plan.orphan_steps || [])])
      orphanNodes = orphanProcessedNodes.map((node) => {
        const origId = (node.data as { id?: string }).id || node.id
        return {
          ...node,
          id: `orphan-${node.id}`,
          data: {
            ...node.data,
            isOrphan: true,
            orphanReuseCount: orphanReuseCounts.get(origId) || 0,
          }
        }
      })
    }

    // Add orphan section label node if there are orphan steps
    if (orphanNodes.length > 0) {
      const orphanLabelNode: WorkflowNode = {
        id: 'orphan-label',
        type: 'start',  // Reuse start node type for simple label
        position: { x: 0, y: 0 },
        data: {
          id: 'orphan-label',
          title: 'Orphan Steps (workshop-only)',
          status: 'pending' as const,
          stepIndex: -1,
          step: {} as PlanStep
        }
      }
      orphanNodes = [orphanLabelNode, ...orphanNodes]
    }

    // Add start node
    const startNode: WorkflowNode = {
      id: 'start',
      type: 'start',
      position: { x: 0, y: 0 },
      data: {
        id: 'start',
        title: 'Start',
        status: 'completed',
        stepIndex: -1,
        step: {} as PlanStep
      }
    }

    // Add variables node (between start and first step)
    const variablesNode: WorkflowNode = {
      id: 'variables',
      type: 'variables',
      position: { x: 0, y: 0 },
      data: {
        manifest: variablesManifest,
        onOpenSidebar: onOpenVariablesSidebar,
        isLoading: isLoadingVariables
      } as VariablesNodeData
    }

    // Add end node
    const endNode: WorkflowNode = {
      id: 'end',
      type: 'end',
      position: { x: 0, y: 0 },
      data: {
        id: 'end',
        title: 'End',
        status: 'pending',
        stepIndex: -1,
        step: {} as PlanStep
      }
    }

    // Node order: Start -> Execution Settings -> Variables -> Steps -> End (+ orphan nodes)
    const nodes = [startNode, variablesNode, ...processedNodes, endNode, ...orphanNodes]

    // Create edges: Start -> Execution Settings -> Variables -> First step (or End if no steps)
    const edges: WorkflowEdge[] = []

    edges.push({
      id: 'start-to-variables',
      source: 'start',
      target: 'variables',
      type: 'smoothstep',
      style: { stroke: '#6b7280', strokeWidth: 2 }
    })

    // Variables to first step (or to End if no steps)
    if (processedNodes.length > 0) {
      edges.push({
        id: 'variables-to-first',
        source: 'variables',
        target: processedNodes[0].id,
        type: 'smoothstep',
        style: { stroke: '#6b7280', strokeWidth: 2 }
      })
    } else {
      // Connect Variables to End if no steps
      edges.push({
        id: 'variables-to-end',
        source: 'variables',
        target: 'end',
        type: 'smoothstep',
        style: { stroke: '#6b7280', strokeWidth: 2 }
      })
    }

    // Add sequential edges
    edges.push(...sequentialEdges)

    // Create dependency edges (context flow) - only if enabled
    if (showDependencyEdges) {
      const dependencyEdges = createDependencyEdges(processedNodes)
      edges.push(...dependencyEdges)
    }

    // Find last node to connect to end (could be step, validation, or learning node)
    // We want the actual last node in the sequence (considering validation/learning nodes)
    const topLevelNodes = processedNodes.filter(n => !n.id.includes('-true-') && !n.id.includes('-false-'))
    if (topLevelNodes.length > 0) {
      // Find the last node - could be learning, validation, or step
      const lastNode = topLevelNodes[topLevelNodes.length - 1]

      // Check if it's a step-type node and if it has a condition
      const isStepType = isStepTypeNode(lastNode)
      const hasCondition = isStepType && isConditionalStep(lastNode.data.step)

      if (!hasCondition) {
        // Regular step - connect to end
        edges.push({
          id: 'last-to-end',
          source: lastNode.id,
          target: 'end',
          type: 'smoothstep',
          style: { stroke: '#6b7280', strokeWidth: 2 }
        })
      } else {
        // Conditional step - check if branch exit nodes need to connect to end
        const conditionalStep = lastNode.data.step as PlanStep
        const stepId = conditionalStep.id || lastNode.id

        // Check which edges already exist to "end" (from explicit next_step_id)
        const existingEndEdges = edges.filter(e => e.target === 'end' && (
          e.id.includes(stepId) ||
          e.id.includes(lastNode.id) ||
          e.source === lastNode.id
        ))

        // Find branch exit nodes that need connection to end
        // These are nodes that don't already have an edge to "end" or to another step
        const branchExitNodes: string[] = []

        // Check true branch
        if (isConditionalStep(conditionalStep) && conditionalStep.if_true_steps && conditionalStep.if_true_steps.length > 0) {
          // Branch has steps - find the last node in the branch
          const trueBranchNodes = processedNodes.filter(n =>
            n.id.startsWith(`${lastNode.id}-true-`) &&
            !n.id.includes('-false-')
          )
          if (trueBranchNodes.length > 0) {
            const lastTrueNode = trueBranchNodes[trueBranchNodes.length - 1]
            // Check if this node already has an edge to end or to another step
            const hasConnection = edges.some(e =>
              e.source === lastTrueNode.id && (e.target === 'end' || !e.target.includes('-true-') && !e.target.includes('-false-'))
            )
            if (!hasConnection && (!isConditionalStep(conditionalStep) || !conditionalStep.if_true_next_step_id)) {
              branchExitNodes.push(lastTrueNode.id)
            }
          }
        } else {
          // Empty true branch - conditional node itself is the exit
          const hasConnection = existingEndEdges.some(e =>
            e.id.includes('true') || (e.source === lastNode.id && e.id.includes('true'))
          )
          if (!hasConnection && (!isConditionalStep(conditionalStep) || !conditionalStep.if_true_next_step_id)) {
            branchExitNodes.push(lastNode.id)
          }
        }

        // Check false branch
        if (isConditionalStep(conditionalStep) && conditionalStep.if_false_steps && conditionalStep.if_false_steps.length > 0) {
          // Branch has steps - find the last node in the branch
          const falseBranchNodes = processedNodes.filter(n =>
            n.id.startsWith(`${lastNode.id}-false-`) &&
            !n.id.includes('-true-')
          )
          if (falseBranchNodes.length > 0) {
            const lastFalseNode = falseBranchNodes[falseBranchNodes.length - 1]
            // Check if this node already has an edge to end or to another step
            const hasConnection = edges.some(e =>
              e.source === lastFalseNode.id && (e.target === 'end' || !e.target.includes('-true-') && !e.target.includes('-false-'))
            )
            if (!hasConnection && (!isConditionalStep(conditionalStep) || !conditionalStep.if_false_next_step_id)) {
              branchExitNodes.push(lastFalseNode.id)
            }
          }
        } else {
          // Empty false branch - conditional node itself is the exit (if not already added)
          if (!branchExitNodes.includes(lastNode.id)) {
            const hasConnection = existingEndEdges.some(e =>
              e.id.includes('false') || (e.source === lastNode.id && e.id.includes('false'))
            )
            if (!hasConnection && (!isConditionalStep(conditionalStep) || !conditionalStep.if_false_next_step_id)) {
              branchExitNodes.push(lastNode.id)
            }
          }
        }

        // Connect branch exit nodes to end
        branchExitNodes.forEach((exitNodeId, index) => {
          // Determine label based on which branch this is
          const isTrueBranch = isConditionalStep(conditionalStep) && conditionalStep.if_true_steps && conditionalStep.if_true_steps.length === 0 && exitNodeId === lastNode.id
            ? true
            : exitNodeId.includes('-true-') || (exitNodeId === lastNode.id && isConditionalStep(conditionalStep) && conditionalStep.if_true_steps && conditionalStep.if_true_steps.length === 0)
          const isFalseBranch = !isTrueBranch && (
            exitNodeId.includes('-false-') ||
            (exitNodeId === lastNode.id && isConditionalStep(conditionalStep) && conditionalStep.if_false_steps && conditionalStep.if_false_steps.length === 0)
          )

          edges.push({
            id: `${exitNodeId}-to-end-conditional-${index}`,
            source: exitNodeId,
            target: 'end',
            type: 'smoothstep',
            label: isTrueBranch ? 'Yes' : isFalseBranch ? 'No' : undefined,
            labelStyle: isTrueBranch || isFalseBranch ? {
              fill: isTrueBranch ? '#22c55e' : '#ef4444',
              fontWeight: 600,
              fontSize: 11
            } : undefined,
            labelBgStyle: isTrueBranch || isFalseBranch ? {
              fill: isTrueBranch ? '#f0fdf4' : '#fef2f2',
              fillOpacity: 0.9
            } : undefined,
            labelBgPadding: isTrueBranch || isFalseBranch ? [4, 4] as [number, number] : undefined,
            labelBgBorderRadius: isTrueBranch || isFalseBranch ? 4 : undefined,
            style: {
              stroke: isTrueBranch ? '#22c55e' : isFalseBranch ? '#ef4444' : '#6b7280',
              strokeWidth: 2
            }
          })
        })
      }
    }

    // CRITICAL: Position header nodes BEFORE Dagre runs
    // This ensures they're excluded from Dagre and maintain horizontal layout
    const HEADER_GAP = layoutDirection === 'TB' ? 40 : 100
    const HEADER_START_X = 80
    const HEADER_Y = 80
    
    // Position header nodes horizontally BEFORE Dagre
    const headerNodesWithPositions = nodes.map(node => {
      if (node.id === 'start') {
        return { ...node, position: { x: HEADER_START_X, y: HEADER_Y } }
      }
      if (node.id === 'variables') {
        const startDims = NODE_DIMENSIONS.start
        const varsX = HEADER_START_X + startDims.width + HEADER_GAP
        return { ...node, position: { x: varsX, y: HEADER_Y } }
      }
      return node
    })

    // Apply dagre layout (header nodes are excluded from Dagre)
    const layoutedResult = layoutWithDagre(headerNodesWithPositions, edges, layoutDirection)

    // Header nodes are already positioned correctly above, but verify and ensure they stay horizontal
    const HEADER_TO_WORKFLOW_GAP = layoutDirection === 'TB' ? 180 : 150

    const startNodeIndex = layoutedResult.nodes.findIndex(n => n.id === 'start')
    const variablesNodeIndex = layoutedResult.nodes.findIndex(n => n.id === 'variables')

    if (startNodeIndex !== -1 && variablesNodeIndex !== -1) {
      const startDims = NODE_DIMENSIONS.start
      const variablesDims = NODE_DIMENSIONS.variables

      // Calculate max height for vertical centering
      const maxHeaderHeight = Math.max(startDims.height, variablesDims.height)

      // CRITICAL: Enforce header node positions (they were set before Dagre, but ensure they're still correct)
      // Since header nodes are excluded from Dagre, they should already have correct positions
      // But we enforce them here to be absolutely sure
      // TEST: Using same large gaps as above
      let startPos = { x: HEADER_START_X, y: HEADER_Y }
      let varsPos = { x: HEADER_START_X + startDims.width + HEADER_GAP, y: HEADER_Y }

      // Enforce positions (even though they should already be correct since header nodes are excluded from Dagre)
      layoutedResult.nodes[startNodeIndex] = {
        ...layoutedResult.nodes[startNodeIndex],
        position: startPos
      }
      layoutedResult.nodes[variablesNodeIndex] = {
        ...layoutedResult.nodes[variablesNodeIndex],
        position: varsPos
      }

      // Header positions are now correctly set horizontally

      // Calculate where the workflow should start (after the header row)
      const headerRowEndX = varsPos.x + variablesDims.width
      const headerRowBottom = HEADER_Y + maxHeaderHeight

      // Find the first step node (step-0 or the first non-header node connected to variables)
      const firstStepNode = layoutedResult.nodes.find(n =>
        n.id === 'step-0' ||
        (isStepTypeNode(n) && !n.id.includes('-true-') && !n.id.includes('-false-') && !n.id.includes('-sub-agent-'))
      )

      if (firstStepNode) {
        // Calculate the leftmost point of this node (accounting for sub-agent overflow if it's a compound node)
        let firstStepLeftEdge = firstStepNode.position.x
        if (firstStepNode.type === 'todo_task') {
          const data = firstStepNode.data as TodoTaskNodeData
          const routes = (data as TodoTaskNodeData).predefined_routes
          const numSubAgents = routes?.length || 0
          
          if (numSubAgents > 0 && layoutDirection === 'LR') {
            const subAgentRowWidth = getSubAgentGridMetrics(numSubAgents, layoutDirection).width
            const parentWidth = 300
            if (subAgentRowWidth > parentWidth) {
              // Sub-agents extend further left than the parent card
              const overflow = (subAgentRowWidth - parentWidth) / 2
              firstStepLeftEdge -= overflow
            }
          }
        }

        if (layoutDirection === 'TB') {
          // Start and Variables stack VERTICALLY on the left (Start on top), and
          // the workflow graph sits to the RIGHT of that column, top-aligned with
          // Start. This keeps the tall, expandable Variables panel beside the
          // graph instead of on top of it, so it never overlaps workflow nodes.
          const VERTICAL_HEADER_GAP = 24
          const COLUMN_TO_WORKFLOW_GAP = 140
          const headerColumnWidth = Math.max(startDims.width, variablesDims.width)

          startPos = { x: HEADER_START_X, y: HEADER_Y }
          varsPos = { x: HEADER_START_X, y: HEADER_Y + startDims.height + VERTICAL_HEADER_GAP }
          layoutedResult.nodes[startNodeIndex] = { ...layoutedResult.nodes[startNodeIndex], position: startPos }
          layoutedResult.nodes[variablesNodeIndex] = { ...layoutedResult.nodes[variablesNodeIndex], position: varsPos }

          // Shift the whole dagre-laid workflow right of the header column and
          // align its top with Start. Measure the workflow's bounding box first.
          let workflowMinX = Number.POSITIVE_INFINITY
          let workflowMinY = Number.POSITIVE_INFINITY
          layoutedResult.nodes.forEach((node, index) => {
            if (node.id === 'start' || node.id === 'variables') return
            if (index === startNodeIndex || index === variablesNodeIndex) return
            if (node.type === 'end') return
            workflowMinX = Math.min(workflowMinX, node.position.x)
            workflowMinY = Math.min(workflowMinY, node.position.y)
          })
          if (!Number.isFinite(workflowMinX)) { workflowMinX = 0; workflowMinY = 0 }

          const offsetX = (HEADER_START_X + headerColumnWidth + COLUMN_TO_WORKFLOW_GAP) - workflowMinX
          const offsetY = HEADER_Y - workflowMinY

          layoutedResult.nodes = layoutedResult.nodes.map((node, index) => {
            if (node.id === 'start' || node.id === 'variables') return node
            if (index === startNodeIndex || index === variablesNodeIndex) return node
            if (node.type === 'end') return node
            return { ...node, position: { x: node.position.x + offsetX, y: node.position.y + offsetY } }
          })
        } else {
          // LR mode: workflow flows horizontally, so first step should be to the right of header
          const firstStepTargetX = headerRowEndX + HEADER_TO_WORKFLOW_GAP
          // Align the first step vertically with the center of the header row
          const firstStepTargetY = HEADER_Y + maxHeaderHeight / 2

          // Calculate offset to shift all workflow nodes
          // Use firstStepLeftEdge to ensure sub-agents don't overlap with header
          const offsetX = firstStepTargetX - firstStepLeftEdge
          const offsetY = firstStepTargetY - firstStepNode.position.y

          // Shift all non-header nodes by this offset
          layoutedResult.nodes = layoutedResult.nodes.map((node, index) => {
            // CRITICAL: Never shift header nodes - check by ID, not just index
            if (node.id === 'start' || node.id === 'variables') {
              return node // Keep header nodes in place
            }
            if (index === startNodeIndex || index === variablesNodeIndex) {
              return node // Also check by index as backup
            }
            if (node.type === 'end') {
              // End node - will be repositioned later
              return node
            }
            return {
              ...node,
              position: {
                x: node.position.x + offsetX,
                y: node.position.y + offsetY
              }
            }
          })
        }
      }
    }

    // Routing branches are spread by dagre (subtree-aware), not the manual
    // lane layout — disabled as part of the dagre-only simplification.

    // Position sub-agents relative to their parent todo_task nodes.
    // TB is the active canvas layout: children form a vertical tree, with
    // sibling branches spread horizontally by their recursive footprint.
    const parentNodeMap = new Map<string, { nodeIndex: number; subAgentIndices: number[] }>()

    // Pass 1: Find all todo task nodes first to initialize map
    layoutedResult.nodes.forEach((node, index) => {
      if (node.type === 'todo_task') {
        parentNodeMap.set(node.id, { nodeIndex: index, subAgentIndices: [] })
      }
    })

    // Pass 2: Find all sub-agents and attach to their immediate parent
    const todoTaskParentIds = new Set(parentNodeMap.keys())
    layoutedResult.nodes.forEach((node, index) => {
      if (node.id.includes('-sub-agent-')) {
        const parentId = getImmediateSubAgentParentId(node.id, todoTaskParentIds)
        if (!parentId) return
        const parentInfo = parentNodeMap.get(parentId)
        if (parentInfo) {
          parentInfo.subAgentIndices.push(index)
        }
      }
    })

    // Position sub-agents based on layout direction
    parentNodeMap.forEach(({ nodeIndex: parentNodeIndex, subAgentIndices }) => {
      const parentNode = layoutedResult.nodes[parentNodeIndex]
      const parentDimensions = getNodeLayoutDimensions(parentNode)

      if (subAgentIndices.length === 0) return

      const subAgentDimensions = subAgentIndices.map(index => getNodeLayoutDimensions(layoutedResult.nodes[index]))
      const subAgentFootprints = subAgentIndices.map(index =>
        getNodeFootprintDimensions(layoutedResult.nodes[index], layoutedResult.nodes, todoTaskParentIds, layoutDirection)
      )

      const subAgentGrid = getSubAgentGridMetricsFromDimensions(subAgentFootprints, layoutDirection)
      const columnWidths = subAgentGrid.columnWidths
      const rowHeights = subAgentGrid.rowHeights
      const startX = parentNode.position.x + (parentDimensions.width - subAgentGrid.width) / 2
      const startY = parentNode.position.y + parentDimensions.height + SUB_AGENT_LAYOUT.parentGap

      subAgentIndices.forEach((subAgentIndex, index) => {
        const subAgent = layoutedResult.nodes[subAgentIndex]
        const dimensions = subAgentDimensions[index]
        const footprint = subAgentFootprints[index]
        const column = index % subAgentGrid.columns
        const row = Math.floor(index / subAgentGrid.columns)
        const cellX = startX + columnWidths.slice(0, column).reduce((sum, width) => sum + width, 0) + (column * SUB_AGENT_LAYOUT.cellGap)
        const cellY = startY + rowHeights.slice(0, row).reduce((sum, height) => sum + height, 0) + (row * SUB_AGENT_LAYOUT.cellGap)

        layoutedResult.nodes[subAgentIndex] = {
          ...subAgent,
          position: {
            x: cellX + (footprint.width - dimensions.width) / 2,
            y: cellY
          }
        }
      })
    })

    // After Dagre + todo_task positioning, keep validation/learning/evaluation nodes
    // visually close to their parent step/decision nodes (but with overall higher spacing)
    const positionedNodes: WorkflowNode[] = layoutedResult.nodes.map(node => ({ ...node }))
    const nodeIndexById = new Map<string, number>()
    positionedNodes.forEach((node, index) => {
      nodeIndexById.set(node.id, index)
    })

    const getDimensions = (type: string | undefined) => {
      if (!type) {
        return NODE_DIMENSIONS.step
      }
      return NODE_DIMENSIONS[type as keyof typeof NODE_DIMENSIONS] || NODE_DIMENSIONS.step
    }

    // Group validation, learning, and evaluation nodes by their parent step ID
    const validationByParent = new Map<string, WorkflowNode>()
    const learningByParent = new Map<string, WorkflowNode>()
    const evaluationByParent = new Map<string, WorkflowNode[]>()

    positionedNodes.forEach(node => {
      if (node.type === 'validation') {
        const data = node.data as ValidationNodeData
        if (data.parentStepId && !data.parentStepId.includes('-sub-agent-') && !node.id.includes('-sub-agent-')) {
          validationByParent.set(data.parentStepId, node)
        }
      } else if (node.type === 'learning') {
        const data = node.data as LearningNodeData
        if (data.parentStepId && !data.parentStepId.includes('-sub-agent-') && !node.id.includes('-sub-agent-')) {
          learningByParent.set(data.parentStepId, node)
        }
      } else if (node.type === 'evaluation') {
        const data = node.data as EvaluationNodeData
        if (data.parentStepId && !data.parentStepId.includes('-sub-agent-') && !node.id.includes('-sub-agent-')) {
          const list = evaluationByParent.get(data.parentStepId) || []
          list.push(node)
          evaluationByParent.set(data.parentStepId, list)
        }
      }
    })

    // Position validation nodes just to the right of their parent step
    validationByParent.forEach((validationNode, parentId) => {
      const parentIndex = nodeIndexById.get(parentId)
      if (parentIndex === undefined) return
      const parentNode = positionedNodes[parentIndex]
      const parentDims = getDimensions(parentNode.type)
      const valDims = getDimensions(validationNode.type)

      const baseX = parentNode.position.x + parentDims.width + 48
      const baseY = parentNode.position.y + (parentDims.height - valDims.height) / 2

      const validationIndex = nodeIndexById.get(validationNode.id)
      if (validationIndex === undefined) return
      positionedNodes[validationIndex] = {
        ...positionedNodes[validationIndex],
        position: { x: baseX, y: baseY }
      }
    })

    // Position learning nodes to the right of validation (if present) or parent step
    learningByParent.forEach((learningNode, parentId) => {
      const learningIndex = nodeIndexById.get(learningNode.id)
      if (learningIndex === undefined) return

      const validationNode = validationByParent.get(parentId)
      let anchorNode: WorkflowNode | null = null
      if (validationNode) {
        const vIndex = nodeIndexById.get(validationNode.id)
        if (vIndex !== undefined) {
          anchorNode = positionedNodes[vIndex]
        }
      }

      if (!anchorNode) {
        const parentIndex = nodeIndexById.get(parentId)
        if (parentIndex === undefined) return
        anchorNode = positionedNodes[parentIndex]
      }

      const anchorDims = getDimensions(anchorNode.type)
      const learnDims = getDimensions(learningNode.type)

      const baseX = anchorNode.position.x + anchorDims.width + 36
      const baseY = anchorNode.position.y + (anchorDims.height - learnDims.height) / 2

      positionedNodes[learningIndex] = {
        ...positionedNodes[learningIndex],
        position: { x: baseX, y: baseY }
      }
    })

    // Position evaluation nodes to the right of learning (preferred) or parent decision node
    evaluationByParent.forEach((evalNodes, parentId) => {
      // Determine anchor: learning node if available, otherwise parent step/decision node
      let anchorNode: WorkflowNode | null = null
      const learningNode = learningByParent.get(parentId)
      if (learningNode) {
        const lIndex = nodeIndexById.get(learningNode.id)
        if (lIndex !== undefined) {
          anchorNode = positionedNodes[lIndex]
        }
      }

      if (!anchorNode) {
        const parentIndex = nodeIndexById.get(parentId)
        if (parentIndex === undefined) return
        anchorNode = positionedNodes[parentIndex]
      }

      const anchorDims = getDimensions(anchorNode.type)

      evalNodes.forEach((evalNode, index) => {
        const evalIndex = nodeIndexById.get(evalNode.id)
        if (evalIndex === undefined) return

        const evalDims = getDimensions(evalNode.type)

        const horizontalOffset = 48
        const verticalGap = 24

        // Slight vertical staggering if there are multiple evaluation nodes for same parent
        const offsetY = index * (evalDims.height + verticalGap)

        const baseX = anchorNode!.position.x + anchorDims.width + horizontalOffset
        const baseY = anchorNode!.position.y + (anchorDims.height - evalDims.height) / 2 + offsetY

        positionedNodes[evalIndex] = {
          ...positionedNodes[evalIndex],
          position: { x: baseX, y: baseY }
        }
      })
    })

    // Replace nodes with the adjusted positions
    layoutedResult.nodes = positionedNodes

    // Position the end node at the end of the workflow
    const endNodeIndex = layoutedResult.nodes.findIndex(n => n.id === 'end')
    if (endNodeIndex !== -1) {
      const endDims = NODE_DIMENSIONS.end
      // Find all workflow nodes (exclude header and end nodes)
      const workflowNodes = layoutedResult.nodes.filter(n =>
        n.id !== 'start' && n.id !== 'variables' && n.id !== 'end'
      )

      if (workflowNodes.length > 0) {
        if (layoutDirection === 'TB') {
          // TB mode: end node at the bottom, centered horizontally
          const maxY = Math.max(...workflowNodes.map(n => {
            const dims = NODE_DIMENSIONS[n.type as keyof typeof NODE_DIMENSIONS] || NODE_DIMENSIONS.step
            return n.position.y + dims.height
          }))
          const avgX = workflowNodes.reduce((sum, n) => sum + n.position.x, 0) / workflowNodes.length
          const workflowWidth = workflowNodes.reduce((max, n) => {
            const dims = NODE_DIMENSIONS[n.type as keyof typeof NODE_DIMENSIONS] || NODE_DIMENSIONS.step
            return Math.max(max, dims.width)
          }, 0)

          layoutedResult.nodes[endNodeIndex] = {
            ...layoutedResult.nodes[endNodeIndex],
            position: {
              x: avgX + (workflowWidth - endDims.width) / 2,
              y: maxY + 100
            }
          }
        } else {
          // LR mode: end node at the right, centered vertically with the workflow
          const maxX = Math.max(...workflowNodes.map(n => {
            const dims = NODE_DIMENSIONS[n.type as keyof typeof NODE_DIMENSIONS] || NODE_DIMENSIONS.step
            return n.position.x + dims.width
          }))
          const minY = Math.min(...workflowNodes.map(n => n.position.y))
          const maxY = Math.max(...workflowNodes.map(n => {
            const dims = NODE_DIMENSIONS[n.type as keyof typeof NODE_DIMENSIONS] || NODE_DIMENSIONS.step
            return n.position.y + dims.height
          }))
          const centerY = (minY + maxY) / 2

          layoutedResult.nodes[endNodeIndex] = {
            ...layoutedResult.nodes[endNodeIndex],
            position: {
              x: maxX + 100,
              y: centerY - endDims.height / 2
            }
          }
        }
      }
    }

    // Global collision resolution is disabled — dagre already separates nodes by
    // their subtree extent (nodesep is the floor), so the extra shove-apart pass
    // (which ignored tree grouping) is no longer needed.

    if (layoutDirection === 'TB') {
      const startHeaderNode = layoutedResult.nodes.find(node => node.id === 'start')
      const variablesHeaderNode = layoutedResult.nodes.find(node => node.id === 'variables')
      const workflowNodes = layoutedResult.nodes.filter(node =>
        node.id !== 'start' &&
        node.id !== 'variables' &&
        !node.id.startsWith('orphan-')
      )

      if (startHeaderNode && variablesHeaderNode && workflowNodes.length > 0) {
        const startDims = getNodeLayoutDimensions(startHeaderNode)
        const variablesDims = getNodeLayoutDimensions(variablesHeaderNode)
        const headerBottom = Math.max(
          startHeaderNode.position.y + startDims.height,
          variablesHeaderNode.position.y + variablesDims.height
        )
        const minWorkflowTop = headerBottom + HEADER_TO_WORKFLOW_GAP
        const currentWorkflowTop = Math.min(...workflowNodes.map(node => node.position.y))
        const shiftY = minWorkflowTop - currentWorkflowTop

        if (shiftY > 0) {
          layoutedResult.nodes = layoutedResult.nodes.map(node => {
            if (node.id === 'start' || node.id === 'variables' || node.id.startsWith('orphan-')) {
              return node
            }
            return {
              ...node,
              position: {
                x: node.position.x,
                y: node.position.y + shiftY
              }
            }
          })
        }
      }
    }

    // Inject read-only context into step-type nodes.
    // Also make validation, learning, and evaluation nodes non-draggable
    layoutedResult.nodes = layoutedResult.nodes.map(node => {
      if (node.type === 'step' || node.type === 'conditional' || node.type === 'human_input' || node.type === 'todo_task') {
        return {
          ...node,
          data: {
            ...node.data,
            workspacePath: options.workspacePath,
            selectedRunFolder: options.selectedRunFolder
          }
        } as WorkflowNode
      }

      // Validation, learning, and evaluation nodes are now draggable (can be manually positioned)
      // They can be moved independently or will move with their parent nodes
      return node
    }) as WorkflowNode[]

    // Log critical nodes only

    // Position orphan nodes below the main flow
    if (orphanNodes.length > 0) {
      // Find max Y position of all non-orphan nodes
      const mainNodes = layoutedResult.nodes.filter(n => !n.id.startsWith('orphan-'))
      const maxY = Math.max(...mainNodes.map(n => {
        const dims = NODE_DIMENSIONS[n.type as keyof typeof NODE_DIMENSIONS] || NODE_DIMENSIONS.step
        return n.position.y + dims.height
      }))

      const ORPHAN_GAP = 200  // Gap between main flow and orphan section
      const ORPHAN_SPACING = 30  // Spacing between orphan nodes

      // Position orphan nodes below the main flow
      let currentX = HEADER_START_X
      layoutedResult.nodes = layoutedResult.nodes.map(node => {
        if (!node.id.startsWith('orphan-')) return node

        if (node.id === 'orphan-label') {
          return {
            ...node,
            position: { x: HEADER_START_X, y: maxY + ORPHAN_GAP }
          }
        }

        const dims = NODE_DIMENSIONS[node.type as keyof typeof NODE_DIMENSIONS] || NODE_DIMENSIONS.step
        const positioned = {
          ...node,
          position: { x: currentX, y: maxY + ORPHAN_GAP + 60 }  // 60px below label
        }
        currentX += dims.width + ORPHAN_SPACING
        return positioned
      })
    }

    lastComputedFlowRef.current = layoutedResult
    return layoutedResult
  // Note: stepStatusMapAsMap is intentionally NOT a dependency here.
  // Status updates are handled by the fast-path effect in WorkflowCanvas (surgical node updates),
  // so we avoid recalculating the entire node/edge layout on every status change.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [disabled, plan, showDependencyEdges, changes, presetUseCodeExecutionMode, presetLLMConfig, availableLLMs, completedStepIndices, options.workspacePath, options.selectedRunFolder, variablesManifest, onOpenVariablesSidebar, isLoadingVariables, layoutDirection])
}

export default usePlanToFlow
