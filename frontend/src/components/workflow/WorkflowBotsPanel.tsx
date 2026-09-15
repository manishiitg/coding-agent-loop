import { AlertCircle, AlertTriangle, ArrowLeft, MessageSquare, Phone } from 'lucide-react'
import { useWorkflowBots } from './bots/useWorkflowBots'
import { SlackSetup } from './bots/SlackSetup'
import { WhatsAppSetup } from './bots/WhatsAppSetup'
import { GmailNotifications } from './bots/GmailNotifications'
import { RouteChip } from './bots/RouteChips'
import { ChannelRow } from './bots/AddChannel'
import { routeId } from './bots/types'

type WorkflowBotsPanelProps = {
  workspacePath: string | null
}

// Composition over useWorkflowBots: status card, route chips, add-channel
// rows, the Slack/WhatsApp drill-ins, and the shared Gmail block.
export default function WorkflowBotsPanel({ workspacePath }: WorkflowBotsPanelProps) {
  const bots = useWorkflowBots(workspacePath)
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
          <span className="text-xs font-normal text-muted-foreground">· shared by all workflows</span>
        </div>
        {setup === 'slack' ? <SlackSetup bots={bots} /> : <WhatsAppSetup bots={bots} />}
      </div>
    )
  }

  // ── Main view ─────────────────────────────────────────────────────────────

  return (
    <div className="space-y-4">
      {/* This workflow answers on */}
      <div className="rounded-lg border border-border bg-muted/20 p-3">
        <div className="mb-1.5 text-sm font-medium text-muted-foreground">This workflow answers on</div>
        {!workflowId ? (
          <p className="text-xs text-muted-foreground">This panel needs an active workflow folder.</p>
        ) : myRoutes.length === 0 ? (
          <p className="text-xs text-muted-foreground">No bot routes yet — add one below.</p>
        ) : (
          <div className="flex flex-wrap gap-1.5">{myRoutes.map(route => <RouteChip key={routeId(route)} bots={bots} route={route} />)}</div>
        )}
        {routeError && (
          <p className="mt-2 flex items-start gap-1.5 text-xs text-red-600 dark:text-red-400">
            <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />{routeError}
          </p>
        )}
        {waRoutingError && (
          <p className="mt-2 flex items-start gap-1.5 text-xs text-amber-600 dark:text-amber-400">
            <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />WhatsApp routing unavailable: {waRoutingError}
          </p>
        )}
      </div>

      {/* Add a bot route */}
      <div>
        <div className="mb-1.5 text-sm font-medium text-muted-foreground">Add a bot route</div>
        <div className="divide-y divide-border overflow-hidden rounded-md border border-border bg-background">
          <ChannelRow bots={bots} kind="slack" />
          <ChannelRow bots={bots} kind="whatsapp" />
        </div>
      </div>

      {/* Email notifications (Gmail) — account-wide, shared by every workflow */}
      <GmailNotifications bots={bots} />
    </div>
  )
}
