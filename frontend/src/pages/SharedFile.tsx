import { useEffect, useState } from 'react'
import { Loader2, FileText, ArrowLeft, LogIn } from 'lucide-react'
import { MarkdownRenderer } from '../components/ui/MarkdownRenderer'
import { CsvRenderer } from '../components/ui/CsvRenderer'
import { getApiBaseUrl, getAuthToken } from '../services/api'
import { isValidJSON } from '../utils/event-helpers'

interface SharedFileProps {
  encodedPath: string
  uid?: string
  onBack?: () => void
}

const getCodeFileLanguage = (filepath: string): string | null => {
  const ext = filepath.toLowerCase().split('.').pop() || ''
  const codeExtensions: Record<string, string> = {
    'go': 'go', 'py': 'python', 'ts': 'typescript', 'tsx': 'typescript',
    'js': 'javascript', 'jsx': 'javascript', 'java': 'java', 'c': 'c',
    'cpp': 'cpp', 'cs': 'csharp', 'php': 'php', 'rb': 'ruby', 'sql': 'sql',
    'html': 'html', 'css': 'css', 'scss': 'scss', 'sh': 'shell',
    'bash': 'shell', 'yaml': 'yaml', 'yml': 'yaml', 'xml': 'xml',
    'rs': 'rust', 'swift': 'swift', 'kt': 'kotlin', 'dockerfile': 'dockerfile',
  }
  return codeExtensions[ext] || null
}

export function SharedFile({ encodedPath, uid, onBack }: SharedFileProps) {
  const [content, setContent] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [needsAuth, setNeedsAuth] = useState(false)

  // Decode the base64 path
  const filePath = (() => {
    try {
      return decodeURIComponent(escape(atob(encodedPath)))
    } catch {
      return null
    }
  })()

  const fileName = filePath?.split('/').pop() || 'Unknown file'

  useEffect(() => {
    if (!filePath) {
      setError('Invalid file path encoding')
      setLoading(false)
      return
    }

    const loadFile = async () => {
      try {
        setLoading(true)
        setError(null)
        const base = getApiBaseUrl() || ''
        const headers: Record<string, string> = {}
        const token = getAuthToken()
        if (token) headers['Authorization'] = `Bearer ${token}`
        const uidParam = uid ? `&uid=${encodeURIComponent(uid)}` : ''
        const resp = await fetch(`${base}/api/public/file?path=${encodedPath}${uidParam}`, { headers })
        if (resp.status === 401) {
          setNeedsAuth(true)
          return
        }
        if (!resp.ok) {
          throw new Error(resp.status === 404 ? 'File not found' : `Failed to load file (${resp.status})`)
        }
        const text = await resp.text()
        setContent(text)
      } catch (err) {
        console.error('Failed to load shared file:', err)
        setError(err instanceof Error ? err.message : 'Failed to load file')
      } finally {
        setLoading(false)
      }
    }

    loadFile()
  }, [encodedPath, filePath])

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-100 dark:bg-gray-900">
        <div className="text-center">
          <Loader2 className="h-8 w-8 animate-spin mx-auto text-blue-500" />
          <p className="mt-4 text-gray-600 dark:text-gray-400">Loading file...</p>
        </div>
      </div>
    )
  }

  if (needsAuth) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-100 dark:bg-gray-900">
        <div className="max-w-md w-full p-8 bg-white dark:bg-gray-800 rounded-lg shadow-lg text-center">
          <LogIn className="h-12 w-12 mx-auto mb-4 text-blue-500" />
          <h2 className="text-xl font-bold text-gray-900 dark:text-white mb-2">Login Required</h2>
          <p className="text-gray-600 dark:text-gray-400 mb-6">
            You need to be logged in to view this shared file.
          </p>
          <button
            onClick={() => { window.location.href = '/' }}
            className="px-6 py-2 bg-blue-600 hover:bg-blue-700 text-white rounded-lg font-medium transition-colors"
          >
            Go to Login
          </button>
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-100 dark:bg-gray-900">
        <div className="max-w-md w-full p-8 bg-white dark:bg-gray-800 rounded-lg shadow-lg text-center">
          <div className="text-red-500 dark:text-red-400 text-6xl mb-4">!</div>
          <h2 className="text-xl font-bold text-gray-900 dark:text-white mb-2">File Not Found</h2>
          <p className="text-gray-600 dark:text-gray-400 mb-4">{error}</p>
          {onBack && (
            <button onClick={onBack} className="text-blue-600 dark:text-blue-400 hover:underline">
              Go back
            </button>
          )}
        </div>
      </div>
    )
  }

  if (content === null) return null

  const lowerPath = (filePath || '').toLowerCase()
  const language = getCodeFileLanguage(filePath || '')

  const renderContent = () => {
    if (lowerPath.endsWith('.csv')) {
      return <CsvRenderer content={content} />
    }

    if (lowerPath.endsWith('.json') || isValidJSON(content)) {
      return (
        <div className="bg-gray-50 dark:bg-gray-900 border border-gray-200 dark:border-gray-700 rounded-lg p-4">
          <pre className="text-sm font-mono text-gray-800 dark:text-gray-200 overflow-x-auto whitespace-pre-wrap break-words leading-relaxed">
            {content}
          </pre>
        </div>
      )
    }

    if (language) {
      const codeBlockContent = `\`\`\`${language}\n${content}\n\`\`\``
      return (
        <div className="prose prose-sm max-w-none dark:prose-invert">
          <MarkdownRenderer content={codeBlockContent} className="max-w-none" showScrollbar={true} />
        </div>
      )
    }

    // Default: render as markdown
    return (
      <div className="prose prose-sm max-w-none dark:prose-invert prose-headings:font-semibold prose-headings:text-gray-900 dark:prose-headings:text-gray-100 prose-p:text-gray-700 dark:prose-p:text-gray-300 prose-a:text-blue-600 dark:prose-a:text-blue-400 prose-a:no-underline hover:prose-a:underline prose-code:text-blue-600 dark:prose-code:text-blue-400 prose-pre:bg-gray-50 dark:prose-pre:bg-gray-900">
        <MarkdownRenderer content={content} className="max-w-none" showScrollbar={true} />
      </div>
    )
  }

  return (
    <div className="min-h-screen bg-gray-100 dark:bg-gray-900">
      {/* Header */}
      <div className="bg-white dark:bg-gray-800 shadow sticky top-0 z-10">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center space-x-3">
              {onBack && (
                <button
                  onClick={onBack}
                  className="p-1.5 rounded-md text-gray-500 hover:text-gray-900 dark:hover:text-gray-100 hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors"
                  title="Back to folder"
                >
                  <ArrowLeft className="h-5 w-5" />
                </button>
              )}
              <FileText className="h-5 w-5 text-blue-500 flex-shrink-0" />
              <div className="min-w-0">
                <h1 className="text-lg font-semibold text-gray-900 dark:text-white truncate">{fileName}</h1>
                <p className="text-xs text-gray-500 dark:text-gray-400 truncate">{filePath}</p>
              </div>
            </div>
            <span className="bg-yellow-100 dark:bg-yellow-900/30 text-yellow-800 dark:text-yellow-400 px-2 py-0.5 rounded text-xs font-medium flex-shrink-0">
              Read-only
            </span>
          </div>
        </div>
      </div>

      {/* Content */}
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6 pb-16">
        <div className="bg-white dark:bg-gray-800 rounded-lg shadow overflow-hidden p-6">
          {renderContent()}
        </div>
      </div>
    </div>
  )
}
