// Runs widget — workflow run-folder summary view. Reads from the run-folders
// API; renders summary card, duration chart, status chart, and a per-run
// table with status-based row coloring.

import { useMemo } from 'react'
import type { ReportWidget, RunFoldersResponse } from '../../../services/api-types'
import { formatNamed } from '../../../lib/reportFormatters'
import { WidgetError, WidgetHeader } from './shared'
import { useCompactWidgetLayout } from './tableHelpers'
import {
  formatRuntimeDuration,
  parseTimestamp,
} from './costSummaries'
import {
  makeSyntheticChartWidget,
  makeSyntheticTableWidget,
} from './widgetFactories'
import { TableWidget } from './TableWidget'
import { ChartWidget } from './ChartWidget'

export function RunsWidget({
  widget,
  runsData,
  loading,
  error,
}: {
  widget: ReportWidget
  runsData: RunFoldersResponse | null
  loading: boolean
  error: string | null
}) {
  const [summaryRef, isCompact] = useCompactWidgetLayout()
  const view = widget.runsView ?? 'summary'
  const now = Date.now()

  const selectedRuns = useMemo(() => {
    const runs = (runsData?.folders ?? [])
      .filter(entry => {
        if (!widget.group) return true
        return entry.name === widget.group || entry.name.endsWith(`/${widget.group}`)
      })
      .sort((a, b) => {
        const aTime = parseTimestamp(a.metadata?.created_at) ?? 0
        const bTime = parseTimestamp(b.metadata?.created_at) ?? 0
        return bTime - aTime || a.name.localeCompare(b.name)
      })

    if (!widget.runFolder) return runs
    if (widget.runFolder === 'latest') return runs.length > 0 ? [runs[0]] : []
    return runs.filter(entry => entry.name === widget.runFolder)
  }, [runsData, widget.group, widget.runFolder])

  const normalisedRuns = useMemo(() => {
    return selectedRuns.map(run => {
      const startedAt = run.metadata?.started_at ?? run.metadata?.created_at ?? ''
      const completedAt = run.metadata?.completed_at ?? ''
      const startedMs = parseTimestamp(startedAt)
      const completedMs = parseTimestamp(completedAt)
      const explicitDurationMs = run.metadata?.duration_ms
      const durationMs = explicitDurationMs != null
        ? Math.max(0, explicitDurationMs)
        : startedMs == null
          ? 0
          : Math.max(0, (completedMs ?? now) - startedMs)
      return {
        run,
        run_folder: run.name,
        status: run.metadata?.status ?? 'unknown',
        triggered_by: run.metadata?.triggered_by ?? '',
        created_at: startedAt,
        completed_at: completedAt,
        duration_ms: durationMs,
        duration_minutes: durationMs / 60000,
        duration_label: formatRuntimeDuration(durationMs),
      }
    })
  }, [now, selectedRuns])

  const summary = useMemo(() => {
    if (normalisedRuns.length === 0) return null
    const completed = normalisedRuns.filter(run => run.status === 'completed').length
    const running = normalisedRuns.filter(run => run.status === 'running').length
    const failed = normalisedRuns.filter(run => run.status === 'failed').length
    const totalDurationMs = normalisedRuns.reduce((sum, run) => sum + run.duration_ms, 0)
    return {
      totalRuns: normalisedRuns.length,
      completed,
      running,
      failed,
      totalDurationMs,
      averageDurationMs: totalDurationMs / normalisedRuns.length,
      latestRun: normalisedRuns[0] ?? null,
    }
  }, [normalisedRuns])

  const summaryCards = useMemo(() => {
    if (!summary) return []
    const latestRunAt =
      summary.latestRun?.created_at ||
      summary.latestRun?.completed_at ||
      null
    return [
      { label: 'Last Run', value: formatNamed(latestRunAt, 'datetime').text },
    ]
  }, [summary])

  const durationChartRows = useMemo(() => {
    return normalisedRuns.map(run => ({
      label: run.run_folder.split('/').slice(-2).join(' / ') || run.run_folder,
      value: Number(run.duration_minutes.toFixed(2)),
    }))
  }, [normalisedRuns])

  const statusChartRows = useMemo(() => {
    if (!summary) return []
    return [
      { label: 'Completed', value: summary.completed },
      { label: 'Running', value: summary.running },
      { label: 'Failed', value: summary.failed },
    ].filter(row => row.value > 0)
  }, [summary])

  const runRows = useMemo(() => {
    return normalisedRuns.map(run => ({
      run_folder: run.run_folder,
      status: run.status,
      triggered_by: run.triggered_by,
      created_at: run.created_at,
      completed_at: run.completed_at,
      duration: run.duration_label,
      duration_minutes: Number(run.duration_minutes.toFixed(2)),
    }))
  }, [normalisedRuns])

  if (loading) {
    return (
      <div className="flex flex-col gap-3">
        <WidgetHeader widget={widget} />
        <div className="rounded-xl border border-border/60 bg-background/70 px-3 py-3 text-sm text-muted-foreground">
          Loading workflow runs…
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
          hint="Runs widgets read workspace run-folder metadata from the workflow API."
          showWidgetMeta={false}
        />
      </div>
    )
  }

  if (!summary || normalisedRuns.length === 0) {
    return (
      <div className="flex flex-col gap-3">
        <WidgetHeader widget={widget} />
        <WidgetError
          widget={widget}
          message="No workflow runs are available for the selected scope."
          hint="Run the workflow first, or remove the run/group filter so the widget can see existing runs."
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
          <div className={`grid gap-2.5 ${isCompact ? 'grid-cols-1' : 'grid-cols-1'}`}>
            {summaryCards.map(card => (
              <div key={card.label} className={isCompact ? 'rounded-xl bg-card/70 px-3 py-2.5' : 'rounded-2xl border border-border/60 bg-gradient-to-br from-card via-card to-muted/20 px-3 py-2.5 shadow-sm'}>
                <div className="text-[10px] font-semibold uppercase tracking-[0.18em] text-muted-foreground">{card.label}</div>
                <div className="mt-1 text-xl font-semibold tracking-tight text-foreground">{card.value}</div>
              </div>
            ))}
          </div>
        </>
      )}
      {view === 'duration-chart' && (
        <ChartWidget
          value={durationChartRows}
          widget={makeSyntheticChartWidget({
            chartType: 'bar',
            xAxis: 'label',
            yAxis: 'value',
          })}
        />
      )}
      {view === 'status-chart' && (
        <ChartWidget
          value={statusChartRows}
          widget={makeSyntheticChartWidget({
            chartType: 'bar',
            xAxis: 'label',
            yAxis: 'value',
          })}
        />
      )}
      {view === 'table' && (
        <TableWidget
          value={runRows}
          widget={makeSyntheticTableWidget({
            formats: {
              created_at: 'datetime',
              completed_at: 'datetime',
              duration_minutes: 'number-2dp',
            },
            defaultSort: { field: 'created_at', direction: 'desc' },
            colorBy: 'status',
            colorMap: {
              completed: '#10b981',
              running: '#3b82f6',
              failed: '#ef4444',
              unknown: '#6b7280',
            },
            hideColumns: ['duration_minutes'],
          })}
        />
      )}
      {view !== 'summary' && view !== 'duration-chart' && view !== 'status-chart' && view !== 'table' && (
        <WidgetError widget={widget} message={`Unsupported runs view "${view}".`} severity="info" showWidgetMeta={false} />
      )}
    </div>
  )
}
