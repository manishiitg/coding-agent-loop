import type { VariableGroup, VariablesManifest } from '../services/api-types'

/**
 * Utility functions for workflow-related operations
 * Consolidates logic for handling group folders, iteration paths, and display name sanitization
 */

/**
 * Sanitizes a display name for use in folder paths
 * Converts to lowercase, replaces special characters with dashes, normalizes multiple dashes
 * 
 * @param displayName - The display name to sanitize
 * @returns Sanitized display name, or empty string if invalid
 */
export function sanitizeDisplayNameForFolder(displayName: string | undefined): string {
  if (!displayName) return ''
  
  return displayName
    .toLowerCase()
    .replace(/[^a-z0-9-]/g, '-')
    .replace(/-+/g, '-')
    .trim()
    .replace(/^-+|-+$/g, '')
}

/**
 * Extracts the iteration folder from a run folder path
 * Handles both formats: "iteration-X" and "iteration-X/group-Y"
 * 
 * @param runFolder - The run folder path (e.g., "iteration-1" or "iteration-1/group-5")
 * @returns The iteration folder name (e.g., "iteration-1"), or null if not found
 */
export function extractIterationFolder(runFolder: string | null | undefined): string | null {
  if (!runFolder || runFolder === 'new') return null
  
  if (runFolder.includes('/')) {
    return runFolder.split('/')[0] // e.g., "iteration-1" from "iteration-1/group-5"
  }
  
  if (runFolder.startsWith('iteration-')) {
    return runFolder
  }
  
  return null
}

/**
 * Extracts the group ID from a run folder path
 * Handles both formats: "iteration-X/group-Y" and "iteration-X/display-name"
 * 
 * @param runFolder - The run folder path (e.g., "iteration-1/group-5" or "iteration-1/production")
 * @param manifest - Optional variables manifest to resolve display names to group IDs
 * @returns The group ID (e.g., "group-5"), or null if not found
 */
export function extractGroupIdFromFolder(
  runFolder: string | null | undefined,
  manifest?: VariablesManifest | null
): string | null {
  if (!runFolder || runFolder === 'new' || !runFolder.includes('/')) {
    return null
  }
  
  const parts = runFolder.split('/')
  if (parts.length !== 2) return null
  
  const groupFolderName = parts[1] // e.g., "group-5" or "production"
  
  // If it starts with "group-", return it directly as the group ID
  if (groupFolderName.startsWith('group-')) {
    return groupFolderName // e.g., "group-5"
  }
  
  // Otherwise, it's a display name - try to resolve it to a group ID using manifest
  if (manifest?.groups) {
    const sanitizeForMatch = (name: string) => 
      name.toLowerCase().replace(/[^a-z0-9-]/g, '-').replace(/-+/g, '-').trim()
    
    const group = manifest.groups.find((g: VariableGroup) => {
      const sanitizedDisplayName = g.display_name ? sanitizeForMatch(g.display_name) : ''
      const folderNameSanitized = sanitizeForMatch(groupFolderName)
      
      return sanitizedDisplayName === folderNameSanitized || g.group_id === groupFolderName
    })
    
    return group?.group_id || null
  }
  
  // No manifest provided, return the folder name as-is (might be a display name)
  return groupFolderName
}

/**
 * Builds a group folder path from a group ID
 * Uses sanitized display_name if available, otherwise falls back to group_id
 * 
 * @param groupId - The group ID (e.g., "group-5")
 * @param iterationFolder - The iteration folder (e.g., "iteration-1")
 * @param manifest - Variables manifest to get group details
 * @returns The full group folder path (e.g., "iteration-1/group-5" or "iteration-1/production"), or null if group not found
 */
export function buildGroupFolderPath(
  groupId: string,
  iterationFolder: string | null | undefined,
  manifest?: VariablesManifest | null
): string | null {
  if (!groupId || !iterationFolder) return null
  
  // Find the group in manifest
  const group = manifest?.groups?.find(g => g.group_id === groupId)
  if (!group) {
    // Group not found in manifest, use group_id as folder name
    return `${iterationFolder}/${groupId}`
  }
  
  // Determine folder name (sanitized display_name or group_id)
  const folderName = group.display_name && sanitizeDisplayNameForFolder(group.display_name)
    ? sanitizeDisplayNameForFolder(group.display_name)
    : group.group_id
  
  return `${iterationFolder}/${folderName}`
}

/**
 * Determines the specific group folder path to use for phases that need context
 * Priority: currentRunningGroupId > selectedRunFolder (if group path) > first selectedGroupIds
 * 
 * @param options - Configuration options
 * @param options.currentRunningGroupId - Currently running group ID (if during batch execution)
 * @param options.selectedRunFolder - Currently selected run folder
 * @param options.selectedGroupIds - Array of selected group IDs from checkboxes
 * @param options.manifest - Variables manifest
 * @returns The resolved group folder path, or the original selectedRunFolder if no groups involved
 */
export function resolveGroupFolderPath(options: {
  currentRunningGroupId?: string | null
  selectedRunFolder?: string | null
  selectedGroupIds?: string[]
  manifest?: VariablesManifest | null
}): string | undefined {
  const {
    currentRunningGroupId,
    selectedRunFolder,
    selectedGroupIds = [],
    manifest
  } = options
  
  // If no manifest or groups, return selectedRunFolder as-is
  if (!manifest?.groups || manifest.groups.length === 0) {
    return selectedRunFolder === 'new' ? undefined : selectedRunFolder || undefined
  }
  
  let targetGroupId: string | null = null
  let resolvedFolder: string | undefined = selectedRunFolder === 'new' ? undefined : selectedRunFolder || undefined
  
  // Priority 1: Use currently running group (during batch execution)
  if (currentRunningGroupId) {
    targetGroupId = currentRunningGroupId
  }
  // Priority 2: Check if selectedRunFolder already specifies a group
  else if (selectedRunFolder && selectedRunFolder !== 'new' && selectedRunFolder.includes('/')) {
    // selectedRunFolder already contains a group path, use it as-is
    resolvedFolder = selectedRunFolder
    return resolvedFolder
  }
  // Priority 3: Use first selected group from checkboxes
  else if (selectedGroupIds.length > 0) {
    targetGroupId = selectedGroupIds[0]
    
    if (selectedGroupIds.length > 1) {
      console.warn('[workflowUtils] Multiple groups selected, using first group for context:', targetGroupId)
    }
  }
  
  // Build the group folder path if we have a target group ID
  if (targetGroupId && (!resolvedFolder || !resolvedFolder.includes('/'))) {
    const iterationFolder = extractIterationFolder(selectedRunFolder)
    if (iterationFolder) {
      const groupPath = buildGroupFolderPath(targetGroupId, iterationFolder, manifest)
      if (groupPath) {
        resolvedFolder = groupPath
      }
    }
  }
  
  return resolvedFolder
}
