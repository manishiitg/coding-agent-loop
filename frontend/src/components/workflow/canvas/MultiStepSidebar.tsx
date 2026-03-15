import React, { useState, useMemo, useCallback, useEffect } from 'react'
import { X, Settings, ChevronDown, ChevronUp, Sparkles, Code2, Search, Loader2 } from 'lucide-react'
import LLMSelectionDropdown from '../../LLMSelectionDropdown'
import { ToolSelectionSection } from '../../ToolSelectionSection'
import { Button } from '../../ui/Button'
import { useGlobalPresetStore, usePresetApplication } from '../../../stores/useGlobalPresetStore'
import { useLLMStore } from '../../../stores/useLLMStore'
import { useCapabilitiesStore } from '../../../stores/useCapabilitiesStore'
import { Tooltip, TooltipContent, TooltipTrigger, TooltipProvider } from '../../ui/tooltip'
import type { LLMOption } from '../../../types/llm'
import type { AgentLLMConfig, AgentConfigs, PlanStep, PlanningResponse } from '../../../utils/stepConfigMatching'
import { isConditionalStep, isDecisionStep, isTodoTaskStep } from '../../../utils/stepConfigMatching'
import { getToolsByCategory, getCategoryForTool, HUMAN_TOOLS } from '../../../utils/customToolNames'

// Sub-categories that belong to workspace_tools parent
const WORKSPACE_SUB_CATEGORIES = ['workspace_advanced']

interface MultiStepSidebarProps {
  selectedStepIds: string[]
  plan: PlanningResponse | null
  onClose: () => void
  onBulkUpdate: (updates: Array<{ stepId: string; updates: Partial<PlanStep> }>) => Promise<void>
  isCompact?: boolean
  showChatArea?: boolean
}

const MAX_TURNS_OPTIONS = [10, 25, 50, 75, 100] as const

export const MultiStepSidebar: React.FC<MultiStepSidebarProps> = ({
  selectedStepIds,
  plan,
  onClose,
  onBulkUpdate,
  isCompact = false,
}) => {
  const { availableLLMs, getCurrentLLMOption } = useLLMStore()
  const { capabilities } = useCapabilitiesStore()
  const { currentPresetTools } = usePresetApplication()
  const [isExpanded, setIsExpanded] = useState(true)
  const [isSaving, setIsSaving] = useState(false)

  // Get preset information
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

  const presetServers = useMemo(() => activePreset?.selectedServers || [], [activePreset])
  const presetLLMConfig = useMemo(() => activePreset?.llmConfig || null, [activePreset])
  const presetUseCodeExecutionMode = useMemo(() => activePreset?.useCodeExecutionMode ?? false, [activePreset])

  // Local state for configuration (mirrors StepEditPanel)
  const [agentConfigs, setAgentConfigs] = useState<AgentConfigs>({})
  const [selectedServers, setSelectedServers] = useState<string[]>(presetServers)
  const [selectedTools, setSelectedTools] = useState<string[]>(currentPresetTools || [])

  // Custom tools state (unified format: "category:tool" or "category:*")
  const [enabledCustomTools, setEnabledCustomTools] = useState<string[]>([])
  const [expandedToolCategories, setExpandedToolCategories] = useState<Set<string>>(new Set(['workspace_tools']))
  // Note: expandedWorkspaceSubCategories removed - workspace tools are shown flat now

  // Track if we've initialized from first step
  const [hasInitialized, setHasInitialized] = useState(false)

  // Collect all steps from the plan
  // Every step (including sub-agents) has a unique ID - use the actual step ID for everything
  const getAllSteps = useCallback((): Array<{ step: PlanStep; stepId: string }> => {
    if (!plan?.steps) return []
    const steps: Array<{ step: PlanStep; stepId: string }> = []

    const collectSteps = (stepList: PlanStep[]) => {
      stepList.forEach((step) => {
        steps.push({ step, stepId: step.id })
        if (isConditionalStep(step)) {
          if (step.if_true_steps) collectSteps(step.if_true_steps)
          if (step.if_false_steps) collectSteps(step.if_false_steps)
        }
        if (isDecisionStep(step) && step.decision_step) {
          steps.push({ step: step.decision_step as PlanStep, stepId: step.decision_step.id })
        }
        if (isTodoTaskStep(step)) {
          // Add the inner todo_task_step
          if (step.todo_task_step) {
            steps.push({ step: step.todo_task_step as PlanStep, stepId: step.todo_task_step.id })
          }
          // Add sub-agent steps from predefined_routes (use actual step ID)
          if (step.predefined_routes) {
            step.predefined_routes.forEach((route) => {
              if (route.sub_agent_step && route.sub_agent_step.id) {
                steps.push({ step: route.sub_agent_step as PlanStep, stepId: route.sub_agent_step.id })
              }
            })
          }
        }
      })
    }
    collectSteps(plan.steps)
    return steps
  }, [plan])

  const getSelectedSteps = useCallback(() => {
    const allSteps = getAllSteps()
    return allSteps.filter(({ stepId }) => selectedStepIds.includes(stepId))
  }, [getAllSteps, selectedStepIds])

  const selectedSteps = useMemo(() => getSelectedSteps(), [getSelectedSteps])

  // Initialize state from the first selected step
  useEffect(() => {
    if (selectedSteps.length > 0 && !hasInitialized) {
      const firstStep = selectedSteps[0].step
      const configs = firstStep.agent_configs || {}

      // Initialize agentConfigs from first step
      setAgentConfigs({
        execution_llm: configs.execution_llm,
        learning_llm: configs.learning_llm,
        execution_max_turns: configs.execution_max_turns,
        disable_learning: configs.disable_learning,
        use_code_execution_mode: configs.use_code_execution_mode,
        use_tool_search_mode: configs.use_tool_search_mode,
        learning_detail_level: configs.learning_detail_level,
        enable_context_offloading: configs.enable_context_offloading,
        disable_parallel_tool_execution: configs.disable_parallel_tool_execution,
      })

      // Initialize servers/tools from first step or fall back to preset
      if (configs.selected_servers && configs.selected_servers.length > 0) {
        setSelectedServers(configs.selected_servers)
      } else {
        setSelectedServers(presetServers)
      }

      if (configs.selected_tools && configs.selected_tools.length > 0) {
        setSelectedTools(configs.selected_tools)
      } else {
        setSelectedTools(currentPresetTools || [])
      }

      // Initialize custom tools from first step
      if (configs.enabled_custom_tools && configs.enabled_custom_tools.length > 0) {
        setEnabledCustomTools(configs.enabled_custom_tools)
      } else {
        setEnabledCustomTools([])
      }

      setHasInitialized(true)
    }
  }, [selectedSteps, hasInitialized, presetServers, currentPresetTools])

  // Reset initialization when selection changes
  useEffect(() => {
    setHasInitialized(false)
  }, [selectedStepIds])

  // Helper functions (same as StepEditPanel)
  const llmConfigToOption = (config: AgentLLMConfig | undefined): LLMOption | null => {
    if (!config || !config.provider || !config.model_id) return null
    const llm = availableLLMs.find(l => l.provider === config.provider && l.model === config.model_id)
    return llm || null
  }

  const getPresetDefaultLLM = (agentType: 'execution' | 'validation' | 'learning'): LLMOption | null => {
    if (!presetLLMConfig) return null
    let config: AgentLLMConfig | undefined
    if (agentType === 'execution') {
      config = presetLLMConfig.execution_llm || (presetLLMConfig.provider && presetLLMConfig.model_id ? {
        provider: presetLLMConfig.provider, model_id: presetLLMConfig.model_id
      } : undefined)
    } else if (agentType === 'learning') {
      config = presetLLMConfig.learning_llm || (presetLLMConfig.provider && presetLLMConfig.model_id ? {
        provider: presetLLMConfig.provider, model_id: presetLLMConfig.model_id
      } : undefined)
    }
    return config ? llmConfigToOption(config) : null
  }

  const optionToLLMConfig = (option: LLMOption | null): AgentLLMConfig | undefined => {
    if (!option) return undefined
    return { provider: option.provider as AgentLLMConfig['provider'], model_id: option.model }
  }

  // Helper functions for custom tools (unified format: "category:tool" or "category:*")
  const formatToolEntry = (category: string, tool: string): string => `${category}:${tool}`

  const parseToolEntry = (entry: string): { category: string; tool: string } | null => {
    const colonIndex = entry.indexOf(':')
    if (colonIndex === -1 || colonIndex === 0) return null
    return { category: entry.substring(0, colonIndex), tool: entry.substring(colonIndex + 1) }
  }

  const isCategoryEnabled = (category: string, enabledTools: string[]): boolean => {
    if (enabledTools.length === 0) return true
    return enabledTools.includes(formatToolEntry(category, '*'))
  }

  const isToolEnabled = (category: string, toolName: string, enabledTools: string[]): boolean => {
    if (enabledTools.length === 0) return true
    if (WORKSPACE_SUB_CATEGORIES.includes(category) && isCategoryEnabled('workspace_tools', enabledTools)) return true
    if (isCategoryEnabled(category, enabledTools)) return true
    return enabledTools.includes(formatToolEntry(category, toolName))
  }

  // Sentinel value: represents "no tools enabled". Backend won't match this category,
  // so no tools pass the filter. Distinct from [] which means "all enabled by default".
  const NONE_ENABLED_SENTINEL = 'none:*'

  const enableCategory = (category: string, enabledTools: string[]): string[] => {
    // Remove any specific tools from this category + sentinel, add category:*
    const filtered = enabledTools.filter(entry => {
      if (entry === NONE_ENABLED_SENTINEL) return false
      const parsed = parseToolEntry(entry)
      return !parsed || parsed.category !== category
    })
    return [...filtered, formatToolEntry(category, '*')]
  }

  const disableCategory = (category: string, enabledTools: string[]): string[] => {
    if (enabledTools.length === 0) {
      if (category === 'workspace_tools') {
        return [formatToolEntry('human_tools', '*')]
      } else if (category === 'human_tools') {
        return [formatToolEntry('workspace_tools', '*')]
      } else if (WORKSPACE_SUB_CATEGORIES.includes(category)) {
        const allWsTools = getToolsByCategory('workspace_tools', capabilities?.workspace)
        const disabledTools = new Set(getToolsByCategory(category, capabilities?.workspace))
        const result: string[] = []
        for (const tool of allWsTools) {
          if (!disabledTools.has(tool)) {
            const cat = getCategoryForTool(tool) || 'workspace_tools'
            result.push(formatToolEntry(cat, tool))
          }
        }
        result.push(formatToolEntry('human_tools', '*'))
        return result
      }
    }

    if (category === 'workspace_tools') {
      const result = enabledTools.filter(entry => {
        if (entry === NONE_ENABLED_SENTINEL) return false
        const parsed = parseToolEntry(entry)
        if (!parsed) return true
        return !parsed.category.startsWith('workspace')
      })
      // Use sentinel if result would be empty — [] means "all enabled by default"
      return result.length > 0 ? result : [NONE_ENABLED_SENTINEL]
    }

    const result = enabledTools.filter(entry => {
      if (entry === NONE_ENABLED_SENTINEL) return false
      const parsed = parseToolEntry(entry)
      return !parsed || parsed.category !== category
    })
    // Use sentinel if result would be empty — [] means "all enabled by default"
    return result.length > 0 ? result : [NONE_ENABLED_SENTINEL]
  }

  const enableTool = (category: string, toolName: string, enabledTools: string[]): string[] => {
    let filtered = enabledTools

    if (WORKSPACE_SUB_CATEGORIES.includes(category) && isCategoryEnabled('workspace_tools', enabledTools)) {
      filtered = filtered.filter(e => e !== formatToolEntry('workspace_tools', '*'))
      const allWsTools = getToolsByCategory('workspace_tools', capabilities?.workspace)
      for (const tool of allWsTools) {
        const cat = getCategoryForTool(tool) || 'workspace_tools'
        filtered.push(formatToolEntry(cat, tool))
      }
    }

    if (isCategoryEnabled(category, filtered)) {
      filtered = filtered.filter(e => e !== formatToolEntry(category, '*'))
      const allCategoryTools = getToolsByCategory(category, capabilities?.workspace)
      filtered = [...filtered, ...allCategoryTools.map(t => formatToolEntry(category, t))]
    }

    const toolEntry = formatToolEntry(category, toolName)
    if (!filtered.includes(toolEntry)) {
      filtered = [...filtered, toolEntry]
    }
    return filtered
  }

  const disableTool = (category: string, toolName: string, enabledTools: string[]): string[] => {
    let filtered = enabledTools

    if (WORKSPACE_SUB_CATEGORIES.includes(category) && isCategoryEnabled('workspace_tools', enabledTools)) {
      filtered = filtered.filter(e => e !== formatToolEntry('workspace_tools', '*'))
      const allWsTools = getToolsByCategory('workspace_tools', capabilities?.workspace)
      for (const tool of allWsTools) {
        const cat = getCategoryForTool(tool) || 'workspace_tools'
        filtered.push(formatToolEntry(cat, tool))
      }
    }

    if (isCategoryEnabled(category, filtered)) {
      filtered = filtered.filter(e => e !== formatToolEntry(category, '*'))
      const allCategoryTools = getToolsByCategory(category, capabilities?.workspace)
      const otherTools = allCategoryTools.filter(t => t !== toolName)
      filtered = [...filtered, ...otherTools.map(t => formatToolEntry(category, t))]
      return filtered
    }

    return filtered.filter(entry => entry !== formatToolEntry(category, toolName))
  }

  const isSubCategoryEnabled = (category: string, subCategoryTools: string[], enabledTools: string[]): boolean => {
    if (enabledTools.length === 0) return true
    if (WORKSPACE_SUB_CATEGORIES.includes(category) && isCategoryEnabled('workspace_tools', enabledTools)) return true
    if (isCategoryEnabled(category, enabledTools)) return true

    const enabledInSubCategory = subCategoryTools.filter(toolName =>
      isToolEnabled(category, toolName, enabledTools)
    )
    return enabledInSubCategory.length === subCategoryTools.length
  }

  const toggleSubCategory = (category: string, subCategoryTools: string[], enabled: boolean, enabledTools: string[]): string[] => {
    let result = enabledTools

    if (WORKSPACE_SUB_CATEGORIES.includes(category) && isCategoryEnabled('workspace_tools', result)) {
      result = result.filter(e => e !== formatToolEntry('workspace_tools', '*'))
      const allWsTools = getToolsByCategory('workspace_tools', capabilities?.workspace)
      for (const tool of allWsTools) {
        const cat = getCategoryForTool(tool) || 'workspace_tools'
        result.push(formatToolEntry(cat, tool))
      }
    }

    if (enabled) {
      if (isCategoryEnabled(category, result)) {
        return result
      }
      for (const toolName of subCategoryTools) {
        const entry = formatToolEntry(category, toolName)
        if (!result.includes(entry)) {
          result = [...result, entry]
        }
      }
    } else {
      if (isCategoryEnabled(category, result)) {
        result = result.filter(e => e !== formatToolEntry(category, '*'))
      }
      result = result.filter(entry => {
        const parsed = parseToolEntry(entry)
        return !parsed || parsed.category !== category
      })
    }

    return result
  }

  // LLM handlers
  const handleExecutionLLMSelect = (llm: LLMOption) => {
    setAgentConfigs(prev => ({ ...prev, execution_llm: optionToLLMConfig(llm) }))
  }
  const handleLearningLLMSelect = (llm: LLMOption) => {
    setAgentConfigs(prev => ({ ...prev, learning_llm: optionToLLMConfig(llm) }))
  }

  // Toggle handlers
  const handleToggleChange = (key: keyof AgentConfigs, value: boolean) => {
    setAgentConfigs(prev => ({ ...prev, [key]: value }))
  }

  // Max turns handler
  const handleMaxTurnsChange = (value: number) => {
    setAgentConfigs(prev => ({ ...prev, execution_max_turns: value }))
  }

  // Apply all changes to selected steps
  const handleApplyAll = async () => {
    setIsSaving(true)
    try {
      // All steps use their actual step.id for both matching and backend updates
      const updates = selectedSteps.map(({ step }) => {
        const existingConfigs = step.agent_configs || {}
        const newAgentConfigs: AgentConfigs = { ...existingConfigs, ...agentConfigs }

        // Handle servers/tools - store in agent_configs
        if (selectedServers.length > 0) {
          newAgentConfigs.selected_servers = selectedServers
        }
        if (selectedTools.length > 0) {
          newAgentConfigs.selected_tools = selectedTools
        }

        // Handle custom tools
        if (enabledCustomTools.length === 0) {
          newAgentConfigs.enabled_custom_tools = undefined
        } else {
          newAgentConfigs.enabled_custom_tools = enabledCustomTools
        }

        // Handle context offloading
        if (agentConfigs.enable_context_offloading === false) {
          newAgentConfigs.enable_context_offloading = false
        } else {
          newAgentConfigs.enable_context_offloading = undefined
        }

        // Handle parallel tool execution
        if (agentConfigs.disable_parallel_tool_execution === true) {
          newAgentConfigs.disable_parallel_tool_execution = true
        } else {
          newAgentConfigs.disable_parallel_tool_execution = undefined
        }

        const stepUpdates: Partial<PlanStep> = { agent_configs: newAgentConfigs }

        return { stepId: step.id, updates: stepUpdates }
      })
      await onBulkUpdate(updates)
    } finally {
      setIsSaving(false)
    }
  }

  // Check if NO_SERVERS is selected
  const hasNoServers = selectedServers.includes("NO_SERVERS")

  // Get summary
  const getAgentConfigSummary = () => {
    const effectiveCodeExecMode = agentConfigs.use_code_execution_mode !== undefined
      ? agentConfigs.use_code_execution_mode
      : presetUseCodeExecutionMode

    let mode = 'Simple'
    if (effectiveCodeExecMode) mode = 'Code Exec'
    if (agentConfigs.use_tool_search_mode) mode = 'Tool Search'

    return `${mode} mode • ${selectedStepIds.length} steps selected`
  }

  const getMCPConfigSummary = () => {
    if (hasNoServers) return 'No servers (Pure LLM mode)'
    if (selectedServers.length === 0) return `Using preset defaults (${presetServers.length} servers)`
    return `${selectedServers.length} server(s) selected`
  }

  const sidebarWidth = isCompact ? 'w-[400px]' : 'w-[600px]'

  return (
    <div className={`absolute right-0 top-0 bottom-0 ${sidebarWidth} bg-white dark:bg-gray-900 border-l border-gray-200 dark:border-gray-700 shadow-xl z-50 flex flex-col transition-all duration-300`}>
      {/* Header - Same as StepSidebar */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800">
        <div className="flex flex-col gap-0.5">
          <span className="text-base font-semibold text-gray-900 dark:text-gray-100">
            Configure {selectedStepIds.length} Steps
          </span>
          <span className="text-xs text-gray-500 dark:text-gray-400">
            Showing values from: {selectedSteps[0]?.step.title || 'first step'}
          </span>
        </div>
        <button
          onClick={onClose}
          className="p-1.5 rounded-md hover:bg-gray-200 dark:hover:bg-gray-700 text-gray-600 dark:text-gray-400 transition-colors"
        >
          <X className="w-5 h-5" />
        </button>
      </div>

      {/* Content - Scrollable */}
      <div className="flex-1 overflow-y-auto p-4">
        {/* Selected Steps Preview */}
        <div className="mb-4 p-3 bg-purple-50 dark:bg-purple-900/20 rounded-lg border border-purple-200 dark:border-purple-800">
          <div className="text-xs font-medium text-purple-600 dark:text-purple-400 mb-2">Selected Steps:</div>
          <div className="flex flex-wrap gap-1.5">
            {selectedSteps.slice(0, 6).map(({ step }) => (
              <span key={step.id} className="px-2 py-1 bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300 text-xs rounded truncate max-w-[140px]" title={step.title}>
                {step.title}
              </span>
            ))}
            {selectedSteps.length > 6 && (
              <span className="px-2 py-1 bg-gray-200 dark:bg-gray-700 text-gray-600 dark:text-gray-400 text-xs rounded">
                +{selectedSteps.length - 6} more
              </span>
            )}
          </div>
        </div>

        {/* Agent Config Section - Mirrors StepEditPanel exactly */}
        <div className="border border-gray-200 dark:border-gray-700 rounded-lg">
          {/* Compact Header - Always Visible */}
          <div
            className="cursor-pointer hover:bg-gray-100 dark:hover:bg-gray-700/30 px-3 py-2.5 transition-colors"
            onClick={() => setIsExpanded(!isExpanded)}
          >
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2 flex-1 min-w-0">
                <Settings className="w-3.5 h-3.5 text-gray-500 dark:text-gray-400 flex-shrink-0" />
                <span className="text-xs font-medium text-gray-600 dark:text-gray-400">Agent Config</span>
                <span className="text-xs text-gray-500 dark:text-gray-500 truncate">{getAgentConfigSummary()}</span>
              </div>
              {isExpanded ? (
                <ChevronUp className="w-3.5 h-3.5 text-gray-500 dark:text-gray-400" />
              ) : (
                <ChevronDown className="w-3.5 h-3.5 text-gray-500 dark:text-gray-400" />
              )}
            </div>
            <div className="flex items-center gap-2 mt-1 ml-6">
              <span className="text-xs font-medium text-gray-600 dark:text-gray-400">MCP Config:</span>
              <span className="text-xs text-gray-500 dark:text-gray-500">{getMCPConfigSummary()}</span>
            </div>
          </div>

          {/* Expanded Configuration Panel */}
          {isExpanded && (
            <div className="p-3 border-t border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-900/20">
              <div className="space-y-4">

                {/* Execution Agent Configuration */}
                <div className="space-y-2">
                  <div className="flex items-center justify-between">
                    <div className="text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wide">
                      Execution
                    </div>
                    {/* Code Execution Mode Toggle - Same as StepEditPanel */}
                    <div className="flex items-center border border-gray-300 dark:border-gray-600 rounded-md overflow-hidden">
                      <TooltipProvider>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <button
                              type="button"
                              onClick={() => setAgentConfigs(prev => ({ ...prev, use_code_execution_mode: false, use_tool_search_mode: false }))}
                              className={`px-2 py-1 text-xs font-medium transition-colors border-r border-gray-300 dark:border-gray-600 ${
                                !agentConfigs.use_tool_search_mode && (agentConfigs.use_code_execution_mode === false || (agentConfigs.use_code_execution_mode === undefined && !presetUseCodeExecutionMode))
                                  ? 'bg-blue-500 text-white'
                                  : 'bg-white dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-600'
                              }`}
                            >
                              <Sparkles className="w-3 h-3 inline mr-1" />
                              Simple
                            </button>
                          </TooltipTrigger>
                          <TooltipContent><p>Simple mode - Direct MCP tool access</p></TooltipContent>
                        </Tooltip>

                        <Tooltip>
                          <TooltipTrigger asChild>
                            <button
                              type="button"
                              onClick={() => setAgentConfigs(prev => ({ ...prev, use_code_execution_mode: true, use_tool_search_mode: false, disable_learning: false }))}
                              className={`px-2 py-1 text-xs font-medium transition-colors border-r border-gray-300 dark:border-gray-600 ${
                                !agentConfigs.use_tool_search_mode && (agentConfigs.use_code_execution_mode === true || (agentConfigs.use_code_execution_mode === undefined && presetUseCodeExecutionMode))
                                  ? 'bg-green-500 text-white'
                                  : 'bg-white dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-600'
                              }`}
                            >
                              <Code2 className="w-3 h-3 inline mr-1" />
                              Code Exec
                            </button>
                          </TooltipTrigger>
                          <TooltipContent><p>Code Exec mode - MCP tools via generated Go code</p></TooltipContent>
                        </Tooltip>

                        <Tooltip>
                          <TooltipTrigger asChild>
                            <button
                              type="button"
                              onClick={() => setAgentConfigs(prev => ({ ...prev, use_code_execution_mode: false, use_tool_search_mode: true }))}
                              className={`px-2 py-1 text-xs font-medium transition-colors ${
                                agentConfigs.use_tool_search_mode === true
                                  ? 'bg-purple-500 text-white'
                                  : 'bg-white dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-600'
                              }`}
                            >
                              <Search className="w-3 h-3 inline mr-1" />
                              Tool Search
                            </button>
                          </TooltipTrigger>
                          <TooltipContent><p>Tool Search mode - Dynamic tool discovery</p></TooltipContent>
                        </Tooltip>
                      </TooltipProvider>
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <div className="flex-1 min-w-0">
                      <LLMSelectionDropdown
                        availableLLMs={availableLLMs}
                        selectedLLM={llmConfigToOption(agentConfigs.execution_llm) || getPresetDefaultLLM('execution') || getCurrentLLMOption()}
                        onLLMSelect={handleExecutionLLMSelect}
                        inModal={false}
                        openDirection="down"
                      />
                    </div>
                    <div className="flex items-center gap-2">
                      <label className="text-xs text-gray-600 dark:text-gray-400 whitespace-nowrap">Max Turns:</label>
                      <select
                        value={agentConfigs.execution_max_turns || 100}
                        onChange={(e) => handleMaxTurnsChange(parseInt(e.target.value))}
                        className="px-2 py-1.5 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 text-xs focus:ring-2 focus:ring-blue-500 w-20"
                      >
                        {MAX_TURNS_OPTIONS.map((value) => (
                          <option key={value} value={value}>{value}</option>
                        ))}
                      </select>
                    </div>
                  </div>
                </div>

                {/* LLM Validation Agent Configuration — disabled (LLM validation is dead code in backend).
                   Only pre-validation (code-based structural checks) is active. Kept commented out for reference. */}

                {/* Learning Agent Configuration */}
                <div className="border-t border-gray-200 dark:border-gray-700 pt-4 space-y-2">
                  <div className="flex items-center justify-between">
                    <div className="text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wide">
                      Learning
                    </div>
                    <label className="flex items-center gap-2 cursor-pointer">
                      <input
                        type="checkbox"
                        checked={agentConfigs.disable_learning !== true}
                        onChange={(e) => handleToggleChange('disable_learning', !e.target.checked)}
                        className="w-4 h-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500"
                      />
                      <span className="text-xs text-gray-600 dark:text-gray-400">Enabled</span>
                    </label>
                  </div>
                  {agentConfigs.disable_learning !== true && (
                    <div className="space-y-2">
                      <LLMSelectionDropdown
                        availableLLMs={availableLLMs}
                        selectedLLM={llmConfigToOption(agentConfigs.learning_llm) || getPresetDefaultLLM('learning') || getCurrentLLMOption()}
                        onLLMSelect={handleLearningLLMSelect}
                        inModal={false}
                        openDirection="down"
                      />
                    </div>
                  )}
                </div>

                {/* MCP Servers Selection - Reuse ToolSelectionSection */}
                <div className="border-t border-gray-200 dark:border-gray-700 pt-4 space-y-2">
                  <div className="text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wide">
                    MCP Servers & Tools
                  </div>
                  <ToolSelectionSection
                    availableServers={presetServers}
                    selectedServers={selectedServers}
                    selectedTools={selectedTools}
                    onServerChange={setSelectedServers}
                    onToolChange={setSelectedTools}
                    agentMode="workflow"
                  />
                </div>

                {/* Custom Tools Section */}
                <div className="border-t border-gray-200 dark:border-gray-700 pt-4 space-y-3">
                  <div className="text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wide">
                    Custom Tools
                  </div>
                  <div className="text-xs text-gray-500 dark:text-gray-500 italic">
                    Select categories (enables all tools) or individual tools. By default, all tools are enabled.
                  </div>

                  {/* Workspace Tools Category */}
                  <div className="space-y-1.5">
                    <div className="flex items-center justify-between">
                      <label className="flex items-center gap-2 cursor-pointer flex-1">
                        <input
                          type="checkbox"
                          checked={(() => {
                            const advancedTools = getToolsByCategory('workspace_advanced', capabilities?.workspace)
                            if (enabledCustomTools.length === 0) return true
                            if (isCategoryEnabled('workspace_tools', enabledCustomTools)) return true
                            if (isCategoryEnabled('workspace_advanced', enabledCustomTools)) return true
                            const enabledCount = advancedTools.filter(t => isToolEnabled('workspace_advanced', t, enabledCustomTools)).length
                            return enabledCount === advancedTools.length
                          })()}
                          onChange={(e) => {
                            if (e.target.checked) {
                              setEnabledCustomTools(prev => enableCategory('workspace_tools', prev))
                            } else {
                              setEnabledCustomTools(prev => disableCategory('workspace_tools', prev))
                            }
                          }}
                          className="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500"
                        />
                        <span className="text-xs font-medium text-gray-700 dark:text-gray-300">Workspace Tools</span>
                        <span className="text-xs text-gray-500 dark:text-gray-500">
                          {(() => {
                            const advancedTools = getToolsByCategory('workspace_advanced', capabilities?.workspace)
                            if (enabledCustomTools.length === 0) return `(${advancedTools.length}/${advancedTools.length} tools)`
                            if (isCategoryEnabled('workspace_tools', enabledCustomTools)) return `(${advancedTools.length}/${advancedTools.length} tools)`
                            if (isCategoryEnabled('workspace_advanced', enabledCustomTools)) return `(${advancedTools.length}/${advancedTools.length} tools)`
                            const enabledCount = advancedTools.filter(t => isToolEnabled('workspace_advanced', t, enabledCustomTools)).length
                            return `(${enabledCount}/${advancedTools.length} tools)`
                          })()}
                        </span>
                      </label>
                      <button
                        type="button"
                        onClick={() => {
                          const newExpanded = new Set(expandedToolCategories)
                          if (newExpanded.has('workspace_tools')) {
                            newExpanded.delete('workspace_tools')
                          } else {
                            newExpanded.add('workspace_tools')
                          }
                          setExpandedToolCategories(newExpanded)
                        }}
                        className="text-xs text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300"
                      >
                        {expandedToolCategories.has('workspace_tools') ? 'Hide' : 'Show'} tools
                      </button>
                    </div>

                    {expandedToolCategories.has('workspace_tools') && (
                      <div className="ml-6 space-y-1.5 pl-2 border-l-2 border-gray-200 dark:border-gray-700">
                        {getToolsByCategory('workspace_advanced', capabilities?.workspace).map((toolName: string) => {
                          const toolIsEnabled = isToolEnabled('workspace_advanced', toolName, enabledCustomTools)
                          return (
                            <label key={toolName} className="flex items-center gap-2 cursor-pointer">
                              <input
                                type="checkbox"
                                checked={toolIsEnabled}
                                onChange={(e) => {
                                  if (e.target.checked) {
                                    setEnabledCustomTools(prev => enableTool('workspace_advanced', toolName, prev))
                                  } else {
                                    setEnabledCustomTools(prev => disableTool('workspace_advanced', toolName, prev))
                                  }
                                }}
                                className="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500"
                              />
                              <span className="text-xs text-gray-600 dark:text-gray-400">{toolName}</span>
                            </label>
                          )
                        })}
                      </div>
                    )}
                  </div>

                  {/* Human Tools Category */}
                  <div className="space-y-1.5">
                    <div className="flex items-center justify-between">
                      <label className="flex items-center gap-2 cursor-pointer flex-1">
                        <input
                          type="checkbox"
                          checked={(() => {
                            const allHumanTools = getToolsByCategory('human_tools', capabilities?.workspace)
                            if (enabledCustomTools.length === 0) return true
                            if (isCategoryEnabled('human_tools', enabledCustomTools)) return true
                            const enabledCount = enabledCustomTools
                              .map(entry => parseToolEntry(entry))
                              .filter(parsed => parsed && parsed.category === 'human_tools' && parsed.tool !== '*' && allHumanTools.includes(parsed.tool))
                              .length
                            return enabledCount === allHumanTools.length
                          })()}
                          onChange={(e) => {
                            if (e.target.checked) {
                              setEnabledCustomTools(prev => enableCategory('human_tools', prev))
                            } else {
                              setEnabledCustomTools(prev => disableCategory('human_tools', prev))
                            }
                          }}
                          className="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500"
                        />
                        <span className="text-xs font-medium text-gray-700 dark:text-gray-300">Human Tools</span>
                        <span className="text-xs text-gray-500 dark:text-gray-500">
                          {(() => {
                            const allHumanTools = getToolsByCategory('human_tools', capabilities?.workspace)
                            if (enabledCustomTools.length === 0) {
                              return `(${allHumanTools.length}/${allHumanTools.length} tools)`
                            }
                            if (isCategoryEnabled('human_tools', enabledCustomTools)) {
                              return `(${allHumanTools.length}/${allHumanTools.length} tools)`
                            }
                            const enabledCount = enabledCustomTools
                              .map(entry => parseToolEntry(entry))
                              .filter(parsed => parsed && parsed.category === 'human_tools' && parsed.tool !== '*' && allHumanTools.includes(parsed.tool))
                              .length
                            return `(${enabledCount}/${allHumanTools.length} tools)`
                          })()}
                        </span>
                      </label>
                      <button
                        type="button"
                        onClick={() => {
                          const newExpanded = new Set(expandedToolCategories)
                          if (newExpanded.has('human_tools')) {
                            newExpanded.delete('human_tools')
                          } else {
                            newExpanded.add('human_tools')
                          }
                          setExpandedToolCategories(newExpanded)
                        }}
                        className="text-xs text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300"
                      >
                        {expandedToolCategories.has('human_tools') ? 'Hide' : 'Show'} tools
                      </button>
                    </div>
                    {expandedToolCategories.has('human_tools') && (
                      <div className="ml-6 space-y-1.5 pl-2 border-l-2 border-gray-200 dark:border-gray-700">
                        {HUMAN_TOOLS.map((toolName) => {
                          const toolIsEnabled = isToolEnabled('human_tools', toolName, enabledCustomTools)
                          return (
                            <label key={toolName} className="flex items-center gap-2 cursor-pointer">
                              <input
                                type="checkbox"
                                checked={toolIsEnabled}
                                onChange={(e) => {
                                  if (e.target.checked) {
                                    setEnabledCustomTools(prev => enableTool('human_tools', toolName, prev))
                                  } else {
                                    setEnabledCustomTools(prev => disableTool('human_tools', toolName, prev))
                                  }
                                }}
                                className="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500"
                              />
                              <span className="text-xs text-gray-600 dark:text-gray-400">{toolName}</span>
                            </label>
                          )
                        })}
                      </div>
                    )}
                  </div>
                </div>

                {/* Context Offloading Virtual Tools Toggle */}
                <div className="border-t border-gray-200 dark:border-gray-700 pt-4 space-y-2">
                  <div className="text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wide">
                    Context Offloading Virtual Tools
                  </div>
                  <div className="flex items-center gap-2">
                    <input
                      type="checkbox"
                      id="context-offloading-multi"
                      checked={agentConfigs.enable_context_offloading !== false}
                      onChange={(e) => {
                        setAgentConfigs((prev) => ({
                          ...prev,
                          enable_context_offloading: e.target.checked,
                        }))
                      }}
                      className="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500"
                    />
                    <label
                      htmlFor="context-offloading-multi"
                      className="text-xs text-gray-600 dark:text-gray-400 cursor-pointer flex-1"
                    >
                      Enable Context Offloading Virtual Tools
                      <span className="text-gray-500 dark:text-gray-500 ml-1">
                        (read_large_output, search_large_output, query_large_output)
                      </span>
                    </label>
                  </div>
                </div>

                {/* Parallel Tool Execution Toggle */}
                <div className="border-t border-gray-200 dark:border-gray-700 pt-4 space-y-2">
                  <div className="text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wide">
                    Parallel Tool Execution
                  </div>
                  <div className="flex items-center gap-2">
                    <input
                      type="checkbox"
                      id="parallel-tool-exec-multi"
                      checked={agentConfigs.disable_parallel_tool_execution !== true}
                      onChange={(e) => {
                        setAgentConfigs((prev) => ({
                          ...prev,
                          disable_parallel_tool_execution: !e.target.checked,
                        }))
                      }}
                      className="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500"
                    />
                    <label
                      htmlFor="parallel-tool-exec-multi"
                      className="text-xs text-gray-600 dark:text-gray-400 cursor-pointer flex-1"
                    >
                      Enable Parallel Tool Execution
                      <span className="text-gray-500 dark:text-gray-500 ml-1">
                        (allows concurrent execution of multiple independent tool calls)
                      </span>
                    </label>
                  </div>
                </div>
              </div>
            </div>
          )}
        </div>
      </div>

      {/* Footer with Apply Button */}
      <div className="px-4 py-3 border-t border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800">
        <div className="flex items-center justify-between">
          <p className="text-xs text-gray-500 dark:text-gray-400">
            Shift+Click or Ctrl/Cmd+Click to select steps
          </p>
          <Button
            onClick={handleApplyAll}
            disabled={isSaving}
            className="px-4"
          >
            {isSaving ? (
              <>
                <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                Applying...
              </>
            ) : (
              `Apply to ${selectedStepIds.length} Steps`
            )}
          </Button>
        </div>
      </div>

      {/* Loading Overlay */}
      {isSaving && (
        <div className="absolute inset-0 bg-white/80 dark:bg-gray-900/80 flex items-center justify-center z-50 backdrop-blur-sm">
          <div className="flex flex-col items-center gap-3">
            <Loader2 className="w-8 h-8 text-blue-500 animate-spin" />
            <div className="text-sm font-medium text-gray-700 dark:text-gray-300">
              Applying to {selectedStepIds.length} steps...
            </div>
            <div className="text-xs text-gray-500 dark:text-gray-400">
              This may take a moment
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

export default MultiStepSidebar
