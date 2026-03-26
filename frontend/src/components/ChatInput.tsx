import React, { useRef, useCallback, useMemo, useState, useEffect, useLayoutEffect } from 'react'

const DBG = '[skill-popup]'
import { Send, Square, Code2, Sparkles, Wand2, Loader2, FolderOpen, Search, Globe, Layers, X, History, Bot, Server, ImagePlus, Download, Paperclip } from 'lucide-react'
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
import { CommandEditorDialog } from './commands/CommandEditorDialog'
import { findCommand, loadAndRegisterUserCommands, type CommandContext, type CommandDefinition } from '../commands'
import { commandsApi } from '../api/commands'
import WorkflowSelectionDialog from './WorkflowSelectionDialog'
import { isChatCompatiblePhase } from '../utils/chatSubmitHelpers'
import { useWorkflowStore } from '../stores/useWorkflowStore'

function WorkshopModeToggle() {
  const activePresetId = useGlobalPresetStore(state => state.activePresetIds.workflow)
  const workflowMode = useWorkflowStore(state => state.workflowMode)
  const setWorkflowMode = useWorkflowStore(state => state.setWorkflowMode)
  const workshopMode = useWorkflowStore(state =>
    (activePresetId && state.workshopModeByPreset[activePresetId]) || state.workshopMode
  )
  const setWorkshopMode = useWorkflowStore(state => state.setWorkshopMode)

  const builderModes = [
    { id: 'builder' as const, label: 'Build', title: 'Build', description: 'Design and refine the workflow structure and step instructions.' },
    { id: 'optimizer' as const, label: 'Optimize', title: 'Optimize', description: 'Improve reliability, learnings, validation, and step efficiency.' },
    { id: 'debugger' as const, label: 'Debug', title: 'Debug', description: 'Inspect prior runs and failures without re-executing the workflow.' },
    { id: 'runner' as const, label: 'Run', title: 'Run', description: 'Use the finished workflow and focus on execution results.' },
  ]

  const topLevelModes = [
    { id: 'plan' as const, label: 'Builder', title: 'Builder', description: 'Author the main workflow and choose a builder sub-mode.' },
    { id: 'eval' as const, label: 'Eval', title: 'Eval', description: 'Create and maintain evaluation plans for completed workflow runs.' },
    { id: 'output' as const, label: 'Report', title: 'Report', description: 'Define the final markdown report generated after workflow completion.' }
  ]

  return (
    <TooltipProvider delayDuration={120}>
      <div className="flex items-center gap-2">
        <div className="flex items-center rounded-md border border-border overflow-hidden text-xs font-medium">
          {topLevelModes.map(({ id, label, title, description }) => (
            <Tooltip key={id}>
              <TooltipTrigger asChild>
                <button
                  type="button"
                  onClick={() => setWorkflowMode(id)}
                  className={`px-2.5 py-1 transition-colors ${
                    workflowMode === id
                      ? 'bg-primary text-primary-foreground'
                      : 'bg-background text-muted-foreground hover:text-foreground hover:bg-muted'
                  }`}
                  aria-label={title}
                >
                  {label}
                </button>
              </TooltipTrigger>
              <TooltipContent>
                <p>{description}</p>
              </TooltipContent>
            </Tooltip>
          ))}
        </div>

        {workflowMode === 'plan' && (
          <div className="flex items-center rounded-md border border-border overflow-hidden text-xs font-medium">
            {builderModes.map(({ id, label, title, description }) => (
              <Tooltip key={id}>
                <TooltipTrigger asChild>
                  <button
                    type="button"
                    onClick={() => setWorkshopMode(id)}
                    className={`px-2 py-1 transition-colors ${
                      workshopMode === id
                        ? 'bg-muted-foreground text-background'
                        : 'bg-background text-muted-foreground hover:text-foreground hover:bg-muted'
                    }`}
                    aria-label={title}
                  >
                    {label}
                  </button>
                </TooltipTrigger>
                <TooltipContent>
                  <p>{description}</p>
                </TooltipContent>
              </Tooltip>
            ))}
          </div>
        )}
      </div>
    </TooltipProvider>
  )
}
import InlineSelectionPopup from './InlineSelectionPopup'
import type { InlineSelectionItem } from './InlineSelectionPopup'
import SkillImportDialog from './skills/SkillImportDialog'
import SubAgentImportDialog from './subagents/SubAgentImportDialog'
import { MCPConfigPopup } from './MCPConfigPopup'
import MCPDetailsModal from './MCPDetailsModal'
import LLMConfigurationModal from './LLMConfigurationModal'
import ResumeSessionDialog from './ResumeSessionDialog'
import { ImageGenerationConfigModal } from './ImageGenerationConfigModal'
import WorkflowBuilderModal from './WorkflowBuilderModal'
import type { PlannerFile, PollingEvent } from '../services/api-types'
import type { LLMOption } from '../types/llm'
import { useAppStore, useMCPStore, useLLMStore, useChatStore } from '../stores'
import { useCapabilitiesStore } from '../stores/useCapabilitiesStore'
import { useWorkspaceStore } from '../stores/useWorkspaceStore'
import { useCommandDialogStore } from '../stores/useCommandDialogStore'
import { usePresetApplication, useGlobalPresetStore } from '../stores/useGlobalPresetStore'
import { useModeStore } from '../stores/useModeStore'
import { agentApi, getApiBaseUrl } from '../services/api'
import { skillsApi } from '../api/skills'
import { subagentsApi } from '../api/subagents'
import type { Skill } from '../types/skills'
import type { SubAgent } from '../types/subagents'

// MCP servers managed by dedicated toolbar buttons — excluded from the general server dropdown
// Managed by dedicated UI controls in ChatInput (browser/GWS icons), not MCP dropdown.
const DEDICATED_MCP_SERVERS = new Set(['playwright', 'camofox', 'gws'])

export interface ActiveAgentInfo {
  name: string
  type: 'agent' | 'delegation'
  depth: number
}

interface ChatInputProps {
  // Handlers (callbacks only)
  onSubmit: (query: string) => void
  onStopStreaming: () => void
  activeAgents?: ActiveAgentInfo[]
}

// Stable empty array reference to avoid infinite loops in selectors
const EMPTY_EVENTS: never[] = []

// Collapsible queued message item — shows preview for long messages with expand/collapse toggle
const QueuedMessageItem: React.FC<{
  index: number
  msg: string
  preview: string
  isLong: boolean
  onDelete: () => void
  onSteer?: () => void
  isSteering?: boolean
}> = ({ index, msg, preview, isLong, onDelete, onSteer, isSteering }) => {
  const [expanded, setExpanded] = useState(false)
  return (
    <div className="flex items-start gap-2 px-2 py-1 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded text-xs text-blue-700 dark:text-blue-300">
      <div className="w-1.5 h-1.5 bg-blue-500 rounded-full animate-pulse mt-1.5 flex-shrink-0"></div>
      <div className="flex-1 min-w-0">
        {expanded ? (
          <div className="max-h-48 overflow-y-auto break-words whitespace-pre-wrap pr-1">
            <span className="font-medium">#{index + 1}:</span> {msg}
          </div>
        ) : (
          <span className="break-words whitespace-pre-wrap">
            <span className="font-medium">#{index + 1}:</span> {preview}
          </span>
        )}
        {isLong && (
          <button
            type="button"
            onClick={() => setExpanded(!expanded)}
            className="ml-1 text-blue-500 dark:text-blue-400 hover:text-blue-700 dark:hover:text-blue-200 underline"
          >
            {expanded ? 'collapse' : 'expand'}
          </button>
        )}
      </div>
      {onSteer && (
        <button
          type="button"
          onClick={onSteer}
          disabled={isSteering}
          className="flex items-center justify-center w-5 h-5 rounded hover:bg-amber-200 dark:hover:bg-amber-800 text-amber-600 dark:text-amber-400 hover:text-amber-800 dark:hover:text-amber-200 transition-colors flex-shrink-0 mt-0.5 disabled:opacity-50"
          title="Steer — inject this message into the running conversation"
        >
          {isSteering ? (
            <svg className="h-3.5 w-3.5 animate-spin" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
              <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
              <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
            </svg>
          ) : (
            <svg xmlns="http://www.w3.org/2000/svg" className="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 5l7 7-7 7M5 5l7 7-7 7" />
            </svg>
          )}
        </button>
      )}
      <button
        type="button"
        onClick={onDelete}
        className="flex items-center justify-center w-5 h-5 rounded hover:bg-blue-200 dark:hover:bg-blue-800 text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-200 transition-colors flex-shrink-0 mt-0.5"
        title="Delete from queue"
      >
        <svg xmlns="http://www.w3.org/2000/svg" className="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
        </svg>
      </button>
    </div>
  )
}

// Completely isolated input component that doesn't re-render when events change
const ChatInputComponent: React.FC<ChatInputProps> = ({
  onSubmit,
  onStopStreaming,
  activeAgents = []
}) => {
  // Store subscriptions
  const {
    agentMode,
    setWorkspaceMinimized,
    delegationMode,
    showWorkflowsOverview
  } = useAppStore()
  const selectedModeCategory = useModeStore(state => state.selectedModeCategory)
  const isMultiAgentMode = selectedModeCategory === 'multi-agent'
  // Detect workflow phase chat — hide extras like browser, skills, etc.
  const workflowPhaseId = useChatStore(state => {
    const tabId = state.activeTabId
    const tab = tabId ? state.chatTabs[tabId] : null
    if (tab?.metadata?.mode !== 'workflow' || !tab?.metadata?.phaseId) return undefined
    return isChatCompatiblePhase(tab.metadata.phaseId) ? tab.metadata.phaseId : undefined
  })
  const isWorkflowPhaseChat = !!workflowPhaseId
  const isWorkflowMode = selectedModeCategory === 'workflow'
  // Hide extras (servers, skills, agent mode, etc.) in workflow mode but show in multi-agent
  const hideExtras = isWorkflowMode
  // Multi-agent now runs in spawn mode by default (can still plan via tools when needed)
  const effectiveDelegationMode: 'off' | 'spawn' = isMultiAgentMode ? 'spawn' : delegationMode

  // Detect plan phase from events (planning → execution after confirm_plan_execution succeeds)
  // We'll compute this from tabEvents below after they're defined

  // Use selectors to subscribe only to specific values, reducing re-renders
  const activeTabId = useChatStore(state => state.activeTabId)
  const setTabConfig = useChatStore(state => state.setTabConfig)
  const addToast = useChatStore(state => state.addToast)
  // Get active tab and its config (ChatInput is only rendered in multi-agent mode)
  // Use selector to get only the tab we need, preventing re-renders when other tabs change
  const activeTab = useChatStore(state => 
    activeTabId ? state.chatTabs[activeTabId] : undefined
  )
  const isOrganizationAssistant = !!activeTab?.metadata?.isOrganizationAssistant
  const isOrganizationContext = isOrganizationAssistant || showWorkflowsOverview
  
  // Memoize tabConfig to prevent unnecessary re-renders
  const tabConfig = useMemo(() => activeTab?.config, [activeTab?.config])

  const defaultReasoningLevel = tabConfig?.defaultReasoningLevel ?? null
  const setDefaultReasoningLevel = useCallback((level: 'high' | 'medium' | 'low' | null) => {
    const tabId = useChatStore.getState().activeTabId
    if (tabId) {
      useChatStore.getState().setTabConfig(tabId, { defaultReasoningLevel: level })
    }
  }, [])

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
  //
  // NOTE: Backend serializes AgentEvent.Data (Go interface) flat — event.data.data IS the
  // typed event (e.g. TokenUsageEvent) directly, NOT wrapped in EventDataUnion.
  // The schema-gen uses EventDataUnion for JSON Schema but the wire format is flat.
  // Use getEventData() or event.data?.data directly — NOT event.data?.data?.token_usage.
  const { contextUsagePercent, latestTokenUsage } = useMemo(() => {
    if (tabEvents.length === 0) return { contextUsagePercent: null, latestTokenUsage: null }

    // Iterate backwards (newest first) to find the latest quickly
    let latestTokenUsageEvent = null
    let latestTotalEvent = null

    for (let i = tabEvents.length - 1; i >= 0 && !latestTotalEvent; i--) {
      const event = tabEvents[i]
      if (isEventType(event, 'token_usage')) {
        // Skip sub-agent token_usage events — we want the parent agent's context
        if (isSubAgentEvent(event)) continue
        const data = getEventData(event)
        if (data.context === 'conversation_total') {
          latestTotalEvent = event
          break
        }
        if (!latestTokenUsageEvent) {
          latestTokenUsageEvent = event
        }
      }
    }

    const latestEvent = latestTotalEvent || latestTokenUsageEvent

    if (latestEvent && isEventType(latestEvent, 'token_usage')) {
      const tokenUsage = getEventData(latestEvent) as TokenUsageEvent
      const isTotalEvent = tokenUsage.context === 'conversation_total'
      const contextPercent = isTotalEvent
        ? (tokenUsage.generation_info?.context_usage_percent as number | undefined) ?? tokenUsage.context_usage_percent
        : tokenUsage.context_usage_percent ?? (tokenUsage.generation_info?.context_usage_percent as number | undefined)

      return {
        contextUsagePercent: contextPercent !== undefined && contextPercent !== null ? contextPercent : null,
        latestTokenUsage: tokenUsage
      }
    }

    // Fallback: Check llm_generation_end and tool_call_end events for context usage (iterate backwards)
    for (let i = tabEvents.length - 1; i >= 0; i--) {
      const event = tabEvents[i]
      if (isSubAgentEvent(event)) continue

      if (isEventType(event, 'llm_generation_end')) {
        const data = getEventData(event)
        const metadata = data.metadata as Record<string, unknown> | undefined
        const contextPercent = metadata?.context_usage_percent as number | undefined
        if (contextPercent && contextPercent > 0) {
          return {
            contextUsagePercent: contextPercent,
            latestTokenUsage: {
              context_usage_percent: contextPercent,
              model_context_window: metadata?.model_context_window as number | undefined,
              context_window_usage: metadata?.current_context_window_usage as number | undefined,
              model_id: metadata?.model_id as string | undefined,
            }
          }
        }
      }

      if (isEventType(event, 'tool_call_end')) {
        const data = getEventData(event)
        const contextPercent = data.context_usage_percent
        if (contextPercent && contextPercent > 0) {
          return {
            contextUsagePercent: contextPercent,
            latestTokenUsage: {
              context_usage_percent: contextPercent,
              model_context_window: data.model_context_window,
              context_window_usage: data.context_window_usage,
              model_id: data.model_id,
            }
          }
        }
      }
    }

    return { contextUsagePercent: null, latestTokenUsage: null }
  }, [tabEvents, isSubAgentEvent])

  // Always use tab-specific config (ChatInput is only in multi-agent mode)
  // Memoize to prevent unnecessary re-renders when other config values change
  const chatFileContext = useMemo(() => tabConfig?.fileContext || [], [tabConfig?.fileContext])
  // Use ?? instead of || to preserve false values (user's selection)
  // Only default to false if the value is undefined/null (not explicitly set)
  const isCLIProvider = useMemo(() => tabConfig?.llmConfig?.provider === 'claude-code' || tabConfig?.llmConfig?.provider === 'gemini-cli' || tabConfig?.llmConfig?.provider === 'codex-cli', [tabConfig?.llmConfig?.provider])
  const isClaudeCode = useMemo(() => tabConfig?.llmConfig?.provider === 'claude-code', [tabConfig?.llmConfig?.provider])
  // CLI providers always require code execution mode
  const useCodeExecutionMode = useMemo(() => isCLIProvider ? true : (tabConfig?.useCodeExecutionMode ?? false), [isCLIProvider, tabConfig?.useCodeExecutionMode])
  const useToolSearchMode = useMemo(() => isCLIProvider ? false : (tabConfig?.useToolSearchMode ?? false), [isCLIProvider, tabConfig?.useToolSearchMode])
  const enableWorkspaceAccess = useMemo(() => tabConfig?.enableWorkspaceAccess ?? true, [tabConfig?.enableWorkspaceAccess])
  const enableImageGeneration = useMemo(() => tabConfig?.enableImageGeneration ?? false, [tabConfig?.enableImageGeneration])
  const browserMode = useMemo(() => tabConfig?.browserMode ?? 'none', [tabConfig?.browserMode])
  const enableBrowserAccess = useMemo(() => browserMode === 'headless' || browserMode === 'cdp', [browserMode])
  const useCdp = useMemo(() => browserMode === 'cdp', [browserMode])
  const cdpPort = useMemo(() => tabConfig?.cdpPort ?? 9222, [tabConfig?.cdpPort])
  const isLocalMode = useCapabilitiesStore(state => state.capabilities?.local_mode ?? false)
  const workspaceActiveFolder = useWorkspaceStore(state => state.activeFolder)
  const camofoxHeaded = useMemo(() => tabConfig?.camofoxHeaded ?? true, [tabConfig?.camofoxHeaded])
  const [cdpConnected, setCdpConnected] = useState<boolean | null>(null)
  const [cdpChecking, setCdpChecking] = useState(false)
  const [showCdpPopup, setShowCdpPopup] = useState(false)
  const [showImageGenConfig, setShowImageGenConfig] = useState(false)
  const [showGWSPopup, setShowGWSPopup] = useState(false)
  const [showReasoningPopup, setShowReasoningPopup] = useState(false)
  const [showActiveAgentsPanel, setShowActiveAgentsPanel] = useState(false)
  const [isUploadingFiles, setIsUploadingFiles] = useState(false)
  const [isDraggingFiles, setIsDraggingFiles] = useState(false)
  // Auto-close panel when no agents are running
  useEffect(() => {
    if (activeAgents.length === 0) setShowActiveAgentsPanel(false)
  }, [activeAgents.length])
  const [gwsChatAuthStatus, setGwsChatAuthStatus] = useState<{
    configured?: boolean; auth_method?: string; token_valid?: boolean; token_error?: string;
    enabled_api_count?: number; scope_count?: number; error?: string;
  } | null>(null)
  const [gwsChatChecking, setGwsChatChecking] = useState(false)
  const [gwsChatSyncing, setGwsChatSyncing] = useState(false)
  const [gwsChatSyncResult, setGwsChatSyncResult] = useState<{
    synced?: number; failed?: { name: string; error: string }[]; error?: string;
  } | null>(null)

  // Camofox browser connection state
  const [camofoxConnected, setCamofoxConnected] = useState<boolean | null>(null)
  const [camofoxStarting, setCamofoxStarting] = useState(false)

  // Playwright MCP availability: check if 'playwright' server exists in toolList
  const toolList = useMCPStore(state => state.toolList)
  const playwrightServerStatus = useMemo(() => {
    const entry = toolList.find(t => t.server === 'playwright')
    if (!entry) return 'not_found' as const
    if (entry.status === 'ok') return 'ok' as const
    if (entry.status === 'error') return 'error' as const
    return 'loading' as const
  }, [toolList])

  // Camofox MCP availability: check if 'camofox' server exists in toolList
  const camofoxServerStatus = useMemo(() => {
    const entry = toolList.find(t => t.server === 'camofox')
    if (!entry) return 'not_found' as const
    if (entry.status === 'ok') return 'ok' as const
    if (entry.status === 'error') return 'error' as const
    return 'loading' as const
  }, [toolList])


  const syncGWSChatSkills = useCallback(async () => {
    setGwsChatSyncing(true)
    setGwsChatSyncResult(null)
    try {
      const result = await agentApi.syncGWSSkills()
      setGwsChatSyncResult(result)
    } catch {
      setGwsChatSyncResult({ error: 'Failed to sync skills' })
    } finally {
      setGwsChatSyncing(false)
    }
  }, [])

  const checkGWSChatAuth = useCallback(async () => {
    setGwsChatChecking(true)
    setGwsChatAuthStatus(null)
    try {
      const result = await agentApi.checkGWSAuthStatus()
      setGwsChatAuthStatus(result)
    } catch {
      setGwsChatAuthStatus({ configured: false, error: 'Failed to connect to backend' })
    } finally {
      setGwsChatChecking(false)
    }
  }, [])


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

  const {
    toolList: mcpToolList,
    setChatSelectedServers
  } = useMCPStore()

  const availableServers = useMemo(
    () => [...new Set(mcpToolList.filter(t => t.status === 'ok').map(t => t.server).filter((s): s is string => typeof s === 'string' && !DEDICATED_MCP_SERVERS.has(s)))],
    [mcpToolList]
  )

  const setEnableWorkspaceAccess = useCallback((enabled: boolean) => {
    if (activeTabId) {
      setTabConfig(activeTabId, { enableWorkspaceAccess: enabled })
      // Open workspace sidebar when workspace access is enabled
      if (enabled) {
        setWorkspaceMinimized(false)
      }
    }
  }, [activeTabId, setTabConfig, setWorkspaceMinimized])

  const setEnableImageGeneration = useCallback((enabled: boolean) => {
    if (activeTabId) {
      setTabConfig(activeTabId, { enableImageGeneration: enabled })
    }
  }, [activeTabId, setTabConfig])

  const setBrowserMode = useCallback((mode: 'none' | 'headless' | 'cdp' | 'playwright' | 'stealth') => {
    if (!activeTabId) return

    if (mode === 'stealth') {
      // Stealth: uses camofox MCP server (anti-detect Firefox via camofox-browser on host)
      const currentServers = tabConfig?.selectedServers || []
      const newServers = currentServers.filter(s => s !== 'playwright')
      if (!newServers.includes('camofox')) newServers.push('camofox')
      const cleanedSkills = (tabConfig?.selectedSkills || []).filter(s => s !== 'stealth-browser')
      setTabConfig(activeTabId, {
        browserMode: 'stealth',
        enableBrowserAccess: false,
        useCdp: false,
        enableWorkspaceAccess: true,
        selectedServers: newServers,
        selectedSkills: cleanedSkills,
      })
      setChatSelectedServers(newServers)
      if (!showCdpPopup) setWorkspaceMinimized(false)
    } else if (mode === 'playwright') {
      // Playwright: no virtual tool, add 'playwright' to selectedServers, enable workspace
      const currentServers = tabConfig?.selectedServers || []
      const newServers = currentServers.filter(s => s !== 'camofox')
      if (!newServers.includes('playwright')) newServers.push('playwright')
      setTabConfig(activeTabId, {
        browserMode: 'playwright',
        enableBrowserAccess: false,
        useCdp: false,
        enableWorkspaceAccess: true,
        selectedServers: newServers
      })
      setChatSelectedServers(newServers)
      if (!showCdpPopup) setWorkspaceMinimized(false)
    } else if (mode === 'headless' || mode === 'cdp') {
      // Headless/CDP: use agent_browser virtual tool, remove 'playwright' and 'camofox' from servers
      const currentServers = tabConfig?.selectedServers || []
      const newServers = currentServers.filter(s => s !== 'playwright' && s !== 'camofox')
      setTabConfig(activeTabId, {
        browserMode: mode,
        enableBrowserAccess: true,
        useCdp: mode === 'cdp',
        enableWorkspaceAccess: true,
        selectedServers: newServers
      })
      setChatSelectedServers(newServers)
      if (!showCdpPopup) setWorkspaceMinimized(false)
    } else {
      // None: disable everything, remove 'playwright' and 'camofox' from servers
      const currentServers = tabConfig?.selectedServers || []
      const newServers = currentServers.filter(s => s !== 'playwright' && s !== 'camofox')
      setTabConfig(activeTabId, {
        browserMode: 'none',
        enableBrowserAccess: false,
        useCdp: false,
        selectedServers: newServers
      })
      setChatSelectedServers(newServers)
    }
  }, [activeTabId, tabConfig?.selectedServers, tabConfig?.selectedSkills, setTabConfig, setChatSelectedServers, setWorkspaceMinimized, showCdpPopup])

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

  // Auto-check CDP connection when CDP mode is active or port changes
  useEffect(() => {
    if (browserMode !== 'cdp') {
      setCdpConnected(null)
      return
    }
    const timer = setTimeout(() => {
      checkCdpConnection(cdpPort)
    }, 500)
    return () => clearTimeout(timer)
  }, [browserMode, cdpPort, checkCdpConnection])

  // Auto-start camofox-browser when stealth mode is selected or headed toggle changes
  useEffect(() => {
    if (browserMode !== 'stealth') {
      setCamofoxConnected(null)
      setCamofoxStarting(false)
      return
    }
    let cancelled = false
    const startCamofox = async () => {
      setCamofoxStarting(true)
      setCamofoxConnected(null)
      try {
        const result = await agentApi.startCamofox(camofoxHeaded)
        if (!cancelled) {
          setCamofoxConnected(result.connected)
        }
      } catch {
        if (!cancelled) {
          setCamofoxConnected(false)
        }
      } finally {
        if (!cancelled) {
          setCamofoxStarting(false)
        }
      }
    }
    const timer = setTimeout(startCamofox, 300)
    return () => { cancelled = true; clearTimeout(timer) }
  }, [browserMode, camofoxHeaded])

  // Get preset info for multi-agent mode
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

  // Auto-check GWS auth when popup opens
  useEffect(() => {
    if (showGWSPopup && !gwsChatAuthStatus && !gwsChatChecking) {
      checkGWSChatAuth()
    }
  }, [showGWSPopup]) // eslint-disable-line react-hooks/exhaustive-deps

  // Get queued messages from tab config
  const queuedMessages = useMemo(() => tabConfig?.queuedMessages || [], [tabConfig?.queuedMessages])
  
  // State for summarization
  const [isSummarizing, setIsSummarizing] = useState(false)

  // State for steer message loading
  const [steeringIndex, setSteeringIndex] = useState<number | null>(null)

  // Use tab-specific servers - memoize to prevent re-renders
  const manualSelectedServers = useMemo(() => tabConfig?.selectedServers || [], [tabConfig?.selectedServers])
  
  const gwsEnabled = useMemo(() => tabConfig?.enableGWSAccess ?? false, [tabConfig?.enableGWSAccess])

  const toggleGWSServer = useCallback(() => {
    if (!activeTabId) return
    const newEnabled = !gwsEnabled
    setTabConfig(activeTabId, {
      enableGWSAccess: newEnabled,
      ...(newEnabled ? { enableWorkspaceAccess: true } : {}),
    })
    if (newEnabled) setWorkspaceMinimized(false)
  }, [activeTabId, gwsEnabled, setTabConfig, setWorkspaceMinimized])

  // Server operations (always update tab config AND sync to chat-specific MCP store)
  // This ensures new chat tabs inherit the user's manual server selection
  // Browser servers are mutually exclusive — only one can be active at a time
  const BROWSER_SERVERS = ['playwright', 'camofox'] as const

  const onManualServerToggle = useCallback((server: string) => {
    if (activeTabId) {
      // Remove "NO_SERVERS" if it exists (when selecting a real server)
      const serversWithoutNoServers = manualSelectedServers.filter(s => s !== "NO_SERVERS")

      const isToggling = serversWithoutNoServers.includes(server)
      let newServers: string[]
      if (isToggling) {
        // Toggling off — just remove it
        newServers = serversWithoutNoServers.filter(s => s !== server)
      } else {
        // Toggling on — if it's a browser server, remove the other browser servers
        const isBrowserServer = (BROWSER_SERVERS as readonly string[]).includes(server)
        const base = isBrowserServer
          ? serversWithoutNoServers.filter(s => !(BROWSER_SERVERS as readonly string[]).includes(s))
          : serversWithoutNoServers
        newServers = [...base, server]

        // If enabling a browser server via MCP dropdown, also sync browserMode
        if (server === 'camofox') {
          setTabConfig(activeTabId, {
            selectedServers: newServers,
            browserMode: 'stealth',
            enableBrowserAccess: false,
            useCdp: false,
            enableWorkspaceAccess: true,
          })
          setChatSelectedServers(newServers)
          setWorkspaceMinimized(false)
          return
        }
        if (server === 'playwright') {
          setTabConfig(activeTabId, {
            selectedServers: newServers,
            browserMode: 'playwright',
            enableBrowserAccess: false,
            useCdp: false,
            enableWorkspaceAccess: true,
          })
          setChatSelectedServers(newServers)
          setWorkspaceMinimized(false)
          return
        }
      }

      setTabConfig(activeTabId, { selectedServers: newServers })
      // Sync to chat-specific MCP store so new chat tabs inherit this selection
      setChatSelectedServers(newServers)
    }
  }, [activeTabId, manualSelectedServers, setTabConfig, setChatSelectedServers, setWorkspaceMinimized])
  
  const onSelectAllServers = useCallback(() => {
    if (activeTabId) {
      // Select all servers, but only keep one browser server (mutual exclusivity)
      // Keep whichever browser server is already selected; if none, exclude both
      const currentBrowser = manualSelectedServers.find(s => (BROWSER_SERVERS as readonly string[]).includes(s))
      const allServers = availableServers.filter(s => {
        if (!(BROWSER_SERVERS as readonly string[]).includes(s)) return true
        return s === currentBrowser // only keep the currently active browser server
      })
      setTabConfig(activeTabId, { selectedServers: allServers })
      // Sync to chat-specific MCP store so new chat tabs inherit this selection
      setChatSelectedServers(allServers)
    }
  }, [activeTabId, availableServers, manualSelectedServers, setTabConfig, setChatSelectedServers])

  const onClearAllServers = useCallback(() => {
    if (activeTabId) {
      setTabConfig(activeTabId, { selectedServers: ["NO_SERVERS"] })
      // Sync to chat-specific MCP store so new chat tabs inherit this selection
      setChatSelectedServers(["NO_SERVERS"])
    }
  }, [activeTabId, setTabConfig, setChatSelectedServers])

  // Auto-enable tool search mode in multi-agent mode when more than 2 MCP servers are selected
  useEffect(() => {
    if (selectedModeCategory !== 'multi-agent' || !activeTabId) return
    const realServers = manualSelectedServers.filter(s => s !== 'NO_SERVERS')
    const totalTools = mcpToolList
      .filter(t => t.status === 'ok' && t.server && realServers.includes(t.server))
      .reduce((sum, t) => sum + (t.toolsEnabled || 0), 0)
    if (totalTools >= 10 && !useToolSearchMode) {
      setTabConfig(activeTabId, { useToolSearchMode: true })
    }
  }, [manualSelectedServers, mcpToolList, selectedModeCategory, activeTabId, useToolSearchMode, setTabConfig])

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
    workflowPrimaryConfig
  } = useLLMStore()

  const { scrollToFile } = useWorkspaceStore()
  const { showSkillImport, showSubAgentImport, showMCPDetails, showMCPConfig, showModels, showResume, showWorkflowBuilder, openDialog, closeDialog } = useCommandDialogStore()

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
        provider: llm.provider as 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure' | 'claude-code' | 'gemini-cli' | 'codex-cli',
        model_id: llm.model
      }

      // CLI providers always require code execution mode
      if (llm.provider === 'claude-code' || llm.provider === 'gemini-cli' || llm.provider === 'codex-cli') {
        setTabConfig(activeTabId, { llmConfig: newConfig, useCodeExecutionMode: true, useToolSearchMode: false })
      } else {
        setTabConfig(activeTabId, { llmConfig: newConfig })
      }
    }
  }, [activeTabId, tabConfig?.llmConfig, setTabConfig])

  // Computed values - get LLM option from tab config
  const primaryLLM = useMemo(() => {
    if (isWorkflowPhaseChat) {
      // Show the phase_llm from preset (what the backend actually uses)
      const preset = getActivePreset('workflow')
      const phaseLLM = preset?.llmConfig?.phase_llm
      if (phaseLLM?.provider && phaseLLM?.model_id) {
        const found = availableLLMs.find(llm =>
          llm.provider === phaseLLM.provider && llm.model === phaseLLM.model_id
        )
        if (found) return found
        return {
          provider: phaseLLM.provider,
          model: phaseLLM.model_id,
          label: `${phaseLLM.provider} - ${phaseLLM.model_id}`,
          description: 'Phase LLM'
        }
      }
      // Fallback to preset primary LLM
      const presetLLM = preset?.llmConfig
      if (presetLLM?.provider && presetLLM?.model_id) {
        const found = availableLLMs.find(llm =>
          llm.provider === presetLLM.provider && llm.model === presetLLM.model_id
        )
        if (found) return found
        return {
          provider: presetLLM.provider,
          model: presetLLM.model_id,
          label: `${presetLLM.provider} - ${presetLLM.model_id}`,
          description: 'Workflow preset LLM'
        }
      }
    }

    if (tabConfig?.llmConfig) {
      const config = tabConfig.llmConfig
      const foundLLM = availableLLMs.find(llm =>
        llm.provider === config.provider && llm.model === config.model_id
      )
      if (foundLLM) return foundLLM

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
  }, [tabConfig?.llmConfig, availableLLMs, getCurrentLLMOption, isWorkflowPhaseChat, getActivePreset, workflowPrimaryConfig])
  
  // Preset folder selection
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const fileUploadInputRef = useRef<HTMLInputElement>(null)
  const dragCounterRef = useRef(0)
  
  // Track previous input value to distinguish user deletion from programmatic clearing
  const prevInputTextRef = useRef<string>('')
  
  // File selection dialog state
  const [showFileDialog, setShowFileDialog] = useState(false)
  const [fileDialogPosition, setFileDialogPosition] = useState({ top: 0, left: 0 })
  const [fileSearchQuery, setFileSearchQuery] = useState('')
  const [atPosition, setAtPosition] = useState(-1) // Position of @ in text
  // Extra files for @ dialog (Chats/ — loaded on demand so workflow-scoped trees still show them)
  const [extraAtFiles, setExtraAtFiles] = useState<PlannerFile[]>([])

  // Command selection dialog state
  const [showCommandDialog, setShowCommandDialog] = useState(false)
  const [commandDialogPosition, setCommandDialogPosition] = useState({ bottom: 0, left: 0 })
  const [commandSearchQuery, setCommandSearchQuery] = useState('')
  const [slashPosition, setSlashPosition] = useState(-1) // Position of / in text

  // Command editor dialog state
  const [showCommandEditor, setShowCommandEditor] = useState(false)
  const [editingUserCommand, setEditingUserCommand] = useState<{ folder_name: string; frontmatter: { name: string; description: string; icon?: string; modes?: string[] }; content: string } | null>(null)

  // Workflow selection dialog state (# trigger)
  const [showWorkflowDialog, setShowWorkflowDialog] = useState(false)
  const [workflowDialogPosition, setWorkflowDialogPosition] = useState({ bottom: 0, left: 0 })
  const [workflowSearchQuery, setWorkflowSearchQuery] = useState('')
  const [hashPosition, setHashPosition] = useState(-1) // Position of # in text

  // ! skill inline popup state
  const [showSkillPopup, setShowSkillPopup] = useState(false)
  const [skillPopupPosition, setSkillPopupPosition] = useState({ bottom: 0, left: 0 })
  const [skillPopupSearchQuery, setSkillPopupSearchQuery] = useState('')
  const [exclamationPosition, setExclamationPosition] = useState(-1)

  // $ server inline popup state
  const [showServerPopup, setShowServerPopup] = useState(false)
  const [serverPopupPosition, setServerPopupPosition] = useState({ bottom: 0, left: 0 })
  const [serverPopupSearchQuery, setServerPopupSearchQuery] = useState('')
  const [dollarPosition, setDollarPosition] = useState(-1)

  // ^ sub-agent inline popup state
  const [showSubAgentPopup, setShowSubAgentPopup] = useState(false)
  const [subAgentPopupPosition, setSubAgentPopupPosition] = useState({ bottom: 0, left: 0 })
  const [subAgentPopupSearchQuery, setSubAgentPopupSearchQuery] = useState('')
  const [caretPosition, setCaretPosition] = useState(-1)

  // Lazy-loaded data for inline popups
  const [allSkills, setAllSkills] = useState<Skill[]>([])
  const [allSubAgents, setAllSubAgents] = useState<SubAgent[]>([])
  const [skillsLoading, setSkillsLoading] = useState(false)
  const [subAgentsLoading, setSubAgentsLoading] = useState(false)

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

  // Get active preset for multi-agent mode (used for preset query sync and UI)
  const chatActivePreset = getActivePreset('multi-agent')
  
  // Sync tab config inputText with preset query when preset is selected
  useEffect(() => {
    const activePresetId = activePresetIds['multi-agent']
    
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


  // Close workspace sidebar when workspace access is disabled
  // Do NOT force it open — respect the user's persisted preference
  useEffect(() => {
    if (!enableWorkspaceAccess) {
      setWorkspaceMinimized(true)
      if (showFileDialog) {
        setShowFileDialog(false)
        setAtPosition(-1)
        setFileSearchQuery('')
      }
    }
  }, [enableWorkspaceAccess, setWorkspaceMinimized, showFileDialog])

  // Fetch Chats/ on demand when @ dialog opens (these may not be in the
  // workspace tree when it's scoped to a workflow folder).
  // The API returns the CONTENTS of a folder, so we wrap them in synthetic folder entries.
  useEffect(() => {
    if (!showFileDialog) return
    let cancelled = false
    const fetchExtraFolders = async () => {
      try {
        const chats = await agentApi.getPlannerFiles('Chats', -1, 2).catch(() => null)
        if (cancelled) return
        const extra: PlannerFile[] = []
        if (chats?.success && chats.data?.length) {
          extra.push({ filepath: 'Chats', content: '', last_modified: '', type: 'folder', children: chats.data })
        }
        setExtraAtFiles(extra)
      } catch {
        // Silently ignore
      }
    }
    fetchExtraFolders()
    return () => { cancelled = true }
  }, [showFileDialog])

  // Lazy-load skills when ! popup opens (always re-fetch to pick up new skills)
  useEffect(() => {
    // console.log(DBG + ' showSkillPopup changed:', showSkillPopup)
    if (showSkillPopup) {
      setSkillsLoading(true)
      skillsApi.listSkills()
        .then(res => {
          const raw = res.skills || []
          const seen = new Set<string>()
          const unique = raw.filter((s: { file_path?: string; folder_name: string }) => {
            if (seen.has(s.folder_name)) return false
            seen.add(s.folder_name)
            return true
          })
          // console.log(DBG + ' skills loaded:', raw.length, '→ deduplicated:', unique.length)
          setAllSkills(unique)
        })
        .catch((err: unknown) => { console.error(DBG + ' skills load error:', err) })
        .finally(() => setSkillsLoading(false))
    }
  }, [showSkillPopup])

  // Lazy-load sub-agents when ^ popup opens (always re-fetch to pick up new custom sub-agents)
  useEffect(() => {
    if (showSubAgentPopup) {
      setSubAgentsLoading(true)
      subagentsApi.listSubAgents()
        .then(res => setAllSubAgents(res.subagents || []))
        .catch(() => {})
        .finally(() => setSubAgentsLoading(false))
    }
  }, [showSubAgentPopup])

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

  // Guard: prevent form submit from firing when Stop button click causes a button swap
  // (React re-renders Stop→Send mid-click, causing the browser to dispatch submit on the new button)
  const justStoppedStreamingRef = useRef(false)

  // Guard: prevent double submission from rapid Enter presses, key repeat, or double-clicks
  // queryToSubmit is a memoized value that doesn't update until re-render, so a second
  // submit within the same render cycle would re-send the same message
  const justSubmittedRef = useRef(false)

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

    // Skip most special character triggers for workflow phase chat — but allow @ for file references
    if (isWorkflowPhaseChat) {
      // Only process @ trigger in workflow phase chat
      const cursorPos = e.target.selectionStart || 0
      const textBefore = newValue.substring(0, cursorPos)
      const atIdx = textBefore.lastIndexOf('@')
      if (atIdx >= 0 && enableWorkspaceAccess) {
        const textAfterAt = textBefore.substring(atIdx + 1)
        const hasValidAt = textAfterAt === '' || textAfterAt.match(/^[a-zA-Z0-9/._\-\\]*$/)
        if (hasValidAt) {
          setAtPosition(atIdx)
          setFileSearchQuery(textAfterAt)
          setShowFileDialog(true)

          const textarea = e.target
          const rect = textarea.getBoundingClientRect()
          const dialogHeight = 320
          const spaceAbove = rect.top
          setFileDialogPosition({
            top: spaceAbove > dialogHeight ? rect.top - dialogHeight - 8 : rect.bottom + 8,
            left: rect.left + window.scrollX
          })
        } else {
          setShowFileDialog(false)
          setAtPosition(-1)
          setFileSearchQuery('')
        }
      } else {
        setShowFileDialog(false)
        setAtPosition(-1)
        setFileSearchQuery('')
      }
      return
    }

    const cursorPosition = e.target.selectionStart || 0
    const textBeforeCursor = newValue.substring(0, cursorPosition)

    const lastSlashIndex = textBeforeCursor.lastIndexOf('/')
    const lastAtIndex = textBeforeCursor.lastIndexOf('@')
    const lastHashIndex = textBeforeCursor.lastIndexOf('#')
    const lastExclamationIndex = textBeforeCursor.lastIndexOf('!')
    const lastDollarIndex = textBeforeCursor.lastIndexOf('$')
    const lastCaretIndex = textBeforeCursor.lastIndexOf('^')

    // If @ appears before the current /, the / is part of a path (e.g. "@ workflow /") — stay in file dialog
    const slashIsPartOfAtPath = lastAtIndex >= 0 && lastSlashIndex > lastAtIndex

    const slashDistance = lastSlashIndex >= 0 ? cursorPosition - lastSlashIndex : Infinity
    const atDistance = lastAtIndex >= 0 ? cursorPosition - lastAtIndex : Infinity
    const hashDistance = lastHashIndex >= 0 ? cursorPosition - lastHashIndex : Infinity
    const exclamationDistance = lastExclamationIndex >= 0 ? cursorPosition - lastExclamationIndex : Infinity
    const dollarDistance = lastDollarIndex >= 0 ? cursorPosition - lastDollarIndex : Infinity
    const caretDistance = lastCaretIndex >= 0 ? cursorPosition - lastCaretIndex : Infinity

    // Check if # is a markdown heading (at line start AND followed by a space) — don't trigger dialog for headings
    // e.g. "# Heading" is a heading, but "#workflow" is a workflow trigger
    const charAfterHash = lastHashIndex >= 0 ? newValue[lastHashIndex + 1] : undefined
    const hashIsAtLineStart = lastHashIndex >= 0 && (lastHashIndex === 0 || textBeforeCursor[lastHashIndex - 1] === '\n')
    const hashIsHeading = hashIsAtLineStart && charAfterHash === ' '

    // Find the closest trigger to cursor
    const closestTrigger = Math.min(slashDistance, atDistance, hashDistance, exclamationDistance, dollarDistance, caretDistance)

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
    // Check for ! skill trigger
    else if (lastExclamationIndex >= 0 && closestTrigger === exclamationDistance) {
      const textAfterExcl = textBeforeCursor.substring(lastExclamationIndex + 1)
      const hasValidExcl = textAfterExcl === '' || textAfterExcl.match(/^[a-zA-Z0-9_-]*$/)
      // console.log(DBG + ' ! trigger — textAfterExcl:', JSON.stringify(textAfterExcl), 'hasValidExcl:', hasValidExcl)

      if (hasValidExcl) {
        setExclamationPosition(lastExclamationIndex)
        setSkillPopupSearchQuery(textAfterExcl)
        setShowSkillPopup(true)
        // console.log(DBG + ' ! trigger — setSkillPopupSearchQuery:', JSON.stringify(textAfterExcl))
        setShowCommandDialog(false)
        setShowFileDialog(false)
        setShowWorkflowDialog(false)
        setShowServerPopup(false)
        setShowSubAgentPopup(false)

        const textarea = e.target
        const rect = textarea.getBoundingClientRect()
        setSkillPopupPosition({
          bottom: window.innerHeight - rect.top + 8,
          left: rect.left + window.scrollX
        })
      } else {
        setShowSkillPopup(false)
        setExclamationPosition(-1)
        setSkillPopupSearchQuery('')
      }
    }
    // Check for $ server trigger
    else if (lastDollarIndex >= 0 && closestTrigger === dollarDistance) {
      const textAfterDollar = textBeforeCursor.substring(lastDollarIndex + 1)
      const hasValidDollar = textAfterDollar === '' || textAfterDollar.match(/^[a-zA-Z0-9_-]*$/)

      if (hasValidDollar) {
        setDollarPosition(lastDollarIndex)
        setServerPopupSearchQuery(textAfterDollar)
        setShowServerPopup(true)
        setShowCommandDialog(false)
        setShowFileDialog(false)
        setShowWorkflowDialog(false)
        setShowSkillPopup(false)
        setShowSubAgentPopup(false)

        const textarea = e.target
        const rect = textarea.getBoundingClientRect()
        setServerPopupPosition({
          bottom: window.innerHeight - rect.top + 8,
          left: rect.left + window.scrollX
        })
      } else {
        setShowServerPopup(false)
        setDollarPosition(-1)
        setServerPopupSearchQuery('')
      }
    }
    // Check for ^ sub-agent trigger
    else if (lastCaretIndex >= 0 && closestTrigger === caretDistance) {
      const textAfterCaret = textBeforeCursor.substring(lastCaretIndex + 1)
      const hasValidCaret = textAfterCaret === '' || textAfterCaret.match(/^[a-zA-Z0-9_-]*$/)

      if (hasValidCaret) {
        setCaretPosition(lastCaretIndex)
        setSubAgentPopupSearchQuery(textAfterCaret)
        setShowSubAgentPopup(true)
        setShowCommandDialog(false)
        setShowFileDialog(false)
        setShowWorkflowDialog(false)
        setShowSkillPopup(false)
        setShowServerPopup(false)

        const textarea = e.target
        const rect = textarea.getBoundingClientRect()
        setSubAgentPopupPosition({
          bottom: window.innerHeight - rect.top + 8,
          left: rect.left + window.scrollX
        })
      } else {
        setShowSubAgentPopup(false)
        setCaretPosition(-1)
        setSubAgentPopupSearchQuery('')
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
      // console.log(DBG + ' no trigger matched — closing all popups. textBeforeCursor:', JSON.stringify(textBeforeCursor), 'closestTrigger:', closestTrigger)
      setShowFileDialog(false)
      setAtPosition(-1)
      setFileSearchQuery('')
      setShowCommandDialog(false)
      setSlashPosition(-1)
      setCommandSearchQuery('')
      setShowWorkflowDialog(false)
      setHashPosition(-1)
      setWorkflowSearchQuery('')
      setShowSkillPopup(false)
      setExclamationPosition(-1)
      setSkillPopupSearchQuery('')
      setShowServerPopup(false)
      setDollarPosition(-1)
      setServerPopupSearchQuery('')
      setShowSubAgentPopup(false)
      setCaretPosition(-1)
      setSubAgentPopupSearchQuery('')
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
  }, [chatFileContext, removeFileFromContext, showCommandDialog, showWorkflowDialog, activeTabId, setTabConfig, enableWorkspaceAccess, adjustTextareaHeight, isWorkflowPhaseChat])

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
    if (showCommandDialog || showFileDialog || showWorkflowDialog || showSkillPopup || showServerPopup || showSubAgentPopup) {
      // Prevent default for arrow keys, enter, escape so textarea doesn't move cursor
      if (['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight', 'Enter', 'Escape'].includes(e.key)) {
        e.preventDefault()
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
        const saContext = 'You are in Sub-Agent Builder mode. Create a new sub-agent template in subagents/custom/. Follow the SUBAGENT.md format with YAML frontmatter (name, description, default_reasoning_level, default_tool_mode) and markdown instructions.'
        const message = textAfterCommand
          ? `${textAfterCommand}\n\n${saContext}`
          : `I want to build a sub-agent template. ${saContext}`
        onSubmit(message)
        return
      }

      // Handle /add-skill command — open the import dialog
      if (trimmedQuery.startsWith('/add-skill')) {
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
      if (trimmedQuery.startsWith('/mcp-add')) {
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
      if (trimmedQuery.startsWith('/mcp')) {
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
      if (trimmedQuery.startsWith('/models')) {
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
      if (trimmedQuery.startsWith('/resume')) {
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
        // Guard: prevent double submission from rapid key repeat or double press
        if (justSubmittedRef.current) return
        justSubmittedRef.current = true
        setTimeout(() => { justSubmittedRef.current = false }, 300)

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
  }, [inputText, onSubmit, showFileDialog, showCommandDialog, showWorkflowDialog, showSkillPopup, showServerPopup, showSubAgentPopup, tabSessionId, canSubmit, canSubmitImmediately, queryToSubmit, isSummarizing, isStreaming, handleSummarize, handleCompact, activeTabId, setTabConfig, tabConfig?.queuedMessages, onStopStreaming, openDialog, tabConfig?.selectedSkills, addToast, isMultiAgentMode])

  const handleSubmit = useCallback((e: React.FormEvent) => {
    e.preventDefault()

    // Guard: ignore form submit triggered by Stop→Send button swap during a click
    if (justStoppedStreamingRef.current) {
      return
    }

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
      // Guard: prevent double submission from rapid clicks
      if (justSubmittedRef.current) return
      justSubmittedRef.current = true
      setTimeout(() => { justSubmittedRef.current = false }, 300)

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

    // Look up and execute the command from the registry
    const cmd = findCommand(command)
    if (cmd) {
      const ctx: CommandContext = {
        beforeSlash,
        activeTabId,
        tabSessionId,
        tabConfig,
        isSummarizing,
        isStreaming,
        onSubmit,
        openDialog,
        setTabConfig,
        addToast,
        handleSummarize,
        handleCompact,
        getAppStore: () => useAppStore.getState(),
        getWorkspaceStore: () => useWorkspaceStore.getState()
      }
      cmd.execute(ctx)
    } else {
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
  }, [inputText, slashPosition, commandSearchQuery, activeTabId, tabSessionId, isSummarizing, isStreaming, handleSummarize, handleCompact, tabConfig, setTabConfig, onSubmit, openDialog, addToast])

  // Command management callbacks
  const handleManageCommands = useCallback(() => {
    setShowCommandDialog(false)
    setEditingUserCommand(null)
    setShowCommandEditor(true)
  }, [])

  const handleEditCommand = useCallback((cmd: CommandDefinition) => {
    setShowCommandDialog(false)
    // Fetch full command data from API to populate editor
    commandsApi.getCommand(cmd.command).then(uc => {
      setEditingUserCommand({
        folder_name: uc.folder_name,
        frontmatter: uc.frontmatter,
        content: uc.content
      })
      setShowCommandEditor(true)
    }).catch(() => {
      addToast('Failed to load command for editing', 'error')
    })
  }, [addToast])

  const handleDeleteCommand = useCallback(async (cmd: CommandDefinition) => {
    try {
      await commandsApi.deleteCommand(cmd.command)
      await loadAndRegisterUserCommands()
      addToast(`Command /${cmd.command} deleted`, 'success')
    } catch {
      addToast('Failed to delete command', 'error')
    }
  }, [addToast])

  const handleCommandEditorClose = useCallback(() => {
    setShowCommandEditor(false)
    setEditingUserCommand(null)
  }, [])

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

    // If the selected file is a direct multi-agent plan folder, write .last_used timestamp
    const isPlanFolder = file.type === 'folder' && /^Chats\/[^/]+$/.test(file.filepath)
    if (isPlanFolder) {
      agentApi.updatePlannerFile(`${file.filepath}/.last_used`, new Date().toISOString()).catch(() => {})
    }

    // Focus back to textarea and position cursor after the space
    setTimeout(() => {
      if (textareaRef.current) {
        textareaRef.current.focus()
        const cursorPosition = beforeAt.length + '@'.length + file.filepath.length + ' '.length
        textareaRef.current.setSelectionRange(cursorPosition, cursorPosition)
      }
    }, 0)
  }, [inputText, atPosition, fileSearchQuery, chatFileContext, addFileToContext, scrollToFile, activeTabId, setTabConfig])

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

  const uploadTargetFolder = useMemo(() => {
    if (selectedModeCategory === 'workflow') {
      return workspaceActiveFolder || 'Workflow'
    }
    return 'Chats'
  }, [selectedModeCategory, workspaceActiveFolder])

  const uploadFilesToChat = useCallback(async (files: File[]) => {
    if (files.length === 0 || isUploadingFiles) {
      console.info('[CHAT_UPLOAD] no files selected or upload already in progress', { fileCount: files.length, isUploadingFiles })
      return
    }

    setIsUploadingFiles(true)
    addToast(`Uploading ${files.length} file${files.length > 1 ? 's' : ''}...`, 'info')
    console.info('[CHAT_UPLOAD] starting upload', { count: files.length, target: uploadTargetFolder })
    const uploadedPaths: string[] = []
    const failures: string[] = []

    for (const file of files) {
      try {
        console.info('[CHAT_UPLOAD] uploading file', { name: file.name, size: file.size, type: file.type })
        const response = await agentApi.uploadPlannerFile(file, uploadTargetFolder, `Upload ${file.name} from chat input`)
        const uploadedPath =
          response?.data?.file_path ||
          response?.data?.filepath ||
          response?.file_path ||
          response?.filepath
        if (uploadedPath && typeof uploadedPath === 'string') {
          uploadedPaths.push(uploadedPath)
          console.info('[CHAT_UPLOAD] upload success', { name: file.name, path: uploadedPath })
        } else {
          failures.push(`${file.name}: Upload succeeded but filepath missing in response`)
          console.error('[CHAT_UPLOAD] missing filepath in upload response', response)
        }
      } catch (error) {
        const message = error instanceof Error ? error.message : 'Upload failed'
        failures.push(`${file.name}: ${message}`)
        console.error('[CHAT_UPLOAD] upload failed', { name: file.name, error })
      }
    }

    if (uploadedPaths.length > 0) {
      uploadedPaths.forEach((path) => {
        const exists = chatFileContext.some((item: { path: string }) => item.path === path)
        if (!exists) {
          addFileToContext({
            name: path.split('/').pop() || path,
            path,
            type: 'file'
          })
        }
      })

      const refs = uploadedPaths.map(path => `@${path}`).join(' ')
      const prefix = inputText.trim().length > 0 ? `${inputText} ` : ''
      const newText = `${prefix}${refs} `
      setLocalInputText(newText)
      if (activeTabId) {
        setTabConfig(activeTabId, { inputText: newText })
      }

      const ws = useWorkspaceStore.getState()
      ws.fetchFiles(ws.activeFolder ?? undefined).catch(() => {})

      addToast(
        `Uploaded ${uploadedPaths.length}/${files.length} file${files.length > 1 ? 's' : ''} to ${uploadTargetFolder}`,
        'success'
      )

      setTimeout(() => {
        textareaRef.current?.focus()
      }, 0)
    }

    if (failures.length > 0) {
      addToast(`Upload failed for ${failures.length} file(s): ${failures.slice(0, 2).join('; ')}`, 'error')
    }
    if (uploadedPaths.length === 0 && failures.length === 0) {
      addToast('No files were uploaded. Please try again.', 'error')
    }
    console.info('[CHAT_UPLOAD] upload completed', { uploadedCount: uploadedPaths.length, failureCount: failures.length })

    setIsUploadingFiles(false)
  }, [activeTabId, isUploadingFiles, uploadTargetFolder, chatFileContext, addFileToContext, inputText, setTabConfig, addToast])

  const handleUploadFilesSelected = useCallback(async (event: React.ChangeEvent<HTMLInputElement>) => {
    console.info('[CHAT_UPLOAD] file input change fired')
    const files = event.target.files ? Array.from(event.target.files) : []
    event.target.value = ''
    await uploadFilesToChat(files)
  }, [uploadFilesToChat])

  const handleTextareaDragEnter = useCallback((event: React.DragEvent<HTMLTextAreaElement>) => {
    if (!event.dataTransfer?.types?.includes('Files')) return
    event.preventDefault()
    event.stopPropagation()
    dragCounterRef.current += 1
    setIsDraggingFiles(true)
  }, [])

  const handleTextareaDragOver = useCallback((event: React.DragEvent<HTMLTextAreaElement>) => {
    if (!event.dataTransfer?.types?.includes('Files')) return
    event.preventDefault()
    event.stopPropagation()
    event.dataTransfer.dropEffect = 'copy'
  }, [])

  const handleTextareaDragLeave = useCallback((event: React.DragEvent<HTMLTextAreaElement>) => {
    if (!event.dataTransfer?.types?.includes('Files')) return
    event.preventDefault()
    event.stopPropagation()
    dragCounterRef.current = Math.max(0, dragCounterRef.current - 1)
    if (dragCounterRef.current === 0) {
      setIsDraggingFiles(false)
    }
  }, [])

  const handleTextareaDrop = useCallback(async (event: React.DragEvent<HTMLTextAreaElement>) => {
    if (!event.dataTransfer?.files) return
    event.preventDefault()
    event.stopPropagation()
    dragCounterRef.current = 0
    setIsDraggingFiles(false)
    const files = Array.from(event.dataTransfer.files)
    console.info('[CHAT_UPLOAD] files dropped', { count: files.length })
    await uploadFilesToChat(files)
  }, [uploadFilesToChat])

  // Inline skill popup: toggle skill (stays open for multi-select)
  const handleSkillPopupToggle = useCallback((skillFolderName: string) => {
    onSkillToggle(skillFolderName)
  }, [onSkillToggle])

  // Close skill popup: remove trigger text and close
  const handleSkillPopupClose = useCallback(() => {
    if (exclamationPosition >= 0) {
      const before = inputText.substring(0, exclamationPosition)
      const after = inputText.substring(exclamationPosition + 1 + skillPopupSearchQuery.length)
      const newText = (before + after).replace(/  +/g, ' ')
      setLocalInputText(newText)
      if (syncToStoreTimeoutRef.current) {
        clearTimeout(syncToStoreTimeoutRef.current)
        syncToStoreTimeoutRef.current = null
      }
      if (activeTabId) {
        setTabConfig(activeTabId, { inputText: newText })
      }
    }
    setShowSkillPopup(false)
    setExclamationPosition(-1)
    setSkillPopupSearchQuery('')
    setTimeout(() => textareaRef.current?.focus(), 0)
  }, [exclamationPosition, inputText, skillPopupSearchQuery, activeTabId, setTabConfig])

  // Inline server popup: toggle server (stays open for multi-select)
  const handleServerPopupToggle = useCallback((serverName: string) => {
    onManualServerToggle(serverName)
  }, [onManualServerToggle])

  // Close server popup: remove trigger text and close
  const handleServerPopupClose = useCallback(() => {
    if (dollarPosition >= 0) {
      const before = inputText.substring(0, dollarPosition)
      const after = inputText.substring(dollarPosition + 1 + serverPopupSearchQuery.length)
      const newText = (before + after).replace(/  +/g, ' ')
      setLocalInputText(newText)
      if (syncToStoreTimeoutRef.current) {
        clearTimeout(syncToStoreTimeoutRef.current)
        syncToStoreTimeoutRef.current = null
      }
      if (activeTabId) {
        setTabConfig(activeTabId, { inputText: newText })
      }
    }
    setShowServerPopup(false)
    setDollarPosition(-1)
    setServerPopupSearchQuery('')
    setTimeout(() => textareaRef.current?.focus(), 0)
  }, [dollarPosition, inputText, serverPopupSearchQuery, activeTabId, setTabConfig])

  // Inline sub-agent popup: toggle sub-agent (stays open for multi-select)
  const handleSubAgentPopupToggle = useCallback((folderName: string) => {
    onSubAgentToggle(folderName)
  }, [onSubAgentToggle])

  // Close sub-agent popup: remove trigger text and close
  const handleSubAgentPopupClose = useCallback(() => {
    if (caretPosition >= 0) {
      const before = inputText.substring(0, caretPosition)
      const after = inputText.substring(caretPosition + 1 + subAgentPopupSearchQuery.length)
      const newText = (before + after).replace(/  +/g, ' ')
      setLocalInputText(newText)
      if (syncToStoreTimeoutRef.current) {
        clearTimeout(syncToStoreTimeoutRef.current)
        syncToStoreTimeoutRef.current = null
      }
      if (activeTabId) {
        setTabConfig(activeTabId, { inputText: newText })
      }
    }
    setShowSubAgentPopup(false)
    setCaretPosition(-1)
    setSubAgentPopupSearchQuery('')
    setTimeout(() => textareaRef.current?.focus(), 0)
  }, [caretPosition, inputText, subAgentPopupSearchQuery, activeTabId, setTabConfig])

  // Memoized items arrays for inline popups
  const skillPopupItems: InlineSelectionItem[] = useMemo(() => {
    const seen = new Set<string>()
    return allSkills
      .filter(s => {
        if (seen.has(s.folder_name)) return false
        seen.add(s.folder_name)
        return true
      })
      .map(s => ({
        id: s.folder_name,
        name: s.frontmatter.name,
        description: s.frontmatter.description,
        isSelected: selectedSkills.includes(s.folder_name)
      }))
  }
  , [allSkills, selectedSkills])

  const serverPopupItems: InlineSelectionItem[] = useMemo(() =>
    [...new Set(availableServers)].map(name => ({
      id: name,
      name,
      isSelected: manualSelectedServers.includes(name)
    }))
  , [availableServers, manualSelectedServers])

  const subAgentPopupItems: InlineSelectionItem[] = useMemo(() => {
    const seen = new Set<string>()
    return allSubAgents
      .filter(sa => {
        if (seen.has(sa.folder_name)) return false
        seen.add(sa.folder_name)
        return true
      })
      .map(sa => ({
        id: sa.folder_name,
        name: sa.frontmatter.name,
        description: sa.frontmatter.description,
        isSelected: selectedSubAgents.includes(sa.folder_name)
      }))
  }, [allSubAgents, selectedSubAgents])

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

  // Removed editing preset query functionality - not needed for multi-agent mode

  // Check if query is valid (view-only tabs cannot submit)
  const hasValidQuery = Boolean(inputText?.trim())
  // Block submission if CDP is enabled but not connected, or playwright server not found
  const isCdpDisconnected = browserMode === 'cdp' && cdpConnected === false
  const isPlaywrightMissing = browserMode === 'playwright' && playwrightServerStatus === 'not_found'
  const isCamofoxMissing = browserMode === 'stealth' && (camofoxServerStatus === 'not_found' || camofoxStarting || camofoxConnected === false)
  const submitButtonDisabled = !hasValidQuery || !tabSessionId || isViewOnly || isCdpDisconnected || isPlaywrightMissing || isCamofoxMissing
  
  // Memoized placeholder
  const placeholder = useMemo(() => {
    if (isViewOnly) return "View only — cannot continue this conversation"
    if (isWorkflowPhaseChat) {
      return 'Chat with the workflow builder...'
    }
    const baseHints = "@ files, / commands, # workflows, ! skills, $ servers, ^ agents"
    if (isMultiAgentMode) return `Ask anything... (${baseHints})`
    return `Ask anything... (${baseHints})`
  }, [isViewOnly, isMultiAgentMode, isWorkflowPhaseChat, workflowPhaseId])

  // For view-only (restored) tabs, show a minimal indicator instead of the full input form
  if (isViewOnly) {
    return (
      <div className="px-4 py-2 border-t border-gray-200 dark:border-gray-700">
        <div className="flex items-center justify-center gap-2 py-1 text-xs text-muted-foreground">
          <History className="w-3.5 h-3.5" />
          <span>View only — restored conversation</span>
        </div>
      </div>
    )
  }

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
                {queuedMessages.map((msg: string, index: number) => {
                  const isLong = msg.length > 150
                  const preview = isLong ? msg.substring(0, 150) + '...' : msg
                  return (
                    <QueuedMessageItem
                      key={index}
                      index={index}
                      msg={msg}
                      preview={preview}
                      isLong={isLong}
                      onDelete={() => {
                        if (activeTabId) {
                          const updated = queuedMessages.filter((_: string, i: number) => i !== index)
                          setTabConfig(activeTabId, { queuedMessages: updated })
                        }
                      }}
                      onSteer={isStreaming && tabSessionId ? async () => {
                        setSteeringIndex(index)
                        try {
                          await agentApi.steerMessage(tabSessionId, msg)
                          // Remove from queue on success
                          if (activeTabId) {
                            const updated = queuedMessages.filter((_: string, i: number) => i !== index)
                            setTabConfig(activeTabId, { queuedMessages: updated })
                          }
                        } catch (err) {
                          addToast('Failed to steer message: ' + (err instanceof Error ? err.message : 'Unknown error'), 'error')
                        } finally {
                          setSteeringIndex(null)
                        }
                      } : undefined}
                      isSteering={steeringIndex === index}
                    />
                  )
                })}
              </div>
            )}
            {/* Show text input */}
            <Textarea
              ref={textareaRef}
              value={inputText}
              onChange={handleTextChange}
              onKeyDown={handleKeyDown}
              onDragEnter={handleTextareaDragEnter}
              onDragOver={handleTextareaDragOver}
              onDragLeave={handleTextareaDragLeave}
              onDrop={handleTextareaDrop}
              placeholder={placeholder}
              className={`!min-h-[40px] max-h-[100px] resize-none text-xs overflow-y-auto leading-[1.3] !py-1 !px-3 placeholder:text-xs ${
                isDraggingFiles ? 'ring-2 ring-blue-500 border-blue-500 bg-blue-50/30 dark:bg-blue-900/10' : ''
              }`}
              disabled={isSummarizing || !tabSessionId || isViewOnly}
              data-testid="chat-input-textarea"
            />
            {isDraggingFiles && (
              <div className="text-[11px] text-blue-600 dark:text-blue-400 px-1">
                Drop files to upload and attach to this chat
              </div>
            )}
            <div className="flex justify-between items-center">
              <div className="flex items-center gap-2">
                {/* Agent Mode Selector — hidden in workflow mode, show LLM label instead */}
                {hideExtras ? (
                  isWorkflowPhaseChat ? (
                    <div className="flex items-center gap-1 px-2 py-1.5 rounded-md border border-gray-300 dark:border-gray-600 bg-gray-100 dark:bg-gray-800 text-gray-500 dark:text-gray-400 text-xs">
                      {primaryLLM?.provider && primaryLLM?.model ? `${primaryLLM.provider}/${primaryLLM.model.split('/').pop()}` : 'LLM'}
                    </div>
                  ) : null
                ) : (
                  isClaudeCode ? (
                    /* Claude Code always uses code execution mode */
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <div className="flex items-center gap-1 p-1.5 rounded-md border border-amber-400 dark:border-amber-600 bg-amber-100 dark:bg-amber-900/40 text-amber-600 dark:text-amber-400 cursor-default">
                          <Code2 className="w-4 h-4 flex-shrink-0" />
                          <span className="text-xs font-medium">Code</span>
                        </div>
                      </TooltipTrigger>
                      <TooltipContent side="top">
                        <p>Claude Code always uses code execution mode</p>
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
                    {/* Code Execution Mode */}
                    <button
                      type="button"
                      onClick={() => {
                        setUseCodeExecutionMode(true)
                        setUseToolSearchMode(false)
                      }}
                      disabled={isStreaming || isSummarizing}
                      data-testid="agent-mode-code-exec"
                      className={`group flex items-center gap-1 p-1.5 rounded-md border transition-all duration-200 ${
                        useCodeExecutionMode
                          ? 'bg-amber-100 dark:bg-amber-900/40 border-amber-400 dark:border-amber-600 text-amber-600 dark:text-amber-400'
                          : 'bg-gray-100 dark:bg-gray-800 border-gray-300 dark:border-gray-600 text-gray-400 dark:text-gray-500'
                      } ${(isStreaming || isSummarizing) ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer hover:pr-2'}`}
                    >
                      <Code2 className="w-4 h-4 flex-shrink-0" />
                      <span className="text-xs font-medium max-w-0 overflow-hidden whitespace-nowrap group-hover:max-w-[50px] transition-all duration-200">
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
                  )
                )}

                {/* Server and LLM Selection — hidden in workflow phase chat (servers come from preset) */}
                {(
                  <div className="flex items-center gap-2">

                      <>
                        {!hideExtras && (
                        <ServerSelectionDropdown
                          availableServers={availableServers}
                          selectedServers={manualSelectedServers}
                          onServerToggle={onManualServerToggle}
                          onSelectAll={onSelectAllServers}
                          onClearAll={onClearAllServers}
                          disabled={isStreaming || isSummarizing}
                          agentMode={agentMode}
                        />
                        )}
                        {!hideExtras && (
                          <SkillSelectionDropdown
                            selectedSkills={selectedSkills}
                            onSkillToggle={onSkillToggle}
                            onSelectAll={onSelectAllSkills}
                            onClearAll={onClearAllSkills}
                            disabled={isStreaming || isSummarizing}
                            onImportClick={() => openDialog('skillImport')}
                          />
                        )}
                        {!hideExtras && effectiveDelegationMode === 'spawn' && (
                          <SubAgentSelectionDropdown
                            selectedSubAgents={selectedSubAgents}
                            onSubAgentToggle={onSubAgentToggle}
                            onSelectAll={onSelectAllSubAgents}
                            onClearAll={onClearAllSubAgents}
                            disabled={isStreaming || isSummarizing}
                            onImportClick={() => openDialog('subAgentImport')}
                          />
                        )}
                      </>

                    {!hideExtras && !isMultiAgentMode && (
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
                    {/* Workspace Access Toggle - hidden for phase chat, always on in multi-agent, toggleable in chat */}
                    {!hideExtras && (isMultiAgentMode ? (
                      <div className="flex items-center gap-1 p-1.5 rounded-md border bg-blue-100 dark:bg-blue-900/40 border-blue-400 dark:border-blue-600 text-blue-600 dark:text-blue-400">
                        <FolderOpen className="w-4 h-4 flex-shrink-0" />
                      </div>
                    ) : (
                      <button
                        type="button"
                        onClick={() => setEnableWorkspaceAccess(!enableWorkspaceAccess)}
                        disabled={isStreaming || isSummarizing || browserMode !== 'none'}
                        className={`group flex items-center gap-1 p-1.5 rounded-md border transition-all duration-200 ${
                          enableWorkspaceAccess
                            ? 'bg-blue-100 dark:bg-blue-900/40 border-blue-400 dark:border-blue-600 text-blue-600 dark:text-blue-400'
                            : 'bg-gray-100 dark:bg-gray-800 border-gray-300 dark:border-gray-600 text-gray-400 dark:text-gray-500'
                        } ${(isStreaming || isSummarizing || browserMode !== 'none') ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer hover:pr-2'}`}
                      >
                        <FolderOpen className="w-4 h-4 flex-shrink-0" />
                        <span className="text-xs font-medium max-w-0 overflow-hidden whitespace-nowrap group-hover:max-w-[80px] transition-all duration-200">
                          Workspace
                        </span>
                      </button>
                    ))}
                    {/* Browser Access Toggle — hidden in workflow mode */}
                    {!hideExtras && <button
                      type="button"
                      onClick={() => {
                        if (browserMode === 'none') {
                          // Enabling browser: show config popup and default to headless
                          setBrowserMode('headless')
                          setShowCdpPopup(true)
                          setWorkspaceMinimized(true)
                        } else {
                          // Clicking again while enabled: re-open popup to change settings
                          setShowCdpPopup(true)
                          setWorkspaceMinimized(true)
                        }
                      }}
                      disabled={isStreaming || isSummarizing}
                      className={`group flex items-center gap-1 p-1.5 rounded-md border transition-all duration-200 ${
                        browserMode === 'stealth'
                          ? camofoxConnected === false || camofoxServerStatus === 'not_found'
                            ? 'bg-red-900/40 border-red-600 text-red-400'
                            : camofoxStarting
                              ? 'bg-yellow-900/40 border-yellow-600 text-yellow-400'
                              : 'bg-orange-900/40 border-orange-600 text-orange-400'
                          : browserMode === 'cdp'
                          ? cdpConnected === false
                            ? 'bg-red-900/40 border-red-600 text-red-400'
                            : cdpChecking || cdpConnected === null
                              ? 'bg-yellow-900/40 border-yellow-600 text-yellow-400'
                              : 'bg-green-900/40 border-green-600 text-green-400'
                          : browserMode === 'playwright'
                            ? playwrightServerStatus === 'not_found'
                              ? 'bg-red-900/40 border-red-600 text-red-400'
                              : 'bg-purple-900/40 border-purple-600 text-purple-400'
                            : browserMode === 'headless'
                              ? 'bg-blue-900/40 border-blue-600 text-blue-400'
                              : 'bg-gray-800 border-gray-600 text-gray-500'
                      } ${(isStreaming || isSummarizing) ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer hover:pr-2'}`}
                    >
                      <Globe className="w-4 h-4 flex-shrink-0" />
                      {browserMode !== 'none' ? (
                        <span className={`text-[10px] font-semibold px-1 rounded ${
                          browserMode === 'stealth'
                            ? camofoxConnected === false || camofoxServerStatus === 'not_found'
                              ? 'bg-red-800 text-red-200'
                              : camofoxStarting
                                ? 'bg-yellow-800 text-yellow-200'
                                : 'bg-orange-800 text-orange-200'
                            : browserMode === 'cdp'
                            ? cdpConnected === false
                              ? 'bg-red-800 text-red-200'
                              : cdpChecking || cdpConnected === null
                                ? 'bg-yellow-800 text-yellow-200'
                                : 'bg-green-800 text-green-200'
                            : browserMode === 'playwright'
                              ? playwrightServerStatus === 'not_found'
                                ? 'bg-red-800 text-red-200'
                                : 'bg-purple-800 text-purple-200'
                              : 'bg-blue-800 text-blue-200'
                        }`}>
                          {browserMode === 'stealth' ? 'Stealth' : browserMode === 'cdp' ? 'CDP' : browserMode === 'playwright' ? 'Playwright' : 'Headless'}
                        </span>
                      ) : (
                        <span className="text-xs font-medium max-w-0 overflow-hidden whitespace-nowrap group-hover:max-w-[60px] transition-all duration-200">
                          Browser
                        </span>
                      )}
                    </button>}

                    {/* Google Workspace Toggle — hidden in workflow mode */}
                    {!hideExtras && (
                    <button
                      type="button"
                      onClick={() => setShowGWSPopup(true)}
                      disabled={isStreaming || isSummarizing}
                      className={`group flex items-center gap-1 p-1.5 rounded-md border transition-all duration-200 ${
                        gwsEnabled
                          ? 'bg-blue-900/40 border-blue-600 text-blue-400'
                          : 'bg-gray-800 border-gray-600 text-gray-500'
                      } ${(isStreaming || isSummarizing) ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer hover:pr-2'}`}
                    >
                      <svg viewBox="0 0 24 24" className="w-4 h-4 flex-shrink-0" fill="none">
                        <path d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92c-.26 1.37-1.04 2.53-2.21 3.31v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.09z" fill="currentColor" opacity="0.9"/>
                        <path d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z" fill="currentColor" opacity="0.7"/>
                        <path d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l3.66-2.84z" fill="currentColor" opacity="0.8"/>
                        <path d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z" fill="currentColor" opacity="0.85"/>
                      </svg>
                      {gwsEnabled ? (
                        <span className="text-[10px] font-semibold px-1 rounded bg-white/10">GWS</span>
                      ) : (
                        <span className="text-xs font-medium max-w-0 overflow-hidden whitespace-nowrap group-hover:max-w-[40px] transition-all duration-200">
                          GWS
                        </span>
                      )}
                    </button>
                    )}

                    {/* Image Generation Toggle — hidden in workflow mode */}
                    {!hideExtras && <button
                      type="button"
                      onClick={() => {
                        if (!enableImageGeneration) {
                          setEnableImageGeneration(true)
                        }
                        setShowImageGenConfig(true)
                      }}
                      disabled={isStreaming || isSummarizing}
                      className={`group flex items-center gap-1 p-1.5 rounded-md border transition-all duration-200 ${
                        enableImageGeneration
                          ? 'bg-purple-900/40 border-purple-600 text-purple-400'
                          : 'bg-gray-800 border-gray-600 text-gray-500'
                      } ${(isStreaming || isSummarizing) ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer hover:pr-2'}`}
                    >
                      <ImagePlus className="w-4 h-4 flex-shrink-0" />
                      {enableImageGeneration ? (
                        <span className="text-[10px] font-semibold px-1 rounded bg-purple-800/60 text-purple-200">IMG</span>
                      ) : (
                        <span className="text-xs font-medium max-w-0 overflow-hidden whitespace-nowrap group-hover:max-w-[60px] transition-all duration-200">
                          Image Gen
                        </span>
                      )}
                    </button>}

                  </div>
                )}

                {/* Image Generation Config Modal */}
                {showImageGenConfig && (
                  <ImageGenerationConfigModal
                    onClose={() => setShowImageGenConfig(false)}
                    onDisable={() => setEnableImageGeneration(false)}
                  />
                )}

                {/* Browser Access Configuration Popup */}
                {showCdpPopup && (
                  <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm" onClick={() => { setShowCdpPopup(false); setWorkspaceMinimized(false) }}>
                    <div className="bg-gray-900 rounded-xl shadow-2xl border border-gray-700 w-[900px] max-w-[95vw]" onClick={(e) => e.stopPropagation()}>
                      {/* Header */}
                      <div className="flex items-center justify-between px-5 py-4 border-b border-gray-700">
                        <div className="flex items-center gap-2">
                          <Globe className="w-5 h-5 text-green-400" />
                          <h3 className="text-base font-semibold text-white">Browser Access</h3>
                        </div>
                        <button onClick={() => { setShowCdpPopup(false); setWorkspaceMinimized(false) }} className="text-gray-400 hover:text-gray-200 transition-colors">
                          <X className="w-5 h-5" />
                        </button>
                      </div>

                      {/* Content: 2-column layout */}
                      <div className="px-5 py-4 flex gap-4 items-stretch">

                        {/* Left: mode options */}
                        <div className="flex-1 space-y-2">
                          {/* Headless */}
                          <label className={`flex items-start gap-3 p-3 rounded-lg border cursor-pointer transition-colors ${
                            browserMode === 'headless'
                              ? 'border-blue-500 bg-blue-950/40'
                              : 'border-gray-700 hover:bg-gray-800'
                          }`}>
                            <input
                              type="radio"
                              name="browserMode"
                              checked={browserMode === 'headless'}
                              onChange={() => setBrowserMode('headless')}
                              className="mt-0.5 w-4 h-4 text-blue-500 accent-blue-500"
                            />
                            <div>
                              <div className="text-sm font-medium text-gray-100">Headless Browser</div>
                              <div className="text-xs text-gray-400 mt-0.5">
                                Uses{' '}
                                <a
                                  href="https://github.com/vercel/agent-browser"
                                  target="_blank"
                                  rel="noopener noreferrer"
                                  className="text-blue-400 hover:underline"
                                  onClick={(e) => e.stopPropagation()}
                                >
                                  agent-browser
                                </a>
                                {' '}by Vercel running inside a Docker container. No visible window.
                              </div>
                            </div>
                          </label>

                          {/* CDP */}
                          <label className={`flex items-start gap-3 p-3 rounded-lg border cursor-pointer transition-colors ${
                            browserMode === 'cdp'
                              ? 'border-green-500 bg-green-950/40'
                              : 'border-gray-700 hover:bg-gray-800'
                          }`}>
                            <input
                              type="radio"
                              name="browserMode"
                              checked={browserMode === 'cdp'}
                              onChange={() => setBrowserMode('cdp')}
                              className="mt-0.5 w-4 h-4 text-green-500 accent-green-500"
                            />
                            <div>
                              <div className="text-sm font-medium text-gray-100">Local Chrome (CDP)</div>
                              <div className="text-xs text-gray-400 mt-0.5">Connect to your real Chrome browser.</div>
                            </div>
                          </label>

                          {/* Playwright */}
                          <label className={`flex items-start gap-3 p-3 rounded-lg border transition-colors ${
                            playwrightServerStatus === 'not_found'
                              ? 'border-gray-700 opacity-50 cursor-not-allowed'
                              : browserMode === 'playwright'
                                ? 'border-purple-500 bg-purple-950/40 cursor-pointer'
                                : 'border-gray-700 hover:bg-gray-800 cursor-pointer'
                          }`}>
                            <input
                              type="radio"
                              name="browserMode"
                              checked={browserMode === 'playwright'}
                              onChange={() => setBrowserMode('playwright')}
                              disabled={playwrightServerStatus === 'not_found'}
                              className="mt-0.5 w-4 h-4 text-purple-500 accent-purple-500"
                            />
                            <div className="flex-1">
                              <div className="text-sm font-medium text-gray-100">Playwright MCP</div>
                              <div className="text-xs text-gray-400 mt-0.5">Opens a visible browser window via MCP server.</div>
                              {playwrightServerStatus === 'not_found' && (
                                <div className="text-xs text-red-400 mt-1 flex items-center gap-1">
                                  <span className="w-2 h-2 rounded-full bg-red-500 flex-shrink-0" />
                                  Server not found &mdash; add in MCP Settings
                                </div>
                              )}
                              {playwrightServerStatus === 'loading' && (
                                <div className="text-xs text-yellow-400 mt-1 flex items-center gap-1">
                                  <Loader2 className="w-3 h-3 animate-spin flex-shrink-0" />
                                  Discovering...
                                </div>
                              )}
                              {playwrightServerStatus === 'error' && (
                                <div className="text-xs text-amber-400 mt-1 flex items-center gap-1">
                                  <span className="w-2 h-2 rounded-full bg-amber-500 flex-shrink-0" />
                                  Server has errors &mdash; check MCP Settings
                                </div>
                              )}
                            </div>
                          </label>

                          {/* Stealth */}
                          <label className={`flex items-start gap-3 p-3 rounded-lg border transition-colors ${
                            camofoxServerStatus === 'not_found'
                              ? 'border-gray-700 opacity-50 cursor-not-allowed'
                              : browserMode === 'stealth'
                                ? 'border-orange-500 bg-orange-950/40 cursor-pointer'
                                : 'border-gray-700 hover:bg-gray-800 cursor-pointer'
                          }`}>
                            <input
                              type="radio"
                              name="browserMode"
                              checked={browserMode === 'stealth'}
                              onChange={() => setBrowserMode('stealth')}
                              disabled={camofoxServerStatus === 'not_found'}
                              className="mt-0.5 w-4 h-4 text-orange-500 accent-orange-500"
                            />
                            <div className="flex-1">
                              <div className="text-sm font-medium text-gray-100">Stealth Browser (Camofox)</div>
                              <div className="text-xs text-gray-400 mt-0.5">Anti-detect Firefox for bot-resistant sites.</div>
                              {camofoxServerStatus === 'not_found' && (
                                <div className="text-xs text-red-400 mt-1 flex items-center gap-1">
                                  <span className="w-2 h-2 rounded-full bg-red-500 flex-shrink-0" />
                                  Server not found &mdash; add in MCP Settings
                                </div>
                              )}
                              {camofoxServerStatus === 'loading' && (
                                <div className="text-xs text-yellow-400 mt-1 flex items-center gap-1">
                                  <Loader2 className="w-3 h-3 animate-spin flex-shrink-0" />
                                  Discovering...
                                </div>
                              )}
                              {camofoxServerStatus === 'error' && (
                                <div className="text-xs text-amber-400 mt-1 flex items-center gap-1">
                                  <span className="w-2 h-2 rounded-full bg-amber-500 flex-shrink-0" />
                                  Server has errors &mdash; check MCP Settings
                                </div>
                              )}
                              {browserMode === 'stealth' && camofoxStarting && (
                                <div className="text-xs text-yellow-400 mt-1 flex items-center gap-1">
                                  <Loader2 className="w-3 h-3 animate-spin flex-shrink-0" />
                                  Starting camofox-browser...
                                </div>
                              )}
                              {browserMode === 'stealth' && !camofoxStarting && camofoxConnected === true && (
                                <div className="text-xs text-green-400 mt-1 flex items-center gap-1">
                                  <span className="w-2 h-2 rounded-full bg-green-500 flex-shrink-0" />
                                  camofox-browser connected
                                </div>
                              )}
                              {browserMode === 'stealth' && !camofoxStarting && camofoxConnected === false && (
                                <div className="text-xs text-red-400 mt-1 flex items-center gap-1">
                                  <span className="w-2 h-2 rounded-full bg-red-500 flex-shrink-0" />
                                  Failed to start &mdash; check npm packages
                                </div>
                              )}
                            </div>
                          </label>
                        </div>

                        {/* Right: context panel */}
                        <div className="w-80 flex-shrink-0 rounded-lg bg-gray-800/60 border border-gray-700 p-3 flex flex-col gap-3">
                          {browserMode === 'cdp' && (<>
                            <div className="space-y-2">
                              <div className="flex items-center gap-2">
                                <label className="text-xs text-gray-400 whitespace-nowrap">Port:</label>
                                <input
                                  type="number"
                                  value={cdpPort}
                                  onChange={(e) => setCdpPort(parseInt(e.target.value) || 9222)}
                                  className="w-20 px-2 py-1 text-sm border border-gray-600 rounded-md bg-gray-800 text-white focus:border-green-500 focus:outline-none"
                                  min={1}
                                  max={65535}
                                />
                              </div>
                              <button
                                type="button"
                                onClick={() => checkCdpConnection(cdpPort)}
                                disabled={cdpChecking}
                                className="w-full px-3 py-1.5 text-xs font-medium bg-gray-700 hover:bg-gray-600 rounded-md text-gray-200 disabled:opacity-50 transition-colors"
                              >
                                {cdpChecking ? 'Checking...' : 'Check Connection'}
                              </button>
                              <div className="flex items-start gap-1.5">
                                {cdpChecking ? (
                                  <>
                                    <div className="w-2.5 h-2.5 rounded-full bg-yellow-400 animate-pulse mt-0.5 flex-shrink-0" />
                                    <span className="text-xs text-yellow-400">Checking port {cdpPort}...</span>
                                  </>
                                ) : cdpConnected === true ? (
                                  <>
                                    <div className="w-2.5 h-2.5 rounded-full bg-green-500 mt-0.5 flex-shrink-0" />
                                    <span className="text-xs text-green-400">Connected on port {cdpPort}.</span>
                                  </>
                                ) : cdpConnected === false ? (
                                  <>
                                    <div className="w-2.5 h-2.5 rounded-full bg-red-500 mt-0.5 flex-shrink-0" />
                                    <span className="text-xs text-red-400">Not reachable on port {cdpPort}.</span>
                                  </>
                                ) : (
                                  <span className="text-xs text-gray-500">Click Check Connection to verify.</span>
                                )}
                              </div>
                            </div>
                            <div className="border-t border-gray-700 pt-3 space-y-1.5">
                              <p className="text-xs font-medium text-gray-300">Launch Chrome with CDP</p>
                              {navigator.platform?.includes('Mac') && (
                                <div className="space-y-1">
                                  <a
                                    href={`${getApiBaseUrl()}/api/downloads/chrome-cdp-macOS.zip`}
                                    download="Chrome-CDP-macOS.zip"
                                    target="_blank"
                                    rel="noopener noreferrer"
                                    onClick={(e) => e.stopPropagation()}
                                    className="inline-flex items-center gap-1.5 px-2 py-1 text-xs font-medium bg-green-700 hover:bg-green-600 text-white rounded-md transition-colors"
                                  >
                                    <Download className="w-3 h-3" />
                                    Download Chrome CDP.app (macOS)
                                  </a>
                                  <ol className="text-xs text-gray-500 list-decimal list-inside space-y-0.5">
                                    <li>Double-click the zip to unzip.</li>
                                    <li>Drag <strong className="text-gray-300">Chrome CDP.app</strong> to your <strong className="text-gray-300">Applications</strong> folder.</li>
                                    <li>Open it from Spotlight (⌘+Space) or Launchpad.</li>
                                  </ol>
                                  <p className="text-xs text-gray-500">If macOS says &quot;damaged&quot;, run in Terminal:</p>
                                  <code className="block bg-gray-950 px-2 py-1 rounded text-[10px] font-mono text-amber-400 border border-gray-700">
                                    xattr -c /Applications/Chrome\ CDP.app
                                  </code>
                                  <p className="text-xs text-gray-600">then open it again, or right-click → Open.</p>
                                </div>
                              )}
                              <p className="text-xs text-gray-500">Or run in Terminal (close all Chrome windows first):</p>
                              <code className="block bg-gray-950 px-2 py-1.5 rounded text-[10px] font-mono break-all text-green-400 border border-gray-700">
                                {navigator.platform?.includes('Mac')
                                  ? `/Applications/Google\\ Chrome.app/Contents/MacOS/Google\\ Chrome --remote-debugging-port=${cdpPort}`
                                  : `google-chrome --remote-debugging-port=${cdpPort}`}
                              </code>
                            </div>
                          </>)}

                          {browserMode === 'stealth' && (
                            <div className="space-y-2.5">
                              <div className="space-y-1.5">
                                <p className="text-xs font-medium text-gray-300">Stealth Browser (Camofox)</p>
                                <p className="text-xs text-gray-400">Anti-detect Firefox that bypasses bot-detection on sites like LinkedIn, Amazon, and others.</p>
                                <p className="text-xs text-gray-500">Features: randomised fingerprints, persistent sessions, stealth headers, and optional headed mode.</p>
                                <p className="text-xs text-gray-500">Requires the <code className="bg-gray-950 px-1 rounded text-[10px]">camofox</code> MCP server in MCP Settings.</p>
                              </div>
                              <label className="flex items-start gap-2.5 cursor-pointer">
                                <input
                                  type="checkbox"
                                  checked={camofoxHeaded}
                                  onChange={(e) => {
                                    if (activeTabId) {
                                      setTabConfig(activeTabId, { camofoxHeaded: e.target.checked })
                                    }
                                  }}
                                  className="mt-0.5 w-4 h-4 rounded accent-orange-500"
                                />
                                <div>
                                  <div className="text-sm text-gray-200">Show browser window</div>
                                  <div className="text-xs text-gray-500 mt-0.5">
                                    {camofoxHeaded
                                      ? 'Visible Firefox — watch agent navigate in real-time'
                                      : 'Headless mode — browser runs in background'}
                                  </div>
                                </div>
                              </label>
                            </div>
                          )}

                          {browserMode === 'headless' && (
                            <div className="space-y-2">
                              <p className="text-xs font-medium text-gray-300">Headless Browser</p>
                              <p className="text-xs text-gray-400">
                                Powered by{' '}
                                <a
                                  href="https://github.com/vercel/agent-browser"
                                  target="_blank"
                                  rel="noopener noreferrer"
                                  className="text-blue-400 hover:underline"
                                >
                                  agent-browser
                                </a>
                                {' '}by Vercel, running inside a Docker container.
                              </p>
                              <p className="text-xs text-gray-500">No visible window — the agent navigates Chromium in the background.</p>
                            </div>
                          )}

                          {browserMode === 'playwright' && (
                            <div className="space-y-2">
                              <p className="text-xs font-medium text-gray-300">Playwright MCP</p>
                              <p className="text-xs text-gray-400">
                                Uses the official{' '}
                                <a
                                  href="https://github.com/microsoft/playwright-mcp"
                                  target="_blank"
                                  rel="noopener noreferrer"
                                  className="text-purple-400 hover:underline"
                                >
                                  microsoft/playwright-mcp
                                </a>
                                {' '}server.
                              </p>
                              <p className="text-xs text-gray-500">Each session opens a <strong className="text-gray-300">new Chrome instance</strong> — existing browser windows are not reused.</p>
                              <p className="text-xs text-gray-500">Requires the <code className="bg-gray-950 px-1 rounded text-[10px]">playwright</code> MCP server to be configured in MCP Settings.</p>
                            </div>
                          )}

                          {browserMode === 'none' && (
                            <p className="text-xs text-gray-500">Select a mode to see configuration options.</p>
                          )}
                        </div>
                      </div>

                      {/* Footer */}
                      <div className="flex justify-end gap-2 px-5 py-3 border-t border-gray-700">
                        <button
                          type="button"
                          onClick={() => {
                            setBrowserMode('none')
                            setShowCdpPopup(false)
                            setWorkspaceMinimized(false)
                          }}
                          className="px-4 py-2 text-sm text-gray-300 hover:bg-gray-800 rounded-md transition-colors"
                        >
                          Disable Browser
                        </button>
                        <button
                          type="button"
                          onClick={() => { setShowCdpPopup(false); setWorkspaceMinimized(false) }}
                          disabled={browserMode === 'cdp' && cdpConnected !== true}
                          className="px-4 py-2 text-sm font-medium bg-green-600 hover:bg-green-500 text-white rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                        >
                          {browserMode === 'cdp' && cdpConnected !== true ? (cdpChecking ? 'Checking...' : 'Connect Chrome First') : 'Done'}
                        </button>
                      </div>
                    </div>
                  </div>
                )}

                {/* Google Workspace Popup */}
                {showGWSPopup && (
                  <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm" onClick={() => setShowGWSPopup(false)}>
                    <div className="bg-gray-900 rounded-xl shadow-2xl border border-gray-700 w-[480px] max-w-[90vw]" onClick={(e) => e.stopPropagation()}>
                      <div className="flex items-center justify-between px-5 py-4 border-b border-gray-700">
                        <div className="flex items-center gap-2">
                          <svg viewBox="0 0 24 24" className="w-5 h-5" fill="none">
                            <path d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92c-.26 1.37-1.04 2.53-2.21 3.31v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.09z" fill="#4285F4"/>
                            <path d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z" fill="#34A853"/>
                            <path d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l3.66-2.84z" fill="#FBBC05"/>
                            <path d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z" fill="#EA4335"/>
                          </svg>
                          <h3 className="text-base font-semibold text-white">Google Workspace</h3>
                          <a
                            href="https://github.com/googleworkspace/cli"
                            target="_blank"
                            rel="noopener noreferrer"
                            className="text-xs text-blue-400 hover:text-blue-300 transition-colors"
                          >
                            @googleworkspace/cli ↗
                          </a>
                        </div>
                        <button onClick={() => setShowGWSPopup(false)} className="text-gray-400 hover:text-gray-200 transition-colors">
                          <X className="w-5 h-5" />
                        </button>
                      </div>

                      <div className="px-5 py-4 space-y-4">
                        {/* Enable/disable toggle */}
                        <div className="flex items-center justify-between">
                          <div>
                            <div className="text-sm font-medium text-gray-100">Google Workspace</div>
                            <div className="text-xs text-gray-400 mt-0.5">Drive · Gmail · Calendar · Docs · Sheets · Slides</div>
                          </div>
                          <label className={`relative inline-flex items-center ${gwsChatAuthStatus?.token_valid === false || (!gwsEnabled && !gwsChatAuthStatus?.configured) ? 'cursor-not-allowed opacity-40' : 'cursor-pointer'}`}>
                            <input
                              type="checkbox"
                              checked={gwsEnabled}
                              onChange={toggleGWSServer}
                              disabled={gwsChatAuthStatus?.token_valid === false || (!gwsEnabled && !gwsChatAuthStatus?.configured)}
                              className="sr-only peer"
                            />
                            <div className="w-11 h-6 bg-gray-600 peer-focus:outline-none rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all peer-checked:bg-blue-600"></div>
                          </label>
                        </div>

                        {/* Auth gate hint */}
                        {gwsChatAuthStatus?.token_valid === false ? (
                          <p className="text-xs text-amber-400">
                            Token invalid — run <code className="text-amber-300">gws auth login</code>, then <code className="text-amber-300">gws auth export --unmasked &gt; agent_go/gws-credentials.json</code> and restart docker compose
                          </p>
                        ) : !gwsEnabled && !gwsChatAuthStatus?.configured && (
                          <p className="text-xs text-amber-400">
                            {gwsChatChecking ? 'Checking auth...' : 'Auth check required before enabling'}
                          </p>
                        )}

                        {/* Auth status */}
                        <div className="space-y-2">
                          <div className="flex items-center gap-2">
                            <button
                              type="button"
                              onClick={checkGWSChatAuth}
                              disabled={gwsChatChecking}
                              className="px-3 py-1.5 text-xs bg-gray-700 hover:bg-gray-600 rounded text-gray-200 disabled:opacity-50 transition-colors"
                            >
                              {gwsChatChecking ? 'Checking...' : 'Check Auth Status'}
                            </button>
                            {gwsChatAuthStatus && (
                              gwsChatAuthStatus.configured && gwsChatAuthStatus.token_valid !== false ? (
                                <div className="flex items-center gap-1.5">
                                  <div className="w-2 h-2 rounded-full bg-green-500 flex-shrink-0" />
                                  <span className="text-xs text-green-400">
                                    Auth OK · {gwsChatAuthStatus.enabled_api_count ?? 0} APIs
                                    {gwsChatAuthStatus.auth_method ? ` (${gwsChatAuthStatus.auth_method})` : ''}
                                  </span>
                                </div>
                              ) : gwsChatAuthStatus.configured && gwsChatAuthStatus.token_valid === false ? (
                                <div className="flex items-center gap-1.5">
                                  <div className="w-2 h-2 rounded-full bg-amber-500 flex-shrink-0" />
                                  <span className="text-xs text-amber-400">Token invalid</span>
                                </div>
                              ) : (
                                <div className="flex items-center gap-1.5">
                                  <div className="w-2 h-2 rounded-full bg-red-500 flex-shrink-0" />
                                  <span className="text-xs text-red-400">
                                    {gwsChatAuthStatus.token_valid === false
                                      ? `Token invalid — run gws auth login${gwsChatAuthStatus.token_error ? ` (${gwsChatAuthStatus.token_error})` : ''}`
                                      : (gwsChatAuthStatus.error ?? 'Not configured')}
                                  </span>
                                </div>
                              )
                            )}
                          </div>
                          {!gwsChatAuthStatus && (
                            <p className="text-xs text-gray-500">Run <code className="text-gray-400">gws auth login</code>, then <code className="text-gray-400">gws auth export --unmasked &gt; agent_go/gws-credentials.json</code> and restart docker compose</p>
                          )}
                        </div>

                        {/* Skills */}
                        <div className="border-t border-gray-700 pt-4 space-y-2">
                          <div className="flex items-center justify-between">
                            <div>
                              <div className="text-sm font-medium text-gray-100">Skills</div>
                              <div className="text-xs text-gray-400 mt-0.5">
                                Sync all gws-* skills from{' '}
                                <a
                                  href="https://github.com/googleworkspace/cli/tree/main/skills"
                                  target="_blank"
                                  rel="noopener noreferrer"
                                  className="text-blue-400 hover:text-blue-300 underline underline-offset-2"
                                >
                                  github.com/googleworkspace/cli
                                </a>
                              </div>
                            </div>
                            <button
                              type="button"
                              onClick={syncGWSChatSkills}
                              disabled={gwsChatSyncing}
                              className="ml-3 flex-shrink-0 px-3 py-1.5 text-xs bg-blue-700 hover:bg-blue-600 disabled:opacity-50 text-white rounded transition-colors"
                            >
                              {gwsChatSyncing ? 'Syncing...' : 'Sync from GitHub'}
                            </button>
                          </div>
                          {gwsChatSyncResult && (
                            gwsChatSyncResult.error ? (
                              <p className="text-xs text-red-400">{gwsChatSyncResult.error}</p>
                            ) : (
                              <p className="text-xs text-green-400">
                                Synced {gwsChatSyncResult.synced} skill{gwsChatSyncResult.synced !== 1 ? 's' : ''}
                                {gwsChatSyncResult.failed?.length ? ` · ${gwsChatSyncResult.failed.length} failed` : ''}
                              </p>
                            )
                          )}
                        </div>
                      </div>

                      <div className="flex justify-end gap-2 px-5 py-3 border-t border-gray-700">
                        <button
                          type="button"
                          onClick={() => { if (gwsEnabled) toggleGWSServer(); setShowGWSPopup(false) }}
                          className="px-4 py-2 text-sm text-gray-300 hover:bg-gray-800 rounded-md transition-colors"
                        >
                          Disable GWS
                        </button>
                        <button
                          type="button"
                          onClick={() => setShowGWSPopup(false)}
                          className="px-4 py-2 text-sm font-medium bg-blue-600 hover:bg-blue-500 text-white rounded-md transition-colors"
                        >
                          Done
                        </button>
                      </div>
                    </div>
                  </div>
                )}

                {/* Reasoning Level Popup */}
                {showReasoningPopup && (
                  <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm" onClick={() => setShowReasoningPopup(false)}>
                    <div className="bg-gray-900 rounded-xl shadow-2xl border border-gray-700 w-[320px] max-w-[90vw]" onClick={(e) => e.stopPropagation()}>
                      <div className="flex items-center justify-between px-5 py-4 border-b border-gray-700">
                        <div className="flex items-center gap-2">
                          <Bot className="w-5 h-5 text-blue-400" />
                          <h3 className="text-base font-semibold text-white">Reasoning Level</h3>
                        </div>
                        <button onClick={() => setShowReasoningPopup(false)} className="text-gray-400 hover:text-gray-200 transition-colors">
                          <X className="w-5 h-5" />
                        </button>
                      </div>
                      <div className="px-5 py-4 space-y-2">
                        <p className="text-xs text-gray-400 mb-3">Sets the default reasoning effort for delegated sub-agent tasks.</p>
                        {([
                          { level: 'high',   label: 'High',   desc: 'Deep thinking — complex reasoning, research, planning',   activeClass: 'border-orange-500 bg-orange-950/40', dotClass: 'bg-orange-500' },
                          { level: 'medium', label: 'Medium', desc: 'Balanced — good for most tasks',                          activeClass: 'border-yellow-500 bg-yellow-950/40', dotClass: 'bg-yellow-400' },
                          { level: 'low',    label: 'Low',    desc: 'Fast — simple lookups, straightforward actions',          activeClass: 'border-green-500 bg-green-950/40',  dotClass: 'bg-green-500'  },
                        ] as const).map(({ level, label, desc, activeClass, dotClass }) => (
                          <label key={level} className={`flex items-start gap-3 p-3 rounded-lg border cursor-pointer transition-colors ${
                            defaultReasoningLevel === level ? activeClass : 'border-gray-700 hover:bg-gray-800'
                          }`}>
                            <input
                              type="radio"
                              name="reasoningLevel"
                              checked={defaultReasoningLevel === level}
                              onChange={() => setDefaultReasoningLevel(level)}
                              className="sr-only"
                            />
                            <div className={`w-3 h-3 rounded-full mt-0.5 flex-shrink-0 ${defaultReasoningLevel === level ? dotClass : 'bg-gray-600'}`} />
                            <div>
                              <div className="text-sm font-medium text-gray-100">{label}</div>
                              <div className="text-xs text-gray-400 mt-0.5">{desc}</div>
                            </div>
                          </label>
                        ))}
                      </div>
                      <div className="flex justify-between gap-2 px-5 py-3 border-t border-gray-700">
                        <button
                          type="button"
                          onClick={() => { setDefaultReasoningLevel(null); setShowReasoningPopup(false) }}
                          className="px-4 py-2 text-sm text-gray-300 hover:bg-gray-800 rounded-md transition-colors"
                        >
                          Clear (Auto)
                        </button>
                        <button
                          type="button"
                          onClick={() => setShowReasoningPopup(false)}
                          className="px-4 py-2 text-sm font-medium bg-blue-600 hover:bg-blue-500 text-white rounded-md transition-colors"
                        >
                          Done
                        </button>
                      </div>
                    </div>
                  </div>
                )}

                {/* Secrets dropdown - hidden for workflow mode */}
                {!hideExtras && (
                <SecretSelectionDropdown
                  selectedSecrets={selectedSecrets}
                  onSecretToggle={onSecretToggle}
                  onSelectAll={onSelectAllSecrets}
                  onClearAll={onClearAllSecrets}
                  disabled={isStreaming || isSummarizing}
                />
                )}

                {/* Status text - removed observer initialization message */}
              </div>
              {/* Show old buttons */}
              {(
                <div className="flex items-center gap-2">
                  {/* Active agents indicator — left of context circle */}
                  {activeAgents.length > 0 && (
                    <div className="relative">
                      <button
                        type="button"
                        onClick={() => setShowActiveAgentsPanel(prev => !prev)}
                        className="flex items-center gap-1.5 px-2 py-1 rounded-md bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 hover:bg-blue-100 dark:hover:bg-blue-900/40 transition-colors"
                      >
                        <Loader2 className="w-3 h-3 animate-spin text-blue-500 dark:text-blue-400 flex-shrink-0" />
                        <span className="text-[11px] text-blue-600 dark:text-blue-400 font-medium">
                          {activeAgents.length}
                        </span>
                      </button>
                      {showActiveAgentsPanel && (
                        <>
                          <div className="fixed inset-0 z-40" onClick={() => setShowActiveAgentsPanel(false)} />
                          <div className="absolute bottom-full right-0 mb-2 z-50 w-72 bg-white dark:bg-gray-800 rounded-lg shadow-lg border border-gray-200 dark:border-gray-700 overflow-hidden">
                            <div className="px-3 py-2 border-b border-gray-200 dark:border-gray-700 flex items-center justify-between">
                              <span className="text-xs font-semibold text-gray-700 dark:text-gray-300">
                                {activeAgents.length === 1 ? '1 agent running' : `${activeAgents.length} agents running`}
                              </span>
                              <button type="button" onClick={() => setShowActiveAgentsPanel(false)} className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-200">
                                <X className="w-3.5 h-3.5" />
                              </button>
                            </div>
                            <div className="max-h-48 overflow-y-auto">
                              {activeAgents.map((a, i) => (
                                <div
                                  key={i}
                                  className="flex items-center gap-1.5 py-1.5 border-b last:border-b-0 border-gray-100 dark:border-gray-700/50 pr-3"
                                  style={{ paddingLeft: `${8 + a.depth * 14}px` }}
                                >
                                  {a.depth > 0 && (
                                    <span className="text-gray-300 dark:text-gray-600 flex-shrink-0 text-[10px] leading-none">└─</span>
                                  )}
                                  <Loader2 className="w-3 h-3 animate-spin text-blue-500 dark:text-blue-400 flex-shrink-0" />
                                  <span className="text-xs text-gray-700 dark:text-gray-300 truncate">{a.name}</span>
                                  <span className="text-[10px] text-gray-400 dark:text-gray-500 flex-shrink-0 ml-auto">
                                    {a.type === 'delegation' ? 'sub-agent' : a.depth === 0 ? 'step' : 'agent'}
                                  </span>
                                </div>
                              ))}
                            </div>
                          </div>
                        </>
                      )}
                    </div>
                  )}

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
                      {/* Workshop mode toggle: Build / Optimize / Run */}
                      {workflowPhaseId === 'workflow-builder' && !isOrganizationContext && (
                        <WorkshopModeToggle />
                      )}
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <Button
                            type="button"
                            variant="outline"
                            size="sm"
                            disabled={isStreaming || isSummarizing || isUploadingFiles}
                            onClick={() => {
                              const inputEl = fileUploadInputRef.current
                              if (!inputEl) {
                                console.error('[CHAT_UPLOAD] upload input ref not available')
                                addToast('Upload input not ready. Please retry.', 'error')
                                return
                              }
                              console.info('[CHAT_UPLOAD] opening file picker')
                              inputEl.click()
                            }}
                            className="px-2.5"
                            data-testid="chat-upload-button"
                          >
                            {isUploadingFiles ? (
                              <Loader2 className="w-4 h-4 animate-spin" />
                            ) : (
                              <Paperclip className="w-4 h-4" />
                            )}
                          </Button>
                        </TooltipTrigger>
                        <TooltipContent>
                          <p>{isUploadingFiles ? 'Uploading files...' : `Upload file(s) to ${uploadTargetFolder}`}</p>
                        </TooltipContent>
                      </Tooltip>
                      {isStreaming ? (
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <Button
                              type="button"
                              variant="destructive"
                              onClick={() => {
                                justStoppedStreamingRef.current = true
                                setTimeout(() => { justStoppedStreamingRef.current = false }, 300)
                                onStopStreaming()
                              }}
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
                                  : isCdpDisconnected
                                    ? 'Chrome CDP not reachable. Check connection.'
                                    : isPlaywrightMissing
                                      ? 'Playwright MCP server not found. Add it in MCP Settings.'
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
        <input
          ref={fileUploadInputRef}
          type="file"
          multiple
          onChange={handleUploadFilesSelected}
          className="hidden"
          disabled={isStreaming || isSummarizing || isUploadingFiles}
        />
      </div>
      
      {/* Command Selection Dialog */}
      <CommandSelectionDialog
        isOpen={showCommandDialog}
        onClose={handleCommandDialogClose}
        onSelectCommand={handleCommandSelect}
        searchQuery={commandSearchQuery}
        position={commandDialogPosition}
        modeCategory={selectedModeCategory}
        onManageCommands={handleManageCommands}
        onEditCommand={handleEditCommand}
        onDeleteCommand={handleDeleteCommand}
      />

      {/* Command Editor Dialog */}
      <CommandEditorDialog
        isOpen={showCommandEditor}
        onClose={handleCommandEditorClose}
        editingCommand={editingUserCommand}
      />

      {/* File Selection Dialog */}
      <FileSelectionDialog
        isOpen={showFileDialog}
        onClose={handleFileDialogClose}
        onSelectFile={handleFileSelect}
        onNavigateIntoFolder={handleNavigateIntoFolder}
        searchQuery={fileSearchQuery}
        position={fileDialogPosition}
        extraFiles={extraAtFiles}
      />

      {/* Workflow Selection Dialog */}
      <WorkflowSelectionDialog
        isOpen={showWorkflowDialog}
        onClose={handleWorkflowDialogClose}
        onSelectWorkflow={handleWorkflowSelect}
        searchQuery={workflowSearchQuery}
        position={workflowDialogPosition}
      />

      {/* Inline Skill Selection Popup */}
      <InlineSelectionPopup
        isOpen={showSkillPopup}
        onClose={handleSkillPopupClose}
        onToggleItem={handleSkillPopupToggle}
        items={skillPopupItems}
        searchQuery={skillPopupSearchQuery}
        position={skillPopupPosition}
        title="Skills"
        icon={<Wand2 className="w-4 h-4 text-muted-foreground" />}
        emptyMessage="No skills available"
        isLoading={skillsLoading}
      />

      {/* Inline Server Selection Popup */}
      <InlineSelectionPopup
        isOpen={showServerPopup}
        onClose={handleServerPopupClose}
        onToggleItem={handleServerPopupToggle}
        items={serverPopupItems}
        searchQuery={serverPopupSearchQuery}
        position={serverPopupPosition}
        title="MCP Servers"
        icon={<Server className="w-4 h-4 text-muted-foreground" />}
        emptyMessage="No MCP servers available"
      />

      {/* Inline Sub-Agent Selection Popup */}
      <InlineSelectionPopup
        isOpen={showSubAgentPopup}
        onClose={handleSubAgentPopupClose}
        onToggleItem={handleSubAgentPopupToggle}
        items={subAgentPopupItems}
        searchQuery={subAgentPopupSearchQuery}
        position={subAgentPopupPosition}
        title="Sub-Agents"
        icon={<Bot className="w-4 h-4 text-muted-foreground" />}
        emptyMessage="No sub-agent templates available"
        isLoading={subAgentsLoading}
      />

      {/* Slash command dialogs */}
      {showSkillImport && (
        <SkillImportDialog
          onClose={() => closeDialog('skillImport')}
          onSuccess={() => closeDialog('skillImport')}
        />
      )}

      {showSubAgentImport && (
        <SubAgentImportDialog
          onClose={() => closeDialog('subAgentImport')}
          onSuccess={() => closeDialog('subAgentImport')}
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
      {showWorkflowBuilder && (
        <WorkflowBuilderModal onClose={() => closeDialog('workflowBuilder')} />
      )}
      </div>
    </TooltipProvider>
  )
}

ChatInputComponent.displayName = 'ChatInput'

export const ChatInput = React.memo(ChatInputComponent)
