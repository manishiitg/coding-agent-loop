import type {
  PulseFindingLifecycle,
  PulseFindingVerification,
  PulseFixAttempt,
} from '../../services/api-types'
import {
  acknowledgedReason,
  isPulseFindingClosed,
  isPulseOwnedFinding,
  pulseIssueForFinding,
} from './pulseModuleInspectorUtils'

export type PulseFindingQueue =
  | 'needs_action'
  | 'queued_repair'
  | 'waiting_proof'
  | 'decisions'
  | 'proposals'
  | 'blocked'
  | 'platform'
  | 'resolved'
  | 'workflow_reported'

export type PulseFindingTone = 'danger' | 'warning' | 'info' | 'success' | 'decision' | 'neutral'

export type PulseFindingPresentation = {
  label: string
  queue: PulseFindingQueue
  tone: PulseFindingTone
  nextAction: string
}

export type PulseFindingProgressStep = {
  label: string
  state: 'done' | 'current' | 'pending'
}

function readable(value?: string): string {
  return (value || '').trim().replaceAll('_', ' ')
}

function titleCase(value: string): string {
  return value.replace(/\b\w/g, (character) => character.toUpperCase())
}

function latestVerification(finding: PulseFindingLifecycle): PulseFindingVerification | undefined {
  // The lifecycle API returns verification records newest-first. A historical
  // passing check must not hide a later failed check on the same finding.
  return finding.verifications[0]
}

/**
 * The raw step_id is the durable origin of a finding. `module` may be a
 * normalized grouping (for example an old bug_review is grouped under the new
 * workflow_review), so prefer step_id when naming who actually reported it.
 */
export function pulseFindingReporter(
  finding: PulseFindingLifecycle,
  groupedModuleLabel?: string,
): string {
  const origin = readable(finding.step_id || finding.module)
  if (finding.phase !== 'review') {
    return origin ? `Workflow step · ${origin}` : 'Workflow run'
  }
  if (!origin) return groupedModuleLabel || 'Pulse reviewer'
  // Review-area membership must not rename the step that originally reported it.
  if (!['technical_review', 'workflow_review', 'llm_ops_review', 'strategic_review',
    'strategy_auditor', 'goal_advisor', 'plan_drift_review'].includes(finding.step_id || finding.module || '')) {
    return titleCase(origin)
  }
  if (finding.step_id && finding.module && finding.step_id !== finding.module) {
    return titleCase(origin)
  }
  return groupedModuleLabel || titleCase(origin)
}

export function pulseFindingDisposition(finding: PulseFindingLifecycle): string {
  for (const event of finding.events) {
    const disposition = event.metadata?.disposition
    if (typeof disposition === 'string' && disposition.trim()) return disposition.trim()
  }
  for (const attempt of finding.fix_attempts) {
    const issueID = finding.issue?.id || finding.finding_id
    const reference = attempt.findings?.find((candidate) => (
      issueID && candidate.finding_id === issueID
    ))
    if (reference?.disposition?.trim()) return reference.disposition.trim()
  }
  return ''
}

/**
 * A repair can be recorded, then a later ordinary workflow run can show that
 * the same problem still exists. Keep that history visible: presenting the
 * finding as merely "New" discards the most useful part of its lifecycle.
 */
function hasRecordedAppliedFix(finding: PulseFindingLifecycle): boolean {
  const disposition = pulseFindingDisposition(finding)
  return finding.fix_attempts.some((attempt) => attempt.changed_files.length > 0)
    || finding.events.some((event) => event.event_type === 'fix_applied')
    || ['changed_unverified', 'fixed_verified'].includes(disposition)
}

function reopenedAfterAppliedFix(finding: PulseFindingLifecycle): boolean {
  return finding.status === 'open' && hasRecordedAppliedFix(finding)
}

function closedLabel(finding: PulseFindingLifecycle): string {
  const disposition = pulseFindingDisposition(finding)
  if (disposition === 'fixed_verified') return 'Fixed and verified'
  if (disposition === 'verified_no_change') return 'Closed · no fix needed'
  if (finding.status === 'rejected') return 'Closed · not proceeding'
  return 'Resolved'
}

export function pulseFindingPresentation(finding: PulseFindingLifecycle): PulseFindingPresentation {
  if (!isPulseOwnedFinding(finding)) {
    return {
      label: 'Reported by workflow run',
      queue: 'workflow_reported',
      tone: 'neutral',
      nextAction: finding.resolution_note?.trim()
        || 'Triage this run evidence and link it to a canonical Pulse issue when it describes the same behavior.',
    }
  }

  if (finding.status === 'external_action_required') {
    const owner = readable(finding.external_owner) || 'platform owner'
    return {
      label: 'Platform action required',
      queue: 'platform',
      tone: 'neutral',
      nextAction: finding.resolution_note?.trim()
        || (finding.reopen_condition?.trim()
          ? `Owned by ${owner}. Reopen when ${finding.reopen_condition.trim()}`
          : `Owned by ${owner}; Pulse cannot repair it inside this workflow.`),
    }
  }

  if (isPulseFindingClosed(finding.status)) {
    return {
      label: closedLabel(finding),
      queue: 'resolved',
      tone: 'success',
      nextAction: finding.resolution_note?.trim() || 'No further action is required unless new evidence reopens the issue.',
    }
  }

  if (finding.status === 'fixing') {
    return {
      label: 'Fix in progress',
      queue: 'needs_action',
      tone: 'info',
      nextAction: finding.resolution_note?.trim()
        || finding.fix_attempts[0]?.summary?.trim()
        || 'Pulse is applying the current repair attempt.',
    }
  }

  if (finding.status === 'awaiting_verification') {
    const failed = latestVerification(finding)?.verdict === 'failed'
    return {
      label: failed ? 'Verification failed' : 'Fix applied · needs verification',
      queue: failed ? 'needs_action' : 'waiting_proof',
      tone: failed ? 'danger' : 'warning',
      nextAction: finding.resolution_note?.trim()
        || (failed
          ? 'Review the failed check, reopen the repair, and run verification again.'
          : 'Run the required post-change check before closing this issue.'),
    }
  }

  if (finding.status === 'awaiting_run') {
    return {
      label: 'Waiting for workflow run',
      queue: 'waiting_proof',
      tone: 'warning',
      nextAction: finding.details?.next_check?.trim() || finding.resolution_note?.trim()
        || 'Run the producing workflow again so Pulse can verify the changed behavior.',
    }
  }

  if (finding.status === 'queued_for_engineering') {
    const missingDecisionRequest = finding.events.some((event) => (
      event.event_type === 'decision_request_missing'
    ))
    // Pulse may defer an issue once; repeated deferral means it is stuck.
    const timesDeferred = finding.events.filter(event => event.event_type === 'queued_for_engineering').length
    const stuck = timesDeferred > 1
    return {
      label: missingDecisionRequest ? 'Decision request missing' : stuck ? `Deferred ${timesDeferred}×` : 'Queued for Pulse',
      queue: 'queued_repair',
      tone: missingDecisionRequest || stuck ? 'danger' : 'info',
      nextAction: finding.resolution_note?.trim() || (missingDecisionRequest
        ? 'Pulse must re-review this finding and create a linked, answerable decision only if one is still needed.'
        : finding.details?.next_check?.trim()
          || 'Pulse will select this safe repair in a later Engineering pass.'),
    }
  }

  if (finding.status === 'open' && latestVerification(finding)?.verdict === 'failed') {
    return {
      label: 'Verification failed',
      queue: 'needs_action',
      tone: 'danger',
      nextAction: finding.resolution_note?.trim()
        || 'The latest verification failed; Pulse must reopen the repair and check it again.',
    }
  }

  if (reopenedAfterAppliedFix(finding)) {
    return {
      label: 'Reopened after fix',
      queue: 'needs_action',
      tone: 'danger',
      nextAction: 'New workflow evidence appeared after the recorded repair. Pulse must compare that run with the repair, then either repair again or record why this is a distinct issue.',
    }
  }

  // Current structured evidence requirements supersede legacy proposal events.
  // Active repairs, failed verification, and terminal states above still win.
  if (['open', 'acknowledged'].includes(finding.status)
    && finding.details?.recommended_route === 'evidence_wait') {
    return {
      label: 'Waiting for evidence',
      queue: 'waiting_proof',
      tone: 'warning',
      nextAction: finding.details.next_check?.trim()
        || 'Pulse must record the exact future evidence boundary before revisiting this recommendation.',
    }
  }

  if (finding.status === 'acknowledged') {
    const reason = acknowledgedReason(finding)
    if (reason === 'awaiting_user') {
      const hasDecisionRequest = finding.events.some((event) => (
        typeof event.metadata?.human_input_id === 'string'
        && event.metadata.human_input_id.trim().length > 0
      ))
      return {
        label: hasDecisionRequest ? 'Your decision needed' : 'Decision request missing',
        queue: hasDecisionRequest ? 'decisions' : 'needs_action',
        tone: hasDecisionRequest ? 'decision' : 'danger',
        nextAction: finding.resolution_note?.trim() || (hasDecisionRequest
          ? 'Choose the requested option before Pulse continues.'
          : 'Pulse must create and link an answerable decision before waiting on you.'),
      }
    }
    if (reason === 'proposal') {
      return {
        label: 'Proposed improvement',
        queue: 'proposals',
        tone: 'info',
        nextAction: finding.resolution_note?.trim() || 'This is an idea for consideration; it is not blocked on your approval.',
      }
    }
    if (reason === 'blocked') {
      // Old records used `blocked` to mean both “cannot act” and “not selected
      // this pass.” Preserve their useful intent until the new durable queue
      // state arrives, rather than falsely presenting deferred work as dead.
      const latest = finding.events[0]?.summary?.toLowerCase() || ''
      if (/next .*run|needs triage on the next|needs another .*run/.test(latest)) {
        return {
          label: 'Waiting for next run',
          queue: 'waiting_proof',
          tone: 'warning',
          nextAction: finding.resolution_note?.trim() || finding.events[0]?.summary || 'A future workflow run will determine the next repair.',
        }
      }
      if (/not attempted|deprioritized|deferred to (a )?future|next engineering pass/.test(latest)) {
        return {
          label: 'Queued for Pulse',
          queue: 'queued_repair',
          tone: 'info',
          nextAction: finding.resolution_note?.trim() || finding.events[0]?.summary || 'Pulse will select this safe repair in a later Engineering pass.',
        }
      }
      return {
        label: 'Paused · no safe action',
        queue: 'blocked',
        tone: 'neutral',
        nextAction: finding.resolution_note?.trim()
          || (finding.reopen_condition?.trim()
            ? `Reopen when ${finding.reopen_condition.trim()}`
            : 'Pulse has diagnosed this issue but has no safe repair path.'),
      }
    }
  }

  const advisorModule = finding.step_id || finding.module
  const advisorFinding = finding.phase === 'review'
    && ['strategic_review', 'strategy_auditor', 'goal_advisor'].includes(advisorModule || '')
  if (advisorFinding && finding.details?.recommended_route !== 'fixer_handoff') {
    return {
      label: 'Untriaged recommendation',
      queue: 'proposals',
      tone: 'info',
      nextAction: finding.details?.recommended_route === 'decision_required'
        ? 'Pulse must create and link the decision card before asking you to act.'
        : 'Pulse must classify this recommendation as a decision, evidence wait, or technical handoff before acting.',
    }
  }

  return {
    label: 'New',
    queue: 'needs_action',
    tone: 'danger',
    nextAction: finding.resolution_note?.trim() || 'Pulse needs to diagnose this issue and assign a repair.',
  }
}

export function pulseFindingProgress(finding: PulseFindingLifecycle): PulseFindingProgressStep[] {
  const presentation = pulseFindingPresentation(finding)
  const disposition = pulseFindingDisposition(finding)
  const diagnosed = finding.fix_attempts.length > 0
    || finding.events.some((event) => !['filed', 'rediscovered'].includes(event.event_type))
    || presentation.queue === 'decisions'
    || presentation.queue === 'proposals'
    || presentation.queue === 'blocked'
    || presentation.queue === 'platform'
    || presentation.queue === 'resolved'
  const fixApplied = hasRecordedAppliedFix(finding)
  const verified = latestVerification(finding)?.verdict === 'passed'
    || ['fixed_verified', 'verified_no_change'].includes(disposition)
  if (reopenedAfterAppliedFix(finding)) {
    const flags = [true, diagnosed, fixApplied, verified, false]
    const labels = ['Found', 'Diagnosed', 'Fix applied', 'Verified', 'Reopened']
    return labels.map((label, index) => ({
      label,
      // Reopened is the current condition, even when an earlier verification
      // stage is absent. Otherwise the UI implies that it is merely waiting
      // for a first verification rather than needing the repair reconsidered.
      state: flags[index] ? 'done' : index === labels.length - 1 ? 'current' : 'pending',
    }))
  }
  const closed = presentation.queue === 'resolved'
  const flags = [true, diagnosed, fixApplied, verified, closed]
  const labels = ['Found', 'Diagnosed', 'Fix applied', 'Verified', 'Closed']
  const firstPending = flags.findIndex((flag) => !flag)
  return labels.map((label, index) => ({
    label,
    state: flags[index] ? 'done' : index === firstPending ? 'current' : 'pending',
  }))
}

export function pulseVerificationLevel(verification: PulseFindingVerification): string {
  const text = `${verification.check} ${verification.expected || ''} ${verification.observed || ''}`.toLowerCase()
  if (/\b(static|structural|configuration|config inspection|schema|plan validation|file check)\b/.test(text)) {
    return 'Static check'
  }
  if (/\b(replay|fixture|deterministic|inert|model cases?)\b/.test(text)) {
    return 'Deterministic check'
  }
  if (/\b(runtime|producing|workflow run|consumer behavior|publish|linkedin|evaluation runtime)\b/.test(text)) {
    return 'Producing-run check'
  }
  return 'Verification check'
}

export function pulseFixAttemptIsIncomplete(
  finding: PulseFindingLifecycle,
  attempt: PulseFixAttempt,
): boolean {
  if (attempt.status !== 'fixing' || attempt.completed_at) return false
  return finding.status !== 'fixing'
    || finding.events.some((event) => event.attempt_id === attempt.attempt_id && event.event_type !== 'fix_started')
}

export function pulseFindingImpact(finding: PulseFindingLifecycle): string {
  const issue = pulseIssueForFinding(finding)
  return finding.details?.impact?.trim() || issue.description?.trim() || ''
}
