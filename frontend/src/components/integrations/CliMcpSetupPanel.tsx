import { useEffect, useState } from 'react'
import { Check, Copy, Download, Globe, KeyRound, Plug, Terminal } from 'lucide-react'
import { SettingsCard } from '../ui/SettingsCard'
import { Button } from '../ui/Button'
import api, { authApi, externalSkillApi, getApiBaseUrl } from '../../services/api'

function CopyButton({ text, label }: { text: string; label: string }) {
  const [copied, setCopied] = useState(false)
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    } catch {
      setCopied(false)
    }
  }
  return (
    <Button variant="ghost" size="icon" className="h-7 w-7 shrink-0" title={copied ? 'Copied' : `Copy: ${label}`} onClick={() => void copy()}>
      {copied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
    </Button>
  )
}

function CommandRow({ command, label }: { command: string; label: string }) {
  return (
    <div className="flex items-center gap-2">
      <code className="min-w-0 flex-1 overflow-x-auto rounded-md border border-border bg-muted/40 px-3 py-2 font-mono text-xs text-foreground" aria-label={label}>
        {command}
      </code>
      <CopyButton text={command} label={label} />
    </div>
  )
}

function JsonBlock({ json, label, hint }: { json: string; label: string; hint: string }) {
  return (
    <div className="space-y-1">
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs font-medium text-foreground">{label}</span>
        <CopyButton text={json} label={label} />
      </div>
      <pre className="overflow-x-auto rounded-md border border-border bg-muted/40 px-3 py-2 font-mono text-xs text-foreground" aria-label={label}>
        {json}
      </pre>
      <p className="text-xs text-muted-foreground">{hint}</p>
    </div>
  )
}

interface Connection {
  id: string
  token: string
}
interface OAuthConnection { id: string; client_name: string; scopes: string[]; expires_at: string }

type Destination = 'terminal' | 'local-assistant' | 'hosted-assistant'
type LocalClient = 'claude-code' | 'codex' | 'json-client'
type HostedClient = 'chatgpt' | 'cowork'

const CONNECT_TOKEN_NAME = 'Connect tab'

function storageKey(server: string) {
  return `agentworks.connectToken.${server}`
}

function loadStored(server: string): Connection | null {
  try {
    const raw = window.localStorage.getItem(storageKey(server))
    if (!raw) return null
    const parsed = JSON.parse(raw) as Partial<Connection>
    if (typeof parsed.id !== 'string' || typeof parsed.token !== 'string' || !parsed.id || !parsed.token) return null
    return { id: parsed.id, token: parsed.token }
  } catch {
    return null
  }
}

/**
 * Shared Setup → Integrations tab for Crew projects and Builder automations.
 * Provisions a read-and-run token for local clients and shows ready-to-paste
 * commands. Hosted MCP clients use OAuth. The local token is kept in this browser and
 * reused on every visit until it is revoked or expires; the server only ever
 * confirms the token id is still valid. The token reads and runs every
 * workflow the user can access and expires after 30 days; revoke it here
 * when done.
 */
export function CliMcpSetupPanel() {
  const [destination, setDestination] = useState<Destination>('terminal')
  const [localClient, setLocalClient] = useState<LocalClient>('claude-code')
  const [hostedClient, setHostedClient] = useState<HostedClient>('chatgpt')
  const [connection, setConnection] = useState<Connection | null>(null)
  const [oauthConnections, setOAuthConnections] = useState<OAuthConnection[]>([])
  const [busy, setBusy] = useState(false)
  const [checking, setChecking] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [skillBusy, setSkillBusy] = useState<'download' | 'copy' | null>(null)
  const [skillMsg, setSkillMsg] = useState<string | null>(null)
  const [tokenCopied, setTokenCopied] = useState(false)
  const server = getApiBaseUrl() || window.location.origin

  useEffect(() => {
    if (destination !== 'hosted-assistant') return
    let cancelled = false
    api.get<{ connections: OAuthConnection[] }>('/api/oauth/mcp/connections')
      .then(({ data }) => { if (!cancelled) setOAuthConnections(data.connections) })
      .catch(() => { /* Connection instructions still work without the list. */ })
    return () => { cancelled = true }
  }, [destination])

  const revokeOAuthConnection = async (id: string) => {
    try {
      await api.delete(`/api/oauth/mcp/connections/${encodeURIComponent(id)}`)
      setOAuthConnections(current => current.filter(item => item.id !== id))
    } catch (err) { setError(err instanceof Error ? err.message : 'Could not revoke connection') }
  }

  useEffect(() => {
    let cancelled = false
    const rehydrate = async () => {
      const stored = loadStored(server)
      if (!stored) {
        if (!cancelled) setChecking(false)
        return
      }
      try {
        const { tokens } = await authApi.listAccessTokens()
        const found = tokens.find((t) => t.id === stored.id)
        const alive = found && found.revoked_at === null && new Date(found.expires_at).getTime() > Date.now()
        if (!cancelled) {
          if (alive) {
            setConnection(stored)
          } else {
            try { window.localStorage.removeItem(storageKey(server)) } catch { /* keep going */ }
          }
          setChecking(false)
        }
      } catch {
        // Unverified (offline?): still show the stored secret — it is only
        // displayed, never auto-used, and a dead token fails loudly in the
        // CLI where the user can rotate it with New token.
        if (!cancelled) {
          setConnection(stored)
          setChecking(false)
        }
      }
    }
    void rehydrate()
    return () => { cancelled = true }
  }, [server])

  const generate = async () => {
    setBusy(true)
    setError(null)
    try {
      if (connection) {
        try { await authApi.revokeAccessToken(connection.id) } catch { /* replaced below */ }
      }
      // Drop orphaned same-name tokens (e.g. this browser's secret was
      // cleared) so Generate never piles up connections.
      try {
        const { tokens } = await authApi.listAccessTokens()
        for (const token of tokens) {
          if (token.name === CONNECT_TOKEN_NAME && token.id !== connection?.id) {
            try { await authApi.revokeAccessToken(token.id) } catch { /* best effort */ }
          }
        }
      } catch { /* creation below still applies */ }
      const result = await authApi.createAccessToken({
        name: CONNECT_TOKEN_NAME,
        expires_in_days: 30,
        scopes: ['workflows:read', 'files:read', 'runs:execute'],
        all_workflows: true,
        workflow_ids: [],
      })
      const next = { id: result.access_token.id, token: result.token }
      try { window.localStorage.setItem(storageKey(server), JSON.stringify(next)) } catch { /* reuse just won't persist */ }
      setConnection(next)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const revoke = async () => {
    if (!connection) return
    setBusy(true)
    setError(null)
    try {
      await authApi.revokeAccessToken(connection.id)
      try { window.localStorage.removeItem(storageKey(server)) } catch { /* already gone */ }
      setConnection(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const downloadSkill = async () => {
    setSkillBusy('download')
    setSkillMsg(null)
    try {
      const blob = await externalSkillApi.downloadSkillZIP()
      const url = URL.createObjectURL(blob)
      try {
        const anchor = document.createElement('a')
        anchor.href = url
        anchor.download = 'agentworks-skill.zip'
        document.body.appendChild(anchor)
        anchor.click()
        anchor.remove()
      } finally {
        URL.revokeObjectURL(url)
      }
      setSkillMsg('Downloaded agentworks-skill.zip')
    } catch {
      setSkillMsg('Download failed — try again.')
    } finally {
      setSkillBusy(null)
    }
  }

  const copySkill = async () => {
    setSkillBusy('copy')
    setSkillMsg(null)
    try {
      await navigator.clipboard.writeText(await externalSkillApi.fetchSkillMD())
      setSkillMsg('Skill text copied')
      window.setTimeout(() => setSkillMsg((current) => (current === 'Skill text copied' ? null : current)), 2000)
    } catch {
      setSkillMsg('Copy failed — try again.')
    } finally {
      setSkillBusy(null)
    }
  }

  const copyToken = async () => {
    if (!connection) return
    try {
      await navigator.clipboard.writeText(connection.token)
      setTokenCopied(true)
      window.setTimeout(() => setTokenCopied(false), 1500)
    } catch {
      setTokenCopied(false)
    }
  }

  const quoted = (value: string) => `'${value.replace(/'/g, `'\\''`)}'`
  const origin = server.replace(/\/+$/, '')
  const isLoopbackOrigin = (() => {
    try {
      const host = new URL(origin).hostname
      return host === 'localhost' || host === '127.0.0.1' || host === '0.0.0.0' || host === '::1'
    } catch {
      return false
    }
  })()
  const displayToken = connection ? connection.token : 'YOUR_TOKEN'
  const installer = `curl -fsSL ${JSON.stringify(`${origin}/api/downloads/cli/install-agentworks.sh`)} | sh -s -- --server ${JSON.stringify(origin)} --token ${quoted(displayToken)}`
  const mcpJson = JSON.stringify({ mcpServers: { agentworks: { command: 'agentworks', args: ['mcp', 'serve'], env: { AGENTWORKS_SERVER: origin, AGENTWORKS_TOKEN: displayToken } } } }, null, 2)

  const destinations = [
    { id: 'terminal', icon: Terminal, title: 'Terminal or scripts', description: 'Run agentworks commands yourself.' },
    { id: 'local-assistant', icon: Plug, title: 'AI app on this computer', description: 'Claude Code, Codex, or another local MCP client.' },
    { id: 'hosted-assistant', icon: Globe, title: 'Hosted AI app', description: 'ChatGPT or Claude Cowork.' },
  ] as const

  return (
    <div className="space-y-5">
      <div className="space-y-3">
        <div>
          <h3 className="text-base font-semibold text-foreground">Where will you use AgentWorks?</h3>
          <p className="mt-1 text-sm text-muted-foreground">
            Choose one setup path. Hosted apps ask you to sign in; local tools use a personal token.
          </p>
        </div>
        <div className="grid gap-2" role="group" aria-label="Connection destination">
          {destinations.map(({ id, icon: Icon, title, description }) => (
            <button
              key={id}
              type="button"
              aria-pressed={destination === id}
              onClick={() => setDestination(id)}
              className={`flex items-center gap-3 rounded-lg border p-3 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${destination === id ? 'border-primary bg-primary/5' : 'border-border hover:border-primary/50 hover:bg-muted/30'}`}
            >
              <Icon className="h-4 w-4 shrink-0 text-primary" />
              <span className="flex min-w-0 flex-col gap-0.5">
                <span className="text-sm font-medium text-foreground">{title}</span>
                <span className="text-xs text-muted-foreground">{description}</span>
              </span>
            </button>
          ))}
        </div>
      </div>
      {destination !== 'hosted-assistant' && <SettingsCard
        icon={<KeyRound className="h-4 w-4 text-primary" />}
        title="Connection"
        description="Create a 30-day token for the CLI or a local AI app. It can read and run workflows you can access; it cannot edit plans or files."
        actions={
          connection ? (
            <div className="flex flex-wrap gap-2">
              <Button variant="ghost" size="sm" disabled={busy} onClick={() => void copyToken()}>
                <Copy className="mr-1 h-3.5 w-3.5" />{tokenCopied ? 'Copied' : 'Copy token'}
              </Button>
              <Button variant="ghost" size="sm" disabled={busy} onClick={() => void generate()}>Replace token</Button>
              <Button variant="outline" size="sm" className="text-destructive" disabled={busy} onClick={() => void revoke()}>Revoke</Button>
            </div>
          ) : (
            <Button variant="outline" size="sm" disabled={busy || checking} onClick={() => void generate()}>
              <KeyRound className="mr-1 h-3.5 w-3.5" />{busy ? 'Creating…' : 'Create connection'}
            </Button>
          )
        }
      >
        {error && (
          <div className="rounded-md border border-destructive/30 bg-destructive/10 p-3 text-xs text-destructive">{error}</div>
        )}
        {checking ? (
          <p className="text-sm text-muted-foreground">Looking for an existing connection…</p>
        ) : connection ? (
          <p className="text-sm text-muted-foreground">Connected. The setup instructions below include your token. Replacing it disconnects anything using the previous token.</p>
        ) : (
          <p className="text-sm text-muted-foreground">Create a connection to see the instructions for your chosen destination.</p>
        )}
      </SettingsCard>}
      <SettingsCard
        icon={destination === 'terminal' ? <Terminal className="h-4 w-4 text-primary" /> : destination === 'local-assistant' ? <Plug className="h-4 w-4 text-primary" /> : <Globe className="h-4 w-4 text-primary" />}
        title={destination === 'terminal' ? 'Set up the command line' : destination === 'local-assistant' ? 'Connect a local AI app' : 'Connect a hosted AI app'}
        description={destination === 'terminal' ? 'Use this for terminal commands and scripts.' : destination === 'local-assistant' ? 'Install the CLI on this computer, then add its MCP bridge to your AI app.' : 'Use the HTTPS MCP URL in an AI app that connects from the cloud. No CLI install is needed.'}
      >
        {destination === 'hosted-assistant' ? (
          <div className="space-y-4">
            <div className="flex flex-wrap gap-2" role="group" aria-label="Hosted MCP client">
              <Button variant={hostedClient === 'chatgpt' ? 'default' : 'outline'} size="sm" aria-pressed={hostedClient === 'chatgpt'} onClick={() => setHostedClient('chatgpt')}>ChatGPT</Button>
              <Button variant={hostedClient === 'cowork' ? 'default' : 'outline'} size="sm" aria-pressed={hostedClient === 'cowork'} onClick={() => setHostedClient('cowork')}>Claude Cowork</Button>
            </div>
            <p className="text-xs text-muted-foreground">
              {hostedClient === 'chatgpt'
                ? 'In ChatGPT, open Settings → Apps & Connectors → Developer Mode, then add a custom MCP connector.'
                : 'In Claude Cowork, open Settings → Connectors → Add custom connector.'}
            </p>
            <CommandRow label="Remote MCP URL" command={`${origin}/api/external/v1/mcp`} />
            {isLoopbackOrigin && <p className="text-xs text-amber-500">Hosted apps need a public server URL; open Connect on that server instead.</p>}
            <p className="text-xs text-muted-foreground">Choose OAuth when the app asks how to authenticate. AgentWorks will open a sign-in and permission screen.</p>
            {oauthConnections.length > 0 && <div className="space-y-2 border-t border-border pt-3">
              <p className="text-xs font-medium text-foreground">Connected apps</p>
              {oauthConnections.map(item => <div key={item.id} className="flex items-center justify-between gap-2 text-xs">
                <span className="min-w-0 truncate">{item.client_name}</span>
                <Button variant="outline" size="sm" onClick={() => void revokeOAuthConnection(item.id)}>Revoke</Button>
              </div>)}
            </div>}
            {error && <p role="alert" className="text-xs text-destructive">{error}</p>}
            <details className="rounded-md border border-border p-3 text-xs text-muted-foreground">
              <summary className="cursor-pointer font-medium text-foreground">Optional: give the assistant workflow guidance</summary>
              <p className="mt-2">Upload the skill where Skills are supported, or paste its text into the app&apos;s custom instructions.</p>
              <div className="mt-3 flex flex-wrap gap-2">
                <Button variant="outline" size="sm" disabled={skillBusy !== null} onClick={() => void downloadSkill()}><Download className="mr-1 h-3.5 w-3.5" />Download skill .zip</Button>
                <Button variant="ghost" size="sm" disabled={skillBusy !== null} onClick={() => void copySkill()}><Copy className="mr-1 h-3.5 w-3.5" />Copy skill text</Button>
              </div>
              {skillMsg && <p className="mt-2">{skillMsg}</p>}
            </details>
          </div>
        ) : checking ? (
          <p className="text-sm text-muted-foreground">Looking for an existing connection…</p>
        ) : !connection ? (
          <p className="rounded-md border border-dashed border-border p-3 text-sm text-muted-foreground">Create a connection above to get the setup details.</p>
        ) : destination === 'terminal' ? (
          <div className="space-y-2">
            <p className="text-xs font-medium text-foreground">Paste this in your terminal to install the CLI and sign in.</p>
            <CommandRow label="Install and sign in" command={installer} />
          </div>
        ) : destination === 'local-assistant' ? (
          <div className="space-y-4">
            <div className="space-y-2">
              <p className="text-xs font-medium text-foreground">1. Install the CLI on this computer</p>
              <CommandRow label="Install CLI for local MCP" command={installer} />
            </div>
            <div className="space-y-2">
              <p className="text-xs font-medium text-foreground">2. Choose your AI app</p>
              <div className="flex flex-wrap gap-2" role="group" aria-label="Local MCP client">
                <Button variant={localClient === 'claude-code' ? 'default' : 'outline'} size="sm" aria-pressed={localClient === 'claude-code'} onClick={() => setLocalClient('claude-code')}>Claude Code</Button>
                <Button variant={localClient === 'codex' ? 'default' : 'outline'} size="sm" aria-pressed={localClient === 'codex'} onClick={() => setLocalClient('codex')}>Codex</Button>
                <Button variant={localClient === 'json-client' ? 'default' : 'outline'} size="sm" aria-pressed={localClient === 'json-client'} onClick={() => setLocalClient('json-client')}>JSON MCP client</Button>
              </div>
              {localClient === 'claude-code' ? (
                <CommandRow label="Add AgentWorks to Claude Code" command={`claude mcp add agentworks -e AGENTWORKS_SERVER=${quoted(origin)} -e AGENTWORKS_TOKEN=${quoted(displayToken)} -- agentworks mcp serve`} />
              ) : localClient === 'codex' ? (
                <CommandRow label="Add AgentWorks to Codex" command={`codex mcp add agentworks --env AGENTWORKS_SERVER=${quoted(origin)} --env AGENTWORKS_TOKEN=${quoted(displayToken)} -- agentworks mcp serve`} />
              ) : (
                <JsonBlock label="MCP client config" json={mcpJson} hint="Paste into a JSON-configured local MCP client such as Claude Desktop or Cursor. If it cannot find agentworks, run which agentworks in a terminal and use that full path as the command." />
              )}
            </div>
            {localClient === 'claude-code' && (
              <details className="rounded-md border border-border p-3 text-xs text-muted-foreground">
                <summary className="cursor-pointer font-medium text-foreground">Optional: install the AgentWorks skill</summary>
                <p className="mt-2 mb-2">The skill teaches Claude Code how to use the available workflow tools.</p>
                <CommandRow label="Install skill for Claude Code" command="agentworks skills install --dir ~/.claude/skills" />
              </details>
            )}
          </div>
        ) : null}
      </SettingsCard>
    </div>
  )
}
