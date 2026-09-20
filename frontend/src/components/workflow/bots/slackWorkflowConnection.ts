import type { SlackConnection } from '../../../services/api-types'

export type WorkflowSlackSelection = {
  // Connection scoped to this workflow (owned by its owners), if any.
  own: SlackConnection | null
  // The connection this workflow actually talks through: the manifest
  // selection, else the platform default. Null when the selection dangles
  // or nothing is configured.
  effective: SlackConnection | null
  // The raw manifest selection, kept so the UI can warn when it dangles.
  selectionId: string
}

export function resolveWorkflowSlackConnection(
  connections: SlackConnection[] | undefined,
  workspacePath: string | null,
  selectionId: string | undefined,
  defaultId: string | undefined,
): WorkflowSlackSelection {
  const list = connections || []
  const byId = new Map(list.map(entry => [entry.id, entry]))
  const own = (workspacePath ? list.find(entry => entry.workspace_path === workspacePath) : undefined) || null
  const selection = (selectionId || '').trim()
  let effective: SlackConnection | null = null
  if (selection) {
    effective = byId.get(selection) || null
  } else if (defaultId) {
    effective = byId.get(defaultId) || null
  }
  return { own, effective, selectionId: selection }
}
