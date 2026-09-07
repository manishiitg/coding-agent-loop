import { useEffect, useState, useCallback } from 'react'
import { X, BookOpen, Loader2, AlertCircle, ChevronDown, ChevronRight, Code, FileText, Trash2, Search, Globe, Check, Copy, ArrowLeft } from 'lucide-react'
import { agentApi } from '../../services/api'
import type { PlanningResponse, PlanStep } from '../../utils/stepConfigMatching'
import { effectiveExecutionMode, isRouteSwitchStep, isTodoTaskStep } from '../../utils/stepConfigMatching'
import { MarkdownRenderer } from '../ui/MarkdownRenderer'
import type { PlannerFile } from '../../services/api-types'
import ConfirmationDialog from '../ui/ConfirmationDialog'

interface LearningsViewProps {
  workspacePath: string | null
  plan: PlanningResponse | null
}

// LearningMetadata — fields read from learnings/{stepId}/.learning_metadata.json,
// merged with step_config.json entries by the backend API. Field names mirror the
// Go LearningMetadata struct and the AgentConfigs struct (snake_case in JSON).
type LearningsAccess = 'read' | 'read-write' | 'none'

type LearningFileFreshness = {
  lastConfirmedAt: string
  lastAction: string
}

interface LearningMetadata {
  step_id?: string
  successful_runs?: number
  last_turn_count?: number
  total_iterations?: number
  description_hash_runs?: number

  // Adaptive execution tiering (description-hash scoped). These are written by
  // controller_execution_tiering.go, NOT by any learnings mechanism: a step is
  // promoted High -> Medium after executionTierPromotionThreshold (3) stable
  // successful runs on an unchanged description, and a description change
  // resets the counter. They live here because the popup surfaces them.
  //
  // Step-config fields merged in by the backend
  use_code_execution_mode?: boolean
  learnings_access?: LearningsAccess
  learning_objective?: string

  // Global learning only: per-step contribution counts
  step_contributions?: Record<string, number>
}

// Check if learnings folder exists
// Returns true only if metadata contains actual learning data (not just step config fields)
// Step config fields (use_code_execution_mode, learnings_access) can exist
// even when the folder doesn't exist, so we need to check for actual learning data fields
function hasLearningsFolder(
  metadata: LearningMetadata | null,
  cachedContent: { content: string; codeContent?: string; codeFileName?: string; error: string | null } | undefined
): boolean {
  if (!metadata) return false
  
  // Check if metadata has actual learning data fields (not just step config)
  // These fields indicate the folder exists and has been used for learning:
  const hasLearningData = 
    metadata.step_id !== undefined ||
    metadata.successful_runs !== undefined ||
    metadata.last_turn_count !== undefined ||
    metadata.description_hash_runs !== undefined ||
    metadata.total_iterations !== undefined
  
  // If no learning data fields, folder doesn't exist (only step config fields present)
  if (!hasLearningData) return false
  
  // If we have cached content with an error indicating folder doesn't exist, return false
  if (cachedContent?.error) {
    const errorLower = cachedContent.error.toLowerCase()
    if (errorLower.includes('not found') || 
        errorLower.includes("doesn't exist") ||
        errorLower.includes('does not exist') ||
        errorLower.includes('no such file') ||
        errorLower.includes('no such directory')) {
      return false
    }
  }
  
  // Folder exists if we have learning data fields
  return true
}

// Parse learnings API response into typed Record
function parseLearningsResponse(learningsData: Record<string, unknown>): Record<string, LearningMetadata | null> {
  const result: Record<string, LearningMetadata | null> = {}
  for (const [stepId, metadata] of Object.entries(learningsData)) {
    result[stepId] = metadata as LearningMetadata | null
  }
  return result
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

// Get step title from plan
function getStepTitle(plan: PlanningResponse | null, stepId: string): string {
  if (stepId === '_global') return 'Automation Knowledge (Global)'
  if (!plan?.steps) return stepId

  const findStep = (steps: PlanStep[], id: string): PlanStep | null => {
    for (const step of steps) {
      if (step.id === id) return step
      // Check todo_task predefined_routes
      if ('predefined_routes' in step && step.predefined_routes) {
        for (const route of step.predefined_routes) {
          if (route.sub_agent_step && route.sub_agent_step.id === id) {
            return route.sub_agent_step
          }
        }
      }
    }
    return null
  }

  const step = findStep(plan.steps, stepId)
  return step?.title || stepId
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

export default function LearningsView({ workspacePath, plan }: LearningsViewProps) {
  const [learnings, setLearnings] = useState<Record<string, LearningMetadata | null>>({})
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  
  // Expanded items state - tracks which step IDs have their learning content expanded
  const [expandedStepIds, setExpandedStepIds] = useState<Set<string>>(new Set())
  
  // Learning content cache - stores fetched markdown content and code content for each step
  const [learningContentCache, setLearningContentCache] = useState<Record<string, { content: string; codeContent?: string; codeFileName?: string; error: string | null }>>({})
  
  // Loading states for individual items
  const [loadingStepIds, setLoadingStepIds] = useState<Set<string>>(new Set())

  
  // Delete state
  const [deletingStepIds, setDeletingStepIds] = useState<Set<string>>(new Set())
  const [deleteConfirmStepId, setDeleteConfirmStepId] = useState<string | null>(null)
  
  // Filter state - show only unlocked steps
  // Search state
  const [searchTerm, setSearchTerm] = useState('')

  // Global skill state: SKILL.md content + the full file tree under _global/.
  // Displayed as a featured card at the top (global skill is the primary artifact
  // under the current architecture — per-step learnings are secondary).
  const [globalSkillContent, setGlobalSkillContent] = useState<string>('')
  // globalFiles holds EVERY file under _global/ (references/, scripts/, assets/,
  // root-level markdown, etc.) except the already-rendered SKILL.md. Each entry is
  // keyed by its relative path (e.g. "references/selectors.md") so grouping by dir
  // is trivial.
  const [globalFiles, setGlobalFiles] = useState<Array<{ name: string; relPath: string; absPath: string; dir: string }>>([])
  const [globalFileFreshness, setGlobalFileFreshness] = useState<Record<string, LearningFileFreshness>>({})
  const [globalLoading, setGlobalLoading] = useState(false)
  const [globalError, setGlobalError] = useState<string | null>(null)
  const [globalExpanded, setGlobalExpanded] = useState(true)
  const [expandedFilePaths, setExpandedFilePaths] = useState<Set<string>>(new Set())
  const [fileContentCache, setFileContentCache] = useState<Record<string, string>>({})

  // Tab switching state for expanded code/readme sections per step
  const [stepTabs, setStepTabs] = useState<Record<string, 'readme' | 'code'>>({})
  // Copy status tracking state for each copied section
  const [copiedStatus, setCopiedStatus] = useState<Record<string, boolean>>({})

  // Standard premium copy-to-clipboard handler
  const copyToClipboard = useCallback((text: string, id: string) => {
    navigator.clipboard.writeText(text).then(() => {
      setCopiedStatus(prev => ({ ...prev, [id]: true }))
      setTimeout(() => {
        setCopiedStatus(prev => ({ ...prev, [id]: false }))
      }, 2000)
    }).catch((err) => {
      console.error('Failed to copy text: ', err)
    })
  }, [])

  // Effective learnings_access applies the auto-migration rule mirrored from the
  // backend's resolveLearningsAccess: if unset, infer from learning_objective.
  const effectiveAccess = useCallback((metadata: LearningMetadata | null): LearningsAccess => {
    if (!metadata) return 'read'
    if (metadata.learnings_access === 'read' || metadata.learnings_access === 'read-write' || metadata.learnings_access === 'none') {
      return metadata.learnings_access
    }
    if (metadata.learning_objective && metadata.learning_objective.trim() !== '') {
      return 'read-write'
    }
    return 'read'
  }, [])

  // Fetch learnings on mount (API now includes step config data merged in)
  useEffect(() => {
    if (!workspacePath) return

    setIsLoading(true)
    setError(null)

    agentApi.getAllStepLearnings(workspacePath)
      .then((response) => {
        if (response.success) {
          console.log('[LEARNINGS_POPUP_DEBUG] fetched', {
            workspacePath,
            learningStepIds: Object.keys(response.learnings || {}),
          })
          setLearnings(parseLearningsResponse(response.learnings || {}))
        } else {
          setError('Failed to load learnings')
        }
      })
      .catch((err: Error) => {
        console.error('[LearningsView] Error fetching learnings:', err)
        setError('Failed to load learnings: ' + (err.message || 'Unknown error'))
      })
      .finally(() => {
        setIsLoading(false)
      })
  }, [workspacePath])

  // Fetch everything under _global/ on mount: SKILL.md content + the full file
  // tree (references/, scripts/, assets/, any other artifacts the learning agent
  // decided to write). Per-file content is lazy-loaded on click.
  useEffect(() => {
    if (!workspacePath) return
    let cancelled = false
    setGlobalLoading(true)
    setGlobalError(null)
    setGlobalSkillContent('')
    setGlobalFiles([])
    setGlobalFileFreshness({})
    setFileContentCache({})
    setExpandedFilePaths(new Set())

    const globalPath = `${workspacePath}/learnings/_global`
    const resolveAbs = (raw: string): string => {
      const clean = raw.replace(/^\/+/, '')
      if (raw.startsWith(workspacePath) || clean.startsWith(workspacePath)) return clean
      if (clean.includes('/learnings/_global/')) return clean
      if (clean.startsWith('learnings/_global/')) return `${workspacePath}/${clean}`
      return `${globalPath}/${clean}`
    }
    const relFromGlobal = (absOrRel: string): string => {
      // Strip everything up to and including "/_global/" so the display key is stable.
      const idx = absOrRel.indexOf('/_global/')
      if (idx !== -1) return absOrRel.slice(idx + '/_global/'.length)
      // Already relative
      return absOrRel.replace(/^\/+/, '')
    }

    ;(async () => {
      try {
        const filesResponse = await agentApi.getPlannerFiles(globalPath, 500)
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
          const rel = relFromGlobal(f.filepath || '')
          return rel === 'SKILL.md'
        })
        if (skill) {
          const skillPath = resolveAbs(skill.filepath || '')
          const contentResp = await agentApi.getPlannerFileContent(skillPath)
          if (!cancelled && contentResp.success && contentResp.data?.content) {
            let text = contentResp.data.content
            if (text.startsWith('---')) {
              const endIdx = text.indexOf('\n---', 3)
              if (endIdx !== -1) text = text.slice(endIdx + 4).trim()
            }
            setGlobalSkillContent(text)
          }
        }

        const freshnessFile = flatFiles.find(f => relFromGlobal(f.filepath || '') === '_freshness.json')
        if (freshnessFile) {
          const freshnessPath = resolveAbs(freshnessFile.filepath || '')
          const freshnessResp = await agentApi.getPlannerFileContent(freshnessPath)
          if (!cancelled && freshnessResp.success && freshnessResp.data?.content) {
            setGlobalFileFreshness(parseGlobalFileFreshness(freshnessResp.data.content))
          }
        }

        // Every other file (excluding SKILL.md + .learning_metadata.json + anything
        // that somehow resolved outside _global/). Grouped by directory for display;
        // content fetched on demand.
        const dedupedByRelPath = new Map<string, { relPath: string; rawPath: string }>()
        for (const file of flatFiles) {
          const rawPath = file.filepath || ''
          const relPath = normalizeGlobalSkillRelPath(relFromGlobal(rawPath))

          if (!relPath || relPath === 'SKILL.md') continue
          // Freshness is display metadata for the files below, not a learning
          // artifact users need to open on its own.
          if (relPath === '_freshness.json') continue
          if (relPath.endsWith('.learning_metadata.json')) continue
          if (isPatchArtifactPath(relPath)) continue
          if (relPath.endsWith('/')) continue
          // Safety: only include files we can place under _global/. If relFromGlobal
          // didn't strip a /_global/ prefix AND the raw path doesn't look relative
          // (e.g. it's a sibling workflow folder), skip it — the listing probably
          // included a parent's content because _global/ is empty.
          if (!rawPath.includes('/_global/') && rawPath.includes('/') && !rawPath.startsWith('references/') && !rawPath.startsWith('scripts/') && !rawPath.startsWith('assets/')) {
            // Raw path has directory separators but none of them are under _global.
            // Likely outside the target folder. Exclude to avoid confusing UI rows.
            continue
          }

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
            return { name, relPath, absPath: resolveAbs(rawPath), dir: dirPath }
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

  // Lazy-load a single file under _global/ when its row is expanded.
  const toggleGlobalFile = async (relPath: string, absPath: string) => {
    setExpandedFilePaths(prev => {
      const next = new Set(prev)
      if (next.has(relPath)) {
        next.delete(relPath)
      } else {
        next.add(relPath)
        if (!fileContentCache[relPath]) {
          agentApi.getPlannerFileContent(absPath).then(resp => {
            if (resp.success && resp.data?.content !== undefined) {
              setFileContentCache(prevC => ({ ...prevC, [relPath]: resp.data.content }))
            } else {
              setFileContentCache(prevC => ({ ...prevC, [relPath]: '_(empty or unreadable)_' }))
            }
          }).catch(() => {
            setFileContentCache(prevC => ({ ...prevC, [relPath]: '_(failed to load)_' }))
          })
        }
      }
      return next
    })
  }

  const handleDeleteLearning = async (stepId: string) => {
    if (!workspacePath || deletingStepIds.has(stepId)) return

    setDeletingStepIds(prev => new Set(prev).add(stepId))
    setDeleteConfirmStepId(null)

    try {
      // Delete learnings folder
      const deleteResult = await agentApi.deleteStepLearnings(workspacePath, stepId)
      
      if (!deleteResult.success) {
        throw new Error(deleteResult.message || 'Failed to delete learnings')
      }

      // Remove from cache
      setLearningContentCache(prev => {
        const newCache = { ...prev }
        delete newCache[stepId]
        return newCache
      })

      // Remove from expanded items if it was expanded
      setExpandedStepIds(prev => {
        const newSet = new Set(prev)
        newSet.delete(stepId)
        return newSet
      })

      // Clear any error state
      setError(null)

      // Refresh learnings list to update UI
      const response = await agentApi.getAllStepLearnings(workspacePath)
      if (response.success) {
        setLearnings(parseLearningsResponse(response.learnings || {}))
      }
    } catch (err: unknown) {
      console.error('[LearningsView] Error deleting learnings:', err)
      const errorMessage = err instanceof Error ? err.message : 'Unknown error'
      setError('Failed to delete learnings: ' + errorMessage)
    } finally {
      setDeletingStepIds(prev => {
        const newSet = new Set(prev)
        newSet.delete(stepId)
        return newSet
      })
    }
  }

  // Fetch learning content when an item is expanded
  const fetchLearningContent = async (stepId: string) => {
    if (!workspacePath || learningContentCache[stepId]) {
      // Already cached or no workspace path
      return
    }

    setLoadingStepIds(prev => new Set(prev).add(stepId))

    try {
      const learningsPath = `${workspacePath}/learnings/${stepId}`
      let mdContent = ''
      let codeContent = ''
      let codeFileName = ''
      let error: string | null = null

      // The manifest, not directory presence, selects canonical code. A missing
      // learnings folder must not hide a new-layout step's saved implementation.
      const manifestResponse = await agentApi.getPlannerFileContent(`${workspacePath}/workflow.json`)
      const canonicalCode = manifestResponse.success && manifestResponse.data?.content
        ? JSON.parse(manifestResponse.data.content).code_layout_version === 1 : false
      if (canonicalCode) {
        const source = await agentApi.getPlannerFileContent(`${workspacePath}/code/${stepId}/main.py`)
        if (source.success && source.data?.content) {
          codeContent = source.data.content
          codeFileName = 'main.py'
        }
      }

      const flattenLeafFiles = (items: Array<PlannerFile & { name?: string }>) => {
        const out: Array<PlannerFile & { name?: string }> = []
        const seen = new Set<string>()
        const walk = (nodes: Array<PlannerFile & { name?: string }>) => {
          for (const node of nodes) {
            const isFolder = node.type === 'folder' || (Array.isArray(node.children) && node.children.length > 0)
            if (isFolder) {
              if (Array.isArray(node.children)) walk(node.children as Array<PlannerFile & { name?: string }>)
              continue
            }

            const key = node.filepath || node.name || ''
            if (!key || seen.has(key)) continue
            seen.add(key)
            out.push(node)
          }
        }
        walk(items)
        return out
      }

      const resolveAbsPath = (rawPath: string) => {
        let filePath = rawPath
        if (!filePath.startsWith(workspacePath)) {
          const cleanPath = filePath.startsWith('/') ? filePath.slice(1) : filePath
          filePath = `${workspacePath}/${cleanPath}`
        }
        if (!filePath.includes('/learnings/')) {
          filePath = `${learningsPath}/${filePath}`
        }
        return filePath
      }

      const relFromStepLearnings = (rawPath: string) => {
        const normalized = rawPath.replace(/\\/g, '/')
        const marker = `/learnings/${stepId}/`
        const idx = normalized.indexOf(marker)
        if (idx !== -1) return normalized.slice(idx + marker.length)
        return normalized.replace(/^\/+/, '')
      }

      // List files in the learnings folder to find the markdown file and saved scripts
      const filesResponse = await agentApi.getPlannerFiles(learningsPath, 100, 3).catch(() => [])
      const rawFiles: Array<PlannerFile & { name?: string }> = Array.isArray(filesResponse)
        ? filesResponse
        : (filesResponse?.data && Array.isArray(filesResponse.data) ? filesResponse.data : [])
      const files = flattenLeafFiles(rawFiles)

      // Find the first .md file (excluding metadata files)
      const mdFile = files.find((file) => {
        const fileName = file.filepath || file.name || ''
        return fileName.endsWith('.md') && !fileName.includes('.learning_metadata')
      })

      // Fetch markdown content
      if (mdFile) {
        const rawPath = mdFile.filepath || mdFile.name
        const filePath = rawPath ? resolveAbsPath(rawPath) : ''
        if (filePath) {
          const response = await agentApi.getPlannerFileContent(filePath)
          if (response.success && response.data && response.data.content) {
            mdContent = response.data.content
          }
        }
      }

      // Check for saved scripts. The canonical scripted artifact is
      // learnings/{stepId}/main.py; older/secondary artifacts may live under code/.
      const codeExtensions = ['.go', '.py', '.sh', '.js', '.ts', '.jsx', '.tsx', '.bash', '.curl', '.rb', '.java', '.rs', '.c', '.cpp', '.json', '.yaml', '.yml']
      let codeFiles = files.filter((file) => {
        const fileName = relFromStepLearnings(file.filepath || file.name || '')
        return codeExtensions.some(ext => fileName.endsWith(ext))
      })

      // Fallback: some workspace API responses only return top-level entries for the
      // folder listing, so check code/ explicitly as well.
      if (!canonicalCode && codeFiles.length === 0) {
        try {
          const codePath = `${learningsPath}/code`
          const codeFilesResponse = await agentApi.getPlannerFiles(codePath, 100)
          const rawCodeFiles: Array<PlannerFile & { name?: string }> = Array.isArray(codeFilesResponse)
            ? codeFilesResponse
            : (codeFilesResponse?.data && Array.isArray(codeFilesResponse.data) ? codeFilesResponse.data : [])
          codeFiles = flattenLeafFiles(rawCodeFiles).filter((file) => {
            const fileName = relFromStepLearnings(file.filepath || file.name || '')
            return codeExtensions.some(ext => fileName.endsWith(ext))
          })
        } catch {
          // code/ may not exist for non-scripted steps
        }
      }

      const codePriority = (file: PlannerFile & { name?: string }) => {
        const relPath = relFromStepLearnings(file.filepath || file.name || '')
        const baseName = relPath.split('/').pop() || relPath

        if (relPath === 'main.py') return 0
        if (relPath === 'code/main.py') return 1
        if (baseName === 'main.py') return 2
        if (relPath.startsWith('code/')) return 3
        return 4
      }

      const codeFile = [...codeFiles].sort((a, b) => {
        const priorityDiff = codePriority(a) - codePriority(b)
        if (priorityDiff !== 0) return priorityDiff
        const aRel = relFromStepLearnings(a.filepath || a.name || '')
        const bRel = relFromStepLearnings(b.filepath || b.name || '')
        return aRel.localeCompare(bRel)
      })[0]

      if (!canonicalCode && codeFile) {
        const rawCodeFilePath = codeFile.filepath || codeFile.name
        const codeFilePath = rawCodeFilePath ? resolveAbsPath(rawCodeFilePath) : ''
        if (codeFilePath) {
          codeFileName = codeFilePath.split('/').pop() || 'code'
          const codeResponse = await agentApi.getPlannerFileContent(codeFilePath)
          if (codeResponse.success && codeResponse.data && codeResponse.data.content) {
            codeContent = codeResponse.data.content
          }
        }
      }

      // Strip YAML frontmatter from SKILL.md files (---\n...\n---)
      if (mdContent && mdContent.startsWith('---')) {
        const endIndex = mdContent.indexOf('\n---', 3)
        if (endIndex !== -1) {
          mdContent = mdContent.slice(endIndex + 4).trim()
        }
      }

      if (!mdContent && !codeContent) {
        error = 'No learning content found'
      }

      setLearningContentCache(prev => ({
        ...prev,
        [stepId]: { content: mdContent, codeContent, codeFileName, error }
      }))
    } catch (err: unknown) {
      console.error('[LearningsView] Error fetching learning content:', err)
      const errorMessage = err instanceof Error ? err.message : 'Unknown error'
      setLearningContentCache(prev => ({
        ...prev,
        [stepId]: { content: '', error: 'Failed to load learning content: ' + errorMessage }
      }))
    } finally {
      setLoadingStepIds(prev => {
        const newSet = new Set(prev)
        newSet.delete(stepId)
        return newSet
      })
    }
  }

  // Toggle expand/collapse for a step
  const toggleExpand = (stepId: string) => {
    setExpandedStepIds(prev => {
      if (prev.has(stepId)) {
        return new Set()
      } else {
        // Fetch content if not cached
        if (!learningContentCache[stepId]) {
          fetchLearningContent(stepId)
        }
        return new Set([stepId])
      }
    })
  }

  // Collect all step IDs in execution order from plan with metadata
  const getStepsInExecutionOrder = useCallback((): Array<{ stepId: string; stepType: string }> => {
    if (!plan || !plan.steps) return []

    const stepsWithMetadata: Array<{ stepId: string; stepType: string }> = []
    const isScripted = (step: PlanStep): boolean => effectiveExecutionMode(step) === 'scripted'

    const collectSteps = (steps: PlanStep[]) => {
      steps.forEach((step) => {
        if (step.id && !isScripted(step) && !isRouteSwitchStep(step)) {
          const stepType = step.type || 'regular'
          stepsWithMetadata.push({
            stepId: step.id,
            stepType
          })
        }

        // Handle todo_task steps - collect sub-agent step IDs from predefined_routes
        if (isTodoTaskStep(step)) {
          if (step.predefined_routes) {
            step.predefined_routes.forEach((route) => {
              if (route.sub_agent_step && route.sub_agent_step.id && !isScripted(route.sub_agent_step)) {
                stepsWithMetadata.push({
                  stepId: route.sub_agent_step.id,
                  stepType: 'todo_sub_agent'
                })
              }
            })
          }
        }
      })
    }

    collectSteps(plan.steps)
    return stepsWithMetadata
  }, [plan])

  // Steps in execution order. _global is rendered separately as a featured card
  // above — no longer prepended into this list.
  const allStepsInOrder = getStepsInExecutionOrder()
  let stepsWithLearnings = allStepsInOrder.filter(step => step.stepId in learnings && step.stepId !== '_global')
  
  // Apply search filter
  if (searchTerm) {
    const lowerTerm = searchTerm.toLowerCase()
    stepsWithLearnings = stepsWithLearnings.filter(step => {
      const title = getStepTitle(plan, step.stepId).toLowerCase()
      const id = step.stepId.toLowerCase()
      return title.includes(lowerTerm) || id.includes(lowerTerm)
    })
  }

  const focusedStepId = expandedStepIds.values().next().value as string | undefined
  const visibleStepsWithLearnings = focusedStepId
    ? stepsWithLearnings.filter(step => step.stepId === focusedStepId)
    : stepsWithLearnings

  console.log('[LEARNINGS_POPUP_DEBUG] visible', {
    workspacePath,
    allPlanStepIds: allStepsInOrder.map(step => step.stepId),
    fetchedLearningStepIds: Object.keys(learnings),
    visibleLearningStepIds: stepsWithLearnings.map(step => step.stepId),
    searchTerm,
  })

  return (
      <div className="flex h-full min-h-0 w-full flex-col bg-background text-foreground">
        {/* Header — title only. Step search / filter / expand controls sit
            above the step list so they're visually adjacent to what they
            operate on (the per-step section, not the global skill). */}
        <div className="flex items-start justify-between gap-3 border-b border-border flex-shrink-0 p-3 sm:p-4">
          <div className="flex min-w-0 items-center gap-2">
            <BookOpen className="w-5 h-5 text-primary" />
            <h2 className="truncate text-lg font-semibold">Automation Learnings</h2>
          </div>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-y-auto p-4 space-y-4">
          {isLoading && (
            <div className="flex items-center justify-center py-8">
              <Loader2 className="w-6 h-6 animate-spin text-primary" />
              <span className="ml-2 text-muted-foreground">Loading learnings...</span>
            </div>
          )}

          {error && (
            <div className="flex items-center gap-2 p-4 bg-destructive/10 border border-destructive/20 rounded-md text-destructive">
              <AlertCircle className="w-5 h-5" />
              <span>{error}</span>
            </div>
          )}

          {/* Global Skill — primary artifact, rendered as a featured card. */}
          {!isLoading && !error && (
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
                        learnings/_global/
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
                        basePath={`${workspacePath}/learnings/_global/SKILL.md`}
                        maxHeight="500px"
                        showScrollbar={true}
                      />
                    </div>
                  )}
                  {!globalLoading && !globalError && globalFiles.length > 0 && (() => {
                    // Group files by directory for display. "" = root-level files.
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
                                  const isMarkdown = file.name.endsWith('.md')
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
                                              <MarkdownRenderer content={cached} basePath={`${workspacePath}/learnings/_global/${file.relPath}`} maxHeight="300px" showScrollbar={true} />
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
          )}

          {/* Step toolbar — search and expand/collapse controls.
              Placed here (after the global skill card) because these actions
              only apply to the per-step section below. */}
          {!isLoading && !error && (
            <div className="flex items-center gap-2 pt-1">
              <div className="relative flex-1">
                <Search className="absolute left-2.5 top-2 w-4 h-4 text-muted-foreground" />
                <input
                  type="text"
                  placeholder="Search steps..."
                  value={searchTerm}
                  onChange={(e) => setSearchTerm(e.target.value)}
                  className="w-full pl-9 pr-8 py-1.5 text-sm bg-muted/40 border border-input rounded-md focus:outline-none focus:ring-1 focus:ring-primary"
                />
                {searchTerm && (
                  <button
                    onClick={() => setSearchTerm('')}
                    className="absolute right-2 top-1.5 p-0.5 rounded-full hover:bg-muted transition-colors"
                  >
                    <X className="w-3 h-3 text-muted-foreground" />
                  </button>
                )}
              </div>
            </div>
          )}

          {/* Per-step list — secondary, metadata + main.py + inline controls. */}
          {!isLoading && !error && stepsWithLearnings.length === 0 && (
            <div className="text-center py-8 text-muted-foreground flex flex-col items-center gap-2">
              <BookOpen className="w-10 h-10 opacity-20" />
              <p>No per-step learning metadata yet</p>
              {searchTerm && <p className="text-sm">Try adjusting your search query</p>}
            </div>
          )}

          {!isLoading && !error && stepsWithLearnings.length > 0 && (
            <div className="space-y-3">
              {focusedStepId && (
                <button
                  type="button"
                  onClick={() => setExpandedStepIds(new Set())}
                  className="sticky top-0 z-20 inline-flex items-center gap-2 rounded-md border border-border bg-background/95 px-3 py-2 text-sm font-medium shadow-sm backdrop-blur-sm transition-colors hover:bg-accent"
                >
                  <ArrowLeft className="h-4 w-4" />
                  Back to all steps
                </button>
              )}
              {visibleStepsWithLearnings.map(({ stepId, stepType }) => {
                const metadata = learnings[stepId]
                const access = effectiveAccess(metadata || null)
                const objective = (metadata?.learning_objective || '').trim()
                const stepTitle = getStepTitle(plan, stepId)

                const isExpanded = expandedStepIds.has(stepId)
                const isLoadingContent = loadingStepIds.has(stepId)
                const cachedContent = learningContentCache[stepId]

                // Check if this is a sub-agent (should be indented)
                const isSubAgent = stepType === 'sub_agent' || stepType === 'todo_sub_agent'

                // Determine border and active hover accent based on step type
                const getBorderAccent = () => {
                  if (stepType === 'global') return 'hover:border-emerald-500/50 hover:shadow-emerald-500/5'
                  if (isSubAgent) return 'border-l-4 border-l-orange-500 hover:border-orange-500/50 hover:shadow-orange-500/5'
                  if (stepType === 'decision_inner') return 'hover:border-indigo-500/50 hover:shadow-indigo-500/5'
                  return 'hover:border-sky-500/50 hover:shadow-sky-500/5'
                }

                return (
                  <div
                    key={stepId}
                    className={`relative border border-border rounded-xl bg-muted/10 dark:bg-card/40 hover:bg-muted/20 dark:hover:bg-card/75 transition-all duration-300 shadow-sm hover:shadow-md overflow-hidden ${
                      isSubAgent ? 'ml-6' : ''
                    } ${getBorderAccent()}`}
                  >
                    {isSubAgent && (
                      <div className="absolute -left-6 top-0 bottom-0 w-6 flex items-center justify-center pointer-events-none">
                        <div className="border-l-2 border-b-2 border-dashed border-border/70 w-3 h-1/2 self-start rounded-bl-lg"></div>
                      </div>
                    )}
                    <div
                      className="p-4 cursor-pointer"
                      onClick={() => toggleExpand(stepId)}
                    >
                      <div className="flex items-start justify-between gap-3">
                        <div className="flex-1 min-w-0">
                          <div className={`flex items-center gap-2.5 flex-wrap sm:flex-nowrap ${isExpanded ? 'mb-2.5' : ''}`}>
                            <button
                              onClick={(e) => {
                                e.stopPropagation()
                                toggleExpand(stepId)
                              }}
                              className="p-1 hover:bg-muted rounded-md transition-colors shrink-0 flex items-center justify-center"
                              title={isExpanded ? "Collapse" : "Expand"}
                              aria-label={`${isExpanded ? 'Collapse' : 'Expand'} ${stepTitle} learnings`}
                              aria-expanded={isExpanded}
                            >
                              {isExpanded ? (
                                <ChevronDown className="w-4 h-4 text-muted-foreground" />
                              ) : (
                                <ChevronRight className="w-4 h-4 text-muted-foreground" />
                              )}
                            </button>
                            <h3 className="font-semibold text-sm truncate text-foreground hover:text-primary transition-colors flex-1" title={stepTitle}>
                              {stepTitle}
                            </h3>
                          </div>

                          {isExpanded && <div className="flex flex-col gap-2 ml-7">
                            {/* Read-only learning metadata. */}
                            <div className="flex items-center gap-2.5 flex-wrap text-xs">
                              <span className="rounded-md border border-border/60 bg-muted/40 px-2.5 py-1 text-[11px] font-medium text-muted-foreground">
                                Learnings access: <span className="text-foreground">{access}</span>
                              </span>

                              {/* Turns + Iter badges. */}
                              {metadata && metadata.last_turn_count !== undefined && metadata.last_turn_count > 0 && (
                                <span className="text-[10px] text-muted-foreground bg-muted/40 px-2 py-1 rounded-md border border-border/30">
                                  Turns: <span className="font-semibold text-foreground">{metadata.last_turn_count}</span>
                                </span>
                              )}
                              {metadata && metadata.total_iterations !== undefined && (
                                <span className="text-[10px] text-muted-foreground bg-muted/40 px-2 py-1 rounded-md border border-border/30 ml-auto flex items-center gap-1">
                                  Iter: <span className="font-mono font-semibold text-foreground">{metadata.total_iterations}</span>
                                </span>
                              )}
                            </div>

                            {!metadata && (
                              <div className="text-xs text-muted-foreground italic mt-0.5">
                                No learning metadata yet
                              </div>
                            )}

                          </div>}
                        </div>

                        {/* Delete Button */}
                        {isExpanded && hasLearningsFolder(metadata, cachedContent) && (
                          <button
                            onClick={(e) => {
                              e.stopPropagation()
                              setDeleteConfirmStepId(stepId)
                            }}
                            disabled={deletingStepIds.has(stepId)}
                            className="p-1.5 rounded-lg text-muted-foreground hover:text-red-600 hover:bg-red-50 dark:hover:bg-red-950/30 transition-colors disabled:opacity-50 disabled:cursor-not-allowed shrink-0 self-start border border-transparent hover:border-red-200 dark:hover:border-red-900/30"
                            title="Delete learnings"
                          >
                            {deletingStepIds.has(stepId) ? (
                              <Loader2 className="w-3.5 h-3.5 animate-spin" />
                            ) : (
                              <Trash2 className="w-3.5 h-3.5" />
                            )}
                          </button>
                        )}
                      </div>
                    </div>

                    {/* Expanded Learning Content */}
                    {isExpanded && (
                      <div className="border-t border-border/60 px-5 py-5 bg-muted/10">
                        {/* Read-only learning objective */}
                        <div className="mb-5 p-4 bg-muted/10 dark:bg-card border border-border/80 rounded-xl shadow-sm">
                          <div className="mb-2.5">
                            <div className="text-xs font-bold text-muted-foreground uppercase tracking-wider flex items-center gap-1.5">
                              <BookOpen className="w-3.5 h-3.5 text-primary" />
                              Learning Objective
                            </div>
                          </div>
                          {objective ? (
                            <div className="text-xs text-foreground whitespace-pre-wrap font-mono leading-relaxed bg-muted/30 p-2.5 rounded-lg border border-border/40">{objective}</div>
                          ) : (
                            <div className="text-xs text-muted-foreground italic">
                              {access === 'read-write'
                                ? 'MISSING — learnings_access is "read-write" but objective is empty. Learning writes are gated until both are set.'
                                : 'Empty. Not required when learnings_access is "read" or "none".'}
                            </div>
                          )}
                        </div>

                        {isLoadingContent && (
                          <div className="flex items-center justify-center py-6">
                            <Loader2 className="w-6 h-6 animate-spin text-primary" />
                            <span className="ml-2.5 text-sm text-muted-foreground font-medium">Loading learning content...</span>
                          </div>
                        )}

                        {!isLoadingContent && cachedContent?.error && (
                          <div className="flex items-center gap-2.5 p-4 bg-destructive/10 border border-destructive/20 rounded-xl text-destructive text-sm shadow-sm">
                            <AlertCircle className="w-4 h-4" />
                            <span>{cachedContent.error}</span>
                          </div>
                        )}

                        {!isLoadingContent && cachedContent && !cachedContent.error && (
                          <div>
                            {(() => {
                              const currentTab = stepTabs[stepId] || (cachedContent.content ? 'readme' : 'code')
                              const hasReadme = !!cachedContent.content
                              const hasCode = !!cachedContent.codeContent

                              if (!hasReadme && !hasCode) {
                                return (
                                  <div className="text-center py-6 text-sm text-muted-foreground italic bg-card border border-border/60 rounded-xl">
                                    No learning content available
                                  </div>
                                )
                              }

                              return (
                                <div className="border border-border rounded-xl bg-card overflow-hidden shadow-sm">
                                  {/* Beautiful horizontal tabs */}
                                  <div className="flex items-center justify-between border-b border-border bg-muted/40 px-3 py-1.5 flex-wrap gap-2">
                                    <div className="flex gap-1">
                                      {hasReadme && (
                                        <button
                                          onClick={() => setStepTabs(prev => ({ ...prev, [stepId]: 'readme' }))}
                                          className={`px-3 py-1.5 rounded-lg text-xs font-semibold transition-all flex items-center gap-1.5 ${
                                            currentTab === 'readme'
                                              ? 'bg-background text-foreground shadow-sm border border-border/80'
                                              : 'text-muted-foreground hover:text-foreground hover:bg-muted/40'
                                          }`}
                                        >
                                          <FileText className="w-3.5 h-3.5 text-primary" />
                                          <span>Readme (SKILL.md)</span>
                                        </button>
                                      )}
                                      {hasCode && (
                                        <button
                                          onClick={() => setStepTabs(prev => ({ ...prev, [stepId]: 'code' }))}
                                          className={`px-3 py-1.5 rounded-lg text-xs font-semibold transition-all flex items-center gap-1.5 ${
                                            currentTab === 'code'
                                              ? 'bg-background text-foreground shadow-sm border border-border/80'
                                              : 'text-muted-foreground hover:text-foreground hover:bg-muted/40'
                                          }`}
                                        >
                                          <Code className="w-3.5 h-3.5 text-emerald-500" />
                                          <span>Agent Code ({cachedContent.codeFileName || 'main.py'})</span>
                                        </button>
                                      )}
                                    </div>

                                    {/* Copy to Clipboard Buttons */}
                                    <div className="flex items-center">
                                      {currentTab === 'readme' && hasReadme && (
                                        <button
                                          onClick={() => copyToClipboard(cachedContent.content, `${stepId}-readme`)}
                                          className="flex items-center gap-1.5 text-xs px-2.5 py-1 rounded-lg bg-background hover:bg-muted border border-border text-muted-foreground hover:text-foreground transition-all duration-200 shadow-sm"
                                        >
                                          {copiedStatus[`${stepId}-readme`] ? (
                                            <>
                                              <Check className="w-3.5 h-3.5 text-green-500" />
                                              <span className="text-green-500 font-bold">Copied!</span>
                                            </>
                                          ) : (
                                            <>
                                              <Copy className="w-3.5 h-3.5" />
                                              <span>Copy Markdown</span>
                                            </>
                                          )}
                                        </button>
                                      )}
                                      {currentTab === 'code' && hasCode && (
                                        <button
                                          onClick={() => copyToClipboard(cachedContent.codeContent || '', `${stepId}-code`)}
                                          className="flex items-center gap-1.5 text-xs px-2.5 py-1 rounded-lg bg-background hover:bg-muted border border-border text-muted-foreground hover:text-foreground transition-all duration-200 shadow-sm"
                                        >
                                          {copiedStatus[`${stepId}-code`] ? (
                                            <>
                                              <Check className="w-3.5 h-3.5 text-green-500" />
                                              <span className="text-green-500 font-bold">Copied!</span>
                                            </>
                                          ) : (
                                            <>
                                              <Copy className="w-3.5 h-3.5" />
                                              <span>Copy Code</span>
                                            </>
                                          )}
                                        </button>
                                      )}
                                    </div>
                                  </div>

                                  {/* Tab Contents */}
                                  <div className="p-4 bg-background">
                                    {currentTab === 'readme' && hasReadme && (
                                      <div className="prose prose-sm max-w-none dark:prose-invert">
                                        <MarkdownRenderer content={cachedContent.content} basePath={`${workspacePath}/learnings/${stepId}/SKILL.md`} maxHeight="400px" showScrollbar={true} />
                                      </div>
                                    )}
                                    {currentTab === 'code' && hasCode && (
                                      <div className="relative rounded-lg border border-border bg-slate-50 dark:bg-slate-950 overflow-hidden">
                                        <div className="max-h-[400px] overflow-auto p-4 font-mono text-xs text-slate-800 dark:text-slate-100 whitespace-pre-wrap break-all leading-relaxed">
                                          <code>{cachedContent.codeContent}</code>
                                        </div>
                                      </div>
                                    )}
                                  </div>
                                </div>
                              )
                            })()}
                          </div>
                        )}
                      </div>
                    )}
                  </div>
                )
              })}
            </div>
          )}
        </div>

      {/* Delete Confirmation Dialog */}
      <ConfirmationDialog
        isOpen={deleteConfirmStepId !== null}
        onClose={() => setDeleteConfirmStepId(null)}
        onConfirm={() => {
          if (deleteConfirmStepId) {
            handleDeleteLearning(deleteConfirmStepId)
          }
        }}
        title="Delete Learnings"
        message={
          deleteConfirmStepId
            ? (() => {
                const stepTitle = getStepTitle(plan, deleteConfirmStepId)
                return `Are you sure you want to delete all learnings for "${stepTitle}"? This will permanently delete the learnings folder at \`learnings/${deleteConfirmStepId}/\` and all its contents. The learnings will also be unlocked. This action cannot be undone.`
              })()
            : ''
        }
        confirmText="Delete Learnings"
        cancelText="Cancel"
        type="danger"
      />
    </div>
  )
}
