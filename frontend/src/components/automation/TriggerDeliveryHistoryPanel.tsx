import { useCallback, useEffect, useState } from 'react'
import { ArrowUpRight, Loader2 } from 'lucide-react'
import { productWebhooksApi, type ProductAPITrigger, type ProductTriggerScope } from '../../api/productWebhooks'
import type { ScheduledJobRun } from '../../services/api-types'
import { useResumePreviousChat } from '../../hooks/useResumePreviousChat'
import { PreviousChatHistoryPanel } from '../PreviousChatHistoryPanel'

function ProductTriggerDeliveryHistory({ scope }: { scope: ProductTriggerScope }) {
  const [items, setItems] = useState<Array<{ trigger: ProductAPITrigger; run: ScheduledJobRun }>>([])
  const [loading, setLoading] = useState(true)
  const [failed, setFailed] = useState(false)
  const openChat = useResumePreviousChat()
  const refresh = useCallback(async () => {
    try {
      const triggers = (await productWebhooksApi.list(scope)).triggers
      const histories = await Promise.all(triggers.map(async trigger => ({
        trigger,
        runs: (await productWebhooksApi.runs(scope, trigger.id, 30)).runs || [],
      })))
      setItems(histories.flatMap(({ trigger, runs }) => runs.map(run => ({ trigger, run })))
        .sort((a, b) => Date.parse(b.run.started_at || '') - Date.parse(a.run.started_at || '')))
      setFailed(false)
    } catch {
      setFailed(true)
    } finally {
      setLoading(false)
    }
  }, [scope])
  useEffect(() => {
    void refresh()
    const timer = window.setInterval(() => { if (!document.hidden) void refresh() }, 10000)
    return () => window.clearInterval(timer)
  }, [refresh])

  return <section className="min-w-0 border-b border-border bg-background">
    <div className="flex items-center justify-between border-b border-border px-3 py-2">
      <h3 className="text-sm font-medium text-foreground">Delivery history</h3>
      <span className="text-xs text-muted-foreground">{loading ? 'Loading…' : items.length}</span>
    </div>
    {loading ? <div className="flex items-center gap-2 px-3 py-4 text-xs text-muted-foreground"><Loader2 className="h-3.5 w-3.5 animate-spin" />Loading deliveries…</div>
      : failed ? <p className="px-3 py-4 text-xs text-destructive">Could not load delivery history.</p>
        : items.length === 0 ? <p className="px-3 py-4 text-xs text-muted-foreground">No webhook deliveries recorded yet.</p>
          : <div className="divide-y divide-border">{items.map(({ trigger, run }) => <button
            key={run.id}
            type="button"
            disabled={!run.session_id}
            onClick={() => run.session_id && void openChat({
              session_id: run.session_id,
              title: trigger.name,
              query: trigger.message,
              workshop_mode: 'run',
              created_at: run.started_at,
              updated_at: run.completed_at || run.started_at,
            })}
            className="flex w-full items-center gap-3 px-3 py-2 text-left transition-colors hover:bg-muted/30 disabled:cursor-default"
          >
            <span className={`h-2 w-2 shrink-0 rounded-full ${run.status === 'success' ? 'bg-emerald-500' : run.status === 'running' ? 'animate-pulse bg-sky-500' : 'bg-destructive'}`} />
            <span className="min-w-0 flex-1"><span className="block truncate text-xs font-medium text-foreground">{trigger.name}</span><span className="block text-[10px] text-muted-foreground">{new Date(run.started_at).toLocaleString()} · {run.status}</span></span>
            {run.session_id && <ArrowUpRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />}
          </button>)}</div>}
  </section>
}

export function TriggerDeliveryHistoryPanel({
  workspacePath,
  entityType,
  productTriggerScope,
}: {
  workspacePath: string
  entityType: 'workflow' | 'product'
  productTriggerScope?: ProductTriggerScope
}) {
  if (entityType === 'product' && productTriggerScope) {
    return <ProductTriggerDeliveryHistory scope={productTriggerScope} />
  }
  return (
    <PreviousChatHistoryPanel
      workspacePath={workspacePath}
      title="Delivery history"
      emptyText="No webhook deliveries recorded yet."
      runOnly="webhook"
      runEntityType={entityType}
      readOnly
      showAll
      onSelectSession={() => {}}
    />
  )
}
