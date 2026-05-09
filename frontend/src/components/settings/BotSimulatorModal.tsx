import { useState, useEffect, useRef, useCallback } from 'react'
import { X, Send, RotateCcw, Bot, User, Loader2, MessageSquare, Plus, Layers } from 'lucide-react'
import { MarkdownRenderer } from '../ui/MarkdownRenderer'
import { agentApi } from '../../services/api'
import { useLLMStore } from '../../stores'
import type { SimulatorThreadInfo } from '../../services/api-types'

interface BotSimulatorModalProps {
  isOpen: boolean
  onClose: () => void
}

// A chat message displayed in the UI
interface ChatMessage {
  id: string
  text: string
  is_bot: boolean
  timestamp: string
}

type SimStatus = 'idle' | 'sending' | 'running' | 'completed' | 'error'

export default function BotSimulatorModal({ isOpen, onClose }: BotSimulatorModalProps) {
  const { delegationTierConfig } = useLLMStore()
  const [input, setInput] = useState('')
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [status, setStatus] = useState<SimStatus>('idle')
  const [sending, setSending] = useState(false)
  const [errorMsg, setErrorMsg] = useState('')
  const [threadMode, setThreadMode] = useState<'threaded' | 'non-threaded'>('non-threaded')
  const [threads, setThreads] = useState<SimulatorThreadInfo[]>([])
  const [activeThreadId, setActiveThreadId] = useState<string | null>(null)
  const [serverTierConfig, setServerTierConfig] = useState<Record<string, { provider?: string; model_id?: string }> | null>(null)
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const pollingRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const threadOffsetRef = useRef(0)

  const scrollToBottom = useCallback(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [])

  // Fetch mode from backend on open + sync tier config to DB + fetch back from DB
  useEffect(() => {
    if (!isOpen) return
    agentApi.getSimulatorMode().then(data => {
      setThreadMode(data.threaded ? 'threaded' : 'non-threaded')
    }).catch(() => {})

    // Sync tier config from localStorage to DB (with provider API keys), then fetch back
    const syncAndFetch = async () => {
      if (delegationTierConfig && Object.keys(delegationTierConfig).length > 0) {
        // Collect provider API keys for the tiers
        const llmState = useLLMStore.getState()
        const providerConfigs: Record<string, { api_key?: string }> = {
          openrouter: llmState.openrouterConfig,
          openai: llmState.openaiConfig,
          anthropic: llmState.anthropicConfig,
          vertex: llmState.vertexConfig,
          bedrock: llmState.bedrockConfig,
          azure: llmState.azureConfig,
        }
        const providerKeys: Record<string, string> = {}
        const tierCfg = delegationTierConfig as Record<string, { provider?: string }>
        for (const tier of ['high', 'medium', 'low']) {
          const provider = tierCfg[tier]?.provider
          if (provider && providerConfigs[provider]?.api_key && !providerKeys[provider]) {
            providerKeys[provider] = providerConfigs[provider].api_key!
          }
        }
        // Also collect keys from custom tiers
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
      // Fetch the shared workspace tier config used by both bot simulation and multi-agent chat
      try {
        const cfg = await agentApi.getDelegationTierConfig()
        if (cfg && typeof cfg === 'object' && Object.keys(cfg).length > 0) {
          setServerTierConfig(cfg as Record<string, { provider?: string; model_id?: string }>)
        } else {
          setServerTierConfig(null)
        }
      } catch {
        setServerTierConfig(null)
      }
    }
    syncAndFetch()
  }, [isOpen, delegationTierConfig])

  // Fetch thread list once on open (no polling)
  useEffect(() => {
    if (!isOpen) return
    agentApi.listSimulatorThreads().then(data => {
      setThreads(data.threads || [])
    }).catch(() => {})
  }, [isOpen])

  // Auto-scroll on new messages
  useEffect(() => {
    scrollToBottom()
  }, [messages, scrollToBottom])

  // Poll messages only when a session is running (for progress updates)
  // Uses setTimeout chain (not setInterval) to prevent overlapping polls
  useEffect(() => {
    if (status !== 'running' || !activeThreadId) return

    let cancelled = false
    let lastCount = threadOffsetRef.current

    const poll = async () => {
      if (cancelled) return
      try {
        const data = await agentApi.getSimulatorMessages(activeThreadId, lastCount)
        if (cancelled) return
        if (data.messages && data.messages.length > 0) {
          const newMsgs: ChatMessage[] = data.messages
              .map(m => ({
              id: m.id,
              text: m.text,
              is_bot: m.is_bot,
              timestamp: m.timestamp,
            }))
          // Deduplicate by message ID to prevent duplicates from race conditions
          setMessages(prev => {
            const existingIds = new Set(prev.map(m => m.id))
            const unique = newMsgs.filter(m => !existingIds.has(m.id))
            return unique.length > 0 ? [...prev, ...unique] : prev
          })
          lastCount = data.total

          // Check if session ended
          const lastBot = [...data.messages].reverse().find(m => m.is_bot)
          if (lastBot) {
            if (lastBot.text.includes('Session completed')) setStatus('completed')
            else if (lastBot.text.includes('Session failed') || lastBot.text.includes('Cancelled')) setStatus('error')
          }
        }
      } catch {
        // ignore
      }
      // Schedule next poll only after current one completes
      if (!cancelled) {
        pollingRef.current = setTimeout(poll, 2000)
      }
    }

    pollingRef.current = setTimeout(poll, 2000)
    return () => {
      cancelled = true
      if (pollingRef.current) clearTimeout(pollingRef.current)
    }
  }, [status, activeThreadId])

  const handleSend = async () => {
    if (!input.trim() || sending) return

    const text = input.trim()
    setInput('')
    setSending(true)
    setStatus('sending')
    setErrorMsg('')

    // Add user message to UI immediately
    setMessages(prev => [...prev, {
      id: 'user_' + Date.now(),
      text,
      is_bot: false,
      timestamp: new Date().toISOString(),
    }])

    try {
      const result = await agentApi.simulateBotMessage(text, activeThreadId || undefined)

      // Track thread offset for polling initialization
      if (result.thread_offset) {
        threadOffsetRef.current = result.thread_offset
      }

      // Every response now starts a session immediately (follow_up type)
      setActiveThreadId(result.thread_id)
      if (result.type === 'follow_up') {
        setStatus('running')
      } else if (result.type === 'conversation' && result.response) {
        // Fallback for conversation type (e.g. plan rejection)
        setMessages(prev => [...prev, {
          id: 'bot_' + Date.now(),
          text: result.response || '',
          is_bot: true,
          timestamp: new Date().toISOString(),
        }])
        setStatus('idle')
      }

      // Refresh thread list
      agentApi.listSimulatorThreads().then(data => {
        setThreads(data.threads || [])
      }).catch(() => {})
    } catch (err) {
      console.error('Failed to send:', err)
      setErrorMsg(err instanceof Error ? err.message : 'Failed to send message. Check server logs.')
      setStatus('error')
    } finally {
      setSending(false)
    }
  }

  // Load messages for a thread from the backend (for switching to existing threads)
  const switchToThread = useCallback(async (threadId: string) => {
    if (pollingRef.current) clearTimeout(pollingRef.current)
    setActiveThreadId(threadId)
    setMessages([])
    setStatus('idle')
    setErrorMsg('')

    try {
      const data = await agentApi.getSimulatorMessages(threadId, 0)
      if (data.messages && data.messages.length > 0) {
        setMessages(data.messages
          .map(m => ({
            id: m.id,
            text: m.text,
            is_bot: m.is_bot,
            timestamp: m.timestamp,
          })))
      }
    } catch {
      // ignore
    }
  }, [])

  const handleNewThread = () => {
    if (pollingRef.current) clearTimeout(pollingRef.current)
    setActiveThreadId(null)
    setMessages([])
    setStatus('idle')
    setErrorMsg('')
    threadOffsetRef.current = 0
  }

  const handleReset = async () => {
    if (activeThreadId) {
      try {
        await agentApi.clearSimulatorThread(activeThreadId)
      } catch {
        // ignore
      }
    }
    if (pollingRef.current) clearTimeout(pollingRef.current)
    setMessages([])
    setActiveThreadId(null)
    setStatus('idle')
    setInput('')
    setErrorMsg('')
    threadOffsetRef.current = 0
  }

  const handleClose = () => {
    if (pollingRef.current) clearTimeout(pollingRef.current)
    onClose()
  }

  const handleModeChange = async (newMode: 'threaded' | 'non-threaded') => {
    setThreadMode(newMode)
    try {
      await agentApi.setSimulatorMode(newMode === 'threaded')
    } catch {
      setThreadMode(threadMode)
    }
  }


  if (!isOpen) return null

  const statusConfig: Record<SimStatus, { label: string; color: string }> = {
    idle: { label: 'Ready', color: 'bg-gray-500' },
    sending: { label: 'Starting...', color: 'bg-blue-500 animate-pulse' },
    running: { label: 'Running', color: 'bg-green-500 animate-pulse' },
    completed: { label: 'Completed', color: 'bg-green-600' },
    error: { label: 'Error / Cancelled', color: 'bg-red-500' },
  }

  const currentStatus = statusConfig[status]

  const formatTime = (ts: string) => {
    try {
      return new Date(ts).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    } catch {
      return ''
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div className="bg-background border border-border rounded-lg shadow-xl w-full max-w-4xl h-[600px] flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-border">
          <div className="flex items-center gap-3">
            <Bot className="w-5 h-5 text-primary" />
            <h2 className="text-base font-semibold">Bot Simulator</h2>
            <select
              value={threadMode}
              onChange={e => handleModeChange(e.target.value as 'threaded' | 'non-threaded')}
              className="text-xs px-2 py-1 bg-secondary border border-border rounded focus:outline-none focus:ring-1 focus:ring-primary"
            >
              <option value="non-threaded">Non-Threaded</option>
              <option value="threaded">Threaded</option>
            </select>
            <div className="flex items-center gap-1.5">
              <div className={`w-2 h-2 rounded-full ${currentStatus.color}`} />
              <span className="text-xs text-muted-foreground">{currentStatus.label}</span>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={handleReset}
              className="p-1.5 text-muted-foreground hover:text-foreground transition-colors rounded"
              title="Reset"
            >
              <RotateCcw className="w-4 h-4" />
            </button>
            <button
              onClick={handleClose}
              className="p-1.5 text-muted-foreground hover:text-foreground transition-colors rounded"
            >
              <X className="w-4 h-4" />
            </button>
          </div>
        </div>

        {/* Tier Config Bar — shows what's actually saved on the server */}
        <div className="px-4 py-1.5 border-b border-border bg-muted/30 flex items-center gap-2 text-[11px] text-muted-foreground">
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
              {/* Custom tiers */}
              {serverTierConfig.custom && typeof serverTierConfig.custom === 'object' &&
                Object.entries(serverTierConfig.custom as Record<string, { provider?: string; model_id?: string }>).map(([slug, cfg]) => {
                  if (!cfg?.model_id) return null
                  const label = cfg.provider ? `${cfg.provider}/${cfg.model_id}` : cfg.model_id
                  return (
                    <span key={slug} className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded bg-secondary border border-purple-500/30">
                      <span className="font-medium text-purple-400">{slug}:</span>
                      <span className="truncate max-w-[200px]">{label}</span>
                    </span>
                  )
                })
              }
            </>
          ) : (
            <span className="text-yellow-500">Not configured — uses the same tier config as multi-agent chat</span>
          )}
        </div>

        {/* Body: Sidebar + Messages */}
        <div className="flex-1 flex overflow-hidden">
          {/* Thread Sidebar */}
          <div className="w-48 border-r border-border flex flex-col overflow-hidden">
            <div className="px-3 py-2 border-b border-border flex items-center justify-between">
              <span className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Threads</span>
              <button
                onClick={handleNewThread}
                className="p-1 text-muted-foreground hover:text-foreground transition-colors rounded hover:bg-muted"
                title="New Thread"
              >
                <Plus className="w-3.5 h-3.5" />
              </button>
            </div>
            <div className="flex-1 overflow-y-auto">
              {threads.length === 0 ? (
                <div className="p-3 text-xs text-muted-foreground text-center">No threads yet</div>
              ) : (
                threads.map(t => (
                  <button
                    key={t.thread_id}
                    onClick={() => switchToThread(t.thread_id)}
                    className={`w-full text-left px-3 py-2 border-b border-border/50 hover:bg-muted/50 transition-colors ${
                      activeThreadId === t.thread_id ? 'bg-muted' : ''
                    }`}
                  >
                    <div className="flex items-center gap-1.5 mb-0.5">
                      <MessageSquare className="w-3 h-3 text-muted-foreground flex-shrink-0" />
                      <span className="text-xs truncate text-foreground">
                        {t.preview || '(no message)'}
                      </span>
                    </div>
                    <div className="flex items-center justify-between text-[10px] text-muted-foreground">
                      <span>{formatTime(t.created_at)}</span>
                      <span>{t.message_count} msg{t.message_count !== 1 ? 's' : ''}</span>
                    </div>
                  </button>
                ))
              )}
            </div>
          </div>

          {/* Messages Panel */}
          <div className="flex-1 flex flex-col overflow-hidden">
            <div className="flex-1 overflow-y-auto p-4 space-y-3">
              {messages.length === 0 && (
                <div className="flex flex-col items-center justify-center h-full text-muted-foreground text-sm gap-2">
                  <Bot className="w-8 h-8 opacity-50" />
                  <p>Send a message to test the bot flow</p>
                  <p className="text-xs opacity-75">Messages start a session immediately — the agent handles everything.</p>
                </div>
              )}

              {messages.map((msg, idx) => (
                <div key={msg.id || idx} className={`flex gap-2 ${msg.is_bot ? '' : 'justify-end'}`}>
                  {msg.is_bot && (
                    <div className="w-7 h-7 rounded-full bg-primary/10 flex items-center justify-center flex-shrink-0 mt-0.5">
                      <Bot className="w-3.5 h-3.5 text-primary" />
                    </div>
                  )}
                  <div className={`max-w-[80%] ${msg.is_bot ? '' : 'order-first'}`}>
                    {msg.is_bot ? (
                      <div className="px-3 py-2 rounded-lg text-sm bg-muted text-foreground">
                        <MarkdownRenderer content={msg.text} maxHeight="none" />
                      </div>
                    ) : (
                      <div className="px-3 py-2 rounded-lg text-sm whitespace-pre-wrap bg-primary text-primary-foreground">
                        {msg.text}
                      </div>
                    )}
                  </div>
                  {!msg.is_bot && (
                    <div className="w-7 h-7 rounded-full bg-secondary flex items-center justify-center flex-shrink-0 mt-0.5">
                      <User className="w-3.5 h-3.5 text-muted-foreground" />
                    </div>
                  )}
                </div>
              ))}

              {(() => {
                const lastMsg = messages[messages.length - 1]
                const waitingForBot = status === 'running' && lastMsg && !lastMsg.is_bot
                const initialWait = status === 'running' && !messages.some(m => m.is_bot)
                if (status === 'sending' || waitingForBot || initialWait) {
                  return (
                    <div className="flex gap-2">
                      <div className="w-7 h-7 rounded-full bg-primary/10 flex items-center justify-center flex-shrink-0">
                        <Loader2 className="w-3.5 h-3.5 text-primary animate-spin" />
                      </div>
                      <div className="px-3 py-2 rounded-lg bg-muted text-muted-foreground text-sm">
                        Agent is working...
                      </div>
                    </div>
                  )
                }
                return null
              })()}

              {errorMsg && (
                <div className="text-xs text-red-500 text-center py-1">{errorMsg}</div>
              )}

              <div ref={messagesEndRef} />
            </div>

            {/* Input */}
            <div className="px-4 py-3 border-t border-border">
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
                <input
                  type="text"
                  value={input}
                  onChange={e => setInput(e.target.value)}
                  onKeyDown={e => e.key === 'Enter' && !e.shiftKey && handleSend()}
                  placeholder="Type a message... (@status, @full, @concise)"
                  disabled={sending}
                  className="flex-1 px-3 py-2 text-sm bg-secondary border border-border rounded-md focus:outline-none focus:ring-1 focus:ring-primary disabled:opacity-50"
                />
                <button
                  onClick={handleSend}
                  disabled={!input.trim() || sending}
                  className="px-3 py-2 bg-primary text-primary-foreground rounded-md hover:bg-primary/90 transition-colors disabled:opacity-50 flex items-center gap-1.5"
                >
                  {sending ? <Loader2 className="w-4 h-4 animate-spin" /> : <Send className="w-4 h-4" />}
                </button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
