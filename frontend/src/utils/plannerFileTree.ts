import type { PlannerFile } from '../services/api-types'

export interface VisibleFileRow {
  file: PlannerFile
  depth: number
}

export const WORKSPACE_SCROLL_TO_FILE_EVENT = 'workspace-scroll-to-file'

export interface WorkspaceScrollToFileDetail {
  filepath: string
}

const sortPlannerFiles = (files: PlannerFile[]): PlannerFile[] => (
  [...files].sort((left, right) => {
    if (left.type === right.type) return left.filepath.localeCompare(right.filepath)
    if (left.type === 'folder') return -1
    if (right.type === 'folder') return 1
    return 0
  })
)

export const flattenVisiblePlannerFiles = (
  files: PlannerFile[],
  expandedFolders: Set<string>,
  forceExpandFolders: boolean,
): VisibleFileRow[] => {
  const rows: VisibleFileRow[] = []
  const append = (items: PlannerFile[], depth: number) => {
    for (const file of sortPlannerFiles(items)) {
      rows.push({ file, depth })
      const expanded = forceExpandFolders || expandedFolders.has(file.filepath)
      if (file.type === 'folder' && expanded && file.children?.length) {
        append(file.children, depth + 1)
      }
    }
  }
  append(files, 0)
  return rows
}
