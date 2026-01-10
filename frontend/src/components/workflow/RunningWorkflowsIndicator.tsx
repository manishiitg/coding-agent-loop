import React, { useMemo } from 'react'
import { Layers } from 'lucide-react'
import { useRunningWorkflowsStore, useRunningWorkflows } from '../../stores/useRunningWorkflowsStore'
import { useChatStore } from '../../stores/useChatStore'
import { cn } from '@/lib/utils'

/**
 * Floating indicator showing count of running workflows.
 * Combines tracked workflows with active workflow tabs.
 * Always visible in bottom-right corner.
 * Click to open the RunningWorkflowsDrawer.
 */
export const RunningWorkflowsIndicator: React.FC = () => {
  const setShowRunningDrawer = useRunningWorkflowsStore(state => state.setShowRunningDrawer)
  const runningWorkflows = useRunningWorkflows()

  const chatTabs = useChatStore(state => state.chatTabs)
  const getTabStreamingStatus = useChatStore(state => state.getTabStreamingStatus)

  // Count all workflows (tracked + active streaming tabs)
  const { running, total } = useMemo(() => {
    const seenSessionIds = new Set<string>()
    let runningCount = 0
    let totalCount = 0

    // Count tracked workflows
    runningWorkflows.forEach(wf => {
      totalCount++
      if (wf.status === 'running') runningCount++
      if (wf.sessionId) seenSessionIds.add(wf.sessionId)
    })

    // Count active workflow tabs not already tracked
    Object.values(chatTabs).forEach(tab => {
      if (tab.metadata?.mode !== 'workflow') return
      if (tab.sessionId && seenSessionIds.has(tab.sessionId)) return
      if (!tab.metadata?.presetQueryId) return

      const isStreaming = getTabStreamingStatus(tab.tabId)
      if (isStreaming) {
        totalCount++
        runningCount++
      }
    })

    return { running: runningCount, total: totalCount }
  }, [runningWorkflows, chatTabs, getTabStreamingStatus])

  const hasRunning = running > 0
  const hasWorkflows = total > 0

  return (
    <button
      onClick={() => setShowRunningDrawer(true)}
      className={cn(
        "absolute bottom-4 right-4 z-[40]",
        "flex items-center gap-2 px-3 py-2",
        "bg-background border rounded-full shadow-md",
        "hover:shadow-lg hover:scale-105 transition-all duration-200",
        "focus:outline-none focus:ring-2 focus:ring-primary focus:ring-offset-2",
        hasRunning ? "border-primary border-2" : hasWorkflows ? "border-border" : "border-border/50 opacity-70 hover:opacity-100"
      )}
      title={hasWorkflows ? `${running} running, ${total} total workflows` : "Running workflows"}
    >
      <Layers className={cn(
        "w-4 h-4",
        hasRunning ? "text-primary" : hasWorkflows ? "text-muted-foreground" : "text-muted-foreground/50"
      )} />

      {/* Badge showing count - always show */}
      <div className={cn(
        "flex items-center justify-center min-w-[20px] h-5 px-1.5",
        "text-xs font-bold rounded-full",
        hasRunning
          ? "bg-primary text-primary-foreground"
          : hasWorkflows
            ? "bg-muted text-muted-foreground"
            : "bg-muted/50 text-muted-foreground/70"
      )}>
        {total > 99 ? '99+' : total}
      </div>

      {/* Running indicator dot with pulse animation */}
      {hasRunning && (
        <div className="relative">
          <div className="w-2 h-2 rounded-full bg-green-500" />
          <div className="absolute inset-0 w-2 h-2 rounded-full bg-green-500 animate-ping" />
        </div>
      )}
    </button>
  )
}

// Debug: Track why this component re-renders
RunningWorkflowsIndicator.whyDidYouRender = true

export default RunningWorkflowsIndicator
