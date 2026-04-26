import React, { useEffect, useState, useCallback } from 'react'
import {
  X,
  Loader2,
  Tag,
  Trash2,
  History,
  Plus,
  Package,
  AlertCircle
} from 'lucide-react'
import { agentApi } from '../../services/api'
import type { WorkflowVersionMeta } from '../../services/api-types'
import ConfirmationDialog from '../ui/ConfirmationDialog'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../ui/tooltip'

interface WorkflowVersionsPopupProps {
  isOpen: boolean
  onClose: () => void
  workspacePath: string | null
  onRefresh?: () => Promise<void>
}

// Format relative time (e.g., "2 hours ago", "3 days ago")
const formatRelativeTime = (dateStr: string): string => {
  const date = new Date(dateStr)
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffSec = Math.floor(diffMs / 1000)
  const diffMin = Math.floor(diffSec / 60)
  const diffHr = Math.floor(diffMin / 60)
  const diffDay = Math.floor(diffHr / 24)

  if (diffSec < 60) return 'just now'
  if (diffMin < 60) return `${diffMin}m ago`
  if (diffHr < 24) return `${diffHr}h ago`
  if (diffDay < 30) return `${diffDay}d ago`
  return date.toLocaleDateString()
}

const WorkflowVersionsPopup: React.FC<WorkflowVersionsPopupProps> = ({
  isOpen,
  onClose,
  workspacePath,
  onRefresh
}) => {
  const [loading, setLoading] = useState(false)
  const [versions, setVersions] = useState<WorkflowVersionMeta[]>([])
  const [error, setError] = useState<string | null>(null)

  // Publish state
  const [showPublishForm, setShowPublishForm] = useState(false)
  const [publishLabel, setPublishLabel] = useState('')
  const [isPublishing, setIsPublishing] = useState(false)

  // Revert confirmation state
  const [revertVersion, setRevertVersion] = useState<WorkflowVersionMeta | null>(null)
  const [isReverting, setIsReverting] = useState(false)

  // Delete confirmation state
  const [deleteVersion, setDeleteVersion] = useState<WorkflowVersionMeta | null>(null)
  const [isDeleting, setIsDeleting] = useState(false)

  const loadVersions = useCallback(async () => {
    if (!workspacePath) return
    setLoading(true)
    setError(null)
    try {
      const resp = await agentApi.listVersions(workspacePath)
      setVersions(resp.versions || [])
    } catch (err) {
      setError('Failed to load versions')
      console.error('Failed to load versions:', err)
    } finally {
      setLoading(false)
    }
  }, [workspacePath])

  useEffect(() => {
    if (isOpen && workspacePath) {
      loadVersions()
    }
  }, [isOpen, workspacePath, loadVersions])

  const handlePublish = async () => {
    if (!workspacePath || !publishLabel.trim()) return
    setIsPublishing(true)
    try {
      await agentApi.publishVersion(workspacePath, publishLabel.trim())
      setPublishLabel('')
      setShowPublishForm(false)
      await loadVersions()
    } catch (err) {
      setError('Failed to publish version')
      console.error('Failed to publish version:', err)
    } finally {
      setIsPublishing(false)
    }
  }

  const handleRevert = async () => {
    if (!workspacePath || !revertVersion) return
    setIsReverting(true)
    try {
      await agentApi.revertToVersion(workspacePath, revertVersion.version)
      setRevertVersion(null)
      if (onRefresh) await onRefresh()
    } catch (err) {
      setError('Failed to revert version')
      console.error('Failed to revert version:', err)
    } finally {
      setIsReverting(false)
    }
  }

  const handleDelete = async () => {
    if (!workspacePath || !deleteVersion) return
    setIsDeleting(true)
    try {
      await agentApi.deleteVersion(workspacePath, deleteVersion.version)
      setDeleteVersion(null)
      await loadVersions()
    } catch (err) {
      setError('Failed to delete version')
      console.error('Failed to delete version:', err)
    } finally {
      setIsDeleting(false)
    }
  }

  if (!isOpen) return null

  return (
    <>
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm p-2 sm:p-4">
        <div className="bg-background rounded-lg shadow-xl w-full max-w-lg max-h-[calc(100dvh-1rem)] sm:max-h-[80vh] flex flex-col border border-border relative">
          {/* Header */}
          <div className="flex items-start justify-between gap-3 px-4 py-3 border-b border-border sm:px-5 sm:py-3.5">
            <h2 className="text-base font-semibold text-foreground flex items-center gap-2">
              <Package className="w-4.5 h-4.5 text-primary" />
              Workflow Versions
            </h2>
            <div className="flex items-center gap-2">
              {!showPublishForm && (
                <button
                  onClick={() => setShowPublishForm(true)}
                  className="flex items-center gap-1.5 px-2.5 py-1.5 text-xs font-medium rounded-md bg-primary text-primary-foreground hover:bg-primary/90 transition-colors"
                >
                  <Plus className="w-3.5 h-3.5" />
                  Publish Current
                </button>
              )}
              <button
                onClick={onClose}
                className="p-1 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
              >
                <X className="w-4 h-4" />
              </button>
            </div>
          </div>

          {/* Publish form */}
          {showPublishForm && (
            <div className="px-5 py-3 border-b border-border bg-muted/30">
              <div className="flex items-center gap-2">
                <input
                  type="text"
                  value={publishLabel}
                  onChange={(e) => setPublishLabel(e.target.value)}
                  placeholder="Version label (e.g., Stable plan, Added validation)"
                  className="flex-1 px-3 py-1.5 text-sm rounded-md border border-border bg-background text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-primary"
                  autoFocus
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' && publishLabel.trim()) handlePublish()
                    if (e.key === 'Escape') setShowPublishForm(false)
                  }}
                />
                <button
                  onClick={handlePublish}
                  disabled={!publishLabel.trim() || isPublishing}
                  className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-md bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
                >
                  {isPublishing ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Tag className="w-3.5 h-3.5" />}
                  Publish
                </button>
                <button
                  onClick={() => { setShowPublishForm(false); setPublishLabel('') }}
                  className="p-1.5 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
                >
                  <X className="w-3.5 h-3.5" />
                </button>
              </div>
            </div>
          )}

          {/* Error */}
          {error && (
            <div className="px-5 py-2 bg-destructive/10 text-destructive text-xs flex items-center gap-2">
              <AlertCircle className="w-3.5 h-3.5 flex-shrink-0" />
              {error}
              <button onClick={() => setError(null)} className="ml-auto text-destructive/70 hover:text-destructive">
                <X className="w-3 h-3" />
              </button>
            </div>
          )}

          {/* Content */}
          <div className="flex-1 overflow-y-auto px-5 py-3">
            {loading ? (
              <div className="flex items-center justify-center py-12">
                <Loader2 className="w-5 h-5 animate-spin text-muted-foreground" />
              </div>
            ) : versions.length === 0 ? (
              <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
                <Package className="w-8 h-8 mb-2 opacity-50" />
                <p className="text-sm">No versions published yet</p>
                <p className="text-xs mt-1">Click "Publish Current" to create a snapshot</p>
              </div>
            ) : (
              <div className="space-y-2">
                {versions.map((v) => (
                  <div
                    key={v.version}
                    className="flex items-center gap-3 px-3 py-2.5 rounded-md border border-border hover:bg-muted/50 transition-colors"
                  >
                    {/* Version badge */}
                    <span className="flex-shrink-0 px-2 py-0.5 text-xs font-bold rounded bg-primary/10 text-primary">
                      v{v.version}
                    </span>

                    {/* Info */}
                    <div className="flex-1 min-w-0">
                      <div className="text-sm font-medium text-foreground truncate">
                        {v.label || 'Untitled'}
                      </div>
                      <div className="text-xs text-muted-foreground">
                        {formatRelativeTime(v.created_at)} &middot; {v.files_count} file{v.files_count !== 1 ? 's' : ''}
                      </div>
                    </div>

                    {/* Actions */}
                    <TooltipProvider delayDuration={150}>
                      <div className="flex items-center gap-1 flex-shrink-0">
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <button
                              onClick={() => setRevertVersion(v)}
                              className="p-1.5 rounded-md text-muted-foreground hover:text-primary hover:bg-primary/10 transition-colors"
                            >
                              <History className="w-3.5 h-3.5" />
                            </button>
                          </TooltipTrigger>
                          <TooltipContent side="bottom">
                            <p>Revert to v{v.version}</p>
                          </TooltipContent>
                        </Tooltip>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <button
                              onClick={() => setDeleteVersion(v)}
                              className="p-1.5 rounded-md text-muted-foreground hover:text-destructive hover:bg-destructive/10 transition-colors"
                            >
                              <Trash2 className="w-3.5 h-3.5" />
                            </button>
                          </TooltipTrigger>
                          <TooltipContent side="bottom">
                            <p>Delete v{v.version}</p>
                          </TooltipContent>
                        </Tooltip>
                      </div>
                    </TooltipProvider>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Revert Confirmation */}
      <ConfirmationDialog
        isOpen={revertVersion !== null}
        onClose={() => setRevertVersion(null)}
        onConfirm={handleRevert}
        title={`Revert to v${revertVersion?.version}?`}
        message={`This will overwrite your current workflow config with the snapshot from v${revertVersion?.version} (${revertVersion?.label || 'Untitled'}). Consider publishing the current state first to preserve your changes.`}
        confirmText="Revert"
        type="warning"
        isLoading={isReverting}
      />

      {/* Delete Confirmation */}
      <ConfirmationDialog
        isOpen={deleteVersion !== null}
        onClose={() => setDeleteVersion(null)}
        onConfirm={handleDelete}
        title={`Delete v${deleteVersion?.version}?`}
        message={`This will permanently delete version v${deleteVersion?.version} (${deleteVersion?.label || 'Untitled'}). This cannot be undone.`}
        confirmText="Delete"
        type="danger"
        isLoading={isDeleting}
      />
    </>
  )
}

export default WorkflowVersionsPopup
