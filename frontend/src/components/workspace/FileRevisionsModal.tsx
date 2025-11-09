import { useState, useEffect, useCallback } from 'react'
import { X, Clock, User, GitCommit, Eye, RotateCcw, FileText } from 'lucide-react'
import { agentApi } from '../../services/api'
import type { FileVersion } from '../../services/api-types'
import FileEditor from './FileEditor'

interface FileRevisionsModalProps {
  isOpen: boolean
  onClose: () => void
  filepath: string
  onRestoreVersion?: (version: FileVersion) => void
}

export default function FileRevisionsModal({
  isOpen,
  onClose,
  filepath,
  onRestoreVersion
}: FileRevisionsModalProps) {
  const [versions, setVersions] = useState<FileVersion[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [selectedVersion, setSelectedVersion] = useState<FileVersion | null>(null)
  const [showDiffModal, setShowDiffModal] = useState(false)
  const [showFullContent, setShowFullContent] = useState(false)

  // Keyboard shortcuts
  useEffect(() => {
    if (!isOpen) return

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        onClose()
      }
    }

    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [isOpen, onClose])

  // Fetch file versions
  const fetchVersions = useCallback(async () => {
    if (!filepath) return
    
    try {
      setLoading(true)
      setError(null)
      const response = await agentApi.getFileVersions(filepath, 20)
      if (response.success && response.data) {
        setVersions(response.data)
      } else {
        setError(response.message || 'Failed to fetch file versions')
      }
    } catch (err) {
      console.error('Failed to fetch file versions:', err)
      setError(err instanceof Error ? err.message : 'Failed to fetch file versions')
    } finally {
      setLoading(false)
    }
  }, [filepath])

  // Load versions when modal opens
  useEffect(() => {
    if (isOpen && filepath) {
      fetchVersions()
    }
  }, [isOpen, filepath, fetchVersions])

  // Format date
  const formatDate = (dateString: string) => {
    try {
      return new Date(dateString).toLocaleString()
    } catch {
      return dateString
    }
  }

  // Format commit hash
  const formatCommitHash = (hash: string) => {
    return hash.substring(0, 8)
  }

  if (!isOpen) return null

  return (
    <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-4xl h-5/6 flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between p-4 border-b border-gray-200 dark:border-gray-700">
          <div>
            <h2 className="text-lg font-semibold text-gray-900 dark:text-gray-100">
              File Revisions
            </h2>
            <p className="text-sm text-gray-500 dark:text-gray-400 font-mono">
              {filepath}
            </p>
          </div>
          <button
            onClick={onClose}
            className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
          >
            <X className="w-6 h-6" />
          </button>
        </div>

        {/* Content */}
        <div className="flex-1 flex overflow-hidden">
          {/* Versions List */}
          <div className="w-1/2 border-r border-gray-200 dark:border-gray-700 overflow-y-auto">
            <div className="p-4">
              {loading ? (
                <div className="flex items-center justify-center py-8">
                  <div className="w-6 h-6 border-4 border-gray-300 border-t-blue-500 rounded-full animate-spin"></div>
                  <span className="ml-2 text-sm text-gray-500">Loading versions...</span>
                </div>
              ) : error ? (
                <div className="text-center py-8">
                  <p className="text-red-500 text-sm">{error}</p>
                  <button
                    onClick={fetchVersions}
                    className="mt-2 px-3 py-1 text-sm bg-blue-500 text-white rounded hover:bg-blue-600"
                  >
                    Retry
                  </button>
                </div>
              ) : versions.length === 0 ? (
                <div className="text-center py-8">
                  <p className="text-gray-500 text-sm">No versions found</p>
                </div>
              ) : (
                <div className="space-y-2">
                  {versions.map((version, index) => (
                    <div
                      key={version.commit_hash}
                      onClick={() => {
                        setSelectedVersion(version)
                        setShowFullContent(false)
                      }}
                      className={`p-3 rounded-lg border cursor-pointer transition-colors ${
                        selectedVersion?.commit_hash === version.commit_hash
                          ? 'border-blue-500 bg-blue-50 dark:bg-blue-900/20'
                          : 'border-gray-200 dark:border-gray-700 hover:bg-gray-50 dark:hover:bg-gray-700'
                      }`}
                    >
                      <div className="flex items-start justify-between">
                        <div className="flex-1 min-w-0">
                          <div className="flex items-center gap-2 mb-1">
                            <GitCommit className="w-4 h-4 text-gray-400" />
                            <span className="text-sm font-mono text-gray-600 dark:text-gray-400">
                              {formatCommitHash(version.commit_hash)}
                            </span>
                            {index === 0 && (
                              <span className="px-2 py-0.5 text-xs bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200 rounded">
                                Latest
                              </span>
                            )}
                          </div>
                          <p className="text-sm text-gray-900 dark:text-gray-100 line-clamp-2">
                            {version.commit_message}
                          </p>
                          <div className="flex items-center gap-4 mt-2 text-xs text-gray-500 dark:text-gray-400">
                            <div className="flex items-center gap-1">
                              <User className="w-3 h-3" />
                              {version.author}
                            </div>
                            <div className="flex items-center gap-1">
                              <Clock className="w-3 h-3" />
                              {formatDate(version.date)}
                            </div>
                          </div>
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>

          {/* Version Details */}
          <div className="w-1/2 flex flex-col">
            {selectedVersion ? (
              <>
                {/* Version Header */}
                <div className="p-4 border-b border-gray-200 dark:border-gray-700">
                  <div className="flex items-center justify-between">
                    <div>
                      <h3 className="font-medium text-gray-900 dark:text-gray-100">
                        Version Details
                      </h3>
                      <p className="text-sm text-gray-500 dark:text-gray-400 font-mono">
                        {formatCommitHash(selectedVersion.commit_hash)}
                      </p>
                    </div>
                    <div className="flex gap-2">
                      {selectedVersion.content && (
                        <button
                          onClick={() => setShowFullContent(!showFullContent)}
                          className="flex items-center gap-1 px-3 py-1.5 text-sm text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-md transition-colors"
                        >
                          <FileText className="w-4 h-4" />
                          {showFullContent ? 'Hide' : 'View'} Full Content
                        </button>
                      )}
                      <button
                        onClick={() => setShowDiffModal(true)}
                        disabled={!selectedVersion.diff}
                        className="flex items-center gap-1 px-3 py-1.5 text-sm text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                      >
                        <Eye className="w-4 h-4" />
                        View Diff
                      </button>
                      {onRestoreVersion && (
                        <button
                          onClick={() => onRestoreVersion(selectedVersion)}
                          className="flex items-center gap-1 px-3 py-1.5 text-sm text-white bg-blue-500 hover:bg-blue-600 rounded-md transition-colors"
                        >
                          <RotateCcw className="w-4 h-4" />
                          Restore
                        </button>
                      )}
                    </div>
                  </div>
                </div>

                {/* Version Content */}
                <div className="flex-1 overflow-y-auto p-4">
                  <div className="space-y-4">
                    <div>
                      <h4 className="text-sm font-medium text-gray-900 dark:text-gray-100 mb-2">
                        Commit Message
                      </h4>
                      <p className="text-sm text-gray-700 dark:text-gray-300 bg-gray-50 dark:bg-gray-700 p-3 rounded">
                        {selectedVersion.commit_message}
                      </p>
                    </div>

                    <div>
                      <h4 className="text-sm font-medium text-gray-900 dark:text-gray-100 mb-2">
                        Author
                      </h4>
                      <p className="text-sm text-gray-700 dark:text-gray-300">
                        {selectedVersion.author}
                      </p>
                    </div>

                    <div>
                      <h4 className="text-sm font-medium text-gray-900 dark:text-gray-100 mb-2">
                        Date
                      </h4>
                      <p className="text-sm text-gray-700 dark:text-gray-300">
                        {formatDate(selectedVersion.date)}
                      </p>
                    </div>

                    {selectedVersion.content && (
                      <div>
                        <h4 className="text-sm font-medium text-gray-900 dark:text-gray-100 mb-2">
                          {showFullContent ? 'Full Content' : 'Content Preview'}
                        </h4>
                        {showFullContent ? (
                          <div className="border border-gray-200 dark:border-gray-700 rounded-lg overflow-hidden" style={{ height: '400px' }}>
                            <FileEditor
                              value={selectedVersion.content}
                              filepath={filepath}
                              readOnly={true}
                              height="400px"
                            />
                          </div>
                        ) : (
                          <div className="bg-gray-50 dark:bg-gray-700 p-3 rounded max-h-40 overflow-y-auto">
                            <pre className="text-xs text-gray-700 dark:text-gray-300 whitespace-pre-wrap">
                              {selectedVersion.content.substring(0, 500)}
                              {selectedVersion.content.length > 500 && '...'}
                            </pre>
                          </div>
                        )}
                      </div>
                    )}

                    {selectedVersion.diff && (
                      <div>
                        <h4 className="text-sm font-medium text-gray-900 dark:text-gray-100 mb-2">
                          Diff
                        </h4>
                        <div className="bg-gray-50 dark:bg-gray-700 p-3 rounded max-h-40 overflow-y-auto">
                          <pre className="text-xs text-gray-700 dark:text-gray-300 whitespace-pre-wrap">
                            {selectedVersion.diff}
                          </pre>
                        </div>
                      </div>
                    )}
                  </div>
                </div>
              </>
            ) : (
              <div className="flex-1 flex items-center justify-center">
                <div className="text-center">
                  <GitCommit className="w-12 h-12 text-gray-400 mx-auto mb-2" />
                  <p className="text-gray-500 text-sm">Select a version to view details</p>
                </div>
              </div>
            )}
          </div>
        </div>

        {/* Diff Modal */}
        {showDiffModal && selectedVersion && (
          <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-60">
            <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-6xl h-5/6 flex flex-col">
              {/* Diff Header */}
              <div className="flex items-center justify-between p-4 border-b border-gray-200 dark:border-gray-700">
                <div>
                  <h3 className="text-lg font-semibold text-gray-900 dark:text-gray-100">
                    Diff View
                  </h3>
                  <p className="text-sm text-gray-500 dark:text-gray-400 font-mono">
                    {formatCommitHash(selectedVersion.commit_hash)} • {selectedVersion.commit_message}
                  </p>
                </div>
                <button
                  onClick={() => setShowDiffModal(false)}
                  className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
                >
                  <X className="w-6 h-6" />
                </button>
              </div>

              {/* Diff Content */}
              <div className="flex-1 overflow-y-auto p-4">
                <div className="bg-gray-900 text-gray-100 p-4 rounded-lg font-mono text-sm overflow-x-auto">
                  <pre className="whitespace-pre-wrap">
                    {selectedVersion.diff?.split('\n').map((line, index) => {
                      let className = 'text-gray-300'
                      if (line.startsWith('+')) {
                        className = 'text-green-400 bg-green-900/20'
                      } else if (line.startsWith('-')) {
                        className = 'text-red-400 bg-red-900/20'
                      } else if (line.startsWith('@@')) {
                        className = 'text-blue-400 bg-blue-900/20'
                      } else if (line.startsWith('diff --git') || line.startsWith('index ') || line.startsWith('---') || line.startsWith('+++')) {
                        className = 'text-yellow-400'
                      }
                      return (
                        <div key={index} className={className}>
                          {line}
                        </div>
                      )
                    })}
                  </pre>
                </div>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
