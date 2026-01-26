import React, { useMemo, useState, useCallback, useEffect } from 'react'
import { ChevronDown, ChevronUp, CheckCircle, XCircle, Loader2, ArrowRight, Code, GitBranch, Repeat, Zap, Lock, SkipForward, ShieldCheck, Search } from 'lucide-react'
import type { WorkflowNode, StepNodeData, ConditionalNodeData, LoopNodeData, DecisionNodeData, OrchestratorNodeData } from '../hooks/usePlanToFlow'
import type { PlanStep } from '../../../utils/stepConfigMatching'
import { isConditionalStep, isOrchestrationStep } from '../../../utils/stepConfigMatching'
import { useGlobalPresetStore } from '../../../stores/useGlobalPresetStore'
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
  const presetUseToolSearchMode = activePreset?.useToolSearchMode ?? false

  // Type guard to check if node has step data
  const hasStepData = (node: WorkflowNode): node is WorkflowNode & { data: StepNodeData | ConditionalNodeData | LoopNodeData | DecisionNodeData | OrchestratorNodeData } => {
    return (node.type === 'step' || node.type === 'conditional' || node.type === 'loop' || node.type === 'decision' || node.type === 'orchestrator') &&
           'step' in node.data &&
           typeof (node.data as StepNodeData | ConditionalNodeData | LoopNodeData | DecisionNodeData | OrchestratorNodeData).step === 'object'
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

    const mainSteps = processStepsRecursive(plan.steps)
    
    // Add sub-agents from orchestrator nodes
    const subAgentSteps: Array<{
      step: PlanStep
      stepIndex: number
      node: WorkflowNode | undefined
      nodeId: string
      branchType?: 'true' | 'false'
      depth: number
      parentStepIndex?: number
    }> = []
    
    // Find all orchestrator nodes and their sub-agents
    nodes.forEach(node => {
      if (node.type === 'orchestrator' && hasStepData(node)) {
        const orchestratorData = node.data as OrchestratorNodeData
        const orchestratorStep = orchestratorData.step
        const orchestratorStepIndex = orchestratorData.stepIndex
        
        // Find sub-agent nodes that belong to this orchestrator node
        if (isOrchestrationStep(orchestratorStep) && orchestratorStep.orchestration_routes) {
          orchestratorStep.orchestration_routes.forEach((route, routeIndex) => {
            if (route.sub_agent_step) {
              // Find the sub-agent node by ID pattern
              const subAgentNodeId = `${node.id}-sub-agent-${route.route_id || route.sub_agent_step.id || routeIndex}`
              const subAgentNode = nodeMapByNodeId.get(subAgentNodeId)
              
              if (subAgentNode && hasStepData(subAgentNode)) {
                subAgentSteps.push({
                  step: route.sub_agent_step,
                  stepIndex: orchestratorStepIndex, // Use parent's step index
                  node: subAgentNode,
                  nodeId: subAgentNodeId,
                  depth: 1, // Indent sub-agents one level
                  parentStepIndex: orchestratorStepIndex
                })
              }
            }
          })
        }
      }
    })
    
    // Combine main steps and sub-agents, inserting sub-agents after their parent orchestrator step
    const result: Array<{
      step: PlanStep
      stepIndex: number
      node: WorkflowNode | undefined
      nodeId: string
      branchType?: 'true' | 'false'
      depth: number
      parentStepIndex?: number
    }> = []
    
    mainSteps.forEach(mainStep => {
      result.push(mainStep)
      
      // If this is an orchestrator step, add its sub-agents right after it
      if (mainStep.node?.type === 'orchestrator') {
        const orchestratorSubAgents = subAgentSteps.filter(sa => sa.parentStepIndex === mainStep.stepIndex)
        result.push(...orchestratorSubAgents)
      }
    })
    
    return result
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
      const stepId = (isOrchestrationStep(step) && step.orchestration_step?.id) ?? step.id
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
          return fileName.endsWith('.md') || (fileName.startsWith('code/') && fileName.endsWith('.go'))
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
      // For orchestration steps, also check orchestration_step.id
      if (isOrchestrationStep(step) && step.orchestration_step?.id === currentStepId) return true
      // Match by node.data.step.id
      if (node && hasStepData(node)) {
        const nodeStepId = (node.data as StepNodeData | ConditionalNodeData | LoopNodeData | DecisionNodeData | OrchestratorNodeData).step?.id
        if (nodeStepId === currentStepId) return true
      }
      return false
    })

    if (!runningStep) return null

    const { step, stepIndex } = runningStep
    const title = isOrchestrationStep(step) && step.orchestration_step?.title
      ? step.orchestration_step.title
      : step.title || `Step ${stepIndex + 1}`

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
    <div className="absolute bottom-4 left-4 z-10 w-72">
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

              // Determine if tool search mode is enabled for this step
              // Priority: step config > preset default (matching backend logic)
              const stepToolSearchSetting = step.agent_configs?.use_tool_search_mode
              const useToolSearchMode = stepToolSearchSetting !== undefined 
                ? stepToolSearchSetting === true  // Step has explicit setting
                : presetUseToolSearchMode         // Fall back to preset default

              // Check if learnings are locked (matching StepNode logic - show icon if lock_learnings is true)
              // For orchestration steps, check orchestration_step.agent_configs
              // For regular steps, check step.agent_configs
              const orchestrationStepConfigs = isOrchestrationStep(step) ? step.orchestration_step?.agent_configs : undefined
              const stepConfigs = step.agent_configs
              
              // Check lock_learnings in the appropriate config based on step type
              const lockLearningsConfig = isOrchestrationStep(step)
                ? orchestrationStepConfigs?.lock_learnings
                : stepConfigs?.lock_learnings
              
              // Check disable_learning in the appropriate config
              const isLearningDisabled = isOrchestrationStep(step)
                ? (orchestrationStepConfigs?.disable_learning ?? stepConfigs?.disable_learning) === true
                : stepConfigs?.disable_learning === true
              
              // Show lock icon if lock_learnings is true and learning is not disabled (matching StepNode behavior)
              const lockLearnings = lockLearningsConfig === true && !isLearningDisabled

              // Check validation skipped (llm_validation_mode === 'skip')
              const skipLLMValidation = isOrchestrationStep(step)
                ? orchestrationStepConfigs?.llm_validation_mode === 'skip'
                : stepConfigs?.llm_validation_mode === 'skip'
              
              // Check if LLM validation is disabled (disabled by default, only enabled when false)
              const isValidationDisabled = isOrchestrationStep(step)
                ? orchestrationStepConfigs?.disable_validation !== false
                : stepConfigs?.disable_validation !== false

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
                      <div className={`flex-shrink-0 w-4 h-4 rounded-full flex items-center justify-center text-[9px] font-semibold bg-cyan-500/20 dark:bg-cyan-500/30 text-cyan-700 dark:text-cyan-400`}>
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
                        {nodeType === 'orchestrator' && (
                          <GitBranch className="w-3 h-3 text-teal-500 flex-shrink-0" />
                        )}
                        <span>
                          {nodeType === 'orchestrator' && isOrchestrationStep(step) && step.orchestration_step?.title
                            ? step.orchestration_step.title
                            : step.title || `Step ${stepIndex + 1}`
                          }
                        </span>
                        {useCodeExecutionMode && (
                          <Code className="w-3 h-3 text-blue-500 flex-shrink-0" />
                        )}
                        {!useCodeExecutionMode && useToolSearchMode && (
                          <Search className="w-3 h-3 text-yellow-500 flex-shrink-0" />
                        )}
                        {lockLearnings && (
                          <span title="Learnings are locked" className="flex-shrink-0">
                            <Lock className="w-3 h-3 text-purple-500" />
                          </span>
                        )}
                        {skipLLMValidation && (
                          <span title="LLM validation will be skipped if pre-validation passes" className="flex-shrink-0">
                            <SkipForward className="w-3 h-3 text-cyan-500" />
                          </span>
                        )}
                        {isValidationDisabled && (
                          <span title="Validation disabled - only pre-validation runs" className="flex-shrink-0">
                            <ShieldCheck className="w-3 h-3 text-orange-500" />
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

