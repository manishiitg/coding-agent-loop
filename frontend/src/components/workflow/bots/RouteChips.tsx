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
      <div className="flex min-w-0 items-center gap-3">
        <Icon className="h-4 w-4 shrink-0 text-muted-foreground" aria-label={platformLabel} />
        <div className="min-w-0 flex-1 truncate font-mono text-sm font-semibold text-foreground" title={channelLabel}>{channelLabel}</div>
        {route.target_label && <span className="max-w-[35%] truncate text-xs text-muted-foreground" title={route.target_label}>{route.target_label}</span>}
        <span className="inline-flex shrink-0 items-center gap-1 text-xs text-muted-foreground"><PlayCircle className="h-3.5 w-3.5" />Run</span>
        <button
          type="button"
          onClick={() => setExpandedChip(expanded ? null : id)}
          className="inline-flex shrink-0 items-center gap-1 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground"
          aria-expanded={expanded}
        >
          Options
          <ChevronDown className={`h-3 w-3 transition-transform ${expanded ? 'rotate-180' : ''}`} />
        </button>
        {saving ? <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-muted-foreground" /> : (
          <button
            type="button"
            onClick={() => void removeRoute(route)}
            disabled={readOnly}
            className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-red-500/10 hover:text-red-600 disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-transparent disabled:hover:text-muted-foreground"
            aria-label={`Stop answering on ${platformLabel} ${channelLabel}`}
            title={readOnly ? READ_ONLY_TITLE : 'Remove from this workflow'}
          >
            <Trash2 className="h-3.5 w-3.5" />
          </button>
        )}
      </div>
      {expanded && (
        <div className="mt-1.5 flex flex-wrap items-center gap-3 rounded-md border border-border bg-muted/20 px-3 py-2 text-xs">
          {route.kind === 'slack' && (
            <label className="w-full text-muted-foreground">
              Blocked email addresses
              <input aria-label="Blocked email addresses" className="mt-1 w-full rounded border border-border bg-background px-2 py-1" value={blockedEmails} disabled={readOnly || saving} onChange={e => setBlockedEmails(e.target.value)} placeholder="person@example.com, another@example.com" />
              <button type="button" className="mt-1 rounded border border-border px-2 py-1" disabled={readOnly || saving} onClick={() => void updateRoute(route, { blocked_emails: blockedEmails.split(',').map(email => email.trim()).filter(Boolean) })}>Save</button>
              <span className="mt-1 block">Everyone in the channel is allowed unless blocked.</span>
            </label>
          )}
          {route.kind === 'whatsapp' && (
            <label className="flex items-center gap-1.5 text-muted-foreground" title="Include workflow progress and step updates alongside replies, requests for input, and errors.">
              <input
                type="checkbox"
                checked={!!route.send_full_details}
                disabled={readOnly || saving}
                onChange={e => void updateRoute(route, { send_full_details: e.target.checked })}
                className="h-3.5 w-3.5"
              />
              Show workflow progress
            </label>
          )}
        </div>
      )}
    </div>
  )
}
