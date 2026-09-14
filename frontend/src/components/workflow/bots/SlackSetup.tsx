import { AlertCircle, AlertTriangle, CheckCircle, Eye, EyeOff, Loader2 } from 'lucide-react'
import { Button } from '../../ui/Button'
import { Card } from '../../ui/Card'
import { READ_ONLY_TITLE } from '../../../hooks/useCanWriteWorkflow'
import type { WorkflowBots } from './useWorkflowBots'
import { StatusBanner } from './StatusBanner'

const toggleClass = "w-11 h-6 bg-gray-200 peer-focus:outline-none rounded-full peer dark:bg-gray-700 peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all dark:border-gray-600 peer-checked:bg-blue-600"

// ── Drill-in: Slack setup ─────────────────────────────────────────────────

type SlackSetupBots = Pick<WorkflowBots,
  | 'readOnly'
  | 'slackConfig' | 'setSlackConfig' | 'slackLoading' | 'slackSaving' | 'slackTesting' | 'slackError' | 'slackSuccess'
  | 'testResult' | 'testReply' | 'pollingForReply' | 'showBotToken' | 'setShowBotToken' | 'showAppToken' | 'setShowAppToken'
  | 'handleSlackSave' | 'handleSlackTest' | 'slackHasChanges'
>

export function SlackSetup({ bots }: { bots: SlackSetupBots }) {
  const {
    readOnly,
    slackConfig, setSlackConfig, slackLoading, slackSaving, slackTesting, slackError, slackSuccess,
    testResult, testReply, pollingForReply, showBotToken, setShowBotToken, showAppToken, setShowAppToken,
    handleSlackSave, handleSlackTest, slackHasChanges,
  } = bots

  return (
    <div className="space-y-4">
      {slackLoading ? (
        <div className="flex items-center justify-center py-12"><Loader2 className="w-8 h-8 animate-spin text-primary" /></div>
      ) : (
        <>
          {slackError && <StatusBanner tone="error">{slackError}</StatusBanner>}
          {slackSuccess && <StatusBanner tone="success">{slackSuccess}</StatusBanner>}

          {/* Enable Slack */}
          <Card className="p-4">
            <div className="flex items-center justify-between">
              <div>
                <h3 className="text-sm font-medium text-foreground">Enable Slack app</h3>
                <p className="text-xs text-muted-foreground mt-0.5">Connect the two-way Slack bot for @mentions, threads, and human replies</p>
              </div>
              <label className="relative inline-flex items-center cursor-pointer">
                <input type="checkbox" checked={slackConfig.enabled} disabled={readOnly} onChange={e => setSlackConfig({ ...slackConfig, enabled: e.target.checked })} className="sr-only peer" />
                <div className={toggleClass}></div>
              </label>
            </div>
          </Card>

          {slackConfig.enabled && (
            <>
              {/* Bot Mode */}
              <Card className="p-4">
                <div className="flex items-center justify-between">
                  <div>
                    <h3 className="text-sm font-medium text-foreground">Bot Mode (@mention)</h3>
                    <p className="text-xs text-muted-foreground mt-0.5">Users can @mention the bot to start agent sessions directly from Slack. Required for workflow slug routing.</p>
                  </div>
                  <label className="relative inline-flex items-center cursor-pointer">
                    <input type="checkbox" checked={slackConfig.bot_mode || false} disabled={readOnly} onChange={e => setSlackConfig({ ...slackConfig, bot_mode: e.target.checked })} className="sr-only peer" />
                    <div className={toggleClass}></div>
                  </label>
                </div>
              </Card>

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
                        <li><code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">chat:write</code>, <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">chat:write.public</code></li>
                        <li><code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">reactions:write</code> (for the hourglass "bot is working" indicator)</li>
                        <li><code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">users:read</code>, <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">users:read.email</code> (required for per-user memory)</li>
                        <li><code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">files:read</code>, <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">files:write</code> (optional, for attachments)</li>
                      </ul>
                    </div>
                    <div>
                      <p className="font-semibold">3. Enable Socket Mode &amp; generate App Token</p>
                      <p className="mt-1"><b>Socket Mode</b> (sidebar) → toggle <b>Enable Socket Mode</b> ON. It will prompt you to create an <b>App-Level Token</b> with the <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">connections:write</code> scope. Copy the token — it starts with <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">xapp-</code>. This is your <b>App Token</b> below.</p>
                    </div>
                    <div>
                      <p className="font-semibold">4. Enable Event Subscriptions</p>
                      <p className="mt-1"><b>Event Subscriptions</b> (sidebar) → toggle <b>Enable Events</b> ON. Under <b>Subscribe to bot events</b>, add: <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">app_mention</code>, <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">message.channels</code>, <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">message.groups</code>. Save changes.</p>
                    </div>
                    <div>
                      <p className="font-semibold">5. Install to workspace &amp; copy Bot Token</p>
                      <p className="mt-1"><b>Install App</b> (sidebar) → <b>Install to Workspace</b> → approve. After install, go back to <b>OAuth &amp; Permissions</b> — the <b>Bot User OAuth Token</b> now appears at the top. Copy it — it starts with <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">xoxb-</code>. This is your <b>Bot Token</b> below.</p>
                    </div>
                    <div>
                      <p className="font-semibold">6. Invite the bot &amp; get Channel ID</p>
                      <p className="mt-1">In Slack, invite the bot to a channel: <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">/invite @YourBot</code>. Then right-click the channel → <b>View channel details</b> → scroll to the bottom — the <b>Channel ID</b> starts with <code className="bg-blue-100 dark:bg-blue-800/40 px-1 rounded font-mono">C</code>.</p>
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
                {/* Bot Token */}
                <Card className="p-4">
                  <label className="block text-sm font-medium text-foreground mb-2">Bot Token <span className="text-red-500">*</span></label>
                  <div className="relative">
                    <input type={showBotToken ? 'text' : 'password'} value={slackConfig.bot_token || ''} onChange={e => setSlackConfig({ ...slackConfig, bot_token: e.target.value })} disabled={readOnly} placeholder="xoxb-..." className="w-full px-3 py-2 pr-10 border border-border rounded-md bg-background text-foreground focus:outline-none focus:ring-2 focus:ring-primary" />
                    <button type="button" onClick={() => setShowBotToken(!showBotToken)} className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground">
                      {showBotToken ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                    </button>
                  </div>
                  <p className="text-xs text-muted-foreground mt-1">OAuth & Permissions → Bot User OAuth Token (starts with xoxb-)</p>
                </Card>

                {/* Channel ID */}
                <Card className="p-4">
                  <label className="block text-sm font-medium text-foreground mb-2">Channel ID <span className="text-red-500">*</span></label>
                  <input type="text" value={slackConfig.channel_id || ''} onChange={e => setSlackConfig({ ...slackConfig, channel_id: e.target.value })} disabled={readOnly} placeholder="C1234567890" className="w-full px-3 py-2 border border-border rounded-md bg-background text-foreground focus:outline-none focus:ring-2 focus:ring-primary" />
                  <p className="text-xs text-muted-foreground mt-1">Right-click channel → View channel details → Channel ID (starts with C)</p>
                </Card>

                {/* App Token */}
                <Card className="p-4">
                  <label className="block text-sm font-medium text-foreground mb-2">App Token (Socket Mode) <span className="text-red-500">*</span></label>
                  <div className="relative">
                    <input type={showAppToken ? 'text' : 'password'} value={slackConfig.app_token || ''} onChange={e => setSlackConfig({ ...slackConfig, app_token: e.target.value })} disabled={readOnly} placeholder="xapp-..." className="w-full px-3 py-2 pr-10 border border-border rounded-md bg-background text-foreground focus:outline-none focus:ring-2 focus:ring-primary" />
                    <button type="button" onClick={() => setShowAppToken(!showAppToken)} className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground">
                      {showAppToken ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                    </button>
                  </div>
                  <p className="text-xs text-muted-foreground mt-1">Basic Information → App-Level Tokens → Generate with <code className="bg-secondary px-1 rounded font-mono">connections:write</code> scope (starts with xapp-)</p>
                </Card>

                {/* Test Connection */}
                <div className="space-y-1">
                  <Button variant="outline" onClick={handleSlackTest} disabled={readOnly || !slackConfig.enabled || slackTesting || slackLoading} title={readOnly ? READ_ONLY_TITLE : undefined} className="w-full flex items-center justify-center gap-2">
                    {slackTesting ? <><Loader2 className="w-4 h-4 animate-spin" />Testing...</> : 'Test Connection'}
                  </Button>
                  <p className="text-xs text-muted-foreground text-center">
                    Tests the <b>saved</b> config from the workspace — click <b>Save</b> first if you've edited any field above.
                  </p>
                </div>

                {testResult && (
                  <div className={`p-3 border rounded-lg flex items-start gap-2 ${testResult.success ? 'bg-green-50 dark:bg-green-900/20 border-green-200 dark:border-green-800' : 'bg-red-50 dark:bg-red-900/20 border-red-200 dark:border-red-800'}`}>
                    {testResult.success ? <CheckCircle className="w-4 h-4 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" /> : <AlertCircle className="w-4 h-4 text-red-600 dark:text-red-400 flex-shrink-0 mt-0.5" />}
                    <p className={`text-sm ${testResult.success ? 'text-green-700 dark:text-green-300' : 'text-red-700 dark:text-red-300'}`}>{testResult.message}</p>
                  </div>
                )}
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

          <div className="flex items-center justify-end gap-2 border-t border-border pt-3">
            <Button onClick={handleSlackSave} disabled={readOnly || !slackHasChanges || slackSaving || slackLoading} title={readOnly ? READ_ONLY_TITLE : undefined} className="flex items-center gap-2">
              {slackSaving ? <><Loader2 className="w-4 h-4 animate-spin" />Saving...</> : <><CheckCircle className="w-4 h-4" />Save</>}
            </Button>
          </div>
        </>
      )}
    </div>
  )
}
