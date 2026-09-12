import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { FolderOpen, LoaderCircle, Plus, Trash2 } from 'lucide-react'
import { workflowManifestApi } from '../../services/api'
import type { WorkflowFolderAccessRequest, WorkflowFolderGrant } from '../../services/api-types'
import { useWorkflowManifestStore } from '../../stores/useWorkflowManifestStore'
import { KnowledgebaseSources } from './KnowledgebaseSources'
import { READ_ONLY_TITLE, useCanWriteWorkflow } from '../../hooks/useCanWriteWorkflow'

interface WorkflowFolderAccessViewProps {
  workspacePath: string | null
  headerAction?: React.ReactNode
}

function aliasFromPath(path: string): string {
  const base = path.split(/[\\/]/).filter(Boolean).pop() || 'attached-folder'
  const normalized = base.replace(/[^a-zA-Z0-9_-]+/g, '-').replace(/^-+|-+$/g, '')
  return normalized || 'attached-folder'
}

function grantID(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID()
  return `folder-${Date.now()}-${Math.random().toString(16).slice(2)}`
}

function legacyFolderRequest(request: WorkflowFolderAccessRequest): { path: string; reason: string } | null {
  const match = request.reason.match(/^Folder:\s*(.+?)\s+(?:—|–|-)\s+(.+)$/s)
  if (!match) return null
  const path = match[1].trim()
  const isAbsolute = path.startsWith('/') || /^[a-zA-Z]:[\\/]/.test(path) || path.startsWith('\\\\')
  return isAbsolute ? { path, reason: match[2].trim() } : null
}

function requestedPathFor(request: WorkflowFolderAccessRequest): string {
  return request.requested_path?.trim() || legacyFolderRequest(request)?.path || ''
}

function requestReasonFor(request: WorkflowFolderAccessRequest): string {
  return legacyFolderRequest(request)?.reason || request.reason
}

export default function WorkflowFolderAccessView({ workspacePath, headerAction }: WorkflowFolderAccessViewProps) {
  const canWriteWorkflow = useCanWriteWorkflow(workspacePath)
  const [grants, setGrants] = useState<WorkflowFolderGrant[]>([])
  const [requests, setRequests] = useState<WorkflowFolderAccessRequest[]>([])
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [pendingPath, setPendingPath] = useState('')
  const [pendingAlias, setPendingAlias] = useState('')
  const [pendingAccess, setPendingAccess] = useState<'read_only' | 'read_write'>('read_only')
  const [pendingReason, setPendingReason] = useState('')
  const [activeRequestID, setActiveRequestID] = useState<string | null>(null)

  const load = useCallback(async () => {
    if (!workspacePath) return
    setLoading(true)
    setError(null)
    try {
      const response = await workflowManifestApi.getWorkflowManifest(workspacePath)
      setGrants(response.manifest.folder_access || [])
      setRequests(response.manifest.folder_access_requests || [])
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Unable to load attached folders')
    } finally {
      setLoading(false)
    }
  }, [workspacePath])

  useEffect(() => {
    void load()
  }, [load])

  const canAdd = useMemo(() => {
    const alias = pendingAlias.trim().toLowerCase()
    return Boolean(pendingPath && alias && !grants.some(grant => grant.alias.toLowerCase() === alias))
  }, [grants, pendingAlias, pendingPath])

  const chooseFolder = useCallback(async (request?: WorkflowFolderAccessRequest) => {
    if (!canWriteWorkflow) return
    setError(null)
    if (!window.electronAPI?.pickWorkflowFolder) {
      setError('Folder attachment requires the AgentWorks desktop app so the host folder can be selected safely.')
      return
    }
    const selected = await window.electronAPI.pickWorkflowFolder()
    if (!selected) return
    setPendingPath(selected)
    setPendingAlias(request?.alias || aliasFromPath(selected))
    setPendingAccess(request?.access || 'read_only')
    setPendingReason(request?.reason || '')
    setActiveRequestID(request?.id || null)
  }, [canWriteWorkflow])

  const persist = useCallback(async (next: WorkflowFolderGrant[], nextRequests = requests) => {
    if (!canWriteWorkflow) return false
    if (!workspacePath) return false
    setSaving(true)
    setError(null)
    try {
      await workflowManifestApi.updateWorkflowManifest({ workspace_path: workspacePath, folder_access: next, folder_access_requests: nextRequests })
      setGrants(next)
      setRequests(nextRequests)
      await useWorkflowManifestStore.getState().refreshWorkflows()
      return true
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Unable to update attached folders')
      return false
    } finally {
      setSaving(false)
    }
  }, [canWriteWorkflow, requests, workspacePath])

  const addGrant = useCallback(async () => {
    if (!canAdd) return
    if (pendingAccess === 'read_write' && !window.confirm(`Allow this workflow to modify files in ${pendingPath}?`)) return
    const now = new Date().toISOString()
    const next = [...grants, {
      id: grantID(),
      alias: pendingAlias.trim(),
      path: pendingPath,
      access: pendingAccess,
      reason: pendingReason.trim() || undefined,
      created_at: now,
      updated_at: now,
    }]
    const nextRequests = activeRequestID ? requests.filter(request => request.id !== activeRequestID) : requests
    if (await persist(next, nextRequests)) {
      setPendingPath('')
      setPendingAlias('')
      setPendingAccess('read_only')
      setPendingReason('')
      setActiveRequestID(null)
    }
  }, [activeRequestID, canAdd, grants, pendingAccess, pendingAlias, pendingPath, pendingReason, persist, requests])

  const dismissRequest = useCallback(async (request: WorkflowFolderAccessRequest) => {
    await persist(grants, requests.filter(candidate => candidate.id !== request.id))
  }, [grants, persist, requests])

  const approveRequest = useCallback(async (request: WorkflowFolderAccessRequest) => {
    const requestedPath = requestedPathFor(request)
    if (!requestedPath) return
    const now = new Date().toISOString()
    const next = [...grants, {
      id: grantID(),
      alias: request.alias,
      path: requestedPath,
      access: request.access,
      reason: requestReasonFor(request) || undefined,
      created_at: now,
      updated_at: now,
    }]
    await persist(next, requests.filter(candidate => candidate.id !== request.id))
  }, [grants, persist, requests])

  const removeGrant = useCallback(async (grant: WorkflowFolderGrant) => {
    if (!window.confirm(`Remove access to ${grant.alias}? Open workflow sessions will lose this folder immediately.`)) return
    await persist(grants.filter(candidate => candidate.id !== grant.id))
  }, [grants, persist])

  const changeAccess = useCallback(async (grant: WorkflowFolderGrant, access: 'read_only' | 'read_write') => {
    if (access === grant.access) return
    if (access === 'read_write' && !window.confirm(`Allow this workflow to modify files in ${grant.path}?`)) return
    const now = new Date().toISOString()
    await persist(grants.map(candidate => candidate.id === grant.id ? { ...candidate, access, updated_at: now } : candidate))
  }, [grants, persist])

  return (
        <div className="flex h-full min-h-0 w-full max-w-none flex-col bg-background">
          <div className="flex items-start justify-between border-b border-border px-5 py-4">
            <div>
              <h2 className="text-base font-semibold text-foreground">Attached folders</h2>
              <p className="mt-1 text-xs text-muted-foreground">Folders and shared knowledge bases this workflow can access.</p>
            </div>
            <div className="flex shrink-0 items-center gap-2">
              {!canWriteWorkflow && <span className="rounded-full border border-amber-500/30 bg-amber-500/10 px-2 py-1 text-[11px] font-medium text-amber-700 dark:text-amber-300">Read-only</span>}
              {headerAction}
            </div>
          </div>

          <div className="min-h-0 flex-1 space-y-5 overflow-y-auto p-5">
            {workspacePath && <KnowledgebaseSources key={workspacePath} workspacePath={workspacePath} variant="folders" />}
            <h3 className="text-sm font-semibold text-foreground">External folders</h3>
            {error && <div className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive">{error}</div>}
            {loading ? (
              <div className="flex justify-center py-8"><LoaderCircle className="h-5 w-5 animate-spin text-muted-foreground" /></div>
            ) : (
              <div className="space-y-2">
                {grants.length === 0 && <div className="rounded-lg border border-dashed border-border px-4 py-5 text-center text-xs text-muted-foreground">No external folders attached.</div>}
                {grants.map(grant => (
                  <div key={grant.id} className="rounded-lg border border-border bg-muted/20 p-3">
                    <div className="flex items-start gap-3">
                      <FolderOpen className="mt-0.5 h-4 w-4 shrink-0 text-primary" />
                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="text-sm font-medium text-foreground">{grant.alias}</span>
                          <code className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">linked://{grant.alias}/</code>
                        </div>
                        <div className="mt-1 truncate text-xs text-muted-foreground" title={grant.path}>{grant.path}</div>
                        {grant.reason && <div className="mt-1 text-xs text-muted-foreground">{grant.reason}</div>}
                      </div>
                      <select value={grant.access} disabled={!canWriteWorkflow || saving} title={!canWriteWorkflow ? READ_ONLY_TITLE : undefined} onChange={event => void changeAccess(grant, event.target.value as 'read_only' | 'read_write')} className="rounded-md border border-border bg-background px-2 py-1 text-xs text-foreground disabled:cursor-not-allowed disabled:opacity-50">
                        <option value="read_only">Read only</option>
                        <option value="read_write">Read & write</option>
                      </select>
                      <button type="button" disabled={!canWriteWorkflow || saving} title={!canWriteWorkflow ? READ_ONLY_TITLE : undefined} onClick={() => void removeGrant(grant)} className="rounded-md p-1.5 text-muted-foreground hover:bg-destructive/10 hover:text-destructive disabled:cursor-not-allowed disabled:opacity-50" aria-label={`Remove ${grant.alias}`}><Trash2 className="h-3.5 w-3.5" /></button>
                    </div>
                  </div>
                ))}
              </div>
            )}

            {!loading && requests.length > 0 && (
              <div className="space-y-2">
                <div className="text-sm font-medium text-foreground">Pending requests</div>
                {requests.map(request => {
                  const requestedPath = requestedPathFor(request)
                  return <div key={request.id} className="rounded-lg border border-primary/30 bg-primary/5 p-3">
                    <div className="flex items-start gap-3">
                      <FolderOpen className="mt-0.5 h-4 w-4 shrink-0 text-primary" />
                      <div className="min-w-0 flex-1">
                        <div className="text-sm font-medium text-foreground">{request.alias}</div>
                        <div className="mt-1 text-xs text-muted-foreground">{request.access === 'read_write' ? 'Read & write' : 'Read only'} · {requestReasonFor(request)}</div>
                        {requestedPath && <div className="mt-1 truncate text-xs text-muted-foreground" title={requestedPath}>{requestedPath}</div>}
                      </div>
                      {requestedPath ? (
                        <button type="button" disabled={!canWriteWorkflow || saving} title={!canWriteWorkflow ? READ_ONLY_TITLE : undefined} onClick={() => void approveRequest(request)} className="rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground disabled:opacity-50">Approve</button>
                      ) : (
                        <button type="button" disabled={!canWriteWorkflow || saving} title={!canWriteWorkflow ? READ_ONLY_TITLE : undefined} onClick={() => void chooseFolder(request)} className="rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground disabled:opacity-50">Choose folder</button>
                      )}
                      <button type="button" disabled={!canWriteWorkflow || saving} title={!canWriteWorkflow ? READ_ONLY_TITLE : undefined} onClick={() => void dismissRequest(request)} className="rounded-md px-2 py-1.5 text-xs text-muted-foreground hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50">Deny</button>
                    </div>
                  </div>
                } )}
              </div>
            )}

            <div className="rounded-lg border border-border p-4">
              <div className="text-sm font-medium text-foreground">Attach another folder</div>
              <button type="button" disabled={!canWriteWorkflow} title={!canWriteWorkflow ? READ_ONLY_TITLE : undefined} onClick={() => void chooseFolder()} className="mt-3 inline-flex items-center gap-2 rounded-md border border-border bg-muted px-3 py-2 text-xs font-medium text-foreground hover:bg-muted/70 disabled:cursor-not-allowed disabled:opacity-50"><FolderOpen className="h-4 w-4" />Choose folder…</button>
              {pendingPath && (
                <div className="mt-3 grid gap-3 sm:grid-cols-2">
                  <label className="text-xs text-muted-foreground">Alias<input value={pendingAlias} disabled={!canWriteWorkflow} title={!canWriteWorkflow ? READ_ONLY_TITLE : undefined} onChange={event => setPendingAlias(event.target.value)} className="mt-1 w-full rounded-md border border-border bg-background px-2.5 py-2 text-sm text-foreground disabled:cursor-not-allowed disabled:opacity-50" /></label>
                  <label className="text-xs text-muted-foreground">Access<select value={pendingAccess} disabled={!canWriteWorkflow} title={!canWriteWorkflow ? READ_ONLY_TITLE : undefined} onChange={event => setPendingAccess(event.target.value as 'read_only' | 'read_write')} className="mt-1 w-full rounded-md border border-border bg-background px-2.5 py-2 text-sm text-foreground disabled:cursor-not-allowed disabled:opacity-50"><option value="read_only">Read only</option><option value="read_write">Read & write</option></select></label>
                  <label className="text-xs text-muted-foreground sm:col-span-2">Reason (optional)<input value={pendingReason} disabled={!canWriteWorkflow} title={!canWriteWorkflow ? READ_ONLY_TITLE : undefined} onChange={event => setPendingReason(event.target.value)} className="mt-1 w-full rounded-md border border-border bg-background px-2.5 py-2 text-sm text-foreground disabled:cursor-not-allowed disabled:opacity-50" placeholder="Why this workflow needs the folder" /></label>
                  <div className="truncate text-xs text-muted-foreground sm:col-span-2" title={pendingPath}>{pendingPath}</div>
                  <button type="button" disabled={!canWriteWorkflow || !canAdd || saving} title={!canWriteWorkflow ? READ_ONLY_TITLE : undefined} onClick={() => void addGrant()} className="inline-flex w-fit items-center gap-2 rounded-md bg-primary px-3 py-2 text-xs font-medium text-primary-foreground disabled:opacity-50"><Plus className="h-3.5 w-3.5" />Attach folder</button>
                </div>
              )}
            </div>
            <p className="text-[11px] leading-relaxed text-muted-foreground">Attached folders are host-local. Shell commands receive a WORKFLOW_FOLDER_* environment variable, and safe patches can use the linked:// alias. Existing tools are not duplicated or restored.</p>
          </div>
        </div>
  )
}
