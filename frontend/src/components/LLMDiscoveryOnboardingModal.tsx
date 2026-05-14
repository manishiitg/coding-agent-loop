import { useEffect, useMemo, useState } from 'react'
import { AlertCircle, CheckCircle, Loader2, RefreshCw, Settings, Terminal, X, KeyRound } from 'lucide-react'
import { Button } from './ui/Button'
import { llmConfigService } from '../services/llm-config-api'
import type { LLMDiscoveryCandidate, LLMModel, SavedLLM, TierModel } from '../services/api-types'
import { useLLMStore } from '../stores'

interface LLMDiscoveryOnboardingModalProps {
  isOpen: boolean
  onClose: () => void
  onAdvancedSetup?: () => void
}

type CandidateTestState = {
  status: 'idle' | 'testing' | 'valid' | 'invalid'
  message?: string
}

function candidateSortValue(candidate: LLMDiscoveryCandidate): number {
  if (candidate.usable && candidate.kind === 'local_cli') return 0
  if (candidate.usable) return 1
  if (candidate.kind === 'local_cli') return 2
  return 3
}

function candidateSubtitle(candidate: LLMDiscoveryCandidate): string {
  const parts = [candidate.model_name || candidate.model_id]
  if (candidate.runtime_command) {
    parts.push(`${candidate.runtime_command} ${candidate.runtime_available ? 'found' : 'missing'}`)
  }
  if (candidate.auth_source) {
    parts.push(candidate.auth_source)
  }
  return parts.filter(Boolean).join(' · ')
}

function modelForCandidate(candidate: LLMDiscoveryCandidate): LLMModel {
  const options: Record<string, unknown> = {}
  if (candidate.provider === 'codex-cli') {
    options.reasoning_effort = 'medium'
  }
  if (candidate.provider === 'claude-code') {
    options.reasoning_effort = 'high'
  }

  return {
    provider: candidate.provider,
    model_id: candidate.model_id,
    ...(Object.keys(options).length > 0 ? { options } : {}),
  }
}

function publishedName(candidate: LLMDiscoveryCandidate): string {
  if (candidate.provider === 'codex-cli') return 'Codex CLI'
  if (candidate.provider === 'claude-code') return 'Claude Code'
  if (candidate.provider === 'gemini-cli') return `Gemini CLI (${candidate.model_id})`
  if (candidate.provider === 'kimi' && candidate.model_id === 'kimi-code') return 'Kimi Code'
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
  } = useLLMStore()

  const [candidates, setCandidates] = useState<LLMDiscoveryCandidate[]>([])
  const [notes, setNotes] = useState<string[]>([])
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [isLoading, setIsLoading] = useState(false)
  const [isEnabling, setIsEnabling] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState<string | null>(null)
  const [testStates, setTestStates] = useState<Record<string, CandidateTestState>>({})

  const sortedCandidates = useMemo(
    () => [...candidates].sort((a, b) => candidateSortValue(a) - candidateSortValue(b) || a.label.localeCompare(b.label)),
    [candidates]
  )

  const selectedCandidate = useMemo(
    () => sortedCandidates.find(candidate => candidate.id === selectedId) || sortedCandidates[0] || null,
    [selectedId, sortedCandidates]
  )

  const loadDiscovery = async () => {
    setIsLoading(true)
    setError(null)
    setSuccess(null)
    try {
      if (!defaultsLoaded) {
        await loadDefaultsFromBackend()
      }
      const response = await llmConfigService.discoverLLMSetup()
      const nextCandidates = response.candidates || []
      setCandidates(nextCandidates)
      setNotes(response.notes || [])
      setTestStates({})
      const recommended = nextCandidates
        .filter(candidate => candidate.usable)
        .sort((a, b) => candidateSortValue(a) - candidateSortValue(b))[0] || nextCandidates[0] || null
      setSelectedId(recommended?.id || null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to discover local LLM setup.')
    } finally {
      setIsLoading(false)
    }
  }

  useEffect(() => {
    if (isOpen) {
      loadDiscovery()
    }
  }, [isOpen])

  if (!isOpen) return null

  const validateCandidate = async (candidate: LLMDiscoveryCandidate) => {
    const validation = await llmConfigService.validateAPIKey({
      provider: candidate.provider,
      model_id: candidate.model_id,
    })
    if (!validation.valid) {
      throw new Error(validation.message || validation.error || `${candidate.label} is not ready yet.`)
    }
    return validation.message || `${candidate.label} is working.`
  }

  const testCandidate = async (candidate: LLMDiscoveryCandidate) => {
    setError(null)
    setSuccess(null)
    setTestStates(prev => ({
      ...prev,
      [candidate.id]: { status: 'testing' },
    }))

    try {
      const message = await validateCandidate(candidate)
      setTestStates(prev => ({
        ...prev,
        [candidate.id]: { status: 'valid', message },
      }))
    } catch (err) {
      setTestStates(prev => ({
        ...prev,
        [candidate.id]: { status: 'invalid', message: err instanceof Error ? err.message : 'Connection test failed.' },
      }))
    }
  }

  const enableCandidate = async () => {
    if (!selectedCandidate) return

    setIsEnabling(true)
    setError(null)
    setSuccess(null)

    try {
      const validationMessage = await validateCandidate(selectedCandidate)
      setTestStates(prev => ({
        ...prev,
        [selectedCandidate.id]: { status: 'valid', message: validationMessage },
      }))

      const llmModel = modelForCandidate(selectedCandidate)
      const alreadyPublished = savedLLMs.some(
        (saved: SavedLLM) => saved.provider === llmModel.provider && saved.model_id === llmModel.model_id
      )
      if (!alreadyPublished) {
        await saveLLM(
          llmModel,
          publishedName(selectedCandidate),
          selectedCandidate.model_name || selectedCandidate.model_id,
          selectedCandidate.kind === 'local_cli' ? 'none' : 'api_key'
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
      setDelegationTierConfig({
        main: tier,
        high: tier,
        medium: tier,
        low: tier,
      })

      setSuccess(`${publishedName(selectedCandidate)} is enabled for multi-agent mode.`)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to enable this model.')
    } finally {
      setIsEnabling(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={onClose}>
      <div
        className="bg-white dark:bg-slate-800 rounded-lg shadow-xl w-full max-w-3xl mx-4 max-h-[88vh] flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200 dark:border-slate-700 shrink-0">
          <div>
            <h2 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Set Up Models</h2>
            <p className="text-sm text-gray-500 dark:text-gray-400 mt-0.5">Use an installed CLI or configured provider key.</p>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={loadDiscovery}
              disabled={isLoading}
              className="p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 transition-colors disabled:opacity-50"
              title="Run discovery again"
            >
              <RefreshCw className={`w-5 h-5 ${isLoading ? 'animate-spin' : ''}`} />
            </button>
            <button
              onClick={onClose}
              className="p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 transition-colors"
              aria-label="Close"
            >
              <X className="w-5 h-5" />
            </button>
          </div>
        </div>

        <div className="p-6 overflow-y-auto min-h-0">
          {isLoading && (
            <div className="flex items-center gap-3 text-sm text-gray-600 dark:text-gray-300 py-8">
              <Loader2 className="w-5 h-5 animate-spin" />
              Checking local CLIs and configured provider auth...
            </div>
          )}

          {!isLoading && sortedCandidates.length === 0 && (
            <div className="border border-dashed border-gray-300 dark:border-slate-600 rounded-lg p-5">
              <div className="flex items-start gap-3">
                <AlertCircle className="w-5 h-5 text-amber-500 mt-0.5" />
                <div>
                  <h3 className="font-medium text-gray-900 dark:text-gray-100">No ready model setup was detected</h3>
                  <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
                    Install Codex, Claude Code, Gemini CLI, or add a provider key in advanced setup.
                  </p>
                </div>
              </div>
            </div>
          )}

          {!isLoading && sortedCandidates.length > 0 && (
            <div className="space-y-3">
              {sortedCandidates.map(candidate => {
                const selected = selectedCandidate?.id === candidate.id
                const Icon = candidate.kind === 'local_cli' ? Terminal : KeyRound
                const testState = testStates[candidate.id] || { status: 'idle' as const }
                return (
                  <div
                    key={candidate.id}
                    data-selected={selected ? 'true' : 'false'}
                    role="button"
                    tabIndex={0}
                    aria-label={`Select ${candidate.label}`}
                    onClick={() => setSelectedId(candidate.id)}
                    onKeyDown={(event) => {
                      if (event.key === 'Enter' || event.key === ' ') {
                        event.preventDefault()
                        setSelectedId(candidate.id)
                      }
                    }}
                    className={`llm-discovery-candidate w-full text-left border rounded-lg p-4 transition-colors cursor-pointer focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2 dark:focus:ring-offset-slate-800 ${
                      selected
                        ? 'border-blue-500'
                        : 'border-gray-200 dark:border-slate-600'
                    }`}
                  >
                    <div className="flex items-start gap-3">
                      <Icon className={`w-5 h-5 mt-0.5 ${candidate.usable ? 'text-green-500' : 'text-amber-500'}`} />
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                          <h3 className="font-medium text-gray-900 dark:text-gray-100">{candidate.label}</h3>
                          {candidate.usable ? (
                            <span className="text-xs px-2 py-0.5 rounded-full bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-300">Ready</span>
                          ) : (
                            <span className="text-xs px-2 py-0.5 rounded-full bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300">Needs setup</span>
                          )}
                        </div>
                        <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">{candidateSubtitle(candidate)}</p>
                        <p className="text-xs text-gray-500 dark:text-gray-400 mt-2">{candidate.reason}</p>
                        {candidate.setup_hint && (
                          <p className="text-xs text-amber-600 dark:text-amber-300 mt-1">{candidate.setup_hint}</p>
                        )}
                        {testState.status === 'valid' && testState.message && (
                          <p className="text-xs text-green-600 dark:text-green-400 mt-2">{testState.message}</p>
                        )}
                        {testState.status === 'invalid' && testState.message && (
                          <p className="text-xs text-red-600 dark:text-red-400 mt-2">{testState.message}</p>
                        )}
                      </div>
                      <div className="shrink-0">
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          className="dark:border-slate-600 dark:bg-slate-800 dark:text-slate-100 dark:hover:bg-slate-700 dark:hover:text-white"
                          onClick={(event) => {
                            event.stopPropagation()
                            setSelectedId(candidate.id)
                            testCandidate(candidate)
                          }}
                          disabled={testState.status === 'testing'}
                        >
                          {testState.status === 'testing' ? (
                            <>
                              <Loader2 className="w-4 h-4 mr-1 animate-spin" />
                              Testing
                            </>
                          ) : testState.status === 'valid' ? (
                            <>
                              <CheckCircle className="w-4 h-4 mr-1 text-green-500" />
                              Test again
                            </>
                          ) : testState.status === 'invalid' ? (
                            <>
                              <AlertCircle className="w-4 h-4 mr-1 text-red-500" />
                              Retry
                            </>
                          ) : (
                            'Test'
                          )}
                        </Button>
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>
          )}

          {notes.length > 0 && (
            <div className="mt-4 text-xs text-gray-500 dark:text-gray-400 space-y-1">
              {notes.map(note => <p key={note}>{note}</p>)}
            </div>
          )}

          {error && (
            <div className="mt-4 flex items-start gap-2 text-sm text-red-600 dark:text-red-400">
              <AlertCircle className="w-4 h-4 mt-0.5 shrink-0" />
              <span>{error}</span>
            </div>
          )}

          {success && (
            <div className="mt-4 flex items-start gap-2 text-sm text-green-600 dark:text-green-400">
              <CheckCircle className="w-4 h-4 mt-0.5 shrink-0" />
              <span>{success}</span>
            </div>
          )}
        </div>

        <div className="flex items-center justify-between px-6 py-4 border-t border-gray-200 dark:border-slate-700 shrink-0">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              onClose()
              onAdvancedSetup?.()
            }}
          >
            <Settings className="w-4 h-4 mr-2" />
            Advanced setup
          </Button>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              className="dark:border-slate-600 dark:bg-slate-800 dark:text-slate-100 dark:hover:bg-slate-700 dark:hover:text-white"
              onClick={onClose}
            >
              Close
            </Button>
            <Button
              size="sm"
              onClick={enableCandidate}
              disabled={!selectedCandidate || isEnabling}
            >
              {isEnabling && <Loader2 className="w-4 h-4 mr-2 animate-spin" />}
              Test and enable
            </Button>
          </div>
        </div>
      </div>
    </div>
  )
}
