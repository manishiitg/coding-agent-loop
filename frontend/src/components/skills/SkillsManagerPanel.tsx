import { useEffect, useCallback, useMemo, useState } from 'react'
import { WandSparkles, Loader2, AlertCircle, Plus, RefreshCw, Search, X } from 'lucide-react'
import { skillsApi } from '../../api/skills'
import type { Skill } from '../../types/skills'
import SkillRow from './SkillRow'
import SkillImportDialog from './SkillImportDialog'
import { READ_ONLY_TITLE, useCanWriteWorkflow } from '../../hooks/useCanWriteWorkflow'

interface SkillsManagerPanelProps {
  /** The embedded workflow-panel context: tighter spacing for a narrow side
   * panel instead of the modal's roomier padding. */
  compact?: boolean
  // When provided (the workflow-panel embedding), the panel also shows the
  // selected-skills chips at top and gives each row an add/remove-from-workflow
  // toggle -- the point of surfacing skill management here in the first
  // place, mirroring ConnectorsBrowser.
  selectedSkills?: string[]
  onToggleSkill?: (folderName: string) => void
  workspacePath?: string | null
  /** Product surfaces use the same manager with project terminology. */
  selectionLabel?: string
  emptySelectionText?: string
  selectionScopeLabel?: string
}

export default function SkillsManagerPanel({
  compact = false,
  selectedSkills,
  onToggleSkill,
  workspacePath,
  selectionLabel = 'Skills for this workflow',
  emptySelectionText = 'No skills yet — pick one below.',
  selectionScopeLabel = 'workflow',
}: SkillsManagerPanelProps) {
  const [skills, setSkills] = useState<Skill[]>([])
  const [query, setQuery] = useState('')
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [showImportDialog, setShowImportDialog] = useState(false)
  // Import and delete write the shared skills library immediately; the
  // add/remove toggle changes this workflow's selection. All disable for
  // read-only users.
  const readOnly = !useCanWriteWorkflow(workspacePath)

  const loadSkills = useCallback(async () => {
    setIsLoading(true)
    setError(null)
    try {
      const response = await skillsApi.listSkills()
      // Deduplicate by file_path to prevent React duplicate key crashes
      const raw = response.skills || []
      const seen = new Set<string>()
      const unique = raw.filter(s => {
        const key = s.file_path || s.folder_name
        if (seen.has(key)) return false
        seen.add(key)
        return true
      })
      setSkills(unique)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load skills')
      setSkills([])
    } finally {
      setIsLoading(false)
    }
  }, [])

  useEffect(() => {
    loadSkills()
  }, [loadSkills])

  const handleDelete = async (folderName: string) => {
    if (readOnly) return
    if (!confirm(`Are you sure you want to delete the skill "${folderName}"?`)) {
      return
    }

    try {
      await skillsApi.deleteSkill(folderName)
      loadSkills()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete skill')
    }
  }

  const handleImportSuccess = () => {
    setShowImportDialog(false)
    loadSkills()
  }

  const visibleSkills = useMemo(() => {
    const q = query.trim().toLowerCase()
    const base = q
      ? skills.filter(skill =>
          skill.frontmatter.name.toLowerCase().includes(q) ||
          skill.folder_name.toLowerCase().includes(q) ||
          (skill.frontmatter.description || '').toLowerCase().includes(q)
        )
      : skills
    if (!selectedSkills) return base
    // Selected skills bubble to the top so they're easy to find in a long list.
    return [...base].sort((a, b) => {
      const aSelected = selectedSkills.includes(a.folder_name)
      const bSelected = selectedSkills.includes(b.folder_name)
      if (aSelected && !bSelected) return -1
      if (!aSelected && bSelected) return 1
      return a.frontmatter.name.localeCompare(b.frontmatter.name)
    })
  }, [skills, query, selectedSkills])

  return (
    <div className={`flex min-h-0 flex-1 flex-col ${compact ? 'gap-2' : 'gap-3'}`}>
      {onToggleSkill && (
        <div className="shrink-0">
          <div className="mb-1.5 text-sm font-medium text-muted-foreground">{selectionLabel}</div>
          {(selectedSkills || []).length === 0 ? (
            <p className="text-xs text-gray-500 dark:text-gray-400">{emptySelectionText}</p>
          ) : (
            <div className="flex flex-wrap gap-1.5">
              {(selectedSkills || []).map(folderName => {
                // Fall back to the raw folder name when the skill isn't in the
                // currently loaded list (e.g. renamed/removed elsewhere) --
                // a selected skill must never be invisible.
                const skill = skills.find(s => s.folder_name === folderName)
                const label = skill?.frontmatter.name || folderName
                return (
                  <span
                    key={folderName}
                    className="inline-flex items-center gap-1.5 rounded-full border border-primary/30 bg-primary/10 py-1 pl-2.5 pr-1.5 text-xs font-medium text-primary"
                  >
                    <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-muted-foreground/40" />
                    {label}
                    <button
                      type="button"
                      onClick={() => onToggleSkill(folderName)}
                      disabled={readOnly}
                      title={readOnly ? READ_ONLY_TITLE : undefined}
                      className="rounded-full p-0.5 text-primary/70 transition-colors hover:bg-red-500/15 hover:text-red-500 disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-transparent disabled:hover:text-primary/70"
                      aria-label={`Remove ${label} from this ${selectionScopeLabel}`}
                    >
                      <X className="h-3 w-3" />
                    </button>
                  </span>
                )
              })}
            </div>
          )}
        </div>
      )}

      <div className="flex shrink-0 items-center justify-between">
        <span className="text-sm font-medium text-gray-700 dark:text-gray-300">
          {query.trim()
            ? `${visibleSkills.length} of ${skills.length}`
            : `${skills.length} ${skills.length === 1 ? 'Skill' : 'Skills'}`}
        </span>
        <div className="flex items-center gap-1">
          <button
            onClick={loadSkills}
            className="p-1.5 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 transition-colors"
            title="Refresh skills"
          >
            <RefreshCw className={`w-4 h-4 ${isLoading ? 'animate-spin' : ''}`} />
          </button>
          <button
            onClick={() => setShowImportDialog(true)}
            disabled={readOnly}
            title={readOnly ? READ_ONLY_TITLE : undefined}
            className="px-2.5 py-1 text-xs font-medium text-primary bg-primary/10 hover:bg-primary/20 rounded-md transition-colors flex items-center gap-1.5 disabled:cursor-not-allowed disabled:opacity-50"
          >
            <Plus className="w-3.5 h-3.5" />
            Import
          </button>
        </div>
      </div>

      {skills.length > 0 && (
        <div className="relative shrink-0">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-400" />
          <input
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search skills"
            aria-label="Search skills"
            className="w-full rounded-lg border border-gray-300 bg-white py-2 pl-10 pr-3 text-sm text-gray-900 placeholder-gray-400 transition-colors focus:border-primary focus:outline-none focus:ring-1 focus:ring-primary dark:border-gray-700 dark:bg-gray-800 dark:text-gray-100"
          />
        </div>
      )}

      {error && (
        <div className="flex shrink-0 items-center gap-2 text-sm text-red-500 dark:text-red-400">
          <AlertCircle className="w-4 h-4" />
          <span>Error: {error}</span>
        </div>
      )}

      <div className="min-h-0 flex-1 overflow-y-auto">
        {isLoading && skills.length === 0 ? (
          <div className="flex items-center gap-2 py-4 text-sm text-gray-500 dark:text-gray-400">
            <Loader2 className="w-4 h-4 animate-spin" />
            <span>Loading skills...</span>
          </div>
        ) : (skills?.length || 0) === 0 ? (
          <div className={`flex flex-col items-center justify-center text-gray-500 dark:text-gray-400 ${compact ? 'py-6' : 'h-full'}`}>
            <WandSparkles className={`${compact ? 'w-8 h-8 mb-2' : 'w-12 h-12 mb-4'} opacity-50`} />
            <p className={`${compact ? 'text-sm' : 'text-lg'} font-medium mb-2`}>No skills installed</p>
            <p className="text-sm text-center mb-4">
              Import skills to extend your agent's capabilities
            </p>
            <button
              onClick={() => setShowImportDialog(true)}
              disabled={readOnly}
              title={readOnly ? READ_ONLY_TITLE : undefined}
              className="px-4 py-2 text-sm font-medium text-primary-foreground bg-primary hover:bg-primary/90 rounded-md transition-colors flex items-center gap-2 disabled:cursor-not-allowed disabled:opacity-50"
            >
              <Plus className="w-4 h-4" />
              Import
            </button>
          </div>
        ) : visibleSkills.length === 0 ? (
          <p className="py-8 text-center text-sm text-gray-500 dark:text-gray-400">
            No skills match "{query}".
          </p>
        ) : (
          <div className="rounded-md border border-gray-200 dark:border-gray-700">
            {visibleSkills.map((skill) => (
              <SkillRow
                key={skill.file_path || skill.folder_name}
                skill={skill}
                onDelete={() => handleDelete(skill.folder_name)}
                selected={onToggleSkill ? (selectedSkills || []).includes(skill.folder_name) : undefined}
                onToggleSelect={onToggleSkill ? () => onToggleSkill(skill.folder_name) : undefined}
                readOnly={readOnly}
                selectionScopeLabel={selectionScopeLabel}
              />
            ))}
          </div>
        )}
      </div>

      {showImportDialog && (
        <SkillImportDialog
          onClose={() => setShowImportDialog(false)}
          onSuccess={handleImportSuccess}
        />
      )}
    </div>
  )
}
