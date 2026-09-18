export interface ChatSubmissionOptions {
  isAutoNotification?: boolean
  preferLiveInput?: boolean
  sourceTabId?: string
  sourceSessionId?: string
  sourceComposerId?: string
  identity?: number
  submissionId?: string
  queuedDelivery?: boolean
  builderHandoff?: { tabId?: string; sessionId?: string }
}

// Only submissions already pending from the same composer share its newly
// created conversation. Returning to Builder mounts a different composer.
const pendingBuilders = new Map<string, { target: NonNullable<ChatSubmissionOptions['builderHandoff']>; count: number }>()
export function acquireBuilderSubmission(key: string) {
  let batch = pendingBuilders.get(key)
  if (!batch) {
    batch = { target: {}, count: 0 }
    pendingBuilders.set(key, batch)
  }
  batch.count++
  const owned = batch
  return { target: owned.target, release: () => {
    if (--owned.count === 0 && pendingBuilders.get(key) === owned) pendingBuilders.delete(key)
  } }
}
