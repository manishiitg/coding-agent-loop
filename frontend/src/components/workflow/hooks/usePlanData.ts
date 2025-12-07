import { useState, useCallback, useEffect, useRef } from 'react'
import { agentApi } from '../../../services/api'
import type { PlanStep, PlanningResponse, StepConfigFile, StepConfig, AgentConfigs } from '../../../utils/stepConfigMatching'

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
  changes: PlanChanges | null  // Latest detected changes
  loadPlan: () => Promise<PlanChanges | null>  // Returns detected changes
  savePlan: (plan: PlanningResponse) => Promise<void>
  updateStep: (stepIndex: number, updates: Partial<PlanStep>) => Promise<void>
  deleteStep: (stepIndex: number) => Promise<void>
  addStep: (step: PlanStep, afterIndex?: number) => Promise<void>
  refresh: () => Promise<PlanChanges | null>  // Returns detected changes (alias for loadPlan)
  clearChanges: () => void  // Clear the changes state
}

/**
 * Compare two plans to detect changes (added, updated, deleted steps)
 */
function detectPlanChanges(
  oldPlan: PlanningResponse | null,
  newPlan: PlanningResponse | null
): PlanChanges {
  const changes: PlanChanges = {
    added: [],
    updated: [],
    deleted: [],
    hasChanges: false
  }

  if (!newPlan?.steps) {
    // If new plan has no steps but old plan did, all are deleted
    if (oldPlan?.steps) {
      changes.deleted = collectAllStepIds(oldPlan.steps)
      changes.hasChanges = changes.deleted.length > 0
    }
    return changes
  }

  if (!oldPlan?.steps) {
    // If old plan had no steps, all new steps are added
    changes.added = collectAllStepIds(newPlan.steps)
    changes.hasChanges = changes.added.length > 0
    return changes
  }

  // Create maps of step IDs to step data for comparison
  const oldStepsMap = createStepMap(oldPlan.steps)
  const newStepsMap = createStepMap(newPlan.steps)

  // Find added and updated steps
  for (const [id, newStep] of newStepsMap) {
    const oldStep = oldStepsMap.get(id)
    if (!oldStep) {
      changes.added.push(id)
    } else if (hasStepChanged(oldStep, newStep)) {
      changes.updated.push(id)
    }
  }

  // Find deleted steps
  for (const id of oldStepsMap.keys()) {
    if (!newStepsMap.has(id)) {
      changes.deleted.push(id)
    }
  }

  changes.hasChanges = changes.added.length > 0 || 
                       changes.updated.length > 0 || 
                       changes.deleted.length > 0

  return changes
}

/**
 * Collect all step IDs from a plan (including nested branch steps)
 */
function collectAllStepIds(steps: PlanStep[]): string[] {
  const ids: string[] = []
  
  const collect = (stepList: PlanStep[]) => {
    for (const step of stepList) {
      if (step.id) ids.push(step.id)
      if (step.if_true_steps) collect(step.if_true_steps)
      if (step.if_false_steps) collect(step.if_false_steps)
    }
  }
  
  collect(steps)
  return ids
}

/**
 * Create a map of step ID -> step data (including nested steps)
 */
function createStepMap(steps: PlanStep[]): Map<string, PlanStep> {
  const map = new Map<string, PlanStep>()
  
  const addToMap = (stepList: PlanStep[]) => {
    for (const step of stepList) {
      if (step.id) map.set(step.id, step)
      if (step.if_true_steps) addToMap(step.if_true_steps)
      if (step.if_false_steps) addToMap(step.if_false_steps)
    }
  }
  
  addToMap(steps)
  return map
}

/**
 * Check if a step has been meaningfully changed
 */
function hasStepChanged(oldStep: PlanStep, newStep: PlanStep): boolean {
  // Compare key fields that would indicate a change
  return (
    oldStep.title !== newStep.title ||
    oldStep.description !== newStep.description ||
    oldStep.success_criteria !== newStep.success_criteria ||
    oldStep.has_condition !== newStep.has_condition ||
    oldStep.has_loop !== newStep.has_loop ||
    oldStep.loop_condition !== newStep.loop_condition ||
    oldStep.max_iterations !== newStep.max_iterations ||
    oldStep.condition_question !== newStep.condition_question ||
    JSON.stringify(oldStep.context_dependencies) !== JSON.stringify(newStep.context_dependencies) ||
    JSON.stringify(oldStep.context_output) !== JSON.stringify(newStep.context_output) ||
    // Also detect agent_configs changes (step_config.json)
    JSON.stringify(oldStep.agent_configs) !== JSON.stringify(newStep.agent_configs)
  )
}

/**
 * Normalize step_config.json content to canonical format
 * Handles multiple input formats and converts to: { steps: [{ id, agent_configs: {...} }] }
 */
function normalizeStepConfigFile(rawContent: unknown): StepConfigFile {
  // If already in canonical format
  if (rawContent && typeof rawContent === 'object' && 'steps' in rawContent && Array.isArray((rawContent as StepConfigFile).steps)) {
    return rawContent as StepConfigFile
  }

  // If array format: [{ step_id, selected_servers, ... }, ...] OR [{ id, agent_configs, ... }, ...]
  if (Array.isArray(rawContent)) {
    const isFlatArray = rawContent.length > 0 && 
      (rawContent[0] && typeof rawContent[0] === 'object' && 
       (('step_id' in rawContent[0]) || ('id' in rawContent[0])) &&
       (('selected_servers' in rawContent[0]) || ('selected_tools' in rawContent[0])))
    
    if (isFlatArray) {
      // Convert flat array format to canonical format
      const convertedSteps: StepConfig[] = (rawContent as Array<Record<string, unknown>>)
        .map((item) => {
          const stepId = (item.step_id || item.id) as string | undefined
          if (!stepId) {
            console.warn('[normalizeStepConfigFile] Array item missing step_id/id:', item)
            return null
          }
          
          // Convert flat structure to nested agent_configs
          const agentConfigs: AgentConfigs = {
            ...((item.agent_configs as AgentConfigs | undefined) || {}),
            selected_servers: item.selected_servers as string[] | undefined,
            selected_tools: item.selected_tools as string[] | undefined,
            enabled_custom_tools: item.enabled_custom_tools as string[] | undefined,
          }
          
          return {
            id: stepId,
            agent_configs: agentConfigs
          } as StepConfig
        })
        .filter((item): item is StepConfig => item !== null)
      
      return { steps: convertedSteps }
    } else {
      // Array format: [{ id, agent_configs: {...} }, ...]
      return { steps: rawContent as StepConfig[] }
    }
  }

  // If flat object format: { step_id, selected_servers, selected_tools, ... }
  if (rawContent && typeof rawContent === 'object' && !Array.isArray(rawContent)) {
    const flatConfig = rawContent as Record<string, unknown>
    const stepId = (flatConfig.step_id || flatConfig.id) as string | undefined
    
    if (stepId) {
      const agentConfigs: AgentConfigs = {
        ...((flatConfig.agent_configs as AgentConfigs | undefined) || {}),
        selected_servers: flatConfig.selected_servers as string[] | undefined,
        selected_tools: flatConfig.selected_tools as string[] | undefined,
        enabled_custom_tools: flatConfig.enabled_custom_tools as string[] | undefined,
      }
      
      return {
        steps: [{
          id: stepId,
          agent_configs: agentConfigs
        }]
      }
    }
  }

  // Default: empty structure
  return { steps: [] }
}

/**
 * Merge agent_configs from step_config.json into plan steps
 * This adds the agent_configs (including use_code_execution_mode) to each step
 */
function mergeStepConfigs(
  plan: PlanningResponse,
  stepConfigFile: StepConfigFile | null
): PlanningResponse {
  if (!stepConfigFile?.steps) return plan

  // Create map: step ID -> agent_configs
  const configMap = new Map<string, AgentConfigs>()
  for (const config of stepConfigFile.steps) {
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
  
  // Keep track of previous plan for change detection
  const previousPlanRef = useRef<PlanningResponse | null>(null)
  // Track workspace path to detect workflow switches (don't animate on switch)
  const currentWorkspaceRef = useRef<string | null>(null)
  // Track if this is the initial load for current workspace
  const isInitialLoadRef = useRef<boolean>(true)

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

  // Load plan from workspace (returns detected changes for refresh scenarios)
  const loadPlan = useCallback(async (): Promise<PlanChanges | null> => {
    const planPath = getPlanFilePath()
    const stepConfigPath = getStepConfigFilePath()
    if (!planPath) {
      setError('No workspace path provided')
      return null
    }

    console.log('[WorkflowPlanUpdate] loadPlan called', { planPath, stepConfigPath, workspacePath })

    // Check if workspace changed - if so, this is an initial load (no animations)
    const isWorkspaceChange = currentWorkspaceRef.current !== workspacePath
    if (isWorkspaceChange) {
      currentWorkspaceRef.current = workspacePath
      isInitialLoadRef.current = true
      previousPlanRef.current = null
      console.log('[WorkflowPlanUpdate] Workspace changed, resetting for initial load')
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
              
              // Normalize to canonical format (handles all input formats)
              const stepConfigFile = normalizeStepConfigFile(rawStepConfig)
              console.log('[StepConfigDebug] Normalized step_config.json:', {
                stepCount: stepConfigFile.steps.length,
                format: 'canonical',
                steps: stepConfigFile.steps.map(s => ({
                  id: s.id,
                  hasAgentConfigs: !!s.agent_configs,
                  selectedTools: s.agent_configs?.selected_tools
                }))
              })
              
              // Merge agent_configs into plan steps
              planData = mergeStepConfigs(planData, stepConfigFile)
              console.log('[StepConfigDebug] Merged step_config.json into plan', { 
                stepConfigCount: stepConfigFile.steps.length,
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
        
        // Only detect changes if NOT initial load (don't animate on workflow switch/first load)
        if (isInitialLoadRef.current) {
          console.log('[WorkflowPlanUpdate] Initial load - skipping change detection, setting plan')
          setPlan(planData)
          previousPlanRef.current = planData
          isInitialLoadRef.current = false
          return null
        }
        
        // Detect changes between previous and new plan
        console.log('[WorkflowPlanUpdate] Detecting changes between previous and new plan')
        console.log('[WorkflowPlanUpdate] Previous plan:', {
          hasPrevious: !!previousPlanRef.current,
          previousStepCount: previousPlanRef.current?.steps?.length || 0,
          previousStepIds: previousPlanRef.current?.steps?.map(s => s.id) || []
        })
        console.log('[WorkflowPlanUpdate] New plan:', {
          hasNew: !!planData,
          newStepCount: planData.steps?.length || 0,
          newStepIds: planData.steps?.map(s => s.id) || []
        })
        
        // Compare specific step that was updated (if we can identify it)
        if (previousPlanRef.current && planData.steps) {
          const updatedStepId = 'cleanup-data-and-delete-sensitive-data-from-workspace-files-yob6og'
          const prevStep = previousPlanRef.current.steps.find(s => s.id === updatedStepId)
          const newStep = planData.steps.find(s => s.id === updatedStepId)
          if (prevStep && newStep) {
            console.log('[WorkflowPlanUpdate] Comparing updated step:', {
              stepId: updatedStepId,
              prevSuccessCriteria: prevStep.success_criteria,
              newSuccessCriteria: newStep.success_criteria,
              successCriteriaChanged: prevStep.success_criteria !== newStep.success_criteria,
              prevStepStr: JSON.stringify(prevStep),
              newStepStr: JSON.stringify(newStep),
              stepChanged: JSON.stringify(prevStep) !== JSON.stringify(newStep)
            })
          }
        }
        
        const detectedChanges = detectPlanChanges(previousPlanRef.current, planData)
        console.log('[WorkflowPlanUpdate] Change detection result:', {
          hasChanges: detectedChanges.hasChanges,
          added: detectedChanges.added?.length || 0,
          updated: detectedChanges.updated?.length || 0,
          deleted: detectedChanges.deleted?.length || 0,
          addedIds: detectedChanges.added,
          updatedIds: detectedChanges.updated,
          deletedIds: detectedChanges.deleted
        })
        
        // Update state
        console.log('[WorkflowPlanUpdate] Updating plan state')
        setPlan(planData)
        previousPlanRef.current = planData
        
        if (detectedChanges.hasChanges) {
          setChanges(detectedChanges)
          console.log('[WorkflowPlanUpdate] Plan changes detected and set:', detectedChanges)
          return detectedChanges
        } else {
          console.log('[WorkflowPlanUpdate] No changes detected, plan is the same')
          // Even if no changes detected, return null to indicate refresh completed
          return null
        }
      } else {
        // Plan doesn't exist yet - that's okay
        if (isInitialLoadRef.current) {
          setPlan(null)
          previousPlanRef.current = null
          isInitialLoadRef.current = false
          console.log('[usePlanData] Initial load - no plan found at:', planPath)
          return null
        }
        
        const detectedChanges = detectPlanChanges(previousPlanRef.current, null)
        setPlan(null)
        previousPlanRef.current = null
        console.log('[usePlanData] No plan found at:', planPath)
        
        if (detectedChanges.hasChanges) {
          setChanges(detectedChanges)
          return detectedChanges
        }
      }
      return null
    } catch (err) {
      // Check if it's a 404 (plan doesn't exist)
      if (err && typeof err === 'object' && 'response' in err) {
        const axiosError = err as { response?: { status?: number } }
        if (axiosError.response?.status === 404) {
          // Plan doesn't exist yet - not an error
          if (isInitialLoadRef.current) {
            setPlan(null)
            previousPlanRef.current = null
            isInitialLoadRef.current = false
            console.log('[usePlanData] Initial load - plan not found (404):', planPath)
            return null
          }
          
          const detectedChanges = detectPlanChanges(previousPlanRef.current, null)
          setPlan(null)
          previousPlanRef.current = null
          console.log('[usePlanData] Plan not found (404):', planPath)
          if (detectedChanges.hasChanges) {
            setChanges(detectedChanges)
            return detectedChanges
          }
          return null
        }
      }
      
      console.error('[usePlanData] Failed to load plan:', err)
      setError(err instanceof Error ? err.message : 'Failed to load plan')
      return null
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
      let stepConfigFile: StepConfigFile = { steps: [] }
      try {
        const response = await agentApi.getPlannerFileContent(stepConfigPath)
        // Check if response has data AND content exists and is a string
        if (response.success && response.data && response.data.content && typeof response.data.content === 'string') {
          const rawContent = JSON.parse(response.data.content)
          // Normalize to canonical format (handles array, flat, or canonical formats)
          stepConfigFile = normalizeStepConfigFile(rawContent)
        }
      } catch {
        // File doesn't exist yet - use empty structure
        console.log('[usePlanData] step_config.json doesn\'t exist yet, creating new file')
      }

      // Find existing step config or create new one
      const existingIndex = stepConfigFile.steps.findIndex(s => s.id === stepId)
      
      if (agentConfigs) {
        // Update or add step config
        const stepConfig: StepConfig = {
          id: stepId,
          agent_configs: agentConfigs
        }
        
        if (existingIndex >= 0) {
          // Update existing
          stepConfigFile.steps[existingIndex] = {
            ...stepConfigFile.steps[existingIndex],
            ...stepConfig
          }
        } else {
          // Add new
          stepConfigFile.steps.push(stepConfig)
        }
      } else {
        // If agentConfigs is undefined/null, remove the step config
        if (existingIndex >= 0) {
          stepConfigFile.steps.splice(existingIndex, 1)
        }
      }

      // Save updated step_config.json
      const content = JSON.stringify(stepConfigFile, null, 2)
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

    const updatedPlan: PlanningResponse = {
      ...plan,
      steps: updatedSteps
    }

    // Save plan.json
    await savePlan(updatedPlan)

    // If agent_configs is in updates, also save to step_config.json
    if ('agent_configs' in updates) {
      await saveStepConfig(stepId, updates.agent_configs)
    }
  }, [plan, savePlan, saveStepConfig])

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
      previousPlanRef.current = null
      currentWorkspaceRef.current = null
      isInitialLoadRef.current = true
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
    updateStep,
    deleteStep,
    addStep,
    refresh,
    clearChanges
  }
}

export default usePlanData

