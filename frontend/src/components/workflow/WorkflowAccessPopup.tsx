import React, { useCallback, useEffect, useState } from 'react'
import { X, Loader2, Trash2, Plus, AlertCircle, ShieldCheck } from 'lucide-react'
import { authApi, type WorkflowUserPermission } from '../../services/api'
import ModalPortal from '../ui/ModalPortal'

interface WorkflowAccessPopupProps {
  isOpen: boolean
  onClose: () => void
}

type AccessLevel = 'read' | 'write' | 'owner'

const ACCESS_LEVELS: { value: AccessLevel; label: string; hint: string }[] = [
  { value: 'read', label: 'Read', hint: 'Run mode only — no builder/optimizer.' },
  { value: 'write', label: 'Write', hint: 'Run, builder, and optimizer modes.' },
  { value: 'owner', label: 'Owner', hint: 'Write + can manage other users’ access.' },
]

const WorkflowAccessPopup: React.FC<WorkflowAccessPopupProps> = ({ isOpen, onClose }) => {
  const [permissions, setPermissions] = useState<WorkflowUserPermission[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [newUserKey, setNewUserKey] = useState('')
  const [newAccess, setNewAccess] = useState<AccessLevel>('read')
  const [submitting, setSubmitting] = useState(false)
  const [busyKey, setBusyKey] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const resp = await authApi.listWorkflowUserPermissions()
      setPermissions(resp.permissions || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (isOpen) refresh()
  }, [isOpen, refresh])

  const handleAdd = useCallback(async () => {
    const key = newUserKey.trim()
    if (!key) return
    setSubmitting(true)
    setError(null)
    try {
      await authApi.upsertWorkflowUserPermission(key, newAccess)
      setNewUserKey('')
      setNewAccess('read')
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSubmitting(false)
    }
  }, [newUserKey, newAccess, refresh])

  const handleChange = useCallback(async (userKey: string, level: AccessLevel) => {
    setBusyKey(userKey)
    setError(null)
    try {
      await authApi.upsertWorkflowUserPermission(userKey, level)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusyKey(null)
    }
  }, [refresh])

  const handleRevoke = useCallback(async (userKey: string) => {
    if (!confirm(`Revoke workflow access for ${userKey}?`)) return
    setBusyKey(userKey)
    setError(null)
    try {
      await authApi.deleteWorkflowUserPermission(userKey)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusyKey(null)
    }
  }, [refresh])

  if (!isOpen) return null

  return (
    <ModalPortal>
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
        <div
          className="bg-background border border-border rounded-lg shadow-xl w-full max-w-2xl max-h-[85vh] flex flex-col"
          onClick={(e) => e.stopPropagation()}
        >
          <div className="flex items-center justify-between p-4 border-b border-border">
            <div className="flex items-center gap-2">
              <ShieldCheck className="w-5 h-5 text-muted-foreground" />
              <h2 className="text-lg font-semibold">Workflow Access</h2>
            </div>
            <button onClick={onClose} className="p-1 rounded hover:bg-accent">
              <X className="w-4 h-4" />
            </button>
          </div>

          <div className="px-4 pt-3 pb-2 text-xs text-muted-foreground">
            Grant per-user workflow access by username, user ID, or email. Persisted to{' '}
            <code className="text-[11px] bg-muted px-1 py-0.5 rounded">config/workflow-user-permissions.json</code>.
            File grants override any env-var defaults.
          </div>

          {/* Add new */}
          <div className="px-4 pt-2 pb-3 border-b border-border">
            <div className="flex flex-col gap-2 sm:flex-row sm:items-end">
              <div className="flex-1">
                <label className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">User</label>
                <input
                  type="text"
                  value={newUserKey}
                  onChange={(e) => setNewUserKey(e.target.value)}
                  placeholder="username, user ID, or email"
                  className="w-full mt-1 px-2 py-1.5 text-sm bg-muted/40 border border-border rounded focus:outline-none focus:ring-1 focus:ring-primary"
                  onKeyDown={(e) => { if (e.key === 'Enter' && newUserKey.trim() && !submitting) handleAdd() }}
                />
              </div>
              <div className="sm:w-40">
                <label className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">Access</label>
                <select
                  value={newAccess}
                  onChange={(e) => setNewAccess(e.target.value as AccessLevel)}
                  className="w-full mt-1 px-2 py-1.5 text-sm bg-muted/40 border border-border rounded focus:outline-none focus:ring-1 focus:ring-primary"
                >
                  {ACCESS_LEVELS.map(a => (
                    <option key={a.value} value={a.value}>{a.label}</option>
                  ))}
                </select>
              </div>
              <button
                onClick={handleAdd}
                disabled={!newUserKey.trim() || submitting}
                className="inline-flex items-center gap-1 px-3 py-1.5 text-sm rounded bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {submitting ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Plus className="w-3.5 h-3.5" />}
                Add / Update
              </button>
            </div>
            <div className="mt-1 text-[11px] text-muted-foreground">
              {ACCESS_LEVELS.find(a => a.value === newAccess)?.hint}
            </div>
          </div>

          {/* List */}
          <div className="flex-1 overflow-auto px-4 py-3">
            {error && (
              <div className="mb-2 flex items-start gap-2 text-xs text-destructive bg-destructive/10 p-2 rounded">
                <AlertCircle className="w-4 h-4 mt-0.5 flex-shrink-0" />
                <span>{error}</span>
              </div>
            )}
            {loading ? (
              <div className="flex items-center justify-center py-8 text-muted-foreground">
                <Loader2 className="w-5 h-5 animate-spin" />
              </div>
            ) : permissions.length === 0 ? (
              <div className="py-8 text-center text-sm text-muted-foreground">
                No persisted grants yet. Add a user above.
              </div>
            ) : (
              <table className="w-full text-sm">
                <thead className="text-[11px] uppercase tracking-wide text-muted-foreground">
                  <tr>
                    <th className="text-left py-1 pr-2">User</th>
                    <th className="text-left py-1 pr-2 w-32">Access</th>
                    <th className="text-right py-1 w-10"></th>
                  </tr>
                </thead>
                <tbody>
                  {permissions.map(p => (
                    <tr key={p.user_key} className="border-t border-border">
                      <td className="py-2 pr-2 font-mono text-xs break-all">{p.user_key}</td>
                      <td className="py-2 pr-2">
                        <select
                          value={p.workflow_access}
                          disabled={busyKey === p.user_key}
                          onChange={(e) => handleChange(p.user_key, e.target.value as AccessLevel)}
                          className="px-2 py-1 text-xs bg-muted/40 border border-border rounded focus:outline-none focus:ring-1 focus:ring-primary disabled:opacity-50"
                        >
                          {ACCESS_LEVELS.map(a => (
                            <option key={a.value} value={a.value}>{a.label}</option>
                          ))}
                        </select>
                      </td>
                      <td className="py-2 text-right">
                        <button
                          onClick={() => handleRevoke(p.user_key)}
                          disabled={busyKey === p.user_key}
                          className="p-1 rounded text-muted-foreground hover:text-destructive hover:bg-destructive/10 disabled:opacity-50"
                          title="Revoke access"
                        >
                          {busyKey === p.user_key ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Trash2 className="w-3.5 h-3.5" />}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </div>
      </div>
    </ModalPortal>
  )
}

export default WorkflowAccessPopup
