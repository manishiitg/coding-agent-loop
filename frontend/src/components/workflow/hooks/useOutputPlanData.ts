import { useState, useCallback, useEffect } from 'react'
import { agentApi } from '../../../services/api'
import type { WorkflowOutputPlan } from '../../../services/api-types'

export interface UseOutputPlanDataReturn {
  outputPlan: WorkflowOutputPlan | null
  loading: boolean
  error: string | null
  refresh: () => Promise<void>
}

export function useOutputPlanData(workspacePath: string | null): UseOutputPlanDataReturn {
  const [outputPlan, setOutputPlan] = useState<WorkflowOutputPlan | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const loadOutputPlan = useCallback(async () => {
    if (!workspacePath) {
      setOutputPlan(null)
      setError(null)
      return
    }

    setLoading(true)
    setError(null)

    try {
      const candidatePaths = [
        `${workspacePath}/planning/output_plan.json`,
        `${workspacePath}/output/output_plan.json`
      ]

      let loadedPlan: WorkflowOutputPlan | null = null
      for (const path of candidatePaths) {
        const response = await agentApi.getPlannerFileContent(path)
        if (response.success && response.data?.content) {
          const parsed = JSON.parse(response.data.content) as WorkflowOutputPlan
          const step = parsed.step ?? null
          loadedPlan = step ? { step } : null
          break
        }
      }
      setOutputPlan(loadedPlan)
    } catch (err) {
      console.error('[OutputPlan] Failed to load output plan:', err)
      setOutputPlan(null)
      setError(err instanceof Error ? err.message : 'Failed to load output plan')
    } finally {
      setLoading(false)
    }
  }, [workspacePath])

  useEffect(() => {
    loadOutputPlan()
  }, [loadOutputPlan])

  return {
    outputPlan,
    loading,
    error,
    refresh: loadOutputPlan
  }
}
