import { useEffect, useState } from 'react'
import { X, AlertCircle, CheckCircle, Loader2, Bell } from 'lucide-react'
import { Button } from '../ui/Button'
import { Card } from '../ui/Card'
import { agentApi } from '../../services/api'
import type { NotificationPreference } from '../../services/api-types'

interface NotificationPreferencesModalProps {
  isOpen: boolean
  onClose: () => void
}

const empty: NotificationPreference = {
  slack_channel_id: '',
  slack_disabled: false,
  whatsapp_phone: '',
  whatsapp_disabled: false,
}

export default function NotificationPreferencesModal({ isOpen, onClose }: NotificationPreferencesModalProps) {
  const [pref, setPref] = useState<NotificationPreference>(empty)
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState<string | null>(null)

  useEffect(() => {
    if (!isOpen) return
    const load = async () => {
      try {
        setLoading(true)
        setError(null)
        const data = await agentApi.getNotificationPreferences()
        setPref({ ...empty, ...data })
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load preferences')
      } finally {
        setLoading(false)
      }
    }
    load()
  }, [isOpen])

  const handleSave = async () => {
    try {
      setSaving(true)
      setError(null)
      setSuccess(null)
      await agentApi.updateNotificationPreferences(pref)
      setSuccess('Preferences saved.')
      setTimeout(() => setSuccess(null), 3000)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save preferences')
    } finally {
      setSaving(false)
    }
  }

  if (!isOpen) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
      <div className="bg-background rounded-lg shadow-xl w-full max-w-2xl max-h-[90vh] overflow-hidden flex flex-col border border-border">
        {/* Header */}
        <div className="flex items-center justify-between p-4 border-b border-border">
          <div className="flex items-center gap-2">
            <Bell className="w-5 h-5 text-foreground" />
            <h2 className="text-lg font-semibold text-foreground">My Notification Preferences</h2>
          </div>
          <button
            onClick={onClose}
            className="text-muted-foreground hover:text-foreground transition-colors"
            aria-label="Close"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Body */}
        <div className="p-4 overflow-y-auto space-y-4">
          {loading ? (
            <div className="flex items-center justify-center py-12">
              <Loader2 className="w-6 h-6 animate-spin text-muted-foreground" />
            </div>
          ) : (
            <>
              <p className="text-sm text-muted-foreground">
                When an automation asks you a question and you don't reply in the app within 2 minutes,
                we'll send a notification using these preferences. Leave a field blank to use the
                workspace-wide default.
              </p>

              {error && (
                <Card className="p-3 bg-red-50 dark:bg-red-900/20 border-red-300 dark:border-red-700">
                  <div className="flex items-start gap-2">
                    <AlertCircle className="w-4 h-4 text-red-600 dark:text-red-400 mt-0.5 flex-shrink-0" />
                    <p className="text-sm text-red-700 dark:text-red-300">{error}</p>
                  </div>
                </Card>
              )}
              {success && (
                <Card className="p-3 bg-green-50 dark:bg-green-900/20 border-green-300 dark:border-green-700">
                  <div className="flex items-start gap-2">
                    <CheckCircle className="w-4 h-4 text-green-600 dark:text-green-400 mt-0.5 flex-shrink-0" />
                    <p className="text-sm text-green-700 dark:text-green-300">{success}</p>
                  </div>
                </Card>
              )}

              {/* Slack */}
              <Card className="p-4 space-y-3">
                <div className="flex items-center justify-between">
                  <h3 className="text-sm font-medium text-foreground">Slack channel</h3>
                  <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
                    <input
                      type="checkbox"
                      checked={!!pref.slack_disabled}
                      onChange={(e) => setPref({ ...pref, slack_disabled: e.target.checked })}
                    />
                    Don't notify me on Slack
                  </label>
                </div>
                <input
                  type="text"
                  placeholder="C0123456789 — leave blank to use workspace default"
                  value={pref.slack_channel_id || ''}
                  disabled={!!pref.slack_disabled}
                  onChange={(e) => setPref({ ...pref, slack_channel_id: e.target.value.trim() })}
                  className="w-full px-3 py-2 text-sm bg-secondary border border-border rounded-md font-mono disabled:opacity-50"
                />
                <p className="text-xs text-muted-foreground">
                  Slack channel ID (starts with <code className="font-mono">C</code>). The bot must be a member of this channel.
                </p>
              </Card>

              {/* WhatsApp */}
              <Card className="p-4 space-y-3">
                <div className="flex items-center justify-between">
                  <h3 className="text-sm font-medium text-foreground">WhatsApp number</h3>
                  <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
                    <input
                      type="checkbox"
                      checked={!!pref.whatsapp_disabled}
                      onChange={(e) => setPref({ ...pref, whatsapp_disabled: e.target.checked })}
                    />
                    Don't notify me on WhatsApp
                  </label>
                </div>
                <input
                  type="tel"
                  placeholder="+919000000000 — leave blank to use workspace default"
                  value={pref.whatsapp_phone || ''}
                  disabled={!!pref.whatsapp_disabled}
                  onChange={(e) => setPref({ ...pref, whatsapp_phone: e.target.value.trim() })}
                  className="w-full px-3 py-2 text-sm bg-secondary border border-border rounded-md font-mono disabled:opacity-50"
                />
                <p className="text-xs text-muted-foreground">
                  Phone number in E.164 format (with country code). The bot must be paired and reachable.
                </p>
              </Card>

              <Card className="p-3 bg-blue-50 dark:bg-blue-900/20 border-blue-300 dark:border-blue-700">
                <p className="text-xs text-blue-800 dark:text-blue-200">
                  <strong>Note:</strong> when an automation runs from a Slack or WhatsApp bot conversation,
                  replies always go back to that thread — these preferences only apply to questions
                  that originate from the web/desktop app.
                </p>
              </Card>
            </>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-end gap-2 p-4 border-t border-border">
          <Button onClick={onClose} variant="outline" disabled={saving}>
            Cancel
          </Button>
          <Button onClick={handleSave} disabled={loading || saving}>
            {saving ? <Loader2 className="w-4 h-4 animate-spin" /> : 'Save'}
          </Button>
        </div>
      </div>
    </div>
  )
}
