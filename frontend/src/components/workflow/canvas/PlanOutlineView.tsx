import React, { useState, useMemo, useCallback, useEffect } from 'react'
import {
  ChevronDown,
  ChevronRight,
  Folder,
  FolderOpen,
  FileText,
  FileCode2,
  BookOpen,
  GitBranch,
  User,
  Route,
  ListTodo,
  CheckCircle2,
  Loader2,
  XCircle,
  Zap,
  Settings,
  Braces,
  Play,
  X,
  RefreshCw,
} from 'lucide-react'
import type { PlanStep, PlanningResponse, PlanRoutingRoute } from '../../../utils/stepConfigMatching'
import type { StepProgress } from '../../../services/api-types'
import { agentApi } from '../../../services/api'
import { MarkdownRenderer } from '../../ui/MarkdownRenderer'

interface PlanOutlineViewProps {
  plan: PlanningResponse
  stepProgress: StepProgress | null
  stepStatusMap: Map<string, 'pending' | 'running' | 'completed' | 'failed'>
  onStepClick?: (stepId: string) => void
  onFileClick?: (filePath: string) => void
  onRefresh?: () => Promise<void>
  workspacePath?: string | null
  className?: string
}

// ── Types ────────────────────────────────────────────────────
interface VirtualFile {
  name: string
  icon: React.ElementType
  iconClass: string
  content: string
  /** If set, fetch real content from this workspace path */
  workspacePath?: string
  /** Mark as a folder that should be lazy-loaded */
  isLazyFolder?: boolean
  /** Workspace folder to list children from */
  workspaceFolder?: string
}

// Selected file for the content panel
interface SelectedFile {
  key: string
  name: string
  icon: React.ElementType
  iconClass: string
  content: string
  stepTitle: string
  isMarkdown: boolean
  isLoading?: boolean
}

// ── Helpers ──────────────────────────────────────────────────
function StatusDot({ status }: { status?: 'pending' | 'running' | 'completed' | 'failed' }) {
  switch (status) {
    case 'completed':
      return <CheckCircle2 className="w-3 h-3 text-green-500 flex-shrink-0" />
    case 'running':
      return <Loader2 className="w-3 h-3 text-blue-500 animate-spin flex-shrink-0" />
    case 'failed':
      return <XCircle className="w-3 h-3 text-red-500 flex-shrink-0" />
    default:
      return null
  }
}

function stepTypeIcon(step: PlanStep): { icon: React.ElementType; accent: string } {
  switch (step.type) {
    case 'conditional': return { icon: GitBranch, accent: 'text-purple-500' }
    case 'decision': return { icon: Zap, accent: 'text-amber-500' }
    case 'human_input': return { icon: User, accent: 'text-blue-500' }
    case 'todo_task': return { icon: ListTodo, accent: 'text-teal-500' }
    case 'routing': return { icon: Route, accent: 'text-orange-500' }
    default:
      return { icon: Play, accent: 'text-muted-foreground' }
  }
}

function fileIcon(name: string): { icon: React.ElementType; iconClass: string } {
  if (name.endsWith('.py')) return { icon: FileCode2, iconClass: 'text-blue-500' }
  if (name.endsWith('.md')) return { icon: FileText, iconClass: 'text-muted-foreground' }
  if (name.endsWith('.json')) return { icon: Braces, iconClass: 'text-yellow-500' }
  return { icon: FileText, iconClass: 'text-muted-foreground' }
}

function isRichRenderFile(name: string): boolean {
  return name.endsWith('.md') || name.endsWith('.py')
}

/** Wrap code files in a markdown code block for MarkdownRenderer */
function wrapContentForRender(name: string, content: string): string {
  if (name.endsWith('.py')) return '```python\n' + content + '\n```'
  return content
}

// ── Build virtual files for a step ───────────────────────────
function buildFiles(step: PlanStep): VirtualFile[] {
  const files: VirtualFile[] = []

  // Flat format: description and validation_schema are directly on the step
  const description = step.description
  const validationSchema = step.validation_schema
  const decisionDesc = step.type === 'decision' && step.decision_step?.description && !description
    ? step.decision_step.description : null

  if (description) {
    files.push({ name: 'README.md', icon: FileText, iconClass: 'text-muted-foreground', content: description })
  } else if (decisionDesc) {
    files.push({ name: 'README.md', icon: FileText, iconClass: 'text-muted-foreground', content: decisionDesc })
  }

  if (validationSchema?.files?.length) {
    const lines = validationSchema.files.map(f => {
      const checks = f.json_checks?.length ? `  (${f.json_checks.length} validation checks)` : ''
      return `${f.file_name}${f.must_exist ? '  [required]' : ''}${checks}`
    })
    files.push({ name: 'schema.json', icon: Braces, iconClass: 'text-yellow-500', content: lines.join('\n') })
  }

  const ac = step.agent_configs
  if (ac) {
    const parts: string[] = []
    if (ac.execution_llm) parts.push(`"execution_llm": "${ac.execution_llm}"`)
    if (ac.learning_llm) parts.push(`"learning_llm": "${ac.learning_llm}"`)
    if (ac.execution_max_turns) parts.push(`"execution_max_turns": ${ac.execution_max_turns}`)
    if (ac.selected_servers?.length) parts.push(`"selected_servers": [${ac.selected_servers.map(s => `"${s}"`).join(', ')}]`)
    if (ac.selected_tools?.length) parts.push(`"selected_tools": [${ac.selected_tools.map(t => `"${t}"`).join(', ')}]`)
    if (ac.use_code_execution_mode) parts.push(`"use_code_execution_mode": true`)
    if (ac.disable_learning) parts.push(`"disable_learning": true`)
    if (ac.lock_learnings) parts.push(`"lock_learnings": true`)
    if (parts.length) {
      files.push({ name: 'config.json', icon: Settings, iconClass: 'text-muted-foreground', content: '{\n  ' + parts.join(',\n  ') + '\n}' })
    }
  }

  // Learnings as a lazy-loaded folder
  if (step.id && !ac?.disable_learning) {
    files.push({
      name: 'learnings',
      icon: BookOpen,
      iconClass: 'text-amber-500',
      content: '',
      isLazyFolder: true,
      workspaceFolder: `learnings/${step.id}`,
    })
  }

  // Type-specific
  if (step.type === 'conditional' && step.condition_question) {
    files.push({ name: 'condition.md', icon: GitBranch, iconClass: 'text-purple-500', content: step.condition_question })
  }
  if (step.type === 'decision' && step.decision_evaluation_question) {
    files.push({ name: 'evaluation.md', icon: Zap, iconClass: 'text-amber-500', content: step.decision_evaluation_question })
  }
  if (step.type === 'human_input') {
    const c = step.question + (step.options?.length ? '\n\nOptions:\n' + step.options.map((o, i) => `${i + 1}. ${o}`).join('\n') : '')
    files.push({ name: 'prompt.md', icon: User, iconClass: 'text-blue-500', content: c })
  }
  if (step.type === 'routing' && step.routing_question) {
    const c = step.routing_question + (step.routes?.map(r => `\n• ${r.route_name} — ${r.condition}`).join('') || '')
    files.push({ name: 'routing.md', icon: Route, iconClass: 'text-orange-500', content: c })
  }
  if (step.context_dependencies?.length) {
    files.push({ name: 'dependencies.md', icon: FileText, iconClass: 'text-muted-foreground', content: step.context_dependencies.join('\n') })
  }

  return files
}

// ── Lazy folder that loads children from workspace API ────────
function LazyFolder({
  label,
  workspaceFolder,
  workspacePath,
  depth,
  accent,
  onSelectWorkspaceFile,
  activeFileKey,
}: {
  label: string
  workspaceFolder: string
  workspacePath?: string | null
  depth: number
  accent?: string
  onSelectWorkspaceFile: (key: string, name: string, wsPath: string) => void
  activeFileKey: string | null
}) {
  const [open, setOpen] = useState(false)
  const [children, setChildren] = useState<{ name: string; path: string }[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(false)

  const handleToggle = useCallback(async () => {
    const next = !open
    setOpen(next)
    if (!next) {
      // Reset cache on close so re-opening fetches fresh data
      setChildren(null)
      return
    }
    if (!loading) {
      setLoading(true)
      setError(false)
      try {
        const fullFolder = workspacePath ? `${workspacePath}/${workspaceFolder}` : workspaceFolder
        const resp = await agentApi.getPlannerFiles(fullFolder, -1, 2)

        // Response: { success, data: PlannerFile[] } — flat list of folder + file entries
        const allItems: any[] = resp?.data || (Array.isArray(resp) ? resp : [])
        // Keep only file entries (skip folder entries)
        const fileItems = allItems.filter((item: any) => item.type === 'file')
        // Ensure paths are full workspace-relative paths
        const files = fileItems.map((f) => {
          const fp = f.filepath || ''
          // If filepath doesn't start with the folder prefix, prepend it
          const fullPath = fp.startsWith(fullFolder) ? fp : (fp.includes('/') ? fp : `${fullFolder}/${fp}`)
          return {
            name: fp.split('/').pop() || fp,
            path: fullPath,
          }
        })

        setChildren(files)
      } catch (err) {

        setChildren([])
        setError(true)
      } finally {
        setLoading(false)
      }
    }
  }, [open, children, loading, workspaceFolder, workspacePath])

  return (
    <>
      <div
        className="flex items-center gap-1 py-[2px] cursor-pointer hover:bg-muted/50 transition-colors"
        style={{ paddingLeft: depth * 14 + 4 }}
        onClick={handleToggle}
      >
        {open
          ? <ChevronDown className="w-3 h-3 text-muted-foreground/50 flex-shrink-0" />
          : <ChevronRight className="w-3 h-3 text-muted-foreground/50 flex-shrink-0" />
        }
        {open
          ? <FolderOpen className={`w-3.5 h-3.5 flex-shrink-0 ${accent || 'text-amber-500/80'}`} />
          : <Folder className={`w-3.5 h-3.5 flex-shrink-0 ${accent || 'text-amber-500/80'}`} />
        }
        <span className="text-[12px] truncate select-none text-foreground/80">{label}</span>
        {loading && <Loader2 className="w-3 h-3 text-muted-foreground animate-spin flex-shrink-0" />}
      </div>
      {open && (
        <>
          {error && (
            <div className="text-[11px] text-muted-foreground/60 italic" style={{ paddingLeft: (depth + 1) * 14 + 20 }}>
              (empty)
            </div>
          )}
          {children && children.length === 0 && !loading && !error && (
            <div className="text-[11px] text-muted-foreground/60 italic" style={{ paddingLeft: (depth + 1) * 14 + 20 }}>
              (empty)
            </div>
          )}
          {children && children.map(child => {
            const fi = fileIcon(child.name)
            const key = `ws:${child.path}`
            return (
              <div
                key={child.path}
                className={`flex items-center gap-1 py-[2px] cursor-pointer transition-colors ${
                  activeFileKey === key ? 'bg-primary/10 text-primary' : 'hover:bg-muted/50 text-foreground/70'
                }`}
                style={{ paddingLeft: (depth + 1) * 14 + 4 }}
                onClick={() => onSelectWorkspaceFile(key, child.name, child.path)}
              >
                <span className="w-3" />
                <fi.icon className={`w-3.5 h-3.5 ${fi.iconClass} flex-shrink-0`} />
                <span className="text-[12px] truncate select-none">{child.name}</span>
              </div>
            )
          })}
        </>
      )}
    </>
  )
}

// ── Reports folder — loads from workspace, auto-selects latest ──
function ReportsFolder({
  workspacePath,
  depth,
  onSelectWorkspaceFile,
  activeFileKey,
  refreshKey = 0,
}: {
  workspacePath: string
  depth: number
  onSelectWorkspaceFile: (key: string, name: string, wsPath: string) => void
  activeFileKey: string | null
  refreshKey?: number
}) {
  const [open, setOpen] = useState(true) // open by default
  const [groups, setGroups] = useState<{ name: string; files: { name: string; path: string }[] }[] | null>(null)
  const [loading, setLoading] = useState(false)
  // Use module-level flag so it survives unmount/remount

  // Fetch reports on mount and on refresh
  useEffect(() => {
    let cancelled = false
    const fetchReports = async () => {
      setLoading(true)
      try {
        const folder = `${workspacePath}/reports`
        const resp = await agentApi.getPlannerFiles(folder, -1, 3)
        const allItems: any[] = resp?.data || []

        // Parse: top-level folders are groups, files inside are reports
        const groupMap = new Map<string, { name: string; path: string }[]>()
        for (const item of allItems) {
          if (item.type === 'folder' && item.filepath !== folder) {
            // It's a group folder
            const groupName = item.filepath.split('/').pop() || item.filepath
            if (!groupMap.has(groupName)) groupMap.set(groupName, [])
            // Check for children
            if (item.children && Array.isArray(item.children)) {
              for (const child of item.children) {
                if (child.type === 'file') {
                  groupMap.get(groupName)!.push({
                    name: child.filepath.split('/').pop() || child.filepath,
                    path: child.filepath,
                  })
                }
              }
            }
          } else if (item.type === 'file' && item.filepath !== folder) {
            // File directly in reports/ (no group)
            const parts = item.filepath.replace(folder + '/', '').split('/')
            if (parts.length >= 2) {
              // e.g. reports/group/file.md
              const groupName = parts[0]
              if (!groupMap.has(groupName)) groupMap.set(groupName, [])
              groupMap.get(groupName)!.push({
                name: parts.slice(1).join('/'),
                path: item.filepath,
              })
            } else {
              // File directly under reports/
              const groupName = '_root'
              if (!groupMap.has(groupName)) groupMap.set(groupName, [])
              groupMap.get(groupName)!.push({
                name: parts[0],
                path: item.filepath,
              })
            }
          }
        }

        if (cancelled) return

        // Sort files within each group by name descending (latest timestamp first)
        const result = Array.from(groupMap.entries()).map(([name, files]) => ({
          name,
          files: files.sort((a, b) => b.name.localeCompare(a.name)),
        }))
        setGroups(result)
      } catch {
        if (!cancelled) setGroups([])
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    fetchReports()
    return () => { cancelled = true }
  }, [workspacePath, refreshKey])

  // Auto-select latest report — only on first load when nothing is selected
  useEffect(() => {
    console.log('[POV ReportsFolder] auto-select effect:', { done: _reportAutoSelectDone, groupsLength: groups?.length, activeFileKey })
    if (_reportAutoSelectDone || !groups || groups.length === 0) return
    // Find the latest file across all groups
    let latest: { name: string; path: string; group: string } | null = null
    for (const g of groups) {
      if (g.files.length > 0) {
        const f = g.files[0] // already sorted desc
        if (!latest || f.name > latest.name) {
          latest = { ...f, group: g.name }
        }
      }
    }
    if (latest) {
      _reportAutoSelectDone = true
      onSelectWorkspaceFile(`ws:${latest.path}`, latest.name, latest.path)
    }
  }, [groups, activeFileKey, onSelectWorkspaceFile])

  if (groups !== null && groups.length === 0 && !loading) return null // no reports folder

  return (
    <>
      <div
        className="flex items-center gap-1 py-[2px] cursor-pointer hover:bg-muted/50 transition-colors"
        style={{ paddingLeft: depth * 14 + 4 }}
        onClick={() => setOpen(v => !v)}
      >
        {open
          ? <ChevronDown className="w-3 h-3 text-muted-foreground/50 flex-shrink-0" />
          : <ChevronRight className="w-3 h-3 text-muted-foreground/50 flex-shrink-0" />
        }
        {open
          ? <FolderOpen className="w-3.5 h-3.5 flex-shrink-0 text-green-500/80" />
          : <Folder className="w-3.5 h-3.5 flex-shrink-0 text-green-500/80" />
        }
        <span className="text-[12px] truncate select-none text-foreground/80 font-semibold">reports</span>
        {loading && <Loader2 className="w-3 h-3 text-muted-foreground animate-spin flex-shrink-0" />}
      </div>
      {open && groups && groups.map(group => (
        <React.Fragment key={group.name}>
          {/* Group subfolder — skip if only one group */}
          {groups.length > 1 && (
            <div
              className="flex items-center gap-1 py-[2px] text-foreground/60"
              style={{ paddingLeft: (depth + 1) * 14 + 4 }}
            >
              <span className="w-3" />
              <Folder className="w-3 h-3 flex-shrink-0 text-green-500/60" />
              <span className="text-[11px] truncate select-none">{group.name === '_root' ? '.' : group.name}</span>
            </div>
          )}
          {group.files.map(file => {
            const fi = fileIcon(file.name)
            const key = `ws:${file.path}`
            const itemDepth = groups.length > 1 ? depth + 2 : depth + 1
            return (
              <div
                key={file.path}
                className={`flex items-center gap-1 py-[2px] cursor-pointer transition-colors ${
                  activeFileKey === key ? 'bg-primary/10 text-primary' : 'hover:bg-muted/50 text-foreground/70'
                }`}
                style={{ paddingLeft: itemDepth * 14 + 4 }}
                onClick={() => onSelectWorkspaceFile(key, file.name, file.path)}
              >
                <span className="w-3" />
                <fi.icon className={`w-3.5 h-3.5 ${fi.iconClass} flex-shrink-0`} />
                <span className="text-[12px] truncate select-none">{file.name}</span>
              </div>
            )
          })}
        </React.Fragment>
      ))}
    </>
  )
}

// ── Tree folder ──────────────────────────────────────────────
function TreeFolder({
  label,
  depth,
  defaultOpen,
  accent,
  statusIcon,
  children,
}: {
  label: string
  depth: number
  defaultOpen?: boolean
  accent?: string
  statusIcon?: React.ReactNode
  children: React.ReactNode
}) {
  const [open, setOpen] = useState(defaultOpen ?? false)
  return (
    <>
      <div
        className="flex items-center gap-1 py-[2px] cursor-pointer hover:bg-muted/50 transition-colors"
        style={{ paddingLeft: depth * 14 + 4 }}
        onClick={() => setOpen(v => !v)}
      >
        {open
          ? <ChevronDown className="w-3 h-3 text-muted-foreground/50 flex-shrink-0" />
          : <ChevronRight className="w-3 h-3 text-muted-foreground/50 flex-shrink-0" />
        }
        {open
          ? <FolderOpen className={`w-3.5 h-3.5 flex-shrink-0 ${accent || 'text-amber-500/80'}`} />
          : <Folder className={`w-3.5 h-3.5 flex-shrink-0 ${accent || 'text-amber-500/80'}`} />
        }
        <span className="text-[12px] truncate select-none text-foreground/80">{label}</span>
        {statusIcon}
      </div>
      {open && children}
    </>
  )
}

// ── Tree file row ────────────────────────────────────────────
function TreeFile({
  file,
  depth,
  isActive,
  onClick,
}: {
  file: VirtualFile
  depth: number
  isActive: boolean
  onClick: () => void
}) {
  const Icon = file.icon
  return (
    <div
      className={`flex items-center gap-1 py-[2px] cursor-pointer transition-colors ${
        isActive ? 'bg-primary/10 text-primary' : 'hover:bg-muted/50 text-foreground/70'
      }`}
      style={{ paddingLeft: depth * 14 + 4 }}
      onClick={onClick}
    >
      <span className="w-3" />
      <Icon className={`w-3.5 h-3.5 ${file.iconClass} flex-shrink-0`} />
      <span className="text-[12px] truncate select-none">{file.name}</span>
    </div>
  )
}

// ── Step tree node ───────────────────────────────────────────
function StepTreeNode({
  step,
  index,
  depth,
  status,
  activeFileKey,
  onSelectFile,
  onSelectWorkspaceFile,
  onStepClick,
  workspacePath,
  defaultOpen = false,
}: {
  step: PlanStep
  index: number
  depth: number
  status?: 'pending' | 'running' | 'completed' | 'failed'
  activeFileKey: string | null
  onSelectFile: (key: string, file: VirtualFile, stepTitle: string) => void
  onSelectWorkspaceFile: (key: string, name: string, wsPath: string) => void
  onStepClick?: (stepId: string) => void
  workspacePath?: string | null
  defaultOpen?: boolean
}) {
  const files = useMemo(() => buildFiles(step), [step])
  const label = `${index + 1}. ${step.title || step.id}`
  const fileDepth = depth + 1
  const stepKey = step.id

  const childBranches: { label: string; steps: PlanStep[] }[] = []
  if (step.type === 'conditional') {
    if (step.if_true_steps?.length) childBranches.push({ label: 'if_true', steps: step.if_true_steps })
    if (step.if_false_steps?.length) childBranches.push({ label: 'if_false', steps: step.if_false_steps })
  }
  if (step.type === 'decision' && step.decision_step) {
    childBranches.push({ label: 'decision_step', steps: [step.decision_step] })
  }
  const todoRoutes: PlanRoutingRoute[] = step.type === 'todo_task' ? (step.predefined_routes || []) : []

  return (
    <TreeFolder label={label} depth={depth} defaultOpen={defaultOpen} statusIcon={<StatusDot status={status} />}>
      {files.map(f =>
        f.isLazyFolder && f.workspaceFolder ? (
          <LazyFolder
            key={f.name}
            label={f.name}
            workspaceFolder={f.workspaceFolder}
            workspacePath={workspacePath}
            depth={fileDepth}
            accent={f.iconClass}
            onSelectWorkspaceFile={onSelectWorkspaceFile}
            activeFileKey={activeFileKey}
          />
        ) : (
          <TreeFile
            key={f.name}
            file={f}
            depth={fileDepth}
            isActive={activeFileKey === `${stepKey}/${f.name}`}
            onClick={() => onSelectFile(`${stepKey}/${f.name}`, f, step.title || step.id)}
          />
        )
      )}

      {childBranches.map(({ label: branchLabel, steps: nested }) => (
        <TreeFolder key={branchLabel} label={branchLabel} depth={fileDepth} accent="text-purple-500/80">
          {nested.map((sub, si) => (
            <StepTreeNode
              key={sub.id}
              step={sub}
              index={si}
              depth={fileDepth + 1}
              activeFileKey={activeFileKey}
              onSelectFile={onSelectFile}
              onSelectWorkspaceFile={onSelectWorkspaceFile}
              onStepClick={onStepClick}
              workspacePath={workspacePath}
            />
          ))}
        </TreeFolder>
      ))}

      {todoRoutes.length > 0 && (
        <TreeFolder label="routes" depth={fileDepth} accent="text-teal-500/80">
          {todoRoutes.map((route, ri) => {
            if (!route.sub_agent_step) return null
            return (
              <StepTreeNode
                key={route.route_id}
                step={route.sub_agent_step}
                index={ri}
                depth={fileDepth + 1}
                activeFileKey={activeFileKey}
                onSelectFile={onSelectFile}
                onSelectWorkspaceFile={onSelectWorkspaceFile}
                onStepClick={onStepClick}
                workspacePath={workspacePath}
              />
            )
          })}
        </TreeFolder>
      )}
    </TreeFolder>
  )
}

// ── Content panel (right side) ───────────────────────────────
function ContentPanel({
  file,
  onClose,
}: {
  file: SelectedFile
  onClose: () => void
}) {
  return (
    <div className="flex flex-col h-full">
      {/* Tab bar */}
      <div className="flex items-center border-b border-border bg-muted/30 flex-shrink-0">
        <div className="flex items-center gap-1.5 px-3 py-1.5 bg-background border-r border-border text-[12px]">
          <file.icon className={`w-3.5 h-3.5 ${file.iconClass} flex-shrink-0`} />
          <span className="text-foreground/80">{file.name}</span>
          <button onClick={onClose} className="ml-1 p-0.5 rounded hover:bg-muted text-muted-foreground hover:text-foreground transition-colors">
            <X className="w-3 h-3" />
          </button>
        </div>
        <div className="flex-1" />
        <span className="text-[10px] text-muted-foreground px-3 truncate">{file.stepTitle}</span>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-y-auto p-4">
        {file.isLoading ? (
          <div className="flex items-center gap-2 text-muted-foreground">
            <Loader2 className="w-4 h-4 animate-spin" />
            <span className="text-sm">Loading...</span>
          </div>
        ) : file.isMarkdown && typeof file.content === 'string' && file.content ? (
          <div className="prose prose-sm max-w-none dark:prose-invert prose-p:my-1 prose-headings:my-2 prose-pre:my-1 prose-ul:my-1 prose-ol:my-1">
            <MarkdownRenderer content={wrapContentForRender(file.name, file.content)} className="max-w-none" />
          </div>
        ) : (
          <pre className="text-[13px] leading-relaxed text-foreground/80 whitespace-pre-wrap font-mono">
            {file.content}
          </pre>
        )}
      </div>
    </div>
  )
}

// Module-level flag: once a report has been auto-selected, don't do it again
// (survives component unmount/remount caused by parent re-renders)
let _reportAutoSelectDone = false

// ── Main component ───────────────────────────────────────────
export function PlanOutlineView({
  plan,
  stepProgress,
  stepStatusMap,
  onStepClick,
  onFileClick,
  onRefresh,
  workspacePath,
  className = '',
}: PlanOutlineViewProps) {
  console.log('[POV] PlanOutlineView render', { planSteps: plan.steps?.length, refreshKey: 'n/a' })
  const steps = plan.steps || []
  const orphanSteps = plan.orphan_steps || []

  useEffect(() => {
    console.log('[POV] PlanOutlineView MOUNTED')
    return () => console.log('[POV] PlanOutlineView UNMOUNTED')
  }, [])

  const [activeFile, _setActiveFile] = useState<SelectedFile | null>(null)
  const [refreshKey, setRefreshKey] = useState(0)

  // Wrap setActiveFile with logging
  const setActiveFile = useCallback((file: SelectedFile | null | ((prev: SelectedFile | null) => SelectedFile | null)) => {
    _setActiveFile(prev => {
      const next = typeof file === 'function' ? file(prev) : file
      console.log('[POV] setActiveFile:', { prevKey: prev?.key, nextKey: next?.key, nextName: next?.name })
      return next
    })
  }, [])

  // Select a virtual file (inline content)
  const handleSelectFile = useCallback((key: string, file: VirtualFile, stepTitle: string) => {
    setActiveFile({
      key,
      name: file.name,
      icon: file.icon,
      iconClass: file.iconClass,
      content: file.content,
      stepTitle,
      isMarkdown: isRichRenderFile(file.name),
    })
  }, [])

  // Select a real workspace file (fetches content from API)
  const handleSelectWorkspaceFile = useCallback(async (key: string, name: string, wsPath: string) => {
    const fi = fileIcon(name)
    setActiveFile({
      key,
      name,
      icon: fi.icon,
      iconClass: fi.iconClass,
      content: '',
      // Show short path: just the last 2 segments (e.g. "read-credentials/SKILL.md")
      stepTitle: wsPath.split('/').slice(-2).join('/'),
      isMarkdown: isRichRenderFile(name),
      isLoading: true,
    })
    try {
      const resp = await agentApi.getPlannerFileContent(wsPath)
      // Response shape: { success, data: { content: string, ... } }
      const rawContent = resp?.data?.content ?? resp?.content ?? (typeof resp === 'string' ? resp : '')
      const fileContent = typeof rawContent === 'string' ? rawContent : JSON.stringify(rawContent, null, 2)
      setActiveFile(prev => prev?.key === key ? { ...prev, content: fileContent, isLoading: false } : prev)
    } catch {
      setActiveFile(prev => prev?.key === key ? { ...prev, content: '(Failed to load file)', isLoading: false } : prev)
    }
  }, [])

  const handleClose = useCallback(() => {
    setActiveFile(null)
  }, [])

  return (
    <div className={`h-full flex bg-background ${className}`}>
      {/* Left: File tree */}
      <div className={`${activeFile ? 'w-64' : 'w-full max-w-md'} flex-shrink-0 border-r border-border overflow-y-auto pb-1 font-mono`}>
        {/* Refresh button */}
        <div className="flex items-center justify-end px-2 pt-1.5 pb-1">
          <button
            onClick={() => { setRefreshKey(k => k + 1); onRefresh?.() }}
            className="p-1 rounded hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
            title="Refresh"
          >
            <RefreshCw className="w-3 h-3" />
          </button>
        </div>

        {/* Reports folder — shown at top, auto-opens latest */}
        {workspacePath && (
          <ReportsFolder
            workspacePath={workspacePath}
            depth={0}
            onSelectWorkspaceFile={handleSelectWorkspaceFile}
            activeFileKey={activeFile?.key ?? null}
            refreshKey={refreshKey}
          />
        )}

        {steps.map((step, i) => {
          const status = stepStatusMap.get(step.id) ||
            (stepProgress?.completed_step_indices?.includes(i) ? 'completed' : undefined)
          return (
            <StepTreeNode
              key={step.id}
              step={step}
              index={i}
              depth={0}
              status={status}
              activeFileKey={activeFile?.key ?? null}
              onSelectFile={handleSelectFile}
              onSelectWorkspaceFile={handleSelectWorkspaceFile}
              onStepClick={onStepClick}
              workspacePath={workspacePath}
              defaultOpen={i === 0}
            />
          )
        })}

        {orphanSteps.length > 0 && (
          <TreeFolder label="orphan_steps" depth={0}>
            {orphanSteps.map((step, i) => (
              <StepTreeNode
                key={step.id}
                step={step}
                index={i}
                depth={1}
                activeFileKey={activeFile?.key ?? null}
                onSelectFile={handleSelectFile}
                onSelectWorkspaceFile={handleSelectWorkspaceFile}
                onStepClick={onStepClick}
                workspacePath={workspacePath}
              />
            ))}
          </TreeFolder>
        )}
      </div>

      {/* Right: Content panel */}
      {activeFile && (
        <div className="flex-1 min-w-0">
          <ContentPanel file={activeFile} onClose={handleClose} />
        </div>
      )}
    </div>
  )
}
