import { useCallback, useEffect, useRef, useState } from 'react'
import axios from 'axios'
import { Copy, Plus, RefreshCw, Webhook } from 'lucide-react'
import { workflowWebhooksApi, apiTriggerURL, type APITriggerOptions, type WorkflowAPITrigger } from '../../api/workflowWebhooks'
import { useCanWriteWorkflow } from '../../hooks/useCanWriteWorkflow'
import { useWorkflowManifestStore } from '../../stores/useWorkflowManifestStore'
import { useWorkflowStore } from '../../stores/useWorkflowStore'

const emptyOptions: APITriggerOptions = { triggers: [], routes: [], groups: [] }
type TriggerForm = Pick<WorkflowAPITrigger, 'name' | 'enabled' | 'auth_mode' | 'route_selections' | 'group_names'> & { id?: string }
const newForm = (): TriggerForm => ({ name: '', enabled: true, auth_mode: 'bearer', route_selections: {}, group_names: [] })
const fieldClass = 'w-full rounded-md border border-border bg-background px-3 py-2 text-sm'
const buttonClass = 'rounded-md border border-border px-3 py-1.5 text-xs hover:bg-muted disabled:opacity-50'

function errorMessage(error: unknown): string {
  if (axios.isAxiosError(error) && typeof error.response?.data === 'string') return error.response.data
  return error instanceof Error ? error.message : 'Unable to update API triggers'
}

export default function WorkflowAPITriggersView({ workspacePath }: { workspacePath: string | null }) {
  const canWrite = useCanWriteWorkflow(workspacePath)
  const [options, setOptions] = useState<APITriggerOptions>(emptyOptions)
  const [form, setForm] = useState<TriggerForm | null>(null)
  const [issued, setIssued] = useState<WorkflowAPITrigger | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [copied, setCopied] = useState('')
  const requestGeneration = useRef(0)

  const refresh = useCallback(async () => {
    if (!workspacePath) return
    const generation = ++requestGeneration.current
    try {
      const result = await workflowWebhooksApi.list(workspacePath)
      if (generation === requestGeneration.current) setOptions(result)
    } catch (cause) {
      if (generation === requestGeneration.current) setError(errorMessage(cause))
    }
  }, [workspacePath])

  const cancelPendingRequests = useCallback(() => { requestGeneration.current++ }, [])

  useEffect(() => {
    setOptions(emptyOptions); setForm(null); setIssued(null); setError(''); setCopied('')
    void refresh()
    return cancelPendingRequests
  }, [refresh, cancelPendingRequests])

  const afterSave = async () => {
    if (!workspacePath) return
    await refresh()
    await useWorkflowManifestStore.getState().refreshWorkflows()
  }

  const save = async (value: TriggerForm, rotate = false) => {
    if (!workspacePath || !canWrite || busy) return
    setBusy(true); setError(''); setCopied('')
    try {
      const result = await workflowWebhooksApi.save({ ...value, workspace_path: workspacePath, rotate_secret: rotate }, value.id)
      if (result.secret) setIssued(result)
      setForm(null)
      await afterSave()
    } catch (cause) { setError(errorMessage(cause)) }
    finally { setBusy(false) }
  }

  const remove = async (id: string) => {
    if (!workspacePath || !canWrite || busy) return
    setBusy(true); setError('')
    try {
      await workflowWebhooksApi.delete(workspacePath, id)
      if (issued?.id === id) setIssued(null)
      if (form?.id === id) setForm(null)
      await afterSave()
    } catch (cause) { setError(errorMessage(cause)) }
    finally { setBusy(false) }
  }

  const copy = async (text: string, label: string) => {
    try { await navigator.clipboard.writeText(text); setCopied(label) }
    catch { setError('Clipboard access failed. Select and copy the displayed value.') }
  }

  const routers = [...new Map(options.routes.map(route => [route.step_id, route.step_title])).entries()]

  if (!workspacePath) return <p className="p-4 text-sm text-muted-foreground">Select a workflow to configure API triggers.</p>
  return (
    <div className="h-full overflow-y-auto p-4 space-y-5">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h2 className="flex items-center gap-2 text-base font-semibold"><Webhook size={17} />API triggers</h2>
          <p className="mt-1 text-xs text-muted-foreground">Start a saved workflow route when an external service sends a webhook.</p>
        </div>
        <button type="button" aria-label="Refresh API triggers" className={buttonClass} onClick={() => { setError(''); void refresh() }}><RefreshCw size={14} /></button>
      </div>
      <p className="text-xs leading-relaxed text-muted-foreground">Time triggers run on a schedule. API triggers run when their endpoint receives a request. Both use the saved plan and its route, including prerequisite steps. <button type="button" className="underline text-foreground" onClick={() => useWorkflowStore.getState().openWorkspaceView('schedules')}>View trigger runs</button></p>
      {error && <p role="alert" className="rounded-md border border-destructive/30 p-3 text-sm text-destructive">{error}</p>}
      {issued?.secret && (
        <section className="space-y-3 rounded-lg border border-primary/30 bg-primary/5 p-3">
          <h3 className="text-sm font-medium">Secret for {issued.name}</h3>
          <p className="text-xs text-muted-foreground">Copy this secret now. It is only shown when created or rotated. Rotation replaces the previous secret.</p>
          <code className="block break-all rounded bg-background p-2 text-xs select-all">{issued.secret}</code>
          <div className="flex gap-2"><button type="button" className={buttonClass} onClick={() => void copy(issued.secret!, 'Secret copied')}>Copy secret</button><button type="button" className={buttonClass} onClick={() => setIssued(null)}>Dismiss secret</button></div>
          {issued.auth_mode === 'github' ? <p className="text-xs text-muted-foreground">In GitHub webhook settings, paste the endpoint into Payload URL, choose application/json, paste this value into Secret, and select the events to deliver. Setup pings do not start runs.</p> : <pre className="overflow-x-auto rounded bg-background p-2 text-xs">{`curl -X POST '${apiTriggerURL(issued.path)}' \\\n  -H 'Authorization: Bearer YOUR_TRIGGER_SECRET' \\\n  -H 'Content-Type: application/json' \\\n  -H 'Idempotency-Key: delivery-123' \\\n  -d '{"event":"example","data":{}}'`}</pre>}
        </section>
      )}
      <div className="space-y-3">
        {options.triggers.map(trigger => (
          <section key={trigger.id} className="space-y-3 rounded-lg border border-border p-3">
            <div className="flex justify-between gap-2"><h3 className="text-sm font-medium">{trigger.name}</h3><span className="text-xs text-muted-foreground">{trigger.enabled ? 'Enabled' : 'Disabled'}</span></div>
            <div className="flex items-start gap-2"><code className="min-w-0 flex-1 break-all text-xs select-all">{apiTriggerURL(trigger.path)}</code><button type="button" aria-label={`Copy endpoint for ${trigger.name}`} className={buttonClass} onClick={() => void copy(apiTriggerURL(trigger.path), 'Endpoint copied')}><Copy size={13} /></button></div>
            <p className="text-xs text-muted-foreground">{trigger.auth_mode === 'github' ? 'GitHub signature' : 'Bearer token'} · Groups: {trigger.group_names.join(', ')}</p>
            <ul className="text-xs space-y-1">{Object.entries(trigger.route_selections).map(([stepId, routeId]) => {
              const route = options.routes.find(option => option.step_id === stepId && option.route_id === routeId)
              return <li key={stepId}>{route ? `${route.step_title} → ${route.route_name || routeId}` : `${stepId} → ${routeId} (route unavailable)`}</li>
            })}</ul>
            {canWrite && <div className="flex flex-wrap gap-2">
              <button type="button" disabled={busy} className={buttonClass} onClick={() => { setForm({ ...trigger }); setError('') }}>Edit</button>
              <button type="button" disabled={busy} className={buttonClass} onClick={() => void save({ ...trigger, enabled: !trigger.enabled })}>{trigger.enabled ? 'Disable' : 'Enable'}</button>
              <button type="button" disabled={busy} className={buttonClass} onClick={() => void save(trigger, true)}>Rotate secret</button>
              <button type="button" disabled={busy} className={buttonClass} onClick={() => void remove(trigger.id)}>Remove</button>
            </div>}
          </section>
        ))}
        {options.triggers.length === 0 && !form && <p className="rounded-lg border border-dashed border-border p-5 text-center text-sm text-muted-foreground">No API triggers attached.</p>}
      </div>
      {copied && <p role="status" className="text-xs text-muted-foreground">{copied}</p>}
      {options.route_error && <p className="text-xs text-muted-foreground">{options.route_error}</p>}
      {canWrite && !form && <button type="button" className={`${buttonClass} inline-flex items-center gap-2`} onClick={() => { setForm({ ...newForm(), group_names: options.groups.length === 1 ? options.groups : [] }); setError('') }}><Plus size={14} />Add API trigger</button>}
      {canWrite && form && (
        <form className="space-y-4 rounded-lg border border-border p-4" onSubmit={event => { event.preventDefault(); void save(form) }}>
          <h3 className="text-sm font-medium">{form.id ? 'Edit API trigger' : 'New API trigger'}</h3>
          <label className="block space-y-1 text-xs">Name<input required className={fieldClass} value={form.name} onChange={event => setForm({ ...form, name: event.target.value })} placeholder="Issue opened" /></label>
          <label className="block space-y-1 text-xs">Authentication<select className={fieldClass} value={form.auth_mode} onChange={event => setForm({ ...form, auth_mode: event.target.value as TriggerForm['auth_mode'] })}><option value="bearer">Generic API — bearer token</option><option value="github">GitHub — signed webhook</option></select></label>
          {form.id && <p className="text-xs text-muted-foreground">Changing authentication generates a new secret.</p>}
          {routers.length === 0 ? <p className="text-xs text-muted-foreground">Add a routing or branch step to the plan before attaching an API trigger.</p> : routers.map(([stepId, title]) => (
            <label key={stepId} className="block space-y-1 text-xs">Route at {title}<select className={fieldClass} value={form.route_selections[stepId] || ''} onChange={event => {
              const selections = { ...form.route_selections }
              if (event.target.value) selections[stepId] = event.target.value; else delete selections[stepId]
              setForm({ ...form, route_selections: selections })
            }}><option value="">Use workflow default</option>{options.routes.filter(route => route.step_id === stepId).map(route => <option key={route.route_id} value={route.route_id}>{route.route_name || route.route_id}</option>)}</select></label>
          ))}
          <fieldset className="space-y-2"><legend className="mb-2 text-xs">Variable groups</legend>{options.groups.length === 0 && <p className="text-xs text-muted-foreground">Add a variable group before creating a trigger.</p>}{options.groups.map(group => <label key={group} className="flex items-center gap-2 text-xs"><input type="checkbox" checked={form.group_names.includes(group)} onChange={event => setForm({ ...form, group_names: event.target.checked ? [...form.group_names, group] : form.group_names.filter(name => name !== group) })} />{group}</label>)}</fieldset>
          <label className="flex items-center gap-2 text-xs"><input type="checkbox" checked={form.enabled} onChange={event => setForm({ ...form, enabled: event.target.checked })} />Enabled</label>
          <div className="flex gap-2"><button type="submit" className={buttonClass} disabled={busy || !form.name.trim() || !Object.keys(form.route_selections).length || !form.group_names.length}>{busy ? 'Saving…' : 'Save trigger'}</button><button type="button" className={buttonClass} disabled={busy} onClick={() => setForm(null)}>Cancel</button></div>
        </form>
      )}
      <p className="text-xs leading-relaxed text-muted-foreground">Use a server URL reachable by the caller; localhost is only reachable on this computer. Public services need a reachable HTTPS deployment or tunnel. Send JSON up to 1 MiB. A successful delivery returns 202 with a run ID. Busy workflows return 503 with Retry-After; configure the sender to retry. Reuse Idempotency-Key (or GitHub’s delivery ID) to avoid duplicate runs.</p>
    </div>
  )
}
