import React, { useEffect, useState, useCallback, useMemo, useRef } from 'react'
import {
  X,
  Loader2,
  RefreshCw,
  Target,
  TrendingUp,
  BarChart3,
  FileText,
  ClipboardCheck,
  AlertTriangle,
} from 'lucide-react'
import {
  CartesianGrid,
  Line,
  LineChart,
  ReferenceLine,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { agentApi } from '../../services/api'
import ModalPortal from '../ui/ModalPortal'
import { MarkdownRenderer } from '../ui/MarkdownRenderer'
import { HtmlRenderer } from '../ui/HtmlRenderer'
import { EvaluationReportsPanel } from './EvaluationPopup'

// =====================================================================
// AutoImprovementPopup — surfaces the auto-improvement framework state for a
// workflow: metric definitions, metric trajectory, and durable
// improvement/review logs.
//
// See docs/workflow/auto_improvement_framework.md for the design.
// =====================================================================

interface AutoImprovementPopupProps {
  isOpen: boolean
  onClose: () => void
  workspacePath: string | null
  selectedRunFolder?: string | null
}

type Tab = 'metrics' | 'evaluation' | 'soul' | 'improve' | 'review'
type BuilderDocKind = 'soul' | 'improve' | 'review'
type BuilderDoc = { exists: boolean; content: string; path: string }

const emptyDocLoadingState = (): Record<BuilderDocKind, boolean> => ({
  soul: false,
  improve: false,
  review: false,
})

const emptyDocErrorState = (): Record<BuilderDocKind, string | null> => ({
  soul: null,
  improve: null,
  review: null,
})

interface BuilderDocArchiveFile {
  path: string
  label: string
}

interface Metric {
  id: string
  label?: string
  role?: 'primary' | 'secondary'
  category?: string
  unit: string
  direction: 'higher_better' | 'lower_better'
  mode: 'target' | 'slo'
  target?: number
  floor?: number
  ceiling?: number
  source: { type: string; id?: string; field?: string }
  success_criteria?: string
  version?: number
}

// MetricSnapshotRow is one row from db/metrics_history.jsonl, written by the
// post-run snapshot hook (see agent_go/cmd/server/metrics_snapshot.go).
interface MetricSnapshotRow {
  run_folder: string
  completed_at: string
  metric_id: string
  metric_version?: number
  value: number
  has_value: boolean
  resolve_error?: string
  threshold_kind?: string
  threshold_value?: number
  passed?: boolean
}

interface TrajectoryPoint {
  t: number              // ms since epoch — completed_at of the run
  value: number          // metric value for that run
  runFolder: string      // for tooltip
  passed?: boolean       // pass/fail vs threshold; undefined when not evaluable
}

interface MetricHistoryIssue {
  metric: Metric
  missingCount: number
  latestMissing?: MetricSnapshotRow
  lastError?: MetricSnapshotRow
}

interface MetricTrendDatum {
  label: string
  completedAt: string
  value: number
  runFolder: string
  passed?: boolean
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

const metricRole = (metric: Metric): 'primary' | 'secondary' | 'uncategorized' => {
  if (metric.role === 'primary' || metric.role === 'secondary') return metric.role
  return 'uncategorized'
}

const metricCategory = (metric: Metric): string => metric.category?.trim() || 'uncategorized'

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

const buildSeries = (metric: Metric, history: MetricSnapshotRow[]): TrajectoryPoint[] => {
  const points: TrajectoryPoint[] = []
  const activeVersion = metric.version || 1
  for (const row of history) {
    if (row.metric_id !== metric.id) continue
    const rowVersion = row.metric_version || 1
    if (rowVersion !== activeVersion) continue
    if (!row.has_value) continue
    const t = Date.parse(row.completed_at)
    if (!Number.isFinite(t)) continue
    points.push({ t, value: row.value, runFolder: row.run_folder, passed: row.passed })
  }
  return points.sort((a, b) => a.t - b.t)
}

const metricHistoryRows = (metric: Metric, history: MetricSnapshotRow[]): MetricSnapshotRow[] => {
  const activeVersion = metric.version || 1
  return history
    .filter((row) => row.metric_id === metric.id && (row.metric_version || 1) === activeVersion)
    .sort((a, b) => Date.parse(b.completed_at) - Date.parse(a.completed_at))
}

const metricHistoryIssue = (metric: Metric, history: MetricSnapshotRow[]): MetricHistoryIssue | null => {
  const rows = metricHistoryRows(metric, history)
  const missingRows = rows.filter((row) => !row.has_value)
  if (missingRows.length === 0) return null
  const latest = rows[0]
  return {
    metric,
    missingCount: missingRows.length,
    latestMissing: latest && !latest.has_value ? latest : undefined,
    lastError: missingRows.find((row) => row.resolve_error),
  }
}

const metricCurrentIssue = (metric: Metric, history: MetricSnapshotRow[]): MetricHistoryIssue | null => {
  const rows = metricHistoryRows(metric, history)
  const latest = rows[0]
  if (!latest || latest.has_value) return null
  return {
    metric,
    missingCount: rows.filter((row) => !row.has_value).length,
    latestMissing: latest,
    lastError: latest.resolve_error ? latest : rows.find((row) => !row.has_value && row.resolve_error),
  }
}

const MetricTrajectoryChart: React.FC<{ metric: Metric; history: MetricSnapshotRow[]; color: string }> = ({ metric, history, color }) => {
  const points = useMemo(() => buildSeries(metric, history), [metric, history])
  const data: MetricTrendDatum[] = useMemo(() => {
    return points.map((point) => ({
      label: new Date(point.t).toLocaleDateString(undefined, { month: 'short', day: 'numeric' }),
      completedAt: new Date(point.t).toLocaleString(),
      value: point.value,
      runFolder: point.runFolder,
      passed: point.passed,
    }))
  }, [points])
  const threshold = metricThreshold(metric)

  if (data.length === 0) {
    return (
      <div className="flex h-24 items-center justify-center rounded-md border border-dashed bg-muted/20 text-xs text-muted-foreground">
        No resolved trajectory values yet.
      </div>
    )
  }

  const tooltip = (props: {
    active?: boolean
    payload?: ReadonlyArray<{ payload?: MetricTrendDatum }>
    label?: unknown
  }) => {
    const { active, payload, label } = props
    if (!active || !payload || payload.length === 0) return null
    const row = payload[0]?.payload
    if (!row) return null
    const status = row.passed === true ? 'pass' : row.passed === false ? 'fail' : 'unknown'
    const statusClass = status === 'pass' ? 'text-emerald-600' : status === 'fail' ? 'text-red-600' : 'text-muted-foreground'

    return (
      <div className="max-w-xs rounded-md border bg-popover p-3 text-xs shadow-lg">
        <div className="mb-1 font-medium text-foreground">{label == null ? row.completedAt : String(label)}</div>
        <div className="grid grid-cols-[minmax(0,1fr)_auto] gap-3">
          <div className="min-w-0">
            <div className="truncate text-muted-foreground">{row.runFolder}</div>
            <div className="truncate text-muted-foreground">{row.completedAt}</div>
          </div>
          <div className="text-right tabular-nums">
            <div className="font-medium text-foreground">{formatNumber(row.value)} {metric.unit}</div>
            <div className={statusClass}>{status}</div>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="h-32 w-full">
      <ResponsiveContainer>
        <LineChart data={data} margin={{ top: 12, right: 12, left: 0, bottom: 0 }}>
          <CartesianGrid stroke="hsl(var(--border))" strokeDasharray="3 3" vertical={false} opacity={0.65} />
          <XAxis
            dataKey="label"
            tick={{ fontSize: 10, fill: 'hsl(var(--muted-foreground))' }}
            tickLine={false}
            axisLine={{ stroke: 'hsl(var(--border))' }}
            minTickGap={18}
          />
          <YAxis
            tick={{ fontSize: 10, fill: 'hsl(var(--muted-foreground))' }}
            tickLine={false}
            axisLine={{ stroke: 'hsl(var(--border))' }}
            width={44}
            tickFormatter={(value) => formatNumber(Number(value))}
          />
          <Tooltip content={tooltip} />
          {threshold !== undefined && (
            <ReferenceLine
              y={threshold}
              stroke="#16a34a"
              strokeDasharray="4 4"
              strokeWidth={1.25}
              label={{ value: metricThresholdText(metric), fill: '#16a34a', fontSize: 10, position: 'insideTopRight' }}
            />
          )}
          <Line
            type="monotone"
            dataKey="value"
            name={metricName(metric)}
            stroke={color}
            strokeWidth={2.2}
            dot={{ r: 3, fill: color, stroke: 'hsl(var(--background))', strokeWidth: 1 }}
            activeDot={{ r: 5, stroke: 'hsl(var(--background))', strokeWidth: 2 }}
            isAnimationActive={false}
          />
        </LineChart>
      </ResponsiveContainer>
    </div>
  )
}

const MetricCard: React.FC<{ metric: Metric; history: MetricSnapshotRow[]; color: string }> = ({ metric, history, color }) => {
  const rows = useMemo(() => metricHistoryRows(metric, history), [metric, history])
  const issue = useMemo(() => metricCurrentIssue(metric, history), [metric, history])
  const historicalIssue = useMemo(() => metricHistoryIssue(metric, history), [metric, history])
  const latest = rows[0]
  const criteria = metricSuccessCriteria(metric)
  const source = `${metric.source.type}${metric.source.id ? `:${metric.source.id}` : ''}${metric.source.field ? `:${metric.source.field}` : ''}`
  const role = metricRole(metric)
  const category = metricCategory(metric)
  const status = !latest || !latest.has_value ? 'missing' : latest.passed === true ? 'pass' : latest.passed === false ? 'fail' : 'unknown'
  const statusClass = status === 'pass'
    ? 'bg-emerald-100 text-emerald-800 dark:bg-emerald-900/30 dark:text-emerald-300'
    : status === 'fail'
      ? 'bg-red-100 text-red-800 dark:bg-red-900/30 dark:text-red-300'
      : 'bg-muted text-muted-foreground'
  const roleClass = role === 'primary'
    ? 'border-blue-200 bg-blue-50 text-blue-800 dark:border-blue-900/50 dark:bg-blue-950/30 dark:text-blue-300'
    : role === 'secondary'
      ? 'border-slate-200 bg-slate-50 text-slate-700 dark:border-slate-800 dark:bg-slate-900/40 dark:text-slate-300'
      : 'border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-200'

  return (
    <div className="rounded-md border bg-card p-3">
      <div className="space-y-3">
        <div className="min-w-0 space-y-2">
          <div className="flex items-start justify-between gap-3">
            <div className="min-w-0 flex items-start gap-2">
              <span className="mt-1 h-2.5 w-2.5 rounded-full flex-none" style={{ backgroundColor: color }} />
              <div className="min-w-0">
                <div className="truncate text-sm font-medium text-foreground">{metricName(metric)}</div>
                <code className="text-[10px] text-muted-foreground">{metric.id}</code>
              </div>
            </div>
            <div className="flex flex-none items-center gap-1">
              <span className={`inline-flex rounded border px-1.5 py-0.5 text-[10px] font-medium ${roleClass}`}>{role}</span>
              <span className={`inline-flex rounded px-1.5 py-0.5 text-[10px] font-medium ${statusClass}`}>{status}</span>
            </div>
          </div>

          {criteria ? (
            <div className="rounded border bg-background/60 px-2 py-1.5 text-xs text-foreground">
              <span className="font-medium">Success criteria:</span> {criteria}
            </div>
          ) : (
            <div className="flex items-start gap-1.5 rounded border border-amber-200 bg-amber-50 px-2 py-1.5 text-xs text-amber-900 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-200">
              <AlertTriangle className="mt-0.5 h-3.5 w-3.5 flex-none" />
              <span>Not linked to a success criteria in <code>planning/metrics.json</code>.</span>
            </div>
          )}

          <div className="grid gap-2 text-xs text-muted-foreground sm:grid-cols-2 xl:grid-cols-4">
            <div>
              <div className="text-[10px] uppercase tracking-wide">Latest</div>
              <div className="tabular-nums text-foreground">{latest?.has_value ? `${formatNumber(latest.value)} ${metric.unit}` : '—'}</div>
            </div>
            <div>
              <div className="text-[10px] uppercase tracking-wide">Threshold</div>
              <div className="text-foreground">{metricThresholdText(metric)}</div>
            </div>
            <div>
              <div className="text-[10px] uppercase tracking-wide">Category</div>
              <div className="text-foreground">{category}</div>
            </div>
            <div>
              <div className="text-[10px] uppercase tracking-wide">Source</div>
              <div className="truncate text-foreground" title={source}>{source}</div>
            </div>
          </div>

          {issue && (
            <div className="rounded border border-amber-200 bg-amber-50 px-2 py-1.5 text-xs text-amber-900 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-200">
              {issue.latestMissing?.resolve_error ? (
                <span><span className="font-medium">Latest value missing:</span> {issue.latestMissing.resolve_error}</span>
              ) : issue.lastError?.resolve_error ? (
                <span>{issue.missingCount} no-value row{issue.missingCount === 1 ? '' : 's'}; latest resolver error: {issue.lastError.resolve_error}</span>
              ) : (
                <span>{issue.missingCount} no-value row{issue.missingCount === 1 ? '' : 's'} without resolver detail.</span>
              )}
            </div>
          )}

          {!issue && historicalIssue && (
            <div className="text-xs text-muted-foreground">
              Earlier history has {historicalIssue.missingCount} untracked value{historicalIssue.missingCount === 1 ? '' : 's'}; latest snapshot resolves.
            </div>
          )}
        </div>

        <div className="min-w-0">
          <MetricTrajectoryChart metric={metric} history={history} color={color} />
        </div>
      </div>
    </div>
  )
}

const MetricsPanel: React.FC<{ metrics: Metric[]; history: MetricSnapshotRow[] }> = ({ metrics, history }) => {
  const currentIssues = useMemo(() => {
    return metrics
      .map((metric) => metricCurrentIssue(metric, history))
      .filter((issue): issue is MetricHistoryIssue => issue !== null)
  }, [metrics, history])
  const missingLinkedCriteria = metrics.filter((metric) => !metricSuccessCriteria(metric)).length
  const missingRoles = metrics.filter((metric) => metricRole(metric) === 'uncategorized').length
  const missingCategories = metrics.filter((metric) => metricCategory(metric) === 'uncategorized').length
  const metricSections = useMemo(() => {
    const primary = metrics.filter((metric) => metricRole(metric) === 'primary')
    const secondary = metrics.filter((metric) => metricRole(metric) === 'secondary')
    const uncategorized = metrics.filter((metric) => metricRole(metric) === 'uncategorized')
    return [
      { key: 'primary', title: 'Primary Metrics', description: 'North-star and must-not-break signals the optimizer should care about first.', items: primary },
      { key: 'secondary', title: 'Secondary Metrics', description: 'Diagnostics, guardrails, and supporting signals that explain primary movement.', items: secondary },
      { key: 'uncategorized', title: 'Uncategorized Metrics', description: 'Metrics missing role metadata in planning/metrics.json.', items: uncategorized },
    ].filter((section) => section.items.length > 0)
  }, [metrics])

  if (metrics.length === 0) {
    return <p className="text-sm text-muted-foreground">No metrics defined yet. Run <code>/define-success</code> in optimizer mode to bootstrap.</p>
  }

  return (
    <div className="space-y-3">
      {missingLinkedCriteria > 0 && (
        <div className="flex items-start gap-2 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-900 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-200">
          <AlertTriangle className="mt-0.5 h-4 w-4 flex-none" />
          <div>
            <div className="font-medium">{missingLinkedCriteria} metric{missingLinkedCriteria === 1 ? ' is' : 's are'} not linked to success criteria.</div>
            <div className="mt-0.5 text-amber-800/80 dark:text-amber-100/80">
              Add <code>success_criteria</code> in <code>planning/metrics.json</code> so every number is anchored to a user outcome.
            </div>
          </div>
        </div>
      )}

      {(missingRoles > 0 || missingCategories > 0) && (
        <div className="flex items-start gap-2 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-900 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-200">
          <AlertTriangle className="mt-0.5 h-4 w-4 flex-none" />
          <div>
            <div className="font-medium">Metric organization metadata is incomplete.</div>
            <div className="mt-0.5 text-amber-800/80 dark:text-amber-100/80">
              {missingRoles} missing role; {missingCategories} missing category. Add <code>role</code> and <code>category</code> so primary signals stay separate from diagnostics.
            </div>
          </div>
        </div>
      )}

      {currentIssues.length > 0 && (
        <div className="flex items-start gap-2 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-900 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-200">
          <AlertTriangle className="mt-0.5 h-4 w-4 flex-none" />
          <div>
            <div className="font-medium">{currentIssues.length} metric{currentIssues.length === 1 ? ' is' : 's are'} not tracked in the latest snapshot.</div>
            <div className="mt-0.5 text-amber-800/80 dark:text-amber-100/80">
              Latest resolver errors are shown inline on the affected metric. Missing values are not plotted in the trajectory.
            </div>
          </div>
        </div>
      )}

      {history.length === 0 && (
        <div className="rounded-md border border-dashed px-3 py-2 text-xs text-muted-foreground">
          No metric history yet. Completed workflow runs will add points from <code>db/metrics_history.jsonl</code>.
        </div>
      )}

      <div className="space-y-5">
        {metricSections.map((section) => (
          <section key={section.key} className="space-y-2">
            <div>
              <h3 className="text-sm font-semibold text-foreground">{section.title} ({section.items.length})</h3>
              <p className="text-xs text-muted-foreground">{section.description}</p>
            </div>
            <div className="space-y-3">
              {section.items.map((metric, index) => {
                const globalIndex = metrics.findIndex((candidate) => candidate.id === metric.id)
                return (
                  <MetricCard
                    key={metric.id}
                    metric={metric}
                    history={history}
                    color={TRAJECTORY_COLORS[(globalIndex >= 0 ? globalIndex : index) % TRAJECTORY_COLORS.length]}
                  />
                )
              })}
            </div>
          </section>
        ))}
      </div>
    </div>
  )
}

interface BuilderDocPanelProps {
  which: BuilderDocKind
  doc: BuilderDoc | null
  loading: boolean
  error: string | null
  onRefresh: () => void
  archiveFiles?: BuilderDocArchiveFile[]
  selectedPath?: string
  onSelectPath?: (path: string) => void
}

const BuilderDocPanel: React.FC<BuilderDocPanelProps> = ({ which, doc, loading, error, onRefresh, archiveFiles = [], selectedPath, onSelectPath }) => {
  const copy = {
    soul: {
      title: 'Soul',
      blurb: 'The workflow north star. Optimizer treats this as the source of truth for objective and success criteria; metrics, reviews, and plans are judged against it.',
      emptyHint: 'soul/soul.md is missing. Define ## Objective and ## Success Criteria before relying on metrics or improvement.',
    },
    improve: {
      title: 'Improve log',
      blurb: 'The optimizer agent\'s durable improvement ledger. Slash commands like /improve-evaluation, /improve-workflow, and /optimize-* read this on the way in and append structured decision blocks here.',
      emptyHint: 'No entries yet. Run /define-success to bootstrap it, then use /improve-* commands for ongoing changes.',
    },
    review: {
      title: 'Review log',
      blurb: 'The reviewer agent\'s findings log. /review-* slash commands append dated entries with severity-ordered findings and follow-ups (REVIEW = recommend, not apply).',
      emptyHint: 'No entries yet. Run any /review-* slash command to append the first entry.',
    },
  }[which]
  const showFileMenu = (which === 'improve' || which === 'review') && !!onSelectPath
  const activePath = selectedPath || doc?.path || ''
  const currentPath = which === 'review' ? 'builder/review.html' : 'builder/improve.html'
  const currentLabel = which === 'review' ? 'Current review' : 'Current ledger'
  const filesLabel = which === 'review' ? 'Review files' : 'Improve files'
  const fileOptions = [{ path: currentPath, label: currentLabel }, ...archiveFiles]

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
      {showFileMenu && (
        <div className="flex items-center justify-between gap-3 rounded-md border bg-card px-3 py-2">
          <div className="min-w-0">
            <div className="text-[10px] uppercase tracking-wide text-muted-foreground font-medium">{filesLabel}</div>
            <div className="truncate text-xs text-muted-foreground">{activePath || currentPath}</div>
          </div>
          <select
            value={activePath || currentPath}
            onChange={(event) => onSelectPath?.(event.target.value)}
            className="min-w-[180px] max-w-[280px] rounded-md border bg-background px-2 py-1 text-xs text-foreground focus:outline-none focus:ring-2 focus:ring-primary/40"
            title={activePath || currentPath}
          >
            {fileOptions.map((file) => (
              <option key={file.path} value={file.path}>
                {file.label}
              </option>
            ))}
            {archiveFiles.length === 0 && (
              <option value="__no_archives" disabled>
                No archive files
              </option>
            )}
          </select>
        </div>
      )}
      <div className="min-w-0">
        {doc && !doc.exists && (
          <div className="border border-dashed rounded-md p-4 text-sm text-muted-foreground">
            {copy.emptyHint}
          </div>
        )}
        {doc && doc.exists && (
          <div className="border rounded-md p-3 bg-card">
            {doc.content.trimStart().startsWith('<!DOCTYPE') || doc.content.trimStart().startsWith('<html') ? (
              <div className="h-[70vh]">
                <HtmlRenderer content={doc.content} />
              </div>
            ) : (
              <MarkdownRenderer
                content={doc.content}
                disablePathLinking
                className="!text-[12px] leading-relaxed [&_p]:!text-[12px] [&_li]:!text-[12px] [&_h1]:!text-base [&_h2]:!text-sm [&_h3]:!text-xs [&_h1]:mt-3 [&_h2]:mt-3 [&_h3]:mt-2 [&_p]:my-1.5 [&_ul]:my-1.5 [&_ol]:my-1.5 [&_code]:!text-[11px] [&_pre]:!text-[11px]"
              />
            )}
          </div>
        )}
      </div>
    </div>
  )
}

const AutoImprovementPopup: React.FC<AutoImprovementPopupProps> = ({ isOpen, onClose, workspacePath, selectedRunFolder }) => {
  const [tab, setTab] = useState<Tab>('metrics')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [metrics, setMetrics] = useState<Metric[]>([])
  const [metricsHistory, setMetricsHistory] = useState<MetricSnapshotRow[]>([])
  const [improveDoc, setImproveDoc] = useState<BuilderDoc | null>(null)
  const [reviewDoc, setReviewDoc] = useState<BuilderDoc | null>(null)
  const [soulDoc, setSoulDoc] = useState<BuilderDoc | null>(null)
  const [improveArchiveFiles, setImproveArchiveFiles] = useState<BuilderDocArchiveFile[]>([])
  const [reviewArchiveFiles, setReviewArchiveFiles] = useState<BuilderDocArchiveFile[]>([])
  const [selectedImprovePath, setSelectedImprovePath] = useState('builder/improve.html')
  const [selectedReviewPath, setSelectedReviewPath] = useState('builder/review.html')
  const [docLoading, setDocLoading] = useState<Record<BuilderDocKind, boolean>>(emptyDocLoadingState)
  const [docError, setDocError] = useState<Record<BuilderDocKind, string | null>>(emptyDocErrorState)
  const docRequestSeq = useRef<Record<BuilderDocKind, number>>({ soul: 0, improve: 0, review: 0 })
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
      const [m, h, mh] = await Promise.all([
        agentApi.getAutoImprovementMetrics(workspacePath).catch((err) => ({ success: false, error: String(err), file: undefined })),
        agentApi.getFrameworkHealth(workspacePath).catch((err) => ({ success: false, error: String(err), soul_exists: false, objective_ok: false, success_criteria_ok: false })),
        agentApi.getMetricsHistory(workspacePath).catch((err) => ({ success: false, rows: [], error: String(err) })),
      ])
      if (m.success && m.file) {
        setMetrics(Array.isArray(m.file.metrics) ? m.file.metrics : [])
      } else {
        setMetrics([])
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
      const errs = [m.error, h.error, mh.error].filter(Boolean)
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

  const fetchDoc = useCallback(async (which: BuilderDocKind, filePath?: string) => {
    if (!workspacePath) return
    const requestSeq = docRequestSeq.current[which] + 1
    docRequestSeq.current[which] = requestSeq
    setDocLoading((prev) => ({ ...prev, [which]: true }))
    setDocError((prev) => ({ ...prev, [which]: null }))
    try {
      const res = await agentApi.getBuilderDoc(workspacePath, which, filePath)
      if (docRequestSeq.current[which] !== requestSeq) return
      const payload = { exists: !!res.exists, content: res.content || '', path: res.path || '' }
      if (which === 'soul') setSoulDoc(payload)
      else if (which === 'improve') setImproveDoc(payload)
      else setReviewDoc(payload)
      if (!res.success && res.error) setDocError((prev) => ({ ...prev, [which]: res.error || null }))
    } catch (err) {
      if (docRequestSeq.current[which] === requestSeq) {
        setDocError((prev) => ({ ...prev, [which]: err instanceof Error ? err.message : String(err) }))
      }
    } finally {
      if (docRequestSeq.current[which] === requestSeq) {
        setDocLoading((prev) => ({ ...prev, [which]: false }))
      }
    }
  }, [workspacePath])

  const fetchDocArchives = useCallback(async (which: 'improve' | 'review') => {
    if (!workspacePath) return
    try {
      const res = await agentApi.getBuilderDocArchives(workspacePath, which)
      if (which === 'improve') setImproveArchiveFiles(res.success ? res.files : [])
      else setReviewArchiveFiles(res.success ? res.files : [])
      if (!res.success && res.error) setDocError((prev) => ({ ...prev, [which]: res.error || null }))
    } catch (err) {
      setDocError((prev) => ({ ...prev, [which]: err instanceof Error ? err.message : String(err) }))
    }
  }, [workspacePath])

  // Bust cached docs whenever the workspace switches or the popup re-opens.
  useEffect(() => {
    docRequestSeq.current = {
      soul: docRequestSeq.current.soul + 1,
      improve: docRequestSeq.current.improve + 1,
      review: docRequestSeq.current.review + 1,
    }
    setSoulDoc(null)
    setImproveDoc(null)
    setReviewDoc(null)
    setImproveArchiveFiles([])
    setReviewArchiveFiles([])
    setSelectedImprovePath('builder/improve.html')
    setSelectedReviewPath('builder/review.html')
    setDocLoading(emptyDocLoadingState())
    setDocError(emptyDocErrorState())
  }, [workspacePath, isOpen])

  useEffect(() => {
    if (!isOpen || !workspacePath) return
    fetchDoc('improve')
    fetchDoc('review')
    fetchDocArchives('improve')
    fetchDocArchives('review')
  }, [isOpen, workspacePath, fetchDoc, fetchDocArchives])

  useEffect(() => {
    if (!isOpen || !workspacePath) return
    if (tab === 'soul' && soulDoc === null) fetchDoc('soul')
    if (tab === 'improve') {
      fetchDocArchives('improve')
      if (improveDoc === null || improveDoc.path !== selectedImprovePath) {
        fetchDoc('improve', selectedImprovePath === 'builder/improve.html' ? undefined : selectedImprovePath)
      }
    }
    if (tab === 'review') {
      fetchDocArchives('review')
      if (reviewDoc === null || reviewDoc.path !== selectedReviewPath) {
        fetchDoc('review', selectedReviewPath === 'builder/review.html' ? undefined : selectedReviewPath)
      }
    }
  }, [isOpen, workspacePath, tab, soulDoc, improveDoc, selectedImprovePath, reviewDoc, selectedReviewPath, fetchDoc, fetchDocArchives])

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
                { id: 'evaluation', icon: BarChart3, label: 'Evaluation' },
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
              <MetricsPanel
                metrics={metrics}
                history={metricsHistory}
              />
            )}

            {tab === 'evaluation' && (
              <EvaluationReportsPanel
                workspacePath={workspacePath}
                selectedRunFolder={selectedRunFolder || null}
                isActive={isOpen && tab === 'evaluation'}
              />
            )}

            {(tab === 'soul' || tab === 'improve' || tab === 'review') && (
              <BuilderDocPanel
                which={tab}
                doc={tab === 'soul' ? soulDoc : tab === 'improve' ? improveDoc : reviewDoc}
                loading={docLoading[tab]}
                error={docError[tab]}
                onRefresh={() => {
                  if (tab === 'improve') fetchDocArchives('improve')
                  if (tab === 'review') fetchDocArchives('review')
                  const selectedPath = tab === 'improve'
                    ? selectedImprovePath
                    : tab === 'review'
                      ? selectedReviewPath
                      : ''
                  const rootPath = tab === 'improve'
                    ? 'builder/improve.html'
                    : tab === 'review'
                      ? 'builder/review.html'
                      : ''
                  fetchDoc(tab, selectedPath && selectedPath !== rootPath ? selectedPath : undefined)
                }}
                archiveFiles={tab === 'improve' ? improveArchiveFiles : tab === 'review' ? reviewArchiveFiles : undefined}
                selectedPath={tab === 'improve' ? selectedImprovePath : tab === 'review' ? selectedReviewPath : undefined}
                onSelectPath={tab === 'improve' ? (path) => {
                  setSelectedImprovePath(path)
                  fetchDoc('improve', path === 'builder/improve.html' ? undefined : path)
                } : tab === 'review' ? (path) => {
                  setSelectedReviewPath(path)
                  fetchDoc('review', path === 'builder/review.html' ? undefined : path)
                } : undefined}
              />
            )}
          </div>
        </div>
      </div>
    </ModalPortal>
  )
}

export default AutoImprovementPopup
