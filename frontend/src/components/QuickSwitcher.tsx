import React, { useState, useEffect, useRef, useMemo, useCallback } from 'react'
import { Activity, Layers, MessageSquare, Search } from 'lucide-react'
import { useGlobalPresetStore } from '../stores/useGlobalPresetStore'
import { useModeStore } from '../stores/useModeStore'
import { useChatStore } from '../stores'
import type { ChatTab } from '../stores/useChatStore'
import type { CustomPreset, PredefinedPreset } from '../types/preset'
import type { ActiveSessionInfo } from '../services/api-types'
import { activateTab } from '../utils/activateTab'
import { openActiveSession, openWorkflowPresetPage, pickWorkflowActiveSession, workflowSessionBotPlatform } from '../utils/workflowSessionRestore'
import { runtimeHasBackgroundAgents, runtimeNeedsUserInput, sessionRuntimeStatus } from '../utils/runtimeActivity'
import { nonWorkflowActivityTitle } from '../utils/activitySessions'

interface QuickSwitcherProps {
  isOpen: boolean
  onClose: () => void
  initialQuery?: string
}

interface WorkflowItem {
  type: 'workflow'
  id: string
  label: string
  subtitle: string
  isActive: boolean
  lastAccessedAt: number
  preset: CustomPreset | PredefinedPreset
  activeSession?: ActiveSessionInfo
}

interface ChatTabItem {
  type: 'chat'
  id: string
  label: string
  subtitle: string
  isActive: boolean
  lastAccessedAt: number
  tabId: string
  activeSession?: ActiveSessionInfo
}

interface ActiveWorkItem {
  type: 'active'
  id: string
  label: string
  subtitle: string
  isActive: boolean
  lastAccessedAt: number
  session: ActiveSessionInfo
  tabId?: string
  mode: 'workflow' | 'multi-agent'
}

type QuickSwitcherItem = WorkflowItem | ChatTabItem | ActiveWorkItem

const EMPTY_CHAT_TABS: Record<string, ChatTab> = {}
const EMPTY_ACTIVE_SESSIONS: ActiveSessionInfo[] = []
const EMPTY_WORKFLOW_PRESETS: Array<CustomPreset | PredefinedPreset> = []
const EMPTY_RECENT_PRESET_ORDER: string[] = []
const EMPTY_RECENT_PRESET_ACCESSED_AT: Record<string, number> = {}

const isWorkflowSession = (session: ActiveSessionInfo): boolean => {
  return session.agent_mode === 'workflow' ||
    session.agent_mode === 'workflow_phase' ||
    !!session.workflow_name ||
    !!session.workflow_label ||
    !!session.workspace_path ||
    !!session.preset_query_id
}

const isVisibleActiveSession = (session: ActiveSessionInfo): boolean => {
  const status = sessionRuntimeStatus(session)
  return status === 'busy' || status === 'idle' || runtimeNeedsUserInput(session) || runtimeHasBackgroundAgents(session)
}

const activeSessionLabel = (session: ActiveSessionInfo): string => {
  if (isWorkflowSession(session)) {
    return session.workflow_label ||
      session.workflow_name ||
      session.preset_name ||
      session.workspace_path?.split('/').filter(Boolean).pop() ||
      session.title ||
      'Automation'
  }
  return nonWorkflowActivityTitle(session)
}

const activeSessionStatusLabel = (session: ActiveSessionInfo): string => {
  if (runtimeNeedsUserInput(session)) return 'waiting for input'
  const bgCount = session.running_background_agent_count ?? 0
  if (bgCount > 0) return `${bgCount} bg agent${bgCount === 1 ? '' : 's'}`
  if (runtimeHasBackgroundAgents(session)) return 'bg agents running'
  return sessionRuntimeStatus(session)
}

const sessionShortId = (sessionId: string): string => sessionId.slice(0, 8)

const normalizeWorkspacePath = (path?: string): string => (path || '').replace(/\/+$/, '')

const findTabForSession = (tabs: Record<string, ChatTab>, sessionId: string): ChatTab | undefined => {
  return Object.values(tabs).find(tab => tab.sessionId === sessionId)
}

const workflowSessionMatchesPreset = (
  session: ActiveSessionInfo,
  preset: CustomPreset | PredefinedPreset,
  tabs: Record<string, ChatTab>,
): boolean => {
  if (!isWorkflowSession(session)) return false
  if (session.preset_query_id === preset.id) return true
  if (
    normalizeWorkspacePath(session.workspace_path) &&
    normalizeWorkspacePath(session.workspace_path) === normalizeWorkspacePath(preset.selectedFolder?.filepath)
  ) return true
  const tab = findTabForSession(tabs, session.session_id)
  return tab?.metadata?.presetQueryId === preset.id
}

const itemTypeRank = (item: QuickSwitcherItem): number => {
  if (item.type === 'active') return 0
  if (item.type === 'chat') return 1
  if (item.type === 'workflow') return 2
  return 3
}

const itemActiveSession = (item: QuickSwitcherItem): ActiveSessionInfo | undefined => {
  if (item.type === 'active') return item.session
  return item.activeSession
}

const activeSessionSuffix = (session?: ActiveSessionInfo): string => {
  if (!session) return ''
  const source = workflowSessionBotPlatform(session)
  const sourcePart = source ? ` · ${source}` : ''
  const current = session.current_execution_name ? ` · ${session.current_execution_name}` : ''
  return ` · active: ${activeSessionStatusLabel(session)}${sourcePart}${current} · ${sessionShortId(session.session_id)}`
}

const requestChatScrollToBottom = () => {
  useChatStore.getState().setAutoScroll(true)
  window.dispatchEvent(new CustomEvent('chat-scroll-to-bottom'))
  setTimeout(() => window.dispatchEvent(new CustomEvent('chat-scroll-to-bottom')), 120)
  setTimeout(() => window.dispatchEvent(new CustomEvent('chat-scroll-to-bottom')), 400)
}

export const QuickSwitcher: React.FC<QuickSwitcherProps> = ({
  isOpen,
  onClose,
  initialQuery = '',
}) => {
  const [query, setQuery] = useState('')
  const [selectedIndex, setSelectedIndex] = useState(0)
  const searchInputRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)

  const selectedModeCategory = useModeStore(state => state.selectedModeCategory)
  const isWorkflowMode = selectedModeCategory === 'workflow'
  const isChatMode = selectedModeCategory === 'multi-agent'
  const activePresetId = useGlobalPresetStore(state => state.activePresetIds.workflow)
  // Subscribe only while open. chatTabs changes on streaming event updates, so
  // keeping this inactive when the switcher is closed avoids background churn.
  const activeTabId = useChatStore(state => (isOpen ? state.activeTabId : null))
  const chatTabs = useChatStore(state => (isOpen ? state.chatTabs : EMPTY_CHAT_TABS))
  const activeSessions = useChatStore(state => (isOpen ? state.activeSessionsCache : EMPTY_ACTIVE_SESSIONS))
  const workflowPresets = useGlobalPresetStore(state => (isOpen ? state.workflowPresets : EMPTY_WORKFLOW_PRESETS))
  const recentPresetOrder = useGlobalPresetStore(state => (isOpen ? state.recentPresetOrder : EMPTY_RECENT_PRESET_ORDER))
  const recentPresetAccessedAt = useGlobalPresetStore(state => (isOpen ? state.recentPresetAccessedAt : EMPTY_RECENT_PRESET_ACCESSED_AT))

  // Track Shift key state to show "minimize" hint on selected item
  const [shiftHeld, setShiftHeld] = useState(false)
  useEffect(() => {
    if (!isOpen) return
    const onKey = (e: KeyboardEvent) => setShiftHeld(e.shiftKey)
    window.addEventListener('keydown', onKey)
    window.addEventListener('keyup', onKey)
    return () => {
      window.removeEventListener('keydown', onKey)
      window.removeEventListener('keyup', onKey)
      setShiftHeld(false)
    }
  }, [isOpen])

  // Reset state on open.
  useEffect(() => {
    if (isOpen) {
      setQuery(initialQuery)
      setSelectedIndex(0)
      void useChatStore.getState().getActiveSessions(true)
      setTimeout(() => searchInputRef.current?.focus(), 50)
    }
  }, [isOpen, initialQuery, workflowPresets])

  // Build a cross-mode command center: active work + chats + workflows.
  const allItems = useMemo<QuickSwitcherItem[]>(() => {
    if (!isOpen) return []

    const allTabs = Object.values(chatTabs)
    const activeSessionsByID = new Map<string, ActiveSessionInfo>()
    for (const session of activeSessions.filter(isVisibleActiveSession)) {
      activeSessionsByID.set(session.session_id, session)
    }
    const visibleActiveSessions = Array.from(activeSessionsByID.values()).filter(isVisibleActiveSession)

    const builderStateSuffix = (tab?: ChatTab): string => {
      if (!tab?.hasRunningBgAgents) return ''
      return tab.isStreaming || tab.isSyntheticTurn ? ' · builder busy' : ' · builder idle'
    }

    const chatItems: ChatTabItem[] = Object.values(chatTabs)
      .filter(tab => tab.metadata?.mode === 'multi-agent' && !tab.metadata?.isOrganizationAssistant)
      .sort((a, b) => a.createdAt - b.createdAt)
      .map(tab => {
        const activeSession = tab.sessionId ? visibleActiveSessions.find(session => session.session_id === tab.sessionId) : undefined
        const streamingLabel = tab.isStreaming ? 'Streaming...' : tab.isCompleted ? 'Completed' : tab.sessionId ? 'Active' : 'New'
        return {
          type: 'chat' as const,
          id: `chat:${tab.tabId}`,
          label: tab.name,
          subtitle: `Chat · ${streamingLabel}${builderStateSuffix(tab)}${activeSessionSuffix(activeSession)}`,
          isActive: isChatMode && tab.tabId === activeTabId,
          lastAccessedAt: tab.lastAccessedAt || tab.createdAt || 0,
          tabId: tab.tabId,
          activeSession,
        }
      })

    const workflowItems: WorkflowItem[] = workflowPresets
      .filter(p => p.selectedFolder?.filepath)
      .map(p => {
        const matchingActiveSessions = visibleActiveSessions.filter(session => workflowSessionMatchesPreset(session, p, chatTabs))
        const activeSession = pickWorkflowActiveSession(matchingActiveSessions, p, chatTabs)
        const workflowTab = allTabs.find(tab => tab.metadata?.mode === 'workflow' && tab.metadata?.presetQueryId === p.id)
        const activeCountSuffix = matchingActiveSessions.length > 1 ? ` · ${matchingActiveSessions.length} active runs` : ''
        return {
          type: 'workflow' as const,
          id: `workflow:${p.id}`,
          label: p.label,
          subtitle: `Automation · ${p.selectedFolder!.filepath}${builderStateSuffix(workflowTab)}${activeSessionSuffix(activeSession)}${activeCountSuffix}`,
          isActive: isWorkflowMode && p.id === activePresetId,
          lastAccessedAt: recentPresetAccessedAt[p.id] || (() => {
            const recentIndex = recentPresetOrder.indexOf(p.id)
            return recentIndex >= 0 ? 1_000_000 - recentIndex : 0
          })(),
          preset: p,
          activeSession,
        }
      })

    const activeItems: ActiveWorkItem[] = visibleActiveSessions
      .filter(session => {
        const tab = findTabForSession(chatTabs, session.session_id)
        if (tab && !tab.metadata?.isOrganizationAssistant) return false
        if (
          isWorkflowSession(session) &&
          workflowPresets.some(preset => workflowSessionMatchesPreset(session, preset, chatTabs))
        ) return false
        return true
      })
      .map(session => {
        const tab = findTabForSession(chatTabs, session.session_id)
        const workflow = isWorkflowSession(session)
        const status = activeSessionStatusLabel(session)
        const current = session.current_execution_name ? ` · ${session.current_execution_name}` : ''
        return {
          type: 'active' as const,
          id: `active:${session.session_id}`,
          label: activeSessionLabel(session),
          subtitle: `${workflow ? 'Active automation' : 'Active chat'} · ${status}${current} · ${sessionShortId(session.session_id)}`,
          isActive: !!tab && tab.tabId === activeTabId,
          lastAccessedAt: tab?.lastAccessedAt || tab?.createdAt || Date.parse(session.last_activity || session.created_at || '') || 0,
          session,
          tabId: tab?.tabId,
          mode: workflow ? 'workflow' : 'multi-agent',
        }
      })

    workflowItems.sort((a, b) => {
      const aIdx = recentPresetOrder.indexOf(a.preset.id)
      const bIdx = recentPresetOrder.indexOf(b.preset.id)
      if (aIdx !== -1 && bIdx !== -1) return aIdx - bIdx
      if (aIdx !== -1) return -1
      if (bIdx !== -1) return 1
      return a.label.localeCompare(b.label)
    })

    return [...activeItems, ...chatItems, ...workflowItems].sort((a, b) => {
      if (a.isActive !== b.isActive) return a.isActive ? -1 : 1
      if (a.lastAccessedAt !== b.lastAccessedAt) return b.lastAccessedAt - a.lastAccessedAt
      if (a.type !== b.type) return itemTypeRank(a) - itemTypeRank(b)
      return a.label.localeCompare(b.label)
    })
  }, [isOpen, isWorkflowMode, isChatMode, activePresetId, chatTabs, activeSessions, activeTabId, workflowPresets, recentPresetOrder, recentPresetAccessedAt])

  // Filter and sort
  const filteredItems = useMemo<QuickSwitcherItem[]>(() => {
    const rawQuery = query.toLowerCase().trim()
    if (!rawQuery) return allItems

    const scopeMatch = rawQuery.match(/^@(active|workflows?|chats?|tabs)\s*/)
    const scope = scopeMatch?.[1] || null
    const q = scopeMatch ? rawQuery.slice(scopeMatch[0].length).trim() : rawQuery
    const scoped = scope
      ? allItems.filter(item => {
          if (scope === 'active') return !!itemActiveSession(item)
          if (scope === 'workflow' || scope === 'workflows') return item.type === 'workflow'
          if (scope === 'chat' || scope === 'chats') return item.type === 'chat'
          return item.type === 'chat' || item.type === 'workflow'
        })
      : allItems

    if (!q) return scoped

    const filtered = scoped.filter(item =>
      item.label.toLowerCase().includes(q) ||
      item.subtitle.toLowerCase().includes(q)
    )

    filtered.sort((a, b) => {
      const aExact = a.label.toLowerCase() === q
      const bExact = b.label.toLowerCase() === q
      if (aExact && !bExact) return -1
      if (!aExact && bExact) return 1
      const aStarts = a.label.toLowerCase().startsWith(q)
      const bStarts = b.label.toLowerCase().startsWith(q)
      if (aStarts && !bStarts) return -1
      if (!aStarts && bStarts) return 1
      return a.label.localeCompare(b.label)
    })

    return filtered
  }, [query, allItems])

  // Read filteredItems via a ref so this effect does NOT fire on every new
  // array reference — only on real user intent changes (query, mode, open).
  // Otherwise streaming re-renders would snap the selection back on each tick.
  const filteredItemsRef = useRef(filteredItems)
  filteredItemsRef.current = filteredItems
  useEffect(() => {
    if (!query.trim() && (isChatMode || isWorkflowMode)) {
      const firstNonActive = filteredItemsRef.current.findIndex(item => !item.isActive)
      setSelectedIndex(firstNonActive >= 0 ? firstNonActive : 0)
    } else {
      setSelectedIndex(0)
    }
  }, [isOpen, isChatMode, isWorkflowMode, query])

  // Clamp (don't reset) when the list length changes so a narrowing filter
  // keeps a valid index without discarding the user's position.
  useEffect(() => {
    setSelectedIndex(prev => {
      if (filteredItems.length === 0) return 0
      return Math.min(prev, filteredItems.length - 1)
    })
  }, [filteredItems.length])

  useEffect(() => {
    if (listRef.current && selectedIndex >= 0) {
      const el = listRef.current.children[selectedIndex] as HTMLElement
      el?.scrollIntoView({ block: 'nearest', behavior: 'auto' })
    }
  }, [selectedIndex])

  const handleSelect = useCallback(async (item: QuickSwitcherItem, minimize = false) => {
    if (item.type === 'active') {
      // Shared path with the header activity monitor so opening the same session
      // behaves identically from either surface.
      await openActiveSession(item.session, {
        title: item.label,
        source: 'quick-switcher',
      })
      onClose()
      return
    }

    if (item.type === 'chat') {
      console.log(`%c[QuickSwitcher] Switching to chat tab: ${item.label} (${item.tabId})`, 'color: #FF9800; font-weight: bold')
      activateTab(item.tabId)
      requestChatScrollToBottom()
      onClose()
      return
    }

    // Workflow switching
    console.log(`%c[QuickSwitcher] Switching to workflow: ${item.label?.slice(0,30)} (${item.id?.slice(0,8)})`, 'color: #FF9800; font-weight: bold')
    console.time('[QuickSwitcher] workflow-switch-total')

    if (minimize) {
      const chatStore = useChatStore.getState()
      Object.values(chatStore.chatTabs).forEach(tab => {
        if (tab.metadata?.mode === 'workflow' && tab.metadata?.presetQueryId === item.preset.id) {
          chatStore.setTabViewMode(tab.tabId, 'tree')
        }
      })
    }

    await openWorkflowPresetPage(item.preset, {
      activeSession: item.activeSession,
      title: item.label,
      source: 'quick-switcher',
    })
    console.timeEnd('[QuickSwitcher] workflow-switch-total')
    requestChatScrollToBottom()
    onClose()
  }, [onClose])

  const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setSelectedIndex(prev => Math.min(prev + 1, filteredItems.length - 1))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setSelectedIndex(prev => Math.max(prev - 1, 0))
    } else if (e.key === 'Enter') {
      e.preventDefault()
      if (filteredItems.length > 0 && selectedIndex >= 0 && selectedIndex < filteredItems.length) {
        void handleSelect(filteredItems[selectedIndex], e.shiftKey)
      }
    } else if (e.key === 'Escape') {
      e.preventDefault()
      onClose()
    }
  }, [filteredItems, selectedIndex, handleSelect, onClose])

  if (!isOpen) return null

  const placeholder = 'Search automations, chats, or active work...'
  const emptyText = query ? 'No matching items' : 'No switchable items available'
  return (
    <div
      className="absolute inset-0 z-50 flex items-start justify-center pt-[20vh]"
      onClick={onClose}
    >
      {/* Backdrop */}
      <div className="absolute inset-0 bg-black/50" />

      {/* Dialog */}
      <div
        className="relative w-[min(46rem,calc(100vw-2rem))] bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl shadow-2xl overflow-hidden text-gray-900 dark:text-gray-100"
        onClick={e => e.stopPropagation()}
      >
        {/* Search input */}
        <div className="flex items-center gap-3 px-4 py-3 border-b border-gray-200 dark:border-gray-700">
          <Search className="w-5 h-5 text-gray-400 flex-shrink-0" />
          <input
            ref={searchInputRef}
            type="text"
            placeholder={placeholder}
            value={query}
            onChange={e => setQuery(e.target.value)}
            onKeyDown={handleKeyDown}
            className="flex-1 bg-transparent text-sm text-foreground placeholder:text-muted-foreground focus:outline-none"
          />
          <kbd className="hidden sm:inline-flex px-1.5 py-0.5 text-[10px] font-mono text-gray-400 bg-gray-100 dark:bg-gray-700 rounded">
            ESC
          </kbd>
        </div>

        {/* Item list */}
        <div ref={listRef} className="overflow-y-auto max-h-[48vh]">
          {filteredItems.length === 0 ? (
            <div className="px-4 py-8 text-center text-muted-foreground text-sm">
              {emptyText}
            </div>
          ) : (
            filteredItems.map((item, index) => {
              const isSelected = index === selectedIndex
              const ItemIcon = item.type === 'workflow'
                ? Layers
                : item.type === 'active'
                  ? Activity
                  : MessageSquare
              const activeSession = itemActiveSession(item)
              return (
                <div
                  key={item.id}
                  className={`px-4 py-2.5 cursor-pointer flex items-center gap-3 transition-colors ${
                    isSelected
                      ? 'bg-blue-50 dark:bg-blue-900/30'
                      : 'hover:bg-gray-50 dark:hover:bg-gray-700/50'
                  }`}
                  onMouseEnter={() => setSelectedIndex(index)}
                  onMouseDown={e => { e.preventDefault(); void handleSelect(item, e.shiftKey) }}
                >
                  <ItemIcon className={`w-4 h-4 flex-shrink-0 ${item.isActive ? 'text-blue-500' : 'text-gray-400 dark:text-gray-500'}`} />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className={`text-sm font-medium truncate ${item.isActive ? 'text-blue-600 dark:text-blue-400' : 'text-gray-900 dark:text-gray-100'}`}>
                        {item.label}
                      </span>
                      <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-gray-100 dark:bg-gray-700 text-gray-500 dark:text-gray-300 font-medium flex-shrink-0">
                        {item.type === 'active' ? 'active' : item.type}
                      </span>
                      {activeSession && item.type !== 'active' && (
                        activeSession.needs_user_input ? (
                          <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-300 font-medium flex-shrink-0">
                            needs input
                          </span>
                        ) : (
                          <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-emerald-100 dark:bg-emerald-900/40 text-emerald-700 dark:text-emerald-300 font-medium flex-shrink-0">
                            active
                          </span>
                        )
                      )}
                      {item.isActive && (
                        <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-blue-100 dark:bg-blue-900/50 text-blue-600 dark:text-blue-400 font-medium flex-shrink-0">
                          current
                        </span>
                      )}
                    </div>
                    <div className="text-xs text-muted-foreground truncate">{item.subtitle}</div>
                  </div>
                  {/* Show "minimize current" hint when Shift is held on a non-active workflow item */}
                  {isSelected && shiftHeld && !item.isActive && item.type === 'workflow' && (
                    <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-amber-100 dark:bg-amber-900/40 text-amber-600 dark:text-amber-300 font-medium flex-shrink-0 animate-in fade-in duration-150">
                      minimize current
                    </span>
                  )}
                </div>
              )
            })
          )}
        </div>

        {/* Footer */}
        <div className="border-t border-gray-200 bg-gray-50 dark:border-gray-700 dark:bg-gray-900/50">
          <div className="px-4 py-2 text-[11px] text-gray-400 dark:text-gray-500 flex items-center justify-between">
            <div className="flex items-center gap-3 min-w-0">
              <span><kbd className="px-1 py-0.5 bg-gray-200 dark:bg-gray-600 rounded text-[10px]">↑↓</kbd> navigate</span>
              <span><kbd className="px-1 py-0.5 bg-gray-200 dark:bg-gray-600 rounded text-[10px]">↵</kbd> switch</span>
              {filteredItems.some(item => item.type === 'workflow') && (
                <span><kbd className="px-1 py-0.5 bg-amber-200 dark:bg-amber-800 text-amber-700 dark:text-amber-300 rounded text-[10px]">⇧↵</kbd> switch &amp; minimize</span>
              )}
            </div>
            <span className="hidden sm:inline flex-shrink-0">@active @workflows @chats</span>
            <span className="flex-shrink-0"><kbd className="px-1 py-0.5 bg-gray-200 dark:bg-gray-600 rounded text-[10px]">esc</kbd> close</span>
          </div>
        </div>
      </div>
    </div>
  )
}

export default QuickSwitcher
