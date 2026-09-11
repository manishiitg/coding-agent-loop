import { useEffect, useState } from 'react'
import { AlertTriangle, ChevronRight, Loader2, Mail, RotateCcw } from 'lucide-react'
import { agentApi } from '../../../services/api'
import type { GmailConnection, GoogleServiceGrant } from '../../../services/api-types'
import { Button } from '../../ui/Button'
import { Card } from '../../ui/Card'
import { READ_ONLY_TITLE } from '../../../hooks/useCanWriteWorkflow'
import type { WorkflowBots } from './useWorkflowBots'
import { StatusBanner } from './StatusBanner'
import { GmailSetupGuide } from './GmailSetupGuide'
import { gmailConnectionUsesSharedOAuthClient } from './gmailSharedOAuthClient'

// ── Email notifications (account-wide, shared by every workflow) ──────────

// Maps a raw OAuth scope to what it actually grants, so the connection card
// reads as "Gmail: send + read" instead of an unlabeled "gmail.modify" chip
// nobody can place. Keyed on both the full scope URL and Google's bare OIDC
// literals (some tokens carry "email"/"openid" as-is, others carry the
// equivalent https://www.googleapis.com/auth/userinfo.email long form —
// Google can return either depending on how a given consent was originally
// requested, so both keys map to the same label).
//
// gmail.modify (full read/write/organize) and the legacy default "tasks"
// scope predate this connector's current send-only-by-default design and its
// opt-in Drive/Sheets/Docs/Slides/Calendar catalog (google_services.go) --
// they show up here on connections authorized before that shipped, not
// something the current "+ Add account" flow can request. Reconnecting an
// existing connection re-requests exactly its current allow_read_access +
// services, which drops any such legacy scope the stored config no longer
// asks for.
const GMAIL_SCOPE_LABELS: Record<string, { label: string; detail: string }> = {
  'openid': { label: 'Sign-in', detail: 'OpenID sign-in — identifies which Google account this is' },
  'email': { label: 'Email address', detail: 'Read the account’s email address (identity only, not mailbox content)' },
  'profile': { label: 'Basic profile', detail: 'Read the account’s basic Google profile (name, picture)' },
  'https://www.googleapis.com/auth/userinfo.email': { label: 'Email address', detail: 'Read the account’s email address (identity only, not mailbox content)' },
  'https://www.googleapis.com/auth/userinfo.profile': { label: 'Basic profile', detail: 'Read the account’s basic Google profile (name, picture)' },
  'https://www.googleapis.com/auth/gmail.send': { label: 'Gmail: send', detail: 'Send email as this account. Cannot read or search the mailbox.' },
  'https://www.googleapis.com/auth/gmail.readonly': { label: 'Gmail: read', detail: 'Read and search this mailbox. Cannot send.' },
  'https://www.googleapis.com/auth/gmail.modify': { label: 'Gmail: full (legacy)', detail: 'Full mailbox access — read, send, and organize. Broader than this connector currently requests; carried over from an older connection.' },
  'https://www.googleapis.com/auth/drive.readonly': { label: 'Drive: read', detail: 'Read files in Google Drive. Cannot create or modify them.' },
  'https://www.googleapis.com/auth/drive': { label: 'Drive: read+write', detail: 'Read and modify files in Google Drive.' },
  'https://www.googleapis.com/auth/spreadsheets.readonly': { label: 'Sheets: read', detail: 'Read Google Sheets spreadsheets. Cannot modify them.' },
  'https://www.googleapis.com/auth/spreadsheets': { label: 'Sheets: read+write', detail: 'Read and modify Google Sheets spreadsheets.' },
  'https://www.googleapis.com/auth/documents.readonly': { label: 'Docs: read', detail: 'Read Google Docs documents. Cannot modify them.' },
  'https://www.googleapis.com/auth/documents': { label: 'Docs: read+write', detail: 'Read and modify Google Docs documents.' },
  'https://www.googleapis.com/auth/presentations.readonly': { label: 'Slides: read', detail: 'Read Google Slides presentations. Cannot modify them.' },
  'https://www.googleapis.com/auth/presentations': { label: 'Slides: read+write', detail: 'Read and modify Google Slides presentations.' },
  'https://www.googleapis.com/auth/calendar.readonly': { label: 'Calendar: read', detail: 'Read Google Calendar events. Cannot create or modify them.' },
  'https://www.googleapis.com/auth/calendar': { label: 'Calendar: read+write', detail: 'Read and modify Google Calendar events.' },
  'https://www.googleapis.com/auth/tasks.readonly': { label: 'Tasks: read (legacy)', detail: 'Read Google Tasks. Not requestable from this connector’s current Add account flow; carried over from an older connection.' },
  'https://www.googleapis.com/auth/tasks': { label: 'Tasks: read+write (legacy)', detail: 'Read and modify Google Tasks. Not requestable from this connector’s current Add account flow; carried over from an older connection.' },
}

// Falls back to the old last-path-segment shorthand for any scope this
// account was granted that isn't in the lookup above, so an unrecognized
// scope still shows something rather than disappearing silently.
function formatGmailScope(scope: string): { label: string; title: string } {
  const known = GMAIL_SCOPE_LABELS[scope.trim()]
  if (known) return { label: known.label, title: `${known.detail}\n\n(${scope})` }
  const parts = scope.split('/')
  return { label: parts[parts.length - 1] || scope, title: scope }
}

type GmailCapabilityState = 'none' | 'read' | 'write'

interface GmailCapabilityRow {
  label: string
  state: GmailCapabilityState
  detail: string
}

// One full read+send+organize scope can stand in for a narrower one this
// connector would otherwise request separately (gmail.modify implies both
// gmail.readonly and gmail.send) — checked here so a legacy connection's
// broader grant still shows the capability as present instead of "not
// authorized" just because the exact narrower scope string isn't in the list.
function gmailScopesGrant(scopes: string[], ...anyOf: string[]): boolean {
  return anyOf.some(scope => scopes.includes(scope))
}

// Builds the full permission matrix — every capability this connector can
// request, PLUS any legacy scope this specific account still carries — each
// marked by what's actually granted right now (from conn.auth.scopes, the
// live truth from Google) rather than the stored allow_read_access/services
// config, which can go stale for a connection authorized before that config
// existed (see GMAIL_SCOPE_LABELS' gmail.modify/tasks comment).
function gmailCapabilityMatrix(scopes: string[]): GmailCapabilityRow[] {
  const has = (...anyOf: string[]) => gmailScopesGrant(scopes, ...anyOf)
  const rows: GmailCapabilityRow[] = [
    {
      label: 'Gmail: Send',
      state: has('https://www.googleapis.com/auth/gmail.send', 'https://www.googleapis.com/auth/gmail.modify') ? 'write' : 'none',
      detail: 'Send email as this account.',
    },
    {
      label: 'Gmail: Read',
      state: has('https://www.googleapis.com/auth/gmail.readonly', 'https://www.googleapis.com/auth/gmail.modify') ? 'read' : 'none',
      detail: 'Read and search this mailbox.',
    },
    {
      label: 'Drive',
      state: has('https://www.googleapis.com/auth/drive') ? 'write' : has('https://www.googleapis.com/auth/drive.readonly') ? 'read' : 'none',
      detail: 'Google Drive files.',
    },
    {
      label: 'Sheets',
      state: has('https://www.googleapis.com/auth/spreadsheets') ? 'write' : has('https://www.googleapis.com/auth/spreadsheets.readonly') ? 'read' : 'none',
      detail: 'Google Sheets spreadsheets.',
    },
    {
      label: 'Docs',
      state: has('https://www.googleapis.com/auth/documents') ? 'write' : has('https://www.googleapis.com/auth/documents.readonly') ? 'read' : 'none',
      detail: 'Google Docs documents.',
    },
    {
      label: 'Slides',
      state: has('https://www.googleapis.com/auth/presentations') ? 'write' : has('https://www.googleapis.com/auth/presentations.readonly') ? 'read' : 'none',
      detail: 'Google Slides presentations.',
    },
    {
      label: 'Calendar',
      state: has('https://www.googleapis.com/auth/calendar') ? 'write' : has('https://www.googleapis.com/auth/calendar.readonly') ? 'read' : 'none',
      detail: 'Google Calendar events.',
    },
  ]
  // Tasks isn't offered by the current Add-account flow at all, so it only
  // ever shows up here (green) on a connection that already carries it from
  // before that flow existed — never as a "not authorized" gray row, since
  // there both would be nothing to reconnect toward and nothing to grant it.
  if (has('https://www.googleapis.com/auth/tasks', 'https://www.googleapis.com/auth/tasks.readonly')) {
    rows.push({
      label: 'Tasks (legacy)',
      state: has('https://www.googleapis.com/auth/tasks') ? 'write' : 'read',
      detail: 'Google Tasks — carried over from before this connector had a Drive/Sheets/Docs/Slides/Calendar picker; not requestable from Add account today.',
    })
  }
  return rows
}

const GMAIL_CAPABILITY_STATE_STYLE: Record<GmailCapabilityState, string> = {
  write: 'border-green-600/40 bg-green-500/15 text-green-700 dark:border-green-500/40 dark:bg-green-500/10 dark:text-green-400',
  read: 'border-amber-600/40 bg-amber-500/15 text-amber-700 dark:border-amber-500/40 dark:bg-amber-500/10 dark:text-amber-400',
  none: 'border-border bg-transparent text-muted-foreground/50',
}

const GMAIL_CAPABILITY_STATE_LABEL: Record<GmailCapabilityState, string> = {
  write: 'read + write',
  read: 'read-only',
  none: 'not authorized',
}

// Names the CLI backend a GmailAuthStatus was computed against, so the UI
// never assumes gws — a deployment may be configured for gog instead.
function gmailBackendLabel(backend: string | undefined): { name: string; install: string } {
  if (backend === 'gog') return { name: 'gog', install: 'gogcli' }
  return { name: 'gws', install: '@googleworkspace/cli' }
}

type GmailNotificationsBots = Pick<WorkflowBots,
  | 'readOnly'
  | 'gmailOpen' | 'setGmailOpen' | 'gmailConfig' | 'setGmailConfig' | 'gmailBlockedText' | 'setGmailBlockedText'
  | 'gmailLoading' | 'gmailChecking' | 'gmailSaving' | 'gmailTesting' | 'gmailError' | 'gmailSuccess' | 'gmailTestResult'
  | 'gmailBlockedDefaults' | 'gmailDefaultIsBlocked' | 'gmailCanEnable' | 'gmailHasChanges' | 'loadGmail' | 'saveGmail' | 'testGmail'
  | 'gmailConnections' | 'gmailConnectionsBusy' | 'gmailAuthPending' | 'gmailAuthUrl'
  | 'runGmailConnectionAction' | 'connectGmailAccount'
  | 'gmailOAuthClientsBusy' | 'gmailOAuthClientError'
  | 'gmailNewClientEmail' | 'setGmailNewClientEmail'
  | 'createGmailOAuthClient' | 'removeGmailMailboxAndClient'
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
    gmailOAuthClientsBusy, gmailOAuthClientError,
    gmailNewClientEmail, setGmailNewClientEmail,
    createGmailOAuthClient, removeGmailMailboxAndClient,
  } = bots

  const [sharedClientConfirm, setSharedClientConfirm] = useState<string | null>(null)

  const handleRemoveMailbox = (conn: { id: string; display_name: string; client_name?: string }) => {
    if (!window.confirm(`Remove "${conn.display_name}"? This also removes its OAuth client if no other account uses it.`)) return
    void removeGmailMailboxAndClient(conn.id, conn.client_name || '')
  }

  const [newClientFile, setNewClientFile] = useState<File | null>(null)
  // Off by default: notifications only ever send, so the consent screen asks
  // for gmail.send alone unless the operator deliberately widens it here.
  const [newClientAllowRead, setNewClientAllowRead] = useState(false)
  // Additional Google Workspace services (Drive, Sheets, Docs, Slides,
  // Calendar...) the new mailbox may also be authorized for, beyond Gmail.
  // Keyed by service id; { write: false } means "selected, read-only" —
  // absence means not selected at all. Read-only is the default access
  // level for the same reason Gmail read is opt-in: the safer grant.
  const [newClientServices, setNewClientServices] = useState<Record<string, { write: boolean }>>({})
  const [serviceCatalog, setServiceCatalog] = useState<Record<string, string> | null>(null)

  // Edit-access panel for an EXISTING connection — same shape as the
  // newClient* state above (reused deliberately, same checkbox UI), but this
  // only ever edits one connection at a time and seeds from that
  // connection's current stored values when opened. Saving only changes the
  // STORED request (see gmail_connections.go's UpdateConnection comment) —
  // it never talks to Google, so the panel always ends with a prompt to
  // reconnect rather than claiming the new access is already live.
  const [editingGrantsConnId, setEditingGrantsConnId] = useState<string | null>(null)
  const [editAllowRead, setEditAllowRead] = useState(false)
  const [editServices, setEditServices] = useState<Record<string, { write: boolean }>>({})
  const [editGrantsSavedConnId, setEditGrantsSavedConnId] = useState<string | null>(null)

  const openEditGrants = (conn: GmailConnection) => {
    setEditingGrantsConnId(conn.id)
    setEditGrantsSavedConnId(null)
    setEditAllowRead(conn.allow_read_access ?? false)
    const seeded: Record<string, { write: boolean }> = {}
    for (const grant of conn.services || []) {
      seeded[grant.service] = { write: grant.write ?? false }
    }
    setEditServices(seeded)
  }

  const saveEditGrants = async (conn: GmailConnection) => {
    const services: GoogleServiceGrant[] = Object.entries(editServices).map(([service, grant]) => ({ service, write: grant.write }))
    const ok = await runGmailConnectionAction(conn.id, () => agentApi.updateGmailConnection(conn.id, {
      allow_read_access: editAllowRead,
      services,
      services_set: true,
    }))
    if (ok) {
      setEditingGrantsConnId(null)
      setEditGrantsSavedConnId(conn.id)
    }
  }
  useEffect(() => {
    let cancelled = false
    agentApi.getGoogleServiceCatalog().then(catalog => { if (!cancelled) setServiceCatalog(catalog) }).catch(() => {
      // Best-effort: the "add account" form still works Gmail-only if this fails.
    })
    return () => { cancelled = true }
  }, [])
  const [newClientParseError, setNewClientParseError] = useState<string | null>(null)
  // Collapsed by default once at least one account exists — no reason to
  // keep the upload form permanently on screen once the common case (one
  // mailbox already set up) is done.
  const [showAddClientForm, setShowAddClientForm] = useState(false)

  const handleAddOAuthClient = async (): Promise<boolean> => {
    if (!newClientFile) return false
    setNewClientParseError(null)
    let parsed: unknown
    try {
      parsed = JSON.parse(await newClientFile.text())
    } catch {
      setNewClientParseError('That file is not valid JSON — download the client_secret.json Google Cloud gave you and upload it unmodified.')
      return false
    }
    const services: GoogleServiceGrant[] = Object.entries(newClientServices).map(([service, grant]) => ({ service, write: grant.write }))
    // createGmailOAuthClient derives the internal client name from this
    // email — asking for the mailbox directly is what an operator actually
    // thinks in terms of, not an arbitrary label for the Google Cloud app.
    const ok = await createGmailOAuthClient(gmailNewClientEmail.trim(), parsed, newClientAllowRead, services)
    if (ok) {
      setNewClientFile(null)
      setNewClientAllowRead(false)
      setNewClientServices({})
    }
    return ok
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
                    <p className="mt-0.5 text-xs text-muted-foreground">{gmailBackendLabel(gmailConfig.auth.backend).name} CLI on the server host.</p>
                  </div>
                  <div className="flex items-center gap-2 text-xs">
                    {gmailChecking ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <span className={`h-2 w-2 rounded-full ${gmailConfig.auth.authenticated && gmailConfig.auth.has_gmail_scope ? 'bg-green-500' : 'bg-amber-500'}`} />}
                    <span>{!gmailConfig.auth.gws_installed ? `${gmailBackendLabel(gmailConfig.auth.backend).name} not installed` : !gmailConfig.auth.authenticated ? 'Not connected' : !gmailConfig.auth.has_gmail_scope ? 'Missing Gmail scope' : 'Connected'}</span>
                    <button onClick={() => loadGmail(true)} disabled={gmailChecking} className="rounded p-1 text-muted-foreground hover:text-foreground" aria-label="Refresh Gmail connection"><RotateCcw className="h-3.5 w-3.5" /></button>
                  </div>
                </div>
              </Card>
              {!(gmailConfig.auth.authenticated && gmailConfig.auth.has_gmail_scope) && gmailConnections.length === 0 && (
                <Card className="border-amber-300 bg-amber-50 p-4 text-xs text-amber-900 dark:border-amber-700 dark:bg-amber-900/20 dark:text-amber-100">
                  <div className="flex gap-2"><AlertTriangle className="h-4 w-4 flex-shrink-0" /><div><strong>No account connected yet.</strong> Add a sending account below and sign in with Google. <code>{gmailBackendLabel(gmailConfig.auth.backend).install}</code> must be installed on the server host.</div></div>
                </Card>
              )}

              <GmailSetupGuide />

              <Card className="space-y-3 p-4">
                <div>
                  <h4 className="text-sm font-medium">Sending accounts</h4>
                  <p className="mt-0.5 text-xs text-muted-foreground">
                    Which mailbox notifications are sent from. Upload the Google Cloud client file for a mailbox
                    and it signs in immediately — a second upload for the same mailbox never replaces an existing
                    client by accident. A workflow may pick a specific account; otherwise the default is used.
                  </p>
                </div>
                {gmailOAuthClientError && <StatusBanner tone="error">{gmailOAuthClientError}</StatusBanner>}
                {newClientParseError && <StatusBanner tone="error">{newClientParseError}</StatusBanner>}

                {gmailConnections.length === 0 ? (
                  <p className="text-xs text-muted-foreground">No sending accounts yet — add one below.</p>
                ) : (
                  <ul className="space-y-2">
                    {gmailConnections.map(conn => (
                      <li key={conn.id} className="rounded-md border border-border p-3">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className={`h-2 w-2 flex-shrink-0 rounded-full ${conn.ready ? 'bg-green-500' : conn.auth?.checking ? 'bg-muted-foreground' : 'bg-amber-500'}`} />
                          <span className="text-sm font-medium">{conn.display_name}</span>
                          {conn.is_default && <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-muted-foreground">Default</span>}
                          {!conn.enabled && <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-muted-foreground">Disabled</span>}
                          <span
                            className="rounded bg-muted px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-muted-foreground"
                            title={conn.allow_read_access ? 'Authorized to send and to read/search this mailbox' : 'Authorized to send only — cannot read this mailbox'}
                          >
                            {conn.allow_read_access ? 'Send + read' : 'Send only'}
                          </span>
                          {(conn.services || []).map(grant => (
                            <span
                              key={grant.service}
                              className="rounded bg-muted px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-muted-foreground"
                              title={`Authorized for ${serviceCatalog?.[grant.service] || grant.service} (${grant.write ? 'read + write' : 'read-only'}) — a workflow can use this via the google_workspace_cli tool`}
                            >
                              {serviceCatalog?.[grant.service] || grant.service}{grant.write ? '' : ' (ro)'}
                            </span>
                          ))}
                          <span className="ml-auto text-xs text-muted-foreground">
                            {conn.auth?.checking ? 'Checking…' : conn.ready ? 'Connected' : 'Not connected'}
                          </span>
                        </div>
                        <p className="mt-1 font-mono text-xs text-muted-foreground">
                          {conn.email || 'Address not known yet'}
                          {conn.client_name && <span className="ml-2 text-muted-foreground/70">via {conn.client_name}</span>}
                        </p>
                        {conn.auth?.scopes && conn.auth.scopes.length > 0 && (
                          <div className="mt-1.5">
                            <p className="text-[10px] uppercase tracking-wide text-muted-foreground/70">
                              Currently authorized (green = read+write, amber = read-only, gray = not granted)
                            </p>
                            <p className="mt-1 flex flex-wrap gap-1">
                              {gmailCapabilityMatrix(conn.auth.scopes).map(row => (
                                <span
                                  key={row.label}
                                  className={`rounded border px-1.5 py-0.5 text-[10px] ${GMAIL_CAPABILITY_STATE_STYLE[row.state]}`}
                                  title={`${row.detail} Currently: ${GMAIL_CAPABILITY_STATE_LABEL[row.state]}.`}
                                >
                                  {row.label}
                                </span>
                              ))}
                            </p>
                            <details className="mt-1">
                              <summary className="cursor-pointer text-[10px] text-muted-foreground/70 hover:text-muted-foreground">
                                Raw granted scopes ({conn.auth.scopes.length})
                              </summary>
                              <p className="mt-1 flex flex-wrap gap-1">
                                {conn.auth.scopes.map(scope => {
                                  const formatted = formatGmailScope(scope)
                                  return (
                                    <span key={scope} className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground" title={formatted.title}>
                                      {formatted.label}
                                    </span>
                                  )
                                })}
                              </p>
                            </details>
                          </div>
                        )}

                        <div className="mt-2 flex flex-wrap gap-2">
                          <button
                            onClick={() => {
                              if (gmailConnectionUsesSharedOAuthClient(conn)) {
                                setSharedClientConfirm(conn.id)
                                return
                              }
                              connectGmailAccount(conn.id)
                            }}
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
                            onClick={() => editingGrantsConnId === conn.id ? setEditingGrantsConnId(null) : openEditGrants(conn)}
                            disabled={readOnly || gmailConnectionsBusy === conn.id}
                            title={readOnly ? READ_ONLY_TITLE : 'Change what this connection is authorized for. Saving only updates the request — Google requires a fresh Reconnect afterward for it to take effect.'}
                            className="rounded border border-border px-2 py-1 text-xs text-muted-foreground hover:text-foreground disabled:opacity-40"
                          >
                            {editingGrantsConnId === conn.id ? 'Cancel edit' : 'Edit access'}
                          </button>
                          <button
                            onClick={() => handleRemoveMailbox(conn)}
                            disabled={readOnly || gmailConnectionsBusy === conn.id}
                            title={readOnly ? READ_ONLY_TITLE : undefined}
                            className="ml-auto rounded border border-border px-2 py-1 text-xs text-red-600 hover:bg-red-50 disabled:opacity-40 dark:text-red-400 dark:hover:bg-red-900/20"
                          >
                            Remove
                          </button>
                        </div>

                        {sharedClientConfirm === conn.id && (
                          <div className="mt-2 rounded-md border border-amber-500/40 bg-amber-50 p-2 dark:bg-amber-900/20">
                            <p className="text-xs text-amber-900 dark:text-amber-200">
                              This older account uses the host&rsquo;s shared Google OAuth client, which may also back
                              the <code>gws</code> login in its terminal. Reconnecting updates that shared Google
                              authorization. Existing permissions are preserved, but the authorization is not isolated
                              to this account row.
                            </p>
                            <p className="mt-1 text-xs text-amber-900 dark:text-amber-200">
                              To keep the host login separate, cancel and add the mailbox with its own OAuth client.
                            </p>
                            <div className="mt-1.5 flex gap-2">
                              <button
                                onClick={() => {
                                  setSharedClientConfirm(null)
                                  connectGmailAccount(conn.id)
                                }}
                                className="rounded border border-amber-600 px-2 py-1 text-xs text-amber-900 hover:bg-amber-100 dark:text-amber-200 dark:hover:bg-amber-900/40"
                              >
                                Reconnect anyway
                              </button>
                              <button
                                onClick={() => setSharedClientConfirm(null)}
                                className="rounded border border-border px-2 py-1 text-xs text-muted-foreground hover:text-foreground"
                              >
                                Cancel
                              </button>
                            </div>
                          </div>
                        )}

                        {editGrantsSavedConnId === conn.id && (
                          <StatusBanner tone="success">
                            Saved. This only changed the stored request — click <strong>Reconnect</strong> above and complete Google's consent screen for it to actually take effect.
                          </StatusBanner>
                        )}

                        {editingGrantsConnId === conn.id && (
                          <div className="mt-2 space-y-1.5 rounded-md border border-border bg-muted/20 p-2">
                            <label className="flex items-start gap-2 text-xs text-muted-foreground">
                              <input
                                type="checkbox"
                                checked={editAllowRead}
                                disabled={readOnly}
                                onChange={event => setEditAllowRead(event.target.checked)}
                                className="mt-0.5"
                              />
                              <span>Also allow <strong>reading</strong> this mailbox</span>
                            </label>
                            {serviceCatalog && Object.keys(serviceCatalog).length > 0 && (
                              <div className="space-y-1.5 border-t border-border pt-1.5">
                                {Object.entries(serviceCatalog).map(([service, label]) => {
                                  const grant = editServices[service]
                                  return (
                                    <div key={service} className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                                      <label className="flex items-center gap-2">
                                        <input
                                          type="checkbox"
                                          checked={!!grant}
                                          disabled={readOnly}
                                          onChange={event => setEditServices(prev => {
                                            const next = { ...prev }
                                            if (event.target.checked) next[service] = { write: false }
                                            else delete next[service]
                                            return next
                                          })}
                                        />
                                        {label}
                                      </label>
                                      {grant && (
                                        <label
                                          className="flex items-center gap-1.5 pl-1 text-[11px]"
                                          title="Off (read-only) is the safer default. Turn on only if a workflow must create or edit, not just read. Changing this requires Reconnect below to take effect."
                                        >
                                          <input
                                            type="checkbox"
                                            checked={grant.write}
                                            disabled={readOnly}
                                            onChange={event => setEditServices(prev => ({ ...prev, [service]: { write: event.target.checked } }))}
                                          />
                                          allow write access{grant.write ? '' : ' (off = read-only)'}
                                        </label>
                                      )}
                                    </div>
                                  )
                                })}
                              </div>
                            )}
                            <div className="flex gap-2 pt-1">
                              <Button
                                onClick={() => saveEditGrants(conn)}
                                disabled={readOnly || gmailConnectionsBusy === conn.id}
                                title={readOnly ? READ_ONLY_TITLE : undefined}
                              >
                                {gmailConnectionsBusy === conn.id ? <><Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />Saving…</> : 'Save request'}
                              </Button>
                              <button
                                onClick={() => setEditingGrantsConnId(null)}
                                className="rounded border border-border px-2 py-1 text-xs text-muted-foreground hover:text-foreground"
                              >
                                Cancel
                              </button>
                            </div>
                          </div>
                        )}

                        {gmailAuthPending === conn.id && gmailAuthUrl && (
                          <SignInLinkBox url={gmailAuthUrl} />
                        )}
                      </li>
                    ))}
                  </ul>
                )}

                {gmailConnections.length > 0 && !showAddClientForm ? (
                  <button
                    onClick={() => setShowAddClientForm(true)}
                    disabled={readOnly}
                    title={readOnly ? READ_ONLY_TITLE : undefined}
                    className="rounded border border-border px-2 py-1 text-xs text-muted-foreground hover:text-foreground disabled:opacity-40"
                  >
                    + Add account
                  </button>
                ) : (
                  <div className="flex flex-wrap items-center gap-2 border-t border-border pt-3">
                    <input
                      type="email"
                      autoFocus={gmailConnections.length > 0}
                      value={gmailNewClientEmail}
                      onChange={event => setGmailNewClientEmail(event.target.value)}
                      disabled={readOnly}
                      placeholder="Mailbox to connect (e.g. you@example.com)"
                      className="flex-1 rounded-md border border-border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-primary"
                    />
                    <input
                      type="file"
                      accept="application/json"
                      disabled={readOnly}
                      onChange={event => setNewClientFile(event.target.files?.[0] || null)}
                      className="flex-1 text-xs text-muted-foreground file:mr-2 file:rounded file:border file:border-border file:bg-muted/40 file:px-2 file:py-1 file:text-xs"
                    />
                    <label
                      className="flex basis-full items-start gap-2 text-xs text-muted-foreground"
                      title="Send-only is the default and is all notifications need. Read access lets a workflow search or read this mailbox too; it is fixed at sign-in, so changing it later means reconnecting."
                    >
                      <input
                        type="checkbox"
                        checked={newClientAllowRead}
                        disabled={readOnly}
                        onChange={event => setNewClientAllowRead(event.target.checked)}
                        className="mt-0.5"
                      />
                      <span>
                        Also allow <strong>reading</strong> this mailbox
                        <span className="block text-[11px] text-muted-foreground/80">Off by default: sending is all notifications need. Turn on only if a workflow must read or search mail.</span>
                      </span>
                    </label>
                    {serviceCatalog && Object.keys(serviceCatalog).length > 0 && (
                      <div className="basis-full space-y-1.5 border-t border-border pt-2">
                        <p className="text-xs text-muted-foreground">Also connect other Google Workspace services for this mailbox — a workflow can then use them directly. Read-only by default; fixed at sign-in like above.</p>
                        {Object.entries(serviceCatalog).map(([service, label]) => {
                          const grant = newClientServices[service]
                          return (
                            <div key={service} className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                              <label className="flex items-center gap-2">
                                <input
                                  type="checkbox"
                                  checked={!!grant}
                                  disabled={readOnly}
                                  onChange={event => setNewClientServices(prev => {
                                    const next = { ...prev }
                                    if (event.target.checked) next[service] = { write: false }
                                    else delete next[service]
                                    return next
                                  })}
                                />
                                {label}
                              </label>
                              {grant && (
                                <label className="flex items-center gap-1.5 pl-1 text-[11px]" title="Off (read-only) is the safer default. Turn on only if a workflow must create or edit, not just read.">
                                  <input
                                    type="checkbox"
                                    checked={grant.write}
                                    disabled={readOnly}
                                    onChange={event => setNewClientServices(prev => ({ ...prev, [service]: { write: event.target.checked } }))}
                                  />
                                  allow write access{grant.write ? '' : ' (off = read-only)'}
                                </label>
                              )}
                            </div>
                          )
                        })}
                      </div>
                    )}
                    <Button
                      variant="outline"
                      disabled={readOnly || !gmailNewClientEmail.trim() || !newClientFile || gmailOAuthClientsBusy}
                      title={readOnly ? READ_ONLY_TITLE : undefined}
                      onClick={async () => {
                        if (await handleAddOAuthClient()) setShowAddClientForm(false)
                      }}
                    >
                      {gmailOAuthClientsBusy ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Add & sign in'}
                    </Button>
                  </div>
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
