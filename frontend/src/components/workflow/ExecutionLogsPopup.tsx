import React, { useEffect, useMemo, useState } from 'react'
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
  Filter,
  RefreshCw,
  ListTodo,
  Archive,
  Search
} from 'lucide-react'
import { agentApi } from '../../services/api'
import type { ExecutionLogsResponse } from '../../services/api-types'
import { ConversationViewer } from './ConversationViewer'
import { MarkdownRenderer } from '../ui/MarkdownRenderer'

interface ValidationFeedback {
  severity: 'CRITICAL' | 'HIGH' | 'MEDIUM' | 'LOW' | string
  description: string
}

interface ExecutionLogsPopupProps {
  isOpen: boolean
  onClose: () => void
  workspacePath: string | null
  runFolder: string | null
  runFolders: string[] // Available run folders (iterations and groups)
}

const ITERATION_ZERO_DEFAULT_FOLDER = 'iteration-0/default'

const isIterationZeroRunFolder = (folder: string) => (
  folder === 'iteration-0' || folder.startsWith('iteration-0/')
)

const getDefaultRunFolder = (initialRunFolder: string | null | undefined, runFolders: string[]) => {
  if (initialRunFolder && initialRunFolder !== 'new') return initialRunFolder
  const iterationZeroFolder = runFolders.find(isIterationZeroRunFolder)
  if (iterationZeroFolder) return iterationZeroFolder
  return ITERATION_ZERO_DEFAULT_FOLDER
}

const StepMetadata = ({ description, successCriteria }: { description?: string, successCriteria?: string }) => {
  if (!description && !successCriteria) return null;
  
  return (
    <div className="p-4 bg-muted/20 border-b border-border space-y-3">
      {description && (
        <div>
          <div className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider mb-1 flex items-center gap-1.5">
            <FileText className="w-3 h-3" /> Description
          </div>
          <p className="text-xs text-foreground leading-relaxed">
            {description}
          </p>
        </div>
      )}
      {successCriteria && (
        <div>
          <div className="text-[10px] font-semibold text-green-600 dark:text-green-400 uppercase tracking-wider mb-1 flex items-center gap-1.5">
            <CheckCircle className="w-3 h-3" /> Success Criteria
          </div>
          <p className="text-xs text-foreground leading-relaxed bg-green-500/5 p-2 rounded border border-green-500/10">
            {successCriteria}
          </p>
        </div>
      )}
    </div>
  )
}

const getStepIcon = (type: string) => {
  switch (type) {
    case 'orchestration':
      return <Network className="w-4 h-4 text-purple-500" />
    case 'todo_task':
      return <ListTodo className="w-4 h-4 text-purple-500" />
    case 'conditional':
      return <GitBranch className="w-4 h-4 text-blue-500" />
    case 'human_input':
      return <User className="w-4 h-4 text-orange-500" />
    case 'sub-agent':
      return <Bot className="w-4 h-4 text-indigo-500" />
    case 'branch':
      return <Split className="w-4 h-4 text-indigo-500" />
    default:
      return <Terminal className="w-4 h-4 text-muted-foreground" />
  }
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

  // Match: number, or 'true', or 'false', or 'sub-agent', or 'sub', or 'generic'
  const pattern = /(\d+|true|false|sub-agent|sub|generic)/g
  let match
  while ((match = pattern.exec(withoutPrefix)) !== null) {
    const val = match[1]
    if (val === 'true' || val === 'false' || val === 'sub-agent' || val === 'sub' || val === 'generic') {
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
    if (seg === 'true' || seg === 'false' || seg === 'sub-agent' || seg === 'sub' || seg === 'generic') {
      level++
    }
  }

  return level
}

// Determine the nesting context (what type of parent this is nested under)
const getStepNestingContext = (stepId: string): 'none' | 'branch' | 'sub-agent' => {
  // Check the last nesting indicator in the ID
  const lastBranchIndex = Math.max(stepId.lastIndexOf('-true-'), stepId.lastIndexOf('-false-'), stepId.lastIndexOf('-decision-'))
  const lastSubIndex = Math.max(stepId.lastIndexOf('-sub-'), stepId.lastIndexOf('-generic-'))
  const lastSubAgentIndex = Math.max(stepId.lastIndexOf('-sub-agent-'), lastSubIndex)

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
  return { marginLeft: `${level * 32}px` }
}

// Get additional CSS class for nested steps (colored left border)
const getStepNestingClass = (stepId: string): string => {
  const context = getStepNestingContext(stepId)

  switch (context) {
    case 'sub-agent':
      return 'border-l-4 border-l-purple-500/50'
    case 'branch':
      return 'border-l-4 border-l-indigo-500/50'
    default:
      return ''
  }
}

type LogRecord = Record<string, unknown>

type StepMetrics = {
  inputTokens: number
  outputTokens: number
  totalTokens: number
  cacheTokens: number
  reasoningTokens: number
  durationMs: number
  llmCalls: number
}

const asRecord = (value: unknown): LogRecord | null => (
  value && typeof value === 'object' && !Array.isArray(value) ? value as LogRecord : null
)

const asNumber = (value: unknown): number => {
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string' && value.trim() !== '') {
    const parsed = Number(value)
    return Number.isFinite(parsed) ? parsed : 0
  }
  return 0
}

const durationFromTimestamps = (start: unknown, end: unknown): number => {
  if (typeof start !== 'string' || typeof end !== 'string') return 0
  const startMs = Date.parse(start)
  const endMs = Date.parse(end)
  if (!Number.isFinite(startMs) || !Number.isFinite(endMs) || endMs < startMs) return 0
  return endMs - startMs
}

const formatTokenCount = (tokens: number): string => {
  if (!tokens) return '0'
  if (tokens >= 1_000_000) return `${(tokens / 1_000_000).toFixed(tokens >= 10_000_000 ? 1 : 2)}M`
  if (tokens >= 1_000) return `${(tokens / 1_000).toFixed(tokens >= 100_000 ? 0 : 1)}k`
  return `${tokens}`
}

const formatDuration = (durationMs: number): string => {
  if (!durationMs) return '0s'
  const totalSeconds = Math.max(1, Math.round(durationMs / 1000))
  const hours = Math.floor(totalSeconds / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60

  if (hours > 0) return `${hours}h ${minutes}m`
  if (minutes > 0) return `${minutes}m ${seconds}s`
  return `${seconds}s`
}

const addCallTokens = (metrics: StepMetrics, call: LogRecord) => {
  metrics.inputTokens += asNumber(call.prompt_tokens)
  metrics.outputTokens += asNumber(call.completion_tokens)
  metrics.cacheTokens += asNumber(call.cache_tokens)
  metrics.reasoningTokens += asNumber(call.reasoning_tokens)

  const totalTokens = asNumber(call.total_tokens)
  if (totalTokens > 0) {
    metrics.totalTokens += totalTokens
  } else {
    metrics.totalTokens += asNumber(call.prompt_tokens) + asNumber(call.completion_tokens) + asNumber(call.reasoning_tokens)
  }
}

const getExecutionMetrics = (exec: unknown): StepMetrics => {
  const metrics: StepMetrics = {
    inputTokens: 0,
    outputTokens: 0,
    totalTokens: 0,
    cacheTokens: 0,
    reasoningTokens: 0,
    durationMs: 0,
    llmCalls: 0,
  }

  const execRecord = asRecord(exec)
  const content = asRecord(execRecord?.content)
  const timing = asRecord(execRecord?.timing) || asRecord(content?.timing) || content
  const agent = asRecord(timing?.agent) || content
  const llm = asRecord(timing?.llm)

  metrics.durationMs = asNumber(agent?.duration_ms) ||
    durationFromTimestamps(agent?.started_at, agent?.completed_at) ||
    asNumber(content?.duration_ms)
  metrics.llmCalls = asNumber(agent?.llm_call_count) || asNumber(llm?.count)

  const calls = Array.isArray(llm?.calls) ? llm.calls : []
  calls.forEach(call => {
    const callRecord = asRecord(call)
    if (callRecord) addCallTokens(metrics, callRecord)
  })

  if (calls.length === 0 && content) {
    addCallTokens(metrics, content)
  }

  return metrics
}

const getStepMetrics = (executions: unknown[]): StepMetrics => executions.reduce<StepMetrics>((acc, exec) => {
  const metrics = getExecutionMetrics(exec)
  acc.inputTokens += metrics.inputTokens
  acc.outputTokens += metrics.outputTokens
  acc.totalTokens += metrics.totalTokens
  acc.cacheTokens += metrics.cacheTokens
  acc.reasoningTokens += metrics.reasoningTokens
  acc.durationMs += metrics.durationMs
  acc.llmCalls += metrics.llmCalls
  return acc
}, {
  inputTokens: 0,
  outputTokens: 0,
  totalTokens: 0,
  cacheTokens: 0,
  reasoningTokens: 0,
  durationMs: 0,
  llmCalls: 0,
})

const hasStepMetrics = (metrics: StepMetrics) => (
  metrics.durationMs > 0 || metrics.totalTokens > 0 || metrics.inputTokens > 0 || metrics.outputTokens > 0 || metrics.llmCalls > 0
)

const StepMetricChip = ({ title, children }: { title: string; children: React.ReactNode }) => (
  <span
    title={title}
    className="inline-flex items-center gap-1 rounded-md border border-border bg-muted/50 px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground"
  >
    {children}
  </span>
)

const ExecutionLogsPopup: React.FC<ExecutionLogsPopupProps> = ({
  isOpen,
  onClose,
  workspacePath,
  runFolder: initialRunFolder,
  runFolders
}) => {
  const runFolderOptions = useMemo(() => {
    const defaultRunFolder = getDefaultRunFolder(initialRunFolder, runFolders)
    if (!defaultRunFolder || runFolders.includes(defaultRunFolder)) return runFolders
    return [defaultRunFolder, ...runFolders]
  }, [initialRunFolder, runFolders])

  const [loading, setLoading] = useState(false)
  const [logs, setLogs] = useState<ExecutionLogsResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [expandedSteps, setExpandedSteps] = useState<Set<string>>(new Set())
  const [expandedValidations, setExpandedValidations] = useState<Set<string>>(new Set())
  const [expandedExecutions, setExpandedExecutions] = useState<Set<string>>(new Set())
  const [expandedArchived, setExpandedArchived] = useState<Set<string>>(new Set())
  const [selectedRunFolder, setSelectedRunFolder] = useState<string>(() => getDefaultRunFolder(initialRunFolder, runFolders))
  const [stepSearchQueries, setStepSearchQueries] = useState<Record<string, string>>({})
  
  // State for inline file viewing
  const [expandedFiles, setExpandedFiles] = useState<Set<string>>(new Set())
  const [fileContents, setFileContents] = useState<Record<string, string>>({})
  const [loadingFiles, setLoadingFiles] = useState<Set<string>>(new Set())

  // Update selected run folder when prop changes
  useEffect(() => {
    setSelectedRunFolder(getDefaultRunFolder(initialRunFolder, runFolders))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initialRunFolder, runFolders])

  useEffect(() => {
    if (isOpen && workspacePath && selectedRunFolder) {
      loadLogs()
    } else {
      setLogs(null)
      setError(null)
      setExpandedFiles(new Set())
      setFileContents({})
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isOpen, workspacePath, selectedRunFolder])

  useEffect(() => {
    if (!isOpen || !workspacePath || !selectedRunFolder) return

    const intervalId = window.setInterval(() => {
      loadLogs({ silent: true })
    }, 2500)

    return () => window.clearInterval(intervalId)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isOpen, workspacePath, selectedRunFolder])

  const loadLogs = async (options?: { silent?: boolean }) => {
    if (!workspacePath || !selectedRunFolder) return
    
    if (!options?.silent) setLoading(true)
    setError(null)
    try {
      // Use selected run folder
      const data = await agentApi.getExecutionLogs(workspacePath, selectedRunFolder)
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
      if (!options?.silent) setLoading(false)
    }
  }

  const toggleStep = (stepId: string) => {
    setExpandedSteps(prev => {
      const next = new Set(prev)
      if (next.has(stepId)) {
        next.delete(stepId)
      } else {
        next.add(stepId)
        
        // Auto-expand latest execution attempt
        const stepLogs = logs?.steps[stepId]
        if (stepLogs && stepLogs.executions && stepLogs.executions.length > 0) {
          const latest = stepLogs.executions[stepLogs.executions.length - 1]
          const execId = `${stepId}-exec-${latest.attempt}-${latest.iteration}`
          setExpandedExecutions(prevExec => new Set(prevExec).add(execId))
        }
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

  const toggleExecution = (id: string) => {
    setExpandedExecutions(prev => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      return next
    })
  }

  const toggleArchived = (id: string) => {
    setExpandedArchived(prev => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      return next
    })
  }

  const toggleFileExpansion = async (path: string) => {
    if (expandedFiles.has(path)) {
      setExpandedFiles(prev => {
        const next = new Set(prev)
        next.delete(path)
        return next
      })
      return
    }

    setExpandedFiles(prev => new Set(prev).add(path))
    
    if (!fileContents[path]) {
      setLoadingFiles(prev => new Set(prev).add(path))
      try {
        const content = await agentApi.getLogFile(path)
        const contentStr = typeof content === 'string' ? content : JSON.stringify(content, null, 2)
        setFileContents(prev => ({ ...prev, [path]: contentStr }))
      } catch (e) {
        console.error(e)
        setFileContents(prev => ({ ...prev, [path]: 'Error: Failed to load content' }))
      } finally {
        setLoadingFiles(prev => {
          const next = new Set(prev)
          next.delete(path)
          return next
        })
      }
    }
  }

  // Recursive render function for step content
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const renderStepContent = (stepId: string, stepLogs: any) => {
      const validations = stepLogs.validations || []
      const searchQuery = stepSearchQueries[stepId] || ''
      
      const matchesSearch = (item: unknown) => {
        if (!searchQuery) return true
        return JSON.stringify(item).toLowerCase().includes(searchQuery.toLowerCase())
      }
      
      return (
        <div className="border-t border-border divide-y divide-border">
          {/* Local Search Input */}
          <div className="px-4 py-2 bg-muted/10 border-b border-border flex items-center gap-2 sticky top-0 z-10 backdrop-blur-sm">
            <Search className="w-3.5 h-3.5 text-muted-foreground" />
            <input
              type="text"
              placeholder="Search logs in this step..."
              value={searchQuery}
              onChange={(e) => setStepSearchQueries(prev => ({ ...prev, [stepId]: e.target.value }))}
              className="text-xs bg-transparent border-none focus:outline-none focus:ring-0 w-full placeholder:text-muted-foreground/70 text-foreground"
              autoFocus
            />
            {searchQuery && (
                <button onClick={() => setStepSearchQueries(prev => { const n = {...prev}; delete n[stepId]; return n })} className="text-muted-foreground hover:text-foreground p-1">
                    <X className="w-3 h-3" />
                </button>
            )}
          </div>

          {/* Step Metadata (Description & Success Criteria) */}
          <StepMetadata 
            description={stepLogs.description} 
            successCriteria={stepLogs.success_criteria}
          />
          {/* Executions Section */}
          {stepLogs.executions.filter(matchesSearch).length > 0 && (
            <div className="p-4 bg-background">
              <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-3">Execution Logs</h4>
              <div className="space-y-3">
                {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
                {stepLogs.executions.filter(matchesSearch).map((exec: any, idx: number) => {
                  const execId = `${stepId}-exec-${exec.attempt}-${exec.iteration}`
                  const isExecExpanded = expandedExecutions.has(execId)
                  const isFastPath = exec.fast_path === true
                  const execMetrics = getExecutionMetrics(exec)
                  // Fast-path entries carry LearnCodeFastPathLog shape: success/exit_code/output/error.
                  // LLM-attempt entries carry ExecutionResult shape with execution_result/model.
                  const result = isFastPath
                    ? (exec.content?.success ? (exec.content?.output || '') : (exec.content?.error || exec.content?.output || ''))
                    : exec.content?.execution_result
                  const model = isFastPath ? null : exec.content?.model
                  const fpSuccess = isFastPath ? exec.content?.success === true : null
                  const fpExit = isFastPath ? exec.content?.exit_code : null

                  return (
                    <div key={idx} className={`bg-background rounded border overflow-hidden ${isFastPath ? 'border-indigo-200 dark:border-indigo-800' : 'border-border'}`}>
                      <button
                        onClick={() => toggleExecution(execId)}
                        className="w-full flex items-start gap-3 p-3 text-left hover:bg-accent/50 transition-colors"
                      >
                        <Terminal className={`w-4 h-4 mt-0.5 flex-shrink-0 ${isFastPath ? 'text-indigo-600 dark:text-indigo-400' : 'text-muted-foreground'}`} />
                        <div className="flex-1 min-w-0">
                          <div className="flex items-center justify-between mb-1">
                            <div className="flex items-center gap-2 flex-wrap">
                              <span className="text-sm font-medium text-foreground">
                                {isFastPath
                                  ? 'Saved main.py (fast path)'
                                  : <>Attempt {exec.attempt} {exec.iteration > 0 && `(Iteration ${exec.iteration})`}</>}
                              </span>
                              {isFastPath && (
                                <span className={`text-[10px] font-medium px-1.5 py-0.5 rounded border ${
                                  fpSuccess
                                    ? 'bg-green-50 text-green-700 border-green-200 dark:bg-green-950/30 dark:text-green-300 dark:border-green-800'
                                    : 'bg-red-50 text-red-700 border-red-200 dark:bg-red-950/30 dark:text-red-300 dark:border-red-800'
                                }`}>
                                  {fpSuccess ? 'ok' : 'fail'}{fpExit !== undefined ? ` · exit=${fpExit}` : ''}
                                </span>
                              )}
                              {model && (
                                <span className="text-[10px] font-mono bg-muted px-1.5 py-0.5 rounded text-muted-foreground border border-border">
                                  {model}
                                </span>
                              )}
                              {execMetrics.totalTokens > 0 && (
                                <span className="text-[10px] font-medium bg-muted px-1.5 py-0.5 rounded text-muted-foreground border border-border">
                                  {formatTokenCount(execMetrics.totalTokens)} tok
                                </span>
                              )}
                              {execMetrics.durationMs > 0 && (
                                <span className="text-[10px] font-medium bg-muted px-1.5 py-0.5 rounded text-muted-foreground border border-border">
                                  {formatDuration(execMetrics.durationMs)}
                                </span>
                              )}
                            </div>
                            {isExecExpanded ? <ChevronDown className="w-3 h-3 text-muted-foreground" /> : <ChevronRight className="w-3 h-3 text-muted-foreground" />}
                          </div>
                          {result && (
                            <p className="text-xs text-muted-foreground line-clamp-2 whitespace-pre-wrap">
                              {result}
                            </p>
                          )}
                        </div>
                      </button>
                      
                      {isExecExpanded && exec.content && (
                        <div className="p-3 border-t border-border bg-muted/30 text-xs font-mono">
                          {isFastPath ? (
                            // Fast-path: no LLM conversation, just main.py stdout/error.
                            // Render a compact script header + output block.
                            <div>
                              {exec.content.script_path && (
                                <div className="mb-2 text-[10px]">
                                  <span className="text-muted-foreground">Script: </span>
                                  <span className="text-foreground font-semibold">{exec.content.script_path}</span>
                                </div>
                              )}
                              {exec.content.timestamp && (
                                <div className="mb-2 text-[10px] text-muted-foreground">
                                  Ran at {exec.content.timestamp}
                                </div>
                              )}
                              {exec.content.output && (
                                <>
                                  <div className="font-semibold text-foreground mb-1">stdout:</div>
                                  <pre className="whitespace-pre-wrap overflow-x-auto text-muted-foreground max-h-[40vh] overflow-y-auto bg-background border border-border rounded p-2 mb-3">
                                    {exec.content.output}
                                  </pre>
                                </>
                              )}
                              {exec.content.error && exec.content.error !== exec.content.output && (
                                <>
                                  <div className="font-semibold text-red-600 dark:text-red-400 mb-1">error:</div>
                                  <pre className="whitespace-pre-wrap overflow-x-auto text-red-700 dark:text-red-300 max-h-[40vh] overflow-y-auto bg-red-50 dark:bg-red-950/30 border border-red-200 dark:border-red-800 rounded p-2 mb-3">
                                    {exec.content.error}
                                  </pre>
                                </>
                              )}
                              <div className="font-semibold text-foreground mb-1">Full JSON:</div>
                              <pre className="whitespace-pre-wrap overflow-x-auto text-muted-foreground max-h-[40vh] overflow-y-auto">
                                {JSON.stringify(exec.content, null, 2)}
                              </pre>
                            </div>
                          ) : (
                            // LLM attempt: conversation viewer + execution_result + full JSON.
                            <>
                              <div className="flex justify-end mb-2">
                                <button
                                  onClick={(e) => {
                                    e.stopPropagation()
                                    toggleFileExpansion(exec.conversation_path)
                                  }}
                                  disabled={loadingFiles.has(exec.conversation_path)}
                                  className="flex items-center gap-1.5 px-2 py-1 text-xs font-medium text-primary bg-primary/10 hover:bg-primary/20 rounded transition-colors"
                                >
                                  {loadingFiles.has(exec.conversation_path) ? <Loader2 className="w-3 h-3 animate-spin" /> : <MessageSquare className="w-3 h-3" />}
                                  {expandedFiles.has(exec.conversation_path) ? 'Hide Conversation' : 'View Full Conversation'}
                                </button>
                              </div>

                              {expandedFiles.has(exec.conversation_path) && (
                                <div className="mb-4 bg-background rounded border border-border p-3">
                                  <div className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider mb-2 border-b border-border pb-1">
                                    Conversation History
                                  </div>
                                  {fileContents[exec.conversation_path] ? (
                                    <ConversationViewer content={fileContents[exec.conversation_path]} searchQuery={searchQuery} />
                                  ) : (
                                    <div className="flex items-center gap-2 py-4 justify-center text-muted-foreground">
                                      <Loader2 className="w-4 h-4 animate-spin" />
                                      Loading conversation...
                                    </div>
                                  )}
                                </div>
                              )}

                              <div className="font-semibold text-foreground mb-1">Execution Result:</div>
                              <div className="max-h-[60vh] overflow-y-auto mb-3">
                                <MarkdownRenderer content={result || ''} className="!text-[11px] [&_p]:!text-[11px] [&_li]:!text-[11px] [&_h1]:!text-base [&_h2]:!text-sm [&_h3]:!text-xs [&_code]:!text-[10px]" />
                              </div>
                              <div className="font-semibold text-foreground mb-1">Full JSON:</div>
                              <pre className="whitespace-pre-wrap overflow-x-auto text-muted-foreground max-h-[60vh] overflow-y-auto">
                                {JSON.stringify(exec.content, null, 2)}
                              </pre>
                            </>
                          )}
                        </div>
                      )}
                    </div>
                  )
                })}
              </div>
            </div>
          )}

          {/* Step Output Section */}
          {(stepLogs.output_content || stepLogs.context_output) && (!searchQuery || matchesSearch(stepLogs.output_content)) && (
            <div className="p-4 bg-muted/30">
              <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-3 flex items-center gap-2">
                <FileText className="w-3.5 h-3.5" />
                Step Output
                <span className="text-[10px] font-normal text-muted-foreground bg-background border border-border px-1.5 py-0.5 rounded font-mono">
                  {stepLogs.context_output || 'output'}
                </span>
              </h4>
              {stepLogs.output_content ? (
                <div className="bg-background rounded border border-border overflow-hidden">
                  <div className="p-3 max-h-[60vh] overflow-auto">
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
              ) : (
                <div className="p-3 bg-background/50 rounded border border-border border-dashed text-xs text-muted-foreground italic flex items-center gap-2">
                  <Clock className="w-3 h-3" />
                  Expected output file not yet produced or found.
                </div>
              )}
            </div>
          )}

          {/* Artifacts Section */}
          {stepLogs.artifacts && stepLogs.artifacts.filter(matchesSearch).length > 0 && (
            <div className="p-4 bg-gray-50 dark:bg-gray-900/30">
              <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-3 flex items-center gap-2">
                <FileText className="w-3.5 h-3.5" />
                Artifacts & Files
              </h4>
              <div className="space-y-2">
                {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
                {stepLogs.artifacts.filter(matchesSearch).map((artifact: any, idx: number) => {
                  const isFileExpanded = expandedFiles.has(artifact.file_path)
                  return (
                    <div key={idx} className="bg-background rounded border border-border overflow-hidden">
                      <button
                        onClick={() => toggleFileExpansion(artifact.file_path)}
                        className="w-full flex items-center justify-between p-2 text-left hover:bg-accent/50 transition-colors"
                      >
                        <div className="flex items-center gap-2 truncate">
                          <FileText className="w-3.5 h-3.5 text-muted-foreground flex-shrink-0" />
                          <span className="font-mono text-xs text-foreground truncate">{artifact.file_name}</span>
                        </div>
                        <div className="flex items-center gap-2">
                          {loadingFiles.has(artifact.file_path) && <Loader2 className="w-3 h-3 animate-spin text-muted-foreground" />}
                          {isFileExpanded ? <ChevronDown className="w-3 h-3 text-muted-foreground" /> : <ChevronRight className="w-3 h-3 text-muted-foreground" />}
                        </div>
                      </button>
                      {isFileExpanded && (
                        <div className="p-3 border-t border-border bg-muted/20">
                          {fileContents[artifact.file_path] ? (
                            <pre className="text-[10px] font-mono whitespace-pre-wrap text-muted-foreground max-h-[60vh] overflow-auto">
                              {fileContents[artifact.file_path]}
                            </pre>
                          ) : !loadingFiles.has(artifact.file_path) && (
                            <div className="text-xs text-muted-foreground italic flex items-center gap-2">
                              <AlertCircle className="w-3 h-3" />
                              Failed to load content.
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

          {/* Validations Section */}
          {validations.filter(matchesSearch).length > 0 && (
            <div className="p-4 bg-muted/30">
              <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-3">Validations</h4>
              <div className="space-y-3">
                {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
                {validations.filter(matchesSearch).map((val: any, idx: number) => {
                  const valId = `${stepId}-val-${val.attempt}`
                  const isValExpanded = expandedValidations.has(valId)
                  const valStatus = val.content?.execution_status
                  const reasoning = val.content?.reasoning
                  const feedback = (val.content?.feedback || []) as ValidationFeedback[]
                  
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
                                {feedback.map((fb, i: number) => (
                                  <li key={i}>
                                    <span className={`font-semibold ${fb.severity === 'CRITICAL' || fb.severity === 'HIGH' ? 'text-destructive' : 'text-yellow-500'}`}>[{fb.severity}]</span> {fb.description}
                                  </li>
                                ))}
                              </ul>
                            </div>
                          )}
                          <div className="font-semibold text-foreground mb-1">Full Response:</div>
                          <pre className="whitespace-pre-wrap overflow-x-auto text-muted-foreground max-h-[60vh] overflow-y-auto">
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
          {stepLogs.learnings && stepLogs.learnings.filter(matchesSearch).length > 0 && (
            <div className="p-4 bg-background border-t border-border">
              <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-3 flex items-center gap-2">
                <BookOpen className="w-4 h-4" /> Learning Logs
              </h4>
              <div className="space-y-3">
                {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
                {stepLogs.learnings.filter(matchesSearch).map((log: any, idx: number) => (
                  <div key={idx} className="bg-background rounded border border-border p-3 text-sm">
                    <div className="flex items-center gap-2 mb-2">
                      <span className={`px-2 py-0.5 rounded text-xs uppercase font-medium ${
                        log.type === 'learning_completed' ? 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300' :
                        log.type === 'learning_failed' ? 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300' :
                        log.type === 'learning_skipped' ? 'bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300' :
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

                    {/* Trigger Reason (Why learning started) */}
                    {log.trigger_reason && (
                      <div className="mt-2 text-xs bg-blue-50 dark:bg-blue-900/10 p-2 rounded border border-blue-100 dark:border-blue-900/30">
                        <div className="font-semibold text-blue-700 dark:text-blue-300 mb-1 flex items-center gap-1.5">
                          <span className="text-sm">💡</span> Trigger Reason
                        </div>
                        <p className="text-muted-foreground">{log.trigger_reason}</p>
                      </div>
                    )}

                    {/* Skip Reason (Why learning was skipped) */}
                    {log.skip_reason && (
                      <div className="mt-2 text-xs bg-gray-50 dark:bg-gray-800/30 p-2 rounded border border-gray-100 dark:border-gray-800/50">
                        <div className="font-semibold text-muted-foreground mb-1 flex items-center gap-1.5">
                          <span className="text-sm">⏭️</span> Skip Reason
                        </div>
                        <p className="text-muted-foreground">{log.skip_reason}</p>
                      </div>
                    )}
                    
                    {log.result && (
                        <div className="mt-2 text-xs">
                            <div className="font-semibold text-foreground mb-1">Extracted Learning:</div>
                            <pre className="p-2 bg-muted/50 rounded border border-border font-mono whitespace-pre-wrap text-muted-foreground max-h-[40vh] overflow-y-auto">
                                {log.result}
                            </pre>
                        </div>
                    )}

                    {log.conversation_path && (
                        <div className="mt-3">
                            <div className="flex justify-end">
                                <button
                                    onClick={(e) => {
                                        e.stopPropagation()
                                        toggleFileExpansion(log.conversation_path!)
                                    }}
                                    disabled={loadingFiles.has(log.conversation_path!)}
                                    className="flex items-center gap-1.5 px-2 py-1 text-xs font-medium text-primary bg-primary/10 hover:bg-primary/20 rounded transition-colors"
                                >
                                    {loadingFiles.has(log.conversation_path!) ? <Loader2 className="w-3 h-3 animate-spin" /> : <MessageSquare className="w-3 h-3" />}
                                    {expandedFiles.has(log.conversation_path!) ? 'Hide Conversation' : 'View Full Conversation'}
                                </button>
                            </div>
                            
                            {expandedFiles.has(log.conversation_path!) && (
                                <div className="mt-2 bg-background rounded border border-border p-3">
                                  <div className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider mb-2 border-b border-border pb-1">
                                    Learning Conversation History
                                  </div>
                                  {fileContents[log.conversation_path!] ? (
                                    <ConversationViewer content={fileContents[log.conversation_path!]} searchQuery={searchQuery} />
                                  ) : (
                                    <div className="flex items-center gap-2 py-4 justify-center text-muted-foreground">
                                      <Loader2 className="w-4 h-4 animate-spin" />
                                      Loading conversation...
                                    </div>
                                  )}
                                </div>
                            )}
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
          {stepLogs.orchestration && stepLogs.orchestration.filter(matchesSearch).length > 0 && (
            <div className="p-4 bg-muted/30 border-t border-border">
              <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-3 flex items-center gap-2">
                <Network className="w-4 h-4" /> Orchestration Logs
              </h4>
              <div className="space-y-6">
                {Object.entries(
                  stepLogs.orchestration.filter(matchesSearch).reduce((acc: Record<number, any[]>, log: any) => { // eslint-disable-line @typescript-eslint/no-explicit-any
                    const iter = log.iteration || 1
                    if (!acc[iter]) acc[iter] = []
                    // Skip main_step as it's redundant with routing
                    if (log.type !== 'main_step') {
                      acc[iter].push(log)
                    }
                    return acc
                  }, {})
                ).sort(([a], [b]) => Number(a) - Number(b)).map(([iteration, iterLogs]) => (
                  <div key={iteration} className="relative">
                    <div className="flex items-center gap-2 mb-3">
                      <span className="flex items-center justify-center w-5 h-5 rounded-full bg-primary/10 text-primary text-[10px] font-bold ring-4 ring-muted/30">
                        {iteration}
                      </span>
                      <span className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                        Iteration {iteration}
                      </span>
                      <div className="h-px bg-border flex-1 ml-2" />
                    </div>
                    
                    <div className="space-y-3 pl-2.5 border-l-2 border-border/50 ml-2.5 pb-2">
                      {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
                      {(iterLogs as any[]).map((log, idx) => (
                        <div key={idx} className="pl-4 relative">
                          {/* Timeline dot */}
                          <div className={`absolute -left-[5px] top-3 w-2.5 h-2.5 rounded-full border-2 border-background ${
                            log.type === 'routing' ? 'bg-blue-500' :
                            log.type === 'evaluation' ? (log.success_criteria_met ? 'bg-green-500' : 'bg-red-500') :
                            'bg-gray-400'
                          }`} />

                          <div className="bg-background rounded border border-border p-3 text-sm shadow-sm">
                            <div className="flex items-center gap-2 mb-2">
                              <span className={`font-mono text-[10px] px-1.5 py-0.5 rounded uppercase font-bold tracking-wide ${
                                log.type === 'routing' ? 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300' :
                                log.type === 'evaluation' ? 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-300' :
                                'bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300'
                              }`}>
                                {log.type}
                              </span>
                              <span className="text-[10px] text-muted-foreground ml-auto font-mono">
                                {new Date(log.timestamp).toLocaleTimeString()}
                              </span>
                            </div>

                            {log.type === 'routing' && log.orchestration_response && (
                              <div className="space-y-3 mt-3">
                                <div className="flex flex-col gap-2 p-3 bg-primary/5 rounded border border-primary/20">
                                    <div className="flex justify-between items-start">
                                        <span className="font-medium text-foreground text-xs flex items-center gap-1.5 mt-0.5">
                                          <Split className="w-3.5 h-3.5 text-primary" />
                                          Selected Sub-Agent
                                        </span>
                                        {log.orchestration_response.selected_route_id && 
                                         log.orchestration_response.selected_route_id !== (log.orchestration_response.selected_sub_agent_title || log.orchestration_response.selected_route_name) && (
                                          <span className="font-mono text-[10px] text-muted-foreground bg-background px-1.5 py-0.5 rounded border border-border">
                                            ID: {log.orchestration_response.selected_route_id}
                                          </span>
                                        )}
                                    </div>
                                    <div className="text-sm font-semibold text-primary pl-5">
                                        {log.orchestration_response.selected_sub_agent_title || log.orchestration_response.selected_route_name || log.orchestration_response.selected_route_id}
                                    </div>
                                    
                                    {log.orchestration_response.selected_sub_agent_path && (
                                        <div className="flex justify-end mt-1">
                                            {/* View Execution button removed in favor of inline expansion */}
                                        </div>
                                    )}

                                    {/* Inline Sub-Agent Logs */}
                                    {log.orchestration_response.selected_sub_agent_path && logs?.steps?.[log.orchestration_response.selected_sub_agent_path] && (
                                        <div className="mt-3 border-t border-border pt-3">
                                            <details className="group">
                                                <summary className="text-xs font-semibold text-primary cursor-pointer hover:text-primary/80 flex items-center gap-2 select-none">
                                                    <ChevronRight className="w-4 h-4 transition-transform group-open:rotate-90" />
                                                    View Sub-Agent Execution ({logs!.steps[log.orchestration_response.selected_sub_agent_path].title})
                                                </summary>
                                                <div className="mt-3 pl-2 border-l-2 border-primary/20">
                                                    {renderStepContent(log.orchestration_response.selected_sub_agent_path, logs!.steps[log.orchestration_response.selected_sub_agent_path])}
                                                </div>
                                            </details>
                                        </div>
                                    )}
                                </div>
                                
                                {/* Success Reasoning / Decision Logic */}
                                {log.orchestration_response.success_reasoning && (
                                    <div className="text-xs">
                                        <div className="font-semibold text-foreground mb-1.5 flex items-center gap-1.5 text-amber-600 dark:text-amber-400">
                                          <span className="text-sm">💡</span> Why this agent was selected?
                                        </div>
                                        <div className="bg-amber-500/10 p-3 rounded-md border border-amber-500/20 text-foreground leading-relaxed shadow-sm">
                                            "{log.orchestration_response.success_reasoning}"
                                        </div>
                                    </div>
                                )}

                                {/* Instructions to Sub-Agent */}
                                {log.orchestration_response.instructions_to_sub_agent && (
                                    <div className="text-xs mt-2">
                                        <div className="font-semibold text-foreground mb-1.5 flex items-center gap-1.5">
                                            <Terminal className="w-3 h-3 text-primary" />
                                            Instructions to Sub-Agent
                                        </div>
                                        <div className="p-3 bg-muted/30 rounded border border-border font-mono whitespace-pre-wrap text-muted-foreground max-h-[60vh] overflow-y-auto text-[11px] leading-relaxed">
                                            {log.orchestration_response.instructions_to_sub_agent}
                                        </div>
                                    </div>
                                )}

                                {/* Success Criteria for Sub-Agent */}
                                {log.orchestration_response.success_criteria_for_sub_agent && (
                                    <div className="text-xs">
                                        <div className="font-semibold text-foreground mb-1.5 flex items-center gap-1.5">
                                            <CheckCircle className="w-3 h-3 text-green-600 dark:text-green-400" />
                                            Sub-Agent Success Criteria
                                        </div>
                                        <p className="text-green-700 dark:text-green-300 bg-green-500/10 p-2.5 rounded border border-green-500/20 italic">
                                            {log.orchestration_response.success_criteria_for_sub_agent}
                                        </p>
                                    </div>
                                )}
                              </div>
                            )}

                            {log.type === 'evaluation' && (
                              <div className="mt-2">
                                <div className={`flex items-center gap-2 p-2 rounded border ${
                                  log.success_criteria_met 
                                    ? 'bg-green-50 border-green-200 text-green-800 dark:bg-green-900/10 dark:border-green-900/30 dark:text-green-300' 
                                    : 'bg-red-50 border-red-200 text-red-800 dark:bg-red-900/10 dark:border-red-900/30 dark:text-red-300'
                                }`}>
                                    {log.success_criteria_met ? <CheckCircle className="w-4 h-4" /> : <XCircle className="w-4 h-4" />}
                                    <span className="font-semibold text-xs">
                                      Success Criteria Met: {log.success_criteria_met ? 'Yes' : 'No'}
                                    </span>
                                </div>
                              </div>
                            )}

                            <details className="mt-3 group">
                                <summary className="text-[10px] text-muted-foreground cursor-pointer hover:text-foreground flex items-center gap-1 select-none w-fit">
                                  <ChevronRight className="w-3 h-3 transition-transform group-open:rotate-90" />
                                  View Raw JSON
                                </summary>
                                <pre className="mt-2 text-[10px] font-mono whitespace-pre-wrap overflow-x-auto text-muted-foreground bg-muted/50 p-2 rounded max-h-[40vh] overflow-y-auto border border-border">
                                    {JSON.stringify(log, null, 2)}
                                </pre>
                            </details>
                          </div>
                        </div>
                      ))}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}
          {/* Todo Task Section */}
          {stepLogs.todo_task && stepLogs.todo_task.filter(matchesSearch).length > 0 && (
            <div className="p-4 bg-muted/30 border-t border-border">
              <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-3 flex items-center gap-2">
                <ListTodo className="w-4 h-4" /> Todo Task Logs
              </h4>
              <div className="space-y-6">
                {Object.entries(
                  // eslint-disable-next-line @typescript-eslint/no-explicit-any
                  stepLogs.todo_task.filter(matchesSearch).reduce((acc: Record<number, any[]>, log: any) => {
                    const iter = log.iteration || 1
                    if (!acc[iter]) acc[iter] = []
                    acc[iter].push(log)
                    return acc
                  }, {})
                ).sort(([a], [b]) => Number(a) - Number(b)).map(([iteration, iterLogs]) => {
                  // Extract sub-agent info from logs in this iteration
                  // eslint-disable-next-line @typescript-eslint/no-explicit-any
                  const routingLog = (iterLogs as any[]).find((l: any) => l.type === 'routing' && l.todo_task_response)
                  const subAgentName = routingLog?.todo_task_response?.selected_route_name ||
                                      (routingLog?.todo_task_response?.use_generic_agent ? 'Generic Agent' : null) ||
                                      routingLog?.todo_task_response?.selected_route_id
                  const todoTitle = routingLog?.todo_task_response?.todo_title || routingLog?.todo_task_response?.todo_id_to_execute

                  return (
                  <div key={iteration} className="relative">
                    <div className="flex items-center gap-2 mb-3">
                      <span className="flex items-center justify-center w-5 h-5 rounded-full bg-purple-500/10 text-purple-600 text-[10px] font-bold ring-4 ring-muted/30">
                        {iteration}
                      </span>
                      <span className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                        Iteration {iteration}
                      </span>
                      {subAgentName && (
                        <span className="text-xs font-medium px-2 py-0.5 rounded bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-300">
                          → {subAgentName}
                        </span>
                      )}
                      {todoTitle && (
                        <span className="text-xs text-muted-foreground truncate max-w-[200px]" title={todoTitle}>
                          ({todoTitle})
                        </span>
                      )}
                      <div className="h-px bg-border flex-1 ml-2" />
                    </div>

                    <div className="space-y-3 pl-2.5 border-l-2 border-purple-500/30 ml-2.5 pb-2">
                      {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
                      {(iterLogs as any[]).map((log, idx) => (
                        <div key={idx} className="pl-4 relative">
                          {/* Timeline dot */}
                          <div className={`absolute -left-[5px] top-3 w-2.5 h-2.5 rounded-full border-2 border-background ${
                            log.type === 'routing' ? 'bg-purple-500' :
                            log.type === 'evaluation' ? (log.all_tasks_complete ? 'bg-green-500' : 'bg-amber-500') :
                            'bg-gray-400'
                          }`} />

                          <div className="bg-background rounded border border-border p-3 text-sm shadow-sm">
                            <div className="flex items-center gap-2 mb-2">
                              <span className={`font-mono text-[10px] px-1.5 py-0.5 rounded uppercase font-bold tracking-wide ${
                                log.type === 'routing' ? 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-300' :
                                log.type === 'evaluation' ? 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300' :
                                'bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300'
                              }`}>
                                {log.type}
                              </span>
                              {log.model && (
                                <span className="text-[10px] font-mono bg-muted px-1.5 py-0.5 rounded text-muted-foreground border border-border">
                                  {log.model}
                                </span>
                              )}
                              <span className="text-[10px] text-muted-foreground ml-auto font-mono">
                                {log.timestamp ? new Date(log.timestamp).toLocaleTimeString() : ''}
                              </span>
                            </div>

                            {log.type === 'routing' && log.todo_task_response && (
                              <div className="space-y-3 mt-3">
                                {/* Next Action */}
                                <div className="flex items-center gap-2">
                                  <span className={`px-2 py-1 rounded text-xs font-medium ${
                                    log.todo_task_response.next_action === 'complete'
                                      ? 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300'
                                      : log.todo_task_response.next_action === 'delegate'
                                      ? 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300'
                                      : 'bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300'
                                  }`}>
                                    Action: {log.todo_task_response.next_action}
                                  </span>
                                  {log.todo_task_response.all_tasks_complete && (
                                    <span className="flex items-center gap-1 text-xs text-green-600 dark:text-green-400">
                                      <CheckCircle className="w-3.5 h-3.5" /> All tasks complete
                                    </span>
                                  )}
                                </div>

                                {/* Selected Agent */}
                                {(log.todo_task_response.selected_route_id || log.todo_task_response.use_generic_agent) && (
                                  <div className="flex flex-col gap-2 p-3 bg-purple-500/5 rounded border border-purple-500/20">
                                    <div className="flex justify-between items-start">
                                      <span className="font-medium text-foreground text-xs flex items-center gap-1.5 mt-0.5">
                                        {log.todo_task_response.use_generic_agent ? (
                                          <>
                                            <Bot className="w-3.5 h-3.5 text-purple-500" />
                                            Generic Agent
                                          </>
                                        ) : (
                                          <>
                                            <Split className="w-3.5 h-3.5 text-purple-500" />
                                            Predefined Sub-Agent
                                          </>
                                        )}
                                      </span>
                                      {log.todo_task_response.selected_route_id && (
                                        <span className="font-mono text-[10px] text-muted-foreground bg-background px-1.5 py-0.5 rounded border border-border">
                                          ID: {log.todo_task_response.selected_route_id}
                                        </span>
                                      )}
                                    </div>
                                    {log.todo_task_response.selected_route_name && (
                                      <div className="text-sm font-semibold text-purple-600 dark:text-purple-400 pl-5">
                                        {log.todo_task_response.selected_route_name}
                                      </div>
                                    )}
                                  </div>
                                )}

                                {/* Todo Item Being Executed */}
                                {log.todo_task_response.todo_id_to_execute && (
                                  <div className="text-xs">
                                    <div className="font-semibold text-foreground mb-1.5 flex items-center gap-1.5">
                                      <ListTodo className="w-3 h-3 text-purple-500" />
                                      Todo Item
                                    </div>
                                    <div className="p-2 bg-muted/30 rounded border border-border">
                                      <span className="font-mono text-[10px] text-muted-foreground">ID: {log.todo_task_response.todo_id_to_execute}</span>
                                      {log.todo_task_response.todo_title && (
                                        <div className="font-medium text-foreground mt-1">{log.todo_task_response.todo_title}</div>
                                      )}
                                    </div>
                                  </div>
                                )}

                                {/* Selection Reasoning */}
                                {log.todo_task_response.selection_reasoning && (
                                  <div className="text-xs">
                                    <div className="font-semibold text-foreground mb-1.5 flex items-center gap-1.5 text-amber-600 dark:text-amber-400">
                                      <span className="text-sm">💡</span> Why this agent was selected?
                                    </div>
                                    <div className="bg-amber-500/10 p-3 rounded-md border border-amber-500/20 text-foreground leading-relaxed shadow-sm">
                                      "{log.todo_task_response.selection_reasoning}"
                                    </div>
                                  </div>
                                )}

                                {/* Instructions to Sub-Agent */}
                                {log.todo_task_response.instructions_to_sub_agent && (
                                  <div className="text-xs mt-2">
                                    <div className="font-semibold text-foreground mb-1.5 flex items-center gap-1.5">
                                      <Terminal className="w-3 h-3 text-purple-500" />
                                      Instructions to Sub-Agent
                                    </div>
                                    <div className="p-3 bg-muted/30 rounded border border-border font-mono whitespace-pre-wrap text-muted-foreground max-h-[60vh] overflow-y-auto text-[11px] leading-relaxed">
                                      {log.todo_task_response.instructions_to_sub_agent}
                                    </div>
                                  </div>
                                )}

                                {/* Success Criteria for Sub-Agent */}
                                {log.todo_task_response.success_criteria_for_sub_agent && (
                                  <div className="text-xs">
                                    <div className="font-semibold text-foreground mb-1.5 flex items-center gap-1.5">
                                      <CheckCircle className="w-3 h-3 text-green-600 dark:text-green-400" />
                                      Sub-Agent Success Criteria
                                    </div>
                                    <p className="text-green-700 dark:text-green-300 bg-green-500/10 p-2.5 rounded border border-green-500/20 italic">
                                      {log.todo_task_response.success_criteria_for_sub_agent}
                                    </p>
                                  </div>
                                )}

                                {/* Progress Summary */}
                                {log.todo_task_response.progress_summary && (
                                  <div className="text-xs text-muted-foreground bg-muted/50 p-2 rounded border border-border flex items-center gap-2">
                                    <Clock className="w-3 h-3" />
                                    Progress: {log.todo_task_response.progress_summary}
                                  </div>
                                )}

                                {/* Inline Sub-Agent Logs */}
                                {log.todo_task_response.selected_sub_agent_path && logs?.steps?.[log.todo_task_response.selected_sub_agent_path] && (
                                  <details className="mt-2 group/sub">
                                    <summary className="text-xs font-semibold text-purple-600 dark:text-purple-400 cursor-pointer hover:underline flex items-center gap-1.5 select-none list-none">
                                      <ChevronRight className="w-3.5 h-3.5 transition-transform group-open/sub:rotate-90" />
                                      View Sub-Agent Execution ({logs!.steps[log.todo_task_response.selected_sub_agent_path].title})
                                    </summary>
                                    <div className="mt-3 ml-2 pl-3 border-l-2 border-purple-200 dark:border-purple-900/50">
                                      {renderStepContent(log.todo_task_response.selected_sub_agent_path, logs!.steps[log.todo_task_response.selected_sub_agent_path])}
                                    </div>
                                  </details>
                                )}
                              </div>
                            )}

                            {log.type === 'evaluation' && (
                              <div className="mt-2">
                                <div className={`flex items-center gap-2 p-2 rounded border ${
                                  log.all_tasks_complete
                                    ? 'bg-green-50 border-green-200 text-green-800 dark:bg-green-900/10 dark:border-green-900/30 dark:text-green-300'
                                    : 'bg-amber-50 border-amber-200 text-amber-800 dark:bg-amber-900/10 dark:border-amber-900/30 dark:text-amber-300'
                                }`}>
                                  {log.all_tasks_complete ? <CheckCircle className="w-4 h-4" /> : <Clock className="w-4 h-4" />}
                                  <span className="font-semibold text-xs">
                                    All Tasks Complete: {log.all_tasks_complete ? 'Yes' : 'No'}
                                  </span>
                                </div>
                              </div>
                            )}

                            <details className="mt-3 group">
                              <summary className="text-[10px] text-muted-foreground cursor-pointer hover:text-foreground flex items-center gap-1 select-none w-fit">
                                <ChevronRight className="w-3 h-3 transition-transform group-open:rotate-90" />
                                View Raw JSON
                              </summary>
                              <pre className="mt-2 text-[10px] font-mono whitespace-pre-wrap overflow-x-auto text-muted-foreground bg-muted/50 p-2 rounded max-h-[40vh] overflow-y-auto border border-border">
                                {JSON.stringify(log, null, 2)}
                              </pre>
                            </details>
                          </div>
                        </div>
                      ))}
                    </div>
                  </div>
                  )
                })}
              </div>
            </div>
          )}
          {/* Conditionals Section */}
          {stepLogs.conditionals && stepLogs.conditionals.filter(matchesSearch).length > 0 && (
            <div className="p-4 bg-muted/30 border-t border-border">
              <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-3 flex items-center gap-2">
                <GitBranch className="w-4 h-4" /> Conditional Logs
              </h4>
              <div className="space-y-3">
                {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
                {stepLogs.conditionals.filter(matchesSearch).map((cond: any, idx: number) => (
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

          {/* Archived Logs Section (Previous Runs) */}
          {stepLogs.archived_logs && stepLogs.archived_logs.filter(matchesSearch).length > 0 && (
            <div className="p-4 bg-amber-500/5 border-t border-amber-500/20">
              <h4 className="text-xs font-semibold text-amber-600 dark:text-amber-400 uppercase tracking-wider mb-3 flex items-center gap-2">
                <History className="w-4 h-4" /> Previous Runs ({stepLogs.archived_logs.filter(matchesSearch).length})
              </h4>
              <div className="space-y-3">
                {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
                {stepLogs.archived_logs.filter(matchesSearch).map((archive: any, archiveIdx: number) => {
                  const archiveId = `${stepId}-archive-${archiveIdx}`
                  const isArchiveExpanded = expandedArchived.has(archiveId)
                  const totalLogs = (archive.validations?.length || 0) + (archive.executions?.length || 0) +
                                   (archive.learnings?.length || 0) + (archive.orchestration?.length || 0) +
                                   (archive.conditionals?.length || 0)

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
                                                                        {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
                                                                        {archive.executions.map((exec: any, idx: number) => (
                                                                          <div key={idx} className="text-xs bg-background border border-border rounded p-2 mb-2">
                                                                            <div className="flex items-center justify-between mb-1">
                                                                              <div className="flex items-center gap-2">
                                                                                <span className="font-medium">Attempt {exec.attempt}</span>
                                                                                {exec.content?.model && (
                                                                                  <span className="text-[10px] font-mono bg-muted px-1.5 py-0.5 rounded text-muted-foreground border border-border">
                                                                                    {exec.content.model}
                                                                                  </span>
                                                                                )}
                                                                              </div>
                                                                              {exec.conversation_path && (
                                                                                <button
                                                                                  onClick={() => toggleFileExpansion(exec.conversation_path)}
                                                                                  disabled={loadingFiles.has(exec.conversation_path)}
                                                                                  className="text-primary hover:underline text-[10px] font-medium"
                                                                                >
                                                                                  {loadingFiles.has(exec.conversation_path) ? 'Loading...' : expandedFiles.has(exec.conversation_path) ? 'Hide' : 'View'}
                                                                                </button>
                                                                              )}
                                                                            </div>
                                                                            {exec.content?.execution_result && (
                                                                              <p className="text-muted-foreground line-clamp-2">{exec.content.execution_result}</p>
                                                                            )}
                                                                            {expandedFiles.has(exec.conversation_path) && (
                                                                              <div className="mt-2 pt-2 border-t border-border">
                                                                                {fileContents[exec.conversation_path] ? (
                                                                                  <pre className="whitespace-pre-wrap overflow-x-auto text-muted-foreground max-h-[60vh] overflow-y-auto font-mono text-[10px]">
                                                                                    {fileContents[exec.conversation_path]}
                                                                                  </pre>
                                                                                ) : (
                                                                                  <div className="flex items-center gap-2 py-2 text-muted-foreground">
                                                                                    <Loader2 className="w-3 h-3 animate-spin" />
                                                                                    Loading...
                                                                                  </div>
                                                                                )}
                                                                              </div>
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
                                                                        {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
                                                                        {archive.validations.map((val: any, idx: number) => {
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
                                                                        {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
                                                                        {archive.learnings.map((learning: any, idx: number) => (
                                                                          <div key={idx} className="text-xs bg-background border border-border rounded p-2 mb-2">
                                                                            <div className="flex items-center justify-between">
                                                                              <span className="font-medium">{learning.learning_type}</span>
                                                                              {learning.conversation_path && (
                                                                                <button
                                                                                  onClick={() => toggleFileExpansion(learning.conversation_path!)}
                                                                                  disabled={loadingFiles.has(learning.conversation_path!)}
                                                                                  className="text-primary hover:underline text-[10px] font-medium"
                                                                                >
                                                                                  {loadingFiles.has(learning.conversation_path!) ? 'Loading...' : expandedFiles.has(learning.conversation_path!) ? 'Hide' : 'View'}
                                                                                </button>
                                                                              )}
                                                                            </div>
                                                                            {learning.result && (
                                                                              <p className="text-muted-foreground mt-1 line-clamp-2">{learning.result}</p>
                                                                            )}
                                                                            {expandedFiles.has(learning.conversation_path!) && (
                                                                              <div className="mt-2 pt-2 border-t border-border">
                                                                                {fileContents[learning.conversation_path!] ? (
                                                                                  <pre className="whitespace-pre-wrap overflow-x-auto text-muted-foreground max-h-[60vh] overflow-y-auto font-mono text-[10px]">
                                                                                    {fileContents[learning.conversation_path!]}
                                                                                  </pre>
                                                                                ) : (
                                                                                  <div className="flex items-center gap-2 py-2 text-muted-foreground">
                                                                                    <Loader2 className="w-3 h-3 animate-spin" />
                                                                                    Loading...
                                                                                  </div>
                                                                                )}
                                                                              </div>
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
                              {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
                              {archive.orchestration.map((orch: any, idx: number) => (
                                <div key={idx} className="text-xs bg-background border border-border rounded p-2 mb-1">
                                  <span className="font-medium">{orch.type}</span>
                                  {orch.selected_route_id && (
                                    <span className="ml-2 text-muted-foreground">Route: {orch.selected_route_id}</span>
                                  )}
                                </div>
                              ))}
                            </div>
                          )}

                          {/* Archived Todo Task */}
                          {archive.todo_task && archive.todo_task.length > 0 && (
                            <div>
                              <div className="text-xs font-semibold text-muted-foreground mb-2 flex items-center gap-1">
                                <ListTodo className="w-3 h-3" /> Todo Task ({archive.todo_task.length})
                              </div>
                              {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
                              {archive.todo_task.map((task: any, idx: number) => (
                                <div key={idx} className="text-xs bg-background border border-border rounded p-2 mb-1">
                                  <span className="font-medium">{task.type}</span>
                                  {task.todo_task_response?.selected_route_id && (
                                    <span className="ml-2 text-muted-foreground">Route: {task.todo_task_response.selected_route_id}</span>
                                  )}
                                  {task.todo_task_response?.use_generic_agent && (
                                    <span className="ml-2 text-muted-foreground">Generic Agent</span>
                                  )}
                                  {task.todo_task_response?.all_tasks_complete && (
                                    <span className="ml-2 text-green-600 dark:text-green-400">✓ Complete</span>
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
                              {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
                              {archive.conditionals.map((cond: any, idx: number) => (
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

                        </div>
                      )}
                    </div>
                  )
                })}
              </div>
            </div>
          )}

          {/* Archived Execution Runs Section (from decision step routing) */}
          {stepLogs.archived_executions && stepLogs.archived_executions.filter(matchesSearch).length > 0 && (
            <div className="p-4 bg-blue-500/5 border-t border-blue-500/20">
              <h4 className="text-xs font-semibold text-blue-600 dark:text-blue-400 uppercase tracking-wider mb-3 flex items-center gap-2">
                <Archive className="w-4 h-4" /> Archived Execution Runs ({stepLogs.archived_executions.filter(matchesSearch).length})
              </h4>
              <div className="space-y-3">
                {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
                {stepLogs.archived_executions.filter(matchesSearch).map((archive: any, archiveIdx: number) => {
                  const archiveId = `${stepId}-archived-exec-${archiveIdx}`
                  const isArchiveExpanded = expandedArchived.has(archiveId)
                  const hasOutput = !!archive.output_content
                  const artifactCount = archive.artifacts?.length || 0

                  return (
                    <div key={archiveIdx} className="bg-background rounded border border-blue-500/30 overflow-hidden">
                      <button
                        onClick={() => toggleArchived(archiveId)}
                        className="w-full flex items-center gap-3 p-3 text-left hover:bg-blue-500/10 transition-colors"
                      >
                        {isArchiveExpanded ? <ChevronDown className="w-4 h-4 text-blue-500" /> : <ChevronRight className="w-4 h-4 text-blue-500" />}
                        <div className="flex-1 min-w-0">
                          <div className="flex items-center justify-between">
                            <span className="text-sm font-medium text-foreground">
                              Run {archive.run_number}
                            </span>
                            <span className="text-xs text-muted-foreground">
                              {hasOutput ? '1 output' : ''}{hasOutput && artifactCount > 0 ? ', ' : ''}{artifactCount > 0 ? `${artifactCount} artifact${artifactCount !== 1 ? 's' : ''}` : ''}
                            </span>
                          </div>
                        </div>
                      </button>

                      {isArchiveExpanded && (
                        <div className="border-t border-blue-500/20 p-3 space-y-3 bg-muted/20">
                          {/* Archived Output Content */}
                          {archive.output_content && (
                            <div>
                              <div className="text-xs font-semibold text-muted-foreground mb-2 flex items-center gap-1">
                                <FileText className="w-3 h-3" /> Output
                              </div>
                              <div className="text-xs bg-background border border-border rounded p-2">
                                <div className="flex items-center justify-between mb-1">
                                  <span className="font-mono text-[10px] text-muted-foreground truncate max-w-[200px]">
                                    {archive.output_content.file_path?.split('/').pop()}
                                  </span>
                                  <button
                                    onClick={() => toggleFileExpansion(archive.output_content.file_path)}
                                    className="text-primary hover:underline text-[10px] font-medium"
                                  >
                                    {expandedFiles.has(archive.output_content.file_path) ? 'Hide' : 'View'}
                                  </button>
                                </div>
                                {expandedFiles.has(archive.output_content.file_path) && (
                                  <pre className="whitespace-pre-wrap overflow-x-auto text-muted-foreground max-h-[60vh] overflow-y-auto font-mono text-[10px] mt-2 pt-2 border-t border-border">
                                    {archive.output_content.is_json
                                      ? JSON.stringify(archive.output_content.content, null, 2)
                                      : String(archive.output_content.content)}
                                  </pre>
                                )}
                              </div>
                            </div>
                          )}

                          {/* Archived Artifacts */}
                          {archive.artifacts && archive.artifacts.length > 0 && (
                            <div>
                              <div className="text-xs font-semibold text-muted-foreground mb-2 flex items-center gap-1">
                                <FileText className="w-3 h-3" /> Artifacts ({archive.artifacts.length})
                              </div>
                              <div className="space-y-1">
                                {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
                                {archive.artifacts.map((artifact: any, idx: number) => (
                                  <div key={idx} className="text-xs bg-background border border-border rounded p-2">
                                    <div className="flex items-center justify-between">
                                      <span className="font-mono text-[10px] text-muted-foreground truncate max-w-[200px]">
                                        {artifact.file_name}
                                      </span>
                                      <button
                                        onClick={() => toggleFileExpansion(artifact.file_path)}
                                        disabled={loadingFiles.has(artifact.file_path)}
                                        className="text-primary hover:underline text-[10px] font-medium"
                                      >
                                        {loadingFiles.has(artifact.file_path) ? 'Loading...' : expandedFiles.has(artifact.file_path) ? 'Hide' : 'View'}
                                      </button>
                                    </div>
                                    {expandedFiles.has(artifact.file_path) && (
                                      <div className="mt-2 pt-2 border-t border-border">
                                        {fileContents[artifact.file_path] ? (
                                          <pre className="whitespace-pre-wrap overflow-x-auto text-muted-foreground max-h-[60vh] overflow-y-auto font-mono text-[10px]">
                                            {fileContents[artifact.file_path]}
                                          </pre>
                                        ) : (
                                          <div className="flex items-center gap-2 py-2 text-muted-foreground">
                                            <Loader2 className="w-3 h-3 animate-spin" />
                                            Loading...
                                          </div>
                                        )}
                                      </div>
                                    )}
                                  </div>
                                ))}
                              </div>
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
      )
  }

  if (!isOpen) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm p-2 sm:p-4">
      <div className="bg-background rounded-lg shadow-xl w-full max-w-[calc(100vw-1rem)] sm:max-w-[90vw] h-[calc(100dvh-1rem)] sm:h-[95vh] flex flex-col border border-border relative">
        {/* Header */}
        <div className="flex items-start justify-between gap-3 px-4 py-3 border-b border-border sm:px-6 sm:py-4">
          <div className="flex-1 min-w-0">
            <h2 className="text-lg font-semibold text-foreground flex items-center gap-2">
              <Terminal className="w-5 h-5 text-primary" />
              Execution Logs
            </h2>
            <div className="flex flex-wrap items-center gap-2 mt-1 sm:gap-4">
              {/* Run Folder Selector */}
              {runFolderOptions.length > 0 && (
                <div className="flex min-w-0 items-center gap-2">
                  <Filter className="w-4 h-4 text-muted-foreground" />
                  <select
                    value={selectedRunFolder}
                    onChange={(e) => setSelectedRunFolder(e.target.value)}
                    className="min-w-0 max-w-full text-xs bg-muted border border-border rounded-md px-2 py-1 text-foreground sm:min-w-[200px]"
                  >
                    <option value="">Select iteration/group...</option>
                    {runFolderOptions.map(folder => (
                      <option key={folder} value={folder}>{folder}</option>
                    ))}
                  </select>
                </div>
              )}
              {selectedRunFolder && (
                <p className="min-w-0 text-sm text-muted-foreground">
                  Run: <span className="font-mono text-xs bg-muted px-1.5 py-0.5 rounded text-foreground">{selectedRunFolder}</span>
                </p>
              )}
              <button
                onClick={loadLogs}
                disabled={loading || !selectedRunFolder}
                className="p-1.5 rounded-md hover:bg-muted transition-colors text-muted-foreground hover:text-foreground disabled:opacity-50 disabled:cursor-not-allowed"
                title="Refresh logs"
              >
                <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
              </button>
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
          ) : !selectedRunFolder ? (
            <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
              <FileText className="w-12 h-12 mb-3 opacity-50" />
              <p className="text-sm font-medium">Select an iteration or group to view logs</p>
              <p className="text-xs mt-2 opacity-70">
                {runFolderOptions.length > 0 
                  ? `Choose from ${runFolderOptions.length} available ${runFolderOptions.length === 1 ? 'run' : 'runs'} above.`
                  : 'No run folders available. Execute a workflow to generate logs.'}
              </p>
            </div>
          ) : (
            <div className="space-y-4">
              {/* Message when no step logs found */}
              {logs && Object.keys(logs.steps).length === 0 && (
                <div className="flex flex-col items-center justify-center py-8 text-muted-foreground border border-dashed border-border rounded-lg">
                  <FileText className="w-10 h-10 mb-2 opacity-50" />
                  <p className="text-sm">No step execution logs found for <span className="font-mono text-xs bg-muted px-1.5 py-0.5 rounded">{selectedRunFolder}</span>.</p>
                  {runFolders.length > 1 && (
                    <p className="text-xs mt-2 opacity-70">
                      Try selecting a different iteration or group from the dropdown above.
                    </p>
                  )}
                </div>
              )}

              {Object.entries(logs?.steps || {})
                .sort((a, b) => sortStepIds(a[0], b[0]))
                .map(([stepId, stepLogs]) => {
                  const isExpanded = expandedSteps.has(stepId)
                  // Determine overall status based on step_done.json, validations, or other activity
                  const title = stepLogs.title || stepId
                  const description = stepLogs.description || ''
                  const nestingLevel = getStepNestingLevel(stepId)
                  const indentStyle = getStepIndentStyle(nestingLevel)
                  const nestingClass = getStepNestingClass(stepId)
                  const stepMetrics = getStepMetrics(stepLogs.executions || [])
                  const showMetrics = hasStepMetrics(stepMetrics)

                  return (
                    <div key={stepId} className={`border border-border rounded-lg overflow-hidden bg-card ${nestingClass}`} style={indentStyle}>
                      <button
                        onClick={() => toggleStep(stepId)}
                        className={`
                          w-full flex flex-col gap-2 px-4 py-3 text-left transition-colors
                          ${isExpanded ? 'bg-accent/50' : 'hover:bg-accent/50'}
                        `}
                      >
                        <div className="flex min-w-0 items-center gap-3 overflow-hidden">
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
                        
                        <div className="flex w-full flex-wrap items-center gap-1.5 pl-7 text-xs text-muted-foreground">
                          {showMetrics && (
                            <>
                              {stepMetrics.totalTokens > 0 && (
                                <StepMetricChip title={`Tokens used: ${stepMetrics.totalTokens.toLocaleString()} total (${stepMetrics.inputTokens.toLocaleString()} input, ${stepMetrics.outputTokens.toLocaleString()} output${stepMetrics.reasoningTokens > 0 ? `, ${stepMetrics.reasoningTokens.toLocaleString()} reasoning` : ''}${stepMetrics.cacheTokens > 0 ? `, ${stepMetrics.cacheTokens.toLocaleString()} cache` : ''})`}>
                                  {formatTokenCount(stepMetrics.totalTokens)} tok
                                </StepMetricChip>
                              )}
                              {stepMetrics.durationMs > 0 && (
                                <StepMetricChip title={`Time taken: ${formatDuration(stepMetrics.durationMs)}${stepMetrics.llmCalls > 0 ? ` across ${stepMetrics.llmCalls} LLM call${stepMetrics.llmCalls !== 1 ? 's' : ''}` : ''}`}>
                                  <Clock className="h-3 w-3" />
                                  {formatDuration(stepMetrics.durationMs)}
                                </StepMetricChip>
                              )}
                            </>
                          )}
                          <span className="whitespace-nowrap">
                            {stepLogs.executions.length} exec • {stepLogs.validations.length} val
                            {stepLogs.todo_task && stepLogs.todo_task.length > 0 && ` • ${stepLogs.todo_task.length} todo`}
                          </span>
                        </div>
                      </button>

                      {isExpanded && renderStepContent(stepId, stepLogs)}
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
      </div>
    </div>
  )
}

export default ExecutionLogsPopup
