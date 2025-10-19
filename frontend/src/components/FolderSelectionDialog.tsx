import React, { useState, useEffect, useRef, useCallback } from 'react'
import { Folder, Search, ChevronRight, ChevronDown, FolderPlus } from 'lucide-react'
import type { PlannerFile } from '../services/api-types'
import { useWorkspaceStore } from '../stores/useWorkspaceStore'
import CreateFolderDialog from './workspace/CreateFolderDialog'
import { agentApi } from '../services/api'

interface FolderSelectionDialogProps {
  isOpen: boolean
  onClose: () => void
  onSelectFolder: (folder: PlannerFile) => void
  searchQuery: string
  position: { top: number; left: number }
  agentMode?: 'simple' | 'ReAct' | 'orchestrator' | 'workflow' // Add agent mode to filter folders
}

export const FolderSelectionDialog: React.FC<FolderSelectionDialogProps> = ({
  isOpen,
  onClose,
  onSelectFolder,
  searchQuery,
  position,
  agentMode
}) => {
  const { files, fetchFiles } = useWorkspaceStore()
  const [selectedIndex, setSelectedIndex] = useState(0)
  const [filteredFolders, setFilteredFolders] = useState<PlannerFile[]>([])
  const [expandedFolders, setExpandedFolders] = useState<Set<string>>(new Set())
  const [showCreateFolderDialog, setShowCreateFolderDialog] = useState(false)
  const [isCreatingFolder, setIsCreatingFolder] = useState(false)
  const dialogRef = useRef<HTMLDivElement>(null)
  const listRef = useRef<HTMLDivElement>(null)

  // Keyboard shortcuts
  useEffect(() => {
    if (!isOpen) return

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        // Don't close main dialog if create folder dialog is open
        if (showCreateFolderDialog) {
          return
        }
        onClose()
      } else if (event.key === 'Enter') {
        event.preventDefault()
        if (filteredFolders.length > 0 && selectedIndex >= 0 && selectedIndex < filteredFolders.length) {
          onSelectFolder(filteredFolders[selectedIndex])
        }
      } else if (event.key === 'ArrowDown') {
        event.preventDefault()
        setSelectedIndex(prev => Math.min(prev + 1, filteredFolders.length - 1))
      } else if (event.key === 'ArrowUp') {
        event.preventDefault()
        setSelectedIndex(prev => Math.max(prev - 1, 0))
      }
    }

    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [isOpen, onClose, onSelectFolder, filteredFolders, selectedIndex, showCreateFolderDialog])

  // Get the appropriate parent path for folder creation based on agent mode
  const getParentPathForCreation = useCallback((): string => {
    if (!agentMode || agentMode === 'simple' || agentMode === 'ReAct') {
      return '' // Root level for simple modes
    }
    
    if (agentMode === 'workflow') {
      return 'Workflow'
    }
    
    if (agentMode === 'orchestrator') {
      return 'Tasks'
    }
    
    return ''
  }, [agentMode])

  // Handle create folder button click
  const handleCreateFolderClick = useCallback(() => {
    setShowCreateFolderDialog(true)
  }, [])

  // Handle folder creation
  const handleCreateFolder = useCallback(async (folderPath: string, commitMessage?: string) => {
    setIsCreatingFolder(true)
    
    try {
      await agentApi.createPlannerFolder(folderPath, commitMessage)
      
      // Refresh the file list to show the new folder
      await fetchFiles()
      
      // Close the create folder dialog
      setShowCreateFolderDialog(false)
      
      // Don't automatically select and close - let user choose
      // The folder will now appear in the list for manual selection
    } catch (error) {
      console.error('Failed to create folder:', error)
      throw error // Re-throw to let CreateFolderDialog handle the error
    } finally {
      setIsCreatingFolder(false)
    }
  }, [fetchFiles])

  // Handle create folder dialog close
  const handleCreateFolderDialogClose = useCallback(() => {
    if (!isCreatingFolder) {
      setShowCreateFolderDialog(false)
    }
  }, [isCreatingFolder])

  // Filter folders based on agent mode while maintaining hierarchy
  const filterFoldersByAgentMode = useCallback((files: PlannerFile[]): PlannerFile[] => {
    if (!agentMode || agentMode === 'simple' || agentMode === 'ReAct') {
      // For simple and ReAct modes, show all folders
      return files
    }

    const targetPrefix = agentMode === 'workflow' ? 'Workflow/' : 'Tasks/'
    
    // Recursively filter folders while maintaining hierarchy
    const filterHierarchy = (fileList: PlannerFile[]): PlannerFile[] => {
      const result: PlannerFile[] = []
      
      for (const file of fileList) {
        if (file.type === 'folder') {
          // If this folder starts with target prefix, include it and its children
          if (file.filepath.startsWith(targetPrefix)) {
            const filteredChildren = file.children ? filterHierarchy(file.children) : []
            result.push({
              ...file,
              children: filteredChildren
            })
          } else {
            // If this folder doesn't match, check its children recursively
            if (file.children) {
              const filteredChildren = filterHierarchy(file.children)
              if (filteredChildren.length > 0) {
                // Only include this folder if it has matching children
                result.push({
                  ...file,
                  children: filteredChildren
                })
              }
            }
          }
        } else if (file.children) {
          // If it's a file but has children, still traverse children
          const filteredChildren = filterHierarchy(file.children)
          if (filteredChildren.length > 0) {
            result.push({
              ...file,
              children: filteredChildren
            })
          }
        }
      }
      
      return result
    }
    
    return filterHierarchy(files)
  }, [agentMode])

  // Calculate fuzzy match score (how close the characters are together)
  const calculateFuzzyScore = (filepath: string, query: string): number => {
    let score = 0
    let queryIndex = 0
    let lastMatchIndex = -1
    
    for (let i = 0; i < filepath.length && queryIndex < query.length; i++) {
      if (filepath[i] === query[queryIndex]) {
        // Bonus for consecutive matches
        if (i === lastMatchIndex + 1) {
          score += 10
        }
        // Bonus for matches at word boundaries (after / or at start)
        if (i === 0 || filepath[i - 1] === '/') {
          score += 5
        }
        // Penalty for distance from last match
        if (lastMatchIndex >= 0) {
          score += Math.max(0, 10 - (i - lastMatchIndex))
        }
        
        lastMatchIndex = i
        queryIndex++
      }
    }
    
    return score
  }

  // Flatten hierarchical structure while respecting expanded folders (folders only)
  const flattenWithExpandedFolders = (files: PlannerFile[], expandedFolders: Set<string>): PlannerFile[] => {
    const result: PlannerFile[] = []
    
    const flatten = (fileList: PlannerFile[], depth = 0) => {
      for (const file of fileList) {
        // Only include folders
        if (file.type === 'folder') {
          result.push({ ...file, depth })
          
          // If it's expanded, add its children
          if (expandedFolders.has(file.filepath) && file.children) {
            flatten(file.children, depth + 1)
          }
        } else if (file.children) {
          // If it's a file but has children, still traverse children
          flatten(file.children, depth)
        }
      }
    }
    
    flatten(files)
    return result
  }

  // Filter folders based on search query with VS Code-style fuzzy matching
  useEffect(() => {
    // First filter by agent mode
    const agentModeFilteredFiles = filterFoldersByAgentMode(files)
    
    // Auto-expand root folders for better hierarchy visibility
    const autoExpandRootFolders = (fileList: PlannerFile[]): Set<string> => {
      const newExpanded = new Set(expandedFolders)
      fileList.forEach(file => {
        if (file.type === 'folder') {
          // Auto-expand root level folders for workflow/tasks modes
          if (agentMode === 'workflow' || agentMode === 'orchestrator') {
            const targetPrefix = agentMode === 'workflow' ? 'Workflow/' : 'Tasks/'
            if (file.filepath === targetPrefix.slice(0, -1)) {
              newExpanded.add(file.filepath)
            }
          }
        }
      })
      return newExpanded
    }
    
    const expandedFoldersWithAuto = autoExpandRootFolders(agentModeFilteredFiles)
    
    if (!searchQuery.trim()) {
      // Show hierarchical structure when no search, respecting expanded folders
      const flattened = flattenWithExpandedFolders(agentModeFilteredFiles, expandedFoldersWithAuto)
      setFilteredFolders(flattened)
      return
    }

    const query = searchQuery.toLowerCase().trim()
    
    // For search, we need to flatten but also maintain some hierarchy context
    // We'll show search results with their full paths but still allow expansion
    const flattenFolders = (files: PlannerFile[]): PlannerFile[] => {
      const result: PlannerFile[] = []
      
      const flatten = (fileList: PlannerFile[]) => {
        for (const file of fileList) {
          // Only include folders in search results
          if (file.type === 'folder') {
            result.push(file)
          }
          if (file.children && file.children.length > 0) {
            flatten(file.children)
          }
        }
      }
      
      flatten(files)
      return result
    }
    
    const allFolders = flattenFolders(agentModeFilteredFiles)
    
    const filtered = allFolders.filter(folder => {
      // Filter by filepath with fuzzy matching (like VS Code) - folders only
      const filepath = folder.filepath.toLowerCase()
      
      if (!query) {
        return true // Show all folders if no query
      }
      
      // VS Code-style fuzzy search: find query characters in order within the filepath
      let queryIndex = 0
      for (let i = 0; i < filepath.length && queryIndex < query.length; i++) {
        if (filepath[i] === query[queryIndex]) {
          queryIndex++
        }
      }
      
      // All query characters must be found in order
      const fuzzyMatch = queryIndex === query.length
      
      // Fallback: simple substring match for better user experience
      const substringMatch = filepath.includes(query)
      
      return fuzzyMatch || substringMatch
    })
    
    // Sort by relevance: exact matches first, then partial matches
    const sorted = filtered.sort((a, b) => {
      const aPath = a.filepath.toLowerCase()
      const bPath = b.filepath.toLowerCase()
      
      // Exact match bonus
      const aExact = aPath === query ? 1 : 0
      const bExact = bPath === query ? 1 : 0
      
      // Starts with query bonus
      const aStartsWith = aPath.startsWith(query) ? 1 : 0
      const bStartsWith = bPath.startsWith(query) ? 1 : 0
      
      // Folder name match bonus (last part of path)
      const aFolderName = aPath.split('/').pop() || ''
      const bFolderName = bPath.split('/').pop() || ''
      const aFolderNameMatch = aFolderName.includes(query) ? 1 : 0
      const bFolderNameMatch = bFolderName.includes(query) ? 1 : 0
      
      // Fuzzy match score (how close the characters are together)
      const aFuzzyScore = calculateFuzzyScore(aPath, query)
      const bFuzzyScore = calculateFuzzyScore(bPath, query)
      
      // Calculate final score
      const aScore = aExact * 100 + aStartsWith * 50 + aFolderNameMatch * 25 + aFuzzyScore * 10
      const bScore = bExact * 100 + bStartsWith * 50 + bFolderNameMatch * 25 + bFuzzyScore * 10
      
      return bScore - aScore
    }) // Show all search results

    setFilteredFolders(sorted)
    setSelectedIndex(0) // Reset selection when filtering
  }, [files, searchQuery, expandedFolders, agentMode, filterFoldersByAgentMode])

  // Handle keyboard navigation
  const handleKeyDown = useCallback((e: KeyboardEvent) => {
    if (!isOpen) return

    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault()
        setSelectedIndex(prev => 
          prev < filteredFolders.length - 1 ? prev + 1 : 0
        )
        break
      case 'ArrowUp':
        e.preventDefault()
        setSelectedIndex(prev => 
          prev > 0 ? prev - 1 : filteredFolders.length - 1
        )
        break
      case 'ArrowRight': {
        e.preventDefault()
        const selectedItem = filteredFolders[selectedIndex]
        if (selectedItem && selectedItem.type === 'folder') {
          // Toggle folder expansion
          setExpandedFolders(prev => {
            const newSet = new Set(prev)
            if (newSet.has(selectedItem.filepath)) {
              newSet.delete(selectedItem.filepath)
            } else {
              newSet.add(selectedItem.filepath)
            }
            return newSet
          })
        }
        break
      }
      case 'Enter':
        e.preventDefault()
        if (filteredFolders[selectedIndex]) {
          onSelectFolder(filteredFolders[selectedIndex])
        }
        break
      case 'Escape':
        e.preventDefault()
        // Don't close main dialog if create folder dialog is open
        if (showCreateFolderDialog) {
          return
        }
        onClose()
        break
    }
  }, [isOpen, filteredFolders, selectedIndex, onSelectFolder, onClose, showCreateFolderDialog])

  // Add keyboard event listeners
  useEffect(() => {
    if (isOpen) {
      document.addEventListener('keydown', handleKeyDown)
      return () => {
        document.removeEventListener('keydown', handleKeyDown)
      }
    }
  }, [isOpen, handleKeyDown])

  // Scroll selected item into view
  useEffect(() => {
    if (listRef.current && selectedIndex >= 0) {
      const selectedElement = listRef.current.children[selectedIndex] as HTMLElement
      if (selectedElement) {
        selectedElement.scrollIntoView({
          block: 'nearest',
          behavior: 'smooth'
        })
      }
    }
  }, [selectedIndex])

  // Close dialog when clicking outside
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (dialogRef.current && !dialogRef.current.contains(event.target as Node)) {
        // Don't close if the create folder dialog is open
        if (showCreateFolderDialog) {
          return
        }
        onClose()
      }
    }

    if (isOpen) {
      document.addEventListener('mousedown', handleClickOutside)
      return () => {
        document.removeEventListener('mousedown', handleClickOutside)
      }
    }
  }, [isOpen, onClose, showCreateFolderDialog])

  // Handle folder click - expand/collapse or select
  const handleFolderClick = useCallback((folder: PlannerFile, event: React.MouseEvent) => {
    // Check if the click was on the chevron icon
    const target = event.target as HTMLElement
    const isChevronClick = target.closest('[data-chevron]')
    
    if (isChevronClick) {
      // Toggle folder expansion
      setExpandedFolders(prev => {
        const newSet = new Set(prev)
        if (newSet.has(folder.filepath)) {
          newSet.delete(folder.filepath)
        } else {
          newSet.add(folder.filepath)
        }
        return newSet
      })
    } else {
      // Select the folder
      onSelectFolder(folder)
    }
  }, [onSelectFolder])

  // Get display name for folder based on agent mode
  const getDisplayName = (folder: PlannerFile): string => {
    if (!agentMode || agentMode === 'simple' || agentMode === 'ReAct') {
      return folder.filepath
    }
    
    const targetPrefix = agentMode === 'workflow' ? 'Workflow/' : 'Tasks/'
    if (folder.filepath.startsWith(targetPrefix)) {
      const relativePath = folder.filepath.substring(targetPrefix.length)
      // If it's the root folder (empty relative path), show the prefix without trailing slash
      return relativePath || targetPrefix.slice(0, -1)
    }
    
    return folder.filepath
  }

  // Calculate display depth based on agent mode
  const getDisplayDepth = (folder: PlannerFile): number => {
    // Use the actual depth from the flattened structure
    return folder.depth || 0
  }

  // Get folder icon
  const getFolderIcon = () => {
    return <Folder className="w-4 h-4 text-primary" />
  }

  // Highlight matching parts of the filepath
  const highlightMatch = (filepath: string, query: string): React.ReactNode => {
    if (!query.trim()) {
      return filepath
    }

    const queryLower = query.toLowerCase()
    const filepathLower = filepath.toLowerCase()
    
    // Find all matches
    const matches: number[] = []
    let index = filepathLower.indexOf(queryLower)
    while (index !== -1) {
      matches.push(index)
      index = filepathLower.indexOf(queryLower, index + 1)
    }

    if (matches.length === 0) {
      return filepath
    }

    const parts: React.ReactNode[] = []
    let lastIndex = 0

    matches.forEach(matchIndex => {
      // Add text before match
      if (matchIndex > lastIndex) {
        parts.push(filepath.substring(lastIndex, matchIndex))
      }
      
      // Add highlighted match
      parts.push(
        <span key={matchIndex} className="bg-warning/20 text-warning">
          {filepath.substring(matchIndex, matchIndex + query.length)}
        </span>
      )
      
      lastIndex = matchIndex + query.length
    })

    // Add remaining text
    if (lastIndex < filepath.length) {
      parts.push(filepath.substring(lastIndex))
    }

    return parts
  }

  if (!isOpen) return null

  return (
    <React.Fragment>
    <div
      ref={dialogRef}
      className="fixed z-50 bg-background border border-border rounded-lg shadow-lg max-w-md w-full max-h-80 overflow-hidden"
      style={{
        top: position.top,
        left: position.left
      }}
    >
      {/* Header */}
      <div className="px-3 py-2 border-b border-border bg-secondary">
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Search className="w-4 h-4" />
          <span>
            {agentMode === 'workflow' ? 'Select Workflow Folder' : 
             agentMode === 'orchestrator' ? 'Select Tasks Folder' : 
             'Select folder'}
          </span>
          {searchQuery && (
            <span className="text-muted-foreground">• {filteredFolders.length} folders</span>
          )}
        </div>
        {agentMode === 'workflow' && (
          <p className="text-xs text-muted-foreground mt-1">
            Showing folders inside Workflow/
          </p>
        )}
        {agentMode === 'orchestrator' && (
          <p className="text-xs text-muted-foreground mt-1">
            Showing folders inside Tasks/
          </p>
        )}
      </div>

      {/* Folder List */}
      <div
        ref={listRef}
        className="overflow-y-auto max-h-64"
      >
        {filteredFolders.length === 0 ? (
          <div className="px-3 py-4 text-center text-muted-foreground text-sm">
            {searchQuery ? 'No folders found' : 'No folders available'}
          </div>
        ) : (
          filteredFolders.map((folder, index) => (
            <div
              key={folder.filepath}
              className={`px-3 py-2 cursor-pointer flex items-center gap-2 text-sm transition-colors ${
                index === selectedIndex
                  ? 'bg-primary/10 text-primary border-l-2 border-primary'
                  : 'hover:bg-secondary'
              }`}
              onClick={(e) => handleFolderClick(folder, e)}
              style={{ paddingLeft: `${12 + getDisplayDepth(folder) * 16}px` }}
            >
              {/* Folder icon with proper spacing */}
              <div className="flex-shrink-0 w-4 h-4 flex items-center justify-center">
                {getFolderIcon()}
              </div>
              <div className="flex-1 min-w-0">
                <div className="truncate">
                  {highlightMatch(getDisplayName(folder), searchQuery)}
                </div>
              </div>
              {folder.type === 'folder' && (
                expandedFolders.has(folder.filepath) ? (
                  <ChevronDown className="w-3 h-3 text-muted-foreground" data-chevron />
                ) : (
                  <ChevronRight className="w-3 h-3 text-muted-foreground" data-chevron />
                )
              )}
            </div>
          ))
        )}
      </div>

      {/* Footer */}
      <div className="px-3 py-2 border-t border-border bg-secondary">
        <div className="flex items-center justify-between mb-2">
          <span className="text-xs text-muted-foreground">↑↓ to navigate • → to expand folders</span>
          <span className="text-xs text-muted-foreground">Enter to select • Esc to close</span>
        </div>
        
        {/* Create New Folder Button */}
        <button
          onClick={handleCreateFolderClick}
          disabled={isCreatingFolder}
          className="w-full flex items-center justify-center gap-1.5 px-2 py-1.5 text-xs bg-primary/10 hover:bg-primary/20 text-primary border border-primary/20 rounded transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
        >
          <FolderPlus className="w-3 h-3" />
          <span>Create New Folder</span>
        </button>
        
        {agentMode && (agentMode === 'workflow' || agentMode === 'orchestrator') && (
          <p className="text-xs text-muted-foreground mt-1 text-center">
            Will be created in {agentMode === 'workflow' ? 'Workflow/' : 'Tasks/'}
          </p>
        )}
      </div>
    </div>

    {/* Create Folder Dialog */}
    <CreateFolderDialog
      isOpen={showCreateFolderDialog}
      onClose={handleCreateFolderDialogClose}
      onCreateFolder={handleCreateFolder}
      parentPath={getParentPathForCreation()}
    />
  </React.Fragment>
  )
}
