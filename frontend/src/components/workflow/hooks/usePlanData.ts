import { useState, useCallback, useEffect, useRef } from 'react'
import { agentApi } from '../../../services/api'
import type { PlanStep, PlanningResponse, StepConfig, AgentConfigs } from '../../../utils/stepConfigMatching'
import { isConditionalStep, isDecisionStep, isOrchestrationStep, isTodoTaskStep } from '../../../utils/stepConfigMatching'
import { useWorkflowStore } from '../../../stores/useWorkflowStore'

// Module-level cache to dedupe loadPlan calls across multiple hook instances
// This prevents duplicate API calls when multiple components use usePlanData
interface PlanCache {
  workspacePath: string | null
  promise: Promise<PlanningResponse | null> | null
  data: PlanningResponse | null
  timestamp: number
}

const planCache: PlanCache = {
  workspacePath: null,
  promise: null,
  data: null,
  timestamp: 0
}

// Cache expiry time (5 seconds) - allows refetch after mutations
const CACHE_EXPIRY_MS = 5000

/**
 * Signal that plan.json was modified externally (e.g., by an LLM tool call).
 * Dispatches a custom DOM event that usePlanData listens for to auto-refresh.
 */
export function signalPlanModified() {
  window.dispatchEvent(new Event('plan-modified'))
}

// Types for plan change detection
export type ChangeType = 'added' | 'updated' | 'deleted'

export interface PlanChanges {
  added: string[]      // Step IDs that were added
  updated: string[]    // Step IDs that were updated
  deleted: string[]    // Step IDs that were deleted
  hasChanges: boolean
}

export interface UsePlanDataReturn {
  plan: PlanningResponse | null
  loading: boolean
  error: string | null
  changes: PlanChanges | null  // Latest detected changes (set via setChanges from granular events)
  stepOverride: AgentConfigs | null  // Global step override config
  loadPlan: () => Promise<void>  // Loads plan without comparison
  /** @deprecated Legacy method - prefer using updateStep() which calls backend APIs. See method documentation for when to use. */
  savePlan: (plan: PlanningResponse) => Promise<void>
  /** @deprecated Legacy method - prefer using agentApi.updateStepConfig(). See method documentation for when to use. */
  saveStepConfig: (stepId: string, agentConfigs: AgentConfigs | undefined) => Promise<void>
  /** @deprecated Legacy method - prefer using agentApi.batchUpdateSteps(). See method documentation for when to use. */
  batchSaveStepConfig: (stepConfigs: Array<{ stepId: string; agentConfigs: AgentConfigs | undefined }>) => Promise<void>
  updateStep: (stepIndex: number, updates: Partial<PlanStep>) => Promise<void>
  deleteStep: (stepIndex: number) => Promise<void>
  addStep: (step: PlanStep, afterIndex?: number) => Promise<void>
  saveStepOverride: (agentConfigs: AgentConfigs | null) => Promise<void>  // Save global step override
  refresh: () => Promise<void>  // Refreshes plan without comparison (alias for loadPlan)
  clearChanges: () => void  // Clear the changes state
  setChanges: (changes: PlanChanges | null) => void  // Set changes directly (for granular events)
}

/**
 * Normalize step_config.json content to array format
 * Handles object format: { "steps": [{ "id": "...", "agent_configs": {...} }] }
 */
function normalizeStepConfigFile(rawContent: unknown): StepConfig[] {
  // Handle object format with "steps" field: { "steps": [...] }
  if (rawContent && typeof rawContent === 'object' && !Array.isArray(rawContent)) {
    const obj = rawContent as Record<string, unknown>
    if ('steps' in obj && Array.isArray(obj.steps)) {
      return obj.steps as StepConfig[]
    }
  }

  // Legacy support: If it's already an array, return it (for backward compatibility during migration)
  if (Array.isArray(rawContent)) {
    console.warn('[normalizeStepConfigFile] Detected legacy array format, please migrate to { "steps": [...] } format')
    return rawContent as StepConfig[]
  }

  // Default: empty array
  return []
}

/**
 * Merge agent_configs from step_config.json into plan steps
 * This adds the agent_configs (including use_code_execution_mode) to each step
 */
function mergeStepConfigs(
  plan: PlanningResponse,
  stepConfigs: StepConfig[] | null
): PlanningResponse {
  if (!stepConfigs || stepConfigs.length === 0) return plan

  // Create map: step ID -> agent_configs
  const configMap = new Map<string, AgentConfigs>()
  for (const config of stepConfigs) {
    if (config.id && config.agent_configs) {
      configMap.set(config.id, config.agent_configs)
    }
  }

  // Recursively merge configs into a single step (handles nested structures)
  const mergeIntoStep = (step: PlanStep): PlanStep => {
    const config = step.id ? configMap.get(step.id) : undefined
    let mergedStep: PlanStep = {
      ...step,
      ...(config ? { agent_configs: config } : {})
    }
    
    // Handle nested branch steps
    if (isConditionalStep(step)) {
      mergedStep = {
        ...mergedStep,
        if_true_steps: step.if_true_steps ? mergeIntoSteps(step.if_true_steps) : undefined,
        if_false_steps: step.if_false_steps ? mergeIntoSteps(step.if_false_steps) : undefined,
      } as PlanStep
    }
    
    // Handle decision step
    if (isDecisionStep(step)) {
      mergedStep = {
        ...mergedStep,
        decision_step: step.decision_step ? mergeIntoStep(step.decision_step) : undefined,
      } as PlanStep
    }
    
    // Handle orchestration step
    if (isOrchestrationStep(step)) {
      mergedStep = {
        ...mergedStep,
        orchestration_step: step.orchestration_step ? mergeIntoStep(step.orchestration_step) : undefined,
        orchestration_routes: step.orchestration_routes ? step.orchestration_routes.map(route => ({
          ...route,
          sub_agent_step: mergeIntoStep(route.sub_agent_step)
        })) : undefined,
      } as PlanStep
    }

    // Handle todo task step
    if (isTodoTaskStep(step)) {
      mergedStep = {
        ...mergedStep,
        todo_task_step: step.todo_task_step ? mergeIntoStep(step.todo_task_step) : undefined,
        predefined_routes: step.predefined_routes ? step.predefined_routes.map(route => ({
          ...route,
          sub_agent_step: route.sub_agent_step ? mergeIntoStep(route.sub_agent_step) : undefined
        })) : undefined,
      } as PlanStep
    }

    return mergedStep
  }

  // Recursively merge configs into steps
  const mergeIntoSteps = (steps: PlanStep[]): PlanStep[] => {
    return steps.map(step => mergeIntoStep(step))
  }

  return {
    ...plan,
    steps: mergeIntoSteps(plan.steps)
  }
}

/**
 * Hook to read and write plan.json from the workspace
 * @param workspacePath - The workspace path (e.g., "Workflow/HRMS PR Review")
 */
export function usePlanData(workspacePath: string | null): UsePlanDataReturn {
  const [plan, setPlan] = useState<PlanningResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [changes, setChanges] = useState<PlanChanges | null>(null)
  const [stepOverride, setStepOverrideLocal] = useState<AgentConfigs | null>(null)
  const setStepOverrideInStore = useWorkflowStore(state => state.setStepOverride)

  // Sync stepOverride to both local state and workflow store
  const setStepOverride = useCallback((override: AgentConfigs | null) => {
    setStepOverrideLocal(override)
    setStepOverrideInStore(override)
  }, [setStepOverrideInStore])

  // Track workspace path to detect workflow switches
  const currentWorkspaceRef = useRef<string | null>(null)

  // Construct the plan file path
  const getPlanFilePath = useCallback(() => {
    if (!workspacePath) return null
    return `${workspacePath}/planning/plan.json`
  }, [workspacePath])

  // Construct the step config file path
  const getStepConfigFilePath = useCallback(() => {
    if (!workspacePath) return null
    return `${workspacePath}/planning/step_config.json`
  }, [workspacePath])

  // Internal function to actually fetch plan data (used by cached loadPlan)
  const fetchPlanData = useCallback(async (): Promise<PlanningResponse | null> => {
    const planPath = getPlanFilePath()
    const stepConfigPath = getStepConfigFilePath()
    if (!planPath) {
      throw new Error('No workspace path provided')
    }

    console.log('[WorkflowPlanUpdate] Fetching plan from:', planPath)
    const response = await agentApi.getPlannerFileContent(planPath)

    if (response.success && response.data && response.data.content && typeof response.data.content === 'string') {
      let planData: PlanningResponse = JSON.parse(response.data.content)
      console.log('[WorkflowPlanUpdate] Plan loaded:', { stepCount: planData.steps?.length || 0 })

      // Also load step_config.json to merge agent_configs
      if (stepConfigPath) {
        try {
          const stepConfigResponse = await agentApi.getPlannerFileContent(stepConfigPath)
          if (stepConfigResponse.success && stepConfigResponse.data && stepConfigResponse.data.content && typeof stepConfigResponse.data.content === 'string') {
            const rawStepConfig = JSON.parse(stepConfigResponse.data.content)
            const stepConfigs = normalizeStepConfigFile(rawStepConfig)
            planData = mergeStepConfigs(planData, stepConfigs)
            console.log('[WorkflowPlanUpdate] Merged step_config.json:', { stepConfigCount: stepConfigs.length })
          }
        } catch {
          // step_config.json doesn't exist or couldn't be loaded - that's okay
          console.log('[WorkflowPlanUpdate] No step_config.json found')
        }
      }

      // Also load step_override.json (global overrides)
      if (workspacePath) {
        try {
          const overrideResponse = await agentApi.getStepOverride(workspacePath)
          if (overrideResponse.success && overrideResponse.data?.agent_configs) {
            setStepOverride(overrideResponse.data.agent_configs)
            console.log('[WorkflowPlanUpdate] Loaded step_override.json')
          } else {
            setStepOverride(null)
          }
        } catch {
          // step_override.json doesn't exist - that's okay
          setStepOverride(null)
          console.log('[WorkflowPlanUpdate] No step_override.json found')
        }
      }

      return planData
    }

    return null
  }, [getPlanFilePath, getStepConfigFilePath])

  // Load plan from workspace with caching to prevent duplicate API calls
  const loadPlan = useCallback(async (): Promise<void> => {
    if (!workspacePath) {
      setError('No workspace path provided')
      return
    }

    const now = Date.now()

    // Check if we have a valid cached result for this workspace
    if (
      planCache.workspacePath === workspacePath &&
      planCache.data !== null &&
      (now - planCache.timestamp) < CACHE_EXPIRY_MS
    ) {
      console.log('[WorkflowPlanUpdate] Using cached plan data')
      setPlan(planCache.data)
      return
    }

    // Check if a load is already in progress for this workspace
    if (planCache.workspacePath === workspacePath && planCache.promise) {
      console.log('[WorkflowPlanUpdate] Load already in progress, waiting...')
      try {
        const data = await planCache.promise
        setPlan(data)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load plan')
      }
      return
    }

    // Check if workspace changed
    const isWorkspaceChange = currentWorkspaceRef.current !== workspacePath
    if (isWorkspaceChange) {
      currentWorkspaceRef.current = workspacePath
      // Clear cache for different workspace
      planCache.workspacePath = workspacePath
      planCache.data = null
      planCache.promise = null
      console.log('[WorkflowPlanUpdate] Workspace changed, loading plan')
    } else {
      console.log('[WorkflowPlanUpdate] loadPlan called for same workspace')
    }

    setLoading(true)
    setError(null)

    // Create the promise and cache it
    const loadPromise = fetchPlanData()
    planCache.promise = loadPromise
    planCache.workspacePath = workspacePath

    try {
      const planData = await loadPromise

      // Update cache
      planCache.data = planData
      planCache.timestamp = Date.now()
      planCache.promise = null

      setPlan(planData)
      if (!planData) {
        console.log('[usePlanData] No plan found')
      }
    } catch (err) {
      planCache.promise = null

      // Check if it's a 404 or "not found" error (plan doesn't exist yet)
      const is404 = err && typeof err === 'object' && 'response' in err &&
        (err as { response?: { status?: number } }).response?.status === 404
      const httpStatus = err && typeof err === 'object' && 'response' in err
        ? (err as { response?: { status?: number } }).response?.status
        : undefined
      const errMsg = err instanceof Error ? err.message : String(err)
      const isNotFound = is404 || /not found|does not exist|no such file/i.test(errMsg)

      console.log('[WORKFLOW_BUILDER] usePlanData catch:', {
        is404,
        httpStatus,
        errMsg,
        isNotFound,
        errType: typeof err,
        errKeys: err && typeof err === 'object' ? Object.keys(err) : [],
      })

      if (isNotFound) {
        setPlan(null)
        console.log('[WORKFLOW_BUILDER] usePlanData: Plan not found, treating as empty plan state')
        return
      }

      console.error('[WORKFLOW_BUILDER] usePlanData: Setting error state:', errMsg)
      setError(errMsg || 'Failed to load plan')
      return
    } finally {
      setLoading(false)
    }
  }, [workspacePath, fetchPlanData])

  /**
   * LEGACY METHOD: Direct file save for plan.json
   * 
   * ⚠️ WARNING: This method bypasses backend validation and should be used sparingly.
   * 
   * ✅ USE THIS METHOD FOR:
   *   - Initial plan creation (when creating a brand new plan.json file)
   *   - One-time migrations or bulk imports
   *   - Edge cases where backend API is unavailable
   * 
   * ❌ DO NOT USE THIS METHOD FOR:
   *   - Normal step updates (use updateStep() instead, which calls backend APIs)
   *   - Step configuration changes (use updateStepConfig API instead)
   *   - Any operation that should be validated by the backend
   * 
   * The backend APIs (updatePlanStep, updateStepConfig, batchUpdateSteps, etc.) provide:
   *   - Automatic validation
   *   - Consistent data structure
   *   - Prevention of agent_configs in plan.json
   *   - Atomic operations
   * 
   * @deprecated Prefer using backend APIs (updateStep, deleteStep, addStep) for normal operations
   */
  // Helper to invalidate the plan cache (call after mutations)
  const invalidatePlanCache = useCallback(() => {
    planCache.data = null
    planCache.timestamp = 0
    planCache.promise = null
  }, [])

  const savePlan = useCallback(async (updatedPlan: PlanningResponse) => {
    const planPath = getPlanFilePath()
    if (!planPath) {
      throw new Error('No workspace path provided')
    }

    try {
      const content = JSON.stringify(updatedPlan, null, 2)
      const response = await agentApi.updatePlannerFile(
        planPath,
        content,
        'Updated plan via workflow canvas'
      )

      if (!response.success) {
        throw new Error(response.message || 'Failed to save plan')
      }

      // Update local state and cache
      setPlan(updatedPlan)
      planCache.data = updatedPlan
      planCache.timestamp = Date.now()
    } catch (err) {
      console.error('[usePlanData] Failed to save plan:', err)
      throw err
    }
  }, [getPlanFilePath])

  /**
   * LEGACY METHOD: Direct file save for step_config.json
   * 
   * ⚠️ WARNING: This method bypasses backend validation and should be used sparingly.
   * 
   * ✅ USE THIS METHOD FOR:
   *   - Initial step config creation (when setting up a new workspace)
   *   - One-time migrations or bulk imports
   *   - Edge cases where backend API is unavailable
   *   - TodoStepsExtractedEvent handler (if needed for backward compatibility)
   * 
   * ❌ DO NOT USE THIS METHOD FOR:
   *   - Normal step config updates (use updateStepConfig API instead)
   *   - Updates from WorkflowCanvas or StepSidebar (use updateStep() instead)
   *   - Bulk operations (use batchUpdateSteps API instead)
   *   - Any operation that should be validated by the backend
   * 
   * The backend API (updateStepConfig) provides:
   *   - Automatic validation
   *   - Consistent data structure
   *   - Prevention of orphaned configs
   *   - Atomic operations with plan.json updates
   * 
   * @deprecated Prefer using agentApi.updateStepConfig() for normal operations
   */
  const saveStepConfig = useCallback(async (stepId: string, agentConfigs: AgentConfigs | undefined) => {
    const stepConfigPath = getStepConfigFilePath()
    if (!stepConfigPath) {
      throw new Error('No workspace path provided')
    }

    try {
      // Read existing step_config.json (or create empty if doesn't exist)
      let stepConfigs: StepConfig[] = []
      try {
        const response = await agentApi.getPlannerFileContent(stepConfigPath)
        // Check if response has data AND content exists and is a string
        if (response.success && response.data && response.data.content && typeof response.data.content === 'string') {
          const rawContent = JSON.parse(response.data.content)
          // Normalize to array format
          stepConfigs = normalizeStepConfigFile(rawContent)
        }
      } catch {
        // File doesn't exist yet - use empty array
        console.log('[usePlanData] step_config.json doesn\'t exist yet, creating new file')
      }

      // Find existing step config or create new one
      const existingIndex = stepConfigs.findIndex(s => s.id === stepId)
      
      if (agentConfigs) {
        // Update or add step config
        const stepConfig: StepConfig = {
          id: stepId,
          agent_configs: agentConfigs
        }
        
        if (existingIndex >= 0) {
          // Update existing
          stepConfigs[existingIndex] = {
            ...stepConfigs[existingIndex],
            ...stepConfig
          }
        } else {
          // Add new
          stepConfigs.push(stepConfig)
        }
      } else {
        // If agentConfigs is undefined/null, remove the step config
        if (existingIndex >= 0) {
          stepConfigs.splice(existingIndex, 1)
        }
      }

      // Save updated step_config.json in object format: { "steps": [...] }
      const content = JSON.stringify({ steps: stepConfigs }, null, 2)
      const response = await agentApi.updatePlannerFile(
        stepConfigPath,
        content,
        `Updated step config for step ${stepId}`
      )

      if (!response.success) {
        throw new Error(response.message || 'Failed to save step config')
      }

      console.log('[usePlanData] Saved step_config.json for step:', stepId)
    } catch (err) {
      console.error('[usePlanData] Failed to save step config:', err)
      throw err
    }
  }, [getStepConfigFilePath])

  /**
   * LEGACY METHOD: Batch direct file save for step_config.json
   * 
   * ⚠️ WARNING: This method bypasses backend validation and should be used sparingly.
   * 
   * ✅ USE THIS METHOD FOR:
   *   - Initial bulk step config creation (when setting up a new workspace)
   *   - One-time migrations or bulk imports
   *   - Edge cases where backend API is unavailable
   * 
   * ❌ DO NOT USE THIS METHOD FOR:
   *   - Normal bulk operations (use batchUpdateSteps API instead)
   *   - BulkStepConfigModal operations (now uses batchUpdateSteps API)
   *   - Any operation that should be validated by the backend
   * 
   * The backend API (batchUpdateSteps) provides:
   *   - Automatic validation
   *   - Consistent data structure
   *   - Prevention of orphaned configs
   *   - Atomic operations with plan.json updates
   *   - Better error handling and rollback
   * 
   * @deprecated Prefer using agentApi.batchUpdateSteps() for normal operations
   */
  const batchSaveStepConfig = useCallback(async (stepConfigs: Array<{ stepId: string; agentConfigs: AgentConfigs | undefined }>) => {
    const stepConfigPath = getStepConfigFilePath()
    if (!stepConfigPath) {
      throw new Error('No workspace path provided')
    }

    try {
      // Read existing step_config.json (or create empty if doesn't exist)
      let existingConfigs: StepConfig[] = []
      try {
        const response = await agentApi.getPlannerFileContent(stepConfigPath)
        if (response.success && response.data && response.data.content && typeof response.data.content === 'string') {
          const rawContent = JSON.parse(response.data.content)
          existingConfigs = normalizeStepConfigFile(rawContent)
        }
      } catch {
        console.log('[usePlanData] step_config.json doesn\'t exist yet, creating new file')
      }

      // Create a map of existing configs by step ID
      const configMap = new Map<string, StepConfig>()
      existingConfigs.forEach(config => {
        if (config.id) {
          configMap.set(config.id, config)
        }
      })

      // Update or add each step config
      stepConfigs.forEach(({ stepId, agentConfigs }) => {
        if (agentConfigs) {
          // Update or add step config
          const existingConfig = configMap.get(stepId)
          if (existingConfig) {
            // Merge with existing config
            configMap.set(stepId, {
              ...existingConfig,
              agent_configs: {
                ...existingConfig.agent_configs,
                ...agentConfigs
              }
            })
          } else {
            // Add new config
            configMap.set(stepId, {
              id: stepId,
              agent_configs: agentConfigs
            })
          }
        } else {
          // Remove config if agentConfigs is undefined
          configMap.delete(stepId)
        }
      })

      // Convert map back to array
      const updatedConfigs = Array.from(configMap.values())

      // Save updated step_config.json in object format: { "steps": [...] }
      const content = JSON.stringify({ steps: updatedConfigs }, null, 2)
      const response = await agentApi.updatePlannerFile(
        stepConfigPath,
        content,
        `Batch updated step configs for ${stepConfigs.length} step(s)`
      )

      if (!response.success) {
        throw new Error(response.message || 'Failed to batch save step configs')
      }

      console.log('[usePlanData] Batch saved step_config.json for', stepConfigs.length, 'step(s)')
    } catch (err) {
      console.error('[usePlanData] Failed to batch save step configs:', err)
      throw err
    }
  }, [getStepConfigFilePath])

  // Update a single step
  const updateStep = useCallback(async (stepIndex: number, updates: Partial<PlanStep>) => {
    if (!plan || !workspacePath) {
      throw new Error('No plan loaded or workspace path missing')
    }

    const updatedSteps = [...plan.steps]
    if (stepIndex < 0 || stepIndex >= updatedSteps.length) {
      throw new Error('Invalid step index')
    }

    const stepId = updatedSteps[stepIndex].id
    if (!stepId) {
      throw new Error('Step is missing required ID field')
    }

    // Separate plan updates and config updates
    const { agent_configs, ...planUpdates } = updates

    // Send update instructions to backend
    const promises: Promise<{ success: boolean; message: string; data?: unknown }>[] = []

    // Update plan if there are plan-related fields
    if (Object.keys(planUpdates).length > 0) {
      promises.push(
        agentApi.updatePlanStep(workspacePath, stepId, planUpdates)
      )
    }

    // Update config if agent_configs is provided
    if (agent_configs !== undefined) {
      promises.push(
        agentApi.updateStepConfig(workspacePath, stepId, agent_configs)
      )
    }

    // Wait for all updates to complete
    await Promise.all(promises)

    // Refresh plan from backend
    await loadPlan()
  }, [plan, workspacePath, loadPlan])

  // Delete a step
  const deleteStep = useCallback(async (stepIndex: number) => {
    if (!plan || !workspacePath) {
      throw new Error('No plan loaded or workspace path missing')
    }

    const updatedSteps = [...plan.steps]
    if (stepIndex < 0 || stepIndex >= updatedSteps.length) {
      throw new Error('Invalid step index')
    }

    const stepId = updatedSteps[stepIndex].id
    if (!stepId) {
      throw new Error('Step is missing required ID field')
    }

    // Call backend API to delete step
    await agentApi.deleteStep(workspacePath, stepId)

    // Refresh plan from backend
    await loadPlan()
  }, [plan, workspacePath, loadPlan])

  // Add a new step
  const addStep = useCallback(async (step: PlanStep, afterIndex?: number) => {
    if (!plan || !workspacePath) {
      throw new Error('No plan loaded or workspace path missing')
    }

    // Remove agent_configs from step if present (validation)
    const { agent_configs, ...stepWithoutConfig } = step

    // Determine insert_after_step_id if afterIndex is provided
    let insertAfterStepId: string | undefined
    if (afterIndex !== undefined && afterIndex >= 0 && afterIndex < plan.steps.length) {
      insertAfterStepId = plan.steps[afterIndex].id
    }

    // Call backend API to add step
    await agentApi.addStep(workspacePath, stepWithoutConfig, {
      insertAfterStepId: insertAfterStepId
    })

    // If agent_configs was provided, update config separately
    if (agent_configs !== undefined) {
      await agentApi.updateStepConfig(workspacePath, step.id, agent_configs)
    }

    // Refresh plan from backend
    await loadPlan()
  }, [plan, workspacePath, loadPlan])

  // Save global step override
  const saveStepOverride = useCallback(async (agentConfigs: AgentConfigs | null) => {
    if (!workspacePath) {
      throw new Error('No workspace path provided')
    }

    try {
      const result = await agentApi.updateStepOverride(workspacePath, agentConfigs)
      if (!result.success) {
        throw new Error(result.message || 'Failed to save step override')
      }

      // Update local state
      setStepOverride(agentConfigs)
      console.log('[usePlanData] Saved step_override.json:', agentConfigs ? 'updated' : 'cleared')
    } catch (err) {
      console.error('[usePlanData] Failed to save step override:', err)
      throw err
    }
  }, [workspacePath])

  // Refresh plan (returns detected changes)
  // Refresh function that invalidates cache and reloads
  const refresh = useCallback(async () => {
    invalidatePlanCache()
    await loadPlan()
  }, [loadPlan, invalidatePlanCache])

  // Clear changes state (call after highlighting animation completes)
  const clearChanges = useCallback(() => {
    setChanges(null)
  }, [])

  // Auto-load plan when workspace path changes
  // Note: loadPlan has internal caching that prevents duplicate API calls,
  // so we don't need to re-run this effect when loadPlan is recreated
  useEffect(() => {
    if (workspacePath) {
      loadPlan()
    } else {
      // Reset everything when no workspace
      setPlan(null)
      currentWorkspaceRef.current = null
      setError(null)
      setChanges(null)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [workspacePath]) // Only re-run when workspace changes, not when loadPlan is recreated

  // Listen for external plan modification signals (e.g., from LLM tool calls via SSE)
  useEffect(() => {
    if (!workspacePath) return
    const handler = () => {
      console.log('[usePlanData] plan-modified event received, refreshing...')
      invalidatePlanCache()
      loadPlan()
    }
    window.addEventListener('plan-modified', handler)
    return () => window.removeEventListener('plan-modified', handler)
  }, [workspacePath, loadPlan, invalidatePlanCache])

  return {
    plan,
    loading,
    error,
    changes,
    stepOverride,
    loadPlan,
    savePlan,
    saveStepConfig,
    batchSaveStepConfig,
    updateStep,
    deleteStep,
    addStep,
    saveStepOverride,
    refresh,
    clearChanges,
    setChanges
  }
}

export default usePlanData

