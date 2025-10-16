import { useEffect, useCallback, useRef } from 'react'
import { Plus, Upload, FolderPlus, ChevronDown } from 'lucide-react'
import { agentApi } from '../services/api'
import type { PlannerFile } from '../services/api-types'
import PlannerFileList from './workspace/PlannerFileList'
import GitSyncStatus from './workspace/GitSyncStatus'
import SemanticSearchSync from './workspace/SemanticSearchSync'
import CreateFolderDialog from './workspace/CreateFolderDialog'
import ConfirmationDialog from './ui/ConfirmationDialog'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './ui/tooltip'
import { useWorkspaceStore } from '../stores/useWorkspaceStore'
import { useAppStore } from '../stores'

interface WorkspaceProps {
  minimized: boolean
  onToggleMinimize: () => void
}

export default function Workspace({
  minimized,
  onToggleMinimize
}: WorkspaceProps) {
  // Store subscriptions
  const {
    chatFileContext,
    addFileToContext,
    setSelectedFile,
    setFileContent,
    setLoadingFileContent,
    setShowFileContent
  } = useAppStore()

  
  const {
    files,
    setFiles,
    loading,
    setLoading,
    error,
    setError,
    searchQuery,
    setSearchQuery,
    uploadDialog,
    setUploadDialog,
    openUploadDialog,
    closeUploadDialog,
    createFolderDialog,
    openCreateFolderDialog,
    closeCreateFolderDialog,
    deleteDialog,
    setDeleteDialog,
    openDeleteDialog,
    closeDeleteDialog,
    deleteAllFilesDialog,
    setDeleteAllFilesDialog,
    openDeleteAllFilesDialog,
    closeDeleteAllFilesDialog,
    showActionsDropdown,
    setShowActionsDropdown,
    removeFile,
    expandedFolders,
    expandFoldersForFile,
    toggleFolder,
    expandFoldersToLevel
  } = useWorkspaceStore()
  
  // Ref for the workspace scrollable container
  const workspaceScrollRef = useRef<HTMLDivElement>(null)
  
  // API now returns hierarchical structure, no need to reconstruct
  const processHierarchicalFiles = (files: PlannerFile[]): PlannerFile[] => {
    // API returns hierarchical structure directly, just ensure type is set correctly
    return files.map(file => ({
      ...file,
      type: file.type || 'file', // Ensure type is set
      children: file.children || [] // Ensure children array exists
    }))
  }
  
  // Fetch files from Planner
  const fetchFiles = useCallback(async () => {
    try {
      setLoading(true)
      setError(null)
      const response = await agentApi.getPlannerFiles()
      if (response.success && response.data) {
        const allFiles = response.data
        
        // Process hierarchical structure from API
        const processedFiles = processHierarchicalFiles(allFiles)
        setFiles(processedFiles)
        
        // Auto-expand folders up to level 1 by default
        expandFoldersToLevel(processedFiles, 1)
      } else {
        setError(response.message || 'Failed to load files')
      }
    } catch (err) {
      console.error('Failed to fetch Planner files:', err)
      setError(err instanceof Error ? err.message : 'Failed to fetch files')
    } finally {
      setLoading(false)
    }
  }, [expandFoldersToLevel, setLoading, setError, setFiles])
  
  // Use workspace store for file highlighting
  const { highlightedFile } = useWorkspaceStore()
  
  // Function to scroll to highlighted file
  const scrollToHighlightedFile = useCallback((filepath: string) => {
    if (!workspaceScrollRef.current) return
    
    // Find the highlighted file element by looking for the data attribute or class
    const highlightedElement = workspaceScrollRef.current.querySelector(`[data-filepath="${filepath}"]`) ||
                              workspaceScrollRef.current.querySelector(`[data-highlighted="true"]`)
    
    if (highlightedElement) {
      highlightedElement.scrollIntoView({
        behavior: 'smooth',
        block: 'center',
        inline: 'nearest'
      })
    }
  }, [])
  
  // Enhanced file highlighting with folder expansion and auto-scroll
  useEffect(() => {
    if (highlightedFile) {
      expandFoldersForFile(highlightedFile)
      
      // Auto-scroll to highlighted file after a short delay to allow folder expansion
      setTimeout(() => {
        scrollToHighlightedFile(highlightedFile)
      }, 100)
    }
  }, [highlightedFile, expandFoldersForFile, scrollToHighlightedFile])
  
  // Simple filter: show all folders, filter only files
  const filterFiles = (files: PlannerFile[], query: string): PlannerFile[] => {
    if (!query.trim()) return files
    
    const lowercaseQuery = query.toLowerCase()
    
    const filterRecursive = (fileList: PlannerFile[]): PlannerFile[] => {
      return fileList.map(file => {
        if (file.type === 'folder') {
          // For folders: always show them, filter their children
          return {
            ...file,
            children: file.children ? filterRecursive(file.children) : []
          }
        } else {
          // For files: only show if they match
          const fileName = file.filepath.split('/').pop() || file.filepath
          const matches = fileName.toLowerCase().includes(lowercaseQuery) || 
                        file.filepath.toLowerCase().includes(lowercaseQuery)
          return matches ? file : null
        }
      }).filter(Boolean) as PlannerFile[]
    }
    
    return filterRecursive(files)
  }
  
  // Get filtered files
  const filteredFiles = filterFiles(files, searchQuery)
  
  // Close dropdown when clicking outside
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (showActionsDropdown) {
        const target = event.target as Element
        if (!target.closest('.actions-dropdown')) {
          setShowActionsDropdown(false)
        }
      }
    }

    document.addEventListener('mousedown', handleClickOutside)
    return () => {
      document.removeEventListener('mousedown', handleClickOutside)
    }
  }, [showActionsDropdown, setShowActionsDropdown])

  // Load files on component mount
  useEffect(() => {
    fetchFiles()
  }, [fetchFiles])

  // Handle file click - fetch content and show in chat area
  const handleFileClick = async (file: PlannerFile) => {
    if (file.type === 'file' || !file.type) {
      try {
        setLoadingFileContent(true)
        const fileName = file.filepath.split('/').pop() || file.filepath
        setSelectedFile({ name: fileName, path: file.filepath })
        
        // Use the filepath as-is since the API expects the relative path
        const response = await agentApi.getPlannerFileContent(file.filepath)
        
        if (response.success && response.data) {
          let processedContent = response.data.content
          
          // Check if this is an image file
          if (response.data.is_image && processedContent.startsWith('data:image/')) {
            // For images, the content is already base64 encoded data URL
            // No processing needed for images
          } else {
            // Process the content to convert escaped newlines to actual newlines
            processedContent = processedContent
              .replace(/\\n/g, '\n')  // Convert \n to actual newlines
              .replace(/\\t/g, '\t')  // Convert \t to actual tabs
              .replace(/\\r/g, '\r'); // Convert \r to actual carriage returns
          }
          
          setFileContent(processedContent)
          setShowFileContent(true)
        } else {
          setError(response.message || 'Failed to load file content')
        }
      } catch (err) {
        console.error('Failed to fetch file content:', err)
        setError(err instanceof Error ? err.message : 'Failed to fetch file content')
      } finally {
        setLoadingFileContent(false)
      }
    }
  }

  // Handle folder click - only folders are clickable now
  const handleFolderClick = (folder: PlannerFile) => {
    if (folder.type === 'folder') {
      // Toggle folder expansion
      if (expandedFolders.has(folder.filepath)) {
        // Collapse folder
        toggleFolder(folder.filepath)
      } else {
        // Expand folder - children are already loaded
        toggleFolder(folder.filepath)
      }
    }
  }

  // Handle file delete
  const handleFileDelete = (file: PlannerFile) => {
    openDeleteDialog(file)
  }

  // Handle folder delete
  const handleFolderDelete = (folder: PlannerFile) => {
    openDeleteDialog(folder)
  }

  // Handle delete all contents in folder
  const handleDeleteAllFilesInFolder = (folder: PlannerFile) => {
    openDeleteAllFilesDialog(folder)
  }

  // Confirm delete
  const confirmDelete = async () => {
    if (!deleteDialog.item) return

    setDeleteDialog({ isLoading: true })

    try {
      // Use the filepath as-is (already relative path from createFolderStructure)
      if (deleteDialog.item.type === 'file') {
        await agentApi.deletePlannerFile(deleteDialog.item.filepath)
      } else {
        await agentApi.deletePlannerFolder(deleteDialog.item.filepath)
      }
      
      // Remove the deleted item from store
      removeFile(deleteDialog.item.filepath)
      
      // Close dialog
      closeDeleteDialog()
    } catch (err) {
      console.error('Failed to delete item:', err)
      setError(err instanceof Error ? err.message : 'Failed to delete item')
      setDeleteDialog({ isLoading: false })
    }
  }

  // Cancel delete
  const cancelDelete = () => {
    closeDeleteDialog()
  }

  // Confirm delete all contents
  const confirmDeleteAllFiles = async () => {
    if (!deleteAllFilesDialog.folder) return

    setDeleteAllFilesDialog({ isLoading: true })

    try {
      await agentApi.deleteAllFilesInFolder(deleteAllFilesDialog.folder.filepath)
      
      // Refresh the file list to show updated state
      await fetchFiles()
      
      // Close dialog
      closeDeleteAllFilesDialog()
    } catch (err) {
      console.error('Failed to delete all contents:', err)
      setError(err instanceof Error ? err.message : 'Failed to delete all contents')
      setDeleteAllFilesDialog({ isLoading: false })
    }
  }

  // Cancel delete all contents
  const cancelDeleteAllFiles = () => {
    closeDeleteAllFilesDialog()
  }

  // Upload functionality
  const handleUploadClick = () => {
    openUploadDialog('/')
  }

  // Upload to specific folder
  const handleFolderUploadClick = (folderPath: string) => {
    openUploadDialog(folderPath)
  }

  const handleFileSelect = useCallback(async (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0]
    if (!file) return

    // Validate file type (text-based files only)
    const allowedTypes = [
      'text/plain', 'text/markdown', 'application/json', 'text/csv',
      'text/yaml', 'text/xml', 'text/html', 'text/css', 'text/javascript',
      'application/javascript', 'text/x-python', 'text/x-go', 'text/x-java',
      'text/x-c', 'text/x-c++', 'text/x-csharp', 'text/x-php', 'text/x-ruby',
      'text/x-sql', 'text/x-typescript', 'text/x-vue', 'text/x-svelte'
    ]

    if (!allowedTypes.includes(file.type) && !file.name.match(/\.(txt|md|json|csv|yaml|yml|xml|html|css|js|py|go|java|c|cpp|cs|php|rb|sql|ts|vue|svelte)$/i)) {
      setError('Only text-based files are allowed (txt, md, json, csv, yaml, xml, html, css, js, py, go, etc.)')
      return
    }

    // Validate file size (10MB limit)
    if (file.size > 10 * 1024 * 1024) {
      setError('File size must be less than 10MB')
      return
    }

    try {
      setUploadDialog({ isLoading: true })
      setError(null)

      const folderPath = uploadDialog.folderPath || '/'
      const commitMessage = uploadDialog.commitMessage || `Upload ${file.name}`

      await agentApi.uploadPlannerFile(file, folderPath, commitMessage)
      
      // Refresh file list
      await fetchFiles()
      
      // Close dialog
      closeUploadDialog()
    } catch (err) {
      console.error('Failed to upload file:', err)
      setError(err instanceof Error ? err.message : 'Failed to upload file')
      setUploadDialog({ isLoading: false })
    }
  }, [uploadDialog.folderPath, uploadDialog.commitMessage, setUploadDialog, closeUploadDialog, fetchFiles, setError])

  const cancelUpload = useCallback(() => {
    closeUploadDialog()
  }, [closeUploadDialog])

  // Keyboard shortcuts for upload dialog
  useEffect(() => {
    if (!uploadDialog.isOpen) return

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        if (!uploadDialog.isLoading) {
          cancelUpload()
        }
      } else if (event.key === 'Enter') {
        event.preventDefault()
        if (!uploadDialog.isLoading) {
          // Trigger file input click or submit if file is selected
          const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement
          if (fileInput && fileInput.files && fileInput.files.length > 0) {
            handleFileSelect({ target: fileInput } as React.ChangeEvent<HTMLInputElement>)
          }
        }
      }
    }

    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [uploadDialog.isOpen, uploadDialog.isLoading, cancelUpload, handleFileSelect])

  // Folder creation handlers
  const handleCreateFolder = (parentPath?: string) => {
    openCreateFolderDialog(parentPath)
  }

  const handleCreateFolderSubmit = async (folderPath: string, commitMessage?: string) => {
    try {
      await agentApi.createPlannerFolder(folderPath, commitMessage)
      
      // Refresh file list to show the new folder
      await fetchFiles()
      
      // Close dialog
      closeCreateFolderDialog()
    } catch (err) {
      console.error('Failed to create folder:', err)
      throw err // Re-throw to let the dialog handle the error
    }
  }

  const cancelCreateFolder = () => {
    closeCreateFolderDialog()
  }

  return (
    <TooltipProvider>
      <div className="flex flex-col h-full bg-gray-50 dark:bg-gray-900">
      {/* Header */}
      <div className="px-4 py-2 border-b border-gray-200 dark:border-gray-700">
        {!minimized ? (
          <div className="flex items-center justify-between mb-3">
            <div>
              <h2 className="text-base font-semibold text-gray-900 dark:text-gray-100">
                Workspace
              </h2>
              {/* Mode-specific workspace info */}
            </div>
            <div className="flex items-center gap-2">
              <Tooltip>
                <TooltipTrigger asChild>
                  <button
                    onClick={fetchFiles}
                    disabled={loading}
                    className="p-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 disabled:opacity-50"
                  >
                    <svg className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
                    </svg>
                  </button>
                </TooltipTrigger>
                <TooltipContent>
                  <p>Refresh files</p>
                </TooltipContent>
              </Tooltip>
              
              {/* Combined Actions Dropdown */}
              <div className="relative actions-dropdown">
                <Tooltip>
                  <TooltipTrigger asChild>
                    <button
                      onClick={() => setShowActionsDropdown(!showActionsDropdown)}
                      disabled={loading}
                      className="p-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 disabled:opacity-50 flex items-center gap-1"
                    >
                      <Plus className="w-4 h-4" />
                      <ChevronDown className="w-3 h-3" />
                    </button>
                  </TooltipTrigger>
                  <TooltipContent>
                    <p>Add files or folders</p>
                  </TooltipContent>
                </Tooltip>

                {/* Dropdown Menu */}
                {showActionsDropdown && (
                  <div className="absolute top-full right-0 mt-2 w-48 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-lg z-50">
                    <div className="py-1">
                      <button
                        onClick={() => {
                          handleUploadClick()
                          setShowActionsDropdown(false)
                        }}
                        className="w-full px-4 py-2 text-left text-sm text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 flex items-center gap-2"
                      >
                        <Upload className="w-4 h-4" />
                        Upload File
                      </button>
                      <button
                        onClick={() => {
                          handleCreateFolder()
                          setShowActionsDropdown(false)
                        }}
                        className="w-full px-4 py-2 text-left text-sm text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 flex items-center gap-2"
                      >
                        <FolderPlus className="w-4 h-4" />
                        Create Folder
                      </button>
                    </div>
                  </div>
                )}
              </div>

              {/* Git Sync Status */}
              <div className="relative">
                <GitSyncStatus onSync={fetchFiles} isVisible={!minimized} />
              </div>

              {/* Search Sync Status */}
              <div className="relative">
                <SemanticSearchSync onResync={fetchFiles} isVisible={!minimized} />
              </div>
              <div className="flex items-center gap-1">
                <span className="text-xs text-gray-400 dark:text-gray-500 font-mono">⌘5</span>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <button
                      onClick={onToggleMinimize}
                      className="p-1 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 transition-colors relative group"
                    >
                      <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
                      </svg>
                    </button>
                  </TooltipTrigger>
                  <TooltipContent>
                    <p>{minimized ? "Expand workspace" : "Minimize workspace"} (Ctrl+5)</p>
                  </TooltipContent>
                </Tooltip>
              </div>
            </div>
          </div>
        ) : (
          <div className="flex items-center justify-between mb-3">
            <div>
              <h2 className="text-base font-semibold text-gray-900 dark:text-gray-100">
                Workspace
              </h2>
            </div>
            <div className="flex items-center gap-1">
              <span className="text-xs text-gray-400 dark:text-gray-500 font-mono">⌘5</span>
              <Tooltip>
                <TooltipTrigger asChild>
                  <button
                    onClick={onToggleMinimize}
                    className="p-1 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 transition-colors relative group"
                  >
                    <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
                    </svg>
                  </button>
                </TooltipTrigger>
                <TooltipContent>
                  <p>{minimized ? "Expand workspace" : "Minimize workspace"} (Ctrl+5)</p>
                </TooltipContent>
              </Tooltip>
            </div>
          </div>
        )}
        
        {/* Search/Filter Input */}
        {!minimized && (
          <div className="relative">
            <div className="absolute inset-y-0 left-0 pl-3 flex items-center pointer-events-none">
              <svg className="h-4 w-4 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
              </svg>
            </div>
            <input
              type="text"
              placeholder="Search files and folders..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="block w-full pl-10 pr-10 py-2 border border-gray-300 dark:border-gray-600 rounded-md leading-5 bg-white dark:bg-gray-800 placeholder-gray-500 dark:placeholder-gray-400 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-1 focus:ring-blue-500 focus:border-blue-500 text-sm"
            />
            {searchQuery && (
              <div className="absolute inset-y-0 right-0 pr-3 flex items-center">
                <Tooltip>
                  <TooltipTrigger asChild>
                    <button
                      onClick={() => setSearchQuery('')}
                      className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
                    >
                      <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                      </svg>
                    </button>
                  </TooltipTrigger>
                  <TooltipContent>
                    <p>Clear search</p>
                  </TooltipContent>
                </Tooltip>
              </div>
            )}
          </div>
        )}
        
      </div>

      {/* Content */}
      {!minimized && (
        <div className="flex-1 overflow-hidden">
          {/* Folder Structure - Full Width */}
          <div ref={workspaceScrollRef} className="h-full overflow-y-auto">
            <div className="p-4">
              <PlannerFileList
                files={filteredFiles}
                loading={loading}
                error={error}
                onFolderClick={handleFolderClick}
                onFileClick={handleFileClick}
                onFileDelete={handleFileDelete}
                onFolderDelete={handleFolderDelete}
                onDeleteAllFilesInFolder={handleDeleteAllFilesInFolder}
                onRetry={fetchFiles}
                expandedFolders={expandedFolders}
                loadingChildren={new Set()}
                chatFileContext={chatFileContext}
                addFileToContext={addFileToContext}
                highlightedFile={highlightedFile}
                onFolderUpload={handleFolderUploadClick}
                onCreateFolder={handleCreateFolder}
              />
            </div>
          </div>
        </div>
      )}

      {/* Minimized Icons */}
      {minimized && (
        <div className="flex-1 flex flex-col items-center py-4 space-y-4">
          {/* Files Icon */}
          <button
            onClick={fetchFiles}
            className="p-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 transition-colors"
            title="Refresh Files"
          >
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2H5a2 2 0 00-2-2z" />
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 5a2 2 0 012-2h4a2 2 0 012 2v2H8V5z" />
            </svg>
          </button>

          {/* Search Icon */}
          <button
            onClick={() => setSearchQuery('')}
            className="p-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 transition-colors"
            title="Clear Search"
          >
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
            </svg>
          </button>

          {/* Context Icon */}
          <button
            onClick={() => addFileToContext({name: 'Current Context', path: '/', type: 'folder'})}
            className="p-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 transition-colors"
            title={`Files in Context: ${chatFileContext.length}`}
          >
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
            </svg>
          </button>
        </div>
      )}

      {/* Delete Confirmation Dialog */}
      <ConfirmationDialog
        isOpen={deleteDialog.isOpen}
        onClose={cancelDelete}
        onConfirm={confirmDelete}
        title={deleteDialog.item?.type === 'folder' ? 'Delete Folder' : 'Delete File'}
        message={`Are you sure you want to delete "${deleteDialog.item ? deleteDialog.item.filepath.split('/').pop() : 'this item'}"? This action cannot be undone.${
          deleteDialog.item?.type === 'folder' ? ' This will delete all files and subfolders inside.' : ''
        }`}
        confirmText="Delete"
        cancelText="Cancel"
        type="danger"
        isLoading={deleteDialog.isLoading}
      />

      {/* Delete All Files Confirmation Dialog */}
      <ConfirmationDialog
        isOpen={deleteAllFilesDialog.isOpen}
        onClose={cancelDeleteAllFiles}
        onConfirm={confirmDeleteAllFiles}
        title="Delete All Contents"
        message={`Are you sure you want to delete ALL CONTENTS (files and folders) in "${deleteAllFilesDialog.folder ? deleteAllFilesDialog.folder.filepath.split('/').pop() : 'this folder'}"? This action cannot be undone. The folder itself will remain, but all files and subfolders inside will be permanently deleted.`}
        confirmText="Delete All Contents"
        cancelText="Cancel"
        type="warning"
        isLoading={deleteAllFilesDialog.isLoading}
      />

      {/* Upload Dialog */}
      {uploadDialog.isOpen && (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
          <form onSubmit={(e) => {
            e.preventDefault()
            const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement
            if (fileInput && fileInput.files && fileInput.files.length > 0) {
              handleFileSelect({ target: fileInput } as React.ChangeEvent<HTMLInputElement>)
            }
          }} className="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-md mx-4">
            <h3 className="text-lg font-semibold text-gray-900 dark:text-gray-100 mb-4">
              Upload File
            </h3>
            
            <div className="space-y-4">
              {/* Upload Destination Display */}
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                  Upload Destination
                </label>
                <div className="px-3 py-2 bg-gray-50 dark:bg-gray-700 border border-gray-300 dark:border-gray-600 rounded-md">
                  <p className="text-sm text-gray-900 dark:text-gray-100">
                    {uploadDialog.folderPath === '/' ? 'Root directory (/)' : uploadDialog.folderPath}
                  </p>
                </div>
              </div>

              {/* Commit Message Input */}
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                  Commit Message (Optional)
                </label>
                <input
                  type="text"
                  value={uploadDialog.commitMessage}
                  onChange={(e) => setUploadDialog({ commitMessage: e.target.value })}
                  placeholder="Upload description"
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-1 focus:ring-blue-500 focus:border-blue-500"
                />
              </div>

              {/* File Input */}
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                  Select File
                </label>
                <input
                  type="file"
                  onChange={handleFileSelect}
                  accept=".txt,.md,.json,.csv,.yaml,.yml,.xml,.html,.css,.js,.py,.go,.java,.c,.cpp,.cs,.php,.rb,.sql,.ts,.vue,.svelte"
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-1 focus:ring-blue-500 focus:border-blue-500"
                />
                <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                  Only text-based files allowed (10MB max)
                </p>
              </div>
            </div>

            {/* Action Buttons */}
            <div className="flex justify-end gap-2 mt-6">
              <button
                type="button"
                onClick={cancelUpload}
                disabled={uploadDialog.isLoading}
                className="px-4 py-2 text-sm text-gray-600 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200 disabled:opacity-50"
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={uploadDialog.isLoading}
                className="px-4 py-2 text-sm bg-blue-600 text-white rounded-md hover:bg-blue-700 disabled:opacity-50"
              >
                {uploadDialog.isLoading ? 'Uploading...' : 'Upload'}
              </button>
            </div>
          </form>
        </div>
      )}

      {/* Create Folder Dialog */}
      <CreateFolderDialog
        isOpen={createFolderDialog.isOpen}
        onClose={cancelCreateFolder}
        onCreateFolder={handleCreateFolderSubmit}
        parentPath={createFolderDialog.parentPath}
      />
      </div>
    </TooltipProvider>
  )
}