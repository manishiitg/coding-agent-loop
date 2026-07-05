import React, { useCallback, useEffect, useState } from 'react'
import { Activity, Cloud, ExternalLink, Globe, X } from 'lucide-react'
import { schedulerApi } from '../api/scheduler'
import { agentApi } from '../services/api'
import type { ScheduledJob } from '../services/api-types'
import { useAppStore } from '../stores/useAppStore'
import { useChatStore } from '../stores/useChatStore'
import ModalPortal from './ui/ModalPortal'
import { formatBackupStateLabel, getBackupDotClass } from './workflow/backupStatus'
import { formatPublishStateLabel, getPublishDotClass } from './workflow/publishStatus'

// The Org Pulse daily pass is a built-in multi-agent schedule. Enabling and
// tuning it is intentionally guided through /pulse-setup so cadence, goals, and
// backup/publish expectations are confirmed in chat instead of hidden behind a
// bare switch.
const ORG_PULSE_JOB_ID = 'builtin-org-pulse'
const ORG_PULSE_SETUP_SLASH_COMMAND = '/pulse-setup '

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

export const OrgPulseControl: React.FC = () => {
  const [job, setJob] = useState<ScheduledJob | null>(null)
  const [open, setOpen] = useState(false)
  const [backupState, setBackupState] = useState('not_configured')
  const [publishState, setPublishState] = useState('not_configured')
  const activeTabId = useChatStore(s => s.activeTabId)
  const setTabConfig = useChatStore(s => s.setTabConfig)
  const addToast = useChatStore(s => s.addToast)
  const setWorkspaceMinimized = useAppStore(s => s.setWorkspaceMinimized)
  const setMultiAgentRightPanelView = useAppStore(s => s.setMultiAgentRightPanelView)

  const load = useCallback(async () => {
    try {
      const resp = await schedulerApi.listJobs({ mode: 'multi-agent' })
      setJob((resp.jobs || []).find(j => j.id === ORG_PULSE_JOB_ID) || null)
    } catch { /* leave last known state */ }
  }, [])

  useEffect(() => { void load() }, [load])

  const loadOrgOps = useCallback(async () => {
    const [backup, publish] = await Promise.allSettled([
      agentApi.getOrgBackup(),
      agentApi.getOrgPublish()
    ])
    if (backup.status === 'fulfilled') {
      setBackupState(backup.value.effective_state || 'not_configured')
    }
    if (publish.status === 'fulfilled') {
      setPublishState(publish.value.effective_state || 'not_configured')
    }
  }, [])

  useEffect(() => { void loadOrgOps() }, [loadOrgOps])
  useEffect(() => {
    if (open) void loadOrgOps()
  }, [open, loadOrgOps])

  const enabled = !!job?.enabled
  const hasRun = !!job?.last_run_at

  const runPulseSetup = useCallback(() => {
    if (!activeTabId) {
      addToast('Open a Chief of Staff chat and type /pulse-setup.', 'info')
      setOpen(false)
      return
    }
    setWorkspaceMinimized(false)
    setTabConfig(activeTabId, { inputText: ORG_PULSE_SETUP_SLASH_COMMAND })
    setOpen(false)
  }, [activeTabId, addToast, setTabConfig, setWorkspaceMinimized])

  const openLog = useCallback(() => {
    setMultiAgentRightPanelView('org-pulse')
    setWorkspaceMinimized(false)
    setOpen(false)
  }, [setMultiAgentRightPanelView, setWorkspaceMinimized])

  return (
    <>
      <button
        onClick={() => setOpen(true)}
        data-testid="org-pulse-button"
        className="inline-flex items-center gap-1.5 rounded-lg border border-border bg-background/90 px-2 py-1 text-[11px] font-medium text-muted-foreground shadow-sm backdrop-blur-sm transition-colors hover:bg-muted"
        title="Org Pulse — the Chief of Staff's daily org log"
      >
        <Activity className={`h-3.5 w-3.5 ${enabled ? 'text-primary' : ''}`} />
        <span className="hidden sm:inline">Org Pulse</span>
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
                  <Activity className="h-4 w-4 text-primary" />
                  <h2 className="text-sm font-semibold text-foreground">Org Pulse</h2>
                </div>
                <button onClick={() => setOpen(false)} className="rounded-md p-1 text-muted-foreground hover:bg-muted hover:text-foreground" aria-label="Close">
                  <X className="h-4 w-4" />
                </button>
              </div>

              <div className="space-y-3 px-5 py-4 text-sm text-muted-foreground">
                <div className="flex items-center justify-between gap-3 rounded-md border border-border bg-muted/30 px-3 py-3">
                  <div>
                    <div className="text-sm font-medium text-foreground">Daily Org Pulse</div>
                    <div className="text-xs text-muted-foreground">
                      {enabled
                        ? `On · ${job?.next_run_at ? `next ${relTime(job.next_run_at)}` : 'scheduled daily'} · last run ${relTime(job?.last_run_at)}`
                        : 'Off — not reviewing the org'}
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
                <p>When <span className="font-medium text-foreground">on</span>, your Chief of Staff reviews the whole org <span className="font-medium text-foreground">once a day</span> — rolls up each workflow's health, uses task findings from <code className="rounded bg-muted px-1 py-0.5 font-medium text-foreground">pulse/task.html</code>, and records suggestions in a single log you open on the right.</p>
                <p>It only <span className="font-medium text-foreground">reviews and suggests</span> — it never changes a workflow. Acting on a suggestion (e.g. turning a repeated task into a workflow) is up to you.</p>
                <p>Use <code className="rounded bg-muted px-1 py-0.5 font-medium text-foreground">/org-backup</code> and <code className="rounded bg-muted px-1 py-0.5 font-medium text-foreground">/org-publish</code> to protect or share the org Goals/Pulse pages. They use the same config/status pattern as workflow backup and publish.</p>
                <div className="grid grid-cols-2 gap-2 pt-1">
                  <div className="rounded-md border border-border bg-muted/30 px-3 py-2">
                    <div className="flex items-center gap-2 text-xs font-medium text-foreground">
                      <span className="relative inline-flex">
                        <Cloud className="h-3.5 w-3.5 text-muted-foreground" />
                        <span className={`absolute -right-1 -top-1 h-2 w-2 rounded-full border border-background ${getBackupDotClass(backupState)}`} />
                      </span>
                      Backup
                    </div>
                    <div className="mt-1 text-[11px] text-muted-foreground">{formatBackupStateLabel(backupState)}</div>
                    <code className="mt-1 inline-block rounded bg-background px-1 py-0.5 text-[10px] text-foreground">/org-backup</code>
                  </div>
                  <div className="rounded-md border border-border bg-muted/30 px-3 py-2">
                    <div className="flex items-center gap-2 text-xs font-medium text-foreground">
                      <span className="relative inline-flex">
                        <Globe className="h-3.5 w-3.5 text-muted-foreground" />
                        <span className={`absolute -right-1 -top-1 h-2 w-2 rounded-full border border-background ${getPublishDotClass(publishState)}`} />
                      </span>
                      Publish
                    </div>
                    <div className="mt-1 text-[11px] text-muted-foreground">{formatPublishStateLabel(publishState)}</div>
                    <code className="mt-1 inline-block rounded bg-background px-1 py-0.5 text-[10px] text-foreground">/org-publish</code>
                  </div>
                </div>
              </div>

              <div className="space-y-2 border-t border-border px-5 py-4">
                <button
                  onClick={runPulseSetup}
                  className="inline-flex w-full items-center justify-center gap-1.5 rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground hover:bg-primary/90"
                >
                  Use /pulse-setup in chat
                </button>
                {hasRun ? (
                  <button
                    onClick={openLog}
                    className="inline-flex w-full items-center justify-center gap-1.5 rounded-md border border-border px-3 py-1.5 text-xs font-medium text-foreground hover:bg-muted"
                  >
                    <ExternalLink className="h-3.5 w-3.5" />
                    Open today's log
                  </button>
                ) : (
                  <div className="rounded-md bg-muted/60 px-3 py-2.5 text-xs text-muted-foreground">
                    No Org Pulse log yet. Use <code className="rounded bg-background px-1 py-0.5 font-medium text-foreground">/pulse-setup</code> in Chief of Staff to configure Daily Org Pulse.
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

export default OrgPulseControl
