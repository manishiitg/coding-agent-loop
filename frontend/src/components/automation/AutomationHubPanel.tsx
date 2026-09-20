import { cloneElement, isValidElement, lazy, Suspense, useCallback, useEffect, useMemo, useState, type ReactElement, type ReactNode } from 'react'
import { Bot, CalendarClock, MessageSquareText, Webhook, Zap } from 'lucide-react'
import type { ProductTriggerScope } from '../../api/productWebhooks'
import { useWorkflowStore } from '../../stores/useWorkflowStore'
import { TriggerDeliveryHistoryPanel } from './TriggerDeliveryHistoryPanel'
import type { WorkflowScope } from '../scheduler/scheduleRuns/helpers'
import { ScheduleStatusPills, type ScheduleStatusSnapshot } from '../scheduler/scheduleRuns/ScheduleStatusPills'
import { AskAIButton } from '../workflow/AskAIButton'
import { getWorkspaceAskAIMessage } from '../workflow/workspaceAskAI'
import { WorkspaceViewHeader } from '../workflow/WorkspaceViewHeader'
import { WorkspaceViewIconButton } from '../workflow/WorkspaceViewIconButton'

const WorkflowScheduleRunsPanel = lazy(() => import('../scheduler/WorkflowScheduleRunsPanel'))
const ProductAPITriggersView = lazy(() => import('../workflow/ProductAPITriggersView'))
const WorkflowAPITriggersView = lazy(() => import('../workflow/WorkflowAPITriggersView'))

export type AutomationHubSection = 'chats' | 'schedules' | 'triggers' | 'bots'

type AutomationHubPanelProps = {
  entityType: 'workflow' | 'product'
  workspacePath: string
  workflowScope?: WorkflowScope
  productTriggerScope?: ProductTriggerScope
  chatContent?: ReactNode
  botContent?: ReactNode
  canManage?: boolean
  scopeNoun?: 'automation' | 'project'
  initialSection?: AutomationHubSection
  /**
   * Per-tab Ask AI messages. Workflow hubs default to the workspace messages;
   * product (Crew) hubs must pass explicit messages: a workspacePath-routed
   * message would land in the wrong chat.
   */
  askAIMessages?: Partial<Record<AutomationHubSection, string>>
  /** Ask AI routing override (Crew panes route to the project chat). */
  onAskAI?: (message: string) => void | Promise<void>
}

const SECTION_DEFS = [
  { id: 'schedules', label: 'Schedules', icon: CalendarClock },
  { id: 'triggers', label: 'Triggers', icon: Webhook },
  { id: 'bots', label: 'Bots', icon: Bot },
  { id: 'chats', label: 'Chats', icon: MessageSquareText },
] as const

const WORKFLOW_SECTION_MESSAGES: Record<AutomationHubSection, string> = {
  schedules: getWorkspaceAskAIMessage('schedules'),
  triggers: getWorkspaceAskAIMessage('webhooks'),
  chats: getWorkspaceAskAIMessage('workshop'),
  bots: getWorkspaceAskAIMessage('workshop'),
}

export function AutomationHubPanel({
  entityType,
  workspacePath,
  workflowScope,
  productTriggerScope,
  chatContent,
  botContent,
  canManage,
  scopeNoun = entityType === 'product' ? 'project' : 'automation',
  initialSection = 'schedules',
  askAIMessages,
  onAskAI,
}: AutomationHubPanelProps) {
  // Workflow hubs default every Ask AI message to the workflow Builder chat.
  // Product (Crew) hubs must pass explicit messages: a workspacePath-routed
  // message would land in the wrong chat.
  const isWorkflow = entityType === 'workflow'
  const [schedulesRefreshToken, setSchedulesRefreshToken] = useState(0)
  const [schedulesStatus, setSchedulesStatus] = useState<ScheduleStatusSnapshot | null>(null)
  const [triggersRefreshToken, setTriggersRefreshToken] = useState(0)
  const [triggersCounts, setTriggersCounts] = useState<{ active: number; paused: number } | null>(null)
  const [chatsRefreshToken, setChatsRefreshToken] = useState(0)
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

  const askMessage = askAIMessages?.[section] ?? (isWorkflow ? WORKFLOW_SECTION_MESSAGES[section] : undefined)

  return (
    <div
      data-testid="automation-hub-panel"
      className="flex h-full min-h-0 min-w-0 w-full max-w-none flex-1 flex-col bg-background"
    >
      <WorkspaceViewHeader
        icon={Zap}
        title="Automation"
        subtitle="Chat history and the channels that can start work."
        actions={askMessage ? <AskAIButton workspacePath={workspacePath} message={askMessage} onAsk={onAskAI} iconOnly /> : undefined}
        tabActions={{
          schedules: <WorkspaceViewIconButton label="Refresh schedules" onClick={() => setSchedulesRefreshToken(token => token + 1)} spinning={schedulesStatus?.isLoading} />,
          triggers: <WorkspaceViewIconButton label="Refresh triggers" onClick={() => setTriggersRefreshToken(token => token + 1)} />,
          chats: <WorkspaceViewIconButton label="Refresh chats" onClick={() => setChatsRefreshToken(token => token + 1)} />,
        }}
        context={
          section === 'schedules' && schedulesStatus ? <ScheduleStatusPills status={schedulesStatus} />
          : section === 'triggers' && triggersCounts ? (
            <span className="text-xs text-muted-foreground">{triggersCounts.active} active · {triggersCounts.paused} paused</span>
          ) : undefined
        }
        tabs={{
          value: section,
          onChange: (value: string) => selectSection(value as AutomationHubSection),
          options: SECTION_DEFS.filter(item => availableSections.has(item.id)).map(({ id, label, icon }) => ({ value: id, label, icon })),
          ariaLabel: 'Automation center',
        }}
      />

      <div className="min-h-0 flex-1 overflow-hidden">
        <Suspense fallback={<div className="p-4 text-sm text-muted-foreground">Loading…</div>}>
        {section === 'chats' && chatContent && (
          <div data-testid="automation-hub-chats" className="flex h-full min-h-0 flex-col">
            {isValidElement(chatContent)
              ? cloneElement(chatContent as ReactElement<{ refreshToken?: number }>, { refreshToken: chatsRefreshToken })
              : chatContent}
          </div>
        )}
        {section === 'schedules' && <WorkflowScheduleRunsPanel
          embedded
          active
          entityType={entityType}
          workflowScope={workflowScope}
          canManage={canManage}
          scopeNoun={scopeNoun}
          showAutomationTabs={false}
          hideHeader
          refreshToken={schedulesRefreshToken}
          onStatus={setSchedulesStatus}
          onClose={() => {}}
        />}
        {section === 'triggers' && entityType === 'workflow' && <WorkflowAPITriggersView
          workspacePath={workspacePath}
          deliveryHistory={<TriggerDeliveryHistoryPanel workspacePath={workspacePath} entityType="workflow" />}
          hideHeader
          refreshToken={triggersRefreshToken}
          onCounts={setTriggersCounts}
        />}
        {section === 'triggers' && entityType === 'product' && productTriggerScope && <ProductAPITriggersView
          scope={productTriggerScope}
          deliveryHistory={<TriggerDeliveryHistoryPanel workspacePath={workspacePath} entityType="product" productTriggerScope={productTriggerScope} />}
          hideHeader
          refreshToken={triggersRefreshToken}
          onCounts={setTriggersCounts}
        />}
        {section === 'bots' && botContent}
        </Suspense>
      </div>
    </div>
  )
}
