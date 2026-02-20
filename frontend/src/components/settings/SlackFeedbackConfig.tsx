import { useState, useEffect } from 'react'
import { X, Settings, CheckCircle, AlertCircle, Loader2, Eye, EyeOff, MessageSquare, AlertTriangle } from 'lucide-react'
import { Button } from '../ui/Button'
import { Card } from '../ui/Card'
import { agentApi } from '../../services/api'
import type { SlackConfig, SlackConfigRequest, SlackTestResponse } from '../../services/api-types'

interface SlackFeedbackConfigProps {
  isOpen: boolean
  onClose: () => void
}

export default function SlackFeedbackConfig({ isOpen, onClose }: SlackFeedbackConfigProps) {
  const [config, setConfig] = useState<SlackConfig>({
    enabled: false,
    bot_token: '',
    app_token: '',
    channel_id: '',
  })
  const [originalConfig, setOriginalConfig] = useState<SlackConfig>(config)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState<string | null>(null)
  const [testResult, setTestResult] = useState<SlackTestResponse | null>(null)
  const [testReply, setTestReply] = useState<string | null>(null)
  const [pollingForReply, setPollingForReply] = useState(false)
  const [showBotToken, setShowBotToken] = useState(false)
  const [showAppToken, setShowAppToken] = useState(false)

  // Load configuration when modal opens
  useEffect(() => {
    if (isOpen) {
      loadConfig()
    }
  }, [isOpen])

  const loadConfig = async () => {
    try {
      setLoading(true)
      setError(null)
      const data = await agentApi.getSlackFeedbackConfig()
      setConfig(data)
      setOriginalConfig(data)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load Slack configuration')
    } finally {
      setLoading(false)
    }
  }

  const handleSave = async () => {
    try {
      setSaving(true)
      setError(null)
      setSuccess(null)

      const request: SlackConfigRequest = {
        enabled: config.enabled,
        bot_token: config.bot_token || '',
        app_token: config.app_token || '',
        channel_id: config.channel_id || '',
        bot_mode: config.bot_mode || false,
      }

      await agentApi.updateSlackFeedbackConfig(request)
      setSuccess('Slack configuration saved successfully!')
      await loadConfig() // Reload to get updated config
      
      // Clear success message after 3 seconds
      setTimeout(() => setSuccess(null), 3000)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save Slack configuration')
    } finally {
      setSaving(false)
    }
  }

  const handleTest = async () => {
    try {
      setTesting(true)
      setError(null)
      setTestResult(null)
      setTestReply(null)
      setPollingForReply(false)

      // Test with current form values (without saving)
      const testConfig: SlackConfigRequest = {
        enabled: config.enabled,
        bot_token: config.bot_token || '',
        app_token: config.app_token || '',
        channel_id: config.channel_id || '',
        bot_mode: config.bot_mode || false,
      }

      const result = await agentApi.testSlackConnection(testConfig)
      setTestResult(result)

      // If test succeeded and we have a test_id, start polling for replies
      if (result.success && result.test_id) {
        setPollingForReply(true)
        pollForTestReply(result.test_id)
      }
    } catch (err) {
      setTestResult({
        success: false,
        message: err instanceof Error ? err.message : 'Connection test failed',
      })
    } finally {
      setTesting(false)
    }
  }

  const pollForTestReply = async (testId: string) => {
    const maxAttempts = 60 // Poll for up to 60 seconds (1 second intervals)
    let attempts = 0

    const poll = async () => {
      if (attempts >= maxAttempts) {
        setPollingForReply(false)
        return
      }

      try {
        const reply = await agentApi.getTestConnectionReply(testId)
        console.log(`[SLACK_TEST] Poll attempt ${attempts + 1}:`, reply)
        if (reply && reply.received) {
          console.log(`[SLACK_TEST] ✅ Reply received:`, reply.reply)
          setTestReply(reply.reply)
          setPollingForReply(false)
          return
        }
      } catch (err) {
        // 204 is expected when no reply yet, don't log as error
        if (err && typeof err === 'object' && 'response' in err) {
          const axiosError = err as { response?: { status?: number } }
          if (axiosError.response?.status === 204) {
            // No reply yet, continue polling
            console.log(`[SLACK_TEST] Poll attempt ${attempts + 1}: No reply yet (204)`)
          } else {
            console.error('[SLACK_TEST] Error polling for test reply:', err)
          }
        } else {
          console.error('[SLACK_TEST] Error polling for test reply:', err)
        }
      }

      attempts++
      setTimeout(poll, 1000) // Poll every second
    }

    poll()
  }

  const hasChanges = JSON.stringify(config) !== JSON.stringify(originalConfig)

  if (!isOpen) return null

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-2 sm:p-4">
      <div className="bg-background border border-border rounded-lg shadow-xl w-full max-w-2xl max-h-[90vh] flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between p-6 border-b border-border flex-shrink-0">
          <div className="flex items-center gap-3">
            <MessageSquare className="w-6 h-6 text-primary" />
            <h2 className="text-xl font-semibold text-foreground">Slack Feedback Configuration</h2>
          </div>
          <Button
            variant="ghost"
            size="sm"
            onClick={onClose}
            className="h-8 w-8 p-0 hover:bg-secondary"
          >
            <X className="w-4 h-4" />
          </Button>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-y-auto p-6 space-y-6">
          {loading ? (
            <div className="flex items-center justify-center py-12">
              <Loader2 className="w-8 h-8 animate-spin text-primary" />
            </div>
          ) : (
            <>
              {/* Status Messages */}
              {error && (
                <div className="p-4 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg flex items-start gap-3">
                  <AlertCircle className="w-5 h-5 text-red-600 dark:text-red-400 flex-shrink-0 mt-0.5" />
                  <div className="flex-1">
                    <p className="text-sm font-medium text-red-800 dark:text-red-200">Error</p>
                    <p className="text-sm text-red-700 dark:text-red-300 mt-1">{error}</p>
                  </div>
                </div>
              )}

              {success && (
                <div className="p-4 bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-lg flex items-start gap-3">
                  <CheckCircle className="w-5 h-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                  <div className="flex-1">
                    <p className="text-sm font-medium text-green-800 dark:text-green-200">Success</p>
                    <p className="text-sm text-green-700 dark:text-green-300 mt-1">{success}</p>
                  </div>
                </div>
              )}


              {/* Enable/Disable Toggle */}
              <Card className="p-4">
                <div className="flex items-center justify-between">
                  <div>
                    <h3 className="text-sm font-medium text-foreground">Enable Slack Notifications</h3>
                    <p className="text-xs text-muted-foreground mt-1">
                      Send Slack notifications if user doesn't respond within 2 minutes
                    </p>
                  </div>
                  <label className="relative inline-flex items-center cursor-pointer">
                    <input
                      type="checkbox"
                      checked={config.enabled}
                      onChange={(e) => setConfig({ ...config, enabled: e.target.checked })}
                      className="sr-only peer"
                    />
                    <div className="w-11 h-6 bg-gray-200 peer-focus:outline-none peer-focus:ring-4 peer-focus:ring-blue-300 dark:peer-focus:ring-blue-800 rounded-full peer dark:bg-gray-700 peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all dark:border-gray-600 peer-checked:bg-blue-600"></div>
                  </label>
                </div>
              </Card>

              {/* Bot Mode Toggle */}
              {config.enabled && (
                <Card className="p-4">
                  <div className="flex items-center justify-between">
                    <div>
                      <h3 className="text-sm font-medium text-foreground">Bot Mode (@mention)</h3>
                      <p className="text-xs text-muted-foreground mt-1">
                        Users can @mention the bot to start agent sessions directly from Slack
                      </p>
                    </div>
                    <label className="relative inline-flex items-center cursor-pointer">
                      <input
                        type="checkbox"
                        checked={config.bot_mode || false}
                        onChange={(e) => setConfig({ ...config, bot_mode: e.target.checked })}
                        className="sr-only peer"
                      />
                      <div className="w-11 h-6 bg-gray-200 peer-focus:outline-none peer-focus:ring-4 peer-focus:ring-blue-300 dark:peer-focus:ring-blue-800 rounded-full peer dark:bg-gray-700 peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all dark:border-gray-600 peer-checked:bg-blue-600"></div>
                    </label>
                  </div>
                </Card>
              )}

              {/* Critical Setup Reminder */}
              {config.enabled && (
                <Card className="p-4 bg-amber-50 dark:bg-amber-900/20 border-amber-300 dark:border-amber-700">
                  <div className="flex items-start gap-3">
                    <AlertTriangle className="w-5 h-5 text-amber-600 dark:text-amber-400 flex-shrink-0 mt-0.5" />
                    <div className="flex-1">
                      <p className="text-sm font-semibold text-amber-800 dark:text-amber-200">
                        Required: Event Subscriptions
                      </p>
                      <p className="text-xs text-amber-700 dark:text-amber-300 mt-1">
                        Your Slack App <strong>must</strong> have Event Subscriptions enabled or the bot won't receive any messages. Go to{' '}
                        <a href="https://api.slack.com/apps" target="_blank" rel="noopener noreferrer" className="underline font-medium">api.slack.com/apps</a>
                        {' '}→ your app → <strong>Event Subscriptions</strong> → Enable Events → Subscribe to bot events:
                      </p>
                      <div className="mt-2 flex flex-wrap gap-1.5">
                        <code className="bg-amber-100 dark:bg-amber-800/40 px-1.5 py-0.5 rounded text-xs font-mono text-amber-900 dark:text-amber-200 border border-amber-300 dark:border-amber-700">app_mention</code>
                        <code className="bg-amber-100 dark:bg-amber-800/40 px-1.5 py-0.5 rounded text-xs font-mono text-amber-900 dark:text-amber-200 border border-amber-300 dark:border-amber-700">message.channels</code>
                        <code className="bg-amber-100 dark:bg-amber-800/40 px-1.5 py-0.5 rounded text-xs font-mono text-amber-900 dark:text-amber-200 border border-amber-300 dark:border-amber-700">message.groups</code>
                      </div>
                      <p className="text-xs text-amber-700 dark:text-amber-300 mt-2">
                        After adding events, <strong>reinstall the app</strong> to your workspace. Also invite the bot to the channel: <code className="bg-amber-100 dark:bg-amber-800/40 px-1 py-0.5 rounded font-mono text-amber-900 dark:text-amber-200">/invite @YourBotName</code>
                      </p>
                    </div>
                  </div>
                </Card>
              )}

              {/* Configuration Fields */}
              {config.enabled && (
                <div className="space-y-4">
                  {/* Bot Token */}
                  <Card className="p-4">
                    <label className="block text-sm font-medium text-foreground mb-2">
                      Bot Token
                      <span className="text-red-500 ml-1">*</span>
                    </label>
                    <div className="relative">
                      <input
                        type={showBotToken ? 'text' : 'password'}
                        value={config.bot_token || ''}
                        onChange={(e) => setConfig({ ...config, bot_token: e.target.value })}
                        placeholder="xoxb-..."
                        className="w-full px-3 py-2 pr-10 border border-border rounded-md bg-background text-foreground focus:outline-none focus:ring-2 focus:ring-primary focus:border-transparent"
                      />
                      <button
                        type="button"
                        onClick={() => setShowBotToken(!showBotToken)}
                        className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                      >
                        {showBotToken ? (
                          <EyeOff className="w-4 h-4" />
                        ) : (
                          <Eye className="w-4 h-4" />
                        )}
                      </button>
                    </div>
                    <p className="text-xs text-muted-foreground mt-1">
                      Your Slack bot token (starts with xoxb-). 
                      <span className="block mt-1">
                        <strong>Where to find:</strong> Slack App → OAuth & Permissions → Bot User OAuth Token (after installing to workspace)
                      </span>
                    </p>
                  </Card>

                  {/* Channel ID */}
                  <Card className="p-4">
                    <label className="block text-sm font-medium text-foreground mb-2">
                      Channel ID
                      <span className="text-red-500 ml-1">*</span>
                    </label>
                    <input
                      type="text"
                      value={config.channel_id || ''}
                      onChange={(e) => setConfig({ ...config, channel_id: e.target.value })}
                      placeholder="C1234567890"
                      className="w-full px-3 py-2 border border-border rounded-md bg-background text-foreground focus:outline-none focus:ring-2 focus:ring-primary focus:border-transparent"
                    />
                    <p className="text-xs text-muted-foreground mt-1">
                      The Slack channel ID where notifications will be sent (starts with C).
                      <span className="block mt-1">
                        <strong>Where to find:</strong> Open channel in Slack → Right-click channel name → View channel details → Scroll to find Channel ID
                      </span>
                    </p>
                  </Card>

                  {/* App Token */}
                  <Card className="p-4">
                    <label className="block text-sm font-medium text-foreground mb-2">
                      App Token (Socket Mode)
                      <span className="text-red-500 ml-1">*</span>
                    </label>
                    <div className="relative">
                      <input
                        type={showAppToken ? 'text' : 'password'}
                        value={config.app_token || ''}
                        onChange={(e) => setConfig({ ...config, app_token: e.target.value })}
                        placeholder="xapp-..."
                        className="w-full px-3 py-2 pr-10 border border-border rounded-md bg-background text-foreground focus:outline-none focus:ring-2 focus:ring-primary focus:border-transparent"
                      />
                      <button
                        type="button"
                        onClick={() => setShowAppToken(!showAppToken)}
                        className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                      >
                        {showAppToken ? (
                          <EyeOff className="w-4 h-4" />
                        ) : (
                          <Eye className="w-4 h-4" />
                        )}
                      </button>
                    </div>
                    <p className="text-xs text-muted-foreground mt-1">
                      Your Slack app-level token (starts with xapp-). Required for Socket Mode (real-time WebSocket connection).
                      <span className="block mt-1">
                        <strong>Where to find:</strong> Slack App → Basic Information → App-Level Tokens → Generate Token → Add <code className="bg-secondary px-1.5 py-0.5 rounded font-mono text-foreground">connections:write</code> scope
                      </span>
                    </p>
                  </Card>

                  {/* Step-by-Step Installation Guide */}
                  <Card className="p-4 bg-secondary/50 border-border">
                    <div className="flex items-start gap-3">
                      <Settings className="w-5 h-5 text-primary flex-shrink-0 mt-0.5" />
                      <div className="flex-1 text-sm text-foreground">
                        <p className="font-medium mb-3">Slack Bot Installation Guide</p>

                        <p className="text-xs font-medium text-foreground mb-1">Step 1: Create a Slack App</p>
                        <ol className="list-decimal list-inside space-y-0.5 text-xs text-muted-foreground mb-3 ml-2">
                          <li>Go to <a href="https://api.slack.com/apps" target="_blank" rel="noopener noreferrer" className="text-primary underline">api.slack.com/apps</a></li>
                          <li>Click <strong>"Create New App"</strong> → <strong>"From scratch"</strong></li>
                          <li>Name your app (e.g. "AI Agent") and select your workspace</li>
                        </ol>

                        <p className="text-xs font-medium text-foreground mb-1">Step 2: Enable Socket Mode</p>
                        <ol className="list-decimal list-inside space-y-0.5 text-xs text-muted-foreground mb-3 ml-2">
                          <li>Go to <strong>Socket Mode</strong> (left sidebar) → Toggle <strong>Enable Socket Mode</strong> on</li>
                          <li>When prompted, name the token (e.g. "Socket Mode Token")</li>
                          <li>It auto-adds <code className="bg-secondary px-1 py-0.5 rounded font-mono text-foreground">connections:write</code> scope</li>
                          <li>Click <strong>Generate</strong> → Copy the <code className="bg-secondary px-1 py-0.5 rounded font-mono text-foreground">xapp-...</code> token — this is your <strong>App Token</strong></li>
                        </ol>

                        <p className="text-xs font-medium text-foreground mb-1">Step 3: Add Bot Token Scopes</p>
                        <p className="text-xs text-muted-foreground mb-1 ml-2">Go to <strong>OAuth & Permissions</strong> → <strong>Scopes</strong> → <strong>Bot Token Scopes</strong> → Add:</p>
                        <ul className="list-disc list-inside space-y-0.5 text-xs text-muted-foreground mb-3 ml-4">
                          <li><code className="bg-secondary px-1 py-0.5 rounded font-mono text-foreground">chat:write</code> — send messages</li>
                          <li><code className="bg-secondary px-1 py-0.5 rounded font-mono text-foreground">channels:read</code> — read channel info</li>
                          <li><code className="bg-secondary px-1 py-0.5 rounded font-mono text-foreground">channels:history</code> — receive messages in public channels</li>
                          <li><code className="bg-secondary px-1 py-0.5 rounded font-mono text-foreground">groups:history</code> — receive messages in private channels</li>
                          <li><code className="bg-secondary px-1 py-0.5 rounded font-mono text-foreground">app_mentions:read</code> — receive @mention events (for bot mode)</li>
                          <li><code className="bg-secondary px-1 py-0.5 rounded font-mono text-foreground">users:read</code> — look up user profiles</li>
                          <li><code className="bg-secondary px-1 py-0.5 rounded font-mono text-foreground">users:read.email</code> — resolve user emails (for allowed emails filter)</li>
                        </ul>

                        <p className="text-xs font-medium text-foreground mb-1">Step 4: Install App to Workspace</p>
                        <ol className="list-decimal list-inside space-y-0.5 text-xs text-muted-foreground mb-3 ml-2">
                          <li>Go to <strong>OAuth & Permissions</strong> → Click <strong>"Install to Workspace"</strong></li>
                          <li>Authorize the requested permissions</li>
                          <li>Copy the <code className="bg-secondary px-1 py-0.5 rounded font-mono text-foreground">xoxb-...</code> token — this is your <strong>Bot Token</strong></li>
                        </ol>

                        <p className="text-xs font-medium text-foreground mb-1">Step 5: Subscribe to Events</p>
                        <ol className="list-decimal list-inside space-y-0.5 text-xs text-muted-foreground mb-3 ml-2">
                          <li>Go to <strong>Event Subscriptions</strong> → Toggle <strong>Enable Events</strong> on</li>
                          <li>Under <strong>"Subscribe to bot events"</strong>, add:
                            <ul className="list-disc list-inside ml-4 mt-0.5 space-y-0.5">
                              <li><code className="bg-secondary px-1 py-0.5 rounded font-mono text-foreground">message.channels</code> — messages in public channels</li>
                              <li><code className="bg-secondary px-1 py-0.5 rounded font-mono text-foreground">message.groups</code> — messages in private channels</li>
                              <li><code className="bg-secondary px-1 py-0.5 rounded font-mono text-foreground">app_mention</code> — @mentions of the bot</li>
                            </ul>
                          </li>
                          <li>Click <strong>Save Changes</strong></li>
                        </ol>

                        <p className="text-xs font-medium text-foreground mb-1">Step 6: Invite Bot to Channel</p>
                        <ol className="list-decimal list-inside space-y-0.5 text-xs text-muted-foreground mb-3 ml-2">
                          <li>Open the Slack channel you want to use</li>
                          <li>Type <code className="bg-secondary px-1 py-0.5 rounded font-mono text-foreground">/invite @YourBotName</code></li>
                          <li>Get the Channel ID: right-click channel name → <strong>View channel details</strong> → scroll to find the <strong>Channel ID</strong> (starts with <code className="bg-secondary px-1 py-0.5 rounded font-mono text-foreground">C</code>)</li>
                        </ol>

                        <p className="text-xs font-medium text-foreground mb-1">Step 7: Configure Here</p>
                        <p className="text-xs text-muted-foreground ml-2">
                          Paste your <strong>Bot Token</strong>, <strong>App Token</strong>, and <strong>Channel ID</strong> into the fields above, enable the toggle, and save. Use <strong>Test Connection</strong> to verify everything works.
                        </p>

                        <div className="mt-3 p-2 bg-warning/10 border border-warning/30 rounded text-xs text-muted-foreground">
                          <strong className="text-foreground">Re-install after scope changes:</strong> If you add new scopes later, you must go to <strong>OAuth & Permissions</strong> → <strong>"Reinstall to Workspace"</strong> for changes to take effect.
                        </div>
                      </div>
                    </div>
                  </Card>

                  {/* Test Connection Button */}
                  <div className="pt-2 space-y-3">
                    <Button
                      variant="outline"
                      onClick={handleTest}
                      disabled={!config.enabled || testing || loading}
                      className="w-full flex items-center justify-center gap-2"
                    >
                      {testing ? (
                        <>
                          <Loader2 className="w-4 h-4 animate-spin" />
                          Testing Connection...
                        </>
                      ) : (
                        <>
                          <Settings className="w-4 h-4" />
                          Test Connection
                        </>
                      )}
                    </Button>

                    {/* Test Result */}
                    {testResult && (
                      <div className="space-y-3">
                        <div
                          className={`p-4 border rounded-lg flex items-start gap-3 ${
                            testResult.success
                              ? 'bg-green-50 dark:bg-green-900/20 border-green-200 dark:border-green-800'
                              : 'bg-red-50 dark:bg-red-900/20 border-red-200 dark:border-red-800'
                          }`}
                        >
                          {testResult.success ? (
                            <CheckCircle className="w-5 h-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                          ) : (
                            <AlertCircle className="w-5 h-5 text-red-600 dark:text-red-400 flex-shrink-0 mt-0.5" />
                          )}
                          <div className="flex-1">
                            <p
                              className={`text-sm font-medium ${
                                testResult.success
                                  ? 'text-green-800 dark:text-green-200'
                                  : 'text-red-800 dark:text-red-200'
                              }`}
                            >
                              {testResult.success ? 'Connection Test Successful' : 'Connection Test Failed'}
                            </p>
                            <p
                              className={`text-sm mt-1 ${
                                testResult.success
                                  ? 'text-green-700 dark:text-green-300'
                                  : 'text-red-700 dark:text-red-300'
                              }`}
                            >
                              {testResult.message}
                            </p>
                          </div>
                        </div>

                        {/* Polling indicator */}
                        {testResult.success && pollingForReply && !testReply && (
                          <div className="p-3 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg flex items-center gap-2">
                            <Loader2 className="w-4 h-4 animate-spin text-blue-600 dark:text-blue-400" />
                            <p className="text-sm text-blue-800 dark:text-blue-200">
                              Waiting for reply in Slack thread... (will auto-update when you reply)
                            </p>
                          </div>
                        )}

                        {/* Test Reply Display */}
                        {testReply && (
                          <div className="p-4 bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-lg flex items-start gap-3">
                            <CheckCircle className="w-5 h-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                            <div className="flex-1">
                              <p className="text-sm font-medium text-green-800 dark:text-green-200">
                                ✅ Reply Received!
                              </p>
                              <p className="text-sm mt-1 text-green-700 dark:text-green-300">
                                <strong>Your reply:</strong> {testReply}
                              </p>
                              <p className="text-xs mt-2 text-green-600 dark:text-green-400 italic">
                                Socket Mode is working correctly! Replies are being received in real-time.
                              </p>
                            </div>
                          </div>
                        )}

                        {/* Troubleshooting: No Reply Received */}
                        {testResult && testResult.success && !pollingForReply && !testReply && (
                          <div className="p-4 bg-warning/10 border border-warning/30 rounded-lg">
                            <p className="text-sm font-medium text-foreground mb-2">
                              ⚠️ Not receiving replies? Check these:
                            </p>
                            <ul className="list-disc list-inside space-y-1 text-xs text-muted-foreground">
                              <li>Is the bot invited to the channel? Type <code className="bg-secondary px-1 rounded text-foreground">/invite @YourBotName</code></li>
                              <li>Do you have <code className="bg-secondary px-1 rounded text-foreground">channels:history</code> scope? Check OAuth & Permissions</li>
                              <li>Did you reinstall the app after adding scopes? Go to OAuth & Permissions → Reinstall to Workspace</li>
                              <li>Is <code className="bg-secondary px-1 rounded text-foreground">message.channels</code> subscribed in Event Subscriptions?</li>
                              <li>Are you replying in a <strong>thread</strong> (not as a new message)?</li>
                            </ul>
                          </div>
                        )}
                      </div>
                    )}
                  </div>
                </div>
              )}
            </>
          )}
        </div>

        {/* Footer Actions */}
        <div className="flex items-center justify-end gap-3 p-6 border-t border-border flex-shrink-0">
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            onClick={handleSave}
            disabled={!hasChanges || saving || loading || !config.enabled}
            className="flex items-center gap-2"
          >
            {saving ? (
              <>
                <Loader2 className="w-4 h-4 animate-spin" />
                Saving...
              </>
            ) : (
              <>
                <CheckCircle className="w-4 h-4" />
                Save Configuration
              </>
            )}
          </Button>
        </div>
      </div>
    </div>
  )
}

