import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { Activity, AlertTriangle, LayoutDashboard, Loader2, RefreshCw, Target } from 'lucide-react'
import { agentApi } from '../../services/api'

type HealthStatus = 'healthy' | 'bug' | 'critical' | 'idle'
type ProgressStatus = 'on-track' | 'at-risk' | 'off-goal' | 'idle'

interface CardData {
  status: string
  headline: string
  goal: string
  updated: string
}

interface WorkflowDashEntry {
  workspacePath: string
  label: string
  health: CardData | null
  progress: CardData | null
  failed: boolean
}

interface OrgDashboardProps {
  workflows: Array<{ workspacePath: string; label: string }>
}

const HEALTH_VALUES: ReadonlySet<string> = new Set(['healthy', 'bug', 'critical'])
const PROGRESS_VALUES: ReadonlySet<string> = new Set(['on-track', 'at-risk', 'off-goal'])

function parseCard(content: string | undefined | null): CardData | null {
  if (!content || !content.trim()) return null
  try {
    const doc = new DOMParser().parseFromString(content, 'text/html')
    const el = doc.querySelector('[data-status]') || doc.querySelector('article') || doc.body.firstElementChild
    if (!el) return null
    const status = (el.getAttribute('data-status') || '').trim()
    const goal = (el.getAttribute('data-goal') || '').trim()
    const updated = (el.getAttribute('data-updated') || '').trim()
    const headlineEl = doc.querySelector('[data-field="headline"]')
    const headline = (headlineEl?.textContent || '').trim()
    return { status, headline, goal, updated }
  } catch {
    return null
  }
}

function healthStatus(card: CardData | null): HealthStatus {
  if (card && HEALTH_VALUES.has(card.status)) return card.status as HealthStatus
  return 'idle'
}

function progressStatus(card: CardData | null): ProgressStatus {
  if (card && PROGRESS_VALUES.has(card.status)) return card.status as ProgressStatus
  return 'idle'
}

const PILL_BASE = 'inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-medium'

const HEALTH_PILL: Record<HealthStatus, { className: string; label: string }> = {
  healthy: { className: 'bg-emerald-500/15 text-emerald-600', label: 'Healthy' },
  bug: { className: 'bg-amber-500/15 text-amber-600', label: 'Bug' },
  critical: { className: 'bg-red-500/15 text-red-600', label: 'Critical' },
  idle: { className: 'bg-muted text-muted-foreground', label: 'No status' },
}

const PROGRESS_PILL: Record<ProgressStatus, { className: string; label: string }> = {
  'on-track': { className: 'bg-emerald-500/15 text-emerald-600', label: 'On track' },
  'at-risk': { className: 'bg-amber-500/15 text-amber-600', label: 'At risk' },
  'off-goal': { className: 'bg-red-500/15 text-red-600', label: 'Off goal' },
  idle: { className: 'bg-muted text-muted-foreground', label: 'No goal yet' },
}

function relativeTime(iso: string): string {
  if (!iso) return ''
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return ''
  const diffMs = Date.now() - then
  if (diffMs < 0) return 'just now'
  const mins = Math.floor(diffMs / 60000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `updated ${mins}m ago`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `updated ${hours}h ago`
  const days = Math.floor(hours / 24)
  return `updated ${days}d ago`
}

const Pill: React.FC<{ icon: string; className: string; label: string }> = ({ icon, className, label }) => (
  <span className={`${PILL_BASE} ${className}`}>
    <span aria-hidden>{icon}</span>
    {label}
  </span>
)

export const OrgDashboard: React.FC<OrgDashboardProps> = ({ workflows }) => {
  const [entries, setEntries] = useState<WorkflowDashEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const results = await Promise.all(
        workflows.map(async (wf): Promise<WorkflowDashEntry> => {
          try {
            const [health, progress] = await Promise.all([
              agentApi.getBuilderDoc(wf.workspacePath, 'card-health'),
              agentApi.getBuilderDoc(wf.workspacePath, 'card-progress'),
            ])
            return {
              workspacePath: wf.workspacePath,
              label: wf.label,
              health: health.success && health.exists ? parseCard(health.content) : null,
              progress: progress.success && progress.exists ? parseCard(progress.content) : null,
              failed: false,
            }
          } catch {
            return {
              workspacePath: wf.workspacePath,
              label: wf.label,
              health: null,
              progress: null,
              failed: true,
            }
          }
        })
      )
      setEntries(results)
      if (results.some(r => r.failed)) {
        setError('Some workflow status cards could not be loaded.')
      }
    } catch {
      setError('Could not load the org dashboard.')
    } finally {
      setLoading(false)
    }
  }, [workflows])

  useEffect(() => { void load() }, [load])

  const triage = useMemo(() => {
    let critical = 0, bug = 0, healthy = 0
    let offGoal = 0, atRisk = 0, onTrack = 0
    for (const e of entries) {
      const h = healthStatus(e.health)
      if (h === 'critical') critical++
      else if (h === 'bug') bug++
      else if (h === 'healthy') healthy++
      const p = progressStatus(e.progress)
      if (p === 'off-goal') offGoal++
      else if (p === 'at-risk') atRisk++
      else if (p === 'on-track') onTrack++
    }
    const needAttention = critical + bug + offGoal + atRisk
    return { critical, bug, healthy, offGoal, atRisk, onTrack, needAttention }
  }, [entries])

  const groups = useMemo(() => {
    const map = new Map<string, WorkflowDashEntry[]>()
    for (const e of entries) {
      const goal = e.progress?.goal?.trim() || 'Unassigned'
      const bucket = map.get(goal)
      if (bucket) bucket.push(e)
      else map.set(goal, [e])
    }
    // Sort groups: real goals first (alpha), Unassigned last.
    return Array.from(map.entries()).sort(([a], [b]) => {
      if (a === 'Unassigned') return 1
      if (b === 'Unassigned') return -1
      return a.localeCompare(b)
    })
  }, [entries])

  const hasAnyCard = useMemo(
    () => entries.some(e => e.health || e.progress),
    [entries]
  )

  const refreshButton = (
    <button
      type="button"
      onClick={() => void load()}
      disabled={loading}
      className="inline-flex items-center gap-1.5 rounded-lg border border-border bg-background/90 px-2.5 py-1.5 text-xs font-medium text-muted-foreground shadow-sm transition-colors hover:bg-muted hover:text-foreground disabled:opacity-50"
    >
      <RefreshCw className={`h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`} />
      Refresh
    </button>
  )

  // Loading
  if (loading && entries.length === 0) {
    return (
      <div className="flex h-full min-h-[320px] flex-col items-center justify-center gap-3 text-muted-foreground">
        <Loader2 className="h-6 w-6 animate-spin" />
        <p className="text-sm">Loading org dashboard…</p>
      </div>
    )
  }

  // No workflows at all
  if (workflows.length === 0) {
    return (
      <div className="flex h-full min-h-[320px] flex-col items-center justify-center gap-3 px-6 text-center text-muted-foreground">
        <LayoutDashboard className="h-10 w-10 text-muted-foreground/50" />
        <p className="text-sm font-medium text-foreground">No automations yet</p>
        <p className="max-w-sm text-xs">No automations yet — create a workflow to see it here.</p>
      </div>
    )
  }

  const header = (
    <div className="flex items-center justify-between gap-3 px-4 pt-4">
      <div className="flex items-center gap-2">
        <LayoutDashboard className="h-5 w-5 text-primary" />
        <h2 className="text-base font-semibold text-foreground">Org Dashboard</h2>
      </div>
      {refreshButton}
    </div>
  )

  // Workflows exist but no cards yet
  if (!hasAnyCard) {
    return (
      <div className="flex h-full flex-col">
        {header}
        <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 py-12 text-center text-muted-foreground">
          <Activity className="h-10 w-10 text-muted-foreground/50" />
          <p className="text-sm font-medium text-foreground">Org dashboard is warming up</p>
          <p className="max-w-md text-xs">
            Org dashboard is warming up — status cards appear here after your workflows run their
            Pulse (health) and Auto-improve (goal) loops.
          </p>
        </div>
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col">
      {header}

      {error && (
        <div className="mx-4 mt-3 flex items-center justify-between gap-3 rounded-xl border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs text-amber-600">
          <span className="inline-flex items-center gap-1.5">
            <AlertTriangle className="h-3.5 w-3.5" />
            {error}
          </span>
          {refreshButton}
        </div>
      )}

      {/* Triage bar */}
      <div className="mx-4 mt-3 flex flex-wrap items-center gap-2 rounded-2xl border border-border bg-card px-3 py-2.5 shadow-sm">
        <span className="text-xs font-semibold text-foreground">
          {triage.needAttention} need attention
        </span>
        <span className="text-muted-foreground">·</span>
        <span className="inline-flex items-center gap-2 text-xs text-muted-foreground">
          <span className="inline-flex items-center gap-1">🔴 {triage.critical}</span>
          <span className="inline-flex items-center gap-1">🟠 {triage.bug}</span>
          <span className="inline-flex items-center gap-1">🟢 {triage.healthy}</span>
        </span>
        <span className="text-muted-foreground">·</span>
        <span className="inline-flex items-center gap-2 text-xs text-muted-foreground">
          <Target className="h-3.5 w-3.5" />
          <span>off-goal {triage.offGoal}</span>
          <span>at-risk {triage.atRisk}</span>
          <span>on-track {triage.onTrack}</span>
        </span>
      </div>

      {/* Goal groups */}
      <div className="flex-1 space-y-5 overflow-auto px-4 py-4">
        {groups.map(([goal, items]) => (
          <section key={goal} className="space-y-2">
            <div className="flex items-center gap-2">
              <Target className="h-4 w-4 text-muted-foreground" />
              <h3 className="text-sm font-semibold text-foreground">{goal}</h3>
              <span className="text-xs text-muted-foreground">
                {items.length} workflow{items.length !== 1 ? 's' : ''}
              </span>
            </div>
            <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
              {items.map(entry => {
                const h = healthStatus(entry.health)
                const p = progressStatus(entry.progress)
                const updated = entry.health?.updated || entry.progress?.updated || ''
                const rel = relativeTime(updated)
                return (
                  <div
                    key={entry.workspacePath}
                    className="rounded-2xl border border-border bg-card p-3 shadow-sm"
                  >
                    <div className="flex items-start justify-between gap-2">
                      <h4 className="min-w-0 truncate text-sm font-semibold text-foreground" title={entry.label}>
                        {entry.label}
                      </h4>
                    </div>
                    <div className="mt-2 flex flex-wrap items-center gap-1.5">
                      <Pill icon="🩺" className={HEALTH_PILL[h].className} label={HEALTH_PILL[h].label} />
                      <Pill icon="🎯" className={PROGRESS_PILL[p].className} label={PROGRESS_PILL[p].label} />
                    </div>
                    <div className="mt-2 space-y-1 text-xs text-muted-foreground">
                      {entry.health?.headline && (
                        <p className="line-clamp-2">🩺 {entry.health.headline}</p>
                      )}
                      {entry.progress?.headline && (
                        <p className="line-clamp-2">🎯 {entry.progress.headline}</p>
                      )}
                      {!entry.health?.headline && !entry.progress?.headline && (
                        <p className="italic">No status reported yet.</p>
                      )}
                    </div>
                    {rel && <p className="mt-2 text-[10px] text-muted-foreground/80">{rel}</p>}
                  </div>
                )
              })}
            </div>
          </section>
        ))}
      </div>
    </div>
  )
}

export default OrgDashboard
