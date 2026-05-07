import React, { useEffect, useState, useCallback, useMemo } from 'react'
import {
  X,
  Loader2,
  RefreshCw,
  Target,
  ListChecks,
  TrendingUp,
  FileText,
  ClipboardCheck,
  AlertTriangle,
} from 'lucide-react'
import {
  CartesianGrid,
  Line,
  LineChart,
  ReferenceArea,
  ReferenceLine,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { agentApi } from '../../services/api'
import ModalPortal from '../ui/ModalPortal'
import { MarkdownRenderer } from '../ui/MarkdownRenderer'

// =====================================================================
// AutoImprovementPopup — surfaces the auto-improvement framework state for a
// workflow: metric definitions, metric trajectory, decisions, and durable
// improvement/review logs.
//
// See docs/workflow/auto_improvement_framework.md for the design.
// =====================================================================

interface AutoImprovementPopupProps {
  isOpen: boolean
  onClose: () => void
  workspacePath: string | null
}

type Tab = 'metrics' | 'trajectory' | 'decisions' | 'soul' | 'improve' | 'review'
type BuilderDocKind = 'soul' | 'improve' | 'review'

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
  success_criteria?: string
}

interface Decision {
  ts: string
  id: string
  source: 'agent' | 'user' | 'system'
  trigger: string
  rationale?: string
  applied_changes: string[]
  target_metrics?: string[]
  rule_added?: string
  rule_section?: string
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

// MetricSnapshotRow is one row from db/metrics_history.jsonl, written by the
// post-run snapshot hook (see agent_go/cmd/server/metrics_snapshot.go).
interface MetricSnapshotRow {
  run_folder: string
  completed_at: string
  metric_id: string
  value: number
  has_value: boolean
  resolve_error?: string
  threshold_kind?: string
  threshold_value?: number
  passed?: boolean
}

interface TrajectoryPanelProps {
  metrics: Metric[]
  history: MetricSnapshotRow[]
}

interface TrajectoryPoint {
  t: number              // ms since epoch — completed_at of the run
  value: number          // metric value for that run
  runFolder: string      // for tooltip
  passed?: boolean       // pass/fail vs threshold; undefined when not evaluable
}

interface NormalizedTrajectoryPoint extends TrajectoryPoint {
  progress: number
  plottedProgress: number
}

interface NormalizedTrajectorySeries {
  metric: Metric
  color: string
  points: NormalizedTrajectoryPoint[]
}

type TrajectoryDatum = {
  t: number
  label: string
  [key: string]: string | number | boolean | undefined
}

interface MetricHistoryIssue {
  metric: Metric
  missingCount: number
  latestMissing?: MetricSnapshotRow
  lastError?: MetricSnapshotRow
}

const TRAJECTORY_COLORS = [
  '#2563eb',
  '#16a34a',
  '#dc2626',
  '#9333ea',
  '#0891b2',
  '#ca8a04',
  '#db2777',
  '#4f46e5',
  '#ea580c',
  '#059669',
]

const metricName = (metric: Metric) => metric.label || metric.id

const metricSuccessCriteria = (metric: Metric): string => metric.success_criteria?.trim() || ''

const metricThreshold = (metric: Metric): number | undefined => {
  if (typeof metric.target === 'number') return metric.target
  if (typeof metric.floor === 'number') return metric.floor
  if (typeof metric.ceiling === 'number') return metric.ceiling
  return undefined
}

const metricThresholdText = (metric: Metric): string => {
  if (typeof metric.target === 'number') return `target ${formatNumber(metric.target)}`
  if (typeof metric.floor === 'number') return `floor ${formatNumber(metric.floor)}`
  if (typeof metric.ceiling === 'number') return `ceiling ${formatNumber(metric.ceiling)}`
  return 'no threshold'
}

const formatNumber = (v: number): string => {
  if (!Number.isFinite(v)) return '—'
  if (Math.abs(v) >= 1000) return v.toLocaleString(undefined, { maximumFractionDigits: 0 })
  if (Math.abs(v) >= 10) return v.toLocaleString(undefined, { maximumFractionDigits: 1 })
  if (Math.abs(v) >= 1) return v.toLocaleString(undefined, { maximumFractionDigits: 2 })
  return v.toLocaleString(undefined, { maximumFractionDigits: 3 })
}

const progressForMetricValue = (metric: Metric, value: number): number | null => {
  const threshold = metricThreshold(metric)
  if (threshold === undefined || !Number.isFinite(value)) return null

  if (metric.direction === 'higher_better') {
    if (threshold === 0) return value >= threshold ? 100 : 0
    return Math.max(0, (value / threshold) * 100)
  }

  if (threshold === 0) return value <= 0 ? 100 : 0
  if (value <= threshold) return 100
  return Math.max(0, (threshold / value) * 100)
}

const buildSeries = (metricId: string, history: MetricSnapshotRow[]): TrajectoryPoint[] => {
  const points: TrajectoryPoint[] = []
  for (const row of history) {
    if (row.metric_id !== metricId) continue
    if (!row.has_value) continue
    const t = Date.parse(row.completed_at)
    if (!Number.isFinite(t)) continue
    points.push({ t, value: row.value, runFolder: row.run_folder, passed: row.passed })
  }
  return points.sort((a, b) => a.t - b.t)
}

const CombinedTrajectoryChart: React.FC<{ metrics: Metric[]; history: MetricSnapshotRow[] }> = ({ metrics, history }) => {
  const seriesList: NormalizedTrajectorySeries[] = useMemo(() => {
    return metrics
      .map((metric, metricIndex) => {
        const raw = buildSeries(metric.id, history)
        const points = raw
          .map((p): NormalizedTrajectoryPoint | null => {
            const progress = progressForMetricValue(metric, p.value)
            if (progress === null) return null
            return {
              ...p,
              progress,
              plottedProgress: Math.min(progress, 160),
            }
          })
          .filter((p): p is NormalizedTrajectoryPoint => p !== null)
        return {
          metric,
          color: TRAJECTORY_COLORS[metricIndex % TRAJECTORY_COLORS.length],
          points,
        }
      })
      .filter((series) => series.points.length > 0)
  }, [metrics, history])

  const latestByMetric = useMemo(() => {
    const latest = new Map<string, MetricSnapshotRow>()
    for (const row of history) {
      const prev = latest.get(row.metric_id)
      if (!prev || Date.parse(row.completed_at) > Date.parse(prev.completed_at)) {
        latest.set(row.metric_id, row)
      }
    }
    return latest
  }, [history])

  const [visibleMetricIds, setVisibleMetricIds] = useState<Set<string>>(() => new Set())

  useEffect(() => {
    setVisibleMetricIds((prev) => {
      const available = new Set(seriesList.map((series) => series.metric.id))
      const next = new Set(Array.from(prev).filter((id) => available.has(id)))
      if (next.size === 0) {
        for (const series of seriesList) next.add(series.metric.id)
      }
      return next
    })
  }, [seriesList])

  const effectiveVisibleMetricIds = visibleMetricIds.size > 0
    ? visibleMetricIds
    : new Set(seriesList.map((series) => series.metric.id))
  const visibleSeries = seriesList.filter((series) => effectiveVisibleMetricIds.has(series.metric.id))
  const chartData: TrajectoryDatum[] = useMemo(() => {
    const byTimestamp = new Map<number, TrajectoryDatum>()
    for (const series of seriesList) {
      for (const point of series.points) {
        let datum = byTimestamp.get(point.t)
        if (!datum) {
          datum = {
            t: point.t,
            label: new Date(point.t).toLocaleDateString(undefined, { month: 'short', day: 'numeric' }),
          }
          byTimestamp.set(point.t, datum)
        }
        datum[series.metric.id] = point.plottedProgress
        datum[`${series.metric.id}__progress`] = point.progress
        datum[`${series.metric.id}__raw`] = point.value
        datum[`${series.metric.id}__run`] = point.runFolder
        datum[`${series.metric.id}__passed`] = point.passed
      }
    }
    return Array.from(byTimestamp.values()).sort((a, b) => a.t - b.t)
  }, [seriesList])

  const totalMetricCount = metrics.length
  const latestWithValue = metrics.filter((m) => latestByMetric.get(m.id)?.has_value).length
  const latestPassing = metrics.filter((m) => latestByMetric.get(m.id)?.passed === true).length
  const latestFailing = metrics.filter((m) => latestByMetric.get(m.id)?.passed === false).length
  const latestMissing = totalMetricCount - latestWithValue
  const historyIssues = useMemo<MetricHistoryIssue[]>(() => {
    return metrics
      .map((metric) => {
        const rows = history
          .filter((row) => row.metric_id === metric.id)
          .sort((a, b) => Date.parse(b.completed_at) - Date.parse(a.completed_at))
        const missingRows = rows.filter((row) => !row.has_value)
        const latest = rows[0]
        return {
          metric,
          missingCount: missingRows.length,
          latestMissing: latest && !latest.has_value ? latest : undefined,
          lastError: missingRows.find((row) => row.resolve_error),
        }
      })
      .filter((issue) => issue.missingCount > 0)
  }, [metrics, history])
  const totalMissingHistoryRows = historyIssues.reduce((sum, issue) => sum + issue.missingCount, 0)

  if (seriesList.length === 0) {
    return (
      <div className="border rounded-md p-4 text-sm text-muted-foreground">
        No metric has enough resolved values to draw a trajectory yet.
      </div>
    )
  }

  const toggleMetric = (metricId: string) => {
    setVisibleMetricIds((prev) => {
      const next = prev.size > 0
        ? new Set(prev)
        : new Set(seriesList.map((series) => series.metric.id))
      if (next.has(metricId)) {
        next.delete(metricId)
      } else {
        next.add(metricId)
      }
      return next
    })
  }

  const showFailingOnly = () => {
    const failingIds = seriesList
      .filter((series) => latestByMetric.get(series.metric.id)?.passed === false)
      .map((series) => series.metric.id)
    setVisibleMetricIds(new Set(failingIds.length > 0 ? failingIds : seriesList.map((series) => series.metric.id)))
  }

  const showAll = () => setVisibleMetricIds(new Set(seriesList.map((series) => series.metric.id)))

  const tooltip = (props: {
    active?: boolean
    payload?: ReadonlyArray<{ dataKey?: unknown; value?: unknown; color?: string; payload?: TrajectoryDatum }>
    label?: unknown
  }) => {
    const { active, payload, label } = props
    if (!active || !payload || payload.length === 0) return null
    const rows = payload
      .filter((entry) => entry.value !== undefined && entry.dataKey !== undefined)
      .map((entry) => {
        if (typeof entry.dataKey !== 'string' && typeof entry.dataKey !== 'number') return null
        const metricId = String(entry.dataKey)
        const series = seriesList.find((candidate) => candidate.metric.id === metricId)
        if (!series || !entry.payload) return null
        return {
          metric: series.metric,
          color: entry.color || series.color,
          progress: Number(entry.payload[`${metricId}__progress`] ?? entry.value),
          raw: Number(entry.payload[`${metricId}__raw`]),
          run: String(entry.payload[`${metricId}__run`] ?? ''),
          passed: entry.payload[`${metricId}__passed`],
        }
      })
      .filter((row): row is { metric: Metric; color: string; progress: number; raw: number; run: string; passed: string | number | boolean | undefined } => row !== null)
      .sort((a, b) => a.progress - b.progress)

    return (
      <div className="max-w-sm rounded-md border bg-popover p-3 text-xs shadow-lg">
        <div className="mb-2 font-medium text-foreground">{label == null ? '' : String(label)}</div>
        <div className="space-y-2">
          {rows.map((row) => {
            const status = row.passed === true ? 'pass' : row.passed === false ? 'fail' : 'unknown'
            const statusClass = status === 'pass' ? 'text-emerald-600' : status === 'fail' ? 'text-red-600' : 'text-muted-foreground'
            return (
              <div key={row.metric.id} className="grid grid-cols-[10px_minmax(0,1fr)_auto] gap-2 items-start">
                <span className="mt-1 h-2.5 w-2.5 rounded-full" style={{ backgroundColor: row.color }} />
                <div className="min-w-0">
                  <div className="truncate font-medium text-foreground">{metricName(row.metric)}</div>
                  <div className="truncate text-muted-foreground">{row.run}</div>
                </div>
                <div className="text-right tabular-nums">
                  <div className={statusClass}>{formatNumber(row.progress)}%</div>
                  <div className="text-muted-foreground">{formatNumber(row.raw)} {row.metric.unit}</div>
                </div>
              </div>
            )
          })}
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-3">
      <div className="border rounded-md bg-card p-4">
        <div className="flex items-start justify-between gap-3 flex-wrap mb-3">
          <div>
            <h3 className="text-sm font-semibold text-foreground">Metric trajectory</h3>
            <p className="text-xs text-muted-foreground mt-1">
              One line per metric. Y-axis is normalized to threshold; 100% is the pass line.
            </p>
          </div>
          <div className="grid grid-cols-4 gap-2 text-center text-xs">
            <div className="rounded border px-2 py-1">
              <div className="font-semibold text-foreground">{totalMetricCount}</div>
              <div className="text-[10px] text-muted-foreground">metrics</div>
            </div>
            <div className="rounded border px-2 py-1">
              <div className="font-semibold text-emerald-600">{latestPassing}</div>
              <div className="text-[10px] text-muted-foreground">pass</div>
            </div>
            <div className="rounded border px-2 py-1">
              <div className="font-semibold text-red-600">{latestFailing}</div>
              <div className="text-[10px] text-muted-foreground">fail</div>
            </div>
            <div className="rounded border px-2 py-1">
              <div className="font-semibold text-muted-foreground">{latestMissing}</div>
              <div className="text-[10px] text-muted-foreground">missing</div>
            </div>
          </div>
        </div>

        <div className="mb-3 flex flex-wrap items-center gap-2">
          <button onClick={showAll} className="rounded border px-2 py-1 text-xs hover:bg-accent">All</button>
          <button onClick={showFailingOnly} className="rounded border px-2 py-1 text-xs hover:bg-accent">Failing</button>
          <span className="text-xs text-muted-foreground">{visibleSeries.length} shown</span>
        </div>

        <div className="h-[360px] w-full">
          <ResponsiveContainer>
            <LineChart data={chartData} margin={{ top: 12, right: 18, left: 0, bottom: 6 }}>
              <ReferenceArea y1={0} y2={100} fill="#dc2626" fillOpacity={0.04} />
              <ReferenceArea y1={100} y2={160} fill="#16a34a" fillOpacity={0.05} />
              <CartesianGrid stroke="hsl(var(--border))" strokeDasharray="3 3" vertical={false} opacity={0.75} />
              <XAxis
                dataKey="label"
                tick={{ fontSize: 11, fill: 'hsl(var(--muted-foreground))' }}
                tickLine={false}
                axisLine={{ stroke: 'hsl(var(--border))' }}
                minTickGap={18}
              />
              <YAxis
                domain={[0, 160]}
                ticks={[0, 50, 100, 150]}
                tickFormatter={(value) => `${value}%`}
                tick={{ fontSize: 11, fill: 'hsl(var(--muted-foreground))' }}
                tickLine={false}
                axisLine={{ stroke: 'hsl(var(--border))' }}
                width={48}
              />
              <Tooltip content={tooltip} />
              <ReferenceLine
                y={100}
                stroke="#16a34a"
                strokeDasharray="5 4"
                strokeWidth={1.5}
                label={{ value: 'pass line', fill: '#16a34a', fontSize: 11, position: 'insideTopRight' }}
              />
              {visibleSeries.map((series) => (
                <Line
                  key={series.metric.id}
                  type="monotone"
                  dataKey={series.metric.id}
                  name={metricName(series.metric)}
                  stroke={series.color}
                  strokeWidth={2.4}
                  dot={{ r: 3, fill: series.color, stroke: 'hsl(var(--background))', strokeWidth: 1 }}
                  activeDot={{ r: 6, stroke: 'hsl(var(--background))', strokeWidth: 2 }}
                  connectNulls={false}
                  isAnimationActive={false}
                />
              ))}
            </LineChart>
          </ResponsiveContainer>
        </div>

        <div className="mt-3 flex flex-wrap gap-2">
          {seriesList.map((series) => {
            const active = effectiveVisibleMetricIds.has(series.metric.id)
            return (
              <button
                key={series.metric.id}
                onClick={() => toggleMetric(series.metric.id)}
                className={`inline-flex max-w-[260px] items-center gap-1.5 rounded border px-2 py-1 text-xs transition-colors ${active ? 'bg-background text-foreground' : 'bg-muted/50 text-muted-foreground opacity-60'}`}
                title={metricName(series.metric)}
              >
                <span className="h-2.5 w-2.5 rounded-full flex-none" style={{ backgroundColor: series.color }} />
                <span className="truncate">{metricName(series.metric)}</span>
              </button>
            )
          })}
        </div>
      </div>

      {historyIssues.length > 0 && (
        <div className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-900 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-200">
          <div className="flex items-start gap-2">
            <AlertTriangle className="mt-0.5 h-4 w-4 flex-none" />
            <div>
              <div className="font-medium">
                Metric history has {totalMissingHistoryRows} no-value row{totalMissingHistoryRows === 1 ? '' : 's'} across {historyIssues.length} active metric{historyIssues.length === 1 ? '' : 's'}.
              </div>
              <div className="mt-0.5 text-amber-800/80 dark:text-amber-100/80">
                No-value rows are not plotted. See the Issue column below for the latest resolve error.
              </div>
            </div>
          </div>
        </div>
      )}

      <div className="border rounded-md overflow-hidden">
        <div className="grid grid-cols-[minmax(180px,1fr)_90px_90px_90px_minmax(220px,1.4fr)] gap-3 bg-muted/40 px-3 py-2 text-[11px] font-medium text-muted-foreground">
          <div>Metric</div>
          <div>Latest</div>
          <div>Threshold</div>
          <div>Status</div>
          <div>Issue</div>
        </div>
        <div className="divide-y">
          {metrics.map((metric, index) => {
            const latest = latestByMetric.get(metric.id)
            const issue = historyIssues.find((candidate) => candidate.metric.id === metric.id)
            const color = TRAJECTORY_COLORS[index % TRAJECTORY_COLORS.length]
            const status = !latest || !latest.has_value ? 'missing' : latest.passed === true ? 'pass' : latest.passed === false ? 'fail' : 'unknown'
            const statusClass = status === 'pass'
              ? 'bg-emerald-100 text-emerald-800 dark:bg-emerald-900/30 dark:text-emerald-300'
              : status === 'fail'
                ? 'bg-red-100 text-red-800 dark:bg-red-900/30 dark:text-red-300'
                : 'bg-muted text-muted-foreground'
            return (
              <div key={metric.id} className="grid grid-cols-[minmax(180px,1fr)_90px_90px_90px_minmax(220px,1.4fr)] gap-3 px-3 py-2 text-xs items-center">
                <div className="min-w-0 flex items-center gap-2">
                  <span className="h-2.5 w-2.5 rounded-full flex-none" style={{ backgroundColor: color }} />
                  <div className="min-w-0">
                    <div className="font-medium text-foreground truncate">{metricName(metric)}</div>
                    <code className="text-[10px] text-muted-foreground">{metric.id}</code>
                  </div>
                </div>
                <div className="tabular-nums text-foreground">
                  {latest?.has_value ? formatNumber(latest.value) : '—'}
                </div>
                <div className="text-muted-foreground truncate" title={metricThresholdText(metric)}>
                  {metricThresholdText(metric)}
                </div>
                <div>
                  <span className={`inline-flex rounded px-1.5 py-0.5 text-[10px] font-medium ${statusClass}`}>{status}</span>
                </div>
                <div className="min-w-0">
                  {issue?.latestMissing?.resolve_error ? (
                    <div className="truncate text-red-700 dark:text-red-300" title={issue.latestMissing.resolve_error}>
                      {issue.latestMissing.resolve_error}
                    </div>
                  ) : issue?.lastError?.resolve_error ? (
                    <div className="truncate text-amber-700 dark:text-amber-300" title={issue.lastError.resolve_error}>
                      {issue.missingCount} older no-value row{issue.missingCount === 1 ? '' : 's'}: {issue.lastError.resolve_error}
                    </div>
                  ) : issue ? (
                    <div className="truncate text-muted-foreground">
                      {issue.missingCount} no-value row{issue.missingCount === 1 ? '' : 's'} without resolver detail
                    </div>
                  ) : (
                    <span className="text-muted-foreground">—</span>
                  )}
                </div>
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}

const TrajectoryPanel: React.FC<TrajectoryPanelProps> = ({ metrics, history }) => {
  if (metrics.length === 0) {
    return <p className="text-sm text-muted-foreground">No metrics defined yet — define metrics to see their trajectories.</p>
  }
  if (history.length === 0) {
    return <p className="text-sm text-muted-foreground">No metric history yet — once a workflow run completes, this view plots one point per run from <code>db/metrics_history.jsonl</code>.</p>
  }
  return (
    <CombinedTrajectoryChart metrics={metrics} history={history} />
  )
}

interface BuilderDocPanelProps {
  which: BuilderDocKind
  doc: { exists: boolean; content: string; path: string } | null
  loading: boolean
  error: string | null
  onRefresh: () => void
}

const BuilderDocPanel: React.FC<BuilderDocPanelProps> = ({ which, doc, loading, error, onRefresh }) => {
  const copy = {
    soul: {
      title: 'Soul',
      blurb: 'The workflow north star. Optimizer treats this as the source of truth for objective and success criteria; metrics, reviews, and plans are judged against it.',
      emptyHint: 'soul/soul.md is missing. Define ## Objective and ## Success Criteria before relying on metrics or improvement decisions.',
    },
    improve: {
      title: 'Improve log',
      blurb: 'The optimizer agent\'s durable improvement log. Slash commands like /improve-eval, /improve-workflow, and /optimize-* read this on the way in and append decisions on the way out.',
      emptyHint: 'No entries yet. Run /improve-setup-framework or any /improve-* command to bootstrap it.',
    },
    review: {
      title: 'Review log',
      blurb: 'The reviewer agent\'s findings log. /review-* slash commands append dated entries with severity-ordered findings and follow-ups (REVIEW = recommend, not apply).',
      emptyHint: 'No entries yet. Run any /review-* slash command to append the first entry.',
    },
  }[which]

  return (
    <div className="space-y-3">
      <div className="flex items-baseline justify-between gap-2 flex-wrap">
        <div>
          <h3 className="text-sm font-semibold">{copy.title}</h3>
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
      <p className="text-xs text-muted-foreground">{copy.blurb}</p>
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
          {copy.emptyHint}
        </div>
      )}
      {doc && doc.exists && (
        <div className="border rounded-md p-3 bg-card">
          <MarkdownRenderer
            content={doc.content}
            disablePathLinking
            className="!text-[12px] leading-relaxed [&_p]:!text-[12px] [&_li]:!text-[12px] [&_h1]:!text-base [&_h2]:!text-sm [&_h3]:!text-xs [&_h1]:mt-3 [&_h2]:mt-3 [&_h3]:mt-2 [&_p]:my-1.5 [&_ul]:my-1.5 [&_ol]:my-1.5 [&_code]:!text-[11px] [&_pre]:!text-[11px]"
          />
        </div>
      )}
    </div>
  )
}

const AutoImprovementPopup: React.FC<AutoImprovementPopupProps> = ({ isOpen, onClose, workspacePath }) => {
  const [tab, setTab] = useState<Tab>('metrics')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [metrics, setMetrics] = useState<Metric[]>([])
  const [decisions, setDecisions] = useState<Decision[]>([])
  const [metricsHistory, setMetricsHistory] = useState<MetricSnapshotRow[]>([])
  const [decisionFilter, setDecisionFilter] = useState<'all' | 'agent' | 'user' | 'system'>('all')
  const [improveDoc, setImproveDoc] = useState<{ exists: boolean; content: string; path: string } | null>(null)
  const [reviewDoc, setReviewDoc] = useState<{ exists: boolean; content: string; path: string } | null>(null)
  const [soulDoc, setSoulDoc] = useState<{ exists: boolean; content: string; path: string } | null>(null)
  const [docLoading, setDocLoading] = useState<BuilderDocKind | null>(null)
  const [docError, setDocError] = useState<string | null>(null)
  const [frameworkHealth, setFrameworkHealth] = useState<{
    soul_exists: boolean
    objective_ok: boolean
    success_criteria_ok: boolean
  } | null>(null)

  const refresh = useCallback(async () => {
    if (!workspacePath) return
    setLoading(true)
    setError(null)
    try {
      const [m, d, h, mh] = await Promise.all([
        agentApi.getAutoImprovementMetrics(workspacePath).catch((err) => ({ success: false, error: String(err), file: undefined })),
        agentApi.getAutoImprovementDecisions(workspacePath).catch((err) => ({ success: false, decisions: [], error: String(err) })),
        agentApi.getFrameworkHealth(workspacePath).catch((err) => ({ success: false, error: String(err), soul_exists: false, objective_ok: false, success_criteria_ok: false })),
        agentApi.getMetricsHistory(workspacePath).catch((err) => ({ success: false, rows: [], error: String(err) })),
      ])
      if (m.success && m.file) {
        setMetrics(Array.isArray(m.file.metrics) ? m.file.metrics : [])
      } else {
        setMetrics([])
      }
      if (d.success) {
        setDecisions(Array.isArray(d.decisions) ? d.decisions : [])
      }
      if (h.success) {
        setFrameworkHealth({
          soul_exists: !!h.soul_exists,
          objective_ok: !!h.objective_ok,
          success_criteria_ok: !!h.success_criteria_ok,
        })
      } else {
        setFrameworkHealth(null)
      }
      if (mh.success) {
        setMetricsHistory(Array.isArray(mh.rows) ? mh.rows : [])
      } else {
        setMetricsHistory([])
      }
      const errs = [m.error, d.error, h.error, mh.error].filter(Boolean)
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

  const fetchDoc = useCallback(async (which: BuilderDocKind) => {
    if (!workspacePath) return
    setDocLoading(which)
    setDocError(null)
    try {
      const res = await agentApi.getBuilderDoc(workspacePath, which)
      const payload = { exists: !!res.exists, content: res.content || '', path: res.path || '' }
      if (which === 'soul') setSoulDoc(payload)
      else if (which === 'improve') setImproveDoc(payload)
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
    if (tab === 'soul' && soulDoc === null) fetchDoc('soul')
    if (tab === 'improve' && improveDoc === null) fetchDoc('improve')
    if (tab === 'review' && reviewDoc === null) fetchDoc('review')
  }, [isOpen, workspacePath, tab, soulDoc, improveDoc, reviewDoc, fetchDoc])

  // Bust the cached docs whenever the workspace switches or the popup re-opens.
  useEffect(() => {
    setSoulDoc(null)
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
              <TrendingUp className="w-5 h-5 text-purple-600" />
              <h2 className="text-lg font-semibold">Auto-improvement framework</h2>
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
                { id: 'metrics', icon: Target, label: `Metrics (${metrics.length})` },
                { id: 'trajectory', icon: TrendingUp, label: 'Trajectory' },
                { id: 'decisions', icon: ListChecks, label: `Decisions (${decisions.length})` },
                { id: 'soul', icon: Target, label: 'Soul' },
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

          {frameworkHealth && (() => {
            const issues: { kind: 'critical' | 'warning'; msg: string }[] = []
            if (!frameworkHealth.soul_exists) {
              issues.push({ kind: 'critical', msg: 'soul/soul.md is missing — define ## Objective and ## Success Criteria before adding metrics.' })
            } else {
              if (!frameworkHealth.objective_ok) issues.push({ kind: 'critical', msg: 'soul.md ## Objective is empty or still a TODO placeholder.' })
              if (!frameworkHealth.success_criteria_ok) issues.push({ kind: 'critical', msg: 'soul.md ## Success Criteria is empty — without it, metrics have no north star to measure against.' })
            }
            if (issues.length === 0) return null
            const hasCritical = issues.some((i) => i.kind === 'critical')
            return (
              <div className={`px-4 py-2 text-xs border-b ${hasCritical ? 'bg-red-50 dark:bg-red-900/20 text-red-800 dark:text-red-200 border-red-200 dark:border-red-800' : 'bg-amber-50 dark:bg-amber-900/20 text-amber-800 dark:text-amber-200 border-amber-200 dark:border-amber-800'}`}>
                <div className="font-medium mb-1">Framework health</div>
                <ul className="list-disc list-inside space-y-0.5">
                  {issues.map((i, n) => <li key={n}>{i.msg}</li>)}
                </ul>
              </div>
            )
          })()}

          <div className="flex-1 overflow-y-auto p-4">
	            {tab === 'metrics' && (
	              <div>
	                {metrics.length === 0 ? (
	                  <p className="text-sm text-muted-foreground">No metrics defined yet. Run <code>/improve-setup-framework</code> in optimizer mode to bootstrap.</p>
	                ) : (
	                  <div className="space-y-3">
	                    {metrics.some((m) => !metricSuccessCriteria(m)) && (
	                      <div className="flex items-start gap-2 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-900 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-200">
	                        <AlertTriangle className="mt-0.5 h-4 w-4 flex-none" />
	                        <div>
	                          <div className="font-medium">Some metrics are not linked to success criteria.</div>
	                          <div className="mt-0.5 text-amber-800/80 dark:text-amber-100/80">
	                            Add <code>success_criteria</code> to each metric in <code>planning/metrics.json</code> so every number is anchored to a user outcome.
	                          </div>
	                        </div>
	                      </div>
	                    )}
	                  <div className="overflow-x-auto">
	                    <table className="w-full text-sm">
	                      <thead className="text-xs text-muted-foreground border-b">
	                        <tr>
	                          <th className="text-left py-2 px-2">id</th>
	                          <th className="text-left py-2 px-2 min-w-[220px]">success criteria</th>
	                          <th className="text-left py-2 px-2">unit</th>
	                          <th className="text-left py-2 px-2">direction</th>
	                          <th className="text-left py-2 px-2">mode</th>
                          <th className="text-left py-2 px-2">target / floor / ceiling</th>
                          <th className="text-left py-2 px-2">source</th>
                        </tr>
	                      </thead>
	                      <tbody>
	                        {metrics.map((m) => {
	                          const criteria = metricSuccessCriteria(m)
	                          return (
	                            <tr key={m.id} className="border-b last:border-0 hover:bg-accent/30 align-top">
	                              <td className="py-2 px-2"><code className="text-xs">{m.id}</code>{m.label && <div className="text-[10px] text-muted-foreground">{m.label}</div>}</td>
	                              <td className="py-2 px-2 text-xs">
	                                {criteria ? (
	                                  <div className="max-w-sm text-foreground">{criteria}</div>
	                                ) : (
	                                  <span className="inline-flex items-center gap-1 rounded bg-amber-100 px-1.5 py-0.5 text-[10px] font-medium text-amber-800 dark:bg-amber-900/30 dark:text-amber-200" title="Metric is missing success_criteria in planning/metrics.json">
	                                    <AlertTriangle className="h-3 w-3" />
	                                    not linked
	                                  </span>
	                                )}
	                              </td>
	                              <td className="py-2 px-2 text-xs">{m.unit}</td>
	                              <td className="py-2 px-2 text-xs">{m.direction}</td>
	                              <td className="py-2 px-2 text-xs">{m.mode}</td>
	                              <td className="py-2 px-2 text-xs">{m.target ?? m.floor ?? m.ceiling ?? '—'}</td>
	                              <td className="py-2 px-2 text-xs">{m.source.type}{m.source.id && `:${m.source.id}`}{m.source.field && `:${m.source.field}`}</td>
	                            </tr>
	                          )
	                        })}
	                      </tbody>
	                    </table>
	                  </div>
	                  </div>
	                )}
	              </div>
	            )}

            {tab === 'trajectory' && (
              <TrajectoryPanel
                metrics={metrics}
                history={metricsHistory}
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

            {(tab === 'soul' || tab === 'improve' || tab === 'review') && (
              <BuilderDocPanel
                which={tab}
                doc={tab === 'soul' ? soulDoc : tab === 'improve' ? improveDoc : reviewDoc}
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
