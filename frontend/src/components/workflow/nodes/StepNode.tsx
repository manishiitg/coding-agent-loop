import { memo, useMemo, useCallback, type ReactElement, type MouseEvent } from 'react'
import { Handle, Position } from '@xyflow/react'
import { CheckCircle, XCircle, Loader2, Plus, RefreshCw, Code, Terminal, ArrowDownToLine, ArrowUpFromLine, Settings, Play, AlertTriangle, Lock, SkipForward, Search, Bot } from 'lucide-react'
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

// Provider icons and styles
const ProviderIcon = ({ provider }: { provider: string }) => {
  switch (provider) {
    case 'anthropic':
      // Anthropic/Claude - stylized "A" shape
      return (
        <svg viewBox="0 0 24 24" className="w-4 h-4" fill="currentColor">
          <path d="M12 2L2 22h4l2-4h8l2 4h4L12 2zm0 6l3 8H9l3-8z"/>
        </svg>
      )
    case 'openai':
      // OpenAI - hexagon shape
      return (
        <svg viewBox="0 0 24 24" className="w-4 h-4" fill="currentColor">
          <path d="M22.282 9.821a5.985 5.985 0 0 0-.516-4.91 6.046 6.046 0 0 0-6.51-2.9A6.065 6.065 0 0 0 4.981 4.18a5.985 5.985 0 0 0-3.998 2.9 6.046 6.046 0 0 0 .743 7.097 5.98 5.98 0 0 0 .51 4.911 6.051 6.051 0 0 0 6.515 2.9A5.985 5.985 0 0 0 13.26 24a6.056 6.056 0 0 0 5.772-4.206 5.99 5.99 0 0 0 3.997-2.9 6.056 6.056 0 0 0-.747-7.073zM13.26 22.43a4.476 4.476 0 0 1-2.876-1.04l.141-.081 4.779-2.758a.795.795 0 0 0 .392-.681v-6.737l2.02 1.168a.071.071 0 0 1 .038.052v5.583a4.504 4.504 0 0 1-4.494 4.494zM3.6 18.304a4.47 4.47 0 0 1-.535-3.014l.142.085 4.783 2.759a.771.771 0 0 0 .78 0l5.843-3.369v2.332a.08.08 0 0 1-.033.062L9.74 19.95a4.5 4.5 0 0 1-6.14-1.646zM2.34 7.896a4.485 4.485 0 0 1 2.366-1.973V11.6a.766.766 0 0 0 .388.676l5.815 3.355-2.02 1.168a.076.076 0 0 1-.071 0l-4.83-2.786A4.504 4.504 0 0 1 2.34 7.872zm16.597 3.855l-5.833-3.387L15.119 7.2a.076.076 0 0 1 .071 0l4.83 2.791a4.494 4.494 0 0 1-.676 8.105v-5.678a.79.79 0 0 0-.407-.667zm2.01-3.023l-.141-.085-4.774-2.782a.776.776 0 0 0-.785 0L9.409 9.23V6.897a.066.066 0 0 1 .028-.061l4.83-2.787a4.5 4.5 0 0 1 6.68 4.66zm-12.64 4.135l-2.02-1.164a.08.08 0 0 1-.038-.057V6.075a4.5 4.5 0 0 1 7.375-3.453l-.142.08-4.778 2.758a.795.795 0 0 0-.393.681zm1.097-2.365l2.602-1.5 2.607 1.5v2.999l-2.597 1.5-2.607-1.5z"/>
        </svg>
      )
    case 'openrouter':
      // OpenRouter - router/network symbol
      return (
        <svg viewBox="0 0 24 24" className="w-4 h-4" fill="currentColor">
          <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-1 17.93c-3.95-.49-7-3.85-7-7.93 0-.62.08-1.21.21-1.79L9 15v1c0 1.1.9 2 2 2v1.93zm6.9-2.54c-.26-.81-1-1.39-1.9-1.39h-1v-3c0-.55-.45-1-1-1H8v-2h2c.55 0 1-.45 1-1V7h2c1.1 0 2-.9 2-2v-.41c2.93 1.19 5 4.06 5 7.41 0 2.08-.8 3.97-2.1 5.39z"/>
        </svg>
      )
    case 'bedrock':
      // AWS Bedrock - AWS-style cloud/cube
      return (
        <svg viewBox="0 0 24 24" className="w-4 h-4" fill="currentColor">
          <path d="M12 2L2 7v10l10 5 10-5V7L12 2zm0 2.18l6.9 3.45L12 11.09 5.1 7.63 12 4.18zM4 8.82l7 3.5v7.36l-7-3.5V8.82zm9 10.86v-7.36l7-3.5v7.36l-7 3.5z"/>
        </svg>
      )
    case 'vertex':
      // Google Gemini - sparkle/star shape
      return (
        <svg viewBox="0 0 24 24" className="w-4 h-4" fill="currentColor">
          <path d="M12 0c.514 4.148 2.852 7.486 7 8-.514 4.148-2.852 7.486-7 8 .514-4.148 2.852-7.486 7-8-.514-4.148-2.852-7.486-7-8zM5 8c.257 2.074 1.426 3.743 3.5 4-.257 2.074-1.426 3.743-3.5 4 .257-2.074 1.426-3.743 3.5-4-.257-2.074-1.426-3.743-3.5-4z"/>
        </svg>
      )
    default:
      // Fallback: show first 2 letters of provider name
      return (
        <span className="text-[10px] font-bold uppercase">
          {provider?.slice(0, 2) || '?'}
        </span>
      )
  }
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
      ${isSubAgent ? 'border-dashed border-cyan-400 dark:border-cyan-500' : ''}
      ${selected ? 'ring-2 ring-blue-500/40' : ''}
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
        className={`!w-3 !h-3 !border-2 !border-white dark:!border-gray-900 ${isSubAgent ? '!bg-cyan-400 dark:!bg-cyan-600' : '!bg-transparent pointer-events-none opacity-0'}`}
        style={isHorizontal ? { left: '-6px', top: '30%' } : { top: '-6px', left: '50%' }}
      />

      {/* Header */}
      <div className="px-4 py-3 bg-gray-50 dark:bg-gray-800/50 border-b border-gray-200 dark:border-gray-700">
        {/* First row: Step number (or sub-agent indicator) and title */}
        <div className="flex items-start gap-3 mb-2">
          {isSubAgent ? (
            <div className="flex items-center justify-center w-8 h-8 rounded-md bg-cyan-100 dark:bg-cyan-900/40 text-cyan-700 dark:text-cyan-300 flex-shrink-0" title="Sub-agent">
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
        {/* Second row: Action buttons */}
        <div className="flex items-center gap-1.5">
          {/* Run from this step button */}
          {onRunFromStep ? (
            <button
              onClick={handleRunClick}
              disabled={isRunDisabled}
              className={`
                flex items-center justify-center w-8 h-8 rounded-md transition-all relative z-10
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
              <Play className="w-4 h-4" />
            </button>
          ) : (
            <div className="w-8 h-8 flex items-center justify-center text-xs text-gray-400" title="Run callback not available">
              ⚠️
            </div>
          )}
          {/* Settings icon button - opens sidebar */}
          {onOpenSidebar ? (
            <button
              onClick={handleSettingsClick}
              className="flex items-center justify-center w-8 h-8 rounded-md transition-all relative z-10 bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400 hover:bg-gray-200 dark:hover:bg-gray-600 hover:scale-105 cursor-pointer"
              title="Open step settings"
            >
              <Settings className="w-4 h-4" />
            </button>
          ) : null}
          {/* Agent Mode Badge */}
          {useCodeExecutionMode ? (
            <div className="flex items-center justify-center w-8 h-8 rounded-md bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-300 border border-amber-200 dark:border-amber-800" title="Code Execution Mode">
              <Terminal className="w-4 h-4" />
            </div>
          ) : useToolSearchMode ? (
            <div className="flex items-center justify-center w-8 h-8 rounded-md bg-yellow-100 dark:bg-yellow-900/40 text-yellow-700 dark:text-yellow-300 border border-yellow-200 dark:border-yellow-800" title="Tool Search Mode">
              <Search className="w-4 h-4" />
            </div>
          ) : (
            <div className="flex items-center justify-center w-8 h-8 rounded-md bg-slate-100 dark:bg-slate-800/60 text-slate-700 dark:text-slate-300 border border-slate-200 dark:border-slate-700" title="Simple Agent Mode">
              <Code className="w-4 h-4" />
            </div>
          )}
          {/* LLM Provider Badge */}
          {executionProvider && (
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <div
                    className={`flex items-center justify-center w-8 h-8 rounded-md cursor-default ${providerBadgeStyles[executionProvider]?.bg || 'bg-gray-100 dark:bg-gray-800'} ${providerBadgeStyles[executionProvider]?.text || 'text-gray-600 dark:text-gray-400'} border border-gray-200 dark:border-gray-700`}
                  >
                    <ProviderIcon provider={executionProvider} />
                  </div>
                </TooltipTrigger>
                <TooltipContent side="bottom" className="max-w-xs">
                  <p className="font-medium">{executionProvider}</p>
                  {executionLLM && <p className="text-xs text-gray-500">{executionLLM}</p>}
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          )}
          {/* Prerequisite Detection Badge */}
          {prerequisiteEnabled && (
            <div 
              className="flex items-center gap-1 px-2.5 py-1.5 rounded-md bg-orange-100 dark:bg-orange-900/40 text-orange-700 dark:text-orange-300 text-[10px] font-semibold border border-orange-200 dark:border-orange-800"
              title={
                prerequisiteRules && prerequisiteRules.length > 0
                  ? `Prerequisite detection enabled. ${prerequisiteRules.length} rule(s) configured`
                  : 'Prerequisite detection enabled'
              }
            >
              <AlertTriangle className="w-3.5 h-3.5" />
              <span>Prereq</span>
            </div>
          )}
          {/* Lock Learnings Badge */}
          {stepConfig?.agent_configs?.lock_learnings && !stepConfig?.agent_configs?.disable_learning && (
            <div 
              className="flex items-center justify-center w-8 h-8 rounded-md bg-purple-100 dark:bg-purple-900/40 text-purple-700 dark:text-purple-300 border border-purple-200 dark:border-purple-800"
              title="Learnings are locked - learning agent will not run but existing learnings will be used"
            >
              <Lock className="w-3.5 h-3.5" />
            </div>
          )}
          {/* Validation Skipped Badge */}
          {stepConfig?.agent_configs?.llm_validation_mode === 'skip' && (
            <div 
              className="flex items-center justify-center w-8 h-8 rounded-md bg-cyan-100 dark:bg-cyan-900/40 text-cyan-700 dark:text-cyan-400 border border-cyan-200 dark:border-cyan-800"
              title="LLM validation will be skipped if pre-validation passes"
            >
              <SkipForward className="w-3.5 h-3.5" />
            </div>
          )}
          {statusIcons[status]}
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
