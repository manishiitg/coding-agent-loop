import { useEffect, useRef, useState } from 'react'
import type { KeyboardEvent, MouseEvent, ReactNode } from 'react'
import { CheckCircle2, Circle, Copy, FolderOpen, Loader2, Square, X } from 'lucide-react'
import api, { getApiBaseUrl, getAuthToken } from '../../services/api'
import { useWorkflowStore } from '../../stores/useWorkflowStore'
import { useChatStore } from '../../stores/useChatStore'
import { useCanWriteWorkflow } from '../../hooks/useCanWriteWorkflow'

const PLAYWRIGHT_BROWSER = 'playwright-tests'

type Recording = { recording: boolean; directory?: string; errors?: string[] }

type BrowserSession = { browser_session: string; workflow_session: string; label?: string; kind?: string; read_only?: string }
type BrowserTab = { tabId: string; title: string; url: string; active: boolean }

export default function WorkflowLiveBrowser({ workspacePath, toolbar }: { workspacePath: string | null; toolbar?: ReactNode }) {
  const [sessions, setSessions] = useState<BrowserSession[]>([])
  const [session, setSession] = useState('')
  const [selection, setSelection] = useState('')
  const selectedBrowser = useRef('')
  const [tabs, setTabs] = useState<BrowserTab[]>([])
  const [frame, setFrame] = useState('')
  const [lastPlaywrightFrame, setLastPlaywrightFrame] = useState<{ workspace: string; session: string; frame: string } | null>(null)
  const [connected, setConnected] = useState(false)
  const [controlling, setControlling] = useState(false)
  const [error, setError] = useState('')
  const [retry, setRetry] = useState(0)
  const [recording, setRecording] = useState<Recording>({ recording: false })
  const [recordingBusy, setRecordingBusy] = useState(false)
  const [dismissedRecording, setDismissedRecording] = useState('')
  const recordingNoticeKey = `browser-recording-dismissed:${workspacePath}:${session}`

  useEffect(() => {
    try { setDismissedRecording(sessionStorage.getItem(recordingNoticeKey) || '') }
    catch { setDismissedRecording('') }
  }, [recordingNoticeKey])

  function dismissRecordingNotice() {
    const directory = recording.directory || ''
    setDismissedRecording(directory)
    try { sessionStorage.setItem(recordingNoticeKey, directory) } catch { /* Dismissal still works without browser storage. */ }
  }

  async function copyRecordingPath() {
    if (!recording.directory) return
    try {
      await navigator.clipboard.writeText(recording.directory)
      useChatStore.getState().addToast('Recording path copied', 'success')
    } catch {
      useChatStore.getState().addToast('Unable to copy recording path', 'error')
    }
  }
  const [fit, setFit] = useState<'width' | 'page'>('width')
  const pendingTab = useRef('')
  const socket = useRef<WebSocket | null>(null)
  const viewport = useRef({ width: 1280, height: 720 })
  const screen = useRef<HTMLImageElement>(null)
  const canWrite = useCanWriteWorkflow(workspacePath)
  const followingPlaywright = selection === PLAYWRIGHT_BROWSER
  const currentBrowser = sessions.find(item => item.browser_session === session)
  const readOnly = followingPlaywright || currentBrowser?.read_only === 'true'
  const canControl = canWrite && !readOnly
  const retainedFrame = !connected && lastPlaywrightFrame?.workspace === workspacePath
    && (followingPlaywright || session.startsWith('pw-'))
    && (!session || session === lastPlaywrightFrame.session) ? lastPlaywrightFrame.frame : ''
  const displayFrame = frame || retainedFrame
  const completed = Boolean(retainedFrame && !session)

  useEffect(() => {
    let cancelled = false
    const controller = new AbortController()
    setSessions([])
    setSession('')
    setLastPlaywrightFrame(null)
    try { selectedBrowser.current = sessionStorage.getItem(`browser-selection:${workspacePath}`) || '' }
    catch { selectedBrowser.current = '' }
    setSelection(selectedBrowser.current)
    let polling = false
    const poll = async () => {
      if (!workspacePath || polling) return
      polling = true
      try {
        const { data } = await api.get<{ sessions: BrowserSession[] }>('/api/browser/live/sessions', {
          params: { workspace_path: workspacePath }, signal: controller.signal, timeout: 5000,
        })
        if (cancelled) return
        const nextSessions = data.sessions ?? []
        setSessions(nextSessions)
        const tests = nextSessions.filter(item => item.kind === 'playwright')
        let choice = selectedBrowser.current
        if (choice && choice !== PLAYWRIGHT_BROWSER && !nextSessions.some(item => item.browser_session === choice)) {
          choice = choice.startsWith('pw-') ? PLAYWRIGHT_BROWSER : ''
        }
        // Follow tests as they start, including when the always-present shared
        // browser was selected by default. An explicit browser choice wins.
        if (!choice && tests.length) choice = PLAYWRIGHT_BROWSER
        selectedBrowser.current = choice
        setSelection(choice)
        setSession(current => choice === PLAYWRIGHT_BROWSER
          ? tests.find(item => item.browser_session === current)?.browser_session || tests[0]?.browser_session || ''
          : choice || nextSessions[0]?.browser_session || '')
      } catch {
        if (!cancelled) setError('Unable to load workflow browser sessions.')
      } finally { polling = false }
    }
    void poll()
    const timer = window.setInterval(() => { void poll() }, 1000)
    return () => { cancelled = true; controller.abort(); window.clearInterval(timer) }
  }, [workspacePath])

  useEffect(() => {
    pendingTab.current = ''
    setFrame(''); setTabs([]); setConnected(false); setControlling(false); setError('')
    if (!session || !workspacePath) return
    setLastPlaywrightFrame(previous => previous?.workspace === workspacePath && previous.session === session ? previous : null)
    const url = new URL(`${getApiBaseUrl() || window.location.origin}/api/browser/live/${encodeURIComponent(session)}/stream`)
    url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
    url.searchParams.set('workspace_path', workspacePath)
    const token = getAuthToken()
    if (token) url.searchParams.set('token', token)
    const ws = new WebSocket(url)
    socket.current = ws
    let disposed = false
    let receivedFrame = false
    ws.onopen = () => { if (!disposed) setConnected(true) }
    ws.onmessage = event => {
      if (disposed) return
      try {
        const message = JSON.parse(event.data)
        if (message.type === 'frame' && typeof message.data === 'string') {
          const nextFrame = `data:image/${message.data.startsWith('iVBOR') ? 'png' : 'jpeg'};base64,${message.data}`
          receivedFrame = true
          setFrame(nextFrame)
          if (session.startsWith('pw-')) setLastPlaywrightFrame({ workspace: workspacePath, session, frame: nextFrame })
          if (message.metadata?.deviceWidth > 0 && message.metadata?.deviceHeight > 0) {
            viewport.current = { width: message.metadata.deviceWidth, height: message.metadata.deviceHeight }
          }
        } else if (message.type === 'tabs' && Array.isArray(message.tabs)) {
          setTabs(message.tabs)
        } else if (message.type === 'viewer_control') {
          setControlling(message.controlling === true)
          setError('')
          if (message.controlling) {
            screen.current?.focus()
            if (pendingTab.current) { ws.send(JSON.stringify({ type: 'switch_tab', tab: pendingTab.current })); pendingTab.current = '' }
          }
          else screen.current?.blur()
        } else if (message.type === 'viewer_error') {
          pendingTab.current = ''
          setError(message.message)
        }
      } catch { /* Ignore unsupported runtime messages. */ }
    }
    ws.onclose = () => {
      if (!disposed) { setConnected(false); setControlling(false); setFrame(''); setError(session.startsWith('pw-') && receivedFrame ? '' : session.startsWith('pw-') ? 'Playwright test browser disconnected or finished. Running test browsers appear automatically.' : 'Live view disconnected. Reconnect to continue watching. If it persists, check that the server has a streaming-capable agent-browser version.') }
    }
    const heartbeat = window.setInterval(() => {
      if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: 'ping' }))
    }, 10000)
    return () => { disposed = true; window.clearInterval(heartbeat); ws.close(); if (socket.current === ws) socket.current = null }
  }, [session, workspacePath, retry])

  useEffect(() => {
    setRecording({ recording: false })
    if (!session || !workspacePath || readOnly) return
    let cancelled = false
    const poll = async () => {
      try {
        const { data } = await api.post<Recording>(`/api/browser/live/${encodeURIComponent(session)}/recording`, { action: 'status' }, { params: { workspace_path: workspacePath }, timeout: 10000 })
        if (!cancelled) setRecording(data)
      } catch { /* Keep the stream usable when recording status is unavailable. */ }
    }
    void poll()
    const timer = window.setInterval(() => { void poll() }, 10000)
    return () => { cancelled = true; window.clearInterval(timer) }
  }, [session, workspacePath, readOnly])

  function chooseBrowser(choice: string) {
    selectedBrowser.current = choice
    setSelection(choice)
    try { sessionStorage.setItem(`browser-selection:${workspacePath}`, choice) } catch { /* Selection still works in memory. */ }
    setSession(choice === PLAYWRIGHT_BROWSER ? sessions.find(item => item.kind === 'playwright')?.browser_session || '' : choice)
  }

  async function toggleRecording() {
    if (recordingBusy) return
    setRecordingBusy(true); setError('')
    try {
      const { data } = await api.post<Recording>(`/api/browser/live/${encodeURIComponent(session)}/recording`, { action: recording.recording ? 'stop' : 'start' }, { params: { workspace_path: workspacePath }, timeout: 105000 })
      setRecording(data)
      if (data.errors?.length) setError(`Recording saved with issues: ${data.errors.join('; ')}`)
      else if (!data.recording && data.directory) useChatStore.getState().addToast(`Recording saved to ${data.directory}`, 'success')
    } catch (cause) {
      const response = (cause as { response?: { data?: { error?: string } } }).response
      setError(response?.data?.error || 'Recording request failed. Check its status before retrying.')
    } finally { setRecordingBusy(false) }
  }

  const hasFrame = Boolean(displayFrame)
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
    if (message.type === 'take_control') chooseBrowser(session)
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
    <section className="flex min-h-0 flex-1 flex-col overflow-hidden bg-background" aria-label="Live workflow browser">
      <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-border px-3 py-2">
        <h3 className="text-sm font-medium">Browser</h3>
        <span className="text-xs text-muted-foreground" role="status">{controlling ? 'You have control' : connected ? 'Watching' : completed ? 'Completed' : retainedFrame ? 'Disconnected' : 'Not connected'}</span>
        <select className="min-w-0 max-w-64 rounded border border-border bg-background p-1 text-xs" aria-label="Browser session" value={selection || session} onChange={event => chooseBrowser(event.target.value)}>
          {!selection && !session && <option value="" disabled>No managed browser</option>}
          <option value={PLAYWRIGHT_BROWSER}>Playwright tests</option>
          {sessions.map((item, index) => <option key={item.browser_session} value={item.browser_session}>{item.label || `Browser ${index + 1} · ${item.workflow_session.slice(0, 8)}`}</option>)}
        </select>
        {followingPlaywright && currentBrowser?.label && <span className="max-w-64 truncate text-xs text-muted-foreground" title={currentBrowser.label}>{currentBrowser.label}</span>}
        <div className="ml-auto flex gap-2">
          {session && !connected && <button className="rounded border border-border px-3 py-1 text-xs" onClick={() => setRetry(value => value + 1)}>Reconnect</button>}
          {connected && canControl && <button className="rounded border border-border px-3 py-1 text-xs" onClick={() => send({ type: controlling ? 'release_control' : 'take_control' })}>{controlling ? 'Return control to agent' : 'Take control'}</button>}
          {session && canControl && (
            <button type="button" disabled={recordingBusy} className={`inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50 ${recording.recording ? 'bg-red-500/10 text-red-600 hover:bg-red-500/15 dark:text-red-400' : 'bg-muted text-foreground hover:bg-muted/70'}`} onClick={() => void toggleRecording()}>
              {recordingBusy ? <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden="true" /> : recording.recording ? <Square className="h-3 w-3 fill-current" aria-hidden="true" /> : <Circle className="h-3 w-3 fill-red-500 text-red-500" aria-hidden="true" />}
              {recordingBusy ? recording.recording ? 'Saving recording…' : 'Starting recording…' : recording.recording ? 'Stop recording' : 'Start recording'}
            </button>
          )}
          <select aria-label="Browser sizing" value={fit} onChange={event => setFit(event.target.value as 'width' | 'page')} className="rounded border border-border bg-background px-1 text-xs"><option value="width">Fill width</option><option value="page">Fit page</option></select>
          {toolbar}
        </div>
      </div>
      {!recording.recording && recording.directory && recording.directory !== dismissedRecording && (
        <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-border bg-muted/30 px-3 py-2 text-xs">
          <CheckCircle2 className="h-4 w-4 shrink-0 text-emerald-600 dark:text-emerald-400" aria-hidden="true" />
          <span className="min-w-0 flex-1 font-medium" title={recording.directory}>Recording saved</span>
          <div className="ml-auto flex items-center gap-1">
            <button type="button" className="inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" onClick={() => void copyRecordingPath()}>
              <Copy className="h-4 w-4" aria-hidden="true" />
              Copy path
            </button>
            <button type="button" className="inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" onClick={() => useWorkflowStore.getState().openWorkspaceView('files', `${recording.directory}/manifest.json`)}>
              <FolderOpen className="h-4 w-4" aria-hidden="true" />
              Open recording files
            </button>
            <button type="button" aria-label="Dismiss recording notification" title="Dismiss" className="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" onClick={dismissRecordingNotice}>
              <X className="h-4 w-4" aria-hidden="true" />
            </button>
          </div>
        </div>
      )}
      {error && <p className="p-3 text-xs text-destructive" role="alert">{error}</p>}
      {tabs.length > 0 && <div className="flex shrink-0 gap-1 overflow-x-auto border-b border-border px-2 py-1" aria-label="Browser tabs">
        {tabs.map(tab => <button key={tab.tabId} disabled={!canControl || tab.active} aria-pressed={tab.active} title={controlling ? tab.url : 'Switch tab and take control'} onClick={() => { if (controlling) send({ type: 'switch_tab', tab: tab.tabId }); else { pendingTab.current = tab.tabId; send({ type: 'take_control' }) } }} className={`max-w-52 shrink-0 truncate rounded px-3 py-1.5 text-xs ${tab.active ? 'bg-muted font-medium' : 'text-muted-foreground'} disabled:cursor-default`}>{tab.title || tab.url || tab.tabId}</button>)}
      </div>}
      {displayFrame ? <div className="relative min-h-0 flex-1 bg-muted/20"><div className={`absolute inset-0 ${fit === 'width' ? 'overflow-auto' : 'flex items-center justify-center'}`}>
        <img ref={screen} src={displayFrame} alt={retainedFrame ? "Last Playwright test frame" : "Live server browser viewport"} draggable={false} tabIndex={controlling ? 0 : -1} className={`block h-auto select-none outline-none focus:ring-2 focus:ring-inset focus:ring-ring ${fit === 'width' ? 'w-full max-w-none' : 'max-h-full w-auto max-w-full'}`} onMouseDown={event => mouse(event, 'mousePressed')} onMouseUp={event => mouse(event, 'mouseReleased')} onMouseMove={event => mouse(event, 'mouseMoved')} onContextMenu={event => event.preventDefault()} onKeyDown={event => keyboard(event, 'keyDown')} onKeyUp={event => keyboard(event, 'keyUp')} />
      </div>{retainedFrame && <span className="pointer-events-none absolute bottom-3 right-3 rounded bg-background/90 px-3 py-1 text-xs shadow">{completed ? 'Completed' : 'Disconnected'} · Last frame</span>}</div> : <div className="flex min-h-0 flex-1 items-center justify-center px-6 py-12 text-center text-sm text-muted-foreground">{session ? 'Waiting for the browser’s live view…' : followingPlaywright ? 'Waiting for a Playwright test. Tests using the AgentWorks fixture will appear here automatically.' : 'When this workflow opens a managed browser, its live view will appear here.'}</div>}
      <p className="shrink-0 border-t border-border px-3 py-1 text-[11px] text-muted-foreground">{readOnly ? 'Playwright test · Watch-only. Recordings are saved by the test runner.' : session === 'shared-browser' ? 'Shared browser · everyone uses the same tabs and sign-ins. Coordinate before making changes.' : controlling ? 'Browser automation is paused while you interact. Return control or press Escape to let it continue.' : 'Live server browser · Take control to interact.'}</p>
    </section>
  )
}
