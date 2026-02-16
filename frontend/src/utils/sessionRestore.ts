import type { ChatTabConfig } from '../stores/useChatStore'
import type { ChatSessionConfig, ExtendedLLMConfiguration } from '../services/api-types'
import { useLLMStore } from '../stores/useLLMStore'
import { useChatStore } from '../stores/useChatStore'
import { agentApi } from '../services/api'

/**
 * Build a partial ChatTabConfig from a stored session config.
 * Centralizes the config restoration logic used across auto-restore, sidebar restore,
 * and resume dialog flows.
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
 */
export async function hydrateTabEvents(
  sessionId: string,
  eventMode: 'micro' | 'tiny' | 'basic' | 'advanced'
): Promise<void> {
  const response = await agentApi.getSessionEvents(sessionId, -1, { eventMode })
  const chatStore = useChatStore.getState()
  chatStore.setTabEvents(sessionId, response.events)
  const lastIndex = response.last_processed_index ?? (response.events.length > 0 ? response.events.length - 1 : -1)
  chatStore.setTabLastEventIndex(sessionId, lastIndex)
  if (response.has_more !== undefined) {
    chatStore.setTabHasMoreOlderEvents(sessionId, response.has_more)
  }
}
