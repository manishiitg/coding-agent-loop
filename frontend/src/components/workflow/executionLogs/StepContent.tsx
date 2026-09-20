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
  Network,
  Bot,
  Split,
  BookOpen,
  History,
  ListTodo,
  Archive,
  Search,
  Users,
  ExternalLink,
} from 'lucide-react'
import type { ExecutionLogsResponse } from '../../../services/api-types'
import { useProductSurfaceStore } from '../../../stores/useProductSurfaceStore'
import { ConversationViewer } from '../ConversationViewer'
import { MarkdownRenderer } from '../../ui/MarkdownRenderer'
import { StepMetadata, StructuredJsonView } from './LogPrimitives'
import {
  formatDuration,
  formatTokenCount,
  getExecutionMetrics,
  getExecutionOrigin,
  getMessageSequenceReflection,
  getSentAgentMessages,
  type ValidationFeedback,
} from './helpers'

export interface StepContentProps {
  stepId: string
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  stepLogs: any
  logs: ExecutionLogsResponse | null
  stepSearchQueries: Record<string, string>
  setStepSearchQueries: React.Dispatch<React.SetStateAction<Record<string, string>>>
  expandedValidations: Set<string>
  toggleValidation: (id: string) => void
  expandedExecutions: Set<string>
  toggleExecution: (id: string) => void
  expandedArchived: Set<string>
  toggleArchived: (id: string) => void
  expandedFiles: Set<string>
  fileContents: Record<string, string>
  loadingFiles: Set<string>
  toggleFileExpansion: (path: string) => void
}

interface CrewRunRecord {
  crew_profile_id?: string
  crew_project_id?: string
  trigger_id?: string
  crew_run_id?: string
  session_id?: string
  status?: string
  error?: string
  started_at?: string
  completed_at?: string
  usage?: {
    prompt_tokens?: number
    completion_tokens?: number
    cost_usd?: number
    llm_call_count?: number
  }
  timeline?: { status?: string; at?: string }[]
}

const crewStatusChip = (status: string): string => {
  switch (status) {
    case 'success':
      return 'bg-emerald-500/15 text-emerald-600 dark:text-emerald-400'
    case 'error':
    case 'failed':
      return 'bg-red-500/15 text-red-600 dark:text-red-400'
    case 'running':
    case 'queued':
      return 'bg-blue-500/15 text-blue-600 dark:text-blue-400'
    default:
      return 'bg-muted text-muted-foreground'
  }
}

// CrewRunSection renders the crew-run.json artifact of a crew step inline:
// the run's identity, status timeline, token spend, and a jump into the
// owning crew. Content loads through the same artifact loader as the
// Artifacts section below, so nothing fetches twice.
function CrewRunSection({
  stepLogs,
  fileContents,
  loadingFiles,
  toggleFileExpansion,
}: {
  stepLogs: StepContentProps['stepLogs']
  fileContents: Record<string, string>
  loadingFiles: Set<string>
  toggleFileExpansion: (path: string) => void
}) {
  const record = ((stepLogs.artifacts || []) as { file_name?: string; file_path: string }[]).find(
    (artifact) => artifact.file_name === 'crew-run.json',
  )
  if (!record) return null
  const raw = fileContents[record.file_path]
  let parsed: CrewRunRecord | null = null
  if (raw) {
    try {
      parsed = JSON.parse(typeof raw === 'string' ? raw : JSON.stringify(raw)) as CrewRunRecord
    } catch {
      parsed = null
    }
  }
  const status = parsed?.status || ''
  const usage = parsed?.usage
  const timeline = Array.isArray(parsed?.timeline) ? parsed.timeline : []
  const openInCrew = () => {
    if (!parsed?.crew_project_id) return
    const surfaces = useProductSurfaceStore.getState()
    surfaces.setSelectedWorkProjectId(parsed.crew_project_id)
    surfaces.setProductSurface('work')
  }

  return (
    <div className="p-4 bg-muted/30">
      <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-3 flex items-center gap-2">
        <Users className="w-3.5 h-3.5" />
        Crew run
        {status && (
          <span className={`rounded px-1.5 py-0.5 font-mono text-[10px] normal-case ${crewStatusChip(status)}`}>{status}</span>
        )}
      </h4>
      {!parsed ? (
        <button
          onClick={() => toggleFileExpansion(record.file_path)}
          disabled={loadingFiles.has(record.file_path)}
          className="inline-flex items-center gap-2 rounded-md border border-border bg-background px-2.5 py-1.5 text-xs text-foreground/80 transition-colors hover:bg-accent/50 disabled:cursor-wait disabled:opacity-60"
        >
          {loadingFiles.has(record.file_path) ? <Loader2 className="w-3 h-3 animate-spin" /> : <FileText className="w-3 h-3" />}
          {loadingFiles.has(record.file_path) ? 'Loading crew run…' : 'Load crew run record'}
        </button>
      ) : (
        <div className="space-y-3">
          <dl className="grid grid-cols-2 gap-x-4 gap-y-1.5 text-xs">
            <div className="flex items-baseline gap-2 min-w-0">
              <dt className="shrink-0 text-muted-foreground">Crew</dt>
              <dd className="truncate font-mono text-foreground/90" title={parsed.crew_project_id}>{parsed.crew_project_id || '—'}</dd>
            </div>
            <div className="flex items-baseline gap-2 min-w-0">
              <dt className="shrink-0 text-muted-foreground">Run</dt>
              <dd className="truncate font-mono text-foreground/90" title={parsed.crew_run_id}>{parsed.crew_run_id || '—'}</dd>
            </div>
            {parsed.session_id && (
              <div className="flex items-baseline gap-2 min-w-0 col-span-2">
                <dt className="shrink-0 text-muted-foreground">Session</dt>
                <dd className="truncate font-mono text-foreground/90" title={parsed.session_id}>{parsed.session_id}</dd>
              </div>
            )}
            {usage && (usage.prompt_tokens || usage.completion_tokens) ? (
              <div className="flex items-baseline gap-2 min-w-0 col-span-2">
                <dt className="shrink-0 text-muted-foreground">Spend</dt>
                <dd className="text-foreground/90">
                  {(usage.prompt_tokens || 0).toLocaleString()} in / {(usage.completion_tokens || 0).toLocaleString()} out
                  {typeof usage.cost_usd === 'number' && usage.cost_usd > 0 && <> · ${usage.cost_usd.toFixed(4)}</>}
                </dd>
              </div>
            ) : null}
          </dl>
          {parsed.error && (
            <p className="text-xs leading-relaxed text-red-600 dark:text-red-400">{parsed.error}</p>
          )}
          {timeline.length > 0 && (
            <ol className="space-y-1">
              {timeline.map((transition, index) => (
                <li key={`${transition.status}-${index}`} className="flex items-center gap-2 text-xs text-foreground/80">
                  <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-muted-foreground/50" />
                  <span className="font-mono">{transition.status || '—'}</span>
                  {transition.at && (
                    <span className="text-muted-foreground">{new Date(transition.at).toLocaleTimeString()}</span>
                  )}
                </li>
              ))}
            </ol>
          )}
          {parsed.crew_project_id && (
            <button
              onClick={openInCrew}
              className="inline-flex items-center gap-1.5 rounded-md border border-border bg-background px-2.5 py-1.5 text-xs text-foreground/80 transition-colors hover:bg-accent/50"
            >
              <ExternalLink className="w-3 h-3" />
              Open in Crew
            </button>
          )}
        </div>
      )}
    </div>
  )
}

// Recursive: sub-agent executions render the nested step's content through
// the same component.
export function StepContent(props: StepContentProps) {
  const {
    stepId,
    stepLogs,
    logs,
    stepSearchQueries,
    setStepSearchQueries,
    expandedValidations,
    toggleValidation,
    expandedExecutions,
    toggleExecution,
    expandedArchived,
    toggleArchived,
    expandedFiles,
    fileContents,
    loadingFiles,
    toggleFileExpansion,
  } = props
      const validations = stepLogs.validations || []
      const searchQuery = stepSearchQueries[stepId] || ''
      const searchNeedle = searchQuery.toLowerCase()

      const matchesSearch = (item: unknown) => {
        if (!searchQuery) return true
        return JSON.stringify(item).toLowerCase().includes(searchNeedle)
      }

      // Same identity rule as before (run + output path + sorted artifact
      // paths), keeping the first occurrence; one pass instead of a
      // findIndex-inside-filter that re-stringified every pair.
      const seenArchiveIdentities = new Set<string>()
      const visibleArchivedExecutions = (stepLogs.archived_executions || [])
        .filter((archive: any) => archive.output_content || (archive.artifacts?.length || 0) > 0)
        .filter((archive: any) => {
          const identity = JSON.stringify({
            run: archive.run_number,
            output: archive.output_content?.file_path || '',
            artifacts: (archive.artifacts || []).map((artifact: any) => artifact.file_path).sort(),
          })
          if (seenArchiveIdentities.has(identity)) return false
          seenArchiveIdentities.add(identity)
          return true
        })

      // Each section filtered once per render (they used to be filtered
      // twice: once for the "any?" guard and again for the map).
      const visibleExecutions = stepLogs.executions.filter(matchesSearch)
      const visibleArtifacts = (stepLogs.artifacts || []).filter(matchesSearch)
      const visibleValidations = validations.filter(matchesSearch)
      const visibleLearnings = (stepLogs.learnings || []).filter(matchesSearch)
      const visibleOrchestration = (stepLogs.orchestration || []).filter(matchesSearch)
      const visibleTodoTask = (stepLogs.todo_task || []).filter(matchesSearch)
      const visibleArchivedLogs = (stepLogs.archived_logs || []).filter(matchesSearch)
      const visibleArchivedRuns = visibleArchivedExecutions.filter(matchesSearch)
      
      return (
        <div className="border-t border-border divide-y divide-border">
          {/* Local Search Input */}
          <div className="px-4 py-2 bg-muted/10 border-b border-border flex items-center gap-2 sticky top-0 z-10 backdrop-blur-sm">
            <Search className="w-3.5 h-3.5 text-muted-foreground" />
            <input
              type="text"
              placeholder="Search results, tools, and artifacts..."
              aria-label={`Search ${stepLogs.title || stepId} execution details`}
              value={searchQuery}
              onChange={(e) => setStepSearchQueries(prev => ({ ...prev, [stepId]: e.target.value }))}
              className="text-xs bg-transparent border-none focus:outline-none focus:ring-0 w-full placeholder:text-muted-foreground/70 text-foreground"
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

          {/* A message-sequence session is its own durable execution trace. Its
              closing Reflection item is intentionally surfaced separately so
              operators can tell whether the sequence actually reflected, not
              merely whether the enclosing step completed. */}
          {stepLogs.message_sequence && (
            <div className="p-4 bg-teal-500/[0.03] border-b border-teal-500/15">
              {(() => {
                const reflection = getMessageSequenceReflection(stepLogs)
                const sessionEntries = stepLogs.message_sequence.entries || []
                const reflectionStatus = reflection?.status || 'not run'
                const reflectionClass = reflectionStatus === 'completed'
                  ? 'border-emerald-500/25 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
                  : reflectionStatus === 'failed'
                    ? 'border-rose-500/25 bg-rose-500/10 text-rose-700 dark:text-rose-300'
                    : 'border-border bg-muted/40 text-muted-foreground'
                return (
                  <>
                    <div className="flex flex-wrap items-center gap-2">
                      <MessageSquare className="w-4 h-4 text-teal-600 dark:text-teal-300" />
                      <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Agent</h4>
                      <span className="rounded border border-teal-500/20 bg-teal-500/10 px-2 py-0.5 text-[10px] font-medium text-teal-700 dark:text-teal-300">
                        {stepLogs.message_sequence.status || 'recorded'}
                      </span>
                      <span className={`rounded border px-2 py-0.5 text-[10px] font-medium ${reflectionClass}`}>
                        Reflection: {reflectionStatus}
                      </span>
                    </div>
                    <p className="mt-2 text-xs text-muted-foreground">
                      {sessionEntries.length} recorded turn{sessionEntries.length === 1 ? '' : 's'} in this sequence.
                    </p>
                    {reflection?.summary && (
                      <details className="mt-3 rounded border border-teal-500/15 bg-background/70 p-2.5">
                        <summary className="cursor-pointer text-xs font-medium text-teal-700 dark:text-teal-300">View reflection result</summary>
                        <p className="mt-2 whitespace-pre-wrap text-xs leading-relaxed text-foreground/85">{reflection.summary}</p>
                      </details>
                    )}
                  </>
                )
              })()}
            </div>
          )}
          {/* Executions Section */}
          {visibleExecutions.length > 0 && (
            <div className="p-4 bg-background">
              <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-3">Execution Logs</h4>
              <div className="space-y-3">
                {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
                {visibleExecutions.map((exec: any, idx: number) => {
                  const execId = `${stepId}-exec-${exec.attempt}-${exec.iteration}`
                  // executions is already filtered to searchQuery matches above,
                  // so an active search implies every rendered row is a hit --
                  // force it open instead of making the user click through each one.
                  const isExecExpanded = expandedExecutions.has(execId) || !!searchQuery
                  const isFastPath = exec.fast_path === true
                  const execMetrics = getExecutionMetrics(exec)
                  // Fast-path entries carry ScriptedFastPathLog shape: success/exit_code/output/error.
                  // LLM-attempt entries carry ExecutionResult shape with execution_result/model.
                  const result = isFastPath
                    ? (exec.content?.success ? (exec.content?.output || '') : (exec.content?.error || exec.content?.output || ''))
                    : exec.content?.execution_result
                  const model = isFastPath ? null : exec.content?.model
                  const fpSuccess = isFastPath ? exec.content?.success === true : null
                  const fpExit = isFastPath ? exec.content?.exit_code : null
                  const executionOrigin = isFastPath ? null : getExecutionOrigin(exec, validations, stepLogs.planned_messages || [])
                  const sentMessages = expandedFiles.has(exec.conversation_path) && fileContents[exec.conversation_path]
                    ? getSentAgentMessages(fileContents[exec.conversation_path])
                    : []
                  // "Attempt N" is a fixed retry-slot label, not chronological order — a
                  // fresh top-level re-run overwrites slot 1's file while an unrelated
                  // older retry can still occupy slot 2, hours or days apart. Show the
                  // real timestamp so which attempt is actually newest is never a guess.
                  const execTimestamp = exec.content?.completed_at ?? exec.content?.started_at
                  const execTimestampMs = typeof execTimestamp === 'string' ? Date.parse(execTimestamp) : NaN

                  return (
                    <div key={idx} className={`bg-background rounded border overflow-hidden ${isFastPath ? 'border-indigo-200 dark:border-indigo-800' : 'border-border'}`}>
                      <button
                        onClick={() => toggleExecution(execId)}
                        aria-expanded={isExecExpanded}
                        aria-label={`${isExecExpanded ? 'Collapse' : 'Expand'} ${isFastPath ? 'saved fast-path execution' : `attempt ${exec.attempt}`}`}
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
                              {executionOrigin && (
                                <span title={executionOrigin.detail} className={`text-[10px] font-medium px-1.5 py-0.5 rounded border ${executionOrigin.className}`}>
                                  {executionOrigin.label}
                                </span>
                              )}
                              {isFastPath && (
                                <span className={`text-[10px] font-medium px-1.5 py-0.5 rounded border ${
                                  fpSuccess
                                    ? 'bg-emerald-500/10 text-emerald-600 border-emerald-500/20 dark:bg-emerald-500/20 dark:text-emerald-300 dark:border-emerald-500/30'
                                    : 'bg-rose-500/10 text-rose-600 border-rose-500/20 dark:bg-rose-500/20 dark:text-rose-300 dark:border-rose-500/30'
                                }`}>
                                  {fpSuccess ? 'ok' : 'fail'}{fpExit !== undefined ? ` · exit=${fpExit}` : ''}
                                </span>
                              )}
                              {model && (
                                <span className="text-[10px] font-mono bg-muted px-1.5 py-0.5 rounded text-muted-foreground border border-border">
                                  {model}
                                </span>
                              )}
                              {execMetrics.inputTokens > 0 && (
                                <span title={`Input tokens: ${execMetrics.inputTokens.toLocaleString()}`} className="text-[10px] font-medium bg-muted px-1.5 py-0.5 rounded text-muted-foreground border border-border">
                                  {formatTokenCount(execMetrics.inputTokens)} in
                                </span>
                              )}
                              {execMetrics.outputTokens > 0 && (
                                <span title={`Output tokens: ${execMetrics.outputTokens.toLocaleString()}${execMetrics.reasoningTokens > 0 ? ` (includes ${execMetrics.reasoningTokens.toLocaleString()} reasoning)` : ''}`} className="text-[10px] font-medium bg-muted px-1.5 py-0.5 rounded text-muted-foreground border border-border">
                                  {formatTokenCount(execMetrics.outputTokens)} out
                                </span>
                              )}
                              {execMetrics.cacheTokens > 0 && (
                                <span title={`Cached tokens: ${execMetrics.cacheTokens.toLocaleString()}`} className="text-[10px] font-medium bg-muted px-1.5 py-0.5 rounded text-muted-foreground border border-border">
                                  {formatTokenCount(execMetrics.cacheTokens)} cache
                                </span>
                              )}
                              {execMetrics.durationMs > 0 && (
                                <span className="text-[10px] font-medium bg-muted px-1.5 py-0.5 rounded text-muted-foreground border border-border">
                                  {formatDuration(execMetrics.durationMs)}
                                </span>
                              )}
                              {!Number.isNaN(execTimestampMs) && (
                                <span className="text-[10px] text-muted-foreground" title={`Completed: ${new Date(execTimestampMs).toLocaleString()}`}>
                                  {new Date(execTimestampMs).toLocaleString()}
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
                          {executionOrigin && (
                            <p className="mt-1 text-[11px] leading-relaxed text-muted-foreground line-clamp-2">
                              Why it ran: {executionOrigin.detail}
                            </p>
                          )}
                          {executionOrigin?.plannedMessage && (
                            <p className="mt-1 text-[11px] leading-relaxed text-sky-700 dark:text-sky-300 line-clamp-2">
                              Planned message: {executionOrigin.plannedMessage}
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
                                  <div className="font-semibold text-rose-600 dark:text-rose-400 mb-1">error:</div>
                                  <pre className="whitespace-pre-wrap overflow-x-auto text-rose-700 dark:text-rose-300 max-h-[40vh] overflow-y-auto bg-rose-500/10 dark:bg-rose-950/20 border border-rose-500/20 dark:border-rose-900/30 rounded p-2 mb-3">
                                    {exec.content.error}
                                  </pre>
                                </>
                              )}
                              <StructuredJsonView value={exec.content} />
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
                                  {expandedFiles.has(exec.conversation_path) ? 'Hide Message & Conversation' : 'View Message & Conversation'}
                                </button>
                              </div>

                              {expandedFiles.has(exec.conversation_path) && (
                                <div className="mb-4 bg-background rounded border border-border p-3">
                                  {sentMessages.length > 0 && (
                                    <div className="mb-4 rounded border border-sky-500/20 bg-sky-500/[0.04] p-3">
                                      <div className="mb-2 text-[10px] font-semibold uppercase tracking-wider text-sky-700 dark:text-sky-300">
                                        Messages sent to the agent ({sentMessages.length})
                                      </div>
                                      <div className="space-y-2">
                                        {sentMessages.map((sentMessage, messageIndex) => (
                                          <details key={`${sentMessage.label}-${messageIndex}`} className="rounded border border-sky-500/15 bg-background/70 p-2">
                                            <summary className="cursor-pointer text-xs font-medium text-foreground">{sentMessage.label}</summary>
                                            <p className="mt-2 whitespace-pre-wrap text-xs leading-relaxed text-foreground/85">{sentMessage.message}</p>
                                          </details>
                                        ))}
                                      </div>
                                    </div>
                                  )}
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
                              {executionOrigin?.plannedMessage && (
                                <div className="mb-3 rounded border border-sky-500/20 bg-sky-500/[0.04] p-3">
                                  <div className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-sky-700 dark:text-sky-300">Planned message sent to the agent</div>
                                  <p className="whitespace-pre-wrap font-sans text-xs leading-relaxed text-foreground">{executionOrigin.plannedMessage}</p>
                                </div>
                              )}
                              <StructuredJsonView value={exec.content} />
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

          {/* Crew run record (crew steps only) */}
          {stepLogs.type === 'crew' && (
            <CrewRunSection
              stepLogs={stepLogs}
              fileContents={fileContents}
              loadingFiles={loadingFiles}
              toggleFileExpansion={toggleFileExpansion}
            />
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
                      <StructuredJsonView value={stepLogs.output_content.content} label="Output data" collapsed={false} />
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
          {visibleArtifacts.length > 0 && (
            <div className="p-4 bg-gray-50 dark:bg-gray-900/30">
              <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-3 flex items-center gap-2">
                <FileText className="w-3.5 h-3.5" />
                Artifacts & Files
              </h4>
              <div className="space-y-2">
                {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
                {visibleArtifacts.map((artifact: any, idx: number) => {
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
                            <StructuredJsonView value={fileContents[artifact.file_path]} label={artifact.file_name || 'File data'} collapsed={false} />
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
          {visibleValidations.length > 0 && (
            <div className="p-4 bg-muted/30">
              <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-3">Validations</h4>
              <div className="space-y-3">
                {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
                {visibleValidations.map((val: any, idx: number) => {
                  const valId = `${stepId}-val-${val.kind || 'validation'}-${val.attempt}`
                  const isValExpanded = expandedValidations.has(valId)
                  const valStatus = val.content?.execution_status
                  const isAutomaticFinalValidation = val.kind === 'pre_validation' && val.phase === 'message-sequence-automatic-final-validation'
                  const valPassed = val.content?.overall_pass
                  const passedChecks = val.content?.passed_checks
                  const failedChecks = val.content?.failed_checks
                  const reasoning = val.content?.reasoning
                  const feedback = (val.content?.feedback || []) as ValidationFeedback[]
                  const firstError = Array.isArray(val.content?.errors) ? val.content.errors[0] : null
                  const validationSummary = typeof firstError?.Message === 'string'
                    ? firstError.Message
                    : typeof firstError?.message === 'string'
                      ? firstError.message
                      : reasoning
                  const validationSucceeded = valStatus === 'COMPLETED' || valPassed === true
                  const validationFailed = valStatus === 'FAILED' || valPassed === false
                  // val.attempt (validation_attempt) is nearly always 1 — it doesn't
                  // distinguish the initial-check/saved-script/final-gate phases, or
                  // separate retry-slot executions (execution_001 vs execution_002)
                  // within the same phase, which can be many hours apart. Label with
                  // the real distinguishing fields instead of a misleading "attempt N".
                  const validationPhase = val.content?.validation_phase || val.phase
                  const validationExecAttempt = val.content?.execution_attempt
                  const validationTimestampMs = val.content?.timestamp ? Date.parse(val.content.timestamp) : NaN

                  return (
                    <div key={idx} className="bg-background rounded border border-border overflow-hidden">
                      <button
                        onClick={() => toggleValidation(valId)}
                        className="w-full flex items-start gap-3 p-3 text-left hover:bg-accent/50 transition-colors"
                      >
                        <div className={`mt-0.5 w-2 h-2 rounded-full flex-shrink-0 ${validationSucceeded ? 'bg-emerald-500' : validationFailed ? 'bg-rose-500' : 'bg-slate-400 dark:bg-slate-500'}`} />
                        <div className="flex-1 min-w-0">
                          <div className="flex items-center justify-between mb-1">
                            <div className="flex items-center gap-2 flex-wrap">
                              <span className="text-sm font-medium text-foreground">
                                {isAutomaticFinalValidation
                                  ? `Automatic final validation${validationExecAttempt ? ` (execution ${validationExecAttempt})` : ''}`
                                  : `${validationPhase ? `${validationPhase} validation` : 'Validation'}${validationExecAttempt ? ` (execution ${validationExecAttempt})` : ''}`}
                              </span>
                              {!Number.isNaN(validationTimestampMs) && (
                                <span className="text-[10px] text-muted-foreground" title={new Date(validationTimestampMs).toLocaleString()}>
                                  {new Date(validationTimestampMs).toLocaleString()}
                                </span>
                              )}
                              {isAutomaticFinalValidation && (
                                <span className={`text-[10px] font-medium px-1.5 py-0.5 rounded border ${validationSucceeded ? 'border-emerald-500/25 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300' : validationFailed ? 'border-rose-500/25 bg-rose-500/10 text-rose-700 dark:text-rose-300' : 'border-border bg-muted text-muted-foreground'}`}>
                                  {validationSucceeded ? 'passed' : validationFailed ? 'failed' : 'recorded'}
                                </span>
                              )}
                            </div>
                            {isValExpanded ? <ChevronDown className="w-3 h-3 text-muted-foreground" /> : <ChevronRight className="w-3 h-3 text-muted-foreground" />}
                          </div>
                          {isAutomaticFinalValidation && (typeof passedChecks === 'number' || typeof failedChecks === 'number') && (
                            <p className="text-xs text-muted-foreground">
                              {typeof passedChecks === 'number' ? `${passedChecks} passed` : ''}{typeof passedChecks === 'number' && typeof failedChecks === 'number' ? ' · ' : ''}{typeof failedChecks === 'number' ? `${failedChecks} failed` : ''}
                            </p>
                          )}
                          {validationSummary && (
                            <p className="text-xs text-muted-foreground line-clamp-2">
                              {validationSummary}
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
          {visibleLearnings.length > 0 && (
            <div className="p-4 bg-background border-t border-border">
              <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-3 flex items-center gap-2">
                <BookOpen className="w-4 h-4" /> Learning Logs
              </h4>
              <div className="space-y-3">
                {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
                {visibleLearnings.map((log: any, idx: number) => (
                  <div key={idx} className="bg-background rounded border border-border p-3 text-sm">
                    <div className="flex items-center gap-2 mb-2">
                      <span className={`px-2 py-0.5 rounded text-xs uppercase font-medium border ${
                        log.type === 'learning_completed' ? 'bg-emerald-500/10 text-emerald-600 border-emerald-500/20 dark:bg-emerald-500/20 dark:text-emerald-300 dark:border-emerald-500/30' :
                        log.type === 'learning_failed' ? 'bg-rose-500/10 text-rose-600 border-rose-500/20 dark:bg-rose-500/20 dark:text-rose-300 dark:border-rose-500/30' :
                        log.type === 'learning_skipped' ? 'bg-slate-100 text-slate-700 border-slate-200 dark:bg-slate-800 dark:text-slate-300 dark:border-slate-700/50' :
                        'bg-indigo-500/10 text-indigo-600 border-indigo-500/20 dark:bg-indigo-500/20 dark:text-indigo-300 dark:border-indigo-500/30'
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
                      <div className="mt-2 text-xs bg-indigo-500/[0.04] dark:bg-indigo-500/[0.08] p-2 rounded border border-indigo-500/15 dark:border-indigo-500/25">
                        <div className="font-semibold text-indigo-600 dark:text-indigo-300 mb-1 flex items-center gap-1.5">
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
          {visibleOrchestration.length > 0 && (
            <div className="p-4 bg-muted/30 border-t border-border">
              <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-3 flex items-center gap-2">
                <Network className="w-4 h-4" /> Orchestration & Routing Logs
              </h4>
              <div className="space-y-6">
                {Object.entries(
                  visibleOrchestration.reduce((acc: Record<number, any[]>, log: any) => { // eslint-disable-line @typescript-eslint/no-explicit-any
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
                            log.type === 'routing' ? 'bg-indigo-500' :
                            log.type === 'branch' ? 'bg-cyan-500' :
                            log.type === 'evaluation' ? (log.success_criteria_met ? 'bg-emerald-500' : 'bg-rose-500') :
                            'bg-slate-400 dark:bg-slate-500'
                          }`} />

                          <div className="bg-background rounded border border-border p-3 text-sm shadow-sm">
                            <div className="flex items-center gap-2 mb-2">
                              <span className={`font-mono text-[10px] px-1.5 py-0.5 rounded uppercase font-bold tracking-wide border ${
                                log.type === 'routing' ? 'bg-indigo-500/10 text-indigo-600 border-indigo-500/20 dark:bg-indigo-500/20 dark:text-indigo-300 dark:border-indigo-500/30' :
                                log.type === 'branch' ? 'bg-cyan-500/10 text-cyan-600 border-cyan-500/20 dark:bg-cyan-500/20 dark:text-cyan-300 dark:border-cyan-500/30' :
                                log.type === 'evaluation' ? 'bg-violet-500/10 text-violet-600 border-violet-500/20 dark:bg-violet-500/20 dark:text-violet-300 dark:border-violet-500/30' :
                                'bg-slate-100 text-slate-700 border-slate-200 dark:bg-slate-800 dark:text-slate-300 dark:border-slate-700/50'
                              }`}>
                                {log.type}
                              </span>
                              <span className="text-[10px] text-muted-foreground ml-auto font-mono">
                                {new Date(log.timestamp).toLocaleTimeString()}
                              </span>
                            </div>

                            {(log.type === 'routing' || log.type === 'branch') && log.routing_evaluation && (
                              <div className="mt-3 space-y-3">
                                <div className={`rounded border p-3 ${log.type === 'branch' ? 'border-cyan-500/20 bg-cyan-500/[0.05]' : 'border-indigo-500/20 bg-indigo-500/[0.05]'}`}>
                                  <div className="flex flex-wrap items-center justify-between gap-2">
                                    <span className="text-xs font-medium text-foreground">Selected route</span>
                                    <span className={`rounded border bg-background px-1.5 py-0.5 font-mono text-[10px] ${log.type === 'branch' ? 'border-cyan-500/20 text-cyan-700 dark:text-cyan-300' : 'border-indigo-500/20 text-indigo-700 dark:text-indigo-300'}`}>
                                      {log.routing_evaluation.selected_route_id || 'not recorded'}
                                    </span>
                                  </div>
                                  {log.routing_evaluation.route_next_steps?.[log.routing_evaluation.selected_route_id] && (
                                    <p className="mt-2 text-xs text-muted-foreground">
                                      Continues to <span className="font-mono text-foreground">{log.routing_evaluation.route_next_steps[log.routing_evaluation.selected_route_id]}</span>
                                    </p>
                                  )}
                                </div>
                                {log.routing_evaluation.routing_question && (
                                  <div className="text-xs">
                                    <div className="mb-1 font-semibold text-foreground">{log.type === 'branch' ? 'Branch question' : 'Routing question'}</div>
                                    <p className="text-muted-foreground">{log.routing_evaluation.routing_question}</p>
                                  </div>
                                )}
                                {log.routing_evaluation.routing_reasoning && (
                                  <div className="text-xs">
                                    <div className="mb-1 font-semibold text-foreground">Decision reason</div>
                                    <p className="rounded border border-border bg-muted/30 p-2.5 text-muted-foreground">{log.routing_evaluation.routing_reasoning}</p>
                                  </div>
                                )}
                              </div>
                            )}

                            {(log.type === 'routing' || log.type === 'branch') && log.orchestration_response && !log.routing_evaluation && (
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
                                                    <StepContent {...props} stepId={log.orchestration_response.selected_sub_agent_path} stepLogs={logs!.steps[log.orchestration_response.selected_sub_agent_path]} />
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
                                            <CheckCircle className="w-3 h-3 text-emerald-600 dark:text-emerald-400" />
                                            Sub-Agent Success Criteria
                                        </div>
                                        <p className="text-emerald-700 dark:text-emerald-300 bg-emerald-500/[0.04] p-2.5 rounded border border-emerald-500/15 italic">
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
                                    ? 'bg-emerald-500/10 border-emerald-500/20 text-emerald-800 dark:bg-emerald-950/20 dark:border-emerald-900/30 dark:text-emerald-300' 
                                    : 'bg-rose-500/10 border-rose-500/20 text-rose-800 dark:bg-rose-950/20 dark:border-rose-900/30 dark:text-rose-300'
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
          {visibleTodoTask.length > 0 && (
            <div className="p-4 bg-muted/30 border-t border-border">
              <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-3 flex items-center gap-2">
                <ListTodo className="w-4 h-4" /> Todo Task Logs
              </h4>
              <div className="space-y-6">
                {Object.entries(
                  // eslint-disable-next-line @typescript-eslint/no-explicit-any
                  visibleTodoTask.reduce((acc: Record<number, any[]>, log: any) => {
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
                            log.type === 'routing' ? 'bg-indigo-500' :
                            log.type === 'evaluation' ? (log.all_tasks_complete ? 'bg-emerald-500' : 'bg-amber-500') :
                            'bg-slate-400 dark:bg-slate-500'
                          }`} />

                          <div className="bg-background rounded border border-border p-3 text-sm shadow-sm">
                            <div className="flex items-center gap-2 mb-2">
                              <span className={`font-mono text-[10px] px-1.5 py-0.5 rounded uppercase font-bold tracking-wide border ${
                                log.type === 'routing' ? 'bg-indigo-500/10 text-indigo-600 border-indigo-500/20 dark:bg-indigo-500/20 dark:text-indigo-300 dark:border-indigo-500/30' :
                                log.type === 'evaluation' ? 'bg-amber-500/10 text-amber-600 border-amber-500/20 dark:bg-amber-500/20 dark:text-amber-300 dark:border-amber-500/30' :
                                'bg-slate-100 text-slate-700 border-slate-200 dark:bg-slate-800 dark:text-slate-300 dark:border-slate-700/50'
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
                                  <span className={`px-2 py-1 rounded text-xs font-medium border ${
                                    log.todo_task_response.next_action === 'complete'
                                      ? 'bg-emerald-500/10 text-emerald-600 border-emerald-500/20 dark:bg-emerald-500/20 dark:text-emerald-300 dark:border-emerald-500/30'
                                      : log.todo_task_response.next_action === 'delegate'
                                      ? 'bg-indigo-500/10 text-indigo-600 border-indigo-500/20 dark:bg-indigo-500/20 dark:text-indigo-300 dark:border-indigo-500/30'
                                      : 'bg-slate-100 text-slate-700 border-slate-200 dark:bg-slate-800 dark:text-slate-300 dark:border-slate-700/50'
                                  }`}>
                                    Action: {log.todo_task_response.next_action}
                                  </span>
                                  {log.todo_task_response.all_tasks_complete && (
                                    <span className="flex items-center gap-1 text-xs text-emerald-600 dark:text-emerald-400">
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
                                      <CheckCircle className="w-3 h-3 text-emerald-600 dark:text-emerald-400" />
                                      Sub-Agent Success Criteria
                                    </div>
                                    <p className="text-emerald-700 dark:text-emerald-300 bg-emerald-500/[0.04] p-2.5 rounded border border-emerald-500/15 italic">
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
                                      <StepContent {...props} stepId={log.todo_task_response.selected_sub_agent_path} stepLogs={logs!.steps[log.todo_task_response.selected_sub_agent_path]} />
                                    </div>
                                  </details>
                                )}
                              </div>
                            )}

                            {log.type === 'evaluation' && (
                              <div className="mt-2">
                                <div className={`flex items-center gap-2 p-2 rounded border ${
                                  log.all_tasks_complete
                                    ? 'bg-emerald-500/10 border-emerald-500/20 text-emerald-800 dark:bg-emerald-950/20 dark:border-emerald-900/30 dark:text-emerald-300'
                                    : 'bg-amber-500/10 border-amber-200 text-amber-800 dark:bg-amber-950/20 dark:border-amber-900/30 dark:text-amber-300'
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
          {/* Archived Logs Section (Previous Runs) */}
          {visibleArchivedLogs.length > 0 && (
            <div className="p-4 bg-amber-500/5 border-t border-amber-500/20">
              <h4 className="text-xs font-semibold text-amber-600 dark:text-amber-400 uppercase tracking-wider mb-3 flex items-center gap-2">
                <History className="w-4 h-4" /> Previous Runs ({visibleArchivedLogs.length})
              </h4>
              <div className="space-y-3">
                {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
                {visibleArchivedLogs.map((archive: any, archiveIdx: number) => {
                  const archiveId = `${stepId}-archive-${archiveIdx}`
                  const isArchiveExpanded = expandedArchived.has(archiveId)
                  const totalLogs = (archive.validations?.length || 0) + (archive.executions?.length || 0) +
                                   (archive.learnings?.length || 0) + (archive.orchestration?.length || 0)

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
                                                                                <div className={`w-2 h-2 rounded-full ${valStatus === 'COMPLETED' ? 'bg-emerald-500' : valStatus === 'FAILED' ? 'bg-rose-500' : 'bg-slate-400 dark:bg-slate-500'}`} />
                                                                                <span className="font-medium">Attempt {val.attempt}</span>
                                                                                <span className={`ml-auto text-xs ${valStatus === 'COMPLETED' ? 'text-emerald-600 dark:text-emerald-400' : valStatus === 'FAILED' ? 'text-rose-600 dark:text-rose-400' : 'text-muted-foreground'}`}>
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

                        </div>
                      )}
                    </div>
                  )
                })}
              </div>
            </div>
          )}

          {/* Archived execution outputs from deterministic routing. */}
          {visibleArchivedRuns.length > 0 && (
            <div className="p-4 bg-indigo-500/[0.03] border-t border-indigo-500/15">
              <h4 className="text-xs font-semibold text-indigo-600 dark:text-indigo-400 uppercase tracking-wider mb-3 flex items-center gap-2">
                <Archive className="w-4 h-4" /> Archived Execution Runs ({visibleArchivedRuns.length})
              </h4>
              <div className="space-y-3">
                {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
                {visibleArchivedRuns.map((archive: any, archiveIdx: number) => {
                  const archiveId = `${stepId}-archived-exec-${archiveIdx}`
                  const isArchiveExpanded = expandedArchived.has(archiveId)
                  const hasOutput = !!archive.output_content
                  const artifactCount = archive.artifacts?.length || 0

                  return (
                    <div key={archiveIdx} className="bg-background rounded border border-indigo-500/20 dark:border-indigo-500/30 overflow-hidden">
                      <button
                        onClick={() => toggleArchived(archiveId)}
                        aria-expanded={isArchiveExpanded}
                        aria-label={`${isArchiveExpanded ? 'Collapse' : 'Expand'} archived run ${archive.run_number}`}
                        className="w-full flex items-center gap-3 p-3 text-left hover:bg-indigo-500/10 transition-colors"
                      >
                        {isArchiveExpanded ? <ChevronDown className="w-4 h-4 text-indigo-500" /> : <ChevronRight className="w-4 h-4 text-indigo-500" />}
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
                        <div className="border-t border-indigo-500/15 p-3 space-y-3 bg-muted/20">
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
