import { useCallback, useEffect, useState } from 'react'
import {
  AlertCircle,
  AlertTriangle,
  BellRing,
  CheckCircle,
  Loader2,
  Mail,
  RotateCcw,
  SlidersHorizontal,
  X,
} from 'lucide-react'
import { agentApi } from '../../services/api'
import type {
  GmailConfigRequest,
  GmailConfigResponse,
  GmailTestResponse,
  NotificationPreference,
} from '../../services/api-types'
import { Button } from '../ui/Button'
import { Card } from '../ui/Card'
import ModalPortal from '../ui/ModalPortal'

interface NotificationPreferencesModalProps {
  isOpen: boolean
  onClose: () => void
}

type Section = 'gmail' | 'routing'

const emptyPreference: NotificationPreference = {
  slack_channel_id: '',
  slack_disabled: false,
  whatsapp_phone: '',
  whatsapp_disabled: false,
}

const emptyGmailConfig: GmailConfigResponse = {
  enabled: false,
  default_to: '',
  auth: { gws_installed: false, authenticated: false, has_gmail_scope: false },
  ready: false,
}

const normalizeEmails = (values: string | string[] | undefined): string[] => {
  const source = Array.isArray(values) ? values : [values || '']
  const seen = new Set<string>()
  const result: string[] = []
  for (const raw of source) {
    for (const part of String(raw).split(/[\s,;]+/)) {
      const email = part.trim().toLowerCase()
      if (!email || seen.has(email)) continue
      seen.add(email)
      result.push(email)
    }
  }
  return result
}

export default function NotificationPreferencesModal({
  isOpen,
  onClose,
}: NotificationPreferencesModalProps) {
  const [activeSection, setActiveSection] = useState<Section>('gmail')
  const [preference, setPreference] = useState<NotificationPreference>(emptyPreference)
  const [routingLoading, setRoutingLoading] = useState(false)
  const [routingSaving, setRoutingSaving] = useState(false)
  const [routingError, setRoutingError] = useState<string | null>(null)
  const [routingSuccess, setRoutingSuccess] = useState<string | null>(null)

  const [gmailConfig, setGmailConfig] = useState<GmailConfigResponse>(emptyGmailConfig)
  const [gmailOriginal, setGmailOriginal] = useState({ enabled: false, default_to: '', blocked_recipients: [] as string[] })
  const [gmailBlockedText, setGmailBlockedText] = useState('')
  const [gmailLoading, setGmailLoading] = useState(false)
  const [gmailChecking, setGmailChecking] = useState(false)
  const [gmailSaving, setGmailSaving] = useState(false)
  const [gmailTesting, setGmailTesting] = useState(false)
  const [gmailError, setGmailError] = useState<string | null>(null)
  const [gmailSuccess, setGmailSuccess] = useState<string | null>(null)
  const [gmailTestResult, setGmailTestResult] = useState<GmailTestResponse | null>(null)
  const [gmailTestedTo, setGmailTestedTo] = useState<string | null>(null)

  const loadRouting = useCallback(async () => {
    try {
      setRoutingLoading(true)
      setRoutingError(null)
      const data = await agentApi.getNotificationPreferences()
      setPreference({ ...emptyPreference, ...data })
    } catch (error) {
      setRoutingError(error instanceof Error ? error.message : 'Failed to load notification routing')
    } finally {
      setRoutingLoading(false)
    }
  }, [])

  const loadGmail = useCallback(async (background = false) => {
    try {
      if (background) setGmailChecking(true)
      else setGmailLoading(true)
      setGmailError(null)
      const data = await agentApi.getGmailFeedbackConfig()
      if (background) {
        setGmailConfig(current => ({ ...current, auth: data.auth, ready: data.ready }))
      } else {
        const blocked = normalizeEmails(data.blocked_recipients)
        setGmailConfig({ ...data, blocked_recipients: blocked })
        setGmailBlockedText(blocked.join(', '))
        setGmailOriginal({ enabled: data.enabled, default_to: data.default_to || '', blocked_recipients: blocked })
      }
    } catch (error) {
      setGmailError(error instanceof Error ? error.message : 'Failed to load Gmail configuration')
    } finally {
      setGmailLoading(false)
      setGmailChecking(false)
    }
  }, [])

  useEffect(() => {
    if (!isOpen) return
    setActiveSection('gmail')
    setRoutingSuccess(null)
    setGmailSuccess(null)
    void loadRouting()
    void loadGmail()
  }, [isOpen, loadRouting, loadGmail])

  const saveRouting = async () => {
    try {
      setRoutingSaving(true)
      setRoutingError(null)
      await agentApi.updateNotificationPreferences(preference)
      setRoutingSuccess('Notification routing saved.')
    } catch (error) {
      setRoutingError(error instanceof Error ? error.message : 'Failed to save notification routing')
    } finally {
      setRoutingSaving(false)
    }
  }

  const currentBlocked = normalizeEmails(gmailBlockedText)
  const defaultRecipient = (gmailConfig.default_to || '').trim().toLowerCase()
  const defaultIsBlocked = defaultRecipient !== '' && currentBlocked.includes(defaultRecipient)
  const gmailTestPassed = gmailTestResult?.success === true
    && gmailTestedTo === (gmailConfig.default_to || '')
    && !defaultIsBlocked
  const gmailHasChanges = gmailConfig.enabled !== gmailOriginal.enabled
    || (gmailConfig.default_to || '') !== gmailOriginal.default_to
    || JSON.stringify(currentBlocked) !== JSON.stringify(gmailOriginal.blocked_recipients)

  const saveGmail = async () => {
    try {
      setGmailSaving(true)
      setGmailError(null)
      setGmailSuccess(null)
      const request: GmailConfigRequest = {
        enabled: gmailConfig.enabled,
        default_to: gmailConfig.default_to || '',
        blocked_recipients: currentBlocked,
      }
      const data = await agentApi.updateGmailFeedbackConfig(request)
      const blocked = normalizeEmails(data.blocked_recipients)
      setGmailConfig({ ...data, blocked_recipients: blocked })
      setGmailBlockedText(blocked.join(', '))
      setGmailOriginal({ enabled: data.enabled, default_to: data.default_to || '', blocked_recipients: blocked })
      setGmailSuccess('Gmail notification settings saved.')
    } catch (error) {
      setGmailError(error instanceof Error ? error.message : 'Failed to save Gmail configuration')
    } finally {
      setGmailSaving(false)
    }
  }

  const testGmail = async () => {
    try {
      setGmailTesting(true)
      setGmailError(null)
      setGmailTestResult(null)
      const request: GmailConfigRequest = {
        enabled: gmailConfig.enabled,
        default_to: gmailConfig.default_to || '',
        blocked_recipients: currentBlocked,
      }
      const result = await agentApi.testGmailConnection(request)
      setGmailTestResult(result)
      setGmailTestedTo(result.success ? (gmailConfig.default_to || '') : null)
    } catch (error) {
      setGmailTestResult({ success: false, message: error instanceof Error ? error.message : 'Test failed' })
      setGmailTestedTo(null)
    } finally {
      setGmailTesting(false)
    }
  }

  if (!isOpen) return null

  return (
    <ModalPortal>
      <div className="fixed inset-0 z-[9999] flex items-center justify-center bg-black/50 p-2 backdrop-blur-sm sm:p-4">
        <div className="flex max-h-[calc(100dvh-1rem)] w-full max-w-4xl flex-col overflow-hidden rounded-lg border border-border bg-background shadow-xl sm:max-h-[90vh]">
          <div className="flex flex-shrink-0 items-center justify-between border-b border-border px-4 py-3">
            <h2 className="flex items-center gap-2 text-base font-semibold">
              <BellRing className="h-5 w-5 text-primary" />
              Notifications
            </h2>
            <button onClick={onClose} className="rounded p-1.5 text-muted-foreground transition-colors hover:text-foreground" aria-label="Close notifications">
              <X className="h-4 w-4" />
            </button>
          </div>

          <div className="flex min-h-0 flex-1 overflow-hidden">
            <div className="flex w-48 flex-shrink-0 flex-col border-r border-border">
              <div className="border-b border-border px-3 py-2.5 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                One-way delivery
              </div>
              <div className="flex-1 space-y-1 overflow-y-auto py-1">
                <button onClick={() => setActiveSection('gmail')} className={`flex w-full items-center gap-2.5 px-3 py-2 text-sm transition-colors ${activeSection === 'gmail' ? 'bg-accent font-medium text-accent-foreground' : 'text-muted-foreground hover:bg-muted hover:text-foreground'}`}>
                  <Mail className="h-4 w-4" />
                  <span className="flex-1 text-left">Gmail</span>
                  {gmailConfig.ready && <span className="h-1.5 w-1.5 rounded-full bg-green-500" />}
                </button>
                <button onClick={() => setActiveSection('routing')} className={`flex w-full items-center gap-2.5 px-3 py-2 text-sm transition-colors ${activeSection === 'routing' ? 'bg-accent font-medium text-accent-foreground' : 'text-muted-foreground hover:bg-muted hover:text-foreground'}`}>
                  <SlidersHorizontal className="h-4 w-4" />
                  <span className="flex-1 text-left">My routing</span>
                </button>
              </div>
            </div>

            <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
              {activeSection === 'gmail' && (
                <>
                  <div className="flex-1 space-y-4 overflow-y-auto p-4">
                    {gmailLoading ? (
                      <div className="flex justify-center py-12"><Loader2 className="h-7 w-7 animate-spin text-primary" /></div>
                    ) : (
                      <>
                        <div>
                          <h3 className="text-sm font-semibold">Gmail notifications</h3>
                          <p className="mt-1 text-xs text-muted-foreground">Account-level one-way email delivery. Email replies do not resume an agent.</p>
                        </div>
                        {gmailError && <Card className="border-red-300 bg-red-50 p-3 text-sm text-red-700 dark:border-red-700 dark:bg-red-900/20 dark:text-red-300"><AlertCircle className="mr-2 inline h-4 w-4" />{gmailError}</Card>}
                        {gmailSuccess && <Card className="border-green-300 bg-green-50 p-3 text-sm text-green-700 dark:border-green-700 dark:bg-green-900/20 dark:text-green-300"><CheckCircle className="mr-2 inline h-4 w-4" />{gmailSuccess}</Card>}
                        <Card className="p-4">
                          <div className="flex items-center justify-between gap-3">
                            <div>
                              <h4 className="text-sm font-medium">Enable Gmail</h4>
                              <p className="mt-0.5 text-xs text-muted-foreground">Available to notify_user across workflows and Chief of Staff.</p>
                            </div>
                            <label className={`relative inline-flex items-center ${!gmailConfig.enabled && !gmailTestPassed ? 'cursor-not-allowed' : 'cursor-pointer'}`}>
                              <input type="checkbox" checked={gmailConfig.enabled} disabled={!gmailConfig.enabled && !gmailTestPassed} onChange={event => setGmailConfig({ ...gmailConfig, enabled: event.target.checked })} className="peer sr-only" />
                              <div className="h-6 w-11 rounded-full bg-gray-200 after:absolute after:left-[2px] after:top-[2px] after:h-5 after:w-5 after:rounded-full after:border after:border-gray-300 after:bg-white after:transition-all after:content-[''] peer-checked:bg-blue-600 peer-checked:after:translate-x-full peer-checked:after:border-white peer-disabled:opacity-40 dark:bg-gray-700" />
                            </label>
                          </div>
                          {!gmailConfig.enabled && !gmailTestPassed && <p className="mt-2 text-xs text-amber-600 dark:text-amber-400">Send a successful test email before enabling.</p>}
                        </Card>
                        <Card className="p-4">
                          <div className="flex items-center justify-between gap-2">
                            <div>
                              <h4 className="text-sm font-medium">Connection</h4>
                              <p className="mt-0.5 text-xs text-muted-foreground">Google Workspace CLI on the server host.</p>
                            </div>
                            <div className="flex items-center gap-2 text-xs">
                              {gmailChecking ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <span className={`h-2 w-2 rounded-full ${gmailConfig.auth.authenticated && gmailConfig.auth.has_gmail_scope ? 'bg-green-500' : 'bg-amber-500'}`} />}
                              <span>{!gmailConfig.auth.gws_installed ? 'gws not installed' : !gmailConfig.auth.authenticated ? 'Not connected' : !gmailConfig.auth.has_gmail_scope ? 'Missing Gmail scope' : 'Connected'}</span>
                              <button onClick={() => loadGmail(true)} disabled={gmailChecking} className="rounded p-1 text-muted-foreground hover:text-foreground" aria-label="Refresh Gmail connection"><RotateCcw className="h-3.5 w-3.5" /></button>
                            </div>
                          </div>
                        </Card>
                        {!(gmailConfig.auth.authenticated && gmailConfig.auth.has_gmail_scope) && (
                          <Card className="border-amber-300 bg-amber-50 p-4 text-xs text-amber-900 dark:border-amber-700 dark:bg-amber-900/20 dark:text-amber-100">
                            <div className="flex gap-2"><AlertTriangle className="h-4 w-4 flex-shrink-0" /><div><strong>Setup on the server host:</strong> install <code>@googleworkspace/cli</code>, then run <code>gws auth login -s gmail</code> and refresh this status.</div></div>
                          </Card>
                        )}
                        <Card className="space-y-3 p-4">
                          <div>
                            <label className="mb-2 block text-sm font-medium">Default recipient</label>
                            <input type="email" value={gmailConfig.default_to || ''} onChange={event => setGmailConfig({ ...gmailConfig, default_to: event.target.value })} placeholder="you@example.com" className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-primary" />
                          </div>
                          <div>
                            <label className="mb-2 block text-sm font-medium">Disallowed recipients</label>
                            <textarea value={gmailBlockedText} onChange={event => setGmailBlockedText(event.target.value)} rows={3} placeholder="blocked@example.com, no-notify@example.com" className="w-full resize-y rounded-md border border-border bg-background px-3 py-2 font-mono text-sm focus:outline-none focus:ring-2 focus:ring-primary" />
                            {defaultIsBlocked && <p className="mt-1 text-xs text-red-600 dark:text-red-400">The default recipient is in the disallowed list.</p>}
                          </div>
                        </Card>
                        <Button variant="outline" onClick={testGmail} disabled={gmailTesting || !gmailConfig.default_to || defaultIsBlocked} className="w-full">{gmailTesting ? <><Loader2 className="mr-2 h-4 w-4 animate-spin" />Sending…</> : 'Send test email'}</Button>
                        {gmailTestResult && <Card className={`p-3 text-sm ${gmailTestResult.success ? 'border-green-300 bg-green-50 text-green-700 dark:border-green-700 dark:bg-green-900/20 dark:text-green-300' : 'border-red-300 bg-red-50 text-red-700 dark:border-red-700 dark:bg-red-900/20 dark:text-red-300'}`}>{gmailTestResult.message}</Card>}
                      </>
                    )}
                  </div>
                  <div className="flex flex-shrink-0 justify-end gap-2 border-t border-border px-4 py-3">
                    <Button variant="ghost" onClick={onClose}>Cancel</Button>
                    <Button onClick={saveGmail} disabled={!gmailHasChanges || gmailSaving || gmailLoading || defaultIsBlocked || (gmailConfig.enabled && !gmailTestPassed)}>{gmailSaving ? <><Loader2 className="mr-2 h-4 w-4 animate-spin" />Saving…</> : 'Save'}</Button>
                  </div>
                </>
              )}

              {activeSection === 'routing' && (
                <>
                  <div className="flex-1 space-y-4 overflow-y-auto p-4">
                    <div>
                      <h3 className="text-sm font-semibold">My notification routing</h3>
                      <p className="mt-1 text-xs text-muted-foreground">Optional personal destinations for the interactive Slack and WhatsApp connectors. A bot must be connected before these routes work.</p>
                    </div>
                    {routingLoading ? <div className="flex justify-center py-12"><Loader2 className="h-7 w-7 animate-spin" /></div> : (
                      <>
                        {routingError && <Card className="border-red-300 bg-red-50 p-3 text-sm text-red-700 dark:border-red-700 dark:bg-red-900/20 dark:text-red-300">{routingError}</Card>}
                        {routingSuccess && <Card className="border-green-300 bg-green-50 p-3 text-sm text-green-700 dark:border-green-700 dark:bg-green-900/20 dark:text-green-300">{routingSuccess}</Card>}
                        <Card className="space-y-3 p-4">
                          <div className="flex items-center justify-between"><h4 className="text-sm font-medium">Slack bot channel</h4><label className="flex items-center gap-2 text-xs text-muted-foreground"><input type="checkbox" checked={!!preference.slack_disabled} onChange={event => setPreference({ ...preference, slack_disabled: event.target.checked })} />Disable</label></div>
                          <input value={preference.slack_channel_id || ''} disabled={!!preference.slack_disabled} onChange={event => setPreference({ ...preference, slack_channel_id: event.target.value.trim() })} placeholder="C0123456789 — blank uses workspace default" className="w-full rounded-md border border-border bg-secondary px-3 py-2 font-mono text-sm disabled:opacity-50" />
                        </Card>
                        <Card className="space-y-3 p-4">
                          <div className="flex items-center justify-between"><h4 className="text-sm font-medium">WhatsApp number</h4><label className="flex items-center gap-2 text-xs text-muted-foreground"><input type="checkbox" checked={!!preference.whatsapp_disabled} onChange={event => setPreference({ ...preference, whatsapp_disabled: event.target.checked })} />Disable</label></div>
                          <input value={preference.whatsapp_phone || ''} disabled={!!preference.whatsapp_disabled} onChange={event => setPreference({ ...preference, whatsapp_phone: event.target.value.trim() })} placeholder="+919000000000 — blank uses workspace default" className="w-full rounded-md border border-border bg-secondary px-3 py-2 font-mono text-sm disabled:opacity-50" />
                        </Card>
                        <Card className="border-blue-300 bg-blue-50 p-3 text-xs text-blue-800 dark:border-blue-700 dark:bg-blue-900/20 dark:text-blue-200">notify_user uses these for one-way FYIs. Short-lived human_feedback may also alert through interactive connectors, but the workflow Slack webhook is deliberately excluded because it cannot accept an answer.</Card>
                      </>
                    )}
                  </div>
                  <div className="flex flex-shrink-0 justify-end gap-2 border-t border-border px-4 py-3"><Button variant="ghost" onClick={onClose}>Cancel</Button><Button onClick={saveRouting} disabled={routingLoading || routingSaving}>{routingSaving ? <><Loader2 className="mr-2 h-4 w-4 animate-spin" />Saving…</> : 'Save'}</Button></div>
                </>
              )}
            </div>
          </div>
        </div>
      </div>
    </ModalPortal>
  )
}
