import { FileContentViewerBody } from './FileContentViewer'
import Workspace from './Workspace'
import { useWorkspaceStore } from '../stores/useWorkspaceStore'
import { EXPAND_FIRST_LEVEL_FOLDERS_BY_DEFAULT } from '../utils/workspacePathUtils'

type FileWorkspacePaneProps = {
  workspacePath?: string
  title?: string
  hiddenRootFolders?: string[]
  hideAddToChat?: boolean
  hideRootActions?: boolean
  expandFirstLevelFolders?: boolean
  hideManagedEntriesByDefault?: boolean
  hideMinimizeControl?: boolean
  showMinimizeShortcut?: boolean
  onClose: () => void
  testId?: string
}

/**
 * Shared Files surface for any product that exposes a workspace. It keeps the
 * tree mounted while a file is open, preserving navigation state, and makes
 * the file viewer occupy that product's right-side Files pane rather than
 * opening a competing full-screen overlay.
 */
export function FileWorkspacePane({
  workspacePath,
  title,
  hiddenRootFolders,
  hideAddToChat = false,
  hideRootActions = false,
  expandFirstLevelFolders = EXPAND_FIRST_LEVEL_FOLDERS_BY_DEFAULT,
  hideManagedEntriesByDefault = false,
  hideMinimizeControl = true,
  showMinimizeShortcut = true,
  onClose,
  testId,
}: FileWorkspacePaneProps) {
  const showFileContent = useWorkspaceStore(state => state.showFileContent)

  return (
    <div className="relative flex h-full min-h-0 flex-col bg-background" data-testid={testId}>
      <div className="min-h-0 flex-1" hidden={showFileContent}>
        <Workspace
          minimized={false}
          onToggleMinimize={onClose}
          hideMinimizeControl={hideMinimizeControl}
          showMinimizeShortcut={showMinimizeShortcut}
          scopedWorkspacePath={workspacePath}
          hiddenRootFolders={hiddenRootFolders}
          hideAddToChat={hideAddToChat}
          hideRootActions={hideRootActions}
          expandFirstLevelFolders={expandFirstLevelFolders}
          hideManagedEntriesByDefault={hideManagedEntriesByDefault}
          title={title}
        />
      </div>
      {showFileContent && (
        <div className="min-h-0 flex-1">
          <FileContentViewerBody variant="pane" />
        </div>
      )}
    </div>
  )
}
