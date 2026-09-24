import type { PlannerFile } from '../services/api-types'

/**
 * @-mention files for product-profile chats (Crew and other product projects).
 * The agent's working directory is the project root, so the picker lists that
 * project's own files with project-relative paths — the same spelling the CLI
 * resolves from its cwd — instead of the global AgentWorks workspace tree.
 */

type FileLister = {
  listFiles: (folder?: string, limit?: number, maxDepth?: number) => Promise<{ success: boolean; data: PlannerFile[] }>
}

/** Heavy or tool-owned folders that only add noise to the picker. */
const SKIPPED_SEGMENTS = new Set(['node_modules', '.git', '.sandbox-cache', '.cache', '.local', '.tmp', 'tool_output_folder'])

export const PROFILE_AT_FILES_MAX_DEPTH = 5

function normalizeRoot(root: string): string {
  return root.replace(/\\/g, '/').replace(/^\/+|\/+$/g, '')
}

/** Project-relative path for filepath under root, or null when outside it. */
export function profileRelativePath(root: string, filepath: string): string | null {
  const base = normalizeRoot(root)
  const target = filepath.replace(/\\/g, '/').replace(/^\/+|\/+$/g, '')
  if (!base || target === base) return null
  if (target.startsWith(`${base}/`)) return target.slice(base.length + 1)
  // Logical (Chats/...) and physical (_users/<id>/Chats/...) spellings of the
  // same project: match on the logical suffix.
  const logical = (path: string) => path.replace(/^_users\/[^/]+\//, '')
  const logicalBase = logical(base)
  const logicalTarget = logical(target)
  if (logicalTarget.startsWith(`${logicalBase}/`)) return logicalTarget.slice(logicalBase.length + 1)
  return null
}

/** Rebuild a folder tree (project-relative paths) from any listing shape. */
export function buildProfileAtFileTree(root: string, listing: PlannerFile[]): PlannerFile[] {
  const flat: Array<{ path: string; type: 'file' | 'folder' }> = []
  const visit = (entries: PlannerFile[] | undefined) => {
    for (const entry of entries || []) {
      const rel = profileRelativePath(root, entry.filepath)
      if (rel && !rel.split('/').some(segment => SKIPPED_SEGMENTS.has(segment))) {
        flat.push({ path: rel, type: entry.type === 'folder' || (entry.children?.length ?? 0) > 0 ? 'folder' : 'file' })
      }
      visit(entry.children)
    }
  }
  visit(listing)

  const nodes = new Map<string, PlannerFile>()
  const roots: PlannerFile[] = []
  const ensure = (path: string, type: 'file' | 'folder'): PlannerFile => {
    const existing = nodes.get(path)
    if (existing) {
      if (type === 'folder') existing.type = 'folder'
      return existing
    }
    const node: PlannerFile = { filepath: path, type, ...(type === 'folder' ? { children: [] } : {}) }
    nodes.set(path, node)
    const slash = path.lastIndexOf('/')
    if (slash === -1) {
      roots.push(node)
    } else {
      const parent = ensure(path.slice(0, slash), 'folder')
      parent.children = parent.children || []
      parent.children.push(node)
    }
    return node
  }
  for (const item of flat.sort((a, b) => a.path.localeCompare(b.path))) ensure(item.path, item.type)
  const sort = (entries: PlannerFile[]) => {
    entries.sort((a, b) => (a.type === b.type ? a.filepath.localeCompare(b.filepath) : a.type === 'folder' ? -1 : 1))
    for (const entry of entries) if (entry.children) sort(entry.children)
  }
  sort(roots)
  return roots
}

/**
 * Loads a product project's files for the @ picker. Tries the normal workspace
 * listing first; a shared Crew (another owner's project) is refused there and
 * falls back to the mediated shared-project client when one is supplied.
 */
export async function loadProfileAtFiles(root: string, primary: FileLister, fallback?: FileLister | null): Promise<PlannerFile[]> {
  const attempt = async (client: FileLister) => {
    const response = await client.listFiles(root, -1, PROFILE_AT_FILES_MAX_DEPTH)
    if (!response?.success) throw new Error('listing failed')
    return buildProfileAtFileTree(root, response.data || [])
  }
  try {
    return await attempt(primary)
  } catch (cause) {
    if (!fallback) throw cause
    return attempt(fallback)
  }
}
