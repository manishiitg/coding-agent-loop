import { useEffect, useState, useCallback } from 'react'
import { X, BookOpen, Lock, Unlock, Loader2, AlertCircle, ChevronDown, ChevronRight, Code, FileText, Trash2, Search, Terminal } from 'lucide-react'
import { agentApi } from '../../services/api'
import type { PlanningResponse, PlanStep } from '../../utils/stepConfigMatching'
import { isConditionalStep, isDecisionStep, isOrchestrationStep, isTodoTaskStep } from '../../utils/stepConfigMatching'
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
  use_tool_search_mode?: boolean
  learning_detail_level?: string
  lock_learnings?: boolean
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

// Check if learnings folder exists
// Returns true only if metadata contains actual learning data (not just step config fields)
// Step config fields (use_code_execution_mode, learning_detail_level, lock_learnings) can exist
// even when the folder doesn't exist, so we need to check for actual learning data fields
function hasLearningsFolder(
  metadata: LearningMetadata | null,
  cachedContent: { content: string; codeContent?: string; codeFileName?: string; error: string | null } | undefined
): boolean {
  if (!metadata) return false
  
  // Check if metadata has actual learning data fields (not just step config)
  // These fields indicate the folder exists and has been used for learning:
  const hasLearningData = 
    metadata.step_id !== undefined ||
    metadata.successful_runs_simple !== undefined ||
    metadata.successful_runs_medium !== undefined ||
    metadata.successful_runs_complex !== undefined ||
    metadata.last_turn_count !== undefined ||
    metadata.auto_locked_at !== undefined ||
    metadata.auto_lock_reason !== undefined ||
    metadata.total_iterations !== undefined ||
    metadata.lock_threshold !== undefined
  
  // If no learning data fields, folder doesn't exist (only step config fields present)
  if (!hasLearningData) return false
  
  // If we have cached content with an error indicating folder doesn't exist, return false
  if (cachedContent?.error) {
    const errorLower = cachedContent.error.toLowerCase()
    if (errorLower.includes('not found') || 
        errorLower.includes("doesn't exist") ||
        errorLower.includes('does not exist') ||
        errorLower.includes('no such file') ||
        errorLower.includes('no such directory')) {
      return false
    }
  }
  
  // Folder exists if we have learning data fields
  return true
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
      // Check orchestration step and its routes
      if ('orchestration_step' in step && step.orchestration_step && step.orchestration_step.id === id) {
        return step.orchestration_step
      }
      if ('orchestration_routes' in step && step.orchestration_routes) {
        for (const route of step.orchestration_routes) {
          if (route.sub_agent_step && route.sub_agent_step.id === id) {
            return route.sub_agent_step
          }
        }
      }
      // Check todo_task predefined_routes
      if ('predefined_routes' in step && step.predefined_routes) {
        for (const route of step.predefined_routes) {
          if (route.sub_agent_step && route.sub_agent_step.id === id) {
            return route.sub_agent_step
          }
        }
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
  // Search state
  const [searchTerm, setSearchTerm] = useState('')

  // Get preset default for code execution mode (fallback when step doesn't have explicit setting)
  const activePresetId = useGlobalPresetStore(state => state.activePresetIds.workflow)
  const customPresets = useGlobalPresetStore(state => state.customPresets)
  const predefinedPresets = useGlobalPresetStore(state => state.predefinedPresets)
  const activePreset = activePresetId
    ? customPresets.find(p => p.id === activePresetId) || predefinedPresets.find(p => p.id === activePresetId)
    : null
  const presetUseCodeExecutionMode = activePreset?.useCodeExecutionMode ?? false
  const presetUseToolSearchMode = activePreset?.useToolSearchMode ?? false

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

      // Remove from expanded items if it was expanded
      setExpandedStepIds(prev => {
        const newSet = new Set(prev)
        newSet.delete(stepId)
        return newSet
      })

      // Clear any error state
      setError(null)

      // Refresh learnings list to update UI
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

      // Check for code files in the code/ subdirectory (already returned in the files listing with code/ prefix)
      const codeExtensions = ['.go', '.py', '.sh', '.js', '.ts', '.jsx', '.tsx', '.bash', '.curl', '.rb', '.java', '.rs', '.c', '.cpp', '.json', '.yaml', '.yml']

      // First try: find code files from the already-fetched file listing (files with code/ prefix)
      let codeFile = files.find((file) => {
        const fileName = file.filepath || file.name || ''
        return fileName.startsWith('code/') && codeExtensions.some(ext => fileName.endsWith(ext))
      })
      console.log('[LearningsPopup] Code file from parent listing:', codeFile)

      // Fallback: try listing the code/ subdirectory directly
      if (!codeFile) {
        try {
          const codePath = `${learningsPath}/code`
          console.log('[LearningsPopup] Checking code path:', codePath)
          const codeFilesResponse = await agentApi.getPlannerFiles(codePath, 100)
          console.log('[LearningsPopup] Code files response:', codeFilesResponse)
          const codeFiles: Array<PlannerFile & { name?: string }> = Array.isArray(codeFilesResponse)
            ? codeFilesResponse
            : (codeFilesResponse?.data && Array.isArray(codeFilesResponse.data) ? codeFilesResponse.data : [])
          console.log('[LearningsPopup] Code files found:', codeFiles)

          codeFile = codeFiles.find((file) => {
            const fileName = file.filepath || file.name || ''
            return codeExtensions.some(ext => fileName.endsWith(ext))
          })
          console.log('[LearningsPopup] Code file from subfolder listing:', codeFile)
        } catch (codeErr) {
          console.log('[LearningsPopup] Code folder error (might not exist):', codeErr)
        }
      }

      if (codeFile) {
        let codeFilePath = codeFile.filepath || codeFile.name
        if (codeFilePath) {
          codeFileName = codeFilePath.split('/').pop() || 'code'
          if (!codeFilePath.startsWith(workspacePath)) {
            const cleanPath = codeFilePath.startsWith('/') ? codeFilePath.slice(1) : codeFilePath
            codeFilePath = `${workspacePath}/${cleanPath}`
          }
          // If filepath is relative (e.g. code/file.py), prepend learningsPath
          if (!codeFilePath.includes('/learnings/')) {
            codeFilePath = `${learningsPath}/${codeFilePath}`
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
  const getStepsInExecutionOrder = useCallback((): Array<{ stepId: string; stepNumber: number; stepType: string; branchType?: string; parentStepId?: string }> => {
    if (!plan || !plan.steps) return []

    const stepsWithMetadata: Array<{ stepId: string; stepNumber: number; stepType: string; branchType?: string; parentStepId?: string }> = []
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
                  branchType: `sub-agent-${routeIdx}`,
                  parentStepId: step.orchestration_step?.id || step.id // Track parent for nesting
                })
              }
            })
          }
        }

        // Handle todo_task steps - collect sub-agent step IDs from predefined_routes
        if (isTodoTaskStep(step)) {
          if (step.predefined_routes) {
            step.predefined_routes.forEach((route, routeIdx) => {
              if (route.sub_agent_step && route.sub_agent_step.id) {
                stepCounter++
                stepsWithMetadata.push({
                  stepId: route.sub_agent_step.id,
                  stepNumber: stepCounter,
                  stepType: 'todo_sub_agent',
                  branchType: `todo-sub-agent-${route.route_id || routeIdx}`,
                  parentStepId: step.id // Track parent for nesting
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

  // Apply search filter
  if (searchTerm) {
    const lowerTerm = searchTerm.toLowerCase()
    stepsWithLearnings = stepsWithLearnings.filter(step => {
      const title = getStepTitle(plan, step.stepId).toLowerCase()
      const id = step.stepId.toLowerCase()
      return title.includes(lowerTerm) || id.includes(lowerTerm)
    })
  }

  const handleExpandAll = () => {
    const newExpanded = new Set<string>()
    stepsWithLearnings.forEach(step => {
      newExpanded.add(step.stepId)
      // Trigger fetch if not cached
      if (!learningContentCache[step.stepId]) {
        fetchLearningContent(step.stepId)
      }
    })
    setExpandedStepIds(newExpanded)
  }

  const handleCollapseAll = () => {
    setExpandedStepIds(new Set())
  }

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4" style={{ zIndex: 50 }}>
      <div className="bg-background border border-border rounded-lg shadow-xl w-full max-w-4xl max-h-[90vh] flex flex-col">
        {/* Header */}
        <div className="flex flex-col border-b border-border flex-shrink-0">
          <div className="flex items-center justify-between p-4 pb-2">
            <div className="flex items-center gap-2">
              <BookOpen className="w-5 h-5 text-primary" />
              <h2 className="text-lg font-semibold">Step Learnings</h2>
            </div>
            <div className="flex items-center gap-3">
              <button
                onClick={onClose}
                className="p-1 rounded-md hover:bg-muted transition-colors"
                title="Close (Esc)"
              >
                <X className="w-5 h-5" />
              </button>
            </div>
          </div>
          
          <div className="flex items-center gap-3 px-4 pb-4">
             {/* Search Bar */}
             <div className="relative flex-1">
              <Search className="absolute left-2.5 top-2.5 w-4 h-4 text-muted-foreground" />
              <input
                type="text"
                placeholder="Search steps..."
                value={searchTerm}
                onChange={(e) => setSearchTerm(e.target.value)}
                className="w-full pl-9 pr-3 py-2 text-sm bg-muted/50 border border-input rounded-md focus:outline-none focus:ring-1 focus:ring-primary"
              />
              {searchTerm && (
                <button
                  onClick={() => setSearchTerm('')}
                  className="absolute right-2.5 top-2.5 p-0.5 rounded-full hover:bg-muted transition-colors"
                >
                  <X className="w-3 h-3 text-muted-foreground" />
                </button>
              )}
            </div>

            {/* Actions */}
            <div className="flex items-center gap-2">
              <button
                onClick={handleExpandAll}
                className="px-3 py-2 text-xs font-medium bg-muted hover:bg-muted/80 rounded-md transition-colors whitespace-nowrap"
              >
                Expand All
              </button>
              <button
                onClick={handleCollapseAll}
                className="px-3 py-2 text-xs font-medium bg-muted hover:bg-muted/80 rounded-md transition-colors whitespace-nowrap"
              >
                Collapse All
              </button>
              {/* Filter: Show only unlocked steps */}
              <button
                onClick={() => setShowOnlyUnlocked(!showOnlyUnlocked)}
                className={`flex items-center gap-2 px-3 py-2 rounded-md text-xs font-medium transition-colors whitespace-nowrap ${
                  showOnlyUnlocked
                    ? 'bg-yellow-100 hover:bg-yellow-200 dark:bg-yellow-900/30 dark:hover:bg-yellow-900/50 text-yellow-700 dark:text-yellow-400'
                    : 'bg-muted hover:bg-muted/80 text-foreground'
                }`}
                title={showOnlyUnlocked ? 'Show all steps' : 'Show only unlocked steps'}
              >
                <Unlock className="w-3.5 h-3.5" />
                <span>Unlocked Only</span>
              </button>
            </div>
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
            <div className="text-center py-8 text-muted-foreground flex flex-col items-center gap-2">
              <BookOpen className="w-10 h-10 opacity-20" />
              <p>No steps with learnings found</p>
              {searchTerm && <p className="text-sm">Try adjusting your search query</p>}
              {showOnlyUnlocked && <p className="text-sm">Try disabling the "Unlocked Only" filter</p>}
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
                  // Use same "Sub-Agent" label for both orchestration and todo_task sub-agents
                  if (branchType?.startsWith('todo-sub-agent') || stepType === 'todo_sub_agent') return 'Sub-Agent'
                  if (branchType?.startsWith('sub-agent') || stepType === 'sub_agent') return 'Sub-Agent'
                  if (stepType === 'decision_inner') return 'Decision'
                  if (stepType === 'orchestration_inner') return 'Orchestration'
                  return stepType.charAt(0).toUpperCase() + stepType.slice(1)
                }

                const getStepTypeBadgeColor = () => {
                  if (branchType === 'true') return 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400'
                  if (branchType === 'false') return 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400'
                  // Use same orange color for both orchestration and todo_task sub-agents
                  if (branchType?.startsWith('todo-sub-agent') || stepType === 'todo_sub_agent') return 'bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400'
                  if (branchType?.startsWith('sub-agent') || stepType === 'sub_agent') return 'bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400'
                  if (stepType === 'decision_inner' || stepType === 'orchestration_inner') return 'bg-indigo-100 text-indigo-700 dark:bg-indigo-900/30 dark:text-indigo-400'
                  return 'bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300'
                }

                // Check if this is a sub-agent (should be indented)
                const isSubAgent = stepType === 'sub_agent' || stepType === 'todo_sub_agent'

                // Determine effective modes: step config > preset default
                const effectiveUseCodeExecutionMode = metadata?.use_code_execution_mode !== undefined
                  ? metadata.use_code_execution_mode
                  : presetUseCodeExecutionMode
                
                const effectiveUseToolSearchMode = metadata?.use_tool_search_mode !== undefined
                  ? metadata.use_tool_search_mode
                  : presetUseToolSearchMode

                return (
                  <div
                    key={stepId}
                    className={`border border-border rounded-lg bg-muted/30 hover:bg-muted/50 transition-colors ${
                      isSubAgent ? 'ml-6 border-l-4 border-l-orange-400 dark:border-l-orange-500' : ''
                    }`}
                  >
                    <div 
                      className="p-5 cursor-pointer"
                      onClick={() => toggleExpand(stepId)}
                    >
                      <div className="flex items-start justify-between">
                        <div className="flex-1 min-w-0">
                          <div className="flex items-start gap-3 mb-2">
                            <button
                              onClick={(e) => {
                                e.stopPropagation()
                                toggleExpand(stepId)
                              }}
                              className="mt-0.5 p-1 hover:bg-muted rounded-md transition-colors shrink-0"
                              title={isExpanded ? "Collapse" : "Expand"}
                            >
                              {isExpanded ? (
                                <ChevronDown className="w-4 h-4 text-muted-foreground" />
                              ) : (
                                <ChevronRight className="w-4 h-4 text-muted-foreground" />
                              )}
                            </button>
                            <div className="flex-1 min-w-0">
                              <div className="flex items-center gap-2 mb-1 flex-wrap">
                                <span className="text-xs font-mono font-semibold text-primary bg-primary/10 px-1.5 py-0.5 rounded shrink-0">
                                  #{stepNumber}
                                </span>
                                <span className={`text-xs px-1.5 py-0.5 rounded font-medium shrink-0 ${getStepTypeBadgeColor()}`}>
                                  {getStepTypeLabel()}
                                </span>
                                <h3 className="font-medium truncate" title={stepTitle}>{stepTitle}</h3>
                              </div>
                              <div className="text-xs text-muted-foreground font-mono truncate" title={stepId}>{stepId}</div>
                            </div>
                          </div>

                          <div className="flex flex-col gap-3 ml-8">
                            {/* Metadata Row 1: Lock Status & Complexity */}
                            <div className="flex items-center gap-4 flex-wrap text-sm">
                              {metadata && (
                                <>
                                  <div className="flex items-center gap-2">
                                    {isLocked ? (
                                      <>
                                        <Lock className="w-3.5 h-3.5 text-green-600 dark:text-green-400" />
                                        <span className="text-green-600 dark:text-green-400 font-medium text-xs">
                                          {isAutoLocked && isManuallyLocked ? 'Locked (Auto + Manual)' :
                                           isAutoLocked ? 'Locked (Auto)' :
                                           'Locked'}
                                        </span>
                                      </>
                                    ) : (
                                      <>
                                        <Unlock className="w-3.5 h-3.5 text-yellow-600 dark:text-yellow-400" />
                                        <span className="text-yellow-600 dark:text-yellow-400 font-medium text-xs">Unlocked</span>
                                      </>
                                    )}
                                  </div>
                                  <button
                                    onClick={(e) => {
                                      e.stopPropagation()
                                      toggleLock(stepId, isLocked)
                                    }}
                                    disabled={isUpdatingLock}
                                    className={`flex items-center gap-1.5 px-2 py-0.5 rounded text-xs font-medium transition-colors disabled:opacity-50 disabled:cursor-not-allowed ${
                                      isLocked
                                        ? 'bg-yellow-50 hover:bg-yellow-100 dark:bg-yellow-900/20 dark:hover:bg-yellow-900/40 text-yellow-700 dark:text-yellow-400'
                                        : 'bg-muted hover:bg-muted/80 text-muted-foreground'
                                    }`}
                                    title={isLocked ? "Unlock learnings" : "Lock learnings manually"}
                                  >
                                    {isUpdatingLock ? (
                                      <>
                                        <Loader2 className="w-3 h-3 animate-spin" />
                                        <span>Updating...</span>
                                      </>
                                    ) : isLocked ? (
                                      <>
                                        <Unlock className="w-3 h-3" />
                                        <span>Unlock</span>
                                      </>
                                    ) : (
                                      <>
                                        <Lock className="w-3 h-3" />
                                        <span>Lock</span>
                                      </>
                                    )}
                                  </button>
                                </>
                              )}
                            </div>

                            {/* Metadata Row 2: Badges */}
                            {metadata && (
                              <div className="flex items-center gap-3 flex-wrap">
                                {metadata.last_turn_count !== undefined && metadata.last_turn_count > 0 && (
                                  <div className="flex items-center gap-1.5 text-xs text-muted-foreground bg-muted/30 px-2 py-1 rounded border border-border">
                                    <span>Turn Count:</span>
                                    <span className="font-medium text-foreground">{metadata.last_turn_count}</span>
                                  </div>
                                )}
                                

                                <span className={`text-xs px-2 py-1 rounded font-medium border flex items-center gap-1.5 ${
                                  effectiveUseCodeExecutionMode
                                    ? 'bg-amber-50 text-amber-700 border-amber-200 dark:bg-amber-900/20 dark:text-amber-300 dark:border-amber-800'
                                    : effectiveUseToolSearchMode
                                    ? 'bg-blue-50 text-blue-700 border-blue-200 dark:bg-blue-900/20 dark:text-blue-300 dark:border-blue-800'
                                    : 'bg-slate-50 text-slate-700 border-slate-200 dark:bg-slate-800/50 dark:text-slate-300 dark:border-slate-700'
                                }`}>
                                  {effectiveUseCodeExecutionMode ? (
                                    <>
                                      <Terminal className="w-3.5 h-3.5" />
                                      Agent Mode
                                    </>
                                  ) : effectiveUseToolSearchMode ? (
                                    <>
                                      <Search className="w-3.5 h-3.5" />
                                      Tool Search
                                    </>
                                  ) : (
                                    <>
                                      <FileText className="w-3.5 h-3.5" />
                                      Simple Mode
                                    </>
                                  )}
                                </span>

                                {metadata.total_iterations !== undefined && (
                                  <div className="flex items-center gap-1.5 text-xs text-muted-foreground bg-muted/30 px-2 py-1 rounded border border-border ml-auto">
                                    <span>Iterations:</span>
                                    <span className="font-mono font-medium text-foreground">{metadata.total_iterations}</span>
                                    {metadata.auto_lock_reason && (
                                      <span className="text-amber-600 dark:text-amber-500 border-l border-border pl-1.5 ml-0.5 truncate max-w-[150px]" title={metadata.auto_lock_reason}>
                                        {metadata.auto_lock_reason.replace('threshold_reached_', '').replace(/_/g, ' ')}
                                      </span>
                                    )}
                                  </div>
                                )}
                              </div>
                            )}
                          </div>
                        </div>

                        {/* Delete Button */}
                        {hasLearningsFolder(metadata, cachedContent) && (
                          <button
                            onClick={(e) => {
                              e.stopPropagation()
                              setDeleteConfirmStepId(stepId)
                            }}
                            disabled={deletingStepIds.has(stepId)}
                            className="p-2 rounded-md text-muted-foreground hover:text-red-600 hover:bg-red-50 dark:hover:bg-red-900/20 transition-colors disabled:opacity-50 disabled:cursor-not-allowed ml-2 shrink-0 self-start"
                            title="Delete learnings"
                          >
                            {deletingStepIds.has(stepId) ? (
                              <Loader2 className="w-4 h-4 animate-spin" />
                            ) : (
                              <Trash2 className="w-4 h-4" />
                            )}
                          </button>
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
