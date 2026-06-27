import { useEffect, useMemo, useState } from 'react'
import { AlertCircle, CheckCircle, Copy, Loader2, RefreshCw, Settings, Terminal, X, KeyRound, ChevronDown, ChevronRight } from 'lucide-react'
import { Button } from './ui/Button'
import { llmConfigService, type DynamicModelEntry, type ModelMetadata, type ProviderManifestEntry } from '../services/llm-config-api'
import type { LLMDiscoveryCandidate, LLMModel, LLMProvider, SavedLLM, TierModel } from '../services/api-types'
import { useLLMStore } from '../stores'
import ModalPortal from './ui/ModalPortal'

interface LLMDiscoveryOnboardingModalProps {
  isOpen: boolean
  onClose: () => void
  onAdvancedSetup?: () => void
}

type CandidateTestState = {
  status: 'idle' | 'testing' | 'valid' | 'invalid'
  message?: string
}

type ReadinessLevel = 'ready' | 'needs_setup' | 'not_detected'
type TierKey = 'main' | 'high' | 'medium' | 'low'
type VisibleTierKey = Exclude<TierKey, 'main'>

const PRIMARY_CODING_CLI_ORDER = ['claude-code', 'codex-cli', 'cursor-cli'] as const
const SECONDARY_CODING_CLI_ORDER = ['pi-cli'] as const
const FALLBACK_CODING_CLI_ORDER = [...PRIMARY_CODING_CLI_ORDER, ...SECONDARY_CODING_CLI_ORDER] as const
const CODING_CLI_DISPLAY_ORDER = new Map<string, number>(
  FALLBACK_CODING_CLI_ORDER.map((provider, index) => [provider, index])
)
const VISIBLE_TIER_LABELS: Record<VisibleTierKey, string> = {
  high: 'High',
  medium: 'Med',
  low: 'Low',
}

const CODING_CLI_INFO: Record<typeof FALLBACK_CODING_CLI_ORDER[number], {
  label: string
  binary: string
  setupCommand: string
  installHint: string
}> = {
  'codex-cli': {
    label: 'OpenAI Codex CLI',
    binary: 'codex',
    setupCommand: 'codex login',
    installHint: 'Install Codex CLI so the codex command is available on the backend PATH.',
  },
  'claude-code': {
    label: 'Claude Code',
    binary: 'claude',
    setupCommand: 'claude',
    installHint: 'Install Claude Code so the claude command is available on the backend PATH.',
  },
  'cursor-cli': {
    label: 'Cursor CLI',
    binary: 'cursor-agent',
    setupCommand: 'cursor-agent login',
    installHint: 'Install Cursor CLI so the cursor-agent command is available on the backend PATH.',
  },
  'pi-cli': {
    label: 'Pi.dev',
    binary: 'pi',
    setupCommand: 'npm install -g @earendil-works/pi-coding-agent',
    installHint: 'Install Pi CLI so the pi command is available on the backend PATH, or ensure npx is available.',
  },
}

const PI_PROVIDER_FALLBACK_LABELS = ['Gemini', 'OpenRouter', 'Z.AI / GLM', 'MiniMax', 'Kimi', 'DeepSeek']
const PI_MODEL_FALLBACK_PREVIEW: DynamicModelEntry[] = [
  { model_id: 'google/gemini-3.5-flash', model_name: 'Gemini 3.5 Flash', group: 'Gemini' },
  { model_id: 'openrouter/minimax/minimax-m3-20260531', model_name: 'MiniMax M3', group: 'OpenRouter' },
  { model_id: 'zai/glm-5.2', model_name: 'GLM-5.2', group: 'Z.AI / GLM' },
  { model_id: 'minimax/MiniMax-M3', model_name: 'MiniMax M3', group: 'MiniMax' },
  { model_id: 'kimi-coding/k2p7', model_name: 'Kimi K2.7 Code', group: 'Kimi' },
  { model_id: 'deepseek/deepseek-v4-pro', model_name: 'DeepSeek V4 Pro', group: 'DeepSeek' },
]

const isFallbackCodingCLIProvider = (provider: string): provider is typeof FALLBACK_CODING_CLI_ORDER[number] =>
  (FALLBACK_CODING_CLI_ORDER as readonly string[]).includes(provider)

const isPrimaryCodingCLIProvider = (provider: string) =>
  (PRIMARY_CODING_CLI_ORDER as readonly string[]).includes(provider)

const codingCliDisplayRank = (provider: string) =>
  CODING_CLI_DISPLAY_ORDER.get(provider) ?? 999

function sortCodingCliCandidates(candidates: LLMDiscoveryCandidate[]) {
  return [...candidates].sort((a, b) =>
    codingCliDisplayRank(a.provider) - codingCliDisplayRank(b.provider) ||
    a.label.localeCompare(b.label)
  )
}

function fallbackCodingCandidate(provider: typeof FALLBACK_CODING_CLI_ORDER[number]): LLMDiscoveryCandidate {
  const info = CODING_CLI_INFO[provider]
  return {
    id: `${provider}:${provider}`,
    provider: provider as LLMProvider,
    model_id: provider,
    model_name: 'Default',
    label: info.label,
    kind: 'local_cli',
    detection_source: 'Supported coding CLI',
    auth_configured: false,
    runtime_command: info.binary,
    runtime_available: false,
    usable: false,
    recommended: false,
    reason: 'CLI runtime was not detected.',
    setup_hint: info.installHint,
  }
}

function manifestProviderCandidate(provider: ProviderManifestEntry): LLMDiscoveryCandidate | null {
  if (provider.deprecated) return null
  const defaultModel = provider.models.find(model => model.model_id === provider.default_model_id) || provider.models[0]
  const modelID = provider.default_model_id || defaultModel?.model_id || provider.id
  if (!modelID) return null

  return {
    id: `${provider.id}:${modelID}`,
    provider: provider.id as LLMProvider,
    model_id: modelID,
    model_name: defaultModel?.model_name || provider.display_name,
    label: provider.display_name,
    kind: provider.kind,
    detection_source: provider.description,
    auth_source: provider.auth_source,
    auth_configured: provider.auth_configured,
    runtime_command: provider.runtime_command,
    runtime_available: provider.runtime_available,
    usable: provider.usable,
    recommended: false,
    reason: provider.usable ? 'Ready to enable.' : provider.setup_hint || provider.auth_description,
    setup_hint: provider.setup_hint || provider.description,
    options: provider.models.map(model => model.model_id),
  }
}

function candidateReadiness(candidate: LLMDiscoveryCandidate): ReadinessLevel {
  if (candidate.kind === 'local_cli' && candidate.runtime_available === false) return 'not_detected'
  if (!candidate.usable) return 'needs_setup'
  return 'ready'
}

function modelForCandidate(
  candidate: LLMDiscoveryCandidate,
  selectedModelId: string | undefined,
  providerManifest: ProviderManifestEntry[],
): LLMModel {
  const manifestDefaults = providerManifest.find(provider => provider.id === candidate.provider && !provider.deprecated)?.default_tier_models
  const main = manifestDefaults?.main

  return {
    provider: (main?.provider || candidate.provider) as LLMProvider,
    model_id: selectedModelId || main?.model_id || candidate.model_id,
    ...(main?.options && Object.keys(main.options).length > 0 ? { options: main.options } : {}),
  }
}

function tierModelsForCandidate(
  candidate: LLMDiscoveryCandidate,
  providerManifest: ProviderManifestEntry[] = [],
): Record<TierKey, { provider: string; modelID: string }> {
  const manifestDefaults = providerManifest.find(provider => provider.id === candidate.provider && !provider.deprecated)?.default_tier_models
  if (manifestDefaults) {
    return {
      main: { provider: manifestDefaults.main.provider, modelID: manifestDefaults.main.model_id },
      high: { provider: manifestDefaults.high.provider, modelID: manifestDefaults.high.model_id },
      medium: { provider: manifestDefaults.medium.provider, modelID: manifestDefaults.medium.model_id },
      low: { provider: manifestDefaults.low.provider, modelID: manifestDefaults.low.model_id },
    }
  }

  const fallback = candidate.model_id
  return {
    main: { provider: candidate.provider, modelID: fallback },
    high: { provider: candidate.provider, modelID: fallback },
    medium: { provider: candidate.provider, modelID: fallback },
    low: { provider: candidate.provider, modelID: fallback },
  }
}

function tierConfigForCandidate(candidate: LLMDiscoveryCandidate, providerManifest: ProviderManifestEntry[]): Record<TierKey, TierModel> {
  const tierModels = tierModelsForCandidate(candidate, providerManifest)
  return {
    main: { provider: tierModels.main.provider, model_id: tierModels.main.modelID },
    high: { provider: tierModels.high.provider, model_id: tierModels.high.modelID },
    medium: { provider: tierModels.medium.provider, model_id: tierModels.medium.modelID },
    low: { provider: tierModels.low.provider, model_id: tierModels.low.modelID },
  }
}

function modelNameFor(provider: string, modelID: string, metadata: ModelMetadata[], providerManifest: ProviderManifestEntry[]) {
  const meta = metadata.find(m => m.provider === provider && m.model_id === modelID)
  if (meta?.model_name) return meta.model_name
  const providerInfo = providerManifest.find(p => p.id === provider)
  const manifestModel = providerInfo?.models.find(model => model.model_id === modelID)
  return manifestModel?.model_name || modelID
}

function compactModelLabel(modelID: string, modelName: string) {
  const raw = (modelName || modelID)
    .replace(/\s*\(default,\s*pricing varies\)/i, '')
    .replace(/\s*\(recommended,\s*pricing varies\)/i, '')
    .trim()
  const lower = `${modelID} ${raw}`.toLowerCase()
  if (lower.includes('opus')) return 'Opus'
  if (lower.includes('sonnet')) return 'Sonnet'
  if (lower.includes('haiku')) return 'Haiku'
  if (['high', 'medium', 'low', 'auto'].includes(modelID)) {
    return modelID.charAt(0).toUpperCase() + modelID.slice(1)
  }
  const tail = raw.split('/').pop() || raw
  return tail.length > 18 ? `${tail.slice(0, 18)}...` : tail
}

function visibleTierEntries(candidate: LLMDiscoveryCandidate, metadata: ModelMetadata[], providerManifest: ProviderManifestEntry[]) {
  const tierModels = tierModelsForCandidate(candidate, providerManifest)
  return (['high', 'medium', 'low'] as VisibleTierKey[]).map(key => {
    const { provider, modelID } = tierModels[key]
    const modelName = modelNameFor(provider, modelID, metadata, providerManifest)
    return {
      key,
      label: VISIBLE_TIER_LABELS[key],
      modelID,
      display: compactModelLabel(modelID, modelName),
    }
  })
}

function piProviderLabelForModel(modelID: string) {
  const prefix = modelID.split('/')[0]?.trim().toLowerCase()
  switch (prefix) {
    case 'google':
    case 'google-vertex':
      return 'Gemini'
    case 'openrouter':
      return 'OpenRouter'
    case 'zai':
    case 'zai-coding-cn':
      return 'Z.AI / GLM'
    case 'minimax':
    case 'minimax-cn':
      return 'MiniMax'
    case 'kimi-coding':
    case 'moonshotai':
    case 'moonshotai-cn':
      return 'Kimi'
    case 'deepseek':
      return 'DeepSeek'
    default:
      return prefix ? prefix : 'Custom'
  }
}

function uniquePiProviderLabels(models: DynamicModelEntry[]) {
  if (models.length === 0) return PI_PROVIDER_FALLBACK_LABELS
  const labels = new Set<string>()
  for (const model of models) {
    labels.add(piProviderLabelForModel(model.model_id))
  }
  return Array.from(labels)
}

function publishedName(candidate: LLMDiscoveryCandidate, selectedModelId?: string): string {
  if (candidate.provider === 'codex-cli') return 'Codex CLI'
  if (candidate.provider === 'cursor-cli') return 'Cursor CLI'
  if (candidate.provider === 'pi-cli') return `Pi.dev (${selectedModelId || candidate.model_id})`
  if (candidate.provider === 'claude-code') return 'Claude Code'
  return candidate.label
}

export default function LLMDiscoveryOnboardingModal({ isOpen, onClose, onAdvancedSetup }: LLMDiscoveryOnboardingModalProps) {
  const {
    savedLLMs,
    saveLLM,
    setAgentConfig,
    setChatPrimaryConfig,
    setDelegationTierConfig,
    refreshAvailableLLMs,
    loadDefaultsFromBackend,
    defaultsLoaded,
    providerManifest,
    providerManifestLoaded,
    loadProviderManifest,
    loadDelegationTierDefaults,
  } = useLLMStore()

  const [candidates, setCandidates] = useState<LLMDiscoveryCandidate[]>([])
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState<string | null>(null)
  const [testStates, setTestStates] = useState<Record<string, CandidateTestState>>({})
  const [enablingId, setEnablingId] = useState<string | null>(null)
  const [copiedCommand, setCopiedCommand] = useState<string | null>(null)
  const [metadata, setMetadata] = useState<ModelMetadata[]>([])
  const [piDynamicModels, setPiDynamicModels] = useState<DynamicModelEntry[]>([])
  const [showAPIProviders, setShowAPIProviders] = useState(false)

  const codingCliCandidates = useMemo(() => {
    const byProvider = new Map<string, LLMDiscoveryCandidate>()
    const byRuntime = new Map<string, LLMDiscoveryCandidate>()
    for (const candidate of candidates) {
      if (candidate.deprecated) continue
      if (candidate.kind === 'local_cli' && !byProvider.has(candidate.provider)) {
        byProvider.set(candidate.provider, candidate)
        const runtimeKey = candidate.runtime_command || candidate.provider
        if (!byRuntime.has(runtimeKey)) byRuntime.set(runtimeKey, candidate)
      }
    }

    const seenRuntimeKeys = new Set<string>()
    const manifestCodingProviders = providerManifest.filter(provider => {
      if (provider.deprecated) return false
      if (provider.integration_kind !== 'coding_agent') return false
      const runtimeKey = provider.runtime_command || provider.id
      if (seenRuntimeKeys.has(runtimeKey)) return false
      seenRuntimeKeys.add(runtimeKey)
      return true
    })
    if (manifestCodingProviders.length > 0) {
      const seen = new Set<string>()
      const ordered: LLMDiscoveryCandidate[] = []
      for (const provider of manifestCodingProviders) {
        const runtimeKey = provider.runtime_command || provider.id
        const candidate = byProvider.get(provider.id) || byRuntime.get(runtimeKey) || manifestProviderCandidate(provider)
        if (candidate) {
          ordered.push(candidate)
          seen.add(provider.id)
        }
      }
      for (const candidate of byProvider.values()) {
        const runtimeKey = candidate.runtime_command || candidate.provider
        if (!seen.has(candidate.provider) && !seenRuntimeKeys.has(runtimeKey)) ordered.push(candidate)
      }
      return sortCodingCliCandidates(ordered)
    }

    return FALLBACK_CODING_CLI_ORDER.map(provider => byProvider.get(provider) || fallbackCodingCandidate(provider))
  }, [candidates, providerManifest])

  const primaryCodingCliCandidates = useMemo(
    () => codingCliCandidates.filter(candidate => isPrimaryCodingCLIProvider(candidate.provider)),
    [codingCliCandidates]
  )
  const secondaryCodingCliCandidates = useMemo(
    () => codingCliCandidates.filter(candidate => !isPrimaryCodingCLIProvider(candidate.provider)),
    [codingCliCandidates]
  )
  const piCodingCliCandidate = useMemo(
    () => codingCliCandidates.find(candidate => candidate.provider === 'pi-cli'),
    [codingCliCandidates]
  )
  const otherCodingCliCandidates = useMemo(
    () => secondaryCodingCliCandidates.filter(candidate => candidate.provider !== 'pi-cli'),
    [secondaryCodingCliCandidates]
  )

  const grouped = useMemo(() => {
    const readyAPI: LLMDiscoveryCandidate[] = []
    const apiNeedsSetup: LLMDiscoveryCandidate[] = []
    for (const c of candidates) {
      if (c.deprecated) continue
      if (c.kind === 'local_cli') continue
      const level = candidateReadiness(c)
      if (level === 'ready') {
        readyAPI.push(c)
      } else if (level === 'needs_setup') {
        apiNeedsSetup.push(c)
      }
    }
    return { readyAPI, apiNeedsSetup }
  }, [candidates])

  const hasReadyCodingCLI = codingCliCandidates.some(candidate => candidateReadiness(candidate) === 'ready')

  const loadDiscovery = async () => {
    setIsLoading(true)
    setError(null)
    setSuccess(null)
    try {
      if (!defaultsLoaded) await loadDefaultsFromBackend()
      const [response, metaResponse, , , piModelsResponse] = await Promise.all([
        llmConfigService.discoverLLMSetup(),
        llmConfigService.getModelMetadata().catch(() => ({ models: [] })),
        providerManifestLoaded ? Promise.resolve() : loadProviderManifest(),
        loadDelegationTierDefaults(),
        llmConfigService.getProviderModels('pi-cli').catch(() => ({ models: [] as DynamicModelEntry[] })),
      ])
      setCandidates(response.candidates || [])
      setMetadata(metaResponse.models || [])
      setPiDynamicModels(piModelsResponse.models || [])
      setTestStates({})
      setCopiedCommand(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to discover local LLM setup.')
    } finally {
      setIsLoading(false)
    }
  }

  useEffect(() => {
    if (isOpen) loadDiscovery()
  }, [isOpen])

  if (!isOpen) return null

  const copyCommand = async (command: string) => {
    try {
      await navigator.clipboard.writeText(command)
      setCopiedCommand(command)
      setTimeout(() => setCopiedCommand(current => current === command ? null : current), 1600)
    } catch {
      setCopiedCommand(null)
    }
  }

  const renderCodingCliCard = (candidate: LLMDiscoveryCandidate) => {
    const readiness = candidateReadiness(candidate)
    const testState = testStates[candidate.id] || { status: 'idle' as const }
    const isEnabling = enablingId === candidate.id
    const providerInfo = isFallbackCodingCLIProvider(candidate.provider)
      ? CODING_CLI_INFO[candidate.provider]
      : undefined
    const setupCommand = providerInfo?.setupCommand || (readiness === 'needs_setup' ? candidate.runtime_command || candidate.provider : undefined)
    const tierChips = visibleTierEntries(candidate, metadata, providerManifest)
    const isPrimaryProvider = isPrimaryCodingCLIProvider(candidate.provider)
    const statusClasses = readiness === 'ready'
      ? 'bg-success/10 text-success'
      : readiness === 'needs_setup'
        ? 'bg-warning/20 text-warning'
        : 'bg-muted text-muted-foreground'
    const statusLabel = readiness === 'ready'
      ? 'Detected'
      : readiness === 'needs_setup'
        ? 'Login needed'
        : 'Install'

    return (
      <div
        key={candidate.id}
        className={`rounded-lg border p-4 shadow-sm ${
          isPrimaryProvider
            ? 'border-primary/40 bg-card ring-1 ring-primary/20'
            : 'border-border bg-card'
        }`}
      >
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <div className="flex items-start gap-2">
              <Terminal className="h-4 w-4 shrink-0 text-muted-foreground" />
              <h4 className="min-w-0 text-sm font-semibold leading-snug text-foreground">{candidate.label}</h4>
            </div>
          </div>
          <div className="flex shrink-0 flex-col items-end gap-1">
            <span className={`rounded-full px-2 py-0.5 text-[11px] font-medium ${statusClasses}`}>
              {statusLabel}
            </span>
          </div>
        </div>

        <div className="mt-3 space-y-2 text-xs text-muted-foreground">
          <div className="flex flex-wrap gap-1.5">
            {tierChips.map(tier => (
              <span
                key={tier.key}
                className="rounded bg-muted px-1.5 py-0.5 text-[11px] text-muted-foreground"
                title={`${tier.label}: ${tier.modelID}`}
              >
                {tier.label}: {tier.display}
              </span>
            ))}
          </div>
          {readiness === 'not_detected' ? (
            <p className="line-clamp-2">{candidate.setup_hint || candidate.reason}</p>
          ) : null}
        </div>

        <div className={`mt-4 flex items-center gap-2 ${readiness === 'ready' && testState.status !== 'invalid' ? 'justify-end' : 'justify-between'}`}>
          {readiness === 'ready' ? (
            <>
              {testState.status === 'invalid' ? (
                <span className="truncate text-xs text-red-500" title={testState.message}>{testState.message || 'Validation failed'}</span>
              ) : null}
              <Button
                size="sm"
                onClick={() => enableCandidate(candidate)}
                disabled={isEnabling || enablingId !== null}
                className="h-7 px-2.5 text-xs"
              >
                {isEnabling ? (
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                ) : testState.status === 'valid' ? (
                  <CheckCircle className="h-3.5 w-3.5" />
                ) : (
                  'Use'
                )}
              </Button>
            </>
          ) : readiness === 'needs_setup' && setupCommand ? (
            <>
              <code className="truncate rounded bg-muted px-2 py-1 text-xs text-foreground">{setupCommand}</code>
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="h-8 shrink-0"
                onClick={() => { void copyCommand(setupCommand) }}
              >
                <Copy className="mr-1.5 h-3.5 w-3.5" />
                {copiedCommand === setupCommand ? 'Copied' : 'Copy'}
              </Button>
            </>
          ) : (
            <span className="text-xs text-muted-foreground">Install, then use Rescan.</span>
          )}
        </div>
      </div>
    )
  }

  const renderPiDevCard = (candidate: LLMDiscoveryCandidate) => {
    const readiness = candidateReadiness(candidate)
    const testState = testStates[candidate.id] || { status: 'idle' as const }
    const isEnabling = enablingId === candidate.id
    const providerInfo = isFallbackCodingCLIProvider(candidate.provider)
      ? CODING_CLI_INFO[candidate.provider]
      : undefined
    const setupCommand = providerInfo?.setupCommand || (readiness === 'needs_setup' ? candidate.runtime_command || candidate.provider : undefined)
    const statusClasses = readiness === 'ready'
      ? 'bg-success/10 text-success'
      : readiness === 'needs_setup'
        ? 'bg-warning/20 text-warning'
        : 'bg-muted text-muted-foreground'
    const statusLabel = readiness === 'ready'
      ? 'Detected'
      : readiness === 'needs_setup'
        ? 'Key/login needed'
        : 'Install'
    const previewModels = (piDynamicModels.length > 0 ? piDynamicModels : PI_MODEL_FALLBACK_PREVIEW).slice(0, 10)
    const providerLabels = uniquePiProviderLabels(piDynamicModels).slice(0, 8)
    const selectedPreview = previewModels.find(model => model.model_id === candidate.model_id)
    const defaultModelLabel = selectedPreview?.model_name || candidate.model_name || candidate.model_id

    return (
      <div
        key={candidate.id}
        className="rounded-lg border border-lime-500/40 bg-lime-500/5 p-4 shadow-sm ring-1 ring-lime-500/10"
      >
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <div className="flex items-start gap-2">
              <Terminal className="h-4 w-4 shrink-0 text-lime-700 dark:text-lime-300" />
              <div className="min-w-0">
                <h4 className="text-sm font-semibold leading-snug text-foreground">Pi.dev multi-model coding</h4>
                <p className="mt-1 max-w-3xl text-xs leading-relaxed text-muted-foreground">
                  One tmux-backed Pi CLI integration for Gemini, OpenRouter, Chinese coding models, DeepSeek, and custom Pi provider IDs.
                </p>
              </div>
            </div>
          </div>
          <span className={`rounded-full px-2 py-0.5 text-[11px] font-medium ${statusClasses}`}>
            {statusLabel}
          </span>
        </div>

        <div className="mt-3 flex flex-wrap gap-1.5">
          {providerLabels.map(label => (
            <span
              key={label}
              className="rounded bg-background/80 px-2 py-1 text-[11px] font-medium text-foreground ring-1 ring-border"
            >
              {label}
            </span>
          ))}
        </div>

        <div className="mt-3 space-y-2">
          <div className="flex flex-wrap items-center gap-1.5 text-xs">
            <span className="font-medium text-muted-foreground">Curated models</span>
            {previewModels.map(model => (
              <span
                key={model.model_id}
                className="rounded bg-muted px-1.5 py-0.5 text-[11px] text-muted-foreground"
                title={model.model_id}
              >
                {model.model_name || model.model_id}
              </span>
            ))}
          </div>
          <p className="text-xs text-muted-foreground">
            Default: <code className="rounded bg-muted px-1 py-0.5 text-foreground">{defaultModelLabel}</code>.
            Provider keys are saved encrypted in Advanced setup and exported to Pi per selected model.
          </p>
          {readiness === 'not_detected' ? (
            <p className="text-xs text-muted-foreground">{candidate.setup_hint || candidate.reason}</p>
          ) : null}
        </div>

        <div className={`mt-4 flex flex-wrap items-center gap-2 ${readiness === 'ready' && testState.status !== 'invalid' ? 'justify-end' : 'justify-between'}`}>
          {readiness === 'ready' ? (
            <>
              {testState.status === 'invalid' ? (
                <span className="min-w-0 flex-1 truncate text-xs text-red-500" title={testState.message}>{testState.message || 'Validation failed'}</span>
              ) : null}
              {onAdvancedSetup ? (
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="h-7 px-2.5 text-xs"
                  onClick={onAdvancedSetup}
                >
                  <Settings className="mr-1.5 h-3.5 w-3.5" />
                  Models & keys
                </Button>
              ) : null}
              <Button
                size="sm"
                onClick={() => enableCandidate(candidate)}
                disabled={isEnabling || enablingId !== null}
                className="h-7 px-2.5 text-xs"
              >
                {isEnabling ? (
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                ) : testState.status === 'valid' ? (
                  <CheckCircle className="h-3.5 w-3.5" />
                ) : (
                  'Use default'
                )}
              </Button>
            </>
          ) : readiness === 'needs_setup' && setupCommand ? (
            <>
              <code className="min-w-0 flex-1 truncate rounded bg-muted px-2 py-1 text-xs text-foreground">{setupCommand}</code>
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="h-8 shrink-0"
                onClick={() => { void copyCommand(setupCommand) }}
              >
                <Copy className="mr-1.5 h-3.5 w-3.5" />
                {copiedCommand === setupCommand ? 'Copied' : 'Copy'}
              </Button>
            </>
          ) : (
            <span className="text-xs text-muted-foreground">Install Pi, then use Rescan.</span>
          )}
        </div>
      </div>
    )
  }

  const enableCandidate = async (candidate: LLMDiscoveryCandidate) => {
    setEnablingId(candidate.id)
    setError(null)
    setSuccess(null)
    setTestStates(prev => ({ ...prev, [candidate.id]: { status: 'testing' } }))

    try {
      const validation = await llmConfigService.validateAPIKey({
        provider: candidate.provider,
        model_id: candidate.model_id,
      })
      if (!validation.valid) {
        throw new Error(validation.message || validation.error || `${candidate.label} is not ready.`)
      }

      setTestStates(prev => ({ ...prev, [candidate.id]: { status: 'valid', message: validation.message } }))

      const modelId = candidate.model_id
      const llmModel = modelForCandidate(candidate, modelId, providerManifest)
      const alreadyPublished = savedLLMs.some(
        (saved: SavedLLM) => saved.provider === llmModel.provider && saved.model_id === llmModel.model_id
      )
      if (!alreadyPublished) {
        const modelMeta = metadata.find(m => m.model_id === modelId && m.provider === candidate.provider)
        await saveLLM(
          llmModel,
          publishedName(candidate, modelId),
          modelMeta?.model_name || candidate.model_name || modelId,
          candidate.kind === 'local_cli' ? 'none' : 'api_key',
          modelMeta || undefined
        )
      } else {
        await refreshAvailableLLMs()
      }

      setChatPrimaryConfig({
        provider: llmModel.provider,
        model_id: llmModel.model_id,
        fallback_models: [],
        cross_provider_fallback: undefined,
      })
      setAgentConfig({ primary: llmModel, fallbacks: [] })
      setDelegationTierConfig(tierConfigForCandidate(candidate, providerManifest))

      setSuccess(`${publishedName(candidate, modelId)} is enabled. You can change models later in Advanced setup.`)
    } catch (err) {
      setTestStates(prev => ({
        ...prev,
        [candidate.id]: { status: 'invalid', message: err instanceof Error ? err.message : 'Failed.' },
      }))
      setError(err instanceof Error ? err.message : 'Failed to enable this model.')
    } finally {
      setEnablingId(null)
    }
  }


  return (
    <ModalPortal>
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
        <div
          className="w-full max-w-6xl max-h-[calc(100vh-1rem)] overflow-hidden rounded-xl bg-white shadow-2xl dark:bg-slate-800 flex flex-col"
          onClick={(e) => e.stopPropagation()}
        >
          {/* Header */}
          <div className="flex items-start justify-between gap-5 border-b border-border px-7 py-5 shrink-0">
            <div className="min-w-0">
              <h2 className="text-xl font-semibold text-foreground">Connect a Coding CLI</h2>
              <p className="mt-1 max-w-2xl text-sm text-muted-foreground">
                Choose an installed coding CLI, or install one from the supported list. You can change models later.
              </p>
            </div>
            <div className="flex shrink-0 items-center gap-2">
              <button
                onClick={loadDiscovery}
                disabled={isLoading}
                className="p-2 rounded-md text-gray-400 hover:text-gray-600 hover:bg-gray-100 dark:hover:text-gray-300 dark:hover:bg-slate-700 transition-colors disabled:opacity-50"
                title="Re-scan"
              >
                <RefreshCw className={`w-4 h-4 ${isLoading ? 'animate-spin' : ''}`} />
              </button>
              <button
                onClick={onClose}
                className="p-2 rounded-md text-gray-400 hover:text-gray-600 hover:bg-gray-100 dark:hover:text-gray-300 dark:hover:bg-slate-700 transition-colors"
              >
                <X className="w-4 h-4" />
              </button>
            </div>
          </div>

          {/* Content */}
          <div className="px-7 py-6 overflow-y-auto min-h-0 flex-1">
            {isLoading && (
              <div className="flex flex-col items-center justify-center py-16 text-gray-500 dark:text-gray-400">
                <Loader2 className="w-6 h-6 animate-spin mb-3" />
                <p className="text-sm">Scanning for installed CLIs and provider credentials...</p>
              </div>
            )}

            {!isLoading && (
              <div className="space-y-5">
                <section>
                  <div className="mb-3 flex items-start justify-between gap-3">
                    <div>
                      <h3 className="flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                        <Terminal className="h-3.5 w-3.5" />
                        Dedicated CLIs
                      </h3>
                      <p className="mt-1 text-sm text-muted-foreground">
                        {hasReadyCodingCLI
                          ? 'Claude Code, Codex CLI, and Cursor CLI use their own local runtime and account setup.'
                          : 'Install and sign in to one of these, then rescan.'}
                      </p>
                    </div>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      className="h-8 shrink-0"
                      onClick={loadDiscovery}
                    >
                      <RefreshCw className="mr-1.5 h-3.5 w-3.5" />
                      Rescan
                    </Button>
                  </div>
                  <div className="grid gap-3 md:grid-cols-3">
                    {primaryCodingCliCandidates.map(renderCodingCliCard)}
                  </div>
                  {piCodingCliCandidate && (
                    <div className="mt-5">
                      <h3 className="mb-2 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                        <Terminal className="h-3.5 w-3.5" />
                        Multi-Provider Coding
                      </h3>
                      {renderPiDevCard(piCodingCliCandidate)}
                    </div>
                  )}
                  {otherCodingCliCandidates.length > 0 && (
                    <div className="mt-5">
                      <h3 className="mb-2 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                        <Terminal className="h-3.5 w-3.5" />
                        Other CLIs
                      </h3>
                      <div className="grid gap-3 lg:grid-cols-2 xl:grid-cols-4">
                        {otherCodingCliCandidates.map(renderCodingCliCard)}
                      </div>
                    </div>
                  )}
                </section>

                {/* API Providers — ready */}
                {(grouped.readyAPI.length > 0 || grouped.apiNeedsSetup.length > 0) && (
                  <button
                    type="button"
                    onClick={() => setShowAPIProviders(current => !current)}
                    className="inline-flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
                  >
                    {showAPIProviders ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
                    API Providers
                  </button>
                )}

                {showAPIProviders && grouped.readyAPI.length > 0 && (
                  <section>
                    <h3 className="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400 mb-2 flex items-center gap-1.5">
                      <KeyRound className="w-3.5 h-3.5" />
                      Also Detected API Providers
                    </h3>
                    <div className="grid gap-1.5 sm:grid-cols-2 lg:grid-cols-3 opacity-90">
                      {grouped.readyAPI.map(candidate => {
                        const testState = testStates[candidate.id] || { status: 'idle' as const }
                        const isEnabling = enablingId === candidate.id
                        return (
                          <div
                            key={candidate.id}
                            className="flex items-center justify-between gap-2 border border-gray-200 dark:border-slate-600 rounded px-3 py-1.5 bg-white dark:bg-slate-800 hover:border-blue-300 dark:hover:border-blue-500 transition-colors"
                          >
                            <span className="text-sm font-medium text-gray-900 dark:text-gray-100 truncate">{candidate.label}</span>
                            {testState.status === 'invalid' ? (
                              <span className="text-[10px] text-red-500 shrink-0" title={testState.message}>Failed</span>
                            ) : (
                              <button
                                onClick={() => enableCandidate(candidate)}
                                disabled={isEnabling || enablingId !== null}
                                className="text-xs font-medium text-blue-600 dark:text-blue-400 hover:text-blue-700 dark:hover:text-blue-300 disabled:opacity-50 shrink-0"
                              >
                                {isEnabling ? (
                                  <Loader2 className="w-3 h-3 animate-spin" />
                                ) : testState.status === 'valid' ? (
                                  <CheckCircle className="w-3.5 h-3.5 text-green-500" />
                                ) : (
                                  'Enable'
                                )}
                              </button>
                            )}
                          </div>
                        )
                      })}
                    </div>
                  </section>
                )}

                {showAPIProviders && grouped.apiNeedsSetup.length > 0 && (
                  <section>
                    <h3 className="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400 mb-2 flex items-center gap-1.5">
                      <AlertCircle className="w-3.5 h-3.5 text-amber-500" />
                      API Providers Needing Setup
                    </h3>
                    <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3 opacity-90">
                      {grouped.apiNeedsSetup.map(candidate => (
                        <div
                          key={candidate.id}
                          className="border border-gray-200 dark:border-slate-700 rounded px-2.5 py-2 bg-gray-50 dark:bg-slate-800/50"
                        >
                          <div className="flex items-center gap-2">
                            <KeyRound className="w-3.5 h-3.5 text-amber-500 shrink-0" />
                            <h4 className="text-xs font-medium text-gray-900 dark:text-gray-100 truncate">{candidate.label}</h4>
                          </div>
                          {(candidate.setup_hint || candidate.reason) && (
                            <p className="text-[11px] text-gray-500 dark:text-gray-400 mt-1 ml-5.5 line-clamp-1">
                              {candidate.setup_hint || candidate.reason}
                            </p>
                          )}
                        </div>
                      ))}
                    </div>
                  </section>
                )}
              </div>
            )}

            {/* Status messages */}
            {error && (
              <div className="mt-4 flex items-start gap-2 text-sm text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-950/20 border border-red-200 dark:border-red-900/40 rounded-lg p-3">
                <AlertCircle className="w-4 h-4 mt-0.5 shrink-0" />
                <span>{error}</span>
              </div>
            )}
            {success && (
              <div className="mt-4 flex items-start gap-2 text-sm text-green-600 dark:text-green-400 bg-green-50 dark:bg-green-950/20 border border-green-200 dark:border-green-900/40 rounded-lg p-3">
                <CheckCircle className="w-4 h-4 mt-0.5 shrink-0" />
                <span>{success}</span>
              </div>
            )}
          </div>

          {/* Footer */}
          <div className="flex items-center justify-between px-7 py-4 border-t border-gray-200 bg-white dark:border-slate-700 dark:bg-slate-800 shrink-0">
            <Button
              variant="ghost"
              size="sm"
              onClick={() => {
                if (onAdvancedSetup) { onAdvancedSetup(); return }
                onClose()
              }}
            >
              <Settings className="w-4 h-4 mr-2" />
              Advanced setup
            </Button>
            <Button
              variant="outline"
              size="sm"
              className="dark:border-slate-600 dark:bg-slate-800 dark:text-slate-100 dark:hover:bg-slate-700"
              onClick={onClose}
            >
              {success ? 'Done' : 'Skip for now'}
            </Button>
          </div>
        </div>
      </div>
    </ModalPortal>
  )
}
