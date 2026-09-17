import { AlertCircle, ArrowLeft, MessageSquare, Phone } from 'lucide-react'
import { useWorkflowBots, type BotRouteTarget } from './bots/useWorkflowBots'
import { SlackSetup } from './bots/SlackSetup'
import { WhatsAppSetup } from './bots/WhatsAppSetup'
import { RouteChip } from './bots/RouteChips'
import { ChannelRow } from './bots/AddChannel'
import { routeId } from './bots/types'
import { AskAIButton } from './AskAIButton'

type WorkflowBotsPanelProps = {
  workspacePath: string | null
  target?: BotRouteTarget
  scopeNoun?: 'workflow' | 'project'
  onAsk?: (message: string) => void | Promise<void>
}

// Composition over useWorkflowBots: status card, route chips, add-channel
// rows, and the Slack/WhatsApp drill-ins.
export default function WorkflowBotsPanel({ workspacePath, target, scopeNoun = 'workflow', onAsk }: WorkflowBotsPanelProps) {
  const bots = useWorkflowBots(workspacePath, target, 'bots')
  const { setup, setSetup, workflowId, myRoutes, routeError, waRoutingError } = bots

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
        {setup === 'slack' && askSlackSetup}
        {setup === 'slack' ? <SlackSetup bots={bots} /> : <WhatsAppSetup bots={bots} />}
      </div>
    )
  }

  return (
    <div className="space-y-4">
      {(['slack', 'whatsapp'] as const).map(kind => {
        const routes = myRoutes.filter(route => route.kind === kind)
        return (
          <section key={kind} className="overflow-hidden rounded-md border border-border bg-background">
            <ChannelRow bots={bots} kind={kind} manageRoutes headerAction={kind === 'slack' ? askSlackSetup : undefined} />
            <div className="space-y-2 px-3 pb-3">
              <h3 className="text-xs font-medium text-muted-foreground">Routes for this {scopeNoun}</h3>
              {!workflowId ? (
                <p className="text-xs text-muted-foreground">Select a {scopeNoun} to manage routes.</p>
              ) : routes.length === 0 ? (
                <p className="text-xs text-muted-foreground">No routes yet.</p>
              ) : (
                <div className="grid max-h-96 gap-2 overflow-y-auto sm:grid-cols-2">
                  {routes.map(route => <RouteChip key={routeId(route)} bots={bots} route={route} />)}
                </div>
              )}
              {routeError && <p className="flex items-start gap-1.5 text-xs text-red-600 dark:text-red-400"><AlertCircle className="h-3.5 w-3.5 shrink-0" />{routeError}</p>}
              {kind === 'whatsapp' && waRoutingError && <p className="text-xs text-amber-600 dark:text-amber-400">WhatsApp routing unavailable: {waRoutingError}</p>}
            </div>
          </section>
        )
      })}
    </div>
  )
}
