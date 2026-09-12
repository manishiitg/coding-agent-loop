import { lazy, Suspense, useState } from 'react'
import { CalendarDays, LayoutDashboard, MessageSquare } from 'lucide-react'
import { EmployeeDashboard } from './EmployeeDashboard'
import { useLLMStore } from '../stores/useLLMStore'
import { useAppStore } from '../stores/useAppStore'

const Schedules = lazy(() => import('./scheduler/WorkflowScheduleRunsPanel'))
const tabs = [
  { id: 'updates', label: 'Updates', icon: MessageSquare },
  { id: 'schedules', label: 'Schedules', icon: CalendarDays },
] as const

type ActivityTab = typeof tabs[number]['id']

/** Keep both views mounted after first use so filters and selection survive navigation. */
export default function ActivityPage() {
  const showActivity = useAppStore(state => state.showWorkflowsOverview)
  const showProviders = useLLMStore(state => state.showLLMModal)
  const [activeTab, setActiveTab] = useState<ActivityTab>('updates')
  const [openedSchedules, setOpenedSchedules] = useState(false)
  const setShowWorkflowsOverview = useAppStore(state => state.setShowWorkflowsOverview)
  const selectTab = (tab: ActivityTab) => {
    if (tab === 'schedules') setOpenedSchedules(true)
    setActiveTab(tab)
  }

  return (
    <section aria-label="Activity" className="flex h-full min-h-0 flex-col bg-background">
      <header className="flex shrink-0 flex-wrap items-center gap-x-6 border-b border-border px-4 sm:px-6">
        <div className="flex items-center gap-2 py-3">
          <LayoutDashboard className="h-4 w-4 text-primary" />
          <div>
            <h1 className="text-base font-semibold text-foreground">Activity</h1>
            <p className="sr-only">Updates, decisions, and schedules across your automations.</p>
          </div>
        </div>
        <div role="tablist" aria-label="Activity views" className="flex gap-5">
          {tabs.map(({ id, label, icon: Icon }, index) => (
            <button
              key={id}
              type="button"
              role="tab"
              id={`activity-tab-${id}`}
              aria-controls={`activity-panel-${id}`}
              aria-selected={activeTab === id}
              tabIndex={activeTab === id ? 0 : -1}
              onClick={() => selectTab(id)}
              onKeyDown={event => {
                const next = event.key === 'Home' ? 0 : event.key === 'End' ? tabs.length - 1
                  : event.key === 'ArrowRight' ? (index + 1) % tabs.length
                  : event.key === 'ArrowLeft' ? (index + tabs.length - 1) % tabs.length : null
                if (next === null) return
                event.preventDefault()
                selectTab(tabs[next].id)
                document.getElementById(`activity-tab-${tabs[next].id}`)?.focus()
              }}
              className={`flex items-center gap-2 border-b-2 px-1 py-3 text-sm font-medium transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary ${activeTab === id
                ? 'border-primary text-foreground'
                : 'border-transparent text-muted-foreground hover:text-foreground'}`}
            >
              <Icon className="h-4 w-4" />{label}
            </button>
          ))}
        </div>
      </header>
      <div id="activity-panel-updates" role="tabpanel" aria-labelledby="activity-tab-updates" hidden={activeTab !== 'updates'} className="min-h-0 flex-1">
        <EmployeeDashboard />
      </div>
      <div id="activity-panel-schedules" role="tabpanel" aria-labelledby="activity-tab-schedules" hidden={activeTab !== 'schedules'} className="min-h-0 flex-1">
        {openedSchedules && <Suspense fallback={<div className="p-6 text-sm text-muted-foreground">Loading schedules…</div>}>
          <Schedules embedded active={showActivity && !showProviders && activeTab === 'schedules'} onClose={() => setShowWorkflowsOverview(false)} />
        </Suspense>}
      </div>
    </section>
  )
}
