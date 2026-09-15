import { lazy, Suspense } from 'react'
import { CalendarClock } from 'lucide-react'
import { useLLMStore } from '../stores/useLLMStore'
import { useAppStore } from '../stores/useAppStore'

const Schedules = lazy(() => import('./scheduler/WorkflowScheduleRunsPanel'))

/** Standalone schedules page, opened from the top-bar icon next to Activity. */
export default function SchedulesPage() {
  const showSchedules = useAppStore(state => state.showSchedulesOverview)
  const showProviders = useLLMStore(state => state.showLLMModal)
  const setShowSchedulesOverview = useAppStore(state => state.setShowSchedulesOverview)

  return (
    <section aria-label="Schedules" className="flex h-full min-h-0 flex-col bg-background">
      <header className="flex shrink-0 flex-wrap items-center gap-x-6 border-b border-border px-4 sm:px-6">
        <div className="flex items-center gap-2 py-3">
          <CalendarClock className="h-4 w-4 text-primary" />
          <div>
            <h1 className="text-base font-semibold text-foreground">Schedules</h1>
            <p className="sr-only">Automation schedules grouped by workflow.</p>
          </div>
        </div>
      </header>
      <div className="min-h-0 flex-1">
        <Suspense fallback={<div className="p-6 text-sm text-muted-foreground">Loading schedules…</div>}>
          <Schedules embedded active={showSchedules && !showProviders} onClose={() => setShowSchedulesOverview(false)} />
        </Suspense>
      </div>
    </section>
  )
}
