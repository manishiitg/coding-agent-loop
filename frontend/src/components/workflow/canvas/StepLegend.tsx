import React, { useMemo, useState, useCallback } from 'react'
import { ChevronDown, ChevronUp, CheckCircle, XCircle, Loader2, ArrowRight, Code, GitBranch, Repeat, Zap, Lock } from 'lucide-react'
import type { WorkflowNode, StepNodeData, ConditionalNodeData, LoopNodeData, DecisionNodeData, RoutingNodeData } from '../hooks/usePlanToFlow'
import type { PlanStep } from '../../../utils/stepConfigMatching'
import { useGlobalPresetStore } from '../../../stores/useGlobalPresetStore'

interface StepLegendProps {
  plan: { steps: PlanStep[] } | null
  nodes: WorkflowNode[]
  selectedNodeId: string | null
  onStepClick: (nodeId: string) => void
}

/**
 * Step Legend Component
 * Shows a collapsible list of all workflow steps at the bottom-left of the canvas
 * Allows quick navigation to any step by clicking on it
 */
export const StepLegend: React.FC<StepLegendProps> = ({
  plan,
  nodes,
  selectedNodeId,
  onStepClick
}) => {
  const [isCollapsed, setIsCollapsed] = useState(true)

  // Get active preset for code execution mode default
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

  // Type guard to check if node has step data
  const hasStepData = (node: WorkflowNode): node is WorkflowNode & { data: StepNodeData | ConditionalNodeData | LoopNodeData | DecisionNodeData | RoutingNodeData } => {
    return (node.type === 'step' || node.type === 'conditional' || node.type === 'loop' || node.type === 'decision' || node.type === 'routing') &&
           'step' in node.data &&
           typeof (node.data as StepNodeData | ConditionalNodeData | LoopNodeData | DecisionNodeData | RoutingNodeData).step === 'object'
  }

  // Build a flat list of all steps including branch steps
  // Includes: regular steps, conditional steps, loop steps, and branch steps (if_true_steps, if_false_steps)
  // Excludes: validation nodes, learning nodes
  const allSteps = useMemo(() => {
    if (!plan?.steps || !nodes) return []

    // Create a map of node IDs to nodes for quick lookup
    const nodeMapByNodeId = new Map<string, WorkflowNode>()
    const nodeMapByStepId = new Map<string, WorkflowNode>()
    
    nodes.forEach(node => {
      if (hasStepData(node)) {
        nodeMapByNodeId.set(node.id, node)
        const stepId = node.data.step?.id || node.id
        nodeMapByStepId.set(stepId, node)
      }
    })

    // Recursively process steps to include branch steps
    const processStepsRecursive = (
      steps: PlanStep[],
      parentIndex: number = 0,
      parentId?: string,
      branchType?: 'true' | 'false',
      depth: number = 0
    ): Array<{
      step: PlanStep
      stepIndex: number
      node: WorkflowNode | undefined
      nodeId: string
      branchType?: 'true' | 'false'
      depth: number
      parentStepIndex?: number
    }> => {
      const result: Array<{
        step: PlanStep
        stepIndex: number
        node: WorkflowNode | undefined
        nodeId: string
        branchType?: 'true' | 'false'
        depth: number
        parentStepIndex?: number
      }> = []

      steps.forEach((step, index) => {
        // Calculate the node ID - for branch steps it's always constructed, for top-level use step.id or fallback
        const calculatedNodeId = parentId 
          ? `${parentId}-${branchType}-${index}`  // Branch steps: always constructed from parent
          : (step.id || `step-${index}`)          // Top-level: use step.id or fallback
        
        // For stepId lookup, use step.id if it exists (for both top-level and branch steps)
        const stepId = step.id

        // Try to find the node by node ID first (this works for both top-level and branch steps)
        let node: WorkflowNode | undefined = nodeMapByNodeId.get(calculatedNodeId)
        // Fallback: try by step ID if node ID lookup fails (for top-level steps)
        if (!node && stepId) {
          node = nodeMapByStepId.get(stepId)
        }
        
        // For branch steps, also try searching by pattern (in case parentId format differs)
        if (!node && parentId && branchType) {
          // Search for nodes with matching pattern: *-branchType-index
          const searchPattern = `-${branchType}-${index}`
          for (const [id, n] of nodeMapByNodeId.entries()) {
            if (id.endsWith(searchPattern) && hasStepData(n)) {
              // Verify it starts with the parent ID (might have different format)
              if (id.includes(parentId) || id.startsWith(`step-`) && id.includes(`-${branchType}-${index}`)) {
                node = n
                break
              }
            }
          }
        }

        // Include step if it has a valid node with step data, OR if it's a branch step (we want to show all branch steps)
        const shouldInclude = node && hasStepData(node)
        const isBranchStep = !!branchType
        
        if (shouldInclude || isBranchStep) {
          // Use the actual node ID if found, otherwise use calculated node ID
          const finalNodeId = node?.id || calculatedNodeId
          
          result.push({
            step,
            stepIndex: parentIndex + index,
            node: node && hasStepData(node) ? node : undefined,
            nodeId: finalNodeId,
            branchType,
            depth,
            parentStepIndex: parentId ? parentIndex : undefined
          })

          // Process branch steps if this is a conditional step
          // Use the actual node ID if found, otherwise use calculated node ID
          if (step.has_condition) {
            const conditionalNodeId = node?.id || finalNodeId
            
            // Process if_true_steps
            if (step.if_true_steps && step.if_true_steps.length > 0) {
              const trueBranchSteps = processStepsRecursive(
                step.if_true_steps,
                parentIndex + index,
                conditionalNodeId,
                'true',
                depth + 1
              )
              result.push(...trueBranchSteps)
            }

            // Process if_false_steps
            if (step.if_false_steps && step.if_false_steps.length > 0) {
              const falseBranchSteps = processStepsRecursive(
                step.if_false_steps,
                parentIndex + index,
                conditionalNodeId,
                'false',
                depth + 1
              )
              result.push(...falseBranchSteps)
            }
          }
        }
      })

      return result
    }

    return processStepsRecursive(plan.steps)
  }, [plan, nodes])

  const handleStepClick = useCallback((nodeId: string) => {
    onStepClick(nodeId)
    setIsCollapsed(true) // Minimize after navigating to step
  }, [onStepClick])

  if (!plan || allSteps.length === 0) {
    return null
  }

  const getStatusIcon = (node: WorkflowNode) => {
    if (!hasStepData(node)) {
      return <ArrowRight className="w-3 h-3 text-muted-foreground" />
    }
    
    const status = node.data.status || 'pending'
    switch (status) {
      case 'running':
        return <Loader2 className="w-3 h-3 text-blue-500 animate-spin" />
      case 'completed':
        return <CheckCircle className="w-3 h-3 text-green-500" />
      case 'failed':
        return <XCircle className="w-3 h-3 text-red-500" />
      default:
        return <ArrowRight className="w-3 h-3 text-muted-foreground" />
    }
  }

  return (
    <div className="absolute bottom-4 left-4 z-10 w-64">
      <div className="bg-background/98 dark:bg-gray-900/98 backdrop-blur-md rounded-lg border border-border shadow-xl">
        {/* Header */}
        <button
          onClick={() => setIsCollapsed(!isCollapsed)}
          className="w-full px-3 py-1.5 flex items-center justify-between text-xs font-semibold text-foreground hover:bg-muted/50 rounded-t-lg transition-colors border-b border-border"
        >
          <span className="flex items-center gap-1.5">
            <span className="w-1.5 h-1.5 rounded-full bg-primary"></span>
            Steps ({allSteps.length})
          </span>
          {isCollapsed ? (
            <ChevronUp className="w-3.5 h-3.5 text-muted-foreground" />
          ) : (
            <ChevronDown className="w-3.5 h-3.5 text-muted-foreground" />
          )}
        </button>

        {/* Step List */}
        {!isCollapsed && (
          <div className="max-h-[280px] overflow-y-auto [&::-webkit-scrollbar]:w-1.5 [&::-webkit-scrollbar-track]:bg-transparent [&::-webkit-scrollbar-thumb]:bg-gray-300 [&::-webkit-scrollbar-thumb]:dark:bg-gray-600 [&::-webkit-scrollbar-thumb]:rounded-full">
            {allSteps.map(({ step, stepIndex, node, nodeId, branchType, depth }) => {
              // For branch steps, show them even if node lookup failed (they're still valid steps)
              // For non-branch steps, require a valid node
              if (!node && !branchType) return null
              
              const isSelected = selectedNodeId === nodeId
              const statusIcon = node ? getStatusIcon(node) : <ArrowRight className="w-3 h-3 text-muted-foreground" />
              const nodeType = node?.type

              // Determine if code execution mode is enabled for this step
              // Priority: step config > preset default (matching backend logic)
              const stepCodeExecSetting = step.agent_configs?.use_code_execution_mode
              const useCodeExecutionMode = stepCodeExecSetting !== undefined 
                ? stepCodeExecSetting === true  // Step has explicit setting
                : presetUseCodeExecutionMode     // Fall back to preset default

              // Check if learnings are locked (same logic as StepNode)
              const lockLearnings = step.agent_configs?.lock_learnings === true && step.agent_configs?.disable_learning !== true

              // Calculate indentation for branch steps (1rem = 16px, so 0.75rem = 12px per level)
              const indentRem = depth * 0.75 // 0.75rem (12px) per depth level

              return (
                <button
                  key={nodeId}
                  onClick={() => node && handleStepClick(nodeId)}
                  disabled={!node}
                  className={`w-full pr-2.5 text-left hover:bg-muted/50 transition-colors border-b border-border/50 last:border-b-0 ${
                    isSelected 
                      ? 'bg-primary/10 dark:bg-primary/20 border-l-2 border-l-primary' 
                      : ''
                  } ${branchType ? 'bg-muted/20 dark:bg-muted/10' : ''}`}
                  style={{ 
                    paddingLeft: `${0.625 + indentRem}rem`,
                    paddingTop: '0.75rem',
                    paddingBottom: '0.75rem',
                    minHeight: '2.5rem'
                  }}
                >
                  <div className="flex items-center gap-2">
                    {/* Step Number or Branch Indicator */}
                    {branchType ? (
                      <div className={`flex-shrink-0 w-4 h-4 rounded-full flex items-center justify-center text-[9px] font-semibold ${
                        branchType === 'true'
                          ? 'bg-green-500/20 dark:bg-green-500/30 text-green-700 dark:text-green-400'
                          : 'bg-red-500/20 dark:bg-red-500/30 text-red-700 dark:text-red-400'
                      }`}>
                        {branchType === 'true' ? 'Y' : 'N'}
                      </div>
                    ) : (
                      <div className={`flex-shrink-0 w-5 h-5 rounded flex items-center justify-center text-[10px] font-bold ${
                        isSelected
                          ? 'bg-primary text-primary-foreground'
                          : 'bg-muted text-muted-foreground'
                      }`}>
                        {stepIndex + 1}
                      </div>
                    )}

                    {/* Step Title */}
                    <div className="flex-1 min-w-0">
                      <div className={`flex items-center gap-1.5 text-[11px] leading-normal ${
                        isSelected 
                          ? 'font-semibold text-foreground' 
                          : 'font-medium text-foreground/90'
                      }`}>
                        {/* Conditional, Loop, Decision, or Routing Icon */}
                        {nodeType === 'conditional' && (
                          <GitBranch className="w-3 h-3 text-purple-500 flex-shrink-0" />
                        )}
                        {nodeType === 'loop' && (
                          <Repeat className="w-3 h-3 text-cyan-500 flex-shrink-0" />
                        )}
                        {nodeType === 'decision' && (
                          <Zap className="w-3 h-3 text-indigo-500 flex-shrink-0" />
                        )}
                        {nodeType === 'routing' && (
                          <GitBranch className="w-3 h-3 text-teal-500 flex-shrink-0" />
                        )}
                        <span>{step.title || `Step ${stepIndex + 1}`}</span>
                        {useCodeExecutionMode && (
                          <Code className="w-3 h-3 text-blue-500 flex-shrink-0" />
                        )}
                        {lockLearnings && (
                          <span title="Learnings are locked" className="flex-shrink-0">
                            <Lock className="w-3 h-3 text-purple-500" />
                          </span>
                        )}
                      </div>
                    </div>

                    {/* Status Icon */}
                    <div className="flex-shrink-0">
                      {statusIcon}
                    </div>
                  </div>
                </button>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}

