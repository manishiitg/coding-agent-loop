import { AlertCircle, AlertTriangle, CheckCircle, Loader2 } from 'lucide-react'
import { Button } from '../../ui/Button'
import { Card } from '../../ui/Card'
import { FormSection } from '../../ui/FormSection'
import { Input } from '../../ui/Input'
import { Label } from '../../ui/label'
import { SecretField } from '../../ui/SecretField'
import { ToggleRow } from '../../ui/ToggleRow'
import { READ_ONLY_TITLE } from '../../../hooks/useCanWriteWorkflow'
import type { WorkflowBots } from './useWorkflowBots'
import { StatusBanner } from './StatusBanner'
import type { SlackTestResponse } from '../../../services/api-types'

const OWNER_ONLY_TITLE = 'Only a workflow owner can manage its Slack app'

// ── Drill-in: Slack setup ─────────────────────────────────────────────────

type SlackSetupBots = Pick<WorkflowBots,
  | 'readOnly'
  | 'slackConfig' | 'setSlackConfig' | 'slackLoading' | 'slackSaving' | 'slackTesting' | 'slackError' | 'slackSuccess'
  | 'testResult' | 'testReply' | 'pollingForReply' | 'showBotToken' | 'setShowBotToken' | 'showAppToken' | 'setShowAppToken'
  | 'handleSlackSave' | 'handleSlackTest' | 'slackHasChanges'
  | 'canManageSlackDefault' | 'canManageWorkflowSlack' | 'hasProfileTarget' | 'slackSelection'
  | 'slackConnName' | 'setSlackConnName' | 'slackConnBot' | 'setSlackConnBot' | 'slackConnApp' | 'setSlackConnApp'
  | 'slackConnEnabled' | 'setSlackConnEnabled' | 'slackConnSaving' | 'slackConnTesting' | 'slackConnTestResult'
  | 'slackConnConfirmDelete' | 'slackConnShowBot' | 'setSlackConnShowBot' | 'slackConnShowApp' | 'setSlackConnShowApp'
  | 'slackConnHasChanges' | 'saveWorkflowSlackConnection' | 'selectWorkflowSlackConnection'
  | 'testWorkflowSlackConnection' | 'removeWorkflowSlackConnection'
>

function SlackChecksView({ result }: { result: SlackTestResponse }) {
  return (
    <div className={`p-3 border rounded-lg flex items-start gap-2 ${result.success ? 'bg-green-50 dark:bg-green-900/20 border-green-200 dark:border-green-800' : 'bg-red-50 dark:bg-red-900/20 border-red-200 dark:border-red-800'}`}>
      {result.success ? <CheckCircle className="w-4 h-4 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" /> : <AlertCircle className="w-4 h-4 text-red-600 dark:text-red-400 flex-shrink-0 mt-0.5" />}
      <div className="min-w-0 space-y-2 text-sm">
        <p>{result.message}</p>
        {!!result.checks?.length && (
          <details open={!result.success}>
            <summary className="cursor-pointer font-medium">Setup checks</summary>
            <ul className="mt-2 space-y-2">
              {result.checks.map(check => (
                <li key={check.name}>
                  <div className="flex items-center justify-between gap-2">
                    <span className="font-medium">{check.name}</span>
                    <span className={check.status === 'passed' ? 'text-green-600 dark:text-green-400' : check.status === 'manual' ? 'text-amber-600 dark:text-amber-400' : 'text-red-600 dark:text-red-400'}>
                      {check.status === 'manual' ? 'Verify manually' : check.status === 'passed' ? 'Passed' : check.status === 'missing' ? 'Missing' : 'Failed'}
                    </span>
                  </div>
                  {check.status !== 'passed' && <p className="text-xs text-muted-foreground">{check.message}</p>}
                </li>
              ))}
            </ul>
          </details>
        )}
      </div>
    </div>
  )
}

export function SlackSetup({ bots }: { bots: SlackSetupBots }) {
  const {
    readOnly,
    slackConfig, setSlackConfig, slackLoading, slackSaving, slackTesting, slackError, slackSuccess,
    testResult, testReply, pollingForReply,
    handleSlackSave, handleSlackTest, slackHasChanges,
    canManageSlackDefault, canManageWorkflowSlack, hasProfileTarget, slackSelection,
    slackConnName, setSlackConnName, slackConnBot, setSlackConnBot, slackConnApp, setSlackConnApp,
    slackConnEnabled, setSlackConnEnabled, slackConnSaving, slackConnTesting, slackConnTestResult,
    slackConnConfirmDelete,
    slackConnHasChanges, saveWorkflowSlackConnection, selectWorkflowSlackConnection,
    testWorkflowSlackConnection, removeWorkflowSlackConnection,
  } = bots

  const defaultConn = (slackConfig.connections || []).find(c => c.is_default) || null
  const ownConn = slackSelection.own
  const effectiveConn = slackSelection.effective
  const selectionDangles = slackSelection.selectionId !== '' && !effectiveConn
  const defaultTitle = canManageSlackDefault ? undefined : 'Only a platform admin can change the shared default'
  const ownTitle = canManageWorkflowSlack ? undefined : (readOnly ? READ_ONLY_TITLE : OWNER_ONLY_TITLE)
  const botEnabled = slackConfig.enabled && !!slackConfig.bot_mode

  return (
    <div className="space-y-4">
      {slackLoading ? (
        <div className="flex items-center justify-center py-12"><Loader2 className="w-8 h-8 animate-spin text-primary" /></div>
      ) : (
        <>
          {slackError && <StatusBanner tone="error">{slackError}</StatusBanner>}
          {slackSuccess && <StatusBanner tone="success">{slackSuccess}</StatusBanner>}

          {/* Enable Slack (platform switch) */}
          <Card className="p-4">
            <ToggleRow
              label="Enable Slack bot"
              description={`Platform switch for @mentions, threads, and channel triggers${canManageSlackDefault ? '' : ' — a platform admin turns this on'}`}
              checked={botEnabled}
              onCheckedChange={checked => setSlackConfig({ ...slackConfig, enabled: checked, bot_mode: checked })}
              disabled={!canManageSlackDefault}
              disabledTitle={defaultTitle}
            />
          </Card>

          <p className="text-xs text-muted-foreground">
            Everyone in a routed channel can use the bot by default. To block specific users,
            add their email addresses under that channel’s Options → Blocked email addresses.
          </p>

          {/* This workflow/project's Slack app */}
          {(
            <FormSection
              title={hasProfileTarget ? 'This project’s Slack app' : 'This workflow’s Slack app'}
              description={effectiveConn
                ? <>Using <b>{effectiveConn.display_name}</b>{effectiveConn.is_default ? ' (platform default)' : hasProfileTarget ? ' (this project)' : ' (this workflow)'} · {effectiveConn.enabled ? 'enabled' : 'disabled'}{effectiveConn.configured ? '' : ' · missing tokens'}</>
                : selectionDangles
                  ? 'The selected app is gone — pick another below.'
                  : 'No Slack app selected yet.'}
            >
              {selectionDangles && (
                <StatusBanner tone="error">This {hasProfileTarget ? 'project' : 'workflow'} points at a Slack app that no longer exists. Sends will fail until you switch to the platform default or set up this {hasProfileTarget ? 'project’s' : 'workflow’s'} own app.</StatusBanner>
              )}
              <div className="space-y-2" title={ownTitle}>
                <label className={`flex items-center gap-2 text-sm ${!canManageWorkflowSlack ? 'opacity-60' : 'cursor-pointer'}`}>
                  <input
                    type="radio"
                    name="slack-app-selection"
                    checked={slackSelection.selectionId === ''}
                    disabled={!canManageWorkflowSlack || slackConnSaving}
                    onChange={() => void selectWorkflowSlackConnection(null)}
                  />
                  <span>Platform default{defaultConn ? ` — ${defaultConn.display_name}` : ' — not configured'}</span>
                </label>
                <label className={`flex items-center gap-2 text-sm ${!canManageWorkflowSlack || !ownConn ? 'opacity-60' : 'cursor-pointer'}`}>
                  <input
                    type="radio"
                    name="slack-app-selection"
                    checked={slackSelection.selectionId !== '' && !!effectiveConn}
                    disabled={!canManageWorkflowSlack || !ownConn || slackConnSaving}
                    onChange={() => ownConn && void selectWorkflowSlackConnection(ownConn.id)}
                  />
                  <span>This {hasProfileTarget ? 'project’s' : 'workflow’s'} own app{ownConn ? ` — ${ownConn.display_name}` : ' — set up below'}</span>
                </label>
              </div>

              <div>
                <Label className="mb-2 block">App name</Label>
                <Input type="text" value={slackConnName} onChange={e => setSlackConnName(e.target.value)} disabled={!canManageWorkflowSlack} placeholder="e.g. Support bot" title={ownTitle} />
              </div>
              <SecretField
                label="Bot Token"
                value={slackConnBot}
                onChange={setSlackConnBot}
                disabled={!canManageWorkflowSlack}
                placeholder="xoxb-..."
                disabledTitle={ownTitle}
              />
              <SecretField
                label="App Token (Socket Mode)"
                value={slackConnApp}
                onChange={setSlackConnApp}
                disabled={!canManageWorkflowSlack}
                placeholder="xapp-..."
                disabledTitle={ownTitle}
              />
              <ToggleRow
                label="Enable this app"
                checked={slackConnEnabled}
                onCheckedChange={setSlackConnEnabled}
                disabled={!canManageWorkflowSlack}
                disabledTitle={ownTitle}
              />
                <div className="flex items-center gap-2">
                  <Button onClick={() => void saveWorkflowSlackConnection()} disabled={!canManageWorkflowSlack || !slackConnHasChanges || slackConnSaving || slackConnTesting} title={ownTitle} className="flex items-center gap-2">
                    {slackConnSaving ? <><Loader2 className="w-4 h-4 animate-spin" />Saving...</> : <><CheckCircle className="w-4 h-4" />Save app</>}
                  </Button>
                  <Button variant="outline" onClick={() => void testWorkflowSlackConnection()} disabled={!canManageWorkflowSlack || slackConnTesting || slackConnSaving} title={ownTitle} className="flex items-center gap-2">
                    {slackConnTesting ? <><Loader2 className="w-4 h-4 animate-spin" />Testing...</> : 'Test app'}
                  </Button>
                  {ownConn && (
                    <Button variant="outline" onClick={() => void removeWorkflowSlackConnection()} disabled={!canManageWorkflowSlack || slackConnSaving || slackConnTesting} title={ownTitle} className="ml-auto">
                      {slackConnConfirmDelete ? 'Click again to remove' : 'Remove'}
                    </Button>
                  )}
                </div>
                <p className="text-xs text-muted-foreground">Saving a new app selects it for this {hasProfileTarget ? 'project' : 'workflow'}. Test saves first, then checks tokens and scopes.</p>
                {slackConnTestResult && <SlackChecksView result={slackConnTestResult} />}
            </FormSection>
          )}

          {botEnabled && (
            <>
              <Card className="p-4 bg-blue-50 dark:bg-blue-900/20 border-blue-300 dark:border-blue-700">
                <details>
                  <summary className="cursor-pointer text-sm font-semibold text-blue-800 dark:text-blue-200 select-none">
                    First time? Click for step-by-step setup instructions
                  </summary>
                  <div className="mt-3 text-xs text-blue-900 dark:text-blue-100 space-y-3">
                    <div>
                      <p className="font-semibold">1. Create a Slack App</p>
                      <p className="mt-1">Go to <a href="https://api.slack.com/apps" target="_blank" rel="noreferrer" className="underline">api.slack.com/apps</a> → <b>Create New App</b> → <b>From scratch</b>. Pick a name and your workspace.</p>
                    </div>
                    <div>
                      <p className="font-semibold">2. Add Bot Token Scopes</p>
                      <p className="mt-1">In the sidebar: <b>OAuth &amp; Permissions</b> → <b>Scopes</b> → <b>Bot Token Scopes</b>. Add at minimum:</p>
                      <ul className="mt-1 ml-4 list-disc space-y-0.5">
                        <li><code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">app_mentions:read</code></li>
                        <li><code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">channels:history</code>, <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">groups:history</code></li>
                        <li><code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">channels:read</code>, <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">groups:read</code> (channel information)</li>
                        <li><code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">chat:write</code>, <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">chat:write.public</code></li>
                        <li><code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">reactions:write</code> (for the hourglass "bot is working" indicator)</li>
                        <li><code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">users:read</code>, <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">users:read.email</code> (required for per-user memory)</li>
                        <li><code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">files:read</code> (optional, for incoming attachments)</li>
                      </ul>
                    </div>
                    <div>
                      <p className="font-semibold">3. Enable Socket Mode &amp; generate App Token</p>
                      <p className="mt-1"><b>Socket Mode</b> (sidebar) → toggle <b>Enable Socket Mode</b> ON. It will prompt you to create an <b>App-Level Token</b> with the <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">connections:write</code> scope. Copy the token — it starts with <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">xapp-</code>. This is your <b>App Token</b> below.</p>
                    </div>
                    <div>
                      <p className="font-semibold">4. Required: Enable Event Subscriptions</p>
                      <p className="mt-1"><b>Event Subscriptions</b> (sidebar) → toggle <b>Enable Events</b> ON. Under <b>Subscribe to bot events</b>, add: <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">app_mention</code>, <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">message.channels</code>, <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">message.groups</code>. All three events are required for mentions and thread replies. Leave Request URL empty after enabling Socket Mode. If Save Changes is disabled and a URL field appears, verify Socket Mode is on in this same app and refresh. Save changes.</p>
                    </div>
                    <div>
                      <p className="font-semibold">5. Install to workspace &amp; copy Bot Token</p>
                      <p className="mt-1"><b>Install App</b> (sidebar) → <b>Install to Workspace</b> → approve. After install, go back to <b>OAuth &amp; Permissions</b> — the <b>Bot User OAuth Token</b> now appears at the top. Copy it — it starts with <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">xoxb-</code>. This is your <b>Bot Token</b> below.</p>
                    </div>
                    <div>
                      <p className="font-semibold">6. Invite the bot &amp; get Channel ID</p>
                      <p className="mt-1">In Slack, invite the bot to a channel: <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">/invite @YourBot</code>. Add its channel ID as a bot route after connecting. Find it in <b>View channel details</b>; it starts with <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">C</code>.</p>
                    </div>
                    <p className="pt-1 italic opacity-80">If you re-add scopes or events later, you must re-install the app for changes to take effect.</p>
                  </div>
                </details>
              </Card>

              <Card className="p-4 bg-amber-50 dark:bg-amber-900/20 border-amber-300 dark:border-amber-700">
                <div className="flex items-start gap-2">
                  <AlertTriangle className="w-4 h-4 text-amber-600 dark:text-amber-400 flex-shrink-0 mt-0.5" />
                  <div>
                    <p className="text-sm font-semibold text-amber-800 dark:text-amber-200">Required: Event Subscriptions</p>
                    <p className="text-xs text-amber-700 dark:text-amber-300 mt-1">
                      Enable Event Subscriptions in your Slack App and subscribe to: <code className="bg-amber-100 dark:bg-amber-800/40 px-1 rounded font-mono">app_mention</code>, <code className="bg-amber-100 dark:bg-amber-800/40 px-1 rounded font-mono">message.channels</code>, <code className="bg-amber-100 dark:bg-amber-800/40 px-1 rounded font-mono">message.groups</code>
                    </p>
                  </div>
                </div>
              </Card>

              <div className="space-y-3">
                <FormSection title={<>Platform default Slack app{canManageSlackDefault ? '' : ' (managed by a platform admin)'}</>}>
                  <SecretField
                    label="Bot Token"
                    required
                    hint="OAuth & Permissions → Bot User OAuth Token (starts with xoxb-)"
                    value={slackConfig.bot_token || ''}
                    onChange={value => setSlackConfig({ ...slackConfig, bot_token: value })}
                    disabled={!canManageSlackDefault}
                    placeholder="xoxb-..."
                    disabledTitle={defaultTitle}
                  />
                  <SecretField
                    label="App Token (Socket Mode)"
                    required
                    hint={<>Basic Information → App-Level Tokens → Generate with <code className="bg-secondary px-1 rounded font-mono">connections:write</code> scope (starts with xapp-)</>}
                    value={slackConfig.app_token || ''}
                    onChange={value => setSlackConfig({ ...slackConfig, app_token: value })}
                    disabled={!canManageSlackDefault}
                    placeholder="xapp-..."
                    disabledTitle={defaultTitle}
                  />
                </FormSection>

                {/* Test Connection */}
                <div className="space-y-1">
                  <Button variant="outline" onClick={handleSlackTest} disabled={!canManageSlackDefault || !slackConfig.enabled || slackTesting || slackSaving || slackLoading} title={defaultTitle} className="w-full flex items-center justify-center gap-2">
                    {slackTesting ? <><Loader2 className="w-4 h-4 animate-spin" />Testing...</> : 'Test Connection'}
                  </Button>
                  <p className="text-xs text-muted-foreground text-center">
                    Saves your current settings, then tests the connection.
                  </p>
                </div>

                {testResult && <SlackChecksView result={testResult} />}
                {testResult?.success && pollingForReply && !testReply && (
                  <div className="p-3 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg flex items-center gap-2">
                    <Loader2 className="w-4 h-4 animate-spin text-blue-600 dark:text-blue-400" />
                    <p className="text-sm text-blue-800 dark:text-blue-200">Waiting for reply in Slack thread...</p>
                  </div>
                )}
                {testReply && (
                  <div className="p-3 bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-lg">
                    <p className="text-sm font-medium text-green-800 dark:text-green-200">Reply received: {testReply}</p>
                  </div>
                )}
              </div>
            </>
          )}

          <div className="flex items-center justify-end gap-2">
            <Button onClick={handleSlackSave} disabled={!canManageSlackDefault || !slackHasChanges || slackSaving || slackTesting || slackLoading} title={defaultTitle} className="flex items-center gap-2">
              {slackSaving ? <><Loader2 className="w-4 h-4 animate-spin" />Saving...</> : <><CheckCircle className="w-4 h-4" />Save platform settings</>}
            </Button>
          </div>
        </>
      )}
    </div>
  )
}
