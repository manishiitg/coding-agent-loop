import { memo, useCallback, useMemo, type ReactElement, type MouseEvent } from 'react'
import { Handle, Position } from '@xyflow/react'
import { CheckCircle, XCircle, Loader2, Plus, RefreshCw, Play, Settings, Code, Terminal, AlertTriangle, Lock, ArrowDownToLine, ArrowUpFromLine, GitBranch } from 'lucide-react'
import { useGlobalPresetStore } from '../../../stores/useGlobalPresetStore'
import { useLLMStore } from '../../../stores/useLLMStore'
import { useWorkspaceStore } from '../../../stores/useWorkspaceStore'
import { useAppStore } from '../../../stores'
import { agentApi } from '../../../services/api'
import { isValidJSON } from '../../../utils/event-helpers'
import { getToolsByCategory } from '../../../utils/customToolNames'
import { NodeConfigFooter } from './NodeConfigFooter'
import type { RoutingNodeData } from '../hooks/usePlanToFlow'
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
    return parsed && parsed.category === category && parsed.tool !== '*'
  }).length
  return { enabled, total: allCategoryTools.length }
}

interface RoutingNodeProps {
  data: RoutingNodeData
  selected?: boolean
}

const statusBorderColors: Record<string, string> = {
  pending: 'border-gray-300 dark:border-gray-600',
  executing: 'border-blue-500 dark:border-blue-600',
  evaluating: 'border-blue-500 dark:border-blue-600',
  routing: 'border-blue-500 dark:border-blue-600',
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
  executing: <Loader2 className="w-4 h-4 text-blue-500 dark:text-blue-400 animate-spin" />,
  evaluating: <Loader2 className="w-4 h-4 text-blue-500 dark:text-blue-400 animate-spin" />,
  routing: <Loader2 className="w-4 h-4 text-blue-500 dark:text-blue-400 animate-spin" />,
  completed: <CheckCircle className="w-4 h-4 text-green-500" />
}

export const RoutingNode = memo(({ data, selected }: RoutingNodeProps) => {
  const { id, title, orchestration_step, orchestration_routes, status, stepIndex, changeType, step, onRunFromStep, onOpenSidebar, isExecuting, canRun, workspacePath, selectedRunFolder } = data
  const { highlightFile, setShowFileContent, fetchFiles, setSelectedFile, setFileContent, setLoadingFileContent, setError } = useWorkspaceStore()
  const { setWorkspaceMinimized } = useAppStore()
  
  // Extract description and success criteria from step and orchestration_step
  const routingDescription = step?.description
  const routingSuccessCriteria = step?.success_criteria
  const mainStepDescription = orchestration_step?.description
  const mainStepSuccessCriteria = orchestration_step?.success_criteria

  // Context inputs and outputs from the MAIN STEP (orchestration_step) - this is what actually executes
  const contextInputs = useMemo(() => orchestration_step?.context_dependencies || [], [orchestration_step?.context_dependencies])
  const contextOutputs = useMemo(() => {
    const output = orchestration_step?.context_output
    if (!output) return []
    return Array.isArray(output) ? output : [output]
  }, [orchestration_step?.context_output])
  const hasContext = contextInputs.length > 0 || contextOutputs.length > 0

  // Get preset for config badges
  const activePresetId = useGlobalPresetStore(state => state.activePresetIds.workflow)
  const customPresets = useGlobalPresetStore(state => state.customPresets)
  const predefinedPresets = useGlobalPresetStore(state => state.predefinedPresets)
  
  const activePreset = activePresetId
    ? customPresets.find(p => p.id === activePresetId) || predefinedPresets.find(p => p.id === activePresetId)
    : null

  const { availableLLMs } = useLLMStore()

  // Get step config (agent_configs)
  const stepConfig = step as { agent_configs?: { 
    use_code_execution_mode?: boolean
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
    enable_large_output_virtual_tools?: boolean
  } }

  // Determine code execution mode: step config > preset default
  const presetUseCodeExecutionMode = activePreset?.useCodeExecutionMode ?? false
  const stepCodeExecSetting = stepConfig?.agent_configs?.use_code_execution_mode
  const useCodeExecutionMode = stepCodeExecSetting !== undefined 
    ? stepCodeExecSetting === true
    : presetUseCodeExecutionMode

  // Execution LLM: step config > preset execution_llm > preset default
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

  // Conditional LLM (for routing evaluation): step config > execution LLM > preset default
  const conditionalLLM = useMemo(() => {
    const presetLLMConfig = activePreset?.llmConfig
    const stepConditionalLLM = stepConfig?.agent_configs?.conditional_llm
    const presetExecutionLLM = presetLLMConfig?.execution_llm
    const presetDefaultLLM = presetLLMConfig?.provider && presetLLMConfig?.model_id 
      ? { provider: presetLLMConfig.provider, model_id: presetLLMConfig.model_id } : null
    
    const llmConfig = stepConditionalLLM || presetExecutionLLM || presetDefaultLLM
    if (!llmConfig?.provider || !llmConfig?.model_id) return null
    
    const llm = availableLLMs?.find(l => l.provider === llmConfig.provider && l.model === llmConfig.model_id)
    return llm?.label || `${llmConfig.provider} ${llmConfig.model_id.split('-').slice(0, 2).join('-')}`
  }, [stepConfig?.agent_configs?.conditional_llm, activePreset?.llmConfig, availableLLMs])

  // Learning LLM: step config > preset learning_llm > preset default
  const learningLLM = useMemo(() => {
    const presetLLMConfig = activePreset?.llmConfig
    const stepLearningLLM = stepConfig?.agent_configs?.learning_llm
    const presetLearningLLM = presetLLMConfig?.learning_llm
    const presetDefaultLLM = presetLLMConfig?.provider && presetLLMConfig?.model_id 
      ? { provider: presetLLMConfig.provider, model_id: presetLLMConfig.model_id } : null
    
    const llmConfig = stepLearningLLM || presetLearningLLM || presetDefaultLLM
    if (!llmConfig?.provider || !llmConfig?.model_id) return null
    
    const llm = availableLLMs?.find(l => l.provider === llmConfig.provider && l.model === llmConfig.model_id)
    return llm?.label || `${llmConfig.provider} ${llmConfig.model_id.split('-').slice(0, 2).join('-')}`
  }, [stepConfig?.agent_configs?.learning_llm, activePreset?.llmConfig, availableLLMs])

  // Learning detail level
  const learningDetailLevel = useMemo(() => {
    return stepConfig?.agent_configs?.learning_detail_level || 'general'
  }, [stepConfig?.agent_configs?.learning_detail_level])

  // Lock learnings
  const lockLearnings = useMemo(() => {
    return stepConfig?.agent_configs?.lock_learnings || false
  }, [stepConfig?.agent_configs?.lock_learnings])

  // Execution max turns
  const executionMaxTurns = useMemo(() => {
    return stepConfig?.agent_configs?.execution_max_turns || 100
  }, [stepConfig?.agent_configs?.execution_max_turns])

  // MCP Servers: step config > preset
  const presetServers = useMemo(() => activePreset?.selectedServers || [], [activePreset?.selectedServers])
  const stepServers = stepConfig?.agent_configs?.selected_servers
  const effectiveServers = useMemo(() => {
    if (stepServers !== undefined && stepServers !== null) {
      return stepServers.filter(s => s !== 'NO_SERVERS')
    }
    return presetServers
  }, [stepServers, presetServers])

  // Tools: step config > preset
  const presetTools = useMemo(() => activePreset?.selectedTools || [], [activePreset?.selectedTools])
  const effectiveTools = useMemo(() => {
    if (effectiveServers.length === 0) {
      return []
    }
    return stepConfig?.agent_configs?.selected_tools?.length 
      ? stepConfig.agent_configs.selected_tools 
      : presetTools
  }, [effectiveServers.length, stepConfig?.agent_configs?.selected_tools, presetTools])

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

  // Parse custom tools
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
  const hasLargeOutput = stepConfig?.agent_configs?.enable_large_output_virtual_tools !== false

  // Button states
  const isRunDisabled = isExecuting || !canRun || !onRunFromStep

  // Handle run from this step button click
  const handleRunClick = useCallback((e: MouseEvent) => {
    e.stopPropagation()
    e.preventDefault()
    if (onRunFromStep && !isExecuting && canRun) {
      onRunFromStep(stepIndex, step.id || `step-${stepIndex}`)
    }
  }, [onRunFromStep, isExecuting, canRun, stepIndex, step.id])

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

  // Handle file click - open file in workspace (same as DecisionNode)
  const handleFileClick = useCallback(async (filename: string, e: MouseEvent) => {
    e.stopPropagation() // Prevent node selection
    
    // Don't open if no workspace path or run folder selected
    if (!workspacePath || !selectedRunFolder || selectedRunFolder === 'new') {
      console.warn('[RoutingNode] Cannot open file: missing workspacePath or selectedRunFolder')
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
      console.error('[RoutingNode] Error opening file:', error)
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

  // Calculate node height based on content
  const nodeHeight = useMemo(() => {
    let height = 120 // Base height
    if (routingDescription) height += 30
    if (routingSuccessCriteria) height += 50
    if (orchestration_step) height += 40
    if (hasContext) height += 40
    return Math.max(height, 200) // Minimum height
  }, [orchestration_step, routingDescription, routingSuccessCriteria, hasContext])

  return (
    <div className={`relative w-[360px] ${changeType ? changeHighlightStyles[changeType] : ''}`}>
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
          <div className="flex items-center gap-1 px-2 py-1 rounded-md bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-300 text-[10px] font-semibold border border-amber-200 dark:border-amber-800">
            <Terminal className="w-3 h-3" />
            <span>Code</span>
          </div>
        ) : (
          <div className="flex items-center gap-1 px-2 py-1 rounded-md bg-slate-100 dark:bg-slate-800/60 text-slate-700 dark:text-slate-300 text-[10px] font-semibold border border-slate-200 dark:border-slate-700">
            <Code className="w-3 h-3" />
            <span>Agent</span>
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
        {stepConfig?.agent_configs?.lock_learnings && !stepConfig?.agent_configs?.disable_learning && (
          <div 
            className="flex items-center gap-1 px-2 py-1 rounded-md bg-purple-100 dark:bg-purple-900/40 text-purple-700 dark:text-purple-300 text-[10px] font-semibold border border-purple-200 dark:border-purple-800"
            title="Learnings are locked - learning agent will not run but existing learnings will be used"
          >
            <Lock className="w-3 h-3" />
            <span>Locked</span>
          </div>
        )}
      </div>

      {/* Orchestrator Badge - Top */}
      <div className="absolute -top-2.5 left-1/2 -translate-x-1/2 z-10 flex items-center gap-1.5 px-2.5 py-1 rounded-full bg-blue-600 dark:bg-blue-700 text-white text-[11px] font-semibold shadow-lg">
        <GitBranch className="w-3.5 h-3.5" />
        <span>Orchestrator</span>
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
          ${selected ? 'ring-2 ring-blue-500/40' : ''}
          ${status === 'executing' || status === 'evaluating' || status === 'routing' ? 'animate-pulse' : ''}
        `}
        style={{
          minHeight: `${nodeHeight}px`,
          width: '360px'
        }}
      >
        {/* Input handle */}
        <Handle 
          type="target" 
          position={Position.Left} 
          className="!w-3 !h-3 !bg-blue-500 dark:!bg-blue-600 !border-2 !border-white dark:!border-gray-900"
          style={{ left: '-6px', top: '50%' }}
        />

        {/* Content */}
        <div className="flex flex-col px-4 py-4">
          <div className="flex items-center gap-1.5 mb-2 justify-center">
            {statusIcons[status]}
          </div>
          <h3 className="text-sm font-semibold text-gray-900 dark:text-white leading-tight text-center mb-1.5">
            {title || `Orchestrator ${stepIndex + 1}`}
          </h3>
          
          {/* Routing step description (main step) */}
          {routingDescription && (
            <p className="text-xs text-gray-600 dark:text-gray-400 leading-relaxed mb-2 px-1">
              {routingDescription}
            </p>
          )}
          
          {/* Routing step success criteria (main step) */}
          {routingSuccessCriteria && (
            <div className="flex gap-2 p-2.5 rounded-lg bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800/50 mb-2">
              <CheckCircle className="w-4 h-4 text-green-500 flex-shrink-0 mt-0.5" />
              <p className="text-xs text-green-700 dark:text-green-300 leading-relaxed">
                {routingSuccessCriteria}
              </p>
            </div>
          )}
          
          {/* Main orchestrator step info */}
          {orchestration_step && (
            <div className="mt-1.5 p-2.5 rounded-lg bg-gray-50 dark:bg-gray-800/50 border border-gray-200 dark:border-gray-700/60">
              <p className="text-[10px] text-gray-700 dark:text-gray-300 font-semibold mb-1.5">
                Orchestrator: {orchestration_step.title || 'Untitled Step'}
              </p>
              {/* Main step description - REQUIRED */}
              {mainStepDescription ? (
                <p className="text-[10px] text-gray-600 dark:text-gray-400 leading-relaxed">
                  {mainStepDescription}
                </p>
              ) : (
                <p className="text-[10px] text-red-600 dark:text-red-400 italic">
                  ⚠️ Description is required
                </p>
              )}
              {/* Main step success criteria - REQUIRED */}
              {mainStepSuccessCriteria ? (
                <div className="flex gap-1.5 mt-2 p-2 rounded-lg bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800/50">
                  <CheckCircle className="w-3.5 h-3.5 text-green-500 flex-shrink-0 mt-0.5" />
                  <p className="text-[10px] text-green-700 dark:text-green-300 leading-relaxed">
                    {mainStepSuccessCriteria}
                  </p>
                </div>
              ) : (
                <div className="flex gap-1.5 mt-2 p-2 rounded-lg bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800/50">
                  <AlertTriangle className="w-3.5 h-3.5 text-red-500 flex-shrink-0 mt-0.5" />
                  <p className="text-[10px] text-red-700 dark:text-red-300 leading-relaxed italic">
                    ⚠️ Success criteria is required
                  </p>
                </div>
              )}
            </div>
          )}

          {/* Context Files - from main step (orchestration_step) */}
          {hasContext && (
            <div className="space-y-1.5 mt-2">
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

        {/* Output handles - one for each route (or single handle if no routes) */}
        {orchestration_routes && orchestration_routes.length > 0 ? (
          orchestration_routes.map((route, index) => {
            const totalRoutes = orchestration_routes.length
            const positionPercent = totalRoutes === 1 
              ? 50 
              : 20 + (index * (60 / (totalRoutes - 1))) // Distribute handles from 20% to 80%
            return (
              <Handle 
                key={route.route_id}
                type="source" 
                position={Position.Bottom} 
                id={route.route_id}
                className="!w-3 !h-3 !bg-blue-500 dark:!bg-blue-600 !border-2 !border-white dark:!border-gray-900 !shadow-md"
                style={{ left: `${positionPercent}%`, bottom: '-6px' }}
              />
            )
          })
        ) : (
          <Handle 
            type="source" 
            position={Position.Bottom} 
            className="!w-3 !h-3 !bg-blue-500 dark:!bg-blue-600 !border-2 !border-white dark:!border-gray-900 !shadow-md"
            style={{ left: '50%', bottom: '-6px' }}
          />
        )}
      </div>

      {/* Config Footer */}
      <div className="mt-2 mx-4">
        <NodeConfigFooter
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
        />
        {/* Conditional/Evaluation LLM badge */}
        {conditionalLLM && (
          <div className="px-3 py-2 bg-gray-50 dark:bg-gray-800/30 border-t border-gray-200 dark:border-gray-700 rounded-b-lg">
            <div className="flex flex-wrap gap-1.5 justify-center">
              <span className="px-2 py-0.5 rounded-md text-[10px] font-medium bg-gray-100 dark:bg-gray-800/50 text-gray-700 dark:text-gray-300 border border-gray-200 dark:border-gray-700/60" title="LLM used for routing evaluation">
                Eval: {conditionalLLM}
              </span>
            </div>
          </div>
        )}
      </div>
    </div>
  )
})

RoutingNode.displayName = 'RoutingNode'
export default RoutingNode

