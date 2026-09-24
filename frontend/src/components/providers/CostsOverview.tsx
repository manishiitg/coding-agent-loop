import { Fragment, useCallback, useEffect, useMemo, useState } from 'react'
import { ChevronRight, CircleAlert, Loader2, RefreshCw } from 'lucide-react'
import { agentApi } from '../../services/api'
import type { CostAggregate, CostOverview, CostOverviewItem } from '../../services/api-types'
import { formatTokens, formatUSD } from '../workflow/costs/helpers'
import { costAgentLabel } from '../workflow/costs/CostsModelSection'
import type { WorkSession } from '../../products/work/workSessions'

const RANGES = [
  { days: 7, label: '7 days' },
  { days: 30, label: '30 days' },
  { days: 90, label: '90 days' },
] as const

// Ledger scopes shown as their own columns; everything else (chat,
// evaluation, tool, unknown) sums into "Chat & other".
const SCOPE_COLUMNS = [
  { scope: 'workflow_execution', label: 'Runs' },
  { scope: 'pulse', label: 'Pulse' },
  { scope: 'builder', label: 'Builder' },
] as const

const utcDate = (date: Date) => date.toISOString().slice(0, 10)

const costRangeBounds = (days: number, now = new Date()) => {
  const from = new Date(now)
  from.setUTCDate(from.getUTCDate() - (days - 1))
  return { from: utcDate(from), to: utcDate(now) }
}

const scopeCost = (item: CostOverviewItem, scope: string) => item.by_scope?.[scope]?.total_cost_usd ?? 0

const otherScopeCost = (item: CostOverviewItem) =>
  Object.entries(item.by_scope || {})
    .filter(([scope]) => !SCOPE_COLUMNS.some(column => column.scope === scope))
    .reduce((sum, [, aggregate]) => sum + (aggregate?.total_cost_usd ?? 0), 0)

const tokenCount = (aggregate?: CostAggregate) =>
  (aggregate?.prompt_tokens ?? 0) + (aggregate?.completion_tokens ?? 0) + (aggregate?.cache_read_tokens ?? 0) + (aggregate?.cache_write_tokens ?? 0)

// Crew rows arrive keyed by project folder; the Crew list supplies names.
const crewProjectId = (workspacePath: string) => {
  const marker = 'Chats/Work/projects/'
  const index = workspacePath.indexOf(marker)
  return index >= 0 ? workspacePath.slice(index + marker.length).split('/')[0] : ''
}

function KindBadge({ item, crew }: { item: CostOverviewItem; crew?: WorkSession }) {
  if (item.kind === 'workflow') return <span className="text-[11px] text-gray-500 dark:text-gray-400">Workflow</span>
  if (item.kind === 'crew') {
    const owner = crew?.shared ? ` · ${crew.shared.ownerUsername || crew.shared.ownerId}` : ''
    return <span className="text-[11px] text-gray-500 dark:text-gray-400">Crew{owner}</span>
  }
  return <span className="text-[11px] text-gray-500 dark:text-gray-400">Chats and spend not tied to a workflow</span>
}

export default function CostsOverview() {
  const [days, setDays] = useState<number>(30)
  const [data, setData] = useState<CostOverview | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [expanded, setExpanded] = useState<string | null>(null)
  const [crews, setCrews] = useState<WorkSession[]>([])
  const [reloadKey, setReloadKey] = useState(0)

  useEffect(() => {
    const controller = new AbortController()
    const { from, to } = costRangeBounds(days)
    setLoading(true)
    setError(null)
    agentApi.getCostOverview(from, to, controller.signal)
      .then(result => {
        // An older server without this endpoint answers with the app's HTML
        // fallback rather than a 404, so check the shape before using it.
        if (!result || typeof result !== 'object' || !Array.isArray(result.items)) {
          setData(null)
          setError('The server did not return cost data. Restart the AgentWorks server to enable this view.')
          return
        }
        setData(result)
      })
      .catch(err => {
        if (controller.signal.aborted) return
        setError(err instanceof Error ? err.message : 'Could not load costs')
      })
      .finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [days, reloadKey])

  useEffect(() => {
    let cancelled = false
    // Loaded lazily: a static import of the Crew product module from the
    // app shell forms an import cycle.
    void import('../../products/work/workSessions')
      .then(module => module.loadWorkSessionsIncludingShared())
      .then(list => { if (!cancelled) setCrews(list) })
      .catch(() => { if (!cancelled) setCrews([]) })
    return () => { cancelled = true }
  }, [])

  const crewByProject = useMemo(() => {
    const map = new Map<string, WorkSession>()
    for (const crew of crews) {
      map.set(crewProjectId(crew.workspacePath) || crew.id, crew)
    }
    return map
  }, [crews])

  const itemLabel = useCallback((item: CostOverviewItem) => {
    if (item.kind !== 'crew') return item.name
    const crew = crewByProject.get(item.name)
    return crew?.identity?.name?.trim() || crew?.title || item.name
  }, [crewByProject])

  const providerRows = useMemo(() => Object.entries(data?.by_provider || {})
    .map(([provider, usage]) => ({ provider, label: costAgentLabel(provider === 'unknown' ? '' : provider, ''), usage }))
    .filter(row => row.usage.total_cost_usd > 0 || row.usage.call_count > 0)
    .sort((a, b) => b.usage.total_cost_usd - a.usage.total_cost_usd), [data])

  const items = data?.items?.filter(item => item.total_cost_usd > 0 || item.call_count > 0) ?? []
  const total = data?.total
  const maxProviderCost = Math.max(...providerRows.map(row => row.usage.total_cost_usd), 0)

  return (
    <div className="mx-auto max-w-4xl">
      <div className="mb-5 flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-xl font-semibold text-gray-950 dark:text-white">Costs</h2>
          <p className="mt-1 max-w-2xl text-sm leading-6 text-gray-600 dark:text-gray-300">
            Spend across every workflow and Crew you can open{data?.includes_other ? ', plus chats and spend not tied to a workflow' : ''}.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <div role="group" aria-label="Date range" className="inline-flex rounded-lg border border-gray-200 p-0.5 dark:border-gray-700">
            {RANGES.map(range => (
              <button
                type="button"
                key={range.days}
                onClick={() => setDays(range.days)}
                aria-pressed={days === range.days}
                className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors ${
                  days === range.days
                    ? 'bg-violet-50 text-violet-700 dark:bg-violet-500/20 dark:text-violet-200'
                    : 'text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800'
                }`}
              >
                {range.label}
              </button>
            ))}
          </div>
          <button
            type="button"
            onClick={() => setReloadKey(key => key + 1)}
            disabled={loading}
            aria-label="Refresh costs"
            className="rounded-lg p-2 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-800 disabled:opacity-50 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-gray-100"
          >
            <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
          </button>
        </div>
      </div>

      {error && (
        <div className="mb-4 flex items-start gap-2 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-300">
          <CircleAlert className="mt-0.5 h-4 w-4 shrink-0" />
          <span className="flex-1">{error}</span>
        </div>
      )}

      {loading && !data ? (
        <div className="flex items-center gap-2 py-10 text-sm text-gray-500">
          <Loader2 className="h-4 w-4 animate-spin" /> Loading costs…
        </div>
      ) : data && (
        <>
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
            {[
              { label: 'Total spend', value: formatUSD(total?.total_cost_usd) },
              { label: 'Workflows & Crews', value: String(items.filter(item => item.kind !== 'other').length) },
              { label: 'LLM calls', value: (total?.call_count ?? 0).toLocaleString() },
              { label: 'Tokens', value: formatTokens(tokenCount(total)) },
            ].map(card => (
              <div key={card.label} className="rounded-xl border border-gray-200 bg-white p-3 dark:border-gray-700 dark:bg-gray-900">
                <div className="text-xs text-gray-500 dark:text-gray-400">{card.label}</div>
                <div className="mt-1 text-lg font-semibold tabular-nums text-gray-950 dark:text-white">{card.value}</div>
              </div>
            ))}
          </div>
          {((total?.subscription_shadow_cost_usd ?? 0) > 0 || (total?.token_estimate_cost_usd ?? 0) > 0) && (
            <p className="mt-2 text-xs leading-5 text-gray-500 dark:text-gray-400">
              {formatUSD(total?.provider_actual_cost_usd)} reported by providers · {formatUSD(total?.subscription_shadow_cost_usd)} subscription-equivalent · {formatUSD(total?.token_estimate_cost_usd)} estimated from tokens
            </p>
          )}

          {providerRows.length > 0 && (
            <section className="mt-6">
              <h3 className="mb-2 text-sm font-semibold text-gray-900 dark:text-gray-100">By coding agent</h3>
              <div className="space-y-2">
                {providerRows.map(row => (
                  <div key={row.provider} className="grid grid-cols-[8rem_minmax(0,1fr)_5.5rem] items-center gap-3 text-sm">
                    <span className="truncate text-gray-700 dark:text-gray-200">{row.label}</span>
                    <div className="h-2 overflow-hidden rounded-full bg-gray-100 dark:bg-gray-800">
                      <div
                        className="h-full rounded-full bg-violet-500 dark:bg-violet-400"
                        style={{ width: `${maxProviderCost > 0 ? Math.max(2, (row.usage.total_cost_usd / maxProviderCost) * 100) : 0}%` }}
                      />
                    </div>
                    <span className="text-right tabular-nums text-gray-900 dark:text-gray-100">{formatUSD(row.usage.total_cost_usd)}</span>
                  </div>
                ))}
              </div>
            </section>
          )}

          <section className="mt-6">
            <h3 className="mb-2 text-sm font-semibold text-gray-900 dark:text-gray-100">By workflow and Crew</h3>
            {items.length === 0 ? (
              <p className="rounded-lg border border-dashed border-gray-200 px-3 py-6 text-center text-sm text-gray-500 dark:border-gray-700">
                No spend recorded in the last {days} days.
              </p>
            ) : (
              <div className="overflow-x-auto rounded-xl border border-gray-200 dark:border-gray-700">
                <table className="w-full min-w-[36rem] text-sm">
                  <thead className="bg-gray-50 text-xs text-gray-500 dark:bg-gray-950/40 dark:text-gray-400">
                    <tr>
                      <th scope="col" className="px-3 py-2 text-left font-medium">Name</th>
                      {SCOPE_COLUMNS.map(column => (
                        <th scope="col" key={column.scope} className="px-3 py-2 text-right font-medium">{column.label}</th>
                      ))}
                      <th scope="col" className="px-3 py-2 text-right font-medium">Chat & other</th>
                      <th scope="col" className="px-3 py-2 text-right font-medium">Total</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-gray-100 dark:divide-gray-800">
                    {items.map(item => {
                      const isExpanded = expanded === item.id
                      const models = Object.entries(item.by_model || {})
                        .filter(([, usage]) => usage.total_cost_usd > 0 || usage.call_count > 0)
                        .sort(([, a], [, b]) => b.total_cost_usd - a.total_cost_usd)
                      return (
                        <Fragment key={item.id}>
                          <tr className="text-gray-900 dark:text-gray-100">
                            <td className="px-3 py-2">
                              <button
                                type="button"
                                onClick={() => setExpanded(isExpanded ? null : item.id)}
                                aria-expanded={isExpanded}
                                className="flex min-w-0 items-start gap-1.5 text-left"
                              >
                                <ChevronRight className={`mt-0.5 h-3.5 w-3.5 shrink-0 text-gray-400 transition-transform ${isExpanded ? 'rotate-90' : ''}`} />
                                <span className="min-w-0">
                                  <span className="block truncate font-medium">{itemLabel(item)}</span>
                                  <KindBadge item={item} crew={item.kind === 'crew' ? crewByProject.get(item.name) : undefined} />
                                </span>
                              </button>
                            </td>
                            {SCOPE_COLUMNS.map(column => (
                              <td key={column.scope} className="px-3 py-2 text-right tabular-nums text-gray-600 dark:text-gray-300">
                                {scopeCost(item, column.scope) > 0 ? formatUSD(scopeCost(item, column.scope)) : '—'}
                              </td>
                            ))}
                            <td className="px-3 py-2 text-right tabular-nums text-gray-600 dark:text-gray-300">
                              {otherScopeCost(item) > 0 ? formatUSD(otherScopeCost(item)) : '—'}
                            </td>
                            <td className="px-3 py-2 text-right font-semibold tabular-nums">{formatUSD(item.total_cost_usd)}</td>
                          </tr>
                          {isExpanded && (
                            <tr className="bg-gray-50/70 dark:bg-gray-950/30">
                              <td colSpan={SCOPE_COLUMNS.length + 3} className="px-3 py-2 pl-8">
                                {models.length === 0 ? (
                                  <span className="text-xs text-gray-500">No per-model breakdown recorded.</span>
                                ) : (
                                  <ul className="space-y-1 text-xs">
                                    {models.map(([modelId, usage]) => (
                                      <li key={modelId} className="flex items-center justify-between gap-3 text-gray-600 dark:text-gray-300">
                                        <span className="min-w-0 truncate">
                                          {costAgentLabel(usage.provider || '', modelId)} · <span className="font-mono">{modelId}</span>
                                        </span>
                                        <span className="shrink-0 tabular-nums">
                                          {usage.call_count.toLocaleString()} calls · {formatTokens(tokenCount(usage))} tokens · {formatUSD(usage.total_cost_usd)}
                                        </span>
                                      </li>
                                    ))}
                                  </ul>
                                )}
                              </td>
                            </tr>
                          )}
                        </Fragment>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            )}
          </section>
        </>
      )}
    </div>
  )
}
