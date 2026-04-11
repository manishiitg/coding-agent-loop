import { memo, useCallback, useMemo, useState, useEffect, type ReactElement, type MouseEvent } from 'react'
import { Handle, Position } from '@xyflow/react'
import { XCircle, Loader2, Plus, RefreshCw, GitBranch, Play, Settings, Code, Terminal, Lock, CheckCircle } from 'lucide-react'
import { useActiveWorkflowPreset } from '../../../hooks/useActiveWorkflowPreset'
import { useLLMStore } from '../../../stores/useLLMStore'
import { useWorkflowStore } from '../../../stores/useWorkflowStore'
import { useCapabilitiesStore } from '../../../stores/useCapabilitiesStore'
import { agentApi } from '../../../services/api'
import { getToolsByCategory } from '../../../utils/customToolNames'
import { NodeConfigFooter } from './NodeConfigFooter'

import type { ConditionalNodeData } from '../hooks/usePlanToFlow'
import type { ChangeType } from '../hooks/usePlanData'
import type { ConditionalPlanStep } from '../../../utils/stepConfigMatching'

// Helper to parse tool entry (format: "category:tool" or "category:*")
const parseToolEntry = (entry: string): { category: string; tool: string } | null => {
  const parts = entry.split(':')
  if (parts.length !== 2) return null
  return { category: parts[0], tool: parts[1] }
}

// Helper to check if a category is fully enabled (all tools via "*")
const isCategoryEnabled = (category: string, enabledTools: string[]): boolean => {
  if (enabledTools.length === 0) return true // Default: all enabled
  return enabledTools.includes(`${category}:*`)
}

// Get tool count for a category
const getCategoryToolCount = (category: string, enabledTools: string[], allCategoryTools: string[]): { enabled: number; total: number } => {
  if (enabledTools.length === 0 || isCategoryEnabled(category, enabledTools)) {
    return { enabled: allCategoryTools.length, total: allCategoryTools.length }
  }
  // Count specific tools enabled
  const enabled = enabledTools.filter(entry => {
    const parsed = parseToolEntry(entry)
    // Only count if it matches category AND is in the available tools list
    return parsed && parsed.category === category && parsed.tool !== '*' && allCategoryTools.includes(parsed.tool)
  }).length
  return { enabled, total: allCategoryTools.length }
}

interface ConditionalNodeProps {
  data: ConditionalNodeData
  selected?: boolean
}

const statusBorderColors: Record<string, string> = {
  pending: 'border-purple-400 dark:border-purple-500',
  evaluating: 'border-purple-500 dark:border-purple-400',
  decided_true: 'border-green-500 dark:border-green-400',
  decided_false: 'border-red-500 dark:border-red-400',
  completed: 'border-green-500 dark:border-green-400'
}

const changeHighlightStyles: Record<ChangeType, string> = {
  added: 'ring-2 ring-emerald-500/60 shadow-emerald-500/20',
  updated: 'ring-2 ring-blue-500/60 shadow-blue-500/20',
  deleted: 'ring-2 ring-red-500/60 shadow-red-500/20 opacity-50'
}

const changeBadgeStyles: Record<ChangeType, { bg: string; icon: ReactElement }> = {
  added: { bg: 'bg-emerald-500', icon: <Plus className="w-3 h-3" /> },
  updated: { bg: 'bg-blue-500', icon: <RefreshCw className="w-3 h-3" /> },
  deleted: { bg: 'bg-red-500', icon: <XCircle className="w-3 h-3" /> }
}

const statusIcons: Record<string, ReactElement | null> = {
  pending: null,
  evaluating: <Loader2 className="w-4 h-4 text-purple-500 animate-spin" />,
  decided_true: <CheckCircle className="w-4 h-4 text-green-500" />,
  decided_false: <XCircle className="w-4 h-4 text-red-500" />,
  completed: <CheckCircle className="w-4 h-4 text-green-500" />
}

export const ConditionalNode = memo(({ data, selected }: ConditionalNodeProps) => {
  const { id, title, condition_question, status, stepIndex, changeType, step, onRunFromStep, onOpenSidebar, isExecuting, workspacePath, isOrphan } = data

  // Process text to convert escaped newlines to actual newlines
  const processText = (text: string | undefined): string | undefined => {
    if (!text) return undefined
    return text
      .replace(/\\n/g, '\n')  // Convert \n to actual newlines
      .replace(/\\t/g, '\t')  // Convert \t to actual tabs
      .replace(/\\r/g, '\r')  // Convert \r to actual carriage returns
  }

  const processedConditionQuestion = processText(condition_question)

  // Get preset for config badges
  const activePreset = useActiveWorkflowPreset()

  const { availableLLMs } = useLLMStore()
  const { capabilities } = useCapabilitiesStore()
  const stepOverride = useWorkflowStore(state => state.stepOverride)

  // Get step config (agent_configs)
  const stepConfig = step as { agent_configs?: { 
    use_code_execution_mode?: boolean
    conditional_llm?: { provider?: string; model_id?: string }
    execution_llm?: { provider?: string; model_id?: string }
    learning_llm?: { provider?: string; model_id?: string }
    disable_learning?: boolean
    lock_learnings?: boolean
    learning_detail_level?: string
    execution_max_turns?: number
    selected_servers?: string[]
    selected_tools?: string[]
    enabled_custom_tools?: string[]
    enable_context_offloading?: boolean
  } }

  // Determine code execution mode: override > step config > preset default
  const presetUseCodeExecutionMode = activePreset?.useCodeExecutionMode ?? false
  const overrideCodeExec = stepOverride?.use_code_execution_mode
  const stepCodeExecSetting = stepConfig?.agent_configs?.use_code_execution_mode
  const useCodeExecutionMode = overrideCodeExec !== undefined
    ? overrideCodeExec === true
    : stepCodeExecSetting !== undefined
      ? stepCodeExecSetting === true
      : presetUseCodeExecutionMode

  // Execution LLM: global override > step config > preset default
  const executionLLM = useMemo(() => {
    const presetLLMConfig = activePreset?.llmConfig
    const overrideLLMConfig = stepOverride?.execution_llm
    const stepLLMConfig = stepConfig?.agent_configs?.execution_llm
    const presetDefaultLLM = presetLLMConfig?.provider && presetLLMConfig?.model_id
      ? { provider: presetLLMConfig.provider, model_id: presetLLMConfig.model_id } : null

    const llmConfig = overrideLLMConfig || stepLLMConfig || presetDefaultLLM
    if (!llmConfig?.provider || !llmConfig?.model_id) return null

    const llm = availableLLMs?.find(l => l.provider === llmConfig.provider && l.model === llmConfig.model_id)
    return llm?.label || `${llmConfig.provider} ${llmConfig.model_id.split('-').slice(0, 2).join('-')}`
  }, [stepOverride?.execution_llm, stepConfig?.agent_configs?.execution_llm, activePreset?.llmConfig, availableLLMs])

  // Conditional LLM: step config > execution LLM > preset default
  const conditionalLLM = useMemo(() => {
    const presetLLMConfig = activePreset?.llmConfig
    const stepConditionalLLM = stepConfig?.agent_configs?.conditional_llm
    const stepExecutionLLM = stepConfig?.agent_configs?.execution_llm
    const presetDefaultLLM = presetLLMConfig?.provider && presetLLMConfig?.model_id 
      ? { provider: presetLLMConfig.provider, model_id: presetLLMConfig.model_id } : null
    
    // Priority: step conditional_llm > step execution_llm > preset default
    const llmConfig = stepConditionalLLM || stepExecutionLLM || presetDefaultLLM
    if (!llmConfig?.provider || !llmConfig?.model_id) return null
    
    const llm = availableLLMs?.find(l => l.provider === llmConfig.provider && l.model === llmConfig.model_id)
    return llm?.label || `${llmConfig.provider} ${llmConfig.model_id.split('-').slice(0, 2).join('-')}`
  }, [stepConfig?.agent_configs?.conditional_llm, stepConfig?.agent_configs?.execution_llm, activePreset?.llmConfig, availableLLMs])

  // Learning disabled: override > step config
  const learningDisabled = useMemo(() => {
    if (stepOverride?.disable_learning !== undefined) return stepOverride.disable_learning === true
    return stepConfig?.agent_configs?.disable_learning === true
  }, [stepOverride?.disable_learning, stepConfig?.agent_configs?.disable_learning])

  // Learning LLM: override > step config > preset learning_llm > preset default
  const learningLLM = useMemo(() => {
    if (learningDisabled) return null

    const presetLLMConfig = activePreset?.llmConfig
    const overrideLLMConfig = stepOverride?.learning_llm
    const stepLLMConfig = stepConfig?.agent_configs?.learning_llm
    const presetLearningLLM = presetLLMConfig?.learning_llm
    const presetDefaultLLM = presetLLMConfig?.provider && presetLLMConfig?.model_id
      ? { provider: presetLLMConfig.provider, model_id: presetLLMConfig.model_id } : null

    const llmConfig = overrideLLMConfig || stepLLMConfig || presetLearningLLM || presetDefaultLLM
    if (!llmConfig?.provider || !llmConfig?.model_id) return null

    const llm = availableLLMs?.find(l => l.provider === llmConfig.provider && l.model === llmConfig.model_id)
    return llm?.label || `${llmConfig.provider} ${llmConfig.model_id.split('-').slice(0, 2).join('-')}`
  }, [learningDisabled, stepOverride?.learning_llm, stepConfig?.agent_configs?.learning_llm, activePreset?.llmConfig, availableLLMs])


  // Check if learnings exist in backend (for conditional steps, use step.ID)
  const [learningsExist, setLearningsExist] = useState<boolean | null>(null) // null = checking, true/false = result
  const stepIdForLearnings = step?.id

  useEffect(() => {
    // Only check if we have workspace path and step ID
    if (!workspacePath || !stepIdForLearnings) {
      setLearningsExist(false)
      return
    }

    // Check if learnings folder exists and has content
    const checkLearningsExist = async () => {
      try {
        const learningsPath = `${workspacePath}/learnings/${stepIdForLearnings}`
        const files = await agentApi.getPlannerFiles(learningsPath, 100)
        
        // Check if there are any learning files (exclude .learning_metadata.json)
        const hasLearningFiles = files && Array.isArray(files) && files.some((file: { filepath?: string; name?: string }) => {
          const fileName = file.filepath || file.name || ''
          return fileName.endsWith('.md') || (fileName.startsWith('code/') && /\.(go|py|sh|js|ts|jsx|tsx|bash|rb|java|rs|c|cpp|json|yaml|yml)$/.test(fileName))
        })
        
        setLearningsExist(hasLearningFiles)
      } catch (error) {
        // If folder doesn't exist or error, assume no learnings
        console.debug('[ConditionalNode] Failed to check learnings:', error)
        setLearningsExist(false)
      }
    }

    checkLearningsExist()
  }, [workspacePath, stepIdForLearnings])

  // Lock learnings: override > step config
  const isLockedInConfig = useMemo(() => {
    if (stepOverride?.lock_learnings !== undefined) return stepOverride.lock_learnings === true
    return stepConfig?.agent_configs?.lock_learnings ?? false
  }, [stepOverride?.lock_learnings, stepConfig?.agent_configs?.lock_learnings])

  const lockLearnings = useMemo(() => {
    return isLockedInConfig && (learningsExist === true) && !learningDisabled
  }, [isLockedInConfig, learningsExist, learningDisabled])

  // Execution max turns: override > step config (defaults to 100)
  const executionMaxTurns = useMemo(() => {
    return stepOverride?.execution_max_turns || stepConfig?.agent_configs?.execution_max_turns || 100
  }, [stepOverride?.execution_max_turns, stepConfig?.agent_configs?.execution_max_turns])

  // MCP Servers: override > step config > preset
  const presetServers = useMemo(() => activePreset?.selectedServers || [], [activePreset?.selectedServers])
  const overrideServers = stepOverride?.selected_servers
  const stepServers = stepConfig?.agent_configs?.selected_servers
  const effectiveServers = useMemo(() => {
    if (overrideServers !== undefined && overrideServers !== null) {
      return overrideServers.filter(s => s !== 'NO_SERVERS')
    }
    if (stepServers !== undefined && stepServers !== null) {
      return stepServers.filter(s => s !== 'NO_SERVERS')
    }
    return presetServers
  }, [overrideServers, stepServers, presetServers])

  // Tools: override > step config > preset
  const presetTools = useMemo(() => activePreset?.selectedTools || [], [activePreset?.selectedTools])
  const overrideTools = stepOverride?.selected_tools
  const effectiveTools = useMemo(() => {
    if (effectiveServers.length === 0) {
      return []
    }
    if (overrideTools !== undefined && overrideTools !== null && overrideTools.length > 0) {
      return overrideTools
    }
    return stepConfig?.agent_configs?.selected_tools?.length
      ? stepConfig.agent_configs.selected_tools
      : presetTools
  }, [effectiveServers.length, overrideTools, stepConfig?.agent_configs?.selected_tools, presetTools])

  // Group tools by server and detect "all tools" (*) entries
  const toolsDisplayInfo = useMemo(() => {
    const serverMap = new Map<string, { hasAllTools: boolean; specificTools: number }>()
    
    effectiveTools.forEach(tool => {
      const [server, toolName] = tool.split(':')
      if (!server) return
      
      if (!serverMap.has(server)) {
        serverMap.set(server, { hasAllTools: false, specificTools: 0 })
      }
      
      const info = serverMap.get(server)!
      if (toolName === '*') {
        // If server has "*", it means all tools - reset specific tools count
        info.hasAllTools = true
        info.specificTools = 0
      } else if (!info.hasAllTools) {
        // Only count specific tools if we don't already have "all tools"
        info.specificTools++
      }
    })
    
    return Array.from(serverMap.entries()).map(([server, info]) => ({
      server,
      ...info
    }))
  }, [effectiveTools])

  // Parse custom tools (workspace_tools, human_tools)
  const enabledCustomTools = useMemo(() => stepConfig?.agent_configs?.enabled_custom_tools || [], [stepConfig?.agent_configs?.enabled_custom_tools])
  
  const workspaceToolsInfo = useMemo(() => {
    const allWorkspaceTools = getToolsByCategory('workspace_tools', capabilities?.workspace)
    return getCategoryToolCount('workspace_tools', enabledCustomTools, allWorkspaceTools)
  }, [enabledCustomTools, capabilities?.workspace])
  
  const humanToolsInfo = useMemo(() => {
    const allHumanTools = getToolsByCategory('human_tools', capabilities?.workspace)
    return getCategoryToolCount('human_tools', enabledCustomTools, allHumanTools)
  }, [enabledCustomTools, capabilities?.workspace])

  const hasWorkspaceTools = workspaceToolsInfo.enabled > 0
  const hasHumanTools = humanToolsInfo.enabled > 0
  const hasLargeOutput = (stepOverride?.enable_context_offloading !== undefined
    ? stepOverride.enable_context_offloading
    : stepConfig?.agent_configs?.enable_context_offloading) !== false // Default is enabled

  // Button is disabled if executing or no callback
  const isRunDisabled = isExecuting || !onRunFromStep

  // Handle run from this step button click
  const handleRunClick = useCallback((e: MouseEvent) => {
    e.stopPropagation() // Prevent node selection
    e.preventDefault() // Prevent any default behavior
    console.log('[ConditionalNode] Run button clicked:', { stepIndex, stepId: step.id, onRunFromStep: !!onRunFromStep, isExecuting, isRunDisabled })
    if (onRunFromStep && !isExecuting) {
      console.log('[ConditionalNode] Calling onRunFromStep with:', stepIndex, step.id || `step-${stepIndex}`)
      onRunFromStep(stepIndex, step.id || `step-${stepIndex}`)
    } else {
      console.warn('[ConditionalNode] Cannot run step:', { 
        hasCallback: !!onRunFromStep, 
        isExecuting, 
        isRunDisabled 
      })
    }
  }, [onRunFromStep, isExecuting, stepIndex, step.id, isRunDisabled])

  // Handle settings icon click - opens the sidebar
  const handleSettingsClick = useCallback((e: MouseEvent) => {
    e.stopPropagation() // Prevent node selection
    e.preventDefault() // Prevent any default behavior
    console.log('[ConditionalNode] Settings button clicked:', { nodeId: id, stepIndex, stepId: step.id, onOpenSidebar: !!onOpenSidebar })
    if (onOpenSidebar && typeof onOpenSidebar === 'function') {
      // Use the node's actual ID (data.id) instead of step.id to ensure we find the correct node
      console.log('[ConditionalNode] Calling onOpenSidebar with node ID:', id)
      onOpenSidebar(id)
    } else {
      console.warn('[ConditionalNode] onOpenSidebar callback not available')
    }
  }, [onOpenSidebar, id, stepIndex, step.id])

  return (
    <div className={`relative w-[240px] ${changeType ? changeHighlightStyles[changeType] : ''} ${isOrphan ? 'border-dashed border-2 border-amber-400 dark:border-amber-500 rounded-xl' : ''}`}>
      {/* Header with buttons - above the diamond */}
      <div className="absolute -top-12 left-0 right-0 flex items-center justify-center gap-2 z-20">
        {/* Run from this step button */}
        {onRunFromStep && !isOrphan ? (
          <button
            onClick={handleRunClick}
            disabled={isRunDisabled}
            className={`
              flex items-center justify-center w-7 h-7 rounded-lg transition-all relative z-10
              ${isRunDisabled
                ? 'bg-gray-200 dark:bg-gray-700 text-gray-400 cursor-not-allowed opacity-50'
                : 'bg-green-100 dark:bg-green-900/40 text-green-600 dark:text-green-400 hover:bg-green-200 dark:hover:bg-green-900/60 hover:scale-105 cursor-pointer'
              }
            `}
            title={
              isExecuting
                ? 'Execution in progress...'
                : `Run step ${stepIndex + 1} only`
            }
          >
            <Play className="w-3.5 h-3.5" />
          </button>
        ) : (
          <div className="w-7 h-7 flex items-center justify-center text-xs text-gray-400" title="Run callback not available">
            ⚠️
          </div>
        )}
        {/* Settings icon button - opens sidebar */}
        {onOpenSidebar ? (
          <button
            onClick={handleSettingsClick}
            className="flex items-center justify-center w-7 h-7 rounded-lg transition-all relative z-10 bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400 hover:bg-gray-200 dark:hover:bg-gray-600 hover:scale-105 cursor-pointer"
            title="Open step settings"
          >
            <Settings className="w-3.5 h-3.5" />
          </button>
        ) : null}
        {/* Agent Mode Badge */}
        {useCodeExecutionMode ? (
          <div className="flex items-center justify-center w-7 h-7 rounded-md bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-300 border border-amber-200 dark:border-amber-800" title="Code Execution Mode">
            <Terminal className="w-3.5 h-3.5" />
          </div>
        ) : (
          <div className="flex items-center justify-center w-7 h-7 rounded-md bg-slate-100 dark:bg-slate-800/60 text-slate-700 dark:text-slate-300 border border-slate-200 dark:border-slate-700" title="Simple Agent Mode">
            <Code className="w-3.5 h-3.5" />
          </div>
        )}
        {/* Lock Learnings Badge */}
        {lockLearnings && (
          <div 
            className="flex items-center gap-1 px-2 py-1 rounded-md bg-purple-100 dark:bg-purple-900/40 text-purple-700 dark:text-purple-300 text-[10px] font-semibold border border-purple-200 dark:border-purple-800"
            title="Learnings are locked - learning agent will not run but existing learnings will be used"
          >
            <Lock className="w-3 h-3" />
            <span>Locked</span>
          </div>
        )}
      </div>

      {/* Condition Badge - Top */}
      <div className="absolute -top-2.5 left-1/2 -translate-x-1/2 z-10 flex items-center gap-1.5 px-2.5 py-1 rounded-full bg-purple-600 dark:bg-purple-500 text-white text-[11px] font-semibold shadow-lg">
        <GitBranch className="w-3.5 h-3.5" />
        <span>Condition</span>
      </div>

      {/* Change badge */}
      {/* Change badge - positioned at top-right edge */}
      {changeType && (
        <div className={`absolute top-0 right-0 z-10 flex items-center gap-1 px-1.5 py-0.5 rounded-bl-lg rounded-tr-xl ${changeBadgeStyles[changeType].bg} text-white text-[10px] font-medium shadow-lg`}>
          {changeBadgeStyles[changeType].icon}
          <span className="capitalize">{changeType}</span>
        </div>
      )}

      {/* Diamond Shape Card */}
      <div 
        className={`
          relative rounded-xl border-2 bg-white dark:bg-gray-900 shadow-lg overflow-visible
          ${statusBorderColors[status]}
          ${selected ? 'ring-2 ring-purple-500/60' : ''}
          ${status === 'evaluating' ? 'animate-pulse' : ''}
        `}
        style={{
          clipPath: 'polygon(15% 0%, 85% 0%, 100% 50%, 85% 100%, 15% 100%, 0% 50%)',
          minHeight: '100px'
        }}
      >
        {/* Input handle */}
        <Handle 
          type="target" 
          position={Position.Left} 
          className="!w-3 !h-3 !bg-purple-400 dark:!bg-purple-500 !border-2 !border-white dark:!border-gray-900"
          style={{ left: '-6px' }}
        />

        {/* Content */}
        <div className="flex flex-col items-center justify-center px-12 py-5 pt-6 text-center min-h-[100px]">
          <div className="flex items-center gap-1.5 mb-2">
            {statusIcons[status]}
          </div>
          <h3 className="text-sm font-semibold text-gray-900 dark:text-white leading-tight">
            {title || `Condition ${stepIndex + 1}`}
          </h3>
        </div>

        {/* True handle - top right area */}
        <Handle 
          type="source" 
          position={Position.Right} 
          id="true" 
          className="!w-3 !h-3 !bg-green-500 !border-2 !border-white dark:!border-gray-900 !shadow-md"
          style={{ top: '30%', right: '-6px' }}
        />

        {/* False handle - bottom right area */}
        <Handle 
          type="source" 
          position={Position.Right} 
          id="false" 
          className="!w-3 !h-3 !bg-red-500 !border-2 !border-white dark:!border-gray-900 !shadow-md"
          style={{ top: '70%', right: '-6px' }}
        />
      </div>

      {/* Condition question - what is being evaluated */}
      {processedConditionQuestion && (
        <div className="mx-4 mt-2 p-2 rounded-lg bg-purple-50 dark:bg-purple-900/20 border border-purple-200 dark:border-purple-800/50">
          <p className="text-[10px] text-purple-700 dark:text-purple-300 text-center">
            <span className="font-semibold">Evaluates: </span>
            {processedConditionQuestion.length > 60
              ? `${processedConditionQuestion.substring(0, 60)}...`
              : processedConditionQuestion}
          </p>
        </div>
      )}

      {/* Branch Routing Information */}
      {(() => {
        // Type guard: check if step is a conditional step
        const conditionalStep = step.type === 'conditional' ? step as ConditionalPlanStep : null
        if (!conditionalStep) return null

        const hasRoutingInfo = conditionalStep.if_true_next_step_id || 
                               conditionalStep.if_false_next_step_id || 
                               (conditionalStep.if_true_steps && conditionalStep.if_true_steps.length > 0) || 
                               (conditionalStep.if_false_steps && conditionalStep.if_false_steps.length > 0)
        
        if (!hasRoutingInfo) return null
        
        return (
          <div className={`mx-4 ${condition_question ? 'mt-2' : 'mt-3'}`}>
            <div className="p-2 rounded-lg bg-gray-50 dark:bg-gray-800/50 border border-gray-200 dark:border-gray-700">
              <div className="text-[10px] font-semibold text-gray-700 dark:text-gray-300 mb-1.5 text-center">
                Branch Routing
              </div>
              <div className="space-y-1.5">
                {/* Yes Branch */}
                <div className="flex items-center justify-between text-[10px]">
                  <div className="flex items-center gap-1.5">
                    <span className="text-green-600 dark:text-green-400 font-medium">✓ Yes:</span>
                    {conditionalStep.if_true_steps && conditionalStep.if_true_steps.length > 0 ? (
                      <span className="text-gray-600 dark:text-gray-400">
                        {conditionalStep.if_true_steps.length} step{conditionalStep.if_true_steps.length !== 1 ? 's' : ''}
                      </span>
                    ) : (
                      <span className="text-gray-500 dark:text-gray-500 italic text-[9px]">(empty)</span>
                    )}
                  </div>
                  {conditionalStep.if_true_next_step_id && (
                    <span className="text-gray-600 dark:text-gray-400 text-[9px]">
                      → {conditionalStep.if_true_next_step_id === 'end' ? 'End' : conditionalStep.if_true_next_step_id}
                    </span>
                  )}
                </div>
                
                {/* No Branch */}
                <div className="flex items-center justify-between text-[10px]">
                  <div className="flex items-center gap-1.5">
                    <span className="text-red-600 dark:text-red-400 font-medium">✗ No:</span>
                    {conditionalStep.if_false_steps && conditionalStep.if_false_steps.length > 0 ? (
                      <span className="text-gray-600 dark:text-gray-400">
                        {conditionalStep.if_false_steps.length} step{conditionalStep.if_false_steps.length !== 1 ? 's' : ''}
                      </span>
                    ) : (
                      <span className="text-gray-500 dark:text-gray-500 italic text-[9px]">(empty)</span>
                    )}
                  </div>
                  {conditionalStep.if_false_next_step_id && (
                    <span className="text-gray-600 dark:text-gray-400 text-[9px]">
                      → {conditionalStep.if_false_next_step_id === 'end' ? 'End' : conditionalStep.if_false_next_step_id}
                    </span>
                  )}
                </div>
              </div>
            </div>
          </div>
        )
      })()}

      {/* Config Footer - only render when selected to reduce DOM nodes */}
      {selected && <div className="mt-2 mx-4">
        <NodeConfigFooter
          description={step?.description}
          successCriteria={step?.success_criteria}
          evalLLM={conditionalLLM}
          decisionQuestion={processedConditionQuestion}
          executionLLM={executionLLM}
          executionMaxTurns={executionMaxTurns}
          learningLLM={learningLLM}

          lockLearnings={lockLearnings}
          effectiveServers={effectiveServers}
          toolsDisplayInfo={toolsDisplayInfo}
          workspaceToolsInfo={workspaceToolsInfo}
          hasWorkspaceTools={hasWorkspaceTools}
          humanToolsInfo={humanToolsInfo}
          hasHumanTools={hasHumanTools}
          hasLargeOutput={hasLargeOutput}
        />
      </div>}
    </div>
  )
})

ConditionalNode.displayName = 'ConditionalNode'
export default ConditionalNode
