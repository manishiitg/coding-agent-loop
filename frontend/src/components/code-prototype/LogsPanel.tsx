import React, { useEffect, useRef, useState, useCallback } from 'react'
import { Terminal, X, Trash2, RefreshCw } from 'lucide-react'
import { useCodePrototypeStore } from '../../stores/useCodePrototypeStore'
import { getApiBaseUrl } from '../../services/api'

type StreamStatus = 'connecting' | 'streaming' | 'stopped' | 'not-started' | 'error'

export const LogsPanel: React.FC = () => {
  const { currentProject, setShowLogs } = useCodePrototypeStore()
  const [lines, setLines] = useState<string[]>([])
  const [status, setStatus] = useState<StreamStatus>('connecting')
  const bottomRef = useRef<HTMLDivElement>(null)
  const esRef = useRef<EventSource | null>(null)

  const connect = useCallback(() => {
    if (!currentProject) return
    esRef.current?.close()
    setLines([])
    setStatus('connecting')

    const url = `${getApiBaseUrl()}/api/code-prototype/projects/${encodeURIComponent(currentProject.name)}/logs/stream`
    console.log('[LOGS] connecting:', url)

    const es = new EventSource(url, { withCredentials: true })
    esRef.current = es

    es.onopen = () => {
      console.log('[LOGS] stream open')
      setStatus('streaming')
    }

    es.addEventListener('log', (e: MessageEvent) => {
      try {
        const { line } = JSON.parse(e.data) as { line: string }
        setLines(prev => [...prev.slice(-2000), line])
      } catch {}
    })

    es.addEventListener('done', (e: MessageEvent) => {
      try {
        const { message } = JSON.parse(e.data) as { message: string }
        if (message === 'not-started') {
          setStatus('not-started' as StreamStatus)
          es.close()
          return
        }
      } catch {}
      console.log('[LOGS] container stopped')
      setStatus('stopped')
      es.close()
    })

    es.addEventListener('error', (e: MessageEvent) => {
      try {
        const { error } = JSON.parse(e.data) as { error: string }
        setLines(prev => [...prev, `[error] ${error}`])
      } catch {}
      setStatus('error')
      es.close()
    })

    es.onerror = () => {
      if (es.readyState === EventSource.CLOSED) {
        setStatus(s => s === 'streaming' ? 'error' : s)
      }
    }
  }, [currentProject?.name]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    connect()
    return () => esRef.current?.close()
  }, [connect])

  // Auto-scroll to bottom on new lines
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'instant' })
  }, [lines])

  const statusDot: Record<StreamStatus, string> = {
    connecting:  'bg-yellow-400',
    streaming:   'bg-emerald-400',
    stopped:     'bg-gray-500',
    'not-started': 'bg-gray-500',
    error:       'bg-red-400',
  }

  return (
    <div className="flex flex-col h-full border-l border-gray-200 dark:border-gray-700 bg-gray-950 min-w-0">
      {/* toolbar */}
      <div className="flex items-center gap-1.5 px-2 py-1.5 border-b border-gray-800 bg-gray-900 flex-shrink-0">
        <Terminal className="w-3.5 h-3.5 text-emerald-400 flex-shrink-0" />
        <span className="text-xs text-gray-400 font-mono flex-1 truncate min-w-0">
          {currentProject ? `prototype-…-${currentProject.name}` : 'no project'}
        </span>
        <span className={`w-1.5 h-1.5 rounded-full flex-shrink-0 ${statusDot[status]}`} title={status} />
        <button
          onClick={connect}
          className="p-1 text-gray-500 hover:text-gray-200 transition-colors flex-shrink-0"
          title="Reconnect"
        >
          <RefreshCw className="w-3 h-3" />
        </button>
        <button
          onClick={() => setLines([])}
          className="p-1 text-gray-500 hover:text-gray-200 transition-colors flex-shrink-0"
          title="Clear"
        >
          <Trash2 className="w-3 h-3" />
        </button>
        <button
          onClick={() => setShowLogs(false)}
          className="p-1 text-gray-500 hover:text-gray-200 transition-colors flex-shrink-0"
          title="Close logs"
        >
          <X className="w-3 h-3" />
        </button>
      </div>

      {/* log output */}
      <div className="flex-1 min-h-0 overflow-y-auto font-mono text-xs text-gray-300 p-2 leading-5">
        {lines.length === 0 && status === 'connecting' && (
          <span className="text-gray-600">Connecting to container logs…</span>
        )}
        {status === 'not-started' && (
          <span className="text-gray-500">Dev server not started yet. Ask the agent to start it, or run the dev command manually.</span>
        )}
        {lines.length === 0 && status === 'error' && (
          <span className="text-red-500">Container not running or not found.</span>
        )}
        {lines.map((line, i) => (
          <div key={i} className="whitespace-pre-wrap break-all">{line}</div>
        ))}
        {status === 'stopped' && (
          <div className="text-gray-600 mt-1 italic">— container stopped —</div>
        )}
        <div ref={bottomRef} />
      </div>
    </div>
  )
}
