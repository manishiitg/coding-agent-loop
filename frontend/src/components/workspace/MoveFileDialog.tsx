import { useState, useEffect, useCallback } from 'react'
import { X, Move, Folder, ChevronRight, ChevronDown, Search } from 'lucide-react'
import type { PlannerFile } from '../../services/api-types'
import { useWorkspaceStore } from '../../stores/useWorkspaceStore'

interface MoveFileDialogProps {
  isOpen: boolean
  onClose: () => void
  onMove: (destinationPath: string, commitMessage?: string) => Promise<void>
  item: PlannerFile | null
  destinationPath: string
  setDestinationPath: (path: string) => void
  commitMessage: string
  setCommitMessage: (message: string) => void
  isLoading: boolean
}

export default function MoveFileDialog({
  isOpen,
  onClose,
  onMove,
  item,
  destinationPath,
  setDestinationPath,
  commitMessage,
  setCommitMessage,
  isLoading
}: MoveFileDialogProps) {
  const [error, setError] = useState('')
  const [searchQuery, setSearchQuery] = useState('')
  const [expandedFolders, setExpandedFolders] = useState<Set<string>>(new Set())
  const { files } = useWorkspaceStore()

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    
    if (!destinationPath.trim()) {
      setError('Destination path is required')
      return
    }

    if (!item) {
      setError('No item selected')
      return
    }

    // Validate that destination is different from source
    if (destinationPath === item.filepath) {
      setError('Destination path must be different from source path')
      return
    }

    setError('')

    try {
      await onMove(destinationPath, commitMessage || undefined)
      
      // Reset form
      setDestinationPath('')
      setCommitMessage('')
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to move item')
    }
  }

  const handleClose = useCallback(() => {
    if (!isLoading) {
      setDestinationPath('')
      setCommitMessage('')
      setError('')
      onClose()
    }
  }, [isLoading, onClose, setDestinationPath, setCommitMessage])

  const handleFolderSelect = (folder: PlannerFile) => {
    if (!item) return
    
    const itemName = item.filepath.split('/').pop() || item.filepath
    const folderPath = folder.filepath
    
    // Set destination path: folderPath + "/" + itemName
    const newDestination = folderPath ? `${folderPath}/${itemName}` : itemName
    setDestinationPath(newDestination)
  }

  const toggleFolder = (folderPath: string) => {
    setExpandedFolders(prev => {
      const newSet = new Set(prev)
      if (newSet.has(folderPath)) {
        newSet.delete(folderPath)
      } else {
        newSet.add(folderPath)
      }
      return newSet
    })
  }

  // Filter and flatten folders for display
  const getFilteredFolders = (): PlannerFile[] => {
    const filterFolders = (fileList: PlannerFile[]): PlannerFile[] => {
      const result: PlannerFile[] = []
      
      for (const file of fileList) {
        if (file.type === 'folder') {
          // Filter by search query if provided
          if (searchQuery.trim()) {
            const query = searchQuery.toLowerCase()
            const filepath = file.filepath.toLowerCase()
            if (!filepath.includes(query)) {
              // Check children even if this folder doesn't match
              if (file.children) {
                const filteredChildren = filterFolders(file.children)
                if (filteredChildren.length > 0) {
                  result.push({ ...file, children: filteredChildren })
                }
              }
              continue
            }
          }
          
          result.push(file)
        } else if (file.children) {
          // If it's a file but has children, still traverse children
          const filteredChildren = filterFolders(file.children)
          if (filteredChildren.length > 0) {
            result.push({ ...file, children: filteredChildren })
          }
        }
      }
      
      return result
    }
    
    return filterFolders(files)
  }

  // Flatten folders respecting expanded state
  const flattenFolders = (fileList: PlannerFile[], depth = 0): PlannerFile[] => {
    const result: PlannerFile[] = []
    
    for (const file of fileList) {
      if (file.type === 'folder') {
        result.push({ ...file, depth } as PlannerFile & { depth: number })
        
        // If expanded, add children
        if (expandedFolders.has(file.filepath) && file.children) {
          result.push(...flattenFolders(file.children, depth + 1))
        }
      } else if (file.children) {
        // If it's a file but has children, still traverse
        result.push(...flattenFolders(file.children, depth))
      }
    }
    
    return result
  }

  const filteredFolders = flattenFolders(getFilteredFolders())

  // Keyboard shortcuts
  useEffect(() => {
    if (!isOpen) return

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        if (!isLoading) {
          handleClose()
        }
      }
      // Enter key is handled by the form's onSubmit
    }

    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [isOpen, isLoading, handleClose])

  // Reset search and expanded folders when dialog opens/closes
  useEffect(() => {
    if (!isOpen) {
      setSearchQuery('')
      setExpandedFolders(new Set())
    }
  }, [isOpen])

  if (!isOpen || !item) return null

  const itemName = item.filepath.split('/').pop() || item.filepath
  const isFolder = item.type === 'folder'

  return (
    <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-2xl mx-4 max-h-[90vh] flex flex-col">
        <div className="flex items-center justify-between p-4 border-b border-gray-200 dark:border-gray-700">
          <div className="flex items-center gap-2">
            <Move className="w-5 h-5 text-blue-500" />
            <h3 className="text-lg font-semibold text-gray-900 dark:text-gray-100">
              Move {isFolder ? 'Folder' : 'File'}
            </h3>
          </div>
          <button
            onClick={handleClose}
            disabled={isLoading}
            className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 disabled:opacity-50"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        <form onSubmit={handleSubmit} className="flex flex-col flex-1 overflow-hidden">
          <div className="p-4 space-y-4 flex-1 overflow-y-auto">
            {/* Current Path Display */}
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                Current Path
              </label>
              <div className="px-3 py-2 bg-gray-50 dark:bg-gray-700 border border-gray-300 dark:border-gray-600 rounded-md">
                <p className="text-sm text-gray-900 dark:text-gray-100 truncate">
                  {item.filepath}
                </p>
              </div>
            </div>

            {/* Choose Destination Folder */}
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                Choose Destination Folder
              </label>
              
              {/* Search Input */}
              <div className="relative mb-2">
                <Search className="absolute left-2 top-1/2 transform -translate-y-1/2 w-4 h-4 text-gray-400" />
                <input
                  type="text"
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                  placeholder="Search folders..."
                  disabled={isLoading}
                  className="w-full pl-8 pr-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500 bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 disabled:opacity-50 text-sm"
                />
              </div>

              {/* Folder List */}
              <div className="border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 max-h-64 overflow-y-auto">
                {filteredFolders.length === 0 ? (
                  <div className="px-4 py-8 text-center text-sm text-gray-500 dark:text-gray-400">
                    {searchQuery ? 'No folders found' : 'No folders available'}
                  </div>
                ) : (
                  <div className="py-2">
                    {filteredFolders.map((folder) => {
                      const folderDepth = (folder as PlannerFile & { depth?: number }).depth || 0
                      const isExpanded = expandedFolders.has(folder.filepath)
                      const hasChildren = folder.children && folder.children.some(f => f.type === 'folder')
                      
                      return (
                        <div
                          key={folder.filepath}
                          className="flex items-center gap-2 px-3 py-2 hover:bg-gray-100 dark:hover:bg-gray-600 cursor-pointer transition-colors"
                          style={{ paddingLeft: `${12 + folderDepth * 16}px` }}
                          onClick={() => handleFolderSelect(folder)}
                        >
                          {/* Expand/Collapse Icon */}
                          {hasChildren ? (
                            <button
                              type="button"
                              onClick={(e) => {
                                e.stopPropagation()
                                toggleFolder(folder.filepath)
                              }}
                              className="flex-shrink-0 w-4 h-4 flex items-center justify-center text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
                            >
                              {isExpanded ? (
                                <ChevronDown className="w-3 h-3" />
                              ) : (
                                <ChevronRight className="w-3 h-3" />
                              )}
                            </button>
                          ) : (
                            <div className="w-4 h-4" />
                          )}
                          
                          {/* Folder Icon */}
                          <Folder className="w-4 h-4 text-blue-500 flex-shrink-0" />
                          
                          {/* Folder Name */}
                          <span className="text-sm text-gray-900 dark:text-gray-100 truncate flex-1">
                            {folder.filepath.split('/').pop() || folder.filepath}
                          </span>
                        </div>
                      )
                    })}
                  </div>
                )}
              </div>
            </div>

            {/* Destination Path Input (Manual Override) */}
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                Destination Path (or enter manually)
              </label>
              <input
                type="text"
                value={destinationPath}
                onChange={(e) => setDestinationPath(e.target.value)}
                placeholder={`Enter destination path for ${itemName}`}
                disabled={isLoading}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500 bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 disabled:opacity-50"
              />
            </div>

            {/* Commit Message Input */}
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                Commit Message (Optional)
              </label>
              <input
                type="text"
                value={commitMessage}
                onChange={(e) => setCommitMessage(e.target.value)}
                placeholder="Add commit message for version control"
                disabled={isLoading}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500 bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 disabled:opacity-50"
              />
            </div>

            {error && (
              <div className="text-sm text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md p-2">
                {error}
              </div>
            )}
          </div>

          {/* Action Buttons */}
          <div className="flex justify-end gap-2 p-4 border-t border-gray-200 dark:border-gray-700">
            <button
              type="button"
              onClick={handleClose}
              disabled={isLoading}
              className="px-4 py-2 text-sm font-medium text-gray-700 dark:text-gray-300 bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md disabled:opacity-50"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={isLoading || !destinationPath.trim()}
              className="px-4 py-2 text-sm font-medium text-white bg-blue-600 hover:bg-blue-700 rounded-md disabled:opacity-50 flex items-center gap-2"
            >
              {isLoading ? (
                <>
                  <div className="w-4 h-4 border-2 border-white border-t-transparent rounded-full animate-spin" />
                  Moving...
                </>
              ) : (
                <>
                  <Move className="w-4 h-4" />
                  Move {isFolder ? 'Folder' : 'File'}
                </>
              )}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

