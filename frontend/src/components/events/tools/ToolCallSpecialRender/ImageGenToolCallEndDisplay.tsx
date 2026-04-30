import React from 'react'
import type { ToolCallEndEvent } from '../../../../generated/events'
import { getApiBaseUrl } from '../../../../services/api'
import { ImagePlus, Download } from 'lucide-react'

interface ImageGenToolCallEndDisplayProps {
  event: ToolCallEndEvent
}

interface ImageGenResult {
  model: string
  cost_per_image?: number | null
  cost_note?: string
  prompt: string
  saved_paths?: string[]
  count: number
}

// Build a URL to serve a workspace file via the backend proxy.
// Uses /api/public/file?path=<base64> — auth context provides user ID server-side.
function workspaceFileUrl(workspacePath: string): string {
  const base = getApiBaseUrl()
  const encoded = btoa(unescape(encodeURIComponent(workspacePath)))
  return `${base}/api/public/file?path=${encoded}`
}

async function downloadWorkspaceFile(url: string, filename: string) {
  try {
    const res = await fetch(url)
    const blob = await res.blob()
    const blobUrl = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = blobUrl
    a.download = filename
    a.click()
    URL.revokeObjectURL(blobUrl)
  } catch {
    // fallback: open in new tab
    window.open(url, '_blank')
  }
}

function formatImageCost(data: ImageGenResult): string {
  if (typeof data.cost_per_image === 'number') {
    return `$${data.cost_per_image.toFixed(2)}/image`
  }
  return data.cost_note || 'Cost unavailable'
}

export const ImageGenToolCallEndDisplay: React.FC<ImageGenToolCallEndDisplayProps> = ({ event }) => {
  if (!event.result) return null

  let data: ImageGenResult | null = null
  try {
    const parsed = JSON.parse(event.result)
    if (
      typeof parsed === 'object' &&
      typeof parsed.count === 'number' &&
      Array.isArray(parsed.saved_paths)
    ) {
      data = parsed as ImageGenResult
    }
  } catch {
    return null
  }

  if (!data) return null

  const hasSavedPaths = data.saved_paths && data.saved_paths.length > 0
  return (
    <div className="bg-purple-50 dark:bg-purple-900/20 border border-purple-200 dark:border-purple-800 rounded p-3 space-y-3">
      {/* Header */}
      <div className="flex items-center gap-2 text-sm font-medium text-purple-700 dark:text-purple-300">
        <ImagePlus className="w-4 h-4 flex-shrink-0" />
        <span>
          Generated {data.count} image{data.count !== 1 ? 's' : ''} · {data.model} · {formatImageCost(data)}
        </span>
      </div>

      {/* Images from workspace saved paths */}
      {hasSavedPaths && (
        <div className="flex flex-wrap gap-3">
          {data.saved_paths!.map((path, i) => (
            <div key={i} className="relative group">
              <img
                src={workspaceFileUrl(path)}
                alt={`Generated image ${i + 1}`}
                className="max-w-sm max-h-72 rounded border border-purple-200 dark:border-purple-700 object-contain bg-white dark:bg-gray-900"
                onError={(e) => {
                  // If image fails to load, hide it gracefully
                  const el = e.currentTarget as HTMLImageElement
                  el.style.display = 'none'
                }}
              />
              <button
                onClick={() => downloadWorkspaceFile(workspaceFileUrl(path), path.split('/').pop() ?? 'image.png')}
                className="absolute bottom-1 right-1 flex items-center gap-1 bg-black/70 hover:bg-black/90 text-white text-xs px-2 py-1 rounded opacity-0 group-hover:opacity-100 transition-opacity"
              >
                <Download className="w-3 h-3" />
                Save
              </button>
            </div>
          ))}
        </div>
      )}

      {/* Prompt */}
      <p className="text-xs text-purple-600 dark:text-purple-400 italic">"{data.prompt}"</p>

      {/* Saved paths (compact) */}
      {hasSavedPaths && (
        <p className="text-xs text-gray-500 dark:text-gray-400">
          Saved to workspace: {data.saved_paths!.join(', ')}
        </p>
      )}
    </div>
  )
}
