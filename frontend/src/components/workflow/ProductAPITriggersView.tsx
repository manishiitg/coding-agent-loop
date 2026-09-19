import { useCallback, useEffect, useState, type ReactNode } from 'react'
import axios from 'axios'
import { Copy, RefreshCw, Webhook } from 'lucide-react'
import { apiTriggerURL, productWebhooksApi, type ProductAPITrigger, type ProductTriggerScope } from '../../api/productWebhooks'

const buttonClass = 'rounded-md border border-border px-3 py-1.5 text-xs hover:bg-muted disabled:opacity-50'

function errorMessage(error: unknown): string {
  if (axios.isAxiosError(error) && typeof error.response?.data === 'string') return error.response.data
  return error instanceof Error ? error.message : 'Unable to update project triggers'
}

export default function ProductAPITriggersView({ scope, onViewRuns, deliveryHistory, headerAction }: { scope: ProductTriggerScope; onViewRuns?: () => void; deliveryHistory?: ReactNode; headerAction?: React.ReactNode }) {
  const { profileId, projectId } = scope
  const [triggers, setTriggers] = useState<ProductAPITrigger[]>([])
  const [issued, setIssued] = useState<ProductAPITrigger | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [copied, setCopied] = useState('')

  const refresh = useCallback(async () => {
    try { setTriggers((await productWebhooksApi.list({ profileId, projectId })).triggers) }
    catch (cause) { setError(errorMessage(cause)) }
  }, [profileId, projectId])

  useEffect(() => { setError(''); setIssued(null); void refresh() }, [refresh])

  const save = async (trigger: ProductAPITrigger, rotate = false) => {
    if (busy) return
    setBusy(true); setError(''); setCopied('')
    try {
      const result = await productWebhooksApi.save(scope, trigger, rotate)
      if (result.secret) setIssued(result)
      await refresh()
    } catch (cause) { setError(errorMessage(cause)) }
    finally { setBusy(false) }
  }

  const remove = async (id: string) => {
    if (busy) return
    setBusy(true); setError('')
    try { await productWebhooksApi.delete(scope, id); await refresh() }
    catch (cause) { setError(errorMessage(cause)) }
    finally { setBusy(false) }
  }

  const copy = async (text: string, label: string) => {
    try { await navigator.clipboard.writeText(text); setCopied(label) }
    catch { setError('Clipboard access failed. Select and copy the displayed value.') }
  }

  return <div className="h-full min-w-0 w-full max-w-none space-y-5 overflow-x-hidden overflow-y-auto p-4">
    <div className="flex items-start justify-between gap-3">
      <div>
        <h2 className="flex items-center gap-2 text-base font-semibold"><Webhook size={17} />Webhooks</h2>
        <p className="mt-1 text-xs text-muted-foreground">Send one saved message to this project when an external service sends authenticated JSON.</p>
      </div>
      <div className="flex items-center gap-2"><button type="button" aria-label="Refresh project triggers" className={buttonClass} onClick={() => void refresh()}><RefreshCw size={14} /></button>{headerAction}</div>
    </div>
    <p className="text-xs leading-relaxed text-muted-foreground">Schedules start by time; webhooks start on delivery. Both use the same durable Builder conversation. Webhooks do not run AgentWorks routes or workflow steps. {!deliveryHistory && onViewRuns && <button type="button" className="underline text-foreground" onClick={onViewRuns}>View delivery history</button>}</p>
    <p className="text-xs text-muted-foreground">Ask the project chat to create a webhook or change its saved message and authentication.</p>
    {error && <p role="alert" className="rounded-md border border-destructive/30 p-3 text-sm text-destructive">{error}</p>}
    {issued?.secret && <section className="space-y-3 rounded-lg border border-primary/30 bg-primary/5 p-3">
      <h3 className="text-sm font-medium">New secret for {issued.name}</h3>
      <p className="text-xs text-muted-foreground">Copy it now. It is only shown when created or rotated.</p>
      <code className="block break-all rounded bg-background p-2 text-xs select-all">{issued.secret}</code>
      <button type="button" className={buttonClass} onClick={() => void copy(issued.secret!, 'Secret copied')}>Copy secret</button>
    </section>}
    <div className="min-w-0 max-w-full space-y-3">{triggers.map(trigger => <section key={trigger.id} className="min-w-0 max-w-full space-y-3 overflow-hidden rounded-lg border border-border p-3">
      <div className="flex justify-between gap-2"><h3 className="text-sm font-medium">{trigger.name}</h3><span className="text-xs text-muted-foreground">{trigger.enabled ? 'Enabled' : 'Disabled'}</span></div>
      <div className="flex items-start gap-2"><code className="min-w-0 flex-1 break-all text-xs select-all">{apiTriggerURL(trigger.path)}</code><button type="button" aria-label={`Copy endpoint for ${trigger.name}`} className={buttonClass} onClick={() => void copy(apiTriggerURL(trigger.path), 'Endpoint copied')}><Copy size={13} /></button></div>
      <p className="text-xs text-muted-foreground">{trigger.auth_mode === 'github' ? 'GitHub signature' : 'Bearer token'} · Message: {trigger.message}</p>
      <div className="flex flex-wrap gap-2"><button type="button" disabled={busy} className={buttonClass} onClick={() => void save({ ...trigger, enabled: !trigger.enabled })}>{trigger.enabled ? 'Disable' : 'Enable'}</button><button type="button" disabled={busy} className={buttonClass} onClick={() => void save(trigger, true)}>Rotate secret</button><button type="button" disabled={busy} className={buttonClass} onClick={() => void remove(trigger.id)}>Remove</button></div>
    </section>)}{triggers.length === 0 && <p className="rounded-lg border border-dashed border-border p-5 text-center text-sm text-muted-foreground">No webhooks configured. Ask the project chat to create one.</p>}</div>
    {copied && <p role="status" className="text-xs text-muted-foreground">{copied}</p>}
    {deliveryHistory && <div className="min-w-0 max-w-full overflow-hidden rounded-lg border border-border">{deliveryHistory}</div>}
    <p className="text-xs leading-relaxed text-muted-foreground">Send JSON up to 1 MiB. Bearer triggers use the Authorization header; GitHub triggers verify X-Hub-Signature-256. Reuse an Idempotency-Key or GitHub delivery ID to avoid duplicate runs.</p>
  </div>
}
