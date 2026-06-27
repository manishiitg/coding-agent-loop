import React, { useCallback, useEffect, useState } from 'react'
import { AlertCircle, Cloud, Download, GitBranch, HardDrive, Info, Loader2, RefreshCw, X } from 'lucide-react'
import type { WorkflowBackupInfoResponse, WorkflowBackupStrategyInfo } from '../../services/api-types'
import ModalPortal from '../ui/ModalPortal'
import { formatBackupStateLabel, getBackupStateVisual } from '../workflow/backupStatus'
import {
  backupDestinationTitle,
  compactHash,
  coverageText,
  extractErrorMessage,
  findBackupDestinationStatus,
  formatRelativeTime,
} from './popupUtils'

type BackupSetupAction = {
  label: React.ReactNode
  onClick?: () => void
}

type BackupExportAction = {
  label: string
  filename: string
  exportBlob: () => Promise<Blob>
}

export interface BackupPopupProps {
  isOpen: boolean
  onClose: () => void
  loadInfo: () => Promise<WorkflowBackupInfoResponse>
  onStateLoaded?: (state: string) => void
  fallbackStrategies: WorkflowBackupStrategyInfo[]
  subtitle: string
  emptyDestinationsText: string
  destinationsHelp: string
  statusPathFallback: string
  setupAction: BackupSetupAction
  getSummary: (info: WorkflowBackupInfoResponse | null) => string
  loadErrorMessage?: string
  showEnabledBadge?: boolean
  exportAction?: BackupExportAction
}

const iconButtonClass = 'inline-flex h-8 w-8 items-center justify-center rounded-md border border-border text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:opacity-50'
const actionClass = 'inline-flex items-center gap-1 rounded-md border border-border bg-muted/40 px-2.5 py-1.5 text-[11px] text-muted-foreground transition-colors hover:bg-muted'

const BackupPopup: React.FC<BackupPopupProps> = ({
  isOpen,
  onClose,
  loadInfo,
  onStateLoaded,
  fallbackStrategies,
  subtitle,
  emptyDestinationsText,
  destinationsHelp,
  statusPathFallback,
  setupAction,
  getSummary,
  loadErrorMessage = 'Failed to load backup status',
  showEnabledBadge = false,
  exportAction,
}) => {
  const [loading, setLoading] = useState(false)
  const [info, setInfo] = useState<WorkflowBackupInfoResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [isExporting, setIsExporting] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const resp = await loadInfo()
      setInfo(resp)
      onStateLoaded?.(resp.effective_state || 'not_configured')
    } catch (err) {
      setError(extractErrorMessage(err, loadErrorMessage))
    } finally {
      setLoading(false)
    }
  }, [loadErrorMessage, loadInfo, onStateLoaded])

  useEffect(() => {
    if (isOpen) void load()
  }, [isOpen, load])

  const handleExport = async () => {
    if (!exportAction) return
    setIsExporting(true)
    setError(null)
    try {
      const blob = await exportAction.exportBlob()
      const url = URL.createObjectURL(blob)
      const anchor = document.createElement('a')
      anchor.href = url
      anchor.download = exportAction.filename
      document.body.appendChild(anchor)
      anchor.click()
      anchor.remove()
      URL.revokeObjectURL(url)
    } catch (err) {
      setError(extractErrorMessage(err, 'Failed to export local ZIP'))
    } finally {
      setIsExporting(false)
    }
  }

  if (!isOpen) return null

  const state = info?.effective_state || 'not_configured'
  const visual = getBackupStateVisual(state)
  const StateIcon = visual.Icon
  const configEnabled = Boolean(info?.config?.enabled)
  const destinations = info?.config?.destinations || []
  const supportedStrategies = info?.supported?.length ? info.supported : fallbackStrategies
  const setupControl = setupAction.onClick ? (
    <button type="button" onClick={setupAction.onClick} className={actionClass}>
      {setupAction.label}
    </button>
  ) : (
    <span className={actionClass}>{setupAction.label}</span>
  )

  return (
    <ModalPortal>
      <div className="fixed inset-0 z-[9999] flex items-center justify-center bg-black/50 p-2 backdrop-blur-sm sm:p-4">
        <div className="relative flex max-h-[calc(100dvh-1rem)] w-full max-w-4xl flex-col rounded-lg border border-border bg-background shadow-xl sm:max-h-[86vh]">
          <div className="flex items-start justify-between gap-3 border-b border-border px-4 py-3 sm:px-5 sm:py-3.5">
            <div className="min-w-0">
              <h2 className="flex items-center gap-2 text-base font-semibold text-foreground">
                <Cloud className="h-4 w-4 text-primary" />
                Backup
              </h2>
              <p className="mt-0.5 truncate text-xs text-muted-foreground">{subtitle}</p>
            </div>
            <button onClick={onClose} className="rounded-md p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground" aria-label="Close">
              <X className="h-4 w-4" />
            </button>
          </div>

          {error && (
            <div className="flex items-center gap-2 bg-destructive/10 px-5 py-2 text-xs text-destructive">
              <AlertCircle className="h-3.5 w-3.5 flex-shrink-0" />
              <span className="min-w-0 flex-1">{error}</span>
              <button onClick={() => setError(null)} className="text-destructive/70 hover:text-destructive" aria-label="Dismiss error">
                <X className="h-3 w-3" />
              </button>
            </div>
          )}

          <div className="flex-1 overflow-y-auto px-4 py-4 sm:px-5">
            {loading && !info ? (
              <div className="flex items-center justify-center py-12">
                <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
              </div>
            ) : (
              <div className="space-y-4">
                <section className="overflow-hidden rounded-md border border-border">
                  <div className="flex flex-col gap-3 bg-muted/30 px-4 py-4 sm:flex-row sm:items-start sm:justify-between">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <StateIcon className={`h-4 w-4 ${visual.icon}`} />
                        {showEnabledBadge && (
                          <span className={`inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium ${
                            configEnabled
                              ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
                              : 'border-border bg-background text-muted-foreground'
                          }`}>
                            {configEnabled ? 'Enabled' : 'Disabled'}
                          </span>
                        )}
                        <span className={`inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium ${visual.badge}`}>
                          {formatBackupStateLabel(state)}
                        </span>
                      </div>
                      <h3 className="mt-2 text-base font-semibold text-foreground">Remote backup</h3>
                      <p className="mt-1 max-w-2xl text-sm text-muted-foreground">{getSummary(info)}</p>
                      {info?.status?.last_error && <p className="mt-2 text-xs text-destructive">{info.status.last_error}</p>}
                    </div>
                    <div className="flex flex-wrap items-center gap-2">
                      <button onClick={() => { void load() }} disabled={loading} className={iconButtonClass} aria-label="Refresh backup status">
                        <RefreshCw className={`h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`} />
                      </button>
                      {setupControl}
                    </div>
                  </div>

                  <div className="grid border-t border-border text-sm sm:grid-cols-4">
                    <div className="border-b border-border px-4 py-3 sm:border-b-0 sm:border-r">
                      <div className="text-xs text-muted-foreground">Last success</div>
                      <div className="mt-1 font-medium text-foreground">{formatRelativeTime(info?.status?.last_success_at)}</div>
                    </div>
                    <div className="border-b border-border px-4 py-3 sm:border-b-0 sm:border-r">
                      <div className="text-xs text-muted-foreground">Last attempt</div>
                      <div className="mt-1 font-medium text-foreground">{formatRelativeTime(info?.status?.last_attempt_at)}</div>
                    </div>
                    <div className="border-b border-border px-4 py-3 sm:border-b-0 sm:border-r">
                      <div className="text-xs text-muted-foreground">Tracked files</div>
                      <div className="mt-1 font-medium text-foreground">{info?.tracked_files_count ?? 0}</div>
                    </div>
                    <div className="px-4 py-3">
                      <div className="text-xs text-muted-foreground">Source hash</div>
                      <div className="mt-1 truncate font-mono text-xs text-foreground" title={info?.current_source_hash || ''}>
                        {compactHash(info?.current_source_hash)}
                      </div>
                    </div>
                  </div>
                </section>

                <section className="rounded-md border border-border">
                  <div className="flex items-center justify-between gap-3 border-b border-border px-4 py-3">
                    <div>
                      <h3 className="text-sm font-semibold text-foreground">Configured destinations</h3>
                      <p className="mt-0.5 text-xs text-muted-foreground">{destinationsHelp}</p>
                    </div>
                    <span className="rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground">{destinations.length}</span>
                  </div>
                  {destinations.length === 0 ? (
                    <div className="px-4 py-5 text-sm text-muted-foreground">{emptyDestinationsText}</div>
                  ) : (
                    <div className="divide-y divide-border">
                      {destinations.map(destination => {
                        const status = findBackupDestinationStatus(info?.status?.destinations, destination)
                        const destinationState = status?.state || 'configured_not_verified'
                        const destinationVisual = getBackupStateVisual(destinationState)
                        const DestinationIcon = destinationVisual.Icon
                        return (
                          <div key={destination.id || backupDestinationTitle(destination)} className="flex flex-col gap-2 px-4 py-3 sm:flex-row sm:items-start sm:justify-between">
                            <div className="min-w-0">
                              <div className="flex flex-wrap items-center gap-2">
                                <DestinationIcon className={`h-3.5 w-3.5 ${destinationVisual.icon}`} />
                                <span className="text-sm font-medium text-foreground">{destination.id || 'Destination'}</span>
                                <span className="rounded bg-muted px-1.5 py-0.5 text-[11px] uppercase tracking-wide text-muted-foreground">{destination.provider || destination.type}</span>
                              </div>
                              <div className="mt-1 truncate text-xs text-muted-foreground">{backupDestinationTitle(destination)}</div>
                              <div className="mt-1 text-xs text-muted-foreground">Covers: {coverageText(destination.covers)}</div>
                              {status?.summary && <div className="mt-1 text-xs text-foreground">{status.summary}</div>}
                              {status?.error && <div className="mt-1 text-xs text-destructive">{status.error}</div>}
                            </div>
                            <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground sm:justify-end">
                              <span className={`rounded-full border px-2 py-0.5 ${destinationVisual.badge}`}>{formatBackupStateLabel(destinationState)}</span>
                              {status?.commit && <span className="font-mono">{status.commit.slice(0, 8)}</span>}
                              {typeof status?.objects_synced === 'number' && status.objects_synced > 0 && <span>{status.objects_synced} objects</span>}
                              {status?.last_success_at && <span>{formatRelativeTime(status.last_success_at)}</span>}
                            </div>
                          </div>
                        )
                      })}
                    </div>
                  )}
                </section>

                <div className={exportAction ? 'grid gap-3 lg:grid-cols-[1fr_280px]' : ''}>
                  <section className="rounded-md border border-border">
                    <div className="border-b border-border px-4 py-3">
                      <h3 className="text-sm font-semibold text-foreground">Supported strategies</h3>
                    </div>
                    <div className="grid divide-y divide-border md:grid-cols-2 md:divide-x md:divide-y-0">
                      {supportedStrategies.map((strategy) => (
                        <div key={strategy.id} className="px-4 py-3">
                          <div className="flex items-center gap-2 text-sm font-medium text-foreground">
                            {strategy.id === 'git' ? <GitBranch className="h-3.5 w-3.5 text-muted-foreground" /> : <Cloud className="h-3.5 w-3.5 text-muted-foreground" />}
                            {strategy.label}
                          </div>
                          <p className="mt-1 text-xs leading-5 text-muted-foreground">{strategy.description}</p>
                        </div>
                      ))}
                    </div>
                  </section>

                  {exportAction && (
                    <section className="rounded-md border border-border px-4 py-3">
                      <div className="flex items-center gap-2 text-sm font-semibold text-foreground">
                        <HardDrive className="h-3.5 w-3.5 text-muted-foreground" />
                        Local export
                      </div>
                      <p className="mt-2 text-xs leading-5 text-muted-foreground">
                        ZIP export is manual recovery. It does not replace a remote backup destination.
                      </p>
                      <button
                        onClick={() => { void handleExport() }}
                        disabled={isExporting}
                        className="mt-3 inline-flex w-full items-center justify-center gap-1.5 rounded-md border border-border px-3 py-1.5 text-xs font-medium text-foreground hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        {isExporting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Download className="h-3.5 w-3.5" />}
                        {exportAction.label}
                      </button>
                      <div className="mt-3 truncate text-[11px] text-muted-foreground" title={info?.status_path || ''}>
                        Status: {info?.status_path || statusPathFallback}
                      </div>
                    </section>
                  )}
                </div>

                {!exportAction && (
                  <p className="flex items-center gap-1 px-1 text-[11px] text-muted-foreground">
                    <Info className="h-3 w-3" />
                    Status: {info?.status_path || statusPathFallback}
                  </p>
                )}
              </div>
            )}
          </div>
        </div>
      </div>
    </ModalPortal>
  )
}

export default BackupPopup
