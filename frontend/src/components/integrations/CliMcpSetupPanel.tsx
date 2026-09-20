import { useState } from 'react'
import { Check, Copy, Plug, Terminal } from 'lucide-react'
import { SettingsCard } from '../ui/SettingsCard'
import { Button } from '../ui/Button'
import { getApiBaseUrl } from '../../services/api'
import AccessTokensDialog from '../topbar/AccessTokensDialog'

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

/**
 * Shared Setup → Integrations tab for Crew projects and Builder automations.
 * Points at the live installation's own API origin; all substantive guidance
 * is fetched by the agent from the server, so nothing here can go stale.
 */
export function CliMcpSetupPanel() {
  const [managingTokens, setManagingTokens] = useState(false)
  const server = getApiBaseUrl() || window.location.origin
  const login = `agentworks login --server ${JSON.stringify(server)} --token-stdin`

  return (
    <div className="space-y-4">
      <SettingsCard
        icon={<Terminal className="h-4 w-4 text-primary" />}
        title="Command line"
        description="Run AgentWorks from your terminal. List workflows, read and edit files, and change plans — every edit is revision-checked, so stale changes are rejected instead of overwriting."
        actions={
          <Button variant="outline" size="sm" onClick={() => setManagingTokens(true)}>
            Open access tokens
          </Button>
        }
      >
        <div className="space-y-2">
          <CommandRow label="Log in command" command={login} />
          <CommandRow label="List workflows command" command="agentworks workflows list --json" />
          <CommandRow label="Load guidance command" command="agentworks guidance context --action plan_change" />
        </div>
      </SettingsCard>
      <SettingsCard
        icon={<Plug className="h-4 w-4 text-primary" />}
        title="AI assistants"
        description="Let Claude Code, Codex, or another assistant work with your workflows through the same connection. The assistant loads instructions and guidance from this server on its own."
      >
        <div className="space-y-2">
          <CommandRow label="Register MCP bridge command" command="claude mcp add --transport stdio agentworks -- agentworks mcp serve" />
          <CommandRow label="Install skill command" command="agentworks skills install --dir ~/.claude/skills" />
        </div>
      </SettingsCard>
      {managingTokens && <AccessTokensDialog onClose={() => setManagingTokens(false)} />}
    </div>
  )
}
