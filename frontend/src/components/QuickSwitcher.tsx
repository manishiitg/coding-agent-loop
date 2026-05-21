import React, { useState, useEffect, useRef, useMemo, useCallback } from 'react'
import { Activity, Database, Layers, MessageSquare, Search } from 'lucide-react'
import { useGlobalPresetStore } from '../stores/useGlobalPresetStore'
import { useModeStore } from '../stores/useModeStore'
import { useChatStore } from '../stores'
import type { ChatTab } from '../stores/useChatStore'
import { useWorkflowStore } from '../stores/useWorkflowStore'
import { useAppStore } from '../stores/useAppStore'
import type { CustomPreset, PredefinedPreset } from '../types/preset'
import type { ActiveSessionInfo, PollingEvent } from '../services/api-types'
import { restoreSession } from '../utils/sessionRestore'
import { isBotWorkflowSession, isScheduledWorkflowSession, restoreBotWorkflowRunChat, restoreScheduledWorkflowRunChat, restoreWorkflowSessionChat, workflowSessionBotPlatform } from '../utils/workflowSessionRestore'
import { formatEventMemoryBytes, hasEventMemoryPressure, type EventMemoryStats } from '../utils/eventMemory'

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
  eventStats?: EventMemoryStats
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
  eventStats?: EventMemoryStats
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
  eventStats?: EventMemoryStats
}

interface EventMemoryItem {
  type: 'events'
  id: string
  label: string
  subtitle: string
  isActive: boolean
  lastAccessedAt: number
  sessionId: string
  tabIds: string[]
  mode: 'workflow' | 'multi-agent' | undefined
  stats: EventMemoryStats
}

type QuickSwitcherItem = WorkflowItem | ChatTabItem | ActiveWorkItem | EventMemoryItem

const EMPTY_CHAT_TABS: Record<string, ChatTab> = {}
const EMPTY_TAB_EVENTS: Record<string, PollingEvent[]> = {}
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
  const status = (session.status || '').toLowerCase()
  return status === 'running' ||
    status === 'active' ||
    status === 'in_progress' ||
    status === 'paused' ||
    status === 'waiting' ||
    status === 'waiting_feedback' ||
    status === 'idle' ||
    !!session.needs_user_input ||
    session.has_running_background_agents === true ||
    (session.running_background_agent_count ?? 0) > 0
}

const activeSessionLabel = (session: ActiveSessionInfo): string => {
  if (isWorkflowSession(session)) {
    return session.workflow_label ||
      session.workflow_name ||
      session.preset_name ||
      session.workspace_path?.split('/').filter(Boolean).pop() ||
      session.title ||
      'Workflow'
  }
  return session.title || session.query || 'Chat'
}

const activeSessionStatusLabel = (session: ActiveSessionInfo): string => {
  if (session.needs_user_input) return 'waiting for input'
  const bgCount = session.running_background_agent_count ?? 0
  if (bgCount > 0) return `${bgCount} bg agent${bgCount === 1 ? '' : 's'}`
  if (session.has_running_background_agents) return 'bg agents running'
  return (session.status || 'active').replace(/_/g, ' ')
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

const workflowSessionPriority = (session: ActiveSessionInfo): number => {
  const status = (session.status || '').toLowerCase()
  let score = 0
  // Workflow rows should reopen the interactive builder/execution chat first.
  // Scheduled/Bot runs remain restorable as active rows, but they should not
  // steal the main workflow preset switch and leave the builder looking empty.
  if (isScheduledWorkflowSession(session) || isBotWorkflowSession(session)) score += 100
  if (session.needs_user_input) score -= 30
  if (session.has_running_background_agents || (session.running_background_agent_count ?? 0) > 0) score -= 20
  if (status === 'running' || status === 'active' || status === 'in_progress') score -= 10
  return score
}

const pickWorkflowActiveSession = (
  sessions: ActiveSessionInfo[],
  preset: CustomPreset | PredefinedPreset,
  tabs: Record<string, ChatTab>,
): ActiveSessionInfo | undefined => {
  return sessions
    .filter(isVisibleActiveSession)
    .filter(session => workflowSessionMatchesPreset(session, preset, tabs))
    .sort((a, b) => {
      const priorityDelta = workflowSessionPriority(a) - workflowSessionPriority(b)
      if (priorityDelta !== 0) return priorityDelta
      return Date.parse(b.last_activity || b.created_at || '') - Date.parse(a.last_activity || a.created_at || '')
    })[0]
}

const itemTypeRank = (item: QuickSwitcherItem): number => {
  if (item.type === 'active') return 0
  if (item.type === 'chat') return 1
  if (item.type === 'workflow') return 2
  return 3
}

const itemEventStats = (item: QuickSwitcherItem): EventMemoryStats | undefined => {
  return item.type === 'events' ? item.stats : item.eventStats
}

const itemActiveSession = (item: QuickSwitcherItem): ActiveSessionInfo | undefined => {
  if (item.type === 'active') return item.session
  if (item.type === 'events') return undefined
  return item.activeSession
}

const eventStatsSuffix = (stats?: EventMemoryStats): string => {
  if (!stats || stats.eventCount === 0) return ''
  if (stats.sizeBytes <= 0) return ` · ${stats.eventCount.toLocaleString()} events`
  const largest = stats.largestEventType
    ? ` · largest ${stats.largestEventType} (${formatEventMemoryBytes(stats.largestEventBytes)})`
    : ''
  return ` · ${stats.eventCount.toLocaleString()} events · ${formatEventMemoryBytes(stats.sizeBytes)}${largest}`
}

const eventStatsLabel = (stats: EventMemoryStats): string => {
  if (stats.sizeBytes <= 0) return `${stats.eventCount.toLocaleString()} events`
  const largest = stats.largestEventType
    ? ` · largest ${stats.largestEventType} (${formatEventMemoryBytes(stats.largestEventBytes)})`
    : ''
  return `${stats.eventCount.toLocaleString()} events · ${formatEventMemoryBytes(stats.sizeBytes)}${largest}`
}

const lightweightEventStats = (events: PollingEvent[] | undefined): EventMemoryStats | undefined => {
  if (!events || events.length === 0) return undefined
  return {
    eventCount: events.length,
    sizeBytes: 0,
    largestEventBytes: 0,
    largestEventType: '',
  }
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
  const tabEvents = useChatStore(state => (isOpen ? state.tabEvents : EMPTY_TAB_EVENTS))
  const activeSessions = useChatStore(state => (isOpen ? state.activeSessionsCache : EMPTY_ACTIVE_SESSIONS))
  const workflowPresets = useGlobalPresetStore(state => (isOpen ? state.workflowPresets : EMPTY_WORKFLOW_PRESETS))
  const recentPresetOrder = useGlobalPresetStore(state => (isOpen ? state.recentPresetOrder : EMPTY_RECENT_PRESET_ORDER))
  const recentPresetAccessedAt = useGlobalPresetStore(state => (isOpen ? state.recentPresetAccessedAt : EMPTY_RECENT_PRESET_ACCESSED_AT))
  const totalEventStats = useMemo(() => {
    let eventCount = 0
    let sizeBytes = 0
    let largestEventBytes = 0
    let largestEventType = ''

    for (const events of Object.values(tabEvents)) {
      eventCount += events.length
    }

    return { eventCount, sizeBytes, largestEventBytes, largestEventType } satisfies EventMemoryStats
  }, [tabEvents])

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
  }, [isOpen, initialQuery])

  // Build a cross-mode command center: active work + chats + workflows + retained events.
  const allItems = useMemo<QuickSwitcherItem[]>(() => {
    if (!isOpen) return []

    const allTabs = Object.values(chatTabs)
    const tabsBySession = allTabs.reduce<Record<string, ChatTab[]>>((acc, tab) => {
      if (!tab.sessionId) return acc
      if (!acc[tab.sessionId]) acc[tab.sessionId] = []
      acc[tab.sessionId].push(tab)
      return acc
    }, {})

    const eventStatsBySession = Object.entries(tabEvents).reduce<Record<string, EventMemoryStats>>((acc, [sessionId, events]) => {
      const stats = lightweightEventStats(events)
      if (stats) acc[sessionId] = stats
      return acc
    }, {})
    const visibleActiveSessions = activeSessions.filter(isVisibleActiveSession)

    const chatItems: ChatTabItem[] = Object.values(chatTabs)
      .filter(tab => tab.metadata?.mode === 'multi-agent' && !tab.metadata?.isOrganizationAssistant)
      .sort((a, b) => a.createdAt - b.createdAt)
      .map(tab => {
        const eventStats = tab.sessionId ? eventStatsBySession[tab.sessionId] : undefined
        const activeSession = tab.sessionId ? visibleActiveSessions.find(session => session.session_id === tab.sessionId) : undefined
        return {
          type: 'chat' as const,
          id: `chat:${tab.tabId}`,
          label: tab.name,
          subtitle: `Chat · ${tab.isStreaming ? 'Streaming...' : tab.isCompleted ? 'Completed' : tab.sessionId ? 'Active' : 'New'}${activeSessionSuffix(activeSession)}${eventStatsSuffix(eventStats)}`,
          isActive: isChatMode && tab.tabId === activeTabId,
          lastAccessedAt: tab.lastAccessedAt || tab.createdAt || 0,
          tabId: tab.tabId,
          eventStats,
          activeSession,
        }
      })

    const workflowItems: WorkflowItem[] = workflowPresets
      .filter(p => p.selectedFolder?.filepath)
      .map(p => {
        const presetEvents = allTabs
          .filter(tab => tab.metadata?.mode === 'workflow' && tab.metadata?.presetQueryId === p.id && tab.sessionId)
          .flatMap(tab => tabEvents[tab.sessionId!] || [])
        const eventStats = lightweightEventStats(presetEvents)
        const activeSession = pickWorkflowActiveSession(visibleActiveSessions, p, chatTabs)
        return {
          type: 'workflow' as const,
          id: `workflow:${p.id}`,
          label: p.label,
          subtitle: `Workflow · ${p.selectedFolder!.filepath}${activeSessionSuffix(activeSession)}${eventStatsSuffix(eventStats)}`,
          isActive: isWorkflowMode && p.id === activePresetId,
          lastAccessedAt: recentPresetAccessedAt[p.id] || (() => {
            const recentIndex = recentPresetOrder.indexOf(p.id)
            return recentIndex >= 0 ? 1_000_000 - recentIndex : 0
          })(),
          preset: p,
          eventStats,
          activeSession,
        }
      })

    const activeItems: ActiveWorkItem[] = activeSessions
      .filter(isVisibleActiveSession)
      .filter(session => {
        const tab = findTabForSession(chatTabs, session.session_id)
        if (tab && !tab.metadata?.isOrganizationAssistant) return false
        if (
          isWorkflowSession(session) &&
          !isScheduledWorkflowSession(session) &&
          !isBotWorkflowSession(session) &&
          workflowPresets.some(preset => workflowSessionMatchesPreset(session, preset, chatTabs))
        ) return false
        return true
      })
      .map(session => {
        const tab = findTabForSession(chatTabs, session.session_id)
        const workflow = isWorkflowSession(session)
        const status = activeSessionStatusLabel(session)
        const current = session.current_execution_name ? ` · ${session.current_execution_name}` : ''
        const eventStats = eventStatsBySession[session.session_id]
        return {
          type: 'active' as const,
          id: `active:${session.session_id}`,
          label: activeSessionLabel(session),
          subtitle: `${workflow ? 'Active workflow' : 'Active chat'} · ${status}${current} · ${sessionShortId(session.session_id)}${eventStatsSuffix(eventStats)}`,
          isActive: !!tab && tab.tabId === activeTabId,
          lastAccessedAt: tab?.lastAccessedAt || tab?.createdAt || Date.parse(session.last_activity || session.created_at || '') || 0,
          session,
          tabId: tab?.tabId,
          mode: workflow ? 'workflow' : 'multi-agent',
          eventStats,
        }
      })

    const eventItems: EventMemoryItem[] = Object.entries(tabEvents)
      .map<EventMemoryItem | null>(([sessionId, events]) => {
        const stats = lightweightEventStats(events)
        if (!stats) return null
        const tabs = tabsBySession[sessionId] || []
        if (tabs.length > 0) return null
        const primaryTab = tabs[0]
        const tabNames = tabs.map(tab => tab.name).filter(Boolean)
        return {
          type: 'events' as const,
          id: `events:${sessionId}`,
          label: `Orphan events: ${tabNames.length > 0 ? tabNames.join(', ') : sessionShortId(sessionId)}`,
          subtitle: eventStatsLabel(stats),
          isActive: tabs.some(tab => tab.tabId === activeTabId),
          lastAccessedAt: Math.max(0, ...tabs.map(tab => tab.lastAccessedAt || tab.createdAt || 0)),
          sessionId,
          tabIds: tabs.map(tab => tab.tabId),
          mode: primaryTab?.metadata?.mode,
          stats,
        } satisfies EventMemoryItem
      })
      .filter((item): item is EventMemoryItem => item !== null)

    workflowItems.sort((a, b) => {
      const aIdx = recentPresetOrder.indexOf(a.preset.id)
      const bIdx = recentPresetOrder.indexOf(b.preset.id)
      if (aIdx !== -1 && bIdx !== -1) return aIdx - bIdx
      if (aIdx !== -1) return -1
      if (bIdx !== -1) return 1
      return a.label.localeCompare(b.label)
    })

    return [...activeItems, ...chatItems, ...workflowItems, ...eventItems].sort((a, b) => {
      if (a.isActive !== b.isActive) return a.isActive ? -1 : 1
      if (a.lastAccessedAt !== b.lastAccessedAt) return b.lastAccessedAt - a.lastAccessedAt
      if (a.type !== b.type) return itemTypeRank(a) - itemTypeRank(b)
      return a.label.localeCompare(b.label)
    })
  }, [isOpen, isWorkflowMode, isChatMode, activePresetId, chatTabs, tabEvents, activeSessions, activeTabId, workflowPresets, recentPresetOrder, recentPresetAccessedAt])

  // Filter and sort
  const filteredItems = useMemo<QuickSwitcherItem[]>(() => {
    const rawQuery = query.toLowerCase().trim()
    if (!rawQuery) return allItems

    const scopeMatch = rawQuery.match(/^@(active|events|workflows?|chats?|tabs)\s*/)
    const scope = scopeMatch?.[1] || null
    const q = scopeMatch ? rawQuery.slice(scopeMatch[0].length).trim() : rawQuery
    const scoped = scope
      ? allItems.filter(item => {
          if (scope === 'active') return !!itemActiveSession(item)
          if (scope === 'events') return item.type === 'events' || !!itemEventStats(item)
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

  // Switch to a workflow, chat tab, active session, or retained-event owner.
  const switchToTab = useCallback((tab: ChatTab) => {
    useAppStore.getState().setShowWorkflowsOverview(false)
    const tabMode = tab.metadata?.mode || 'multi-agent'
    if (useModeStore.getState().selectedModeCategory !== tabMode) {
      useModeStore.getState().setModeCategory(tabMode)
    }
    useChatStore.getState().switchTab(tab.tabId)
    if (tabMode === 'workflow') {
      useWorkflowStore.getState().setShowChatArea(true)
    }
    requestChatScrollToBottom()
  }, [])

  const handleSelect = useCallback(async (item: QuickSwitcherItem, minimize = false) => {
    if (item.type === 'active') {
      const chatStore = useChatStore.getState()
      const existingTab = item.tabId ? chatStore.getTab(item.tabId) : findTabForSession(chatStore.chatTabs, item.session.session_id)
      if (existingTab) {
        switchToTab(existingTab)
        onClose()
        return
      }

      if (item.mode === 'workflow') {
        if (isScheduledWorkflowSession(item.session)) {
          await restoreScheduledWorkflowRunChat(item.session)
        } else if (isBotWorkflowSession(item.session)) {
          await restoreBotWorkflowRunChat(item.session)
        } else {
          await restoreWorkflowSessionChat(item.session)
        }
        onClose()
        return
      }

      const restoredTabId = await restoreSession(item.session.session_id, {
        title: item.label,
        source: 'quick-switcher',
      })
      const restoredTab = useChatStore.getState().getTab(restoredTabId)
      if (restoredTab) switchToTab(restoredTab)
      onClose()
      return
    }

    if (item.type === 'events') {
      const chatStore = useChatStore.getState()
      const tab = item.tabIds.map(tabId => chatStore.getTab(tabId)).find(Boolean)
      if (tab) {
        switchToTab(tab)
      }
      onClose()
      return
    }

    if (item.type === 'chat') {
      console.log(`%c[QuickSwitcher] Switching to chat tab: ${item.label} (${item.tabId})`, 'color: #FF9800; font-weight: bold')
      useAppStore.getState().setShowWorkflowsOverview(false)
      if (useModeStore.getState().selectedModeCategory !== 'multi-agent') {
        useModeStore.getState().setModeCategory('multi-agent')
      }
      useChatStore.getState().switchTab(item.tabId)
      requestChatScrollToBottom()
      onClose()
      return
    }

    // Workflow switching
    console.log(`%c[QuickSwitcher] Switching to workflow: ${item.label?.slice(0,30)} (${item.id?.slice(0,8)})`, 'color: #FF9800; font-weight: bold')
    console.time('[QuickSwitcher] workflow-switch-total')
    const chatStore = useChatStore.getState()
    const presetStore = useGlobalPresetStore.getState()
    useAppStore.getState().setShowWorkflowsOverview(false)
    if (useModeStore.getState().selectedModeCategory !== 'workflow') {
      useModeStore.getState().setModeCategory('workflow')
    }

    if (minimize) {
      Object.values(chatStore.chatTabs).forEach(tab => {
        if (tab.metadata?.mode === 'workflow' && tab.metadata?.presetQueryId === item.preset.id) {
          chatStore.setTabViewMode(tab.tabId, 'tree')
        }
      })
    }

    presetStore.applyPreset(item.preset, 'workflow')

    const refreshedActiveSession = item.activeSession ||
      pickWorkflowActiveSession(await useChatStore.getState().getActiveSessions(true), item.preset, useChatStore.getState().chatTabs)

    if (refreshedActiveSession) {
      if (isScheduledWorkflowSession(refreshedActiveSession)) {
        await restoreScheduledWorkflowRunChat(refreshedActiveSession, { preset: item.preset })
      } else if (isBotWorkflowSession(refreshedActiveSession)) {
        await restoreBotWorkflowRunChat(refreshedActiveSession, { preset: item.preset })
      } else {
        await restoreWorkflowSessionChat(refreshedActiveSession, { preset: item.preset })
      }
      console.timeEnd('[QuickSwitcher] workflow-switch-total')
      onClose()
      return
    }

    // Switch to the correct tab for the new preset (the App.tsx effect only
    // runs on mode change, not on preset change within workflow mode)
    const updatedChatStore = useChatStore.getState()
    const currentTab = updatedChatStore.activeTabId ? updatedChatStore.getTab(updatedChatStore.activeTabId) : null
    const hasValidTab = currentTab &&
      currentTab.metadata?.mode === 'workflow' &&
      currentTab.metadata?.presetQueryId === item.preset.id

    if (!hasValidTab) {
      // Find the most recent workflow tab for the new preset
      const presetTabs = Object.values(updatedChatStore.chatTabs)
        .filter(tab => tab.metadata?.mode === 'workflow' && tab.metadata?.presetQueryId === item.preset.id && (tab.sessionId || tab.isStreaming))
        .sort((a, b) => b.createdAt - a.createdAt)

      if (presetTabs.length > 0) {
        updatedChatStore.switchTab(presetTabs[0].tabId)
        useWorkflowStore.getState().setShowChatArea(true)
      } else {
        // No tabs for this preset yet — create an empty builder tab. WorkflowLayout
        // watches this state and offers workspace builder-history restore when present.
        const tabId = await updatedChatStore.createChatTab('Workflow Builder', {
          mode: 'workflow',
          phaseId: 'workflow-builder',
          phaseName: 'Workflow Builder',
          presetQueryId: item.preset.id,
        })
        updatedChatStore.switchTab(tabId)
        useWorkflowStore.getState().setShowChatArea(true)
      }
    }

    console.timeEnd('[QuickSwitcher] workflow-switch-total')
    requestChatScrollToBottom()
    onClose()
  }, [onClose, switchToTab])

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

  const placeholder = 'Search workflows, chats, active work, or events...'
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
                  : item.type === 'events'
                    ? Database
                    : MessageSquare
              const stats = itemEventStats(item)
              const activeSession = itemActiveSession(item)
              const itemIsPressure = !!stats && hasEventMemoryPressure(stats)
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
                  <ItemIcon className={`w-4 h-4 flex-shrink-0 ${item.isActive ? 'text-blue-500' : itemIsPressure ? 'text-amber-500' : 'text-gray-400 dark:text-gray-500'}`} />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className={`text-sm font-medium truncate ${item.isActive ? 'text-blue-600 dark:text-blue-400' : 'text-gray-900 dark:text-gray-100'}`}>
                        {item.label}
                      </span>
                      <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-gray-100 dark:bg-gray-700 text-gray-500 dark:text-gray-300 font-medium flex-shrink-0">
                        {item.type === 'active' ? 'active' : item.type}
                      </span>
                      {activeSession && item.type !== 'active' && (
                        <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-emerald-100 dark:bg-emerald-900/40 text-emerald-700 dark:text-emerald-300 font-medium flex-shrink-0">
                          active
                        </span>
                      )}
                      {item.isActive && (
                        <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-blue-100 dark:bg-blue-900/50 text-blue-600 dark:text-blue-400 font-medium flex-shrink-0">
                          current
                        </span>
                      )}
                      {itemIsPressure && (
                        <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-300 font-medium flex-shrink-0">
                          high
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
          <div className="flex items-center justify-between gap-3 px-4 py-2 text-[11px] text-gray-500 dark:text-gray-400">
            <div className="min-w-0 truncate">
              Events retained: <span className="font-medium text-gray-700 dark:text-gray-200">{totalEventStats.eventCount.toLocaleString()}</span>
              {totalEventStats.sizeBytes > 0 && (
                <>
                  <span className="mx-1.5 text-gray-300 dark:text-gray-600">·</span>
                  Memory: <span className="font-medium text-gray-700 dark:text-gray-200">{formatEventMemoryBytes(totalEventStats.sizeBytes)}</span>
                  {totalEventStats.largestEventBytes > 0 && (
                    <>
                      <span className="mx-1.5 text-gray-300 dark:text-gray-600">·</span>
                      <span className="truncate">largest {totalEventStats.largestEventType || 'n/a'} ({formatEventMemoryBytes(totalEventStats.largestEventBytes)})</span>
                    </>
                  )}
                </>
              )}
            </div>
            <span className="hidden sm:inline flex-shrink-0">@active @events @workflows @chats</span>
          </div>
          <div className="px-4 py-2 border-t border-gray-200 dark:border-gray-700 text-[11px] text-gray-400 dark:text-gray-500 flex items-center justify-between">
            <div className="flex items-center gap-3 min-w-0">
              <span><kbd className="px-1 py-0.5 bg-gray-200 dark:bg-gray-600 rounded text-[10px]">↑↓</kbd> navigate</span>
              <span><kbd className="px-1 py-0.5 bg-gray-200 dark:bg-gray-600 rounded text-[10px]">↵</kbd> switch</span>
              {filteredItems.some(item => item.type === 'workflow') && (
                <span><kbd className="px-1 py-0.5 bg-amber-200 dark:bg-amber-800 text-amber-700 dark:text-amber-300 rounded text-[10px]">⇧↵</kbd> switch &amp; minimize</span>
              )}
            </div>
            <span className="flex-shrink-0"><kbd className="px-1 py-0.5 bg-gray-200 dark:bg-gray-600 rounded text-[10px]">esc</kbd> close</span>
          </div>
        </div>
      </div>
    </div>
  )
}

export default QuickSwitcher
