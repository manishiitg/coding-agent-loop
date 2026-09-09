import WorkflowLiveBrowser from './WorkflowLiveBrowser'
import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { LoaderCircle, Save, Settings2, X } from 'lucide-react'
import { ToolSelectionSection } from '../ToolSelectionSection'
import SkillsManagerPanel from '../skills/SkillsManagerPanel'
import { SecretSelectionSection } from '../secrets/SecretSelectionSection'
import BrowserAutomationSettings, { type BrowserAutomationMode } from '../BrowserAutomationSettings'
import WorkflowLLMConfigurationPanel from './WorkflowLLMConfigurationPanel'
import WorkflowBotsPanel from './WorkflowBotsPanel'
import ConnectorsBrowser from '../connectors/ConnectorsBrowser'
import { agentApi, workflowManifestApi } from '../../services/api'
import type { WorkflowCapabilities } from '../../services/api-types'
import { useMCPStore } from '../../stores/useMCPStore'
import { useWorkflowManifestStore } from '../../stores/useWorkflowManifestStore'
import { useCanWriteWorkflow } from '../../hooks/useCanWriteWorkflow'
import { toggleServerSelection } from '../../utils/mcpServerAlias'
import { getWorkspaceView, type CapabilityViewId } from './workspaceViews'

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
  skills: {
    title: 'Workflow skills',
    description: 'Select reusable skills for this workflow’s builder context.',
    savesViaManifest: true,
  },
  mcp: {
    title: 'Workflow MCP',
    description: 'Select the MCP servers and tools this workflow may use.',
    savesViaManifest: true,
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
  bots: {
    title: 'Workflow bots',
    description: 'Slack channels and WhatsApp slugs this workflow answers on. Connections are shared by all workflows.',
    savesViaManifest: false,
  },
}

// Structural equality for the manifest capabilities: a flat object of
// primitives, string arrays, and one nested plain-JSON `llm_config`.
function capabilitiesEqual(a: unknown, b: unknown): boolean {
  if (a === b) return true
  if (typeof a !== typeof b || a === null || b === null || typeof a !== 'object') return false
  if (Array.isArray(a) !== Array.isArray(b)) return false
  if (Array.isArray(a) && Array.isArray(b)) {
    return a.length === b.length && a.every((item, index) => capabilitiesEqual(item, b[index]))
  }
  const left = a as Record<string, unknown>
  const right = b as Record<string, unknown>
  const keys = new Set([...Object.keys(left), ...Object.keys(right)])
  for (const key of keys) {
    if (!capabilitiesEqual(left[key], right[key])) return false
  }
  return true
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
  // Lets the "Connect a new MCP server" browser add/remove an already-connected
  // server from this workflow's selection directly, without needing the main
  // (now selected-only) list above -- same alias-safe logic ToolSelectionSection
  // itself uses for its own checkbox, via the shared toggleServerSelection util.
  const handleToggleServerForWorkflow = useCallback((serverName: string) => {
    setCapabilities(current => {
      const { servers, tools } = toggleServerSelection(serverName, current.selected_servers, current.selected_tools)
      return { ...current, selected_servers: servers, selected_tools: tools }
    })
  }, [])
  const copy = SECTION_COPY[section]
  const view = getWorkspaceView(section)
  const SectionIcon = view.icon

  const load = useCallback(async () => {
    if (!workspacePath) {
      setError('This panel needs an active workflow folder.')
      setLoading(false)
      return
    }
    setLoading(true)
    setError(null)
    try {
      const response = await workflowManifestApi.getWorkflowManifest(workspacePath)
      const next = { ...EMPTY_CAPABILITIES, ...response.manifest.capabilities }
      setCapabilities(next)
      setLoaded(next)
      setCdpPort(response.manifest.capabilities.cdp_ports?.[0] || 9222)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Unable to load workflow capabilities')
    } finally {
      setLoading(false)
    }
  }, [workspacePath])

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

  const persist = useCallback(async (next: WorkflowCapabilities) => {
    if (!workspacePath) {
      setError('This panel needs an active workflow folder before it can save.')
      return
    }
    setSaving(true)
    setError(null)
    try {
      await workflowManifestApi.updateWorkflowManifest({ workspace_path: workspacePath, capabilities: next })
      setLoaded(next)
      await useWorkflowManifestStore.getState().refreshWorkflows()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Unable to save workflow capabilities')
    } finally {
      setSaving(false)
    }
  }, [workspacePath])

  const [browserSettingsOpen, setBrowserSettingsOpen] = useState(false)

  const save = useCallback(() => persist(capabilities), [capabilities, persist])

  return (
    <section className="flex h-full min-h-0 w-full max-w-none flex-col bg-background">
      {section !== 'browser' && <header className="flex shrink-0 items-start gap-3 border-b px-4 py-3">
        <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
          <SectionIcon className="h-4 w-4" />
        </div>
        <div className="min-w-0 flex-1">
          <h2 className="text-sm font-semibold text-foreground">{copy.title}</h2>
          <p className="mt-0.5 text-xs text-muted-foreground">{copy.description}</p>
        </div>
      </header>}

      <div className={`min-h-0 flex-1 p-4 ${section === 'browser' ? '!p-0 flex flex-col overflow-hidden relative' : view.managesOwnScroll ? 'flex flex-col overflow-hidden' : 'overflow-y-auto'}`}>
        {loading ? (
          <div className="flex items-center justify-center gap-2 py-12 text-sm text-muted-foreground">
            <LoaderCircle className="h-4 w-4 animate-spin" /> Loading workflow settings…
          </div>
        ) : (
          <>
            {error && <p className="mb-4 rounded-md border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive">{error}</p>}
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
              <div className="flex min-h-0 flex-1 flex-col">
                <div className="shrink-0 overflow-y-auto">
                  <ToolSelectionSection
                    availableServers={availableServers}
                    selectedServers={capabilities.selected_servers}
                    selectedTools={capabilities.selected_tools}
                    onServerChange={(selected_servers) => setCapabilities(current => ({ ...current, selected_servers }))}
                    onToolChange={(selected_tools) => setCapabilities(current => ({ ...current, selected_tools }))}
                    agentMode="workflow"
                    hideHeader
                    showSelectedOnly
                    disabled={!canWriteWorkflow}
                  />
                </div>
                <div className="mt-3 flex min-h-0 flex-1 flex-col border-t border-border pt-3">
                  <div className="shrink-0 text-sm font-medium text-muted-foreground">
                    Connect a new MCP server
                  </div>
                  <div className="mt-3 min-h-0 flex-1">
                    <ConnectorsBrowser
                      compact
                      workspacePath={workspacePath}
                      selectedServers={capabilities.selected_servers}
                      onToggleServer={handleToggleServerForWorkflow}
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
              <>
                <WorkflowLiveBrowser workspacePath={workspacePath} toolbar={<button type="button" aria-label="Browser settings" title="Browser settings" onClick={() => setBrowserSettingsOpen(value => !value)} className="rounded p-1.5 text-muted-foreground hover:bg-muted"><Settings2 className="h-4 w-4" /></button>} />
                {browserSettingsOpen && <div role="dialog" aria-label="Browser settings" className="absolute inset-x-2 top-12 z-10 max-h-[calc(100%-4rem)] overflow-y-auto rounded-lg border border-border bg-background p-4 shadow-xl">
                  <div className="mb-3 flex items-center justify-between"><h3 className="text-sm font-medium">Browser settings</h3><button type="button" aria-label="Close browser settings" onClick={() => setBrowserSettingsOpen(false)} className="rounded p-1 hover:bg-muted"><X className="h-4 w-4" /></button></div>
                <BrowserAutomationSettings
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
                />
                {canWriteWorkflow && <div className="mt-3 flex items-center justify-end gap-3 border-t pt-3">{dirty && <span className="text-xs text-muted-foreground">Unsaved changes</span>}<button type="button" disabled={!dirty || saving} onClick={() => void save()} className="rounded bg-primary px-3 py-1.5 text-xs text-primary-foreground disabled:opacity-50">{saving ? 'Saving…' : 'Save settings'}</button></div>}
                </div>}
              </>
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
                onUseProvider={async (llm_config) => {
                  const next = { ...capabilities, llm_config }
                  setCapabilities(next)
                  await persist(next)
                }}
              />
            )}
            {/* Bots write straight to the shared connector config (routes
                already carry workflow_id), so nothing here goes through the
                manifest Save below. */}
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
