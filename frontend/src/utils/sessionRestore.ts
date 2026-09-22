import { captureChatIdentity, assertChatIdentityCurrent } from './chatIdentity'
import { useChatStore } from '../stores/useChatStore'
import { useModeStore } from '../stores/useModeStore'
import { agentApi } from '../services/api'
import type { PollingEvent } from '../services/api-types'
import { truncateTabTitle } from './textUtils'
import { applyLiveInputConfirmations, resolveLiveInputConfirmations, splitLiveInputConfirmations, stampLiveInputIdentity, withDeliveryConfirmation } from './liveInputReceipt'
import type { LiveInputConfirmationUpdate } from './liveInputReceipt'
import axios from 'axios'

const TAG = '[SessionRestore]'

type RuntimeSessionState = {
  status: string
  hasRunningBackgroundAgents?: boolean
  isSyntheticTurn?: boolean
  canSteer?: boolean
  restoredEvents?: PollingEvent[]
}

function isForegroundStreaming(state: RuntimeSessionState): boolean {
  if (state.status !== 'running') return false
  // Background-only work should not lock the composer after restore.
  // Synthetic auto-notification turns are activity, but they should not queue user input.
  return !state.isSyntheticTurn && (!state.hasRunningBackgroundAgents || !!state.canSteer)
}

/**
 * Per-session async lock to prevent duplicate restores.
 * If restoreSession is called concurrently for the same session,
 * subsequent calls return the existing Promise.
 */
const restoreInProgress = new Map<string, Promise<string>>()
const hydrateRequestVersions = new Map<string, number>()

/**
 * Apply session status (completed/streaming/restored) to a tab.
 */
function applySessionStatus(tabId: string, state: RuntimeSessionState): void {
  const chatStore = useChatStore.getState()
  const isDone = state.status === 'completed' || state.status === 'stopped'
  const isError = state.status === 'error'
  chatStore.setTabCompleted(tabId, isDone)
  chatStore.setTabStreaming(tabId, isDone || isError ? false : isForegroundStreaming(state))
  chatStore.setTabHasRunningBgAgents(tabId, !!state.hasRunningBackgroundAgents)
  chatStore.setTabSyntheticTurn(tabId, !!state.isSyntheticTurn)
  chatStore.setTabCanSteer(tabId, !!state.canSteer)
  if (isDone || isError) {
    chatStore.setTabMetadata(tabId, { isRestored: true })
  }
}

/**
 * Unified session restoration function.
 * Handles all restore flows: auto-restore, page-refresh hydration, and inline history selection.
 *
 * Returns the tabId for the restored session.
 */
export async function restoreSession(
  sessionId: string,
  options?: {
    title?: string
    source?: string
    skipConfigRestore?: boolean
    workspacePath?: string
  }
): Promise<string> {
  // Async lock: if already restoring this session, return the existing promise
  const restoreKey = `${captureChatIdentity()}:${sessionId}`
  const existing = restoreInProgress.get(restoreKey)
  if (existing) {
    console.log(`${TAG} Dedup hit for ${sessionId} (source=${options?.source}), returning existing promise`)
    return existing
  }

  const promise = doRestoreSession(sessionId, options)
  restoreInProgress.set(restoreKey, promise)

  try {
    return await promise
  } finally {
    restoreInProgress.delete(restoreKey)
  }
}

async function doRestoreSession(
  sessionId: string,
  options?: {
    title?: string
    source?: string
    skipConfigRestore?: boolean
    workspacePath?: string
  }
): Promise<string> {
  const identity = captureChatIdentity()
  const src = options?.source || 'unknown'
  console.log(`${TAG} Start session=${sessionId} source=${src} title=${options?.title ?? '(none)'}`)
  const chatStore = useChatStore.getState()

  // Step 1: Check for existing tab with events already loaded
  const existingTabWithSession = Object.values(chatStore.chatTabs).find(tab => tab.sessionId === sessionId)
  const existingTab = existingTabWithSession
  const existingEventCount = existingTab ? chatStore.getTabEvents(sessionId).length : 0
  if (existingTab) {
    if (existingEventCount > 0) {
      console.log(`${TAG} [${src}] Tab ${existingTab.tabId} already has ${existingEventCount} events, refreshing runtime state`)
    } else {
      console.log(`${TAG} [${src}] Tab ${existingTab.tabId} exists but has 0 events, will hydrate`)
    }
  }

  // Chat sessions are in-memory on the backend now — there is no persisted
  // session metadata to fetch. Tab state (title, config) is the frontend's
  // responsibility; session status comes from the polling API.
  const tabMode = 'multi-agent' as const
  useModeStore.getState().setModeCategory('multi-agent')

  let tabId: string
  if (existingTab) {
    tabId = existingTab.tabId
    console.log(`${TAG} [${src}] Reusing existing tab ${tabId}`)
  } else {
    const title = truncateTabTitle(options?.title || 'Chat')
    tabId = await chatStore.createChatTab(
      title,
      { mode: tabMode, isRestored: false },
      sessionId,
    )
    assertChatIdentityCurrent(identity)
    console.log(`${TAG} [${src}] Created tab ${tabId} mode=${tabMode}`)
  }

  // Step 7: Sync runtime state / events
  try {
    if (existingEventCount > 0) {
      const currentLastIndex = chatStore.getTabLastEventIndex(sessionId)
      const runtime = await agentApi.getSessionEvents(sessionId, currentLastIndex, {
        durableChat: true,
        workspacePath: options?.workspacePath || existingTab?.metadata?.agentProfileWorkspace,
      })
      assertChatIdentityCurrent(identity)
      applySessionStatus(tabId, {
        status: runtime.session_status,
        hasRunningBackgroundAgents: runtime.has_running_background_agents,
        isSyntheticTurn: runtime.is_synthetic_turn,
        canSteer: runtime.can_steer,
      })
      if (runtime.events.length > 0) {
        appendRestoredLiveTail(sessionId, runtime.events)
      }
      const cursor = runtime.latest_sequence ?? runtime.last_processed_index
      if (cursor !== undefined) {
        chatStore.setTabLastEventIndex(sessionId, cursor)
      }
      if (runtime.has_more !== undefined) {
        chatStore.setTabHasMoreOlderEvents(sessionId, runtime.has_more)
      }
      console.log(`${TAG} [${src}] Refreshed runtime state for existing tab ${tabId}`)
    } else {
      const runtime = await hydrateTabEvents(sessionId, {
        workspacePath: options?.workspacePath || existingTab?.metadata?.agentProfileWorkspace,
        fallbackToChatHistory: true,
      })
      assertChatIdentityCurrent(identity)
      applySessionStatus(tabId, runtime)
      const eventCount = chatStore.getTabEvents(sessionId).length
      console.log(`${TAG} [${src}] Hydrated ${eventCount} events`)
    }
  } catch (err) {
    assertChatIdentityCurrent(identity)
    if (isNotFoundError(err) && existingEventCount > 0) {
      console.log(`${TAG} [${src}] Session ${sessionId} was not found; keeping locally restored events`)
      applySessionStatus(tabId, {
        status: 'completed',
        hasRunningBackgroundAgents: false,
        isSyntheticTurn: false,
        canSteer: false,
      })
    } else {
      console.error(`${TAG} [${src}] Failed to sync runtime state for ${sessionId}:`, err)
    }
  }

  console.log(`${TAG} [${src}] Done session=${sessionId} tab=${tabId}`)
  return tabId
}

function isNotFoundError(error: unknown): boolean {
  return axios.isAxiosError(error) && error.response?.status === 404
}

const ACCEPTED_LIVE_INPUT_STATUSES = new Set(['sent_to_cli', 'next_turn_started', 'queued_for_injection', 'queued_for_turn'])

function eventPayload(event: PollingEvent): Record<string, unknown> {
  const outer = event.data && typeof event.data === 'object'
    ? event.data as unknown as Record<string, unknown>
    : {}
  return outer.data && typeof outer.data === 'object' ? outer.data as Record<string, unknown> : outer
}

function eventMetadata(event: PollingEvent): Record<string, unknown> {
  const metadata = eventPayload(event).metadata
  return metadata && typeof metadata === 'object' ? metadata as Record<string, unknown> : {}
}

function isAcceptedOptimisticLiveInput(event: PollingEvent): boolean {
  if (event.type !== 'user_message' || !event.id?.startsWith('user-message-')) return false
  const metadata = eventMetadata(event)
  return metadata.source === 'coding_agent_live_input'
    && ACCEPTED_LIVE_INPUT_STATUSES.has(String(metadata.delivery_status || ''))
}

// appendTimelineAndApplyConfirmations lands a timeline window and consumes
// its durability receipts in one ordering: rows first, verdicts second,
// against the merged store. The synchronous append keeps row visibility
// immediate so a receipt in this same window matches a row it arrived
// with — the live path and restore share this helper rather than
// maintaining two ingestion orderings. Verdicts apply via patchTabEvents
// so micro-batched events from the same window still flush afterwards.
export function appendTimelineAndApplyConfirmations(sessionId: string, timelineEvents: PollingEvent[], confirmations: LiveInputConfirmationUpdate[]) {
  const chatStore = useChatStore.getState()
  if (timelineEvents.length > 0) chatStore._addTabEventsImmediate(sessionId, timelineEvents)
  if (confirmations.length === 0) return
  // patchTabEvents, not get+set: setTabEvents clears the micro-batch
  // buffer, dropping tool events that arrived in this same window.
  chatStore.patchTabEvents(sessionId, events => applyLiveInputConfirmations(events, confirmations))
}

// appendRestoredLiveTail appends a raw live-tail window the same way the live
// path ingests it: durability receipts never enter the timeline and upgrade
// the merged rows they name. A receipt-only window appends nothing. Without
// receipts the call stays on the micro-batched path exactly as before.
export function appendRestoredLiveTail(sessionId: string, incoming: ReadonlyArray<PollingEvent>) {
  const chatStore = useChatStore.getState()
  const { timelineEvents, confirmations } = splitLiveInputConfirmations(incoming)
  if (confirmations.length === 0) {
    if (timelineEvents.length > 0) chatStore.addTabEvents(sessionId, timelineEvents)
    return
  }
  if (timelineEvents.length > 0) chatStore._addTabEventsImmediate(sessionId, timelineEvents)
  // Identity-transfer against the merged rows first, then the shared
  // append+apply consumes the receipts (with an empty remainder: this
  // window's rows already landed above).
  chatStore.patchTabEvents(sessionId, events => transferLiveTailInputIdentity(events, timelineEvents))
  appendTimelineAndApplyConfirmations(sessionId, [], confirmations)
}

// transferLiveTailInputIdentity copies live-input identity (message_id and
// delivery facts) from volatile live-tail user rows onto restored rows with
// the same content. Durable conversation carriers keep role/content only, so
// without the transfer a restored row can never match the durability receipt
// that names it and the tick is lost on every restore. Content matching
// carries the same identical-resend ambiguity the live echo suppression
// already accepts; a mismatch only costs a tick, never a row.
function transferLiveTailInputIdentity(
  restored: PollingEvent[],
  liveTails: ReadonlyArray<PollingEvent>,
): PollingEvent[] {
  const donors = liveTails.filter(event =>
    event.type === 'user_message' && String(eventMetadata(event).message_id || '').trim())
  if (donors.length === 0) return restored
  return restored.map(row => {
    if (row.type !== 'user_message' || String(eventMetadata(row).message_id || '').trim()) return row
    const content = String(eventPayload(row).content || '').trim()
    if (!content) return row
    const donor = donors.find(candidate => String(eventPayload(candidate).content || '').trim() === content)
    if (!donor) return row
    const metadata = eventMetadata(donor)
    const messageId = String(metadata.message_id || '').trim()
    const stamped = stampLiveInputIdentity(row, messageId,
      typeof metadata.delivery_status === 'string' && metadata.delivery_status ? metadata.delivery_status : 'sent_to_cli',
      typeof metadata.provider === 'string' ? metadata.provider : undefined)
    // A donor that already carries a terminal verdict (a live-upgraded row
    // surviving in the tab) transfers the verdict too, so the tick survives
    // even when the receipt itself fell out of the re-fetched window.
    const verdict = metadata.confirmation
    if (verdict === 'confirmed' || verdict === 'accepted_but_unflushed' || verdict === 'failed') {
      const latencyRaw = metadata.latency_ms
      return withDeliveryConfirmation(stamped, {
        messageId,
        outcome: verdict,
        proofSource: typeof metadata.proof_source === 'string' ? metadata.proof_source : undefined,
        latencyMs: typeof latencyRaw === 'number' ? latencyRaw : undefined,
        provider: typeof metadata.provider === 'string' ? metadata.provider : undefined,
      })
    }
    return stamped
  })
}

function durableLiveInputMessageIDs(events: ReadonlyArray<PollingEvent>): Set<string> {
  const ids = new Set<string>()
  for (const event of events) {
    if (event.type !== 'user_message') continue
    const messageID = String(eventMetadata(event).message_id || '').trim()
    if (messageID) ids.add(messageID)
  }
  return ids
}

// An accepted live-input bubble is a local receipt for a server mutation. A
// history response can lag that mutation, so absence from one snapshot is not
// deletion authority. Keep the receipt until the durable UI trace confirms the
// server message ID. This invariant runs after projection because projection is
// allowed to replace the complete event array.
function preserveUnconfirmedAcceptedLiveInputs(
  projectedEvents: PollingEvent[],
  currentEvents: ReadonlyArray<PollingEvent>,
): PollingEvent[] {
  const durableMessageIDs = durableLiveInputMessageIDs(projectedEvents)
  const projectedIDs = new Set(projectedEvents.map(event => event.id).filter((id): id is string => !!id))
  const missing = currentEvents.filter(event => {
    if (!isAcceptedOptimisticLiveInput(event) || (event.id && projectedIDs.has(event.id))) return false
    const messageID = String(eventMetadata(event).message_id || '').trim()
    return !messageID || !durableMessageIDs.has(messageID)
  })
  if (missing.length === 0) return projectedEvents

  return [...projectedEvents, ...missing]
    .map((event, index) => ({ event, index }))
    .sort((left, right) => {
      if (left.event.type === 'conversation_resumed') return -1
      if (right.event.type === 'conversation_resumed') return 1
      const leftTime = Date.parse(left.event.timestamp || '')
      const rightTime = Date.parse(right.event.timestamp || '')
      if (Number.isFinite(leftTime) && Number.isFinite(rightTime) && leftTime !== rightTime) return leftTime - rightTime
      return left.index - right.index
    })
    .map(({ event }) => event)
}

/** Load the canonical, bounded chat page from SQLite. */
export async function hydrateTabEvents(
  sessionId: string,
  options: {
    workspacePath?: string
    fallbackToChatHistory?: boolean
    // Kept for callers that explicitly request history. Durable history is
    // now the default for every session, so this no longer changes behavior.
    preferChatHistory?: boolean
    // Restore the bounded, formatted UI trace for every retained chat. It
    // supplies tool calls that structured provider history represents only as
    // function markers; raw terminal frames are never requested.
    includeUiEvents?: boolean
  } = {},
): Promise<RuntimeSessionState> {
  const identity = captureChatIdentity()
  const chatStore = useChatStore.getState()
  const eventsAtStart = chatStore.getTabEvents(sessionId)
  const startingIDs = new Set(eventsAtStart.map(event => event.id).filter(Boolean))
  const requestKey = `${identity}:${sessionId}`
  const requestVersion = (hydrateRequestVersions.get(requestKey) || 0) + 1
  hydrateRequestVersions.set(requestKey, requestVersion)
  const response = await agentApi.getRecentChatEvents(sessionId, options.workspacePath)
  assertChatIdentityCurrent(identity)
  if (hydrateRequestVersions.get(requestKey) !== requestVersion) {
    return {
      status: response.session_status,
      hasRunningBackgroundAgents: response.has_running_background_agents,
      isSyntheticTurn: response.is_synthetic_turn,
      canSteer: response.can_steer,
    }
  }
  const eventsNow = chatStore.getTabEvents(sessionId)
  const concurrentEvents = eventsNow.filter(event => event.id && !startingIDs.has(event.id))
  const restored = preserveUnconfirmedAcceptedLiveInputs(
    transferLiveTailInputIdentity(response.events, eventsNow),
    eventsNow,
  )
  chatStore.setTabEvents(sessionId, resolveLiveInputConfirmations(restored))
  if (concurrentEvents.length > 0) appendRestoredLiveTail(sessionId, concurrentEvents)
  const cursor = response.latest_sequence ?? response.last_processed_index
  if (cursor !== undefined) chatStore.setTabLastEventIndex(sessionId, cursor)
  chatStore.setTabHasMoreOlderEvents(sessionId, response.has_more)
  chatStore.setTabHistoryPagination(sessionId, response.has_more && response.oldest_sequence
    ? { hasMore: true, nextOffset: response.oldest_sequence }
    : null)

  return {
    status: response.session_status,
    hasRunningBackgroundAgents: response.has_running_background_agents,
    isSyntheticTurn: response.is_synthetic_turn,
    canSteer: response.can_steer,
    restoredEvents: restored,
  }
}
