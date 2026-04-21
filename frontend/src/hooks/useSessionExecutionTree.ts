import { useQuery } from '@tanstack/react-query'
import { agentApi } from '../services/api'
import type { SessionExecutionTreeResponse } from '../services/api-types'

export function useSessionExecutionTree(sessionId?: string | null, enabled: boolean = true) {
  return useQuery<SessionExecutionTreeResponse>({
    queryKey: ['session-execution-tree', sessionId],
    queryFn: () => agentApi.getSessionExecutionTree(sessionId!),
    enabled: enabled && !!sessionId,
    refetchInterval: (query) => {
      const data = query.state.data
      if (!data) return 2000
      return data.summary.display_status === 'busy' ? 2000 : 5000
    },
    staleTime: 1000,
    retry: false,
  })
}
