import { useEffect, useMemo } from 'react'
import { Link2, X } from 'lucide-react'
import { useWorkflowManifestStore } from '../../stores/useWorkflowManifestStore'
import { normalizeWorkspacePath } from '../../utils/workspacePathUtils'

type WorkflowReferenceAccessProps = {
  selectedPaths: string[]
  onChange: (paths: string[]) => void | Promise<unknown>
  excludeWorkspacePath?: string | null
  disabled?: boolean
}

export function WorkflowReferenceAccess({ selectedPaths, onChange, excludeWorkspacePath, disabled = false }: WorkflowReferenceAccessProps) {
  const workflows = useWorkflowManifestStore(state => state.workflows)
  const refreshWorkflows = useWorkflowManifestStore(state => state.refreshWorkflows)
  const normalizedSelected = useMemo(
    () => [...new Set(selectedPaths.map(normalizeWorkspacePath).filter(Boolean))],
    [selectedPaths],
  )
  const excluded = normalizeWorkspacePath(excludeWorkspacePath || '')
  const available = workflows.filter(workflow => {
    const path = normalizeWorkspacePath(workflow.workspace_path)
    return path && path !== excluded && !normalizedSelected.includes(path)
  })

  useEffect(() => {
    if (workflows.length === 0) void refreshWorkflows()
  }, [refreshWorkflows, workflows.length])

  const add = (path: string) => {
    const normalized = normalizeWorkspacePath(path)
    if (!normalized || normalized === excluded || normalizedSelected.includes(normalized)) return
    void onChange([...normalizedSelected, normalized])
  }

  return (
    <section className="rounded-lg border border-border p-4">
      <div className="flex items-start gap-2">
        <Link2 className="mt-0.5 h-4 w-4 text-muted-foreground" />
        <div>
          <h3 className="text-sm font-medium text-foreground">Linked workflows</h3>
          <p className="mt-1 text-xs text-muted-foreground">Attach another workflow as durable read-only context. Access is checked again on every run.</p>
        </div>
      </div>
      {normalizedSelected.length > 0 && (
        <div className="mt-3 space-y-2">
          {normalizedSelected.map(path => {
            const workflow = workflows.find(item => normalizeWorkspacePath(item.workspace_path) === path)
            return (
              <div key={path} className="flex items-center gap-2 rounded-md border border-border bg-muted/30 px-3 py-2">
                <div className="min-w-0 flex-1">
                  <div className="truncate text-xs font-medium text-foreground">{workflow?.manifest.label || path.split('/').pop() || path}</div>
                  <div className="truncate font-mono text-[10px] text-muted-foreground">{path} · read only</div>
                </div>
                <button type="button" disabled={disabled} onClick={() => void onChange(normalizedSelected.filter(candidate => candidate !== path))} aria-label={`Remove ${path}`} className="rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-50"><X className="h-3.5 w-3.5" /></button>
              </div>
            )
          })}
        </div>
      )}
      <select disabled={disabled || available.length === 0} value="" onChange={event => add(event.target.value)} className="mt-3 w-full rounded-md border border-border bg-background px-2.5 py-2 text-xs text-foreground disabled:opacity-50">
        <option value="">{available.length > 0 ? 'Attach a workflow…' : 'No other accessible workflows'}</option>
        {available.map(workflow => <option key={workflow.workspace_path} value={workflow.workspace_path}>{workflow.manifest.label} — {workflow.workspace_path}</option>)}
      </select>
    </section>
  )
}
