import { useState, useEffect, useCallback } from 'react'
import { X, Github, ExternalLink, Check, Copy, Loader2, Key } from 'lucide-react'

interface PushToGistDialogProps {
  isOpen: boolean
  onClose: () => void
  fileContent: string
  fileName: string
}

export default function PushToGistDialog({
  isOpen,
  onClose,
  fileContent,
  fileName
}: PushToGistDialogProps) {
  const [pat, setPat] = useState('')
  const [isPublic, setIsPublic] = useState(false)
  const [isPushing, setIsPushing] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [gistUrl, setGistUrl] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  // Load PAT from local storage on mount
  useEffect(() => {
    if (isOpen) {
      const storedPat = localStorage.getItem('github_gist_pat')
      if (storedPat) {
        setPat(storedPat)
      }
      setGistUrl(null)
      setError(null)
      setCopied(false)
      setIsPublic(false) // Default to secret/private
    }
  }, [isOpen])

  const handleCopyUrl = async () => {
    if (gistUrl) {
      await navigator.clipboard.writeText(gistUrl)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }
  }

  const handlePush = async (e?: React.FormEvent) => {
    if (e) e.preventDefault()
    
    if (!pat.trim()) {
      setError('Personal Access Token is required')
      return
    }

    setIsPushing(true)
    setError(null)

    try {
      const response = await fetch('https://api.github.com/gists', {
        method: 'POST',
        headers: {
          'Authorization': `token ${pat}`,
          'Content-Type': 'application/json',
          'Accept': 'application/vnd.github.v3+json'
        },
        body: JSON.stringify({
          description: `Uploaded from AgentForge: ${fileName}`,
          public: isPublic,
          files: {
            [fileName]: {
              content: fileContent
            }
          }
        })
      })

      if (!response.ok) {
        let errorData
        try {
          errorData = await response.json()
        } catch (e) {
          // Ignore JSON parse error for error responses
        }

        if (response.status === 401) {
          localStorage.removeItem('github_gist_pat')
          throw new Error('Invalid GitHub token. Please check your token and try again.')
        }
        
        if (response.status === 404) {
          localStorage.removeItem('github_gist_pat')
          throw new Error('GitHub returned "Not Found". This almost always means your token is missing the "gist" scope. Please create a new token with "gist" checked.')
        }

        throw new Error(errorData?.message || `Failed to create Gist (HTTP ${response.status})`)
      }

      const data = await response.json()
      setGistUrl(data.html_url)
      
      // Save valid PAT for future use
      localStorage.setItem('github_gist_pat', pat)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setIsPushing(false)
    }
  }

  // Handle escape to close
  const handleKeyDown = useCallback((e: KeyboardEvent) => {
    if (e.key === 'Escape' && !isPushing) {
      onClose()
    }
  }, [isPushing, onClose])

  useEffect(() => {
    if (isOpen) {
      document.addEventListener('keydown', handleKeyDown)
    }
    return () => {
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [isOpen, handleKeyDown])

  if (!isOpen) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/50 backdrop-blur-sm animate-in fade-in duration-200">
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-md overflow-hidden animate-in zoom-in-95 duration-200">
        <div className="flex items-center justify-between px-6 py-4 border-b border-gray-100 dark:border-gray-700 bg-gray-50 dark:bg-gray-800/50">
          <div className="flex items-center gap-2 text-gray-900 dark:text-gray-100 font-semibold">
            <Github className="w-5 h-5" />
            Push to GitHub Gist
          </div>
          <button
            onClick={onClose}
            disabled={isPushing}
            className="text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 transition-colors disabled:opacity-50"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        <div className="p-6">
          {gistUrl ? (
            <div className="space-y-4">
              <div className="flex items-center justify-center w-12 h-12 rounded-full bg-green-100 dark:bg-green-900/30 text-green-600 dark:text-green-400 mx-auto mb-4">
                <Check className="w-6 h-6" />
              </div>
              <h3 className="text-center text-lg font-medium text-gray-900 dark:text-gray-100">
                Gist Created Successfully!
              </h3>
              
              <div className="flex items-center gap-2 p-3 bg-gray-50 dark:bg-gray-900 rounded-md border border-gray-200 dark:border-gray-700">
                <input 
                  type="text" 
                  readOnly 
                  value={gistUrl}
                  className="bg-transparent flex-1 outline-none text-sm text-gray-600 dark:text-gray-300 min-w-0"
                />
                <button
                  onClick={handleCopyUrl}
                  className="p-1.5 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 bg-white dark:bg-gray-800 rounded shadow-sm border border-gray-200 dark:border-gray-700 transition-colors"
                  title="Copy URL"
                >
                  {copied ? <Check className="w-4 h-4 text-green-500" /> : <Copy className="w-4 h-4" />}
                </button>
                <a
                  href={gistUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="p-1.5 text-blue-500 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300 bg-white dark:bg-gray-800 rounded shadow-sm border border-gray-200 dark:border-gray-700 transition-colors"
                  title="Open in new tab"
                >
                  <ExternalLink className="w-4 h-4" />
                </a>
              </div>

              <div className="pt-4 flex justify-center">
                <button
                  onClick={onClose}
                  className="px-4 py-2 bg-gray-100 hover:bg-gray-200 dark:bg-gray-700 dark:hover:bg-gray-600 text-gray-700 dark:text-gray-200 rounded-md font-medium transition-colors"
                >
                  Close
                </button>
              </div>
            </div>
          ) : (
            <form onSubmit={handlePush} className="space-y-4">
              <p className="text-sm text-gray-600 dark:text-gray-400">
                Push <span className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 py-0.5 rounded">{fileName}</span> to GitHub Gist.
              </p>

              <div className="flex flex-col gap-3 p-3 bg-gray-50 dark:bg-gray-900/50 rounded-md border border-gray-100 dark:border-gray-700">
                <label className="text-sm font-medium text-gray-700 dark:text-gray-300">Visibility</label>
                <div className="flex p-1 bg-gray-200 dark:bg-gray-800 rounded-lg">
                  <button
                    type="button"
                    onClick={() => setIsPublic(false)}
                    className={`flex-1 flex items-center justify-center gap-2 py-1.5 text-xs font-medium rounded-md transition-all ${
                      !isPublic 
                        ? 'bg-white dark:bg-gray-700 text-gray-900 dark:text-white shadow-sm' 
                        : 'text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300'
                    }`}
                  >
                    Secret
                  </button>
                  <button
                    type="button"
                    onClick={() => setIsPublic(true)}
                    className={`flex-1 flex items-center justify-center gap-2 py-1.5 text-xs font-medium rounded-md transition-all ${
                      isPublic 
                        ? 'bg-white dark:bg-gray-700 text-gray-900 dark:text-white shadow-sm' 
                        : 'text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300'
                    }`}
                  >
                    Public
                  </button>
                </div>
                <p className="text-[11px] text-gray-500 dark:text-gray-400">
                  {isPublic 
                    ? 'Public gists are searchable and appear in your GitHub profile.' 
                    : 'Secret gists are not searchable but can be viewed by anyone with the URL.'}
                </p>
              </div>

              <div className="space-y-1.5">
                <label className="text-sm font-medium text-gray-700 dark:text-gray-300 flex items-center gap-1.5">
                  <Key className="w-4 h-4" />
                  GitHub Personal Access Token
                </label>
                <input
                  type="password"
                  value={pat}
                  onChange={(e) => setPat(e.target.value)}
                  placeholder="ghp_..."
                  className="w-full px-3 py-2 bg-white dark:bg-gray-900 border border-gray-300 dark:border-gray-600 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500/50 dark:focus:ring-blue-400/50"
                  disabled={isPushing}
                  required
                />
                <p className="text-xs text-gray-500 dark:text-gray-400">
                  Requires <code className="bg-gray-100 dark:bg-gray-800 px-1 rounded">gist</code> scope. Your token will be saved locally in your browser.
                </p>
              </div>

              {error && (
                <div className="p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-600 dark:text-red-400">
                  {error}
                </div>
              )}

              <div className="pt-4 flex items-center justify-end gap-3">
                <button
                  type="button"
                  onClick={onClose}
                  disabled={isPushing}
                  className="px-4 py-2 text-sm font-medium text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-md transition-colors disabled:opacity-50"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={isPushing || !pat.trim()}
                  className="flex items-center gap-2 px-4 py-2 text-sm font-medium text-white bg-blue-500 hover:bg-blue-600 rounded-md transition-colors disabled:opacity-50"
                >
                  {isPushing ? (
                    <>
                      <Loader2 className="w-4 h-4 animate-spin" />
                      Pushing...
                    </>
                  ) : (
                    <>
                      <Github className="w-4 h-4" />
                      Create Gist
                    </>
                  )}
                </button>
              </div>
            </form>
          )}
        </div>
      </div>
    </div>
  )
}
