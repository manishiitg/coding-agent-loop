import { useEffect, useMemo, useState } from 'react'
import { AlertCircle, CheckCircle, Loader2, RefreshCw, Settings, Terminal, X, KeyRound } from 'lucide-react'
import { Button } from './ui/Button'
import { llmConfigService } from '../services/llm-config-api'
import type { LLMDiscoveryCandidate, LLMModel, SavedLLM, TierModel } from '../services/api-types'
import { useLLMStore } from '../stores'
import ModalPortal from './ui/ModalPortal'
import {
  getProviderIntegrationInfo,
  getProviderIntegrationKind,
  type LLMIntegrationKind,
} from '../utils/llmDisplay'
import { CodingAgentCapabilities } from './llm/CodingAgentCapabilities'

interface LLMDiscoveryOnboardingModalProps {
  isOpen: boolean
  onClose: () => void
  onAdvancedSetup?: () => void
}

type CandidateTestState = {
  status: 'idle' | 'testing' | 'valid' | 'invalid'
  message?: string
}

type DiscoveryIntegrationKind = Exclude<LLMIntegrationKind, 'audio_provider'>

const DISCOVERY_INTEGRATION_ORDER: DiscoveryIntegrationKind[] = ['coding_agent', 'api_model']

function candidateSortValue(candidate: LLMDiscoveryCandidate): number {
  if (candidate.usable && candidate.kind === 'local_cli') return 0
  if (candidate.usable) return 1
  if (candidate.kind === 'local_cli') return 2
  return 3
}

function candidateSubtitle(candidate: LLMDiscoveryCandidate): string {
  if (candidate.kind === 'local_cli') {
    return ''
  }

  const parts: string[] = []
  parts.push(candidate.model_name || candidate.model_id)
  if (candidate.runtime_command) {
    if (candidate.runtime_available === false) {
      parts.push(`${candidate.runtime_command} missing`)
    } else {
      parts.push(`${candidate.runtime_command} found`)
    }
  }
  if (!candidateIsDisabled(candidate) && candidate.auth_source) {
    parts.push(candidate.auth_source)
  }
  return parts.filter(Boolean).join(' · ')
}

function candidateIsDisabled(candidate: LLMDiscoveryCandidate): boolean {
  return candidate.kind === 'local_cli' && candidate.runtime_available === false
}

function candidateHeaderDetail(candidate: LLMDiscoveryCandidate): string {
  if (candidate.kind === 'local_cli') {
    return ''
  }
  return candidate.model_name || candidate.model_id
}

function shouldShowStatusReason(candidate: LLMDiscoveryCandidate, testState: CandidateTestState): boolean {
  if (candidate.kind === 'local_cli') {
    return false
  }
  if (candidateIsDisabled(candidate) || !candidate.usable) {
    return true
  }
  return testState.status === 'testing' || testState.status === 'invalid'
}

function candidateCardStatus(candidate: LLMDiscoveryCandidate, testState: CandidateTestState) {
  if (testState.status === 'testing') {
    return {
      label: 'Testing',
      badgeClass: 'bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300',
      iconClass: 'text-blue-500',
      reason: 'Checking this setup now.',
    }
  }
  if (testState.status === 'valid') {
    return {
      label: 'Verified',
      badgeClass: 'bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-300',
      iconClass: 'text-green-500',
      reason: candidate.kind === 'local_cli' ? 'CLI login was verified.' : 'Provider auth was verified.',
    }
  }
  if (testState.status === 'invalid') {
    return {
      label: 'Failed',
      badgeClass: 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300',
      iconClass: 'text-red-500',
      reason: 'Test failed. Check the setup hint below.',
    }
  }
  if (candidateIsDisabled(candidate)) {
    return {
      label: 'Not detected',
      badgeClass: 'bg-gray-100 text-gray-600 dark:bg-slate-700 dark:text-slate-300',
      iconClass: 'text-gray-400 dark:text-slate-500',
      reason: 'CLI runtime was not found on this machine.',
    }
  }
  if (!candidate.usable) {
    return {
      label: 'Needs setup',
      badgeClass: 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300',
      iconClass: 'text-amber-500',
      reason: candidate.reason,
    }
  }
  if (candidate.kind === 'local_cli') {
    return {
      label: 'Installed',
      badgeClass: 'bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300',
      iconClass: 'text-blue-500',
      reason: 'CLI runtime was detected. Use Test to verify login before enabling.',
    }
  }
  return {
    label: 'Configured',
    badgeClass: 'bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300',
    iconClass: 'text-blue-500',
    reason: 'Provider auth was detected. Use Test to verify it before enabling.',
  }
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
  if (candidate.provider === 'cursor-cli') return 'Cursor CLI'
  if (candidate.provider === 'claude-code') return 'Claude Code'
  if (candidate.provider === 'gemini-cli') return `Gemini CLI (${candidate.model_id})`
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

  const candidateGroups = useMemo(() => {
    return DISCOVERY_INTEGRATION_ORDER.map(kind => ({
      kind,
      candidates: sortedCandidates.filter(candidate => getProviderIntegrationKind(candidate.provider, candidate.model_id) === kind),
    })).filter(group => group.candidates.length > 0)
  }, [sortedCandidates])

  const selectedCandidate = useMemo(
    () =>
      sortedCandidates.find(candidate => candidate.id === selectedId && !candidateIsDisabled(candidate)) ||
      sortedCandidates.find(candidate => !candidateIsDisabled(candidate)) ||
      null,
    [selectedId, sortedCandidates]
  )
  const integrationIcons: Record<DiscoveryIntegrationKind, typeof Terminal> = {
    coding_agent: Terminal,
    api_model: KeyRound,
  }

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
        .filter(candidate => candidate.usable && !candidateIsDisabled(candidate))
        .sort((a, b) => candidateSortValue(a) - candidateSortValue(b))[0] ||
        nextCandidates.find(candidate => !candidateIsDisabled(candidate)) ||
        null
      setSelectedId(recommended && !candidateIsDisabled(recommended) ? recommended.id : null)
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
    <ModalPortal>
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <div
        className="bg-white dark:bg-slate-800 rounded-lg shadow-xl w-full max-w-5xl max-h-[calc(100vh-2rem)] flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200 dark:border-slate-700 shrink-0">
          <div>
            <h2 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Set Up Models</h2>
            <p className="text-sm text-gray-500 dark:text-gray-400 mt-0.5">
              We check this machine for coding CLIs and configured provider auth. Test one to verify it works before enabling.
            </p>
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
                    Install Codex, Cursor CLI, Claude Code, Gemini CLI, or add a provider key in advanced setup.
                  </p>
                </div>
              </div>
            </div>
          )}

          {!isLoading && sortedCandidates.length > 0 && (
            <div className="space-y-5">
              <div className="rounded-md border border-blue-200 bg-blue-50 px-3 py-2 text-xs text-blue-800 dark:border-blue-900/60 dark:bg-blue-950/30 dark:text-blue-200">
                Coding CLIs are shown even when missing. Detected means the CLI or credentials are present; testing verifies login, model access, and basic provider connectivity.
              </div>
              {candidateGroups.map(group => {
                const integrationInfo = getProviderIntegrationInfo(group.candidates[0]?.provider, group.candidates[0]?.model_id)
                const IntegrationIcon = integrationIcons[group.kind]
                return (
                  <section key={group.kind} className="overflow-hidden rounded-md border border-gray-200 bg-white dark:border-slate-700 dark:bg-slate-800">
                    <div className="flex items-center justify-between border-b border-gray-200 bg-gray-50 px-3 py-2 dark:border-slate-700 dark:bg-slate-900/40">
                      <div className={`flex items-center gap-2 text-xs font-semibold uppercase tracking-wide ${integrationInfo.toneClass}`}>
                        <IntegrationIcon className="w-4 h-4" />
                        {integrationInfo.label}
                      </div>
                      <span className="rounded bg-white px-1.5 py-0.5 text-[11px] text-gray-500 dark:bg-slate-800 dark:text-slate-400">
                        {group.candidates.length}
                      </span>
                    </div>
                    <div className="grid gap-3 p-3 [grid-template-columns:repeat(auto-fit,minmax(260px,1fr))]">
                      {group.candidates.map(candidate => {
                        const disabled = candidateIsDisabled(candidate)
                        const selected = !disabled && selectedCandidate?.id === candidate.id
                        const testState = testStates[candidate.id] || { status: 'idle' as const }
                        const cardStatus = candidateCardStatus(candidate, testState)
                        const headerDetail = candidateHeaderDetail(candidate)
                        const subtitle = candidateSubtitle(candidate)
                        return (
                          <div
                            key={candidate.id}
                            data-selected={selected ? 'true' : 'false'}
                            role="button"
                            aria-disabled={disabled}
                            tabIndex={disabled ? -1 : 0}
                            aria-label={`Select ${candidate.label}`}
                            onClick={() => {
                              if (!disabled) {
                                setSelectedId(candidate.id)
                              }
                            }}
                            onKeyDown={(event) => {
                              if (!disabled && (event.key === 'Enter' || event.key === ' ')) {
                                event.preventDefault()
                                setSelectedId(candidate.id)
                              }
                            }}
                            className={`llm-discovery-candidate flex min-h-[136px] w-full flex-col text-left border rounded-lg p-3 transition-colors focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2 dark:focus:ring-offset-slate-800 ${
                              disabled
                                ? 'cursor-not-allowed border-gray-200 bg-gray-50 opacity-70 dark:border-slate-700 dark:bg-slate-900/40'
                                : selected
                                ? 'border-blue-500 bg-blue-50/70 ring-1 ring-blue-500 dark:bg-blue-950/20'
                                : 'border-gray-200 bg-white hover:border-gray-300 hover:bg-gray-50 dark:border-slate-600 dark:bg-slate-800 dark:hover:border-slate-500 dark:hover:bg-slate-700/40'
                            }`}
                          >
                            <div className="flex min-h-0 flex-1 flex-col">
                              <div className="flex items-start justify-between gap-2">
                                <div className="flex min-w-0 items-start gap-2">
                                  <IntegrationIcon className={`mt-0.5 h-5 w-5 shrink-0 ${cardStatus.iconClass}`} />
                                  <div className="min-w-0">
                                    <h3 className="truncate font-medium text-gray-900 dark:text-gray-100">{candidate.label}</h3>
                                    {headerDetail && (
                                      <p className="mt-0.5 truncate text-xs text-gray-500 dark:text-gray-400">{headerDetail}</p>
                                    )}
                                  </div>
                                </div>
                                <div className="shrink-0">
                                  <span className={`rounded-full px-2 py-0.5 text-xs ${cardStatus.badgeClass}`}>{cardStatus.label}</span>
                                </div>
                              </div>

                              <div className="mt-3 space-y-2">
                                {subtitle && (
                                  <p className="truncate text-xs text-gray-500 dark:text-gray-400">{subtitle}</p>
                                )}
                                {shouldShowStatusReason(candidate, testState) && (
                                  <p className="text-xs text-gray-500 dark:text-gray-400">{cardStatus.reason}</p>
                                )}
                                {getProviderIntegrationKind(candidate.provider, candidate.model_id) === 'coding_agent' && (
                                  <CodingAgentCapabilities provider={candidate.provider} modelId={candidate.model_id} compact />
                                )}
                                {candidate.kind !== 'local_cli' && candidate.setup_hint && (
                                  <p className="text-xs text-amber-600 dark:text-amber-300">{candidate.setup_hint}</p>
                                )}
                                {candidate.kind !== 'local_cli' && testState.status === 'valid' && testState.message && (
                                  <p className="text-xs text-green-600 dark:text-green-400">{testState.message}</p>
                                )}
                                {candidate.kind !== 'local_cli' && testState.status === 'invalid' && testState.message && (
                                  <p className="text-xs text-red-600 dark:text-red-400">{testState.message}</p>
                                )}
                              </div>

                              <div className="mt-auto flex justify-end pt-3">
                                <Button
                                  type="button"
                                  variant="outline"
                                  size="sm"
                                  className="dark:border-slate-600 dark:bg-slate-800 dark:text-slate-100 dark:hover:bg-slate-700 dark:hover:text-white"
                                  onClick={(event) => {
                                    event.stopPropagation()
                                    if (disabled) return
                                    setSelectedId(candidate.id)
                                    testCandidate(candidate)
                                  }}
                                  disabled={disabled || testState.status === 'testing'}
                                >
                                  {disabled ? (
                                    'Install first'
                                  ) : testState.status === 'testing' ? (
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
                  </section>
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
              if (onAdvancedSetup) {
                onAdvancedSetup()
                return
              }
              onClose()
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
    </ModalPortal>
  )
}
