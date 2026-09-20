import { AskAIButton } from './AskAIButton'
import { WorkspaceViewIconButton } from './WorkspaceViewIconButton'

interface WorkspaceViewActionsProps {
  workspacePath: string | null
  message: string
  onRefresh: () => void | Promise<void>
  refreshing?: boolean
  refreshLabel?: string
  /** Ask AI routing override (Crew panes route to the project chat). */
  onAsk?: (message: string) => void | Promise<void>
}

/**
 * The standard action pair shown in a right-side workspace view header.
 * Order is a product rule: Ask AI always left, refresh always right — every
 * right-pane header (report, files, inspectors, schedules, triggers) follows
 * it so the pair is predictable wherever it appears.
 */
export function WorkspaceViewActions({
  workspacePath,
  message,
  onRefresh,
  refreshing = false,
  refreshLabel = 'Refresh view',
  onAsk,
}: WorkspaceViewActionsProps) {
  return (
    <div className="flex shrink-0 items-center gap-2">
      <AskAIButton workspacePath={workspacePath} message={message} iconOnly onAsk={onAsk} />
      <WorkspaceViewIconButton label={refreshLabel} onClick={onRefresh} disabled={refreshing} spinning={refreshing} />
    </div>
  )
}
