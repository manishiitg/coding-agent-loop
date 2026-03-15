import { useEffect, useRef, useCallback, forwardRef, useImperativeHandle, useMemo, useState, type ForwardedRef } from 'react'
import { useRenderLogger, useMemoLogger } from '../utils/renderLogger'
import { useShallow } from 'zustand/react/shallow'
import { agentApi, resetSessionId, getSessionId } from '../services/api'
import type { PollingEvent, ExtendedLLMConfiguration, SSEEventMessage, SSEStatusMessage, ExecutionOptions } from '../services/api-types'
import type { AgentMode } from '../stores/types'
import { ChatInput } from './ChatInput'
import { EventDisplay } from './EventDisplay'
import { WorkflowModeHandler, type WorkflowModeHandlerRef, signalPlanModified } from './workflow'
import { ToastContainer } from './ui/Toast'
import { useWorkspaceStore } from '../stores/useWorkspaceStore'
import { useWorkflowStore } from '../stores/useWorkflowStore'
import { WorkflowExplanation } from './WorkflowExplanation'
import { useAppStore, useLLMStore, useMCPStore, useChatStore, useGlobalPresetStore } from '../stores'
import { useModeStore, type ModeCategory } from '../stores/useModeStore'
import { ModeEmptyState } from './ModeEmptyState'
import { PresetSelectionOverlay } from './PresetSelectionOverlay'
import { ModeSwitchDialog } from './ui/ModeSwitchDialog'
import type { ChatTab } from '../stores/useChatStore'
import type { CustomPreset } from '../types/preset'
import { WORKSPACE_TOOLS } from '../utils/customToolNames'
import { restoreSession } from '../utils/sessionRestore'
import { logger } from '../utils/logger'
import { secretsApi } from '../api/secrets'
import { useSecretsStore } from '../stores'
import {
  determineModeFlag,
  buildLLMConfigWithApiKeys,
  buildQueryRequestPayload,
  resolveOrCreateTab,
  createUserMessageEvent,
  validateExecutionGroups,
  isChatCompatiblePhase,
} from '../utils/chatSubmitHelpers'

// Stable empty array to avoid infinite re-render loops in Zustand selectors
// (a new [] on every selector call breaks referential equality checks)
const EMPTY_EVENTS: PollingEvent[] = []

const STEP_TYPES = [
  { name: 'Regular', desc: 'LLM agent executes instructions and writes output files' },
  { name: 'Conditional', desc: 'Evaluates a condition, then runs if_true or if_false branch steps' },
  { name: 'Decision', desc: 'Executes then evaluates output to route to different next steps' },
  { name: 'Routing', desc: 'Multi-way conditional — evaluates a question to pick one of several routes' },
  { name: 'Todo Task', desc: 'Dynamic task list with sub-agents delegated per task' },
  { name: 'Human Input', desc: 'Collects user input (text, yes/no, or multiple choice)' },
]

const PHASE_CHAT_INFO: Record<string, {
  title: string
  description: string
  capabilities: string[]
  limitations: string[]
  showStepTypes?: boolean
}> = {
  'planning': {
    title: 'Planning Agent',
    description: 'Chat with the planning agent to create and refine your execution plan.',
    capabilities: [
      'View and discuss the current plan',
      'Add, update, or remove plan steps',
      'Reorganize step order and dependencies',
      'Refine objectives and requirements',
    ],
    limitations: [
      'Cannot execute the plan — use the Execution phase for that',
      'Cannot read execution logs or results from previous runs',
      'Cannot modify evaluation plans or learnings',
      'Canvas won\'t auto-refresh — re-open the tab to see plan changes',
    ],
    showStepTypes: true,
  },
  'execution-qa': {
    title: 'Execution Q&A',
    description: 'Ask questions about execution results, logs, and plan state. Read-only.',
    capabilities: [
      'Answer questions about what happened during execution',
      'Read execution logs, validation reports, and outputs',
      'Analyze step failures and identify root causes',
      'Explain plan structure and step dependencies',
    ],
    limitations: [
      'Read-only — cannot modify the plan, learnings, or any files',
      'Cannot execute or re-run steps',
      'Cannot modify evaluation plans',
      'No plan modification tools available',
    ],
  },
  'evaluation-builder': {
    title: 'Evaluation Builder',
    description: 'Design and refine evaluation plans, analyze results.',
    capabilities: [
      'Create new evaluation steps from the execution plan',
      'Review evaluation scores and reasoning from past runs',
      'Identify low-scoring steps and suggest improvements',
      'Update, add, or remove evaluation steps',
      'Read execution outputs for context',
    ],
    limitations: [
      'Cannot modify the execution plan (use Workflow Builder)',
      'Cannot execute evaluations — use Evaluation Execution phase',
      'Cannot modify learnings or knowledgebase files',
    ],
  },
  'workflow-builder': {
    title: 'Workflow Builder',
    description: 'Execute steps in the background, debug results, update the plan, generate learnings, and tweak configs — all in one free-flow session.',
    capabilities: [
      'Run any plan step in the background and poll for results',
      'Cancel a running step mid-execution',
      'Update plan steps (add, edit, reorder, delete)',
      'Update step_config.json — servers, tools, disable learning',
      'Generate/update learnings with optional human guidance',
      'View the system prompt and conversation from a past run',
      'Run shell commands for investigation',
    ],
    limitations: [
      'Steps run one at a time per execute_step call',
      'System prompts only available for runs after this feature was added',
      'Cannot modify evaluation plans',
    ],
  },
  'human-assisted-execution': {
    title: 'Human Assisted Execution',
    description: 'Run workflow steps interactively — choose which steps to run, monitor progress, and review results.',
    capabilities: [
      'Run any plan step in the background and poll for results',
      'Cancel a running step mid-execution',
      'Choose which steps to run and in what order',
      'View step results and debug failures',
      'Run shell commands for investigation',
    ],
    limitations: [
      'Cannot modify the plan or step configs',
      'No optimization or learning management',
      'Steps run one at a time per execute_step call',
    ],
  },
}

function PhaseChatEmptyState({ phaseId }: { phaseId: string }) {
  const info = PHASE_CHAT_INFO[phaseId]
  if (!info) return null

  return (
    <div className="flex flex-col items-center justify-center h-full p-8 text-center overflow-y-auto">
      <div className="mb-4 w-10 h-10 rounded-full bg-blue-100 dark:bg-blue-900/30 flex items-center justify-center">
        <span className="text-blue-600 dark:text-blue-400 text-lg">💬</span>
      </div>
      <h3 className="text-xl font-bold text-gray-900 dark:text-white mb-2">
        {info.title}
      </h3>
      <p className="text-sm text-gray-600 dark:text-gray-400 mb-6 max-w-sm">
        {info.description}
      </p>
      <div className="w-full max-w-md text-left">
        <h4 className="text-xs font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider mb-3">
          What it can do
        </h4>
        <div className="space-y-2 mb-5">
          {info.capabilities.map((cap, i) => (
            <div key={i} className="flex items-start gap-2 text-sm text-gray-600 dark:text-gray-400">
              <div className="w-1.5 h-1.5 bg-green-500 rounded-full mt-1.5 flex-shrink-0" />
              {cap}
            </div>
          ))}
        </div>
        <h4 className="text-xs font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider mb-3">
          What it cannot do
        </h4>
        <div className="space-y-2 mb-5">
          {info.limitations.map((lim, i) => (
            <div key={i} className="flex items-start gap-2 text-sm text-gray-600 dark:text-gray-400">
              <div className="w-1.5 h-1.5 bg-red-400 rounded-full mt-1.5 flex-shrink-0" />
              {lim}
            </div>
          ))}
        </div>
        {info.showStepTypes && (
          <>
            <h4 className="text-xs font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider mb-3">
              Available step types
            </h4>
            <div className="grid grid-cols-2 gap-2">
              {STEP_TYPES.map((st, i) => (
                <div key={i} className="bg-gray-50 dark:bg-gray-800 rounded-lg p-2 border border-gray-200 dark:border-gray-700">
                  <div className="text-xs font-medium text-gray-800 dark:text-gray-200">{st.name}</div>
                  <div className="text-[11px] text-gray-500 dark:text-gray-400 leading-tight mt-0.5">{st.desc}</div>
                </div>
              ))}
            </div>
          </>
        )}
      </div>
    </div>
  )
}

interface ChatAreaProps {
  // New chat handler
  onNewChat: () => void
  // Hide header when used inside another layout (like WorkflowLayout)
  hideHeader?: boolean
  // Hide input area when used inside workflow mode
  hideInput?: boolean
  // Compact mode for smaller font sizes (used in workflow layout)
  compact?: boolean
  // Tab ID - if provided, use this tab's session ID (works for both chat and workflow modes).
  // Pass null explicitly to disable all active behavior (SSE, polling, queue) — used when
  // this ChatArea instance is hidden behind another instance for the same tab.
  tabId?: string | null
}

// Ref interface for ChatArea component
export interface ChatAreaRef {
  handleNewChat: () => void
  resetChatState: () => void
  refreshWorkflowPresets: () => Promise<void>
  submitQuery: (query: string, executionOptions?: ExecutionOptions) => Promise<void>
  getEvents: () => PollingEvent[]
  isStreaming: boolean
  currentWorkflowPhase: string
}


// Global flag to ensure auto-restore only happens once per page load
let globalHasRestored = false

// Inner component for chat area
const ChatAreaInner = forwardRef((props: ChatAreaProps, ref: ForwardedRef<ChatAreaRef>) => {
  const { onNewChat, hideHeader = false, hideInput = false, compact = false, tabId } = props
  // null means "inactive — don't subscribe to any tab or run any effects"
  const isInactive = tabId === null

  // Store subscriptions
  const {
    agentMode,
    setCurrentQuery,
  } = useAppStore(useShallow(state => ({
    agentMode: state.agentMode,
    setCurrentQuery: state.setCurrentQuery,
  })))
  
  const { selectedModeCategory, getAgentModeFromCategory } = useModeStore(useShallow(state => ({
    selectedModeCategory: state.selectedModeCategory,
    getAgentModeFromCategory: state.getAgentModeFromCategory
  })))
  const { getActivePreset, applyPreset, clearActivePreset, currentPresetServers } = useGlobalPresetStore(useShallow(state => ({
    getActivePreset: state.getActivePreset,
    applyPreset: state.applyPreset,
    clearActivePreset: state.clearActivePreset,
    currentPresetServers: state.currentPresetServers
  })))
  
  // Derive correct agent mode from selectedModeCategory (source of truth)
  const correctAgentMode = useMemo(() => {
    if (selectedModeCategory) {
      return getAgentModeFromCategory(selectedModeCategory) as AgentMode
    }
    return agentMode // Fallback to agentMode if selectedModeCategory is null
  }, [selectedModeCategory, agentMode, getAgentModeFromCategory])
  
  // LLM provider configs are read via useLLMStore.getState() in helpers
  
  const {
    toolList: allTools,
    selectedServers,
  } = useMCPStore(useShallow(state => ({
    toolList: state.toolList,
    selectedServers: state.selectedServers,
  })))

  // All servers that are currently connected (status=ok)
  const connectedServers = useMemo(
    () => new Set(allTools.filter(t => t.status === 'ok').map(t => t.server).filter(Boolean)),
    [allTools]
  )
  
  // Get active tab reactively (works for both chat and workflow modes)
  // Use selector to ensure reactivity when tab config changes
  const activeTabIdFromStore = useChatStore(state => state.activeTabId)
  // null = explicitly inactive (no tab); undefined = use store's active tab
  const targetTabId = isInactive ? null : (tabId || activeTabIdFromStore)
  const activeTab = useChatStore(state => 
    targetTabId ? state.chatTabs[targetTabId] : undefined
  )
  
  // PERF FIX: Stable tab-session key to avoid phantom re-renders.
  //
  // PROBLEM: Previously `const chatTabs = useChatStore(state => state.chatTabs)` subscribed
  // to the full chatTabs object. Every `setTabStreaming`, `setTabCompleted`, `setTabConfig`
  // call creates a new `chatTabs` reference (Zustand immutable update), causing ChatArea
  // to re-render even when no tab/session was added or removed. This caused 10-20 phantom
  // renders between actual data changes (visible as "no dep change" in render logs).
  //
  // FIX: Derive a stable string key from tab IDs + session IDs + modes. This key only
  // changes when tabs are created/deleted or sessions are assigned — NOT when tab properties
  // tabsWithSessions, tabsWithActiveSessions) recompute only when this key changes.
  const tabSessionKey = useChatStore(state => {
    const tabs = state.chatTabs
    const parts: string[] = []
    for (const id of Object.keys(tabs)) {
      const t = tabs[id]
      parts.push(`${id}:${t.sessionId || ''}:${t.metadata?.mode || ''}`)
    }
    return parts.sort().join(',')
  })

  // Determine which servers to use based on mode category
  // CRITICAL: Workflow preset servers should ONLY be used in workflow mode, never leak into chat mode
  const effectiveServers = useMemo(() => {
    // For workflow mode, use preset servers
    if (selectedModeCategory === 'workflow') {
      return currentPresetServers.length > 0 ? currentPresetServers : selectedServers
    }
    // For chat mode, ALWAYS use tab's selected servers from config (if available), otherwise fall back to global
    // NEVER use currentPresetServers in chat mode - workflow preset state is isolated to workflow mode only
    const isChatLike = selectedModeCategory === 'chat' || selectedModeCategory === 'multi-agent'
    const tabSelectedServers = (isChatLike && activeTab?.config)
      ? activeTab.config.selectedServers 
      : selectedServers
    
    // If no servers are selected (empty array), default to no servers (pure LLM mode)
    // User must explicitly select servers to enable tools
    if (tabSelectedServers.length === 0) {
      return ["NO_SERVERS"]
    }
    // Filter out servers that aren't currently connected (status=ok).
    // Stale servers from localStorage could block queries if sent to backend.
    const filtered = tabSelectedServers.filter(s => s === "NO_SERVERS" || connectedServers.has(s))
    return filtered
  }, [
    selectedModeCategory,
    currentPresetServers,
    selectedServers,
    connectedServers,
    activeTab?.config
  ])
  
  // Filter tools to only include those from effective servers
  // If "NO_SERVERS" is selected, return empty tools (pure LLM mode)
  // Also filter out workspace tools if workspace access is disabled
  const enabledTools = useMemo(() => {
    if (effectiveServers.includes("NO_SERVERS")) {
      return []
    }
    
    // Get workspace access setting from tab config (default: true)
    const enableWorkspaceAccess = ((selectedModeCategory === 'chat' || selectedModeCategory === 'multi-agent') && activeTab?.config)
      ? (activeTab.config.enableWorkspaceAccess ?? true)
      : true // Default to enabled for workflow mode or if no tab config
    
    let filteredTools = allTools.filter(tool => 
      tool.server && effectiveServers.includes(tool.server)
    )
    
    // Filter out workspace tools if workspace access is disabled
    // Use category-based filtering: check if tool name is in WORKSPACE_TOOLS list
    // This matches the backend category system where workspace tools have category "workspace"
    if (!enableWorkspaceAccess) {
      const workspaceToolSet = new Set<string>(WORKSPACE_TOOLS as readonly string[])
      filteredTools = filteredTools.filter(tool => {
        const toolName = tool.name || ''
        return !workspaceToolSet.has(toolName)
      })
    }
    
    return filteredTools
  }, [allTools, effectiveServers, selectedModeCategory, activeTab?.config])
  
  // PERF FIX: Derive tab lists from stable tabSessionKey instead of raw chatTabs reference.
  // Uses getState() for the actual tab objects (avoids subscription), and tabSessionKey
  // as the recomputation trigger (only changes on tab add/remove/session change).
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const allTabs = useMemo(() => Object.values(useChatStore.getState().chatTabs), [tabSessionKey])
  const tabsWithSessions = useMemo(() => allTabs.filter(tab => tab.sessionId), [allTabs])
  
  // No observer ID syncing needed - sessions are used directly

  const {
    // Chat state
    isStreaming,
    setIsStreaming,
    lastEventIndex,
    setLastEventIndex,
    pollingInterval,
    // Deprecated: totalEvents, setTotalEvents, setLastEventCount, events, setEvents removed
    getTabEvents,
    addTabEvents,
    setTabEvents,
    getTabLastEventIndex,
    setTabLastEventIndex,
    setHasActiveChat,
    autoScroll,
    setAutoScroll,
    finalResponse,
    setIsCompleted,
    isLoadingHistory,
    setIsLoadingHistory,
    // eslint-disable-next-line @typescript-eslint/no-unused-vars
    setIsApprovingWorkflow: _setIsApprovingWorkflow,
    sessionState,
    setSessionState,
    isCheckingActiveSessions,
    setIsCheckingActiveSessions,
    currentWorkflowPhase,
    setCurrentWorkflowPhase,
    setCurrentWorkflowQueryId,
    toasts,
    addToast,
    removeToast,
    resetChatState,
    isAtBottom,
    switchTab
  } = useChatStore(useShallow(state => ({
    isStreaming: state.isStreaming,
    setIsStreaming: state.setIsStreaming,
    lastEventIndex: state.lastEventIndex,
    setLastEventIndex: state.setLastEventIndex,
    pollingInterval: state.pollingInterval,
    getTabEvents: state.getTabEvents,
    addTabEvents: state.addTabEvents,
    setTabEvents: state.setTabEvents,
    getTabLastEventIndex: state.getTabLastEventIndex,
    setTabLastEventIndex: state.setTabLastEventIndex,
    setHasActiveChat: state.setHasActiveChat,
    autoScroll: state.autoScroll,
    setAutoScroll: state.setAutoScroll,
    finalResponse: state.finalResponse,
    setIsCompleted: state.setIsCompleted,
    isLoadingHistory: state.isLoadingHistory,
    setIsLoadingHistory: state.setIsLoadingHistory,
    setIsApprovingWorkflow: state.setIsApprovingWorkflow,
    sessionState: state.sessionState,
    setSessionState: state.setSessionState,
    isCheckingActiveSessions: state.isCheckingActiveSessions,
    setIsCheckingActiveSessions: state.setIsCheckingActiveSessions,
    currentWorkflowPhase: state.currentWorkflowPhase,
    setCurrentWorkflowPhase: state.setCurrentWorkflowPhase,
    setCurrentWorkflowQueryId: state.setCurrentWorkflowQueryId,
    toasts: state.toasts,
    addToast: state.addToast,
    removeToast: state.removeToast,
    resetChatState: state.resetChatState,
    isAtBottom: state.isAtBottom,
    switchTab: state.switchTab
  })))

  // Session-specific selector: only re-renders when the ACTIVE session's events change
  // (not when any other session gets events)
  const activeSessionId = activeTab?.sessionId
  const tabEvents = useChatStore((state) =>
    activeSessionId ? state.tabEvents[activeSessionId] || EMPTY_EVENTS : EMPTY_EVENTS
  )

  // Get active preset for workflow mode
  const activeWorkflowPreset = getActivePreset('workflow')
  const selectedWorkflowPreset = activeWorkflowPreset?.id || null
  
  // Always use tab events - never fall back to global events to prevent cross-tab mixing
  // If there are no tabs, return empty array (tabs should always exist in multi-tab mode)
  // Filter out workspace_file_operation events from display
  // (These events are still sent to frontend for workspace store processing, but hidden from chat UI)
  const displayEvents = useMemo(() => {
    return tabEvents.filter(event => {
      if (event.type === 'workspace_file_operation') return false

      // Hide Total Token Usage and Context Offloading events
      if (event.type === 'token_usage') {
        const agentEvent = event.data as { data?: Record<string, unknown> } | undefined
        const payload = agentEvent?.data || event.data as Record<string, unknown> | undefined

        if (payload?.context === 'conversation_total') {
          return false
        }
      }

      if (event.type === 'large_tool_output_detected' || event.type === 'large_tool_output_file_written') {
        return false
      }

      return true
    })
  }, [tabEvents])

  // --- Render tracking (filter by [Render] in console) ---
  useRenderLogger('ChatArea', {
    displayEvents: displayEvents.length,
    tabEvents: tabEvents.length,
    isStreaming,
    autoScroll,
    activeTabId: activeTab?.tabId,
    activeSessionId,
    finalResponse: !!finalResponse,
    tabSessionKey,
  })
  useMemoLogger('ChatArea.displayEvents', displayEvents, displayEvents.length)
  
  // Computed values
  const isRequiredFolderSelected = useMemo(() => {
    if (selectedModeCategory !== 'workflow') return true; // No validation needed for other modes

    // Workflow mode requires Workflow/ folder from preset
    if (selectedModeCategory === 'workflow') {
      const workflowFolder = activeWorkflowPreset?.selectedFolder?.filepath
      return workflowFolder ? workflowFolder.startsWith('Workflow/') : false
    }
    
    return true;
  }, [selectedModeCategory, activeWorkflowPreset])

  // Use currentPresetServers from props (passed from App.tsx when preset is selected)

  // State for preset selection overlay
  const [showPresetSelection, setShowPresetSelection] = useState(false)
  const [pendingModeCategory, setPendingModeCategory] = useState<Exclude<ModeCategory, 'chat' | null> | null>(null)
  
  // State for mode switch dialog
  const [showModeSwitchDialog, setShowModeSwitchDialog] = useState(false)
  const [pendingModeSwitch, setPendingModeSwitch] = useState<Exclude<ModeCategory, null> | null>(null)
  

  // Handle mode selection from dropdown
  // Handle mode switching with preset selection for Workflow
  const handleModeSwitchWithPreset = (category: Exclude<ModeCategory, null>) => {
    if (category === 'chat') {
      // Chat mode doesn't need preset selection
      // Clear any active presets when switching to chat mode
      clearActivePreset('workflow')
      switchMode(category)
    } else {
      // Workflow mode - always show preset selection when switching between modes
      // Clear the current mode's preset first
      if (selectedModeCategory === 'workflow') {
        clearActivePreset('workflow')
      }
      
      // Check if target mode already has a preset
      const activePreset = getActivePreset(category)
      
      if (activePreset) {
        // Preset already selected, switch mode directly
        switchMode(category)
      } else {
        // No preset selected, show preset selection overlay
        setPendingModeCategory(category)
        setShowPresetSelection(true)
      }
    }
  }

  // Switch mode function
  const switchMode = (category: Exclude<ModeCategory, null>) => {
    const { setModeCategory, getAgentModeFromCategory } = useModeStore.getState()
    const { setAgentMode } = useAppStore.getState()
    
    setModeCategory(category)
    
    // Set the corresponding agent mode using centralized mapping
    const agentModeToSet = getAgentModeFromCategory(category) as AgentMode
    setAgentMode(agentModeToSet)
  }

  // Handle preset selection from overlay
  const handlePresetSelected = (presetId: string) => {
    if (pendingModeCategory) {
      // Now switch to the mode
      switchMode(pendingModeCategory)
      
      // Apply the preset after mode switch (this will also set the active preset ID)
      setTimeout(() => {
        const result = applyPreset(presetId, pendingModeCategory)
        if (!result.success) {
          logger.error('ChatArea', 'Failed to apply preset:', result.error)
        }
      }, 100)
      
      // Close overlay
      setShowPresetSelection(false)
      setPendingModeCategory(null)
    }
  }

  // Handle preset selection overlay close
  const handlePresetSelectionClose = () => {
    setShowPresetSelection(false)
    setPendingModeCategory(null)
  }

  
  // Filter toasts to only include types supported by ToastContainer
  const filteredToasts = toasts.filter((toast: { type: string }) => toast.type === 'success' || toast.type === 'info' || toast.type === 'error') as Array<{id: string, message: string, type: 'success' | 'info' | 'error'}>
  
  // Handle mode switch dialog confirmation
  const handleModeSwitchConfirm = () => {
    if (pendingModeSwitch) {
      handleModeSwitchWithPreset(pendingModeSwitch)
      // Clear backend session and reset UI after mode switch
      handleNewChat()
    }
    setShowModeSwitchDialog(false)
    setPendingModeSwitch(null)
  }
  
  // Handle mode switch dialog cancellation
  const handleModeSwitchCancel = () => {
    setShowModeSwitchDialog(false)
    setPendingModeSwitch(null)
  }
  
  // Add ref for auto-scrolling
  const chatContentRef = useRef<HTMLDivElement>(null)
  
  // Add ref for workflow mode handler
  const workflowModeHandlerRef = useRef<WorkflowModeHandlerRef>(null)
  
  
  // Track processed completion events to avoid stopping on old ones
  const processedCompletionEventsRef = useRef<Set<string>>(new Set())
  

  // Selected preset folder state
  const lastEventIndexRef = useRef<number>(-1)
  // Deprecated: totalEventsRef removed
  const previousEventCountRef = useRef<number>(0)

  // Track whether workspace-modifying events occurred during the current run
  const hadWorkspaceActivityRef = useRef<boolean>(false)

  // Ref to track if we're currently performing programmatic scrolling
  const isProgrammaticScrollRef = useRef<boolean>(false)
  const programmaticScrollTimeoutRef = useRef<NodeJS.Timeout | null>(null)
  // Local ref for scroll position — avoids Zustand re-renders on every scroll event
  const lastScrollTopRef = useRef<number>(0)

  // Ref to track currentWorkflowPhase without causing callback re-renders
  const currentWorkflowPhaseRef = useRef<string>(currentWorkflowPhase)
  useEffect(() => {
    currentWorkflowPhaseRef.current = currentWorkflowPhase
  }, [currentWorkflowPhase])

  // Observer initialization removed - no longer needed

  // Re-enable auto-scroll when user scrolls back to the bottom.
  // The wheel handler below covers the disable-on-scroll-up path.
  const handleScroll = useCallback(() => {
    if (!chatContentRef.current) return;
    if (isProgrammaticScrollRef.current) return;
    const element = chatContentRef.current;
    if (isAtBottom(element) && !autoScroll) setAutoScroll(true);
  }, [autoScroll, isAtBottom, setAutoScroll]);

  // Set up scroll + wheel event listeners
  useEffect(() => {
    const element = chatContentRef.current;
    if (!element) return;

    lastScrollTopRef.current = element.scrollTop;

    const onWheel = (e: WheelEvent) => {
      if (e.deltaY < 0) setAutoScroll(false); // user scrolling up → disable auto-scroll
    };

    element.addEventListener('scroll', handleScroll);
    element.addEventListener('wheel', onWheel, { passive: true });
    return () => {
      element.removeEventListener('scroll', handleScroll);
      element.removeEventListener('wheel', onWheel);
      if (programmaticScrollTimeoutRef.current) {
        clearTimeout(programmaticScrollTimeoutRef.current);
        programmaticScrollTimeoutRef.current = null;
      }
    };
  }, [handleScroll, setAutoScroll]);

  // Reset auto-scroll when starting new conversation (events go from 0 to > 0)
  // Use displayEvents (tabEvents) instead of events to track the actual displayed events
  useEffect(() => {
    const currentEventCount = displayEvents.length
    const previousEventCount = previousEventCountRef.current
    
    // Only reset auto-scroll when starting a new conversation (0 -> > 0)
    // Don't reset if user has manually disabled it or if events are just updating
    if (previousEventCount === 0 && currentEventCount > 0 && !isStreaming) {
      setAutoScroll(true);
    }
    
    previousEventCountRef.current = currentEventCount
  }, [displayEvents.length, isStreaming, setAutoScroll]);

  // Improved auto-scroll for new events
  const scrollToBottom = useCallback((behavior: ScrollBehavior = 'smooth') => {
    if (!chatContentRef.current) return;
    
    const element = chatContentRef.current;
    const targetScrollTop = element.scrollHeight - element.clientHeight;
    
    // Mark that we're performing programmatic scrolling
    isProgrammaticScrollRef.current = true
    
    // Clear any existing timeout
    if (programmaticScrollTimeoutRef.current) {
      clearTimeout(programmaticScrollTimeoutRef.current)
    }
    
    // Use requestAnimationFrame for smoother scrolling
    requestAnimationFrame(() => {
      element.scrollTo({
        top: targetScrollTop,
        behavior
      });
      
      // Clear the programmatic scroll flag after scroll completes
      // For smooth scroll, wait longer; for instant, clear immediately
      const timeoutDuration = behavior === 'smooth' ? 600 : 100
      programmaticScrollTimeoutRef.current = setTimeout(() => {
        isProgrammaticScrollRef.current = false
        programmaticScrollTimeoutRef.current = null
      }, timeoutDuration)
    });
  }, [])

  // Callback to re-enable auto-scroll and scroll to bottom after feedback submission
  const handleFeedbackSubmitted = useCallback(() => {
    setAutoScroll(true)
    scrollToBottom('smooth')
  }, [setAutoScroll, scrollToBottom])

  // Auto-scroll to bottom when new events arrive (only if autoScroll is enabled)
  // Use displayEvents (tabEvents) instead of events to track the actual displayed events
  useEffect(() => {
    if (autoScroll && chatContentRef.current && displayEvents.length > 0) {
      // During streaming, use instant scroll — smooth scroll called repeatedly every event
      // causes each call to interrupt the previous animation, producing visible jank.
      scrollToBottom(isStreaming ? 'instant' : 'smooth');
    }
  }, [displayEvents.length, autoScroll, scrollToBottom, isStreaming])

  // Auto-scroll to bottom when final response is updated (only if autoScroll is enabled)
  useEffect(() => {
    if (autoScroll && chatContentRef.current && finalResponse) {
      scrollToBottom('smooth');
    }
  }, [finalResponse, autoScroll, scrollToBottom])

  // Scroll to bottom when switching tabs
  useEffect(() => {
    if (!targetTabId) return
    // Small delay to let the new tab's content render before scrolling
    const timer = setTimeout(() => {
      scrollToBottom('instant')
    }, 50)
    return () => clearTimeout(timer)
  }, [targetTabId, scrollToBottom])

  // Auto-scroll when streaming text first appears (brings the "Generating..." card into view)
  const hasStreamingText = useChatStore(state =>
    activeSessionId ? !!state.streamingText[activeSessionId] : false
  )
  const prevHasStreamingTextRef = useRef(false)
  useEffect(() => {
    if (hasStreamingText && !prevHasStreamingTextRef.current && autoScroll && chatContentRef.current) {
      scrollToBottom('smooth')
    }
    prevHasStreamingTextRef.current = hasStreamingText
  }, [hasStreamingText, autoScroll, scrollToBottom])


  // Update refs when values change (for global observer)
  useEffect(() => {
    if (!activeTab) {
      lastEventIndexRef.current = lastEventIndex
    }
  }, [lastEventIndex, activeTab])
  
  // Update displayEvents when active tab changes
  // Tab events are automatically loaded via tabEvents useMemo
  
  // Deprecated: totalEventsRef useEffect removed

  // Workflow preset handlers
  const handleWorkflowPresetSelected = useCallback(async (presetId: string, presetContent: string) => {
    // Apply the preset using the global preset store
    // File context is now preset-specific (from preset.selectedFolder), no need to clear
    applyPreset(presetId, 'workflow')
    setCurrentWorkflowQueryId(presetId) // Store the preset query ID for workflow approval
    
    try {
      // Ensure phases are loaded and get them from store
      const workflowStore = useWorkflowStore.getState()
      if (!workflowStore.phasesInitialized) {
        await workflowStore.loadPhases()
      }
      const phases = workflowStore.phases
      const phaseIds = phases.map(p => p.id)
      const defaultPhase = workflowStore.getDefaultPhase()
      
      // Check if workflow already exists for this preset
      const workflowStatus = await agentApi.getWorkflowStatus(presetId)
      
      if (workflowStatus.success && workflowStatus.workflow) {
        const workflow = workflowStatus.workflow
        const status = workflow.workflow_status
        
        // Set the workflow phase based on the database status
        // Use the status if it's a valid phase ID, otherwise use default (first phase)
        if (status && phaseIds.includes(status)) {
          setCurrentWorkflowPhase(status)
        } else {
          // Default to first phase if status is invalid or not found
          setCurrentWorkflowPhase(defaultPhase)
        }
        
        // Use presetContent directly (this is the objective from preset query)
        setCurrentQuery(presetContent)
      } else {
        // No workflow exists, proceed with default phase
        setCurrentWorkflowPhase(defaultPhase)
        setCurrentQuery(presetContent)
      }
    } catch (error) {
      logger.error('ChatArea', 'Error checking workflow status:', error)
      // Fallback to default phase on error
      const defaultPhase = useWorkflowStore.getState().getDefaultPhase()
      setCurrentWorkflowPhase(defaultPhase)
      setCurrentQuery(presetContent)
    }
  }, [setCurrentQuery, applyPreset, setCurrentWorkflowPhase, setCurrentWorkflowQueryId])

  const handleWorkflowPresetCleared = useCallback(() => {
    clearActivePreset('workflow')
    setCurrentWorkflowQueryId(null) // Clear the stored preset query ID
    const defaultPhase = useWorkflowStore.getState().getDefaultPhase()
    setCurrentWorkflowPhase(defaultPhase) // Reset to default phase
    setCurrentQuery('')
  }, [clearActivePreset, setCurrentWorkflowQueryId, setCurrentWorkflowPhase, setCurrentQuery])
  
  // Clear workflow state when starting a new chat
  const clearWorkflowState = useCallback(() => {
    clearActivePreset('workflow')
    setCurrentWorkflowQueryId(null)
    const defaultPhase = useWorkflowStore.getState().getDefaultPhase()
    setCurrentWorkflowPhase(defaultPhase)
  }, [clearActivePreset, setCurrentWorkflowQueryId, setCurrentWorkflowPhase])

  // Handle human verification actions
  // TODO: Re-enable when RequestHumanFeedbackEvent is available
  /*
  const handleApproveWorkflow = useCallback(async (_requestId: string, eventData?: { next_phase?: string }) => {
    
    setIsApprovingWorkflow(true)  // Set loading state
    
    // Use the stored preset query ID instead of the request ID
    const presetQueryId = currentWorkflowQueryId
    if (!presetQueryId) {
      logger.error('ChatArea', 'No preset query ID available for workflow approval')
      setIsApprovingWorkflow(false)
      return
    }
    
    try {
      // Determine next phase based on event data
      // If next_phase is provided, use it; otherwise get the second phase (planning) as default
      let nextPhase = eventData?.next_phase
      if (!nextPhase) {
        const phases = useWorkflowStore.getState().phases
        // Use second phase (planning) if available, otherwise first phase
        nextPhase = phases.length > 1 ? phases[1].id : (phases.length > 0 ? phases[0].id : 'execution')
      }
      
      // Update workflow status to the determined next phase
      await agentApi.updateWorkflow(presetQueryId, nextPhase)
      
      // Stop any ongoing SSE / polling to prevent events from coming back
      if (currentTab?.sessionId) {
        disconnectSSE(currentTab.sessionId)
      }
      if (pollingInterval) {
        stopPolling()
      }

      // Clear all events to show clean slate for execution phase
      // Note: Using tabEvents now, not global events
      if (currentTab?.sessionId) {
        chatStore.clearTabEvents(currentTab.sessionId)
      }
      // Deprecated: setLastEventCount removed
      setLastEventIndex(-1)
      setFinalResponse('')
      setIsCompleted(false)
      setCurrentUserMessage('')
      setShowUserMessage(false)
      
      // Update phase to the determined next phase
      setCurrentWorkflowPhase(nextPhase as WorkflowPhase)
      
    } catch (error) {
      logger.error('ChatArea', 'Failed to approve workflow:', error)
      // TODO: Show error message to user
    } finally {
      setIsApprovingWorkflow(false)  // Clear loading state
    }
  }, [currentWorkflowQueryId, pollingInterval, setIsApprovingWorkflow, setLastEventIndex, setFinalResponse, setIsCompleted, setCurrentUserMessage, setShowUserMessage, setCurrentWorkflowPhase, setPollingInterval])
  */

  // Observer initialization removed - no longer needed

  // SSE event batching: accumulate non-streaming events per session and flush once per animation frame
  // Streaming events (streaming_start/chunk/end) bypass batching for real-time text display
  interface PendingSSEBatch {
    events: PollingEvent[]
    sessionStatus?: string
    lastProcessedIndex: number
    hasMore?: boolean
    hasRunningBgAgents?: boolean
    sessionId: string // actualSessionId (may differ from map key)
  }
  const pendingSSEEventsRef = useRef<Map<string, PendingSSEBatch>>(new Map())
  // Throttle flush to max every 200ms (was requestAnimationFrame = up to 60x/sec).
  // Streaming text is handled immediately outside this path, so user-visible latency is unaffected.
  const flushRAFRef = useRef<ReturnType<typeof setTimeout> | 0>(0)

  // Removed extractUserMessageContent - no longer needed since we removed duplicate detection


  // Get polling management actions from store (before pollEvents callback)
  const { startPolling, stopPolling, getActiveSessions, connectSSE, disconnectSSE, disconnectAllSSE } = useChatStore(useShallow(state => ({
    startPolling: state.startPolling,
    stopPolling: state.stopPolling,
    getActiveSessions: state.getActiveSessions,
    connectSSE: state.connectSSE,
    disconnectSSE: state.disconnectSSE,
    disconnectAllSSE: state.disconnectAllSSE,
  })))

  // Get active sessions from cache (shared across all components)
  const startActiveSessionsPolling = useChatStore(state => state.startActiveSessionsPolling)
  
  // Subscribe to active sessions cache updates
  // Get the array first, then memoize the Set to avoid infinite loops
  const activeSessionsCache = useChatStore((state) => state.activeSessionsCache)
  const activeSessionIds = useMemo(() => {
    return new Set(activeSessionsCache.map(s => s.session_id))
  }, [activeSessionsCache])

  // Track recently notified workshop agent names to prevent duplicate notifications
  // (retries emit multiple orchestrator_agent_end events with the same agent name)
  const notifiedWorkshopAgentsRef = useRef<Set<string>>(new Set())

  // Reusable event processing logic — shared by both SSE and polling paths.
  // Takes an events response (same shape from SSE or REST) and a tab, then processes
  // session status, streaming chunks, event filtering, and stores events.
  const processEventsResponse = useCallback((
    response: { events: PollingEvent[]; session_status?: string; last_processed_index?: number; has_more?: boolean; has_running_background_agents?: boolean; session_id?: string },
    sessionId: string,
    tab: ChatTab | null
  ) => {
    const chatStore = useChatStore.getState()
    const actualSessionId = response.session_id || sessionId


    // --- Session status handling ---
    const sessionStatus = response.session_status
    if (tab && sessionStatus) {
      const hasBgAgents = response.has_running_background_agents ?? false
      if (sessionStatus === 'completed' || sessionStatus === 'error') {
        console.log(`[QUEUE_DEBUG] SESSION tabId=${tab.tabId} status=${sessionStatus} hasBgAgents=${hasBgAgents} isCompleted=${tab.isCompleted} isStreaming=${tab.isStreaming} queueLen=${tab.config?.queuedMessages?.length ?? 0}`)
        if (hasBgAgents) {
          chatStore.setTabCompleted(tab.tabId, false)
          chatStore.setTabStreaming(tab.tabId, false)
        } else {
          chatStore.setTabCompleted(tab.tabId, true)
          chatStore.setTabStreaming(tab.tabId, false)
        }
        chatStore.clearStreamingText(actualSessionId)
      } else if (sessionStatus === 'running') {
        chatStore.setTabCompleted(tab.tabId, false)
        chatStore.setTabStreaming(tab.tabId, true)
      } else if (sessionStatus === 'stopped' || sessionStatus === 'inactive') {
        chatStore.setTabCompleted(tab.tabId, false)
        chatStore.setTabStreaming(tab.tabId, false)
        chatStore.clearStreamingText(actualSessionId)
      }
      chatStore.setTabHasRunningBgAgents(tab.tabId, hasBgAgents)
    } else if (!tab && sessionStatus) {
      if (sessionStatus === 'completed' || sessionStatus === 'error') {
        setIsStreaming(false)
        setIsCompleted(true)
        setHasActiveChat(false)
        chatStore.clearStreamingText(actualSessionId)
      } else if (sessionStatus === 'running') {
        setIsStreaming(true)
        setIsCompleted(false)
      } else if (sessionStatus === 'stopped' || sessionStatus === 'inactive') {
        setIsStreaming(false)
        setIsCompleted(false)
        chatStore.clearStreamingText(actualSessionId)
      }
    }

    // --- Update last event index ---
    // CRITICAL: Must happen BEFORE the empty-events early return below.
    // SSE backfill may contain only streaming events (handled immediately in handleSSEMessage),
    // leaving the batched events array empty. Without updating the index here, tabEventIndices
    // stays at 0 and every SSE reconnection re-fetches all events from the beginning.
    if (response.last_processed_index !== undefined && response.last_processed_index >= 0) {
      let newLastEventIndex = response.last_processed_index
      if (tab) {
        setTabLastEventIndex(actualSessionId, newLastEventIndex)
        if (response.has_more !== undefined) {
          chatStore.setTabHasMoreOlderEvents(actualSessionId, response.has_more)
        }
      } else {
        setLastEventIndex(newLastEventIndex)
      }
    }

    if (response.events.length === 0) return

    // --- Event filtering & processing ---
    const eventsBeforeFilter = response.events as PollingEvent[]
    const newEvents: PollingEvent[] = []
    let hasCompletionEvent = false

    for (const event of eventsBeforeFilter) {
      const agentEvent = event.data as Record<string, unknown> | undefined
      const innerData = agentEvent?.data as Record<string, unknown> | undefined
      const rawComponent = (event as unknown as Record<string, unknown>).component ?? innerData?.component ?? agentEvent?.component
      const rawCorrelationId = (event as unknown as Record<string, unknown>).correlation_id ?? innerData?.correlation_id ?? agentEvent?.correlation_id
      const isSubAgentEvent = (typeof rawComponent === 'string' && rawComponent.startsWith('delegation-'))
        || (typeof rawCorrelationId === 'string' && (rawCorrelationId.startsWith('delegation-') || rawCorrelationId.startsWith('workshop-')))

      if (event.type === 'streaming_start') {
        const correlationId = innerData?.correlation_id ?? agentEvent?.correlation_id
        const isDelegationStreaming = typeof correlationId === 'string' && correlationId.startsWith('delegation-')
        // Workshop background agents (execute_step, optimize_step, generate_learnings) use
        // workshop-* correlation IDs. Drop their streaming events — they render in EventDisplay cards.
        const isWorkshopStreaming = typeof correlationId === 'string' && correlationId.startsWith('workshop-')
        if (isDelegationStreaming) {
          chatStore.clearDelegationStreamingText(correlationId as string)
        } else if (!isWorkshopStreaming) {
          chatStore.clearStreamingText(actualSessionId)
        }
        continue
      }
      if (event.type === 'streaming_chunk') {
        const correlationId = innerData?.correlation_id ?? agentEvent?.correlation_id
        const isDelegationStreaming = typeof correlationId === 'string' && correlationId.startsWith('delegation-')
        const isWorkshopStreaming = typeof correlationId === 'string' && correlationId.startsWith('workshop-')
        const rawContent = innerData?.content ?? agentEvent?.content
        const content = typeof rawContent === 'string' ? rawContent : ''
        const rawIndex = innerData?.chunk_index ?? agentEvent?.chunk_index
        const chunkIndex = typeof rawIndex === 'number' ? rawIndex : -1
        if (isDelegationStreaming) {
          if (content) {
            if (chunkIndex === 0 || chunkIndex === 1) chatStore.clearDelegationStreamingText(correlationId as string)
            chatStore.appendDelegationStreamingChunk(correlationId as string, chunkIndex, content)
          }
        } else if (!isWorkshopStreaming && content) {
          if (chunkIndex === 0 || chunkIndex === 1) {
            chatStore.clearStreamingText(actualSessionId)
          }
          chatStore.appendStreamingChunk(actualSessionId, chunkIndex, content)
        }
        continue
      }
      if (event.type === 'streaming_end') {
        const correlationId = innerData?.correlation_id ?? agentEvent?.correlation_id
        const isDelegationStreaming = typeof correlationId === 'string' && correlationId.startsWith('delegation-')
        const isWorkshopStreaming = typeof correlationId === 'string' && correlationId.startsWith('workshop-')
        if (!isDelegationStreaming && !isWorkshopStreaming) {
          chatStore.clearStreamingStatus(actualSessionId)
          const sidForClear = actualSessionId
          const textSnapshot = useChatStore.getState().streamingText[sidForClear]
          setTimeout(() => {
            const currentText = useChatStore.getState().streamingText[sidForClear]
            const match = currentText === textSnapshot
            if (currentText && match) {
              useChatStore.getState().clearStreamingText(sidForClear)
            }
          }, 500)
        }
        continue
      }
      if (event.type === 'user_message') continue

      if (!isSubAgentEvent && (event.type === 'llm_generation_end' || event.type === 'unified_completion' || event.type === 'agent_end' || event.type === 'conversation_end' || event.type === 'conversation_error' || event.type === 'context_cancelled')) {
        hasCompletionEvent = true
      }

      if (event.type === 'delegation_end') {
        const correlationId = innerData?.correlation_id ?? innerData?.delegation_id ?? agentEvent?.correlation_id ?? agentEvent?.delegation_id
        if (correlationId && typeof correlationId === 'string') {
          chatStore.clearDelegationStreamingText(correlationId)
        }
      }

      // Auto-refresh plan canvas when a plan modification tool completes
      if (event.type === 'tool_call_end') {
        const toolName = (innerData?.tool_name ?? agentEvent?.tool_name ?? '') as string
        const isPlanModTool = toolName.startsWith('update_') && (
          toolName.includes('step') || toolName.includes('validation') || toolName.includes('success_criteria')
        )
        const isAddTool = toolName.startsWith('add_') && toolName.includes('step')
        const isDeleteTool = toolName === 'delete_plan_steps'
        if (isPlanModTool || isAddTool || isDeleteTool) {
          console.log('[PLAN REFRESH] Plan modification detected via tool:', toolName)
          signalPlanModified()
        }
      }

      // Also detect workspace_file_operation events targeting plan.json
      if (event.type === 'workspace_file_operation') {
        const filePath = (innerData?.filepath ?? agentEvent?.filepath ?? innerData?.file_path ?? agentEvent?.file_path ?? '') as string
        if (filePath.includes('plan.json') || filePath.includes('step_config.json')) {
          console.log('[PLAN REFRESH] Workspace file operation on plan file:', filePath)
          signalPlanModified()
        }
      }

      // Clear dedup tracker when a new workshop step execution or debug starts
      if (event.type === 'orchestrator_agent_start') {
        const startAgentType = innerData?.agent_type ?? agentEvent?.agent_type
        if (startAgentType === 'workshop-step-execution' || startAgentType === 'workshop-step-debug' || startAgentType === 'workshop-step-learning') {
          notifiedWorkshopAgentsRef.current.clear()
        }
      }

      // Auto-notify chat agent when a workshop step completes (the wrapper event only).
      // Sub-agent events (execution, learning) are ignored — only the final step-level event triggers a notification.
      if (event.type === 'orchestrator_agent_end' && tab) {
        const agentType = (innerData?.agent_type ?? agentEvent?.agent_type ?? '') as string
        if (agentType === 'workshop-step-execution' || agentType === 'workshop-step-debug' || agentType === 'workshop-step-learning') {
          const agentName = (innerData?.agent_name ?? agentEvent?.agent_name ?? 'unknown') as string
          const success = (innerData?.success ?? agentEvent?.success) as boolean
          const result = (innerData?.result ?? agentEvent?.result ?? '') as string

          // Skip notification for cancelled steps — only real failures (validation, execution errors)
          // should be reported to the agent for debugging
          const isCancelled = result.startsWith('Cancelled:')
          if (isCancelled) {
            console.log('[WORKSHOP] Skipping notification for cancelled step', { agentName, result })
          } else {
            const truncated = result.length > 5000 ? result.substring(0, 5000) + '...' : result

            // Prefix all notifications so the LLM knows these are automated, not user messages
            const AUTO_PREFIX = '[AUTO-NOTIFICATION] '
            let notification: string
            if (agentType === 'workshop-step-learning') {
              notification = success
                ? `${AUTO_PREFIX}[LEARNING COMPLETE] ${agentName} — ${truncated}`
                : `${AUTO_PREFIX}[LEARNING FAILED] ${agentName} failed.\nError: ${truncated}`
            } else if (agentType === 'workshop-step-debug') {
              notification = success
                ? `${AUTO_PREFIX}[OPTIMIZATION COMPLETE] ${agentName} — ${truncated}`
                : `${AUTO_PREFIX}[OPTIMIZATION FAILED] ${agentName} failed.\nError: ${truncated}`
            } else {
              // Check if step is marked as optimized in step config (passed via input_data)
              const inputData = innerData?.input_data ?? agentEvent?.input_data
              const isOptimized = inputData?.step_optimized === 'true'

              if (success && isOptimized) {
                notification = `${AUTO_PREFIX}[STEP COMPLETED] ${agentName} finished successfully.\nResult: ${truncated}`
              } else if (success) {
                notification = `${AUTO_PREFIX}[STEP COMPLETED] ${agentName} finished successfully.\nResult: ${truncated}`
              } else {
                notification = `${AUTO_PREFIX}[STEP FAILED] ${agentName} failed.\nError: ${truncated}`
              }
            }

            const dedupeKey = `${agentName}::${agentType}`
            if (notifiedWorkshopAgentsRef.current.has(dedupeKey)) {
              console.log('[WORKSHOP] Skipping duplicate notification for', dedupeKey)
            } else {
              notifiedWorkshopAgentsRef.current.add(dedupeKey)
              const currentQueue = chatStore.getTabConfig(tab.tabId)?.queuedMessages || []
              chatStore.setTabConfig(tab.tabId, { queuedMessages: [...currentQueue, notification] })
              console.log('[WORKSHOP] Queued step completion notification', { agentName, agentType, success })
            }
          }
        }
      }

      // Track workspace-modifying events for refresh-on-completion
      if (event.type === 'workspace_file_operation') {
        hadWorkspaceActivityRef.current = true
      }
      if (event.type === 'tool_execution') {
        const toolName = innerData?.tool_name ?? agentEvent?.tool_name
        if (toolName === 'execute_shell_command') {
          hadWorkspaceActivityRef.current = true
        }
      }

      // PERF FIX: Only call processWorkspaceEvent for workspace_file_operation events.
      // Previously called for ALL events (tool_execution, streaming_text, delegation_start, etc.),
      // each incurring function call + event type check + dedup lookup overhead.
      if (event.type === 'workspace_file_operation') {
        useWorkspaceStore.getState().processWorkspaceEvent(event)
      }

      newEvents.push(event)
    }

    // PERF FIX: Mark workspace as stale instead of auto-fetching.
    //
    // PROBLEM: Previously called fetchFiles() here, which fetches the entire workspace tree
    // (~2-3MB JSON for large workspaces with many workflow runs). This happened on every
    // completion event and background agent completion.
    //
    // FIX: Set needsRefresh flag → Workspace component shows a "Files may be out of date"
    // banner with a manual "Refresh" button. New files during execution are still added
    // incrementally via addFileToTree (from workspace_file_operation events, no network).
    const isCompletionLike = hasCompletionEvent || newEvents.some(e => e.type === 'background_agent_completed')
    if (isCompletionLike && hadWorkspaceActivityRef.current) {
      hadWorkspaceActivityRef.current = false
      const isChatLikeMode = selectedModeCategory === 'chat' || selectedModeCategory === 'multi-agent'
      if (isChatLikeMode) {
        // Auto-refresh workspace for chat modes so the file tree updates immediately
        console.log('[Workspace] Auto-refreshing workspace (completion event + had workspace activity, chat mode)')
        useWorkspaceStore.getState().fetchFiles()
      } else {
        // Workflow mode: just mark stale — workflow has its own debounced refresh logic
        console.log('[Workspace] Marking needsRefresh (completion event + had workspace activity)')
        useWorkspaceStore.getState().setNeedsRefresh(true)
      }
    }

    // Defer streaming text clear
    if (hasCompletionEvent) {
      const sid = actualSessionId
      const textBeforeClear = useChatStore.getState().streamingText[sid]
      requestAnimationFrame(() => {
        const currentText = useChatStore.getState().streamingText[sid]
        if (currentText === textBeforeClear) {
          useChatStore.getState().clearStreamingText(sid)
        }
      })
    }

    // Process workflow events
    if (selectedModeCategory === 'workflow') {
      const workflowStore = useWorkflowStore.getState()
      for (const event of response.events as PollingEvent[]) {
        if (event.type === 'batch_group_start') {
          const eventData = event.data as Record<string, unknown> | undefined
          const batchGroupStartData = (eventData?.data as Record<string, unknown>) || eventData
          const groupId = batchGroupStartData?.group_id as string | undefined
          const runFolder = batchGroupStartData?.run_folder as string | undefined
          const workspacePath = batchGroupStartData?.workspace_path as string | undefined
          const groupIndex = batchGroupStartData?.group_index as number | undefined
          const totalGroups = batchGroupStartData?.total_groups as number | undefined
          if (groupId && runFolder) {
            workflowStore.handleBatchGroupStart(groupId, runFolder, workspacePath, groupIndex, totalGroups)
          }
        }
        if (event.type === 'batch_group_end') {
          const eventData = event.data as Record<string, unknown> | undefined
          const batchGroupEndData = (eventData?.data as Record<string, unknown>) || eventData
          const groupId = batchGroupEndData?.group_id as string | undefined
          const success = batchGroupEndData?.success as boolean | undefined
          const remainingGroups = batchGroupEndData?.remaining_groups as number | undefined
          if (groupId) {
            workflowStore.handleBatchGroupEnd(groupId, success, remainingGroups)
          }
        }
        if (event.type === 'step_progress_updated') {
          const eventData = event.data as Record<string, unknown> | undefined
          const stepProgressData = (eventData?.data as Record<string, unknown>) || eventData
          const stepId = stepProgressData?.current_step_id as string | undefined
          const status = stepProgressData?.status as string | undefined
          if (stepId && status) {
            if (status === 'start') {
              workflowStore.setCurrentStepId(stepId)
              workflowStore.setStepStatus(stepId, 'running')
            } else if (status === 'end') {
              workflowStore.setStepStatus(stepId, 'completed')
            } else if (status === 'failed') {
              workflowStore.setStepStatus(stepId, 'failed')
            }
          }
          const groupId = stepProgressData?.group_id as string | undefined
          const groupIndex = stepProgressData?.group_index as number | undefined
          const totalGroups = stepProgressData?.total_groups as number | undefined
          const runFolder = stepProgressData?.run_folder as string | undefined
          if (groupId && totalGroups !== undefined && totalGroups > 0) {
            workflowStore.handleBatchGroupStart(groupId, runFolder || '', undefined, groupIndex, totalGroups)
          }
        }
      }
    }

    // Store events
    if (tab) {
      const finalTab = chatStore.getTab(tab.tabId)
      if (!finalTab) return
      addTabEvents(actualSessionId, newEvents)
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [getTabEvents, setTabLastEventIndex, setLastEventIndex, addTabEvents, setIsStreaming, setIsCompleted, setHasActiveChat, selectedModeCategory])

  // Flush all pending SSE events: merges accumulated events per session into a single processEventsResponse call
  const flushPendingSSEEvents = useCallback(() => {
    flushRAFRef.current = 0
    const pending = pendingSSEEventsRef.current
    if (pending.size === 0) return

    // Log flush stats for perf debugging
    let totalEvents = 0
    for (const batch of pending.values()) totalEvents += batch.events.length
    console.debug(`[SSE Flush] ${pending.size} sessions, ${totalEvents} events`)

    // Take snapshot and clear
    const batches = new Map(pending)
    pending.clear()

    for (const [sid, batch] of batches) {
      const store = useChatStore.getState()
      const matchingTab = Object.values(store.chatTabs).find(t => t.sessionId === sid) || null
      processEventsResponse(
        {
          events: batch.events,
          session_status: batch.sessionStatus,
          last_processed_index: batch.lastProcessedIndex,
          has_more: batch.hasMore,
          has_running_background_agents: batch.hasRunningBgAgents,
          session_id: batch.sessionId !== sid ? batch.sessionId : undefined,
        },
        sid,
        matchingTab
      )
    }
  }, [processEventsResponse])

  // Handle an incoming SSE event message: process streaming events immediately, batch the rest
  const handleSSEMessage = useCallback((msg: SSEEventMessage, sid: string) => {
    const chatStore = useChatStore.getState()
    const actualSessionId = (msg as unknown as Record<string, unknown>).session_id as string || sid

    // Separate streaming events (immediate) from non-streaming events (batched)
    const nonStreamingEvents: PollingEvent[] = []
    for (const event of msg.events) {
      if (event.type === 'streaming_start' || event.type === 'streaming_chunk' || event.type === 'streaming_end') {
        // Process streaming events immediately for real-time text display
        const agentEvent = event.data as Record<string, unknown> | undefined
        const innerData = agentEvent?.data as Record<string, unknown> | undefined

        if (event.type === 'streaming_start') {
          const correlationId = innerData?.correlation_id ?? agentEvent?.correlation_id
          const isDelegation = typeof correlationId === 'string' && correlationId.startsWith('delegation-')
          if (isDelegation) {
            chatStore.clearDelegationStreamingText(correlationId as string)
          } else {
            chatStore.clearStreamingText(actualSessionId)
          }
        } else if (event.type === 'streaming_chunk') {
          const correlationId = innerData?.correlation_id ?? agentEvent?.correlation_id
          const isDelegation = typeof correlationId === 'string' && correlationId.startsWith('delegation-')
          const rawContent = innerData?.content ?? agentEvent?.content
          const content = typeof rawContent === 'string' ? rawContent : ''
          const rawIndex = innerData?.chunk_index ?? agentEvent?.chunk_index
          const chunkIndex = typeof rawIndex === 'number' ? rawIndex : -1
          if (isDelegation) {
            if (content) {
              if (chunkIndex === 0 || chunkIndex === 1) chatStore.clearDelegationStreamingText(correlationId as string)
              chatStore.appendDelegationStreamingChunk(correlationId as string, chunkIndex, content)
            }
          } else if (content) {
            if (chunkIndex === 0 || chunkIndex === 1) chatStore.clearStreamingText(actualSessionId)
            chatStore.appendStreamingChunk(actualSessionId, chunkIndex, content)
          }
        } else if (event.type === 'streaming_end') {
          const correlationId = innerData?.correlation_id ?? agentEvent?.correlation_id
          const isDelegation = typeof correlationId === 'string' && correlationId.startsWith('delegation-')
          if (!isDelegation) {
            chatStore.clearStreamingStatus(actualSessionId)
            const sidForClear = actualSessionId
            const textSnapshot = useChatStore.getState().streamingText[sidForClear]
            setTimeout(() => {
              const currentText = useChatStore.getState().streamingText[sidForClear]
              const match = currentText === textSnapshot
              if (currentText && match) {
                useChatStore.getState().clearStreamingText(sidForClear)
              }
            }, 500)
          }
        }
      } else {
        nonStreamingEvents.push(event)
      }
    }

    // Accumulate non-streaming events and metadata into the pending batch
    const pending = pendingSSEEventsRef.current
    const existing = pending.get(sid)
    if (existing) {
      if (nonStreamingEvents.length > 0) existing.events.push(...nonStreamingEvents)
      if (msg.session_status) existing.sessionStatus = msg.session_status
      if (msg.last_processed_index > existing.lastProcessedIndex) {
        existing.lastProcessedIndex = msg.last_processed_index
      }
      const msgAny = msg as unknown as Record<string, unknown>
      if (msgAny.has_more !== undefined) existing.hasMore = msgAny.has_more as boolean
      if (msg.has_running_background_agents !== undefined) existing.hasRunningBgAgents = msg.has_running_background_agents
      if (actualSessionId !== sid) existing.sessionId = actualSessionId
    } else {
      const msgAny = msg as unknown as Record<string, unknown>
      pending.set(sid, {
        events: nonStreamingEvents,
        sessionStatus: msg.session_status,
        lastProcessedIndex: msg.last_processed_index,
        hasMore: msgAny.has_more as boolean | undefined,
        hasRunningBgAgents: msg.has_running_background_agents,
        sessionId: actualSessionId,
      })
    }

    // Throttle flush to max every 200ms (batches all events arriving within the window).
    // Streaming text events are handled immediately above, so user-visible latency is unaffected.
    if (!flushRAFRef.current) {
      flushRAFRef.current = setTimeout(flushPendingSSEEvents, 200)
    }
  }, [flushPendingSSEEvents])

  // Handle SSE status-only messages (no events, just session status updates)
  const handleSSEStatus = useCallback((msg: SSEStatusMessage, sid: string) => {
    handleSSEMessage(
      { events: [], ...msg, last_processed_index: -1 } as SSEEventMessage,
      sid
    )
  }, [handleSSEMessage])

  // Cancel pending SSE flush on unmount
  useEffect(() => {
    return () => {
      if (flushRAFRef.current) {
        clearTimeout(flushRAFRef.current)
        flushRAFRef.current = 0
      }
    }
  }, [])

  // Polling function to get events for ALL active sessions (fallback when SSE unavailable)
  const pollEvents = useCallback(async () => {

    const chatStore = useChatStore.getState()

    // Read mode from store directly to avoid stale closure from setInterval capture
    const currentModeCategory = useModeStore.getState().selectedModeCategory

    // Get all tabs that should be polled (all tabs in current mode)
    const allTabs = Object.values(chatStore.chatTabs).filter(tab => {
      // If mode category is null (not yet selected), poll all non-workflow tabs
      if (!currentModeCategory) {
        return tab.metadata?.mode !== 'workflow'
      }
      return tab.metadata?.mode === currentModeCategory
    })
    
    // CRITICAL: Only poll tabs that are:
    // 1. Actively streaming (query in progress)
    // 2. Have session ID in backend's active sessions list (backend determines activity based on events)
    // 3. Multi-agent tabs (always poll — bg agents can produce events after orchestrator completes)
    // We don't poll completed sessions - they're done and won't have new events
    // We also don't poll uninitialized sessions (no query submitted yet)
    //
    // Read activeSessionIds fresh from the store to avoid stale closure from setInterval capture
    const freshActiveIds = new Set(chatStore.activeSessionsCache.map(s => s.session_id))
    const tabsToPoll = allTabs.filter(tab => {
      const currentTab = chatStore.getTab(tab.tabId)
      if (!currentTab?.sessionId) {
        return false
      }

      // Multi-agent tabs always get polled — bg agents can produce events
      // after the orchestrator completes (session_status='completed')
      if (currentTab.metadata?.mode === 'multi-agent') {
        return true
      }

      // Check if session is in backend's active sessions list (source of truth)
      // Backend determines activity based on event activity (10 min timeout)
      // CRITICAL: Also allow polling if tab is streaming (user just submitted a query)
      const isStreaming = currentTab.isStreaming
      const isInActiveSessions = freshActiveIds.has(currentTab.sessionId)

      // Allow polling if:
      // 1. Session is in backend's active sessions list, OR
      // 2. Tab is currently streaming (query just submitted)
      if (!isInActiveSessions && !isStreaming) {
        return false
      }

      // Skip if completed (definitely done) — unless background agents are still running
      if (currentTab.isCompleted && !currentTab.hasRunningBgAgents) {
        return false
      }

      return true
    })
    
    // CRITICAL: Poll by sessionId, not observerId
    // Multiple observers can view the same session, but events are stored per session
    const sessionsToPoll: Array<{ sessionId: string; tab: ChatTab | null }> = []
    
    // Add all tab sessions (deduplicate by sessionId)
    const seenSessionIds = new Set<string>()
    tabsToPoll.forEach(tab => {
      const currentTab = chatStore.getTab(tab.tabId)
      const sessionId = currentTab?.sessionId || tab.sessionId
      if (sessionId && !seenSessionIds.has(sessionId)) {
        seenSessionIds.add(sessionId)
        sessionsToPoll.push({ sessionId, tab: currentTab || tab })
      }
    })
    
    if (sessionsToPoll.length === 0) {
      return
    }
    
    // Poll each session
    for (const { sessionId, tab } of sessionsToPoll) {
      let currentTab = tab
      
      if (tab) {
        // Re-fetch the tab from store to ensure we have the latest session ID
        const fetchedTab = chatStore.getTab(tab.tabId)
        if (!fetchedTab) {
          continue
        }
        currentTab = fetchedTab
        
        // Verify session ID matches
        if (currentTab.sessionId !== sessionId) {
          // Use the new session ID
          if (!currentTab.sessionId) {
            continue
          }
        }
        
        // Double-check: verify this tab should still be polled
        // Only check isCompleted and sessionId - isStreaming is UI-only, not used for polling decisions
        if (currentTab.isCompleted && !currentTab.sessionId) {
          continue
        }
      }
      
      // Get fresh tab from store to ensure we have latest session ID
      const freshTab = currentTab ? chatStore.getTab(currentTab.tabId) : null
      const effectiveSessionId = freshTab?.sessionId || currentTab?.sessionId || sessionId
      
      let rawLastEventIndex = currentTab 
        ? getTabLastEventIndex(effectiveSessionId)
        : lastEventIndexRef.current
      
      // CRITICAL: Detect sentinel value (9999) which means "all events processed" but not an actual index
      // If lastEventIndex is 9999 or higher, check stored events to get the actual last index
      if (rawLastEventIndex >= 9999) {
        const storedEvents = getTabEvents(effectiveSessionId)
        if (storedEvents && storedEvents.length > 0) {
          const actualLastIndex = storedEvents.length - 1
          rawLastEventIndex = actualLastIndex
          // Update the stored index to the correct value
          if (currentTab) {
            setTabLastEventIndex(effectiveSessionId, actualLastIndex)
          } else {
            setLastEventIndex(actualLastIndex)
          }
        } else {
          // No stored events, but sentinel value - reset to 0 to start fresh
          rawLastEventIndex = 0
          if (currentTab) {
            setTabLastEventIndex(effectiveSessionId, 0)
          } else {
            setLastEventIndex(0)
          }
        }
      } else if (rawLastEventIndex === -1) {
        // Safety check: if index is -1 but we have events, use the event count
        // This prevents re-fetching from 0 if index state was lost but events exist
        const storedEvents = getTabEvents(effectiveSessionId)
        if (storedEvents && storedEvents.length > 0) {
          const actualLastIndex = storedEvents.length - 1
          rawLastEventIndex = actualLastIndex
          logger.debug('ChatArea', `Recovered lastEventIndex ${actualLastIndex} for session ${effectiveSessionId}`)
          
          if (currentTab) {
            setTabLastEventIndex(effectiveSessionId, actualLastIndex)
          }
        }
      }
      
      // Ensure lastEventIndex is >= 0 (API requirement)
      // -1 means "no events yet", which should be treated as 0
      const currentLastEventIndex = Math.max(0, rawLastEventIndex === -1 ? 0 : rawLastEventIndex)
      
      // Track which session is currently being polled (for derived isStreaming)

      try {
        const response = await agentApi.getSessionEvents(effectiveSessionId, currentLastEventIndex)

        // If response has a different session ID, update the tab
        if (currentTab && response.session_id && response.session_id !== effectiveSessionId) {
          chatStore.updateTabSessionId(currentTab.tabId, response.session_id)
        }

        processEventsResponse(response, effectiveSessionId, currentTab)
      } catch {
        // Continue polling other observers even if one fails
      }
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps -- selectedModeCategory read from store directly inside callback to avoid stale setInterval closure
  }, [getTabLastEventIndex, setTabLastEventIndex, setLastEventIndex, addTabEvents, getTabEvents, setIsStreaming, setIsCompleted, setHasActiveChat, activeSessionIds, processEventsResponse])


  
  // Start centralized active sessions polling when component mounts
  useEffect(() => {
    startActiveSessionsPolling()
    return () => {
      // Note: We don't stop polling here because other components might be using it
      // The polling will be managed globally and cleaned up when app unmounts
    }
  }, [startActiveSessionsPolling])

  // Unified page-load restore: handles both active sessions AND persisted tabs with no events.
  // Runs once per page load to avoid duplicate restores from separate effects racing each other.
  useEffect(() => {
    if (globalHasRestored) return
    // Only restore in chat / multi-agent modes (workflow handles its own restore)
    if (selectedModeCategory !== 'chat' && selectedModeCategory !== 'multi-agent') return

    const restoreAll = async () => {
      globalHasRestored = true

      try {
        // Wait for active-sessions polling to start and return initial data
        await new Promise(resolve => setTimeout(resolve, 500))

        // --- Phase 1: restore active / recently-completed sessions from backend ---
        const activeSessions = await getActiveSessions(true)
        const restoredSessionIds = new Set<string>()

        if (activeSessions.length > 0) {
          const runningSessions = activeSessions.filter(s => {
            if (s.agent_mode?.toLowerCase() === 'workflow') return false
            if (s.status === 'running') return true
            if (s.status === 'completed' && s.last_activity) {
              if (new Date(s.last_activity).getTime() > Date.now() - 30 * 60 * 1000) return true
            }
            return false
          })

          // Only restore sessions that have a persisted tab or are actively running
          const chatStore = useChatStore.getState()
          const persistedSessionIds = new Set(
            Object.values(chatStore.chatTabs)
              .filter(tab => tab.sessionId)
              .map(tab => tab.sessionId!)
          )
          const sessionsToRestore = runningSessions.filter(s =>
            persistedSessionIds.has(s.session_id) || s.status === 'running'
          )

          for (const activeSession of sessionsToRestore) {
            try {
              const tabId = await restoreSession(activeSession.session_id, {
                title: activeSession.query || 'Active Chat',
                source: 'auto-restore',
              })
              restoredSessionIds.add(activeSession.session_id)
              if (sessionsToRestore.indexOf(activeSession) === 0) {
                switchTab(tabId)
              }
            } catch (err) {
              console.error(`[SessionRestore] auto-restore failed for ${activeSession.session_id}:`, err)
            }
          }
        }

        // --- Phase 2: hydrate persisted tabs that Phase 1 didn't cover ---
        // (completed sessions from history that are in localStorage but have no events)
        const chatStore = useChatStore.getState()
        const tabs = Object.values(chatStore.chatTabs)
        for (const tab of tabs) {
          if (!tab.sessionId || tab.metadata?.mode === 'workflow') continue
          if (restoredSessionIds.has(tab.sessionId)) continue // already handled above
          const events = chatStore.getTabEvents(tab.sessionId)
          if (events.length > 0) continue
          try {
            await restoreSession(tab.sessionId, { source: 'page-refresh', skipConfigRestore: true })
          } catch (err) {
            console.error(`[SessionRestore] page-refresh hydrate failed for tab ${tab.tabId}:`, err)
          }
        }
      } catch (error) {
        console.error('[SessionRestore] page-load restore failed:', error)
      }
    }

    restoreAll()
  }, [getActiveSessions, switchTab, selectedModeCategory])

  // Only poll tabs that have their session ID in the backend's active sessions list
  // Backend determines activity based on event activity (10 min timeout)
  // CRITICAL: Also include tabs that are streaming (user just submitted a query)
  // This ensures restored sessions start polling immediately when replying
  const tabsWithActiveSessions = useMemo(() => {
    const activeIds = activeSessionIds // Capture in closure
    const chatStore = useChatStore.getState() // Get fresh store state to check streaming status
    
    const filtered = tabsWithSessions.filter(tab => {
      // Must have session ID
      if (!tab.sessionId) {
        return false
      }
      
      // Workflow tabs: keep SSE alive as long as the tab exists.
      // Workshop steps run in background goroutines after the main agent turn completes.
      // Their events (orchestrator_agent_end for sub-agents, learning, etc.) must still
      // reach the frontend so it can auto-send notifications to the main chat agent.
      // SSE is cleaned up when the tab is closed.
      if (tab.metadata?.mode === 'workflow') {
        return true
      }

      // Skip completed sessions (definitely done) — unless bg agents are still running
      // In multi-agent mode, always keep polling (background agents can restart the session)
      const freshTab = chatStore.getTab(tab.tabId)
      if (tab.isCompleted && !(freshTab?.hasRunningBgAgents) && tab.metadata?.mode !== 'multi-agent') {
        return false
      }

      // CRITICAL: Check streaming status directly from store (not from tab object)
      // This ensures we get the latest streaming status even if tabsWithSessions is stale
      const currentTab = chatStore.getTab(tab.tabId)
      const isStreaming = currentTab?.isStreaming ?? tab.isStreaming

      // CRITICAL: Include tabs that are streaming (user just submitted a query)
      // This handles the case where a restored session is being replied to
      // The backend might not have added it to active sessions yet, but we should poll it
      if (isStreaming) {
        return true
      }

      // Include tabs with running background agents (even if session is "completed")
      if (currentTab?.hasRunningBgAgents) {
        return true
      }

      // In multi-agent mode, always keep polling (background agents can restart session at any time)
      if (tab.metadata?.mode === 'multi-agent') {
        return true
      }

      // Must be in backend's active sessions list
      // If backend says it's active, poll it even if local isStreaming is false
      // This ensures we catch events that come after stop is pressed
      if (!activeIds.has(tab.sessionId)) {
        return false
      }

      return true
    })
    
    return filtered
    // PERF FIX: Removed `chatTabs` from dependencies. Previously this memo recomputed on
    // every setTabStreaming/setTabCompleted/setTabConfig because `chatTabs` changed reference.
    // The function already uses getState() for fresh tab data (lines above), so the memo
    // only needs to recompute when tabsWithSessions or activeSessionIds actually change.
  }, [tabsWithSessions, activeSessionIds])
  
  // SSE connection management — connect/disconnect based on active sessions
  // Falls back to polling if SSE connection fails (handled inside connectSSE's onError callback)
  // NOTE: sseConnections is intentionally NOT in the dependency array to avoid infinite loops
  // (connectSSE updates the store → sseConnections changes → effect re-fires → connectSSE again)
  useEffect(() => {
    // Read SSE state fresh from store (not from React state to avoid dep cycle)
    const currentSSE = useChatStore.getState().sseConnections

    // Determine which session IDs need SSE connections
    const neededSessionIds = new Set<string>()
    for (const tab of tabsWithActiveSessions) {
      if (tab.sessionId) neededSessionIds.add(tab.sessionId)
    }

    // Connect SSE for sessions that don't have a connection yet
    for (const tab of tabsWithActiveSessions) {
      if (!tab.sessionId) continue
      const sid = tab.sessionId
      if (currentSSE[sid]) continue // Already connected

      connectSSE(
        sid,
        (msg: SSEEventMessage) => handleSSEMessage(msg, sid),
        (msg: SSEStatusMessage) => handleSSEStatus(msg, sid)
      )
    }

    // Disconnect SSE for sessions that are no longer active
    for (const sid of Object.keys(currentSSE)) {
      if (!neededSessionIds.has(sid)) {
        disconnectSSE(sid)
      }
    }

    // Stop polling when no active sessions
    if (neededSessionIds.size === 0 && pollingInterval) {
      stopPolling()
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps -- sseConnections excluded to prevent infinite loop
  }, [tabsWithActiveSessions, connectSSE, disconnectSSE, handleSSEMessage, handleSSEStatus, pollingInterval, startPolling, stopPolling, pollEvents])

  // Cleanup polling and SSE on unmount
  useEffect(() => {
    return () => {
      // Disconnect all SSE connections
      disconnectAllSSE()
      // Use store's stopPolling to clean up
      if (pollingInterval) {
        stopPolling()
      }
    }
  }, [pollingInterval, stopPolling, disconnectAllSSE])
  

  const stopStreaming = useCallback(async () => {
    const chatStore = useChatStore.getState()
    
    // DO NOT stop polling - let backend determine activity based on events
    // Backend will mark session as inactive after 10 minutes of no events
    // This ensures we catch any pending events after stop is pressed
    
    // Update UI state only (isStreaming is UI-only, not used for polling decisions)
    setIsStreaming(false) // UI: Hide stop button, show send button
    
    // Update active tab's streaming status (UI feedback only)
    if (activeTab) {
      chatStore.setTabStreaming(activeTab.tabId, false) // UI: Hide stop button, show send button
    }

    // Call backend to stop the agent execution (preserves conversation history)
    // CRITICAL: Only use the active tab's session ID - never fall back to global sessionId
    // Falling back to global sessionId could stop a different tab's session
    const sessionIdToStop = activeTab?.sessionId
    if (!sessionIdToStop) {
      logger.warn('ChatArea', 'No session ID available for active tab')
      return
    }

    try {
      await agentApi.stopSession(sessionIdToStop, true)
    } catch (error) {
      logger.error('ChatArea', 'Failed to stop session:', error)
    }

    // Mark tab as completed so queued messages get auto-sent
    if (activeTab) {
      chatStore.setTabCompleted(activeTab.tabId, true)
    }

    // Deprecated: setLastEventCount removed
  }, [setIsStreaming, activeTab])

  // Store execution options for use in the request
  const executionOptionsRef = useRef<ExecutionOptions | undefined>(undefined)

  // Guard: prevent double submission from any source (Enter key repeat, double-click, effect race, etc.)
  const isSubmittingQueryRef = useRef(false)

  // Helper: reset streaming state (replaces 4 duplicated blocks)
  const resetStreamingState = useCallback((tabId?: string) => {
    const store = useChatStore.getState()
    store.setIsStreaming(false)
    store.setHasActiveChat(false)
    if (tabId) store.setTabStreaming(tabId, false)
  }, [])

  // Wrapper function to submit query with the current local query
  const submitQueryWithQuery = useCallback(async (query: string, executionOptions?: ExecutionOptions) => {
    // Prevent double submission: if already submitting, ignore
    if (isSubmittingQueryRef.current) {
      console.warn('[ChatArea] Blocked duplicate submitQueryWithQuery call', { query: query.substring(0, 50) })
      return
    }
    isSubmittingQueryRef.current = true
    // Reset after a short delay — long enough to block rapid duplicates,
    // short enough to allow the next legitimate send (e.g., queued messages after completion)
    setTimeout(() => { isSubmittingQueryRef.current = false }, 500)

    console.log('[ChatArea] submitQueryWithQuery called', { query: query.substring(0, 80), stack: new Error().stack?.split('\n').slice(1, 4).join(' <- ') })

    // Get fresh tab state from store to avoid stale closure issues
    const chatStore = useChatStore.getState()
    const freshActiveTab = activeTab?.tabId ? chatStore.chatTabs[activeTab.tabId] : activeTab

    executionOptionsRef.current = executionOptions

    // Early validation
    if (!query?.trim()) {
      logger.warn('ChatArea', 'Empty query, returning early')
      return
    }

    if (selectedModeCategory === 'workflow' && !isRequiredFolderSelected) {
      logger.error('ChatArea', 'Workflow folder required for workflow mode')
      return
    }

    // Resolve or create tab
    const resolved = await resolveOrCreateTab({ freshActiveTab, selectedModeCategory })
    if (!resolved) return
    const { tab: currentTab, sessionId: tabSessionId } = resolved

    // Build file context
    let effectiveFileContext: Array<{ name: string; path: string; type: 'file' | 'folder' }> = []
    if ((selectedModeCategory === 'chat' || selectedModeCategory === 'multi-agent') && currentTab?.config) {
      effectiveFileContext = currentTab.config.fileContext
    } else if (selectedModeCategory === 'workflow' && activeWorkflowPreset?.selectedFolder) {
      const folderPath = activeWorkflowPreset.selectedFolder.filepath
      effectiveFileContext = [{
        name: folderPath.split('/').pop() || folderPath,
        path: folderPath,
        type: (activeWorkflowPreset.selectedFolder.type || 'folder') as 'file' | 'folder'
      }]
    }

    const queryWithContext = effectiveFileContext.length > 0
      ? `${query.trim()}\n\n📁 Files in context: ${effectiveFileContext.map((file: { path: string }) => file.path).join(', ')}`
      : query.trim()

    // Decrypt selected secrets for payload (passed separately, never in query text)
    // Merge secrets from tab config (chat/multi-agent) and workflow preset
    let decryptedSecrets: Array<{ name: string; value: string }> | undefined
    const tabSecretIds = currentTab?.config?.selectedSecrets || []
    const presetSecretIds = (selectedModeCategory === 'workflow' && activeWorkflowPreset)
      ? ((activeWorkflowPreset as CustomPreset).selectedSecrets || [])
      : []
    const selectedSecretIds = [...new Set([...tabSecretIds, ...presetSecretIds])]
    if (selectedSecretIds.length > 0) {
      try {
        const secretsStore = useSecretsStore.getState()
        const secretsToInject = selectedSecretIds
          .map(id => secretsStore.getSecret(id))
          .filter((s): s is NonNullable<typeof s> => !!s)

        if (secretsToInject.length > 0) {
          decryptedSecrets = await Promise.all(
            secretsToInject.map(async (s) => {
              const { value } = await secretsApi.decrypt(s.encryptedValue)
              return { name: s.name, value }
            })
          )
        }
      } catch (err) {
        logger.error('ChatArea', 'Failed to decrypt secrets:', err)
      }
    }

    if (selectedModeCategory === 'workflow') {
      useAppStore.getState().setCurrentQuery(queryWithContext)
    }

    // Add user message event (without secrets for display safety)
    chatStore.addTabEvents(tabSessionId, [createUserMessageEvent(query.trim())])

    // Enable auto-scroll and scroll to bottom
    chatStore.setAutoScroll(true)
    setTimeout(() => { scrollToBottom('smooth') }, 50)

    // Clear query text
    useAppStore.getState().setCurrentQuery('')

    // Preserve final response as completion event if needed
    const eventsToCheck = chatStore.getTabEvents(tabSessionId)
    const hasCompletionEvent = eventsToCheck.some(event =>
      event.type === 'unified_completion' || event.type === 'agent_end'
    )
    if (finalResponse && !hasCompletionEvent) {
      const completionEvent: PollingEvent = {
        id: `completion-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`,
        type: 'unified_completion',
        timestamp: new Date().toISOString(),
        data: {
          unified_completion: {
            content: finalResponse,
            timestamp: new Date().toISOString()
          }
        } as PollingEvent['data']
      }
      chatStore.addTabEvents(tabSessionId, [completionEvent])
    }

    // Reset UI state for new query
    chatStore.setFinalResponse('')
    chatStore.setIsCompleted(false)
    chatStore.setIsStreaming(true)
    chatStore.setHasActiveChat(true)
    chatStore.setTabCompleted(currentTab.tabId, false)
    chatStore.setTabStreaming(currentTab.tabId, true)

    // Reset lastEventIndex so polling starts fresh from the in-memory event store
    // (critical when continuing a restored session — DB events have different indices than in-memory)
    chatStore.setTabLastEventIndex(tabSessionId, -1)

    // SSE connection is established in connectAfterRefresh below (after getActiveSessions)
    // Polling is only used as a fallback if SSE fails (handled by connectSSE's onError)

    processedCompletionEventsRef.current.clear()

    try {
      // Get active presets for the current mode
      const presetStore = useGlobalPresetStore.getState()
      const chatPreset = correctAgentMode === 'simple' ? presetStore.getActivePreset('chat') : null
      const workflowPreset = correctAgentMode === 'workflow' ? (selectedWorkflowPreset ? presetStore.getActivePreset('workflow') : null) : null
      const activePreset = workflowPreset || chatPreset

      const presetTools = activePreset?.selectedTools || []
      const filteredPresetTools = presetTools.filter(t => !t.endsWith(':*'))

      const chatPresetId = chatPreset?.id || null
      const workflowPresetId = workflowPreset?.id || null

      // Determine mode flags using helper
      const useCodeExecutionMode = determineModeFlag({
        correctAgentMode,
        selectedModeCategory: selectedModeCategory || '',
        presetValue: activePreset?.useCodeExecutionMode,
        tabConfigValue: currentTab?.config?.useCodeExecutionMode,
      })
      const useToolSearchMode = determineModeFlag({
        correctAgentMode,
        selectedModeCategory: selectedModeCategory || '',
        presetValue: activePreset?.useToolSearchMode,
        tabConfigValue: currentTab?.config?.useToolSearchMode,
      })

      // Build LLM config
      const isMultiAgentMode = selectedModeCategory === 'multi-agent'
      const llmStore = useLLMStore.getState()
      // For chat, multi-agent, and workflow phase chat: use tab's LLM if set (user may override)
      const isWorkflowPhaseChat = selectedModeCategory === 'workflow'
        && currentTab?.metadata?.phaseId
        && isChatCompatiblePhase(currentTab.metadata.phaseId)
      // For phase chat: prefer preset LLM if user hasn't explicitly overridden
      // (tab config always has a default from workflowPrimaryConfig, so we also check the preset)
      const phaseChatPreset = isWorkflowPhaseChat
        ? (presetStore.getActivePreset('workflow'))
        : null
      const presetLLMConfig = phaseChatPreset?.llmConfig?.provider && phaseChatPreset?.llmConfig?.model_id
        ? phaseChatPreset.llmConfig
        : null
      const baseLLMConfig = isWorkflowPhaseChat
        ? (currentTab?.config?.llmConfig || presetLLMConfig || llmStore.primaryConfig)
        : ((selectedModeCategory === 'chat' || isMultiAgentMode) && currentTab?.config?.llmConfig)
          ? currentTab.config.llmConfig
          : llmStore.primaryConfig
      const tierConfig = llmStore.delegationTierConfig
      const effectiveLLMConfig: ExtendedLLMConfiguration = (isMultiAgentMode && tierConfig?.main?.provider && tierConfig?.main?.model_id)
        ? { ...baseLLMConfig, provider: tierConfig.main.provider as ExtendedLLMConfiguration['provider'], model_id: tierConfig.main.model_id }
        : baseLLMConfig

      const llmConfigWithApiKeys = buildLLMConfigWithApiKeys(effectiveLLMConfig)

      // Compute effective plan phase for multi-agent mode (mirrors ChatInput logic)
      let effectivePlanPhase: string | undefined
      if (isMultiAgentMode) {
        const planPhaseOverride = currentTab?.config?.planPhaseOverride ?? null
        let autoDetectedPlanPhase: 'planning' | 'execution' | null = null
        const currentTabEvents = currentTab?.sessionId ? (tabEvents) : []
        for (let i = currentTabEvents.length - 1; i >= 0; i--) {
          const event = currentTabEvents[i]
          if (event.type === 'plan_approval') {
            autoDetectedPlanPhase = 'execution'
            break
          }
          if (event.type === 'tool_call_start' || event.type === 'tool_call_end') {
            const agentEvent = event.data as { data?: { tool_name?: string }; tool_name?: string } | undefined
            const toolName = agentEvent?.data?.tool_name || agentEvent?.tool_name
            if (toolName === 'create_delegation_plan') {
              autoDetectedPlanPhase = 'planning'
              break
            }
          }
        }
        effectivePlanPhase = planPhaseOverride ?? autoDetectedPlanPhase ?? 'planning'
      }

      // Build request payload
      const requestPayload = buildQueryRequestPayload({
        queryWithContext,
        correctAgentMode,
        selectedModeCategory,
        enabledTools,
        effectiveServers,
        currentTab,
        effectiveLLMConfig,
        llmConfigWithApiKeys,
        useCodeExecutionMode,
        useToolSearchMode,
        executionOptions: executionOptionsRef.current,
        workflowPresetId,
        chatPresetId,
        filteredPresetTools,
        hasActivePreset: !!activePreset,
        effectivePlanPhase,
        decryptedSecrets,
        selectedGlobalSecrets: (activePreset?.selectedGlobalSecretNames !== undefined ? activePreset.selectedGlobalSecretNames : useSecretsStore.getState().selectedGlobalSecretNames) ?? [],
      })

      // Validate execution groups for workflow mode
      if (correctAgentMode === 'workflow' && requestPayload.execution_options) {
        const validationError = validateExecutionGroups(requestPayload.execution_options)
        if (validationError) {
          chatStore.addToast(validationError, 'warning')
          resetStreamingState(currentTab.tabId)
          return
        }
      }

      // Set session ID and submit
      chatStore.setSessionId(tabSessionId)
      const response = await agentApi.startQuery(requestPayload, tabSessionId)

      if (response.status === 'started' || response.status === 'workflow_started') {
        const responseSessionId = response.session_id || response.query_id
        if (!responseSessionId) {
          logger.error('ChatArea', 'No sessionId in response')
          resetStreamingState(currentTab.tabId)
          return
        }

        chatStore.setSessionId(responseSessionId)
        chatStore.updateTabSessionId(currentTab.tabId, responseSessionId)
        chatStore.setTabStreaming(currentTab.tabId, true)
        chatStore.setTabCompleted(currentTab.tabId, false)

        // Reactivate historical session if needed
        const currentSessionState = useChatStore.getState().sessionState
        if (currentSessionState === 'completed' || currentSessionState === 'error') {
          chatStore.setSessionState('active')
        }

        // Refresh active sessions cache — SSE connection useEffect will pick up the new session
        const connectAfterRefresh = () => {
          const store = useChatStore.getState()
          const sid = responseSessionId
          // Connect SSE for the new session immediately
          if (!store.sseConnections[sid]) {
            connectSSE(
              sid,
              (msg: SSEEventMessage) => handleSSEMessage(msg, sid),
              (msg: SSEStatusMessage) => handleSSEStatus(msg, sid)
            )
          }
        }

        getActiveSessions(true)
          .then(connectAfterRefresh)
          .catch(error => {
            logger.error('ChatArea', 'Failed to refresh active sessions cache:', error)
            connectAfterRefresh()
          })
      } else {
        logger.error('ChatArea', 'Backend error:', response)
        resetStreamingState(currentTab.tabId)
      }
    } catch (error) {
      logger.error('ChatArea', 'Failed to submit query:', error)
      resetStreamingState(currentTab.tabId)
    }

  }, [correctAgentMode, selectedModeCategory, isRequiredFolderSelected, isStreaming, stopStreaming, finalResponse, startPolling, effectiveServers, enabledTools, selectedWorkflowPreset, activeWorkflowPreset, pollEvents, processedCompletionEventsRef, activeTab, scrollToBottom, getActiveSessions, resetStreamingState, connectSSE, handleSSEMessage, handleSSEStatus])

  // Auto-send queued messages when agent is idle (not streaming)
  const submitQueryWithQueryRef = useRef(submitQueryWithQuery)
  useEffect(() => { submitQueryWithQueryRef.current = submitQueryWithQuery }, [submitQueryWithQuery])

  useEffect(() => {
    const currentIsStreaming = activeTab?.isStreaming ?? false
    const queuedMessages = activeTab?.config?.queuedMessages || []

    // Read the shared lock from the store (fresh, not from closure) to prevent
    // multiple ChatArea instances from double-processing the same queue.
    const freshConfig = activeTab ? useChatStore.getState().getTabConfig(activeTab.tabId) : undefined
    const isProcessing = freshConfig?.isQueueProcessing ?? false

    console.log(`[QUEUE_DEBUG] tabId=${activeTab?.tabId} isStreaming=${currentIsStreaming} queueLength=${queuedMessages.length} isProcessing=${isProcessing}`)

    // Process queued messages when agent is idle (not streaming).
    // Uses !isStreaming instead of isCompleted because workshop step goroutines
    // may still be running in the background after the main agent turn finishes.
    if (currentIsStreaming || !activeTab || isProcessing || queuedMessages.length === 0) {
      return
    }

    const tabId = activeTab.tabId
    const chatStore = useChatStore.getState()

    // Claim the store-level lock atomically before any async work.
    // All ChatArea instances share this lock via the store.
    chatStore.setTabConfig(tabId, { isQueueProcessing: true })

    // Combine ALL queued messages into a single message so the agent
    // receives everything at once instead of one-at-a-time round-trips.
    const combinedMessage = queuedMessages.map(m => m.trim()).join('\n\n')

    // Clear the entire queue
    chatStore.setTabConfig(tabId, { queuedMessages: [] })

    // Small delay to ensure state is fully processed before sending
    setTimeout(async () => {
      try {
        await submitQueryWithQueryRef.current(combinedMessage)
      } catch (error) {
        logger.error('ChatArea', 'Failed to send queued messages:', error)
        // Re-add all messages back to the queue
        const currentChatStore = useChatStore.getState()
        const currentQueue = currentChatStore.getTabConfig(tabId)?.queuedMessages || []
        currentChatStore.setTabConfig(tabId, {
          queuedMessages: [...queuedMessages, ...currentQueue]
        })
        addToast('Failed to send queued messages. They have been re-queued.', 'error')
      } finally {
        // Release the lock after a delay to allow the new session to start streaming
        setTimeout(() => {
          useChatStore.getState().setTabConfig(tabId, { isQueueProcessing: false })
        }, 500)
      }
    }, 200)
  }, [activeTab?.isStreaming, activeTab?.config?.queuedMessages, activeTab?.config?.isQueueProcessing, activeTab?.tabId])

  // Handle new chat - clear backend session and reset all chat state
  const handleNewChat = useCallback(async () => {
    // Clear conversation history from backend first (if sessionId is available)
    const currentSessionId = getSessionId()
    const sessionIdToClear = activeTab?.sessionId || currentSessionId
    if (sessionIdToClear) {
      try {
        await agentApi.clearSession(sessionIdToClear)
      } catch (error) {
        logger.error('ChatArea', 'Failed to clear session:', error)
        // Continue with frontend reset even if backend clear fails
      }
    }
    
    // For workflow mode, preserve the selected preset but reset workflow phase
    if (selectedModeCategory === 'workflow' && selectedWorkflowPreset) {
      // Keep the preset selected, just reset the workflow phase to default
      const defaultPhase = useWorkflowStore.getState().getDefaultPhase()
      setCurrentWorkflowPhase(defaultPhase)
      // Don't clear selectedWorkflowPreset or currentWorkflowQueryId
    } else {
      // For other modes, clear workflow state completely
      clearWorkflowState()
    }
    
    // Reset frontend state
    resetChatState()
    
    // Clear queued messages if any
    if (activeTab) {
      const chatStore = useChatStore.getState()
      chatStore.setTabConfig(activeTab.tabId, { queuedMessages: [] })
      isProcessingQueueRef.current = false
    }
    
    // Explicitly reset events and tracking for new chat
    // Note: Using tabEvents now, not global events
    // Events are cleared when tab is removed/cleared
    setLastEventIndex(-1)
    processedCompletionEventsRef.current.clear()
    
    // Clear guidance state
    // Reset session ID for the active tab (will generate a new one on next query)
    resetSessionId()
    
    // Call the parent's new chat handler
    onNewChat()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [clearWorkflowState, resetChatState, onNewChat, activeTab?.sessionId, activeTab?.tabId, selectedModeCategory, selectedWorkflowPreset, setCurrentWorkflowPhase, setLastEventIndex])

  // Refresh workflow presets function
  const refreshWorkflowPresets = useCallback(async () => {
    if (workflowModeHandlerRef.current) {
      await workflowModeHandlerRef.current.refreshPresets()
    }
  }, [])

  // Expose methods to parent component
  useImperativeHandle(ref, () => ({
    handleNewChat,
    resetChatState,
    refreshWorkflowPresets,
    submitQuery: submitQueryWithQuery,
    getEvents: () => displayEvents,
    isStreaming,
    currentWorkflowPhase
  }), [handleNewChat, resetChatState, refreshWorkflowPresets, submitQueryWithQuery, displayEvents, isStreaming, currentWorkflowPhase])

  return (
    <div className="flex flex-col h-full min-w-0" data-testid="chat-area-container">
      {/* Preset Selection Overlay */}
      {showPresetSelection && pendingModeCategory && (
        <PresetSelectionOverlay
          isOpen={showPresetSelection}
          onClose={handlePresetSelectionClose}
          onPresetSelected={handlePresetSelected}
          modeCategory={pendingModeCategory}
          setCurrentQuery={setCurrentQuery}
        />
      )}

      {/* Mode Switch Dialog */}
      {showModeSwitchDialog && pendingModeSwitch && (
        <ModeSwitchDialog
          isOpen={showModeSwitchDialog}
          onCancel={handleModeSwitchCancel}
          onConfirm={handleModeSwitchConfirm}
          currentModeCategory={selectedModeCategory}
          newModeCategory={pendingModeSwitch}
        />
      )}



      {/* Chat Content - Separated to prevent input re-renders */}
      <div ref={chatContentRef} className={`flex-1 overflow-y-auto overflow-x-hidden min-w-0 relative overscroll-y-none ${compact ? 'text-sm' : ''}`} style={{ scrollBehavior: 'auto' }}>
        
        <div className={`min-w-0 ${compact ? 'px-2 pb-2' : 'px-4 pb-4'}`}>
          {/* Loading indicator for historical events */}
          {isLoadingHistory && (
            <div className={`flex items-center justify-center ${compact ? 'py-4' : 'py-8'}`}>
              <div className="flex items-center gap-3 text-gray-600 dark:text-gray-400">
                <div className={`${compact ? 'w-4 h-4' : 'w-5 h-5'} border-2 border-gray-300 dark:border-gray-600 border-t-blue-600 dark:border-t-blue-400 rounded-full animate-spin`}></div>
                <span className={compact ? 'text-xs' : 'text-sm'}>Loading chat history...</span>
              </div>
            </div>
          )}

          {/* Loading indicator for active session checking */}
          {isCheckingActiveSessions && (
            <div className={`flex items-center justify-center ${compact ? 'py-4' : 'py-8'}`}>
              <div className="flex items-center gap-3 text-gray-600 dark:text-gray-400">
                <div className={`${compact ? 'w-4 h-4' : 'w-5 h-5'} border-2 border-gray-300 dark:border-gray-600 border-t-green-600 dark:border-t-green-400 rounded-full animate-spin`}></div>
                <span className={compact ? 'text-xs' : 'text-sm'}>Checking for active session...</span>
              </div>
            </div>
          )}

          {/* Active session indicator */}
          {sessionState === 'active' && (
            <div className={`flex items-center justify-center ${compact ? 'py-2' : 'py-4'}`}>
              <div className={`flex items-center gap-2 ${compact ? 'px-2 py-1' : 'px-3 py-2'} bg-green-100 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-lg`}>
                <div className={`${compact ? 'w-1.5 h-1.5' : 'w-2 h-2'} bg-green-500 rounded-full animate-pulse`}></div>
                <span className={`${compact ? 'text-xs' : 'text-sm'} text-green-700 dark:text-green-300 font-medium`}>Live Session - Reconnected</span>
              </div>
            </div>
          )}

          {/* Session error indicator */}
          {sessionState === 'error' && (
            <div className={`flex items-center justify-center ${compact ? 'py-2' : 'py-4'}`}>
              <div className={`flex items-center gap-2 ${compact ? 'px-2 py-1' : 'px-3 py-2'} bg-red-100 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg`}>
                <svg className={`${compact ? 'w-3 h-3' : 'w-4 h-4'} text-red-600 dark:text-red-400`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
                <span className={`${compact ? 'text-xs' : 'text-sm'} text-red-700 dark:text-red-300 font-medium`}>Session Error - Unable to reconnect</span>
              </div>
            </div>
          )}

          {/* Show workflow explanation when in workflow mode but no preset selected */}
          {selectedModeCategory === 'workflow' && (
            <WorkflowExplanation agentMode={correctAgentMode} selectedWorkflowPreset={selectedWorkflowPreset} />
          )}


          {/* Show Deep Search explanation when in Deep Search mode */}


        {selectedModeCategory === 'workflow' ? (
          <WorkflowModeHandler
            ref={workflowModeHandlerRef}
            onPresetSelected={handleWorkflowPresetSelected}
            onPresetCleared={handleWorkflowPresetCleared}
            onWorkflowPhaseChange={setCurrentWorkflowPhase}
          >
            {/* Empty State - Show when no events and not in historical session */}
            {displayEvents.length === 0 && !isStreaming && !isChatCompatiblePhase(activeTab?.metadata?.phaseId) && (
              <ModeEmptyState modeCategory={selectedModeCategory} />
            )}
            {/* Phase Chat Help - Show for chat-compatible phases until AI has responded */}
            {!activeTab?.isStreaming && isChatCompatiblePhase(activeTab?.metadata?.phaseId) && !displayEvents.some(e => e.type === 'unified_completion' || e.type === 'agent_end' || e.type === 'llm_generation_end') && (
              <PhaseChatEmptyState phaseId={activeTab!.metadata!.phaseId!} />
            )}

            {activeTab?.sessionId && (
              <EventDisplay events={displayEvents} onFeedbackSubmitted={handleFeedbackSubmitted} onSendMessage={submitQueryWithQuery} compact={compact} flatHierarchy={true} sessionId={activeTab.sessionId} tabId={targetTabId || undefined} />
            )}
          </WorkflowModeHandler>
        ) : (
          <>
            {/* Empty State - Show when no events and not in historical session */}
            {displayEvents.length === 0 && !isStreaming && (
              <ModeEmptyState modeCategory={selectedModeCategory} />
            )}

            {activeTab?.sessionId && (
              <EventDisplay events={displayEvents} onFeedbackSubmitted={handleFeedbackSubmitted} onSendMessage={submitQueryWithQuery} compact={compact} sessionId={activeTab.sessionId} tabId={targetTabId || undefined} />
            )}
          </>
        )}
        </div>
      </div>

      {/* Input Area - Completely isolated from event updates, hidden in workflow mode */}
      {!hideInput && (
        <ChatInput
          onSubmit={submitQueryWithQuery}
          onStopStreaming={stopStreaming}
        />
      )}
      
      {/* Toast notifications */}
      <ToastContainer 
        toasts={filteredToasts} 
        onRemoveToast={removeToast} 
      />
    </div>
  )
})

ChatAreaInner.displayName = 'ChatAreaInner'

// Main ChatArea component
const ChatArea = ChatAreaInner

ChatArea.displayName = 'ChatArea'
ChatArea.whyDidYouRender = true

export default ChatArea

