import { useChatStore } from '../stores/useChatStore'

const MAX_WAIT_MS = 3000

function restoring(counts: Record<string, number>): boolean {
  return Object.values(counts).some(count => count > 0)
}

// Background panel checks (activity badge, plan changelog) wait until the
// workflow chat being opened has restored, so they don't compete with it for
// the connection during a switch. Never waits longer than MAX_WAIT_MS.
export function whenWorkflowChatSettled(maxWaitMs = MAX_WAIT_MS): Promise<void> {
  return new Promise(resolve => {
    let done = false
    let unsubscribe: (() => void) | undefined
    const finish = () => {
      if (done) return
      done = true
      unsubscribe?.()
      window.clearTimeout(timer)
      resolve()
    }
    const timer = window.setTimeout(finish, maxWaitMs)
    // Let the switch start its restore before judging that nothing is restoring.
    window.setTimeout(() => {
      if (done) return
      if (!restoring(useChatStore.getState().restoringWorkflowSessions)) return finish()
      unsubscribe = useChatStore.subscribe(state => {
        if (!restoring(state.restoringWorkflowSessions)) finish()
      })
    }, 0)
  })
}
