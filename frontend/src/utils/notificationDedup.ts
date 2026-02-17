/**
 * Notification deduplication utility.
 * Persists to localStorage so sounds/notifications don't replay on page refresh.
 */

const NOTIFIED_KEY = 'mcp_notified_request_ids'
const SUBMITTED_FEEDBACK_KEY = 'mcp_submitted_feedback'
const SUBMITTED_QUESTIONS_KEY = 'mcp_submitted_questions'
// Entries older than 1 hour are purged
const MAX_AGE_MS = 60 * 60 * 1000

interface TimestampedEntry<T> {
  value: T
  ts: number
}

function loadMap<T>(key: string): Map<string, TimestampedEntry<T>> {
  try {
    const raw = localStorage.getItem(key)
    if (!raw) return new Map()
    const entries: [string, TimestampedEntry<T>][] = JSON.parse(raw)
    const now = Date.now()
    // Filter out expired entries
    return new Map(entries.filter(([, entry]) => now - entry.ts < MAX_AGE_MS))
  } catch {
    return new Map()
  }
}

function saveMap<T>(key: string, map: Map<string, TimestampedEntry<T>>): void {
  try {
    localStorage.setItem(key, JSON.stringify(Array.from(map.entries())))
  } catch {
    // localStorage full or unavailable — ignore
  }
}

// --- Notification dedup (has this request already triggered a sound?) ---

export function hasBeenNotified(requestId: string): boolean {
  const map = loadMap<boolean>(NOTIFIED_KEY)
  return map.has(requestId)
}

export function markNotified(requestId: string): void {
  const map = loadMap<boolean>(NOTIFIED_KEY)
  map.set(requestId, { value: true, ts: Date.now() })
  saveMap(NOTIFIED_KEY, map)
}

// --- Submitted feedback cache (persisted across page reloads) ---

export function getSubmittedFeedback(requestId: string): string | undefined {
  const map = loadMap<string>(SUBMITTED_FEEDBACK_KEY)
  return map.get(requestId)?.value
}

export function setSubmittedFeedback(requestId: string, feedback: string): void {
  const map = loadMap<string>(SUBMITTED_FEEDBACK_KEY)
  map.set(requestId, { value: feedback, ts: Date.now() })
  saveMap(SUBMITTED_FEEDBACK_KEY, map)
}

// --- Submitted questions cache (persisted across page reloads) ---

interface QuestionsData {
  answers: Record<string, string>
  general_feedback: string
}

export function getSubmittedQuestions(requestId: string): QuestionsData | undefined {
  const map = loadMap<QuestionsData>(SUBMITTED_QUESTIONS_KEY)
  return map.get(requestId)?.value
}

export function setSubmittedQuestions(requestId: string, data: QuestionsData): void {
  const map = loadMap<QuestionsData>(SUBMITTED_QUESTIONS_KEY)
  map.set(requestId, { value: data, ts: Date.now() })
  saveMap(SUBMITTED_QUESTIONS_KEY, map)
}
