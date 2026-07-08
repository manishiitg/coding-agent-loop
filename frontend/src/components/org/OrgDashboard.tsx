import React, { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Activity,
  AlertTriangle,
  Bug,
  CheckCircle2,
  CircleAlert,
  CircleHelp,
  DollarSign,
  LayoutDashboard,
  Loader2,
  RefreshCw,
  Target,
  X,
} from 'lucide-react'
import { agentApi } from '../../services/api'
import ModalPortal from '../ui/ModalPortal'

type HealthStatus = 'healthy' | 'bug' | 'critical' | 'idle'
type ProgressStatus = 'on-track' | 'at-risk' | 'off-goal' | 'idle'
type CostStatus = 'normal' | 'elevated' | 'missing' | 'idle'

interface CardData {
  status: string
  headline: string
  goal: string
  updated: string
  title: string
  metric: string
  detail: string
  fields: Record<string, string>
}

interface WorkflowDashEntry {
  workspacePath: string
  label: string
  health: CardData | null
  progress: CardData | null
  cost: CardData | null
  failed: boolean
}

interface OrgDashboardProps {
  workflows: Array<{ workspacePath: string; label: string }>
}

const HEALTH_VALUES: ReadonlySet<string> = new Set(['healthy', 'bug', 'critical'])
const PROGRESS_VALUES: ReadonlySet<string> = new Set(['on-track', 'at-risk', 'off-goal'])
const COST_VALUES: ReadonlySet<string> = new Set(['normal', 'elevated', 'missing'])

function parseCard(content: string | undefined | null): CardData | null {
  if (!content || !content.trim()) return null
  try {
    const doc = new DOMParser().parseFromString(content, 'text/html')
    const el = doc.querySelector('[data-status]') || doc.querySelector('article') || doc.body.firstElementChild
    if (!el) return null
    const status = (el.getAttribute('data-status') || '').trim()
    const goal = (el.getAttribute('data-goal') || '').trim()
    const updated = (el.getAttribute('data-updated') || '').trim()
    const title = (doc.querySelector('h1,h2,h3,h4')?.textContent || '').trim()
    const fields: Record<string, string> = {}
    doc.querySelectorAll('[data-field]').forEach((node) => {
      const key = node.getAttribute('data-field')?.trim()
      const value = (node.textContent || '').trim()
      if (key && value && !fields[key]) fields[key] = value
    })
    const headline = fields.headline || ''
    const metric = fields.metric || ''
    const detail = fields.detail || ''
    return { status, headline, goal, updated, title, metric, detail, fields }
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

function costStatus(card: CardData | null): CostStatus {
  if (card && COST_VALUES.has(card.status)) return card.status as CostStatus
  return 'idle'
}

function openInputCount(card: CardData | null): number {
  const value = card?.fields.input?.trim()
  if (!value) return 0
  if (/^(0\b|none\b|no\b|no open|no input|no questions)/i.test(value)) return 0
  const match = value.match(/\b(\d+)\b/)
  if (match) {
    const count = Number.parseInt(match[1], 10)
    return count > 0 ? count : 0
  }
  return 1
}

function latestUpdated(...cards: Array<CardData | null>): string {
  let latest = ''
  let latestMs = Number.NEGATIVE_INFINITY
  for (const card of cards) {
    const updated = card?.updated?.trim()
    if (!updated) continue
    const ms = new Date(updated).getTime()
    if (Number.isNaN(ms)) {
      if (!latest) latest = updated
      continue
    }
    if (ms > latestMs) {
      latestMs = ms
      latest = updated
    }
  }
  return latest
}

const PILL_BASE = 'font-runloop-mono inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[10px] font-semibold uppercase tracking-[0.06em]'

const HEALTH_PILL: Record<HealthStatus, { className: string; label: string; Icon: React.ComponentType<{ className?: string }> }> = {
  healthy: { className: 'border-emerald-500/25 bg-emerald-500/10 text-emerald-600 dark:text-emerald-300', label: 'Healthy', Icon: CheckCircle2 },
  bug: { className: 'border-amber-500/25 bg-amber-500/10 text-amber-600 dark:text-amber-300', label: 'Bug', Icon: Bug },
  critical: { className: 'border-red-500/25 bg-red-500/10 text-red-600 dark:text-red-300', label: 'Critical', Icon: CircleAlert },
  idle: { className: 'border-border bg-muted/70 text-muted-foreground', label: 'No status', Icon: CircleHelp },
}

const PROGRESS_PILL: Record<ProgressStatus, { className: string; label: string; Icon: React.ComponentType<{ className?: string }> }> = {
  'on-track': { className: 'border-emerald-500/25 bg-emerald-500/10 text-emerald-600 dark:text-emerald-300', label: 'On track', Icon: CheckCircle2 },
  'at-risk': { className: 'border-amber-500/25 bg-amber-500/10 text-amber-600 dark:text-amber-300', label: 'At risk', Icon: AlertTriangle },
  'off-goal': { className: 'border-red-500/25 bg-red-500/10 text-red-600 dark:text-red-300', label: 'Off goal', Icon: CircleAlert },
  idle: { className: 'border-border bg-muted/70 text-muted-foreground', label: 'Not assessed', Icon: Target },
}

const COST_PILL: Record<CostStatus, { className: string; label: string; Icon: React.ComponentType<{ className?: string }> }> = {
  normal: { className: 'border-sky-500/25 bg-sky-500/10 text-sky-600 dark:text-sky-300', label: 'Cost ok', Icon: DollarSign },
  elevated: { className: 'border-amber-500/25 bg-amber-500/10 text-amber-600 dark:text-amber-300', label: 'Cost watch', Icon: AlertTriangle },
  missing: { className: 'border-red-500/25 bg-red-500/10 text-red-600 dark:text-red-300', label: 'Cost missing', Icon: CircleAlert },
  idle: { className: 'border-border bg-muted/70 text-muted-foreground', label: 'No cost', Icon: DollarSign },
}

const HEALTH_DETAIL_FIELDS: Array<readonly [key: string, label: string]> = [
  ['state', 'State'],
  ['input', 'Needs input'],
  ['fix', 'Fix'],
  ['harden', 'Harden'],
  ['artifact', 'Artifact review'],
  ['backup', 'Backup'],
  ['publish', 'Publish'],
  ['cost', 'Cost/time'],
  ['evidence', 'Evidence'],
  ['next', 'Next'],
]

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

function absoluteTime(iso: string): string {
  if (!iso) return ''
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  return date.toLocaleString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

const Pill: React.FC<{ Icon: React.ComponentType<{ className?: string }>; className: string; label: string }> = ({ Icon, className, label }) => (
  <span className={`${PILL_BASE} ${className}`}>
    <Icon className="h-3 w-3" aria-hidden />
    {label}
  </span>
)

const TriageMetric: React.FC<{
  label: string
  value: number
  Icon: React.ComponentType<{ className?: string }>
  className: string
}> = ({ label, value, Icon, className }) => (
  <span className={`font-runloop-mono inline-flex items-center gap-1.5 rounded-md border px-2 py-1 text-[11px] font-semibold uppercase tracking-[0.06em] ${className}`}>
    <Icon className="h-3.5 w-3.5" />
    <span>{label}</span>
    <span className="text-foreground">{value}</span>
  </span>
)

const DetailRow: React.FC<{ label: string; value?: string }> = ({ label, value }) => {
  if (!value) return null
  return (
    <div className="grid gap-1">
      <div className="font-runloop-mono text-[10px] font-semibold uppercase tracking-[0.08em] text-muted-foreground/70">
        {label}
      </div>
      <div className="text-sm leading-6 text-foreground">{value}</div>
    </div>
  )
}

const StatusDetail: React.FC<{
  title: string
  card: CardData | null
  status: HealthStatus | ProgressStatus | CostStatus
  pill: { className: string; label: string; Icon: React.ComponentType<{ className?: string }> }
  extraFields?: Array<readonly [key: string, label: string]>
}> = ({ title, card, status, pill, extraFields }) => (
  <section className="rounded-lg border border-border bg-card/95 p-3">
    <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
      <div className="flex items-center gap-2">
        <h3 className="text-sm font-semibold tracking-tight text-foreground">{title}</h3>
        <Pill Icon={pill.Icon} className={pill.className} label={pill.label} />
      </div>
      <span className="font-runloop-mono text-[10px] uppercase tracking-[0.08em] text-muted-foreground">
        {status}
      </span>
    </div>
    {card ? (
      <div className="space-y-4">
        <DetailRow label="Headline" value={card.headline} />
        <DetailRow label="Metric" value={card.metric} />
        <DetailRow label="Detail" value={card.detail} />
        {extraFields?.map(([key, label]) => (
          <DetailRow key={key} label={label} value={card.fields[key]} />
        ))}
        <DetailRow label="Goal" value={card.goal} />
        <DetailRow label="Updated" value={absoluteTime(card.updated)} />
      </div>
    ) : (
      <p className="text-sm text-muted-foreground">No {title.toLowerCase()} card has been reported yet.</p>
    )}
  </section>
)

const WorkflowDetailModal: React.FC<{
  entry: WorkflowDashEntry
  onClose: () => void
}> = ({ entry, onClose }) => {
  const h = healthStatus(entry.health)
  const p = progressStatus(entry.progress)
  const c = costStatus(entry.cost)
  const updated = latestUpdated(entry.health, entry.progress, entry.cost)
  const goalLabel = entry.health?.goal?.trim() || entry.progress?.goal?.trim() || entry.cost?.goal?.trim() || ''

  return (
    <ModalPortal>
      <div
        className="fixed inset-0 z-[10000] flex items-center justify-center bg-black/55 p-4"
        onClick={(event) => {
          if (event.target === event.currentTarget) onClose()
        }}
      >
        <div
          role="dialog"
          aria-modal="true"
          aria-label={`${entry.label} details`}
          className="flex max-h-[85vh] w-full max-w-2xl flex-col overflow-hidden rounded-lg border border-border bg-background shadow-2xl"
        >
          <div className="flex items-start justify-between gap-3 border-b border-border bg-card/95 px-4 py-3">
            <div className="min-w-0">
              <div className="font-runloop-mono text-[10px] font-semibold uppercase tracking-[0.1em] text-muted-foreground">
                Workflow status
              </div>
              <h2 className="mt-1 truncate text-lg font-semibold tracking-tight text-foreground">{entry.label}</h2>
              {goalLabel && (
                <p className="mt-1 flex items-center gap-1.5 truncate text-xs text-muted-foreground">
                  <Target className="h-3.5 w-3.5 shrink-0" />
                  {goalLabel}
                </p>
              )}
            </div>
            <button
              type="button"
              onClick={onClose}
              aria-label="Close details"
              className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
            >
              <X className="h-4 w-4" />
            </button>
          </div>

          <div className="min-h-0 flex-1 overflow-auto p-4">
            <div className="mb-4 grid gap-3 rounded-lg border border-border bg-card/60 p-3 sm:grid-cols-2">
              <DetailRow label="Workspace path" value={entry.workspacePath} />
              <DetailRow label="Latest update" value={updated ? `${absoluteTime(updated)} (${relativeTime(updated).replace(/^updated /, '')})` : ''} />
            </div>
            <div className="grid gap-3">
              <StatusDetail title="Health" card={entry.health} status={h} pill={HEALTH_PILL[h]} extraFields={HEALTH_DETAIL_FIELDS} />
              <StatusDetail title="Goal alignment" card={entry.progress} status={p} pill={PROGRESS_PILL[p]} />
              <StatusDetail title="Cost and time" card={entry.cost} status={c} pill={COST_PILL[c]} />
            </div>
          </div>
        </div>
      </div>
    </ModalPortal>
  )
}

export const OrgDashboard: React.FC<OrgDashboardProps> = ({ workflows }) => {
  const [entries, setEntries] = useState<WorkflowDashEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [selectedEntry, setSelectedEntry] = useState<WorkflowDashEntry | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const results = await Promise.all(
        workflows.map(async (wf): Promise<WorkflowDashEntry> => {
          try {
            const [health, progress, cost] = await Promise.all([
              agentApi.getBuilderDoc(wf.workspacePath, 'card-health'),
              agentApi.getBuilderDoc(wf.workspacePath, 'card-progress'),
              agentApi.getBuilderDoc(wf.workspacePath, 'card-cost'),
            ])
            return {
              workspacePath: wf.workspacePath,
              label: wf.label,
              health: health.success && health.exists ? parseCard(health.content) : null,
              progress: progress.success && progress.exists ? parseCard(progress.content) : null,
              cost: cost.success && cost.exists ? parseCard(cost.content) : null,
              failed: false,
            }
          } catch {
            return {
              workspacePath: wf.workspacePath,
              label: wf.label,
              health: null,
              progress: null,
              cost: null,
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

  useEffect(() => {
    if (!selectedEntry) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setSelectedEntry(null)
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [selectedEntry])

  const triage = useMemo(() => {
    let critical = 0, bug = 0, healthy = 0
    let offGoal = 0, atRisk = 0, onTrack = 0
    let costElevated = 0, costMissing = 0, costNormal = 0
    let inputOpen = 0
    for (const e of entries) {
      const h = healthStatus(e.health)
      if (h === 'critical') critical++
      else if (h === 'bug') bug++
      else if (h === 'healthy') healthy++
      const p = progressStatus(e.progress)
      if (p === 'off-goal') offGoal++
      else if (p === 'at-risk') atRisk++
      else if (p === 'on-track') onTrack++
      const c = costStatus(e.cost)
      if (c === 'elevated') costElevated++
      else if (c === 'missing') costMissing++
      else if (c === 'normal') costNormal++
      inputOpen += openInputCount(e.health)
    }
    const needAttention = critical + bug + offGoal + atRisk + costElevated + costMissing + inputOpen
    return { critical, bug, healthy, offGoal, atRisk, onTrack, costElevated, costMissing, costNormal, inputOpen, needAttention }
  }, [entries])

  const groups = useMemo(() => {
    const ATTENTION_GROUP = 'Need attention'
    const OK_GROUP = 'Healthy / on-track'
    const map = new Map<string, WorkflowDashEntry[]>()
    for (const e of entries) {
      const h = healthStatus(e.health)
      const p = progressStatus(e.progress)
      const c = costStatus(e.cost)
      const needsAttention = h === 'critical' || h === 'bug' || p === 'off-goal' || p === 'at-risk' || c === 'elevated' || c === 'missing' || openInputCount(e.health) > 0
      const group = needsAttention ? ATTENTION_GROUP : OK_GROUP
      const bucket = map.get(group)
      if (bucket) bucket.push(e)
      else map.set(group, [e])
    }
    // Need attention first, always.
    return Array.from(map.entries()).sort(([a], [b]) => {
      if (a === ATTENTION_GROUP) return -1
      if (b === ATTENTION_GROUP) return 1
      return a.localeCompare(b)
    })
  }, [entries])

  const hasAnyCard = useMemo(
    () => entries.some(e => e.health || e.progress || e.cost),
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
        <h2 className="text-base font-semibold tracking-tight text-foreground">Org Dashboard</h2>
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
            Pulse (health/cost) and Goal Advisor (goal) loops.
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
      <div className="mx-4 mt-3 flex flex-wrap items-center gap-2 rounded-lg border border-border bg-card/95 px-3 py-2.5 shadow-sm">
        <span className="font-runloop-mono rounded-md border border-border bg-background/70 px-2 py-1 text-[11px] font-semibold uppercase tracking-[0.06em] text-foreground">
          Attention {triage.needAttention}
        </span>
        <TriageMetric label="Critical" value={triage.critical} Icon={CircleAlert} className="border-red-500/25 bg-red-500/10 text-red-600 dark:text-red-300" />
        <TriageMetric label="Bug" value={triage.bug} Icon={Bug} className="border-amber-500/25 bg-amber-500/10 text-amber-600 dark:text-amber-300" />
        <TriageMetric label="Healthy" value={triage.healthy} Icon={CheckCircle2} className="border-emerald-500/25 bg-emerald-500/10 text-emerald-600 dark:text-emerald-300" />
        <span className="hidden h-5 w-px bg-border sm:block" />
        <TriageMetric label="Off goal" value={triage.offGoal} Icon={CircleAlert} className="border-red-500/20 bg-background/60 text-muted-foreground" />
        <TriageMetric label="At risk" value={triage.atRisk} Icon={AlertTriangle} className="border-amber-500/20 bg-background/60 text-muted-foreground" />
        <TriageMetric label="On track" value={triage.onTrack} Icon={Target} className="border-emerald-500/20 bg-background/60 text-muted-foreground" />
        <span className="hidden h-5 w-px bg-border sm:block" />
        <TriageMetric label="Cost watch" value={triage.costElevated} Icon={AlertTriangle} className="border-amber-500/20 bg-background/60 text-muted-foreground" />
        <TriageMetric label="Cost missing" value={triage.costMissing} Icon={CircleAlert} className="border-red-500/20 bg-background/60 text-muted-foreground" />
        <TriageMetric label="Cost ok" value={triage.costNormal} Icon={DollarSign} className="border-sky-500/20 bg-background/60 text-muted-foreground" />
        <span className="hidden h-5 w-px bg-border sm:block" />
        <TriageMetric label="Input" value={triage.inputOpen} Icon={CircleHelp} className="border-violet-500/20 bg-background/60 text-muted-foreground" />
      </div>

      {/* Status groups */}
      <div className="flex-1 space-y-5 overflow-auto px-4 py-4">
        {groups.map(([group, items]) => (
          <section key={group} className="space-y-2">
            <div className="flex items-center gap-2">
              {group === 'Need attention' ? (
                <AlertTriangle className="h-4 w-4 text-amber-500" />
              ) : (
                <Activity className="h-4 w-4 text-emerald-500" />
              )}
              <h3 className="text-sm font-semibold tracking-tight text-foreground">{group}</h3>
              <span className="font-runloop-mono text-[11px] text-muted-foreground">
                {items.length} workflow{items.length !== 1 ? 's' : ''}
              </span>
            </div>
            <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
              {items.map(entry => {
                const h = healthStatus(entry.health)
                const p = progressStatus(entry.progress)
                const c = costStatus(entry.cost)
                const updated = latestUpdated(entry.health, entry.progress, entry.cost)
                const rel = relativeTime(updated)
                const goalLabel = entry.health?.goal?.trim() || entry.progress?.goal?.trim() || entry.cost?.goal?.trim() || ''
                const inputOpen = openInputCount(entry.health)
                const healthSecondary = entry.health?.fields.next || entry.health?.fields.fix || entry.health?.detail
                const healthSecondaryLabel = entry.health?.fields.next ? 'Next' : entry.health?.fields.fix ? 'Fix' : 'Detail'
                return (
                  <div
                    key={entry.workspacePath}
                    role="button"
                    tabIndex={0}
                    aria-label={`Open ${entry.label} details`}
                    onClick={() => setSelectedEntry(entry)}
                    onKeyDown={(event) => {
                      if (event.key === 'Enter' || event.key === ' ') {
                        event.preventDefault()
                        setSelectedEntry(entry)
                      }
                    }}
                    className="cursor-pointer rounded-lg border border-border bg-card/95 p-3 text-left shadow-sm transition-colors hover:border-primary/40 hover:bg-card focus:outline-none focus:ring-2 focus:ring-ring/50"
                  >
                    <div className="flex items-start justify-between gap-2">
                      <h4 className="min-w-0 truncate text-sm font-semibold text-foreground" title={entry.label}>
                        {entry.label}
                      </h4>
                      {rel && <span className="font-runloop-mono shrink-0 text-[10px] text-muted-foreground/80">{rel.replace(/^updated /, '')}</span>}
                    </div>
                    {goalLabel && (
                      <p className="mt-0.5 flex items-center gap-1 truncate text-[11px] text-muted-foreground" title={goalLabel}>
                        <Target className="h-3 w-3 shrink-0" />
                        {goalLabel}
                      </p>
                    )}
                    <div className="mt-2 flex flex-wrap items-center gap-1.5">
                      <Pill Icon={HEALTH_PILL[h].Icon} className={HEALTH_PILL[h].className} label={HEALTH_PILL[h].label} />
                      <Pill Icon={PROGRESS_PILL[p].Icon} className={PROGRESS_PILL[p].className} label={PROGRESS_PILL[p].label} />
                      <Pill Icon={COST_PILL[c].Icon} className={COST_PILL[c].className} label={COST_PILL[c].label} />
                      {inputOpen > 0 && (
                        <Pill Icon={CircleHelp} className="border-violet-500/25 bg-violet-500/10 text-violet-600 dark:text-violet-300" label="Needs input" />
                      )}
                    </div>
                    <div className="mt-2 space-y-1.5 text-xs leading-5 text-muted-foreground">
                      {entry.health?.headline && (
                        <p className="line-clamp-2">
                          <span className="font-runloop-mono mr-1 text-[10px] font-semibold uppercase tracking-[0.08em] text-muted-foreground/70">Health</span>
                          {entry.health.headline}
                        </p>
                      )}
                      {healthSecondary && (
                        <p className="line-clamp-1">
                          <span className="font-runloop-mono mr-1 text-[10px] font-semibold uppercase tracking-[0.08em] text-muted-foreground/70">{healthSecondaryLabel}</span>
                          {healthSecondary}
                        </p>
                      )}
                      {entry.progress?.headline && (
                        <p className="line-clamp-2">
                          <span className="font-runloop-mono mr-1 text-[10px] font-semibold uppercase tracking-[0.08em] text-muted-foreground/70">Goal</span>
                          {entry.progress.headline}
                        </p>
                      )}
                      {entry.cost?.headline && (
                        <p className="line-clamp-2">
                          <span className="font-runloop-mono mr-1 text-[10px] font-semibold uppercase tracking-[0.08em] text-muted-foreground/70">Cost</span>
                          {entry.cost.headline}
                        </p>
                      )}
                      {!entry.health?.headline && !entry.progress?.headline && !entry.cost?.headline && (
                        <p className="italic">No status reported yet.</p>
                      )}
                    </div>
                  </div>
                )
              })}
            </div>
          </section>
        ))}
      </div>
      {selectedEntry && (
        <WorkflowDetailModal
          entry={selectedEntry}
          onClose={() => setSelectedEntry(null)}
        />
      )}
    </div>
  )
}

export default OrgDashboard
