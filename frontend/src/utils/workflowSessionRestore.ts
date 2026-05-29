import { activateTab } from './activateTab'
import { agentApi } from '../services/api'
import type { ActiveSessionInfo, RunningWorkflowInfo } from '../services/api-types'
import { useAppStore } from '../stores/useAppStore'
import { normalizeEventViewMode, useChatStore, type ChatTab, type EventViewMode } from '../stores/useChatStore'
import { useGlobalPresetStore } from '../stores/useGlobalPresetStore'
import { useModeStore } from '../stores/useModeStore'
import { useRunningWorkflowsStore } from '../stores/useRunningWorkflowsStore'
import { useWorkflowStore } from '../stores/useWorkflowStore'
import type { CustomPreset, PredefinedPreset } from '../types/preset'

type RestoreWorkflowSessionOptions = {
  preset?: CustomPreset | PredefinedPreset
  runningWorkflow?: RunningWorkflowInfo
  scrollToBottom?: boolean
}

function normalizeWorkspacePath(path?: string): string {
  return (path || '').replace(/\/+$/, '')
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
  return triggeredBy.includes('schedule') || sessionId.startsWith('schedule-') || sessionId.includes('-schedule-')
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

function currentWorkflowViewMode(): EventViewMode {
  const chatStore = useChatStore.getState()
  const activeTab = chatStore.activeTabId ? chatStore.chatTabs[chatStore.activeTabId] : undefined
  if (activeTab?.metadata?.mode === 'workflow') {
    return normalizeEventViewMode(activeTab.viewMode)
  }
  return normalizeEventViewMode(chatStore.eventViewModePreference)
}

export async function restoreWorkflowSessionChat(
  session: ActiveSessionInfo,
  options: RestoreWorkflowSessionOptions = {},
): Promise<string> {
  const resolvedPreset = options.preset || findWorkflowPresetForSession(session, options.runningWorkflow)
  const presetId = resolvedPreset?.id || session.preset_query_id || options.runningWorkflow?.preset_query_id
  const isActive = isActiveWorkflowSession(session)
  const restoreViewMode = currentWorkflowViewMode()

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
      existingTab.name !== 'Workflow Builder'
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

    const tabId = builderTab?.tabId ?? await latestChatStore.createChatTab('Workflow Builder', {
      mode: 'workflow',
      phaseId: 'workflow-builder',
      phaseName: 'Workflow Builder',
      presetQueryId: presetId,
    }, session.session_id)

    if (builderTab?.sessionId !== session.session_id) {
      latestChatStore.updateTabSessionId(tabId, session.session_id)
    }
    latestChatStore.setTabViewMode(tabId, restoreViewMode)

    const hasExistingEvents = latestChatStore.getTabEvents(session.session_id).length > 0
    // Fast path for switching back to an already-open running workflow:
    // keep the in-memory event buffer and SSE connection intact. Re-fetching
    // recent events here replaces the tab event array and makes Ctrl+K feel
    // like a reload even though the workflow chat is already live.
    if (builderTab?.sessionId === session.session_id && hasExistingEvents) {
      latestChatStore.setTabStreaming(tabId, isActive)
      latestChatStore.setTabCompleted(tabId, !isActive)
      useWorkflowStore.getState().setShowChatArea(true)
      activateTab(tabId)
      if (options.scrollToBottom !== false) requestChatScrollToBottom()
      return tabId
    }

    try {
      const response = await agentApi.getRecentSessionEvents(session.session_id)
      const restoredEvents = response.events || []
      latestChatStore.setTabEvents(session.session_id, restoredEvents)
      latestChatStore.setTabLastEventIndex(
        session.session_id,
        response.last_processed_index ?? (restoredEvents.length ? restoredEvents.length - 1 : -1),
      )
      latestChatStore.setTabStreaming(tabId, isActive || response.session_status === 'running')
      latestChatStore.setTabCompleted(tabId, !isActive && response.session_status !== 'running')
    } catch {
      latestChatStore.setTabStreaming(tabId, isActive)
      latestChatStore.setTabCompleted(tabId, !isActive)
    }

    useWorkflowStore.getState().setShowChatArea(true)
    activateTab(tabId)
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
  const restoreViewMode = currentWorkflowViewMode()

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
  }
  const desiredName = options.tabName

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
  chatStore.setTabViewMode(tabId, restoreViewMode)
  if (existingTab && existingTab.name !== desiredName) {
    useChatStore.setState((state) => {
      const tab = state.chatTabs[tabId]
      if (!tab) return state
      return { chatTabs: { ...state.chatTabs, [tabId]: { ...tab, name: desiredName } } }
    })
  }

  try {
    const existingEvents = chatStore.getTabEvents(session.session_id)
    const response = existingEvents.length === 0
      ? await agentApi.getRecentSessionEvents(session.session_id)
      : await agentApi.getSessionEvents(session.session_id, chatStore.getTabLastEventIndex(session.session_id))

    if (response.events.length > 0) {
      if (existingEvents.length === 0) {
        chatStore.setTabEvents(session.session_id, response.events)
      } else {
        chatStore.addTabEvents(session.session_id, response.events)
      }
    }
    if (response.last_processed_index !== undefined) {
      chatStore.setTabLastEventIndex(session.session_id, response.last_processed_index)
    }
    if (response.has_more !== undefined) {
      chatStore.setTabHasMoreOlderEvents(session.session_id, response.has_more)
    }
    const isDone = response.session_status === 'completed' || response.session_status === 'stopped'
    const isError = response.session_status === 'error'
    chatStore.setTabCompleted(tabId, isDone)
    chatStore.setTabStreaming(tabId, !isDone && !isError && response.session_status === 'running')
    chatStore.setTabHasRunningBgAgents(tabId, !!response.has_running_background_agents)
    chatStore.setTabSyntheticTurn(tabId, !!response.is_synthetic_turn)
    chatStore.setTabCanSteer(tabId, !!response.can_steer)
  } catch {
    chatStore.setTabStreaming(tabId, isActiveWorkflowSession(session))
    chatStore.setTabCompleted(tabId, !isActiveWorkflowSession(session))
  }

  activateTab(tabId)
  window.dispatchEvent(new CustomEvent('workflow-readonly-run-restored', {
    detail: { presetId, tabId }
  }))
  if (options.scrollToBottom !== false) requestChatScrollToBottom()
  return tabId
  } finally {
    useRunningWorkflowsStore.getState().setIsRestoringWorkflow(false)
  }
}
