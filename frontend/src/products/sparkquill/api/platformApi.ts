// FamilyApi against the AgentWorks platform: the SparkQuill product profiles
// (internal/sparkquillproduct) reached through the agent-profile chat routes,
// with the session event stream mapped back into the preview and result
// shapes the UI already understands. Turns themselves run through the shared
// ChatArea (PlatformChat / ChildPlatformChat); this adapter covers setup,
// workspace, history and connectors.
import type { ApiEngine, QuickCommand, VoiceStatus } from '../stores/types'
// Loaded on first use, not at module evaluation: the shared secrets client
// pulls in the whole shared services graph (src/services/api.ts,
// mcpConfigApi.ts, useMCPStore), which has a circular import that only
// resolves when entered from the AgentWorks app's own entry point. The
// desktop app is fine either way; a bare test or node consumer of this
// adapter is not.
const secrets = () => import('../../../api/secrets').then((m) => m.secretsApi)
import type {
  FamilyApi, FastMode, ModelInfo, PulseConfig, SetupState,
  StoredConversation,
  WhatsAppStatus, WhatsAppVoiceTranscription,
} from './familyApi'
import { messagesFromEvents, type PlatformEvent } from './platform/events'
import { quickCommandsFromProfile } from './platform/commands'
import { fetchSessionEvents, conversationToRestoredEvents, type RestorableConversation } from '../../../../shared/session'
import { FamilyWorkspace, documentsURL } from './platform/workspace'

export const PARENT_PROFILE = 'sparkquill'
export const CHILD_PROFILE = 'sparkquill-child'
const TOKEN_KEY = 'sparkquill.platform.token'
// Where WhatsApp attachments land, relative to the family root: the same
// inbox the attach button uploads to, so Quill files them the same way.
const WHATSAPP_UPLOAD_FOLDER = 'inbox'

export type PlatformApiOptions = {
  baseUrl: string
  /** Credentials for multi-user servers; a single-user server needs none. */
  login?: { username: string; password: string }
  /** Overrides the token store (node tests). */
  tokenStore?: { get(): string | null; set(token: string | null): void }
}

type Conversation = { sessionID: string; cursor: number }

const notYet = (what: string) => () => Promise.reject(new Error(`${what} is not available on the platform yet`))

export function createPlatformApi(options: PlatformApiOptions): FamilyApi {
  const base = options.baseUrl.replace(/\/+$/, '')
  const store = options.tokenStore ?? browserTokenStore()
  const conversations = new Map<string, Conversation>()

  // ---- auth ------------------------------------------------------------
  async function login(): Promise<string> {
    const res = await fetch(`${base}/api/auth/login`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: options.login ? JSON.stringify({ ...options.login, provider: 'local' }) : undefined,
    })
    if (!res.ok) throw new Error(`login failed: HTTP ${res.status}`)
    const data = (await res.json()) as { token?: string }
    if (!data.token) throw new Error('login returned no token')
    store.set(data.token)
    return data.token
  }

  let loginInFlight: Promise<string> | null = null
  async function token(): Promise<string> {
    const saved = store.get()
    if (saved) return saved
    // Several requests start together on boot; log in once, not once each.
    if (!loginInFlight) loginInFlight = login().finally(() => { loginInFlight = null })
    return loginInFlight
  }

  async function request<T>(method: string, path: string, body?: unknown, retry = true): Promise<T> {
    const form = typeof FormData !== 'undefined' && body instanceof FormData
    const res = await fetch(`${base}${path}`, {
      method,
      // Every read here is against live, frequently-edited family data (an
      // activity's page, family.json, a pinned page) at a stable URL with no
      // cache-busting query param — the browser's default HTTP cache can and
      // did serve a stale GET after Quill rewrote a file, with "Refresh"
      // appearing to do nothing since the request fired again but the
      // response came from cache. No read here should ever be stale.
      cache: 'no-store',
      headers: { Authorization: `Bearer ${await token()}`, ...(body === undefined || form ? {} : { 'Content-Type': 'application/json' }) },
      body: body === undefined ? undefined : form ? (body as FormData) : JSON.stringify(body),
    })
    if (res.status === 401 && retry) {
      store.set(null)
      return request<T>(method, path, body, false)
    }
    const text = await res.text()
    let parsed: unknown = null
    try { parsed = text ? JSON.parse(text) : null } catch { parsed = null }
    if (!res.ok) {
      const message = (parsed as { error?: string } | null)?.error ?? text ?? `HTTP ${res.status}`
      throw new Error(message)
    }
    return parsed as T
  }

  // ---- conversations -----------------------------------------------------
  function conversationKeyFor(profile: string, activityDir?: string): string {
    if (profile === PARENT_PROFILE) return ''
    const slug = String(activityDir ?? '').replace(/\/+$/, '').split('/').pop() ?? ''
    if (!slug) throw new Error('the child needs an activity to talk in')
    return slug
  }

  const sessionClient = { baseUrl: base, token }

  async function conversation(profile: string, key: string): Promise<Conversation> {
    const cacheKey = `${profile}/${key}`
    const cached = conversations.get(cacheKey)
    if (cached) return cached
    const resolved = await request<{ session_id: string }>('POST', `/api/agent-profiles/${profile}/conversation`, key ? { conversation_key: key } : {})
    const batch = await fetchSessionEvents(sessionClient, resolved.session_id, 0).catch(() => null)
    const cursor = batch && typeof batch.last_processed_index === 'number' && batch.last_processed_index >= 0 ? batch.last_processed_index : 0
    const conv = { sessionID: resolved.session_id, cursor }
    conversations.set(cacheKey, conv)
    return conv
  }

  /** Rebuilds a transcript from the session's event history. */
  // The persisted chat history is the durable record (the live event store is
  // in memory and empty after a server restart); it is rebuilt into events by
  // the platform's own restore converter and then read like a live turn.
  async function history(profile: string, key: string): Promise<StoredConversation | null> {
    const conv = await conversation(profile, key)
    let stored: RestorableConversation | null = null
    try {
      stored = await request<RestorableConversation>('GET', `/api/chat-history/sessions/${encodeURIComponent(conv.sessionID)}`)
    } catch (err) {
      if (!/HTTP 404/.test(String(err))) throw err
    }
    const events: PlatformEvent[] = stored?.conversation_history?.length
      ? (conversationToRestoredEvents({ ...stored, session_id: conv.sessionID }) as PlatformEvent[])
      : (await fetchSessionEvents(sessionClient, conv.sessionID, 0)).events ?? []
    return { messages: messagesFromEvents(events, conv.sessionID) }
  }

  // ---- workspace -----------------------------------------------------------
  const ws = new FamilyWorkspace(request)

  // ---- check-in (the product schedule) --------------------------------------
  const CHECKIN_JOB_ID = `product:${PARENT_PROFILE}:pulse`
  type ScheduleJob = { enabled?: boolean; last_run_at?: string | null; last_session_id?: string | null }
  type ProfileSchedules = { schedules?: { id?: string; enabled?: boolean; cadence_hours?: number }[] }
  async function checkinConfig(): Promise<PulseConfig> {
    const [job, family, profile] = await Promise.all([
      request<ScheduleJob>('GET', `/api/scheduler/jobs/${encodeURIComponent(CHECKIN_JOB_ID)}`).catch(() => null),
      ws.readFamily(),
      request<ProfileSchedules>('GET', `/api/agent-profiles/${PARENT_PROFILE}`).catch(() => null),
    ])
    const declared = (profile?.schedules ?? []).find((s) => s.id === 'pulse')
    return {
      enabled: job?.enabled ?? declared?.enabled ?? false,
      cadence_hours: declared?.cadence_hours ?? 24,
      last_run_at: job?.last_run_at ?? undefined,
      last_session_id: job?.last_session_id ?? undefined,
      watch_sites: family.watch_sites ?? [],
      preferred_hour: 8,
      preferred_hour_set: false,
    }
  }
  async function resetCheckinHistory(): Promise<void> {
    await request('POST', `/api/scheduler/jobs/${encodeURIComponent(CHECKIN_JOB_ID)}/reset-history`, {})
  }
  const readFile = (path: string) => ws.readFile(path)

  async function setup(): Promise<SetupState> {
    const state = await ws.readFamily()
    const engineSet = !!state.engine
    const childDone = !!(state.child && state.child.name)
    const pinSet = !!state.pin_hash
    return {
      engine: state.engine,
      model: state.model,
      child: state.child ?? null,
      parent_label: state.parent_label,
      pin_set: pinSet,
      setup_complete: engineSet && childDone && pinSet,
      next_step: !engineSet ? 'engine' : !childDone ? 'child' : !pinSet ? 'pin' : 'done',
    }
  }

  async function commands(): Promise<{ parent: QuickCommand[]; child: QuickCommand[] }> {
    const [parent, child] = await Promise.all([
      request<{ commands?: Array<Record<string, unknown>> }>('GET', `/api/agent-profiles/${PARENT_PROFILE}`),
      request<{ commands?: Array<Record<string, unknown>> }>('GET', `/api/agent-profiles/${CHILD_PROFILE}`),
    ])
    return { parent: quickCommandsFromProfile(parent), child: quickCommandsFromProfile(child) }
  }

  type ProviderOption = { id: string; label: string; provider: string; model_id: string; default?: boolean }
  async function declaredProviderOptions(): Promise<ProviderOption[]> {
    const profile = await request<{ runtime?: { provider_options?: ProviderOption[] } }>('GET', `/api/agent-profiles/${PARENT_PROFILE}`)
    return profile.runtime?.provider_options ?? []
  }

  type ProviderManifestEntry = {
    id: string
    runtime_command?: string
    runtime_available?: boolean
    auth_configured: boolean
    usable: boolean
    setup_hint?: string
    deprecated?: boolean
  }

  // Only the profile's own curated provider_options are offered — never the
  // full AgentWorks provider catalog — but their real readiness (installed?
  // logged in?) comes from the platform's own provider manifest, the same one
  // AgentWorks' own model picker reads.
  async function engines(): Promise<ApiEngine[]> {
    const [options, manifest] = await Promise.all([
      declaredProviderOptions(),
      request<{ providers?: ProviderManifestEntry[] }>('GET', '/api/llm-config/providers').catch(() => ({ providers: [] })),
    ])
    const byID = new Map((manifest.providers ?? []).map((p) => [p.id, p]))
    return options.map((o) => {
      const entry = byID.get(o.provider)
      return {
        id: o.id,
        name: o.label,
        runtime_command: entry?.runtime_command ?? '',
        runtime_available: entry?.runtime_available ?? false,
        auth_configured: entry?.auth_configured ?? false,
        usable: entry?.usable ?? false,
        setup_hint: entry?.setup_hint,
        deprecated: entry?.deprecated,
      } as ApiEngine
    })
  }

  async function validateEngine(engineID: string): Promise<{ valid: boolean; message?: string }> {
    const options = await declaredProviderOptions()
    const option = options.find((o) => o.id === engineID)
    if (!option) return { valid: false, message: 'Unknown learning helper.' }
    const res = await request<{ valid: boolean; message?: string; error?: string }>('POST', '/api/llm-config/validate-key', { provider: option.provider })
    return { valid: res.valid, message: res.message ?? res.error }
  }

  async function selectEngine(engineID: string, model?: string): Promise<void> {
    await ws.saveEngine(engineID, model)
  }

  const api: FamilyApi = {
    baseUrl: base,

    setup,
    engines,
    commands,
    // Re-store even an existing token so every place that mirrors it (the
    // shared chat's own auth key) sees it, not only a fresh login.
    ensureSession: async () => { store.set(await token()) },
    validateEngine,
    selectEngine,
    saveChild: (child) => ws.saveChild(child),
    setPin: (pin) => ws.setPin(pin),
    verifyPin: (pin) => ws.verifyPin(pin),


    childActivity: () => ws.currentActivity(),
    handoff: (dir, resume) => ws.handoff(dir, resume),
    resetChildConversation: async (activityDir) => {
      const key = conversationKeyFor(CHILD_PROFILE, activityDir)
      await request('POST', `/api/agent-profiles/${CHILD_PROFILE}/conversation/new`, { conversation_key: key })
      conversations.delete(`${CHILD_PROFILE}/${key}`)
    },
    loadChildConversation: (activityDir) => history(CHILD_PROFILE, conversationKeyFor(CHILD_PROFILE, activityDir)),

    tree: () => ws.tree(),
    readFile,
    // Browser elements cannot send headers, so the token rides in the URL
    // (the server accepts ?token= everywhere). /raw serves inline bytes with
    // range support, which is what <img>, <video> and the PDF frame need.
    rawUrl: (path, opts) => `${base}${documentsURL(path, '/raw')}?${opts?.download ? 'download=true&' : ''}token=${encodeURIComponent(store.get() ?? '')}`,
    // The app ships its own static assets (public/lib); they live on the app's origin.
    assetUrl: (relPath) => (typeof window !== 'undefined' ? `${window.location.origin}/${relPath.replace(/^\/+/, '')}` : `/${relPath.replace(/^\/+/, '')}`),
    upload: async (file, scope) => {
      let folder = 'inbox'
      if (scope === 'child') {
        const current = await ws.currentActivity()
        if (!current) return { name: file.name, error: 'no activity is currently active' }
        folder = current.dir
      }
      return ws.upload(file, folder)
    },
    saveState: (key, data) => ws.writeJSON(ws.stateFile(key), { key, data }),
    loadState: async (key) => (await ws.readJSON<{ data?: unknown }>(ws.stateFile(key)))?.data ?? null,
    activities: () => ws.activities(),
    deleteActivities: async (dirs) => {
      for (const dir of dirs) await ws.deleteFolder(dir).catch(() => undefined)
    },

    models: async () => null as ModelInfo | null,
    saveModel: async () => {},
    fastMode: async () => ({ enabled: false, child_enabled: false }) as FastMode,
    saveFastMode: async () => {},
    // The platform's per-user secret store: the value is encrypted server-side
    // first, only the name is ever listed back. This is the same store the
    // agent's set_user_secret / list_secrets tools use, so Settings and chat
    // see one list.
    secrets: async () => (await (await secrets()).listStoredSecrets()).map((s) => s.name),
    saveSecret: async (name, value) => {
      const { encrypted } = await (await secrets()).encrypt(value)
      await (await secrets()).storeSecret(name, encrypted)
      return (await (await secrets()).listStoredSecrets()).map((s) => s.name)
    },
    deleteSecret: async (name) => {
      await (await secrets()).deleteStoredSecret(name)
      return (await (await secrets()).listStoredSecrets()).map((s) => s.name)
    },
    // The platform runs one shared speech engine for every product; Settings
    // shows it as a single tier in the family server's catalog shape.
    voiceStatus: async () => {
      const p = await request<{ available?: boolean; installed?: boolean; downloading?: boolean; got_bytes?: number; total_bytes?: number; loading?: boolean; ready?: boolean; size_mb?: number }>('GET', '/api/voice/status')
      const tier = {
        id: 'platform',
        label: 'Built-in voice',
        description: 'Runs inside SparkQuill’s server, shared by everything you use here. Nothing to install.',
        size_mb: p.size_mb,
        languages: 'English',
        available: p.available !== false,
        installed: p.installed !== false,
        warm: p.ready === true,
        installing: p.downloading === true || p.loading === true,
        got_bytes: p.got_bytes ?? 0,
        total_bytes: p.total_bytes ?? 0,
        can_install: p.installed === false && p.available !== false,
        can_remove: false,
      }
      return { hardware: { arch: '', is_apple_silicon: false, total_ram_bytes: 0 }, stt_tiers: [tier] } as VoiceStatus
    },
    browserStatus: async () => ({ cli_installed: false }),

    // The platform's shared WhatsApp connector: one pairing per account, and
    // the parent profile is its default destination, so a message in the
    // parent's own "message yourself" chat reaches Quill with no @slug. The
    // QR request names the profile (and where photos land — the inbox the
    // attach button already uses, so process-file files them); the status
    // poll repairs a pairing made before the default existed.
    whatsappStatus: async () => {
      const s = await request<{
        paired?: boolean; connected?: boolean; own_jid?: string
        qr_available?: boolean; qr_expires_at?: string
        default_profile_id?: string
        devices?: { slot: string; paired: boolean; connected: boolean; own_jid?: string }[]
        next_device?: { slot?: string; qr_available?: boolean; qr_expires_at?: string }
      }>('GET', '/api/whatsapp/status')
      if (s.paired && s.default_profile_id !== PARENT_PROFILE) {
        await request('PUT', '/api/whatsapp/default-profile', { profile_id: PARENT_PROFILE, upload_folder: WHATSAPP_UPLOAD_FOLDER }).catch(() => undefined)
      }
      // Every linked phone (one per parent); the QR is always for the phone
      // a scan would pair next, so "add another parent" needs no extra step.
      const devices = s.devices ?? (s.paired && s.own_jid ? [{ slot: '', paired: true, connected: s.connected === true, own_jid: s.own_jid }] : [])
      const pairing = s.next_device ?? { qr_available: s.qr_available, qr_expires_at: s.qr_expires_at }
      return {
        accounts: devices.filter((d) => d.paired && d.own_jid).map((d) => ({ jid: (d.own_jid ?? '').replace(/@.*$/, ''), connected: d.connected })),
        pairing: { qr_available: pairing.qr_available === true, qr_expires_at: pairing.qr_expires_at },
      }
    },
    whatsappPairImageUrl: (nonce) => {
      const params = new URLSearchParams({ profile_id: PARENT_PROFILE, upload_folder: WHATSAPP_UPLOAD_FOLDER, device: 'next', size: '384', n: String(nonce) })
      // An <img> cannot send a header; the platform accepts the token as a
      // query parameter, the same way rawUrl does.
      params.set('token', store.get() ?? '')
      return `${base}/api/whatsapp/pair?${params.toString()}`
    },
    whatsappUnpair: async (jid) => { await request('DELETE', `/api/whatsapp/session?jid=${encodeURIComponent(jid)}`) },
    // Voice notes are transcribed by the platform's own STT for every
    // channel; there is no per-product toggle to offer.
    whatsappVoice: notYet('WhatsApp voice transcription') as (enabled: boolean) => Promise<WhatsAppVoiceTranscription>,
    // The check-in is the product's `pulse` schedule, run by the platform
    // scheduler: a fixed message sequence on a cadence from the manifest.
    // The parent can switch it on or off and run it now; the cadence is the
    // product's. Watched websites are family state the prompt reads.
    pulseConfig: checkinConfig,
    resetCheckinHistory,
    savePulseConfig: async (patch) => {
      if (patch.enabled !== undefined) {
        await request('POST', `/api/scheduler/jobs/${encodeURIComponent(CHECKIN_JOB_ID)}/${patch.enabled ? 'enable' : 'disable'}`, {})
      }
      if (patch.watch_sites) {
        const family = await ws.readFamily()
        await ws.writeJSON('family.json', { ...family, watch_sites: patch.watch_sites.map((s) => s.trim()).filter(Boolean) })
      }
      return checkinConfig()
    },
    runPulse: async () => {
      await request('POST', `/api/scheduler/jobs/${encodeURIComponent(CHECKIN_JOB_ID)}/trigger`, {})
      return { ok: true }
    },
  }
  return api
}

function browserTokenStore() {
  return {
    get: () => { try { return localStorage.getItem(TOKEN_KEY) } catch { return null } },
    set: (t: string | null) => { try { if (t) localStorage.setItem(TOKEN_KEY, t); else localStorage.removeItem(TOKEN_KEY) } catch { /* ignore */ } },
  }
}

