import React, { useCallback, useEffect, useState } from 'react'
import { Cloud, Globe } from 'lucide-react'
import { agentApi } from '../../services/api'
import type {
  WorkflowBackupInfoResponse,
  WorkflowBackupStrategyInfo,
  WorkflowPublishInfoResponse,
  WorkflowPublishStrategyInfo,
} from '../../services/api-types'
import { Tooltip, TooltipContent, TooltipTrigger } from '../ui/tooltip'
import { formatBackupStateLabel, getBackupDotClass } from '../workflow/backupStatus'
import { formatPublishStateLabel, getPublishDotClass } from '../workflow/publishStatus'
import BackupPopup from '../backup-publish/BackupPopup'
import PublishPopup from '../backup-publish/PublishPopup'

const ORG_BACKUP_COMMAND_MESSAGE = `Help me set up or run org-level backup.

Call get_reference_doc(kind="backup-strategy") and follow its org-level workflow-style contract. Read pulse/backup.json and pulse/backup/status.json if they exist.

Scope:
- pulse/goals.html
- pulse/org-pulse.html
- Chief of Staff memory files
- employee/org config files
- multi-agent schedules/config

If org backup is NOT configured yet: set up the zero-config local-git default, write pulse/backup.json, write pulse/backup/status.json with state "configured_not_verified" or "healthy" if you can complete the local backup now, and ask me before adding any remote destination or credentials.

If org backup IS configured: run a backup now, skip only if pulse/backup/status.json proves the current source hash is unchanged, and report the result.

Always write pulse/backup/status.json. Never write org backup state into any workflow.json or content HTML file, and never back up secrets.`

const ORG_PUBLISH_COMMAND_MESSAGE = `Help me set up or run org-level publish.

Call get_reference_doc(kind="publish-strategy") and follow its org-level workflow-style contract. Read pulse/publish.json and pulse/publish/status.json if they exist.

Publish scope:
- pulse/goals.html as goals.html
- pulse/org-pulse.html as pulse.html
- an index.html wrapper with Goals | Pulse navigation

If org publish is NOT configured: ask me which static host to use, default to private visibility with a PUBLISH_PASSWORD secret, write pulse/publish.json, and write pulse/publish/status.json with state "configured_not_verified". Do not do the first/verifying publish until I confirm the destination and visibility.

If org publish IS configured and verified: publish now only if the org HTML changed since the last publish. Stage files outside the workspace, force dark mode, deploy, then come back and update pulse/publish/status.json with state "published", the url, and last_source_hash.

Always write pulse/publish/status.json. Never publish secrets or raw memory files. Never write org publish state into any workflow.json or content HTML file.`

const FALLBACK_ORG_BACKUP_STRATEGIES: WorkflowBackupStrategyInfo[] = [
  {
    id: 'git',
    label: 'Git / GitHub',
    description: 'Default for org goals, pulse HTML, memory index, schedule/config snapshots, and small text artifacts.',
    best_for: ['org-goals', 'org-pulse', 'memory', 'schedules']
  },
  {
    id: 'object_store',
    label: 'R2 / S3 / B2',
    description: 'Best when org artifacts grow into larger generated assets or exported dashboards.',
    best_for: ['large-artifacts', 'exports']
  },
  {
    id: 'local_zip',
    label: 'Local ZIP export',
    description: 'Manual recovery snapshot. Useful as a fallback, not a substitute for remote backup.',
    best_for: ['manual-export', 'restore']
  }
]

const FALLBACK_ORG_PUBLISH_STRATEGIES: WorkflowPublishStrategyInfo[] = [
  { id: 'netlify', label: 'Netlify', method: 'cli', description: 'Static deploy for goals.html, pulse.html, and an index wrapper.' },
  { id: 'vercel', label: 'Vercel', method: 'cli', description: 'Static deploy with optional private/unguessable visibility conventions.' },
  { id: 'cloudflare-pages', label: 'Cloudflare Pages', method: 'cli', description: 'Static hosting through wrangler pages deploy.' },
  { id: 'github-pages', label: 'GitHub Pages', method: 'git', description: 'Push static files to a pages branch or repo.' },
  { id: 's3', label: 'S3 / object store', method: 'sync', description: 'Sync prepared static files to any bucket-backed static host.' }
]

const OrgBackupPopup: React.FC<{
  isOpen: boolean
  onClose: () => void
  onSubmitCommand?: (query: string) => void
  onStateLoaded?: (state: string) => void
}> = ({ isOpen, onClose, onSubmitCommand, onStateLoaded }) => {
  const runInChat = useCallback(() => {
    onSubmitCommand?.(ORG_BACKUP_COMMAND_MESSAGE)
    onClose()
  }, [onClose, onSubmitCommand])

  const getSummary = useCallback((info: WorkflowBackupInfoResponse | null): string => {
    const state = info?.effective_state
    if (info?.status?.summary) return info.status.summary
    if (!info?.config?.enabled) return 'No org backup destination is configured yet.'
    if (state === 'stale') return 'Org goals or pulse changed since the last healthy backup.'
    return 'Org backup status is waiting for the Chief of Staff to update pulse/backup/status.json.'
  }, [])

  return (
    <BackupPopup
      isOpen={isOpen}
      onClose={onClose}
      loadInfo={agentApi.getOrgBackup}
      onStateLoaded={onStateLoaded}
      fallbackStrategies={FALLBACK_ORG_BACKUP_STRATEGIES}
      subtitle="Org goals, pulse, memory, schedules, and config"
      emptyDestinationsText="Use setup to configure the zero-config local git default or a remote destination."
      destinationsHelp="The Chief of Staff writes destination status after each backup."
      statusPathFallback="pulse/backup/status.json"
      setupAction={{
        onClick: runInChat,
        label: <>Set up · run · restore in chat with <code className="ml-1 rounded bg-background px-1 font-medium text-foreground">/org-backup</code></>
      }}
      getSummary={getSummary}
      loadErrorMessage="Failed to load org backup status"
      showEnabledBadge
    />
  )
}

const OrgPublishPopup: React.FC<{
  isOpen: boolean
  onClose: () => void
  onSubmitCommand?: (query: string) => void
  onStateLoaded?: (state: string) => void
}> = ({ isOpen, onClose, onSubmitCommand, onStateLoaded }) => {
  const runInChat = useCallback(() => {
    onSubmitCommand?.(ORG_PUBLISH_COMMAND_MESSAGE)
    onClose()
  }, [onClose, onSubmitCommand])

  const getSummary = useCallback((info: WorkflowPublishInfoResponse | null): string => {
    const state = info?.effective_state
    if (info?.status?.summary) return info.status.summary
    if (!info?.config?.enabled) return 'No org publish destination is configured yet.'
    if (state === 'stale') return 'Org goals or pulse changed since the last publish.'
    return 'Org publish status is waiting for the Chief of Staff to update pulse/publish/status.json.'
  }, [])

  const loadAccessSecret = useCallback(async (secretName: string) => {
    const resp = await agentApi.getOrgPublishSecret(secretName)
    return resp.value
  }, [])

  return (
    <PublishPopup
      isOpen={isOpen}
      onClose={onClose}
      loadInfo={agentApi.getOrgPublish}
      loadAccessSecret={loadAccessSecret}
      onStateLoaded={onStateLoaded}
      fallbackStrategies={FALLBACK_ORG_PUBLISH_STRATEGIES}
      subtitle="Share org Goals/Pulse pages at a static URL"
      emptyDestinationsText="Use setup to pick a static host like Netlify, Vercel, Cloudflare Pages, GitHub Pages, or S3."
      destinationsHelp="The Chief of Staff deploys these static artifacts and writes the URL."
      statusPathFallback="pulse/publish/status.json"
      defaultTargetLabel="goals.html, pulse.html, index.html"
      setupAction={{
        onClick: runInChat,
        label: <>Set up · publish in chat with <code className="ml-1 rounded bg-background px-1 font-medium text-foreground">/org-publish</code></>
      }}
      getSummary={getSummary}
      loadErrorMessage="Failed to load org publish status"
      showEnabledBadge
    />
  )
}

export const OrgBackupPublishControls: React.FC<{ onSubmitCommand?: (query: string) => void }> = ({ onSubmitCommand }) => {
  const [backupState, setBackupState] = useState('not_configured')
  const [publishState, setPublishState] = useState('not_configured')
  const [showBackupPopup, setShowBackupPopup] = useState(false)
  const [showPublishPopup, setShowPublishPopup] = useState(false)

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

  const buttonClass = 'relative rounded-md bg-muted p-1.5 text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground'

  return (
    <>
      <div className="flex items-center gap-1">
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              type="button"
              onClick={() => setShowBackupPopup(true)}
              title={`Backup - ${formatBackupStateLabel(backupState)}`}
              aria-label="Org backup"
              className={buttonClass}
            >
              <Cloud className="h-3.5 w-3.5" />
              <span className={`absolute -right-0.5 -top-0.5 h-2 w-2 rounded-full border border-background ${getBackupDotClass(backupState)}`} />
            </button>
          </TooltipTrigger>
          <TooltipContent>
            <p>Backup &middot; {formatBackupStateLabel(backupState)}</p>
          </TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              type="button"
              onClick={() => setShowPublishPopup(true)}
              title={`Publish - ${formatPublishStateLabel(publishState)}`}
              aria-label="Org publish"
              className={buttonClass}
            >
              <Globe className="h-3.5 w-3.5" />
              <span className={`absolute -right-0.5 -top-0.5 h-2 w-2 rounded-full border border-background ${getPublishDotClass(publishState)}`} />
            </button>
          </TooltipTrigger>
          <TooltipContent>
            <p>Publish &middot; {formatPublishStateLabel(publishState)}</p>
          </TooltipContent>
        </Tooltip>
      </div>
      <OrgBackupPopup
        isOpen={showBackupPopup}
        onClose={() => setShowBackupPopup(false)}
        onSubmitCommand={onSubmitCommand}
        onStateLoaded={setBackupState}
      />
      <OrgPublishPopup
        isOpen={showPublishPopup}
        onClose={() => setShowPublishPopup(false)}
        onSubmitCommand={onSubmitCommand}
        onStateLoaded={setPublishState}
      />
    </>
  )
}
