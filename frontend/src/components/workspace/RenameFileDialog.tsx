import { useState, useEffect, useCallback } from 'react'
import { X, Edit2, FileText, Folder } from 'lucide-react'
import type { PlannerFile } from '../../services/api-types'

interface RenameFileDialogProps {
  isOpen: boolean
  onClose: () => void
  onRename: (newName: string, commitMessage?: string) => Promise<void>
  item: PlannerFile | null
  isLoading: boolean
}

export default function RenameFileDialog({
  isOpen,
  onClose,
  onRename,
  item,
  isLoading
}: RenameFileDialogProps) {
  const [error, setError] = useState('')
  const [newName, setNewName] = useState('')
  const [commitMessage, setCommitMessage] = useState('')

  // Initialize newName when item changes or dialog opens
  useEffect(() => {
    if (isOpen && item) {
      const fileName = item.filepath.split('/').pop() || item.filepath
      setNewName(fileName)
      setCommitMessage('')
      setError('')
    }
  }, [isOpen, item])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    
    if (!newName.trim()) {
      setError('Name is required')
      return
    }

    if (!item) {
      setError('No item selected')
      return
    }

    const currentName = item.filepath.split('/').pop() || item.filepath
    if (newName === currentName) {
      handleClose()
      return
    }

    setError('')

    try {
      await onRename(newName, commitMessage || undefined)
      // Close is handled by parent or success
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to rename item')
    }
  }

  const handleClose = useCallback(() => {
    if (!isLoading) {
      setError('')
      setCommitMessage('')
      onClose()
    }
  }, [isLoading, onClose, setCommitMessage])

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
    }

    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [isOpen, isLoading, handleClose])

  if (!isOpen || !item) return null

  const isFolder = item.type === 'folder'

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div className="bg-card rounded-md shadow-md border border-border w-full max-w-md mx-4 flex flex-col">
        <div className="flex items-center justify-between border-b border-border p-4">
          <div className="flex items-center gap-2">
            <Edit2 className="w-5 h-5 text-primary" />
            <h3 className="text-sm font-semibold text-foreground">
              Rename {isFolder ? 'Folder' : 'File'}
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

        <form onSubmit={handleSubmit} className="flex flex-col flex-1">
          <div className="p-4 space-y-4">
            {/* Current Path Display */}
            <div>
              <label className="block text-sm font-medium text-foreground mb-1">
                Location
              </label>
              <div className="px-3 py-2 bg-muted border border-border rounded-md flex items-center gap-2 text-muted-foreground">
                {isFolder ? <Folder className="w-4 h-4" /> : <FileText className="w-4 h-4" />}
                <span className="text-sm truncate">
                  {item.filepath.split('/').slice(0, -1).join('/') || '/'}
                </span>
              </div>
            </div>

            {/* New Name Input */}
            <div>
              <label className="block text-sm font-medium text-foreground mb-1">
                Name
              </label>
              <input
                type="text"
                value={newName}
                onChange={(e) => setNewName(e.target.value)}
                placeholder={`Enter new name`}
                autoFocus
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
              disabled={isLoading || !newName.trim()}
              className="px-4 py-2 text-sm font-medium text-primary-foreground bg-primary hover:bg-primary/90 rounded-md disabled:opacity-50 flex items-center gap-2"
            >
              {isLoading ? (
                <>
                  <div className="w-4 h-4 border-2 border-white border-t-transparent rounded-full animate-spin" />
                  Renaming...
                </>
              ) : (
                <>
                  <Edit2 className="w-4 h-4" />
                  Rename
                </>
              )}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
