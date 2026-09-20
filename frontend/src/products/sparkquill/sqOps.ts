// Pure helpers for the SparkQuill page bridge: the sandboxed worksheet iframe
// talks to the app only through `parent.postMessage({__sq: 1, op, ...})`, and
// the host turns answer/timer ops into the child's next tutor message. Kept
// free of React/DOM so it unit-tests directly; LearningApp wires these into
// its viewer message listener.

export const SQ_MAX_TEXT = 2000
export const SQ_MAX_ID = 32
export const SQ_MAX_TIMERS = 40
// 5 seconds to 6 hours: shorter is a misclick vector, longer is not a test.
export const SQ_MIN_TIMER_SECONDS = 5
export const SQ_MAX_TIMER_SECONDS = 21600

/** Question/test ids back to a safe token; '' when there is nothing usable. */
export function sanitizeSqId(raw: unknown): string {
  if (typeof raw !== 'string') return ''
  return raw.replace(/[^A-Za-z0-9_-]/g, '').slice(0, SQ_MAX_ID)
}

function capText(raw: unknown): string {
  if (typeof raw !== 'string') return ''
  return raw.slice(0, SQ_MAX_TEXT)
}

/**
 * A widget answer as the tutor should receive it: `Q3: 3/5`. The qid prefix
 * is what lets the tutor match the answer to its question; without one the
 * raw value goes through unchanged. Null when there is no answer to send.
 */
export function buildSqAnswerText(qid: unknown, value: unknown): string | null {
  const text = capText(value).trim()
  if (!text) return null
  const id = sanitizeSqId(qid)
  return id ? `${id}: ${text}` : text
}

/**
 * A timer firing as the tutor should receive it. Always prefixed: the tutor
 * prompt treats `[Timer]` lines as system events, never as her words — an
 * expiry must not read as something she said.
 */
export function buildSqTimerText(qid: unknown): string {
  const id = sanitizeSqId(qid)
  return id
    ? `[Timer] Time expired on ${id} with no answer.`
    : '[Timer] Time expired on this test.'
}

export interface SqTimerConfig {
  qid: string
  seconds: number
}

/** A page's timer-config post back to valid timers; drops junk entries. */
export function sanitizeSqTimerConfigs(raw: unknown): SqTimerConfig[] {
  if (!Array.isArray(raw)) return []
  const out: SqTimerConfig[] = []
  const seen = new Set<string>()
  for (const entry of raw.slice(0, SQ_MAX_TIMERS)) {
    if (!entry || typeof entry !== 'object') continue
    const { qid, seconds } = entry as { qid?: unknown; seconds?: unknown }
    const id = sanitizeSqId(qid)
    if (typeof seconds !== 'number' || !Number.isFinite(seconds)) continue
    const secs = Math.floor(seconds)
    if (secs < SQ_MIN_TIMER_SECONDS || secs > SQ_MAX_TIMER_SECONDS) continue
    if (seen.has(id)) continue
    seen.add(id)
    out.push({ qid: id, seconds: secs })
  }
  return out
}
