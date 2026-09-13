import React, { useEffect, useState } from 'react'
import { CalendarClock, Clock, Search, MessageSquare, Webhook } from 'lucide-react'
import ModalPortal from '../ui/ModalPortal'
import type { ScheduledJob } from '../../services/api-types'
import { TooltipProvider } from '../ui/tooltip'
import { useScheduleRunsData } from './scheduleRuns/useScheduleRunsData'
import type { WorkflowScope } from './scheduleRuns/helpers'
import { ScheduleRunsHeader } from './scheduleRuns/ScheduleRunsHeader'
import { ScheduleOverviewView } from './scheduleRuns/ScheduleOverviewView'
import { ScheduleCalendarView } from './scheduleRuns/ScheduleCalendarView'
import { ScheduleGroupsView } from './scheduleRuns/ScheduleGroupsView'
import { ScheduleListView } from './scheduleRuns/ScheduleListView'
import { ScheduleTableView } from './scheduleRuns/ScheduleTableView'
import WorkflowAPITriggersView from '../workflow/WorkflowAPITriggersView'
import { useWorkflowStore } from '../../stores/useWorkflowStore'

interface WorkflowScheduleRunsPanelProps {
  onClose: () => void
  embedded?: boolean
  active?: boolean
  onJobsLoaded?: (jobs: ScheduledJob[]) => void
  workflowScope?: WorkflowScope
  headerAction?: React.ReactNode
}

const WorkflowScheduleRunsPanel: React.FC<WorkflowScheduleRunsPanelProps> = ({ onClose, onJobsLoaded, workflowScope, embedded = false, active = true, headerAction }) => {
  const panel = useScheduleRunsData({ onClose, onJobsLoaded, workflowScope, active })
  const workspaceViewTarget = useWorkflowStore(state => state.workspaceViewTarget)
  const hasWorkflowWebhooks = Boolean(workflowScope?.workspacePath)
  const [automationSection, setAutomationSection] = useState<'schedules' | 'webhooks'>(() =>
    hasWorkflowWebhooks && workspaceViewTarget?.view === 'schedules' && workspaceViewTarget.target === 'webhooks'
      ? 'webhooks'
      : 'schedules',
  )
  useEffect(() => {
    if (workspaceViewTarget?.view !== 'schedules') return
    setAutomationSection(hasWorkflowWebhooks && workspaceViewTarget.target === 'webhooks' ? 'webhooks' : 'schedules')
  }, [hasWorkflowWebhooks, workspaceViewTarget])
  const {
    isLoading,
    error,
    isWorkflowScoped,
    activeView,
    setActiveView,
    activeFilter,
    setActiveFilter,
    searchQuery,
    setSearchQuery,
    selectedWorkflowFilter,
    setSelectedWorkflowFilter,
    panelJobs,
    workflowOptions,
    filteredJobs,
    workflowGroups,
    monthlyCalendar,
    filterPills,
    activeFilterLabel,
  } = panel

  const compact = embedded && !isWorkflowScoped

  const views = [
    { key: 'schedules' as const, label: 'List' },
    { key: 'calendar' as const, label: 'Calendar' },
  ]
  const viewControls = (
    <div className="flex items-center gap-1 rounded-lg bg-muted/40 p-0.5" role="group" aria-label="Schedule views">
      {views.map(view => <button key={view.key} type="button" aria-pressed={activeView === view.key}
        onClick={() => setActiveView(view.key)}
        className={`rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${activeView === view.key
          ? 'bg-background text-foreground shadow-sm ring-1 ring-border'
          : 'text-muted-foreground hover:bg-background/50 hover:text-foreground'}`}>
        {view.label}
      </button>)}
    </div>
  )

  const panelElement = (
    <TooltipProvider delayDuration={300}>
    <div
      className={embedded
        ? 'flex h-full min-h-0 w-full bg-background'
        : 'fixed inset-0 z-[9999] flex items-center justify-center bg-black/50'}
      onClick={embedded ? undefined : (e) => { if (e.target === e.currentTarget) onClose() }}
    >
      <div className={embedded
        ? 'flex h-full min-h-0 w-full flex-col bg-card text-card-foreground'
        : 'mx-4 flex max-h-[85vh] w-full max-w-6xl flex-col rounded-xl border border-border bg-card text-card-foreground shadow-2xl'}>

        {hasWorkflowWebhooks && (
          <div className="flex shrink-0 items-center gap-1 border-b border-border px-4 py-2 sm:px-6" role="tablist" aria-label="Workflow triggers">
            <button type="button" role="tab" aria-selected={automationSection === 'schedules'}
              onClick={() => setAutomationSection('schedules')}
              className={`inline-flex items-center gap-2 rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${automationSection === 'schedules' ? 'bg-muted text-foreground' : 'text-muted-foreground hover:bg-muted/60 hover:text-foreground'}`}>
              <CalendarClock className="h-3.5 w-3.5" />Schedules
            </button>
            <button type="button" role="tab" aria-selected={automationSection === 'webhooks'}
              onClick={() => setAutomationSection('webhooks')}
              className={`inline-flex items-center gap-2 rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${automationSection === 'webhooks' ? 'bg-muted text-foreground' : 'text-muted-foreground hover:bg-muted/60 hover:text-foreground'}`}>
              <Webhook className="h-3.5 w-3.5" />Webhooks
            </button>
          </div>
        )}

        {automationSection === 'webhooks' && workflowScope?.workspacePath ? (
          <div className="min-h-0 flex-1">
            <WorkflowAPITriggersView
              workspacePath={workflowScope.workspacePath}
              onViewRuns={() => setAutomationSection('schedules')}
              headerAction={headerAction}
            />
          </div>
        ) : <>

        {/* Header */}
        <ScheduleRunsHeader panel={panel} onClose={onClose} showClose={!embedded} headerAction={headerAction}
          compact={compact} navigation={compact ? viewControls : undefined} />

        {/* Body */}
        <div className="flex-1 overflow-y-auto">
          {!compact && panelJobs.length > 0 && (
            <div className="sticky top-0 z-10 border-b border-border bg-card/95 backdrop-blur px-5 py-3">
              <div className="space-y-2">
                {viewControls}

                <div className="text-xs text-muted-foreground">
                  {activeView === 'overview'
                    ? 'Summary and schedule health'
                    : activeView === 'calendar'
                      ? `${monthlyCalendar.total} scheduled item${monthlyCalendar.total === 1 ? '' : 's'} this month`
                      : activeView === 'by-workflow'
                        ? `${workflowGroups.length} automation${workflowGroups.length === 1 ? '' : 's'} with schedules`
                        : `${filteredJobs.length} schedule${filteredJobs.length !== 1 ? 's' : ''} · ${activeFilterLabel}`}
                </div>
                {activeView === 'schedules' && (
                  <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                    <MessageSquare className="h-3.5 w-3.5 shrink-0" />
                    <span>To change a schedule, ask the automation agent in Chat.</span>
                  </div>
                )}
              </div>
            </div>
          )}

          {isLoading && panelJobs.length === 0 ? (
            <div className="flex items-center justify-center h-40 text-sm text-muted-foreground">Loading...</div>
          ) : error ? (
            <div className="flex items-center justify-center h-40 text-sm text-red-500">{error}</div>
          ) : panelJobs.length === 0 ? (
            <div className="flex flex-col items-center justify-center h-48 gap-3 px-6 text-center text-sm text-muted-foreground">
              <Clock className="w-8 h-8 opacity-30" />
              <div>
                <p>{isWorkflowScoped ? 'No schedules for this automation yet.' : 'No automation schedules yet.'}</p>
                <p className="mt-1 text-xs">
                  {isWorkflowScoped
                    ? 'Ask chat to schedule this automation when you are ready.'
                    : 'Ask chat to schedule an automation when you are ready.'}
                </p>
              </div>
            </div>
          ) : activeView === 'overview' ? (
            <ScheduleOverviewView panel={panel} />
          ) : activeView === 'calendar' ? (
            <ScheduleCalendarView panel={panel} />
          ) : (
            <>
              <div className={`border-b border-border px-4 sm:px-6 ${compact ? 'py-2.5' : 'py-4'}`}>
                <div className="flex flex-wrap items-center gap-2">
                    <div className="flex min-w-0 flex-1 flex-wrap gap-2">
                      <div className="relative min-w-48 flex-1">
                        <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
                        <input
                          type="text"
                          value={searchQuery}
                          onChange={(e) => setSearchQuery(e.target.value)}
                          aria-label="Search schedules"
                          placeholder={isWorkflowScoped ? 'Search schedules…' : 'Search automations or schedules…'}
                          className="w-full rounded-lg border border-border bg-background pl-9 pr-3 py-1.5 text-sm text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-primary/50"
                        />
                      </div>
                      {!isWorkflowScoped && (activeView !== 'by-workflow' || selectedWorkflowFilter !== 'all') && (
                        <select
                          aria-label="Filter by automation"
                          value={selectedWorkflowFilter}
                          onChange={(event) => setSelectedWorkflowFilter(event.target.value)}
                          className="min-w-40 max-w-full rounded-lg border border-border bg-background px-3 py-1.5 text-sm text-foreground focus:outline-none focus:ring-2 focus:ring-primary/50"
                        >
                          <option value="all">All automations</option>
                          {workflowOptions.map((option) => (
                            <option key={option.value} value={option.value}>{option.label}</option>
                          ))}
                        </select>
                      )}
                    </div>

                  <div className="flex items-center gap-2">
                    <label className="sr-only" htmlFor={compact ? 'global-schedule-status' : 'workflow-schedule-status'}>Filter schedules by state</label>
                    <select id={compact ? 'global-schedule-status' : 'workflow-schedule-status'} value={activeFilter}
                      onChange={event => setActiveFilter(event.target.value as typeof activeFilter)}
                      className="rounded-lg border border-border bg-background px-3 py-1.5 text-sm text-foreground focus:outline-none focus:ring-2 focus:ring-primary/50">
                      {filterPills.filter(pill => !compact || !['issues', 'missed'].includes(pill.key)).map(pill => <option key={pill.key} value={pill.key}>{pill.key === 'all' ? 'All states' : pill.label} ({pill.count})</option>)}
                    </select>
                  </div>
                </div>
              </div>
            </>
          )}
          {panelJobs.length > 0 && activeView === 'by-workflow' && workflowGroups.length === 0 && (
            <div className="flex flex-col items-center justify-center h-40 gap-2 text-sm text-muted-foreground px-6 text-center">
              <Search className="w-8 h-8 opacity-30" />
              <p>No automations match the current filter.</p>
              <button
                onClick={() => {
                  setSearchQuery('')
                  setActiveFilter('all')
                  setSelectedWorkflowFilter('all')
                }}
                className="text-xs text-primary hover:underline"
              >
                Clear search and show all schedules
              </button>
            </div>
          )}
          {panelJobs.length > 0 && activeView === 'by-workflow' && workflowGroups.length > 0 && (
            <ScheduleGroupsView panel={panel} />
          )}
          {panelJobs.length > 0 && activeView === 'schedules' && filteredJobs.length === 0 && (
            <div className="flex flex-col items-center justify-center h-40 gap-2 text-sm text-muted-foreground px-6 text-center">
              <Search className="w-8 h-8 opacity-30" />
              <p>No schedules match the current filter.</p>
              <button
                onClick={() => {
                  setSearchQuery('')
                  setActiveFilter('all')
                  setSelectedWorkflowFilter('all')
                }}
                className="text-xs text-primary hover:underline"
              >
                Clear search and show all schedules
              </button>
            </div>
          )}
          {panelJobs.length > 0 && activeView === 'schedules' && filteredJobs.length > 0 && (
            compact ? <ScheduleTableView panel={panel} /> : <ScheduleListView panel={panel} />
          )}
        </div>
        </>}
      </div>

    </div>
    </TooltipProvider>
  )

  return embedded ? panelElement : <ModalPortal>{panelElement}</ModalPortal>
}

export default WorkflowScheduleRunsPanel
