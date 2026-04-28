import React, { useEffect, useState, useCallback } from 'react'
import {
  X,
  Loader2,
  RefreshCw,
  Beaker,
  Target,
  Activity,
  History,
  Clock,
  CheckCircle,
  XCircle,
  AlertTriangle,
  StopCircle,
  Plus,
  Hand,
  ListChecks,
  ChevronDown,
  ChevronRight,
  TrendingUp,
  FileText,
  ClipboardCheck,
} from 'lucide-react'
import { agentApi } from '../../services/api'
import ModalPortal from '../ui/ModalPortal'
import { MarkdownRenderer } from '../ui/MarkdownRenderer'

// =====================================================================
// AutoImprovementPopup — surfaces the auto-improvement framework state
// for a workflow: active experiments, history, metric definitions, the
// decisions feed. Read endpoints land directly; mutating actions (abort,
// extend, manual-conclude, approve) call the corresponding POST routes.
//
// See docs/workflow/auto_improvement_framework.md for the design.
// =====================================================================

interface AutoImprovementPopupProps {
  isOpen: boolean
  onClose: () => void
  workspacePath: string | null
}

type Tab = 'experiments' | 'metrics' | 'trajectory' | 'decisions' | 'improve' | 'review'

interface Metric {
  id: string
  label?: string
  unit: string
  direction: 'higher_better' | 'lower_better'
  mode: 'target' | 'slo'
  target?: number
  floor?: number
  ceiling?: number
  source: { type: string; id?: string; field?: string }
  evaluable_at_lag?: string
  parent?: string
  version?: number
  linked_success_criteria?: string[]
}

interface Experiment {
  id: string
  status: string
  hypothesis: string
  target_metrics: string[]
  expected_direction: 'increase' | 'decrease' | 'maintain'
  expected_magnitude: number
  baseline?: { mean?: Record<string, number>; std?: Record<string, number>; insufficient?: boolean }
  measurement?: { target_runs: number; completed_runs: number; values?: Record<string, number[]> }
  conclusion?: {
    verdict?: string
    rationale?: string
    evidence?: { post_mean?: Record<string, number>; magnitude_observed?: Record<string, number>; per_run_beat_pct?: Record<string, number>; drift_flagged?: boolean }
    verdict_overridden?: boolean
  }
  started_at: string
  concluded_at?: string
  intervention?: { trigger: string; applied_changes: string[] }
}

interface Decision {
  ts: string
  id: string
  source: 'agent' | 'user' | 'system'
  trigger: string
  rationale?: string
  applied_changes: string[]
  target_metrics?: string[]
  linked_experiment_id?: string
  rule_added?: string
  rule_section?: string
}

const STATUS_COLORS: Record<string, string> = {
  proposed: 'bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-300',
  'awaiting-approval': 'bg-amber-100 text-amber-800 dark:bg-amber-900/30 dark:text-amber-300',
  measuring: 'bg-indigo-100 text-indigo-800 dark:bg-indigo-900/30 dark:text-indigo-300',
  evaluating: 'bg-purple-100 text-purple-800 dark:bg-purple-900/30 dark:text-purple-300',
  'awaiting-conclusion-approval': 'bg-amber-100 text-amber-800 dark:bg-amber-900/30 dark:text-amber-300',
  concluded: 'bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-300',
  aborted: 'bg-gray-200 text-gray-700 dark:bg-gray-700 dark:text-gray-300',
}

const VERDICT_COLORS: Record<string, string> = {
  kept: 'text-green-600 dark:text-green-400',
  reverted: 'text-red-600 dark:text-red-400',
  inconclusive: 'text-amber-600 dark:text-amber-400',
  extend: 'text-blue-600 dark:text-blue-400',
}

const SOURCE_BADGE: Record<string, string> = {
  agent: 'bg-indigo-100 text-indigo-800 dark:bg-indigo-900/30 dark:text-indigo-300',
  user: 'bg-emerald-100 text-emerald-800 dark:bg-emerald-900/30 dark:text-emerald-300',
  system: 'bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-300',
}

const formatTs = (ts: string) => {
  if (!ts) return ''
  const d = new Date(ts)
  if (isNaN(d.getTime())) return ts
  return d.toLocaleString()
}

const VERDICT_DOT: Record<string, string> = {
  kept: '#16a34a',
  reverted: '#dc2626',
  inconclusive: '#d97706',
  extend: '#2563eb',
}

interface TrajectoryPanelProps {
  metrics: Metric[]
  experiments: Experiment[]
  decisions: Decision[]
}

interface TrajectoryPoint {
  t: number              // ms since epoch — concluded_at OR started_at fallback
  value: number          // post_mean for finished, baseline_mean for unfinished
  experiment: Experiment
  isProjected: boolean   // true if we're plotting baseline (no post_mean yet)
}

const buildSeries = (metricId: string, experiments: Experiment[]): TrajectoryPoint[] => {
  const points: TrajectoryPoint[] = []
  for (const exp of experiments) {
    if (!exp.target_metrics.includes(metricId)) continue
    const post = exp.conclusion?.evidence?.post_mean?.[metricId]
    const baseline = exp.baseline?.mean?.[metricId]
    const tStr = exp.concluded_at || exp.started_at
    const t = tStr ? Date.parse(tStr) : NaN
    if (!Number.isFinite(t)) continue
    if (typeof post === 'number') {
      points.push({ t, value: post, experiment: exp, isProjected: false })
    } else if (typeof baseline === 'number') {
      points.push({ t, value: baseline, experiment: exp, isProjected: true })
    }
  }
  return points.sort((a, b) => a.t - b.t)
}

const TrajectoryChart: React.FC<{ metric: Metric; series: TrajectoryPoint[]; decisions: Decision[] }> = ({ metric, series, decisions }) => {
  const W = 640, H = 180
  const padL = 56, padR = 16, padT = 12, padB = 28
  const innerW = W - padL - padR
  const innerH = H - padT - padB

  if (series.length === 0) {
    return (
      <div className="border rounded-md p-3 text-xs text-muted-foreground">
        <div className="font-medium text-foreground">{metric.id}</div>
        No experiments have targeted this metric yet.
      </div>
    )
  }

  const ts = series.map((p) => p.t)
  const vs = series.map((p) => p.value)
  // Pull target/floor/ceiling into the y range so reference lines stay on canvas.
  const refValues: number[] = []
  if (typeof metric.target === 'number') refValues.push(metric.target)
  if (typeof metric.floor === 'number') refValues.push(metric.floor)
  if (typeof metric.ceiling === 'number') refValues.push(metric.ceiling)
  const allYs = [...vs, ...refValues]
  const yMinRaw = Math.min(...allYs)
  const yMaxRaw = Math.max(...allYs)
  const ySpan = yMaxRaw - yMinRaw || Math.max(Math.abs(yMaxRaw) * 0.1, 1)
  const yMin = yMinRaw - ySpan * 0.1
  const yMax = yMaxRaw + ySpan * 0.1

  const tMin = Math.min(...ts)
  const tMax = Math.max(...ts)
  const tSpan = tMax - tMin || 1

  const xOf = (t: number) => padL + ((t - tMin) / tSpan) * innerW
  const yOf = (v: number) => padT + (1 - (v - yMin) / (yMax - yMin)) * innerH

  const path = series
    .map((p, i) => `${i === 0 ? 'M' : 'L'} ${xOf(p.t).toFixed(1)} ${yOf(p.value).toFixed(1)}`)
    .join(' ')

  const formatY = (v: number) => {
    if (Math.abs(v) >= 100) return v.toFixed(0)
    if (Math.abs(v) >= 1) return v.toFixed(2)
    return v.toFixed(3)
  }
  const formatX = (t: number) => new Date(t).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })

  // Decision markers: vertical ticks at each decision tied to one of the
  // experiments shown (so we don't clutter the chart with unrelated decisions).
  const expIds = new Set(series.map((p) => p.experiment.id))
  const decisionMarkers = decisions
    .filter((d) => d.linked_experiment_id && expIds.has(d.linked_experiment_id))
    .map((d) => ({ t: Date.parse(d.ts), source: d.source, trigger: d.trigger }))
    .filter((d) => Number.isFinite(d.t) && d.t >= tMin && d.t <= tMax)

  // Reference lines: target / floor / ceiling.
  const refLines: { v: number; label: string; color: string }[] = []
  if (typeof metric.target === 'number') refLines.push({ v: metric.target, label: `target ${metric.target}`, color: '#7c3aed' })
  if (typeof metric.floor === 'number') refLines.push({ v: metric.floor, label: `floor ${metric.floor}`, color: '#dc2626' })
  if (typeof metric.ceiling === 'number') refLines.push({ v: metric.ceiling, label: `ceiling ${metric.ceiling}`, color: '#dc2626' })

  return (
    <div className="border rounded-md p-3 bg-card">
      <div className="flex items-baseline justify-between mb-2 gap-2 flex-wrap">
        <div>
          <span className="font-medium text-sm">{metric.id}</span>
          {metric.label && <span className="text-xs text-muted-foreground ml-2">{metric.label}</span>}
        </div>
        <div className="text-[10px] text-muted-foreground">
          {metric.unit} · {metric.direction} · {metric.mode} · {series.length} point{series.length === 1 ? '' : 's'}
        </div>
      </div>
      <svg viewBox={`0 0 ${W} ${H}`} className="w-full h-auto">
        {/* axes */}
        <line x1={padL} y1={padT} x2={padL} y2={padT + innerH} stroke="currentColor" strokeOpacity={0.25} />
        <line x1={padL} y1={padT + innerH} x2={padL + innerW} y2={padT + innerH} stroke="currentColor" strokeOpacity={0.25} />
        {/* y ticks */}
        {[0, 0.25, 0.5, 0.75, 1].map((f) => {
          const v = yMin + (yMax - yMin) * (1 - f)
          const y = padT + innerH * f
          return (
            <g key={`y-${f}`}>
              <line x1={padL} y1={y} x2={padL + innerW} y2={y} stroke="currentColor" strokeOpacity={0.08} />
              <text x={padL - 6} y={y + 3} textAnchor="end" fontSize={10} fill="currentColor" fillOpacity={0.6}>{formatY(v)}</text>
            </g>
          )
        })}
        {/* x ticks: first / middle / last */}
        {[tMin, tMin + tSpan / 2, tMax].map((t, i) => (
          <text key={`x-${i}`} x={xOf(t)} y={padT + innerH + 16} textAnchor={i === 0 ? 'start' : i === 2 ? 'end' : 'middle'} fontSize={10} fill="currentColor" fillOpacity={0.6}>
            {formatX(t)}
          </text>
        ))}
        {/* reference lines */}
        {refLines.map((r, i) => (
          <g key={`ref-${i}`}>
            <line x1={padL} y1={yOf(r.v)} x2={padL + innerW} y2={yOf(r.v)} stroke={r.color} strokeOpacity={0.55} strokeDasharray="4 3" />
            <text x={padL + innerW - 4} y={yOf(r.v) - 3} textAnchor="end" fontSize={9} fill={r.color}>{r.label}</text>
          </g>
        ))}
        {/* decision markers */}
        {decisionMarkers.map((d, i) => (
          <line key={`dec-${i}`} x1={xOf(d.t)} y1={padT} x2={xOf(d.t)} y2={padT + innerH} stroke={d.source === 'user' ? '#10b981' : d.source === 'agent' ? '#6366f1' : '#9ca3af'} strokeOpacity={0.4} strokeDasharray="2 2">
            <title>{`${d.source} · ${d.trigger} · ${new Date(d.t).toLocaleString()}`}</title>
          </line>
        ))}
        {/* trajectory line */}
        <path d={path} fill="none" stroke="#7c3aed" strokeOpacity={0.7} strokeWidth={1.5} />
        {/* points */}
        {series.map((p, i) => {
          const verdict = p.experiment.conclusion?.verdict || ''
          const fill = p.isProjected ? '#9ca3af' : (VERDICT_DOT[verdict] || '#6b7280')
          return (
            <circle
              key={`pt-${i}`}
              cx={xOf(p.t)}
              cy={yOf(p.value)}
              r={p.isProjected ? 3 : 4.5}
              fill={fill}
              stroke="white"
              strokeWidth={1}
              fillOpacity={p.isProjected ? 0.5 : 1}
            >
              <title>{`${p.experiment.id}\n${p.experiment.hypothesis}\nvalue=${formatY(p.value)}${p.isProjected ? ' (baseline — no post-mean yet)' : ''}\nverdict=${verdict || '—'}\n${new Date(p.t).toLocaleString()}`}</title>
            </circle>
          )
        })}
      </svg>
      <div className="flex flex-wrap items-center gap-3 mt-1 text-[10px] text-muted-foreground">
        <span>verdict:</span>
        {(['kept', 'reverted', 'inconclusive', 'extend'] as const).map((v) => (
          <span key={v} className="inline-flex items-center gap-1">
            <span className="inline-block w-2 h-2 rounded-full" style={{ backgroundColor: VERDICT_DOT[v] }} />
            {v}
          </span>
        ))}
        <span className="inline-flex items-center gap-1">
          <span className="inline-block w-2 h-2 rounded-full bg-gray-400 opacity-50" />
          baseline (no post-mean yet)
        </span>
      </div>
    </div>
  )
}

const TrajectoryPanel: React.FC<TrajectoryPanelProps> = ({ metrics, experiments, decisions }) => {
  if (metrics.length === 0) {
    return <p className="text-sm text-muted-foreground">No metrics defined yet — define metrics to see their trajectories.</p>
  }
  if (experiments.length === 0) {
    return <p className="text-sm text-muted-foreground">No experiments yet — once experiments run, this view plots their post-means against the metric reference lines.</p>
  }
  return (
    <div className="space-y-3">
      <p className="text-xs text-muted-foreground">
        Each point is one experiment's post-mean for the metric, plotted at <code>concluded_at</code> (or <code>started_at</code> if still running). Color = verdict. Dashed horizontal lines mark target / floor / ceiling. Vertical ticks mark linked decisions.
      </p>
      {metrics.map((m) => (
        <TrajectoryChart key={m.id} metric={m} series={buildSeries(m.id, experiments)} decisions={decisions} />
      ))}
    </div>
  )
}

interface BuilderDocPanelProps {
  which: 'improve' | 'review'
  doc: { exists: boolean; content: string; path: string } | null
  loading: boolean
  error: string | null
  onRefresh: () => void
}

const BuilderDocPanel: React.FC<BuilderDocPanelProps> = ({ which, doc, loading, error, onRefresh }) => {
  const title = which === 'improve' ? 'Improve log' : 'Review log'
  const blurb = which === 'improve'
    ? 'The optimizer agent\'s durable improvement log. Slash commands like /improve-eval, /improve-workflow, and /optimize-* read this on the way in and append decisions on the way out.'
    : 'The reviewer agent\'s findings log. /review-* slash commands append dated entries with severity-ordered findings and follow-ups (REVIEW = recommend, not apply).'
  const emptyHint = which === 'improve'
    ? 'No entries yet. Run /improve-setup-framework or any /improve-* command to bootstrap it.'
    : 'No entries yet. Run any /review-* slash command to append the first entry.'

  return (
    <div className="space-y-3">
      <div className="flex items-baseline justify-between gap-2 flex-wrap">
        <div>
          <h3 className="text-sm font-semibold">{title}</h3>
          {doc?.path && <code className="text-[10px] text-muted-foreground">{doc.path}</code>}
        </div>
        <button
          onClick={onRefresh}
          disabled={loading}
          className="inline-flex items-center gap-1 px-2 py-1 text-xs rounded border hover:bg-accent disabled:opacity-50"
        >
          {loading ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <RefreshCw className="w-3.5 h-3.5" />}
          Refresh
        </button>
      </div>
      <p className="text-xs text-muted-foreground">{blurb}</p>
      {error && (
        <div className="text-xs text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded p-2">
          {error}
        </div>
      )}
      {loading && !doc && (
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="w-4 h-4 animate-spin" /> Loading…
        </div>
      )}
      {doc && !doc.exists && (
        <div className="border border-dashed rounded-md p-4 text-sm text-muted-foreground">
          {emptyHint}
        </div>
      )}
      {doc && doc.exists && (
        <div className="border rounded-md p-4 bg-card">
          <MarkdownRenderer content={doc.content} disablePathLinking />
        </div>
      )}
    </div>
  )
}

const AutoImprovementPopup: React.FC<AutoImprovementPopupProps> = ({ isOpen, onClose, workspacePath }) => {
  const [tab, setTab] = useState<Tab>('experiments')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [metrics, setMetrics] = useState<Metric[]>([])
  const [activeMode, setActiveMode] = useState<string>('')
  const [activeExperiments, setActiveExperiments] = useState<Experiment[]>([])
  const [historyExperiments, setHistoryExperiments] = useState<Experiment[]>([])
  const [decisions, setDecisions] = useState<Decision[]>([])
  const [expandedExperiment, setExpandedExperiment] = useState<string | null>(null)
  const [expandedHistory, setExpandedHistory] = useState<string | null>(null)
  const [decisionFilter, setDecisionFilter] = useState<'all' | 'agent' | 'user' | 'system'>('all')
  const [improveDoc, setImproveDoc] = useState<{ exists: boolean; content: string; path: string } | null>(null)
  const [reviewDoc, setReviewDoc] = useState<{ exists: boolean; content: string; path: string } | null>(null)
  const [docLoading, setDocLoading] = useState<'improve' | 'review' | null>(null)
  const [docError, setDocError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    if (!workspacePath) return
    setLoading(true)
    setError(null)
    try {
      const [m, e, d] = await Promise.all([
        agentApi.getAutoImprovementMetrics(workspacePath).catch((err) => ({ success: false, error: String(err), file: undefined })),
        agentApi.getAutoImprovementExperiments(workspacePath, true).catch((err) => ({ success: false, active: [], history: [], error: String(err) })),
        agentApi.getAutoImprovementDecisions(workspacePath).catch((err) => ({ success: false, decisions: [], error: String(err) })),
      ])
      if (m.success && m.file) {
        setMetrics(Array.isArray(m.file.metrics) ? m.file.metrics : [])
        setActiveMode(m.file.active_mode || '')
      } else {
        setMetrics([])
        setActiveMode('')
      }
      if (e.success) {
        setActiveExperiments(Array.isArray(e.active) ? e.active : [])
        setHistoryExperiments(Array.isArray(e.history) ? e.history : [])
      }
      if (d.success) {
        setDecisions(Array.isArray(d.decisions) ? d.decisions : [])
      }
      const errs = [m.error, e.error, d.error].filter(Boolean)
      if (errs.length > 0) setError(errs.join('; '))
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [workspacePath])

  useEffect(() => {
    if (isOpen && workspacePath) {
      refresh()
    }
  }, [isOpen, workspacePath, refresh])

  const handleAbort = useCallback(async (experimentId: string) => {
    if (!workspacePath) return
    const reason = window.prompt('Reason for aborting this experiment? (required, will be logged)')
    if (!reason || !reason.trim()) return
    setLoading(true)
    try {
      const res = await agentApi.abortExperiment(workspacePath, experimentId, reason.trim())
      if (!res.success) {
        setError(res.error || 'abort failed')
      } else {
        await refresh()
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [workspacePath, refresh])

  const handleExtend = useCallback(async (experimentId: string) => {
    if (!workspacePath) return
    const runsStr = window.prompt('How many additional runs?', '5')
    if (!runsStr) return
    const additionalRuns = parseInt(runsStr, 10)
    if (!Number.isFinite(additionalRuns) || additionalRuns <= 0) {
      setError('additional_runs must be > 0')
      return
    }
    const reason = window.prompt('Reason for extending? (will be logged)') || 'extend window'
    setLoading(true)
    try {
      const res = await agentApi.extendExperiment(workspacePath, experimentId, additionalRuns, reason.trim() || 'extend window')
      if (!res.success) setError(res.error || 'extend failed')
      else await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [workspacePath, refresh])

  const handleApprove = useCallback(async (experimentId: string, gate: 'hypothesis' | 'conclusion') => {
    if (!workspacePath) return
    setLoading(true)
    try {
      const res = await agentApi.approveExperiment(workspacePath, experimentId, gate)
      if (!res.success) setError(res.error || 'approve failed')
      else await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [workspacePath, refresh])

  const handleManualConclude = useCallback(async (experimentId: string) => {
    if (!workspacePath) return
    const verdict = window.prompt('Verdict? (kept | reverted | inconclusive | extend)', 'kept')
    if (!verdict || !['kept', 'reverted', 'inconclusive', 'extend'].includes(verdict.trim())) {
      setError('verdict must be kept | reverted | inconclusive | extend')
      return
    }
    const reason = window.prompt('Override reason? (required, will be flagged in audit)')
    if (!reason || !reason.trim()) return
    const rationale = window.prompt('Short rationale for the verdict?') || reason.trim()
    setLoading(true)
    try {
      const res = await agentApi.manualConcludeExperiment(workspacePath, experimentId, verdict.trim(), reason.trim(), rationale.trim())
      if (!res.success) setError(res.error || 'manual conclude failed')
      else await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [workspacePath, refresh])

  const fetchDoc = useCallback(async (which: 'improve' | 'review') => {
    if (!workspacePath) return
    setDocLoading(which)
    setDocError(null)
    try {
      const res = await agentApi.getBuilderDoc(workspacePath, which)
      const payload = { exists: !!res.exists, content: res.content || '', path: res.path || '' }
      if (which === 'improve') setImproveDoc(payload)
      else setReviewDoc(payload)
      if (!res.success && res.error) setDocError(res.error)
    } catch (err) {
      setDocError(err instanceof Error ? err.message : String(err))
    } finally {
      setDocLoading(null)
    }
  }, [workspacePath])

  useEffect(() => {
    if (!isOpen || !workspacePath) return
    if (tab === 'improve' && improveDoc === null) fetchDoc('improve')
    if (tab === 'review' && reviewDoc === null) fetchDoc('review')
  }, [isOpen, workspacePath, tab, improveDoc, reviewDoc, fetchDoc])

  // Bust the cached docs whenever the workspace switches or the popup re-opens.
  useEffect(() => {
    setImproveDoc(null)
    setReviewDoc(null)
    setDocError(null)
  }, [workspacePath, isOpen])

  const filteredDecisions = decisions.filter((d) => decisionFilter === 'all' || d.source === decisionFilter)

  if (!isOpen) return null

  return (
    <ModalPortal>
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
        <div className="bg-background border rounded-lg shadow-xl w-full max-w-6xl max-h-[90vh] flex flex-col">
          <div className="flex items-center justify-between p-4 border-b">
            <div className="flex items-center gap-2">
              <Beaker className="w-5 h-5 text-purple-600" />
              <h2 className="text-lg font-semibold">Auto-improvement framework</h2>
              {activeMode && (
                <span className="ml-3 inline-flex items-center px-2 py-0.5 rounded text-xs bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-300">
                  active mode: {activeMode}
                </span>
              )}
            </div>
            <div className="flex items-center gap-2">
              <button
                onClick={refresh}
                disabled={loading}
                className="p-1.5 rounded-md hover:bg-accent disabled:opacity-50"
                title="Refresh"
              >
                {loading ? <Loader2 className="w-4 h-4 animate-spin" /> : <RefreshCw className="w-4 h-4" />}
              </button>
              <button onClick={onClose} className="p-1.5 rounded-md hover:bg-accent">
                <X className="w-4 h-4" />
              </button>
            </div>
          </div>

          <div className="flex border-b text-sm">
            {(
              [
                { id: 'experiments', icon: Activity, label: `Experiments (${activeExperiments.length} active / ${historyExperiments.length} done)` },
                { id: 'metrics', icon: Target, label: `Metrics (${metrics.length})` },
                { id: 'trajectory', icon: TrendingUp, label: 'Trajectory' },
                { id: 'decisions', icon: ListChecks, label: `Decisions (${decisions.length})` },
                { id: 'improve', icon: FileText, label: 'Improve log' },
                { id: 'review', icon: ClipboardCheck, label: 'Review log' },
              ] as const
            ).map((t) => {
              const Icon = t.icon
              const active = tab === t.id
              return (
                <button
                  key={t.id}
                  onClick={() => setTab(t.id)}
                  className={`flex items-center gap-2 px-4 py-2 border-b-2 transition-colors ${
                    active ? 'border-purple-600 text-purple-600' : 'border-transparent text-muted-foreground hover:text-foreground'
                  }`}
                >
                  <Icon className="w-4 h-4" />
                  {t.label}
                </button>
              )
            })}
          </div>

          {error && (
            <div className="px-4 py-2 text-xs text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-900/20 border-b">
              {error}
            </div>
          )}

          <div className="flex-1 overflow-y-auto p-4">
            {tab === 'experiments' && (
              <div className="space-y-6">
                <section>
                  <h3 className="text-sm font-semibold mb-2 flex items-center gap-2">
                    <Activity className="w-4 h-4" /> Active experiments
                  </h3>
                  {activeExperiments.length === 0 ? (
                    <p className="text-sm text-muted-foreground">No active experiments. Open one via <code>/improve-eval</code>, <code>/improve-workflow</code>, or directly via the agent in optimizer mode.</p>
                  ) : (
                    <div className="space-y-2">
                      {activeExperiments.map((exp) => {
                        const expanded = expandedExperiment === exp.id
                        const N = exp.measurement?.completed_runs ?? 0
                        const M = exp.measurement?.target_runs ?? 0
                        const pct = M > 0 ? Math.round((N / M) * 100) : 0
                        return (
                          <div key={exp.id} className="border rounded-md overflow-hidden">
                            <button
                              onClick={() => setExpandedExperiment(expanded ? null : exp.id)}
                              className="w-full text-left p-3 hover:bg-accent/30 flex items-start gap-2"
                            >
                              {expanded ? <ChevronDown className="w-4 h-4 mt-0.5 flex-shrink-0" /> : <ChevronRight className="w-4 h-4 mt-0.5 flex-shrink-0" />}
                              <div className="flex-1 min-w-0">
                                <div className="flex items-center gap-2 mb-1 flex-wrap">
                                  <span className={`inline-flex items-center px-2 py-0.5 rounded text-xs ${STATUS_COLORS[exp.status] || 'bg-gray-100 text-gray-800'}`}>
                                    {exp.status}
                                  </span>
                                  <code className="text-xs text-muted-foreground">{exp.id}</code>
                                  <span className="text-xs text-muted-foreground">{N}/{M} runs ({pct}%)</span>
                                </div>
                                <p className="text-sm font-medium leading-tight">{exp.hypothesis}</p>
                                <p className="text-xs text-muted-foreground mt-1">
                                  targets: {exp.target_metrics.join(', ')} · expected {exp.expected_direction} by {exp.expected_magnitude} · started {formatTs(exp.started_at)}
                                </p>
                              </div>
                            </button>
                            {expanded && (
                              <div className="border-t p-3 bg-muted/30 text-xs space-y-2">
                                {exp.baseline?.mean && Object.keys(exp.baseline.mean).length > 0 && (
                                  <div>
                                    <span className="font-medium">Baseline mean:</span>{' '}
                                    {Object.entries(exp.baseline.mean).map(([k, v]) => `${k}=${v}`).join(', ')}
                                    {exp.baseline.insufficient && <span className="ml-2 text-amber-600">(insufficient history flagged)</span>}
                                  </div>
                                )}
                                {exp.measurement?.values && Object.keys(exp.measurement.values).length > 0 && (
                                  <div>
                                    <span className="font-medium">Measured values:</span>{' '}
                                    {Object.entries(exp.measurement.values).map(([k, vs]) => `${k}=[${vs.join(', ')}]`).join('; ')}
                                  </div>
                                )}
                                {exp.intervention && (
                                  <div>
                                    <span className="font-medium">Intervention ({exp.intervention.trigger}):</span>{' '}
                                    {exp.intervention.applied_changes.join(', ')}
                                  </div>
                                )}
                                {exp.conclusion?.verdict && (
                                  <div>
                                    <span className="font-medium">Verdict:</span>{' '}
                                    <span className={VERDICT_COLORS[exp.conclusion.verdict] || ''}>{exp.conclusion.verdict}</span>
                                    {exp.conclusion.rationale && <span className="ml-2">— {exp.conclusion.rationale}</span>}
                                  </div>
                                )}
                                <div className="flex flex-wrap gap-2 pt-2">
                                  {exp.status === 'awaiting-approval' && (
                                    <button
                                      onClick={() => handleApprove(exp.id, 'hypothesis')}
                                      className="inline-flex items-center gap-1 px-2 py-1 text-xs rounded bg-green-100 text-green-800 hover:bg-green-200 dark:bg-green-900/30 dark:text-green-300"
                                    >
                                      <CheckCircle className="w-3.5 h-3.5" /> Approve hypothesis
                                    </button>
                                  )}
                                  {exp.status === 'awaiting-conclusion-approval' && (
                                    <button
                                      onClick={() => handleApprove(exp.id, 'conclusion')}
                                      className="inline-flex items-center gap-1 px-2 py-1 text-xs rounded bg-green-100 text-green-800 hover:bg-green-200 dark:bg-green-900/30 dark:text-green-300"
                                    >
                                      <CheckCircle className="w-3.5 h-3.5" /> Approve conclusion
                                    </button>
                                  )}
                                  {(exp.status === 'measuring' || exp.status === 'evaluating') && (
                                    <button
                                      onClick={() => handleExtend(exp.id)}
                                      className="inline-flex items-center gap-1 px-2 py-1 text-xs rounded bg-blue-100 text-blue-800 hover:bg-blue-200 dark:bg-blue-900/30 dark:text-blue-300"
                                    >
                                      <Plus className="w-3.5 h-3.5" /> Extend
                                    </button>
                                  )}
                                  {exp.status !== 'concluded' && exp.status !== 'aborted' && (
                                    <button
                                      onClick={() => handleManualConclude(exp.id)}
                                      className="inline-flex items-center gap-1 px-2 py-1 text-xs rounded bg-amber-100 text-amber-800 hover:bg-amber-200 dark:bg-amber-900/30 dark:text-amber-300"
                                    >
                                      <Hand className="w-3.5 h-3.5" /> Manual conclude
                                    </button>
                                  )}
                                  {exp.status !== 'concluded' && exp.status !== 'aborted' && (
                                    <button
                                      onClick={() => handleAbort(exp.id)}
                                      className="inline-flex items-center gap-1 px-2 py-1 text-xs rounded bg-red-100 text-red-800 hover:bg-red-200 dark:bg-red-900/30 dark:text-red-300"
                                    >
                                      <StopCircle className="w-3.5 h-3.5" /> Abort
                                    </button>
                                  )}
                                </div>
                              </div>
                            )}
                          </div>
                        )
                      })}
                    </div>
                  )}
                </section>

                <section>
                  <h3 className="text-sm font-semibold mb-2 flex items-center gap-2">
                    <History className="w-4 h-4" /> Past experiments ({historyExperiments.length})
                  </h3>
                  {historyExperiments.length === 0 ? (
                    <p className="text-sm text-muted-foreground">No concluded experiments yet.</p>
                  ) : (
                    <div className="space-y-2">
                      {historyExperiments.slice().reverse().map((exp) => {
                        const expanded = expandedHistory === exp.id
                        const verdict = exp.conclusion?.verdict || exp.status
                        return (
                          <div key={exp.id} className="border rounded-md overflow-hidden">
                            <button
                              onClick={() => setExpandedHistory(expanded ? null : exp.id)}
                              className="w-full text-left p-3 hover:bg-accent/30 flex items-start gap-2"
                            >
                              {expanded ? <ChevronDown className="w-4 h-4 mt-0.5 flex-shrink-0" /> : <ChevronRight className="w-4 h-4 mt-0.5 flex-shrink-0" />}
                              <div className="flex-1 min-w-0">
                                <div className="flex items-center gap-2 mb-1 flex-wrap">
                                  <span className={`text-xs font-semibold ${VERDICT_COLORS[verdict] || ''}`}>{verdict}</span>
                                  {exp.conclusion?.verdict_overridden && (
                                    <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] bg-amber-100 text-amber-800 dark:bg-amber-900/30 dark:text-amber-300">
                                      <AlertTriangle className="w-2.5 h-2.5 mr-0.5" />override
                                    </span>
                                  )}
                                  <code className="text-xs text-muted-foreground">{exp.id}</code>
                                  <span className="text-xs text-muted-foreground"><Clock className="w-3 h-3 inline mr-0.5" />{formatTs(exp.concluded_at || exp.started_at)}</span>
                                </div>
                                <p className="text-sm leading-tight">{exp.hypothesis}</p>
                                <p className="text-xs text-muted-foreground mt-1">
                                  targets: {exp.target_metrics.join(', ')}
                                </p>
                              </div>
                            </button>
                            {expanded && exp.conclusion && (
                              <div className="border-t p-3 bg-muted/30 text-xs space-y-1">
                                {exp.conclusion.rationale && <div><span className="font-medium">Rationale:</span> {exp.conclusion.rationale}</div>}
                                {exp.baseline?.mean && (
                                  <div><span className="font-medium">Baseline:</span> {Object.entries(exp.baseline.mean).map(([k, v]) => `${k}=${v}`).join(', ')}</div>
                                )}
                                {exp.measurement?.values && (
                                  <div><span className="font-medium">Measured:</span> {Object.entries(exp.measurement.values).map(([k, vs]) => `${k}=[${vs.join(', ')}]`).join('; ')}</div>
                                )}
                              </div>
                            )}
                          </div>
                        )
                      })}
                    </div>
                  )}
                </section>
              </div>
            )}

            {tab === 'metrics' && (
              <div>
                {metrics.length === 0 ? (
                  <p className="text-sm text-muted-foreground">No metrics defined yet. Run <code>/improve-setup-framework</code> in optimizer mode to bootstrap.</p>
                ) : (
                  <div className="overflow-x-auto">
                    <table className="w-full text-sm">
                      <thead className="text-xs text-muted-foreground border-b">
                        <tr>
                          <th className="text-left py-2 px-2">id</th>
                          <th className="text-left py-2 px-2">unit</th>
                          <th className="text-left py-2 px-2">direction</th>
                          <th className="text-left py-2 px-2">mode</th>
                          <th className="text-left py-2 px-2">target / floor / ceiling</th>
                          <th className="text-left py-2 px-2">source</th>
                          <th className="text-left py-2 px-2">success criteria</th>
                          <th className="text-left py-2 px-2">lag</th>
                          <th className="text-left py-2 px-2">v</th>
                        </tr>
                      </thead>
                      <tbody>
                        {metrics.map((m) => (
                          <tr key={m.id} className="border-b last:border-0 hover:bg-accent/30">
                            <td className="py-2 px-2"><code className="text-xs">{m.id}</code>{m.label && <div className="text-[10px] text-muted-foreground">{m.label}</div>}</td>
                            <td className="py-2 px-2 text-xs">{m.unit}</td>
                            <td className="py-2 px-2 text-xs">{m.direction}</td>
                            <td className="py-2 px-2 text-xs">{m.mode}</td>
                            <td className="py-2 px-2 text-xs">{m.target ?? m.floor ?? m.ceiling ?? '—'}</td>
                            <td className="py-2 px-2 text-xs">{m.source.type}{m.source.id && `:${m.source.id}`}{m.source.field && `:${m.source.field}`}</td>
                            <td className="py-2 px-2 text-xs">
                              {m.linked_success_criteria && m.linked_success_criteria.length > 0 ? (
                                <span className="inline-flex flex-wrap gap-1">
                                  {m.linked_success_criteria.map((sc, i) => (
                                    <span key={i} className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] bg-emerald-100 text-emerald-800 dark:bg-emerald-900/30 dark:text-emerald-300" title={sc}>
                                      {sc.length > 32 ? sc.slice(0, 30) + '…' : sc}
                                    </span>
                                  ))}
                                </span>
                              ) : (
                                <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] bg-amber-100 text-amber-800 dark:bg-amber-900/30 dark:text-amber-300" title="No linked success criteria — telemetry / auxiliary metric. Verdicts on this metric do not directly reflect user-facing success.">
                                  unanchored
                                </span>
                              )}
                            </td>
                            <td className="py-2 px-2 text-xs">{m.evaluable_at_lag || '—'}</td>
                            <td className="py-2 px-2 text-xs">{m.version || 1}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
              </div>
            )}

            {tab === 'trajectory' && (
              <TrajectoryPanel
                metrics={metrics}
                experiments={[...activeExperiments, ...historyExperiments]}
                decisions={decisions}
              />
            )}

            {tab === 'decisions' && (
              <div className="space-y-2">
                <div className="flex items-center gap-2 text-xs">
                  <span className="text-muted-foreground">Source filter:</span>
                  {(['all', 'agent', 'user', 'system'] as const).map((s) => (
                    <button
                      key={s}
                      onClick={() => setDecisionFilter(s)}
                      className={`px-2 py-0.5 rounded ${decisionFilter === s ? 'bg-purple-600 text-white' : 'bg-muted hover:bg-accent'}`}
                    >
                      {s}
                    </button>
                  ))}
                  <span className="ml-auto text-muted-foreground">{filteredDecisions.length} of {decisions.length}</span>
                </div>
                {filteredDecisions.length === 0 ? (
                  <p className="text-sm text-muted-foreground">No decisions yet.</p>
                ) : (
                  <div className="space-y-1">
                    {filteredDecisions.slice().reverse().map((d) => (
                      <div key={d.id} className="border rounded-md p-2 text-xs">
                        <div className="flex items-center gap-2 flex-wrap mb-1">
                          <span className={`inline-flex items-center px-1.5 py-0.5 rounded text-[10px] ${SOURCE_BADGE[d.source] || ''}`}>{d.source}</span>
                          <code className="text-muted-foreground">{d.trigger}</code>
                          <span className="text-muted-foreground">{formatTs(d.ts)}</span>
                          {d.linked_experiment_id && <code className="text-muted-foreground">→ {d.linked_experiment_id}</code>}
                        </div>
                        {d.rule_added && (
                          <div className="mt-1">
                            <span className="font-medium">Rule:</span> {d.rule_added}
                            {d.rule_section && <span className="text-muted-foreground"> (section: {d.rule_section})</span>}
                          </div>
                        )}
                        {d.rationale && <div className="mt-1">{d.rationale}</div>}
                        {d.target_metrics && d.target_metrics.length > 0 && (
                          <div className="mt-1 text-muted-foreground">→ targets: {d.target_metrics.join(', ')}</div>
                        )}
                        {d.applied_changes && d.applied_changes.length > 0 && (
                          <div className="mt-1 text-muted-foreground">files: {d.applied_changes.join(', ')}</div>
                        )}
                      </div>
                    ))}
                  </div>
                )}
              </div>
            )}

            {(tab === 'improve' || tab === 'review') && (
              <BuilderDocPanel
                which={tab}
                doc={tab === 'improve' ? improveDoc : reviewDoc}
                loading={docLoading === tab}
                error={docError}
                onRefresh={() => fetchDoc(tab)}
              />
            )}
          </div>
        </div>
      </div>
    </ModalPortal>
  )
}

export default AutoImprovementPopup
