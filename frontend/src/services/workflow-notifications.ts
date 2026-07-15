import type { WorkflowNotificationAccountChannelInfo, WorkflowNotificationDestinationInfo, WorkflowNotificationState } from './api-types'
import { agentApi } from './api'

export type { WorkflowNotificationState } from './api-types'

export interface WorkflowNotificationInfo {
  workflowLabel: string
  effectiveState: WorkflowNotificationState
  slackWebhook: WorkflowNotificationDestinationInfo
  gmail: WorkflowNotificationAccountChannelInfo | null
}

export async function loadWorkflowNotificationInfo(workspacePath: string): Promise<WorkflowNotificationInfo> {
  const response = await agentApi.getWorkflowNotifications(workspacePath)
  const slackWebhook = response.destinations.find(destination => destination.type === 'slack_webhook') || {
    id: 'workflow-slack-webhook',
    type: 'slack_webhook',
    label: 'Workflow Slack webhook',
    state: response.effective_state,
  }
  return {
    workflowLabel: response.workflow_label,
    effectiveState: response.effective_state,
    slackWebhook,
    gmail: response.account_channels.find(channel => channel.id === 'gmail') || null,
  }
}
