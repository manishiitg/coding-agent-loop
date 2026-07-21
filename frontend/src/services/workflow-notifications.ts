import type { WorkflowNotificationAccountChannelInfo, WorkflowNotificationDestinationInfo, WorkflowNotificationState } from './api-types'
import { agentApi } from './api'

export type { WorkflowNotificationState } from './api-types'

export interface WorkflowNotificationInfo {
  scopeLabel: string
  effectiveState: WorkflowNotificationState
  slackWebhook: WorkflowNotificationDestinationInfo
  gmail: WorkflowNotificationAccountChannelInfo | null
  // Per-workflow preferences from workflow.json notifications (display-only).
  excludeChannels: string[]
  blockRecipients: string[]
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
    scopeLabel: response.scope_label || response.workflow_label || workspacePath.split('/').filter(Boolean).pop() || 'Workflow',
    effectiveState: response.effective_state,
    slackWebhook,
    gmail: response.account_channels.find(channel => channel.id === 'gmail') || null,
    excludeChannels: response.exclude_channels || [],
    blockRecipients: response.block_recipients || [],
  }
}

export async function loadOrgNotificationInfo(): Promise<WorkflowNotificationInfo> {
  const response = await agentApi.getOrgNotifications()
  const slackWebhook = response.destinations.find(destination => destination.type === 'slack_webhook') || {
    id: 'chief-of-staff-slack-webhook',
    type: 'slack_webhook',
    label: 'Chief of Staff Slack webhook',
    state: response.effective_state,
  }
  return {
    scopeLabel: response.scope_label || 'Chief of Staff',
    effectiveState: response.effective_state,
    slackWebhook,
    gmail: response.account_channels.find(channel => channel.id === 'gmail') || null,
    excludeChannels: response.exclude_channels || [],
    blockRecipients: response.block_recipients || [],
  }
}
