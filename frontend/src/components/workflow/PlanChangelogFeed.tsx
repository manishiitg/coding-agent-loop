import React, { useCallback, useEffect, useState } from 'react'
import { Loader2, GitCommitVertical, RefreshCw, Trash2 } from 'lucide-react'
import { agentApi } from '../../services/api'
import type { PlanChangelogEntry, PlanChangelogFieldChange } from '../../services/api-types'
import { WORKFLOW_LOG_REFRESH_EVENT } from './LogViewer'

interface PlanChangelogFeedProps {
  workspacePath: string
}

const formatRelativeTime = (dateStr?: string): string => {
  if (!dateStr) return ''
  const date = new Date(dateStr)
  if (Number.isNaN(date.getTime())) return ''
  const diffMs = Date.now() - date.getTime()
  const diffSec = Math.floor(diffMs / 1000)
  const diffMin = Math.floor(diffSec / 60)
  const diffHr = Math.floor(diffMin / 60)
  const diffDay = Math.floor(diffHr / 24)
  if (diffSec < 60) return 'just now'
  if (diffMin < 60) return `${diffMin}m ago`
  if (diffHr < 24) return `${diffHr}h ago`
  if (diffDay < 30) return `${diffDay}d ago`
  return date.toLocaleDateString()
}

// Render an old/new changelog value compactly. Values can be strings, numbers,
// booleans, null, or nested JSON — short ones inline, anything else stringified.
const renderValue = (value: unknown): string => {
  if (value === null || value === undefined) return '∅'
  if (typeof value === 'string') return value.trim() === '' ? '∅' : value
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  try {
    return JSON.stringify(value)
  } catch {
    return String(value)
  }
}

const FieldDiff: React.FC<{ change: PlanChangelogFieldChange }> = ({ change }) => (
  <div className="rounded border border-border bg-muted/30 px-2.5 py-1.5 text-xs">
    <div className="font-medium text-foreground">
      {change.field}
      {change.step_id && <span className="ml-1 font-normal text-muted-foreground">· {change.step_id}</span>}
    </div>
    <div className="mt-1 flex flex-wrap items-center gap-1.5 text-muted-foreground">
      <span className="rounded bg-destructive/10 px-1.5 py-0.5 text-destructive line-through decoration-destructive/40">
        {renderValue(change.old_value)}
      </span>
      <span aria-hidden>→</span>
      <span className="rounded bg-emerald-500/10 px-1.5 py-0.5 text-emerald-700 dark:text-emerald-300">
        {renderValue(change.new_value)}
      </span>
    </div>
  </div>
)

const ChangelogRow: React.FC<{ entry: PlanChangelogEntry }> = ({ entry }) => (
  <div className="relative pl-6">
    <span className="absolute left-0 top-1 text-muted-foreground">
      <GitCommitVertical className="h-4 w-4" />
    </span>
    <div className="flex flex-wrap items-center gap-2">
      <span className="rounded bg-primary/10 px-1.5 py-0.5 font-mono text-[11px] text-primary">{entry.tool}</span>
      {entry.step_ids && entry.step_ids.length > 0 && (
        <span className="text-xs text-muted-foreground">{entry.step_ids.join(', ')}</span>
      )}
      <span className="ml-auto text-[11px] text-muted-foreground" title={entry.timestamp}>
        {formatRelativeTime(entry.timestamp)}
      </span>
    </div>
    {entry.reason && <p className="mt-1 text-sm text-foreground">{entry.reason}</p>}
    {entry.changes && entry.changes.length > 0 && (
      <div className="mt-2 space-y-1.5">
        {entry.changes.map((change, i) => (
          <FieldDiff key={`${change.field}-${change.step_id}-${i}`} change={change} />
        ))}
      </div>
    )}
  </div>
)

// PlanChangelogFeed renders the granular "Plan edits" audit trail from
// planning/changelog/*.json: each plan-mod tool call with its mandatory reason
// and per-field old→new diffs, newest first. It is the structured complement to
// the authored improve.html narrative in the History view.
export function PlanChangelogFeed({ workspacePath }: PlanChangelogFeedProps) {
  const [entries, setEntries] = useState<PlanChangelogEntry[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    if (!workspacePath) return
    setLoading(true)
    setError(null)
    try {
      const res = await agentApi.getPlanChangelog(workspacePath)
      setEntries(res.entries)
      if (!res.success && res.error) setError(res.error)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [workspacePath])

  useEffect(() => { void load() }, [load])

  const [pruneDays, setPruneDays] = useState(30)
  const [pruning, setPruning] = useState(false)
  const prune = useCallback(async () => {
    if (!workspacePath || pruning) return
    setPruning(true)
    try {
      await agentApi.prunePlanChangelog(workspacePath, pruneDays)
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setPruning(false)
    }
  }, [workspacePath, pruneDays, pruning, load])

  // Refresh alongside the timeline when a run completes or the user hits the
  // pane refresh control.
  useEffect(() => {
    const onRefresh = () => { void load() }
    window.addEventListener(WORKFLOW_LOG_REFRESH_EVENT, onRefresh)
    return () => window.removeEventListener(WORKFLOW_LOG_REFRESH_EVENT, onRefresh)
  }, [load])

  if (loading && entries.length === 0) {
    return (
      <div className="flex h-full items-center justify-center gap-2 text-sm text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" /> Loading plan edits…
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex h-full items-center justify-center p-6">
        <div className="max-w-md rounded-md border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-800 dark:bg-red-900/20 dark:text-red-300">
          {error}
        </div>
      </div>
    )
  }

  if (entries.length === 0) {
    return (
      <div className="flex h-full items-center justify-center p-6 text-center">
        <div className="max-w-md text-sm text-muted-foreground">
          No plan edits recorded yet. Every change the builder makes to the plan is logged here
          with its reason and a field-level diff.
        </div>
      </div>
    )
  }

  return (
    <div className="h-full overflow-y-auto bg-muted/20 px-4 py-4 dark:bg-black/20">
      <div className="mx-auto max-w-[880px]">
        <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
          <span className="text-xs text-muted-foreground">{entries.length} plan edit{entries.length !== 1 ? 's' : ''}</span>
          <div className="flex items-center gap-1.5">
            <span className="text-[11px] text-muted-foreground">Consolidate: drop older than</span>
            <select
              value={pruneDays}
              onChange={(e) => setPruneDays(Number(e.target.value))}
              className="rounded-md border border-border bg-background px-1.5 py-1 text-xs text-foreground focus:outline-none focus:ring-1 focus:ring-primary"
              aria-label="Drop plan edits older than"
            >
              <option value={7}>7 days</option>
              <option value={30}>30 days</option>
              <option value={90}>90 days</option>
              <option value={180}>180 days</option>
            </select>
            <button
              onClick={prune}
              disabled={pruning}
              title={`Permanently delete plan-edit history older than ${pruneDays} days`}
              className="inline-flex items-center gap-1.5 rounded-md border border-border px-2 py-1 text-xs text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-50"
            >
              {pruning ? <Loader2 className="h-3 w-3 animate-spin" /> : <Trash2 className="h-3 w-3" />} Consolidate
            </button>
            <button
              onClick={load}
              disabled={loading}
              className="inline-flex items-center gap-1.5 rounded-md border border-border px-2 py-1 text-xs text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-50"
            >
              <RefreshCw className={`h-3 w-3 ${loading ? 'animate-spin' : ''}`} /> Refresh
            </button>
          </div>
        </div>
        <div className="space-y-4 border-l border-border pl-1">
          {entries.map((entry, i) => (
            <ChangelogRow key={`${entry.file}-${entry.timestamp}-${i}`} entry={entry} />
          ))}
        </div>
      </div>
    </div>
  )
}

export default PlanChangelogFeed
