/**
 * Pure helper functions extracted from ChatArea.tsx submitQueryWithQuery
 * and WorkflowLayout.tsx handleStartPhase to reduce complexity.
 */

import type { PollingEvent, ExtendedLLMConfiguration, AgentQueryRequest, ExecutionOptions } from '../services/api-types'
import type { ChatTab } from '../stores/useChatStore'
import type { ModeCategory } from '../stores/useModeStore'
import { useChatStore } from '../stores/useChatStore'
import { useLLMStore } from '../stores'
import { useGlobalPresetStore } from '../stores/useGlobalPresetStore'
import { useAppStore } from '../stores/useAppStore'
import { useWorkflowStore } from '../stores/useWorkflowStore'
import { useImageGenStore } from '../stores/useImageGenStore'
import { logger } from './logger'

// Workflow phases that support conversational chat mode instead of blocking human_feedback
const CHAT_COMPATIBLE_PHASES = new Set([
  'planning',
  'plan-improvement',
  'execution-debugger',
  'evaluation-debugger',
  'code-exec-debugging',
])

// NOTE: Backend support (server.go workflow_phase handler) currently only handles
// planning and plan-improvement phases. The debugger phases need their own
// system prompt templates and tools to be added in server.go before they'll
// work end-to-end. The frontend is ready for all phases listed above.

export function isChatCompatiblePhase(phaseId: string | undefined): boolean {
  return !!phaseId && CHAT_COMPATIBLE_PHASES.has(phaseId)
}

// ---------------------------------------------------------------------------
// 1a. determineModeFlag — deduplicate useCodeExecutionMode / useToolSearchMode
// ---------------------------------------------------------------------------

export function determineModeFlag(params: {
  correctAgentMode: string
  selectedModeCategory: string
  presetValue: boolean | undefined
  tabConfigValue: boolean | undefined
}): boolean | undefined {
  const { correctAgentMode, selectedModeCategory, presetValue, tabConfigValue } = params

  if (correctAgentMode === 'simple') {
    // Chat mode: preset wins, else tab config (default false)
    if (presetValue !== undefined) return presetValue
    if (selectedModeCategory === 'chat' || selectedModeCategory === 'multi-agent') {
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
  providerConfigs?: Record<string, ExtendedLLMConfiguration>
): ExtendedLLMConfiguration & { api_keys: Record<string, unknown> } {
  const configs = providerConfigs ?? (() => {
    const store = useLLMStore.getState()
    return {
      openrouter: store.openrouterConfig,
      openai: store.openaiConfig,
      anthropic: store.anthropicConfig,
      vertex: store.vertexConfig,
      bedrock: store.bedrockConfig,
      azure: store.azureConfig,
      minimax: store.minimaxConfig,
      'minimax-coding-plan': store.minimaxCodingPlanConfig,
    }
  })()

  const or = configs.openrouter as ExtendedLLMConfiguration | undefined
  const oi = configs.openai as ExtendedLLMConfiguration | undefined
  const an = configs.anthropic as ExtendedLLMConfiguration | undefined
  const vx = configs.vertex as ExtendedLLMConfiguration | undefined
  const br = configs.bedrock as ExtendedLLMConfiguration | undefined
  const az = configs.azure as ExtendedLLMConfiguration | undefined
  const mm = configs.minimax as ExtendedLLMConfiguration | undefined
  const mmcp = configs['minimax-coding-plan'] as ExtendedLLMConfiguration | undefined

  return {
    ...effectiveLLMConfig,
    api_keys: {
      ...(or?.api_key ? { openrouter: or.api_key } : {}),
      ...(oi?.api_key ? { openai: oi.api_key } : {}),
      ...(an?.api_key ? { anthropic: an.api_key } : {}),
      ...(vx?.api_key ? { vertex: vx.api_key } : {}),
      ...(br?.region ? { bedrock: { region: br.region } } : {}),
      ...(az?.endpoint && az?.api_key
        ? { azure: { endpoint: az.endpoint, api_key: az.api_key, api_version: (az.options?.api_version as string) || undefined, region: az.region || undefined } }
        : {}),
      ...(mm?.api_key ? { minimax: mm.api_key } : {}),
      ...(mmcp?.api_key ? { 'minimax-coding-plan': mmcp.api_key } : {}),
      ...(() => {
        const geminiKey = useLLMStore.getState().geminiCliApiKey
        return geminiKey ? { gemini_cli: geminiKey } : {}
      })(),
    }
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
  useToolSearchMode: boolean | undefined
  executionOptions: unknown | undefined
  workflowPresetId: string | null
  chatPresetId: string | null
  filteredPresetTools: string[]
  hasActivePreset: boolean
  effectivePlanPhase?: string
  decryptedSecrets?: Array<{ name: string; value: string }>
  selectedGlobalSecrets?: string[]
}): AgentQueryRequest {
  const {
    queryWithContext, correctAgentMode, selectedModeCategory,
    enabledTools, effectiveServers, currentTab, effectiveLLMConfig,
    llmConfigWithApiKeys, useCodeExecutionMode, useToolSearchMode,
    executionOptions, workflowPresetId, chatPresetId,
    filteredPresetTools, hasActivePreset, effectivePlanPhase, decryptedSecrets,
    selectedGlobalSecrets,
  } = params

  const isChatMode = selectedModeCategory === 'chat'
  const isMultiAgentMode = selectedModeCategory === 'multi-agent'
  // Detect workflow phase chat mode: tab has a phaseId and the phase supports conversational editing
  const isWorkflowPhaseChat = selectedModeCategory === 'workflow'
    && currentTab?.metadata?.phaseId
    && CHAT_COMPATIBLE_PHASES.has(currentTab.metadata.phaseId)
  // isChatLikeMode: includes phase chat for basic settings (context summarization, workspace access)
  const isChatLikeMode = isChatMode || isMultiAgentMode || isWorkflowPhaseChat
  // isChatWithExtras: only real chat/multi-agent modes get optional extras (browser, GWS, skills, secrets, etc.)
  const isChatWithExtras = isChatMode || isMultiAgentMode

  // Context editing from workflow preset
  let enableContextEditing: boolean | undefined = undefined
  if (selectedModeCategory === 'workflow') {
    const presetStore = useGlobalPresetStore.getState()
    const presetId = presetStore.activePresetIds.workflow
    const preset = presetId
      ? presetStore.customPresets.find(p => p.id === presetId)
        || presetStore.predefinedPresets.find(p => p.id === presetId)
      : null
    if (preset?.llmConfig?.enable_context_editing === false) {
      enableContextEditing = false
    }
  }

  return {
    query: queryWithContext,
    agent_mode: (isWorkflowPhaseChat ? 'workflow_phase' : correctAgentMode) as AgentQueryRequest['agent_mode'],
    phase_id: isWorkflowPhaseChat ? currentTab.metadata!.phaseId : undefined,
    enabled_tools: enabledTools.map(tool => tool.name),
    enabled_servers: effectiveServers,
    selected_tools: hasActivePreset ? filteredPresetTools : undefined,
    provider: effectiveLLMConfig.provider,
    model_id: effectiveLLMConfig.model_id,
    llm_config: llmConfigWithApiKeys as AgentQueryRequest['llm_config'],
    preset_query_id: workflowPresetId || chatPresetId || undefined,
    use_code_execution_mode: correctAgentMode === 'simple' ? (useCodeExecutionMode ?? false) : useCodeExecutionMode,
    use_tool_search_mode: correctAgentMode === 'simple' ? (useToolSearchMode ?? false) : useToolSearchMode,
    execution_options: executionOptions as AgentQueryRequest['execution_options'],
    enable_context_summarization: isChatLikeMode ? true : undefined,
    summarize_on_max_turns: isChatLikeMode ? true : undefined,
    summary_keep_last_messages: isChatLikeMode ? 4 : undefined,
    enable_workspace_access: isChatLikeMode
      ? (currentTab?.config?.enableWorkspaceAccess ?? true)
      : undefined,
    enable_browser_access: isChatWithExtras
      ? ((currentTab?.config?.browserMode ?? 'none') === 'headless' || (currentTab?.config?.browserMode ?? 'none') === 'cdp')
      : undefined,
    enable_gws_access: isChatWithExtras
      ? (currentTab?.config?.enableGWSAccess ?? false)
      : undefined,
    cdp_port: isChatWithExtras && (currentTab?.config?.browserMode ?? 'none') === 'cdp'
      ? (currentTab?.config?.cdpPort || 9222)
      : undefined,
    delegation_mode: isMultiAgentMode
      ? 'plan' as const
      : (isChatMode && useAppStore.getState().delegationMode !== 'off'
        ? useAppStore.getState().delegationMode as 'spawn'
        : undefined),
    plan_phase: isMultiAgentMode
      ? (effectivePlanPhase ?? currentTab?.config?.planPhaseOverride ?? 'planning')
      : undefined,
    delegation_tier_config: isMultiAgentMode
      ? (currentTab?.config?.delegationTierConfig ?? useLLMStore.getState().delegationTierConfig ?? undefined)
      : undefined,
    selected_skills: isChatWithExtras && currentTab?.config?.selectedSkills?.length
      ? currentTab.config.selectedSkills
      : undefined,
    selected_subagents: isChatWithExtras && currentTab?.config?.selectedSubAgents?.length
      ? currentTab.config.selectedSubAgents
      : undefined,
    enable_context_editing: enableContextEditing,
    decrypted_secrets: isChatWithExtras && decryptedSecrets?.length ? decryptedSecrets : undefined,
    selected_global_secrets: isChatWithExtras ? selectedGlobalSecrets : undefined,
    workflow_context_paths: isChatWithExtras && currentTab?.config?.workflowContext?.length
      ? currentTab.config.workflowContext.map(w => w.workspacePath)
      : undefined,
    plan_folder: isMultiAgentMode
      ? (currentTab?.config?.fileContext?.find(f => /^Plans\/[^/]+$/.test(f.path))?.path ?? undefined)
      : undefined,
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
// 1d. resolveOrCreateTab — tab resolution + session ID guarantee for chat/multi-agent
// ---------------------------------------------------------------------------

export async function resolveOrCreateTab(params: {
  freshActiveTab: ChatTab | undefined
  selectedModeCategory: ModeCategory
}): Promise<{ tab: ChatTab; sessionId: string } | null> {
  const { freshActiveTab, selectedModeCategory } = params
  let currentTab = freshActiveTab

  if (!currentTab && (selectedModeCategory === 'chat' || selectedModeCategory === 'multi-agent')) {
    const chatStore = useChatStore.getState()
    const tabs = Object.values(chatStore.chatTabs).filter(tab =>
      tab.metadata?.mode === selectedModeCategory
    )

    if (tabs.length === 0) {
      try {
        const tabName = selectedModeCategory === 'multi-agent' ? 'Agent Chat 1' : 'Chat 1'
        const newTabId = await chatStore.createChatTab(tabName, { mode: selectedModeCategory })
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

  // Single pass: get all tabs for this phase
  const existingPhaseTabs = getTabsByPhaseId(phaseId)

  // Prefer streaming tab, then newest, then any matching preset tab
  const runningTab = existingPhaseTabs.find(t => getTabStreamingStatus(t.tabId))
  const newestTab = existingPhaseTabs.length > 0
    ? existingPhaseTabs.sort((a, b) => b.createdAt - a.createdAt)[0]
    : null

  // Also check for matching preset if no direct phase match
  const matchingPresetTab = !runningTab && !newestTab
    ? Object.values(chatStore.chatTabs).find(t =>
        t.metadata?.mode === 'workflow' &&
        t.metadata?.phaseId === phaseId &&
        (t.metadata?.presetQueryId === activePresetId || (!t.metadata?.presetQueryId && activePresetId))
      )
    : null

  const existingTab = runningTab || newestTab || matchingPresetTab

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

export function createUserMessageEvent(content: string): PollingEvent {
  return {
    id: `user-message-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`,
    type: 'user_message',
    timestamp: new Date().toISOString(),
    data: {
      type: 'user_message',
      timestamp: new Date().toISOString(),
      data: {
        content,
        timestamp: new Date().toISOString()
      }
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    } as any
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
// validateExecutionGroups — check enabled_group_ids for workflow mode
// ---------------------------------------------------------------------------

export function validateExecutionGroups(
  executionOptions: ExecutionOptions | undefined
): string | null {
  if (!executionOptions) return null

  const workflowStore = useWorkflowStore.getState()
  const variablesManifest = workflowStore.variablesManifest

  if (!variablesManifest?.groups || variablesManifest.groups.length === 0) return null

  const enabledGroupIds = executionOptions.enabled_group_ids
  if (!enabledGroupIds || enabledGroupIds.length === 0) {
    return 'Please select at least one group to execute. Groups are available but no groups are selected.'
  }

  return null
}
