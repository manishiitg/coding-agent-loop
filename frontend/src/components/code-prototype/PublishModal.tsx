import React, { useState } from 'react'
import { Loader2, Upload, ExternalLink, Unlink, AlertTriangle, ArrowDownToLine, ArrowUpFromLine } from 'lucide-react'
import { codePrototypeApi } from '../../api/codePrototype'
import type { PrototypeGitHub } from '../../stores/useCodePrototypeStore'

interface Props {
  projectName: string
  github: PrototypeGitHub | undefined
  onClose: () => void
  onConnected: () => void
  onDisconnected: () => void
}

export const PublishModal: React.FC<Props> = ({ projectName, github, onClose, onConnected, onDisconnected }) => {
  const [repoUrl, setRepoUrl] = useState('')
  const [pat, setPat] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [published, setPublished] = useState(false)
  // When the remote repo already has content, show a choice before first publish
  const [remoteConflict, setRemoteConflict] = useState(false)

  const handleConnect = async () => {
    if (!repoUrl.trim() || !pat.trim()) return
    setLoading(true)
    setError('')
    try {
      const result = await codePrototypeApi.githubConnect(projectName, repoUrl.trim(), pat.trim())
      // Set conflict state BEFORE onConnected to avoid race with github prop update
      if (result.has_remote_content) {
        setRemoteConflict(true)
        setLoading(false)
      }
      onConnected()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Connection failed')
      setLoading(false)
    }
  }

  const handlePublish = async () => {
    setLoading(true)
    setError('')
    try {
      await codePrototypeApi.githubPublish(projectName)
      setPublished(true)
      setRemoteConflict(false)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Publish failed')
    } finally {
      setLoading(false)
    }
  }

  const handlePullRemote = async () => {
    setLoading(true)
    setError('')
    try {
      await codePrototypeApi.githubPull(projectName)
      setRemoteConflict(false)
      onConnected() // refresh project files
      onClose()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Pull failed')
    } finally {
      setLoading(false)
    }
  }

  const handleDisconnect = async () => {
    setLoading(true)
    try {
      await codePrototypeApi.githubDisconnect(projectName)
      onDisconnected()
      onClose()
    } catch {
      setLoading(false)
    }
  }

  // ── Remote conflict resolution ───────────────────────────────────────────
  if (remoteConflict && github) {
    const repoDisplay = github.repo_url.replace(/^https?:\/\//, '').replace(/\.git$/, '')

    return (
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
        <div className="bg-white dark:bg-gray-800 rounded-xl shadow-xl w-full max-w-md mx-4 p-6">
          <div className="flex items-center gap-2 mb-4">
            <AlertTriangle className="w-5 h-5 text-amber-500" />
            <h2 className="text-base font-semibold text-gray-900 dark:text-gray-100">Repository has existing files</h2>
          </div>

          <p className="text-sm text-gray-600 dark:text-gray-400 mb-2">
            <a href={github.repo_url} target="_blank" rel="noreferrer" className="text-emerald-500 hover:text-emerald-400 inline-flex items-center gap-1">
              {repoDisplay} <ExternalLink className="w-3 h-3" />
            </a>
            {' '}already contains files.
          </p>
          <p className="text-sm text-gray-600 dark:text-gray-400 mb-5">
            How would you like to proceed?
          </p>

          {error && <p className="mb-3 text-xs text-red-500">{error}</p>}

          <div className="space-y-2">
            <button
              onClick={handlePullRemote}
              disabled={loading}
              className="w-full flex items-center gap-3 px-4 py-3 text-left border border-gray-200 dark:border-gray-600 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-700/70 transition-colors disabled:opacity-50"
            >
              <ArrowDownToLine className="w-5 h-5 text-blue-500 flex-shrink-0" />
              <div>
                <div className="text-sm font-medium text-gray-900 dark:text-gray-100">Pull from GitHub</div>
                <div className="text-xs text-gray-500 dark:text-gray-400">Replace local files with the repo content</div>
              </div>
              {loading && <Loader2 className="w-4 h-4 animate-spin ml-auto" />}
            </button>

            <button
              onClick={handlePublish}
              disabled={loading}
              className="w-full flex items-center gap-3 px-4 py-3 text-left border border-gray-200 dark:border-gray-600 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-700/70 transition-colors disabled:opacity-50"
            >
              <ArrowUpFromLine className="w-5 h-5 text-amber-500 flex-shrink-0" />
              <div>
                <div className="text-sm font-medium text-gray-900 dark:text-gray-100">Push local files</div>
                <div className="text-xs text-gray-500 dark:text-gray-400">Overwrite the repo with your local project files</div>
              </div>
              {loading && <Loader2 className="w-4 h-4 animate-spin ml-auto" />}
            </button>
          </div>

          <div className="flex justify-end mt-4">
            <button
              onClick={onClose}
              disabled={loading}
              className="px-4 py-2 text-sm text-gray-600 dark:text-gray-300 hover:text-gray-900 dark:hover:text-gray-100 transition-colors"
            >
              Cancel
            </button>
          </div>
        </div>
      </div>
    )
  }

  // ── Already connected ──────────────────────────────────────────────────────
  if (github) {
    const repoDisplay = github.repo_url.replace(/^https?:\/\//, '').replace(/\.git$/, '')

    if (published) {
      return (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
          <div className="bg-white dark:bg-gray-800 rounded-xl shadow-xl w-full max-w-md mx-4 p-6 text-center">
            <div className="w-12 h-12 rounded-full bg-emerald-100 dark:bg-emerald-900/40 flex items-center justify-center mx-auto mb-3">
              <Upload className="w-6 h-6 text-emerald-600 dark:text-emerald-400" />
            </div>
            <h2 className="text-base font-semibold text-gray-900 dark:text-gray-100 mb-1">Published!</h2>
            <a
              href={github.repo_url}
              target="_blank"
              rel="noreferrer"
              className="text-sm text-emerald-500 hover:text-emerald-400 inline-flex items-center gap-1"
            >
              {repoDisplay} <ExternalLink className="w-3 h-3" />
            </a>
            <button
              onClick={onClose}
              className="block w-full mt-5 px-4 py-2 text-sm font-medium bg-emerald-600 hover:bg-emerald-700 text-white rounded-lg transition-colors"
            >
              Done
            </button>
          </div>
        </div>
      )
    }

    return (
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
        <div className="bg-white dark:bg-gray-800 rounded-xl shadow-xl w-full max-w-md mx-4 p-6">
          <div className="flex items-center gap-2 mb-4">
            <Upload className="w-5 h-5 text-emerald-500" />
            <h2 className="text-base font-semibold text-gray-900 dark:text-gray-100">Publish to GitHub</h2>
          </div>

          <div className="flex items-center justify-between py-2 px-3 bg-gray-50 dark:bg-gray-700 rounded-lg mb-4">
            <a
              href={github.repo_url}
              target="_blank"
              rel="noreferrer"
              className="text-sm text-emerald-500 hover:text-emerald-400 inline-flex items-center gap-1 truncate"
            >
              {repoDisplay} <ExternalLink className="w-3 h-3 flex-shrink-0" />
            </a>
          </div>

          {error && <p className="mb-3 text-xs text-red-500">{error}</p>}

          <div className="flex justify-between items-center mt-4">
            <button
              onClick={handleDisconnect}
              disabled={loading}
              className="flex items-center gap-1 text-xs text-gray-400 hover:text-red-500 transition-colors disabled:opacity-50"
            >
              <Unlink className="w-3.5 h-3.5" /> Disconnect
            </button>
            <div className="flex gap-2">
              <button
                onClick={onClose}
                className="px-4 py-2 text-sm text-gray-600 dark:text-gray-300 hover:text-gray-900 dark:hover:text-gray-100 transition-colors"
              >
                Cancel
              </button>
              <button
                onClick={handlePublish}
                disabled={loading}
                className="flex items-center gap-1.5 px-4 py-2 text-sm font-medium bg-emerald-600 hover:bg-emerald-700 text-white rounded-lg disabled:opacity-50 transition-colors"
              >
                {loading ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Upload className="w-3.5 h-3.5" />}
                {loading ? 'Publishing…' : 'Publish now'}
              </button>
            </div>
          </div>
        </div>
      </div>
    )
  }

  // ── Not connected yet ──────────────────────────────────────────────────────
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div className="bg-white dark:bg-gray-800 rounded-xl shadow-xl w-full max-w-md mx-4 p-6">
        <div className="flex items-center gap-2 mb-4">
          <Upload className="w-5 h-5 text-emerald-500" />
          <h2 className="text-base font-semibold text-gray-900 dark:text-gray-100">Connect to GitHub</h2>
        </div>

        <div className="space-y-3">
          <div>
            <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">
              GitHub repository URL
            </label>
            <input
              type="text"
              value={repoUrl}
              onChange={e => setRepoUrl(e.target.value)}
              placeholder="https://github.com/you/my-app"
              className="w-full px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-emerald-500"
            />
          </div>
          <div>
            <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">
              Access token{' '}
              <a
                href="https://github.com/settings/tokens/new?scopes=repo"
                target="_blank"
                rel="noreferrer"
                className="text-emerald-500 hover:underline"
              >
                How to get one
              </a>
            </label>
            <input
              type="password"
              value={pat}
              onChange={e => setPat(e.target.value)}
              placeholder="ghp_..."
              className="w-full px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-emerald-500"
            />
            <p className="mt-1 text-xs text-gray-400">Stored securely, never shared</p>
          </div>
        </div>

        {error && <p className="mt-3 text-xs text-red-500">{error}</p>}

        <div className="flex justify-end gap-2 mt-5">
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm text-gray-600 dark:text-gray-300 hover:text-gray-900 dark:hover:text-gray-100 transition-colors"
          >
            Cancel
          </button>
          <button
            onClick={handleConnect}
            disabled={loading || !repoUrl.trim() || !pat.trim()}
            className="flex items-center gap-1.5 px-4 py-2 text-sm font-medium bg-emerald-600 hover:bg-emerald-700 text-white rounded-lg disabled:opacity-50 transition-colors"
          >
            {loading ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Upload className="w-3.5 h-3.5" />}
            {loading ? 'Connecting…' : 'Connect & Publish'}
          </button>
        </div>
      </div>
    </div>
  )
}
