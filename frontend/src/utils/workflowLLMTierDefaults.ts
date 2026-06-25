import type { AgentLLMConfig, LLMProvider } from '../services/api-types'
import type { ProviderManifestEntry, ProviderDefaultTierModels } from '../services/llm-config-api'
import type { LLMOption } from '../types/llm'

type WorkflowTierDefaults = {
  base: AgentLLMConfig
  tier1: AgentLLMConfig
  tier2: AgentLLMConfig
  tier3: AgentLLMConfig
  phase: AgentLLMConfig
  usesTierDefaults: boolean
}

const hasOptions = (options?: Record<string, unknown>) =>
  Boolean(options && Object.keys(options).length > 0)

function toAgentLLMConfig(option: LLMOption, modelID: string, provider = option.provider): AgentLLMConfig {
  const samePublishedModel = provider === option.provider && modelID === option.model
  return {
    ...(samePublishedModel && option.id ? { published_llm_id: option.id } : {}),
    provider: provider as LLMProvider,
    model_id: modelID,
    ...(samePublishedModel && hasOptions(option.options) ? { options: option.options } : {}),
  }
}

function toAgentLLMConfigFromRef(
  option: LLMOption,
  ref: ProviderDefaultTierModels[keyof ProviderDefaultTierModels],
): AgentLLMConfig {
  const config = toAgentLLMConfig(option, ref.model_id, ref.provider)
  if (hasOptions(ref.options)) {
    return { ...config, options: ref.options }
  }
  return config
}

function sameModelDefaults(option: LLMOption): WorkflowTierDefaults {
  const base = toAgentLLMConfig(option, option.model)
  return {
    base,
    tier1: base,
    tier2: base,
    tier3: base,
    phase: base,
    usesTierDefaults: false,
  }
}

function findManifestDefaults(provider: string, providerManifest: ProviderManifestEntry[] = []) {
  return providerManifest.find(entry => entry.id === provider)?.default_tier_models
}

export function hasWorkflowLLMTierDefaults(provider: string, providerManifest: ProviderManifestEntry[] = []) {
  return Boolean(findManifestDefaults(provider, providerManifest))
}

function getTierDefaults(option: LLMOption, providerManifest: ProviderManifestEntry[] = []) {
  return findManifestDefaults(option.provider, providerManifest)
}

export function getWorkflowLLMTierDefaults(
  option: LLMOption,
  providerManifest: ProviderManifestEntry[] = [],
): WorkflowTierDefaults {
  if (option.section === 'published_model') return sameModelDefaults(option)

  const defaults = getTierDefaults(option, providerManifest)
  if (!defaults) return sameModelDefaults(option)

  return {
    base: toAgentLLMConfigFromRef(option, defaults.main),
    tier1: toAgentLLMConfigFromRef(option, defaults.high),
    tier2: toAgentLLMConfigFromRef(option, defaults.medium),
    tier3: toAgentLLMConfigFromRef(option, defaults.low),
    phase: toAgentLLMConfigFromRef(option, defaults.phase),
    usesTierDefaults: true,
  }
}

export function agentLLMToPresetBase(config: AgentLLMConfig) {
  return {
    ...(config.published_llm_id ? { published_llm_id: config.published_llm_id } : {}),
    provider: config.provider,
    model_id: config.model_id,
    ...(hasOptions(config.options) ? { options: config.options } : {}),
  }
}

export function getWorkflowLLMOptions(
  availableLLMs: LLMOption[],
  providerManifest: ProviderManifestEntry[] = [],
): LLMOption[] {
  const options: LLMOption[] = []
  const seenCodingAgents = new Set<string>()

  providerManifest.forEach(entry => {
    if (entry.integration_kind !== 'coding_agent') return
    if (!entry.default_tier_models) return

    const main = entry.default_tier_models.main
    const key = `${main.provider}/${main.model_id}`
    if (seenCodingAgents.has(key)) return
    seenCodingAgents.add(key)
    options.push({
      provider: main.provider,
      model: main.model_id,
      label: entry.display_name,
      description: entry.usable
        ? entry.description
        : entry.setup_hint || entry.description,
      section: 'coding_agent',
      ...(hasOptions(main.options) ? { options: main.options } : {}),
    })
  })

  availableLLMs.forEach(option => {
    options.push({
      ...option,
      section: 'published_model',
    })
  })

  console.log('[WorkflowLLMOptions] Built workflow dropdown options', {
    providerManifestCount: providerManifest.length,
    publishedModelCount: availableLLMs.length,
    codingAgents: options
      .filter(option => option.section === 'coding_agent')
      .map(option => ({ provider: option.provider, model: option.model, label: option.label })),
    publishedModels: options
      .filter(option => option.section === 'published_model')
      .map(option => ({ provider: option.provider, model: option.model, label: option.label })),
    manifestCodingAgentProviders: providerManifest
      .filter(entry => entry.integration_kind === 'coding_agent')
      .map(entry => ({
        id: entry.id,
        displayName: entry.display_name,
        usable: entry.usable,
        hasDefaultTierModels: Boolean(entry.default_tier_models),
        main: entry.default_tier_models?.main,
      })),
  })

  return options
}
