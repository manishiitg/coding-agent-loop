import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { agentApi } from '../../../services/api'
import { useWorkflowManifestStore } from '../../../stores/useWorkflowManifestStore'
import { useCanWriteWorkflow } from '../../../hooks/useCanWriteWorkflow'
import type {
  ChannelRoute, GmailConfigRequest, GmailConfigResponse, GmailConnection, GmailOAuthClient, GmailTestResponse,
  GoogleServiceGrant, SlackConfig, SlackConfigRequest, SlackTestResponse, WhatsAppRoute, WhatsAppStatus,
} from '../../../services/api-types'
import { routeId, type ChannelKind, type WorkflowRoute } from './types'
import { gmailOAuthAttemptCompleted } from './gmailOAuthState'

type WaRoute = WhatsAppRoute

const SLUG_RE = /^[a-z0-9-]+$/

const normalizeGmailEmails = (values: string | string[] | undefined): string[] => {
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

const emptyGmailConfig: GmailConfigResponse = {
  enabled: false,
  default_to: '',
  auth: { gws_installed: false, authenticated: false, has_gmail_scope: false },
  ready: false,
}

// All state, loaders, effects, routing writes, and handlers for the Bots
// panel. The panel and its children are composition over this hook's result.
export function useWorkflowBots(workspacePath: string | null) {
  // ── Workflow identity ─────────────────────────────────────────────────────
  const workflows = useWorkflowManifestStore(state => state.workflows)
  const refreshWorkflows = useWorkflowManifestStore(state => state.refreshWorkflows)
  const workflow = useMemo(
    () => (workspacePath ? workflows.find(w => w.workspace_path === workspacePath) : undefined),
    [workflows, workspacePath],
  )
  const workflowId = workflow?.manifest.id ?? null
  const workflowLabel = (id: string) => workflows.find(w => w.manifest.id === id)?.manifest.label || id
  // Every write here lands in shared connector config immediately -- there's
  // no Save step for the panel to gate -- so each mutating control disables.
  const readOnly = !useCanWriteWorkflow(workspacePath)

  useEffect(() => {
    if (workflows.length === 0) void refreshWorkflows()
  }, [refreshWorkflows, workflows.length])

  // ── Panel navigation ──────────────────────────────────────────────────────
  const [setup, setSetup] = useState<ChannelKind | null>(null)
  const [expandedChip, setExpandedChip] = useState<string | null>(null)
  const [routeSaving, setRouteSaving] = useState<string | null>(null)
  const [routeError, setRouteError] = useState<string | null>(null)
  const [newSlackChannel, setNewSlackChannel] = useState('')
  const [newWaSlug, setNewWaSlug] = useState('')
  const [addError, setAddError] = useState<Partial<Record<ChannelKind, string>>>({})

  // ── Slack ─────────────────────────────────────────────────────────────────
  const [slackConfig, setSlackConfig] = useState<SlackConfig>({ enabled: false, bot_token: '', app_token: '', channel_id: '' })
  const [slackOriginal, setSlackOriginal] = useState<SlackConfig>(slackConfig)
  const [slackLoading, setSlackLoading] = useState(true)
  const [slackSaving, setSlackSaving] = useState(false)
  const [slackTesting, setSlackTesting] = useState(false)
  const [slackError, setSlackError] = useState<string | null>(null)
  const [slackSuccess, setSlackSuccess] = useState<string | null>(null)
  const [testResult, setTestResult] = useState<SlackTestResponse | null>(null)
  const [testReply, setTestReply] = useState<string | null>(null)
  const [pollingForReply, setPollingForReply] = useState(false)
  const [showBotToken, setShowBotToken] = useState(false)
  const [showAppToken, setShowAppToken] = useState(false)
  const [allowedEmails, setAllowedEmails] = useState('')
  const [emailsDirty, setEmailsDirty] = useState(false)
  const [emailsSaving, setEmailsSaving] = useState(false)
  const [emailsSaved, setEmailsSaved] = useState(false)

  // ── WhatsApp ──────────────────────────────────────────────────────────────
  const [waStatus, setWaStatus] = useState<WhatsAppStatus | null>(null)
  const [waError, setWaError] = useState<string | null>(null)
  const [waRouting, setWaRouting] = useState<Record<string, WaRoute>>({})
  const [waRoutingError, setWaRoutingError] = useState<string | null>(null)
  const [qrBust, setQrBust] = useState<number>(() => Date.now())
  const [qrImageURL, setQrImageURL] = useState<string | null>(null)
  const [qrLoading, setQrLoading] = useState(false)
  const [qrError, setQrError] = useState<string | null>(null)
  const [unpairConfirm, setUnpairConfirm] = useState(false)
  const [unpairing, setUnpairing] = useState(false)
  const waPollingRef = useRef<ReturnType<typeof setInterval> | null>(null)

  // ── Gmail (account-wide, shared by every workflow) ────────────────────────
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
  // Multi-account senders. Empty is the normal state for an install that has
  // not adopted connections, and the card then simply says so.
  const [gmailConnections, setGmailConnections] = useState<GmailConnection[]>([])
  const [gmailConnectionsBusy, setGmailConnectionsBusy] = useState<string | null>(null)
  // Named OAuth clients (the Google Cloud app a connection authorizes
  // under). Treated 1:1 with a sending account in the UI: registering one
  // creates its account and starts sign-in immediately
  // (createGmailOAuthClient), and removing the account also removes its
  // client (removeGmailMailboxAndClient) — upload+email is one action, not
  // two separate entities to manage. The backend still allows a client to
  // back more than one account (an older client created before this UI
  // simplification might), which is why removal only cascades when the
  // client truly has no other account left using it.
  const [gmailOAuthClients, setGmailOAuthClients] = useState<GmailOAuthClient[]>([])
  const [gmailOAuthClientsBusy, setGmailOAuthClientsBusy] = useState(false)
  const [gmailOAuthClientError, setGmailOAuthClientError] = useState<string | null>(null)
  // The email the operator intends to sign in with this client — used to
  // derive the client's internal name (slugified) instead of asking for an
  // arbitrary label, since the mailbox is what an operator actually thinks
  // in terms of. The email itself is not verified until sign-in; this is a
  // naming hint, not a claim about which account will actually authorize.
  const [gmailNewClientEmail, setGmailNewClientEmail] = useState('')
  // Set while a Google sign-in is in flight, so the row can say it is waiting.
  const [gmailAuthPending, setGmailAuthPending] = useState<string | null>(null)
  // The authorize URL handed to the browser, kept so the UI can offer it as
  // copyable text. openExternal/window.open land in whichever Chrome profile
  // is frontmost, which is often not the profile holding the mailbox being
  // connected. Copying the address bar out of that tab does not work: by
  // then Chrome has followed Google's redirects, and the bar holds a
  // continuation URL whose token is bound to the originating profile's
  // cookies — pasting it into another profile fails with a bare "400 ...
  // malformed". Only this original authorize URL is safe to paste anywhere.
  const [gmailAuthUrl, setGmailAuthUrl] = useState<string | null>(null)
  const [gmailTestedTo, setGmailTestedTo] = useState<string | null>(null)
  const [gmailOpen, setGmailOpen] = useState(false)

  // ── Loaders ───────────────────────────────────────────────────────────────
  const loadEmails = useCallback(async () => {
    try {
      const cfg = await agentApi.getBotConfig()
      if (Array.isArray(cfg.allowed_emails)) setAllowedEmails(cfg.allowed_emails.join(', '))
    } catch { /* ignore */ }
  }, [])

  const loadSlack = useCallback(async () => {
    try {
      setSlackLoading(true)
      setSlackError(null)
      const data = await agentApi.getSlackFeedbackConfig()
      setSlackConfig(data)
      setSlackOriginal(data)
    } catch (err) {
      setSlackError(err instanceof Error ? err.message : 'Failed to load Slack configuration')
    } finally {
      setSlackLoading(false)
    }
  }, [])

  const loadWaStatus = useCallback(async () => {
    try {
      const s = await agentApi.getWhatsAppStatus()
      setWaStatus(s)
      setWaError(null)
      return s
    } catch (err) {
      setWaError(err instanceof Error ? err.message : String(err))
      return null
    }
  }, [])

  const loadWaRouting = useCallback(async () => {
    try {
      const data = await agentApi.getWhatsAppRouting()
      setWaRouting(data.routing || {})
      setWaRoutingError(null)
    } catch (err) {
      setWaRoutingError(err instanceof Error ? err.message : String(err))
    }
  }, [])

  const refreshWaQR = useCallback(() => {
    setQrBust(Date.now())
  }, [])

  const loadGmailConnections = useCallback(async (attempt = 0) => {
    try {
      const data = await agentApi.listGmailConnections()
      const connections = data.connections || []
      setGmailConnections(connections)
      // The server answers the first read with "checking" while it runs each
      // account's auth check in the background (~5s per gws call) and never
      // pushes the result, so a row sat on "Checking…" with a "Sign in with
      // Google" button for an account that was already connected (RTS,
      // 2026-09-03). Re-read while anything is still checking, bounded so a
      // check that never settles cannot poll forever.
      if (attempt < 15 && connections.some(entry => entry.auth?.checking)) {
        window.setTimeout(() => { void loadGmailConnections(attempt + 1) }, 2000)
      }
    } catch {
      // A registry that cannot be read must not break the single-account view
      // this panel has always shown.
      setGmailConnections([])
    }
  }, [])

  const loadGmailOAuthClients = useCallback(async () => {
    try {
      const data = await agentApi.listGmailOAuthClients()
      setGmailOAuthClients(data.clients || [])
    } catch {
      setGmailOAuthClients([])
    }
  }, [])

  // Every mutation re-reads the list rather than patching local state, so the
  // server stays the single source of truth for status and which is default.
  const runGmailConnectionAction = useCallback(
    async (id: string, action: () => Promise<unknown>) => {
      try {
        setGmailConnectionsBusy(id)
        setGmailError(null)
        await action()
        await loadGmailConnections()
      } catch (error) {
        setGmailError(error instanceof Error ? error.message : 'Connection action failed')
      } finally {
        setGmailConnectionsBusy(null)
      }
    },
    [loadGmailConnections],
  )

  // Open Google's consent screen and refresh once the account is connected.
  //
  // Electron blocks window.open, so prefer the desktop bridge that opens the
  // system browser. Signing in there is also better than a popup: people can
  // see the real accounts.google.com address bar, which is exactly what they
  // should check before typing a password.
  const connectGmailAccount = useCallback(async (id: string) => {
    try {
      setGmailError(null)
      setGmailAuthPending(id)
      setGmailAuthUrl(null)
      // Capture server state before starting. Reconnect often begins while
      // the old token is still healthy, so `ready === true` cannot prove the
      // new consent flow completed.
      const beforeData = await agentApi.listGmailConnections()
      const before = beforeData.connections.find(entry => entry.id === id)
      if (!before) throw new Error(`Gmail connection ${id} no longer exists.`)
      const { auth_url } = await agentApi.startGmailConnectionAuth(id)
      setGmailAuthUrl(auth_url)

      const electronAPI = (window as unknown as {
        electronAPI?: { openExternal?: (url: string) => void }
      }).electronAPI
      const popup = electronAPI?.openExternal
        ? (electronAPI.openExternal(auth_url), null)
        : window.open(auth_url, 'gmail-oauth', 'width=520,height=680')

      if (!electronAPI?.openExternal && !popup) {
        setGmailError('Allow pop-ups for this app, then try connecting again.')
        setGmailAuthPending(null)
        setGmailAuthUrl(null)
        return
      }

      // The callback lands on the server, not in this document, so there is no
      // event here to listen for — and with an external browser there is no
      // window to watch closing either. Poll until the server reports that
      // this attempt changed the persisted connection, not merely `ready`
      // from a credential that existed before Reconnect was clicked.
      //
      // The window matches the server's 15-minute pending-state TTL
      // (gmailOAuthPendingTTL) rather than guessing shorter: anyone whose
      // mailbox lives in a different Chrome profile has to open that profile
      // and paste the copied link across by hand, and giving up while the
      // server would still accept the callback strands them mid-sign-in.
      const startedAt = Date.now()
      let popupClosedNoticeShown = false
      let pollInFlight = false
      const timer = window.setInterval(async () => {
        if (pollInFlight) return
        pollInFlight = true
        const timedOut = Date.now() - startedAt > 15 * 60 * 1000
        if (popup?.closed && !popupClosedNoticeShown) {
          popupClosedNoticeShown = true
          // Closing the first popup often means the mailbox belongs to a
          // different browser profile. Keep the server attempt and copyable
          // link alive so it can be pasted there instead.
          setGmailError('The sign-in window was closed. Use the Copy link option below to continue in another browser profile.')
        }

        try {
          const data = await agentApi.listGmailConnections().catch(() => null)
          const after = data?.connections?.find(entry => entry.id === id)
          const connected = gmailOAuthAttemptCompleted(before, after)
          if (!connected && !timedOut) return

          window.clearInterval(timer)
          setGmailAuthPending(null)
          setGmailAuthUrl(null)
          setGmailError(connected ? null : 'Sign-in did not complete before the link expired. Try connecting again.')
          await loadGmailConnections()
        } finally {
          pollInFlight = false
        }
      }, 1500)
    } catch (error) {
      setGmailAuthPending(null)
      setGmailAuthUrl(null)
      setGmailError(error instanceof Error ? error.message : 'Could not start Google sign-in')
    }
  }, [loadGmailConnections])

  // Registers the OAuth client, creates its sending account, and immediately
  // opens Google's consent screen for it — upload + email + one click is the
  // whole flow; there's no separate "now click Sign in with Google" step to
  // remember, since 99% of the time the mailbox being added is the same one
  // the client's credentials are for.
  // allowReadAccess opts the new mailbox into gmail.readonly on top of
  // gmail.send. Defaults off: notifications only ever send, so the consent
  // screen asks for the minimum unless the operator deliberately widens it.
  // services requests additional Google Workspace scopes (Drive, Sheets...)
  // beyond Gmail — see GoogleServiceGrant. Also defaults to none.
  const createGmailOAuthClient = useCallback(async (
    email: string,
    clientSecretJson: unknown,
    allowReadAccess = false,
    services: GoogleServiceGrant[] = [],
  ) => {
    const trimmedEmail = email.trim()
    // Backend name pattern is lowercase letters/digits/hyphens only (it
    // becomes a directory name) — an email's @ and . don't qualify, so
    // derive a slug from it rather than asking the operator for a second,
    // arbitrary label.
    const name = trimmedEmail.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 63)
    try {
      setGmailOAuthClientsBusy(true)
      setGmailOAuthClientError(null)
      const client = await agentApi.createGmailOAuthClient(name, clientSecretJson)
      const connection = await agentApi.createGmailConnection({
        display_name: trimmedEmail,
        client_name: client.name,
        allow_read_access: allowReadAccess,
        services,
      })
      await Promise.all([loadGmailOAuthClients(), loadGmailConnections()])
      setGmailNewClientEmail('')
      void connectGmailAccount(connection.id)
      return true
    } catch (error) {
      setGmailOAuthClientError(error instanceof Error ? error.message : 'Failed to register the OAuth client')
      return false
    } finally {
      setGmailOAuthClientsBusy(false)
    }
  }, [loadGmailOAuthClients, loadGmailConnections, connectGmailAccount])

  // Removes a sending account and, when it was the last one using its OAuth
  // client, the client too — the two were created together as one action
  // (createGmailOAuthClient above), so removal undoes both by default rather
  // than leaving an orphaned, unused client behind that has to be separately
  // noticed and cleaned up. A client still backing another mailbox is left
  // alone: only the account being removed is deleted.
  const removeGmailMailboxAndClient = useCallback(async (connectionId: string, clientName: string) => {
    try {
      setGmailConnectionsBusy(connectionId)
      setGmailError(null)
      await agentApi.deleteGmailConnection(connectionId)
      const stillInUse = gmailConnections.some(c => c.id !== connectionId && c.client_name === clientName)
      if (!stillInUse && clientName) {
        await agentApi.deleteGmailOAuthClient(clientName).catch(() => {
          // Best-effort: the account is already gone either way, and an
          // orphaned client can still be removed later from its own row.
        })
      }
      await Promise.all([loadGmailConnections(), loadGmailOAuthClients()])
    } catch (error) {
      setGmailError(error instanceof Error ? error.message : 'Failed to remove the sending account')
    } finally {
      setGmailConnectionsBusy(null)
    }
  }, [gmailConnections, loadGmailConnections, loadGmailOAuthClients])

  const loadGmail = useCallback(async (background = false) => {
    try {
      if (background) setGmailChecking(true)
      else setGmailLoading(true)
      setGmailError(null)
      const data = await agentApi.getGmailFeedbackConfig()
      if (background) {
        setGmailConfig(current => ({ ...current, auth: data.auth, ready: data.ready }))
      } else {
        const blocked = normalizeGmailEmails(data.blocked_recipients)
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
    void loadGmailConnections()
    void loadGmailOAuthClients()
  }, [loadGmailConnections, loadGmailOAuthClients])

  useEffect(() => {
    void loadEmails()
    void loadSlack()
    void loadWaStatus()
    void loadWaRouting()
    void loadGmail()
  }, [loadEmails, loadGmail, loadSlack, loadWaRouting, loadWaStatus])

  // ── WhatsApp: status polling while the pairing screen is open ─────────────
  // Not yet paired → poll /status every 3s so a fresh QR (rotating every
  // ~20s on the server) plus the transition to "paired" shows up without the
  // user having to refresh. Only runs while the WhatsApp setup screen is open.
  useEffect(() => {
    if (setup !== 'whatsapp') {
      if (waPollingRef.current) {
        clearInterval(waPollingRef.current)
        waPollingRef.current = null
      }
      return
    }

    let cancelled = false
    let lastExpires: string | undefined
    const tick = async () => {
      try {
        const s = await agentApi.getWhatsAppStatus()
        if (cancelled) return
        setWaStatus(s)
        setWaError(null)
        if (s.qr_expires_at !== lastExpires) {
          lastExpires = s.qr_expires_at
          setQrBust(Date.now())
        }
      } catch (err) {
        if (cancelled) return
        setWaError(err instanceof Error ? err.message : String(err))
      }
    }
    tick()
    waPollingRef.current = setInterval(tick, 3000)
    return () => {
      cancelled = true
      if (waPollingRef.current) {
        clearInterval(waPollingRef.current)
        waPollingRef.current = null
      }
    }
  }, [setup])

  useEffect(() => {
    if (setup !== 'whatsapp' || !waStatus?.enabled || waStatus.paired || !waStatus.qr_available) {
      setQrLoading(false)
      setQrError(null)
      setQrImageURL(prev => {
        if (prev) URL.revokeObjectURL(prev)
        return null
      })
      return
    }

    let cancelled = false
    setQrLoading(true)
    setQrError(null)
    agentApi.getWhatsAppPairQR(384, qrBust)
      .then(blob => {
        if (cancelled) return
        const nextURL = URL.createObjectURL(blob)
        setQrImageURL(prev => {
          if (prev) URL.revokeObjectURL(prev)
          return nextURL
        })
      })
      .catch(err => {
        if (cancelled) return
        setQrImageURL(prev => {
          if (prev) URL.revokeObjectURL(prev)
          return null
        })
        setQrError(err instanceof Error ? err.message : String(err))
      })
      .finally(() => {
        if (!cancelled) setQrLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [setup, waStatus?.enabled, waStatus?.paired, waStatus?.qr_available, qrBust])

  useEffect(() => {
    return () => {
      if (qrImageURL) URL.revokeObjectURL(qrImageURL)
    }
  }, [qrImageURL])

  // ── Routes for this workflow ──────────────────────────────────────────────
  const myRoutes = useMemo<WorkflowRoute[]>(() => {
    if (!workflowId) return []
    const slack = Object.entries(slackOriginal.channel_routing || {})
      .filter(([, r]) => r.workflow_id === workflowId)
      .map(([key, r]) => ({ kind: 'slack' as const, key, workshop_mode: r.workshop_mode, send_full_details: r.send_full_details }))
    const wa = Object.entries(waRouting)
      .filter(([, r]) => r.workflow_id === workflowId)
      .map(([key, r]) => ({ kind: 'whatsapp' as const, key, workshop_mode: r.workshop_mode, send_full_details: r.send_full_details }))
    return [...slack, ...wa]
  }, [slackOriginal.channel_routing, waRouting, workflowId])

  // Slack routing writes: base on the last *loaded* config so unsaved edits
  // on the Set up screen never ride along, and only channel_routing changes.
  // Masked tokens round-trip as "no change" server-side.
  const saveSlackRouting = useCallback(async (next: Record<string, ChannelRoute>) => {
    const request: SlackConfigRequest = {
      enabled: slackOriginal.enabled,
      bot_token: slackOriginal.bot_token || '',
      app_token: slackOriginal.app_token || '',
      channel_id: slackOriginal.channel_id || '',
      bot_mode: slackOriginal.bot_mode || false,
      channel_routing: next,
    }
    await agentApi.updateSlackFeedbackConfig(request)
    await loadSlack()
  }, [loadSlack, slackOriginal])

  // WhatsApp routing writes: PUT replaces the whole map, so always send the
  // full map with only this workflow's entries changed.
  const saveWaRouting = useCallback(async (next: Record<string, WaRoute>) => {
    const data = await agentApi.updateWhatsAppRouting(next)
    setWaRouting(data.routing || {})
    setWaRoutingError(null)
  }, [])

  const withRouteSaving = async (id: string, fn: () => Promise<void>) => {
    setRouteSaving(id)
    setRouteError(null)
    try {
      await fn()
    } catch (err) {
      setRouteError(err instanceof Error ? err.message : 'Failed to save routing')
    } finally {
      setRouteSaving(null)
    }
  }

  const removeRoute = (route: WorkflowRoute) => withRouteSaving(routeId(route), async () => {
    if (route.kind === 'slack') {
      const next = { ...(slackOriginal.channel_routing || {}) }
      delete next[route.key]
      await saveSlackRouting(next)
    } else {
      const next = { ...waRouting }
      delete next[route.key]
      await saveWaRouting(next)
    }
    if (expandedChip === routeId(route)) setExpandedChip(null)
  })

  const updateRoute = (route: WorkflowRoute, patch: { workshop_mode?: string; send_full_details?: boolean }) => withRouteSaving(routeId(route), async () => {
    if (route.kind === 'slack') {
      const current = slackOriginal.channel_routing?.[route.key]
      if (!current) return
      const nextRoute: ChannelRoute = { ...current }
      if ('workshop_mode' in patch) {
        if (patch.workshop_mode === 'run') nextRoute.workshop_mode = 'run'
        else delete nextRoute.workshop_mode
      }
      if ('send_full_details' in patch) {
        if (patch.send_full_details) nextRoute.send_full_details = true
        else delete nextRoute.send_full_details
      }
      await saveSlackRouting({ ...(slackOriginal.channel_routing || {}), [route.key]: nextRoute })
    } else {
      const current = waRouting[route.key]
      if (!current) return
      const nextRoute: WaRoute = { ...current }
      if ('workshop_mode' in patch) nextRoute.workshop_mode = 'run'
      if ('send_full_details' in patch) {
        if (patch.send_full_details) nextRoute.send_full_details = true
        else delete nextRoute.send_full_details
      }
      await saveWaRouting({ ...waRouting, [route.key]: nextRoute })
    }
  })

  const addSlackRoute = () => {
    const channel = newSlackChannel.trim()
    if (!channel || !workflowId) return
    const existing = slackOriginal.channel_routing?.[channel]
    if (existing && existing.workflow_id !== workflowId) {
      setAddError(e => ({ ...e, slack: `Channel ${channel} is already routed to ${workflowLabel(existing.workflow_id)}.` }))
      return
    }
    if (existing) {
      setAddError(e => ({ ...e, slack: `Channel ${channel} is already routed to this workflow.` }))
      return
    }
    setAddError(e => ({ ...e, slack: undefined }))
    void withRouteSaving(`slack:${channel}`, async () => {
      const route: ChannelRoute = { workflow_id: workflowId, workspace_path: workflow?.workspace_path || '' }
      await saveSlackRouting({ ...(slackOriginal.channel_routing || {}), [channel]: route })
      setNewSlackChannel('')
    })
  }

  const addWaRoute = () => {
    const slug = newWaSlug.trim().toLowerCase()
    if (!slug || !workflowId) return
    if (!SLUG_RE.test(slug)) {
      setAddError(e => ({ ...e, whatsapp: 'Slugs can only contain a-z, 0-9 and dashes.' }))
      return
    }
    const existing = waRouting[slug]
    if (existing && existing.workflow_id !== workflowId) {
      setAddError(e => ({ ...e, whatsapp: `@${slug} is already routed to ${workflowLabel(existing.workflow_id)}.` }))
      return
    }
    if (existing) {
      setAddError(e => ({ ...e, whatsapp: `@${slug} is already routed to this workflow.` }))
      return
    }
    setAddError(e => ({ ...e, whatsapp: undefined }))
    void withRouteSaving(`whatsapp:${slug}`, async () => {
      const route: WaRoute = { workflow_id: workflowId, workspace_path: workflow?.workspace_path || '', workshop_mode: 'run' }
      await saveWaRouting({ ...waRouting, [slug]: route })
      setNewWaSlug('')
    })
  }

  // ── Slack handlers (setup screen) ─────────────────────────────────────────
  const handleEmailsSave = async () => {
    setEmailsSaving(true)
    try {
      const emails = allowedEmails.split(',').map(e => e.trim()).filter(e => e.length > 0)
      await agentApi.saveBotConfig({ allowed_emails: emails })
      setEmailsDirty(false)
      setEmailsSaved(true)
      setTimeout(() => setEmailsSaved(false), 2000)
    } catch { /* ignore */ } finally { setEmailsSaving(false) }
  }

  const handleSlackSave = async () => {
    try {
      setSlackSaving(true)
      setSlackError(null)
      setSlackSuccess(null)
      const request: SlackConfigRequest = {
        enabled: slackConfig.enabled, bot_token: slackConfig.bot_token || '',
        app_token: slackConfig.app_token || '', channel_id: slackConfig.channel_id || '',
        bot_mode: slackConfig.bot_mode || false,
        // Routing is edited from the chips, never from this screen -- always
        // carry the last-loaded map so a credentials save can't drop routes.
        channel_routing: slackOriginal.channel_routing || {},
      }
      await agentApi.updateSlackFeedbackConfig(request)
      setSlackSuccess('Saved successfully!')
      await loadSlack()
      setTimeout(() => setSlackSuccess(null), 3000)
    } catch (err) {
      setSlackError(err instanceof Error ? err.message : 'Failed to save Slack configuration')
    } finally { setSlackSaving(false) }
  }

  const pollForTestReply = (testId: string) => {
    let attempts = 0
    const poll = async () => {
      if (attempts >= 60) { setPollingForReply(false); return }
      try {
        const reply = await agentApi.getTestConnectionReply(testId)
        if (reply?.received) { setTestReply(reply.reply); setPollingForReply(false); return }
      } catch { /* ignore 204 */ }
      attempts++
      setTimeout(poll, 1000)
    }
    poll()
  }

  const handleSlackTest = async () => {
    try {
      setSlackTesting(true)
      setSlackError(null)
      setTestResult(null)
      setTestReply(null)
      setPollingForReply(false)
      // Test against the saved workspace config, not whatever is typed in the form.
      const result = await agentApi.testSlackConnection()
      setTestResult(result)
      if (result.success && result.test_id) { setPollingForReply(true); pollForTestReply(result.test_id) }
    } catch (err) {
      setTestResult({ success: false, message: err instanceof Error ? err.message : 'Connection test failed' })
    } finally { setSlackTesting(false) }
  }

  // ── WhatsApp handlers (setup screen) ──────────────────────────────────────
  // Drops the paired phone, restarts the service, and refreshes local status.
  // Two-step confirmation prevents accidental clicks.
  const handleUnpairWhatsApp = async () => {
    if (!unpairConfirm) {
      setUnpairConfirm(true)
      setTimeout(() => setUnpairConfirm(false), 5000)
      return
    }
    try {
      setUnpairing(true)
      setWaError(null)
      await agentApi.unpairWhatsApp()
      setUnpairConfirm(false)
      await loadWaStatus()
      setQrBust(Date.now())
    } catch (err) {
      setWaError(err instanceof Error ? err.message : 'Failed to unpair')
    } finally {
      setUnpairing(false)
    }
  }

  // ── Gmail derived state + save/test ───────────────────────────────────────
  const gmailCurrentBlocked = normalizeGmailEmails(gmailBlockedText)
  const gmailDefaultRecipients = normalizeGmailEmails(gmailConfig.default_to)
  const gmailBlockedDefaults = gmailDefaultRecipients.filter(email => gmailCurrentBlocked.includes(email))
  const gmailDefaultIsBlocked = gmailBlockedDefaults.length > 0
  const gmailTestPassed = gmailTestResult?.success === true
    && gmailTestedTo === (gmailConfig.default_to || '')
    && !gmailDefaultIsBlocked
  // An authenticated account with the Gmail scope is reason enough to enable
  // the channel (user decision 2026-09-03; the server auto-enables on the same
  // condition). A passing test still counts, for a host that is authenticated
  // some other way.
  const gmailCanEnable = gmailTestPassed
    || (gmailConfig.auth?.authenticated === true && gmailConfig.auth?.has_gmail_scope === true)
  const gmailHasChanges = gmailConfig.enabled !== gmailOriginal.enabled
    || (gmailConfig.default_to || '') !== gmailOriginal.default_to
    || JSON.stringify(gmailCurrentBlocked) !== JSON.stringify(gmailOriginal.blocked_recipients)

  const saveGmail = async () => {
    try {
      setGmailSaving(true)
      setGmailError(null)
      setGmailSuccess(null)
      const request: GmailConfigRequest = {
        enabled: gmailConfig.enabled,
        default_to: gmailConfig.default_to || '',
        blocked_recipients: gmailCurrentBlocked,
      }
      const data = await agentApi.updateGmailFeedbackConfig(request)
      const blocked = normalizeGmailEmails(data.blocked_recipients)
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
        blocked_recipients: gmailCurrentBlocked,
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

  // ── Connection status per channel ─────────────────────────────────────────
  // Slack routing only works with the app enabled AND bot mode on -- both are
  // required before a channel can be added.
  const slackReady = slackOriginal.enabled && !!slackOriginal.bot_mode
  const slackStatusLabel = slackLoading ? 'Loading…'
    : !slackOriginal.enabled ? 'Not connected'
      : !slackOriginal.bot_mode ? 'Bot mode off'
        : 'Connected'

  const waReady = !!waStatus?.enabled && !!waStatus.paired
  const waStatusLabel = !waStatus ? (waError ? 'Unavailable' : 'Loading…')
    : !waStatus.enabled ? 'Disabled on server'
      : !waStatus.paired ? 'Not paired'
        : waStatus.connected ? 'Connected' : 'Paired, offline'

  const slackHasChanges = JSON.stringify(slackConfig) !== JSON.stringify(slackOriginal)

  return {
    // identity / access
    workflowId, readOnly,
    // navigation
    setup, setSetup, expandedChip, setExpandedChip,
    routeSaving, routeError, newSlackChannel, setNewSlackChannel, newWaSlug, setNewWaSlug, addError, setAddError,
    // slack
    slackConfig, setSlackConfig, slackOriginal, slackLoading, slackSaving, slackTesting, slackError, slackSuccess,
    testResult, testReply, pollingForReply, showBotToken, setShowBotToken, showAppToken, setShowAppToken,
    allowedEmails, setAllowedEmails, emailsDirty, setEmailsDirty, emailsSaving, emailsSaved, setEmailsSaved,
    handleEmailsSave, handleSlackSave, handleSlackTest, slackHasChanges, slackReady, slackStatusLabel,
    // whatsapp
    waStatus, waError, waRoutingError, qrImageURL, qrLoading, qrError, unpairConfirm, unpairing, refreshWaQR,
    handleUnpairWhatsApp, waReady, waStatusLabel,
    // routes
    myRoutes, removeRoute, updateRoute, addSlackRoute, addWaRoute,
    // gmail
    gmailOpen, setGmailOpen, gmailConfig, setGmailConfig, gmailBlockedText, setGmailBlockedText,
    gmailLoading, gmailChecking, gmailSaving, gmailTesting, gmailError, gmailSuccess, gmailTestResult,
    gmailBlockedDefaults, gmailDefaultIsBlocked, gmailTestPassed, gmailCanEnable, gmailHasChanges, loadGmail, saveGmail, testGmail,
    // gmail senders (multi-account)
    gmailConnections, gmailConnectionsBusy, gmailAuthPending, gmailAuthUrl,
    loadGmailConnections, runGmailConnectionAction, connectGmailAccount,
    // gmail OAuth clients (named Google Cloud apps connections authorize under)
    gmailOAuthClients, gmailOAuthClientsBusy, gmailOAuthClientError,
    gmailNewClientEmail, setGmailNewClientEmail,
    loadGmailOAuthClients, createGmailOAuthClient, removeGmailMailboxAndClient,
  }
}

export type WorkflowBots = ReturnType<typeof useWorkflowBots>
