import { useEffect, useRef } from 'react'
import type { PlannerFile } from '../../services/api-types'
import { findIterationFolders, findFolderInTree, isIterationFolder } from '../../utils/workspacePathUtils'

interface UseIterationExpansionProps {
  selectedModeCategory: string | null
  workflowFolderPath: string | null
  filteredFiles: PlannerFile[]
  selectedRunFolder: string | null
  expandedFolders: Set<string>
  setExpandedFolders: (folders: Set<string>) => void
}

/**
 * Custom hook to handle auto-collapse/expand of iteration folders
 * When a run folder is selected, it collapses other iterations and expands the selected one
 * Preserves manually expanded folders within the selected iteration
 */
export function useIterationExpansion({
  selectedModeCategory,
  workflowFolderPath,
  filteredFiles,
  selectedRunFolder,
  expandedFolders,
  setExpandedFolders
}: UseIterationExpansionProps) {
  // Use ref to track last processed selectedRunFolder to avoid re-running on manual folder expansion
  const lastProcessedRunFolderRef = useRef<string | null>(null)

  useEffect(() => {
    // Only run in workflow mode when we have filtered files and a selected iteration
    if (
      selectedModeCategory !== 'workflow' ||
      !workflowFolderPath ||
      filteredFiles.length === 0 ||
      !selectedRunFolder ||
      selectedRunFolder === 'new'
    ) {
      lastProcessedRunFolderRef.current = null
      return
    }

    // Only run if selectedRunFolder actually changed (not when expandedFolders changes)
    if (lastProcessedRunFolderRef.current === selectedRunFolder) {
      return
    }

    lastProcessedRunFolderRef.current = selectedRunFolder

    // Find all iteration folders in the filtered tree
    const allIterationFolders = findIterationFolders(filteredFiles)

    if (allIterationFolders.length === 0) {
      return // No iteration folders found, nothing to do
    }

    // Build the path for the selected iteration
    // selectedRunFolder can be: "iteration-10" or "iteration-10/group-1"
    // In filtered view, it appears as "runs/iteration-10" or "runs/iteration-10/group-1"
    const selectedIterationPath = selectedRunFolder.startsWith('runs/')
      ? selectedRunFolder
      : `runs/${selectedRunFolder}`

    // Also check without "runs/" prefix in case paths are adjusted differently
    const selectedIterationPathAlt = selectedRunFolder

    // Get current expanded folders
    const currentExpanded = new Set(expandedFolders)
    const newExpanded = new Set<string>()

    // Copy all non-iteration folders to keep them expanded
    for (const folder of currentExpanded) {
      // Check if this is NOT an iteration folder
      if (!isIterationFolder(folder)) {
        newExpanded.add(folder)
      }
    }

    // Find the matching iteration folder path in the tree (may have different path format)
    const matchingIterationPath = allIterationFolders.find(
      (path) =>
        path === selectedIterationPath ||
        path === selectedIterationPathAlt ||
        path.endsWith(`/${selectedRunFolder}`) ||
        path === selectedRunFolder
    )

    // Collapse all iteration folders, then expand only the selected one
    // Also expand parent folders needed to show the selected iteration
    if (matchingIterationPath) {
      newExpanded.add(matchingIterationPath)

      // Preserve any manually expanded folders that are children of the selected iteration
      // This allows users to manually expand group folders within the selected iteration
      const selectedIterationBase = matchingIterationPath.split('/').slice(0, 2).join('/') // e.g., "runs/iteration-10"
      for (const folder of currentExpanded) {
        // If this folder is a child of the selected iteration, preserve it
        if (folder.startsWith(selectedIterationBase + '/') && folder !== matchingIterationPath) {
          newExpanded.add(folder)
        }
      }

      // Check if this is a group path (e.g., "iteration-10/group-1" or "iteration-10/production")
      // If so, also expand the parent iteration folder to show all groups
      // A group path is any nested folder under iteration
      const isGroupPath = selectedRunFolder.includes('/') && selectedRunFolder.split('/').length === 2
      if (isGroupPath) {
        // Extract parent iteration folder (e.g., "iteration-10" from "iteration-10/group-1")
        const parentIterationName = selectedRunFolder.split('/')[0]
        const parentIterationPath = selectedIterationPath.startsWith('runs/')
          ? `runs/${parentIterationName}`
          : parentIterationName

        // Find the parent iteration folder in the file tree (not just in allIterationFolders)
        // This is needed because when groups exist, backend only returns group folders, not parent
        const parentIterationFolder =
          findFolderInTree(filteredFiles, parentIterationPath) ||
          findFolderInTree(filteredFiles, `runs/${parentIterationName}`) ||
          findFolderInTree(filteredFiles, parentIterationName)

        if (parentIterationFolder) {
          // Use the actual filepath from the tree (may have different format)
          const parentPathToExpand = parentIterationFolder.filepath || parentIterationFolder.originalFilepath
          if (parentPathToExpand) {
            newExpanded.add(parentPathToExpand)
          }
        }
      }

      // Also expand parent folders (e.g., "runs" if needed)
      const pathParts = matchingIterationPath.split('/')
      for (let i = 1; i < pathParts.length; i++) {
        const parentPath = pathParts.slice(0, i).join('/')
        newExpanded.add(parentPath)
      }
    }

    // Only update if something changed
    if (
      newExpanded.size !== currentExpanded.size ||
      Array.from(newExpanded).some((f) => !currentExpanded.has(f)) ||
      Array.from(currentExpanded).some((f) => !newExpanded.has(f))
    ) {
      setExpandedFolders(newExpanded)
    }
    // expandedFolders is intentionally excluded from dependencies - we only want to run when selectedRunFolder changes,
    // not when folders are manually expanded, to allow manual folder expansion
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedModeCategory, workflowFolderPath, filteredFiles, selectedRunFolder, setExpandedFolders])
}
