import { useEffect, useState, useCallback, type ReactNode } from 'react'
import { BookOpen, Loader2, AlertCircle, ChevronDown, ChevronRight, Code, FileText, Globe } from 'lucide-react'
import { agentApi } from '../../services/api'
import type { PlanningResponse } from '../../utils/stepConfigMatching'
import { MarkdownRenderer } from '../ui/MarkdownRenderer'
import type { PlannerFile } from '../../services/api-types'

interface LearningsViewProps {
  workspacePath: string | null
  plan: PlanningResponse | null
  headerAction?: ReactNode
}

type LearningFileFreshness = {
  lastConfirmedAt: string
  lastAction: string
}

const normalizeGlobalSkillRelPath = (filepath: string): string => {
  try {
    filepath = decodeURIComponent(filepath)
  } catch {
    // keep original path
  }
  return filepath
    .split(/[?#]/, 1)[0]
    .replace(/^\/+/, '')
    .split('/')
    .filter(segment => segment && segment !== '.')
    .join('/')
}

const isPatchArtifactPath = (filepath: string): boolean => {
  const normalized = normalizeGlobalSkillRelPath(filepath).toLowerCase()
  return normalized.endsWith('.orig') || normalized.endsWith('.rej')
}

function parseGlobalFileFreshness(content: string): Record<string, LearningFileFreshness> {
  try {
    const parsed: unknown = JSON.parse(content)
    if (!parsed || typeof parsed !== 'object') return {}
    const items = (parsed as { items?: unknown }).items
    if (!items || typeof items !== 'object') return {}

    return Object.fromEntries(
      Object.entries(items as Record<string, unknown>).flatMap(([path, value]) => {
        if (!value || typeof value !== 'object') return []
        const entry = value as { last_confirmed_at?: unknown; last_action?: unknown }
        const lastConfirmedAt = typeof entry.last_confirmed_at === 'string' ? entry.last_confirmed_at : ''
        if (!lastConfirmedAt) return []
        return [[path, {
          lastConfirmedAt,
          lastAction: typeof entry.last_action === 'string' ? entry.last_action : '',
        }]]
      }),
    )
  } catch {
    return {}
  }
}

function formatFreshnessDate(timestamp: string): string {
  const date = new Date(timestamp)
  if (!Number.isFinite(date.getTime())) return 'Fresh'
  return `Fresh ${date.toLocaleDateString([], { month: 'short', day: 'numeric' })}`
}

export default function LearningsView({ workspacePath, headerAction }: LearningsViewProps) {
  // Global skill state: SKILL.md content + the shared learning package tree.
  // Displayed as a featured card at the top (global skill is the primary artifact
  // under the current architecture — per-step learnings are secondary).
  const [globalSkillContent, setGlobalSkillContent] = useState<string>('')
  // globalFiles holds shared files under learnings/ (root markdown plus
  // references/, scripts/, assets/, and _global/) except the already-rendered SKILL.md. Each entry is
  // keyed by its relative path (e.g. "references/selectors.md") so grouping by dir
  // is trivial.
  const [globalFiles, setGlobalFiles] = useState<Array<{ name: string; relPath: string; absPath: string; dir: string }>>([])
  const [globalFileFreshness, setGlobalFileFreshness] = useState<Record<string, LearningFileFreshness>>({})
  const [globalSkillBasePath, setGlobalSkillBasePath] = useState<string>('')
  const [globalLoading, setGlobalLoading] = useState(false)
  const [globalError, setGlobalError] = useState<string | null>(null)
  const [globalExpanded, setGlobalExpanded] = useState(true)
  const [expandedFilePaths, setExpandedFilePaths] = useState<Set<string>>(new Set())
  const [fileContentCache, setFileContentCache] = useState<Record<string, string>>({})

  // Fetch the shared learning package on mount: SKILL.md content + the file
  // tree (root markdown, references/, scripts/, assets/, and _global/)
  // decided to write). Per-file content is lazy-loaded on click.
  useEffect(() => {
    if (!workspacePath) return
    let cancelled = false
    setGlobalLoading(true)
    setGlobalError(null)
    setGlobalSkillContent('')
    setGlobalSkillBasePath('')
    setGlobalFiles([])
    setGlobalFileFreshness({})
    setFileContentCache({})
    setExpandedFilePaths(new Set())

    const learningsPath = `${workspacePath}/learnings`
    const resolveAbs = (raw: string, relPath?: string): string => {
      const clean = raw.replace(/^\/+/, '')
      if (raw.startsWith(workspacePath) || clean.startsWith(workspacePath)) return clean
      if (clean.includes('/learnings/_global/')) return clean
      if (clean.startsWith('learnings/_global/')) return `${workspacePath}/${clean}`
      if (clean.includes('/learnings/')) return clean
      if (clean.startsWith('learnings/')) return `${workspacePath}/${clean}`
      return `${learningsPath}/${relPath || clean}`
    }
    const relFromLearnings = (absOrRel: string): string => {
      const normalized = absOrRel.replace(/\\/g, '/')
      const marker = '/learnings/'
      const idx = normalized.indexOf(marker)
      if (idx !== -1) return normalized.slice(idx + marker.length)
      if (normalized.startsWith('learnings/')) return normalized.slice('learnings/'.length)
      return normalized.replace(/^\/+/, '')
    }
    const isGlobalLearningPackageFile = (relPath: string): boolean => {
      if (!relPath || relPath.endsWith('/')) return false
      if (relPath === 'SKILL.md') return true
      if (/^[^/]+\.(md|markdown)$/i.test(relPath)) return true
      return /^(?:_global|references|scripts|assets)\//.test(relPath)
    }

    ;(async () => {
      try {
        const filesResponse = await agentApi.getPlannerFiles(learningsPath, 500, 3)
        const files: PlannerFile[] = Array.isArray(filesResponse)
          ? filesResponse as PlannerFile[]
          : (filesResponse?.data && Array.isArray(filesResponse.data) ? filesResponse.data as PlannerFile[] : [])

        // The planner API returns a tree: folders have children. Flatten recursively,
        // keeping only leaf file entries. Directory entries come back with
        // type === 'folder' (or with a non-empty children array) and must NOT be
        // passed to getPlannerFileContent — that's what caused "_(failed to load)_".
        const flatFiles: PlannerFile[] = []
        const walk = (nodes: PlannerFile[]) => {
          for (const node of nodes) {
            const isFolder = node.type === 'folder' || (Array.isArray(node.children) && node.children.length > 0)
            if (isFolder) {
              if (Array.isArray(node.children)) walk(node.children)
              continue
            }
            flatFiles.push(node)
          }
        }
        walk(files)

        // Pull SKILL.md first for the featured markdown view.
        const skill = flatFiles.find(f => {
          const rel = normalizeGlobalSkillRelPath(relFromLearnings(f.filepath || ''))
          return rel === '_global/SKILL.md' || rel === 'SKILL.md'
        })
        if (skill) {
          const skillRelPath = normalizeGlobalSkillRelPath(relFromLearnings(skill.filepath || ''))
          const skillPath = resolveAbs(skill.filepath || '', skillRelPath)
          const contentResp = await agentApi.getPlannerFileContent(skillPath)
          if (!cancelled && contentResp.success && contentResp.data?.content) {
            let text = contentResp.data.content
            if (text.startsWith('---')) {
              const endIdx = text.indexOf('\n---', 3)
              if (endIdx !== -1) text = text.slice(endIdx + 4).trim()
            }
            setGlobalSkillContent(text)
            setGlobalSkillBasePath(skillPath)
          }
        }

        const freshnessFile = flatFiles.find(f => normalizeGlobalSkillRelPath(relFromLearnings(f.filepath || '')) === '_global/_freshness.json')
        if (freshnessFile) {
          const freshnessPath = resolveAbs(freshnessFile.filepath || '', '_global/_freshness.json')
          const freshnessResp = await agentApi.getPlannerFileContent(freshnessPath)
          if (!cancelled && freshnessResp.success && freshnessResp.data?.content) {
            setGlobalFileFreshness(parseGlobalFileFreshness(freshnessResp.data.content))
          }
        }

        // Every other file in the global learning package (excluding SKILL.md +
        // .learning_metadata.json). Grouped by directory for display;
        // content fetched on demand.
        const dedupedByRelPath = new Map<string, { relPath: string; rawPath: string }>()
        for (const file of flatFiles) {
          const rawPath = file.filepath || ''
          const relPath = normalizeGlobalSkillRelPath(relFromLearnings(rawPath))

          if (!relPath || relPath === 'SKILL.md' || relPath === '_global/SKILL.md') continue
          // Freshness is display metadata for the files below, not a learning
          // artifact users need to open on its own.
          if (relPath === '_freshness.json') continue
          if (relPath.endsWith('.learning_metadata.json')) continue
          if (isPatchArtifactPath(relPath)) continue
          if (!isGlobalLearningPackageFile(relPath)) continue

          // The workspace documents API can include a file both as a top-level entry
          // and nested under its parent folder's children. Keep one row per path.
          if (!dedupedByRelPath.has(relPath)) {
            dedupedByRelPath.set(relPath, { relPath, rawPath })
          }
        }

        const tree = Array.from(dedupedByRelPath.values())
          .map(({ relPath, rawPath }) => {
            const name = relPath.split('/').pop() || relPath
            const dirPath = relPath.includes('/') ? relPath.slice(0, relPath.lastIndexOf('/')) : ''
            return { name, relPath, absPath: resolveAbs(rawPath, relPath), dir: dirPath }
          })
          .sort((a, b) => {
            if (a.dir === b.dir) return a.name.localeCompare(b.name)
            if (a.dir === '') return -1
            if (b.dir === '') return 1
            return a.dir.localeCompare(b.dir)
          })

        if (!cancelled) setGlobalFiles(tree)
      } catch (err: unknown) {
        const msg = err instanceof Error ? err.message : 'Unknown error'
        const isMissing = /not found|no such|doesn't exist|does not exist/i.test(msg)
        if (!cancelled && !isMissing) {
          console.error('[LearningsView] Error loading global skill:', err)
          setGlobalError('Failed to load global skill: ' + msg)
        }
      } finally {
        if (!cancelled) setGlobalLoading(false)
      }
    })()
    return () => { cancelled = true }
  }, [workspacePath])

  const loadGlobalFileContent = useCallback((relPath: string, absPath: string) => {
    if (fileContentCache[relPath] !== undefined) return
    agentApi.getPlannerFileContent(absPath).then(resp => {
      if (resp.success && resp.data?.content !== undefined) {
        setFileContentCache(prevC => ({ ...prevC, [relPath]: resp.data.content }))
      } else {
        setFileContentCache(prevC => ({ ...prevC, [relPath]: '_(empty or unreadable)_' }))
      }
    }).catch(() => {
      setFileContentCache(prevC => ({ ...prevC, [relPath]: '_(failed to load)_' }))
    })
  }, [fileContentCache])

  const relPathFromGlobalLink = useCallback((filepath: string, displayPath?: string): string | null => {
    const normalized = filepath.replace(/\\/g, '/')
    const candidates = [normalized, displayPath?.replace(/\\/g, '/')].filter((value): value is string => Boolean(value))
    const slashMarker = '/learnings/'
    for (const candidate of candidates) {
      const slashIndex = candidate.indexOf(slashMarker)
      if (slashIndex !== -1) {
        const relPath = normalizeGlobalSkillRelPath(candidate.slice(slashIndex + slashMarker.length))
        return relPath.split('/').includes('..') ? null : relPath
      }
    }

    const marker = 'learnings/'
    for (const candidate of candidates) {
      const markerIndex = candidate.indexOf(marker)
      if (markerIndex !== -1) {
        const relPath = normalizeGlobalSkillRelPath(candidate.slice(markerIndex + marker.length))
        return relPath.split('/').includes('..') ? null : relPath
      }
    }

    for (const candidate of candidates) {
      const relPath = normalizeGlobalSkillRelPath(candidate)
      if (!relPath || relPath.split('/').includes('..')) continue
      if (globalFiles.some(file => file.relPath === relPath)) {
        return relPath
      }
    }

    const relPath = normalizeGlobalSkillRelPath(normalized)
    if (relPath && /^[^/]+\.(md|markdown)$/i.test(relPath)) {
      return relPath.split('/').includes('..') ? null : relPath
    }

    return null
  }, [globalFiles])

  const resolveGlobalFileLink = useCallback((filepath: string, displayPath?: string): { relPath: string; absPath: string } | null => {
    if (!workspacePath) return null

    let relPath = relPathFromGlobalLink(filepath, displayPath)
    if (!relPath || relPath === 'SKILL.md') return null

    if (!globalFiles.some(file => file.relPath === relPath) && relPath.includes('/')) {
      const basename = relPath.split('/').pop() || ''
      const rootMatch = globalFiles.find(file => file.relPath === basename)
      if (rootMatch) relPath = rootMatch.relPath
    }

    const existing = globalFiles.find(file => file.relPath === relPath)
    const absPath = existing?.absPath || `${workspacePath}/learnings/${relPath}`
    return { relPath, absPath }
  }, [globalFiles, relPathFromGlobalLink, workspacePath])

  const getGlobalLinkDisplayPath = useCallback((absPath: string): string => {
    const normalized = absPath.replace(/\\/g, '/')
    const marker = '/workspace-docs/'
    const markerIndex = normalized.indexOf(marker)
    if (markerIndex !== -1) return normalized.slice(markerIndex + marker.length)
    return normalized
  }, [])

  const resolveGlobalFileForMarkdown = useCallback((filepath: string, displayPath: string): { filepath: string; displayPath: string } | null => {
    const resolved = resolveGlobalFileLink(filepath, displayPath)
    if (!resolved) return null
    return { filepath: resolved.absPath, displayPath: getGlobalLinkDisplayPath(resolved.absPath) }
  }, [getGlobalLinkDisplayPath, resolveGlobalFileLink])

  const openGlobalFileFromLink = useCallback((filepath: string, displayPath?: string): boolean => {
    const resolved = resolveGlobalFileLink(filepath, displayPath)
    if (!resolved) return false

    const { relPath, absPath } = resolved
    const existing = globalFiles.find(file => file.relPath === relPath)

    if (!existing) {
      setGlobalFiles(prev => {
        if (prev.some(file => file.relPath === relPath)) return prev
        const name = relPath.split('/').pop() || relPath
        const dir = relPath.includes('/') ? relPath.slice(0, relPath.lastIndexOf('/')) : ''
        return [...prev, { name, relPath, absPath, dir }].sort((a, b) => {
          if (a.dir === b.dir) return a.name.localeCompare(b.name)
          if (a.dir === '') return -1
          if (b.dir === '') return 1
          return a.dir.localeCompare(b.dir)
        })
      })
    }

    setGlobalExpanded(true)
    setExpandedFilePaths(prev => new Set(prev).add(relPath))
    loadGlobalFileContent(relPath, absPath)
    return true
  }, [globalFiles, loadGlobalFileContent, resolveGlobalFileLink])

  // Lazy-load a single file under _global/ when its row is expanded.
  const toggleGlobalFile = async (relPath: string, absPath: string) => {
    setExpandedFilePaths(prev => {
      const next = new Set(prev)
      if (next.has(relPath)) {
        next.delete(relPath)
      } else {
        next.add(relPath)
        loadGlobalFileContent(relPath, absPath)
      }
      return next
    })
  }

  return (
    <div className="flex h-full min-h-0 w-full flex-col bg-background text-foreground">
      <div className="flex items-start justify-between gap-3 border-b border-border flex-shrink-0 p-3 sm:p-4">
        <div className="flex min-w-0 items-center gap-2">
          <BookOpen className="w-5 h-5 text-primary" />
          <h2 className="truncate text-lg font-semibold">Automation Learnings</h2>
        </div>
        {headerAction}
      </div>

      <div className="flex-1 overflow-y-auto p-4">
        <div className="border border-border rounded-md bg-muted/20">
          <div
            className="p-3 cursor-pointer flex items-center justify-between hover:bg-muted/40 transition-colors rounded-md"
            onClick={() => setGlobalExpanded(!globalExpanded)}
          >
            <div className="flex items-center gap-2.5 min-w-0">
              <Globe className="w-4 h-4 text-muted-foreground shrink-0" />
              <div className="min-w-0">
                <div className="flex items-center gap-2 flex-wrap">
                  <h3 className="font-medium text-sm">Global Automation Skill</h3>
                  <span className="text-[10px] px-1.5 py-0.5 rounded bg-muted text-muted-foreground font-mono">
                    learnings/
                  </span>
                  {globalFiles.length > 0 && (
                    <span className="text-[10px] text-muted-foreground">
                      {globalFiles.length} file{globalFiles.length === 1 ? '' : 's'}
                    </span>
                  )}
                </div>
                <div className="text-[11px] text-muted-foreground mt-0.5 truncate">
                  Shared HOW-knowledge — every step with <code className="text-[10px]">read-write</code> access contributes.
                </div>
              </div>
            </div>
            <button className="p-0.5 hover:bg-muted rounded transition-colors shrink-0" aria-label={globalExpanded ? 'Collapse global skill' : 'Expand global skill'}>
              {globalExpanded ? (
                <ChevronDown className="w-3.5 h-3.5 text-muted-foreground" />
              ) : (
                <ChevronRight className="w-3.5 h-3.5 text-muted-foreground" />
              )}
            </button>
          </div>

          {globalExpanded && (
            <div className="border-t border-border px-4 py-3">
              {globalLoading && (
                <div className="flex items-center gap-2 text-muted-foreground text-sm py-4">
                  <Loader2 className="w-4 h-4 animate-spin" />
                  Loading global skill...
                </div>
              )}
              {!globalLoading && globalError && (
                <div className="flex items-center gap-2 p-3 bg-destructive/10 border border-destructive/20 rounded-md text-destructive text-sm">
                  <AlertCircle className="w-4 h-4" />
                  <span>{globalError}</span>
                </div>
              )}
              {!globalLoading && !globalError && !globalSkillContent && globalFiles.length === 0 && (
                <div className="text-sm text-muted-foreground italic py-4">
                  Global skill is empty. It will be generated as steps with <code>learnings_access: "read-write"</code> complete successful runs.
                </div>
              )}
              {!globalLoading && !globalError && globalSkillContent && (
                <div className="prose prose-sm max-w-none dark:prose-invert mb-3">
                  <MarkdownRenderer
                    content={globalSkillContent}
                    basePath={globalSkillBasePath || `${workspacePath}/learnings/_global/SKILL.md`}
                    onWorkspaceLinkResolve={resolveGlobalFileForMarkdown}
                    onWorkspaceLinkClick={openGlobalFileFromLink}
                    maxHeight="500px"
                    showScrollbar={true}
                  />
                </div>
              )}
              {!globalLoading && !globalError && globalFiles.length > 0 && (() => {
                const grouped = new Map<string, typeof globalFiles>()
                globalFiles.forEach(f => {
                  const arr = grouped.get(f.dir) || []
                  arr.push(f)
                  grouped.set(f.dir, arr)
                })
                const sortedDirs = Array.from(grouped.keys()).sort((a, b) => {
                  if (a === '') return -1
                  if (b === '') return 1
                  return a.localeCompare(b)
                })
                return (
                  <div className="mt-2 pt-3 border-t border-border space-y-3">
                    <div className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
                      Additional files ({globalFiles.length})
                    </div>
                    {sortedDirs.map(dir => {
                      const entries = grouped.get(dir)!
                      return (
                        <div key={dir || 'root'}>
                          {dir && (
                            <div className="text-[10px] font-mono text-muted-foreground mb-1 flex items-center gap-1">
                              <FileText className="w-2.5 h-2.5" />
                              {dir}/
                            </div>
                          )}
                          <div className="space-y-1">
                            {entries.map(file => {
                              const isExpanded = expandedFilePaths.has(file.relPath)
                              const isMarkdown = /\.(md|markdown)$/i.test(file.name)
                              const cached = fileContentCache[file.relPath]
                              const freshness = globalFileFreshness[file.relPath]
                              return (
                                <div key={file.relPath} className="border border-border rounded">
                                  <button
                                    onClick={() => toggleGlobalFile(file.relPath, file.absPath)}
                                    className="w-full flex items-center gap-2 px-2 py-1.5 text-left hover:bg-muted/40 transition-colors"
                                  >
                                    {isExpanded ? (
                                      <ChevronDown className="w-3 h-3 text-muted-foreground shrink-0" />
                                    ) : (
                                      <ChevronRight className="w-3 h-3 text-muted-foreground shrink-0" />
                                    )}
                                    {isMarkdown ? (
                                      <FileText className="w-3 h-3 text-muted-foreground shrink-0" />
                                    ) : (
                                      <Code className="w-3 h-3 text-muted-foreground shrink-0" />
                                    )}
                                    <span className="text-[11px] font-mono truncate flex-1">{file.name}</span>
                                    {freshness && (
                                      <span
                                        className="shrink-0 rounded bg-emerald-500/10 px-1.5 py-0.5 text-[9px] font-medium text-emerald-700 dark:text-emerald-300"
                                        title={`Last ${freshness.lastAction || 'confirmed'}: ${new Date(freshness.lastConfirmedAt).toLocaleString()}`}
                                      >
                                        {formatFreshnessDate(freshness.lastConfirmedAt)}
                                      </span>
                                    )}
                                    {!dir && (
                                      <span className="text-[9px] text-muted-foreground shrink-0">/</span>
                                    )}
                                  </button>
                                  {isExpanded && (
                                    <div className="border-t border-border px-2 py-2 bg-muted/10">
                                      {cached === undefined ? (
                                        <div className="flex items-center gap-2 text-xs text-muted-foreground">
                                          <Loader2 className="w-3 h-3 animate-spin" />
                                          Loading...
                                        </div>
                                      ) : isMarkdown ? (
                                        <div className="prose prose-sm max-w-none dark:prose-invert">
                                          <MarkdownRenderer
                                            content={cached}
                                            basePath={file.absPath}
                                            onWorkspaceLinkResolve={resolveGlobalFileForMarkdown}
                                            onWorkspaceLinkClick={openGlobalFileFromLink}
                                            maxHeight="300px"
                                            showScrollbar={true}
                                          />
                                        </div>
                                      ) : (
                                        <div className="relative rounded bg-slate-900 dark:bg-slate-950 overflow-hidden">
                                          <div className="max-h-[300px] overflow-auto">
                                            <pre className="p-3 text-[11px] font-mono text-slate-100 whitespace-pre-wrap break-words">
                                              <code>{cached}</code>
                                            </pre>
                                          </div>
                                        </div>
                                      )}
                                    </div>
                                  )}
                                </div>
                              )
                            })}
                          </div>
                        </div>
                      )
                    })}
                  </div>
                )
              })()}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
