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
 * The persisted workflow tabs whose durable transcript must be pulled back
 * after a page load or preset switch.
 *
 * An interactive builder tab is always included, even when it already has
 * events. The backend event store is a bounded transport cache, so its
 * non-empty tail can end before newer messages that native-transcript sync has
 * already written to durable conversation history. Treating "has any event"
 * as "fully hydrated" made refresh restore a stale answer and omit the newest
 * user/final turns.
 *
 * Read-only scheduled/bot run tabs are included only when empty: they used to
 * be filtered out entirely, which left a finished run's tab on "Restoring
 * previous session..." forever after the backend restarted.
 */
export function workflowTabsNeedingHydration<T extends HydratableTab>(
  tabs: T[],
  getTabEvents: (sessionId: string) => PollingEvent[],
): T[] {
  return tabs.filter(tab =>
    tab.metadata?.mode === 'workflow' &&
    !!tab.sessionId &&
    (tab.metadata?.isViewOnly !== true || getTabEvents(tab.sessionId).length === 0),
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
