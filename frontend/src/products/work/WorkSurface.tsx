import { useCallback, useEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import { Loader2, PanelLeftOpen, PanelRightOpen, Plus } from 'lucide-react'
import { useShallow } from 'zustand/react/shallow'
import ChatArea from '../../components/ChatArea'
import { GlobalHumanFeedbackPrompt } from '../../components/GlobalHumanFeedbackPrompt'
import { ModePresetBar } from '../../components/ModePresetBar'
import LlmModalHost from '../../components/topbar/LlmModalHost'
import { TopBarEntitySelector } from '../../components/topbar/TopBarEntitySelector'
import { UpdateProgressToast } from '../../components/UpdateProgressToast'
import { agentApi } from '../../services/api'
import { useAppStore } from '../../stores/useAppStore'
import { useChatStore, waitForChatStoreHydration } from '../../stores/useChatStore'
import { useModeStore } from '../../stores/useModeStore'
import { useLLMStore } from '../../stores/useLLMStore'
import { hydrateTabEvents } from '../../utils/sessionRestore'
import { activateTab } from '../../utils/activateTab'
import { AgentWorksChatTabItem } from '../../components/chat/AgentWorksChatTabItem'
import { WORK_PROFILE_ID, WORK_PROFILE_VERSION } from './workData'
import { createWorkSession, loadWorkSessions, workLLMConfigFromSelection, workLLMSelectionFromConfig, type WorkSession } from './workSessions'
import { WorkWorkspacePane, WorkWorkspaceToolbar, type WorkWorkspaceView } from './WorkWorkspacePane'
import { usePointerDrag } from '../../hooks/usePointerDrag'
import { WorkspaceSplitCollapseControls, WorkspaceSplitDivider } from '../../components/workspace/WorkspaceSplitDivider'
import { WorkspaceTopToolbar } from '../../components/workspace/WorkspaceTopToolbar'
import { loadAgentProfileInteractionKinds, loadAgentProfileUIPanels } from '../../utils/agentProfileCapabilities'
import { parseProductInteraction } from '../../../shared/session/interactions'
import { belongsToWorkProject, markWorkProjectRuntimeDirty, preferredWorkProjectTabId, setWorkProjectRuntimeSelection, visibleWorkProjectTabs, workConversationResumeKey, workTabDisplayName, type ProductEngineSelectionDetail, type WorkRuntimeSelection } from './workTabs'
import { updateProductProjectLLMConfig, updateProductProjectSelections } from '../../platform/chat/productProjects'
import { CreateWorkProjectDialog } from './CreateWorkProjectDialog'
import { useWorkspaceUIControl, type WorkspaceUIControlAdapter } from '../../platform/ui-control/useWorkspaceUIControl'
import { usePresentationEvents } from '../../platform/presentations/usePresentationEvents'

const WORK_SPLIT_PREFERENCE_KEY = 'work_workspace_split_ratio'
const WORK_UI_PRESENTATION_VIEWS = {
  report: 'dashboard', database: 'database', browser: 'browser', costs: 'costs', schedules: 'schedules', files: 'files',
  skills: 'skills', secrets: 'secrets', mcp: 'mcp', llm: 'models', bots: 'bots', folders: 'folders',
} as const satisfies Record<string, WorkWorkspaceView>
type WorkUIPresentationView = keyof typeof WORK_UI_PRESENTATION_VIEWS
const WORK_UI_LABELS: Record<WorkUIPresentationView, string> = {
  report: 'Dashboard', database: 'Database', browser: 'Browser', costs: 'Costs and usage', schedules: 'Schedules', files: 'Files',
  skills: 'Skills', secrets: 'Secrets', mcp: 'MCP servers', llm: 'Agent configuration', bots: 'Bots', folders: 'Attached folders',
}

function workPresentationView(view: WorkWorkspaceView): WorkUIPresentationView {
  return (Object.entries(WORK_UI_PRESENTATION_VIEWS).find(([, panel]) => panel === view)?.[0] ?? 'files') as WorkUIPresentationView
}

function clampWorkSplitRatio(ratio: number, width: number): number {
  const minPaneWidth = 240
  const minRatio = Math.max(0.15, Math.min(0.5, minPaneWidth / Math.max(width, minPaneWidth * 2)))
  return Math.max(minRatio, Math.min(Math.min(0.85, 1 - minRatio), ratio))
}

function readWorkSplitRatio(projectId?: string): number {
  if (typeof window === 'undefined' || !projectId) return 0.5
  try {
    const value = Number.parseFloat(window.localStorage.getItem(`${WORK_SPLIT_PREFERENCE_KEY}:${projectId}`) || '')
    return Number.isFinite(value) && value >= 0.15 && value <= 0.85 ? value : 0.5
  } catch {
    return 0.5
  }
}

function writeWorkSplitRatio(projectId: string | undefined, ratio: number) {
  if (typeof window === 'undefined' || !projectId) return
  try { window.localStorage.setItem(`${WORK_SPLIT_PREFERENCE_KEY}:${projectId}`, String(ratio)) } catch { /* UI preference only. */ }
}

async function restoreWorkRuntimeSelection(tabId: string, sessionId: string, workspacePath: string): Promise<WorkRuntimeSelection | null> {
  const cachedRuntime = useChatStore.getState().activeSessionsCache.find(
    session => session.session_id === sessionId,
  )?.runtime
  try {
    const history = await agentApi.getChatHistoryConversation(sessionId, workspacePath, 1)
    const provider = history.runtime?.provider?.trim() || cachedRuntime?.provider?.trim()
    const modelId = history.runtime?.model_id?.trim() || cachedRuntime?.model_id?.trim()
    if (!provider || !modelId) return null
    useChatStore.getState().setTabMetadata(tabId, {
      agentProfileEngine: provider,
      agentProfileModelID: modelId,
      agentProfileReasoningEffort: undefined,
    })
    return { engine: provider, provider, modelId }
  } catch {
    // A new project has no history yet. A restored session can still carry the
    // authoritative native runtime in the activity cache.
    const provider = cachedRuntime?.provider?.trim()
    const modelId = cachedRuntime?.model_id?.trim()
    if (!provider || !modelId) return null
    useChatStore.getState().setTabMetadata(tabId, {
      agentProfileEngine: provider,
      agentProfileModelID: modelId,
      agentProfileReasoningEffort: undefined,
    })
    return { engine: provider, provider, modelId }
  }
}

function useWorkSessions() {
  const [sessions, setSessions] = useState<WorkSession[]>([])
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    const listed = await loadWorkSessions()
    setSessions(listed)
    setSelectedId((current) => current && listed.some(item => item.id === current) ? current : listed[0]?.id ?? null)
    return listed
  }, [])

  useEffect(() => {
    let cancelled = false
    void loadWorkSessions()
      .then((listed) => {
        if (cancelled) return
        setSessions(listed)
        setSelectedId((current) => current ?? listed[0]?.id ?? null)
      })
      .catch((cause) => {
        if (!cancelled) setError(cause instanceof Error ? cause.message : 'Could not load projects.')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => { cancelled = true }
  }, [])

  const create = useCallback(async (title: string, description: string) => {
    const session = await createWorkSession(title, description)
    setSessions((current) => [session, ...current])
    setSelectedId(session.id)
    return session
  }, [])

  const updateLLMConfig = useCallback(async (projectId: string, selection: WorkRuntimeSelection) => {
    const project = sessions.find(item => item.id === projectId)
    if (!project) throw new Error('This Crew project is no longer available.')
    const llmConfig = workLLMConfigFromSelection({
      provider: selection.provider || selection.engine,
      modelId: selection.modelId,
      reasoningEffort: selection.reasoningEffort,
    })
    const updated = await updateProductProjectLLMConfig(project, llmConfig, `Update Crew project model ${project.title}`, 'workflow.json')
    setSessions(current => current.map(item => item.id === projectId ? updated : item))
    return updated
  }, [sessions])

  const updateSelections = useCallback(async (projectId: string, patch: { selectedServers?: string[]; selectedSkills?: string[]; selectedSecrets?: string[]; workflowContextPaths?: string[] }) => {
    const project = sessions.find(item => item.id === projectId)
    if (!project) throw new Error('This Crew project is no longer available.')
    const updated = await updateProductProjectSelections(project, patch, `Update Crew project integrations ${project.title}`, 'workflow.json')
    setSessions(current => current.map(item => item.id === projectId ? updated : item))
    return updated
  }, [sessions])

  return {
    sessions,
    selected: sessions.find((session) => session.id === selectedId) ?? null,
    select: setSelectedId,
    create,
    updateLLMConfig,
    updateSelections,
    refresh,
    loading,
    error,
  }
}

function useWorkChatTabs(
  session: WorkSession | null,
  onLegacyRuntimeDiscovered: (selection: WorkRuntimeSelection) => void | Promise<void>,
) {
  const [error, setError] = useState<string | null>(null)
  const [ready, setReady] = useState(false)
  const { chatTabs, activeTabId } = useChatStore(useShallow(state => ({
    chatTabs: state.chatTabs,
    activeTabId: state.activeTabId,
  })))

  useEffect(() => {
    setReady(false)
    setError(null)
    if (!session) return
    let cancelled = false
    const prepare = async () => {
      try {
        useModeStore.getState().setModeCategory('multi-agent')
        useAppStore.getState().setAgentMode('multi-agent')
        await waitForChatStoreHydration()
        if (cancelled) return
        const chatStore = useChatStore.getState()
        const savedRuntime = workLLMSelectionFromConfig(session.llmConfig)
        const savedServers = session.selectedServers.length > 0 ? session.selectedServers : ['NO_SERVERS']
        const savedSkills = session.selectedSkills
        const projectMetadata = {
          mode: 'multi-agent',
          agentProfileId: WORK_PROFILE_ID,
          agentProfileVersion: WORK_PROFILE_VERSION,
          agentProfileWorkspace: session.workspacePath,
          agentProfileProjectId: session.id,
          agentProfileProjectTitle: session.title,
          agentProfileChatContract: 'profile-v1',
        } as const
        const selectedRuntimeMetadata = savedRuntime ? {
            agentProfileEngine: savedRuntime.provider,
            agentProfileModelID: savedRuntime.modelId,
            agentProfileReasoningEffort: savedRuntime.reasoningEffort,
          } : {}
        let builder = Object.values(chatStore.chatTabs).find(
          tab => belongsToWorkProject(tab, session.id) && tab.metadata?.agentProfileBuilder,
        )
        const chats = Object.values(chatStore.chatTabs).filter(
          tab => belongsToWorkProject(tab, session.id) && !tab.metadata?.agentProfileBuilder,
        )

        // Migrate the original one-chat Crew tabs into the project-scoped tab
        // family without discarding their native CLI conversation.
        let legacyRuntime: WorkRuntimeSelection | null = null
        const orderedChats = [...chats].sort((left, right) => {
          if (left.tabId === chatStore.activeTabId) return -1
          if (right.tabId === chatStore.activeTabId) return 1
          return (right.lastAccessedAt ?? right.createdAt) - (left.lastAccessedAt ?? left.createdAt)
        })
        for (const chat of orderedChats) {
          chatStore.setTabMetadata(chat.tabId, { ...projectMetadata, agentProfileBuilder: false })
          chatStore.setTabConfig(chat.tabId, { selectedServers: savedServers, selectedSkills: savedSkills })
          chatStore.setTabMetadata(chat.tabId, { agentProfileMCPSelectionInitialized: true })
          const hasRuntimeBinding = Boolean(chat.metadata?.agentProfileEngine && chat.metadata.agentProfileModelID)
          if (!chat.sessionId) continue

          // A browser tab can outlive many frontend and backend deployments.
          // Reassert its durable session before the composer becomes ready;
          // otherwise the server registry may resolve the key to a newer or
          // empty conversation and the next message silently loses context.
          // This also upgrades legacy tabs that shared the project's Builder
          // key into one key per retained conversation.
          const conversationKey = workConversationResumeKey(chat, session.id)
          if (!conversationKey) continue
          const resumed = await agentApi.switchAgentProfileConversation(WORK_PROFILE_ID, {
            conversation_key: conversationKey,
            session_id: chat.sessionId,
          })
          chatStore.setTabMetadata(chat.tabId, {
            agentProfileConversationKey: resumed.conversation_key,
            agentProfileConversationId: resumed.conversation_id,
          })
          chatStore.updateTabSessionId(chat.tabId, resumed.session_id)

          if ((chatStore.tabEvents[chat.sessionId]?.length ?? 0) === 0) {
            await hydrateTabEvents(chat.sessionId, {
              workspacePath: session.workspacePath,
              fallbackToChatHistory: true,
              preferChatHistory: true,
            })
          }
          if (!hasRuntimeBinding) {
            const restored = await restoreWorkRuntimeSelection(chat.tabId, chat.sessionId, session.workspacePath)
            if (restored) legacyRuntime ??= restored
            else if (savedRuntime) chatStore.setTabMetadata(chat.tabId, selectedRuntimeMetadata)
          }
        }

        if (!builder) {
          const builderId = await chatStore.createChatTab('Builder', {
            ...projectMetadata,
            ...selectedRuntimeMetadata,
            agentProfileBuilder: true,
            agentProfileConversationKey: session.id,
          })
          builder = chatStore.getTab(builderId)
        }
        if (cancelled || !builder) return
        chatStore.setTabMetadata(builder.tabId, {
          ...projectMetadata,
          ...selectedRuntimeMetadata,
          agentProfileConversationKey: session.id,
        })
        chatStore.setTabConfig(builder.tabId, { selectedServers: savedServers, selectedSkills: savedSkills })
        chatStore.setTabMetadata(builder.tabId, { agentProfileMCPSelectionInitialized: true })

        const preferredTabId = preferredWorkProjectTabId(
          chatStore.chatTabs,
          session.id,
          chatStore.activeTabId,
        )
        if (preferredTabId && preferredTabId !== chatStore.activeTabId) activateTab(preferredTabId)
        if (!savedRuntime && legacyRuntime) {
          await onLegacyRuntimeDiscovered(legacyRuntime)
        }
        setReady(true)
      } catch (cause) {
        if (!cancelled) setError(cause instanceof Error ? cause.message : 'Could not open Crew.')
      }
    }
    void prepare()
    return () => { cancelled = true }
  }, [onLegacyRuntimeDiscovered, session])

  const projectTabs = session ? visibleWorkProjectTabs(chatTabs, session.id, activeTabId) : []
  const preferredTabId = session
    ? preferredWorkProjectTabId(chatTabs, session.id, activeTabId)
    : null
  const activeProjectTab = projectTabs.find(tab => tab.tabId === preferredTabId) ?? projectTabs[0]

  return { tabId: ready ? activeProjectTab?.tabId ?? null : null, tabs: projectTabs, error }
}

function WorkChatTabs({
  tabs,
  activeTabId,
  workshopOpen,
  onWorkshopOpen,
  onChatOpen,
}: {
  tabs: ReturnType<typeof useWorkChatTabs>['tabs']
  activeTabId: string | null
  workshopOpen: boolean
  onWorkshopOpen: () => void
  onChatOpen: () => void
}) {
  const closeTab = useChatStore(state => state.closeTab)
  const renameTab = useChatStore(state => state.renameTab)
  const addToast = useChatStore(state => state.addToast)
  const close = useCallback((tabId: string) => {
    const nextId = activeTabId === tabId
      ? tabs.find(tab => tab.tabId !== tabId)?.tabId
      : undefined
    void closeTab(tabId, false).then(() => { if (nextId) activateTab(nextId) })
  }, [activeTabId, closeTab, tabs])

  const rename = useCallback(async (tab: (typeof tabs)[number], requested: string): Promise<boolean> => {
    if (!tab.sessionId) {
      addToast('Send the first message before naming this chat.', 'info')
      return false
    }
    const title = requested.replace(/\s+/g, ' ').trim()
    if (!title) {
      addToast('Enter a name for this chat.', 'info')
      return false
    }
    try {
      const response = await agentApi.renameChatHistorySession(
        tab.sessionId,
        title,
        tab.metadata?.agentProfileWorkspace,
      )
      renameTab(tab.tabId, response.title)
      addToast('Chat renamed', 'success')
      return true
    } catch {
      addToast('Failed to rename chat', 'error')
      return false
    }
  }, [addToast, renameTab, tabs])

  return (
    <div className="flex min-w-0 flex-1 items-center gap-1 overflow-hidden">
      {tabs.map(tab => {
        const builder = tab.metadata?.agentProfileBuilder === true
        return <AgentWorksChatTabItem
          key={tab.tabId}
          tab={tab}
          isActive={builder ? workshopOpen : (!workshopOpen && tab.tabId === activeTabId)}
          canClose={!builder}
          isBlank={builder}
          displayName={builder ? 'Workshop' : workTabDisplayName(tab.name === 'Builder' ? 'Chat' : tab.name)}
          onTabClick={(tabId) => {
            activateTab(tabId)
            if (builder) onWorkshopOpen()
            else onChatOpen()
          }}
          onCloseTab={close}
          onRenameTab={rename}
        />
      })}
    </div>
  )
}

function WorkTopBarControl({
  sessions,
  selected,
  onSelect,
  onNewProject,
  creating,
}: {
  sessions: WorkSession[]
  selected: WorkSession | null
  onSelect: (id: string) => void
  onNewProject: () => void
  creating: boolean
}) {
  const [open, setOpen] = useState(false)

  return (
    <TopBarEntitySelector
      label={selected?.title}
      leading={selected?.identity?.icon ? <span className="shrink-0 text-base leading-none" title={`Project agent identity: ${selected.identity.name || selected.identity.role || selected.identity.icon}`}>{selected.identity.icon}</span> : undefined}
      placeholder="New project"
      open={open}
      onToggle={() => setOpen(current => !current)}
      onClose={() => setOpen(false)}
      onAdd={onNewProject}
      addLabel="New project"
      addDisabled={creating}
    >
      <div role="menu" aria-label="Projects" className="max-h-96 space-y-1 overflow-y-auto p-2">
        <button
          type="button"
          onClick={() => { setOpen(false); onNewProject() }}
          disabled={creating}
          className="w-full rounded-md p-2 text-left text-sm text-gray-700 hover:bg-gray-100 disabled:opacity-50 dark:text-gray-300 dark:hover:bg-slate-700"
        >
          <span className="flex items-center gap-2 font-medium">
            <span className="h-2 w-2 rounded-full bg-blue-500" />
            {creating ? 'Creating project…' : '+ New project'}
          </span>
        </button>
        {sessions.length === 0 ? (
          <div className="p-2 text-center text-sm text-gray-500 dark:text-gray-400">No projects yet. Create one to get started.</div>
        ) : sessions.map(session => (
          <button
            key={session.id}
            type="button"
            role="menuitemradio"
            aria-checked={session.id === selected?.id}
            onClick={() => { onSelect(session.id); setOpen(false) }}
            className={`w-full rounded-md p-2 text-left text-sm transition-colors ${session.id === selected?.id ? 'bg-blue-100 text-blue-900 dark:bg-blue-900/30 dark:text-blue-100' : 'text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-slate-700'}`}
          >
            <span className="flex items-center gap-2">
              {session.identity?.icon
                ? <span className="w-5 shrink-0 text-center text-base leading-none" aria-hidden="true">{session.identity.icon}</span>
                : <span className="h-2 w-2 shrink-0 rounded-full bg-green-500" />}
              <span className="truncate font-medium">{session.title}</span>
            </span>
          </button>
        ))}
      </div>
    </TopBarEntitySelector>
  )
}

export function WorkSurface() {
  const { sessions, selected, select, create, updateLLMConfig, updateSelections, refresh, loading: sessionsLoading, error: sessionsError } = useWorkSessions()
  const workflowContextSignature = selected?.workflowContextPaths.join('\u0000') || ''
  const persistLegacyRuntime = useCallback(async (selection: WorkRuntimeSelection) => {
    if (!selected) return
    await updateLLMConfig(selected.id, selection)
  }, [selected, updateLLMConfig])
  const { tabId, tabs, error: chatError } = useWorkChatTabs(selected, persistLegacyRuntime)

  // A browser reload loses transient tab metadata while the durable references
  // remain in workflow.json. Force the first follow-up through the full profile
  // route so an old retained CLI cannot bypass the current read-only grants.
  useEffect(() => {
    if (selected?.id && workflowContextSignature) markWorkProjectRuntimeDirty(selected.id)
  }, [selected?.id, workflowContextSignature])
  const [projectRefreshInteractionKinds, setProjectRefreshInteractionKinds] = useState<Set<string>>(() => new Set())
  useEffect(() => {
    let cancelled = false
    void loadAgentProfileInteractionKinds(WORK_PROFILE_ID, 'product.refresh', WORK_PROFILE_VERSION).then(kinds => {
      if (cancelled) return
      setProjectRefreshInteractionKinds(kinds)
    })
    return () => { cancelled = true }
  }, [])
  const projectConfigRefreshToken = useChatStore(state => tabs
    .flatMap(tab => tab.sessionId ? state.tabEvents[tab.sessionId] || [] : [])
    .filter(event => {
      const interaction = parseProductInteraction(event)
      return (interaction?.product === 'work' && projectRefreshInteractionKinds.has(interaction.kind)) ||
        // Backward compatibility for events persisted before typed product interactions.
        event.type === 'work_identity_updated' || event.type === 'work_workflow_references_updated'
    })
    .map(event => event.id || event.timestamp || '')
    .join('|'))
  const handledProjectConfigRefreshToken = useRef('')

  useEffect(() => {
    if (!projectConfigRefreshToken || projectConfigRefreshToken === handledProjectConfigRefreshToken.current) return
    handledProjectConfigRefreshToken.current = projectConfigRefreshToken
    void refresh()
  }, [projectConfigRefreshToken, refresh])
  const [creating, setCreating] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [chatOpen, setChatOpen] = useState(true)
  const [workshopOpen, setWorkshopOpen] = useState(false)
  const [panelOpen, setPanelOpen] = useState(true)
  const [workspaceView, setWorkspaceView] = useState<WorkWorkspaceView>('files')
  const [workspaceViewRefresh, setWorkspaceViewRefresh] = useState(0)
  const [enabledWorkspacePanels, setEnabledWorkspacePanels] = useState<Set<string> | undefined>()
  const splitLayoutRef = useRef<HTMLDivElement>(null)
  const [splitRatio, setSplitRatioState] = useState(() => readWorkSplitRatio(selected?.id))
  const splitRatioRef = useRef(splitRatio)
  const { start: startSplitDrag, stop: stopSplitDrag } = usePointerDrag()
  const [createError, setCreateError] = useState<string | null>(null)
  const showProviders = useLLMStore((state) => state.showLLMModal)
  const activeSessionId = useChatStore(state => tabId ? state.chatTabs[tabId]?.sessionId : undefined)
  const legacyViewEvents = usePresentationEvents(activeSessionId ?? undefined, ['workflow.view'])
  const handledLegacyViewEvents = useRef<{ session?: string; count: number }>({ session: activeSessionId ?? undefined, count: legacyViewEvents.length })
  const openWorkPresentationView = useCallback((view: string) => {
    if (!(view in WORK_UI_PRESENTATION_VIEWS)) return
    const panel = WORK_UI_PRESENTATION_VIEWS[view as WorkUIPresentationView]
    if (enabledWorkspacePanels && !enabledWorkspacePanels.has(panel)) return
    setPanelOpen(true)
    setWorkspaceView(panel)
  }, [enabledWorkspacePanels])
  const workUIAdapter = useMemo<WorkspaceUIControlAdapter>(() => ({
    getView: () => workPresentationView(workspaceView),
    openView: openWorkPresentationView,
    isViewSupported: (view) => view in WORK_UI_PRESENTATION_VIEWS,
    labelForView: (view) => WORK_UI_LABELS[view as WorkUIPresentationView] ?? view,
    actorLabel: 'Crew',
  }), [openWorkPresentationView, workspaceView])
  useWorkspaceUIControl(activeSessionId ?? undefined, workUIAdapter)

  useEffect(() => {
    const session = activeSessionId ?? undefined
    if (handledLegacyViewEvents.current.session !== session) {
      handledLegacyViewEvents.current = { session, count: legacyViewEvents.length }
      return
    }
    for (const event of legacyViewEvents.slice(handledLegacyViewEvents.current.count)) {
      const view = event.payload.view
      if (typeof view === 'string') {
        openWorkPresentationView(view)
        if (event.payload.action === 'refresh') setWorkspaceViewRefresh(value => value + 1)
      }
    }
    handledLegacyViewEvents.current = { session, count: legacyViewEvents.length }
  }, [activeSessionId, legacyViewEvents, openWorkPresentationView])

  const changeWorkRuntime = useCallback(async (selection: WorkRuntimeSelection) => {
    if (!selected || !tabId) return
    const previous = workLLMSelectionFromConfig(selected.llmConfig)
    const providerChanged = Boolean(previous?.provider && selection.provider && previous.provider !== selection.provider)
    try {
      await updateLLMConfig(selected.id, selection)
      setWorkProjectRuntimeSelection(selected.id, tabId, selection, { newChatsOnly: providerChanged })
      if (providerChanged) {
        const chatStore = useChatStore.getState()
        const builder = Object.values(chatStore.chatTabs).find(
          tab => belongsToWorkProject(tab, selected.id) && tab.metadata?.agentProfileBuilder === true,
        )
        const current = chatStore.chatTabs[tabId]
        if (current && current.metadata?.agentProfileBuilder !== true) {
          await chatStore.closeTab(tabId, false)
        }
        if (builder) activateTab(builder.tabId)
        const label = selection.engine === 'claude-code' ? 'Claude Code'
          : selection.engine === 'codex-cli' ? 'Codex'
            : selection.engine === 'cursor-cli' ? 'Cursor'
              : selection.engine === 'pi-cli' ? 'Pi'
                : selection.engine === 'muse-cli' ? 'Muse'
                  : selection.engine
        chatStore.addToast(`Coding agent changed to ${label}. Existing chats keep their original agent; new chats will use ${label}.`, 'success')
      }
    } catch (cause) {
      useChatStore.getState().addToast(cause instanceof Error ? cause.message : 'Could not save the project model.', 'error')
    }
  }, [selected, tabId, updateLLMConfig])

  useEffect(() => {
    useModeStore.getState().setModeCategory('multi-agent')
    useAppStore.getState().setAgentMode('multi-agent')
    useAppStore.getState().setShowWorkflowsOverview(false)
    useAppStore.getState().setShowSchedulesOverview(false)
  }, [])

  useEffect(() => {
    let cancelled = false
    void loadAgentProfileUIPanels(WORK_PROFILE_ID, WORK_PROFILE_VERSION).then(panels => {
      // An unavailable/mismatched profile must not turn the entire project
      // workspace into an empty capability set. Crew has a complete local
      // panel implementation, so retain that safe UI fallback until the
      // resolved backend feature list is available.
      if (!cancelled) setEnabledWorkspacePanels(panels.size > 0 ? panels : undefined)
    })
    return () => { cancelled = true }
  }, [])

  useEffect(() => {
    if (enabledWorkspacePanels && !enabledWorkspacePanels.has(workspaceView)) {
      const fallback = enabledWorkspacePanels.has('files') ? 'files' : [...enabledWorkspacePanels][0]
      if (fallback) setWorkspaceView(fallback as WorkWorkspaceView)
    }
  }, [enabledWorkspacePanels, workspaceView])

  useEffect(() => {
    if (!selected) return
    const handleEngineSelection = (event: Event) => {
      const detail = (event as CustomEvent<ProductEngineSelectionDetail>).detail
      const sourceTabId = detail?.tabId || tabId
      const sourceTab = sourceTabId ? useChatStore.getState().chatTabs[sourceTabId] : undefined
      if (detail?.profileId !== WORK_PROFILE_ID || !detail.engine || !detail.modelId || !sourceTab || !belongsToWorkProject(sourceTab, selected.id)) return
      void changeWorkRuntime({
        engine: detail.engine!,
        provider: detail.provider,
        modelId: detail.modelId!,
        reasoningEffort: detail.reasoningEffort,
      })
    }
    window.addEventListener('agentworks:product-engine-selected', handleEngineSelection)
    return () => window.removeEventListener('agentworks:product-engine-selected', handleEngineSelection)
  }, [changeWorkRuntime, selected, tabId])

  useEffect(() => {
    const next = readWorkSplitRatio(selected?.id)
    splitRatioRef.current = next
    setSplitRatioState(next)
    return stopSplitDrag
  }, [selected?.id, stopSplitDrag])

  const setSplitRatio = useCallback((next: number, persist = false) => {
    const width = splitLayoutRef.current?.getBoundingClientRect().width || window.innerWidth
    const ratio = clampWorkSplitRatio(next, width)
    splitRatioRef.current = ratio
    setSplitRatioState(ratio)
    if (persist) writeWorkSplitRatio(selected?.id, ratio)
  }, [selected?.id])

  const handleSplitPointerDown = useCallback((event: ReactPointerEvent<HTMLButtonElement>) => {
    if (window.innerWidth < 768) return
    const rect = splitLayoutRef.current?.getBoundingClientRect()
    if (!rect?.width) return
    startSplitDrag(event, {
      onMove: clientX => setSplitRatio((clientX - rect.left) / rect.width),
      onEnd: () => writeWorkSplitRatio(selected?.id, splitRatioRef.current),
    })
  }, [selected?.id, setSplitRatio, startSplitDrag])

  const createProject = useCallback(async (title: string, description: string) => {
    if (creating) return
    setCreating(true)
    setCreateError(null)
    try {
      await create(title, description)
      setCreateOpen(false)
    } catch (cause) {
      setCreateError(cause instanceof Error ? cause.message : 'Could not create project.')
    } finally {
      setCreating(false)
    }
  }, [create, creating])

  const openCreateProject = useCallback(() => {
    setCreateError(null)
    setCreateOpen(true)
  }, [])

  const topBarControl = useMemo(() => (
    <WorkTopBarControl
      sessions={sessions}
      selected={selected}
      onSelect={select}
      onNewProject={openCreateProject}
      creating={creating}
    />
  ), [creating, openCreateProject, select, selected, sessions])

  const error = sessionsError || chatError

  useEffect(() => {
    const active = tabs.find(tab => tab.tabId === tabId)
    if (active) setWorkshopOpen(active.metadata?.agentProfileBuilder === true)
  }, [tabId, tabs])

  return (
    <div className="flex h-screen min-h-0 flex-col bg-background">
      <UpdateProgressToast />
      <GlobalHumanFeedbackPrompt />
      <ModePresetBar productControl={topBarControl} reduced />
      {createOpen ? (
        <CreateWorkProjectDialog
          onClose={() => { if (!creating) setCreateOpen(false) }}
          onCreate={createProject}
          submitting={creating}
          error={createError}
        />
      ) : null}
      <div
        data-ui-workspace={selected?.workspacePath}
        data-ui-view={selected ? workPresentationView(workspaceView) : undefined}
        className="relative min-h-0 flex-1 overflow-hidden"
      >
        <LlmModalHost />
        <div className={showProviders ? 'hidden' : 'h-full'}>
          {error ? (
            <div className="grid h-full place-items-center p-6 text-center text-sm text-destructive">{error}</div>
          ) : !selected ? (
            <div className="flex h-full items-center justify-center bg-gray-50 dark:bg-gray-900">
              {sessionsLoading || creating ? (
                <span className="text-sm text-muted-foreground"><Loader2 className="mr-2 inline h-4 w-4 animate-spin" />Opening Crew…</span>
              ) : (
                <div className="flex max-w-xl flex-col items-center px-6 text-center">
                  <div className="flex h-20 w-20 items-center justify-center rounded-full bg-gray-200 dark:bg-gray-700">
                    <span className="font-mono text-3xl font-semibold text-gray-600 dark:text-gray-200">&lt;&gt;</span>
                  </div>
                  <div className="mt-5">
                    <p className="text-xs font-semibold uppercase tracking-[0.18em] text-primary">Crew</p>
                    <h2 className="mt-2 text-2xl font-semibold text-gray-900 dark:text-gray-100">Your AI workspace for any project</h2>
                    <p className="mx-auto mt-2 max-w-lg text-sm leading-6 text-muted-foreground">
                      Create a persistent crew member for everyday questions, research, coding, and ongoing work. It can use your project files, browser, terminal, MCP servers, and connected tools.
                    </p>
                    <div className="mt-5 grid grid-cols-1 gap-2 text-left text-xs text-muted-foreground sm:grid-cols-3">
                      <div className="rounded-lg border border-border bg-background/70 px-3 py-2.5">
                        <span className="block font-medium text-foreground">Chat and create</span>
                        Ask questions, research, write, analyze, and keep the context together.
                      </div>
                      <div className="rounded-lg border border-border bg-background/70 px-3 py-2.5">
                        <span className="block font-medium text-foreground">Code and operate</span>
                        Work with files, code, the browser, terminal, skills, and MCP tools.
                      </div>
                      <div className="rounded-lg border border-border bg-background/70 px-3 py-2.5">
                        <span className="block font-medium text-foreground">Run automatically</span>
                        Continue work with schedules, triggers, background tasks, and bots.
                      </div>
                    </div>
                    <button
                      type="button"
                      onClick={openCreateProject}
                      className="mt-5 inline-flex items-center gap-1.5 rounded-md bg-primary px-3 py-1.5 text-sm font-medium text-primary-foreground hover:bg-primary/90"
                    >
                      <Plus className="h-3.5 w-3.5" /> Create your first project
                    </button>
                  </div>
                </div>
              )}
            </div>
          ) : (
            <>
              {!panelOpen ? (
                <button
                  type="button"
                  onClick={() => setPanelOpen(true)}
                  title="Show workspace"
                  aria-label="Show workspace"
                  className="absolute right-0 top-1/2 z-30 hidden -translate-y-1/2 flex-col items-center gap-1.5 rounded-l-lg border border-r-0 border-border bg-background/95 py-3 pl-1.5 pr-1 text-muted-foreground shadow-md backdrop-blur-sm transition-colors hover:bg-muted hover:text-foreground md:flex"
                >
                  <PanelRightOpen className="h-4 w-4" />
                  <span className="[writing-mode:vertical-rl] text-[10px] font-semibold uppercase tracking-wider">Workspace</span>
                </button>
              ) : null}
              {!chatOpen ? (
                <button
                  type="button"
                  onClick={() => setChatOpen(true)}
                  title="Show chat panel"
                  aria-label="Show chat panel"
                  className="absolute left-0 top-1/2 z-30 hidden -translate-y-1/2 flex-col items-center gap-1.5 rounded-r-lg border border-l-0 border-border bg-background/95 py-3 pl-1 pr-1.5 text-muted-foreground shadow-md backdrop-blur-sm transition-colors hover:bg-muted hover:text-foreground md:flex"
                >
                  <PanelLeftOpen className="h-4 w-4" />
                  <span className="[writing-mode:vertical-rl] text-[10px] font-semibold uppercase tracking-wider">Chat</span>
                </button>
              ) : null}
              <div
                ref={splitLayoutRef}
                className={`grid h-full min-h-0 min-w-0 grid-cols-1 grid-rows-[auto_minmax(0,1fr)] ${chatOpen && panelOpen ? 'md:[grid-template-columns:var(--work-split-columns)]' : ''}`}
                style={chatOpen && panelOpen ? ({ '--work-split-columns': `minmax(240px, ${splitRatio}fr) minmax(240px, ${1 - splitRatio}fr)` } as React.CSSProperties) : undefined}
              >
                <WorkspaceTopToolbar className={`${chatOpen && panelOpen ? 'md:col-span-2' : ''} col-start-1 row-start-1`}>
                  {chatOpen && tabId ? <WorkChatTabs
                    tabs={tabs}
                    activeTabId={tabId}
                    workshopOpen={workshopOpen}
                    onWorkshopOpen={() => setWorkshopOpen(true)}
                    onChatOpen={() => setWorkshopOpen(false)}
                  /> : <div className="min-w-0 flex-1" />}
                  {panelOpen ? <WorkWorkspaceToolbar workspacePath={selected.workspacePath} view={workspaceView} onViewChange={setWorkspaceView} enabledPanels={enabledWorkspacePanels} /> : null}
                </WorkspaceTopToolbar>
                {chatOpen ? <main className={`flex min-h-0 min-w-0 flex-col overflow-hidden bg-background col-start-1 row-start-2 ${panelOpen ? 'border-b border-border md:border-b-0 md:border-r' : ''}`}>
                  {tabId ? (
                      <div className="min-h-0 flex-1">
                        <ChatArea
                          tabId={tabId}
                          compact
                          onNewChat={() => activateTab(tabs.find(tab => tab.metadata?.agentProfileBuilder)?.tabId ?? tabId)}
                          previousChatsWorkspacePath={selected.workspacePath}
                          previousChatsRecentOnly
                          forcePreviousChats={workshopOpen}
                          composerPlaceholder="Describe what you want to build… (@ files, # automations)"
                          showCompactRuntimeLoading
                          showProductSteerAction
                          showProductTerminalControl
                        />
                      </div>
                  ) : (
                    <div className="grid h-full place-items-center p-6 text-center text-sm text-muted-foreground">
                      <span><Loader2 className="mr-2 inline h-4 w-4 animate-spin" />Opening project…</span>
                    </div>
                  )}
                </main> : null}
                {chatOpen && panelOpen ? (
                  <WorkspaceSplitDivider
                    ratio={splitRatio}
                    onPointerDown={handleSplitPointerDown}
                    onStep={delta => setSplitRatio(splitRatioRef.current + delta, true)}
                    className="md:row-start-2"
                  >
                    <WorkspaceSplitCollapseControls
                      onCollapseChat={() => setChatOpen(false)}
                      onCollapseWorkspace={() => setPanelOpen(false)}
                    />
                  </WorkspaceSplitDivider>
                ) : null}
                {panelOpen ? (
                  <aside
                    data-ui-workspace={selected.workspacePath}
                    data-ui-view={workPresentationView(workspaceView)}
                    className={`min-h-0 min-w-0 overflow-hidden bg-background row-start-2 ${chatOpen ? 'md:col-start-2' : 'col-start-1'}`}
                  >
                  {tabId ? (
                    <><span hidden data-ui-view-mounted /><WorkWorkspacePane
                        key={`${selected.id}:${workspaceViewRefresh}`}
                        workspacePath={selected.workspacePath}
                        projectId={selected.id}
                        projectTitle={selected.title}
                        tabId={tabId}
                        onClose={() => setPanelOpen(false)}
                        view={workspaceView}
                        onViewChange={setWorkspaceView}
                        enabledPanels={enabledWorkspacePanels}
                        projectLLMConfig={selected.llmConfig}
                        selectedSecrets={selected.selectedSecrets}
                        workflowContextPaths={selected.workflowContextPaths}
                        onRuntimeChange={changeWorkRuntime}
                        onSelectedServersChange={servers => updateSelections(selected.id, { selectedServers: servers })}
                        onSelectedSkillsChange={skills => updateSelections(selected.id, { selectedSkills: skills })}
                        onSelectedSecretsChange={secrets => updateSelections(selected.id, { selectedSecrets: secrets })}
                        onWorkflowContextPathsChange={async paths => {
                          await updateSelections(selected.id, { workflowContextPaths: paths })
                          markWorkProjectRuntimeDirty(selected.id)
                        }}
                      /></>
                  ) : (
                    <div className="grid h-full place-items-center text-sm text-muted-foreground">Opening workspace…</div>
                  )}
                  </aside>
                ) : null}
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  )
}
