import { useCallback, useEffect, useRef, useState } from 'react'
import { Inbox } from 'lucide-react'
import { agentApi } from '../../services/api'
import { workflowHasRecentActivity } from '../../utils/workflowActivity'
import { Tooltip, TooltipContent, TooltipTrigger } from '../ui/tooltip'

const ACTIVITY_REFRESH_MS = 30_000

interface GlobalActivityButtonProps {
  workspacePaths: string[]
  active: boolean
  onOpen: () => void
}

export function GlobalActivityButton({ workspacePaths, active, onOpen }: GlobalActivityButtonProps) {
  const [hasRecentActivity, setHasRecentActivity] = useState(false)
  const refreshSequence = useRef(0)
  const pathKey = workspacePaths.join('\u0000')

  const refresh = useCallback(async () => {
    const request = ++refreshSequence.current
    if (workspacePaths.length === 0) {
      setHasRecentActivity(false)
      return
    }
    try {
      const [notifications, humanInputs] = await Promise.all([
        agentApi.getOrgDashboardNotifications(workspacePaths, 1),
        agentApi.listReportHumanInputsAggregate(workspacePaths, 'pending').catch(() => ({ success: false, inputs: [] })),
      ])
      if (request !== refreshSequence.current) return
      const inputs = humanInputs.inputs || []
      setHasRecentActivity(workspacePaths.some(path => workflowHasRecentActivity(path, notifications, inputs)))
    } catch {
      if (request === refreshSequence.current) setHasRecentActivity(false)
    }
  // `pathKey` provides stable value-based identity when callers rebuild the array.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pathKey])

  useEffect(() => {
    const requestRefresh = () => {
      if (!document.hidden) void refresh()
    }
    requestRefresh()
    const interval = window.setInterval(requestRefresh, ACTIVITY_REFRESH_MS)
    document.addEventListener('visibilitychange', requestRefresh)
    return () => {
      refreshSequence.current += 1
      window.clearInterval(interval)
      document.removeEventListener('visibilitychange', requestRefresh)
    }
  }, [refresh])

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          onClick={onOpen}
          data-tour="global-activity"
          aria-label="Activity"
          aria-pressed={active}
          className={`rounded-md p-1.5 transition-colors ${active
            ? 'bg-primary/10 text-primary'
            : hasRecentActivity
              ? 'text-sky-500/70 hover:bg-sky-500/5 dark:text-sky-400/70'
              : 'text-muted-foreground hover:bg-muted hover:text-foreground'}`}
        >
          <Inbox className="h-4 w-4" />
        </button>
      </TooltipTrigger>
      <TooltipContent side="bottom">{hasRecentActivity ? 'Activity · Updates in the last 24 hours' : 'Activity · Updates'}</TooltipContent>
    </Tooltip>
  )
}
