import { Suspense, lazy, useCallback, useEffect, useState } from 'react'
import {
  BrainCircuit,
  Database,
  DollarSign,
  Files,
  FolderOpen,
  KeyRound,
  LayoutDashboard,
  Mail,
  Monitor,
  Puzzle,
  Server,
  Zap,
  type LucideIcon,
} from 'lucide-react'
import { FileWorkspacePane } from '../../components/FileWorkspacePane'
import ServerSelectionDropdown from '../../components/ServerSelectionDropdown'
import ConnectorsBrowser from '../../components/connectors/ConnectorsBrowser'
import SkillsManagerPanel from '../../components/skills/SkillsManagerPanel'
import { SecretSelectionSection } from '../../components/secrets/SecretSelectionSection'
import { AskAIButton } from '../../components/workflow/AskAIButton'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../../components/ui/tooltip'
import { WorkspaceToolbarGroup } from '../../components/workspace/WorkspaceToolbarGroup'
import { ReportDocumentSwitcher } from '../../components/workflow/ReportDocumentSwitcher'
import { agentApi, workFolderApi } from '../../services/api'
import type { PresetLLMConfig, WorkFolderGrant } from '../../services/api-types'
import { useChatStore } from '../../stores/useChatStore'
import { useMCPStore } from '../../stores/useMCPStore'
import { FolderGrantList } from '../../components/folders/FolderGrantList'
import { WorkflowReferenceAccess } from '../../components/folders/WorkflowReferenceAccess'
import { WorkModelsPanel } from './WorkModelsPanel'
import { BrowserWorkspacePanel } from '../../components/workflow/BrowserWorkspacePanel'
import WorkflowBotsPanel from '../../components/workflow/WorkflowBotsPanel'
import WorkflowEmailPanel from '../../components/workflow/WorkflowEmailPanel'
import type { BrowserAutomationMode } from '../../components/BrowserAutomationSettings'
import { isBrowserCDPEnabled } from '../../utils/runtimeCapabilities'
import { sendWorkspacePaneMessageToChat } from '../../utils/workspacePaneChat'
import type { WorkRuntimeSelection } from './workTabs'
import type { ProductIdentity } from '../../platform/chat/productProjects'
import { loadWorkSessions } from './workSessions'
import { PreviousChatHistoryPanel } from '../../components/PreviousChatHistoryPanel'
import { useResumePreviousChat } from '../../hooks/useResumePreviousChat'

const CostsPopup = lazy(() => import('../../components/workflow/CostsPopup'))
const AutomationHubPanel = lazy(() => import('../../components/automation/AutomationHubPanel').then(module => ({ default: module.AutomationHubPanel })))
const ReportView = lazy(() => import('../../components/workflow/ReportViewer').then(module => ({ default: module.ReportView })))
const DatabaseView = lazy(() => import('../../components/workflow/DatabaseView'))

export type WorkWorkspaceView = 'dashboard' | 'database' | 'files' | 'browser' | 'costs' | 'schedules' | 'skills' | 'mcp' | 'secrets' | 'models' | 'bots' | 'email' | 'folders'

function sendWorkProjectPaneMessage(projectId: string, message: string) {
  return sendWorkspacePaneMessageToChat({ profileId: 'work', conversationKey: projectId, message })
}

const VIEW_BUTTONS: Array<{ id: WorkWorkspaceView; label: string; icon: LucideIcon }> = [
  { id: 'dashboard', label: 'Dashboard', icon: LayoutDashboard },
  { id: 'browser', label: 'Browser', icon: Monitor },
  { id: 'costs', label: 'Costs and usage', icon: DollarSign },
  { id: 'schedules', label: 'Automation', icon: Zap },
]

const OPS_BUTTONS: Array<{ id: WorkWorkspaceView; label: string; icon: LucideIcon }> = [
  { id: 'files', label: 'Files', icon: Files },
  { id: 'database', label: 'Database', icon: Database },
]

const SETUP_BUTTONS: Array<{ id: WorkWorkspaceView; label: string; icon: LucideIcon }> = [
  { id: 'skills', label: 'Skills', icon: Puzzle },
  { id: 'secrets', label: 'Secrets', icon: KeyRound },
  { id: 'mcp', label: 'Integrations', icon: Server },
  { id: 'models', label: 'Agent configuration', icon: BrainCircuit },
  { id: 'email', label: 'Gmail', icon: Mail },
  { id: 'folders', label: 'Attached folders', icon: FolderOpen },
]

function WorkToolbarButton({ active, icon: Icon, label, onClick }: { active: boolean; icon: LucideIcon; label: string; onClick: () => void }) {
  const button = (
    <button
      type="button"
      onClick={onClick}
      className={`flex h-6 w-7 items-center justify-center rounded transition-colors ${active ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:bg-background/70 hover:text-foreground'}`}
      aria-label={label}
      aria-pressed={active}
    >
      <Icon className="h-3.5 w-3.5" />
    </button>
  )
  if (active) return button
  return <Tooltip><TooltipTrigger asChild>{button}</TooltipTrigger><TooltipContent side="bottom"><p>{label}</p></TooltipContent></Tooltip>
}

export function WorkWorkspaceToolbar({ workspacePath, view, onViewChange, enabledPanels }: { workspacePath: string; view: WorkWorkspaceView; onViewChange: (view: WorkWorkspaceView) => void; enabledPanels?: Set<string> }) {
  const visibleViews = enabledPanels ? VIEW_BUTTONS.filter(item => enabledPanels.has(item.id)) : VIEW_BUTTONS
  const visibleOps = enabledPanels ? OPS_BUTTONS.filter(item => enabledPanels.has(item.id)) : OPS_BUTTONS
  const visibleSetup = enabledPanels ? SETUP_BUTTONS.filter(item => enabledPanels.has(item.id)) : SETUP_BUTTONS
  const [openGroup, setOpenGroup] = useState<'ops' | 'setup' | null>(() =>
    OPS_BUTTONS.some(item => item.id === view) ? 'ops' : SETUP_BUTTONS.some(item => item.id === view) ? 'setup' : null,
  )

  useEffect(() => {
    setOpenGroup(OPS_BUTTONS.some(item => item.id === view) ? 'ops' : SETUP_BUTTONS.some(item => item.id === view) ? 'setup' : null)
  }, [view])

  return (
    <div data-tour="work-tools" className="ml-auto flex shrink-0 items-center gap-1">
      <TooltipProvider delayDuration={150}>
        {visibleViews.some(item => item.id === 'dashboard') && <ReportDocumentSwitcher workspacePath={workspacePath} active={view === 'dashboard'} onOpen={() => onViewChange('dashboard')} />}
        <div className="inline-flex h-8 items-center divide-x divide-border rounded-lg border border-border bg-muted/60 py-0.5 shadow-sm">
          <div className="inline-flex items-center gap-0.5 px-0.5">
            {visibleViews.filter(item => item.id !== 'dashboard').map((item) => <WorkToolbarButton key={item.id} {...item} active={view === item.id} onClick={() => onViewChange(item.id)} />)}
          </div>
          {visibleOps.length > 0 && <WorkspaceToolbarGroup label="Ops" open={openGroup === 'ops'} onToggle={() => setOpenGroup(current => current === 'ops' ? null : 'ops')} title="Operations: project files and database">
            <div className="inline-flex items-center gap-0.5">{visibleOps.map((item) => <WorkToolbarButton key={item.id} {...item} active={view === item.id} onClick={() => onViewChange(item.id)} />)}</div>
          </WorkspaceToolbarGroup>}
          <WorkspaceToolbarGroup label="Setup" open={openGroup === 'setup'} onToggle={() => setOpenGroup(current => current === 'setup' ? null : 'setup')} title="Setup: skills, secrets, integrations, models, connectors, Gmail and folders">
            <div className="inline-flex items-center gap-0.5">{visibleSetup.map((item) => <WorkToolbarButton key={item.id} {...item} active={view === item.id} onClick={() => onViewChange(item.id)} />)}</div>
          </WorkspaceToolbarGroup>
        </div>
      </TooltipProvider>
    </div>
  )
}

function WorkFoldersPanel({ workspacePath, workflowContextPaths, onWorkflowContextPathsChange }: { workspacePath: string; workflowContextPaths: string[]; onWorkflowContextPathsChange: (paths: string[]) => Promise<unknown> }) {
  const [folders, setFolders] = useState<WorkFolderGrant[]>([])
  const [roots, setRoots] = useState<string[]>([])
  const [path, setPath] = useState('')
  const [alias, setAlias] = useState('')
  const [access, setAccess] = useState<'read_only' | 'read_write'>('read_only')
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [crewReferences, setCrewReferences] = useState<Array<{ path: string; label: string; icon?: string }>>([])

  const refresh = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const response = await workFolderApi.listWorkFolders()
      setFolders(response.folders || [])
      setRoots(response.roots || [])
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not load attached folders.')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void refresh() }, [refresh])
  useEffect(() => {
    let cancelled = false
    void loadWorkSessions().then(sessions => {
      if (cancelled) return
      setCrewReferences(sessions.map(session => ({
        path: session.workspacePath,
        label: session.identity?.name || session.title,
        icon: session.identity?.icon,
      })))
    }).catch(() => { if (!cancelled) setCrewReferences([]) })
    return () => { cancelled = true }
  }, [])

  const add = async () => {
    if (!path.trim() || !alias.trim() || saving) return
    setSaving(true)
    setError('')
    try {
      await workFolderApi.addWorkFolder({ path: path.trim(), alias: alias.trim(), access })
      setPath('')
      setAlias('')
      await refresh()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not attach folder.')
    } finally {
      setSaving(false)
    }
  }

  const remove = async (folder: WorkFolderGrant) => {
    setError('')
    try {
      await workFolderApi.deleteWorkFolder(folder.id)
      await refresh()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not remove folder.')
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-col overflow-y-auto p-4">
      <h2 className="text-sm font-semibold text-foreground">Attached folders</h2>
      <p className="mt-1 text-xs text-muted-foreground">Give Crew access to an existing server folder in addition to this project.</p>
      {roots.length > 0 && <p className="mt-2 text-[11px] text-muted-foreground">Allowed roots: {roots.join(', ')}</p>}
      <div className="mt-4"><WorkflowReferenceAccess
        selectedPaths={workflowContextPaths}
        onChange={onWorkflowContextPathsChange}
        excludeWorkspacePath={workspacePath}
        additionalReferences={crewReferences}
        additionalLabel="Crew"
        showAdditionalGroup
      /></div>
      <div className="mt-4 grid gap-2 rounded-lg border border-border p-3">
        <input value={path} onChange={(event) => setPath(event.target.value)} placeholder="Absolute folder path" className="rounded-md border border-border bg-background px-3 py-2 text-sm" />
        <div className="grid grid-cols-[1fr_auto_auto] gap-2">
          <input value={alias} onChange={(event) => setAlias(event.target.value)} placeholder="Alias" className="min-w-0 rounded-md border border-border bg-background px-3 py-2 text-sm" />
          <select value={access} onChange={(event) => setAccess(event.target.value as typeof access)} className="rounded-md border border-border bg-background px-2 text-xs">
            <option value="read_only">Read only</option>
            <option value="read_write">Read &amp; write</option>
          </select>
          <button type="button" onClick={() => void add()} disabled={!path.trim() || !alias.trim() || saving} className="rounded-md bg-primary px-3 text-xs font-medium text-primary-foreground disabled:opacity-50">{saving ? 'Adding…' : 'Add'}</button>
        </div>
      </div>
      {error && <p className="mt-3 text-xs text-destructive">{error}</p>}
      <div className="mt-4">{loading ? <p className="text-xs text-muted-foreground">Loading…</p> : <FolderGrantList grants={folders} onRemove={remove} />}</div>
    </div>
  )
}

function WorkMCPPanel({ tabId, projectId, workspacePath, onSelectedServersChange }: { tabId: string; projectId: string; workspacePath: string; onSelectedServersChange: (servers: string[]) => Promise<unknown> }) {
  const selectedServers = useChatStore(state => state.chatTabs[tabId]?.config.selectedServers || [])
  const toolList = useMCPStore(state => state.toolList)
  const availableServers = [...new Set(toolList
    .filter(tool => tool.connection === 'connected' && tool.server)
    .map(tool => tool.server as string))]
    .sort((a, b) => a.localeCompare(b))
  const actualSelected = selectedServers.filter(server => server !== 'NO_SERVERS')

  const setSelected = async (servers: string[]) => {
    const store = useChatStore.getState()
    const selected = servers.length > 0 ? servers : ['NO_SERVERS']
    try {
      await onSelectedServersChange(servers)
    } catch (cause) {
      store.addToast(cause instanceof Error ? cause.message : 'Could not save project integrations.', 'error')
      return
    }
    for (const tab of Object.values(store.chatTabs)) {
      if (!projectId || tab.metadata?.agentProfileId !== 'work' || tab.metadata?.agentProfileProjectId !== projectId) continue
      store.setTabConfig(tab.tabId, { selectedServers: selected })
      store.setTabMetadata(tab.tabId, { agentProfileMCPSelectionInitialized: true, agentProfileRuntimeDirty: true })
    }
  }
  const askAI = async (message: string) => {
    await sendWorkProjectPaneMessage(projectId, message)
  }

  return (
    <div className="flex h-full min-h-0 flex-col p-4">
      <div className="mb-3 flex shrink-0 items-center justify-between gap-3 border-b border-border pb-3">
        <div>
          <h2 className="text-sm font-semibold text-foreground">Integrations</h2>
          <p className="mt-0.5 text-xs text-muted-foreground">Choose connected servers for this project, or connect another one.</p>
        </div>
        <ServerSelectionDropdown
          availableServers={availableServers}
          selectedServers={selectedServers}
          onServerToggle={(server) => void setSelected(actualSelected.includes(server)
            ? actualSelected.filter(item => item !== server)
            : [...actualSelected, server])}
          onSelectAll={() => void setSelected(availableServers)}
          onClearAll={() => void setSelected([])}
          agentMode="multi-agent"
          openDirection="down"
          align="right"
        />
      </div>
      <div className="min-h-0 flex-1">
        <ConnectorsBrowser compact workspacePath={workspacePath} workspaceLabel="project" assistantLabel="agent" onAskAI={askAI} />
      </div>
    </div>
  )
}

function WorkBrowserPanel({ tabId, projectId, workspacePath }: { tabId: string; projectId: string; workspacePath: string }) {
  const savedMode = useChatStore(state => state.chatTabs[tabId]?.config.browserMode ?? 'auto')
  const savedPort = useChatStore(state => state.chatTabs[tabId]?.config.cdpPort ?? 9222)
  const [browserMode, setBrowserMode] = useState<BrowserAutomationMode>(savedMode)
  const [cdpPort, setCdpPort] = useState(savedPort)
  const [cdpConnected, setCdpConnected] = useState<boolean | null>(null)
  const [cdpError, setCdpError] = useState<string | null>(null)
  const [cdpChecking, setCdpChecking] = useState(false)
  const [saving, setSaving] = useState(false)
  const dirty = browserMode !== savedMode || cdpPort !== savedPort

  useEffect(() => {
    setBrowserMode(savedMode)
    setCdpPort(savedPort)
  }, [savedMode, savedPort, tabId])

  const checkCdpConnection = useCallback(async (port: number) => {
    if (!isBrowserCDPEnabled()) {
      setCdpConnected(false)
      setCdpError('CDP is disabled on this server deployment.')
      return
    }
    setCdpChecking(true)
    setCdpConnected(null)
    setCdpError(null)
    try {
      const result = await agentApi.checkCdpPort(port)
      setCdpConnected(result.connected)
      setCdpError(result.connected ? null : result.error || null)
    } catch {
      setCdpConnected(false)
      setCdpError('Unable to check the CDP port.')
    } finally {
      setCdpChecking(false)
    }
  }, [])

  const save = useCallback(() => {
    const store = useChatStore.getState()
    const current = store.getTab(tabId)
    const projectId = current?.metadata?.agentProfileProjectId
    setSaving(true)
    for (const tab of Object.values(store.chatTabs)) {
      if (tab.tabId !== tabId && (!projectId || tab.metadata?.agentProfileProjectId !== projectId)) continue
      store.setTabConfig(tab.tabId, {
        browserMode,
        cdpPort,
        enableBrowserAccess: browserMode !== 'none',
        useCdp: browserMode === 'cdp',
      })
    }
    setSaving(false)
  }, [browserMode, cdpPort, tabId])

  return (
    <BrowserWorkspacePanel
      workspacePath={workspacePath}
      browserMode={browserMode}
      onBrowserModeChange={setBrowserMode}
      cdpPort={cdpPort}
      onCdpPortChange={setCdpPort}
      cdpConnected={cdpConnected}
      cdpError={cdpError}
      cdpChecking={cdpChecking}
      onCheckCdpConnection={checkCdpConnection}
      dirty={dirty}
      saving={saving}
      onSave={save}
      scopeNoun="project"
      assistantControl={
        <AskAIButton
          workspacePath={workspacePath}
          message="Help me configure browser access for this project. Ask what site or task it is for before changing anything."
          onAsk={async message => { await sendWorkProjectPaneMessage(projectId, message) }}
          iconOnly
          className="flex items-center gap-1.5 rounded p-1.5 text-muted-foreground hover:bg-muted"
        />
      }
    />
  )
}

export function WorkWorkspacePane({ workspacePath, projectId, projectTitle, projectIdentity, tabId, onClose, view, onViewChange, enabledPanels, projectLLMConfig, selectedSecrets, selectedGlobalSecrets, workflowContextPaths, onRuntimeChange, onSelectedServersChange, onSelectedSkillsChange, onSelectedSecretsChange, onSelectedGlobalSecretsChange, onWorkflowContextPathsChange }: { workspacePath: string; projectId: string; projectTitle: string; projectIdentity?: ProductIdentity; tabId: string; onClose: () => void; view: WorkWorkspaceView; onViewChange: (view: WorkWorkspaceView) => void; enabledPanels?: Set<string>; projectLLMConfig?: PresetLLMConfig; selectedSecrets: string[]; selectedGlobalSecrets: string[]; workflowContextPaths: string[]; onRuntimeChange: (selection: WorkRuntimeSelection) => void | Promise<void>; onSelectedServersChange: (servers: string[]) => Promise<unknown>; onSelectedSkillsChange: (skills: string[]) => Promise<unknown>; onSelectedSecretsChange: (secrets: string[]) => Promise<unknown>; onSelectedGlobalSecretsChange: (secrets: string[]) => Promise<unknown>; onWorkflowContextPathsChange: (paths: string[]) => Promise<unknown> }) {
  const openHistoryChat = useResumePreviousChat()
  const selectedSkills = useChatStore(state => state.chatTabs[tabId]?.config.selectedSkills || [])
  const activeSessionId = useChatStore(state => state.chatTabs[tabId]?.sessionId ?? undefined)

  const toggleSkill = async (folderName: string) => {
    const next = selectedSkills.includes(folderName)
      ? selectedSkills.filter((name) => name !== folderName)
      : [...selectedSkills, folderName]
    const store = useChatStore.getState()
    try {
      await onSelectedSkillsChange(next)
    } catch (cause) {
      store.addToast(cause instanceof Error ? cause.message : 'Could not save project skills.', 'error')
      return
    }
    for (const tab of Object.values(store.chatTabs)) {
      if (tab.metadata?.agentProfileId !== 'work' || tab.metadata?.agentProfileProjectId !== projectId) continue
      store.setTabConfig(tab.tabId, { selectedSkills: next })
      store.setTabMetadata(tab.tabId, { agentProfileRuntimeDirty: true })
    }
  }

  const updateSecretSelection = async (secrets: string[]) => {
    try {
      await onSelectedSecretsChange(secrets)
    } catch (cause) {
      useChatStore.getState().addToast(cause instanceof Error ? cause.message : 'Could not save project secret selection.', 'error')
    }
  }

  if (enabledPanels && !enabledPanels.has(view)) {
    return <div className="grid h-full place-items-center bg-background text-sm text-muted-foreground">No workspace view is enabled for this product.</div>
  }

  return (
    <div className="flex h-full min-h-0 flex-col bg-background">
      <div className="min-h-0 flex-1 overflow-hidden">
        {view === 'files' && <FileWorkspacePane workspacePath={workspacePath} hiddenRootFolders={['.git', 'node_modules', 'product.json', 'workflow.json']} hideManagedEntriesByDefault title="Workspace" hideAddToChat hideRootActions onClose={onClose} testId="work-files-panel" />}
        {view === 'skills' && <div className="flex h-full min-h-0 flex-col p-4"><SkillsManagerPanel compact workspacePath={workspacePath} selectedSkills={selectedSkills} onToggleSkill={folderName => { void toggleSkill(folderName) }} selectionLabel="Skills for this project" emptySelectionText="No project skills yet — pick one below." selectionScopeLabel="project" /></div>}
        {view === 'mcp' && <WorkMCPPanel tabId={tabId} projectId={projectId} workspacePath={workspacePath} onSelectedServersChange={onSelectedServersChange} />}
        {view === 'secrets' && <div className="h-full overflow-y-auto p-4"><SecretSelectionSection
          selectedSecrets={selectedSecrets}
          onSecretChange={secrets => { void updateSecretSelection(secrets) }}
          selectedGlobalSecrets={selectedGlobalSecrets}
          onGlobalSecretChange={secrets => { void onSelectedGlobalSecretsChange(secrets || []) }}
          persistExplicitGlobalSelection
          workflowPath={workspacePath}
          workspaceNoun="project"
          workspaceSecretHeading="Project secrets"
          workspaceBadgeLabel="Project"
          workspaceSharingBadgeLabel="Project"
          showGlobalSecrets
          showSharedSecrets={false}
          allowGlobalPromotion
        /></div>}
        {view === 'folders' && <WorkFoldersPanel workspacePath={workspacePath} workflowContextPaths={workflowContextPaths} onWorkflowContextPathsChange={onWorkflowContextPathsChange} />}
        {view === 'models' && <WorkModelsPanel tabId={tabId} workspacePath={workspacePath} onAsk={async message => { await sendWorkProjectPaneMessage(projectId, message) }} projectLLMConfig={projectLLMConfig} onRuntimeChange={onRuntimeChange} />}
        {view === 'email' && <div className="h-full overflow-y-auto p-4"><WorkflowEmailPanel workspacePath={workspacePath} /></div>}
        {view === 'bots' && <div className="h-full overflow-y-auto p-4"><WorkflowBotsPanel
          workspacePath={workspacePath}
          scopeNoun="project"
          onAsk={async message => { await sendWorkProjectPaneMessage(projectId, message) }}
          target={{ profileId: 'work', conversationKey: projectId, label: projectTitle }}
        /></div>}
        <Suspense fallback={<div className="grid h-full place-items-center text-sm text-muted-foreground">Loading…</div>}>
          {view === 'dashboard' && <ReportView
            workspacePath={workspacePath}
            emptyIdentity={{ icon: projectIdentity?.icon, name: projectIdentity?.name || projectTitle, projectName: projectTitle }}
            emptyDescription="Ask Crew to create a visual dashboard for this project. It can organize tasks, notes, plans, status, research, or anything else you want to manage visually."
            sendChatMessage={async (message) => ({ status: 'queued', ...await sendWorkProjectPaneMessage(projectId, `From this project's dashboard:\n\n${message}`) })}
          />}
          {view === 'database' && <DatabaseView workspacePath={workspacePath} />}
          {view === 'browser' && <WorkBrowserPanel tabId={tabId} projectId={projectId} workspacePath={workspacePath} />}
          {view === 'costs' && <CostsPopup isOpen embedded projectMode onClose={() => onViewChange('files')} workspacePath={workspacePath} runFolders={[]} selectedRunFolder={null} emptyHint="Send a message to see this project's usage here." />}
          {view === 'schedules' && <AutomationHubPanel
            entityType="product"
            workspacePath={workspacePath}
            entityLabel={projectIdentity?.name || projectTitle}
            entityIcon={projectIdentity?.icon}
            canManage
            scopeNoun="project"
            productTriggerScope={enabledPanels?.has('triggers') === false ? undefined : { profileId: 'work', projectId }}
            chatContent={<PreviousChatHistoryPanel
              workspacePath={workspacePath}
              activeSessionId={activeSessionId}
              title=""
              emptyText="No earlier chats for this Crew member."
              recentOnly
              readOnly
              allowOpen
              fill
              showAll
              actionLabel="Open"
              onSelectSession={openHistoryChat}
            />}
            botContent={enabledPanels?.has('bots') === false ? undefined : <div className="h-full overflow-y-auto p-4"><WorkflowBotsPanel
              workspacePath={workspacePath}
              scopeNoun="project"
              onAsk={async message => { await sendWorkProjectPaneMessage(projectId, message) }}
              target={{ profileId: 'work', conversationKey: projectId, label: projectTitle }}
            /></div>}
            workflowScope={{ workflowId: projectId, workspacePath, label: projectTitle }}
            scheduleHeaderAction={<AskAIButton
              workspacePath={workspacePath}
              message="Help me manage this project's schedules or authenticated webhook triggers. Each sends exactly one saved instruction to this Crew project; do not create workflow routes or workflow executions."
              onAsk={async message => { await sendWorkProjectPaneMessage(projectId, message) }}
              iconOnly
            />}
            triggerHeaderAction={<AskAIButton
              workspacePath={workspacePath}
              message="Help me manage this project's authenticated triggers. Each trigger sends one saved instruction to this Crew project."
              onAsk={async message => { await sendWorkProjectPaneMessage(projectId, message) }}
              iconOnly
            />}
          />}
        </Suspense>
      </div>
    </div>
  )
}
