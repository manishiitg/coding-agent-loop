import type { WorkflowNotificationAccountChannelInfo, WorkflowNotificationDestinationInfo, WorkflowNotificationState } from './api-types'
import { agentApi } from './api'

export type { WorkflowNotificationState } from './api-types'

export interface WorkflowNotificationInfo {
  scopeLabel: string
  effectiveState: WorkflowNotificationState
  slackWebhook: WorkflowNotificationDestinationInfo
  gmail: WorkflowNotificationAccountChannelInfo | null
  // Per-workflow content preferences from workflow.json notifications.
  runSummaryInstructions: string
  pulseSummaryInstructions: string
  runSummaryChannels: string[]
  pulseSummaryChannels: string[]
  // Who each summary is emailed to. Empty means the account default recipient.
  runSummaryRecipients: string[]
  /** One sender for this workflow's Notify emails; empty inherits the account default. */
  gmailConnectionId: string
  /** Which Gmail account(s) each summary sends FROM; empty means inherit. */
  runSummaryGmailConnectionIds: string[]
  pulseSummaryGmailConnectionIds: string[]
  pulseSummaryRecipients: string[]
  // Slack channels per summary, as webhook secret names (one webhook = one channel).
  runSummarySlackWebhooks: string[]
  pulseSummarySlackWebhooks: string[]
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
    runSummaryInstructions: response.run_summary_instructions || '',
    pulseSummaryInstructions: response.pulse_summary_instructions || '',
    runSummaryChannels: response.run_summary_channels || [],
    pulseSummaryChannels: response.pulse_summary_channels || [],
    runSummaryRecipients: response.run_summary_recipients || [],
    gmailConnectionId: response.gmail_connection_id || '',
    runSummaryGmailConnectionIds: response.run_summary_gmail_connection_ids || [],
    pulseSummaryGmailConnectionIds: response.pulse_summary_gmail_connection_ids || [],
    pulseSummaryRecipients: response.pulse_summary_recipients || [],
    runSummarySlackWebhooks: response.run_summary_slack_webhooks || [],
    pulseSummarySlackWebhooks: response.pulse_summary_slack_webhooks || [],
    excludeChannels: response.exclude_channels || [],
    blockRecipients: response.block_recipients || [],
  }
}
