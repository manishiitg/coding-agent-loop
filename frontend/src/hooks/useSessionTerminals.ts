import { useEffect, useMemo } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { agentApi } from '../services/api'
import type { ListTerminalsResponse } from '../services/api-types'
import { TERMINAL_REFRESH_REQUEST_EVENT } from '../utils/terminalRefresh'
import { isLiveWorkflowTerminal } from '../utils/workflowTerminalActivity'

/**
 * Terminal-presence probe for a session. Used by ChatArea's chat-surface
 * resolver to detect a restored/native-resume tab whose surface is a live
 * terminal (which leaves NO execution-tree nodes) so it stays on the terminal
 * instead of bouncing to the previous-chats landing. Content is fetched as
 * 'none' — we only need presence, not scrollback.
 */
export function useSessionTerminals(sessionId?: string | null, enabled: boolean = true) {
  const queryClient = useQueryClient()
  const queryKey = useMemo(() => ['session-terminals-presence', sessionId] as const, [sessionId])

  useEffect(() => {
    if (!enabled || !sessionId || typeof window === 'undefined') return
    const handleRefreshRequest = () => {
      void queryClient.invalidateQueries({
        queryKey,
        refetchType: 'active',
      })
    }
    window.addEventListener(TERMINAL_REFRESH_REQUEST_EVENT, handleRefreshRequest)
    return () => {
      window.removeEventListener(TERMINAL_REFRESH_REQUEST_EVENT, handleRefreshRequest)
    }
  }, [enabled, queryClient, queryKey, sessionId])

  return useQuery<ListTerminalsResponse>({
    queryKey,
    queryFn: () => agentApi.listTerminals(sessionId!, 'none'),
    enabled: enabled && !!sessionId,
    refetchInterval: (query) => {
      // A startup placeholder can appear before the provider has attached its
      // tmux session. Stopping on that provisional row freezes a non-live
      // snapshot in this presence cache; when streaming later settles, the chat
      // surface then incorrectly falls back to Previous chats. Keep polling
      // until a terminal has an authoritative live/retained backing pane.
      const data = query.state.data
      if (data?.terminals?.some(isLiveWorkflowTerminal)) return false
      return 3000
    },
    staleTime: 1000,
    retry: false,
  })
}
