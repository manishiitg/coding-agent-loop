import { useEffect, useState, useCallback } from 'react'
import { X, BookOpen, Lock, Unlock, Loader2, AlertCircle, ChevronDown, ChevronRight, Code, FileText, Trash2 } from 'lucide-react'
import { agentApi } from '../../services/api'
import type { PlanningResponse, PlanStep } from '../../utils/stepConfigMatching'
import { isConditionalStep, isDecisionStep, isOrchestrationStep } from '../../utils/stepConfigMatching'
import { MarkdownRenderer } from '../ui/MarkdownRenderer'
import { useGlobalPresetStore } from '../../stores/useGlobalPresetStore'
import type { PlannerFile } from '../../services/api-types'
import ConfirmationDialog from '../ui/ConfirmationDialog'

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
  total_iterations?: number
  lock_threshold?: number  // Calculated by backend based on last_turn_count
  // Fields from step_config.json (merged by backend API)
  use_code_execution_mode?: boolean
  learning_detail_level?: string
  lock_learnings?: boolean
}

// Determine complexity based on successful runs counters and last_turn_count
// TODO: Turn-based classification is not reliable - turn count varies significantly based on
// the LLM model used and doesn't reflect actual step complexity. We need a better complexity metric.
// PRIORITY: Check successful runs counters FIRST (they reflect actual complexity category where runs were recorded)
// Then fall back to turn count if no successful runs exist
function getComplexity(metadata: LearningMetadata | null): 'simple' | 'medium' | 'complex' | 'unknown' {
  if (!metadata) return 'unknown'
  
  // PRIORITY 1: Infer from successful runs counters (most reliable - reflects actual complexity category)
  // If any counter has a value > 0, use that to determine complexity
  if ((metadata.successful_runs_simple || 0) > 0) return 'simple'
  if ((metadata.successful_runs_medium || 0) > 0) return 'medium'
  if ((metadata.successful_runs_complex || 0) > 0) return 'complex'
  
  // PRIORITY 2: Fallback to last_turn_count (less reliable, but better than nothing)
  const turnCount = metadata.last_turn_count
  if (turnCount !== undefined && turnCount > 0) {
    if (turnCount < 100) return 'simple'
    if (turnCount <= 200) return 'medium'
    return 'complex'
  }
  
  return 'unknown'
}

// Get lock threshold from metadata (calculated by backend - single source of truth)
// Backend calculates threshold based on last_turn_count and includes it in metadata.lock_threshold
function getLockThreshold(metadata: LearningMetadata | null): number {
  return metadata?.lock_threshold ?? 0
}

// Get total successful runs count (sum of all complexity categories)
// The threshold is still based on the determined complexity, but the count is the total across all categories
function getSuccessfulRuns(metadata: LearningMetadata | null): number {
  if (!metadata) return 0
  // Sum all successful runs across all complexity categories
  return (metadata.successful_runs_simple || 0) + 
         (metadata.successful_runs_medium || 0) + 
         (metadata.successful_runs_complex || 0)
}

// Parse learnings API response into typed Record
function parseLearningsResponse(learningsData: Record<string, unknown>): Record<string, LearningMetadata | null> {
  const result: Record<string, LearningMetadata | null> = {}
  for (const [stepId, metadata] of Object.entries(learningsData)) {
    result[stepId] = metadata as LearningMetadata | null
  }
  return result
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
  
  // Learning content cache - stores fetched markdown content and code content for each step
  const [learningContentCache, setLearningContentCache] = useState<Record<string, { content: string; codeContent?: string; codeFileName?: string; error: string | null }>>({})
  
  // Loading states for individual items
  const [loadingStepIds, setLoadingStepIds] = useState<Set<string>>(new Set())

  const [updatingLockStepIds, setUpdatingLockStepIds] = useState<Set<string>>(new Set())
  
  // Delete state
  const [deletingStepIds, setDeletingStepIds] = useState<Set<string>>(new Set())
  const [deleteConfirmStepId, setDeleteConfirmStepId] = useState<string | null>(null)
  
  // Filter state - show only unlocked steps
  const [showOnlyUnlocked, setShowOnlyUnlocked] = useState(false)

  // Get preset default for code execution mode (fallback when step doesn't have explicit setting)
  const activePresetId = useGlobalPresetStore(state => state.activePresetIds.workflow)
  const customPresets = useGlobalPresetStore(state => state.customPresets)
  const predefinedPresets = useGlobalPresetStore(state => state.predefinedPresets)
  const activePreset = activePresetId
    ? customPresets.find(p => p.id === activePresetId) || predefinedPresets.find(p => p.id === activePresetId)
    : null
  const presetUseCodeExecutionMode = activePreset?.useCodeExecutionMode ?? false

  // Fetch learnings when popup opens (API now includes step config data merged in)
  useEffect(() => {
    if (!isOpen || !workspacePath) return

    setIsLoading(true)
    setError(null)

    agentApi.getAllStepLearnings(workspacePath)
      .then((response) => {
        if (response.success) {
          setLearnings(parseLearningsResponse(response.learnings || {}))
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
  }, [isOpen, workspacePath])

  const toggleLock = async (stepId: string, isCurrentlyLocked: boolean) => {
    if (!workspacePath || updatingLockStepIds.has(stepId)) return

    setUpdatingLockStepIds(prev => new Set(prev).add(stepId))

    try {
      const step = plan?.steps?.find(s => s.id === stepId)
      const metadata = learnings[stepId]
      const currentConfigs = step?.agent_configs || (metadata ? { lock_learnings: metadata.lock_learnings } : {})

      await agentApi.updateStepConfig(workspacePath, stepId, {
        ...currentConfigs,
        lock_learnings: !isCurrentlyLocked
      })

      // Refresh learnings
      const response = await agentApi.getAllStepLearnings(workspacePath)
      if (response.success) {
        setLearnings(parseLearningsResponse(response.learnings || {}))
      }
    } catch (err: unknown) {
      console.error('[LearningsPopup] Error toggling lock:', err)
      const errorMessage = err instanceof Error ? err.message : 'Unknown error'
      setError('Failed to update lock status: ' + errorMessage)
    } finally {
      setUpdatingLockStepIds(prev => {
        const newSet = new Set(prev)
        newSet.delete(stepId)
        return newSet
      })
    }
  }

  const handleDeleteLearning = async (stepId: string) => {
    if (!workspacePath || deletingStepIds.has(stepId)) return

    setDeletingStepIds(prev => new Set(prev).add(stepId))
    setDeleteConfirmStepId(null)

    try {
      // Delete learnings folder
      const deleteResult = await agentApi.deleteStepLearnings(workspacePath, stepId)
      
      if (!deleteResult.success) {
        throw new Error(deleteResult.message || 'Failed to delete learnings')
      }

      // Unlock learnings after deletion
      const step = plan?.steps?.find(s => s.id === stepId)
      const metadata = learnings[stepId]
      const currentConfigs = step?.agent_configs || (metadata ? { lock_learnings: metadata.lock_learnings } : {})

      try {
        await agentApi.updateStepConfig(workspacePath, stepId, {
          ...currentConfigs,
          lock_learnings: false
        })
      } catch (unlockErr) {
        console.warn('[LearningsPopup] Failed to unlock learnings after deletion:', unlockErr)
        // Continue even if unlock fails - deletion was successful
      }

      // Remove from cache
      setLearningContentCache(prev => {
        const newCache = { ...prev }
        delete newCache[stepId]
        return newCache
      })

      // Refresh learnings list
      const response = await agentApi.getAllStepLearnings(workspacePath)
      if (response.success) {
        setLearnings(parseLearningsResponse(response.learnings || {}))
      }
    } catch (err: unknown) {
      console.error('[LearningsPopup] Error deleting learnings:', err)
      const errorMessage = err instanceof Error ? err.message : 'Unknown error'
      setError('Failed to delete learnings: ' + errorMessage)
    } finally {
      setDeletingStepIds(prev => {
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
      const learningsPath = `${workspacePath}/learnings/${stepId}`
      let mdContent = ''
      let codeContent = ''
      let codeFileName = ''
      let error: string | null = null

      // List files in the learnings folder to find the markdown file
      const filesResponse = await agentApi.getPlannerFiles(learningsPath, 100)
      const files: Array<PlannerFile & { name?: string }> = Array.isArray(filesResponse)
        ? filesResponse
        : (filesResponse?.data && Array.isArray(filesResponse.data) ? filesResponse.data : [])

      // Find the first .md file (excluding metadata files)
      const mdFile = files.find((file) => {
        const fileName = file.filepath || file.name || ''
        return fileName.endsWith('.md') && !fileName.includes('.learning_metadata')
      })

      // Fetch markdown content
      if (mdFile) {
        let filePath = mdFile.filepath || mdFile.name
        if (filePath) {
          if (!filePath.startsWith(workspacePath)) {
            const cleanPath = filePath.startsWith('/') ? filePath.slice(1) : filePath
            filePath = `${workspacePath}/${cleanPath}`
          }
          const response = await agentApi.getPlannerFileContent(filePath)
          if (response.success && response.data && response.data.content) {
            mdContent = response.data.content
          }
        }
      }

      // Also check the code/ subdirectory for .go files
      try {
        const codePath = `${learningsPath}/code`
        console.log('[LearningsPopup] Checking code path:', codePath)
        const codeFilesResponse = await agentApi.getPlannerFiles(codePath, 100)
        console.log('[LearningsPopup] Code files response:', codeFilesResponse)
        const codeFiles: Array<PlannerFile & { name?: string }> = Array.isArray(codeFilesResponse)
          ? codeFilesResponse
          : (codeFilesResponse?.data && Array.isArray(codeFilesResponse.data) ? codeFilesResponse.data : [])
        console.log('[LearningsPopup] Code files found:', codeFiles)

        // Find the first .go file
        const codeFile = codeFiles.find((file) => {
          const fileName = file.filepath || file.name || ''
          return fileName.endsWith('.go')
        })
        console.log('[LearningsPopup] Code file:', codeFile)

        if (codeFile) {
          let codeFilePath = codeFile.filepath || codeFile.name
          if (codeFilePath) {
            codeFileName = codeFilePath.split('/').pop() || 'code.go'
            if (!codeFilePath.startsWith(workspacePath)) {
              const cleanPath = codeFilePath.startsWith('/') ? codeFilePath.slice(1) : codeFilePath
              codeFilePath = `${workspacePath}/${cleanPath}`
            }
            console.log('[LearningsPopup] Fetching code from:', codeFilePath)
            const codeResponse = await agentApi.getPlannerFileContent(codeFilePath)
            console.log('[LearningsPopup] Code response:', codeResponse)
            if (codeResponse.success && codeResponse.data && codeResponse.data.content) {
              codeContent = codeResponse.data.content
              console.log('[LearningsPopup] Code content loaded, length:', codeContent.length)
            }
          }
        }
      } catch (codeErr) {
        console.log('[LearningsPopup] Code folder error (might not exist):', codeErr)
      }

      if (!mdContent && !codeContent) {
        error = 'No learning content found'
      }

      setLearningContentCache(prev => ({
        ...prev,
        [stepId]: { content: mdContent, codeContent, codeFileName, error }
      }))
    } catch (err: unknown) {
      console.error('[LearningsPopup] Error fetching learning content:', err)
      const errorMessage = err instanceof Error ? err.message : 'Unknown error'
      setLearningContentCache(prev => ({
        ...prev,
        [stepId]: { content: '', error: 'Failed to load learning content: ' + errorMessage }
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
  let stepsWithLearnings = allStepsInOrder.filter(step => step.stepId in learnings)
  
  // Apply unlocked filter if enabled
  if (showOnlyUnlocked) {
    stepsWithLearnings = stepsWithLearnings.filter(step => {
      const metadata = learnings[step.stepId]
      const isAutoLocked = metadata?.auto_locked_at !== undefined && metadata.auto_locked_at !== ''
      const isManuallyLocked = metadata?.lock_learnings === true
      const isLocked = isAutoLocked || isManuallyLocked
      return !isLocked // Show only unlocked steps
    })
  }

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4" style={{ zIndex: 50 }}>
      <div className="bg-background border border-border rounded-lg shadow-xl w-full max-w-4xl max-h-[90vh] flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between p-4 border-b border-border flex-shrink-0">
          <div className="flex items-center gap-2">
            <BookOpen className="w-5 h-5 text-primary" />
            <h2 className="text-lg font-semibold">Step Learnings</h2>
          </div>
          <div className="flex items-center gap-3">
            {/* Filter: Show only unlocked steps */}
            <button
              onClick={() => setShowOnlyUnlocked(!showOnlyUnlocked)}
              className={`flex items-center gap-2 px-3 py-1.5 rounded-md text-sm font-medium transition-colors ${
                showOnlyUnlocked
                  ? 'bg-yellow-100 hover:bg-yellow-200 dark:bg-yellow-900/30 dark:hover:bg-yellow-900/50 text-yellow-700 dark:text-yellow-400'
                  : 'bg-gray-100 hover:bg-gray-200 dark:bg-gray-800 dark:hover:bg-gray-700 text-gray-700 dark:text-gray-300'
              }`}
              title={showOnlyUnlocked ? 'Show all steps' : 'Show only unlocked steps'}
            >
              <Unlock className="w-4 h-4" />
              <span>Unlocked Only</span>
            </button>
            <button
              onClick={onClose}
              className="p-1 rounded-md hover:bg-muted transition-colors"
              title="Close (Esc)"
            >
              <X className="w-5 h-5" />
            </button>
          </div>
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
                // Check lock status from both sources: auto_locked_at (metadata) OR lock_learnings (from step config via API)
                const isAutoLocked = metadata?.auto_locked_at !== undefined && metadata.auto_locked_at !== ''
                const isManuallyLocked = metadata?.lock_learnings === true
                const isLocked = isAutoLocked || isManuallyLocked
                const complexity = getComplexity(metadata) // Used only for display (complexity label/color)
                const threshold = getLockThreshold(metadata) // Backend-calculated threshold
                const successfulRuns = getSuccessfulRuns(metadata)
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

                // Determine effective code execution mode: step config > preset default
                const effectiveUseCodeExecutionMode = metadata?.use_code_execution_mode !== undefined
                  ? metadata.use_code_execution_mode
                  : presetUseCodeExecutionMode

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
                            <button
                              onClick={(e) => {
                                e.stopPropagation()
                                setDeleteConfirmStepId(stepId)
                              }}
                              disabled={deletingStepIds.has(stepId)}
                              className="flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium transition-colors disabled:opacity-50 disabled:cursor-not-allowed bg-red-100 hover:bg-red-200 dark:bg-red-900/30 dark:hover:bg-red-900/50 text-red-700 dark:text-red-400"
                              title="Delete learnings"
                            >
                              {deletingStepIds.has(stepId) ? (
                                <>
                                  <Loader2 className="w-3.5 h-3.5 animate-spin" />
                                  <span>Deleting...</span>
                                </>
                              ) : (
                                <>
                                  <Trash2 className="w-3.5 h-3.5" />
                                  <span>Delete</span>
                                </>
                              )}
                            </button>
                            {metadata && (
                              <div className="flex flex-col gap-1">
                                <div className="flex items-center gap-2">
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
                                  
                                  {/* Detail Level Badge - from step config (via API) */}
                                  {metadata?.learning_detail_level && (
                                    <span className={`ml-2 text-xs px-1.5 py-0.5 rounded font-medium border ${
                                      metadata.learning_detail_level === 'general'
                                        ? 'bg-blue-50 text-blue-700 border-blue-200 dark:bg-blue-900/20 dark:text-blue-300 dark:border-blue-800'
                                        : 'bg-purple-50 text-purple-700 border-purple-200 dark:bg-purple-900/20 dark:text-purple-300 dark:border-purple-800'
                                    }`}>
                                      {metadata.learning_detail_level === 'general' ? 'General Mode' : 'Exact Mode'}
                                    </span>
                                  )}

                                  {/* Execution Mode Badge - Simple vs Agent (step config > preset default) */}
                                  <span className={`ml-2 text-xs px-1.5 py-0.5 rounded font-medium border flex items-center gap-1 ${
                                    effectiveUseCodeExecutionMode
                                      ? 'bg-teal-50 text-teal-600 border-teal-200 dark:bg-teal-900/20 dark:text-teal-400 dark:border-teal-800'
                                      : 'bg-gray-50 text-gray-600 border-gray-200 dark:bg-gray-800/50 dark:text-gray-400 dark:border-gray-700'
                                  }`}>
                                    {effectiveUseCodeExecutionMode ? (
                                      <>
                                        <Code className="w-3 h-3" />
                                        Agent Mode
                                      </>
                                    ) : (
                                      <>
                                        <FileText className="w-3 h-3" />
                                        Simple Mode
                                      </>
                                    )}
                                  </span>
                                </div>
                                
                                {metadata.total_iterations !== undefined && (
                                  <div className="flex items-center gap-2 text-xs text-muted-foreground">
                                    <span>Total Iterations: {metadata.total_iterations}</span>
                                    {metadata.auto_lock_reason && (
                                      <span className="text-amber-600 dark:text-amber-500" title={metadata.auto_lock_reason}>
                                        • Reason: {metadata.auto_lock_reason.replace('threshold_reached_', '').replace(/_/g, ' ')}
                                      </span>
                                    )}
                                  </div>
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
                            
                            {/* Learning Caps Info */}
                            <div className="flex items-center gap-4 mt-1.5 text-[10px] text-muted-foreground">
                              <div className="flex items-center gap-1">
                                <span className={successfulRuns >= 3 ? "text-amber-600 dark:text-amber-500 font-medium" : ""}>
                                  Success Cap: 3 runs
                                </span>
                                {successfulRuns >= 3 && !isLocked && (
                                  <span className="text-[9px] bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300 px-1 rounded">
                                    Capped (Skipping)
                                  </span>
                                )}
                              </div>
                              <div>Failure Cap: Max 2/run</div>
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

                      {/* Code Content Section for Agent Mode */}
                      {!isLoadingContent && cachedContent && !cachedContent.error && cachedContent.codeContent && (
                        <div className="mt-4">
                          <div className="flex items-center gap-2 mb-2">
                            <Code className="w-4 h-4 text-emerald-600 dark:text-emerald-400" />
                            <span className="text-sm font-medium text-emerald-700 dark:text-emerald-300">
                              Agent Code
                            </span>
                            {cachedContent.codeFileName && (
                              <span className="text-xs text-muted-foreground font-mono bg-muted px-1.5 py-0.5 rounded">
                                {cachedContent.codeFileName}
                              </span>
                            )}
                          </div>
                          <div className="relative rounded-lg border border-emerald-200 dark:border-emerald-800 bg-slate-900 dark:bg-slate-950 overflow-hidden">
                            <div className="max-h-[400px] overflow-auto">
                              <pre className="p-4 text-sm font-mono text-slate-100 whitespace-pre-wrap break-words">
                                <code>{cachedContent.codeContent}</code>
                              </pre>
                            </div>
                          </div>
                        </div>
                      )}

                      {!isLoadingContent && cachedContent && !cachedContent.error && !cachedContent.content && !cachedContent.codeContent && (
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

      {/* Delete Confirmation Dialog */}
      <ConfirmationDialog
        isOpen={deleteConfirmStepId !== null}
        onClose={() => setDeleteConfirmStepId(null)}
        onConfirm={() => {
          if (deleteConfirmStepId) {
            handleDeleteLearning(deleteConfirmStepId)
          }
        }}
        title="Delete Learnings"
        message={
          deleteConfirmStepId
            ? (() => {
                const stepTitle = getStepTitle(plan, deleteConfirmStepId)
                return `Are you sure you want to delete all learnings for "${stepTitle}"? This will permanently delete the learnings folder at \`learnings/${deleteConfirmStepId}/\` and all its contents. The learnings will also be unlocked. This action cannot be undone.`
              })()
            : ''
        }
        confirmText="Delete Learnings"
        cancelText="Cancel"
        type="danger"
      />
    </div>
  )
}
