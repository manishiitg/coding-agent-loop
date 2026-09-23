import type { WorkflowContractUpgradeStatus } from '../../services/api-types'

export const MANUAL_CONTRACT_UPGRADE_MESSAGE = 'Update this workflow to the current platform contract now. Use get_contract_upgrades, complete and verify each pending migration in order, and stamp each completed version before continuing to the next. Do not run the workflow as part of the migration. If a migration requires a genuine product, business, or safety choice, stop and ask me in this chat instead of guessing. When all migrations are complete, confirm the final workflow contract version.'
export const WORKFLOW_CONTRACT_UPGRADE_STATUS_EVENT = 'workflow-contract-upgrade-status'
export const WORKFLOW_CONTRACT_UPGRADE_CHAT_EVENT = 'workflow-contract-upgrade-chat'

export type WorkflowContractUpgradeStatusEvent = CustomEvent<{
  workspacePath: string
  status: WorkflowContractUpgradeStatus | null
}>

export type WorkflowContractUpgradeChatEvent = CustomEvent<{
  workspacePath: string
  tabId: string
}>
