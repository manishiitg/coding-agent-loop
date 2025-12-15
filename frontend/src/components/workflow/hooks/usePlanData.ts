import { useState, useCallback, useEffect, useRef } from 'react'
import { agentApi } from '../../../services/api'
import type { PlanStep, PlanningResponse, StepConfig, AgentConfigs } from '../../../utils/stepConfigMatching'

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
  loadPlan: () => Promise<void>  // Loads plan without comparison
  savePlan: (plan: PlanningResponse) => Promise<void>
  saveStepConfig: (stepId: string, agentConfigs: AgentConfigs | undefined) => Promise<void>
  updateStep: (stepIndex: number, updates: Partial<PlanStep>) => Promise<void>
  deleteStep: (stepIndex: number) => Promise<void>
  addStep: (step: PlanStep, afterIndex?: number) => Promise<void>
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

  // Recursively merge configs into steps
  const mergeIntoSteps = (steps: PlanStep[]): PlanStep[] => {
    return steps.map(step => {
      const config = step.id ? configMap.get(step.id) : undefined
      const mergedStep: PlanStep = {
        ...step,
        ...(config ? { agent_configs: config } : {})
      }
      
      // Handle nested branch steps
      if (step.if_true_steps) {
        mergedStep.if_true_steps = mergeIntoSteps(step.if_true_steps)
      }
      if (step.if_false_steps) {
        mergedStep.if_false_steps = mergeIntoSteps(step.if_false_steps)
      }
      
      return mergedStep
    })
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

  // Load plan from workspace (no comparison - changes come from granular events)
  const loadPlan = useCallback(async (): Promise<void> => {
    const planPath = getPlanFilePath()
    const stepConfigPath = getStepConfigFilePath()
    if (!planPath) {
      setError('No workspace path provided')
      return
    }

    console.log('[WorkflowPlanUpdate] loadPlan called', { planPath, stepConfigPath, workspacePath })

    // Check if workspace changed
    const isWorkspaceChange = currentWorkspaceRef.current !== workspacePath
    if (isWorkspaceChange) {
      currentWorkspaceRef.current = workspacePath
      console.log('[WorkflowPlanUpdate] Workspace changed')
    }

    setLoading(true)
    setError(null)

    try {
      console.log('[WorkflowPlanUpdate] Fetching plan from:', planPath)
      const response = await agentApi.getPlannerFileContent(planPath)
      console.log('[WorkflowPlanUpdate] Plan fetch response:', { success: response.success, hasData: !!response.data, hasContent: !!response.data?.content })
      
      // Check if response has data AND content exists and is a string
      if (response.success && response.data && response.data.content && typeof response.data.content === 'string') {
        let planData: PlanningResponse = JSON.parse(response.data.content)
        console.log('[WorkflowPlanUpdate] Plan loaded:', { stepCount: planData.steps?.length || 0, hasSteps: !!planData.steps })
        
        // Also load step_config.json to merge agent_configs
        if (stepConfigPath) {
          try {
            console.log('[WorkflowPlanUpdate] Fetching step_config from:', stepConfigPath)
            const stepConfigResponse = await agentApi.getPlannerFileContent(stepConfigPath)
            console.log('[StepConfigDebug] ⚠️ CRITICAL: step_config.json API response:', {
              success: stepConfigResponse.success,
              hasData: !!stepConfigResponse.data,
              hasContent: !!stepConfigResponse.data?.content,
              dataKeys: stepConfigResponse.data ? Object.keys(stepConfigResponse.data) : [],
              responseStructure: stepConfigResponse
            })
            // Check if step_config has content before parsing
            if (stepConfigResponse.success && stepConfigResponse.data && stepConfigResponse.data.content && typeof stepConfigResponse.data.content === 'string') {
              console.log('[StepConfigDebug] ⚠️ CRITICAL: Raw file content BEFORE parsing:', {
                contentLength: stepConfigResponse.data.content.length,
                contentPreview: stepConfigResponse.data.content.substring(0, 500),
                fullContent: stepConfigResponse.data.content
              })
              const rawStepConfig = JSON.parse(stepConfigResponse.data.content)
              console.log('[StepConfigDebug] ⚠️ CRITICAL: Raw step_config.json structure AFTER parsing:', {
                hasSteps: !Array.isArray(rawStepConfig) && 'steps' in rawStepConfig,
                isArray: Array.isArray(rawStepConfig),
                keys: Array.isArray(rawStepConfig) ? `Array[${rawStepConfig.length}]` : Object.keys(rawStepConfig),
                firstItem: Array.isArray(rawStepConfig) && rawStepConfig.length > 0 ? rawStepConfig[0] : null,
                rawContent: rawStepConfig
              })
              
              // Normalize from object format: { "steps": [...] }
              const stepConfigs = normalizeStepConfigFile(rawStepConfig)
              console.log('[StepConfigDebug] Normalized step_config.json:', {
                stepCount: stepConfigs.length,
                format: 'object with steps field',
                steps: stepConfigs.map(s => ({
                  id: s.id,
                  hasAgentConfigs: !!s.agent_configs,
                  selectedTools: s.agent_configs?.selected_tools
                }))
              })
              
              // Merge agent_configs into plan steps
              planData = mergeStepConfigs(planData, stepConfigs)
              console.log('[StepConfigDebug] Merged step_config.json into plan', {
                stepConfigCount: stepConfigs.length,
                mergedSteps: planData.steps.map(s => ({
                  id: s.id,
                  hasAgentConfigs: !!s.agent_configs,
                  selectedTools: s.agent_configs?.selected_tools,
                  selectedServers: s.agent_configs?.selected_servers
                }))
              })
            }
          } catch (err) {
            // step_config.json doesn't exist or couldn't be loaded - that's okay
            console.log('[WorkflowPlanUpdate] No step_config.json found, using plan without agent configs', err)
          }
        }
        
        // Update state (no comparison - changes come from granular events via setChanges)
        console.log('[WorkflowPlanUpdate] Updating plan state')
        setPlan(planData)
      } else {
        // Plan doesn't exist yet - that's okay
        setPlan(null)
        console.log('[usePlanData] No plan found at:', planPath)
      }
      return
    } catch (err) {
      // Check if it's a 404 (plan doesn't exist)
      if (err && typeof err === 'object' && 'response' in err) {
        const axiosError = err as { response?: { status?: number } }
        if (axiosError.response?.status === 404) {
          // Plan doesn't exist yet - not an error
          setPlan(null)
          console.log('[usePlanData] Plan not found (404):', planPath)
          return
        }
      }
      
      console.error('[usePlanData] Failed to load plan:', err)
      setError(err instanceof Error ? err.message : 'Failed to load plan')
      return
    } finally {
      setLoading(false)
    }
  }, [getPlanFilePath, getStepConfigFilePath, workspacePath])

  // Save entire plan to workspace
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

      setPlan(updatedPlan)
    } catch (err) {
      console.error('[usePlanData] Failed to save plan:', err)
      throw err
    }
  }, [getPlanFilePath])

  // Save step_config.json to workspace
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

  // Helper function to check if updates contain plan-related fields (excluding agent_configs)
  const hasPlanRelatedFields = useCallback((updates: Partial<PlanStep>): boolean => {
    // List of all plan-related fields (everything except agent_configs)
    const planFields = [
      'id', 'title', 'description', 'success_criteria', 'context_dependencies', 'context_output',
      'has_loop', 'loop_condition', 'max_iterations', 'loop_description',
      'has_condition', 'condition_question', 'condition_context',
      'if_true_steps', 'if_false_steps', 'if_true_next_step_id', 'if_false_next_step_id',
      'condition_result', 'condition_reason',
      'has_decision_step', 'decision_step', 'decision_evaluation_question',
      'decision_result', 'decision_reason'
    ]
    
    // Check if any plan-related field is in updates
    return planFields.some(field => field in updates)
  }, [])

  // Update a single step
  const updateStep = useCallback(async (stepIndex: number, updates: Partial<PlanStep>) => {
    if (!plan) {
      throw new Error('No plan loaded')
    }

    const updatedSteps = [...plan.steps]
    if (stepIndex < 0 || stepIndex >= updatedSteps.length) {
      throw new Error('Invalid step index')
    }

    const stepId = updatedSteps[stepIndex].id
    updatedSteps[stepIndex] = {
      ...updatedSteps[stepIndex],
      ...updates
    }

    // Only save plan.json if updates contain plan-related fields (not just agent_configs)
    const shouldSavePlan = hasPlanRelatedFields(updates)
    if (shouldSavePlan) {
      const updatedPlan: PlanningResponse = {
        ...plan,
        steps: updatedSteps
      }
      await savePlan(updatedPlan)
    }

    // If agent_configs is in updates, save to step_config.json
    if ('agent_configs' in updates) {
      await saveStepConfig(stepId, updates.agent_configs)
    }
  }, [plan, savePlan, saveStepConfig, hasPlanRelatedFields])

  // Delete a step
  const deleteStep = useCallback(async (stepIndex: number) => {
    if (!plan) {
      throw new Error('No plan loaded')
    }

    const updatedSteps = [...plan.steps]
    if (stepIndex < 0 || stepIndex >= updatedSteps.length) {
      throw new Error('Invalid step index')
    }

    // Get the deleted step's context_output to clean up dependencies
    const deletedStep = updatedSteps[stepIndex]
    const deletedContextOutput = deletedStep.context_output

    // Remove the step
    updatedSteps.splice(stepIndex, 1)

    // Clean up context_dependencies in remaining steps
    if (deletedContextOutput) {
      const outputToRemove = Array.isArray(deletedContextOutput) 
        ? deletedContextOutput 
        : [deletedContextOutput]
      
      updatedSteps.forEach(step => {
        if (step.context_dependencies) {
          step.context_dependencies = step.context_dependencies.filter(
            dep => !outputToRemove.includes(dep)
          )
        }
      })
    }

    const updatedPlan: PlanningResponse = {
      ...plan,
      steps: updatedSteps
    }

    await savePlan(updatedPlan)
  }, [plan, savePlan])

  // Add a new step
  const addStep = useCallback(async (step: PlanStep, afterIndex?: number) => {
    if (!plan) {
      throw new Error('No plan loaded')
    }

    const updatedSteps = [...plan.steps]
    
    if (afterIndex !== undefined && afterIndex >= 0) {
      // Insert after the specified index
      updatedSteps.splice(afterIndex + 1, 0, step)
    } else {
      // Add to the end
      updatedSteps.push(step)
    }

    const updatedPlan: PlanningResponse = {
      ...plan,
      steps: updatedSteps
    }

    await savePlan(updatedPlan)
  }, [plan, savePlan])

  // Refresh plan (returns detected changes)
  const refresh = loadPlan

  // Clear changes state (call after highlighting animation completes)
  const clearChanges = useCallback(() => {
    setChanges(null)
  }, [])

  // Auto-load plan when workspace path changes
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
  }, [workspacePath, loadPlan])

  return {
    plan,
    loading,
    error,
    changes,
    loadPlan,
    savePlan,
    saveStepConfig,
    updateStep,
    deleteStep,
    addStep,
    refresh,
    clearChanges,
    setChanges
  }
}

export default usePlanData

