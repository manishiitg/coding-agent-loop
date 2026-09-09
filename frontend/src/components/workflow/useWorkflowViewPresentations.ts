// The workflow agent can open a toolbar view for the user (the
// open_workspace_view tool emits a "workflow.view" presentation). This turns
// those events, for the chat tab on screen, into the same store call the
// toolbar buttons make. Events already present when the tab was opened are
// history and are not replayed.
import { useEffect, useRef } from 'react'
import { useChatStore } from '../../stores/useChatStore'
import { useWorkflowStore } from '../../stores/useWorkflowStore'
import { usePresentationEvents } from '../../platform/presentations/usePresentationEvents'
import { getWorkspaceView, isWorkspaceViewId } from './workspaceViews'
import { useWorkspaceUIControl } from '../../platform/ui-control/useWorkspaceUIControl'

export const WORKFLOW_VIEW_PRESENTATION_KIND = 'workflow.view'
const KINDS = [WORKFLOW_VIEW_PRESENTATION_KIND]

export function useWorkflowViewPresentations(tabId: string | null | undefined): void {
  const sessionId = useChatStore(state => (tabId ? state.chatTabs[tabId]?.sessionId : undefined))
  useWorkspaceUIControl(sessionId ?? undefined)
  const openWorkspaceView = useWorkflowStore(state => state.openWorkspaceView)
  const presentations = usePresentationEvents(sessionId ?? undefined, KINDS)
  const handed = useRef<{ sessionId: string; count: number } | null>(null)
  useEffect(() => {
    if (!sessionId) return
    if (!handed.current || handed.current.sessionId !== sessionId) {
      handed.current = { sessionId, count: presentations.length }
      return
    }
    for (const p of presentations.slice(handed.current.count)) {
      const view = p.payload.view
      if (!isWorkspaceViewId(view)) continue
      const target = typeof p.payload.target === 'string' ? p.payload.target : undefined
      const store = useWorkflowStore.getState()
      const shown = store.showWorkspacePane ? (store.workflowWorkspaceView ?? store.lastCanvasView) : null
      if (p.payload.action === 'refresh') {
        // refresh_workspace_view: reload what the view shows. A view that is
        // not on screen is opened first; opening mounts it fresh, so the
        // explicit reload is only needed when it was already there.
        if (shown === view) store.refreshWorkspaceView(target)
        else openWorkspaceView(view, target)
      } else if (shown !== view || target) {
        // A target re-fires even on the open view: "show me this step" is a
        // real instruction, not a no-op, when the view is already up.
        openWorkspaceView(view, target)
      }
      if (shown !== view) {
        const label = view === 'browser' ? 'Browser' : getWorkspaceView(view).label
        useChatStore.getState().addToast(`Builder opened ${label}`, 'info')
      }
    }
    handed.current = { sessionId, count: presentations.length }
  }, [sessionId, presentations, openWorkspaceView])
}
