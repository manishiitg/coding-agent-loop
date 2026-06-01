import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { Plus, ArrowDown, ListTree, Terminal } from 'lucide-react'
import { normalizeEventViewMode, useChatStore, type ChatTab } from '../stores/useChatStore'
import { useAppStore } from '../stores/useAppStore'
import { useModeStore } from '../stores/useModeStore'
import { Tooltip, TooltipContent, TooltipTrigger } from './ui/tooltip'
import { TreeViewAlphaDialog, shouldShowTreeViewAlphaWarning } from './TreeViewAlphaDialog'

interface ChatTabsProps {
  // For multi-agent mode: callback when starting a new chat (reset-in-place)
  onNewChat?: () => void
  // Auto-scroll state and toggle
  autoScroll?: boolean
  onToggleAutoScroll?: () => void
}

// Multi-agent chat is single-tab: this bar is a slim header for the one chat
// tab (title + view controls + New Chat). It is not a tab switcher anymore.
// Workflow mode renders its own tabs (WorkflowChatTabs) inside the chat panel.
export const ChatTabs: React.FC<ChatTabsProps> = ({ onNewChat, autoScroll, onToggleAutoScroll }) => {
  const [pendingTreeViewTabId, setPendingTreeViewTabId] = useState<string | null>(null)
  const selectedModeCategory = useModeStore(state => state.selectedModeCategory)
  const showWorkflowsOverview = useAppStore(state => state.showWorkflowsOverview)
  const {
    chatTabs,
    activeTabId,
    switchTab,
    autoScroll: storeAutoScroll,
    setAutoScroll,
    setTabViewMode,
  } = useChatStore()

  const activeViewMode = useMemo(
    () => normalizeEventViewMode(activeTabId ? chatTabs[activeTabId]?.viewMode : undefined),
    [activeTabId, chatTabs]
  )

  const isHiddenOrganizationTab = useCallback((tab: ChatTab) => {
    // Only hide tabs explicitly marked as org assistant via metadata.
    // Never match by tab name — that can hide normal chat tabs.
    return tab.metadata?.isOrganizationAssistant === true
  }, [])

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

  const activeTab = activeTabId ? chatTabs[activeTabId] : undefined
  const showHeaderContent =
    !!activeTab && activeTab.metadata?.mode === 'multi-agent' && !isHiddenOrganizationTab(activeTab)
  const chatTitle = showHeaderContent ? (activeTab?.name || 'Agent Chat') : 'Agent Chat'

  return (
    <>
    <div className="flex-shrink-0 flex items-center gap-2 bg-gray-50 dark:bg-gray-800 px-3 py-1.5 border-b border-gray-200 dark:border-gray-700">
      {/* Single-chat title */}
      <span className="text-sm font-medium text-gray-900 dark:text-gray-100 whitespace-nowrap truncate">
        {chatTitle}
      </span>

      <div className="ml-auto flex items-center gap-1">
        {/* New Chat — resets the current chat in place (confirmation handled upstream) */}
        {onNewChat && (
          <button
            onClick={onNewChat}
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
      </div>
      <TreeViewAlphaDialog
        isOpen={pendingTreeViewTabId !== null}
        onContinue={confirmTreeView}
        onCancel={() => setPendingTreeViewTabId(null)}
      />
    </>
  )
}
