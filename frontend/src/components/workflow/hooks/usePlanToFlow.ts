import { useMemo } from 'react'
import type { Node, Edge } from '@xyflow/react'
import dagre from 'dagre'
import type { PlanStep, PlanningResponse, AgentConfigs, AgentLLMConfig } from '../../../utils/stepConfigMatching'
import type { ChangeType, PlanChanges } from './usePlanData'
import { useGlobalPresetStore } from '../../../stores/useGlobalPresetStore'
import { useLLMStore } from '../../../stores/useLLMStore'

// Callback type for running from a specific step
export type OnRunFromStepCallback = (stepIndex: number, stepId: string) => void

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

export type WorkflowNodeData = StepNodeData | ConditionalNodeData | LoopNodeData | ValidationNodeData | LearningNodeData

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
  onRunFromStep?: OnRunFromStepCallback  // Callback for "run from step" button
  isExecuting?: boolean  // Whether execution is currently in progress
  completedStepIndices?: number[]  // 0-based indices of completed steps (from steps_done.json)
  workspacePath?: string | null  // Workspace path for file opening
  selectedRunFolder?: string  // Selected iteration folder for file opening
}

// Dagre layout configuration
const DAGRE_CONFIG = {
  rankdir: 'LR', // Left to right (tree layout)
  nodesep: 80,   // Vertical spacing between nodes in same rank
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
  end: { width: 80, height: 36 }
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
  completedStepIndices: number[] = [],
  workspacePath?: string | null,
  selectedRunFolder?: string
): WorkflowNode {
  const nodeId = parentId 
    ? `${parentId}-${branchType}-${stepIndex}`
    : step.id || `step-${stepIndex}`

  // Determine change type for highlighting
  const changeType = getChangeType(step.id || nodeId, changes)

  // Check if this step is completed (only for main plan steps, not nested branches)
  // For main plan steps, stepIndex matches the global index
  // For nested steps, we don't check completion status (they're tracked differently)
  const isCompleted = !parentId && completedStepIndices.includes(stepIndex)
  const status = isCompleted ? 'completed' as const : 'pending' as const

  const baseData = {
    id: nodeId,
    title: step.title || `Step ${stepIndex + 1}`,
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
  
  // Get learning LLM provider and model (uses execution_llm in code exec mode)
  const learningLLM = useCodeExecutionMode
    ? (agentConfigs?.execution_llm || presetLLMConfig)
    : (agentConfigs?.learning_llm || presetLearningLLM || presetLLMConfig)
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
  workspacePath?: string | null,
  selectedRunFolder?: string
): { nodes: WorkflowNode[], edges: WorkflowEdge[] } {
  const nodes: WorkflowNode[] = []
  const edges: WorkflowEdge[] = []
  
  // Track the last "exit" node ID for edge connections
  let lastExitNodeId: string | null = null

  steps.forEach((step, index) => {
    const node = stepToNode(step, index, parentId, branchType, changes, completedStepIndices, workspacePath, selectedRunFolder)
    nodes.push(node)

    // Create edge from previous step's exit node (sequential flow)
    if (lastExitNodeId) {
      edges.push({
        id: `${lastExitNodeId}-to-${node.id}`,
        source: lastExitNodeId,
        target: node.id,
        type: 'smoothstep',
        animated: false,
        style: { stroke: '#6b7280', strokeWidth: 2 }
      })
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
      // For conditional nodes, they connect to branches, not directly to validation/learning
      lastExitNodeId = null
    }

    // Handle conditional branches
    if (step.has_condition) {
      // Process if_true_steps
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
          workspacePath,
          selectedRunFolder
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
        }
      }

      // Process if_false_steps
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
          workspacePath,
          selectedRunFolder
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
 * Hook to convert plan.json to React Flow nodes and edges
 */
export function usePlanToFlow(
  plan: PlanningResponse | null, 
  options: UsePlanToFlowOptions = {}
): UsePlanToFlowResult {
  const { showDependencyEdges = false, changes = null, onRunFromStep, isExecuting = false, completedStepIndices = [] } = options
  
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
      options.workspacePath,
      options.selectedRunFolder
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

    const nodes = [startNode, ...processedNodes, endNode]

    // Create edge from start to first step
    const edges: WorkflowEdge[] = []
    if (processedNodes.length > 0) {
      edges.push({
        id: 'start-to-first',
        source: 'start',
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
        edges.push({
          id: 'last-to-end',
          source: lastNode.id,
          target: 'end',
          type: 'smoothstep',
          style: { stroke: '#6b7280', strokeWidth: 2 }
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
    
    // Inject onRunFromStep callback, isExecuting state, canRun, workspacePath, and selectedRunFolder into step-type nodes
    layoutedResult.nodes = layoutedResult.nodes.map(node => {
      if (node.type === 'step' || node.type === 'conditional' || node.type === 'loop') {
        const stepIndex = (node.data as StepNodeData | ConditionalNodeData | LoopNodeData).stepIndex
        const canRun = canStepRun(stepIndex)
        return {
          ...node,
          data: {
            ...node.data,
            onRunFromStep,
            isExecuting,
            canRun,
            workspacePath: options.workspacePath,
            selectedRunFolder: options.selectedRunFolder
          }
        }
      }
      return node
    })
    
    return layoutedResult
  }, [plan, showDependencyEdges, changes, presetUseCodeExecutionMode, presetLLMConfig, presetValidationLLM, presetLearningLLM, availableLLMs, onRunFromStep, isExecuting, completedStepIndices, options.workspacePath, options.selectedRunFolder])
}

export default usePlanToFlow

