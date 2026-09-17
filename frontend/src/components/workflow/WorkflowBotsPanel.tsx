import { AlertCircle, ArrowLeft, MessageSquare, Phone } from 'lucide-react'
import { useWorkflowBots, type BotRouteTarget } from './bots/useWorkflowBots'
import { SlackSetup } from './bots/SlackSetup'
import { WhatsAppSetup } from './bots/WhatsAppSetup'
import { RouteChip } from './bots/RouteChips'
import { ChannelRow } from './bots/AddChannel'
import { routeId } from './bots/types'

type WorkflowBotsPanelProps = {
  workspacePath: string | null
  target?: BotRouteTarget
  scopeNoun?: 'workflow' | 'project'
}

// Composition over useWorkflowBots: status card, route chips, add-channel
// rows, and the Slack/WhatsApp drill-ins.
export default function WorkflowBotsPanel({ workspacePath, target, scopeNoun = 'workflow' }: WorkflowBotsPanelProps) {
  const bots = useWorkflowBots(workspacePath, target, 'bots')
  const { setup, setSetup, workflowId, myRoutes, routeError, waRoutingError } = bots

  if (setup !== null) {
    return (
      <div className="space-y-4">
        <button
          type="button"
          onClick={() => setSetup(null)}
          className="inline-flex items-center gap-1.5 text-sm font-medium text-muted-foreground transition-colors hover:text-foreground"
        >
          <ArrowLeft className="h-4 w-4" />
          Back to bots
        </button>
        <div className="flex items-center gap-2 text-sm font-semibold text-foreground">
          {setup === 'slack' ? <MessageSquare className="h-4 w-4" /> : <Phone className="h-4 w-4" />}
          {setup === 'slack' ? 'Slack' : 'WhatsApp'}
          <span className="text-xs font-normal text-muted-foreground">· shared across AgentWorks</span>
        </div>
        <section className="space-y-3 rounded-lg border border-border p-3">
          <h3 className="text-sm font-medium">Routes for this {scopeNoun}</h3>
          {!workflowId ? (
            <p className="text-xs text-muted-foreground">This panel needs an active {scopeNoun}.</p>
          ) : myRoutes.filter(route => route.kind === setup).length === 0 ? (
            <p className="text-xs text-muted-foreground">No routes yet. Connect the bot and add a route below.</p>
          ) : (
            <div className="grid max-h-96 gap-2 overflow-y-auto sm:grid-cols-2">
              {myRoutes.filter(route => route.kind === setup).map(route => <RouteChip key={routeId(route)} bots={bots} route={route} />)}
            </div>
          )}
          {routeError && <p className="flex items-start gap-1.5 text-xs text-red-600 dark:text-red-400"><AlertCircle className="h-3.5 w-3.5 shrink-0" />{routeError}</p>}
          {setup === 'whatsapp' && waRoutingError && <p className="text-xs text-amber-600 dark:text-amber-400">WhatsApp routing unavailable: {waRoutingError}</p>}
          <ChannelRow bots={bots} kind={setup} manageRoutes showSettings={false} />
        </section>
        {setup === 'slack' ? <SlackSetup bots={bots} /> : <WhatsAppSetup bots={bots} />}
      </div>
    )
  }

  return (
    <div className="divide-y divide-border overflow-hidden rounded-md border border-border bg-background">
      <ChannelRow bots={bots} kind="slack" />
      <ChannelRow bots={bots} kind="whatsapp" />
    </div>
  )
}
