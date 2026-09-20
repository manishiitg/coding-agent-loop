import { useState } from 'react'
import { usePersistentTab } from '../../hooks/usePersistentTab'
import { Server } from 'lucide-react'
import ServerSelectionDropdown from '../../components/ServerSelectionDropdown'
import ConnectorsBrowser from '../../components/connectors/ConnectorsBrowser'
import SkillsManagerPanel from '../../components/skills/SkillsManagerPanel'
import WorkflowBotsPanel from '../../components/workflow/WorkflowBotsPanel'
import WorkflowEmailPanel from '../../components/workflow/WorkflowEmailPanel'
import { CliMcpSetupPanel } from '../../components/integrations/CliMcpSetupPanel'
import { WorkspaceViewActions } from '../../components/workflow/WorkspaceViewActions'
import { WorkspaceViewHeader } from '../../components/workflow/WorkspaceViewHeader'
import { useChatStore } from '../../stores/useChatStore'
import { useMCPStore } from '../../stores/useMCPStore'
import { useAuthStore } from '../../stores/useAuthStore'
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
  cli: "Help me connect the command line or an AI assistant to this installation. Explain access tokens, the login command, and the MCP bridge, and ask what I want to do first.",
}

function WorkMCPTabBody({ tabId, projectId, workspacePath, onAsk, onSelectedServersChange }: {
  tabId: string
  projectId: string
  workspacePath: string
  onAsk: (message: string) => Promise<void>
  onSelectedServersChange: (servers: string[]) => Promise<unknown>
}) {
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

  return (
    <div className="flex flex-col gap-3">
      {/* Server picker — which integrations this view shows. Pickers live in
      content below the header, never in the header row (see exec-logs rule). */}
      <div className="flex shrink-0 items-center gap-2">
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
          align="left"
        />
      </div>
      <div>
        <ConnectorsBrowser compact manageOwnScroll={false} workspacePath={workspacePath} workspaceLabel="project" assistantLabel="agent" onAskAI={(message) => void onAsk(message)} />
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
  const isMultiUserMode = useAuthStore(state => state.isMultiUserMode)
  // The Connect tab points at this installation's hosted API origin, so it
  // only exists on multi-user servers — never on local installs.
  const visibleTabs = INTEGRATION_TABS.filter(option =>
    isWorkIntegrationTabEnabled(option.value, enabledPanels) && (option.value !== 'cli' || isMultiUserMode))
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
