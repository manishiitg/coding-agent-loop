export type WorkflowSessionActivity = {
  isStreaming: boolean
  hasRunningBackgroundAgents: boolean
  isBackendActive: boolean
}

export type ChatSessionActivity = WorkflowSessionActivity & {
  isVisible: boolean
}

/**
 * Keep a workflow transcript connected while any authoritative activity signal
 * remains. In particular, the backend can still deliver child completions after
 * the foreground turn and the local background-agent flag have settled.
 */
export function shouldKeepWorkflowSessionSubscribed(activity: WorkflowSessionActivity): boolean {
  return (
    activity.isStreaming ||
    activity.hasRunningBackgroundAgents ||
    activity.isBackendActive
  )
}

/**
 * A normal chat only needs a transport while it is visible or genuinely
 * active. Keeping every restored chat connected forever causes hidden,
 * completed transcripts to replay their durable journal on each reconnect.
 */
export function shouldKeepChatSessionSubscribed(activity: ChatSessionActivity): boolean {
  return activity.isVisible || shouldKeepWorkflowSessionSubscribed(activity)
}
