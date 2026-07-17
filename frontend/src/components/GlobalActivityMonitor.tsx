import React, { useCallback, useEffect, useMemo } from 'react'
import { AlertCircle, Clock, Loader2, Pause } from 'lucide-react'
import type { ActiveSessionInfo, RunningWorkflowInfo } from '../services/api-types'
import { useChatStore, type ChatTab } from '../stores/useChatStore'
import { useModeStore } from '../stores/useModeStore'
import { activateTab } from '../utils/activateTab'
import { useGlobalPresetStore } from '../stores/useGlobalPresetStore'
import { isScheduledWorkflowSession, openActiveSession } from '../utils/workflowSessionRestore'
import { useAppStore } from '../stores/useAppStore'
import { isLocalActivityFallbackTab } from '../utils/activityFallback'
import { hasIdleAliveCodingAgent, hasLiveBackgroundAgents, normalizedActivityStatus } from '../utils/activitySessions'
import { runtimeNeedsUserInput, sessionRuntimeStatus } from '../utils/runtimeActivity'

// This matches useChatStore's active-session cache TTL. A longer store TTL also
// increases the monitor's effective freshness window and should be changed here.
const ACTIVITY_DETAILS_POLL_MS = 30000
const MAX_INLINE_ACTIVITY_ITEMS = 2

type ActivityMonitorItem =
  | { type: 'session'; id: string; session: ActiveSessionInfo }
  | { type: 'builder-tab'; id: string; tab: ChatTab }

function normalizedStatus(status?: string): string {
  return normalizedActivityStatus(status)
}

function isWorkflowSession(session: ActiveSessionInfo): boolean {
  return session.agent_mode?.toLowerCase().includes('workflow') ?? false
}

function isActiveSession(session: ActiveSessionInfo): boolean {
  const status = sessionRuntimeStatus(session)

  // Scheduled/cron sessions: show only while actively running (so user can observe),
  // hide once completed — they don't need user attention after finishing.
  if (isScheduledWorkflowSession(session)) {
    return (
      status === 'busy' ||
      status === 'idle' ||
      hasLiveBackgroundAgents(session)
    )
  }

  // Idle-but-alive coding-agent CLI: a live tmux pane still holding agent context,
  // ready to resume the moment the user sends another message. Shown as idle (clock).
  if (hasIdleAliveCodingAgent(session)) return true

  if (
    runtimeNeedsUserInput(session) ||
    hasLiveBackgroundAgents(session) ||
    status === 'busy' ||
    status === 'idle'
  ) return true

  return false
}

function sessionTitle(session: ActiveSessionInfo, workflow?: RunningWorkflowInfo, fallbackWorkflowName?: string | null): string {
  if (isWorkflowSession(session)) {
    const workflowFolder = (workflow?.workspace_path || session.workspace_path)?.split('/').filter(Boolean).pop()
    const hasBackgroundWork = hasLiveBackgroundAgents(session)
    const scheduled = isScheduledWorkflowSession(session, workflow)

    if (scheduled) {
      return (
        workflow?.preset_name ||
        session.preset_name ||
        session.workflow_name ||
        session.workflow_label ||
        workflowFolder ||
        fallbackWorkflowName ||
        workflow?.title ||
        session.title ||
        session.query ||
        'Automation'
      )
    }

    return (
      workflow?.preset_name ||
      session.preset_name ||
      session.workflow_name ||
      session.workflow_label ||
      workflow?.title ||
      session.title ||
      workflowFolder ||
      fallbackWorkflowName ||
      (hasBackgroundWork ? 'Automation background task' : '') ||
      session.query ||
      'Automation'
    )
  }

  return (
    session.current_execution_name ||
    session.title ||
    session.query ||
    (isWorkflowSession(session) ? 'Automation' : 'Agent chat')
  )
}

function displaySessionTitle(
  session: ActiveSessionInfo,
  tab?: ChatTab,
  workflow?: RunningWorkflowInfo,
  fallbackWorkflowName?: string | null,
): string {
  if (isWorkflowSession(session)) {
    // For view-only (schedule/bot) tabs, tab.name is a type label ("Schedule", "WhatsApp"),
    // not the actual workflow name — skip it and resolve the real workflow title instead.
    const genericTabName = (tab?.name || '').trim().toLowerCase()
    const tabNameIsTypeLabel = genericTabName === 'schedule' ||
      genericTabName === 'scheduled run' ||
      genericTabName === 'bot' ||
      genericTabName === 'whatsapp' ||
      genericTabName === 'slack'
    if (tab?.name && tab.name !== 'Automation Builder' && !tab.metadata?.isViewOnly && !tabNameIsTypeLabel) {
      return tab.name
    }
    return sessionTitle(session, workflow, fallbackWorkflowName)
  }

  return tab?.name || sessionTitle(session, workflow)
}

function shortText(value: string, limit = 72): string {
  return value.length > limit ? `${value.slice(0, limit - 1)}…` : value
}

function headerStatusLabel(session: ActiveSessionInfo, workflow?: RunningWorkflowInfo): string {
  if (runtimeNeedsUserInput(session)) return 'waiting for input'
  const hasBackgroundAgents = hasLiveBackgroundAgents(session)
  if (session.runtime_state) {
    const status = sessionRuntimeStatus(session)
    if (status === 'idle') return hasBackgroundAgents ? 'waiting for background agents' : 'idle'
    if (status === 'stopped') return 'stopped'
    return hasBackgroundAgents && !session.runtime_state.foreground_turn.busy
      ? 'background running'
      : 'running'
  }
  const status = normalizedStatus(workflow?.status || session.status)
  if (status === 'paused') return 'paused'
  if (status === 'idle') return 'idle'
  if ((status === 'waiting' || status === 'waiting_feedback') && hasBackgroundAgents) return 'waiting for background agents'
  if (status === 'waiting' || status === 'waiting_feedback') return 'waiting'
  if ((status === 'completed' || status === 'idle') && hasBackgroundAgents) return 'background running'
  // Idle-but-alive coding CLI (backend marked it completed once the turn ended, but
  // the tmux agent is still up waiting for input): show it as idle (clock), never as
  // a spinner. Precedence: a genuinely-running/busy session keeps status "running".
  if ((status === 'completed' || status === 'idle') && hasIdleAliveCodingAgent(session)) return 'idle'
  if (status === 'completed' && isWorkflowSession(session)) return 'idle'
  return status || 'running'
}

function statusTone(session: ActiveSessionInfo, workflow?: RunningWorkflowInfo): 'running' | 'needs-input' | 'paused' | 'background' | 'idle' {
  const status = headerStatusLabel(session, workflow)
  if (status === 'waiting for input') return 'needs-input'
  if (status === 'idle' || status === 'waiting') return 'idle'
  if (status === 'paused') return 'paused'
  if (status === 'background running' || status === 'waiting for background agents') return 'background'
  return 'running'
}

function normalizedActivityIdentity(value?: string | null): string {
  return (value || '').trim().replace(/\/+$/, '').toLowerCase()
}

function pushActivityKey(keys: string[], prefix: string, value?: string | null): void {
  const normalized = normalizedActivityIdentity(value)
  if (normalized) keys.push(`${prefix}:${normalized}`)
}

function activityKeysForSession(session: ActiveSessionInfo, workflow?: RunningWorkflowInfo): string[] {
  const keys: string[] = []
  pushActivityKey(keys, 'session', session.session_id)
  pushActivityKey(keys, 'preset', session.preset_query_id || workflow?.preset_query_id)
  pushActivityKey(keys, 'workspace', workflow?.workspace_path || session.workspace_path)
  return keys
}

function activityKeysForTab(tab: ChatTab): string[] {
  const keys: string[] = []
  pushActivityKey(keys, 'session', tab.sessionId)
  pushActivityKey(keys, 'preset', tab.metadata?.presetQueryId)
  return keys
}

export const GlobalActivityMonitor: React.FC = () => {
  const activeSessionsCache = useChatStore(state => state.activeSessionsCache)
  const getActiveSessions = useChatStore(state => state.getActiveSessions)
  const activeTabId = useChatStore(state => state.activeTabId)
  const chatTabs = useChatStore(state => state.chatTabs)
  const selectedModeCategory = useModeStore(state => state.selectedModeCategory)
  const showWorkflowsOverview = useAppStore(state => state.showWorkflowsOverview)
  const currentWorkflowPresetName = useGlobalPresetStore(state => {
    const presetId = state.activePresetIds.workflow
    return state.workflowPresets.find(preset => preset.id === presetId)?.label ?? null
  })

  useEffect(() => {
    let disposed = false
    let refreshPromise: Promise<void> | null = null

    const refresh = async () => {
      await getActiveSessions().catch(() => [])
      if (disposed) return
    }

    const requestRefresh = () => {
      if (document.hidden || refreshPromise) return
      refreshPromise = refresh().finally(() => {
        refreshPromise = null
      })
    }
    const handleVisibilityChange = () => requestRefresh()

    requestRefresh()
    const interval = window.setInterval(requestRefresh, ACTIVITY_DETAILS_POLL_MS)
    document.addEventListener('visibilitychange', handleVisibilityChange)
    return () => {
      disposed = true
      window.clearInterval(interval)
      document.removeEventListener('visibilitychange', handleVisibilityChange)
    }
  }, [getActiveSessions])

  const activeSessions = useMemo(() => {
    return activeSessionsCache.filter(isActiveSession)
  }, [activeSessionsCache])

  const currentSessionId = useMemo(() => {
    if (showWorkflowsOverview || !activeTabId) return null
    const activeTab = chatTabs[activeTabId]
    if (!activeTab || activeTab.metadata?.mode !== selectedModeCategory) return null
    return activeTab.sessionId ?? null
  }, [activeTabId, chatTabs, selectedModeCategory, showWorkflowsOverview])
  const visibleSessions = useMemo(() => {
    const filtered = activeSessions.filter(session => session.session_id !== currentSessionId)

    // De-duplicate by workflow: if multiple sessions share the same workflow
    // identity, keep the most useful authoritative session row. A retained tmux
    // wins because it can reopen the live pane.
    const workflowKey = (s: ActiveSessionInfo) => s.workflow_name || s.workflow_label || s.workspace_path || ''
    const byWorkflow = new Map<string, ActiveSessionInfo>()
    const nonWorkflow: ActiveSessionInfo[] = []
    const rank = (s: ActiveSessionInfo) => {
      const st = sessionRuntimeStatus(s)
      let score = 0
      if (st === 'busy') score += 30
      if (st === 'idle') score += 10
      if (hasLiveBackgroundAgents(s)) score += 20
      if (runtimeNeedsUserInput(s)) score += 15
      if (s.has_retained_tmux_session) score += 50
      return score
    }
    const timestamp = (s: ActiveSessionInfo) =>
      Date.parse(s.last_activity || s.created_at || '') || 0

    for (const session of filtered) {
      const key = isWorkflowSession(session) ? workflowKey(session) : ''
      if (!key) {
        nonWorkflow.push(session)
        continue
      }
      const existing = byWorkflow.get(key)
      if (!existing) {
        byWorkflow.set(key, session)
        continue
      }
      const rankDelta = rank(session) - rank(existing)
      if (rankDelta > 0 || (rankDelta === 0 && timestamp(session) > timestamp(existing))) {
        byWorkflow.set(key, session)
      }
    }

    return [...byWorkflow.values(), ...nonWorkflow]
  }, [activeSessions, currentSessionId])

  const visibleActivityKeys = useMemo(() => {
    const keys = new Set<string>()
    visibleSessions.forEach(session => {
      activityKeysForSession(session)
        .forEach(key => keys.add(key))
    })
    return keys
  }, [visibleSessions])

  // Local tab background-agent state is only a fallback. If the backend already
  // exposes the same session/preset/workflow, render the backend-backed item once.
  const fallbackBuilderTabs = useMemo(
    () => Object.values(chatTabs).filter(tab =>
      tab.tabId !== activeTabId &&
      isLocalActivityFallbackTab(tab) &&
      !activityKeysForTab(tab).some(key => visibleActivityKeys.has(key))
    ),
    [chatTabs, activeTabId, visibleActivityKeys],
  )

  const sortedSessions = useMemo(() => {
    return [...visibleSessions].sort((a, b) => {
      if (runtimeNeedsUserInput(a) !== runtimeNeedsUserInput(b)) {
        return runtimeNeedsUserInput(a) ? -1 : 1
      }
      if (isWorkflowSession(a) !== isWorkflowSession(b)) {
        return isWorkflowSession(a) ? -1 : 1
      }
      return new Date(b.last_activity).getTime() - new Date(a.last_activity).getTime()
    })
  }, [visibleSessions])

  const activityItems = useMemo<ActivityMonitorItem[]>(() => [
    ...sortedSessions.map(session => ({
      type: 'session' as const,
      id: `session:${session.session_id}`,
      session,
    })),
    ...fallbackBuilderTabs.map(tab => ({
      type: 'builder-tab' as const,
      id: `builder-tab:${tab.tabId}`,
      tab,
    })),
  ], [sortedSessions, fallbackBuilderTabs])

  const dockRunningActivityCount = useMemo(() => {
    const busySessionIds = new Set<string>()
    for (const session of activeSessions) {
      const tone = statusTone(session)
      if (tone === 'running' || tone === 'background' || tone === 'needs-input') {
        busySessionIds.add(session.session_id)
      }
    }

    let localBusyTabCount = 0
    for (const tab of Object.values(chatTabs)) {
      if (!tab.isStreaming && !tab.isSyntheticTurn) continue
      if (tab.sessionId && busySessionIds.has(tab.sessionId)) continue
      localBusyTabCount += 1
    }

    return busySessionIds.size + localBusyTabCount
  }, [activeSessions, chatTabs])

  useEffect(() => {
    const api = (window as Window & { electronAPI?: { setRunningActivity?: (value: { count: number }) => void } }).electronAPI
    if (!api?.setRunningActivity) return
    api.setRunningActivity({ count: dockRunningActivityCount })
  }, [dockRunningActivityCount])

  useEffect(() => {
    return () => {
      const api = (window as Window & { electronAPI?: { setRunningActivity?: (value: { count: number }) => void } }).electronAPI
      api?.setRunningActivity?.({ count: 0 })
    }
  }, [])

  const openActiveWorkInQuickSwitcher = useCallback(() => {
    window.dispatchEvent(new CustomEvent('open-quick-switcher', {
      detail: { query: '@active ' },
    }))
  }, [])

  const handleOpenSession = useCallback(async (session: ActiveSessionInfo) => {
    // Shared path with the Ctrl+K quick-switcher so opening the same session
    // behaves identically from either surface.
    await openActiveSession(session, {
      title: sessionTitle(session, undefined, currentWorkflowPresetName),
      source: 'global-activity-monitor',
    })
  }, [currentWorkflowPresetName])

  if (activityItems.length === 0) {
    return null
  }

  const inlineActivityItems = activityItems.slice(0, MAX_INLINE_ACTIVITY_ITEMS)
  const overflowActivityCount = Math.max(0, activityItems.length - inlineActivityItems.length)
  // Keep the header compact: show one or two direct-jump pills, then send the
  // rest to Ctrl+K's @active view.
  const totalPillCount = inlineActivityItems.length + (overflowActivityCount > 0 ? 1 : 0)
  const nameCharLimit = totalPillCount >= 3 ? 5 : totalPillCount === 2 ? 8 : 12
  const pillClasses = 'flex items-center gap-1 px-2 py-1 rounded-md border text-xs font-medium transition-colors border-blue-200 bg-blue-50 text-blue-700 hover:bg-blue-100 dark:border-blue-800/60 dark:bg-blue-950/40 dark:text-blue-300 dark:hover:bg-blue-950/60'

  return (
    <div className="relative flex items-center gap-1">
      {inlineActivityItems.map((item, i) => {
        if (item.type === 'builder-tab') {
          const builderBusy = item.tab.isStreaming || item.tab.isSyntheticTurn
          const builderWorkflowName = (item.tab.name && item.tab.name !== 'Automation Builder')
            ? item.tab.name
            : currentWorkflowPresetName

          return (
            <React.Fragment key={item.id}>
              {i > 0 && <span className="text-gray-400 dark:text-gray-600 select-none text-xs">/</span>}
              <button
                type="button"
                onClick={() => activateTab(item.tab.tabId)}
                className={pillClasses}
                title={builderBusy ? 'Builder is processing — wait before sending a message' : 'Builder is idle — ready for your next message'}
              >
                {builderBusy
                  ? <Loader2 className="w-3.5 h-3.5 animate-spin" />
                  : <span className="h-1.5 w-1.5 rounded-full bg-emerald-400 dark:bg-emerald-300 animate-pulse" />}
                <span className="whitespace-nowrap">
                  {shortText(builderWorkflowName || 'builder', nameCharLimit)}
                </span>
              </button>
            </React.Fragment>
          )
        }

        const session = item.session
        const tab = Object.values(chatTabs).find(t => t.sessionId === session.session_id)
        const fallbackName = selectedModeCategory === 'workflow' ? currentWorkflowPresetName : null
        const tone = statusTone(session)
        const title = displaySessionTitle(session, tab, undefined, fallbackName)
        const statusLabel = headerStatusLabel(session)
        // End user only cares about two states: is it working, or is it waiting for me?
        // The icon alone conveys this — spinner = running, amber alert = waiting for input.
        // No status text at all; full detail stays in the hover tooltip.
        const isWorking = tone === 'running' || tone === 'background'
        const name = shortText(title, nameCharLimit)
        const waitingTitle = session.waiting_message ? ` · ${session.waiting_message}` : ''
        return (
          <React.Fragment key={item.id}>
            {i > 0 && <span className="text-gray-400 dark:text-gray-600 select-none text-xs">/</span>}
            <button
              type="button"
              data-tour={i === 0 ? 'active-work-switcher' : undefined}
              data-testid={i === 0 ? 'tour-active-work-switcher' : undefined}
              onClick={() => void handleOpenSession(session)}
              className={pillClasses}
              title={`${title} · ${statusLabel}${waitingTitle}`}
            >
              {tone === 'needs-input'
                ? <AlertCircle className="w-3.5 h-3.5 text-amber-500 dark:text-amber-400" />
                : isWorking
                  ? <Loader2 className="w-3.5 h-3.5 animate-spin opacity-70" />
                  : tone === 'paused'
                    ? <Pause className="w-3.5 h-3.5 opacity-50" />
                    : <Clock className="w-3.5 h-3.5 opacity-50" />}
              <span className="whitespace-nowrap">{name}</span>
            </button>
          </React.Fragment>
        )
      })}

      {overflowActivityCount > 0 && (
        <>
          {inlineActivityItems.length > 0 && <span className="text-gray-400 dark:text-gray-600 select-none text-xs">/</span>}
          <button
            type="button"
            onClick={openActiveWorkInQuickSwitcher}
            className={pillClasses}
            title={`Open ${overflowActivityCount} more active item${overflowActivityCount === 1 ? '' : 's'} in Ctrl+K`}
            aria-label={`Open ${overflowActivityCount} more active item${overflowActivityCount === 1 ? '' : 's'} in Ctrl+K`}
          >
            <span className="whitespace-nowrap">+{overflowActivityCount}</span>
          </button>
        </>
      )}

    </div>
  )
}
