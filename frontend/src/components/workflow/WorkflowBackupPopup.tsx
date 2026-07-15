import React, { useCallback } from 'react'
import { agentApi } from '../../services/api'
import type { WorkflowBackupInfoResponse, WorkflowBackupStrategyInfo } from '../../services/api-types'
import BackupPopup from '../backup-publish/BackupPopup'

interface WorkflowBackupPopupProps {
  isOpen: boolean
  onClose: () => void
  workspacePath: string | null
  // Called whenever backup info is (re)loaded, so the parent can keep an
  // at-a-glance status indicator (e.g. the toolbar dot) in sync.
  onStateLoaded?: (state: string) => void
}

const FALLBACK_SUPPORTED_STRATEGIES: WorkflowBackupStrategyInfo[] = [
  {
    id: 'git',
    label: 'GitHub / remote Git (recommended)',
    description: 'Recommended for off-device protection of automation config, planning, knowledgebase, learnings, scripts, and small JSON. Local Git alone is not a durable backup.',
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

const getBackupSummary = (backupInfo: WorkflowBackupInfoResponse | null): string => {
  const state = backupInfo?.effective_state
  if (!backupInfo?.config?.enabled) {
    return 'No off-device backup is configured. Add GitHub, another remote Git host, or an object store to protect this workflow if this laptop is lost.'
  }
  if (state === 'local_only') {
    return 'This backup exists only on this laptop. Add GitHub, another remote Git host, or an object store for durable off-device recovery.'
  }
  if (backupInfo.status?.summary) return backupInfo.status.summary
  switch (state) {
    case 'configured_not_verified':
      return 'Backup is configured, but the builder has not verified a successful run yet.'
    case 'running':
      return 'A builder backup task is running and will update backup/status.json.'
    case 'stale':
      return 'The automation changed since the last healthy backup.'
    default:
      return 'Backup status is waiting for the builder to update backup/status.json.'
  }
}

const WorkflowBackupPopup: React.FC<WorkflowBackupPopupProps> = ({
  isOpen,
  onClose,
  workspacePath,
  onStateLoaded
}) => {
  const loadInfo = useCallback(async () => {
    if (!workspacePath) throw new Error('No workflow is selected')
    return agentApi.getWorkflowBackup(workspacePath)
  }, [workspacePath])

  const exportBlob = useCallback(async () => {
    if (!workspacePath) throw new Error('No workflow is selected')
    return agentApi.exportWorkflowBackup(workspacePath)
  }, [workspacePath])

  const name = workspacePath?.split('/').filter(Boolean).pop() || 'workflow'

  return (
    <BackupPopup
      isOpen={isOpen}
      onClose={onClose}
      loadInfo={loadInfo}
      onStateLoaded={onStateLoaded}
      fallbackStrategies={FALLBACK_SUPPORTED_STRATEGIES}
      subtitle="Remote backup strategy, destination status, and local ZIP export"
      emptyDestinationsText="Use setup to choose GitHub/Git for config and R2, S3, B2, or HuggingFace for large artifacts."
      destinationsHelp="The builder executes these and writes destination status."
      statusPathFallback="backup/status.json"
      setupAction={{
        label: <>Set up · run · restore in chat with <code className="rounded bg-background px-1 font-medium text-foreground">/backup</code></>
      }}
      getSummary={getBackupSummary}
      exportAction={{
        label: 'Download ZIP',
        filename: `${name}-backup.zip`,
        exportBlob,
      }}
    />
  )
}

export default WorkflowBackupPopup
