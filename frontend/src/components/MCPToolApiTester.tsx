import { useState } from 'react'
import { Play, Copy, Check, AlertCircle, Loader2 } from 'lucide-react'
import { MarkdownRenderer } from './ui/MarkdownRenderer'
import { getApiBaseUrl, getAuthToken } from '../services/api'

interface MCPToolApiTesterProps {
  isOpen: boolean
  onClose: () => void
  serverName: string
  toolName: string
  toolDetail?: {
    description: string
    parameters?: Record<string, {
      description?: string
      type?: string
    }>
    required?: string[]
  }
}

export default function MCPToolApiTester({
  isOpen,
  onClose,
  serverName,
  toolName,
  toolDetail
}: MCPToolApiTesterProps) {
  const [args, setArgs] = useState('{}')
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const [response, setResponse] = useState<any>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  // Generate example args from tool parameters
  const generateExampleArgs = () => {
    if (!toolDetail?.parameters) return '{}'

    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const example: Record<string, any> = {}
    Object.entries(toolDetail.parameters).forEach(([name, info]) => {
      if (info.type === 'string') {
        example[name] = `example_${name}`
      } else if (info.type === 'number' || info.type === 'integer') {
        example[name] = 0
      } else if (info.type === 'boolean') {
        example[name] = false
      } else if (info.type === 'array') {
        example[name] = []
      } else if (info.type === 'object') {
        example[name] = {}
      } else {
        example[name] = null
      }
    })

    return JSON.stringify(example, null, 2)
  }

  const apiBaseUrl = getApiBaseUrl()

  const executeTool = async () => {
    setLoading(true)
    setError(null)
    setResponse(null)

    try {
      const parsedArgs = JSON.parse(args)

      const requestBody = {
        server: serverName,
        tool: toolName,
        args: parsedArgs
      }

      console.log('[MCPToolApiTester] Executing tool:', requestBody)

      const headers: HeadersInit = { 'Content-Type': 'application/json' }
      const token = getAuthToken()
      if (token) {
        headers['Authorization'] = `Bearer ${token}`
      }

      const res = await fetch(`${apiBaseUrl}/api/mcp/execute`, {
        method: 'POST',
        headers,
        body: JSON.stringify(requestBody)
      })

      console.log('[MCPToolApiTester] Response status:', res.status, res.statusText)

      // Check if response is ok (2xx status codes)
      if (!res.ok) {
        const errorText = await res.text()
        console.error('[MCPToolApiTester] HTTP Error:', res.status, errorText)
        setError(`HTTP ${res.status}: ${errorText || res.statusText}`)
        return
      }

      const data = await res.json()
      console.log('[MCPToolApiTester] Response data:', data)
      setResponse(data)
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    } catch (err: any) {
      console.error('[MCPToolApiTester] Error:', err)
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }

  const copyCurlCommand = () => {
    const curlCmd = `curl -X POST ${apiBaseUrl}/api/mcp/execute \\
  -H "Content-Type: application/json" \\
  -d '{
    "server": "${serverName}",
    "tool": "${toolName}",
    "args": ${args}
  }'`

    navigator.clipboard.writeText(curlCmd)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  if (!isOpen) return null

  return (
    <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4">
      <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-xl w-full max-w-4xl max-h-[90vh] overflow-hidden flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between p-4 border-b border-gray-200 dark:border-gray-700">
          <div>
            <h3 className="text-lg font-semibold text-gray-900 dark:text-gray-100">
              Test MCP Tool API
            </h3>
            <div className="flex items-center gap-2 mt-1">
              <span className="text-sm text-gray-500">
                {serverName}
              </span>
              <span className="text-gray-400">→</span>
              <span className="text-sm font-mono text-blue-600 dark:text-blue-400">
                {toolName}
              </span>
            </div>
          </div>
          <button
            onClick={onClose}
            className="text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
          >
            ✕
          </button>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-y-auto p-4 space-y-4">
          {/* Tool Description */}
          {toolDetail?.description && (
            <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-700 rounded-lg p-3">
              <h4 className="text-sm font-semibold text-blue-900 dark:text-blue-100 mb-2">
                Description
              </h4>
              <MarkdownRenderer
                content={toolDetail.description}
                className="text-sm text-blue-800 dark:text-blue-200"
              />
            </div>
          )}

          {/* Parameters Info */}
          {toolDetail?.parameters && (
            <div className="bg-gray-50 dark:bg-gray-900/50 border border-gray-200 dark:border-gray-700 rounded-lg p-3">
              <h4 className="text-sm font-semibold text-gray-900 dark:text-gray-100 mb-2">
                Parameters
              </h4>
              <div className="space-y-2">
                {Object.entries(toolDetail.parameters).map(([name, info]) => (
                  <div key={name} className="flex items-center gap-2 text-sm">
                    <span className="font-mono text-gray-700 dark:text-gray-300">
                      {name}
                    </span>
                    <span className="text-xs text-gray-500 bg-gray-200 dark:bg-gray-700 px-2 py-0.5 rounded">
                      {info.type || 'unknown'}
                    </span>
                    {toolDetail.required?.includes(name) && (
                      <span className="text-xs text-red-500">required</span>
                    )}
                  </div>
                ))}
              </div>
              <button
                onClick={() => setArgs(generateExampleArgs())}
                className="mt-3 text-xs text-blue-600 dark:text-blue-400 hover:underline"
              >
                Generate example args
              </button>
            </div>
          )}

          {/* Args Input */}
          <div>
            <div className="flex items-center justify-between mb-2">
              <label className="text-sm font-semibold text-gray-900 dark:text-gray-100">
                Arguments (JSON)
              </label>
              <button
                onClick={copyCurlCommand}
                className="flex items-center gap-1 px-2 py-1 text-xs text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700 rounded transition-colors"
              >
                {copied ? (
                  <>
                    <Check className="w-3 h-3" />
                    Copied!
                  </>
                ) : (
                  <>
                    <Copy className="w-3 h-3" />
                    Copy as cURL
                  </>
                )}
              </button>
            </div>
            <textarea
              value={args}
              onChange={(e) => setArgs(e.target.value)}
              className="w-full h-32 p-3 font-mono text-sm bg-gray-50 dark:bg-gray-900 border border-gray-300 dark:border-gray-600 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500"
              placeholder='{"param1": "value1"}'
            />
          </div>

          {/* Execute Button */}
          <button
            onClick={executeTool}
            disabled={loading}
            className="w-full flex items-center justify-center gap-2 px-4 py-3 bg-green-600 hover:bg-green-700 disabled:bg-gray-400 text-white font-medium rounded-lg transition-colors"
          >
            {loading ? (
              <>
                <Loader2 className="w-4 h-4 animate-spin" />
                Executing...
              </>
            ) : (
              <>
                <Play className="w-4 h-4" />
                Execute Tool
              </>
            )}
          </button>

          {/* Error Display */}
          {error && (
            <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-700 rounded-lg p-3">
              <div className="flex items-center gap-2 mb-2">
                <AlertCircle className="w-4 h-4 text-red-600 dark:text-red-400" />
                <h4 className="text-sm font-semibold text-red-900 dark:text-red-100">
                  Error
                </h4>
              </div>
              <pre className="text-sm text-red-800 dark:text-red-200 whitespace-pre-wrap">
                {error}
              </pre>
            </div>
          )}

          {/* Response Display */}
          {response && (
            <div className={`border rounded-lg p-3 ${
              response.success
                ? 'bg-green-50 dark:bg-green-900/20 border-green-200 dark:border-green-700'
                : 'bg-red-50 dark:bg-red-900/20 border-red-200 dark:border-red-700'
            }`}>
              <h4 className={`text-sm font-semibold mb-2 ${
                response.success
                  ? 'text-green-900 dark:text-green-100'
                  : 'text-red-900 dark:text-red-100'
              }`}>
                Response
              </h4>
              <pre className={`text-sm whitespace-pre-wrap font-mono ${
                response.success
                  ? 'text-green-800 dark:text-green-200'
                  : 'text-red-800 dark:text-red-200'
              }`}>
                {JSON.stringify(response, null, 2)}
              </pre>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
