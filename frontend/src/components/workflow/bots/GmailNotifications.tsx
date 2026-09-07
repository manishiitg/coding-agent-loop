import { useState } from 'react'
import { AlertTriangle, ChevronRight, Loader2, Mail, RotateCcw } from 'lucide-react'
import { agentApi } from '../../../services/api'
import { Button } from '../../ui/Button'
import { Card } from '../../ui/Card'
import { READ_ONLY_TITLE } from '../../../hooks/useCanWriteWorkflow'
import type { WorkflowBots } from './useWorkflowBots'
import { StatusBanner } from './StatusBanner'
import { GmailSetupGuide } from './GmailSetupGuide'

// ── Email notifications (account-wide, shared by every workflow) ──────────

// Shortens a full OAuth scope URL to its last path segment for display (e.g.
// "https://www.googleapis.com/auth/gmail.send" -> "gmail.send"); shows the
// full string on hover via the caller's title attribute.
function formatGmailScope(scope: string): string {
  const parts = scope.split('/')
  return parts[parts.length - 1] || scope
}

type GmailNotificationsBots = Pick<WorkflowBots,
  | 'readOnly'
  | 'gmailOpen' | 'setGmailOpen' | 'gmailConfig' | 'setGmailConfig' | 'gmailBlockedText' | 'setGmailBlockedText'
  | 'gmailLoading' | 'gmailChecking' | 'gmailSaving' | 'gmailTesting' | 'gmailError' | 'gmailSuccess' | 'gmailTestResult'
  | 'gmailBlockedDefaults' | 'gmailDefaultIsBlocked' | 'gmailCanEnable' | 'gmailHasChanges' | 'loadGmail' | 'saveGmail' | 'testGmail'
  | 'gmailConnections' | 'gmailConnectionsBusy' | 'gmailAuthPending' | 'gmailAuthUrl'
  | 'runGmailConnectionAction' | 'connectGmailAccount'
  | 'gmailOAuthClients' | 'gmailOAuthClientsBusy' | 'gmailOAuthClientError'
  | 'gmailNewClientEmail' | 'setGmailNewClientEmail'
  | 'createGmailOAuthClient' | 'deleteGmailOAuthClient'
>

// Shown while a sign-in is in flight, so the link can be pasted into a
// different Chrome profile than the one the auto-opened tab landed in —
// copying the address bar out of that tab does not work (see gmailAuthUrl's
// comment in useWorkflowBots.ts for why).
function SignInLinkBox({ url }: { url: string }) {
  const [copyState, setCopyState] = useState<'idle' | 'copied' | 'failed'>('idle')

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(url)
      setCopyState('copied')
    } catch {
      setCopyState('failed')
    }
    window.setTimeout(() => setCopyState('idle'), 2000)
  }

  return (
    <div className="mt-2 rounded-md border border-border bg-muted/40 p-2">
      <p className="text-xs text-muted-foreground">
        A tab opened in your default Chrome profile. If this mailbox lives in a different profile,
        open that profile and paste this link there — copying the address bar out of the tab that
        opened will not work, since Google ties it to the profile it started in.
      </p>
      <div className="mt-1.5 flex items-center gap-2">
        <code className="flex-1 truncate rounded bg-background px-2 py-1 font-mono text-[11px] text-muted-foreground">
          {url}
        </code>
        <button
          onClick={handleCopy}
          className="shrink-0 rounded border border-border px-2 py-1 text-xs text-muted-foreground hover:text-foreground"
        >
          {copyState === 'copied' ? 'Copied!' : copyState === 'failed' ? 'Copy failed' : 'Copy link'}
        </button>
      </div>
    </div>
  )
}

export function GmailNotifications({ bots }: { bots: GmailNotificationsBots }) {
  const {
    readOnly,
    gmailOpen, setGmailOpen, gmailConfig, setGmailConfig, gmailBlockedText, setGmailBlockedText,
    gmailLoading, gmailChecking, gmailSaving, gmailTesting, gmailError, gmailSuccess, gmailTestResult,
    gmailBlockedDefaults, gmailDefaultIsBlocked, gmailCanEnable, gmailHasChanges, loadGmail, saveGmail, testGmail,
    gmailConnections, gmailConnectionsBusy, gmailAuthPending, gmailAuthUrl,
    runGmailConnectionAction, connectGmailAccount,
    gmailOAuthClients, gmailOAuthClientsBusy, gmailOAuthClientError,
    gmailNewClientEmail, setGmailNewClientEmail,
    createGmailOAuthClient, deleteGmailOAuthClient,
  } = bots

  const handleRemoveOAuthClient = (name: string) => {
    const inUse = gmailConnections.filter(conn => conn.client_name === name)
    const warning = inUse.length > 0
      ? `${inUse.length} sending account${inUse.length === 1 ? '' : 's'} (${inUse.map(c => c.display_name).join(', ')}) use this client and will need reconnecting afterward. `
      : ''
    if (!window.confirm(`${warning}Remove the OAuth client "${name}"?`)) return
    void deleteGmailOAuthClient(name)
  }

  const [newClientFile, setNewClientFile] = useState<File | null>(null)
  const [newClientParseError, setNewClientParseError] = useState<string | null>(null)

  // A second (or third...) mailbox reusing an already-registered client
  // (the documented pattern: grant each mailbox IAM access to the same
  // project) needs its own small entry point, scoped to that client's row,
  // now that adding the client itself also creates its first account.
  const [addMailboxFor, setAddMailboxFor] = useState<string | null>(null)
  const [addMailboxEmail, setAddMailboxEmail] = useState('')

  const handleAddMailbox = async (clientName: string) => {
    const email = addMailboxEmail.trim()
    if (!email) return
    await runGmailConnectionAction('new', () =>
      agentApi.createGmailConnection({ display_name: email, client_name: clientName }))
    setAddMailboxFor(null)
    setAddMailboxEmail('')
  }

  const handleAddOAuthClient = async () => {
    if (!newClientFile) return
    setNewClientParseError(null)
    let parsed: unknown
    try {
      parsed = JSON.parse(await newClientFile.text())
    } catch {
      setNewClientParseError('That file is not valid JSON — download the client_secret.json Google Cloud gave you and upload it unmodified.')
      return
    }
    // createGmailOAuthClient derives the internal client name from this
    // email — asking for the mailbox directly is what an operator actually
    // thinks in terms of, not an arbitrary label for the Google Cloud app.
    const ok = await createGmailOAuthClient(gmailNewClientEmail.trim(), parsed)
    if (ok) setNewClientFile(null)
  }

  return (
    <div className="rounded-md border border-border">
      <button
        type="button"
        onClick={() => setGmailOpen(open => !open)}
        aria-expanded={gmailOpen}
        className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm font-medium text-foreground transition-colors hover:bg-muted/50"
      >
        <ChevronRight className={`h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform ${gmailOpen ? 'rotate-90' : ''}`} />
        <Mail className="h-4 w-4 shrink-0 text-muted-foreground" />
        Email notifications
        <span className="font-normal text-muted-foreground">— shared by all workflows</span>
        <span className="flex-1" />
        <span className={`inline-flex items-center gap-1 text-[11px] font-medium ${gmailConfig.enabled ? 'text-emerald-600 dark:text-emerald-400' : 'text-muted-foreground'}`}>
          <span className={`h-1.5 w-1.5 rounded-full ${gmailConfig.enabled ? 'bg-emerald-500' : 'bg-muted-foreground/40'}`} />
          {gmailConfig.enabled ? 'On' : 'Off'}
        </span>
      </button>
      {gmailOpen && (
        <div className="space-y-4 border-t border-border p-3">
          {gmailLoading ? (
            <div className="flex items-center justify-center py-12"><Loader2 className="w-8 h-8 animate-spin text-primary" /></div>
          ) : (
            <>
              <p className="text-xs text-muted-foreground">Account-wide one-way email delivery, shared by <code>notify_user</code> across every workflow and product chat. Turn this off to stop all outbound email. Email replies do not resume an agent.</p>
              {gmailError && <StatusBanner tone="error">{gmailError}</StatusBanner>}
              {gmailSuccess && <StatusBanner tone="success">{gmailSuccess}</StatusBanner>}
              <Card className="p-4">
                <div className="flex items-center justify-between gap-3">
                  <div>
                    <h4 className="text-sm font-medium">Enable Gmail</h4>
                    <p className="mt-0.5 text-xs text-muted-foreground">Available to notify_user across workflows and product chats.</p>
                  </div>
                  <label className={`relative inline-flex items-center ${!gmailConfig.enabled && !gmailCanEnable ? 'cursor-not-allowed' : 'cursor-pointer'}`}>
                    <input type="checkbox" checked={gmailConfig.enabled} disabled={readOnly || (!gmailConfig.enabled && !gmailCanEnable)} onChange={event => setGmailConfig({ ...gmailConfig, enabled: event.target.checked })} className="peer sr-only" />
                    <div className="h-6 w-11 rounded-full bg-gray-200 after:absolute after:left-[2px] after:top-[2px] after:h-5 after:w-5 after:rounded-full after:border after:border-gray-300 after:bg-white after:transition-all after:content-[''] peer-checked:bg-blue-600 peer-checked:after:translate-x-full peer-checked:after:border-white peer-disabled:opacity-40 dark:bg-gray-700" />
                  </label>
                </div>
                {!gmailConfig.enabled && !gmailCanEnable && <p className="mt-2 text-xs text-amber-600 dark:text-amber-400">Sign in a Gmail account below to enable; it switches on automatically once one is connected.</p>}
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
              {!(gmailConfig.auth.authenticated && gmailConfig.auth.has_gmail_scope) && gmailConnections.length === 0 && (
                <Card className="border-amber-300 bg-amber-50 p-4 text-xs text-amber-900 dark:border-amber-700 dark:bg-amber-900/20 dark:text-amber-100">
                  <div className="flex gap-2"><AlertTriangle className="h-4 w-4 flex-shrink-0" /><div><strong>No account connected yet.</strong> Add a sending account below and sign in with Google. <code>@googleworkspace/cli</code> must be installed on the server host.</div></div>
                </Card>
              )}

              <GmailSetupGuide />

              <Card className="space-y-3 p-4">
                <div>
                  <h4 className="text-sm font-medium">OAuth clients</h4>
                  <p className="mt-0.5 text-xs text-muted-foreground">
                    The Google Cloud app each account signs in through. Say which mailbox it's for, upload the
                    client file, and that mailbox appears below as a sending account, ready to sign in — a
                    second upload never replaces an existing client by accident. Adding another <em>mailbox</em>{' '}
                    that reuses this same client is done from its row below, not here.
                  </p>
                </div>
                {gmailOAuthClientError && <StatusBanner tone="error">{gmailOAuthClientError}</StatusBanner>}
                {newClientParseError && <StatusBanner tone="error">{newClientParseError}</StatusBanner>}
                {gmailOAuthClients.length > 0 && (
                  <ul className="space-y-1">
                    {gmailOAuthClients.map(client => (
                      <li key={client.name} className="rounded border border-border px-2 py-1.5 text-xs">
                        <div className="flex items-center gap-2">
                          <span className="font-medium">{client.name}</span>
                          {client.client_id && <span className="truncate font-mono text-muted-foreground">{client.client_id}</span>}
                          <button
                            onClick={() => setAddMailboxFor(current => (current === client.name ? null : client.name))}
                            disabled={readOnly}
                            title={readOnly ? READ_ONLY_TITLE : undefined}
                            className="ml-auto rounded border border-border px-2 py-0.5 text-muted-foreground hover:text-foreground disabled:opacity-40"
                          >
                            + Add mailbox
                          </button>
                          <button
                            onClick={() => handleRemoveOAuthClient(client.name)}
                            disabled={readOnly || gmailOAuthClientsBusy}
                            title={readOnly ? READ_ONLY_TITLE : undefined}
                            className="rounded border border-border px-2 py-0.5 text-red-600 hover:bg-red-50 disabled:opacity-40 dark:text-red-400 dark:hover:bg-red-900/20"
                          >
                            Remove
                          </button>
                        </div>
                        {addMailboxFor === client.name && (
                          <div className="mt-1.5 flex items-center gap-2">
                            <input
                              type="email"
                              autoFocus
                              value={addMailboxEmail}
                              onChange={event => setAddMailboxEmail(event.target.value)}
                              disabled={readOnly}
                              placeholder="another-mailbox@example.com"
                              className="flex-1 rounded border border-border bg-background px-2 py-1 text-xs focus:outline-none focus:ring-2 focus:ring-primary"
                            />
                            <Button
                              variant="outline"
                              disabled={readOnly || !addMailboxEmail.trim() || gmailConnectionsBusy !== null}
                              title={readOnly ? READ_ONLY_TITLE : undefined}
                              onClick={() => handleAddMailbox(client.name)}
                            >
                              Add
                            </Button>
                          </div>
                        )}
                      </li>
                    ))}
                  </ul>
                )}
                <div className="flex flex-wrap items-center gap-2">
                  <input
                    type="email"
                    value={gmailNewClientEmail}
                    onChange={event => setGmailNewClientEmail(event.target.value)}
                    disabled={readOnly}
                    placeholder="Mailbox this client is for (e.g. you@example.com)"
                    className="flex-1 rounded-md border border-border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-primary"
                  />
                  <input
                    type="file"
                    accept="application/json"
                    disabled={readOnly}
                    onChange={event => setNewClientFile(event.target.files?.[0] || null)}
                    className="flex-1 text-xs text-muted-foreground file:mr-2 file:rounded file:border file:border-border file:bg-muted/40 file:px-2 file:py-1 file:text-xs"
                  />
                  <Button
                    variant="outline"
                    disabled={readOnly || !gmailNewClientEmail.trim() || !newClientFile || gmailOAuthClientsBusy}
                    title={readOnly ? READ_ONLY_TITLE : undefined}
                    onClick={handleAddOAuthClient}
                  >
                    {gmailOAuthClientsBusy ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Add client'}
                  </Button>
                </div>
              </Card>

              <Card className="space-y-3 p-4">
                <div>
                  <h4 className="text-sm font-medium">Sending accounts</h4>
                  <p className="mt-0.5 text-xs text-muted-foreground">
                    Which mailbox notifications are sent from. A workflow may pick one; otherwise the default is used.
                  </p>
                </div>

                {gmailConnections.length === 0 ? (
                  <p className="text-xs text-muted-foreground">
                    No sending accounts yet — mail goes out from whichever account <code>gws</code> is authenticated as on the host.
                  </p>
                ) : (
                  <ul className="space-y-2">
                    {gmailConnections.map(conn => (
                      <li key={conn.id} className="rounded-md border border-border p-3">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className={`h-2 w-2 flex-shrink-0 rounded-full ${conn.ready ? 'bg-green-500' : conn.auth?.checking ? 'bg-muted-foreground' : 'bg-amber-500'}`} />
                          <span className="text-sm font-medium">{conn.display_name}</span>
                          {conn.is_default && <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-muted-foreground">Default</span>}
                          {!conn.enabled && <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-muted-foreground">Disabled</span>}
                          <span className="ml-auto text-xs text-muted-foreground">
                            {conn.auth?.checking ? 'Checking…' : conn.ready ? 'Connected' : 'Not connected'}
                          </span>
                        </div>
                        <p className="mt-1 font-mono text-xs text-muted-foreground">
                          {conn.email || 'Address not known yet'}
                          {conn.client_name && <span className="ml-2 text-muted-foreground/70">via {conn.client_name}</span>}
                        </p>
                        {conn.auth?.scopes && conn.auth.scopes.length > 0 && (
                          <p className="mt-1 flex flex-wrap gap-1">
                            {conn.auth.scopes.map(scope => (
                              <span key={scope} className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground" title={scope}>
                                {formatGmailScope(scope)}
                              </span>
                            ))}
                          </p>
                        )}

                        <div className="mt-2 flex flex-wrap gap-2">
                          <button
                            onClick={() => connectGmailAccount(conn.id)}
                            disabled={readOnly || gmailAuthPending !== null}
                            title={readOnly ? READ_ONLY_TITLE : undefined}
                            className="rounded border border-primary px-2 py-1 text-xs text-primary hover:bg-primary/10 disabled:opacity-40"
                          >
                            {gmailAuthPending === conn.id ? 'Waiting for Google…' : conn.ready ? 'Reconnect' : 'Sign in with Google'}
                          </button>
                          <button
                            onClick={() => runGmailConnectionAction(conn.id, () => agentApi.setDefaultGmailConnection(conn.id))}
                            disabled={readOnly || conn.is_default || !conn.enabled || gmailConnectionsBusy === conn.id}
                            title={readOnly ? READ_ONLY_TITLE : undefined}
                            className="rounded border border-border px-2 py-1 text-xs text-muted-foreground hover:text-foreground disabled:opacity-40"
                          >
                            Make default
                          </button>
                          <button
                            onClick={() => runGmailConnectionAction(conn.id, () => agentApi.testGmailConnectionById(conn.id, gmailConfig.default_to || undefined))}
                            disabled={readOnly || gmailConnectionsBusy === conn.id}
                            title={readOnly ? READ_ONLY_TITLE : undefined}
                            className="rounded border border-border px-2 py-1 text-xs text-muted-foreground hover:text-foreground disabled:opacity-40"
                          >
                            Send test
                          </button>
                          <button
                            onClick={() => runGmailConnectionAction(conn.id, () => agentApi.updateGmailConnection(conn.id, { enabled: !conn.enabled }))}
                            disabled={readOnly || gmailConnectionsBusy === conn.id}
                            title={readOnly ? READ_ONLY_TITLE : undefined}
                            className="rounded border border-border px-2 py-1 text-xs text-muted-foreground hover:text-foreground disabled:opacity-40"
                          >
                            {conn.enabled ? 'Disable' : 'Enable'}
                          </button>
                          <button
                            onClick={() => runGmailConnectionAction(conn.id, () => agentApi.deleteGmailConnection(conn.id))}
                            disabled={readOnly || gmailConnectionsBusy === conn.id}
                            title={readOnly ? READ_ONLY_TITLE : undefined}
                            className="ml-auto rounded border border-border px-2 py-1 text-xs text-red-600 hover:bg-red-50 disabled:opacity-40 dark:text-red-400 dark:hover:bg-red-900/20"
                          >
                            Remove
                          </button>
                        </div>

                        {gmailAuthPending === conn.id && gmailAuthUrl && (
                          <SignInLinkBox url={gmailAuthUrl} />
                        )}
                      </li>
                    ))}
                  </ul>
                )}

                {gmailOAuthClients.length === 0 && (
                  <p className="border-t border-border pt-3 text-xs text-amber-600 dark:text-amber-400">
                    Add an OAuth client above first — every account needs one to sign in through, and adding one
                    creates its first sending account automatically.
                  </p>
                )}
              </Card>
              <Card className="space-y-3 p-4">
                <div>
                  <label className="mb-2 block text-sm font-medium">Default recipients</label>
                  {/* Deliberately type="text": type="email" rejects a comma-separated
                      list, which is the whole point of this field. */}
                  <input type="text" inputMode="email" value={gmailConfig.default_to || ''} onChange={event => setGmailConfig({ ...gmailConfig, default_to: event.target.value })} disabled={readOnly} placeholder="you@example.com, teammate@example.com" className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-primary" />
                  <p className="mt-1 text-xs text-muted-foreground">Where notifications are emailed when a workflow has no recipients of its own. Separate several addresses with commas.</p>
                </div>
                <div>
                  <label className="mb-2 block text-sm font-medium">Disallowed recipients</label>
                  <textarea value={gmailBlockedText} onChange={event => setGmailBlockedText(event.target.value)} disabled={readOnly} rows={3} placeholder="blocked@example.com, no-notify@example.com" className="w-full resize-y rounded-md border border-border bg-background px-3 py-2 font-mono text-sm focus:outline-none focus:ring-2 focus:ring-primary" />
                  {gmailDefaultIsBlocked && <p className="mt-1 text-xs text-red-600 dark:text-red-400">{gmailBlockedDefaults.join(', ')} {gmailBlockedDefaults.length === 1 ? 'is' : 'are'} both a default recipient and disallowed.</p>}
                </div>
              </Card>
              <Button variant="outline" onClick={testGmail} disabled={readOnly || gmailTesting || !gmailConfig.default_to || gmailDefaultIsBlocked} title={readOnly ? READ_ONLY_TITLE : undefined} className="w-full">{gmailTesting ? <><Loader2 className="mr-2 h-4 w-4 animate-spin" />Sending…</> : 'Send test email'}</Button>
              {gmailTestResult && <Card className={`p-3 text-sm ${gmailTestResult.success ? 'border-green-300 bg-green-50 text-green-700 dark:border-green-700 dark:bg-green-900/20 dark:text-green-300' : 'border-red-300 bg-red-50 text-red-700 dark:border-red-700 dark:bg-red-900/20 dark:text-red-300'}`}>{gmailTestResult.message}</Card>}
              <div className="flex justify-end">
                <Button onClick={saveGmail} disabled={readOnly || !gmailHasChanges || gmailSaving || gmailLoading || gmailDefaultIsBlocked || (gmailConfig.enabled && !gmailCanEnable)} title={readOnly ? READ_ONLY_TITLE : undefined}>{gmailSaving ? <><Loader2 className="mr-2 h-4 w-4 animate-spin" />Saving…</> : 'Save'}</Button>
              </div>
            </>
          )}
        </div>
      )}
    </div>
  )
}
