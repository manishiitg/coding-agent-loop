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
  BarChart3,
  Target,
  TrendingUp,
  TrendingDown,
  Award,
  Filter,
  RefreshCw
} from 'lucide-react'
import { agentApi } from '../../services/api'
import type { EvaluationReportsResponse } from '../../services/api-types'

interface EvaluationPopupProps {
  isOpen: boolean
  onClose: () => void
  workspacePath: string | null
  selectedRunFolder: string | null // Currently selected run folder in the UI
}

// Format percentage for display
const formatPercentage = (value: number) => {
  return `${value.toFixed(1)}%`
}

// Get score color based on percentage
const getScoreColor = (percentage: number) => {
  if (percentage >= 80) return 'text-green-600 dark:text-green-400'
  if (percentage >= 50) return 'text-yellow-600 dark:text-yellow-400'
  return 'text-red-600 dark:text-red-400'
}

// Get score background color
const getScoreBgColor = (percentage: number) => {
  if (percentage >= 80) return 'bg-green-100 dark:bg-green-900/30'
  if (percentage >= 50) return 'bg-yellow-100 dark:bg-yellow-900/30'
  return 'bg-red-100 dark:bg-red-900/30'
}

// Get progress bar color
const getProgressBarColor = (percentage: number) => {
  if (percentage >= 80) return 'bg-green-500'
  if (percentage >= 50) return 'bg-yellow-500'
  return 'bg-red-500'
}

const EvaluationPopup: React.FC<EvaluationPopupProps> = ({
  isOpen,
  onClose,
  workspacePath,
  selectedRunFolder
}) => {
  const [loading, setLoading] = useState(false)
  const [data, setData] = useState<EvaluationReportsResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [expandedReports, setExpandedReports] = useState<Set<string>>(new Set())
  const [expandedSteps, setExpandedSteps] = useState<Set<string>>(new Set())
  const [filterRunFolder, setFilterRunFolder] = useState<string>('')
  const [viewMode, setViewMode] = useState<'all' | 'single'>(selectedRunFolder ? 'single' : 'all')

  // Load evaluation reports when popup opens
  useEffect(() => {
    if (isOpen && workspacePath) {
      // If we have a selected run folder, default to single mode
      if (selectedRunFolder) {
        setViewMode('single')
        setFilterRunFolder(selectedRunFolder)
      } else {
        setViewMode('all')
        setFilterRunFolder('')
      }
      loadReports()
    } else {
      setData(null)
      setError(null)
      setExpandedReports(new Set())
      setExpandedSteps(new Set())
    }
  }, [isOpen, workspacePath, selectedRunFolder])

  // Update filter when view mode changes
  useEffect(() => {
    if (viewMode === 'single' && selectedRunFolder) {
      setFilterRunFolder(selectedRunFolder)
    } else if (viewMode === 'all') {
      setFilterRunFolder('')
    }
  }, [viewMode, selectedRunFolder])

  // Reload when filter changes
  useEffect(() => {
    if (isOpen && workspacePath) {
      loadReports()
    }
  }, [filterRunFolder])

  const loadReports = async () => {
    if (!workspacePath) return

    setLoading(true)
    setError(null)
    try {
      const response = await agentApi.getEvaluationReports(workspacePath, filterRunFolder)
      setData(response)

      // Auto-expand the first report if there's only one
      if (response.reports?.length === 1) {
        setExpandedReports(new Set([response.reports[0].run_folder]))
      }
    } catch (err) {
      console.error('Failed to load evaluation reports:', err)
      setError('Failed to load evaluation reports')
    } finally {
      setLoading(false)
    }
  }

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

  // Get unique run folders that have evaluation reports
  const availableRunFolders = useMemo(() => {
    if (!data?.reports) return []
    return [...new Set(data.reports.map(r => r.run_folder))].sort()
  }, [data?.reports])

  if (!isOpen) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm p-4">
      <div className="bg-background rounded-lg shadow-xl w-full max-w-5xl max-h-[90vh] flex flex-col border border-border relative">
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-border">
          <div className="flex-1 min-w-0">
            <h2 className="text-lg font-semibold text-foreground flex items-center gap-2">
              <BarChart3 className="w-5 h-5 text-primary" />
              Evaluation Reports
            </h2>
            <div className="flex items-center gap-4 mt-1">
              {/* View Mode Toggle */}
              <div className="flex items-center gap-2 text-sm">
                <button
                  onClick={() => setViewMode('all')}
                  className={`px-2.5 py-1 rounded-md transition-colors ${
                    viewMode === 'all'
                      ? 'bg-primary text-primary-foreground'
                      : 'bg-muted hover:bg-muted/80 text-muted-foreground'
                  }`}
                >
                  All Iterations
                </button>
                <button
                  onClick={() => setViewMode('single')}
                  className={`px-2.5 py-1 rounded-md transition-colors ${
                    viewMode === 'single'
                      ? 'bg-primary text-primary-foreground'
                      : 'bg-muted hover:bg-muted/80 text-muted-foreground'
                  }`}
                >
                  Single Iteration
                </button>
              </div>

              {/* Run Folder Filter - only show in single mode */}
              {viewMode === 'single' && availableRunFolders.length > 0 && (
                <div className="flex items-center gap-2">
                  <Filter className="w-4 h-4 text-muted-foreground" />
                  <select
                    value={filterRunFolder}
                    onChange={(e) => setFilterRunFolder(e.target.value)}
                    className="text-xs bg-muted border border-border rounded-md px-2 py-1 text-foreground"
                  >
                    <option value="">Select iteration...</option>
                    {availableRunFolders.map(folder => (
                      <option key={folder} value={folder}>{folder}</option>
                    ))}
                  </select>
                </div>
              )}

              {/* Refresh Button */}
              <button
                onClick={loadReports}
                disabled={loading}
                className="p-1.5 rounded-md hover:bg-muted transition-colors text-muted-foreground hover:text-foreground"
                title="Refresh"
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
          ) : !data || !data.reports || data.reports.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
              <FileText className="w-12 h-12 mb-3 opacity-50" />
              <p>No evaluation reports found.</p>
              <p className="text-sm mt-2">Run evaluation on workflow iterations to see results here.</p>
            </div>
          ) : (
            <div className="space-y-6">
              {/* Aggregate Summary - only show in "all" mode */}
              {viewMode === 'all' && data.aggregate && (
                <div className="bg-card border border-border rounded-lg p-4 shadow-sm">
                  <h3 className="text-sm font-semibold text-foreground mb-4 flex items-center gap-2">
                    <Award className="w-4 h-4 text-primary" />
                    Aggregate Summary ({data.aggregate.total_runs} runs)
                  </h3>
                  <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                    {/* Average Score */}
                    <div className={`rounded-lg p-3 ${getScoreBgColor(data.aggregate.average_percentage)}`}>
                      <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1">Average Score</div>
                      <div className={`text-2xl font-bold ${getScoreColor(data.aggregate.average_percentage)}`}>
                        {formatPercentage(data.aggregate.average_percentage)}
                      </div>
                      <div className="text-xs text-muted-foreground mt-1">
                        {data.aggregate.average_score.toFixed(1)} / {data.aggregate.max_possible_score}
                      </div>
                    </div>

                    {/* Highest Score */}
                    <div className="bg-green-100 dark:bg-green-900/30 rounded-lg p-3">
                      <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1 flex items-center gap-1">
                        <TrendingUp className="w-3 h-3" />
                        Highest
                      </div>
                      <div className="text-2xl font-bold text-green-600 dark:text-green-400">
                        {data.aggregate.highest_score} / {data.aggregate.max_possible_score}
                      </div>
                    </div>

                    {/* Lowest Score */}
                    <div className="bg-red-100 dark:bg-red-900/30 rounded-lg p-3">
                      <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1 flex items-center gap-1">
                        <TrendingDown className="w-3 h-3" />
                        Lowest
                      </div>
                      <div className="text-2xl font-bold text-red-600 dark:text-red-400">
                        {data.aggregate.lowest_score} / {data.aggregate.max_possible_score}
                      </div>
                    </div>

                    {/* Total Runs */}
                    <div className="bg-muted rounded-lg p-3">
                      <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1 flex items-center gap-1">
                        <Target className="w-3 h-3" />
                        Total Runs
                      </div>
                      <div className="text-2xl font-bold text-foreground">
                        {data.aggregate.total_runs}
                      </div>
                    </div>
                  </div>
                </div>
              )}

              {/* Individual Reports */}
              <div className="space-y-3">
                {data.reports.map((entry) => {
                  const isExpanded = expandedReports.has(entry.run_folder)
                  const report = entry.report
                  const scorePercentage = report.score_percentage

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
                                {new Date(report.generated_at).toLocaleString()}
                              </span>
                            </div>
                          </div>
                        </div>

                        {/* Score Badge */}
                        <div className="flex items-center gap-3 flex-shrink-0 ml-4">
                          <div className={`flex items-center gap-2 px-3 py-1.5 rounded-full ${getScoreBgColor(scorePercentage)}`}>
                            {scorePercentage >= 80 ? (
                              <CheckCircle className={`w-4 h-4 ${getScoreColor(scorePercentage)}`} />
                            ) : scorePercentage >= 50 ? (
                              <AlertCircle className={`w-4 h-4 ${getScoreColor(scorePercentage)}`} />
                            ) : (
                              <XCircle className={`w-4 h-4 ${getScoreColor(scorePercentage)}`} />
                            )}
                            <span className={`text-sm font-semibold ${getScoreColor(scorePercentage)}`}>
                              {report.total_score} / {report.max_possible_score}
                            </span>
                            <span className={`text-xs ${getScoreColor(scorePercentage)}`}>
                              ({formatPercentage(scorePercentage)})
                            </span>
                          </div>
                        </div>
                      </button>

                      {/* Expanded Content */}
                      {isExpanded && (
                        <div className="border-t border-border">
                          {/* Summary */}
                          {report.summary && (
                            <div className="px-4 py-3 bg-muted/30 border-b border-border">
                              <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-2">
                                Summary
                              </h4>
                              <p className="text-sm text-foreground whitespace-pre-wrap">
                                {report.summary}
                              </p>
                            </div>
                          )}

                          {/* Step Scores */}
                          <div className="p-4">
                            <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-3">
                              Step Scores ({report.step_scores.length} steps)
                            </h4>
                            <div className="space-y-2">
                              {report.step_scores.map((step, idx) => {
                                const stepKey = `${entry.run_folder}-${step.step_id}`
                                const isStepExpanded = expandedSteps.has(stepKey)
                                const stepPercentage = (step.score / step.max_score) * 100

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
                                            {step.step_title || step.step_id}
                                          </span>
                                        </div>

                                        {/* Progress Bar */}
                                        <div className="flex items-center gap-2">
                                          <div className="flex-1 bg-muted rounded-full h-2 overflow-hidden">
                                            <div
                                              className={`h-full transition-all ${getProgressBarColor(stepPercentage)}`}
                                              style={{ width: `${stepPercentage}%` }}
                                            />
                                          </div>
                                          <span className={`text-xs font-medium ${getScoreColor(stepPercentage)}`}>
                                            {step.score}/{step.max_score}
                                          </span>
                                        </div>
                                      </div>
                                    </button>

                                    {/* Step Details */}
                                    {isStepExpanded && (
                                      <div className="px-4 py-3 border-t border-border bg-muted/20 space-y-3">
                                        {/* Reasoning */}
                                        {step.reasoning && (
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
                                        {step.evidence && (
                                          <div>
                                            <h5 className="text-xs font-semibold text-muted-foreground mb-1">
                                              Evidence
                                            </h5>
                                            <pre className="text-xs bg-background border border-border rounded p-2 overflow-x-auto whitespace-pre-wrap max-h-40 overflow-y-auto">
                                              {step.evidence}
                                            </pre>
                                          </div>
                                        )}

                                        {/* Success Criteria */}
                                        {step.success_criteria && (
                                          <div>
                                            <h5 className="text-xs font-semibold text-muted-foreground mb-1">
                                              Success Criteria
                                            </h5>
                                            <pre className="text-xs bg-background border border-border rounded p-2 overflow-x-auto whitespace-pre-wrap max-h-40 overflow-y-auto">
                                              {step.success_criteria}
                                            </pre>
                                          </div>
                                        )}
                                      </div>
                                    )}
                                  </div>
                                )
                              })}
                            </div>
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

export default EvaluationPopup
