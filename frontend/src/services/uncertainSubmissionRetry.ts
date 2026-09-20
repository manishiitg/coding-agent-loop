import type { AxiosError, InternalAxiosRequestConfig } from 'axios'

// A 409 with this marker means the submission was durably journaled but the
// first attempt had not finished when this same-key request arrived. The
// retry must adopt the first attempt's outcome, never surface an error and
// never send a duplicate: re-posting the identical config (same
// Idempotency-Key) is idempotent by server contract.
const UNCERTAIN_DELIVERY_MARKER = 'delivery_uncertain'
const UNCERTAIN_DELIVERY_STATUS = 409

const MAX_UNCERTAIN_SUBMISSION_RETRIES = 3
const UNCERTAIN_SUBMISSION_BACKOFF_MS = [2000, 5000, 10000]

export type UncertainRetriableRequestConfig = InternalAxiosRequestConfig & {
  __uncertainSubmissionRetries?: number
}

export function isUncertainChatSubmission(error: unknown): boolean {
  if (!error || typeof error !== 'object' || !('response' in error)) return false
  const response = (error as { response?: { status?: number; data?: unknown } }).response
  if (response?.status !== UNCERTAIN_DELIVERY_STATUS) return false
  const data = (response.data ?? {}) as { error?: unknown; delivery_status?: unknown }
  return data.error === UNCERTAIN_DELIVERY_MARKER || data.delivery_status === UNCERTAIN_DELIVERY_MARKER
}

export function uncertainSubmissionRetryDelayMs(attempt: number): number {
  const index = Math.min(Math.max(attempt, 0), UNCERTAIN_SUBMISSION_BACKOFF_MS.length - 1)
  return UNCERTAIN_SUBMISSION_BACKOFF_MS[index]
}

function defaultSleep(ms: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, ms)
  })
}

export async function retryUncertainChatSubmission(
  instance: (config: UncertainRetriableRequestConfig) => Promise<unknown>,
  error: unknown,
  sleep: (ms: number) => Promise<void> = defaultSleep,
): Promise<unknown> {
  if (!isUncertainChatSubmission(error)) return Promise.reject(error)
  const config = (error as AxiosError).config as UncertainRetriableRequestConfig | undefined
  if (!config) return Promise.reject(error)
  const attempt = config.__uncertainSubmissionRetries ?? 0
  if (attempt >= MAX_UNCERTAIN_SUBMISSION_RETRIES) return Promise.reject(error)
  config.__uncertainSubmissionRetries = attempt + 1
  await sleep(uncertainSubmissionRetryDelayMs(attempt))
  return instance(config)
}
