import React, { useMemo } from 'react'
import { Layers } from 'lucide-react'
import { useRunningWorkflowsStore, useRunningWorkflows } from '../../stores/useRunningWorkflowsStore'
import { useChatStore } from '../../stores/useChatStore'
import { cn } from '@/lib/utils'
import { Tooltip, TooltipContent, TooltipTrigger } from '../ui/tooltip'

interface RunningWorkflowsIndicatorProps {
  variant?: 'floating' | 'sidebar'
  minimized?: boolean
}
export const RunningWorkflowsIndicator: React.FC<RunningWorkflowsIndicatorProps> = ({
  variant = 'floating',
  minimized = false
}) => {
  const setShowRunningDrawer = useRunningWorkflowsStore(state => state.setShowRunningDrawer)
  const runningWorkflows = useRunningWorkflows()

  const chatTabs = useChatStore(state => state.chatTabs)
  const tabSessionStatus = useChatStore(state => state.tabSessionStatus)
  const getTabStreamingStatus = useChatStore(state => state.getTabStreamingStatus)

  // Count running workflows (tracked + active tabs by streaming OR session status)
  const { running, hasWorkflows } = useMemo(() => {
    const seenSessionIds = new Set<string>()
    let runningCount = 0
    let anyWorkflows = runningWorkflows.length > 0

    // Count tracked workflows
    runningWorkflows.forEach(wf => {
      if (wf.status === 'running') runningCount++
      if (wf.sessionId) seenSessionIds.add(wf.sessionId)
    })

    // Count active workflow tabs not already tracked
    Object.values(chatTabs).forEach(tab => {
      if (tab.metadata?.mode !== 'workflow') return
      if (tab.sessionId && seenSessionIds.has(tab.sessionId)) return
      if (!tab.metadata?.presetQueryId) return
      if (tab.isCompleted) return

      anyWorkflows = true
      const isStreaming = getTabStreamingStatus(tab.tabId)
      const sessionStatus = tabSessionStatus[tab.tabId]?.status
      const isRunningSession = sessionStatus === 'running' || sessionStatus === 'active'
      if (isStreaming || isRunningSession) {
        runningCount++
      }
    })

    return { running: runningCount, hasWorkflows: anyWorkflows }
  }, [runningWorkflows, chatTabs, tabSessionStatus, getTabStreamingStatus])

  const hasRunning = running > 0
  const tooltipText = hasRunning ? `${running} running workflows` : "Workflows"

  if (variant === 'sidebar') {
    if (minimized) {
      return (
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              onClick={(e) => {
                e.stopPropagation()
                setShowRunningDrawer(true)
              }}
              className="p-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 transition-colors relative"
            >
              <Layers className="w-5 h-5" />
              {hasRunning && (
                <div className="absolute top-1 right-1">
                  <div className="w-2 h-2 rounded-full bg-green-500 border border-white dark:border-slate-900" />
                  <div className="absolute inset-0 w-2 h-2 rounded-full bg-green-500 animate-ping" />
                </div>
              )}
            </button>
          </TooltipTrigger>
          <TooltipContent side="right">
            <p>{tooltipText}</p>
          </TooltipContent>
        </Tooltip>
      )
    }

    // Expanded sidebar item
    return (
      <button
        onClick={() => setShowRunningDrawer(true)}
        className="w-full flex items-center justify-between px-2 py-2 text-sm text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-slate-800 rounded-md transition-colors group"
      >
        <div className="flex items-center gap-2">
          <Layers className="w-4 h-4 text-gray-500 dark:text-gray-400 group-hover:text-gray-700 dark:group-hover:text-gray-200" />
          <span>Workflows</span>
        </div>
        {hasRunning && (
          <span className="flex items-center justify-center min-w-[20px] h-5 px-1.5 text-xs font-semibold text-white bg-green-500 rounded-full">
            {running}
          </span>
        )}
      </button>
    )
  }

  return (
    <button
      onClick={() => setShowRunningDrawer(true)}
      className={cn(
        "absolute bottom-12 right-4 z-[40]",
        "flex items-center gap-2 px-3 py-2",
        "bg-background border rounded-full shadow-md",
        "hover:shadow-lg hover:scale-105 transition-all duration-200",
        "focus:outline-none focus:ring-2 focus:ring-primary focus:ring-offset-2",
        hasRunning ? "border-primary border-2" : hasWorkflows ? "border-border" : "border-border/50 opacity-70 hover:opacity-100"
      )}
      title={tooltipText}
    >
      <Layers className={cn(
        "w-5 h-5",
        hasRunning ? "text-primary" : hasWorkflows ? "text-muted-foreground" : "text-muted-foreground/50"
      )} />

      {/* Running indicator dot with pulse animation */}
      {hasRunning && (
        <div className="relative">
          <div className="w-2.5 h-2.5 rounded-full bg-green-500" />
          <div className="absolute inset-0 w-2.5 h-2.5 rounded-full bg-green-500 animate-ping" />
        </div>
      )}
    </button>
  )
}

// Debug: Track why this component re-renders
RunningWorkflowsIndicator.whyDidYouRender = true

export default RunningWorkflowsIndicator
