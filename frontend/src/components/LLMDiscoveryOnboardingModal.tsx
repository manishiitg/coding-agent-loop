import { useEffect, useMemo, useState } from 'react'
import { AlertCircle, CheckCircle, Loader2, RefreshCw, Settings, Terminal, X, KeyRound, ChevronDown } from 'lucide-react'
import { Button } from './ui/Button'
import { llmConfigService, type ModelMetadata } from '../services/llm-config-api'
import type { LLMDiscoveryCandidate, LLMModel, SavedLLM, TierModel } from '../services/api-types'
import { useLLMStore } from '../stores'
import ModalPortal from './ui/ModalPortal'
import { getProviderIntegrationKind } from '../utils/llmDisplay'

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
  const [showNotDetected, setShowNotDetected] = useState(false)
  const [metadata, setMetadata] = useState<ModelMetadata[]>([])

  const isCodingAgent = (c: LLMDiscoveryCandidate) =>
    getProviderIntegrationKind(c.provider, c.model_id) === 'coding_agent'

  const grouped = useMemo(() => {
    const readyCLI: LLMDiscoveryCandidate[] = []
    const readyAPI: LLMDiscoveryCandidate[] = []
    const needsSetup: LLMDiscoveryCandidate[] = []
    const notDetected: LLMDiscoveryCandidate[] = []
    for (const c of candidates) {
      const level = candidateReadiness(c)
      if (level === 'ready') {
        if (isCodingAgent(c)) readyCLI.push(c)
        else readyAPI.push(c)
      } else if (level === 'needs_setup') {
        needsSetup.push(c)
      } else {
        notDetected.push(c)
      }
    }
    return { readyCLI, readyAPI, needsSetup, notDetected }
  }, [candidates])

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
              <h2 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Set Up Models</h2>
              <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
                Pick a provider and model to get started. We detected what's available on this machine.
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

            {!isLoading && candidates.length === 0 && (
              <div className="flex flex-col items-center justify-center py-16 text-center">
                <AlertCircle className="w-10 h-10 text-amber-400 mb-3" />
                <h3 className="font-medium text-gray-900 dark:text-gray-100 mb-1">No providers detected</h3>
                <p className="text-sm text-gray-500 dark:text-gray-400 max-w-md">
                  Install a coding CLI (Codex, Cursor, Claude Code, Gemini, OpenCode) or configure an API provider key.
                </p>
              </div>
            )}

            {!isLoading && candidates.length > 0 && (
              <div className="space-y-5">
                {/* Coding Agents — ready */}
                {grouped.readyCLI.length > 0 && (
                  <section>
                    <h3 className="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400 mb-2 flex items-center gap-1.5">
                      <Terminal className="w-3.5 h-3.5" />
                      Coding Agents
                    </h3>
                    <div className="grid gap-1.5 sm:grid-cols-2 lg:grid-cols-3">
                      {grouped.readyCLI.map(candidate => {
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

                {/* API Providers — ready */}
                {grouped.readyAPI.length > 0 && (
                  <section>
                    <h3 className="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400 mb-2 flex items-center gap-1.5">
                      <KeyRound className="w-3.5 h-3.5" />
                      API Providers
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

                {/* Needs setup */}
                {grouped.needsSetup.length > 0 && (
                  <section>
                    <h3 className="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400 mb-2 flex items-center gap-1.5">
                      <AlertCircle className="w-3.5 h-3.5 text-amber-500" />
                      Needs Setup
                    </h3>
                    <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
                      {grouped.needsSetup.map(candidate => (
                        <div
                          key={candidate.id}
                          className="border border-gray-200 dark:border-slate-700 rounded px-2.5 py-2 bg-gray-50 dark:bg-slate-800/50"
                        >
                          <div className="flex items-center gap-2">
                            {isCodingAgent(candidate) ? (
                              <Terminal className="w-3.5 h-3.5 text-amber-500 shrink-0" />
                            ) : (
                              <KeyRound className="w-3.5 h-3.5 text-amber-500 shrink-0" />
                            )}
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

                {/* Not detected — collapsed */}
                {grouped.notDetected.length > 0 && (
                  <section>
                    <button
                      onClick={() => setShowNotDetected(!showNotDetected)}
                      className="text-sm font-semibold text-gray-500 dark:text-gray-400 flex items-center gap-2 hover:text-gray-700 dark:hover:text-gray-300 transition-colors"
                    >
                      <ChevronDown className={`w-4 h-4 transition-transform ${showNotDetected ? '' : '-rotate-90'}`} />
                      Not detected ({grouped.notDetected.length})
                    </button>
                    {showNotDetected && (
                      <div className="mt-2 grid gap-1.5 sm:grid-cols-2 lg:grid-cols-3">
                        {grouped.notDetected.map(candidate => (
                          <div
                            key={candidate.id}
                            className="flex items-center gap-2 px-2.5 py-1.5 text-xs text-gray-400 dark:text-slate-500 rounded border border-dashed border-gray-200 dark:border-slate-700"
                          >
                            <Terminal className="w-3 h-3 shrink-0" />
                            <span className="truncate">{candidate.label}</span>
                          </div>
                        ))}
                      </div>
                    )}
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
