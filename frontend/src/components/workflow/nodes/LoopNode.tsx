import { memo, useMemo, type ReactElement } from 'react'
import { Handle, Position } from '@xyflow/react'
import { RefreshCw, CheckCircle, XCircle, Loader2, Plus, Code, Terminal, ArrowDownToLine, ArrowUpFromLine, Repeat } from 'lucide-react'
import { useGlobalPresetStore } from '../../../stores/useGlobalPresetStore'
import { useLLMStore } from '../../../stores/useLLMStore'
import type { LoopNodeData } from '../hooks/usePlanToFlow'
import type { ChangeType } from '../hooks/usePlanData'

interface LoopNodeProps {
  data: LoopNodeData
  selected?: boolean
}

const statusBorderColors: Record<string, string> = {
  pending: 'border-gray-300 dark:border-gray-600',
  running: 'border-cyan-500 dark:border-cyan-400',
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

export const LoopNode = memo(({ data, selected }: LoopNodeProps) => {
  const { title, loop_condition, max_iterations, current_iteration, status, stepIndex, changeType, step } = data
  const { availableLLMs } = useLLMStore()

  const activePresetId = useGlobalPresetStore(state => state.activePresetIds.workflow)
  const customPresets = useGlobalPresetStore(state => state.customPresets)
  const predefinedPresets = useGlobalPresetStore(state => state.predefinedPresets)
  
  const activePreset = activePresetId
    ? customPresets.find(p => p.id === activePresetId) || predefinedPresets.find(p => p.id === activePresetId)
    : null

  // Get step details (same as StepNode)
  const description = step.description
  const success_criteria = step.success_criteria

  const stepConfig = step as { agent_configs?: { 
    use_code_execution_mode?: boolean
    execution_llm?: { provider?: string; model_id?: string }
    selected_servers?: string[]
    selected_tools?: string[]
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

  const hasContext = contextInputs.length > 0 || contextOutputs.length > 0
  const hasConfig = executionLLM || effectiveServers.length > 0 || effectiveTools.length > 0

  return (
    <div className={`
      relative w-[360px] rounded-xl border-2 border-dashed bg-white dark:bg-gray-900 shadow-lg
      ${statusBorderColors[status]}
      ${selected ? 'ring-2 ring-blue-500/40' : ''}
      ${changeType ? changeHighlightStyles[changeType] : ''}
    `}>
      {/* Loop Badge - Top Left */}
      <div className="absolute -top-3 left-4 z-20 flex items-center gap-1 px-2 py-0.5 rounded-md bg-gray-800 text-white text-[10px] font-semibold shadow-md border border-gray-700">
        <Repeat className="w-3 h-3 text-cyan-400" />
        <span>Loop</span>
        {max_iterations && (
          <span className="text-cyan-300 ml-0.5">
            {current_iteration !== undefined && status === 'running' 
              ? `${current_iteration}/${max_iterations}` 
              : `×${max_iterations}`}
          </span>
        )}
      </div>

      {/* Change badge */}
      {changeType && (
        <div className={`absolute -top-2 -right-2 z-10 flex items-center gap-1 px-2 py-0.5 rounded-full ${changeBadgeStyles[changeType].bg} text-white text-[10px] font-medium shadow-lg`}>
          {changeBadgeStyles[changeType].icon}
          <span className="capitalize">{changeType}</span>
        </div>
      )}

      <Handle type="target" position={Position.Left} className="!w-3 !h-3 !bg-gray-400 dark:!bg-gray-500 !border-2 !border-white dark:!border-gray-900" />

      {/* Header */}
      <div className="flex items-center gap-3 px-4 py-3 pt-5 bg-gray-50 dark:bg-gray-800/50 border-b border-gray-200 dark:border-gray-700">
        <div className="flex-1 min-w-0">
          <h3 className="text-sm font-semibold text-gray-900 dark:text-white leading-tight">
            {title || `Loop ${stepIndex + 1}`}
          </h3>
        </div>
        <div className="flex items-center gap-2">
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
        {/* Loop Condition */}
        {loop_condition && (
          <div className="p-2.5 rounded-lg bg-gray-100 dark:bg-gray-800 border border-gray-200 dark:border-gray-700">
            <div className="text-[10px] font-bold text-cyan-600 dark:text-cyan-400 uppercase tracking-wide mb-1">Until</div>
            <p className="text-xs text-gray-700 dark:text-gray-300 leading-relaxed">
              {loop_condition}
            </p>
          </div>
        )}

        {/* Description */}
        {description && (
          <p className="text-xs text-gray-600 dark:text-gray-400 leading-relaxed">
            {description}
          </p>
        )}
        
        {/* Success Criteria */}
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

        {/* Progress Bar */}
        {max_iterations && status === 'running' && current_iteration !== undefined && (
          <div className="space-y-1">
            <div className="flex justify-between text-[10px] text-gray-500 dark:text-gray-400">
              <span>Progress</span>
              <span>{Math.round((current_iteration / max_iterations) * 100)}%</span>
            </div>
            <div className="h-1.5 bg-gray-200 dark:bg-gray-700 rounded-full overflow-hidden">
              <div 
                className="h-full bg-blue-500 rounded-full transition-all duration-300"
                style={{ width: `${(current_iteration / max_iterations) * 100}%` }}
              />
            </div>
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
          </div>
        </div>
      )}

      {/* Loop back handle */}
      <Handle type="source" position={Position.Top} id="loop-back" className="!w-3 !h-3 !bg-gray-400 dark:!bg-gray-500 !border-2 !border-white dark:!border-gray-900" />

      {/* Retry Handle - for validation loop-back */}
      <Handle
        type="target"
        position={Position.Top}
        id="retry"
        className="!w-2 !h-2 !bg-amber-500 !border-2 !border-white dark:!border-gray-900 !left-1/3"
      />

      {/* Output handle */}
      <Handle type="source" position={Position.Right} className="!w-3 !h-3 !bg-gray-400 dark:!bg-gray-500 !border-2 !border-white dark:!border-gray-900" />
    </div>
  )
})

LoopNode.displayName = 'LoopNode'
export default LoopNode
