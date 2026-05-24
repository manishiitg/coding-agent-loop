import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { AlertCircle, Clock, Loader2, Pause } from 'lucide-react'
import type { ActiveSessionInfo, RunningWorkflowInfo, SessionExecutionTreeNode } from '../services/api-types'
import { agentApi } from '../services/api'
import { useChatStore, type ChatTab } from '../stores/useChatStore'
import { useModeStore } from '../stores/useModeStore'
import { useGlobalPresetStore } from '../stores/useGlobalPresetStore'
import { restoreSession } from '../utils/sessionRestore'
import { isBotWorkflowSession, isScheduledWorkflowSession, restoreBotWorkflowRunChat, restoreScheduledWorkflowRunChat, restoreWorkflowSessionChat, workflowSessionBotPlatform } from '../utils/workflowSessionRestore'

const ACTIVITY_DETAILS_POLL_MS = 30000

type RuntimeExecutionDetail = {
  label: string
  kind: string
  status: string
  startedAt?: string
}

function normalizedStatus(status?: string): string {
  return (status || '').toLowerCase().trim()
}

function isWorkflowSession(session: ActiveSessionInfo): boolean {
  return session.agent_mode?.toLowerCase().includes('workflow') ?? false
}

function isActiveSession(session: ActiveSessionInfo): boolean {
  const status = normalizedStatus(session.status)
  if (
    session.needs_user_input === true ||
    session.has_running_background_agents === true ||
    (session.running_background_agent_count ?? 0) > 0 ||
    status === 'running' ||
    status === 'paused' ||
    status === 'idle' ||
    status === 'waiting' ||
    status === 'waiting_feedback'
  ) return true

  // A completed workflow session is still "waiting for the user's next builder command".
  // Show it for up to 30 minutes after last activity — same cap as bg-agent max-age.
  if (status === 'completed' && isWorkflowSession(session) && session.last_activity) {
    const lastMs = new Date(session.last_activity).getTime()
    if (!Number.isNaN(lastMs) && Date.now() - lastMs < 30 * 60 * 1000) return true
  }
  return false
}

function sessionTitle(session: ActiveSessionInfo, workflow?: RunningWorkflowInfo, fallbackWorkflowName?: string | null): string {
  if (isWorkflowSession(session)) {
    const workflowFolder = (workflow?.workspace_path || session.workspace_path)?.split('/').filter(Boolean).pop()
    const hasBackgroundWork = session.has_running_background_agents || (session.running_background_agent_count ?? 0) > 0

    return (
      workflow?.preset_name ||
      session.preset_name ||
      session.workflow_name ||
      session.workflow_label ||
      workflow?.title ||
      session.title ||
      workflowFolder ||
      fallbackWorkflowName ||
      (hasBackgroundWork ? 'Workflow background task' : '') ||
      session.query ||
      'Workflow'
    )
  }

  return (
    session.current_execution_name ||
    session.title ||
    session.query ||
    (isWorkflowSession(session) ? 'Workflow' : 'Agent chat')
  )
}

function displaySessionTitle(
  session: ActiveSessionInfo,
  tab?: ChatTab,
  workflow?: RunningWorkflowInfo,
  fallbackWorkflowName?: string | null,
): string {
  if (isWorkflowSession(session)) {
    if (tab?.name && tab.name !== 'Workflow Builder') {
      return tab.name
    }
    return sessionTitle(session, workflow, fallbackWorkflowName)
  }

  return tab?.name || sessionTitle(session, workflow)
}

function shortText(value: string, limit = 72): string {
  return value.length > limit ? `${value.slice(0, limit - 1)}…` : value
}

function relativeTime(value?: string): string {
  if (!value) return ''
  const then = new Date(value).getTime()
  if (Number.isNaN(then)) return ''

  const seconds = Math.max(0, Math.floor((Date.now() - then) / 1000))
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m`
  const hours = Math.floor(minutes / 60)
  return `${hours}h ${minutes % 60}m`
}

function headerStatusLabel(session: ActiveSessionInfo, workflow?: RunningWorkflowInfo): string {
  if (session.needs_user_input) return 'needs input'
  const hasBackgroundAgents = session.has_running_background_agents === true || (session.running_background_agent_count ?? 0) > 0
  const status = normalizedStatus(workflow?.status || session.status)
  if (status === 'paused') return 'paused'
  if (status === 'idle') return 'idle'
  if ((status === 'waiting' || status === 'waiting_feedback') && hasBackgroundAgents) return 'waiting for background agents'
  if (status === 'waiting' || status === 'waiting_feedback') return 'waiting'
  if ((status === 'completed' || status === 'idle') && hasBackgroundAgents) return 'background running'
  if (status === 'completed' && isWorkflowSession(session)) return 'idle'
  return status || 'running'
}

function statusTone(session: ActiveSessionInfo, workflow?: RunningWorkflowInfo): 'running' | 'needs-input' | 'paused' | 'background' | 'idle' {
  const status = headerStatusLabel(session, workflow)
  if (status === 'needs input') return 'needs-input'
  if (status === 'idle' || status === 'waiting') return 'idle'
  if (status === 'paused') return 'paused'
  if (status === 'background running' || status === 'waiting for background agents') return 'background'
  return 'running'
}

function statusDotClasses(tone: ReturnType<typeof statusTone>): string {
  switch (tone) {
    case 'needs-input':
      return 'bg-amber-500 shadow-[0_0_0_2px_rgba(245,158,11,0.18)]'
    case 'idle':
      return 'bg-yellow-400 shadow-[0_0_0_2px_rgba(250,204,21,0.18)]'
    case 'paused':
      return 'bg-slate-400 shadow-[0_0_0_2px_rgba(148,163,184,0.18)]'
    case 'background':
      return 'bg-cyan-400 shadow-[0_0_0_2px_rgba(34,211,238,0.18)]'
    case 'running':
    default:
      return 'bg-emerald-400 shadow-[0_0_0_2px_rgba(52,211,153,0.18)]'
  }
}

function statusTextClasses(tone: ReturnType<typeof statusTone>): string {
  switch (tone) {
    case 'needs-input':
      return 'text-amber-700 dark:text-amber-200'
    case 'idle':
      return 'text-yellow-700 dark:text-yellow-200'
    case 'paused':
      return 'text-slate-600 dark:text-slate-300'
    case 'background':
      return 'text-cyan-700 dark:text-cyan-200'
    case 'running':
    default:
      return 'text-emerald-700 dark:text-emerald-200'
  }
}

function compactHeaderLabel(
  session: ActiveSessionInfo,
  tab?: ChatTab,
  workflow?: RunningWorkflowInfo,
  fallbackWorkflowName?: string | null,
): string {
  const title = shortText(displaySessionTitle(session, tab, workflow, fallbackWorkflowName), 28)
  const source = workflowSessionBotPlatform(session, workflow)
  const status = headerStatusLabel(session, workflow)
  const label = source ? `${source} · ${title}` : title
  return status === 'running' ? label : `${label} · ${status}`
}

function countLabel(count: number, singular: string, plural = `${singular}s`): string {
  return `${count} ${count === 1 ? singular : plural}`
}

function shortWorkflowHeaderName(name: string): string {
  return name.trim().slice(0, 5) || 'flow'
}

function hasWorkflowIdentity(session: ActiveSessionInfo, workflow?: RunningWorkflowInfo): boolean {
  return !!(
    workflow?.preset_name ||
    workflow?.title ||
    workflow?.workspace_path ||
    session.preset_name ||
    session.workflow_name ||
    session.workflow_label ||
    session.workspace_path
  )
}

function workflowFallbackName(workflow: RunningWorkflowInfo): string {
  return (
    workflow.preset_name ||
    workflow.title ||
    workflow.workspace_path?.split('/').filter(Boolean).pop() ||
    workflow.query ||
    'Workflow'
  )
}

function workflowNameFromPath(path?: string): string {
  return path?.split('/').filter(Boolean).pop() || ''
}

function workflowDisplayName(
  session: ActiveSessionInfo,
  workflow?: RunningWorkflowInfo,
  fallbackWorkflowName?: string | null,
): string {
  return (
    workflow?.preset_name ||
    session.preset_name ||
    session.workflow_name ||
    session.workflow_label ||
    workflowNameFromPath(workflow?.workspace_path || session.workspace_path) ||
    fallbackWorkflowName ||
    'Workflow'
  )
}

function findCurrentExecutionNode(node?: SessionExecutionTreeNode): SessionExecutionTreeNode | null {
  if (!node) return null

  let best: SessionExecutionTreeNode | null = node.status === 'running' ? node : null
  for (const child of node.children || []) {
    const candidate = findCurrentExecutionNode(child)
    if (candidate) {
      best = candidate
    }
  }

  if (best?.kind === 'session' || best?.kind === 'root') {
    return null
  }
  return best
}

function findLatestExecutionNode(node?: SessionExecutionTreeNode): SessionExecutionTreeNode | null {
  if (!node) return null

  let best: SessionExecutionTreeNode | null = node.kind === 'session' || node.kind === 'root' ? null : node
  for (const child of node.children || []) {
    const candidate = findLatestExecutionNode(child)
    if (!candidate) continue
    if (!best || new Date(candidate.started_at).getTime() > new Date(best.started_at).getTime()) {
      best = candidate
    }
  }
  return best
}

function executionKindLabel(kind: string): string {
  switch (kind) {
    case 'workflow_step':
      return 'step'
    case 'background_agent':
      return 'agent'
    case 'delegation':
      return 'delegation'
    case 'workflow':
      return 'workflow'
    default:
      return 'active'
  }
}

function currentWorkLabel(session: ActiveSessionInfo, workflow?: RunningWorkflowInfo, execution?: RuntimeExecutionDetail): string {
  if (execution?.label) {
    return `${executionKindLabel(execution.kind)}: ${execution.label}`
  }
  if (workflow?.current_step_title) return `step: ${workflow.current_step_title}`
  if (workflow?.current_step_id) return `step: ${workflow.current_step_id}`
  if (session.current_execution_name) return `active: ${session.current_execution_name}`
  return ''
}

function currentWorkStatus(session: ActiveSessionInfo, workflow?: RunningWorkflowInfo, execution?: RuntimeExecutionDetail): string {
  return normalizedStatus(execution?.status || workflow?.status || session.status) || 'running'
}

function joinCompactParts(parts: Array<string | false | null | undefined>): string {
  return parts.filter(Boolean).join(' · ')
}

function isGenericWorkflowTitle(title: string): boolean {
  const normalized = title.trim().toLowerCase()
  return normalized === 'workflow' || normalized === 'workflow background task'
}

function workNameFromLabel(label: string): string {
  return label.replace(/^(step|agent|delegation|workflow|active):\s*/i, '')
}

function sessionFromRunningWorkflow(workflow: RunningWorkflowInfo): ActiveSessionInfo {
  const timestamp = workflow.started_at || new Date().toISOString()
  return {
    session_id: workflow.session_id,
    observer_id: '',
    agent_mode: 'workflow',
    status: workflow.status || 'running',
    last_activity: timestamp,
    created_at: timestamp,
    query: workflowFallbackName(workflow),
    title: workflowFallbackName(workflow),
    triggered_by: workflow.triggered_by,
    needs_user_input: workflow.needs_user_input,
    waiting_message: workflow.waiting_message,
    waiting_since: workflow.waiting_since,
  }
}

export const GlobalActivityMonitor: React.FC = () => {
  const [open, setOpen] = useState(false)
  const [runningWorkflowsBySession, setRunningWorkflowsBySession] = useState<Record<string, RunningWorkflowInfo>>({})
  const [currentExecutionBySession, setCurrentExecutionBySession] = useState<Record<string, RuntimeExecutionDetail>>({})
  const containerRef = useRef<HTMLDivElement | null>(null)
  const activeSessionsCache = useChatStore(state => state.activeSessionsCache)
  const getActiveSessions = useChatStore(state => state.getActiveSessions)
  const activeTabId = useChatStore(state => state.activeTabId)
  const chatTabs = useChatStore(state => state.chatTabs)
  const selectedModeCategory = useModeStore(state => state.selectedModeCategory)
  const currentWorkflowPresetName = useGlobalPresetStore(state => {
    const presetId = state.activePresetIds.workflow
    return state.workflowPresets.find(preset => preset.id === presetId)?.label ?? null
  })

  useEffect(() => {
    const refresh = async () => {
      const active = await getActiveSessions(false).catch(() => [])
      try {
        const response = await agentApi.listRunningWorkflows()
        const running = response.running || []
        setRunningWorkflowsBySession(Object.fromEntries(
          running.map(workflow => [workflow.session_id, workflow]),
        ))
        const runningSessionIds = active
          .filter(isActiveSession)
          .map(session => session.session_id)
        const sessionIds = Array.from(new Set([
          ...runningSessionIds,
          ...running.map(workflow => workflow.session_id),
        ])).filter(Boolean).slice(0, 20)
        const treeResults = await Promise.allSettled(
          sessionIds.map(async sessionId => {
            const tree = await agentApi.getSessionExecutionTree(sessionId)
            const current = findCurrentExecutionNode(tree.root) || findLatestExecutionNode(tree.root)
            return current
              ? [sessionId, {
                label: current.name,
                kind: current.kind,
                status: current.status,
                startedAt: current.started_at,
              }] as const
              : null
          }),
        )
        setCurrentExecutionBySession(Object.fromEntries(
          treeResults
            .flatMap(result => result.status === 'fulfilled' && result.value ? [result.value] : []),
        ))
      } catch {
        setRunningWorkflowsBySession({})
        setCurrentExecutionBySession({})
      }
    }

    refresh()
    const interval = window.setInterval(() => {
      refresh()
    }, ACTIVITY_DETAILS_POLL_MS)
    return () => window.clearInterval(interval)
  }, [getActiveSessions])

  useEffect(() => {
    if (!open) return

    const onMouseDown = (event: MouseEvent) => {
      if (!containerRef.current?.contains(event.target as Node)) {
        setOpen(false)
      }
    }

    document.addEventListener('mousedown', onMouseDown)
    return () => document.removeEventListener('mousedown', onMouseDown)
  }, [open])

  const activeSessions = useMemo(() => {
    const bySession = new Map<string, ActiveSessionInfo>()

    for (const session of activeSessionsCache) {
      const workflow = runningWorkflowsBySession[session.session_id]
      bySession.set(session.session_id, workflow?.status
        ? { ...session, status: workflow.status }
        : session)
    }

    for (const workflow of Object.values(runningWorkflowsBySession)) {
      if (!bySession.has(workflow.session_id)) {
        bySession.set(workflow.session_id, sessionFromRunningWorkflow(workflow))
      }
    }

    return Array.from(bySession.values()).filter(isActiveSession)
  }, [activeSessionsCache, runningWorkflowsBySession])

  const currentSessionId = activeTabId ? chatTabs[activeTabId]?.sessionId ?? null : null
  const visibleSessions = useMemo(
    () => activeSessions.filter(session => session.session_id !== currentSessionId),
    [activeSessions, currentSessionId],
  )

  // Builder-idle indicator: find any tab OTHER than the currently active one that has
  // running background agents. We intentionally exclude the active tab because the user
  // can already see that tab's chat directly — the indicator is only useful when the
  // builder is running in the background while the user is looking at a different tab.
  const builderTab = useMemo(
    () => Object.values(chatTabs).find(tab => tab.tabId !== activeTabId && tab.hasRunningBgAgents),
    [chatTabs, activeTabId],
  )
  const builderHasBgAgents = !!builderTab
  const builderBusy = (builderTab?.isStreaming ?? false) || (builderTab?.isSyntheticTurn ?? false)

  const inputCount = useMemo(
    () => visibleSessions.filter(session => session.needs_user_input).length,
    [visibleSessions],
  )

  const workflowCount = useMemo(
    () => visibleSessions.filter(isWorkflowSession).length,
    [visibleSessions],
  )
  const chatCount = Math.max(0, visibleSessions.length - workflowCount)
  const missingWorkflowIdentityCount = useMemo(
    () => visibleSessions.filter(session => {
      const workflow = runningWorkflowsBySession[session.session_id]
      return isWorkflowSession(session) && !hasWorkflowIdentity(session, workflow)
    }).length,
    [visibleSessions, runningWorkflowsBySession],
  )

  const sortedSessions = useMemo(() => {
    return [...visibleSessions].sort((a, b) => {
      if (!!a.needs_user_input !== !!b.needs_user_input) {
        return a.needs_user_input ? -1 : 1
      }
      if (isWorkflowSession(a) !== isWorkflowSession(b)) {
        return isWorkflowSession(a) ? -1 : 1
      }
      return new Date(b.last_activity).getTime() - new Date(a.last_activity).getTime()
    })
  }, [visibleSessions])

  const primarySession = sortedSessions[0]

  const primaryTab = primarySession
    ? Object.values(chatTabs).find(item => item.sessionId === primarySession.session_id)
    : undefined
  const primaryWorkflow = primarySession
    ? runningWorkflowsBySession[primarySession.session_id]
    : undefined
  const primaryWorkflowFallbackName = primarySession &&
    selectedModeCategory === 'workflow' &&
    !primaryWorkflow
    ? currentWorkflowPresetName
    : null
  const primaryTone = primarySession ? statusTone(primarySession, primaryWorkflow) : 'running'
  const workflowHeaderNames = sortedSessions
    .filter(isWorkflowSession)
    .slice(0, 3)
    .map(session => shortWorkflowHeaderName(workflowDisplayName(
      session,
      runningWorkflowsBySession[session.session_id],
      selectedModeCategory === 'workflow' ? currentWorkflowPresetName : null,
    )))
  const workflowHeaderLabel = workflowHeaderNames.length > 0
    ? `${workflowHeaderNames.join(' · ')}${workflowCount > workflowHeaderNames.length ? ` +${workflowCount - workflowHeaderNames.length}` : ''}`
    : countLabel(workflowCount, 'workflow')
  const headerLabel = inputCount > 0
    ? `${workflowHeaderLabel}${chatCount > 0 ? ` · ${countLabel(chatCount, 'chat')}` : ''} · ${countLabel(inputCount, 'needs input', 'need input')}`
    : workflowCount > 0
      ? `${workflowHeaderLabel}${chatCount > 0 ? ` · ${countLabel(chatCount, 'chat')}` : ''}`
      : countLabel(visibleSessions.length, 'active')

  const openActiveWorkInQuickSwitcher = useCallback(() => {
    setOpen(false)
    window.dispatchEvent(new CustomEvent('open-quick-switcher', {
      detail: { query: '@active ' },
    }))
  }, [])

  const handleOpenSession = useCallback(async (session: ActiveSessionInfo) => {
    const chatStore = useChatStore.getState()
    const existingTab = Object.values(chatStore.chatTabs).find(tab => tab.sessionId === session.session_id)

    if (isWorkflowSession(session)) {
      const workflowInfo = runningWorkflowsBySession[session.session_id]
      if (isScheduledWorkflowSession(session, workflowInfo)) {
        await restoreScheduledWorkflowRunChat(session, { runningWorkflow: workflowInfo })
      } else if (isBotWorkflowSession(session, workflowInfo)) {
        await restoreBotWorkflowRunChat(session, { runningWorkflow: workflowInfo })
      } else {
        await restoreWorkflowSessionChat(session, { runningWorkflow: workflowInfo })
      }
      setOpen(false)
      return
    }

    if (existingTab) {
      if (existingTab.metadata?.mode === 'workflow' || existingTab.metadata?.mode === 'multi-agent') {
        useModeStore.getState().setModeCategory(existingTab.metadata.mode)
      }
      chatStore.switchTab(existingTab.tabId)
      setOpen(false)
      return
    }

    const tabId = await restoreSession(session.session_id, {
      title: sessionTitle(session, runningWorkflowsBySession[session.session_id], currentWorkflowPresetName),
      source: 'global-activity-monitor',
    })
    useModeStore.getState().setModeCategory('multi-agent')
    useChatStore.getState().switchTab(tabId)
    setOpen(false)
  }, [currentWorkflowPresetName, runningWorkflowsBySession])

  if (visibleSessions.length === 0 && !builderHasBgAgents) {
    return null
  }

  const builderStateLabel = builderBusy ? 'busy' : 'idle'
  const builderWorkflowName = (builderTab?.name && builderTab.name !== 'Workflow Builder')
    ? builderTab.name
    : currentWorkflowPresetName
  const builderChipLabel = builderWorkflowName
    ? `${builderWorkflowName} · ${builderStateLabel}`
    : `builder · ${builderStateLabel}`

  // Each session + the builder tab gets its own pill. Name length shrinks as pill count grows.
  const totalPillCount = visibleSessions.length + (builderHasBgAgents ? 1 : 0)
  const nameCharLimit = totalPillCount >= 3 ? 5 : totalPillCount === 2 ? 8 : 12
  const pillClasses = 'flex items-center gap-1 px-2 py-1 rounded-md border text-xs font-medium transition-colors border-blue-200 bg-blue-50 text-blue-700 hover:bg-blue-100 dark:border-blue-800/60 dark:bg-blue-950/40 dark:text-blue-300 dark:hover:bg-blue-950/60'

  return (
    <div ref={containerRef} className="relative flex items-center gap-1">
      {sortedSessions.map((session, i) => {
        const tab = Object.values(chatTabs).find(t => t.sessionId === session.session_id)
        const workflowInfo = runningWorkflowsBySession[session.session_id]
        const fallbackName = selectedModeCategory === 'workflow' && !workflowInfo ? currentWorkflowPresetName : null
        const tone = statusTone(session, workflowInfo)
        const name = shortText(displaySessionTitle(session, tab, workflowInfo, fallbackName), nameCharLimit)
        const statusLabel = headerStatusLabel(session, workflowInfo)
        const showStatus = statusLabel !== 'running'
        return (
          <React.Fragment key={session.session_id}>
            {i > 0 && <span className="text-gray-400 dark:text-gray-600 select-none text-xs">/</span>}
            <button
              type="button"
              data-tour={i === 0 ? 'active-work-switcher' : undefined}
              data-testid={i === 0 ? 'tour-active-work-switcher' : undefined}
              onClick={() => void handleOpenSession(session)}
              className={pillClasses}
              title={`${displaySessionTitle(session, tab, workflowInfo, fallbackName)} · ${statusLabel}`}
            >
              <span className={`h-1.5 w-1.5 rounded-full ${statusDotClasses(tone)}`} />
              <span className="whitespace-nowrap">{name}</span>
              {tone === 'needs-input' && <AlertCircle className="w-3 h-3 text-amber-500 dark:text-amber-400" />}
              {tone === 'idle' && <Clock className="w-3 h-3 opacity-50" />}
              {tone === 'paused' && <Pause className="w-3 h-3 opacity-50" />}
              {tone === 'background' && <Loader2 className="w-3 h-3 animate-spin opacity-60" />}
            </button>
          </React.Fragment>
        )
      })}
      {builderHasBgAgents && (
        <>
          {visibleSessions.length > 0 && <span className="text-gray-400 dark:text-gray-600 select-none text-xs">/</span>}
          <button
            type="button"
            onClick={() => builderTab && useChatStore.getState().switchTab(builderTab.tabId)}
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
        </>
      )}

      {false && open && (
        <div className="absolute right-0 top-full mt-2 w-[460px] max-w-[calc(100vw-2rem)] rounded-lg border border-gray-200 bg-white shadow-xl dark:border-gray-700 dark:bg-gray-900 z-50 overflow-hidden">
          <div className="px-3 py-2 border-b border-gray-200 dark:border-gray-700">
            <div className="flex items-center justify-between gap-3">
              <div className="text-xs font-semibold text-gray-900 dark:text-gray-100">Active work</div>
              <div className="text-[11px] text-gray-500 dark:text-gray-400">
                {countLabel(workflowCount, 'workflow')} · {countLabel(chatCount, 'chat')}
              </div>
            </div>
            <div className="mt-0.5 text-[11px] text-gray-500 dark:text-gray-400">
              {inputCount > 0 ? `${inputCount} waiting for input` : 'Click any row to switch to it'} · also in Ctrl+K
            </div>
          </div>

          <div className="max-h-80 overflow-y-auto">
            {sortedSessions.map(session => {
              const tab = Object.values(chatTabs).find(item => item.sessionId === session.session_id)
              const workflowInfo = runningWorkflowsBySession[session.session_id]
              const fallbackWorkflowName = selectedModeCategory === 'workflow' && !workflowInfo
                ? currentWorkflowPresetName
                : null
              const isActiveTab = !!tab && tab.tabId === activeTabId
              const workflow = isWorkflowSession(session)
              const bgCount = session.running_background_agent_count ?? 0
              const hasBgAgents = session.has_running_background_agents === true || bgCount > 0
              const bgAgentLabel = hasBgAgents
                ? bgCount > 0
                  ? `${bgCount} bg agent${bgCount === 1 ? '' : 's'}`
                  : 'bg agents running'
                : null
              const age = relativeTime(session.waiting_since || session.last_activity)
              const tone = statusTone(session, workflowInfo)
              const executionInfo = currentExecutionBySession[session.session_id]
              const activeWork = currentWorkLabel(session, workflowInfo, executionInfo)
              const activeWorkStatus = currentWorkStatus(session, workflowInfo, executionInfo)
              const title = displaySessionTitle(session, tab, workflowInfo, fallbackWorkflowName)
              const activeWorkName = workNameFromLabel(activeWork)
              const primaryTitle = workflow
                ? workflowDisplayName(session, workflowInfo, fallbackWorkflowName)
                : title
              const showActiveWork = activeWork && activeWorkName !== primaryTitle
              const secondaryLine = joinCompactParts([
                showActiveWork ? activeWorkName : null,
                activeWorkStatus,
                bgAgentLabel,
                session.waiting_message ? shortText(session.waiting_message, 64) : null,
              ])

              return (
                <button
                  key={session.session_id}
                  type="button"
                  onClick={() => void handleOpenSession(session)}
                  className={`w-full text-left px-3 py-2.5 border-b last:border-b-0 border-gray-100 dark:border-gray-800 transition-colors ${
                    isActiveTab
                      ? '!bg-[#17313a]'
                      : '!bg-transparent hover:!bg-[#2a2f35]'
                  }`}
                >
                  <div className="flex items-start gap-2">
                    <div className="mt-1 flex w-4 shrink-0 justify-center">
                      <span className={`h-2 w-2 rounded-full ${statusDotClasses(tone)}`} />
                    </div>

                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2 min-w-0">
                        <div className="truncate text-xs font-semibold text-gray-900 dark:text-gray-100">
                          {shortText(primaryTitle, 58)}
                        </div>
                        <span className="shrink-0 text-[10px] text-gray-400 dark:text-gray-500">
                          {workflow ? 'workflow' : 'chat'}
                        </span>
                        {isActiveTab && (
                          <span className="shrink-0 rounded-full bg-blue-100 px-1.5 py-0.5 text-[10px] font-medium text-blue-700 dark:bg-blue-900/50 dark:text-blue-200">
                            open
                          </span>
                        )}
                      </div>
                      {secondaryLine && (
                        <div
                          className={`mt-0.5 truncate text-[11px] ${session.needs_user_input ? 'text-amber-700 dark:text-amber-200' : statusTextClasses(tone)}`}
                          title={joinCompactParts([activeWork || headerStatusLabel(session, workflowInfo), activeWorkStatus, bgAgentLabel, session.waiting_message])}
                        >
                          {shortText(secondaryLine, 100)}
                        </div>
                      )}
                    </div>

                    {age && (
                      <div className="flex shrink-0 items-center gap-1 text-[10px] text-gray-400 dark:text-gray-500">
                        <Clock className="w-3 h-3" />
                        {age}
                      </div>
                    )}
                  </div>
                </button>
              )
            })}
          </div>
        </div>
      )}
    </div>
  )
}
