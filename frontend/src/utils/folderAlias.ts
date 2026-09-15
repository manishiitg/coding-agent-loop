// Suggests a folder alias from a host path: base name, safe characters
// only. Shared by the workflow attached-folders view and the Work
// attached-folders panel.
export function aliasFromPath(path: string): string {
  const base = path.split(/[\\/]/).filter(Boolean).pop() || 'attached-folder'
  const normalized = base.replace(/[^a-zA-Z0-9_-]+/g, '-').replace(/^-+|-+$/g, '')
  return normalized || 'attached-folder'
}
