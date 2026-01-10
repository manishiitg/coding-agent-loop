/**
 * Workflow State Normalization Utilities
 * 
 * Single source of truth for converting between different representations:
 * - UI Display: folder names, display names
 * - Canonical State: group_ids, step numbers
 * - API Format: group_ids, step numbers, folder paths
 * 
 * All normalization happens here to ensure consistency.
 */

import type { VariablesManifest } from '../services/api-types'

/**
 * Sanitizes a display name for folder matching
 */
function sanitizeForMatch(name: string): string {
  return name.toLowerCase().replace(/[^a-z0-9-]/g, '-').replace(/-+/g, '-').trim()
}

/**
 * Normalizes group IDs from any format to canonical group_ids
 * 
 * Input can be:
 * - group_ids (e.g., "group-1") ✅ Already canonical
 * - folder names (e.g., "manishiithuf") → needs lookup
 * - display names (e.g., "Manishi Ithuf") → needs lookup
 * 
 * @param input - Array of group IDs, folder names, or display names
 * @param manifest - Variables manifest to resolve folder/display names to group_ids
 * @returns Array of valid group_ids from manifest (filtered and normalized)
 */
export function normalizeGroupIds(
  input: string[],
  manifest: VariablesManifest | null
): string[] {
  if (!input || input.length === 0) {
    return []
  }

  if (!manifest?.groups || manifest.groups.length === 0) {
    // No manifest - can't normalize, return empty (invalid state)
    console.warn('[normalizeGroupIds] No manifest available, cannot normalize group IDs')
    return []
  }

  const normalized: string[] = []
  const seen = new Set<string>()

  for (const item of input) {
    // Skip if already processed (deduplication)
    if (seen.has(item)) continue

    let matchedGroupId: string | null = null

    // Strategy 1: Check if it's already a valid group_id
    const exactMatch = manifest.groups.find(g => g.group_id === item)
    if (exactMatch) {
      matchedGroupId = exactMatch.group_id
    } else {
      // Strategy 2: Try to match by sanitized display_name (might be a folder name)
      const displayNameMatch = manifest.groups.find(g => {
        if (!g.display_name) return false
        const sanitizedDisplayName = sanitizeForMatch(g.display_name)
        const itemSanitized = sanitizeForMatch(item)
        return sanitizedDisplayName === itemSanitized
      })
      if (displayNameMatch) {
        matchedGroupId = displayNameMatch.group_id
      }
    }

    // Only add if we found a valid match
    if (matchedGroupId) {
      normalized.push(matchedGroupId)
      seen.add(matchedGroupId) // Deduplicate by group_id
    } else {
      console.warn('[normalizeGroupIds] Could not normalize group ID:', item)
    }
  }

  return normalized
}

/**
 * Validates that group IDs exist in manifest and are enabled
 * 
 * @param groupIds - Array of group_ids to validate
 * @param manifest - Variables manifest
 * @returns Array of valid, enabled group_ids
 */
export function validateGroupIds(
  groupIds: string[],
  manifest: VariablesManifest | null
): string[] {
  if (!manifest?.groups || groupIds.length === 0) {
    return []
  }

  return groupIds.filter(groupId => {
    const group = manifest.groups!.find(g => g.group_id === groupId)
    return group && group.enabled
  })
}

/**
 * Gets display information for a group ID
 * 
 * @param groupId - Canonical group_id
 * @param manifest - Variables manifest
 * @param iterationFolder - Optional iteration folder for building folder path
 * @returns Display information or null if group not found
 */
export function getGroupDisplayInfo(
  groupId: string,
  manifest: VariablesManifest | null,
  iterationFolder?: string | null
): {
  groupId: string
  displayName: string
  folderName: string
  folderPath: string | null
} | null {
  if (!manifest?.groups) {
    return null
  }

  const group = manifest.groups.find(g => g.group_id === groupId)
  if (!group) {
    return null
  }

  // Determine folder name (sanitized display_name or group_id)
  const folderName = group.display_name
    ? sanitizeForMatch(group.display_name) || group.group_id
    : group.group_id

  // Build folder path if iteration is provided
  const folderPath = iterationFolder ? `${iterationFolder}/${folderName}` : null

  return {
    groupId: group.group_id,
    displayName: group.display_name || group.group_id,
    folderName,
    folderPath
  }
}

/**
 * Normalizes start point from any format to canonical format
 * 
 * @param input - Could be number, string, or undefined
 * @returns Canonical start point: 0 (beginning) or step number (1-based)
 */
export function normalizeStartPoint(input: number | string | undefined | null): number {
  if (input === undefined || input === null || input === '') {
    return 0 // Default: start from beginning
  }

  const parsed = typeof input === 'string' ? parseInt(input, 10) : input
  if (isNaN(parsed) || parsed < 0) {
    return 0 // Invalid: default to beginning
  }

  return parsed
}

/**
 * Normalizes run folder selection
 * 
 * Ensures the folder path is valid and extracts iteration/group info
 * 
 * @param folderPath - Folder path (e.g., "iteration-4", "iteration-4/manishiithuf", "new")
 * @param manifest - Variables manifest for validation
 * @returns Normalized folder path or "new"
 */
export function normalizeRunFolder(
  folderPath: string | null | undefined,
  manifest: VariablesManifest | null
): string {
  if (!folderPath || folderPath === 'new') {
    return 'new'
  }

  // Validate format: should be "iteration-X" or "iteration-X/group-name"
  if (!folderPath.startsWith('iteration-')) {
    console.warn('[normalizeRunFolder] Invalid folder path format:', folderPath)
    return 'new'
  }

  // If it's a group folder, validate the group exists in manifest
  if (folderPath.includes('/')) {
    const parts = folderPath.split('/')
    if (parts.length === 2) {
      const [, groupFolderName] = parts
      
      // Check if group folder name matches any group in manifest
      if (manifest?.groups) {
        const groupExists = manifest.groups.some(g => {
          // Match by group_id
          if (g.group_id === groupFolderName) return true
          
          // Match by sanitized display_name
          if (g.display_name) {
            const sanitizedDisplayName = sanitizeForMatch(g.display_name)
            const folderNameSanitized = sanitizeForMatch(groupFolderName)
            return sanitizedDisplayName === folderNameSanitized
          }
          
          return false
        })
        
        if (!groupExists) {
          console.warn('[normalizeRunFolder] Group folder not found in manifest:', groupFolderName)
          // Return just the iteration folder
          return parts[0]
        }
      }
    }
  }

  return folderPath
}

/**
 * Type guard to check if a value is a valid group_id format
 */
export function isValidGroupIdFormat(value: string): boolean {
  // Group IDs typically start with "group-" followed by a number
  // But we allow any string as long as it's not empty
  return typeof value === 'string' && value.length > 0
}
