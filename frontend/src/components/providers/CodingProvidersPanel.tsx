import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Check,
  CheckCircle2,
  ChevronRight,
  CircleAlert,
  Clipboard,
  Code2,
  Loader2,
  RefreshCw,
  ShieldCheck,
  Terminal,
  X,
} from 'lucide-react'
import ModalPortal from '../ui/ModalPortal'
import {
  llmConfigService,
  type DynamicModelEntry,
  type ProviderDefaultTierModels,
  type ProviderManifestEntry,
} from '../../services/llm-config-api'
import { CODING_PROVIDER_GUIDES } from './codingProviderGuides'
import GuidedProviderTerminal from './GuidedProviderTerminal'
import { useAuthStore } from '../../stores/useAuthStore'
import type { ProviderSetupAction, ProviderSetupSession } from '../../services/llm-config-api'

interface CodingProvidersPanelProps {
  isOpen: boolean
  onClose: () => void
}

type ProviderStatus = 'ready' | 'auth' | 'missing' | 'deprecated'

const providerStatus = (provider: ProviderManifestEntry): ProviderStatus => {
  if (provider.deprecated) return 'deprecated'
  if (provider.usable) return 'ready'
  if (provider.runtime_available === false) return 'missing'
  return 'auth'
}

const STATUS_STYLES: Record<ProviderStatus, { label: string; className: string }> = {
  ready: {
    label: 'Ready',
    className: 'bg-emerald-50 text-emerald-700 ring-emerald-200 dark:bg-emerald-500/10 dark:text-emerald-300 dark:ring-emerald-500/30',
  },
  auth: {
    label: 'Needs authentication',
    className: 'bg-amber-50 text-amber-700 ring-amber-200 dark:bg-amber-500/10 dark:text-amber-300 dark:ring-amber-500/30',
  },
  missing: {
    label: 'Not installed',
    className: 'bg-gray-100 text-gray-600 ring-gray-200 dark:bg-gray-800 dark:text-gray-300 dark:ring-gray-700',
  },
  deprecated: {
    label: 'Deprecated',
    className: 'bg-red-50 text-red-700 ring-red-200 dark:bg-red-500/10 dark:text-red-300 dark:ring-red-500/30',
  },
}

const TIER_ORDER: Array<keyof ProviderDefaultTierModels> = ['builder', 'high', 'medium', 'low', 'maintenance', 'pulse']
const GUIDED_SETUP_PROVIDERS = new Set(['claude-code', 'codex-cli', 'cursor-cli', 'muse-cli'])

function StatusBadge({ provider }: { provider: ProviderManifestEntry }) {
  const status = STATUS_STYLES[providerStatus(provider)]
  return (
    <span className={`inline-flex shrink-0 items-center rounded-full px-2 py-0.5 text-[11px] font-medium ring-1 ring-inset ${status.className}`}>
      {status.label}
    </span>
  )
}

function ProviderListStatus({ provider }: { provider: ProviderManifestEntry }) {
  const status = providerStatus(provider)
  const label = STATUS_STYLES[status].label
  const color = status === 'ready'
    ? 'text-emerald-500 dark:text-emerald-400'
    : status === 'auth'
      ? 'text-amber-500 dark:text-amber-400'
      : status === 'deprecated'
        ? 'text-red-500 dark:text-red-400'
        : 'text-gray-400 dark:text-gray-500'
  return (
    <span
      className={`inline-flex shrink-0 items-center ${color}`}
      title={label}
      aria-label={status === 'ready' ? `${provider.display_name} is ready` : `${provider.display_name}: ${label}`}
    >
      {status === 'ready' ? <CheckCircle2 className="h-4 w-4" /> : <CircleAlert className="h-4 w-4" />}
    </span>
  )
}

function SetupStep({
  number,
  title,
  complete,
  children,
}: {
  number: number
  title: string
  complete?: boolean
  children: React.ReactNode
}) {
  return (
    <section className="relative grid grid-cols-[2rem_minmax(0,1fr)] gap-3">
      <div className={`flex h-8 w-8 items-center justify-center rounded-full text-xs font-semibold ring-1 ring-inset ${
        complete
          ? 'bg-emerald-50 text-emerald-700 ring-emerald-200 dark:bg-emerald-500/10 dark:text-emerald-300 dark:ring-emerald-500/30'
          : 'bg-gray-100 text-gray-600 ring-gray-200 dark:bg-gray-800 dark:text-gray-300 dark:ring-gray-700'
      }`}>
        {complete ? <Check className="h-4 w-4" /> : number}
      </div>
      <div className="min-w-0 pb-6">
        <h3 className="mb-2 text-sm font-semibold text-gray-900 dark:text-gray-100">{title}</h3>
        {children}
      </div>
    </section>
  )
}

function TierSummary({ tiers }: { tiers?: ProviderDefaultTierModels }) {
  if (!tiers) return null
  const entries = TIER_ORDER.flatMap(tier => {
    const model = tiers[tier]
    return model ? [{ tier, model }] : []
  })
  if (entries.length === 0) return null

  return (
    <div className="mt-5">
      <div className="mb-2 text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Default usage tiers</div>
      <div className="grid gap-2 sm:grid-cols-2">
        {entries.map(({ tier, model }) => (
          <div key={tier} className="rounded-lg border border-gray-200 bg-gray-50 px-3 py-2 dark:border-gray-700 dark:bg-gray-800/60">
            <div className="text-[10px] font-semibold uppercase tracking-wide text-gray-400">{tier}</div>
            <div className="mt-0.5 truncate text-xs font-medium text-gray-700 dark:text-gray-200" title={model.model_id}>
              {model.model_id}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

const tierModels = (provider: ProviderManifestEntry): DynamicModelEntry[] => {
  const tiers = provider.default_tier_models
  if (!tiers) return []
  return TIER_ORDER.flatMap(tier => {
    const model = tiers[tier]
    return model ? [{ model_id: model.model_id, model_name: model.model_id }] : []
  })
}

const manifestModels = (provider: ProviderManifestEntry): DynamicModelEntry[] =>
  provider.models.map(model => ({
    model_id: model.model_id,
    model_name: model.model_name || model.model_id,
    is_default: model.model_id === provider.default_model_id,
    context_window: model.context_window,
  }))

const uniqueModels = (models: DynamicModelEntry[], defaultModelId: string): DynamicModelEntry[] => {
  const seen = new Set<string>()
  const unique: DynamicModelEntry[] = []
  for (const model of models) {
    const id = model.model_id.trim()
    if (!id || seen.has(id)) continue
    seen.add(id)
    unique.push({ ...model, model_id: id, model_name: model.model_name || id })
  }
  return unique.sort((left, right) => {
    const leftDefault = left.is_default || left.model_id === defaultModelId
    const rightDefault = right.is_default || right.model_id === defaultModelId
    if (leftDefault !== rightDefault) return leftDefault ? -1 : 1
    return left.model_name.localeCompare(right.model_name)
  })
}

function ProviderModelCatalog({ provider }: { provider: ProviderManifestEntry }) {
  const fallbackModels = useMemo(() => uniqueModels([
    ...manifestModels(provider),
    ...tierModels(provider),
  ], provider.default_model_id), [provider])
  const [models, setModels] = useState<DynamicModelEntry[]>(fallbackModels)
  const [loading, setLoading] = useState(false)
  const [expanded, setExpanded] = useState(false)

  useEffect(() => {
    let cancelled = false
    setModels(fallbackModels)
    setExpanded(false)

    if (provider.id === 'pi-cli') return () => { cancelled = true }
    setLoading(true)
    void llmConfigService.getProviderModels(provider.id)
      .then(response => {
        if (!cancelled) {
          setModels(uniqueModels([...response.models, ...fallbackModels], provider.default_model_id))
        }
      })
      .catch(() => {
        // Keep the manifest fallback when live discovery is unavailable.
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => { cancelled = true }
  }, [fallbackModels, provider.default_model_id, provider.id])

  if (provider.id === 'pi-cli') return null

  const visibleModels = expanded ? models : models.slice(0, 6)
  return (
    <div className="mt-5" data-testid="provider-model-catalog">
      <div className="mb-2 flex items-center justify-between gap-3">
        <div className="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Available models</div>
        <div className="text-[11px] text-gray-400">
          {loading ? 'Checking…' : `${models.length} model${models.length === 1 ? '' : 's'}`}
        </div>
      </div>
      {models.length === 0 && !loading ? (
        <p className="text-xs text-gray-500 dark:text-gray-400">This provider did not report a model catalog.</p>
      ) : (
        <div className="overflow-hidden rounded-lg border border-gray-200 dark:border-gray-700">
          {visibleModels.map(model => {
            const isDefault = model.is_default || model.model_id === provider.default_model_id
            return (
              <div key={model.model_id} className="flex min-h-9 items-center justify-between gap-3 border-b border-gray-200 px-3 py-1.5 last:border-b-0 dark:border-gray-700">
                <div className="min-w-0">
                  <div className="truncate text-xs font-medium text-gray-800 dark:text-gray-200" title={model.model_name}>{model.model_name}</div>
                  {model.model_name !== model.model_id && (
                    <div className="truncate font-mono text-[10px] text-gray-400" title={model.model_id}>{model.model_id}</div>
                  )}
                </div>
                {isDefault && (
                  <span className="shrink-0 rounded bg-violet-50 px-1.5 py-0.5 text-[10px] font-medium text-violet-700 dark:bg-violet-500/10 dark:text-violet-300">Default</span>
                )}
              </div>
            )
          })}
          {models.length > 6 && (
            <button
              type="button"
              onClick={() => setExpanded(value => !value)}
              className="w-full border-t border-gray-200 px-3 py-2 text-left text-xs font-medium text-violet-600 hover:bg-gray-50 dark:border-gray-700 dark:text-violet-300 dark:hover:bg-gray-800/60"
            >
              {expanded ? 'Show fewer models' : `Show ${models.length - 6} more models`}
            </button>
          )}
        </div>
      )}
    </div>
  )
}

export default function CodingProvidersPanel({ isOpen, onClose }: CodingProvidersPanelProps) {
  const [providers, setProviders] = useState<ProviderManifestEntry[]>([])
  const [providerOrder, setProviderOrder] = useState<string[]>([])
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [guidedSession, setGuidedSession] = useState<ProviderSetupSession | null>(null)
  const [guidedStarting, setGuidedStarting] = useState<ProviderSetupAction | null>(null)
  const [guidedError, setGuidedError] = useState<string | null>(null)
  const isMultiUserMode = useAuthStore(state => state.isMultiUserMode)
  const isAdmin = useAuthStore(state => state.user?.is_admin === true)
  const canRunGuidedSetup = !isMultiUserMode || isAdmin

  const refresh = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const manifest = await llmConfigService.getProviderManifest()
      const order = new Map(manifest.provider_order.map((id, index) => [id, index]))
      const codingAgents = manifest.providers
        .filter(provider => provider.integration_kind === 'coding_agent')
        .sort((a, b) => (order.get(a.id) ?? 999) - (order.get(b.id) ?? 999))
      setProviders(codingAgents)
      setProviderOrder(manifest.provider_order)
      setSelectedId(current => current && codingAgents.some(provider => provider.id === current)
        ? current
        : codingAgents[0]?.id ?? null)
    } catch (refreshError) {
      setError(refreshError instanceof Error ? refreshError.message : 'Could not load provider status')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (!isOpen) return
    void refresh()
  }, [isOpen, refresh])

  useEffect(() => {
    if (!isOpen) return
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        if (guidedSession?.status === 'running') {
          void llmConfigService.cancelProviderSetup(guidedSession.id).catch(() => undefined)
        }
        setGuidedSession(null)
        onClose()
      }
    }
    window.addEventListener('keydown', closeOnEscape)
    return () => {
      document.body.style.overflow = previousOverflow
      window.removeEventListener('keydown', closeOnEscape)
    }
  }, [guidedSession, isOpen, onClose])

  const orderedProviders = useMemo(() => {
    const order = new Map(providerOrder.map((id, index) => [id, index]))
    return [...providers].sort((a, b) => (order.get(a.id) ?? 999) - (order.get(b.id) ?? 999))
  }, [providerOrder, providers])

  const selectedProvider = orderedProviders.find(provider => provider.id === selectedId) ?? orderedProviders[0]
  const guide = selectedProvider ? CODING_PROVIDER_GUIDES[selectedProvider.id] : undefined

  const startGuidedSetup = async (action: ProviderSetupAction) => {
    if (!selectedProvider || !GUIDED_SETUP_PROVIDERS.has(selectedProvider.id)) return
    setGuidedStarting(action)
    setGuidedError(null)
    try {
      const session = await llmConfigService.startProviderSetup(selectedProvider.id, action, 100, 24)
      setGuidedSession(session)
    } catch (setupError) {
      const responseMessage = (setupError as { response?: { data?: { error?: string } } })?.response?.data?.error
      setGuidedError(responseMessage || (setupError instanceof Error ? setupError.message : 'Could not start guided setup'))
    } finally {
      setGuidedStarting(null)
    }
  }

  const closePanel = () => {
    if (guidedSession?.status === 'running') {
      void llmConfigService.cancelProviderSetup(guidedSession.id).catch(() => undefined)
    }
    setGuidedSession(null)
    onClose()
  }

  if (!isOpen) return null

  return (
    <ModalPortal>
      <div
        className="fixed inset-0 z-[1000] flex items-center justify-center bg-gray-950/55 p-2 backdrop-blur-sm sm:p-5"
        onMouseDown={event => {
          if (event.target === event.currentTarget) closePanel()
        }}
      >
        <div
          role="dialog"
          aria-modal="true"
          aria-labelledby="providers-title"
          className="flex h-[min(860px,calc(100vh-1rem))] w-full max-w-6xl flex-col overflow-hidden rounded-2xl border border-gray-200 bg-white shadow-2xl dark:border-gray-700 dark:bg-gray-900 sm:h-[min(860px,calc(100vh-2.5rem))]"
        >
          <header className="flex shrink-0 items-center justify-between gap-4 border-b border-gray-200 px-4 py-3 dark:border-gray-700 sm:px-6 sm:py-4">
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <Code2 className="h-5 w-5 text-violet-600 dark:text-violet-400" />
                <h2 id="providers-title" className="text-base font-semibold text-gray-950 dark:text-white sm:text-lg">
                  Providers
                </h2>
              </div>
              <p className="mt-0.5 truncate text-xs text-gray-500 dark:text-gray-400 sm:text-sm">
                Coding agents are installed by AgentWorks. Connect them once, then use them in any workflow.
              </p>
            </div>
            <div className="flex shrink-0 items-center gap-1">
              <button
                type="button"
                onClick={refresh}
                disabled={loading}
                aria-label="Refresh provider status"
                className="rounded-lg p-2 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-800 disabled:opacity-50 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-gray-100"
              >
                <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
              </button>
              <button
                type="button"
                onClick={closePanel}
                aria-label="Close providers"
                className="rounded-lg p-2 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-800 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-gray-100"
              >
                <X className="h-4 w-4" />
              </button>
            </div>
          </header>

          {error && (
            <div className="mx-4 mt-4 flex items-start gap-2 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-300 sm:mx-6">
              <CircleAlert className="mt-0.5 h-4 w-4 shrink-0" />
              <span className="flex-1">{error}</span>
              <button type="button" onClick={refresh} className="font-medium underline underline-offset-2">Retry</button>
            </div>
          )}

          <div className="grid min-h-0 flex-1 grid-cols-1 md:grid-cols-[14rem_minmax(0,1fr)]">
            <aside className="border-b border-gray-200 bg-gray-50/80 p-2 dark:border-gray-700 dark:bg-gray-950/30 md:overflow-y-auto md:border-b-0 md:border-r md:p-3">
              <div className="mb-1 hidden px-2 text-[10px] font-semibold uppercase tracking-wider text-gray-400 md:block">Available providers</div>
              {loading && providers.length === 0 ? (
                <div className="flex items-center gap-2 px-2 py-4 text-sm text-gray-500">
                  <Loader2 className="h-4 w-4 animate-spin" /> Checking the server…
                </div>
              ) : (
                <div className="flex gap-1.5 overflow-x-auto md:flex-col md:overflow-x-visible">
                  {orderedProviders.map(provider => (
                    <button
                      type="button"
                      key={provider.id}
                      onClick={() => setSelectedId(provider.id)}
                      className={`min-w-[12rem] rounded-lg border px-2.5 py-2 text-left transition-colors md:min-w-0 ${
                        selectedProvider?.id === provider.id
                          ? 'border-violet-300 bg-white shadow-sm dark:border-violet-500/50 dark:bg-gray-800'
                          : 'border-transparent hover:border-gray-200 hover:bg-white dark:hover:border-gray-700 dark:hover:bg-gray-800/70'
                      }`}
                    >
                      <div className="flex min-h-6 items-center gap-2">
                        <span className="min-w-0 flex-1 truncate text-sm font-medium text-gray-900 dark:text-gray-100">{provider.display_name}</span>
                        <ProviderListStatus provider={provider} />
                        <ChevronRight className={`h-3.5 w-3.5 shrink-0 text-gray-400 ${selectedProvider?.id === provider.id ? 'text-violet-500' : ''}`} />
                      </div>
                    </button>
                  ))}
                </div>
              )}
            </aside>

            <main className="min-h-0 overflow-y-auto px-4 py-5 sm:px-7 sm:py-6">
              {!loading && orderedProviders.length === 0 && !error && (
                <div className="flex h-full items-center justify-center text-sm text-gray-500">No coding providers are available.</div>
              )}

              {selectedProvider && guide && (
                <div className="mx-auto max-w-3xl">
                  <div className="mb-6 flex flex-wrap items-start justify-between gap-3">
                    <div>
                      <div className="flex flex-wrap items-center gap-2">
                        <h2 className="text-xl font-semibold text-gray-950 dark:text-white">{selectedProvider.display_name}</h2>
                        <StatusBadge provider={selectedProvider} />
                      </div>
                      <p className="mt-1 max-w-2xl text-sm leading-6 text-gray-600 dark:text-gray-300">{selectedProvider.description}</p>
                    </div>
                  </div>

                  {selectedProvider.deprecated && selectedProvider.deprecation_reason && (
                    <div className="mb-5 rounded-xl border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-300">
                      {selectedProvider.deprecation_reason}
                    </div>
                  )}

                  {guidedSession && guidedSession.provider === selectedProvider.id && (
                    <GuidedProviderTerminal
                      session={guidedSession}
                      onFinished={finishedSession => {
                        setGuidedSession(finishedSession)
                        void refresh()
                      }}
                      onClose={() => setGuidedSession(null)}
                    />
                  )}

                  {guidedError && (
                    <div className="mb-5 flex items-start gap-2 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-300">
                      <CircleAlert className="mt-0.5 h-4 w-4 shrink-0" /> {guidedError}
                    </div>
                  )}

                  <SetupStep number={1} title="CLI availability" complete={selectedProvider.runtime_available === true}>
                    {selectedProvider.runtime_available === true ? (
                      <p className="flex items-center gap-1.5 text-sm text-emerald-700 dark:text-emerald-300">
                        <CheckCircle2 className="h-4 w-4" /> Installed and maintained by AgentWorks
                      </p>
                    ) : (
                      <p className="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-800 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-200">
                        This CLI is missing from the AgentWorks installation. A platform administrator must repair or update the deployment.
                      </p>
                    )}
                  </SetupStep>

                  <SetupStep number={2} title="Authenticate" complete={selectedProvider.auth_configured}>
                    <p className="text-sm leading-6 text-gray-600 dark:text-gray-300">{guide.authenticateNote}</p>
                    {GUIDED_SETUP_PROVIDERS.has(selectedProvider.id) && selectedProvider.runtime_available === true && (
                      canRunGuidedSetup ? (
                        <button
                          type="button"
                          onClick={() => void startGuidedSetup('authenticate')}
                          disabled={guidedStarting !== null || guidedSession?.status === 'running'}
                          className="mt-3 inline-flex items-center gap-2 rounded-lg bg-violet-600 px-3 py-2 text-sm font-medium text-white transition-colors hover:bg-violet-700 disabled:cursor-not-allowed disabled:opacity-50"
                        >
                          {guidedStarting === 'authenticate' ? <Loader2 className="h-4 w-4 animate-spin" /> : <Terminal className="h-4 w-4" />}
                          {selectedProvider.auth_configured ? 'Sign in again' : 'Start sign-in'}
                        </button>
                      ) : (
                        <p className="mt-2 text-xs text-gray-500 dark:text-gray-400">An administrator must authenticate providers on this server.</p>
                      )
                    )}
                    {!selectedProvider.auth_configured && !GUIDED_SETUP_PROVIDERS.has(selectedProvider.id) && (
                      <p className="mt-2 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-200">
                        Guided authentication for this provider is not available yet. A platform administrator must configure its credentials.
                      </p>
                    )}
                    {selectedProvider.auth_configured && (
                      <p className="mt-2 flex items-center gap-1.5 text-xs text-emerald-700 dark:text-emerald-300">
                        <ShieldCheck className="h-3.5 w-3.5" /> Authentication detected{selectedProvider.auth_source ? ` via ${selectedProvider.auth_source}` : ''}
                      </p>
                    )}
                  </SetupStep>

                  <SetupStep number={3} title="Verify the installation" complete={selectedProvider.usable}>
                    <p className="text-sm leading-6 text-gray-600 dark:text-gray-300">
                      AgentWorks checks the installation and authentication status directly. This does not make a model call or consume provider credits.
                    </p>
                    <button
                      type="button"
                      onClick={() => void refresh()}
                      disabled={loading}
                      className="mt-3 inline-flex items-center gap-2 rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm font-medium text-gray-700 transition-colors hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-50 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-200 dark:hover:bg-gray-700"
                    >
                      <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
                      {loading ? 'Checking status…' : 'Check status'}
                    </button>
                    {selectedProvider.setup_hint && !selectedProvider.usable && (
                      <p className="mt-2 flex items-start gap-1.5 text-xs text-amber-700 dark:text-amber-300">
                        <CircleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" /> {selectedProvider.setup_hint}
                      </p>
                    )}
                  </SetupStep>

                  <SetupStep number={4} title="Use in a workflow">
                    <div className="rounded-xl border border-violet-200 bg-violet-50/70 p-4 dark:border-violet-500/30 dark:bg-violet-500/10">
                      <div className="flex items-start gap-3">
                        <Clipboard className="mt-0.5 h-4 w-4 shrink-0 text-violet-600 dark:text-violet-300" />
                        <div>
                          <p className="text-sm font-medium text-violet-950 dark:text-violet-100">Provider access stays separate from workflow choice</p>
                          <p className="mt-1 text-sm leading-6 text-violet-800/80 dark:text-violet-200/80">
                            Open a workflow, go to Setup → Workflow LLM configuration, choose Change provider, then select Use in this workflow.
                          </p>
                        </div>
                      </div>
                    </div>
                  </SetupStep>

                  <div className="border-t border-gray-200 pt-5 dark:border-gray-700">
                    <div className="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Capabilities</div>
                    <div className="mt-2 flex flex-wrap gap-2">
                      {selectedProvider.capabilities.map(capability => (
                        <span key={capability} className="rounded-md bg-gray-100 px-2 py-1 text-xs text-gray-600 dark:bg-gray-800 dark:text-gray-300">
                          {capability.replaceAll('_', ' ')}
                        </span>
                      ))}
                      {selectedProvider.supports_dynamic_models && (
                        <span className="rounded-md bg-gray-100 px-2 py-1 text-xs text-gray-600 dark:bg-gray-800 dark:text-gray-300">dynamic models</span>
                      )}
                    </div>
                    <TierSummary tiers={selectedProvider.default_tier_models} />
                    <ProviderModelCatalog provider={selectedProvider} />
                  </div>
                </div>
              )}
            </main>
          </div>
        </div>
      </div>
    </ModalPortal>
  )
}
