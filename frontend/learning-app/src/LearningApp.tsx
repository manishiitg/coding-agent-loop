import { useState, useEffect, useRef, type FormEvent, type ChangeEvent } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import {
  ArrowLeft,
  ArrowRight,
  BookOpen,
  Check,
  CheckCircle2,
  ExternalLink,
  FileText,
  LockKeyhole,
  PanelLeftClose,
  PanelLeftOpen,
  Paperclip,
  Plus,
  RefreshCw,
  Send,
  Sparkles,
  Star,
  Sun,
} from 'lucide-react'
import './learning-app.css'
import {
  useSetupStore,
  useFamilyStore,
  useParentChatStore,
  useWorkspaceStore,
  useChildChatStore,
  useWhatsAppStore,
  usePinGateStore,
  toParentMsg,
  type Screen,
  type DrawerTab,
  type ApiEngine,
  type ConvMeta,
  type ParentMsg,
  type StoredMsg,
  type TreeNode,
  type WsFile,
  type ChildSuggestion,
} from './stores'

const FAMILY_API = (import.meta as { env?: { VITE_FAMILY_API?: string } }).env?.VITE_FAMILY_API ?? 'http://127.0.0.1:8010'

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

// Which side of the handoff the browser should land on after a refresh.
// Without this, a refresh always falls back to Parent Mode — letting a child
// bypass the PIN gate entirely just by reloading the page. Persisted in
// localStorage (this is a single local-machine app, same as the rest of its
// state) and flipped explicitly at every real hand-off/PIN-unlock point.
const HANDOFF_SIDE_KEY = 'sparkquill.handoff-side'
function persistHandoffSide(side: 'tutor' | 'parent') {
  try { localStorage.setItem(HANDOFF_SIDE_KEY, side) } catch { /* best-effort */ }
}
function readHandoffSide(): 'tutor' | 'parent' {
  try { return localStorage.getItem(HANDOFF_SIDE_KEY) === 'tutor' ? 'tutor' : 'parent' } catch { return 'parent' }
}

// Markdown renders the agent's reply with react-markdown + GFM — the same
// battle-tested renderer the main AgentWorks frontend uses (handles tables,
// nested lists, lazy-continuation of terminal-wrapped list items, code, etc.).
function Markdown({ text }: { text: string }) {
  return <ReactMarkdown remarkPlugins={[remarkGfm]}>{text}</ReactMarkdown>
}

// QUICK_SKILLS are one-click shortcuts in the composer menu; each sends a message
// that triggers the matching agent skill.
const QUICK_SKILLS = [
  { label: 'Create study material', message: 'Create study material for my child — follow your create-study-material skill and make it a designed, static (view-only) HTML page.' },
  { label: 'Create a practice test', message: 'Create a practice test for my child — follow your create-test skill: an interactive HTML page that records my child’s typed answers, plus a separate answer key for me.' },
  { label: 'Update progress report', message: 'Build an updated progress report — follow your create-progress-report skill, make it a designed HTML page, and give me a short coach-style read of the evidence here in chat too.' },
  { label: 'Update academic map', message: 'Update the academic map — follow your create-academic-map skill (designed HTML at shared/academic-map.html).' },
  { label: 'Back up workspace', message: 'Back up my workspace now — follow your backup skill.' },
]

// PARENT_WAIT_HINTS / CHILD_WAIT_HINTS cycle in the "thinking" indicator while
// waiting for a reply with no live tool-status yet — real, usable tips on how
// to use the chat, shown instead of a bare "thinking…" so the wait is at
// least a little useful. Live tool status (e.g. "Opening the file…") always
// takes priority over these when it's available.
const PARENT_WAIT_HINTS = [
  'Tip: ask "How is my child doing so far?" anytime for an evidence-based read of their progress.',
  'Tip: once a test or guide is ready, use the "Give to child" button to hand it over.',
  'Tip: tell Quill how you want tutoring handled — e.g. "give one hint before the answer" — and it remembers.',
  'Tip: ask for several things at once — "make a guide, a quick test, and an advanced one" — bundled as one package.',
  'Tip: Quill can look up board-specific tips and exam strategies — just ask.',
  'Tip: you can ask Quill to explain a topic to you, not just make material for your child.',
]
const CHILD_WAIT_HINTS = [
  'Tip: stuck? Just say "give me a hint!"',
  'Tip: you can ask Quill to explain it a different way.',
  'Tip: tell Quill your answer — it will tell you if you got it right.',
  'Tip: ask for an example if a question feels tricky.',
  'Tip: you can ask Quill anything about what you\'re learning, not just the current question.',
]

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
const IMAGE_PATH_RE = /\.(png|jpe?g|gif|webp|svg|bmp)$/i

// sanitizeDecorativeHtml allows a tiny, LLM-authored decorative fragment (a
// dash of inline color/styling around the label) inside a suggestion pill,
// without letting it execute anything: script/style blocks and their content
// are stripped entirely, event-handler attributes and javascript: URLs are
// stripped, and only a small inline-formatting tag whitelist survives — any
// other tag is dropped (its text content is kept, just not the markup).
// Click-to-send behavior always lives in the wrapping React <button>, never
// in this content, so even a maximally hostile fragment can't do more than
// render inert, differently-colored text.
function sanitizeDecorativeHtml(html: string): string {
  if (!html || html.length > 400) return ''
  let safe = html
  safe = safe.replace(/<(script|style)[^>]*>[\s\S]*?<\/\1>/gi, '')
  safe = safe.replace(/<\/?(script|style)[^>]*>/gi, '')
  safe = safe.replace(/\son\w+\s*=\s*"(?:[^"\\]|\\.)*"/gi, '')
  safe = safe.replace(/\son\w+\s*=\s*'(?:[^'\\]|\\.)*'/gi, '')
  safe = safe.replace(/(href|src)\s*=\s*["']\s*javascript:[^"']*["']/gi, '')
  safe = safe.replace(/<(?!\/?(span|strong|em|b|i|br|small)\b)[^>]*>/gi, '')
  return safe
}

// parseAssetPath reads whatever structure a generated/uploaded file's path
// carries — shared/<type>/<subject>/<topic>/<yyyy-mm-dd>-<name>.ext, or a
// shallower legacy path — into subject/topic/date + a human label. Filenames
// are auto-generated noise (WhatsApp Image ..., s02.png) so the label prefers
// the date-stripped name, falling back to "Photo"/"File" for image uploads
// whose name carries no information at all.
function parseAssetPath(p: string): { subject?: string; topic?: string; date?: string; label: string } {
  const parts = p.split('/')
  const rest = parts.slice(2) // drop "shared/<type>"
  const filename = rest[rest.length - 1] || parts[parts.length - 1] || p
  const subject = rest.length >= 3 ? rest[0] : rest.length === 2 ? rest[0] : undefined
  const topic = rest.length >= 3 ? rest[1] : undefined
  const nameNoExt = filename.replace(/\.[a-z0-9]+$/i, '')
  const dateMatch = nameNoExt.match(/^(\d{4}-\d{2}-\d{2})[-_](.+)$/)
  const date = dateMatch ? dateMatch[1] : undefined
  let rawLabel = (dateMatch ? dateMatch[2] : nameNoExt).replace(/[-_]+/g, ' ').trim()
  if (!rawLabel || /^(whatsapp image|img\d*|s\d+|image\d*|photo\d*)\b/i.test(rawLabel)) {
    rawLabel = IMAGE_PATH_RE.test(p) ? 'Photo' : 'File'
  }
  return { subject, topic, date, label: rawLabel }
}

export default function LearningApp() {
  const screen = useSetupStore((s) => s.screen)
  const setScreen = useSetupStore((s) => s.setScreen)
  const engines = useSetupStore((s) => s.engines)
  const setEngines = useSetupStore((s) => s.setEngines)
  const enginesState = useSetupStore((s) => s.enginesState)
  const setEnginesState = useSetupStore((s) => s.setEnginesState)
  const engine = useSetupStore((s) => s.engine)
  const setEngine = useSetupStore((s) => s.setEngine)
  const testState = useSetupStore((s) => s.testState)
  const setTestState = useSetupStore((s) => s.setTestState)
  const testMessage = useSetupStore((s) => s.testMessage)
  const setTestMessage = useSetupStore((s) => s.setTestMessage)

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
  const childName = useFamilyStore((s) => s.childName)
  const setChildName = useFamilyStore((s) => s.setChildName)
  const grade = useFamilyStore((s) => s.grade)
  const setGrade = useFamilyStore((s) => s.setGrade)
  const board = useFamilyStore((s) => s.board)
  const setBoard = useFamilyStore((s) => s.setBoard)
  const focusInput = useParentChatStore((s) => s.focusInput)
  const setFocusInput = useParentChatStore((s) => s.setFocusInput)
  const parentMessages = useParentChatStore((s) => s.parentMessages)
  const setParentMessages = useParentChatStore((s) => s.setParentMessages)
  const sending = useParentChatStore((s) => s.sending)
  const setSending = useParentChatStore((s) => s.setSending)
  const liveStatus = useParentChatStore((s) => s.liveStatus)
  const setLiveStatus = useParentChatStore((s) => s.setLiveStatus)
  const suggestions = useParentChatStore((s) => s.suggestions)
  const setSuggestions = useParentChatStore((s) => s.setSuggestions)
  const pendingHandoff = useParentChatStore((s) => s.pendingHandoff)
  const setPendingHandoff = useParentChatStore((s) => s.setPendingHandoff)
  const menuOpen = useParentChatStore((s) => s.menuOpen)
  const setMenuOpen = useParentChatStore((s) => s.setMenuOpen)
  const waOpen = useWhatsAppStore((s) => s.waOpen)
  const setWaOpen = useWhatsAppStore((s) => s.setWaOpen)
  const waMessages = useWhatsAppStore((s) => s.waMessages)
  const setWaMessages = useWhatsAppStore((s) => s.setWaMessages)
  const waInput = useWhatsAppStore((s) => s.waInput)
  const setWaInput = useWhatsAppStore((s) => s.setWaInput)
  const waSending = useWhatsAppStore((s) => s.waSending)
  const setWaSending = useWhatsAppStore((s) => s.setWaSending)
  const wsFiles = useWorkspaceStore((s) => s.wsFiles)
  const setWsFiles = useWorkspaceStore((s) => s.setWsFiles)
  const allFiles = useWorkspaceStore((s) => s.allFiles)
  const setAllFiles = useWorkspaceStore((s) => s.setAllFiles)
  // conversationId/childConversationId are lazily seeded with a fresh generated
  // id on first render (same as before Zustand) — the stores default to '',
  // which would send an empty conversation_id on the very first message before
  // any resume/new-conversation action has run.
  const [conversationId, setConversationId] = useState(newConversationId)
  const conversations = useParentChatStore((s) => s.conversations)
  const setConversations = useParentChatStore((s) => s.setConversations)
  const childSessionsList = useParentChatStore((s) => s.childSessionsList)
  const setChildSessionsList = useParentChatStore((s) => s.setChildSessionsList)
  const resumedRef = useRef(false)
  const childResumedRef = useRef(false)
  const childMessages = useChildChatStore((s) => s.childMessages)
  const setChildMessages = useChildChatStore((s) => s.setChildMessages)
  const childSending = useChildChatStore((s) => s.childSending)
  const setChildSending = useChildChatStore((s) => s.setChildSending)
  const childInput = useChildChatStore((s) => s.childInput)
  const setChildInput = useChildChatStore((s) => s.setChildInput)
  const childSuggestions = useChildChatStore((s) => s.childSuggestions)
  const setChildSuggestions = useChildChatStore((s) => s.setChildSuggestions)
  const childLiveStatus = useChildChatStore((s) => s.childLiveStatus)
  const setChildLiveStatus = useChildChatStore((s) => s.setChildLiveStatus)
  const childStars = useFamilyStore((s) => s.childStars)
  const setChildStars = useFamilyStore((s) => s.setChildStars)
  const parentLabel = useFamilyStore((s) => s.parentLabel)
  const setParentLabel = useFamilyStore((s) => s.setParentLabel)
  const [childConversationId, setChildConversationId] = useState(newConversationId)

  const wsRefreshKey = useWorkspaceStore((s) => s.wsRefreshKey)
  const setWsRefreshKey = useWorkspaceStore((s) => s.setWsRefreshKey)
  // Reflect the workspace file system in the drawer (materials the agent can
  // read). Refetches when entering the chat and after each upload/tool event.
  useEffect(() => {
    // Also runs on 'tutor' (not just 'parent') — this is where the child's own
    // conversation gets resumed from its persisted transcript on load; skipping
    // it for tutor meant a refreshed child always silently got a brand-new,
    // empty, cold-start session instead of continuing where they left off.
    if (screen !== 'parent' && screen !== 'tutor') return
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
        const childConvs = valid.filter((c) => c.scope === 'child')
        setChildSessionsList(childConvs)
        // Resume the most recent conversation by default (once) so the parent
        // continues where they left off instead of starting a fresh chat each load.
        if (!resumedRef.current && parentMessages.length === 0 && parentConvs.length > 0) {
          resumedRef.current = true
          const top = parentConvs[0]
          fetch(`${FAMILY_API}/api/workspace/file?path=${encodeURIComponent(`${top.scope}/conversations/${top.id}.json`)}`)
            .then((r) => r.json())
            .then((dd) => {
              const c = JSON.parse(dd.content) as { id: string; messages?: StoredMsg[] }
              setConversationId(c.id)
              setParentMessages((c.messages || []).map(toParentMsg))
            })
            .catch(() => {})
        }
        // Same for the child's own conversation — without this, every page
        // refresh silently started a brand-new child session (fresh cold-start
        // agent turn, empty visible thread, lost stars/celebration history)
        // even though the screen itself now correctly stays on "tutor".
        if (!childResumedRef.current && childMessages.length === 0 && childConvs.length > 0) {
          childResumedRef.current = true
          const top = childConvs[0]
          fetch(`${FAMILY_API}/api/workspace/file?path=${encodeURIComponent(`${top.scope}/conversations/${top.id}.json`)}`)
            .then((r) => r.json())
            .then((dd) => {
              const c = JSON.parse(dd.content) as { id: string; messages?: StoredMsg[] }
              setChildConversationId(c.id)
              setChildMessages((c.messages || []).map(toParentMsg))
            })
            .catch(() => {})
        }
      })
      .catch(() => {})
    return () => { cancelled = true }
  }, [screen, parentMessages.length, wsRefreshKey])
  const railOpen = useParentChatStore((s) => s.railOpen)
  const setRailOpen = useParentChatStore((s) => s.setRailOpen)
  const drawerOpen = true // right side always open
  const threadEndRef = useRef<HTMLDivElement>(null)
  const childThreadEndRef = useRef<HTMLDivElement>(null)
  const iframeRef = useRef<HTMLIFrameElement>(null)
  const drawerTab = useWorkspaceStore((s) => s.drawerTab)
  const setDrawerTab = useWorkspaceStore((s) => s.setDrawerTab)
  const filesView = useWorkspaceStore((s) => s.filesView)
  const setFilesView = useWorkspaceStore((s) => s.setFilesView)
  const prefsContent = useWorkspaceStore((s) => s.prefsContent)
  const setPrefsContent = useWorkspaceStore((s) => s.setPrefsContent)
  const childFiles = useChildChatStore((s) => s.childFiles)
  const setChildFiles = useChildChatStore((s) => s.setChildFiles)
  const childPackages = useChildChatStore((s) => s.childPackages)
  const setChildPackages = useChildChatStore((s) => s.setChildPackages)
  const childViewerPath = useChildChatStore((s) => s.childViewerPath)
  const setChildViewerPath = useChildChatStore((s) => s.setChildViewerPath)
  // Bumped whenever open_file fires, even for the SAME path — Quill re-opens
  // the child's own active/ copy after editing it to add a progress note, and
  // a same-string setChildViewerPath wouldn't otherwise trigger a refetch.
  const [childViewerRefreshKey, setChildViewerRefreshKey] = useState(0)
  const childViewerContent = useChildChatStore((s) => s.childViewerContent)
  const setChildViewerContent = useChildChatStore((s) => s.setChildViewerContent)
  const childTreeRefreshKey = useChildChatStore((s) => s.childTreeRefreshKey)
  const setChildTreeRefreshKey = useChildChatStore((s) => s.setChildTreeRefreshKey)
  const filesSubjectFilter = useWorkspaceStore((s) => s.filesSubjectFilter)
  const setFilesSubjectFilter = useWorkspaceStore((s) => s.setFilesSubjectFilter)
  const treeNodes = useWorkspaceStore((s) => s.treeNodes)
  const setTreeNodes = useWorkspaceStore((s) => s.setTreeNodes)
  const viewerPath = useWorkspaceStore((s) => s.viewerPath)
  const setViewerPath = useWorkspaceStore((s) => s.setViewerPath)
  const viewerRefreshKey = useWorkspaceStore((s) => s.viewerRefreshKey)
  const setViewerRefreshKey = useWorkspaceStore((s) => s.setViewerRefreshKey)
  const viewerImageList = useWorkspaceStore((s) => s.viewerImageList)
  const setViewerImageList = useWorkspaceStore((s) => s.setViewerImageList)
  const viewerContent = useWorkspaceStore((s) => s.viewerContent)
  const setViewerContent = useWorkspaceStore((s) => s.setViewerContent)
  const mapHtml = useWorkspaceStore((s) => s.mapHtml)
  const setMapHtml = useWorkspaceStore((s) => s.setMapHtml)
  const mapRefreshKey = useWorkspaceStore((s) => s.mapRefreshKey)
  const setMapRefreshKey = useWorkspaceStore((s) => s.setMapRefreshKey)
  const progressHtml = useWorkspaceStore((s) => s.progressHtml)
  const setProgressHtml = useWorkspaceStore((s) => s.setProgressHtml)
  const booting = useSetupStore((s) => s.booting)
  const setBooting = useSetupStore((s) => s.setBooting)
  const bootError = useSetupStore((s) => s.bootError)
  const setBootError = useSetupStore((s) => s.setBootError)
  const pin = useSetupStore((s) => s.pin)
  const setPin = useSetupStore((s) => s.setPin)
  const pinConfirm = useSetupStore((s) => s.pinConfirm)
  const setPinConfirm = useSetupStore((s) => s.setPinConfirm)
  const pinError = useSetupStore((s) => s.pinError)
  const setPinError = useSetupStore((s) => s.setPinError)
  const saving = useSetupStore((s) => s.saving)
  const setSaving = useSetupStore((s) => s.setSaving)
  // Child→Parent is gated by the parent PIN so a child can't reach answer keys.
  const pinGate = usePinGateStore((s) => s.pinGate)
  const setPinGate = usePinGateStore((s) => s.setPinGate)
  const gateValue = usePinGateStore((s) => s.gateValue)
  const setGateValue = usePinGateStore((s) => s.setGateValue)
  const gateError = usePinGateStore((s) => s.gateError)
  const setGateError = usePinGateStore((s) => s.setGateError)

  // Load the real, agent-generated shared/academic-map.html for the Subjects
  // tab — refetches whenever the tab is opened or a turn just completed (the
  // agent may have rebuilt the map during that turn).
  useEffect(() => {
    if (drawerTab !== 'map') return
    let cancelled = false
    fetch(`${FAMILY_API}/api/workspace/file?path=${encodeURIComponent('shared/academic-map.html')}`)
      .then((r) => r.json())
      .then((d) => { if (!cancelled) setMapHtml(d.content ?? '') })
      .catch(() => { if (!cancelled) setMapHtml('') })
    return () => { cancelled = true }
  }, [drawerTab, mapRefreshKey])

  // Load the real, agent-generated shared/reports/progress.html for the
  // Progress tab — a single living document, rendered directly (not a link
  // the parent has to click through to).
  useEffect(() => {
    if (drawerTab !== 'progress') return
    let cancelled = false
    fetch(`${FAMILY_API}/api/workspace/file?path=${encodeURIComponent('shared/reports/progress.html')}`)
      .then((r) => r.json())
      .then((d) => { if (!cancelled) setProgressHtml(d.content ?? '') })
      .catch(() => { if (!cancelled) setProgressHtml('') })
    return () => { cancelled = true }
  }, [drawerTab, mapRefreshKey])

  // Load parent/preferences.md for the Reference tab, refetched whenever it's
  // opened or a turn just completed (the agent may have updated it).
  useEffect(() => {
    if (filesView !== 'reference') return
    let cancelled = false
    fetch(`${FAMILY_API}/api/workspace/file?path=${encodeURIComponent('parent/preferences.md')}`)
      .then((r) => r.json())
      .then((d) => { if (!cancelled) setPrefsContent(d.content ?? '') })
      .catch(() => { if (!cancelled) setPrefsContent('') })
    return () => { cancelled = true }
  }, [filesView, mapRefreshKey])

  // Load the child's own scoped file list (only what the parent has approved,
  // plus the child's own saved attempts) — refetches whenever the tutor screen
  // is entered or a child turn just completed.
  useEffect(() => {
    if (screen !== 'tutor') return
    let cancelled = false
    fetch(`${FAMILY_API}/api/child/workspace/tree`)
      .then((r) => r.json())
      .then((nodes: TreeNode[]) => {
        if (cancelled) return
        const files: string[] = []
        const walk = (ns: TreeNode[]) => ns?.forEach((n) => {
          if (n.type === 'file' && !n.name.endsWith('.meta.json')) files.push(n.path)
          if (n.children) walk(n.children)
        })
        walk(nodes)
        setChildFiles(files)
      })
      .catch(() => { if (!cancelled) setChildFiles([]) })
    return () => { cancelled = true }
  }, [screen, childTreeRefreshKey])

  // A learning package (shared/packages/*.json) bundles several approved files
  // under one title/order — read each manifest so the materials list can show
  // "the package" the parent handed off, instead of its raw manifest file plus
  // every item repeated as its own separate entry.
  useEffect(() => {
    const manifestPaths = childFiles.filter((p) => p.startsWith('shared/packages/') && p.endsWith('.json'))
    if (manifestPaths.length === 0) { setChildPackages([]); return }
    let cancelled = false
    Promise.all(manifestPaths.map((p) =>
      fetch(`${FAMILY_API}/api/workspace/file?path=${encodeURIComponent(p)}`)
        .then((r) => r.json())
        .then((d: { content?: string }) => {
          try {
            const pkg = JSON.parse(d.content ?? '{}')
            return {
              path: p,
              title: String(pkg.title ?? 'Learning package'),
              items: Array.isArray(pkg.items) ? pkg.items : [],
              guideNote: typeof pkg.guide_note === 'string' ? pkg.guide_note : undefined,
            }
          } catch { return null }
        })
        .catch(() => null)
    )).then((results) => { if (!cancelled) setChildPackages(results.filter((r): r is { path: string; title: string; items: string[]; guideNote?: string } => r !== null)) })
    return () => { cancelled = true }
  }, [childFiles])

  // Load the selected file for the child's own inline viewer.
  useEffect(() => {
    if (!childViewerPath) { setChildViewerContent(null); return }
    let cancelled = false
    setChildViewerContent(null)
    fetch(`${FAMILY_API}/api/workspace/file?path=${encodeURIComponent(childViewerPath)}`)
      .then((r) => r.json())
      .then((d) => { if (!cancelled) setChildViewerContent({ isText: !!d.is_text, content: d.content ?? '' }) })
      .catch(() => { if (!cancelled) setChildViewerContent({ isText: false, content: '' }) })
    return () => { cancelled = true }
  }, [childViewerPath, childViewerRefreshKey])

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
  }, [viewerPath, viewerRefreshKey])

  // Left/Right arrow keys step through the image list the viewer was opened
  // from (e.g. the Uploaded Material thumbnail grid for the current subject).
  useEffect(() => {
    if (!viewerPath || !IMAGE_PATH_RE.test(viewerPath) || viewerImageList.length < 2) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return
      const idx = viewerImageList.indexOf(viewerPath)
      if (idx === -1) return
      const dir = e.key === 'ArrowLeft' ? -1 : 1
      const next = (idx + dir + viewerImageList.length) % viewerImageList.length
      setViewerPath(viewerImageList[next])
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [viewerPath, viewerImageList])

  // Keep the conversation scrolled to the latest message / thinking indicator.
  useEffect(() => {
    threadEndRef.current?.scrollIntoView({ behavior: 'smooth', block: 'end' })
  }, [parentMessages, sending])

  // Same, for the child's own thread — this had no auto-scroll at all before,
  // so new replies (and the "thinking" indicator) could land below the fold
  // with no automatic scroll to reveal them.
  useEffect(() => {
    childThreadEndRef.current?.scrollIntoView({ behavior: 'smooth', block: 'end' })
  }, [childMessages, childSending])

  // Cycle a usable "how to use the chat" tip in the thinking indicator instead
  // of a bare "thinking…" — resets and restarts each time a new turn begins,
  // and only matters while there's no real live tool status to show instead.
  const [parentHintIndex, setParentHintIndex] = useState(0)
  useEffect(() => {
    if (!sending) { setParentHintIndex(0); return }
    const id = window.setInterval(() => setParentHintIndex((i) => (i + 1) % PARENT_WAIT_HINTS.length), 3500)
    return () => window.clearInterval(id)
  }, [sending])
  const [childHintIndex, setChildHintIndex] = useState(0)
  useEffect(() => {
    if (!childSending) { setChildHintIndex(0); return }
    const id = window.setInterval(() => setChildHintIndex((i) => (i + 1) % CHILD_WAIT_HINTS.length), 3500)
    return () => window.clearInterval(id)
  }, [childSending])

  // Bridge for interactive HTML: the sandboxed viewer iframe posts SQ.save/load
  // messages; the app persists them to a workspace file (child/attempts) so the
  // child's answers survive reloads and Quill can read them later.
  useEffect(() => {
    const onMsg = (e: MessageEvent) => {
      const m = e.data
      if (!m || typeof m !== 'object' || (m as { __sq?: unknown }).__sq !== 1) return
      const msg = m as { op?: string; key?: string; id?: string; data?: unknown }
      if (msg.op === 'save' && typeof msg.key === 'string') {
        fetch(`${FAMILY_API}/api/workspace/state`, {
          method: 'POST', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ key: msg.key, data: msg.data }),
        }).catch(() => {})
      } else if (msg.op === 'load' && typeof msg.key === 'string') {
        fetch(`${FAMILY_API}/api/workspace/state?key=${encodeURIComponent(msg.key)}`)
          .then((r) => r.json())
          .then((d) => iframeRef.current?.contentWindow?.postMessage({ __sq: 1, op: 'loaded', id: msg.id, data: d?.data ?? null }, '*'))
          .catch(() => iframeRef.current?.contentWindow?.postMessage({ __sq: 1, op: 'loaded', id: msg.id, data: null }, '*'))
      }
    }
    window.addEventListener('message', onMsg)
    return () => window.removeEventListener('message', onMsg)
  }, [])

  // On launch, ask family-server where onboarding stands. If setup is complete
  // we land straight in the chat; otherwise resume at the right step.
  useEffect(() => {
    let cancelled = false
    const load = (attempt: number) => {
      fetch(`${FAMILY_API}/api/setup`)
        .then((res) => { if (!res.ok) throw new Error(String(res.status)); return res.json() })
        .then((data: { next_step?: string; engine?: string; child?: { name?: string; grade?: string; board?: string; stars?: number } | null; parent_label?: string }) => {
          if (cancelled) return
          if (data.engine) setEngine(data.engine)
          if (data.child) {
            if (data.child.name) setChildName(data.child.name)
            if (data.child.grade) setGrade(data.child.grade)
            if (data.child.board) setBoard(data.child.board)
            if (data.child.stars) setChildStars(data.child.stars)
          }
          if (data.parent_label) setParentLabel(data.parent_label)
          const step = data.next_step
          if (step === 'done') setScreen(readHandoffSide() === 'tutor' ? 'tutor' : 'parent')
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

  // Verify the parent PIN before returning to Parent Mode from the child screen.
  const submitPinGate = () => {
    setGateError('')
    fetch(`${FAMILY_API}/api/parent/pin/verify`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ pin: gateValue }),
    })
      .then((res) => res.json())
      .then((data: { ok?: boolean }) => {
        if (data.ok) { setPinGate(false); setGateValue(''); persistHandoffSide('parent'); move('parent') }
        else setGateError('That PIN isn’t right.')
      })
      .catch(() => setGateError('Could not check the PIN.'))
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
      .then((data: { error?: string }) => { if (data.error) { setPinError(data.error); return } persistHandoffSide('parent'); move('parent') })
      .catch(() => setPinError('Could not save the PIN.'))
      .finally(() => setSaving(false))
  }

  const sendParentText = (raw: string) => {
    const text = raw.trim()
    if (!text || sending) return
    const next: ParentMsg[] = [...parentMessages, { role: 'user', text }]
    setParentMessages(next)
    setFocusInput('')
    setSuggestions([])
    setPendingHandoff(null)
    setSending(true)
    setLiveStatus('')
    // Live "what Quill is doing right now" line, sourced from the same turn's
    // tool handlers — purely cosmetic, so a stream error is silently ignored.
    const statusSource = new EventSource(`${FAMILY_API}/api/parent/status?conversation_id=${encodeURIComponent(conversationId)}`)
    statusSource.onmessage = (ev) => setLiveStatus(ev.data)
    statusSource.onerror = () => statusSource.close()
    const history = next.filter((m) => m.role === 'user' || m.role === 'assistant').map((m) => ({ role: m.role, text: m.text ?? '' }))
    fetch(`${FAMILY_API}/api/parent/message`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ messages: history, conversation_id: conversationId }),
    })
      .then((res) => res.json())
      .then((data: { reply?: string; error?: string; suggestions?: { label: string; message: string }[]; tool_events?: { tool: string; name?: string; grade?: string; board?: string; path?: string; parent_label?: string }[]; handoff?: { label: string; path: string } }) => {
        const events = data.tool_events ?? []
        const toolMsgs: ParentMsg[] = events.filter((e) => e.tool === 'set_child_profile').map((e) => ({ role: 'tool', tool: e.tool, name: e.name, grade: e.grade, board: e.board }))
        const cp = events.find((e) => e.tool === 'set_child_profile')
        if (cp) { if (cp.name) setChildName(cp.name); if (cp.grade) setGrade(cp.grade); if (cp.board) setBoard(cp.board) }
        const pl = events.find((e) => e.tool === 'set_parent_label' && e.parent_label)
        if (pl?.parent_label) setParentLabel(pl.parent_label)
        const of = events.find((e) => e.tool === 'open_file' && e.path)
        if (of?.path) { setDrawerTab('files'); setViewerImageList([]); setViewerPath(of.path); setViewerRefreshKey((k) => k + 1) }
        setSuggestions(data.suggestions ?? [])
        setPendingHandoff(data.handoff ?? null)
        setParentMessages((cur) => [...cur, ...toolMsgs, { role: 'assistant', text: data.error ? `Sorry — ${data.error}` : (data.reply || '(no response)') }])
      })
      .catch(() => setParentMessages((cur) => [...cur, { role: 'assistant', text: 'Sorry — I couldn’t reach the learning engine.' }]))
      .finally(() => { setSending(false); setLiveStatus(''); statusSource.close(); setMapRefreshKey((k) => k + 1) })
  }

  const sendParentMessage = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    sendParentText(focusInput)
  }

  // WhatsApp simulator — previews how Quill replies over WhatsApp (plain text).
  const sendWhatsApp = (raw: string) => {
    const text = raw.trim()
    if (!text || waSending) return
    const next = [...waMessages, { role: 'user' as const, text }]
    setWaMessages(next)
    setWaInput('')
    setWaSending(true)
    fetch(`${FAMILY_API}/api/whatsapp/message`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ messages: next.map((m) => ({ role: m.role, text: m.text })), conversation_id: 'whatsapp-sim' }),
    })
      .then((r) => r.json())
      .then((d: { reply?: string; error?: string }) => setWaMessages((cur) => [...cur, { role: 'assistant', text: d.error ? `⚠️ ${d.error}` : (d.reply || '…') }]))
      .catch(() => setWaMessages((cur) => [...cur, { role: 'assistant', text: '⚠️ Could not reach SparkQuill.' }]))
      .finally(() => setWaSending(false))
  }

  // Load a past conversation into the chat view (reads the transcript file).
  const loadConversation = (item: ConvMeta) => {
    fetch(`${FAMILY_API}/api/workspace/file?path=${encodeURIComponent(`${item.scope}/conversations/${item.id}.json`)}`)
      .then((r) => r.json())
      .then((d) => {
        const c = JSON.parse(d.content) as { id: string; messages?: StoredMsg[] }
        setConversationId(c.id)
        setParentMessages((c.messages || []).map(toParentMsg))
      })
      .catch(() => {})
  }

  const startNewConversation = () => {
    resumedRef.current = true
    setConversationId(newConversationId())
    setParentMessages([])
    setSuggestions([])
    setPendingHandoff(null)
  }

  // Child Mode tutor — talks to /api/child/message (sandboxed child agent).
  // convIdOverride lets a caller that JUST generated a fresh id (e.g. starting
  // a new session right before this call) pass it explicitly — childConversationId
  // itself won't reflect a setChildConversationId() call made earlier in the
  // same synchronous handler until the next render, so reading it from the
  // closure here would silently target the OLD conversation.
  const sendChildText = (raw: string, base?: ParentMsg[], convIdOverride?: string) => {
    const text = raw.trim()
    if (!text || childSending) return
    const convId = convIdOverride ?? childConversationId
    const next: ParentMsg[] = [...(base ?? childMessages), { role: 'user', text }]
    setChildMessages(next)
    setChildInput('')
    setChildSuggestions([])
    setChildSending(true)
    setChildLiveStatus('')
    const statusSource = new EventSource(`${FAMILY_API}/api/child/status?conversation_id=${encodeURIComponent(convId)}`)
    statusSource.onmessage = (ev) => setChildLiveStatus(ev.data)
    statusSource.onerror = () => statusSource.close()
    const history = next.filter((m) => m.role === 'user' || m.role === 'assistant').map((m) => ({ role: m.role, text: m.text ?? '' }))
    fetch(`${FAMILY_API}/api/child/message`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ messages: history, conversation_id: convId }),
    })
      .then((res) => res.json())
      .then((data: { reply?: string; error?: string; tool_events?: { tool: string; path?: string; stars?: number; total?: number; reason?: string }[]; suggestions?: ChildSuggestion[] }) => {
        const events = data.tool_events ?? []
        const of = events.find((e) => e.tool === 'open_file' && e.path)
        if (of?.path) { setChildViewerPath(of.path); setChildViewerRefreshKey((k) => k + 1) }
        const cel = events.find((e) => e.tool === 'celebrate')
        if (cel) setChildStars(cel.total ?? 0)
        setChildSuggestions(data.suggestions ?? [])
        setChildMessages((cur) => {
          const next: ParentMsg[] = [...cur, { role: 'assistant', text: data.error ? `Hmm, something went wrong — ${data.error}` : (data.reply || '(no response)') }]
          if (cel) next.push({ role: 'tool', tool: 'celebrate', stars: cel.stars ?? 1, reason: cel.reason ?? '' })
          return next
        })
      })
      .catch(() => setChildMessages((cur) => [...cur, { role: 'assistant', text: 'I couldn’t reach the tutor just now — try again in a moment.' }]))
      .finally(() => { setChildSending(false); setChildLiveStatus(''); statusSource.close(); setChildTreeRefreshKey((k) => k + 1) })
  }

  // Enter Child Mode after a handoff response. new_session decides whether the
  // child continues their existing conversation (still the same package) or
  // starts a clean one (a different package, or a standalone file — per-
  // handoff resume only makes sense while it's genuinely the same package).
  // filePath is shown in the viewer directly, unconditionally — we already
  // know exactly which file the handoff points to (the backend just told us),
  // so we don't wait on Quill to call open_file itself. On a resumed
  // conversation Quill often reasons "I already opened this earlier" and
  // skips the call, which used to leave the child staring at a bare file
  // list instead of the actual document.
  const enterChildModeAfterHandoff = (newSession: boolean, greeting: string, filePath: string) => {
    // A dynamic/instruction-only package hands back its own manifest path (no
    // real file exists) — never show that raw JSON; let the conversation itself
    // be the content instead of trying to open anything.
    const isManifest = filePath.startsWith('shared/packages/') && filePath.endsWith('.json')
    setChildViewerPath(isManifest ? null : filePath)
    persistHandoffSide('tutor')
    setScreen('tutor')
    setChildTreeRefreshKey((k) => k + 1)
    if (newSession) {
      const freshId = newConversationId()
      setChildConversationId(freshId)
      setChildSuggestions([])
      setChildMessages([])
      sendChildText(greeting, [], freshId)
    } else {
      sendChildText(greeting)
    }
  }

  // handoffGreeting is what the child's chat "says" to kick off a handoff — it
  // reads like the child speaking to Quill, so it uses parentLabel ("mom",
  // "dad", a name) when known, falling back to "parent" until Quill has asked.
  const handoffGreeting = (what: string) => `My ${parentLabel || 'parent'} just ${what}. Can you help me get started?`

  // The real parent→child handoff behind "Give to <child>": approve the file,
  // switch into child mode, and kick off Quill — it finds the just-shared
  // material, opens it, and guides the child. No filename/path is shown; Quill
  // composes everything the child reads.
  const startHandoff = (path: string) => {
    fetch(`${FAMILY_API}/api/parent/handoff`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path }),
    })
      .then((res) => res.json())
      .then((data: { new_session?: boolean; path?: string }) => {
        enterChildModeAfterHandoff(!!data.new_session, handoffGreeting('shared something new for me to work on'), data.path || path)
      })
      .catch(() => {})
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
                <img className="fl-header-logo" src="/sparkquill-mark.svg" alt="" width={30} height={30} />
                <div className="fl-toolbar-title">
                  <strong className="fl-brand-word">Spark<span>Quill</span></strong>
                  <span>{childName || 'Your child'}{grade ? ` · Grade ${grade}` : ''}{board ? ` · ${board}` : ''}</span>
                </div>
              </div>
              <div className="fl-toolbar-right">
                <button className="fl-whatsapp-btn" type="button" aria-label="WhatsApp" title="SparkQuill on WhatsApp" onClick={() => setWaOpen(true)}>
                  <svg viewBox="0 0 24 24" width="19" height="19" fill="currentColor" aria-hidden="true"><path d="M17.472 14.382c-.297-.149-1.758-.867-2.03-.967-.273-.099-.471-.148-.67.15-.197.297-.767.966-.94 1.164-.173.199-.347.223-.644.075-.297-.15-1.255-.463-2.39-1.475-.883-.788-1.48-1.761-1.653-2.059-.173-.297-.018-.458.13-.606.134-.133.298-.347.446-.52.149-.174.198-.298.298-.497.099-.198.05-.371-.025-.52-.075-.149-.669-1.612-.916-2.207-.242-.579-.487-.5-.669-.51l-.57-.01c-.198 0-.52.074-.792.372-.272.297-1.04 1.016-1.04 2.479 0 1.462 1.065 2.875 1.213 3.074.149.198 2.096 3.2 5.077 4.487.71.306 1.263.489 1.694.626.712.226 1.36.194 1.872.118.571-.085 1.758-.719 2.006-1.413.248-.694.248-1.289.173-1.413-.074-.124-.272-.198-.57-.347m-5.421 7.403h-.004a9.87 9.87 0 01-5.031-1.378l-.361-.214-3.741.982.998-3.648-.235-.374a9.86 9.86 0 01-1.51-5.26c.001-5.45 4.436-9.884 9.888-9.884 2.64 0 5.122 1.03 6.988 2.898a9.825 9.825 0 012.893 6.994c-.003 5.45-4.437 9.884-9.885 9.884m8.413-18.297A11.815 11.815 0 0012.05 0C5.495 0 .16 5.335.157 11.892c0 2.096.547 4.142 1.588 5.945L.057 24l6.305-1.654a11.882 11.882 0 005.683 1.448h.005c6.554 0 11.89-5.335 11.893-11.893a11.821 11.821 0 00-3.48-8.413z"/></svg>
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
                        <div className="fl-toolcard"><Check size={15} strokeWidth={3} /> <span>Saved <strong>{m.name || 'child profile'}</strong>{m.grade ? ` · Grade ${m.grade}` : ''}{m.board ? ` · ${m.board}` : ''}</span></div>
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
                    <div className="fl-thinking"><img src="/sparkquill-loader.svg" alt="" width={38} height={38} /> <span>{PARENT_WAIT_HINTS[parentHintIndex]}</span></div>
                  </div>
                </div>
              )}

              {parentMessages.length === 0 && !sending && (
                <div className="parent-quick-actions" aria-label="Suggested parent requests">
                  <button type="button" onClick={() => setFocusInput(`How is ${childName || 'my child'} doing so far?`)}>Understand progress</button>
                  <button type="button" onClick={() => setFocusInput('Make a short revision worksheet for my child')}>Create study material</button>
                  <button type="button" onClick={() => setFocusInput('Create a short practice test for my child')}>Create a test</button>
                </div>
              )}
              {pendingHandoff && !sending && (
                <div className="fl-suggestions" aria-label="Handoff to child">
                  <button
                    type="button"
                    className="fl-suggestion fl-suggestion-handoff"
                    onClick={() => { const h = pendingHandoff; setPendingHandoff(null); startHandoff(h.path) }}
                  >
                    {pendingHandoff.label}
                  </button>
                </div>
              )}
              {suggestions.length > 0 && !sending && (
                <div className="fl-suggestions" aria-label="Recommended next steps">
                  {suggestions.map((s, i) => (
                    <button key={i} type="button" className="fl-suggestion" onClick={() => sendParentText(s.message)}>{s.label}</button>
                  ))}
                </div>
              )}
              <div ref={threadEndRef} />
            </div>

            <form className="fl-composer" onSubmit={sendParentMessage}>
              <input ref={fileInputRef} type="file" multiple accept="image/*,application/pdf" onChange={onFilesSelected} style={{ display: 'none' }} />
              <button className="composer-icon" type="button" aria-label="Attach a photo or PDF" onClick={onPickFiles} disabled={uploading}><Paperclip size={19} /></button>
              <input
                aria-label="Message the learning guide"
                placeholder={`Ask anything about ${childName || 'your child'}’s learning…`}
                value={focusInput}
                onChange={(event) => setFocusInput(event.target.value)}
                disabled={sending}
              />
              <div className="fl-composer-menu">
                {menuOpen && <div className="fl-menu-backdrop" onClick={() => setMenuOpen(false)} />}
                <button type="button" className="composer-icon" aria-label="Quick actions" aria-expanded={menuOpen} onClick={() => setMenuOpen((v) => !v)} disabled={sending}><Sparkles size={19} /></button>
                {menuOpen && (
                  <div className="fl-menu" role="menu">
                    {QUICK_SKILLS.map((s) => (
                      <button key={s.label} type="button" role="menuitem" onClick={() => { setMenuOpen(false); sendParentText(s.message) }}>{s.label}</button>
                    ))}
                  </div>
                )}
              </div>
              <button className="composer-send" type="submit" aria-label="Send message" disabled={!focusInput.trim() || sending}><Send size={18} /></button>
            </form>
            <p className="fl-disclaimer">SparkQuill can make mistakes. Please review important content before sharing it with {childName || 'your child'}.</p>
          </section>

          <aside className="fl-drawer" aria-label="Learning workspace">
            {!(drawerTab === 'files' && viewerPath) && (
              <div className="fl-drawer-tabs" role="tablist" aria-label="Workspace views">
                <button role="tab" aria-selected={drawerTab === 'map'} className={drawerTab === 'map' ? 'is-active' : ''} type="button" onClick={() => setDrawerTab('map')}>Academics</button>
                <button role="tab" aria-selected={drawerTab === 'progress'} className={drawerTab === 'progress' ? 'is-active' : ''} type="button" onClick={() => setDrawerTab('progress')}>Progress</button>
                <button role="tab" aria-selected={drawerTab === 'files'} className={drawerTab === 'files' ? 'is-active' : ''} type="button" onClick={() => setDrawerTab('files')}>Workspace</button>
                <button
                  type="button"
                  className="fl-icon-btn fl-refresh-btn"
                  aria-label="Refresh workspace"
                  title="Refresh"
                  onClick={() => { setWsRefreshKey((k) => k + 1); setMapRefreshKey((k) => k + 1) }}
                >
                  <RefreshCw size={15} />
                </button>
              </div>
            )}

            <div className="fl-drawer-scroll">
              {drawerTab === 'assets' && (
                <>
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
                mapHtml === null ? (
                  <p className="fl-note">Loading the academic map…</p>
                ) : mapHtml.includes('living view grows as') ? (
                  // Still the startup placeholder seedWorkspace() writes before the
                  // agent has ever run create-academic-map — an honest empty state
                  // rather than a blank iframe.
                  <p className="fl-note">The academic map hasn't been built yet — ask Quill to "update the academic map" once there's some material to show.</p>
                ) : (
                  <iframe className="fl-map-frame" title="Academic map" sandbox="allow-scripts" srcDoc={mapHtml} />
                )
              )}

              {drawerTab === 'progress' && (
                <>
                  {progressHtml === null ? (
                    <p className="fl-note">Loading the progress report…</p>
                  ) : progressHtml.includes('living report grows as') ? (
                    <p className="fl-note">The progress report hasn't been built yet — ask Quill to "update the progress report" once there's some real activity to show.</p>
                  ) : (
                    <iframe className="fl-map-frame" title="Progress report" sandbox="allow-scripts" srcDoc={progressHtml} />
                  )}
                </>
              )}

              {drawerTab === 'files' && (
                viewerPath ? (
                  <div className="fl-viewer">
                    <div className="fl-viewer-bar">
                      <button className="fl-viewer-back" type="button" onClick={() => setViewerPath(null)}><ArrowLeft size={15} /> Files</button>
                      <span className="fl-viewer-name">{viewerPath.split('/').pop()}</span>
                      <button
                        className="fl-icon-btn"
                        type="button"
                        aria-label="Refresh"
                        title="Reload this file"
                        onClick={() => setViewerRefreshKey((k) => k + 1)}
                      >
                        <RefreshCw size={14} />
                      </button>
                      {/^shared\/(tests|study|reports)\//.test(viewerPath) && (
                        <button
                          className="fl-give-to-child"
                          type="button"
                          disabled={sending}
                          onClick={() => startHandoff(viewerPath)}
                        >
                          Give to {childName || 'child'}
                        </button>
                      )}
                    </div>
                    {/\.(png|jpe?g|gif|webp|svg|bmp)$/i.test(viewerPath) ? (
                      <img className="fl-viewer-img" src={`${FAMILY_API}/api/workspace/raw?path=${encodeURIComponent(viewerPath)}`} alt={viewerPath.split('/').pop() || ''} />
                    ) : !viewerContent ? (
                      <p className="fl-note">Loading…</p>
                    ) : !viewerContent.isText ? (
                      <p className="fl-note">This file type can’t be previewed here.</p>
                    ) : (viewerPath.endsWith('.html') || viewerPath.endsWith('.htm')) ? (
                      <iframe ref={iframeRef} className="fl-viewer-frame" title="File preview" sandbox="allow-scripts" srcDoc={viewerContent.content} />
                    ) : (viewerPath.endsWith('.md') || viewerPath.endsWith('.markdown')) ? (
                      <div className="fl-viewer-md"><Markdown text={viewerContent.content} /></div>
                    ) : (
                      <pre className="fl-viewer-pre">{viewerContent.content}</pre>
                    )}
                  </div>
                ) : (
                  <>
                    <div className="fl-files-toggle">
                      <button type="button" className={filesView === 'subjects' ? 'is-active' : ''} onClick={() => setFilesView('subjects')}>Subjects</button>
                      <button type="button" className={filesView === 'uploaded' ? 'is-active' : ''} onClick={() => setFilesView('uploaded')}>Uploaded Material</button>
                      <button type="button" className={filesView === 'reference' ? 'is-active' : ''} onClick={() => setFilesView('reference')}>Reference</button>
                      <button type="button" className={filesView === 'advanced' ? 'is-active' : ''} onClick={() => setFilesView('advanced')}>All files</button>
                    </div>
                    {filesView === 'reference' ? (
                      prefsContent === null ? <p className="fl-note">Loading…</p> : <Markdown text={prefsContent} />
                    ) : filesView === 'advanced' ? (
                      treeNodes.length === 0 ? <p className="fl-note">No files yet.</p> : <FileTree nodes={treeNodes} onOpen={(p) => { setViewerImageList([]); setViewerPath(p) }} />
                    ) : (() => {
                      // Hierarchy: subject -> topic -> type (test/notes/...) -> date -> file.
                      // "Subjects" and "Uploaded Material" are the same tree, filtered to
                      // generated content vs. raw uploads; the academic map already has its
                      // own real (agent-generated) view via the outer Subjects drawer tab,
                      // so it's not duplicated here.
                      type Entry = { path: string; date?: string; label: string }
                      const typeOf = (p: string): string | null => {
                        if (p === 'shared/academic-map.html') return null
                        if (p.startsWith('shared/tests/')) return 'Practice tests'
                        if (p.startsWith('shared/study/')) return 'Study guides'
                        if (p.startsWith('shared/reports/')) return 'Reports'
                        if (p.includes('/materials/')) return 'Uploaded material'
                        return null
                      }
                      const usable = allFiles.filter((p) => !p.endsWith('.meta.json') && !p.startsWith('skills/') && !p.includes('/conversations/') && !p.endsWith('child-profile.json'))
                      const classified = usable.map((p) => ({ p, type: typeOf(p), ...parseAssetPath(p) })).filter((f) => f.type)
                      const subjectsList = Array.from(new Set(classified.filter((f) => f.subject).map((f) => f.subject!))).sort()
                      const wantedTypes = filesView === 'uploaded' ? ['Uploaded material'] : ['Practice tests', 'Study guides', 'Reports']
                      const relevant = classified.filter((f) => wantedTypes.includes(f.type!) && (!filesSubjectFilter || f.subject === filesSubjectFilter))

                      const bySubject = new Map<string, Map<string, Map<string, Entry[]>>>()
                      const general = new Map<string, Entry[]>()
                      relevant.forEach((f) => {
                        const entry: Entry = { path: f.p, date: f.date, label: f.label }
                        if (!f.subject) {
                          if (!general.has(f.type!)) general.set(f.type!, [])
                          general.get(f.type!)!.push(entry)
                          return
                        }
                        if (!bySubject.has(f.subject)) bySubject.set(f.subject, new Map())
                        const topics = bySubject.get(f.subject)!
                        const topicKey = f.topic || '—'
                        if (!topics.has(topicKey)) topics.set(topicKey, new Map())
                        const types = topics.get(topicKey)!
                        if (!types.has(f.type!)) types.set(f.type!, [])
                        types.get(f.type!)!.push(entry)
                      })
                      const byDateDesc = (a: Entry, b: Entry) => (b.date || '').localeCompare(a.date || '')
                      const renderEntries = (entries: Entry[]) => {
                        const isUploaded = filesView === 'uploaded'
                        const sorted = [...entries].sort(byDateDesc)
                        const imagePaths = sorted.filter((e) => IMAGE_PATH_RE.test(e.path)).map((e) => e.path)
                        return (
                          <div className={isUploaded ? 'fl-thumb-grid' : undefined}>
                            {sorted.map((e) => (
                              isUploaded && IMAGE_PATH_RE.test(e.path) ? (
                                <button key={e.path} type="button" className="fl-thumb-item" onClick={() => { setViewerImageList(imagePaths); setViewerPath(e.path) }}>
                                  <img className="fl-thumb-img" src={`${FAMILY_API}/api/workspace/raw?path=${encodeURIComponent(e.path)}`} alt="" loading="lazy" />
                                  <span className="fl-thumb-caption">{e.label}{e.date ? ` · ${e.date}` : ''}</span>
                                </button>
                              ) : (
                                <button key={e.path} type="button" className="fl-file-item" onClick={() => { setViewerImageList([]); setViewerPath(e.path) }}>
                                  <FileText size={16} />
                                  <span>{e.label}{e.date ? ` · ${e.date}` : ''}</span>
                                </button>
                              )
                            ))}
                          </div>
                        )
                      }
                      return (
                        <>
                          {subjectsList.length > 0 && (
                            <select
                              className="fl-subject-select"
                              aria-label="Filter by subject"
                              value={filesSubjectFilter}
                              onChange={(e) => setFilesSubjectFilter(e.target.value)}
                            >
                              <option value="">All subjects</option>
                              {subjectsList.map((s) => <option key={s} value={s}>{s}</option>)}
                            </select>
                          )}
                          {bySubject.size === 0 && general.size === 0 ? (
                            <p className="fl-note">
                              {filesView === 'uploaded'
                                ? 'No uploaded material yet.'
                                : 'Nothing here yet. Ask Quill to make study material or a test.'}
                            </p>
                          ) : (
                            <>
                              {Array.from(bySubject.entries()).map(([subj, topics]) => (
                                <section key={subj} className="fl-asset-group">
                                  <p className="fl-drawer-label">{subj}</p>
                                  {Array.from(topics.entries()).map(([top, types]) => (
                                    <div key={top} className="fl-asset-topic">
                                      <p className="fl-asset-topic-label">{top === '—' ? 'Other' : top}</p>
                                      {Array.from(types.entries()).map(([type, entries]) => (
                                        <div key={type} className="fl-asset-type">
                                          {filesView !== 'uploaded' && <p className="fl-asset-type-label">{type}</p>}
                                          {renderEntries(entries)}
                                        </div>
                                      ))}
                                    </div>
                                  ))}
                                </section>
                              ))}
                              {general.size > 0 && (
                                <section className="fl-asset-group">
                                  <p className="fl-drawer-label">General</p>
                                  {Array.from(general.entries()).map(([type, entries]) => (
                                    <div key={type} className="fl-asset-type">
                                      {filesView !== 'uploaded' && <p className="fl-asset-type-label">{type}</p>}
                                      {renderEntries(entries)}
                                    </div>
                                  ))}
                                </section>
                              )}
                            </>
                          )}
                        </>
                      )
                    })()}
                  </>
                )
              )}
            </div>
          </aside>

          {waOpen && (
            <div className="fl-wa-backdrop" role="dialog" aria-modal="true" onClick={() => setWaOpen(false)}>
              <div className="fl-wa" onClick={(e) => e.stopPropagation()}>
                <div className="fl-wa-head">
                  <span className="fl-wa-title">SparkQuill on WhatsApp <em>· preview</em></span>
                  <button className="fl-wa-close" type="button" onClick={() => setWaOpen(false)} aria-label="Close">×</button>
                </div>
                <div className="fl-wa-body">
                  {waMessages.length === 0 && <p className="fl-wa-hint">This previews how Quill would reply over WhatsApp. Type a message as {childName || 'your child'}’s parent.</p>}
                  {waMessages.map((m, i) => (
                    <div key={i} className={`fl-wa-msg ${m.role === 'user' ? 'is-me' : 'is-quill'}`}>{m.text}</div>
                  ))}
                  {waSending && <div className="fl-wa-msg is-quill">…</div>}
                </div>
                <form className="fl-wa-composer" onSubmit={(e) => { e.preventDefault(); sendWhatsApp(waInput) }}>
                  <input value={waInput} onChange={(e) => setWaInput(e.target.value)} placeholder="Message SparkQuill…" disabled={waSending} />
                  <button type="submit" disabled={waSending || !waInput.trim()} aria-label="Send"><Send size={16} /></button>
                </form>
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
          <div className="fl-child-body">
            <section className="fl-child-chat">
              <header className="fl-child-top">
                <div className="fl-child-id">
                  <img className="fl-header-logo" src="/sparkquill-mark.svg" alt="" width={30} height={30} />
                  <div className="fl-child-hi"><strong>Hi {childName || 'Maya'}!</strong><small>Let’s keep learning together</small></div>
                </div>
                <div className="fl-child-top-right">
                  {childStars > 0 && <span className="fl-star-badge"><Star size={15} fill="currentColor" strokeWidth={1.5} /> {childStars}</span>}
                  <button className="fl-parent-return" type="button" onClick={() => { setGateValue(''); setGateError(''); setPinGate(true) }}><LockKeyhole size={16} /> Parent Mode</button>
                </div>
              </header>
              <div className="fl-child-thread" aria-label="Tutor conversation">
                <div className="fl-tmsg is-tutor">
                  <span className="fl-tmsg-avatar"><Sun size={20} /></span>
                  <div className="fl-tbubble">Hi {childName || 'Maya'}! Ready to keep learning? Tell me what you’re working on, or ask me anything — I’ll help you figure it out step by step.</div>
                </div>
                {childMessages.map((m, i) => (
                  m.role === 'tool' && m.tool === 'celebrate' ? (
                    <div key={i} className="fl-celebration" role="status">
                      <span className="fl-celebration-stars">
                        {Array.from({ length: m.stars ?? 1 }, (_, si) => (
                          <Star key={si} className="fl-celebration-star" size={20} fill="currentColor" strokeWidth={1} style={{ animationDelay: `${si * 0.12}s` }} />
                        ))}
                      </span>
                      <span className="fl-celebration-text">{m.reason}</span>
                    </div>
                  ) : m.role === 'assistant' ? (
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
                  <div className="fl-thinking"><img src="/sparkquill-loader.svg" alt="" width={38} height={38} /> <span>{childLiveStatus ? `Quill is: ${childLiveStatus}…` : CHILD_WAIT_HINTS[childHintIndex]}</span></div>
                )}
                <div ref={childThreadEndRef} />
              </div>
              {childSuggestions.length > 0 && !childSending && (
                <div className="fl-child-actions" aria-label="Quick replies">
                  {childSuggestions.map((s, i) => {
                    const safeHtml = s.html ? sanitizeDecorativeHtml(s.html) : ''
                    return (
                      <button key={i} type="button" className={`tone-${s.tone || 'neutral'}`} onClick={() => sendChildText(s.message)}>
                        {s.emoji && <span className="fl-pill-emoji">{s.emoji}</span>}
                        {safeHtml ? <span dangerouslySetInnerHTML={{ __html: safeHtml }} /> : <span>{s.label}</span>}
                      </button>
                    )
                  })}
                </div>
              )}
              <form className="fl-child-composer" onSubmit={sendChildMessage}>
                <input aria-label="Message your tutor" placeholder="Type your answer or ask for help…" value={childInput} onChange={(e) => setChildInput(e.target.value)} disabled={childSending} />
                <button className="composer-send" type="submit" aria-label="Send message" disabled={childSending}><Send size={18} /></button>
              </form>
            </section>
            <aside className="fl-child-side">
              <div className="fl-child-side-scroll">
              {childViewerPath ? (
                <div className="fl-viewer">
                  <div className="fl-viewer-bar">
                    <button className="fl-viewer-back" type="button" onClick={() => setChildViewerPath(null)}><ArrowLeft size={15} /> Back</button>
                    <span className="fl-viewer-name">{parseAssetPath(childViewerPath).label}</span>
                    <button
                      className="fl-icon-btn"
                      type="button"
                      aria-label="Refresh"
                      title="Reload this page"
                      onClick={() => setChildViewerRefreshKey((k) => k + 1)}
                    >
                      <RefreshCw size={14} />
                    </button>
                  </div>
                  {IMAGE_PATH_RE.test(childViewerPath) ? (
                    <img className="fl-viewer-img" src={`${FAMILY_API}/api/workspace/raw?path=${encodeURIComponent(childViewerPath)}`} alt="" />
                  ) : !childViewerContent ? (
                    <p className="fl-note">Loading…</p>
                  ) : !childViewerContent.isText ? (
                    <p className="fl-note">Can’t show this one here.</p>
                  ) : (childViewerPath.endsWith('.html') || childViewerPath.endsWith('.htm')) ? (
                    <iframe className="fl-viewer-frame" title="Preview" sandbox="allow-scripts" srcDoc={childViewerContent.content} />
                  ) : childViewerPath.endsWith('.md') ? (
                    <div className="fl-viewer-md"><Markdown text={childViewerContent.content} /></div>
                  ) : (
                    <pre className="fl-viewer-pre">{childViewerContent.content}</pre>
                  )}
                </div>
              ) : (
                <>
                  {(() => {
                    // A package's manifest and its listed items are shown as ONE
                    // package card, not as the raw manifest file plus every item
                    // repeated again as its own separate entry.
                    const packagedPaths = new Set(childPackages.flatMap((pkg) => pkg.items))
                    const manifestPaths = new Set(childPackages.map((pkg) => pkg.path))
                    const materials = childFiles.filter((p) => p.startsWith('shared/') && !manifestPaths.has(p) && !packagedPaths.has(p))
                    const attempts = childFiles.filter((p) => p.startsWith('child/attempts/'))
                    if (childPackages.length === 0 && materials.length === 0 && attempts.length === 0) {
                      return <p className="fl-child-note"><Sparkles size={15} /> Ask Quill what to work on next!</p>
                    }
                    return (
                      <>
                        {childPackages.length > 0 && (
                          <section className="fl-asset-group">
                            <p className="fl-drawer-label">From your parent</p>
                            {childPackages.map((pkg) => (
                              <button
                                key={pkg.path}
                                type="button"
                                className="fl-file-item is-package"
                                onClick={() => {
                                  if (pkg.items.length > 0) {
                                    // A real file to show — open it directly, never the raw manifest.
                                    setChildViewerPath(pkg.items[0])
                                  } else {
                                    // Instruction-only package: there's no file at all, just a live
                                    // activity — kick it off in chat instead of trying to "open" anything.
                                    setChildViewerPath(null)
                                    sendChildText(`Let's start ${pkg.title}!`)
                                  }
                                }}
                              >
                                <BookOpen size={16} /><span>{pkg.title}<small>{pkg.items.length > 0 ? `${pkg.items.length} part${pkg.items.length === 1 ? '' : 's'}` : 'Adaptive practice'}</small></span>
                              </button>
                            ))}
                          </section>
                        )}
                        {materials.length > 0 && (
                          <section className="fl-asset-group">
                            <p className="fl-drawer-label">Your materials</p>
                            {materials.map((p) => {
                              const { label, date } = parseAssetPath(p)
                              return (
                                <button key={p} type="button" className="fl-file-item" onClick={() => setChildViewerPath(p)}>
                                  <FileText size={16} /><span>{label}{date ? ` · ${date}` : ''}</span>
                                </button>
                              )
                            })}
                          </section>
                        )}
                        {attempts.length > 0 && (
                          <section className="fl-asset-group">
                            <p className="fl-drawer-label">Your work</p>
                            {attempts.map((p) => {
                              const { label, date } = parseAssetPath(p)
                              return (
                                <button key={p} type="button" className="fl-file-item" onClick={() => setChildViewerPath(p)}>
                                  <FileText size={16} /><span>{label}{date ? ` · ${date}` : ''}</span>
                                </button>
                              )
                            })}
                          </section>
                        )}
                      </>
                    )
                  })()}
                </>
              )}
              </div>
            </aside>
          </div>
          {pinGate && (
            <div className="fl-signoff-backdrop" role="dialog" aria-modal="true" aria-labelledby="fl-gate-title">
              <div className="fl-signoff-card">
                <span className="fl-signoff-icon"><LockKeyhole size={22} /></span>
                <h2 id="fl-gate-title">Enter parent PIN</h2>
                <p>Parent Mode is protected. Enter your PIN to return.</p>
                <input
                  className="fl-gate-input"
                  type="password"
                  inputMode="numeric"
                  autoFocus
                  value={gateValue}
                  onChange={(e) => setGateValue(e.target.value.replace(/\D/g, '').slice(0, 8))}
                  onKeyDown={(e) => { if (e.key === 'Enter') submitPinGate() }}
                  placeholder="PIN"
                />
                {gateError && <p className="pin-error"><LockKeyhole size={16} /> {gateError}</p>}
                <div className="fl-signoff-actions">
                  <button className="fl-ghost-btn" type="button" onClick={() => { setPinGate(false); setGateValue('') }}>Cancel</button>
                  <button className="primary-button" type="button" onClick={submitPinGate} disabled={!gateValue}>Unlock <ArrowRight size={18} /></button>
                </div>
              </div>
            </div>
          )}
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
