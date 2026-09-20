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
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div className="bg-card rounded-md shadow-md border border-border w-full max-w-2xl mx-4 max-h-[90vh] flex flex-col">
        <div className="flex items-center justify-between border-b border-border p-4">
          <div className="flex items-center gap-2">
            <Move className="w-5 h-5 text-primary" />
            <h3 className="text-sm font-semibold text-foreground">
              Move {isFolder ? 'Folder' : 'File'}
            </h3>
          </div>
          <button
            onClick={handleClose}
            disabled={isLoading}
            className="text-muted-foreground hover:text-foreground disabled:opacity-50"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        <form onSubmit={handleSubmit} className="flex flex-col flex-1 overflow-hidden">
          <div className="p-4 space-y-4 flex-1 overflow-y-auto">
            {/* Current Path Display */}
            <div>
              <label className="block text-sm font-medium text-foreground mb-1">
                Current Path
              </label>
              <div className="px-3 py-2 bg-muted border border-border rounded-md">
                <p className="text-sm text-foreground truncate">
                  {item.filepath}
                </p>
              </div>
            </div>

            {/* Choose Destination Folder */}
            <div>
              <label className="block text-sm font-medium text-foreground mb-2">
                Choose Destination Folder
              </label>
              
              {/* Search Input */}
              <div className="relative mb-2">
                <Search className="absolute left-2 top-1/2 transform -translate-y-1/2 w-4 h-4 text-muted-foreground" />
                <input
                  type="text"
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                  placeholder="Search folders..."
                  disabled={isLoading}
                  className="w-full pl-8 pr-3 py-2 rounded-md border border-input bg-transparent text-foreground placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:opacity-50 text-sm"
                />
              </div>

              {/* Folder List */}
              <div className="border border-border rounded-md bg-transparent max-h-64 overflow-y-auto">
                {filteredFolders.length === 0 ? (
                  <div className="px-4 py-8 text-center text-sm text-muted-foreground">
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
                          className="flex items-center gap-2 px-3 py-2 hover:bg-muted cursor-pointer transition-colors"
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
                              className="flex-shrink-0 w-4 h-4 flex items-center justify-center text-muted-foreground hover:text-foreground"
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
                          <Folder className="w-4 h-4 text-muted-foreground flex-shrink-0" />
                          
                          {/* Folder Name */}
                          <span className="text-sm text-foreground truncate flex-1">
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
              <label className="block text-sm font-medium text-foreground mb-1">
                Destination Path (or enter manually)
              </label>
              <input
                type="text"
                value={destinationPath}
                onChange={(e) => setDestinationPath(e.target.value)}
                placeholder={`Enter destination path for ${itemName}`}
                disabled={isLoading}
                className="w-full px-3 py-2 rounded-md border border-input bg-transparent text-foreground placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:opacity-50"
              />
            </div>

            {/* Commit Message Input */}
            <div>
              <label className="block text-sm font-medium text-foreground mb-1">
                Commit Message (Optional)
              </label>
              <input
                type="text"
                value={commitMessage}
                onChange={(e) => setCommitMessage(e.target.value)}
                placeholder="Add commit message for version control"
                disabled={isLoading}
                className="w-full px-3 py-2 rounded-md border border-input bg-transparent text-foreground placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:opacity-50"
              />
            </div>

            {error && (
              <div className="text-sm text-destructive bg-destructive/10 border border-destructive/20 rounded-md p-2">
                {error}
              </div>
            )}
          </div>

          {/* Action Buttons */}
          <div className="flex justify-end gap-2 border-t border-border p-4">
            <button
              type="button"
              onClick={handleClose}
              disabled={isLoading}
              className="px-4 py-2 text-sm font-medium text-secondary-foreground bg-secondary hover:bg-secondary/80 rounded-md disabled:opacity-50"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={isLoading || !destinationPath.trim()}
              className="px-4 py-2 text-sm font-medium text-primary-foreground bg-primary hover:bg-primary/90 rounded-md disabled:opacity-50 flex items-center gap-2"
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
