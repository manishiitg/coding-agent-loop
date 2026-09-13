import { useCallback, useEffect, useMemo, useState } from 'react'
import { AlertTriangle, BookMarked, CheckCircle2, Loader2, RefreshCw } from 'lucide-react'
import { playbooksApi } from '../../api/playbooks'
import { usePresetApplication } from '../../stores/useGlobalPresetStore'
import { useWorkflowStore } from '../../stores/useWorkflowStore'
import { openWorkflowPresetPage } from '../../utils/workflowSessionRestore'
import { isNewerPlaybookVersion, type PlaybookCatalogItem } from './playbookCatalog'
import { buildPlaybookCoverage, type PlaybookCoverageRow, type WorkflowPlaybookInstallation } from './playbookCoverage'

const stateCopy = {
  not_used: { label: 'Not used', className: 'border-border bg-muted text-muted-foreground' },
  active: { label: 'Active', className: 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300' },
  draft: { label: 'Setup needed', className: 'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300' },
  update_available: { label: 'Update available', className: 'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300' },
} as const

export default function PlaybookOperationsOverview() {
  const { workflowPresets } = usePresetApplication()
  const [catalog, setCatalog] = useState<PlaybookCatalogItem[]>([])
  const [workflows, setWorkflows] = useState<WorkflowPlaybookInstallation[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    setLoading(true); setError('')
    try {
      const nextCatalog = await playbooksApi.list()
      const candidates = workflowPresets.flatMap(preset => {
        const workspacePath = preset.selectedFolder?.filepath
        return workspacePath ? [{ preset, workspacePath }] : []
      })
      const results = await Promise.allSettled(candidates.map(async item => ({
        ...item,
        installed: await playbooksApi.listInstalled(item.workspacePath),
      })))
      const loaded = results.flatMap(result => result.status === 'fulfilled' ? [result.value] : [])
      setCatalog(nextCatalog)
      setWorkflows(loaded)
      const failed = results.length - loaded.length
      if (failed > 0) setError(`${failed} workflow${failed === 1 ? '' : 's'} could not be included.`)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Unable to load engineering operations coverage.')
    } finally {
      setLoading(false)
    }
  }, [workflowPresets])

  useEffect(() => { void load() }, [load])

  const coverage = useMemo(() => buildPlaybookCoverage(catalog, workflows), [catalog, workflows])
  const categories = useMemo(() => [...new Set(coverage.map(item => item.category))], [coverage])
  const used = coverage.filter(item => item.uses.length > 0).length
  const updates = coverage.filter(item => item.state === 'update_available').length
  const coveredWorkflows = new Set(coverage.flatMap(item => item.uses.map(use => use.workspacePath))).size

  const openWorkflowPlaybook = async (use: PlaybookCoverageRow['uses'][number]) => {
    await openWorkflowPresetPage(use.preset, { source: 'engineering-operations-playbooks' })
    useWorkflowStore.getState().openWorkspaceView('playbooks')
  }

  if (loading && catalog.length === 0) return <div className="flex h-full items-center justify-center gap-2 text-sm text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin" />Loading engineering operations…</div>

  return <div className="h-full overflow-y-auto p-4 sm:p-6">
    <div className="mx-auto max-w-6xl space-y-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div><h2 className="text-base font-semibold">Engineering Operations</h2><p className="mt-1 text-xs text-muted-foreground">Playbook coverage across your workflows: what is active, what needs setup or updates, and which capabilities are not used yet.</p></div>
        <button type="button" onClick={() => void load()} disabled={loading} className="rounded-md border border-border p-2 text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-50" aria-label="Refresh playbook coverage"><RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} /></button>
      </div>
      <div className="grid gap-3 sm:grid-cols-4">
        {[['Playbooks used', `${used} / ${coverage.length}`], ['Capabilities not used', String(coverage.length - used)], ['Updates available', String(updates)], ['Workflows covered', `${coveredWorkflows} / ${workflowPresets.filter(preset => preset.selectedFolder?.filepath).length}`]].map(([label, value]) => <div key={label} className="rounded-xl border bg-card p-4"><div className="text-xl font-semibold">{value}</div><div className="mt-1 text-xs text-muted-foreground">{label}</div></div>)}
      </div>
      {error && <div className="flex items-center gap-2 rounded-md border border-amber-500/30 bg-amber-500/10 p-3 text-xs text-amber-700 dark:text-amber-300"><AlertTriangle className="h-4 w-4" />{error}</div>}
      {categories.map(category => {
        const rows = coverage.filter(item => item.category === category)
        return <section key={category} className="overflow-hidden rounded-xl border bg-card">
          <div className="flex items-center gap-2 border-b bg-muted/20 px-4 py-3"><BookMarked className="h-4 w-4 text-primary" /><h3 className="text-sm font-semibold">{category}</h3><span className="ml-auto text-xs text-muted-foreground">{rows.filter(row => row.uses.length > 0).length} of {rows.length} used</span></div>
          <div className="divide-y">
            {rows.map(row => {
              const state = stateCopy[row.state]
              return <div key={row.id} className="grid gap-3 px-4 py-4 lg:grid-cols-[minmax(0,1.2fr)_minmax(0,1fr)]">
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2"><h4 className="text-sm font-medium">{row.title}</h4><span className={`rounded-full border px-2 py-0.5 text-[10px] font-medium ${state.className}`}>{state.label}</span><span className="text-[10px] text-muted-foreground">catalog v{row.version}</span></div>
                  <p className="mt-1 text-xs leading-5 text-muted-foreground">{row.description}</p>
                  {!!row.outputs?.length && <p className="mt-2 text-[11px] leading-5 text-muted-foreground"><span className="font-medium text-foreground/80">Delivers:</span> {row.outputs.slice(0, 3).join(' · ')}</p>}
                </div>
                <div className="space-y-2">
                  {row.uses.length === 0 ? <div className="rounded-md border border-dashed p-3 text-xs text-muted-foreground">No workflow currently uses this playbook.</div> : row.uses.map(use => {
                    const update = isNewerPlaybookVersion(row.version, use.installation.version)
                    return <button key={use.workspacePath} type="button" onClick={() => void openWorkflowPlaybook(use)} className="flex w-full items-center gap-3 rounded-md border bg-background px-3 py-2 text-left hover:border-primary/40 hover:bg-muted/20">
                      <CheckCircle2 className={`h-4 w-4 shrink-0 ${use.installation.status === 'ready' && !update ? 'text-emerald-600' : 'text-amber-500'}`} />
                      <span className="min-w-0 flex-1"><span className="block truncate text-xs font-medium">{use.preset.label || use.workspacePath}</span><span className="block text-[11px] text-muted-foreground">v{use.installation.version} · {update ? `update to v${row.version}` : use.installation.status}</span></span>
                    </button>
                  })}
                </div>
              </div>
            })}
          </div>
        </section>
      })}
    </div>
  </div>
}
