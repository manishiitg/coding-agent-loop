import { useEffect, useState, type ReactNode } from 'react'
import { AlertTriangle, CheckCircle, Loader2, MessageSquare, Plus, Users } from 'lucide-react'
import { Button } from '../../ui/Button'
import { FormSection } from '../../ui/FormSection'
import { Input } from '../../ui/Input'
import { Label } from '../../ui/label'
import { SecretField } from '../../ui/SecretField'
import { ToggleRow } from '../../ui/ToggleRow'
import { READ_ONLY_TITLE } from '../../../hooks/useCanWriteWorkflow'
import type { SlackConnection } from '../../../services/api-types'
import type { WorkflowBots } from './useWorkflowBots'
import { StatusBanner } from './StatusBanner'
import { SharedSlackBotSettings } from '../../admin/SlackAdminPanel'
import { RouteChip } from './RouteChips'
import { SlackAppSetupSteps, SlackChecksView, SlackManifestSetup, SlackPermissionsChecklist, SlackTokenHint } from './SlackAppSetupSteps'
import { routeId } from './types'

// The Slack tab for one workflow or crew project answers a single question:
// who answers for it in Slack?
//
//  - Its own bot: a Slack app scoped to this workflow/project. It answers
//    only for it, in any channel it is invited to; no channel setup.
//  - The shared bot: the platform's default app. It answers for this
//    workflow in the channels routed to it.
//
// Platform plumbing (enable switch, the shared app's tokens) is admin-only:
// Access → Slack, or collapsed under the shared bot option here.

const OWNER_ONLY_TITLE = 'Only an owner can manage this Slack bot'

type SlackMode = 'own' | 'shared'

function connStatus(conn: SlackConnection): { label: string; ok: boolean } {
  if (!conn.configured) return { label: 'Missing tokens', ok: false }
  if (!conn.enabled) return { label: 'Disabled', ok: false }
  return { label: 'Ready', ok: true }
}

function StatusDot({ ok, label }: { ok: boolean; label: string }) {
  return (
    <span className={`inline-flex items-center gap-1 text-[11px] font-medium ${ok ? 'text-emerald-600 dark:text-emerald-400' : 'text-amber-600 dark:text-amber-400'}`}>
      <span className={`h-1.5 w-1.5 rounded-full ${ok ? 'bg-emerald-500' : 'bg-amber-500'}`} />
      {label}
    </span>
  )
}

function ModeOption({ checked, disabled, title, onSelect, icon, label, hint }: {
  checked: boolean; disabled: boolean; title?: string; onSelect: () => void; icon: ReactNode; label: string; hint: string
}) {
  return (
    <label
      title={title}
      className={`flex flex-1 items-start gap-2 rounded-md border p-3 text-sm transition-colors ${checked ? 'border-primary bg-primary/5' : 'border-border'} ${disabled ? 'opacity-60' : 'cursor-pointer hover:bg-muted/40'}`}
    >
      <input type="radio" name="slack-mode" className="mt-1" checked={checked} disabled={disabled} onChange={onSelect} />
      <span className="min-w-0">
        <span className="flex items-center gap-1.5 font-medium text-foreground">{icon}{label}</span>
        <span className="mt-0.5 block text-xs text-muted-foreground">{hint}</span>
      </span>
    </label>
  )
}

type SlackSetupBots = Pick<WorkflowBots,
  | 'readOnly' | 'workflowId' | 'slackAppDefaultName'
  | 'slackOriginal' | 'loadSlack' | 'canManageSlackDefault' | 'slackLoading' | 'slackError' | 'slackSuccess'
  | 'canManageWorkflowSlack' | 'hasProfileTarget' | 'slackSelection'
  | 'slackConnName' | 'setSlackConnName' | 'slackConnBot' | 'setSlackConnBot' | 'slackConnApp' | 'setSlackConnApp'
  | 'slackConnEnabled' | 'setSlackConnEnabled' | 'slackConnSaving' | 'slackConnTesting' | 'slackConnTestResult'
  | 'slackConnConfirmDelete' | 'slackConnHasChanges' | 'saveWorkflowSlackConnection'
  | 'testWorkflowSlackConnection' | 'removeWorkflowSlackConnection'
  | 'workflowRoutes' | 'routeError' | 'newSlackChannel' | 'setNewSlackChannel' | 'addSlackRoute'
  | 'routeSaving' | 'myRoutes' | 'addError' | 'setAddError'
  | 'expandedChip' | 'setExpandedChip' | 'removeRoute' | 'updateRoute'
>

export function SlackSetup({ bots, headerAction }: { bots: SlackSetupBots; headerAction?: ReactNode }) {
  const {
    readOnly, workflowId, slackOriginal, slackLoading, slackError, slackSuccess,
    canManageWorkflowSlack, hasProfileTarget, slackSelection,
    workflowRoutes, routeError,
  } = bots
  const noun = hasProfileTarget ? 'project' : 'workflow'
  const own = slackSelection.own
  const shared = (slackOriginal.connections || []).find(conn => conn.is_default) || null
  const slackRoutes = workflowRoutes.filter(route => route.kind === 'slack')
  const ownTitle = canManageWorkflowSlack ? undefined : (readOnly ? READ_ONLY_TITLE : OWNER_ONLY_TITLE)

  // The own bot is the source of truth: it answers whenever it exists. The
  // radio only chooses what to show while nothing is set up yet.
  const [mode, setMode] = useState<SlackMode>(own ? 'own' : slackRoutes.length > 0 ? 'shared' : 'own')
  const ownId = own?.id || null
  useEffect(() => {
    if (ownId) setMode('own')
  }, [ownId])
  const [editing, setEditing] = useState(false)
  useEffect(() => { setEditing(false) }, [ownId])

  if (slackLoading) {
    return <div className="flex items-center justify-center py-12"><Loader2 className="h-8 w-8 animate-spin text-primary" /></div>
  }

  return (
    <div className="space-y-4">
      {slackError && <StatusBanner tone="error">{slackError}</StatusBanner>}
      {slackSuccess && <StatusBanner tone="success">{slackSuccess}</StatusBanner>}

      <FormSection title={`Who answers for this ${noun} in Slack?`} actions={headerAction}>
        <div className="flex flex-col gap-2 sm:flex-row">
          <ModeOption
            checked={mode === 'own'}
            disabled={false}
            onSelect={() => setMode('own')}
            icon={<MessageSquare className="h-3.5 w-3.5" />}
            label={`Its own bot${own ? ` · ${own.display_name}` : ''}`}
            hint={`Answers only for this ${noun}, in any channel it's invited to. Recommended.`}
          />
          <ModeOption
            checked={mode === 'shared'}
            disabled={false}
            onSelect={() => setMode('shared')}
            icon={<Users className="h-3.5 w-3.5" />}
            label={`Shared bot${shared ? ` · ${shared.display_name}` : ''}`}
            hint="One bot for many workflows. Answers here in the channels you pick."
          />
        </div>
      </FormSection>

      {mode === 'own' ? (
        <OwnBotSection bots={bots} noun={noun} ownTitle={ownTitle} editing={editing || !own} onEdit={setEditing} />
      ) : (
        <SharedBotSection bots={bots} noun={noun} ownTitle={ownTitle} shared={shared} />
      )}

      {mode === 'own' && slackRoutes.length > 0 && (
        <section className="space-y-2">
          <h3 className="text-xs font-medium text-muted-foreground">Also answers through the shared bot in</h3>
          <div className="grid gap-2">
            {slackRoutes.map(route => <RouteChip key={routeId(route)} bots={bots} route={route} />)}
          </div>
        </section>
      )}
      {routeError && <StatusBanner tone="error">{routeError}</StatusBanner>}

      {!workflowId && <p className="text-xs text-muted-foreground">Select a {noun} to set up Slack.</p>}
    </div>
  )
}

function OwnBotSection({ bots, noun, ownTitle, editing, onEdit }: {
  bots: SlackSetupBots; noun: string; ownTitle?: string; editing: boolean; onEdit: (editing: boolean) => void
}) {
  const {
    canManageWorkflowSlack, slackSelection, slackConnConfirmDelete, removeWorkflowSlackConnection,
    slackConnName, setSlackConnName, slackConnBot, setSlackConnBot, slackConnApp, setSlackConnApp,
    slackConnEnabled, setSlackConnEnabled, slackConnSaving, slackConnTesting, slackConnTestResult,
    slackConnHasChanges, saveWorkflowSlackConnection, testWorkflowSlackConnection, slackAppDefaultName,
  } = bots
  const own = slackSelection.own
  const inviteHint = <>Invite it to a channel with <code className="rounded bg-muted px-1 font-mono">/invite @{own?.display_name || 'YourBot'}</code>, then @mention it. No channel setup needed here.</>

  if (own && !editing) {
    const status = connStatus(own)
    return (
      <FormSection
        title={<span className="flex items-center gap-2">{own.display_name}<StatusDot ok={status.ok} label={status.label} /></span>}
        description={inviteHint}
        actions={<Button variant="ghost" size="xs" onClick={() => onEdit(true)} disabled={!canManageWorkflowSlack} title={ownTitle}>Edit</Button>}
      >
        <div className="flex items-center gap-2">
          <Button variant="outline" onClick={() => void testWorkflowSlackConnection()} disabled={!canManageWorkflowSlack || slackConnTesting || slackConnSaving} title={ownTitle}>
            {slackConnTesting ? <><Loader2 className="mr-2 h-4 w-4 animate-spin" />Testing...</> : 'Test bot'}
          </Button>
          <Button variant="outline" onClick={() => void removeWorkflowSlackConnection()} disabled={!canManageWorkflowSlack || slackConnSaving || slackConnTesting} title={ownTitle} className="ml-auto">
            {slackConnConfirmDelete ? 'Click again to remove' : 'Remove bot'}
          </Button>
        </div>
        {slackConnTestResult && <SlackChecksView result={slackConnTestResult} />}
      </FormSection>
    )
  }

  return (
    <FormSection title={own ? `Edit ${own.display_name}` : `Set up this ${noun}'s bot`} description={<>Create a Slack app for this {noun} and paste its two tokens. {inviteHint}</>}>
      <SlackManifestSetup defaultName={slackConnName || slackAppDefaultName} finalStep={<>Save, then in Slack run <b>/invite @YourBot</b> in any channel and @mention it.</>} />
      <SlackAppSetupSteps finalStep={<>Save below, then in Slack run <b>/invite @YourBot</b> in any channel and @mention it.</>} />
      <SlackPermissionsChecklist />
      <div>
        <Label className="mb-2 block">Bot name</Label>
        <Input type="text" value={slackConnName} onChange={e => setSlackConnName(e.target.value)} disabled={!canManageWorkflowSlack} placeholder="e.g. Support bot" title={ownTitle} />
      </div>
      <SecretField label="Bot Token" hint={<SlackTokenHint kind="bot" />} value={slackConnBot} onChange={setSlackConnBot} disabled={!canManageWorkflowSlack} placeholder="xoxb-..." disabledTitle={ownTitle} />
      <SecretField label="App Token (Socket Mode)" hint={<SlackTokenHint kind="app" />} value={slackConnApp} onChange={setSlackConnApp} disabled={!canManageWorkflowSlack} placeholder="xapp-..." disabledTitle={ownTitle} />
      {own && (
        <ToggleRow label="Bot enabled" checked={slackConnEnabled} onCheckedChange={setSlackConnEnabled} disabled={!canManageWorkflowSlack} disabledTitle={ownTitle} />
      )}
      <div className="flex items-center gap-2">
        <Button onClick={() => void saveWorkflowSlackConnection()} disabled={!canManageWorkflowSlack || !slackConnHasChanges || slackConnSaving || slackConnTesting} title={ownTitle} className="flex items-center gap-2">
          {slackConnSaving ? <><Loader2 className="h-4 w-4 animate-spin" />Saving...</> : <><CheckCircle className="h-4 w-4" />Save bot</>}
        </Button>
        <Button variant="outline" onClick={() => void testWorkflowSlackConnection()} disabled={!canManageWorkflowSlack || slackConnTesting || slackConnSaving} title={ownTitle}>
          {slackConnTesting ? <><Loader2 className="mr-2 h-4 w-4 animate-spin" />Testing...</> : 'Save & test'}
        </Button>
        {own && (
          <Button variant="outline" onClick={() => onEdit(false)} disabled={slackConnSaving || slackConnTesting} className="ml-auto">
            Done
          </Button>
        )}
      </div>
      {slackConnTestResult && <SlackChecksView result={slackConnTestResult} />}
    </FormSection>
  )
}

function SharedBotSection({ bots, noun, ownTitle, shared }: {
  bots: SlackSetupBots; noun: string; ownTitle?: string; shared: SlackConnection | null
}) {
  const {
    readOnly, workflowId, canManageWorkflowSlack, canManageSlackDefault, loadSlack, slackSelection, slackConnSaving, slackConnConfirmDelete,
    removeWorkflowSlackConnection, workflowRoutes, newSlackChannel, setNewSlackChannel, addSlackRoute,
    routeSaving, myRoutes, addError, setAddError,
  } = bots
  const own = slackSelection.own
  const routes = workflowRoutes.filter(route => route.kind === 'slack')
  const status = shared ? connStatus(shared) : { label: canManageSlackDefault ? 'Not set up · see shared bot settings below' : 'Not set up · ask an admin (Access → Slack)', ok: false }
  const adding = routeSaving?.startsWith('slack:') && !myRoutes.some(route => routeId(route) === routeSaving)
  const error = addError.slack

  return (
    <FormSection
      title={<span className="flex items-center gap-2">{shared?.display_name || 'Shared bot'}<StatusDot ok={status.ok} label={status.label} /></span>}
      description={`Add the channels where the shared bot answers for this ${noun}. Invite the bot there, then find the channel ID (starts with C) under View channel details.`}
    >
      {own && (
        <div className="flex items-start gap-2 rounded-md border border-amber-300 bg-amber-50 p-2 text-xs text-amber-800 dark:border-amber-700 dark:bg-amber-900/20 dark:text-amber-200">
          <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
          <span className="flex-1">This {noun}'s own bot <b>{own.display_name}</b> still answers wherever it's invited. Remove it to use only the shared bot.</span>
          <Button variant="link" size="xs" onClick={() => void removeWorkflowSlackConnection()} disabled={!canManageWorkflowSlack || slackConnSaving} title={ownTitle} className="h-auto shrink-0 p-0 text-amber-800 dark:text-amber-200">
            {slackConnConfirmDelete ? 'Click again to remove' : 'Remove own bot'}
          </Button>
        </div>
      )}
      {workflowId && (
        <div className="flex items-center gap-2">
          <Input
            type="text"
            value={newSlackChannel}
            onChange={e => {
              setNewSlackChannel(e.target.value.toUpperCase().replace(/[^A-Z0-9_,;\s-]/g, ''))
              if (error) setAddError(prev => ({ ...prev, slack: undefined }))
            }}
            onKeyDown={e => { if (e.key === 'Enter') addSlackRoute() }}
            placeholder="channel ID, e.g. C1234567890"
            disabled={readOnly || !!adding}
            title={readOnly ? READ_ONLY_TITLE : undefined}
            className="h-8 min-w-0 flex-1 font-mono text-xs"
          />
          <Button variant="outline" size="sm" onClick={addSlackRoute} disabled={readOnly || !newSlackChannel.trim() || !!adding} title={readOnly ? READ_ONLY_TITLE : undefined}>
            {adding ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Plus className="h-3.5 w-3.5" />}
            Add channel
          </Button>
        </div>
      )}
      {error && <p className="text-xs text-red-600 dark:text-red-400">{error}</p>}
      {routes.length > 0 ? (
        <div className="grid gap-2">
          {routes.map(route => <RouteChip key={routeId(route)} bots={bots} route={route} />)}
        </div>
      ) : (
        <p className="text-xs text-muted-foreground">No channels yet.</p>
      )}
      {canManageSlackDefault && (
        <details className="border-t border-border pt-2 text-xs">
          <summary className="cursor-pointer select-none font-medium text-muted-foreground">Shared bot settings (admin)</summary>
          <div className="mt-3">
            <SharedSlackBotSettings onSaved={() => void loadSlack()} />
          </div>
        </details>
      )}
    </FormSection>
  )
}
