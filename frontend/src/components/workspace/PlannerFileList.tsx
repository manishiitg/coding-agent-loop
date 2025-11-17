import { FileText, Folder, AlertCircle, Loader2, ChevronRight, ChevronDown, Trash2, MessageSquare, Upload, Plus, Image, MoreHorizontal, Move } from 'lucide-react'
import type { PlannerFile } from '../../services/api-types'
import { Tooltip, TooltipContent, TooltipTrigger, TooltipProvider } from '../ui/tooltip'
import { useWorkspaceStore } from '../../stores/useWorkspaceStore'

interface PlannerFileListProps {
  files: PlannerFile[]
  loading: boolean
  error: string | null
  onFolderClick: (folder: PlannerFile) => void
  onFileClick: (file: PlannerFile) => void
  onFileDelete: (file: PlannerFile) => void
  onFolderDelete: (folder: PlannerFile) => void
  onDeleteAllFilesInFolder?: (folder: PlannerFile) => void
  onRetry: () => void
  expandedFolders: Set<string>
  loadingChildren: Set<string>
  chatFileContext: Array<{name: string, path: string, type: 'file' | 'folder'}>
  addFileToContext: (file: {name: string, path: string, type: 'file' | 'folder'}) => void
  highlightedFile?: string | null
  onFolderUpload?: (folderPath: string) => void
  onCreateFolder?: (parentFolder?: PlannerFile | string) => void
  onFileMove?: (file: PlannerFile) => void
  onFolderMove?: (folder: PlannerFile) => void
}

export default function PlannerFileList({
  files,
  loading,
  error,
  onFolderClick,
  onFileClick,
  onFileDelete,
  onFolderDelete,
  onDeleteAllFilesInFolder,
  onRetry,
  expandedFolders,
  loadingChildren,
  chatFileContext,
  addFileToContext,
  highlightedFile,
  onFolderUpload,
  onCreateFolder,
  onFileMove,
  onFolderMove
}: PlannerFileListProps) {
  const { scrollToFile } = useWorkspaceStore()

  // Render a single item (file or folder) with proper hierarchy
  const renderFileItem = (file: PlannerFile, depth: number = 0) => {
    const isExpanded = expandedFolders.has(file.filepath)
    const isLoadingChildren = loadingChildren.has(file.filepath)
    const isClickable = file.type === 'folder' || file.type === 'file' || !file.type
    const fileName = file.filepath.split('/').pop() || file.filepath
    // Check both filepath (adjusted for display) and originalFilepath (original path)
    // This ensures workspace tool events can highlight files even when paths are adjusted in workflow mode
    const isHighlighted = highlightedFile === file.filepath || highlightedFile === file.originalFilepath
    const isInContext = chatFileContext.some(ctx => ctx.path === file.filepath)

    return (
      <div key={file.filepath} className="select-none">
        <div
          className={`
            flex items-center gap-2 p-2 rounded-md cursor-pointer transition-colors
            ${isClickable ? 'hover:bg-gray-100 dark:hover:bg-gray-800' : 'cursor-default'}
            ${isHighlighted ? 'bg-blue-100 dark:bg-blue-900/30 border border-blue-300 dark:border-blue-700' : ''}
            ${isInContext ? 'bg-green-50 dark:bg-green-900/20 border-l-2 border-green-500' : ''}
          `}
          style={{ paddingLeft: `${depth * 16 + 8}px` }}
          data-filepath={file.filepath}
          data-original-filepath={file.originalFilepath || undefined}
          data-highlighted={isHighlighted ? 'true' : 'false'}
          onClick={() => {
            if (file.type === 'folder') {
              onFolderClick(file)
            } else if (file.type === 'file' || !file.type) {
              onFileClick(file)
            }
          }}
        >
          {/* File/Folder Icon with expansion indicator */}
          <div className="flex-shrink-0">
            {file.type === 'folder' ? (
              isExpanded ? (
                <ChevronDown className="w-4 h-4 text-blue-500" />
              ) : (
                <ChevronRight className="w-4 h-4 text-blue-500" />
              )
            ) : file.is_image ? (
              <Image className="w-4 h-4 text-green-600" />
            ) : (
              <FileText className="w-4 h-4 text-gray-600" />
            )}
          </div>

          {/* File Name - with reserved space for icons */}
          <div className="flex-1 min-w-0 max-w-[calc(100%-80px)]">
            <span className="text-sm font-medium text-gray-900 dark:text-gray-100 truncate block">
              {fileName}
            </span>
          </div>

          {/* Loading indicator for children */}
          {file.type === 'folder' && isLoadingChildren && (
            <Loader2 className="w-4 h-4 text-gray-400 animate-spin flex-shrink-0" />
          )}

          {/* Action buttons container - compact space */}
          <div className="flex items-center gap-1 flex-shrink-0">
            {/* Send to Chat button - always visible */}
            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  onClick={(e) => {
                    e.stopPropagation()
                    // Use the filepath as-is for context
                    addFileToContext({
                      name: fileName,
                      path: file.filepath,
                      type: (file.type || 'file') as 'file' | 'folder'
                    })
                    
                    // Auto-scroll to the file in workspace
                    scrollToFile(file.filepath)
                  }}
                  className="p-1 hover:bg-blue-100 dark:hover:bg-blue-900/20 rounded text-blue-500 hover:text-blue-700 dark:hover:text-blue-400"
                >
                  <MessageSquare className="w-3 h-3" />
                </button>
              </TooltipTrigger>
              <TooltipContent>
                <p>Send {file.type || 'file'} to chat context</p>
              </TooltipContent>
            </Tooltip>

            {/* More actions dropdown for folders */}
            {file.type === 'folder' && (onCreateFolder || onFolderUpload || onFolderMove) && (
              <div className="relative group">
                <Tooltip>
                  <TooltipTrigger asChild>
                    <button
                      onClick={(e) => {
                        e.stopPropagation()
                        // Toggle dropdown - we'll handle this with CSS
                      }}
                      className="p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 transition-colors"
                    >
                      <MoreHorizontal className="w-3 h-3" />
                    </button>
                  </TooltipTrigger>
                  <TooltipContent>
                    <p>More actions</p>
                  </TooltipContent>
                </Tooltip>
                
                {/* Dropdown menu */}
                <div className="absolute right-0 top-full mt-1 w-32 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md shadow-lg opacity-0 invisible group-hover:opacity-100 group-hover:visible transition-all duration-200 z-50">
                  <div className="py-1">
                    {onCreateFolder && (
                      <button
                        onClick={(e) => {
                          e.stopPropagation()
                          onCreateFolder(file)
                        }}
                        className="w-full px-3 py-1 text-left text-xs text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 flex items-center gap-2"
                      >
                        <Plus className="w-3 h-3" />
                        Create Folder
                      </button>
                    )}
                    {onFolderUpload && (
                      <button
                        onClick={(e) => {
                          e.stopPropagation()
                          onFolderUpload(file.filepath)
                        }}
                        className="w-full px-3 py-1 text-left text-xs text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 flex items-center gap-2"
                      >
                        <Upload className="w-3 h-3" />
                        Upload File
                      </button>
                    )}
                    {onFolderMove && (
                      <button
                        onClick={(e) => {
                          e.stopPropagation()
                          onFolderMove(file)
                        }}
                        className="w-full px-3 py-1 text-left text-xs text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 flex items-center gap-2"
                      >
                        <Move className="w-3 h-3" />
                        Move
                      </button>
                    )}
                    {onDeleteAllFilesInFolder && (
                      <button
                        onClick={(e) => {
                          e.stopPropagation()
                          onDeleteAllFilesInFolder(file)
                        }}
                        className="w-full px-3 py-1 text-left text-xs text-orange-600 dark:text-orange-400 hover:bg-orange-50 dark:hover:bg-orange-900/20 flex items-center gap-2"
                      >
                        <Trash2 className="w-3 h-3" />
                        Delete All Contents
                      </button>
                    )}
                    <button
                      onClick={(e) => {
                        e.stopPropagation()
                        onFolderDelete(file)
                      }}
                      className="w-full px-3 py-1 text-left text-xs text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20 flex items-center gap-2"
                    >
                      <Trash2 className="w-3 h-3" />
                      Delete
                    </button>
                  </div>
                </div>
              </div>
            )}

            {/* Move button for files */}
            {file.type !== 'folder' && onFileMove && (
              <Tooltip>
                <TooltipTrigger asChild>
                  <button
                    onClick={(e) => {
                      e.stopPropagation()
                      onFileMove(file)
                    }}
                    className="p-1 text-gray-400 hover:text-blue-500 transition-colors"
                  >
                    <Move className="w-3 h-3" />
                  </button>
                </TooltipTrigger>
                <TooltipContent>
                  <p>Move file</p>
                </TooltipContent>
              </Tooltip>
            )}

            {/* Delete button for files */}
            {file.type !== 'folder' && (
              <Tooltip>
                <TooltipTrigger asChild>
                  <button
                    onClick={(e) => {
                      e.stopPropagation()
                      onFileDelete(file)
                    }}
                    className="p-1 text-gray-400 hover:text-red-500 transition-colors"
                  >
                    <Trash2 className="w-3 h-3" />
                  </button>
                </TooltipTrigger>
                <TooltipContent>
                  <p>Delete file</p>
                </TooltipContent>
              </Tooltip>
            )}
          </div>
        </div>

        {/* Render children if folder is expanded */}
        {file.type === 'folder' && isExpanded && file.children && (
          <div>
            {file.children
              .sort((a, b) => {
                // If both are folders or both are files, sort alphabetically
                if (a.type === b.type) {
                  return a.filepath.localeCompare(b.filepath)
                }
                // Folders come first
                if (a.type === 'folder') return -1
                if (b.type === 'folder') return 1
                return 0
              })
              .map(child => renderFileItem(child, depth + 1))}
          </div>
        )}
      </div>
    )
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center p-8">
        <Loader2 className="w-6 h-6 animate-spin text-gray-500" />
        <span className="ml-2 text-sm text-gray-500">Loading files...</span>
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex flex-col items-center justify-center p-8 text-center">
        <AlertCircle className="w-8 h-8 text-red-500 mb-2" />
        <p className="text-sm text-red-600 dark:text-red-400 mb-4">{error}</p>
        <button
          onClick={onRetry}
          className="px-4 py-2 text-sm bg-red-500 text-white rounded-md hover:bg-red-600 transition-colors"
        >
          Retry
        </button>
      </div>
    )
  }

  if (files.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center p-8 text-center">
        <Folder className="w-8 h-8 text-gray-400 mb-2" />
        <p className="text-sm text-gray-500">No files found</p>
      </div>
    )
  }

  // Sort files to show folders first, then files
  const sortedFiles = [...files].sort((a, b) => {
    // If both are folders or both are files, sort alphabetically
    if (a.type === b.type) {
      return a.filepath.localeCompare(b.filepath)
    }
    // Folders come first
    if (a.type === 'folder') return -1
    if (b.type === 'folder') return 1
    return 0
  })

  return (
    <TooltipProvider>
      <div className="space-y-1">
        {sortedFiles.map(file => renderFileItem(file))}
      </div>
    </TooltipProvider>
  )
}
