import { useMemo, useState } from 'react'
import { usePersistentTab } from '../../hooks/usePersistentTab'
import { AlertTriangle, Search, Server } from 'lucide-react'
import ConnectorsBrowser from '../../components/connectors/ConnectorsBrowser'
import { ToolSelectionSection } from '../../components/ToolSelectionSection'
import { isSelectedServer, serverNamesMatch } from '../../utils/mcpServerAlias'
import SkillsManagerPanel from '../../components/skills/SkillsManagerPanel'
import WorkflowBotsPanel from '../../components/workflow/WorkflowBotsPanel'
import WorkflowEmailPanel from '../../components/workflow/WorkflowEmailPanel'
import { CliMcpSetupPanel } from '../../components/integrations/CliMcpSetupPanel'
import { WorkspaceViewActions } from '../../components/workflow/WorkspaceViewActions'
import { WorkspaceViewHeader } from '../../components/workflow/WorkspaceViewHeader'
import { useChatStore } from '../../stores/useChatStore'
import { useMCPStore } from '../../stores/useMCPStore'
import { isWorkIntegrationTabEnabled } from './workViewGating'

export type WorkIntegrationTab = 'apps' | 'skills' | 'slack' | 'whatsapp' | 'gmail' | 'cli'

const INTEGRATION_TABS: Array<{ value: WorkIntegrationTab; label: string }> = [
  { value: 'apps', label: 'MCPs' },
  { value: 'skills', label: 'Skills' },
  { value: 'slack', label: 'Slack' },
  { value: 'whatsapp', label: 'WhatsApp' },
  { value: 'gmail', label: 'Gmail' },
  { value: 'cli', label: 'Connect' },
]

const INTEGRATION_TAB_ASK_AI_MESSAGE: Record<WorkIntegrationTab, string> = {
  apps: "Help me with this Crew project's connected apps. Explain what's connected and ask what I want to add or change.",
  skills: "Help me with this Crew project's skills. Explain what's available and ask what I want to add or change.",
  slack: "Help me with this Crew project's Slack bot. Explain what's connected and ask what I want to change.",
  whatsapp: "Help me with this Crew project's WhatsApp bot. Explain what's connected and ask what I want to change.",
  gmail: "Help me with this Crew project's Gmail. Explain the setup and ask what I want to change.",
  cli: "Help me connect an AI agent to this installation through MCP. Explain the HTTP MCP URL and browser sign-in, and ask which AI app I use.",
}

export function WorkMCPTabBody({ tabId, projectId, workspacePath, onAsk, onSelectedServersChange }: {
  tabId: string
  projectId: string
  workspacePath: string
  onAsk: (message: string) => Promise<void>
  onSelectedServersChange: (servers: string[]) => Promise<unknown>
}) {
  const selectedServers = useChatStore(state => state.chatTabs[tabId]?.config.selectedServers || [])
  const toolList = useMCPStore(state => state.toolList)
  const toolsLoading = useMCPStore(state => state.isLoadingTools)
  // Mirror the workflow tab: connected servers plus already-selected ones
  // (a selected-but-since-disconnected server stays visible/manageable
  // instead of silently vanishing from the project's config).
  const availableServers = useMemo(() => {
    const connected = toolList
      .filter(tool => tool.connection === 'connected' && tool.server)
      .map(tool => tool.server as string)
    return [...new Set([...connected, ...selectedServers.filter(server => server !== 'NO_SERVERS')])]
  }, [toolList, selectedServers])
  const actualSelected = selectedServers.filter(server => server !== 'NO_SERVERS')
  const selectedAvailableServers = useMemo(() => availableServers.filter(serverName => isSelectedServer(actualSelected, serverName)), [availableServers, actualSelected])
  const unselectedAvailableServers = useMemo(() => availableServers.filter(serverName => !isSelectedServer(actualSelected, serverName)), [availableServers, actualSelected])
  const [searchQuery, setSearchQuery] = useState('')
  // Selected for this project but not connected to the platform (e.g. a Crew
  // the Builder created with an app that still needs sign-in). Its tools do
  // not work until someone connects it, so say so and offer the way.
  const needsConnecting = useMemo(() => toolsLoading ? [] : actualSelected.filter(serverName =>
    !toolList.some(tool => tool.server && serverNamesMatch(tool.server, serverName) && tool.connection === 'connected')),
  [toolsLoading, actualSelected, toolList])

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

  return (
    <div className="flex flex-col gap-3">
      {needsConnecting.length > 0 && (
        <div data-testid="work-mcp-needs-connecting" className="rounded-md border border-amber-300 bg-amber-50 p-3 text-sm text-amber-900 dark:border-amber-700/60 dark:bg-amber-950/30 dark:text-amber-200">
          <div className="flex items-center gap-2 font-medium">
            <AlertTriangle className="h-4 w-4 shrink-0" />
            Needs connecting
          </div>
          <p className="mt-1 text-xs leading-5">
            This project uses {needsConnecting.length === 1 ? 'an app that is' : 'apps that are'} not connected yet. Its tools won't work until {needsConnecting.length === 1 ? 'it is' : 'they are'} connected.
          </p>
          <ul className="mt-2 space-y-1.5">
            {needsConnecting.map(serverName => (
              <li key={serverName} className="flex flex-wrap items-center gap-2">
                <span className="font-medium">{serverName}</span>
                <button
                  type="button"
                  className="rounded border border-amber-400 px-2 py-0.5 text-xs hover:bg-amber-100 dark:border-amber-600 dark:hover:bg-amber-900/40"
                  onClick={() => setSearchQuery(serverName)}
                >
                  Connect
                </button>
                <button
                  type="button"
                  className="rounded px-2 py-0.5 text-xs underline-offset-2 hover:underline"
                  onClick={() => void onAsk(`Help me connect ${serverName} for this Crew project. It is selected but not connected yet; walk me through signing in or adding its credentials.`)}
                >
                  Ask agent
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
      {selectedAvailableServers.length > 0 && (
        <div>
          <div className="mb-3 text-sm font-medium text-muted-foreground">
            This project
          </div>
          <ToolSelectionSection
            stepId="project-selected"
            availableServers={selectedAvailableServers}
            selectedServers={actualSelected}
            selectedTools={[]}
            onServerChange={(servers) => void setSelected(servers)}
            onToolChange={() => {}}
            query={searchQuery}
            agentMode="multi-agent"
            hideHeader
            hideToolDetails
            manageOwnScroll={false}
          />
        </div>
      )}
      {unselectedAvailableServers.length > 0 && (
        <div className="mt-3 border-t border-border pt-3">
          <div className="mb-1 text-sm font-medium text-muted-foreground">
            Platform connected
          </div>
          <p className="mb-3 text-xs leading-5 text-muted-foreground">
            Shared with everyone. Tick one to let this project use it.
          </p>
          <ToolSelectionSection
            stepId="project-available"
            availableServers={unselectedAvailableServers}
            selectedServers={actualSelected}
            selectedTools={[]}
            onServerChange={(servers) => void setSelected(servers)}
            onToolChange={() => {}}
            query={searchQuery}
            agentMode="multi-agent"
            hideHeader
            hideToolDetails
            manageOwnScroll={false}
          />
        </div>
      )}
      <div className="relative mt-3 shrink-0">
        <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-400" />
        <input
          type="text"
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          placeholder="Search apps"
          aria-label="Search apps"
          className="w-full rounded-lg border border-gray-300 bg-white py-2.5 pl-10 pr-3 text-sm text-gray-900 placeholder-gray-400 transition-colors focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500 dark:border-gray-700 dark:bg-gray-800 dark:text-gray-100"
        />
      </div>
      <div className="mt-3 border-t border-border pt-3">
        <div className="mb-3 text-sm font-medium text-muted-foreground">
          Connect a new app
        </div>
        <ConnectorsBrowser
          compact
          manageOwnScroll={false}
          workspacePath={workspacePath}
          workspaceLabel="project"
          assistantLabel="agent"
          onAskAI={(message) => void onAsk(message)}
          query={searchQuery}
          hideSearch
          hideConnectedSection
        />
      </div>
    </div>
  )
}

export function WorkIntegrationsPanel({ workspacePath, projectId, projectTitle, tabId, enabledPanels, onAsk, onSelectedServersChange, onSelectedSkillsChange }: {
  workspacePath: string
  projectId: string
  projectTitle: string
  tabId: string
  enabledPanels?: Set<string>
  onAsk: (message: string) => Promise<void>
  onSelectedServersChange: (servers: string[]) => Promise<unknown>
  onSelectedSkillsChange: (skills: string[]) => Promise<unknown>
}) {
  // The Connect tab points at this installation's API origin. Hosted apps need
  // a public origin; local agents can connect directly to a loopback MCP URL.
  const visibleTabs = INTEGRATION_TABS.filter(option =>
    isWorkIntegrationTabEnabled(option.value, enabledPanels))
  const [tab, setTab] = usePersistentTab<WorkIntegrationTab>('agentworks.tab.crew-integrations', 'apps', INTEGRATION_TABS.map(option => option.value))
  const activeTab = visibleTabs.some(option => option.value === tab) ? tab : visibleTabs[0].value
  // Every tab loads on mount, so Refresh always remounts.
  const [tabNonce, setTabNonce] = useState(0)
  const selectedSkills = useChatStore(state => state.chatTabs[tabId]?.config.selectedSkills || [])

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

  return (
    <div className="flex h-full min-h-0 flex-col bg-background">
      <WorkspaceViewHeader
        icon={Server}
        title="Integrations"
        helpTopic={`Integrations · ${visibleTabs.find(option => option.value === activeTab)?.label ?? 'MCPs'}`}
        subtitle="Choose connected apps, skills, and bots for this project."
        actions={(
          <WorkspaceViewActions
            workspacePath={workspacePath}
            message={INTEGRATION_TAB_ASK_AI_MESSAGE[activeTab]}
            onAsk={onAsk}
            onRefresh={() => setTabNonce(nonce => nonce + 1)}
            refreshLabel={`Refresh ${visibleTabs.find(option => option.value === activeTab)?.label ?? 'view'}`}
          />
        )}
        tabs={{ value: activeTab, onChange: (value: string) => setTab(value as WorkIntegrationTab), options: visibleTabs, ariaLabel: 'Integrations' }}
      />
      <div key={`${activeTab}:${tabNonce}`} className="min-h-0 flex-1 overflow-y-auto p-4">
        {activeTab === 'apps' && <WorkMCPTabBody
          tabId={tabId}
          projectId={projectId}
          workspacePath={workspacePath}
          onAsk={onAsk}
          onSelectedServersChange={onSelectedServersChange}
        />}
        {activeTab === 'skills' && <SkillsManagerPanel
          compact
          manageOwnScroll={false}
          workspacePath={workspacePath}
          selectedSkills={selectedSkills}
          onToggleSkill={folderName => { void toggleSkill(folderName) }}
          selectionLabel="Skills for this project"
          emptySelectionText="No project skills yet — pick one below."
          selectionScopeLabel="project"
        />}
        {activeTab === 'slack' && <WorkflowBotsPanel
          workspacePath={workspacePath}
          fixedChannel="slack"
          scopeNoun="project"
          onAsk={onAsk}
          target={{ profileId: 'work', conversationKey: projectId, label: projectTitle }}
        />}
        {activeTab === 'whatsapp' && <WorkflowBotsPanel
          workspacePath={workspacePath}
          fixedChannel="whatsapp"
          scopeNoun="project"
          onAsk={onAsk}
          target={{ profileId: 'work', conversationKey: projectId, label: projectTitle }}
        />}
        {activeTab === 'gmail' && <WorkflowEmailPanel workspacePath={workspacePath} scopeNoun="project" onAsk={onAsk} />}
        {activeTab === 'cli' && <CliMcpSetupPanel />}
      </div>
    </div>
  )
}
