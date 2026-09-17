import { useState } from 'react'
import { ChevronDown, Loader2, MessageSquare, Phone, PlayCircle, Trash2 } from 'lucide-react'
import { READ_ONLY_TITLE } from '../../../hooks/useCanWriteWorkflow'
import { routeId, type WorkflowRoute } from './types'
import type { WorkflowBots } from './useWorkflowBots'

// One route card in "this workflow answers on".

type RouteChipBots = Pick<WorkflowBots, 'readOnly' | 'expandedChip' | 'setExpandedChip' | 'routeSaving' | 'removeRoute' | 'updateRoute'>

export function RouteChip({ bots, route }: { bots: RouteChipBots; route: WorkflowRoute }) {
  const { readOnly, expandedChip, setExpandedChip, routeSaving, removeRoute, updateRoute } = bots

  const [blockedEmails, setBlockedEmails] = useState((route.blocked_emails || []).join(", "))
  const id = routeId(route)
  const expanded = expandedChip === id
  const saving = routeSaving === id
  const channelLabel = route.kind === 'slack' ? route.key : `@${route.key}`
  const Icon = route.kind === 'slack' ? MessageSquare : Phone
  const platformLabel = route.kind === 'slack' ? 'Slack' : 'WhatsApp'


  return (
    <div className="min-w-0 rounded-md border border-border bg-background p-2 shadow-sm">
      <div className="flex min-w-0 items-start gap-2">
        <span className="mt-0.5 rounded-md border border-border bg-muted/40 p-1.5 text-muted-foreground">
          <Icon className="h-3.5 w-3.5" />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center gap-1.5">
            <span className="text-xs font-medium text-muted-foreground">{platformLabel}</span>
            {saving && <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />}
          </div>
          <div className="truncate font-mono text-sm font-semibold text-foreground" title={channelLabel}>{channelLabel}</div>
        </div>
        {!saving && (
          <button
            type="button"
            onClick={() => void removeRoute(route)}
            disabled={readOnly}
            className="rounded-md p-1 text-muted-foreground transition-colors hover:bg-red-500/10 hover:text-red-600 disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-transparent disabled:hover:text-muted-foreground"
            aria-label={`Stop answering on ${platformLabel} ${channelLabel}`}
            title={readOnly ? READ_ONLY_TITLE : 'Remove from this workflow'}
          >
            <Trash2 className="h-3.5 w-3.5" />
          </button>
        )}
      </div>
      <div className="mt-2 flex items-center gap-1 text-xs text-muted-foreground"><PlayCircle className="h-3.5 w-3.5" />Run</div>
      <button
        type="button"
        onClick={() => setExpandedChip(expanded ? null : id)}
        className="mt-2 inline-flex items-center gap-1 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground"
        aria-expanded={expanded}
      >
        Options
        <ChevronDown className={`h-3 w-3 transition-transform ${expanded ? 'rotate-180' : ''}`} />
      </button>
      {expanded && (
        <div className="mt-1.5 flex flex-wrap items-center gap-3 rounded-md border border-border bg-muted/20 px-3 py-2 text-xs">
          <span className="text-muted-foreground">The bot can run existing work. Channel members cannot change the workflow.</span>
          {route.kind === 'slack' && (
            <label className="w-full text-muted-foreground">
              Blocked email addresses
              <input aria-label="Blocked email addresses" className="mt-1 w-full rounded border border-border bg-background px-2 py-1" value={blockedEmails} disabled={readOnly || saving} onChange={e => setBlockedEmails(e.target.value)} placeholder="person@example.com, another@example.com" />
              <button type="button" className="mt-1 rounded border border-border px-2 py-1" disabled={readOnly || saving} onClick={() => void updateRoute(route, { blocked_emails: blockedEmails.split(',').map(email => email.trim()).filter(Boolean) })}>Save exclusions</button>
              <span className="mt-1 block">When exclusions exist, Slack must provide a verified email. Removing all exclusions allows everyone in the channel.</span>
            </label>
          )}
          {route.kind === 'slack' ? (
            <p className="w-full text-muted-foreground">Slack shows workflow progress, step updates, and the final reply in the conversation thread.</p>
          ) : (
            <div className="w-full space-y-1">
              <label className="flex items-center gap-1.5 text-muted-foreground">
                <input
                  type="checkbox"
                  checked={!!route.send_full_details}
                  disabled={readOnly || saving}
                  onChange={e => void updateRoute(route, { send_full_details: e.target.checked })}
                  className="h-3.5 w-3.5"
                />
                Show workflow progress
              </label>
              <p className="text-muted-foreground">Include workflow progress and step updates alongside replies, requests for input, and errors.</p>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
