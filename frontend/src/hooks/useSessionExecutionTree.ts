import { useQuery } from '@tanstack/react-query'
import { agentApi } from '../services/api'
import type { SessionExecutionTreeResponse } from '../services/api-types'
import { executionTreeRuntimeStatus } from '../utils/runtimeActivity'

function getHttpStatus(error: unknown): number | undefined {
  if (!error || typeof error !== 'object') return undefined
  const response = (error as { response?: { status?: unknown } }).response
  return typeof response?.status === 'number' ? response.status : undefined
}

export function useSessionExecutionTree(sessionId?: string | null, enabled: boolean = true) {
  return useQuery<SessionExecutionTreeResponse>({
    queryKey: ['session-execution-tree', sessionId],
    queryFn: () => agentApi.getSessionExecutionTree(sessionId!),
    enabled: enabled && !!sessionId,
    refetchInterval: (query) => {
      if (getHttpStatus(query.state.error) === 404) return false
      const data = query.state.data
      if (!data) return 3000
      return executionTreeRuntimeStatus(data) === 'busy' ? 4000 : false
    },
    staleTime: 1000,
    retry: false,
  })
}
