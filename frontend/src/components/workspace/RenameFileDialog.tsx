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
    <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-md mx-4 flex flex-col">
        <div className="flex items-center justify-between p-4 border-b border-gray-200 dark:border-gray-700">
          <div className="flex items-center gap-2">
            <Edit2 className="w-5 h-5 text-blue-500" />
            <h3 className="text-lg font-semibold text-gray-900 dark:text-gray-100">
              Rename {isFolder ? 'Folder' : 'File'}
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

        <form onSubmit={handleSubmit} className="flex flex-col flex-1">
          <div className="p-4 space-y-4">
            {/* Current Path Display */}
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                Location
              </label>
              <div className="px-3 py-2 bg-gray-50 dark:bg-gray-700 border border-gray-300 dark:border-gray-600 rounded-md flex items-center gap-2 text-gray-500 dark:text-gray-400">
                {isFolder ? <Folder className="w-4 h-4" /> : <FileText className="w-4 h-4" />}
                <span className="text-sm truncate">
                  {item.filepath.split('/').slice(0, -1).join('/') || '/'}
                </span>
              </div>
            </div>

            {/* New Name Input */}
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                Name
              </label>
              <input
                type="text"
                value={newName}
                onChange={(e) => setNewName(e.target.value)}
                placeholder={`Enter new name`}
                autoFocus
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
              disabled={isLoading || !newName.trim()}
              className="px-4 py-2 text-sm font-medium text-white bg-blue-600 hover:bg-blue-700 rounded-md disabled:opacity-50 flex items-center gap-2"
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
