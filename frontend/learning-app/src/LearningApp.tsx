import { useState, useEffect, useRef, type FormEvent, type ChangeEvent, type ReactNode } from 'react'
import {
  ArrowLeft,
  ArrowRight,
  BookOpen,
  Check,
  CheckCircle2,
  CircleUserRound,
  ExternalLink,
  FileText,
  LockKeyhole,
  PanelLeftClose,
  PanelLeftOpen,
  PanelRightClose,
  PanelRightOpen,
  Paperclip,
  Plus,
  Send,
  Sparkles,
  Sun,
} from 'lucide-react'
import './learning-app.css'

type Screen = 'engine' | 'child' | 'pin' | 'parent' | 'tutor'
type DrawerTab = 'assets' | 'map' | 'progress' | 'files'

const FAMILY_API = (import.meta as { env?: { VITE_FAMILY_API?: string } }).env?.VITE_FAMILY_API ?? 'http://127.0.0.1:8010'

type ApiEngine = {
  id: string
  name: string
  runtime_command?: string
  runtime_available: boolean
  auth_configured: boolean
  usable: boolean
  setup_hint?: string
  deprecated?: boolean
}

function engineStatus(e: ApiEngine): { label: string; ready: boolean } {
  if (e.usable) return { label: 'Ready', ready: true }
  if (!e.runtime_available) return { label: 'Not set up', ready: false }
  if (!e.auth_configured) return { label: 'Needs sign-in', ready: false }
  return { label: 'Unavailable', ready: false }
}

// Parent-friendly presentation, keyed by the technical engine id.
// Order reflects the product preference: ChatGPT → Claude → Cursor → Pi.
const ENGINE_PRESENTATION: Record<string, { name: string; blurb: string; order: number; preferred?: boolean }> = {
  'codex-cli': { name: 'ChatGPT', blurb: 'Uses your ChatGPT account · can also create images', order: 1, preferred: true },
  'claude-code': { name: 'Claude', blurb: 'Careful, step-by-step teaching from Anthropic', order: 2 },
  'cursor-cli': { name: 'Cursor', blurb: 'Cursor’s AI assistant', order: 3 },
  'pi-cli': { name: 'Pi', blurb: 'Access OpenRouter and many other models', order: 4 },
}
function pres(id: string, fallbackName: string) {
  return ENGINE_PRESENTATION[id] ?? { name: fallbackName, blurb: 'Available on this computer', order: 99, preferred: false }
}

// Child profile options — edit here to adjust the setup form.
// Targeting grades 6–12, with 4–5 also offered.
const GRADES = ['4', '5', '6', '7', '8', '9', '10', '11', '12']
const BOARDS = ['CBSE', 'ICSE / ISC (CISCE)', 'State Board', 'NIOS', 'IB', 'Cambridge / IGCSE', 'Other', 'Not sure']

type ConvMeta = { id: string; title: string; when: string; scope: 'parent' | 'child'; updated: string }

// Convert an ISO timestamp into a short relative label for the rail.
function relTime(iso: string): string {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return ''
  const s = Math.max(0, (Date.now() - t) / 1000)
  if (s < 60) return 'Just now'
  if (s < 3600) return `${Math.floor(s / 60)}m ago`
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`
  return new Date(t).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}

function newConversationId(): string {
  return `conv-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`
}

// mdInline renders **bold** and `code` spans within a line.
function mdInline(text: string): ReactNode[] {
  const out: ReactNode[] = []
  const re = /(\*\*([^*]+)\*\*|`([^`]+)`)/g
  let last = 0
  let k = 0
  let m: RegExpExecArray | null
  while ((m = re.exec(text)) !== null) {
    if (m.index > last) out.push(text.slice(last, m.index))
    if (m[2] !== undefined) out.push(<strong key={k++}>{m[2]}</strong>)
    else out.push(<code key={k++}>{m[3]}</code>)
    last = m.index + m[0].length
  }
  if (last < text.length) out.push(text.slice(last))
  return out
}

// Markdown is a small, dependency-free renderer for the subset the agent emits:
// paragraphs, bullet + numbered lists, headings, bold and inline code. Keeps chat
// replies readable instead of a wall of raw markdown text.
function Markdown({ text }: { text: string }) {
  const lines = (text || '').replace(/\r/g, '').split('\n')
  const blocks: ReactNode[] = []
  let i = 0
  let key = 0
  while (i < lines.length) {
    if (lines[i].trim() === '') { i++; continue }
    const h = lines[i].match(/^(#{1,6})\s+(.*)/)
    if (h) { blocks.push(<p key={key++} className="md-h">{mdInline(h[2])}</p>); i++; continue }
    if (/^\s*[-*]\s+/.test(lines[i])) {
      const items: ReactNode[] = []
      let j = 0
      while (i < lines.length && /^\s*[-*]\s+/.test(lines[i])) { items.push(<li key={j++}>{mdInline(lines[i].replace(/^\s*[-*]\s+/, ''))}</li>); i++ }
      blocks.push(<ul key={key++}>{items}</ul>); continue
    }
    if (/^\s*\d+\.\s+/.test(lines[i])) {
      const items: ReactNode[] = []
      let j = 0
      while (i < lines.length && /^\s*\d+\.\s+/.test(lines[i])) { items.push(<li key={j++}>{mdInline(lines[i].replace(/^\s*\d+\.\s+/, ''))}</li>); i++ }
      blocks.push(<ol key={key++}>{items}</ol>); continue
    }
    const para: string[] = []
    while (i < lines.length && lines[i].trim() !== '' && !/^\s*[-*]\s+/.test(lines[i]) && !/^\s*\d+\.\s+/.test(lines[i]) && !/^#{1,6}\s/.test(lines[i])) { para.push(lines[i]); i++ }
    blocks.push(<p key={key++}>{mdInline(para.join(' '))}</p>)
  }
  return <>{blocks}</>
}

type ParentMsg = { role: 'user' | 'assistant' | 'tool'; text?: string; tool?: string; subject?: string; topic?: string; name?: string }
type TreeNode = { name: string; path: string; type: 'dir' | 'file'; children?: TreeNode[] }

// FileTree renders the workspace as an expandable tree (AgentWorks-style). Files
// are clickable to open in the viewer; .meta.json is hidden as noise.
function FileTree({ nodes, onOpen, depth = 0 }: { nodes: TreeNode[]; onOpen: (path: string) => void; depth?: number }) {
  const visible = nodes.filter((n) => !n.name.startsWith('.') && !n.name.endsWith('.meta.json'))
  if (visible.length === 0) return null
  return (
    <ul className="fl-tree">
      {visible.map((n) => (
        <li key={n.path}>
          {n.type === 'dir' ? (
            <details open={depth < 1}>
              <summary className="fl-tree-dir">{n.name}</summary>
              {n.children && <FileTree nodes={n.children} onOpen={onOpen} depth={depth + 1} />}
            </details>
          ) : (
            <button className="fl-tree-file" type="button" onClick={() => onOpen(n.path)}>{n.name}</button>
          )}
        </li>
      ))}
    </ul>
  )
}
type WsFile = { path: string; name: string; scope: string; subject: string; topic: string }

export default function LearningApp() {
  const [screen, setScreen] = useState<Screen>('engine')
  const [engines, setEngines] = useState<ApiEngine[]>([])
  const [enginesState, setEnginesState] = useState<'loading' | 'ready' | 'error'>('loading')
  const [engine, setEngine] = useState('')
  const [testState, setTestState] = useState<'idle' | 'testing' | 'valid' | 'invalid'>('idle')
  const [testMessage, setTestMessage] = useState('')

  useEffect(() => {
    let cancelled = false
    setEnginesState('loading')
    fetch(`${FAMILY_API}/api/engines`)
      .then((res) => { if (!res.ok) throw new Error(String(res.status)); return res.json() })
      .then((data: ApiEngine[]) => {
        if (cancelled) return
        const sorted = [...data].sort((a, b) => pres(a.id, a.name).order - pres(b.id, b.name).order)
        setEngines(sorted)
        const firstReady = sorted.find((item) => item.usable) ?? sorted[0]
        if (firstReady) setEngine(firstReady.id)
        setEnginesState('ready')
      })
      .catch(() => { if (!cancelled) setEnginesState('error') })
    return () => { cancelled = true }
  }, [])
  const [childName, setChildName] = useState('Maya')
  const [grade, setGrade] = useState('10')
  const [board, setBoard] = useState('CBSE')
  const [subject, setSubject] = useState('')
  const [topic, setTopic] = useState('')
  const [focusInput, setFocusInput] = useState('')
  const [parentMessages, setParentMessages] = useState<ParentMsg[]>([])
  const [sending, setSending] = useState(false)
  const [wsFiles, setWsFiles] = useState<WsFile[]>([])
  const [allFiles, setAllFiles] = useState<string[]>([])
  const [conversationId, setConversationId] = useState(newConversationId)
  const [conversations, setConversations] = useState<ConvMeta[]>([])
  const [childSessionsList, setChildSessionsList] = useState<ConvMeta[]>([])
  const resumedRef = useRef(false)
  const [childMessages, setChildMessages] = useState<ParentMsg[]>([])
  const [childSending, setChildSending] = useState(false)
  const [childInput, setChildInput] = useState('')
  const [childConversationId, setChildConversationId] = useState(newConversationId)

  // Reflect the workspace file system in the drawer (materials the agent can
  // read). Refetches when entering the chat and after each upload/tool event.
  useEffect(() => {
    if (screen !== 'parent') return
    let cancelled = false
    fetch(`${FAMILY_API}/api/workspace/tree`)
      .then((res) => res.json())
      .then(async (nodes: TreeNode[]) => {
        if (cancelled) return
        const files: { path: string; name: string }[] = []
        const walk = (ns: TreeNode[]) => ns?.forEach((n) => {
          if (n.type === 'file') files.push({ path: n.path, name: n.name })
          if (n.children) walk(n.children)
        })
        walk(nodes)
        if (!cancelled) setTreeNodes(nodes)
        const mats: WsFile[] = files
          .filter((f) => f.path.includes('/materials/') && !f.name.endsWith('.meta.json'))
          .map((f) => {
            const parts = f.path.split('/')
            const mi = parts.indexOf('materials')
            return { path: f.path, name: f.name, scope: parts[0] || '', subject: parts[mi + 1] || '', topic: parts[mi + 2] || '' }
          })
        if (!cancelled) { setWsFiles(mats); setAllFiles(files.map((f) => f.path)) }
        // Derive past conversations from <scope>/conversations/*.json — the left
        // rail reflects the file system, no bespoke conversations API.
        const convPaths = files.filter((f) => f.path.includes('/conversations/') && f.path.endsWith('.json')).map((f) => f.path)
        const metas = await Promise.all(convPaths.map(async (p) => {
          try {
            const d = await fetch(`${FAMILY_API}/api/workspace/file?path=${encodeURIComponent(p)}`).then((r) => r.json())
            const c = JSON.parse(d.content) as { id: string; title?: string; updated_at?: string }
            const scope: 'parent' | 'child' = p.startsWith('child/') ? 'child' : 'parent'
            return { id: c.id, title: c.title || 'Conversation', when: relTime(c.updated_at || ''), scope, updated: c.updated_at || '' } as ConvMeta
          } catch { return null }
        }))
        if (cancelled) return
        const valid = metas.filter((m): m is ConvMeta => m !== null)
        valid.sort((a, b) => b.updated.localeCompare(a.updated))
        const parentConvs = valid.filter((c) => c.scope === 'parent')
        setConversations(parentConvs)
        setChildSessionsList(valid.filter((c) => c.scope === 'child'))
        // Resume the most recent conversation by default (once) so the parent
        // continues where they left off instead of starting a fresh chat each load.
        if (!resumedRef.current && parentMessages.length === 0 && parentConvs.length > 0) {
          resumedRef.current = true
          const top = parentConvs[0]
          fetch(`${FAMILY_API}/api/workspace/file?path=${encodeURIComponent(`${top.scope}/conversations/${top.id}.json`)}`)
            .then((r) => r.json())
            .then((dd) => {
              const c = JSON.parse(dd.content) as { id: string; messages?: { role: string; text: string }[] }
              setConversationId(c.id)
              setParentMessages((c.messages || []).map((mm) => ({ role: mm.role as ParentMsg['role'], text: mm.text })))
            })
            .catch(() => {})
        }
      })
      .catch(() => {})
    return () => { cancelled = true }
  }, [screen, parentMessages.length])
  const [signoff, setSignoff] = useState(false)
  const [railOpen, setRailOpen] = useState(false)
  const [drawerOpen, setDrawerOpen] = useState(true)
  const [drawerTab, setDrawerTab] = useState<DrawerTab>('assets')
  const [treeNodes, setTreeNodes] = useState<TreeNode[]>([])
  const [viewerPath, setViewerPath] = useState<string | null>(null)
  const [viewerContent, setViewerContent] = useState<{ isText: boolean; content: string } | null>(null)
  const [booting, setBooting] = useState(true)
  const [bootError, setBootError] = useState(false)
  const [pin, setPin] = useState('')
  const [pinConfirm, setPinConfirm] = useState('')
  const [pinError, setPinError] = useState('')
  const [saving, setSaving] = useState(false)

  // Load the selected file for the drawer's Files viewer.
  useEffect(() => {
    if (!viewerPath) { setViewerContent(null); return }
    let cancelled = false
    setViewerContent(null)
    fetch(`${FAMILY_API}/api/workspace/file?path=${encodeURIComponent(viewerPath)}`)
      .then((r) => r.json())
      .then((d) => { if (!cancelled) setViewerContent({ isText: !!d.is_text, content: d.content ?? '' }) })
      .catch(() => { if (!cancelled) setViewerContent({ isText: false, content: '' }) })
    return () => { cancelled = true }
  }, [viewerPath])

  // On launch, ask family-server where onboarding stands. If setup is complete
  // we land straight in the chat; otherwise resume at the right step.
  useEffect(() => {
    let cancelled = false
    const load = (attempt: number) => {
      fetch(`${FAMILY_API}/api/setup`)
        .then((res) => { if (!res.ok) throw new Error(String(res.status)); return res.json() })
        .then((data: { next_step?: string; engine?: string; subject?: string; topic?: string; child?: { name?: string; grade?: string; board?: string } | null }) => {
          if (cancelled) return
          if (data.engine) setEngine(data.engine)
          if (data.subject) setSubject(data.subject)
          if (data.topic) setTopic(data.topic)
          if (data.child) {
            if (data.child.name) setChildName(data.child.name)
            if (data.child.grade) setGrade(data.child.grade)
            if (data.child.board) setBoard(data.child.board)
          }
          const step = data.next_step
          if (step === 'done') setScreen('parent')
          else if (step === 'pin') setScreen('pin')
          else if (step === 'child') setScreen('child')
          else setScreen('engine')
          setBooting(false)
        })
        .catch(() => {
          if (cancelled) return
          // Never fall back to onboarding on a transient failure — retry, then
          // show an explicit error so completed setup is not lost visually.
          if (attempt < 4) window.setTimeout(() => load(attempt + 1), 500)
          else { setBootError(true); setBooting(false) }
        })
    }
    load(0)
    return () => { cancelled = true }
  }, [])

  const selectedEngine = engines.find((item) => item.id === engine)
  const initial = childName.trim().slice(0, 1).toUpperCase() || 'M'

  const runTest = () => {
    if (!selectedEngine) return
    setTestState('testing')
    setTestMessage('')
    fetch(`${FAMILY_API}/api/engines/validate`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ provider: selectedEngine.id, model_id: '' }),
    })
      .then((res) => res.json())
      .then((data: { valid: boolean; message?: string }) => {
        setTestState(data.valid ? 'valid' : 'invalid')
        setTestMessage(data.message ?? (data.valid ? 'Connection works.' : 'Test failed.'))
      })
      .catch(() => {
        setTestState('invalid')
        setTestMessage('Could not run the test.')
      })
  }

  const move = (next: Screen) => {
    setSignoff(false)
    setScreen(next)
  }

  const persistEngineAndContinue = () => {
    if (!selectedEngine) return
    setSaving(true)
    fetch(`${FAMILY_API}/api/engine/selection`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ engine: selectedEngine.id }),
    }).finally(() => { setSaving(false); move('child') })
  }

  const createChildAndContinue = () => {
    if (!childName.trim()) return
    setSaving(true)
    fetch(`${FAMILY_API}/api/child`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: childName, grade, board }),
    }).finally(() => { setSaving(false); move('pin') })
  }

  const savePinAndContinue = () => {
    setPinError('')
    if (!/^\d{4,8}$/.test(pin)) { setPinError('Use 4–8 digits.'); return }
    if (pin !== pinConfirm) { setPinError('The two PINs don’t match.'); return }
    setSaving(true)
    fetch(`${FAMILY_API}/api/parent/pin`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ pin }),
    })
      .then((res) => res.json())
      .then((data: { error?: string }) => { if (data.error) { setPinError(data.error); return } move('parent') })
      .catch(() => setPinError('Could not save the PIN.'))
      .finally(() => setSaving(false))
  }

  const sendParentMessage = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const text = focusInput.trim()
    if (!text || sending) return
    const next: ParentMsg[] = [...parentMessages, { role: 'user', text }]
    setParentMessages(next)
    setFocusInput('')
    setSending(true)
    const history = next.filter((m) => m.role === 'user' || m.role === 'assistant').map((m) => ({ role: m.role, text: m.text ?? '' }))
    fetch(`${FAMILY_API}/api/parent/message`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ messages: history, conversation_id: conversationId }),
    })
      .then((res) => res.json())
      .then((data: { reply?: string; error?: string; tool_events?: { tool: string; subject?: string; topic?: string }[] }) => {
        const toolMsgs: ParentMsg[] = (data.tool_events ?? []).map((e) => ({ role: 'tool', tool: e.tool, subject: e.subject, topic: e.topic }))
        const st = (data.tool_events ?? []).find((e) => e.tool === 'set_subject_topic')
        if (st) { if (st.subject) setSubject(st.subject); if (st.topic) setTopic(st.topic) }
        setParentMessages((cur) => [...cur, ...toolMsgs, { role: 'assistant', text: data.error ? `Sorry — ${data.error}` : (data.reply || '(no response)') }])
      })
      .catch(() => setParentMessages((cur) => [...cur, { role: 'assistant', text: 'Sorry — I couldn’t reach the learning engine.' }]))
      .finally(() => setSending(false))
  }

  // Load a past conversation into the chat view (reads the transcript file).
  const loadConversation = (item: ConvMeta) => {
    fetch(`${FAMILY_API}/api/workspace/file?path=${encodeURIComponent(`${item.scope}/conversations/${item.id}.json`)}`)
      .then((r) => r.json())
      .then((d) => {
        const c = JSON.parse(d.content) as { id: string; messages?: { role: string; text: string }[] }
        setConversationId(c.id)
        setParentMessages((c.messages || []).map((m) => ({ role: m.role as ParentMsg['role'], text: m.text })))
      })
      .catch(() => {})
  }

  const startNewConversation = () => {
    resumedRef.current = true
    setConversationId(newConversationId())
    setParentMessages([])
  }

  // Child Mode tutor — talks to /api/child/message (sandboxed child agent).
  const sendChildText = (raw: string) => {
    const text = raw.trim()
    if (!text || childSending) return
    const next: ParentMsg[] = [...childMessages, { role: 'user', text }]
    setChildMessages(next)
    setChildInput('')
    setChildSending(true)
    const history = next.filter((m) => m.role === 'user' || m.role === 'assistant').map((m) => ({ role: m.role, text: m.text ?? '' }))
    fetch(`${FAMILY_API}/api/child/message`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ messages: history, conversation_id: childConversationId }),
    })
      .then((res) => res.json())
      .then((data: { reply?: string; error?: string }) => {
        setChildMessages((cur) => [...cur, { role: 'assistant', text: data.error ? `Hmm, something went wrong — ${data.error}` : (data.reply || '(no response)') }])
      })
      .catch(() => setChildMessages((cur) => [...cur, { role: 'assistant', text: 'I couldn’t reach the tutor just now — try again in a moment.' }]))
      .finally(() => setChildSending(false))
  }

  const sendChildMessage = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    sendChildText(childInput)
  }

  const fileInputRef = useRef<HTMLInputElement>(null)
  const [uploading, setUploading] = useState(false)

  const onPickFiles = () => fileInputRef.current?.click()

  const onFilesSelected = (event: ChangeEvent<HTMLInputElement>) => {
    const files = event.target.files
    if (!files || files.length === 0) return
    setUploading(true)
    const jobs = Array.from(files).map((f) => {
      const fd = new FormData()
      fd.append('file', f)
      fd.append('scope', 'shared')
      if (subject) fd.append('subject', subject)
      if (topic) fd.append('topic', topic)
      return fetch(`${FAMILY_API}/api/upload`, { method: 'POST', body: fd })
        .then((res) => res.json())
        .then((data: { name?: string; error?: string }) => ({ name: data.name || f.name, error: data.error }))
        .catch(() => ({ name: f.name, error: 'upload failed' }))
    })
    Promise.all(jobs)
      .then((results) => {
        const cards: ParentMsg[] = results.map((r) => ({ role: 'tool', tool: r.error ? 'upload_error' : 'upload', name: r.name }))
        setParentMessages((cur) => [...cur, ...cards])
      })
      .finally(() => {
        setUploading(false)
        if (fileInputRef.current) fileInputRef.current.value = ''
      })
  }

  if (booting) {
    return (
      <main className="learning-app">
        <div className="fl-boot"><img src="/sparkquill-loader.svg" alt="" width={76} height={76} /><p>Starting SparkQuill…</p></div>
      </main>
    )
  }

  if (bootError) {
    return (
      <main className="learning-app">
        <div className="fl-boot">
          <img src="/sparkquill-mark.svg" alt="" width={64} height={64} />
          <p>Couldn’t reach SparkQuill on this computer.</p>
          <button className="primary-button" type="button" onClick={() => window.location.reload()}>Try again</button>
        </div>
      </main>
    )
  }

  if (screen === 'parent') {
    return (
      <main className="learning-app">
        <div className="fl-shell" data-rail={railOpen ? 'open' : 'closed'} data-drawer={drawerOpen ? 'open' : 'closed'}>
          <aside className="fl-rail" aria-label="Conversations and sessions">
            <div className="fl-rail-brand">
              <img className="fl-rail-logo" src="/sparkquill-mark.svg" alt="" width={26} height={26} />
              <span className="brand-word">Spark<strong>Quill</strong></span>
            </div>
            <button className="fl-new" type="button" onClick={startNewConversation}><Plus size={17} /> New conversation</button>
            <div className="fl-rail-scroll">
              <div className="fl-rail-group">
                <p className="fl-rail-label">Parent conversations</p>
                {conversations.length === 0 && <p className="fl-rail-empty">No conversations yet</p>}
                {conversations.map((item) => (
                  <button key={item.id} type="button" className={`fl-rail-item ${item.id === conversationId ? 'is-active' : ''}`} onClick={() => loadConversation(item)}>
                    <span className="fl-rail-item-title">{item.title}</span>
                    <span className="fl-rail-item-when">{item.when}</span>
                  </button>
                ))}
              </div>
              <div className="fl-rail-group">
                <p className="fl-rail-label">Child sessions</p>
                {childSessionsList.length === 0 && <p className="fl-rail-empty">No sessions yet</p>}
                {childSessionsList.map((item) => (
                  <button key={item.id} type="button" className="fl-rail-item" onClick={() => loadConversation(item)}>
                    <span className="fl-rail-item-title">{item.title}</span>
                    <span className="fl-rail-item-when">{item.when}</span>
                  </button>
                ))}
              </div>
            </div>
            <div className="fl-rail-child">
              <span className="fl-child-avatar">{initial}</span>
              <span className="fl-child-meta"><strong>{childName || 'Your child'}</strong><small>Grade {grade} · {board}</small></span>
            </div>
          </aside>

          <section className="fl-center">
            <div className="fl-toolbar">
              <div className="fl-toolbar-left">
                <button
                  className="fl-icon-btn"
                  type="button"
                  aria-label={railOpen ? 'Hide conversation list' : 'Show conversation list'}
                  aria-pressed={railOpen}
                  onClick={() => setRailOpen((value) => !value)}
                >
                  {railOpen ? <PanelLeftClose size={18} /> : <PanelLeftOpen size={18} />}
                </button>
                <div className="fl-toolbar-title">
                  <strong>Parent Learning Guide</strong>
                  <span>{childName || 'Your child'}{subject ? ` · ${subject}` : ''}{topic ? ` · ${topic}` : ''}</span>
                </div>
              </div>
              <div className="fl-toolbar-right">
                <span className="learning-mode-pill"><CircleUserRound size={16} /> Parent Mode</span>
                <button
                  className="fl-icon-btn"
                  type="button"
                  aria-label={drawerOpen ? 'Hide workspace drawer' : 'Show workspace drawer'}
                  aria-pressed={drawerOpen}
                  onClick={() => setDrawerOpen((value) => !value)}
                >
                  {drawerOpen ? <PanelRightClose size={18} /> : <PanelRightOpen size={18} />}
                </button>
              </div>
            </div>

            <div className="fl-thread" aria-label="Parent learning conversation">
              <div className="fl-msg is-agent">
                <span className="fl-msg-avatar is-sun"><Sun size={18} /></span>
                <div className="fl-msg-col">
                  <div className="fl-bubble">Hi! I’m Quill, {childName || 'your child'}’s learning guide. Tell me what {childName || 'your child'} is working on, or ask me to explain progress, make study material, or create a test.</div>
                </div>
              </div>

              {parentMessages.map((m, i) => {
                if (m.role === 'tool') {
                  if (m.tool === 'upload' || m.tool === 'upload_error') {
                    const bad = m.tool === 'upload_error'
                    return (
                      <div key={i} className="fl-msg is-agent">
                        <span className="fl-msg-avatar is-sun"><Paperclip size={16} /></span>
                        <div className="fl-msg-col">
                          <div className={`fl-toolcard ${bad ? 'is-error' : 'is-upload'}`}><Paperclip size={15} /> <span>{bad ? <>Couldn’t add <strong>{m.name}</strong></> : <>Added material — <strong>{m.name}</strong></>}</span></div>
                        </div>
                      </div>
                    )
                  }
                  return (
                    <div key={i} className="fl-msg is-agent">
                      <span className="fl-msg-avatar is-sun"><Check size={18} strokeWidth={3} /></span>
                      <div className="fl-msg-col">
                        <div className="fl-toolcard"><Check size={15} strokeWidth={3} /> <span>Saved <strong>Subject &amp; Topic</strong> — {m.subject} · {m.topic}</span></div>
                      </div>
                    </div>
                  )
                }
                return (
                  <div key={i} className={`fl-msg ${m.role === 'user' ? 'is-parent' : 'is-agent'}`}>
                    {m.role === 'user' ? (
                      <>
                        <div className="fl-msg-col"><div className="fl-bubble">{m.text}</div></div>
                        <span className="fl-msg-avatar is-parent">{initial}</span>
                      </>
                    ) : (
                      <>
                        <span className="fl-msg-avatar is-sun"><Sun size={18} /></span>
                        <div className="fl-msg-col"><div className="fl-bubble"><Markdown text={m.text ?? ''} /></div></div>
                      </>
                    )}
                  </div>
                )
              })}

              {sending && (
                <div className="fl-msg is-agent">
                  <span className="fl-msg-avatar is-sun"><Sun size={18} /></span>
                  <div className="fl-msg-col">
                    <div className="fl-thinking"><img src="/sparkquill-loader.svg" alt="" width={38} height={38} /> <span>Quill is thinking…</span></div>
                  </div>
                </div>
              )}

              {parentMessages.length === 0 && !sending && (
                <div className="parent-quick-actions" aria-label="Suggested parent requests">
                  <button type="button" onClick={() => setFocusInput(`How is ${childName || 'my child'} doing so far?`)}>Understand progress</button>
                  <button type="button" onClick={() => setFocusInput('Make a short revision worksheet on the current topic')}>Create study material</button>
                  <button type="button" onClick={() => setFocusInput('Create a short practice test on the current topic')}>Create a test</button>
                </div>
              )}
            </div>

            <form className="fl-composer" onSubmit={sendParentMessage}>
              <input ref={fileInputRef} type="file" multiple accept="image/*,application/pdf" onChange={onFilesSelected} style={{ display: 'none' }} />
              <button className="composer-icon" type="button" aria-label="New conversation" title="Start a new conversation" onClick={startNewConversation} disabled={sending}><Plus size={19} /></button>
              <button className="composer-icon" type="button" aria-label="Attach a photo or PDF" onClick={onPickFiles} disabled={uploading}><Paperclip size={19} /></button>
              <input
                aria-label="Message the learning guide"
                placeholder={`Ask anything about ${childName || 'your child'}’s learning…`}
                value={focusInput}
                onChange={(event) => setFocusInput(event.target.value)}
                disabled={sending}
              />
              <button className="composer-send" type="submit" aria-label="Send message" disabled={!focusInput.trim() || sending}><Send size={18} /></button>
            </form>
            <p className="fl-disclaimer">SparkQuill can make mistakes. Please review important content before sharing it with {childName || 'your child'}.</p>
          </section>

          <aside className="fl-drawer" aria-label="Learning workspace">
            <div className="fl-drawer-head">
              <strong>{childName || 'Your child'}’s workspace</strong>
            </div>
            <div className="fl-drawer-tabs" role="tablist" aria-label="Workspace views">
              <button role="tab" aria-selected={drawerTab === 'assets'} className={drawerTab === 'assets' ? 'is-active' : ''} type="button" onClick={() => setDrawerTab('assets')}>Assets</button>
              <button role="tab" aria-selected={drawerTab === 'map'} className={drawerTab === 'map' ? 'is-active' : ''} type="button" onClick={() => setDrawerTab('map')}>Map</button>
              <button role="tab" aria-selected={drawerTab === 'progress'} className={drawerTab === 'progress' ? 'is-active' : ''} type="button" onClick={() => setDrawerTab('progress')}>Progress</button>
              <button role="tab" aria-selected={drawerTab === 'files'} className={drawerTab === 'files' ? 'is-active' : ''} type="button" onClick={() => setDrawerTab('files')}>Files</button>
            </div>

            <div className="fl-drawer-scroll">
              {drawerTab === 'assets' && (
                <>
                  <section className="fl-context-card">
                    <span className="tool-label"><Check size={15} strokeWidth={3} /> Subject &amp; Topic tool</span>
                    <dl>
                      <div><dt>Subject</dt><dd>{subject || 'Not set yet'}</dd></div>
                      <div><dt>Current topic</dt><dd>{topic || 'Not set yet'}</dd></div>
                    </dl>
                  </section>
                  {(() => {
                    if (wsFiles.length === 0) {
                      return <p className="fl-note">No materials yet. Use the attach button to add photos or PDFs — they’ll appear here, organized by subject and topic, for Quill to read.</p>
                    }
                    const groups: Record<string, WsFile[]> = {}
                    wsFiles.forEach((f) => { const k = f.subject || 'General'; (groups[k] = groups[k] || []).push(f) })
                    return Object.entries(groups).map(([subj, files]) => (
                      <section key={subj} className="fl-asset-group">
                        <p className="fl-drawer-label">{subj}</p>
                        {files.map((f) => (
                          <div key={f.path} className="fl-asset">
                            <span className="fl-asset-icon"><FileText size={17} /></span>
                            <span className="fl-asset-body"><strong>{f.name}</strong><small>{f.topic || 'material'}</small></span>
                          </div>
                        ))}
                      </section>
                    ))
                  })()}
                  <p className="fl-callout"><span className="fl-dot is-ready" /> Materials live in the family workspace on this computer. Quill reads them to explain progress and create study material.</p>
                </>
              )}

              {drawerTab === 'map' && (
                <>
                  <section className="fl-context-card">
                    <span className="tool-label"><Check size={15} strokeWidth={3} /> Subject &amp; Topic tool</span>
                    <dl>
                      <div><dt>Subject</dt><dd>{subject || 'Not set yet'}</dd></div>
                      <div><dt>Current topic</dt><dd>{topic || 'Not set yet'}</dd></div>
                    </dl>
                  </section>
                  <p className="fl-drawer-label">Academic map · living view</p>
                  {(() => {
                    const bySubject: Record<string, { topics: Set<string>; count: number }> = {}
                    wsFiles.forEach((f) => {
                      const s = f.subject || 'General'
                      const e = bySubject[s] || { topics: new Set<string>(), count: 0 }
                      if (f.topic) e.topics.add(f.topic)
                      e.count += 1
                      bySubject[s] = e
                    })
                    const entries = Object.entries(bySubject)
                    if (entries.length === 0) {
                      return <p className="fl-note">The map grows as you add materials — it starts from the confirmed subject and topic, nothing invented from grade or board alone.</p>
                    }
                    return entries.map(([name, v]) => (
                      <div key={name} className={`fl-map-subject ${name === subject ? 'is-current' : ''}`}>
                        <div className="fl-map-subject-head">
                          <strong>{name}</strong>
                          {name === subject && <span className="fl-badge is-current">Current</span>}
                        </div>
                        <div className="fl-map-topic">{v.topics.size ? `Topics: ${Array.from(v.topics).join(', ')}` : 'No topic yet'}</div>
                        <div className="fl-map-meta">{v.count} source{v.count === 1 ? '' : 's'}</div>
                      </div>
                    ))
                  })()}
                  <p className="fl-note">This map grows from real materials on this computer. It starts with the confirmed subject and topic — nothing is invented from grade or board alone.</p>
                </>
              )}

              {drawerTab === 'progress' && (() => {
                const materials = allFiles.filter((p) => p.includes('/materials/')).length
                const study = allFiles.filter((p) => p.includes('/study/')).length
                const tests = allFiles.filter((p) => p.includes('/tests/')).length
                const recent = [...conversations, ...childSessionsList].sort((a, b) => b.updated.localeCompare(a.updated)).slice(0, 6)
                return (
                  <>
                    <div className="fl-prog-focus">
                      <span className="fl-drawer-label">Current focus</span>
                      <strong>{subject ? `${subject}${topic ? ' · ' + topic : ''}` : 'Not set yet'}</strong>
                    </div>
                    <section className="fl-prog-group">
                      <p className="fl-drawer-label">Workspace snapshot</p>
                      <div className="fl-prog-item is-strong"><FileText size={16} /> {materials} material{materials === 1 ? '' : 's'} uploaded</div>
                      <div className="fl-prog-item is-strong"><FileText size={16} /> {study} study sheet{study === 1 ? '' : 's'} created</div>
                      <div className="fl-prog-item is-strong"><FileText size={16} /> {tests} practice test{tests === 1 ? '' : 's'} created</div>
                    </section>
                    <section className="fl-prog-group">
                      <p className="fl-drawer-label">Recent activity</p>
                      {recent.length === 0 && <p className="fl-note">No sessions yet.</p>}
                      {recent.map((item) => (
                        <div key={item.scope + item.id} className="fl-prog-recent">
                          <span className="fl-signal" data-signal={item.scope === 'child' ? 'mixed' : 'strong'} aria-hidden="true" />
                          <span className="fl-prog-recent-label">{item.scope === 'child' ? childName || 'Maya' : 'Parent'}: {item.title}</span>
                          <small>{item.when}</small>
                        </div>
                      ))}
                    </section>
                    <p className="fl-note">Built from real files and sessions on this computer — no numeric scores are invented. Ask Quill to review {childName || 'Maya'}’s work for a deeper read.</p>
                  </>
                )
              })()}

              {drawerTab === 'files' && (
                viewerPath ? (
                  <div className="fl-viewer">
                    <div className="fl-viewer-bar">
                      <button className="fl-viewer-back" type="button" onClick={() => setViewerPath(null)}><ArrowLeft size={15} /> Files</button>
                      <span className="fl-viewer-name">{viewerPath.split('/').pop()}</span>
                    </div>
                    {!viewerContent ? (
                      <p className="fl-note">Loading…</p>
                    ) : !viewerContent.isText ? (
                      <p className="fl-note">This file type can’t be previewed here.</p>
                    ) : (viewerPath.endsWith('.html') || viewerPath.endsWith('.htm')) ? (
                      <iframe className="fl-viewer-frame" title="File preview" sandbox="" srcDoc={viewerContent.content} />
                    ) : (viewerPath.endsWith('.md') || viewerPath.endsWith('.markdown')) ? (
                      <div className="fl-viewer-md"><Markdown text={viewerContent.content} /></div>
                    ) : (
                      <pre className="fl-viewer-pre">{viewerContent.content}</pre>
                    )}
                  </div>
                ) : (
                  treeNodes.length === 0
                    ? <p className="fl-note">No files yet.</p>
                    : <FileTree nodes={treeNodes} onOpen={(p) => setViewerPath(p)} />
                )
              )}
            </div>

            <div className="fl-drawer-foot">
              <button className="primary-button handoff-button" onClick={() => setSignoff(true)} type="button">Open child learning space <ArrowRight size={18} /></button>
            </div>
          </aside>

          {signoff && (
            <div className="fl-signoff-backdrop" role="dialog" aria-modal="true" aria-labelledby="fl-signoff-title">
              <div className="fl-signoff-card">
                <span className="fl-signoff-icon"><ArrowRight size={22} /></span>
                <h2 id="fl-signoff-title">Hand this device to {childName || 'your child'}?</h2>
                <p>Parent Mode will lock and {childName || 'your child'} will start in their learning space. You’ll need your PIN to return to Parent Mode.</p>
                <ul className="fl-signoff-list">
                  <li><Check size={15} strokeWidth={3} /> Parent notes, answer keys and drafts stay hidden</li>
                  <li><Check size={15} strokeWidth={3} /> {childName || 'Your child'} sees only material you approved</li>
                </ul>
                <div className="fl-signoff-actions">
                  <button className="fl-ghost-btn" type="button" onClick={() => setSignoff(false)}>Cancel</button>
                  <button className="primary-button" type="button" onClick={() => { setSignoff(false); setScreen('tutor') }}>Hand over to {childName || 'your child'} <ArrowRight size={18} /></button>
                </div>
              </div>
            </div>
          )}
        </div>
      </main>
    )
  }

  if (screen === 'tutor') {
    return (
      <main className="learning-app">
        <div className="fl-child">
          <header className="fl-child-top">
            <div className="fl-child-id">
              <span className="fl-child-avatar is-big">{initial}</span>
              <div className="fl-child-hi"><strong>Hi {childName || 'Maya'}!</strong><small>Let’s keep learning together</small></div>
            </div>
            <div className="fl-child-top-right">
              <span className="fl-today-pill"><Sparkles size={15} /> Today · {topic}</span>
              <button className="fl-parent-return" type="button" onClick={() => move('parent')}><LockKeyhole size={16} /> Parent Mode</button>
            </div>
          </header>
          <div className="fl-child-body">
            <section className="fl-child-chat">
              <div className="fl-child-thread" aria-label="Tutor conversation">
                <div className="fl-tmsg is-tutor">
                  <span className="fl-tmsg-avatar"><Sun size={20} /></span>
                  <div className="fl-tbubble">Hi {childName || 'Maya'}! Ready to keep going with {topic ? topic.toLowerCase() : 'your learning'}? Tell me what you’re working on, or ask me anything — I’ll help you figure it out step by step.</div>
                </div>
                {childMessages.map((m, i) => (
                  m.role === 'assistant' ? (
                    <div key={i} className="fl-tmsg is-tutor">
                      <span className="fl-tmsg-avatar"><Sun size={20} /></span>
                      <div className="fl-tbubble"><Markdown text={m.text ?? ''} /></div>
                    </div>
                  ) : (
                    <div key={i} className="fl-tmsg is-child">
                      <div className="fl-tbubble"><Markdown text={m.text ?? ''} /></div>
                      <span className="fl-tmsg-avatar is-child">{initial}</span>
                    </div>
                  )
                ))}
                {childSending && (
                  <div className="fl-tmsg is-tutor">
                    <span className="fl-tmsg-avatar"><Sun size={20} /></span>
                    <div className="fl-tbubble">Thinking…</div>
                  </div>
                )}
              </div>
              <div className="fl-child-actions">
                <button type="button" onClick={() => sendChildText('Can I have a hint?')} disabled={childSending}>Ask for a hint</button>
                <button type="button" onClick={() => sendChildText('Can you check my answer?')} disabled={childSending}>Check my answer</button>
                <button type="button" onClick={() => sendChildText('Can you explain it a different way?')} disabled={childSending}>Explain it differently</button>
              </div>
              <form className="fl-child-composer" onSubmit={sendChildMessage}>
                <input aria-label="Message your tutor" placeholder="Type your answer or ask for help…" value={childInput} onChange={(e) => setChildInput(e.target.value)} disabled={childSending} />
                <button className="composer-send" type="submit" aria-label="Send message" disabled={childSending}><Send size={18} /></button>
              </form>
            </section>
            <aside className="fl-child-side">
              <div className="fl-goal-card">
                <span className="fl-goal-label">Today’s goal</span>
                <strong>{topic}</strong>
                <div className="fl-goal-progress">
                  <div className="fl-goal-bar"><span style={{ width: '33%' }} /></div>
                  <small>2 of 6 practice questions done</small>
                </div>
                <button className="primary-button" type="button">Continue practice <ArrowRight size={18} /></button>
              </div>
              <p className="fl-child-note"><Sparkles size={15} /> Take your time. It’s okay to ask for hints — that’s how you learn.</p>
            </aside>
          </div>
        </div>
      </main>
    )
  }

  return (
    <main className="learning-app">
      <header className="learning-header">
        <div className="learning-brand">
          <img className="brand-mark" src="/sparkquill-mark.svg" alt="" width={30} height={30} />
          <span className="brand-word">Spark<strong>Quill</strong></span>
        </div>
        <div className="setup-progress" aria-label={`Setup step ${screen === 'engine' ? '1' : screen === 'child' ? '2' : '3'} of 3`}>
          <span className="setup-step-name">{screen === 'engine' ? 'Learning helper' : screen === 'child' ? 'Your child' : 'Parent PIN'}</span>
          <span className="setup-step-count">{screen === 'engine' ? '1' : screen === 'child' ? '2' : '3'} of 3</span>
          <span className="setup-progress-track" aria-hidden="true">
            <i className="is-complete" />
            <i className={screen === 'child' || screen === 'pin' ? 'is-complete' : ''} />
            <i className={screen === 'pin' ? 'is-complete' : ''} />
          </span>
        </div>
      </header>

      <section className={`learning-stage is-${screen}`}>
        {screen === 'engine' && (
          <section className="learning-panel setup-panel">
            <span className="eyebrow">01 · Choose your learning helper</span>
            <h1>Pick the AI that will help your child learn.</h1>
            <p className="lead">It runs on this computer and powers every lesson, hint, and practice session.</p>

            {enginesState === 'loading' && (
              <p className="engine-note">Checking which AI teachers are installed on this computer…</p>
            )}
            {enginesState === 'error' && (
              <p className="engine-note is-error">Couldn’t reach the learning service at {FAMILY_API}. Make sure it’s running, then <button type="button" className="linklike" onClick={() => window.location.reload()}>try again</button>.</p>
            )}

            {enginesState === 'ready' && (
              <div className="engine-grid">
                {engines.map((item) => {
                  const status = engineStatus(item)
                  return (
                    <button
                      key={item.id}
                      type="button"
                      className={`engine-card ${engine === item.id ? 'is-selected' : ''} ${status.ready ? '' : 'is-unavailable'}`}
                      onClick={() => { setEngine(item.id); setTestState('idle'); setTestMessage('') }}
                    >
                      <span className="engine-icon"><Sparkles size={24} /></span>
                      <span className="engine-content">
                        <strong>{pres(item.id, item.name).name} {pres(item.id, item.name).preferred && <em className="preferred-badge">Recommended</em>}</strong>
                        <small>{pres(item.id, item.name).blurb}</small>
                      </span>
                      <span className={`engine-status ${status.ready ? 'is-ready' : ''}`}>{status.label}</span>
                    </button>
                  )
                })}
              </div>
            )}

            <div className="setup-footer">
              <p>
                {selectedEngine
                  ? (engineStatus(selectedEngine).ready
                      ? <><CheckCircle2 size={18} /> {pres(selectedEngine.id, selectedEngine.name).name} is ready.</>
                      : <><LockKeyhole size={18} /> {pres(selectedEngine.id, selectedEngine.name).name}: {engineStatus(selectedEngine).label.toLowerCase()}.</>)
                  : <>Select a learning helper to continue.</>}
                {selectedEngine && engineStatus(selectedEngine).ready && (
                  <button type="button" className="linklike" onClick={runTest} disabled={testState === 'testing'}>
                    {testState === 'testing' ? 'Testing…' : testState === 'valid' ? 'Test passed ✓' : testState === 'invalid' ? 'Test failed — retry' : 'Test connection'}
                  </button>
                )}
              </p>
              <button className="primary-button" onClick={persistEngineAndContinue} type="button" disabled={!selectedEngine || !engineStatus(selectedEngine).ready || saving}>Continue <ArrowRight size={18} /></button>
            </div>
            {testMessage && <p className={`engine-note ${testState === 'invalid' ? 'is-error' : ''}`}>{testMessage}</p>}
            {selectedEngine && !engineStatus(selectedEngine).ready && selectedEngine.setup_hint && (
              <details className="engine-setup"><summary>Setup details</summary><p>{selectedEngine.setup_hint}</p></details>
            )}
          </section>
        )}

        {screen === 'child' && (
          <section className="learning-panel setup-panel">
            <span className="eyebrow">02 · Add your child</span>
            <h1>Create one calm learning space.</h1>
            <p className="lead">Tell the learning guide just enough to make each session feel personal.</p>
            <div className="child-form-card">
              <label>
                <span>Name or nickname</span>
                <input value={childName} onChange={(event) => setChildName(event.target.value)} />
              </label>
              <div className="form-row">
                <label>
                  <span>Grade</span>
                  <select value={grade} onChange={(event) => setGrade(event.target.value)}>
                    {GRADES.map((g) => <option key={g} value={g}>Grade {g}</option>)}
                  </select>
                </label>
                <label>
                  <span>School board</span>
                  <select value={board} onChange={(event) => setBoard(event.target.value)}>
                    {BOARDS.map((b) => <option key={b} value={b}>{b}</option>)}
                  </select>
                </label>
              </div>
              <div className="profile-preview">
                <span className="avatar-preview">{initial}</span>
                <span><strong>{childName || 'Your child'}</strong><small>Grade {grade} · {board} · English</small></span>
              </div>
            </div>
            <div className="setup-footer">
              <p><LockKeyhole size={18} /> Next, set a parent PIN.</p>
              <div className="setup-actions">
                <button className="setup-back" onClick={() => move('engine')} type="button"><ArrowLeft size={16} /> Back</button>
                <button className="primary-button" onClick={createChildAndContinue} type="button" disabled={!childName.trim() || saving}>Continue <ArrowRight size={18} /></button>
              </div>
            </div>
          </section>
        )}

        {screen === 'pin' && (
          <section className="learning-panel setup-panel">
            <span className="eyebrow">03 · Set a parent PIN</span>
            <h1>Create your parent PIN.</h1>
            <p className="lead">This keeps Parent Mode — your notes, answer keys, grading, and settings — separate from {childName || 'your child'}’s space on this shared computer.</p>
            <div className="child-form-card">
              <div className="form-row">
                <label>
                  <span>Parent PIN</span>
                  <input type="password" inputMode="numeric" value={pin} onChange={(event) => setPin(event.target.value.replace(/\D/g, '').slice(0, 8))} placeholder="4–8 digits" />
                </label>
                <label>
                  <span>Confirm PIN</span>
                  <input type="password" inputMode="numeric" value={pinConfirm} onChange={(event) => setPinConfirm(event.target.value.replace(/\D/g, '').slice(0, 8))} placeholder="Re-enter" />
                </label>
              </div>
              <p className="pin-hint">You’ll enter this to return to Parent Mode after handing the computer to {childName || 'your child'}.</p>
            </div>
            <div className="setup-footer">
              <p>{pinError ? <span className="pin-error"><LockKeyhole size={18} /> {pinError}</span> : <><LockKeyhole size={18} /> Only you should know this PIN.</>}</p>
              <div className="setup-actions">
                <button className="setup-back" onClick={() => move('child')} type="button"><ArrowLeft size={16} /> Back</button>
                <button className="primary-button" onClick={savePinAndContinue} type="button" disabled={saving}>Enter SparkQuill <ArrowRight size={18} /></button>
              </div>
            </div>
          </section>
        )}
      </section>
    </main>
  )
}
