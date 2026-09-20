import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { Loader2, AlertCircle, Share2, Trash2, Plus } from 'lucide-react'
import { authApi, type WorkflowAccessInfo, type WorkflowAccessUser } from '../../services/api'
import { useAuthStore } from '../../stores/useAuthStore'
import { useWorkflowManifestStore } from '../../stores/useWorkflowManifestStore'
import { SettingsCard, SettingsEmpty } from '../ui/SettingsCard'
import { Button } from '../ui/Button'
import { Label } from '../ui/label'

interface WorkflowSharePopupProps {
  workspacePath: string
  /** Readers may inspect membership but cannot change grants. */
  readOnly?: boolean
}

/**
 * Share one workflow: who owns it (edit, run, share, delete) and who may
 * read it (chat, run, watch — nothing changes). Owners and admins only; the
 * server refuses anything else and never lets the last owner go.
 * docs/design/user_accounts_and_workflow_sharing.md, phase 3.
 */
const WorkflowSharePopup: React.FC<WorkflowSharePopupProps> = ({ workspacePath, readOnly = false }) => {
  const me = useAuthStore((s) => s.user)
  const refreshWorkflows = useWorkflowManifestStore((s) => s.refreshWorkflows)
  const [info, setInfo] = useState<WorkflowAccessInfo | null>(null)
  const [directory, setDirectory] = useState<WorkflowAccessUser[]>([])
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [pickId, setPickId] = useState('')
  const [pickRole, setPickRole] = useState<'reader' | 'owner'>('reader')

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const [access, dir] = await Promise.all([authApi.getWorkflowAccess(workspacePath), authApi.listUserDirectory()])
      setInfo(access)
      setDirectory(dir.users || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [workspacePath])

  useEffect(() => {
    void load()
  }, [load])

  const save = useCallback(async (owners: string[], readers: string[]) => {
    if (readOnly) return
    setSaving(true)
    setError(null)
    try {
      const next = await authApi.setWorkflowAccess(workspacePath, owners, readers)
      setInfo(next)
      void refreshWorkflows()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }, [readOnly, workspacePath, refreshWorkflows])

  const ownerIds = useMemo(() => (info?.owners ?? []).map((u) => u.id), [info])
  const readerIds = useMemo(() => (info?.readers ?? []).map((u) => u.id), [info])
  const candidates = useMemo(
    () => directory.filter((u) => !ownerIds.includes(u.id) && !readerIds.includes(u.id)),
    [directory, ownerIds, readerIds],
  )

  const add = () => {
    if (!pickId) return
    if (pickRole === 'owner') void save([...ownerIds, pickId], readerIds)
    else void save(ownerIds, [...readerIds, pickId])
    setPickId('')
  }
  const remove = (id: string) => void save(ownerIds.filter((x) => x !== id), readerIds.filter((x) => x !== id))
  const promote = (id: string) => void save([...ownerIds, id], readerIds.filter((x) => x !== id))
  const demote = (id: string) => void save(ownerIds.filter((x) => x !== id), [...readerIds, id])

  const label = (u: WorkflowAccessUser) => (u.id === me?.id ? `${u.username} (you)` : u.username)
  const total = ownerIds.length + readerIds.length

  return (
    <div className="space-y-4">
      <SettingsCard
        icon={<Share2 className="h-4 w-4 text-primary" />}
        title="Sharing"
        count={info ? `${total} ${total === 1 ? 'person' : 'people'}` : undefined}
        description={
          <>
            Owners edit, run, share and delete. Read-only people can chat, run and watch, but change nothing.
            {readOnly && <span className="mt-1 block font-medium text-amber-600 dark:text-amber-400">Read-only: membership is visible, but all changes are disabled.</span>}
            {info?.legacy && <span className="mt-1 block text-amber-600">Nothing recorded yet: every member can edit this workflow until you save a first grant.</span>}
          </>
        }
      >
        {error && (
          <div className="flex items-start gap-2 rounded-md border border-destructive/30 bg-destructive/10 p-3 text-xs text-destructive">
            <AlertCircle className="h-4 w-4 mt-0.5 flex-shrink-0" />
            <span>{error}</span>
          </div>
        )}
        {!readOnly && (
          <div className="flex flex-col gap-2 sm:flex-row sm:items-end">
            <div className="flex-1">
              <Label>Add a person</Label>
              <select value={pickId} onChange={(e) => setPickId(e.target.value)} className="w-full mt-1 px-2 py-1.5 text-sm bg-muted/40 border border-border rounded disabled:cursor-not-allowed disabled:opacity-50">
                <option value="">Choose a user…</option>
                {candidates.map((u) => <option key={u.id} value={u.id}>{u.username}{u.email ? ` · ${u.email}` : ''}</option>)}
              </select>
            </div>
            <div className="sm:w-36">
              <Label>As</Label>
              <select value={pickRole} onChange={(e) => setPickRole(e.target.value as 'reader' | 'owner')} className="w-full mt-1 px-2 py-1.5 text-sm bg-muted/40 border border-border rounded disabled:cursor-not-allowed disabled:opacity-50">
                <option value="reader">Read-only</option>
                <option value="owner">Owner</option>
              </select>
            </div>
            <Button size="sm" disabled={!pickId || saving} onClick={add}>
              {saving ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Plus className="h-3.5 h-3.5" />} Add
            </Button>
          </div>
        )}
        {loading || !info ? (
          <div className="flex items-center gap-2 text-muted-foreground">
            <Loader2 className="h-4 w-4 animate-spin" />
            <span>Loading sharing…</span>
          </div>
        ) : (
          <>
            <div>
              <p className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground mb-1">Owners</p>
              {info.owners.length === 0 && <SettingsEmpty>No owner recorded.</SettingsEmpty>}
              {info.owners.map((u) => (
                <div key={u.id} className="flex items-center justify-between py-1.5 border-t border-border">
                  <span className="text-sm">{label(u)}{u.email && <span className="ml-1 text-xs text-muted-foreground">{u.email}</span>}</span>
                  <span className="flex items-center gap-1">
                    <Button variant="outline" size="sm" disabled={readOnly || saving || info.owners.length < 2} title={readOnly ? 'Read-only access' : info.owners.length < 2 ? 'A workflow needs at least one owner' : 'Make read-only'} onClick={() => demote(u.id)}>Make read-only</Button>
                    <Button variant="ghost" size="icon" className="h-7 w-7 text-destructive hover:bg-destructive/10" disabled={readOnly || saving || info.owners.length < 2} title={readOnly ? 'Read-only access' : 'Remove'} onClick={() => remove(u.id)}><Trash2 className="h-3.5 w-3.5" /></Button>
                  </span>
                </div>
              ))}
            </div>
            <div>
              <p className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground mt-4 mb-1">Read-only</p>
              {info.readers.length === 0 && <SettingsEmpty>Nobody yet.</SettingsEmpty>}
              {info.readers.map((u) => (
                <div key={u.id} className="flex items-center justify-between py-1.5 border-t border-border">
                  <span className="text-sm">{label(u)}{u.email && <span className="ml-1 text-xs text-muted-foreground">{u.email}</span>}</span>
                  <span className="flex items-center gap-1">
                    <Button variant="outline" size="sm" disabled={readOnly || saving} title={readOnly ? 'Read-only access' : undefined} onClick={() => promote(u.id)}>Make owner</Button>
                    <Button variant="ghost" size="icon" className="h-7 w-7 text-destructive hover:bg-destructive/10" disabled={readOnly || saving} title={readOnly ? 'Read-only access' : 'Remove'} onClick={() => remove(u.id)}><Trash2 className="h-3.5 w-3.5" /></Button>
                  </span>
                </div>
              ))}
            </div>
          </>
        )}
      </SettingsCard>
    </div>
  )
}

export default WorkflowSharePopup
