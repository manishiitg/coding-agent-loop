import React, { useMemo, useEffect, useCallback, useRef, useState } from 'react'
import { useShallow } from 'zustand/react/shallow'
import { MessageSquare, X } from 'lucide-react'
import { useChatStore, type ChatTab } from '../../stores/useChatStore'
import { activateTab } from '../../utils/activateTab'
import { useWorkflowStore } from '../../stores/useWorkflowStore'
import { useGlobalPresetStore } from '../../stores/useGlobalPresetStore'
import {
  convertObservedWorkflowTabToInteractive,
  isMisclassifiedRestoredWorkflowChat,
} from './workflowChatTabConversion'
import { shouldDisplayWorkflowTab, workflowTabDisplayName } from './workflowRuntimeTabProjection'
import { isBlankWorkflowBuilderTab } from '../../utils/workflowTabResolution'
import { useAuthStore } from '../../stores/useAuthStore'
import { isWorkflowReadOnly } from '../../utils/workflowPermissions'

// ---------------------------------------------------------------------------
// WorkflowTabItem — per-tab component with narrow store subscriptions
// ---------------------------------------------------------------------------

interface WorkflowTabItemProps {
  tab: ChatTab
  isActive: boolean
  canClose: boolean
  isBlank: boolean
  onTabClick: (tabId: string) => void
  onCloseTab: (tabId: string) => void
  onMakeInteractive: (tabId: string) => void
}

// TEMPORARY 2026-09-04: schedule/bot tabs cannot be turned into an
// interactive chat right now, at the user's explicit request. Set back to
// true to restore the "make interactive" icon.
const ALLOW_MAKE_SCHEDULE_INTERACTIVE = false

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
  isBlank,
  onTabClick,
  onCloseTab,
  onMakeInteractive,
}) => {
	const isReadOnlyUser = useAuthStore(state => isWorkflowReadOnly(state.user, state.isMultiUserMode))
	const displayName = workflowTabDisplayName(tab, isBlank)

  // Tabs are a product-level conversation switcher. Derive their small status
  // marker from the session's own lifecycle flags rather than polling the
  // diagnostic execution tree for every active tab.
  const rawStatus: 'busy' | 'idle' | 'stopped' =
    tab.isStreaming || tab.hasRunningBgAgents
      ? 'busy'
      : tab.isCompleted
        ? 'stopped'
        : 'idle'

  // A live multi-step run can toggle isStreaming/hasRunningBgAgents on
  // consecutive 500ms polls as background agents register/deregister between
  // steps or tool calls — real backend state, but a raw render of it makes
  // the status dot visibly flicker every second or two. Entering busy
  // applies instantly (immediate feedback), so this only smooths the
  // busy -> idle/stopped edge: it must hold for a short window before the
  // pill actually leaves the busy state.
  const [status, setStatus] = useState(rawStatus)
  useEffect(() => {
    if (rawStatus === status) return
    if (rawStatus === 'busy') {
      setStatus('busy')
      return
    }
    const timer = setTimeout(() => setStatus(rawStatus), 1200)
    return () => clearTimeout(timer)
  }, [rawStatus, status])
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
      {/* Live status dot (busy/idle/stopped) -- not on Builder. It's a
          permanent fixture, not a run with a lifecycle to report on, and the
          dot never meaningfully left "idle" for it anyway. */}
      {!isBlank && <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${dot.cls}`} title={dot.label} aria-label={dot.label} />}

      {/* Tab Name */}
      <span className="min-w-0 max-w-[14rem] truncate whitespace-nowrap">{displayName}</span>

      {/* Convert a read-only scheduled/bot run into an interactive Automation Builder chat.
          Hidden for a read-only-access user (PLAT-262): the resulting session
          would still be Run-mode-restricted server-side, but this button's
          own label ("Interact in Automation Builder") promises full edit
          capability the account doesn't have — same UX-confusion class as
          the schedule panel's equivalent icon.
          TEMPORARILY DISABLED 2026-09-04 at the user's request: don't let
          anyone turn a schedule tab into a chat right now. Flip
          ALLOW_MAKE_SCHEDULE_INTERACTIVE back on to restore it. */}
      {ALLOW_MAKE_SCHEDULE_INTERACTIVE && tab.metadata?.isViewOnly && (tab.metadata?.isScheduledRun || tab.metadata?.isBotRun) && !isReadOnlyUser && (
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

      {/* A busy tab can't be closed. Closing never stopped its run — the work
          kept executing on the backend and the still-running-workflow
          reconciler recreated the tab on its next poll, so the X read as a
          no-op. Stop the run first (the control in the chat input), then close. */}
      {canClose && (
        <button
          type="button"
          disabled={isBusy}
          onClick={(e) => {
            e.stopPropagation()
            if (isBusy) return
            onCloseTab(tab.tabId)
          }}
          className={`ml-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded text-gray-400 transition-colors ${
            isBusy
              ? 'cursor-not-allowed opacity-40'
              : 'hover:bg-gray-200 hover:text-gray-700 dark:hover:bg-gray-700 dark:hover:text-gray-200'
          }`}
          aria-label={isBusy ? `${displayName} is still running — stop it before closing` : `Close ${displayName}`}
          title={isBusy ? 'Still running — stop the run before closing' : 'Close tab'}
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
  // When true, render inline (no bordered/background bar wrapper) so the strip can
  // be embedded inside the WorkflowToolbar row instead of being its own bar.
  embedded?: boolean
}

export const WorkflowChatTabs: React.FC<WorkflowChatTabsProps> = ({ embedded = false }) => {
  const {
    chatTabs,
    activeTabId,
    tabEvents,
    closeTab,
  } = useChatStore(useShallow(state => ({
    chatTabs: state.chatTabs,
    activeTabId: state.activeTabId,
    tabEvents: state.tabEvents,
    closeTab: state.closeTab,
  })))

  const setShowChatArea = useWorkflowStore(state => state.setShowChatArea)
  const setFocusedPane = useWorkflowStore(state => state.setFocusedPane)
  const activePresetId = useGlobalPresetStore(state => state.activePresetIds.workflow)

  // Repair tabs already corrupted by the old Restore path. The durable
  // restoredConversationPath marker proves this is an interactive restore,
  // while isViewOnly proves it incorrectly retained Schedule/full-run state.
  useEffect(() => {
    const affected = Object.values(chatTabs).filter(isMisclassifiedRestoredWorkflowChat)
    if (affected.length === 0) return
    useChatStore.setState(state => {
      const nextTabs = { ...state.chatTabs }
      affected.forEach(tab => {
        const current = nextTabs[tab.tabId]
        if (current && isMisclassifiedRestoredWorkflowChat(current)) {
          nextTabs[tab.tabId] = convertObservedWorkflowTabToInteractive(current)
        }
      })
      return { chatTabs: nextTabs }
    })
  }, [chatTabs])

  // Filter to workflow tabs for the active preset, but always keep the active
  // workflow tab visible. Scheduled-run restores can briefly lack a preset match
  // while the tab is being created/switched, and hiding the active tab makes the
  // restore look like it failed.
  const activeWorkflowTabs = useMemo(() => {
    const allTabs = Object.values(chatTabs)
    const matched = allTabs.filter(tab =>
      tab.metadata?.mode === 'workflow' &&
      tab.metadata.presetQueryId === activePresetId &&
      shouldDisplayWorkflowTab(tab, activeTabId)
    )
    const activeTab = activeTabId ? chatTabs[activeTabId] : undefined
    const activeWorkflowTab = activeTab?.metadata?.mode === 'workflow' ? activeTab : undefined

    const visibleById = new Map<string, ChatTab>()
    matched.forEach(tab => visibleById.set(tab.tabId, tab))
    if (activeWorkflowTab) {
      visibleById.set(activeWorkflowTab.tabId, activeWorkflowTab)
    }

    const visible = visibleById.size > 0
      ? Array.from(visibleById.values())
      : allTabs.filter(tab =>
          tab.metadata?.mode === 'workflow' &&
          tab.metadata?.phaseId === 'workflow-builder' &&
          !tab.metadata?.presetQueryId
        )
    // The blank Builder tab is this workflow's permanent home base: always
    // first in the strip, never sorted by when it happened to be created.
    return visible.sort((a, b) => {
      const aBlank = isBlankWorkflowBuilderTab(a, activePresetId || '', tabEvents)
      const bBlank = isBlankWorkflowBuilderTab(b, activePresetId || '', tabEvents)
      if (aBlank !== bBlank) return aBlank ? -1 : 1
      return a.createdAt - b.createdAt
    })
  }, [chatTabs, activePresetId, activeTabId, tabEvents])

  // Skip auto-close on initial mount
  const hasRenderedRef = useRef(false)

  const handleTabClick = useCallback((tabId: string) => {
    activateTab(tabId)
    // On narrow screens the toolbar stays visible while only one content pane
    // is shown. A chat-tab selection must therefore bring the chat pane back.
    setFocusedPane('chat')
  }, [setFocusedPane])

  const handleCloseTab = useCallback((tabId: string) => {
    const nextWorkflowTabId = activeTabId === tabId
      ? activeWorkflowTabs.find(tab => tab.tabId !== tabId)?.tabId ?? null
      : null

    void closeTab(tabId, false).then(() => {
      if (nextWorkflowTabId) {
        activateTab(nextWorkflowTabId)
      }
    })
  }, [activeTabId, activeWorkflowTabs, closeTab])

  // Make the scheduled/bot conversation interactive without changing its
  // identity. The backend routes the next message to this session's retained
  // live tmux first; if the pane is gone, it resumes the same native coding-CLI
  // conversation. Rotating the session ID here would incorrectly fork away
  // from both the tmux and its conversation context.
  const handleMakeInteractive = useCallback((tabId: string) => {
    const chatStore = useChatStore.getState()
    const tab = chatStore.getTab(tabId)
    if (!tab) return

    useChatStore.setState((state) => {
      const current = state.chatTabs[tabId]
      if (!current) return state
      return {
        chatTabs: {
          ...state.chatTabs,
          [tabId]: convertObservedWorkflowTabToInteractive(current),
        },
      }
    })
    activateTab(tabId)
    setShowChatArea(true)
    setFocusedPane('chat')
  }, [setFocusedPane, setShowChatArea])

  // DISABLED 2026-09-04 (shipped and reverted same day): staleWorkflowTabIds
  // treats a tab as "safe to sweep" using isStreaming/hasRunningBgAgents,
  // but reconcileRunningWorkflowTab (WorkflowLayout.tsx) treats a session as
  // still "running" on a broader definition that also covers idle/waiting --
  // which is the normal resting state of an Automation Builder chat between
  // messages. So the sweep was closing perfectly ordinary idle tabs, and the
  // reconciler's next ~10s poll recreated them from the still-tracked
  // backend session -- unprompted close/reopen flicker, live in production.
  // Re-enable only once staleWorkflowTabIds (or its caller) checks the same
  // "is this session still running" signal reconcileRunningWorkflowTab does,
  // not the narrower streaming/bg-agent flags. staleWorkflowTabIds and its
  // tests are left in place -- the function itself is correct, it just
  // needs a correct "is this session done" input.

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
    {/* min-w-0 without flex-1: this strip takes only the width its tab pills
        need, so the toolbar's other controls sit immediately after it. */}
    <div className={embedded
      ? 'flex min-w-0 shrink-0'
      : 'shrink-0 border-b border-gray-200 bg-gray-50 dark:border-gray-700 dark:bg-gray-800'}>
      <div className={embedded
        ? 'flex min-w-0 items-center gap-1'
        : 'flex min-w-0 items-center gap-1 px-2 py-1'}>
        <div className="flex min-w-0 items-center gap-1 overflow-x-auto">
          {activeWorkflowTabs.map((tab) => {
            const isBlank = isBlankWorkflowBuilderTab(tab, activePresetId || '', tabEvents)
            return (
              <WorkflowTabItem
                key={tab.tabId}
                tab={tab}
                isActive={tab.tabId === activeTabId}
                // Builder is this workflow's permanent home base -- never
                // closeable, regardless of how many other tabs are open.
                canClose={!isBlank && activeWorkflowTabs.length > 1}
                isBlank={isBlank}
                onTabClick={handleTabClick}
                onCloseTab={handleCloseTab}
                onMakeInteractive={handleMakeInteractive}
              />
            )
          })}
        </div>
      </div>
    </div>
    </>
  )
}
