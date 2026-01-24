import { useState } from 'react'
import { Loader2, CheckCircle, AlertCircle, Download, ExternalLink } from 'lucide-react'
import { skillsApi } from '../../api/skills'
import type { ValidateSkillResponse } from '../../types/skills'

interface SkillImportDialogProps {
  onClose: () => void
  onSuccess: () => void
}

export default function SkillImportDialog({ onClose, onSuccess }: SkillImportDialogProps) {
  const [url, setUrl] = useState('')
  const [isValidating, setIsValidating] = useState(false)
  const [isImporting, setIsImporting] = useState(false)
  const [validationResult, setValidationResult] = useState<ValidateSkillResponse | null>(null)
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
      const result = await skillsApi.validateSkill({ github_url: url.trim() })
      setValidationResult(result)
      if (!result.valid) {
        setError(result.error || 'Invalid skill')
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
      const result = await skillsApi.importSkill({ github_url: url.trim() })
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

  return (
    <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4">
      <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg p-6 shadow-xl w-full max-w-lg">
        <div className="flex items-center justify-between mb-4">
          <h3 className="text-lg font-semibold text-gray-900 dark:text-gray-100">
            Import Skill from GitHub
          </h3>
          <button
            onClick={onClose}
            className="text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
          >
            ✕
          </button>
        </div>

        <div className="space-y-4">
          {/* URL Input */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              GitHub Folder URL
            </label>
            <input
              type="text"
              value={url}
              onChange={(e) => {
                setUrl(e.target.value)
                setValidationResult(null)
                setError(null)
              }}
              placeholder="https://github.com/user/repo/tree/main/skills/my-skill"
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-purple-500 focus:border-transparent"
            />
            <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
              Paste a URL to a skill folder containing SKILL.md
            </p>
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
                  Valid skill found
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
                  {validationResult.frontmatter.allowed_tools && validationResult.frontmatter.allowed_tools.length > 0 && (
                    <p className="text-sm text-green-800 dark:text-green-200">
                      <span className="font-medium">Tools:</span> {validationResult.frontmatter.allowed_tools.join(', ')}
                    </p>
                  )}
                </div>
              )}

              {validationResult.files && validationResult.files.length > 0 && (
                <div className="pl-7">
                  <p className="text-xs text-green-600 dark:text-green-400">
                    Files: {validationResult.files.join(', ')}
                  </p>
                </div>
              )}
            </div>
          )}

          {/* Actions */}
          <div className="flex items-center justify-between pt-4 border-t border-gray-200 dark:border-gray-700">
            <a
              href="https://smithery.ai/skills"
              target="_blank"
              rel="noopener noreferrer"
              className="text-sm text-purple-600 dark:text-purple-400 hover:underline flex items-center gap-1"
            >
              <ExternalLink className="w-3 h-3" />
              Browse skills on Smithery
            </a>

            <div className="flex items-center gap-2">
              <button
                onClick={handleValidate}
                disabled={isValidating || !url.trim()}
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
                className="px-4 py-2 text-sm font-medium text-white bg-purple-600 hover:bg-purple-700 rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed flex items-center gap-2"
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
