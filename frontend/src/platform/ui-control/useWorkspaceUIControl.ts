import { useEffect, useRef } from 'react'
import { useChatStore } from '../../stores/useChatStore'
import { workflowUIControl } from '../../services/api'
import { useWorkflowStore } from '../../stores/useWorkflowStore'
import { usePresentationEvents } from '../presentations/usePresentationEvents'
import { getWorkspaceView, isWorkspaceViewId } from '../../components/workflow/workspaceViews'
import { UI_CONTROL_CONTRACT } from './contract.generated'
import { applyUIAction, workspaceHost, type UIAction, type UISnapshot } from './client'

const kinds = ['workflow.ui-action']
type Binding = { binding: string; token: string; workspace: string }

export type WorkspaceUIControlAdapter = {
  getView: () => string
  openView: (view: string, target?: string) => void
  refreshView?: (view: string, target?: string) => void
  isViewSupported: (view: string) => boolean
  labelForView: (view: string) => string
  actorLabel?: string
  getTarget?: (view: string, host: HTMLElement | undefined) => string | undefined
}

// One lease per mounted, visible chat. No history playback, persisted binding,
// auto product switch, or shared global command queue. Duplicate SSE only wakes
// sync; the authenticated server atomically claims commands for this binding.
export function useWorkspaceUIControl(session: string | undefined, adapter?: WorkspaceUIControlAdapter): void {
  const adapterRef = useRef(adapter)
  adapterRef.current = adapter
  const events = usePresentationEvents(session, kinds)
  const wake = useRef<(() => void) | null>(null)
  useEffect(() => { wake.current?.() }, [events])
  useEffect(() => {
    if (!session) return
    let binding: Binding | undefined
    let stopped = false
    let busy = false
    let lastView = ''
    let revision = 0
    let lastVisible = false
    let lastTarget: string | undefined
    const controller = new AbortController()
    const state = (): UISnapshot => {
      const configured = adapterRef.current
      const store = useWorkflowStore.getState()
      const view = configured?.getView() ?? store.workflowWorkspaceView ?? store.lastCanvasView
      const host = binding ? workspaceHost(binding.workspace) : undefined
      const visible = !!host && host.getClientRects().length > 0 && !!host.querySelector('[data-ui-view-mounted]') && document.visibilityState === 'visible'
      const panel = host?.querySelector<HTMLElement>('[data-ui-plan-step]')
      const target = configured?.getTarget?.(view, host)
        ?? (view === 'flow' && panel?.getClientRects().length ? panel.dataset.uiPlanStep : undefined)
      if (lastView !== view || lastVisible !== visible || lastTarget !== target) {
        revision++; lastView = view; lastVisible = visible; lastTarget = target
      }
      return { view, revision, visible, target }
    }
    // Don't send the returned workspace back: identities are server-derived.
    const boundCall = (body: Record<string, unknown>) => workflowUIControl(session, {
      version: UI_CONTROL_CONTRACT.version, binding: binding?.binding, token: binding?.token, ...body,
    })
    const release = async () => {
      const previous = binding
      binding = undefined
      if (previous) await workflowUIControl(session, { version: UI_CONTROL_CONTRACT.version, operation: 'unbind', binding: previous.binding, token: previous.token }).catch(() => {})
    }
    const sync = async () => {
      if (busy || stopped) return
      if (document.visibilityState !== 'visible') { await release(); return }
      busy = true
      try {
        if (!binding) {
          const next = await workflowUIControl(session, { version: UI_CONTROL_CONTRACT.version, operation: 'bind' }) as Binding
          if (!next || typeof next.binding !== 'string' || typeof next.token !== 'string' || typeof next.workspace !== 'string') {
            console.warn('[WorkspaceUIControl] bind invalid_binding_response')
            return
          }
          if (stopped) {
            await workflowUIControl(session, { version: UI_CONTROL_CONTRACT.version, operation: 'unbind', binding: next.binding, token: next.token })
            return
          }
          binding = next
        }
        if (!workspaceHost(binding.workspace)) {
          console.warn('[WorkspaceUIControl] bind workspace_host_missing')
          await release(); return
        }
        const commands = await boundCall({ operation: 'sync', state: state() }) as UIAction[]
        for (const command of commands) {
          if (stopped) break
          const before = state()
          const result = await applyUIAction(command, binding.workspace, (view, target) => {
            const configured = adapterRef.current
            if (configured) {
              if (configured.isViewSupported(view)) configured.openView(view, target)
            } else if (isWorkspaceViewId(view)) useWorkflowStore.getState().openWorkspaceView(view, target)
          }, state, controller.signal, (view, target) => {
            const configured = adapterRef.current
            if (configured) {
              configured.openView(view, target)
              configured.refreshView?.(view, target)
            } else if (isWorkspaceViewId(view)) {
              const store = useWorkflowStore.getState()
              store.openWorkspaceView(view, target)
              store.refreshWorkspaceView(target)
            }
          })
          if (stopped) break
          if (result.status === 'applied') {
            revision++
            const after = state()
            const configured = adapterRef.current
            const supported = configured?.isViewSupported(after.view) ?? isWorkspaceViewId(after.view)
            if (after.visible && (!before.visible || before.view !== after.view) && supported) {
              const label = configured?.labelForView(after.view) ?? (after.view === 'browser' ? 'Browser' : getWorkspaceView(after.view as Parameters<typeof getWorkspaceView>[0]).label)
              useChatStore.getState().addToast(`${configured?.actorLabel ?? 'Builder'} opened ${label}`, 'info')
            }
          }
          await boundCall({ operation: 'ack', request_id: command.request_id, ...result, state: state() })
        }
      } catch (error) {
        // Never log the request/config object: it includes the binding token.
        const response = (error as { response?: { status?: number; data?: unknown } })?.response
        const code = typeof response?.data === 'string' && /^[a-z_]+\s*$/.test(response.data)
          ? response.data.trim() : 'connection_failed'
        console.warn(`[WorkspaceUIControl] ${binding ? 'sync' : 'bind'} status=${response?.status ?? 'network_error'} code=${code}`)
        // An uncertain outcome is never replayed. A new lease cannot ACK or
        // claim the previous lease's commands; the server expires those.
        await release()
      } finally { busy = false }
    }
    const onVisibility = () => {
      if (document.visibilityState !== 'visible') void release()
      else void sync()
    }
    wake.current = () => { void sync() }
    const timer = setInterval(() => { void sync() }, 3000)
    document.addEventListener('visibilitychange', onVisibility)
    void sync()
    return () => {
      stopped = true
      controller.abort()
      clearInterval(timer)
      wake.current = null
      document.removeEventListener('visibilitychange', onVisibility)
      void release()
    }
  }, [session])
}
