import type { SavedLLM } from '../services/api-types'
import type { ModelMetadata } from '../services/llm-config-api'
import type { LLMOption } from '../types/llm'

export type ProviderType =
  | 'openrouter'
  | 'bedrock'
  | 'openai'
  | 'vertex'
  | 'anthropic'
  | 'azure'
  | 'z-ai'
  | 'kimi'
  | 'claude-code'
  | 'gemini-cli'
  | 'codex-cli'
  | 'cursor-cli'
  | 'agy-cli'
  | 'opencode-cli'
  | 'minimax'
  | 'minimax-coding-plan'
  | 'elevenlabs'
  | 'deepgram'

export type LLMIntegrationKind = 'coding_agent' | 'api_model' | 'audio_provider'

type ProviderDisplayInfo = {
  name: string
  authDescription: string
  colorClass: string
}

export type LLMIntegrationDisplayInfo = {
  label: string
  description: string
  toneClass: string
}

export const LLM_INTEGRATION_ORDER: LLMIntegrationKind[] = [
  'coding_agent',
  'api_model',
  'audio_provider',
]

export const LLM_INTEGRATION_DISPLAY_INFO: Record<LLMIntegrationKind, LLMIntegrationDisplayInfo> = {
  coding_agent: {
    label: 'Coding Agents',
    description: 'Local agent runtimes',
    toneClass: 'text-amber-700 dark:text-amber-300',
  },
  api_model: {
    label: 'API Providers',
    description: 'Provider-hosted chat models',
    toneClass: 'text-blue-700 dark:text-blue-300',
  },
  audio_provider: {
    label: 'Audio Providers',
    description: 'Speech, voice, and media models',
    toneClass: 'text-violet-700 dark:text-violet-300',
  },
}

export const CODING_AGENT_PROVIDERS = new Set(['claude-code', 'gemini-cli', 'codex-cli', 'cursor-cli', 'agy-cli', 'opencode-cli'])
const AUDIO_PROVIDER_PROVIDERS = new Set(['elevenlabs', 'deepgram'])

const PROVIDER_DISPLAY_INFO: Record<ProviderType, ProviderDisplayInfo> = {
  openrouter: {
    name: 'OpenRouter',
    authDescription: 'API Key',
    colorClass: 'text-blue-600 dark:text-blue-400',
  },
  bedrock: {
    name: 'AWS Bedrock',
    authDescription: 'AWS IAM',
    colorClass: 'text-orange-600 dark:text-orange-400',
  },
  openai: {
    name: 'OpenAI',
    authDescription: 'API Key',
    colorClass: 'text-green-600 dark:text-green-400',
  },
  vertex: {
    name: 'Google Vertex',
    authDescription: 'API Key',
    colorClass: 'text-purple-600 dark:text-purple-400',
  },
  anthropic: {
    name: 'Anthropic',
    authDescription: 'API Key',
    colorClass: 'text-red-600 dark:text-red-400',
  },
  azure: {
    name: 'Azure OpenAI',
    authDescription: 'Endpoint + API Key',
    colorClass: 'text-sky-600 dark:text-sky-400',
  },
  'z-ai': {
    name: 'Z.AI',
    authDescription: 'API Key',
    colorClass: 'text-fuchsia-600 dark:text-fuchsia-400',
  },
  kimi: {
    name: 'Kimi',
    authDescription: 'API Key',
    colorClass: 'text-rose-600 dark:text-rose-400',
  },
  'claude-code': {
    name: 'Claude Code',
    authDescription: 'Local CLI (no API key)',
    colorClass: 'text-amber-600 dark:text-amber-400',
  },
  'gemini-cli': {
    name: 'Gemini CLI',
    authDescription: 'Local CLI (no API key)',
    colorClass: 'text-indigo-600 dark:text-indigo-400',
  },
  'codex-cli': {
    name: 'Codex CLI',
    authDescription: 'Local CLI (API key optional)',
    colorClass: 'text-emerald-600 dark:text-emerald-400',
  },
  'cursor-cli': {
    name: 'Cursor CLI',
    authDescription: 'Local CLI (API key optional)',
    colorClass: 'text-slate-600 dark:text-slate-300',
  },
  'agy-cli': {
    name: 'Antigravity CLI',
    authDescription: 'Local CLI (Agy sign-in)',
    colorClass: 'text-zinc-600 dark:text-zinc-300',
  },
  'opencode-cli': {
    name: 'OpenCode CLI',
    authDescription: 'Local CLI (API key optional)',
    colorClass: 'text-cyan-600 dark:text-cyan-400',
  },
  minimax: {
    name: 'MiniMax',
    authDescription: 'API Key',
    colorClass: 'text-cyan-600 dark:text-cyan-400',
  },
  elevenlabs: {
    name: 'ElevenLabs',
    authDescription: 'API Key',
    colorClass: 'text-violet-600 dark:text-violet-400',
  },
  deepgram: {
    name: 'Deepgram',
    authDescription: 'API Key',
    colorClass: 'text-emerald-600 dark:text-emerald-400',
  },
  'minimax-coding-plan': {
    name: 'MiniMax Coding Plan',
    authDescription: 'Coding Plan Key (sk-cp-)',
    colorClass: 'text-teal-600 dark:text-teal-400',
  },
}

export const PROVIDER_ORDER: ProviderType[] = [
  'codex-cli',
  'cursor-cli',
  'opencode-cli',
  'claude-code',
  'gemini-cli',
  'bedrock',
  'openai',
  'vertex',
  'anthropic',
  'azure',
  'minimax',
  'elevenlabs',
  'deepgram',
]

export function getProviderDisplayInfo(provider?: string): ProviderDisplayInfo {
  if (!provider) {
    return {
      name: 'No LLM selected',
      authDescription: '',
      colorClass: 'text-gray-600 dark:text-gray-400',
    }
  }

  if (provider in PROVIDER_DISPLAY_INFO) {
    return PROVIDER_DISPLAY_INFO[provider as ProviderType]
  }

  return {
    name: provider,
    authDescription: 'API Key',
    colorClass: 'text-gray-600 dark:text-gray-400',
  }
}

export function getProviderIntegrationKind(provider?: string, modelId?: string): LLMIntegrationKind {
  const normalizedProvider = (provider || '').trim().toLowerCase()
  const normalizedModel = (modelId || '').trim().toLowerCase()

  if (CODING_AGENT_PROVIDERS.has(normalizedProvider) || normalizedProvider.startsWith('opencode-cli-')) {
    return 'coding_agent'
  }
  if (AUDIO_PROVIDER_PROVIDERS.has(normalizedProvider)) {
    return 'audio_provider'
  }
  if (normalizedProvider === 'minimax' && /^(speech|music|audio|voice)[-_]/.test(normalizedModel)) {
    return 'audio_provider'
  }
  return 'api_model'
}

export function getProviderIntegrationInfo(provider?: string, modelId?: string): LLMIntegrationDisplayInfo {
  return LLM_INTEGRATION_DISPLAY_INFO[getProviderIntegrationKind(provider, modelId)]
}

export function shouldShowLLMPricing(provider?: string, modelId?: string): boolean {
  const normalizedProvider = (provider || '').trim().toLowerCase()
  if (normalizedProvider === 'minimax-coding-plan') {
    return false
  }
  return getProviderIntegrationKind(provider, modelId) !== 'coding_agent'
}

type ModelDisplayNameOptions = {
  provider?: string
  modelId?: string
  metadata?: ModelMetadata[]
  savedLLMs?: SavedLLM[]
  availableLLMs?: LLMOption[]
}

export function getModelDisplayName({
  provider,
  modelId,
  metadata = [],
  savedLLMs = [],
  availableLLMs = [],
}: ModelDisplayNameOptions): string {
  if (!modelId) return 'Unknown'

  if (provider === 'minimax' || provider === 'minimax-coding-plan') {
    return 'MiniMax'
  }

  const publishedLLM = savedLLMs.find(
    (llm) => llm.provider === provider && llm.model_id === modelId
  )
  if (publishedLLM?.name) return publishedLLM.name
  if (publishedLLM?.model_name) return publishedLLM.model_name

  const metadataMatch =
    metadata.find((item) => item.provider === provider && item.model_id === modelId) ||
    metadata.find((item) => item.model_id === modelId)
  if (metadataMatch?.model_name) return metadataMatch.model_name

  const availableLLM = availableLLMs.find(
    (llm) => llm.provider === provider && llm.model === modelId
  )
  if (availableLLM?.label) return availableLLM.label

  return modelId
}
