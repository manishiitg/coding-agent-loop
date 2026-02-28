import type { ChatTabConfig } from '../stores/useChatStore'
import type { ChatSessionConfig, ExtendedLLMConfiguration } from '../services/api-types'
import { useLLMStore } from '../stores/useLLMStore'
import { useChatStore } from '../stores/useChatStore'
import { useModeStore } from '../stores/useModeStore'
import { agentApi } from '../services/api'
import { truncateTabTitle } from './textUtils'

const TAG = '[SessionRestore]'

/**
 * Per-session async lock to prevent duplicate restores.
 * If restoreSession is called concurrently for the same session,
 * subsequent calls return the existing Promise.
 */
const restoreInProgress = new Map<string, Promise<string>>()

/**
 * Apply session status (completed/streaming/restored) to a tab.
 */
function applySessionStatus(tabId: string, status: string): void {
  const chatStore = useChatStore.getState()
  const isDone = status === 'completed' || status === 'stopped'
  const isError = status === 'error'
  chatStore.setTabCompleted(tabId, isDone)
  chatStore.setTabStreaming(tabId, !isDone && !isError)
  if (isDone || isError) {
    chatStore.setTabMetadata(tabId, { isRestored: true })
  }
}

/**
 * Unified session restoration function.
 * Handles all restore flows: auto-restore, page-refresh hydration, sidebar click, resume dialog.
 *
 * Returns the tabId for the restored session.
 */
export async function restoreSession(
  sessionId: string,
  options?: {
    title?: string
    source?: string
    skipConfigRestore?: boolean
  }
): Promise<string> {
  // Async lock: if already restoring this session, return the existing promise
  const existing = restoreInProgress.get(sessionId)
  if (existing) {
    console.log(`${TAG} Dedup hit for ${sessionId} (source=${options?.source}), returning existing promise`)
    return existing
  }

  const promise = doRestoreSession(sessionId, options)
  restoreInProgress.set(sessionId, promise)

  try {
    return await promise
  } finally {
    restoreInProgress.delete(sessionId)
  }
}

async function doRestoreSession(
  sessionId: string,
  options?: {
    title?: string
    source?: string
    skipConfigRestore?: boolean
  }
): Promise<string> {
  const src = options?.source || 'unknown'
  console.log(`${TAG} Start session=${sessionId} source=${src} title=${options?.title ?? '(none)'}`)
  const chatStore = useChatStore.getState()

  // Step 1: Check for existing tab with events already loaded
  const existingTab = Object.values(chatStore.chatTabs).find(tab => tab.sessionId === sessionId)
  if (existingTab) {
    const existingEvents = chatStore.getTabEvents(sessionId)
    if (existingEvents.length > 0) {
      console.log(`${TAG} [${src}] Tab ${existingTab.tabId} already has ${existingEvents.length} events, returning early`)
      return existingTab.tabId
    }
    console.log(`${TAG} [${src}] Tab ${existingTab.tabId} exists but has 0 events, will hydrate`)
  }

  // Step 2: Fetch session details
  let chatSession: Awaited<ReturnType<typeof agentApi.getChatSession>> | null = null
  try {
    chatSession = await agentApi.getChatSession(sessionId)
    console.log(`${TAG} [${src}] Fetched session: status=${chatSession.status}, hasConfig=${!!chatSession.config}, delegation_mode=${chatSession.config?.delegation_mode ?? 'none'}`)
  } catch (err) {
    console.error(`${TAG} [${src}] Failed to fetch session ${sessionId}:`, err)
  }

  // Step 3: Detect mode
  const config = chatSession?.config
  const isMultiAgent = config?.delegation_mode === 'plan'
  const tabMode = isMultiAgent ? 'multi-agent' as const : 'chat' as const
  if (isMultiAgent) {
    console.log(`${TAG} [${src}] Switching to multi-agent mode`)
    useModeStore.getState().setModeCategory('multi-agent')
  }

  // Step 4: Create or reuse tab
  let tabId: string
  if (existingTab) {
    tabId = existingTab.tabId
    console.log(`${TAG} [${src}] Reusing existing tab ${tabId}`)
  } else {
    const title = truncateTabTitle(options?.title || chatSession?.title || 'Chat')
    const isRestored = chatSession?.status === 'completed' || chatSession?.status === 'stopped' || chatSession?.status === 'error'
    tabId = await chatStore.createChatTab(
      title,
      { mode: tabMode, isRestored: isRestored ?? false },
      sessionId
    )
    console.log(`${TAG} [${src}] Created tab ${tabId} mode=${tabMode} isRestored=${isRestored}`)
  }

  // Step 5: Apply status IMMEDIATELY after tab creation to prevent polling from
  // overriding with stale 'running' status while we load events asynchronously.
  if (chatSession) {
    applySessionStatus(tabId, chatSession.status)
    console.log(`${TAG} [${src}] Applied status=${chatSession.status} to tab ${tabId}`)
  }

  // Step 6: Restore config (skip if config already persisted in localStorage)
  if (!options?.skipConfigRestore && config) {
    const configUpdate = buildTabConfigFromSession(config)
    const keys = Object.keys(configUpdate)
    if (keys.length > 0) {
      chatStore.setTabConfig(tabId, configUpdate)
      console.log(`${TAG} [${src}] Restored config keys: ${keys.join(', ')}`)
    }
  } else if (options?.skipConfigRestore) {
    console.log(`${TAG} [${src}] Skipped config restore (already persisted)`)
  }

  // Step 7: Load events
  try {
    await hydrateTabEvents(sessionId)
    const eventCount = chatStore.getTabEvents(sessionId).length
    console.log(`${TAG} [${src}] Hydrated ${eventCount} events`)
  } catch (err) {
    console.error(`${TAG} [${src}] Failed to load events for ${sessionId}:`, err)
  }

  console.log(`${TAG} [${src}] Done session=${sessionId} tab=${tabId}`)
  return tabId
}

/**
 * Build a partial ChatTabConfig from a stored session config.
 * Centralizes the config restoration logic used across all restore flows.
 */
export function buildTabConfigFromSession(config: ChatSessionConfig): Partial<ChatTabConfig> {
  const configUpdate: Partial<ChatTabConfig> = {}

  // Restore selected servers (prefer enabled_servers over selected_servers)
  if (config.enabled_servers && config.enabled_servers.length > 0) {
    configUpdate.selectedServers = config.enabled_servers
  } else if (config.selected_servers && config.selected_servers.length > 0) {
    configUpdate.selectedServers = config.selected_servers
  }

  // Restore code execution mode
  if (config.use_code_execution_mode !== undefined) {
    configUpdate.useCodeExecutionMode = config.use_code_execution_mode
  }

  // Restore context summarization
  if (config.enable_context_summarization !== undefined) {
    configUpdate.enableContextSummarization = config.enable_context_summarization
  }

  // Restore LLM config
  if (config.llm_config) {
    let provider = config.llm_config.provider as string
    if (!provider || provider === '.' || provider.trim() === '') {
      provider = useLLMStore.getState().primaryConfig.provider || 'openai'
    }
    let modelId = config.llm_config.model_id || ''
    if (!modelId || modelId.trim() === '') {
      modelId = useLLMStore.getState().primaryConfig.model_id || ''
    }
    const llmConfig: ExtendedLLMConfiguration = {
      provider: provider as 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure',
      model_id: modelId,
      fallback_models: config.llm_config.fallback_models || [],
    }
    if (config.llm_config.cross_provider_fallback) {
      llmConfig.cross_provider_fallback = {
        provider: config.llm_config.cross_provider_fallback.provider as 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure',
        models: config.llm_config.cross_provider_fallback.models || [],
      }
    }
    configUpdate.llmConfig = llmConfig
  }

  // Restore workspace file context
  if (config.file_context && Array.isArray(config.file_context)) {
    configUpdate.fileContext = config.file_context.map((item) => ({
      name: item.name || item.path || '',
      path: item.path || '',
      type: (item.type === 'folder' ? 'folder' : 'file') as 'file' | 'folder',
    }))
  }

  // Restore workspace access setting
  if (config.enable_workspace_access !== undefined) {
    configUpdate.enableWorkspaceAccess = config.enable_workspace_access
  }

  // Restore selected skills
  if (config.selected_skills && Array.isArray(config.selected_skills)) {
    configUpdate.selectedSkills = config.selected_skills
  }

  // Restore selected sub-agent templates
  if (config.selected_subagents && Array.isArray(config.selected_subagents)) {
    configUpdate.selectedSubAgents = config.selected_subagents
  }

  // Restore delegation tier config (for multi-agent sessions)
  if (config.delegation_tier_config) {
    configUpdate.delegationTierConfig = config.delegation_tier_config
  }

  return configUpdate
}

/**
 * Load events from the backend and hydrate a tab's event state.
 * Centralizes the event loading logic used across restore flows.
 *
 * Uses the in-memory polling API first (for active sessions).
 * Falls back to the database API (for completed/historical sessions).
 */
export async function hydrateTabEvents(
  sessionId: string,
): Promise<void> {
  const chatStore = useChatStore.getState()

  // Try the in-memory polling API first (works for active sessions)
  const response = await agentApi.getSessionEvents(sessionId, -1)

  if (response.events.length > 0) {
    chatStore.setTabEvents(sessionId, response.events)
    const lastIndex = response.last_processed_index ?? (response.events.length > 0 ? response.events.length - 1 : -1)
    chatStore.setTabLastEventIndex(sessionId, lastIndex)
    if (response.has_more !== undefined) {
      chatStore.setTabHasMoreOlderEvents(sessionId, response.has_more)
    }
    return
  }

  // Polling API returned 0 events — session is likely completed/historical.
  // Fall back to the database API.
  const dbResponse = await agentApi.getChatSessionEvents(sessionId, 1000, 0)
  if (dbResponse.events.length > 0) {
    chatStore.setTabEvents(sessionId, dbResponse.events)
    chatStore.setTabLastEventIndex(sessionId, dbResponse.events.length - 1)
    chatStore.setTabHasMoreOlderEvents(sessionId, dbResponse.total > dbResponse.events.length)
  }
}
