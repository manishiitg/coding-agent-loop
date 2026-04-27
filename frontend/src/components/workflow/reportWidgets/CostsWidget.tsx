// Costs widget — workflow-level cost / token-usage view. Reads from the
// workflow costs API (not from db/), so source/path are ignored. Renders
// summary cards, stage breakdown bar chart, run/step/model tables; for
// builder-style flows renders the phase-aggregated variant instead.

import { useMemo } from 'react'
import type { ReportWidget, WorkflowCostsResponse } from '../../../services/api-types'
import { formatNamed } from '../../../lib/reportFormatters'
import { WidgetError, WidgetHeader } from './shared'
import { useCompactWidgetLayout } from './tableHelpers'
import {
  type RunCostSummary,
  aggregateRunCostSummaries,
  formatMetricValue,
  metricLabel,
  metricValue,
  summarisePhaseCosts,
  summariseRunCosts,
} from './costSummaries'
import {
  makeSyntheticChartWidget,
  makeSyntheticTableWidget,
} from './widgetFactories'
import { TableWidget } from './TableWidget'
import { ChartWidget } from './ChartWidget'

const DEFAULT_COST_WIDGET_RUN_LIMIT = 3

export function CostsWidget({
  widget,
  costsData,
  loading,
  error,
}: {
  widget: ReportWidget
  costsData: WorkflowCostsResponse | null
  loading: boolean
  error: string | null
}) {
  const [summaryRef, isCompact] = useCompactWidgetLayout()
  const scope = widget.costsScope ?? 'all'
  const view = widget.costsView ?? 'summary'
  const metric = widget.costsMetric ?? 'cost'
  const runScope = scope === 'phase' ? 'all' : scope

  const phaseSummary = useMemo(() => summarisePhaseCosts(costsData?.phase_token_usage ?? null), [costsData])

  const runSummaries = useMemo(() => {
    if (!costsData?.runs || scope === 'phase') return []
    const filtered = costsData.runs
      .filter(entry => {
        if (!widget.group) return true
        return entry.run_folder === widget.group || entry.run_folder.endsWith(`/${widget.group}`)
      })
      .map(entry => summariseRunCosts(entry.run_folder, entry.token_usage, entry.evaluation_token_usage, runScope))
      .filter((entry): entry is RunCostSummary => entry !== null)
      .sort((a, b) => {
        const aTime = a.updatedAt ? new Date(a.updatedAt).getTime() : 0
        const bTime = b.updatedAt ? new Date(b.updatedAt).getTime() : 0
        return bTime - aTime || b.totalCost - a.totalCost || a.runFolder.localeCompare(b.runFolder)
      })
    if (!widget.runFolder) return filtered.slice(0, DEFAULT_COST_WIDGET_RUN_LIMIT)
    if (widget.runFolder === 'latest') return filtered.length > 0 ? [filtered[0]] : []
    return filtered.filter(entry => entry.runFolder === widget.runFolder)
  }, [costsData, runScope, scope, widget.group, widget.runFolder])

  const aggregateRunSummary = useMemo(() => aggregateRunCostSummaries(runSummaries), [runSummaries])
  const hasAnyRunCosts = (costsData?.runs?.length ?? 0) > 0
  const hasScopedRunCosts = runSummaries.length > 0
  const costsEmptyState = useMemo(() => {
    if (!hasAnyRunCosts) {
      return {
        message: 'No run cost data is available yet.',
        hint: 'Execution and evaluation costs appear after runs have persisted token usage into the workflow costs ledger.',
      }
    }
    if (widget.runFolder || widget.group) {
      return {
        message: 'No run cost data matched the selected filter.',
        hint: 'Adjust the run/group filter, or remove it so the widget can see the available run ledgers.',
      }
    }
    return {
      message: 'No run cost data is available for the selected scope.',
      hint: 'The workflow has cost data, but none of the persisted run ledgers matched this widget scope.',
    }
  }, [hasAnyRunCosts, widget.group, widget.runFolder])

  const summaryCards = useMemo(() => {
    if (scope === 'phase') {
      if (!phaseSummary) return []
      return [
        { label: 'Total Cost', value: formatMetricValue('cost', phaseSummary.totalCost) },
        { label: 'Total Tokens', value: formatMetricValue('total_tokens', phaseSummary.totalTokens) },
        { label: 'LLM Calls', value: formatMetricValue('llm_calls', phaseSummary.totalLLMCalls) },
        { label: 'Phases', value: String(phaseSummary.phaseCosts.length) },
      ]
    }
    if (!aggregateRunSummary) return []
    return [
      { label: 'Total Cost', value: formatMetricValue('cost', aggregateRunSummary.totalCost) },
      { label: 'Total Tokens', value: formatMetricValue('total_tokens', aggregateRunSummary.totalTokens) },
      { label: 'LLM Calls', value: formatMetricValue('llm_calls', aggregateRunSummary.totalLLMCalls) },
      { label: 'Runs', value: String(runSummaries.length) },
    ]
  }, [aggregateRunSummary, phaseSummary, runSummaries.length, scope])

  const recentRunCards = useMemo(() => {
    return runSummaries.slice(0, DEFAULT_COST_WIDGET_RUN_LIMIT).map(run => ({
      runFolder: run.runFolder,
      updatedAt: run.updatedAt,
      value: formatMetricValue(metric, metricValue(metric, run)),
    }))
  }, [metric, runSummaries])

  if (loading) {
    return (
      <div className="flex flex-col gap-3">
        <WidgetHeader widget={widget} />
        <div className="rounded-xl border border-border/60 bg-background/70 px-3 py-3 text-sm text-muted-foreground">
          Loading workflow costs…
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
          hint="Costs widgets read aggregated data from the workflow costs API."
          showWidgetMeta={false}
        />
      </div>
    )
  }

  if (scope === 'phase') {
    if (!phaseSummary) {
      return (
        <div className="flex flex-col gap-3">
          <WidgetHeader widget={widget} />
          <WidgetError
            widget={widget}
            message="No phase cost data is available yet."
            hint="Phase costs come from builder-style workflow sessions and appear after token usage has been persisted."
            severity="info"
            showWidgetMeta={false}
          />
        </div>
      )
    }

    const phaseChartRows = phaseSummary.phaseCosts.map(phase => ({
      label: phase.phaseTitle,
      value: metricValue(metric, phase),
    }))
    const modelRows = phaseSummary.modelCosts.map(model => ({
      model: model.modelID,
      provider: model.provider,
      total_cost: model.totalCost,
      total_tokens: model.totalTokens,
      input_tokens: model.inputTokens,
      output_tokens: model.outputTokens,
      llm_calls: model.llmCalls,
    }))

    return (
      <div ref={summaryRef} className="flex flex-col gap-3">
        <WidgetHeader widget={widget} />
        {view === 'summary' && (
          <>
            <div className={`grid gap-2.5 ${isCompact ? 'grid-cols-1' : 'grid-cols-2 xl:grid-cols-4'}`}>
              {summaryCards.map(card => (
                <div key={card.label} className={isCompact ? 'rounded-xl bg-card/70 px-3 py-2.5' : 'rounded-2xl border border-border/60 bg-gradient-to-br from-card via-card to-muted/20 px-3 py-2.5 shadow-sm'}>
                  <div className="text-[10px] font-semibold uppercase tracking-[0.18em] text-muted-foreground">{card.label}</div>
                  <div className="mt-1 text-xl font-semibold tracking-tight text-foreground">{card.value}</div>
                </div>
              ))}
            </div>
            <div className={isCompact ? 'rounded-xl bg-background/35 px-2.5 py-2.5' : 'rounded-xl border border-border/60 bg-background/55 px-2.5 py-2.5'}>
              <div className="mb-2 text-[11px] font-semibold uppercase tracking-[0.16em] text-muted-foreground">Phase Breakdown</div>
              <div className="flex flex-wrap gap-1.5">
                {phaseSummary.phaseCosts.slice(0, 6).map(phase => (
                  <div key={phase.phaseID} className={isCompact ? 'rounded-full bg-background/60 px-2.5 py-1 text-xs text-foreground' : 'rounded-full border border-border/60 bg-background/80 px-2.5 py-1 text-xs text-foreground'}>
                    {phase.phaseTitle}: <span className="font-medium">{formatMetricValue(metric, metricValue(metric, phase))}</span>
                  </div>
                ))}
              </div>
            </div>
          </>
        )}
        {view === 'stage-breakdown' && (
          <ChartWidget
            value={phaseChartRows}
            widget={makeSyntheticChartWidget({
              chartType: 'bar',
              xAxis: 'label',
              yAxis: 'value',
            })}
          />
        )}
        {view === 'model-table' && (
          <TableWidget
            value={modelRows}
            widget={makeSyntheticTableWidget({
              formats: {
                total_cost: 'currency-usd',
                total_tokens: 'number',
                input_tokens: 'number',
                output_tokens: 'number',
                llm_calls: 'number',
              },
              defaultSort: { field: metric === 'cost' ? 'total_cost' : metric, direction: 'desc' },
            })}
          />
        )}
        {view === 'run-table' || view === 'step-table' ? (
          <WidgetError
            widget={widget}
            message={`"${view}" is not available for phase scope.`}
            hint="Use `summary`, `stage-breakdown`, or `model-table` with `scope: phase`."
            severity="info"
            showWidgetMeta={false}
          />
        ) : null}
        {view !== 'summary' && view !== 'stage-breakdown' && view !== 'model-table' && view !== 'run-table' && view !== 'step-table' && (
          <WidgetError widget={widget} message={`Unsupported costs view "${view}".`} severity="info" showWidgetMeta={false} />
        )}
      </div>
    )
  }

  if (!aggregateRunSummary || !hasScopedRunCosts) {
    return (
      <div className="flex flex-col gap-3">
        <WidgetHeader widget={widget} />
        <WidgetError
          widget={widget}
          message={costsEmptyState.message}
          hint={costsEmptyState.hint}
          severity="info"
          showWidgetMeta={false}
        />
      </div>
    )
  }

  const stageMetricMap = aggregateRunSummary.stepCosts.reduce((acc, step) => {
    const current = acc.get(step.stage) ?? {
      label: step.stage.replace(/_/g, ' '),
      totalCost: 0,
      totalTokens: 0,
      inputTokens: 0,
      outputTokens: 0,
      llmCalls: 0,
    }
    current.totalCost += step.totalCost
    current.totalTokens += step.totalTokens
    current.inputTokens += step.inputTokens
    current.outputTokens += step.outputTokens
    current.llmCalls += step.llmCalls
    acc.set(step.stage, current)
    return acc
  }, new Map<string, {
    label: string
    totalCost: number
    totalTokens: number
    inputTokens: number
    outputTokens: number
    llmCalls: number
  }>())

  const stageMetricRows = Array.from(stageMetricMap.values())
    .map(stage => ({
      label: stage.label,
      value: metricValue(metric, stage),
    }))
    .filter(stage => stage.value > 0)

  const runRows = runSummaries.map(run => ({
    run_folder: run.runFolder,
    updated_at: run.updatedAt ?? '',
    total_cost: run.totalCost,
    total_tokens: run.totalTokens,
    input_tokens: run.totalInputTokens,
    output_tokens: run.totalOutputTokens,
    llm_calls: run.totalLLMCalls,
  }))

  const stepRows = aggregateRunSummary.stepCosts.map(step => ({
    step: step.stepTitle,
    stage: step.stage,
    total_cost: step.totalCost,
    total_tokens: step.totalTokens,
    input_tokens: step.inputTokens,
    output_tokens: step.outputTokens,
    llm_calls: step.llmCalls,
  }))

  const modelRows = aggregateRunSummary.modelCosts.map(model => ({
    model: model.modelID,
    provider: model.provider,
    total_cost: model.totalCost,
    total_tokens: model.totalTokens,
    input_tokens: model.inputTokens,
    output_tokens: model.outputTokens,
    llm_calls: model.llmCalls,
  }))

  return (
    <div ref={summaryRef} className="flex flex-col gap-3">
      <WidgetHeader widget={widget} />
      {view === 'summary' && (
        <>
          <div className={`grid gap-2.5 ${isCompact ? 'grid-cols-1' : 'grid-cols-1 xl:grid-cols-3'}`}>
            {recentRunCards.map(card => (
              <div key={card.runFolder} className={isCompact ? 'rounded-xl bg-card/70 px-3 py-2.5' : 'rounded-2xl border border-border/60 bg-gradient-to-br from-card via-card to-muted/20 px-3 py-2.5 shadow-sm'}>
                <div className="text-[10px] font-semibold uppercase tracking-[0.18em] text-muted-foreground">{metricLabel(metric)}</div>
                <div className="mt-1 truncate text-sm font-medium text-foreground">{card.runFolder}</div>
                <div className="mt-1 text-xl font-semibold tracking-tight text-foreground">{card.value}</div>
                <div className="mt-1 text-xs text-muted-foreground">
                  {formatNamed(card.updatedAt, 'datetime').text}
                </div>
              </div>
            ))}
          </div>
        </>
      )}
      {view === 'stage-breakdown' && (
        <ChartWidget
          value={stageMetricRows}
          widget={makeSyntheticChartWidget({
            chartType: 'bar',
            xAxis: 'label',
            yAxis: 'value',
          })}
        />
      )}
      {view === 'run-table' && (
        <TableWidget
          value={runRows}
          widget={makeSyntheticTableWidget({
            formats: {
              total_cost: 'currency-usd',
              total_tokens: 'number',
              input_tokens: 'number',
              output_tokens: 'number',
              llm_calls: 'number',
              updated_at: 'datetime',
            },
            defaultSort: { field: metric === 'cost' ? 'total_cost' : metric, direction: 'desc' },
          })}
        />
      )}
      {view === 'step-table' && (
        <TableWidget
          value={stepRows}
          widget={makeSyntheticTableWidget({
            formats: {
              total_cost: 'currency-usd',
              total_tokens: 'number',
              input_tokens: 'number',
              output_tokens: 'number',
              llm_calls: 'number',
            },
            defaultSort: { field: metric === 'cost' ? 'total_cost' : metric, direction: 'desc' },
          })}
        />
      )}
      {view === 'model-table' && (
        <TableWidget
          value={modelRows}
          widget={makeSyntheticTableWidget({
            formats: {
              total_cost: 'currency-usd',
              total_tokens: 'number',
              input_tokens: 'number',
              output_tokens: 'number',
              llm_calls: 'number',
            },
            defaultSort: { field: metric === 'cost' ? 'total_cost' : metric, direction: 'desc' },
          })}
        />
      )}
      {view !== 'summary' && view !== 'stage-breakdown' && view !== 'run-table' && view !== 'step-table' && view !== 'model-table' && (
        <WidgetError widget={widget} message={`Unsupported costs view "${view}".`} severity="info" showWidgetMeta={false} />
      )}
    </div>
  )
}
