import { useState, useEffect } from 'react'
import mammoth from 'mammoth'

interface DocxRendererProps {
  data: ArrayBuffer
}

export function DocxRenderer({ data }: DocxRendererProps) {
  const [html, setHtml] = useState<string | null>(null)
  const [warnings, setWarnings] = useState<string[]>([])
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancelled = false

    async function convert() {
      try {
        setLoading(true)
        const result = await mammoth.convertToHtml({ arrayBuffer: data })
        if (cancelled) return
        setHtml(result.value)
        setWarnings(result.messages.map(m => m.message))
      } catch (err) {
        if (cancelled) return
        console.error('Failed to convert DOCX:', err)
        setError(err instanceof Error ? err.message : 'Failed to convert document')
      } finally {
        if (!cancelled) setLoading(false)
      }
    }

    convert()
    return () => { cancelled = true }
  }, [data])

  if (loading) {
    return (
      <div className="flex items-center gap-2 text-sm text-gray-500 dark:text-gray-400 py-8 justify-center">
        <div className="animate-spin h-4 w-4 border-2 border-gray-300 border-t-blue-500 rounded-full" />
        Converting document...
      </div>
    )
  }

  if (error) {
    return (
      <p className="text-red-500 dark:text-red-400 text-sm">
        Failed to render DOCX: {error}
      </p>
    )
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2 text-sm text-gray-600 dark:text-gray-400">
        <span className="font-medium">DOCX Document</span>
        {warnings.length > 0 && (
          <span className="text-xs bg-yellow-100 dark:bg-yellow-900 text-yellow-800 dark:text-yellow-200 px-2 py-0.5 rounded">
            {warnings.length} warning{warnings.length !== 1 ? 's' : ''}
          </span>
        )}
      </div>

      {warnings.length > 0 && (
        <details className="text-xs text-yellow-700 dark:text-yellow-300">
          <summary className="cursor-pointer hover:underline">Show conversion warnings</summary>
          <ul className="mt-1 ml-4 list-disc space-y-0.5">
            {warnings.map((w, i) => (
              <li key={i}>{w}</li>
            ))}
          </ul>
        </details>
      )}

      <div className="bg-white dark:bg-gray-900 border border-gray-200 dark:border-gray-700 rounded-lg p-6">
        <div
          className="prose prose-sm max-w-none dark:prose-invert prose-headings:font-semibold prose-headings:text-gray-900 dark:prose-headings:text-gray-100 prose-p:text-gray-700 dark:prose-p:text-gray-300 prose-a:text-blue-600 dark:prose-a:text-blue-400 prose-strong:text-gray-900 dark:prose-strong:text-gray-100"
          dangerouslySetInnerHTML={{ __html: html || '' }}
        />
      </div>
    </div>
  )
}
