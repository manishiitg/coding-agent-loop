import React from 'react'
import { DollarSign, Coins, RefreshCw } from 'lucide-react'
import { formatStartedAt } from '../../../utils/duration'
import { formatUSD, formatTokens } from './helpers'
import type { CostsData } from './useCostsData'

type CostsHeaderProps = Pick<CostsData, 'overallSummary' | 'aggregateSummary' | 'phaseCostSummary' | 'loading' | 'loadAllCosts'> & {
  startedAt?: string | null
  headerAction?: React.ReactNode
}

// Header content only; InspectorShell owns the row wrapper and the close X.
const CostsHeader: React.FC<CostsHeaderProps> = ({
  startedAt,
  overallSummary,
  aggregateSummary,
  phaseCostSummary,
  loading,
  loadAllCosts,
  headerAction,
}) => (
          <div className="flex min-w-0 flex-1 items-start justify-between gap-3">
            <div className="min-w-0 flex-1">
              <h2 className="text-lg font-semibold text-foreground flex items-center gap-2">
                <DollarSign className="w-5 h-5 text-primary" />
                Cost Analysis
                {startedAt && (
                  <span className="text-xs font-normal text-muted-foreground">{formatStartedAt(startedAt)}</span>
                )}
              </h2>
              <div className="mt-1 flex flex-wrap items-center gap-2 sm:gap-4">
                {overallSummary && (
                  <div className="flex flex-wrap items-center gap-2 text-xs sm:gap-3">
                    <div className="flex items-center gap-1.5 text-green-600 dark:text-green-400 font-medium">
                      <DollarSign className="w-3.5 h-3.5" />
                      {formatUSD(overallSummary.totalCost)}
                    </div>
                    <div className="flex items-center gap-1.5 text-muted-foreground">
                      <Coins className="w-3.5 h-3.5" />
                      {formatTokens(overallSummary.totalTokens)} tokens
                    </div>
                    {aggregateSummary && (
                      <div className="text-muted-foreground">
                        {aggregateSummary.totalRuns} run{aggregateSummary.totalRuns !== 1 ? 's' : ''}
                      </div>
                    )}
                    {aggregateSummary && aggregateSummary.totalToolCost > 0 && (
                      <div className="text-muted-foreground">
                        LLM {formatUSD(aggregateSummary.totalLLMCost)} | Tools {formatUSD(aggregateSummary.totalToolCost)}
                      </div>
                    )}
                    {phaseCostSummary && (
                      <div className="text-amber-600 dark:text-amber-400 font-medium">
                        Builder {formatUSD(phaseCostSummary.totalCost)}
                      </div>
                    )}
                  </div>
                )}
              </div>
            </div>
            <div className="ml-auto flex shrink-0 items-center gap-2">
              {headerAction}
              <button
                onClick={loadAllCosts}
                disabled={loading}
                className="p-1.5 rounded-md hover:bg-muted transition-colors text-muted-foreground hover:text-foreground"
                title="Refresh"
              >
                <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
              </button>
            </div>
          </div>
)

export default CostsHeader
