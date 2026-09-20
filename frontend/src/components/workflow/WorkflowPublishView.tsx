import React, { useCallback } from 'react'
import { agentApi } from '../../services/api'
import type { WorkflowPublishInfoResponse, WorkflowPublishStrategyInfo } from '../../services/api-types'
import PublishPopupBody from '../backup-publish/PublishPopupBody'

interface WorkflowPublishViewProps {
  workspacePath: string | null
  onStateLoaded?: (state: string) => void
  headerAction?: React.ReactNode
}

const FALLBACK_SUPPORTED: WorkflowPublishStrategyInfo[] = [
  { id: 'netlify', label: 'Netlify', method: 'cli', description: 'netlify deploy --prod; default URL *.netlify.app.' },
  { id: 'vercel', label: 'Vercel', method: 'cli', description: 'vercel deploy --prod; default URL *.vercel.app.' },
  { id: 'cloudflare-pages', label: 'Cloudflare Pages', method: 'cli', description: 'wrangler pages deploy; default URL *.pages.dev.' },
  { id: 'github-pages', label: 'GitHub Pages', method: 'git', description: 'Push static files to the gh-pages branch.' },
  { id: 's3', label: 'S3 / object store', method: 'sync', description: 'aws s3 sync / rclone to a static bucket — the any-host catch-all.' }
]

const getPublishSummary = (info: WorkflowPublishInfoResponse | null): string => {
  if (!info?.config?.enabled) return 'Not published yet. Set one up with /publish in chat.'
  switch (info?.effective_state) {
    case 'published':
      return 'Published. Your site is up to date.'
    case 'publishing':
      return 'Publishing now.'
    case 'configured_not_verified':
      return 'Set up. Waiting for the first publish.'
    case 'failed':
      return 'Last publish failed.'
    default:
      return 'Waiting for publish status.'
  }
}

const WorkflowPublishView: React.FC<WorkflowPublishViewProps> = ({ workspacePath, onStateLoaded, headerAction }) => {
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
    <PublishPopupBody
      loadInfo={loadInfo}
      loadAccessSecret={loadAccessSecret}
      onStateLoaded={onStateLoaded}
      fallbackStrategies={FALLBACK_SUPPORTED}
      subtitle="Share this automation's Pulse log and dashboard at a public URL"
      emptyDestinationsText="No hosts yet — set one up with /publish in chat. Any static host works."
      destinationsHelp="Deploys update automatically."
      askContext={{ workspacePath, strategyVerb: 'publish this workflow to' }}
      supportedHelp="Suggestions, not a limit — any static host works."
      defaultTargetLabel="pulse, dashboard"
      getSummary={getPublishSummary}
      headerAction={headerAction}
    />
  )
}

export default WorkflowPublishView
