import { useEffect, useState } from 'react'
import { Check, Copy, Download, Globe, Plug, Terminal } from 'lucide-react'
import { SettingsCard } from '../ui/SettingsCard'
import { Button } from '../ui/Button'
import api, { externalSkillApi, getApiBaseUrl } from '../../services/api'

type Destination = 'terminal' | 'local-assistant' | 'hosted-assistant'
type LocalClient = 'claude-code' | 'codex' | 'json-client'
type HostedClient = 'chatgpt' | 'cowork'
interface OAuthConnection { id: string; client_name: string; scopes: string[]; expires_at: string }

function CopyButton({ text, label }: { text: string; label: string }) {
  const [copied, setCopied] = useState(false)
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    } catch { setCopied(false) }
  }
  return <Button variant="ghost" size="icon" className="h-7 w-7 shrink-0" title={copied ? 'Copied' : `Copy: ${label}`} onClick={() => void copy()}>
    {copied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
  </Button>
}

function CommandRow({ command, label }: { command: string; label: string }) {
  return <div className="flex items-center gap-2">
    <code className="min-w-0 flex-1 overflow-x-auto rounded-md border border-border bg-muted/40 px-3 py-2 font-mono text-xs text-foreground" aria-label={label}>{command}</code>
    <CopyButton text={command} label={label} />
  </div>
}

function JsonBlock({ json, label, hint }: { json: string; label: string; hint: string }) {
  return <div className="space-y-1">
    <div className="flex items-center justify-between gap-2"><span className="text-xs font-medium text-foreground">{label}</span><CopyButton text={json} label={label} /></div>
    <pre className="overflow-x-auto rounded-md border border-border bg-muted/40 px-3 py-2 font-mono text-xs text-foreground" aria-label={label}>{json}</pre>
    <p className="text-xs text-muted-foreground">{hint}</p>
  </div>
}

/** One connection path at a time. Browser approval keeps credentials out of setup instructions. */
export function CliMcpSetupPanel() {
  const [destination, setDestination] = useState<Destination>('terminal')
  const [localClient, setLocalClient] = useState<LocalClient>('claude-code')
  const [hostedClient, setHostedClient] = useState<HostedClient>('chatgpt')
  const [connections, setConnections] = useState<OAuthConnection[]>([])
  const [connectionLoadError, setConnectionLoadError] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [skillBusy, setSkillBusy] = useState<'download' | 'copy' | null>(null)
  const [skillMsg, setSkillMsg] = useState<string | null>(null)
  const origin = (getApiBaseUrl() || window.location.origin).replace(/\/+$/, '')
  const quoted = (value: string) => `'${value.replace(/'/g, `'\\''`)}'`
  const installer = `curl -fsSL ${quoted(`${origin}/api/downloads/cli/install-agentworks.sh`)} | sh -s -- --server ${quoted(origin)}`
  const mcpJson = JSON.stringify({ mcpServers: { agentworks: { command: 'agentworks', args: ['mcp', 'serve'], env: { AGENTWORKS_SERVER: origin } } } }, null, 2)
  const isLoopbackOrigin = (() => { try { return ['localhost', '127.0.0.1', '0.0.0.0', '::1'].includes(new URL(origin).hostname) } catch { return false } })()

  useEffect(() => {
    let cancelled = false
    api.get<{ connections: OAuthConnection[] }>('/api/oauth/mcp/connections')
      .then(({ data }) => {
        if (cancelled) return
        if (Array.isArray(data?.connections)) {
          setConnections(data.connections)
          return
        }
        setConnections([])
        setConnectionLoadError(true)
      })
      .catch(() => { /* Setup instructions remain available. */ })
    return () => { cancelled = true }
  }, [])

  const revoke = async (id: string) => {
    setError(null)
    try {
      await api.delete(`/api/oauth/mcp/connections/${encodeURIComponent(id)}`)
      setConnections(current => current.filter(item => item.id !== id))
    } catch (err) { setError(err instanceof Error ? err.message : 'Could not revoke connection') }
  }

  const downloadSkill = async () => {
    setSkillBusy('download'); setSkillMsg(null)
    try {
      const blob = await externalSkillApi.downloadSkillZIP()
      const url = URL.createObjectURL(blob)
      try {
        const anchor = document.createElement('a')
        anchor.href = url; anchor.download = 'agentworks-skill.zip'
        document.body.appendChild(anchor); anchor.click(); anchor.remove()
      } finally { URL.revokeObjectURL(url) }
      setSkillMsg('Downloaded agentworks-skill.zip')
    } catch { setSkillMsg('Download failed — try again.') }
    finally { setSkillBusy(null) }
  }

  const copySkill = async () => {
    setSkillBusy('copy'); setSkillMsg(null)
    try { await navigator.clipboard.writeText(await externalSkillApi.fetchSkillMD()); setSkillMsg('Skill text copied') }
    catch { setSkillMsg('Copy failed — try again.') }
    finally { setSkillBusy(null) }
  }

  const destinations = [
    { id: 'terminal', icon: Terminal, title: 'Terminal or scripts', description: 'Run agentworks commands yourself.' },
    { id: 'local-assistant', icon: Plug, title: 'AI app on this computer', description: 'Claude Code, Codex, or another local MCP client.' },
    { id: 'hosted-assistant', icon: Globe, title: 'Hosted AI app', description: 'ChatGPT or Claude Cowork.' },
  ] as const

  return <div className="space-y-5">
    <div className="space-y-3">
      <div><h3 className="text-base font-semibold text-foreground">Where will you use AgentWorks?</h3>
        <p className="mt-1 text-sm text-muted-foreground">Choose one setup path. Sign in through your browser when prompted.</p></div>
      <div className="grid gap-2" role="group" aria-label="Connection destination">
        {destinations.map(({ id, icon: Icon, title, description }) => <button key={id} type="button" aria-pressed={destination === id} onClick={() => setDestination(id)} className={`flex items-center gap-3 rounded-lg border p-3 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${destination === id ? 'border-primary bg-primary/5' : 'border-border hover:border-primary/50 hover:bg-muted/30'}`}>
          <Icon className="h-4 w-4 shrink-0 text-primary" /><span className="flex min-w-0 flex-col gap-0.5"><span className="text-sm font-medium text-foreground">{title}</span><span className="text-xs text-muted-foreground">{description}</span></span>
        </button>)}
      </div>
    </div>
    <SettingsCard icon={destination === 'terminal' ? <Terminal className="h-4 w-4 text-primary" /> : destination === 'local-assistant' ? <Plug className="h-4 w-4 text-primary" /> : <Globe className="h-4 w-4 text-primary" />}
      title={destination === 'terminal' ? 'Set up the command line' : destination === 'local-assistant' ? 'Connect a local AI app' : 'Connect a hosted AI app'}
      description={destination === 'terminal' ? 'Install the CLI and approve it in your browser.' : destination === 'local-assistant' ? 'Install and sign in to the CLI, then add its MCP bridge.' : 'Use the HTTPS MCP URL in an AI app that connects from the cloud.'}>
      {destination === 'terminal' ? <div className="space-y-2">
        <p className="text-xs font-medium text-foreground">Paste this in your terminal. The installer will open a browser sign-in link.</p>
        <CommandRow label="Install and sign in" command={installer} />
      </div> : destination === 'local-assistant' ? <div className="space-y-4">
        <div className="space-y-2"><p className="text-xs font-medium text-foreground">1. Install and sign in on this computer</p><CommandRow label="Install CLI for local MCP" command={installer} /></div>
        <div className="space-y-2">
          <p className="text-xs font-medium text-foreground">2. Choose your AI app</p>
          <div className="flex flex-wrap gap-2" role="group" aria-label="Local MCP client">
            <Button variant={localClient === 'claude-code' ? 'default' : 'outline'} size="sm" aria-pressed={localClient === 'claude-code'} onClick={() => setLocalClient('claude-code')}>Claude Code</Button>
            <Button variant={localClient === 'codex' ? 'default' : 'outline'} size="sm" aria-pressed={localClient === 'codex'} onClick={() => setLocalClient('codex')}>Codex</Button>
            <Button variant={localClient === 'json-client' ? 'default' : 'outline'} size="sm" aria-pressed={localClient === 'json-client'} onClick={() => setLocalClient('json-client')}>JSON MCP client</Button>
          </div>
          {localClient === 'claude-code' ? <CommandRow label="Add AgentWorks to Claude Code" command={`claude mcp add agentworks -e AGENTWORKS_SERVER=${quoted(origin)} -- agentworks mcp serve`} />
            : localClient === 'codex' ? <CommandRow label="Add AgentWorks to Codex" command={`codex mcp add agentworks --env AGENTWORKS_SERVER=${quoted(origin)} -- agentworks mcp serve`} />
              : <JsonBlock label="MCP client config" json={mcpJson} hint="Paste into a JSON-configured local MCP client. If it cannot find agentworks, use the full path from which agentworks." />}
        </div>
        {localClient === 'claude-code' && <details className="rounded-md border border-border p-3 text-xs text-muted-foreground"><summary className="cursor-pointer font-medium text-foreground">Optional: install the AgentWorks skill</summary><p className="mt-2 mb-2">The skill teaches Claude Code how to use the workflow tools.</p><CommandRow label="Install skill for Claude Code" command="agentworks skills install --dir ~/.claude/skills" /></details>}
      </div> : <div className="space-y-4">
        <div className="flex flex-wrap gap-2" role="group" aria-label="Hosted MCP client">
          <Button variant={hostedClient === 'chatgpt' ? 'default' : 'outline'} size="sm" aria-pressed={hostedClient === 'chatgpt'} onClick={() => setHostedClient('chatgpt')}>ChatGPT</Button>
          <Button variant={hostedClient === 'cowork' ? 'default' : 'outline'} size="sm" aria-pressed={hostedClient === 'cowork'} onClick={() => setHostedClient('cowork')}>Claude Cowork</Button>
        </div>
        <p className="text-xs text-muted-foreground">{hostedClient === 'chatgpt' ? 'In ChatGPT, open Settings → Apps & Connectors → Developer Mode, then add a custom MCP connector.' : 'In Claude Cowork, open Settings → Connectors → Add custom connector.'}</p>
        <CommandRow label="Remote MCP URL" command={`${origin}/api/external/v1/mcp`} />
        {isLoopbackOrigin && <p className="text-xs text-amber-500">Hosted apps need a public server URL; open Connect on that server instead.</p>}
        <p className="text-xs text-muted-foreground">Choose OAuth when the app asks how to authenticate. AgentWorks will open a sign-in and permission screen.</p>
        <details className="rounded-md border border-border p-3 text-xs text-muted-foreground">
          <summary className="cursor-pointer font-medium text-foreground">Optional: give the assistant workflow guidance</summary>
          <p className="mt-2">Upload the skill where Skills are supported, or paste its text into the app&apos;s custom instructions.</p>
          <div className="mt-3 flex flex-wrap gap-2">
            <Button variant="outline" size="sm" disabled={skillBusy !== null} onClick={() => void downloadSkill()}><Download className="mr-1 h-3.5 w-3.5" />Download skill .zip</Button>
            <Button variant="ghost" size="sm" disabled={skillBusy !== null} onClick={() => void copySkill()}><Copy className="mr-1 h-3.5 w-3.5" />Copy skill text</Button>
          </div>
          {skillMsg && <p className="mt-2">{skillMsg}</p>}
        </details>
      </div>}
    </SettingsCard>
    {connectionLoadError && <p role="alert" className="text-sm text-amber-600">Connected apps are unavailable. Restart or update the AgentWorks server to use browser sign-in.</p>}
    {connections.length > 0 && <SettingsCard icon={<Plug className="h-4 w-4 text-primary" />} title="Connected apps" description="Revoke access to a CLI or hosted AI app when you are done.">
      <div className="space-y-2">{connections.map(item => <div key={item.id} className="flex items-center justify-between gap-2 text-sm"><span className="min-w-0 truncate">{item.client_name}</span><Button variant="outline" size="sm" onClick={() => void revoke(item.id)}>Revoke</Button></div>)}</div>
      {error && <p role="alert" className="text-xs text-destructive">{error}</p>}
    </SettingsCard>}
  </div>
}
