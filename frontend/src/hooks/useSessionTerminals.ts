import { useQuery } from '@tanstack/react-query'
import { agentApi } from '../services/api'
import type { ListTerminalsResponse } from '../services/api-types'

/**
 * Terminal-presence probe for a session. Used by ChatArea's chat-surface
 * resolver to detect a restored/native-resume tab whose surface is a live
 * terminal (which leaves NO execution-tree nodes) so it stays on the terminal
 * instead of bouncing to the previous-chats landing. Content is fetched as
 * 'none' — we only need presence, not scrollback.
 *
 * keepPolling: by default the probe stops once any terminal appears (presence is
 * all the resolver needs). ChatInput's tmux-transport routing instead needs the
 * CURRENT liveness (a pane can later go idle or exit), so it passes keepPolling
 * to keep refreshing the `active`/`tmux_session` fields. Consumers share one
 * query by key, so the continuous poll simply gives every consumer fresher data.
 */
export function useSessionTerminals(sessionId?: string | null, enabled: boolean = true, keepPolling: boolean = false) {
  return useQuery<ListTerminalsResponse>({
    queryKey: ['session-terminals-presence', sessionId],
    queryFn: () => agentApi.listTerminals(sessionId!, 'none'),
    enabled: enabled && !!sessionId,
    refetchInterval: (query) => {
      // Routing needs current liveness — keep polling so a pane that goes idle or
      // exits is reflected, not just first-appearance presence.
      if (keepPolling) return 3000
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
