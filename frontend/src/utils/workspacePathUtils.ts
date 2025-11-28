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
  workflowFolderPath: string, 
  includeDownloads: boolean = false
): boolean {
  const normalizedPath = normalizePathForComparison(filepath)
  const normalizedWorkflow = normalizePathForComparison(workflowFolderPath)
  const normalizedDownloads = normalizePathForComparison('Downloads')
  
  // Check if within workflow folder
  if (normalizedPath === normalizedWorkflow || normalizedPath.startsWith(normalizedWorkflow + '/')) {
    return true
  }
  
  // Check if within Downloads folder (if including)
  if (includeDownloads && (normalizedPath === normalizedDownloads || normalizedPath.startsWith(normalizedDownloads + '/'))) {
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

