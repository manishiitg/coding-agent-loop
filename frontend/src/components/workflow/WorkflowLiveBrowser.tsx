import { useEffect, useRef, useState } from 'react'
import type { KeyboardEvent, MouseEvent } from 'react'
import api, { getApiBaseUrl, getAuthToken } from '../../services/api'
import { useCanWriteWorkflow } from '../../hooks/useCanWriteWorkflow'

type BrowserSession = { browser_session: string; workflow_session: string }
type BrowserTab = { tabId: string; title: string; url: string; active: boolean }

export default function WorkflowLiveBrowser({ workspacePath }: { workspacePath: string | null }) {
  const [sessions, setSessions] = useState<BrowserSession[]>([])
  const [session, setSession] = useState('')
  const [tabs, setTabs] = useState<BrowserTab[]>([])
  const [frame, setFrame] = useState('')
  const [connected, setConnected] = useState(false)
  const [controlling, setControlling] = useState(false)
  const [error, setError] = useState('')
  const [retry, setRetry] = useState(0)
  const socket = useRef<WebSocket | null>(null)
  const viewport = useRef({ width: 1280, height: 720 })
  const screen = useRef<HTMLImageElement>(null)
  const canControl = useCanWriteWorkflow(workspacePath)

  useEffect(() => {
    let cancelled = false
    const controller = new AbortController()
    setSessions([])
    setSession('')
    const poll = async () => {
      if (!workspacePath) return
      try {
        const { data } = await api.get<{ sessions: BrowserSession[] }>('/api/browser/live/sessions', {
          params: { workspace_path: workspacePath }, signal: controller.signal, timeout: 5000,
        })
        if (cancelled) return
        setSessions(data.sessions)
        setSession(current => data.sessions.some(item => item.browser_session === current) ? current : data.sessions[0]?.browser_session || '')
      } catch {
        if (!cancelled) setError('Unable to load workflow browser sessions.')
      }
    }
    void poll()
    const timer = window.setInterval(() => { void poll() }, 5000)
    return () => { cancelled = true; controller.abort(); window.clearInterval(timer) }
  }, [workspacePath])

  useEffect(() => {
    setFrame(''); setTabs([]); setConnected(false); setControlling(false); setError('')
    if (!session || !workspacePath) return
    const url = new URL(`${getApiBaseUrl() || window.location.origin}/api/browser/live/${encodeURIComponent(session)}/stream`)
    url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
    url.searchParams.set('workspace_path', workspacePath)
    const token = getAuthToken()
    if (token) url.searchParams.set('token', token)
    const ws = new WebSocket(url)
    socket.current = ws
    let disposed = false
    ws.onopen = () => { if (!disposed) setConnected(true) }
    ws.onmessage = event => {
      if (disposed) return
      try {
        const message = JSON.parse(event.data)
        if (message.type === 'frame' && typeof message.data === 'string') {
          setFrame(`data:image/${message.data.startsWith('iVBOR') ? 'png' : 'jpeg'};base64,${message.data}`)
          if (message.metadata?.deviceWidth > 0 && message.metadata?.deviceHeight > 0) {
            viewport.current = { width: message.metadata.deviceWidth, height: message.metadata.deviceHeight }
          }
        } else if (message.type === 'tabs' && Array.isArray(message.tabs)) {
          setTabs(message.tabs)
        } else if (message.type === 'viewer_control') {
          setControlling(message.controlling === true)
          setError('')
          if (message.controlling) screen.current?.focus()
          else screen.current?.blur()
        } else if (message.type === 'viewer_error') {
          setError(message.message)
        }
      } catch { /* Ignore unsupported runtime messages. */ }
    }
    ws.onclose = () => {
      if (!disposed) { setConnected(false); setControlling(false); setFrame(''); setError('Live view disconnected. Reconnect to continue watching. If it persists, check that the server has a streaming-capable agent-browser version.') }
    }
    const heartbeat = window.setInterval(() => {
      if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: 'ping' }))
    }, 10000)
    return () => { disposed = true; window.clearInterval(heartbeat); ws.close(); if (socket.current === ws) socket.current = null }
  }, [session, workspacePath, retry])

  const hasFrame = Boolean(frame)
  useEffect(() => {
    const image = screen.current
    if (!image || !controlling) return
    const wheel = (event: WheelEvent) => {
      event.preventDefault()
      const rect = image.getBoundingClientRect()
      const ws = socket.current
      if (!rect.width || !rect.height || ws?.readyState !== WebSocket.OPEN) return
      const unit = event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? viewport.current.height : 1
      ws.send(JSON.stringify({ type: 'input_mouse', eventType: 'mouseWheel',
        x: (event.clientX - rect.left) * viewport.current.width / rect.width,
        y: (event.clientY - rect.top) * viewport.current.height / rect.height,
        deltaX: event.deltaX * unit, deltaY: event.deltaY * unit,
      }))
    }
    image.addEventListener('wheel', wheel, { passive: false })
    return () => image.removeEventListener('wheel', wheel)
  }, [controlling, hasFrame])

  function send(message: Record<string, unknown>) {
    if (socket.current?.readyState === WebSocket.OPEN) socket.current.send(JSON.stringify(message))
  }
  function point(clientX: number, clientY: number) {
    const rect = screen.current?.getBoundingClientRect()
    if (!rect || !rect.width || !rect.height) return { x: 0, y: 0 }
    return { x: (clientX - rect.left) * viewport.current.width / rect.width, y: (clientY - rect.top) * viewport.current.height / rect.height }
  }
  function mouse(event: MouseEvent<HTMLImageElement>, eventType: string) {
    if (!controlling) return
    event.preventDefault()
    if (eventType === 'mousePressed') screen.current?.focus()
    send({ type: 'input_mouse', eventType, ...point(event.clientX, event.clientY), button: eventType === 'mouseMoved' && !event.buttons ? 'none' : ['left', 'middle', 'right'][event.button] || 'left', clickCount: event.detail || 1 })
  }
  function keyboard(event: KeyboardEvent<HTMLImageElement>, eventType: string) {
    if (!controlling) return
    // Escape releases control and keeps keyboard users from being trapped.
    if (event.key === 'Escape') { event.preventDefault(); send({ type: 'release_control' }); return }
    event.preventDefault()
    const modifiers = (event.altKey ? 1 : 0) | (event.ctrlKey ? 2 : 0) | (event.metaKey ? 4 : 0) | (event.shiftKey ? 8 : 0)
    send({ type: 'input_keyboard', eventType, key: event.key, code: event.code, text: eventType === 'keyDown' && event.key.length === 1 && !event.ctrlKey && !event.metaKey && !event.altKey ? event.key : '', windowsVirtualKeyCode: event.keyCode, modifiers })
  }

  return (
    <section className="mb-5 overflow-hidden rounded-lg border border-border bg-background" aria-label="Live workflow browser">
      <div className="flex flex-wrap items-center gap-3 border-b border-border p-3">
        <h3 className="text-sm font-medium">Live browser</h3>
        <span className="text-xs text-muted-foreground" role="status">{controlling ? 'You have control' : connected ? 'Watching' : 'Not connected'}</span>
        {sessions.length > 0 && <select className="min-w-0 max-w-64 rounded border border-border bg-background p-1 text-xs" aria-label="Browser session" value={session} onChange={event => setSession(event.target.value)}>
          {sessions.map((item, index) => <option key={item.browser_session} value={item.browser_session}>Browser {index + 1} · {item.workflow_session.slice(0, 8)}</option>)}
        </select>}
        <div className="ml-auto flex gap-2">
          {session && !connected && <button className="rounded border border-border px-3 py-1 text-xs" onClick={() => setRetry(value => value + 1)}>Reconnect</button>}
          {connected && canControl && <button className="rounded border border-border px-3 py-1 text-xs" onClick={() => send({ type: controlling ? 'release_control' : 'take_control' })}>{controlling ? 'Return control to agent' : 'Take control'}</button>}
        </div>
      </div>
      {error && <p className="p-3 text-xs text-destructive" role="alert">{error}</p>}
      {tabs.length > 0 && <div className="flex gap-1 overflow-x-auto border-b border-border p-2" aria-label="Browser tabs">
        {tabs.map(tab => <button key={tab.tabId} disabled={!controlling || tab.active} aria-pressed={tab.active} title={controlling ? tab.url : 'Take control to switch tabs. Watch mode follows the agent.'} onClick={() => send({ type: 'switch_tab', tab: tab.tabId })} className={`max-w-52 shrink-0 truncate rounded px-3 py-1.5 text-xs ${tab.active ? 'bg-muted font-medium' : 'text-muted-foreground'} disabled:cursor-default`}>{tab.title || tab.url || tab.tabId}</button>)}
      </div>}
      {frame ? <div className="overflow-auto bg-muted/30 p-2">
        <img ref={screen} src={frame} alt="Live server browser viewport" draggable={false} tabIndex={controlling ? 0 : -1} className="mx-auto block h-auto max-h-[65vh] max-w-full select-none rounded outline-none focus:ring-2 focus:ring-ring" onMouseDown={event => mouse(event, 'mousePressed')} onMouseUp={event => mouse(event, 'mouseReleased')} onMouseMove={event => mouse(event, 'mouseMoved')} onContextMenu={event => event.preventDefault()} onKeyDown={event => keyboard(event, 'keyDown')} onKeyUp={event => keyboard(event, 'keyUp')} />
      </div> : <div className="px-6 py-12 text-center text-sm text-muted-foreground">{session ? 'Waiting for the browser’s live view…' : 'When this workflow opens a managed browser, its live view will appear here.'}</div>}
      <p className="border-t border-border px-3 py-2 text-xs text-muted-foreground">{controlling ? 'Browser automation is paused while you interact. Return control or press Escape to let it continue.' : 'Watch mode follows the agent’s active tab. Take control to click, type, scroll, or switch tabs.'}</p>
    </section>
  )
}
