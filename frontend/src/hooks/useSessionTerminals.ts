import { useQuery } from '@tanstack/react-query'
import { agentApi } from '../services/api'
import type { ListTerminalsResponse } from '../services/api-types'

/**
 * Terminal-presence probe for a session. Used by ChatArea's chat-surface
 * resolver to detect a restored/native-resume tab whose surface is a live
 * terminal (which leaves NO execution-tree nodes) so it stays on the terminal
 * instead of bouncing to the previous-chats landing. Content is fetched as
 * 'none' — we only need presence, not scrollback.
 */
export function useSessionTerminals(sessionId?: string | null, enabled: boolean = true) {
  return useQuery<ListTerminalsResponse>({
    queryKey: ['session-terminals-presence', sessionId],
    queryFn: () => agentApi.listTerminals(sessionId!, 'none'),
    enabled: enabled && !!sessionId,
    refetchInterval: (query) => {
      // A resume reattaches its terminal asynchronously — keep polling until one
      // appears, then stop (presence is all the resolver needs).
      const data = query.state.data
      if (data && (data.terminals?.length ?? 0) > 0) return false
      return 3000
    },
    staleTime: 1000,
    retry: false,
  })
}
