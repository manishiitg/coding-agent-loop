import { memo, useMemo, useCallback, type ReactElement, type MouseEvent } from 'react'
import { Handle, Position } from '@xyflow/react'
import { CheckCircle, XCircle, Loader2, Plus, RefreshCw, Code, Terminal, ArrowDownToLine, ArrowUpFromLine, Play } from 'lucide-react'
import { useGlobalPresetStore } from '../../../stores/useGlobalPresetStore'
import { useLLMStore } from '../../../stores/useLLMStore'
import type { StepNodeData } from '../hooks/usePlanToFlow'
import type { ChangeType } from '../hooks/usePlanData'
import { getToolsByCategory } from '../../../utils/customToolNames'

interface StepNodeProps {
  data: StepNodeData
  selected?: boolean
}

const statusBorderColors: Record<string, string> = {
  pending: 'border-gray-300 dark:border-gray-600',
  running: 'border-blue-500 dark:border-blue-400',
  completed: 'border-green-500 dark:border-green-400',
  failed: 'border-red-500 dark:border-red-400'
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
  running: <Loader2 className="w-4 h-4 text-blue-500 animate-spin" />,
  completed: <CheckCircle className="w-4 h-4 text-green-500" />,
  failed: <XCircle className="w-4 h-4 text-red-500" />
}

const getFileName = (path: string): string => path.split('/').pop() || path

// Helper to parse tool entry: "category:tool" or "category:*"
const parseToolEntry = (entry: string): { category: string; tool: string } | null => {
  const colonIndex = entry.indexOf(':')
  if (colonIndex === -1 || colonIndex === 0) return null
  return { 
    category: entry.substring(0, colonIndex), 
    tool: entry.substring(colonIndex + 1) 
  }
}

// Check if category is enabled (empty array means all enabled by default)
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
    return parsed && parsed.category === category && parsed.tool !== '*'
  }).length
  return { enabled, total: allCategoryTools.length }
}

export const StepNode = memo(({ data, selected }: StepNodeProps) => {
  const { title, description, success_criteria, status, stepIndex, changeType, step, onRunFromStep, isExecuting, canRun = true } = data
  const { availableLLMs } = useLLMStore()

  // Button is disabled if executing, can't run (previous steps not done), or no callback
  const isRunDisabled = isExecuting || !canRun || !onRunFromStep

  // Handle run from this step button click
  const handleRunClick = useCallback((e: MouseEvent) => {
    e.stopPropagation() // Prevent node selection
    console.log('[StepNode] Run button clicked:', { stepIndex, stepId: step.id, onRunFromStep: !!onRunFromStep, isExecuting, canRun, isRunDisabled })
    if (onRunFromStep && !isExecuting && canRun) {
      console.log('[StepNode] Calling onRunFromStep with:', stepIndex, step.id || `step-${stepIndex}`)
      onRunFromStep(stepIndex, step.id || `step-${stepIndex}`)
    } else {
      console.warn('[StepNode] Cannot run step:', { 
        hasCallback: !!onRunFromStep, 
        isExecuting, 
        canRun, 
        isRunDisabled 
      })
    }
  }, [onRunFromStep, isExecuting, canRun, stepIndex, step.id, isRunDisabled])

  const activePresetId = useGlobalPresetStore(state => state.activePresetIds.workflow)
  const customPresets = useGlobalPresetStore(state => state.customPresets)
  const predefinedPresets = useGlobalPresetStore(state => state.predefinedPresets)
  
  const activePreset = activePresetId
    ? customPresets.find(p => p.id === activePresetId) || predefinedPresets.find(p => p.id === activePresetId)
    : null

  const stepConfig = step as { agent_configs?: { 
    use_code_execution_mode?: boolean
    execution_llm?: { provider?: string; model_id?: string }
    selected_servers?: string[]
    selected_tools?: string[]
    enabled_custom_tools?: string[]
    enable_large_output_virtual_tools?: boolean
  } }
  
  // Get preset's default code execution mode
  const presetUseCodeExecutionMode = activePreset?.useCodeExecutionMode ?? false
  
  // Determine code execution mode: Priority - step config > preset default (matching backend logic)
  // Only use step-specific if it's EXPLICITLY set (not undefined)
  const stepCodeExecSetting = stepConfig?.agent_configs?.use_code_execution_mode
  const useCodeExecutionMode = stepCodeExecSetting !== undefined 
    ? stepCodeExecSetting === true  // Step has explicit setting
    : presetUseCodeExecutionMode     // Fall back to preset default

  const contextInputs = useMemo(() => step.context_dependencies || [], [step.context_dependencies])
  const contextOutputs = useMemo(() => {
    const output = step.context_output
    if (!output) return []
    return Array.isArray(output) ? output : [output]
  }, [step.context_output])

  const executionLLM = useMemo(() => {
    const presetLLMConfig = activePreset?.llmConfig
    const stepLLMConfig = stepConfig?.agent_configs?.execution_llm
    const presetExecutionLLM = presetLLMConfig?.execution_llm
    const presetDefaultLLM = presetLLMConfig?.provider && presetLLMConfig?.model_id 
      ? { provider: presetLLMConfig.provider, model_id: presetLLMConfig.model_id } : null
    
    const llmConfig = stepLLMConfig || presetExecutionLLM || presetDefaultLLM
    if (!llmConfig?.provider || !llmConfig?.model_id) return null
    
    const llm = availableLLMs?.find(l => l.provider === llmConfig.provider && l.model === llmConfig.model_id)
    return llm?.label || `${llmConfig.provider} ${llmConfig.model_id.split('-').slice(0, 2).join('-')}`
  }, [stepConfig?.agent_configs?.execution_llm, activePreset?.llmConfig, availableLLMs])

  const presetServers = useMemo(() => activePreset?.selectedServers || [], [activePreset?.selectedServers])
  const stepServers = stepConfig?.agent_configs?.selected_servers
  const effectiveServers = useMemo(() => {
    if (stepServers?.length && !stepServers.includes('NO_SERVERS')) return stepServers
    return presetServers
  }, [stepServers, presetServers])

  const presetTools = useMemo(() => activePreset?.selectedTools || [], [activePreset?.selectedTools])
  const effectiveTools = stepConfig?.agent_configs?.selected_tools?.length 
    ? stepConfig.agent_configs.selected_tools : presetTools

  // Parse custom tools (workspace_tools, human_tools)
  const enabledCustomTools = useMemo(() => stepConfig?.agent_configs?.enabled_custom_tools || [], [stepConfig?.agent_configs?.enabled_custom_tools])
  
  const workspaceToolsInfo = useMemo(() => {
    const allWorkspaceTools = getToolsByCategory('workspace_tools')
    return getCategoryToolCount('workspace_tools', enabledCustomTools, allWorkspaceTools)
  }, [enabledCustomTools])
  
  const humanToolsInfo = useMemo(() => {
    const allHumanTools = getToolsByCategory('human_tools')
    return getCategoryToolCount('human_tools', enabledCustomTools, allHumanTools)
  }, [enabledCustomTools])
  
  const hasWorkspaceTools = workspaceToolsInfo.enabled > 0
  const hasHumanTools = humanToolsInfo.enabled > 0
  const hasLargeOutput = stepConfig?.agent_configs?.enable_large_output_virtual_tools !== false // Default is enabled

  const hasContext = contextInputs.length > 0 || contextOutputs.length > 0
  const hasConfig = executionLLM || effectiveServers.length > 0 || effectiveTools.length > 0 || hasWorkspaceTools || hasHumanTools || hasLargeOutput

  return (
    <div className={`
      relative w-[340px] rounded-xl border-2 bg-white dark:bg-gray-900 shadow-lg overflow-hidden
      ${statusBorderColors[status]}
      ${selected ? 'ring-2 ring-blue-500/40' : ''}
      ${changeType ? changeHighlightStyles[changeType] : ''}
    `}>
      {/* Change badge */}
      {changeType && (
        <div className={`absolute -top-2 -right-2 z-10 flex items-center gap-1 px-2 py-0.5 rounded-full ${changeBadgeStyles[changeType].bg} text-white text-[10px] font-medium shadow-lg`}>
          {changeBadgeStyles[changeType].icon}
          <span className="capitalize">{changeType}</span>
        </div>
      )}

      <Handle type="target" position={Position.Left} className="!w-3 !h-3 !bg-gray-400 dark:!bg-gray-500 !border-2 !border-white dark:!border-gray-900" />

      {/* Header */}
      <div className="flex items-center gap-3 px-4 py-3 bg-gray-50 dark:bg-gray-800/50 border-b border-gray-200 dark:border-gray-700">
        <div className="flex items-center justify-center w-7 h-7 rounded-lg bg-gray-200 dark:bg-gray-700 text-gray-700 dark:text-gray-200 text-sm font-bold">
          {stepIndex + 1}
        </div>
        <div className="flex-1 min-w-0">
          <h3 className="text-sm font-semibold text-gray-900 dark:text-white leading-tight truncate">
            {title}
          </h3>
        </div>
        <div className="flex items-center gap-2">
          {/* Run from this step button */}
          {onRunFromStep ? (
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
                  : !canRun 
                    ? 'Complete previous steps first' 
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
          {/* Agent Mode Badge */}
          {useCodeExecutionMode ? (
            <div className="flex items-center gap-1 px-2 py-1 rounded-md bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-300 text-[10px] font-semibold">
              <Terminal className="w-3 h-3" />
              <span>Code</span>
            </div>
          ) : (
            <div className="flex items-center gap-1 px-2 py-1 rounded-md bg-blue-100 dark:bg-blue-900/40 text-blue-700 dark:text-blue-300 text-[10px] font-semibold">
              <Code className="w-3 h-3" />
              <span>Agent</span>
            </div>
          )}
          {statusIcons[status]}
        </div>
      </div>

      {/* Content */}
      <div className="px-4 py-3 space-y-3">
        {description && (
          <p className="text-xs text-gray-600 dark:text-gray-400 leading-relaxed">
            {description}
          </p>
        )}
        
        {success_criteria && (
          <div className="flex gap-2 p-2.5 rounded-lg bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800/50">
            <CheckCircle className="w-4 h-4 text-green-500 flex-shrink-0 mt-0.5" />
            <p className="text-xs text-green-700 dark:text-green-300 leading-relaxed">
              {success_criteria}
            </p>
          </div>
        )}

        {/* Context Files */}
        {hasContext && (
          <div className="space-y-1.5">
            {contextInputs.length > 0 && (
              <div className="flex items-start gap-2">
                <ArrowDownToLine className="w-3.5 h-3.5 text-blue-500 mt-0.5 flex-shrink-0" />
                <div className="flex flex-wrap gap-1">
                  {contextInputs.map((f, i) => (
                    <span key={i} className="px-1.5 py-0.5 rounded text-[10px] bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300" title={f}>
                      {getFileName(f)}
                    </span>
                  ))}
                </div>
              </div>
            )}
            {contextOutputs.length > 0 && (
              <div className="flex items-start gap-2">
                <ArrowUpFromLine className="w-3.5 h-3.5 text-emerald-500 mt-0.5 flex-shrink-0" />
                <div className="flex flex-wrap gap-1">
                  {contextOutputs.map((f, i) => (
                    <span key={i} className="px-1.5 py-0.5 rounded text-[10px] bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300" title={f}>
                      {getFileName(f)}
                    </span>
                  ))}
                </div>
              </div>
            )}
          </div>
        )}
      </div>

      {/* Config Footer */}
      {hasConfig && (
        <div className="px-4 py-2.5 bg-gray-50 dark:bg-gray-800/30 border-t border-gray-200 dark:border-gray-700">
          <div className="flex flex-wrap gap-1.5">
            {executionLLM && (
              <span className="px-2 py-0.5 rounded-md text-[10px] font-medium bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300">
                {executionLLM}
              </span>
            )}
            {effectiveServers.map((s, i) => (
              <span key={i} className="px-2 py-0.5 rounded-md text-[10px] font-medium bg-gray-200 dark:bg-gray-700 text-gray-700 dark:text-gray-300">
                {s}
              </span>
            ))}
            {effectiveTools.length > 0 && (
              <span className="px-2 py-0.5 rounded-md text-[10px] font-medium bg-gray-200 dark:bg-gray-700 text-gray-600 dark:text-gray-400">
                {effectiveTools.length} tools
              </span>
            )}
            {hasWorkspaceTools && (
              <span className="px-2 py-0.5 rounded-md text-[10px] font-medium bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300" title={`Workspace tools: ${workspaceToolsInfo.enabled}/${workspaceToolsInfo.total}`}>
                WS: {workspaceToolsInfo.enabled}/{workspaceToolsInfo.total}
              </span>
            )}
            {hasHumanTools && (
              <span className="px-2 py-0.5 rounded-md text-[10px] font-medium bg-gray-200 dark:bg-gray-700 text-gray-700 dark:text-gray-300" title={`Human tools: ${humanToolsInfo.enabled}/${humanToolsInfo.total}`}>
                Human: {humanToolsInfo.enabled}/{humanToolsInfo.total}
              </span>
            )}
            {hasLargeOutput && (
              <span className="px-2 py-0.5 rounded-md text-[10px] font-medium bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300" title="Large output virtual tools enabled">
                Large Output
              </span>
            )}
          </div>
        </div>
      )}

      <Handle type="source" position={Position.Right} className="!w-3 !h-3 !bg-gray-400 dark:!bg-gray-500 !border-2 !border-white dark:!border-gray-900" />
      
      {/* Retry Handle - for validation loop-back */}
      <Handle
        type="target"
        position={Position.Top}
        id="retry"
        className="!w-2 !h-2 !bg-amber-500 !border-2 !border-white dark:!border-gray-900"
      />
    </div>
  )
})

StepNode.displayName = 'StepNode'
export default StepNode
