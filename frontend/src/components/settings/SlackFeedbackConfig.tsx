import { useState, useEffect } from 'react'
import { X, Settings, CheckCircle, AlertCircle, Loader2, Eye, EyeOff, MessageSquare } from 'lucide-react'
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
                      Send human feedback requests to Slack channel
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

                  {/* OAuth Scopes Required */}
                  <Card className="p-4 bg-secondary/50 border-border">
                    <div className="flex items-start gap-3">
                      <AlertCircle className="w-5 h-5 text-muted-foreground flex-shrink-0 mt-0.5" />
                      <div className="flex-1 text-sm text-foreground">
                        <p className="font-medium mb-2">Required OAuth Scopes (Bot Token):</p>
                        <p className="text-xs mb-2 text-muted-foreground">Go to <strong>OAuth & Permissions</strong> → <strong>Scopes</strong> → <strong>Bot Token Scopes</strong> and add:</p>
                        <ul className="list-disc list-inside space-y-1 text-xs text-muted-foreground">
                          <li><code className="bg-secondary px-1.5 py-0.5 rounded font-mono text-foreground">chat:write</code> - Required to send messages to channels</li>
                          <li><code className="bg-secondary px-1.5 py-0.5 rounded font-mono text-foreground">channels:read</code> - Required to read channel information</li>
                          <li><code className="bg-secondary px-1.5 py-0.5 rounded font-mono text-foreground">channels:history</code> - <strong>Required to receive messages in public channels</strong></li>
                          <li><code className="bg-secondary px-1.5 py-0.5 rounded font-mono text-foreground">groups:history</code> - Required to receive messages in private channels (if using private channels)</li>
                        </ul>
                        <p className="text-xs mt-2 italic text-muted-foreground">⚠️ <strong>Important:</strong> After adding scopes, you must reinstall the app to your workspace for changes to take effect. Go to <strong>OAuth & Permissions</strong> → Click <strong>"Reinstall to Workspace"</strong></p>
                      </div>
                    </div>
                  </Card>

                  {/* Socket Mode Permissions */}
                  <Card className="p-4 bg-info/10 border-info/30">
                    <div className="flex items-start gap-3">
                      <AlertCircle className="w-5 h-5 text-info flex-shrink-0 mt-0.5" />
                      <div className="flex-1 text-sm text-foreground">
                        <p className="font-medium mb-2 text-foreground">ℹ️ Socket Mode Permissions (App-Level Token):</p>
                        <p className="text-xs mb-2 text-muted-foreground">Socket Mode requires an <strong>App-Level Token</strong> (different from Bot Token):</p>
                        <ol className="list-decimal list-inside space-y-1 text-xs text-muted-foreground">
                          <li>Go to <strong>Basic Information</strong> → <strong>App-Level Tokens</strong></li>
                          <li>Click <strong>"Generate Token and Scopes"</strong></li>
                          <li>Token name: e.g., "Socket Mode Token"</li>
                          <li>Add scope: <code className="bg-secondary px-1.5 py-0.5 rounded font-mono text-foreground">connections:write</code> (required for Socket Mode)</li>
                          <li>Click <strong>"Generate"</strong> and copy the token (starts with <code className="bg-secondary px-1.5 py-0.5 rounded font-mono text-foreground">xapp-</code>)</li>
                        </ol>
                        <p className="text-xs mt-2 text-muted-foreground">⚠️ <strong>Note:</strong> App-Level Tokens are different from Bot Tokens. Socket Mode requires both - Bot Token for API calls and App-Level Token for WebSocket connection.</p>
                      </div>
                    </div>
                  </Card>

                  {/* Channel Permissions */}
                  <Card className="p-4 bg-warning/10 border-warning/30">
                    <div className="flex items-start gap-3">
                      <AlertCircle className="w-5 h-5 text-warning flex-shrink-0 mt-0.5" />
                      <div className="flex-1 text-sm text-foreground">
                        <p className="font-medium mb-2 text-foreground">⚠️ Channel Permissions (Required for Receiving Messages):</p>
                        <p className="text-xs mb-2 text-muted-foreground">The bot must be a member of the channel to receive messages:</p>
                        <ol className="list-decimal list-inside space-y-1 text-xs text-muted-foreground">
                          <li>Open your Slack channel</li>
                          <li>Type <code className="bg-secondary px-1.5 py-0.5 rounded font-mono text-foreground">/invite @YourBotName</code> in the channel</li>
                          <li>Or: Right-click channel name → <strong>Integrations</strong> → <strong>Add apps</strong> → Select your bot</li>
                        </ol>
                        <p className="text-xs mt-2 font-medium text-foreground">If the bot is not in the channel, it cannot receive thread replies!</p>
                      </div>
                    </div>
                  </Card>

                  {/* Help Text */}
                  <Card className="p-4 bg-secondary/50 border-border">
                    <div className="flex items-start gap-3">
                      <AlertCircle className="w-5 h-5 text-muted-foreground flex-shrink-0 mt-0.5" />
                      <div className="flex-1 text-sm text-foreground">
                        <p className="font-medium mb-2">Setup Instructions:</p>
                        <ol className="list-decimal list-inside space-y-1 text-xs text-muted-foreground">
                          <li>Create a Slack App at https://api.slack.com/apps</li>
                          <li>Add required OAuth scopes (see above) - <strong>Make sure to add <code className="bg-secondary px-1 rounded text-foreground">channels:history</code>!</strong></li>
                          <li>Install app to workspace: <strong>OAuth & Permissions</strong> → <strong>Install to Workspace</strong></li>
                          <li>Enable Socket Mode: Go to <strong>Socket Mode</strong> → Enable Socket Mode</li>
                          <li>Create App-Level Token: <strong>Basic Information</strong> → <strong>App-Level Tokens</strong> → Generate Token with <code className="bg-secondary px-1 rounded text-foreground">connections:write</code> scope (required for Socket Mode)</li>
                          <li>Enable Events API: <strong>Event Subscriptions</strong> → Enable Events → Subscribe to <code className="bg-secondary px-1 rounded text-foreground">message.channels</code> under <strong>Subscribe to bot events</strong></li>
                          <li><strong>Invite bot to channel:</strong> In your Slack channel, type <code className="bg-secondary px-1 rounded text-foreground">/invite @YourBotName</code></li>
                          <li>Copy Bot Token, App Token, and Channel ID to the fields above</li>
                        </ol>
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

