import type { PlannerFile } from '../services/api-types'

// Shared planner-files helpers for product surfaces that store one
// manifest per project/session (Video Studio projects, Work sessions).
// The listing returns the requested folder as a node whose children are
// the projects, AND each project again as a top-level sibling, so callers
// must flatten and dedupe by filepath.

export function responseFiles(response: unknown): PlannerFile[] {
  if (Array.isArray(response)) return response as PlannerFile[]
  if (!response || typeof response !== 'object') return []
  const data = (response as { data?: unknown }).data
  return Array.isArray(data) ? data as PlannerFile[] : []
}

export function flattenFiles(items: PlannerFile[], result: PlannerFile[] = []): PlannerFile[] {
  for (const item of items) {
    result.push(item)
    if (Array.isArray(item.children)) flattenFiles(item.children, result)
  }
  return result
}

// Keeps the first entry per filepath, preserving listing order. The listing can
// describe one file from more than one branch of the tree it returns.
export function dedupeByFilepath(files: PlannerFile[]): PlannerFile[] {
  const seen = new Set<string>()
  return files.filter((file) => {
    if (seen.has(file.filepath)) return false
    seen.add(file.filepath)
    return true
  })
}

export function responseContent(response: unknown): { content: string; lastModified?: string } | null {
  if (!response || typeof response !== 'object') return null
  const envelope = response as { data?: unknown; content?: unknown; last_modified?: unknown }
  const value = envelope.data && typeof envelope.data === 'object'
    ? envelope.data as { content?: unknown; last_modified?: unknown }
    : envelope
  if (typeof value.content !== 'string') return null
  return {
    content: value.content,
    lastModified: typeof value.last_modified === 'string' ? value.last_modified : undefined,
  }
}

export function slugifyTitle(title: string, fallback: string): string {
  const slug = title
    .normalize('NFKD')
    .replace(/[\u0300-\u036f]/g, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 48)
  return slug || fallback
}
