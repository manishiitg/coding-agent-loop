import { useEffect, useState, useCallback } from 'react'
import { X, BookOpen, Lock, Unlock, Loader2, AlertCircle, ChevronDown, ChevronRight, Code, FileText, Trash2, Search, Globe, Hash, RefreshCw, Eye, Edit2, Save, Ban, Check, Copy, GitBranch, Bot, Terminal, CheckCircle2 } from 'lucide-react'
import { agentApi } from '../../services/api'
import type { PlanningResponse, PlanStep } from '../../utils/stepConfigMatching'
import { isConditionalStep, isTodoTaskStep } from '../../utils/stepConfigMatching'
import { MarkdownRenderer } from '../ui/MarkdownRenderer'
import type { PlannerFile } from '../../services/api-types'
import ConfirmationDialog from '../ui/ConfirmationDialog'
import ModalPortal from '../ui/ModalPortal'

interface LearningsPopupProps {
  isOpen: boolean
  onClose: () => void
  workspacePath: string | null
  plan: PlanningResponse | null
}

// LearningMetadata — fields read from learnings/{stepId}/.learning_metadata.json,
// merged with step_config.json entries by the backend API. Field names mirror the
// Go LearningMetadata struct and the AgentConfigs struct (snake_case in JSON).
type LearningsAccess = 'read' | 'read-write' | 'none'

interface LearningMetadata {
  step_id?: string
  successful_runs?: number
  last_turn_count?: number
  total_iterations?: number

  // Auto-lock lifecycle (description-hash scoped)
  auto_locked_at?: string
  auto_lock_reason?: string
  auto_unlocked_at?: string
  auto_unlock_reason?: string
  last_description_hash?: string
  description_hash_runs?: number

  // Step-config fields merged in by the backend
  use_code_execution_mode?: boolean
  lock_learnings?: boolean
  learnings_access?: LearningsAccess
  learning_objective?: string

  // Global learning only: per-step contribution counts
  step_contributions?: Record<string, number>
}

function isStepLocked(metadata: LearningMetadata | null): boolean {
  return metadata?.lock_learnings === true
}

function isStepAutoLocked(metadata: LearningMetadata | null): boolean {
  return isStepLocked(metadata) && !!metadata?.auto_locked_at
}

function getSuccessfulRuns(metadata: LearningMetadata | null): number {
  if (!metadata) return 0
  return metadata.successful_runs || 0
}

// Check if learnings folder exists
// Returns true only if metadata contains actual learning data (not just step config fields)
// Step config fields (use_code_execution_mode, learning_detail_level, lock_learnings) can exist
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
    metadata.auto_locked_at !== undefined ||
    metadata.auto_lock_reason !== undefined ||
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

const getMarkdownLinkedGlobalPaths = (content: string): Set<string> => {
  const linked = new Set<string>()
  for (const match of content.matchAll(/\[[^\]]+\]\(([^)]+)\)/g)) {
    const href = match[1]?.trim()
    if (!href || href.startsWith('#') || href.startsWith('//') || /^[a-z][a-z0-9+.-]*:/i.test(href)) continue
    linked.add(normalizeGlobalSkillRelPath(href))
  }
  return linked
}

const isPatchArtifactPath = (filepath: string): boolean => {
  const normalized = normalizeGlobalSkillRelPath(filepath).toLowerCase()
  return normalized.endsWith('.orig') || normalized.endsWith('.rej')
}

// Get step title from plan
function getStepTitle(plan: PlanningResponse | null, stepId: string): string {
  if (stepId === '_global') return 'Workflow Knowledge (Global)'
  if (!plan?.steps) return stepId

  const findStep = (steps: PlanStep[], id: string): PlanStep | null => {
    for (const step of steps) {
      if (step.id === id) return step
      // Check branch steps for conditional steps
      if ('if_true_steps' in step && step.if_true_steps) {
        const found = findStep(step.if_true_steps, id)
        if (found) return found
      }
      if ('if_false_steps' in step && step.if_false_steps) {
        const found = findStep(step.if_false_steps, id)
        if (found) return found
      }
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

export default function LearningsPopup({ isOpen, onClose, workspacePath, plan }: LearningsPopupProps) {
  const [learnings, setLearnings] = useState<Record<string, LearningMetadata | null>>({})
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  
  // Expanded items state - tracks which step IDs have their learning content expanded
  const [expandedStepIds, setExpandedStepIds] = useState<Set<string>>(new Set())
  
  // Learning content cache - stores fetched markdown content and code content for each step
  const [learningContentCache, setLearningContentCache] = useState<Record<string, { content: string; codeContent?: string; codeFileName?: string; error: string | null }>>({})
  
  // Loading states for individual items
  const [loadingStepIds, setLoadingStepIds] = useState<Set<string>>(new Set())

  const [updatingLockStepIds, setUpdatingLockStepIds] = useState<Set<string>>(new Set())
  
  // Delete state
  const [deletingStepIds, setDeletingStepIds] = useState<Set<string>>(new Set())
  const [deleteConfirmStepId, setDeleteConfirmStepId] = useState<string | null>(null)
  
  // Filter state - show only unlocked steps
  const [showOnlyUnlocked, setShowOnlyUnlocked] = useState(false)
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
  const [globalLoading, setGlobalLoading] = useState(false)
  const [globalError, setGlobalError] = useState<string | null>(null)
  const [globalExpanded, setGlobalExpanded] = useState(true)
  const [expandedFilePaths, setExpandedFilePaths] = useState<Set<string>>(new Set())
  const [fileContentCache, setFileContentCache] = useState<Record<string, string>>({})

  // Per-step inline editors for the new access/objective controls.
  const [editingAccessStepId, setEditingAccessStepId] = useState<string | null>(null)
  const [editingObjectiveStepId, setEditingObjectiveStepId] = useState<string | null>(null)
  const [objectiveDraft, setObjectiveDraft] = useState<string>('')
  const [savingConfigStepIds, setSavingConfigStepIds] = useState<Set<string>>(new Set())

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

  // Fetch learnings when popup opens (API now includes step config data merged in)
  useEffect(() => {
    if (!isOpen || !workspacePath) return

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
        console.error('[LearningsPopup] Error fetching learnings:', err)
        setError('Failed to load learnings: ' + (err.message || 'Unknown error'))
      })
      .finally(() => {
        setIsLoading(false)
      })
  }, [isOpen, workspacePath])

  // Fetch everything under _global/ on open: SKILL.md content + the full file
  // tree (references/, scripts/, assets/, any other artifacts the learning agent
  // decided to write). Per-file content is lazy-loaded on click.
  useEffect(() => {
    if (!isOpen || !workspacePath) return
    let cancelled = false
    setGlobalLoading(true)
    setGlobalError(null)
    setGlobalSkillContent('')
    setGlobalFiles([])
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
        let skillText = ''
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
            skillText = text
            setGlobalSkillContent(text)
          }
        }

        // Every other file (excluding SKILL.md + .learning_metadata.json + anything
        // that somehow resolved outside _global/). Grouped by directory for display;
        // content fetched on demand.
        const linkedGlobalPaths = getMarkdownLinkedGlobalPaths(skillText)
        const dedupedByRelPath = new Map<string, { relPath: string; rawPath: string }>()
        for (const file of flatFiles) {
          const rawPath = file.filepath || ''
          const relPath = normalizeGlobalSkillRelPath(relFromGlobal(rawPath))

          if (!relPath || relPath === 'SKILL.md') continue
          if (relPath.endsWith('.learning_metadata.json')) continue
          if (isPatchArtifactPath(relPath)) continue
          if (relPath.endsWith('/')) continue
          // Keep linked files visible in Additional files explorer so they are always browseable.
          // if (linkedGlobalPaths.has(relPath)) continue
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
          console.error('[LearningsPopup] Error loading global skill:', err)
          setGlobalError('Failed to load global skill: ' + msg)
        }
      } finally {
        if (!cancelled) setGlobalLoading(false)
      }
    })()
    return () => { cancelled = true }
  }, [isOpen, workspacePath])

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

  // Update a step's learnings_access + learning_objective through the same
  // update_step_config endpoint. Validation (read-write requires objective) is
  // enforced server-side; we just surface errors.
  const handleUpdateStepConfig = async (
    stepId: string,
    patch: Partial<{ learnings_access: LearningsAccess; learning_objective: string; lock_learnings: boolean }>
  ) => {
    if (!workspacePath || savingConfigStepIds.has(stepId)) return
    setSavingConfigStepIds(prev => new Set(prev).add(stepId))
    try {
      const step = plan?.steps?.find(s => s.id === stepId)
      const current = step?.agent_configs || {}
      const metadata = learnings[stepId]
      // Preserve any existing fields we track locally so we don't clobber them.
      const merged: Record<string, unknown> = {
        ...current,
        learnings_access: current.learnings_access ?? metadata?.learnings_access,
        learning_objective: current.learning_objective ?? metadata?.learning_objective,
        lock_learnings: current.lock_learnings ?? metadata?.lock_learnings,
        ...patch,
      }
      await agentApi.updateStepConfig(workspacePath, stepId, merged)
      const response = await agentApi.getAllStepLearnings(workspacePath)
      if (response.success) setLearnings(parseLearningsResponse(response.learnings || {}))
      setEditingAccessStepId(null)
      setEditingObjectiveStepId(null)
      setObjectiveDraft('')
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Unknown error'
      console.error('[LearningsPopup] Error updating step config:', err)
      setError('Failed to update step config: ' + msg)
    } finally {
      setSavingConfigStepIds(prev => {
        const next = new Set(prev)
        next.delete(stepId)
        return next
      })
    }
  }

  const toggleLock = async (stepId: string, isCurrentlyLocked: boolean) => {
    if (!workspacePath || updatingLockStepIds.has(stepId)) return

    setUpdatingLockStepIds(prev => new Set(prev).add(stepId))

    try {
      const step = plan?.steps?.find(s => s.id === stepId)
      const metadata = learnings[stepId]
      const currentConfigs = step?.agent_configs || (metadata ? { lock_learnings: metadata.lock_learnings } : {})

      await agentApi.updateStepConfig(workspacePath, stepId, {
        ...currentConfigs,
        lock_learnings: !isCurrentlyLocked
      })

      // Refresh learnings
      const response = await agentApi.getAllStepLearnings(workspacePath)
      if (response.success) {
        setLearnings(parseLearningsResponse(response.learnings || {}))
      }
    } catch (err: unknown) {
      console.error('[LearningsPopup] Error toggling lock:', err)
      const errorMessage = err instanceof Error ? err.message : 'Unknown error'
      setError('Failed to update lock status: ' + errorMessage)
    } finally {
      setUpdatingLockStepIds(prev => {
        const newSet = new Set(prev)
        newSet.delete(stepId)
        return newSet
      })
    }
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

      // Unlock learnings after deletion
      const step = plan?.steps?.find(s => s.id === stepId)
      const metadata = learnings[stepId]
      const currentConfigs = step?.agent_configs || (metadata ? { lock_learnings: metadata.lock_learnings } : {})

      try {
        await agentApi.updateStepConfig(workspacePath, stepId, {
          ...currentConfigs,
          lock_learnings: false
        })
      } catch (unlockErr) {
        console.warn('[LearningsPopup] Failed to unlock learnings after deletion:', unlockErr)
        // Continue even if unlock fails - deletion was successful
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
      console.error('[LearningsPopup] Error deleting learnings:', err)
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

  // Handle Escape key to close modal
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && isOpen) {
        onClose()
      }
    }

    if (isOpen) {
      document.addEventListener('keydown', handleKeyDown)
    }

    return () => {
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [isOpen, onClose])

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
      const filesResponse = await agentApi.getPlannerFiles(learningsPath, 100, 3)
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
        let filePath = rawPath ? resolveAbsPath(rawPath) : ''
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
      if (codeFiles.length === 0) {
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

      if (codeFile) {
        const rawCodeFilePath = codeFile.filepath || codeFile.name
        let codeFilePath = rawCodeFilePath ? resolveAbsPath(rawCodeFilePath) : ''
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
      console.error('[LearningsPopup] Error fetching learning content:', err)
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
      const newSet = new Set(prev)
      if (newSet.has(stepId)) {
        newSet.delete(stepId)
      } else {
        newSet.add(stepId)
        // Fetch content if not cached
        if (!learningContentCache[stepId]) {
          fetchLearningContent(stepId)
        }
      }
      return newSet
    })
  }

  // Collect all step IDs in execution order from plan with metadata
  const getStepsInExecutionOrder = useCallback((): Array<{ stepId: string; stepNumber: number; stepType: string; branchType?: string; parentStepId?: string }> => {
    if (!plan || !plan.steps) return []

    const stepsWithMetadata: Array<{ stepId: string; stepNumber: number; stepType: string; branchType?: string; parentStepId?: string }> = []
    let stepCounter = 0

    const collectSteps = (steps: PlanStep[], branchType?: string) => {
      steps.forEach((step) => {
        if (step.id) {
          stepCounter++
          const stepType = step.type || 'regular'
          stepsWithMetadata.push({
            stepId: step.id,
            stepNumber: stepCounter,
            stepType,
            branchType
          })
        }

        // Handle conditional steps - collect branch steps
        if (isConditionalStep(step)) {
          if (step.if_true_steps && step.if_true_steps.length > 0) {
            collectSteps(step.if_true_steps, 'true')
          }
          if (step.if_false_steps && step.if_false_steps.length > 0) {
            collectSteps(step.if_false_steps, 'false')
          }
        }

        // Handle todo_task steps - collect sub-agent step IDs from predefined_routes
        if (isTodoTaskStep(step)) {
          if (step.predefined_routes) {
            step.predefined_routes.forEach((route, routeIdx) => {
              if (route.sub_agent_step && route.sub_agent_step.id) {
                stepCounter++
                stepsWithMetadata.push({
                  stepId: route.sub_agent_step.id,
                  stepNumber: stepCounter,
                  stepType: 'todo_sub_agent',
                  branchType: `todo-sub-agent-${route.route_id || routeIdx}`,
                  parentStepId: step.id // Track parent for nesting
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

  if (!isOpen) return null

  // Steps in execution order. _global is rendered separately as a featured card
  // above — no longer prepended into this list.
  const allStepsInOrder = getStepsInExecutionOrder()
  let stepsWithLearnings = allStepsInOrder.filter(step => step.stepId in learnings && step.stepId !== '_global')
  
  // Apply unlocked filter if enabled
  if (showOnlyUnlocked) {
    stepsWithLearnings = stepsWithLearnings.filter(step => {
      const metadata = learnings[step.stepId]
      return !isStepLocked(metadata) // Show only unlocked steps
    })
  }

  // Apply search filter
  if (searchTerm) {
    const lowerTerm = searchTerm.toLowerCase()
    stepsWithLearnings = stepsWithLearnings.filter(step => {
      const title = getStepTitle(plan, step.stepId).toLowerCase()
      const id = step.stepId.toLowerCase()
      return title.includes(lowerTerm) || id.includes(lowerTerm)
    })
  }

  console.log('[LEARNINGS_POPUP_DEBUG] visible', {
    workspacePath,
    allPlanStepIds: allStepsInOrder.map(step => step.stepId),
    fetchedLearningStepIds: Object.keys(learnings),
    visibleLearningStepIds: stepsWithLearnings.map(step => step.stepId),
    showOnlyUnlocked,
    searchTerm,
  })

  const handleExpandAll = () => {
    const newExpanded = new Set<string>()
    stepsWithLearnings.forEach(step => {
      newExpanded.add(step.stepId)
      // Trigger fetch if not cached
      if (!learningContentCache[step.stepId]) {
        fetchLearningContent(step.stepId)
      }
    })
    setExpandedStepIds(newExpanded)
  }

  const handleCollapseAll = () => {
    setExpandedStepIds(new Set())
  }

  return (
    <ModalPortal>
    <div className="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-center justify-center z-[9999] p-2 sm:p-4">
      <div className="bg-background text-foreground border border-border rounded-lg shadow-2xl w-full max-w-6xl xl:max-w-7xl h-[calc(100dvh-1rem)] sm:h-[92vh] flex flex-col">
        {/* Header — title + close only. Step search / filter / expand controls
            moved to sit above the step list so they're visually adjacent to what
            they operate on (the per-step section, not the global skill). */}
        <div className="flex items-start justify-between gap-3 border-b border-border flex-shrink-0 p-3 sm:p-4">
          <div className="flex min-w-0 items-center gap-2">
            <BookOpen className="w-5 h-5 text-primary" />
            <h2 className="truncate text-lg font-semibold">Workflow Learnings</h2>
          </div>
          <button
            onClick={onClose}
            className="p-1 rounded-md hover:bg-muted transition-colors"
            title="Close (Esc)"
          >
            <X className="w-5 h-5" />
          </button>
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
                      <h3 className="font-medium text-sm">Global Workflow Skill</h3>
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

          {/* Step toolbar — search + expand/collapse/unlocked controls.
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
              <div className="flex items-center gap-1.5">
                <button
                  onClick={handleExpandAll}
                  className="px-2.5 py-1.5 text-xs font-medium bg-muted hover:bg-muted/80 rounded-md transition-colors whitespace-nowrap"
                >
                  Expand All
                </button>
                <button
                  onClick={handleCollapseAll}
                  className="px-2.5 py-1.5 text-xs font-medium bg-muted hover:bg-muted/80 rounded-md transition-colors whitespace-nowrap"
                >
                  Collapse All
                </button>
                <button
                  onClick={() => setShowOnlyUnlocked(!showOnlyUnlocked)}
                  className={`flex items-center gap-1.5 px-2.5 py-1.5 rounded-md text-xs font-medium transition-colors whitespace-nowrap ${
                    showOnlyUnlocked
                      ? 'bg-yellow-100 hover:bg-yellow-200 dark:bg-yellow-900/30 dark:hover:bg-yellow-900/50 text-yellow-700 dark:text-yellow-400'
                      : 'bg-muted hover:bg-muted/80 text-foreground'
                  }`}
                  title={showOnlyUnlocked ? 'Show all steps' : 'Show only unlocked steps'}
                >
                  <Unlock className="w-3.5 h-3.5" />
                  <span>Unlocked Only</span>
                </button>
              </div>
            </div>
          )}

          {/* Per-step list — secondary, metadata + main.py + inline controls. */}
          {!isLoading && !error && stepsWithLearnings.length === 0 && (
            <div className="text-center py-8 text-muted-foreground flex flex-col items-center gap-2">
              <BookOpen className="w-10 h-10 opacity-20" />
              <p>No per-step learning metadata yet</p>
              {searchTerm && <p className="text-sm">Try adjusting your search query</p>}
              {showOnlyUnlocked && <p className="text-sm">Try disabling the "Unlocked Only" filter</p>}
            </div>
          )}

          {!isLoading && !error && stepsWithLearnings.length > 0 && (
            <div className="space-y-3">
              {stepsWithLearnings.map(({ stepId, stepNumber, stepType, branchType }) => {
                const metadata = learnings[stepId]
                // Lock state comes only from step_config.json (merged by the backend
                // into metadata.lock_learnings for this API response). Metadata is
                // used only to explain whether a current lock was auto or manual.
                const isLocked = isStepLocked(metadata)
                const isAutoLocked = isStepAutoLocked(metadata)
                const isManuallyLocked = isLocked && !isAutoLocked
                const wasRecentlyAutoUnlocked = !!metadata?.auto_unlocked_at &&
                  (!metadata.auto_locked_at || (metadata.auto_unlocked_at > metadata.auto_locked_at))

                // Hash-scoped run counter drives auto-lock. Falls back to legacy
                // successful_runs while .learning_metadata.json hasn't been rewritten.
                const hashRuns = metadata?.description_hash_runs ?? 0
                const successfulRuns = getSuccessfulRuns(metadata)
                const progressRuns = hashRuns > 0 ? hashRuns : successfulRuns
                const progress = (progressRuns / 3) * 100
                const access = effectiveAccess(metadata || null)
                const accessExplicit = metadata?.learnings_access === 'read' ||
                  metadata?.learnings_access === 'read-write' ||
                  metadata?.learnings_access === 'none'
                const objective = (metadata?.learning_objective || '').trim()
                const isSavingConfig = savingConfigStepIds.has(stepId)
                const stepTitle = getStepTitle(plan, stepId)

                const isExpanded = expandedStepIds.has(stepId)
                const isLoadingContent = loadingStepIds.has(stepId)
                const cachedContent = learningContentCache[stepId]
                const isUpdatingLock = updatingLockStepIds.has(stepId)

                // Determine step type label and badge color
                const getStepTypeLabel = () => {
                  if (stepType === 'global') return 'Global'
                  if (branchType === 'true') return 'If True'
                  if (branchType === 'false') return 'If False'
                  // Use same "Sub-Agent" label for both orchestration and todo_task sub-agents
                  if (branchType?.startsWith('todo-sub-agent') || stepType === 'todo_sub_agent') return 'Sub-Agent'
                  if (branchType?.startsWith('sub-agent') || stepType === 'sub_agent') return 'Sub-Agent'
                  if (stepType === 'decision_inner') return 'Decision'
                  return stepType.charAt(0).toUpperCase() + stepType.slice(1)
                }

                const getStepTypeBadgeColor = () => {
                  if (stepType === 'global') return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400'
                  if (branchType === 'true') return 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400'
                  if (branchType === 'false') return 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400'
                  // Use same orange color for both orchestration and todo_task sub-agents
                  if (branchType?.startsWith('todo-sub-agent') || stepType === 'todo_sub_agent') return 'bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400'
                  if (branchType?.startsWith('sub-agent') || stepType === 'sub_agent') return 'bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400'
                  if (stepType === 'decision_inner') return 'bg-indigo-100 text-indigo-700 dark:bg-indigo-900/30 dark:text-indigo-400'
                  return 'bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300'
                }

                // Check if this is a sub-agent (should be indented)
                const isSubAgent = stepType === 'sub_agent' || stepType === 'todo_sub_agent'

                // Determine premium icon to represent step type
                const getStepIcon = () => {
                  if (stepType === 'global') return <Globe className="w-3.5 h-3.5 text-emerald-500" />
                  if (isSubAgent) return <Bot className="w-3.5 h-3.5 text-orange-500" />
                  if (stepType === 'decision_inner') return <GitBranch className="w-3.5 h-3.5 text-indigo-500" />
                  return <Terminal className="w-3.5 h-3.5 text-sky-500" />
                }

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
                          <div className="flex items-center gap-2.5 mb-2.5 flex-wrap sm:flex-nowrap">
                            <button
                              onClick={(e) => {
                                e.stopPropagation()
                                toggleExpand(stepId)
                              }}
                              className="p-1 hover:bg-muted rounded-md transition-colors shrink-0 flex items-center justify-center"
                              title={isExpanded ? "Collapse" : "Expand"}
                            >
                              {isExpanded ? (
                                <ChevronDown className="w-4 h-4 text-muted-foreground" />
                              ) : (
                                <ChevronRight className="w-4 h-4 text-muted-foreground" />
                              )}
                            </button>
                            <span className="text-[10px] font-mono font-bold text-primary bg-primary/10 border border-primary/20 px-1.5 py-0.5 rounded shrink-0">
                              #{stepNumber}
                            </span>
                            <span className={`text-[10px] px-2 py-0.5 rounded-full font-medium shrink-0 flex items-center gap-1 ${getStepTypeBadgeColor()}`}>
                              {getStepIcon()}
                              {getStepTypeLabel()}
                            </span>
                            <h3 className="font-semibold text-sm truncate text-foreground hover:text-primary transition-colors flex-1" title={stepTitle}>
                              {stepTitle}
                            </h3>
                            <span className="text-[10px] text-muted-foreground font-mono truncate shrink-0 max-w-[120px]" title={stepId}>
                              {stepId}
                            </span>
                          </div>

                          <div className="flex flex-col gap-2 ml-7">
                            {/* Single-line metadata: access, lock status, lock button, auto-unlock badge, turns, iterations. */}
                            <div className="flex items-center gap-2.5 flex-wrap text-xs">
                              
                              {/* Custom Segmented Control for Learnings Access */}
                              <div className="flex items-center gap-1 bg-muted/60 p-0.5 rounded-lg border border-border/60 shrink-0" onClick={(e) => e.stopPropagation()}>
                                {(['none', 'read', 'read-write'] as LearningsAccess[]).map((opt) => {
                                  const isActive = access === opt
                                  const isSelSaving = isSavingConfig
                                  return (
                                    <button
                                      key={opt}
                                      disabled={isSelSaving}
                                      onClick={(e) => {
                                        e.stopPropagation()
                                        if (!isActive) {
                                          handleUpdateStepConfig(stepId, { learnings_access: opt })
                                        }
                                      }}
                                      className={`px-2.5 py-1 rounded-md text-[11px] font-medium transition-all duration-200 flex items-center gap-1 ${
                                        isActive
                                          ? opt === 'read-write'
                                            ? 'bg-emerald-500 text-white shadow-sm font-semibold'
                                            : opt === 'read'
                                            ? 'bg-blue-500 text-white shadow-sm font-semibold'
                                            : 'bg-zinc-600 text-white shadow-sm dark:bg-zinc-500 font-semibold'
                                          : 'text-muted-foreground hover:text-foreground hover:bg-muted'
                                      } disabled:opacity-50 disabled:cursor-not-allowed`}
                                      title={`Set learnings_access to ${opt}`}
                                    >
                                      {opt === 'none' && <Ban className="w-3 h-3" />}
                                      {opt === 'read' && <Eye className="w-3 h-3" />}
                                      {opt === 'read-write' && <BookOpen className="w-3 h-3" />}
                                      <span>{opt}</span>
                                      {!isActive && !accessExplicit && opt === 'read' && (
                                        <span className="text-[9px] opacity-65 italic font-normal">(auto)</span>
                                      )}
                                    </button>
                                  )
                                })}
                              </div>

                              {/* Auto-unlocked indicator */}
                              {wasRecentlyAutoUnlocked && (
                                <div
                                  className="flex items-center gap-1 text-xs px-2.5 py-1 rounded-lg bg-blue-500/10 text-blue-600 dark:text-blue-400 border border-blue-500/20 shadow-sm"
                                  title={metadata?.auto_unlock_reason || 'Description changed — previous auto-lock invalidated'}
                                >
                                  <RefreshCw className="w-3.5 h-3.5" />
                                  <span>Auto-unlocked (description changed)</span>
                                </div>
                              )}

                              {metadata && (
                                <div className="flex items-center gap-2 bg-muted/30 px-2 py-0.5 rounded-lg border border-border/40">
                                  <div className="flex items-center gap-1">
                                    {isLocked ? (
                                      <>
                                        <Lock className="w-3.5 h-3.5 text-green-500" />
                                        <span className="text-green-600 dark:text-green-400 font-semibold text-xs">
                                          {isAutoLocked ? 'Locked (Auto)' :
                                           isManuallyLocked ? 'Locked (Manual)' :
                                           'Locked'}
                                        </span>
                                      </>
                                    ) : (
                                      <>
                                        <Unlock className="w-3.5 h-3.5 text-amber-500" />
                                        <span className="text-amber-600 dark:text-amber-400 font-semibold text-xs">Unlocked</span>
                                      </>
                                    )}
                                  </div>
                                  <div className="w-px h-3 bg-border/80 mx-1" />
                                  <button
                                    onClick={(e) => {
                                      e.stopPropagation()
                                      toggleLock(stepId, isLocked)
                                    }}
                                    disabled={isUpdatingLock}
                                    className={`flex items-center gap-1 px-2 py-0.5 rounded-md text-xs font-semibold transition-colors disabled:opacity-50 disabled:cursor-not-allowed ${
                                      isLocked
                                        ? 'bg-amber-50 hover:bg-amber-100 dark:bg-amber-950/30 dark:hover:bg-amber-900/40 text-amber-700 dark:text-amber-400'
                                        : 'bg-muted hover:bg-muted/80 text-muted-foreground hover:text-foreground'
                                    }`}
                                    title={isLocked ? "Unlock learnings" : "Lock learnings manually"}
                                  >
                                    {isUpdatingLock ? (
                                      <>
                                        <Loader2 className="w-3 h-3 animate-spin" />
                                        <span>Updating...</span>
                                      </>
                                    ) : isLocked ? (
                                      <>
                                        <Unlock className="w-3 h-3" />
                                        <span>Unlock</span>
                                      </>
                                    ) : (
                                      <>
                                        <Lock className="w-3 h-3" />
                                        <span>Lock</span>
                                      </>
                                    )}
                                  </button>
                                </div>
                              )}

                              {/* Turns + Iter badges sharing the same row as access/lock. */}
                              {metadata && metadata.last_turn_count !== undefined && metadata.last_turn_count > 0 && (
                                <span className="text-[10px] text-muted-foreground bg-muted/40 px-2 py-1 rounded-md border border-border/30">
                                  Turns: <span className="font-semibold text-foreground">{metadata.last_turn_count}</span>
                                </span>
                              )}
                              {metadata && metadata.total_iterations !== undefined && (
                                <span className="text-[10px] text-muted-foreground bg-muted/40 px-2 py-1 rounded-md border border-border/30 ml-auto flex items-center gap-1">
                                  Iter: <span className="font-mono font-semibold text-foreground">{metadata.total_iterations}</span>
                                  {metadata.auto_lock_reason && (
                                    <span className="text-amber-600 dark:text-amber-500 ml-1.5 truncate max-w-[120px] inline-block align-bottom font-medium" title={metadata.auto_lock_reason}>
                                      · {metadata.auto_lock_reason.replace('threshold_reached_', '').replace(/_/g, ' ').slice(0, 30)}
                                    </span>
                                  )}
                                </span>
                              )}
                            </div>

                            {/* Milestone Node Track */}
                            {metadata && (
                              <div className="mt-1 bg-muted/25 border border-border/40 rounded-xl p-2.5 flex flex-wrap sm:flex-nowrap items-center justify-between gap-4">
                                <div className="flex items-center gap-2 flex-wrap">
                                  <span className="text-[11px] font-semibold text-muted-foreground flex items-center gap-1 shrink-0">
                                    <Hash className="w-3 h-3" /> Runs on current description:
                                  </span>
                                  {/* 3 milestone circles with connecting line */}
                                  <div className="flex items-center gap-1.5 shrink-0">
                                    {[1, 2, 3].map((node) => {
                                      const isDone = progressRuns >= node
                                      const isCurrent = progressRuns === node - 1
                                      return (
                                        <div key={node} className="flex items-center">
                                          <div
                                            className={`w-5 h-5 rounded-full flex items-center justify-center text-[10px] font-bold transition-all duration-300 ${
                                              isDone
                                                ? 'bg-emerald-500 text-white shadow-sm shadow-emerald-500/10'
                                                : isCurrent
                                                ? 'border-2 border-amber-500 text-amber-600 dark:text-amber-400 font-extrabold bg-amber-50 dark:bg-amber-950/20 animate-pulse'
                                                : 'border border-border bg-muted/30 text-muted-foreground'
                                            }`}
                                            title={isDone ? `Run ${node} completed` : `Run ${node} pending`}
                                          >
                                            {node}
                                          </div>
                                          {node < 3 && (
                                            <div
                                              className={`h-0.5 w-4 transition-all duration-300 ${
                                                progressRuns >= node ? 'bg-emerald-500' : 'bg-border'
                                              }`}
                                            />
                                          )}
                                        </div>
                                      )
                                    })}
                                  </div>
                                </div>

                                <div className="flex items-center gap-2 text-[11px] ml-auto">
                                  {metadata.last_description_hash && (
                                    <span className="font-mono text-[9px] bg-muted/60 px-2 py-0.5 rounded text-muted-foreground border border-border/40 shrink-0" title={`Description hash: ${metadata.last_description_hash}`}>
                                      hash: {metadata.last_description_hash.slice(0, 8)}
                                    </span>
                                  )}
                                  {isLocked ? (
                                    <span className="flex items-center gap-1 bg-green-500/10 text-green-600 dark:text-green-400 px-2 py-0.5 rounded border border-green-500/20 font-semibold shrink-0">
                                      <Lock className="w-3 h-3" /> Fully Locked
                                    </span>
                                  ) : (
                                    <span className="flex items-center gap-1 bg-amber-500/10 text-amber-600 dark:text-amber-400 px-2 py-0.5 rounded border border-amber-500/20 font-semibold shrink-0 animate-pulse">
                                      <Unlock className="w-3 h-3" /> Unlocked
                                    </span>
                                  )}
                                </div>
                              </div>
                            )}

                            {!metadata && (
                              <div className="text-xs text-muted-foreground italic mt-0.5">
                                No learning metadata yet
                              </div>
                            )}

                          </div>
                        </div>

                        {/* Delete Button */}
                        {hasLearningsFolder(metadata, cachedContent) && (
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
                        {/* learning_objective inline editor */}
                        <div className="mb-5 p-4 bg-muted/10 dark:bg-card border border-border/80 rounded-xl shadow-sm">
                          <div className="flex items-center justify-between mb-2.5">
                            <div className="text-xs font-bold text-muted-foreground uppercase tracking-wider flex items-center gap-1.5">
                              <BookOpen className="w-3.5 h-3.5 text-primary" />
                              Learning Objective
                            </div>
                            {editingObjectiveStepId !== stepId ? (
                              <button
                                onClick={(e) => {
                                  e.stopPropagation()
                                  setObjectiveDraft(objective)
                                  setEditingObjectiveStepId(stepId)
                                }}
                                className="flex items-center gap-1 text-xs px-2.5 py-1 rounded-md hover:bg-muted border border-border/60 transition-colors text-muted-foreground hover:text-foreground"
                                title="Edit objective"
                              >
                                <Edit2 className="w-3 h-3" />
                                Edit
                              </button>
                            ) : (
                              <div className="flex items-center gap-1.5" onClick={(e) => e.stopPropagation()}>
                                <button
                                  onClick={(e) => {
                                    e.stopPropagation()
                                    handleUpdateStepConfig(stepId, { learning_objective: objectiveDraft.trim() })
                                  }}
                                  disabled={isSavingConfig}
                                  className="flex items-center gap-1 text-xs px-3 py-1 rounded-md bg-primary text-primary-foreground hover:opacity-90 transition-opacity disabled:opacity-50 font-semibold shadow-sm"
                                >
                                  {isSavingConfig ? <Loader2 className="w-3 h-3 animate-spin" /> : <Save className="w-3 h-3" />}
                                  Save
                                </button>
                                <button
                                  onClick={(e) => {
                                    e.stopPropagation()
                                    setEditingObjectiveStepId(null)
                                    setObjectiveDraft('')
                                  }}
                                  className="text-xs px-2.5 py-1 rounded-md hover:bg-muted transition-colors text-muted-foreground"
                                >
                                  Cancel
                                </button>
                              </div>
                            )}
                          </div>
                          {editingObjectiveStepId === stepId ? (
                            <textarea
                              value={objectiveDraft}
                              onChange={(e) => setObjectiveDraft(e.target.value)}
                              onClick={(e) => e.stopPropagation()}
                              placeholder={'Describe what SKILL.md should capture from this step. Required when learnings_access="read-write".'}
                              className="w-full min-h-[90px] p-3 text-sm bg-background border border-input rounded-lg focus:outline-none focus:ring-2 focus:ring-primary/20 focus:border-primary resize-y font-mono"
                            />
                          ) : objective ? (
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
    </ModalPortal>
  )
}
