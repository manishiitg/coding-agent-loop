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
          description: `Uploaded from AgentWorks: ${fileName}`,
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
        } catch {
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
      <div className="bg-card rounded-md shadow-md border border-border w-full max-w-md overflow-hidden animate-in zoom-in-95 duration-200">
        <div className="flex items-center justify-between px-6 py-4 border-b border-border bg-muted/50">
          <div className="flex items-center gap-2 text-foreground font-semibold">
            <Github className="w-5 h-5" />
            Push to GitHub Gist
          </div>
          <button
            onClick={onClose}
            disabled={isPushing}
            className="text-muted-foreground hover:text-foreground transition-colors disabled:opacity-50"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        <div className="p-6">
          {gistUrl ? (
            <div className="space-y-4">
              <div className="flex items-center justify-center w-12 h-12 rounded-full bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 mx-auto mb-4">
                <Check className="w-6 h-6" />
              </div>
              <h3 className="text-center text-sm font-medium text-foreground">
                Gist Created Successfully!
              </h3>
              
              <div className="flex items-center gap-2 p-3 bg-muted rounded-md border border-border">
                <input 
                  type="text" 
                  readOnly 
                  value={gistUrl}
                  className="bg-transparent flex-1 outline-none text-sm text-muted-foreground min-w-0"
                />
                <button
                  onClick={handleCopyUrl}
                  className="p-1.5 text-muted-foreground hover:text-foreground bg-card rounded border border-border shadow-sm transition-colors"
                  title="Copy URL"
                >
                  {copied ? <Check className="w-4 h-4 text-emerald-500" /> : <Copy className="w-4 h-4" />}
                </button>
                <a
                  href={gistUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="p-1.5 text-primary hover:text-primary bg-card rounded border border-border shadow-sm transition-colors"
                  title="Open in new tab"
                >
                  <ExternalLink className="w-4 h-4" />
                </a>
              </div>

              <div className="pt-4 flex justify-center">
                <button
                  onClick={onClose}
                  className="px-4 py-2 bg-secondary hover:bg-secondary/80 text-secondary-foreground rounded-md font-medium transition-colors"
                >
                  Close
                </button>
              </div>
            </div>
          ) : (
            <form onSubmit={handlePush} className="space-y-4">
              <p className="text-sm text-muted-foreground">
                Push <span className="font-mono text-xs bg-muted px-1 py-0.5 rounded">{fileName}</span> to GitHub Gist.
              </p>

              <div className="flex flex-col gap-3 p-3 bg-muted/50 rounded-md border border-border">
                <label className="text-sm font-medium text-foreground">Visibility</label>
                <div className="flex rounded-md bg-muted p-1">
                  <button
                    type="button"
                    onClick={() => setIsPublic(false)}
                    className={`flex-1 flex items-center justify-center gap-2 py-1.5 text-xs font-medium rounded-md transition-all ${
                      !isPublic 
                        ? 'bg-card text-foreground shadow-sm' 
                        : 'text-muted-foreground hover:text-foreground'
                    }`}
                  >
                    Secret
                  </button>
                  <button
                    type="button"
                    onClick={() => setIsPublic(true)}
                    className={`flex-1 flex items-center justify-center gap-2 py-1.5 text-xs font-medium rounded-md transition-all ${
                      isPublic 
                        ? 'bg-card text-foreground shadow-sm' 
                        : 'text-muted-foreground hover:text-foreground'
                    }`}
                  >
                    Public
                  </button>
                </div>
                <p className="text-xs text-muted-foreground">
                  {isPublic 
                    ? 'Public gists are searchable and appear in your GitHub profile.' 
                    : 'Secret gists are not searchable but can be viewed by anyone with the URL.'}
                </p>
              </div>

              <div className="space-y-1.5">
                <label className="text-sm font-medium text-foreground flex items-center gap-1.5">
                  <Key className="w-4 h-4" />
                  GitHub Personal Access Token
                </label>
                <input
                  type="password"
                  value={pat}
                  onChange={(e) => setPat(e.target.value)}
                  placeholder="ghp_..."
                  className="w-full px-3 py-2 border border-input bg-transparent rounded-md text-sm text-foreground placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                  disabled={isPushing}
                  required
                />
                <p className="text-xs text-muted-foreground">
                  Requires <code className="bg-muted px-1 rounded">gist</code> scope. Your token will be saved locally in your browser.
                </p>
              </div>

              {error && (
                <div className="rounded-md border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive">
                  {error}
                </div>
              )}

              <div className="pt-4 flex items-center justify-end gap-3">
                <button
                  type="button"
                  onClick={onClose}
                  disabled={isPushing}
                  className="px-4 py-2 text-sm font-medium text-muted-foreground hover:text-foreground hover:bg-muted rounded-md transition-colors disabled:opacity-50"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={isPushing || !pat.trim()}
                  className="flex items-center gap-2 px-4 py-2 text-sm font-medium text-primary-foreground bg-primary hover:bg-primary/90 rounded-md transition-colors disabled:opacity-50"
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
