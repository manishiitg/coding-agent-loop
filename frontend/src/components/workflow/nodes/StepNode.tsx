import { memo, useMemo, useCallback, type ReactElement, type MouseEvent } from 'react'
import { Handle, Position } from '@xyflow/react'
import { CheckCircle, XCircle, Loader2, Plus, RefreshCw, Code, Terminal, ArrowDownToLine, ArrowUpFromLine, Settings, Play, AlertTriangle, Lock, SkipForward, Search, Bot, Pause } from 'lucide-react'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../../ui/tooltip'
import { useGlobalPresetStore } from '../../../stores/useGlobalPresetStore'
import { useLLMStore } from '../../../stores/useLLMStore'
import { useWorkspaceStore } from '../../../stores/useWorkspaceStore'
import { useWorkflowStore } from '../../../stores/useWorkflowStore'
import { useAppStore } from '../../../stores'
import { useCapabilitiesStore } from '../../../stores/useCapabilitiesStore'
import { agentApi } from '../../../services/api'
import { isValidJSON } from '../../../utils/event-helpers'
import type { StepNodeData } from '../hooks/usePlanToFlow'
import type { ChangeType } from '../hooks/usePlanData'
import { getToolsByCategory } from '../../../utils/customToolNames'
import { NodeConfigFooter } from './NodeConfigFooter'

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

const providerBadgeStyles: Record<string, { bg: string; text: string }> = {
  anthropic: { bg: 'bg-orange-100 dark:bg-orange-900/40', text: 'text-orange-700 dark:text-orange-300' },
  openai: { bg: 'bg-emerald-100 dark:bg-emerald-900/40', text: 'text-emerald-700 dark:text-emerald-300' },
  openrouter: { bg: 'bg-purple-100 dark:bg-purple-900/40', text: 'text-purple-700 dark:text-purple-300' },
  bedrock: { bg: 'bg-amber-100 dark:bg-amber-900/40', text: 'text-amber-700 dark:text-amber-300' },
  vertex: { bg: 'bg-blue-100 dark:bg-blue-900/40', text: 'text-blue-700 dark:text-blue-300' }
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
    // Only count if it matches category AND is in the available tools list
    return parsed && parsed.category === category && parsed.tool !== '*' && allCategoryTools.includes(parsed.tool)
  }).length
  return { enabled, total: allCategoryTools.length }
}

export const StepNode = memo(({ data, selected }: StepNodeProps) => {
  const { id, title, status, stepIndex, changeType, step, onRunFromStep, onOpenSidebar, isExecuting, workspacePath, selectedRunFolder, routeName, routeCondition } = data
  
  const { availableLLMs, workflowPrimaryConfig, primaryConfig } = useLLMStore()
  const { capabilities } = useCapabilitiesStore()
  const { highlightFile, setShowFileContent, fetchFiles, setSelectedFile, setFileContent, setLoadingFileContent, setError } = useWorkspaceStore()
  const setWorkspaceMinimized = useAppStore(state => state.setWorkspaceMinimized)
  const layoutDirection = useWorkflowStore(state => state.layoutDirection)

  // Determine handle positions based on layout direction
  const isHorizontal = layoutDirection === 'LR'

  // Check if this is a sub-agent (part of a routing step)
  const isSubAgent = useMemo(() => id.includes('-sub-agent-'), [id])

  // Button is disabled if executing, no callback, or if it's a sub-agent (sub-agents cannot run independently)
  const isRunDisabled = isExecuting || !onRunFromStep || isSubAgent

  // Handle run from this step button click
  const handleRunClick = useCallback((e: MouseEvent) => {
    e.stopPropagation() // Prevent node selection and sidebar opening
    e.preventDefault() // Prevent any default behavior
    console.log('[StepNode] Run button clicked:', { stepIndex, stepId: step.id, onRunFromStep: !!onRunFromStep, isExecuting, isRunDisabled })
    if (onRunFromStep && !isExecuting) {
      console.log('[StepNode] Calling onRunFromStep with:', stepIndex, step.id || `step-${stepIndex}`)
      onRunFromStep(stepIndex, step.id || `step-${stepIndex}`)
    } else {
      console.warn('[StepNode] Cannot run step:', { 
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
    if (onOpenSidebar && typeof onOpenSidebar === 'function') {
      onOpenSidebar(id)
    }
  }, [onOpenSidebar, id])

  const activePresetId = useGlobalPresetStore(state => state.activePresetIds.workflow)
  const customPresets = useGlobalPresetStore(state => state.customPresets)
  const predefinedPresets = useGlobalPresetStore(state => state.predefinedPresets)
  
  const activePreset = activePresetId
    ? customPresets.find(p => p.id === activePresetId) || predefinedPresets.find(p => p.id === activePresetId)
    : null

  const stepConfig = step as { agent_configs?: {
    use_code_execution_mode?: boolean
    use_tool_search_mode?: boolean
    execution_llm?: { provider?: string; model_id?: string }
    execution_max_turns?: number
    learning_llm?: { provider?: string; model_id?: string }
    disable_learning?: boolean
    lock_learnings?: boolean
    learning_detail_level?: 'exact' | 'general'
    selected_servers?: string[]
    selected_tools?: string[]
    enabled_custom_tools?: string[]
    enable_context_offloading?: boolean
    enable_prerequisite_detection?: boolean
    prerequisite_rules?: Array<{ depends_on_step: string; description: string }>
    llm_validation_mode?: string
    pre_discovered_tools?: string[]
    disable_parallel_tool_execution?: boolean
  } }
  
  // Backward-compatible prerequisite flags/rules (agent_configs takes priority over top-level fields)
  type PrereqRule = { depends_on_step: string; description: string }
  const prerequisiteEnabled =
    stepConfig?.agent_configs?.enable_prerequisite_detection ??
    (step as unknown as { enable_prerequisite_detection?: boolean }).enable_prerequisite_detection
  const prerequisiteRules =
    (stepConfig?.agent_configs?.prerequisite_rules ??
      (step as unknown as { prerequisite_rules?: PrereqRule[] }).prerequisite_rules) as
      | PrereqRule[]
      | undefined
  
  // Get preset's default code execution mode
  const presetUseCodeExecutionMode = activePreset?.useCodeExecutionMode ?? false
  
  // Determine code execution mode: Priority - step config > preset default (matching backend logic)
  // Only use step-specific if it's EXPLICITLY set (not undefined)
  const stepCodeExecSetting = stepConfig?.agent_configs?.use_code_execution_mode
  const useCodeExecutionMode = stepCodeExecSetting !== undefined 
    ? stepCodeExecSetting === true  // Step has explicit setting
    : presetUseCodeExecutionMode     // Fall back to preset default

  // Get preset's default tool search mode
  const presetUseToolSearchMode = activePreset?.useToolSearchMode ?? false
  
  // Determine tool search mode: Priority - step config > preset default (matching backend logic)
  // Only use step-specific if it's EXPLICITLY set (not undefined)
  const stepToolSearchSetting = stepConfig?.agent_configs?.use_tool_search_mode
  const useToolSearchMode = stepToolSearchSetting !== undefined 
    ? stepToolSearchSetting === true  // Step has explicit setting
    : presetUseToolSearchMode         // Fall back to preset default

  const contextInputs = useMemo(() => step.context_dependencies || [], [step.context_dependencies])
  const contextOutputs = useMemo(() => {
    const output = step.context_output
    if (!output) return []
    return Array.isArray(output) ? output : [output]
  }, [step.context_output])

  const { executionLLM, executionProvider } = useMemo(() => {
    const presetLLMConfig = activePreset?.llmConfig
    const stepLLMConfig = stepConfig?.agent_configs?.execution_llm
    const presetExecutionLLM = presetLLMConfig?.execution_llm
    const presetDefaultLLM = presetLLMConfig?.provider && presetLLMConfig?.model_id
      ? { provider: presetLLMConfig.provider, model_id: presetLLMConfig.model_id } : null

    const llmConfig = stepLLMConfig || presetExecutionLLM || presetDefaultLLM

    // If we have a valid config, use it
    if (llmConfig?.provider && llmConfig?.model_id) {
      const llm = availableLLMs?.find(l => l.provider === llmConfig.provider && l.model === llmConfig.model_id)
      return {
        executionLLM: llm?.label || `${llmConfig.provider} ${llmConfig.model_id.split('-').slice(0, 2).join('-')}`,
        executionProvider: llmConfig.provider
      }
    }

    // Fallback: use workflow primary config from LLM store
    if (workflowPrimaryConfig?.provider) {
      const llm = availableLLMs?.find(l => l.provider === workflowPrimaryConfig.provider && l.model === workflowPrimaryConfig.model_id)
      return {
        executionLLM: llm?.label || (workflowPrimaryConfig.model_id ? `${workflowPrimaryConfig.provider} ${workflowPrimaryConfig.model_id.split('-').slice(0, 2).join('-')}` : null),
        executionProvider: workflowPrimaryConfig.provider
      }
    }

    // Last fallback: use legacy primary config
    if (primaryConfig?.provider) {
      const llm = availableLLMs?.find(l => l.provider === primaryConfig.provider && l.model === primaryConfig.model_id)
      return {
        executionLLM: llm?.label || (primaryConfig.model_id ? `${primaryConfig.provider} ${primaryConfig.model_id.split('-').slice(0, 2).join('-')}` : null),
        executionProvider: primaryConfig.provider
      }
    }

    return { executionLLM: null, executionProvider: null }
  }, [stepConfig?.agent_configs?.execution_llm, activePreset?.llmConfig, availableLLMs, workflowPrimaryConfig, primaryConfig])

  // Learning LLM: step config > preset learning_llm > preset default
  // Always use learning_llm config (not execution_llm), even in code exec mode
  const learningLLM = useMemo(() => {
    // Check if learning is disabled
    if (stepConfig?.agent_configs?.disable_learning === true) {
      return null
    }
    
    const presetLLMConfig = activePreset?.llmConfig
    const stepLLMConfig = stepConfig?.agent_configs?.learning_llm
    const presetLearningLLM = presetLLMConfig?.learning_llm
    const presetDefaultLLM = presetLLMConfig?.provider && presetLLMConfig?.model_id 
      ? { provider: presetLLMConfig.provider, model_id: presetLLMConfig.model_id } : null
    
    const llmConfig = stepLLMConfig || presetLearningLLM || presetDefaultLLM
    if (!llmConfig?.provider || !llmConfig?.model_id) return null
    
    const llm = availableLLMs?.find(l => l.provider === llmConfig.provider && l.model === llmConfig.model_id)
    return llm?.label || `${llmConfig.provider} ${llmConfig.model_id.split('-').slice(0, 2).join('-')}`
  }, [stepConfig?.agent_configs?.learning_llm, stepConfig?.agent_configs?.disable_learning, activePreset?.llmConfig, availableLLMs])

  // Learning detail level (defaults to 'exact', but 'exact' in code exec mode)
  const learningDetailLevel = useMemo(() => {
    if (stepConfig?.agent_configs?.disable_learning === true) {
      return null
    }
    // In code execution mode, learning is always 'exact'
    if (useCodeExecutionMode) {
      return 'exact'
    }
    return stepConfig?.agent_configs?.learning_detail_level || 'exact'
  }, [stepConfig?.agent_configs?.learning_detail_level, stepConfig?.agent_configs?.disable_learning, useCodeExecutionMode])

  // Lock learnings status
  const lockLearnings = useMemo(() => {
    return stepConfig?.agent_configs?.lock_learnings === true && stepConfig?.agent_configs?.disable_learning !== true
  }, [stepConfig?.agent_configs?.lock_learnings, stepConfig?.agent_configs?.disable_learning])

  // Execution max turns (defaults to 100)
  const executionMaxTurns = useMemo(() => {
    return stepConfig?.agent_configs?.execution_max_turns || 100
  }, [stepConfig?.agent_configs?.execution_max_turns])

  const presetServers = useMemo(() => activePreset?.selectedServers || [], [activePreset?.selectedServers])
  const stepServers = stepConfig?.agent_configs?.selected_servers
  const effectiveServers = useMemo(() => {
    // If step config explicitly sets servers (even if empty or NO_SERVERS), use it
    if (stepServers !== undefined && stepServers !== null) {
      // Filter out NO_SERVERS marker and return the result (empty array if only NO_SERVERS was present)
      return stepServers.filter(s => s !== 'NO_SERVERS')
    }
    // Otherwise, fall back to preset servers
    return presetServers
  }, [stepServers, presetServers])

  const presetTools = useMemo(() => activePreset?.selectedTools || [], [activePreset?.selectedTools])
  const effectiveTools = useMemo(() => {
    // If no servers are selected (NO_SERVERS or empty array), no tools should be shown
    if (effectiveServers.length === 0) {
      return []
    }
    // Otherwise, use step config tools or fall back to preset tools
    return stepConfig?.agent_configs?.selected_tools?.length 
      ? stepConfig.agent_configs.selected_tools 
      : presetTools
  }, [effectiveServers.length, stepConfig?.agent_configs?.selected_tools, presetTools])

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
  const hasLargeOutput = stepConfig?.agent_configs?.enable_context_offloading !== false // Default is enabled

  const hasContext = contextInputs.length > 0 || contextOutputs.length > 0

  // Handle file click - open file in workspace (same as workspace sidebar)
  const handleFileClick = useCallback(async (filename: string, e: MouseEvent) => {
    e.stopPropagation() // Prevent node selection
    
    // Don't open if no workspace path or run folder selected
    if (!workspacePath || !selectedRunFolder || selectedRunFolder === 'new') {
      console.warn('[StepNode] Cannot open file: missing workspacePath or selectedRunFolder')
      return
    }
    
    // Construct file path: {workspacePath}/runs/{selectedRunFolder}/execution/{filename}
    const filePath = `${workspacePath}/runs/${selectedRunFolder}/execution/${filename}`
    
    try {
      // Ensure workspace is visible
      setWorkspaceMinimized(false)
      
      // Clear any previous errors
      setError(null)
      
      // Set loading state
      setLoadingFileContent(true)
      
      // Set selected file
      const fileName = filePath.split('/').pop() || filePath
      setSelectedFile({ name: fileName, path: filePath })
      
      // Fetch file content (same as workspace sidebar)
      const response = await agentApi.getPlannerFileContent(filePath)
      
      if (response.success && response.data) {
        // Ensure content exists and is a string before processing
        const rawContent = response.data.content
        if (rawContent === undefined || rawContent === null) {
          setError(`File content is empty or unavailable: ${fileName}\n\nPath: ${filePath}`)
          setShowFileContent(false)
          setSelectedFile(null)
          setLoadingFileContent(false)
          return
        }
        
        let processedContent = typeof rawContent === 'string' ? rawContent : String(rawContent)
        let isJsonFile = false
        let formattedJson = null
        
        // Check if this is an image file
        if (response.data.is_image && processedContent && processedContent.startsWith('data:image/')) {
          // For images, the content is already base64 encoded data URL
          // No processing needed for images
        } else {
          // Process the content to convert escaped newlines to actual newlines
          processedContent = processedContent
            .replace(/\\n/g, '\n')  // Convert \n to actual newlines
            .replace(/\\t/g, '\t')  // Convert \t to actual tabs
            .replace(/\\r/g, '\r'); // Convert \r to actual carriage returns
          
          // Check if this is a JSON file (by extension OR content)
          const extensionIsJson = filePath.toLowerCase().endsWith('.json')
          const contentIsJson = isValidJSON(processedContent)
          isJsonFile = extensionIsJson || contentIsJson
          
          // If it's a JSON file, try to parse and format it
          if (isJsonFile) {
            try {
              const parsed = JSON.parse(processedContent)
              formattedJson = JSON.stringify(parsed, null, 2)
            } catch (parseError) {
              // If JSON parsing fails, keep the original content
              console.warn('Failed to parse JSON file:', parseError)
              formattedJson = null
            }
          }
        }
        
        // Store both original content and formatted JSON (if applicable)
        setFileContent(processedContent)
        if (formattedJson) {
          setFileContent(formattedJson)
        }
        
        // Refresh file tree to ensure file is available and highlight it
        await fetchFiles()
        setTimeout(() => {
          highlightFile(filePath)
        }, 200)
        
        setShowFileContent(true)
      } else {
        // File doesn't exist or failed to load
        const errorMessage = response.message || 'File not found'
        setError(`File not found: ${fileName}\n${errorMessage}\n\nPath: ${filePath}`)
        // Don't show file content panel if file doesn't exist
        setShowFileContent(false)
        // Clear selected file since it doesn't exist
        setSelectedFile(null)
      }
    } catch (error) {
      console.error('[StepNode] Error opening file:', error)
      const fileName = filePath.split('/').pop() || filePath
      const errorMessage = error instanceof Error ? error.message : 'Failed to fetch file content'
      setError(`Failed to open file: ${fileName}\n${errorMessage}\n\nPath: ${filePath}`)
      // Don't show file content panel on error
      setShowFileContent(false)
      setSelectedFile(null)
    } finally {
      setLoadingFileContent(false)
    }
  }, [workspacePath, selectedRunFolder, highlightFile, setShowFileContent, fetchFiles, setWorkspaceMinimized, setSelectedFile, setFileContent, setLoadingFileContent, setError])

  return (
    <div className={`
      relative w-[280px] rounded-xl border-2 bg-white dark:bg-gray-900 shadow-lg
      ${isSubAgent ? 'overflow-visible' : 'overflow-hidden'}
      ${statusBorderColors[status]}
      ${isSubAgent ? 'border-dashed border-indigo-400 dark:border-indigo-500' : ''}
      ${selected ? 'ring-2 ring-purple-500/60' : ''}
      ${changeType ? changeHighlightStyles[changeType] : ''}
    `}>
      {/* Status badge - positioned at top-right edge (shows for running/failed status) */}
      {status === 'running' && (
        <div className="absolute top-0 right-0 z-10 flex items-center gap-1 px-1.5 py-0.5 rounded-bl-lg rounded-tr-xl bg-blue-500 text-white text-[10px] font-medium shadow-lg">
          <Loader2 className="w-3 h-3 animate-spin" />
          <span>Running</span>
        </div>
      )}
      {status === 'failed' && (
        <div className="absolute top-0 right-0 z-10 flex items-center gap-1 px-1.5 py-0.5 rounded-bl-lg rounded-tr-xl bg-red-500 text-white text-[10px] font-medium shadow-lg">
          <XCircle className="w-3 h-3" />
          <span>Failed</span>
        </div>
      )}
      
      {/* Change badge - positioned below status badge (or top-right if no status badge) */}
      {changeType && (
        <div className={`absolute ${status === 'running' || status === 'failed' ? 'top-6 right-0' : 'top-0 right-0'} z-10 flex items-center gap-1 px-1.5 py-0.5 rounded-bl-lg rounded-tr-xl ${changeBadgeStyles[changeType].bg} text-white text-[10px] font-medium shadow-lg`}>
          {changeBadgeStyles[changeType].icon}
          <span className="capitalize">{changeType}</span>
        </div>
      )}

      <Handle type="target" position={Position.Left} className="!w-3 !h-3 !bg-gray-400 dark:!bg-gray-500 !border-2 !border-white dark:!border-gray-900" />
      
      {/* Top/Left handle for sub-agents (to receive connections from routing node) */}
      {/* In LR mode: receives from Right side of parent (so position on Left) */}
      {/* In TB mode: receives from Bottom side of parent (so position on Top) */}
      {/* Always create the handle but only show it for sub-agents to avoid React Flow warnings */}
      <Handle
        type="target"
        position={isHorizontal ? Position.Left : Position.Top}
        id="top"
        className={`!w-3 !h-3 !border-2 !border-white dark:!border-gray-900 ${isSubAgent ? '!bg-indigo-400 dark:!bg-indigo-600' : '!bg-transparent pointer-events-none opacity-0'}`}
        style={isHorizontal ? { left: '-6px', top: '30%' } : { top: '-6px', left: '50%' }}
      />

      {/* Header */}
      <div className="px-4 py-3 bg-gray-50 dark:bg-gray-800/50 border-b border-gray-200 dark:border-gray-700">
        {/* First row: Step number (or sub-agent indicator) and title */}
        <div className="flex items-start gap-3 mb-2">
          {isSubAgent ? (
            <div className="flex items-center justify-center w-8 h-8 rounded-md bg-indigo-100 dark:bg-indigo-900/40 text-indigo-700 dark:text-indigo-300 flex-shrink-0" title="Sub-agent">
              <Bot className="w-4 h-4" />
            </div>
          ) : (
            <div className="flex items-center justify-center w-8 h-8 rounded-md bg-blue-100 dark:bg-blue-900/40 text-blue-700 dark:text-blue-300 text-sm font-bold flex-shrink-0">
              {stepIndex + 1}
            </div>
          )}
          <div className="flex-1 min-w-0">
            <h3 className="text-sm font-semibold text-gray-900 dark:text-white leading-relaxed">
              {title}
            </h3>
          </div>
        </div>
        {/* Second row: Actions + Status */}
        <div className="flex items-center gap-1.5">
          {onRunFromStep ? (
            <button
              onClick={handleRunClick}
              disabled={isRunDisabled}
              className={`
                flex items-center justify-center w-7 h-7 rounded-md transition-all relative z-10
                ${isRunDisabled
                  ? 'bg-gray-200 dark:bg-gray-700 text-gray-400 cursor-not-allowed opacity-50'
                  : 'bg-emerald-500 dark:bg-emerald-600 text-white hover:bg-emerald-600 dark:hover:bg-emerald-500 hover:scale-105 cursor-pointer shadow-sm'
                }
              `}
              title={
                isExecuting
                  ? 'Execution in progress...'
                  : isSubAgent
                    ? 'Sub-agents cannot be run independently (run the parent routing step)'
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
          {onOpenSidebar ? (
            <button
              onClick={handleSettingsClick}
              className="flex items-center justify-center w-7 h-7 rounded-md transition-all relative z-10 bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400 hover:bg-gray-200 dark:hover:bg-gray-600 hover:scale-105 cursor-pointer"
              title="Open step settings"
            >
              <Settings className="w-3.5 h-3.5" />
            </button>
          ) : null}
          <div className="flex-1" />
          {statusIcons[status]}
        </div>
        {/* Third row: Model info + mode/config indicators */}
        <div className="flex items-center gap-2 mt-1.5">
          {executionProvider && executionLLM && (
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <span className={`text-[10px] font-medium truncate max-w-[140px] cursor-default ${providerBadgeStyles[executionProvider]?.text || 'text-gray-500 dark:text-gray-400'}`}>
                    {executionLLM}
                  </span>
                </TooltipTrigger>
                <TooltipContent side="bottom" className="max-w-xs">
                  <p className="font-medium">{executionLLM}</p>
                  <p className="text-xs text-gray-500">{executionProvider}</p>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          )}
          {/* Mode + config flag indicators */}
          {(() => {
            const flags: { icon: ReactElement; label: string; color: string }[] = []
            // Agent mode (always show one)
            if (useCodeExecutionMode) {
              flags.push({
                icon: <Terminal className="w-3 h-3" />,
                label: 'Code Execution Mode',
                color: 'text-amber-600 dark:text-amber-400',
              })
            } else if (useToolSearchMode) {
              flags.push({
                icon: <Search className="w-3 h-3" />,
                label: 'Tool Search Mode',
                color: 'text-yellow-600 dark:text-yellow-400',
              })
            } else {
              flags.push({
                icon: <Code className="w-3 h-3" />,
                label: 'Simple Agent Mode',
                color: 'text-slate-500 dark:text-slate-400',
              })
            }
            // Config flags
            if (prerequisiteEnabled) {
              flags.push({
                icon: <AlertTriangle className="w-3 h-3" />,
                label: prerequisiteRules && prerequisiteRules.length > 0
                  ? `Prerequisite detection (${prerequisiteRules.length} rule${prerequisiteRules.length > 1 ? 's' : ''})`
                  : 'Prerequisite detection',
                color: 'text-orange-500 dark:text-orange-400',
              })
            }
            if (stepConfig?.agent_configs?.lock_learnings && !stepConfig?.agent_configs?.disable_learning) {
              flags.push({
                icon: <Lock className="w-3 h-3" />,
                label: 'Learnings locked',
                color: 'text-purple-500 dark:text-purple-400',
              })
            }
            if (stepConfig?.agent_configs?.disable_parallel_tool_execution) {
              flags.push({
                icon: <Pause className="w-3 h-3" />,
                label: 'Parallel tool execution disabled',
                color: 'text-rose-500 dark:text-rose-400',
              })
            }
            if (stepConfig?.agent_configs?.llm_validation_mode === 'skip') {
              flags.push({
                icon: <SkipForward className="w-3 h-3" />,
                label: 'LLM validation skipped',
                color: 'text-indigo-500 dark:text-indigo-400',
              })
            }
            return (
              <TooltipProvider>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <div className="flex items-center gap-1 ml-auto cursor-default">
                      {flags.map((f, i) => (
                        <span key={i} className={f.color}>{f.icon}</span>
                      ))}
                    </div>
                  </TooltipTrigger>
                  <TooltipContent side="bottom" className="max-w-xs">
                    <div className="space-y-1">
                      {flags.map((f, i) => (
                        <div key={i} className="flex items-center gap-1.5 text-xs">
                          <span className={f.color}>{f.icon}</span>
                          <span>{f.label}</span>
                        </div>
                      ))}
                    </div>
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>
            )
          })()}
        </div>
      </div>

      {/* Content */}
      <div className="px-4 py-3 space-y-3">
        {/* Prerequisite Rules */}
        {prerequisiteEnabled && prerequisiteRules && prerequisiteRules.length > 0 && (
          <div className="space-y-2">
            {prerequisiteRules.map((rule, ruleIndex) => (
              <div key={ruleIndex} className="flex items-start gap-2 p-2.5 rounded-lg bg-orange-50 dark:bg-orange-900/20 border border-orange-200 dark:border-orange-800/50">
                <AlertTriangle className="w-3.5 h-3.5 text-orange-500 mt-0.5 flex-shrink-0" />
                <div className="flex-1">
                  <div className="text-[10px] font-semibold text-orange-700 dark:text-orange-300 mb-1">
                    Rule {ruleIndex + 1}: Depends on {rule.depends_on_step}
                  </div>
                  {rule.description && (
                    <div className="mt-1 text-[10px] text-orange-600 dark:text-orange-400 italic leading-relaxed">
                      "{rule.description}"
                    </div>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}

        {/* Context Files */}
        {hasContext && (
          <div className="space-y-1.5">
            {contextInputs.length > 0 && (
              <div className="flex items-start gap-2">
                <ArrowDownToLine className="w-3.5 h-3.5 text-blue-500 mt-0.5 flex-shrink-0" />
                <div className="flex flex-wrap gap-1">
                  {contextInputs.map((f, i) => {
                    const fileName = getFileName(f)
                    const canOpen = workspacePath && selectedRunFolder && selectedRunFolder !== 'new'
                    return (
                      <span
                        key={i}
                        onClick={canOpen ? (e) => handleFileClick(f, e) : undefined}
                        className={`
                          px-1.5 py-0.5 rounded text-[10px] bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300
                          ${canOpen ? 'cursor-pointer hover:bg-blue-200 dark:hover:bg-blue-900/50 hover:underline' : ''}
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

      {/* Config Footer */}
      <NodeConfigFooter
        description={step.description}
        successCriteria={step.success_criteria}
        routeName={routeName}
        routeCondition={routeCondition}
        executionLLM={executionLLM}
        executionMaxTurns={executionMaxTurns}
        learningLLM={learningLLM}
        learningDetailLevel={learningDetailLevel}
        lockLearnings={lockLearnings}
        effectiveServers={effectiveServers}
        toolsDisplayInfo={toolsDisplayInfo}
        workspaceToolsInfo={workspaceToolsInfo}
        hasWorkspaceTools={hasWorkspaceTools}
        humanToolsInfo={humanToolsInfo}
        hasHumanTools={hasHumanTools}
        hasLargeOutput={hasLargeOutput}
        useCodeExecutionMode={useCodeExecutionMode}
        useToolSearchMode={useToolSearchMode}
        preDiscoveredTools={stepConfig?.agent_configs?.pre_discovered_tools}
      />

      <Handle type="source" position={Position.Right} className="!w-3 !h-3 !bg-gray-400 dark:!bg-gray-500 !border-2 !border-white dark:!border-gray-900" />
      
      {/* Prerequisite source handles (bottom, for edges going back to previous steps when prerequisite is detected during execution) */}
      {/* Hidden by default, only functional for edge connections */}
      <Handle
        type="source"
        position={Position.Bottom}
        id="prereq-left"
        style={{ left: '25%' }}
        className="!w-2 !h-2 !bg-transparent !border-0"
      />
      <Handle
        type="source"
        position={Position.Bottom}
        id="prereq-middle"
        style={{ left: '50%' }}
        className="!w-2 !h-2 !bg-transparent !border-0"
      />
      <Handle
        type="source"
        position={Position.Bottom}
        id="prereq-right"
        style={{ left: '75%' }}
        className="!w-2 !h-2 !bg-transparent !border-0"
      />

      {/* Prerequisite target handles (bottom, for edges coming from step/learning nodes when prerequisite is detected during execution) */}
      {/* Hidden by default, only functional for edge connections */}
      <Handle
        type="target"
        position={Position.Bottom}
        id="prereq-target-left"
        style={{ left: '25%' }}
        className="!w-2 !h-2 !bg-transparent !border-0"
      />
      <Handle
        type="target"
        position={Position.Bottom}
        id="prereq-target-middle"
        style={{ left: '50%' }}
        className="!w-2 !h-2 !bg-transparent !border-0"
      />
      <Handle
        type="target"
        position={Position.Bottom}
        id="prereq-target-right"
        style={{ left: '75%' }}
        className="!w-2 !h-2 !bg-transparent !border-0"
      />

      {/* Retry Handle - for validation loop-back (hidden by default) */}
      <Handle
        type="target"
        position={Position.Top}
        id="retry"
        className="!w-2 !h-2 !bg-transparent !border-0"
      />
    </div>
  )
})

StepNode.displayName = 'StepNode'
export default StepNode
