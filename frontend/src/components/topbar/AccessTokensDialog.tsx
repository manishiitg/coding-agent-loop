import { useEffect, useRef, useState } from 'react'
import { Copy, Terminal, X } from 'lucide-react'
import ModalPortal from '../ui/ModalPortal'
import { agentApi, authApi, getApiBaseUrl, type PersonalAccessToken } from '../../services/api'

const permissions = [
  ['workflows:read', 'Read workflows, plans and run logs'],
  ['files:read', 'Read and search documents and skills'],
  ['files:write', 'Create and edit documents and skills'],
  ['plan:write', 'Edit plans through validated tools'],
  ['builder:chat', 'Use Workflow Builder chat'],
] as const
const errorMessage = (error: unknown) => {
  const e = error as { response?: { data?: { error?: { message?: string } | string } }; message?: string }
  const detail = e.response?.data?.error
  return (typeof detail === 'string' ? detail : detail?.message) || e.message || 'Could not complete the request.'
}
const date = (value: string) => new Date(value).toLocaleDateString()

export default function AccessTokensDialog({ onClose }: { onClose: () => void }) {
  const [tokens, setTokens] = useState<PersonalAccessToken[]>([])
  const [workflows, setWorkflows] = useState<{ id: string; label: string }[]>([])
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [name, setName] = useState('')
  const [days, setDays] = useState(30)
  const [allWorkflows, setAllWorkflows] = useState(true)
  const [workflowIDs, setWorkflowIDs] = useState<string[]>([])
  const [scopes, setScopes] = useState<string[]>(['workflows:read', 'files:read'])
  const [created, setCreated] = useState('')
  const [copied, setCopied] = useState(false)
  const dialog = useRef<HTMLDivElement>(null)
  const mounted = useRef(true)
  const busyRef = useRef(false)
  const closeRef = useRef(onClose)
  closeRef.current = onClose

  useEffect(() => {
    mounted.current = true
    const previous = document.activeElement as HTMLElement | null
    dialog.current?.querySelector<HTMLInputElement>('input')?.focus()
    void Promise.all([authApi.listAccessTokens(), agentApi.listWorkflowManifests()]).then(([list, choices]) => {
      if (!mounted.current) return
      setTokens(list.tokens)
      setWorkflows(choices.workflows.map(w => ({ id: w.manifest.id, label: w.manifest.label })))
    }).catch(e => { if (mounted.current) setError(errorMessage(e)) }).finally(() => { if (mounted.current) setLoading(false) })
    const keydown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !busyRef.current) { event.preventDefault(); event.stopPropagation(); closeRef.current() }
      if (event.key !== 'Tab') return
      const items = dialog.current?.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled)')
      if (!items?.length) return
      const first = items[0], last = items[items.length - 1]
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus() }
      if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
    }
    document.addEventListener('keydown', keydown)
    return () => { mounted.current = false; previous?.focus(); document.removeEventListener('keydown', keydown) }
  }, [])

  const changeScope = (scope: string, checked: boolean) => {
    if (scope === 'builder:chat' && checked) {
      setScopes(permissions.map(([key]) => key)); setAllWorkflows(true); setWorkflowIDs([])
    } else {
      setScopes(current => checked ? [...current, scope] : current.filter(s => s !== scope && s !== 'builder:chat'))
    }
  }
  const create = async () => {
    if (busyRef.current) return
    busyRef.current = true; setBusy(true); setError('')
    try {
      const result = await authApi.createAccessToken({ name: name.trim(), expires_in_days: days, scopes, all_workflows: allWorkflows, workflow_ids: allWorkflows ? [] : workflowIDs })
      if (mounted.current) { setCreated(result.token); setCopied(false); setTokens(current => [result.access_token, ...current]) }
    } catch (e) { if (mounted.current) setError(errorMessage(e)) }
    finally { busyRef.current = false; if (mounted.current) setBusy(false) }
  }
  const revoke = async (id: string) => {
    if (busyRef.current) return
    busyRef.current = true; setBusy(true); setError('')
    try {
      await authApi.revokeAccessToken(id)
      if (mounted.current) setTokens(current => current.map(t => t.id === id ? { ...t, revoked_at: new Date().toISOString() } : t))
    } catch (e) { if (mounted.current) setError(errorMessage(e)) }
    finally { busyRef.current = false; if (mounted.current) setBusy(false) }
  }
  const copy = async () => {
    try { await navigator.clipboard.writeText(created); if (mounted.current) setCopied(true) }
    catch { if (mounted.current) setError('Could not copy automatically. Select and copy the token below.') }
  }
  const inputClass = 'w-full mt-1 px-3 py-2 text-sm bg-background border border-border rounded-md focus:outline-none focus:ring-2 focus:ring-primary'
  const buttonClass = 'px-3 py-2 text-sm rounded-md border border-border hover:bg-accent disabled:opacity-50'
  const server = getApiBaseUrl() || window.location.origin
  const command = `agentworks login --server ${JSON.stringify(server)} --token-stdin`

  return <ModalPortal>
    <div className="fixed inset-0 z-[70] flex items-center justify-center bg-black/50 p-4" onMouseDown={e => { if (e.target === e.currentTarget && !busy) onClose() }}>
      <div ref={dialog} role="dialog" aria-modal="true" aria-labelledby="access-tokens-title" className="bg-background border border-border rounded-xl shadow-xl w-full max-w-2xl max-h-[90vh] overflow-y-auto">
        <div className="flex items-center justify-between p-5 border-b border-border">
          <div><h2 id="access-tokens-title" className="text-lg font-semibold flex items-center gap-2"><Terminal className="w-5 h-5" />Access tokens</h2>
            <p className="text-sm text-muted-foreground mt-1">Connect the AgentWorks CLI, MCP clients and scripts.</p></div>
          <button className={buttonClass} disabled={busy} onClick={onClose} aria-label="Close access tokens"><X className="w-4 h-4" /></button>
        </div>
        <div className="p-5 space-y-5">
          {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
          {created ? <section className="space-y-3" aria-label="New access token">
            <h3 className="font-medium">Your token is ready</h3>
            <p className="text-sm text-muted-foreground">Copy it now. You cannot view it again after closing this screen.</p>
            <label className="block text-sm">Access token<textarea autoFocus readOnly value={created} className={`${inputClass} font-mono break-all`} onFocus={e => e.target.select()} /></label>
            <button className={buttonClass} onClick={() => void copy()}><Copy className="w-4 h-4 inline mr-2" />{copied ? 'Copied' : 'Copy token'}</button>
            <p className="text-sm">Run this command, paste the token, then press Enter and Ctrl-D to finish standard input on macOS/Linux.</p>
            <pre className="p-3 bg-muted rounded-md text-xs whitespace-pre-wrap break-all">{command}</pre>
            <p className="text-sm text-muted-foreground">For MCP, use <code>agentworks mcp serve</code> after login. Access ends at expiry or when you revoke this token.</p>
            <button className={buttonClass} onClick={() => { setCreated(''); setName(''); setScopes(['workflows:read', 'files:read']); setAllWorkflows(true); setWorkflowIDs([]) }}>Done</button>
          </section> : <form className="space-y-4" onSubmit={e => { e.preventDefault(); void create() }}>
            <h3 className="font-medium">Generate a token</h3>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <label className="text-sm">Name<input required maxLength={80} value={name} onChange={e => setName(e.target.value)} placeholder="Claude Code on my laptop" className={inputClass} /></label>
              <label className="text-sm">Expires in<select value={days} onChange={e => setDays(Number(e.target.value))} className={inputClass}><option value={7}>7 days</option><option value={30}>30 days</option><option value={90}>90 days</option></select></label>
            </div>
            <fieldset className="space-y-2"><legend className="text-sm font-medium mb-2">Permissions</legend>
              {permissions.map(([key, label]) => <label key={key} className="flex items-center gap-2 text-sm"><input type="checkbox" checked={scopes.includes(key)} onChange={e => changeScope(key, e.target.checked)} />{label}</label>)}
              <p className="text-xs text-muted-foreground">Builder chat requires all permissions and all accessible workflows. It can run tools and shell commands with your existing account access.</p>
            </fieldset>
            <label className="block text-sm">Workflow access<select className={inputClass} value={allWorkflows ? 'all' : 'selected'} onChange={e => { const all = e.target.value === 'all'; setAllWorkflows(all); if (!all) setScopes(s => s.filter(v => v !== 'builder:chat')) }}><option value="all">All workflows I can access, including future workflows</option><option value="selected">Selected workflows</option></select></label>
            {!allWorkflows && <fieldset className="space-y-2 border border-border rounded-md p-3 max-h-40 overflow-auto"><legend className="text-sm">Select workflows</legend>
              {workflows.map(w => <label key={w.id} className="flex items-center gap-2 text-sm"><input type="checkbox" checked={workflowIDs.includes(w.id)} onChange={e => setWorkflowIDs(ids => e.target.checked ? [...ids, w.id] : ids.filter(id => id !== w.id))} />{w.label}</label>)}
              {!workflows.length && <p className="text-sm text-muted-foreground">No accessible workflows.</p>}
            </fieldset>}
            <p className="text-xs text-muted-foreground">A token can never grant more access than your account has.</p>
            <button type="submit" disabled={loading || busy || !name.trim() || !scopes.length || (!allWorkflows && !workflowIDs.length)} className="px-4 py-2 rounded-md bg-primary text-primary-foreground text-sm disabled:opacity-50">{busy ? 'Saving…' : 'Generate token'}</button>
          </form>}
          <section className="border-t border-border pt-4 space-y-3" aria-label="Existing access tokens"><h3 className="font-medium">Your tokens</h3>
            {loading ? <p role="status" className="text-sm text-muted-foreground">Loading tokens…</p> : !tokens.length ? <p className="text-sm text-muted-foreground">No tokens yet.</p> : tokens.map(t => {
              const expired = new Date(t.expires_at).getTime() <= Date.now()
              return <div key={t.id} className="flex items-start justify-between gap-3 rounded-md border border-border p-3">
                <div className="min-w-0 space-y-1"><p className="text-sm font-medium break-words">{t.name}</p>
                  <p className="text-xs text-muted-foreground">{t.revoked_at ? 'Revoked' : expired ? 'Expired' : `Expires ${date(t.expires_at)}`} · {t.last_used_at ? `Last used ${new Date(t.last_used_at).toLocaleString()}` : 'Never used'}</p>
                  <p className="text-xs text-muted-foreground">{t.all_workflows ? 'All accessible workflows' : `${t.workflow_ids?.length || 0} selected workflows`} · {t.scopes.map(s => permissions.find(([key]) => key === s)?.[1] || s).join(', ')}</p></div>
                {!t.revoked_at && !expired && <button className={`${buttonClass} text-destructive shrink-0`} disabled={busy} onClick={() => void revoke(t.id)} aria-label={`Revoke ${t.name}`}>Revoke</button>}
              </div>
            })}
          </section>
        </div>
      </div>
    </div>
  </ModalPortal>
}
