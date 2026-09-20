import { Suspense, lazy, useCallback, useEffect, useState } from 'react'
import {
  Database,
  DollarSign,
  Files,
  Fingerprint,
  LayoutDashboard,
  Monitor,
  Server,
  Zap,
  type LucideIcon,
} from 'lucide-react'
import { AskAIButton } from '../../components/workflow/AskAIButton'
import { WorkspaceViewActions } from '../../components/workflow/WorkspaceViewActions'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../../components/ui/tooltip'
import { WorkspaceToolbarGroup } from '../../components/workspace/WorkspaceToolbarGroup'
import { ReportDocumentSwitcher } from '../../components/workflow/ReportDocumentSwitcher'
import { agentApi } from '../../services/api'
import type { PresetLLMConfig } from '../../services/api-types'
import { useChatStore } from '../../stores/useChatStore'
import { BrowserWorkspacePanel } from '../../components/workflow/BrowserWorkspacePanel'
import type { BrowserAutomationMode } from '../../components/BrowserAutomationSettings'
import { isBrowserCDPEnabled } from '../../utils/runtimeCapabilities'
import { sendWorkspacePaneMessageToChat } from '../../utils/workspacePaneChat'
import type { WorkRuntimeSelection } from './workTabs'
import type { ProductIdentity, ProductIdentityPatch } from '../../platform/chat/productProjects'
import { PreviousChatHistoryPanel } from '../../components/PreviousChatHistoryPanel'
import { useResumePreviousChat } from '../../hooks/useResumePreviousChat'
import { WorkIdentityPanel } from './WorkIdentityPanel'
import { WorkIntegrationsPanel } from './WorkIntegrationsPanel'
import { isWorkWorkspaceViewEnabled } from './workViewGating'

const CostsPopup = lazy(() => import('../../components/workflow/CostsPopup'))
const AutomationHubPanel = lazy(() => import('../../components/automation/AutomationHubPanel').then(module => ({ default: module.AutomationHubPanel })))
const ReportView = lazy(() => import('../../components/workflow/ReportViewer').then(module => ({ default: module.ReportView })))
const DatabaseView = lazy(() => import('../../components/workflow/DatabaseView'))
const FileWorkspacePane = lazy(() => import('../../components/FileWorkspacePane').then(module => ({ default: module.FileWorkspacePane })))

export type WorkWorkspaceView = 'dashboard' | 'database' | 'files' | 'browser' | 'costs' | 'schedules' | 'identity' | 'mcp'

function sendWorkProjectPaneMessage(projectId: string, message: string) {
  return sendWorkspacePaneMessageToChat({ profileId: 'work', conversationKey: projectId, message })
}

const VIEW_BUTTONS: Array<{ id: WorkWorkspaceView; label: string; icon: LucideIcon }> = [
  { id: 'dashboard', label: 'Dashboard', icon: LayoutDashboard },
  { id: 'browser', label: 'Browser', icon: Monitor },
  { id: 'schedules', label: 'Automation', icon: Zap },
]

const OPS_BUTTONS: Array<{ id: WorkWorkspaceView; label: string; icon: LucideIcon }> = [
  { id: 'files', label: 'Files', icon: Files },
  { id: 'database', label: 'Database', icon: Database },
  { id: 'costs', label: 'Costs and usage', icon: DollarSign },
]

const SETUP_BUTTONS: Array<{ id: WorkWorkspaceView; label: string; icon: LucideIcon }> = [
  { id: 'identity', label: 'Identity', icon: Fingerprint },
  { id: 'mcp', label: 'Integrations', icon: Server },
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
  const visibleSetup = enabledPanels ? SETUP_BUTTONS.filter(item => isWorkWorkspaceViewEnabled(item.id, enabledPanels)) : SETUP_BUTTONS
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
          {visibleOps.length > 0 && <WorkspaceToolbarGroup label="Ops" open={openGroup === 'ops'} onToggle={() => setOpenGroup(current => current === 'ops' ? null : 'ops')} title="Operations: project files, database and costs">
            <div className="inline-flex items-center gap-0.5">{visibleOps.map((item) => <WorkToolbarButton key={item.id} {...item} active={view === item.id} onClick={() => onViewChange(item.id)} />)}</div>
          </WorkspaceToolbarGroup>}
          {visibleSetup.length > 0 && <WorkspaceToolbarGroup label="Setup" open={openGroup === 'setup'} onToggle={() => setOpenGroup(current => current === 'setup' ? null : 'setup')} title="Setup: identity and integrations">
            <div className="inline-flex items-center gap-0.5">{visibleSetup.map((item) => <WorkToolbarButton key={item.id} {...item} active={view === item.id} onClick={() => onViewChange(item.id)} />)}</div>
          </WorkspaceToolbarGroup>}
        </div>
      </TooltipProvider>
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
  // Crew has no workspace refresh token: remount the panel to reload sessions.
  const [refreshNonce, setRefreshNonce] = useState(0)
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
      key={refreshNonce}
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
        <WorkspaceViewActions
          workspacePath={workspacePath}
          message="Help me configure browser access for this project. Ask what site or task it is for before changing anything."
          onAsk={async message => { await sendWorkProjectPaneMessage(projectId, message) }}
          onRefresh={() => setRefreshNonce(nonce => nonce + 1)}
          refreshLabel="Refresh Browser"
        />
      }
    />
  )
}

export function WorkWorkspacePane({ workspacePath, projectId, projectTitle, projectIdentity, tabId, view, enabledPanels, projectLLMConfig, selectedSecrets, selectedGlobalSecrets, workflowContextPaths, onRuntimeChange, onSelectedServersChange, onSelectedSkillsChange, onSelectedSecretsChange, onSelectedGlobalSecretsChange, onWorkflowContextPathsChange, onUpdateIdentity, onDeleteRequest }: { workspacePath: string; projectId: string; projectTitle: string; projectIdentity?: ProductIdentity; tabId: string; view: WorkWorkspaceView; enabledPanels?: Set<string>; projectLLMConfig?: PresetLLMConfig; selectedSecrets: string[]; selectedGlobalSecrets: string[]; workflowContextPaths: string[]; onRuntimeChange: (selection: WorkRuntimeSelection) => void | Promise<void>; onSelectedServersChange: (servers: string[]) => Promise<unknown>; onSelectedSkillsChange: (skills: string[]) => Promise<unknown>; onSelectedSecretsChange: (secrets: string[]) => Promise<unknown>; onSelectedGlobalSecretsChange: (secrets: string[]) => Promise<unknown>; onWorkflowContextPathsChange: (paths: string[]) => Promise<unknown>; onUpdateIdentity: (patch: ProductIdentityPatch) => Promise<unknown>; onDeleteRequest: () => void }) {
  const openHistoryChat = useResumePreviousChat()
  const activeSessionId = useChatStore(state => state.chatTabs[tabId]?.sessionId ?? undefined)
  const canonicalSessionId = useChatStore(state => Object.values(state.chatTabs).find(tab =>
    tab.metadata?.agentProfileId === 'work' &&
    tab.metadata?.agentProfileProjectId === projectId &&
    tab.metadata?.agentProfileConversationKey === projectId &&
    tab.metadata?.isViewOnly !== true,
  )?.sessionId)

  const updateSecretSelection = async (secrets: string[]) => {
    try {
      await onSelectedSecretsChange(secrets)
    } catch (cause) {
      useChatStore.getState().addToast(cause instanceof Error ? cause.message : 'Could not save project secret selection.', 'error')
    }
  }

  if (!isWorkWorkspaceViewEnabled(view, enabledPanels)) {
    return <div className="grid h-full place-items-center bg-background text-sm text-muted-foreground">No workspace view is enabled for this product.</div>
  }

  return (
    <div className="flex h-full min-h-0 flex-col bg-background">
      <div className="min-h-0 flex-1 overflow-hidden">
        {view === 'files' && <Suspense fallback={<div className="grid h-full place-items-center text-sm text-muted-foreground">Loading…</div>}><FileWorkspacePane workspacePath={workspacePath} hiddenRootFolders={['.git', 'node_modules', 'product.json', 'workflow.json']} hideManagedEntriesByDefault title="Workspace" hideAddToChat hideRootActions testId="work-files-panel" headerAction={<AskAIButton
          workspacePath={workspacePath}
          message="Help me with this Crew project's files. Ask what I want to find, understand, or change."
          onAsk={async message => { await sendWorkProjectPaneMessage(projectId, message) }}
          iconOnly
        />} /></Suspense>}
        {view === 'identity' && <WorkIdentityPanel
          workspacePath={workspacePath}
          projectTitle={projectTitle}
          projectIdentity={projectIdentity}
          tabId={tabId}
          selectedSecrets={selectedSecrets}
          selectedGlobalSecrets={selectedGlobalSecrets}
          workflowContextPaths={workflowContextPaths}
          projectLLMConfig={projectLLMConfig}
          enabledPanels={enabledPanels}
          onAsk={async message => { await sendWorkProjectPaneMessage(projectId, message) }}
          onRuntimeChange={onRuntimeChange}
          onSelectedSecretsChange={updateSecretSelection}
          onSelectedGlobalSecretsChange={onSelectedGlobalSecretsChange}
          onWorkflowContextPathsChange={onWorkflowContextPathsChange}
          onUpdateIdentity={onUpdateIdentity}
          onDeleteRequest={onDeleteRequest}
        />}
        {view === 'mcp' && <WorkIntegrationsPanel
          workspacePath={workspacePath}
          projectId={projectId}
          projectTitle={projectTitle}
          tabId={tabId}
          enabledPanels={enabledPanels}
          onAsk={async message => { await sendWorkProjectPaneMessage(projectId, message) }}
          onSelectedServersChange={onSelectedServersChange}
          onSelectedSkillsChange={onSelectedSkillsChange}
        />}
        <Suspense fallback={<div className="grid h-full place-items-center text-sm text-muted-foreground">Loading…</div>}>
          {view === 'dashboard' && <ReportView
            workspacePath={workspacePath}
            emptyIdentity={{ icon: projectIdentity?.icon, name: projectIdentity?.name || projectTitle, projectName: projectTitle }}
            emptyDescription="Ask Crew to create a visual dashboard for this project. It can organize tasks, notes, plans, status, research, or anything else you want to manage visually."
            sendChatMessage={async (message) => ({ status: 'queued', ...await sendWorkProjectPaneMessage(projectId, `From this project's dashboard:\n\n${message}`) })}
            headerAction={<AskAIButton
              workspacePath={workspacePath}
              message="Help me with this Crew project's results page. Explain what it shows in plain words and ask what I want to change."
              onAsk={async message => { await sendWorkProjectPaneMessage(projectId, message) }}
              iconOnly
            />}
          />}
          {view === 'database' && <DatabaseView workspacePath={workspacePath} headerAction={<AskAIButton
            workspacePath={workspacePath}
            message="Help me with this Crew project's stored records. Explain what's kept and ask what I want to look at or change."
            onAsk={async message => { await sendWorkProjectPaneMessage(projectId, message) }}
            iconOnly
          />} />}
          {view === 'browser' && <WorkBrowserPanel tabId={tabId} projectId={projectId} workspacePath={workspacePath} />}
          {view === 'costs' && <CostsPopup projectMode workspacePath={workspacePath} runFolders={[]} selectedRunFolder={null} emptyHint="Send a message to see this project's usage here." headerAction={<AskAIButton
            workspacePath={workspacePath}
            message="Help me understand what this Crew project costs to run. Explain in plain words where the money goes, then ask what I want to change."
            onAsk={async message => { await sendWorkProjectPaneMessage(projectId, message) }}
            iconOnly
          />} />}
          {view === 'schedules' && <AutomationHubPanel
            entityType="product"
            workspacePath={workspacePath}
            canManage
            scopeNoun="project"
            productTriggerScope={enabledPanels?.has('triggers') === false ? undefined : { profileId: 'work', projectId }}
            chatContent={<PreviousChatHistoryPanel
              workspacePath={workspacePath}
              activeSessionId={canonicalSessionId || activeSessionId}
              title=""
              emptyText="No earlier chats for this Crew member."
              recentOnly
              includeAutomationChats
              readOnly
              allowOpen
              openOnRowClick
              runEntityType="product"
              productTriggerScope={{ profileId: 'work', projectId }}
              fill
              showAll
              actionLabel="Open"
              onSelectSession={openHistoryChat}
            />}
            workflowScope={{ workflowId: projectId, workspacePath, label: projectTitle }}
            askAIMessages={{
              chats: "Help me with this Crew project's automation: past chats, schedules, and triggers. Explain what's here and ask what I want to review or change.",
              schedules: "Help me manage this project's schedules or authenticated webhook triggers. Each sends exactly one saved instruction to this Crew project; do not create workflow routes or workflow executions.",
              triggers: "Help me manage this project's authenticated triggers. Each trigger sends one saved instruction to this Crew project.",
            }}
            onAskAI={async message => { await sendWorkProjectPaneMessage(projectId, message) }}
          />}
        </Suspense>
      </div>
    </div>
  )
}
