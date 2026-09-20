import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { Loader2, Trash2, AlertCircle, Users, KeyRound, Ban, CheckCircle2 } from 'lucide-react'
import { authApi, type AdminUser, type AdminUserWrite } from '../../services/api'
import { useAuthStore } from '../../stores/useAuthStore'
import { SettingsCard, SettingsEmpty } from '../ui/SettingsCard'
import { Button } from '../ui/Button'
import { Checkbox } from '../ui/checkbox'
import { Badge } from '../ui/badge'
import { SecretField } from '../ui/SecretField'
import ConfirmationDialog from '../ui/ConfirmationDialog'

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
 * Users & access: the admin page for the user directory. Set each account's
 * role and which products they may open, reset passwords, disable or delete.
 */
const UsersAdminPanel: React.FC = () => {
  const me = useAuthStore((s) => s.user)
  const [users, setUsers] = useState<AdminUser[]>([])
  const [products, setProducts] = useState<string[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [busyId, setBusyId] = useState<string | null>(null)

  const [resetFor, setResetFor] = useState<AdminUser | null>(null)
  const [resetPassword, setResetPassword] = useState('')
  const [deleteFor, setDeleteFor] = useState<AdminUser | null>(null)

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
    void refresh()
  }, [refresh])

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

  return (
    <div className="space-y-4">
      <SettingsCard
        icon={<Users className="h-4 w-4 text-primary" />}
        title="Accounts"
        count={`${sorted.length} ${sorted.length === 1 ? 'account' : 'accounts'}`}
        description="Everyone who can open this deployment, and what each account may do. A member owns what they create; a contributor may edit assigned workflows but cannot create new ones; a read-only account only sees shared workflows. Product boxes decide which surfaces an account may open."
      >
        {error && (
          <div className="flex items-start gap-2 rounded-md border border-destructive/30 bg-destructive/10 p-3 text-xs text-destructive">
            <AlertCircle className="h-4 w-4 mt-0.5 flex-shrink-0" />
            <span>{error}</span>
          </div>
        )}
        {loading ? (
          <div className="flex items-center gap-2 text-muted-foreground">
            <Loader2 className="h-4 w-4 animate-spin" />
            <span>Loading accounts…</span>
          </div>
        ) : sorted.length === 0 ? (
          <SettingsEmpty>No accounts yet. New accounts appear here after they are added.</SettingsEmpty>
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
                            <label key={p} className="inline-flex items-center gap-1.5">
                              <Checkbox
                                disabled={busy}
                                checked={u.products.includes(p)}
                                onCheckedChange={() => { void run(u.id, () => authApi.updateAdminUser(u.id, { products: toggleProduct(u.products, p) })) }}
                                aria-label={`${productLabel(p)} for ${u.username}`}
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
                      {u.disabled
                        ? <Badge variant="outline" className="text-destructive"><Ban className="mr-1 h-3 w-3" />Disabled</Badge>
                        : <Badge variant="secondary"><CheckCircle2 className="mr-1 h-3 w-3" />Active</Badge>}
                    </td>
                    <td className="py-2 align-top">
                      <div className="flex items-center justify-end gap-1">
                        {busy && <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />}
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-7 w-7"
                          title="Set a new password"
                          disabled={busy}
                          onClick={() => { setResetFor(u); setResetPassword('') }}
                        >
                          <KeyRound className="h-3.5 w-3.5" />
                        </Button>
                        {!isMe && (
                          <Button
                            variant="ghost"
                            size="icon"
                            className="h-7 w-7"
                            title={u.disabled ? 'Enable account' : 'Disable account'}
                            disabled={busy}
                            onClick={() => { void run(u.id, () => authApi.updateAdminUser(u.id, { disabled: !u.disabled })) }}
                          >
                            {u.disabled ? <CheckCircle2 className="h-3.5 w-3.5" /> : <Ban className="h-3.5 h-3.5" />}
                          </Button>
                        )}
                        {!isMe && (
                          <Button
                            variant="ghost"
                            size="icon"
                            className="h-7 w-7 text-destructive hover:bg-destructive/10"
                            title="Delete account (their files are kept)"
                            disabled={busy}
                            onClick={() => setDeleteFor(u)}
                          >
                            <Trash2 className="h-3.5 h-3.5" />
                          </Button>
                        )}
                      </div>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        )}
      </SettingsCard>

      {resetFor && (
        <SettingsCard
          icon={<KeyRound className="h-4 w-4 text-primary" />}
          title={`New password for ${resetFor.username}`}
          description="At least 8 characters. The old password stops working immediately."
        >
          <div className="flex flex-col gap-2 sm:flex-row sm:items-end">
            <div className="flex-1">
              <SecretField
                label="New password"
                value={resetPassword}
                onChange={setResetPassword}
                placeholder="min 8 characters"
              />
            </div>
            <div className="flex gap-2">
              <Button
                disabled={resetPassword.length < 8 || busyId === resetFor.id}
                onClick={() => { const target = resetFor; void run(target.id, async () => { await authApi.updateAdminUser(target.id, { password: resetPassword }); setResetFor(null) }) }}
              >
                Save password
              </Button>
              <Button variant="outline" onClick={() => setResetFor(null)}>Cancel</Button>
            </div>
          </div>
        </SettingsCard>
      )}

      <ConfirmationDialog
        isOpen={deleteFor !== null}
        onClose={() => setDeleteFor(null)}
        onConfirm={() => { const target = deleteFor; setDeleteFor(null); if (target) void run(target.id, () => authApi.deleteAdminUser(target.id)) }}
        title={`Delete ${deleteFor?.username ?? 'account'}?`}
        message={deleteFor ? `Delete the account ${deleteFor.username}? Their files stay on disk. This cannot be undone.` : ''}
        confirmText="Delete account"
        type="danger"
        requireText={deleteFor?.username}
      />
    </div>
  )
}

export default UsersAdminPanel
