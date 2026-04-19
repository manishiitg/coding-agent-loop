import { useState, useEffect, useRef, useCallback } from 'react'
import {
  X, CheckCircle, AlertCircle, Loader2, Eye, EyeOff, AlertTriangle,
  Bot, User, Send, RotateCcw, Plus, MessageSquare, Layers, Play, Trash2,
} from 'lucide-react'
import { Button } from '../ui/Button'
import { Card } from '../ui/Card'
import { MarkdownRenderer } from '../ui/MarkdownRenderer'
import { agentApi } from '../../services/api'
import { useLLMStore } from '../../stores'
import type { SlackConfig, SlackConfigRequest, SlackTestResponse, SimulatorThreadInfo, DiscoveredWorkflow, ChannelRoute } from '../../services/api-types'

interface BotConnectorModalProps {
  isOpen: boolean
  onClose: () => void
}

type Section = 'slack' | 'simulate'
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
  const [newWorkshopMode, setNewWorkshopMode] = useState<'' | 'builder' | 'optimizer' | 'run'>('')

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
          openrouter: llmState.openrouterConfig, openai: llmState.openaiConfig,
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
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div className={`bg-background border border-border rounded-lg shadow-xl w-full flex flex-col ${isSimulate ? 'max-w-5xl h-[85vh]' : 'max-w-3xl max-h-[90vh]'}`}>

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
                                              const mode = e.target.value as '' | 'builder' | 'optimizer' | 'run'
                                              const updated = { ...slackConfig.channel_routing }
                                              const nextRoute: ChannelRoute = { ...r }
                                              if (mode) nextRoute.workshop_mode = mode
                                              else delete nextRoute.workshop_mode
                                              updated[chId] = nextRoute
                                              setSlackConfig({ ...slackConfig, channel_routing: updated })
                                            }}
                                            className="px-1.5 py-1 text-xs bg-secondary border border-border rounded focus:outline-none focus:ring-1 focus:ring-primary"
                                            title="Override workshop mode for this channel. 'Default' uses whatever the workflow manifest specifies."
                                          >
                                            <option value="">Default</option>
                                            <option value="run">Run</option>
                                            <option value="optimizer">Optimize</option>
                                            <option value="builder">Builder</option>
                                          </select>
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
                                    onChange={e => setNewWorkshopMode(e.target.value as '' | 'builder' | 'optimizer' | 'run')}
                                    className="px-1.5 py-1 text-xs bg-secondary border border-border rounded focus:outline-none focus:ring-1 focus:ring-primary"
                                    title="Override workshop mode for this channel. 'Default' uses the workflow manifest value."
                                  >
                                    <option value="">Default</option>
                                    <option value="run">Run</option>
                                    <option value="optimizer">Optimize</option>
                                    <option value="builder">Builder</option>
                                  </select>
                                  <button
                                    onClick={() => {
                                      if (!newChannelID.trim() || !newWorkflowID) return
                                      const selectedWf = workflows.find(w => w.manifest.id === newWorkflowID)
                                      const route: ChannelRoute = {
                                        workflow_id: newWorkflowID,
                                        workspace_path: selectedWf?.workspace_path || '',
                                      }
                                      if (newWorkshopMode) route.workshop_mode = newWorkshopMode
                                      setSlackConfig({
                                        ...slackConfig,
                                        channel_routing: { ...(slackConfig.channel_routing || {}), [newChannelID.trim()]: route },
                                      })
                                      setNewChannelID('')
                                      setNewWorkflowID('')
                                      setNewWorkshopMode('')
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
                      <div className="flex gap-2">
                        <input type="text" value={input} onChange={e => setInput(e.target.value)} onKeyDown={e => e.key === 'Enter' && !e.shiftKey && handleSend()} placeholder="Type a message..." disabled={sending} className="flex-1 px-3 py-2 text-sm bg-secondary border border-border rounded-md focus:outline-none focus:ring-1 focus:ring-primary disabled:opacity-50" />
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
  )
}
