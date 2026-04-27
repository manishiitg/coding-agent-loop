import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { AlertCircle, ChevronDown, Clock, Loader2 } from 'lucide-react'
import type { ActiveSessionInfo, RunningWorkflowInfo, SessionExecutionTreeNode } from '../services/api-types'
import { agentApi } from '../services/api'
import { useChatStore, type ChatTab } from '../stores/useChatStore'
import { useModeStore } from '../stores/useModeStore'
import { useGlobalPresetStore } from '../stores/useGlobalPresetStore'
import { restoreSession } from '../utils/sessionRestore'

const ACTIVE_SESSION_POLL_MS = 8000

type RuntimeExecutionDetail = {
  label: string
  kind: string
}

function normalizedStatus(status?: string): string {
  return (status || '').toLowerCase().trim()
}

function isWorkflowSession(session: ActiveSessionInfo): boolean {
  return session.agent_mode?.toLowerCase().includes('workflow') ?? false
}

function isActiveSession(session: ActiveSessionInfo): boolean {
  const status = normalizedStatus(session.status)
  return (
    session.needs_user_input === true ||
    session.has_running_background_agents === true ||
    (session.running_background_agent_count ?? 0) > 0 ||
    status === 'running' ||
    status === 'paused' ||
    status === 'idle' ||
    status === 'waiting' ||
    status === 'waiting_feedback'
  )
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
  const status = normalizedStatus(workflow?.status || session.status)
  if (status === 'paused') return 'paused'
  if (status === 'idle') return 'idle'
  if (status === 'waiting' || status === 'waiting_feedback') return 'waiting'
  if (status === 'completed' && (session.running_background_agent_count ?? 0) > 0) return 'background running'
  return status || 'running'
}

function statusTone(session: ActiveSessionInfo, workflow?: RunningWorkflowInfo): 'running' | 'needs-input' | 'paused' | 'background' | 'idle' {
  const status = headerStatusLabel(session, workflow)
  if (status === 'needs input') return 'needs-input'
  if (status === 'idle' || status === 'waiting') return 'idle'
  if (status === 'paused') return 'paused'
  if (status === 'background running') return 'background'
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
  const status = headerStatusLabel(session, workflow)
  return status === 'running' || status === 'background running' ? title : `${title} · ${status}`
}

function countLabel(count: number, singular: string, plural = `${singular}s`): string {
  return `${count} ${count === 1 ? singular : plural}`
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

function workflowDetailParts(session: ActiveSessionInfo, workflow?: RunningWorkflowInfo, execution?: RuntimeExecutionDetail): string[] {
  const parts: string[] = []

  if (execution?.label) {
    parts.push(`${executionKindLabel(execution.kind)}: ${execution.label}`)
  } else if (workflow?.current_step_title) {
    parts.push(`step: ${workflow.current_step_title}`)
  } else if (workflow?.current_step_id) {
    parts.push(`step: ${workflow.current_step_id}`)
  } else if (session.current_execution_name) {
    parts.push(`active: ${session.current_execution_name}`)
  }

  if (workflow?.run_folder) {
    parts.push(workflow.run_folder)
  }

  if (workflow?.triggered_by) {
    parts.push(workflow.triggered_by === 'cron' ? 'schedule' : workflow.triggered_by)
  }

  const workspacePath = workflow?.workspace_path || session.workspace_path
  if (workspacePath) {
    parts.push(workspacePath)
  }

  return parts
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
      getActiveSessions(true).catch(() => undefined)
      try {
        const response = await agentApi.listRunningWorkflows()
        const running = response.running || []
        setRunningWorkflowsBySession(Object.fromEntries(
          running.map(workflow => [workflow.session_id, workflow]),
        ))
        const treeResults = await Promise.allSettled(
          running.slice(0, 10).map(async workflow => {
            const tree = await agentApi.getSessionExecutionTree(workflow.session_id)
            const current = findCurrentExecutionNode(tree.root)
            return current
              ? [workflow.session_id, { label: current.name, kind: current.kind }] as const
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
    }, ACTIVE_SESSION_POLL_MS)
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

  const inputCount = useMemo(
    () => activeSessions.filter(session => session.needs_user_input).length,
    [activeSessions],
  )

  const workflowCount = useMemo(
    () => activeSessions.filter(isWorkflowSession).length,
    [activeSessions],
  )
  const chatCount = Math.max(0, activeSessions.length - workflowCount)
  const missingWorkflowIdentityCount = useMemo(
    () => activeSessions.filter(session => {
      const workflow = runningWorkflowsBySession[session.session_id]
      return isWorkflowSession(session) && !hasWorkflowIdentity(session, workflow)
    }).length,
    [activeSessions, runningWorkflowsBySession],
  )

  const sortedSessions = useMemo(() => {
    return [...activeSessions].sort((a, b) => {
      if (!!a.needs_user_input !== !!b.needs_user_input) {
        return a.needs_user_input ? -1 : 1
      }
      if (isWorkflowSession(a) !== isWorkflowSession(b)) {
        return isWorkflowSession(a) ? -1 : 1
      }
      return new Date(b.last_activity).getTime() - new Date(a.last_activity).getTime()
    })
  }, [activeSessions])

  const primarySession = sortedSessions[0]

  const primaryTab = primarySession
    ? Object.values(chatTabs).find(item => item.sessionId === primarySession.session_id)
    : undefined
  const primaryWorkflow = primarySession
    ? runningWorkflowsBySession[primarySession.session_id]
    : undefined
  const primaryWorkflowFallbackName = primarySession &&
    selectedModeCategory === 'workflow' &&
    !primaryWorkflow &&
    missingWorkflowIdentityCount === 1
    ? currentWorkflowPresetName
    : null
  const primaryTone = primarySession ? statusTone(primarySession, primaryWorkflow) : 'running'
  const headerLabel = inputCount > 0
    ? `${countLabel(inputCount, 'needs input', 'need input')} · ${countLabel(activeSessions.length, 'active')}`
    : workflowCount > 0
      ? `${countLabel(workflowCount, 'workflow')}${chatCount > 0 ? ` · ${countLabel(chatCount, 'chat')}` : ''}`
      : countLabel(activeSessions.length, 'active')

  const handleOpenSession = useCallback(async (session: ActiveSessionInfo) => {
    const chatStore = useChatStore.getState()
    const existingTab = Object.values(chatStore.chatTabs).find(tab => tab.sessionId === session.session_id)

    if (existingTab) {
      if (existingTab.metadata?.mode === 'workflow' || existingTab.metadata?.mode === 'multi-agent') {
        useModeStore.getState().setModeCategory(existingTab.metadata.mode)
      }
      chatStore.switchTab(existingTab.tabId)
      setOpen(false)
      return
    }

    if (isWorkflowSession(session)) {
      useModeStore.getState().setModeCategory('workflow')
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

  if (activeSessions.length === 0) {
    return null
  }

  return (
    <div ref={containerRef} className="relative">
      <button
        type="button"
        onClick={() => setOpen(value => !value)}
        className="relative flex items-center gap-2 px-2.5 py-1 rounded-md border text-xs font-medium transition-colors border-blue-200 bg-blue-50 text-blue-700 hover:bg-blue-100 dark:border-blue-700/50 dark:bg-blue-900/20 dark:text-blue-200 dark:hover:bg-blue-900/35"
        aria-expanded={open}
        aria-label="Open running work activity"
      >
        <span className={`h-2 w-2 rounded-full ${statusDotClasses(primaryTone)}`} />
        {primaryTone === 'needs-input' ? (
          <AlertCircle className="w-3.5 h-3.5" />
        ) : (
          <Loader2 className="w-3.5 h-3.5 animate-spin" />
        )}
        <span className="whitespace-nowrap">
          {activeSessions.length === 1 && primarySession
            ? compactHeaderLabel(primarySession, primaryTab, primaryWorkflow, primaryWorkflowFallbackName)
            : headerLabel}
        </span>
        <ChevronDown className={`w-3 h-3 transition-transform ${open ? 'rotate-180' : ''}`} />
      </button>

      {open && (
        <div className="absolute right-0 top-full mt-2 w-[460px] max-w-[calc(100vw-2rem)] rounded-lg border border-gray-200 bg-white shadow-xl dark:border-gray-700 dark:bg-gray-900 z-50 overflow-hidden">
          <div className="px-3 py-2 border-b border-gray-200 dark:border-gray-700">
            <div className="flex items-center justify-between gap-3">
              <div className="text-xs font-semibold text-gray-900 dark:text-gray-100">Active work</div>
              <div className="text-[11px] text-gray-500 dark:text-gray-400">
                {countLabel(workflowCount, 'workflow')} · {countLabel(chatCount, 'chat')}
              </div>
            </div>
            <div className="mt-0.5 text-[11px] text-gray-500 dark:text-gray-400">
              {inputCount > 0 ? `${inputCount} waiting for input` : 'Click any row to switch to it'}
            </div>
          </div>

          <div className="max-h-80 overflow-y-auto">
            {sortedSessions.map(session => {
              const tab = Object.values(chatTabs).find(item => item.sessionId === session.session_id)
              const workflowInfo = runningWorkflowsBySession[session.session_id]
              const fallbackWorkflowName = selectedModeCategory === 'workflow' && !workflowInfo && missingWorkflowIdentityCount === 1
                ? currentWorkflowPresetName
                : null
              const isActiveTab = !!tab && tab.tabId === activeTabId
              const workflow = isWorkflowSession(session)
              const bgCount = session.running_background_agent_count ?? 0
              const age = relativeTime(session.waiting_since || session.last_activity)
              const tone = statusTone(session, workflowInfo)
              const executionInfo = currentExecutionBySession[session.session_id]
              const detailParts = workflowDetailParts(session, workflowInfo, executionInfo)

              return (
                <button
                  key={session.session_id}
                  type="button"
                  onClick={() => void handleOpenSession(session)}
                  className={`w-full text-left px-3 py-2.5 border-b last:border-b-0 border-gray-100 dark:border-gray-800 transition-colors ${
                    isActiveTab
                      ? 'bg-blue-50/80 dark:bg-blue-500/10'
                      : 'hover:bg-gray-100/70 dark:hover:bg-white/[0.04]'
                  }`}
                >
                  <div className="flex items-start gap-2">
                    <div className="mt-1 flex w-4 shrink-0 justify-center">
                      <span className={`h-2 w-2 rounded-full ${statusDotClasses(tone)}`} />
                    </div>

                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2 min-w-0">
                        <div className="truncate text-xs font-semibold text-gray-900 dark:text-gray-100">
                          {shortText(displaySessionTitle(session, tab, workflowInfo, fallbackWorkflowName), 58)}
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
                      <div className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[11px] text-gray-500 dark:text-gray-400">
                        <span className={statusTextClasses(tone)}>{headerStatusLabel(session, workflowInfo)}</span>
                        {bgCount > 0 && (
                          <>
                            <span>·</span>
                            <span>{bgCount} bg agent{bgCount === 1 ? '' : 's'}</span>
                          </>
                        )}
                      </div>
                      {detailParts.length > 0 && (
                        <div className="mt-1 space-y-0.5">
                          <div className="truncate text-[11px] text-gray-700 dark:text-gray-300" title={detailParts[0]}>
                            {shortText(detailParts[0], 74)}
                          </div>
                          {detailParts.length > 1 && (
                            <div className="truncate text-[10px] text-gray-400 dark:text-gray-500" title={detailParts.slice(1).join(' · ')}>
                              {shortText(detailParts.slice(1).join(' · '), 86)}
                            </div>
                          )}
                        </div>
                      )}
                      {session.waiting_message && (
                        <div className="mt-1 truncate text-[11px] text-amber-700 dark:text-amber-200">
                          {shortText(session.waiting_message, 96)}
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
