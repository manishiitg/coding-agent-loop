import React, { useCallback, useEffect, useState } from 'react'
import { Activity, X, ExternalLink, Play } from 'lucide-react'
import { schedulerApi } from '../api/scheduler'
import type { ScheduledJob } from '../services/api-types'
import { useWorkspaceStore } from '../stores/useWorkspaceStore'
import ModalPortal from './ui/ModalPortal'

// The Org Pulse daily pass is a built-in multi-agent schedule; the toggle just
// enables/disables it (same as the Scheduled Tasks popup), and "Open log" shows
// its single HTML output on the right. Mirrors the workflow Pulse toggle.
const ORG_PULSE_JOB_ID = 'builtin-org-pulse'
const ORG_PULSE_LOG_PATH = 'pulse/org-pulse.html'

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
  const [saving, setSaving] = useState(false)
  const [triggered, setTriggered] = useState(false)
  const setSelectedFile = useWorkspaceStore(s => s.setSelectedFile)
  const setShowFileContent = useWorkspaceStore(s => s.setShowFileContent)

  const load = useCallback(async () => {
    try {
      const resp = await schedulerApi.listJobs({ mode: 'multi-agent' })
      setJob((resp.jobs || []).find(j => j.id === ORG_PULSE_JOB_ID) || null)
    } catch { /* leave last known state */ }
  }, [])

  useEffect(() => { void load() }, [load])

  const enabled = !!job?.enabled
  const hasRun = !!job?.last_run_at

  const toggle = useCallback(async () => {
    if (saving) return
    setSaving(true)
    try {
      const updated = enabled
        ? await schedulerApi.disableJob(ORG_PULSE_JOB_ID)
        : await schedulerApi.enableJob(ORG_PULSE_JOB_ID)
      setJob(updated)
    } catch {
      await load()
    } finally {
      setSaving(false)
    }
  }, [enabled, saving, load])

  const openLog = useCallback(() => {
    setSelectedFile({ name: 'Org Pulse', path: ORG_PULSE_LOG_PATH })
    setShowFileContent(true)
    setOpen(false)
  }, [setSelectedFile, setShowFileContent])

  const runNow = useCallback(async () => {
    try {
      await schedulerApi.triggerJob(ORG_PULSE_JOB_ID)
      setTriggered(true)
    } catch { /* ignore — surfaced via no state change */ }
  }, [])

  return (
    <>
      <button
        onClick={() => setOpen(true)}
        data-testid="org-pulse-button"
        className="flex items-center gap-1 px-2 py-1 text-xs rounded transition-colors text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-700"
        title="Org Pulse — the Chief of Staff's daily org log"
      >
        <Activity className={`w-4 h-4 ${enabled ? 'text-primary' : ''}`} />
        <span className="hidden sm:inline">Org Pulse</span>
        <span className={`text-[10px] font-semibold tracking-wide ${enabled ? 'text-primary' : 'text-gray-400 dark:text-gray-500'}`}>
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
                <p>When <span className="font-medium text-foreground">on</span>, your Chief of Staff reviews the whole org <span className="font-medium text-foreground">once a day</span> — rolls up each workflow's health, harvests reports &amp; learnings into memory, and records suggestions in a single log you open on the right.</p>
                <p>It only <span className="font-medium text-foreground">reviews and suggests</span> — it never changes a workflow. Acting on a suggestion (e.g. turning a repeated task into a workflow) is up to you.</p>
              </div>

              {/* enable / disable */}
              <div className="flex items-center justify-between border-t border-border px-5 py-3.5">
                <div>
                  <div className="text-sm font-medium text-foreground">Daily Org Pulse</div>
                  <div className="text-xs text-muted-foreground">
                    {enabled
                      ? `On · ${job?.next_run_at ? `next ${relTime(job.next_run_at)}` : 'scheduled daily'} · last run ${relTime(job?.last_run_at)}`
                      : 'Off — not reviewing the org'}
                  </div>
                </div>
                <button
                  type="button"
                  role="switch"
                  aria-checked={enabled}
                  onClick={() => { void toggle() }}
                  disabled={saving}
                  className={`relative inline-flex h-5 w-9 flex-none items-center rounded-full p-0 transition-colors disabled:opacity-50 ${enabled ? 'bg-primary' : 'bg-muted-foreground/30'}`}
                  aria-label="Toggle Org Pulse"
                >
                  <span className={`inline-block h-4 w-4 rounded-full bg-white shadow-sm transition-transform ${enabled ? 'translate-x-[18px]' : 'translate-x-[2px]'}`} />
                </button>
              </div>

              {/* view log / empty state */}
              <div className="border-t border-border px-5 py-4">
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
                    {triggered ? (
                      <span>Org Pulse is running now — its log will appear here once it finishes.</span>
                    ) : (
                      <div className="flex items-center justify-between gap-3">
                        <span className="min-w-0">No Org Pulse log yet — it posts after its first run.</span>
                        <button
                          onClick={() => { void runNow() }}
                          className="inline-flex flex-none items-center gap-1.5 rounded-md border border-border bg-background px-2.5 py-1 text-[11px] font-medium text-foreground hover:bg-muted"
                        >
                          <Play className="h-3 w-3" />
                          Run now
                        </button>
                      </div>
                    )}
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
