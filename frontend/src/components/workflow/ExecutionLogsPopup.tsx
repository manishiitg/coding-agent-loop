import React, { useEffect, useState } from 'react'
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
  ExternalLink
} from 'lucide-react'
import { agentApi } from '../../services/api'
import type { ExecutionLogsResponse } from '../../services/api-types'

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
  runFolder: initialRunFolder,
  runFolders
}) => {
  const [loading, setLoading] = useState(false)
  const [logs, setLogs] = useState<ExecutionLogsResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [expandedSteps, setExpandedSteps] = useState<Set<string>>(new Set())
  const [expandedValidations, setExpandedValidations] = useState<Set<string>>(new Set())
  const [expandedExecutions, setExpandedExecutions] = useState<Set<string>>(new Set())
  const [expandedArchived, setExpandedArchived] = useState<Set<string>>(new Set())
  const [selectedRunFolder, setSelectedRunFolder] = useState<string>(initialRunFolder || '')
  
  // State for viewing full file content (conversation logs)
  const [viewingFile, setViewingFile] = useState<{path: string, title: string, content: string} | null>(null)
  const [loadingFiles, setLoadingFiles] = useState<Set<string>>(new Set())

  // Update selected run folder when prop changes
  useEffect(() => {
    if (initialRunFolder !== selectedRunFolder) {
      setSelectedRunFolder(initialRunFolder || '')
    }
  }, [initialRunFolder, selectedRunFolder])

  useEffect(() => {
    if (isOpen && workspacePath && selectedRunFolder) {
      loadLogs()
    } else {
      setLogs(null)
      setError(null)
      setViewingFile(null)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isOpen, workspacePath, selectedRunFolder])

  const loadLogs = async () => {
    if (!workspacePath || !selectedRunFolder) return
    
    setLoading(true)
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
      setLoading(false)
    }
  }

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

  const handleJumpToStep = (stepId: string) => {
    setExpandedSteps(prev => new Set(prev).add(stepId))
    setTimeout(() => {
        const element = document.getElementById(`step-${stepId}`)
        if (element) {
            element.scrollIntoView({ behavior: 'smooth', block: 'center' })
            element.classList.add('ring-2', 'ring-primary', 'ring-offset-2')
            setTimeout(() => {
                element.classList.remove('ring-2', 'ring-primary', 'ring-offset-2')
            }, 2000)
        }
    }, 100)
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

  // Recursive render function for step content
  const renderStepContent = (stepId: string, stepLogs: any) => {
      const validations = stepLogs.validations || []
      
      return (
        <div className="border-t border-border divide-y divide-border">
          {/* Executions Section */}
          {stepLogs.executions.length > 0 && (
            <div className="p-4 bg-background">
              <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-3">Execution Logs</h4>
              <div className="space-y-3">
                {stepLogs.executions.map((exec: any, idx: number) => {
                  const execId = `${stepId}-exec-${exec.attempt}-${exec.iteration}`
                  const isExecExpanded = expandedExecutions.has(execId)
                  const result = exec.content?.execution_result
                  const model = exec.content?.model
                  
                  return (
                    <div key={idx} className="bg-background rounded border border-border overflow-hidden">
                      <button
                        onClick={() => toggleExecution(execId)}
                        className="w-full flex items-start gap-3 p-3 text-left hover:bg-accent/50 transition-colors"
                      >
                        <Terminal className="w-4 h-4 mt-0.5 text-muted-foreground flex-shrink-0" />
                        <div className="flex-1 min-w-0">
                          <div className="flex items-center justify-between mb-1">
                            <div className="flex items-center gap-2">
                              <span className="text-sm font-medium text-foreground">
                                Attempt {exec.attempt} {exec.iteration > 0 && `(Iteration ${exec.iteration})`}
                              </span>
                              {model && (
                                <span className="text-[10px] font-mono bg-muted px-1.5 py-0.5 rounded text-muted-foreground border border-border">
                                  {model}
                                </span>
                              )}
                            </div>
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
                {validations.map((val: any, idx: number) => {
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
                {stepLogs.learnings.map((log: any, idx: number) => (
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
              <div className="space-y-6">
                {Object.entries(
                  stepLogs.orchestration.reduce((acc: Record<number, any[]>, log: any) => {
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
                                        <div className="p-3 bg-muted/30 rounded border border-border font-mono whitespace-pre-wrap text-muted-foreground max-h-60 overflow-y-auto text-[11px] leading-relaxed">
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
                                <pre className="mt-2 text-[10px] font-mono whitespace-pre-wrap overflow-x-auto text-muted-foreground bg-muted/50 p-2 rounded max-h-40 overflow-y-auto border border-border">
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
          {/* CONDITIONALS_SECTION */}
          {/* DECISIONS_SECTION */}
          {/* ARCHIVED_LOGS_SECTION */}
        </div>
      )
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
              {/* Run Folder Selector */}
              {runFolders.length > 0 && (
                <div className="flex items-center gap-2">
                  <Filter className="w-4 h-4 text-muted-foreground" />
                  <select
                    value={selectedRunFolder}
                    onChange={(e) => setSelectedRunFolder(e.target.value)}
                    className="text-xs bg-muted border border-border rounded-md px-2 py-1 text-foreground min-w-[200px]"
                  >
                    <option value="">Select iteration/group...</option>
                    {runFolders.map(folder => (
                      <option key={folder} value={folder}>{folder}</option>
                    ))}
                  </select>
                </div>
              )}
              {selectedRunFolder && (
                <p className="text-sm text-muted-foreground whitespace-nowrap">
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
                {runFolders.length > 0 
                  ? `Choose from ${runFolders.length} available ${runFolders.length === 1 ? 'run' : 'runs'} above.`
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
                              <div className="space-y-6">
                                {Object.entries(
                                  stepLogs.orchestration.reduce((acc: Record<number, any[]>, log: any) => {
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
                                              {log.model && (
                                                <span className="font-mono text-[10px] bg-muted px-1.5 py-0.5 rounded text-muted-foreground border border-border">
                                                  {log.model}
                                                </span>
                                              )}
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
                                                            <button
                                                                onClick={() => handleJumpToStep(log.orchestration_response.selected_sub_agent_path)}
                                                                className="text-[10px] flex items-center gap-1 bg-background border border-border px-2 py-1 rounded hover:bg-muted transition-colors text-muted-foreground hover:text-foreground"
                                                            >
                                                                <ExternalLink className="w-3 h-3" />
                                                                View Execution
                                                            </button>
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
                                                        <div className="p-3 bg-muted/30 rounded border border-border font-mono whitespace-pre-wrap text-muted-foreground max-h-60 overflow-y-auto text-[11px] leading-relaxed">
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
                                                <pre className="mt-2 text-[10px] font-mono whitespace-pre-wrap overflow-x-auto text-muted-foreground bg-muted/50 p-2 rounded max-h-40 overflow-y-auto border border-border">
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