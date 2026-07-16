import { createRequestCoalescer } from '../services/requestCoalescer'

export type LiveInputSubmissionCoordinator = <T>(
  sessionKey: string,
  message: string,
  submit: () => Promise<T>,
) => Promise<T>

// A rapid Enter/double-click can invoke ChatInput twice before the first
// /live-input response clears the draft. Share the complete submission promise
// for that exact session + message so the HTTP mutation, optimistic event, and
// fallback decision all execute once. A later intentional repeat is allowed
// after the first submission settles.
export function createLiveInputSubmissionCoordinator(): LiveInputSubmissionCoordinator {
  const coalesce = createRequestCoalescer()
  return <T>(sessionKey: string, message: string, submit: () => Promise<T>) => {
    const key = JSON.stringify([sessionKey, message.trim()])
    return coalesce(key, submit)
  }
}

export const liveInputSubmissionCoordinator = createLiveInputSubmissionCoordinator()
