import { useState } from 'react'
import { Loader2, CheckCircle, AlertCircle, Download, ExternalLink } from 'lucide-react'
import { subagentsApi } from '../../api/subagents'
import type { ValidateSubAgentResponse } from '../../types/subagents'

interface SubAgentImportDialogProps {
  onClose: () => void
  onSuccess: () => void
}

export default function SubAgentImportDialog({ onClose, onSuccess }: SubAgentImportDialogProps) {
  const [url, setUrl] = useState('')
  const [isPrivate, setIsPrivate] = useState(false)
  const [pat, setPat] = useState('')
  const [isValidating, setIsValidating] = useState(false)
  const [isImporting, setIsImporting] = useState(false)
  const [validationResult, setValidationResult] = useState<ValidateSubAgentResponse | null>(null)
  const [error, setError] = useState<string | null>(null)

  const handleValidate = async () => {
    if (!url.trim()) {
      setError('Please enter a GitHub URL')
      return
    }

    setIsValidating(true)
    setError(null)
    setValidationResult(null)

    try {
      const result = await subagentsApi.validateSubAgent({ github_url: url.trim(), ...(pat ? { github_token: pat } : {}) })
      setValidationResult(result)
      if (!result.valid) {
        setError(result.error || 'Invalid sub-agent')
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Validation failed')
    } finally {
      setIsValidating(false)
    }
  }

  const handleImport = async () => {
    if (!url.trim()) {
      setError('Please enter a GitHub URL')
      return
    }

    setIsImporting(true)
    setError(null)

    try {
      const result = await subagentsApi.importSubAgent({ github_url: url.trim(), ...(pat ? { github_token: pat } : {}) })
      if (result.success) {
        onSuccess()
      } else {
        setError(result.error || 'Import failed')
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Import failed')
    } finally {
      setIsImporting(false)
    }
  }

  const canValidate = url.trim() !== ''

  return (
    <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4">
      <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg p-6 shadow-xl w-full max-w-lg">
        <div className="flex items-center justify-between mb-4">
          <h3 className="text-lg font-semibold text-gray-900 dark:text-gray-100">
            Import Sub-Agent
          </h3>
          <button
            onClick={onClose}
            className="text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
          >
            ✕
          </button>
        </div>

        <div className="space-y-4">
          {/* GitHub URL Input */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              GitHub File URL
            </label>
            <input
              type="text"
              value={url}
              onChange={(e) => {
                setUrl(e.target.value)
                setValidationResult(null)
                setError(null)
              }}
              placeholder="https://github.com/user/repo/blob/main/subagents/my-agent.md"
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
            />
            <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
              Paste a URL to a sub-agent file (must contain YAML frontmatter).
            </p>

            <label className="flex items-center gap-2 mt-3 cursor-pointer">
              <input
                type="checkbox"
                checked={isPrivate}
                onChange={(e) => {
                  setIsPrivate(e.target.checked)
                  if (!e.target.checked) setPat('')
                }}
                className="w-4 h-4 rounded border-gray-300 dark:border-gray-600 text-indigo-600 focus:ring-indigo-500"
              />
              <span className="text-sm text-gray-700 dark:text-gray-300">Private repository</span>
            </label>

            {isPrivate && (
              <div className="mt-2">
                <input
                  type="password"
                  value={pat}
                  onChange={(e) => setPat(e.target.value)}
                  placeholder="ghp_..."
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
                />
                <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
                  Personal Access Token with repo scope. Your token is only used for this request and is not stored anywhere.
                </p>
              </div>
            )}
          </div>

          {/* Error Message */}
          {error && (
            <div className="flex items-center gap-2 p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-md">
              <AlertCircle className="w-5 h-5 text-red-500 flex-shrink-0" />
              <span className="text-sm text-red-700 dark:text-red-300">{error}</span>
            </div>
          )}

          {/* Validation Result */}
          {validationResult && validationResult.valid && (
            <div className="p-3 bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-800 rounded-md space-y-2">
              <div className="flex items-center gap-2">
                <CheckCircle className="w-5 h-5 text-green-500" />
                <span className="text-sm font-medium text-green-700 dark:text-green-300">
                  Valid sub-agent found
                </span>
              </div>

              {validationResult.frontmatter && (
                <div className="space-y-1 pl-7">
                  <p className="text-sm text-green-800 dark:text-green-200">
                    <span className="font-medium">Name:</span> {validationResult.frontmatter.name}
                  </p>
                  <p className="text-sm text-green-800 dark:text-green-200">
                    <span className="font-medium">Description:</span> {validationResult.frontmatter.description}
                  </p>
                </div>
              )}

              {validationResult.exists && (
                <div className="flex items-center gap-2 p-2 bg-amber-50 dark:bg-amber-900/30 border border-amber-200 dark:border-amber-800 rounded-md">
                  <AlertCircle className="w-4 h-4 text-amber-500 flex-shrink-0" />
                  <span className="text-sm text-amber-700 dark:text-amber-300">
                    A sub-agent with this name already exists. Importing will overwrite it.
                  </span>
                </div>
              )}
            </div>
          )}

          {/* Actions */}
          <div className="flex items-center justify-between pt-4 border-t border-gray-200 dark:border-gray-700">
            <a
              href="https://github.com/VoltAgent/awesome-claude-code-subagents"
              target="_blank"
              rel="noopener noreferrer"
              className="text-sm text-indigo-600 dark:text-indigo-400 hover:underline flex items-center gap-1"
            >
              <ExternalLink className="w-3 h-3" />
              Browse sub-agents on GitHub
            </a>

            <div className="flex items-center gap-2">
              <button
                onClick={handleValidate}
                disabled={isValidating || !canValidate}
                className="px-4 py-2 text-sm font-medium text-gray-700 dark:text-gray-300 bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed flex items-center gap-2"
              >
                {isValidating ? (
                  <>
                    <Loader2 className="w-4 h-4 animate-spin" />
                    Validating...
                  </>
                ) : (
                  'Validate'
                )}
              </button>

              <button
                onClick={handleImport}
                disabled={isImporting || !validationResult?.valid}
                className="px-4 py-2 text-sm font-medium text-white bg-indigo-600 hover:bg-indigo-700 rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed flex items-center gap-2"
              >
                {isImporting ? (
                  <>
                    <Loader2 className="w-4 h-4 animate-spin" />
                    Importing...
                  </>
                ) : (
                  <>
                    <Download className="w-4 h-4" />
                    Import
                  </>
                )}
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
