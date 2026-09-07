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

type GmailNotificationsBots = Pick<WorkflowBots,
  | 'readOnly'
  | 'gmailOpen' | 'setGmailOpen' | 'gmailConfig' | 'setGmailConfig' | 'gmailBlockedText' | 'setGmailBlockedText'
  | 'gmailLoading' | 'gmailChecking' | 'gmailSaving' | 'gmailTesting' | 'gmailError' | 'gmailSuccess' | 'gmailTestResult'
  | 'gmailBlockedDefaults' | 'gmailDefaultIsBlocked' | 'gmailCanEnable' | 'gmailHasChanges' | 'loadGmail' | 'saveGmail' | 'testGmail'
  | 'gmailConnections' | 'gmailConnectionsBusy' | 'gmailAuthPending'
  | 'gmailNewConnectionName' | 'setGmailNewConnectionName'
  | 'gmailNewConnectionDir' | 'setGmailNewConnectionDir'
  | 'runGmailConnectionAction' | 'connectGmailAccount'
  | 'gmailOAuthClients' | 'gmailOAuthClientsBusy' | 'gmailOAuthClientError'
  | 'gmailNewClientName' | 'setGmailNewClientName'
  | 'gmailSelectedClientName' | 'setGmailSelectedClientName'
  | 'createGmailOAuthClient' | 'deleteGmailOAuthClient'
>

export function GmailNotifications({ bots }: { bots: GmailNotificationsBots }) {
  const {
    readOnly,
    gmailOpen, setGmailOpen, gmailConfig, setGmailConfig, gmailBlockedText, setGmailBlockedText,
    gmailLoading, gmailChecking, gmailSaving, gmailTesting, gmailError, gmailSuccess, gmailTestResult,
    gmailBlockedDefaults, gmailDefaultIsBlocked, gmailCanEnable, gmailHasChanges, loadGmail, saveGmail, testGmail,
    gmailConnections, gmailConnectionsBusy, gmailAuthPending,
    gmailNewConnectionName, setGmailNewConnectionName,
    gmailNewConnectionDir, setGmailNewConnectionDir,
    runGmailConnectionAction, connectGmailAccount,
    gmailOAuthClients, gmailOAuthClientsBusy, gmailOAuthClientError,
    gmailNewClientName, setGmailNewClientName,
    gmailSelectedClientName, setGmailSelectedClientName,
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

  // Match the backend's slug pattern (lowercase letters/digits/hyphens).
  const slugifyClientName = (value: string) =>
    value.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 63)

  // The downloaded JSON already names the Google Cloud project — prefill the
  // name field from it instead of asking the user to type one, so picking
  // the file is normally the only step. Never overrides a name they already
  // typed, and silently leaves the field blank on a file that doesn't parse
  // (handleAddOAuthClient reports that error on submit instead).
  const handleClientFileChange = async (file: File | null) => {
    setNewClientFile(file)
    setNewClientParseError(null)
    if (!file || gmailNewClientName.trim()) return
    try {
      const parsed = JSON.parse(await file.text()) as Record<string, unknown>
      const entry = (parsed.installed ?? parsed.web) as Record<string, unknown> | undefined
      const projectId = typeof entry?.project_id === 'string' ? entry.project_id : undefined
      if (projectId) setGmailNewClientName(slugifyClientName(projectId))
    } catch {
      // Leave the name blank — the submit handler below surfaces the parse error.
    }
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
    const ok = await createGmailOAuthClient(gmailNewClientName.trim(), parsed)
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
                    The Google Cloud app each account signs in through. Give it a name — a second upload never
                    replaces an existing one by accident; it needs its own name.
                  </p>
                </div>
                {gmailOAuthClientError && <StatusBanner tone="error">{gmailOAuthClientError}</StatusBanner>}
                {newClientParseError && <StatusBanner tone="error">{newClientParseError}</StatusBanner>}
                {gmailOAuthClients.length > 0 && (
                  <ul className="space-y-1">
                    {gmailOAuthClients.map(client => (
                      <li key={client.name} className="flex items-center gap-2 rounded border border-border px-2 py-1.5 text-xs">
                        <span className="font-medium">{client.name}</span>
                        {client.client_id && <span className="truncate font-mono text-muted-foreground">{client.client_id}</span>}
                        <button
                          onClick={() => handleRemoveOAuthClient(client.name)}
                          disabled={readOnly || gmailOAuthClientsBusy}
                          title={readOnly ? READ_ONLY_TITLE : undefined}
                          className="ml-auto rounded border border-border px-2 py-0.5 text-red-600 hover:bg-red-50 disabled:opacity-40 dark:text-red-400 dark:hover:bg-red-900/20"
                        >
                          Remove
                        </button>
                      </li>
                    ))}
                  </ul>
                )}
                <div className="flex flex-wrap items-center gap-2">
                  <input
                    type="text"
                    value={gmailNewClientName}
                    onChange={event => setGmailNewClientName(event.target.value)}
                    disabled={readOnly}
                    placeholder="Client name (e.g. primary)"
                    className="flex-1 rounded-md border border-border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-primary"
                  />
                  <input
                    type="file"
                    accept="application/json"
                    disabled={readOnly}
                    onChange={event => void handleClientFileChange(event.target.files?.[0] || null)}
                    className="flex-1 text-xs text-muted-foreground file:mr-2 file:rounded file:border file:border-border file:bg-muted/40 file:px-2 file:py-1 file:text-xs"
                  />
                  <Button
                    variant="outline"
                    disabled={readOnly || !gmailNewClientName.trim() || !newClientFile || gmailOAuthClientsBusy}
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
                      </li>
                    ))}
                  </ul>
                )}

                <div className="space-y-2 border-t border-border pt-3">
                  {gmailOAuthClients.length === 0 ? (
                    <p className="text-xs text-amber-600 dark:text-amber-400">
                      Add an OAuth client above first — every account needs one to sign in through.
                    </p>
                  ) : (
                    <select
                      value={gmailSelectedClientName}
                      onChange={event => setGmailSelectedClientName(event.target.value)}
                      disabled={readOnly}
                      className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-primary"
                    >
                      <option value="" disabled>Choose an OAuth client…</option>
                      {gmailOAuthClients.map(client => (
                        <option key={client.name} value={client.name}>{client.name}</option>
                      ))}
                    </select>
                  )}
                  <div className="flex gap-2">
                    <input
                      type="text"
                      value={gmailNewConnectionName}
                      onChange={event => setGmailNewConnectionName(event.target.value)}
                      disabled={readOnly}
                      placeholder="Account name (e.g. Work)"
                      className="flex-1 rounded-md border border-border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-primary"
                    />
                    <Button
                      variant="outline"
                      disabled={readOnly || !gmailNewConnectionName.trim() || !gmailSelectedClientName || gmailConnectionsBusy !== null}
                      title={readOnly ? READ_ONLY_TITLE : undefined}
                      onClick={() => runGmailConnectionAction('new', async () => {
                        await agentApi.createGmailConnection({
                          display_name: gmailNewConnectionName.trim(),
                          client_name: gmailSelectedClientName,
                          config_home: gmailNewConnectionDir.trim() || undefined,
                        })
                        setGmailNewConnectionName('')
                        setGmailNewConnectionDir('')
                      })}
                    >
                      Add account
                    </Button>
                  </div>
                  <input
                    type="text"
                    value={gmailNewConnectionDir}
                    onChange={event => setGmailNewConnectionDir(event.target.value)}
                    disabled={readOnly}
                    placeholder="Existing gws config directory (optional)"
                    className="w-full rounded-md border border-border bg-background px-3 py-2 font-mono text-xs focus:outline-none focus:ring-2 focus:ring-primary"
                  />
                  <p className="text-xs text-muted-foreground">
                    Add the account, then click <strong>Sign in with Google</strong> on its row to authorize it in your browser.
                    The directory field is only needed to adopt a <code>gws</code> profile already authenticated on the host.
                  </p>
                </div>
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
