import { Activity, RefreshCw } from 'lucide-react'
import { PulseWorkspace } from './PulseWorkspace'
import { WORKFLOW_SOUL_REFRESH_EVENT } from './SoulViewer'
import type { PulseFinalCommandState, PulseModuleState, PulseReviewFocus, PulseReviewerModule } from '../../services/api-types'

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
  finalCommandStates: PulseFinalCommandState[]
  reviewFocuses: PulseReviewFocus[]
  reviewFocusSelections: PulseReviewFocus[]
  statusError: string | null
  statusLoading: boolean
  overview: PulseOverview
  onRefresh: () => void
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
  finalCommandStates,
  reviewFocuses,
  reviewFocusSelections,
  statusError,
  statusLoading,
  overview,
  onRefresh,
}: PulseViewProps) {
  return (
    <div className="flex h-full min-h-0 w-full max-w-none flex-col bg-background">
      <div className="flex shrink-0 items-center justify-between gap-3 border-b border-border px-4 py-3.5 sm:px-5">
        <div className="flex min-w-0 items-center gap-3">
          <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md border border-primary/25 bg-primary/10 text-primary">
            <Activity className="h-4 w-4" />
          </div>
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="text-sm font-semibold text-foreground">Pulse</h2>
              <span className={`rounded-full border px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide ${monitorOn ? 'border-primary/25 bg-primary/10 text-primary' : 'border-border bg-muted text-muted-foreground'}`}>
                {monitorOn ? 'On' : 'Off'}
              </span>
            </div>
            <div className="mt-0.5 flex flex-wrap items-center gap-x-2 text-[11px] text-muted-foreground">
              <span>{overview.recorded}/{overview.total} statuses recorded</span>
              {overview.latest && <span>Updated {overview.latest}</span>}
            </div>
          </div>
        </div>
        {monitorOn && (
          <button
            type="button"
            onClick={() => {
              window.dispatchEvent(new CustomEvent(WORKFLOW_SOUL_REFRESH_EVENT))
              onRefresh()
            }}
            disabled={statusLoading}
            className="shrink-0 rounded-md p-1.5 text-muted-foreground hover:bg-accent hover:text-foreground disabled:opacity-60"
            aria-label="Refresh Pulse status"
            title="Refresh Pulse status"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${statusLoading ? 'animate-spin' : ''}`} />
          </button>
        )}
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="p-3 sm:p-4">
          {workspacePath && (
            <PulseWorkspace
              workspacePath={workspacePath}
              moduleStates={moduleStates}
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
