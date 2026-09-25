import { useEffect, useState } from 'react'
import { agentCallsApi, type AgentCallResult } from '../../api/agentCalls'

export interface FunctionInputField {
  name: string
  type: string
  required?: boolean
  enum?: unknown[]
  default?: unknown
}

function fieldInitialValue(field: FunctionInputField): string {
  if (field.default === undefined || field.default === null) return ''
  if (typeof field.default === 'object') return JSON.stringify(field.default)
  return String(field.default)
}

function parseValue(field: FunctionInputField, value: string): unknown {
  if (field.type === 'boolean') return value === 'true'
  if (field.type === 'integer' || field.type === 'number') {
    const parsed = Number(value)
    if (!Number.isFinite(parsed) || field.type === 'integer' && !Number.isInteger(parsed)) throw new Error(`${field.name} needs a valid ${field.type}`)
    return parsed
  }
  if (field.type === 'object' || field.type === 'array') {
    try { return JSON.parse(value) }
    catch { throw new Error(`${field.name} needs valid JSON`) }
  }
  return value
}

export default function FunctionRunForm({ target, functionName, fields, canRun = true }: {
  target: string
  functionName: string
  fields: FunctionInputField[]
  canRun?: boolean
}) {
  const storageKey = `agent-function-inputs:${target}:${functionName}`
  const [open, setOpen] = useState(false)
  const [remembered] = useState<Record<string, string>>(() => {
    try { return JSON.parse(window.localStorage.getItem(storageKey) || '{}') }
    catch { return {} }
  })
  const [values, setValues] = useState<Record<string, string>>(() => ({
    ...Object.fromEntries(fields.map(field => [field.name, fieldInitialValue(field)])), ...remembered,
  }))
  const [call, setCall] = useState<AgentCallResult | null>(null)
  const [error, setError] = useState('')
  const [running, setRunning] = useState(false)

  useEffect(() => {
    if (!call?.call_id || call.status !== 'working' && call.status !== 'queued') return
    let active = true
    const timer = window.setInterval(() => {
      void agentCallsApi.get(call.call_id!).then(next => { if (active) setCall(next) }).catch(cause => { if (active) setError(String(cause)) })
    }, 2000)
    return () => { active = false; window.clearInterval(timer) }
  }, [call?.call_id, call?.status])

  const run = async () => {
    setError('')
    const args: Record<string, unknown> = {}
    try {
      for (const field of fields) {
        const value = values[field.name] ?? ''
        if (value === '' && field.default === undefined) {
          if (field.required) throw new Error(`${field.name} is required`)
          continue
        }
        if (value === '' && field.default !== undefined) continue
        args[field.name] = parseValue(field, value)
      }
      window.localStorage.setItem(storageKey, JSON.stringify(values))
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); return }
    setRunning(true)
    try {
      setCall(functionName === 'ask'
        ? await agentCallsApi.ask(target, String(args.message ?? ''))
        : await agentCallsApi.run(target, functionName, args))
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)) }
    finally { setRunning(false) }
  }

  const copy = () => {
    const args = Object.fromEntries(fields.filter(field => field.required && field.default === undefined).map(field => [field.name, `<${field.name}>`]))
    const sample = functionName === 'ask' ? { name: 'ask', arguments: { target, message: '<message>' } }
      : { name: 'call_function', arguments: { target, function: functionName, args } }
    void navigator.clipboard?.writeText(JSON.stringify(sample, null, 2))
  }

  return <div className="space-y-2 pt-1 text-xs">
    <div className="flex flex-wrap justify-end gap-1.5">
      <button type="button" className="rounded-md border border-border px-2 py-1 hover:bg-muted" onClick={copy}>Copy call</button>
      {canRun && <button type="button" className="rounded-md border border-border px-2 py-1 hover:bg-muted" onClick={() => setOpen(!open)}>{open ? 'Close' : 'Run now'}</button>}
    </div>
    {open && canRun && <div className="space-y-2 rounded-md bg-muted/30 p-2">
      {fields.map(field => <label key={field.name} className="flex flex-col gap-1">
        <span>{field.name}{field.required && field.default === undefined ? ' *' : ''}{Object.prototype.hasOwnProperty.call(remembered, field.name) ? ' · last used' : field.default !== undefined ? ' · default' : ''}</span>
        {field.enum?.length ? <select className="rounded border border-border bg-background px-2 py-1" value={values[field.name] ?? ''} onChange={event => setValues({ ...values, [field.name]: event.target.value })}>
          <option value="">Select</option>{field.enum.map(value => <option key={String(value)} value={String(value)}>{String(value)}</option>)}
        </select> : field.type === 'boolean' ? <select className="rounded border border-border bg-background px-2 py-1" value={values[field.name] ?? ''} onChange={event => setValues({ ...values, [field.name]: event.target.value })}>
          <option value="">Use default</option><option value="true">True</option><option value="false">False</option>
        </select> : <input className="rounded border border-border bg-background px-2 py-1" type={field.type === 'integer' || field.type === 'number' ? 'number' : 'text'} step={field.type === 'integer' ? '1' : 'any'} value={values[field.name] ?? ''} onChange={event => setValues({ ...values, [field.name]: event.target.value })} />}
      </label>)}
      <button type="button" disabled={running} className="rounded bg-primary px-3 py-1 text-primary-foreground disabled:opacity-50" onClick={() => void run()}>{running ? 'Starting…' : 'Run'}</button>
    </div>}
    {error && <p role="alert" className="text-destructive">{error}</p>}
    {call && <div className="space-y-1 rounded border border-border p-2">
      <p>{call.status}{call.call_id ? ` · ${call.call_id}` : ''}</p>
      {call.problems?.map(problem => <p key={problem} className="text-destructive">{problem}</p>)}
      {call.reason && <p className="text-destructive">{call.reason}</p>}
      {call.error && <p className="text-destructive">{call.error}</p>}
      {call.progress?.slice(-3).map((entry, index) => <p key={index} className="text-muted-foreground">{entry.message}</p>)}
      {call.answer && <p className="whitespace-pre-wrap">{call.answer}</p>}
      {call.result !== undefined && !call.answer && <pre className="max-h-56 overflow-auto whitespace-pre-wrap break-all">{JSON.stringify(call.result, null, 2)}</pre>}
      {call.run_id && <p className="text-muted-foreground">Run: {call.run_id}</p>}
    </div>}
  </div>
}
