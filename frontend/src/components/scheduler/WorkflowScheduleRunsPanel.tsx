import React from 'react'
import { Clock, Search, MessageSquare } from 'lucide-react'
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

interface WorkflowScheduleRunsPanelProps {
  onClose: () => void
  embedded?: boolean
  onJobsLoaded?: (jobs: ScheduledJob[]) => void
  workflowScope?: WorkflowScope
  headerAction?: React.ReactNode
}

const WorkflowScheduleRunsPanel: React.FC<WorkflowScheduleRunsPanelProps> = ({ onClose, onJobsLoaded, workflowScope, embedded = false, headerAction }) => {
  const panel = useScheduleRunsData({ onClose, onJobsLoaded, workflowScope })
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

        {/* Header */}
        <ScheduleRunsHeader panel={panel} onClose={onClose} showClose={!embedded} headerAction={headerAction} />

        {/* Body */}
        <div className="flex-1 overflow-y-auto">
          {!isLoading && panelJobs.length > 0 && (
            <div className="sticky top-0 z-10 border-b border-border bg-card/95 backdrop-blur px-5 py-3">
              <div className="space-y-2">
                <div className="-mx-1 flex items-center gap-2 overflow-x-auto px-1 pb-1">
                  {!isWorkflowScoped && (
                    <>
                      <button
                        onClick={() => setActiveView('overview')}
                        className={`shrink-0 rounded-full px-3 py-1.5 text-xs font-medium transition-colors ${
                          activeView === 'overview'
                            ? 'bg-foreground text-background'
                            : 'border border-border bg-background text-muted-foreground hover:bg-muted hover:text-foreground'
                        }`}
                      >
                        Overview
                      </button>
                      <button
                        onClick={() => setActiveView('by-workflow')}
                        className={`shrink-0 rounded-full px-3 py-1.5 text-xs font-medium transition-colors ${
                          activeView === 'by-workflow'
                            ? 'bg-foreground text-background'
                            : 'border border-border bg-background text-muted-foreground hover:bg-muted hover:text-foreground'
                        }`}
                      >
                        Workflows
                      </button>
                    </>
                  )}
                  <button
                    onClick={() => setActiveView('schedules')}
                    className={`shrink-0 rounded-full px-3 py-1.5 text-xs font-medium transition-colors ${
                      activeView === 'schedules'
                        ? 'bg-foreground text-background'
                        : 'border border-border bg-background text-muted-foreground hover:bg-muted hover:text-foreground'
                    }`}
                  >
                    {isWorkflowScoped ? 'Schedules' : 'All Schedules'}
                  </button>
                  <button
                    onClick={() => setActiveView('calendar')}
                    className={`shrink-0 rounded-full px-3 py-1.5 text-xs font-medium transition-colors ${
                      activeView === 'calendar'
                        ? 'bg-foreground text-background'
                        : 'border border-border bg-background text-muted-foreground hover:bg-muted hover:text-foreground'
                    }`}
                  >
                    Month Calendar
                  </button>
                </div>

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
              <div className="border-b border-border px-5 py-4">
                <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
                    <div className="flex flex-1 max-w-4xl flex-col gap-3 md:flex-row">
                      <div className="relative flex-1">
                        <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
                        <input
                          type="text"
                          value={searchQuery}
                          onChange={(e) => setSearchQuery(e.target.value)}
                          placeholder={isWorkflowScoped ? 'Search schedules, cron, workspace...' : 'Search by automation, preset, cron, workspace...'}
                          className="w-full rounded-lg border border-border bg-background px-9 py-2 text-sm text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-amber-400/50"
                        />
                      </div>
                      {!isWorkflowScoped && (
                        <select
                          value={selectedWorkflowFilter}
                          onChange={(event) => setSelectedWorkflowFilter(event.target.value)}
                          className="min-w-48 rounded-lg border border-border bg-background px-3 py-2 text-sm text-foreground focus:outline-none focus:ring-2 focus:ring-amber-400/50"
                        >
                          <option value="all">All automations</option>
                          {workflowOptions.map((option) => (
                            <option key={option.value} value={option.value}>{option.label}</option>
                          ))}
                        </select>
                      )}
                    </div>

                  <div className="flex flex-wrap gap-2">
                    {filterPills.filter((pill) => pill.key === 'all' || pill.count > 0).map((pill) => (
                      <button
                        key={pill.key}
                        onClick={() => setActiveFilter(pill.key)}
                        className={`rounded-full px-3 py-1.5 text-xs font-medium transition-colors ${
                          activeFilter === pill.key
                            ? 'bg-foreground text-background'
                            : 'border border-border bg-background text-muted-foreground hover:bg-muted hover:text-foreground'
                        }`}
                      >
                        {pill.label} ({pill.count})
                      </button>
                    ))}
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
                className="text-xs text-amber-600 dark:text-amber-400 hover:underline"
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
                className="text-xs text-amber-600 dark:text-amber-400 hover:underline"
              >
                Clear search and show all schedules
              </button>
            </div>
          )}
          {panelJobs.length > 0 && activeView === 'schedules' && filteredJobs.length > 0 && (
            <ScheduleListView panel={panel} />
          )}
        </div>
      </div>

    </div>
    </TooltipProvider>
  )

  return embedded ? panelElement : <ModalPortal>{panelElement}</ModalPortal>
}

export default WorkflowScheduleRunsPanel
