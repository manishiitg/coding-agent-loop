import { useEffect, useState } from 'react'
import {
  X,
  ShieldCheck,
  AlertTriangle,
  Loader2,
  CheckCircle2,
  ExternalLink,
  Sparkles,
} from 'lucide-react'
import ModalPortal from '../ui/ModalPortal'
import ConnectionIcon from './ConnectionIcon'
import ErrorNotice from './ErrorNotice'
import { useConnectionsStore } from '../../stores/useConnectionsStore'
import type { CatalogEntry, FriendlyError, TestResult } from '../../services/connectionsApi'

type Step = 'review' | 'authenticating' | 'testing' | 'done' | 'error'

interface ConnectFlowModalProps {
  entry: CatalogEntry
  onClose: () => void
}

/**
 * The guided connect flow: review the access being granted, authenticate,
 * verify it works, and land on a success state — without ever showing the user
 * an MCP server or a JSON file.
 */
export default function ConnectFlowModal({ entry, onClose }: ConnectFlowModalProps) {
  const connect = useConnectionsStore(s => s.connect)
  const test = useConnectionsStore(s => s.test)

  const [step, setStep] = useState<Step>('review')
  const [error, setError] = useState<FriendlyError | null>(null)
  const [testResult, setTestResult] = useState<TestResult | null>(null)

  // Credential inputs, only used by auth=token and the needs_client_id fallback.
  const [token, setToken] = useState('')
  const [clientId, setClientId] = useState('')
  const [needsClientId, setNeedsClientId] = useState(false)
  const [extraEnv, setExtraEnv] = useState<Record<string, string>>(
    () => ({ ...(entry.extra_env ?? {}) })
  )

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      // Escape must not abandon a half-finished authentication silently.
      if (e.key === 'Escape' && step !== 'authenticating' && step !== 'testing') {
        onClose()
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [step, onClose])

  const needsToken = entry.auth === 'token'
  const canSubmit = needsToken ? token.trim().length > 0 : !needsClientId || clientId.trim().length > 0

  const handleConnect = async () => {
    setStep('authenticating')
    setError(null)

    const outcome = await connect(entry.id, {
      token: needsToken ? token.trim() : undefined,
      client_id: needsClientId ? clientId.trim() : undefined,
      env: Object.keys(extraEnv).length ? extraEnv : undefined,
    })

    if (outcome.kind === 'needs_client_id') {
      // The provider has no dynamic client registration — collect the id once.
      setNeedsClientId(true)
      setStep('review')
      return
    }

    if (outcome.kind === 'failed') {
      setError(outcome.error)
      setStep('error')
      return
    }

    // Connected — verify it actually works before claiming success.
    setStep('testing')
    const result = await test(entry.id)
    if (result) {
      setTestResult(result)
      setStep('done')
    } else {
      // Auth succeeded but the first call failed; the connection is saved, so
      // this is a warning rather than a failure.
      setStep('done')
    }
  }

  const title =
    step === 'done' ? `${entry.name} connected` : `Connect ${entry.name}`

  return (
    <ModalPortal>
      <div className="fixed inset-0 z-[60] flex items-center justify-center bg-black/50 p-4">
        <div
          role="dialog"
          aria-modal="true"
          aria-label={title}
          className="w-full max-w-lg overflow-hidden rounded-lg bg-white shadow-2xl dark:bg-slate-800"
        >
          {/* Header */}
          <div className="flex items-start justify-between gap-3 border-b border-gray-200 px-5 py-4 dark:border-slate-700">
            <div className="flex min-w-0 items-center gap-3">
              <ConnectionIcon icon={entry.icon} brandColor={entry.brand_color} />
              <div className="min-w-0">
                <h2 className="truncate text-base font-semibold text-gray-900 dark:text-gray-100">
                  {title}
                </h2>
                {entry.tagline && (
                  <p className="truncate text-xs text-gray-500 dark:text-gray-400">
                    {entry.tagline}
                  </p>
                )}
              </div>
            </div>
            <button
              type="button"
              onClick={onClose}
              disabled={step === 'authenticating' || step === 'testing'}
              className="rounded-md p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 disabled:opacity-40 dark:text-gray-400 dark:hover:bg-slate-700"
              aria-label="Close"
            >
              <X className="h-4 w-4" />
            </button>
          </div>

          <div className="max-h-[70vh] space-y-4 overflow-y-auto px-5 py-4">
            {step === 'review' && (
              <>
                {entry.description && (
                  <p className="text-sm text-gray-600 dark:text-gray-300">
                    {entry.description}
                  </p>
                )}

                {/* Plain-language access review — issue #185 asks that users see
                    what the agent can do before enabling access. */}
                {entry.capabilities && entry.capabilities.length > 0 && (
                  <section>
                    <h3 className="mb-2 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                      <ShieldCheck className="h-3.5 w-3.5" aria-hidden="true" />
                      What agents will be able to do
                    </h3>
                    <ul className="space-y-1.5">
                      {entry.capabilities.map(cap => (
                        <li
                          key={cap}
                          className="flex items-start gap-2 text-sm text-gray-700 dark:text-gray-300"
                        >
                          <CheckCircle2
                            className="mt-0.5 h-3.5 w-3.5 shrink-0 text-green-600 dark:text-green-400"
                            aria-hidden="true"
                          />
                          {cap}
                        </li>
                      ))}
                    </ul>
                  </section>
                )}

                {entry.sensitive_actions && entry.sensitive_actions.length > 0 && (
                  <section className="rounded-md border border-amber-200 bg-amber-50 p-3 dark:border-amber-900/50 dark:bg-amber-900/20">
                    <h3 className="mb-1.5 flex items-center gap-1.5 text-xs font-semibold text-amber-900 dark:text-amber-200">
                      <AlertTriangle className="h-3.5 w-3.5" aria-hidden="true" />
                      Sensitive access
                    </h3>
                    <ul className="space-y-1">
                      {entry.sensitive_actions.map(action => (
                        <li key={action} className="text-xs text-amber-800 dark:text-amber-300">
                          {action}
                        </li>
                      ))}
                    </ul>
                    <p className="mt-2 text-xs text-amber-700 dark:text-amber-400">
                      Agents ask for confirmation before performing these actions.
                    </p>
                  </section>
                )}

                {needsToken && (
                  <section className="space-y-2">
                    <label
                      htmlFor="connection-token"
                      className="block text-sm font-medium text-gray-700 dark:text-gray-300"
                    >
                      {entry.token_label || 'Access token'}
                    </label>
                    <input
                      id="connection-token"
                      type="password"
                      value={token}
                      onChange={e => setToken(e.target.value)}
                      placeholder={entry.token_placeholder}
                      autoComplete="off"
                      className="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 focus:border-blue-500 focus:ring-2 focus:ring-blue-500 dark:border-slate-600 dark:bg-slate-700 dark:text-gray-100"
                    />
                    {Object.keys(entry.extra_env ?? {}).map(key => (
                      <div key={key}>
                        <label
                          htmlFor={`env-${key}`}
                          className="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400"
                        >
                          {key}
                        </label>
                        <input
                          id={`env-${key}`}
                          type="text"
                          value={extraEnv[key] ?? ''}
                          onChange={e =>
                            setExtraEnv(prev => ({ ...prev, [key]: e.target.value }))
                          }
                          className="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 focus:border-blue-500 focus:ring-2 focus:ring-blue-500 dark:border-slate-600 dark:bg-slate-700 dark:text-gray-100"
                        />
                      </div>
                    ))}
                    {entry.setup_hint && (
                      <p className="text-xs text-gray-500 dark:text-gray-400">
                        {entry.setup_hint}
                      </p>
                    )}
                  </section>
                )}

                {needsClientId && (
                  <section className="space-y-2">
                    <label
                      htmlFor="connection-client-id"
                      className="block text-sm font-medium text-gray-700 dark:text-gray-300"
                    >
                      OAuth client ID
                    </label>
                    <p className="text-xs text-gray-500 dark:text-gray-400">
                      {entry.name} does not register applications automatically. Paste the
                      client ID from your {entry.name} OAuth app.
                    </p>
                    <input
                      id="connection-client-id"
                      type="text"
                      value={clientId}
                      onChange={e => setClientId(e.target.value)}
                      autoComplete="off"
                      className="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 focus:border-blue-500 focus:ring-2 focus:ring-blue-500 dark:border-slate-600 dark:bg-slate-700 dark:text-gray-100"
                    />
                  </section>
                )}

                {entry.docs_url && (
                  <a
                    href={entry.docs_url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="inline-flex items-center gap-1 text-xs text-blue-600 hover:underline dark:text-blue-400"
                  >
                    {entry.name} setup documentation
                    <ExternalLink className="h-3 w-3" aria-hidden="true" />
                  </a>
                )}
              </>
            )}

            {step === 'authenticating' && (
              <div className="flex flex-col items-center gap-3 py-8 text-center">
                <Loader2 className="h-7 w-7 animate-spin text-blue-600 dark:text-blue-400" />
                <p className="text-sm font-medium text-gray-900 dark:text-gray-100">
                  Waiting for you to approve access
                </p>
                <p className="max-w-xs text-xs text-gray-500 dark:text-gray-400">
                  A {entry.name} sign-in window opened. Approve the requested access
                  there — the window closes itself and you land back here.
                </p>
              </div>
            )}

            {step === 'testing' && (
              <div className="flex flex-col items-center gap-3 py-8 text-center">
                <Loader2 className="h-7 w-7 animate-spin text-blue-600 dark:text-blue-400" />
                <p className="text-sm font-medium text-gray-900 dark:text-gray-100">
                  Testing the connection
                </p>
              </div>
            )}

            {step === 'done' && (
              <div className="space-y-4">
                <div className="flex flex-col items-center gap-2 py-4 text-center">
                  <CheckCircle2 className="h-10 w-10 text-green-600 dark:text-green-400" />
                  <p className="text-sm font-medium text-gray-900 dark:text-gray-100">
                    {entry.name} is connected
                  </p>
                  {testResult ? (
                    <p className="text-xs text-gray-500 dark:text-gray-400">
                      {testResult.tool_count} action
                      {testResult.tool_count === 1 ? '' : 's'} are now available to your
                      agents.
                    </p>
                  ) : (
                    <p className="text-xs text-gray-500 dark:text-gray-400">
                      Access was approved. Run a test from the connection card if you want
                      to verify it.
                    </p>
                  )}
                </div>

                {testResult && testResult.tools.length > 0 && (
                  <section className="rounded-md bg-gray-50 p-3 dark:bg-slate-700/50">
                    <h3 className="mb-2 flex items-center gap-1.5 text-xs font-semibold text-gray-700 dark:text-gray-300">
                      <Sparkles className="h-3.5 w-3.5" aria-hidden="true" />
                      Try asking an agent
                    </h3>
                    <p className="text-xs text-gray-600 dark:text-gray-400">
                      &ldquo;
                      {entry.capabilities?.[0]
                        ? entry.capabilities[0].charAt(0).toLowerCase() +
                          entry.capabilities[0].slice(1)
                        : `use ${entry.name}`}
                      &rdquo;
                    </p>
                  </section>
                )}
              </div>
            )}

            {step === 'error' && error && (
              <ErrorNotice
                error={error}
                onAction={action => {
                  if (action === 'retry' || action === 'reconnect') {
                    setStep('review')
                    setError(null)
                  }
                }}
              />
            )}
          </div>

          {/* Footer */}
          <div className="flex items-center justify-end gap-2 border-t border-gray-200 px-5 py-3 dark:border-slate-700">
            {step === 'done' ? (
              <button
                type="button"
                onClick={onClose}
                className="rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-blue-700"
              >
                Done
              </button>
            ) : (
              <>
                <button
                  type="button"
                  onClick={onClose}
                  disabled={step === 'authenticating' || step === 'testing'}
                  className="rounded-md px-4 py-2 text-sm text-gray-600 transition-colors hover:text-gray-900 disabled:opacity-40 dark:text-gray-400 dark:hover:text-gray-200"
                >
                  Cancel
                </button>
                {(step === 'review' || step === 'error') && (
                  <button
                    type="button"
                    onClick={handleConnect}
                    disabled={!canSubmit}
                    className="rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    {step === 'error' ? 'Try again' : `Connect ${entry.name}`}
                  </button>
                )}
              </>
            )}
          </div>
        </div>
      </div>
    </ModalPortal>
  )
}
