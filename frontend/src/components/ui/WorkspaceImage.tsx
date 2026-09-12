import { useEffect, useState } from 'react'
import { getApiBaseUrl, getAuthToken } from '../../services/api'

// <img> cannot attach the app's Bearer header. Fetch privately, then display a
// revocable Blob URL instead of putting an account token into the image URL.
export function WorkspaceImage({ path, alt, className }: { path: string; alt?: string; className?: string }) {
  const [src, setSrc] = useState<string>()
  useEffect(() => {
    const controller = new AbortController()
    let objectURL: string | undefined
    setSrc(undefined)
    const bytes = new TextEncoder().encode(path)
    let raw = ''; for (const byte of bytes) raw += String.fromCharCode(byte)
    const query = new URLSearchParams({ path: btoa(raw) })
    const token = getAuthToken()
    void fetch(`${getApiBaseUrl()}/api/public/file?${query}`, { headers: token ? { Authorization: `Bearer ${token}` } : {}, signal: controller.signal })
      .then(async response => { if (!response.ok) throw new Error('Image unavailable'); return response.blob() })
      .then(blob => { if (controller.signal.aborted) return; objectURL = URL.createObjectURL(blob); setSrc(objectURL) })
      .catch(() => {})
    return () => { controller.abort(); if (objectURL) URL.revokeObjectURL(objectURL) }
  }, [path])
  return src ? <img src={src} alt={alt} className={className} /> : <span className="text-sm text-muted-foreground">{alt || 'Workspace image'}</span>
}
