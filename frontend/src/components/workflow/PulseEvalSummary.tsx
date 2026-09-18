import { Fragment, useCallback, useEffect, useMemo, useState } from 'react'
import { ChevronDown, ChevronRight, Loader2, RefreshCw } from 'lucide-react'
import { agentApi } from '../../services/api'
import type { EvalResultRecord } from '../../services/api-types'
import { WORKFLOW_LOG_REFRESH_EVENT } from './workflowEvents'

/** A step "passes" only once it has a captured score at or above half its max. */
function stepPassed(step: EvalResultRecord): boolean {
  if (step.skipped || !step.score_captured) return false
  return step.max_score <= 0 || step.score >= step.max_score / 2
}

const MAX_TREND_RUNS = 8

interface EvalTimelineStep {
  stepID: string
  title: string
  description: string
  historical: boolean
  resultsByRun: Map<string, EvalResultRecord>
}

function formatScore(value: number): string {
  return Number.isInteger(value) ? String(value) : value.toFixed(1)
}

function formatRunTime(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

export function PulseEvalSummary({
  workspacePath,
  className = '',
  headerAction,
}: {
  workspacePath: string
  className?: string
  headerAction?: React.ReactNode
}) {
  const [results, setResults] = useState<EvalResultRecord[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [expandedStepID, setExpandedStepID] = useState<string | null>(null)
  // Historical criteria (no longer in the current plan) pile up over time and
  // read as noise next to the active ones a user actually cares about right
  // now. Grouped under one collapsed-by-default toggle instead of showing
  // every historical row inline.
  const [showHistorical, setShowHistorical] = useState(false)

  const load = useCallback(async () => {
    if (!workspacePath) return
    setLoading(true)
    setError(null)
    try {
      const response = await agentApi.getPulseEvalResults(workspacePath)
      if (!response.success) throw new Error(response.error || 'Failed to load evaluation results.')
      setResults(response.results || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load evaluation results.')
    } finally {
      setLoading(false)
    }
  }, [workspacePath])

  useEffect(() => { void load() }, [load])
  useEffect(() => {
    const refresh = () => { void load() }
    window.addEventListener(WORKFLOW_LOG_REFRESH_EVENT, refresh)
    return () => window.removeEventListener(WORKFLOW_LOG_REFRESH_EVENT, refresh)
  }, [load])

  const { runs, activeSteps, historicalSteps } = useMemo(() => {
    // Results arrive newest-first; runTimes preserves that insertion order, so
    // slicing the first MAX_TREND_RUNS keeps the most recent runs, newest
    // first -- readers expect the latest run leftmost, like a chat/feed, not
    // buried at the end of a horizontal scroll.
    const runTimes = new Map<string, string>()
    for (const result of results) {
      if (!runTimes.has(result.run_folder)) runTimes.set(result.run_folder, result.generated_at)
    }
    const recentRuns = [...runTimes.entries()]
      .slice(0, MAX_TREND_RUNS)
      .map(([runFolder, generatedAt]) => ({ runFolder, generatedAt }))
    const visibleRunFolders = new Set(recentRuns.map(run => run.runFolder))

    const byStep = new Map<string, EvalTimelineStep>()
    for (const result of results) {
      if (!visibleRunFolders.has(result.run_folder)) continue
      let step = byStep.get(result.step_id)
      if (!step) {
        step = {
          stepID: result.step_id,
          title: result.title || result.step_id,
          description: result.description || '',
          historical: Boolean(result.historical),
          resultsByRun: new Map(),
        }
        byStep.set(result.step_id, step)
      }
      if (!step.resultsByRun.has(result.run_folder)) {
        step.resultsByRun.set(result.run_folder, result)
      }
    }

    const timelineSteps = [...byStep.values()]
    const activeSteps = timelineSteps.filter(step => !step.historical)
    const historicalSteps = timelineSteps.filter(step => step.historical)
    return { runs: recentRuns, activeSteps, historicalSteps }
  }, [results])

  const renderStepRow = (step: EvalTimelineStep) => {
    const expanded = expandedStepID === step.stepID
    return (
      <Fragment key={step.stepID}>
        <tr className={expanded ? 'bg-muted/10' : 'border-b'}>
          <th className="sticky left-0 z-10 w-48 max-w-[12rem] overflow-hidden bg-background px-3 py-2 text-xs font-medium text-foreground">
            <button
              type="button"
              onClick={() => setExpandedStepID(expanded ? null : step.stepID)}
              className={`flex w-full min-w-0 gap-1.5 rounded px-1 py-0.5 text-left hover:bg-muted ${expanded ? 'items-start' : 'items-center'}`}
              aria-expanded={expanded}
              aria-label={`${expanded ? 'Hide' : 'Show'} description for ${step.title}`}
            >
              {expanded
                ? <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                : <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />}
              {/* Expanded: show the full title, wrapped -- truncating the one
                  thing the user opened the row to read defeats the point. */}
              <span className={expanded ? 'min-w-0 break-words' : 'min-w-0 truncate'}>{step.title}</span>
              {step.historical && (
                <span className="ml-auto shrink-0 rounded-full border border-amber-500/30 bg-amber-500/10 px-1.5 py-0.5 text-[8px] font-semibold uppercase tracking-wide text-amber-600 dark:text-amber-400">
                  Historical
                </span>
              )}
            </button>
          </th>
          {runs.map(run => {
            const result = step.resultsByRun.get(run.runFolder)
            const state = !result
              ? 'missing'
              : result.skipped
                ? 'skipped'
                : result.score_captured && stepPassed(result)
                  ? 'passed'
                  : 'failed'
            return (
              <td key={run.runFolder} className="px-3 py-2.5 text-center">
                <span
                  className={`inline-flex min-w-16 justify-center rounded-md px-2 py-1 text-[10px] font-medium ${
                    state === 'passed'
                      ? 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400'
                      : state === 'failed'
                        ? 'bg-red-500/10 text-red-600 dark:text-red-400'
                        : 'bg-muted text-muted-foreground'
                  }`}
                  title={result?.reasoning || result?.evidence || undefined}
                >
                  {!result
                    ? '—'
                    : result.skipped
                      ? 'Skipped'
                      : result.score_captured
                        ? `${formatScore(result.score)} / ${formatScore(result.max_score)}`
                        : 'No score'}
                </span>
              </td>
            )
          })}
        </tr>
        {expanded && (
          <tr className="border-b bg-muted/10">
            <td colSpan={runs.length + 1} className="px-5 py-3 text-xs leading-relaxed text-muted-foreground">
              <span className="font-medium text-foreground">Description:</span>{' '}
              {step.description || (step.historical
                ? 'This evaluation criterion is no longer in the current plan. Its previous results are retained for historical comparison.'
                : 'No description has been provided for this evaluation criterion.')}
            </td>
          </tr>
        )}
      </Fragment>
    )
  }

  return (
    <section className={`min-w-0 overflow-hidden rounded-xl border bg-background ${className}`} aria-label="Evaluation results">
      <div className="flex items-center justify-between border-b px-4 py-3">
        <div className="min-w-0 flex-1">
          <h3 className="text-sm font-semibold text-foreground">Evaluation</h3>
          <p className="mt-0.5 text-[11px] text-muted-foreground">
            {runs.length > 0
              ? runs.length === 1
                ? 'Latest evaluation run'
                : `${runs.length} recent runs · newest first`
              : 'Criterion scores across recent workflow runs'}
          </p>
        </div>
        <div className="ml-auto flex shrink-0 items-center gap-2">
          {headerAction}
          <button
            type="button"
            onClick={() => { void load() }}
            disabled={loading}
            className="rounded p-1.5 text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-50"
            aria-label="Refresh evaluation results"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`} />
          </button>
        </div>
      </div>

      {error ? (
        <div className="m-4 rounded border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive">{error}</div>
      ) : loading && results.length === 0 ? (
        <div className="flex min-h-32 items-center justify-center gap-2 text-xs text-muted-foreground">
          <Loader2 className="h-3.5 w-3.5 animate-spin" /> Loading evaluation results…
        </div>
      ) : activeSteps.length === 0 && historicalSteps.length === 0 ? (
        <div className="flex min-h-32 items-center justify-center text-xs text-muted-foreground">No evaluation results yet.</div>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[560px] table-fixed border-collapse text-left">
            <colgroup>
              <col className="w-48" />
              {runs.map(run => <col key={run.runFolder} className="w-32" />)}
            </colgroup>
            <thead>
              <tr className="border-b">
                <th className="sticky left-0 z-10 w-48 max-w-[12rem] overflow-hidden bg-muted px-4 py-2.5 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">
                  Criterion
                </th>
                {runs.map((run, index) => {
                  const [, ...routeParts] = run.runFolder.split('/')
                  const routeLabel = routeParts.join('/')
                  // Newest run displays leftmost (index 0) but keeps the
                  // highest run number, matching "higher number = more
                  // recent" regardless of which side of the screen it's on.
                  const runLabel = runs.length === 1 ? 'Run' : `Run ${runs.length - index}`
                  return (
                    <th key={run.runFolder} className="bg-muted px-2 py-2.5 text-center align-bottom">
                      <div className="truncate text-[11px] font-semibold text-foreground">{runLabel}</div>
                      {routeLabel && routeLabel !== 'default' && (
                        <div className="truncate text-[9px] font-normal text-muted-foreground" title={routeLabel}>
                          {routeLabel}
                        </div>
                      )}
                      <div className="mt-0.5 whitespace-nowrap text-[9px] font-normal text-muted-foreground">
                        {formatRunTime(run.generatedAt)}
                      </div>
                      {index === 0 && runs.length > 1 && (
                        <div className="mt-1 text-[8px] font-semibold uppercase tracking-wide text-primary">Latest</div>
                      )}
                    </th>
                  )
                })}
              </tr>
            </thead>
            <tbody>
              <tr className="border-b bg-muted/10">
                <th className="sticky left-0 z-10 w-48 max-w-[12rem] overflow-hidden bg-background px-4 py-2.5 text-xs font-semibold text-foreground">Overall</th>
                {runs.map(run => {
                  const runResults = [...activeSteps, ...historicalSteps]
                    .map(step => step.resultsByRun.get(run.runFolder))
                    .filter((result): result is EvalResultRecord => Boolean(result && !result.skipped))
                  const captured = runResults.filter(result => result.score_captured)
                  const score = captured.reduce((total, result) => total + result.score, 0)
                  const maxScore = captured.reduce((total, result) => total + result.max_score, 0)
                  const passed = captured.filter(stepPassed).length
                  return (
                    <td key={run.runFolder} className="px-3 py-2.5 text-center">
                      <div className="text-xs font-semibold text-foreground">
                        {captured.length > 0 ? `${formatScore(score)} / ${formatScore(maxScore)}` : '—'}
                      </div>
                      {captured.length > 0 && (
                        <div className="text-[9px] text-muted-foreground">{passed}/{captured.length} passed</div>
                      )}
                    </td>
                  )
                })}
              </tr>
              {activeSteps.map(step => renderStepRow(step))}
              {historicalSteps.length > 0 && (
                <tr className="border-b bg-muted/10">
                  <th colSpan={runs.length + 1} className="px-3 py-2 text-left">
                    <button
                      type="button"
                      onClick={() => setShowHistorical(prev => !prev)}
                      className="flex items-center gap-1.5 rounded px-1 py-0.5 text-left text-xs font-medium text-muted-foreground hover:bg-muted hover:text-foreground"
                      aria-expanded={showHistorical}
                      aria-label={`${showHistorical ? 'Hide' : 'Show'} ${historicalSteps.length} historical criteria`}
                    >
                      {showHistorical
                        ? <ChevronDown className="h-3.5 w-3.5 shrink-0" />
                        : <ChevronRight className="h-3.5 w-3.5 shrink-0" />}
                      <span className="rounded-full border border-amber-500/30 bg-amber-500/10 px-1.5 py-0.5 text-[8px] font-semibold uppercase tracking-wide text-amber-600 dark:text-amber-400">
                        Historical
                      </span>
                      <span>
                        {historicalSteps.length} criteri{historicalSteps.length === 1 ? 'on' : 'a'} no longer in the current plan
                      </span>
                    </button>
                  </th>
                </tr>
              )}
              {showHistorical && historicalSteps.map(step => renderStepRow(step))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}
