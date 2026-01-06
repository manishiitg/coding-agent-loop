import React, { useEffect, useState, useMemo } from 'react'
import {
  X,
  Loader2,
  ChevronRight,
  ChevronDown,
  CheckCircle,
  XCircle,
  AlertCircle,
  FileText,
  Clock,
  Terminal,
  MessageSquare,
  GitBranch,
  Network,
  Bot,
  User,
  Split,
  BookOpen,
  History,
  DollarSign,
  Coins,
  Cpu,
  List
} from 'lucide-react'
import { agentApi } from '../../services/api'
import type { ExecutionLogsResponse, StepExecutionLogs, ValidationLog, ArchivedLogEntry, ModelTokenUsage } from '../../services/api-types'

interface ExecutionLogsPopupProps {
  isOpen: boolean
  onClose: () => void
  workspacePath: string | null
  runFolder: string | null
}

const getStepIcon = (type: string) => {
  switch (type) {
    case 'orchestration':
      return <Network className="w-4 h-4 text-purple-500" />
    case 'conditional':
    case 'decision':
      return <GitBranch className="w-4 h-4 text-blue-500" />
    case 'human_input':
      return <User className="w-4 h-4 text-orange-500" />
    case 'sub-agent':
      return <Bot className="w-4 h-4 text-indigo-500" />
    case 'branch':
      return <Split className="w-4 h-4 text-cyan-500" />
    case 'decision-inner':
      return <Terminal className="w-4 h-4 text-blue-400" />
    default:
      return <Terminal className="w-4 h-4 text-muted-foreground" />
  }
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

// Parse step ID into sortable segments
// step-1 → [1]
// step-1-true-0 → [1, 'true', 0]
// step-8-sub-agent-2 → [8, 'sub-agent', 2]
// step-1-true-0-sub-agent-1 → [1, 'true', 0, 'sub-agent', 1]
const parseStepId = (stepId: string): (string | number)[] => {
  const segments: (string | number)[] = []

  // Remove 'step-' prefix and split by patterns
  const withoutPrefix = stepId.replace(/^step-/, '')

  // Match: number, or 'true', or 'false', or 'sub-agent'
  const pattern = /(\d+|true|false|sub-agent)/g
  let match
  while ((match = pattern.exec(withoutPrefix)) !== null) {
    const val = match[1]
    if (val === 'true' || val === 'false' || val === 'sub-agent') {
      segments.push(val)
    } else {
      segments.push(parseInt(val, 10))
    }
  }

  return segments
}

// Sort step IDs so nested items appear after their parent
const sortStepIds = (a: string, b: string): number => {
  const segA = parseStepId(a)
  const segB = parseStepId(b)

  const minLen = Math.min(segA.length, segB.length)

  for (let i = 0; i < minLen; i++) {
    const valA = segA[i]
    const valB = segB[i]

    // Both numbers - compare numerically
    if (typeof valA === 'number' && typeof valB === 'number') {
      if (valA !== valB) return valA - valB
    }
    // Both strings - compare alphabetically
    else if (typeof valA === 'string' && typeof valB === 'string') {
      if (valA !== valB) return valA.localeCompare(valB)
    }
    // Mixed - numbers come before strings
    else if (typeof valA === 'number') {
      return -1
    } else {
      return 1
    }
  }

  // Shorter one (parent) comes first
  return segA.length - segB.length
}

// Calculate nesting level based on step ID pattern
const getStepNestingLevel = (stepId: string): number => {
  const segments = parseStepId(stepId)
  let level = 0

  for (const seg of segments) {
    if (seg === 'true' || seg === 'false' || seg === 'sub-agent') {
      level++
    }
  }

  return level
}

// Determine the nesting context (what type of parent this is nested under)
const getStepNestingContext = (stepId: string): 'none' | 'branch' | 'sub-agent' => {
  // Check the last nesting indicator in the ID
  const lastBranchIndex = Math.max(stepId.lastIndexOf('-true-'), stepId.lastIndexOf('-false-'))
  const lastSubAgentIndex = stepId.lastIndexOf('-sub-agent-')

  if (lastBranchIndex === -1 && lastSubAgentIndex === -1) {
    return 'none'
  }

  // Return whichever comes last (most immediate parent)
  if (lastSubAgentIndex > lastBranchIndex) {
    return 'sub-agent'
  }
  return 'branch'
}

// Get the indentation style for a step based on its nesting level
const getStepIndentStyle = (level: number): React.CSSProperties => {
  if (level === 0) return {}
  return { marginLeft: `${level * 24}px` }
}

// Get additional CSS class for nested steps (colored left border)
const getStepNestingClass = (stepId: string): string => {
  const context = getStepNestingContext(stepId)

  switch (context) {
    case 'sub-agent':
      return 'border-l-4 border-l-purple-500/50'
    case 'branch':
      return 'border-l-4 border-l-cyan-500/50'
    default:
      return ''
  }
}

const ExecutionLogsPopup: React.FC<ExecutionLogsPopupProps> = ({
  isOpen,
  onClose,
  workspacePath,
  runFolder
}) => {
  const [loading, setLoading] = useState(false)
  const [logs, setLogs] = useState<ExecutionLogsResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [expandedSteps, setExpandedSteps] = useState<Set<string>>(new Set())
  const [expandedValidations, setExpandedValidations] = useState<Set<string>>(new Set())
  const [expandedExecutions, setExpandedExecutions] = useState<Set<string>>(new Set())
  const [expandedArchived, setExpandedArchived] = useState<Set<string>>(new Set())
  const [showCostBreakdown, setShowCostBreakdown] = useState(false)
  const [expandedCostModels, setExpandedCostModels] = useState<Set<string>>(new Set())
  
  // State for viewing full file content (conversation logs)
  const [viewingFile, setViewingFile] = useState<{path: string, title: string, content: string} | null>(null)
  const [loadingFiles, setLoadingFiles] = useState<Set<string>>(new Set())

  useEffect(() => {
    if (isOpen && workspacePath) {
      loadLogs()
    } else {
      setLogs(null)
      setError(null)
      setViewingFile(null)
      setExpandedCostModels(new Set())
    }
  }, [isOpen, workspacePath, runFolder])

  const loadLogs = async () => {
    if (!workspacePath) return
    
    setLoading(true)
    setError(null)
    try {
      // Use selected run folder or default to logs/ if not selected (handled by backend)
      const data = await agentApi.getExecutionLogs(workspacePath, runFolder || '')
      setLogs(data)
      
      // Auto-expand steps with failures or recent activity
      const newExpanded = new Set<string>()
      Object.entries(data.steps).forEach(([stepId, stepLogs]) => {
        // Expand if any validation failed
        const hasFailure = stepLogs.validations.some(v => v.content?.execution_status === 'FAILED')
        if (hasFailure) {
          newExpanded.add(stepId)
        }
      })
      setExpandedSteps(newExpanded)
    } catch (err) {
      console.error('Failed to load execution logs:', err)
      setError('Failed to load execution logs')
    } finally {
      setLoading(false)
    }
  }

  // Calculate total costs and tokens
  const costSummary = useMemo(() => {
    if (!logs?.token_usage?.by_model) return null

    let totalCost = 0
    let totalInputTokens = 0
    let totalOutputTokens = 0
    let totalLLMCalls = 0
    let totalCacheReadTokens = 0
    let totalCacheWriteTokens = 0
    let totalReasoningTokens = 0
    
    // Stage costs
    const stageCosts = {
      execution: 0,
      validation: 0,
      learning: 0,
      other: 0
    }

    // Calculate totals from by_model
    if (logs.token_usage.by_model) {
      Object.values(logs.token_usage.by_model).forEach(usage => {
        totalCost += usage.total_cost_usd || 0
        totalInputTokens += usage.input_tokens || 0
        totalOutputTokens += usage.output_tokens || 0
        totalLLMCalls += usage.llm_call_count || 0
        totalCacheReadTokens += usage.cache_read_tokens || usage.cache_tokens || 0
        totalCacheWriteTokens += usage.cache_write_tokens || 0
        totalReasoningTokens += usage.reasoning_tokens || 0
      })
    }

    // Calculate stage costs from by_step_and_model
    if (logs.token_usage.by_step_and_model) {
      Object.entries(logs.token_usage.by_step_and_model).forEach(([key, modelMap]) => {
        const phase = key.split(':')[0]
        let cost = 0
        Object.values(modelMap).forEach(u => cost += u.total_cost_usd || 0)

        if (phase === 'execution_only') {
          stageCosts.execution += cost
        } else if (phase === 'validation') {
          stageCosts.validation += cost
        } else if (phase === 'success_learning' || phase === 'failure_learning') {
          stageCosts.learning += cost
        } else {
          stageCosts.other += cost
        }
      })
    }

    return {
      totalCost,
      totalInputTokens,
      totalOutputTokens,
      totalTokens: totalInputTokens + totalOutputTokens,
      totalLLMCalls,
      totalCacheReadTokens,
      totalCacheWriteTokens,
      totalReasoningTokens,
      stageCosts
    }
  }, [logs?.token_usage])

  const toggleStep = (stepId: string) => {
    setExpandedSteps(prev => {
      const next = new Set(prev)
      if (next.has(stepId)) {
        next.delete(stepId)
      } else {
        next.add(stepId)
      }
      return next
    })
  }

  const toggleCostModel = (modelId: string) => {
    setExpandedCostModels(prev => {
      const next = new Set(prev)
      if (next.has(modelId)) {
        next.delete(modelId)
      } else {
        next.add(modelId)
      }
      return next
    })
  }

  const toggleValidation = (id: string) => {
    setExpandedValidations(prev => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      }
      else {
        next.add(id)
      }
      return next
    })
  }

  const handleViewConversation = async (path: string, title: string) => {
    setLoadingFiles(prev => new Set(prev).add(path))
    try {
      const content = await agentApi.getLogFile(path)
      // If content is object, stringify
      const contentStr = typeof content === 'string' ? content : JSON.stringify(content, null, 2)
      
      // Check file size (approximate)
      if (contentStr.length > 1024 * 1024) { // 1MB limit
        if (!window.confirm(`The log file is large (${(contentStr.length / 1024 / 1024).toFixed(2)} MB). Loading it may freeze your browser. Continue?`)) {
            return;
        }
      }
      
      setViewingFile({ path, title, content: contentStr })
    } catch (e) {
      console.error(e)
      alert("Failed to load conversation log")
    } finally {
      setLoadingFiles(prev => {
        const next = new Set(prev)
        next.delete(path)
        return next
      })
    }
  }

  if (!isOpen) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm p-4">
      <div className="bg-background rounded-lg shadow-xl w-full max-w-6xl max-h-[90vh] flex flex-col border border-border relative">
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-border">
          <div className="flex-1 min-w-0">
            <h2 className="text-lg font-semibold text-foreground flex items-center gap-2">
              <Terminal className="w-5 h-5 text-primary" />
              Execution Logs
            </h2>
            <div className="flex items-center gap-4 mt-1">
              <p className="text-sm text-muted-foreground whitespace-nowrap">
                Run: <span className="font-mono text-xs bg-muted px-1.5 py-0.5 rounded text-foreground">{runFolder || 'Default'}</span>
              </p>
              {costSummary && (
                <div className="flex items-center gap-3 text-xs border-l border-border pl-4">
                  <div className="flex items-center gap-1.5 text-green-600 dark:text-green-400 font-medium">
                    <DollarSign className="w-3.5 h-3.5" />
                    {formatUSD(costSummary.totalCost)}
                  </div>
                  <div className="flex items-center gap-1.5 text-muted-foreground">
                    <Coins className="w-3.5 h-3.5" />
                    {formatTokens(costSummary.totalTokens)} tokens
                  </div>
                  <button 
                    onClick={() => setShowCostBreakdown(!showCostBreakdown)}
                    className="text-primary hover:underline ml-1"
                  >
                    {showCostBreakdown ? 'Hide Details' : 'View Details'}
                  </button>
                </div>
              )}
            </div>
          </div>
          <button 
            onClick={onClose}
            className="p-2 rounded-full hover:bg-accent hover:text-accent-foreground transition-colors ml-4"
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
                onClick={loadLogs}
                className="mt-4 px-4 py-2 bg-destructive/10 text-destructive rounded-md hover:bg-destructive/20 transition-colors text-sm font-medium"
              >
                Retry
              </button>
            </div>
          ) : !logs || Object.keys(logs.steps).length === 0 ? (
            <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
              <FileText className="w-12 h-12 mb-3 opacity-50" />
              <p>No execution logs found for this run.</p>
            </div>
          ) : (
            <div className="space-y-4">
              {/* Cost Breakdown Section */}
              {showCostBreakdown && logs?.token_usage && (
                <div className="space-y-4 mb-6 animate-in fade-in slide-in-from-top-2 duration-300">
                  
                  {/* Stage Summary Cards */}
                  {costSummary?.stageCosts && (
                    <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
                      <div className="bg-card border border-border rounded-lg p-3 shadow-sm">
                        <div className="text-xs text-muted-foreground font-medium mb-1 uppercase tracking-wider">Execution</div>
                        <div className="text-lg font-bold text-foreground">{formatUSD(costSummary.stageCosts.execution)}</div>
                        <div className="text-[10px] text-muted-foreground">Core Agent Logic</div>
                      </div>
                      <div className="bg-card border border-border rounded-lg p-3 shadow-sm">
                        <div className="text-xs text-muted-foreground font-medium mb-1 uppercase tracking-wider">Validation</div>
                        <div className="text-lg font-bold text-foreground">{formatUSD(costSummary.stageCosts.validation)}</div>
                        <div className="text-[10px] text-muted-foreground">Quality Checks</div>
                      </div>
                      <div className="bg-card border border-border rounded-lg p-3 shadow-sm">
                        <div className="text-xs text-muted-foreground font-medium mb-1 uppercase tracking-wider">Learning</div>
                        <div className="text-lg font-bold text-foreground">{formatUSD(costSummary.stageCosts.learning)}</div>
                        <div className="text-[10px] text-muted-foreground">Self-Improvement</div>
                      </div>
                      <div className="bg-card border border-border rounded-lg p-3 shadow-sm">
                        <div className="text-xs text-muted-foreground font-medium mb-1 uppercase tracking-wider">Other</div>
                        <div className="text-lg font-bold text-foreground">{formatUSD(costSummary.stageCosts.other)}</div>
                        <div className="text-[10px] text-muted-foreground">Orchestration/System</div>
                      </div>
                    </div>
                  )}

                  {/* Model Breakdown Table */}
                  {logs.token_usage.by_model && (
                    <div className="bg-card border border-border rounded-lg overflow-hidden shadow-sm">
                      <div className="px-4 py-3 bg-muted/30 border-b border-border flex items-center justify-between">
                        <h3 className="text-sm font-semibold flex items-center gap-2">
                          <DollarSign className="w-4 h-4 text-green-500" />
                          Cost Breakdown by Model
                        </h3>
                      </div>
                      <div className="p-4 overflow-x-auto">
                        <table className="w-full text-xs">
                          <thead>
                            <tr className="text-muted-foreground border-b border-border pb-2">
                              <th className="w-8"></th>
                              <th className="text-left font-medium pb-2">Model</th>
                              <th className="text-right font-medium pb-2">Calls</th>
                              <th className="text-right font-medium pb-2">Input</th>
                              <th className="text-right font-medium pb-2">Cached In</th>
                              <th className="text-right font-medium pb-2">Cache Write</th>
                              <th className="text-right font-medium pb-2">Reasoning</th>
                              <th className="text-right font-medium pb-2">Output</th>
                              <th className="text-right font-medium pb-2">Cost (USD)</th>
                            </tr>
                          </thead>
                          <tbody className="divide-y divide-border">
                            {Object.entries(logs.token_usage.by_model).map(([modelId, usage]) => {
                              const cacheRead = usage.cache_read_tokens || usage.cache_tokens || 0
                              const cacheWrite = usage.cache_write_tokens || 0
                              const reasoning = usage.reasoning_tokens || 0
                              const cachePercent = usage.input_tokens > 0 ? (cacheRead / usage.input_tokens) * 100 : 0
                              
                              // Calculate step-wise breakdown for this model
                              const modelSteps = logs.token_usage.by_step_and_model
                                ? Object.entries(logs.token_usage.by_step_and_model)
                                    .map(([stepKey, modelMap]) => {
                                      const stepUsage = modelMap[modelId]
                                      if (!stepUsage) return null

                                      const parts = stepKey.split(':')
                                      const phase = parts[0]
                                      const index = parseInt(parts[1] || '0', 10)
                                      const stepNum = index + 1

                                      // Look up step title from logs.steps
                                      const stepId = `step-${stepNum}`
                                      const stepData = logs.steps[stepId]
                                      const stepTitle = stepData?.title || ''

                                      let phaseLabel = ''
                                      let type = 'other'

                                      if (phase === 'execution_only') { phaseLabel = 'Execution'; type = 'execution' }
                                      else if (phase === 'validation') { phaseLabel = 'Validation'; type = 'validation' }
                                      else if (phase.includes('learning')) { phaseLabel = 'Learning'; type = 'learning' }
                                      else if (phase === 'workflow') { phaseLabel = 'Workflow'; type = 'workflow' }
                                      else { phaseLabel = phase }

                                      // Create label with step number, title, and phase
                                      const label = stepTitle
                                        ? `Step ${stepNum}: ${stepTitle} (${phaseLabel})`
                                        : `Step ${stepNum} (${phaseLabel})`

                                      return { key: stepKey, label, type, index, stepNum, stepTitle, usage: stepUsage }
                                    })
                                    .filter((s): s is NonNullable<typeof s> => s !== null)
                                    .sort((a, b) => {
                                       if (a.index !== b.index) return a.index - b.index
                                       // If same index, order by phase
                                       const order: Record<string, number> = { execution: 1, validation: 2, learning: 3, other: 4 }
                                       return (order[a.type] || 4) - (order[b.type] || 4)
                                    })
                                : []

                              return (
                                <React.Fragment key={modelId}>
                                  <tr className="hover:bg-accent/50 transition-colors cursor-pointer" onClick={() => toggleCostModel(modelId)}>
                                    <td className="py-2 pl-2">
                                      {expandedCostModels.has(modelId) ? (
                                        <ChevronDown className="w-3 h-3 text-muted-foreground" />
                                      ) : (
                                        <ChevronRight className="w-3 h-3 text-muted-foreground" />
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
                                    <td className="py-2 text-right text-muted-foreground">{cacheWrite > 0 ? cacheWrite.toLocaleString() : '-'}</td>
                                    <td className="py-2 text-right text-muted-foreground">{reasoning > 0 ? reasoning.toLocaleString() : '-'}</td>
                                    <td className="py-2 text-right text-muted-foreground">{usage.output_tokens.toLocaleString()}</td>
                                    <td className="py-2 text-right text-green-600 dark:text-green-400 font-semibold">{formatUSD(usage.total_cost_usd)}</td>
                                  </tr>
                                  {expandedCostModels.has(modelId) && (
                                    <tr className="bg-muted/20">
                                      <td colSpan={9} className="p-0">
                                        <div className="p-4 space-y-4 animate-in fade-in slide-in-from-top-1 duration-200">
                                          {/* Aggregate Cards */}
                                          <div className="grid grid-cols-1 md:grid-cols-3 gap-4 text-xs">
                                            {/* Token Breakdown */}
                                            <div className="space-y-2 border border-border bg-background/50 rounded-md p-3">
                                              <h4 className="font-semibold text-muted-foreground uppercase tracking-wider text-[10px] flex items-center gap-1.5">
                                                <Cpu className="w-3 h-3" /> Token Breakdown
                                              </h4>
                                              <div className="grid grid-cols-2 gap-x-2 gap-y-1.5">
                                                <span className="text-muted-foreground">Input:</span>
                                                <span className="text-foreground text-right">{usage.input_tokens.toLocaleString()} <span className="text-muted-foreground opacity-70">({formatUSD(usage.input_cost_usd)})</span></span>
                                                
                                                <span className="text-muted-foreground">Output:</span>
                                                <span className="text-foreground text-right">{usage.output_tokens.toLocaleString()} <span className="text-muted-foreground opacity-70">({formatUSD(usage.output_cost_usd)})</span></span>
                                                
                                                {usage.reasoning_tokens > 0 && (
                                                  <>
                                                    <span className="text-muted-foreground">Reasoning:</span>
                                                    <span className="text-foreground text-right">{usage.reasoning_tokens.toLocaleString()} <span className="text-muted-foreground opacity-70">({formatUSD(usage.reasoning_cost_usd)})</span></span>
                                                  </>
                                                )}
                                              </div>
                                            </div>

                                            {/* Cache Details */}
                                            <div className="space-y-2 border border-border bg-background/50 rounded-md p-3">
                                              <h4 className="font-semibold text-muted-foreground uppercase tracking-wider text-[10px] flex items-center gap-1.5">
                                                <History className="w-3 h-3" /> Cache Performance
                                              </h4>
                                              <div className="grid grid-cols-2 gap-x-2 gap-y-1.5">
                                                <span className="text-muted-foreground">Total Cache:</span>
                                                <span className="text-foreground text-right">{usage.cache_tokens.toLocaleString()}</span>
                                                
                                                <span className="text-muted-foreground">Read (Hit):</span>
                                                <span className="text-foreground text-right">{usage.cache_read_tokens?.toLocaleString() || '0'} <span className="text-green-600/70 dark:text-green-400/70">({formatUSD(usage.cache_read_cost_usd)})</span></span>
                                                
                                                <span className="text-muted-foreground">Write (Miss):</span>
                                                <span className="text-foreground text-right">{usage.cache_write_tokens?.toLocaleString() || '0'} <span className="text-yellow-600/70 dark:text-yellow-400/70">({formatUSD(usage.cache_write_cost_usd)})</span></span>
                                              </div>
                                            </div>

                                            {/* Context Window */}
                                            <div className="space-y-2 border border-border bg-background/50 rounded-md p-3">
                                              <h4 className="font-semibold text-muted-foreground uppercase tracking-wider text-[10px] flex items-center gap-1.5">
                                                <FileText className="w-3 h-3" /> Context Window
                                              </h4>
                                              <div className="grid grid-cols-2 gap-x-2 gap-y-1.5">
                                                <span className="text-muted-foreground">Peak Usage:</span>
                                                <span className="text-foreground text-right">{usage.context_window_usage?.toLocaleString() || '-'}</span>
                                                
                                                <span className="text-muted-foreground">Limit:</span>
                                                <span className="text-foreground text-right">{usage.model_context_window?.toLocaleString() || '-'}</span>
                                                
                                                <span className="text-muted-foreground">Utilization:</span>
                                                <span className="text-foreground text-right font-medium">
                                                  {usage.context_usage_percent ? (
                                                    <span className={usage.context_usage_percent > 80 ? 'text-red-500' : usage.context_usage_percent > 50 ? 'text-yellow-500' : 'text-green-500'}>
                                                      {usage.context_usage_percent.toFixed(1)}%
                                                    </span>
                                                  ) : '-'}
                                                </span>
                                              </div>
                                            </div>
                                          </div>
                                          
                                          {/* Usage by Step Table */}
                                          {modelSteps.length > 0 && (
                                            <div className="border border-border rounded-md overflow-hidden bg-background">
                                              <div className="bg-muted/50 px-4 py-2 border-b border-border flex justify-between items-center">
                                                <h4 className="font-semibold text-xs text-foreground flex items-center gap-2">
                                                  <List className="w-3.5 h-3.5" /> Usage by Step
                                                </h4>
                                                <span className="text-[10px] text-muted-foreground bg-background px-1.5 py-0.5 rounded border border-border">
                                                  {modelSteps.length} steps
                                                </span>
                                              </div>
                                              <div className="overflow-x-auto">
                                                <table className="w-full text-xs">
                                                  <thead>
                                                    <tr className="text-muted-foreground border-b border-border bg-muted/30">
                                                      <th className="px-4 py-2 text-left font-medium">Step</th>
                                                      <th className="px-4 py-2 text-right font-medium">Input</th>
                                                      <th className="px-4 py-2 text-right font-medium">Cached In</th>
                                                      <th className="px-4 py-2 text-right font-medium">Reasoning</th>
                                                      <th className="px-4 py-2 text-right font-medium">Output</th>
                                                      <th className="px-4 py-2 text-right font-medium">Cost</th>
                                                    </tr>
                                                  </thead>
                                                  <tbody className="divide-y divide-border">
                                                    {modelSteps.map((step) => (
                                                      <tr key={step.key} className="hover:bg-muted/30 transition-colors">
                                                        <td className="px-4 py-2">
                                                          <div className="flex items-center gap-2">
                                                            {step.type === 'execution' && <Terminal className="w-3.5 h-3.5 text-blue-500" />}
                                                            {step.type === 'validation' && <CheckCircle className="w-3.5 h-3.5 text-purple-500" />}
                                                            {step.type === 'learning' && <BookOpen className="w-3.5 h-3.5 text-amber-500" />}
                                                            {step.type === 'workflow' && <Network className="w-3.5 h-3.5 text-foreground" />}
                                                            <span className="font-medium text-foreground">{step.label}</span>
                                                          </div>
                                                        </td>
                                                        <td className="px-4 py-2 text-right text-muted-foreground">{step.usage.input_tokens.toLocaleString()}</td>
                                                        <td className="px-4 py-2 text-right text-muted-foreground">
                                                          {(step.usage.cache_read_tokens || step.usage.cache_tokens || 0).toLocaleString()}
                                                        </td>
                                                        <td className="px-4 py-2 text-right text-muted-foreground">
                                                          {(step.usage.reasoning_tokens || 0).toLocaleString()}
                                                        </td>
                                                        <td className="px-4 py-2 text-right text-muted-foreground">{step.usage.output_tokens.toLocaleString()}</td>
                                                        <td className="px-4 py-2 text-right text-green-600 dark:text-green-400 font-medium">{formatUSD(step.usage.total_cost_usd)}</td>
                                                      </tr>
                                                    ))}
                                                  </tbody>
                                                </table>
                                              </div>
                                            </div>
                                          )}
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
                              <td className="py-3 text-foreground">Total Summary</td>
                              <td className="py-3 text-right text-foreground">{costSummary?.totalLLMCalls}</td>
                              <td className="py-3 text-right text-muted-foreground">{costSummary?.totalInputTokens.toLocaleString()}</td>
                              <td className="py-3 text-right text-muted-foreground">
                                {costSummary?.totalCacheReadTokens.toLocaleString()}
                                {costSummary?.totalInputTokens > 0 && (
                                  <span className="text-[10px] text-muted-foreground ml-1">
                                    ({((costSummary.totalCacheReadTokens / costSummary.totalInputTokens) * 100).toFixed(0)}%)
                                  </span>
                                )}
                              </td>
                              <td className="py-3 text-right text-muted-foreground">{costSummary?.totalCacheWriteTokens.toLocaleString()}</td>
                              <td className="py-3 text-right text-muted-foreground">{costSummary?.totalReasoningTokens.toLocaleString()}</td>
                              <td className="py-3 text-right text-muted-foreground">{costSummary?.totalOutputTokens.toLocaleString()}</td>
                              <td className="py-3 text-right text-green-600 dark:text-green-400">{formatUSD(costSummary?.totalCost)}</td>
                            </tr>
                          </tfoot>
                        </table>
                      </div>
                    </div>
                  )}
                </div>
              )}

              {Object.entries(logs.steps)
                .sort((a, b) => sortStepIds(a[0], b[0]))
                .map(([stepId, stepLogs]) => {
                  const isExpanded = expandedSteps.has(stepId)
                  // Determine overall status based on latest validation or other activity
                  const validations = stepLogs.validations || []
                  let status = 'PENDING'
                  
                  if (validations.length > 0) {
                    status = validations[validations.length - 1].content?.execution_status || 'UNKNOWN'
                  } else if (stepLogs.conditionals && stepLogs.conditionals.length > 0) {
                    status = 'COMPLETED'
                  } else if (stepLogs.decisions && stepLogs.decisions.length > 0) {
                    status = 'COMPLETED'
                  } else if (stepLogs.executions && stepLogs.executions.length > 0) {
                    status = 'IN_PROGRESS'
                  } else if (stepLogs.orchestration && stepLogs.orchestration.length > 0) {
                    status = 'IN_PROGRESS'
                  }

                  const title = stepLogs.title || stepId
                  const description = stepLogs.description || ''
                  const nestingLevel = getStepNestingLevel(stepId)
                  const indentStyle = getStepIndentStyle(nestingLevel)
                  const nestingClass = getStepNestingClass(stepId)

                  return (
                    <div key={stepId} className={`border border-border rounded-lg overflow-hidden bg-card ${nestingClass}`} style={indentStyle}>
                      <button
                        onClick={() => toggleStep(stepId)}
                        className={`
                          w-full flex items-center justify-between px-4 py-3 text-left transition-colors
                          ${isExpanded ? 'bg-accent/50' : 'hover:bg-accent/50'}
                        `}
                      >
                        <div className="flex items-center gap-3 overflow-hidden">
                          {isExpanded ? <ChevronDown className="w-4 h-4 text-muted-foreground flex-shrink-0" /> : <ChevronRight className="w-4 h-4 text-muted-foreground flex-shrink-0" />}
                          
                          <div className="flex flex-col items-start text-left min-w-0">
                            <div className="flex items-center gap-2">
                                {getStepIcon(stepLogs.type)}
                                <span className="font-mono text-xs opacity-50">{stepLogs.original_id || stepId}</span>
                                <span className="text-sm font-medium text-foreground truncate">{title}</span>
                            </div>
                            {description && (
                                <span className="text-xs text-muted-foreground line-clamp-1 truncate w-full">{description}</span>
                            )}
                          </div>
                        </div>
                        
                        <div className="flex items-center gap-3 flex-shrink-0 ml-4">
                            {/* Status Badge */}
                            {status === 'COMPLETED' ? (
                                <span className="flex items-center gap-1 text-xs font-medium px-2 py-0.5 rounded-full bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300">
                                <CheckCircle className="w-3 h-3" />
                                Completed
                                </span>
                            ) : status === 'FAILED' ? (
                                <span className="flex items-center gap-1 text-xs font-medium px-2 py-0.5 rounded-full bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300">
                                <XCircle className="w-3 h-3" />
                                Failed
                                </span>
                            ) : (
                                <span className="flex items-center gap-1 text-xs font-medium px-2 py-0.5 rounded-full bg-secondary text-secondary-foreground">
                                <Clock className="w-3 h-3" />
                                {status || 'Pending'}
                                </span>
                            )}
                            <div className="text-xs text-muted-foreground hidden sm:block">
                                {stepLogs.executions.length} exec • {stepLogs.validations.length} val
                            </div>
                        </div>
                      </button>

                      {isExpanded && (
                        <div className="border-t border-border divide-y divide-border">
                          
                          {/* Executions Section */}
                          {stepLogs.executions.length > 0 && (
                            <div className="p-4 bg-background">
                              <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-3">Execution Logs</h4>
                              <div className="space-y-3">
                                {stepLogs.executions.map((exec, idx) => {
                                  const execId = `${stepId}-exec-${exec.attempt}-${exec.iteration}`
                                  const isExecExpanded = expandedExecutions.has(execId)
                                  const result = exec.content?.execution_result
                                  
                                  return (
                                    <div key={idx} className="bg-background rounded border border-border overflow-hidden">
                                      <button
                                        onClick={() => toggleExecution(execId)}
                                        className="w-full flex items-start gap-3 p-3 text-left hover:bg-accent/50 transition-colors"
                                      >
                                        <Terminal className="w-4 h-4 mt-0.5 text-muted-foreground flex-shrink-0" />
                                        <div className="flex-1 min-w-0">
                                          <div className="flex items-center justify-between mb-1">
                                            <span className="text-sm font-medium text-foreground">
                                              Attempt {exec.attempt} {exec.iteration > 0 && `(Iteration ${exec.iteration})`}
                                            </span>
                                            {isExecExpanded ? <ChevronDown className="w-3 h-3 text-muted-foreground" /> : <ChevronRight className="w-3 h-3 text-muted-foreground" />}
                                          </div>
                                          {result && (
                                            <p className="text-xs text-muted-foreground line-clamp-2">
                                              {result}
                                            </p>
                                          )}
                                        </div>
                                      </button>
                                      
                                      {isExecExpanded && exec.content && (
                                        <div className="p-3 border-t border-border bg-muted/30 text-xs font-mono">
                                          <div className="flex justify-end mb-2">
                                            <button
                                                onClick={(e) => {
                                                    e.stopPropagation()
                                                    handleViewConversation(exec.conversation_path, `Execution Conversation - Step ${stepId} (Attempt ${exec.attempt})`)
                                                }}
                                                disabled={loadingFiles.has(exec.conversation_path)}
                                                className="flex items-center gap-1.5 px-2 py-1 text-xs font-medium text-primary bg-primary/10 hover:bg-primary/20 rounded transition-colors"
                                            >
                                                {loadingFiles.has(exec.conversation_path) ? <Loader2 className="w-3 h-3 animate-spin" /> : <MessageSquare className="w-3 h-3" />}
                                                View Full Conversation
                                            </button>
                                          </div>
                                          <div className="font-semibold text-foreground mb-1">Execution Result:</div>
                                          <pre className="whitespace-pre-wrap overflow-x-auto text-muted-foreground max-h-60 overflow-y-auto mb-3">
                                            {result}
                                          </pre>
                                          <div className="font-semibold text-foreground mb-1">Full JSON:</div>
                                          <pre className="whitespace-pre-wrap overflow-x-auto text-muted-foreground max-h-40 overflow-y-auto">
                                            {JSON.stringify(exec.content, null, 2)}
                                          </pre>
                                        </div>
                                      )}
                                    </div>
                                  )
                                })}
                              </div>
                            </div>
                          )}

                          {/* Step Output Section */}
                          {stepLogs.output_content && (
                            <div className="p-4 bg-blue-50/50 dark:bg-blue-950/20">
                              <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-3 flex items-center gap-2">
                                <FileText className="w-3.5 h-3.5" />
                                Step Output
                                <span className="text-[10px] font-normal text-muted-foreground bg-background px-1.5 py-0.5 rounded border border-border">
                                  {stepLogs.context_output}
                                </span>
                              </h4>
                              <div className="bg-background rounded border border-border overflow-hidden">
                                <div className="p-3 max-h-64 overflow-auto">
                                  {stepLogs.output_content.is_json ? (
                                    <pre className="text-xs font-mono text-foreground whitespace-pre-wrap break-words">
                                      {JSON.stringify(stepLogs.output_content.content, null, 2)}
                                    </pre>
                                  ) : (
                                    <pre className="text-xs font-mono text-foreground whitespace-pre-wrap break-words">
                                      {String(stepLogs.output_content.content)}
                                    </pre>
                                  )}
                                </div>
                              </div>
                            </div>
                          )}

                          {/* Validations Section */}
                          {validations.length > 0 && (
                            <div className="p-4 bg-muted/30">
                              <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-3">Validations</h4>
                              <div className="space-y-3">
                                {validations.map((val, idx) => {
                                  const valId = `${stepId}-val-${val.attempt}`
                                  const isValExpanded = expandedValidations.has(valId)
                                  const valStatus = val.content?.execution_status
                                  const reasoning = val.content?.reasoning
                                  const feedback = val.content?.feedback || []
                                  
                                  return (
                                    <div key={idx} className="bg-background rounded border border-border overflow-hidden">
                                      <button
                                        onClick={() => toggleValidation(valId)}
                                        className="w-full flex items-start gap-3 p-3 text-left hover:bg-accent/50 transition-colors"
                                      >
                                        <div className={`mt-0.5 w-2 h-2 rounded-full flex-shrink-0 ${valStatus === 'COMPLETED' ? 'bg-green-500' : valStatus === 'FAILED' ? 'bg-red-500' : 'bg-gray-400'}`} />
                                        <div className="flex-1 min-w-0">
                                          <div className="flex items-center justify-between mb-1">
                                            <span className="text-sm font-medium text-foreground">
                                              Attempt {val.attempt}
                                            </span>
                                            {isValExpanded ? <ChevronDown className="w-3 h-3 text-muted-foreground" /> : <ChevronRight className="w-3 h-3 text-muted-foreground" />}
                                          </div>
                                          {reasoning && (
                                            <p className="text-xs text-muted-foreground line-clamp-2">
                                              {reasoning}
                                            </p>
                                          )}
                                        </div>
                                      </button>
                                      
                                      {isValExpanded && val.content && (
                                        <div className="p-3 border-t border-border bg-muted/30 text-xs font-mono">
                                          {feedback.length > 0 && (
                                            <div className="mb-3">
                                              <div className="font-semibold text-foreground mb-1">Feedback:</div>
                                              <ul className="list-disc pl-4 space-y-1 text-muted-foreground">
                                                {feedback.map((fb: any, i: number) => (
                                                  <li key={i}>
                                                    <span className={`font-semibold ${fb.severity === 'CRITICAL' || fb.severity === 'HIGH' ? 'text-destructive' : 'text-yellow-500'}`}>[{fb.severity}]</span> {fb.description}
                                                  </li>
                                                ))}
                                              </ul>
                                            </div>
                                          )}
                                          <div className="font-semibold text-foreground mb-1">Full Response:</div>
                                          <pre className="whitespace-pre-wrap overflow-x-auto text-muted-foreground max-h-60 overflow-y-auto">
                                            {JSON.stringify(val.content, null, 2)}
                                          </pre>
                                        </div>
                                      )}
                                    </div>
                                  )
                                })}
                              </div>
                            </div>
                          )}

                          {/* Learnings Section */}
                          {stepLogs.learnings && stepLogs.learnings.length > 0 && (
                            <div className="p-4 bg-background border-t border-border">
                              <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-3 flex items-center gap-2">
                                <BookOpen className="w-4 h-4" /> Learning Logs
                              </h4>
                              <div className="space-y-3">
                                {stepLogs.learnings.map((log, idx) => (
                                  <div key={idx} className="bg-background rounded border border-border p-3 text-sm">
                                    <div className="flex items-center gap-2 mb-2">
                                      <span className={`px-2 py-0.5 rounded text-xs uppercase font-medium ${
                                        log.type === 'learning_completed' ? 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300' :
                                        log.type === 'learning_failed' ? 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300' :
                                        'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300'
                                      }`}>
                                        {log.type.replace('learning_', '')}
                                      </span>
                                      <span className="text-xs text-muted-foreground ml-auto">{new Date(log.timestamp).toLocaleTimeString()}</span>
                                    </div>
                                    <div className="flex justify-between items-center text-xs text-muted-foreground mt-1">
                                        <span>Type: {log.learning_type}</span>
                                        {log.detail_level && <span>Level: {log.detail_level}</span>}
                                    </div>
                                    
                                    {log.result && (
                                        <div className="mt-2 text-xs">
                                            <div className="font-semibold text-foreground mb-1">Extracted Learning:</div>
                                            <pre className="p-2 bg-muted/50 rounded border border-border font-mono whitespace-pre-wrap text-muted-foreground max-h-40 overflow-y-auto">
                                                {log.result}
                                            </pre>
                                        </div>
                                    )}

                                    {log.conversation_path && (
                                        <div className="flex justify-end mt-2">
                                            <button
                                                onClick={(e) => {
                                                    e.stopPropagation()
                                                    handleViewConversation(log.conversation_path!, `Learning Conversation - Step ${stepId} (${log.learning_type})`)
                                                }}
                                                disabled={loadingFiles.has(log.conversation_path!)}
                                                className="flex items-center gap-1.5 px-2 py-1 text-xs font-medium text-primary bg-primary/10 hover:bg-primary/20 rounded transition-colors"
                                            >
                                                {loadingFiles.has(log.conversation_path!) ? <Loader2 className="w-3 h-3 animate-spin" /> : <MessageSquare className="w-3 h-3" />}
                                                View Full Conversation
                                            </button>
                                        </div>
                                    )}

                                    {log.error && (
                                        <div className="mt-2 text-xs text-destructive bg-destructive/10 p-2 rounded">
                                            Error: {log.error}
                                        </div>
                                    )}
                                  </div>
                                ))}
                              </div>
                            </div>
                          )}

                          {/* Orchestration Section */}
                          {stepLogs.orchestration && stepLogs.orchestration.length > 0 && (
                            <div className="p-4 bg-muted/30 border-t border-border">
                              <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-3 flex items-center gap-2">
                                <Network className="w-4 h-4" /> Orchestration Logs
                              </h4>
                              <div className="space-y-3">
                                {stepLogs.orchestration.map((log, idx) => (
                                  <div key={idx} className="bg-background rounded border border-border p-3 text-sm">
                                    <div className="flex items-center gap-2 mb-2">
                                      <span className="font-mono text-xs bg-primary/10 text-primary px-1.5 py-0.5 rounded uppercase">{log.type}</span>
                                      <span className="text-xs text-muted-foreground ml-auto">{new Date(log.timestamp).toLocaleTimeString()}</span>
                                    </div>
                                    {log.type === 'routing' && log.orchestration_response && (
                                      <div className="space-y-3 mt-2">
                                        <div className="flex justify-between items-center">
                                            <span className="font-medium text-foreground">Selected Route:</span>
                                            <span className="font-mono text-xs bg-accent px-2 py-0.5 rounded border border-border">{log.orchestration_response.selected_route_id}</span>
                                        </div>
                                        
                                        {/* Success Reasoning */}
                                        {log.orchestration_response.success_reasoning && (
                                            <div className="text-xs">
                                                <div className="font-semibold text-muted-foreground mb-1">Reasoning:</div>
                                                <p className="text-muted-foreground border-l-2 border-primary/20 pl-2 italic">
                                                    {log.orchestration_response.success_reasoning}
                                                </p>
                                            </div>
                                        )}

                                        {/* Instructions to Sub-Agent */}
                                        {log.orchestration_response.instructions_to_sub_agent && (
                                            <div className="text-xs">
                                                <div className="font-semibold text-foreground mb-1 flex items-center gap-1.5">
                                                    <Terminal className="w-3 h-3 text-primary" />
                                                    Instructions to Sub-Agent:
                                                </div>
                                                <div className="p-2 bg-muted/50 rounded border border-border font-mono whitespace-pre-wrap text-muted-foreground max-h-60 overflow-y-auto">
                                                    {log.orchestration_response.instructions_to_sub_agent}
                                                </div>
                                            </div>
                                        )}

                                        {/* Success Criteria for Sub-Agent */}
                                        {log.orchestration_response.success_criteria_for_sub_agent && (
                                            <div className="text-xs">
                                                <div className="font-semibold text-foreground mb-1 flex items-center gap-1.5">
                                                    <CheckCircle className="w-3 h-3 text-green-500" />
                                                    Sub-Agent Success Criteria:
                                                </div>
                                                <p className="text-muted-foreground bg-green-500/5 p-2 rounded border border-green-500/20">
                                                    {log.orchestration_response.success_criteria_for_sub_agent}
                                                </p>
                                            </div>
                                        )}
                                      </div>
                                    )}
                                    {log.type === 'evaluation' && (
                                      <div className="flex items-center gap-2">
                                        <span className={`px-2 py-0.5 rounded text-xs ${log.success_criteria_met ? 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300' : 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'}`}>
                                            Success: {log.success_criteria_met ? 'Yes' : 'No'}
                                        </span>
                                      </div>
                                    )}
                                    <details className="mt-2">
                                        <summary className="text-xs text-muted-foreground cursor-pointer hover:text-foreground">View Details</summary>
                                        <pre className="mt-2 text-xs font-mono whitespace-pre-wrap overflow-x-auto text-muted-foreground bg-muted p-2 rounded max-h-40 overflow-y-auto">
                                            {JSON.stringify(log, null, 2)}
                                        </pre>
                                    </details>
                                  </div>
                                ))}
                              </div>
                            </div>
                          )}

                          {/* Conditionals Section */}
                          {stepLogs.conditionals && stepLogs.conditionals.length > 0 && (
                            <div className="p-4 bg-muted/30 border-t border-border">
                              <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-3 flex items-center gap-2">
                                <GitBranch className="w-4 h-4" /> Conditional Logs
                              </h4>
                              <div className="space-y-3">
                                {stepLogs.conditionals.map((cond, idx) => (
                                  <div key={idx} className="bg-background rounded border border-border p-3 text-sm">
                                    <div className="flex items-center gap-2 mb-2">
                                      <span className={`px-2 py-0.5 rounded-full text-xs font-medium ${cond.condition_result ? 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300' : 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-300'}`}>
                                        Result: {cond.condition_result ? 'True' : 'False'}
                                      </span>
                                      <span className="text-xs text-muted-foreground ml-auto">{new Date(cond.timestamp).toLocaleTimeString()}</span>
                                    </div>
                                    <div className="font-medium text-foreground mb-1">{cond.condition_question}</div>
                                    <p className="text-muted-foreground text-xs italic">{cond.condition_reason}</p>
                                    <div className="mt-2 text-xs text-muted-foreground">
                                        <span className="font-semibold">Branch:</span> {cond.branch_executed}
                                    </div>
                                  </div>
                                ))}
                              </div>
                            </div>
                          )}

                          {/* Decisions Section */}
                          {stepLogs.decisions && stepLogs.decisions.length > 0 && (
                            <div className="p-4 bg-muted/30 border-t border-border">
                              <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-3 flex items-center gap-2">
                                <GitBranch className="w-4 h-4" /> Decision Logs
                              </h4>
                              <div className="space-y-3">
                                {stepLogs.decisions.map((dec, idx) => (
                                  <div key={idx} className="bg-background rounded border border-border p-3 text-sm">
                                    <div className="flex items-center gap-2 mb-2">
                                      <span className={`px-2 py-0.5 rounded-full text-xs font-medium ${dec.decision_result ? 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300' : 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-300'}`}>
                                        Result: {dec.decision_result ? 'True' : 'False'}
                                      </span>
                                      <span className="text-xs text-muted-foreground ml-auto">{new Date(dec.timestamp).toLocaleTimeString()}</span>
                                    </div>
                                    <p className="text-muted-foreground text-xs italic">{dec.decision_reasoning}</p>
                                  </div>
                                ))}
                              </div>
                            </div>
                          )}

                          {/* Archived Logs Section (Previous Runs) */}
                          {stepLogs.archived_logs && stepLogs.archived_logs.length > 0 && (
                            <div className="p-4 bg-amber-500/5 border-t border-amber-500/20">
                              <h4 className="text-xs font-semibold text-amber-600 dark:text-amber-400 uppercase tracking-wider mb-3 flex items-center gap-2">
                                <History className="w-4 h-4" /> Previous Runs ({stepLogs.archived_logs.length})
                              </h4>
                              <div className="space-y-3">
                                {stepLogs.archived_logs.map((archive, archiveIdx) => {
                                  const archiveId = `${stepId}-archive-${archiveIdx}`
                                  const isArchiveExpanded = expandedArchived.has(archiveId)
                                  const totalLogs = (archive.validations?.length || 0) + (archive.executions?.length || 0) +
                                                   (archive.learnings?.length || 0) + (archive.orchestration?.length || 0) +
                                                   (archive.conditionals?.length || 0) + (archive.decisions?.length || 0)

                                  // Format timestamp for display (20260106-115300 -> 2026-01-06 11:53:00)
                                  const formatArchiveTimestamp = (ts: string) => {
                                    if (ts.length === 15 && ts.includes('-')) {
                                      const date = ts.slice(0, 8)
                                      const time = ts.slice(9)
                                      return `${date.slice(0, 4)}-${date.slice(4, 6)}-${date.slice(6, 8)} ${time.slice(0, 2)}:${time.slice(2, 4)}:${time.slice(4, 6)}`
                                    }
                                    return ts
                                  }

                                  return (
                                    <div key={archiveIdx} className="bg-background rounded border border-amber-500/30 overflow-hidden">
                                      <button
                                        onClick={() => toggleArchived(archiveId)}
                                        className="w-full flex items-center gap-3 p-3 text-left hover:bg-amber-500/10 transition-colors"
                                      >
                                        {isArchiveExpanded ? <ChevronDown className="w-4 h-4 text-amber-500" /> : <ChevronRight className="w-4 h-4 text-amber-500" />}
                                        <div className="flex-1 min-w-0">
                                          <div className="flex items-center justify-between">
                                            <span className="text-sm font-medium text-foreground">
                                              Run from {formatArchiveTimestamp(archive.timestamp)}
                                            </span>
                                            <span className="text-xs text-muted-foreground">
                                              {totalLogs} log{totalLogs !== 1 ? 's' : ''}
                                            </span>
                                          </div>
                                        </div>
                                      </button>

                                      {isArchiveExpanded && (
                                        <div className="border-t border-amber-500/20 p-3 space-y-3 bg-muted/20">
                                          {/* Archived Executions */}
                                          {archive.executions && archive.executions.length > 0 && (
                                            <div>
                                              <div className="text-xs font-semibold text-muted-foreground mb-2 flex items-center gap-1">
                                                <Terminal className="w-3 h-3" /> Executions ({archive.executions.length})
                                              </div>
                                              {archive.executions.map((exec, idx) => (
                                                <div key={idx} className="text-xs bg-background border border-border rounded p-2 mb-1">
                                                  <div className="flex items-center justify-between mb-1">
                                                    <span className="font-medium">Attempt {exec.attempt}</span>
                                                    {exec.conversation_path && (
                                                      <button
                                                        onClick={() => handleViewConversation(exec.conversation_path, `Archived Execution - ${archive.timestamp}`)}
                                                        disabled={loadingFiles.has(exec.conversation_path)}
                                                        className="text-primary hover:underline text-xs"
                                                      >
                                                        {loadingFiles.has(exec.conversation_path) ? 'Loading...' : 'View'}
                                                      </button>
                                                    )}
                                                  </div>
                                                  {exec.content?.execution_result && (
                                                    <p className="text-muted-foreground line-clamp-2">{exec.content.execution_result}</p>
                                                  )}
                                                </div>
                                              ))}
                                            </div>
                                          )}

                                          {/* Archived Validations */}
                                          {archive.validations && archive.validations.length > 0 && (
                                            <div>
                                              <div className="text-xs font-semibold text-muted-foreground mb-2 flex items-center gap-1">
                                                <CheckCircle className="w-3 h-3" /> Validations ({archive.validations.length})
                                              </div>
                                              {archive.validations.map((val, idx) => {
                                                const valStatus = val.content?.execution_status
                                                return (
                                                  <div key={idx} className="text-xs bg-background border border-border rounded p-2 mb-1">
                                                    <div className="flex items-center gap-2">
                                                      <div className={`w-2 h-2 rounded-full ${valStatus === 'COMPLETED' ? 'bg-green-500' : valStatus === 'FAILED' ? 'bg-red-500' : 'bg-gray-400'}`} />
                                                      <span className="font-medium">Attempt {val.attempt}</span>
                                                      <span className={`ml-auto text-xs ${valStatus === 'COMPLETED' ? 'text-green-600' : valStatus === 'FAILED' ? 'text-red-600' : 'text-muted-foreground'}`}>
                                                        {valStatus || 'Unknown'}
                                                      </span>
                                                    </div>
                                                    {val.content?.reasoning && (
                                                      <p className="text-muted-foreground mt-1 line-clamp-2">{val.content.reasoning}</p>
                                                    )}
                                                  </div>
                                                )
                                              })}
                                            </div>
                                          )}

                                          {/* Archived Learnings */}
                                          {archive.learnings && archive.learnings.length > 0 && (
                                            <div>
                                              <div className="text-xs font-semibold text-muted-foreground mb-2 flex items-center gap-1">
                                                <BookOpen className="w-3 h-3" /> Learnings ({archive.learnings.length})
                                              </div>
                                              {archive.learnings.map((learning, idx) => (
                                                <div key={idx} className="text-xs bg-background border border-border rounded p-2 mb-1">
                                                  <div className="flex items-center justify-between">
                                                    <span className="font-medium">{learning.learning_type}</span>
                                                    {learning.conversation_path && (
                                                      <button
                                                        onClick={() => handleViewConversation(learning.conversation_path!, `Archived Learning - ${archive.timestamp}`)}
                                                        disabled={loadingFiles.has(learning.conversation_path!)}
                                                        className="text-primary hover:underline text-xs"
                                                      >
                                                        {loadingFiles.has(learning.conversation_path!) ? 'Loading...' : 'View'}
                                                      </button>
                                                    )}
                                                  </div>
                                                  {learning.result && (
                                                    <p className="text-muted-foreground mt-1 line-clamp-2">{learning.result}</p>
                                                  )}
                                                </div>
                                              ))}
                                            </div>
                                          )}

                                          {/* Archived Orchestration */}
                                          {archive.orchestration && archive.orchestration.length > 0 && (
                                            <div>
                                              <div className="text-xs font-semibold text-muted-foreground mb-2 flex items-center gap-1">
                                                <Network className="w-3 h-3" /> Orchestration ({archive.orchestration.length})
                                              </div>
                                              {archive.orchestration.map((orch, idx) => (
                                                <div key={idx} className="text-xs bg-background border border-border rounded p-2 mb-1">
                                                  <span className="font-medium">{orch.type}</span>
                                                  {orch.selected_route_id && (
                                                    <span className="ml-2 text-muted-foreground">Route: {orch.selected_route_id}</span>
                                                  )}
                                                </div>
                                              ))}
                                            </div>
                                          )}

                                          {/* Archived Conditionals */}
                                          {archive.conditionals && archive.conditionals.length > 0 && (
                                            <div>
                                              <div className="text-xs font-semibold text-muted-foreground mb-2 flex items-center gap-1">
                                                <GitBranch className="w-3 h-3" /> Conditionals ({archive.conditionals.length})
                                              </div>
                                              {archive.conditionals.map((cond, idx) => (
                                                <div key={idx} className="text-xs bg-background border border-border rounded p-2 mb-1">
                                                  <div className="flex items-center gap-2">
                                                    <span className={`px-1.5 py-0.5 rounded text-xs ${cond.condition_result ? 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300' : 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-300'}`}>
                                                      {cond.condition_result ? 'True' : 'False'}
                                                    </span>
                                                    <span className="text-muted-foreground truncate">{cond.condition_question}</span>
                                                  </div>
                                                </div>
                                              ))}
                                            </div>
                                          )}

                                          {/* Archived Decisions */}
                                          {archive.decisions && archive.decisions.length > 0 && (
                                            <div>
                                              <div className="text-xs font-semibold text-muted-foreground mb-2 flex items-center gap-1">
                                                <GitBranch className="w-3 h-3" /> Decisions ({archive.decisions.length})
                                              </div>
                                              {archive.decisions.map((dec, idx) => (
                                                <div key={idx} className="text-xs bg-background border border-border rounded p-2 mb-1">
                                                  <div className="flex items-center gap-2">
                                                    <span className={`px-1.5 py-0.5 rounded text-xs ${dec.decision_result ? 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300' : 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-300'}`}>
                                                      {dec.decision_result ? 'True' : 'False'}
                                                    </span>
                                                  </div>
                                                  {dec.decision_reasoning && (
                                                    <p className="text-muted-foreground mt-1 line-clamp-2">{dec.decision_reasoning}</p>
                                                  )}
                                                </div>
                                              ))}
                                            </div>
                                          )}
                                        </div>
                                      )}
                                    </div>
                                  )
                                })}
                              </div>
                            </div>
                          )}
                        </div>
                      )}
                    </div>
                  )
                })}
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

        {/* File Viewer Overlay */}
        {viewingFile && (
            <div className="absolute inset-0 z-50 bg-background flex flex-col rounded-lg">
                <div className="flex items-center justify-between px-6 py-4 border-b border-border bg-muted/30">
                    <div>
                        <h3 className="text-md font-semibold text-foreground flex items-center gap-2">
                            <MessageSquare className="w-4 h-4 text-primary" />
                            {viewingFile.title}
                        </h3>
                        <p className="text-xs text-muted-foreground mt-0.5 font-mono truncate max-w-xl" title={viewingFile.path}>
                            {viewingFile.path}
                        </p>
                    </div>
                    <button 
                        onClick={() => setViewingFile(null)}
                        className="px-3 py-1.5 bg-secondary text-secondary-foreground rounded text-xs font-medium hover:bg-secondary/80 transition-colors"
                    >
                        Back to Logs
                    </button>
                </div>
                <div className="flex-1 overflow-auto p-4 bg-background">
                    <pre className="text-xs font-mono whitespace-pre-wrap text-muted-foreground">
                        {viewingFile.content}
                    </pre>
                </div>
            </div>
        )}
      </div>
    </div>
  )
}

export default ExecutionLogsPopup