import { capabilitiesEqual, mergeRemoteCapabilities } from './workflowCapabilitiesSync'
import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { LoaderCircle, Save } from 'lucide-react'
import { ToolSelectionSection } from '../ToolSelectionSection'
import SkillsManagerPanel from '../skills/SkillsManagerPanel'
import PlaybooksPanel from '../playbooks/PlaybooksPanel'
import { SecretSelectionSection } from '../secrets/SecretSelectionSection'
import WorkflowLLMConfigurationPanel from './WorkflowLLMConfigurationPanel'
import WorkflowBotsPanel from './WorkflowBotsPanel'
import WorkflowEmailPanel from './WorkflowEmailPanel'
import ConnectorsBrowser from '../connectors/ConnectorsBrowser'
import { agentApi, workflowManifestApi } from '../../services/api'
import type { WorkflowCapabilities } from '../../services/api-types'
import { useMCPStore } from '../../stores/useMCPStore'
import { useWorkflowManifestStore } from '../../stores/useWorkflowManifestStore'
import { useWorkflowStore } from '../../stores/useWorkflowStore'
import { useCanWriteWorkflow } from '../../hooks/useCanWriteWorkflow'
import { getWorkspaceView, type CapabilityViewId } from './workspaceViews'
import { BrowserWorkspacePanel } from './BrowserWorkspacePanel'
import type { BrowserAutomationMode } from '../BrowserAutomationSettings'
import { WorkspaceViewActions } from './WorkspaceViewActions'
import { getWorkspaceAskAIMessage } from './workspaceAskAI'

// Which sections exist is decided by the registry in workspaceViews.ts; this
// panel only carries the per-section copy.
export type WorkflowCapabilitySection = CapabilityViewId

interface WorkflowCapabilitiesPanelProps {
  section: WorkflowCapabilitySection
  workspacePath: string | null
}

const EMPTY_CAPABILITIES: WorkflowCapabilities = {
  selected_servers: [],
  selected_tools: [],
  selected_skills: [],
  selected_secrets: [],
  selected_global_secret_names: null,
  browser_mode: 'none',
  use_code_execution_mode: false,
}

// `savesViaManifest`: the section edits `capabilities` and persists through the
// Save footer. Sections that write straight to shared state (bots routing and
// credentials) never touch the manifest, so the footer would save nothing.
const SECTION_COPY: Record<WorkflowCapabilitySection, { title: string; description: string; savesViaManifest: boolean }> = {
  playbooks: {
    title: 'Workflow playbooks',
    description: 'Apply proven AgentWorks setups to this workflow with the Builder.',
    savesViaManifest: false,
  },
  skills: {
    title: 'Workflow skills',
    description: 'Select reusable skills for this workflow’s builder context.',
    savesViaManifest: true,
  },
  mcp: {
    title: 'Workflow MCP',
    description: 'Select the MCP servers and tools this workflow may use.',
    // Selection changes persist immediately, so this long directory can use
    // one uninterrupted scroll surface without a fixed Save footer.
    savesViaManifest: false,
  },
  secrets: {
    title: 'Workflow secrets',
    description: 'Choose which workflow and global secrets this workflow may access.',
    // A tick is the whole action: it writes the manifest right away, like
    // the LLM section, so there is nothing left for a footer Save to do.
    savesViaManifest: false,
  },
  browser: {
    title: 'Browser automation',
    description: 'Watch this workflow’s browser and configure its automation access.',
    savesViaManifest: true,
  },
  llm: {
    title: 'Workflow LLM configuration',
    description: 'Pick the provider this workflow runs on. Changes apply immediately.',
    // Every change here (provider pick, "Use in this workflow", Advanced
    // role pins) writes the manifest on its own, so the footer Save would
    // only ever show "nothing to save".
    savesViaManifest: false,
  },
  email: {
    title: 'Email',
    description: 'Gmail accounts, default recipients, and email access settings. Connections are shared across AgentWorks.',
    savesViaManifest: false,
  },
  bots: {
    title: 'Workflow bots',
    description: 'Slack channels and WhatsApp slugs this workflow answers on. Connections are shared by all workflows.',
    savesViaManifest: false,
  },
}

export default function WorkflowCapabilitiesPanel({ section, workspacePath }: WorkflowCapabilitiesPanelProps) {
  const canWriteWorkflow = useCanWriteWorkflow(workspacePath)
  const [capabilities, setCapabilities] = useState<WorkflowCapabilities>(EMPTY_CAPABILITIES)
  // What the manifest last held, so the footer can tell "edited" from "saved".
  const [loaded, setLoaded] = useState<WorkflowCapabilities>(EMPTY_CAPABILITIES)
  const dirty = useMemo(() => !capabilitiesEqual(capabilities, loaded), [capabilities, loaded])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [cdpPort, setCdpPort] = useState(9222)
  const [cdpConnected, setCdpConnected] = useState<boolean | null>(null)
  const [cdpError, setCdpError] = useState<string | null>(null)
  const [cdpChecking, setCdpChecking] = useState(false)
  const toolList = useMCPStore(state => state.toolList)
  const refreshTools = useMCPStore(state => state.refreshTools)
  const [refreshingServers, setRefreshingServers] = useState(false)
  // "Available to select for this workflow" means connected -- you can't
  // meaningfully pick tools from a server nobody has authenticated yet. A
  // not-yet-connected server only belongs in the "Connect a new MCP server"
  // browser below, not this checklist. Still include an already-selected
  // server even if it's since been disconnected, so it stays visible/
  // manageable here instead of silently vanishing from the workflow's config.
  const availableServers = useMemo(() => {
    const connected = toolList
      .filter(tool => tool.connection === 'connected' && tool.server)
      .map(tool => tool.server as string)
    return [...new Set([...connected, ...capabilities.selected_servers])]
  }, [toolList, capabilities.selected_servers])
  const copy = SECTION_COPY[section]
  const view = getWorkspaceView(section)

  const SectionIcon = view.icon

  const latest = useRef({ workspacePath, capabilities, loaded, saving })
  latest.current = { workspacePath, capabilities, loaded, saving }
  const loadVersion = useRef(0)
  const saveVersion = useRef(0)
  const saveQueue = useRef<Promise<void>>(Promise.resolve())

  const load = useCallback(async (background = false) => {
    if (!workspacePath) {
      setError('This panel needs an active workflow folder.')
      setLoading(false)
      return
    }
    const version = ++loadVersion.current
    if (!background) setLoading(true)
    setError(null)
    try {
      const response = await workflowManifestApi.getWorkflowManifest(workspacePath)
      if (latest.current.workspacePath !== workspacePath || version !== loadVersion.current || latest.current.saving) return
      const next = { ...EMPTY_CAPABILITIES, ...response.manifest.capabilities }
      setCapabilities(background ? mergeRemoteCapabilities(latest.current.capabilities, latest.current.loaded, next) : next)
      setLoaded(next)
      setCdpPort(response.manifest.capabilities.cdp_ports?.[0] || 9222)
    } catch (cause) {
      if (latest.current.workspacePath === workspacePath && version === loadVersion.current) {
        setError(cause instanceof Error ? cause.message : 'Unable to load workflow capabilities')
      }
    } finally {
      if (latest.current.workspacePath === workspacePath && version === loadVersion.current) setLoading(false)
    }
  }, [workspacePath])

  const handleRefreshServers = useCallback(async () => {
    if (latest.current.saving) return
    setRefreshingServers(true)
    try {
      await Promise.all([refreshTools(), load(true)])
    } finally {
      setRefreshingServers(false)
    }
  }, [refreshTools, load])

  // Agent tools (and shell edits) can change this configuration while the panel
  // stays open. Refresh both sources, only for the visible MCP panel.
  useEffect(() => {
    if (section !== 'mcp' || !workspacePath) return
    let refreshing = false
    const refresh = async () => {
      if (document.hidden || refreshing || latest.current.saving) return
      refreshing = true
      try { await handleRefreshServers() } finally { refreshing = false }
    }
    const onFocus = () => { void refresh() }
    const timer = window.setInterval(onFocus, 15000)
    window.addEventListener('focus', onFocus)
    return () => {
      window.clearInterval(timer)
      window.removeEventListener('focus', onFocus)
      ++loadVersion.current
    }
  }, [section, workspacePath, handleRefreshServers])

  useEffect(() => {
    void load()
  }, [load])

  const checkCdpConnection = useCallback(async (port: number) => {
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

  const persist = useCallback((next: WorkflowCapabilities): Promise<void> => {
    if (!workspacePath) {
      setError('This panel needs an active workflow folder before it can save.')
      return Promise.resolve()
    }
    const targetPath = workspacePath
    const version = ++saveVersion.current
    ++loadVersion.current
    setSaving(true)
    setError(null)

    // Chat and settings consume the same manifest store. Update it immediately
    // so the composer never displays the previous provider while this save is
    // in flight. Requests are serialized below, making the server last-write-
    // wins in the same order as the user's selections.
    const manifestStore = useWorkflowManifestStore.getState()
    const current = manifestStore.getWorkflowByPath(targetPath)
    if (current) {
      manifestStore.replaceWorkflowManifest(targetPath, {
        ...current.manifest,
        capabilities: next,
      })
    }

    const save = async () => {
      try {
        const response = await workflowManifestApi.updateWorkflowManifest({
          workspace_path: targetPath,
          capabilities: next,
        })
        if (version !== saveVersion.current) return
        useWorkflowManifestStore.getState().replaceWorkflowManifest(targetPath, response.manifest)
        if (latest.current.workspacePath === targetPath) setLoaded(next)
      } catch (cause) {
        if (version !== saveVersion.current) return
        await useWorkflowManifestStore.getState().refreshWorkflows()
        if (latest.current.workspacePath === targetPath) {
          setError(cause instanceof Error ? cause.message : 'Unable to save workflow capabilities')
        }
      } finally {
        if (version === saveVersion.current && latest.current.workspacePath === targetPath) setSaving(false)
      }
    }

    const queued = saveQueue.current.then(save, save)
    saveQueue.current = queued
    return queued
  }, [workspacePath])

  const save = useCallback(() => persist(capabilities), [capabilities, persist])

  return (
    <section className={`flex h-full min-h-0 w-full max-w-none flex-col bg-background ${section === 'mcp' ? 'overflow-y-auto' : ''}`}>
      {section !== 'browser' && <header className="flex shrink-0 items-start gap-3 border-b px-4 py-3">
        <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
          <SectionIcon className="h-4 w-4" />
        </div>
        <div className="min-w-0 flex-1">
          <h2 className="text-sm font-semibold text-foreground">{copy.title}</h2>
          <p className="mt-0.5 text-xs text-muted-foreground">{copy.description}</p>
        </div>
        <div className="self-center">
          <WorkspaceViewActions
            workspacePath={workspacePath}
            message={getWorkspaceAskAIMessage(section)}
            onRefresh={section === 'mcp'
              ? handleRefreshServers
              : () => useWorkflowStore.getState().refreshWorkspaceView()}
            refreshing={section === 'mcp' && refreshingServers}
            refreshLabel={section === 'mcp' ? 'Refresh connected MCP servers' : `Refresh ${copy.title}`}
          />
        </div>
      </header>}

      <div className={`p-4 ${section === 'browser' ? 'min-h-0 flex-1 !p-0 flex flex-col overflow-hidden relative' : section === 'mcp' ? 'shrink-0' : view.managesOwnScroll ? 'min-h-0 flex-1 flex flex-col overflow-hidden' : 'min-h-0 flex-1 overflow-y-auto'}`}>
        {loading ? (
          <div className="flex items-center justify-center gap-2 py-12 text-sm text-muted-foreground">
            <LoaderCircle className="h-4 w-4 animate-spin" /> Loading workflow settings…
          </div>
        ) : (
          <>
            {error && <p className="mb-4 rounded-md border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive">{error}</p>}
            {section === 'playbooks' && (
              <PlaybooksPanel workspacePath={workspacePath} />
            )}
            {section === 'skills' && (
              <div className="flex min-h-0 flex-1 flex-col">
                <SkillsManagerPanel
                  compact
                  workspacePath={workspacePath}
                  selectedSkills={capabilities.selected_skills}
                  onToggleSkill={(folderName) => setCapabilities(current => ({
                    ...current,
                    selected_skills: current.selected_skills.includes(folderName)
                      ? current.selected_skills.filter(s => s !== folderName)
                      : [...current.selected_skills, folderName],
                  }))}
                />
              </div>
            )}
            {section === 'mcp' && (
              <div>
                <div>
                  <ToolSelectionSection
                    availableServers={availableServers}
                    selectedServers={capabilities.selected_servers}
                    selectedTools={capabilities.selected_tools}
                    onServerChange={(selected_servers) => {
                      const next = { ...capabilities, selected_servers }
                      setCapabilities(next)
                      void persist(next)
                    }}
                    onToolChange={(selected_tools) => {
                      const next = { ...capabilities, selected_tools }
                      setCapabilities(next)
                      void persist(next)
                    }}
                    agentMode="workflow"
                    hideHeader
                    showSelectedOnly
                    disabled={!canWriteWorkflow}
                  />
                </div>
                <div className="mt-3 border-t border-border pt-3">
                  <div className="text-sm font-medium text-muted-foreground">
                    Connect a new MCP server
                  </div>
                  <div className="mt-3">
                    <ConnectorsBrowser
                      compact
                      workspacePath={workspacePath}
                      manageOwnScroll={false}
                    />
                  </div>
                </div>
              </div>
            )}
            {section === 'secrets' && (
              // Only the workflow's own checklist lives here: automation secrets
              // (the folder-scoped box) and global secrets. Account-level "Your
              // Secrets" are a different store that workflow runs never read
              // (chat tools and bots use them), so the account-wide manager is
              // not mounted in this pane; it stays in the Secrets modal. The
              // pane scrolls as a whole and the list takes only its own height.
              <div>
                <SecretSelectionSection
                  selectedSecrets={capabilities.selected_secrets}
                  onSecretChange={(selected_secrets) => {
                    const next = { ...capabilities, selected_secrets }
                    setCapabilities(next)
                    void persist(next)
                  }}
                  selectedGlobalSecrets={capabilities.selected_global_secret_names}
                  onGlobalSecretChange={(selected_global_secret_names) => {
                    const next = { ...capabilities, selected_global_secret_names }
                    setCapabilities(next)
                    void persist(next)
                  }}
                  workflowPath={workspacePath || ''}
                />
              </div>
            )}
            {section === 'browser' && (
              <BrowserWorkspacePanel
                workspacePath={workspacePath}
                browserMode={capabilities.browser_mode as BrowserAutomationMode}
                onBrowserModeChange={(browser_mode) => setCapabilities(current => ({ ...current, browser_mode }))}
                cdpPort={cdpPort}
                onCdpPortChange={(port) => {
                  setCdpPort(port)
                  setCapabilities(current => ({ ...current, cdp_ports: [port] }))
                }}
                cdpConnected={cdpConnected}
                cdpError={cdpError}
                cdpChecking={cdpChecking}
                onCheckCdpConnection={checkCdpConnection}
                readOnly={!canWriteWorkflow}
                dirty={dirty}
                saving={saving}
                onSave={() => void save()}
                assistantControl={
                  <WorkspaceViewActions
                    workspacePath={workspacePath}
                    message={getWorkspaceAskAIMessage('browser')}
                    onRefresh={() => useWorkflowStore.getState().refreshWorkspaceView()}
                    refreshLabel="Refresh Browser"
                  />
                }
              />
            )}
            {section === 'llm' && (
              <WorkflowLLMConfigurationPanel
                workspacePath={workspacePath}
                llmConfig={capabilities.llm_config}
                onChange={(llm_config) => {
                  const next = { ...capabilities, llm_config }
                  setCapabilities(next)
                  void persist(next)
                }}
              />
            )}
            {/* Bots write straight to the shared connector config (routes
                already carry workflow_id), so nothing here goes through the
                manifest Save below. */}
            {section === 'email' && <WorkflowEmailPanel workspacePath={workspacePath} />}
            {section === 'bots' && <WorkflowBotsPanel workspacePath={workspacePath} />}
          </>
        )}
      </div>

      {/* PLAT-262: Save hidden for a read-only user — nothing in this panel
          can actually persist for that account, so hide the button that
          implies otherwise rather than let it fail after the fact. Also
          hidden for sections that don't save through the manifest at all. */}
      {!loading && canWriteWorkflow && copy.savesViaManifest && section !== 'browser' && (
        <footer className="flex shrink-0 items-center justify-end gap-3 border-t px-4 py-3">
          {dirty && <span className="text-xs text-muted-foreground">Unsaved changes</span>}
          <button
            type="button"
            onClick={() => void save()}
            disabled={saving || !dirty}
            className="inline-flex items-center gap-1.5 rounded-md bg-primary px-3 py-1.5 text-sm font-medium text-primary-foreground transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-60"
          >
            {saving ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
            Save
          </button>
        </footer>
      )}
    </section>
  )
}
