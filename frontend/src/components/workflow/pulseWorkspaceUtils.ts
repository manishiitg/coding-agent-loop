import type {
  PulseFindingLifecycle,
  PulseReviewRecord,
  PulseReviewFocus,
} from '../../services/api-types'
import { pulseIssueForFinding } from './pulseModuleInspectorUtils'
import { pulseFindingPresentation, type PulseFindingQueue } from './pulseFindingPresentation'

export type PulseWorkspaceModuleDefinition = {
  id: string
  label: string
  description: string
}

export type PulseWorkspaceModuleSummary = PulseWorkspaceModuleDefinition & {
  findings: number
  /** Work Pulse can diagnose or repair now, including an in-progress fix. */
  active: number
  fixing: number
  awaitingVerification: number
  awaitingRun: number
  queuedForEngineering: number
  awaitingUser: number
  blocked: number
  proposals: number
  closed: number
  externalAction: number
  workflowReported: number
  recurring: number
  latestReview: PulseReviewRecord | null
}

export function normalizePulseWorkspaceModule(module?: string): string {
  const value = (module || '').trim()
  if (value === 'workflow_review' || value === 'llm_ops_review') return 'technical_review'
  if (value === 'strategy_auditor' || value === 'goal_advisor') return 'strategic_review'
  return value
}

/** A reporting step is not a review area. Explicit reviewer associations can
 * put an issue in more than one area; never infer ownership from its prose. */
export function pulseFindingReviewAreas(
  finding: PulseFindingLifecycle,
  selections: PulseReviewFocus[] = [],
): string[] {
  const areas = new Set<string>()
  const add = (value?: string) => {
    const area = normalizePulseWorkspaceModule(value)
    if (['technical_review', 'architecture_review', 'strategic_review', 'plan_drift_review'].includes(area)) areas.add(area)
  }
  add(finding.module)
  add(finding.issue?.module)
  add(finding.step_id)
  const id = pulseIssueForFinding(finding).id.trim().toUpperCase()
  if (id !== 'PUL-UNKNOWN') {
    selections.forEach((selection) => {
      if (selection.issue_ids?.some((candidate) => candidate.trim().toUpperCase() === id)) add(selection.module)
    })
  }
  return [...areas]
}

export type PulseFocus = 'all' | PulseFindingQueue

/** Shared by the badges and the result list; counts intentionally ignore the
 * selected queue, but always respect the selected review area. */
export function pulseWorkspaceQueueCounts(findings: PulseFindingLifecycle[]): Record<PulseFocus, number> {
  const counts: Record<PulseFocus, number> = {
    all: 0, needs_action: 0, queued_repair: 0, waiting_proof: 0,
    decisions: 0, proposals: 0, blocked: 0, platform: 0, resolved: 0, workflow_reported: 0,
  }
  findings.forEach((finding) => {
    const queue = pulseFindingPresentation(finding).queue
    counts[queue]++
    if (pulseFindingMatchesFocus(finding, 'all')) counts.all++
  })
  return counts
}

export function pulseFindingMatchesFocus(finding: PulseFindingLifecycle, focus: PulseFocus): boolean {
  const queue = pulseFindingPresentation(finding).queue
  return focus === 'all' ? !['resolved', 'workflow_reported'].includes(queue) : queue === focus
}

export function buildPulseWorkspaceModuleSummaries(
  definitions: PulseWorkspaceModuleDefinition[],
  findings: PulseFindingLifecycle[],
  reviews: PulseReviewRecord[],
  selections: PulseReviewFocus[] = [],
): PulseWorkspaceModuleSummary[] {
  const latestReviewByModule = new Map<string, PulseReviewRecord>()
  reviews.forEach((review) => {
    const module = normalizePulseWorkspaceModule(review.module)
    const current = latestReviewByModule.get(module)
    if (!current || review.recorded_at.localeCompare(current.recorded_at) > 0) {
      latestReviewByModule.set(module, review)
    }
  })

  return definitions.map((definition) => {
    const definitionModule = normalizePulseWorkspaceModule(definition.id)
    const moduleFindings = findings.filter((finding) => (
      pulseFindingReviewAreas(finding, selections).includes(definitionModule)
    ))
    const counts = pulseWorkspaceQueueCounts(moduleFindings)
    const fixing = moduleFindings.filter((finding) => finding.status === 'fixing'
      && pulseFindingPresentation(finding).queue === 'needs_action').length
    const awaitingVerification = moduleFindings.filter((finding) => finding.status === 'awaiting_verification'
      && pulseFindingPresentation(finding).queue === 'waiting_proof').length
    return {
      ...definition,
      findings: moduleFindings.length,
      active: counts.needs_action - fixing,
      fixing,
      awaitingVerification,
      awaitingRun: counts.waiting_proof - awaitingVerification,
      queuedForEngineering: counts.queued_repair,
      awaitingUser: counts.decisions,
      blocked: counts.blocked,
      proposals: counts.proposals,
      closed: counts.resolved,
      externalAction: counts.platform,
      workflowReported: counts.workflow_reported,
      recurring: moduleFindings.filter((finding) => (
        finding.seen_count > 1
        && finding.status !== 'external_action_required'
      )).length,
      latestReview: latestReviewByModule.get(definitionModule) || null,
    }
  })
}

export function selectPulseWorkspaceModule(
  summaries: PulseWorkspaceModuleSummary[],
): string | null {
  if (summaries.length === 0) return null
  const ranked = [...summaries].sort((a, b) => {
    const aPriority = (a.active + a.fixing + a.queuedForEngineering) * 100
      + (a.awaitingVerification + a.awaitingRun) * 20
      + a.awaitingUser * 10
      + a.recurring
    const bPriority = (b.active + b.fixing + b.queuedForEngineering) * 100
      + (b.awaitingVerification + b.awaitingRun) * 20
      + b.awaitingUser * 10
      + b.recurring
    if (aPriority !== bPriority) return bPriority - aPriority
    return (b.latestReview?.recorded_at || '').localeCompare(a.latestReview?.recorded_at || '')
  })
  return ranked[0]?.id || summaries[0].id
}
