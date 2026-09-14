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
import { loadAgentProfileUIPanels } from '../../utils/agentProfileCapabilities'
import { belongsToWorkProject, setWorkProjectRuntimeSelection, visibleWorkProjectTabs, type ProductEngineSelectionDetail, type WorkRuntimeSelection } from './workTabs'
import { updateProductProjectLLMConfig, updateProductProjectSelections } from '../../platform/chat/productProjects'

const WORK_SPLIT_PREFERENCE_KEY = 'work_workspace_split_ratio'

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

  const create = useCallback(async () => {
    const session = await createWorkSession('New project', '')
    setSessions((current) => [session, ...current])
    setSelectedId(session.id)
    return session
  }, [])

  const updateLLMConfig = useCallback(async (projectId: string, selection: WorkRuntimeSelection) => {
    const project = sessions.find(item => item.id === projectId)
    if (!project) throw new Error('This Work project is no longer available.')
    const llmConfig = workLLMConfigFromSelection({
      provider: selection.provider || selection.engine,
      modelId: selection.modelId,
      reasoningEffort: selection.reasoningEffort,
    })
    const updated = await updateProductProjectLLMConfig(project, llmConfig, `Update Work project model ${project.title}`)
    setSessions(current => current.map(item => item.id === projectId ? updated : item))
    return updated
  }, [sessions])

  const updateSelections = useCallback(async (projectId: string, patch: { selectedServers?: string[]; selectedSkills?: string[] }) => {
    const project = sessions.find(item => item.id === projectId)
    if (!project) throw new Error('This Work project is no longer available.')
    const updated = await updateProductProjectSelections(project, patch, `Update Work project integrations ${project.title}`)
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

        // Migrate the original one-chat Work tabs into the project-scoped tab
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
          })
          builder = chatStore.getTab(builderId)
        }
        if (cancelled || !builder) return
        chatStore.setTabMetadata(builder.tabId, { ...projectMetadata, ...selectedRuntimeMetadata })
        chatStore.setTabConfig(builder.tabId, { selectedServers: savedServers, selectedSkills: savedSkills })
        chatStore.setTabMetadata(builder.tabId, { agentProfileMCPSelectionInitialized: true })

        const currentlySelected = chatStore.activeTabId
          ? chatStore.chatTabs[chatStore.activeTabId]
          : undefined
        if (!currentlySelected || !belongsToWorkProject(currentlySelected, session.id)) {
          activateTab(chats[0]?.tabId ?? builder.tabId)
        }
        if (!savedRuntime && legacyRuntime) {
          await onLegacyRuntimeDiscovered(legacyRuntime)
        }
        setReady(true)
      } catch (cause) {
        if (!cancelled) setError(cause instanceof Error ? cause.message : 'Could not open Work.')
      }
    }
    void prepare()
    return () => { cancelled = true }
  }, [onLegacyRuntimeDiscovered, session])

  const projectTabs = session ? visibleWorkProjectTabs(chatTabs, session.id, activeTabId) : []
  const activeProjectTab = projectTabs.find(tab => tab.tabId === activeTabId) ?? projectTabs[0]

  return { tabId: ready ? activeProjectTab?.tabId ?? null : null, tabs: projectTabs, error }
}

function WorkChatTabs({ tabs, activeTabId }: { tabs: ReturnType<typeof useWorkChatTabs>['tabs']; activeTabId: string | null }) {
  const closeTab = useChatStore(state => state.closeTab)
  const close = useCallback((tabId: string) => {
    const nextId = activeTabId === tabId
      ? tabs.find(tab => tab.tabId !== tabId)?.tabId
      : undefined
    void closeTab(tabId, false).then(() => { if (nextId) activateTab(nextId) })
  }, [activeTabId, closeTab, tabs])

  return (
    <div className="flex min-w-0 flex-1 items-center gap-1 overflow-hidden">
      {tabs.map(tab => {
        const builder = tab.metadata?.agentProfileBuilder === true
        return <AgentWorksChatTabItem
          key={tab.tabId}
          tab={tab}
          isActive={tab.tabId === activeTabId}
          canClose={!builder}
          isBlank={builder}
          displayName={builder ? 'Builder' : (tab.name === 'Builder' ? 'Chat' : undefined)}
          onTabClick={activateTab}
          onCloseTab={close}
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
              <span className="h-2 w-2 shrink-0 rounded-full bg-green-500" />
              <span className="truncate font-medium">{session.title}</span>
            </span>
          </button>
        ))}
      </div>
    </TopBarEntitySelector>
  )
}

export function WorkSurface() {
  const { sessions, selected, select, create, updateLLMConfig, updateSelections, loading: sessionsLoading, error: sessionsError } = useWorkSessions()
  const persistLegacyRuntime = useCallback(async (selection: WorkRuntimeSelection) => {
    if (!selected) return
    await updateLLMConfig(selected.id, selection)
  }, [selected, updateLLMConfig])
  const { tabId, tabs, error: chatError } = useWorkChatTabs(selected, persistLegacyRuntime)
  const [creating, setCreating] = useState(false)
  const [chatOpen, setChatOpen] = useState(true)
  const [panelOpen, setPanelOpen] = useState(true)
  const [workspaceView, setWorkspaceView] = useState<WorkWorkspaceView>('files')
  const [enabledWorkspacePanels, setEnabledWorkspacePanels] = useState<Set<string> | undefined>()
  const splitLayoutRef = useRef<HTMLDivElement>(null)
  const [splitRatio, setSplitRatioState] = useState(() => readWorkSplitRatio(selected?.id))
  const splitRatioRef = useRef(splitRatio)
  const { start: startSplitDrag, stop: stopSplitDrag } = usePointerDrag()
  const [createError, setCreateError] = useState<string | null>(null)
  const showProviders = useLLMStore((state) => state.showLLMModal)

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
      if (!cancelled) setEnabledWorkspacePanels(panels)
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

  const createProject = useCallback(async () => {
    if (creating) return
    setCreating(true)
    setCreateError(null)
    try {
      await create()
    } catch (cause) {
      setCreateError(cause instanceof Error ? cause.message : 'Could not create project.')
    } finally {
      setCreating(false)
    }
  }, [create, creating])

  const topBarControl = useMemo(() => (
    <WorkTopBarControl
      sessions={sessions}
      selected={selected}
      onSelect={select}
      onNewProject={() => void createProject()}
      creating={creating}
    />
  ), [createProject, creating, select, selected, sessions])

  const error = sessionsError || createError || chatError

  return (
    <div className="flex h-screen min-h-0 flex-col bg-background">
      <UpdateProgressToast />
      <GlobalHumanFeedbackPrompt />
      <ModePresetBar productControl={topBarControl} reduced />
      <div className="relative min-h-0 flex-1 overflow-hidden">
        <LlmModalHost />
        <div className={showProviders ? 'hidden' : 'h-full'}>
          {error ? (
            <div className="grid h-full place-items-center p-6 text-center text-sm text-destructive">{error}</div>
          ) : !selected ? (
            <div className="flex h-full items-center justify-center bg-gray-50 dark:bg-gray-900">
              {sessionsLoading || creating ? (
                <span className="text-sm text-muted-foreground"><Loader2 className="mr-2 inline h-4 w-4 animate-spin" />Opening Work…</span>
              ) : (
                <div className="flex max-w-md flex-col items-center gap-4 text-center">
                  <div className="flex h-20 w-20 items-center justify-center rounded-full bg-gray-200 dark:bg-gray-700">
                    <span className="font-mono text-3xl font-semibold text-gray-600 dark:text-gray-200">&lt;&gt;</span>
                  </div>
                  <div>
                    <h2 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Create a project to get started</h2>
                    <button
                      type="button"
                      onClick={() => void createProject()}
                      className="mt-4 inline-flex items-center gap-1.5 rounded-md bg-primary px-3 py-1.5 text-sm font-medium text-primary-foreground hover:bg-primary/90"
                    >
                      <Plus className="h-3.5 w-3.5" /> New project
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
                  {chatOpen && tabId ? <WorkChatTabs tabs={tabs} activeTabId={tabId} /> : <div className="min-w-0 flex-1" />}
                  {panelOpen ? <WorkWorkspaceToolbar view={workspaceView} onViewChange={setWorkspaceView} enabledPanels={enabledWorkspacePanels} /> : null}
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
                  <aside className={`min-h-0 min-w-0 overflow-hidden bg-background row-start-2 ${chatOpen ? 'md:col-start-2' : 'col-start-1'}`}>
                  {tabId ? (
                    <WorkWorkspacePane
                      key={selected.id}
                      workspacePath={selected.workspacePath}
                      projectId={selected.id}
                      projectTitle={selected.title}
                      tabId={tabId}
                      onClose={() => setPanelOpen(false)}
                      view={workspaceView}
                      onViewChange={setWorkspaceView}
                      enabledPanels={enabledWorkspacePanels}
                      projectLLMConfig={selected.llmConfig}
                      onRuntimeChange={changeWorkRuntime}
                      onSelectedServersChange={servers => updateSelections(selected.id, { selectedServers: servers })}
                      onSelectedSkillsChange={skills => updateSelections(selected.id, { selectedSkills: skills })}
                    />
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
