import { useEffect, useState } from 'react'
import GuidedProviderTerminal from './GuidedProviderTerminal'
import { llmConfigService, type ProviderSetupSession, type ProviderConnection } from '../../services/llm-config-api'

export default function ProviderAccounts({ provider, selectedId, onSelect, disabled = false }: {
  provider: string
  selectedId?: string
  onSelect?: (id: string) => void
  disabled?: boolean
}) {
  const [connections, setConnections] = useState<ProviderConnection[]>([])
  const [editingId, setEditingId] = useState<string | null>(null)
  const [authMethod, setAuthMethod] = useState('api_key')
  const [session, setSession] = useState<ProviderSetupSession | null>(null)
  const [adding, setAdding] = useState(false)
  const [name, setName] = useState('')
  const [credential, setCredential] = useState('')
  const [underlyingProvider, setUnderlyingProvider] = useState('google')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  useEffect(() => {
    let cancelled = false
    setError(null)
    const refresh=()=>{void llmConfigService.getProviderConnections().then(records => {
      if (!cancelled) setConnections(records.filter(record => record.provider === provider))
    }).catch(() => { if (!cancelled) setError('Could not load accounts.') })}
    refresh();window.addEventListener('provider-connections-changed',refresh)
    return () => { cancelled = true;window.removeEventListener('provider-connections-changed',refresh) }
  }, [provider])
  const personalAllowed=connections.find(record=>record.scope==='global')?.personal_accounts_allowed!==false
 const save = async () => {
    setBusy(true); setError(null)
    try {
      const record = editingId ? await (async()=>{await llmConfigService.updateProviderConnection(editingId,{display_name:name,...(credential ? {credential}: {})});return {...connections.find(item=>item.id===editingId)!,display_name:name}})() : await llmConfigService.addProviderConnection({ provider, display_name: name, ...(authMethod === 'cli_login' ? { auth_method: 'cli_login' } : { credential }), ...(provider === 'pi-cli' ? { underlying_provider: underlyingProvider } : {}) })
      setConnections(current => editingId ? current.map(item=>item.id===editingId ? record : item) : [...current, record]); setEditingId(null); setCredential(''); setName(''); setAdding(false)
      window.dispatchEvent(new Event('provider-connections-changed'))
      onSelect?.(record.id)
      if (record.auth_method === "cli_login") await login(record)
    } catch { setError('Could not save account. Check the provider and administrator policy.') }
    finally { setBusy(false) }
  }
  const login = async (record: ProviderConnection) => {
    setBusy(true);setError(null)
    try { setSession(await llmConfigService.startProviderSetup(provider,'authenticate',100,24,undefined,false,record.id)) }
    catch { setError('Could not start account login.') }
    finally {setBusy(false)}
  }
  const remove = async (record: ProviderConnection) => {
    setBusy(true);setError(null)
    try {await llmConfigService.deleteProviderConnection(record.id);setConnections(current=>current.filter(item=>item.id!==record.id));window.dispatchEvent(new Event('provider-connections-changed'))}
    catch {setError('Could not remove account.')}
    finally {setBusy(false)}
  }
  return <section className="rounded-lg border border-border p-3 space-y-3">
    <h3 className="text-sm font-semibold">Accounts</h3>
    {onSelect ? <select aria-label="Provider account" disabled={disabled || busy} value={selectedId || `global:${provider}`} onChange={event => onSelect(event.target.value)} className="w-full rounded border border-border bg-background p-2 text-sm">
      {selectedId && !connections.some(record => record.id === selectedId) && <option value={selectedId}>Selected account unavailable</option>}
      {connections.map(record => <option key={record.id} value={record.id}>{record.display_name}{record.scope === 'global' ? ' (shared)' : ' (private)'}</option>)}
    </select> : <ul className="text-sm space-y-1">{connections.map(record => <li key={record.id}>{record.display_name} · {record.scope === 'global' ? 'Shared' : 'Private'}</li>)}</ul>}
    {!disabled && personalAllowed && <button type="button" className="text-sm text-primary" onClick={() => {setAdding(value => !value);setEditingId(null);setName('');setCredential('');setAuthMethod('api_key');setError(null)}}>{adding ? 'Cancel' : 'Add account'}</button>}
    {!disabled && personalAllowed && connections.filter(record=>record.scope==='user').map(record=><div key={record.id} className="flex gap-3 items-center text-xs"><span>{record.display_name}</span>{record.auth_method==='cli_login' && <button disabled={busy} type="button" className="text-primary" onClick={()=>void login(record)}>Sign in</button>}<button disabled={busy} type="button" className="text-primary" onClick={()=>{setEditingId(record.id);setAdding(true);setName(record.display_name);setCredential('');setAuthMethod(record.auth_method==='cli_login' ? 'cli_login' : 'api_key')}}>Edit</button><button disabled={busy} type="button" className="text-muted-foreground" onClick={()=>void remove(record)}>Remove</button></div>)}
    {session && <GuidedProviderTerminal session={session} onFinished={value=>{setSession(value);window.dispatchEvent(new Event('provider-connections-changed'))}} onClose={()=>setSession(null)} />}
    {adding && <form className="space-y-2" onSubmit={event => { event.preventDefault(); void save() }}>
      <label className="block text-xs">Account name<input required maxLength={120} value={name} onChange={event => setName(event.target.value)} className="mt-1 block w-full rounded border bg-background p-2 text-sm" /></label>
      {!editingId && ['codex-cli','muse-cli'].includes(provider) && <label className="block text-xs">Authentication<select value={authMethod} onChange={event=>{setAuthMethod(event.target.value);setCredential('')}} className="mt-1 block w-full rounded border bg-background p-2 text-sm"><option value="api_key">API key</option><option value="cli_login">Browser login</option></select></label>}
      {authMethod !== 'cli_login' && <label className="block text-xs">{provider === 'claude-code' ? 'Claude Code OAuth token' : 'API key'}<input required={!editingId} type="password" placeholder={editingId ? 'Leave blank to keep current credential' : undefined} autoComplete="new-password" value={credential} onChange={event => setCredential(event.target.value)} className="mt-1 block w-full rounded border bg-background p-2 text-sm" /></label>}
      {provider === 'claude-code' && <p className="text-xs text-muted-foreground">Use a token generated with claude setup-token for the intended account.</p>}
      {provider === 'pi-cli' && <label className="block text-xs">Pi provider ID<input required value={underlyingProvider} onChange={event => setUnderlyingProvider(event.target.value)} className="mt-1 block w-full rounded border bg-background p-2 text-sm" /></label>}
      <p className="text-xs text-muted-foreground">Private to your user. Credentials are stored encrypted on the server.</p>
      <button disabled={busy} type="submit" className="rounded bg-primary px-3 py-2 text-sm text-primary-foreground">{busy ? 'Saving…' : editingId ? 'Update account' : 'Save account'}</button>
    </form>}
    {error && <p role="alert" className="text-xs text-red-600">{error}</p>}
  </section>
}
