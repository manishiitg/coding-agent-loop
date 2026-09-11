import type { ReportHumanInputChatResult } from '../../../utils/reportHumanInputChat'
import type { ReportChatOptions, ReportChatReceipt } from './reportEmbedContext'

type Dispatch = (request: { workspacePath: string; message: string }) => Promise<ReportHumanInputChatResult>
type PendingRequest = {
  message: string
  requestId?: string
  result: Promise<ReportChatReceipt>
}

/** Dispatch through the same workflow chat lane as human decisions.
 * Duplicate protection is per view; durable execution checks belong to the consumer. */
export class ReportChatRequestController {
  private pending: PendingRequest | null = null
  private active = true
  private receipts = new Map<string, { message: string; receipt: ReportChatReceipt }>()

  readonly workspacePath: string
  private dispatch: Dispatch

  constructor(workspacePath: string, dispatch: Dispatch) {
    this.workspacePath = workspacePath
    this.dispatch = dispatch
  }

  activate = () => { this.active = true }
  // Navigation must not cancel an enqueue already accepted from a user action.
  dispose = () => { this.active = false }

  request = (message: string, options?: ReportChatOptions): Promise<ReportChatReceipt> => {
    if (!this.active) return Promise.reject(new Error('This report is no longer open.'))
    if (typeof message !== 'string' || !message.trim() || message.length > 12000) {
      return Promise.reject(new Error('Provide a message between 1 and 12,000 characters.'))
    }
    if (options != null && (typeof options !== 'object' || Array.isArray(options))) {
      return Promise.reject(new Error('Chat options must be an object.'))
    }
    const requestId = options?.requestId
    if (requestId !== undefined && (typeof requestId !== 'string' || !requestId.trim() || requestId.length > 200)) {
      return Promise.reject(new Error('requestId must be a non-empty string of at most 200 characters.'))
    }
    message = message.trim()
    if (requestId) {
      const previous = this.receipts.get(requestId)
      if (previous) return previous.message === message
        ? Promise.resolve(previous.receipt)
        : Promise.reject(new Error('This requestId was already used for a different message. Use a new item/version/action ID.'))
    }
    if (this.pending) {
      if (this.pending.message === message && this.pending.requestId === requestId) return this.pending.result
      return Promise.reject(new Error('Wait for the current report message to queue before sending another.'))
    }
    const result = Promise.resolve().then(async (): Promise<ReportChatReceipt> => {
      const queued = await this.dispatch({
        workspacePath: this.workspacePath,
        message: `From the report for ${this.workspacePath}:\n\n${message}`,
      })
      const receipt: ReportChatReceipt = { status: 'queued', ...queued }
      if (requestId) {
        this.receipts.set(requestId, { message, receipt })
        if (this.receipts.size > 100) this.receipts.delete(this.receipts.keys().next().value!)
      }
      return receipt
    }).finally(() => { this.pending = null })
    this.pending = { message, requestId, result }
    return result
  }
}
