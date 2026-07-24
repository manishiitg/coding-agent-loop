import { useEffect, useMemo, useState } from 'react'
import { AlertTriangle, Check, ChevronDown, ChevronRight, Loader2, ShieldCheck } from 'lucide-react'
import { Button } from '../ui/Button'
import {
  cliSecurityService,
  type CLISecurityConfig,
  type CLISecurityMode,
  type CLISecurityStatus,
} from '../../services/cli-security-api'

const modes: Array<{ id: CLISecurityMode; title: string; description: string }> = [
  {
    id: 'compatibility',
    title: 'Compatibility',
    description: 'Use your normal host CLI setup. Existing workflows keep working, with best-effort folder guards.',
  },
  {
    id: 'isolated',
    title: 'Isolated Environment',
    description: 'Run supported CLIs with an AgentWorks-managed private home instead of your real home files.',
  },
  {
    id: 'verified',
    title: 'Verified Provider',
    description: 'Expose only a tested provider baseline and the assigned workspace. Recommended for certified providers.',
  },
]

export function CLISecuritySection() {
  const [status, setStatus] = useState<CLISecurityStatus | null>(null)
  const [draft, setDraft] = useState<CLISecurityConfig | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [saved, setSaved] = useState(false)
  const [technicalOpen, setTechnicalOpen] = useState<Record<string, boolean>>({})

  useEffect(() => {
    let active = true
    cliSecurityService.get()
      .then((next) => {
        if (!active) return
        setStatus(next)
        setDraft({
          ...next.config,
          approved_profiles: { ...(next.config.approved_profiles || {}) },
        })
      })
      .catch((cause) => {
        if (active) setError(cause?.response?.data || cause?.message || 'Failed to load CLI security settings')
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => { active = false }
  }, [])

  const certifiedProfiles = useMemo(
    () => status?.profiles.filter((profile) => profile.certified) || [],
    [status],
  )

  const setMode = (mode: CLISecurityMode) => {
    setSaved(false)
    setError('')
    setDraft((current) => current ? { ...current, mode } : current)
  }

  const toggleCapability = (provider: string, version: string, capability: string) => {
    setSaved(false)
    setDraft((current) => {
      if (!current) return current
      const existing = current.approved_profiles[provider]
      const capabilities = new Set(existing?.capabilities || [])
      if (capabilities.has(capability)) capabilities.delete(capability)
      else capabilities.add(capability)
      const approved_profiles = { ...current.approved_profiles }
      if (capabilities.size === 0) {
        delete approved_profiles[provider]
      } else {
        approved_profiles[provider] = {
          profile_version: version,
          capabilities: Array.from(capabilities),
        }
      }
      return { ...current, approved_profiles }
    })
  }

  const save = async () => {
    if (!draft) return
    setSaving(true)
    setSaved(false)
    setError('')
    try {
      const next = await cliSecurityService.update(draft)
      setStatus(next)
      setDraft(next.config)
      setSaved(true)
    } catch (cause: any) {
      setError(String(cause?.response?.data || cause?.message || 'Failed to save CLI security settings').trim())
    } finally {
      setSaving(false)
    }
  }

  if (loading) {
    return <div className="flex min-h-[280px] items-center justify-center text-muted-foreground"><Loader2 className="mr-2 h-4 w-4 animate-spin" />Loading security modes…</div>
  }
  if (!status || !draft) {
    return <div className="rounded-md border border-destructive/30 bg-destructive/10 p-4 text-sm text-destructive">{error || 'CLI security settings are unavailable.'}</div>
  }

  return (
    <div className="space-y-6">
      <div>
        <div className="flex items-center gap-2">
          <ShieldCheck className="h-5 w-5 text-primary" />
          <h3 className="text-lg font-semibold text-foreground">CLI filesystem security</h3>
        </div>
        <p className="mt-1 text-sm text-muted-foreground">
          CLI access remains available in every mode. This controls which host files the CLI process can access.
        </p>
      </div>

      <div className="grid gap-3">
        {modes.map((mode) => (
          <button
            key={mode.id}
            type="button"
            onClick={() => setMode(mode.id)}
            className={`rounded-lg border p-4 text-left transition-colors ${
              draft.mode === mode.id ? 'border-primary bg-primary/5' : 'border-border hover:bg-muted/40'
            }`}
          >
            <div className="flex items-start gap-3">
              <span className={`mt-0.5 flex h-5 w-5 items-center justify-center rounded-full border ${
                draft.mode === mode.id ? 'border-primary bg-primary text-primary-foreground' : 'border-muted-foreground/40'
              }`}>
                {draft.mode === mode.id && <Check className="h-3 w-3" />}
              </span>
              <span>
                <span className="block font-medium text-foreground">{mode.title}</span>
                <span className="mt-1 block text-sm text-muted-foreground">{mode.description}</span>
              </span>
            </div>
          </button>
        ))}
      </div>

      {draft.mode === 'verified' && (
        <div className="space-y-3 rounded-lg border border-border bg-muted/20 p-4">
          <div>
            <h4 className="font-medium text-foreground">Approve certified providers</h4>
            <p className="text-sm text-muted-foreground">Approvals are versioned. A changed provider profile must be approved again.</p>
          </div>
          {certifiedProfiles.map((profile) => (
            <div key={profile.provider} className="rounded-md border border-border bg-background p-3">
              <div className="flex items-center justify-between">
                <div>
                  <div className="font-medium text-foreground">{profile.display_name}</div>
                  <div className="text-xs text-muted-foreground">Certified profile v{profile.version}</div>
                </div>
                <span className="rounded-full bg-emerald-500/15 px-2 py-1 text-xs text-emerald-600 dark:text-emerald-400">Certified</span>
              </div>
              <div className="mt-3 space-y-2">
                {profile.capabilities.map((capability) => {
                  const checked = draft.approved_profiles[profile.provider]?.capabilities.includes(capability.id) || false
                  return (
                    <label key={capability.id} className="flex cursor-pointer items-start gap-3 rounded-md p-2 hover:bg-muted/50">
                      <input
                        type="checkbox"
                        checked={checked}
                        onChange={() => toggleCapability(profile.provider, profile.version, capability.id)}
                        className="mt-1"
                      />
                      <span>
                        <span className="block text-sm font-medium text-foreground">{capability.label}</span>
                        <span className="block text-xs text-muted-foreground">{capability.reason}</span>
                      </span>
                    </label>
                  )
                })}
              </div>
              <button
                type="button"
                onClick={() => setTechnicalOpen((current) => ({ ...current, [profile.provider]: !current[profile.provider] }))}
                className="mt-2 flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
              >
                {technicalOpen[profile.provider] ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
                Technical details
              </button>
              {technicalOpen[profile.provider] && (
                <div className="mt-2 rounded bg-muted p-2 font-mono text-[11px] text-muted-foreground">
                  Executable: {profile.executables.join(', ')}
                  {profile.capabilities.flatMap((capability) => capability.read_path_templates || []).map((path) => (
                    <div key={`read-${path}`}>Read: {path}</div>
                  ))}
                  {profile.capabilities.flatMap((capability) => capability.write_path_templates || []).map((path) => (
                    <div key={`write-${path}`}>Write: {path}</div>
                  ))}
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      {draft.mode !== 'compatibility' && (
        <div className="flex gap-2 rounded-md border border-amber-500/30 bg-amber-500/10 p-3 text-sm text-amber-700 dark:text-amber-300">
          <AlertTriangle className="mt-0.5 h-4 w-4 flex-shrink-0" />
          Unsupported providers and unapproved scheduled workflows fail preflight. AgentWorks never silently falls back to Compatibility.
        </div>
      )}

      {error && <div className="rounded-md border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive">{error}</div>}
      <div className="flex items-center justify-end gap-3">
        {saved && <span className="text-sm text-emerald-600 dark:text-emerald-400">Security mode saved</span>}
        <Button onClick={save} disabled={saving}>
          {saving && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
          Apply security mode
        </Button>
      </div>
    </div>
  )
}
