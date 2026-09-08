import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { X, Loader2, Trash2, AlertCircle, Users, KeyRound, Ban, CheckCircle2 } from 'lucide-react'
import { authApi, type AdminUser, type AdminUserWrite } from '../../services/api'
import { useAuthStore } from '../../stores/useAuthStore'
import ModalPortal from '../ui/ModalPortal'

interface UsersAdminPanelProps {
  isOpen: boolean
  onClose: () => void
  /** Render only the body, for a host (WorkflowAccessView) that supplies the pane shell and header. */
  embedded?: boolean
}

// Account roles separate creating a new workflow from editing one explicitly
// assigned to the account. Existing records without can_edit retain their old
// behavior server-side (can_edit follows can_create).
type Role = 'admin' | 'member' | 'contributor' | 'readonly'
const ROLES: { value: Role; label: string; hint: string }[] = [
  { value: 'readonly', label: 'Read-only', hint: 'Can chat, run and watch what is shared with them. Cannot create or edit anything.' },
  { value: 'contributor', label: 'Contributor', hint: 'Can own and edit assigned workflows, but cannot create new workflows.' },
  { value: 'member', label: 'Member', hint: 'Creates workflows and projects and owns what they create.' },
  { value: 'admin', label: 'Admin', hint: 'Member, plus manages users and product access. Can open any workflow.' },
]
const roleOf = (u: { admin: boolean; can_create: boolean; can_edit: boolean }): Role => (
  u.admin ? 'admin' : u.can_create ? 'member' : u.can_edit ? 'contributor' : 'readonly'
)
const roleFields = (r: Role): Pick<AdminUserWrite, 'admin' | 'can_create' | 'can_edit'> => ({
  admin: r === 'admin',
  can_create: r === 'admin' || r === 'member',
  can_edit: r !== 'readonly',
})

const PRODUCT_LABELS: Record<string, string> = {
  agentworks: 'AgentWorks',
  'video-studio': 'Video Studio',
  finance: 'Finance',
  dominion: 'Dominion',
}
const productLabel = (id: string) => PRODUCT_LABELS[id] ?? id

/**
 * Users & access: the admin page for the user directory (config/users.json).
 * Add accounts, set their role and which products they may open, reset
 * passwords, disable or delete. Replaces the old per-user workflow-access
 * tier popup; those tiers are now derived from the role here.
 */
const UsersAdminPanel: React.FC<UsersAdminPanelProps> = ({ isOpen, onClose, embedded = false }) => {
  const me = useAuthStore((s) => s.user)
  const [users, setUsers] = useState<AdminUser[]>([])
  const [products, setProducts] = useState<string[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [busyId, setBusyId] = useState<string | null>(null)

  const [resetFor, setResetFor] = useState<AdminUser | null>(null)
  const [resetPassword, setResetPassword] = useState('')

  const refresh = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const resp = await authApi.listAdminUsers()
      setUsers(resp.users || [])
      setProducts(resp.products || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (isOpen) void refresh()
  }, [isOpen, refresh])

  const run = useCallback(async (id: string | null, action: () => Promise<unknown>) => {
    setBusyId(id)
    setError(null)
    try {
      await action()
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusyId(null)
    }
  }, [refresh])

  const toggleProduct = (list: string[], id: string) => (list.includes(id) ? list.filter((p) => p !== id) : [...list, id])

  const sorted = useMemo(() => [...users].sort((a, b) => a.username.localeCompare(b.username)), [users])

  if (!isOpen) return null

  const body = (
    <>
          <div className="px-4 pt-3 pb-2 text-xs text-muted-foreground">
            Accounts live in <code className="text-[11px] bg-muted px-1 py-0.5 rounded">config/users.json</code>. A member owns what they create;
            a contributor may edit assigned workflows but cannot create new ones; a read-only account only sees shared workflows. Product boxes decide which surfaces an account may open
            (a member with none ticked may open all; a read-only account with none ticked may open none).
          </div>

          {/* No add-account form for now (removed 2026-09-03 at the user's
              request): accounts are created by AUTH_USERS at startup or via
              POST /api/admin/users; this panel manages what exists. */}

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
            ) : sorted.length === 0 ? (
              <div className="py-8 text-center text-sm text-muted-foreground">
                No accounts yet. Set <code>AUTH_USERS</code> and <code>ADMIN_USERS</code> and restart to import.
              </div>
            ) : (
              <table className="w-full text-sm">
                <thead className="text-[11px] uppercase tracking-wide text-muted-foreground">
                  <tr className="text-left">
                    <th className="py-1 pr-3 font-semibold">User</th>
                    <th className="py-1 pr-3 font-semibold">Role</th>
                    <th className="py-1 pr-3 font-semibold">Products</th>
                    <th className="py-1 pr-3 font-semibold">Status</th>
                    <th className="py-1 font-semibold text-right">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {sorted.map((u) => {
                    const isMe = u.id === me?.id
                    const busy = busyId === u.id
                    const role = roleOf(u)
                    return (
                      <tr key={u.id} className={`border-t border-border ${u.disabled ? 'opacity-60' : ''}`}>
                        <td className="py-2 pr-3 align-top">
                          <div className="font-medium">{u.username}{isMe && <span className="ml-1 text-[11px] text-muted-foreground">(you)</span>}</div>
                          <div className="text-[11px] text-muted-foreground">{u.email || '—'} · {u.provider}{u.has_password ? '' : ' · no password'}</div>
                        </td>
                        <td className="py-2 pr-3 align-top">
                          <select
                            value={role}
                            disabled={busy || (isMe && role === 'admin')}
                            title={isMe && role === 'admin' ? 'You cannot remove your own admin access' : undefined}
                            onChange={(e) => { void run(u.id, () => authApi.updateAdminUser(u.id, roleFields(e.target.value as Role))) }}
                            className="px-2 py-1 text-xs bg-muted/40 border border-border rounded"
                          >
                            {ROLES.map((r) => <option key={r.value} value={r.value}>{r.label}</option>)}
                          </select>
                        </td>
                        <td className="py-2 pr-3 align-top">
                          {role === 'admin' ? (
                            <span className="text-xs text-muted-foreground">all</span>
                          ) : (
                            <div className="flex flex-wrap gap-2 text-xs">
                              {products.map((p) => (
                                <label key={p} className="inline-flex items-center gap-1">
                                  <input
                                    type="checkbox"
                                    disabled={busy}
                                    checked={u.products.includes(p)}
                                    onChange={() => { void run(u.id, () => authApi.updateAdminUser(u.id, { products: toggleProduct(u.products, p) })) }}
                                  />
                                  {productLabel(p)}
                                </label>
                              ))}
                              {role === 'member' && u.products.length === 0 && <span className="text-muted-foreground">(all)</span>}
                              {role === 'readonly' && u.products.length === 0 && <span className="text-muted-foreground">(none)</span>}
                            </div>
                          )}
                        </td>
                        <td className="py-2 pr-3 align-top text-xs">
                          {u.disabled ? <span className="inline-flex items-center gap-1 text-destructive"><Ban className="w-3 h-3" /> Disabled</span>
                            : <span className="inline-flex items-center gap-1 text-emerald-600"><CheckCircle2 className="w-3 h-3" /> Active</span>}
                        </td>
                        <td className="py-2 align-top">
                          <div className="flex items-center justify-end gap-1">
                            {busy && <Loader2 className="w-3.5 h-3.5 animate-spin text-muted-foreground" />}
                            <button
                              title="Set a new password"
                              disabled={busy}
                              onClick={() => { setResetFor(u); setResetPassword('') }}
                              className="p-1 rounded hover:bg-accent text-muted-foreground"
                            >
                              <KeyRound className="w-3.5 h-3.5" />
                            </button>
                            {!isMe && (
                              <button
                                title={u.disabled ? 'Enable account' : 'Disable account'}
                                disabled={busy}
                                onClick={() => { void run(u.id, () => authApi.updateAdminUser(u.id, { disabled: !u.disabled })) }}
                                className="p-1 rounded hover:bg-accent text-muted-foreground"
                              >
                                {u.disabled ? <CheckCircle2 className="w-3.5 h-3.5" /> : <Ban className="w-3.5 h-3.5" />}
                              </button>
                            )}
                            {!isMe && (
                              <button
                                title="Delete account (their files are kept)"
                                disabled={busy}
                                onClick={() => { if (confirm(`Delete the account ${u.username}? Their files stay on disk.`)) void run(u.id, () => authApi.deleteAdminUser(u.id)) }}
                                className="p-1 rounded hover:bg-destructive/10 text-destructive"
                              >
                                <Trash2 className="w-3.5 h-3.5" />
                              </button>
                            )}
                          </div>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            )}
          </div>

          {resetFor && (
            <div className="px-4 py-3 border-t border-border flex flex-col gap-2 sm:flex-row sm:items-end">
              <div className="flex-1">
                <label className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">New password for {resetFor.username}</label>
                <input
                  type="password"
                  autoFocus
                  value={resetPassword}
                  onChange={(e) => setResetPassword(e.target.value)}
                  placeholder="min 8 characters"
                  className="w-full mt-1 px-2 py-1.5 text-sm bg-muted/40 border border-border rounded focus:outline-none focus:ring-1 focus:ring-primary"
                  onKeyDown={(e) => { if (e.key === 'Escape') setResetFor(null) }}
                />
              </div>
              <button
                disabled={resetPassword.length < 8 || busyId === resetFor.id}
                onClick={() => { const target = resetFor; void run(target.id, async () => { await authApi.updateAdminUser(target.id, { password: resetPassword }); setResetFor(null) }) }}
                className="px-3 py-1.5 text-sm rounded bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
              >
                Save password
              </button>
              <button onClick={() => setResetFor(null)} className="px-3 py-1.5 text-sm rounded border border-border hover:bg-accent">Cancel</button>
            </div>
          )}
    </>
  )
  if (embedded) return body

  return (
    <ModalPortal>
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
        <div
          className="bg-background border border-border rounded-lg shadow-xl w-full max-w-4xl max-h-[88vh] flex flex-col"
          onClick={(e) => e.stopPropagation()}
        >
          <div className="flex items-center justify-between p-4 border-b border-border">
            <div className="flex items-center gap-2">
              <Users className="w-5 h-5 text-muted-foreground" />
              <h2 className="text-lg font-semibold">Users &amp; access</h2>
            </div>
            <button onClick={onClose} className="p-1 rounded hover:bg-accent" aria-label="Close">
              <X className="w-4 h-4" />
            </button>
          </div>
          {body}
        </div>
      </div>
    </ModalPortal>
  )
}

export default UsersAdminPanel
