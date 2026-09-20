export interface SavedViewport {
  x: number
  y: number
  zoom: number
}

export function viewportStorageKey(workspacePath?: string | null): string {
  return workspacePath ? `workflow-viewport-${workspacePath}` : 'workflow-viewport-default'
}

function isValidViewport(value: unknown, minZoom: number, maxZoom: number): value is SavedViewport {
  if (typeof value !== 'object' || value === null) return false
  const { x, y, zoom } = value as Record<string, unknown>
  return (
    typeof x === 'number' && Number.isFinite(x) &&
    typeof y === 'number' && Number.isFinite(y) &&
    typeof zoom === 'number' && Number.isFinite(zoom) &&
    zoom >= minZoom && zoom <= maxZoom
  )
}

export function readSavedViewport(key: string, minZoom: number, maxZoom: number): SavedViewport | null {
  try {
    const raw = localStorage.getItem(key)
    if (!raw) return null
    const parsed: unknown = JSON.parse(raw)
    return isValidViewport(parsed, minZoom, maxZoom) ? parsed : null
  } catch {
    return null
  }
}

export function writeSavedViewport(key: string, viewport: SavedViewport): void {
  try {
    localStorage.setItem(key, JSON.stringify(viewport))
  } catch { /* ignore */ }
}
