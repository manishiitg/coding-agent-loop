import { activateTab } from './activateTab'
import { restoreSession } from './sessionRestore'
import { agentApi } from '../services/api'
import type { ActiveSessionInfo, RunningWorkflowInfo } from '../services/api-types'
import { useAppStore } from '../stores/useAppStore'
import { useChatStore, type ChatTab } from '../stores/useChatStore'
import { useGlobalPresetStore } from '../stores/useGlobalPresetStore'
import { useModeStore } from '../stores/useModeStore'
import { useRunningWorkflowsStore } from '../stores/useRunningWorkflowsStore'
import { useWorkflowStore } from '../stores/useWorkflowStore'
import type { CustomPreset, PredefinedPreset } from '../types/preset'
import { liveWorkflowTerminalSessionForPreset } from './workflowTerminalActivity'

type RestoreWorkflowSessionOptions = {
  preset?: CustomPreset | PredefinedPreset
  runningWorkflow?: RunningWorkflowInfo
  scrollToBottom?: boolean
}

type OpenWorkflowPresetPageOptions = {
  activeSession?: ActiveSessionInfo
  runningWorkflow?: RunningWorkflowInfo
  title?: string
  source?: string
  scrollToBottom?: boolean
}

const REPORT_PREVIEW_PREFERENCE_KEY = 'workflow_report_preview_preference'
const REPORT_PREVIEW_PREFERENCE_CHANGED_EVENT = 'workflow-report-preview-preference-changed'
const WORKFLOW_TERMINAL_LOOKUP_TIMEOUT_MS = 1500

function normalizeWorkspacePath(path?: string): string {
  return (path || '').replace(/\/+$/, '')
}

function reportPreviewPreferenceKey(scopeId?: string | null): string {
  return scopeId ? `${REPORT_PREVIEW_PREFERENCE_KEY}:${scopeId}` : REPORT_PREVIEW_PREFERENCE_KEY
}

function isActiveWorkflowSession(session: ActiveSessionInfo): boolean {
  const status = (session.status || '').toLowerCase().trim()
  return (
    session.needs_user_input === true ||
    session.has_running_background_agents === true ||
    (session.running_background_agent_count ?? 0) > 0 ||
    status === 'running' ||
    status === 'active' ||
    status === 'in_progress' ||
    status === 'paused' ||
    status === 'idle' ||
    status === 'waiting' ||
    status === 'waiting_feedback'
  )
}

function findTabForSession(tabs: Record<string, ChatTab>, sessionId: string): ChatTab | undefined {
  return Object.values(tabs).find(tab => tab.sessionId === sessionId)
}

function isVisibleActiveSession(session: ActiveSessionInfo): boolean {
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

function isWorkflowSession(session: ActiveSessionInfo): boolean {
  return session.agent_mode === 'workflow' ||
    session.agent_mode === 'workflow_phase' ||
    !!session.workflow_name ||
    !!session.workflow_label ||
    !!session.workspace_path ||
    !!session.preset_query_id
}

function workflowSessionMatchesPreset(
  session: ActiveSessionInfo,
  preset: CustomPreset | PredefinedPreset,
  tabs: Record<string, ChatTab>,
): boolean {
  if (!isWorkflowSession(session)) return false
  if (session.preset_query_id === preset.id) return true
  if (
    normalizeWorkspacePath(session.workspace_path) &&
    normalizeWorkspacePath(session.workspace_path) === normalizeWorkspacePath(preset.selectedFolder?.filepath)
  ) return true
  const tab = findTabForSession(tabs, session.session_id)
  return tab?.metadata?.presetQueryId === preset.id
}

function workflowSessionPriority(session: ActiveSessionInfo): number {
  const status = (session.status || '').toLowerCase()
  let score = 0
  if (isScheduledWorkflowSession(session) || isBotWorkflowSession(session)) score += 100
  // A retained/live tmux session is the only thing that can actually render the
  // terminal pane. Prefer it over a plain running-session registry row so global
  // workflow switches don't land on an empty Schedule tab.
  if (session.has_retained_tmux_session) score -= 25
  if (session.needs_user_input) score -= 30
  if (session.has_running_background_agents || (session.running_background_agent_count ?? 0) > 0) score -= 20
  if (status === 'running' || status === 'active' || status === 'in_progress') score -= 10
  return score
}

export function pickWorkflowActiveSession(
  sessions: ActiveSessionInfo[],
  preset: CustomPreset | PredefinedPreset,
  tabs: Record<string, ChatTab>,
): ActiveSessionInfo | undefined {
  return sessions
    .filter(isVisibleActiveSession)
    .filter(session => workflowSessionMatchesPreset(session, preset, tabs))
    .sort((a, b) => {
      const priorityDelta = workflowSessionPriority(a) - workflowSessionPriority(b)
      if (priorityDelta !== 0) return priorityDelta
      return Date.parse(b.last_activity || b.created_at || '') - Date.parse(a.last_activity || a.created_at || '')
    })[0]
}

function runningWorkflowFallbackName(workflow: RunningWorkflowInfo): string {
  return workflow.preset_name ||
    workflow.title ||
    workflow.workspace_path?.split('/').filter(Boolean).pop() ||
    workflow.query ||
    'Automation'
}

function sessionFromRunningWorkflow(workflow: RunningWorkflowInfo): ActiveSessionInfo {
  const timestamp = workflow.started_at || new Date().toISOString()
  const label = runningWorkflowFallbackName(workflow)
  return {
    session_id: workflow.session_id,
    observer_id: '',
    agent_mode: 'workflow',
    status: workflow.status || 'running',
    last_activity: timestamp,
    created_at: timestamp,
    query: workflow.query || label,
    title: workflow.title || label,
    workflow_name: label,
    workflow_label: label,
    workspace_path: workflow.workspace_path,
    preset_name: workflow.preset_name,
    preset_query_id: workflow.preset_query_id,
    triggered_by: workflow.triggered_by,
    current_execution_name: workflow.current_step_title || workflow.phase_name || workflow.title,
    needs_user_input: workflow.needs_user_input,
    waiting_message: workflow.waiting_message,
    waiting_since: workflow.waiting_since,
  }
}

function isRunningWorkflowEntry(entry: RunningWorkflowInfo): boolean {
  const status = (entry.status || '').toLowerCase()
  return status === 'running' ||
    status === 'active' ||
    status === 'in_progress' ||
    status === 'paused' ||
    status === 'waiting' ||
    status === 'waiting_for_input' ||
    status === 'waiting_feedback' ||
    !!entry.needs_user_input
}

function runningWorkflowMatchesPreset(
  workflow: RunningWorkflowInfo,
  preset: CustomPreset | PredefinedPreset,
): boolean {
  if (workflow.preset_query_id && workflow.preset_query_id === preset.id) return true
  const workflowPath = normalizeWorkspacePath(workflow.workspace_path)
  const presetPath = normalizeWorkspacePath(preset.selectedFolder?.filepath)
  return !!workflowPath && !!presetPath && workflowPath === presetPath
}

async function findRunningWorkflowForPreset(
  preset: CustomPreset | PredefinedPreset,
): Promise<RunningWorkflowInfo | undefined> {
  try {
    const response = await agentApi.listRunningWorkflows()
    return (response.running || [])
      .filter(workflow => workflow.session_id && isRunningWorkflowEntry(workflow))
      .filter(workflow => runningWorkflowMatchesPreset(workflow, preset))
      .sort((a, b) => new Date(b.started_at || 0).getTime() - new Date(a.started_at || 0).getTime())[0]
  } catch {
    return undefined
  }
}

async function findLiveWorkflowTerminalSession(
  preset: CustomPreset | PredefinedPreset,
  title: string,
): Promise<ActiveSessionInfo | undefined> {
  try {
    const response = await Promise.race([
      agentApi.listTerminals(undefined, 'none', { activeOnly: true }),
      new Promise<undefined>(resolve => setTimeout(() => resolve(undefined), WORKFLOW_TERMINAL_LOOKUP_TIMEOUT_MS)),
    ])
    if (!response) return undefined
    return liveWorkflowTerminalSessionForPreset(response.terminals || [], preset, title)
  } catch {
    return undefined
  }
}

function tabSortTimestamp(tab: ChatTab): number {
  return tab.lastAccessedAt ?? tab.createdAt ?? 0
}

function isEmptyWorkflowBuilderTab(tab: ChatTab, presetId: string): boolean {
  const chatStore = useChatStore.getState()
  return tab.metadata?.mode === 'workflow' &&
    tab.metadata?.phaseId === 'workflow-builder' &&
    tab.metadata?.isViewOnly !== true &&
    tab.metadata?.presetQueryId === presetId &&
    !tab.isStreaming &&
    !chatStore.getTabStreamingStatus(tab.tabId) &&
    !tab.config?.restoredConversationPath &&
    (!tab.sessionId || chatStore.getTabEvents(tab.sessionId).length === 0)
}

function findReadOnlyRunTabForSession(
  tabs: Record<string, ChatTab>,
  sessionId: string,
  metadata: NonNullable<ChatTab['metadata']>,
): ChatTab | undefined {
  return Object.values(tabs).find(tab => {
    if (tab.sessionId !== sessionId || tab.metadata?.mode !== 'workflow' || !tab.metadata?.isViewOnly) return false
    if (metadata.isScheduledRun) return tab.metadata.isScheduledRun === true
    if (metadata.isBotRun) return tab.metadata.isBotRun === true
    return false
  })
}

export function isScheduledWorkflowSession(session: ActiveSessionInfo, runningWorkflow?: RunningWorkflowInfo): boolean {
  const triggeredBy = (session.triggered_by || runningWorkflow?.triggered_by || '').toLowerCase()
  const sessionId = (session.session_id || '').toLowerCase()
  return triggeredBy.includes('schedule') ||
    triggeredBy === 'cron' ||
    sessionId.startsWith('schedule-') ||
    sessionId.includes('-schedule-')
}

export function workflowSessionBotPlatform(
  session: ActiveSessionInfo,
  runningWorkflow?: RunningWorkflowInfo,
): string | undefined {
  const rawPlatform = (session.bot_platform || '').trim()
  const candidates = [
    rawPlatform,
    session.triggered_by,
    runningWorkflow?.triggered_by,
    session.session_id,
  ].map(value => (value || '').toLowerCase())

  if (rawPlatform) {
    if (rawPlatform.toLowerCase() === 'whatsapp') return 'WhatsApp'
    if (rawPlatform.toLowerCase() === 'slack') return 'Slack'
    return rawPlatform
  }
  if (candidates.some(value => value.includes('whatsapp'))) return 'WhatsApp'
  if (candidates.some(value => value.includes('slack'))) return 'Slack'
  if (candidates.some(value => value.includes('bot:') || value === 'bot' || value.includes('_bot'))) return 'Bot'
  return undefined
}

export function isBotWorkflowSession(session: ActiveSessionInfo, runningWorkflow?: RunningWorkflowInfo): boolean {
  return !!workflowSessionBotPlatform(session, runningWorkflow)
}

export function findWorkflowPresetForSession(
  session: ActiveSessionInfo,
  runningWorkflow?: RunningWorkflowInfo,
): CustomPreset | PredefinedPreset | undefined {
  const presetStore = useGlobalPresetStore.getState()
  const presetId = session.preset_query_id || runningWorkflow?.preset_query_id
  if (presetId) {
    const byId = presetStore.workflowPresets.find(preset => preset.id === presetId)
    if (byId) return byId
  }

  const workspacePath = normalizeWorkspacePath(runningWorkflow?.workspace_path || session.workspace_path)
  if (!workspacePath) return undefined
  return presetStore.workflowPresets.find(preset =>
    normalizeWorkspacePath(preset.selectedFolder?.filepath) === workspacePath
  )
}

function requestChatScrollToBottom(): void {
  useChatStore.getState().setAutoScroll(true)
  window.dispatchEvent(new CustomEvent('chat-scroll-to-bottom'))
  setTimeout(() => window.dispatchEvent(new CustomEvent('chat-scroll-to-bottom')), 120)
  setTimeout(() => window.dispatchEvent(new CustomEvent('chat-scroll-to-bottom')), 400)
}

function forceMobilePreview(scopeId?: string | null): void {
  if (typeof window === 'undefined') return
  try {
    window.localStorage.setItem(reportPreviewPreferenceKey(scopeId), 'mobile')
  } catch {
    // UI preference only.
  }
  window.dispatchEvent(new CustomEvent(REPORT_PREVIEW_PREFERENCE_CHANGED_EVENT, {
    detail: { preference: 'mobile', scopeId: scopeId ?? null },
  }))
}

function revealWorkflowTerminal(tabId: string, workspacePath?: string | null): void {
  const chatStore = useChatStore.getState()
  chatStore.setTabViewMode(tabId, 'terminal')

  forceMobilePreview(workspacePath)

  const workflowStore = useWorkflowStore.getState()
  workflowStore.setShowChatArea(true)
  workflowStore.setShowWorkspacePane(true)
  workflowStore.setFocusedPane('chat')
}

export async function restoreWorkflowSessionChat(
  session: ActiveSessionInfo,
  options: RestoreWorkflowSessionOptions = {},
): Promise<string> {
  const resolvedPreset = options.preset || findWorkflowPresetForSession(session, options.runningWorkflow)
  const presetId = resolvedPreset?.id || session.preset_query_id || options.runningWorkflow?.preset_query_id
  const workspacePath = resolvedPreset?.selectedFolder?.filepath || options.runningWorkflow?.workspace_path || session.workspace_path || null
  const isActive = isActiveWorkflowSession(session)

  useRunningWorkflowsStore.getState().setIsRestoringWorkflow(true)
  try {
    useAppStore.getState().setShowWorkflowsOverview(false)
    useModeStore.getState().setModeCategory('workflow')
    if (resolvedPreset) {
      useGlobalPresetStore.getState().applyPreset(resolvedPreset, 'workflow')
    } else if (presetId) {
      useGlobalPresetStore.getState().setActivePreset('workflow', presetId)
    }

    const chatStore = useChatStore.getState()
    const existingTab = findTabForSession(chatStore.chatTabs, session.session_id)
    if (
      existingTab?.metadata?.mode === 'workflow' &&
      existingTab.metadata?.phaseId === 'workflow-builder' &&
      existingTab.name !== 'Automation Builder'
    ) {
      await chatStore.closeTab(existingTab.tabId, false)
    }

    const latestChatStore = useChatStore.getState()
    const exactSessionTab = findTabForSession(latestChatStore.chatTabs, session.session_id)
    const exactBuilderTab = exactSessionTab?.metadata?.mode === 'workflow' &&
      exactSessionTab.metadata?.phaseId === 'workflow-builder'
      ? exactSessionTab
      : undefined
    const presetBuilderTab = Object.values(latestChatStore.chatTabs).find(tab =>
      tab.metadata?.mode === 'workflow' &&
      tab.metadata?.phaseId === 'workflow-builder' &&
      presetId &&
      tab.metadata?.presetQueryId === presetId
    )
    const builderTab = exactBuilderTab || (
      presetBuilderTab &&
      (presetBuilderTab.sessionId === session.session_id || !latestChatStore.getTabStreamingStatus(presetBuilderTab.tabId))
        ? presetBuilderTab
        : undefined
    )

    const tabId = builderTab?.tabId ?? await latestChatStore.createChatTab('Automation Builder', {
      mode: 'workflow',
      phaseId: 'workflow-builder',
      phaseName: 'Automation Builder',
      presetQueryId: presetId,
    }, session.session_id)

    if (builderTab?.sessionId !== session.session_id) {
      latestChatStore.updateTabSessionId(tabId, session.session_id)
    }
    latestChatStore.setTabViewMode(tabId, 'terminal')

    const hasExistingEvents = latestChatStore.getTabEvents(session.session_id).length > 0
    // Fast path for switching back to an already-open running workflow:
    // keep the in-memory event buffer and SSE connection intact. Re-fetching
    // recent events here replaces the tab event array and makes Ctrl+K feel
    // like a reload even though the workflow chat is already live.
    if (builderTab?.sessionId === session.session_id && hasExistingEvents) {
      latestChatStore.setTabStreaming(tabId, isActive)
      latestChatStore.setTabCompleted(tabId, !isActive)
      activateTab(tabId)
      revealWorkflowTerminal(tabId, workspacePath)
      if (options.scrollToBottom !== false) requestChatScrollToBottom()
      return tabId
    }

    // Reveal the terminal/chat IMMEDIATELY. Event history is intentionally not
    // hydrated here; workflow switches default to terminal/report surfaces, and
    // the tree/debug view lazy-loads events only when the user opens it.
    latestChatStore.setTabStreaming(tabId, isActive)
    latestChatStore.setTabCompleted(tabId, !isActive)
    activateTab(tabId)
    revealWorkflowTerminal(tabId, workspacePath)
    if (options.scrollToBottom !== false) requestChatScrollToBottom()

    return tabId
  } finally {
    useRunningWorkflowsStore.getState().setIsRestoringWorkflow(false)
  }
}

export async function restoreScheduledWorkflowRunChat(
  session: ActiveSessionInfo,
  options: RestoreWorkflowSessionOptions = {},
): Promise<string> {
  const jobName = options.runningWorkflow?.preset_name ||
    options.runningWorkflow?.title ||
    session.preset_name ||
    session.title ||
    'Scheduled run'

  return restoreReadOnlyWorkflowRunChat(session, {
    ...options,
    tabName: 'Schedule',
    metadata: {
      isScheduledRun: true,
      scheduledJobName: jobName,
      isBotRun: false,
      botPlatform: undefined,
    },
  })
}

export async function restoreBotWorkflowRunChat(
  session: ActiveSessionInfo,
  options: RestoreWorkflowSessionOptions = {},
): Promise<string> {
  const platform = workflowSessionBotPlatform(session, options.runningWorkflow) || 'Bot'
  return restoreReadOnlyWorkflowRunChat(session, {
    ...options,
    tabName: platform,
    metadata: {
      isScheduledRun: false,
      scheduledJobName: undefined,
      isBotRun: true,
      botPlatform: platform,
    },
  })
}

export async function openWorkflowPresetPage(
  preset: CustomPreset | PredefinedPreset,
  options: OpenWorkflowPresetPageOptions = {},
): Promise<void> {
  useAppStore.getState().setShowWorkflowsOverview(false)
  useModeStore.getState().setModeCategory('workflow')
  useGlobalPresetStore.getState().applyPreset(preset, 'workflow')
  useWorkflowStore.getState().setShowChatArea(true)

  const title = options.title || preset.label || 'Automation'
  const chatStore = useChatStore.getState()
  if (options.activeSession) {
    await openActiveSession(options.activeSession, {
      preset,
      runningWorkflow: options.runningWorkflow,
      title,
      source: options.source,
    })
    return
  }

  const terminalBackedSession = await findLiveWorkflowTerminalSession(preset, title)
  if (terminalBackedSession) {
    await openActiveSession(terminalBackedSession, {
      preset,
      title,
      source: options.source || 'workflow-terminal',
    })
    return
  }

  const activeSession = pickWorkflowActiveSession(await chatStore.getActiveSessions(), preset, useChatStore.getState().chatTabs)

  if (activeSession) {
    await openActiveSession(activeSession, {
      preset,
      runningWorkflow: options.runningWorkflow,
      title,
      source: options.source,
    })
    return
  }

  const runningWorkflow = options.runningWorkflow || await findRunningWorkflowForPreset(preset)
  if (runningWorkflow?.session_id) {
    await openActiveSession(sessionFromRunningWorkflow(runningWorkflow), {
      preset,
      runningWorkflow,
      title,
      source: options.source,
    })
    return
  }

  const latestStore = useChatStore.getState()
  const builderTab = Object.values(latestStore.chatTabs)
    .filter(tab => isEmptyWorkflowBuilderTab(tab, preset.id))
    .sort((a, b) => tabSortTimestamp(b) - tabSortTimestamp(a))[0]
  const tabId = builderTab?.tabId ?? await latestStore.createChatTab('Automation Builder', {
    mode: 'workflow',
    phaseId: 'workflow-builder',
    phaseName: 'Automation Builder',
    presetQueryId: preset.id,
  })

  activateTab(tabId)
  useWorkflowStore.getState().setShowChatArea(true)
  if (options.scrollToBottom !== false) requestChatScrollToBottom()
}

type ReadOnlyWorkflowRunOptions = RestoreWorkflowSessionOptions & {
  tabName: string
  metadata: NonNullable<ChatTab['metadata']>
}

async function restoreReadOnlyWorkflowRunChat(
  session: ActiveSessionInfo,
  options: ReadOnlyWorkflowRunOptions,
): Promise<string> {
  const resolvedPreset = options.preset || findWorkflowPresetForSession(session, options.runningWorkflow)
  const presetId = resolvedPreset?.id || session.preset_query_id || options.runningWorkflow?.preset_query_id
  const workspacePath = resolvedPreset?.selectedFolder?.filepath || options.runningWorkflow?.workspace_path || session.workspace_path || null

  useRunningWorkflowsStore.getState().setIsRestoringWorkflow(true)
  try {
  useAppStore.getState().setShowWorkflowsOverview(false)
  useModeStore.getState().setModeCategory('workflow')
  if (resolvedPreset) {
    useGlobalPresetStore.getState().applyPreset(resolvedPreset, 'workflow')
  } else if (presetId) {
    useGlobalPresetStore.getState().setActivePreset('workflow', presetId)
  }
  useWorkflowStore.getState().setShowChatArea(true)

  const chatStore = useChatStore.getState()
  const metadata = {
    mode: 'workflow' as const,
    phaseId: undefined,
    phaseName: undefined,
    ...(presetId ? { presetQueryId: presetId } : {}),
    isViewOnly: true,
    ...options.metadata,
    readOnlyRestoredAt: Date.now(),
  }
  const desiredName = options.tabName

  // If the user already converted this run into an interactive chat (same
  // sessionId, view-only cleared via WorkflowChatTabs.handleMakeInteractive),
  // don't recreate a read-only run tab or revert it back to view-only — just
  // focus the existing interactive tab. Otherwise the header pill / activity
  // monitor would spawn a duplicate 'Schedule' tab for the same session.
  const interactiveTab = findTabForSession(chatStore.chatTabs, session.session_id)
  if (interactiveTab && !interactiveTab.metadata?.isViewOnly) {
    activateTab(interactiveTab.tabId)
    revealWorkflowTerminal(interactiveTab.tabId, workspacePath)
    if (options.scrollToBottom !== false) requestChatScrollToBottom()
    return interactiveTab.tabId
  }

  if (presetId) {
    const emptyBuilderTabs = Object.values(chatStore.chatTabs).filter(tab =>
      tab.metadata?.mode === 'workflow' &&
      tab.metadata?.phaseId === 'workflow-builder' &&
      tab.metadata?.presetQueryId === presetId &&
      !chatStore.getTabStreamingStatus(tab.tabId) &&
      (!tab.sessionId || chatStore.getTabEvents(tab.sessionId).length === 0)
    )

    for (const tab of emptyBuilderTabs) {
      await chatStore.closeTab(tab.tabId, false)
    }
  }

  const existingTab = findReadOnlyRunTabForSession(chatStore.chatTabs, session.session_id, metadata)

  const tabId = existingTab?.tabId ?? await chatStore.createChatTab(desiredName, metadata, session.session_id)
  chatStore.setTabMetadata(tabId, metadata)
  chatStore.setTabViewMode(tabId, 'terminal')
  if (existingTab && existingTab.name !== desiredName) {
    useChatStore.setState((state) => {
      const tab = state.chatTabs[tabId]
      if (!tab) return state
      return { chatTabs: { ...state.chatTabs, [tabId]: { ...tab, name: desiredName } } }
    })
  }

  // Do not hydrate event history on workflow switch. Scheduled/bot workflow runs
  // can have large histories, and opening them from the header activity monitor
  // should focus terminal/report/previous-chats immediately. Tree/debug view
  // lazy-loads events when explicitly opened.
  const isActive = isActiveWorkflowSession(session)
  chatStore.setTabStreaming(tabId, isActive)
  chatStore.setTabCompleted(tabId, !isActive)
  activateTab(tabId)
  revealWorkflowTerminal(tabId, workspacePath)
  window.dispatchEvent(new CustomEvent('workflow-readonly-run-restored', {
    detail: { presetId, tabId, workspacePath }
  }))
  if (options.scrollToBottom !== false) requestChatScrollToBottom()

  return tabId
  } finally {
    useRunningWorkflowsStore.getState().setIsRestoringWorkflow(false)
  }
}

// openActiveSession is the SINGLE shared path for opening an active session row
// from a global surface (the Ctrl+K quick-switcher and the header activity
// monitor). Both call this so clicking the same session behaves identically.
//
// Workflow sessions go through the thorough restore family, which already: jumps
// to an existing tab, closes a stale builder tab, applies the preset, switches to
// workflow mode, clears the Workflows Overview, and scrolls to bottom. Plain chat
// sessions activate their existing tab or restore a fresh one.
export async function openActiveSession(
  session: ActiveSessionInfo,
  options: { preset?: CustomPreset | PredefinedPreset; runningWorkflow?: RunningWorkflowInfo; title?: string; source?: string } = {},
): Promise<void> {
  const isWorkflow = (session.agent_mode || '').toLowerCase().includes('workflow')
  if (isWorkflow) {
    if (isScheduledWorkflowSession(session, options.runningWorkflow)) {
      await restoreScheduledWorkflowRunChat(session, { preset: options.preset, runningWorkflow: options.runningWorkflow })
    } else if (isBotWorkflowSession(session, options.runningWorkflow)) {
      await restoreBotWorkflowRunChat(session, { preset: options.preset, runningWorkflow: options.runningWorkflow })
    } else {
      await restoreWorkflowSessionChat(session, { preset: options.preset, runningWorkflow: options.runningWorkflow })
    }
    return
  }

  const chatStore = useChatStore.getState()
  const existingTab = findTabForSession(chatStore.chatTabs, session.session_id)
  if (existingTab) {
    activateTab(existingTab.tabId)
    requestChatScrollToBottom()
    return
  }
  const tabId = await restoreSession(session.session_id, { title: options.title, source: options.source })
  activateTab(tabId)
  requestChatScrollToBottom()
}
