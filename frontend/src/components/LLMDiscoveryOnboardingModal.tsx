import { useEffect, useMemo, useState } from 'react'
import { AlertCircle, CheckCircle, Copy, Loader2, RefreshCw, Settings, Terminal, X, KeyRound } from 'lucide-react'
import { Button } from './ui/Button'
import { llmConfigService, type ModelMetadata } from '../services/llm-config-api'
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

const CODING_CLI_ORDER = ['codex-cli', 'claude-code', 'cursor-cli', 'opencode-cli', 'gemini-cli'] as const

const CODING_CLI_INFO: Record<typeof CODING_CLI_ORDER[number], {
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
  'opencode-cli': {
    label: 'OpenCode CLI',
    binary: 'opencode',
    setupCommand: 'opencode auth login',
    installHint: 'Install OpenCode CLI so the opencode command is available on the backend PATH, or set OPENCODE_BIN.',
  },
  'gemini-cli': {
    label: 'Gemini CLI',
    binary: 'gemini',
    setupCommand: 'gemini',
    installHint: 'Install Gemini CLI so the gemini command is available on the backend PATH.',
  },
}

const isCodingCLIProvider = (provider: string): provider is typeof CODING_CLI_ORDER[number] =>
  (CODING_CLI_ORDER as readonly string[]).includes(provider)

function fallbackCodingCandidate(provider: typeof CODING_CLI_ORDER[number]): LLMDiscoveryCandidate {
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

function candidateReadiness(candidate: LLMDiscoveryCandidate): ReadinessLevel {
  if (candidate.kind === 'local_cli' && candidate.runtime_available === false) return 'not_detected'
  if (!candidate.usable) return 'needs_setup'
  return 'ready'
}

function modelForCandidate(candidate: LLMDiscoveryCandidate, selectedModelId?: string): LLMModel {
  const options: Record<string, unknown> = {}
  if (candidate.provider === 'codex-cli') options.reasoning_effort = 'medium'
  if (candidate.provider === 'claude-code') options.reasoning_effort = 'high'

  return {
    provider: candidate.provider,
    model_id: selectedModelId || candidate.model_id,
    ...(Object.keys(options).length > 0 ? { options } : {}),
  }
}

function publishedName(candidate: LLMDiscoveryCandidate, selectedModelId?: string): string {
  if (candidate.provider === 'codex-cli') return 'Codex CLI'
  if (candidate.provider === 'cursor-cli') return 'Cursor CLI'
  if (candidate.provider === 'opencode-cli') return 'OpenCode CLI'
  if (candidate.provider === 'claude-code') return 'Claude Code'
  if (candidate.provider === 'gemini-cli') return `Gemini CLI (${selectedModelId || candidate.model_id})`
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
    providerManifestLoaded,
    loadProviderManifest,
  } = useLLMStore()

  const [candidates, setCandidates] = useState<LLMDiscoveryCandidate[]>([])
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState<string | null>(null)
  const [testStates, setTestStates] = useState<Record<string, CandidateTestState>>({})
  const [enablingId, setEnablingId] = useState<string | null>(null)
  const [copiedCommand, setCopiedCommand] = useState<string | null>(null)
  const [metadata, setMetadata] = useState<ModelMetadata[]>([])

  const codingCliCandidates = useMemo(() => {
    const byProvider = new Map<string, LLMDiscoveryCandidate>()
    for (const candidate of candidates) {
      if (isCodingCLIProvider(candidate.provider) && !byProvider.has(candidate.provider)) {
        byProvider.set(candidate.provider, candidate)
      }
    }
    return CODING_CLI_ORDER.map(provider => byProvider.get(provider) || fallbackCodingCandidate(provider))
  }, [candidates])

  const grouped = useMemo(() => {
    const readyAPI: LLMDiscoveryCandidate[] = []
    const apiNeedsSetup: LLMDiscoveryCandidate[] = []
    for (const c of candidates) {
      if (isCodingCLIProvider(c.provider)) continue
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
      if (!providerManifestLoaded) loadProviderManifest()
      const [response, metaResponse] = await Promise.all([
        llmConfigService.discoverLLMSetup(),
        llmConfigService.getModelMetadata().catch(() => ({ models: [] })),
      ])
      setCandidates(response.candidates || [])
      setMetadata(metaResponse.models || [])
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
    const providerInfo = isCodingCLIProvider(candidate.provider)
      ? CODING_CLI_INFO[candidate.provider]
      : undefined
    const binary = candidate.runtime_command || providerInfo?.binary || candidate.provider
    const setupCommand = providerInfo?.setupCommand
    const modelMeta = metadata.find(m => m.provider === candidate.provider && m.model_id === candidate.model_id)
    const statusClasses = readiness === 'ready'
      ? 'bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-300'
      : readiness === 'needs_setup'
        ? 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300'
        : 'bg-slate-100 text-slate-600 dark:bg-slate-700 dark:text-slate-300'
    const statusLabel = readiness === 'ready'
      ? 'Ready'
      : readiness === 'needs_setup'
        ? 'Login needed'
        : 'Install'

    return (
      <div
        key={candidate.id}
        className="rounded-lg border border-gray-200 bg-white p-4 shadow-sm dark:border-slate-700 dark:bg-slate-800"
      >
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <Terminal className="h-4 w-4 shrink-0 text-blue-600 dark:text-blue-300" />
              <h4 className="truncate text-sm font-semibold text-gray-900 dark:text-gray-100">{candidate.label}</h4>
            </div>
            <div className="mt-1 flex flex-wrap items-center gap-1.5 text-[11px] text-gray-500 dark:text-gray-400">
              <code className="rounded bg-gray-100 px-1.5 py-0.5 font-mono dark:bg-slate-700">{binary}</code>
              <span>{candidate.detection_source}</span>
            </div>
          </div>
          <span className={`shrink-0 rounded-full px-2 py-0.5 text-[11px] font-medium ${statusClasses}`}>
            {statusLabel}
          </span>
        </div>

        <div className="mt-3 min-h-[42px] text-xs text-gray-600 dark:text-gray-300">
          {readiness === 'ready' ? (
            <div className="space-y-1">
              <div>
                Default model: <span className="font-medium text-gray-900 dark:text-gray-100">{modelMeta?.model_name || candidate.model_name || candidate.model_id}</span>
              </div>
              <div className="text-gray-500 dark:text-gray-400">
                Auth: {candidate.auth_source || candidate.reason}
              </div>
            </div>
          ) : (
            <p>{candidate.setup_hint || candidate.reason}</p>
          )}
        </div>

        <div className="mt-4 flex items-center justify-between gap-2">
          {readiness === 'ready' ? (
            <>
              {testState.status === 'invalid' ? (
                <span className="truncate text-xs text-red-500" title={testState.message}>{testState.message || 'Validation failed'}</span>
              ) : (
                <span className="text-xs text-gray-500 dark:text-gray-400">
                  {testState.status === 'valid' ? 'Validated' : 'One-click setup'}
                </span>
              )}
              <Button
                size="sm"
                onClick={() => enableCandidate(candidate)}
                disabled={isEnabling || enablingId !== null}
                className="h-8"
              >
                {isEnabling ? (
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                ) : testState.status === 'valid' ? (
                  <CheckCircle className="h-3.5 w-3.5" />
                ) : (
                  'Enable'
                )}
              </Button>
            </>
          ) : readiness === 'needs_setup' && setupCommand ? (
            <>
              <code className="truncate rounded bg-gray-100 px-2 py-1 text-xs dark:bg-slate-700">{setupCommand}</code>
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
            <>
              <span className="text-xs text-gray-500 dark:text-gray-400">Install, then rescan.</span>
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
            </>
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
      const llmModel = modelForCandidate(candidate, modelId)
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

      const tier: TierModel = { provider: llmModel.provider, model_id: llmModel.model_id }
      setChatPrimaryConfig({
        provider: llmModel.provider,
        model_id: llmModel.model_id,
        fallback_models: [],
        cross_provider_fallback: undefined,
      })
      setAgentConfig({ primary: llmModel, fallbacks: [] })
      setDelegationTierConfig({ main: tier, high: tier, medium: tier, low: tier })

      setSuccess(`${publishedName(candidate, modelId)} is enabled and ready to use.`)
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
          className="bg-white dark:bg-slate-800 rounded-xl shadow-2xl w-full max-w-4xl max-h-[calc(100vh-2rem)] flex flex-col"
          onClick={(e) => e.stopPropagation()}
        >
          {/* Header */}
          <div className="flex items-center justify-between px-6 py-5 border-b border-gray-200 dark:border-slate-700 shrink-0">
            <div>
              <h2 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Connect a Coding CLI</h2>
              <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
                Start with a local coding agent. If none are installed yet, these are the CLIs we support.
              </p>
            </div>
            <div className="flex items-center gap-2">
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
          <div className="p-6 overflow-y-auto min-h-0 flex-1">
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
                      <h3 className="flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                        <Terminal className="h-3.5 w-3.5" />
                        Supported Coding CLIs
                      </h3>
                      <p className="mt-1 text-sm text-gray-600 dark:text-gray-300">
                        {hasReadyCodingCLI
                          ? 'Detected CLIs are ready to enable. Other supported CLIs stay visible for later.'
                          : 'No supported coding CLI is ready yet. Install one of these, sign in, then rescan.'}
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
                  <div className="grid gap-3 md:grid-cols-2">
                    {codingCliCandidates.map(renderCodingCliCard)}
                  </div>
                </section>

                {/* API Providers — ready */}
                {grouped.readyAPI.length > 0 && (
                  <section>
                    <h3 className="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400 mb-2 flex items-center gap-1.5">
                      <KeyRound className="w-3.5 h-3.5" />
                      Also Detected API Providers
                    </h3>
                    <div className="grid gap-1.5 sm:grid-cols-2 lg:grid-cols-3">
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

                {grouped.apiNeedsSetup.length > 0 && (
                  <section>
                    <h3 className="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400 mb-2 flex items-center gap-1.5">
                      <AlertCircle className="w-3.5 h-3.5 text-amber-500" />
                      API Providers Needing Setup
                    </h3>
                    <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
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
          <div className="flex items-center justify-between px-6 py-4 border-t border-gray-200 dark:border-slate-700 shrink-0">
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
