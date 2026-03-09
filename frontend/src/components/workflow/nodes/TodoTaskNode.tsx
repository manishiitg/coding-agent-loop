import { memo, useCallback, useMemo, useState, useEffect, type ReactElement, type MouseEvent } from 'react'
import { Handle, Position } from '@xyflow/react'
import { CheckCircle, XCircle, Loader2, Plus, RefreshCw, Play, Settings, Code, Terminal, AlertTriangle, Lock, ArrowDownToLine, ArrowUpFromLine, ListTodo, Search, Bot } from 'lucide-react'
import { useGlobalPresetStore } from '../../../stores/useGlobalPresetStore'
import { useLLMStore } from '../../../stores/useLLMStore'
import { useWorkspaceStore } from '../../../stores/useWorkspaceStore'
import { useWorkflowStore } from '../../../stores/useWorkflowStore'
import { useAppStore } from '../../../stores'
import { useCapabilitiesStore } from '../../../stores/useCapabilitiesStore'
import { agentApi } from '../../../services/api'
import { isValidJSON } from '../../../utils/event-helpers'
import { getToolsByCategory } from '../../../utils/customToolNames'
import { NodeConfigFooter } from './NodeConfigFooter'
import type { TodoTaskNodeData } from '../hooks/usePlanToFlow'
import type { ChangeType } from '../hooks/usePlanData'

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

interface TodoTaskNodeProps {
  data: TodoTaskNodeData
  selected?: boolean
}

const statusBorderColors: Record<string, string> = {
  pending: 'border-gray-300 dark:border-gray-600',
  running: 'border-purple-500 dark:border-purple-600',
  executing: 'border-purple-500 dark:border-purple-600',
  evaluating: 'border-purple-500 dark:border-purple-600',
  orchestrating: 'border-purple-500 dark:border-purple-600',
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
  running: <Loader2 className="w-4 h-4 text-purple-500 dark:text-purple-400 animate-spin" />,
  executing: <Loader2 className="w-4 h-4 text-purple-500 dark:text-purple-400 animate-spin" />,
  evaluating: <Loader2 className="w-4 h-4 text-purple-500 dark:text-purple-400 animate-spin" />,
  orchestrating: <Loader2 className="w-4 h-4 text-purple-500 dark:text-purple-400 animate-spin" />,
  completed: <CheckCircle className="w-4 h-4 text-green-500" />,
  failed: <XCircle className="w-4 h-4 text-red-500" />
}

export const TodoTaskNode = memo(({ data, selected }: TodoTaskNodeProps) => {
  const { id, title, todo_task_step, predefined_routes, enable_generic_agent, status, stepIndex, changeType, step, onRunFromStep, onOpenSidebar, isExecuting, workspacePath, selectedRunFolder } = data
  const { highlightFile, setShowFileContent, fetchFiles, setSelectedFile, setFileContent, setLoadingFileContent, setError } = useWorkspaceStore()
  const { setWorkspaceMinimized } = useAppStore()
  const layoutDirection = useWorkflowStore(state => state.layoutDirection)
  const stepOverride = useWorkflowStore(state => state.stepOverride)

  // Determine handle positions based on layout direction
  const isHorizontal = layoutDirection === 'LR'
  const outputPosition = isHorizontal ? Position.Right : Position.Bottom

  // Context inputs and outputs from the MAIN STEP (todo_task_step)
  const contextInputs = useMemo(() => todo_task_step?.context_dependencies || [], [todo_task_step?.context_dependencies])
  const contextOutputs = useMemo(() => {
    const output = todo_task_step?.context_output
    if (!output) return []
    return Array.isArray(output) ? output : [output]
  }, [todo_task_step?.context_output])
  const hasContext = contextInputs.length > 0 || contextOutputs.length > 0

  // Get preset for config badges
  const activePresetId = useGlobalPresetStore(state => state.activePresetIds.workflow)
  const customPresets = useGlobalPresetStore(state => state.customPresets)
  const predefinedPresets = useGlobalPresetStore(state => state.predefinedPresets)

  const activePreset = activePresetId
    ? customPresets.find(p => p.id === activePresetId) || predefinedPresets.find(p => p.id === activePresetId)
    : null

  const { availableLLMs } = useLLMStore()
  const { capabilities } = useCapabilitiesStore()

  // Get step config (agent_configs) - for TodoTask, check inner todo_task_step first, then outer step
  // The inner todo_task_step contains the actual execution configs
  const innerStep = todo_task_step as { agent_configs?: {
    use_code_execution_mode?: boolean
    use_tool_search_mode?: boolean
    enable_prerequisite_detection?: boolean
    prerequisite_rules?: Array<{ depends_on_step: string; description: string }>
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
  } } | undefined
  const outerStep = step as { agent_configs?: typeof innerStep extends { agent_configs?: infer T } ? T : never }
  // Prefer inner step configs (where the actual execution happens), fall back to outer step
  const stepConfig = innerStep?.agent_configs ? innerStep : outerStep

  // Determine code execution mode: override > step config > preset default
  const presetUseCodeExecutionMode = activePreset?.useCodeExecutionMode ?? false
  const overrideCodeExec = stepOverride?.use_code_execution_mode
  const stepCodeExecSetting = stepConfig?.agent_configs?.use_code_execution_mode
  const useCodeExecutionMode = overrideCodeExec !== undefined
    ? overrideCodeExec === true
    : stepCodeExecSetting !== undefined
      ? stepCodeExecSetting === true
      : presetUseCodeExecutionMode

  // Get preset's default tool search mode
  const presetUseToolSearchMode = activePreset?.useToolSearchMode ?? false

  // Determine tool search mode: override > step config > preset default
  const overrideToolSearch = stepOverride?.use_tool_search_mode
  const stepToolSearchSetting = stepConfig?.agent_configs?.use_tool_search_mode
  const useToolSearchMode = overrideToolSearch !== undefined
    ? overrideToolSearch === true
    : stepToolSearchSetting !== undefined
      ? stepToolSearchSetting === true
      : presetUseToolSearchMode

  // Execution LLM: global override > step config > preset execution_llm > preset default
  const executionLLM = useMemo(() => {
    const presetLLMConfig = activePreset?.llmConfig
    const overrideLLMConfig = stepOverride?.execution_llm
    const stepLLMConfig = stepConfig?.agent_configs?.execution_llm
    const presetExecutionLLM = presetLLMConfig?.execution_llm
    const presetDefaultLLM = presetLLMConfig?.provider && presetLLMConfig?.model_id
      ? { provider: presetLLMConfig.provider, model_id: presetLLMConfig.model_id } : null

    const llmConfig = overrideLLMConfig || stepLLMConfig || presetExecutionLLM || presetDefaultLLM
    if (!llmConfig?.provider || !llmConfig?.model_id) return null

    const llm = availableLLMs?.find(l => l.provider === llmConfig.provider && l.model === llmConfig.model_id)
    return llm?.label || `${llmConfig.provider} ${llmConfig.model_id.split('-').slice(0, 2).join('-')}`
  }, [stepOverride?.execution_llm, stepConfig?.agent_configs?.execution_llm, activePreset?.llmConfig, availableLLMs])

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
    const stepLearningLLM = stepConfig?.agent_configs?.learning_llm
    const presetLearningLLM = presetLLMConfig?.learning_llm
    const presetDefaultLLM = presetLLMConfig?.provider && presetLLMConfig?.model_id
      ? { provider: presetLLMConfig.provider, model_id: presetLLMConfig.model_id } : null

    const llmConfig = overrideLLMConfig || stepLearningLLM || presetLearningLLM || presetDefaultLLM
    if (!llmConfig?.provider || !llmConfig?.model_id) return null

    const llm = availableLLMs?.find(l => l.provider === llmConfig.provider && l.model === llmConfig.model_id)
    return llm?.label || `${llmConfig.provider} ${llmConfig.model_id.split('-').slice(0, 2).join('-')}`
  }, [learningDisabled, stepOverride?.learning_llm, stepConfig?.agent_configs?.learning_llm, activePreset?.llmConfig, availableLLMs])


  // Check if learnings exist in backend
  const [learningsExist, setLearningsExist] = useState<boolean | null>(null)
  const stepIdForLearnings = todo_task_step?.id ?? step?.id

  useEffect(() => {
    if (!workspacePath || !stepIdForLearnings) {
      setLearningsExist(false)
      return
    }

    const checkLearningsExist = async () => {
      try {
        const learningsPath = `${workspacePath}/learnings/${stepIdForLearnings}`
        const files = await agentApi.getPlannerFiles(learningsPath, 100)

        const hasLearningFiles = files && Array.isArray(files) && files.some((file: { filepath?: string; name?: string }) => {
          const fileName = file.filepath || file.name || ''
          return fileName.endsWith('.md') || (fileName.startsWith('code/') && /\.(go|py|sh|js|ts|jsx|tsx|bash|rb|java|rs|c|cpp|json|yaml|yml)$/.test(fileName))
        })

        setLearningsExist(hasLearningFiles)
      } catch (error) {
        console.debug('[TodoTaskNode] Failed to check learnings:', error)
        setLearningsExist(false)
      }
    }

    checkLearningsExist()
  }, [workspacePath, stepIdForLearnings])

  // Lock learnings: override > step config
  const isLockedInConfig = useMemo(() => {
    if (stepOverride?.lock_learnings !== undefined) return stepOverride.lock_learnings === true
    return todo_task_step?.agent_configs?.lock_learnings ?? false
  }, [stepOverride?.lock_learnings, todo_task_step?.agent_configs?.lock_learnings])

  const lockLearnings = useMemo(() => {
    return isLockedInConfig && (learningsExist === true) && !learningDisabled
  }, [isLockedInConfig, learningsExist, learningDisabled])

  // Execution max turns: override > step config
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

  // Group tools by server
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
        info.hasAllTools = true
        info.specificTools = 0
      } else if (!info.hasAllTools) {
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
    : stepConfig?.agent_configs?.enable_context_offloading) !== false

  // Button states
  const isRunDisabled = isExecuting || !onRunFromStep

  // Handle run from this step button click
  const handleRunClick = useCallback((e: MouseEvent) => {
    e.stopPropagation()
    e.preventDefault()
    if (onRunFromStep && !isExecuting) {
      onRunFromStep(stepIndex, step.id || `step-${stepIndex}`)
    }
  }, [onRunFromStep, isExecuting, stepIndex, step.id])

  // Handle settings icon click
  const handleSettingsClick = useCallback((e: MouseEvent) => {
    e.stopPropagation()
    e.preventDefault()
    if (onOpenSidebar && typeof onOpenSidebar === 'function') {
      onOpenSidebar(id)
    }
  }, [onOpenSidebar, id])

  // Helper to get filename from full path
  const getFileName = (filePath: string): string => {
    return filePath.split('/').pop() || filePath
  }

  // Handle file click - open file in workspace
  const handleFileClick = useCallback(async (filename: string, e: MouseEvent) => {
    e.stopPropagation()

    if (!workspacePath || !selectedRunFolder || selectedRunFolder === 'new') {
      console.warn('[TodoTaskNode] Cannot open file: missing workspacePath or selectedRunFolder')
      return
    }

    const filePath = `${workspacePath}/runs/${selectedRunFolder}/execution/${filename}`

    try {
      setWorkspaceMinimized(false)
      setError(null)
      setLoadingFileContent(true)

      const fileName = filePath.split('/').pop() || filePath
      setSelectedFile({ name: fileName, path: filePath })

      const response = await agentApi.getPlannerFileContent(filePath)

      if (response.success && response.data) {
        const rawContent = response.data.content
        if (rawContent === undefined || rawContent === null) {
          setError(`File content is empty or unavailable: ${fileName}\n\nPath: ${filePath}`)
          setShowFileContent(false)
          setSelectedFile(null)
          setLoadingFileContent(false)
          return
        }

        let processedContent = typeof rawContent === 'string' ? rawContent : String(rawContent)
        let formattedJson = null

        if (response.data.is_image && processedContent && processedContent.startsWith('data:image/')) {
          // No processing needed for images
        } else {
          processedContent = processedContent
            .replace(/\\n/g, '\n')
            .replace(/\\t/g, '\t')
            .replace(/\\r/g, '\r')

          const extensionIsJson = filePath.toLowerCase().endsWith('.json')
          const contentIsJson = isValidJSON(processedContent)
          const isJsonFile = extensionIsJson || contentIsJson

          if (isJsonFile) {
            try {
              const parsed = JSON.parse(processedContent)
              formattedJson = JSON.stringify(parsed, null, 2)
            } catch (parseError) {
              console.warn('Failed to parse JSON file:', parseError)
              formattedJson = null
            }
          }
        }

        setFileContent(processedContent)
        if (formattedJson) {
          setFileContent(formattedJson)
        }

        await fetchFiles()
        setTimeout(() => {
          highlightFile(filePath)
        }, 200)

        setShowFileContent(true)
      } else {
        const errorMessage = response.message || 'File not found'
        setError(`File not found: ${fileName}\n${errorMessage}\n\nPath: ${filePath}`)
        setShowFileContent(false)
        setSelectedFile(null)
      }
    } catch (error) {
      console.error('[TodoTaskNode] Error opening file:', error)
      const fileName = filePath.split('/').pop() || filePath
      const errorMessage = error instanceof Error ? error.message : 'Failed to fetch file content'
      setError(`Failed to open file: ${fileName}\n${errorMessage}\n\nPath: ${filePath}`)
      setShowFileContent(false)
      setSelectedFile(null)
    } finally {
      setLoadingFileContent(false)
    }
  }, [workspacePath, selectedRunFolder, highlightFile, setShowFileContent, fetchFiles, setWorkspaceMinimized, setSelectedFile, setFileContent, setLoadingFileContent, setError])

  // Calculate node height based on content
  const nodeHeight = useMemo(() => {
    let height = 80 // Base height (header + title)
    if (todo_task_step) height += 30
    if (hasContext) height += 40
    if (predefined_routes && predefined_routes.length > 0) height += 25
    if (enable_generic_agent) height += 20
    return Math.max(height, 120) // Minimum height
  }, [todo_task_step, hasContext, predefined_routes, enable_generic_agent])

  return (
    <div className={`relative w-[300px] ${changeType ? changeHighlightStyles[changeType] : ''}`}>
      {/* Header with buttons - above the card */}
      <div className="absolute -top-12 left-0 right-0 flex items-center justify-center gap-2 z-20">
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
                : `Run step ${stepIndex + 1} only`
            }
          >
            <Play className="w-3.5 h-3.5" />
          </button>
        ) : (
          <div className="w-7 h-7 flex items-center justify-center text-xs text-gray-400" title="Run callback not available">
            <AlertTriangle className="w-3.5 h-3.5" />
          </div>
        )}
        {/* Settings icon button */}
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
        ) : useToolSearchMode ? (
          <div className="flex items-center justify-center w-7 h-7 rounded-md bg-yellow-100 dark:bg-yellow-900/40 text-yellow-700 dark:text-yellow-300 border border-yellow-200 dark:border-yellow-800" title="Tool Search Mode">
            <Search className="w-3.5 h-3.5" />
          </div>
        ) : (
          <div className="flex items-center justify-center w-7 h-7 rounded-md bg-slate-100 dark:bg-slate-800/60 text-slate-700 dark:text-slate-300 border border-slate-200 dark:border-slate-700" title="Simple Agent Mode">
            <Code className="w-3.5 h-3.5" />
          </div>
        )}
        {/* Prerequisite Detection Badge */}
        {stepConfig?.agent_configs?.enable_prerequisite_detection && (
          <div
            className="flex items-center gap-1 px-2 py-1 rounded-md bg-orange-100 dark:bg-orange-900/40 text-orange-700 dark:text-orange-300 text-[10px] font-semibold border border-orange-200 dark:border-orange-800"
            title={
              stepConfig.agent_configs.prerequisite_rules && stepConfig.agent_configs.prerequisite_rules.length > 0
                ? `Prerequisite detection enabled. ${stepConfig.agent_configs.prerequisite_rules.length} rule(s) configured`
                : 'Prerequisite detection enabled'
            }
          >
            <AlertTriangle className="w-3 h-3" />
            <span>Prereq</span>
          </div>
        )}
        {/* Lock Learnings Badge */}
        {lockLearnings && !learningDisabled && (
          <div
            className="flex items-center gap-1 px-2 py-1 rounded-md bg-purple-100 dark:bg-purple-900/40 text-purple-700 dark:text-purple-300 text-[10px] font-semibold border border-purple-200 dark:border-purple-800"
            title="Learnings are locked - learning agent will not run but existing learnings will be used"
          >
            <Lock className="w-3 h-3" />
            <span>Locked</span>
          </div>
        )}
      </div>

      {/* Todo Task Badge - Top */}
      <div className="absolute -top-2.5 left-1/2 -translate-x-1/2 z-10 flex items-center gap-1.5 px-2.5 py-1 rounded-full bg-purple-600 dark:bg-purple-700 text-white text-[11px] font-semibold shadow-lg">
        <ListTodo className="w-3.5 h-3.5" />
        <span>Todo Task</span>
      </div>

      {/* Change badge */}
      {changeType && (
        <div className={`absolute top-0 right-0 z-10 flex items-center gap-1 px-1.5 py-0.5 rounded-bl-lg rounded-tr-xl ${changeBadgeStyles[changeType].bg} text-white text-[10px] font-medium shadow-lg`}>
          {changeBadgeStyles[changeType].icon}
          <span className="capitalize">{changeType}</span>
        </div>
      )}

      {/* Rectangle Shape Card */}
      <div
        className={`
          relative rounded-xl border-2 bg-white dark:bg-gray-900 shadow-lg overflow-visible
          ${statusBorderColors[status]}
          ${selected ? 'ring-2 ring-purple-500/60' : ''}
          ${status === 'running' || status === 'executing' || status === 'evaluating' || status === 'orchestrating' ? 'animate-pulse' : ''}
        `}
        style={{
          minHeight: `${nodeHeight}px`,
          width: '300px'
        }}
      >
        {/* Input handle */}
        <Handle
          type="target"
          position={Position.Left}
          className="!w-3 !h-3 !bg-purple-500 dark:!bg-purple-600 !border-2 !border-white dark:!border-gray-900"
          style={isHorizontal ? { left: '-6px', top: '50%' } : { top: '-6px', left: '50%' }}
        />

        {/* Content */}
        <div className="flex flex-col px-4 py-4">
          <div className="flex items-center gap-1.5 mb-2 justify-center">
            {statusIcons[status]}
          </div>
          <h3 className="text-sm font-semibold text-gray-900 dark:text-white leading-tight text-center mb-1.5">
            {title || `Todo Task ${stepIndex + 1}`}
          </h3>

          {/* Main todo task step title */}
          {todo_task_step && (
            <div className="mt-1.5 p-2 rounded-lg bg-gray-50 dark:bg-gray-800/50 border border-gray-200 dark:border-gray-700/60">
              <p className="text-[10px] text-gray-700 dark:text-gray-300 font-semibold">
                Task: {todo_task_step.title || 'Untitled Step'}
              </p>
            </div>
          )}

          {/* Predefined Routes Count */}
          {predefined_routes && predefined_routes.length > 0 && (
            <div className="mt-2 flex items-center gap-1.5 text-[10px] text-purple-600 dark:text-purple-400">
              <Bot className="w-3 h-3" />
              <span>{predefined_routes.length} predefined route{predefined_routes.length > 1 ? 's' : ''}</span>
            </div>
          )}

          {/* Generic Agent Indicator */}
          {enable_generic_agent && (
            <div className="mt-1.5 flex items-center gap-1.5 text-[10px] text-gray-500 dark:text-gray-400">
              <Code className="w-3 h-3" />
              <span>Generic agent enabled</span>
            </div>
          )}

          {/* Context Files - from main step (todo_task_step) */}
          {hasContext && (
            <div className="space-y-1.5 mt-2">
              {contextInputs.length > 0 && (
                <div className="flex items-start gap-2">
                  <ArrowDownToLine className="w-3.5 h-3.5 text-purple-500 mt-0.5 flex-shrink-0" />
                  <div className="flex flex-wrap gap-1">
                    {contextInputs.map((f, i) => {
                      const fileName = getFileName(f)
                      const canOpen = workspacePath && selectedRunFolder && selectedRunFolder !== 'new'
                      return (
                        <span
                          key={i}
                          onClick={canOpen ? (e) => handleFileClick(f, e) : undefined}
                          className={`
                            px-1.5 py-0.5 rounded text-[10px] bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300
                            ${canOpen ? 'cursor-pointer hover:bg-purple-200 dark:hover:bg-purple-900/50 hover:underline' : ''}
                          `}
                          title={canOpen ? `Click to open: ${f}` : f}
                        >
                          {fileName}
                        </span>
                      )
                    })}
                  </div>
                </div>
              )}
              {contextOutputs.length > 0 && (
                <div className="flex items-start gap-2">
                  <ArrowUpFromLine className="w-3.5 h-3.5 text-emerald-500 mt-0.5 flex-shrink-0" />
                  <div className="flex flex-wrap gap-1">
                    {contextOutputs.map((f, i) => {
                      const fileName = getFileName(f)
                      const canOpen = workspacePath && selectedRunFolder && selectedRunFolder !== 'new'
                      return (
                        <span
                          key={i}
                          onClick={canOpen ? (e) => handleFileClick(f, e) : undefined}
                          className={`
                            px-1.5 py-0.5 rounded text-[10px] bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300
                            ${canOpen ? 'cursor-pointer hover:bg-emerald-200 dark:hover:bg-emerald-900/50 hover:underline' : ''}
                          `}
                          title={canOpen ? `Click to open: ${f}` : f}
                        >
                          {fileName}
                        </span>
                      )
                    })}
                  </div>
                </div>
              )}
            </div>
          )}
        </div>

        {/* Output handles - one for each predefined route + "end" route option */}
        {predefined_routes && predefined_routes.length > 0 ? (
          <>
            {predefined_routes.map((route, index) => {
              const totalRoutes = predefined_routes.length + 1 // +1 for "end" route
              const positionPercent = 20 + (index * (60 / (totalRoutes - 1)))
              return (
                <Handle
                  key={route.route_id}
                  type="source"
                  position={outputPosition}
                  id={route.route_id}
                  className="!w-3 !h-3 !bg-purple-500 dark:!bg-purple-600 !border-2 !border-white dark:!border-gray-900 !shadow-md"
                  style={isHorizontal
                    ? { right: '-6px', top: `${positionPercent}%` }
                    : { left: `${positionPercent}%`, bottom: '-6px' }
                  }
                />
              )
            })}
            {/* "end" route handle */}
            <Handle
              key="end"
              type="source"
              position={outputPosition}
              id="end"
              className="!w-3 !h-3 !bg-red-500 dark:!bg-red-600 !border-2 !border-white dark:!border-gray-900 !shadow-md"
              style={isHorizontal
                ? { right: '-6px', top: `${20 + (predefined_routes.length * (60 / (predefined_routes.length)))}%` }
                : { left: `${20 + (predefined_routes.length * (60 / (predefined_routes.length)))}%`, bottom: '-6px' }
              }
              title="End workflow route"
            />
          </>
        ) : (
          <>
            <Handle
              type="source"
              position={outputPosition}
              className="!w-3 !h-3 !bg-purple-500 dark:!bg-purple-600 !border-2 !border-white dark:!border-gray-900 !shadow-md"
              style={isHorizontal ? { right: '-6px', top: '40%' } : { left: '40%', bottom: '-6px' }}
            />
            {/* "end" route handle */}
            <Handle
              type="source"
              position={outputPosition}
              id="end"
              className="!w-3 !h-3 !bg-red-500 dark:!bg-red-600 !border-2 !border-white dark:!border-gray-900 !shadow-md"
              style={isHorizontal ? { right: '-6px', top: '60%' } : { left: '60%', bottom: '-6px' }}
              title="End workflow route"
            />
          </>
        )}
      </div>

      {/* Config Footer */}
      <div className="mt-2 mx-4">
        <NodeConfigFooter
          description={step?.description}
          successCriteria={step?.success_criteria}
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
      </div>
    </div>
  )
})

TodoTaskNode.displayName = 'TodoTaskNode'
export default TodoTaskNode
