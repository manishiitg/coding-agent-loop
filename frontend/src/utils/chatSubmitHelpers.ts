/**
 * Pure helper functions extracted from ChatArea.tsx submitQueryWithQuery
 * and WorkflowLayout.tsx handleStartPhase to reduce complexity.
 */

import type { PollingEvent, ExtendedLLMConfiguration, AgentQueryRequest, ExecutionOptions } from '../services/api-types'
import type { ChatTab } from '../stores/useChatStore'
import type { ModeCategory } from '../stores/useModeStore'
import { useChatStore } from '../stores/useChatStore'
import { useGlobalPresetStore } from '../stores/useGlobalPresetStore'
import { useAppStore } from '../stores/useAppStore'
import { useWorkflowStore } from '../stores/useWorkflowStore'
import { useImageGenStore } from '../stores/useImageGenStore'
import { logger } from './logger'

// Workflow phases that support conversational chat mode instead of blocking human_feedback
const CHAT_COMPATIBLE_PHASES = new Set([
  'workflow-builder',
])


export function isChatCompatiblePhase(phaseId: string | undefined): boolean {
  return !!phaseId && CHAT_COMPATIBLE_PHASES.has(phaseId)
}

// ---------------------------------------------------------------------------
// 1a. determineModeFlag — deduplicate useCodeExecutionMode
// ---------------------------------------------------------------------------

export function determineModeFlag(params: {
  correctAgentMode: string
  selectedModeCategory: string
  presetValue: boolean | undefined
  tabConfigValue: boolean | undefined
}): boolean | undefined {
  const { correctAgentMode, selectedModeCategory, presetValue, tabConfigValue } = params

  if (correctAgentMode === 'multi-agent') {
    // Multi-agent mode: preset wins, else tab config (default false)
    if (presetValue !== undefined) return presetValue
    if (selectedModeCategory === 'multi-agent') {
      return tabConfigValue ?? false
    }
    return false
  }

  if (correctAgentMode === 'workflow') {
    return presetValue
  }

  return undefined
}

// ---------------------------------------------------------------------------
// 1b. buildLLMConfigWithApiKeys
// ---------------------------------------------------------------------------

export function buildLLMConfigWithApiKeys(
  effectiveLLMConfig: ExtendedLLMConfiguration,
  _providerConfigs?: Record<string, ExtendedLLMConfiguration>
): ExtendedLLMConfiguration & { api_keys: Record<string, unknown> } {
  // API keys are now loaded server-side from workspace encrypted store.
  // No keys are sent from the frontend.
  return {
    ...effectiveLLMConfig,
    api_keys: {},
  }
}

// ---------------------------------------------------------------------------
// 1c. buildQueryRequestPayload
// ---------------------------------------------------------------------------

export function buildQueryRequestPayload(params: {
  queryWithContext: string
  correctAgentMode: string
  selectedModeCategory: ModeCategory
  enabledTools: Array<{ name: string }>
  effectiveServers: string[]
  currentTab: ChatTab
  effectiveLLMConfig: ExtendedLLMConfiguration
  llmConfigWithApiKeys: ExtendedLLMConfiguration & { api_keys: Record<string, unknown> }
  useCodeExecutionMode: boolean | undefined
  executionOptions: unknown | undefined
  workflowPresetId: string | null
  chatPresetId: string | null
  filteredPresetTools: string[]
  hasActivePreset: boolean
  decryptedSecrets?: Array<{ name: string; value: string }>
  selectedGlobalSecrets?: string[] | null
  restoredConversationPath?: string
}): AgentQueryRequest {
  const {
    queryWithContext, correctAgentMode, selectedModeCategory,
    enabledTools, effectiveServers, currentTab, effectiveLLMConfig,
    llmConfigWithApiKeys, useCodeExecutionMode,
    executionOptions, workflowPresetId, chatPresetId,
    filteredPresetTools, hasActivePreset, decryptedSecrets,
    selectedGlobalSecrets,
    restoredConversationPath,
  } = params

  const isMultiAgentMode = selectedModeCategory === 'multi-agent'
  // Detect workflow phase chat mode: tab has a phaseId and the phase supports conversational editing
  const isWorkflowPhaseChat = selectedModeCategory === 'workflow'
    && currentTab?.metadata?.phaseId
    && CHAT_COMPATIBLE_PHASES.has(currentTab.metadata.phaseId)
  // isChatLikeMode: includes phase chat for basic settings (context summarization, workspace access)
  const isChatLikeMode = isMultiAgentMode || isWorkflowPhaseChat
  // isChatWithExtras: only multi-agent mode gets optional extras (browser, skills, secrets, etc.)
  const isChatWithExtras = isMultiAgentMode

  // Context editing from workflow preset
  let enableContextEditing: boolean | undefined = undefined
  if (selectedModeCategory === 'workflow') {
    const presetStore = useGlobalPresetStore.getState()
    const presetId = presetStore.activePresetIds.workflow
    const preset = presetId
      ? presetStore.workflowPresets.find(p => p.id === presetId)
      : null
    if (preset?.llmConfig?.enable_context_editing === false) {
      enableContextEditing = false
    }
  }

  // Browser mode can drift on resumed/migrated tabs when older fields exist.
  // Derive a robust effective mode so request payloads are consistent.
  const rawBrowserMode = currentTab?.config?.browserMode
  const legacyUseCdp = currentTab?.config?.useCdp === true
  const legacyEnableBrowser = currentTab?.config?.enableBrowserAccess === true
  const selectedServers = currentTab?.config?.selectedServers || []
  let effectiveBrowserMode: 'none' | 'headless' | 'cdp' | 'playwright' =
    rawBrowserMode
      ? rawBrowserMode
      : (legacyEnableBrowser
          ? (legacyUseCdp ? 'cdp' : 'headless')
          : (selectedServers.includes('playwright') ? 'playwright' : 'none'))

  // Guard against stale/migrated config where browserMode says headless
  // but useCdp is actually enabled in tab config.
  if (effectiveBrowserMode === 'headless' && legacyUseCdp) {
    effectiveBrowserMode = 'cdp'
  }

  const isBrowserAccessMode = effectiveBrowserMode === 'headless' || effectiveBrowserMode === 'cdp'
  const payloadServers = isBrowserAccessMode
    ? effectiveServers.filter(s => s !== 'playwright')
    : effectiveServers

  return {
    query: queryWithContext,
    agent_mode: (isWorkflowPhaseChat
        ? 'workflow_phase'
        : correctAgentMode) as AgentQueryRequest['agent_mode'],
    phase_id: isWorkflowPhaseChat ? currentTab.metadata!.phaseId : undefined,
    enabled_tools: enabledTools.map(tool => tool.name),
    enabled_servers: payloadServers,
    selected_tools: hasActivePreset ? filteredPresetTools : undefined,
    provider: effectiveLLMConfig.provider as AgentQueryRequest['provider'],
    model_id: effectiveLLMConfig.model_id,
    llm_config: llmConfigWithApiKeys as AgentQueryRequest['llm_config'],
    preset_query_id: workflowPresetId || chatPresetId || undefined,
    use_code_execution_mode: correctAgentMode === 'multi-agent' ? (useCodeExecutionMode ?? false) : useCodeExecutionMode,
    execution_options: executionOptions as AgentQueryRequest['execution_options'],
    enable_context_summarization: isChatLikeMode ? true : undefined,
    summarize_on_max_turns: isChatLikeMode ? true : undefined,
    summary_keep_last_messages: isChatLikeMode ? 4 : undefined,
    enable_browser_access: isChatWithExtras
      ? isBrowserAccessMode
      : undefined,
    browser_mode: isChatWithExtras
      ? effectiveBrowserMode
      : undefined,
    cdp_port: isChatWithExtras && effectiveBrowserMode === 'cdp'
      ? (currentTab?.config?.cdpPort || 9222)
      : undefined,
    delegation_tier_config: undefined,
    selected_skills: isChatWithExtras && currentTab?.config?.selectedSkills?.length
      ? currentTab.config.selectedSkills
      : undefined,
    enable_context_editing: enableContextEditing,
    decrypted_secrets: (isChatWithExtras || selectedModeCategory === 'workflow') && decryptedSecrets?.length ? decryptedSecrets : undefined,
    selected_global_secrets: (isChatWithExtras || selectedModeCategory === 'workflow')
      ? (selectedGlobalSecrets ?? undefined)
      : undefined,
    workflow_context_paths: (isChatWithExtras || selectedModeCategory === 'workflow') && currentTab?.config?.workflowContext?.length
      ? currentTab.config.workflowContext.map(w => w.workspacePath)
      : undefined,
    restored_conversation_path: restoredConversationPath?.trim() || undefined,
    enable_image_generation: isChatWithExtras ? (currentTab?.config?.enableImageGeneration ?? false) : undefined,
    image_gen_config: (() => {
      if (!isChatWithExtras) return undefined
      const imageGenConfig = useImageGenStore.getState().config
      const cfg = {
        provider: imageGenConfig.provider,
        model_id: imageGenConfig.modelId,
        api_key: imageGenConfig.apiKey || undefined,
      }
      console.log('[IMAGE_GEN] sending image_gen_config:', JSON.stringify(cfg))
      return cfg
    })(),
  }
}

// ---------------------------------------------------------------------------
// 1d. resolveOrCreateTab — tab resolution + session ID guarantee for multi-agent
// ---------------------------------------------------------------------------

export async function resolveOrCreateTab(params: {
  freshActiveTab: ChatTab | undefined
  selectedModeCategory: ModeCategory
}): Promise<{ tab: ChatTab; sessionId: string } | null> {
  const { freshActiveTab, selectedModeCategory } = params
  let currentTab = freshActiveTab

  if (!currentTab && selectedModeCategory === 'multi-agent') {
    const chatStore = useChatStore.getState()
    const tabs = Object.values(chatStore.chatTabs).filter(tab =>
      tab.metadata?.mode === selectedModeCategory
    )

    if (tabs.length === 0) {
      try {
        const tabName = 'Agent Chat 1'
        const newTabId = await chatStore.createChatTab(tabName, {
          mode: selectedModeCategory,
        })
        currentTab = chatStore.getTab(newTabId)
        logger.debug('ChatArea', `Created new ${selectedModeCategory} tab: ${newTabId}`)
      } catch (error) {
        logger.error('ChatArea', `Failed to create ${selectedModeCategory} tab:`, error)
        return null
      }
    } else {
      currentTab = chatStore.getActiveTab() || tabs[0]
    }
  }

  if (!currentTab) {
    logger.error('ChatArea', 'No currentTab — cannot submit query')
    return null
  }

  // Ensure session ID exists
  let sessionId = currentTab.sessionId
  if (!sessionId) {
    sessionId = globalThis.crypto.randomUUID()
    const chatStore = useChatStore.getState()
    chatStore.updateTabSessionId(currentTab.tabId, sessionId)
    currentTab = { ...currentTab, sessionId }
    logger.debug('ChatArea', `Generated session ID for tab ${currentTab.tabId}: ${sessionId}`)
  }

  return { tab: currentTab, sessionId }
}

// ---------------------------------------------------------------------------
// 1e. findOrCreateWorkflowTab — single-pass lookup for workflow phases
// ---------------------------------------------------------------------------

export async function findOrCreateWorkflowTab(params: {
  phaseId: string
  activePresetId: string
  phaseName: string
}): Promise<{ tabId: string; tab: ChatTab; isReusingTab: boolean } | null> {
  const { phaseId, activePresetId, phaseName } = params
  const chatStore = useChatStore.getState()
  const { getTabsByPhaseId, getTabStreamingStatus, switchTab, getActiveTab, createChatTab: createTab } = chatStore

  // Single pass: get all tabs for this phase scoped to the active preset
  const existingPhaseTabs = getTabsByPhaseId(phaseId, activePresetId)

  // Prefer streaming tab, then newest
  const runningTab = existingPhaseTabs.find(t => getTabStreamingStatus(t.tabId))
  const newestTab = existingPhaseTabs.length > 0
    ? existingPhaseTabs.sort((a, b) => b.createdAt - a.createdAt)[0]
    : null

  // Fallback: legacy tabs without presetQueryId that match the phase
  const legacyTab = !runningTab && !newestTab
    ? Object.values(chatStore.chatTabs).find(t =>
        t.metadata?.mode === 'workflow' &&
        t.metadata?.phaseId === phaseId &&
        !t.metadata?.presetQueryId
      )
    : null

  const existingTab = runningTab || newestTab || legacyTab

  if (existingTab) {
    logger.debug('WorkflowLayout', `Reusing tab ${existingTab.tabId} for phase ${phaseId}`)
    switchTab(existingTab.tabId)
    const tab = getActiveTab()
    if (!tab) return null
    return { tabId: existingTab.tabId, tab, isReusingTab: true }
  }

  // Create new tab
  try {
    logger.debug('WorkflowLayout', `Creating new tab for phase ${phaseId}, preset ${activePresetId}`)
    const tabId = await createTab(phaseName, {
      mode: 'workflow',
      phaseId,
      phaseName,
      presetQueryId: activePresetId || undefined
    })
    const tab = getActiveTab()
    if (!tab) return null
    return { tabId, tab, isReusingTab: false }
  } catch (error) {
    logger.error('WorkflowLayout', 'Failed to create workflow tab:', error)
    return null
  }
}

// ---------------------------------------------------------------------------
// 1f. createUserMessageEvent — typed factory replacing `as any` cast
// ---------------------------------------------------------------------------

export function createUserMessageEvent(content: string, eventIndex?: number, timestamp?: string): PollingEvent {
  const eventTimestamp = timestamp ?? new Date().toISOString()
  return {
    id: `user-message-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`,
    type: 'user_message',
    timestamp: eventTimestamp,
    ...(typeof eventIndex === 'number' ? { event_index: eventIndex } : {}),
    data: {
      type: 'user_message',
      timestamp: eventTimestamp,
      data: {
        content,
        timestamp: eventTimestamp
      }
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    } as any
  }
}

// Synthetic separator injected when a follow-up is sent on a restored session.
// EventHierarchy uses this to collapse all events above it.
export function createConversationResumedEvent(previousEventCount: number): PollingEvent {
  return {
    id: `conversation-resumed-${Date.now()}`,
    type: 'conversation_resumed',
    timestamp: new Date().toISOString(),
    data: {
      type: 'conversation_resumed',
      data: {
        previous_event_count: previousEventCount,
        timestamp: new Date().toISOString(),
      }
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    } as any,
  }
}

// ---------------------------------------------------------------------------
// computeNewEventCount — pure function for WorkflowChatTabs badge count
// ---------------------------------------------------------------------------

export function computeNewEventCount(
  tab: ChatTab,
  isActive: boolean,
  tabEvents: Record<string, PollingEvent[]>,
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  shouldShowEventByMode: (type: string, mode?: any) => boolean
): number {
  if (isActive || !tab.sessionId) return 0

  const allEvents = tabEvents[tab.sessionId] || []
  const visibleEvents = allEvents.filter(e => e.type && shouldShowEventByMode(e.type))
  const lastViewedCount = tab.lastViewedEventCounts?.micro ?? tab.lastViewedEventCount ?? 0

  return Math.max(0, visibleEvents.length - lastViewedCount)
}

// ---------------------------------------------------------------------------
// validateExecutionGroups — check enabled_group_names for workflow mode
// ---------------------------------------------------------------------------

export function validateExecutionGroups(
  executionOptions: ExecutionOptions | undefined
): string | null {
  if (!executionOptions) return null

  const workflowStore = useWorkflowStore.getState()
  const variablesManifest = workflowStore.variablesManifest

  if (!variablesManifest?.groups || variablesManifest.groups.length === 0) {
    return 'Please create and enable at least one group before using workflow builder or execution.'
  }

  const enabledGroupNames = executionOptions.enabled_group_names
  if (!enabledGroupNames || enabledGroupNames.length === 0) {
    return 'Please select at least one group before using workflow builder or execution.'
  }

  return null
}
