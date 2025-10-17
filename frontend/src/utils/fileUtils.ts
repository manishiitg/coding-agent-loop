import type { PlannerFile } from '../services/api-types'

/**
 * Processes hierarchical files from API response
 * @param files - Array of files from API
 * @returns Processed files with ensured type and children properties
 */
export const processHierarchicalFiles = (files: PlannerFile[]): PlannerFile[] => {
  // API returns hierarchical structure directly, just ensure type is set correctly
  return files.map(file => ({
    ...file,
    type: file.type || 'file', // Ensure type is set
    children: file.children || [] // Ensure children array exists
  }))
}

/**
 * Recursively searches for a file in the file tree
 * @param fileList - Array of files to search through
 * @param targetPath - Path of the file to find
 * @returns true if file exists, false otherwise
 */
export const findFileInTree = (fileList: PlannerFile[], targetPath: string): boolean => {
  for (const file of fileList) {
    if (file.filepath === targetPath) {
      return true
    }
    if (file.children && file.children.length > 0) {
      if (findFileInTree(file.children, targetPath)) {
        return true
      }
    }
  }
  return false
}

/**
 * Extracts folder paths from a file path
 * @param filepath - Full file path (e.g., "Tasks/hello/task.md" or "Workflow/project/todo.md")
 * @returns Array of folder paths to expand
 */
export const extractFolderPaths = (filepath: string): string[] => {
  const pathParts = filepath.split('/')
  const foldersToExpand: string[] = []
  
  // Build folder paths progressively (exclude the file itself)
  for (let i = 0; i < pathParts.length - 1; i++) {
    const folderPath = pathParts.slice(0, i + 1).join('/')
    foldersToExpand.push(folderPath)
  }
  
  return foldersToExpand
}

/**
 * Checks if a file path represents a new file creation
 * @param toolName - Name of the tool being called
 * @returns true if this is a file creation operation
 */
export const isFileCreationTool = (toolName: string): boolean => {
  return toolName === 'update_workspace_file' || 
         toolName === 'patch_workspace_file' ||
         toolName === 'diff_patch_workspace_file' ||
         toolName === 'read_workspace_file' ||
         toolName === 'get_workspace_file_nested'
}
