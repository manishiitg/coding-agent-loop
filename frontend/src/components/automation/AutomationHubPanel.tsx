import { lazy, Suspense, useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { Bot, CalendarClock, MessageSquareText, Webhook } from 'lucide-react'
import type { ProductTriggerScope } from '../../api/productWebhooks'
import { useWorkflowStore } from '../../stores/useWorkflowStore'
import { EntityIdentityIcon } from '../ui/EntityIdentityIcon'
import { TriggerDeliveryHistoryPanel } from './TriggerDeliveryHistoryPanel'
import type { WorkflowScope } from '../scheduler/scheduleRuns/helpers'

const WorkflowScheduleRunsPanel = lazy(() => import('../scheduler/WorkflowScheduleRunsPanel'))
const ProductAPITriggersView = lazy(() => import('../workflow/ProductAPITriggersView'))
const WorkflowAPITriggersView = lazy(() => import('../workflow/WorkflowAPITriggersView'))

export type AutomationHubSection = 'chats' | 'schedules' | 'triggers' | 'bots'

type AutomationHubPanelProps = {
  entityType: 'workflow' | 'product'
  workspacePath: string
  entityLabel: string
  entityIcon?: string | null
  workflowScope?: WorkflowScope
  productTriggerScope?: ProductTriggerScope
  chatContent?: ReactNode
  botContent?: ReactNode
  canManage?: boolean
  scopeNoun?: 'automation' | 'project'
  initialSection?: AutomationHubSection
  scheduleHeaderAction?: ReactNode
  triggerHeaderAction?: ReactNode
}

const SECTION_DEFS = [
  { id: 'schedules', label: 'Schedules', icon: CalendarClock },
  { id: 'triggers', label: 'Triggers', icon: Webhook },
  { id: 'bots', label: 'Bots', icon: Bot },
  { id: 'chats', label: 'Chats', icon: MessageSquareText },
] as const

export function AutomationHubPanel({
  entityType,
  workspacePath,
  entityLabel,
  entityIcon,
  workflowScope,
  productTriggerScope,
  chatContent,
  botContent,
  canManage,
  scopeNoun = entityType === 'product' ? 'project' : 'automation',
  initialSection = 'chats',
  scheduleHeaderAction,
  triggerHeaderAction,
}: AutomationHubPanelProps) {
  const workspaceViewTarget = useWorkflowStore(state => state.workspaceViewTarget)
  const availableSections = useMemo(() => new Set<AutomationHubSection>([
    ...(chatContent ? ['chats' as const] : []),
    'schedules',
    ...(entityType === 'workflow' || productTriggerScope ? ['triggers' as const] : []),
    ...(botContent ? ['bots' as const] : []),
  ]), [botContent, chatContent, entityType, productTriggerScope])
  const fallbackSection = availableSections.has(initialSection) ? initialSection : 'schedules'
  const [section, setSection] = useState<AutomationHubSection>(fallbackSection)
  const selectSection = useCallback((next: AutomationHubSection) => {
    setSection(next)
    useWorkflowStore.getState().openWorkspaceView('workshop', next)
  }, [])

  useEffect(() => {
    if (availableSections.has(initialSection)) setSection(initialSection)
  }, [availableSections, initialSection])

  useEffect(() => {
    if (workspaceViewTarget?.view !== 'workshop') return
    const target = workspaceViewTarget?.target
    const normalized = target === 'webhooks' ? 'triggers' : target
    if (normalized && availableSections.has(normalized as AutomationHubSection)) {
      setSection(normalized as AutomationHubSection)
    }
  }, [availableSections, workspaceViewTarget])

  return (
    <div
      data-testid="automation-hub-panel"
      className="flex h-full min-h-0 min-w-0 w-full max-w-none flex-1 flex-col bg-background"
    >
      <div className="shrink-0 border-b border-border px-4 pt-4 sm:px-6">
        <div className="flex items-start gap-3 pb-3">
          <EntityIdentityIcon icon={entityIcon} label={entityLabel} className="h-9 w-9 rounded-lg text-lg" />
          <div className="min-w-0">
            <h2 className="truncate text-sm font-semibold text-foreground">{entityLabel}</h2>
            <p className="mt-0.5 text-xs text-muted-foreground">Chat history and the channels that can start work.</p>
          </div>
        </div>
        <div className="flex items-center gap-1 overflow-x-auto pb-2" role="tablist" aria-label="Automation center">
          {SECTION_DEFS.filter(item => availableSections.has(item.id)).map(({ id, label, icon: Icon }) => (
            <button
              key={id}
              type="button"
              role="tab"
              aria-selected={section === id}
              aria-label={label}
              title={label}
              onClick={() => selectSection(id)}
              className={`inline-flex h-8 shrink-0 items-center justify-center gap-2 rounded-md px-2.5 text-xs font-medium transition-colors sm:min-w-24 ${section === id ? 'bg-muted text-foreground shadow-sm' : 'text-muted-foreground hover:bg-muted/60 hover:text-foreground'}`}
            >
              <Icon className="h-3.5 w-3.5" aria-hidden="true" />
              <span className="hidden sm:inline">{label}</span>
            </button>
          ))}
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-hidden">
        <Suspense fallback={<div className="p-4 text-sm text-muted-foreground">Loading…</div>}>
        {section === 'chats' && chatContent}
        {section === 'schedules' && <WorkflowScheduleRunsPanel
          embedded
          active
          entityType={entityType}
          workflowScope={workflowScope}
          canManage={canManage}
          scopeNoun={scopeNoun}
          headerAction={scheduleHeaderAction}
          showAutomationTabs={false}
          hideScheduleTitle
          onClose={() => {}}
        />}
        {section === 'triggers' && entityType === 'workflow' && <WorkflowAPITriggersView
          workspacePath={workspacePath}
          deliveryHistory={<TriggerDeliveryHistoryPanel workspacePath={workspacePath} entityType="workflow" />}
          headerAction={triggerHeaderAction}
        />}
        {section === 'triggers' && entityType === 'product' && productTriggerScope && <ProductAPITriggersView
          scope={productTriggerScope}
          deliveryHistory={<TriggerDeliveryHistoryPanel workspacePath={workspacePath} entityType="product" productTriggerScope={productTriggerScope} />}
          headerAction={triggerHeaderAction}
        />}
        {section === 'bots' && botContent}
        </Suspense>
      </div>
    </div>
  )
}
