import { useMemo } from 'react'
import type { Node, Edge } from '@xyflow/react'
import dagre from 'dagre'
import type { PlanStep, PlanningResponse, AgentConfigs, AgentLLMConfig } from '../../../utils/stepConfigMatching'
import type { ChangeType, PlanChanges } from './usePlanData'
import type { VariablesManifest } from '../../../services/api-types'
import type { VariablesNodeData } from '../nodes/VariablesNode'
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
  canRun?: boolean  // Whether this step can be run (all previous steps completed)
  workspacePath?: string | null  // Workspace path for file opening
  selectedRunFolder?: string  // Selected iteration folder for file opening
}

export interface ConditionalNodeData extends Record<string, unknown> {
  id: string
  title: string
  condition_question?: string
  condition_context?: string
  status: 'pending' | 'evaluating' | 'decided_true' | 'decided_false'
  stepIndex: number
  step: PlanStep
  changeType?: ChangeType  // Highlight type for visual feedback
  onRunFromStep?: OnRunFromStepCallback  // Callback to run from this step
  onOpenSidebar?: OnOpenSidebarCallback  // Callback to open sidebar for editing
  isExecuting?: boolean  // Whether execution is in progress
  canRun?: boolean  // Whether this step can be run (all previous steps completed)
  workspacePath?: string | null  // Workspace path for file opening
  selectedRunFolder?: string  // Selected iteration folder for file opening
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
  canRun?: boolean  // Whether this step can be run (all previous steps completed)
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

export type WorkflowNodeData = StepNodeData | ConditionalNodeData | LoopNodeData | ValidationNodeData | LearningNodeData | VariablesNodeData

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
  stepStatusMap?: Map<string, 'pending' | 'running' | 'completed' | 'failed'>  // Step status from events
  workspacePath?: string | null  // Workspace path for file opening
  selectedRunFolder?: string  // Selected iteration folder for file opening
  variablesManifest?: VariablesManifest | null  // Variables manifest for Variables node
  onOpenVariablesSidebar?: () => void  // Callback for opening variables sidebar
  isLoadingVariables?: boolean  // Whether variables are being loaded
}

// Dagre layout configuration
const DAGRE_CONFIG = {
  rankdir: 'LR', // Left to right (tree layout)
  nodesep: 300,  // Vertical spacing between nodes in same rank (increased significantly to prevent branch overlap)
  ranksep: 150,  // Horizontal spacing between ranks (columns)
  marginx: 60,
  marginy: 60
}

// Node dimensions for layout calculation
const NODE_DIMENSIONS = {
  step: { width: 340, height: 200 },
  conditional: { width: 300, height: 160 },
  loop: { width: 360, height: 280 },
  validation: { width: 120, height: 50 },
  learning: { width: 120, height: 50 },
  start: { width: 80, height: 36 },
  end: { width: 80, height: 36 },
  variables: { width: 260, height: 180 }
}

/**
 * Post-process layout to fix overlapping conditional branches
 * Ensures branches from different conditionals don't overlap vertically
 */
function fixOverlappingBranches(nodes: WorkflowNode[]): WorkflowNode[] {
  // Find all conditional nodes and their branch nodes
  const conditionalNodes = nodes.filter(n => n.type === 'conditional')
  const branchGroups: Array<{ 
    conditionalId: string
    conditionalNode: WorkflowNode
    branchNodes: WorkflowNode[] 
  }> = []

  conditionalNodes.forEach(conditionalNode => {
    const branchNodes = nodes.filter(n => 
      n.id.startsWith(`${conditionalNode.id}-true-`) || 
      n.id.startsWith(`${conditionalNode.id}-false-`)
    )
    if (branchNodes.length > 0) {
      branchGroups.push({
        conditionalId: conditionalNode.id,
        conditionalNode,
        branchNodes
      })
    }
  })  

  // If we have multiple conditional branches, check for overlaps and adjust
  if (branchGroups.length < 2) {
    return nodes // No need to adjust if less than 2 conditionals
  }

  // Sort branch groups by their conditional node's x position (left to right)
  branchGroups.sort((a, b) => a.conditionalNode.position.x - b.conditionalNode.position.x)

  const adjustedNodes = [...nodes]
  const nodeMap = new Map(adjustedNodes.map(n => [n.id, n]))

  // Get vertical bounds for a branch group using current adjusted positions
  const getGroupBounds = (group: typeof branchGroups[0]): { top: number; bottom: number } | null => {
    if (group.branchNodes.length === 0) return null
    const positions = group.branchNodes.map(branchNode => {
      const node = nodeMap.get(branchNode.id)!
      const dimensions = NODE_DIMENSIONS[node.type as keyof typeof NODE_DIMENSIONS] || NODE_DIMENSIONS.step
      return {
        top: node.position.y,
        bottom: node.position.y + dimensions.height
      }
    })
    return {
      top: Math.min(...positions.map(p => p.top)),
      bottom: Math.max(...positions.map(p => p.bottom))
    }
  }

  // For each branch group (after the first), ensure it doesn't overlap with previous groups
  for (let i = 1; i < branchGroups.length; i++) {
    const currentGroup = branchGroups[i]
    const currentBounds = getGroupBounds(currentGroup)
    if (!currentBounds) continue

    // Check overlap with all previous groups
    let maxBottom = -Infinity
    for (let j = 0; j < i; j++) {
      const prevGroup = branchGroups[j]
      const prevBounds = getGroupBounds(prevGroup)
      if (prevBounds) {
        maxBottom = Math.max(maxBottom, prevBounds.bottom)
      }
    }

    // If current group overlaps with previous groups, move it down
    if (currentBounds.top < maxBottom) {
      const minSeparation = 800 // Minimum vertical separation between branch groups (increased significantly)
      const separationNeeded = maxBottom - currentBounds.top + minSeparation
      
      // Move all nodes in current branch group down
      currentGroup.branchNodes.forEach(branchNode => {
        const nodeIndex = adjustedNodes.findIndex(n => n.id === branchNode.id)
        if (nodeIndex >= 0) {
          adjustedNodes[nodeIndex] = {
            ...adjustedNodes[nodeIndex],
            position: {
              ...adjustedNodes[nodeIndex].position,
              y: adjustedNodes[nodeIndex].position.y + separationNeeded
            }
          }
          // Update the map so subsequent calculations use adjusted positions
          nodeMap.set(branchNode.id, adjustedNodes[nodeIndex])
        }
      })
    }
  }

  return adjustedNodes
}

/**
 * Auto-layout nodes using Dagre algorithm
 */
function layoutWithDagre(nodes: WorkflowNode[], edges: WorkflowEdge[]): { nodes: WorkflowNode[], edges: WorkflowEdge[] } {
  const g = new dagre.graphlib.Graph()
  g.setGraph(DAGRE_CONFIG)
  g.setDefaultEdgeLabel(() => ({}))

  // Add nodes to dagre graph
  nodes.forEach(node => {
    const dimensions = NODE_DIMENSIONS[node.type as keyof typeof NODE_DIMENSIONS] || NODE_DIMENSIONS.step
    g.setNode(node.id, { width: dimensions.width, height: dimensions.height })
  })

  // Add edges to dagre graph
  edges.forEach(edge => {
    g.setEdge(edge.source, edge.target)
  })

  // Run layout
  dagre.layout(g)

  // Apply positions to nodes
  const layoutedNodes = nodes.map(node => {
    const nodeWithPosition = g.node(node.id)
    const dimensions = NODE_DIMENSIONS[node.type as keyof typeof NODE_DIMENSIONS] || NODE_DIMENSIONS.step
    
    return {
      ...node,
      position: {
        x: nodeWithPosition.x - dimensions.width / 2,
        y: nodeWithPosition.y - dimensions.height / 2
      }
    }
  })

  // Post-process to fix overlapping branches
  const fixedNodes = fixOverlappingBranches(layoutedNodes)

  return { nodes: fixedNodes, edges }
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
  completedStepIndices: number[] = [],
  stepStatusMap?: Map<string, 'pending' | 'running' | 'completed' | 'failed'>,
  workspacePath?: string | null,
  selectedRunFolder?: string
): WorkflowNode {
  const nodeId = parentId 
    ? `${parentId}-${branchType}-${stepIndex}`
    : step.id || `step-${stepIndex}`

  // Determine change type for highlighting
  const changeType = getChangeType(step.id || nodeId, changes)

  // Determine status: priority is stepStatusMap (from events) > completedStepIndices > pending
  let status: 'pending' | 'running' | 'completed' | 'failed' = 'pending'
  const stepId = step.id || nodeId
  
  // First check stepStatusMap (from events) - this is the most up-to-date
  if (stepStatusMap && stepStatusMap.has(stepId)) {
    status = stepStatusMap.get(stepId)!
  } else {
    // Fall back to completedStepIndices (only for main plan steps, not nested branches)
    // For main plan steps, stepIndex matches the global index
    // For nested steps, we don't check completion status (they're tracked differently)
    const isCompleted = !parentId && completedStepIndices.includes(stepIndex)
    status = isCompleted ? 'completed' as const : 'pending' as const
  }

  // For conditional nodes, use condition_question as title if step.title is missing
  // For other steps, use step.title or fallback to "Step N"
  const getStepTitle = () => {
    if (step.has_condition) {
      // For conditional nodes, prefer condition_question over generic "Step N"
      return step.title || step.condition_question || `Condition ${stepIndex + 1}`
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
    selectedRunFolder
  }

  if (step.has_condition) {
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

  if (step.has_loop) {
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
 * Returns the nodes and edges, plus the "exit" node ID (for connecting to next step)
 */
function createValidationLearningNodes(
  step: PlanStep,
  stepNodeId: string,
  presetUseCodeExecutionMode: boolean,
  presetLLMConfig: AgentLLMConfig | undefined,
  presetValidationLLM: AgentLLMConfig | undefined,
  presetLearningLLM: AgentLLMConfig | undefined,
  availableLLMs: Array<{ provider: string; model: string; label: string }>
): { nodes: WorkflowNode[], edges: WorkflowEdge[], exitNodeId: string } {
  const nodes: WorkflowNode[] = []
  const edges: WorkflowEdge[] = []
  
  const agentConfigs = step.agent_configs as AgentConfigs | undefined
  
  // Determine if code execution mode is enabled
  const stepCodeExecSetting = agentConfigs?.use_code_execution_mode
  const useCodeExecutionMode = stepCodeExecSetting !== undefined 
    ? stepCodeExecSetting 
    : presetUseCodeExecutionMode  // Fall back to preset default
  
  // In code exec mode: validation and learning are always enabled
  // Otherwise: check disable flags
  const hasValidation = useCodeExecutionMode 
    ? true  // Always enabled in code exec mode
    : !agentConfigs?.disable_validation
  
  const hasLearning = useCodeExecutionMode 
    ? true  // Always enabled in code exec mode
    : !agentConfigs?.disable_learning
  
  let currentNodeId = stepNodeId
  
  // Get validation LLM provider and model
  const validationLLM = agentConfigs?.validation_llm || presetValidationLLM || presetLLMConfig
  const validationLLMInfo = getLLMProviderAndModel(validationLLM, presetLLMConfig, availableLLMs)
  
  // Get learning LLM provider and model
  // Always use learning_llm config (not execution_llm), even in code exec mode
  // Priority: step learning_llm → preset learning_llm → preset default (provider/model_id)
  const presetDefaultLLM = presetLLMConfig?.provider && presetLLMConfig?.model_id 
    ? { provider: presetLLMConfig.provider, model_id: presetLLMConfig.model_id } : undefined
  const learningLLM = agentConfigs?.learning_llm || presetLearningLLM || presetDefaultLLM || presetLLMConfig
  const learningLLMInfo = getLLMProviderAndModel(learningLLM, presetLLMConfig, availableLLMs)
  
  // Add validation node if enabled
  if (hasValidation) {
    const validationNodeId = `${stepNodeId}-validation`
    const validationNode: WorkflowNode = {
      id: validationNodeId,
      type: 'validation',
      position: { x: 0, y: 0 },
      data: {
        id: validationNodeId,
        parentStepId: stepNodeId,
        parentStepTitle: step.title,
        status: 'pending',
        llmProvider: validationLLMInfo.provider,
        llmModel: validationLLMInfo.model
      } as ValidationNodeData
    }
    nodes.push(validationNode)
    
    // Edge from step to validation
    edges.push({
      id: `${stepNodeId}-to-validation`,
      source: stepNodeId,
      target: validationNodeId,
      type: 'smoothstep',
      animated: false,
      style: { stroke: '#6366f1', strokeWidth: 2 }
    })
    
    // Loop-back edge from validation to step (retry)
    edges.push({
      id: `${validationNodeId}-retry-to-${stepNodeId}`,
      source: validationNodeId,
      sourceHandle: 'retry',
      target: stepNodeId,
      targetHandle: 'retry',
      type: 'smoothstep',
      label: 'Retry',
      labelStyle: { fill: '#f59e0b', fontWeight: 500, fontSize: 9 },
      labelBgStyle: { fill: '#fffbeb', fillOpacity: 0.9 },
      labelBgPadding: [2, 2] as [number, number],
      labelBgBorderRadius: 3,
      style: { stroke: '#f59e0b', strokeWidth: 1.5, strokeDasharray: '4 2' },
      animated: false
    })
    
    currentNodeId = validationNodeId
  }
  
  // Add learning node if enabled
  if (hasLearning) {
    const learningNodeId = `${stepNodeId}-learning`
    const learningNode: WorkflowNode = {
      id: learningNodeId,
      type: 'learning',
      position: { x: 0, y: 0 },
      data: {
        id: learningNodeId,
        parentStepId: stepNodeId,
        parentStepTitle: step.title,
        status: 'pending',
        llmProvider: learningLLMInfo.provider,
        llmModel: learningLLMInfo.model
      } as LearningNodeData
    }
    nodes.push(learningNode)
    
    // Edge from previous node (step or validation) to learning
    edges.push({
      id: `${currentNodeId}-to-learning`,
      source: currentNodeId,
      target: learningNodeId,
      type: 'smoothstep',
      animated: false,
      style: { stroke: '#f59e0b', strokeWidth: 2 }
    })
    
    currentNodeId = learningNodeId
  }
  
  return { nodes, edges, exitNodeId: currentNodeId }
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
  stepIdToNodeIdMap?: Map<string, string> // Map of step ID to node ID for next_step_id lookups
): { nodes: WorkflowNode[], edges: WorkflowEdge[] } {
  const nodes: WorkflowNode[] = []
  const edges: WorkflowEdge[] = []
  
  // Track the last "exit" node ID for edge connections
  // Can be a single node ID, array of branch exit nodes, or null
  let lastExitNodeId: string | string[] | null = null
  // Track conditional nodes with empty branches for label purposes
  const conditionalEmptyBranches = new Map<string, { trueEmpty: boolean; falseEmpty: boolean }>()

  steps.forEach((step, index) => {
    const node = stepToNode(step, index, parentId, branchType, changes, completedStepIndices, stepStatusMap, workspacePath, selectedRunFolder)
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
    
    // Add validation/learning nodes for non-conditional steps
    if (!step.has_condition) {
      const vlResult = createValidationLearningNodes(
        step, 
        node.id, 
        presetUseCodeExecutionMode,
        presetLLMConfig,
        presetValidationLLM,
        presetLearningLLM,
        availableLLMs
      )
      nodes.push(...vlResult.nodes)
      edges.push(...vlResult.edges)
      lastExitNodeId = vlResult.exitNodeId
    } else {
      // For conditional nodes, track branch exit nodes to reconnect to next step
      lastExitNodeId = null
    }

    // Handle conditional branches
    if (step.has_condition) {
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
          stepIdToNodeIdMap
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
          stepIdToNodeIdMap
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
  })

  return { nodes, edges }
}

/**
 * Check if a node is a step-type node (has step data)
 */
function isStepTypeNode(node: WorkflowNode): node is WorkflowNode & { data: StepNodeData | ConditionalNodeData | LoopNodeData } {
  return node.type === 'step' || node.type === 'conditional' || node.type === 'loop'
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
 * Create edges based on prerequisite dependencies
 * Edges go from validation nodes to previous step nodes
 * Uses handle-based routing to prevent overlapping edges
 */
function createPrerequisiteEdges(nodes: WorkflowNode[]): WorkflowEdge[] {
  const edges: WorkflowEdge[] = []
  
  // Filter to validation nodes and step-type nodes
  const validationNodes = nodes.filter(node => node.type === 'validation')
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

  // Track edges per target step to assign different handles and prevent overlapping
  const targetEdgeCounts = new Map<string, number>()
  
  // For each validation node, check if its parent step has prerequisite rules
  validationNodes.forEach(validationNode => {
    const validationData = validationNode.data as ValidationNodeData
    const parentStepId = validationData.parentStepId
    
    // Find the parent step node
    const parentStepNode = stepNodes.find(node => node.id === parentStepId)
    if (!parentStepNode) return
    
    // Get the step data to access prerequisite rules
    const parentStep = stepNodeIdToStepMap.get(parentStepId)
    if (!parentStep) return
    
    const agentConfigs = parentStep.agent_configs
    if (agentConfigs?.enable_prerequisite_detection && agentConfigs?.prerequisite_rules) {
      // Create edges from validation node to each dependency step
      agentConfigs.prerequisite_rules.forEach((rule: { depends_on_step: string; description: string }) => {
        const depStepId = rule.depends_on_step
        if (depStepId) {
          const targetStepNodeId = stepIdToNodeMap.get(depStepId)
          if (targetStepNodeId && targetStepNodeId !== parentStepId) {
            // Get current count for this target to assign handle position
            const currentCount = targetEdgeCounts.get(targetStepNodeId) || 0
            targetEdgeCounts.set(targetStepNodeId, currentCount + 1)
            
            // Assign handle positions to spread edges horizontally along bottom
            // Use modulo to cycle through handle positions if many edges
            const handlePositions = ['left', 'middle', 'right']
            const handleIndex = currentCount % handlePositions.length
            const targetHandle = `prereq-${handlePositions[handleIndex]}`
            
            // For source (validation node), use different handles based on rule index
            const sourceHandleIndex = currentCount % 3
            const sourceHandle = `prereq-${handlePositions[sourceHandleIndex]}`
            
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
              id: `prereq-${validationNode.id}-to-${targetStepNodeId}-${depStepId}-${currentCount}`,
              source: validationNode.id,
              target: targetStepNodeId,
              sourceHandle: sourceHandle,
              targetHandle: targetHandle,
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
  
  return useMemo(() => {
    if (!plan || !plan.steps || plan.steps.length === 0) {
      return { nodes: [], edges: [] }
    }

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
        if (step.if_true_steps) {
          buildStepIdMap(step.if_true_steps, nodeId, 'true')
        }
        if (step.if_false_steps) {
          buildStepIdMap(step.if_false_steps, nodeId, 'false')
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
      stepStatusMap,
      options.workspacePath,
      options.selectedRunFolder,
      stepIdToNodeIdMap
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

    // Node order: Start -> Variables -> Steps -> End
    const nodes = [startNode, variablesNode, ...processedNodes, endNode]

    // Create edges: Start -> Variables -> First step (or End if no steps)
    const edges: WorkflowEdge[] = []
    
    // Start to Variables
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
      const hasCondition = isStepType && lastNode.data.step.has_condition
      
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
        if (conditionalStep.if_true_steps && conditionalStep.if_true_steps.length > 0) {
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
            if (!hasConnection && !conditionalStep.if_true_next_step_id) {
              branchExitNodes.push(lastTrueNode.id)
            }
          }
        } else {
          // Empty true branch - conditional node itself is the exit
          const hasConnection = existingEndEdges.some(e => 
            e.id.includes('true') || (e.source === lastNode.id && e.id.includes('true'))
          )
          if (!hasConnection && !conditionalStep.if_true_next_step_id) {
            branchExitNodes.push(lastNode.id)
          }
        }
        
        // Check false branch
        if (conditionalStep.if_false_steps && conditionalStep.if_false_steps.length > 0) {
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
            if (!hasConnection && !conditionalStep.if_false_next_step_id) {
              branchExitNodes.push(lastFalseNode.id)
            }
          }
        } else {
          // Empty false branch - conditional node itself is the exit (if not already added)
          if (!branchExitNodes.includes(lastNode.id)) {
            const hasConnection = existingEndEdges.some(e => 
              e.id.includes('false') || (e.source === lastNode.id && e.id.includes('false'))
            )
            if (!hasConnection && !conditionalStep.if_false_next_step_id) {
              branchExitNodes.push(lastNode.id)
            }
          }
        }
        
        // Connect branch exit nodes to end
        branchExitNodes.forEach((exitNodeId, index) => {
          // Determine label based on which branch this is
          const isTrueBranch = conditionalStep.if_true_steps && conditionalStep.if_true_steps.length === 0 && exitNodeId === lastNode.id
            ? true
            : exitNodeId.includes('-true-') || (exitNodeId === lastNode.id && conditionalStep.if_true_steps && conditionalStep.if_true_steps.length === 0)
          const isFalseBranch = !isTrueBranch && (
            exitNodeId.includes('-false-') || 
            (exitNodeId === lastNode.id && conditionalStep.if_false_steps && conditionalStep.if_false_steps.length === 0)
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
    
    // Create a Set for faster lookup of completed step indices
    const completedSet = new Set(completedStepIndices)
    
    // Helper to determine if a step can run
    // A step can run if all previous steps (0 to stepIndex-1) are completed
    const canStepRun = (stepIndex: number): boolean => {
      // First step can always run
      if (stepIndex === 0) return true
      // Check all previous steps are completed
      for (let i = 0; i < stepIndex; i++) {
        if (!completedSet.has(i)) return false
      }
      return true
    }
    
    // Inject onRunFromStep callback, onOpenSidebar callback, isExecuting state, canRun, workspacePath, and selectedRunFolder into step-type nodes
    layoutedResult.nodes = layoutedResult.nodes.map(node => {
      if (node.type === 'step' || node.type === 'conditional' || node.type === 'loop') {
        const stepIndex = (node.data as StepNodeData | ConditionalNodeData | LoopNodeData).stepIndex
        const canRun = canStepRun(stepIndex)
        return {
          ...node,
          data: {
            ...node.data,
            onRunFromStep,
            onOpenSidebar,
            isExecuting,
            canRun,
            workspacePath: options.workspacePath,
            selectedRunFolder: options.selectedRunFolder
          }
        } as WorkflowNode
      }
      return node
    }) as WorkflowNode[]
    
    return layoutedResult
  }, [plan, showDependencyEdges, showPrerequisiteEdges, changes, presetUseCodeExecutionMode, presetLLMConfig, presetValidationLLM, presetLearningLLM, availableLLMs, onRunFromStep, onOpenSidebar, isExecuting, completedStepIndices, stepStatusMap, options.workspacePath, options.selectedRunFolder, variablesManifest, onOpenVariablesSidebar, isLoadingVariables])
}

export default usePlanToFlow

