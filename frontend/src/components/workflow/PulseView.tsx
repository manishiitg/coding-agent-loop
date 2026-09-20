import { Activity } from 'lucide-react'
import { PulseWorkspace } from './PulseWorkspace'
import { WorkspaceViewHeader } from './WorkspaceViewHeader'
import { WorkspaceViewIconButton } from './WorkspaceViewIconButton'
import { WORKFLOW_SOUL_REFRESH_EVENT } from './SoulViewer'
import type { PulseFinalCommandState, PulseModuleState, PulsePlanDriftDueItem, PulseReviewFocus, PulseReviewerModule } from '../../services/api-types'

export interface PulseOverview {
  recorded: number
  total: number
  latest: string
}

interface PulseViewProps {
  workspacePath: string | null
  monitorOn: boolean
  monitorSaving: boolean
  onToggleMonitor: () => void
  disabledReviewModules: PulseReviewerModule[]
  reviewModuleSaving: PulseReviewerModule | null
  onToggleReviewModule: (module: PulseReviewerModule) => void
  moduleStates: PulseModuleState[]
  planDriftDue: boolean
  planDriftDueItems: PulsePlanDriftDueItem[]
  planDriftDueError: string | null
  finalCommandStates: PulseFinalCommandState[]
  reviewFocuses: PulseReviewFocus[]
  reviewFocusSelections: PulseReviewFocus[]
  statusError: string | null
  statusLoading: boolean
  overview: PulseOverview
  onRefresh: () => void
  headerAction?: React.ReactNode
}

export default function PulseView({
  workspacePath,
  monitorOn,
  monitorSaving,
  onToggleMonitor,
  disabledReviewModules,
  reviewModuleSaving,
  onToggleReviewModule,
  moduleStates,
  planDriftDue,
  planDriftDueItems,
  planDriftDueError,
  finalCommandStates,
  reviewFocuses,
  reviewFocusSelections,
  statusError,
  statusLoading,
  overview,
  onRefresh,
  headerAction,
}: PulseViewProps) {
  return (
    <div className="flex h-full min-h-0 w-full max-w-none flex-col bg-background">
      <WorkspaceViewHeader
        icon={Activity}
        title="Pulse"
        context={<span className={`rounded-full border px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide ${monitorOn ? 'border-primary/25 bg-primary/10 text-primary' : 'border-border bg-muted text-muted-foreground'}`}>
          {monitorOn ? 'On' : 'Off'}
        </span>}
        subtitle={`${overview.recorded}/${overview.total} statuses recorded${overview.latest ? ` · Updated ${overview.latest}` : ''}`}
        actions={<>
          {headerAction}
          <WorkspaceViewIconButton
            label="Refresh Pulse status"
            onClick={() => {
              window.dispatchEvent(new CustomEvent(WORKFLOW_SOUL_REFRESH_EVENT))
              onRefresh()
            }}
            disabled={statusLoading}
            spinning={statusLoading}
          />
        </>}
      />

      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="p-3 sm:p-4">
          {workspacePath && (
            <PulseWorkspace
              workspacePath={workspacePath}
              moduleStates={moduleStates}
              planDriftDue={planDriftDue}
              planDriftDueItems={planDriftDueItems}
              planDriftDueError={planDriftDueError}
              finalCommandStates={finalCommandStates}
              reviewFocuses={reviewFocuses}
              reviewFocusSelections={reviewFocusSelections}
              disabledReviewModules={disabledReviewModules}
              reviewModuleSaving={reviewModuleSaving}
              onToggleReviewModule={onToggleReviewModule}
              statusError={statusError}
            />
          )}
        </div>
      </div>

      <div className="flex shrink-0 items-center gap-3 border-t border-border bg-background px-4 py-3 sm:px-5">
        <button
          type="button"
          role="switch"
          aria-checked={monitorOn}
          onClick={onToggleMonitor}
          disabled={monitorSaving}
          className={`relative inline-flex h-5 w-9 flex-none items-center rounded-full p-0 transition-colors disabled:opacity-50 ${monitorOn ? 'bg-primary' : 'bg-muted-foreground/30'}`}
          aria-label="Toggle Pulse"
        >
          <span className={`inline-block h-4 w-4 rounded-full bg-white shadow-sm transition-transform ${monitorOn ? 'translate-x-[18px]' : 'translate-x-[2px]'}`} />
        </button>
        <div className="min-w-0">
          <div className="text-xs font-medium text-foreground">{monitorOn ? 'Reviews scheduled runs' : 'Pulse is off'}</div>
          <div className="truncate text-[11px] text-muted-foreground">{monitorOn ? 'Pulse Gate runs after each normal scheduled run.' : 'Turn on to review completed scheduled runs.'}</div>
        </div>
      </div>
    </div>
  )
}
