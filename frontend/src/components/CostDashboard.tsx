import { useEffect, useState } from 'react'
import { X, RefreshCw, Loader2 } from 'lucide-react'
import { agentApi } from '../services/api'
import type { CostSummary, CostAggregate } from '../services/api-types'

interface CostDashboardProps {
  isOpen: boolean
  onClose: () => void
}

// Format a USD cost value with 4 significant digits — e.g. 0.0234, 1.23, 12.4.
function formatCost(cost: number): string {
  if (cost === 0) return '$0'
  if (cost < 0.01) return `$${cost.toFixed(4)}`
  if (cost < 1) return `$${cost.toFixed(3)}`
  return `$${cost.toFixed(2)}`
}

function formatTokens(n: number): string {
  if (n < 1000) return `${n}`
  if (n < 1_000_000) return `${(n / 1000).toFixed(1)}K`
  return `${(n / 1_000_000).toFixed(2)}M`
}

function AggregateRow({ label, agg }: { label: string; agg: CostAggregate }) {
  return (
    <tr className="border-b border-gray-100 dark:border-gray-800">
      <td className="px-3 py-2 text-sm text-gray-900 dark:text-gray-100">{label}</td>
      <td className="px-3 py-2 text-sm text-right text-gray-600 dark:text-gray-400">{agg.call_count}</td>
      <td className="px-3 py-2 text-sm text-right text-gray-600 dark:text-gray-400">{formatTokens(agg.prompt_tokens)}</td>
      <td className="px-3 py-2 text-sm text-right text-gray-600 dark:text-gray-400">{formatTokens(agg.completion_tokens)}</td>
      <td className="px-3 py-2 text-sm text-right font-medium text-gray-900 dark:text-gray-100">{formatCost(agg.total_cost_usd)}</td>
    </tr>
  )
}

export default function CostDashboard({ isOpen, onClose }: CostDashboardProps) {
  const [summary, setSummary] = useState<CostSummary | null>(null)
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const load = async () => {
    setIsLoading(true)
    setError(null)
    try {
      const data = await agentApi.getCostSummary()
      setSummary(data)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load cost summary')
    } finally {
      setIsLoading(false)
    }
  }

  useEffect(() => {
    if (!isOpen) return
    load()
  }, [isOpen])

  if (!isOpen) return null

  const sortedDates = summary ? Object.keys(summary.by_date).sort().reverse() : []
  const sortedModels = summary
    ? Object.keys(summary.by_model).sort(
        (a, b) => summary.by_model[b].total_cost_usd - summary.by_model[a].total_cost_usd,
      )
    : []

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={onClose}>
      <div
        className="w-full max-w-3xl max-h-[85vh] overflow-hidden bg-white dark:bg-gray-900 rounded-xl shadow-2xl flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between px-5 py-4 border-b border-gray-200 dark:border-gray-700">
          <div>
            <h2 className="text-lg font-semibold text-gray-900 dark:text-gray-100">LLM Costs</h2>
            <p className="text-xs text-gray-500 dark:text-gray-400">Global usage across every chat + workflow run.</p>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={load}
              disabled={isLoading}
              className="p-1.5 rounded-md text-gray-500 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-800 disabled:opacity-50"
              title="Refresh"
            >
              <RefreshCw className={`w-4 h-4 ${isLoading ? 'animate-spin' : ''}`} />
            </button>
            <button
              onClick={onClose}
              className="p-1.5 rounded-md text-gray-500 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-800"
            >
              <X className="w-4 h-4" />
            </button>
          </div>
        </div>

        <div className="flex-1 overflow-y-auto p-5 space-y-6">
          {isLoading && !summary && (
            <div className="flex items-center justify-center py-12">
              <Loader2 className="w-6 h-6 animate-spin text-gray-400" />
            </div>
          )}

          {error && (
            <div className="text-sm text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-900/20 rounded-md px-3 py-2">
              {error}
            </div>
          )}

          {summary && (
            <>
              <div className="grid grid-cols-4 gap-3">
                <div className="bg-gray-50 dark:bg-gray-800 rounded-lg px-3 py-2">
                  <div className="text-xs text-gray-500 dark:text-gray-400">Total cost</div>
                  <div className="text-lg font-semibold text-gray-900 dark:text-gray-100">
                    {formatCost(summary.total.total_cost_usd)}
                  </div>
                </div>
                <div className="bg-gray-50 dark:bg-gray-800 rounded-lg px-3 py-2">
                  <div className="text-xs text-gray-500 dark:text-gray-400">LLM calls</div>
                  <div className="text-lg font-semibold text-gray-900 dark:text-gray-100">
                    {summary.total.call_count}
                  </div>
                </div>
                <div className="bg-gray-50 dark:bg-gray-800 rounded-lg px-3 py-2">
                  <div className="text-xs text-gray-500 dark:text-gray-400">Input tokens</div>
                  <div className="text-lg font-semibold text-gray-900 dark:text-gray-100">
                    {formatTokens(summary.total.prompt_tokens)}
                  </div>
                </div>
                <div className="bg-gray-50 dark:bg-gray-800 rounded-lg px-3 py-2">
                  <div className="text-xs text-gray-500 dark:text-gray-400">Output tokens</div>
                  <div className="text-lg font-semibold text-gray-900 dark:text-gray-100">
                    {formatTokens(summary.total.completion_tokens)}
                  </div>
                </div>
              </div>

              <section>
                <h3 className="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-2">By model</h3>
                {sortedModels.length === 0 ? (
                  <p className="text-sm text-gray-500 dark:text-gray-400">No usage recorded yet.</p>
                ) : (
                  <table className="w-full">
                    <thead className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide border-b border-gray-200 dark:border-gray-700">
                      <tr>
                        <th className="px-3 py-2 text-left">Model</th>
                        <th className="px-3 py-2 text-right">Calls</th>
                        <th className="px-3 py-2 text-right">Input</th>
                        <th className="px-3 py-2 text-right">Output</th>
                        <th className="px-3 py-2 text-right">Cost</th>
                      </tr>
                    </thead>
                    <tbody>
                      {sortedModels.map((m) => (
                        <AggregateRow key={m} label={m} agg={summary.by_model[m]} />
                      ))}
                    </tbody>
                  </table>
                )}
              </section>

              <section>
                <h3 className="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-2">By date</h3>
                {sortedDates.length === 0 ? (
                  <p className="text-sm text-gray-500 dark:text-gray-400">No usage recorded yet.</p>
                ) : (
                  <table className="w-full">
                    <thead className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide border-b border-gray-200 dark:border-gray-700">
                      <tr>
                        <th className="px-3 py-2 text-left">Date</th>
                        <th className="px-3 py-2 text-right">Calls</th>
                        <th className="px-3 py-2 text-right">Input</th>
                        <th className="px-3 py-2 text-right">Output</th>
                        <th className="px-3 py-2 text-right">Cost</th>
                      </tr>
                    </thead>
                    <tbody>
                      {sortedDates.map((d) => (
                        <AggregateRow key={d} label={d} agg={summary.by_date[d]} />
                      ))}
                    </tbody>
                  </table>
                )}
              </section>
            </>
          )}
        </div>
      </div>
    </div>
  )
}
