import { useEffect, useMemo, useState } from 'react'
import { ArrowLeft, BookMarked, CheckCircle2, ChevronDown, ChevronRight, CircleDot, FolderTree, Layers3, Loader2, PackageCheck, Search, Settings2, Wrench } from 'lucide-react'
import { PLAYBOOK_CATALOG, PLAYBOOK_CATEGORIES, type PlaybookCatalogItem } from './playbookCatalog'
import { playbooksApi } from '../../api/playbooks'
import type { InstalledPlaybook } from '../../services/api-types'
import { useCanWriteWorkflow, READ_ONLY_TITLE } from '../../hooks/useCanWriteWorkflow'
import { AskAIButton } from '../workflow/AskAIButton'
import { useWorkflowManifestStore } from '../../stores/useWorkflowManifestStore'

type PlaybooksPanelProps = {
  workspacePath: string | null
}

type Tab = 'installed' | 'catalog'

const tabClass = (active: boolean) => `rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${
  active ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'
}`

export default function PlaybooksPanel({ workspacePath }: PlaybooksPanelProps) {
  const [tab, setTab] = useState<Tab>('catalog')
  const [query, setQuery] = useState('')
  const [expandedCategories, setExpandedCategories] = useState<Set<string>>(() => new Set(PLAYBOOK_CATEGORIES))
  const [selected, setSelected] = useState<PlaybookCatalogItem | null>(null)
  const [catalog, setCatalog] = useState<readonly PlaybookCatalogItem[]>(PLAYBOOK_CATALOG)
  const [installed, setInstalled] = useState<InstalledPlaybook[]>([])
  const [loading, setLoading] = useState(Boolean(workspacePath))
  const [installing, setInstalling] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const canWrite = useCanWriteWorkflow(workspacePath)

  useEffect(() => {
    let active = true
    setLoading(Boolean(workspacePath))
    setError(null)
    const requests: [Promise<PlaybookCatalogItem[]>, Promise<InstalledPlaybook[]>] = [
      playbooksApi.list(),
      workspacePath ? playbooksApi.listInstalled(workspacePath) : Promise.resolve([]),
    ]
    void Promise.all(requests).then(([nextCatalog, nextInstalled]) => {
      if (!active) return
      if (nextCatalog.length > 0) setCatalog(nextCatalog)
      setInstalled(nextInstalled)
    }).catch(cause => {
      if (active) setError(cause instanceof Error ? cause.message : 'Unable to load playbooks')
    }).finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [workspacePath])

  const categories = useMemo(() => [...new Set(catalog.map(playbook => playbook.category))], [catalog])

  const visible = useMemo(() => {
    const needle = query.trim().toLowerCase()
    return catalog
      .filter(playbook => !needle || `${playbook.title} ${playbook.description} ${playbook.category} ${(playbook.setupInputs || []).map(input => input.label).join(' ')} ${(playbook.outputs || []).join(' ')}`.toLowerCase().includes(needle))
      .sort((a, b) => a.category.localeCompare(b.category) || a.order - b.order)
  }, [catalog, query])

  const visibleGroups = useMemo(() => categories
    .map(groupCategory => ({ category: groupCategory, playbooks: visible.filter(playbook => playbook.category === groupCategory) }))
    .filter(group => group.playbooks.length > 0), [categories, visible])

  const toggleCategory = (groupCategory: string) => {
    setExpandedCategories(current => {
      const next = new Set(current)
      if (next.has(groupCategory)) next.delete(groupCategory)
      else next.add(groupCategory)
      return next
    })
  }

  const installedSelection = selected ? installed.find(item => item.id === selected.id) : undefined
  const installSelected = async () => {
    if (!workspacePath || !selected || !canWrite) return
    setInstalling(true)
    setError(null)
    try {
      const result = await playbooksApi.install(workspacePath, selected.id)
      setInstalled(current => [...current.filter(item => item.id !== result.id), result])
      await useWorkflowManifestStore.getState().refreshWorkflows()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Unable to install playbook')
    } finally {
      setInstalling(false)
    }
  }

  if (selected) {
    const setupInputs = selected.setupInputs || []
    const requiredCapabilities = selected.requiredCapabilities || []
    const recommendedTools = selected.recommendedTools || []
    const outputs = selected.outputs || []
    return (
      <div className="min-h-0 flex-1 overflow-y-auto">
        <button type="button" onClick={() => setSelected(null)} className="mb-4 inline-flex items-center gap-1.5 text-xs font-medium text-muted-foreground hover:text-foreground">
          <ArrowLeft className="h-3.5 w-3.5" /> Back to catalog
        </button>
        <div className="rounded-xl border border-border bg-card p-5">
          <div className="flex items-start gap-3">
            <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary"><BookMarked className="h-5 w-5" /></div>
            <div className="min-w-0 flex-1">
              <div className="text-xs font-medium text-primary">AgentWorks / Agentic Engineering Platform / {selected.category}</div>
              <h3 className="mt-1 text-lg font-semibold text-foreground">{selected.title}</h3>
              <p className="mt-2 text-sm leading-6 text-muted-foreground">{selected.description}</p>
            </div>
            <span className="rounded-full border border-border px-2 py-1 text-[11px] text-muted-foreground">v{selected.version}</span>
          </div>
          <div className="mt-5 grid gap-3 sm:grid-cols-2">
            <div className="rounded-lg border border-border bg-muted/20 p-3"><div className="flex items-center gap-2 text-sm font-medium"><Settings2 className="h-4 w-4 text-muted-foreground" /> Setup inputs</div><p className="mt-1 text-xs text-muted-foreground">{selected.inputCount} inputs guide adaptation to the company and workflow.</p></div>
            <div className="rounded-lg border border-border bg-muted/20 p-3"><div className="flex items-center gap-2 text-sm font-medium"><Wrench className="h-4 w-4 text-muted-foreground" /> Recommended tools</div><p className="mt-1 text-xs text-muted-foreground">{selected.toolCount} built-in, CLI, MCP, or skill recommendations.</p></div>
          </div>
          {(setupInputs.length > 0 || requiredCapabilities.length > 0) && (
            <section className="mt-5 rounded-lg border border-border p-4">
              <div className="flex items-center gap-2 text-sm font-semibold text-foreground"><Layers3 className="h-4 w-4 text-primary" /> What this playbook covers</div>
              {setupInputs.length > 0 && (
                <div className="mt-3">
                  <div className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">Setup areas</div>
                  <div className="mt-2 grid gap-2 sm:grid-cols-2">
                    {setupInputs.map(input => (
                      <div key={input.id} className="flex items-start gap-2 text-xs leading-5 text-foreground/90">
                        <CircleDot className="mt-1 h-3 w-3 shrink-0 text-primary/70" />
                        <span>{input.label}{!input.required && <span className="ml-1 text-muted-foreground">(optional)</span>}</span>
                      </div>
                    ))}
                  </div>
                </div>
              )}
              {requiredCapabilities.length > 0 && (
                <div className="mt-4">
                  <div className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">Capabilities configured</div>
                  <div className="mt-2 flex flex-wrap gap-1.5">
                    {requiredCapabilities.map(capability => <span key={capability} className="rounded-full border border-border bg-muted/30 px-2 py-1 text-[11px] text-muted-foreground">{capability.replaceAll('_', ' ')}</span>)}
                  </div>
                </div>
              )}
            </section>
          )}
          {outputs.length > 0 && (
            <section className="mt-4 rounded-lg border border-border p-4">
              <div className="flex items-center gap-2 text-sm font-semibold text-foreground"><PackageCheck className="h-4 w-4 text-primary" /> What you’ll get</div>
              <div className="mt-3 grid gap-2 sm:grid-cols-2">
                {outputs.map(output => <div key={output} className="flex items-start gap-2 text-xs leading-5 text-foreground/90"><CheckCircle2 className="mt-0.5 h-3.5 w-3.5 shrink-0 text-emerald-600" /><span>{output}</span></div>)}
              </div>
            </section>
          )}
          {recommendedTools.length > 0 && (
            <section className="mt-4 rounded-lg border border-border p-4">
              <div className="flex items-center gap-2 text-sm font-semibold text-foreground"><Wrench className="h-4 w-4 text-primary" /> Recommended tools</div>
              <div className="mt-3 space-y-2">
                {recommendedTools.map(tool => (
                  <div key={tool.id} className="rounded-md bg-muted/30 px-3 py-2">
                    <div className="flex flex-wrap items-center gap-2"><span className="text-xs font-medium text-foreground">{tool.name}</span><span className="rounded border border-border px-1.5 py-0.5 text-[10px] uppercase text-muted-foreground">{tool.type}</span></div>
                    <p className="mt-1 text-xs leading-5 text-muted-foreground">{tool.purpose}</p>
                  </div>
                ))}
              </div>
            </section>
          )}
          {error && <p className="mt-4 rounded-md border border-destructive/30 bg-destructive/10 p-3 text-xs text-destructive">{error}</p>}
          <div className="mt-5 rounded-lg border border-dashed border-border p-4">
            <div className="flex items-center gap-2 text-sm font-medium"><CheckCircle2 className="h-4 w-4 text-muted-foreground" /> Setup with Builder</div>
            <p className="mt-1 text-xs leading-5 text-muted-foreground">Installation copies this guide into the workflow, attaches it to Builder chat, and creates a workflow-specific setup record.</p>
            {installedSelection ? (
              <div className="mt-3 flex flex-wrap items-center gap-2">
                <span className="inline-flex items-center gap-1.5 rounded-md border border-emerald-500/30 bg-emerald-500/10 px-2.5 py-2 text-xs font-medium text-emerald-700 dark:text-emerald-300"><CheckCircle2 className="h-3.5 w-3.5" /> Installed · {installedSelection.status}</span>
                <AskAIButton workspacePath={workspacePath} label="Continue setup in Builder" message={`Read the installed skill ${installedSelection.skill_name} with read_skill, then follow it to configure this workflow. ${selected.setupPrompt || 'Inspect the existing workflow first and ask only for required inputs that are missing.'}`} className="flex items-center gap-1.5 rounded-md bg-primary px-3 py-2 text-xs font-medium text-primary-foreground hover:bg-primary/90" />
              </div>
            ) : (
              <button type="button" disabled={!workspacePath || !canWrite || installing} title={!canWrite ? READ_ONLY_TITLE : undefined} onClick={() => void installSelected()} className="mt-3 inline-flex items-center gap-1.5 rounded-md bg-primary px-3 py-2 text-xs font-medium text-primary-foreground disabled:cursor-not-allowed disabled:opacity-50">{installing && <Loader2 className="h-3.5 w-3.5 animate-spin" />}{installing ? 'Installing…' : 'Use playbook'}</button>
            )}
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-center gap-1 rounded-lg bg-muted/60 p-1" role="tablist" aria-label="Workflow playbooks">
        <button type="button" role="tab" aria-selected={tab === 'installed'} className={tabClass(tab === 'installed')} onClick={() => setTab('installed')}>Installed <span className="ml-1 text-muted-foreground">{installed.length}</span></button>
        <button type="button" role="tab" aria-selected={tab === 'catalog'} className={tabClass(tab === 'catalog')} onClick={() => setTab('catalog')}>Catalog <span className="ml-1 text-muted-foreground">{catalog.length}</span></button>
      </div>

      {loading ? <div className="flex flex-1 items-center justify-center gap-2 text-sm text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin" /> Loading playbooks…</div> : tab === 'installed' ? (
        installed.length > 0 ? <div className="mt-3 min-h-0 flex-1 space-y-2 overflow-y-auto">{installed.map(item => <button key={item.id} type="button" onClick={() => { const playbook = catalog.find(value => value.id === item.id); if (playbook) setSelected(playbook) }} className="flex w-full items-center gap-3 rounded-lg border border-border bg-card p-3 text-left hover:border-primary/40"><div className="flex h-8 w-8 items-center justify-center rounded-md bg-emerald-500/10 text-emerald-600"><CheckCircle2 className="h-4 w-4" /></div><div className="min-w-0 flex-1"><div className="truncate text-sm font-medium">{item.title}</div><div className="mt-0.5 text-xs text-muted-foreground">{item.category} · v{item.version} · {item.status}</div></div><ChevronRight className="h-4 w-4 text-muted-foreground" /></button>)}</div> : (
        <div className="flex min-h-0 flex-1 flex-col items-center justify-center px-6 text-center">
          <div className="flex h-11 w-11 items-center justify-center rounded-xl bg-muted text-muted-foreground"><BookMarked className="h-5 w-5" /></div>
          <h3 className="mt-3 text-sm font-semibold text-foreground">No playbooks installed</h3>
          <p className="mt-1 max-w-sm text-xs leading-5 text-muted-foreground">Choose a playbook from the catalog to guide this workflow’s plan, capabilities, and reporting.</p>
          <button type="button" onClick={() => setTab('catalog')} className="mt-4 rounded-md bg-primary px-3 py-2 text-xs font-medium text-primary-foreground">Browse catalog</button>
        </div>)
      ) : (
        <>
          <div className="mt-3 shrink-0 space-y-3">
            <div className="relative">
              <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
              <input value={query} onChange={event => setQuery(event.target.value)} placeholder="Search playbooks" aria-label="Search playbooks" className="w-full rounded-lg border border-border bg-background py-2 pl-9 pr-3 text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary" />
            </div>
            <div className="rounded-lg border border-border bg-muted/20 px-3 py-2.5" aria-label="Playbook catalog hierarchy">
              <div className="flex items-center gap-2 text-xs font-medium text-muted-foreground"><FolderTree className="h-4 w-4" /> AgentWorks</div>
              <div className="ml-2 mt-2 border-l border-border pl-4 text-xs font-semibold text-foreground">Agentic Engineering Platform <span className="ml-1 font-normal text-muted-foreground">{visible.length} playbooks</span></div>
            </div>
          </div>
          <div className="mt-3 min-h-0 flex-1 overflow-y-auto">
            {visible.length === 0 ? <p className="py-10 text-center text-sm text-muted-foreground">No playbooks match this search.</p> : (
              <div className="space-y-3">
                {visibleGroups.map(group => {
                  const expanded = query.trim() !== '' || expandedCategories.has(group.category)
                  return (
                    <section key={group.category} className="rounded-lg border border-border bg-card/50">
                      <button type="button" aria-expanded={expanded} onClick={() => toggleCategory(group.category)} className="flex w-full items-center gap-2 px-3 py-2.5 text-left hover:bg-muted/30">
                        {expanded ? <ChevronDown className="h-4 w-4 text-muted-foreground" /> : <ChevronRight className="h-4 w-4 text-muted-foreground" />}
                        <span className="flex-1 text-sm font-semibold text-foreground">{group.category}</span>
                        <span className="rounded-full bg-muted px-2 py-0.5 text-[10px] font-medium text-muted-foreground">{group.playbooks.length}</span>
                      </button>
                      {expanded && (
                        <div className="ml-5 space-y-2 border-l border-border px-3 pb-3">
                          {group.playbooks.map(playbook => (
                            <button key={playbook.id} type="button" onClick={() => setSelected(playbook)} className="group flex w-full items-start gap-3 rounded-lg border border-border bg-card p-3 text-left transition-colors hover:border-primary/40 hover:bg-muted/20">
                              <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground group-hover:text-primary"><BookMarked className="h-4 w-4" /></div>
                              <div className="min-w-0 flex-1"><div className="flex items-center gap-2"><span className="truncate text-sm font-medium text-foreground">{playbook.title}</span><span className="shrink-0 text-[10px] text-muted-foreground">v{playbook.version}</span></div><p className="mt-1 line-clamp-2 text-xs leading-5 text-muted-foreground">{playbook.description}</p></div>
                              <ChevronRight className="mt-2 h-4 w-4 shrink-0 text-muted-foreground" />
                            </button>
                          ))}
                        </div>
                      )}
                    </section>
                  )
                })}
              </div>
            )}
          </div>
        </>
      )}
      {!workspacePath && <p className="mt-3 text-xs text-destructive">Open a workflow folder to use playbooks.</p>}
    </div>
  )
}
