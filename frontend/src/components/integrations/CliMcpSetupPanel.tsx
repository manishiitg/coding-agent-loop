import { useEffect, useState } from 'react'
import { Check, Copy, KeyRound, Plug, Terminal } from 'lucide-react'
import { SettingsCard } from '../ui/SettingsCard'
import { Button } from '../ui/Button'
import { authApi, getApiBaseUrl } from '../../services/api'

function CommandRow({ command, label }: { command: string; label: string }) {
  const [copied, setCopied] = useState(false)
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(command)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    } catch {
      setCopied(false)
    }
  }
  return (
    <div className="flex items-center gap-2">
      <code className="min-w-0 flex-1 overflow-x-auto rounded-md border border-border bg-muted/40 px-3 py-2 font-mono text-xs text-foreground" aria-label={label}>
        {command}
      </code>
      <Button variant="ghost" size="icon" className="h-7 w-7 shrink-0" title={copied ? 'Copied' : `Copy: ${label}`} onClick={() => void copy()}>
        {copied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
      </Button>
    </div>
  )
}

interface Connection {
  id: string
  token: string
}

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
 * Provisions its own read-only token and shows ready-to-paste commands — no
 * separate token dialog. The token secret is kept in this browser and reused
 * on every visit until it is revoked or expires; the server only ever confirms
 * the token id is still valid. The token reads every workflow the user can
 * access and expires after 30 days; revoke it here when done.
 */
export function CliMcpSetupPanel() {
  const [connection, setConnection] = useState<Connection | null>(null)
  const [busy, setBusy] = useState(false)
  const [checking, setChecking] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const server = getApiBaseUrl() || window.location.origin

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
        scopes: ['workflows:read', 'files:read'],
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

  const quoted = (value: string) => `'${value.replace(/'/g, `'\\''`)}'`
  const origin = server.replace(/\/+$/, '')
  const installer = connection ? `curl -fsSL ${JSON.stringify(`${origin}/api/downloads/cli/install-agentworks.sh`)} | sh -s -- --server ${JSON.stringify(origin)} --token ${quoted(connection.token)}` : ''

  return (
    <div className="space-y-4">
      <p className="text-sm text-muted-foreground">
        Connect this installation to your terminal or an AI assistant. Generate a connection below and paste the
        commands — the assistant can list workflows and read files, plans, and run logs, but cannot change anything.
        The token expires after 30 days, and you can revoke it here anytime.
      </p>
      <SettingsCard
        icon={<Terminal className="h-4 w-4 text-primary" />}
        title="Command line"
        description={
          connection
            ? 'Ready to paste. Installs the CLI and logs it in with this read-only token.'
            : 'Generate a read-only token for this installation. The command installs the CLI and logs it in.'
        }
        actions={
          connection ? (
            <div className="flex gap-2">
              <Button variant="ghost" size="sm" disabled={busy} onClick={() => void generate()}>New token</Button>
              <Button variant="outline" size="sm" className="text-destructive" disabled={busy} onClick={() => void revoke()}>Revoke</Button>
            </div>
          ) : (
            <Button size="sm" disabled={busy || checking} onClick={() => void generate()}>
              <KeyRound className="mr-1 h-3.5 w-3.5" />{busy ? 'Generating…' : 'Generate connection'}
            </Button>
          )
        }
      >
        {error && (
          <div className="rounded-md border border-destructive/30 bg-destructive/10 p-3 text-xs text-destructive">{error}</div>
        )}
        {checking ? (
          <p className="text-sm text-muted-foreground">Looking for an existing connection…</p>
        ) : !connection ? (
          <p className="text-sm text-muted-foreground">The command appears here with the token filled in. Nothing is created until you generate.</p>
        ) : (
          <div className="space-y-2">
            <CommandRow label="Install and log in command" command={installer} />
          </div>
        )}
      </SettingsCard>
      <SettingsCard
        icon={<Plug className="h-4 w-4 text-primary" />}
        title="AI assistants"
        description="Let Claude Code, Codex, or another assistant read your workflows through the same connection. Install the CLI above first — the bridge runs through it."
      >
        {!connection ? (
          <p className="text-sm text-muted-foreground">Generate a connection above first.</p>
        ) : (
          <div className="space-y-2">
            <CommandRow label="Register MCP bridge command" command={`claude mcp add --transport stdio --env AGENTWORKS_TOKEN=${quoted(connection.token)} agentworks -- agentworks mcp serve`} />
            <CommandRow label="Install skill command" command="agentworks skills install --dir ~/.claude/skills" />
          </div>
        )}
      </SettingsCard>
    </div>
  )
}
