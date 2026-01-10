import type { PlannerFile } from '../services/api-types'

/**
 * Utility functions for handling workspace paths in different modes (chat vs workflow)
 * Handles path normalization, adjustment, and mapping between original and adjusted paths
 */

/**
 * Normalize a path for comparison (lowercase, remove leading/trailing slashes)
 */
export function normalizePathForComparison(path: string): string {
  return path.toLowerCase().replace(/^\/+|\/+$/g, '')
}

/**
 * Check if a file path is within a folder path
 */
export function isPathWithinFolder(filepath: string, folderPath: string): boolean {
  const normalizedFile = normalizePathForComparison(filepath)
  const normalizedFolder = normalizePathForComparison(folderPath)
  
  return normalizedFile === normalizedFolder || 
         normalizedFile.startsWith(normalizedFolder + '/')
}

/**
 * Get adjusted path (workflow folder prefix removed) for display in workflow mode
 */
export function getAdjustedPath(filepath: string, workflowFolderPath: string): string {
  const filePathNormalized = normalizePathForComparison(filepath)
  const workflowPathNormalized = normalizePathForComparison(workflowFolderPath)
  
  // If this is the workflow folder itself, return just the folder name
  if (filePathNormalized === workflowPathNormalized) {
    const folderParts = workflowFolderPath.split('/').filter(Boolean)
    return folderParts.length > 0 ? folderParts[folderParts.length - 1] : filepath
  }
  
  // If this is within the workflow folder, remove the prefix
  if (filePathNormalized.startsWith(workflowPathNormalized + '/')) {
    const remaining = filepath.slice(workflowFolderPath.length)
    const adjustedPath = remaining.startsWith('/') ? remaining.slice(1) : remaining
    
    if (!adjustedPath) {
      const folderParts = workflowFolderPath.split('/').filter(Boolean)
      return folderParts.length > 0 ? folderParts[folderParts.length - 1] : filepath
    }
    
    return adjustedPath
  }
  
  // Not within workflow folder, return as-is
  return filepath
}

/**
 * Get original path from adjusted path (add workflow folder prefix back)
 */
export function getOriginalPath(adjustedPath: string, workflowFolderPath: string): string {
  const normalizedAdjusted = normalizePathForComparison(adjustedPath)
  const normalizedWorkflow = normalizePathForComparison(workflowFolderPath)
  
  // If it already starts with the workflow path, it's already original
  if (normalizedAdjusted.startsWith(normalizedWorkflow)) {
    return adjustedPath
  }
  
  // Add workflow prefix
  if (adjustedPath.startsWith('/')) {
    return `${workflowFolderPath}${adjustedPath}`
  } else {
    return `${workflowFolderPath}/${adjustedPath}`
  }
}

/**
 * Check if a file should be included in workflow filter
 */
export function shouldIncludeInWorkflowFilter(
  filepath: string, 
  workflowFolderPath: string
): boolean {
  const normalizedPath = normalizePathForComparison(filepath)
  const normalizedWorkflow = normalizePathForComparison(workflowFolderPath)
  
  // Check if within workflow folder
  if (normalizedPath === normalizedWorkflow || normalizedPath.startsWith(normalizedWorkflow + '/')) {
    return true
  }
  
  return false
}

/**
 * Collect all folder paths from a file tree (including both adjusted and original paths)
 */
export function collectFolderPaths(files: PlannerFile[], includeOriginal: boolean = true): Set<string> {
  const paths = new Set<string>()
  
  const collect = (fileList: PlannerFile[]) => {
    fileList.forEach(file => {
      if (file.type === 'folder') {
        // Add adjusted path
        paths.add(file.filepath)
        
        // Add original path if available
        if (includeOriginal && 'originalFilepath' in file && file.originalFilepath) {
          paths.add(file.originalFilepath)
        }
        
        // Recurse into children
        if (file.children) {
          collect(file.children)
        }
      }
    })
  }
  
  collect(files)
  return paths
}

/**
 * Find a file in the tree by checking both filepath and originalFilepath
 */
export function findFileByPath(files: PlannerFile[], targetPath: string): PlannerFile | null {
  for (const file of files) {
    if (file.filepath === targetPath || ('originalFilepath' in file && file.originalFilepath === targetPath)) {
      return file
    }
    
    if (file.children && file.children.length > 0) {
      const found = findFileByPath(file.children, targetPath)
      if (found) return found
    }
  }
  
  return null
}

/**
 * Restore expanded folders by matching against available folder paths
 * Handles both adjusted and original path formats
 */
export function restoreExpandedFolders(
  previouslyExpanded: Set<string>,
  availableFolderPaths: Set<string>
): Set<string> {
  const restored = new Set<string>()
  
  previouslyExpanded.forEach(folderPath => {
    // Check if this path exists in available paths
    if (availableFolderPaths.has(folderPath)) {
      restored.add(folderPath)
    }
  })
  
  return restored
}

/**
 * Extract folder paths that need to be expanded to show a file
 */
export function extractFolderPathsForFile(filepath: string): string[] {
  const parts = filepath.split('/').filter(Boolean)
  const folders: string[] = []
  
  for (let i = 0; i < parts.length - 1; i++) {
    folders.push(parts.slice(0, i + 1).join('/'))
  }
  
  return folders
}

/**
 * Normalize a path for comparison (lowercase, remove leading/trailing slashes)
 * Alias for normalizePathForComparison for backward compatibility
 */
export const normalizePath = normalizePathForComparison

/**
 * Find a folder in the file tree by path (checks both filepath and originalFilepath)
 */
export function findFolderInTree(fileList: PlannerFile[], targetPath: string): PlannerFile | null {
  for (const file of fileList) {
    if (file.type === 'folder' && (file.filepath === targetPath || ('originalFilepath' in file && file.originalFilepath === targetPath))) {
      return file
    }
    if (file.children && file.children.length > 0) {
      const found = findFolderInTree(file.children, targetPath)
      if (found) return found
    }
  }
  return null
}

/**
 * Recursively find all iteration folders in the tree
 * Matches pattern: runs/iteration-* or iteration-* (with optional group subfolders)
 */
export function findIterationFolders(fileList: PlannerFile[]): string[] {
  const iterationFolders: string[] = []
  
  for (const file of fileList) {
    if (file.type === 'folder') {
      // Check if this is an iteration folder (matches pattern: runs/iteration-* or iteration-*)
      // Also handle group subfolders: runs/iteration-*/group-* or runs/iteration-*/display-name
      // Accepts both "group-X" format and display names (any alphanumeric/dash folder name)
      if (file.filepath.match(/^runs\/iteration-\d+(\/[a-zA-Z0-9_-]+)?$/) || file.filepath.match(/^iteration-\d+(\/[a-zA-Z0-9_-]+)?$/)) {
        iterationFolders.push(file.filepath)
      }
      
      // Recursively search children
      if (file.children && file.children.length > 0) {
        const childIterations = findIterationFolders(file.children)
        iterationFolders.push(...childIterations)
      }
    }
  }
  
  return iterationFolders
}

/**
 * Check if a path matches an iteration folder pattern
 */
export function isIterationFolder(path: string): boolean {
  return !!(
    path.match(/^runs\/iteration-\d+(\/[a-zA-Z0-9_-]+)?$/) || 
    path.match(/^iteration-\d+(\/[a-zA-Z0-9_-]+)?$/)
  )
}

/**
 * Adjust file paths recursively to show workflow folder as root
 * Stores original path in originalFilepath for API calls
 */
export function adjustFilePathsRecursive(
  fileList: PlannerFile[],
  workflowFolderPath: string
): PlannerFile[] {
  return fileList.map(file => {
    const adjustedFilepath = getAdjustedPath(file.filepath, workflowFolderPath)
    const originalFilepath = file.filepath // Store original before adjustment
    
    if (file.type === 'folder') {
      return {
        ...file,
        filepath: adjustedFilepath,
        originalFilepath: originalFilepath,
        children: file.children ? adjustFilePathsRecursive(file.children, workflowFolderPath) : []
      }
    }
    
    return {
      ...file,
      filepath: adjustedFilepath,
      originalFilepath: originalFilepath
    }
  })
}
