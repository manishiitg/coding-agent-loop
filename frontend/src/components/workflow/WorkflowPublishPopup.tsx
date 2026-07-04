import React, { useCallback } from 'react'
import { agentApi } from '../../services/api'
import type { WorkflowPublishInfoResponse, WorkflowPublishStrategyInfo } from '../../services/api-types'
import PublishPopup from '../backup-publish/PublishPopup'

interface WorkflowPublishPopupProps {
  isOpen: boolean
  onClose: () => void
  workspacePath: string | null
  onStateLoaded?: (state: string) => void
}

const FALLBACK_SUPPORTED: WorkflowPublishStrategyInfo[] = [
  { id: 'netlify', label: 'Netlify', method: 'cli', description: 'netlify deploy --prod; default URL *.netlify.app.' },
  { id: 'vercel', label: 'Vercel', method: 'cli', description: 'vercel deploy --prod; default URL *.vercel.app.' },
  { id: 'cloudflare-pages', label: 'Cloudflare Pages', method: 'cli', description: 'wrangler pages deploy; default URL *.pages.dev.' },
  { id: 'github-pages', label: 'GitHub Pages', method: 'git', description: 'Push static files to the gh-pages branch.' },
  { id: 's3', label: 'S3 / object store', method: 'sync', description: 'aws s3 sync / rclone to a static bucket — the any-host catch-all.' }
]

const getPublishSummary = (info: WorkflowPublishInfoResponse | null): string => {
  if (info?.status?.summary) return info.status.summary
  if (!info?.config?.enabled) return 'No publish destination is configured yet.'
  return 'Publish status is waiting for the builder to update publish/status.json.'
}

const WorkflowPublishPopup: React.FC<WorkflowPublishPopupProps> = ({ isOpen, onClose, workspacePath, onStateLoaded }) => {
  const loadInfo = useCallback(async () => {
    if (!workspacePath) throw new Error('No workflow is selected')
    return agentApi.getWorkflowPublish(workspacePath)
  }, [workspacePath])

  const loadAccessSecret = useCallback(async (secretName: string) => {
    if (!workspacePath) throw new Error('No workflow is selected')
    const resp = await agentApi.getWorkflowPublishSecret(workspacePath, secretName)
    return resp.value
  }, [workspacePath])

  return (
    <PublishPopup
      isOpen={isOpen}
      onClose={onClose}
      loadInfo={loadInfo}
      loadAccessSecret={loadAccessSecret}
      onStateLoaded={onStateLoaded}
      fallbackStrategies={FALLBACK_SUPPORTED}
      subtitle="Share this automation's Pulse log & report dashboard at a public URL"
      emptyDestinationsText="Use setup to pick a static host (Netlify, Vercel, Cloudflare Pages, GitHub Pages, S3, ...) — any static host works."
      destinationsHelp="The builder deploys to these and writes the URL."
      supportedHelp="Suggestions, not a limit — any static host works."
      statusPathFallback="publish/status.json"
      defaultTargetLabel="pulse, report"
      setupAction={{
        label: <>Set up · publish in chat with <code className="rounded bg-background px-1 font-medium text-foreground">/publish</code></>
      }}
      getSummary={getPublishSummary}
    />
  )
}

export default WorkflowPublishPopup
