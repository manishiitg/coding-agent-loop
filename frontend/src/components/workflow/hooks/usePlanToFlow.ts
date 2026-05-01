import { useMemo, useRef, useEffect } from 'react'
import type { Node, Edge } from '@xyflow/react'
import dagre from 'dagre'
import type { PlanStep, PlanningResponse, AgentLLMConfig, ValidationSchema, RoutingRoute } from '../../../utils/stepConfigMatching'
import { isConditionalStep, isHumanInputStep, isTodoTaskStep, isRoutingStep } from '../../../utils/stepConfigMatching'
import type { ChangeType, PlanChanges } from './usePlanData'
import type { VariablesManifest, EvaluationStep } from '../../../services/api-types'
import type { VariablesNodeData } from '../nodes/VariablesNode'
import { useActiveWorkflowPreset } from '../../../hooks/useActiveWorkflowPreset'
import { useLLMStore } from '../../../stores/useLLMStore'

const ROUTE_EDGE_LABEL_STYLE = { fill: 'hsl(var(--muted-foreground))', fontWeight: 500, fontSize: 10 }
const COMPLETION_EDGE_LABEL_STYLE = { fill: 'hsl(var(--primary))', fontWeight: 600, fontSize: 11 }
const EDGE_LABEL_BG_STYLE = { fill: 'hsl(var(--popover))', fillOpacity: 0.92 }

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
  nodesep: direction === 'LR' ? 200 : 150,
  ranksep: direction === 'LR' ? 150 : 200,
  marginx: 80,
  marginy: 80
})

// Node dimensions for layout calculation (base dimensions) - smaller since content is simplified
const NODE_DIMENSIONS = {
  step: { width: 280, height: 120 },
  conditional: { width: 240, height: 100 },
  routing: { width: 280, height: 200 },
  todo_task: { width: 300, height: 120 },
  human_input: { width: 260, height: 120 },
  loop: { width: 300, height: 140 },
  start: { width: 80, height: 36 },
  end: { width: 80, height: 36 },
  variables: { width: 220, height: 120 },
  'workflow-artifact': { width: 220, height: 120 }
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

  // Context files (from step.agent_configs)
  if ('step' in data && data.step && typeof data.step === 'object') {
    const step = data.step as PlanStep

    // Context files (inputs and outputs)
    const contextInputs = Array.isArray(step.context_dependencies) ? step.context_dependencies : []
    const contextOutput = step.context_output
    const contextOutputs = Array.isArray(contextOutput)
      ? contextOutput
      : (contextOutput ? [contextOutput] : [])
    if (contextInputs.length > 0 || contextOutputs.length > 0) {
      const totalFiles = contextInputs.length + contextOutputs.length
      contentHeight += 20 + (totalFiles * 20) + 8 // Base + per file + spacing
    }
  }

  // For todo_task nodes, add height for predefined routes and generic agent indicator
  if (node.type === 'todo_task') {
    const todoData = data as TodoTaskNodeData
    if (todoData.predefined_routes && todoData.predefined_routes.length > 0) {
      contentHeight += 25
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
      // Route labels wrap ~3 per row, each row ~22px
      const rows = Math.ceil(routingData.routes.length / 3)
      contentHeight += (rows * 22) + 12
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

/**
 * Calculate topology metrics to adjust layout spacing
 */
function calculateTopologyMetrics(nodes: WorkflowNode[]): { hasOrchestrator: boolean; maxOrchestratorDepth: number; maxOrchestratorSubAgents: number } {
  let hasOrchestrator = false
  let maxOrchestratorDepth = 0
  let maxOrchestratorSubAgents = 0

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
  })

  return { hasOrchestrator, maxOrchestratorDepth, maxOrchestratorSubAgents }
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
    const baseDimensions = NODE_DIMENSIONS[node.type as keyof typeof NODE_DIMENSIONS] || NODE_DIMENSIONS.step
    const estimatedHeight = estimateNodeHeight(node)
    return {
      left: node.position.x,
      right: node.position.x + baseDimensions.width,
      top: node.position.y,
      bottom: node.position.y + estimatedHeight
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
  const { maxOrchestratorSubAgents } = calculateTopologyMetrics(nodes)

  // Get config based on layout direction
  const baseConfig = getDagreConfig(direction)

  // Dynamic config based on topology
  // Increase spacing if we have todo tasks with many sub-agents
  const spacingMultiplier = maxOrchestratorSubAgents > 2 ? 1.5 : 1
  
  const dynamicConfig = {
    ...baseConfig,
    nodesep: baseConfig.nodesep * spacingMultiplier,
    ranksep: baseConfig.ranksep * spacingMultiplier
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

  // Add all nodes except excluded nodes to Dagre graph
  nodes.forEach(node => {
    if (!excludedNodeIds.has(node.id)) {
      let width = NODE_DIMENSIONS[node.type as keyof typeof NODE_DIMENSIONS]?.width || NODE_DIMENSIONS.step.width
      let height = NODE_DIMENSIONS[node.type as keyof typeof NODE_DIMENSIONS]?.height || NODE_DIMENSIONS.step.height

      // For todo tasks, use compound dimensions to reserve space for sub-agents
      if (node.type === 'todo_task') {
        const data = node.data as TodoTaskNodeData
        const routes = (data as TodoTaskNodeData).predefined_routes
        const numSubAgents = routes?.length || 0
        
        if (numSubAgents > 0) {
          const GAP = 60
          const SUB_AGENT_GAP = 20
          
          if (direction === 'LR') {
            // LR: Row Below
            // Compound Height = Orch + Gap + SubAgents
            // Compound Width = Max(Orch, SubAgents) - Actually, for LR we want the node to be centered 
            // relative to its sub-agents. So the compound node's width effectively covers the whole sub-agent row.
            const subAgentRowWidth = (numSubAgents * 280) + ((numSubAgents - 1) * SUB_AGENT_GAP)
            const subAgentRowHeight = 120 // standard step height
            
            height = height + GAP + subAgentRowHeight
            // Use full sub-agent row width for the compound node in Dagre
            // This forces Dagre to leave enough space on left/right for the sub-agents
            width = Math.max(width, subAgentRowWidth)
            console.log(`[Compound Layout] Node ${node.id} (${node.type}, LR): Compound Size ${width}x${height} (SubAgents: ${numSubAgents}, RowWidth: ${subAgentRowWidth})`)
          } else {
            // TB: Column Right
            // Compound Width = Orch + Gap + SubAgents
            // Compound Height = Max(Orch, SubAgents)
            const subAgentColWidth = 280 // standard step width
            const subAgentColHeight = (numSubAgents * 120) + ((numSubAgents - 1) * SUB_AGENT_GAP)
            
            width = width + GAP + subAgentColWidth
            height = Math.max(height, subAgentColHeight)
            console.log(`[Compound Layout] Node ${node.id} (${node.type}, TB): Compound Size ${width}x${height} (SubAgents: ${numSubAgents}, ColHeight: ${subAgentColHeight})`)
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

    const dims = NODE_DIMENSIONS[node.type as keyof typeof NODE_DIMENSIONS] || NODE_DIMENSIONS.step
    
    // Calculate position based on Compound vs Standard dimensions
    let x = nodeWithPosition.x
    let y = nodeWithPosition.y
    
    // Default centering (Dagre returns center)
    x -= dims.width / 2
    y -= dims.height / 2

    // Adjust for TodoTask Compound positioning
    if (node.type === 'todo_task') {
      const data = node.data as TodoTaskNodeData
      const routes = (data as TodoTaskNodeData).predefined_routes
      const numSubAgents = routes?.length || 0
      
      if (numSubAgents > 0) {
        const GAP = 60
        
        if (direction === 'LR') {
          // LR: Orchestrator is at Top Center of compound block
          const subAgentRowHeight = 120
          const compoundHeight = dims.height + GAP + subAgentRowHeight
          const SUB_AGENT_GAP = 20
          // Calculate compound width for potential future layout adjustments
          void ((numSubAgents * 280) + ((numSubAgents - 1) * SUB_AGENT_GAP))
          
          // Re-calculate Y: Top of compound
          const compoundTop = nodeWithPosition.y - (compoundHeight / 2)
          y = compoundTop // Orch/Todo is at top

          // Re-calculate X: Center of compound
          // If sub-agents are wider than parent, Dagre center is center of sub-agents
          // We want parent to be centered relative to sub-agents
          // nodeWithPosition.x is center of compound block
          x = nodeWithPosition.x - (dims.width / 2)
          
          console.log(`[Compound Layout] Adjusting ${node.id} (${node.type}, LR): DagreY=${nodeWithPosition.y}, CompoundH=${compoundHeight}, NewY=${y}, NewX=${x}`)
        } else {
          // TB: Orchestrator is at Left Center of compound block
          const subAgentColWidth = 280
          const compoundWidth = dims.width + GAP + subAgentColWidth
          
          // Re-calculate X: Left of compound
          const compoundLeft = nodeWithPosition.x - (compoundWidth / 2)
          x = compoundLeft // Orch/Todo is at left
          
          console.log(`[Compound Layout] Adjusting ${node.id} (${node.type}, TB): DagreX=${nodeWithPosition.x}, CompoundW=${compoundWidth}, NewX=${x}`)
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
  const withBranches = positionBranchNodes(layoutedNodes, direction)

  return { nodes: withBranches, edges }
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
  presetLearningLLM: AgentLLMConfig | undefined,
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
              type: 'smoothstep',
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
          type: 'smoothstep',
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

    // Create edge from previous step's exit node (sequential flow)
    // If lastExitNodeId is an array, it means we're connecting from multiple branch exits
    if (lastExitNodeId) {
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
          presetLearningLLM,
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
          presetLearningLLM,
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
        step.routes.forEach((route) => {
          const targetNodeId = stepIdToNodeIdMap?.get(route.next_step_id)

          if (targetNodeId) {
            routingEdges.push({
              id: `${sourceNodeId}-routing-${route.route_id}-to-${targetNodeId}`,
              source: sourceNodeId,
              sourceHandle: `route-${route.route_id}`,
              target: targetNodeId,
              type: 'smoothstep',
              label: route.route_name || route.route_id,
              labelStyle: ROUTE_EDGE_LABEL_STYLE,
              labelBgStyle: EDGE_LABEL_BG_STYLE,
              labelBgPadding: [3, 3] as [number, number],
              labelBgBorderRadius: 4,
              style: { stroke: '#14b8a6', strokeWidth: 1.5 },
              animated: false
            })
          } else if (route.next_step_id === 'end') {
            routingEdges.push({
              id: `${sourceNodeId}-routing-${route.route_id}-to-end`,
              source: sourceNodeId,
              sourceHandle: `route-${route.route_id}`,
              target: 'end',
              type: 'smoothstep',
              label: route.route_name || route.route_id,
              labelStyle: ROUTE_EDGE_LABEL_STYLE,
              labelBgStyle: EDGE_LABEL_BG_STYLE,
              labelBgPadding: [3, 3] as [number, number],
              labelBgBorderRadius: 4,
              style: { stroke: '#14b8a6', strokeWidth: 1.5 },
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
  })

  return { nodes, edges }
}

/**
 * Check if a node is a step-type node (has step data)
 */
function isStepTypeNode(node: WorkflowNode): node is WorkflowNode & { data: StepNodeData | ConditionalNodeData | TodoTaskNodeData | HumanInputNodeData } {
  return node.type === 'step' || node.type === 'conditional' || node.type === 'todo_task' || node.type === 'human_input'
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
  const presetLearningLLM = activePreset?.llmConfig?.learning_llm

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
      presetLearningLLM,
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
        presetLearningLLM,
        availableLLMs,
        [],  // no completed step indices for orphan steps
        stepStatusMapAsMap,
        options.workspacePath,
        options.selectedRunFolder,
        stepIdToNodeIdMap,
        new Set<string>()  // no completed step IDs for orphan steps
      )

      // Mark all orphan nodes and remap IDs with 'orphan-' prefix
      orphanNodes = orphanProcessedNodes.map((node) => ({
        ...node,
        id: `orphan-${node.id}`,
        data: {
          ...node.data,
          isOrphan: true,
        }
      }))
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
    const HEADER_GAP = 100 // Increased further to prevent overlap
    const HEADER_START_X = 80 // Increased further
    const HEADER_Y = 80 // Increased further
    
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
    const HEADER_TO_WORKFLOW_GAP = 150 // Increased further to prevent overlap with step1

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
      const startPos = { x: HEADER_START_X, y: HEADER_Y }
      const varsPos = { x: HEADER_START_X + startDims.width + HEADER_GAP, y: HEADER_Y }

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
            const SUB_AGENT_GAP = 20
            const subAgentRowWidth = (numSubAgents * 280) + ((numSubAgents - 1) * SUB_AGENT_GAP)
            const parentWidth = 300
            if (subAgentRowWidth > parentWidth) {
              // Sub-agents extend further left than the parent card
              const overflow = (subAgentRowWidth - parentWidth) / 2
              firstStepLeftEdge -= overflow
            }
          }
        }

        if (layoutDirection === 'TB') {
          // TB mode: workflow flows vertically, so first step should be below the header row
          // Start from the right edge of the header row
          const firstStepTargetX = headerRowEndX + HEADER_TO_WORKFLOW_GAP
          const firstStepTargetY = headerRowBottom + HEADER_TO_WORKFLOW_GAP

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

    // Position sub-agents relative to their parent nodes (todo_task)
    // LR layout: horizontal row BELOW parent
    // TB layout: vertical column to the RIGHT of parent
    const parentNodeMap = new Map<string, { nodeIndex: number; subAgentIndices: number[] }>()

    // Pass 1: Find all todo task nodes first to initialize map
    layoutedResult.nodes.forEach((node, index) => {
      if (node.type === 'todo_task') {
        parentNodeMap.set(node.id, { nodeIndex: index, subAgentIndices: [] })
      }
    })
    console.log(`[SubAgent Mapping] Pass 1: Found ${parentNodeMap.size} parents`)

    // Pass 2: Find all sub-agents and attach to parent
    let subAgentsFound = 0
    layoutedResult.nodes.forEach((node, index) => {
      if (node.id.includes('-sub-agent-')) {
        // Extract parent node ID from sub-agent ID
        const parentId = node.id.split('-sub-agent-')[0]
        const parentInfo = parentNodeMap.get(parentId)
        if (parentInfo) {
          parentInfo.subAgentIndices.push(index)
          subAgentsFound++
        } else {
          console.warn(`[SubAgent Mapping] Orphan sub-agent found: ${node.id} (Parent ${parentId} not found)`)
        }
      }
    })
    console.log(`[SubAgent Mapping] Pass 2: Mapped ${subAgentsFound} sub-agents to parents`)

    // Position sub-agents based on layout direction
    parentNodeMap.forEach(({ nodeIndex: parentNodeIndex, subAgentIndices }) => {
      const parentNode = layoutedResult.nodes[parentNodeIndex]
      const parentDimensions = (NODE_DIMENSIONS[parentNode.type as keyof typeof NODE_DIMENSIONS]) || NODE_DIMENSIONS.step

      if (subAgentIndices.length === 0) return

      const GAP = 60
      const SUB_AGENT_GAP = 20
      

      if (layoutDirection === 'LR') {
        // LR layout: Horizontal row BELOW parent
        // Calculate total width needed for all sub-agents
        let totalWidth = 0
        subAgentIndices.forEach((idx, i) => {
          const dims = NODE_DIMENSIONS[layoutedResult.nodes[idx].type as keyof typeof NODE_DIMENSIONS] || NODE_DIMENSIONS.step
          totalWidth += dims.width
          if (i < subAgentIndices.length - 1) {
            totalWidth += SUB_AGENT_GAP
          }
        })

        // Center sub-agents under parent
        const startX = parentNode.position.x + (parentDimensions.width - totalWidth) / 2
        const subAgentY = parentNode.position.y + parentDimensions.height + GAP

        let currentX = startX
        subAgentIndices.forEach((subAgentIndex) => {
          const subAgent = layoutedResult.nodes[subAgentIndex]
          const subAgentDimensions = NODE_DIMENSIONS[subAgent.type as keyof typeof NODE_DIMENSIONS] || NODE_DIMENSIONS.step

          // Update position - same Y, different X (horizontal row)
          layoutedResult.nodes[subAgentIndex] = {
            ...subAgent,
            position: {
              x: currentX,
              y: subAgentY
            }
          }

          currentX += subAgentDimensions.width + SUB_AGENT_GAP
        })
      } else {
        // TB layout: Vertical column to the RIGHT of parent
        // Calculate total height needed for all sub-agents
        let totalHeight = 0
        subAgentIndices.forEach((idx, i) => {
          const dims = NODE_DIMENSIONS[layoutedResult.nodes[idx].type as keyof typeof NODE_DIMENSIONS] || NODE_DIMENSIONS.step
          totalHeight += dims.height
          if (i < subAgentIndices.length - 1) {
            totalHeight += SUB_AGENT_GAP
          }
        })

        // Center sub-agents vertically relative to parent
        const subAgentX = parentNode.position.x + parentDimensions.width + GAP
        const startY = parentNode.position.y + (parentDimensions.height - totalHeight) / 2
        

        let currentY = startY
        subAgentIndices.forEach((subAgentIndex) => {
          const subAgent = layoutedResult.nodes[subAgentIndex]
          const subAgentDimensions = NODE_DIMENSIONS[subAgent.type as keyof typeof NODE_DIMENSIONS] || NODE_DIMENSIONS.step

          // Update position - same X, different Y (vertical column)
          layoutedResult.nodes[subAgentIndex] = {
            ...subAgent,
            position: {
              x: subAgentX,
              y: currentY
            }
          }

          currentY += subAgentDimensions.height + SUB_AGENT_GAP
        })
      }
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

    // Apply global collision detection and resolution to fix any remaining overlaps
    // This handles overlaps from todo_task sub-agents, conditional branches, loops, etc.
    // For LR layout: prefer vertical shifts. For TB layout: prefer horizontal shifts.
    const nodesBeforeCollision = layoutedResult.nodes.length
    layoutedResult.nodes = detectAndResolveCollisions(layoutedResult.nodes, layoutDirection)
    const nodesAfterCollision = layoutedResult.nodes.length
    if (nodesBeforeCollision !== nodesAfterCollision) {
      // Node count changed during collision detection (log removed to reduce console noise)
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
  }, [disabled, plan, showDependencyEdges, changes, presetUseCodeExecutionMode, presetLLMConfig, presetLearningLLM, availableLLMs, completedStepIndices, options.workspacePath, options.selectedRunFolder, variablesManifest, onOpenVariablesSidebar, isLoadingVariables, layoutDirection])
}

export default usePlanToFlow
