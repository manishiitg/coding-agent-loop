import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { Plus, ArrowDown, ListTree, Terminal, History, X, Globe } from 'lucide-react'
import { normalizeEventViewMode, useChatStore, type ChatTab } from '../stores/useChatStore'
import { useAppStore } from '../stores/useAppStore'
import { OrgPulseControl } from './OrgPulseControl'
import { useModeStore } from '../stores/useModeStore'
import { Tooltip, TooltipContent, TooltipTrigger } from './ui/tooltip'
import { TreeViewAlphaDialog, shouldShowTreeViewAlphaWarning } from './TreeViewAlphaDialog'
import { PreviousChatHistoryPanel } from './PreviousChatHistoryPanel'
import { useResumePreviousChat } from '../hooks/useResumePreviousChat'
import type { ChatHistorySession } from '../services/api-types'
import ServerSelectionDropdown from './ServerSelectionDropdown'
import SkillSelectionDropdown from './skills/SkillSelectionDropdown'
import { useMCPStore } from '../stores/useMCPStore'
import { dispatchChatToolCommand } from '../utils/chatToolEvents'

interface ChatTabsProps {
  // For multi-agent mode: callback when starting a new chat (reset-in-place)
  onNewChat?: () => void
  // Auto-scroll state and toggle
  autoScroll?: boolean
  onToggleAutoScroll?: () => void
}

const DEDICATED_MCP_SERVERS = new Set(['playwright'])

// Multi-agent chat is single-tab: this bar is a slim header for the one chat
// tab (title + view controls + New Chat). It is not a tab switcher anymore.
// Workflow mode renders its own tabs (WorkflowChatTabs) inside the chat panel.
export const ChatTabs: React.FC<ChatTabsProps> = ({ onNewChat, autoScroll, onToggleAutoScroll }) => {
  const [pendingTreeViewTabId, setPendingTreeViewTabId] = useState<string | null>(null)
  const [showHistory, setShowHistory] = useState(false)
  const resumePreviousChat = useResumePreviousChat()
  const handleSelectHistory = useCallback(async (session: ChatHistorySession) => {
    await resumePreviousChat(session)
    setShowHistory(false)
  }, [resumePreviousChat])
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
  // header label, not a tab switcher). Show the resumed conversation's title when
  // a previous chat is open, otherwise the chief-of-staff label.
  const chatTitle = (showHeaderContent ? activeTab?.config?.restoredConversationTitle?.trim() : '') || 'Chief of Staff'

  return (
    <>
    <div className="relative flex-shrink-0 flex items-center gap-2 bg-gray-50 dark:bg-gray-800 px-3 py-1.5 border-b border-gray-200 dark:border-gray-700">
      {/* Single-chat title */}
      <span className="text-sm font-medium text-gray-900 dark:text-gray-100 whitespace-nowrap truncate">
        {chatTitle}
      </span>

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

        <OrgPulseControl />

        {/* History — open the previous-chats list to resume an earlier chat
            without first clearing the current one. */}
        <button
          onClick={() => setShowHistory(v => !v)}
          data-testid="chat-history-button"
          aria-expanded={showHistory}
          className={`flex items-center gap-1 px-2 py-1 text-xs rounded transition-colors ${
            showHistory
              ? 'text-gray-700 dark:text-gray-200 bg-gray-100 dark:bg-gray-700'
              : 'text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-700'
          }`}
          title="Previous chats — resume an earlier conversation"
        >
          <History className="w-4 h-4" />
          <span className="hidden sm:inline">History</span>
        </button>

        {/* New Chat — resets the current chat in place (confirmation handled upstream) */}
        {onNewChat && (
          <button
            onClick={() => { setShowHistory(false); onNewChat() }}
            data-testid="new-chat-button"
            className="flex items-center gap-1 px-2 py-1 mr-1 text-xs text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-700 rounded transition-colors"
            title="New chat — clears the current conversation and starts a fresh session"
          >
            <Plus className="w-4 h-4" />
            <span>New Chat</span>
          </button>
        )}

        {/* Right-side view controls */}
        <div className="flex items-center gap-1 border-l border-gray-200 dark:border-gray-700 pl-2">
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
          {handleToggleAutoScroll && activeViewMode !== 'terminal' && (
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
        </div>

        {showHistory && (
          <>
            {/* Click-away backdrop */}
            <div className="fixed inset-0 z-40" onClick={() => setShowHistory(false)} aria-hidden />
            <div
              className="absolute right-2 top-[calc(100%+4px)] z-50 flex max-h-[70vh] w-[360px] max-w-[calc(100vw-1rem)] flex-col overflow-hidden rounded-lg border border-gray-200 bg-white shadow-xl dark:border-gray-700 dark:bg-gray-900"
              role="dialog"
              aria-label="Previous chats"
            >
              <div className="flex shrink-0 items-center justify-between border-b border-gray-100 px-3 py-2 dark:border-gray-800">
                <span className="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Previous chats</span>
                <button
                  onClick={() => setShowHistory(false)}
                  className="text-gray-400 transition-colors hover:text-gray-600 dark:hover:text-gray-200"
                  aria-label="Close previous chats"
                >
                  <X className="h-4 w-4" />
                </button>
              </div>
              <div className="min-h-0 flex-1 overflow-auto">
                <PreviousChatHistoryPanel
                  activeSessionId={activeTab?.sessionId ?? undefined}
                  title="Previous chats"
                  actionLabel="Resume"
                  emptyText="No previous chats yet."
                  onSelectSession={handleSelectHistory}
                  compact
                />
              </div>
            </div>
          </>
        )}
      </div>
      <TreeViewAlphaDialog
        isOpen={pendingTreeViewTabId !== null}
        onContinue={confirmTreeView}
        onCancel={() => setPendingTreeViewTabId(null)}
      />
    </>
  )
}
