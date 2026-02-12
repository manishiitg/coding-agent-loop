import { useEffect, useState, useCallback } from 'react'
import { Loader2, Folder, FolderOpen, FileText, FileCode, FileJson, Image, ChevronRight, ChevronDown, ArrowLeft, ExternalLink, LogIn, Download, FolderInput, X, Check } from 'lucide-react'
import { getApiBaseUrl, getAuthToken, workspaceApi } from '../services/api'
import { SharedFile } from './SharedFile'

interface FolderItem {
  filepath: string
  type?: 'file' | 'folder'
  last_modified?: string
  children?: FolderItem[]
}

interface SharedFolderProps {
  encodedPath: string
  uid?: string
  onBack?: () => void
}

function getFileIcon(filepath: string) {
  const ext = filepath.toLowerCase().split('.').pop() || ''
  const codeExts = ['go', 'py', 'ts', 'tsx', 'js', 'jsx', 'java', 'c', 'cpp', 'rs', 'rb', 'php', 'sh', 'bash', 'css', 'scss', 'html', 'xml', 'yaml', 'yml', 'sql', 'swift', 'kt']
  const imageExts = ['png', 'jpg', 'jpeg', 'gif', 'svg', 'webp', 'ico', 'bmp']

  if (ext === 'json') return <FileJson className="w-4 h-4 text-yellow-500" />
  if (codeExts.includes(ext)) return <FileCode className="w-4 h-4 text-blue-500" />
  if (imageExts.includes(ext)) return <Image className="w-4 h-4 text-green-500" />
  if (ext === 'md' || ext === 'markdown') return <FileText className="w-4 h-4 text-gray-500" />
  return <FileText className="w-4 h-4 text-gray-400" />
}

function getFileExtBadge(filepath: string) {
  const ext = filepath.toLowerCase().split('.').pop() || ''
  if (!ext) return null
  return (
    <span className="px-1.5 py-0.5 text-[10px] font-mono rounded bg-gray-100 dark:bg-gray-700 text-gray-500 dark:text-gray-400 uppercase">
      {ext}
    </span>
  )
}

function formatDate(dateStr: string) {
  const d = new Date(dateStr)
  const now = new Date()
  const diffMs = now.getTime() - d.getTime()
  const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24))

  if (diffDays === 0) return 'Today'
  if (diffDays === 1) return 'Yesterday'
  if (diffDays < 7) return `${diffDays}d ago`
  return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: d.getFullYear() !== now.getFullYear() ? 'numeric' : undefined })
}

function countFiles(items: FolderItem[]): number {
  let count = 0
  for (const item of items) {
    if (item.type !== 'folder') count++
    if (item.children) count += countFiles(item.children)
  }
  return count
}

export function SharedFolder({ encodedPath, uid, onBack }: SharedFolderProps) {
  const [items, setItems] = useState<FolderItem[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [needsAuth, setNeedsAuth] = useState(false)
  const [selectedFile, setSelectedFile] = useState<string | null>(null)
  const [expandedFolders, setExpandedFolders] = useState<Set<string>>(new Set())
  const [isDownloading, setIsDownloading] = useState(false)
  const [showImportDialog, setShowImportDialog] = useState(false)
  const [workspaceFolders, setWorkspaceFolders] = useState<string[]>([])
  const [selectedDestination, setSelectedDestination] = useState('')
  const [isImporting, setIsImporting] = useState(false)
  const [importResult, setImportResult] = useState<{ success: boolean; message: string } | null>(null)
  const [loadingFolders, setLoadingFolders] = useState(false)

  const folderPath = (() => {
    try {
      return decodeURIComponent(escape(atob(encodedPath)))
    } catch {
      return null
    }
  })()

  const folderName = folderPath?.split('/').filter(Boolean).pop() || folderPath || 'Folder'

  const toggleFolder = useCallback((path: string) => {
    setExpandedFolders(prev => {
      const next = new Set(prev)
      if (next.has(path)) next.delete(path)
      else next.add(path)
      return next
    })
  }, [])

  const loadFolder = useCallback(async (path: string) => {
    try {
      setLoading(true)
      setError(null)
      const base = getApiBaseUrl() || ''
      const encoded = btoa(unescape(encodeURIComponent(path)))
      const headers: Record<string, string> = {}
      const token = getAuthToken()
      if (token) headers['Authorization'] = `Bearer ${token}`
      const uidParam = uid ? `&uid=${encodeURIComponent(uid)}` : ''
      const resp = await fetch(`${base}/api/public/folder?path=${encoded}${uidParam}`, { headers })
      if (resp.status === 401) {
        setNeedsAuth(true)
        return
      }
      if (!resp.ok) {
        throw new Error(resp.status === 404 ? 'Folder not found' : `Failed to load folder (${resp.status})`)
      }
      const json = await resp.json()
      if (json.success && json.data) {
        const fileList: FolderItem[] = Array.isArray(json.data) ? json.data : []
        setItems(fileList)
        // Auto-expand top-level folders
        const topFolders = fileList.filter(i => i.type === 'folder').map(i => i.filepath)
        setExpandedFolders(new Set(topFolders))
      } else {
        setItems([])
      }
    } catch (err) {
      console.error('Failed to load shared folder:', err)
      setError(err instanceof Error ? err.message : 'Failed to load folder')
    } finally {
      setLoading(false)
    }
  }, [])

  const handleDownload = useCallback(async () => {
    if (!folderPath) return
    setIsDownloading(true)
    try {
      const base = getApiBaseUrl() || ''
      const encoded = btoa(unescape(encodeURIComponent(folderPath)))
      const headers: Record<string, string> = {}
      const token = getAuthToken()
      if (token) headers['Authorization'] = `Bearer ${token}`
      const uidParam = uid ? `&uid=${encodeURIComponent(uid)}` : ''
      const resp = await fetch(`${base}/api/public/folder/download?path=${encoded}${uidParam}`, { headers })
      if (!resp.ok) throw new Error(`Download failed (${resp.status})`)
      const blob = await resp.blob()
      const url = window.URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      const name = folderPath.split('/').filter(Boolean).pop() || 'folder'
      const timestamp = new Date().toISOString().replace(/[:.]/g, '-').slice(0, -5)
      link.download = `${name}-backup-${timestamp}.zip`
      document.body.appendChild(link)
      link.click()
      document.body.removeChild(link)
      window.URL.revokeObjectURL(url)
    } catch (err) {
      console.error('Download failed:', err)
      alert(err instanceof Error ? err.message : 'Download failed')
    } finally {
      setIsDownloading(false)
    }
  }, [folderPath, uid])

  const openImportDialog = useCallback(async () => {
    setShowImportDialog(true)
    setImportResult(null)
    setLoadingFolders(true)
    try {
      const resp = await workspaceApi.get('/api/documents', { params: { limit: 100 } })
      if (resp.data?.success && Array.isArray(resp.data.data)) {
        const folders = resp.data.data
          .filter((d: { type?: string }) => d.type === 'folder')
          .map((d: { filepath: string }) => d.filepath)
          .sort()
        setWorkspaceFolders(folders)
        // Default to Plans if available, otherwise first folder
        setSelectedDestination(folders.includes('Plans') ? 'Plans' : folders[0] || '')
      }
    } catch {
      setWorkspaceFolders([])
    } finally {
      setLoadingFolders(false)
    }
  }, [])

  const handleImport = useCallback(async () => {
    if (!folderPath || !selectedDestination) return
    setIsImporting(true)
    setImportResult(null)
    try {
      // Step 1: Download the ZIP from the public endpoint
      const base = getApiBaseUrl() || ''
      const encoded = btoa(unescape(encodeURIComponent(folderPath)))
      const headers: Record<string, string> = {}
      const token = getAuthToken()
      if (token) headers['Authorization'] = `Bearer ${token}`
      const uidParam = uid ? `&uid=${encodeURIComponent(uid)}` : ''
      const resp = await fetch(`${base}/api/public/folder/download?path=${encoded}${uidParam}`, { headers })
      if (!resp.ok) throw new Error(`Download failed (${resp.status})`)
      const blob = await resp.blob()

      // Step 2: Upload to workspace import endpoint
      const formData = new FormData()
      formData.append('file', blob, 'import.zip')
      formData.append('workspace_path', selectedDestination)
      formData.append('overwrite', 'true')

      const importResp = await workspaceApi.post('/api/workspace/import', formData, {
        headers: { 'Content-Type': 'multipart/form-data' },
      })

      if (importResp.data?.success) {
        const count = importResp.data.data?.files_extracted || 0
        setImportResult({ success: true, message: `Imported ${count} files into ${selectedDestination}/` })
      } else {
        setImportResult({ success: false, message: importResp.data?.message || 'Import failed' })
      }
    } catch (err) {
      console.error('Import failed:', err)
      setImportResult({ success: false, message: err instanceof Error ? err.message : 'Import failed' })
    } finally {
      setIsImporting(false)
    }
  }, [folderPath, selectedDestination, uid])

  useEffect(() => {
    if (!folderPath) {
      setError('Invalid folder path encoding')
      setLoading(false)
      return
    }
    loadFolder(folderPath)
  }, [folderPath, loadFolder])

  // If a file is selected, show the SharedFile viewer
  if (selectedFile) {
    const fileEncoded = btoa(unescape(encodeURIComponent(selectedFile)))
    return (
      <SharedFile
        encodedPath={fileEncoded}
        uid={uid}
        onBack={() => setSelectedFile(null)}
      />
    )
  }

  if (needsAuth) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-100 dark:bg-gray-900">
        <div className="max-w-md w-full p-8 bg-white dark:bg-gray-800 rounded-lg shadow-lg text-center">
          <LogIn className="h-12 w-12 mx-auto mb-4 text-blue-500" />
          <h2 className="text-xl font-bold text-gray-900 dark:text-white mb-2">Login Required</h2>
          <p className="text-gray-600 dark:text-gray-400 mb-6">
            You need to be logged in to view this shared folder.
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

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-100 dark:bg-gray-900">
        <div className="text-center">
          <Loader2 className="h-8 w-8 animate-spin mx-auto text-blue-500" />
          <p className="mt-4 text-gray-600 dark:text-gray-400">Loading folder...</p>
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-100 dark:bg-gray-900">
        <div className="max-w-md w-full p-8 bg-white dark:bg-gray-800 rounded-lg shadow-lg text-center">
          <div className="text-red-500 dark:text-red-400 text-6xl mb-4">!</div>
          <h2 className="text-xl font-bold text-gray-900 dark:text-white mb-2">Folder Not Found</h2>
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

  const totalFiles = countFiles(items)

  return (
    <div className="min-h-screen bg-gray-100 dark:bg-gray-900">
      {/* Header */}
      <div className="bg-white dark:bg-gray-800 shadow sticky top-0 z-10">
        <div className="max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 py-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center space-x-3">
              {onBack && (
                <button
                  onClick={onBack}
                  className="p-1.5 rounded-md text-gray-500 hover:text-gray-900 dark:hover:text-gray-100 hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors"
                  title="Back"
                >
                  <ArrowLeft className="h-5 w-5" />
                </button>
              )}
              <Folder className="h-5 w-5 text-yellow-500 flex-shrink-0" />
              <div className="min-w-0">
                <h1 className="text-lg font-semibold text-gray-900 dark:text-white truncate">{folderName}</h1>
                <p className="text-xs text-gray-500 dark:text-gray-400">
                  {totalFiles} file{totalFiles !== 1 ? 's' : ''}
                </p>
              </div>
            </div>
            <div className="flex items-center gap-2 flex-shrink-0">
              <button
                onClick={openImportDialog}
                className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium text-green-700 dark:text-green-300 bg-green-100 dark:bg-green-900/30 hover:bg-green-200 dark:hover:bg-green-900/50 rounded transition-colors"
                title="Import into your workspace"
              >
                <FolderInput className="w-3.5 h-3.5" />
                Import
              </button>
              <button
                onClick={handleDownload}
                disabled={isDownloading}
                className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium text-blue-700 dark:text-blue-300 bg-blue-100 dark:bg-blue-900/30 hover:bg-blue-200 dark:hover:bg-blue-900/50 rounded transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                title="Download folder as ZIP"
              >
                {isDownloading ? (
                  <Loader2 className="w-3.5 h-3.5 animate-spin" />
                ) : (
                  <Download className="w-3.5 h-3.5" />
                )}
                Download
              </button>
              <span className="bg-yellow-100 dark:bg-yellow-900/30 text-yellow-800 dark:text-yellow-400 px-2 py-0.5 rounded text-xs font-medium">
                Read-only
              </span>
            </div>
          </div>
        </div>
      </div>

      {/* Breadcrumb */}
      <div className="max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 pt-4">
        <div className="text-xs text-gray-500 dark:text-gray-400 flex items-center gap-1 flex-wrap">
          {folderPath?.split('/').filter(Boolean).map((seg, i, arr) => (
            <span key={i} className="flex items-center gap-1">
              {i > 0 && <ChevronRight className="w-3 h-3" />}
              <span className={i === arr.length - 1 ? 'text-gray-900 dark:text-gray-100 font-medium' : ''}>{seg}</span>
            </span>
          ))}
        </div>
      </div>

      {/* Content */}
      <div className="max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 py-4 pb-16">
        <div className="bg-white dark:bg-gray-800 rounded-lg shadow overflow-hidden">
          {items.length === 0 ? (
            <div className="text-center py-16 text-gray-500 dark:text-gray-400">
              <Folder className="w-12 h-12 mx-auto mb-3 text-gray-300 dark:text-gray-600" />
              <p>This folder is empty</p>
            </div>
          ) : (
            <div>
              {renderTree(items, 0, expandedFolders, toggleFolder, setSelectedFile)}
            </div>
          )}
        </div>
      </div>

      {/* Footer */}
      {onBack && (
        <div className="fixed bottom-0 left-0 right-0 bg-white dark:bg-gray-800 border-t border-gray-200 dark:border-gray-700 py-2">
          <div className="max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 text-center text-sm text-gray-500 dark:text-gray-400">
            <button onClick={onBack} className="text-blue-600 dark:text-blue-400 hover:underline inline-flex items-center">
              Return to app <ExternalLink className="h-3 w-3 ml-1" />
            </button>
          </div>
        </div>
      )}

      {/* Import Dialog */}
      {showImportDialog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={() => !isImporting && setShowImportDialog(false)}>
          <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-md mx-4 overflow-hidden" onClick={e => e.stopPropagation()}>
            {/* Dialog Header */}
            <div className="flex items-center justify-between px-5 py-4 border-b border-gray-200 dark:border-gray-700">
              <div className="flex items-center gap-2">
                <FolderInput className="w-5 h-5 text-green-500" />
                <h2 className="text-base font-semibold text-gray-900 dark:text-white">Import to Workspace</h2>
              </div>
              <button
                onClick={() => !isImporting && setShowImportDialog(false)}
                className="p-1 rounded hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 transition-colors"
              >
                <X className="w-4 h-4" />
              </button>
            </div>

            {/* Dialog Body */}
            <div className="px-5 py-4 space-y-4">
              <p className="text-sm text-gray-600 dark:text-gray-400">
                Import <span className="font-medium text-gray-900 dark:text-white">{folderName}</span> into your workspace. Choose a destination folder:
              </p>

              {loadingFolders ? (
                <div className="flex items-center justify-center py-4">
                  <Loader2 className="w-5 h-5 animate-spin text-gray-400" />
                </div>
              ) : (
                <div className="space-y-1.5 max-h-48 overflow-y-auto">
                  {workspaceFolders.map(folder => (
                    <button
                      key={folder}
                      onClick={() => setSelectedDestination(folder)}
                      className={`w-full flex items-center gap-2.5 px-3 py-2 rounded-md text-left text-sm transition-colors ${
                        selectedDestination === folder
                          ? 'bg-green-50 dark:bg-green-900/20 text-green-800 dark:text-green-300 ring-1 ring-green-300 dark:ring-green-700'
                          : 'hover:bg-gray-50 dark:hover:bg-gray-700/50 text-gray-700 dark:text-gray-300'
                      }`}
                    >
                      <Folder className="w-4 h-4 text-yellow-500 flex-shrink-0" />
                      <span className="truncate">{folder}</span>
                      {selectedDestination === folder && (
                        <Check className="w-4 h-4 text-green-500 ml-auto flex-shrink-0" />
                      )}
                    </button>
                  ))}
                  {workspaceFolders.length === 0 && (
                    <p className="text-sm text-gray-500 dark:text-gray-400 text-center py-2">No folders found in workspace</p>
                  )}
                </div>
              )}

              {/* Result message */}
              {importResult && (
                <div className={`text-sm p-3 rounded-md ${
                  importResult.success
                    ? 'bg-green-50 dark:bg-green-900/20 text-green-700 dark:text-green-300'
                    : 'bg-red-50 dark:bg-red-900/20 text-red-700 dark:text-red-300'
                }`}>
                  {importResult.message}
                </div>
              )}
            </div>

            {/* Dialog Footer */}
            <div className="flex items-center justify-end gap-2 px-5 py-3 border-t border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800/50">
              <button
                onClick={() => setShowImportDialog(false)}
                disabled={isImporting}
                className="px-4 py-2 text-sm text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-700 rounded-md transition-colors disabled:opacity-50"
              >
                {importResult?.success ? 'Done' : 'Cancel'}
              </button>
              {!importResult?.success && (
                <button
                  onClick={handleImport}
                  disabled={isImporting || !selectedDestination}
                  className="flex items-center gap-1.5 px-4 py-2 text-sm font-medium text-white bg-green-600 hover:bg-green-700 rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  {isImporting ? (
                    <>
                      <Loader2 className="w-3.5 h-3.5 animate-spin" />
                      Importing...
                    </>
                  ) : (
                    <>
                      <FolderInput className="w-3.5 h-3.5" />
                      Import
                    </>
                  )}
                </button>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function renderTree(
  items: FolderItem[],
  depth: number,
  expandedFolders: Set<string>,
  toggleFolder: (path: string) => void,
  onFileClick: (filepath: string) => void
) {
  // Sort: folders first, then files, both alphabetical
  const sorted = [...items].sort((a, b) => {
    if (a.type === 'folder' && b.type !== 'folder') return -1
    if (a.type !== 'folder' && b.type === 'folder') return 1
    return a.filepath.localeCompare(b.filepath)
  })

  return sorted.map((item) => {
    const name = item.filepath.split('/').filter(Boolean).pop() || item.filepath
    const isFolder = item.type === 'folder'
    const isExpanded = expandedFolders.has(item.filepath)

    if (isFolder) {
      const childCount = item.children ? countFiles(item.children) : 0
      return (
        <div key={item.filepath}>
          <button
            onClick={() => toggleFolder(item.filepath)}
            className="w-full flex items-center gap-2.5 px-3 py-2.5 bg-gray-50 dark:bg-gray-700/40 hover:bg-gray-100 dark:hover:bg-gray-700/70 transition-colors text-left border-b border-gray-100 dark:border-gray-700/50"
            style={{ paddingLeft: `${12 + depth * 20}px` }}
          >
            {isExpanded
              ? <ChevronDown className="w-3.5 h-3.5 text-gray-400 flex-shrink-0" />
              : <ChevronRight className="w-3.5 h-3.5 text-gray-400 flex-shrink-0" />
            }
            {isExpanded
              ? <FolderOpen className="w-4 h-4 text-yellow-500 flex-shrink-0" />
              : <Folder className="w-4 h-4 text-yellow-500 flex-shrink-0" />
            }
            <span className="text-sm font-medium text-gray-900 dark:text-gray-100 truncate">{name}</span>
            {childCount > 0 && (
              <span className="text-[10px] text-gray-400 dark:text-gray-500 ml-1">
                {childCount}
              </span>
            )}
          </button>
          {isExpanded && item.children && item.children.length > 0 && (
            <div>
              {renderTree(item.children, depth + 1, expandedFolders, toggleFolder, onFileClick)}
            </div>
          )}
        </div>
      )
    }

    return (
      <button
        key={item.filepath}
        onClick={() => onFileClick(item.filepath)}
        className="w-full flex items-center gap-2.5 px-3 py-2.5 hover:bg-blue-50 dark:hover:bg-blue-900/10 transition-colors text-left group border-b border-gray-100 dark:border-gray-700/50 last:border-b-0"
        style={{ paddingLeft: `${12 + depth * 20 + 18}px` }}
      >
        {getFileIcon(item.filepath)}
        <span className="text-sm text-gray-700 dark:text-gray-300 group-hover:text-blue-600 dark:group-hover:text-blue-400 truncate flex-1">
          {name}
        </span>
        {getFileExtBadge(item.filepath)}
        {item.last_modified && (
          <span className="text-[11px] text-gray-400 dark:text-gray-500 flex-shrink-0 ml-2">
            {formatDate(item.last_modified)}
          </span>
        )}
      </button>
    )
  })
}
