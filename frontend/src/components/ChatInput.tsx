import React, { useRef, useCallback, useMemo, useState, useEffect, useLayoutEffect } from 'react'
import { Send, Square, Code2, Sparkles, Loader2, FolderOpen, Search, Globe, GitBranch, Layers, FileSearch, Play, X } from 'lucide-react'
import { Button } from './ui/Button'
import { Textarea } from './ui/Textarea'
import FileContextDisplay from './FileContextDisplay'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './ui/tooltip'
import { CircularProgress } from './ui/CircularProgress'
import { getEventData, isEventType } from '../generated/event-types'
import type { TokenUsageEvent } from '../generated/events'
import ServerSelectionDropdown from './ServerSelectionDropdown'
import SkillSelectionDropdown from './skills/SkillSelectionDropdown'
import SecretSelectionDropdown from './secrets/SecretSelectionDropdown'
import SubAgentSelectionDropdown from './subagents/SubAgentSelectionDropdown'
import LLMSelectionDropdown from './LLMSelectionDropdown'
import FileSelectionDialog from './FileSelectionDialog'
import CommandSelectionDialog from './CommandSelectionDialog'
import WorkflowSelectionDialog from './WorkflowSelectionDialog'
import SkillImportDialog from './skills/SkillImportDialog'
import { MCPConfigPopup } from './MCPConfigPopup'
import MCPDetailsModal from './MCPDetailsModal'
import LLMConfigurationModal from './LLMConfigurationModal'
import ResumeSessionDialog from './ResumeSessionDialog'
import DelegationTierConfigModal from './DelegationTierConfigModal'
import WorkflowBuilderModal from './WorkflowBuilderModal'
import type { PlannerFile, PollingEvent } from '../services/api-types'
import type { LLMOption } from '../types/llm'
import { useAppStore, useMCPStore, useLLMStore, useChatStore } from '../stores'
import { useCapabilitiesStore } from '../stores/useCapabilitiesStore'
import { useWorkspaceStore } from '../stores/useWorkspaceStore'
import { useCommandDialogStore } from '../stores/useCommandDialogStore'
import { usePresetApplication } from '../stores/useGlobalPresetStore'
import { useModeStore } from '../stores/useModeStore'
import { agentApi } from '../services/api'

interface ChatInputProps {
  // Handlers (callbacks only)
  onSubmit: (query: string) => void
  onStopStreaming: () => void
}

// Shorten model ID to max 5 char label
const shortModelName = (modelId: string): string => {
  let m = modelId.replace(/^(global\.|us\.|eu\.)?anthropic\./, '').replace(/^(global\.|us\.|eu\.)?meta\./, '')
  m = m.split('/').pop() || m
  // Map to short names
  if (/opus-4/i.test(m)) return 'opus4'
  if (/sonnet-4-5|sonnet-4\.5/i.test(m)) return 'sn4.5'
  if (/sonnet-4/i.test(m)) return 'sn4'
  if (/sonnet-3-7|sonnet-3\.7/i.test(m)) return 'sn3.7'
  if (/sonnet-3-5|sonnet-3\.5/i.test(m)) return 'sn3.5'
  if (/haiku-4-5|haiku-4\.5/i.test(m)) return 'hk4.5'
  if (/haiku-3-5|haiku-3\.5/i.test(m)) return 'hk3.5'
  if (/gpt-5-mini/i.test(m)) return 'g5m'
  if (/gpt-5/i.test(m)) return 'gpt5'
  if (/gpt-4\.?1-mini/i.test(m)) return 'g4.1m'
  if (/gpt-4\.?1/i.test(m)) return 'g4.1'
  if (/gpt-4o-mini/i.test(m)) return 'g4om'
  if (/gpt-4o/i.test(m)) return 'g4o'
  if (/grok/i.test(m)) return 'grok'
  if (/gemini.*3.*flash/i.test(m)) return 'gm3f'
  if (/gemini.*3.*pro/i.test(m)) return 'gm3p'
  if (/gemini.*2\.?5.*pro/i.test(m)) return 'gm25p'
  if (/gemini.*2\.?5.*flash/i.test(m)) return 'gm25f'
  if (/gemini.*2\.?0.*flash/i.test(m)) return 'gm20f'
  if (/gemini.*1\.?5.*pro/i.test(m)) return 'gm15p'
  if (/gemini.*1\.?5.*flash/i.test(m)) return 'gm15f'
  if (/gemini/i.test(m)) return 'gem'
  if (/llama/i.test(m)) return 'llama'
  if (/mistral/i.test(m)) return 'mstrl'
  // Fallback: first 5 chars
  return m.slice(0, 5)
}

// Stable empty array reference to avoid infinite loops in selectors
const EMPTY_EVENTS: never[] = []

// Provider color dot + short label for tier chip
const providerMeta: Record<string, { color: string; label: string }> = {
  anthropic: { color: 'bg-amber-500', label: 'An' },
  openai: { color: 'bg-emerald-500', label: 'OA' },
  openrouter: { color: 'bg-purple-500', label: 'OR' },
  bedrock: { color: 'bg-orange-500', label: 'BR' },
  vertex: { color: 'bg-sky-500', label: 'Vx' },
  azure: { color: 'bg-blue-500', label: 'Az' },
}
const TierProviderDot = ({ provider }: { provider: string }) => {
  const meta = providerMeta[provider]
  return (
    <>
      <span className={`w-1.5 h-1.5 rounded-full flex-shrink-0 ${meta?.color || 'bg-gray-400'}`} />
      <span className="opacity-70">{meta?.label || provider.slice(0, 2).toUpperCase()}</span>
    </>
  )
}

// Completely isolated input component that doesn't re-render when events change
const ChatInputComponent: React.FC<ChatInputProps> = ({
  onSubmit,
  onStopStreaming
}) => {
  // Store subscriptions
  const {
    agentMode,
    setWorkspaceMinimized,
    delegationMode
  } = useAppStore()
  const selectedModeCategory = useModeStore(state => state.selectedModeCategory)
  const isMultiAgentMode = selectedModeCategory === 'multi-agent'
  // For plan features, treat multi-agent as always 'plan'
  const effectiveDelegationMode = isMultiAgentMode ? 'plan' as const : delegationMode

  // Detect plan phase from events (planning → execution after confirm_plan_execution succeeds)
  // We'll compute this from tabEvents below after they're defined

  // Use selectors to subscribe only to specific values, reducing re-renders
  const activeTabId = useChatStore(state => state.activeTabId)
  const setTabConfig = useChatStore(state => state.setTabConfig)
  const addToast = useChatStore(state => state.addToast)
  // Get active tab and its config (ChatInput is only rendered in chat mode)
  // Use selector to get only the tab we need, preventing re-renders when other tabs change
  const activeTab = useChatStore(state => 
    activeTabId ? state.chatTabs[activeTabId] : undefined
  )
  
  // Memoize tabConfig to prevent unnecessary re-renders
  const tabConfig = useMemo(() => activeTab?.config, [activeTab?.config])
  
  // CRITICAL: Always use tab's status - never fall back to global to prevent mixing
  // If no active tab, this is an error condition (tabs should always exist)
  const isStreaming = activeTab?.isStreaming ?? false
  const tabSessionId = activeTab?.sessionId ?? null
  const isViewOnly = activeTab?.metadata?.isViewOnly ?? false
  
  // Note: activeTab may be undefined during initial render before tabs are created
  // This is expected and will resolve once the tab store initializes
  
  // Get all tab events from the store (stable selector)
  const allTabEvents = useChatStore(state => state.tabEvents)

  // Derive tab-specific events with useMemo (avoids selector closure issues)
  const tabEvents = useMemo(() => {
    if (!tabSessionId) return EMPTY_EVENTS
    return allTabEvents[tabSessionId] ?? EMPTY_EVENTS
  }, [tabSessionId, allTabEvents])

  // Detect plan phase from events: scan backwards for the most recent phase-changing event.
  // - plan_approval event → execution (plan presented for approval)
  // - create_delegation_plan tool call → planning (new plan being created)
  // Whichever is most recent wins. Default: planning.
  const planPhase = useMemo(() => {
    if (!isMultiAgentMode) return null
    for (let i = tabEvents.length - 1; i >= 0; i--) {
      const event = tabEvents[i]
      if (event.type === 'plan_approval') {
        return 'execution'
      }
      if (event.type === 'tool_call_start' || event.type === 'tool_call_end') {
        const agentEvent = event.data as { data?: { tool_name?: string }; tool_name?: string } | undefined
        const toolName = agentEvent?.data?.tool_name || agentEvent?.tool_name
        if (toolName === 'create_delegation_plan') {
          return 'planning'
        }
      }
    }
    return 'planning'
  }, [isMultiAgentMode, tabEvents])

  // Helper: check if an event is from a sub-agent (delegation)
  const isSubAgentEvent = useCallback((event: PollingEvent): boolean => {
    const agentEvent = event.data as Record<string, unknown> | undefined
    const innerData = agentEvent?.data as Record<string, unknown> | undefined
    const comp = (event as unknown as Record<string, unknown>).component ?? innerData?.component ?? agentEvent?.component
    const corrId = (event as unknown as Record<string, unknown>).correlation_id ?? innerData?.correlation_id ?? agentEvent?.correlation_id
    return (typeof comp === 'string' && comp.startsWith('delegation-'))
      || (typeof corrId === 'string' && corrId.startsWith('delegation-'))
  }, [])

  // Find the latest token usage (optimized with backward iteration)
  // In multi-agent mode, skip sub-agent events — show the PARENT agent's context usage
  const { contextUsagePercent, latestTokenUsage } = useMemo(() => {
    if (tabEvents.length === 0) return { contextUsagePercent: null, latestTokenUsage: null }

    // Iterate backwards (newest first) to find the latest quickly
    let latestTokenUsageEvent = null
    let latestTotalEvent = null

    for (let i = tabEvents.length - 1; i >= 0 && !latestTotalEvent; i--) {
      const event = tabEvents[i]
      if (event.type === 'token_usage') {
        // Skip sub-agent token_usage events — we want the parent agent's context
        if (isSubAgentEvent(event)) continue
        const tokenUsageData = event.data?.data?.token_usage
        if (tokenUsageData?.context === 'conversation_total') {
          latestTotalEvent = event
          break
        }
        if (!latestTokenUsageEvent) {
          latestTokenUsageEvent = event
        }
      }
    }

    const latestEvent = latestTotalEvent || latestTokenUsageEvent

    if (latestEvent) {
      let tokenUsage: TokenUsageEvent | null = null
      if (isEventType(latestEvent, 'token_usage')) {
        tokenUsage = getEventData(latestEvent) as TokenUsageEvent
      } else {
        tokenUsage = latestEvent.data?.data?.token_usage as TokenUsageEvent | undefined || null
      }

      if (tokenUsage) {
        const isTotalEvent = tokenUsage.context === 'conversation_total'
        const contextPercent = isTotalEvent
          ? (tokenUsage.generation_info?.context_usage_percent as number | undefined) ?? tokenUsage.context_usage_percent
          : tokenUsage.context_usage_percent ?? (tokenUsage.generation_info?.context_usage_percent as number | undefined)

        return {
          contextUsagePercent: contextPercent !== undefined && contextPercent !== null ? contextPercent : null,
          latestTokenUsage: tokenUsage
        }
      }
    }

    // Fallback: Check LLM generation end events (iterate backwards)
    for (let i = tabEvents.length - 1; i >= 0; i--) {
      const event = tabEvents[i]
      if (event.type === 'llm_generation_end') {
        // Skip sub-agent llm_generation_end events
        if (isSubAgentEvent(event)) continue
        const llmData = event.data?.data?.llm_generation_end
        const contextPercent = llmData?.metadata?.context_usage_percent as number | undefined

        if (contextPercent && contextPercent > 0) {
          const minimalTokenUsage = {
            context_usage_percent: contextPercent,
            model_context_window: llmData?.metadata?.model_context_window as number | undefined,
            context_window_usage: llmData?.metadata?.current_context_window_usage as number | undefined,
          }
          return {
            contextUsagePercent: contextPercent,
            latestTokenUsage: minimalTokenUsage
          }
        }
      }
    }

    return { contextUsagePercent: null, latestTokenUsage: null }
  }, [tabEvents, isSubAgentEvent])
  
  // Always use tab-specific config (ChatInput is only in chat mode)
  // Memoize to prevent unnecessary re-renders when other config values change
  const chatFileContext = useMemo(() => tabConfig?.fileContext || [], [tabConfig?.fileContext])
  // Use ?? instead of || to preserve false values (user's selection)
  // Only default to false if the value is undefined/null (not explicitly set)
  const useToolSearchMode = useMemo(() => tabConfig?.useToolSearchMode ?? false, [tabConfig?.useToolSearchMode])
  const enableWorkspaceAccess = useMemo(() => tabConfig?.enableWorkspaceAccess ?? true, [tabConfig?.enableWorkspaceAccess])
  const enableBrowserAccess = useMemo(() => tabConfig?.enableBrowserAccess ?? false, [tabConfig?.enableBrowserAccess])
  const useCdp = useMemo(() => tabConfig?.useCdp ?? false, [tabConfig?.useCdp])
  const cdpPort = useMemo(() => tabConfig?.cdpPort ?? 9222, [tabConfig?.cdpPort])
  const isLocalMode = useCapabilitiesStore(state => state.capabilities?.local_mode ?? false)
  const [cdpConnected, setCdpConnected] = useState<boolean | null>(null)
  const [cdpChecking, setCdpChecking] = useState(false)
  const [showCdpPopup, setShowCdpPopup] = useState(false)

  // File context operations (always update tab config)
  const removeFileFromContext = useCallback((path: string) => {
    if (activeTabId && activeTab) {
      const newFileContext = chatFileContext.filter(f => f.path !== path)
      setTabConfig(activeTabId, { fileContext: newFileContext })
    }
  }, [activeTabId, activeTab, chatFileContext, setTabConfig])
  
  const clearFileContext = useCallback(() => {
    if (activeTabId) {
      setTabConfig(activeTabId, { fileContext: [] })
    }
  }, [activeTabId, setTabConfig])
  
  const addFileToContext = useCallback((file: { name: string; path: string; type: 'file' | 'folder' }) => {
    if (activeTabId && activeTab) {
      const newFileContext = [...chatFileContext, file]
      setTabConfig(activeTabId, { fileContext: newFileContext })
    }
  }, [activeTabId, activeTab, chatFileContext, setTabConfig])
  
  const setUseCodeExecutionMode = useCallback((enabled: boolean) => {
    if (activeTabId) {
      setTabConfig(activeTabId, { useCodeExecutionMode: enabled })
    }
  }, [activeTabId, setTabConfig])

  const setUseToolSearchMode = useCallback((enabled: boolean) => {
    if (activeTabId) {
      setTabConfig(activeTabId, { useToolSearchMode: enabled })
    }
  }, [activeTabId, setTabConfig])

  const setEnableWorkspaceAccess = useCallback((enabled: boolean) => {
    if (activeTabId) {
      setTabConfig(activeTabId, { enableWorkspaceAccess: enabled })
      // Open workspace sidebar when workspace access is enabled
      if (enabled) {
        setWorkspaceMinimized(false)
      }
    }
  }, [activeTabId, setTabConfig, setWorkspaceMinimized])

  const setEnableBrowserAccess = useCallback((enabled: boolean) => {
    if (activeTabId) {
      // When browser is enabled, also enable workspace (browser tool lives in workspace category)
      const updates: { enableBrowserAccess: boolean; enableWorkspaceAccess?: boolean } = { enableBrowserAccess: enabled }
      if (enabled) {
        updates.enableWorkspaceAccess = true
        setWorkspaceMinimized(false) // Open workspace sidebar
      }
      setTabConfig(activeTabId, updates)
    }
  }, [activeTabId, setTabConfig, setWorkspaceMinimized])

  const setUseCdp = useCallback((enabled: boolean) => {
    if (activeTabId) {
      setTabConfig(activeTabId, { useCdp: enabled })
    }
  }, [activeTabId, setTabConfig])

  const setCdpPort = useCallback((port: number) => {
    if (activeTabId) {
      setTabConfig(activeTabId, { cdpPort: port })
    }
  }, [activeTabId, setTabConfig])

  const checkCdpConnection = useCallback(async (port: number) => {
    setCdpChecking(true)
    setCdpConnected(null)
    try {
      const result = await agentApi.checkCdpPort(port)
      setCdpConnected(result.connected)
    } catch {
      setCdpConnected(false)
    } finally {
      setCdpChecking(false)
    }
  }, [])

  // Auto-check CDP connection when CDP is toggled on or port changes
  useEffect(() => {
    if (!useCdp || !enableBrowserAccess) {
      setCdpConnected(null)
      return
    }
    const timer = setTimeout(() => {
      checkCdpConnection(cdpPort)
    }, 500)
    return () => clearTimeout(timer)
  }, [useCdp, cdpPort, enableBrowserAccess, checkCdpConnection])

  // Get preset info for chat mode
  const { getActivePreset, activePresetIds, customPresets, predefinedPresets } = usePresetApplication()
  
  // Get input text from tab config (source of truth for persistence)
  const storedInputText = tabConfig?.inputText || ''

  // Local state for immediate UI updates (prevents Zustand updates on every keystroke)
  const [localInputText, setLocalInputText] = useState(storedInputText)
  const inputText = localInputText

  // Debounce ref for syncing to store
  const syncToStoreTimeoutRef = useRef<NodeJS.Timeout | null>(null)

  // Sync local state FROM store when store changes externally (preset sync, etc.)
  useLayoutEffect(() => {
    // Only sync if store value differs and we're not in the middle of typing
    if (storedInputText !== localInputText && !syncToStoreTimeoutRef.current) {
      setLocalInputText(storedInputText)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [storedInputText]) // Intentionally exclude localInputText to avoid loops

  // Cleanup timeout refs on unmount
  useEffect(() => {
    return () => {
      if (syncToStoreTimeoutRef.current) {
        clearTimeout(syncToStoreTimeoutRef.current)
      }
    }
  }, [])

  // Get queued messages from tab config
  const queuedMessages = useMemo(() => tabConfig?.queuedMessages || [], [tabConfig?.queuedMessages])
  
  // State for summarization
  const [isSummarizing, setIsSummarizing] = useState(false)
  
  const {
    enabledServers: availableServers,
    setChatSelectedServers
  } = useMCPStore()
  
  // Use tab-specific servers - memoize to prevent re-renders
  const manualSelectedServers = useMemo(() => tabConfig?.selectedServers || [], [tabConfig?.selectedServers])
  
  // Server operations (always update tab config AND sync to chat-specific MCP store)
  // This ensures new chat tabs inherit the user's manual server selection
  const onManualServerToggle = useCallback((server: string) => {
    if (activeTabId) {
      // Remove "NO_SERVERS" if it exists (when selecting a real server)
      const serversWithoutNoServers = manualSelectedServers.filter(s => s !== "NO_SERVERS")
      
      const newServers = serversWithoutNoServers.includes(server)
        ? serversWithoutNoServers.filter(s => s !== server)
        : [...serversWithoutNoServers, server]
      
      setTabConfig(activeTabId, { selectedServers: newServers })
      // Sync to chat-specific MCP store so new chat tabs inherit this selection
      setChatSelectedServers(newServers)
    }
  }, [activeTabId, manualSelectedServers, setTabConfig, setChatSelectedServers])
  
  const onSelectAllServers = useCallback(() => {
    if (activeTabId) {
      // availableServers is already an array of server names (strings)
      const allServers = [...availableServers]
      setTabConfig(activeTabId, { selectedServers: allServers })
      // Sync to chat-specific MCP store so new chat tabs inherit this selection
      setChatSelectedServers(allServers)
    }
  }, [activeTabId, availableServers, setTabConfig, setChatSelectedServers])

  const onClearAllServers = useCallback(() => {
    if (activeTabId) {
      setTabConfig(activeTabId, { selectedServers: ["NO_SERVERS"] })
      // Sync to chat-specific MCP store so new chat tabs inherit this selection
      setChatSelectedServers(["NO_SERVERS"])
    }
  }, [activeTabId, setTabConfig, setChatSelectedServers])

  // Auto-enable tool search mode in chat mode when more than 2 MCP servers are selected
  useEffect(() => {
    if (selectedModeCategory !== 'chat' || !activeTabId) return
    const realServers = manualSelectedServers.filter(s => s !== 'NO_SERVERS')
    if (realServers.length > 2 && !useToolSearchMode) {
      setTabConfig(activeTabId, { useToolSearchMode: true })
    }
  }, [manualSelectedServers, selectedModeCategory, activeTabId, useToolSearchMode, setTabConfig])

  // Use tab-specific skills - memoize to prevent re-renders
  const selectedSkills = useMemo(() => tabConfig?.selectedSkills || [], [tabConfig?.selectedSkills])

  // Skill operations (update tab config)
  const onSkillToggle = useCallback((skillFolderName: string) => {
    if (activeTabId) {
      const newSkills = selectedSkills.includes(skillFolderName)
        ? selectedSkills.filter(s => s !== skillFolderName)
        : [...selectedSkills, skillFolderName]
      setTabConfig(activeTabId, { selectedSkills: newSkills })
    }
  }, [activeTabId, selectedSkills, setTabConfig])

  const onSelectAllSkills = useCallback((allSkillNames: string[]) => {
    if (activeTabId) {
      setTabConfig(activeTabId, { selectedSkills: allSkillNames })
    }
  }, [activeTabId, setTabConfig])

  const onClearAllSkills = useCallback(() => {
    if (activeTabId) {
      setTabConfig(activeTabId, { selectedSkills: [] })
    }
  }, [activeTabId, setTabConfig])

  // Use tab-specific secrets - memoize to prevent re-renders
  const selectedSecrets = useMemo(() => tabConfig?.selectedSecrets || [], [tabConfig?.selectedSecrets])


  // Secret operations (update tab config)
  const onSecretToggle = useCallback((secretId: string) => {
    if (activeTabId) {
      const newSecrets = selectedSecrets.includes(secretId)
        ? selectedSecrets.filter(s => s !== secretId)
        : [...selectedSecrets, secretId]
      setTabConfig(activeTabId, { selectedSecrets: newSecrets })
    }
  }, [activeTabId, selectedSecrets, setTabConfig])

  const onSelectAllSecrets = useCallback((allSecretIds: string[]) => {
    if (activeTabId) {
      setTabConfig(activeTabId, { selectedSecrets: allSecretIds })
    }
  }, [activeTabId, setTabConfig])

  const onClearAllSecrets = useCallback(() => {
    if (activeTabId) {
      setTabConfig(activeTabId, { selectedSecrets: [] })
    }
  }, [activeTabId, setTabConfig])


  // Use tab-specific sub-agent templates - memoize to prevent re-renders
  const selectedSubAgents = useMemo(() => tabConfig?.selectedSubAgents || [], [tabConfig?.selectedSubAgents])

  // Sub-agent template operations (update tab config)
  const onSubAgentToggle = useCallback((folderName: string) => {
    if (activeTabId) {
      const newSubAgents = selectedSubAgents.includes(folderName)
        ? selectedSubAgents.filter(s => s !== folderName)
        : [...selectedSubAgents, folderName]
      setTabConfig(activeTabId, { selectedSubAgents: newSubAgents })
    }
  }, [activeTabId, selectedSubAgents, setTabConfig])

  const onSelectAllSubAgents = useCallback((allNames: string[]) => {
    if (activeTabId) {
      setTabConfig(activeTabId, { selectedSubAgents: allNames })
    }
  }, [activeTabId, setTabConfig])

  const onClearAllSubAgents = useCallback(() => {
    if (activeTabId) {
      setTabConfig(activeTabId, { selectedSubAgents: [] })
    }
  }, [activeTabId, setTabConfig])

  const {
    availableLLMs,
    getCurrentLLMOption,
    refreshAvailableLLMs: onRefreshAvailableLLMs,
    llmConfigLocked,
    delegationTierConfig
  } = useLLMStore()

  const { scrollToFile } = useWorkspaceStore()
  const { showSkillImport, showMCPDetails, showMCPConfig, showModels, showResume, showWorkflowBuilder, openDialog, closeDialog } = useCommandDialogStore()

  // LLM selection (always update tab config)
  const onPrimaryLLMSelect = useCallback((llm: LLMOption) => {
    if (activeTabId) {
      // Get current config to preserve fallback models and cross-provider fallback
      const currentConfig = tabConfig?.llmConfig || {
        provider: 'openrouter',
        model_id: '',
        fallback_models: [],
        cross_provider_fallback: undefined
      }
      
      const newConfig = {
        ...currentConfig, // ✅ Preserve all existing configuration
        provider: llm.provider as 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure',
        model_id: llm.model
      }
      
      setTabConfig(activeTabId, { llmConfig: newConfig })
    }
  }, [activeTabId, tabConfig?.llmConfig, setTabConfig])

  // Computed values - get LLM option from tab config
  const primaryLLM = useMemo(() => {
    if (tabConfig?.llmConfig) {
      const config = tabConfig.llmConfig
      // Try to find matching LLM in available list for richer metadata
      const foundLLM = availableLLMs.find(llm =>
        llm.provider === config.provider && llm.model === config.model_id
      )
      if (foundLLM) return foundLLM

      // If not found in available list, create option from tab config
      // This preserves user's selection even if model list hasn't loaded
      if (config.provider && config.model_id) {
        return {
          provider: config.provider,
          model: config.model_id,
          label: `${config.provider} - ${config.model_id}`,
          description: 'Selected model'
        }
      }
    }
    return getCurrentLLMOption()
  }, [tabConfig?.llmConfig, availableLLMs, getCurrentLLMOption])
  
  // Preset folder selection
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  
  // Track previous input value to distinguish user deletion from programmatic clearing
  const prevInputTextRef = useRef<string>('')
  
  // File selection dialog state
  const [showFileDialog, setShowFileDialog] = useState(false)
  const [fileDialogPosition, setFileDialogPosition] = useState({ top: 0, left: 0 })
  const [fileSearchQuery, setFileSearchQuery] = useState('')
  const [atPosition, setAtPosition] = useState(-1) // Position of @ in text

  // Command selection dialog state
  const [showCommandDialog, setShowCommandDialog] = useState(false)
  const [commandDialogPosition, setCommandDialogPosition] = useState({ bottom: 0, left: 0 })
  const [commandSearchQuery, setCommandSearchQuery] = useState('')
  const [slashPosition, setSlashPosition] = useState(-1) // Position of / in text

  // Workflow selection dialog state (# trigger)
  const [showWorkflowDialog, setShowWorkflowDialog] = useState(false)
  const [workflowDialogPosition, setWorkflowDialogPosition] = useState({ bottom: 0, left: 0 })
  const [workflowSearchQuery, setWorkflowSearchQuery] = useState('')
  const [hashPosition, setHashPosition] = useState(-1) // Position of # in text

  // Tier config modal state (for plan mode chip)
  const [showTierModal, setShowTierModal] = useState(false)

  // Auto-resize textarea based on content
  const adjustTextareaHeight = useCallback(() => {
    if (textareaRef.current) {
      const textarea = textareaRef.current
      // Reset height to auto to get correct scrollHeight
      textarea.style.height = 'auto'
      // Calculate new height (min 40px for 2 lines, max 100px)
      // scrollHeight includes padding, so we get the exact content height
      const newHeight = Math.min(Math.max(textarea.scrollHeight, 40), 100)
      textarea.style.height = `${newHeight}px`
    }
  }, [])

  // Get active preset for chat mode (used for preset query sync and UI)
  const chatActivePreset = getActivePreset('chat')
  
  // Sync tab config inputText with preset query when preset is selected
  useEffect(() => {
    const activePresetId = activePresetIds['chat']
    
    if (activePresetId && activeTabId) {
      // Find the preset
      const preset = customPresets.find(p => p.id === activePresetId) || 
                    predefinedPresets.find(p => p.id === activePresetId)
      
      if (preset && preset.query) {
        // Sync tab config with preset query
        setTabConfig(activeTabId, { inputText: preset.query })
      }
    } else if (!activePresetId && activeTabId) {
      // No preset active, clear input text
      setTabConfig(activeTabId, { inputText: '' })
    }
  }, [activePresetIds, customPresets, predefinedPresets, activeTabId, setTabConfig])

  // Sync ref with inputText when it changes externally (preset sync, programmatic clearing, etc.)
  useEffect(() => {
    prevInputTextRef.current = inputText || ''
  }, [inputText])

  // Handle auto-run from tab config
  useEffect(() => {
    // Check if autoRun is enabled and we have input text and a session
    if (tabConfig?.autoRun && inputText?.trim() && tabSessionId && !isStreaming) {
      console.log('[ChatInput] Auto-running chat with prompt:', inputText)
      
      // 1. First disable autoRun to prevent loops
      // 2. Clear input text as we're submitting it
      if (activeTabId) {
        setTabConfig(activeTabId, { autoRun: false, inputText: '' })
      }
      
      // 3. Submit the query
      onSubmit(inputText)
    }
  }, [tabConfig?.autoRun, inputText, tabSessionId, isStreaming, activeTabId, setTabConfig, onSubmit])

  // Set initial height and auto-resize textarea when inputText changes
  useEffect(() => {
    if (textareaRef.current) {
      // Set initial height to 2 lines (40px) if empty
      if (!inputText || inputText.trim() === '') {
        textareaRef.current.style.height = '40px'
      } else {
        adjustTextareaHeight()
      }
    }
  }, [inputText, adjustTextareaHeight])
  
  // Set initial height on mount
  useEffect(() => {
    if (textareaRef.current) {
      textareaRef.current.style.height = '40px'
    }
  }, [])

  // Open/close workspace sidebar based on workspace access setting
  // Also close file dialog if workspace access is disabled
  useEffect(() => {
    if (enableWorkspaceAccess) {
      setWorkspaceMinimized(false) // Open workspace sidebar
    } else {
      setWorkspaceMinimized(true) // Close workspace sidebar
      // Close file dialog if workspace access is disabled
      if (showFileDialog) {
        setShowFileDialog(false)
        setAtPosition(-1)
        setFileSearchQuery('')
      }
    }
  }, [enableWorkspaceAccess, setWorkspaceMinimized, showFileDialog])

  // Consolidated query selection logic
  const queryToSubmit = useMemo(() => {
    return inputText
  }, [inputText])

  // Guard to prevent submission before session is ready
  // Allow submission if not streaming, or allow queuing if streaming
  const canSubmit = useMemo(() => {
    return queryToSubmit?.trim() && tabSessionId
  }, [queryToSubmit, tabSessionId])
  
  // Can submit immediately (not streaming) or can queue (streaming)
  const canSubmitImmediately = useMemo(() => {
    return queryToSubmit?.trim() && !isStreaming && tabSessionId
  }, [queryToSubmit, isStreaming, tabSessionId])

  // Ref for debounced file removal check
  const fileRemovalTimeoutRef = useRef<NodeJS.Timeout | null>(null)

  // Memoized handlers to prevent re-creation
  const handleTextChange = useCallback((e: React.ChangeEvent<HTMLTextAreaElement>) => {
    const newValue = e.target.value
    const previousValue = prevInputTextRef.current

    // Update local state immediately for fast UI response
    setLocalInputText(newValue)

    // Debounce sync to Zustand store (300ms delay)
    if (syncToStoreTimeoutRef.current) {
      clearTimeout(syncToStoreTimeoutRef.current)
    }
    syncToStoreTimeoutRef.current = setTimeout(() => {
      if (activeTabId) {
        setTabConfig(activeTabId, { inputText: newValue })
      }
      syncToStoreTimeoutRef.current = null
    }, 300)

    // Update ref for next comparison
    prevInputTextRef.current = newValue

    // Auto-resize textarea
    adjustTextareaHeight()

    const cursorPosition = e.target.selectionStart || 0
    const textBeforeCursor = newValue.substring(0, cursorPosition)

    const lastSlashIndex = textBeforeCursor.lastIndexOf('/')
    const lastAtIndex = textBeforeCursor.lastIndexOf('@')
    const lastHashIndex = textBeforeCursor.lastIndexOf('#')

    // If @ appears before the current /, the / is part of a path (e.g. "@ workflow /") — stay in file dialog
    const slashIsPartOfAtPath = lastAtIndex >= 0 && lastSlashIndex > lastAtIndex

    const slashDistance = lastSlashIndex >= 0 ? cursorPosition - lastSlashIndex : Infinity
    const atDistance = lastAtIndex >= 0 ? cursorPosition - lastAtIndex : Infinity
    const hashDistance = lastHashIndex >= 0 ? cursorPosition - lastHashIndex : Infinity

    // Check if # is a markdown heading (at line start AND followed by a space) — don't trigger dialog for headings
    // e.g. "# Heading" is a heading, but "#workflow" is a workflow trigger
    const charAfterHash = lastHashIndex >= 0 ? newValue[lastHashIndex + 1] : undefined
    const hashIsAtLineStart = lastHashIndex >= 0 && (lastHashIndex === 0 || textBeforeCursor[lastHashIndex - 1] === '\n')
    const hashIsHeading = hashIsAtLineStart && charAfterHash === ' '

    // Find the closest trigger to cursor
    const closestTrigger = Math.min(slashDistance, atDistance, hashDistance)

    // Check for / command (only when / is not part of an @ path)
    if (!slashIsPartOfAtPath && lastSlashIndex >= 0 && closestTrigger === slashDistance) {
      const textAfterSlash = textBeforeCursor.substring(lastSlashIndex + 1)
      const hasValidSlash = textAfterSlash === '' || textAfterSlash.match(/^[a-zA-Z0-9_]*$/)

      if (hasValidSlash) {
        setSlashPosition(lastSlashIndex)
        setCommandSearchQuery(textAfterSlash)
        setShowCommandDialog(true)
        setShowFileDialog(false)
        setShowWorkflowDialog(false)

        // Calculate dialog position — anchor from bottom so it grows upward
        const textarea = e.target
        const rect = textarea.getBoundingClientRect()

        setCommandDialogPosition({
          bottom: window.innerHeight - rect.top + 8,
          left: rect.left + window.scrollX
        })
      } else {
        setShowCommandDialog(false)
        setSlashPosition(-1)
        setCommandSearchQuery('')
      }
    }
    // Check for # workflow trigger (not a markdown heading, in chat/multi-agent mode)
    else if (!hashIsHeading && lastHashIndex >= 0 && closestTrigger === hashDistance) {
      const textAfterHash = textBeforeCursor.substring(lastHashIndex + 1)
      const hasValidHash = textAfterHash === '' || textAfterHash.match(/^[a-zA-Z0-9_-]*$/)

      if (hasValidHash) {
        setHashPosition(lastHashIndex)
        setWorkflowSearchQuery(textAfterHash)
        setShowWorkflowDialog(true)
        setShowCommandDialog(false)
        setShowFileDialog(false)

        // Calculate dialog position — anchor from bottom so it grows upward
        const textarea = e.target
        const rect = textarea.getBoundingClientRect()

        setWorkflowDialogPosition({
          bottom: window.innerHeight - rect.top + 8,
          left: rect.left + window.scrollX
        })
      } else {
        setShowWorkflowDialog(false)
        setHashPosition(-1)
        setWorkflowSearchQuery('')
      }
    }
    // Check for @ symbol and update file dialog state (only if no other dialog active and workspace access is enabled)
    else if (lastAtIndex >= 0 && !showCommandDialog && !showWorkflowDialog && enableWorkspaceAccess) {
      const textAfterAt = textBeforeCursor.substring(lastAtIndex + 1)
      const hasValidAt = textAfterAt === '' || textAfterAt.match(/^[a-zA-Z0-9/._\-\\]*$/)

      if (hasValidAt) {
        setAtPosition(lastAtIndex)
        setFileSearchQuery(textAfterAt)
        setShowFileDialog(true)

        // Calculate dialog position - smart positioning to avoid overlap
        const textarea = e.target
        const rect = textarea.getBoundingClientRect()
        const dialogHeight = 320 // Approximate dialog height
        const spaceAbove = rect.top
        const spaceBelow = window.innerHeight - rect.bottom

        // Position above if there's more space above, otherwise position below
        const shouldPositionAbove = spaceAbove > dialogHeight || spaceAbove > spaceBelow

        setFileDialogPosition({
          top: shouldPositionAbove
            ? rect.top + window.scrollY - dialogHeight - 10 // Above with gap
            : rect.bottom + window.scrollY + 10, // Below with gap
          left: rect.left + window.scrollX
        })
      } else {
        setShowFileDialog(false)
        setAtPosition(-1)
        setFileSearchQuery('')
      }
    } else {
      // Close all dialogs if none is active
      setShowFileDialog(false)
      setAtPosition(-1)
      setFileSearchQuery('')
      setShowCommandDialog(false)
      setSlashPosition(-1)
      setCommandSearchQuery('')
      setShowWorkflowDialog(false)
      setHashPosition(-1)
      setWorkflowSearchQuery('')
    }

    // Debounce file reference removal check (500ms delay)
    // This prevents expensive iteration on every keystroke
    if (fileRemovalTimeoutRef.current) {
      clearTimeout(fileRemovalTimeoutRef.current)
    }
    fileRemovalTimeoutRef.current = setTimeout(() => {
      // Check if any @file references were removed and remove them from context
      // Only remove if:
      // 1. The file reference existed in the previous input
      // 2. The file reference is missing in the new input
      // 3. The new input is shorter than the previous (user deleted it, not cleared programmatically)
      if (previousValue.length > newValue.length) {
        const removedFiles: string[] = []
        chatFileContext.forEach((file: { path: string }) => {
          const fileReference = '@' + file.path
          const wasInPrevious = previousValue.includes(fileReference)
          const isInNew = newValue.includes(fileReference)

          if (wasInPrevious && !isInNew) {
            removedFiles.push(file.path)
          }
        })
        removedFiles.forEach(filePath => {
          removeFileFromContext(filePath)
        })

        // Check if any #workflow references were removed
        if (activeTabId) {
          const currentWorkflowContext = useChatStore.getState().getTabConfig(activeTabId)?.workflowContext || []
          const removedWorkflows = currentWorkflowContext.filter(w => {
            const wRef = '#' + w.label
            return previousValue.includes(wRef) && !newValue.includes(wRef)
          })
          if (removedWorkflows.length > 0) {
            const remaining = currentWorkflowContext.filter(w => !removedWorkflows.some(r => r.presetId === w.presetId))
            setTabConfig(activeTabId, { workflowContext: remaining })
          }
        }
      }
      fileRemovalTimeoutRef.current = null
    }, 500)
  }, [chatFileContext, removeFileFromContext, showCommandDialog, showWorkflowDialog, activeTabId, setTabConfig, enableWorkspaceAccess, adjustTextareaHeight])

  // Handle manual summarization
  // If messageToSendAfter is provided, it will be sent as a user message after summarization completes
  const handleSummarize = useCallback(async (messageToSendAfter?: string) => {
    if (!tabSessionId || isSummarizing || isStreaming) {
      return
    }

    setIsSummarizing(true)
    try {
      const response = await agentApi.summarizeConversation(tabSessionId)
      addToast(`Summarized: ${response.original_count} → ${response.new_count} messages (−${response.reduced_by})`, 'success')
      
      // If there's a message to send after summarization, send it now
      if (messageToSendAfter && messageToSendAfter.trim() && tabSessionId) {
        // Small delay to ensure summarization is fully processed
        setTimeout(() => {
          onSubmit(messageToSendAfter.trim())
        }, 500)
      }
    } catch (error) {
      console.error('[SUMMARIZATION] Error:', error)
      const errorMessage = error instanceof Error ? error.message : 'Unknown error'
      addToast(`Failed to summarize: ${errorMessage}`, 'error')
    } finally {
      setIsSummarizing(false)
    }
  }, [tabSessionId, isSummarizing, isStreaming, onSubmit, addToast])

  // Handle manual context compaction (context editing)
  // If messageToSendAfter is provided, it will be sent as a user message after compaction completes
  const handleCompact = useCallback(async (messageToSendAfter?: string) => {
    if (!tabSessionId || isSummarizing || isStreaming) {
      return
    }

    setIsSummarizing(true) // Reuse the same loading state
    try {
      const response = await agentApi.compactContext(tabSessionId)
      console.log('[CONTEXT_EDITING] Success:', response)
      addToast(`Compacted ${response.compacted_count} responses, saved ${response.total_tokens_saved?.toLocaleString() || 0} tokens`, 'success')
      
      // If there's a message to send after compaction, send it now
      if (messageToSendAfter && messageToSendAfter.trim() && tabSessionId) {
        // Small delay to ensure compaction is fully processed
        setTimeout(() => {
          onSubmit(messageToSendAfter.trim())
        }, 500)
      }
    } catch (error) {
      console.error('[CONTEXT_EDITING] Error:', error)
      const errorMessage = error instanceof Error ? error.message : 'Unknown error'
      addToast(`Failed to compact: ${errorMessage}`, 'error')
    } finally {
      setIsSummarizing(false)
    }
  }, [tabSessionId, isSummarizing, isStreaming, onSubmit, addToast])

  const handleKeyDown = useCallback((e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    // If any selection dialog is open, let it handle keyboard events
    if (showCommandDialog || showFileDialog || showWorkflowDialog) {
      // Don't prevent default for arrow keys, enter, escape - let dialog handle them
      if (['ArrowUp', 'ArrowDown', 'Enter', 'Escape'].includes(e.key)) {
        return
      }
    }

    // Handle Escape key to stop streaming (when no dialogs are open)
    if (e.key === 'Escape' && isStreaming) {
      e.preventDefault()
      onStopStreaming()
      return
    }

    // Handle normal Enter to submit
    if (e.key === 'Enter' && !e.ctrlKey && !e.metaKey) {
      e.preventDefault()

      // Check for slash commands
      const trimmedQuery = queryToSubmit?.trim() || ''
      const summarizeIndex = trimmedQuery.indexOf('/summarize')
      const compactIndex = trimmedQuery.indexOf('/compact')
      
      if (summarizeIndex >= 0) {
        // Handle summarize command
        if (tabSessionId && !isSummarizing && !isStreaming) {
          // Extract text before /summarize
          const textBeforeSummarize = trimmedQuery.substring(0, summarizeIndex).trim()

          // If there's text before /summarize, send it after summarization
          // Otherwise, just summarize
          handleSummarize(textBeforeSummarize || undefined)
          
          // Clear input after command (both local and store)
          setLocalInputText('')
          
          // Clear any pending store sync to prevent overwriting the empty state
          if (syncToStoreTimeoutRef.current) {
            clearTimeout(syncToStoreTimeoutRef.current)
            syncToStoreTimeoutRef.current = null
          }

          if (activeTabId) {
            setTabConfig(activeTabId, { inputText: '' })
          }
        }
        return
      }

      if (compactIndex >= 0) {
        // Handle compact command
        if (tabSessionId && !isSummarizing && !isStreaming) {
          const textBeforeCompact = trimmedQuery.substring(0, compactIndex).trim()
          handleCompact(textBeforeCompact || undefined)
          
          // Clear input after command (both local and store)
          setLocalInputText('')

          // Clear any pending store sync to prevent overwriting the empty state
          if (syncToStoreTimeoutRef.current) {
            clearTimeout(syncToStoreTimeoutRef.current)
            syncToStoreTimeoutRef.current = null
          }

          if (activeTabId) {
            setTabConfig(activeTabId, { inputText: '' })
          }
        }
        return
      }

      // Handle /build-skill command
      const buildSkillIndex = trimmedQuery.indexOf('/build-skill')
      if (buildSkillIndex >= 0) {
        const textAfterCommand = trimmedQuery.substring(buildSkillIndex + '/build-skill'.length).trim()

        // Auto-add skill-creator to current tab's selectedSkills
        if (activeTabId) {
          const currentSkills = tabConfig?.selectedSkills || []
          if (!currentSkills.includes('skill-creator')) {
            setTabConfig(activeTabId, { selectedSkills: [...currentSkills, 'skill-creator'] })
          }
        }

        // Expand skills/ and skills/custom/ folders in workspace
        const wsStore = useWorkspaceStore.getState()
        const expanded = new Set(wsStore.expandedFolders)
        expanded.add('skills')
        expanded.add('skills/custom')
        wsStore.setExpandedFolders(expanded)

        // Clear input after command (both local and store)
        setLocalInputText('')
        if (syncToStoreTimeoutRef.current) {
          clearTimeout(syncToStoreTimeoutRef.current)
          syncToStoreTimeoutRef.current = null
        }
        if (activeTabId) {
          setTabConfig(activeTabId, { inputText: '' })
        }

        // Submit: always include skill-creator path context, plus user's text if provided
        const skillContext = 'Refer to the skill-creator skill at skills/custom/skill-creator/SKILL.md for instructions on how to build skills.'
        const message = textAfterCommand
          ? `${textAfterCommand}\n\n${skillContext}`
          : `I want to build a skill based on our conversation. ${skillContext}`
        onSubmit(message)
        return
      }

      // Handle /build-subagent command
      const buildSubAgentIndex = trimmedQuery.indexOf('/build-subagent')
      if (buildSubAgentIndex >= 0) {
        const textAfterCommand = trimmedQuery.substring(buildSubAgentIndex + '/build-subagent'.length).trim()

        // Auto-add subagent-creator to current tab's selectedSkills
        if (activeTabId) {
          const currentSkills = tabConfig?.selectedSkills || []
          if (!currentSkills.includes('subagent-creator') && !currentSkills.includes('custom/subagent-creator')) {
            setTabConfig(activeTabId, { selectedSkills: [...currentSkills, 'custom/subagent-creator'] })
          }
        }

        // Expand subagents/ and subagents/custom/ folders in workspace
        const wsStore2 = useWorkspaceStore.getState()
        const expanded2 = new Set(wsStore2.expandedFolders)
        expanded2.add('subagents')
        expanded2.add('subagents/custom')
        wsStore2.setExpandedFolders(expanded2)

        // Clear input after command
        setLocalInputText('')
        if (syncToStoreTimeoutRef.current) {
          clearTimeout(syncToStoreTimeoutRef.current)
          syncToStoreTimeoutRef.current = null
        }
        if (activeTabId) {
          setTabConfig(activeTabId, { inputText: '' })
        }

        // Submit with sub-agent builder context
        const saContext = 'You are in Sub-Agent Builder mode. Create a new sub-agent template in subagents/custom/. Follow the SUBAGENT.md format with YAML frontmatter (name, description, default_reasoning_level, default_tool_mode, skills, servers) and markdown instructions.'
        const message = textAfterCommand
          ? `${textAfterCommand}\n\n${saContext}`
          : `I want to build a sub-agent template. ${saContext}`
        onSubmit(message)
        return
      }

      // Handle /add-skill command — open the import dialog
      if (trimmedQuery.indexOf('/add-skill') >= 0) {
        // Clear input
        setLocalInputText('')
        if (syncToStoreTimeoutRef.current) {
          clearTimeout(syncToStoreTimeoutRef.current)
          syncToStoreTimeoutRef.current = null
        }
        if (activeTabId) {
          setTabConfig(activeTabId, { inputText: '' })
        }

        // Open the skill import dialog
        openDialog('skillImport')
        return
      }

      // Handle /mcp-add command — open MCP config editor (must be before /mcp check)
      if (trimmedQuery.indexOf('/mcp-add') >= 0) {
        setLocalInputText('')
        if (syncToStoreTimeoutRef.current) {
          clearTimeout(syncToStoreTimeoutRef.current)
          syncToStoreTimeoutRef.current = null
        }
        if (activeTabId) {
          setTabConfig(activeTabId, { inputText: '' })
        }
        useAppStore.getState().setWorkspaceMinimized(true)
        openDialog('mcpConfig')
        return
      }

      // Handle /mcp command — open MCP server details list
      if (trimmedQuery.indexOf('/mcp') >= 0) {
        setLocalInputText('')
        if (syncToStoreTimeoutRef.current) {
          clearTimeout(syncToStoreTimeoutRef.current)
          syncToStoreTimeoutRef.current = null
        }
        if (activeTabId) {
          setTabConfig(activeTabId, { inputText: '' })
        }
        useAppStore.getState().setWorkspaceMinimized(true)
        openDialog('mcpDetails')
        return
      }

      // Handle /models command — open LLM model config
      if (trimmedQuery.indexOf('/models') >= 0) {
        setLocalInputText('')
        if (syncToStoreTimeoutRef.current) {
          clearTimeout(syncToStoreTimeoutRef.current)
          syncToStoreTimeoutRef.current = null
        }
        if (activeTabId) {
          setTabConfig(activeTabId, { inputText: '' })
        }
        useAppStore.getState().setWorkspaceMinimized(true)
        openDialog('models')
        return
      }

      // Handle /resume command — show previous conversations
      if (trimmedQuery.indexOf('/resume') >= 0) {
        setLocalInputText('')
        if (syncToStoreTimeoutRef.current) {
          clearTimeout(syncToStoreTimeoutRef.current)
          syncToStoreTimeoutRef.current = null
        }
        if (activeTabId) {
          setTabConfig(activeTabId, { inputText: '' })
        }
        openDialog('resume')
        return
      }

      if (canSubmitImmediately) {
        // Clear input text immediately (both local and store)
        setLocalInputText('')
        
        // Clear any pending store sync to prevent overwriting the empty state
        if (syncToStoreTimeoutRef.current) {
          clearTimeout(syncToStoreTimeoutRef.current)
          syncToStoreTimeoutRef.current = null
        }
        
        if (activeTabId) {
          setTabConfig(activeTabId, { inputText: '' })
        }
        onSubmit(queryToSubmit)
      } else if (canSubmit && isStreaming) {
        // Queue message when streaming - clear input (both local and store)
        setLocalInputText('')
        
        // Clear any pending store sync to prevent overwriting the empty state
        if (syncToStoreTimeoutRef.current) {
          clearTimeout(syncToStoreTimeoutRef.current)
          syncToStoreTimeoutRef.current = null
        }

        if (activeTabId) {
          const currentQueued = tabConfig?.queuedMessages || []
          setTabConfig(activeTabId, {
            inputText: '',
            queuedMessages: [...currentQueued, queryToSubmit.trim()]
          })
        }
      }
    }
    // Handle CTRL+Enter (Windows/Linux) or CMD+Enter (Mac) to add new line
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault()
      const textarea = e.target as HTMLTextAreaElement
      const start = textarea.selectionStart
      const end = textarea.selectionEnd
      const newValue = inputText.substring(0, start) + '\n' + inputText.substring(end)
      // Update local state immediately for fast UI
      setLocalInputText(newValue)

      // Set cursor position after the newline
      setTimeout(() => {
        textarea.selectionStart = textarea.selectionEnd = start + 1
      }, 0)
    }
  }, [inputText, onSubmit, showFileDialog, showCommandDialog, showWorkflowDialog, tabSessionId, canSubmit, canSubmitImmediately, queryToSubmit, isSummarizing, isStreaming, handleSummarize, handleCompact, activeTabId, setTabConfig, tabConfig?.queuedMessages, onStopStreaming, openDialog, tabConfig?.selectedSkills])

  const handleSubmit = useCallback((e: React.FormEvent) => {
    e.preventDefault()

    // Check for slash commands
    const trimmedQuery = queryToSubmit?.trim() || ''
    const summarizeIndex = trimmedQuery.indexOf('/summarize')
    const compactIndex = trimmedQuery.indexOf('/compact')

    if (summarizeIndex >= 0) {
      if (tabSessionId && !isSummarizing && !isStreaming) {
        const textBeforeSummarize = trimmedQuery.substring(0, summarizeIndex).trim()
        handleSummarize(textBeforeSummarize || undefined)

        // Clear input after command (both local and store)
        setLocalInputText('')

        // Clear any pending store sync to prevent overwriting the empty state
        if (syncToStoreTimeoutRef.current) {
          clearTimeout(syncToStoreTimeoutRef.current)
          syncToStoreTimeoutRef.current = null
        }

        if (activeTabId) {
          setTabConfig(activeTabId, { inputText: '' })
        }
      }
      return
    }

    if (compactIndex >= 0) {
      if (tabSessionId && !isSummarizing && !isStreaming) {
        const textBeforeCompact = trimmedQuery.substring(0, compactIndex).trim()
        handleCompact(textBeforeCompact || undefined)
        
        // Clear input after command (both local and store)
        setLocalInputText('')
        
        // Clear any pending store sync to prevent overwriting the empty state
        if (syncToStoreTimeoutRef.current) {
          clearTimeout(syncToStoreTimeoutRef.current)
          syncToStoreTimeoutRef.current = null
        }

        if (activeTabId) {
          setTabConfig(activeTabId, { inputText: '' })
        }
      }
      return
    }

    if (canSubmitImmediately) {
      setLocalInputText('')
      
      // Clear any pending store sync to prevent overwriting the empty state
      if (syncToStoreTimeoutRef.current) {
        clearTimeout(syncToStoreTimeoutRef.current)
        syncToStoreTimeoutRef.current = null
      }

      if (activeTabId) {
        setTabConfig(activeTabId, { inputText: '' })
      }
      onSubmit(queryToSubmit)
    } else if (canSubmit && isStreaming) {
      setLocalInputText('')
      
      // Clear any pending store sync to prevent overwriting the empty state
      if (syncToStoreTimeoutRef.current) {
        clearTimeout(syncToStoreTimeoutRef.current)
        syncToStoreTimeoutRef.current = null
      }

      if (activeTabId) {
        const currentQueued = tabConfig?.queuedMessages || []
        setTabConfig(activeTabId, {
          inputText: '',
          queuedMessages: [...currentQueued, queryToSubmit.trim()]
        })
      }
    }
  }, [canSubmit, canSubmitImmediately, activeTabId, tabSessionId, queryToSubmit, onSubmit, isSummarizing, isStreaming, handleSummarize, handleCompact, setTabConfig, tabConfig?.queuedMessages])

  // Command selection handler - executes commands directly
  const handleCommandSelect = useCallback((command: string) => {
    if (!activeTabId) return

    // Close dialog first
    setShowCommandDialog(false)
    setSlashPosition(-1)
    setCommandSearchQuery('')

    // Get text before the slash command (if any)
    const beforeSlash = slashPosition >= 0 ? inputText.substring(0, slashPosition).trim() : ''

    // Clear input
    setLocalInputText('')
    if (syncToStoreTimeoutRef.current) {
      clearTimeout(syncToStoreTimeoutRef.current)
      syncToStoreTimeoutRef.current = null
    }
    setTabConfig(activeTabId, { inputText: '' })

    // Execute commands directly
    switch (command) {
      case 'summarize':
        if (tabSessionId && !isSummarizing && !isStreaming) {
          handleSummarize(beforeSlash || undefined)
        }
        break
      case 'compact':
        if (tabSessionId && !isSummarizing && !isStreaming) {
          handleCompact(beforeSlash || undefined)
        }
        break
      case 'build-skill': {
        // Auto-add skill-creator to current tab's selectedSkills
        const currentSkills = tabConfig?.selectedSkills || []
        if (!currentSkills.includes('skill-creator')) {
          setTabConfig(activeTabId, { selectedSkills: [...currentSkills, 'skill-creator'] })
        }
        // Expand skills folders in workspace
        const wsStore = useWorkspaceStore.getState()
        const expanded = new Set(wsStore.expandedFolders)
        expanded.add('skills')
        expanded.add('skills/custom')
        wsStore.setExpandedFolders(expanded)
        // Submit with skill context
        const skillContext = 'Refer to the skill-creator skill at skills/custom/skill-creator/SKILL.md for instructions on how to build skills.'
        const message = beforeSlash
          ? `${beforeSlash}\n\n${skillContext}`
          : `I want to build a skill based on our conversation. ${skillContext}`
        onSubmit(message)
        break
      }
      case 'build-subagent': {
        // Auto-add subagent-creator to current tab's selectedSkills
        const currentSkills2 = tabConfig?.selectedSkills || []
        if (!currentSkills2.includes('subagent-creator') && !currentSkills2.includes('custom/subagent-creator')) {
          setTabConfig(activeTabId, { selectedSkills: [...currentSkills2, 'custom/subagent-creator'] })
        }
        // Expand subagents folders in workspace
        const wsStore2 = useWorkspaceStore.getState()
        const expanded2 = new Set(wsStore2.expandedFolders)
        expanded2.add('subagents')
        expanded2.add('subagents/custom')
        wsStore2.setExpandedFolders(expanded2)
        // Submit with sub-agent builder context
        const saContext = 'You are in Sub-Agent Builder mode. Create a new sub-agent template in subagents/custom/. Follow the SUBAGENT.md format with YAML frontmatter (name, description, default_reasoning_level, default_tool_mode, skills, servers) and markdown instructions.'
        const saMessage = beforeSlash
          ? `${beforeSlash}\n\n${saContext}`
          : `I want to build a sub-agent template. ${saContext}`
        onSubmit(saMessage)
        break
      }
      case 'add-skill':
        openDialog('skillImport')
        break
      case 'mcp-add':
        useAppStore.getState().setWorkspaceMinimized(true)
        openDialog('mcpConfig')
        break
      case 'mcp':
        useAppStore.getState().setWorkspaceMinimized(true)
        openDialog('mcpDetails')
        break
      case 'models':
        useAppStore.getState().setWorkspaceMinimized(true)
        openDialog('models')
        break
      case 'resume':
        openDialog('resume')
        break
      case 'spawn': {
        useAppStore.getState().setDelegationMode('spawn')
        addToast(
          'Simple delegation enabled - Agent can delegate tasks to sub-agents',
          'success'
        )
        break
      }
      case 'nospawn': {
        useAppStore.getState().setDelegationMode('off')
        addToast(
          'Sub-agent delegation disabled',
          'success'
        )
        break
      }
      case 'workflow-builder':
        openDialog('workflowBuilder')
        break
      default:
        // For unknown commands, insert into text (fallback)
        if (textareaRef.current) {
          const afterSearch = inputText.substring((slashPosition >= 0 ? slashPosition : 0) + 1 + commandSearchQuery.length)
          const newQuery = beforeSlash + '/' + command + ' ' + afterSearch
          setLocalInputText(newQuery)
          setTimeout(() => {
            if (textareaRef.current) {
              textareaRef.current.focus()
              const cursorPosition = beforeSlash.length + '/'.length + command.length + ' '.length
              textareaRef.current.setSelectionRange(cursorPosition, cursorPosition)
            }
          }, 0)
        }
    }

    // Focus back to textarea
    setTimeout(() => textareaRef.current?.focus(), 0)
  }, [inputText, slashPosition, commandSearchQuery, activeTabId, tabSessionId, isSummarizing, isStreaming, handleSummarize, handleCompact, tabConfig?.selectedSkills, setTabConfig, onSubmit, openDialog, addToast])

  const handleFileSelect = useCallback((file: PlannerFile) => {
    if (!textareaRef.current || atPosition === -1 || !activeTabId) return

    const beforeAt = inputText.substring(0, atPosition)
    const afterSearch = inputText.substring(atPosition + 1 + fileSearchQuery.length)
    const newQuery = beforeAt + '@' + file.filepath + ' ' + afterSearch

    // Update local state immediately for fast UI
    setLocalInputText(newQuery)
    setShowFileDialog(false)
    setAtPosition(-1)
    setFileSearchQuery('')

    // Add file/folder to context
    const fileContextItem = {
      name: file.filepath.split('/').pop() || file.filepath,
      path: file.filepath,
      type: file.type || 'file' as const
    }

    const isAlreadyInContext = chatFileContext.some((item: { path: string }) => item.path === file.filepath)
    if (!isAlreadyInContext) {
      addFileToContext(fileContextItem)
      scrollToFile(file.filepath)
    }

    // Focus back to textarea and position cursor after the space
    setTimeout(() => {
      if (textareaRef.current) {
        textareaRef.current.focus()
        const cursorPosition = beforeAt.length + '@'.length + file.filepath.length + ' '.length
        textareaRef.current.setSelectionRange(cursorPosition, cursorPosition)
      }
    }, 0)
  }, [inputText, atPosition, fileSearchQuery, chatFileContext, addFileToContext, scrollToFile, activeTabId])

  const handleCommandDialogClose = useCallback(() => {
    setShowCommandDialog(false)
    setSlashPosition(-1)
    setCommandSearchQuery('')
    textareaRef.current?.focus()
  }, [])

  const handleFileDialogClose = useCallback(() => {
    setShowFileDialog(false)
    setAtPosition(-1)
    setFileSearchQuery('')
    textareaRef.current?.focus()
  }, [])

  const handleWorkflowSelect = useCallback((workflow: { presetId: string; label: string; workspacePath: string }) => {
    if (!textareaRef.current || hashPosition === -1 || !activeTabId) return

    const beforeHash = inputText.substring(0, hashPosition)
    const afterSearch = inputText.substring(hashPosition + 1 + workflowSearchQuery.length)
    const newQuery = beforeHash + '#' + workflow.label + ' ' + afterSearch

    // Update local state immediately
    setLocalInputText(newQuery)
    setShowWorkflowDialog(false)
    setHashPosition(-1)
    setWorkflowSearchQuery('')

    // Add workflow to context (avoid duplicates)
    const currentWorkflowContext = useChatStore.getState().getTabConfig(activeTabId)?.workflowContext || []
    const isAlreadyInContext = currentWorkflowContext.some(w => w.presetId === workflow.presetId)
    if (!isAlreadyInContext) {
      const updated = [...currentWorkflowContext, {
        presetId: workflow.presetId,
        label: workflow.label,
        workspacePath: workflow.workspacePath
      }]
      // Auto-enable workspace access when workflow context is selected
      setTabConfig(activeTabId, { workflowContext: updated, enableWorkspaceAccess: true })
    }

    // Sync store
    if (syncToStoreTimeoutRef.current) {
      clearTimeout(syncToStoreTimeoutRef.current)
      syncToStoreTimeoutRef.current = null
    }
    setTabConfig(activeTabId, { inputText: newQuery })

    // Focus back to textarea and position cursor
    setTimeout(() => {
      if (textareaRef.current) {
        textareaRef.current.focus()
        const cursorPosition = beforeHash.length + '#'.length + workflow.label.length + ' '.length
        textareaRef.current.setSelectionRange(cursorPosition, cursorPosition)
      }
    }, 0)
  }, [inputText, hashPosition, workflowSearchQuery, activeTabId, setTabConfig])

  const handleWorkflowDialogClose = useCallback(() => {
    setShowWorkflowDialog(false)
    setHashPosition(-1)
    setWorkflowSearchQuery('')
    textareaRef.current?.focus()
  }, [])

  // When user presses → on a folder in the file dialog, set search context to that folder (input after @ becomes folder path)
  const handleNavigateIntoFolder = useCallback((folderPath: string) => {
    if (atPosition === -1 || !activeTabId) return
    const beforeAt = inputText.substring(0, atPosition + 1)
    const newText = beforeAt + folderPath
    setLocalInputText(newText)
    if (syncToStoreTimeoutRef.current) {
      clearTimeout(syncToStoreTimeoutRef.current)
      syncToStoreTimeoutRef.current = null
    }
    setTabConfig(activeTabId, { inputText: newText })
    setFileSearchQuery(folderPath)
  }, [atPosition, inputText, activeTabId, setTabConfig])

  // Removed editing preset query functionality - not needed for chat mode

  // Check if query is valid (view-only tabs cannot submit)
  const hasValidQuery = Boolean(inputText?.trim())
  const submitButtonDisabled = !hasValidQuery || !tabSessionId || isViewOnly
  
  // Memoized placeholder
  const placeholder = useMemo(() => {
    if (isViewOnly) return "View only — cannot continue this conversation"
    return "Ask anything... (@ files, / commands, # workflows)"
  }, [isViewOnly])

  return (
    <TooltipProvider>
      <div className="space-y-2">
      {/* File Context Display */}
      {chatFileContext.length > 0 && (
        <div className="px-4 border-t border-gray-200 dark:border-gray-700">
          <FileContextDisplay
            files={chatFileContext}
            onRemoveFile={removeFileFromContext}
            onClearAll={clearFileContext}
            agentMode={agentMode}
            isRequiredFolderSelected={true}
          />
        </div>
      )}


      {/* Workflow Context Display — same style as FileContextDisplay */}
      {(tabConfig?.workflowContext?.length ?? 0) > 0 && (
        <div className="px-4">
          <div className="border rounded px-1.5 py-0.5 mb-1 bg-gray-50 dark:bg-gray-800 border-gray-200 dark:border-gray-700">
            <div className="flex items-center gap-1.5 flex-wrap">
              <span className="text-xs font-medium text-gray-600 dark:text-gray-400">
                <Layers className="w-3 h-3 inline-block mr-0.5 -mt-0.5" />
                Workflows:
              </span>
              {tabConfig!.workflowContext.map((w, index) => (
                <div key={w.presetId} className="flex items-center gap-0.5">
                  <span className="text-xs text-gray-700 dark:text-gray-300 font-mono">
                    {w.label}
                  </span>
                  <button
                    type="button"
                    onClick={() => {
                      if (activeTabId) {
                        const remaining = tabConfig!.workflowContext.filter(wc => wc.presetId !== w.presetId)
                        setTabConfig(activeTabId, { workflowContext: remaining })
                        const ref = '#' + w.label
                        if (inputText.includes(ref)) {
                          const newText = inputText.replace(ref, '').replace(/  +/g, ' ').trim()
                          setLocalInputText(newText)
                          setTabConfig(activeTabId, { inputText: newText })
                        }
                      }
                    }}
                    className="p-0.5 hover:bg-red-100 dark:hover:bg-red-900/20 rounded text-red-500 hover:text-red-700 dark:hover:text-red-400"
                    title="Remove workflow context"
                  >
                    <X className="w-2 h-2" />
                  </button>
                  {index < tabConfig!.workflowContext.length - 1 && (
                    <span className="text-xs text-gray-400">&bull;</span>
                  )}
                </div>
              ))}
              <button
                type="button"
                onClick={() => {
                  if (activeTabId) {
                    const labels = tabConfig!.workflowContext.map(w => '#' + w.label)
                    setTabConfig(activeTabId, { workflowContext: [] })
                    let newText = inputText
                    labels.forEach(ref => { newText = newText.replace(ref, '') })
                    newText = newText.replace(/  +/g, ' ').trim()
                    setLocalInputText(newText)
                    setTabConfig(activeTabId, { inputText: newText })
                  }
                }}
                className="text-xs text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 hover:underline ml-0.5"
              >
                Clear
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Input Form */}
      <div className="px-4 py-2 border-t border-gray-200 dark:border-gray-700">
        <form onSubmit={handleSubmit} className="space-y-2">
          <div className="space-y-1">
            {/* Queued messages indicator */}
            {queuedMessages.length > 0 && (
              <div className="space-y-1">
                {queuedMessages.map((msg: string, index: number) => (
                  <div key={index} className="flex items-start gap-2 px-2 py-1 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded text-xs text-blue-700 dark:text-blue-300">
                    <div className="w-1.5 h-1.5 bg-blue-500 rounded-full animate-pulse mt-1.5 flex-shrink-0"></div>
                    <span className="flex-1 break-words">
                      <span className="font-medium">#{index + 1}:</span> "{msg}"
                    </span>
                    <button
                      type="button"
                      onClick={() => {
                        if (activeTabId) {
                          const updated = queuedMessages.filter((_: string, i: number) => i !== index)
                          setTabConfig(activeTabId, { queuedMessages: updated })
                        }
                      }}
                      className="flex items-center justify-center w-5 h-5 rounded hover:bg-blue-200 dark:hover:bg-blue-800 text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-200 transition-colors flex-shrink-0 mt-0.5"
                      title="Delete from queue"
                    >
                      <svg xmlns="http://www.w3.org/2000/svg" className="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                      </svg>
                    </button>
                  </div>
                ))}
              </div>
            )}
            {/* Show text input */}
            <Textarea
              ref={textareaRef}
              value={inputText}
              onChange={handleTextChange}
              onKeyDown={handleKeyDown}
              placeholder={placeholder}
              className="!min-h-[40px] max-h-[100px] resize-none text-xs overflow-y-auto leading-[1.3] !py-1 !px-3 placeholder:text-xs"
              disabled={isSummarizing || !tabSessionId || isViewOnly}
              data-testid="chat-input-textarea"
            />
            <div className="flex justify-between items-center">
              <div className="flex items-center gap-2">
                
                {/* Agent Mode Selector - Only show when no preset is active */}
                {!chatActivePreset && (
                  effectiveDelegationMode === 'plan' ? (
                    /* In plan mode, agent auto-picks tool mode per task */
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <div className="flex items-center gap-0.5 px-2 py-1.5 rounded-md border border-blue-300 dark:border-blue-600 bg-blue-50 dark:bg-blue-900/30 text-blue-600 dark:text-blue-400 cursor-default">
                          <Sparkles className="w-3.5 h-3.5" />
                          <Code2 className="w-3.5 h-3.5" />
                          <Search className="w-3.5 h-3.5" />
                          <span className="text-[10px] font-medium ml-0.5">Auto</span>
                        </div>
                      </TooltipTrigger>
                      <TooltipContent side="top">
                        <p>Plan mode: agent picks tool mode per task (simple, code exec, tool search)</p>
                      </TooltipContent>
                    </Tooltip>
                  ) : (
                  <div className="flex items-center gap-1">
                    {/* Simple Mode */}
                    <button
                      type="button"
                      onClick={() => {
                        setUseCodeExecutionMode(false)
                        setUseToolSearchMode(false)
                      }}
                      disabled={isStreaming || isSummarizing}
                      data-testid="agent-mode-simple"
                      className={`group flex items-center gap-1 p-1.5 rounded-md border transition-all duration-200 ${
                        !useToolSearchMode
                          ? 'bg-purple-100 dark:bg-purple-900/40 border-purple-400 dark:border-purple-600 text-purple-600 dark:text-purple-400'
                          : 'bg-gray-100 dark:bg-gray-800 border-gray-300 dark:border-gray-600 text-gray-400 dark:text-gray-500'
                      } ${(isStreaming || isSummarizing) ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer hover:pr-2'}`}
                    >
                      <Sparkles className="w-4 h-4 flex-shrink-0" />
                      <span className="text-xs font-medium max-w-0 overflow-hidden whitespace-nowrap group-hover:max-w-[50px] transition-all duration-200">
                        Simple
                      </span>
                    </button>
                    {/* Tool Search Mode */}
                    <button
                      type="button"
                      onClick={() => {
                        setUseCodeExecutionMode(false)
                        setUseToolSearchMode(true)
                      }}
                      disabled={isStreaming || isSummarizing}
                      data-testid="agent-mode-tool-search"
                      className={`group flex items-center gap-1 p-1.5 rounded-md border transition-all duration-200 ${
                        useToolSearchMode
                          ? 'bg-cyan-100 dark:bg-cyan-900/40 border-cyan-400 dark:border-cyan-600 text-cyan-600 dark:text-cyan-400'
                          : 'bg-gray-100 dark:bg-gray-800 border-gray-300 dark:border-gray-600 text-gray-400 dark:text-gray-500'
                      } ${(isStreaming || isSummarizing) ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer hover:pr-2'}`}
                    >
                      <Search className="w-4 h-4 flex-shrink-0" />
                      <span className="text-xs font-medium max-w-0 overflow-hidden whitespace-nowrap group-hover:max-w-[50px] transition-all duration-200">
                        Search
                      </span>
                    </button>
                  </div>
                  )
                )}
                
                {/* Server and LLM Selection - only show when no preset is active */}
                {!chatActivePreset && (
                  <div className="flex items-center gap-2">
                    
                      <>
                        <ServerSelectionDropdown
                          availableServers={availableServers}
                          selectedServers={manualSelectedServers}
                          onServerToggle={onManualServerToggle}
                          onSelectAll={onSelectAllServers}
                          onClearAll={onClearAllServers}
                          disabled={isStreaming || isSummarizing}
                          agentMode={agentMode}
                        />
                        <SkillSelectionDropdown
                          selectedSkills={selectedSkills}
                          onSkillToggle={onSkillToggle}
                          onSelectAll={onSelectAllSkills}
                          onClearAll={onClearAllSkills}
                          disabled={isStreaming || isSummarizing}
                        />
                        {(effectiveDelegationMode === 'spawn' || effectiveDelegationMode === 'plan') && (
                          <SubAgentSelectionDropdown
                            selectedSubAgents={selectedSubAgents}
                            onSubAgentToggle={onSubAgentToggle}
                            onSelectAll={onSelectAllSubAgents}
                            onClearAll={onClearAllSubAgents}
                            disabled={isStreaming || isSummarizing}
                          />
                        )}
                      </>

                    {/* Hide LLM dropdown in plan/multi-agent mode - show tier summary chip instead */}
                    {effectiveDelegationMode === 'plan' ? (
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <button
                            type="button"
                            onClick={() => setShowTierModal(true)}
                            className="flex items-center gap-1.5 px-2 py-1.5 rounded-md border border-blue-300 dark:border-blue-600 bg-blue-50 dark:bg-blue-900/30 text-blue-600 dark:text-blue-400 hover:bg-blue-100 dark:hover:bg-blue-900/50 transition-colors text-xs"
                          >
                            <Layers className="w-3.5 h-3.5 flex-shrink-0" />
                            {delegationTierConfig && (delegationTierConfig.high || delegationTierConfig.medium || delegationTierConfig.low) ? (
                              <span className="flex items-center gap-1 font-medium">
                                {delegationTierConfig.high && (
                                  <span className="flex items-center gap-0.5 px-1 py-0.5 rounded bg-blue-100 dark:bg-blue-800/40 text-[10px] leading-none">
                                    <TierProviderDot provider={delegationTierConfig.high.provider} />
                                    H
                                  </span>
                                )}
                                {delegationTierConfig.medium && (
                                  <span className="flex items-center gap-0.5 px-1 py-0.5 rounded bg-blue-100 dark:bg-blue-800/40 text-[10px] leading-none">
                                    <TierProviderDot provider={delegationTierConfig.medium.provider} />
                                    M
                                  </span>
                                )}
                                {delegationTierConfig.low && (
                                  <span className="flex items-center gap-0.5 px-1 py-0.5 rounded bg-blue-100 dark:bg-blue-800/40 text-[10px] leading-none">
                                    <TierProviderDot provider={delegationTierConfig.low.provider} />
                                    L
                                  </span>
                                )}
                              </span>
                            ) : (
                              <span className="font-medium">Tiers</span>
                            )}
                          </button>
                        </TooltipTrigger>
                        <TooltipContent side="top">
                          {delegationTierConfig && (delegationTierConfig.high || delegationTierConfig.medium || delegationTierConfig.low) ? (
                            <div className="space-y-1 text-xs">
                              {delegationTierConfig.high && <p>High: {shortModelName(delegationTierConfig.high.model_id)} ({delegationTierConfig.high.provider})</p>}
                              {delegationTierConfig.medium && <p>Med: {shortModelName(delegationTierConfig.medium.model_id)} ({delegationTierConfig.medium.provider})</p>}
                              {delegationTierConfig.low && <p>Low: {shortModelName(delegationTierConfig.low.model_id)} ({delegationTierConfig.low.provider})</p>}
                            </div>
                          ) : (
                            <p>Click to configure delegation tier models</p>
                          )}
                        </TooltipContent>
                      </Tooltip>
                    ) : (
                      <TooltipProvider>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <div className="flex">
                              <LLMSelectionDropdown
                                availableLLMs={availableLLMs}
                                selectedLLM={primaryLLM}
                                onLLMSelect={onPrimaryLLMSelect}
                                onRefresh={onRefreshAvailableLLMs}
                                disabled={isStreaming || isSummarizing}
                                openDirection="up"
                              />
                            </div>
                          </TooltipTrigger>
                          <TooltipContent side="top">
                            <p>{llmConfigLocked ? 'Select from admin-configured LLMs' : 'Select Primary LLM'}</p>
                          </TooltipContent>
                        </Tooltip>
                      </TooltipProvider>
                    )}
                    {/* Workspace Access Toggle - Icon Button with expand on hover */}
                    <button
                      type="button"
                      onClick={() => setEnableWorkspaceAccess(!enableWorkspaceAccess)}
                      disabled={isStreaming || isSummarizing || enableBrowserAccess}
                      className={`group flex items-center gap-1 p-1.5 rounded-md border transition-all duration-200 ${
                        enableWorkspaceAccess
                          ? 'bg-blue-100 dark:bg-blue-900/40 border-blue-400 dark:border-blue-600 text-blue-600 dark:text-blue-400'
                          : 'bg-gray-100 dark:bg-gray-800 border-gray-300 dark:border-gray-600 text-gray-400 dark:text-gray-500'
                      } ${(isStreaming || isSummarizing || enableBrowserAccess) ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer hover:pr-2'}`}
                    >
                      <FolderOpen className="w-4 h-4 flex-shrink-0" />
                      <span className="text-xs font-medium max-w-0 overflow-hidden whitespace-nowrap group-hover:max-w-[80px] transition-all duration-200">
                        Workspace
                      </span>
                    </button>
                    {/* Browser Access Toggle - Icon Button with expand on hover */}
                    <button
                      type="button"
                      onClick={() => {
                        if (!enableBrowserAccess) {
                          // Enabling browser: show CDP config popup
                          setEnableBrowserAccess(true)
                          setShowCdpPopup(true)
                        } else {
                          // Clicking again while enabled: re-open popup to change settings
                          setShowCdpPopup(true)
                        }
                      }}
                      disabled={isStreaming || isSummarizing}
                      className={`group flex items-center gap-1 p-1.5 rounded-md border transition-all duration-200 ${
                        enableBrowserAccess
                          ? useCdp
                            ? 'bg-green-900/40 border-green-600 text-green-400'
                            : 'bg-blue-900/40 border-blue-600 text-blue-400'
                          : 'bg-gray-800 border-gray-600 text-gray-500'
                      } ${(isStreaming || isSummarizing) ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer hover:pr-2'}`}
                    >
                      <Globe className="w-4 h-4 flex-shrink-0" />
                      {enableBrowserAccess ? (
                        <span className={`text-[10px] font-semibold px-1 rounded ${
                          useCdp
                            ? 'bg-green-800 text-green-200'
                            : 'bg-blue-800 text-blue-200'
                        }`}>
                          {useCdp ? 'CDP' : 'Headless'}
                        </span>
                      ) : (
                        <span className="text-xs font-medium max-w-0 overflow-hidden whitespace-nowrap group-hover:max-w-[60px] transition-all duration-200">
                          Browser
                        </span>
                      )}
                    </button>
                  </div>
                )}

                {/* CDP Configuration Popup */}
                {showCdpPopup && (
                  <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm" onClick={() => setShowCdpPopup(false)}>
                    <div className="bg-gray-900 rounded-xl shadow-2xl border border-gray-700 w-[420px] max-w-[90vw]" onClick={(e) => e.stopPropagation()}>
                      {/* Header */}
                      <div className="flex items-center justify-between px-5 py-4 border-b border-gray-700">
                        <div className="flex items-center gap-2">
                          <Globe className="w-5 h-5 text-green-400" />
                          <h3 className="text-base font-semibold text-white">Browser Access</h3>
                        </div>
                        <button onClick={() => setShowCdpPopup(false)} className="text-gray-400 hover:text-gray-200 transition-colors">
                          <X className="w-5 h-5" />
                        </button>
                      </div>

                      {/* Content */}
                      <div className="px-5 py-4 space-y-4">
                        {/* Mode selection */}
                        <div className="space-y-3">
                          {/* Headless mode option */}
                          <label className={`flex items-start gap-3 p-3 rounded-lg border cursor-pointer transition-colors ${
                            !useCdp
                              ? 'border-blue-500 bg-blue-950/40'
                              : 'border-gray-700 hover:bg-gray-800'
                          }`}>
                            <input
                              type="radio"
                              name="browserMode"
                              checked={!useCdp}
                              onChange={() => setUseCdp(false)}
                              className="mt-0.5 w-4 h-4 text-blue-500 accent-blue-500"
                            />
                            <div>
                              <div className="text-sm font-medium text-gray-100">Headless Browser</div>
                              <div className="text-xs text-gray-400 mt-0.5">
                                Agent controls a headless Chromium inside Docker. You won&apos;t see the browser window.
                              </div>
                            </div>
                          </label>

                          {/* CDP mode option */}
                          <label className={`flex items-start gap-3 p-3 rounded-lg border cursor-pointer transition-colors ${
                            useCdp
                              ? 'border-green-500 bg-green-950/40'
                              : 'border-gray-700 hover:bg-gray-800'
                          }`}>
                            <input
                              type="radio"
                              name="browserMode"
                              checked={useCdp}
                              onChange={() => setUseCdp(true)}
                              className="mt-0.5 w-4 h-4 text-green-500 accent-green-500"
                            />
                            <div className="flex-1">
                              <div className="text-sm font-medium text-gray-100">Connect to Local Chrome (CDP)</div>
                              <div className="text-xs text-gray-400 mt-0.5">
                                Agent connects to your real Chrome browser so you can watch it navigate in real-time.
                              </div>
                            </div>
                          </label>
                        </div>

                        {/* CDP configuration - only when CDP is selected */}
                        {useCdp && (
                          <div className="space-y-3 p-3 rounded-lg bg-gray-800/60 border border-gray-700">
                            {/* Port input */}
                            <div className="flex items-center gap-3">
                              <label className="text-sm text-gray-400 whitespace-nowrap">CDP Port:</label>
                              <input
                                type="number"
                                value={cdpPort}
                                onChange={(e) => setCdpPort(parseInt(e.target.value) || 9222)}
                                className="w-24 px-2.5 py-1.5 text-sm border border-gray-600 rounded-md bg-gray-800 text-white focus:border-green-500 focus:outline-none"
                                min={1}
                                max={65535}
                              />
                              <button
                                type="button"
                                onClick={() => checkCdpConnection(cdpPort)}
                                disabled={cdpChecking}
                                className="px-3 py-1.5 text-xs font-medium bg-gray-700 hover:bg-gray-600 rounded-md text-gray-200 disabled:opacity-50 transition-colors"
                              >
                                {cdpChecking ? 'Checking...' : 'Check Connection'}
                              </button>
                            </div>

                            {/* Connection status */}
                            <div className="flex items-start gap-2">
                              {cdpChecking ? (
                                <>
                                  <div className="w-3 h-3 rounded-full bg-yellow-400 animate-pulse mt-0.5 flex-shrink-0" />
                                  <span className="text-sm text-yellow-400">Checking connection to port {cdpPort}...</span>
                                </>
                              ) : cdpConnected === true ? (
                                <>
                                  <div className="w-3 h-3 rounded-full bg-green-500 mt-0.5 flex-shrink-0" />
                                  <span className="text-sm text-green-400">Connected! Chrome is reachable on port {cdpPort}.</span>
                                </>
                              ) : cdpConnected === false ? (
                                <>
                                  <div className="w-3 h-3 rounded-full bg-red-500 mt-0.5 flex-shrink-0" />
                                  <div className="text-sm">
                                    <span className="text-red-400">Chrome is not reachable on port {cdpPort}.</span>
                                    <div className="mt-2 text-xs text-gray-400 space-y-1">
                                      <p className="font-medium text-gray-300">To enable CDP, launch Chrome with:</p>
                                      <code className="block bg-gray-950 px-2 py-1.5 rounded text-xs font-mono break-all text-green-400 border border-gray-700">
                                        {navigator.platform?.includes('Mac')
                                          ? `/Applications/Google\\ Chrome.app/Contents/MacOS/Google\\ Chrome --remote-debugging-port=${cdpPort}`
                                          : `google-chrome --remote-debugging-port=${cdpPort}`}
                                      </code>
                                      <p className="text-gray-500 mt-1">Close all Chrome windows first, then run the command above.</p>
                                    </div>
                                  </div>
                                </>
                              ) : (
                                <span className="text-xs text-gray-500">Click &quot;Check Connection&quot; to verify Chrome is reachable.</span>
                              )}
                            </div>
                          </div>
                        )}
                      </div>

                      {/* Footer */}
                      <div className="flex justify-end gap-2 px-5 py-3 border-t border-gray-700">
                        <button
                          type="button"
                          onClick={() => {
                            setEnableBrowserAccess(false)
                            setUseCdp(false)
                            setShowCdpPopup(false)
                          }}
                          className="px-4 py-2 text-sm text-gray-300 hover:bg-gray-800 rounded-md transition-colors"
                        >
                          Disable Browser
                        </button>
                        <button
                          type="button"
                          onClick={() => setShowCdpPopup(false)}
                          disabled={useCdp && cdpConnected === false}
                          className="px-4 py-2 text-sm font-medium bg-green-600 hover:bg-green-500 text-white rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                        >
                          {useCdp && cdpConnected === false ? 'Connect Chrome First' : 'Done'}
                        </button>
                      </div>
                    </div>
                  </div>
                )}

                {/* Secrets dropdown - always visible (independent of presets) */}
                <SecretSelectionDropdown
                  selectedSecrets={selectedSecrets}
                  onSecretToggle={onSecretToggle}
                  onSelectAll={onSelectAllSecrets}
                  onClearAll={onClearAllSecrets}
                  disabled={isStreaming || isSummarizing}
                />

                {/* Status text - removed observer initialization message */}
              </div>
              {/* Show old buttons */}
              {(
                <div className="flex items-center gap-2">
                  {/* Context completion indicator */}
                  {contextUsagePercent !== null && (
                    <CircularProgress 
                      percentage={contextUsagePercent} 
                      size={24}
                      strokeWidth={2.5}
                      tokenUsage={latestTokenUsage}
                    />
                  )}
                  {isSummarizing ? (
                    <div className="flex items-center gap-2 px-3 py-1.5 text-sm text-gray-600 dark:text-gray-400">
                      <Loader2 className="w-4 h-4 animate-spin" />
                      <span>Summarizing...</span>
                    </div>
                  ) : (
                    <div className="flex items-center gap-1">
                      {/* Delegation mode indicator - show for spawn in chat mode only (multi-agent mode is self-evident) */}
                      {!isMultiAgentMode && delegationMode !== 'off' && (
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <div className="p-1.5 opacity-60 hover:opacity-100 transition-opacity cursor-default text-purple-500 dark:text-purple-400">
                              <GitBranch className="w-3.5 h-3.5" />
                            </div>
                          </TooltipTrigger>
                          <TooltipContent>
                            <p>Simple delegation enabled (/nospawn to disable)</p>
                          </TooltipContent>
                        </Tooltip>
                      )}

                      {/* Multi-agent phase indicator — clickable in execution phase to go back to planning */}
                      {isMultiAgentMode && planPhase && (
                        planPhase === 'execution' ? (
                          <button
                            type="button"
                            onClick={() => onSubmit('I want to start a new task. Plan it out before executing.')}
                            className="flex items-center gap-1 px-2 py-1 rounded-md border border-green-300 dark:border-green-600 bg-green-50 dark:bg-green-900/30 text-green-600 dark:text-green-400 text-xs font-medium hover:bg-green-100 dark:hover:bg-green-900/50 transition-colors cursor-pointer"
                            title="Click to go back to planning mode"
                          >
                            <Play className="w-3 h-3" />
                            <span>Executing</span>
                          </button>
                        ) : (
                          <div className="flex items-center gap-1 px-2 py-1 rounded-md border border-amber-300 dark:border-amber-600 bg-amber-50 dark:bg-amber-900/30 text-amber-600 dark:text-amber-400 text-xs font-medium">
                            <FileSearch className="w-3 h-3" />
                            <span>Planning</span>
                          </div>
                        )
                      )}

                      {isStreaming && !(activeTab?.hasRunningBgAgents) ? (
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <Button
                              type="button"
                              variant="destructive"
                              onClick={onStopStreaming}
                              size="sm"
                              className="px-3"
                              data-testid="chat-stop-button"
                            >
                              <Square className="w-4 h-4" />
                            </Button>
                          </TooltipTrigger>
                          <TooltipContent>
                            <p>Stop streaming</p>
                          </TooltipContent>
                        </Tooltip>
                      ) : (
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <Button
                              type="submit"
                              disabled={submitButtonDisabled}
                              size="sm"
                              className="px-3"
                              data-testid="chat-submit-button"
                            >
                              <Send className="w-4 h-4" />
                            </Button>
                          </TooltipTrigger>
                          <TooltipContent>
                            <p>
                              {isViewOnly
                                ? 'View only — cannot continue this conversation'
                                : !inputText?.trim()
                                  ? 'Type a message to send'
                                  : !tabSessionId
                                    ? 'Session not ready yet'
                                    : 'Send message'
                              }
                            </p>
                          </TooltipContent>
                        </Tooltip>
                      )}
                    </div>
                  )}
                </div>
              )}
            </div>
          </div>
        </form>
      </div>
      
      {/* Command Selection Dialog */}
      <CommandSelectionDialog
        isOpen={showCommandDialog}
        onClose={handleCommandDialogClose}
        onSelectCommand={handleCommandSelect}
        searchQuery={commandSearchQuery}
        position={commandDialogPosition}
        modeCategory={selectedModeCategory}
      />
      
      {/* File Selection Dialog */}
      <FileSelectionDialog
        isOpen={showFileDialog}
        onClose={handleFileDialogClose}
        onSelectFile={handleFileSelect}
        onNavigateIntoFolder={handleNavigateIntoFolder}
        searchQuery={fileSearchQuery}
        position={fileDialogPosition}
      />

      {/* Workflow Selection Dialog */}
      <WorkflowSelectionDialog
        isOpen={showWorkflowDialog}
        onClose={handleWorkflowDialogClose}
        onSelectWorkflow={handleWorkflowSelect}
        searchQuery={workflowSearchQuery}
        position={workflowDialogPosition}
      />

      {/* Slash command dialogs */}
      {showSkillImport && (
        <SkillImportDialog
          onClose={() => closeDialog('skillImport')}
          onSuccess={() => closeDialog('skillImport')}
        />
      )}
      {showMCPDetails && (
        <MCPDetailsModal
          onClose={() => closeDialog('mcpDetails')}
          onOpenConfigEditor={() => openDialog('mcpConfig')}
        />
      )}
      {showMCPConfig && (
        <MCPConfigPopup
          onClose={() => closeDialog('mcpConfig')}
        />
      )}
      <LLMConfigurationModal
        isOpen={showModels}
        onClose={() => closeDialog('models')}
      />
      {showResume && (
        <ResumeSessionDialog
          onClose={() => closeDialog('resume')}
        />
      )}
      <DelegationTierConfigModal
        isOpen={showTierModal}
        onClose={() => setShowTierModal(false)}
      />
      {showWorkflowBuilder && (
        <WorkflowBuilderModal onClose={() => closeDialog('workflowBuilder')} />
      )}
      </div>
    </TooltipProvider>
  )
}

ChatInputComponent.displayName = 'ChatInput'

export const ChatInput = React.memo(ChatInputComponent)
