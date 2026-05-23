import { useState, useEffect, useRef, useCallback } from 'react'
import {
  X, CheckCircle, AlertCircle, Loader2, Eye, EyeOff, AlertTriangle,
  Bot, User, Send, RotateCcw, Plus, MessageSquare, Layers, Play, Trash2,
  Phone,
} from 'lucide-react'
import { Button } from '../ui/Button'
import { Card } from '../ui/Card'
import { MarkdownRenderer } from '../ui/MarkdownRenderer'
import ModalPortal from '../ui/ModalPortal'
import { agentApi } from '../../services/api'
import { useLLMStore } from '../../stores'
import type { SlackConfig, SlackConfigRequest, SlackTestResponse, SimulatorThreadInfo, DiscoveredWorkflow, ChannelRoute } from '../../services/api-types'

interface BotConnectorModalProps {
  isOpen: boolean
  onClose: () => void
}

type Section = 'slack' | 'whatsapp' | 'simulate'
// Bot connectors (Slack/WhatsApp) are user-facing runtime surfaces.
// They always run in "run" mode — never workshop — so plan/config/eval
// mutations can't leak through a bot channel. Persisted route configs
// from before this restriction (which may have "builder" / "optimizer" /
// "workshop") are normalized to "run" at load time.
type WorkflowRouteMode = 'run'

const normalizeWorkflowRouteMode = (_mode?: string): WorkflowRouteMode => {
  // Bot routes always normalize to "run". Any legacy value
  // ("builder" / "optimizer" / "reporting" / "workshop" / "ask" / ...) is
  // folded to "run" so bot channels never expose workshop write tools.
  return 'run'
}

// Shape of GET /api/whatsapp/status. enabled = connector started at server
// startup; paired = device identity stored; connected = live WS.
interface WhatsAppStatus {
  enabled: boolean
  paired: boolean
  connected: boolean
  own_jid: string
  qr_available: boolean
  qr_expires_at?: string
  link_code?: string
  link_code_expires_at?: string
  bound_chat_count?: number
  owner_user_id?: string
  owner_email?: string
  owner_username?: string
  owner_paired_at?: string
}
type SimStatus = 'idle' | 'sending' | 'running' | 'completed' | 'error'
interface ChatMessage { id: string; text: string; is_bot: boolean; timestamp: string }

export default function BotConnectorModal({ isOpen, onClose }: BotConnectorModalProps) {
  const [activeSection, setActiveSection] = useState<Section>('slack')

  // ── Allowed emails (Slack-specific) ──────────────────────────────────────
  const [allowedEmails, setAllowedEmails] = useState('')
  const [emailsDirty, setEmailsDirty] = useState(false)
  const [emailsSaving, setEmailsSaving] = useState(false)
  const [emailsSaved, setEmailsSaved] = useState(false)

  // ── Slack config ──────────────────────────────────────────────────────────
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

  // ── Channel routing ───────────────────────────────────────────────────────
  const [workflows, setWorkflows] = useState<DiscoveredWorkflow[]>([])
  const [newChannelID, setNewChannelID] = useState('')
  const [newWorkflowID, setNewWorkflowID] = useState('')
  const [newWorkshopMode, setNewWorkshopMode] = useState<'' | 'run'>('')
  const [newSendFullDetails, setNewSendFullDetails] = useState(false)

  // ── WhatsApp ──────────────────────────────────────────────────────────────
  const [waStatus, setWaStatus] = useState<WhatsAppStatus | null>(null)
  const [waError, setWaError] = useState<string | null>(null)
  // qrBust changes to force the QR fetch to reload. Bumped when polling
  // detects the QR has rotated or the pairing state transitions.
  const [qrBust, setQrBust] = useState<number>(() => Date.now())
  const [qrImageURL, setQrImageURL] = useState<string | null>(null)
  const [qrLoading, setQrLoading] = useState(false)
  const [qrError, setQrError] = useState<string | null>(null)
  const [unpairConfirm, setUnpairConfirm] = useState(false)
  const [unpairing, setUnpairing] = useState(false)
  const waPollingRef = useRef<ReturnType<typeof setInterval> | null>(null)

  // Routing editor state — a plain array (rather than an object keyed by slug)
  // so new/empty rows can coexist without slug collisions while the user types.
  // Converted to object shape on save.
  type WaRouteRow = { slug: string; workflow_id: string; workshop_mode: WorkflowRouteMode; send_full_details?: boolean }
  const [waRoutes, setWaRoutes] = useState<WaRouteRow[]>([])
  const [waRoutesOriginal, setWaRoutesOriginal] = useState<WaRouteRow[]>([])
  const [waRoutesSaving, setWaRoutesSaving] = useState(false)
  const [waRoutesError, setWaRoutesError] = useState<string | null>(null)
  const [waRoutesSaved, setWaRoutesSaved] = useState(false)

  // ── Simulate ──────────────────────────────────────────────────────────────
  const { delegationTierConfig } = useLLMStore()
  const [input, setInput] = useState('')
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [simStatus, setSimStatus] = useState<SimStatus>('idle')
  const [sending, setSending] = useState(false)
  const [errorMsg, setErrorMsg] = useState('')
  const [threadMode, setThreadMode] = useState<'threaded' | 'non-threaded'>('non-threaded')
  const [threads, setThreads] = useState<SimulatorThreadInfo[]>([])
  const [activeThreadId, setActiveThreadId] = useState<string | null>(null)
  const [serverTierConfig, setServerTierConfig] = useState<Record<string, { provider?: string; model_id?: string }> | null>(null)
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const pollingRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const threadOffsetRef = useRef(0)

  // ── Load ──────────────────────────────────────────────────────────────────
  const loadEmails = useCallback(async () => {
    try {
      const cfg = await agentApi.getSimulatorConfig()
      if ((cfg as Record<string, unknown>).allowed_emails) {
        setAllowedEmails(((cfg as Record<string, unknown>).allowed_emails as string[]).join(', '))
      }
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

  useEffect(() => {
    if (!isOpen) return
    setEmailsDirty(false)
    setEmailsSaved(false)
    loadEmails()
    loadSlack()
    agentApi.listWorkflowManifests().then(data => setWorkflows(data.workflows || [])).catch(() => {})

    agentApi.getSimulatorMode().then(data => {
      setThreadMode(data.threaded ? 'threaded' : 'non-threaded')
    }).catch(() => {})

    agentApi.listSimulatorThreads().then(data => {
      setThreads(data.threads || [])
    }).catch(() => {})

    const syncTiers = async () => {
      if (delegationTierConfig && Object.keys(delegationTierConfig).length > 0) {
        const llmState = useLLMStore.getState()
        const providerConfigs: Record<string, { api_key?: string }> = {
          openai: llmState.openaiConfig,
          anthropic: llmState.anthropicConfig, vertex: llmState.vertexConfig,
          bedrock: llmState.bedrockConfig, azure: llmState.azureConfig,
        }
        const providerKeys: Record<string, string> = {}
        const tierCfg = delegationTierConfig as Record<string, { provider?: string }>
        for (const tier of ['high', 'medium', 'low']) {
          const provider = tierCfg[tier]?.provider
          if (provider && providerConfigs[provider]?.api_key && !providerKeys[provider]) {
            providerKeys[provider] = providerConfigs[provider].api_key!
          }
        }
        const customTiers = (delegationTierConfig as { custom?: Record<string, { provider?: string }> }).custom
        if (customTiers) {
          for (const slug of Object.keys(customTiers)) {
            const provider = customTiers[slug]?.provider
            if (provider && providerConfigs[provider]?.api_key && !providerKeys[provider]) {
              providerKeys[provider] = providerConfigs[provider].api_key!
            }
          }
        }
        await agentApi.saveDelegationTierConfig(
          delegationTierConfig as Record<string, unknown>,
          Object.keys(providerKeys).length > 0 ? providerKeys : undefined
        ).catch(() => {})
      }
      try {
        const cfg = await agentApi.getDelegationTierConfig()
        if (cfg && typeof cfg === 'object' && Object.keys(cfg).length > 0) {
          setServerTierConfig(cfg as Record<string, { provider?: string; model_id?: string }>)
        } else {
          setServerTierConfig(null)
        }
      } catch { setServerTierConfig(null) }
    }
    syncTiers()
  }, [isOpen, delegationTierConfig, loadEmails, loadSlack])

  // ── WhatsApp: status polling ──────────────────────────────────────────────
  // When the WhatsApp tab is active and not yet paired, poll /status every 3s
  // so a fresh QR (rotating every ~20s on the server) plus the transition to
  // "paired" shows up without the user having to refresh. Polls only while
  // the modal is open and the tab is selected — nothing runs in the
  // background otherwise.
  useEffect(() => {
    if (!isOpen || activeSection !== 'whatsapp') {
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
        // If the QR rotated (new expiry) or pairing just completed, bust the
        // <img> cache so the browser re-fetches the latest PNG.
        if (s.qr_expires_at !== lastExpires) {
          lastExpires = s.qr_expires_at
          setQrBust(Date.now())
        }
      } catch (err) {
        if (cancelled) return
        const msg = err instanceof Error ? err.message : String(err)
        setWaError(msg)
      }
    }
    tick()
    waPollingRef.current = setInterval(tick, 3000)

    // One-shot fetch of the routing map when the tab opens. The editor is not
    // polled — we only refresh after explicit save actions.
    agentApi.getWhatsAppRouting().then(data => {
      if (cancelled) return
      const rows = Object.entries(data.routing || {}).map(([slug, r]) => ({
        slug,
        workflow_id: r.workflow_id,
        workshop_mode: normalizeWorkflowRouteMode(r.workshop_mode),
        send_full_details: !!r.send_full_details,
      }))
      setWaRoutes(rows)
      setWaRoutesOriginal(rows)
      setWaRoutesError(null)
    }).catch(err => {
      if (cancelled) return
      setWaRoutesError(err instanceof Error ? err.message : String(err))
    })
    return () => {
      cancelled = true
      if (waPollingRef.current) {
        clearInterval(waPollingRef.current)
        waPollingRef.current = null
      }
    }
  }, [isOpen, activeSection])

  useEffect(() => {
    if (!isOpen || activeSection !== 'whatsapp' || !waStatus?.enabled || waStatus.paired || !waStatus.qr_available) {
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
  }, [isOpen, activeSection, waStatus?.enabled, waStatus?.paired, waStatus?.qr_available, qrBust])

  useEffect(() => {
    return () => {
      if (qrImageURL) URL.revokeObjectURL(qrImageURL)
    }
  }, [qrImageURL])

  // ── Simulate: auto-scroll ─────────────────────────────────────────────────
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages])

  // ── Simulate: polling ─────────────────────────────────────────────────────
  useEffect(() => {
    if (simStatus !== 'running' || !activeThreadId) return
    let cancelled = false
    let lastCount = threadOffsetRef.current
    const poll = async () => {
      if (cancelled) return
      try {
        const data = await agentApi.getSimulatorMessages(activeThreadId, lastCount)
        if (cancelled) return
        if (data.messages && data.messages.length > 0) {
          const newMsgs: ChatMessage[] = data.messages
            .map(m => ({ id: m.id, text: m.text, is_bot: m.is_bot, timestamp: m.timestamp }))
          setMessages(prev => {
            const existingIds = new Set(prev.map(m => m.id))
            const unique = newMsgs.filter(m => !existingIds.has(m.id))
            return unique.length > 0 ? [...prev, ...unique] : prev
          })
          lastCount = data.total
          const lastBot = [...data.messages].reverse().find(m => m.is_bot)
          if (lastBot) {
            if (lastBot.text.includes('Session completed')) setSimStatus('completed')
            else if (lastBot.text.includes('Session failed') || lastBot.text.includes('Cancelled')) setSimStatus('error')
          }
        }
      } catch { /* ignore */ }
      if (!cancelled) pollingRef.current = setTimeout(poll, 2000)
    }
    pollingRef.current = setTimeout(poll, 2000)
    return () => { cancelled = true; if (pollingRef.current) clearTimeout(pollingRef.current) }
  }, [simStatus, activeThreadId])

  // ── Slack handlers ────────────────────────────────────────────────────────
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
        channel_routing: slackConfig.channel_routing || {},
      }
      await agentApi.updateSlackFeedbackConfig(request)
      setSlackSuccess('Saved successfully!')
      await loadSlack()
      setTimeout(() => setSlackSuccess(null), 3000)
    } catch (err) {
      setSlackError(err instanceof Error ? err.message : 'Failed to save Slack configuration')
    } finally { setSlackSaving(false) }
  }

  // handleUnpairWhatsApp drops the paired phone, restarts the service, and
  // refreshes local status. Wipes the SQLite session DB on the server so the
  // user can pair a different account. Two-step confirmation via
  // unpairConfirm prevents accidental clicks.
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
      // Kick polling to reflect the new "unpaired" state immediately.
      try {
        const s = await agentApi.getWhatsAppStatus()
        setWaStatus(s)
      } catch { /* polling tick will retry */ }
      setQrBust(Date.now())
    } catch (err) {
      setWaError(err instanceof Error ? err.message : 'Failed to unpair')
    } finally {
      setUnpairing(false)
    }
  }

  // handleSaveWaRoutes persists the slug → workflow map. Empty slugs are
  // dropped (skipped rather than error) to let users clear a row by blanking
  // it; everything else is validated server-side (slug charset, workflow_id
  // non-empty) and any error is surfaced.
  const handleSaveWaRoutes = async () => {
    try {
      setWaRoutesSaving(true)
      setWaRoutesError(null)
      const payload: Record<string, { workflow_id: string; workshop_mode?: string; workspace_path?: string; send_full_details?: boolean }> = {}
      for (const row of waRoutes) {
        const slug = row.slug.trim().toLowerCase()
        if (!slug) continue
        if (!row.workflow_id) {
          throw new Error(`Row "@${slug}" has no workflow selected`)
        }
        // Look up the workflow's workspace_path from the discovered list so
        // the backend can read its manifest for workshop_mode fallback.
        const wf = workflows.find(w => w.manifest.id === row.workflow_id)
        payload[slug] = {
          workflow_id: row.workflow_id,
          workspace_path: wf?.workspace_path || '',
          workshop_mode: row.workshop_mode,
        }
        if (row.send_full_details) payload[slug].send_full_details = true
      }
      const data = await agentApi.updateWhatsAppRouting(payload)
      const rows = Object.entries(data.routing || {}).map(([slug, r]) => ({
        slug,
        workflow_id: r.workflow_id,
        workshop_mode: normalizeWorkflowRouteMode(r.workshop_mode),
        send_full_details: !!r.send_full_details,
      }))
      setWaRoutes(rows)
      setWaRoutesOriginal(rows)
      setWaRoutesSaved(true)
      setTimeout(() => setWaRoutesSaved(false), 2500)
    } catch (err) {
      setWaRoutesError(err instanceof Error ? err.message : 'Failed to save routing')
    } finally {
      setWaRoutesSaving(false)
    }
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
      // This ensures you're testing what the server will actually use at runtime.
      const result = await agentApi.testSlackConnection()
      setTestResult(result)
      if (result.success && result.test_id) { setPollingForReply(true); pollForTestReply(result.test_id) }
    } catch (err) {
      setTestResult({ success: false, message: err instanceof Error ? err.message : 'Connection test failed' })
    } finally { setSlackTesting(false) }
  }

  // ── Simulate handlers ─────────────────────────────────────────────────────
  const handleSend = async () => {
    if (!input.trim() || sending) return
    const text = input.trim()
    setInput('')
    setSending(true)
    setSimStatus('sending')
    setErrorMsg('')
    setMessages(prev => [...prev, { id: 'user_' + Date.now(), text, is_bot: false, timestamp: new Date().toISOString() }])
    try {
      const result = await agentApi.simulateBotMessage(text, activeThreadId || undefined)
      if (result.thread_offset) threadOffsetRef.current = result.thread_offset
      setActiveThreadId(result.thread_id)
      if (result.type === 'follow_up') {
        setSimStatus('running')
      } else if (result.type === 'conversation' && result.response) {
        setMessages(prev => [...prev, { id: 'bot_' + Date.now(), text: result.response || '', is_bot: true, timestamp: new Date().toISOString() }])
        setSimStatus('idle')
      }
      agentApi.listSimulatorThreads().then(data => setThreads(data.threads || [])).catch(() => {})
    } catch (err) {
      setErrorMsg(err instanceof Error ? err.message : 'Failed to send message.')
      setSimStatus('error')
    } finally { setSending(false) }
  }

  const switchToThread = useCallback(async (threadId: string) => {
    if (pollingRef.current) clearTimeout(pollingRef.current)
    setActiveThreadId(threadId)
    setMessages([])
    setSimStatus('idle')
    setErrorMsg('')
    try {
      const data = await agentApi.getSimulatorMessages(threadId, 0)
      if (data.messages?.length > 0) {
        setMessages(
          data.messages
            .map(m => ({ id: m.id, text: m.text, is_bot: m.is_bot, timestamp: m.timestamp }))
        )
      }
    } catch { /* ignore */ }
  }, [])

  const handleNewThread = () => {
    if (pollingRef.current) clearTimeout(pollingRef.current)
    setActiveThreadId(null); setMessages([]); setSimStatus('idle'); setErrorMsg(''); threadOffsetRef.current = 0
  }

  const handleReset = async () => {
    if (activeThreadId) { try { await agentApi.clearSimulatorThread(activeThreadId) } catch { /* ignore */ } }
    if (pollingRef.current) clearTimeout(pollingRef.current)
    setMessages([]); setActiveThreadId(null); setSimStatus('idle'); setInput(''); setErrorMsg(''); threadOffsetRef.current = 0
  }

  const handleModeChange = async (newMode: 'threaded' | 'non-threaded') => {
    setThreadMode(newMode)
    try { await agentApi.setSimulatorMode(newMode === 'threaded') } catch { setThreadMode(threadMode) }
  }

  const handleClose = () => {
    if (pollingRef.current) clearTimeout(pollingRef.current)
    onClose()
  }

  if (!isOpen) return null

  const slackHasChanges = JSON.stringify(slackConfig) !== JSON.stringify(slackOriginal)

  const simStatusConfig: Record<SimStatus, { label: string; color: string }> = {
    idle: { label: 'Ready', color: 'bg-gray-500' },
    sending: { label: 'Starting...', color: 'bg-blue-500 animate-pulse' },
    running: { label: 'Running', color: 'bg-green-500 animate-pulse' },
    completed: { label: 'Completed', color: 'bg-green-600' },
    error: { label: 'Error / Cancelled', color: 'bg-red-500' },
  }
  const formatTime = (ts: string) => { try { return new Date(ts).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) } catch { return '' } }

  const isSimulate = activeSection === 'simulate'

  return (
    <ModalPortal>
    <div className="fixed inset-0 z-[9999] flex items-center justify-center bg-black/50 backdrop-blur-sm p-2 sm:p-4">
      <div className={`bg-background border border-border rounded-lg shadow-xl w-full flex flex-col overflow-hidden ${isSimulate ? 'max-w-5xl h-[calc(100dvh-1rem)] sm:h-[85vh]' : 'max-w-4xl max-h-[calc(100dvh-1rem)] sm:max-h-[90vh]'}`}>

        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-border flex-shrink-0">
          <h2 className="text-base font-semibold flex items-center gap-2">
            <Bot className="w-5 h-5 text-primary" />
            Bot Connector
          </h2>
          <button onClick={handleClose} className="p-1.5 text-muted-foreground hover:text-foreground transition-colors rounded">
            <X className="w-4 h-4" />
          </button>
        </div>

        {/* Body: left nav + right content */}
        <div className="flex flex-1 overflow-hidden">

          {/* Left nav */}
          <div className="w-44 border-r border-border flex flex-col flex-shrink-0">
            <div className="px-3 py-2.5 border-b border-border">
              <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">Channels</span>
            </div>
            <div className="flex-1 overflow-y-auto py-1">
              {/* Slack channel */}
              <button
                onClick={() => setActiveSection('slack')}
                className={`w-full flex items-center gap-2.5 px-3 py-2 text-sm transition-colors ${
                  activeSection === 'slack'
                    ? 'bg-accent text-accent-foreground font-medium'
                    : 'text-muted-foreground hover:bg-muted hover:text-foreground'
                }`}
              >
                <MessageSquare className="w-4 h-4 flex-shrink-0" />
                <span className="flex-1 text-left truncate">Slack</span>
                {slackConfig.enabled && (
                  <span className="w-1.5 h-1.5 rounded-full bg-green-500 flex-shrink-0" />
                )}
              </button>

              {/* WhatsApp channel */}
              <button
                onClick={() => setActiveSection('whatsapp')}
                className={`w-full flex items-center gap-2.5 px-3 py-2 text-sm transition-colors ${
                  activeSection === 'whatsapp'
                    ? 'bg-accent text-accent-foreground font-medium'
                    : 'text-muted-foreground hover:bg-muted hover:text-foreground'
                }`}
              >
                <Phone className="w-4 h-4 flex-shrink-0" />
                <span className="flex-1 text-left truncate">WhatsApp</span>
                {waStatus?.connected && (
                  <span className="w-1.5 h-1.5 rounded-full bg-green-500 flex-shrink-0" />
                )}
              </button>
            </div>

            {/* Simulate — global, at bottom */}
            <div className="border-t border-border py-1">
              <button
                onClick={() => setActiveSection('simulate')}
                className={`w-full flex items-center gap-2.5 px-3 py-2 text-sm transition-colors ${
                  activeSection === 'simulate'
                    ? 'bg-accent text-accent-foreground font-medium'
                    : 'text-muted-foreground hover:bg-muted hover:text-foreground'
                }`}
              >
                <Play className="w-4 h-4 flex-shrink-0" />
                <span className="flex-1 text-left">Simulate</span>
              </button>
            </div>
          </div>

          {/* Right content */}
          <div className="flex-1 flex flex-col overflow-hidden">

            {/* ── Slack Section ── */}
            {activeSection === 'slack' && (
              <>
                <div className="flex-1 overflow-y-auto p-4 space-y-4">
                  {slackLoading ? (
                    <div className="flex items-center justify-center py-12"><Loader2 className="w-8 h-8 animate-spin text-primary" /></div>
                  ) : (
                    <>
                      {slackError && (
                        <div className="p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg flex items-start gap-2">
                          <AlertCircle className="w-4 h-4 text-red-600 dark:text-red-400 flex-shrink-0 mt-0.5" />
                          <p className="text-sm text-red-700 dark:text-red-300">{slackError}</p>
                        </div>
                      )}
                      {slackSuccess && (
                        <div className="p-3 bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-lg flex items-start gap-2">
                          <CheckCircle className="w-4 h-4 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                          <p className="text-sm text-green-700 dark:text-green-300">{slackSuccess}</p>
                        </div>
                      )}

                      {/* Enable Slack */}
                      <Card className="p-4">
                        <div className="flex items-center justify-between">
                          <div>
                            <h3 className="text-sm font-medium text-foreground">Enable Slack</h3>
                            <p className="text-xs text-muted-foreground mt-0.5">Send notifications and receive @mentions via Slack</p>
                          </div>
                          <label className="relative inline-flex items-center cursor-pointer">
                            <input type="checkbox" checked={slackConfig.enabled} onChange={e => setSlackConfig({ ...slackConfig, enabled: e.target.checked })} className="sr-only peer" />
                            <div className="w-11 h-6 bg-gray-200 peer-focus:outline-none rounded-full peer dark:bg-gray-700 peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all dark:border-gray-600 peer-checked:bg-blue-600"></div>
                          </label>
                        </div>
                      </Card>

                      {/* Allowed Emails */}
                      <Card className="p-4">
                        <div className="flex flex-col gap-1.5">
                          <div className="flex items-center justify-between">
                            <label className="text-sm font-medium text-foreground">Allowed Users</label>
                            <button
                              onClick={handleEmailsSave}
                              disabled={emailsSaving || (!emailsDirty && !emailsSaved)}
                              className={`px-3 py-1 text-xs rounded-md transition-colors flex items-center gap-1 ${
                                emailsSaved ? 'bg-green-600 text-white' : 'bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50'
                              }`}
                            >
                              {emailsSaving ? 'Saving...' : emailsSaved ? <><CheckCircle className="w-3 h-3" /> Saved</> : 'Save'}
                            </button>
                          </div>
                          <input
                            type="text"
                            value={allowedEmails}
                            onChange={e => { setAllowedEmails(e.target.value); setEmailsDirty(true); setEmailsSaved(false) }}
                            placeholder="user@example.com, user2@example.com"
                            className="w-full px-2.5 py-1.5 text-xs bg-secondary border border-border rounded focus:outline-none focus:ring-1 focus:ring-primary"
                          />
                          <span className="text-[10px] text-muted-foreground">Comma-separated email addresses. Leave empty to allow everyone.</span>
                        </div>
                      </Card>

                      {slackConfig.enabled && (
                        <>
                          {/* Bot Mode */}
                          <Card className="p-4">
                            <div className="flex items-center justify-between">
                              <div>
                                <h3 className="text-sm font-medium text-foreground">Bot Mode (@mention)</h3>
                                <p className="text-xs text-muted-foreground mt-0.5">Users can @mention the bot to start agent sessions directly from Slack</p>
                              </div>
                              <label className="relative inline-flex items-center cursor-pointer">
                                <input type="checkbox" checked={slackConfig.bot_mode || false} onChange={e => setSlackConfig({ ...slackConfig, bot_mode: e.target.checked })} className="sr-only peer" />
                                <div className="w-11 h-6 bg-gray-200 peer-focus:outline-none rounded-full peer dark:bg-gray-700 peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all dark:border-gray-600 peer-checked:bg-blue-600"></div>
                              </label>
                            </div>
                          </Card>

                          {/* Channel → Workflow Routing */}
                          {slackConfig.bot_mode && (
                            <Card className="p-4">
                              <div className="flex flex-col gap-3">
                                <div>
                                  <h3 className="text-sm font-medium text-foreground">Channel → Workflow Routing</h3>
                                  <p className="text-xs text-muted-foreground mt-0.5">Messages from mapped channels run a workflow instead of multi-agent chat</p>
                                </div>

                                {/* Existing mappings */}
                                {Object.entries(slackConfig.channel_routing || {}).length > 0 && (
                                  <div className="flex flex-col gap-1.5">
                                    {Object.entries(slackConfig.channel_routing || {}).map(([chId, route]) => {
                                      const r = route as ChannelRoute
                                      const wf = workflows.find(w => w.manifest.id === r.workflow_id)
                                      return (
                                        <div key={chId} className="flex items-center gap-2">
                                          <code className="flex-shrink-0 px-2 py-1 text-xs bg-secondary border border-border rounded font-mono w-36 truncate">{chId}</code>
                                          <span className="text-xs text-muted-foreground flex-shrink-0">→</span>
                                          <span className="flex-1 text-xs truncate text-foreground">{wf ? wf.manifest.label : <span className="text-yellow-500">{r.workflow_id} (not found)</span>}</span>
                                          <select
                                            value={r.workshop_mode || ''}
                                            onChange={e => {
                                              const mode = e.target.value as '' | 'run'
                                              const updated = { ...slackConfig.channel_routing }
                                              const nextRoute: ChannelRoute = { ...r }
                                              if (mode) nextRoute.workshop_mode = mode
                                              else delete nextRoute.workshop_mode
                                              updated[chId] = nextRoute
                                              setSlackConfig({ ...slackConfig, channel_routing: updated })
                                            }}
                                            className="px-1.5 py-1 text-xs bg-secondary border border-border rounded focus:outline-none focus:ring-1 focus:ring-primary"
                                            title="Bot channels always run in Run mode. 'Default' uses the workflow manifest's setting (which is also Run for bot deployments)."
                                          >
                                            <option value="">Default</option>
                                            <option value="run">Run</option>
                                          </select>
                                          <label className="flex items-center gap-1 text-xs text-muted-foreground flex-shrink-0" title="Send detailed workflow step/runtime messages to this channel">
                                            <input
                                              type="checkbox"
                                              checked={!!r.send_full_details}
                                              onChange={e => {
                                                const updated = { ...slackConfig.channel_routing }
                                                const nextRoute: ChannelRoute = { ...r }
                                                if (e.target.checked) nextRoute.send_full_details = true
                                                else delete nextRoute.send_full_details
                                                updated[chId] = nextRoute
                                                setSlackConfig({ ...slackConfig, channel_routing: updated })
                                              }}
                                              className="h-3.5 w-3.5"
                                            />
                                            Full
                                          </label>
                                          <button
                                            onClick={() => {
                                              const updated = { ...slackConfig.channel_routing }
                                              delete updated[chId]
                                              setSlackConfig({ ...slackConfig, channel_routing: updated })
                                            }}
                                            className="p-1 text-muted-foreground hover:text-red-500 transition-colors rounded flex-shrink-0"
                                          >
                                            <Trash2 className="w-3.5 h-3.5" />
                                          </button>
                                        </div>
                                      )
                                    })}
                                  </div>
                                )}

                                {/* Add new mapping row */}
                                <div className="flex items-center gap-2">
                                  <input
                                    type="text"
                                    value={newChannelID}
                                    onChange={e => setNewChannelID(e.target.value)}
                                    placeholder="Channel ID (C...)"
                                    className="w-36 flex-shrink-0 px-2 py-1 text-xs bg-secondary border border-border rounded font-mono focus:outline-none focus:ring-1 focus:ring-primary"
                                  />
                                  <select
                                    value={newWorkflowID}
                                    onChange={e => setNewWorkflowID(e.target.value)}
                                    className="flex-1 px-2 py-1 text-xs bg-secondary border border-border rounded focus:outline-none focus:ring-1 focus:ring-primary"
                                  >
                                    <option value="">Select workflow…</option>
                                    {workflows.map(w => (
                                      <option key={w.manifest.id} value={w.manifest.id}>{w.manifest.label}</option>
                                    ))}
                                  </select>
                                  <select
                                    value={newWorkshopMode}
                                    onChange={e => setNewWorkshopMode(e.target.value as '' | 'run')}
                                    className="px-1.5 py-1 text-xs bg-secondary border border-border rounded focus:outline-none focus:ring-1 focus:ring-primary"
                                    title="Bot channels always run in Run mode. 'Default' uses the workflow manifest's setting."
                                  >
                                    <option value="">Default</option>
                                    <option value="run">Run</option>
                                  </select>
                                  <label className="flex items-center gap-1 text-xs text-muted-foreground flex-shrink-0" title="Send detailed workflow step/runtime messages to this channel">
                                    <input
                                      type="checkbox"
                                      checked={newSendFullDetails}
                                      onChange={e => setNewSendFullDetails(e.target.checked)}
                                      className="h-3.5 w-3.5"
                                    />
                                    Full
                                  </label>
                                  <button
                                    onClick={() => {
                                      if (!newChannelID.trim() || !newWorkflowID) return
                                      const selectedWf = workflows.find(w => w.manifest.id === newWorkflowID)
                                      const route: ChannelRoute = {
                                        workflow_id: newWorkflowID,
                                        workspace_path: selectedWf?.workspace_path || '',
                                      }
                                      if (newWorkshopMode) route.workshop_mode = newWorkshopMode
                                      if (newSendFullDetails) route.send_full_details = true
                                      setSlackConfig({
                                        ...slackConfig,
                                        channel_routing: { ...(slackConfig.channel_routing || {}), [newChannelID.trim()]: route },
                                      })
                                      setNewChannelID('')
                                      setNewWorkflowID('')
                                      setNewWorkshopMode('')
                                      setNewSendFullDetails(false)
                                    }}
                                    disabled={!newChannelID.trim() || !newWorkflowID}
                                    className="p-1.5 text-primary hover:bg-primary/10 disabled:opacity-40 rounded transition-colors flex-shrink-0"
                                  >
                                    <Plus className="w-4 h-4" />
                                  </button>
                                </div>
                              </div>
                            </Card>
                          )}

                          <Card className="p-4 bg-blue-50 dark:bg-blue-900/20 border-blue-300 dark:border-blue-700">
                            <details>
                              <summary className="cursor-pointer text-sm font-semibold text-blue-800 dark:text-blue-200 select-none">
                                First time? Click for step-by-step setup instructions
                              </summary>
                              <div className="mt-3 text-xs text-blue-900 dark:text-blue-100 space-y-3">
                                <div>
                                  <p className="font-semibold">1. Create a Slack App</p>
                                  <p className="mt-1">Go to <a href="https://api.slack.com/apps" target="_blank" rel="noreferrer" className="underline">api.slack.com/apps</a> → <b>Create New App</b> → <b>From scratch</b>. Pick a name and your workspace.</p>
                                </div>
                                <div>
                                  <p className="font-semibold">2. Add Bot Token Scopes</p>
                                  <p className="mt-1">In the sidebar: <b>OAuth &amp; Permissions</b> → <b>Scopes</b> → <b>Bot Token Scopes</b>. Add at minimum:</p>
                                  <ul className="mt-1 ml-4 list-disc space-y-0.5">
                                    <li><code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">app_mentions:read</code></li>
                                    <li><code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">channels:history</code>, <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">groups:history</code></li>
                                    <li><code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">chat:write</code>, <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">chat:write.public</code></li>
                                    <li><code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">reactions:write</code> (for the hourglass "bot is working" indicator)</li>
                                    <li><code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">users:read</code>, <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">users:read.email</code> (required for per-user memory)</li>
                                    <li><code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">files:read</code>, <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">files:write</code> (optional, for attachments)</li>
                                  </ul>
                                </div>
                                <div>
                                  <p className="font-semibold">3. Enable Socket Mode &amp; generate App Token</p>
                                  <p className="mt-1"><b>Socket Mode</b> (sidebar) → toggle <b>Enable Socket Mode</b> ON. It will prompt you to create an <b>App-Level Token</b> with the <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">connections:write</code> scope. Copy the token — it starts with <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">xapp-</code>. This is your <b>App Token</b> below.</p>
                                </div>
                                <div>
                                  <p className="font-semibold">4. Enable Event Subscriptions</p>
                                  <p className="mt-1"><b>Event Subscriptions</b> (sidebar) → toggle <b>Enable Events</b> ON. Under <b>Subscribe to bot events</b>, add: <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">app_mention</code>, <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">message.channels</code>, <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">message.groups</code>. Save changes.</p>
                                </div>
                                <div>
                                  <p className="font-semibold">5. Install to workspace &amp; copy Bot Token</p>
                                  <p className="mt-1"><b>Install App</b> (sidebar) → <b>Install to Workspace</b> → approve. After install, go back to <b>OAuth &amp; Permissions</b> — the <b>Bot User OAuth Token</b> now appears at the top. Copy it — it starts with <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">xoxb-</code>. This is your <b>Bot Token</b> below.</p>
                                </div>
                                <div>
                                  <p className="font-semibold">6. Invite the bot &amp; get Channel ID</p>
                                  <p className="mt-1">In Slack, invite the bot to a channel: <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">/invite @YourBot</code>. Then right-click the channel → <b>View channel details</b> → scroll to the bottom — the <b>Channel ID</b> starts with <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">C</code>.</p>
                                </div>
                                <p className="pt-1 italic opacity-80">If you re-add scopes or events later, you must re-install the app for changes to take effect.</p>
                              </div>
                            </details>
                          </Card>

                          <Card className="p-4 bg-amber-50 dark:bg-amber-900/20 border-amber-300 dark:border-amber-700">
                            <div className="flex items-start gap-2">
                              <AlertTriangle className="w-4 h-4 text-amber-600 dark:text-amber-400 flex-shrink-0 mt-0.5" />
                              <div>
                                <p className="text-sm font-semibold text-amber-800 dark:text-amber-200">Required: Event Subscriptions</p>
                                <p className="text-xs text-amber-700 dark:text-amber-300 mt-1">
                                  Enable Event Subscriptions in your Slack App and subscribe to: <code className="bg-amber-100 dark:bg-amber-800/40 px-1 rounded font-mono">app_mention</code>, <code className="bg-amber-100 dark:bg-amber-800/40 px-1 rounded font-mono">message.channels</code>, <code className="bg-amber-100 dark:bg-amber-800/40 px-1 rounded font-mono">message.groups</code>
                                </p>
                              </div>
                            </div>
                          </Card>

                          <div className="space-y-3">
                            {/* Bot Token */}
                            <Card className="p-4">
                              <label className="block text-sm font-medium text-foreground mb-2">Bot Token <span className="text-red-500">*</span></label>
                              <div className="relative">
                                <input type={showBotToken ? 'text' : 'password'} value={slackConfig.bot_token || ''} onChange={e => setSlackConfig({ ...slackConfig, bot_token: e.target.value })} placeholder="xoxb-..." className="w-full px-3 py-2 pr-10 border border-border rounded-md bg-background text-foreground focus:outline-none focus:ring-2 focus:ring-primary" />
                                <button type="button" onClick={() => setShowBotToken(!showBotToken)} className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground">
                                  {showBotToken ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                                </button>
                              </div>
                              <p className="text-xs text-muted-foreground mt-1">OAuth & Permissions → Bot User OAuth Token (starts with xoxb-)</p>
                            </Card>

                            {/* Channel ID */}
                            <Card className="p-4">
                              <label className="block text-sm font-medium text-foreground mb-2">Channel ID <span className="text-red-500">*</span></label>
                              <input type="text" value={slackConfig.channel_id || ''} onChange={e => setSlackConfig({ ...slackConfig, channel_id: e.target.value })} placeholder="C1234567890" className="w-full px-3 py-2 border border-border rounded-md bg-background text-foreground focus:outline-none focus:ring-2 focus:ring-primary" />
                              <p className="text-xs text-muted-foreground mt-1">Right-click channel → View channel details → Channel ID (starts with C)</p>
                            </Card>

                            {/* App Token */}
                            <Card className="p-4">
                              <label className="block text-sm font-medium text-foreground mb-2">App Token (Socket Mode) <span className="text-red-500">*</span></label>
                              <div className="relative">
                                <input type={showAppToken ? 'text' : 'password'} value={slackConfig.app_token || ''} onChange={e => setSlackConfig({ ...slackConfig, app_token: e.target.value })} placeholder="xapp-..." className="w-full px-3 py-2 pr-10 border border-border rounded-md bg-background text-foreground focus:outline-none focus:ring-2 focus:ring-primary" />
                                <button type="button" onClick={() => setShowAppToken(!showAppToken)} className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground">
                                  {showAppToken ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                                </button>
                              </div>
                              <p className="text-xs text-muted-foreground mt-1">Basic Information → App-Level Tokens → Generate with <code className="bg-secondary px-1 rounded font-mono">connections:write</code> scope (starts with xapp-)</p>
                            </Card>

                            {/* Test Connection */}
                            <div className="space-y-1">
                              <Button variant="outline" onClick={handleSlackTest} disabled={!slackConfig.enabled || slackTesting || slackLoading} className="w-full flex items-center justify-center gap-2">
                                {slackTesting ? <><Loader2 className="w-4 h-4 animate-spin" />Testing...</> : 'Test Connection'}
                              </Button>
                              <p className="text-xs text-muted-foreground text-center">
                                Tests the <b>saved</b> config from the workspace — click <b>Save</b> first if you've edited any field above.
                              </p>
                            </div>

                            {testResult && (
                              <div className={`p-3 border rounded-lg flex items-start gap-2 ${testResult.success ? 'bg-green-50 dark:bg-green-900/20 border-green-200 dark:border-green-800' : 'bg-red-50 dark:bg-red-900/20 border-red-200 dark:border-red-800'}`}>
                                {testResult.success ? <CheckCircle className="w-4 h-4 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" /> : <AlertCircle className="w-4 h-4 text-red-600 dark:text-red-400 flex-shrink-0 mt-0.5" />}
                                <p className={`text-sm ${testResult.success ? 'text-green-700 dark:text-green-300' : 'text-red-700 dark:text-red-300'}`}>{testResult.message}</p>
                              </div>
                            )}
                            {testResult?.success && pollingForReply && !testReply && (
                              <div className="p-3 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg flex items-center gap-2">
                                <Loader2 className="w-4 h-4 animate-spin text-blue-600 dark:text-blue-400" />
                                <p className="text-sm text-blue-800 dark:text-blue-200">Waiting for reply in Slack thread...</p>
                              </div>
                            )}
                            {testReply && (
                              <div className="p-3 bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-lg">
                                <p className="text-sm font-medium text-green-800 dark:text-green-200">Reply received: {testReply}</p>
                              </div>
                            )}
                          </div>
                        </>
                      )}
                    </>
                  )}
                </div>
                <div className="px-4 py-3 border-t border-border flex items-center justify-end gap-2 flex-shrink-0">
                  <Button variant="ghost" onClick={handleClose}>Cancel</Button>
                  <Button onClick={handleSlackSave} disabled={!slackHasChanges || slackSaving || slackLoading} className="flex items-center gap-2">
                    {slackSaving ? <><Loader2 className="w-4 h-4 animate-spin" />Saving...</> : <><CheckCircle className="w-4 h-4" />Save</>}
                  </Button>
                </div>
              </>
            )}

            {/* ── WhatsApp Section ── */}
            {activeSection === 'whatsapp' && (
              <div className="flex-1 overflow-y-auto p-4 space-y-4">
                {waError && (
                  <div className="p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg flex items-start gap-2">
                    <AlertCircle className="w-4 h-4 text-red-600 dark:text-red-400 flex-shrink-0 mt-0.5" />
                    <p className="text-sm text-red-700 dark:text-red-300">{waError}</p>
                  </div>
                )}

                {/* Connector disabled at server startup */}
                {waStatus && !waStatus.enabled && (
                  <Card className="p-4">
                    <div className="flex items-start gap-3">
                      <AlertTriangle className="w-5 h-5 text-amber-500 flex-shrink-0 mt-0.5" />
                      <div className="space-y-1">
                        <h3 className="text-sm font-medium text-foreground">WhatsApp connector is disabled</h3>
                        <p className="text-xs text-muted-foreground">
                          Remove <code className="px-1 py-0.5 bg-muted rounded">WHATSAPP_ENABLED=false</code> from
                          the server's <code className="px-1 py-0.5 bg-muted rounded">.env</code> and restart the
                          agent. The connector is enabled by default, and the per-user session directory can be
                          overridden via <code className="px-1 py-0.5 bg-muted rounded">WHATSAPP_SESSION_DIR</code>.
                        </p>
                      </div>
                    </div>
                  </Card>
                )}

                {/* Status card */}
                {waStatus && waStatus.enabled && (
                  <Card className="p-4">
                    <div className="flex items-center justify-between">
                      <div>
                        <h3 className="text-sm font-medium text-foreground">Status</h3>
                        <p className="text-xs text-muted-foreground mt-0.5">
                          Uses the unofficial WhatsApp Web protocol (whatsmeow). Pair your personal number once
                          by scanning the QR below. On Android: tap the ⋮ menu → Linked Devices → Link a device.
                          On iPhone: Settings → Linked Devices → Link Device.
                        </p>
                      </div>
                      <div className="flex flex-col items-end gap-0.5 text-xs">
                        <span className="flex items-center gap-1.5">
                          <span
                            className={`w-1.5 h-1.5 rounded-full ${
                              waStatus.connected ? 'bg-green-500' : waStatus.paired ? 'bg-amber-500' : 'bg-gray-400'
                            }`}
                          />
                          <span className="text-foreground">
                            {waStatus.connected ? 'Connected' : waStatus.paired ? 'Paired, offline' : 'Not paired'}
                          </span>
                        </span>
                        {waStatus.own_jid && (
                          <span className="text-muted-foreground font-mono text-[10px]">{waStatus.own_jid}</span>
                        )}
                        {(waStatus.owner_email || waStatus.owner_username || waStatus.owner_user_id) && (
                          <span className="text-muted-foreground text-[10px]">
                            bound to{' '}
                            <span className="text-foreground">
                              {waStatus.owner_email || waStatus.owner_username || waStatus.owner_user_id}
                            </span>
                          </span>
                        )}
                      </div>
                    </div>
                  </Card>
                )}

                {/* QR pairing card — shown while unpaired */}
                {waStatus && waStatus.enabled && !waStatus.paired && (
                  <Card className="p-4">
                    <div className="flex flex-col items-center gap-3">
                      <h3 className="text-sm font-medium text-foreground">Scan to pair</h3>
                      {waStatus.qr_available ? (
                        <>
                          {qrImageURL ? (
                            <img
                              src={qrImageURL}
                              alt="WhatsApp pairing QR"
                              width={256}
                              height={256}
                              className="rounded border border-border bg-white p-2"
                            />
                          ) : (
                            <div className="flex h-64 w-64 items-center justify-center rounded border border-border bg-muted/30 p-4 text-center">
                              {qrLoading ? (
                                <div className="flex items-center gap-2 text-sm text-muted-foreground">
                                  <Loader2 className="w-4 h-4 animate-spin" />
                                  Loading QR…
                                </div>
                              ) : qrError ? (
                                <div className="flex flex-col items-center gap-2 text-sm text-red-700 dark:text-red-300">
                                  <AlertCircle className="w-5 h-5" />
                                  <span>{qrError}</span>
                                </div>
                              ) : (
                                <span className="text-sm text-muted-foreground">QR not available yet.</span>
                              )}
                            </div>
                          )}
                          <p className="text-xs text-muted-foreground text-center max-w-sm">
                            Open WhatsApp on your phone. <strong>Android</strong>: ⋮ menu → Linked Devices → Link a
                            device. <strong>iPhone</strong>: Settings → Linked Devices → Link Device. Then scan this
                            code. The QR rotates every few seconds; this page refreshes it automatically.
                          </p>
                        </>
                      ) : (
                        <div className="flex items-center gap-2 text-sm text-muted-foreground py-8">
                          <Loader2 className="w-4 h-4 animate-spin" />
                          Waiting for the server to generate a QR…
                        </div>
                      )}
                    </div>
                  </Card>
                )}

                {/* Workflow routing card — shown once paired. User types
                    @<slug> at the start of a WhatsApp message to route it
                    to a specific workflow instead of default multi-agent
                    chat. Empty table = no routing, everything falls
                    through to chat. */}
                {waStatus && waStatus.enabled && waStatus.paired && (
                  <Card className="p-4">
                    <div className="flex items-start justify-between mb-2">
                      <div>
                        <h3 className="text-sm font-medium text-foreground">Workflow routing</h3>
                        <p className="text-xs text-muted-foreground mt-0.5">
                          Map an <code>@slug</code> to a workflow. A WhatsApp message that starts with
                          <code> @rca …</code> routes straight into the matching workflow. No prefix = default chat.
                        </p>
                      </div>
                      <button
                        onClick={handleSaveWaRoutes}
                        disabled={
                          waRoutesSaving ||
                          JSON.stringify(waRoutes) === JSON.stringify(waRoutesOriginal)
                        }
                        className={`px-3 py-1 text-xs rounded-md transition-colors flex items-center gap-1 ${
                          waRoutesSaved
                            ? 'bg-green-600 text-white'
                            : 'bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50'
                        }`}
                      >
                        {waRoutesSaving
                          ? 'Saving…'
                          : waRoutesSaved
                          ? (<><CheckCircle className="w-3 h-3" /> Saved</>)
                          : 'Save'}
                      </button>
                    </div>
                    {waRoutesError && (
                      <div className="mb-2 p-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded text-xs text-red-700 dark:text-red-300 flex items-start gap-1.5">
                        <AlertCircle className="w-3.5 h-3.5 flex-shrink-0 mt-0.5" />
                        {waRoutesError}
                      </div>
                    )}
                    {waRoutes.length === 0 && (
                      <p className="text-xs text-muted-foreground italic mb-2">
                        No routes yet. Click <em>Add route</em> below to map a slug.
                      </p>
                    )}
                    {waRoutes.length > 0 && (
                      <div className="space-y-1.5">
                        {waRoutes.map((row, idx) => (
                          <div key={idx} className="flex items-center gap-1.5">
                            <span className="text-xs text-muted-foreground select-none">@</span>
                            <input
                              type="text"
                              value={row.slug}
                              onChange={e => {
                                const next = [...waRoutes]
                                next[idx] = { ...row, slug: e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, '') }
                                setWaRoutes(next)
                              }}
                              placeholder="slug"
                              className="w-28 px-2 py-1 text-xs bg-secondary border border-border rounded focus:outline-none focus:ring-1 focus:ring-primary"
                            />
                            <select
                              value={row.workflow_id}
                              onChange={e => {
                                const next = [...waRoutes]
                                next[idx] = { ...row, workflow_id: e.target.value }
                                setWaRoutes(next)
                              }}
                              className="flex-1 px-2 py-1 text-xs bg-secondary border border-border rounded focus:outline-none focus:ring-1 focus:ring-primary"
                            >
                              <option value="">Select workflow…</option>
                              {workflows.map(wf => (
                                <option key={wf.manifest.id} value={wf.manifest.id}>
                                  {wf.manifest.label || wf.manifest.id}
                                </option>
                              ))}
                            </select>
                            <select
                              value={row.workshop_mode}
                              onChange={e => {
                                const next = [...waRoutes]
                                next[idx] = { ...row, workshop_mode: normalizeWorkflowRouteMode(e.target.value) }
                                setWaRoutes(next)
                              }}
                              className="w-24 px-2 py-1 text-xs bg-secondary border border-border rounded focus:outline-none focus:ring-1 focus:ring-primary"
                            >
                              <option value="run">Run</option>
                            </select>
                            <label className="flex items-center gap-1 text-xs text-muted-foreground flex-shrink-0" title="Send detailed workflow step/runtime messages to WhatsApp">
                              <input
                                type="checkbox"
                                checked={!!row.send_full_details}
                                onChange={e => {
                                  const next = [...waRoutes]
                                  next[idx] = { ...row, send_full_details: e.target.checked || undefined }
                                  setWaRoutes(next)
                                }}
                                className="h-3.5 w-3.5"
                              />
                              Full
                            </label>
                            <button
                              onClick={() => {
                                const next = [...waRoutes]
                                next.splice(idx, 1)
                                setWaRoutes(next)
                              }}
                              className="p-1 text-muted-foreground hover:text-red-600 transition-colors"
                              title="Delete route"
                            >
                              <Trash2 className="w-3.5 h-3.5" />
                            </button>
                          </div>
                        ))}
                      </div>
                    )}
                    <button
                      onClick={() => setWaRoutes([...waRoutes, { slug: '', workflow_id: '', workshop_mode: 'run' }])}
                      className="mt-2 px-2 py-1 text-xs text-muted-foreground hover:text-foreground border border-dashed border-border rounded flex items-center gap-1 transition-colors"
                    >
                      <Plus className="w-3 h-3" /> Add route
                    </button>
                  </Card>
                )}

                {/* How-to-chat card — shown once paired. */}
                {waStatus && waStatus.enabled && waStatus.paired && (
                  <Card className="p-4">
                    <h3 className="text-sm font-medium text-foreground mb-1.5">How to chat</h3>
                    <div className="space-y-1.5 text-xs text-muted-foreground">
                      <p>
                        Open WhatsApp → <strong>Message Yourself</strong> chat, or DM the paired WhatsApp number
                        from another phone. First send <code>link {waStatus.link_code || '123456'}</code> from that
                        chat to bind WhatsApp's current phone/LID identity. Then send messages normally.
                      </p>
                      {waStatus.link_code && (
                        <p className="text-muted-foreground/80">
                          Linked chats: {waStatus.bound_chat_count ?? 0}. Link code expires{' '}
                          {waStatus.link_code_expires_at
                            ? new Date(waStatus.link_code_expires_at).toLocaleString()
                            : 'soon'}
                          .
                        </p>
                      )}
                      <p className="text-muted-foreground/80">
                        For a proper separate-bot experience (like Slack's <code>@bot</code>), pair a dedicated
                        WhatsApp number — a second SIM, WhatsApp Business with a different number, or a virtual
                        number from Twilio. Only linked inbound DMs are handled as bot messages.
                      </p>
                    </div>
                  </Card>
                )}

                {/* Unpair card — shown once paired */}
                {waStatus && waStatus.enabled && waStatus.paired && (
                  <Card className="p-4">
                    <div className="flex items-center justify-between gap-3">
                      <div>
                        <h3 className="text-sm font-medium text-foreground">Unpair</h3>
                        <p className="text-xs text-muted-foreground mt-0.5">
                          Drops the current device link and deletes the session file. You'll need to scan a new QR
                          to pair again.
                        </p>
                      </div>
                      <Button
                        onClick={handleUnpairWhatsApp}
                        disabled={unpairing}
                        variant={unpairConfirm ? 'destructive' : 'outline'}
                        size="sm"
                        className="flex-shrink-0 whitespace-nowrap"
                      >
                        {unpairing ? (
                          <><Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" /> Unpairing…</>
                        ) : unpairConfirm ? (
                          <><Trash2 className="w-3.5 h-3.5 mr-1.5" /> Confirm unpair</>
                        ) : (
                          <>Unpair</>
                        )}
                      </Button>
                    </div>
                  </Card>
                )}

                {/* Loading placeholder */}
                {!waStatus && !waError && (
                  <div className="flex items-center justify-center py-12">
                    <Loader2 className="w-8 h-8 animate-spin text-primary" />
                  </div>
                )}
              </div>
            )}

            {/* ── Simulate Section ── */}
            {activeSection === 'simulate' && (
              <>
                {/* Tier bar */}
                <div className="px-4 py-1.5 border-b border-border bg-muted/30 flex items-center gap-2 text-[11px] text-muted-foreground flex-shrink-0">
                  <Layers className="w-3.5 h-3.5 flex-shrink-0" />
                  <span className="font-medium">Tiers:</span>
                  {serverTierConfig ? (
                    <>
                      {['high', 'medium', 'low'].map(tier => {
                        const cfg = serverTierConfig[tier]
                        if (!cfg?.model_id) return null
                        const label = cfg.provider ? `${cfg.provider}/${cfg.model_id}` : cfg.model_id
                        return (
                          <span key={tier} className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded bg-secondary border border-border">
                            <span className="capitalize font-medium">{tier[0].toUpperCase()}:</span>
                            <span className="truncate max-w-[200px]">{label}</span>
                          </span>
                        )
                      })}
                    </>
                  ) : (
                    <span className="text-yellow-500">Not configured — uses the same tier config as multi-agent chat</span>
                  )}
                  <div className="ml-auto flex items-center gap-2">
                    <select value={threadMode} onChange={e => handleModeChange(e.target.value as 'threaded' | 'non-threaded')} className="text-xs px-2 py-1 bg-secondary border border-border rounded focus:outline-none focus:ring-1 focus:ring-primary">
                      <option value="non-threaded">Non-Threaded</option>
                      <option value="threaded">Threaded</option>
                    </select>
                    <div className="flex items-center gap-1.5">
                      <div className={`w-2 h-2 rounded-full ${simStatusConfig[simStatus].color}`} />
                      <span>{simStatusConfig[simStatus].label}</span>
                    </div>
                    <button onClick={handleReset} className="p-1 text-muted-foreground hover:text-foreground transition-colors rounded" title="Reset">
                      <RotateCcw className="w-3.5 h-3.5" />
                    </button>
                  </div>
                </div>

                {/* Thread sidebar + messages */}
                <div className="flex-1 flex overflow-hidden">
                  <div className="w-44 border-r border-border flex flex-col overflow-hidden flex-shrink-0">
                    <div className="px-3 py-2 border-b border-border flex items-center justify-between">
                      <span className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Threads</span>
                      <button onClick={handleNewThread} className="p-1 text-muted-foreground hover:text-foreground transition-colors rounded hover:bg-muted" title="New Thread">
                        <Plus className="w-3.5 h-3.5" />
                      </button>
                    </div>
                    <div className="flex-1 overflow-y-auto">
                      {threads.length === 0 ? (
                        <div className="p-3 text-xs text-muted-foreground text-center">No threads yet</div>
                      ) : threads.map(t => (
                        <button key={t.thread_id} onClick={() => switchToThread(t.thread_id)} className={`w-full text-left px-3 py-2 border-b border-border/50 hover:bg-muted/50 transition-colors ${activeThreadId === t.thread_id ? 'bg-muted' : ''}`}>
                          <div className="flex items-center gap-1.5 mb-0.5">
                            <MessageSquare className="w-3 h-3 text-muted-foreground flex-shrink-0" />
                            <span className="text-xs truncate text-foreground">{t.preview || '(no message)'}</span>
                          </div>
                          <div className="flex items-center justify-between text-[10px] text-muted-foreground">
                            <span>{formatTime(t.created_at)}</span>
                            <span>{t.message_count} msg{t.message_count !== 1 ? 's' : ''}</span>
                          </div>
                        </button>
                      ))}
                    </div>
                  </div>

                  <div className="flex-1 flex flex-col overflow-hidden">
                    <div className="flex-1 overflow-y-auto p-4 space-y-3">
                      {messages.length === 0 && (
                        <div className="flex flex-col items-center justify-center h-full text-muted-foreground text-sm gap-2">
                          <Bot className="w-8 h-8 opacity-50" />
                          <p>Send a message to test the bot flow</p>
                        </div>
                      )}
                      {messages.map((msg, idx) => (
                        <div key={msg.id || idx} className={`flex gap-2 ${msg.is_bot ? '' : 'justify-end'}`}>
                          {msg.is_bot && <div className="w-7 h-7 rounded-full bg-primary/10 flex items-center justify-center flex-shrink-0 mt-0.5"><Bot className="w-3.5 h-3.5 text-primary" /></div>}
                          <div className={`max-w-[80%] ${msg.is_bot ? '' : 'order-first'}`}>
                            {msg.is_bot ? (
                              <div className="px-3 py-2 rounded-lg text-sm bg-muted text-foreground"><MarkdownRenderer content={msg.text} maxHeight="none" /></div>
                            ) : (
                              <div className="px-3 py-2 rounded-lg text-sm whitespace-pre-wrap bg-primary text-primary-foreground">{msg.text}</div>
                            )}
                          </div>
                          {!msg.is_bot && <div className="w-7 h-7 rounded-full bg-secondary flex items-center justify-center flex-shrink-0 mt-0.5"><User className="w-3.5 h-3.5 text-muted-foreground" /></div>}
                        </div>
                      ))}
                      {(() => {
                        const lastMsg = messages[messages.length - 1]
                        if (simStatus === 'sending' || (simStatus === 'running' && lastMsg && !lastMsg.is_bot) || (simStatus === 'running' && !messages.some(m => m.is_bot))) {
                          return (
                            <div className="flex gap-2">
                              <div className="w-7 h-7 rounded-full bg-primary/10 flex items-center justify-center flex-shrink-0"><Loader2 className="w-3.5 h-3.5 text-primary animate-spin" /></div>
                              <div className="px-3 py-2 rounded-lg bg-muted text-muted-foreground text-sm">Agent is working...</div>
                            </div>
                          )
                        }
                        return null
                      })()}
                      {errorMsg && <div className="text-xs text-red-500 text-center py-1">{errorMsg}</div>}
                      <div ref={messagesEndRef} />
                    </div>
                    <div className="px-4 py-3 border-t border-border flex-shrink-0">
                      <div className="flex items-center gap-1.5 mb-2 text-xs text-muted-foreground">
                        <span>Commands:</span>
                        {['@status', '@full', '@concise'].map(cmd => (
                          <button
                            key={cmd}
                            type="button"
                            onClick={() => setInput(cmd)}
                            className="px-1.5 py-0.5 rounded bg-secondary hover:bg-accent text-foreground font-mono"
                          >
                            {cmd}
                          </button>
                        ))}
                      </div>
                      <div className="flex gap-2">
                        <input type="text" value={input} onChange={e => setInput(e.target.value)} onKeyDown={e => e.key === 'Enter' && !e.shiftKey && handleSend()} placeholder="Type a message... (@status, @full, @concise)" disabled={sending} className="flex-1 px-3 py-2 text-sm bg-secondary border border-border rounded-md focus:outline-none focus:ring-1 focus:ring-primary disabled:opacity-50" />
                        <button onClick={handleSend} disabled={!input.trim() || sending} className="px-3 py-2 bg-primary text-primary-foreground rounded-md hover:bg-primary/90 transition-colors disabled:opacity-50 flex items-center gap-1.5">
                          {sending ? <Loader2 className="w-4 h-4 animate-spin" /> : <Send className="w-4 h-4" />}
                        </button>
                      </div>
                    </div>
                  </div>
                </div>
              </>
            )}

          </div>
        </div>
      </div>
    </div>
    </ModalPortal>
  )
}
