import { agentApi } from '../services/api'
import type { ActiveSessionInfo, RunningWorkflowInfo } from '../services/api-types'
import { useAppStore } from '../stores/useAppStore'
import { useChatStore, type ChatTab } from '../stores/useChatStore'
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
    status === 'paused' ||
    status === 'idle' ||
    status === 'waiting' ||
    status === 'waiting_feedback'
  )
}

function findTabForSession(tabs: Record<string, ChatTab>, sessionId: string): ChatTab | undefined {
  return Object.values(tabs).find(tab => tab.sessionId === sessionId)
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

export async function restoreWorkflowSessionChat(
  session: ActiveSessionInfo,
  options: RestoreWorkflowSessionOptions = {},
): Promise<string> {
  const resolvedPreset = options.preset || findWorkflowPresetForSession(session, options.runningWorkflow)
  const presetId = resolvedPreset?.id || session.preset_query_id || options.runningWorkflow?.preset_query_id
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

    try {
      const response = await agentApi.getRecentSessionEvents(session.session_id)
      const restoredEvents = response.events || []
      if (latestChatStore.getTabEvents(session.session_id).length > 0) {
        latestChatStore.addTabEvents(session.session_id, restoredEvents)
      } else {
        latestChatStore.setTabEvents(session.session_id, restoredEvents)
      }
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
    latestChatStore.switchTab(tabId)
    if (options.scrollToBottom !== false) requestChatScrollToBottom()
    return tabId
  } finally {
    useRunningWorkflowsStore.getState().setIsRestoringWorkflow(false)
  }
}
