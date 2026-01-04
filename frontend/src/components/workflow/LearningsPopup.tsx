import { useEffect, useState, useCallback } from 'react'
import { X, BookOpen, Lock, Unlock, Loader2, AlertCircle, ChevronDown, ChevronRight } from 'lucide-react'
import { agentApi } from '../../services/api'
import type { PlanningResponse, PlanStep } from '../../utils/stepConfigMatching'
import { isConditionalStep, isDecisionStep, isOrchestrationStep } from '../../utils/stepConfigMatching'
import { MarkdownRenderer } from '../ui/MarkdownRenderer'

interface LearningsPopupProps {
  isOpen: boolean
  onClose: () => void
  workspacePath: string | null
  plan: PlanningResponse | null
}

interface LearningMetadata {
  step_id?: string
  successful_runs_simple?: number
  successful_runs_medium?: number
  successful_runs_complex?: number
  last_turn_count?: number
  auto_locked_at?: string
  auto_lock_reason?: string
}

// Determine complexity based on last_turn_count and successful runs counters
function getComplexity(metadata: LearningMetadata | null): 'simple' | 'medium' | 'complex' | 'unknown' {
  if (!metadata) return 'unknown'
  
  // First, try to determine from last_turn_count
  const turnCount = metadata.last_turn_count
  if (turnCount !== undefined && turnCount > 0) {
    if (turnCount < 15) return 'simple'
    if (turnCount <= 30) return 'medium'
    return 'complex'
  }
  
  // Fallback: infer from successful runs counters
  // If any counter has a value > 0, use that to determine complexity
  if ((metadata.successful_runs_simple || 0) > 0) return 'simple'
  if ((metadata.successful_runs_medium || 0) > 0) return 'medium'
  if ((metadata.successful_runs_complex || 0) > 0) return 'complex'
  
  return 'unknown'
}

// Get lock threshold based on complexity
function getLockThreshold(complexity: 'simple' | 'medium' | 'complex' | 'unknown'): number {
  switch (complexity) {
    case 'simple': return 3
    case 'medium': return 5
    case 'complex': return 10
    case 'unknown': return 0
    default: return 0
  }
}

// Get current successful runs count based on complexity
function getSuccessfulRuns(metadata: LearningMetadata | null, complexity: 'simple' | 'medium' | 'complex' | 'unknown'): number {
  if (!metadata) return 0
  switch (complexity) {
    case 'simple': return metadata.successful_runs_simple || 0
    case 'medium': return metadata.successful_runs_medium || 0
    case 'complex': return metadata.successful_runs_complex || 0
    case 'unknown': return 0
    default: return 0
  }
}

// Get step title from plan
function getStepTitle(plan: PlanningResponse | null, stepId: string): string {
  if (!plan?.steps) return stepId
  
  const findStep = (steps: PlanStep[], id: string): PlanStep | null => {
    for (const step of steps) {
      if (step.id === id) return step
      // Check branch steps for conditional steps
      if ('if_true_steps' in step && step.if_true_steps) {
        const found = findStep(step.if_true_steps, id)
        if (found) return found
      }
      if ('if_false_steps' in step && step.if_false_steps) {
        const found = findStep(step.if_false_steps, id)
        if (found) return found
      }
      // Check decision step
      if ('decision_step' in step && step.decision_step && step.decision_step.id === id) {
        return step.decision_step
      }
      // Check orchestration step
      if ('orchestration_step' in step && step.orchestration_step && step.orchestration_step.id === id) {
        return step.orchestration_step
      }
    }
    return null
  }
  
  const step = findStep(plan.steps, stepId)
  return step?.title || stepId
}

export default function LearningsPopup({ isOpen, onClose, workspacePath, plan }: LearningsPopupProps) {
  const [learnings, setLearnings] = useState<Record<string, LearningMetadata | null>>({})
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  
  // Expanded items state - tracks which step IDs have their learning content expanded
  const [expandedStepIds, setExpandedStepIds] = useState<Set<string>>(new Set())
  
  // Learning content cache - stores fetched markdown content for each step
  const [learningContentCache, setLearningContentCache] = useState<Record<string, { content: string; error: string | null }>>({})
  
  // Loading states for individual items
  const [loadingStepIds, setLoadingStepIds] = useState<Set<string>>(new Set())
  
  // Step configs state - stores lock_learnings status from step_config.json
  const [stepConfigs, setStepConfigs] = useState<Record<string, { lock_learnings?: boolean }>>({})
  const [updatingLockStepIds, setUpdatingLockStepIds] = useState<Set<string>>(new Set())

  // Fetch step configs to get lock_learnings status
  const fetchStepConfigs = useCallback(async () => {
    if (!workspacePath) return

    try {
      const stepConfigPath = `${workspacePath}/planning/step_config.json`
      const response = await agentApi.getPlannerFileContent(stepConfigPath)
      
      if (response.success && response.data?.content) {
        const rawContent = JSON.parse(response.data.content)
        // Handle both object format { "steps": [...] } and array format
        const configs = rawContent.steps || rawContent || []
        
        const configMap: Record<string, { lock_learnings?: boolean }> = {}
        if (Array.isArray(configs)) {
          configs.forEach((config: any) => {
            if (config.id && config.agent_configs) {
              configMap[config.id] = {
                lock_learnings: config.agent_configs.lock_learnings
              }
            }
          })
        }
        setStepConfigs(configMap)
      }
    } catch (err) {
      // Step config might not exist, that's okay
      console.debug('[LearningsPopup] Could not load step configs:', err)
    }
  }, [workspacePath])

  // Fetch learnings when popup opens
  useEffect(() => {
    if (!isOpen || !workspacePath) return

    setIsLoading(true)
    setError(null)
    
    // Fetch both learnings and step configs
    Promise.all([
      agentApi.getAllStepLearnings(workspacePath),
      fetchStepConfigs()
    ])
      .then(([learningsResponse]) => {
        if (learningsResponse.success) {
          // Type cast the learnings to match our interface
          const learningsData = learningsResponse.learnings || {}
          const typedLearnings: Record<string, LearningMetadata | null> = {}
          for (const [stepId, metadata] of Object.entries(learningsData)) {
            typedLearnings[stepId] = metadata as LearningMetadata | null
          }
          setLearnings(typedLearnings)
        } else {
          setError('Failed to load learnings')
        }
      })
      .catch((err: Error) => {
        console.error('[LearningsPopup] Error fetching learnings:', err)
        setError('Failed to load learnings: ' + (err.message || 'Unknown error'))
      })
      .finally(() => {
        setIsLoading(false)
      })
  }, [isOpen, workspacePath, fetchStepConfigs])

  // Toggle lock/unlock for a step
  const toggleLock = async (stepId: string, isCurrentlyLocked: boolean) => {
    if (!workspacePath || updatingLockStepIds.has(stepId)) return

    setUpdatingLockStepIds(prev => new Set(prev).add(stepId))

    try {
      // Get current agent configs from plan or step configs
      const step = plan?.steps?.find(s => s.id === stepId)
      const stepConfig = stepConfigs[stepId]
      const currentConfigs = step?.agent_configs || (stepConfig ? { lock_learnings: stepConfig.lock_learnings } : {})
      
      // Determine the new lock state
      // If currently locked (auto or manual), unlock it by setting lock_learnings = false
      // If currently unlocked, lock it by setting lock_learnings = true
      const newLockState = !isCurrentlyLocked
      
      // Update lock_learnings
      const updatedConfigs = {
        ...currentConfigs,
        lock_learnings: newLockState
      }

      await agentApi.updateStepConfig(workspacePath, stepId, updatedConfigs)
      
      // Refresh both step configs and learnings
      await fetchStepConfigs()
      
      const response = await agentApi.getAllStepLearnings(workspacePath)
      if (response.success) {
        const learningsData = response.learnings || {}
        const typedLearnings: Record<string, LearningMetadata | null> = {}
        for (const [id, metadata] of Object.entries(learningsData)) {
          typedLearnings[id] = metadata as LearningMetadata | null
        }
        setLearnings(typedLearnings)
      }
    } catch (err: any) {
      console.error('[LearningsPopup] Error toggling lock:', err)
      setError('Failed to update lock status: ' + (err.message || 'Unknown error'))
    } finally {
      setUpdatingLockStepIds(prev => {
        const newSet = new Set(prev)
        newSet.delete(stepId)
        return newSet
      })
    }
  }

  // Handle Escape key to close modal
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && isOpen) {
        onClose()
      }
    }

    if (isOpen) {
      document.addEventListener('keydown', handleKeyDown)
    }

    return () => {
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [isOpen, onClose])

  // Fetch learning content when an item is expanded
  const fetchLearningContent = async (stepId: string) => {
    if (!workspacePath || learningContentCache[stepId]) {
      // Already cached or no workspace path
      return
    }

    setLoadingStepIds(prev => new Set(prev).add(stepId))

    try {
      // First, list files in the learnings folder to find the markdown file
      const learningsPath = `${workspacePath}/learnings/${stepId}`
      const filesResponse = await agentApi.getPlannerFiles(learningsPath, 100)
      
      // Handle both direct array response and wrapped response
      const files = Array.isArray(filesResponse) 
        ? filesResponse 
        : (filesResponse?.data && Array.isArray(filesResponse.data) ? filesResponse.data : [])

      // Find the first .md file (excluding metadata files)
      const mdFile = files.find((file: any) => {
        const fileName = file.filepath || file.name || ''
        return fileName.endsWith('.md') && !fileName.includes('.learning_metadata')
      })

      if (!mdFile) {
        setLearningContentCache(prev => ({
          ...prev,
          [stepId]: { content: '', error: 'No learning markdown file found' }
        }))
        return
      }

      // Construct the full file path
      let filePath = mdFile.filepath || mdFile.name
      if (!filePath) {
        setLearningContentCache(prev => ({
          ...prev,
          [stepId]: { content: '', error: 'Invalid file path' }
        }))
        return
      }

      // If path doesn't start with workspace path, construct it
      if (!filePath.startsWith(workspacePath)) {
        const cleanPath = filePath.startsWith('/') ? filePath.slice(1) : filePath
        filePath = `${workspacePath}/${cleanPath}`
      }
      
      // Read the file content
      const response = await agentApi.getPlannerFileContent(filePath)
      
      if (response.success && response.data && response.data.content) {
        setLearningContentCache(prev => ({
          ...prev,
          [stepId]: { content: response.data.content, error: null }
        }))
      } else {
        setLearningContentCache(prev => ({
          ...prev,
          [stepId]: { content: '', error: 'Failed to read learning file: ' + (response.message || 'Unknown error') }
        }))
      }
    } catch (err: any) {
      console.error('[LearningsPopup] Error fetching learning content:', err)
      setLearningContentCache(prev => ({
        ...prev,
        [stepId]: { content: '', error: 'Failed to load learning content: ' + (err.message || 'Unknown error') }
      }))
    } finally {
      setLoadingStepIds(prev => {
        const newSet = new Set(prev)
        newSet.delete(stepId)
        return newSet
      })
    }
  }

  // Toggle expand/collapse for a step
  const toggleExpand = (stepId: string) => {
    setExpandedStepIds(prev => {
      const newSet = new Set(prev)
      if (newSet.has(stepId)) {
        newSet.delete(stepId)
      } else {
        newSet.add(stepId)
        // Fetch content if not cached
        if (!learningContentCache[stepId]) {
          fetchLearningContent(stepId)
        }
      }
      return newSet
    })
  }

  // Collect all step IDs in execution order from plan with metadata
  const getStepsInExecutionOrder = useCallback((): Array<{ stepId: string; stepNumber: number; stepType: string; branchType?: string }> => {
    if (!plan || !plan.steps) return []

    const stepsWithMetadata: Array<{ stepId: string; stepNumber: number; stepType: string; branchType?: string }> = []
    let stepCounter = 0

    const collectSteps = (steps: PlanStep[], branchType?: string) => {
      steps.forEach((step) => {
        if (step.id) {
          stepCounter++
          const stepType = step.type || 'regular'
          stepsWithMetadata.push({
            stepId: step.id,
            stepNumber: stepCounter,
            stepType,
            branchType
          })
        }

        // Handle conditional steps - collect branch steps
        if (isConditionalStep(step)) {
          if (step.if_true_steps && step.if_true_steps.length > 0) {
            collectSteps(step.if_true_steps, 'true')
          }
          if (step.if_false_steps && step.if_false_steps.length > 0) {
            collectSteps(step.if_false_steps, 'false')
          }
        }

        // Handle decision steps - collect decision step ID
        if (isDecisionStep(step) && step.decision_step) {
          if (step.decision_step.id) {
            stepCounter++
            stepsWithMetadata.push({
              stepId: step.decision_step.id,
              stepNumber: stepCounter,
              stepType: 'decision_inner',
              branchType
            })
          }
        }

        // Handle orchestration steps - collect orchestration step ID and sub-agent IDs
        if (isOrchestrationStep(step)) {
          if (step.orchestration_step && step.orchestration_step.id) {
            stepCounter++
            stepsWithMetadata.push({
              stepId: step.orchestration_step.id,
              stepNumber: stepCounter,
              stepType: 'orchestration_inner',
              branchType
            })
          }
          // Collect sub-agent step IDs from routes
          if (step.orchestration_routes) {
            step.orchestration_routes.forEach((route, routeIdx) => {
              if (route.sub_agent_step && route.sub_agent_step.id) {
                stepCounter++
                stepsWithMetadata.push({
                  stepId: route.sub_agent_step.id,
                  stepNumber: stepCounter,
                  stepType: 'sub_agent',
                  branchType: `sub-agent-${routeIdx}`
                })
              }
            })
          }
        }
      })
    }

    collectSteps(plan.steps)
    return stepsWithMetadata
  }, [plan])

  if (!isOpen) return null

  // Get steps in execution order and filter to only those with learnings
  const allStepsInOrder = getStepsInExecutionOrder()
  const stepsWithLearnings = allStepsInOrder.filter(step => step.stepId in learnings)

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4" style={{ zIndex: 50 }}>
      <div className="bg-background border border-border rounded-lg shadow-xl w-full max-w-4xl max-h-[90vh] flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between p-4 border-b border-border flex-shrink-0">
          <div className="flex items-center gap-2">
            <BookOpen className="w-5 h-5 text-primary" />
            <h2 className="text-lg font-semibold">Step Learnings</h2>
          </div>
          <button
            onClick={onClose}
            className="p-1 rounded-md hover:bg-muted transition-colors"
            title="Close (Esc)"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-y-auto p-4">
          {isLoading && (
            <div className="flex items-center justify-center py-8">
              <Loader2 className="w-6 h-6 animate-spin text-primary" />
              <span className="ml-2 text-muted-foreground">Loading learnings...</span>
            </div>
          )}

          {error && (
            <div className="flex items-center gap-2 p-4 bg-destructive/10 border border-destructive/20 rounded-md text-destructive">
              <AlertCircle className="w-5 h-5" />
              <span>{error}</span>
            </div>
          )}

          {!isLoading && !error && stepsWithLearnings.length === 0 && (
            <div className="text-center py-8 text-muted-foreground">
              No steps with learnings found in the plan
            </div>
          )}

          {!isLoading && !error && stepsWithLearnings.length > 0 && (
            <div className="space-y-3">
              {stepsWithLearnings.map(({ stepId, stepNumber, stepType, branchType }) => {
                const metadata = learnings[stepId]
                const stepConfig = stepConfigs[stepId]
                // Check lock status from both sources: auto_locked_at (metadata) OR lock_learnings (step_config)
                const isAutoLocked = metadata?.auto_locked_at !== undefined && metadata.auto_locked_at !== ''
                const isManuallyLocked = stepConfig?.lock_learnings === true
                const isLocked = isAutoLocked || isManuallyLocked
                const complexity = getComplexity(metadata)
                const threshold = getLockThreshold(complexity)
                const successfulRuns = getSuccessfulRuns(metadata, complexity)
                const progress = threshold > 0 ? (successfulRuns / threshold) * 100 : 0
                const stepTitle = getStepTitle(plan, stepId)

                const isExpanded = expandedStepIds.has(stepId)
                const isLoadingContent = loadingStepIds.has(stepId)
                const cachedContent = learningContentCache[stepId]
                const isUpdatingLock = updatingLockStepIds.has(stepId)

                // Determine step type label and badge color
                const getStepTypeLabel = () => {
                  if (branchType === 'true') return 'If True'
                  if (branchType === 'false') return 'If False'
                  if (branchType?.startsWith('sub-agent')) return 'Sub-Agent'
                  if (stepType === 'decision_inner') return 'Decision'
                  if (stepType === 'orchestration_inner') return 'Orchestration'
                  return stepType.charAt(0).toUpperCase() + stepType.slice(1)
                }

                const getStepTypeBadgeColor = () => {
                  if (branchType === 'true') return 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400'
                  if (branchType === 'false') return 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400'
                  if (branchType?.startsWith('sub-agent')) return 'bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400'
                  if (stepType === 'decision_inner' || stepType === 'orchestration_inner') return 'bg-indigo-100 text-indigo-700 dark:bg-indigo-900/30 dark:text-indigo-400'
                  return 'bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300'
                }

                return (
                  <div
                    key={stepId}
                    className="border border-border rounded-lg bg-muted/30 hover:bg-muted/50 transition-colors"
                  >
                    <div 
                      className="p-4 cursor-pointer"
                      onClick={() => toggleExpand(stepId)}
                    >
                      <div className="flex items-start justify-between mb-2">
                        <div className="flex-1">
                          <div className="flex items-center gap-2 mb-1 flex-wrap">
                            <button
                              onClick={(e) => {
                                e.stopPropagation()
                                toggleExpand(stepId)
                              }}
                              className="p-0.5 hover:bg-muted rounded transition-colors"
                              title={isExpanded ? "Collapse" : "Expand"}
                            >
                              {isExpanded ? (
                                <ChevronDown className="w-4 h-4 text-muted-foreground" />
                              ) : (
                                <ChevronRight className="w-4 h-4 text-muted-foreground" />
                              )}
                            </button>
                            <span className="text-xs font-mono font-semibold text-primary bg-primary/10 px-1.5 py-0.5 rounded">
                              #{stepNumber}
                            </span>
                            <span className={`text-xs px-1.5 py-0.5 rounded font-medium ${getStepTypeBadgeColor()}`}>
                              {getStepTypeLabel()}
                            </span>
                            <h3 className="font-medium">{stepTitle}</h3>
                            <span className="text-xs text-muted-foreground font-mono">({stepId})</span>
                          </div>
                          <div className="flex items-center gap-4 mt-2 text-sm">
                            <div className="flex items-center gap-2">
                              {isLocked ? (
                                <>
                                  <Lock className="w-4 h-4 text-green-600 dark:text-green-400" />
                                  <span className="text-green-600 dark:text-green-400">
                                    {isAutoLocked && isManuallyLocked ? 'Locked (Auto + Manual)' :
                                     isAutoLocked ? 'Locked (Auto)' :
                                     'Locked (Manual)'}
                                  </span>
                                </>
                              ) : (
                                <>
                                  <Unlock className="w-4 h-4 text-yellow-600 dark:text-yellow-400" />
                                  <span className="text-yellow-600 dark:text-yellow-400">Unlocked</span>
                                </>
                              )}
                            </div>
                            <button
                              onClick={(e) => {
                                e.stopPropagation()
                                // If locked (auto or manual), unlock it. If unlocked, lock it.
                                toggleLock(stepId, isLocked)
                              }}
                              disabled={isUpdatingLock}
                              className={`flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium transition-colors disabled:opacity-50 disabled:cursor-not-allowed ${
                                isLocked
                                  ? 'bg-yellow-100 hover:bg-yellow-200 dark:bg-yellow-900/30 dark:hover:bg-yellow-900/50 text-yellow-700 dark:text-yellow-400'
                                  : 'bg-gray-100 hover:bg-gray-200 dark:bg-gray-800 dark:hover:bg-gray-700 text-gray-700 dark:text-gray-300'
                              }`}
                              title={isLocked ? "Unlock learnings" : "Lock learnings manually"}
                            >
                              {isUpdatingLock ? (
                                <>
                                  <Loader2 className="w-3.5 h-3.5 animate-spin" />
                                  <span>Updating...</span>
                                </>
                              ) : isLocked ? (
                                <>
                                  <Unlock className="w-3.5 h-3.5" />
                                  <span>Unlock</span>
                                </>
                              ) : (
                                <>
                                  <Lock className="w-3.5 h-3.5" />
                                  <span>Lock</span>
                                </>
                              )}
                            </button>
                            {metadata && (
                              <div className="flex items-center gap-1">
                                <span className="text-muted-foreground">Complexity:</span>
                                <span className={`font-medium ${
                                  complexity === 'simple' ? 'text-green-600 dark:text-green-400' :
                                  complexity === 'medium' ? 'text-yellow-600 dark:text-yellow-400' :
                                  complexity === 'complex' ? 'text-orange-600 dark:text-orange-400' :
                                  'text-gray-500 dark:text-gray-400'
                                }`}>
                                  {complexity.charAt(0).toUpperCase() + complexity.slice(1)}
                                </span>
                                {metadata.last_turn_count && metadata.last_turn_count > 0 && (
                                  <span className="text-xs text-muted-foreground">
                                    ({metadata.last_turn_count} turns)
                                  </span>
                                )}
                              </div>
                            )}
                          </div>

                        {metadata && threshold > 0 && (
                          <div className="mt-3">
                            <div className="flex items-center justify-between text-sm mb-1">
                              <span className="text-muted-foreground">
                                Progress to lock: {successfulRuns}/{threshold} successful runs
                              </span>
                              <span className="text-muted-foreground">{Math.round(progress)}%</span>
                            </div>
                            <div className="w-full bg-muted rounded-full h-2 overflow-hidden">
                              <div
                                className={`h-full transition-all ${
                                  isLocked
                                    ? 'bg-green-600 dark:bg-green-400'
                                    : progress >= 50
                                    ? 'bg-yellow-600 dark:bg-yellow-400'
                                    : 'bg-blue-600 dark:bg-blue-400'
                                }`}
                                style={{ width: `${Math.min(progress, 100)}%` }}
                              />
                            </div>
                          </div>
                        )}

                        {!metadata && (
                          <div className="text-sm text-muted-foreground mt-2">
                            No learning metadata available
                          </div>
                        )}
                      </div>
                    </div>
                  </div>

                  {/* Expanded Learning Content */}
                  {isExpanded && (
                    <div className="border-t border-border px-4 py-4 bg-background/50">
                      {isLoadingContent && (
                        <div className="flex items-center justify-center py-4">
                          <Loader2 className="w-5 h-5 animate-spin text-primary" />
                          <span className="ml-2 text-sm text-muted-foreground">Loading learning content...</span>
                        </div>
                      )}

                      {!isLoadingContent && cachedContent?.error && (
                        <div className="flex items-center gap-2 p-3 bg-destructive/10 border border-destructive/20 rounded-md text-destructive text-sm">
                          <AlertCircle className="w-4 h-4" />
                          <span>{cachedContent.error}</span>
                        </div>
                      )}

                      {!isLoadingContent && cachedContent && !cachedContent.error && cachedContent.content && (
                        <div className="prose prose-sm max-w-none dark:prose-invert">
                          <MarkdownRenderer content={cachedContent.content} maxHeight="400px" showScrollbar={true} />
                        </div>
                      )}

                      {!isLoadingContent && cachedContent && !cachedContent.error && !cachedContent.content && (
                        <div className="text-center py-4 text-sm text-muted-foreground">
                          No learning content available
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
    </div>
  )
}
