// Evaluations widget — workflow eval-score view. Reads from the evaluation
// reports API (not from db/), so source/path are ignored. Renders summary
// cards, a per-run bar chart, EvalRunCards (rich cards with score band /
// progress bar), and a per-step table.

import { useMemo, useState } from 'react'
import { ChevronLeft, ChevronRight, Download, Search } from 'lucide-react'
import type { EvaluationReportsResponse, ReportEvalsMetric, ReportWidget } from '../../../services/api-types'
import { formatNamed, rowsToCSV } from '../../../lib/reportFormatters'
import { evalScoreTone, scoreTones, scoreTier } from '../../../lib/reportTokens'
import { WidgetError, WidgetHeader } from './shared'
import {
  DEFAULT_TABLE_PAGE_SIZE,
  useCompactWidgetLayout,
} from './tableHelpers'
import {
  makeSyntheticChartWidget,
  makeSyntheticTableWidget,
} from './widgetFactories'
import { TableWidget } from './TableWidget'
import { ChartWidget } from './ChartWidget'

const DEFAULT_EVAL_WIDGET_RUN_LIMIT = 3

const evalScoreBarClass = (scorePercentage: number) => scoreTones[scoreTier(scorePercentage)].barClassName

export function EvalsWidget({
  widget,
  evalsData,
  loading,
  error,
}: {
  widget: ReportWidget
  evalsData: EvaluationReportsResponse | null
  loading: boolean
  error: string | null
}) {
  const [summaryRef, isCompact] = useCompactWidgetLayout()
  const view = widget.evalsView ?? 'summary'
  const metric: ReportEvalsMetric = widget.evalsMetric ?? 'score_percentage'

  const selectedReports = useMemo(() => {
    const reports = (evalsData?.reports ?? [])
      .filter(entry => {
        if (!widget.group) return true
        return entry.run_folder === widget.group || entry.run_folder.endsWith(`/${widget.group}`)
      })
      .sort((a, b) => {
        const aTime = new Date(a.report.generated_at).getTime()
        const bTime = new Date(b.report.generated_at).getTime()
        return bTime - aTime || b.report.score_percentage - a.report.score_percentage || a.run_folder.localeCompare(b.run_folder)
      })

    if (!widget.runFolder) return reports.slice(0, DEFAULT_EVAL_WIDGET_RUN_LIMIT)
    if (widget.runFolder === 'latest') return reports.length > 0 ? [reports[0]] : []
    return reports.filter(entry => entry.run_folder === widget.runFolder)
  }, [evalsData, widget.group, widget.runFolder])

  const summary = useMemo(() => {
    if (selectedReports.length === 0) return null
    const totals = selectedReports.reduce((acc, entry) => {
      acc.totalScore += entry.report.total_score
      acc.totalMaxScore += entry.report.max_possible_score
      acc.totalSteps += entry.report.step_scores.filter(step => !step.skipped).length
      return acc
    }, {
      totalScore: 0,
      totalMaxScore: 0,
      totalSteps: 0,
    })
    const percentages = selectedReports.map(entry => entry.report.score_percentage)
    return {
      totalRuns: selectedReports.length,
      averagePercentage: percentages.reduce((acc, value) => acc + value, 0) / percentages.length,
      highestPercentage: Math.max(...percentages),
      lowestPercentage: Math.min(...percentages),
      totalScore: totals.totalScore,
      totalMaxScore: totals.totalMaxScore,
      totalSteps: totals.totalSteps,
    }
  }, [selectedReports])

  const runChartRows = useMemo(() => {
    return selectedReports.map(entry => ({
      label: entry.run_folder.split('/').slice(-2).join(' / ') || entry.run_folder,
      value: metric === 'total_score' ? entry.report.total_score : entry.report.score_percentage,
    }))
  }, [metric, selectedReports])

  const runRows = useMemo(() => {
    return selectedReports.map(entry => ({
      run_folder: entry.run_folder,
      generated_at: entry.report.generated_at,
      total_score: entry.report.total_score,
      max_possible_score: entry.report.max_possible_score,
      score_percentage: entry.report.score_percentage,
      step_count: entry.report.step_scores.length,
      score_band: evalScoreTone(entry.report.score_percentage).label,
    }))
  }, [selectedReports])

  const stepRows = useMemo(() => {
    return selectedReports.flatMap(entry =>
      entry.report.step_scores.filter(step => !step.skipped).map(step => ({
        run_folder: entry.run_folder,
        generated_at: entry.report.generated_at,
        step_id: step.step_id,
        score: step.score,
        max_score: step.max_score,
        score_percentage: step.max_score > 0 ? (step.score / step.max_score) * 100 : 0,
        reasoning: step.reasoning,
        evidence: step.evidence,
      }))
    )
      .sort((a, b) => a.score_percentage - b.score_percentage || a.run_folder.localeCompare(b.run_folder) || a.step_id.localeCompare(b.step_id))
  }, [selectedReports])

  const summaryCards = useMemo(() => {
    if (!summary) return []
    return [
      { label: 'Average Score', value: `${summary.averagePercentage.toFixed(1)}%` },
      { label: 'Highest Score', value: `${summary.highestPercentage.toFixed(1)}%` },
      { label: 'Lowest Score', value: `${summary.lowestPercentage.toFixed(1)}%` },
      { label: 'Runs', value: String(summary.totalRuns) },
    ]
  }, [summary])

  if (loading) {
    return (
      <div className="flex flex-col gap-3">
        <WidgetHeader widget={widget} />
        <div className="rounded-xl border border-border/60 bg-background/70 px-3 py-3 text-sm text-muted-foreground">
          Loading evaluation reports…
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex flex-col gap-3">
        <WidgetHeader widget={widget} />
        <WidgetError
          widget={widget}
          message={error}
          hint="Evals widgets read aggregated data from the evaluation reports API."
          showWidgetMeta={false}
        />
      </div>
    )
  }

  if (!summary || selectedReports.length === 0) {
    return (
      <div className="flex flex-col gap-3">
        <WidgetHeader widget={widget} />
        <WidgetError
          widget={widget}
          message="No evaluation reports are available for the selected scope."
          hint="Run evaluation first, or remove the run/group filter so the widget can see existing reports."
          severity="info"
          showWidgetMeta={false}
        />
      </div>
    )
  }

  return (
    <div ref={summaryRef} className="flex flex-col gap-3">
      <WidgetHeader widget={widget} />
      {view === 'summary' && (
        <>
          <div className={`grid gap-2.5 ${isCompact ? 'grid-cols-1' : 'grid-cols-1 sm:grid-cols-2 xl:grid-cols-4'}`}>
            {summaryCards.map(card => (
              <div key={card.label} className={isCompact ? 'rounded-xl bg-card/70 px-3 py-2.5' : 'rounded-2xl border border-border/60 bg-gradient-to-br from-card via-card to-muted/20 px-3 py-2.5 shadow-sm'}>
                <div className="text-[10px] font-semibold uppercase tracking-[0.18em] text-muted-foreground">{card.label}</div>
                <div className="mt-1 text-xl font-semibold tracking-tight text-foreground">{card.value}</div>
              </div>
            ))}
          </div>
        </>
      )}
      {view === 'run-chart' && (
        <ChartWidget
          value={runChartRows}
          widget={makeSyntheticChartWidget({
            chartType: 'bar',
            xAxis: 'label',
            yAxis: 'value',
          })}
        />
      )}
      {view === 'run-table' && (
        <EvalRunCards
          rows={runRows}
          pageSize={widget.pageSize ?? DEFAULT_TABLE_PAGE_SIZE}
          compact={isCompact}
        />
      )}
      {view === 'step-table' && (
        <TableWidget
          value={stepRows}
          widget={makeSyntheticTableWidget({
            formats: {
              generated_at: 'datetime',
              score: 'number',
              max_score: 'number',
              score_percentage: 'number-1dp',
            },
            defaultSort: { field: 'generated_at', direction: 'desc' },
          })}
        />
      )}
      {view !== 'summary' && view !== 'run-chart' && view !== 'run-table' && view !== 'step-table' && (
        <WidgetError widget={widget} message={`Unsupported evals view "${view}".`} severity="info" showWidgetMeta={false} />
      )}
    </div>
  )
}

function EvalRunCards({
  rows,
  pageSize,
  compact,
}: {
  rows: Array<{
    run_folder: string
    generated_at: string
    total_score: number
    max_possible_score: number
    score_percentage: number
    step_count: number
    score_band: string
  }>
  pageSize: number
  compact: boolean
}) {
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(0)

  const filteredRows = useMemo(() => {
    const needle = search.trim().toLowerCase()
    if (!needle) return rows
    return rows.filter(row =>
      row.run_folder.toLowerCase().includes(needle) ||
      row.score_band.toLowerCase().includes(needle) ||
      formatNamed(row.generated_at, 'datetime').text.toLowerCase().includes(needle),
    )
  }, [rows, search])

  const totalPages = Math.max(1, Math.ceil(filteredRows.length / pageSize))
  const safePage = Math.min(page, totalPages - 1)
  const pageRows = useMemo(
    () => filteredRows.slice(safePage * pageSize, (safePage + 1) * pageSize),
    [filteredRows, safePage, pageSize],
  )

  const rowCountText =
    filteredRows.length === rows.length
      ? `${filteredRows.length} run${filteredRows.length === 1 ? '' : 's'}`
      : `${filteredRows.length} of ${rows.length}`

  const handleExport = () => {
    const csv = rowsToCSV(filteredRows as Record<string, unknown>[], [
      'run_folder',
      'generated_at',
      'total_score',
      'max_possible_score',
      'score_percentage',
      'step_count',
    ])
    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `eval-scores-${new Date().toISOString().replace(/[:.]/g, '-')}.csv`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
  }

  return (
    <div className="flex flex-col gap-2.5">
      <div className="flex flex-col gap-1.5 text-xs">
        <div className="relative w-full">
          <Search className="absolute left-2 top-1.5 h-3.5 w-3.5 text-muted-foreground" />
          <input
            type="text"
            placeholder="Search runs…"
            value={search}
            onChange={e => {
              setSearch(e.target.value)
              setPage(0)
            }}
            className="w-full rounded-md border border-input bg-muted/30 py-1.5 pl-7 pr-2 text-xs focus:outline-none focus:ring-1 focus:ring-primary"
          />
        </div>
        <div className="flex items-center gap-2">
          <div className="inline-flex items-center rounded-full border border-border bg-background/80 px-2 py-1 text-muted-foreground">
            {rowCountText}
          </div>
          <button
            onClick={handleExport}
            className="ml-auto inline-flex items-center gap-1 rounded-md border border-border bg-background/80 px-2 py-1.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
            title="Export CSV"
          >
            <Download className="h-3.5 w-3.5" />
            <span>CSV</span>
          </button>
        </div>
      </div>

      <div className={`grid gap-2.5 ${compact ? 'grid-cols-1' : 'grid-cols-1 md:grid-cols-2'}`}>
        {pageRows.map((row, index) => {
          const tone = evalScoreTone(row.score_percentage)
          const generatedAt = formatNamed(row.generated_at, 'datetime').text
          const clampedPercentage = Math.max(0, Math.min(100, row.score_percentage))
          return (
            <div
              key={`${row.run_folder}:${row.generated_at}:${index}`}
              className={`overflow-hidden rounded-xl border border-border/50 bg-gradient-to-br ${tone.accentClassName} px-3 py-3 shadow-sm`}
            >
              <div className="flex items-start gap-3">
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-semibold text-foreground">
                    {row.run_folder}
                  </div>
                  <div className="mt-0.5 text-xs text-muted-foreground">
                    {generatedAt}
                  </div>
                </div>
                <div className={`inline-flex shrink-0 items-center rounded-full border px-2 py-1 text-[11px] font-semibold ${tone.pillClassName}`}>
                  {row.score_percentage.toFixed(1)}%
                </div>
              </div>

              <div className="mt-3 rounded-xl border border-border/45 bg-background/45 px-3 py-2.5">
                <div className="flex items-center justify-between gap-3">
                  <div className="text-[10px] font-semibold uppercase tracking-[0.16em] text-muted-foreground">
                    Score Progress
                  </div>
                  <div className="text-sm font-semibold tabular-nums text-foreground">
                    {row.total_score}
                    <span className="text-muted-foreground"> / {row.max_possible_score}</span>
                  </div>
                </div>
                <div className="mt-2 h-2 overflow-hidden rounded-full bg-background/80">
                  <div
                    className={`h-full rounded-full transition-[width] duration-300 ${evalScoreBarClass(row.score_percentage)}`}
                    style={{ width: `${clampedPercentage}%` }}
                  />
                </div>
                <div className="mt-1 flex items-center justify-between text-[11px] text-muted-foreground">
                  <span>{row.score_band}</span>
                  <span>{clampedPercentage.toFixed(1)}%</span>
                </div>
              </div>

              <div className={`mt-3 grid gap-2 ${compact ? 'grid-cols-2' : 'grid-cols-3'}`}>
                <div className="rounded-lg bg-background/60 px-2.5 py-2">
                  <div className="text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">
                    Steps
                  </div>
                  <div className="mt-1 text-sm font-semibold tabular-nums text-foreground">
                    {row.step_count}
                  </div>
                </div>
                <div className="rounded-lg bg-background/60 px-2.5 py-2">
                  <div className="text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">
                    Status
                  </div>
                  <div className="mt-1 text-sm font-semibold text-foreground">
                    {row.score_band}
                  </div>
                </div>
                <div className={`rounded-lg bg-background/60 px-2.5 py-2 ${compact ? 'col-span-2' : ''}`}>
                  <div className="text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">
                    Generated
                  </div>
                  <div className="mt-1 truncate text-sm font-medium text-foreground">
                    {generatedAt}
                  </div>
                </div>
              </div>
            </div>
          )
        })}
      </div>

      {totalPages > 1 && (
        <div className="flex flex-wrap items-center justify-end gap-1.5 text-xs text-muted-foreground">
          <button
            onClick={() => setPage(current => Math.max(0, current - 1))}
            disabled={safePage === 0}
            className="inline-flex items-center gap-0.5 rounded-md border border-border bg-background px-2 py-1 disabled:opacity-30 hover:bg-muted hover:text-foreground transition-colors"
            aria-label="Previous page"
          >
            <ChevronLeft className="h-3.5 w-3.5" />
            Prev
          </button>
          <span className="inline-flex items-center rounded-md bg-primary/10 px-2 py-1 font-medium tabular-nums text-primary">
            {safePage + 1}
            <span className="mx-1 opacity-60">/</span>
            {totalPages}
          </span>
          <button
            onClick={() => setPage(current => Math.min(totalPages - 1, current + 1))}
            disabled={safePage >= totalPages - 1}
            className="inline-flex items-center gap-0.5 rounded-md border border-border bg-background px-2 py-1 disabled:opacity-30 hover:bg-muted hover:text-foreground transition-colors"
            aria-label="Next page"
          >
            Next
            <ChevronRight className="h-3.5 w-3.5" />
          </button>
        </div>
      )}
    </div>
  )
}
