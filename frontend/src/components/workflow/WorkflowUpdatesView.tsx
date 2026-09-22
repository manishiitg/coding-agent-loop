import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { AlertCircle, CheckCircle2, Clock3, Loader2, PackageCheck } from 'lucide-react'
import { workflowManifestApi } from '../../services/api'
import type { WorkflowContractUpgradeItem, WorkflowContractUpgradeStatus } from '../../services/api-types'
import { useCanWriteWorkflow } from '../../hooks/useCanWriteWorkflow'
import { AskAIButton } from './AskAIButton'
import { WorkspaceViewHeader } from './WorkspaceViewHeader'
import { WorkspaceViewIconButton } from './WorkspaceViewIconButton'

const MANUAL_UPDATE_MESSAGE = 'Update this workflow to the current platform contract now. Use get_contract_upgrades, complete and verify each pending migration in order, and stamp each completed version before continuing to the next. Do not run the workflow as part of the migration. If a migration requires a genuine product, business, or safety choice, stop and ask me in this chat instead of guessing. When all migrations are complete, confirm the final workflow contract version.'

function formatUpgradeLabel(label: string): string {
  return label
    .replace(/^upgrade-/, '')
    .split('-')
    .map(word => word.charAt(0).toUpperCase() + word.slice(1))
    .join(' ')
}

function UpgradeRow({ item, state }: { item: WorkflowContractUpgradeItem; state: 'pending' | 'applied' }) {
  const pending = state === 'pending'
  const Icon = pending ? Clock3 : CheckCircle2
  return (
    <li className="flex items-start gap-3 px-4 py-3">
      <span className={`mt-0.5 flex h-6 w-6 shrink-0 items-center justify-center rounded-full ${pending ? 'bg-amber-500/10 text-amber-600 dark:text-amber-300' : 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-300'}`}>
        <Icon className="h-3.5 w-3.5" />
      </span>
      <div className="min-w-0 flex-1">
        <p className="text-sm font-medium text-foreground">{formatUpgradeLabel(item.label)}</p>
        <p className="mt-0.5 text-xs text-muted-foreground">Contract {item.target_version}</p>
      </div>
      <span className={`rounded-full border px-2 py-0.5 text-[11px] font-medium ${pending ? 'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300' : 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'}`}>
        {pending ? 'Pending' : 'Applied'}
      </span>
    </li>
  )
}

export default function WorkflowUpdatesView({
  workspacePath,
  headerAction,
}: {
  workspacePath: string | null
  headerAction?: ReactNode
}) {
  const canWrite = useCanWriteWorkflow(workspacePath)
  const [status, setStatus] = useState<WorkflowContractUpgradeStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    if (!workspacePath) return
    setLoading(true)
    setError(null)
    try {
      const response = await workflowManifestApi.getWorkflowManifest(workspacePath)
      setStatus(response.contract_upgrade ?? null)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not load workflow updates')
    } finally {
      setLoading(false)
    }
  }, [workspacePath])

  useEffect(() => { void load() }, [load])
  useEffect(() => {
    if (!status?.required) return
    const timer = window.setInterval(() => { void load() }, 10_000)
    return () => window.clearInterval(timer)
  }, [load, status?.required])

  const pending = status?.pending ?? []
  const applied = useMemo(() => [...(status?.applied ?? [])].reverse(), [status?.applied])
  const scopeName = workspacePath?.split('/').filter(Boolean).pop() || 'Workflow'
  const currentVersion = status?.current_version || '—'
  const platformVersion = status?.platform_version || '—'

  return (
    <div className="flex h-full min-h-0 w-full flex-col bg-background">
      <WorkspaceViewHeader
        icon={PackageCheck}
        title="Workflow updates"
        context={status && (
          <span className={`rounded-full border px-2 py-0.5 text-xs font-medium ${status.required ? 'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300' : 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'}`}>
            {status.required ? (pending.length > 0 ? `${pending.length} pending` : 'Needs attention') : 'Up to date'}
          </span>
        )}
        subtitle={`${scopeName} · v${currentVersion} · platform v${platformVersion}`}
        actions={<>
          {canWrite && status?.required && <AskAIButton workspacePath={workspacePath} label="Update workflow" message={MANUAL_UPDATE_MESSAGE} />}
          {headerAction}
          <WorkspaceViewIconButton label="Refresh workflow updates" onClick={() => { void load() }} spinning={loading} />
        </>}
      />

      {error && (
        <div className="flex items-center gap-2 bg-destructive/10 px-5 py-2 text-xs text-destructive">
          <AlertCircle className="h-3.5 w-3.5 shrink-0" />{error}
        </div>
      )}

      <div className="flex-1 overflow-y-auto px-4 py-4 sm:px-5">
        {loading && !status ? (
          <div className="flex items-center justify-center py-12"><Loader2 className="h-5 w-5 animate-spin text-muted-foreground" /></div>
        ) : status ? (
          <div className="space-y-5">
            <section>
              <div className="mb-2 flex items-baseline justify-between gap-3">
                <div>
                  <h3 className="text-sm font-semibold text-foreground">Pending</h3>
                  <p className="mt-0.5 text-xs text-muted-foreground">Started manually in chat. Schedules keep running the saved version and never apply updates.</p>
                </div>
                <span className="text-xs tabular-nums text-muted-foreground">{pending.length}</span>
              </div>
              <ul className="divide-y divide-border overflow-hidden rounded-md border border-border">
                {pending.length > 0
                  ? pending.map(item => <UpgradeRow key={`${item.label}:${item.target_version}`} item={item} state="pending" />)
                  : <li className="px-4 py-6 text-center text-sm text-muted-foreground">
                    {status.required ? 'This contract needs review, but this server has no known upgrade path.' : 'No pending updates.'}
                  </li>}
              </ul>
            </section>

            <section>
              <div className="mb-2 flex items-baseline justify-between gap-3">
                <div>
                  <h3 className="text-sm font-semibold text-foreground">Applied before</h3>
                  <p className="mt-0.5 text-xs text-muted-foreground">Confirmed by the workflow's current contract marker; older updates did not record timestamps.</p>
                </div>
                <span className="text-xs tabular-nums text-muted-foreground">{applied.length}</span>
              </div>
              <ul className="divide-y divide-border overflow-hidden rounded-md border border-border">
                {applied.length > 0
                  ? applied.map(item => <UpgradeRow key={`${item.label}:${item.target_version}`} item={item} state="applied" />)
                  : <li className="px-4 py-6 text-center text-sm text-muted-foreground">No earlier platform updates are recorded by this contract.</li>}
              </ul>
            </section>
          </div>
        ) : null}
      </div>
    </div>
  )
}
