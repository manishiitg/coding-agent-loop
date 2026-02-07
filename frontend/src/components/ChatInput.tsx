import React, { useRef, useCallback, useMemo, useState, useEffect, useLayoutEffect } from 'react'
import { Send, Square, Code2, Sparkles, Loader2, FolderOpen, Search, Globe, GitBranch, Layers } from 'lucide-react'
import { Button } from './ui/Button'
import { Textarea } from './ui/Textarea'
import FileContextDisplay from './FileContextDisplay'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './ui/tooltip'
import { CircularProgress } from './ui/CircularProgress'
import { getEventData, isEventType } from '../generated/event-types'
import type { TokenUsageEvent } from '../generated/events'
import ServerSelectionDropdown from './ServerSelectionDropdown'
import SkillSelectionDropdown from './skills/SkillSelectionDropdown'
import LLMSelectionDropdown from './LLMSelectionDropdown'
import FileSelectionDialog from './FileSelectionDialog'
import CommandSelectionDialog from './CommandSelectionDialog'
import SkillImportDialog from './skills/SkillImportDialog'
import { MCPConfigPopup } from './MCPConfigPopup'
import MCPDetailsModal from './MCPDetailsModal'
import LLMConfigurationModal from './LLMConfigurationModal'
import ResumeSessionDialog from './ResumeSessionDialog'
import DelegationTierConfigModal from './DelegationTierConfigModal'
import type { PlannerFile } from '../services/api-types'
import type { LLMOption } from '../types/llm'
import { useAppStore, useMCPStore, useLLMStore, useChatStore } from '../stores'
import { useWorkspaceStore } from '../stores/useWorkspaceStore'
import { useCommandDialogStore } from '../stores/useCommandDialogStore'
import { usePresetApplication } from '../stores/useGlobalPresetStore'
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

// Provider icon for tier chip (official SVGs from simple-icons)
const TierProviderDot = ({ provider }: { provider: string }) => {
  const cls = "w-3 h-3 flex-shrink-0"
  switch (provider) {
    case 'anthropic':
      return <svg viewBox="0 0 24 24" className={cls} fill="currentColor"><path d="M17.304 3.541h-3.672l6.696 16.918H24zm-10.608 0L0 20.459h3.744l1.37-3.553h7.005l1.369 3.553h3.744L10.536 3.541zm-.371 10.223 2.291-5.946 2.291 5.946z"/></svg>
    case 'openai':
      return <svg viewBox="0 0 24 24" className={cls} fill="currentColor"><path d="M22.282 9.821a5.985 5.985 0 0 0-.516-4.91 6.046 6.046 0 0 0-6.51-2.9A6.065 6.065 0 0 0 4.981 4.18a5.985 5.985 0 0 0-3.998 2.9 6.046 6.046 0 0 0 .743 7.097 5.98 5.98 0 0 0 .51 4.911 6.051 6.051 0 0 0 6.515 2.9A5.985 5.985 0 0 0 13.26 24a6.056 6.056 0 0 0 5.772-4.206 5.99 5.99 0 0 0 3.997-2.9 6.056 6.056 0 0 0-.747-7.073zM13.26 22.43a4.476 4.476 0 0 1-2.876-1.04l.141-.081 4.779-2.758a.795.795 0 0 0 .392-.681v-6.737l2.02 1.168a.071.071 0 0 1 .038.052v5.583a4.504 4.504 0 0 1-4.494 4.494zM3.6 18.304a4.47 4.47 0 0 1-.535-3.014l.142.085 4.783 2.759a.771.771 0 0 0 .78 0l5.843-3.369v2.332a.08.08 0 0 1-.033.062L9.74 19.95a4.5 4.5 0 0 1-6.14-1.646zM2.34 7.896a4.485 4.485 0 0 1 2.366-1.973V11.6a.766.766 0 0 0 .388.676l5.815 3.355-2.02 1.168a.076.076 0 0 1-.071 0l-4.83-2.786A4.504 4.504 0 0 1 2.34 7.872zm16.597 3.855-5.833-3.387L15.119 7.2a.076.076 0 0 1 .071 0l4.83 2.791a4.494 4.494 0 0 1-.676 8.105v-5.678a.79.79 0 0 0-.407-.667zm2.01-3.023-.141-.085-4.774-2.782a.776.776 0 0 0-.785 0L9.409 9.23V6.897a.066.066 0 0 1 .028-.061l4.83-2.787a4.5 4.5 0 0 1 6.68 4.66zm-12.64 4.135-2.02-1.164a.08.08 0 0 1-.038-.057V6.075a4.5 4.5 0 0 1 7.375-3.453l-.142.08L8.704 5.46a.795.795 0 0 0-.393.681zm1.097-2.365 2.602-1.5 2.607 1.5v2.999l-2.597 1.5-2.607-1.5z"/></svg>
    case 'openrouter':
      return <svg viewBox="0 0 24 24" className={cls} fill="currentColor"><path d="M16.778 1.844v1.919q-.569-.026-1.138-.032-.708-.008-1.415.037c-1.93.126-4.023.728-6.149 2.237-2.911 2.066-2.731 1.95-4.14 2.75-.396.223-1.342.574-2.185.798-.841.225-1.753.333-1.751.333v4.229s.768.108 1.61.333c.842.224 1.789.575 2.185.799 1.41.798 1.228.683 4.14 2.75 2.126 1.509 4.22 2.11 6.148 2.236.88.058 1.716.041 2.555.005v1.918l7.222-4.168-7.222-4.17v2.176c-.86.038-1.611.065-2.278.021-1.364-.09-2.417-.357-3.979-1.465-2.244-1.593-2.866-2.027-3.68-2.508.889-.518 1.449-.906 3.822-2.59 1.56-1.109 2.614-1.377 3.978-1.466.667-.044 1.418-.017 2.278.02v2.176L24 6.014Z"/></svg>
    case 'bedrock':
      return <svg viewBox="0 0 24 24" className={cls} fill="currentColor"><path d="M4.985 0c-.294.003-.534.247-.534.548l-.006 4.908c0 .145.06.284.159.39a.532.532 0 0 0 .381.155h3.428l8.197 17.681a.537.537 0 0 0 .489.318h5.81c.298 0 .543-.245.543-.548V18.544c0-.303-.239-.548-.543-.548h-2.013L12.739.315A.536.536 0 0 0 12.245 0h-7.254zm.54 1.091h6.368l8.16 17.68a.537.537 0 0 0 .488.318h1.818v3.817h-2.922L11.24 5.226a.536.536 0 0 0-.488-.318H5.522z"/></svg>
    case 'vertex':
      return <svg viewBox="0 0 24 24" className={cls} fill="currentColor"><path d="M11.04 19.32Q12 21.51 12 24q0-2.49.93-4.68.96-2.19 2.58-3.81t3.81-2.55Q21.51 12 24 12q-2.49 0-4.68-.93a12.3 12.3 0 0 1-3.81-2.58 12.3 12.3 0 0 1-2.58-3.81Q12 2.49 12 0q0 2.49-.96 4.68-.93 2.19-2.55 3.81a12.3 12.3 0 0 1-3.81 2.58Q2.49 12 0 12q2.49 0 4.68.96 2.19.93 3.81 2.55t2.55 3.81"/></svg>
    case 'azure':
      return <svg viewBox="0 0 24 24" className={cls} fill="currentColor"><path d="M13.05 4.24 7.56 18.05l-4.3.76L13.05 4.24M14.7.76H9.07L1.97 19.45l5.03-.88L14.7.76zM22.03 18.76 14.26 3.38l-3.2 7.36 4.78 8.79-8.27 1.23L22.03 18.76z"/></svg>
    default:
      return <span className="text-[8px] font-bold uppercase leading-none">{provider?.slice(0, 2) || '?'}</span>
  }
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
  
  // Note: activeTab may be undefined during initial render before tabs are created
  // This is expected and will resolve once the tab store initializes
  
  // Get all tab events from the store (stable selector)
  const allTabEvents = useChatStore(state => state.tabEvents)

  // Derive tab-specific events with useMemo (avoids selector closure issues)
  const tabEvents = useMemo(() => {
    if (!tabSessionId) return EMPTY_EVENTS
    return allTabEvents[tabSessionId] ?? EMPTY_EVENTS
  }, [tabSessionId, allTabEvents])

  // Throttle recalculation - only update when event count changes by 5+
  const eventCountBatch = Math.floor(tabEvents.length / 5)

  // Find the latest token usage (optimized with backward iteration)
  const { contextUsagePercent, latestTokenUsage } = useMemo(() => {
    if (tabEvents.length === 0) return { contextUsagePercent: null, latestTokenUsage: null }

    // Iterate backwards (newest first) to find the latest quickly
    let latestTokenUsageEvent = null
    let latestTotalEvent = null

    for (let i = tabEvents.length - 1; i >= 0 && !latestTotalEvent; i--) {
      const event = tabEvents[i]
      if (event.type === 'token_usage') {
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
  }, [tabEvents, eventCountBatch])
  
  // Always use tab-specific config (ChatInput is only in chat mode)
  // Memoize to prevent unnecessary re-renders when other config values change
  const chatFileContext = useMemo(() => tabConfig?.fileContext || [], [tabConfig?.fileContext])
  // Use ?? instead of || to preserve false values (user's selection)
  // Only default to false if the value is undefined/null (not explicitly set)
  const useCodeExecutionMode = useMemo(() => tabConfig?.useCodeExecutionMode ?? false, [tabConfig?.useCodeExecutionMode])
  const useToolSearchMode = useMemo(() => tabConfig?.useToolSearchMode ?? false, [tabConfig?.useToolSearchMode])
  const enableWorkspaceAccess = useMemo(() => tabConfig?.enableWorkspaceAccess ?? true, [tabConfig?.enableWorkspaceAccess])
  const enableBrowserAccess = useMemo(() => tabConfig?.enableBrowserAccess ?? false, [tabConfig?.enableBrowserAccess])
  
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

  const {
    availableLLMs,
    getCurrentLLMOption,
    refreshAvailableLLMs: onRefreshAvailableLLMs,
    llmConfigLocked,
    delegationTierConfig
  } = useLLMStore()

  const { scrollToFile } = useWorkspaceStore()
  const { showSkillImport, showMCPDetails, showMCPConfig, showModels, showResume, openDialog, closeDialog } = useCommandDialogStore()

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
    
    // Check for / symbol and update command dialog state (prioritize over @)
    const lastSlashIndex = textBeforeCursor.lastIndexOf('/')
    const lastAtIndex = textBeforeCursor.lastIndexOf('@')
    
    // Determine which dialog to show (prioritize the one closest to cursor)
    const slashDistance = lastSlashIndex >= 0 ? cursorPosition - lastSlashIndex : Infinity
    const atDistance = lastAtIndex >= 0 ? cursorPosition - lastAtIndex : Infinity
    
    // Check for / command
    if (lastSlashIndex >= 0 && (lastAtIndex < 0 || slashDistance < atDistance)) {
      const textAfterSlash = textBeforeCursor.substring(lastSlashIndex + 1)
      const hasValidSlash = textAfterSlash === '' || textAfterSlash.match(/^[a-zA-Z0-9_]*$/)
      
      if (hasValidSlash) {
        setSlashPosition(lastSlashIndex)
        setCommandSearchQuery(textAfterSlash)
        setShowCommandDialog(true)
        setShowFileDialog(false) // Hide file dialog if command dialog is active
        
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
    // Check for @ symbol and update file dialog state (only if no / command active and workspace access is enabled)
    else if (lastAtIndex >= 0 && !showCommandDialog && enableWorkspaceAccess) {
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
      // Close both dialogs if neither is active
      setShowFileDialog(false)
      setAtPosition(-1)
      setFileSearchQuery('')
      setShowCommandDialog(false)
      setSlashPosition(-1)
      setCommandSearchQuery('')
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
      }
      fileRemovalTimeoutRef.current = null
    }, 500)
  }, [chatFileContext, removeFileFromContext, showCommandDialog, activeTabId, setTabConfig, enableWorkspaceAccess, adjustTextareaHeight])

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
    // If command dialog is open, let it handle keyboard events
    if (showCommandDialog) {
      // Don't prevent default for arrow keys, enter, escape - let dialog handle them
      if (['ArrowUp', 'ArrowDown', 'Enter', 'Escape'].includes(e.key)) {
        return
      }
    }
    // If file dialog is open, let it handle keyboard events
    if (showFileDialog) {
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
  }, [inputText, onSubmit, showFileDialog, showCommandDialog, tabSessionId, canSubmit, canSubmitImmediately, queryToSubmit, isSummarizing, isStreaming, handleSummarize, handleCompact, activeTabId, activeTab?.tabId, activeTab?.sessionId, setTabConfig, adjustTextareaHeight, tabConfig?.queuedMessages])

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
      case 'plan': {
        // Check if delegation tier config exists in localStorage
        const tierConfig = useLLMStore.getState().delegationTierConfig
        const hasTierConfig = tierConfig && (tierConfig.high || tierConfig.medium || tierConfig.low)

        useAppStore.getState().setDelegationMode('plan')

        if (!hasTierConfig) {
          // First time - open left sidebar and expand delegation tiers section
          useAppStore.getState().setSidebarMinimized(false)
          openDialog('delegationTiers')
          addToast(
            'Plan delegation enabled - Configure your delegation tier models in the sidebar',
            'info'
          )
        } else {
          addToast(
            'Plan delegation enabled - Agent will create plans and delegate with multi-LLM tiers',
            'success'
          )
        }
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

  // Removed editing preset query functionality - not needed for chat mode

  // Check if query is valid
  const hasValidQuery = Boolean(inputText?.trim())
  const submitButtonDisabled = !hasValidQuery || !tabSessionId
  
  // Memoized placeholder
  const placeholder = useMemo(() => {
    return "Ask me anything... I can use tools to help you! (Type /summarize to summarize)"
  }, [])

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


      {/* Hint when no files in context - only show if workspace access is enabled */}
      {chatFileContext.length === 0 && enableWorkspaceAccess && (
        <div className="px-4">
          <div className="bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded px-1.5 py-0.5 mb-0">
            <span className="text-[10px] text-gray-500 dark:text-gray-400">
              💡 Click chat icon in workspace to add files, or type @ to search and add files
            </span>
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
              disabled={isSummarizing || !tabSessionId}
              data-testid="chat-input-textarea"
            />
            <div className="flex justify-between items-center">
              <div className="flex items-center gap-2">
                
                {/* Agent Mode Selector - Only show when no preset is active */}
                {!chatActivePreset && (
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
                        !useCodeExecutionMode && !useToolSearchMode
                          ? 'bg-purple-100 dark:bg-purple-900/40 border-purple-400 dark:border-purple-600 text-purple-600 dark:text-purple-400'
                          : 'bg-gray-100 dark:bg-gray-800 border-gray-300 dark:border-gray-600 text-gray-400 dark:text-gray-500'
                      } ${(isStreaming || isSummarizing) ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer hover:pr-2'}`}
                    >
                      <Sparkles className="w-4 h-4 flex-shrink-0" />
                      <span className="text-xs font-medium max-w-0 overflow-hidden whitespace-nowrap group-hover:max-w-[50px] transition-all duration-200">
                        Simple
                      </span>
                    </button>
                    {/* Code Exec Mode */}
                    <button
                      type="button"
                      onClick={() => {
                        setUseCodeExecutionMode(true)
                        setUseToolSearchMode(false)
                      }}
                      disabled={isStreaming || isSummarizing}
                      data-testid="agent-mode-code-exec"
                      className={`group flex items-center gap-1 p-1.5 rounded-md border transition-all duration-200 ${
                        useCodeExecutionMode && !useToolSearchMode
                          ? 'bg-orange-100 dark:bg-orange-900/40 border-orange-400 dark:border-orange-600 text-orange-600 dark:text-orange-400'
                          : 'bg-gray-100 dark:bg-gray-800 border-gray-300 dark:border-gray-600 text-gray-400 dark:text-gray-500'
                      } ${(isStreaming || isSummarizing) ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer hover:pr-2'}`}
                    >
                      <Code2 className="w-4 h-4 flex-shrink-0" />
                      <span className="text-xs font-medium max-w-0 overflow-hidden whitespace-nowrap group-hover:max-w-[40px] transition-all duration-200">
                        Code
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
                      </>
                    
                    {/* Hide LLM dropdown in plan mode - show tier summary chip instead */}
                    {delegationMode === 'plan' ? (
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <button
                            type="button"
                            onClick={() => setShowTierModal(true)}
                            className="flex items-center gap-1.5 px-2 py-1.5 rounded-md border border-blue-300 dark:border-blue-600 bg-blue-50 dark:bg-blue-900/30 text-blue-600 dark:text-blue-400 hover:bg-blue-100 dark:hover:bg-blue-900/50 transition-colors text-xs"
                          >
                            <Layers className="w-3.5 h-3.5 flex-shrink-0" />
                            {delegationTierConfig && (delegationTierConfig.high || delegationTierConfig.medium || delegationTierConfig.low) ? (
                              <span className="flex items-center gap-2 font-medium">
                                {delegationTierConfig.high && (
                                  <span className="flex items-center gap-0.5" title={`High: ${delegationTierConfig.high.provider}/${delegationTierConfig.high.model_id}`}>
                                    <TierProviderDot provider={delegationTierConfig.high.provider} />
                                    <span className="text-[10px]">H:{shortModelName(delegationTierConfig.high.model_id)}</span>
                                  </span>
                                )}
                                {delegationTierConfig.medium && (
                                  <span className="flex items-center gap-0.5" title={`Medium: ${delegationTierConfig.medium.provider}/${delegationTierConfig.medium.model_id}`}>
                                    <TierProviderDot provider={delegationTierConfig.medium.provider} />
                                    <span className="text-[10px]">M:{shortModelName(delegationTierConfig.medium.model_id)}</span>
                                  </span>
                                )}
                                {delegationTierConfig.low && (
                                  <span className="flex items-center gap-0.5" title={`Low: ${delegationTierConfig.low.provider}/${delegationTierConfig.low.model_id}`}>
                                    <TierProviderDot provider={delegationTierConfig.low.provider} />
                                    <span className="text-[10px]">L:{shortModelName(delegationTierConfig.low.model_id)}</span>
                                  </span>
                                )}
                              </span>
                            ) : (
                              <span className="font-medium">Configure Tiers</span>
                            )}
                          </button>
                        </TooltipTrigger>
                        <TooltipContent side="top">
                          <p>Click to configure delegation tier models</p>
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
                      onClick={() => setEnableBrowserAccess(!enableBrowserAccess)}
                      disabled={isStreaming || isSummarizing}
                      className={`group flex items-center gap-1 p-1.5 rounded-md border transition-all duration-200 ${
                        enableBrowserAccess
                          ? 'bg-green-100 dark:bg-green-900/40 border-green-400 dark:border-green-600 text-green-600 dark:text-green-400'
                          : 'bg-gray-100 dark:bg-gray-800 border-gray-300 dark:border-gray-600 text-gray-400 dark:text-gray-500'
                      } ${(isStreaming || isSummarizing) ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer hover:pr-2'}`}
                    >
                      <Globe className="w-4 h-4 flex-shrink-0" />
                      <span className="text-xs font-medium max-w-0 overflow-hidden whitespace-nowrap group-hover:max-w-[60px] transition-all duration-200">
                        Browser
                      </span>
                    </button>
                  </div>
                )}
                
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
                      {/* Delegation mode indicator */}
                      {delegationMode !== 'off' && (
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <div className={`p-1.5 opacity-60 hover:opacity-100 transition-opacity cursor-default ${
                              delegationMode === 'plan' ? 'text-blue-500 dark:text-blue-400' : 'text-purple-500 dark:text-purple-400'
                            }`}>
                              <GitBranch className="w-3.5 h-3.5" />
                            </div>
                          </TooltipTrigger>
                          <TooltipContent>
                            <p>{delegationMode === 'plan' ? 'Plan delegation enabled' : 'Simple delegation enabled'} (/nospawn to disable)</p>
                          </TooltipContent>
                        </Tooltip>
                      )}

                      {isStreaming ? (
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
                              {!inputText?.trim()
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
      />
      
      {/* File Selection Dialog */}
      <FileSelectionDialog
        isOpen={showFileDialog}
        onClose={handleFileDialogClose}
        onSelectFile={handleFileSelect}
        searchQuery={fileSearchQuery}
        position={fileDialogPosition}
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
      </div>
    </TooltipProvider>
  )
}

ChatInputComponent.displayName = 'ChatInput'

export const ChatInput = React.memo(ChatInputComponent)
