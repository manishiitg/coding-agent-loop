import React, { useEffect, useState, useMemo, useCallback } from 'react'
import {
  X,
  Loader2,
  ChevronRight,
  ChevronDown,
  AlertCircle,
  FileText,
  BarChart3,
  Target,
  RefreshCw
} from 'lucide-react'
import { agentApi } from '../../services/api'
import type { EvaluationReportEntry, EvaluationReportsResponse } from '../../services/api-types'
import { formatStartedAt } from '../../utils/duration'
import { formatStepOutputContent, isFinalScoringPlaceholderText, parseEvaluationPlanDetails } from '../../utils/evaluationReport'
import ModalPortal from '../ui/ModalPortal'

interface EvaluationPopupProps {
  isOpen: boolean
  onClose: () => void
  workspacePath: string | null
  selectedRunFolder: string | null // Currently selected run folder in the UI
  runFolders: string[]
  startedAt?: string | null
}

const orderEvaluationReports = (reports: EvaluationReportEntry[]): EvaluationReportEntry[] => {
  return [...reports].sort((left, right) => {
    const leftIsCurrent = left.run_folder === 'iteration-0' || left.run_folder.startsWith('iteration-0/')
    const rightIsCurrent = right.run_folder === 'iteration-0' || right.run_folder.startsWith('iteration-0/')
    if (leftIsCurrent !== rightIsCurrent) return leftIsCurrent ? -1 : 1

    const leftTime = left.report?.generated_at || ''
    const rightTime = right.report?.generated_at || ''
    return rightTime.localeCompare(leftTime)
  })
}

interface EvaluationReportsPanelProps {
  workspacePath: string | null
  selectedRunFolder: string | null
  isActive?: boolean
}

export const EvaluationReportsPanel: React.FC<EvaluationReportsPanelProps> = ({
  workspacePath,
  selectedRunFolder,
  isActive = true,
}) => {
  const [loading, setLoading] = useState(false)
  const [data, setData] = useState<EvaluationReportsResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [expandedReports, setExpandedReports] = useState<Set<string>>(new Set())
  const [expandedSteps, setExpandedSteps] = useState<Set<string>>(new Set())
  const loadReports = useCallback(async () => {
    if (!workspacePath) return

    setLoading(true)
    setError(null)
    try {
      const response = await agentApi.getEvaluationReports(workspacePath)
      setData(response)

      const firstReport = orderEvaluationReports(response.reports || [])[0]
      if (firstReport) {
        setExpandedReports(new Set([firstReport.run_folder]))
      }
    } catch (err) {
      console.error('Failed to load evaluation reports:', err)
      setError('Failed to load evaluation reports')
    } finally {
      setLoading(false)
    }
  }, [workspacePath])

  // Load evaluation reports when the panel becomes active.
  useEffect(() => {
    if (isActive && workspacePath) {
      loadReports()
    } else {
      setData(null)
      setError(null)
      setExpandedReports(new Set())
      setExpandedSteps(new Set())
    }
  }, [isActive, workspacePath, loadReports])

  const toggleReport = (runFolder: string) => {
    setExpandedReports(prev => {
      const next = new Set(prev)
      if (next.has(runFolder)) {
        next.delete(runFolder)
      } else {
        next.add(runFolder)
      }
      return next
    })
  }

  const evalStepDetailsById = useMemo(() => {
    return parseEvaluationPlanDetails(data?.evaluation_plan)
  }, [data?.evaluation_plan])

  const toggleStep = (stepKey: string) => {
    setExpandedSteps(prev => {
      const next = new Set(prev)
      if (next.has(stepKey)) {
        next.delete(stepKey)
      } else {
        next.add(stepKey)
      }
      return next
    })
  }

  const orderedReports = useMemo(() => {
    return orderEvaluationReports(data?.reports || [])
  }, [data?.reports])

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-3">
        <div>
          <h3 className="text-sm font-semibold text-foreground">Evaluation reports</h3>
          <p className="mt-1 text-xs text-muted-foreground">
            Step-level evaluation outputs across retained runs.
          </p>
        </div>
        <button
          onClick={loadReports}
          disabled={loading || !workspacePath}
          className="inline-flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:opacity-60"
          title="Refresh"
        >
          <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
        </button>
      </div>

          {loading ? (
                <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
                  <Loader2 className="w-8 h-8 animate-spin mb-3 text-primary" />
                  <p>Loading evaluation reports...</p>
                </div>
              ) : error ? (
                <div className="flex flex-col items-center justify-center py-12 text-destructive">
                  <AlertCircle className="w-12 h-12 mb-3" />
                  <p>{error}</p>
                  <button
                    onClick={loadReports}
                    className="mt-4 px-4 py-2 bg-destructive/10 text-destructive rounded-md hover:bg-destructive/20 transition-colors text-sm font-medium"
                  >
                    Retry
                  </button>
                </div>
              ) : !data || orderedReports.length === 0 ? (
                <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
                  <FileText className="w-12 h-12 mb-3 opacity-50" />
                  <p>No evaluation reports found.</p>
                  <p className="text-sm mt-2">Run evaluation on automation iterations to see results here.</p>
                </div>
              ) : (
            <div className="space-y-6">
              {/* Individual Reports */}
              <div className="space-y-3">
                {orderedReports.map((entry) => {
                  const isExpanded = expandedReports.has(entry.run_folder)
                  const report = entry.report
                  const stepScores = Array.isArray(report?.step_scores) ? report.step_scores : []
                  const generatedAt = report?.generated_at
                    ? new Date(report.generated_at).toLocaleString()
                    : 'Unknown time'

                  return (
                    <div
                      key={entry.run_folder}
                      className={`border rounded-lg overflow-hidden bg-card ${
                        entry.run_folder === selectedRunFolder 
                          ? 'border-purple-500/50 ring-1 ring-purple-500/20' 
                          : 'border-border'
                      }`}
                    >
                      {/* Report Header */}
                      <button
                        onClick={() => toggleReport(entry.run_folder)}
                        className={`w-full flex items-center justify-between px-4 py-3 text-left transition-colors ${
                          isExpanded ? 'bg-accent/50' : 'hover:bg-accent/50'
                        } ${entry.run_folder === selectedRunFolder ? 'bg-purple-50/30 dark:bg-purple-900/10' : ''}`}
                      >
                        <div className="flex items-center gap-3 flex-1 min-w-0">
                          {isExpanded ? (
                            <ChevronDown className="w-4 h-4 text-muted-foreground flex-shrink-0" />
                          ) : (
                            <ChevronRight className="w-4 h-4 text-muted-foreground flex-shrink-0" />
                          )}
                          <div className="flex flex-col items-start min-w-0">
                            <div className="flex items-center gap-2">
                              <span className={`font-mono text-xs px-1.5 py-0.5 rounded ${
                                entry.run_folder === selectedRunFolder 
                                  ? 'bg-purple-100 text-purple-700 dark:bg-purple-900/40 dark:text-purple-300 font-bold' 
                                  : 'bg-muted text-foreground'
                              }`}>
                                {entry.run_folder}
                              </span>
                              {entry.run_folder === selectedRunFolder && (
                                <span className="flex items-center gap-1 text-[10px] font-bold uppercase tracking-wider px-1.5 py-0.5 rounded bg-purple-500 text-white shadow-sm">
                                  <Target className="w-2.5 h-2.5" />
                                  Current
                                </span>
                              )}
                              <span className="text-xs text-muted-foreground">
                                {generatedAt}
                              </span>
                            </div>
                          </div>
                        </div>

                        <div className="flex items-center gap-3 flex-shrink-0 ml-4">
                          <div className="flex items-center gap-2 px-3 py-1.5 rounded-full bg-muted text-muted-foreground">
                            <FileText className="w-4 h-4" />
                            <span className="text-sm font-semibold">
                              {stepScores.length} step{stepScores.length === 1 ? '' : 's'}
                            </span>
                          </div>
                        </div>
                      </button>

                      {/* Expanded Content */}
                      {isExpanded && (
                        <div className="border-t border-border">
                          {/* Evaluation step outputs */}
                          <div className="p-4">
                            <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-3">
                              Evaluation Steps ({stepScores.length})
                            </h4>
                            {stepScores.length === 0 ? (
                              <div className="rounded-lg border border-warning/40 bg-warning/10 px-3 py-2 text-sm text-warning">
                                This evaluation report has no step_scores. It may be from an older or incomplete eval run.
                              </div>
                            ) : (
                            <div className="space-y-2">
                              {stepScores.map((step, idx) => {
                                const stepKey = `${entry.run_folder}-${step.step_id}`
                                const isStepExpanded = expandedSteps.has(stepKey)
                                const outputText = formatStepOutputContent(step.output_content)
                                const showReasoning = Boolean(step.reasoning && !isFinalScoringPlaceholderText(step.reasoning))
                                const showEvidence = Boolean(step.evidence && !isFinalScoringPlaceholderText(step.evidence))
                                const stepDetails = evalStepDetailsById.get(step.step_id)

                                return (
                                  <div
                                    key={stepKey}
                                    className="bg-background border border-border rounded-lg overflow-hidden"
                                  >
                                    <button
                                      onClick={() => toggleStep(stepKey)}
                                      className="w-full flex items-center gap-3 px-3 py-2 text-left hover:bg-accent/50 transition-colors"
                                    >
                                      {isStepExpanded ? (
                                        <ChevronDown className="w-4 h-4 text-muted-foreground flex-shrink-0" />
                                      ) : (
                                        <ChevronRight className="w-4 h-4 text-muted-foreground flex-shrink-0" />
                                      )}

                                      <div className="flex-1 min-w-0">
                                        <div className="flex items-center gap-2 mb-1">
                                          <span className="text-xs font-mono bg-muted px-1 py-0.5 rounded">
                                            #{idx + 1}
                                          </span>
                                          <span className="text-sm font-medium text-foreground truncate">
                                            {stepDetails?.title || step.step_id}
                                          </span>
                                          {stepDetails?.title && (
                                            <span className="text-xs font-mono text-muted-foreground truncate">
                                              {step.step_id}
                                            </span>
                                          )}
                                          {step.skipped && (
                                            <span className="text-xs bg-muted px-1.5 py-0.5 rounded text-muted-foreground">
                                              Skipped
                                            </span>
                                          )}
                                        </div>

                                      </div>
                                    </button>

                                    {/* Step Details */}
                                    {isStepExpanded && (
                                      <div className="px-4 py-3 border-t border-border bg-muted/20 space-y-3">
                                        {/* Eval Step Description */}
                                        {stepDetails?.description && (
                                          <div>
                                            <h5 className="text-xs font-semibold text-muted-foreground mb-1">
                                              Description
                                            </h5>
                                            <p className="text-sm text-foreground whitespace-pre-wrap">
                                              {stepDetails.description}
                                            </p>
                                          </div>
                                        )}

                                        {/* Structured Output */}
                                        {outputText && (
                                          <div>
                                            <div className="flex items-center justify-between gap-2 mb-1">
                                              <h5 className="text-xs font-semibold text-muted-foreground">
                                                Output
                                              </h5>
                                              {step.output_content?.file_path && (
                                                <span className="text-[10px] text-muted-foreground font-mono truncate">
                                                  {step.output_content.file_path}
                                                </span>
                                              )}
                                            </div>
                                            <pre className="text-xs bg-background border border-border rounded p-2 overflow-x-auto whitespace-pre-wrap max-h-72 overflow-y-auto">
                                              {outputText}
                                            </pre>
                                          </div>
                                        )}

                                        {/* Reasoning */}
                                        {showReasoning && (
                                          <div>
                                            <h5 className="text-xs font-semibold text-muted-foreground mb-1">
                                              Reasoning
                                            </h5>
                                            <p className="text-sm text-foreground whitespace-pre-wrap">
                                              {step.reasoning}
                                            </p>
                                          </div>
                                        )}

                                        {/* Evidence */}
                                        {showEvidence && (
                                          <div>
                                            <h5 className="text-xs font-semibold text-muted-foreground mb-1">
                                              Evidence
                                            </h5>
                                            <pre className="text-xs bg-background border border-border rounded p-2 overflow-x-auto whitespace-pre-wrap max-h-40 overflow-y-auto">
                                              {step.evidence}
                                            </pre>
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

const EvaluationPopup: React.FC<EvaluationPopupProps> = ({
  isOpen,
  onClose,
  workspacePath,
  selectedRunFolder,
  startedAt,
}) => {
  if (!isOpen) return null

  return (
    <ModalPortal>
    <div className="fixed inset-0 z-[9999] flex items-center justify-center bg-black/50 backdrop-blur-sm p-2 sm:p-4">
      <div className="bg-background rounded-lg shadow-xl w-full max-w-5xl max-h-[calc(100dvh-1rem)] sm:max-h-[90vh] flex flex-col border border-border relative">
        {/* Header */}
        <div className="flex items-center justify-between gap-3 px-4 py-3 border-b border-border sm:px-6 sm:py-4">
          <div className="flex-1 min-w-0">
            <h2 className="text-lg font-semibold text-foreground flex items-center gap-2">
              <BarChart3 className="w-5 h-5 text-primary" />
              Evaluation
              {startedAt && (
                <span className="text-xs font-normal text-muted-foreground">{formatStartedAt(startedAt)}</span>
              )}
            </h2>
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
          <EvaluationReportsPanel
            workspacePath={workspacePath}
            selectedRunFolder={selectedRunFolder}
            isActive={isOpen}
          />
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
    </ModalPortal>
  )
}

export default EvaluationPopup
