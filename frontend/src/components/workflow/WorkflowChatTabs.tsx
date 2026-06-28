import React, { useMemo, useEffect, useCallback, useRef, useState } from 'react'
import { useShallow } from 'zustand/react/shallow'
import { ArrowDown, ListTree, MessageSquare, Plus, Square, Terminal, X } from 'lucide-react'
import { normalizeEventViewMode, useChatStore, type ChatTab } from '../../stores/useChatStore'
import { useSessionExecutionTree } from '../../hooks/useSessionExecutionTree'
import { agentApi } from '../../services/api'
import { activateTab } from '../../utils/activateTab'
import { useWorkflowStore } from '../../stores/useWorkflowStore'
import { useGlobalPresetStore } from '../../stores/useGlobalPresetStore'
import { Tooltip, TooltipContent, TooltipTrigger } from '../ui/tooltip'
import { TreeViewAlphaDialog, shouldShowTreeViewAlphaWarning } from '../TreeViewAlphaDialog'
import LlmTriggerButton from '../topbar/LlmTriggerButton'

// ---------------------------------------------------------------------------
// WorkflowTabItem — per-tab component with narrow store subscriptions
// ---------------------------------------------------------------------------

interface WorkflowTabItemProps {
  tab: ChatTab
  isActive: boolean
  canClose: boolean
  onTabClick: (tabId: string) => void
  onCloseTab: (tabId: string) => void
  onMakeInteractive: (tabId: string) => void
  onStop: (tabId: string) => void
}

// Per-tab live status — mirrors the backend's consolidated busy/idle/stopped
// (sessionDisplayStatus). The dot lives in the tab pill instead of the toolbar.
const TAB_STATUS_DOT: Record<'busy' | 'idle' | 'stopped', { cls: string; label: string }> = {
  busy: { cls: 'bg-[hsl(var(--info))] animate-pulse', label: 'Busy' },
  idle: { cls: 'bg-[hsl(var(--success))]', label: 'Idle' },
  stopped: { cls: 'bg-muted-foreground/60', label: 'Stopped' },
}

const WorkflowTabItem = React.memo<WorkflowTabItemProps>(({
  tab,
  isActive,
  canClose,
  onTabClick,
  onCloseTab,
  onMakeInteractive,
  onStop,
}) => {
  const displayName = tab.metadata?.phaseId === 'workflow-builder' && tab.name === 'Automation Builder'
    ? 'Chat'
    : tab.name

  // Pull this tab's own backend status (only polls while busy). Combine with the
  // local streaming flags so "busy" shows immediately, before the tree catches up.
  const { data: execTree } = useSessionExecutionTree(tab.sessionId, !!tab.sessionId)
  const treeStatus = execTree?.summary.display_status
  const status: 'busy' | 'idle' | 'stopped' =
    tab.isStreaming || tab.hasRunningBgAgents || treeStatus === 'busy'
      ? 'busy'
      : treeStatus === 'stopped'
        ? 'stopped'
        : 'idle'
  const dot = TAB_STATUS_DOT[status]
  const isBusy = status === 'busy'

  return (
    <div
      onClick={() => onTabClick(tab.tabId)}
      onKeyDown={(e) => e.key === 'Enter' && onTabClick(tab.tabId)}
      role="button"
      tabIndex={0}
      className={`
        group flex min-w-0 items-center gap-1.5 px-2 py-1 rounded-t-md text-xs font-medium transition-colors cursor-pointer outline-none
        ${isActive
          ? 'bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 border-b-2 border-blue-500'
          : 'text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700 hover:text-gray-900 dark:hover:text-gray-100'
        }
      `}
    >
      {/* Live status dot (busy/idle/stopped) */}
      <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${dot.cls}`} title={dot.label} aria-label={dot.label} />

      {/* Tab Name */}
      <span className="min-w-0 max-w-[14rem] truncate whitespace-nowrap">{displayName}</span>

      {/* In-tab Stop — only while this tab is busy */}
      {isBusy && tab.sessionId && (
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation()
            onStop(tab.tabId)
          }}
          className="ml-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded text-[hsl(var(--destructive))] opacity-70 transition-colors hover:bg-[hsl(var(--destructive)/0.12)] hover:opacity-100"
          aria-label={`Stop ${displayName}`}
          title="Stop this session"
        >
          <Square className="h-2.5 w-2.5" fill="currentColor" />
        </button>
      )}

      {/* Convert a read-only scheduled/bot run into an interactive Automation Builder chat */}
      {tab.metadata?.isViewOnly && (tab.metadata?.isScheduledRun || tab.metadata?.isBotRun) && (
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation()
            onMakeInteractive(tab.tabId)
          }}
          className="ml-0.5 rounded p-0.5 text-blue-600 opacity-80 hover:bg-blue-100 hover:opacity-100 dark:text-blue-300 dark:hover:bg-blue-900/40"
          title="Interact in Automation Builder"
          aria-label="Interact in Automation Builder"
        >
          <MessageSquare className="w-3 h-3" />
        </button>
      )}

      {canClose && (
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation()
            onCloseTab(tab.tabId)
          }}
          className="ml-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded text-gray-400 transition-colors hover:bg-gray-200 hover:text-gray-700 dark:hover:bg-gray-700 dark:hover:text-gray-200"
          aria-label={`Close ${displayName}`}
          title="Close tab"
        >
          <X className="h-3 w-3" />
        </button>
      )}
    </div>
  )
})

WorkflowTabItem.displayName = 'WorkflowTabItem'

// ---------------------------------------------------------------------------
// WorkflowChatTabs — parent component
// ---------------------------------------------------------------------------

/**
 * Mini ChatTabs component for workflow mode chat area
 * Only shows workflow tabs that are active (have sessionId or isStreaming)
 */
interface WorkflowChatTabsProps {
  onNewChat?: () => void
  // When true, render inline (no bordered/background bar wrapper) so the strip can
  // be embedded inside the WorkflowToolbar row instead of being its own bar.
  embedded?: boolean
}

export const WorkflowChatTabs: React.FC<WorkflowChatTabsProps> = ({ onNewChat, embedded = false }) => {
  const [pendingTreeViewTabId, setPendingTreeViewTabId] = useState<string | null>(null)
  const {
    chatTabs,
    activeTabId,
    closeTab,
    autoScroll,
    setAutoScroll,
    setTabViewMode,
    setTabStreaming,
    setTabHasRunningBgAgents,
  } = useChatStore(useShallow(state => ({
    chatTabs: state.chatTabs,
    activeTabId: state.activeTabId,
    closeTab: state.closeTab,
    autoScroll: state.autoScroll,
    setAutoScroll: state.setAutoScroll,
    setTabViewMode: state.setTabViewMode,
    setTabStreaming: state.setTabStreaming,
    setTabHasRunningBgAgents: state.setTabHasRunningBgAgents,
  })))

  const setShowChatArea = useWorkflowStore(state => state.setShowChatArea)

  const activeViewMode = useChatStore(state => {
    const tab = activeTabId ? state.chatTabs[activeTabId] : null
    return normalizeEventViewMode(tab?.viewMode)
  })
  const rawActiveViewMode = useChatStore(state => activeTabId ? state.chatTabs[activeTabId]?.viewMode : undefined)
  const activePresetId = useGlobalPresetStore(state => state.activePresetIds.workflow)
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

  // Filter to workflow tabs for the active preset, but always keep the active
  // workflow tab visible. Scheduled-run restores can briefly lack a preset match
  // while the tab is being created/switched, and hiding the active tab makes the
  // restore look like it failed.
  const activeWorkflowTabs = useMemo(() => {
    const allTabs = Object.values(chatTabs)
    const shouldShowViewOnlyTab = (tab: ChatTab) =>
      !tab.metadata?.isViewOnly || tab.tabId === activeTabId || tab.isStreaming || tab.hasRunningBgAgents
    const matched = allTabs.filter(tab =>
      tab.metadata?.mode === 'workflow' &&
      tab.metadata.presetQueryId === activePresetId &&
      shouldShowViewOnlyTab(tab)
    )
    const activeTab = activeTabId ? chatTabs[activeTabId] : undefined
    const activeWorkflowTab = activeTab?.metadata?.mode === 'workflow' ? activeTab : undefined
    const isBuilderTab = (tab: ChatTab) => tab.metadata?.phaseId === 'workflow-builder'
    const chooseVisibleBuilder = (tabs: ChatTab[]) => [...tabs].sort((a, b) => {
      if (a.tabId === activeTabId) return -1
      if (b.tabId === activeTabId) return 1
      if (a.isStreaming !== b.isStreaming) return a.isStreaming ? -1 : 1
      return b.createdAt - a.createdAt
    })[0]

    const matchedBuilders = matched.filter(isBuilderTab)
    const visibleBuilder = chooseVisibleBuilder(matchedBuilders)
    const visibleMatched = matched.filter(tab => !isBuilderTab(tab) || tab.tabId === visibleBuilder?.tabId)
    const hasPresetBuilder = Boolean(visibleBuilder)

    const visibleById = new Map<string, ChatTab>()
    visibleMatched.forEach(tab => visibleById.set(tab.tabId, tab))
    if (activeWorkflowTab) {
      const isDuplicateBuilder =
        isBuilderTab(activeWorkflowTab) &&
        hasPresetBuilder &&
        activeWorkflowTab.metadata?.presetQueryId !== activePresetId

      if (!isDuplicateBuilder) {
        visibleById.set(activeWorkflowTab.tabId, activeWorkflowTab)
      }
    }

    const visible = visibleById.size > 0
      ? Array.from(visibleById.values())
      : allTabs.filter(tab =>
          tab.metadata?.mode === 'workflow' &&
          tab.metadata?.phaseId === 'workflow-builder' &&
          !tab.metadata?.presetQueryId
        )
    return visible.sort((a, b) => a.createdAt - b.createdAt)
  }, [chatTabs, activePresetId, activeTabId])

  // Skip auto-close on initial mount
  const hasRenderedRef = useRef(false)

  const handleTabClick = useCallback((tabId: string) => {
    activateTab(tabId)
  }, [])

  const handleCloseTab = useCallback((tabId: string) => {
    const nextWorkflowTabId = activeTabId === tabId
      ? activeWorkflowTabs.find(tab => tab.tabId !== tabId)?.tabId ?? null
      : null

    void closeTab(tabId, false).then(() => {
      if (nextWorkflowTabId) {
        useChatStore.getState().switchTab(nextWorkflowTabId)
      }
    })
  }, [activeTabId, activeWorkflowTabs, closeTab])

  // Convert a read-only scheduled/bot run tab into an interactive Workflow
  // Builder chat: strip the view-only/scheduled metadata, rename it, and focus.
  const handleMakeInteractive = useCallback((tabId: string) => {
    const chatStore = useChatStore.getState()
    const tab = chatStore.getTab(tabId)
    if (!tab) return

    chatStore.setTabMetadata(tabId, {
      mode: 'workflow',
      phaseId: 'workflow-builder',
      phaseName: 'Automation Builder',
      presetQueryId: tab.metadata?.presetQueryId,
      isViewOnly: false,
      isScheduledRun: false,
      scheduledJobName: undefined,
      isBotRun: false,
      botPlatform: undefined,
      readOnlyRestoredAt: undefined,
    })
    useChatStore.setState((state) => {
      const current = state.chatTabs[tabId]
      if (!current) return state
      return {
        chatTabs: {
          ...state.chatTabs,
          [tabId]: { ...current, name: 'Automation Builder' },
        },
      }
    })
    activateTab(tabId)
    setShowChatArea(true)
  }, [setShowChatArea])

  // Stop this tab's running session (from the in-tab Stop control).
  const handleStopTab = useCallback(async (tabId: string) => {
    const t = useChatStore.getState().getTab(tabId)
    if (!t?.sessionId) return
    try {
      await agentApi.stopSession(t.sessionId, true)
      setTabStreaming(tabId, false)
      setTabHasRunningBgAgents(tabId, false)
    } catch (error) {
      console.error('[WorkflowChatTabs] Failed to stop session:', error)
    }
  }, [setTabStreaming, setTabHasRunningBgAgents])

  // Close chat area when all workflow tabs are closed (but not on first render)
  useEffect(() => {
    if (!hasRenderedRef.current) {
      hasRenderedRef.current = true
      return
    }
    if (activeWorkflowTabs.length === 0) {
      setShowChatArea(false)
    }
  }, [activeWorkflowTabs.length, setShowChatArea])

  // Don't render if no active workflow tabs
  if (activeWorkflowTabs.length === 0) {
    return null
  }

  return (
    <>
    <div className={embedded
      ? 'flex min-w-0 flex-1'
      : 'shrink-0 border-b border-gray-200 bg-gray-50 dark:border-gray-700 dark:bg-gray-800'}>
      <div className={embedded
        ? 'flex min-w-0 flex-1 items-center gap-1'
        : 'flex min-w-0 items-center gap-1 px-2 py-1'}>
        <div className="flex min-w-0 items-center gap-1 overflow-x-auto">
          {activeWorkflowTabs.map((tab) => (
            <WorkflowTabItem
              key={tab.tabId}
              tab={tab}
              isActive={tab.tabId === activeTabId}
              canClose={activeWorkflowTabs.length > 1}
              onTabClick={handleTabClick}
              onCloseTab={handleCloseTab}
              onMakeInteractive={handleMakeInteractive}
              onStop={handleStopTab}
            />
          ))}
        </div>

        {/* New Chat sits right next to the tabs (browser-style) */}
        {onNewChat && (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation()
              onNewChat()
            }}
            className="ml-0.5 inline-flex h-7 shrink-0 items-center gap-1 rounded-md px-2 text-xs font-medium text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-gray-100"
            title="Start a new automation chat"
          >
            <Plus className="h-3.5 w-3.5" />
            <span className="hidden sm:inline">New Chat</span>
          </button>
        )}

        {/* Models / LLM config — lives in the mode heading since the model is
            per-context (opens the globally-mounted LLM modal via the store). */}
        <LlmTriggerButton className="ml-0.5 inline-flex h-7 shrink-0 items-center rounded-md px-2 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-gray-100" />

        {/* Layout Mode — tree vs terminal, sits right after New Chat */}
        <div
          data-tour="event-view-mode"
          data-testid="tour-event-view-mode"
          className="ml-1 inline-flex shrink-0 items-center rounded-full border border-gray-200 bg-gray-100 p-0.5 dark:border-gray-700 dark:bg-gray-800"
          role="group"
          aria-label="Event layout mode"
        >
          {([
            { mode: 'tree' as const, Icon: ListTree, label: 'Tree', tip: 'Tree view — group events by automation and agent' },
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
                  aria-label={label}
                  aria-pressed={activeViewMode === mode}
                  className={`flex h-6 w-6 items-center justify-center rounded-full transition-colors ${
                    activeViewMode === mode
                      ? 'bg-blue-600 text-white shadow-sm'
                      : 'text-gray-500 hover:bg-gray-200 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-gray-100'
                  }`}
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

        {/* Spacer pushes the view controls to the right, beside status/tools */}
        <div className="min-w-[0.5rem] flex-1" />

        {/* Auto-scroll and layout controls - only show when there are workflow tabs */}
        {activeWorkflowTabs.length > 0 && (
          <div className="flex shrink-0 items-center gap-1">
            {rawActiveViewMode === 'tree' && (
            <button
              onClick={(e) => {
                e.stopPropagation()
                const newAutoScrollState = !autoScroll
                setAutoScroll(newAutoScrollState)

                if (newAutoScrollState) {
                  setTimeout(() => {
                    const chatAreaContainer = document.querySelector('[data-testid="chat-area-container"]')
                    if (chatAreaContainer) {
                      const scrollableElement = chatAreaContainer.querySelector('.overflow-y-auto')
                      if (scrollableElement) {
                        scrollableElement.scrollTo({
                          top: scrollableElement.scrollHeight,
                          behavior: 'smooth'
                        })
                      }
                    }
                  }, 50)
                }
              }}
              className={`
                flex items-center gap-1 px-2 py-1 rounded text-xs font-medium transition-colors
                ${autoScroll
                  ? 'bg-gray-200 dark:bg-gray-700 text-gray-900 dark:text-gray-100'
                  : 'bg-gray-100 dark:bg-gray-800 text-gray-600 dark:text-gray-400'
                }
                hover:bg-gray-200 dark:hover:bg-gray-700
              `}
            >
              <ArrowDown className={`w-3.5 h-3.5 ${autoScroll ? 'opacity-70' : 'opacity-40'}`} />
              <span className="hidden sm:inline">
                {autoScroll ? 'Auto-scroll' : 'Manual'}
              </span>
            </button>
            )}
          </div>
        )}
      </div>
    </div>
    <TreeViewAlphaDialog
      isOpen={pendingTreeViewTabId !== null}
      onContinue={confirmTreeView}
      onCancel={() => setPendingTreeViewTabId(null)}
    />
    </>
  )
}
