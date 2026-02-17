import React, { useEffect, useState, useMemo } from 'react'
import {
  X,
  Loader2,
  ChevronRight,
  ChevronDown,
  AlertCircle,
  DollarSign,
  Coins,
  Award,
  TrendingUp,
  TrendingDown,
  RefreshCw
} from 'lucide-react'
import { agentApi } from '../services/api'
import type {
  UserCostsResponse,
  SessionCostSummary,
  SessionCostDetail,
  ChatModelUsage
} from '../services/api-types'

interface ChatCostsPopupProps {
  isOpen: boolean
  onClose: () => void
  isMultiAgent: boolean
}

// Format cost in USD
const formatUSD = (amount?: number) => {
  if (amount === undefined || amount === null) return '$0.00'
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 4,
    maximumFractionDigits: 4
  }).format(amount)
}

// Format token count (e.g., 1,234,567 -> 1.23M)
const formatTokens = (count?: number) => {
  if (!count) return '0'
  if (count >= 1000000) {
    return (count / 1000000).toFixed(2) + 'M'
  }
  if (count >= 1000) {
    return (count / 1000).toFixed(1) + 'K'
  }
  return count.toString()
}

// Format date for display
const formatDate = (dateStr: string) => {
  const date = new Date(dateStr)
  return date.toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit'
  })
}

const ChatCostsPopup: React.FC<ChatCostsPopupProps> = ({
  isOpen,
  onClose,
  isMultiAgent
}) => {
  const [loading, setLoading] = useState(false)
  const [costsData, setCostsData] = useState<UserCostsResponse | null>(null)
  const [selectedSessionId, setSelectedSessionId] = useState<string | null>(null)
  const [sessionDetail, setSessionDetail] = useState<SessionCostDetail | null>(null)
  const [sessionDetailLoading, setSessionDetailLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [expandedSessions, setExpandedSessions] = useState<Set<string>>(new Set())
  const [expandedModels, setExpandedModels] = useState<Set<string>>(new Set())
  const [costViewMode, setCostViewMode] = useState<Record<string, 'agent' | 'model'>>({})

  // Load costs data
  useEffect(() => {
    if (isOpen) {
      // Small delay to let polling connections close
      const timer = setTimeout(() => loadCosts(), 100)
      return () => clearTimeout(timer)
    } else {
      setCostsData(null)
      setSelectedSessionId(null)
      setSessionDetail(null)
      setError(null)
      setExpandedSessions(new Set())
      setExpandedModels(new Set())
      setCostViewMode({})
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isOpen])

  const loadCosts = async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await agentApi.getAllSessionCosts()
      setCostsData(data)
    } catch (err) {
      console.error('Failed to load costs:', err)
      setError('Failed to load cost data')
    } finally {
      setLoading(false)
    }
  }

  // Load session detail when a session is selected
  useEffect(() => {
    if (selectedSessionId) {
      loadSessionDetail(selectedSessionId)
    } else {
      setSessionDetail(null)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedSessionId])

  const loadSessionDetail = async (sessionId: string) => {
    setSessionDetailLoading(true)
    try {
      const controller = new AbortController()
      const timeoutId = setTimeout(() => controller.abort(), 15000)
      const data = await agentApi.getSessionCosts(sessionId, controller.signal)
      clearTimeout(timeoutId)
      setSessionDetail(data)
    } catch (err) {
      console.error('Failed to load session costs:', err)
    } finally {
      setSessionDetailLoading(false)
    }
  }

  const toggleSession = (sessionId: string) => {
    setExpandedSessions(prev => {
      const next = new Set(prev)
      if (next.has(sessionId)) {
        next.delete(sessionId)
      } else {
        next.add(sessionId)
      }
      return next
    })
  }

  const toggleModel = (key: string) => {
    setExpandedModels(prev => {
      const next = new Set(prev)
      if (next.has(key)) {
        next.delete(key)
      } else {
        next.add(key)
      }
      return next
    })
  }

  const setViewMode = (sessionId: string, mode: 'agent' | 'model') => {
    setCostViewMode(prev => ({ ...prev, [sessionId]: mode }))
  }

  // Aggregate summary
  const aggregate = costsData?.aggregate

  // Sessions with costs (filter out zero-cost sessions)
  const sessionsWithCosts = useMemo(() => {
    if (!costsData?.sessions) return []
    return costsData.sessions.filter(s => s.total_cost_usd > 0 || s.total_llm_calls > 0)
  }, [costsData])

  // Highest/lowest cost sessions
  const { highestCost, lowestCost } = useMemo(() => {
    if (sessionsWithCosts.length === 0) return { highestCost: 0, lowestCost: 0 }
    let highest = 0
    let lowest = Infinity
    sessionsWithCosts.forEach(s => {
      if (s.total_cost_usd > highest) highest = s.total_cost_usd
      if (s.total_cost_usd < lowest) lowest = s.total_cost_usd
    })
    return { highestCost: highest, lowestCost: lowest === Infinity ? 0 : lowest }
  }, [sessionsWithCosts])

  if (!isOpen) return null

  // Render model table for a given by_model map
  const renderModelTable = (
    byModel: Record<string, ChatModelUsage>,
    keyPrefix: string,
    byAgentAndModel?: Record<string, Record<string, ChatModelUsage>>
  ) => {
    const models = Object.entries(byModel)
    if (models.length === 0) return null

    return (
      <table className="w-full text-xs">
        <thead>
          <tr className="text-muted-foreground border-b border-border pb-2">
            <th className="w-8"></th>
            <th className="text-left font-medium pb-2">Model</th>
            <th className="text-right font-medium pb-2">Calls</th>
            <th className="text-right font-medium pb-2">Input</th>
            <th className="text-right font-medium pb-2">Cached In</th>
            <th className="text-right font-medium pb-2">Reasoning</th>
            <th className="text-right font-medium pb-2">Output</th>
            <th className="text-right font-medium pb-2">Cost (USD)</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border">
          {models.map(([modelId, usage]) => {
            const cacheRead = usage.cache_read_tokens || usage.cache_tokens || 0
            const reasoning = usage.reasoning_tokens || 0
            const cachePercent = usage.input_tokens > 0 ? (cacheRead / usage.input_tokens) * 100 : 0
            const modelKey = `${keyPrefix}-${modelId}`
            const isModelExpanded = expandedModels.has(modelKey)

            // Find per-agent breakdown for this model
            const agentBreakdown = byAgentAndModel
              ? Object.entries(byAgentAndModel)
                  .map(([agentName, agentModels]) => {
                    const agentUsage = agentModels[modelId]
                    if (!agentUsage) return null
                    return { agentName, usage: agentUsage }
                  })
                  .filter((s): s is NonNullable<typeof s> => s !== null)
              : []

            return (
              <React.Fragment key={modelId}>
                <tr
                  className="hover:bg-accent/50 transition-colors cursor-pointer"
                  onClick={() => agentBreakdown.length > 0 && toggleModel(modelKey)}
                >
                  <td className="py-2 pl-2">
                    {agentBreakdown.length > 0 && (
                      isModelExpanded ? (
                        <ChevronDown className="w-3 h-3 text-muted-foreground" />
                      ) : (
                        <ChevronRight className="w-3 h-3 text-muted-foreground" />
                      )
                    )}
                  </td>
                  <td className="py-2">
                    <div className="font-mono text-foreground font-medium">{modelId}</div>
                    <div className="text-[10px] text-muted-foreground uppercase">{usage.provider}</div>
                  </td>
                  <td className="py-2 text-right text-foreground">{usage.llm_call_count}</td>
                  <td className="py-2 text-right text-muted-foreground">{usage.input_tokens.toLocaleString()}</td>
                  <td className="py-2 text-right">
                    <div className="text-foreground">{cacheRead.toLocaleString()}</div>
                    {cachePercent > 0 && (
                      <div className="text-[10px] text-green-600 dark:text-green-400">({cachePercent.toFixed(0)}%)</div>
                    )}
                  </td>
                  <td className="py-2 text-right text-muted-foreground">{reasoning > 0 ? reasoning.toLocaleString() : '-'}</td>
                  <td className="py-2 text-right text-muted-foreground">{usage.output_tokens.toLocaleString()}</td>
                  <td className="py-2 text-right text-green-600 dark:text-green-400 font-semibold">{formatUSD(usage.total_cost_usd)}</td>
                </tr>
                {isModelExpanded && agentBreakdown.length > 0 && (
                  <tr className="bg-muted/20">
                    <td colSpan={8} className="p-0">
                      <div className="p-4">
                        <div className="border border-border rounded-md overflow-hidden bg-background">
                          <div className="bg-muted/50 px-4 py-2 border-b border-border">
                            <h4 className="font-semibold text-xs text-foreground">Usage by Agent</h4>
                          </div>
                          <div className="overflow-x-auto">
                            <table className="w-full text-xs">
                              <thead>
                                <tr className="text-muted-foreground border-b border-border bg-muted/30">
                                  <th className="px-4 py-2 text-left font-medium">Agent</th>
                                  <th className="px-4 py-2 text-right font-medium">Calls</th>
                                  <th className="px-4 py-2 text-right font-medium">Input</th>
                                  <th className="px-4 py-2 text-right font-medium">Output</th>
                                  <th className="px-4 py-2 text-right font-medium">Cost</th>
                                </tr>
                              </thead>
                              <tbody className="divide-y divide-border">
                                {agentBreakdown.map((ab) => (
                                  <tr key={ab.agentName} className="hover:bg-muted/30 transition-colors">
                                    <td className="px-4 py-2 font-medium text-foreground">{ab.agentName}</td>
                                    <td className="px-4 py-2 text-right text-muted-foreground">{ab.usage.llm_call_count}</td>
                                    <td className="px-4 py-2 text-right text-muted-foreground">{ab.usage.input_tokens.toLocaleString()}</td>
                                    <td className="px-4 py-2 text-right text-muted-foreground">{ab.usage.output_tokens.toLocaleString()}</td>
                                    <td className="px-4 py-2 text-right text-green-600 dark:text-green-400 font-medium">{formatUSD(ab.usage.total_cost_usd)}</td>
                                  </tr>
                                ))}
                              </tbody>
                            </table>
                          </div>
                        </div>
                      </div>
                    </td>
                  </tr>
                )}
              </React.Fragment>
            )
          })}
        </tbody>
        <tfoot>
          <tr className="border-t-2 border-border font-bold">
            <td></td>
            <td className="py-3 text-foreground">Total</td>
            <td className="py-3 text-right text-foreground">
              {models.reduce((sum, [, u]) => sum + u.llm_call_count, 0)}
            </td>
            <td className="py-3 text-right text-muted-foreground">
              {models.reduce((sum, [, u]) => sum + u.input_tokens, 0).toLocaleString()}
            </td>
            <td className="py-3 text-right text-muted-foreground">
              {models.reduce((sum, [, u]) => sum + (u.cache_read_tokens || u.cache_tokens || 0), 0).toLocaleString()}
            </td>
            <td className="py-3 text-right text-muted-foreground">
              {models.reduce((sum, [, u]) => sum + (u.reasoning_tokens || 0), 0).toLocaleString()}
            </td>
            <td className="py-3 text-right text-muted-foreground">
              {models.reduce((sum, [, u]) => sum + u.output_tokens, 0).toLocaleString()}
            </td>
            <td className="py-3 text-right text-green-600 dark:text-green-400">
              {formatUSD(models.reduce((sum, [, u]) => sum + u.total_cost_usd, 0))}
            </td>
          </tr>
        </tfoot>
      </table>
    )
  }

  // Render agent table for a given by_agent map
  const renderAgentTable = (byAgent: Record<string, ChatModelUsage>) => {
    const agents = Object.entries(byAgent)
    if (agents.length === 0) return <p className="text-sm text-muted-foreground p-4">No agent breakdown available.</p>

    return (
      <table className="w-full text-xs">
        <thead>
          <tr className="text-muted-foreground border-b border-border pb-2">
            <th className="text-left font-medium pb-2">Agent</th>
            <th className="text-right font-medium pb-2">Calls</th>
            <th className="text-right font-medium pb-2">Input Tokens</th>
            <th className="text-right font-medium pb-2">Output Tokens</th>
            <th className="text-right font-medium pb-2">Total Cost</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border">
          {agents.sort((a, b) => b[1].total_cost_usd - a[1].total_cost_usd).map(([agentName, usage]) => (
            <tr key={agentName} className="hover:bg-accent/50 transition-colors">
              <td className="py-2">
                <span className="font-medium text-foreground">{agentName}</span>
                <div className="text-[10px] text-muted-foreground uppercase">{usage.provider}</div>
              </td>
              <td className="py-2 text-right font-mono text-muted-foreground">{usage.llm_call_count}</td>
              <td className="py-2 text-right font-mono text-muted-foreground">{usage.input_tokens.toLocaleString()}</td>
              <td className="py-2 text-right font-mono text-muted-foreground">{usage.output_tokens.toLocaleString()}</td>
              <td className="py-2 text-right font-bold text-foreground">{formatUSD(usage.total_cost_usd)}</td>
            </tr>
          ))}
          <tr className="bg-muted/30 font-semibold">
            <td className="py-2 text-foreground">Total</td>
            <td className="py-2 text-right font-mono text-muted-foreground">
              {agents.reduce((sum, [, u]) => sum + u.llm_call_count, 0)}
            </td>
            <td className="py-2 text-right font-mono text-muted-foreground">
              {agents.reduce((sum, [, u]) => sum + u.input_tokens, 0).toLocaleString()}
            </td>
            <td className="py-2 text-right font-mono text-muted-foreground">
              {agents.reduce((sum, [, u]) => sum + u.output_tokens, 0).toLocaleString()}
            </td>
            <td className="py-2 text-right font-bold text-green-600 dark:text-green-400">
              {formatUSD(agents.reduce((sum, [, u]) => sum + u.total_cost_usd, 0))}
            </td>
          </tr>
        </tbody>
      </table>
    )
  }

  // Render expanded session content (inline or detail)
  const renderSessionContent = (session: SessionCostSummary) => {
    const viewMode = costViewMode[session.session_id] || 'agent'

    return (
      <div className="border-t border-border p-4 space-y-4">
        {/* Cost Breakdown with View Toggle */}
        <div className="bg-card border border-border rounded-lg overflow-hidden shadow-sm">
          <div className="px-4 py-3 bg-muted/30 border-b border-border flex items-center justify-between">
            <h3 className="text-sm font-semibold flex items-center gap-2">
              <DollarSign className="w-4 h-4 text-green-500" />
              Cost Breakdown
            </h3>
            <div className="flex items-center gap-1 bg-muted rounded-md p-0.5">
              <button
                onClick={() => setViewMode(session.session_id, 'agent')}
                className={`px-3 py-1 text-xs font-medium rounded transition-colors ${
                  viewMode === 'agent'
                    ? 'bg-background text-foreground shadow-sm'
                    : 'text-muted-foreground hover:text-foreground'
                }`}
              >
                By Agent
              </button>
              <button
                onClick={() => setViewMode(session.session_id, 'model')}
                className={`px-3 py-1 text-xs font-medium rounded transition-colors ${
                  viewMode === 'model'
                    ? 'bg-background text-foreground shadow-sm'
                    : 'text-muted-foreground hover:text-foreground'
                }`}
              >
                By Model
              </button>
            </div>
          </div>

          <div className="p-4 overflow-x-auto">
            {viewMode === 'agent' && session.by_agent
              ? renderAgentTable(session.by_agent)
              : renderModelTable(session.by_model, session.session_id)
            }
          </div>
        </div>
      </div>
    )
  }

  // Render session detail view (when selected from dropdown)
  const renderSessionDetail = () => {
    if (!sessionDetail) return null

    const viewMode = costViewMode[`detail-${sessionDetail.session_id}`] || 'agent'

    return (
      <div className="space-y-4">
        {/* Summary Cards */}
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
          <div className="bg-green-100 dark:bg-green-900/30 rounded-lg p-3">
            <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1">Total Cost</div>
            <div className="text-2xl font-bold text-green-600 dark:text-green-400">{formatUSD(sessionDetail.total_cost_usd)}</div>
          </div>
          <div className="bg-blue-100 dark:bg-blue-900/30 rounded-lg p-3">
            <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1">LLM Calls</div>
            <div className="text-2xl font-bold text-blue-600 dark:text-blue-400">{sessionDetail.total_llm_calls}</div>
          </div>
          <div className="bg-purple-100 dark:bg-purple-900/30 rounded-lg p-3">
            <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1">Input Tokens</div>
            <div className="text-2xl font-bold text-purple-600 dark:text-purple-400">{formatTokens(sessionDetail.total_input_tokens)}</div>
          </div>
          <div className="bg-muted rounded-lg p-3">
            <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1">Output Tokens</div>
            <div className="text-2xl font-bold text-foreground">{formatTokens(sessionDetail.total_output_tokens)}</div>
          </div>
        </div>

        {/* By Agent / By Model toggle */}
        <div className="bg-card border border-border rounded-lg overflow-hidden shadow-sm">
          <div className="px-4 py-3 bg-muted/30 border-b border-border flex items-center justify-between">
            <h3 className="text-sm font-semibold flex items-center gap-2">
              <DollarSign className="w-4 h-4 text-green-500" />
              Detailed Breakdown
            </h3>
            <div className="flex items-center gap-1 bg-muted rounded-md p-0.5">
              <button
                onClick={() => setViewMode(`detail-${sessionDetail.session_id}`, 'agent')}
                className={`px-3 py-1 text-xs font-medium rounded transition-colors ${
                  viewMode === 'agent'
                    ? 'bg-background text-foreground shadow-sm'
                    : 'text-muted-foreground hover:text-foreground'
                }`}
              >
                By Agent
              </button>
              <button
                onClick={() => setViewMode(`detail-${sessionDetail.session_id}`, 'model')}
                className={`px-3 py-1 text-xs font-medium rounded transition-colors ${
                  viewMode === 'model'
                    ? 'bg-background text-foreground shadow-sm'
                    : 'text-muted-foreground hover:text-foreground'
                }`}
              >
                By Model
              </button>
            </div>
          </div>

          <div className="p-4 overflow-x-auto">
            {viewMode === 'agent' && sessionDetail.by_agent_and_model ? (
              // Render per-agent with per-model sub-breakdown
              <table className="w-full text-xs">
                <thead>
                  <tr className="text-muted-foreground border-b border-border pb-2">
                    <th className="w-8"></th>
                    <th className="text-left font-medium pb-2">Agent</th>
                    <th className="text-right font-medium pb-2">Calls</th>
                    <th className="text-right font-medium pb-2">Input</th>
                    <th className="text-right font-medium pb-2">Output</th>
                    <th className="text-right font-medium pb-2">Cost (USD)</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {Object.entries(sessionDetail.by_agent_and_model).map(([agentName, modelMap]) => {
                    const agentTotals = Object.values(modelMap).reduce((acc, u) => ({
                      calls: acc.calls + u.llm_call_count,
                      input: acc.input + u.input_tokens,
                      output: acc.output + u.output_tokens,
                      cost: acc.cost + u.total_cost_usd
                    }), { calls: 0, input: 0, output: 0, cost: 0 })

                    const agentKey = `detail-agent-${agentName}`
                    const isExpanded = expandedModels.has(agentKey)

                    return (
                      <React.Fragment key={agentName}>
                        <tr
                          className="hover:bg-accent/50 transition-colors cursor-pointer"
                          onClick={() => toggleModel(agentKey)}
                        >
                          <td className="py-2 pl-2">
                            {isExpanded ? (
                              <ChevronDown className="w-3 h-3 text-muted-foreground" />
                            ) : (
                              <ChevronRight className="w-3 h-3 text-muted-foreground" />
                            )}
                          </td>
                          <td className="py-2 font-medium text-foreground">{agentName}</td>
                          <td className="py-2 text-right text-foreground">{agentTotals.calls}</td>
                          <td className="py-2 text-right text-muted-foreground">{agentTotals.input.toLocaleString()}</td>
                          <td className="py-2 text-right text-muted-foreground">{agentTotals.output.toLocaleString()}</td>
                          <td className="py-2 text-right text-green-600 dark:text-green-400 font-semibold">{formatUSD(agentTotals.cost)}</td>
                        </tr>
                        {isExpanded && (
                          <tr className="bg-muted/20">
                            <td colSpan={6} className="p-0">
                              <div className="p-3 pl-10">
                                <table className="w-full text-xs">
                                  <thead>
                                    <tr className="text-muted-foreground border-b border-border">
                                      <th className="text-left font-medium pb-1">Model</th>
                                      <th className="text-right font-medium pb-1">Calls</th>
                                      <th className="text-right font-medium pb-1">Input</th>
                                      <th className="text-right font-medium pb-1">Output</th>
                                      <th className="text-right font-medium pb-1">Cost</th>
                                    </tr>
                                  </thead>
                                  <tbody className="divide-y divide-border">
                                    {Object.entries(modelMap).map(([modelId, usage]) => (
                                      <tr key={modelId} className="hover:bg-muted/30 transition-colors">
                                        <td className="py-1.5">
                                          <span className="font-mono text-foreground">{modelId}</span>
                                          <span className="text-[10px] text-muted-foreground ml-1">({usage.provider})</span>
                                        </td>
                                        <td className="py-1.5 text-right text-muted-foreground">{usage.llm_call_count}</td>
                                        <td className="py-1.5 text-right text-muted-foreground">{usage.input_tokens.toLocaleString()}</td>
                                        <td className="py-1.5 text-right text-muted-foreground">{usage.output_tokens.toLocaleString()}</td>
                                        <td className="py-1.5 text-right text-green-600 dark:text-green-400 font-medium">{formatUSD(usage.total_cost_usd)}</td>
                                      </tr>
                                    ))}
                                  </tbody>
                                </table>
                              </div>
                            </td>
                          </tr>
                        )}
                      </React.Fragment>
                    )
                  })}
                </tbody>
              </table>
            ) : (
              renderModelTable(sessionDetail.by_model, `detail-${sessionDetail.session_id}`, sessionDetail.by_agent_and_model)
            )}
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm p-4">
      <div className="bg-background rounded-lg shadow-xl w-full max-w-6xl max-h-[90vh] flex flex-col border border-border relative">
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-border">
          <div className="flex-1 min-w-0">
            <h2 className="text-lg font-semibold text-foreground flex items-center gap-2">
              <DollarSign className="w-5 h-5 text-primary" />
              Cost Analysis
            </h2>
            <div className="flex items-center gap-4 mt-1">
              {aggregate && (
                <div className="flex items-center gap-3 text-xs">
                  <div className="flex items-center gap-1.5 text-green-600 dark:text-green-400 font-medium">
                    <DollarSign className="w-3.5 h-3.5" />
                    {formatUSD(aggregate.total_cost_usd)}
                  </div>
                  <div className="flex items-center gap-1.5 text-muted-foreground">
                    <Coins className="w-3.5 h-3.5" />
                    {formatTokens(aggregate.total_input_tokens + aggregate.total_output_tokens)} tokens
                  </div>
                  <div className="text-muted-foreground">
                    {aggregate.total_sessions} session{aggregate.total_sessions !== 1 ? 's' : ''}
                  </div>
                </div>
              )}
              <button
                onClick={loadCosts}
                disabled={loading}
                className="p-1.5 rounded-md hover:bg-muted transition-colors text-muted-foreground hover:text-foreground"
                title="Refresh"
              >
                <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
              </button>
            </div>
          </div>

          {/* Session dropdown */}
          <div className="flex items-center gap-2 mr-4">
            <select
              value={selectedSessionId || ''}
              onChange={(e) => setSelectedSessionId(e.target.value || null)}
              className="text-xs bg-muted border border-border rounded-md px-2 py-1.5 text-foreground max-w-[250px]"
            >
              <option value="">All Sessions (Aggregate)</option>
              {sessionsWithCosts.map(s => (
                <option key={s.session_id} value={s.session_id}>
                  {s.title || 'Untitled'} — {formatDate(s.created_at)} — {formatUSD(s.total_cost_usd)}
                </option>
              ))}
            </select>
          </div>

          <button
            onClick={onClose}
            className="p-2 rounded-full hover:bg-accent hover:text-accent-foreground transition-colors ml-2"
          >
            <X className="w-5 h-5 text-muted-foreground" />
          </button>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-y-auto p-6 bg-background">
          {loading ? (
            <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
              <Loader2 className="w-8 h-8 animate-spin mb-3 text-primary" />
              <p>Loading cost data...</p>
            </div>
          ) : error ? (
            <div className="flex flex-col items-center justify-center py-12 text-destructive">
              <AlertCircle className="w-12 h-12 mb-3" />
              <p>{error}</p>
              <button
                onClick={loadCosts}
                className="mt-4 px-4 py-2 bg-destructive/10 text-destructive rounded-md hover:bg-destructive/20 transition-colors text-sm font-medium"
              >
                Retry
              </button>
            </div>
          ) : sessionsWithCosts.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
              <DollarSign className="w-12 h-12 mb-3 opacity-50" />
              <p>No cost data found.</p>
              <p className="text-sm mt-2">Start chat sessions to see cost data here.</p>
            </div>
          ) : selectedSessionId ? (
            // Single session detail view
            sessionDetailLoading ? (
              <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
                <Loader2 className="w-8 h-8 animate-spin mb-3 text-primary" />
                <p>Loading session details...</p>
              </div>
            ) : sessionDetail ? (
              renderSessionDetail()
            ) : (
              <div className="text-center text-muted-foreground py-8">No detail data available.</div>
            )
          ) : (
            // Aggregate view with session list
            <div className="space-y-6">
              {/* Aggregate Summary */}
              {aggregate && (
                <div className="bg-card border border-border rounded-lg p-4 shadow-sm">
                  <h3 className="text-sm font-semibold text-foreground mb-4 flex items-center gap-2">
                    <Award className="w-4 h-4 text-primary" />
                    Aggregate Summary ({aggregate.total_sessions} sessions)
                  </h3>
                  <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                    <div className="bg-green-100 dark:bg-green-900/30 rounded-lg p-3">
                      <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1">Total Cost</div>
                      <div className="text-2xl font-bold text-green-600 dark:text-green-400">{formatUSD(aggregate.total_cost_usd)}</div>
                      <div className="text-xs text-muted-foreground mt-1">
                        {formatTokens(aggregate.total_input_tokens + aggregate.total_output_tokens)} tokens
                      </div>
                    </div>
                    <div className="bg-blue-100 dark:bg-blue-900/30 rounded-lg p-3">
                      <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1 flex items-center gap-1">
                        <TrendingUp className="w-3 h-3" />
                        Highest
                      </div>
                      <div className="text-2xl font-bold text-blue-600 dark:text-blue-400">{formatUSD(highestCost)}</div>
                    </div>
                    <div className="bg-purple-100 dark:bg-purple-900/30 rounded-lg p-3">
                      <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1 flex items-center gap-1">
                        <TrendingDown className="w-3 h-3" />
                        Lowest
                      </div>
                      <div className="text-2xl font-bold text-purple-600 dark:text-purple-400">{formatUSD(lowestCost)}</div>
                    </div>
                    <div className="bg-muted rounded-lg p-3">
                      <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1">Sessions</div>
                      <div className="text-2xl font-bold text-foreground">{aggregate.total_sessions}</div>
                      <div className="text-xs text-muted-foreground mt-1">
                        {aggregate.total_llm_calls} LLM calls
                      </div>
                    </div>
                  </div>
                </div>
              )}

              {/* Per-session expandable list */}
              <div className="space-y-3">
                {sessionsWithCosts.map((session) => {
                  const isExpanded = expandedSessions.has(session.session_id)

                  return (
                    <div
                      key={session.session_id}
                      className="border rounded-lg overflow-hidden bg-card border-border"
                    >
                      {/* Session Header */}
                      <button
                        onClick={() => toggleSession(session.session_id)}
                        className={`w-full flex items-center justify-between px-4 py-3 text-left transition-colors ${
                          isExpanded ? 'bg-accent/50' : 'hover:bg-accent/50'
                        }`}
                      >
                        <div className="flex items-center gap-3 flex-1 min-w-0">
                          {isExpanded ? (
                            <ChevronDown className="w-4 h-4 text-muted-foreground flex-shrink-0" />
                          ) : (
                            <ChevronRight className="w-4 h-4 text-muted-foreground flex-shrink-0" />
                          )}
                          <div className="min-w-0">
                            <div className="text-sm font-medium text-foreground truncate">
                              {session.title || 'Untitled Session'}
                            </div>
                            <div className="flex items-center gap-2 text-[10px] text-muted-foreground">
                              <span>{formatDate(session.created_at)}</span>
                              {session.agent_mode && (
                                <span className="px-1 py-0.5 rounded bg-muted text-muted-foreground">{session.agent_mode}</span>
                              )}
                              <span className={`px-1 py-0.5 rounded ${
                                session.status === 'active' || session.status === 'running'
                                  ? 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400'
                                  : 'bg-muted text-muted-foreground'
                              }`}>{session.status}</span>
                            </div>
                          </div>
                        </div>

                        {/* Cost Badge */}
                        <div className="flex items-center gap-3 flex-shrink-0 ml-4">
                          <div className="flex items-center gap-2 px-3 py-1.5 rounded-full bg-green-100 dark:bg-green-900/30">
                            <DollarSign className="w-4 h-4 text-green-600 dark:text-green-400" />
                            <span className="text-sm font-semibold text-green-600 dark:text-green-400">
                              {formatUSD(session.total_cost_usd)}
                            </span>
                            <span className="text-xs text-muted-foreground">
                              ({formatTokens(session.total_input_tokens + session.total_output_tokens)})
                            </span>
                          </div>
                        </div>
                      </button>

                      {/* Expanded Content */}
                      {isExpanded && renderSessionContent(session)}
                    </div>
                  )
                })}
              </div>
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="px-6 py-4 border-t border-border flex justify-end bg-background rounded-b-lg">
          <button
            onClick={onClose}
            className="px-4 py-2 bg-secondary text-secondary-foreground rounded-md hover:bg-secondary/80 transition-colors text-sm font-medium"
          >
            Close
          </button>
        </div>
      </div>
    </div>
  )
}

export default ChatCostsPopup
