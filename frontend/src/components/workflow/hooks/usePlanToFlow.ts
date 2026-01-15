import { useMemo } from 'react'
import type { Node, Edge } from '@xyflow/react'
import dagre from 'dagre'
import type { PlanStep, PlanningResponse, AgentConfigs, AgentLLMConfig, PrerequisiteRule, ValidationSchema } from '../../../utils/stepConfigMatching'
import { isRegularStep, isConditionalStep, isDecisionStep, isOrchestrationStep, isHumanInputStep } from '../../../utils/stepConfigMatching'
import type { ChangeType, PlanChanges } from './usePlanData'
import type { VariablesManifest } from '../../../services/api-types'
import type { VariablesNodeData } from '../nodes/VariablesNode'
import type { ExecutionSettingsNodeData } from '../nodes/ExecutionSettingsNode'
import { useGlobalPresetStore } from '../../../stores/useGlobalPresetStore'
import { useLLMStore } from '../../../stores/useLLMStore'

// Callback type for running from a specific step
export type OnRunFromStepCallback = (stepIndex: number, stepId: string) => void

// Callback type for opening the sidebar for a specific node
export type OnOpenSidebarCallback = (nodeId: string) => void

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
  onRunFromStep?: OnRunFromStepCallback  // Callback to run from this step
  isExecuting?: boolean  // Whether execution is in progress
  canRun?: boolean  // Deprecated: always true (all steps can run regardless of previous completion)
  workspacePath?: string | null  // Workspace path for file opening
  selectedRunFolder?: string  // Selected iteration folder for file opening
  validation_schema?: ValidationSchema  // Validation schema from plan.json
  // Sub-agent specific fields
  parentOrchestratorTitle?: string  // Title of parent orchestrator node (for sub-agents)
  routeName?: string  // Route name from orchestration_routes (for sub-agents)
  routeCondition?: string  // Condition from orchestration_routes (for sub-agents)
}

export interface ConditionalNodeData extends Record<string, unknown> {
  id: string
  title: string
  description?: string
  condition_question?: string
  condition_context?: string
  status: 'pending' | 'evaluating' | 'decided_true' | 'decided_false'
  stepIndex: number
  step: PlanStep
  changeType?: ChangeType  // Highlight type for visual feedback
  onRunFromStep?: OnRunFromStepCallback  // Callback to run from this step
  onOpenSidebar?: OnOpenSidebarCallback  // Callback to open sidebar for editing
  isExecuting?: boolean  // Whether execution is in progress
  canRun?: boolean  // Deprecated: always true (all steps can run regardless of previous completion)
  workspacePath?: string | null  // Workspace path for file opening
  selectedRunFolder?: string  // Selected iteration folder for file opening
  validation_schema?: ValidationSchema  // Validation schema from plan.json
}

export interface DecisionNodeData extends Record<string, unknown> {
  id: string
  title: string
  decision_evaluation_question?: string
  decision_step?: PlanStep
  status: 'pending' | 'executing' | 'evaluating' | 'decided_true' | 'decided_false'
  stepIndex: number
  step: PlanStep
  changeType?: ChangeType  // Highlight type for visual feedback
  onRunFromStep?: OnRunFromStepCallback  // Callback to run from this step
  onOpenSidebar?: OnOpenSidebarCallback  // Callback to open sidebar for editing
  isExecuting?: boolean  // Whether execution is in progress
  canRun?: boolean  // Deprecated: always true (all steps can run regardless of previous completion)
  workspacePath?: string | null  // Workspace path for file opening
  selectedRunFolder?: string  // Selected iteration folder for file opening
  validation_schema?: ValidationSchema  // Validation schema from plan.json (from decision_step)
}

export interface LoopNodeData extends Record<string, unknown> {
  id: string
  title: string
  loop_condition?: string
  max_iterations?: number
  current_iteration?: number
  status: 'pending' | 'running' | 'completed' | 'failed'
  stepIndex: number
  step: PlanStep
  changeType?: ChangeType  // Highlight type for visual feedback
  onRunFromStep?: OnRunFromStepCallback  // Callback to run from this step
  isExecuting?: boolean  // Whether execution is in progress
  canRun?: boolean  // Deprecated: always true (all steps can run regardless of previous completion)
  workspacePath?: string | null  // Workspace path for file opening
  selectedRunFolder?: string  // Selected iteration folder for file opening
  validation_schema?: ValidationSchema  // Validation schema from plan.json
}

export interface OrchestratorNodeData extends Record<string, unknown> {
  id: string
  title: string
  orchestration_step?: PlanStep
  orchestration_routes?: Array<{ route_id: string; route_name: string; condition: string; sub_agent_step: PlanStep; context_to_pass?: string }>
  status: 'pending' | 'executing' | 'evaluating' | 'orchestrating' | 'completed'
  stepIndex: number
  step: PlanStep
  changeType?: ChangeType  // Highlight type for visual feedback
  onRunFromStep?: OnRunFromStepCallback  // Callback to run from this step
  onOpenSidebar?: OnOpenSidebarCallback  // Callback to open sidebar for editing
  isExecuting?: boolean  // Whether execution is in progress
  canRun?: boolean  // Deprecated: always true (all steps can run regardless of previous completion)
  workspacePath?: string | null  // Workspace path for file opening
  selectedRunFolder?: string  // Selected iteration folder for file opening
  validation_schema?: ValidationSchema  // Validation schema from plan.json (from orchestration_step)
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
  onRunFromStep?: OnRunFromStepCallback  // Callback to run from this step
  onOpenSidebar?: OnOpenSidebarCallback  // Callback to open sidebar for editing
  isExecuting?: boolean  // Whether execution is in progress
  canRun?: boolean  // Deprecated: always true (all steps can run regardless of previous completion)
  workspacePath?: string | null  // Workspace path for file opening
  selectedRunFolder?: string  // Selected iteration folder for file opening
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

export type WorkflowNodeData = StepNodeData | ConditionalNodeData | DecisionNodeData | LoopNodeData | OrchestratorNodeData | HumanInputNodeData | ValidationNodeData | LearningNodeData | EvaluationNodeData | VariablesNodeData | ExecutionSettingsNodeData

// Node and edge types
export type WorkflowNode = Node<WorkflowNodeData>
export type WorkflowEdge = Edge

interface UsePlanToFlowResult {
  nodes: WorkflowNode[]
  edges: WorkflowEdge[]
}

interface UsePlanToFlowOptions {
  showDependencyEdges?: boolean // Default: false (hide dependency edges for cleaner view)
  showPrerequisiteEdges?: boolean // Default: false (hide prerequisite edges for cleaner view)
  changes?: PlanChanges | null  // Optional: highlight changes on nodes
  onRunFromStep?: OnRunFromStepCallback  // Callback for "run from step" button
  onOpenSidebar?: OnOpenSidebarCallback  // Callback for opening sidebar when settings icon is clicked
  isExecuting?: boolean  // Whether execution is currently in progress
  completedStepIndices?: number[]  // 0-based indices of completed steps (from steps_done.json)
  stepStatusMap?: Map<string, 'pending' | 'running' | 'completed' | 'failed'> | Record<string, 'pending' | 'running' | 'completed' | 'failed'> | null  // Step status from events (Map or serialized object for stable comparison)
  workspacePath?: string | null  // Workspace path for file opening
  selectedRunFolder?: string  // Selected iteration folder for file opening
  variablesManifest?: VariablesManifest | null  // Variables manifest for Variables node
  onOpenVariablesSidebar?: () => void  // Callback for opening variables sidebar
  isLoadingVariables?: boolean  // Whether variables are being loaded
}

// Dagre layout configuration
const DAGRE_CONFIG = {
  rankdir: 'LR', // Left to right (tree layout)
  nodesep: 500,  // Vertical spacing between nodes in same rank (increased for general spacing)
  ranksep: 200,  // Horizontal spacing between ranks (columns)
  marginx: 80,
  marginy: 80
}

// Node dimensions for layout calculation (base dimensions) - smaller since content is simplified
const NODE_DIMENSIONS = {
  step: { width: 280, height: 120 },
  conditional: { width: 240, height: 100 },
  decision: { width: 260, height: 120 },
  orchestrator: { width: 300, height: 120 },
  human_input: { width: 260, height: 120 },
  loop: { width: 300, height: 140 },
  start: { width: 80, height: 36 },
  end: { width: 80, height: 36 },
  variables: { width: 220, height: 120 },
  'execution-settings': { width: 200, height: 100 }
}

/**
 * Estimate node height based on content
 * Simplified version - nodes no longer show description, success criteria, or validation schema
 * Only accounts for: context files, prerequisite rules, loop conditions
 */
function estimateNodeHeight(node: WorkflowNode): number {
  const baseDimensions = NODE_DIMENSIONS[node.type as keyof typeof NODE_DIMENSIONS] || NODE_DIMENSIONS.step
  let estimatedHeight = baseDimensions.height

  // Get node data
  const data = node.data as StepNodeData | ConditionalNodeData | DecisionNodeData | LoopNodeData | OrchestratorNodeData | Record<string, unknown>

  // Base height components (header, padding, footer) - simplified
  const headerHeight = 60 // Header with buttons
  const footerHeight = 40 // Config footer
  const padding = 16 // Top and bottom padding

  // Content height estimation - only for context files and prerequisite rules
  let contentHeight = 0

  // Prerequisite rules and context files (from step.agent_configs)
  if ('step' in data && data.step && typeof data.step === 'object') {
    const step = data.step as PlanStep
    if (step.agent_configs?.enable_prerequisite_detection && step.agent_configs?.prerequisite_rules) {
      const ruleCount = step.agent_configs.prerequisite_rules.length
      contentHeight += (ruleCount * 40) + 8 // ~40px per rule + spacing
    }

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

  // For loop nodes, add height for loop condition
  if (node.type === 'loop') {
    contentHeight += 30 // Loop iteration badge
    if ('loop_condition' in data && typeof data.loop_condition === 'string' && data.loop_condition) {
      contentHeight += 40 // Loop condition box
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
    if (node.type === 'orchestrator') {
      hasOrchestrator = true
      const data = node.data as OrchestratorNodeData
      const routes = data.orchestration_routes?.length || 0
      
      // Count actual sub-agents (excluding "end" route if strictly just counting agent steps, 
      // but usually routes map to sub-agents. We'll count all routes for spacing safety).
      maxOrchestratorSubAgents = Math.max(maxOrchestratorSubAgents, routes)
      
      maxOrchestratorDepth = Math.max(maxOrchestratorDepth, routes)
    }
  })

  return { hasOrchestrator, maxOrchestratorDepth, maxOrchestratorSubAgents }
}

/**
 * Reposition branch nodes for conditional and decision nodes
 * After Dagre layout, adjust Y positions so:
 * - TRUE branch: positioned ABOVE the parent conditional
 * - FALSE branch: positioned BELOW the parent conditional
 * X positions from Dagre are kept (horizontal flow preserved)
 */
function positionBranchNodes(nodes: WorkflowNode[]): WorkflowNode[] {
  const adjustedNodes = [...nodes]
  const nodeMap = new Map(adjustedNodes.map((n, i) => [n.id, { node: n, index: i }]))

  // Find conditional and decision nodes (both have TRUE/FALSE branches)
  const branchingNodes = adjustedNodes.filter(n =>
    n.type === 'conditional' || n.type === 'decision'
  )

  branchingNodes.forEach(parent => {
    const parentDims = NODE_DIMENSIONS[parent.type as keyof typeof NODE_DIMENSIONS] || NODE_DIMENSIONS.step
    const parentCenterY = parent.position.y + parentDims.height / 2

    // Find TRUE branch nodes (direct children only, not nested)
    const trueBranch = adjustedNodes.filter(n => {
      if (!n.id.startsWith(`${parent.id}-true-`)) return false
      // Check it's a direct child (no additional -true- or -false- after the first)
      const suffix = n.id.substring(`${parent.id}-true-`.length)
      return !suffix.includes('-true-') && !suffix.includes('-false-')
    })

    // Find FALSE branch nodes (direct children only)
    const falseBranch = adjustedNodes.filter(n => {
      if (!n.id.startsWith(`${parent.id}-false-`)) return false
      const suffix = n.id.substring(`${parent.id}-false-`.length)
      return !suffix.includes('-true-') && !suffix.includes('-false-')
    })

    // Calculate dynamic offset based on branch content
    // Check if either branch contains orchestrator nodes (which need more vertical space)
    const trueHasOrchestrator = trueBranch.some(n => n.type === 'orchestrator')
    const falseHasOrchestrator = falseBranch.some(n => n.type === 'orchestrator')
    
    // Base offset
    let verticalOffset = 450
    
    // Increase offset if orchestrators are present in branches
    // Orchestrators have sub-agents below them, requiring more vertical clearance
    if (trueHasOrchestrator || falseHasOrchestrator) {
      verticalOffset = 600 // Increased from 450 to 600 for orchestrators
    }

    console.log(`[Layout Debug] Repositioning branches for ${parent.id}: ${trueBranch.length} true, ${falseBranch.length} false. Offset: ${verticalOffset}`)

    // Reposition TRUE branch ABOVE the parent (adjust Y only, keep X from Dagre)
    if (trueBranch.length > 0) {
      const targetY = parentCenterY - verticalOffset

      trueBranch.forEach(node => {
        const info = nodeMap.get(node.id)
        if (info) {
          const dims = NODE_DIMENSIONS[node.type as keyof typeof NODE_DIMENSIONS] || NODE_DIMENSIONS.step
          adjustedNodes[info.index] = {
            ...adjustedNodes[info.index],
            position: {
              x: adjustedNodes[info.index].position.x, // Keep X from Dagre
              y: targetY - dims.height / 2
            }
          }
        }
      })
    }

    // Reposition FALSE branch BELOW the parent (adjust Y only, keep X from Dagre)
    if (falseBranch.length > 0) {
      const targetY = parentCenterY + verticalOffset

      falseBranch.forEach(node => {
        const info = nodeMap.get(node.id)
        if (info) {
          const dims = NODE_DIMENSIONS[node.type as keyof typeof NODE_DIMENSIONS] || NODE_DIMENSIONS.step
          adjustedNodes[info.index] = {
            ...adjustedNodes[info.index],
            position: {
              x: adjustedNodes[info.index].position.x, // Keep X from Dagre
              y: targetY - dims.height / 2
            }
          }
        }
      })
    }
  })

  return adjustedNodes
}

/**
 * Global collision detection and resolution
 * Detects overlaps between all nodes and resolves them by shifting nodes
 * This handles overlaps from orchestrator sub-agents, conditional branches, loops, etc.
 */
function detectAndResolveCollisions(nodes: WorkflowNode[]): WorkflowNode[] {
  const MIN_SEPARATION = 40 // Minimum gap between nodes
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

    // If fully overlapping (both dimensions overlap), prefer vertical shift for LR layout
    if (hOverlapAmount > 0 && vOverlapAmount > 0) {
      // Full overlap - shift vertically (prefer moving down for nodes that come later)
      const shiftY = a.top < b.top
        ? -(vOverlapAmount + MIN_SEPARATION)  // Move a up
        : (vOverlapAmount + MIN_SEPARATION)   // Move a down
      return { dx: 0, dy: shiftY }
    }

    // Partial overlap or too close - determine best direction
    if (vOverlapAmount > 0) {
      // Vertical overlap - shift vertically
      const shiftY = a.top < b.top
        ? -(vOverlapAmount + MIN_SEPARATION)  // Move a up
        : (vOverlapAmount + MIN_SEPARATION)   // Move a down
      return { dx: 0, dy: shiftY }
    } else if (hOverlapAmount > 0) {
      // Horizontal overlap - shift horizontally
      const shiftX = a.left < b.left
        ? -(hOverlapAmount + MIN_SEPARATION)  // Move a left
        : (hOverlapAmount + MIN_SEPARATION)   // Move a right
      return { dx: shiftX, dy: 0 }
    } else if (hDistance < MIN_SEPARATION && vDistance < MIN_SEPARATION) {
      // Too close but not overlapping - shift in direction that needs more space
      if (vDistance < hDistance) {
        // Need more vertical separation
        const shiftY = a.top < b.top
          ? -(MIN_SEPARATION - vDistance)  // Move a up
          : (MIN_SEPARATION - vDistance)   // Move a down
        return { dx: 0, dy: shiftY }
      } else {
        // Need more horizontal separation
        const shiftX = a.left < b.left
          ? -(MIN_SEPARATION - hDistance)  // Move a left
          : (MIN_SEPARATION - hDistance)   // Move a right
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
          // Log collision details for debugging (removed to reduce console noise)

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

  // Log collision detection results (removed to reduce console noise)

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
function layoutWithDagre(nodes: WorkflowNode[], edges: WorkflowEdge[]): { nodes: WorkflowNode[], edges: WorkflowEdge[] } {
  console.log('[Layout Debug] === Starting Dagre Layout ===')
  
  // Calculate topology metrics to determine spacing requirements
  const { hasOrchestrator } = calculateTopologyMetrics(nodes)
  
  // Dynamic config based on topology
  const dynamicConfig = {
    ...DAGRE_CONFIG,
    // Vertical spacing is now handled by the increased default nodesep (500)
    // Horizontal spacing defaults to 150. Specific offsets are handled via minlen on edges.
  }
  
  console.log('[Layout Debug] Input nodes:', nodes.length, 'edges:', edges.length)
  console.log('[Layout Debug] Topology:', { hasOrchestrator })
  console.log('[Layout Debug] Dynamic Config:', dynamicConfig)
  console.log('[Layout Debug] NODE_DIMENSIONS:', NODE_DIMENSIONS)

  const g = new dagre.graphlib.Graph()
  g.setGraph(dynamicConfig)
  g.setDefaultEdgeLabel(() => ({}))

  // Only exclude SUB-AGENT nodes from Dagre (they're positioned manually below orchestrators)
  // Branch nodes MUST be in Dagre to maintain graph connectivity
  const excludedNodeIds = new Set<string>()

  nodes.forEach(node => {
    if (node.id.includes('-sub-agent-')) {
      excludedNodeIds.add(node.id)
    }
  })

  console.log(`[Layout Debug] Excluding ${excludedNodeIds.size} sub-agent nodes from Dagre`)

  // Add all nodes except sub-agents to Dagre graph
  nodes.forEach(node => {
    if (!excludedNodeIds.has(node.id)) {
      const dimensions = NODE_DIMENSIONS[node.type as keyof typeof NODE_DIMENSIONS] || NODE_DIMENSIONS.step
      g.setNode(node.id, { width: dimensions.width, height: dimensions.height })
    }
  })

  // Add all edges except those involving sub-agents
  edges.forEach(edge => {
    if (!excludedNodeIds.has(edge.source) && !excludedNodeIds.has(edge.target)) {
      // Find if source is an orchestrator to apply dynamic horizontal spacing
      const sourceNode = nodes.find(n => n.id === edge.source)
      
      if (sourceNode?.type === 'orchestrator') {
        const data = sourceNode.data as OrchestratorNodeData
        const numSubAgents = data.orchestration_routes?.length || 0
        
        if (numSubAgents > 0) {
          const SUB_AGENT_WIDTH = 280
          const GAP = 40
          // Total width of sub-agent row
          const totalRowWidth = (numSubAgents * SUB_AGENT_WIDTH) + ((numSubAgents - 1) * GAP)
          // Half width (extension to the right from orchestrator center)
          const halfWidth = totalRowWidth / 2
          
          // Calculate minlen: number of rank separations needed to clear the sub-agents
          // We want at least halfWidth + 100px of clearance.
          // Total horizontal distance = ranksep * minlen
          const requiredClearance = halfWidth + 100
          const minlen = Math.ceil(requiredClearance / dynamicConfig.ranksep)
          
          console.log(`[Layout Debug] Edge ${edge.source} -> ${edge.target}: Orchestrator has ${numSubAgents} sub-agents. Setting minlen=${minlen} for clearance.`)
          g.setEdge(edge.source, edge.target, { minlen })
        } else {
          g.setEdge(edge.source, edge.target)
        }
      } else {
        g.setEdge(edge.source, edge.target)
      }
    }
  })

  // Run layout
  dagre.layout(g)
  console.log('[Layout Debug] Dagre layout complete')

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

    const dimensions = NODE_DIMENSIONS[node.type as keyof typeof NODE_DIMENSIONS] || NODE_DIMENSIONS.step

    return {
      ...node,
      position: {
        x: nodeWithPosition.x - dimensions.width / 2,
        y: nodeWithPosition.y - dimensions.height / 2
      }
    }
  })

  // Position branch nodes: TRUE above, FALSE below their parent conditional/decision
  const withBranches = positionBranchNodes(layoutedNodes)

  // Log final positions
  console.log('[Layout Debug] === Final Node Positions ===')
  withBranches.forEach(n => {
    console.log(`[Layout Debug] ${n.id} (${n.type}): x=${Math.round(n.position.x)}, y=${Math.round(n.position.y)}`)
  })
  console.log('[Layout Debug] === End Layout ===')

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
    if (isDecisionStep(step)) {
      // For decision nodes, prefer decision_evaluation_question over generic title
      return step.title || step.decision_evaluation_question || `Decision ${stepIndex + 1}`
    }
    if (isOrchestrationStep(step)) {
      // For orchestration nodes, use step title or fallback
      return step.title || `Orchestrator ${stepIndex + 1}`
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

  if (isDecisionStep(step)) {
    return {
      id: nodeId,
      type: 'decision',
      position: { x: 0, y: 0 },
      data: {
        ...baseData,
        decision_evaluation_question: step.decision_evaluation_question,
        decision_step: step.decision_step,
        // Use validation_schema from decision_step (inner step) if available, otherwise from wrapper
        validation_schema: step.decision_step?.validation_schema || step.validation_schema
        // Note: status is inherited from baseData (computed based on completedStepIndices)
      } as DecisionNodeData
    }
  }

  if (isOrchestrationStep(step)) {
    return {
      id: nodeId,
      type: 'orchestrator',
      position: { x: 0, y: 0 },
      data: {
        ...baseData,
        orchestration_step: step.orchestration_step,
        orchestration_routes: step.orchestration_routes,
        // Use validation_schema from orchestration_step (inner step) if available, otherwise from wrapper
        validation_schema: step.orchestration_step?.validation_schema || step.validation_schema
        // Note: status is inherited from baseData (computed based on completedStepIndices)
      } as OrchestratorNodeData
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

  if (isRegularStep(step) && step.has_loop) {
    return {
      id: nodeId,
      type: 'loop',
      position: { x: 0, y: 0 },
      data: {
        ...baseData,
        loop_condition: step.loop_condition,
        max_iterations: step.max_iterations
        // Note: status is inherited from baseData (computed based on completedStepIndices)
      } as LoopNodeData
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
 * Get provider and model separately for an LLM config
 */
function getLLMProviderAndModel(
  llmConfig: AgentLLMConfig | undefined,
  presetLLMConfig: AgentLLMConfig | undefined,
  availableLLMs: Array<{ provider: string; model: string; label: string }>
): { provider?: string; model?: string } {
  const effectiveLLM = llmConfig || presetLLMConfig
  if (!effectiveLLM) return {}

  const llm = availableLLMs.find(l =>
    l.provider === effectiveLLM.provider &&
    l.model === effectiveLLM.model_id
  )

  if (llm) {
    return {
      provider: llm.provider,
      model: llm.model
    }
  }

  return {
    provider: effectiveLLM.provider,
    model: effectiveLLM.model_id
  }
}

/**
 * Create validation and learning nodes for a step
 * DISABLED: Validation and learning nodes are no longer displayed in the workflow canvas
 * Returns empty nodes and edges, with the step itself as the exit node
 */
function createValidationLearningNodes(
  _step: PlanStep,
  stepNodeId: string,
  _presetUseCodeExecutionMode: boolean,
  _presetLLMConfig: AgentLLMConfig | undefined,
  _presetValidationLLM: AgentLLMConfig | undefined,
  _presetLearningLLM: AgentLLMConfig | undefined,
  _availableLLMs: Array<{ provider: string; model: string; label: string }>,
  _nodeType?: 'step' | 'conditional' | 'decision' | 'loop' | 'orchestrator'
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
  presetValidationLLM: AgentLLMConfig | undefined,
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
        step,
        node.id,
        presetUseCodeExecutionMode,
        presetLLMConfig,
        presetValidationLLM,
        presetLearningLLM,
        availableLLMs,
        node.type as 'step' | 'conditional' | 'decision' | 'loop' | 'orchestrator'
      )
      nodes.push(...vlResult.nodes)
      edges.push(...vlResult.edges)
      lastExitNodeId = vlResult.exitNodeId

      // For decision steps, add an evaluation node after learning for LLM evaluation
      if (isDecisionStep(step) && lastExitNodeId) {
        const agentConfigs = step.agent_configs as AgentConfigs | undefined
        const conditionalLLM = agentConfigs?.conditional_llm || presetLLMConfig
        const evaluationLLMInfo = getLLMProviderAndModel(conditionalLLM, presetLLMConfig, availableLLMs)

        const evaluationNodeId = `${node.id}-evaluation`
        const evaluationNode: WorkflowNode = {
          id: evaluationNodeId,
          type: 'evaluation',
          position: { x: 0, y: 0 },
          data: {
            id: evaluationNodeId,
            parentStepId: node.id,
            parentStepTitle: step.title,
            evaluationQuestion: step.decision_evaluation_question,
            status: 'pending',
            llmProvider: evaluationLLMInfo.provider,
            llmModel: evaluationLLMInfo.model
          } as EvaluationNodeData
        }
        nodes.push(evaluationNode)

        // Edge from learning (or step if no learning) to evaluation
        edges.push({
          id: `${lastExitNodeId}-to-evaluation`,
          source: lastExitNodeId,
          target: evaluationNodeId,
          type: 'smoothstep',
          animated: false,
          style: { stroke: '#8b5cf6', strokeWidth: 2 }
        })

        lastExitNodeId = evaluationNodeId
      }
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
          presetValidationLLM,
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
          presetValidationLLM,
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

    // Handle decision step edge routing
    // Decision steps execute a step first, then route based on evaluation result
    // They have no branch arrays - just direct routing via next_step_id
    // Edges should come from the evaluation node (lastExitNodeId) if it exists, otherwise from the decision node
    if (isDecisionStep(step)) {
      const decisionEdges: WorkflowEdge[] = []
      // Use evaluation node as source if it exists, otherwise use decision node
      // lastExitNodeId can be string | string[] | null, so we need to handle it properly
      const sourceNodeId = (typeof lastExitNodeId === 'string' ? lastExitNodeId : node.id)

      // Handle true branch routing (if_true_next_step_id is REQUIRED for decision steps)
      if (step.if_true_next_step_id) {
        const targetNodeId = stepIdToNodeIdMap?.get(step.if_true_next_step_id)
        if (targetNodeId) {
          decisionEdges.push({
            id: `${sourceNodeId}-decision-true-to-${targetNodeId}`,
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
        } else if (step.if_true_next_step_id === 'end') {
          decisionEdges.push({
            id: `${sourceNodeId}-decision-true-to-end`,
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

      // Handle false branch routing (if_false_next_step_id is REQUIRED for decision steps)
      if (step.if_false_next_step_id) {
        const targetNodeId = stepIdToNodeIdMap?.get(step.if_false_next_step_id)
        if (targetNodeId) {
          decisionEdges.push({
            id: `${sourceNodeId}-decision-false-to-${targetNodeId}`,
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
        } else if (step.if_false_next_step_id === 'end') {
          decisionEdges.push({
            id: `${sourceNodeId}-decision-false-to-end`,
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

      edges.push(...decisionEdges)

      // Decision steps handle their own routing - don't connect to next sequential step
      lastExitNodeId = null
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

    // Handle routing step edge routing
    // Orchestrator steps execute a main orchestrator step, then route to sub-agents based on evaluation
    // After sub-agents complete, they return to the main orchestrator for re-evaluation
    // The orchestrator step connects to next_step_id when success criteria is met
    if (isOrchestrationStep(step)) {
      const orchestratorEdges: WorkflowEdge[] = []
      const orchestratorSubAgentNodes: WorkflowNode[] = []

      // Use learning/evaluation node as source if it exists, otherwise use orchestrator node
      const sourceNodeId = (typeof lastExitNodeId === 'string' ? lastExitNodeId : node.id)

      // Create nodes for sub-agents (private to orchestration step)
      // Handle "end" route separately - it doesn't execute a sub-agent, just terminates workflow
      if (step.orchestration_routes && step.orchestration_routes.length > 0) {
        step.orchestration_routes.forEach((route) => {
          const isEndRoute = route.route_id?.toLowerCase() === "end"

          // Handle "end" route - create edge to end node but skip sub-agent node creation
          if (isEndRoute) {
            // Helper to truncate condition to 10 words
            const truncateToWords = (text: string, maxWords: number): string => {
              if (!text) return ''
              const words = text.trim().split(/\s+/)
              if (words.length <= maxWords) return text
              return words.slice(0, maxWords).join(' ') + '...'
            }

            const conditionLabel = route.condition
              ? truncateToWords(route.condition, 10)
              : route.route_name || route.route_id || "End"

            // Create edge from orchestrator to "end" node
            orchestratorEdges.push({
              id: `${node.id}-route-${route.route_id}-to-end`,
              source: node.id,
              sourceHandle: route.route_id, // Use route_id as handle (on bottom)
              target: 'end',
              type: 'smoothstep',
              label: conditionLabel,
              labelStyle: { fill: '#ef4444', fontWeight: 600, fontSize: 11 },
              labelBgStyle: { fill: '#fef2f2', fillOpacity: 0.9 },
              labelBgPadding: [4, 4] as [number, number],
              labelBgBorderRadius: 4,
              style: { stroke: '#ef4444', strokeWidth: 2 }, // Solid red line for end route
              animated: false
            })
            return // Skip sub-agent node creation for "end" route
          }

          if (route.sub_agent_step) {
            // Use route_id if available, otherwise use step ID or index as fallback
            const routeId = route.route_id || route.sub_agent_step.id || String(step.orchestration_routes?.indexOf(route) ?? 0)
            const subAgentNodeId = `${node.id}-sub-agent-${routeId}`

            // Get the stepIndex from the node data (parent routing step's index)
            const parentStepIndex = (node.data as OrchestratorNodeData).stepIndex
            const orchestratorNodeData = node.data as OrchestratorNodeData
            const orchestratorTitle = orchestratorNodeData.title || step.title || `Orchestrator ${parentStepIndex + 1}`

            // Create sub-agent node directly (don't use stepToNode to avoid wrong ID generation)
            const subAgentStep = route.sub_agent_step
            const stepId = subAgentStep.id || subAgentNodeId

            // Determine status
            let status: 'pending' | 'running' | 'completed' | 'failed' = 'pending'
            if (stepStatusMap && stepStatusMap.has(stepId)) {
              status = stepStatusMap.get(stepId)!
            }

            // Determine change type
            const changeType = getChangeType(stepId, changes)

            // Create the sub-agent node with correct ID from the start
            const subAgentNode: WorkflowNode = {
              id: subAgentNodeId,
              type: 'step',
              position: { x: 0, y: 0 },
              data: {
                id: subAgentNodeId, // CRITICAL: data.id must match node.id
                title: subAgentStep.title || `${route.route_name || route.route_id || routeId}`,
                description: subAgentStep.description,
                success_criteria: subAgentStep.success_criteria,
                status,
                stepIndex: parentStepIndex,
                step: subAgentStep,
                changeType,
                validation_schema: subAgentStep.validation_schema, // Include validation schema for sub-agent
                workspacePath,
                selectedRunFolder,
                // Sub-agent specific info for display
                parentOrchestratorTitle: orchestratorTitle,
                routeName: route.route_name || undefined,
                routeCondition: route.condition || undefined
              } as StepNodeData
            }

            orchestratorSubAgentNodes.push(subAgentNode)

            // Learning nodes are no longer displayed in the workflow canvas
            const subAgentExitNodeId = subAgentNodeId

            // Helper to truncate condition to 10 words
            const truncateToWords = (text: string, maxWords: number): string => {
              if (!text) return ''
              const words = text.trim().split(/\s+/)
              if (words.length <= maxWords) return text
              return words.slice(0, maxWords).join(' ') + '...'
            }

            // Connect orchestrator node to sub-agent (from orchestrator node's bottom handle to sub-agent's top)
            // Show condition on edge (truncated to 10 words)
            const conditionLabel = route.condition
              ? truncateToWords(route.condition, 10)
              : route.route_name || route.route_id

            orchestratorEdges.push({
              id: `${node.id}-route-${route.route_id}-to-sub-agent`,
              source: node.id,
              sourceHandle: route.route_id, // Use route_id as handle (on bottom)
              target: subAgentNodeId,
              targetHandle: 'top', // Connect to top of sub-agent
              type: 'smoothstep',
              label: conditionLabel,
              labelStyle: { fill: '#3b82f6', fontWeight: 600, fontSize: 10 },
              labelBgStyle: { fill: '#eff6ff', fillOpacity: 0.9 },
              labelBgPadding: [3, 3] as [number, number],
              labelBgBorderRadius: 3,
              style: { stroke: '#3b82f6', strokeWidth: 2, strokeDasharray: '5,5' }, // Dashed to show it's conditional
              animated: false
            })

            // Connect sub-agent back to orchestrator node (sub-agents always return)
            orchestratorEdges.push({
              id: `${subAgentExitNodeId}-return-to-${node.id}`,
              source: subAgentExitNodeId,
              target: node.id,
              type: 'smoothstep',
              label: 'Return',
              labelStyle: { fill: '#6b7280', fontWeight: 500, fontSize: 9 },
              labelBgStyle: { fill: '#f9fafb', fillOpacity: 0.9 },
              labelBgPadding: [2, 2] as [number, number],
              labelBgBorderRadius: 3,
              style: { stroke: '#6b7280', strokeWidth: 1.5, strokeDasharray: '3,3' }, // Dashed return path
              animated: false
            })
          }
        })
      }

      // Add sub-agent nodes to the nodes array
      nodes.push(...orchestratorSubAgentNodes)

      // Orchestrator steps connect to next_step_id when success criteria is met (for normal completion)
      // Note: "end" route edges are created above when processing routes
      if (step.next_step_id) {
        const targetNodeId = stepIdToNodeIdMap?.get(step.next_step_id)
        if (targetNodeId) {
          orchestratorEdges.push({
            id: `${sourceNodeId}-orchestrator-to-${targetNodeId}`,
            source: sourceNodeId,
            target: targetNodeId,
            type: 'smoothstep',
            style: { stroke: '#3b82f6', strokeWidth: 2 },
            animated: false
          })
        } else if (step.next_step_id === 'end') {
          // Connect to "end" node (static routing via next_step_id)
          // Use red styling to indicate workflow termination (consistent with "end" route)
          orchestratorEdges.push({
            id: `${sourceNodeId}-orchestrator-to-end-static`,
            source: sourceNodeId,
            target: 'end',
            type: 'smoothstep',
            label: 'Complete',
            labelStyle: { fill: '#ef4444', fontWeight: 600, fontSize: 11 },
            labelBgStyle: { fill: '#fef2f2', fillOpacity: 0.9 },
            labelBgPadding: [4, 4] as [number, number],
            labelBgBorderRadius: 4,
            style: { stroke: '#ef4444', strokeWidth: 2 }, // Red to indicate termination
            animated: false
          })
        }
      }

      edges.push(...orchestratorEdges)

      // Orchestrator steps handle their own routing - don't connect to next sequential step
      lastExitNodeId = null
    }
  })

  return { nodes, edges }
}

/**
 * Check if a node is a step-type node (has step data)
 */
function isStepTypeNode(node: WorkflowNode): node is WorkflowNode & { data: StepNodeData | ConditionalNodeData | DecisionNodeData | LoopNodeData | OrchestratorNodeData | HumanInputNodeData } {
  return node.type === 'step' || node.type === 'conditional' || node.type === 'decision' || node.type === 'loop' || node.type === 'orchestrator' || node.type === 'human_input'
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
 * Helper function to safely extract enable_prerequisite_detection from PlanStep
 * Checks agent_configs first, then falls back to top-level property (for backward compatibility)
 */
function getEnablePrerequisiteDetection(step: PlanStep): boolean | undefined {
  if (step.agent_configs?.enable_prerequisite_detection !== undefined) {
    return step.agent_configs.enable_prerequisite_detection
  }
  // Check top-level property (for backward compatibility)
  const topLevelValue = step['enable_prerequisite_detection']
  if (typeof topLevelValue === 'boolean') {
    return topLevelValue
  }
  return undefined
}

/**
 * Helper function to safely extract prerequisite_rules from PlanStep
 * Checks agent_configs first, then falls back to top-level property (for backward compatibility)
 */
function getPrerequisiteRules(step: PlanStep): PrerequisiteRule[] | undefined {
  if (step.agent_configs?.prerequisite_rules !== undefined) {
    return step.agent_configs.prerequisite_rules
  }
  // Check top-level property (for backward compatibility)
  const topLevelValue = step['prerequisite_rules']
  if (Array.isArray(topLevelValue)) {
    // Type guard: ensure all items match PrerequisiteRule structure
    const isValidRule = (rule: unknown): rule is PrerequisiteRule => {
      if (typeof rule !== 'object' || rule === null) {
        return false
      }
      const ruleObj = rule as Record<string, unknown>
      return (
        'depends_on_step' in ruleObj &&
        'description' in ruleObj &&
        typeof ruleObj.depends_on_step === 'string' &&
        typeof ruleObj.description === 'string'
      )
    }
    if (topLevelValue.every(isValidRule)) {
      return topLevelValue
    }
  }
  return undefined
}

/**
 * Create edges based on prerequisite dependencies
 * Edges go from validation nodes (or step/learning nodes if no validation) to previous step nodes
 * Uses handle-based routing to prevent overlapping edges
 */
function createPrerequisiteEdges(nodes: WorkflowNode[]): WorkflowEdge[] {
  const edges: WorkflowEdge[] = []

  // Filter to validation nodes, learning nodes, and step-type nodes
  const validationNodes = nodes.filter(node => node.type === 'validation')
  const learningNodes = nodes.filter(node => node.type === 'learning')
  const stepNodes = nodes.filter(isStepTypeNode)

  // Create a map of step ID to step node ID
  const stepIdToNodeMap = new Map<string, string>()
  stepNodes.forEach(node => {
    const step = node.data.step
    if (step.id) {
      stepIdToNodeMap.set(step.id, node.id)
    }
  })

  // Create a map of step node ID to step (for getting prerequisite rules)
  const stepNodeIdToStepMap = new Map<string, PlanStep>()
  stepNodes.forEach(node => {
    const step = node.data.step
    if (step.id) {
      stepNodeIdToStepMap.set(node.id, step as PlanStep)
    }
  })

  // Create a map of parent step ID to validation/learning node ID
  const parentStepIdToValidationNodeMap = new Map<string, string>()
  validationNodes.forEach(validationNode => {
    const validationData = validationNode.data as ValidationNodeData
    parentStepIdToValidationNodeMap.set(validationData.parentStepId, validationNode.id)
  })

  const parentStepIdToLearningNodeMap = new Map<string, string>()
  learningNodes.forEach(learningNode => {
    const learningData = learningNode.data as LearningNodeData
    parentStepIdToLearningNodeMap.set(learningData.parentStepId, learningNode.id)
  })

  // Track edges per target step to assign different handles and prevent overlapping
  const targetEdgeCounts = new Map<string, number>()

  // Helper function to create prerequisite edge
  const createPrerequisiteEdge = (
    sourceNodeId: string,
    targetStepNodeId: string,
    depStepId: string,
    rule: { depends_on_step: string; description: string },
    sourceHandle: string,
    targetHandle?: string
  ) => {
    // Get current count for this target to assign handle position
    const currentCount = targetEdgeCounts.get(targetStepNodeId) || 0
    targetEdgeCounts.set(targetStepNodeId, currentCount + 1)

    // Assign handle positions to spread edges horizontally along bottom
    // Use modulo to cycle through handle positions if many edges
    const handlePositions = ['left', 'middle', 'right']
    const handleIndex = currentCount % handlePositions.length
    const finalTargetHandle = targetHandle || `prereq-target-${handlePositions[handleIndex]}`

    // Use description as label (truncate if too long, but allow more characters)
    // Split long text into multiple lines for better readability
    let label = rule.description
    if (label.length > 60) {
      // Try to break at word boundaries
      const words = label.split(' ')
      let line = ''
      const lines: string[] = []
      for (const word of words) {
        if ((line + word).length > 50 && line.length > 0) {
          lines.push(line.trim())
          line = word + ' '
        } else {
          line += word + ' '
        }
      }
      if (line.trim().length > 0) {
        lines.push(line.trim())
      }
      // If still too long, truncate the last line
      if (lines.length > 2) {
        label = lines.slice(0, 2).join('\n') + '...'
      } else {
        label = lines.join('\n')
      }
    }

    edges.push({
      id: `prereq-${sourceNodeId}-to-${targetStepNodeId}-${depStepId}-${currentCount}`,
      source: sourceNodeId,
      target: targetStepNodeId,
      sourceHandle: sourceHandle,
      targetHandle: finalTargetHandle,
      type: 'smoothstep',
      style: { stroke: '#f59e0b', strokeDasharray: '5,5', strokeWidth: 2, opacity: 0.8 },
      animated: false,
      label: label,
      labelStyle: {
        fill: '#f59e0b',
        fontSize: 8,
        fontWeight: 600,
        whiteSpace: 'pre-line',
        textAlign: 'center',
        maxWidth: '200px'
      },
      labelBgStyle: {
        fill: '#fef3c7',
        fillOpacity: 0.95,
        stroke: '#f59e0b',
        strokeWidth: 1
      },
      labelBgPadding: [6, 8] as [number, number],
      labelBgBorderRadius: 4,
      labelShowBg: true
    })
  }

  // Prerequisite detection happens during execution (via detect_prerequisite_failure tool call)
  // Create prerequisite edges from step/learning nodes (not from validation nodes)
  stepNodes.forEach(stepNode => {
    const step = stepNodeIdToStepMap.get(stepNode.id)
    if (!step) return

    // Check for prerequisite rules in agent_configs first, then fall back to top level (for backward compatibility)
    const enablePrerequisiteDetection = getEnablePrerequisiteDetection(step)
    const prerequisiteRules = getPrerequisiteRules(step)

    // Only process if prerequisite detection is enabled and rules exist
    if (!enablePrerequisiteDetection || !prerequisiteRules || prerequisiteRules.length === 0) {
      return
    }

    // Find the appropriate source node: learning node if exists, otherwise step node
    // Prerequisite detection happens during execution, so edges come from execution/learning nodes
    const learningNodeId = parentStepIdToLearningNodeMap.get(stepNode.id)
    const sourceNodeId = learningNodeId || stepNode.id

    prerequisiteRules.forEach((rule: { depends_on_step: string; description: string }) => {
      const depStepId = rule.depends_on_step
      if (depStepId) {
        const targetStepNodeId = stepIdToNodeMap.get(depStepId)
        if (!targetStepNodeId) {
          console.warn('[PrerequisiteEdges] Target step not found in stepIdToNodeMap:', {
            depStepId,
            stepId: step.id,
            stepTitle: step.title,
            availableStepIds: Array.from(stepIdToNodeMap.keys())
          })
        } else if (targetStepNodeId === stepNode.id) {
          // Skip self-reference
        } else {
          // CRITICAL: Verify target node exists and is a StepNode (type 'step') before creating edge
          // Only StepNode (type 'step') has prerequisite target handles (prereq-target-left, prereq-target-middle, prereq-target-right)
          // Other step-type nodes (conditional, decision, loop, orchestrator, human_input) don't have these handles
          const targetNode = nodes.find(node => node.id === targetStepNodeId)
          if (!targetNode) {
            console.warn('[PrerequisiteEdges] Target node not found in nodes array:', {
              targetStepNodeId,
              depStepId,
              stepId: step.id,
              stepTitle: step.title
            })
            return
          }
          
          if (targetNode.type !== 'step') {
            console.warn('[PrerequisiteEdges] Target node is not a StepNode (type "step") - missing prerequisite target handles:', {
              targetStepNodeId,
              targetNodeType: targetNode.type,
              depStepId,
              stepId: step.id,
              stepTitle: step.title
            })
            return
          }
          
          // Use step or learning node as source (execution node)
          const sourceHandleIndex = (targetEdgeCounts.get(targetStepNodeId) || 0) % 3
          const handlePositions = ['left', 'middle', 'right']
          const sourceHandle = `prereq-${handlePositions[sourceHandleIndex]}`
          const targetHandleIndex = (targetEdgeCounts.get(targetStepNodeId) || 0) % 3
          const targetHandle = `prereq-target-${handlePositions[targetHandleIndex]}`
          createPrerequisiteEdge(sourceNodeId, targetStepNodeId, depStepId, rule, sourceHandle, targetHandle)
        }
      }
    })
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
    showPrerequisiteEdges = true, // Always show prerequisite edges by default
    changes = null,
    onRunFromStep,
    onOpenSidebar,
    isExecuting = false,
    completedStepIndices = [],
    stepStatusMap,
    variablesManifest = null,
    onOpenVariablesSidebar,
    isLoadingVariables = false
  } = options

  // Get preset for code execution mode default
  const activePresetId = useGlobalPresetStore(state => state.activePresetIds.workflow)
  const customPresets = useGlobalPresetStore(state => state.customPresets)
  const predefinedPresets = useGlobalPresetStore(state => state.predefinedPresets)

  const activePreset = useMemo(() => {
    if (activePresetId) {
      const customPreset = customPresets.find(p => p.id === activePresetId)
      if (customPreset) return customPreset
      const predefinedPreset = predefinedPresets.find(p => p.id === activePresetId)
      if (predefinedPreset) return predefinedPreset
    }
    return null
  }, [activePresetId, customPresets, predefinedPresets])

  const presetUseCodeExecutionMode = activePreset?.useCodeExecutionMode ?? false

  // Get preset LLM configs
  const presetLLMConfig = activePreset?.llmConfig || undefined
  const presetValidationLLM = activePreset?.llmConfig?.validation_llm
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

  // Convert serialized stepStatusMap back to Map for use in processing
  const stepStatusMapAsMap = useMemo(() => {
    if (!stepStatusMapSerialized) return undefined
    // Convert object back to Map
    return new Map(Object.entries(stepStatusMapSerialized)) as Map<string, 'pending' | 'running' | 'completed' | 'failed'>
  }, [stepStatusMapSerialized])

  return useMemo(() => {
    if (!plan || !plan.steps || plan.steps.length === 0) {
      return { nodes: [], edges: [] }
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

    // Process all steps to create nodes and sequential edges (with change highlighting)
    const { nodes: processedNodes, edges: sequentialEdges } = processSteps(
      plan.steps,
      undefined,
      undefined,
      changes,
      presetUseCodeExecutionMode,
      presetLLMConfig,
      presetValidationLLM,
      presetLearningLLM,
      availableLLMs,
      completedStepIndices,
      stepStatusMapAsMap,
      options.workspacePath,
      options.selectedRunFolder,
      stepIdToNodeIdMap,
      completedStepIds
    )

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

    // Add execution settings node (between start and variables)
    const executionSettingsNode: WorkflowNode = {
      id: 'execution-settings',
      type: 'execution-settings',
      position: { x: 0, y: 0 },
      data: {} as ExecutionSettingsNodeData
    }

    // Add variables node (between execution-settings and first step)
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

    // Node order: Start -> Execution Settings -> Variables -> Steps -> End
    const nodes = [startNode, executionSettingsNode, variablesNode, ...processedNodes, endNode]

    // Create edges: Start -> Execution Settings -> Variables -> First step (or End if no steps)
    const edges: WorkflowEdge[] = []

    // Start to Execution Settings
    edges.push({
      id: 'start-to-execution-settings',
      source: 'start',
      target: 'execution-settings',
      type: 'smoothstep',
      style: { stroke: '#6b7280', strokeWidth: 2 }
    })

    // Execution Settings to Variables
    edges.push({
      id: 'execution-settings-to-variables',
      source: 'execution-settings',
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
    }

    // Add sequential edges
    edges.push(...sequentialEdges)

    // Create dependency edges (context flow) - only if enabled
    if (showDependencyEdges) {
      const dependencyEdges = createDependencyEdges(processedNodes)
      edges.push(...dependencyEdges)
    }

    // Create prerequisite edges - only if enabled
    if (showPrerequisiteEdges) {
      const prerequisiteEdges = createPrerequisiteEdges(processedNodes)
      edges.push(...prerequisiteEdges)
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

    // Apply dagre layout
    const layoutedResult = layoutWithDagre(nodes, edges)

    // Position sub-agents vertically below their parent orchestrator nodes
    console.log('[Layout Debug] === Sub-agent Positioning ===')
    const orchestratorNodeMap = new Map<string, { nodeIndex: number; subAgentIndices: number[] }>()

    // Find all orchestrator nodes and their sub-agents by index
    layoutedResult.nodes.forEach((node, index) => {
      if (node.type === 'orchestrator') {
        orchestratorNodeMap.set(node.id, { nodeIndex: index, subAgentIndices: [] })
      } else if (node.id.includes('-sub-agent-')) {
        // Extract parent orchestrator node ID from sub-agent ID
        const parentId = node.id.split('-sub-agent-')[0]
        const orchestratorInfo = orchestratorNodeMap.get(parentId)
        if (orchestratorInfo) {
          orchestratorInfo.subAgentIndices.push(index)
        }
      }
    })

    // Position sub-agents in HORIZONTAL ROW below their parent orchestrator node
    orchestratorNodeMap.forEach(({ nodeIndex: orchestratorNodeIndex, subAgentIndices }) => {
      const orchestratorNode = layoutedResult.nodes[orchestratorNodeIndex]
      const orchestratorDimensions = NODE_DIMENSIONS.orchestrator || NODE_DIMENSIONS.step
      console.log(`[Layout Debug] Orchestrator "${orchestratorNode.id}" at y=${Math.round(orchestratorNode.position.y)}, has ${subAgentIndices.length} sub-agents`)

      if (subAgentIndices.length === 0) return

      const VERTICAL_GAP = 120  // Gap between orchestrator and sub-agents row
      const HORIZONTAL_GAP = 40 // Gap between sub-agents

      // Calculate total width needed for all sub-agents
      let totalWidth = 0
      subAgentIndices.forEach((idx, i) => {
        const dims = NODE_DIMENSIONS[layoutedResult.nodes[idx].type as keyof typeof NODE_DIMENSIONS] || NODE_DIMENSIONS.step
        totalWidth += dims.width
        if (i < subAgentIndices.length - 1) {
          totalWidth += HORIZONTAL_GAP
        }
      })

      // Center sub-agents under orchestrator
      const startX = orchestratorNode.position.x + (orchestratorDimensions.width - totalWidth) / 2
      const subAgentY = orchestratorNode.position.y + orchestratorDimensions.height + VERTICAL_GAP

      console.log(`[Layout Debug] Sub-agents will be arranged horizontally at y=${Math.round(subAgentY)}, starting x=${Math.round(startX)}`)

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

        currentX += subAgentDimensions.width + HORIZONTAL_GAP
      })
    })

    // After Dagre + orchestrator positioning, keep validation/learning/evaluation nodes
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

        // For decision parents, keep evaluation node very close and only slightly staggered
        const isDecisionParent = anchorNode!.type === 'decision'
        const horizontalOffset = isDecisionParent ? 16 : 48
        const verticalGap = isDecisionParent ? 8 : 24

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

    // Apply global collision detection and resolution to fix any remaining overlaps
    // This handles overlaps from orchestrator sub-agents, conditional branches, loops, etc.
    const nodesBeforeCollision = layoutedResult.nodes.length
    layoutedResult.nodes = detectAndResolveCollisions(layoutedResult.nodes)
    const nodesAfterCollision = layoutedResult.nodes.length
    if (nodesBeforeCollision !== nodesAfterCollision) {
      // Node count changed during collision detection (log removed to reduce console noise)
    }

    // Helper to determine if a step can run
    // All steps can run regardless of previous step completion
    const canStepRun = (): boolean => {
      return true
    }

    // Inject onRunFromStep callback, onOpenSidebar callback, isExecuting state, canRun, workspacePath, and selectedRunFolder into step-type nodes
    // Also make validation, learning, and evaluation nodes non-draggable
    layoutedResult.nodes = layoutedResult.nodes.map(node => {
      if (node.type === 'step' || node.type === 'conditional' || node.type === 'loop' || node.type === 'decision' || node.type === 'orchestrator' || node.type === 'human_input') {
        const canRun = canStepRun()
        // Sub-agents cannot be run independently (they are part of routing steps)
        const isSubAgent = node.id.includes('-sub-agent-')
        return {
          ...node,
          data: {
            ...node.data,
            // Don't allow running sub-agents independently
            onRunFromStep: isSubAgent ? undefined : onRunFromStep,
            onOpenSidebar,
            isExecuting,
            canRun,
            workspacePath: options.workspacePath,
            selectedRunFolder: options.selectedRunFolder
          }
        } as WorkflowNode
      }

      // Validation, learning, and evaluation nodes are now draggable (can be manually positioned)
      // They can be moved independently or will move with their parent nodes
      return node
    }) as WorkflowNode[]

    return layoutedResult
  }, [plan, showDependencyEdges, showPrerequisiteEdges, changes, presetUseCodeExecutionMode, presetLLMConfig, presetValidationLLM, presetLearningLLM, availableLLMs, onRunFromStep, onOpenSidebar, isExecuting, completedStepIndices, stepStatusMapAsMap, options.workspacePath, options.selectedRunFolder, variablesManifest, onOpenVariablesSidebar, isLoadingVariables])
}

export default usePlanToFlow

