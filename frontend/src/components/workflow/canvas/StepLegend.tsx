import React, { useMemo, useState, useCallback, useEffect } from 'react'
import { ChevronDown, ChevronUp, CheckCircle, XCircle, Loader2, ArrowRight, Code, GitBranch, Lock, Route } from 'lucide-react'
import type { WorkflowNode, StepNodeData, ConditionalNodeData, RoutingStepNodeData } from '../hooks/usePlanToFlow'
import type { PlanStep } from '../../../utils/stepConfigMatching'
import { isConditionalStep } from '../../../utils/stepConfigMatching'
import { useActiveWorkflowPreset } from '../../../hooks/useActiveWorkflowPreset'
import { agentApi } from '../../../services/api'
import { WorkflowLegend } from './WorkflowLegend'

interface StepLegendProps {
  plan: { steps: PlanStep[] } | null
  nodes: WorkflowNode[]
  selectedNodeId: string | null
  onStepClick: (nodeId: string) => void
  workspacePath?: string | null
  currentStepId?: string | null  // Currently running step ID from workflow store
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
  onStepClick,
  workspacePath,
  currentStepId
}) => {
  const [isCollapsed, setIsCollapsed] = useState(true)
  // Cache for learnings existence checks: stepId -> boolean | null (null = checking/unknown)
  const [learningsExistCache, setLearningsExistCache] = useState<Map<string, boolean | null>>(new Map())

  // Get active preset for code execution mode default
  const activePreset = useActiveWorkflowPreset()
  
  const presetUseCodeExecutionMode = activePreset?.useCodeExecutionMode ?? false

  // Type guard to check if node has step data
  const hasStepData = (node: WorkflowNode): node is WorkflowNode & { data: StepNodeData | ConditionalNodeData | RoutingStepNodeData } => {
    return (node.type === 'step' || node.type === 'conditional' || node.type === 'routing') &&
           'step' in node.data &&
           typeof (node.data as StepNodeData | ConditionalNodeData | RoutingStepNodeData).step === 'object'
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
          if (isConditionalStep(step)) {
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

  // Generate a stable key for allSteps to prevent infinite loops in useEffect
  // The allSteps array is recreated on every render because it depends on 'nodes' from parent
  const allStepsKey = useMemo(() => {
    return allSteps.map(s => s.step.id).join(',')
  }, [allSteps])

  // Check learnings existence for all steps when workspacePath or step list changes
  useEffect(() => {
    if (!workspacePath || allSteps.length === 0) {
      setLearningsExistCache(new Map())
      return
    }

    // Collect unique step IDs that need checking
    const stepIdsToCheck = new Set<string>()
    allSteps.forEach(({ step }) => {
      const stepId = step.id
      if (stepId && !learningsExistCache.has(stepId)) {
        stepIdsToCheck.add(stepId)
      }
    })

    if (stepIdsToCheck.size === 0) return

    // Mark as checking
    setLearningsExistCache(prev => {
      const newCache = new Map(prev)
      stepIdsToCheck.forEach(id => newCache.set(id, null))
      return newCache
    })

    // Check learnings for all steps in parallel
    const checkPromises = Array.from(stepIdsToCheck).map(async (stepId) => {
      try {
        const learningsPath = `${workspacePath}/learnings/${stepId}`
        const files = await agentApi.getPlannerFiles(learningsPath, 100)
        
        // Check if there are any learning files (exclude .learning_metadata.json)
        const hasLearningFiles = files && Array.isArray(files) && files.some((file: { filepath?: string; name?: string }) => {
          const fileName = file.filepath || file.name || ''
          return fileName.endsWith('.md') || (fileName.startsWith('code/') && /\.(go|py|sh|js|ts|jsx|tsx|bash|rb|java|rs|c|cpp|json|yaml|yml)$/.test(fileName))
        })
        
        return { stepId, exists: hasLearningFiles }
      } catch (error) {
        // If folder doesn't exist or error, assume no learnings
        console.debug('[StepLegend] Failed to check learnings for step:', stepId, error)
        return { stepId, exists: false }
      }
    })

    // Update cache when all checks complete
    Promise.all(checkPromises).then(results => {
      setLearningsExistCache(prev => {
        const newCache = new Map(prev)
        results.forEach(({ stepId, exists }) => {
          newCache.set(stepId, exists)
        })
        return newCache
      })
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [workspacePath, allStepsKey]) // Depend on stable key instead of allSteps object

  const handleStepClick = useCallback((nodeId: string) => {
    onStepClick(nodeId)
    setIsCollapsed(true) // Minimize after navigating to step
  }, [onStepClick])

  // Find the currently running step name to display in collapsed header
  const runningStepInfo = useMemo(() => {
    if (!currentStepId || allSteps.length === 0) return null

    // Find step by matching step.id
    const runningStep = allSteps.find(({ step, node }) => {
      // Match by step.id
      if (step.id === currentStepId) return true
      // Match by node.data.step.id
      if (node && hasStepData(node)) {
        const nodeStepId = (node.data as StepNodeData | ConditionalNodeData).step?.id
        if (nodeStepId === currentStepId) return true
      }
      return false
    })

    if (!runningStep) return null

    const { step, stepIndex } = runningStep
    const title = step.title || `Step ${stepIndex + 1}`

    return { title, stepIndex }
  }, [currentStepId, allSteps])

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
    <div className={`absolute bottom-4 left-4 z-10 transition-all duration-200 ${isCollapsed ? 'w-44' : 'w-72'}`}>
      <div className="bg-background/98 dark:bg-gray-900/98 backdrop-blur-md rounded-lg border border-border shadow-xl">
        {/* Header */}
        <div className="w-full flex items-center justify-between rounded-t-lg border-b border-border transition-colors">
          <button
            onClick={() => setIsCollapsed(!isCollapsed)}
            className="flex-1 px-3 py-1.5 flex items-center gap-1.5 min-w-0 text-xs font-semibold text-foreground hover:bg-muted/50 transition-colors text-left rounded-tl-lg"
          >
            <span className="flex items-center gap-1.5 min-w-0 flex-1">
              {runningStepInfo ? (
                <>
                  <Loader2 className="w-3.5 h-3.5 text-blue-500 animate-spin flex-shrink-0" />
                  <span className="truncate" title={runningStepInfo.title}>
                    {runningStepInfo.title}
                  </span>
                </>
              ) : (
                <>
                  <span className="w-1.5 h-1.5 rounded-full bg-primary flex-shrink-0"></span>
                  <span>Steps ({allSteps.length})</span>
                </>
              )}
            </span>
          </button>
          
          <div className="flex items-center gap-1 pr-2 py-1.5">
            <div onClick={(e) => e.stopPropagation()}>
              <WorkflowLegend />
            </div>
            <button
              onClick={() => setIsCollapsed(!isCollapsed)}
              className="p-0.5 hover:bg-muted rounded text-muted-foreground transition-colors"
            >
              {isCollapsed ? (
                <ChevronUp className="w-3.5 h-3.5 flex-shrink-0" />
              ) : (
                <ChevronDown className="w-3.5 h-3.5 flex-shrink-0" />
              )}
            </button>
          </div>
        </div>

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

              // Check if learnings are locked (matching StepNode logic - show icon if lock_learnings is true)
              const stepConfigs = step.agent_configs

              // Check lock_learnings in step config
              const lockLearningsConfig = stepConfigs?.lock_learnings

              // Check disable_learning in step config
              const isLearningDisabled = stepConfigs?.disable_learning === true

              // Show lock icon if lock_learnings is true and learning is not disabled (matching StepNode behavior)
              const lockLearnings = lockLearningsConfig === true && !isLearningDisabled

              // Check if this is a sub-agent (node ID contains '-sub-agent-')
              const isSubAgent = nodeId.includes('-sub-agent-')
              
              // Calculate indentation for branch steps and sub-agents (1rem = 16px, so 0.75rem = 12px per level)
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
                    {/* Step Number, Branch Indicator, or Sub-Agent Indicator */}
                    {branchType ? (
                      <div className={`flex-shrink-0 w-4 h-4 rounded-full flex items-center justify-center text-[9px] font-semibold ${
                        branchType === 'true'
                          ? 'bg-green-500/20 dark:bg-green-500/30 text-green-700 dark:text-green-400'
                          : 'bg-red-500/20 dark:bg-red-500/30 text-red-700 dark:text-red-400'
                      }`}>
                        {branchType === 'true' ? 'Y' : 'N'}
                      </div>
                    ) : isSubAgent ? (
                      <div className={`flex-shrink-0 w-4 h-4 rounded-full flex items-center justify-center text-[9px] font-semibold bg-indigo-500/20 dark:bg-indigo-500/30 text-indigo-700 dark:text-indigo-400`}>
                        S
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
                        {/* Conditional or Routing Icon */}
                        {nodeType === 'conditional' && (
                          <GitBranch className="w-3 h-3 text-purple-500 flex-shrink-0" />
                        )}
                        {nodeType === 'routing' && (
                          <Route className="w-3 h-3 text-teal-500 flex-shrink-0" />
                        )}
                        <span>
                          {step.title || `Step ${stepIndex + 1}`}
                        </span>
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

