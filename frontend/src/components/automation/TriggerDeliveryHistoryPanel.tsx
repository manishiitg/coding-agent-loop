import { PreviousChatHistoryPanel } from '../PreviousChatHistoryPanel'

export function TriggerDeliveryHistoryPanel({
  workspacePath,
  entityType,
}: {
  workspacePath: string
  entityType: 'workflow' | 'product'
}) {
  return (
    <PreviousChatHistoryPanel
      workspacePath={workspacePath}
      title="Delivery history"
      emptyText="No webhook deliveries recorded yet."
      runOnly="webhook"
      runEntityType={entityType}
      readOnly
      showAll
      onSelectSession={() => {}}
    />
  )
}
