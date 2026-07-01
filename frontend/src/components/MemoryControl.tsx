import React, { useCallback, useEffect, useState } from 'react'
import { Brain, ExternalLink, X } from 'lucide-react'
import { schedulerApi } from '../api/scheduler'
import type { ScheduledJob } from '../services/api-types'
import { useAppStore } from '../stores/useAppStore'
import { useChatStore } from '../stores/useChatStore'
import ModalPortal from './ui/ModalPortal'

const MEMORY_JOB_ID = 'builtin-auto-enrich-memory'
const MEMORY_SETUP_SLASH_COMMAND = '/memory-setup '

const relTime = (iso?: string): string => {
  if (!iso) return 'never'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return 'unknown'
  const diff = Date.now() - d.getTime()
  const m = Math.floor(diff / 60000)
  if (m < 1) return 'just now'
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  return d.toLocaleDateString()
}

export const MemoryControl: React.FC = () => {
  const [job, setJob] = useState<ScheduledJob | null>(null)
  const [open, setOpen] = useState(false)
  const activeTabId = useChatStore(s => s.activeTabId)
  const setTabConfig = useChatStore(s => s.setTabConfig)
  const addToast = useChatStore(s => s.addToast)
  const setWorkspaceMinimized = useAppStore(s => s.setWorkspaceMinimized)
  const setMultiAgentRightPanelView = useAppStore(s => s.setMultiAgentRightPanelView)

  const load = useCallback(async () => {
    try {
      const resp = await schedulerApi.listJobs({ mode: 'multi-agent' })
      setJob((resp.jobs || []).find(j => j.id === MEMORY_JOB_ID) || null)
    } catch { /* leave last known state */ }
  }, [])

  useEffect(() => { void load() }, [load])

  const enabled = !!job?.enabled
  const hasRun = !!job?.last_run_at

  const runMemorySetup = useCallback(() => {
    if (!activeTabId) {
      addToast('Open a Chief of Staff chat and type /memory-setup.', 'info')
      setOpen(false)
      return
    }
    setWorkspaceMinimized(false)
    setMultiAgentRightPanelView('memory')
    setTabConfig(activeTabId, { inputText: MEMORY_SETUP_SLASH_COMMAND })
    setOpen(false)
  }, [activeTabId, addToast, setMultiAgentRightPanelView, setTabConfig, setWorkspaceMinimized])

  const openMemory = useCallback(() => {
    setMultiAgentRightPanelView('memory')
    setWorkspaceMinimized(false)
    setOpen(false)
  }, [setMultiAgentRightPanelView, setWorkspaceMinimized])

  return (
    <>
      <button
        onClick={() => setOpen(true)}
        data-testid="memory-control-button"
        className="inline-flex items-center gap-1.5 rounded-lg border border-border bg-background/90 px-2 py-1 text-[11px] font-medium text-muted-foreground shadow-sm backdrop-blur-sm transition-colors hover:bg-muted"
        title="Memory — Chief of Staff cross-session memory"
      >
        <Brain className={`h-3.5 w-3.5 ${enabled ? 'text-primary' : ''}`} />
        <span className="hidden sm:inline">Memory</span>
        <span className={`text-[10px] font-semibold tracking-wide ${enabled ? 'text-primary' : 'text-muted-foreground/60'}`}>
          {enabled ? 'ON' : 'OFF'}
        </span>
      </button>

      {open && (
        <ModalPortal>
          <div className="fixed inset-0 z-[9999] flex items-center justify-center bg-black/50 p-4" onClick={() => setOpen(false)}>
            <div className="w-full max-w-md rounded-lg border border-border bg-background shadow-xl" onClick={e => e.stopPropagation()}>
              <div className="flex items-center justify-between border-b border-border px-5 py-3.5">
                <div className="flex items-center gap-2">
                  <Brain className="h-4 w-4 text-primary" />
                  <h2 className="text-sm font-semibold text-foreground">Memory</h2>
                </div>
                <button onClick={() => setOpen(false)} className="rounded-md p-1 text-muted-foreground hover:bg-muted hover:text-foreground" aria-label="Close">
                  <X className="h-4 w-4" />
                </button>
              </div>

              <div className="space-y-3 px-5 py-4 text-sm text-muted-foreground">
                <div className="flex items-center justify-between gap-3 rounded-md border border-border bg-muted/30 px-3 py-3">
                  <div>
                    <div className="text-sm font-medium text-foreground">Automatic memory enrichment</div>
                    <div className="text-xs text-muted-foreground">
                      {enabled
                        ? `On · ${job?.next_run_at ? `next ${relTime(job.next_run_at)}` : 'scheduled'} · last run ${relTime(job?.last_run_at)}`
                        : 'Off — not enriching chats automatically'}
                    </div>
                  </div>
                  <span className={`flex-none rounded-full border px-2 py-0.5 text-[10px] font-semibold tracking-wide ${
                    enabled
                      ? 'border-primary/40 bg-primary/10 text-primary'
                      : 'border-border bg-background text-muted-foreground'
                  }`}>
                    {enabled ? 'ON' : 'OFF'}
                  </span>
                </div>

                <p>When <span className="font-medium text-foreground">on</span>, Chief of Staff periodically distills durable lessons from recent chats into its memory index and entity notes.</p>
                <p>The scheduler checks for changed chat history before starting the agent, so no LLM runs when there is nothing new to enrich.</p>
                <p>Memory is for durable user preferences, decisions, recurring patterns, and org lessons. It is not a live facts cache for files, metrics, or current workflow state.</p>
              </div>

              <div className="space-y-2 border-t border-border px-5 py-4">
                <button
                  onClick={runMemorySetup}
                  className="inline-flex w-full items-center justify-center gap-1.5 rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground hover:bg-primary/90"
                >
                  Use /memory-setup in chat
                </button>
                <button
                  onClick={openMemory}
                  className="inline-flex w-full items-center justify-center gap-1.5 rounded-md border border-border px-3 py-1.5 text-xs font-medium text-foreground hover:bg-muted"
                >
                  <ExternalLink className="h-3.5 w-3.5" />
                  Open memory
                </button>
                {!hasRun && (
                  <div className="rounded-md bg-muted/60 px-3 py-2.5 text-xs text-muted-foreground">
                    No automatic memory run yet. Use <code className="rounded bg-background px-1 py-0.5 font-medium text-foreground">/memory-setup</code> to configure it, or <code className="rounded bg-background px-1 py-0.5 font-medium text-foreground">/enrich-memory</code> for a one-time run.
                  </div>
                )}
              </div>
            </div>
          </div>
        </ModalPortal>
      )}
    </>
  )
}

export default MemoryControl
