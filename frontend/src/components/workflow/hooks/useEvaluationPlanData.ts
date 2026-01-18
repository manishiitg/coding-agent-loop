import { useState, useCallback, useEffect, useRef } from 'react'
import { agentApi } from '../../../services/api'
import type { EvaluationPlan, EvaluationStep } from '../../../services/api-types'
import type { AgentConfigs } from '../../../utils/stepConfigMatching'

// Module-level cache
interface EvalPlanCache {
  workspacePath: string | null
  promise: Promise<EvaluationPlan | null> | null
  data: EvaluationPlan | null
  timestamp: number
}

const planCache: EvalPlanCache = {
  workspacePath: null,
  promise: null,
  data: null,
  timestamp: 0
}

const CACHE_EXPIRY_MS = 5000

export interface UseEvaluationPlanDataReturn {
  evaluationPlan: EvaluationPlan | null
  loading: boolean
  error: string | null
  loadEvaluationPlan: () => Promise<void>
  saveEvaluationPlan: (plan: EvaluationPlan) => Promise<void>
  saveEvaluationStepConfig: (stepId: string, agentConfigs: AgentConfigs | undefined) => Promise<void>
  updateEvaluationStep: (stepIndex: number, updates: Partial<EvaluationStep>) => Promise<void>
  refresh: () => Promise<void>
}

// NOTE: Evaluation steps store config in evaluation/step_config.json
// The backend handles this path for evaluation steps automatically if we use the right APIs?
// Wait, agentApi methods usually take workspacePath and file paths.
// Let's check how updatePlanStep works. It modifies planning/plan.json.
// We probably need to implement manual file updates for evaluation plan since backend might not have specialized endpoints for eval plan steps yet,
// OR we can reuse the generic file APIs.
// The document says: "Loads from evaluation/evaluation_plan.json", "Loads step configs from evaluation/step_config.json"

export function useEvaluationPlanData(workspacePath: string | null): UseEvaluationPlanDataReturn {
  const [evaluationPlan, setEvaluationPlan] = useState<EvaluationPlan | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  
  const currentWorkspaceRef = useRef<string | null>(null)

  const getPlanFilePath = useCallback(() => {
    if (!workspacePath) return null
    return `${workspacePath}/evaluation/evaluation_plan.json`
  }, [workspacePath])

  const getStepConfigFilePath = useCallback(() => {
    if (!workspacePath) return null
    return `${workspacePath}/evaluation/step_config.json`
  }, [workspacePath])

  const fetchPlanData = useCallback(async (): Promise<EvaluationPlan | null> => {
    const planPath = getPlanFilePath()
    const stepConfigPath = getStepConfigFilePath()
    if (!planPath) {
      throw new Error('No workspace path provided')
    }

    console.log('[EvaluationPlan] Fetching plan from:', planPath)
    
    try {
      const response = await agentApi.getPlannerFileContent(planPath)
      
      if (response.success && response.data?.content) {
        let planData: EvaluationPlan = JSON.parse(response.data.content)
        
        // Load step configs and merge
        if (stepConfigPath) {
          try {
            const configResponse = await agentApi.getPlannerFileContent(stepConfigPath)
            if (configResponse.success && configResponse.data?.content) {
              const configContent = JSON.parse(configResponse.data.content)
              const configs = Array.isArray(configContent) ? configContent : (configContent.steps || [])
              
              // Merge configs into steps
              const configMap = new Map<string, AgentConfigs>()
              configs.forEach((c: { id: string, agent_configs: AgentConfigs }) => {
                if (c.id && c.agent_configs) configMap.set(c.id, c.agent_configs)
              })
              
              planData = {
                ...planData,
                steps: planData.steps.map(step => ({
                  ...step,
                  agent_configs: configMap.get(step.id) || step.agent_configs
                }))
              }
            }
          } catch (e) {
            console.log('[EvaluationPlan] No step_config.json found or failed to load', e)
          }
        }
        
        return planData
      }
    } catch (err) {
      // 404 is fine (no plan yet)
      console.log('[EvaluationPlan] Plan not found or error', err)
    }
    
    return null
  }, [getPlanFilePath, getStepConfigFilePath])

  const loadEvaluationPlan = useCallback(async () => {
    if (!workspacePath) {
      setEvaluationPlan(null)
      return
    }

    const now = Date.now()
    if (
      planCache.workspacePath === workspacePath &&
      planCache.data !== null &&
      (now - planCache.timestamp) < CACHE_EXPIRY_MS
    ) {
      setEvaluationPlan(planCache.data)
      return
    }

    if (planCache.workspacePath === workspacePath && planCache.promise) {
      try {
        const data = await planCache.promise
        setEvaluationPlan(data)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load evaluation plan')
      }
      return
    }

    if (currentWorkspaceRef.current !== workspacePath) {
      currentWorkspaceRef.current = workspacePath
      planCache.workspacePath = workspacePath
      planCache.data = null
      planCache.promise = null
    }

    setLoading(true)
    setError(null)

    const loadPromise = fetchPlanData()
    planCache.promise = loadPromise
    
    try {
      const data = await loadPromise
      planCache.data = data
      planCache.timestamp = Date.now()
      planCache.promise = null
      setEvaluationPlan(data)
    } catch (err) {
      planCache.promise = null
      setError(err instanceof Error ? err.message : 'Failed to load evaluation plan')
    } finally {
      setLoading(false)
    }
  }, [workspacePath, fetchPlanData])

  const saveEvaluationPlan = useCallback(async (updatedPlan: EvaluationPlan) => {
    const planPath = getPlanFilePath()
    if (!planPath) return

    try {
      // Strip agent_configs before saving to plan.json
      const planToSave = {
        ...updatedPlan,
        steps: updatedPlan.steps.map(step => {
          const { agent_configs, ...rest } = step
          return rest
        })
      }

      await agentApi.updatePlannerFile(
        planPath,
        JSON.stringify(planToSave, null, 2),
        'Update evaluation plan'
      )
      
      // Update cache and state
      setEvaluationPlan(updatedPlan)
      planCache.data = updatedPlan
      planCache.timestamp = Date.now()
    } catch (err) {
      console.error('[EvaluationPlan] Failed to save plan:', err)
      throw err
    }
  }, [getPlanFilePath])

  const saveEvaluationStepConfig = useCallback(async (stepId: string, agentConfigs: AgentConfigs | undefined) => {
    const configPath = getStepConfigFilePath()
    if (!configPath) return

    try {
      // Read existing configs
      let configs: { id: string, agent_configs: AgentConfigs }[] = []
      try {
        const response = await agentApi.getPlannerFileContent(configPath)
        if (response.success && response.data?.content) {
          const content = JSON.parse(response.data.content)
          configs = Array.isArray(content) ? content : (content.steps || [])
        }
      } catch {
        // Create new
      }

      // Update
      const index = configs.findIndex(c => c.id === stepId)
      if (agentConfigs) {
        if (index >= 0) {
          configs[index] = { id: stepId, agent_configs: agentConfigs }
        } else {
          configs.push({ id: stepId, agent_configs: agentConfigs })
        }
      } else if (index >= 0) {
        configs.splice(index, 1)
      }

      // Save
      await agentApi.updatePlannerFile(
        configPath,
        JSON.stringify({ steps: configs }, null, 2),
        `Update evaluation step config ${stepId}`
      )
      
      // Refresh
      await loadEvaluationPlan()
    } catch (err) {
      console.error('[EvaluationPlan] Failed to save step config:', err)
      throw err
    }
  }, [getStepConfigFilePath, loadEvaluationPlan])

  const updateEvaluationStep = useCallback(async (stepIndex: number, updates: Partial<EvaluationStep>) => {
    if (!evaluationPlan) return

    const updatedSteps = [...evaluationPlan.steps]
    if (stepIndex < 0 || stepIndex >= updatedSteps.length) return

    const step = updatedSteps[stepIndex]
    const { agent_configs, ...planUpdates } = updates

    // Update plan if needed
    if (Object.keys(planUpdates).length > 0) {
      updatedSteps[stepIndex] = { ...step, ...planUpdates }
      await saveEvaluationPlan({ ...evaluationPlan, steps: updatedSteps })
    }

    // Update config if needed
    if (agent_configs !== undefined) {
      await saveEvaluationStepConfig(step.id, agent_configs)
    }
  }, [evaluationPlan, saveEvaluationPlan, saveEvaluationStepConfig])

  const refresh = useCallback(async () => {
    planCache.data = null
    planCache.timestamp = 0
    planCache.promise = null
    await loadEvaluationPlan()
  }, [loadEvaluationPlan])

  useEffect(() => {
    if (workspacePath) {
      loadEvaluationPlan()
    } else {
      setEvaluationPlan(null)
    }
  }, [workspacePath, loadEvaluationPlan])

  return {
    evaluationPlan,
    loading,
    error,
    loadEvaluationPlan,
    saveEvaluationPlan,
    saveEvaluationStepConfig,
    updateEvaluationStep,
    refresh
  }
}
