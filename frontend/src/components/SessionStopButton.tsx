import { useRef, useState } from 'react'
import { Loader2, Square } from 'lucide-react'
import { agentApi } from '../services/api'
import { useAuthStore } from '../stores/useAuthStore'
import { useChatStore } from '../stores/useChatStore'
import { isWorkflowReadOnly } from '../utils/workflowPermissions'
import { Button } from './ui/Button'

interface SessionStopButtonProps {
  tabId: string
  footer?: boolean
}

// Shared by the composer and scheduled-run footer. This stops the session
// and background work; Escape remains a separate foreground interrupt.
export function SessionStopButton({ tabId, footer = false }: SessionStopButtonProps) {
  const tab = useChatStore(state => state.chatTabs[tabId])
  const isReadOnlyUser = useAuthStore(state => isWorkflowReadOnly(state.user, state.isMultiUserMode))
  const [stopping, setStopping] = useState(false)
  const inFlight = useRef(false)

  const sessionId = tab?.sessionId
  if (!sessionId || isReadOnlyUser || (!tab?.isStreaming && !tab?.hasRunningBgAgents && !stopping)) return null

  const stopSession = async () => {
    if (inFlight.current) return
    inFlight.current = true
    setStopping(true)
    try {
      await agentApi.stopSession(sessionId, true)
      const store = useChatStore.getState()
      store.setTabStreaming(tabId, false)
      store.setTabHasRunningBgAgents(tabId, false)
    } catch (error) {
      console.error('[SessionStopButton] Failed to stop session:', error)
      useChatStore.getState().addToast('Could not stop the session. Please try again.', 'error')
    } finally {
      inFlight.current = false
      setStopping(false)
    }
  }

  const button = (
    <Button
      type="button"
      onClick={() => void stopSession()}
      disabled={stopping}
      variant={footer ? 'destructive' : 'ghost'}
      size={footer ? 'sm' : 'icon'}
      className={footer ? 'gap-2' : 'h-7 w-7 p-0 text-muted-foreground hover:bg-destructive/10 hover:text-destructive'}
      data-testid={footer ? 'scheduled-run-stop-button' : 'chat-stop-button'}
      aria-label={stopping ? 'Stopping session' : 'Stop session and background work'}
      title="Stop session and background work"
    >
      {stopping
        ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
        : <Square className="h-3.5 w-3.5" fill="currentColor" />}
      {footer && (stopping ? 'Stopping…' : 'Stop run')}
    </Button>
  )

  return footer
    ? <div className="flex shrink-0 justify-end border-t border-border px-4 py-3">{button}</div>
    : button
}
