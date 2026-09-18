import { captureChatIdentity, assertChatIdentityCurrent } from './chatIdentity'
import { useChatStore } from '../stores/useChatStore'
import { conversationToRestoredEvents } from '../../shared/session/restore'
import { useModeStore } from '../stores/useModeStore'
import { agentApi } from '../services/api'
import type { ChatHistoryConversation, PollingEvent } from '../services/api-types'
import { truncateTabTitle } from './textUtils'
import axios from 'axios'

const TAG = '[SessionRestore]'

// Fetch older turns only when the user requests them through history pagination.
const INITIAL_HISTORY_TURNS = 20

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
      const runtime = await agentApi.getSessionEvents(sessionId, currentLastIndex)
      assertChatIdentityCurrent(identity)
      applySessionStatus(tabId, {
        status: runtime.session_status,
        hasRunningBackgroundAgents: runtime.has_running_background_agents,
        isSyntheticTurn: runtime.is_synthetic_turn,
        canSteer: runtime.can_steer,
      })
      // Every chat uses the same recovery contract: the workspace-backed
      // conversation is the authoritative durable transcript, while the
      // polling endpoint is only the volatile live tail. Do this for all
      // products (and while a turn is running) so a refresh cannot leave a
      // user-only event buffer in the UI. If a legacy session has no durable
      // transcript, retain the live events already loaded above.
      const conversation = await tryFetchChatHistoryConversation(
        sessionId,
        options?.workspacePath || existingTab?.metadata?.agentProfileWorkspace,
      )
      assertChatIdentityCurrent(identity)
      if (conversation) {
        hydrateTabEventsFromConversation(sessionId, conversation, runtime.events)
      } else if (runtime.events.length > 0) {
        chatStore.addTabEvents(sessionId, runtime.events)
      }
      if (runtime.last_processed_index !== undefined) {
        chatStore.setTabLastEventIndex(sessionId, runtime.last_processed_index)
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
    const workspacePath = options?.workspacePath || existingTab?.metadata?.agentProfileWorkspace
    // A bounded live cursor can be rejected after a server restart. The
    // durable transcript is independent of that volatile window and applies
    // equally to every product, so recover it before preserving an incomplete
    // local cache.
    const conversation = await tryFetchChatHistoryConversation(sessionId, workspacePath)
    assertChatIdentityCurrent(identity)
    if (conversation) {
      const restored = hydrateTabEventsFromConversation(sessionId, conversation)
      applySessionStatus(tabId, restored)
      console.log(`${TAG} [${src}] Recovered persisted transcript after runtime sync failure`)
      return tabId
    }
    if (isNotFoundError(err) && existingEventCount > 0) {
      console.log(`${TAG} [${src}] Session ${sessionId} no longer in memory; keeping locally restored events`)
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

// The conversation → events converter lives in the shared session client
// (frontend/shared/session/restore.ts); re-exported so existing imports hold.
export { conversationToRestoredEvents }

// Coding-provider stream events can reach persistence with an empty
// tool_params.arguments, while the same conversation's structured tool-call
// message has the complete arguments. Retain the raw event's timing/result and
// hydrate only that missing input by the stable tool call id.
function restoreToolArgumentsFromConversation(
  events: PollingEvent[],
  conversation: ChatHistoryConversation,
): PollingEvent[] {
  const argumentsByCallID = new Map<string, string>()
  for (const message of conversation.conversation_history || []) {
    for (const part of message.Parts || message.parts || []) {
      if (!part || typeof part !== 'object') continue
      const record = part as Record<string, unknown>
      const callID = typeof record.ID === 'string' ? record.ID : typeof record.id === 'string' ? record.id : ''
      const call = record.FunctionCall ?? record.functionCall ?? record.function_call
      const callRecord = call && typeof call === 'object' ? call as Record<string, unknown> : undefined
      const rawArgs = callRecord?.Arguments ?? callRecord?.arguments ?? callRecord?.args
      const args = typeof rawArgs === 'string' ? rawArgs : rawArgs == null ? '' : JSON.stringify(rawArgs)
      if (callID && args) {
        argumentsByCallID.set(callID, args)
        for (const alias of callID.split(/\s+/).filter(Boolean)) argumentsByCallID.set(alias, args)
      }
    }
  }
  if (argumentsByCallID.size === 0) return events

  return events.map((event) => {
    if (event.type !== 'tool_call_start') return event
    const envelope = event.data
    if (!envelope || typeof envelope !== 'object') return event
    const outer = envelope as Record<string, unknown>
    const nested = outer.data
    if (!nested || typeof nested !== 'object') return event
    const fields = nested as Record<string, unknown>
    const callID = typeof fields.tool_call_id === 'string' ? fields.tool_call_id : ''
    const args = argumentsByCallID.get(callID)
    if (!args) return event
    const existingParams = fields.tool_params && typeof fields.tool_params === 'object'
      ? fields.tool_params as Record<string, unknown>
      : {}
    return {
      ...event,
      data: {
        ...outer,
        data: { ...fields, tool_params: { ...existingParams, arguments: args } },
      },
    } as PollingEvent
  })
}

type HydratedHistoryRuntimeState = RuntimeSessionState & { restoredEvents: PollingEvent[] }

function isSyntheticRestoredEvent(event: PollingEvent): boolean {
  return event.id?.startsWith('restored-') === true
}

function combineTranscriptTraceEvents(...sources: Array<ReadonlyArray<PollingEvent> | undefined>): PollingEvent[] {
  const combined: PollingEvent[] = []
  const seenIDs = new Set<string>()
  for (const source of sources) {
    for (const event of source || []) {
      // A repeated hydration sees synthetic durable conversation carriers in
      // the store. Feeding those generated rows back as raw trace would make
      // their generated timestamps influence the next projection.
      //
      // Keep events marked restored_persisted_trace, though: those are raw
      // transport events with stable IDs and real timestamps. A completion can
      // arrive through SSE before the provider transcript is durable. If a
      // second, older hydration drops that marked trace event, the final reply
      // flashes in Chat and then disappears until a later refresh. Stable IDs
      // make retaining it idempotent across repeated hydration.
      if (isSyntheticRestoredEvent(event)) continue
      if (event.id && seenIDs.has(event.id)) continue
      if (event.id) seenIDs.add(event.id)
      combined.push(event)
    }
  }
  return combined
}

const ACCEPTED_LIVE_INPUT_STATUSES = new Set(['sent_to_cli', 'next_turn_started', 'queued_for_injection'])

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

function durableLiveInputMessageIDs(conversation: ChatHistoryConversation): Set<string> {
  const ids = new Set<string>()
  for (const event of (conversation.ui_events as PollingEvent[] | undefined) || []) {
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
  conversation: ChatHistoryConversation,
): PollingEvent[] {
  const durableMessageIDs = durableLiveInputMessageIDs(conversation)
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

// Keep the newest accepted durable snapshot independently of request completion
// order. Source counts and timestamps also catch an older backend replica
// answering a newer request. Unversioned histories may grow but cannot erase
// previously durable rows; deletion requires an explicit versioned API contract.
const durableSnapshots = new Map<string, ChatHistoryConversation>()
const historyRequestOrder = new WeakMap<ChatHistoryConversation, number>()
let nextHistoryRequest = 0
function monotonicConversation(sessionId: string, incoming: ChatHistoryConversation): ChatHistoryConversation {
  const key = `${captureChatIdentity()}:${sessionId}`
  const current = useChatStore.getState().getTabEvents(sessionId)
  const previous = current.length > 0 ? durableSnapshots.get(key) : undefined
  if (previous) {
    const previousRevision = previous.revision ?? 0
    const incomingRevision = incoming.revision ?? 0
    if (incomingRevision < previousRevision) return previous
    if (incomingRevision > previousRevision) {
      durableSnapshots.set(key, incoming)
      return incoming
    }
    const previousTime = Date.parse(previous.updated_at || '')
    const incomingTime = Date.parse(incoming.updated_at || '')
    const count = (history: ChatHistoryConversation) => history.history_source_message_count
      ?? history.history_pagination?.total_turns ?? history.conversation_history.length
    if ((historyRequestOrder.get(incoming) ?? 0) < (historyRequestOrder.get(previous) ?? 0)
      || (Number.isFinite(previousTime) && Number.isFinite(incomingTime) && incomingTime < previousTime)
      || count(incoming) < count(previous)) return previous
  }
  durableSnapshots.set(key, incoming)
  return incoming
}

function hydrateTabEventsFromConversation(
  sessionId: string,
  conversation: ChatHistoryConversation,
  liveEvents: ReadonlyArray<PollingEvent> = [],
): HydratedHistoryRuntimeState {
  const chatStore = useChatStore.getState()
  const currentEvents = chatStore.getTabEvents(sessionId)
  conversation = monotonicConversation(sessionId, conversation)
  // There is one transcript-ordering boundary. Persisted UI events, raw events
  // already received by SSE, and the current EventStore window all enter the
  // conversation converter together. It can then anchor progress inside the
  // matching durable turn and keep the final assistant carrier after it.
  // Appending a second event source after conversion is incorrect: that made
  // older progress appear below a newer durable final answer after refresh.
  const trace = combineTranscriptTraceEvents(
    conversation.ui_events as PollingEvent[] | undefined,
    currentEvents,
    liveEvents,
  )
  const projectedConversation: ChatHistoryConversation = trace.length > 0
    ? { ...conversation, ui_events: trace }
    : conversation
  const rawEvents = conversationToRestoredEvents(projectedConversation)
  const events = preserveUnconfirmedAcceptedLiveInputs(
    restoreToolArgumentsFromConversation(rawEvents, conversation),
    currentEvents,
    conversation,
  )

  chatStore.setTabEvents(sessionId, events)
  // Restored conversation rows are synthesized from durable history, while
  // tabEventIndices is a cursor into the backend's volatile raw event store.
  // Those sequences are unrelated. Using history.length here can put the
  // cursor ahead of a newly restarted coding-agent stream and permanently
  // hide its tool calls and responses from the formatted view.
  chatStore.setTabLastEventIndex(sessionId, -1)
  chatStore.setTabHasMoreOlderEvents(sessionId, conversation.history_pagination?.has_more ?? false)
  chatStore.setTabHistoryPagination(
    sessionId,
    conversation.history_pagination
      ? {
          hasMore: conversation.history_pagination.has_more,
          nextOffset: conversation.history_pagination.next_offset,
        }
      : null,
  )
  console.info(`${TAG} Hydrated persisted conversation`, {
    sessionId,
    eventCount: events.length,
    source: trace.length > 0 ? 'conversation_history + reconciled_trace' : 'conversation_history',
  })

  return {
    status: 'completed',
    hasRunningBackgroundAgents: false,
    isSyntheticTurn: false,
    canSteer: false,
    restoredEvents: events,
  }
}

async function tryFetchChatHistoryConversation(
  sessionId: string,
  workspacePath?: string,
  includeUiEvents = true,
): Promise<ChatHistoryConversation | null> {
  const requestOrder = ++nextHistoryRequest
  try {
    const conversation = includeUiEvents
      ? await agentApi.getChatHistoryResumeConversation(sessionId, workspacePath, INITIAL_HISTORY_TURNS, 0, true)
      : await agentApi.getChatHistoryResumeConversation(sessionId, workspacePath, INITIAL_HISTORY_TURNS)
    historyRequestOrder.set(conversation, requestOrder)
    return conversation
  } catch (error) {
    if (isNotFoundError(error)) {
      return null
    }
    // Shared observers may read history but cannot resume its owner's session.
    // Only explicitly read-only tabs use this fallback; the history endpoint
    // still enforces workspace read access.
    const readOnly = Object.values(useChatStore.getState().chatTabs || {}).some(
      tab => tab.sessionId === sessionId && tab.metadata?.isViewOnly,
    )
    if (readOnly && axios.isAxiosError(error) && error.response?.status === 403) {
      return agentApi.getChatHistoryConversation(sessionId, workspacePath)
    }
    throw error
  }
}

/**
 * Load events from the in-memory polling API and hydrate a tab's event state.
 * If the server restarted and no longer has the session in memory, restore
 * displayable conversation history from the workspace-backed chat history file.
 */
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

  // These two reads never depend on each other's result -- the durable-history
  // fetch is preferred "regardless" of what the live event store returns (see
  // below), and the live-store fetch's only use of the history result is in
  // the NotFound catch branch. Firing them together instead of one-after-
  // another halves this function's network latency on every call.
  const eventsPromise = agentApi.getRecentSessionEvents(sessionId).then(
      (value) => ({ ok: true as const, value }),
      (error: unknown) => ({ ok: false as const, error }),
    )
  const conversationPromise = tryFetchChatHistoryConversation(
    sessionId,
    options.workspacePath,
    options.includeUiEvents,
  )
  const [eventsOutcome, conversation] = await Promise.all([eventsPromise, conversationPromise])
  assertChatIdentityCurrent(identity)

  if (!eventsOutcome.ok) {
    if (isNotFoundError(eventsOutcome.error)) {
      console.log(`${TAG} Polling session ${sessionId} not found; restoring from workspace chat history`)
      if (conversation) return hydrateTabEventsFromConversation(sessionId, conversation)
    }
    throw eventsOutcome.error
  }
  const response = eventsOutcome.value

  // The event store is a short-lived transport cache and can contain only
  // prompts after a browser reload. Prefer the durable conversation for every
  // session, regardless of its owning product. Runtime status remains
  // authoritative so a currently running turn still renders as streaming.
  if (conversation) {
    const restored = hydrateTabEventsFromConversation(sessionId, conversation, response.events)
    if (response.last_processed_index !== undefined) {
      chatStore.setTabLastEventIndex(sessionId, response.last_processed_index)
    }
    return {
      status: response.session_status || restored.status,
      hasRunningBackgroundAgents: response.has_running_background_agents,
      isSyntheticTurn: response.is_synthetic_turn,
      canSteer: response.can_steer,
    }
  }

  if (response.events.length > 0) {
    // A missing durable-history response must not replace an already restored
    // transcript with the server's bounded live tail.
    if (chatStore.getTabEvents(sessionId).length > 0) chatStore.addTabEvents(sessionId, response.events)
    else chatStore.setTabEvents(sessionId, response.events)
    // This is a live event window, not a paged durable conversation. A cursor
    // left over from an earlier resume must not offer unrelated history here.
    chatStore.setTabHistoryPagination(sessionId, null)
    const lastIndex = response.last_processed_index ?? (response.events.length - 1)
    chatStore.setTabLastEventIndex(sessionId, lastIndex)
    if (response.has_more !== undefined) {
      chatStore.setTabHasMoreOlderEvents(sessionId, response.has_more)
    }
  } else if (options.fallbackToChatHistory) {
    // A restored terminal can recreate an in-memory session shell whose status
    // is "completed" but whose event buffer is empty. Status therefore cannot
    // tell us whether the durable transcript exists. The caller only enables
    // this fallback for an explicitly restored chat, so prefer its persisted
    // history whenever the volatile event buffer has no events.
    const fallbackConversation = await tryFetchChatHistoryConversation(sessionId, options.workspacePath, options.includeUiEvents)
    assertChatIdentityCurrent(identity)
    if (fallbackConversation) {
      const restored = hydrateTabEventsFromConversation(sessionId, fallbackConversation)
      return {
        status: response.session_status || restored.status,
        hasRunningBackgroundAgents: response.has_running_background_agents,
        isSyntheticTurn: response.is_synthetic_turn,
        canSteer: response.can_steer,
      }
    }
  }
  return {
    status: response.session_status,
    hasRunningBackgroundAgents: response.has_running_background_agents,
    isSyntheticTurn: response.is_synthetic_turn,
    canSteer: response.can_steer,
  }
}
