import React, { useState, useEffect, useRef, useCallback, useMemo } from 'react'
import { File, Folder, Search, ChevronRight, ChevronDown } from 'lucide-react'
import type { PlannerFile } from '../services/api-types'
import { useWorkspaceStore } from '../stores/useWorkspaceStore'

interface FileSelectionDialogProps {
  isOpen: boolean
  onClose: () => void
  onSelectFile: (file: PlannerFile) => void
  searchQuery: string
  position: { top: number; left: number }
}

export const FileSelectionDialog: React.FC<FileSelectionDialogProps> = ({
  isOpen,
  onClose,
  onSelectFile,
  searchQuery,
  position
}) => {
  const { files } = useWorkspaceStore()
  const [selectedIndex, setSelectedIndex] = useState(0)
  const [filteredFiles, setFilteredFiles] = useState<PlannerFile[]>([])
  const [expandedFolders, setExpandedFolders] = useState<Set<string>>(new Set())
  const dialogRef = useRef<HTMLDivElement>(null)
  const listRef = useRef<HTMLDivElement>(null)

  // Calculate fuzzy match score (how close the characters are together)
  const calculateFuzzyScore = useCallback((filepath: string, query: string): number => {
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
  }, [])

  // Memoize flattened files for search to avoid re-calculating on every search query change
  const allFlattenedFiles = useMemo(() => {
    const result: PlannerFile[] = []
    const flatten = (fileList: PlannerFile[]) => {
      for (const file of fileList) {
        result.push(file)
        if (file.children && file.children.length > 0) {
          flatten(file.children)
        }
      }
    }
    flatten(files)
    return result
  }, [files])

  // Flatten hierarchical structure while respecting expanded folders (for non-search view)
  const flattenWithExpandedFolders = useCallback((files: PlannerFile[], expandedFolders: Set<string>): PlannerFile[] => {
    const result: PlannerFile[] = []
    
    const flatten = (fileList: PlannerFile[], depth = 0) => {
      for (const file of fileList) {
        // Add the file/folder to result
        result.push({ ...file, depth })
        
        // If it's a folder and it's expanded, add its children
        if (file.type === 'folder' && expandedFolders.has(file.filepath) && file.children) {
          flatten(file.children, depth + 1)
        }
      }
    }
    
    flatten(files)
    return result
  }, [])

  // Filter files based on search query with VS Code-style fuzzy matching
  useEffect(() => {
    if (!searchQuery.trim()) {
      // Show hierarchical structure when no search, respecting expanded folders
      const flattened = flattenWithExpandedFolders(files, expandedFolders)
      setFilteredFiles(flattened)
      return
    }

    const query = searchQuery.toLowerCase().trim()
    
    const filtered = allFlattenedFiles.filter(file => {
      // Filter by filepath with fuzzy matching (like VS Code) - includes both files and folders
      const filepath = file.filepath.toLowerCase()
      
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
      
      // Filename match bonus (last part of path)
      const aFileName = aPath.split('/').pop() || ''
      const bFileName = bPath.split('/').pop() || ''
      const aFileNameMatch = aFileName.includes(query) ? 1 : 0
      const bFileNameMatch = bFileName.includes(query) ? 1 : 0
      
      // Fuzzy match score (how close the characters are together)
      const aFuzzyScore = calculateFuzzyScore(aPath, query)
      const bFuzzyScore = calculateFuzzyScore(bPath, query)
      
      // Calculate final score
      const aScore = aExact * 100 + aStartsWith * 50 + aFileNameMatch * 25 + aFuzzyScore * 10
      const bScore = bExact * 100 + bStartsWith * 50 + bFileNameMatch * 25 + bFuzzyScore * 10
      
      return bScore - aScore
    })

    setFilteredFiles(sorted)
    setSelectedIndex(0) // Reset selection when filtering
  }, [files, allFlattenedFiles, searchQuery, expandedFolders, flattenWithExpandedFolders, calculateFuzzyScore])

  // Limit displayed results to improve performance
  const displayLimit = 50
  const displayedFiles = useMemo(() => {
    return filteredFiles.slice(0, displayLimit)
  }, [filteredFiles, displayLimit])
  const remainingCount = Math.max(0, filteredFiles.length - displayLimit)

  // Handle keyboard navigation
  const handleKeyDown = useCallback((e: KeyboardEvent) => {
    if (!isOpen) return

    const visibleCount = displayedFiles.length

    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault()
        setSelectedIndex(prev => 
          prev < visibleCount - 1 ? prev + 1 : 0
        )
        break
      case 'ArrowUp':
        e.preventDefault()
        setSelectedIndex(prev => 
          prev > 0 ? prev - 1 : visibleCount - 1
        )
        break
      case 'ArrowRight': {
        e.preventDefault()
        const selectedItem = displayedFiles[selectedIndex]
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
        if (displayedFiles[selectedIndex]) {
          onSelectFile(displayedFiles[selectedIndex])
        }
        break
      case 'Escape':
        e.preventDefault()
        onClose()
        break
    }
  }, [isOpen, displayedFiles, selectedIndex, onSelectFile, onClose])


  // Add keyboard event listeners
  useEffect(() => {
    if (isOpen) {
      document.addEventListener('keydown', handleKeyDown)
      return () => document.removeEventListener('keydown', handleKeyDown)
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
        onClose()
      }
    }

    if (isOpen) {
      document.addEventListener('mousedown', handleClickOutside)
      return () => document.removeEventListener('mousedown', handleClickOutside)
    }
  }, [isOpen, onClose])

  if (!isOpen) return null

  const getFileIcon = (file: PlannerFile) => {
    if (file.type === 'folder') {
      return <Folder className="w-4 h-4 text-primary" />
    }
    
    // Get file extension for icon styling
    const extension = file.filepath.split('.').pop()?.toLowerCase()
    
    // Color code by file type using theme colors
    if (['js', 'ts', 'jsx', 'tsx'].includes(extension || '')) {
      return <File className="w-4 h-4 text-warning" />
    } else if (['py'].includes(extension || '')) {
      return <File className="w-4 h-4 text-success" />
    } else if (['go'].includes(extension || '')) {
      return <File className="w-4 h-4 text-primary" />
    } else if (['md', 'txt'].includes(extension || '')) {
      return <File className="w-4 h-4 text-muted-foreground" />
    } else if (['json', 'yaml', 'yml'].includes(extension || '')) {
      return <File className="w-4 h-4 text-primary" />
    } else {
      return <File className="w-4 h-4 text-muted-foreground" />
    }
  }

  const highlightMatch = (text: string, query: string) => {
    if (!query.trim()) return text
    
    const queryLower = query.toLowerCase()
    
    // Split query into parts for highlighting
    const queryParts = queryLower.split(/[/\\]/).filter(part => part.length > 0)
    
    if (queryParts.length === 0) return text
    
    // Create a regex that matches any of the query parts
    const regex = new RegExp(`(${queryParts.map(part => part.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('|')})`, 'gi')
    const parts = text.split(regex)
    
    return parts.map((part, index) => 
      regex.test(part) ? (
        <mark key={index} className="bg-warning/20 text-warning px-0.5 rounded">
          {part}
        </mark>
      ) : part
    )
  }

  return (
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
          <span>Select file or folder</span>
          {searchQuery && (
            <span className="text-muted-foreground">• {filteredFiles.length} results</span>
          )}
        </div>
      </div>

      {/* File List */}
      <div 
        ref={listRef}
        className="overflow-y-auto max-h-64"
      >
        {displayedFiles.length === 0 ? (
          <div className="px-3 py-4 text-center text-muted-foreground text-sm">
            {searchQuery ? 'No files found' : 'No files available'}
          </div>
        ) : (
          <>
            {displayedFiles.map((file, index) => (
              <div
                key={file.filepath}
                className={`px-3 py-2 cursor-pointer flex items-center gap-2 text-sm transition-colors ${
                  index === selectedIndex
                    ? 'bg-primary/10 text-primary border-l-2 border-primary'
                    : 'hover:bg-secondary'
                }`}
                onClick={() => onSelectFile(file)}
                style={{ paddingLeft: `${12 + (file.depth || 0) * 16}px` }}
              >
                {getFileIcon(file)}
                <div className="flex-1 min-w-0">
                  <div className="truncate">
                    {highlightMatch(file.filepath, searchQuery)}
                  </div>
                </div>
                {file.type === 'folder' && (
                  expandedFolders.has(file.filepath) ? (
                    <ChevronDown className="w-3 h-3 text-muted-foreground" />
                  ) : (
                    <ChevronRight className="w-3 h-3 text-muted-foreground" />
                  )
                )}
              </div>
            ))}
            {remainingCount > 0 && (
              <div className="px-3 py-2 text-center text-xs text-muted-foreground bg-secondary/30 italic border-t border-border/50">
                ...and {remainingCount} more results. Keep typing to narrow down.
              </div>
            )}
          </>
        )}
      </div>

      {/* Footer */}
      <div className="px-3 py-2 border-t border-border bg-secondary text-xs text-muted-foreground">
        <div className="flex items-center justify-between">
          <span>↑↓ to navigate • → to expand folders</span>
          <span>Enter to select • Esc to close</span>
        </div>
      </div>
    </div>
  )
}

export default FileSelectionDialog
