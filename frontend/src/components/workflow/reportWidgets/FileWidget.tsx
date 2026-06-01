import { useEffect, useMemo, useState } from 'react'
import { File, FileJson, FileText, FolderOpen, Image, Music, Video } from 'lucide-react'
import type { PlannerFile, ReportFileListFormat, ReportFileRenderFormat, ReportWidget } from '../../../services/api-types'
import { agentApi, workspaceApi } from '../../../services/api'
import { MarkdownRenderer } from '../../ui/MarkdownRenderer'
import { WidgetError, WidgetHeader } from './shared'
import { useReportFilePreviewStore } from '../../../stores/useReportFilePreviewStore'

// previewReportFile opens a file-list entry in the in-report preview modal.
// file.filepath is the absolute workspace path the planner-files API returns.
function previewReportFile(file: PlannerFile) {
  useReportFilePreviewStore.getState().show({ path: file.filepath, name: basename(file.filepath) })
}

type ArtifactKind = 'markdown' | 'html' | 'text' | 'code' | 'json' | 'image' | 'video' | 'audio' | 'pdf' | 'other'

type FileContentState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; content?: string; objectUrl?: string; mimeType?: string }

type FileListState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; files: PlannerFile[] }

const TEXT_EXTENSIONS = new Set(['txt', 'log', 'csv', 'tsv', 'yaml', 'yml', 'xml', 'diff', 'patch'])
const CODE_EXTENSIONS = new Set(['js', 'jsx', 'ts', 'tsx', 'py', 'go', 'java', 'c', 'cpp', 'cs', 'php', 'rb', 'rs', 'sql', 'sh', 'bash', 'zsh', 'css'])
const IMAGE_EXTENSIONS = new Set(['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg', 'bmp', 'ico'])
const VIDEO_EXTENSIONS = new Set(['webm', 'mp4', 'mov', 'm4v'])
const AUDIO_EXTENSIONS = new Set(['mp3', 'wav', 'm4a', 'aac', 'ogg', 'oga', 'flac', 'opus'])

function normalizeSource(source: string): string {
  return source.replace(/\\/g, '/').replace(/^\/+/, '').replace(/\/+/g, '/')
}

function isAllowedArtifactSource(source: string): boolean {
  const normalized = normalizeSource(source)
  if (!normalized || normalized.split('/').some(part => part === '..')) return false
  return normalized.startsWith('db/') || normalized.startsWith('knowledgebase/') || normalized.startsWith('docs/')
}

function workspaceFilePath(workspacePath: string, source: string): string {
  return `${workspacePath.replace(/\/+$/, '')}/${normalizeSource(source)}`
}

function extensionFor(path: string): string {
  const leaf = path.split(/[?#]/, 1)[0].split('/').pop() || ''
  const idx = leaf.lastIndexOf('.')
  return idx >= 0 ? leaf.slice(idx + 1).toLowerCase() : ''
}

function artifactKind(path: string): ArtifactKind {
  const ext = extensionFor(path)
  if (ext === 'md' || ext === 'markdown') return 'markdown'
  if (ext === 'html' || ext === 'htm') return 'html'
  if (ext === 'json' || ext === 'jsonl') return 'json'
  if (ext === 'pdf') return 'pdf'
  if (IMAGE_EXTENSIONS.has(ext)) return 'image'
  if (VIDEO_EXTENSIONS.has(ext)) return 'video'
  if (AUDIO_EXTENSIONS.has(ext)) return 'audio'
  if (CODE_EXTENSIONS.has(ext)) return 'code'
  if (TEXT_EXTENSIONS.has(ext)) return 'text'
  return 'other'
}

function effectiveRenderFormat(widget: ReportWidget): ArtifactKind | 'link' {
  const requested = (widget.renderFormat || 'auto') as ReportFileRenderFormat
  if (requested && requested !== 'auto') return requested === 'link' ? 'link' : requested
  return artifactKind(widget.source)
}

function mimeTypeFor(path: string): string {
  const ext = extensionFor(path)
  const mimeTypes: Record<string, string> = {
    pdf: 'application/pdf',
    html: 'text/html',
    htm: 'text/html',
    mp4: 'video/mp4',
    webm: 'video/webm',
    mov: 'video/quicktime',
    mp3: 'audio/mpeg',
    wav: 'audio/wav',
    m4a: 'audio/mp4',
    aac: 'audio/aac',
    ogg: 'audio/ogg',
    oga: 'audio/ogg',
    flac: 'audio/flac',
    opus: 'audio/opus',
  }
  return mimeTypes[ext] || 'application/octet-stream'
}

function basename(path: string): string {
  return path.split('/').filter(Boolean).pop() || path
}

function formatBytes(size?: number): string | null {
  if (typeof size !== 'number' || !Number.isFinite(size) || size < 0) return null
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`
  return `${(size / (1024 * 1024)).toFixed(1)} MB`
}

function formatDate(value?: string): string | null {
  if (!value) return null
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return null
  return date.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })
}

function collectFiles(items: PlannerFile[], out: PlannerFile[] = []): PlannerFile[] {
  for (const item of items) {
    const children = Array.isArray(item.children) ? item.children : []
    const isFolder = item.type === 'folder' || children.length > 0
    if (!isFolder && item.filepath) out.push(item)
    if (children.length > 0) collectFiles(children, out)
  }
  return out
}

async function loadBinaryObjectUrl(path: string, mimeType = mimeTypeFor(path)): Promise<string> {
  const response = await workspaceApi.get(`/api/documents/${encodeURIComponent(path)}`, {
    params: { download: 'true' },
    responseType: 'blob',
    headers: { Accept: 'application/octet-stream' },
    transformResponse: [(data) => data],
  })
  const blob = response.data instanceof Blob
    ? response.data
    : new Blob([response.data], { type: mimeType })
  return URL.createObjectURL(blob.type ? blob : blob.slice(0, blob.size, mimeType))
}

function ArtifactIcon({ kind }: { kind: ArtifactKind }) {
  const className = "h-4 w-4"
  if (kind === 'image') return <Image className={className} />
  if (kind === 'video') return <Video className={className} />
  if (kind === 'audio') return <Music className={className} />
  if (kind === 'json') return <FileJson className={className} />
  if (kind === 'markdown' || kind === 'text' || kind === 'code' || kind === 'html' || kind === 'pdf') return <FileText className={className} />
  return <File className={className} />
}

function useFileContent(widget: ReportWidget, workspacePath: string): FileContentState {
  const [state, setState] = useState<FileContentState>({ status: 'loading' })
  const path = workspaceFilePath(workspacePath, widget.source)
  const format = effectiveRenderFormat(widget)

  useEffect(() => {
    let cancelled = false
    let objectUrl: string | undefined
    setState({ status: 'loading' })

    const load = async () => {
      try {
        if (!isAllowedArtifactSource(widget.source)) {
          throw new Error('File widgets can only read db/, knowledgebase/, or docs/ paths.')
        }
        if (format === 'link') {
          if (!cancelled) setState({ status: 'ready' })
          return
        }
        if (format === 'image') {
          const response = await agentApi.getPlannerFileContent(path)
          const content = typeof response?.data?.content === 'string' ? response.data.content : ''
          if (content.startsWith('data:image/')) {
            if (!cancelled) setState({ status: 'ready', content })
            return
          }
          objectUrl = await loadBinaryObjectUrl(path)
          if (!cancelled) setState({ status: 'ready', objectUrl })
          return
        }
        if (format === 'pdf' || format === 'video' || format === 'audio') {
          objectUrl = await loadBinaryObjectUrl(path)
          if (!cancelled) setState({ status: 'ready', objectUrl, mimeType: mimeTypeFor(path) })
          return
        }
        const response = await agentApi.getPlannerFileContent(path)
        const content = typeof response?.data?.content === 'string' ? response.data.content : ''
        if (!cancelled) setState({ status: 'ready', content })
      } catch (error) {
        if (!cancelled) {
          setState({ status: 'error', message: error instanceof Error ? error.message : String(error) })
        }
      }
    }

    void load()
    return () => {
      cancelled = true
      if (objectUrl) URL.revokeObjectURL(objectUrl)
    }
  }, [format, path, widget.source])

  return state
}

export function FileWidget({ widget, workspacePath }: { widget: ReportWidget; workspacePath: string }) {
  const state = useFileContent(widget, workspacePath)
  const format = effectiveRenderFormat(widget)
  const path = workspaceFilePath(workspacePath, widget.source)
  const name = basename(widget.source)

  if (!isAllowedArtifactSource(widget.source)) {
    return <WidgetError widget={widget} message="Unsupported file source." hint="Use db/, knowledgebase/, or docs/." />
  }

  if (state.status === 'loading') {
    return <div className="py-3 text-sm text-muted-foreground">Loading {name}...</div>
  }
  if (state.status === 'error') {
    return <WidgetError widget={widget} message={`Could not load ${widget.source}.`} hint={state.message} />
  }

  return (
    <div className="flex flex-col gap-3">
      <WidgetHeader widget={widget} />
      {format === 'markdown' && (
        <div className="rounded-lg bg-muted/20 px-3 py-3 text-sm text-foreground">
          <MarkdownRenderer content={state.content || ''} basePath={path} className="max-w-none" maxHeight="none" />
        </div>
      )}
      {format === 'html' && (
        <iframe
          title={widget.title || name}
          srcDoc={state.content || ''}
          sandbox="allow-same-origin allow-scripts"
          className="h-[min(720px,70vh)] w-full rounded-lg border border-border bg-background"
        />
      )}
      {(format === 'text' || format === 'code' || format === 'json') && (
        <pre className="max-h-[640px] overflow-auto rounded-lg bg-muted/25 px-3 py-3 text-xs leading-6 text-foreground">
          {format === 'json' ? formatJSONText(state.content || '') : state.content}
        </pre>
      )}
      {format === 'image' && (
        <img src={state.content || state.objectUrl} alt={widget.title || name} className="max-h-[720px] w-full rounded-lg border border-border object-contain bg-background" />
      )}
      {format === 'video' && state.objectUrl && (
        <video src={state.objectUrl} controls className="max-h-[720px] w-full rounded-lg border border-border bg-black" />
      )}
      {format === 'audio' && state.objectUrl && (
        <audio src={state.objectUrl} controls className="w-full" />
      )}
      {format === 'pdf' && state.objectUrl && (
        <iframe title={widget.title || name} src={state.objectUrl} className="h-[min(820px,75vh)] w-full rounded-lg border border-border bg-background" />
      )}
      {(format === 'link' || format === 'other') && (
        <div className="flex items-center gap-2 rounded-lg border border-border bg-muted/20 px-3 py-3 text-sm text-foreground">
          <ArtifactIcon kind={artifactKind(widget.source)} />
          <span className="min-w-0 truncate">{widget.source}</span>
        </div>
      )}
    </div>
  )
}

function formatJSONText(content: string): string {
  try {
    return JSON.stringify(JSON.parse(content), null, 2)
  } catch {
    return content
  }
}

// Backup / churn folders that should never clutter a report file-list. Matched
// case-insensitively against each folder segment under the widget's source.
const NOISE_DIR_SEGMENTS = new Set(['backups', 'stale_files_backups', 'node_modules', '__pycache__', '.git', '.cache', '.trash'])
function isNoiseDirSegment(seg: string): boolean {
  const s = seg.toLowerCase()
  if (NOISE_DIR_SEGMENTS.has(s)) return true
  if (s.startsWith('.')) return true                                  // hidden dirs
  if (s.endsWith('_backups') || s.endsWith('-backups') || s.endsWith('.backup')) return true
  if (s.startsWith('stale_') || s.startsWith('backup')) return true
  return false
}
// Folder segments of a file relative to the source folder (excludes the filename).
function relFolderSegments(filepath: string, sourceFolderAbs: string): string[] {
  const fp = filepath.replace(/\\/g, '/')
  const prefix = sourceFolderAbs.replace(/\/+$/, '') + '/'
  const rel = fp.startsWith(prefix) ? fp.slice(prefix.length) : basename(fp)
  return rel.split('/').filter(Boolean).slice(0, -1)
}

function useFileList(widget: ReportWidget, workspacePath: string): FileListState {
  const [state, setState] = useState<FileListState>({ status: 'loading' })
  const folder = workspaceFilePath(workspacePath, widget.source)
  const maxDepth = widget.recursive ? -1 : 1

  useEffect(() => {
    let cancelled = false
    setState({ status: 'loading' })
    const load = async () => {
      try {
        if (!isAllowedArtifactSource(widget.source)) {
          throw new Error('File-list widgets can only read db/, knowledgebase/, or docs/ paths.')
        }
        const response = await agentApi.getPlannerFiles(folder, widget.maxItems ?? 200, maxDepth)
        const rawFiles = Array.isArray(response) ? response : Array.isArray(response?.data) ? response.data : []
        let files = collectFiles(rawFiles)
        const allowedExtensions = (widget.extensions || []).map(ext => ext.replace(/^\./, '').toLowerCase()).filter(Boolean)
        if (allowedExtensions.length > 0) {
          const allowed = new Set(allowedExtensions)
          files = files.filter(file => allowed.has(extensionFor(file.filepath)))
        }
        // Drop backup/churn clutter. When not recursive, keep only files directly
        // in the source folder (the API can over-return nested entries); when
        // recursive, keep real subfolders but skip backup/noise dirs.
        files = files.filter(file => {
          const segs = relFolderSegments(file.filepath, folder)
          if (!widget.recursive) return segs.length === 0
          return !segs.some(isNoiseDirSegment)
        })
        if (widget.maxItems && widget.maxItems > 0) files = files.slice(0, widget.maxItems)
        if (!cancelled) setState({ status: 'ready', files })
      } catch (error) {
        if (!cancelled) setState({ status: 'error', message: error instanceof Error ? error.message : String(error) })
      }
    }
    void load()
    return () => {
      cancelled = true
    }
  }, [folder, maxDepth, widget.extensions, widget.maxItems, widget.recursive, widget.source])

  return state
}

// groupFilesByFolder buckets files by the first path segment under the source
// folder (e.g. the PAN folder under db/reports). Files directly in the source
// root land in the "" bucket. Order of first appearance is preserved.
function groupFilesByFolder(files: PlannerFile[], sourceFolderAbs: string): { group: string; files: PlannerFile[] }[] {
  const prefix = sourceFolderAbs.replace(/\/+$/, '') + '/'
  const order: string[] = []
  const buckets = new Map<string, PlannerFile[]>()
  for (const file of files) {
    const fp = file.filepath.replace(/\\/g, '/')
    const rel = fp.startsWith(prefix) ? fp.slice(prefix.length) : basename(fp)
    const slash = rel.indexOf('/')
    const group = slash > 0 ? rel.slice(0, slash) : ''
    if (!buckets.has(group)) {
      buckets.set(group, [])
      order.push(group)
    }
    buckets.get(group)!.push(file)
  }
  return order.map(group => ({ group, files: buckets.get(group)! }))
}

function FileGroupBody({ files, format, workspacePath }: { files: PlannerFile[]; format: ReportFileListFormat; workspacePath: string }) {
  if (format === 'table') return <FileTable files={files} />
  if (format === 'cards' || format === 'gallery') return <FileGallery files={files} workspacePath={workspacePath} compact={format === 'cards'} />
  return <FileList files={files} />
}

export function FileListWidget({ widget, workspacePath }: { widget: ReportWidget; workspacePath: string }) {
  const state = useFileList(widget, workspacePath)
  const format = (widget.listFormat || 'list') as ReportFileListFormat

  if (!isAllowedArtifactSource(widget.source)) {
    return <WidgetError widget={widget} message="Unsupported folder source." hint="Use db/, knowledgebase/, or docs/." />
  }
  if (state.status === 'loading') {
    return <div className="py-3 text-sm text-muted-foreground">Loading files...</div>
  }
  if (state.status === 'error') {
    return <WidgetError widget={widget} message={`Could not list ${widget.source}.`} hint={state.message} />
  }

  // Group by subfolder (e.g. per-PAN) so a recursive listing reads as labelled
  // sections instead of one long flat list. Falls back to a flat render when
  // every file sits in the source root (a single empty group).
  const sourceFolderAbs = workspaceFilePath(workspacePath, widget.source)
  const groups = groupFilesByFolder(state.files, sourceFolderAbs)
  const grouped = groups.length > 1 || (groups.length === 1 && groups[0].group !== '')

  return (
    <div className="flex flex-col gap-3">
      <WidgetHeader widget={widget} />
      {state.files.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border px-3 py-5 text-sm text-muted-foreground">No files found.</div>
      ) : grouped ? (
        <div className="flex flex-col gap-4">
          {groups.map(({ group, files }) => (
            <div key={group || '__root__'} className="flex flex-col gap-1.5">
              <div className="flex items-center gap-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                <FolderOpen className="h-3.5 w-3.5" />
                <span className="truncate">{group || 'Other'}</span>
                <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] font-medium normal-case text-muted-foreground">{files.length}</span>
              </div>
              <FileGroupBody files={files} format={format} workspacePath={workspacePath} />
            </div>
          ))}
        </div>
      ) : (
        <FileGroupBody files={state.files} format={format} workspacePath={workspacePath} />
      )}
    </div>
  )
}

function FileList({ files }: { files: PlannerFile[] }) {
  return (
    <div className="divide-y divide-border rounded-lg border border-border">
      {files.map(file => (
        <button
          key={file.filepath}
          type="button"
          title={`Open ${file.filepath}`}
          onClick={() => previewReportFile(file)}
          className="flex w-full min-w-0 items-center gap-2 px-3 py-2 text-left text-sm hover:bg-muted/40"
        >
          <ArtifactIcon kind={artifactKind(file.filepath)} />
          <span className="min-w-0 flex-1 truncate text-primary underline underline-offset-2">{basename(file.filepath)}</span>
          {formatBytes(file.size) && <span className="text-xs text-muted-foreground">{formatBytes(file.size)}</span>}
        </button>
      ))}
    </div>
  )
}

function FileTable({ files }: { files: PlannerFile[] }) {
  return (
    <div className="overflow-x-auto rounded-lg border border-border">
      <table className="w-full text-left text-sm">
        <thead className="bg-muted/40 text-xs uppercase text-muted-foreground">
          <tr>
            <th className="px-3 py-2 font-medium">File</th>
            <th className="px-3 py-2 font-medium">Type</th>
            <th className="px-3 py-2 font-medium">Size</th>
            <th className="px-3 py-2 font-medium">Modified</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border">
          {files.map(file => (
            <tr key={file.filepath} className="hover:bg-muted/40">
              <td className="max-w-[420px] px-3 py-2">
                <button
                  type="button"
                  title={`Open ${file.filepath}`}
                  onClick={() => previewReportFile(file)}
                  className="block max-w-full truncate text-left text-primary underline underline-offset-2 hover:text-primary/80"
                >
                  {basename(file.filepath)}
                </button>
              </td>
              <td className="px-3 py-2 text-muted-foreground">{extensionFor(file.filepath) || 'file'}</td>
              <td className="px-3 py-2 text-muted-foreground">{formatBytes(file.size) || '-'}</td>
              <td className="px-3 py-2 text-muted-foreground">{formatDate(file.last_modified) || '-'}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function FileGallery({ files, workspacePath, compact }: { files: PlannerFile[]; workspacePath: string; compact: boolean }) {
  return (
    <div className={`grid gap-3 ${compact ? 'grid-cols-1 sm:grid-cols-2' : 'grid-cols-1 sm:grid-cols-2 lg:grid-cols-3'}`}>
      {files.map(file => (
        <ArtifactCard key={file.filepath} file={file} workspacePath={workspacePath} compact={compact} />
      ))}
    </div>
  )
}

function ArtifactCard({ file, workspacePath, compact }: { file: PlannerFile; workspacePath: string; compact: boolean }) {
  const source = file.filepath.startsWith(`${workspacePath}/`) ? file.filepath.slice(workspacePath.length + 1) : file.filepath
  const widget = useMemo<ReportWidget>(() => ({
    kind: 'file',
    source,
    path: '',
    renderFormat: artifactKind(file.filepath) === 'image' || artifactKind(file.filepath) === 'video' ? 'auto' : 'link',
  }), [file.filepath, source])
  const kind = artifactKind(file.filepath)

  return (
    <button
      type="button"
      title={`Open ${file.filepath}`}
      onClick={() => previewReportFile(file)}
      className="block w-full overflow-hidden rounded-lg border border-border bg-background text-left transition-colors hover:border-primary/50 hover:bg-muted/30"
    >
      {!compact && (kind === 'image' || kind === 'video') ? (
        <div className="aspect-video bg-muted/30">
          <FilePreviewMedia widget={widget} workspacePath={workspacePath} kind={kind} />
        </div>
      ) : (
        <div className="flex aspect-video items-center justify-center bg-muted/30 text-muted-foreground">
          <ArtifactIcon kind={kind} />
        </div>
      )}
      <div className="min-w-0 px-3 py-2">
        <div className="truncate text-sm font-medium text-primary underline underline-offset-2">{basename(file.filepath)}</div>
        <div className="mt-0.5 flex items-center gap-2 text-xs text-muted-foreground">
          <span>{extensionFor(file.filepath) || 'file'}</span>
          {formatBytes(file.size) && <span>{formatBytes(file.size)}</span>}
        </div>
      </div>
    </button>
  )
}

// useAbsoluteFileContent loads a workspace file by its ABSOLUTE path (the form
// the planner-files API returns for report file-lists), mirroring useFileContent
// but without needing a ReportWidget/source. Used by the in-report preview modal.
function useAbsoluteFileContent(path: string, kind: ArtifactKind): FileContentState {
  const [state, setState] = useState<FileContentState>({ status: 'loading' })

  useEffect(() => {
    let cancelled = false
    let objectUrl: string | undefined
    setState({ status: 'loading' })

    const load = async () => {
      try {
        if (kind === 'image') {
          const response = await agentApi.getPlannerFileContent(path)
          const content = typeof response?.data?.content === 'string' ? response.data.content : ''
          if (content.startsWith('data:image/')) {
            if (!cancelled) setState({ status: 'ready', content })
            return
          }
          objectUrl = await loadBinaryObjectUrl(path)
          if (!cancelled) setState({ status: 'ready', objectUrl })
          return
        }
        if (kind === 'pdf' || kind === 'video' || kind === 'audio') {
          objectUrl = await loadBinaryObjectUrl(path)
          if (!cancelled) setState({ status: 'ready', objectUrl, mimeType: mimeTypeFor(path) })
          return
        }
        const response = await agentApi.getPlannerFileContent(path)
        const content = typeof response?.data?.content === 'string' ? response.data.content : ''
        if (!cancelled) setState({ status: 'ready', content })
      } catch (error) {
        if (!cancelled) setState({ status: 'error', message: error instanceof Error ? error.message : String(error) })
      }
    }

    void load()
    return () => {
      cancelled = true
      if (objectUrl) URL.revokeObjectURL(objectUrl)
    }
  }, [path, kind])

  return state
}

// FilePreviewByPath renders a single workspace file inline given its absolute
// path — the body of the in-report preview modal. Handles the same formats as
// the single-file FileWidget (pdf/image/video/audio/markdown/html/text/code/json).
export function FilePreviewByPath({ path, name }: { path: string; name?: string }) {
  const kind = artifactKind(path)
  const state = useAbsoluteFileContent(path, kind)
  const label = name || basename(path)

  if (state.status === 'loading') {
    return <div className="flex h-full items-center justify-center text-sm text-muted-foreground">Loading {label}…</div>
  }
  if (state.status === 'error') {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-2 px-4 text-center text-sm">
        <div className="text-destructive">Could not load {label}.</div>
        <div className="text-xs text-muted-foreground">{state.message}</div>
      </div>
    )
  }

  if (kind === 'markdown') {
    return (
      <div className="h-full overflow-auto px-4 py-3 text-sm text-foreground">
        <MarkdownRenderer content={state.content || ''} basePath={path} className="max-w-none" maxHeight="none" />
      </div>
    )
  }
  if (kind === 'html') {
    return (
      <iframe title={label} srcDoc={state.content || ''} sandbox="allow-same-origin allow-scripts" className="h-full w-full bg-background" />
    )
  }
  if (kind === 'pdf' && state.objectUrl) {
    return <iframe title={label} src={state.objectUrl} className="h-full w-full bg-background" />
  }
  if (kind === 'image') {
    return (
      <div className="flex h-full items-center justify-center bg-background p-3">
        <img src={state.content || state.objectUrl} alt={label} className="max-h-full max-w-full object-contain" />
      </div>
    )
  }
  if (kind === 'video' && state.objectUrl) {
    return (
      <div className="flex h-full items-center justify-center bg-black p-3">
        <video src={state.objectUrl} controls className="max-h-full max-w-full" />
      </div>
    )
  }
  if (kind === 'audio' && state.objectUrl) {
    return (
      <div className="flex h-full items-center justify-center p-4">
        <audio src={state.objectUrl} controls className="w-full max-w-xl" />
      </div>
    )
  }
  if (kind === 'text' || kind === 'code' || kind === 'json') {
    return (
      <pre className="h-full overflow-auto bg-muted/20 px-4 py-3 text-xs leading-6 text-foreground">
        {kind === 'json' ? formatJSONText(state.content || '') : state.content}
      </pre>
    )
  }
  // 'other' — no inline renderer; offer a hint.
  return (
    <div className="flex h-full flex-col items-center justify-center gap-2 text-center text-sm text-muted-foreground">
      <ArtifactIcon kind={kind} />
      <div>No inline preview for this file type.</div>
      <div className="text-xs">{label}</div>
    </div>
  )
}

function FilePreviewMedia({ widget, workspacePath, kind }: { widget: ReportWidget; workspacePath: string; kind: ArtifactKind }) {
  const state = useFileContent(widget, workspacePath)
  if (state.status !== 'ready') {
    return <div className="flex h-full items-center justify-center text-xs text-muted-foreground">{state.status === 'loading' ? 'Loading...' : 'Preview unavailable'}</div>
  }
  if (kind === 'image') {
    return <img src={state.content || state.objectUrl} alt={basename(widget.source)} className="h-full w-full object-cover" />
  }
  if (kind === 'video' && state.objectUrl) {
    return <video src={state.objectUrl} controls className="h-full w-full bg-black object-contain" />
  }
  return (
    <div className="flex h-full items-center justify-center text-muted-foreground">
      <FolderOpen className="h-5 w-5" />
    </div>
  )
}
