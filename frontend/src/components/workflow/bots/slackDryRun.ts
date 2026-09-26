import { agentApi } from '../../../services/api'
import type { SlackConnectionCheck, SlackTestResponse } from '../../../services/api-types'

// A bot's own crew or workflow answers in any channel, so any channel ID
// exercises its route; nothing is posted there.
const SLACK_DRY_RUN_CHANNEL = 'C0DRYRUN00'

/**
 * Adds a dry run to a passing token test: a mention goes through the real
 * inbound path and stops before the model, so Save & test shows whether the
 * bot would actually answer, or the exact refusal the user would get.
 */
export async function withSlackDryRun(connectionId: string, result: SlackTestResponse, noun: 'crew' | 'workflow'): Promise<SlackTestResponse> {
  let check: SlackConnectionCheck
  let success = result.success
  let message = result.message
  try {
    const dryRun = await agentApi.dryRunSlackConnection(connectionId, SLACK_DRY_RUN_CHANNEL)
    if (dryRun.admitted) {
      // Channels are groups: the bot answers there in Run mode, whoever asks.
      const mode = dryRun.mode === 'run' ? 'answers in Run mode in channels' : ''
      check = { name: `A mention reaches this ${noun}`, status: 'passed', message: [dryRun.destination, mode].filter(Boolean).join(' · ') }
    } else {
      const reply = dryRun.replies?.[0] || dryRun.reason || 'The bot would not answer.'
      check = { name: `A mention reaches this ${noun}`, status: 'failed', message: `${reply}${dryRun.reason && dryRun.reason !== reply ? ` (${dryRun.reason})` : ''}` }
      success = false
      message = `The tokens work, but a mention would not reach this ${noun}.`
    }
  } catch (err) {
    check = { name: `A mention reaches this ${noun}`, status: 'failed', message: err instanceof Error ? err.message : 'Dry run failed' }
    success = false
    message = `The tokens work, but the routing check could not run.`
  }
  return { ...result, success, message, checks: [...(result.checks || []), check] }
}
