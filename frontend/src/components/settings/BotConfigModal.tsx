import { useState, useEffect, useCallback } from 'react'
import { X, Check, Settings } from 'lucide-react'
import { agentApi } from '../../services/api'

interface BotConfigModalProps {
  isOpen: boolean
  onClose: () => void
}

export default function BotConfigModal({ isOpen, onClose }: BotConfigModalProps) {
  const [allowedEmails, setAllowedEmails] = useState('')
  const [dirty, setDirty] = useState(false)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)

  const loadData = useCallback(async () => {
    try {
      const cfg = await agentApi.getSimulatorConfig()
      if ((cfg as Record<string, unknown>).allowed_emails) {
        setAllowedEmails(((cfg as Record<string, unknown>).allowed_emails as string[]).join(', '))
      }
    } catch {
      // ignore
    }
  }, [])

  useEffect(() => {
    if (!isOpen) return
    setDirty(false)
    setSaved(false)
    loadData()
  }, [isOpen, loadData])

  const handleSave = async () => {
    setSaving(true)
    try {
      const emails = allowedEmails
        .split(',')
        .map(e => e.trim())
        .filter(e => e.length > 0)

      await agentApi.saveBotConfig({ allowed_emails: emails })
      setDirty(false)
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    } catch {
      // ignore
    } finally {
      setSaving(false)
    }
  }

  if (!isOpen) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div className="bg-background border border-border rounded-lg shadow-xl w-full max-w-sm flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-border">
          <h2 className="text-base font-semibold flex items-center gap-2">
            <Settings className="w-5 h-5 text-primary" />
            Bot Configuration
          </h2>
          <button
            onClick={onClose}
            className="p-1.5 text-muted-foreground hover:text-foreground transition-colors rounded"
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        {/* Content */}
        <div className="px-4 py-4 space-y-4">
          <div className="flex flex-col gap-1">
            <span className="text-xs font-medium text-muted-foreground">Allowed Emails (Slack)</span>
            <input
              type="text"
              value={allowedEmails}
              onChange={e => { setAllowedEmails(e.target.value); setDirty(true); setSaved(false) }}
              placeholder="user@example.com, user2@example.com"
              className="w-full px-2.5 py-1.5 text-xs bg-secondary border border-border rounded focus:outline-none focus:ring-1 focus:ring-primary"
            />
            <span className="text-[10px] text-muted-foreground">Comma-separated. Leave empty to allow everyone.</span>
          </div>

          <p className="text-[10px] text-muted-foreground">
            LLM tiers, servers, and skills are configured via the workspace config/ folder and apply to all bot interfaces automatically.
          </p>
        </div>

        {/* Footer */}
        <div className="px-4 py-3 border-t border-border flex items-center justify-end gap-2">
          <button
            onClick={handleSave}
            disabled={saving || (!dirty && !saved)}
            className={`px-4 py-1.5 text-sm rounded-md transition-colors flex items-center gap-1.5 ${
              saved
                ? 'bg-green-600 text-white'
                : 'bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50'
            }`}
          >
            {saving ? 'Saving...' : saved ? (
              <><Check className="w-3.5 h-3.5" /> Saved</>
            ) : 'Save'}
          </button>
        </div>
      </div>
    </div>
  )
}
