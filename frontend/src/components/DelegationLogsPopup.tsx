import React, { useEffect, useState, useMemo, useCallback } from 'react'
import {
  X,
  Loader2,
  ChevronRight,
  ChevronDown,
  CheckCircle,
  XCircle,
  AlertCircle,
  Clock,
  Users,
  DollarSign,
  Coins,
  RefreshCw,
  MessageSquare,
  Cpu,
  Zap,
} from 'lucide-react'
import { agentApi } from '../services/api'
import { MarkdownRenderer } from './ui/MarkdownRenderer'
import { EventList } from './events'
import type {
  AllDelegationLogsResponse,
  SessionDelegationLogs,
  DelegationLogEntry,
  ChatModelUsage,
  PollingEvent,
} from '../services/api-types'

interface DelegationLogsPopupProps {
  isOpen: boolean
  onClose: () => void
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
const formatDate = (dateStr?: string) => {
  if (!dateStr) return ''
  const date = new Date(dateStr)
  return date.toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit'
  })
}

// Get reasoning level badge color
const getReasoningBadgeClass = (level?: string) => {
  switch (level) {
    case 'high':
      return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400'
    case 'medium':
      return 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400'
    case 'low':
      return 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400'
    default:
      return 'bg-gray-100 text-gray-700 dark:bg-gray-900/30 dark:text-gray-400'
  }
}

// Get status icon
const getStatusIcon = (status: string) => {
  switch (status) {
    case 'completed':
      return <CheckCircle className="w-4 h-4 text-green-500" />
    case 'failed':
      return <XCircle className="w-4 h-4 text-red-500" />
    case 'running':
      return <Loader2 className="w-4 h-4 text-blue-500 animate-spin" />
    default:
      return <Clock className="w-4 h-4 text-muted-foreground" />
  }
}

const DelegationLogsPopup: React.FC<DelegationLogsPopupProps> = ({
  isOpen,
  onClose,
}) => {
  const [loading, setLoading] = useState(false)
  const [allData, setAllData] = useState<AllDelegationLogsResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [timeFilter, setTimeFilter] = useState<'3d' | '7d' | '30d' | 'all'>('3d')
  const [expandedSessions, setExpandedSessions] = useState<Set<string>>(new Set())
  const [expandedMainAgents, setExpandedMainAgents] = useState<Set<string>>(new Set())
  const [expandedDelegations, setExpandedDelegations] = useState<Set<string>>(new Set())
  const [expandedModels, setExpandedModels] = useState<Set<string>>(new Set())

  // Delegation events drill-down state
  const [drillDownId, setDrillDownId] = useState<string | null>(null)
  const [drillDownEvents, setDrillDownEvents] = useState<PollingEvent[]>([])
  const [drillDownLoading, setDrillDownLoading] = useState(false)
  const [drillDownTotal, setDrillDownTotal] = useState(0)
  const [drillDownLoadingMore, setDrillDownLoadingMore] = useState(false)

  // Main agent events drill-down state
  const [mainAgentDrillDownId, setMainAgentDrillDownId] = useState<string | null>(null)
  const [mainAgentEvents, setMainAgentEvents] = useState<PollingEvent[]>([])
  const [mainAgentDrillDownLoading, setMainAgentDrillDownLoading] = useState(false)
  const [mainAgentTotal, setMainAgentTotal] = useState(0)
  const [mainAgentLoadingMore, setMainAgentLoadingMore] = useState(false)

  const EVENTS_PAGE_SIZE = 50

  // Load data when opened
  useEffect(() => {
    if (isOpen) {
      loadData()
    } else {
      setAllData(null)
      setError(null)
      setTimeFilter('3d')
      setExpandedSessions(new Set())
      setExpandedMainAgents(new Set())
      setExpandedDelegations(new Set())
      setExpandedModels(new Set())
      setDrillDownId(null)
      setDrillDownEvents([])
      setMainAgentDrillDownId(null)
      setMainAgentEvents([])
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isOpen])

  const loadData = async () => {
    setLoading(true)
    setError(null)
    setDrillDownId(null)
    setDrillDownEvents([])
    setMainAgentDrillDownId(null)
    setMainAgentEvents([])
    try {
      const data = await agentApi.getAllDelegationLogs()
      setAllData(data)
      if (!data) {
        setError('Failed to load delegation logs')
      }
    } catch (err) {
      console.error('Failed to load delegation logs:', err)
      setError('Failed to load delegation logs')
    } finally {
      setLoading(false)
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

  const toggleMainAgent = (sessionId: string) => {
    setExpandedMainAgents(prev => {
      const next = new Set(prev)
      if (next.has(sessionId)) {
        next.delete(sessionId)
      } else {
        next.add(sessionId)
      }
      return next
    })
  }

  const toggleDelegation = (id: string) => {
    setExpandedDelegations(prev => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
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

  // Load drill-down events for a delegation (paged)
  const loadDrillDownEvents = async (sessionId: string, delegationId: string) => {
    if (drillDownId === delegationId) {
      setDrillDownId(null)
      setDrillDownEvents([])
      setDrillDownTotal(0)
      return
    }
    setDrillDownId(delegationId)
    setDrillDownLoading(true)
    try {
      const data = await agentApi.getDelegationEvents(sessionId, delegationId, EVENTS_PAGE_SIZE, 0)
      setDrillDownEvents(data.events || [])
      setDrillDownTotal(data.total || 0)
    } catch (err) {
      console.error('Failed to load delegation events:', err)
      setDrillDownEvents([])
      setDrillDownTotal(0)
    } finally {
      setDrillDownLoading(false)
    }
  }

  // Load more delegation events
  const loadMoreDrillDownEvents = async (sessionId: string, delegationId: string) => {
    setDrillDownLoadingMore(true)
    try {
      const data = await agentApi.getDelegationEvents(sessionId, delegationId, EVENTS_PAGE_SIZE, drillDownEvents.length)
      setDrillDownEvents(prev => [...prev, ...(data.events || [])])
    } catch (err) {
      console.error('Failed to load more delegation events:', err)
    } finally {
      setDrillDownLoadingMore(false)
    }
  }

  // Load main agent events for a session (paged)
  const loadMainAgentEvents = async (sessionId: string) => {
    if (mainAgentDrillDownId === sessionId) {
      setMainAgentDrillDownId(null)
      setMainAgentEvents([])
      setMainAgentTotal(0)
      return
    }
    setMainAgentDrillDownId(sessionId)
    setMainAgentDrillDownLoading(true)
    try {
      const data = await agentApi.getChatSessionEvents(sessionId, EVENTS_PAGE_SIZE, 0)
      setMainAgentEvents(data.events || [])
      setMainAgentTotal(data.total || 0)
    } catch (err) {
      console.error('Failed to load main agent events:', err)
      setMainAgentEvents([])
      setMainAgentTotal(0)
    } finally {
      setMainAgentDrillDownLoading(false)
    }
  }

  // Load more main agent events
  const loadMoreMainAgentEvents = async (sessionId: string) => {
    setMainAgentLoadingMore(true)
    try {
      const data = await agentApi.getChatSessionEvents(sessionId, EVENTS_PAGE_SIZE, mainAgentEvents.length)
      setMainAgentEvents(prev => [...prev, ...(data.events || [])])
    } catch (err) {
      console.error('Failed to load more main agent events:', err)
    } finally {
      setMainAgentLoadingMore(false)
    }
  }

  // Filter sessions by time range
  const filteredSessions = useMemo(() => {
    if (!allData) return []
    if (timeFilter === 'all') return allData.sessions

    const days = timeFilter === '3d' ? 3 : timeFilter === '7d' ? 7 : 30
    const cutoff = new Date()
    cutoff.setDate(cutoff.getDate() - days)

    return allData.sessions.filter(s => {
      if (!s.created_at) return true // keep sessions without dates
      return new Date(s.created_at) >= cutoff
    })
  }, [allData, timeFilter])

  const hiddenCount = (allData?.sessions.length ?? 0) - filteredSessions.length

  // Summary stats (based on filtered sessions)
  const summary = useMemo(() => {
    if (!allData) return null
    let totalDelegations = 0
    let running = 0
    let completed = 0
    let failed = 0
    let totalCost = 0
    let totalTokens = 0
    for (const session of filteredSessions) {
      totalDelegations += session.delegations.length
      totalCost += session.total_cost_usd || 0
      totalTokens += (session.total_input_tokens || 0) + (session.total_output_tokens || 0)
      for (const d of session.delegations) {
        if (d.status === 'running') running++
        else if (d.status === 'completed') completed++
        else if (d.status === 'failed') failed++
      }
    }
    return {
      totalSessions: filteredSessions.length,
      totalDelegations,
      running,
      completed,
      failed,
      totalCost,
      totalTokens,
    }
  }, [allData, filteredSessions])


  if (!isOpen) return null

  // Render model table for cost breakdown
  const renderModelTable = (
    byModel: Record<string, ChatModelUsage>,
    keyPrefix: string,
  ) => {
    const models = Object.entries(byModel)
    if (models.length === 0) return null

    return (
      <table className="w-full text-xs">
        <thead>
          <tr className="text-muted-foreground border-b border-border pb-2">
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

            return (
              <tr key={modelId} className="hover:bg-accent/50 transition-colors">
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
            )
          })}
        </tbody>
        <tfoot>
          <tr className="border-t-2 border-border font-bold">
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

  // Render delegation expanded content
  const renderDelegationContent = (delegation: DelegationLogEntry, sessionId: string) => {
    return (
      <div className="border-t border-border divide-y divide-border">
        {/* Instruction */}
        <div className="p-4 bg-muted/20">
          <div className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider mb-1.5">
            Instruction
          </div>
          <div className="text-sm text-foreground">
            <MarkdownRenderer content={delegation.instruction} />
          </div>
        </div>

        {/* Metadata Grid */}
        <div className="p-4 bg-background">
          <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
            {delegation.model_id && (
              <div>
                <div className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider mb-0.5">Model</div>
                <div className="text-xs font-mono text-foreground">{delegation.model_id}</div>
              </div>
            )}
            {delegation.reasoning_level && (
              <div>
                <div className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider mb-0.5">Reasoning</div>
                <span className={`text-[10px] font-medium px-1.5 py-0.5 rounded ${getReasoningBadgeClass(delegation.reasoning_level)}`}>
                  {delegation.reasoning_level}
                </span>
              </div>
            )}
            {delegation.tool_mode && (
              <div>
                <div className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider mb-0.5">Tool Mode</div>
                <div className="text-xs text-foreground">{delegation.tool_mode}</div>
              </div>
            )}
            {delegation.duration && (
              <div>
                <div className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider mb-0.5">Duration</div>
                <div className="text-xs text-foreground">{delegation.duration}</div>
              </div>
            )}
          </div>

          {/* Servers */}
          {delegation.servers && delegation.servers.length > 0 && (
            <div className="mt-3">
              <div className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider mb-1">Servers</div>
              <div className="flex flex-wrap gap-1">
                {delegation.servers.map(s => (
                  <span key={s} className="text-[10px] font-mono bg-muted px-1.5 py-0.5 rounded text-muted-foreground border border-border">
                    {s}
                  </span>
                ))}
              </div>
            </div>
          )}
        </div>

        {/* Token Usage per model */}
        {delegation.token_usage && Object.keys(delegation.token_usage).length > 0 && (
          <div className="p-4 bg-background">
            <div className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider mb-2">Token Usage by Model</div>
            <div className="overflow-x-auto">
              {renderModelTable(delegation.token_usage, `deleg-${delegation.delegation_id}`)}
            </div>
          </div>
        )}

        {/* Result / Error */}
        {delegation.result && (
          <div className="p-4 bg-background">
            <div className="text-[10px] font-semibold text-green-600 dark:text-green-400 uppercase tracking-wider mb-1.5 flex items-center gap-1">
              <CheckCircle className="w-3 h-3" /> Result
            </div>
            <p className="text-xs text-foreground whitespace-pre-wrap max-h-[200px] overflow-y-auto bg-green-500/5 p-2 rounded border border-green-500/10">
              {delegation.result}
            </p>
          </div>
        )}

        {delegation.error && (
          <div className="p-4 bg-background">
            <div className="text-[10px] font-semibold text-red-600 dark:text-red-400 uppercase tracking-wider mb-1.5 flex items-center gap-1">
              <XCircle className="w-3 h-3" /> Error
            </div>
            <p className="text-xs text-red-700 dark:text-red-300 whitespace-pre-wrap max-h-[200px] overflow-y-auto bg-red-500/5 p-2 rounded border border-red-500/10">
              {delegation.error}
            </p>
          </div>
        )}

        {/* View Conversation Button */}
        <div className="p-4 bg-background">
          <button
            onClick={() => loadDrillDownEvents(sessionId, delegation.delegation_id)}
            disabled={drillDownLoading && drillDownId === delegation.delegation_id}
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium text-purple-700 dark:text-purple-300 bg-purple-100 dark:bg-purple-900/30 hover:bg-purple-200 dark:hover:bg-purple-900/50 rounded transition-colors border border-purple-200 dark:border-purple-800"
          >
            {drillDownLoading && drillDownId === delegation.delegation_id ? (
              <Loader2 className="w-3 h-3 animate-spin" />
            ) : (
              <MessageSquare className="w-3 h-3" />
            )}
            {drillDownId === delegation.delegation_id ? 'Hide Conversation' : 'View Conversation'}
          </button>

          {/* Drill-down events */}
          {drillDownId === delegation.delegation_id && !drillDownLoading && (
            <div className="mt-3 border border-border rounded-lg overflow-hidden bg-muted/20">
              <div className="bg-muted/50 px-4 py-2 border-b border-border flex items-center justify-between">
                <h4 className="text-xs font-semibold text-foreground">Sub-Agent Events ({drillDownEvents.length}{drillDownTotal > drillDownEvents.length ? ` of ${drillDownTotal}` : ''})</h4>
              </div>
              <div className="max-h-[600px] overflow-y-auto">
                {drillDownEvents.length === 0 ? (
                  <p className="text-xs text-muted-foreground p-3 text-center">No events found for this delegation.</p>
                ) : (
                  <>
                      <EventList events={drillDownEvents} compact />
                    {drillDownTotal > drillDownEvents.length && (
                      <div className="flex justify-center py-3 border-t border-border">
                        <button
                          onClick={() => loadMoreDrillDownEvents(sessionId, delegation.delegation_id)}
                          disabled={drillDownLoadingMore}
                          className="px-3 py-1.5 text-xs font-medium text-purple-700 dark:text-purple-300 bg-purple-50 dark:bg-purple-900/20 hover:bg-purple-100 dark:hover:bg-purple-900/40 rounded transition-colors border border-purple-200 dark:border-purple-800"
                        >
                          {drillDownLoadingMore ? (
                            <span className="flex items-center gap-1.5"><Loader2 className="w-3 h-3 animate-spin" /> Loading...</span>
                          ) : (
                            `Load More (${drillDownTotal - drillDownEvents.length} remaining)`
                          )}
                        </button>
                      </div>
                    )}
                  </>
                )}
              </div>
            </div>
          )}
        </div>
      </div>
    )
  }

  // Render main agent section within a session
  const renderMainAgent = (session: SessionDelegationLogs) => {
    const ma = session.main_agent
    if (!ma) return null
    const isExpanded = expandedMainAgents.has(session.session_id)

    return (
      <div className="border border-border rounded-lg overflow-hidden bg-card">
        <button
          onClick={() => toggleMainAgent(session.session_id)}
          className={`w-full flex items-center justify-between px-4 py-2.5 text-left transition-colors ${
            isExpanded ? 'bg-blue-50 dark:bg-blue-900/20' : 'hover:bg-accent/50'
          }`}
        >
          <div className="flex items-center gap-2.5">
            {isExpanded ? (
              <ChevronDown className="w-3.5 h-3.5 text-muted-foreground flex-shrink-0" />
            ) : (
              <ChevronRight className="w-3.5 h-3.5 text-muted-foreground flex-shrink-0" />
            )}
            <Cpu className="w-4 h-4 text-blue-500 flex-shrink-0" />
            <span className="text-sm font-medium text-foreground">Main Agent</span>
            <div className="flex items-center gap-3 text-[11px] text-muted-foreground ml-2">
              <span>{ma.llm_calls} calls</span>
              <span>{formatTokens(ma.input_tokens)} in / {formatTokens(ma.output_tokens)} out</span>
            </div>
          </div>
          {ma.total_cost_usd > 0 && (
            <div className="flex items-center gap-1.5 px-2 py-0.5 rounded-full bg-green-100 dark:bg-green-900/30 flex-shrink-0">
              <DollarSign className="w-3 h-3 text-green-600 dark:text-green-400" />
              <span className="text-xs font-semibold text-green-600 dark:text-green-400">
                {formatUSD(ma.total_cost_usd)}
              </span>
            </div>
          )}
        </button>
        {isExpanded && (
          <div className="border-t border-border divide-y divide-border">
            {ma.by_model && Object.keys(ma.by_model).length > 0 && (
              <div className="p-4 overflow-x-auto">
                {renderModelTable(ma.by_model, `main-${session.session_id}`)}
              </div>
            )}
            {/* View Main Agent Conversation */}
            <div className="p-4 bg-background">
              <button
                onClick={() => loadMainAgentEvents(session.session_id)}
                disabled={mainAgentDrillDownLoading && mainAgentDrillDownId === session.session_id}
                className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium text-blue-700 dark:text-blue-300 bg-blue-100 dark:bg-blue-900/30 hover:bg-blue-200 dark:hover:bg-blue-900/50 rounded transition-colors border border-blue-200 dark:border-blue-800"
              >
                {mainAgentDrillDownLoading && mainAgentDrillDownId === session.session_id ? (
                  <Loader2 className="w-3 h-3 animate-spin" />
                ) : (
                  <MessageSquare className="w-3 h-3" />
                )}
                {mainAgentDrillDownId === session.session_id ? 'Hide Conversation' : 'View Conversation'}
              </button>

              {mainAgentDrillDownId === session.session_id && !mainAgentDrillDownLoading && (
                <div className="mt-3 border border-border rounded-lg overflow-hidden bg-muted/20">
                  <div className="bg-muted/50 px-4 py-2 border-b border-border flex items-center justify-between">
                    <h4 className="text-xs font-semibold text-foreground">Main Agent Events ({mainAgentEvents.length}{mainAgentTotal > mainAgentEvents.length ? ` of ${mainAgentTotal}` : ''})</h4>
                  </div>
                  <div className="max-h-[600px] overflow-y-auto">
                    {mainAgentEvents.length === 0 ? (
                      <p className="text-xs text-muted-foreground p-3 text-center">No events found for this session.</p>
                    ) : (
                      <>
                          <EventList events={mainAgentEvents} compact />
                        {mainAgentTotal > mainAgentEvents.length && (
                          <div className="flex justify-center py-3 border-t border-border">
                            <button
                              onClick={() => loadMoreMainAgentEvents(session.session_id)}
                              disabled={mainAgentLoadingMore}
                              className="px-3 py-1.5 text-xs font-medium text-blue-700 dark:text-blue-300 bg-blue-50 dark:bg-blue-900/20 hover:bg-blue-100 dark:hover:bg-blue-900/40 rounded transition-colors border border-blue-200 dark:border-blue-800"
                            >
                              {mainAgentLoadingMore ? (
                                <span className="flex items-center gap-1.5"><Loader2 className="w-3 h-3 animate-spin" /> Loading...</span>
                              ) : (
                                `Load More (${mainAgentTotal - mainAgentEvents.length} remaining)`
                              )}
                            </button>
                          </div>
                        )}
                      </>
                    )}
                  </div>
                </div>
              )}
            </div>
          </div>
        )}
      </div>
    )
  }

  // Render a session card
  const renderSession = (session: SessionDelegationLogs) => {
    const isExpanded = expandedSessions.has(session.session_id)
    const delegationCount = session.delegations.length

    return (
      <div
        key={session.session_id}
        className="border border-border rounded-lg overflow-hidden bg-card shadow-sm"
      >
        {/* Session Header */}
        <button
          onClick={() => toggleSession(session.session_id)}
          className={`w-full flex items-center justify-between px-5 py-3.5 text-left transition-colors ${
            isExpanded ? 'bg-accent/50' : 'hover:bg-accent/50'
          }`}
        >
          <div className="flex items-center gap-3 flex-1 min-w-0">
            {isExpanded ? (
              <ChevronDown className="w-4 h-4 text-muted-foreground flex-shrink-0" />
            ) : (
              <ChevronRight className="w-4 h-4 text-muted-foreground flex-shrink-0" />
            )}

            {getStatusIcon(session.status)}

            <div className="min-w-0 flex-1">
              <div className="text-sm font-medium text-foreground line-clamp-1">
                {session.title || 'Untitled Session'}
              </div>
              <div className="flex items-center gap-2 mt-0.5 text-[10px] text-muted-foreground">
                <span>{formatDate(session.created_at)}</span>
                <span className="font-medium bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300 px-1.5 py-0.5 rounded">
                  {delegationCount} delegation{delegationCount !== 1 ? 's' : ''}
                </span>
              </div>
            </div>
          </div>

          <div className="flex items-center gap-3 flex-shrink-0 ml-4">
            <div className="text-xs text-muted-foreground hidden sm:block">
              {formatTokens(session.total_input_tokens)} in / {formatTokens(session.total_output_tokens)} out
            </div>
            {session.total_cost_usd > 0 && (
              <div className="flex items-center gap-1.5 px-2.5 py-1 rounded-full bg-green-100 dark:bg-green-900/30">
                <DollarSign className="w-3.5 h-3.5 text-green-600 dark:text-green-400" />
                <span className="text-xs font-semibold text-green-600 dark:text-green-400">
                  {formatUSD(session.total_cost_usd)}
                </span>
              </div>
            )}
          </div>
        </button>

        {/* Session Expanded Content */}
        {isExpanded && (
          <div className="border-t border-border p-4 space-y-4 bg-muted/10">
            {/* Main Agent */}
            {renderMainAgent(session)}

            {/* Sub-Agent Delegations */}
            {delegationCount > 0 && (
              <div>
                <div className="flex items-center gap-2 mb-3">
                  <Users className="w-4 h-4 text-purple-500" />
                  <h4 className="text-xs font-semibold text-foreground uppercase tracking-wider">
                    Sub-Agent Delegations ({delegationCount})
                  </h4>
                </div>
                <div className="space-y-3">
                  {session.delegations.map((delegation, idx) => {
                    const isDelegExpanded = expandedDelegations.has(delegation.delegation_id)
                    const depthIndent = delegation.depth > 0 ? { marginLeft: `${delegation.depth * 24}px` } : {}
                    const depthBorder = delegation.depth > 0 ? 'border-l-4 border-l-purple-500/50' : ''

                    return (
                      <div
                        key={delegation.delegation_id || idx}
                        className={`border border-border rounded-lg overflow-hidden bg-card ${depthBorder}`}
                        style={depthIndent}
                      >
                        <button
                          onClick={() => toggleDelegation(delegation.delegation_id)}
                          className={`w-full flex items-center justify-between px-4 py-3 text-left transition-colors ${
                            isDelegExpanded ? 'bg-accent/50' : 'hover:bg-accent/50'
                          }`}
                        >
                          <div className="flex items-center gap-3 flex-1 min-w-0">
                            {isDelegExpanded ? (
                              <ChevronDown className="w-4 h-4 text-muted-foreground flex-shrink-0" />
                            ) : (
                              <ChevronRight className="w-4 h-4 text-muted-foreground flex-shrink-0" />
                            )}

                            {getStatusIcon(delegation.status)}

                            <div className="min-w-0 flex-1">
                              <div className="text-sm font-medium text-foreground line-clamp-1">
                                {delegation.instruction}
                              </div>
                              <div className="flex items-center gap-2 mt-0.5 text-[10px] text-muted-foreground">
                                <span>{formatDate(delegation.start_time)}</span>
                                {delegation.model_id && (
                                  <span className="font-mono bg-muted px-1 py-0.5 rounded border border-border">
                                    {delegation.model_id}
                                  </span>
                                )}
                                {delegation.reasoning_level && (
                                  <span className={`font-medium px-1 py-0.5 rounded ${getReasoningBadgeClass(delegation.reasoning_level)}`}>
                                    {delegation.reasoning_level}
                                  </span>
                                )}
                                {delegation.duration && (
                                  <span className="flex items-center gap-0.5">
                                    <Clock className="w-2.5 h-2.5" />
                                    {delegation.duration}
                                  </span>
                                )}
                              </div>
                            </div>
                          </div>

                          <div className="flex items-center gap-3 flex-shrink-0 ml-4">
                            <div className="text-xs text-muted-foreground hidden sm:block">
                              {formatTokens(delegation.input_tokens)} in / {formatTokens(delegation.output_tokens)} out
                              {delegation.tool_calls > 0 && ` / ${delegation.tool_calls} tools`}
                            </div>
                            {delegation.total_cost_usd > 0 && (
                              <div className="flex items-center gap-1.5 px-2.5 py-1 rounded-full bg-green-100 dark:bg-green-900/30">
                                <DollarSign className="w-3.5 h-3.5 text-green-600 dark:text-green-400" />
                                <span className="text-xs font-semibold text-green-600 dark:text-green-400">
                                  {formatUSD(delegation.total_cost_usd)}
                                </span>
                              </div>
                            )}
                          </div>
                        </button>

                        {isDelegExpanded && renderDelegationContent(delegation, session.session_id)}
                      </div>
                    )
                  })}
                </div>
              </div>
            )}

            {delegationCount === 0 && (
              <p className="text-xs text-muted-foreground text-center py-2">No sub-agent delegations in this session.</p>
            )}
          </div>
        )}
      </div>
    )
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm p-4">
      <div className="bg-background rounded-lg shadow-xl w-full max-w-[90vw] h-[95vh] flex flex-col border border-border relative">
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-border">
          <div className="flex-1 min-w-0">
            <h2 className="text-lg font-semibold text-foreground flex items-center gap-2">
              <Users className="w-5 h-5 text-purple-500" />
              Execution Logs
            </h2>
            <div className="flex items-center gap-4 mt-1">
              {summary && (
                <div className="flex items-center gap-3 text-xs">
                  <div className="flex items-center gap-1.5 text-foreground font-medium">
                    <Zap className="w-3.5 h-3.5 text-purple-500" />
                    {summary.totalSessions} session{summary.totalSessions !== 1 ? 's' : ''}
                  </div>
                  <div className="text-muted-foreground">|</div>
                  <div className="flex items-center gap-1.5 text-foreground">
                    {summary.totalDelegations} delegation{summary.totalDelegations !== 1 ? 's' : ''}
                  </div>
                  {summary.completed > 0 && (
                    <div className="flex items-center gap-1 text-green-600 dark:text-green-400">
                      <CheckCircle className="w-3 h-3" /> {summary.completed}
                    </div>
                  )}
                  {summary.failed > 0 && (
                    <div className="flex items-center gap-1 text-red-600 dark:text-red-400">
                      <XCircle className="w-3 h-3" /> {summary.failed}
                    </div>
                  )}
                  {summary.running > 0 && (
                    <div className="flex items-center gap-1 text-blue-600 dark:text-blue-400">
                      <Loader2 className="w-3 h-3 animate-spin" /> {summary.running}
                    </div>
                  )}
                  <div className="flex items-center gap-1.5 text-green-600 dark:text-green-400 font-medium">
                    <DollarSign className="w-3.5 h-3.5" />
                    {formatUSD(summary.totalCost)}
                  </div>
                  <div className="flex items-center gap-1.5 text-muted-foreground">
                    <Coins className="w-3.5 h-3.5" />
                    {formatTokens(summary.totalTokens)} tokens
                  </div>
                </div>
              )}
              <button
                onClick={() => loadData()}
                disabled={loading}
                className="p-1.5 rounded-md hover:bg-muted transition-colors text-muted-foreground hover:text-foreground"
                title="Refresh"
              >
                <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
              </button>
            </div>
          </div>

          {/* Time filter chips */}
          <div className="flex items-center gap-1 bg-muted rounded-md p-0.5 mr-4">
            {([['3d', '3 Days'], ['7d', '7 Days'], ['30d', '30 Days'], ['all', 'All']] as const).map(([value, label]) => (
              <button
                key={value}
                onClick={() => setTimeFilter(value)}
                className={`px-2.5 py-1 text-xs font-medium rounded transition-colors ${
                  timeFilter === value
                    ? 'bg-background text-foreground shadow-sm'
                    : 'text-muted-foreground hover:text-foreground'
                }`}
              >
                {label}
              </button>
            ))}
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
              <p>Loading execution logs...</p>
            </div>
          ) : error ? (
            <div className="flex flex-col items-center justify-center py-12 text-destructive">
              <AlertCircle className="w-12 h-12 mb-3" />
              <p>{error}</p>
              <button
                onClick={() => loadData()}
                className="mt-4 px-4 py-2 bg-destructive/10 text-destructive rounded-md hover:bg-destructive/20 transition-colors text-sm font-medium"
              >
                Retry
              </button>
            </div>
          ) : !allData || allData.sessions.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
              <Users className="w-12 h-12 mb-3 opacity-50" />
              <p className="text-sm font-medium">No execution logs found</p>
              <p className="text-xs mt-2 opacity-70">Run delegations in multi-agent mode to see logs here.</p>
            </div>
          ) : filteredSessions.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
              <Clock className="w-12 h-12 mb-3 opacity-50" />
              <p className="text-sm font-medium">No sessions in the last {timeFilter === '3d' ? '3 days' : timeFilter === '7d' ? '7 days' : '30 days'}</p>
              <button
                onClick={() => setTimeFilter('all')}
                className="mt-3 px-4 py-2 bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300 rounded-md hover:bg-purple-200 dark:hover:bg-purple-900/50 transition-colors text-sm font-medium"
              >
                Show All Sessions
              </button>
            </div>
          ) : (
            <div className="space-y-4">
              {filteredSessions.map(session => renderSession(session))}
              {hiddenCount > 0 && (
                <div className="flex justify-center pt-2">
                  <button
                    onClick={() => setTimeFilter('all')}
                    className="px-4 py-2 text-xs font-medium text-muted-foreground hover:text-foreground bg-muted hover:bg-muted/80 rounded-md transition-colors"
                  >
                    Show {hiddenCount} older session{hiddenCount !== 1 ? 's' : ''}
                  </button>
                </div>
              )}
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

export default DelegationLogsPopup
