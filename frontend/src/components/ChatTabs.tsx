import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { Plus, ArrowDown, ListTree, Terminal, Globe, DollarSign, CalendarClock } from 'lucide-react'
import { normalizeEventViewMode, useChatStore, type ChatTab } from '../stores/useChatStore'
import type { PollingEvent } from '../services/api-types'
import { useAppStore } from '../stores/useAppStore'
import { OrgPulseControl } from './OrgPulseControl'
import { OrgBackupPublishControls } from './org/OrgBackupPublishControls'
import { useModeStore } from '../stores/useModeStore'
import { Tooltip, TooltipContent, TooltipTrigger } from './ui/tooltip'
import { TreeViewAlphaDialog, shouldShowTreeViewAlphaWarning } from './TreeViewAlphaDialog'
import ServerSelectionDropdown from './ServerSelectionDropdown'
import SkillSelectionDropdown from './skills/SkillSelectionDropdown'
import { useMCPStore } from '../stores/useMCPStore'
import { dispatchChatToolCommand } from '../utils/chatToolEvents'
import CostDashboard from './CostDashboard'
import MultiAgentSchedulesPopup from './scheduler/MultiAgentSchedulesPopup'
import { schedulerApi } from '../api/scheduler'

interface ChatTabsProps {
  // For multi-agent mode: callback when starting a new chat (reset-in-place)
  onNewChat?: () => void
  // Auto-scroll state and toggle
  autoScroll?: boolean
  onToggleAutoScroll?: () => void
  onSubmitOrgCommand?: (query: string) => void
}

const DEDICATED_MCP_SERVERS = new Set(['playwright'])

// Mirror of ChatArea's user-message helpers so the header can derive the active
// conversation's title from its first real user message (kept local to avoid a
// cross-component import cycle).
const AUTO_NOTIFICATION_PREFIX = '[AUTO-NOTIFICATION]'
const RESTORED_CONVERSATION_CONTEXT_MARKER = '\n\nPrevious workflow-builder conversation file:'

function getUserMessageContent(event: PollingEvent): string {
  const agentEvent = event.data as Record<string, unknown> | undefined
  const innerData = agentEvent?.data as Record<string, unknown> | undefined
  const content = innerData?.content ?? agentEvent?.content
  return typeof content === 'string' ? content : ''
}

function getDisplaySafeUserMessageContent(content: string): string {
  const markerIndex = content.indexOf(RESTORED_CONVERSATION_CONTEXT_MARKER)
  return (markerIndex >= 0 ? content.slice(0, markerIndex) : content).trim()
}

// Short, single-line title from a user message — matches chatHistorySessionTitle's
// whitespace-collapse + length-cap style (default 80 chars).
function deriveConversationTitle(text: string, maxLength = 80): string {
  const normalized = text.replace(/\s+/g, ' ').trim()
  if (!normalized) return ''
  return normalized.length > maxLength ? `${normalized.slice(0, maxLength)}...` : normalized
}

// Multi-agent chat is single-tab: this bar is a slim header for the one chat
// tab (title + view controls + New Chat). It is not a tab switcher anymore.
// Workflow mode renders its own tabs (WorkflowChatTabs) inside the chat panel.
export const ChatTabs: React.FC<ChatTabsProps> = ({ onNewChat, autoScroll, onToggleAutoScroll, onSubmitOrgCommand }) => {
  const [pendingTreeViewTabId, setPendingTreeViewTabId] = useState<string | null>(null)
  const [showCostDashboard, setShowCostDashboard] = useState(false)
  const [showMultiAgentSchedules, setShowMultiAgentSchedules] = useState(false)
  const [multiAgentScheduleCount, setMultiAgentScheduleCount] = useState(0)
  const [multiAgentRunningScheduleCount, setMultiAgentRunningScheduleCount] = useState(0)
  const [multiAgentEnabledScheduleCount, setMultiAgentEnabledScheduleCount] = useState(0)
  const [multiAgentIssueScheduleCount, setMultiAgentIssueScheduleCount] = useState(0)
  const selectedModeCategory = useModeStore(state => state.selectedModeCategory)
  const showWorkflowsOverview = useAppStore(state => state.showWorkflowsOverview)
  const {
    chatTabs,
    activeTabId,
    switchTab,
    autoScroll: storeAutoScroll,
    setAutoScroll,
    setTabConfig,
    setTabViewMode,
  } = useChatStore()
  const { toolList: mcpToolList, setChatSelectedServers } = useMCPStore()

  const activeViewMode = useMemo(
    () => normalizeEventViewMode(activeTabId ? chatTabs[activeTabId]?.viewMode : undefined),
    [activeTabId, chatTabs]
  )
  const activeTab = activeTabId ? chatTabs[activeTabId] : undefined
  const showAutoScrollControl = activeViewMode === 'tree'

  // Reactively follow the active conversation's events so the header title can be
  // derived from the first real user message as the conversation starts.
  const activeSessionId = activeTab?.sessionId
  const activeSessionEvents = useChatStore(state =>
    activeSessionId ? state.tabEvents[activeSessionId] : undefined
  )
  const activeConversationTitle = useMemo(() => {
    if (!activeSessionEvents) return ''
    for (const event of activeSessionEvents) {
      if (event.type !== 'user_message') continue
      const content = getDisplaySafeUserMessageContent(getUserMessageContent(event))
      if (!content || content.startsWith(AUTO_NOTIFICATION_PREFIX)) continue
      return deriveConversationTitle(content)
    }
    return ''
  }, [activeSessionEvents])

  const isHiddenOrganizationTab = useCallback((tab: ChatTab) => {
    // Only hide tabs explicitly marked as org assistant via metadata.
    // Never match by tab name — that can hide normal chat tabs.
    return tab.metadata?.isOrganizationAssistant === true
  }, [])

  const availableServers = useMemo(
    () => [...new Set(
      mcpToolList
        .filter(tool => tool.status === 'ok')
        .map(tool => tool.server)
        .filter((server): server is string => typeof server === 'string' && !DEDICATED_MCP_SERVERS.has(server))
    )],
    [mcpToolList]
  )
  const manualSelectedServers = useMemo(
    () => activeTab?.config?.selectedServers || [],
    [activeTab?.config?.selectedServers]
  )
  const selectedSkills = useMemo(
    () => activeTab?.config?.selectedSkills || [],
    [activeTab?.config?.selectedSkills]
  )
  const browserMode = activeTab?.config?.browserMode || 'none'
  const toolsDisabled = !activeTabId || !!activeTab?.isStreaming || !!activeTab?.metadata?.isViewOnly

  const onManualServerToggle = useCallback((server: string) => {
    if (!activeTabId) return
    const serversWithoutNoServers = manualSelectedServers.filter(item => item !== 'NO_SERVERS')
    const newServers = serversWithoutNoServers.includes(server)
      ? serversWithoutNoServers.filter(item => item !== server)
      : [...serversWithoutNoServers, server]
    setTabConfig(activeTabId, { selectedServers: newServers })
    setChatSelectedServers(newServers)
  }, [activeTabId, manualSelectedServers, setChatSelectedServers, setTabConfig])

  const onSelectAllServers = useCallback(() => {
    if (!activeTabId) return
    setTabConfig(activeTabId, { selectedServers: availableServers })
    setChatSelectedServers(availableServers)
  }, [activeTabId, availableServers, setChatSelectedServers, setTabConfig])

  const onClearAllServers = useCallback(() => {
    if (!activeTabId) return
    setTabConfig(activeTabId, { selectedServers: ['NO_SERVERS'] })
    setChatSelectedServers(['NO_SERVERS'])
  }, [activeTabId, setChatSelectedServers, setTabConfig])

  const onSkillToggle = useCallback((skillFolderName: string) => {
    if (!activeTabId) return
    const newSkills = selectedSkills.includes(skillFolderName)
      ? selectedSkills.filter(item => item !== skillFolderName)
      : [...selectedSkills, skillFolderName]
    setTabConfig(activeTabId, { selectedSkills: newSkills })
  }, [activeTabId, selectedSkills, setTabConfig])

  const onSelectAllSkills = useCallback((allSkillNames: string[]) => {
    if (!activeTabId) return
    setTabConfig(activeTabId, { selectedSkills: allSkillNames })
  }, [activeTabId, setTabConfig])

  const onClearAllSkills = useCallback(() => {
    if (!activeTabId) return
    setTabConfig(activeTabId, { selectedSkills: [] })
  }, [activeTabId, setTabConfig])

  const browserTooltip = browserMode === 'none'
    ? 'Browser access'
    : browserMode === 'cdp'
      ? 'Browser access: CDP'
      : browserMode === 'playwright'
        ? 'Browser access: Playwright'
        : 'Browser access: Headless'

  // Use prop if provided, otherwise use store value
  const effectiveAutoScroll = autoScroll !== undefined ? autoScroll : storeAutoScroll
  const handleToggleAutoScroll = onToggleAutoScroll || (() => {
    setAutoScroll(!storeAutoScroll)
  })
  const requestViewMode = useCallback((tabId: string, mode: 'tree' | 'terminal') => {
    if (mode === 'tree' && activeViewMode !== 'tree' && shouldShowTreeViewAlphaWarning()) {
      setPendingTreeViewTabId(tabId)
      return
    }
    setTabViewMode(tabId, mode)
  }, [activeViewMode, setTabViewMode])

  const confirmTreeView = useCallback(() => {
    if (pendingTreeViewTabId) {
      setTabViewMode(pendingTreeViewTabId, 'tree')
    }
    setPendingTreeViewTabId(null)
  }, [pendingTreeViewTabId, setTabViewMode])

  useEffect(() => {
    if (selectedModeCategory !== 'multi-agent' || showWorkflowsOverview) {
      setMultiAgentScheduleCount(0)
      setMultiAgentRunningScheduleCount(0)
      setMultiAgentEnabledScheduleCount(0)
      setMultiAgentIssueScheduleCount(0)
      return
    }

    let cancelled = false

    const loadSchedules = async () => {
      try {
        const resp = await schedulerApi.listJobs({ mode: 'multi-agent' })
        if (cancelled) return

        const jobs = resp.jobs ?? []
        const now = Date.now()
        const issueCount = jobs.filter(job => {
          if (job.last_status === 'error') return true
          if (!job.enabled || !job.next_run_at) return false
          const nextRunAt = Date.parse(job.next_run_at)
          return Number.isFinite(nextRunAt) && now - nextRunAt > 60_000
        }).length
        setMultiAgentScheduleCount(jobs.length)
        setMultiAgentRunningScheduleCount(jobs.filter(job => job.last_status === 'running').length)
        setMultiAgentEnabledScheduleCount(jobs.filter(job => job.enabled).length)
        setMultiAgentIssueScheduleCount(issueCount)
      } catch {
        if (cancelled) return
        setMultiAgentScheduleCount(0)
        setMultiAgentRunningScheduleCount(0)
        setMultiAgentEnabledScheduleCount(0)
        setMultiAgentIssueScheduleCount(0)
      }
    }

    void loadSchedules()
    const interval = window.setInterval(loadSchedules, 15000)

    return () => {
      cancelled = true
      window.clearInterval(interval)
    }
  }, [selectedModeCategory, showWorkflowsOverview, showMultiAgentSchedules])

  const multiAgentScheduleStatusDotClass = multiAgentIssueScheduleCount > 0
    ? 'bg-red-500'
    : multiAgentRunningScheduleCount > 0
      ? 'bg-green-500'
      : multiAgentEnabledScheduleCount > 0
        ? 'bg-amber-500'
        : 'bg-muted-foreground/50'

  const multiAgentScheduleTooltip = useMemo(() => {
    if (multiAgentScheduleCount === 0) return 'No scheduled Chief of Staff tasks'
    if (multiAgentIssueScheduleCount > 0) {
      return `${multiAgentIssueScheduleCount} Chief of Staff schedule issue${multiAgentIssueScheduleCount === 1 ? '' : 's'}`
    }
    if (multiAgentRunningScheduleCount > 0) {
      return `${multiAgentRunningScheduleCount} active Chief of Staff schedule${multiAgentRunningScheduleCount === 1 ? '' : 's'}`
    }
    if (multiAgentEnabledScheduleCount > 0) {
      return `${multiAgentEnabledScheduleCount} active of ${multiAgentScheduleCount} scheduled Chief of Staff task${multiAgentScheduleCount === 1 ? '' : 's'}`
    }
    return `${multiAgentScheduleCount} paused Chief of Staff schedule${multiAgentScheduleCount === 1 ? '' : 's'}`
  }, [
    multiAgentEnabledScheduleCount,
    multiAgentIssueScheduleCount,
    multiAgentRunningScheduleCount,
    multiAgentScheduleCount,
  ])

  // Auto-select the single multi-agent tab if none is active (e.g. after refresh)
  useEffect(() => {
    if (selectedModeCategory !== 'multi-agent' || showWorkflowsOverview) return

    if (activeTabId) {
      const activeTab = chatTabs[activeTabId]
      if (
        activeTab &&
        activeTab.metadata?.mode === 'multi-agent' &&
        !isHiddenOrganizationTab(activeTab)
      ) {
        return
      }
    }

    const visibleTabs = Object.values(chatTabs).filter(tab =>
      tab.metadata?.mode === 'multi-agent' && !isHiddenOrganizationTab(tab)
    )
    if (visibleTabs.length > 0) {
      const sorted = [...visibleTabs].sort((a, b) => (a.createdAt || 0) - (b.createdAt || 0))
      switchTab(sorted[0].tabId)
    }
  }, [activeTabId, chatTabs, selectedModeCategory, showWorkflowsOverview, switchTab, isHiddenOrganizationTab])

  // Only render in multi-agent mode (workflow tabs live in WorkflowChatTabs).
  const shouldShowHeader = selectedModeCategory === 'multi-agent' && !showWorkflowsOverview
  if (!shouldShowHeader) {
    return null
  }

  const showHeaderContent =
    !!activeTab && activeTab.metadata?.mode === 'multi-agent' && !isHiddenOrganizationTab(activeTab)
  // The multi-agent chat is the user's "chief of staff" (single-tab — this is a
  // header label, not a tab switcher). Priority: resumed conversation's title →
  // the active conversation's first-user-message title → the chief-of-staff label.
  const chatTitle = (showHeaderContent
    ? (activeTab?.config?.restoredConversationTitle?.trim() || activeConversationTitle)
    : '') || 'Chief of Staff'

  return (
    <>
    <div className="relative flex-shrink-0 flex items-center gap-2 bg-gray-50 dark:bg-gray-800 px-3 py-1.5 border-b border-gray-200 dark:border-gray-700">
      {/* Single-chat title */}
      <span className="min-w-0 max-w-[min(360px,34vw)] truncate whitespace-nowrap text-sm font-medium text-gray-900 dark:text-gray-100">
        {chatTitle}
      </span>

      {/* New Chat — resets the current chat in place (confirmation handled upstream) */}
      {onNewChat && (
        <button
          onClick={onNewChat}
          data-testid="new-chat-button"
          className="flex flex-none items-center gap-1 rounded px-2 py-1 text-xs text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-gray-200"
          title="New chat — clears the current conversation and starts a fresh session"
        >
          <Plus className="w-4 h-4" />
          <span>New Chat</span>
        </button>
      )}

      {/* View controls live next to the Chief of Staff title, matching workflow. */}
      <div className="flex flex-none items-center gap-1 border-l border-gray-200 pl-2 dark:border-gray-700">
        <div
          data-tour="event-view-mode"
          data-testid="tour-event-view-mode"
          className="inline-flex items-center rounded-full border border-gray-200 bg-gray-100 p-0.5 dark:border-gray-700 dark:bg-gray-800"
          role="group"
          aria-label="Event layout mode"
        >
          {([
            { mode: 'tree' as const, Icon: ListTree, label: 'Tree', tip: 'Tree view — group events by agent' },
            { mode: 'terminal' as const, Icon: Terminal, label: 'Terminal', tip: 'Terminal view — show only the terminal panes, no events' },
          ]).map(({ mode, Icon, label, tip }) => (
            <Tooltip key={mode}>
              <TooltipTrigger asChild>
                <button
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation()
                    if (activeTabId) {
                      requestViewMode(activeTabId, mode)
                    }
                  }}
                  className={`flex h-6 w-6 items-center justify-center rounded-full transition-colors ${
                    activeViewMode === mode
                      ? 'bg-blue-600 text-white shadow-sm'
                      : 'text-gray-500 hover:bg-gray-200 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-gray-100'
                  }`}
                  aria-label={label}
                  aria-pressed={activeViewMode === mode}
                >
                  <Icon className="h-3.5 w-3.5" />
                </button>
              </TooltipTrigger>
              <TooltipContent>
                <p>{tip}</p>
              </TooltipContent>
            </Tooltip>
          ))}
        </div>
        {handleToggleAutoScroll && showAutoScrollControl && (
          <button
            onClick={handleToggleAutoScroll}
            className={`
              flex items-center gap-1.5 px-2 py-1 rounded text-xs transition-colors
              ${effectiveAutoScroll
                ? 'text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700'
                : 'text-gray-500 dark:text-gray-500 hover:bg-gray-100 dark:hover:bg-gray-700'
              }
            `}
          >
            <ArrowDown className={`w-3.5 h-3.5 ${effectiveAutoScroll ? 'opacity-70' : 'opacity-40'}`} />
            <span className="hidden sm:inline">
              {effectiveAutoScroll ? 'Auto-scroll' : 'Manual'}
            </span>
          </button>
        )}
      </div>

      <div className="ml-auto flex items-center gap-1">
        {showHeaderContent && (
          <div className="mr-1 flex items-center gap-1 border-r border-gray-200 pr-2 dark:border-gray-700">
            <ServerSelectionDropdown
              availableServers={availableServers}
              selectedServers={manualSelectedServers}
              onServerToggle={onManualServerToggle}
              onSelectAll={onSelectAllServers}
              onClearAll={onClearAllServers}
              disabled={toolsDisabled}
              openDirection="down"
              align="right"
              iconOnly
            />
            <SkillSelectionDropdown
              selectedSkills={selectedSkills}
              onSkillToggle={onSkillToggle}
              onSelectAll={onSelectAllSkills}
              onClearAll={onClearAllSkills}
              disabled={toolsDisabled}
              openDirection="down"
              align="right"
              iconOnly
            />
            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  type="button"
                  onClick={() => dispatchChatToolCommand('browser')}
                  disabled={toolsDisabled}
                  className={`relative flex h-7 w-7 items-center justify-center rounded-md border transition-colors disabled:cursor-not-allowed disabled:opacity-50 ${
                    browserMode !== 'none'
                      ? 'border-blue-400 bg-blue-100 text-blue-600 hover:bg-blue-200 dark:border-blue-700 dark:bg-blue-900/40 dark:text-blue-300 dark:hover:bg-blue-900/60'
                      : 'border-gray-300 bg-gray-100 text-gray-400 hover:bg-gray-200 hover:text-gray-700 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-500 dark:hover:bg-gray-700 dark:hover:text-gray-200'
                  }`}
                  aria-label={browserTooltip}
                >
                  <Globe className="h-4 w-4" />
                  {browserMode !== 'none' && (
                    <span className="absolute -right-0.5 -top-0.5 h-2 w-2 rounded-full border border-gray-50 bg-blue-500 dark:border-gray-800" />
                  )}
                </button>
              </TooltipTrigger>
              <TooltipContent>
                <p>{browserTooltip}</p>
              </TooltipContent>
            </Tooltip>
          </div>
        )}

        <Tooltip>
          <TooltipTrigger asChild>
            <button
              type="button"
              onClick={() => setShowCostDashboard(true)}
              className="rounded-md bg-muted p-1.5 text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
              aria-label="LLM costs"
            >
              <DollarSign className="h-3.5 w-3.5" />
            </button>
          </TooltipTrigger>
          <TooltipContent>
            <p>LLM costs</p>
          </TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              type="button"
              onClick={() => setShowMultiAgentSchedules(true)}
              className={`relative rounded-md bg-muted p-1.5 transition-colors hover:bg-accent ${
                multiAgentIssueScheduleCount > 0
                  ? 'text-red-600 dark:text-red-400'
                  : multiAgentRunningScheduleCount > 0
                    ? 'text-green-600 dark:text-green-400'
                    : 'text-muted-foreground hover:text-accent-foreground'
              }`}
              aria-label="Scheduled Chief of Staff tasks"
            >
              <CalendarClock className="h-3.5 w-3.5" />
              {multiAgentScheduleCount > 0 && (
                <span className={`absolute right-1 top-1 h-1.5 w-1.5 rounded-full ${multiAgentScheduleStatusDotClass}`} />
              )}
            </button>
          </TooltipTrigger>
          <TooltipContent>
            <p>{multiAgentScheduleTooltip}</p>
          </TooltipContent>
        </Tooltip>
        <OrgPulseControl />
        <OrgBackupPublishControls onSubmitCommand={onSubmitOrgCommand} />
      </div>

      </div>
      <CostDashboard
        isOpen={showCostDashboard}
        onClose={() => setShowCostDashboard(false)}
      />
      {showMultiAgentSchedules && (
        <MultiAgentSchedulesPopup onClose={() => setShowMultiAgentSchedules(false)} />
      )}
      <TreeViewAlphaDialog
        isOpen={pendingTreeViewTabId !== null}
        onContinue={confirmTreeView}
        onCancel={() => setPendingTreeViewTabId(null)}
      />
    </>
  )
}
