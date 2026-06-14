import React, { useEffect, useState, useCallback } from 'react'
import {
  X,
  Loader2,
  Tag,
  Trash2,
  History,
  Plus,
  Package,
  AlertCircle,
  Cloud,
  Download,
  RefreshCw,
  CheckCircle2,
  XCircle,
  Clock3,
  Info,
  GitBranch,
  HardDrive,
  Play,
  Settings2,
  AlertTriangle
} from 'lucide-react'
import { agentApi } from '../../services/api'
import type {
  WorkflowBackupDestination,
  WorkflowBackupDestinationStatus,
  WorkflowBackupInfoResponse,
  WorkflowBackupStrategyInfo,
  WorkflowVersionMeta
} from '../../services/api-types'
import ConfirmationDialog from '../ui/ConfirmationDialog'
import ModalPortal from '../ui/ModalPortal'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../ui/tooltip'

interface WorkflowVersionsPopupProps {
  isOpen: boolean
  onClose: () => void
  workspacePath: string | null
  onRefresh?: () => Promise<void>
}

type ActiveView = 'backup' | 'versions'

const FALLBACK_SUPPORTED_STRATEGIES: WorkflowBackupStrategyInfo[] = [
  {
    id: 'git',
    label: 'Git / GitHub',
    description: 'Best for workflow config, planning, knowledgebase, learnings, scripts, and small JSON data.',
    best_for: ['workflow', 'planning', 'knowledgebase', 'learnings']
  },
  {
    id: 'object_store',
    label: 'R2 / S3 / B2',
    description: 'Best for run folders, generated media, large artifacts, and files that should not live in git.',
    best_for: ['runs', 'large-artifacts', 'media']
  },
  {
    id: 'huggingface',
    label: 'HuggingFace Hub',
    description: 'Best for dataset/model-style backups, generated media, and revisioned ML artifacts.',
    best_for: ['datasets', 'models', 'media']
  },
  {
    id: 'local_zip',
    label: 'Local ZIP export',
    description: 'Manual full-folder export for transfer or recovery. This is not automatic remote backup.',
    best_for: ['manual-export', 'restore']
  }
]

const formatRelativeTime = (dateStr?: string): string => {
  if (!dateStr) return 'Never'
  const date = new Date(dateStr)
  if (Number.isNaN(date.getTime())) return 'Unknown'
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffSec = Math.floor(diffMs / 1000)
  const diffMin = Math.floor(diffSec / 60)
  const diffHr = Math.floor(diffMin / 60)
  const diffDay = Math.floor(diffHr / 24)

  if (diffSec < 60) return 'just now'
  if (diffMin < 60) return `${diffMin}m ago`
  if (diffHr < 24) return `${diffHr}h ago`
  if (diffDay < 30) return `${diffDay}d ago`
  return date.toLocaleDateString()
}

const extractErrorMessage = (err: unknown, fallback: string): string => {
  const maybe = err as { response?: { data?: { error?: string; message?: string } | string }; message?: string }
  const data = maybe.response?.data
  if (typeof data === 'string') return data
  return data?.message || data?.error || maybe.message || fallback
}

const compactHash = (hash?: string): string => {
  if (!hash) return 'Not tracked'
  return hash.length > 12 ? `${hash.slice(0, 12)}...` : hash
}

const formatStateLabel = (state?: string): string => {
  switch (state) {
    case 'not_configured':
      return 'Not configured'
    case 'configured_not_verified':
      return 'Needs verification'
    case 'running':
      return 'Running'
    case 'healthy':
      return 'Healthy'
    case 'stale':
      return 'Changed since backup'
    case 'partial':
      return 'Partial'
    case 'failed':
      return 'Failed'
    case 'skipped':
      return 'Skipped'
    default:
      return state || 'Unknown'
  }
}

const getStateVisual = (state?: string) => {
  switch (state) {
    case 'healthy':
      return {
        Icon: CheckCircle2,
        badge: 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300',
        icon: 'text-emerald-600 dark:text-emerald-300'
      }
    case 'running':
      return {
        Icon: Clock3,
        badge: 'border-sky-500/30 bg-sky-500/10 text-sky-700 dark:text-sky-300',
        icon: 'text-sky-600 dark:text-sky-300'
      }
    case 'stale':
    case 'partial':
    case 'configured_not_verified':
      return {
        Icon: AlertTriangle,
        badge: 'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300',
        icon: 'text-amber-600 dark:text-amber-300'
      }
    case 'failed':
      return {
        Icon: XCircle,
        badge: 'border-destructive/30 bg-destructive/10 text-destructive',
        icon: 'text-destructive'
      }
    default:
      return {
        Icon: Info,
        badge: 'border-border bg-muted text-muted-foreground',
        icon: 'text-muted-foreground'
      }
  }
}

const getBackupSummary = (backupInfo: WorkflowBackupInfoResponse | null): string => {
  const state = backupInfo?.effective_state
  if (!backupInfo?.config?.enabled) {
    return 'No remote backup strategy is enabled in workflow.json yet.'
  }
  if (backupInfo.status?.summary) return backupInfo.status.summary
  switch (state) {
    case 'configured_not_verified':
      return 'Backup is configured, but the builder has not verified a successful run yet.'
    case 'running':
      return 'A builder backup task is running and will update backup/status.json.'
    case 'stale':
      return 'The workflow changed since the last healthy backup.'
    default:
      return 'Backup status is waiting for the builder to update backup/status.json.'
  }
}

const coverageText = (items?: string[]): string => {
  if (!items || items.length === 0) return 'Coverage not specified'
  return items.join(', ')
}

const destinationTitle = (destination: WorkflowBackupDestination): string => {
  if (destination.repo) return destination.repo
  if (destination.bucket) return destination.prefix ? `${destination.bucket}/${destination.prefix}` : destination.bucket
  return destination.id || destination.provider || destination.type
}

const findDestinationStatus = (
  statuses: WorkflowBackupDestinationStatus[] | undefined,
  destination: WorkflowBackupDestination
): WorkflowBackupDestinationStatus | undefined => {
  return statuses?.find((status) => status.id === destination.id)
}

const WorkflowVersionsPopup: React.FC<WorkflowVersionsPopupProps> = ({
  isOpen,
  onClose,
  workspacePath,
  onRefresh
}) => {
  const [activeView, setActiveView] = useState<ActiveView>('backup')
  const [loadingVersions, setLoadingVersions] = useState(false)
  const [loadingBackup, setLoadingBackup] = useState(false)
  const [versions, setVersions] = useState<WorkflowVersionMeta[]>([])
  const [backupInfo, setBackupInfo] = useState<WorkflowBackupInfoResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)

  const [showPublishForm, setShowPublishForm] = useState(false)
  const [publishLabel, setPublishLabel] = useState('')
  const [isPublishing, setIsPublishing] = useState(false)

  const [revertVersion, setRevertVersion] = useState<WorkflowVersionMeta | null>(null)
  const [isReverting, setIsReverting] = useState(false)

  const [deleteVersion, setDeleteVersion] = useState<WorkflowVersionMeta | null>(null)
  const [isDeleting, setIsDeleting] = useState(false)

  const [backupAction, setBackupAction] = useState<'backup' | 'configure' | null>(null)
  const [isExportingZip, setIsExportingZip] = useState(false)

  const loadVersions = useCallback(async () => {
    if (!workspacePath) return
    setLoadingVersions(true)
    setError(null)
    try {
      const resp = await agentApi.listVersions(workspacePath)
      setVersions(resp.versions || [])
    } catch (err) {
      setError(extractErrorMessage(err, 'Failed to load versions'))
      console.error('Failed to load versions:', err)
    } finally {
      setLoadingVersions(false)
    }
  }, [workspacePath])

  const loadBackup = useCallback(async () => {
    if (!workspacePath) return
    setLoadingBackup(true)
    setError(null)
    try {
      const resp = await agentApi.getWorkflowBackup(workspacePath)
      setBackupInfo(resp)
    } catch (err) {
      setError(extractErrorMessage(err, 'Failed to load backup status'))
      console.error('Failed to load backup status:', err)
    } finally {
      setLoadingBackup(false)
    }
  }, [workspacePath])

  useEffect(() => {
    if (isOpen && workspacePath) {
      setActiveView('backup')
      setNotice(null)
      loadBackup()
      loadVersions()
    }
  }, [isOpen, workspacePath, loadBackup, loadVersions])

  const handlePublish = async () => {
    if (!workspacePath || !publishLabel.trim()) return
    setIsPublishing(true)
    setError(null)
    try {
      await agentApi.publishVersion(workspacePath, publishLabel.trim())
      setPublishLabel('')
      setShowPublishForm(false)
      await loadVersions()
    } catch (err) {
      setError(extractErrorMessage(err, 'Failed to publish version'))
      console.error('Failed to publish version:', err)
    } finally {
      setIsPublishing(false)
    }
  }

  const handleRevert = async () => {
    if (!workspacePath || !revertVersion) return
    setIsReverting(true)
    setError(null)
    try {
      await agentApi.revertToVersion(workspacePath, revertVersion.version)
      setRevertVersion(null)
      if (onRefresh) await onRefresh()
    } catch (err) {
      setError(extractErrorMessage(err, 'Failed to revert version'))
      console.error('Failed to revert version:', err)
    } finally {
      setIsReverting(false)
    }
  }

  const handleDelete = async () => {
    if (!workspacePath || !deleteVersion) return
    setIsDeleting(true)
    setError(null)
    try {
      await agentApi.deleteVersion(workspacePath, deleteVersion.version)
      setDeleteVersion(null)
      await loadVersions()
    } catch (err) {
      setError(extractErrorMessage(err, 'Failed to delete version'))
      console.error('Failed to delete version:', err)
    } finally {
      setIsDeleting(false)
    }
  }

  const handleRunBackup = async (action: 'backup' | 'configure') => {
    if (!workspacePath) return
    setBackupAction(action)
    setError(null)
    setNotice(null)
    try {
      const resp = await agentApi.runWorkflowBackup(workspacePath, action)
      setNotice(resp.message || 'Builder backup task started.')
      await loadBackup()
    } catch (err) {
      setError(extractErrorMessage(err, action === 'configure' ? 'Failed to start backup setup' : 'Failed to start backup'))
      console.error('Failed to run workflow backup:', err)
    } finally {
      setBackupAction(null)
    }
  }

  const handleExportZip = async () => {
    if (!workspacePath) return
    setIsExportingZip(true)
    setError(null)
    try {
      const blob = await agentApi.exportWorkflowBackup(workspacePath)
      const url = URL.createObjectURL(blob)
      const anchor = document.createElement('a')
      const name = workspacePath.split('/').filter(Boolean).pop() || 'workflow'
      anchor.href = url
      anchor.download = `${name}-backup.zip`
      document.body.appendChild(anchor)
      anchor.click()
      anchor.remove()
      URL.revokeObjectURL(url)
    } catch (err) {
      setError(extractErrorMessage(err, 'Failed to export local ZIP'))
      console.error('Failed to export workflow backup ZIP:', err)
    } finally {
      setIsExportingZip(false)
    }
  }

  if (!isOpen) return null

  const configuredDestinations = backupInfo?.config?.destinations || []
  const backupState = backupInfo?.effective_state || 'not_configured'
  const backupVisual = getStateVisual(backupState)
  const BackupStateIcon = backupVisual.Icon
  const configEnabled = Boolean(backupInfo?.config?.enabled)
  const canRunBackup = configEnabled && backupState !== 'running'
  const supportedStrategies = backupInfo?.supported?.length ? backupInfo.supported : FALLBACK_SUPPORTED_STRATEGIES

  const renderBackupTab = () => (
    <div className="space-y-4">
      <section className="rounded-md border border-border overflow-hidden">
        <div className="flex flex-col gap-3 bg-muted/30 px-4 py-4 sm:flex-row sm:items-start sm:justify-between">
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <BackupStateIcon className={`h-4.5 w-4.5 ${backupVisual.icon}`} />
              <span className={`inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium ${backupVisual.badge}`}>
                {formatStateLabel(backupState)}
              </span>
            </div>
            <h3 className="mt-2 text-base font-semibold text-foreground">Remote backup</h3>
            <p className="mt-1 max-w-2xl text-sm text-muted-foreground">
              {getBackupSummary(backupInfo)}
            </p>
            {backupInfo?.status?.last_error && (
              <p className="mt-2 text-xs text-destructive">{backupInfo.status.last_error}</p>
            )}
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <TooltipProvider delayDuration={150}>
              <Tooltip>
                <TooltipTrigger asChild>
                  <button
                    onClick={loadBackup}
                    disabled={loadingBackup}
                    className="inline-flex h-8 w-8 items-center justify-center rounded-md border border-border text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-50"
                    aria-label="Refresh backup status"
                  >
                    <RefreshCw className={`h-3.5 w-3.5 ${loadingBackup ? 'animate-spin' : ''}`} />
                  </button>
                </TooltipTrigger>
                <TooltipContent side="bottom"><p>Refresh backup status</p></TooltipContent>
              </Tooltip>
            </TooltipProvider>
            {configEnabled && (
              <button
                onClick={() => handleRunBackup('backup')}
                disabled={!canRunBackup || backupAction !== null}
                className="inline-flex items-center gap-1.5 rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {backupAction === 'backup' ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Play className="h-3.5 w-3.5" />}
                Run backup
              </button>
            )}
            <button
              onClick={() => handleRunBackup('configure')}
              disabled={backupAction !== null}
              className="inline-flex items-center gap-1.5 rounded-md border border-border px-3 py-1.5 text-xs font-medium text-foreground hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50"
            >
              {backupAction === 'configure' ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Settings2 className="h-3.5 w-3.5" />}
              {configEnabled ? 'Edit setup' : 'Set up'}
            </button>
          </div>
        </div>

        <div className="grid border-t border-border text-sm sm:grid-cols-4">
          <div className="border-b border-border px-4 py-3 sm:border-b-0 sm:border-r">
            <div className="text-xs text-muted-foreground">Last success</div>
            <div className="mt-1 font-medium text-foreground">{formatRelativeTime(backupInfo?.status?.last_success_at)}</div>
          </div>
          <div className="border-b border-border px-4 py-3 sm:border-b-0 sm:border-r">
            <div className="text-xs text-muted-foreground">Last attempt</div>
            <div className="mt-1 font-medium text-foreground">{formatRelativeTime(backupInfo?.status?.last_attempt_at)}</div>
          </div>
          <div className="border-b border-border px-4 py-3 sm:border-b-0 sm:border-r">
            <div className="text-xs text-muted-foreground">Tracked files</div>
            <div className="mt-1 font-medium text-foreground">{backupInfo?.tracked_files_count ?? 0}</div>
          </div>
          <div className="px-4 py-3">
            <div className="text-xs text-muted-foreground">Source hash</div>
            <div className="mt-1 truncate font-mono text-xs text-foreground" title={backupInfo?.current_source_hash || ''}>
              {compactHash(backupInfo?.current_source_hash)}
            </div>
          </div>
        </div>
      </section>

      {notice && (
        <div className="flex items-start gap-2 rounded-md border border-sky-500/30 bg-sky-500/10 px-3 py-2 text-xs text-sky-700 dark:text-sky-300">
          <Info className="mt-0.5 h-3.5 w-3.5 flex-shrink-0" />
          <span>{notice}</span>
        </div>
      )}

      <section className="rounded-md border border-border">
        <div className="flex items-center justify-between gap-3 border-b border-border px-4 py-3">
          <div>
            <h3 className="text-sm font-semibold text-foreground">Configured destinations</h3>
            <p className="mt-0.5 text-xs text-muted-foreground">The builder executes these and writes destination status.</p>
          </div>
          <span className="rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground">
            {configuredDestinations.length}
          </span>
        </div>
        {configuredDestinations.length === 0 ? (
          <div className="px-4 py-5 text-sm text-muted-foreground">
            Use setup to choose GitHub/Git for config and R2, S3, B2, or HuggingFace for large artifacts.
          </div>
        ) : (
          <div className="divide-y divide-border">
            {configuredDestinations.map((destination) => {
              const status = findDestinationStatus(backupInfo?.status?.destinations, destination)
              const state = status?.state || 'configured_not_verified'
              const visual = getStateVisual(state)
              const StateIcon = visual.Icon
              return (
                <div key={destination.id || destinationTitle(destination)} className="flex flex-col gap-2 px-4 py-3 sm:flex-row sm:items-start sm:justify-between">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <StateIcon className={`h-3.5 w-3.5 ${visual.icon}`} />
                      <span className="text-sm font-medium text-foreground">{destination.id || 'Destination'}</span>
                      <span className="rounded bg-muted px-1.5 py-0.5 text-[11px] uppercase tracking-wide text-muted-foreground">
                        {destination.provider || destination.type}
                      </span>
                    </div>
                    <div className="mt-1 truncate text-xs text-muted-foreground">{destinationTitle(destination)}</div>
                    <div className="mt-1 text-xs text-muted-foreground">Covers: {coverageText(destination.covers)}</div>
                    {status?.summary && <div className="mt-1 text-xs text-foreground">{status.summary}</div>}
                    {status?.error && <div className="mt-1 text-xs text-destructive">{status.error}</div>}
                  </div>
                  <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground sm:justify-end">
                    <span className={`rounded-full border px-2 py-0.5 ${visual.badge}`}>{formatStateLabel(state)}</span>
                    {status?.commit && <span className="font-mono">{status.commit.slice(0, 8)}</span>}
                    {typeof status?.objects_synced === 'number' && status.objects_synced > 0 && (
                      <span>{status.objects_synced} objects</span>
                    )}
                    {status?.last_success_at && <span>{formatRelativeTime(status.last_success_at)}</span>}
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </section>

      <div className="grid gap-3 lg:grid-cols-[1fr_280px]">
        <section className="rounded-md border border-border">
          <div className="border-b border-border px-4 py-3">
            <h3 className="text-sm font-semibold text-foreground">Supported strategies</h3>
          </div>
          <div className="grid divide-y divide-border md:grid-cols-2 md:divide-x md:divide-y-0">
            {supportedStrategies.map((strategy: WorkflowBackupStrategyInfo) => (
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

        <section className="rounded-md border border-border px-4 py-3">
          <div className="flex items-center gap-2 text-sm font-semibold text-foreground">
            <HardDrive className="h-3.5 w-3.5 text-muted-foreground" />
            Local export
          </div>
          <p className="mt-2 text-xs leading-5 text-muted-foreground">
            ZIP export is manual recovery. It does not replace a remote backup destination.
          </p>
          <button
            onClick={handleExportZip}
            disabled={isExportingZip}
            className="mt-3 inline-flex w-full items-center justify-center gap-1.5 rounded-md border border-border px-3 py-1.5 text-xs font-medium text-foreground hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50"
          >
            {isExportingZip ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Download className="h-3.5 w-3.5" />}
            Download ZIP
          </button>
          <div className="mt-3 truncate text-[11px] text-muted-foreground" title={backupInfo?.status_path || ''}>
            Status: {backupInfo?.status_path || 'backup/status.json'}
          </div>
        </section>
      </div>
    </div>
  )

  const renderVersionsTab = () => (
    <>
      {showPublishForm && (
        <div className="mb-3 rounded-md border border-border bg-muted/30 px-3 py-3">
          <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
            <input
              type="text"
              value={publishLabel}
              onChange={(e) => setPublishLabel(e.target.value)}
              placeholder="Version label"
              className="min-w-0 flex-1 rounded-md border border-border bg-background px-3 py-1.5 text-sm text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-primary"
              autoFocus
              onKeyDown={(e) => {
                if (e.key === 'Enter' && publishLabel.trim()) handlePublish()
                if (e.key === 'Escape') setShowPublishForm(false)
              }}
            />
            <div className="flex items-center gap-2">
              <button
                onClick={handlePublish}
                disabled={!publishLabel.trim() || isPublishing}
                className="inline-flex items-center gap-1.5 rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {isPublishing ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Tag className="h-3.5 w-3.5" />}
                Publish
              </button>
              <button
                onClick={() => { setShowPublishForm(false); setPublishLabel('') }}
                className="rounded-md p-1.5 text-muted-foreground hover:bg-muted hover:text-foreground"
                aria-label="Cancel publish"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            </div>
          </div>
        </div>
      )}

      {loadingVersions ? (
        <div className="flex items-center justify-center py-12">
          <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
        </div>
      ) : versions.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-md border border-dashed border-border py-12 text-muted-foreground">
          <Package className="mb-2 h-8 w-8 opacity-50" />
          <p className="text-sm">No local versions published yet</p>
          <p className="mt-1 text-xs">Publish a snapshot before risky edits or rollback tests.</p>
        </div>
      ) : (
        <div className="divide-y divide-border rounded-md border border-border">
          {versions.map((v) => (
            <div
              key={v.version}
              className="flex items-center gap-3 px-3 py-2.5 transition-colors hover:bg-muted/50"
            >
              <span className="flex-shrink-0 rounded bg-primary/10 px-2 py-0.5 text-xs font-bold text-primary">
                v{v.version}
              </span>

              <div className="min-w-0 flex-1">
                <div className="truncate text-sm font-medium text-foreground">
                  {v.label || 'Untitled'}
                </div>
                <div className="text-xs text-muted-foreground">
                  {formatRelativeTime(v.created_at)} &middot; {v.files_count} file{v.files_count !== 1 ? 's' : ''}
                </div>
              </div>

              <TooltipProvider delayDuration={150}>
                <div className="flex flex-shrink-0 items-center gap-1">
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <button
                        onClick={() => setRevertVersion(v)}
                        className="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-primary/10 hover:text-primary"
                        aria-label={`Revert to version ${v.version}`}
                      >
                        <History className="h-3.5 w-3.5" />
                      </button>
                    </TooltipTrigger>
                    <TooltipContent side="bottom">
                      <p>Revert to v{v.version}</p>
                    </TooltipContent>
                  </Tooltip>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <button
                        onClick={() => setDeleteVersion(v)}
                        className="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive"
                        aria-label={`Delete version ${v.version}`}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                    </TooltipTrigger>
                    <TooltipContent side="bottom">
                      <p>Delete v{v.version}</p>
                    </TooltipContent>
                  </Tooltip>
                </div>
              </TooltipProvider>
            </div>
          ))}
        </div>
      )}
    </>
  )

  return (
    <ModalPortal>
      <>
        <div className="fixed inset-0 z-[9999] flex items-center justify-center bg-black/50 p-2 backdrop-blur-sm sm:p-4">
          <div className="relative flex max-h-[calc(100dvh-1rem)] w-full max-w-4xl flex-col rounded-lg border border-border bg-background shadow-xl sm:max-h-[86vh]">
            <div className="flex items-start justify-between gap-3 border-b border-border px-4 py-3 sm:px-5 sm:py-3.5">
              <div className="min-w-0">
                <h2 className="flex items-center gap-2 text-base font-semibold text-foreground">
                  <Cloud className="h-4.5 w-4.5 text-primary" />
                  Backup & Versions
                </h2>
                <p className="mt-0.5 truncate text-xs text-muted-foreground">
                  Remote backup status, local ZIP export, and rollback snapshots
                </p>
              </div>
              <div className="flex items-center gap-2">
                {activeView === 'versions' && !showPublishForm && (
                  <button
                    onClick={() => setShowPublishForm(true)}
                    className="inline-flex items-center gap-1.5 rounded-md bg-primary px-2.5 py-1.5 text-xs font-medium text-primary-foreground transition-colors hover:bg-primary/90"
                  >
                    <Plus className="h-3.5 w-3.5" />
                    Publish
                  </button>
                )}
                <button
                  onClick={onClose}
                  className="rounded-md p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                  aria-label="Close"
                >
                  <X className="h-4 w-4" />
                </button>
              </div>
            </div>

            <div className="border-b border-border px-4 py-2 sm:px-5">
              <div className="inline-flex rounded-md border border-border bg-muted/30 p-0.5">
                <button
                  onClick={() => setActiveView('backup')}
                  className={`inline-flex items-center gap-1.5 rounded px-3 py-1.5 text-xs font-medium transition-colors ${activeView === 'backup' ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}`}
                >
                  <Cloud className="h-3.5 w-3.5" />
                  Backup
                </button>
                <button
                  onClick={() => setActiveView('versions')}
                  className={`inline-flex items-center gap-1.5 rounded px-3 py-1.5 text-xs font-medium transition-colors ${activeView === 'versions' ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}`}
                >
                  <Package className="h-3.5 w-3.5" />
                  Versions
                </button>
              </div>
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
              {activeView === 'backup' ? (
                loadingBackup && !backupInfo ? (
                  <div className="flex items-center justify-center py-12">
                    <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
                  </div>
                ) : renderBackupTab()
              ) : renderVersionsTab()}
            </div>
          </div>
        </div>

        <ConfirmationDialog
          isOpen={revertVersion !== null}
          onClose={() => setRevertVersion(null)}
          onConfirm={handleRevert}
          title={`Revert to v${revertVersion?.version}?`}
          message={`This will overwrite your current workflow config with the snapshot from v${revertVersion?.version} (${revertVersion?.label || 'Untitled'}). Consider publishing the current state first to preserve your changes.`}
          confirmText="Revert"
          type="warning"
          isLoading={isReverting}
        />

        <ConfirmationDialog
          isOpen={deleteVersion !== null}
          onClose={() => setDeleteVersion(null)}
          onConfirm={handleDelete}
          title={`Delete v${deleteVersion?.version}?`}
          message={`This will permanently delete version v${deleteVersion?.version} (${deleteVersion?.label || 'Untitled'}). This cannot be undone.`}
          confirmText="Delete"
          type="danger"
          isLoading={isDeleting}
        />
      </>
    </ModalPortal>
  )
}

export default WorkflowVersionsPopup
