import { AlertCircle, ArrowLeft, MessageSquare, Phone } from 'lucide-react'
import { useState } from 'react'
import { useWorkflowBots, type BotRouteTarget } from './bots/useWorkflowBots'
import { SlackSetup } from './bots/SlackSetup'
import { WhatsAppSetup } from './bots/WhatsAppSetup'
import { RouteChip } from './bots/RouteChips'
import { ChannelRow } from './bots/AddChannel'
import { routeId } from './bots/types'
import { AskAIButton } from './AskAIButton'
import { WorkspaceViewTabs } from './WorkspaceViewTabs'

type WorkflowBotsPanelProps = {
  workspacePath: string | null
  target?: BotRouteTarget
  scopeNoun?: 'workflow' | 'project'
  onAsk?: (message: string) => void | Promise<void>
  /**
   * Pins the panel to one channel with no tab row. The Integrations view
   * mounts one pinned panel per top-level tab; standalone hosts (the Work
   * project pane) omit it and get the Slack/WhatsApp tabs.
   */
  fixedChannel?: BotChannel
}

type BotChannel = 'slack' | 'whatsapp'

const CHANNEL_TABS: Array<{ value: BotChannel; label: string }> = [
  { value: 'slack', label: 'Slack' },
  { value: 'whatsapp', label: 'WhatsApp' },
]

// Composition over useWorkflowBots: status card, route chips, add-channel
// rows, and the Slack/WhatsApp drill-ins. Never owns a scroll container:
// every host (the Integrations Bots tab, the Work project pane) scrolls.
export default function WorkflowBotsPanel({ workspacePath, target, scopeNoun = 'workflow', onAsk, fixedChannel }: WorkflowBotsPanelProps) {
  const bots = useWorkflowBots(workspacePath, target, 'bots')
  const { setup, setSetup, workflowId, workflowRoutes, routeError, waRoutingError } = bots
  const [internalChannel, setInternalChannel] = useState<BotChannel>('slack')
  const channel = fixedChannel ?? internalChannel

  const askSlackSetup = (
    <AskAIButton
      workspacePath={workspacePath}
      onAsk={onAsk}
      label={scopeNoun === 'project' ? 'Ask Crew to set up Slack' : 'Ask Builder to set up Slack'}
      message={`Read builder-reference/references/slack-bot-routing.md, inspect get_slack_bot_settings, and help me set up the Slack bot and channel routes for this ${scopeNoun}. Guide me through app creation and credentials in the settings UI without requesting tokens in chat. Use the existing scoped tools to create the route once the channel ID is clear; use run mode without asking for a grant. Everyone in the channel is allowed by default; ask about blocked emails only if I need exclusions.`}
    />
  )

  if (setup !== null) {
    return (
      <div className="flex flex-col gap-4">
        <div className="shrink-0 space-y-4">
          <button
            type="button"
            onClick={() => setSetup(null)}
            className="inline-flex items-center gap-1.5 text-sm font-medium text-muted-foreground transition-colors hover:text-foreground"
          >
            <ArrowLeft className="h-4 w-4" />
            Back
          </button>
          <div className="flex items-center gap-2 text-sm font-semibold text-foreground">
            {setup === 'slack' ? <MessageSquare className="h-4 w-4" /> : <Phone className="h-4 w-4" />}
            {setup === 'slack' ? 'Slack' : 'WhatsApp'}
            <span className="text-xs font-normal text-muted-foreground">· shared across AgentWorks</span>
          </div>
          {setup === 'slack' && askSlackSetup}
        </div>
        {setup === 'slack' ? <SlackSetup bots={bots} /> : <WhatsAppSetup bots={bots} />}
      </div>
    )
  }

  const routes = workflowRoutes.filter(route => route.kind === channel)

  return (
    <div className="space-y-4">
      {fixedChannel === undefined && (
        <div className="border-b border-border">
          <WorkspaceViewTabs value={channel} onChange={(value: string) => setInternalChannel(value as BotChannel)} options={CHANNEL_TABS} ariaLabel="Bots" />
        </div>
      )}
      <section className="overflow-hidden rounded-md border border-border bg-background">
        <ChannelRow bots={bots} kind={channel} manageRoutes headerAction={channel === 'slack' ? askSlackSetup : undefined} />
        <div className="space-y-2 px-3 pb-3">
          <h3 className="text-xs font-medium text-muted-foreground">{target ? `Routes for this ${scopeNoun}` : 'Workflow routes'}</h3>
          {routes.length > 0 ? (
            <div className="grid gap-2">
              {routes.map(route => <RouteChip key={routeId(route)} bots={{ ...bots, readOnly: bots.readOnly || !route.current_target }} route={route} />)}
            </div>
          ) : (
            <p className="text-xs text-muted-foreground">{workflowId ? 'No routes yet.' : `Select a ${scopeNoun} to manage routes.`}</p>
          )}
          {routeError && <p className="flex items-start gap-1.5 text-xs text-red-600 dark:text-red-400"><AlertCircle className="h-3.5 w-3.5 shrink-0" />{routeError}</p>}
          {channel === 'whatsapp' && waRoutingError && <p className="text-xs text-amber-600 dark:text-amber-400">WhatsApp routing unavailable: {waRoutingError}</p>}
        </div>
      </section>
      {channel === 'slack' && (
        <section className="space-y-2">
          <h3 className="text-xs font-medium text-muted-foreground">Settings</h3>
          <SlackSetup bots={bots} />
        </section>
      )}
    </div>
  )
}
