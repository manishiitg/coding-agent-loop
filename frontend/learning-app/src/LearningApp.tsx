import { useState, useEffect, useRef, type FormEvent, type ChangeEvent } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import {
  Activity,
  ArrowLeft,
  ArrowRight,
  BookOpen,
  Check,
  CheckCircle2,
  ExternalLink,
  FileArchive,
  FileCode,
  FileSpreadsheet,
  FileText,
  FileType,
  Film,
  Folder,
  FolderOpen,
  Image as ImageIcon,
  Info,
  LockKeyhole,
  Music,
  Presentation,
  PanelLeftClose,
  PanelLeftOpen,
  Paperclip,
  Plus,
  Printer,
  RefreshCw,
  Send,
  Settings as SettingsIcon,
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
  type LearningPackage,
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

// Absolute date + time label for a package, e.g. "21 Jul 2026, 5:42 PM".
function dateTimeLabel(iso?: string): string {
  if (!iso) return ''
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return ''
  return new Date(t).toLocaleString(undefined, { day: 'numeric', month: 'short', year: 'numeric', hour: 'numeric', minute: '2-digit', hour12: true })
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
  'Tip: connect Gmail in Connectors, then just ask "did the school email anything?" — Quill checks for you.',
  'Tip: link WhatsApp in Connectors to chat with Quill from your phone, and get check-ins there.',
  'Tip: set up the Browser connector and Quill can peek at your school portal for new assignments.',
  'Tip: turn on Pulse (top bar) and Quill checks in on its own — reviewing progress and the school portal.',
  'Tip: just mention things in passing — "her exam is next Friday", "she gets anxious with timers" — Quill remembers and applies them later.',
]
const CHILD_WAIT_HINTS = [
  'Tip: stuck? Just say "give me a hint!"',
  'Tip: you can ask Quill to explain it a different way.',
  'Tip: tell Quill your answer — it will tell you if you got it right.',
  'Tip: ask for an example if a question feels tricky.',
  'Tip: you can ask Quill anything about what you\'re learning, not just the current question.',
  'Tip: ask a parent to set up WhatsApp so you can practice with Quill on the phone too!',
  'Tip: ask a parent to turn on Pulse so Quill keeps track of how you\'re doing.',
  'Tip: ask a parent to connect the school portal so Quill can help with your assignments.',
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
              <summary className="fl-tree-dir">
                <Folder className="fl-tree-icon is-closed" size={15} />
                <FolderOpen className="fl-tree-icon is-open" size={15} />
                <span>{n.name}</span>
              </summary>
              {n.children && <FileTree nodes={n.children} onOpen={onOpen} depth={depth + 1} />}
            </details>
          ) : (
            <button className="fl-tree-file" type="button" onClick={() => onOpen(n.path)}>
              <FileGlyph name={n.name} size={14} />
              <span>{n.name}</span>
            </button>
          )}
        </li>
      ))}
    </ul>
  )
}
const IMAGE_PATH_RE = /\.(png|jpe?g|gif|webp|svg|bmp)$/i

// isPrintable — the viewer shows a print button for documents worth printing:
// HTML pages (tests, study material, reports) and Markdown (which some tests /
// package items are). Images/PDFs use the browser's own controls; other files
// aren't previewed.
function isPrintable(path: string): boolean {
  return /\.(html?|md|markdown)$/i.test(path)
}

// printFile prints a viewer file. HTML opens in a new tab that auto-prints
// (?print=1 on the raw endpoint) — robust: it doesn't depend on the generated
// HTML embedding a print handler (a skill can forget to) and isn't blocked by
// the viewer iframe's sandbox. Markdown/text is rendered in-page as React, so it
// falls back to printViewerContent (CSS-isolated window.print).
function printFile(path: string) {
  if (/\.(html?)$/i.test(path)) {
    window.open(`${FAMILY_API}/api/workspace/raw?path=${encodeURIComponent(path)}&print=1`, '_blank', 'noopener,noreferrer')
  } else {
    printViewerContent()
  }
}

// printViewerContent prints the open viewer's in-page rendered content (Markdown
// / plain text) by flagging the document and letting an @media print rule
// isolate .fl-viewer-md / .fl-viewer-pre before printing.
function printViewerContent() {
  const root = document.documentElement
  root.classList.add('fl-printing')
  const cleanup = () => { root.classList.remove('fl-printing'); window.removeEventListener('afterprint', cleanup) }
  window.addEventListener('afterprint', cleanup)
  window.print()
}

// FileGlyph renders a file-type icon coloured by extension, so the workspace
// shows a PDF/Word/PowerPoint/Excel/image/archive at a glance rather than one
// generic sheet-of-paper for everything.
function fileGlyphFor(name: string): { Icon: typeof FileText; kind: string } {
  const ext = (name.split('.').pop() || '').toLowerCase()
  if (['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg', 'bmp', 'heic'].includes(ext)) return { Icon: ImageIcon, kind: 'image' }
  if (ext === 'pdf') return { Icon: FileType, kind: 'pdf' }
  if (['doc', 'docx', 'rtf', 'odt'].includes(ext)) return { Icon: FileText, kind: 'doc' }
  if (['ppt', 'pptx', 'odp'].includes(ext)) return { Icon: Presentation, kind: 'ppt' }
  if (['xls', 'xlsx', 'csv', 'ods', 'tsv'].includes(ext)) return { Icon: FileSpreadsheet, kind: 'sheet' }
  if (['zip', 'tar', 'gz', 'tgz', 'rar', '7z'].includes(ext)) return { Icon: FileArchive, kind: 'zip' }
  if (['html', 'htm'].includes(ext)) return { Icon: FileCode, kind: 'html' }
  if (['mp4', 'mov', 'avi', 'mkv', 'webm'].includes(ext)) return { Icon: Film, kind: 'video' }
  if (['mp3', 'wav', 'm4a', 'aac', 'ogg', 'flac'].includes(ext)) return { Icon: Music, kind: 'audio' }
  return { Icon: FileText, kind: 'file' }
}
function FileGlyph({ name, size = 16 }: { name: string; size?: number }) {
  const { Icon, kind } = fileGlyphFor(name)
  return <Icon size={size} className={`fl-glyph fl-glyph-${kind}`} />
}

// FileMetaPanel renders the sidecar metadata (<path>.meta.json) the process-file
// skill writes for a filed document — what Quill understood the file to be. Only
// the parent-meaningful fields are shown, in plain language (no raw JSON, no
// paths), consistent with the rest of the parent UI.
function FileMetaPanel({ meta }: { meta: Record<string, unknown> }) {
  const str = (k: string): string => (typeof meta[k] === 'string' ? (meta[k] as string).trim() : '')
  const concepts = Array.isArray(meta.key_concepts)
    ? (meta.key_concepts as unknown[]).filter((c): c is string => typeof c === 'string' && c.trim() !== '')
    : []
  const summary = str('summary')
  const subject = str('subject')
  const topic = str('topic')
  const type = str('type')
  const chips = [subject, topic, type].filter(Boolean)
  if (!summary && chips.length === 0 && concepts.length === 0) return null
  return (
    <div className="fl-meta-panel">
      <p className="fl-meta-title"><Info size={13} /> What Quill knows about this file</p>
      {chips.length > 0 && (
        <div className="fl-meta-chips">
          {chips.map((c, i) => <span key={i} className="fl-meta-chip">{c}</span>)}
        </div>
      )}
      {summary && <p className="fl-meta-summary">{summary}</p>}
      {concepts.length > 0 && (
        <div className="fl-meta-concepts">
          {concepts.map((c, i) => <span key={i} className="fl-meta-concept">{c}</span>)}
        </div>
      )}
    </div>
  )
}

// NonPreviewableFile is shown for files the browser can't display inline (Word,
// PowerPoint, spreadsheets, archives, …). We deliberately don't try to convert
// them — just show what Quill knows about the file (its metadata, if any) and a
// Download button to open it on the device. Keeps preview simple: images, PDFs,
// and HTML/text render inline; everything else is metadata + download.
function NonPreviewableFile({ path, meta }: { path: string; meta: Record<string, unknown> | null }) {
  const name = path.split('/').pop() || path
  return (
    <div className="fl-nopreview">
      <div className="fl-nopreview-head">
        <FileGlyph name={name} size={34} />
        <span className="fl-nopreview-name">{name}</span>
      </div>
      {meta ? (
        <FileMetaPanel meta={meta} />
      ) : (
        <p className="fl-note">This kind of file can’t be shown here — download it to open on your device.</p>
      )}
      <a
        className="fl-download-btn"
        href={`${FAMILY_API}/api/workspace/raw?path=${encodeURIComponent(path)}&download=1`}
        download={name}
      >
        Download
      </a>
    </div>
  )
}

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
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [savingEngine, setSavingEngine] = useState(false)
  const waOpen = useWhatsAppStore((s) => s.waOpen)
  const setWaOpen = useWhatsAppStore((s) => s.setWaOpen)
  const [connectorSection, setConnectorSection] = useState<'whatsapp' | 'gmail' | 'browser'>('whatsapp')
  const [waStatus, setWaStatus] = useState<{ paired: boolean; connected: boolean; qr_available: boolean; own_jid?: string } | null>(null)
  const [waQrNonce, setWaQrNonce] = useState(0)
  const [waUnpairing, setWaUnpairing] = useState(false)
  const [gmailStatus, setGmailStatus] = useState<{ connected: boolean; email?: string } | null>(null)
  const [gmailTesting, setGmailTesting] = useState(false)
  const [gmailTestResult, setGmailTestResult] = useState<string | null>(null)
  const [browserStatus, setBrowserStatus] = useState<{ cli_installed: boolean } | null>(null)
  const [browserCopied, setBrowserCopied] = useState(false)
  const [pulseConfig, setPulseConfig] = useState<{ enabled: boolean; cadence_hours: number; last_run_at?: string; school_gmail_query?: string; watch_sites?: string[]; notify_emails?: string[] } | null>(null)
  const [savingPulse, setSavingPulse] = useState(false)
  const [schoolQueryDraft, setSchoolQueryDraft] = useState('')
  const [watchSitesDraft, setWatchSitesDraft] = useState('')
  const [notifyEmailsDraft, setNotifyEmailsDraft] = useState('')
  const [pulseSaved, setPulseSaved] = useState(false)
  const [pulsePopoverOpen, setPulsePopoverOpen] = useState(false)
  const [pendingConvUpdate, setPendingConvUpdate] = useState<StoredMsg[] | null>(null)
  // Messages the parent typed while a turn was still processing — sent one at
  // a time as the current turn finishes (see the drain effect). Shown as
  // "queued" bubbles so they know it's coming.
  const [queue, setQueue] = useState<string[]>([])
  const [pulseRunning, setPulseRunning] = useState(false)
  const [pulseRunError, setPulseRunError] = useState<string | null>(null)
  const wsFiles = useWorkspaceStore((s) => s.wsFiles)
  const setWsFiles = useWorkspaceStore((s) => s.setWsFiles)
  const allFiles = useWorkspaceStore((s) => s.allFiles)
  const setAllFiles = useWorkspaceStore((s) => s.setAllFiles)
  // The parent has ONE ongoing conversation with Quill — web, WhatsApp, and
  // Pulse all share this single "parent" thread (matching the backend's
  // parentConversationID). No multi-conversation list, so the id is fixed.
  const conversationId = 'parent'
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
        // Resume the single parent conversation (once) so the parent continues
        // where they left off — including anything that arrived via WhatsApp or
        // Pulse, since it's all the same thread now.
        if (!resumedRef.current && parentMessages.length === 0) {
          resumedRef.current = true
          fetch(`${FAMILY_API}/api/workspace/file?path=${encodeURIComponent('parent/conversations/parent.json')}`)
            .then((r) => r.json())
            .then((dd) => {
              if (!dd?.content) return
              const c = JSON.parse(dd.content) as { messages?: StoredMsg[] }
              setParentMessages((c.messages || []).map(toParentMsg))
            })
            .catch(() => {})
        }
        // The child's own sessions still power the child-mode resume below.
        const convPaths = files.filter((f) => f.path.includes('child/conversations/') && f.path.endsWith('.json')).map((f) => f.path)
        const childMetas = await Promise.all(convPaths.map(async (p) => {
          try {
            const d = await fetch(`${FAMILY_API}/api/workspace/file?path=${encodeURIComponent(p)}`).then((r) => r.json())
            const c = JSON.parse(d.content) as { id: string; updated_at?: string }
            return { id: c.id, updated: c.updated_at || '' } as { id: string; updated: string }
          } catch { return null }
        }))
        if (cancelled) return
        const childConvs = childMetas.filter((m): m is { id: string; updated: string } => m !== null)
          .sort((a, b) => b.updated.localeCompare(a.updated))
        // Same for the child's own conversation — without this, every page
        // refresh silently started a brand-new child session (fresh cold-start
        // agent turn, empty visible thread, lost stars/celebration history)
        // even though the screen itself now correctly stays on "tutor".
        if (!childResumedRef.current && childMessages.length === 0 && childConvs.length > 0) {
          childResumedRef.current = true
          const top = childConvs[0]
          fetch(`${FAMILY_API}/api/workspace/file?path=${encodeURIComponent(`child/conversations/${top.id}.json`)}`)
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
  const childFiles = useChildChatStore((s) => s.childFiles)
  const setChildFiles = useChildChatStore((s) => s.setChildFiles)
  const childPackages = useChildChatStore((s) => s.childPackages)
  const setChildPackages = useChildChatStore((s) => s.setChildPackages)
  // The child's CURRENT assignment (child/current-task.json) — the one package
  // the parent most recently handed off. The child workspace shows only this,
  // not every package/material ever approved.
  const [childTask, setChildTask] = useState<{ title?: string; package?: string; items?: string[] } | null>(null)
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
  const packages = useWorkspaceStore((s) => s.packages)
  const setPackages = useWorkspaceStore((s) => s.setPackages)
  const viewerPath = useWorkspaceStore((s) => s.viewerPath)
  const setViewerPath = useWorkspaceStore((s) => s.setViewerPath)
  const viewerRefreshKey = useWorkspaceStore((s) => s.viewerRefreshKey)
  const setViewerRefreshKey = useWorkspaceStore((s) => s.setViewerRefreshKey)
  const viewerImageList = useWorkspaceStore((s) => s.viewerImageList)
  const setViewerImageList = useWorkspaceStore((s) => s.setViewerImageList)
  const viewerContent = useWorkspaceStore((s) => s.viewerContent)
  const setViewerContent = useWorkspaceStore((s) => s.setViewerContent)
  const [viewerMeta, setViewerMeta] = useState<Record<string, unknown> | null>(null)
  const [metaOpen, setMetaOpen] = useState(false)
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

  // Learning packages the parent has bundled for the child — refetched whenever
  // the Subjects tab is open or a turn just completed (Quill may have created one).
  useEffect(() => {
    if (filesView !== 'subjects') return
    let cancelled = false
    fetch(`${FAMILY_API}/api/parent/packages`)
      .then((r) => r.json())
      .then((d: LearningPackage[]) => { if (!cancelled) setPackages(d ?? []) })
      .catch(() => { if (!cancelled) setPackages([]) })
    return () => { cancelled = true }
  }, [filesView, mapRefreshKey])

  // Poll real WhatsApp pairing status while the connector modal's WhatsApp
  // section is open — refreshes the QR (it's short-lived) until paired.
  useEffect(() => {
    if (!waOpen || connectorSection !== 'whatsapp') return
    let cancelled = false
    const poll = () => {
      fetch(`${FAMILY_API}/api/whatsapp/status`)
        .then((r) => r.json())
        .then((d: { paired: boolean; connected: boolean; qr_available: boolean; own_jid?: string }) => {
          if (cancelled) return
          setWaStatus(d)
          if (!d.paired) setWaQrNonce((n) => n + 1)
        })
        .catch(() => {})
    }
    poll()
    const id = window.setInterval(poll, 3000)
    return () => { cancelled = true; window.clearInterval(id) }
  }, [waOpen, connectorSection])

  // Gmail connection status (via the gws CLI, already authenticated on this
  // machine) — checked once whenever the Gmail section is opened.
  useEffect(() => {
    if (!waOpen || connectorSection !== 'gmail') return
    let cancelled = false
    setGmailTestResult(null)
    fetch(`${FAMILY_API}/api/gmail/status`)
      .then((r) => r.json())
      .then((d: { connected: boolean; email?: string }) => { if (!cancelled) setGmailStatus(d) })
      .catch(() => { if (!cancelled) setGmailStatus({ connected: false }) })
    return () => { cancelled = true }
  }, [waOpen, connectorSection])

  const sendGmailTest = () => {
    setGmailTesting(true)
    setGmailTestResult(null)
    fetch(`${FAMILY_API}/api/gmail/test`, { method: 'POST' })
      .then((r) => r.json())
      .then((d: { ok?: boolean; sent_to?: string; error?: string }) => {
        setGmailTestResult(d.ok ? `Sent — check ${d.sent_to}` : `Failed: ${d.error || 'unknown error'}`)
      })
      .catch(() => setGmailTestResult('Failed: could not reach SparkQuill.'))
      .finally(() => setGmailTesting(false))
  }

  // Browser connector — just a one-time CLI-install check; whether a CDP
  // Chrome is actually reachable is decided by agent-browser itself per call.
  useEffect(() => {
    if (!waOpen || connectorSection !== 'browser') return
    let cancelled = false
    fetch(`${FAMILY_API}/api/browser/status`)
      .then((r) => r.json())
      .then((d: { cli_installed: boolean }) => { if (!cancelled) setBrowserStatus(d) })
      .catch(() => { if (!cancelled) setBrowserStatus({ cli_installed: false }) })
    return () => { cancelled = true }
  }, [waOpen, connectorSection])

  // Pulse config — loaded on entering the parent screen (so the header pill
  // reflects real status right away) and refreshed whenever Settings or the
  // pill's own popover opens.
  useEffect(() => {
    if (screen !== 'parent' && !settingsOpen && !pulsePopoverOpen) return
    let cancelled = false
    fetch(`${FAMILY_API}/api/pulse/config`)
      .then((r) => r.json())
      .then((d: { enabled: boolean; cadence_hours: number; last_run_at?: string; school_gmail_query?: string; watch_sites?: string[]; notify_emails?: string[] }) => {
        if (cancelled) return
        setPulseConfig(d)
        setSchoolQueryDraft(d.school_gmail_query || '')
        setWatchSitesDraft((d.watch_sites || []).join('\n'))
        setNotifyEmailsDraft((d.notify_emails || []).join(', '))
      })
      .catch(() => {})
    return () => { cancelled = true }
  }, [screen, settingsOpen, pulsePopoverOpen])

  // Pick up async updates to the open conversation — Pulse (or a WhatsApp
  // reply, if this same conversation is the real WhatsApp thread) can append
  // a new message to this exact conversation file from the background, with
  // nothing else telling this open tab to know. Poll lightly and, if the
  // file has grown since we last rendered it, surface a small "new update"
  // banner rather than silently rewriting the screen under the parent —
  // they choose when to pull it in. Skip while a send is in flight.
  useEffect(() => {
    if (screen !== 'parent' || !conversationId) return
    const id = window.setInterval(() => {
      if (sending) return
      fetch(`${FAMILY_API}/api/workspace/file?path=${encodeURIComponent(`parent/conversations/${conversationId}.json`)}`)
        .then((r) => r.json())
        .then((d) => {
          if (!d?.content) return
          const c = JSON.parse(d.content) as { messages?: StoredMsg[] }
          const fresh = c.messages || []
          if (fresh.length > parentMessages.length) {
            setPendingConvUpdate(fresh)
          }
        })
        .catch(() => {})
    }, 20000)
    return () => window.clearInterval(id)
  }, [screen, conversationId, sending, parentMessages.length])

  // Clear any pending "new update" banner whenever the parent switches
  // conversations or sends their own message — it only ever refers to the
  // specific conversation/point in time it was detected for.
  useEffect(() => { setPendingConvUpdate(null) }, [conversationId])

  // Drain the send queue: once the current turn finishes, send the next queued
  // message. One at a time, in order — so the transcript stays well-formed and
  // each reply builds on the previous. sendParentText itself flips `sending`
  // back on, which re-guards this until that turn also completes.
  useEffect(() => {
    if (sending || queue.length === 0) return
    const [next, ...rest] = queue
    setQueue(rest)
    sendParentText(next)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sending, queue])

  const savePulseConfig = (patch: { enabled?: boolean; cadence_hours?: number; school_gmail_query?: string; watch_sites?: string[]; notify_emails?: string[] }) => {
    setSavingPulse(true)
    fetch(`${FAMILY_API}/api/pulse/config`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(patch),
    })
      .then((r) => r.json())
      .then((d) => setPulseConfig(d))
      .catch(() => {})
      .finally(() => setSavingPulse(false))
  }

  // Runs Pulse right now (regardless of the recurring toggle) — used to test
  // it without waiting for the ticker. Fires the request, then polls config
  // and watches last_run_at change to know when the real turn (which can
  // take a few minutes) has finished.
  const runPulseNow = () => {
    const before = pulseConfig?.last_run_at
    setPulseRunError(null)
    setPulseRunning(true)
    fetch(`${FAMILY_API}/api/pulse/run`, { method: 'POST' })
      .then((r) => r.json().then((d) => ({ ok: r.ok, d })))
      .then(({ ok, d }) => {
        if (!ok) { setPulseRunError(d.error || 'Could not start.'); setPulseRunning(false); return }
        const poll = (attempt: number) => {
          if (attempt > 300) { setPulseRunning(false); setPulseRunError('Taking longer than expected — check back shortly.'); return }
          fetch(`${FAMILY_API}/api/pulse/config`)
            .then((r) => r.json())
            .then((cfg) => {
              setPulseConfig(cfg)
              if (cfg.last_run_at && cfg.last_run_at !== before) {
                setPulseRunning(false)
              } else {
                window.setTimeout(() => poll(attempt + 1), 4000)
              }
            })
            .catch(() => window.setTimeout(() => poll(attempt + 1), 4000))
        }
        window.setTimeout(() => poll(0), 4000)
      })
      .catch(() => { setPulseRunError('Could not reach SparkQuill.'); setPulseRunning(false) })
  }

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
              createdAt: typeof pkg.created_at === 'string' ? pkg.created_at : undefined,
            }
          } catch { return null }
        })
        .catch(() => null)
    )).then((results) => { if (!cancelled) setChildPackages(results.filter((r): r is { path: string; title: string; items: string[]; guideNote?: string; createdAt?: string } => r !== null)) })
    return () => { cancelled = true }
  }, [childFiles])

  // The child's current assignment — read child/current-task.json so the child
  // workspace can show ONLY the package the parent just handed off (its items,
  // already mirrored into child/active), not every package/material.
  useEffect(() => {
    if (screen !== 'tutor') return
    let cancelled = false
    fetch(`${FAMILY_API}/api/workspace/file?path=${encodeURIComponent('child/current-task.json')}`)
      .then((r) => r.json())
      .then((d: { content?: string; is_text?: boolean }) => {
        if (cancelled) return
        if (!d?.is_text || !d.content) { setChildTask(null); return }
        try {
          const t = JSON.parse(d.content)
          setChildTask({
            title: typeof t.title === 'string' ? t.title : undefined,
            package: typeof t.package === 'string' ? t.package : undefined,
            items: Array.isArray(t.items) ? t.items.filter((x: unknown): x is string => typeof x === 'string') : [],
          })
        } catch { setChildTask(null) }
      })
      .catch(() => { if (!cancelled) setChildTask(null) })
    return () => { cancelled = true }
  }, [screen, childTreeRefreshKey])

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

  // Probe for the file's metadata sidecar (<path>.meta.json — written by the
  // process-file skill when Quill files an upload: subject, topic, type, a short
  // summary, key concepts). When present, the viewer shows an info button that
  // reveals it, so the parent can see what Quill understood a document to be.
  useEffect(() => {
    setViewerMeta(null)
    setMetaOpen(false)
    if (!viewerPath || viewerPath.endsWith('.meta.json')) return
    let cancelled = false
    fetch(`${FAMILY_API}/api/workspace/file?path=${encodeURIComponent(viewerPath + '.meta.json')}`)
      .then((r) => r.json())
      .then((d) => {
        if (cancelled || !d || !d.is_text || !d.content) return
        try { setViewerMeta(JSON.parse(d.content) as Record<string, unknown>) } catch { /* not valid meta; ignore */ }
      })
      .catch(() => {})
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
    const id = window.setInterval(() => setParentHintIndex((i) => (i + 1) % PARENT_WAIT_HINTS.length), 7000)
    return () => window.clearInterval(id)
  }, [sending])
  const [childHintIndex, setChildHintIndex] = useState(0)
  useEffect(() => {
    if (!childSending) { setChildHintIndex(0); return }
    const id = window.setInterval(() => setChildHintIndex((i) => (i + 1) % CHILD_WAIT_HINTS.length), 7000)
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
        .then((data: { next_step?: string; engine?: string; child?: { name?: string; grade?: string; board?: string } | null; parent_label?: string }) => {
          if (cancelled) return
          if (data.engine) setEngine(data.engine)
          if (data.child) {
            if (data.child.name) setChildName(data.child.name)
            if (data.child.grade) setGrade(data.child.grade)
            if (data.child.board) setBoard(data.child.board)
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
    if (!text) return
    // A turn is already running — don't drop the message, queue it. The drain
    // effect sends it once the current turn finishes.
    if (sending) {
      setQueue((q) => [...q, text])
      setFocusInput('')
      return
    }
    const next: ParentMsg[] = [...parentMessages, { role: 'user', text }]
    setParentMessages(next)
    setFocusInput('')
    setSuggestions([])
    setPendingHandoff(null)
    // Drop any pending "new update" banner — the parent's own send supersedes
    // it, and applying the stale pre-send snapshot would wipe out the message
    // they just typed (a real bug this caused). Their send + reply, and the
    // next poll, bring things current anyway.
    setPendingConvUpdate(null)
    setSending(true)
    setLiveStatus('')
    // Live "what Quill is doing right now" line, sourced from the same turn's
    // tool handlers — purely cosmetic, so a stream error is silently ignored.
    const statusSource = new EventSource(`${FAMILY_API}/api/parent/status?conversation_id=${encodeURIComponent(conversationId)}`)
    statusSource.onmessage = (ev) => setLiveStatus(ev.data)
    statusSource.onerror = () => statusSource.close()
    // Keep source on each message so Pulse/etc. tags survive the round-trip and
    // don't get flattened to plain replies when this turn re-persists history.
    const history = next.filter((m) => m.role === 'user' || m.role === 'assistant').map((m) => ({ role: m.role, text: m.text ?? '', source: m.source }))
    fetch(`${FAMILY_API}/api/parent/message`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ messages: history, conversation_id: conversationId }),
    })
      .then((res) => res.json())
      .then((data: { reply?: string; error?: string; suggestions?: { label: string; message: string }[]; tool_events?: { tool: string; name?: string; grade?: string; board?: string; path?: string; parent_label?: string }[]; handoff?: { label: string; path: string; manifest?: string } }) => {
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

  // Real WhatsApp connection (whatsmeow QR pairing) — see whatsapp_bot.go.
  // Once paired, incoming messages in the linked account's own "Message
  // Yourself" chat are handled directly by the backend event handler; there
  // is no frontend send path for real WhatsApp messages.
  const unpairWhatsApp = () => {
    if (!window.confirm('Unlink SparkQuill from WhatsApp? You can always re-pair by scanning a new QR code.')) return
    setWaUnpairing(true)
    fetch(`${FAMILY_API}/api/whatsapp/unpair`, { method: 'POST' })
      .then((r) => r.json())
      .then(() => { setWaStatus(null); setWaQrNonce((n) => n + 1) })
      .finally(() => setWaUnpairing(false))
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

  // Same handoff, but for a whole learning package at once (its manifest path)
  // — approves every item in the bundle in one call (create_learning_package
  // already did this when the package was made; this re-triggers it from the
  // Files browser, e.g. to hand off a package made earlier in the conversation).
  const startPackageHandoff = (manifest: string) => {
    fetch(`${FAMILY_API}/api/parent/handoff`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ manifest }),
    })
      .then((res) => res.json())
      .then((data: { new_session?: boolean; path?: string }) => {
        if (!data.path) return
        enterChildModeAfterHandoff(!!data.new_session, handoffGreeting('set up something new for me to work on'), data.path)
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

  const childFileInputRef = useRef<HTMLInputElement>(null)
  const [childUploading, setChildUploading] = useState(false)

  const onPickChildFiles = () => childFileInputRef.current?.click()

  // A photo of the child's own work — lands in child/inbox/ (their own sandbox,
  // not shared/) so Quill can see it immediately with no parent approval step.
  // Auto-triggers a turn afterward (as if the child said so) since a kid won't
  // reliably know to say "look at this" right after picking a photo.
  const onChildFilesSelected = (event: ChangeEvent<HTMLInputElement>) => {
    const files = event.target.files
    if (!files || files.length === 0) return
    setChildUploading(true)
    const jobs = Array.from(files).map((f) => {
      const fd = new FormData()
      fd.append('file', f)
      fd.append('scope', 'child')
      return fetch(`${FAMILY_API}/api/upload`, { method: 'POST', body: fd })
        .then((res) => res.json())
        .then((data: { name?: string; error?: string }) => ({ name: data.name || f.name, error: data.error }))
        .catch(() => ({ name: f.name, error: 'upload failed' }))
    })
    Promise.all(jobs)
      .then((results) => {
        const cards: ParentMsg[] = results.map((r) => ({ role: 'tool', tool: r.error ? 'upload_error' : 'upload', name: r.name }))
        const ok = results.some((r) => !r.error)
        const next = [...childMessages, ...cards]
        setChildMessages(next)
        if (ok) sendChildText('I just uploaded a photo of my work — can you take a look?', next)
      })
      .finally(() => {
        setChildUploading(false)
        if (childFileInputRef.current) childFileInputRef.current.value = ''
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
        <div className="fl-shell" data-rail="closed" data-drawer={drawerOpen ? 'open' : 'closed'}>
          <section className="fl-center">
            <div className="fl-toolbar">
              <div className="fl-toolbar-left">
                <img className="fl-header-logo" src="/sparkquill-mark.svg" alt="" width={30} height={30} />
                <div className="fl-toolbar-title">
                  <strong className="fl-brand-word">Spark<span>Quill</span></strong>
                  <span>{childName || 'Your child'}{grade ? ` · Grade ${grade}` : ''}{board ? ` · ${board}` : ''}</span>
                </div>
              </div>
              <div className="fl-toolbar-right">
                <div className="fl-pulse-wrap">
                  <button
                    className="fl-pulse-pill"
                    type="button"
                    aria-label="Pulse"
                    title="Pulse"
                    onClick={() => setPulsePopoverOpen((v) => !v)}
                  >
                    <Activity size={14} />
                    <span>Pulse</span>
                    <span className={`fl-dot ${pulseConfig?.enabled ? 'is-ready' : ''}`} />
                  </button>
                  {pulsePopoverOpen && (
                    <>
                    <div className="fl-pulse-backdrop" onClick={() => setPulsePopoverOpen(false)} />
                    <div className="fl-pulse-popover" role="dialog">
                      <div className="fl-pulse-popover-head">
                        <Activity size={15} />
                        <span>Pulse</span>
                        <span className={`fl-pulse-badge ${pulseConfig?.enabled ? 'is-on' : 'is-off'}`}>
                          {pulseConfig?.enabled ? 'On' : 'Off'}
                        </span>
                        <button type="button" className="fl-pulse-popover-close" onClick={() => setPulsePopoverOpen(false)} aria-label="Close">×</button>
                      </div>
                      <div className="fl-pulse-body">
                        <div className="fl-pulse-col">
                          <p className="fl-pulse-popover-desc">Quill checks in on its own now and then — reviewing recent activity, keeping the progress report and academic map current.</p>
                          <button
                            type="button"
                            className="fl-pulse-toggle"
                            disabled={savingPulse || !pulseConfig}
                            onClick={() => savePulseConfig({ enabled: !pulseConfig?.enabled })}
                          >
                            <span className={`fl-pulse-toggle-track ${pulseConfig?.enabled ? 'is-on' : ''}`}>
                              <span className="fl-pulse-toggle-thumb" />
                            </span>
                            {pulseConfig?.enabled ? 'Turn off' : 'Turn on'}
                          </button>
                          <label className="fl-pulse-config-row">
                            <span>Check every</span>
                            <select
                              value={pulseConfig?.cadence_hours ?? 24}
                              disabled={savingPulse || !pulseConfig}
                              onChange={(e) => savePulseConfig({ cadence_hours: Number(e.target.value) })}
                            >
                              <option value={6}>6 hours</option>
                              <option value={12}>12 hours</option>
                              <option value={24}>24 hours (daily)</option>
                              <option value={72}>3 days</option>
                              <option value={168}>weekly</option>
                            </select>
                          </label>
                          <div className="fl-pulse-popover-meta">
                            <span>Last check-in</span>
                            <span>{pulseConfig?.last_run_at ? new Date(pulseConfig.last_run_at).toLocaleString() : 'Not yet'}</span>
                          </div>
                          {pulseConfig?.enabled && (
                            <div className="fl-pulse-popover-meta">
                              <span>Next check-in</span>
                              <span>
                                {pulseConfig.last_run_at
                                  ? new Date(new Date(pulseConfig.last_run_at).getTime() + pulseConfig.cadence_hours * 3600_000).toLocaleString()
                                  : `within ${pulseConfig.cadence_hours}h`}
                              </span>
                            </div>
                          )}
                          <button
                            type="button"
                            className="fl-pulse-run-now"
                            disabled={pulseRunning || !pulseConfig}
                            onClick={runPulseNow}
                          >
                            {pulseRunning ? 'Running… (a few minutes)' : 'Run now (test it)'}
                          </button>
                          {pulseRunError && <p className="fl-pulse-run-error">{pulseRunError}</p>}
                        </div>

                        <div className="fl-pulse-col">
                          <p className="fl-pulse-config-hint">School email filter (optional) — a Gmail search for emails from school, e.g. <code>from:school.edu</code>. Quill only ever looks within this filter.</p>
                          <input
                            className="fl-pulse-config-input"
                            type="text"
                            placeholder="from:school.edu"
                            value={schoolQueryDraft}
                            onChange={(e) => { setSchoolQueryDraft(e.target.value); setPulseSaved(false) }}
                          />
                          <p className="fl-pulse-config-hint">Websites to check (optional) — any pages Quill should look at: a school portal, a class site, anything. One per line. Uses your signed-in browser (Connectors → Browser).</p>
                          <textarea
                            className="fl-pulse-config-input"
                            rows={3}
                            placeholder={"https://portal.myraschool.edu/assignments\nhttps://classroom.google.com/..."}
                            value={watchSitesDraft}
                            onChange={(e) => { setWatchSitesDraft(e.target.value); setPulseSaved(false) }}
                          />
                          <p className="fl-pulse-config-hint">Notify by email (optional) — extra addresses to email each check-in to, comma-separated. Quill always also emails the connected Gmail account.</p>
                          <input
                            className="fl-pulse-config-input"
                            type="text"
                            placeholder="dad@example.com, grandma@example.com"
                            value={notifyEmailsDraft}
                            onChange={(e) => { setNotifyEmailsDraft(e.target.value); setPulseSaved(false) }}
                          />
                        </div>
                      </div>
                      <button
                        type="button"
                        className={`fl-pulse-save ${pulseSaved ? 'is-saved' : ''}`}
                        disabled={savingPulse}
                        onClick={() => {
                          const sites = watchSitesDraft.split(/[\n,]/).map((s) => s.trim()).filter(Boolean)
                          const emails = notifyEmailsDraft.split(',').map((s) => s.trim()).filter(Boolean)
                          savePulseConfig({ school_gmail_query: schoolQueryDraft.trim(), watch_sites: sites, notify_emails: emails })
                          setPulseSaved(true)
                        }}
                      >
                        {savingPulse ? 'Saving…' : pulseSaved ? 'Saved ✓' : 'Save'}
                      </button>
                    </div>
                    </>
                  )}
                </div>
                <button className="fl-icon-btn" type="button" aria-label="Settings" title="Settings" onClick={() => setSettingsOpen(true)}>
                  <SettingsIcon size={18} />
                </button>
                <button className="fl-whatsapp-btn" type="button" aria-label="WhatsApp" title="SparkQuill on WhatsApp" onClick={() => setWaOpen(true)}>
                  <svg viewBox="0 0 24 24" width="19" height="19" fill="currentColor" aria-hidden="true"><path d="M17.472 14.382c-.297-.149-1.758-.867-2.03-.967-.273-.099-.471-.148-.67.15-.197.297-.767.966-.94 1.164-.173.199-.347.223-.644.075-.297-.15-1.255-.463-2.39-1.475-.883-.788-1.48-1.761-1.653-2.059-.173-.297-.018-.458.13-.606.134-.133.298-.347.446-.52.149-.174.198-.298.298-.497.099-.198.05-.371-.025-.52-.075-.149-.669-1.612-.916-2.207-.242-.579-.487-.5-.669-.51l-.57-.01c-.198 0-.52.074-.792.372-.272.297-1.04 1.016-1.04 2.479 0 1.462 1.065 2.875 1.213 3.074.149.198 2.096 3.2 5.077 4.487.71.306 1.263.489 1.694.626.712.226 1.36.194 1.872.118.571-.085 1.758-.719 2.006-1.413.248-.694.248-1.289.173-1.413-.074-.124-.272-.198-.57-.347m-5.421 7.403h-.004a9.87 9.87 0 01-5.031-1.378l-.361-.214-3.741.982.998-3.648-.235-.374a9.86 9.86 0 01-1.51-5.26c.001-5.45 4.436-9.884 9.888-9.884 2.64 0 5.122 1.03 6.988 2.898a9.825 9.825 0 012.893 6.994c-.003 5.45-4.437 9.884-9.885 9.884m8.413-18.297A11.815 11.815 0 0012.05 0C5.495 0 .16 5.335.157 11.892c0 2.096.547 4.142 1.588 5.945L.057 24l6.305-1.654a11.882 11.882 0 005.683 1.448h.005c6.554 0 11.89-5.335 11.893-11.893a11.821 11.821 0 00-3.48-8.413z"/></svg>
                </button>
              </div>
            </div>

            {pendingConvUpdate && (
              <button
                type="button"
                className="fl-new-update-banner"
                onClick={() => {
                  // Re-fetch the CURRENT file on tap rather than applying the
                  // snapshot captured when the banner appeared — the snapshot
                  // can be stale (e.g. the parent sent a message after it was
                  // taken), and applying it would drop that message.
                  setPendingConvUpdate(null)
                  fetch(`${FAMILY_API}/api/workspace/file?path=${encodeURIComponent(`parent/conversations/${conversationId}.json`)}`)
                    .then((r) => r.json())
                    .then((d) => {
                      if (!d?.content) return
                      const c = JSON.parse(d.content) as { messages?: StoredMsg[] }
                      setParentMessages((c.messages || []).map(toParentMsg))
                    })
                    .catch(() => {})
                }}
              >
                <RefreshCw size={14} /> New update — tap to refresh
              </button>
            )}
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
                if (m.source === 'pulse' && m.role === 'user') {
                  // The Pulse trigger — shown as a clear, centered "check-in
                  // ran" divider rather than a fake parent bubble (it isn't
                  // something the parent typed), so the whole automated turn
                  // is visible: this divider, then Quill's reply below it.
                  return (
                    <div key={i} className="fl-pulse-divider">
                      <Activity size={13} /> <span>{m.text}</span>
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
                        <span className={`fl-msg-avatar ${m.source === 'pulse' ? 'is-pulse' : 'is-sun'}`}>{m.source === 'pulse' ? <Activity size={17} /> : <Sun size={18} />}</span>
                        <div className="fl-msg-col">
                          <div className={`fl-bubble ${m.source === 'pulse' ? 'is-pulse' : ''}`}><Markdown text={m.text ?? ''} /></div>
                        </div>
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

              {queue.map((q, i) => (
                <div key={`q-${i}`} className="fl-msg is-parent">
                  <div className="fl-msg-col"><div className="fl-bubble is-queued">{q} <span className="fl-queued-tag">queued</span></div></div>
                  <span className="fl-msg-avatar is-parent">{initial}</span>
                </div>
              ))}

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
                    onClick={() => { const h = pendingHandoff; setPendingHandoff(null); if (h.manifest) { startPackageHandoff(h.manifest) } else { startHandoff(h.path) } }}
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
                placeholder={sending ? 'Quill is replying — your next message will be queued…' : `Ask anything about ${childName || 'your child'}’s learning…`}
                value={focusInput}
                onChange={(event) => setFocusInput(event.target.value)}
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
                            <span className="fl-asset-icon"><FileGlyph name={f.name} size={17} /></span>
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
                      {viewerMeta && (
                        <button
                          className={`fl-icon-btn${metaOpen ? ' is-active' : ''}`}
                          type="button"
                          aria-label="About this file"
                          aria-pressed={metaOpen}
                          title="What Quill knows about this file"
                          onClick={() => setMetaOpen((v) => !v)}
                        >
                          <Info size={14} />
                        </button>
                      )}
                      {isPrintable(viewerPath) && (
                        <button
                          className="fl-icon-btn"
                          type="button"
                          aria-label="Print"
                          title="Print this page"
                          onClick={() => printFile(viewerPath)}
                        >
                          <Printer size={14} />
                        </button>
                      )}
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
                    {metaOpen && viewerMeta && <FileMetaPanel meta={viewerMeta} />}
                    {/\.(png|jpe?g|gif|webp|svg|bmp)$/i.test(viewerPath) ? (
                      <img className="fl-viewer-img" src={`${FAMILY_API}/api/workspace/raw?path=${encodeURIComponent(viewerPath)}`} alt={viewerPath.split('/').pop() || ''} />
                    ) : /\.pdf$/i.test(viewerPath) ? (
                      // PDFs render in the browser's native viewer (with its own
                      // zoom/page controls) — the raw endpoint serves them inline
                      // with an application/pdf content type, so a plain iframe
                      // pointed straight at it is all it takes. No sandbox here: it
                      // would disable the built-in PDF viewer, and the bytes are our
                      // own workspace file, not untrusted HTML.
                      <iframe className="fl-viewer-frame" title="PDF preview" src={`${FAMILY_API}/api/workspace/raw?path=${encodeURIComponent(viewerPath)}`} />
                    ) : !viewerContent ? (
                      <p className="fl-note">Loading…</p>
                    ) : !viewerContent.isText ? (
                      <NonPreviewableFile path={viewerPath} meta={viewerMeta} />
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
                      <button type="button" className={filesView === 'advanced' ? 'is-active' : ''} onClick={() => setFilesView('advanced')}>All files</button>
                    </div>
                    {filesView === 'advanced' ? (
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
                      const typeSlug = (t: string) => t === 'Practice tests' ? 'tests' : t === 'Study guides' ? 'guides' : t === 'Reports' ? 'reports' : 'uploaded'
                      const usable = allFiles.filter((p) => !p.endsWith('.meta.json') && !p.startsWith('skills/') && !p.includes('/conversations/') && !p.endsWith('child-profile.json'))
                      const classified = usable.map((p) => ({ p, type: typeOf(p), ...parseAssetPath(p) })).filter((f) => f.type)
                      const subjectsList = Array.from(new Set(classified.filter((f) => f.subject).map((f) => f.subject!))).sort()
                      const wantedTypes = filesView === 'uploaded' ? ['Uploaded material'] : ['Practice tests', 'Study guides', 'Reports']
                      const relevant = classified.filter((f) => wantedTypes.includes(f.type!) && (!filesSubjectFilter || f.subject === filesSubjectFilter))

                      // Packages nested under the subject their first item belongs to (an
                      // instruction-only package with no items has no subject to attach to).
                      const packagesBySubject = new Map<string, LearningPackage[]>()
                      const unplacedPackages: LearningPackage[] = []
                      if (filesView === 'subjects') {
                        packages.forEach((pkg) => {
                          const subj = pkg.items.length > 0 ? parseAssetPath(pkg.items[0]).subject : undefined
                          if (subj && (!filesSubjectFilter || subj === filesSubjectFilter)) {
                            if (!packagesBySubject.has(subj)) packagesBySubject.set(subj, [])
                            packagesBySubject.get(subj)!.push(pkg)
                          } else if (!subj && !filesSubjectFilter) {
                            unplacedPackages.push(pkg)
                          }
                        })
                      }
                      const renderPackages = (pkgs: LearningPackage[]) => pkgs.map((pkg) => (
                        <div key={pkg.manifest} className="fl-file-item is-package fl-package-card">
                          <BookOpen size={16} />
                          <span>
                            {pkg.title}
                            <small>
                              {pkg.items.length > 0 ? `${pkg.items.length} part${pkg.items.length === 1 ? '' : 's'}` : 'Adaptive practice'}
                              {dateTimeLabel(pkg.created_at) ? ` · ${dateTimeLabel(pkg.created_at)}` : ''}
                            </small>
                          </span>
                          <button
                            className="fl-give-to-child"
                            type="button"
                            disabled={sending}
                            onClick={() => startPackageHandoff(pkg.manifest)}
                          >
                            Give to {childName || 'child'}
                          </button>
                        </div>
                      ))

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
                                  <FileGlyph name={e.path} size={16} />
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
                                  {renderPackages(packagesBySubject.get(subj) ?? [])}
                                  {Array.from(topics.entries()).map(([top, types]) => (
                                    <div key={top} className="fl-asset-topic">
                                      <p className="fl-asset-topic-label">{top === '—' ? 'Other' : top}</p>
                                      {Array.from(types.entries()).map(([type, entries]) => (
                                        <div key={type} className={`fl-asset-type is-${typeSlug(type)}`}>
                                          {filesView !== 'uploaded' && <p className="fl-asset-type-label">{type}</p>}
                                          {renderEntries(entries)}
                                        </div>
                                      ))}
                                    </div>
                                  ))}
                                </section>
                              ))}
                              {(general.size > 0 || unplacedPackages.length > 0) && (
                                <section className="fl-asset-group">
                                  <p className="fl-drawer-label">General</p>
                                  {renderPackages(unplacedPackages)}
                                  {Array.from(general.entries()).map(([type, entries]) => (
                                    <div key={type} className={`fl-asset-type is-${typeSlug(type)}`}>
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
              <div className="fl-connectors" onClick={(e) => e.stopPropagation()}>
                <div className="fl-wa-head">
                  <span className="fl-wa-title">Connectors</span>
                  <button className="fl-wa-close" type="button" onClick={() => setWaOpen(false)} aria-label="Close">×</button>
                </div>
                <div className="fl-connectors-body">
                  <nav className="fl-connectors-nav">
                    <button type="button" className={connectorSection === 'whatsapp' ? 'is-active' : ''} onClick={() => setConnectorSection('whatsapp')}>WhatsApp</button>
                    <button type="button" className={connectorSection === 'gmail' ? 'is-active' : ''} onClick={() => setConnectorSection('gmail')}>Gmail</button>
                    <button type="button" className={connectorSection === 'browser' ? 'is-active' : ''} onClick={() => setConnectorSection('browser')}>Browser</button>
                  </nav>
                  <div className="fl-connectors-panel">
                    {connectorSection === 'whatsapp' ? (
                      waStatus?.paired ? (
                        <div className="fl-connector-card">
                          <p className="fl-connector-status is-connected">✓ Connected{waStatus.own_jid ? ` — +${waStatus.own_jid}` : ''}</p>
                          <div className="fl-wa-howto">
                            <p className="fl-wa-howto-title">How to chat with Quill on WhatsApp</p>
                            <ol className="fl-note" style={{ paddingLeft: '1.2em', margin: '6px 0 0' }}>
                              <li>Open WhatsApp on your phone.</li>
                              <li>At the top, search for your own name — the chat labelled <strong>“(You)”</strong> or <strong>“Message yourself”</strong>.</li>
                              <li>Type anything there, like <em>“How is Myra doing this week?”</em> — Quill reads it and replies right in that same chat.</li>
                            </ol>
                            <p className="fl-note" style={{ marginTop: '8px' }}>That’s it — it works just like texting. You can also send a photo of Myra’s worksheet there and Quill will look at it. Quill only ever answers in your own “message yourself” chat — never in your chats with other people.</p>
                          </div>
                          <button className="fl-ghost-btn" type="button" onClick={unpairWhatsApp} disabled={waUnpairing}>{waUnpairing ? 'Unlinking…' : 'Unlink WhatsApp'}</button>
                        </div>
                      ) : (
                        <div className="fl-connector-card">
                          <p className="fl-note">Scan this code with WhatsApp on your phone: <strong>Settings → Linked Devices → Link a Device.</strong></p>
                          {waStatus?.qr_available ? (
                            <img className="fl-wa-qr" src={`${FAMILY_API}/api/whatsapp/pair?n=${waQrNonce}`} alt="WhatsApp pairing QR code" />
                          ) : (
                            <div className="fl-wa-qr is-loading">Preparing QR…</div>
                          )}
                          <p className="fl-note">The code refreshes automatically every 30 seconds until scanned.</p>
                        </div>
                      )
                    ) : connectorSection === 'gmail' ? (
                      gmailStatus?.connected ? (
                        <div className="fl-connector-card">
                          <p className="fl-connector-status is-connected">✓ Connected{gmailStatus.email ? ` — ${gmailStatus.email}` : ''}</p>
                          <p className="fl-note">Sends only ever go to this same address, and only when you click the button below.</p>
                          <button className="fl-ghost-btn" type="button" onClick={sendGmailTest} disabled={gmailTesting}>{gmailTesting ? 'Sending…' : 'Send test email'}</button>
                          {gmailTestResult && <p className="fl-note">{gmailTestResult}</p>}
                        </div>
                      ) : gmailStatus === null ? (
                        <p className="fl-note">Checking Gmail connection…</p>
                      ) : (
                        <div className="fl-connector-card">
                          <p className="fl-note">Not connected. Gmail uses the <code>gws</code> CLI on this computer — sign it into a Google account, then reopen this.</p>
                        </div>
                      )
                    ) : (
                      <div className="fl-connector-card">
                        <p className="fl-connector-status" style={browserStatus?.cli_installed ? { color: 'var(--fl-green, #2e7d32)' } : undefined}>
                          {browserStatus === null ? 'Checking…' : browserStatus.cli_installed ? '✓ Ready' : 'Not set up yet'}
                        </p>
                        <p className="fl-note">For things like school portals — assignments, report cards, uploaded books — the safest way for Quill to check them is to use a browser you're already signed into, so it never needs your password.</p>
                        <div className="fl-install-steps">
                          <p className="fl-note"><strong>One-time setup:</strong> copy this, paste it into the Terminal app on your Mac, and press Enter.</p>
                          <div className="fl-code-row">
                            <pre className="fl-code-block"><code>curl -fsSL 'https://raw.githubusercontent.com/manishiitg/coding-agent-loop/main/scripts/install-chrome-cdp-macOS.sh' | bash</code></pre>
                            <button
                              type="button"
                              className="fl-ghost-btn"
                              onClick={() => {
                                navigator.clipboard.writeText("curl -fsSL 'https://raw.githubusercontent.com/manishiitg/coding-agent-loop/main/scripts/install-chrome-cdp-macOS.sh' | bash")
                                setBrowserCopied(true)
                                window.setTimeout(() => setBrowserCopied(false), 2000)
                              }}
                            >
                              {browserCopied ? 'Copied!' : 'Copy'}
                            </button>
                          </div>
                          <p className="fl-note">A new browser window opens on its own once it's done.</p>
                        </div>
                        <p className="fl-note">Then sign into the school portal (or anything else you'd like Quill to check) in that window, and just leave it open. From then on, Quill can look things up there whenever it's useful — it never sees or stores your password.</p>
                        {browserStatus && !browserStatus.cli_installed && (
                          <p className="fl-note">(Also needed once: ask whoever set this computer up to run <code>npm install -g agent-browser@latest</code>.)</p>
                        )}
                      </div>
                    )}
                  </div>
                </div>
              </div>
            </div>
          )}

          {settingsOpen && (
            <div className="fl-settings-backdrop" role="dialog" aria-modal="true" onClick={() => setSettingsOpen(false)}>
              <div className="fl-settings" onClick={(e) => e.stopPropagation()}>
                <div className="fl-settings-head">
                  <span className="fl-settings-title">Settings</span>
                  <button className="fl-wa-close" type="button" onClick={() => setSettingsOpen(false)} aria-label="Close">×</button>
                </div>
                <div className="fl-settings-body">
                  <p className="fl-drawer-label">AI engine</p>
                  <p className="fl-note">Which coding-agent engine Quill runs on for both the parent chat and Myra’s tutor.</p>
                  {enginesState === 'loading' ? (
                    <p className="fl-note">Checking available engines…</p>
                  ) : engines.length === 0 ? (
                    <p className="fl-note">No engines detected on this machine.</p>
                  ) : (
                    <div className="fl-settings-engines">
                      {engines.map((item) => {
                        const status = engineStatus(item)
                        const active = engine === item.id
                        return (
                          <button
                            key={item.id}
                            type="button"
                            className={`fl-settings-engine-card ${active ? 'is-active' : ''}`}
                            disabled={!status.ready || savingEngine}
                            onClick={() => {
                              setEngine(item.id)
                              setSavingEngine(true)
                              fetch(`${FAMILY_API}/api/engine/selection`, {
                                method: 'POST',
                                headers: { 'Content-Type': 'application/json' },
                                body: JSON.stringify({ engine: item.id }),
                              }).finally(() => setSavingEngine(false))
                            }}
                          >
                            <span className="fl-settings-engine-name">{item.name}</span>
                            <span className={`fl-settings-engine-status ${status.ready ? 'is-ready' : ''}`}>{status.label}</span>
                            {active && <Check size={16} />}
                          </button>
                        )
                      })}
                    </div>
                  )}
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
          <div className="fl-child-body">
            <section className="fl-child-chat">
              <header className="fl-child-top">
                <div className="fl-child-id">
                  <img className="fl-header-logo" src="/sparkquill-mark.svg" alt="" width={30} height={30} />
                  <div className="fl-child-hi"><strong>Hi {childName || 'Maya'}!</strong><small>Let’s keep learning together</small></div>
                </div>
                <div className="fl-child-top-right">
                  <button className="fl-parent-return" type="button" onClick={() => { setGateValue(''); setGateError(''); setPinGate(true) }}><LockKeyhole size={16} /> Parent Mode</button>
                </div>
              </header>
              {childTask && (!!childTask.items?.length || !!childTask.package) && (
                <div className="fl-child-assignment">
                  <div className="fl-assignment-head">
                    <BookOpen size={15} />
                    <span className="fl-assignment-title">{childTask.title || 'Your assignment'}</span>
                    {childTask.items && childTask.items.length > 0 && <small>{childTask.items.length} part{childTask.items.length === 1 ? '' : 's'}</small>}
                  </div>
                  {childTask.items && childTask.items.length > 0 && (
                    <div className="fl-assignment-parts">
                      {childTask.items.map((item, i) => {
                        const { label } = parseAssetPath(item)
                        return (
                          <button key={item} type="button" className="fl-assignment-part" onClick={() => setChildViewerPath(item)}>
                            <span className="fl-package-step">{i + 1}</span>
                            <FileGlyph name={item} size={14} />
                            <span>{label}</span>
                          </button>
                        )
                      })}
                    </div>
                  )}
                </div>
              )}
              <div className="fl-child-thread" aria-label="Tutor conversation">
                <div className="fl-tmsg is-tutor">
                  <span className="fl-tmsg-avatar"><Sun size={20} /></span>
                  <div className="fl-tbubble">Hi {childName || 'Maya'}! Ready to keep learning? Tell me what you’re working on, or ask me anything — I’ll help you figure it out step by step.</div>
                </div>
                {childMessages.map((m, i) => (
                  m.role === 'tool' && (m.tool === 'upload' || m.tool === 'upload_error') ? (
                    <div key={i} className="fl-tmsg is-tutor">
                      <span className="fl-tmsg-avatar"><Paperclip size={16} /></span>
                      <div className={`fl-toolcard ${m.tool === 'upload_error' ? 'is-error' : 'is-upload'}`}>
                        <Paperclip size={15} />
                        <span>{m.tool === 'upload_error' ? <>Couldn’t add your photo</> : <>Added your photo</>}</span>
                      </div>
                    </div>
                  ) : m.role === 'tool' && m.tool === 'celebrate' ? (
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
                <input ref={childFileInputRef} type="file" multiple accept="image/*" onChange={onChildFilesSelected} style={{ display: 'none' }} />
                <button className="composer-icon" type="button" aria-label="Attach a photo of your work" onClick={onPickChildFiles} disabled={childSending || childUploading}><Paperclip size={19} /></button>
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
                    {isPrintable(childViewerPath) && (
                      <button
                        className="fl-icon-btn"
                        type="button"
                        aria-label="Print"
                        title="Print this page"
                        onClick={() => printFile(childViewerPath)}
                      >
                        <Printer size={14} />
                      </button>
                    )}
                  </div>
                  {IMAGE_PATH_RE.test(childViewerPath) ? (
                    <img className="fl-viewer-img" src={`${FAMILY_API}/api/workspace/raw?path=${encodeURIComponent(childViewerPath)}`} alt="" />
                  ) : /\.pdf$/i.test(childViewerPath) ? (
                    <iframe className="fl-viewer-frame" title="PDF preview" src={`${FAMILY_API}/api/workspace/raw?path=${encodeURIComponent(childViewerPath)}`} />
                  ) : !childViewerContent ? (
                    <p className="fl-note">Loading…</p>
                  ) : !childViewerContent.isText ? (
                    <NonPreviewableFile path={childViewerPath} meta={null} />
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
                    // Show ONLY the current assignment (child/current-task.json) — the
                    // one package the parent most recently handed off — plus the
                    // child's own saved work. Not every package/material ever approved.
                    const curPkg = childTask?.package ? childPackages.find((pkg) => pkg.path === childTask.package) : undefined
                    const currentItems = childTask?.items ?? []
                    const attempts = childFiles.filter((p) => p.startsWith('child/attempts/'))
                    const hasCurrent = currentItems.length > 0 || !!childTask?.package
                    if (!hasCurrent && attempts.length === 0) {
                      return <p className="fl-child-note"><Sparkles size={15} /> Ask Quill what to work on next!</p>
                    }
                    return (
                      <>
                        {currentItems.length > 0 ? (
                          <section className="fl-asset-group">
                            <p className="fl-drawer-label">From your parent</p>
                            <div className="fl-child-package">
                              <div className="fl-package-title"><BookOpen size={16} /><span>{childTask?.title || 'Your assignment'}<small>{currentItems.length} part{currentItems.length === 1 ? '' : 's'}{dateTimeLabel(curPkg?.createdAt) ? ` · ${dateTimeLabel(curPkg?.createdAt)}` : ''}</small></span></div>
                              {currentItems.map((item, i) => {
                                const { label } = parseAssetPath(item)
                                return (
                                  <button key={item} type="button" className="fl-file-item fl-package-item" onClick={() => setChildViewerPath(item)}>
                                    <span className="fl-package-step">{i + 1}</span>
                                    <FileGlyph name={item} size={15} />
                                    <span>{label}</span>
                                  </button>
                                )
                              })}
                            </div>
                          </section>
                        ) : childTask?.package ? (
                          // Instruction-only package (no files): kick off the live activity in chat.
                          <section className="fl-asset-group">
                            <p className="fl-drawer-label">From your parent</p>
                            <button type="button" className="fl-file-item is-package" onClick={() => { setChildViewerPath(null); sendChildText(`Let's start ${childTask?.title || 'my activity'}!`) }}>
                              <BookOpen size={16} /><span>{childTask?.title || 'Your activity'}<small>Adaptive practice{dateTimeLabel(curPkg?.createdAt) ? ` · ${dateTimeLabel(curPkg?.createdAt)}` : ''}</small></span>
                            </button>
                          </section>
                        ) : null}
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
