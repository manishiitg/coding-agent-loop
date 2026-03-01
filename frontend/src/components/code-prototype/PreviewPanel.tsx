import React, { useState, useEffect } from 'react'
import { RefreshCw, X, StopCircle, Loader2, Terminal } from 'lucide-react'
import { useCodePrototypeStore } from '../../stores/useCodePrototypeStore'
import { useAppStore } from '../../stores/useAppStore'
import { getApiBaseUrl } from '../../services/api'
import { codePrototypeApi } from '../../api/codePrototype'

export const PreviewPanel: React.FC = () => {
  const { previewUrl, setPreviewUrl, setShowPreview, setShowLogs, showLogs, currentProject } = useCodePrototypeStore()
  const { setWorkspaceMinimized } = useAppStore()
  const apiBase = getApiBaseUrl()
  const proxyUrl = currentProject ? `${apiBase}/api/code-prototype/preview/${currentProject.name}/` : ''
  const [urlInput, setUrlInput] = useState(proxyUrl || previewUrl || '')
  const [reloadKey, setReloadKey] = useState(0)
  const [isStopping, setIsStopping] = useState(false)

  const handleStop = async () => {
    if (isStopping) return
    setIsStopping(true)
    try { await codePrototypeApi.stopDevServers() }
    catch (e) { console.error('[PREVIEW] stop error:', e) }
    finally { setIsStopping(false) }
  }

  const handleToggleLogs = () => {
    const next = !showLogs
    setShowLogs(next)
    if (next) setWorkspaceMinimized(true)
  }

  console.log('[PREVIEW] PreviewPanel mount — project:', currentProject?.name ?? 'none', 'proxyUrl:', proxyUrl)

  // When project changes, reset to the proxy URL for that project
  useEffect(() => {
    if (proxyUrl) {
      console.log('[PREVIEW] project changed → resetting to proxy URL:', proxyUrl)
      setUrlInput(proxyUrl)
      setPreviewUrl(proxyUrl)
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentProject?.name])

  // When a deploy succeeds, the store sets a new previewUrl — sync it
  useEffect(() => {
    if (previewUrl && previewUrl !== proxyUrl) {
      console.log('[PREVIEW] deploy URL updated:', previewUrl)
      setUrlInput(previewUrl)
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [previewUrl])

  const navigate = (e?: React.FormEvent) => {
    e?.preventDefault()
    const url = urlInput.trim()
    if (!url) return
    setPreviewUrl(url)
    setReloadKey(k => k + 1)
  }

  return (
    <div className="flex flex-col h-full border-l border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-900 min-w-0">
      {/* URL bar */}
      <div className="flex items-center gap-1 px-2 py-1.5 border-b border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800 flex-shrink-0">
        <form onSubmit={navigate} className="flex-1 flex items-center gap-1 min-w-0">
          <input
            value={urlInput}
            onChange={e => setUrlInput(e.target.value)}
            className="flex-1 min-w-0 text-xs bg-white dark:bg-gray-700 border border-gray-300 dark:border-gray-600 rounded px-2 py-1 text-gray-800 dark:text-gray-200 font-mono"
            placeholder="/api/code-prototype/preview/{project}/"
          />
          <button
            type="submit"
            className="p-1 text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 flex-shrink-0"
            title="Reload"
          >
            <RefreshCw className="w-3.5 h-3.5" />
          </button>
        </form>

        <button
          onClick={handleToggleLogs}
          className={`p-1 flex-shrink-0 rounded transition-colors ${
            showLogs
              ? 'text-emerald-600 dark:text-emerald-400 bg-emerald-50 dark:bg-emerald-900/40'
              : 'text-gray-400 hover:text-gray-700 dark:hover:text-gray-200'
          }`}
          title="Toggle logs"
        >
          <Terminal className="w-3.5 h-3.5" />
        </button>

        <button
          onClick={handleStop}
          disabled={isStopping}
          className="p-1 text-gray-400 hover:text-red-500 flex-shrink-0 disabled:opacity-40"
          title="Stop dev server"
        >
          {isStopping ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <StopCircle className="w-3.5 h-3.5" />}
        </button>

        <button
          onClick={() => setShowPreview(false)}
          className="p-1 text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 flex-shrink-0"
          title="Close preview"
        >
          <X className="w-3.5 h-3.5" />
        </button>
      </div>

      {/* iframe */}
      <div className="flex-1 min-h-0">
        {previewUrl ? (
          <iframe
            key={reloadKey}
            src={previewUrl}
            className="w-full h-full border-0"
            title="Preview"
          />
        ) : (
          <div className="flex flex-col items-center justify-center h-full gap-2 text-gray-400">
            <span className="text-sm">Enter a URL and press reload to preview</span>
            <button
              onClick={() => navigate()}
              className="text-xs px-3 py-1.5 bg-emerald-600 hover:bg-emerald-700 text-white rounded-md"
            >
              Load {urlInput || 'preview URL'}
            </button>
          </div>
        )}
      </div>
    </div>
  )
}
