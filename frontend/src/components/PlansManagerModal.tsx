import React, { useState, useEffect, useCallback } from 'react'
import { X, GitBranch, Trash2, Clock } from 'lucide-react'
import { agentApi } from '../services/api'
import type { PlannerFile, PlannerFilesResponse } from '../services/api-types'

interface PlanInfo {
  name: string
  folder: string
  hasPlanMd: boolean
  lastModified?: string  // folder's own last_modified
  lastUsed?: string      // .last_used child's last_modified
}

interface Props {
  isOpen: boolean
  onClose: () => void
  onSelectPlan: (planFolder: string) => void
}

function formatDate(iso?: string): string {
  if (!iso) return ''
  try {
    return new Date(iso).toLocaleString(undefined, {
      year: 'numeric', month: 'short', day: 'numeric',
      hour: '2-digit', minute: '2-digit',
    })
  } catch {
    return iso
  }
}

function parsePlans(files: PlannerFile[]): PlanInfo[] {
  const items: PlanInfo[] = []
  for (const entry of files) {
    if (entry.type !== 'folder') continue
    if (!/^Chats\/[^/]+$/.test(entry.filepath)) continue

    const planName = entry.filepath.split('/').pop() || entry.filepath
    let lastUsed: string | undefined
    let hasPlanMd = false
    let hasPlanTracking = false

    for (const child of (entry.children ?? [])) {
      const childName = child.filepath.split('/').pop()
      if (childName === 'plan.md') hasPlanMd = true
      if (childName === 'plan_tracking.md') hasPlanTracking = true
      if (childName === '.last_used') lastUsed = child.last_modified
    }

    // Chats/ is shared with regular chat outputs, so only show folders that
    // look like multi-agent plan folders.
    if (!hasPlanMd && !hasPlanTracking && !lastUsed) {
      continue
    }

    items.push({
      name: planName,
      folder: entry.filepath,
      hasPlanMd,
      lastModified: entry.last_modified,
      lastUsed,
    })
  }

  items.sort((a, b) => {
    const au = a.lastUsed ? new Date(a.lastUsed).getTime() : 0
    const bu = b.lastUsed ? new Date(b.lastUsed).getTime() : 0
    if (bu !== au) return bu - au
    const am = a.lastModified ? new Date(a.lastModified).getTime() : 0
    const bm = b.lastModified ? new Date(b.lastModified).getTime() : 0
    return bm - am
  })

  return items
}

export default function PlansManagerModal({ isOpen, onClose, onSelectPlan }: Props) {
  const [plans, setPlans] = useState<PlanInfo[]>([])
  const [loading, setLoading] = useState(false)
  const [deletingFolder, setDeletingFolder] = useState<string | null>(null)
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null)

  const loadPlans = useCallback(async () => {
    setLoading(true)
    try {
      const chatsResponse = await agentApi.getPlannerFiles('Chats', -1, 2).catch(() => null)

      console.log('[PlansManager] Chats API response:', chatsResponse)

      const merged: PlannerFile[] = [
        ...(chatsResponse?.success && chatsResponse.data ? chatsResponse.data : []),
      ]

      if (merged.length > 0) {
        setPlans(parsePlans(merged))
      } else {
        setPlans([])
      }
    } catch {
      setPlans([])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (isOpen) loadPlans()
  }, [isOpen, loadPlans])

  const handleDelete = useCallback(async (folder: string) => {
    setDeletingFolder(folder)
    try {
      await agentApi.deletePlannerFolder(folder)
      setPlans(prev => prev.filter(p => p.folder !== folder))
    } finally {
      setDeletingFolder(null)
      setConfirmDelete(null)
    }
  }, [])

  if (!isOpen) return null

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm"
      onClick={onClose}
    >
      <div
        className="bg-gray-900 rounded-xl shadow-2xl border border-gray-700 w-[520px] max-w-[95vw] max-h-[80vh] flex flex-col"
        onClick={e => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-gray-700 flex-shrink-0">
          <div className="flex items-center gap-2">
            <GitBranch className="w-5 h-5 text-blue-400" />
            <h3 className="text-base font-semibold text-white">Plan Folders</h3>
          </div>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-200 transition-colors">
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto px-5 py-4 space-y-2">
          {loading && (
            <p className="text-sm text-gray-400 text-center py-8">Loading plans…</p>
          )}
          {!loading && plans.length === 0 && (
            <p className="text-sm text-gray-400 text-center py-8">
              No folders yet. Start a multi-agent chat to create one.
            </p>
          )}
          {!loading && plans.map(plan => (
            <div
              key={plan.folder}
              role="button"
              tabIndex={0}
              onClick={() => { onSelectPlan(plan.folder); onClose() }}
              onKeyDown={e => e.key === 'Enter' && (onSelectPlan(plan.folder), onClose())}
              className="flex items-center gap-3 p-3 rounded-lg border border-gray-700 bg-gray-800/50 hover:bg-gray-800 hover:border-blue-600 cursor-pointer transition-colors"
            >
              <GitBranch className="w-4 h-4 text-blue-400 flex-shrink-0" />
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2">
                  <span className="text-sm font-mono font-medium text-gray-100 truncate">{plan.name}</span>
                  {plan.hasPlanMd
                    ? <span className="text-xs px-1.5 py-0.5 rounded bg-blue-900/50 text-blue-300 flex-shrink-0">plan</span>
                    : <span className="text-xs px-1.5 py-0.5 rounded bg-gray-700/60 text-gray-400 flex-shrink-0">outputs</span>
                  }
                </div>
                <div className="flex items-center gap-3 mt-0.5 flex-wrap">
                  {plan.lastModified && (
                    <span className="flex items-center gap-1 text-xs text-gray-500">
                      <Clock className="w-3 h-3" />
                      {formatDate(plan.lastModified)}
                    </span>
                  )}
                  {plan.lastUsed && (
                    <span className="text-xs text-gray-600">
                      last used {formatDate(plan.lastUsed)}
                    </span>
                  )}
                </div>
              </div>
              {/* Delete button */}
              {confirmDelete === plan.folder ? (
                <div className="flex items-center gap-1 flex-shrink-0" onClick={e => e.stopPropagation()}>
                  <button
                    type="button"
                    onClick={() => handleDelete(plan.folder)}
                    disabled={deletingFolder === plan.folder}
                    className="px-2 py-1 text-xs rounded bg-red-700 hover:bg-red-600 text-white transition-colors disabled:opacity-50"
                  >
                    {deletingFolder === plan.folder ? '…' : 'Delete'}
                  </button>
                  <button
                    type="button"
                    onClick={() => setConfirmDelete(null)}
                    className="px-2 py-1 text-xs rounded bg-gray-700 hover:bg-gray-600 text-gray-200 transition-colors"
                  >
                    Cancel
                  </button>
                </div>
              ) : (
                <button
                  type="button"
                  onClick={e => { e.stopPropagation(); setConfirmDelete(plan.folder) }}
                  className="p-1.5 rounded text-gray-600 hover:text-red-400 hover:bg-red-900/20 transition-colors flex-shrink-0"
                  title="Delete plan"
                >
                  <Trash2 className="w-3.5 h-3.5" />
                </button>
              )}
            </div>
          ))}
        </div>

        {/* Footer */}
        <div className="px-5 py-3 border-t border-gray-700 flex-shrink-0">
          <p className="text-xs text-gray-500">
            Click a folder to continue it. Delete removes the folder permanently.
          </p>
        </div>
      </div>
    </div>
  )
}
