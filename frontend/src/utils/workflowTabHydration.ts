import type { ChatTab } from '../stores/useChatStore'
import type { PollingEvent } from '../services/api-types'

type HydratableTab = Pick<ChatTab, 'tabId' | 'sessionId' | 'metadata'>

/**
 * A read-only view of one scheduled or bot run: a transcript to show, never a
 * conversation to continue. Such a tab gets its events hydrated on reconnect
 * like any other workflow tab, but no conversation-config restore and no
 * canvas step-status restore.
 */
export function isReadOnlyWorkflowRunTab(tab: Pick<ChatTab, 'metadata'>): boolean {
  return tab.metadata?.mode === 'workflow' && tab.metadata?.isViewOnly === true
}

/**
 * The persisted workflow tabs whose event buffer is empty and therefore need
 * their transcript pulled back after a page load or preset switch.
 *
 * Workflow events live only in the backend's in-memory store and in the
 * durable conversation file, never in localStorage, so every persisted tab
 * comes back empty. Read-only scheduled/bot run tabs are deliberately
 * INCLUDED: they used to be filtered out, which left a finished run's tab on
 * "Restoring previous session..." forever after the backend restarted (its
 * in-memory events gone, its on-disk transcript never asked for).
 */
export function workflowTabsNeedingHydration<T extends HydratableTab>(
  tabs: T[],
  getTabEvents: (sessionId: string) => PollingEvent[],
): T[] {
  return tabs.filter(tab =>
    tab.metadata?.mode === 'workflow' &&
    !!tab.sessionId &&
    getTabEvents(tab.sessionId).length === 0,
  )
}

/** Start the selected tab first, with at most two transcript requests in flight.
 * Resolve when that tab settles; other tabs continue without blocking the view.
 * Each failure is handled independently so one unavailable tab cannot stall others.
 */
export async function hydrateWorkflowTabsPrioritized<T extends HydratableTab>(
  tabs: T[],
  activeTabId: string | null,
  hydrate: (tab: T) => Promise<void>,
  onError: (tab: T, error: unknown) => void,
): Promise<number> {
  if (tabs.length === 0) return 0
  const selected = tabs.find(tab => tab.tabId === activeTabId) ?? tabs[0]
  const queue = [selected, ...tabs.filter(tab => tab !== selected)]
  let finishSelected!: () => void
  const selectedDone = new Promise<void>(resolve => { finishSelected = resolve })
  let next = 0
  const worker = async () => {
    while (next < queue.length) {
      const tab = queue[next++]
      try {
        await hydrate(tab)
      } catch (error) {
        onError(tab, error)
      } finally {
        if (tab === selected) finishSelected()
      }
    }
  }
  // The selected tab can finish before the other worker. Consume every worker's
  // outcome even after this call returns; there must be no unhandled rejection.
  void Promise.allSettled([worker(), worker()])
  await selectedDone
  return tabs.length
}
